package messages

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/gammons/slk/internal/proptest"
	"pgregory.net/rapid"
)

// drawRowTS is a row TS: a Slack TS, or now and then a "local:N"
// placeholder id.
func drawRowTS(t *rapid.T, label string) string {
	if rapid.IntRange(0, 4).Draw(t, label+"Local") == 0 {
		return LocalTS(uint64(rapid.IntRange(1, 50).Draw(t, label+"N")))
	}
	return proptest.TS().Draw(t, label)
}

// drawRows is a pane's rows with distinct TSes, each with a word of text
// so a moved or rewritten row is visible to a comparison.
func drawRows(t *rapid.T) []MessageItem {
	tss := rapid.SliceOfNDistinct(rapid.Custom(func(t *rapid.T) string { return drawRowTS(t, "ts") }),
		0, 12, func(s string) string { return s }).Draw(t, "tss")
	rows := make([]MessageItem, len(tss))
	for i, ts := range tss {
		rows[i] = MessageItem{TS: ts, Text: proptest.Word().Draw(t, "text")}
	}
	return rows
}

// drawTarget is a TS to operate on: one of rows' TSes, or (also always
// when rows is empty) one absent from them.
func drawTarget(t *rapid.T, rows []MessageItem) string {
	if len(rows) > 0 && rapid.Bool().Draw(t, "present") {
		return rows[rapid.IntRange(0, len(rows)-1).Draw(t, "at")].TS
	}
	for {
		ts := drawRowTS(t, "absent")
		if IndexOfTS(rows, ts) < 0 {
			return ts
		}
	}
}

func tsList(rows []MessageItem) string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.TS
	}
	return strings.Join(out, ",")
}

func assertDistinctTS(t *rapid.T, rows []MessageItem) {
	seen := make(map[string]bool, len(rows))
	for _, r := range rows {
		if seen[r.TS] {
			t.Fatalf("TS %q appears twice in [%s]", r.TS, tsList(rows))
		}
		seen[r.TS] = true
	}
}

// Settle never leaves two rows with one TS when it was given none,
// whether the authoritative TS is new, already present (the echo landed
// first), or equal to the placeholder's own, and whether the
// placeholder is there at all. It reports what it did, and every row
// other than the placeholder and the row it settles onto keeps its
// order: a replace leaves the rest in place, a move or removal only
// closes the gap and opens one where the message belongs.
func TestSettleNeverDuplicatesTS(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		rows := drawRows(t)
		localTS := drawTarget(t, rows)
		item := MessageItem{TS: drawTarget(t, rows), Text: "settled"}
		before := slices.Clone(rows)
		got, ch := Settle(rows, localTS, item)
		assertDistinctTS(t, got)
		i := IndexOfTS(before, localTS)
		j := IndexOfTS(before, item.TS)
		var want []MessageItem
		var wantCh RowChange
		switch {
		case i < 0:
			want, wantCh = before, RowChange{Op: RowNone, Index: -1}
		case j < 0 || j == i:
			// The placeholder is replaced, then the message moves to
			// where Insert puts it, unless it has no place in the
			// TS order.
			want = slices.Clone(before)
			want[i] = item
			wantCh = RowChange{Op: RowReplaced, Index: i}
			if ordered(item) {
				want = slices.Delete(want, i, i+1)
				var ins RowChange
				want, ins = Insert(want, item)
				if ins.Index != i {
					wantCh = RowChange{Op: RowMoved, Index: ins.Index, From: i}
				}
			}
		default:
			want = slices.Clone(before)
			want[j] = item
			want = slices.Delete(want, i, i+1)
			wantCh = RowChange{Op: RowRemoved, Index: i}
		}
		if ch != wantCh {
			t.Fatalf("Settle(%q, %q) reported %v, want %v", localTS, item.TS, ch, wantCh)
		}
		if !sameRows(got, want) {
			t.Fatalf("Settle(%q, %q) = [%s], want [%s]", localTS, item.TS, tsList(got), tsList(want))
		}
	})
}

