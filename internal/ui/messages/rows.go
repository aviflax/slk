package messages

import (
	"fmt"
	"slices"
	"strings"
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
)

// RowChange reports what a row operation did and where. Index is -1
// for RowNone.
type RowChange struct {
	Op    RowOp
	Index int
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
// to put it back: the index it was at, and whether it was the selected
// row.
type RemovedRow struct {
	Item     MessageItem
	Index    int
	Selected bool
}

// Reinsert puts item back at index i, clamped to the slice, and reports
// RowAdded there. When a row with item's TS is already present (the
// pane reloaded it) nothing changes: RowNone.
func Reinsert(rows []MessageItem, item MessageItem, i int) ([]MessageItem, RowChange) {
	if item.TS != "" && FirstIndexOfTS(rows, item.TS) >= 0 {
		return rows, noRowChange
	}
	i = max(0, min(i, len(rows)))
	return slices.Insert(rows, i, item), RowChange{Op: RowAdded, Index: i}
}

// AppendIfAbsent appends item unless a row with its TS is already
// present. An item with an empty TS is always appended.
func AppendIfAbsent(rows []MessageItem, item MessageItem) ([]MessageItem, RowChange) {
	if item.TS != "" && IndexOfTS(rows, item.TS) >= 0 {
		return rows, noRowChange
	}
	rows = append(rows, item)
	return rows, RowChange{Op: RowAdded, Index: len(rows) - 1}
}

// Upsert replaces the last row with item's TS by item, or appends item
// when there is none. An item with an empty TS is always appended.
func Upsert(rows []MessageItem, item MessageItem) ([]MessageItem, RowChange) {
	if item.TS != "" {
		if out, ch := ReplaceTS(rows, item.TS, item); ch.Op != RowNone {
			return out, ch
		}
	}
	rows = append(rows, item)
	return rows, RowChange{Op: RowAdded, Index: len(rows) - 1}
}

// Settle replaces the placeholder row localTS with item, the
// authoritative message, and reports RowReplaced at the placeholder's
// index. When another row already carries item.TS (the echo got there
// first), that row takes item and the placeholder is removed instead,
// reported as RowRemoved at the placeholder's old index, so the message
// never appears twice. A missing placeholder is a no-op (RowNone).
// Matching is literal, item.TS "" included.
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
	rows[i] = item
	return rows, RowChange{Op: RowReplaced, Index: i}
}
