package entrylist

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/gammons/slk/internal/bubbles/scrollbar"
)

// rows builds n plain rows "<id>-<k>" padded to width w.
func rows(id string, n, w int) []string {
	out := make([]string, n)
	for k := range out {
		s := fmt.Sprintf("%s-%d", id, k)
		out[k] = s + strings.Repeat(" ", max(w-len(s), 0))
	}
	return out
}

// msg is a selectable entry of h rows; trail of them excluded from snapping.
func msg(id string, ref, h, trail int) Entry {
	ls := rows(id, h, 20)
	return Entry{ID: id, Ref: ref, Selectable: true, Lines: ls,
		Plain: PlainLines(strings.Join(ls, "\n")), Trail: trail}
}

// deco is a one-row decoration; withPlain controls whether it can be
// text-selected (the channel pane's dividers can; thread's rules cannot).
func deco(label string, withPlain bool) Entry {
	ls := rows(label, 1, 20)
	e := Entry{Ref: -1, Lines: ls}
	if withPlain {
		e.Plain = PlainLines(ls[0])
	}
	return e
}

// list builds n messages of h rows each.
func list(t *testing.T, n, h int, opts ...Option) *Model {
	t.Helper()
	m := New(opts...)
	es := make([]Entry, n)
	for i := range es {
		es[i] = msg(fmt.Sprintf("m%02d", i), i, h, 0)
	}
	m.SetHeight(10, n-1)
	m.SetEntries(es, 20)
	return m
}

func strip(rs []string) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = strings.TrimRight(ansi.Strip(r), " "+scrollbar.TrackGlyph+scrollbar.ThumbGlyph)
	}
	return out
}

func TestOffsetsAndTotal(t *testing.T) {
	m := list(t, 3, 2)
	if got := m.Offsets(); fmt.Sprint(got) != "[0 2 4]" || m.Total() != 6 {
		t.Fatalf("offsets %v total %d", got, m.Total())
	}
	if i, ok := m.IndexOf("m01"); !ok || i != 1 {
		t.Fatalf("IndexOf(m01) = %d, %v", i, ok)
	}
}

func TestLayoutSnapsToCursorOnlyWhenItMoves(t *testing.T) {
	m := list(t, 20, 2)
	m.Layout(19, nil)
	if m.YOffset() != 30 {
		t.Fatalf("first layout did not snap to the last entry: y=%d", m.YOffset())
	}
	m.ScrollUp(5, 19)
	m.Layout(19, nil)
	if m.YOffset() != 25 {
		t.Fatalf("an unmoved cursor pulled the window back: y=%d", m.YOffset())
	}
	m.Layout(16, nil) // lines 32-33, inside [25, 35)
	if m.YOffset() != 25 {
		t.Fatalf("cursor 16 is visible at y=25, no snap needed: y=%d", m.YOffset())
	}
	m.Layout(18, nil) // lines 36-37, below the window
	if m.YOffset() != 28 {
		t.Fatalf("cursor 18 is below the window: y=%d, want 28", m.YOffset())
	}
	m.Layout(2, nil)
	if m.YOffset() != 4 {
		t.Fatalf("moving the cursor above the window did not snap: y=%d", m.YOffset())
	}
}

func TestTrailRowsAreNotSnappedIntoView(t *testing.T) {
	m := New()
	m.SetHeight(2, 0)
	m.SetEntries([]Entry{msg("a", 0, 3, 1), msg("b", 1, 3, 0)}, 20)
	// a is rows 0..2 but its last row is trail, so the target is [0,2),
	// which fits the 2-row window. Counting the trail row would make the
	// target [0,3) and scroll the window to y=1.
	m.Layout(0, nil)
	if s, e := m.CursorSpan(); s != 0 || e != 2 {
		t.Fatalf("span [%d,%d), want [0,2)", s, e)
	}
	if m.YOffset() != 0 {
		t.Fatalf("y=%d, want 0: the trail row was snapped into view", m.YOffset())
	}
	m.Layout(1, nil) // b is rows 3..5 and has no trail; taller than the window, so its top wins
	if m.YOffset() != 3 {
		t.Fatalf("y=%d, want 3", m.YOffset())
	}
	m.Layout(0, nil)
	if m.YOffset() != 0 {
		t.Fatalf("y=%d, want 0", m.YOffset())
	}
}

