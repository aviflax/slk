package mention

import (
	"strings"
	"testing"

	"github.com/gammons/slk/internal/proptest"
	"pgregory.net/rapid"
)

// Classify is the one definition; InText must agree with it everywhere.
func TestClassifyAgreesWithInText(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		self := proptest.ID("U").Draw(t, "self")
		text := proptest.SlackText([]string{self}, nil).Draw(t, "text")
		if got, want := InText(text, self), Classify(text, self) != None; got != want {
			t.Fatalf("InText = %v, Classify = %v", got, Classify(text, self))
		}
	})
}

// Adding a direct mention anywhere makes the text Direct, whatever else
// it holds; a broadcast alone never makes it more than Broadcast.
func TestClassifyDirectMentionDominates(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		self := proptest.ID("U").Draw(t, "self")
		text := proptest.SlackText(nil, nil).Draw(t, "text")
		cut := rapid.IntRange(0, len(text)).Draw(t, "at")
		with := text[:cut] + " <@" + self + "> " + text[cut:]
		if got := Classify(with, self); got != Direct {
			t.Fatalf("Classify(%q) = %v, want Direct", with, got)
		}
	})
}

// A user whose ID merely starts with self's is somebody else.
func TestClassifyIgnoresLongerIDs(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		self := proptest.ID("U").Draw(t, "self")
		other := self + rapid.StringMatching(`[A-Z0-9]{1,3}`).Draw(t, "suffix")
		text := proptest.SlackText([]string{other}, nil).Draw(t, "text")
		if !strings.Contains(text, "<@"+self+">") && Classify(text, self) == Direct {
			t.Fatalf("Classify(%q, %q) = Direct", text, self)
		}
	})
}

// FuzzClassify feeds arbitrary bytes: Classify must not panic, must agree
// with InText, and Direct must mean the exact token is present.
func FuzzClassify(f *testing.F) {
	for _, s := range []string{"", "<@U1>", "<!here>", "<@U1|x> <!channel|@channel>", "<@U12>"} {
		f.Add(s, "U1")
	}
	f.Fuzz(func(t *testing.T, text, self string) {
		k := Classify(text, self)
		if InText(text, self) != (k != None) {
			t.Fatalf("InText disagrees with Classify %v", k)
		}
		if k == Direct && (self == "" || !strings.Contains(text, "<@"+self+">")) {
			t.Fatalf("Direct without the <@%s> token in %q", self, text)
		}
	})
}
