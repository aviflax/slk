package proptest

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"pgregory.net/rapid"
)

func TestPlainTextHasNoEscapes(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		if s := PlainText().Draw(t, "s"); strings.Contains(s, "\x1b") {
			t.Fatalf("escape in plain text %q", s)
		}
	})
}

// Every escape StyledText splices in is one ansi.Strip recognises, so
// stripping leaves no ESC behind.
func TestStyledTextStripsClean(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		if s := ansi.Strip(StyledText().Draw(t, "s")); strings.Contains(s, "\x1b") {
			t.Fatalf("escape survives Strip: %q", s)
		}
	})
}

func TestIDAndTSShapes(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		if id := ID("U").Draw(t, "id"); !strings.HasPrefix(id, "U") || len(id) < 9 {
			t.Fatalf("bad ID %q", id)
		}
		ts := TS().Draw(t, "ts")
		if sec, frac, ok := strings.Cut(ts, "."); !ok || len(sec) != 10 || len(frac) != 6 {
			t.Fatalf("bad TS %q", ts)
		}
	})
}
