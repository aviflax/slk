package main

import (
	"sync"
	"testing"
	"testing/synctest"
)

// railQueryCall is one call of the thread query, held until the test
// answers it, so a test decides exactly when each background query
// finishes and with what result.
type railQueryCall struct {
	team, self string
	reply      chan bool
}

type railQueryFake struct {
	calls chan railQueryCall
	mu    sync.Mutex
	n     int
}

func newRailQueryFake() *railQueryFake {
	return &railQueryFake{calls: make(chan railQueryCall)}
}

func (f *railQueryFake) query(team, self string) bool {
	f.mu.Lock()
	f.n++
	f.mu.Unlock()
	c := railQueryCall{team: team, self: self, reply: make(chan bool)}
	f.calls <- c
	return <-c.reply
}

func (f *railQueryFake) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.n
}

// next waits for the next query call. There is no deadline: a query
// that never comes hangs the test and go test's -timeout names it.
func (f *railQueryFake) next(t *testing.T) railQueryCall {
	t.Helper()
	return <-f.calls
}

// expectNoCall fails if another query is on its way. It must run inside
// a synctest bubble: Wait returns once every goroutine in the bubble is
// blocked, so a worker the cache should not have started has either
// already reached the fake's send by then or does not exist.
func (f *railQueryFake) expectNoCall(t *testing.T) {
	t.Helper()
	synctest.Wait()
	select {
	case c := <-f.calls:
		t.Errorf("unexpected thread query for %s (self %s)", c.team, c.self)
		c.reply <- false
	default:
	}
}

// notifyLog records the team IDs the cache reports as changed.
type notifyLog struct {
	ch   chan string
	mu   sync.Mutex
	seen []string
}

func newNotifyLog() *notifyLog { return &notifyLog{ch: make(chan string, 16)} }

func (l *notifyLog) notify(teamID string) {
	l.mu.Lock()
	l.seen = append(l.seen, teamID)
	l.mu.Unlock()
	l.ch <- teamID
}

// wait blocks for the next notification, with no deadline.
func (l *notifyLog) wait(t *testing.T) string {
	t.Helper()
	return <-l.ch
}

func (l *notifyLog) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.seen...)
}

// lookup calls c.Unread. The lookup runs on the UI goroutine, so if it
// waited for the query, every keystroke queued behind a read-state event
// would wait too (~820ms per event with 1000 subscribed threads). With
// the fake query nobody answers until the test does, so a blocking
// lookup hangs the test and go test's -timeout names it.
func lookup(t *testing.T, c *railThreadsCache, teamID, selfUserID string) bool {
	t.Helper()
	return c.Unread(teamID, selfUserID)
}

func TestRailThreadsCache_LookupDoesNotWaitForTheQuery(t *testing.T) {
	q := newRailQueryFake()
	c := newRailThreadsCache(q.query)

	if lookup(t, c, "T1", "USELF") {
		t.Errorf("first lookup = true, want false (nothing computed yet)")
	}
	q.next(t).reply <- false // release the background query
}

// Breaks caught: a result that is never stored, a change that is never
// reported, and reporting an unchanged result, which would make every
// UI refresh trigger another refresh forever.
func TestRailThreadsCache_ReportsAChangeOnceAndServesIt(t *testing.T) {
	q := newRailQueryFake()
	n := newNotifyLog()
	c := newRailThreadsCache(q.query)
	c.SetNotify(n.notify)

	lookup(t, c, "T1", "USELF")
	call := q.next(t)
	if call.team != "T1" || call.self != "USELF" {
		t.Fatalf("query(%q, %q), want (T1, USELF)", call.team, call.self)
	}
	call.reply <- true
	if got := n.wait(t); got != "T1" {
		t.Fatalf("notified %q, want T1", got)
	}
	if !lookup(t, c, "T1", "USELF") {
		t.Fatal("lookup after the change = false, want true")
	}

	// That lookup recomputes; the answer is unchanged, so no report.
	q.next(t).reply <- true
	// A team's next query cannot start until the previous result is
	// stored and any report for it sent, so by the time it arrives an
	// unwanted report would already be logged.
	lookup(t, c, "T1", "USELF")
	q.next(t).reply <- false
	if got := n.wait(t); got != "T1" {
		t.Fatalf("notified %q, want T1", got)
	}
	if lookup(t, c, "T1", "USELF") {
		t.Fatal("lookup after the second change = true, want false")
	}
	lookup(t, c, "T1", "USELF")
	q.next(t).reply <- false // unchanged
	lookup(t, c, "T1", "USELF")
	call = q.next(t) // proves the previous result was processed
	if got := n.all(); len(got) != 2 {
		t.Errorf("notifications = %v, want exactly two (one per change)", got)
	}
	call.reply <- false
}

