package ui

import (
	"fmt"
	"slices"
	"testing"

	"pgregory.net/rapid"

	"github.com/gammons/slk/internal/ui/messages"
)

// However edits of one message begin and settle, after each result the
// message shows the newest edit that is pending or has succeeded (its
// own text while pending, Slack's once it succeeded), or what it showed
// before any of them; and a result is "newest" exactly when no later
// edit is pending or has succeeded.
func TestEditLedgerShowsNewestLiveEdit(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		p := newPendingOps()
		prior := editShown{text: "prior", known: true}
		var pending, succeeded []uint64
		live := func() uint64 { // the newest pending or succeeded id, 0 for none
			return max(slices.Max(append([]uint64{0}, pending...)), slices.Max(append([]uint64{0}, succeeded...)))
		}
		for range rapid.IntRange(1, 30).Draw(t, "steps") {
			if len(pending) == 0 || rapid.Bool().Draw(t, "begin") {
				// Ids count up from 1, so the next is p.editID+1.
				text := fmt.Sprintf("t%d", p.editID+1)
				e := p.BeginEdit(pendingEdit{channelID: "C1", ts: "1.0", text: text},
					messages.MessageItem{Text: prior.text, IsEdited: prior.edited}, true)
				if e.text != text {
					t.Fatalf("edit %d is %q, want %q", e.id, e.text, text)
				}
				pending = append(pending, e.id)
				continue
			}
			id := pending[rapid.IntRange(0, len(pending)-1).Draw(t, "end")]
			ok := rapid.Bool().Draw(t, "ok")
			pending = slices.DeleteFunc(pending, func(x uint64) bool { return x == id })
			wantNewest := live() < id
			if ok {
				succeeded = append(succeeded, id)
			}
			want := prior
			if n := live(); n != 0 {
				want = editShown{text: fmt.Sprintf("t%d", n), edited: true, known: true}
				if slices.Contains(succeeded, n) {
					want.text = fmt.Sprintf("s%d", n)
				}
			}
			e, shown, newest, found := p.EndEdit(id, ok, fmt.Sprintf("s%d", id))
			if !found || e.id != id {
				t.Fatalf("EndEdit(%d) found=%v id=%d", id, found, e.id)
			}
			if shown != want || newest != wantNewest {
				t.Fatalf("EndEdit(%d ok=%v): shown %+v newest %v, want %+v newest %v (pending %v succeeded %v)",
					id, ok, shown, newest, want, wantNewest, pending, succeeded)
			}
			if p.EditPending("C1", "1.0") != (len(pending) > 0) {
				t.Fatalf("EditPending = %v with pending %v", !(len(pending) > 0), pending)
			}
			if len(pending) == 0 {
				// The ledger forgets the message; the next edit starts
				// from what it shows now.
				prior, succeeded = shown, nil
			}
		}
	})
}
