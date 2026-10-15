package entrylist

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"pgregory.net/rapid"

	"github.com/gammons/slk/internal/proptest"
)

// listGen builds entries for the state machine, with IDs that are
// unique across the whole run so ReplaceEntries never collides.
type listGen struct {
	nextID, nextRef int
	width           int
}

// line is one rendered row exactly width columns wide, styled, with no
// base+VS16 pair (x/ansi measures it as 2 columns but cuts it at 1;
// see the scrollbar tests).
func (g *listGen) line(t *rapid.T) string {
	s := proptest.StyledText().Draw(t, "line")
	s = proptest.NoVS16(strings.ReplaceAll(s, "\n", " "))
	s = ansi.Truncate(s, g.width, "")
	if w := ansi.StringWidth(s); w < g.width {
		s += strings.Repeat(" ", g.width-w)
	}
	return s
}

func (g *listGen) entry(t *rapid.T, selectable bool) Entry {
	n := rapid.IntRange(1, 4).Draw(t, "lines")
	e := Entry{Ref: -1, Selectable: selectable}
	if selectable {
		g.nextID++
		g.nextRef++
		e.ID, e.Ref = fmt.Sprintf("%d.%06d", 1_700_000_000+g.nextID, 0), g.nextRef
	} else if rapid.Bool().Draw(t, "decoIDed") {
		g.nextID++
		e.ID = fmt.Sprintf("d%d", g.nextID)
	}
	for range n {
		l := g.line(t)
		e.Lines = append(e.Lines, l)
		e.Plain = append(e.Plain, PlainLines(l)[0])
	}
	if selectable && rapid.IntRange(0, 4).Draw(t, "trail") == 0 && n > 1 {
		e.Trail = 1
	}
	return e
}

func (g *listGen) entries(t *rapid.T) []Entry {
	n := rapid.IntRange(0, 8).Draw(t, "entries")
	es := make([]Entry, n)
	for i := range es {
		es[i] = g.entry(t, rapid.IntRange(0, 3).Draw(t, "kind") != 0)
	}
	return es
}

// refSelectionText is the spec of SelectionText: the normalized range
// resolved to absolute lines and columns, each covered row's
// SliceColumns slice trimmed of trailing spaces, joined by newlines.
func refSelectionText(m *Model, live int) string {
	if !m.HasSelection() {
		return ""
	}
	lo, hi := m.Selection().Normalize()
	if lo == hi {
		return ""
	}
	loLine, loCol, ok1 := m.Resolve(lo)
	hiLine, hiCol, ok2 := m.Resolve(hi)
	if !ok1 || !ok2 || loLine > hiLine || (loLine == hiLine && loCol >= hiCol) {
		return ""
	}
	var out []string
	for abs := loLine; abs <= hiLine; abs++ {
		for i, e := range m.Entries() {
			if i >= live {
				break
			}
			j := abs - m.Offsets()[i]
			if j < 0 || j >= len(e.Plain) {
				continue
			}
			from, to := 0, DisplayWidth(e.Plain[j])
			if abs == loLine {
				from = loCol
			}
			if abs == hiLine {
				to = hiCol
			}
			out = append(out, strings.TrimRight(SliceColumns(e.Plain[j], from, to), " "))
		}
	}
	return strings.TrimRight(strings.Join(out, "\n"), "\n")
}