// Matching is literal, so an empty TS is a TS like any other: settling
// onto "" when a row already has an empty TS drops the placeholder and
// leaves one empty-TS row holding the item.
func TestSettleEmptyTSMatchesLiterally(t *testing.T) {
	rows := []MessageItem{{TS: "", Text: "old"}, {TS: "local:1", Text: "pending"}, {TS: "1.000002", Text: "other"}}
	got, ch := Settle(rows, "local:1", MessageItem{TS: "", Text: "settled"})
	want := []MessageItem{{TS: "", Text: "settled"}, {TS: "1.000002", Text: "other"}}
	if ch != (RowChange{Op: RowRemoved, Index: 1}) || !sameRows(got, want) {
		t.Fatalf("got %v [%s], want removed at 1 and [%s]", ch, tsList(got), tsList(want))
	}

	rows = []MessageItem{{TS: "1.000001", Text: "a"}, {TS: "", Text: "pending"}}
	got, ch = Settle(rows, "", MessageItem{TS: "1.000003", Text: "settled"})
	want = []MessageItem{{TS: "1.000001", Text: "a"}, {TS: "1.000003", Text: "settled"}}
	if ch != (RowChange{Op: RowReplaced, Index: 1}) || !sameRows(got, want) {
		t.Fatalf("empty localTS: got %v [%s], want replaced at 1 and [%s]", ch, tsList(got), tsList(want))
	}
}

// Removing a row and inserting it back at the reported index gives the
// original slice back.
func TestRemoveTSThenReinsertRestores(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		rows := drawRows(t)
		ts := drawTarget(t, rows)
		before := slices.Clone(rows)
		got, ch := RemoveTS(rows, ts)
		i := IndexOfTS(before, ts)
		if i < 0 {
			if ch.Op != RowNone || ch.Index != -1 || !sameRows(got, before) {
				t.Fatalf("absent %q: got %v [%s], want unchanged", ts, ch, tsList(got))
			}
			return
		}
		if ch.Op != RowRemoved || ch.Index != i || len(got) != len(before)-1 {
			t.Fatalf("remove %q: got %v len %d, want removed at %d", ts, ch, len(got), i)
		}
		restored := slices.Insert(slices.Clone(got), ch.Index, before[i])
		if !sameRows(restored, before) {
			t.Fatalf("reinsert: [%s], want [%s]", tsList(restored), tsList(before))
		}
	})
}

// drawSortedRows is a pane's rows as Slack returns them: distinct Slack
// TSes in time order, then now and then a few placeholders at the end
// (sends still pending, newer than anything Slack has).
func drawSortedRows(t *rapid.T) []MessageItem {
	tss := rapid.SliceOfNDistinct(proptest.TS(), 0, 12, func(s string) string { return s }).Draw(t, "tss")
	slices.SortFunc(tss, CompareTS)
	rows := make([]MessageItem, 0, len(tss)+2)
	for _, ts := range tss {
		rows = append(rows, MessageItem{TS: ts, Text: proptest.Word().Draw(t, "text")})
	}
	for n := range rapid.IntRange(0, 2).Draw(t, "locals") {
		rows = append(rows, MessageItem{TS: LocalTS(uint64(n + 1))})
	}
	return rows
}

// realSorted reports whether the rows Slack knows are in time order.
func realSorted(rows []MessageItem) bool {
	real := slices.DeleteFunc(slices.Clone(rows), func(r MessageItem) bool { return IsLocalTS(r.TS) })
	return slices.IsSortedFunc(real, func(a, b MessageItem) int { return CompareTS(a.TS, b.TS) })
}

// On rows in TS order, Insert undoes a removal: removing a row and
// inserting it again gives the original slice back, and inserting a
// row whose TS is still present changes nothing.
func TestInsertUndoesRemove(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		rows := drawSortedRows(t)
		i := slices.IndexFunc(rows, func(r MessageItem) bool { return IsLocalTS(r.TS) })
		if i < 0 {
			i = len(rows)
		}
		if i == 0 {
			return
		}
		i = rapid.IntRange(0, i-1).Draw(t, "i")
		before := slices.Clone(rows)
		item := rows[i]
		got, ch := RemoveAt(rows, i)
		got, back := Insert(got, item)
		if back != (RowChange{Op: RowAdded, Index: ch.Index}) || !sameRows(got, before) {
			t.Fatalf("reinsert of %d: %v [%s], want [%s]", i, back, tsList(got), tsList(before))
		}
		again, ch := Insert(slices.Clone(got), item)
		if ch.Op != RowNone || !sameRows(again, before) {
			t.Fatalf("reinsert of a present row: %v [%s]", ch, tsList(again))
		}
	})
}

