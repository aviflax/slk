package scrollbar

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

var (
	bg    = lipgloss.Color("#000000")
	track = lipgloss.Color("#444444")
	thumb = lipgloss.Color("#ff00ff")
)

func TestOverlay_NoOverflowReturnsUnchanged(t *testing.T) {
	in := []string{"row a", "row b"}
	cp := append([]string(nil), in...)
	out := Overlay(cp, 10, 2, 0, 2, bg, track, thumb)
	if strings.Join(out, "\n") != strings.Join(in, "\n") {
		t.Fatalf("expected unchanged when total <= visibleHeight; got %q", out)
	}
}

func TestOverlay_AddsGlyphPerVisibleRow(t *testing.T) {
	in := []string{"aaaaa", "bbbbb", "ccccc"}
	out := Overlay(append([]string(nil), in...), 5, 9, 0, 3, bg, track, thumb)
	if len(out) != 3 {
		t.Fatalf("expected 3 rows; got %d", len(out))
	}
	for i, row := range out {
		// Each row must end with either the track or thumb glyph, and
		// must be exactly width display columns wide.
		if w := ansi.StringWidth(row); w != 5 {
			t.Fatalf("row %d width = %d; want 5 (%q)", i, w, row)
		}
		if !(strings.Contains(row, ThumbGlyph) || strings.Contains(row, TrackGlyph)) {
			t.Fatalf("row %d missing scrollbar glyph: %q", i, row)
		}
	}
}

func TestOverlay_ThumbAtTopWhenAtTop(t *testing.T) {
	in := []string{"...", "...", "..."}
	out := Overlay(append([]string(nil), in...), 3, 100, 0, 3, bg, track, thumb)
	// First row is at yOffset=0, total=100, visible=3 => thumb starts at row 0.
	if !strings.Contains(out[0], "\u2588") {
		t.Fatalf("expected thumb on row 0 at top; got %q", out[0])
	}
}

func TestOverlay_ThumbAtBottomWhenAtBottom(t *testing.T) {
	in := []string{"...", "...", "..."}
	// yOffset = total - visible = 97 -> thumb sits at bottom.
	out := Overlay(append([]string(nil), in...), 3, 100, 97, 3, bg, track, thumb)
	if !strings.Contains(out[2], "\u2588") {
		t.Fatalf("expected thumb on last row at bottom; got %q", out[2])
	}
}

func TestOverlay_PadsShortRowsToWidth(t *testing.T) {
	in := []string{"x", "yy", "zzz"}
	out := Overlay(append([]string(nil), in...), 6, 9, 0, 3, bg, track, thumb)
	for i, row := range out {
		if w := ansi.StringWidth(row); w != 6 {
			t.Fatalf("row %d width = %d; want 6 (%q)", i, w, row)
		}
	}
}

func TestVisible(t *testing.T) {
	if Visible(5, 5) {
		t.Error("Visible(5,5) should be false")
	}
	if !Visible(6, 5) {
		t.Error("Visible(6,5) should be true")
	}
}

func TestThumb(t *testing.T) {
	cases := []struct {
		name                   string
		total, offset, visible int
		start, end             int
	}{
		{"top", 20, 0, 10, 0, 5},
		{"middle rounds down", 20, 5, 10, 2, 7},
		{"bottom", 20, 10, 10, 5, 10},
		{"thumb at least one row", 100, 97, 3, 2, 3},
		{"offset past the end clamps", 20, 50, 10, 5, 10},
		{"no overflow fills the track", 5, 0, 10, 0, 10},
		{"no window", 20, 0, 0, 0, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, e := Thumb(c.total, c.offset, c.visible)
			if s != c.start || e != c.end {
				t.Errorf("Thumb(%d, %d, %d) = [%d, %d), want [%d, %d)",
					c.total, c.offset, c.visible, s, e, c.start, c.end)
			}
		})
	}
}

// Every pane draws its gutter in the last content column, right
// against the pane's own border (│ or the focused ┃). A solid line
// track there read as a doubled border, so the track is dotted.
func TestOverlay_TrackIsNotASolidLine(t *testing.T) {
	out := Overlay([]string{"a", "b", "c", "d"}, 4, 100, 0, 4, bg, track, thumb)
	last := ansi.Strip(out[3])
	glyph := last[len(last)-len(TrackGlyph):]
	if glyph != TrackGlyph {
		t.Fatalf("track row ends %q, want the track glyph %q", last, TrackGlyph)
	}
	if strings.ContainsAny(TrackGlyph, "│┃║|") {
		t.Fatalf("track glyph %q is a solid vertical line, which doubles the pane border", TrackGlyph)
	}
	if !strings.HasSuffix(ansi.Strip(out[0]), ThumbGlyph) {
		t.Fatalf("thumb row ends %q, want %q", ansi.Strip(out[0]), ThumbGlyph)
	}
}
