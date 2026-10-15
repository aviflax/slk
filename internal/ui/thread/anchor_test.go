package thread

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/gammons/slk/internal/bubbles/scrollbar"
	"github.com/gammons/slk/internal/ui/messages"
)

// The thread pane keeps what the user sees in place when its replies
// change, as the channel pane does (messages/anchor_test.go). These
// tests read the rendered frame, not indexes.

const anchorH, anchorW = 16, 60

// anchorThread is a thread of twenty one-line replies "r1".."r20",
// rendered once at the bottom.
func anchorThread(t *testing.T) *Model {
	t.Helper()
	var replies []messages.MessageItem
	for i := 1; i <= 20; i++ {
		replies = append(replies, messages.MessageItem{TS: fmt.Sprintf("%d.000000", i), UserName: "u", Text: fmt.Sprintf("r%d", i)})
	}
	m := New()
	m.SetThread(messages.MessageItem{TS: "0.500000", UserName: "p", Text: "parent"}, replies, "C1", "0.500000")
	_ = m.View(anchorH, anchorW)
	return m
}

// threadFrame renders the pane and returns its content rows (no
// header), stripped of styling, the selection border and the scrollbar.
func threadFrame(m *Model) []string {
	rows := strings.Split(ansi.Strip(m.View(anchorH, anchorW)), "\n")[m.ChromeHeight():]
	for i, r := range rows {
		r = strings.TrimLeft(r, "▌ ")
		rows[i] = strings.TrimRight(r, " "+scrollbar.TrackGlyph+scrollbar.ThumbGlyph)
	}
	return rows
}

func frameHas(rows []string, text string) bool {
	return slices.ContainsFunc(rows, func(r string) bool { return strings.Contains(r, text) })
}

// A colleague's reply that arrives while the user's own reply is
// sending lands before the placeholder and is selected. Both show
// whole, and the user's reply stays on screen once it settles, whether
// it stays last or moves before the colleague's.
func TestReplyArrivingAheadOfAPendingSendShowsBoth(t *testing.T) {
	for _, sentTS := range []string{"22.000000", "20.500000"} {
		m := anchorThread(t)
		m.InsertLocal(messages.MessageItem{TS: messages.LocalTS(1), UserName: "me", Text: "my-pending-text"})
		_ = m.View(anchorH, anchorW)
		m.AddReply(messages.MessageItem{TS: "21.000000", UserName: "colleague", Text: "colleague-text"})
		f := threadFrame(m)
		if !frameHas(f, "colleague-text") || !strings.Contains(f[len(f)-1], "my-pending-text") {
			t.Fatalf("after the arrival, want both replies whole on screen:\n%s", strings.Join(f, "\n"))
		}
		m.Settle(messages.LocalTS(1), messages.MessageItem{TS: sentTS, UserName: "me", Text: "my-sent-text"})
		f = threadFrame(m)
		if !frameHas(f, "colleague-text") || !frameHas(f, "my-sent-text") {
			t.Errorf("settled at %s, want both replies whole on screen:\n%s", sentTS, strings.Join(f, "\n"))
		}
		last := "colleague-text"
		if sentTS == "22.000000" {
			last = "my-sent-text"
		}
		if !strings.Contains(f[len(f)-1], last) {
			t.Errorf("settled at %s, want %s on the last row:\n%s", sentTS, last, strings.Join(f, "\n"))
		}
	}
}

// A pane at the bottom stays there when the last reply grows or
// shrinks.
func TestThreadBottomStaysWhenTheLastReplyChangesHeight(t *testing.T) {
	for _, text := range []string{"one\ntwo\nthree\nfour\nlast-line", "last-line"} {
		m := anchorThread(t)
		m.Patch("20.000000", messages.Body{Text: "x\ny\nz"}.Apply)
		_ = m.View(anchorH, anchorW)
		m.Patch("20.000000", messages.Body{Text: text}.Apply)
		f := threadFrame(m)
		if !strings.Contains(f[len(f)-1], "last-line") {
			t.Errorf("the last row is %q, want the last reply's last line:\n%s", f[len(f)-1], strings.Join(f, "\n"))
		}
	}
}