// A removed row goes back in TS order however much history was loaded
// in front of it or arrived after it in the meantime: the rows Slack
// knows stay sorted, and nothing else moves.
func TestInsertKeepsTSOrderAfterPrependsAndAppends(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		rows := drawSortedRows(t)
		item := MessageItem{TS: proptest.TS().Draw(t, "removed"), Text: "back"}
		if FirstIndexOfTS(rows, item.TS) >= 0 {
			return
		}
		got, ch := Insert(slices.Clone(rows), item)
		if ch.Op != RowAdded || got[ch.Index].TS != item.TS || !realSorted(got) {
			t.Fatalf("reinsert %s into [%s]: %v [%s]", item.TS, tsList(rows), ch, tsList(got))
		}
		if rest := slices.Delete(slices.Clone(got), ch.Index, ch.Index+1); !sameRows(rest, rows) {
			t.Fatalf("other rows moved: [%s], want [%s]", tsList(rest), tsList(rows))
		}
	})
}

// CompareTS is the time order of Slack TSes: it agrees with comparing
// seconds then microseconds, whatever the digit counts, and is
// antisymmetric.
func TestCompareTSIsTimeOrder(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		as, bs := rapid.Int64Range(0, 3_000_000_000).Draw(t, "as"), rapid.Int64Range(0, 3_000_000_000).Draw(t, "bs")
		am, bm := rapid.IntRange(0, 999_999).Draw(t, "am"), rapid.IntRange(0, 999_999).Draw(t, "bm")
		a, b := fmt.Sprintf("%d.%06d", as, am), fmt.Sprintf("%d.%06d", bs, bm)
		want := cmp.Or(cmp.Compare(as, bs), cmp.Compare(am, bm))
		if got := CompareTS(a, b); got != want || CompareTS(b, a) != -want {
			t.Fatalf("CompareTS(%s, %s) = %d, want %d", a, b, got, want)
		}
	})
}

// Replace and patch rewrite only the matched row: the length and every
// other row stay as they were. On an absent TS they change nothing.
func TestReplaceAndPatchTouchOnlyTheMatch(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		rows := drawRows(t)
		ts := drawTarget(t, rows)
		before := slices.Clone(rows)
		var got []MessageItem
		var ch RowChange
		if rapid.Bool().Draw(t, "patch") {
			got, ch = PatchTS(rows, ts, func(m *MessageItem) { m.Text = "patched"; m.IsEdited = true })
		} else {
			got, ch = ReplaceTS(rows, ts, MessageItem{TS: ts, Text: "patched", IsEdited: true})
		}
		i := IndexOfTS(before, ts) // TSes are distinct: either direction
		if len(got) != len(before) {
			t.Fatalf("length %d, want %d", len(got), len(before))
		}
		if i < 0 {
			if ch.Op != RowNone || !sameRows(got, before) {
				t.Fatalf("absent %q: got %v, want unchanged", ts, ch)
			}
			return
		}
		if ch.Op != RowReplaced || ch.Index != i {
			t.Fatalf("got %v, want replaced at %d", ch, i)
		}
		for j := range got {
			if j == i {
				if got[j].Text != "patched" || !got[j].IsEdited || got[j].TS != ts {
					t.Fatalf("row %d not rewritten: %+v", j, got[j])
				}
				continue
			}
			if !sameRow(got[j], before[j]) {
				t.Fatalf("row %d changed: %+v, want %+v", j, got[j], before[j])
			}
		}
	})
}