// The list as a state machine over the documented operations and every
// option. After each step: Offsets are the prefix sums of line counts,
// IDs resolve to their index, Version never decreases, SelectionText is
// the reference text, anchors that no longer resolve give "" rather
// than a panic. After Layout: the offset is inside [0, max(0,
// total-height)], the frame has min(height, total-offset) rows with
// consistent Visible spans, Compose fills the window with rows exactly
// width wide, and a forced snap puts a cursor span that fits on
// screen.
func TestListStateMachine(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		var opts []Option
		eager := rapid.Bool().Draw(t, "eager")
		reanchor := ReanchorPolicy{
			BottomUsesNewHeight: rapid.Bool().Draw(t, "bottomNew"),
			AnchorRefsOnly:      rapid.Bool().Draw(t, "refsOnly"),
		}
		opts = append(opts, WithReanchor(reanchor))
		if eager {
			opts = append(opts, WithEagerClamp())
		}
		if rapid.Bool().Draw(t, "follow") {
			opts = append(opts, WithCursorFollowsScroll())
		}
		if rapid.Bool().Draw(t, "pin") {
			opts = append(opts, WithPinBottomOnShrink())
		}
		if rapid.Bool().Draw(t, "snapDecor") {
			opts = append(opts, WithSnapDecorationAnchors())
		}
		if rapid.Bool().Draw(t, "dirty") {
			opts = append(opts, WithDirtyOnDrag())
		}
		m := New(opts...)
		g := &listGen{width: rapid.IntRange(4, 20).Draw(t, "width")}
		m.SetHeight(rapid.IntRange(1, 12).Draw(t, "h0"), 0)
		m.SetEntries(g.entries(t), g.width, false)
		live := len(m.Entries())
		var version int64
		cursor := 0

		check := func() {
			sum := 0
			for i, e := range m.Entries() {
				if m.Offsets()[i] != sum {
					t.Fatalf("offset[%d]=%d, want %d", i, m.Offsets()[i], sum)
				}
				sum += len(e.Lines)
				if e.ID != "" {
					if idx, ok := m.IndexOf(e.ID); !ok || idx != i {
						t.Fatalf("IndexOf(%q)=%d,%v want %d", e.ID, idx, ok, i)
					}
				}
			}
			if len(m.Offsets()) != len(m.Entries()) || m.Total() != sum {
				t.Fatalf("Total %d, offsets %d for %d entries, want sum %d", m.Total(), len(m.Offsets()), len(m.Entries()), sum)
			}
			if m.Version() < version {
				t.Fatalf("Version went back: %d -> %d", version, m.Version())
			}
			version = m.Version()
			if want, got := refSelectionText(m, live), m.SelectionText(); got != want {
				t.Fatalf("SelectionText %q, reference %q", got, want)
			}
			if m.Total() > 0 {
				if a := m.AbsLine(rapid.IntRange(-5, 40).Draw(t, "contentY")); a < 0 || a >= m.Total() {
					t.Fatalf("AbsLine = %d outside [0,%d)", a, m.Total())
				}
			}
		}

		layout := func(forceSnap bool) {
			if forceSnap {
				m.ResetSnap()
			}
			asked := cursor
			f := m.Layout(cursor, nil)
			h, y := m.Height(), m.YOffset()
			if y < 0 || y > max(0, m.Total()-h) {
				t.Fatalf("YOffset %d outside [0,%d] (total %d, height %d)", y, max(0, m.Total()-h), m.Total(), h)
			}
			if want := max(0, min(h, m.Total()-y)); len(f.Rows) != want {
				t.Fatalf("%d rows, want %d (y %d, total %d, h %d)", len(f.Rows), want, y, m.Total(), h)
			}
			if f.MoreBelow != (y+h < m.Total()) {
				t.Fatalf("MoreBelow %v at y %d total %d h %d", f.MoreBelow, y, m.Total(), h)
			}
			got := 0
			prevIdx := -1
			for _, v := range f.Visible {
				e := m.Entries()[v.Index]
				if v.Index <= prevIdx || v.From < 0 || v.From >= v.To || v.To > len(e.Lines) || v.Row != got {
					t.Fatalf("bad Visible %+v after %d (rows so far %d)", v, prevIdx, got)
				}
				prevIdx = v.Index
				got += v.To - v.From
				if abs := m.Offsets()[v.Index] + v.From; abs != y+v.Row {
					t.Fatalf("Visible %+v starts at line %d, window row %d is line %d", v, abs, v.Row, y+v.Row)
				}
			}
			if got != len(f.Rows) {
				t.Fatalf("Visible covers %d rows of %d", got, len(f.Rows))
			}
			cursor = f.Cursor
			// (A cursor that followed the scroll is on a partly visible entry.)
			if forceSnap && f.Cursor == asked {
				s, e := m.CursorSpan()
				if e-s > 0 && e-s <= h && (s < y || e > y+h) {
					t.Fatalf("cursor span [%d,%d) not in window [%d,%d)", s, e, y, y+h)
				}
			}
			rows := m.Compose(f, "", "", true)
			if len(rows) != max(h, len(f.Rows)) {
				t.Fatalf("Compose gave %d rows, height %d", len(rows), h)
			}
			for i, r := range rows {
				if w := ansi.StringWidth(r); w != g.width {
					t.Fatalf("composed row %d width %d, want %d: %q", i, w, g.width, r)
				}
			}
			check()
		}

		t.Repeat(map[string]func(*rapid.T){
			"": func(t *rapid.T) { check() },
			"setHeight": func(t *rapid.T) {
				m.SetHeight(rapid.IntRange(1, 14).Draw(t, "h"), cursor)
				check()
			},
			"setEntries": func(t *rapid.T) {
				w := g.width
				if rapid.Bool().Draw(t, "resize") {
					w = rapid.IntRange(4, 20).Draw(t, "newWidth")
				}
				keep := rapid.Bool().Draw(t, "keep")
				wasBottom := m.Total() > 0 && m.YOffset()+m.Height() >= m.Total()
				resized := w != g.width
				g.width = w
				m.SetEntries(g.entries(t), w, keep)
				live = len(m.Entries())
				if eager {
					if y := m.YOffset(); y < 0 || y > max(0, m.Total()-m.Height()) {
						t.Fatalf("eager YOffset %d after SetEntries (total %d, h %d)", y, m.Total(), m.Height())
					}
				}
				check()
				if w != m.Width() {
					t.Fatalf("Width %d, want %d", m.Width(), w)
				}
				// A window at the bottom stays there across a rewrap (the
				// offset may overshoot until Layout clamps it).
				if wasBottom && keep && resized && reanchor.BottomUsesNewHeight {
					if want := max(0, m.Total()-m.Height()); m.YOffset() < want {
						t.Fatalf("bottom-anchored window left the bottom: y %d, bottom %d", m.YOffset(), want)
					}
				}
			},
			"replace": func(t *rapid.T) {
				if len(m.Entries()) == 0 {
					return
				}
				updates := map[int]Entry{}
				for range rapid.IntRange(1, 3).Draw(t, "n") {
					i := rapid.IntRange(0, len(m.Entries())-1).Draw(t, "idx")
					old := m.Entries()[i]
					e := g.entry(t, old.Selectable)
					if rapid.Bool().Draw(t, "keepID") {
						e.ID, e.Ref = old.ID, old.Ref
					}
					updates[i] = e
				}
				m.ReplaceEntries(updates)
				check()
			},
			"scrollUp": func(t *rapid.T) {
				m.ScrollUp(rapid.IntRange(-1, 6).Draw(t, "n"), cursor)
				check()
			},
			"scrollDown": func(t *rapid.T) {
				m.ScrollDown(rapid.IntRange(-1, 6).Draw(t, "n"), cursor)
				check()
			},
			"setY": func(t *rapid.T) {
				m.SetYOffset(rapid.IntRange(-3, 40).Draw(t, "y"))
				if eager {
					if y := m.YOffset(); y < 0 || y > max(0, m.Total()-m.Height()) {
						t.Fatalf("eager YOffset %d out of range", y)
					}
				}
			},
			"layout": func(t *rapid.T) {
				c := cursor
				if rapid.Bool().Draw(t, "moveCursor") {
					c = rapid.IntRange(-1, g.nextRef+1).Draw(t, "cursor")
				}
				cursor = c
				layout(false)
			},
			"layoutSnap": func(t *rapid.T) { layout(true) },
			"keepLookups": func(t *rapid.T) {
				live = rapid.IntRange(-1, len(m.Entries())+1).Draw(t, "live")
				m.KeepLookups(live)
				live = max(live, 0)
				if live > len(m.Entries()) {
					live = len(m.Entries())
				}
				check()
			},
			"begin": func(t *rapid.T) {
				m.BeginSelection(rapid.IntRange(-2, 40).Draw(t, "abs"), rapid.IntRange(-2, 25).Draw(t, "col"))
				check()
			},
			"extend": func(t *rapid.T) {
				m.ExtendSelection(rapid.IntRange(-2, 40).Draw(t, "abs"), rapid.IntRange(-2, 25).Draw(t, "col"))
				check()
			},
			"end": func(t *rapid.T) {
				want := m.SelectionText()
				had := m.HasSelection()
				text, ok := m.EndSelection()
				if ok != (text != "") {
					t.Fatalf("EndSelection = %q, %v", text, ok)
				}
				if had && ok && text != want {
					t.Fatalf("EndSelection %q != SelectionText %q", text, want)
				}
				if m.HasSelection() && m.Selection().Active {
					t.Fatal("selection still active after EndSelection")
				}
				check()
			},
			"clear": func(t *rapid.T) {
				m.ClearSelection()
				if m.HasSelection() || m.SelectionText() != "" {
					t.Fatal("selection survived ClearSelection")
				}
				check()
			},
		})
	})
}
