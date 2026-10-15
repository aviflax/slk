// Package buttonrow lays out a row of labelled controls (buttons, select
// labels) that wraps to further rows at a width. It is the one
// implementation shared by the Block Kit renderer and the confirm prompt.
package buttonrow

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Placed is one label's position after Layout.
type Placed struct {
	Index int    // position in the labels passed to Layout
	Row   int    // 0-based row
	Col   int    // display column the label starts at
	Text  string // the label as drawn; shortened with "…" if wider than width
	Width int    // display width of Text
}

// Layout places labels left to right, gap columns apart, starting a new row
// when the next label would pass width. A label wider than width (when
// width > 0) is shortened with "…"; it is ANSI-aware, so styled labels keep
// their escapes. With width <= 0 every label after the first starts a row.
func Layout(labels []string, width, gap int) []Placed {
	out := make([]Placed, 0, len(labels))
	row, end := 0, 0 // end: column just past the last placed label on row
	for i, label := range labels {
		if width > 0 && lipgloss.Width(label) > width {
			label = ansi.Truncate(label, width, "…")
		}
		w := lipgloss.Width(label)
		col := 0
		if i > 0 {
			col = end + gap
			if width <= 0 || col+w > width {
				row++
				col = 0
			}
		}
		out = append(out, Placed{Index: i, Row: row, Col: col, Text: label, Width: w})
		end = col + w
	}
	return out
}

// Rows joins placed labels into one string per row, gap spaces apart.
func Rows(placed []Placed, gap int) []string {
	return RowsFunc(placed, strings.Repeat(" ", gap), func(p Placed) string { return p.Text })
}

// RowsFunc joins placed labels into one string per row, drawing each with
// render and putting gap between neighbours on a row. gap must be as wide
// as the gap passed to Layout, or the columns in Placed stop matching.
func RowsFunc(placed []Placed, gap string, render func(Placed) string) []string {
	var rows []string
	var b strings.Builder
	cur := -1
	for _, p := range placed {
		switch {
		case p.Row != cur && cur >= 0:
			rows = append(rows, b.String())
			b.Reset()
		case p.Row == cur:
			b.WriteString(gap)
		}
		cur = p.Row
		b.WriteString(render(p))
	}
	if cur >= 0 {
		rows = append(rows, b.String())
	}
	return rows
}