func TestLazyClampAbsorbsOvershoot(t *testing.T) {
	m := list(t, 20, 2)
	m.Layout(19, nil)
	m.ScrollDown(50, 19)
	m.ScrollUp(1, 19)
	m.Layout(19, nil)
	if m.YOffset() != 30 {
		t.Fatalf("lazy clamp: y=%d, want 30 (overshoot absorbs the ScrollUp)", m.YOffset())
	}
}

func TestEagerClampMakesOvershootANoOp(t *testing.T) {
	m := list(t, 20, 2, WithEagerClamp())
	m.Layout(19, nil)
	m.ScrollDown(50, 19)
	m.ScrollUp(1, 19)
	m.Layout(19, nil)
	if m.YOffset() != 29 {
		t.Fatalf("eager clamp: y=%d, want 29", m.YOffset())
	}
}

func TestEagerScrollWithoutContentIsANoOpThatBumpsVersion(t *testing.T) {
	m := New(WithEagerClamp())
	v := m.Version()
	m.ScrollUp(3, 0)
	m.ScrollDown(3, 0)
	if m.YOffset() != 0 || m.Version() != v+2 {
		t.Fatalf("y=%d version=%d, want 0 and %d", m.YOffset(), m.Version(), v+2)
	}
}

func TestCursorFollowsScroll(t *testing.T) {
	for _, follow := range []bool{false, true} {
		var opts []Option
		if follow {
			opts = append(opts, WithCursorFollowsScroll())
		}
		m := list(t, 20, 2, opts...)
		m.Layout(19, nil)
		m.ScrollUp(20, 19)
		f := m.Layout(19, nil)
		want := 19
		if follow {
			want = 9 // bottommost entry visible in lines [10, 20)
		}
		if f.Cursor != want {
			t.Errorf("follow=%v: cursor %d, want %d", follow, f.Cursor, want)
		}
	}
}

func TestPinBottomOnShrink(t *testing.T) {
	for _, pin := range []bool{false, true} {
		var opts []Option
		if pin {
			opts = append(opts, WithPinBottomOnShrink())
		}
		m := list(t, 20, 2, opts...)
		m.Layout(19, nil)
		m.SetHeight(7, 19)
		want := 30
		if pin {
			want = 33
		}
		if m.YOffset() != want {
			t.Errorf("pin=%v: y=%d, want %d", pin, m.YOffset(), want)
		}
	}
}

func TestReanchorKeepsTopEntryAcrossWidthChange(t *testing.T) {
	m := list(t, 20, 2)
	m.Layout(19, nil)
	m.ScrollUp(10, 19)
	m.Layout(19, nil) // y=20: entry 10, row 0
	es := make([]Entry, 20)
	for i := range es {
		es[i] = msg(fmt.Sprintf("m%02d", i), i, 3, 0) // rewrapped taller
	}
	m.SetHeight(10, 19)
	m.SetEntries(es, 15)
	if m.YOffset() != 30 {
		t.Fatalf("y=%d, want 30 (entry 10 at the top)", m.YOffset())
	}
}

func TestReanchorBottomUsesOldOrNewHeight(t *testing.T) {
	// 20 entries of 2 rows (40 lines), window 10, three lines up from
	// the bottom: y=27. The next frame is taller (14) and narrower.
	for _, useNew := range []bool{false, true} {
		m := list(t, 20, 2, WithReanchor(ReanchorPolicy{BottomUsesNewHeight: useNew}))
		m.Layout(19, nil)
		m.ScrollUp(3, 19)
		m.Layout(19, nil)
		m.SetHeight(14, 19)
		es := make([]Entry, 20)
		for i := range es {
			es[i] = msg(fmt.Sprintf("m%02d", i), i, 2, 0)
		}
		m.SetEntries(es, 15)
		atBottom := m.YOffset() == m.Total()-14
		if atBottom != useNew {
			t.Errorf("BottomUsesNewHeight=%v: pinned to bottom=%v (y=%d)", useNew, atBottom, m.YOffset())
		}
	}
}

