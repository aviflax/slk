package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/slack-go/slack"

	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/ui/messages"
)

// TestFetchThreadReplies_PrunesCachedRepliesSlackDidNotReturn is the
// regression for a thread that flickered unread forever: a Slackbot
// ephemeral cached from the WebSocket sat in the thread as its newest
// reply. conversations.replies never returns it, so the fetch must drop
// it from the cache, or the cached open marks the thread read at the
// ephemeral's ts and the fetched open marks it back behind it.
func TestFetchThreadReplies_PrunesCachedRepliesSlackDidNotReturn(t *testing.T) {
	db := newTestDB(t)
	const parent = "1700000000.000100"
	for _, ts := range []string{parent, "1700000001.000100", "1700000002.000000"} {
		if err := db.UpsertMessage(cache.Message{TS: ts, ChannelID: "C1", WorkspaceID: "T1",
			UserID: "USLACKBOT", ThreadTS: parent, CreatedAt: 1}); err != nil {
			t.Fatal(err)
		}
	}

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"has_more":false,"messages":[
			{"type":"message","user":"U1","ts":"1700000000.000100","thread_ts":"1700000000.000100","text":"parent"},
			{"type":"message","user":"U2","ts":"1700000001.000100","thread_ts":"1700000000.000100","text":"reply"}]}`))
	}))
	defer srv.Close()

	replies := fetchThreadReplies(newTestClient(t, srv), "C1", parent, db, newUserNameStore(nil), "15:04", nil, nil)
	if len(replies) != 1 {
		t.Fatalf("fetchThreadReplies returned %d replies, want 1", len(replies))
	}

	rows, err := db.GetThreadReplies("C1", parent)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range rows {
		got = append(got, r.TS)
	}
	if want := []string{parent, "1700000001.000100"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("cached thread after fetch = %v, want %v", got, want)
	}
}

// A parent's latest_reply reaches its MessageItem from a fetch and from
// the cache, so the pane knows which replies Slack's reply_count holds.
func TestFetchedAndCachedParentsCarryLatestReply(t *testing.T) {
	db := newTestDB(t)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"has_more":false,"messages":[
			{"type":"message","user":"U1","ts":"1700000000.000100","thread_ts":"1700000000.000100",
			 "reply_count":2,"latest_reply":"1700000005.000100","text":"parent"}]}`))
	}))
	defer srv.Close()

	fetched := fetchChannelMessages(newTestClient(t, srv), "C1", db, newUserNameStore(nil), "15:04", nil, nil)
	if len(fetched) != 1 || fetched[0].LatestReply != "1700000005.000100" {
		t.Fatalf("fetched = %+v, want latest_reply carried", fetched)
	}
	cached := loadCachedMessages(db, "USELF", "C1", newUserNameStore(nil), "15:04", nil)
	if len(cached) != 1 || cached[0].LatestReply != "1700000005.000100" {
		t.Fatalf("cached = %+v, want latest_reply carried", cached)
	}
}

// Slack's edited mark reaches the MessageItem by every path a message
// takes to a pane: each network fetch (history, older history, around a
// message, a thread's replies), the cache those fetches write, and the
// cache the WebSocket writes. A reload used to drop "(edited)".
func TestEveryHistoryPathCarriesTheEditedMark(t *testing.T) {
	const parent, edited, plain = "1700000000.000100", "1700000001.000100", "1700000002.000100"
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Replies carry the parent's thread_ts; history is top level.
		threadTS := `""`
		if strings.Contains(r.URL.Path, "replies") {
			threadTS = `"1700000000.000100"`
		}
		_, _ = w.Write([]byte(strings.ReplaceAll(`{"ok":true,"has_more":false,"messages":[
			{"type":"message","user":"U1","ts":"1700000002.000100","thread_ts":THREAD,"text":"plain"},
			{"type":"message","user":"U1","ts":"1700000001.000100","thread_ts":THREAD,"text":"changed",
			 "edited":{"user":"U1","ts":"1700000009.000000"}},
			{"type":"message","user":"U1","ts":"1700000000.000100","thread_ts":"1700000000.000100","text":"parent"}]}`, "THREAD", threadTS)))
	}))
	defer srv.Close()

	check := func(path string, items []messages.MessageItem) {
		t.Helper()
		seen := false
		for _, m := range items {
			switch m.TS {
			case edited:
				seen = true
				if !m.IsEdited {
					t.Errorf("%s: the edited message is not marked edited", path)
				}
			case plain, parent:
				if m.IsEdited {
					t.Errorf("%s: %s is marked edited", path, m.TS)
				}
			}
		}
		if !seen {
			t.Errorf("%s: did not return the edited message: %+v", path, items)
		}
	}
	names := newUserNameStore(nil)

	db := newTestDB(t)
	check("fetchChannelMessages", fetchChannelMessages(newTestClient(t, srv), "C1", db, names, "15:04", nil, nil))
	check("loadCachedMessages after a fetch", loadCachedMessages(db, "USELF", "C1", names, "15:04", nil))

	older, err := fetchOlderMessages(newTestClient(t, srv), "C2", "1800000000.000000", newTestDB(t), names, "15:04", nil)
	if err != nil {
		t.Fatal(err)
	}
	check("fetchOlderMessages", older)
	check("fetchMessagesAround", fetchMessagesAround(newTestClient(t, srv), "C3", edited, newTestDB(t), names, "15:04", nil))

	db = newTestDB(t)
	check("fetchThreadReplies", fetchThreadReplies(newTestClient(t, srv), "C4", parent, db, names, "15:04", nil, nil))
	check("loadCachedThreadReplies", loadCachedThreadReplies(db, "USELF", "C4", parent, names, "15:04", nil))

	db = newTestDB(t)
	h := &rtmEventHandler{db: db, wsCtx: &WorkspaceContext{}, isActive: func() bool { return true },
		activeChannelID: func() string { return "C5" }}
	h.OnMessage("C5", "U1", plain, "plain", "", "", false, nil, nil, slack.Blocks{}, nil, "", "")
	h.OnMessage("C5", "U1", edited, "changed", "", "", true, &slack.Edited{User: "U1", Timestamp: "1700000009.000000"}, nil, slack.Blocks{}, nil, "", "")
	check("loadCachedMessages after the WebSocket", loadCachedMessages(db, "USELF", "C5", names, "15:04", nil))
}
