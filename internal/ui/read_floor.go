// internal/ui/read_floor.go
//
// Slack's read cursor for a channel or thread moves forward when slk
// marks a message it put on screen. Messages do not always reach the
// screen in TS order: a message the user posted elsewhere is held while
// a send is pending in its conversation and released after newer ones
// were shown and marked. Marking it then would move Slack's cursor back
// and make those newer messages unread again. readFloors keeps, per
// channel and per thread, the newest cursor slk knows Slack holds or is
// about to hold, and an automatic mark is never issued at or below it.
//
// A floor is only as good as slk's view of the cursor, which is
// continuous only while slk watches the workspace: away from it, or
// while its socket is down, slk misses the marks (and mark-unreads)
// made elsewhere. So a workspace's floors are forgotten when it stops
// being the active one and on every change of its connection, and the
// marks the user makes by opening a thread or entering a channel are
// not held back by a floor at all.
package ui

import (
	"slices"

	"github.com/gammons/slk/internal/ui/messages"
)

// readFloorKey names a read cursor: a channel's (threadTS "") or one
// thread's, in one workspace.
type readFloorKey struct{ teamID, channelID, threadTS string }

// readFloor is what slk knows of one cursor: held is the newest Slack
// has confirmed (a mark it accepted, the read state it reported, a mark
// made elsewhere), inFlight the marks slk issued that Slack has not
// answered yet.
type readFloor struct {
	held     string
	inFlight []string
}

// readFloors is the read floor of each channel and thread slk has seen
// a cursor for since it last started watching that workspace.
type readFloors map[readFloorKey]readFloor

// behind reports whether a mark at ts would not move the cursor
// forward: it is at or before the held cursor or a mark in flight.
func (f readFloors) behind(k readFloorKey, ts string) bool {
	fl, ok := f[k]
	if !ok {
		return false
	}
	if fl.held != "" && messages.CompareTS(ts, fl.held) <= 0 {
		return true
	}
	return slices.ContainsFunc(fl.inFlight, func(m string) bool { return messages.CompareTS(ts, m) <= 0 })
}

// issued records a mark at ts slk is sending: until Slack answers, no
// automatic mark goes at or behind it. Ignores "" and placeholders.
func (f readFloors) issued(k readFloorKey, ts string) {
	if ts == "" || messages.IsLocalTS(ts) {
		return
	}
	fl := f[k]
	fl.inFlight = append(fl.inFlight, ts)
	f[k] = fl
}

// held records that Slack holds the cursor at ts or later: it accepted
// slk's mark there, or reported that cursor. The floor rises to ts if
// it is newer, and the mark at ts is no longer in flight.
func (f readFloors) held(k readFloorKey, ts string) {
	if ts == "" || messages.IsLocalTS(ts) {
		return
	}
	fl := f[k]
	if fl.held == "" || messages.CompareTS(ts, fl.held) > 0 {
		fl.held = ts
	}
	if i := slices.Index(fl.inFlight, ts); i >= 0 {
		fl.inFlight = slices.Delete(fl.inFlight, i, i+1)
	}
	f[k] = fl
}

// failed records that Slack rejected slk's mark at ts: the cursor did
// not move, so the mark stops counting.
func (f readFloors) failed(k readFloorKey, ts string) {
	fl, ok := f[k]
	if !ok {
		return
	}
	if i := slices.Index(fl.inFlight, ts); i >= 0 {
		fl.inFlight = slices.Delete(fl.inFlight, i, i+1)
	}
	if fl.held == "" && len(fl.inFlight) == 0 {
		delete(f, k)
		return
	}
	f[k] = fl
}

// reset puts the floor at ts in either direction: Slack's cursor was
// set there on purpose (a mark-unread, or a mark made in another
// client). "" forgets it.
func (f readFloors) reset(k readFloorKey, ts string) {
	if ts == "" {
		delete(f, k)
		return
	}
	f[k] = readFloor{held: ts}
}

// forget drops every floor of workspace teamID ("" drops them all):
// slk has stopped watching its cursors.
func (f readFloors) forget(teamID string) {
	for k := range f {
		if teamID == "" || k.teamID == teamID {
			delete(f, k)
		}
	}
}

// channelFloor and threadFloor key the active workspace's cursors.
func (a *App) channelFloor(channelID string) readFloorKey {
	return readFloorKey{teamID: a.activeTeamID, channelID: channelID}
}

func (a *App) threadFloor(channelID, threadTS string) readFloorKey {
	return readFloorKey{teamID: a.activeTeamID, channelID: channelID, threadTS: threadTS}
}

// leaveWorkspace is called when teamID stops being the active
// workspace: its floors go, and so do the marks staged in it, which the
// channel and thread services would otherwise issue in the workspace
// that replaces it.
func (a *App) leaveWorkspace(teamID string) {
	a.readFloors.forget(teamID)
	if a.pendingChannelMark.teamID == teamID {
		a.pendingChannelMark = pendingChannelMarkState{}
	}
	if a.pendingThreadMark.teamID == teamID {
		a.pendingThreadMark = pendingThreadMarkState{}
	}
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