func TestReanchorClampsTheRowInsideAShorterEntry(t *testing.T) {
	// y=3 is the last row of a 4-row parent that rewraps to 2 rows. The
	// parent stays the top entry, at its last row: y=1.
	m := New()
	m.SetHeight(3, 0)
	m.SetEntries([]Entry{msg("p", -1, 4, 0), msg("a", 0, 4, 0), msg("b", 1, 4, 0)}, 20)
	m.SetYOffset(3)
	m.SetEntries([]Entry{msg("p", -1, 2, 0), msg("a", 0, 4, 0), msg("b", 1, 4, 0)}, 15)
	if m.YOffset() != 1 {
		t.Errorf("y=%d, want 1", m.YOffset())
	}
}

func TestComposePadsAndPlacesIndicators(t *testing.T) {
	m := New()
	m.SetHeight(4, 0)
	m.SetEntries([]Entry{msg("a", 0, 2, 0)}, 20)
	got := strip(m.Compose(m.Layout(0, nil), "TOP", "BOTTOM", false))
	if len(got) != 4 || got[0] != "TOP" || got[3] != "" {
		t.Fatalf("short content: %q (no more-below, so no bottom row)", got)
	}
	m2 := list(t, 20, 2)
	f := m2.Layout(0, nil)
	got = strip(m2.Compose(f, "", "BOTTOM", false))
	if got[len(got)-1] != "BOTTOM" || got[0] != "m00-0" {
		t.Fatalf("overflowing content: %q", got)
	}
}

func TestComposeDrawsScrollbarOnlyWhenOverflowing(t *testing.T) {
	m := list(t, 20, 2)
	out := m.Compose(m.Layout(19, nil), "", "", false)
	if !strings.ContainsAny(ansi.Strip(out[0]), scrollbar.TrackGlyph+scrollbar.ThumbGlyph) {
		t.Fatalf("no scrollbar on an overflowing list: %q", ansi.Strip(out[0]))
	}
	for _, r := range out {
		if w := ansi.StringWidth(r); w != 20 {
			t.Fatalf("row width %d, want 20: %q", w, r)
		}
	}
}

func TestHitsAreContentRelativeAndClipped(t *testing.T) {
	m := New()
	m.SetHeight(3, 0)
	a := msg("a", 7, 5, 0) // 5 rows, window 3: y=2 is in range
	a.Hits = []Hit{{RowStart: 1, RowEnd: 3, ColStart: 2, ColEnd: 5, Kind: HitReaction, Key: "tada"}}
	m.SetEntries([]Entry{a}, 20)
	m.SetYOffset(2)
	m.MarkSnapped(7)
	m.Layout(7, nil)
	h, ok := m.HitTest(HitReaction, 0, 3)
	if !ok || h.Ref != 7 || h.Key != "tada" || h.RowStart != 0 || h.RowEnd != 1 {
		t.Fatalf("hit = %+v, %v; want row [0,1) ref 7", h, ok)
	}
	if _, ok := m.HitTest(HitImage, 0, 3); ok {
		t.Fatal("a reaction hit answered an image hit test")
	}
}

// Static translates the window's hits like Layout, and replaces the last
// frame's: a hit left over from an earlier frame would answer a click
// on content that is no longer there.
func TestStaticTranslatesHitsAndDropsTheLastFrames(t *testing.T) {
	m := New()
	m.SetHeight(5, 0)
	old := msg("old", 3, 4, 0)
	old.Hits = []Hit{{RowStart: 0, RowEnd: 4, ColStart: 0, ColEnd: 10, Kind: HitImage, Key: "F-OLD"}}
	m.SetEntries([]Entry{old}, 20)
	m.Layout(3, nil)

	a := msg("a", -1, 4, 0)
	a.Hits = []Hit{{RowStart: 2, RowEnd: 4, ColStart: 1, ColEnd: 4, Kind: HitImage, Key: "F-A", Aux: 1}}
	m.SetEntries([]Entry{a}, 20)
	m.Static()
	if h, ok := m.HitTest(HitImage, 2, 2); !ok || h.Key != "F-A" || h.Aux != 1 || h.Ref != -1 {
		t.Fatalf("hit = %+v, %v; want F-A attachment 1 on ref -1", h, ok)
	}
	if h, ok := m.HitTest(HitImage, 0, 6); ok {
		t.Fatalf("the last frame's hit survived Static: %+v", h)
	}
}

