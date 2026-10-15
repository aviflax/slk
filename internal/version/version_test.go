package version

import (
	"strings"
	"testing"
)

func TestModalFooter(t *testing.T) {
	got := ModalFooter("v1.2.3")

	// Version is interpolated.
	if !strings.Contains(got, "slk v1.2.3") {
		t.Errorf("footer missing version: %q", got)
	}
	// Attribution wording.
	if !strings.Contains(got, "Made with") {
		t.Errorf("footer missing 'Made with': %q", got)
	}
	if !strings.Contains(got, "\u2764") {
		t.Errorf("footer missing heart glyph: %q", got)
	}
	if !strings.Contains(got, "Kira Howe") {
		t.Errorf("footer missing author: %q", got)
	}
	// URL is present and OSC-8 wrapped (clickable), with visible label.
	if !strings.Contains(got, "https://kirahowe.com") {
		t.Errorf("footer missing url: %q", got)
	}
	if !strings.Contains(got, "\x1b]8;;https://kirahowe.com\x1b\\") {
		t.Errorf("footer url not OSC-8 wrapped: %q", got)
	}
	// URL is parenthesized.
	if !strings.Contains(got, "(") || !strings.Contains(got, ")") {
		t.Errorf("footer url not parenthesized: %q", got)
	}
}

func TestModalFooterUsesGivenVersion(t *testing.T) {
	got := ModalFooter("dev")
	if !strings.Contains(got, "slk dev") {
		t.Errorf("expected 'slk dev' in %q", got)
	}
}

func TestUpstreamCredit(t *testing.T) {
	got := UpstreamCredit()
	for _, want := range []string{"Grant Ammons", "\x1b]8;;https://grant.dev\x1b\\", "v0.26"} {
		if !strings.Contains(got, want) {
			t.Errorf("credit missing %q: %q", want, got)
		}
	}
}
