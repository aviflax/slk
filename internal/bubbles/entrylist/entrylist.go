// Package entrylist is a scrollable, selectable list of pre-rendered
// entries: the substrate the channel and thread panes share.
//
// It owns line offsets, the scroll offset, the visible window, text
// selection, hit-rectangle translation and the scrollbar. It knows
// nothing about messages: an owner renders each item to an Entry and
// keeps everything domain-specific (chrome, empty states, what an entry
// means) to itself.
//
// Every behaviour the two panes do differently is an Option, set by each
// owner so that it behaves exactly as it did before this package existed.
// The options are the executable form of the divergence list that
// internal/ui/thread/lockstep_test.go used to carry.
package entrylist

import (
	"image/color"

	"charm.land/lipgloss/v2"

	"github.com/gammons/slk/internal/bubbles/selection"
)

// Entry is one pre-rendered item: a message, or a decoration row such as
// a day divider.
type Entry struct {
	// ID is the stable key a text selection anchors to (a Slack ts).
	// "" marks a decoration.
	ID string
	// Ref is the owner's index for the entry (a message or reply index),
	// or -1. Hit tests and cursor moves report it.
	Ref int
	// Selectable entries can hold the cursor. Snapping, ClickAt-style
	// lookups and cursor-follows-scroll consider only these.
	Selectable bool
	// Lines are the rendered rows, each exactly the list width.
	Lines []string
	// Selected is the cursor-on variant of Lines, the same length. nil
	// means draw Lines.
	Selected []string
	// Plain mirrors the rows a text selection can cover. Rows at or past
	// len(Plain) (every row, when Plain is nil) are never selected.
	Plain []PlainLine
	// ColOffset is the number of display columns of entry chrome (the
	// "▌" border) before the content Plain mirrors.
	ColOffset int
	// Trail is the number of trailing rows that belong to the entry but
	// are not scrolled into view when the cursor lands on it (the channel
	// pane's spacer row).
	Trail int
	// Hits are clickable rectangles, relative to Lines.
	Hits []Hit
}

// HitKind says what a clickable rectangle is.
type HitKind uint8

const (
	// HitReaction is a reaction pill; Key is the emoji name.
	HitReaction HitKind = iota + 1
	// HitImage is an inline image; Key is the file ID, Aux the
	// attachment index.
	HitImage
)

// Hit is a clickable rectangle relative to an Entry's Lines.
type Hit struct {
	RowStart, RowEnd int // [RowStart, RowEnd)
	ColStart, ColEnd int // [ColStart, ColEnd), display columns
	Kind             HitKind
	Key              string
	Aux              int
}

// FrameHit is a Hit translated into the last frame's window: Row 0 is
// the first row of content under the owner's chrome. Ref is the owning
// entry's Ref.
type FrameHit struct {
	RowStart, RowEnd int
	ColStart, ColEnd int
	Kind             HitKind
	Key              string
	Aux              int
	Ref              int
}

// Styles are the colours the list draws with itself; entries carry their
// own styling.
type Styles struct {
	// Background fills padding rows and the scrollbar gutter.
	Background color.Color
	// Track and Thumb colour the scrollbar.
	Track, Thumb color.Color
	// Selection highlights text-selected columns.
	Selection lipgloss.Style
}

// DefaultStyles returns usable styles for a dark background. Owners with
// a theme build Styles directly.
func DefaultStyles() Styles {
	return Styles{
		Background: lipgloss.Color("#1A1A2E"),
		Track:      lipgloss.Color("#3A3A5C"),
		Thumb:      lipgloss.Color("#4A9EFF"),
		Selection:  lipgloss.NewStyle().Reverse(true),
	}
}

// ReanchorPolicy says how "the window shows the end of the list" is
// measured when the height changed since the last frame (see
// SetEntries).
type ReanchorPolicy struct {
	// BottomUsesNewHeight measures "was at the bottom" with this frame's
	// height instead of the previous frame's.
	BottomUsesNewHeight bool
}

// Option configures a Model.
type Option func(*Model)