func TestSelectionTextAndPlainlessEntries(t *testing.T) {
	m := New()
	m.SetHeight(10, 0)
	m.SetEntries([]Entry{msg("a", 0, 2, 0), deco("rule", false), msg("b", 1, 2, 0)}, 20)
	m.Layout(1, nil)
	m.BeginSelection(0, 2) // a-0, col 2
	m.ExtendSelection(4, 3)
	text, ok := m.EndSelection()
	// Row a-0 from column 2 is "0"; a-1 whole; the rule has no Plain and
	// is skipped; b-0 whole; b-1 up to column 3 is "b-1".
	if !ok || text != "0\na-1\nb-0\nb-1" {
		t.Fatalf("text %q, ok %v", text, ok)
	}
}

func TestPressOnDecoration(t *testing.T) {
	for _, snap := range []bool{false, true} {
		var opts []Option
		if snap {
			opts = append(opts, WithSnapDecorationAnchors())
		}
		// A decoration with Plain can be pressed: it anchors on itself
		// (empty ID), or snaps to the next selectable entry.
		m := New(opts...)
		m.SetHeight(10, 0)
		m.SetEntries([]Entry{deco("day", true), msg("a", 0, 2, 0)}, 20)
		m.Layout(0, nil)
		m.BeginSelection(0, 3)
		if !m.HasSelection() {
			t.Errorf("snap=%v: a press on a decoration with Plain anchored nothing", snap)
		}
		wantID := ""
		if snap {
			wantID = "a"
		}
		if got := m.Selection().Start.MessageID; got != wantID {
			t.Errorf("snap=%v: anchored on %q, want %q", snap, got, wantID)
		}

		// A decoration without Plain (the thread pane's rules) anchors
		// nothing in either setting.
		m = New(opts...)
		m.SetHeight(10, 0)
		m.SetEntries([]Entry{deco("rule", false), msg("a", 0, 2, 0)}, 20)
		m.Layout(0, nil)
		m.BeginSelection(0, 3)
		if m.HasSelection() {
			t.Errorf("snap=%v: a press on a Plain-less decoration started a selection", snap)
		}
	}
}

func TestDirtyOnDrag(t *testing.T) {
	for _, dirty := range []bool{false, true} {
		var opts []Option
		if dirty {
			opts = append(opts, WithDirtyOnDrag())
		}
		m := New(opts...)
		m.SetHeight(10, 0)
		m.SetEntries([]Entry{msg("a", 0, 2, 0)}, 20)
		m.Layout(0, nil)
		v0 := m.Version()
		m.BeginSelection(0, 1)
		m.ExtendSelection(0, 4)
		m.ExtendSelection(0, 4)
		m.EndSelection()
		want := v0 + 1
		if dirty {
			want = v0 + 3 // begin, one real extend, end
		}
		if m.Version() != want {
			t.Errorf("dirty=%v: version %d, want %d", dirty, m.Version(), want)
		}
	}
}

func TestApplySelectionToBorderedSkipsIndicatorRows(t *testing.T) {
	m := list(t, 20, 2)
	f := m.Layout(0, nil)
	_ = m.Compose(f, "", "MORE", false)
	m.BeginSelection(0, 0)
	m.ExtendSelection(9, 4)
	bordered := strings.Join(rows("x", 10, 20), "\n")
	got := strings.Split(m.ApplySelectionToBordered(bordered, 0, 0), "\n")
	if got[9] != rows("x", 10, 20)[9] {
		t.Fatalf("the more-below row was painted: %q", got[9])
	}
	if got[0] == rows("x", 10, 20)[0] {
		t.Fatal("a selected row was not painted")
	}
}

