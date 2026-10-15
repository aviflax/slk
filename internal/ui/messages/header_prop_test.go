package messages

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/gammons/slk/internal/config"
	"github.com/gammons/slk/internal/ui/styles"
	"pgregory.net/rapid"
)

// applyDarkTheme pins the theme for a test that renders styled text.
func applyDarkTheme(t *testing.T) {
	styles.Apply("dark", config.Theme{})
	t.Cleanup(func() { styles.Apply("dark", config.Theme{}) })
}

// ReplaceHeaderTimestamp either leaves rendered alone or swaps exactly
// the first styled short timestamp for the styled replacement, and only
// when the header line it builds fits maxWidth. A replacement equal to
// short is a no-op.
func TestReplaceHeaderTimestamp(t *testing.T) {
	applyDarkTheme(t)
	rapid.Check(t, func(t *rapid.T) {
		short := rapid.SampledFrom([]string{"3:04 PM", "15:04", "9:00 AM"}).Draw(t, "short")
		replacement := rapid.SampledFrom([]string{short, "Tue Sep 29, " + short, "Mon Sep 29 2025, " + short, "x"}).Draw(t, "replacement")
		name := rapid.StringMatching(`[a-z]{1,12}`).Draw(t, "name")
		header := styles.Username("U1", true).Render(name) + " " + styles.Timestamp.Render(short)
		rendered := header
		// A body quoting the same time, styled the same way, must keep it.
		if rapid.Bool().Draw(t, "echo") {
			rendered += "\n" + styles.Timestamp.Render(short)
		}
		maxWidth := rapid.IntRange(0, 40).Draw(t, "maxWidth")

		got := ReplaceHeaderTimestamp(rendered, header, short, replacement, maxWidth)
		if replacement == short {
			if got != rendered {
				t.Fatalf("replacement == short changed %q to %q", rendered, got)
			}
			return
		}
		styledShort, styledNew := styles.Timestamp.Render(short), styles.Timestamp.Render(replacement)
		newHeader := strings.Replace(header, styledShort, styledNew, 1)
		fits := lipgloss.Width(newHeader) <= maxWidth
		want := rendered
		if fits {
			want = strings.Replace(rendered, styledShort, styledNew, 1)
		}
		if got != want {
			t.Fatalf("maxWidth %d (header fits: %v):\n got %q\nwant %q", maxWidth, fits, got, want)
		}
		if got != rendered && lipgloss.Width(strings.SplitN(got, "\n", 2)[0]) > maxWidth {
			t.Fatalf("header line wider than %d: %q", maxWidth, got)
		}
		if strings.Count(got, styledShort) < strings.Count(rendered, styledShort)-1 {
			t.Fatalf("more than one timestamp replaced: %q", got)
		}
	})
}