// A pane scrolled up shows the same rows after a reply is added,
// removed, restored, resized or settled above the window.
func TestThreadRowsAboveTheWindowDoNotMoveIt(t *testing.T) {
	changes := map[string]func(m *Model){
		"insert above": func(m *Model) { m.AddReply(messages.MessageItem{TS: "2.500000", UserName: "u", Text: "late"}) },
		"remove above": func(m *Model) { m.Remove("2.000000") },
		"restore above": func(m *Model) {
			r, _ := m.Remove("2.000000")
			_ = m.View(anchorH, anchorW)
			m.Restore(r)
		},
		"taller above": func(m *Model) { m.Patch("2.000000", messages.Body{Text: "a\nb\nc\nd"}.Apply) },
		"settle above": func(m *Model) {
			m.Settle(messages.LocalTS(1), messages.MessageItem{TS: "2.500000", UserName: "me", Text: "mine"})
		},
	}
	for name, change := range changes {
		m := anchorThread(t)
		if name == "settle above" {
			m.InsertLocal(messages.MessageItem{TS: messages.LocalTS(1), UserName: "me", Text: "mine"})
			_ = m.View(anchorH, anchorW)
		}
		m.ScrollUp(30)
		before := threadFrame(m)
		if !frameHas(before, "r10") {
			t.Fatalf("setup: want r10 on screen:\n%s", strings.Join(before, "\n"))
		}
		change(m)
		if got := threadFrame(m); !slices.Equal(got, before) {
			t.Errorf("%s: the window moved\nbefore:\n%s\nafter:\n%s", name, strings.Join(before, "\n"), strings.Join(got, "\n"))
		}
	}
}

func selectedText(m *Model) string {
	msg, ok := m.SelectedMessage()
	if !ok {
		return ""
	}
	return msg.Text
}

// A thread at the bottom whose cursor the user moved up keeps that
// reply on screen when the last reply grows enough to push it out: the
// window scrolls back to the cursor rather than keeping the bottom.
func TestThreadBottomKeepsAMovedCursorOnScreen(t *testing.T) {
	m := anchorThread(t)
	for range 3 {
		m.MoveUp()
		_ = m.View(anchorH, anchorW)
	}
	if got := selectedText(m); got != "r17" {
		t.Fatalf("setup: selected %q, want r17", got)
	}
	m.Patch("20.000000", messages.Body{Text: "g1\ng2\ng3\ng4\ng5\ng6\ng7\ng8"}.Apply)
	f := threadFrame(m)
	if got := selectedText(m); got != "r17" || !frameHas(f, "r17") {
		t.Errorf("selected %q, want r17 selected and on screen after r20 grew:\n%s", got, strings.Join(f, "\n"))
	}
}

// A thread scrolled up with the cursor on screen scrolls back to it
// when a reply above it, on screen, grows and pushes it out.
func TestThreadScrolledUpKeepsTheCursorOnScreen(t *testing.T) {
	m := anchorThread(t)
	m.ScrollUp(30)
	_ = m.View(anchorH, anchorW)
	m.SelectByIndex(9) // r10
	before := threadFrame(m)
	if !frameHas(before, "r10") || !frameHas(before, "r8") {
		t.Fatalf("setup: want r8 and r10 on screen:\n%s", strings.Join(before, "\n"))
	}
	m.Patch("8.000000", messages.Body{Text: "g1\ng2\ng3\ng4\ng5\ng6\ng7\ng8\ng9\ng10\ng11\ng12\ng13\ng14\ng15\ng16"}.Apply)
	f := threadFrame(m)
	if got := selectedText(m); got != "r10" || !frameHas(f, "r10") {
		t.Errorf("selected %q, want r10 selected and on screen after r8 grew:\n%s", got, strings.Join(f, "\n"))
	}
}

// A new reply follows the cursor only when the cursor was at the end;
// the user's own reply is selected wherever the cursor was.
func TestNewReplyFollowsOnlyACursorAtTheEnd(t *testing.T) {
	m := anchorThread(t)
	m.AddReply(messages.MessageItem{TS: "21.000000", UserName: "u", Text: "r21"})
	if got := selectedText(m); got != "r21" {
		t.Errorf("cursor at the end: selected %q after r21 arrived, want r21", got)
	}

	m = anchorThread(t)
	for range 3 {
		m.MoveUp()
		_ = m.View(anchorH, anchorW)
	}
	before := threadFrame(m)
	m.AddReply(messages.MessageItem{TS: "21.000000", UserName: "u", Text: "r21"})
	if got := selectedText(m); got != "r17" {
		t.Errorf("cursor moved up: selected %q after r21 arrived, want r17 still", got)
	}
	if f := threadFrame(m); !frameHas(f, "r17") {
		t.Errorf("cursor moved up: r17 left the screen\nbefore:\n%s\nafter:\n%s", strings.Join(before, "\n"), strings.Join(f, "\n"))
	}

	m.InsertLocal(messages.MessageItem{TS: messages.LocalTS(1), UserName: "me", Text: "mine"})
	if got := selectedText(m); got != "mine" || !frameHas(threadFrame(m), "mine") {
		t.Errorf("own reply: selected %q, want the placeholder selected and shown", got)
	}
}
