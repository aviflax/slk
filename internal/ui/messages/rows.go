package messages

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Row operations on a pane's []MessageItem, located by TS. Both content
// panes (messages.Model and thread.Model) use them, then fix up their
// own selection and caches from the RowChange. The optimistic send
// paths (append, settle, roll back, upsert), the edit path and the
// delete path write through them; the reaction and reply-count paths
// only locate their row with FirstIndexOfTS and write it themselves.
//
// The functions write through the input's backing array the way the
// panes always have (an in-place replace, an append-based remove), so a
// caller that needs the old slice afterwards must clone it first.
// Matching is literal: an empty ts matches a row with an empty TS, so
// callers that must not touch such rows keep their own empty-ts guard.

// localTSPrefix starts every optimistic placeholder's TS.
const localTSPrefix = "local:"

// LocalTS is the TS of the n-th optimistic placeholder: a row the user
// sent that Slack has not answered for yet. It never looks like a TS
// Slack assigns ("<seconds>.<microseconds>").
func LocalTS(n uint64) string { return fmt.Sprintf("%s%d", localTSPrefix, n) }

// IsLocalTS reports whether ts is a placeholder's (from LocalTS) rather
// than one Slack assigned. Slack has never heard of such a TS: it must
// not reach a Slack request, the cache, a mark, the navigation history
// or a link.
func IsLocalTS(ts string) bool { return strings.HasPrefix(ts, localTSPrefix) }

// KnownToSlack reports whether Slack knows msg as a message of its
// conversation, so a read cursor or an unread boundary may sit on it:
// not a placeholder still sending, and not an ephemeral (Slack never
// returns one from history).
func KnownToSlack(msg MessageItem) bool {
	return msg.TS != "" && !IsLocalTS(msg.TS) && !msg.Ephemeral
}

// LastIndexBefore returns the index of the last row before index i
// for which keep is true, or -1. i may be len(rows) to search them all.
func LastIndexBefore(rows []MessageItem, i int, keep func(MessageItem) bool) int {
	for j := min(i, len(rows)) - 1; j >= 0; j-- {
		if keep(rows[j]) {
			return j
		}
	}
	return -1
}

// RowOp is what a row operation did to the slice.
type RowOp int

const (
	// RowNone: nothing matched; the slice is unchanged.
	RowNone RowOp = iota
	// RowAdded: a row was appended at Index.
	RowAdded
	// RowRemoved: the row that was at Index is gone.
	RowRemoved
	// RowReplaced: the row at Index was replaced or patched in place.
	RowReplaced
	// RowMoved: the row that was at From was replaced and now sits at
	// Index, where its TS belongs (Settle of a placeholder whose
	// message is older than rows shown after it).
	RowMoved
)

// RowChange reports what a row operation did and where. Index is -1
// for RowNone. From is set only for RowMoved.
type RowChange struct {
	Op    RowOp
	Index int
	From  int
}

// Selection is where a cursor on row selected sits after the change,
// kept on the same message: a removal or insertion before it shifts
// it, and a moved row takes the cursor with it. A cursor on a removed
// row is left at the same index; each pane clamps it as it always has.
func (c RowChange) Selection(selected int) int {
	switch c.Op {
	case RowAdded:
		if c.Index <= selected {
			return selected + 1
		}
	case RowRemoved:
		if c.Index < selected {
			return selected - 1
		}
	case RowMoved:
		if selected == c.From {
			return c.Index
		}
		if c.From < selected {
			selected--
		}
		if c.Index <= selected {
			selected++
		}
	}
	return selected
}

var noRowChange = RowChange{Op: RowNone, Index: -1}

// IndexOfTS returns the index of the last row with the given TS, or -1.
// It scans from the back: the rows written by TS are almost always the
// newest.
func IndexOfTS(rows []MessageItem, ts string) int {
	for i := len(rows) - 1; i >= 0; i-- {
		if rows[i].TS == ts {
			return i
		}
	}
	return -1
}

