// internal/ui/unsent.go
//
// Text the user typed whose send or edit failed and that could not go
// back to its composer (that conversation has a newer draft) goes to
// the clipboard. The clipboard holds one thing, so each such text is
// kept in a list for the session and the clipboard always gets all of
// them: a second failure must not overwrite the first. A text leaves the
// list when it goes back to a composer or when a message Slack accepts
// holds it as whole lines.
package ui

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// unsentLimit caps the unsent texts kept; the oldest go first.
const unsentLimit = 20

// unsentTexts is the session's failed texts that are on the clipboard
// and not yet recovered, oldest first.
type unsentTexts struct {
	texts []string
	// dropped counts texts the cap pushed out this session.
	dropped int
}

// add keeps text, moving an identical one to the end, and drops the
// oldest past unsentLimit.
func (u *unsentTexts) add(text string) {
	u.remove(text)
	u.texts = append(u.texts, text)
	if over := len(u.texts) - unsentLimit; over > 0 {
		u.texts = slices.Delete(u.texts, 0, over)
		u.dropped += over
	}
}

// remove forgets text: it reached a composer again.
func (u *unsentTexts) remove(text string) {
	u.texts = slices.DeleteFunc(u.texts, func(t string) bool { return t == text })
}

// sent forgets every text that sent holds as whole lines
// (containsLines): Slack has it, typed again or pasted from the
// clipboard, alone, in the whole bundle or under a note. A text that
// is only part of a line ("yes" in "yesterday works") stays.
func (u *unsentTexts) sent(sent string) {
	u.texts = slices.DeleteFunc(u.texts, func(t string) bool { return containsLines(sent, t) })
}

// containsLines reports whether text's lines appear in sent as whole,
// consecutive lines.
func containsLines(sent, text string) bool {
	have, want := strings.Split(sent, "\n"), strings.Split(text, "\n")
	for i := 0; i+len(want) <= len(have); i++ {
		if slices.Equal(have[i:i+len(want)], want) {
			return true
		}
	}
	return false
}

// clipboard is what the clipboard holds: every unsent text, oldest
// first, separated by a blank line.
func (u *unsentTexts) clipboard() string { return strings.Join(u.texts, "\n\n") }

// note is the end of a failure toast that put text on the clipboard.
func (u *unsentTexts) note() string {
	n := len(u.texts)
	var s string
	if n <= 1 {
		s = "Your text is on the clipboard."
	} else {
		s = fmt.Sprintf("%d unsent messages are on the clipboard.", n)
	}
	if u.dropped > 0 {
		s += fmt.Sprintf(" %d older ones were dropped.", u.dropped)
	}
	return s
}

// keepUnsent adds text to the unsent list and copies the whole list to
// the clipboard.
func (a *App) keepUnsent(text string) tea.Cmd {
	a.unsent.add(text)
	return a.clipboardWrite(a.unsent.clipboard())
}

// recovered forgets text from the unsent list: it is in a composer
// again.
func (a *App) recovered(text string) {
	if text != "" {
		a.unsent.remove(text)
	}
}

// delivered forgets the unsent texts a message or edit Slack accepted
// contains: the user typed one again or pasted the clipboard, which
// holds them all, so sending the clipboard empties the list.
func (a *App) delivered(text string) {
	if text != "" {
		a.unsent.sent(text)
	}
}