// Break caught: one query (or one goroutine) per lookup. A burst of
// read-state events while a query runs must cost one more query, run
// with the newest self user ID.
func TestRailThreadsCache_BurstWhileQueryingRunsOneMore(t *testing.T) {
	synctest.Test(t, testRailBurstWhileQueryingRunsOneMore)
}

func testRailBurstWhileQueryingRunsOneMore(t *testing.T) {
	q := newRailQueryFake()
	c := newRailThreadsCache(q.query)

	lookup(t, c, "T1", "U1")
	first := q.next(t)
	for _, self := range []string{"U2", "U3", "U4", "U5", "U6"} {
		lookup(t, c, "T1", self)
	}
	first.reply <- false

	second := q.next(t)
	if second.self != "U6" {
		t.Errorf("rerun self = %q, want U6 (the latest lookup's)", second.self)
	}
	second.reply <- false

	lookup(t, c, "T1", "U6")
	q.next(t).reply <- false
	q.expectNoCall(t)
	if got := q.count(); got != 3 {
		t.Errorf("queries = %d, want 3 (first, one rerun for the burst, the last lookup)", got)
	}
}

// Break caught: one shared bit instead of one per workspace.
func TestRailThreadsCache_TeamsAreIndependent(t *testing.T) {
	q := newRailQueryFake()
	n := newNotifyLog()
	c := newRailThreadsCache(q.query)
	c.SetNotify(n.notify)

	lookup(t, c, "T1", "U1")
	lookup(t, c, "T2", "U2")
	results := map[string]bool{"T1": true, "T2": false}
	for range 2 {
		call := q.next(t)
		call.reply <- results[call.team]
	}
	if got := n.wait(t); got != "T1" {
		t.Fatalf("notified %q, want T1", got)
	}

	if !lookup(t, c, "T1", "U1") {
		t.Error("T1 lookup = false, want true")
	}
	if lookup(t, c, "T2", "U2") {
		t.Error("T2 lookup = true, want false")
	}
	for range 2 {
		call := q.next(t)
		call.reply <- results[call.team]
	}
}

// main.go installs the notifier only once the bubbletea program exists,
// after the reader is wired. A result computed before then must still
// be served, not dropped, and computing it must not need a notifier.
func TestRailThreadsCache_ResultBeforeSetNotifyIsKept(t *testing.T) {
	q := newRailQueryFake()
	c := newRailThreadsCache(q.query)

	lookup(t, c, "T1", "U1")
	q.next(t).reply <- true
	// The next lookup's query only starts once the first result is stored.
	lookup(t, c, "T1", "U1")
	q.next(t).reply <- true

	if !lookup(t, c, "T1", "U1") {
		t.Error("lookup = false, want true (computed before SetNotify)")
	}
	q.next(t).reply <- true
}

// End to end with the real query: an unread subscribed thread in SQLite
// reaches the cache, is reported, and lights the lookup.
func TestRailThreadsCache_WithTheRealQuery(t *testing.T) {
	db := newTestDB(t)
	seedSubscribedThread(t, db, "1700000150.000000", "1700000200.000000")
	n := newNotifyLog()
	c := newRailThreadsCache(railThreadsUnread(db))
	c.SetNotify(n.notify)

	if lookup(t, c, "T1", "USELF") {
		t.Fatal("first lookup = true, want false (nothing computed yet)")
	}
	if got := n.wait(t); got != "T1" {
		t.Fatalf("notified %q, want T1", got)
	}
	if !lookup(t, c, "T1", "USELF") {
		t.Error("lookup after the report = false, want true")
	}
}
