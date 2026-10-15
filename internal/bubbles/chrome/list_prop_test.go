package chrome

import (
	"testing"

	xansi "github.com/charmbracelet/x/ansi"
	"pgregory.net/rapid"
)

// Window returns 0 <= start <= end <= total, showing exactly
// min(maxRows, total) rows (none when either is non-positive), for any
// selected index, in range or not.
func TestWindowBounds(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		total := rapid.IntRange(-2, 200).Draw(t, "total")
		maxRows := rapid.IntRange(-2, 40).Draw(t, "maxRows")
		sel := rapid.IntRange(-10, 250).Draw(t, "selected")
		s, e := Window(sel, total, maxRows)
		if s < 0 || s > e || e > max(total, 0) {
			t.Fatalf("Window(%d,%d,%d) = [%d,%d)", sel, total, maxRows, s, e)
		}
		if want := max(min(maxRows, total), 0); e-s != want {
			t.Fatalf("Window(%d,%d,%d) shows %d rows, want %d", sel, total, maxRows, e-s, want)
		}
	})
}

// A selected row that exists is inside the window.
func TestWindowShowsSelected(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		total := rapid.IntRange(1, 200).Draw(t, "total")
		maxRows := rapid.IntRange(1, 40).Draw(t, "maxRows")
		sel := rapid.IntRange(0, total-1).Draw(t, "selected")
		if s, e := Window(sel, total, maxRows); sel < s || sel >= e {
			t.Fatalf("Window(%d,%d,%d) = [%d,%d) misses selected", sel, total, maxRows, s, e)
		}
	})
}

// Moving the selection one row down moves the window down by at most
// one row and never up, so the list does not jump while the cursor
// stays inside it; the window never moves while the selection stays
// within it and has not reached the last row of the window.
func TestWindowStable(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		total := rapid.IntRange(2, 200).Draw(t, "total")
		maxRows := rapid.IntRange(1, 40).Draw(t, "maxRows")
		sel := rapid.IntRange(0, total-2).Draw(t, "selected")
		s1, _ := Window(sel, total, maxRows)
		s2, _ := Window(sel+1, total, maxRows)
		if d := s2 - s1; d < 0 || d > 1 {
			t.Fatalf("window start moved by %d (sel %d->%d, total %d, rows %d)", d, sel, sel+1, total, maxRows)
		}
		_, e1 := Window(sel, total, maxRows)
		if sel+1 < e1 && s1 != s2 {
			t.Fatalf("window moved although %d stayed inside [%d,%d)", sel+1, s1, e1)
		}
	})
}

// Gutter marks exactly the thumb rows: the run of thumb cells is
// non-empty when the list scrolls, and there are none when it does not.
func TestListGutterThumbRun(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		total := rapid.IntRange(1, 200).Draw(t, "total")
		maxRows := rapid.IntRange(1, 40).Draw(t, "maxRows")
		sel := rapid.IntRange(0, total-1).Draw(t, "selected")
		s, e := Window(sel, total, maxRows)
		l := List{Start: s, End: e, Total: total}
		var thumbs []int
		for i := s; i < e; i++ {
			g := l.Gutter(testStyles(), i)
			if !l.Scrolls() {
				if g != "" {
					t.Fatalf("gutter on a list that fits: %q", g)
				}
				continue
			}
			if xansi.Strip(g) == "█" {
				thumbs = append(thumbs, i)
			}
		}
		if !l.Scrolls() {
			return
		}
		if len(thumbs) == 0 {
			t.Fatal("no thumb row")
		}
		for k := 1; k < len(thumbs); k++ {
			if thumbs[k] != thumbs[k-1]+1 {
				t.Fatalf("thumb rows not contiguous: %v", thumbs)
			}
		}
	})
}
