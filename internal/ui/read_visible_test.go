package ui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ui/messages"
)

// A message that arrives in the conversation on screen counts as read
// only once the user can see it. One that lands below the fold, because
// the user moved up to read, stays unread (on Slack and in the sidebar)
// until it is brought on screen, as Slack's own clients do.

// stepRendered delivers msg through Update and draws a frame after it,
// as the bubbletea runtime does after every message, then does the same
// for every message its commands produce. A check that reads the last
// frame therefore sees the frame the message before it produced.
func stepRendered(t *testing.T, app *App, msg tea.Msg) {
	t.Helper()
	stepRenderedDepth(t, app, msg, 0)
}

func stepRenderedDepth(t *testing.T, app *App, msg tea.Msg, depth int) {
	t.Helper()
	if msg == nil || depth > 6 {
		return
	}
	_, cmd := app.Update(msg)
	_ = app.View()
	for _, next := range drainBatch(cmd) {
		stepRenderedDepth(t, app, next, depth+1)
	}
}

// foldMessages is n one-line messages "m1".."mn" at ts "1.000000".."n.000000".
func foldMessages(n int, threadTS string) []messages.MessageItem {
	var out []messages.MessageItem
	for i := 1; i <= n; i++ {
		out = append(out, messages.MessageItem{
			TS: fmt.Sprintf("%d.000000", i), ThreadTS: threadTS,
			UserID: "U2", UserName: "u", Text: fmt.Sprintf("m%d", i),
		})
	}
	return out
}

// readingChannelApp is a focused App on C1 showing twenty messages, the
// channel pane focused and drawn at the bottom, with marks recorded.
// moveUp moves the cursor that many messages up first, which leaves
// the newest below the fold once the cursor is far enough up.
func readingChannelApp(t *testing.T, moveUp int) (*App, *[]string) {
	t.Helper()
	app := newTestApp(t, withSize(120, 20), withMessages(foldMessages(20, "")...),
		withActiveChannel("C1"), withRender())
	calls := captureMarks(t, app)
	app.focusedPanel = PanelMessages
	for range moveUp {
		app.messagepane.MoveUp()
		_ = app.View()
	}
	return app, calls
}

func channelMarks(calls []string, channelID string) []string {
	var out []string
	for _, c := range calls {
		if strings.HasPrefix(c, "ch:"+channelID+"/") {
			out = append(out, c)
		}
	}
	return out
}

func TestArrivalBelowTheFold_StaysUnreadUntilScrolledTo(t *testing.T) {
	app, calls := readingChannelApp(t, 15)
	var reports int
	app.setStatusReporterForTest(func(int, int, string, string) { reports++ })

	stepRendered(t, app, NewMessageMsg{ChannelID: "C1", Message: foldMessages(21, "")[20]})

	if len(*calls) != 0 {
		t.Fatalf("calls = %v: a message below the fold must not be marked read", *calls)
	}
	if reports == 0 {
		t.Error("no read-state notification: the sidebar must show the unread message")
	}

	stepRendered(t, app, keyPress('G'))

	if !slices.Equal(*calls, []string{"ch:C1/21.000000"}) {
		t.Fatalf("calls after G = %v, want the held mark issued at 21.000000", *calls)
	}
}

// Focus coming back does not issue a mark held for visibility: the
// message is still below the fold. A blurred arrival at the end is
// unchanged (TestBlurredArrival_DoesNotMarkUntilFocusReturns).
func TestBlurredArrivalBelowTheFold_FocusDoesNotMarkIt(t *testing.T) {
	app, calls := readingChannelApp(t, 15)
	stepRendered(t, app, tea.BlurMsg{})

	stepRendered(t, app, NewMessageMsg{ChannelID: "C1", Message: foldMessages(21, "")[20]})
	stepRendered(t, app, tea.FocusMsg{})

	if len(*calls) != 0 {
		t.Fatalf("calls = %v: refocusing must not mark a message still below the fold", *calls)
	}

	stepRendered(t, app, keyPress('G'))

	if !slices.Equal(*calls, []string{"ch:C1/21.000000"}) {
		t.Fatalf("calls after G = %v, want the held mark issued at 21.000000", *calls)
	}
}

func TestArrivalBelowTheFold_LeavingTheChannelLeavesItUnread(t *testing.T) {
	app, calls := readingChannelApp(t, 15)

	stepRendered(t, app, NewMessageMsg{ChannelID: "C1", Message: foldMessages(21, "")[20]})
	stepRendered(t, app, ChannelSelectedMsg{ID: "C2", Name: "random", Type: "channel"})
	// Bring C2's end on screen, regain focus, and let any tick fire:
	// none of it may issue C1's held mark.
	stepRendered(t, app, keyPress('G'))
	stepRendered(t, app, tea.BlurMsg{})
	stepRendered(t, app, tea.FocusMsg{})
	stepRendered(t, app, markFlushMsg{})

	if got := channelMarks(*calls, "C1"); len(got) != 0 {
		t.Fatalf("C1 marks = %v: leaving without scrolling down must leave C1 unread", got)
	}
}

func TestArrivalAtTheBottom_IsMarkedRead(t *testing.T) {
	app, calls := readingChannelApp(t, 0)

	stepRendered(t, app, NewMessageMsg{ChannelID: "C1", Message: foldMessages(21, "")[20]})

	if !slices.Equal(*calls, []string{"ch:C1/21.000000"}) {
		t.Fatalf("calls = %v, want the message on screen marked read", *calls)
	}
}

// readingThreadApp is readingChannelApp for an open thread on C1 with
// twenty replies, the thread pane focused.
func readingThreadApp(t *testing.T, moveUp int) (*App, *[]string) {
	t.Helper()
	app := newTestApp(t, withSize(160, 20), withMessages(foldMessages(3, "")...),
		withActiveChannel("C1"))
	calls := captureMarks(t, app)
	parent := messages.MessageItem{TS: "1.000000", ThreadTS: "1.000000", UserID: "U2", UserName: "u", Text: "parent"}
	app.threadVisible = true
	app.threadPanel.SetThread(parent, foldMessages(20, "1.000000")[1:], "C1", "1.000000")
	app.focusedPanel = PanelThread
	_ = app.View()
	for range moveUp {
		app.threadPanel.MoveUp()
		_ = app.View()
	}
	return app, calls
}

func TestThreadReplyBelowTheFold_StaysUnreadUntilScrolledTo(t *testing.T) {
	app, calls := readingThreadApp(t, 15)

	stepRendered(t, app, NewMessageMsg{ChannelID: "C1", Message: foldMessages(21, "1.000000")[20]})

	if len(*calls) != 0 {
		t.Fatalf("calls = %v: a reply below the fold must not be marked read", *calls)
	}

	stepRendered(t, app, keyPress('G'))

	if !slices.Equal(*calls, []string{"th:C1/1.000000/21.000000"}) {
		t.Fatalf("calls after G = %v, want the held thread mark issued at 21.000000", *calls)
	}
}

func TestThreadReplyAtTheBottom_IsMarkedRead(t *testing.T) {
	app, calls := readingThreadApp(t, 0)

	stepRendered(t, app, NewMessageMsg{ChannelID: "C1", Message: foldMessages(21, "1.000000")[20]})

	if !slices.Equal(*calls, []string{"th:C1/1.000000/21.000000"}) {
		t.Fatalf("calls = %v, want the reply on screen marked read", *calls)
	}
}