// WithStyles sets the initial styles.
func WithStyles(s Styles) Option { return func(m *Model) { m.styles = s } }

// WithCursorFollowsScroll moves the cursor onto the window when a scroll
// leaves it off-screen (the channel pane).
func WithCursorFollowsScroll() Option { return func(m *Model) { m.follow = true } }

// WithPinBottomOnShrink keeps a bottom-anchored window at the bottom when
// SetHeight shrinks it (the channel pane).
func WithPinBottomOnShrink() Option { return func(m *Model) { m.pinBottom = true } }

// WithSnapDecorationAnchors lets a selection that starts or extends on a
// decoration snap to the nearest selectable entry (the channel pane).
// Without it such a press anchors nothing.
func WithSnapDecorationAnchors() Option { return func(m *Model) { m.snapDecor = true } }

// WithDirtyOnDrag bumps Version on every selection change during a drag
// (the thread pane). Without it only the first press and an empty
// release do, because the channel pane repaints the selection as a
// post-pass (ApplySelectionToBordered).
func WithDirtyOnDrag() Option { return func(m *Model) { m.dirtyOnDrag = true } }

// WithEagerClamp clamps the offset on every assignment and makes a scroll
// past either end a no-op (the thread pane, which used bubbles/viewport).
// Without it the offset is clamped once per frame, in Layout.
func WithEagerClamp() Option { return func(m *Model) { m.eager = true } }

// WithBottomOverlay says the owner covers the window's last row with an
// overlay (Compose's bottom) whenever there is content below it (the
// channel pane's "-- more below --"). A cursor snapped into view from
// below then ends above that row, and a cursor on it does not count as
// shown. Without it the window's last row is content.
func WithBottomOverlay() Option { return func(m *Model) { m.overlay = true } }

// WithReanchor sets how the bottom is measured across a height change.
func WithReanchor(p ReanchorPolicy) Option { return func(m *Model) { m.reanchor = p } }

// Model is the list. The zero value is not usable; call New.
type Model struct {
	styles                                           Styles
	follow, pinBottom, snapDecor, dirtyOnDrag, eager bool
	overlay                                          bool // WithBottomOverlay
	reanchor                                         ReanchorPolicy

	entries []Entry
	live    int // entries lookups may see; -1 = all (see KeepLookups)
	offsets []int
	total   int
	ids     map[string]int
	width   int

	height           int
	laidHeight       int // the height of the last frame laid out; 0 before one
	yOffset          int
	snapped          int
	snapID           string // the ID of the entry snapped, "" when unknown
	hasSnapped       bool
	snapLost         bool // the snapped entry left in SetEntries
	selStart, selEnd int
	// cursorShown says the cursor's entry was wholly in the window as of
	// the last frame or scroll; keepCursor that a change to the entries
	// since must leave it there (see Layout).
	cursorShown, keepCursor bool

	sel    selection.Range
	hasSel bool

	hits                []FrameHit
	overTop, overBottom bool

	spacer      string
	spacerWidth int
	spacerOK    bool

	version int64
}