// FirstIndexOfTS returns the index of the first row with the given TS,
// or -1. The edit, delete, reaction and reply-count paths have always
// scanned from the front and keep doing so: Settle no longer leaves two
// rows with one TS, but rows from before that fix, or an empty TS, can
// still repeat, and the direction decides which of them is written.
func FirstIndexOfTS(rows []MessageItem, ts string) int {
	for i, r := range rows {
		if r.TS == ts {
			return i
		}
	}
	return -1
}

// ReplaceTS replaces the last row with TS ts by item.
func ReplaceTS(rows []MessageItem, ts string, item MessageItem) ([]MessageItem, RowChange) {
	i := IndexOfTS(rows, ts)
	if i < 0 {
		return rows, noRowChange
	}
	rows[i] = item
	return rows, RowChange{Op: RowReplaced, Index: i}
}

// RemoveTS removes the last row with TS ts.
func RemoveTS(rows []MessageItem, ts string) ([]MessageItem, RowChange) {
	return RemoveAt(rows, IndexOfTS(rows, ts))
}

// RemoveAt removes the row at i; an out-of-range i is a no-op.
func RemoveAt(rows []MessageItem, i int) ([]MessageItem, RowChange) {
	if i < 0 || i >= len(rows) {
		return rows, noRowChange
	}
	return append(rows[:i], rows[i+1:]...), RowChange{Op: RowRemoved, Index: i}
}

// PatchTS applies f to the first row with TS ts, in place. It scans
// from the front, like the edit paths it serves (the optimistic edit,
// its rollback and the message_changed echo), so the edit and its echo
// always write the same row.
func PatchTS(rows []MessageItem, ts string, f func(*MessageItem)) ([]MessageItem, RowChange) {
	i := FirstIndexOfTS(rows, ts)
	if i < 0 {
		return rows, noRowChange
	}
	f(&rows[i])
	return rows, RowChange{Op: RowReplaced, Index: i}
}

// RemovedRow is a row a pane took out (by Remove), with what it takes
// to put it back: whether it was the selected row, and the TS of the
// row the removal left the cursor on ("" for none), so a restore can
// tell whether the user has moved the cursor since.
type RemovedRow struct {
	Item     MessageItem
	Selected bool
	LeftOn   string
}

// TSAt is the Slack TS ("<seconds>.<microseconds>") of instant t, to
// compare with CompareTS.
func TSAt(t time.Time) string {
	return fmt.Sprintf("%d.%06d", t.Unix(), t.Nanosecond()/1000)
}

// CompareTS orders two Slack TSes ("<seconds>.<microseconds>") by time,
// numerically, so "9.5" sorts before "10.0". A string not of that shape
// (a placeholder, "") sorts after every TS, and such strings sort among
// themselves as strings, so the order is total on any strings.
func CompareTS(a, b string) int {
	as, af, aok := splitTS(a)
	bs, bf, bok := splitTS(b)
	switch {
	case !aok && !bok:
		return strings.Compare(a, b)
	case !aok:
		return 1
	case !bok:
		return -1
	}
	if c := cmp.Compare(as, bs); c != 0 {
		return c
	}
	return cmp.Compare(af, bf)
}

// splitTS parses a Slack TS into whole seconds and microseconds.
func splitTS(ts string) (sec, micro int64, ok bool) {
	whole, frac, _ := strings.Cut(ts, ".")
	sec, err := strconv.ParseInt(whole, 10, 64)
	if err != nil || len(frac) > 6 {
		return 0, 0, false
	}
	if frac != "" {
		f, err := strconv.ParseInt(frac+strings.Repeat("0", 6-len(frac)), 10, 64)
		if err != nil || f < 0 {
			return 0, 0, false
		}
		micro = f
	}
	return sec, micro, sec >= 0
}

// ordered reports whether a row takes part in the TS order: a row Slack
// assigned a TS. Placeholders (newer than anything Slack has, in the
// order they were sent) and rows with no TS sit after them.
func ordered(r MessageItem) bool { return r.TS != "" && !IsLocalTS(r.TS) }

