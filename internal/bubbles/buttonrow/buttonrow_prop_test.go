package buttonrow

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"pgregory.net/rapid"

	"github.com/gammons/slk/internal/proptest"
)

// labels are words with SGR escapes and wide runes in them, on one line.
func labels() *rapid.Generator[[]string] {
	label := rapid.Custom(func(t *rapid.T) string {
		s := proptest.StyledText().Draw(t, "label")
		s = proptest.NoVS16(strings.ReplaceAll(s, "\n", " "))
		return s
	})
	return rapid.SliceOfN(label, 0, 8)
}

// Layout with a positive width: one Placed per label in order with Index
// preserved; every label fits (Col+Width <= width); rows never go back
// and columns increase along a row, separated by at least gap; Width is
// the display width of Text; the first label starts the first row at 0;
// labels that fit come back unchanged and the others keep their
// visible prefix.
func TestLayoutFits(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		ls := labels().Draw(t, "labels")
		width := rapid.IntRange(1, 40).Draw(t, "width")
		gap := rapid.IntRange(0, 4).Draw(t, "gap")
		placed := Layout(ls, width, gap)
		if len(placed) != len(ls) {
			t.Fatalf("%d placed for %d labels", len(placed), len(ls))
		}
		for i, p := range placed {
			if p.Index != i {
				t.Fatalf("Index %d at position %d", p.Index, i)
			}
			if p.Width != lipgloss.Width(p.Text) {
				t.Fatalf("Width %d != width of %q", p.Width, p.Text)
			}
			if p.Col+p.Width > width {
				t.Fatalf("label %d overflows: col %d + %d > %d (%q)", i, p.Col, p.Width, width, p.Text)
			}
			if lipgloss.Width(ls[i]) <= width && p.Text != ls[i] {
				t.Fatalf("fitting label %q was changed to %q", ls[i], p.Text)
			}
			if i == 0 {
				if p.Row != 0 || p.Col != 0 {
					t.Fatalf("first label at row %d col %d", p.Row, p.Col)
				}
				continue
			}
			q := placed[i-1]
			switch p.Row {
			case q.Row:
				if p.Col < q.Col+q.Width+gap {
					t.Fatalf("labels %d,%d overlap or miss the gap: %+v %+v", i-1, i, q, p)
				}
			case q.Row + 1:
				if p.Col != 0 {
					t.Fatalf("new row starts at col %d", p.Col)
				}
			default:
				t.Fatalf("rows jumped %d -> %d", q.Row, p.Row)
			}
		}
	})
}

// A new row starts only when the label would not fit: Layout is greedy.
func TestLayoutGreedy(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		ls := labels().Draw(t, "labels")
		width := rapid.IntRange(1, 40).Draw(t, "width")
		gap := rapid.IntRange(0, 4).Draw(t, "gap")
		placed := Layout(ls, width, gap)
		for i := 1; i < len(placed); i++ {
			p, q := placed[i], placed[i-1]
			if p.Row != q.Row && q.Col+q.Width+gap+p.Width <= width {
				t.Fatalf("label %d wrapped although it fit: %+v after %+v, width %d", i, p, q, width)
			}
		}
	})
}

// With width <= 0 nothing is truncated and every label after the first
// starts its own row (for any gap >= 0).
func TestLayoutNoWidth(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		ls := labels().Draw(t, "labels")
		width := rapid.IntRange(-5, 0).Draw(t, "width")
		placed := Layout(ls, width, rapid.IntRange(0, 4).Draw(t, "gap"))
		for i, p := range placed {
			if p.Text != ls[i] || p.Row != i || p.Col != 0 {
				t.Fatalf("label %d: %+v for %q", i, p, ls[i])
			}
		}
	})
}

// Rows draws one string per used row; each is as wide as its last
// label's Col+Width (so Col matches what is drawn), within width, and
// the visible text is the labels' text with gaps between.
func TestRowsMatchPlacement(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		ls := labels().Draw(t, "labels")
		width := rapid.IntRange(1, 40).Draw(t, "width")
		gap := rapid.IntRange(0, 4).Draw(t, "gap")
		placed := Layout(ls, width, gap)
		rows := Rows(placed, gap)
		if len(placed) == 0 {
			if len(rows) != 0 {
				t.Fatalf("rows for no labels: %q", rows)
			}
			return
		}
		last := placed[len(placed)-1]
		if len(rows) != last.Row+1 {
			t.Fatalf("%d rows, last label on row %d", len(rows), last.Row)
		}
		for r, row := range rows {
			var end int
			var plain []string
			for _, p := range placed {
				if p.Row == r {
					end = p.Col + p.Width
					plain = append(plain, ansi.Strip(p.Text))
				}
			}
			if w := ansi.StringWidth(row); w != end {
				t.Fatalf("row %d width %d, want %d: %q", r, w, end, row)
			}
			if end > width {
				t.Fatalf("row %d wider than %d", r, width)
			}
			if got := ansi.Strip(row); got != strings.Join(plain, strings.Repeat(" ", gap)) {
				t.Fatalf("row %d text %q", r, got)
			}
		}
	})
}