func TestAbsLineAndEntryAt(t *testing.T) {
	m := list(t, 3, 2)
	if m.AbsLine(-5) != 0 || m.AbsLine(99) != 5 {
		t.Fatalf("AbsLine clamp: %d %d", m.AbsLine(-5), m.AbsLine(99))
	}
	if i, w, ok := m.EntryAt(3); !ok || i != 1 || w != 1 {
		t.Fatalf("EntryAt(3) = %d %d %v", i, w, ok)
	}
}

func TestKeepLookupsHidesDiscardedEntriesButKeepsExtent(t *testing.T) {
	m := list(t, 3, 2)
	m.Layout(2, nil)
	m.KeepLookups(1)
	if _, _, ok := m.EntryAt(3); ok {
		t.Fatal("a discarded entry is still found")
	}
	if _, _, ok := m.EntryAt(1); !ok {
		t.Fatal("a kept entry is no longer found")
	}
	if m.Total() != 6 {
		t.Fatalf("total changed: %d", m.Total())
	}
	m.BeginSelection(3, 0)
	if m.HasSelection() {
		t.Fatal("a press on a discarded entry started a selection")
	}
	m.SetEntries(m.Entries(), 20)
	if _, _, ok := m.EntryAt(3); !ok {
		t.Fatal("SetEntries did not restore lookups")
	}
}

// ReplaceEntries swaps several entries in one pass (a partial rebuild):
// heights may change, offsets and the total follow, other entries are
// untouched, a window at the bottom stays there, and the ID index
// tracks a changed ID.
func TestReplaceEntries(t *testing.T) {
	m := list(t, 4, 2) // offsets 0 2 4 6, total 8
	m.Layout(3, nil)
	y := m.YOffset()
	other := m.Entries()[2]
	m.ReplaceEntries(map[int]Entry{
		0: msg("m00", 0, 3, 0), // taller
		3: msg("z03", 3, 1, 0), // shorter, new ID
	})
	if got := fmt.Sprint(m.Offsets()); got != "[0 3 5 7]" || m.Total() != 8 {
		t.Fatalf("offsets %s total %d, want [0 3 5 7] 8", got, m.Total())
	}
	if e := m.Entries()[2]; e.ID != other.ID || len(e.Lines) != len(other.Lines) {
		t.Fatalf("an entry outside the batch changed: %+v", e)
	}
	if i, ok := m.IndexOf("z03"); !ok || i != 3 {
		t.Fatalf("IndexOf(z03) = %d, %v", i, ok)
	}
	if _, ok := m.IndexOf("m03"); ok {
		t.Fatal("the replaced ID is still indexed")
	}
	if m.YOffset() != y {
		t.Fatalf("a partial rebuild moved a window at the bottom: %d -> %d", y, m.YOffset())
	}
}

// renumber sets each entry's Ref to its index, as an owner's indexes
// follow its rows.
func renumber(es []Entry) []Entry {
	for i := range es {
		if es[i].Ref >= 0 {
			es[i].Ref = i
		}
	}
	return es
}

// frameRows lays out a frame for the cursor and returns its rows,
// stripped of styling and the scrollbar.
func frameRows(m *Model, cursor int) []string {
	return strip(m.Compose(m.Layout(cursor, nil), "", "", false))
}

// refOf is the Ref of the entry with id, for a cursor that stays on its
// entry as the owner's indexes shift.
func refOf(t *testing.T, m *Model, id string) int {
	t.Helper()
	i, ok := m.IndexOf(id)
	if !ok {
		t.Fatalf("no entry %q", id)
	}
	return m.Entries()[i].Ref
}

// An entry inserted before the selected last entry, and selected (the
// newest message arriving ahead of a pending send), shows with the
// entry after it: a window at the bottom stays at the bottom, and the
// cursor moving to a different entry at the same index scrolls to it.
func TestEntryInsertedBeforeTheSelectedLastShowsWithIt(t *testing.T) {
	for _, opts := range [][]Option{nil, {WithEagerClamp()}, {WithCursorFollowsScroll(), WithPinBottomOnShrink()}} {
		m := list(t, 20, 2, opts...)
		m.Layout(19, nil) // the bottom: m15..m19
		es := slices.Clone(m.Entries())
		es = renumber(slices.Insert(es, 19, msg("new", 0, 3, 0)))
		m.SetEntries(es, 20)
		got := frameRows(m, 19) // the cursor is on "new" now
		want := []string{"m17-0", "m17-1", "m18-0", "m18-1", "new-0", "new-1", "new-2", "m19-0", "m19-1"}
		if !slices.Equal(got[1:], want) {
			t.Errorf("frame %q, want it to end %q", got, want)
		}
	}
}

