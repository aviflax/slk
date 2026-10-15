package messages

import (
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
		return fmt.Sprintf("local:%d", rapid.IntRange(1, 50).Draw(t, label+"N"))
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

// Settle never leaves two rows with one TS when it was given none:
// whether the authoritative TS is new, already present (the echo landed
// first), or equal to the placeholder's own, and whether the
// placeholder is there at all.
func TestSettleNeverDuplicatesTS(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		rows := drawRows(t)
		localTS := drawTarget(t, rows)
		item := MessageItem{TS: drawTarget(t, rows), Text: "settled"}
		before := slices.Clone(rows)
		got, ch := Settle(rows, localTS, item)
		assertDistinctTS(t, got)
		hadLocal := IndexOfTS(before, localTS) >= 0
		switch {
		case !hadLocal:
			if ch.Op != RowNone || !sameRows(got, before) {
				t.Fatalf("no placeholder: got %v [%s], want unchanged", ch, tsList(got))
			}
		case IndexOfTS(got, item.TS) < 0:
			t.Fatalf("settled TS %q missing from [%s]", item.TS, tsList(got))
		case IndexOfTS(got, localTS) >= 0 && localTS != item.TS:
			t.Fatalf("placeholder %q still in [%s]", localTS, tsList(got))
		}
	})
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
		i := IndexOfTS(before, ts)
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

// AppendIfAbsent adds a row only for a TS the slice lacks; Upsert always
// ends with exactly one row for the TS, replacing in place or appending.
func TestAppendIfAbsentAndUpsert(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		rows := drawRows(t)
		item := MessageItem{TS: drawTarget(t, rows), Text: "new"}
		present := IndexOfTS(rows, item.TS)
		before := slices.Clone(rows)

		got, ch := AppendIfAbsent(slices.Clone(rows), item)
		if present >= 0 {
			if ch.Op != RowNone || !sameRows(got, before) {
				t.Fatalf("AppendIfAbsent on present %q: %v [%s]", item.TS, ch, tsList(got))
			}
		} else if ch.Op != RowAdded || ch.Index != len(before) || !sameRow(got[len(got)-1], item) {
			t.Fatalf("AppendIfAbsent on absent %q: %v [%s]", item.TS, ch, tsList(got))
		}

		got, ch = Upsert(slices.Clone(rows), item)
		assertDistinctTS(t, got)
		want := RowChange{Op: RowAdded, Index: len(before)}
		if present >= 0 {
			want = RowChange{Op: RowReplaced, Index: present}
		}
		if ch != want || !sameRow(got[ch.Index], item) {
			t.Fatalf("Upsert %q: %v, want %v; [%s]", item.TS, ch, want, tsList(got))
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
