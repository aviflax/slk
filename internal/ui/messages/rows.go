package messages

// Row operations on a pane's []MessageItem, located by TS. Both content
// panes (messages.Model and thread.Model) use them, then fix up their
// own selection and caches from the RowChange. The optimistic send
// paths (append, settle, roll back, upsert) and the delete path write
// through them; the edit, reaction and reply-count paths only locate
// their row with FirstIndexOfTS and write it themselves.
//
// The functions write through the input's backing array the way the
// panes always have (an in-place replace, an append-based remove), so a
// caller that needs the old slice afterwards must clone it first.
// Matching is literal: an empty ts matches a row with an empty TS, so
// callers that must not touch such rows keep their own empty-ts guard.

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

// PatchTS applies f to the last row with TS ts, in place. Nothing calls
// it yet: it is here for the optimistic edit, which will patch a row's
// text before Slack answers. The edit paths scan from the front
// (FirstIndexOfTS), so that change has to pick one direction for both.
func PatchTS(rows []MessageItem, ts string, f func(*MessageItem)) ([]MessageItem, RowChange) {
	i := IndexOfTS(rows, ts)
	if i < 0 {
		return rows, noRowChange
	}
	f(&rows[i])
	return rows, RowChange{Op: RowReplaced, Index: i}
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
