package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui/messages"
)

// Sending then leaving the channel records where the user was for the
// way back. The placeholder's TS must not be that place: walking back
// would ask Slack for history around a TS it never issued.
func TestNavBackAfterSendNeverFetchesPlaceholder(t *testing.T) {
	var fetched []ids.MessageTS
	app := navFixtureApp(t, map[ids.ChannelID][]messages.MessageItem{
		"C1": {{TS: "1.0", Text: "old"}},
		"C2": {{TS: "10.0", Text: "c2"}},
	}, func(channelID ids.ChannelID, ts ids.MessageTS) core.Msg {
		fetched = append(fetched, ts)
		return nil
	})
	app.SetCurrentUserID("USELF")
	app.Update(ChannelSelectedMsg{ID: "C1", Name: "one", Type: "channel"})
	app.Update(SendMessageMsg{ChannelID: "C1", Text: "pending"})
	app.Update(ChannelSelectedMsg{ID: "C2", Name: "two", Type: "channel"})

	selected, _ := walkToCompletion(t, app, app.navigateBack())
	for _, ts := range fetched {
		if messages.IsLocalTS(string(ts)) {
			t.Fatalf("walking back fetched around %q", ts)
		}
	}
	if messages.IsLocalTS(selected) {
		t.Fatalf("walking back selected %q", selected)
	}
}

// Where the user is never names a placeholder or an ephemeral (Slack
// knows neither as a message of the conversation): it is named by the
// nearest message before it that Slack knows, else by the thread's
// parent or the channel alone. A placeholder that is not selected
// changes nothing.
func TestLocationNeverNamesPlaceholder(t *testing.T) {
	sent := func(ts string) messages.MessageItem { return messages.MessageItem{TS: ts, Text: ts} }
	local := func(ts string) messages.MessageItem { return messages.MessageItem{TS: ts, Text: "pending"} }
	eph := func(ts string) messages.MessageItem {
		return messages.MessageItem{TS: ts, Text: "only you", Ephemeral: true}
	}
	cases := []struct {
		name     string
		thread   bool
		rows     []messages.MessageItem
		selected string
		want     string // MessageTS
	}{
		{"channel, after a sent message", false, []messages.MessageItem{sent("1.0"), local("local:1")}, "local:1", "1.0"},
		{"channel, past another placeholder", false, []messages.MessageItem{sent("1.0"), local("local:1"), local("local:2")}, "local:2", "1.0"},
		{"channel, nothing sent before", false, []messages.MessageItem{local("local:1"), sent("2.0")}, "local:1", ""},
		{"channel, a sent message selected", false, []messages.MessageItem{sent("1.0"), local("local:1"), sent("3.0")}, "3.0", "3.0"},
		{"thread, after a sent reply", true, []messages.MessageItem{sent("2.0"), local("local:1")}, "local:1", "2.0"},
		{"thread, nothing sent before", true, []messages.MessageItem{local("local:1")}, "local:1", "1.000000"},
		// An ephemeral is not in Slack's history either.
		{"channel, past an ephemeral", false, []messages.MessageItem{sent("1.0"), eph("2.0"), local("local:1")}, "local:1", "1.0"},
		{"channel, an ephemeral selected", false, []messages.MessageItem{sent("1.0"), eph("2.0")}, "2.0", "1.0"},
		{"thread, an ephemeral selected", true, []messages.MessageItem{sent("2.0"), eph("3.0")}, "3.0", "2.0"},
		{"thread, only an ephemeral", true, []messages.MessageItem{eph("3.0")}, "3.0", "1.000000"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := newTestApp(t, withActiveTeam("T1"), withActiveChannel("C1"))
			if c.thread {
				a.threadPanel.SetThread(messages.MessageItem{TS: "1.000000", Text: "parent"}, c.rows, "C1", "1.000000")
				a.threadVisible = true
				a.focusedPanel = PanelThread
				for i, r := range c.rows {
					if r.TS == c.selected {
						a.threadPanel.SelectByIndex(i)
					}
				}
			} else {
				a.messagepane.SetMessages(c.rows)
				a.messagepane.SelectByTS(c.selected)
				a.focusedPanel = PanelMessages
			}
			loc, ok := a.currentLocation()
			for what, l := range map[string]Location{"currentLocation": loc, "currentPosition": a.currentPosition(), "departurePosition": a.departurePosition()} {
				if !ok || string(l.MessageTS) != c.want || l.ChannelID != "C1" {
					t.Errorf("%s = %+v, want MessageTS %q in C1", what, l, c.want)
				}
			}
		})
	}
}

