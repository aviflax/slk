package demo

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

type mark string

func emit(s string) Act { return func(*Scene) []tea.Msg { return []tea.Msg{mark(s)} } }

type recorder struct {
	mu      sync.Mutex
	changed *sync.Cond
	msgs    []tea.Msg
}

func (r *recorder) send(m tea.Msg) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.msgs = append(r.msgs, m)
	if r.changed != nil {
		r.changed.Broadcast()
	}
}

func (r *recorder) all() []tea.Msg {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.msgs)
}

// waitFor blocks until at least n messages have arrived. It has no
// deadline of its own: a director that never plays the step hangs here
// and go test's -timeout names the test.
func (r *recorder) waitFor(n int) []tea.Msg {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.changed == nil {
		r.changed = sync.NewCond(&r.mu)
	}
	for len(r.msgs) < n {
		r.changed.Wait()
	}
	return slices.Clone(r.msgs)
}

// playOut dispatches the events in order on the calling goroutine, the
// way run does, and returns once every rule they started has finished.
// Use it with an instant clock when the test needs to know that nothing
// more is coming, which run's loop cannot say.
func playOut(d *Director, send func(tea.Msg), events ...Event) {
	var wg sync.WaitGroup
	for _, ev := range events {
		d.dispatch(context.Background(), &wg, send, ev)
	}
	wg.Wait()
}

// delays is an instant clock that records every delay it was asked for.
type delays struct {
	mu  sync.Mutex
	got []time.Duration
}

func (dl *delays) instant(d time.Duration) <-chan time.Time {
	dl.mu.Lock()
	dl.got = append(dl.got, d)
	dl.mu.Unlock()
	ch := make(chan time.Time, 1)
	ch <- time.Time{}
	return ch
}

func (dl *delays) all() []time.Duration {
	dl.mu.Lock()
	defer dl.mu.Unlock()
	return slices.Clone(dl.got)
}

// start runs d until the test ends. The returned stop cancels it and
// waits for run to return.
func start(t *testing.T, d *Director, send func(tea.Msg)) (stop func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { d.run(ctx, send); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	return func() {
		cancel()
		<-done
	}
}

func TestDirectorPlaysStepsInOrderAfterTheirDelays(t *testing.T) {
	d := newDirector(fixtureWorld(), []Rule{{When: on(EventChannelOpened, "C1"), Steps: []Step{
		{After: time.Second, Act: emit("a")},
		{After: 2 * time.Second, Act: emit("b")},
		{After: 3 * time.Second, Act: emit("c")},
	}}})
	dl := &delays{}
	d.after = dl.instant
	rec := &recorder{}
	start(t, d, rec.send)

	d.observe(Event{Kind: EventChannelOpened, ChannelID: "C1"})

	if got, want := rec.waitFor(3), []tea.Msg{mark("a"), mark("b"), mark("c")}; !slices.Equal(got, want) {
		t.Errorf("messages = %v, want %v", got, want)
	}
	if got, want := dl.all(), []time.Duration{time.Second, 2 * time.Second, 3 * time.Second}; !slices.Equal(got, want) {
		t.Errorf("delays = %v, want %v", got, want)
	}
}

func TestDirectorOnceRulesPlayOnce(t *testing.T) {
	d := newDirector(fixtureWorld(), []Rule{
		{When: on(EventChannelOpened, "C1"), Once: true, Steps: []Step{{Act: emit("once")}}},
		{When: on(EventChannelOpened, "C2"), Steps: []Step{{Act: emit("done")}}},
	})
	d.after = (&delays{}).instant
	rec := &recorder{}

	playOut(d, rec.send,
		Event{Kind: EventChannelOpened, ChannelID: "C1"},
		Event{Kind: EventChannelOpened, ChannelID: "C1"},
		Event{Kind: EventChannelOpened, ChannelID: "C2"})

	got := rec.all()
	if n := len(slices.DeleteFunc(slices.Clone(got), func(m tea.Msg) bool { return m != mark("once") })); n != 1 {
		t.Errorf("once-rule played %d times: %v", n, got)
	}
}

func TestDirectorFiresStartOnItsOwn(t *testing.T) {
	d := newDirector(fixtureWorld(), []Rule{{When: on(EventStart, anyChannel), Steps: []Step{{Act: emit("hello")}}}})
	d.after = (&delays{}).instant
	rec := &recorder{}
	start(t, d, rec.send)
	if got := rec.waitFor(1); got[0] != mark("hello") {
		t.Errorf("got %v", got)
	}
}

func TestDirectorCancelStopsPendingSteps(t *testing.T) {
	d := newDirector(fixtureWorld(), []Rule{{When: on(EventStart, anyChannel), Steps: []Step{{After: time.Hour, Act: emit("late")}}}})
	waiting := make(chan struct{})
	d.after = func(time.Duration) <-chan time.Time { // never fires
		close(waiting)
		return make(chan time.Time)
	}
	rec := &recorder{}
	stop := start(t, d, rec.send)
	<-waiting // the step is now parked on its delay
	stop()
	if got := rec.all(); len(got) != 0 {
		t.Errorf("a step fired after cancel: %v", got)
	}
}

func TestObserveNeverBlocks(t *testing.T) {
	d := newDirector(fixtureWorld(), nil)
	// Nobody runs the director and the queue holds 64: a blocking
	// observe hangs here until go test's -timeout.
	for i := 0; i < 1000; i++ {
		d.observe(Event{Kind: EventMessageSent})
	}
}