// A window at the bottom stays there whatever changes: rows inserted
// before the last, the last growing or shrinking, rows removed.
func TestAnchorKeepsTheBottom(t *testing.T) {
	changes := map[string]func([]Entry) []Entry{
		"last taller":   func(es []Entry) []Entry { es[19] = msg("m19", 19, 6, 0); return es },
		"last shorter":  func(es []Entry) []Entry { es[19] = msg("m19", 19, 1, 0); return es },
		"insert before": func(es []Entry) []Entry { return renumber(slices.Insert(es, 18, msg("new", 0, 4, 0))) },
		"remove above":  func(es []Entry) []Entry { return renumber(slices.Delete(es, 2, 3)) },
		"remove inside": func(es []Entry) []Entry { return renumber(slices.Delete(es, 17, 18)) },
	}
	for name, change := range changes {
		for _, eager := range []bool{false, true} {
			var opts []Option
			if eager {
				opts = append(opts, WithEagerClamp())
			}
			m := list(t, 20, 2, opts...)
			m.Layout(19, nil)
			m.SetEntries(change(slices.Clone(m.Entries())), 20)
			got := frameRows(m, refOf(t, m, "m19"))
			last := m.Entries()[len(m.Entries())-1]
			if tail := strip(last.Lines); !slices.Equal(got[len(got)-len(tail):], tail) {
				t.Errorf("%s (eager %v): frame %q does not end with the last entry", name, eager, got)
			}
		}
	}
}

// A window scrolled up keeps its first visible entry on the same screen
// row when rows above or below it change, and the frame is unchanged.
func TestAnchorKeepsTheTopEntryRow(t *testing.T) {
	changes := map[string]func([]Entry) []Entry{
		"insert above":  func(es []Entry) []Entry { return renumber(slices.Insert(es, 2, msg("new", 0, 3, 0))) },
		"remove above":  func(es []Entry) []Entry { return renumber(slices.Delete(es, 2, 3)) },
		"taller above":  func(es []Entry) []Entry { es[2] = msg("m02", 2, 5, 0); return es },
		"shorter above": func(es []Entry) []Entry { es[2] = msg("m02", 2, 1, 0); return es },
		"insert below":  func(es []Entry) []Entry { return renumber(slices.Insert(es, 19, msg("new", 0, 3, 0))) },
		"taller below":  func(es []Entry) []Entry { es[19] = msg("m19", 19, 5, 0); return es },
	}
	for name, change := range changes {
		for _, follow := range []bool{false, true} {
			var opts []Option
			if follow {
				opts = append(opts, WithCursorFollowsScroll())
			}
			m := list(t, 20, 2, opts...)
			m.Layout(19, nil)
			m.ScrollUp(15, 19) // y=15: m07's second row on top
			f := m.Layout(19, nil)
			cursorID := m.Entries()[f.Cursor].ID
			before := frameRows(m, f.Cursor)
			m.SetEntries(change(slices.Clone(m.Entries())), 20)
			if got := frameRows(m, refOf(t, m, cursorID)); !slices.Equal(got, before) {
				t.Errorf("%s (follow %v): frame moved\nbefore %q\nafter  %q", name, follow, before, got)
			}
		}
	}
}

// A partial rebuild that changes a height above the window keeps the
// window's content where it was.
func TestReplaceEntriesKeepsTheTopEntryRow(t *testing.T) {
	m := list(t, 20, 2)
	m.Layout(19, nil)
	m.ScrollUp(15, 19)
	before := frameRows(m, 19)
	m.ReplaceEntries(map[int]Entry{2: msg("m02", 2, 6, 0)})
	if got := frameRows(m, 19); !slices.Equal(got, before) {
		t.Errorf("frame moved\nbefore %q\nafter  %q", before, got)
	}
}