// Insert adds a row only for a TS the slice lacks, and leaves the other
// rows in their order; Upsert always ends with exactly one row for the
// TS, replacing in place or inserting.
func TestInsertAndUpsert(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		rows := drawRows(t)
		item := MessageItem{TS: drawTarget(t, rows), Text: "new"}
		present := IndexOfTS(rows, item.TS)
		before := slices.Clone(rows)

		got, ch := Insert(slices.Clone(rows), item)
		if present >= 0 {
			if ch.Op != RowNone || !sameRows(got, before) {
				t.Fatalf("Insert on present %q: %v [%s]", item.TS, ch, tsList(got))
			}
		} else {
			if ch.Op != RowAdded || !sameRow(got[ch.Index], item) {
				t.Fatalf("Insert on absent %q: %v [%s]", item.TS, ch, tsList(got))
			}
			if rest := slices.Delete(slices.Clone(got), ch.Index, ch.Index+1); !sameRows(rest, before) {
				t.Fatalf("Insert moved other rows: [%s], want [%s]", tsList(rest), tsList(before))
			}
		}

		got, ch = Upsert(slices.Clone(rows), item)
		assertDistinctTS(t, got)
		if present >= 0 {
			if ch != (RowChange{Op: RowReplaced, Index: present}) {
				t.Fatalf("Upsert %q: %v, want replaced at %d", item.TS, ch, present)
			}
		} else if ch.Op != RowAdded {
			t.Fatalf("Upsert %q: %v, want added", item.TS, ch)
		}
		if !sameRow(got[ch.Index], item) {
			t.Fatalf("Upsert %q: row %d is %+v", item.TS, ch.Index, got[ch.Index])
		}
	})
}

// assertPaneOrder fails unless the rows Slack knows are strictly in TS
// order (so each TS once) and every placeholder comes after them.
func assertPaneOrder(t *rapid.T, rows []MessageItem) {
	lastReal := ""
	seenLocal := false
	for _, r := range rows {
		if IsLocalTS(r.TS) {
			seenLocal = true
			continue
		}
		if seenLocal {
			t.Fatalf("row %s after a placeholder: [%s]", r.TS, tsList(rows))
		}
		if lastReal != "" && CompareTS(lastReal, r.TS) >= 0 {
			t.Fatalf("rows out of TS order or repeated: [%s]", tsList(rows))
		}
		lastReal = r.TS
	}
	assertDistinctTS(t, rows)
}

// Whatever order messages arrive in, and however the sends among them
// settle, fail and are put back, a pane's rows Slack knows stay strictly
// in TS order, each once, with the placeholders after them. Each step
// also keeps a cursor on the message it was on (RowChange.Selection),
// unless that message left.
func TestRowOpsKeepPaneOrder(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		var rows []MessageItem
		pool := rapid.SliceOfNDistinct(proptest.TS(), 1, 10, func(s string) string { return s }).Draw(t, "pool")
		var nextLocal uint64
		for range rapid.IntRange(0, 30).Draw(t, "steps") {
			sel := -1
			selTS := ""
			if len(rows) > 0 {
				sel = rapid.IntRange(0, len(rows)-1).Draw(t, "sel")
				selTS = rows[sel].TS
			}
			var ch RowChange
			gone := ""
			switch rapid.IntRange(0, 4).Draw(t, "op") {
			case 0: // a message arrives (or is put back)
				rows, ch = Insert(rows, MessageItem{TS: rapid.SampledFrom(pool).Draw(t, "ts")})
			case 1: // the user sends
				nextLocal++
				rows, ch = Insert(rows, MessageItem{TS: LocalTS(nextLocal)})
			case 2: // a send settles, perhaps onto a TS already shown
				locals := slices.DeleteFunc(slices.Clone(rows), func(r MessageItem) bool { return !IsLocalTS(r.TS) })
				if len(locals) == 0 {
					continue
				}
				l := rapid.SampledFrom(locals).Draw(t, "settle").TS
				gone = l
				rows, ch = Settle(rows, l, MessageItem{TS: rapid.SampledFrom(pool).Draw(t, "as")})
			case 3: // a row goes: a failed send, a delete
				if len(rows) == 0 {
					continue
				}
				gone = rows[rapid.IntRange(0, len(rows)-1).Draw(t, "rm")].TS
				rows, ch = RemoveTS(rows, gone)
			case 4: // the same message arrives twice
				if len(rows) == 0 {
					continue
				}
				rows, ch = Insert(rows, rows[rapid.IntRange(0, len(rows)-1).Draw(t, "dup")])
			}
			assertPaneOrder(t, rows)
			if sel >= 0 && selTS != gone {
				if got := ch.Selection(sel); got < 0 || got >= len(rows) || rows[got].TS != selTS {
					t.Fatalf("cursor on %s at %d went to %d after %v: [%s]", selTS, sel, got, ch, tsList(rows))
				}
			}
		}
	})
}