// Insert is the one way a row is added to a pane, and keeps its
// invariant: the rows Slack knows are in TS order (CompareTS) and each
// TS appears once, with placeholders after them. A row Slack knows goes
// before the first such row with a later TS, or, when there is none,
// after the last one, ahead of any placeholders; rows loaded or added
// before it (older history prepended, newer messages appended) do not
// decide where it goes, only their TSes do. A placeholder, or a row
// with no TS, is appended. Reports RowAdded at the row's index. When a
// row with item's TS is already present nothing changes: RowNone. An
// item with an empty TS is always added.
func Insert(rows []MessageItem, item MessageItem) ([]MessageItem, RowChange) {
	if item.TS != "" && IndexOfTS(rows, item.TS) >= 0 {
		return rows, noRowChange
	}
	if !ordered(item) {
		return append(rows, item), RowChange{Op: RowAdded, Index: len(rows)}
	}
	// From the back: the rows Slack knows are in order, so the newest
	// arrival, the common case, stops at the first row it passes.
	i := -1
	for j := len(rows) - 1; j >= 0; j-- {
		if !ordered(rows[j]) {
			continue
		}
		if CompareTS(rows[j].TS, item.TS) <= 0 {
			break
		}
		i = j
	}
	if i < 0 {
		// No row Slack knows is newer: after the last of them, ahead
		// of any placeholders.
		i = 0
		for j := len(rows) - 1; j >= 0; j-- {
			if ordered(rows[j]) {
				i = j + 1
				break
			}
		}
	}
	return slices.Insert(rows, i, item), RowChange{Op: RowAdded, Index: i}
}

// IsNewest reports whether the row at i is the newest a pane shows: no
// row Slack knows comes after it. A pane follows such a row (selects it
// and scrolls to it), as it always followed an appended one; a row
// inserted further up leaves the cursor where it is.
func IsNewest(rows []MessageItem, i int) bool {
	for _, r := range rows[i+1:] {
		if ordered(r) {
			return false
		}
	}
	return true
}

// Upsert replaces the last row with item's TS by item, or Inserts item
// when there is none. An item with an empty TS is always added.
func Upsert(rows []MessageItem, item MessageItem) ([]MessageItem, RowChange) {
	if item.TS != "" {
		if out, ch := ReplaceTS(rows, item.TS, item); ch.Op != RowNone {
			return out, ch
		}
	}
	return Insert(rows, item)
}

// Settle replaces the placeholder row localTS with item, the
// authoritative message. The message then moves to where its TS
// belongs (Insert's order; an item with no TS, or a placeholder's,
// stays put): RowReplaced at the placeholder's index when it is
// already there, else RowMoved from that index. A message
// older than rows shown after its placeholder (another client's
// message that arrived while it was sending) therefore lands among
// them in time order. When another row already carries item.TS (a
// history load brought it in first), that row takes item and the
// placeholder is removed instead, reported as RowRemoved at the
// placeholder's old index, so the message never appears twice. A
// missing placeholder is a no-op (RowNone). Matching is literal,
// item.TS "" included.
func Settle(rows []MessageItem, localTS string, item MessageItem) ([]MessageItem, RowChange) {
	i := IndexOfTS(rows, localTS)
	if i < 0 {
		return rows, noRowChange
	}
	if item.TS != localTS {
		if j := IndexOfTS(rows, item.TS); j >= 0 {
			rows[j] = item
			return RemoveAt(rows, i)
		}
	}
	if !ordered(item) {
		rows[i] = item
		return rows, RowChange{Op: RowReplaced, Index: i}
	}
	rows, _ = RemoveAt(rows, i)
	rows, ch := Insert(rows, item)
	if ch.Index == i {
		return rows, RowChange{Op: RowReplaced, Index: i}
	}
	return rows, RowChange{Op: RowMoved, Index: ch.Index, From: i}
}