// Marking a message unread puts the boundary on the nearest message
// before it that Slack knows: never on a placeholder or an ephemeral.
func TestMarkUnreadBoundarySkipsPlaceholdersAndEphemerals(t *testing.T) {
	rows := []messages.MessageItem{
		{TS: "1.000000", Text: "sent"},
		{TS: "local:1", Text: "pending"},
		{TS: "2.000000", Text: "only you", Ephemeral: true},
		{TS: "3.000000", Text: "from someone"},
	}
	for _, thread := range []bool{false, true} {
		a := newTestApp(t, withActiveTeam("T1"), withActiveChannel("C1"))
		if thread {
			a.threadPanel.SetThread(messages.MessageItem{TS: "0.500000"}, rows, "C1", "0.500000")
			a.threadVisible = true
			a.focusedPanel = PanelThread
			a.threadPanel.SelectByIndex(3)
		} else {
			a.messagepane.SetMessages(rows)
			a.focusedPanel = PanelMessages
		}
		got := deliveredMsgs[MarkUnreadMsg](t, a.markUnreadOfSelected())[0]
		if got.BoundaryTS != "1.000000" {
			t.Errorf("thread=%v: boundary %q, want 1.000000", thread, got.BoundaryTS)
		}
	}
}

// A thread is marked read up to its newest reply Slack knows, never a
// placeholder.
func TestNewestMarkableReplySkipsPlaceholders(t *testing.T) {
	for _, c := range []struct {
		rows []messages.MessageItem
		want string
	}{
		{[]messages.MessageItem{{TS: "1.0"}, {TS: "local:1"}}, "1.0"},
		{[]messages.MessageItem{{TS: "1.0"}, {TS: "2.0", Ephemeral: true}}, "1.0"},
		{[]messages.MessageItem{{TS: "local:1"}}, ""},
	} {
		if got := newestMarkableReplyTS(c.rows); got != c.want {
			t.Errorf("newestMarkableReplyTS(%q) = %q, want %q", rowsOf(c.rows), got, c.want)
		}
	}
}

// Reaction navigation that is active when a new row is appended (the
// user's own send moves the selection onto its placeholder) ends, and
// the reaction choke points refuse a placeholder whatever reaches them.
func TestReactionsNeverTargetPlaceholder(t *testing.T) {
	a := newTestApp(t, withActiveTeam("T1"), withActiveChannel("C1"), withMessages(messages.MessageItem{
		TS: "1.000000", Text: "hi", Reactions: []messages.ReactionItem{{Emoji: "tada", Count: 1}},
	}))
	a.SetCurrentUserID("USELF")
	focusMessages(t, a)
	a.messagepane.EnterReactionNav()
	if !a.messagepane.ReactionNavActive() {
		t.Fatal("reaction nav did not start")
	}
	a.Update(SendMessageMsg{ChannelID: "C1", Text: "pending"})
	if a.messagepane.ReactionNavActive() {
		t.Error("reaction nav still active after the selection moved to the new row")
	}
	sel, _ := a.messagepane.SelectedMessage()
	if !messages.IsLocalTS(sel.TS) {
		t.Fatalf("selected %q, want the placeholder", sel.TS)
	}
	a.toggleReactionOnSelected(a.messagepane, "C1", "tada")
	if got := statusbarText(a); !strings.Contains(got, stillSendingText) {
		t.Errorf("status bar = %q after a toggle on a placeholder, want %q", got, stillSendingText)
	}
	if got := a.messagepane.Messages()[1].Reactions; len(got) != 0 {
		t.Errorf("placeholder reactions = %+v, want none", got)
	}
	a.openReactionPicker(a.messagepane, "C1")
	if a.mode == ModeReactionPicker {
		t.Error("reaction picker opened on a placeholder")
	}
}

// The reducer arms that call Slack with a message TS refuse a
// placeholder, whoever built the message: nothing reaches the service.
func TestMessageServiceNeverGetsPlaceholder(t *testing.T) {
	var calls []string
	record := func(what string, ts ids.MessageTS) {
		calls = append(calls, what+" "+string(ts))
	}
	a := newTestApp(t, withActiveTeam("T1"), withActiveChannel("C1"))
	a.SetMessageService(core.NewMessageService(core.MessageServiceFuncs{
		Edit:   func(_ ids.ChannelID, ts ids.MessageTS, _ string) core.Msg { record("edit", ts); return nil },
		Delete: func(_ ids.ChannelID, ts ids.MessageTS) core.Msg { record("delete", ts); return nil },
		MarkUnread: func(_ ids.ChannelID, _ ids.ThreadTS, ts ids.MessageTS, _ int) core.Msg {
			record("mark unread", ts)
			return nil
		},
	}))
	for _, msg := range []tea.Msg{
		EditMessageMsg{ChannelID: "C1", TS: "local:1", NewText: "x"},
		DeleteMessageMsg{ChannelID: "C1", TS: "local:1"},
		MarkUnreadMsg{ChannelID: "C1", BoundaryTS: "local:1"},
	} {
		_, cmd := a.Update(msg)
		drainCmd(cmd)
	}
	if len(calls) != 0 {
		t.Fatalf("service calls %q, want none for a placeholder", calls)
	}
}

// Older history is fetched from the oldest message Slack knows, never
// from a placeholder heading an empty conversation.
func TestOldestTSSkipsPlaceholder(t *testing.T) {
	m := messages.New([]messages.MessageItem{{TS: "local:1"}, {TS: "2.0"}}, "general")
	if got := m.OldestTS(); got != "2.0" {
		t.Errorf("OldestTS = %q, want 2.0", got)
	}
	m = messages.New([]messages.MessageItem{{TS: "local:1"}}, "general")
	if got := m.OldestTS(); got != "" {
		t.Errorf("OldestTS = %q, want none", got)
	}
}
