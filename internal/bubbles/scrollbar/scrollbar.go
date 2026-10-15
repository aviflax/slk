// Package scrollbar renders a 1-column proportional scrollbar gutter onto
// pre-rendered visible lines, and computes its thumb (Thumb) for callers
// that draw the gutter themselves. It is the same look used by the modal
// overlays (channelfinder, workspacefinder, themeswitcher, reactionpicker):
// a Border-colored track (┊) with a Primary-colored thumb (█) sized
// proportionally to the visible window, positioned proportionally to the
// scroll offset.
package scrollbar

import (
	"image/color"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// The gutter glyphs Overlay draws. The gutter is a pane's last content
// column, so it always sits against the pane's right border (│, or ┃
// when focused); a solid │ track there read as a doubled border. The
// dotted track stays visible as a track without looking like a line.
const (
	TrackGlyph = "\u250a" // ┊
	ThumbGlyph = "\u2588" // █
)

// Overlay returns visible with a 1-column scrollbar overlaid onto the last
// display column of every line. Each line is truncated to width-1 columns
// and a track or thumb glyph is appended.
//
// total is the total number of scrollable lines in the underlying content.
// yOffset is the line index of the first visible row. visibleHeight is the
// number of visible rows (typically len(visible), but can be smaller if the
// caller wants to leave trailing rows alone).
//
// When total <= visibleHeight (no overflow) the function returns visible
// unchanged: callers should decide separately whether to reserve a gutter.
//
// width is the full panel content width (the last column is replaced by the
// scrollbar). bg is the panel background color used to keep the gutter cell
// flush with the surrounding theme; trackFg / thumbFg control the track and
// thumb glyph colors.
func Overlay(visible []string, width, total, yOffset, visibleHeight int, bg, trackFg, thumbFg color.Color) []string {
	if visibleHeight <= 0 || width < 1 {
		return visible
	}
	if total <= visibleHeight {
		return visible
	}

	thumbStart, thumbEnd := Thumb(total, yOffset, visibleHeight)

	thumbStyle := lipgloss.NewStyle().Background(bg).Foreground(thumbFg)
	trackStyle := lipgloss.NewStyle().Background(bg).Foreground(trackFg)
	thumbCell := thumbStyle.Render(ThumbGlyph)
	trackCell := trackStyle.Render(TrackGlyph)

	gutterCol := width - 1
	if gutterCol < 0 {
		gutterCol = 0
	}

	rowLimit := visibleHeight
	if rowLimit > len(visible) {
		rowLimit = len(visible)
	}

	for i := 0; i < rowLimit; i++ {
		// Truncate this row to the gutter column. ansi.Cut preserves
		// SGR state; rows shorter than gutterCol are padded by appending
		// background-colored spaces so the scrollbar lands at a consistent
		// column across rows of varying width.
		line := visible[i]
		w := ansi.StringWidth(line)
		var prefix string
		switch {
		case w > gutterCol:
			prefix = ansi.Cut(line, 0, gutterCol)
		case w == gutterCol:
			prefix = line
		default:
			pad := lipgloss.NewStyle().Background(bg).Width(gutterCol - w).Render("")
			prefix = line + pad
		}

		var glyph string
		if i >= thumbStart && i < thumbEnd {
			glyph = thumbCell
		} else {
			glyph = trackCell
		}
		visible[i] = prefix + glyph
	}
	return visible
}

// Thumb returns the [start, end) rows of the thumb within a window of
// visible rows scrolled offset rows into total. The thumb is
// visible²/total rows tall, at least one, positioned in proportion to
// offset so it reaches the bottom when the window does. This is the
// one thumb formula: Overlay and the dialog chrome
// (internal/bubbles/chrome) both use it. With no overflow the thumb
// fills the track; with no window it is empty.
func Thumb(total, offset, visible int) (start, end int) {
	if visible <= 0 {
		return 0, 0
	}
	if total <= visible {
		return 0, visible
	}
	height := max(visible*visible/total, 1)
	denom := max(total-visible, 1)
	start = min(max(offset*(visible-height)/denom, 0), visible-height)
	return start, start + height
}

// Visible reports whether a scrollbar should be drawn for the given content
// and viewport size. Callers can use this to decide whether to reserve a
// gutter or to skip the overlay entirely.
func Visible(total, visibleHeight int) bool {
	return total > visibleHeight
}