// With distinct TSes the two lookups agree; with a duplicate, the front
// scan finds the first copy and the back scan the last.
func TestIndexLookupsByDirection(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		rows := drawRows(t)
		ts := drawTarget(t, rows)
		if IndexOfTS(rows, ts) != FirstIndexOfTS(rows, ts) {
			t.Fatalf("distinct rows: last %d != first %d", IndexOfTS(rows, ts), FirstIndexOfTS(rows, ts))
		}
		if len(rows) == 0 {
			return
		}
		src := rapid.IntRange(0, len(rows)-1).Draw(t, "dupOf")
		at := rapid.IntRange(0, len(rows)).Draw(t, "dupAt")
		dup := slices.Insert(slices.Clone(rows), at, rows[src])
		wantFirst, wantLast := at, src+1 // the copy lands before the original
		if at > src {
			wantFirst, wantLast = src, at
		}
		first, last := FirstIndexOfTS(dup, rows[src].TS), IndexOfTS(dup, rows[src].TS)
		if first != wantFirst || last != wantLast {
			t.Fatalf("dup of %d at %d: first %d last %d in [%s]", src, at, first, last, tsList(dup))
		}
	})
}

// sameRow compares the fields these tests set.
func sameRow(a, b MessageItem) bool {
	return a.TS == b.TS && a.Text == b.Text && a.IsEdited == b.IsEdited
}

func sameRows(a, b []MessageItem) bool { return slices.EqualFunc(a, b, sameRow) }

// Every LocalTS is a placeholder and no Slack TS is.
func TestIsLocalTSTellsPlaceholdersFromSlackTSes(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		if n := rapid.Uint64().Draw(t, "n"); !IsLocalTS(LocalTS(n)) {
			t.Fatalf("LocalTS(%d) = %q is not local", n, LocalTS(n))
		}
		if ts := proptest.TS().Draw(t, "ts"); IsLocalTS(ts) {
			t.Fatalf("Slack TS %q counted as local", ts)
		}
	})
}

// LastIndexBefore finds the last row before i that keep accepts: that
// row passes, every row between it and i fails, and -1 means none
// before i passes.
func TestLastIndexBeforeIsTheNearestKeptRow(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		rows := drawRows(t)
		for i := range rows {
			rows[i].Ephemeral = rapid.Bool().Draw(t, "ephemeral")
		}
		i := rapid.IntRange(0, len(rows)).Draw(t, "i")
		j := LastIndexBefore(rows, i, KnownToSlack)
		if j >= i || j < -1 || (j >= 0 && !KnownToSlack(rows[j])) {
			t.Fatalf("LastIndexBefore(%d) = %d", i, j)
		}
		for k := j + 1; k < i; k++ {
			if KnownToSlack(rows[k]) {
				t.Fatalf("LastIndexBefore(%d) = %d skipped row %d (%+v)", i, j, k, rows[k])
			}
		}
	})
}

// CompareTS is a total order on any strings, Slack TSes or not: a TS
// that is not "<seconds>.<microseconds>" (a placeholder, garbage) sorts
// after every one that is, and such strings sort among themselves as
// strings. Comparing them as strings against numeric TSes broke
// transitivity: "10.0" < "9.0x" < "9.5" < "10.0".
func TestCompareTSIsATotalOrder(t *testing.T) {
	ts := rapid.OneOf(
		proptest.TS(),
		rapid.SampledFrom([]string{"9.0x", "10", "1.5", "local:1", "local:10", "", "-1.0", "x"}),
		rapid.String(),
	)
	rapid.Check(t, func(t *rapid.T) {
		a, b, c := ts.Draw(t, "a"), ts.Draw(t, "b"), ts.Draw(t, "c")
		if CompareTS(a, b) != -CompareTS(b, a) {
			t.Fatalf("not antisymmetric: %q %q", a, b)
		}
		if CompareTS(a, b) <= 0 && CompareTS(b, c) <= 0 && CompareTS(a, c) > 0 {
			t.Fatalf("not transitive: %q <= %q <= %q but %q > %q", a, b, c, a, c)
		}
		_, _, aok := splitTS(a)
		_, _, bok := splitTS(b)
		if aok && !bok && CompareTS(a, b) >= 0 {
			t.Fatalf("Slack TS %q does not sort before malformed %q", a, b)
		}
	})
	if CompareTS("10.0", "9.0x") < 0 && CompareTS("9.0x", "9.5") < 0 && CompareTS("9.5", "10.0") < 0 {
		t.Error("10.0 < 9.0x < 9.5 < 10.0: a cycle")
	}
}
