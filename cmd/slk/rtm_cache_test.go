package main

import (
	"testing"

	"github.com/slack-go/slack"

	"github.com/gammons/slk/internal/cache"
)

// A WebSocket event for a thread parent (an edit, an unfurl) carries no
// reply count or latest reply, and a parent's own events carry an empty
// thread_ts. Caching it keeps what the history fetch stored for those,
// so a parent read back from the cache still shows its replies and
// still skips counting a reply Slack already counted (LatestReply).
func TestWebSocketEventKeepsACachedParentsThread(t *testing.T) {
	const parent, latest = "1700000001.000000", "1700000005.000000"
	db := newTestDB(t)
	if err := db.UpsertMessage(cache.Message{
		TS: parent, ChannelID: "C5", WorkspaceID: "T1", UserID: "U1", Text: "parent",
		ThreadTS: parent, ReplyCount: 3,
		RawJSON: `{"type":"message","ts":"` + parent + `","thread_ts":"` + parent + `","reply_count":3,"latest_reply":"` + latest + `","text":"parent"}`,
	}); err != nil {
		t.Fatal(err)
	}
	h := &rtmEventHandler{db: db, wsCtx: &WorkspaceContext{}, isActive: func() bool { return true },
		activeChannelID: func() string { return "C5" }}
	h.OnMessage("C5", "U1", parent, "parent edited", "", "", true, &slack.Edited{User: "U1", Timestamp: "1700000009.000000"}, nil, slack.Blocks{}, nil, "", "")

	items := loadCachedMessages(db, "USELF", "C5", newUserNameStore(nil), "15:04", nil)
	if len(items) != 1 {
		t.Fatalf("cached rows = %+v, want the parent", items)
	}
	got := items[0]
	if got.Text != "parent edited" || !got.IsEdited {
		t.Errorf("parent = %q edited=%v, want the edit applied", got.Text, got.IsEdited)
	}
	if got.ReplyCount != 3 || got.LatestReply != latest || got.ThreadTS != parent {
		t.Errorf("parent reply_count=%d latest_reply=%q thread_ts=%q, want 3, %s and %s kept", got.ReplyCount, got.LatestReply, got.ThreadTS, latest, parent)
	}
}
