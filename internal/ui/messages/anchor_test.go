package messages

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/gammons/slk/internal/bubbles/scrollbar"
)

// The pane keeps what the user sees in place when its rows change: a
// pane at the bottom stays at the bottom, and a pane scrolled up keeps
// its content where it is when rows change above it. These tests read
// the rendered frame, not indexes.

const anchorH, anchorW = 14, 60

// anchorPane is a channel of twenty one-line messages "m1".."m20",
// rendered once at the bottom.
func anchorPane(t *testing.T) *Model {
	t.Helper()
	var msgs []MessageItem
	for i := 1; i <= 20; i++ {
		msgs = append(msgs, MessageItem{TS: fmt.Sprintf("%d.000000", i), UserName: "u", Text: fmt.Sprintf("m%d", i)})
	}
	m := New(msgs, "general")
	_ = m.View(anchorH, anchorW)
	return m
}

// paneFrame renders the pane and returns its content rows (no header),
// stripped of styling, the selection border and the scrollbar.
func paneFrame(m *Model) []string {
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

// A colleague's message that arrives while the user's own message is
// sending lands before the placeholder and is selected. Both show
// whole, and the user's message stays on screen once it settles,
// whether it stays last or moves before the colleague's.
func TestArrivalAheadOfAPendingSendShowsBoth(t *testing.T) {
	for _, sentTS := range []string{"22.000000", "20.500000"} {
		m := anchorPane(t)
		m.InsertLocal(MessageItem{TS: LocalTS(1), UserName: "me", Text: "my-pending-text"})
		_ = m.View(anchorH, anchorW)
		m.AppendMessage(MessageItem{TS: "21.000000", UserName: "colleague", Text: "colleague-text"})
		f := paneFrame(m)
		if !frameHas(f, "colleague-text") || !frameHas(f, "my-pending-text") || frameHas(f, "more below") {
			t.Fatalf("after the arrival, want both messages whole on screen:\n%s", strings.Join(f, "\n"))
		}
		m.Settle(LocalTS(1), MessageItem{TS: sentTS, UserName: "me", Text: "my-sent-text"})
		f = paneFrame(m)
		if !frameHas(f, "colleague-text") || !frameHas(f, "my-sent-text") || frameHas(f, "more below") {
			t.Errorf("settled at %s, want both messages whole on screen:\n%s", sentTS, strings.Join(f, "\n"))
		}
	}
}

// A pane at the bottom stays there when the last message grows or
// shrinks.
func TestBottomStaysWhenTheLastMessageChangesHeight(t *testing.T) {
	for _, text := range []string{"one\ntwo\nthree\nfour\nlast-line", "last-line"} {
		m := anchorPane(t)
		m.Patch("20.000000", Body{Text: "x\ny\nz"}.Apply)
		_ = m.View(anchorH, anchorW)
		m.Patch("20.000000", Body{Text: text}.Apply)
		f := paneFrame(m)
		if !strings.Contains(f[len(f)-1], "last-line") {
			t.Errorf("the last row is %q, want the last message's last line:\n%s", f[len(f)-1], strings.Join(f, "\n"))
		}
	}
}

// A pane scrolled up shows the same rows after a message is added,
// removed, restored, resized or settled above the window.
func TestRowsAboveTheWindowDoNotMoveIt(t *testing.T) {
	changes := map[string]func(m *Model){
		"insert above": func(m *Model) { m.AppendMessage(MessageItem{TS: "2.500000", UserName: "u", Text: "late"}) },
		"remove above": func(m *Model) { m.Remove("2.000000") },
		"restore above": func(m *Model) {
			r, _ := m.Remove("2.000000")
			_ = m.View(anchorH, anchorW)
			m.Restore(r)
		},
		"taller above": func(m *Model) { m.Patch("2.000000", Body{Text: "a\nb\nc\nd"}.Apply) },
		"settle above": func(m *Model) {
			m.Settle(LocalTS(1), MessageItem{TS: "2.500000", UserName: "me", Text: "mine"})
		},
	}
	for name, change := range changes {
		m := anchorPane(t)
		if name == "settle above" {
			m.InsertLocal(MessageItem{TS: LocalTS(1), UserName: "me", Text: "mine"})
			_ = m.View(anchorH, anchorW)
		}
		m.ScrollUp(24)
		before := paneFrame(m)
		if !frameHas(before, "m10") || frameHas(before, "m3") {
			t.Fatalf("setup: want m10 on screen and m3 above it:\n%s", strings.Join(before, "\n"))
		}
		change(m)
		if got := paneFrame(m); !slices.Equal(got, before) {
			t.Errorf("%s: the window moved\nbefore:\n%s\nafter:\n%s", name, strings.Join(before, "\n"), strings.Join(got, "\n"))
		}
	}
}
