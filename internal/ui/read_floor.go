// internal/ui/read_floor.go
//
// Slack's read cursor for a channel or thread moves forward when slk
// marks a message it put on screen. Messages do not always reach the
// screen in TS order: a message the user posted elsewhere is held while
// a send is pending in its conversation and released after newer ones
// were shown and marked. Marking it then would move Slack's cursor back
// and make those newer messages unread again. readFloors keeps, per
// channel and per thread, the newest cursor slk knows Slack holds, and
// an automatic mark is never issued at or below it.
package ui

import "github.com/gammons/slk/internal/ui/messages"

// readFloorKey names a read cursor: a channel's (threadTS "") or one
// thread's, in one workspace.
type readFloorKey struct{ teamID, channelID, threadTS string }

// readFloors is the newest read cursor slk knows Slack holds for each
// channel and thread this session: the marks slk issued, the read state
// it loaded, and the marks made elsewhere. An explicit mark-unread
// (here or in another client) moves it back, and only that does.
type readFloors map[readFloorKey]string

// behind reports whether a mark at ts would not move the cursor
// forward: it is at or before the floor.
func (f readFloors) behind(k readFloorKey, ts string) bool {
	floor, ok := f[k]
	return ok && messages.CompareTS(ts, floor) <= 0
}

// advance raises the floor to ts, a cursor Slack now holds, if it is
// newer. Ignores "" and placeholders.
func (f readFloors) advance(k readFloorKey, ts string) {
	if ts == "" || messages.IsLocalTS(ts) || f.behind(k, ts) {
		return
	}
	f[k] = ts
}

// reset puts the floor at ts in either direction: Slack's cursor was
// set there on purpose (a mark-unread, or a mark made in another
// client). "" forgets it.
func (f readFloors) reset(k readFloorKey, ts string) {
	if ts == "" {
		delete(f, k)
		return
	}
	f[k] = ts
}

// channelFloor and threadFloor key the active workspace's cursors.
func (a *App) channelFloor(channelID string) readFloorKey {
	return readFloorKey{teamID: a.activeTeamID, channelID: channelID}
}

func (a *App) threadFloor(channelID, threadTS string) readFloorKey {
	return readFloorKey{teamID: a.activeTeamID, channelID: channelID, threadTS: threadTS}
}

// markedUnread records the user's mark-unread of channelID (threadTS
// "") or of a thread, from boundaryTS: Slack's cursor now sits just
// before it. The floor moves back to the boundary, so a message after
// it marks again, and a staged automatic mark of that conversation is
// dropped, since issuing it would undo the mark-unread.
func (a *App) markedUnread(channelID, threadTS, boundaryTS string) {
	if threadTS == "" {
		a.readFloors.reset(a.channelFloor(channelID), boundaryTS)
		if a.pendingChannelMark.channelID == channelID {
			a.pendingChannelMark = pendingChannelMarkState{}
		}
		return
	}
	a.readFloors.reset(a.threadFloor(channelID, threadTS), boundaryTS)
	if a.pendingThreadMark.channelID == channelID && a.pendingThreadMark.threadTS == threadTS {
		a.pendingThreadMark = pendingThreadMarkState{}
	}
}
