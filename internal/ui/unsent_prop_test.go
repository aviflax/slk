package ui

import (
	"strings"
	"testing"

	"github.com/gammons/slk/internal/proptest"
	"pgregory.net/rapid"
)

// containsLines finds a text placed on whole lines anywhere in a
// message (alone, under or over other lines), and never finds one
// glued to other text on its first or last line.
func TestContainsLinesProperty(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		lines := rapid.SliceOfN(proptest.Word(), 1, 3).Draw(t, "lines")
		text := strings.Join(lines, "\n")
		above := rapid.SliceOfN(proptest.Word(), 0, 2).Draw(t, "above")
		below := rapid.SliceOfN(proptest.Word(), 0, 2).Draw(t, "below")
		sent := strings.Join(append(append(above, lines...), below...), "\n")
		if !containsLines(sent, text) {
			t.Fatalf("containsLines(%q, %q) = false, want true", sent, text)
		}
		// Glued to a word, the text's first line is not a whole line,
		// and sent has no other place the block could fit.
		glue := proptest.Word().Draw(t, "glue")
		if glue != "" && !strings.Contains(glue, "\n") && containsLines(glue+text, text) {
			t.Fatalf("containsLines(%q, %q) = true for a text glued to a word", glue+text, text)
		}
		if glue != "" && !strings.Contains(glue, "\n") && containsLines(text+glue, text) {
			t.Fatalf("containsLines(%q, %q) = true for a text glued to a word", text+glue, text)
		}
	})
}