// New returns an empty list.
func New(opts ...Option) *Model {
	m := &Model{styles: DefaultStyles(), ids: map[string]int{}, live: -1}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// SetStyles replaces the styles. It does not bump Version: owners call it
// every frame with their theme, and bump their own version when the
// theme changes.
func (m *Model) SetStyles(s Styles) {
	m.styles = s
	m.spacerOK = false
}

// Version increases whenever the list's own state changes in a way that
// can change the next frame (scrolling, selection).
func (m *Model) Version() int64 { return m.version }

// Entries returns the current entries. The slice is the list's own.
func (m *Model) Entries() []Entry { return m.entries }

// Offsets returns the absolute line each entry starts on.
func (m *Model) Offsets() []int { return m.offsets }

// Total returns the number of lines in all entries.
func (m *Model) Total() int { return m.total }

// Width returns the width of the current entries.
func (m *Model) Width() int { return m.width }

// Height returns the window height last set.
func (m *Model) Height() int { return m.height }

// YOffset returns the first visible line.
func (m *Model) YOffset() int { return m.yOffset }

// SetYOffset assigns the first visible line (clamped when eager). It does
// not bump Version, matching viewport.SetYOffset.
func (m *Model) SetYOffset(n int) { m.setY(n); m.noteScrolled() }

// IndexOf returns the index of the entry with id.
func (m *Model) IndexOf(id string) (int, bool) {
	i, ok := m.ids[id]
	return i, ok
}

// Shown reports whether the entry with id was on screen in the last
// frame: its rows (less Trail) in the window and none of them under a
// bottom overlay (WithBottomOverlay). An entry taller than the window
// counts once its last row is shown with nothing below the window. A
// change to the entries since the last frame (KeepLookups, SetEntries
// before Layout) is not seen until the next one: an entry not laid out
// yet is not shown.
func (m *Model) Shown(id string) bool {
	i, ok := m.ids[id]
	if !ok || i >= m.lookupLen() || m.laidHeight <= 0 {
		return false
	}
	start := m.offsets[i]
	end := start + len(m.entries[i].Lines) - m.entries[i].Trail
	bottom := m.yOffset + m.contentRows(m.yOffset, m.laidHeight)
	atEnd := m.yOffset+m.laidHeight >= m.total
	return end > start && end <= bottom && end > m.yOffset && (start >= m.yOffset || atEnd)
}

// ResetSnap makes the next Layout snap the window to the cursor even if
// the cursor has not moved (used when the content is replaced).
func (m *Model) ResetSnap() { m.hasSnapped, m.snapID, m.snapLost = false, "", false }

// HasSnapped reports whether a Layout has snapped since the last
// ResetSnap.
func (m *Model) HasSnapped() bool { return m.hasSnapped }

// CursorSpan returns the last Layout's scroll target for the cursor:
// lines [start, end) of its entry, excluding Trail rows. Both are 0 when
// no entry matched.
func (m *Model) CursorSpan() (start, end int) { return m.selStart, m.selEnd }

// MarkSnapped records sel as already snapped, so the next Layout leaves
// the offset alone unless the cursor moves to another entry. While the
// entries are current (no KeepLookups since SetEntries) the snap
// follows sel's entry when later changes move it to another index.
func (m *Model) MarkSnapped(sel int) {
	if m.hasSnapped && m.snapped == sel && m.snapID != "" {
		return
	}
	m.snapped, m.hasSnapped, m.snapID = sel, true, ""
	if m.live < 0 {
		if i := m.indexOfRef(sel); i >= 0 {
			m.snapID = m.entries[i].ID
		}
	}
}

// indexOfRef is the index of the selectable entry whose Ref is ref, or -1.
func (m *Model) indexOfRef(ref int) int {
	for i := range m.entries {
		if m.entries[i].Selectable && m.entries[i].Ref == ref {
			return i
		}
	}
	return -1
}

// SetHeight sets this frame's window height. With WithPinBottomOnShrink, a
// window that was at the bottom and gets shorter moves down by the
// difference and marks sel snapped, so the cursor does not drag it back.
func (m *Model) SetHeight(h, sel int) {
	if m.pinBottom && m.height > 0 && h < m.height && m.total > 0 && m.yOffset+m.height >= m.total {
		m.yOffset += m.height - h
		m.MarkSnapped(sel)
	}
	m.height = h
}

// SetEntries replaces the entries, rendered at width. The list takes
// ownership of es.
//
// The list keeps the window where the user left it across the change
// (anchoring); owners never position it themselves. A window that
// showed the end of the list shows the end of the new one, whichever
// rows were added, removed or resized, so new content moves the old up
// rather than landing below the fold. Otherwise the first entry with an
// ID that was on screen stays on the same screen row (the next one on
// screen when it is gone), so rows changing above or below it, or every
// row rewrapping to a new width, move nothing on screen. "At the end"
// is measured with the last frame's height (ReanchorPolicy), and a pane
// that got shorter without WithPinBottomOnShrink is not at the end: its
// top stays put. The cursor's snap follows its entry to a new index.
// When that entry is gone (removed, or replaced under a new ID) the
// next Layout takes the cursor it is given as snapped: the owner moved
// it because the content changed, not to show it. The cursor moving to
// another entry otherwise scrolls to it in Layout, as
// does a change at the same width that pushes a cursor that was wholly
// in the window out of the last frame's extent (a resize does not). That
// holds for a window at the end too, unless the cursor is on the last
// selectable entry: keeping the cursor in view wins over keeping the end.
func (m *Model) SetEntries(es []Entry, width int) {
	a := m.captureAnchor()
	a.rewrapped = width != m.width
	m.entries, m.width, m.live = es, width, -1
	m.recompute()
	m.restoreAnchor(a)
	if m.hasSnapped && m.snapID != "" {
		if i, ok := m.ids[m.snapID]; ok && m.entries[i].Selectable {
			m.snapped = m.entries[i].Ref
		} else {
			m.snapLost = true
		}
	}
}

// anchor is where the window was before the entries changed.
type anchor struct {
	ok, bottom bool
	rewrapped  bool // the width changed: the cursor need not stay on screen
	// cursorLast says the cursor's entry was the last selectable one, so
	// a window at the end keeps the end rather than the cursor.
	cursorLast bool
	ids        []string // the entries with an ID on screen, top first
	rows       []int    // the window row each started on (<0: above the top)
}

func (m *Model) captureAnchor() anchor {
	if m.total == 0 || m.height <= 0 {
		return anchor{}
	}
	h := m.height
	if m.laidHeight > 0 && !m.reanchor.BottomUsesNewHeight {
		h = m.laidHeight
	}
	a := anchor{ok: true}
	shrunk := m.laidHeight > 0 && m.height < m.laidHeight
	if m.yOffset+h >= m.total && (m.pinBottom || !shrunk) {
		a.bottom, a.cursorLast = true, m.cursorIsLast()
		return a
	}
	for i := range m.entries {
		start := m.offsets[i]
		if start >= m.yOffset+h {
			break
		}
		if start+len(m.entries[i].Lines) <= m.yOffset {
			continue
		}
		if id := m.entries[i].ID; id != "" {
			a.ids = append(a.ids, id)
			a.rows = append(a.rows, start-m.yOffset)
		}
	}
	return a
}

// cursorIsLast reports whether the cursor's entry, as the last frame
// placed it, is the last selectable entry.
func (m *Model) cursorIsLast() bool {
	if m.selEnd <= m.selStart {
		return false
	}
	found := false
	for i := range m.entries {
		if !m.entries[i].Selectable {
			continue
		}
		if found {
			return false
		}
		found = m.offsets[i] == m.selStart
	}
	return found
}

func (m *Model) restoreAnchor(a anchor) {
	switch {
	case !a.ok:
	case a.bottom:
		m.yOffset = m.maxOffset()
		// A change that pushes a cursor wholly on screen out of the
		// window scrolls back to it in Layout, as below, unless the
		// cursor is on the last entry: then the end stays in view.
		m.keepCursor = m.cursorShown && !a.rewrapped && !a.cursorLast
		return
	default:
		for k, id := range a.ids {
			i, ok := m.ids[id]
			if !ok {
				continue
			}
			row := a.rows[k]
			if row < 0 {
				// Started above the top: the same row of it stays on
				// top, or its last when it got shorter.
				row = -min(-row, max(len(m.entries[i].Lines)-1, 0))
			}
			m.yOffset = m.offsets[i] - row
			break
		}
		m.keepCursor = m.cursorShown && !a.rewrapped
	}
	if m.eager {
		m.setY(m.yOffset)
	}
}

// KeepLookups says that, until the next SetEntries, only the first n
// entries still describe the screen: EntryAt, text selection and anchor
// resolution ignore the rest. The scroll extent (offsets, total) is kept.
// Owners call it when they discard their render cache but have not
// rebuilt it yet (0 for the channel pane; 1 for the thread pane, whose
// parent entry stays current).
func (m *Model) KeepLookups(n int) { m.live = max(n, 0) }

// lookupLen is the number of entries lookups may see.
func (m *Model) lookupLen() int {
	if m.live < 0 || m.live > len(m.entries) {
		return len(m.entries)
	}
	return m.live
}

// ReplaceEntries swaps the entries at the given indexes, keeping every
// other entry (a partial rebuild). Offsets are recomputed once, after all
// of them, and the window is anchored as by SetEntries.
func (m *Model) ReplaceEntries(updates map[int]Entry) {
	a := m.captureAnchor()
	for i, e := range updates {
		if old := m.entries[i].ID; old != e.ID {
			if old != "" && m.ids[old] == i {
				delete(m.ids, old)
			}
			if e.ID != "" {
				m.ids[e.ID] = i
			}
		}
		m.entries[i] = e
	}
	m.recomputeOffsets()
	m.restoreAnchor(a)
}

// ScrollUp moves the window up n lines and marks sel snapped. Eager: a
// no-op at the top or with no content (Version still bumps).
func (m *Model) ScrollUp(n, sel int) {
	if n <= 0 {
		return
	}
	if m.eager {
		if m.yOffset > 0 && m.total > 0 {
			m.setY(m.yOffset - n)
		}
	} else {
		m.yOffset = max(m.yOffset-n, 0)
	}
	m.noteScrolled()
	m.MarkSnapped(sel)
	m.version++
}

// ScrollDown moves the window down n lines and marks sel snapped. Eager:
// a no-op at the bottom or with no content. Lazy: may overshoot until the
// next Layout clamps it.
func (m *Model) ScrollDown(n, sel int) {
	if n <= 0 {
		return
	}
	if m.eager {
		if m.yOffset < m.maxOffset() && m.total > 0 {
			m.setY(m.yOffset + n)
		}
	} else {
		m.yOffset += n
	}
	m.noteScrolled()
	m.MarkSnapped(sel)
	m.version++
}

// ScrollHint reports whether a drag at content row contentY should
// auto-scroll: -1 at (or above) the top row, +1 at the bottom row, else 0.
func (m *Model) ScrollHint(contentY int) int {
	switch {
	case m.height <= 0:
		return 0
	case contentY <= 0:
		return -1
	case contentY >= m.height-1:
		return +1
	}
	return 0
}

func (m *Model) maxOffset() int { return max(0, m.total-m.height) }

func (m *Model) setY(n int) {
	if m.eager {
		n = min(max(n, 0), m.maxOffset())
	}
	m.yOffset = n
}

// clamp bounds the offset to [0, total-height] once per frame.
func (m *Model) clamp() {
	if m.yOffset < 0 {
		m.yOffset = 0
	}
	if mx := max(0, m.total-m.height); m.yOffset > mx {
		m.yOffset = mx
	}
}

func (m *Model) recompute() {
	m.recomputeOffsets()
	clear(m.ids)
	for i := range m.entries {
		if id := m.entries[i].ID; id != "" {
			m.ids[id] = i
		}
	}
}

func (m *Model) recomputeOffsets() {
	m.offsets = m.offsets[:0]
	off := 0
	for i := range m.entries {
		m.offsets = append(m.offsets, off)
		off += len(m.entries[i].Lines)
	}
	m.total = off
}

// noteScrolled records whether the cursor's entry, as the last frame
// placed it, is wholly in the window after a scroll.
func (m *Model) noteScrolled() {
	m.cursorShown = m.selEnd > m.selStart && m.selStart >= m.yOffset && m.selEnd <= m.yOffset+m.contentRows(m.yOffset, m.height)
}

// contentRows is how many of the h rows of a window at offset y show
// content: all of them, less the row a bottom overlay covers when there
// is content below (WithBottomOverlay).
func (m *Model) contentRows(y, h int) int {
	if m.overlay && y+h < m.total && h > 1 {
		return h - 1
	}
	return h
}
