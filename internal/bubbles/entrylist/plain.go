package entrylist

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"
)

// PlainLine is the ANSI-stripped text of one rendered row plus a
// column→byte index into it, so text selection can slice by display
// column without splitting a grapheme cluster.
//
// Bytes has one entry per display column plus a final sentinel equal to
// len(Text). A wide cluster occupies several columns that all point at
// its first byte. Zero-width clusters (combining marks, standalone ZWJ)
// are part of Text but take no column; their bytes attach to the next
// column (or to the previous one when they trail a base cluster).
type PlainLine struct {
	Text  string
	Bytes []int
}

// PlainLines returns the column-aligned plain mirror of each line of s.
//
// Widths come from uniseg (wcwidth-style cell counting), the model the
// rest of the message pipeline uses. A custom emoji whose terminal width
// disagrees may be off by one column; for selection the worst case is a
// one-cell drift between the highlight and the copied text.
func PlainLines(s string) []PlainLine {
	rawLines := strings.Split(ansi.Strip(s), "\n")
	out := make([]PlainLine, len(rawLines))
	for i, line := range rawLines {
		out[i] = buildPlainLine(line)
	}
	return out
}

// buildPlainLine computes the column→byte map for one ANSI-free line.
//
// It uses the grapheme-only iterator (FirstGraphemeClusterInString)
// rather than uniseg.NewGraphemes, whose full state machine also computes
// word, sentence and line breaks; that extra work was ~67% of a full
// message-cache build. Boundaries and widths are identical (pinned by
// TestBuildPlainLine_MatchesUnisegGraphemes).
func buildPlainLine(line string) PlainLine {
	if line == "" {
		return PlainLine{Text: "", Bytes: []int{0}}
	}
	bytesMap := make([]int, 0, len(line))
	byteOffset := 0
	state := -1
	rest := line
	for len(rest) > 0 {
		var cluster string
		var w int
		cluster, rest, w, state = uniseg.FirstGraphemeClusterInString(rest, state)
		if w <= 0 {
			byteOffset += len(cluster)
			continue
		}
		start := byteOffset
		if len(bytesMap) == 0 {
			// Zero-width clusters before the first column belong to it.
			start = 0
		}
		for k := 0; k < w; k++ {
			bytesMap = append(bytesMap, start)
		}
		byteOffset += len(cluster)
	}
	bytesMap = append(bytesMap, len(line))
	return PlainLine{Text: line, Bytes: bytesMap}
}

// DisplayWidth returns the number of display columns in p.
func DisplayWidth(p PlainLine) int {
	if len(p.Bytes) == 0 {
		return 0
	}
	return len(p.Bytes) - 1
}

// SliceColumns returns the text covering display columns [from, to) of
// p. Out-of-range arguments are clamped. Slicing into the middle of a
// wide cluster includes the whole cluster, so copied text reads as
// written.
func SliceColumns(p PlainLine, from, to int) string {
	width := DisplayWidth(p)
	if from < 0 {
		from = 0
	}
	if to > width {
		to = width
	}
	if from >= to {
		return ""
	}
	return p.Text[p.Bytes[from]:p.Bytes[to]]
}
