// internal/ui/location.go
//
// Location is the shared address type for navigable positions in a
// workspace: the currency of the navigation history, permalink jumps,
// and vim-style marks. It is deliberately the same shape as
// slackurl.Permalink (internal/slackurl/slackurl.go) with the
// workspace subdomain replaced by the team ID, because the team ID is
// what workspaceContext and every cache table key on.
package ui

import (
	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui/messages"
)

// Location identifies a workspace, a channel, and optionally a message
// within that channel and a thread the message belongs to. A location
// that names only a channel remains valid and means "the channel, at
// its default position".
type Location struct {
	// TeamID is the workspace the location belongs to. Every recorded
	// location carries it; jump paths assert it matches the active
	// workspace and refuse otherwise.
	TeamID ids.TeamID
	// ChannelID is the channel the location is in.
	ChannelID ids.ChannelID
	// MessageTS is the target message timestamp; empty when the
	// location names only a channel.
	MessageTS ids.MessageTS
	// ThreadTS is the thread parent ts when the location names a
	// reply inside a thread; empty for channel-level locations. A
	// non-empty ThreadTS means "open the thread panel instead of
	// selecting".
	ThreadTS ids.ThreadTS
}

// currentLocation names where the user is, for the navigation history
// and back-jumps: selectedLocation, except that a placeholder (a message
// still sending, whose TS Slack has never seen) or an ephemeral (which
// Slack never returns from history) is named by the nearest message
// before it that Slack knows (sentLocation).
func (a *App) currentLocation() (Location, bool) {
	loc, ok := a.selectedLocation()
	return a.sentLocation(loc), ok
}

// selectedLocation snapshots the message the user is looking at right
// now: the thread panel's selected reply when the thread panel is
// focused; in the Threads view, the parent of the selected thread; and
// otherwise the messages pane's selection. In the Activity view nothing
// is named: the messages pane is hidden behind the list there. ok=false
// when there is nothing to name. It may name a placeholder; setMark
// refuses one.
func (a *App) selectedLocation() (Location, bool) {
	if a.focusedPanel != PanelThread {
		switch a.view {
		case ViewThreads:
			// The messages pane behind the list still holds the last
			// channel, so reading it would name a message the user
			// cannot see.
			s, ok := a.threadsView.SelectedSummary()
			if !ok {
				return Location{}, false
			}
			return Location{
				TeamID:    ids.TeamID(a.activeTeamID),
				ChannelID: ids.ChannelID(s.ChannelID),
				MessageTS: ids.MessageTS(s.ThreadTS),
				ThreadTS:  ids.ThreadTS(s.ThreadTS),
			}, true
		case ViewActivity:
			return Location{}, false
		}
		return a.selectedChannelPaneLocation()
	}
	reply := a.threadPanel.SelectedReply()
	if reply == nil {
		return Location{}, false
	}
	// The thread's own channel, NOT activeChannelID. In the Threads
	// view the panel shows a thread from any channel while
	// activeChannelID still names whichever channel was last
	// opened, so recording the active channel here stored a thread
	// ts against a channel that has no such thread — the jump then
	// opened an empty panel reading "0 replies".
	channelID := a.threadPanel.ChannelID()
	if channelID == "" {
		channelID = a.activeChannelID
	}
	return Location{
		TeamID:    ids.TeamID(a.activeTeamID),
		ChannelID: ids.ChannelID(channelID),
		MessageTS: ids.MessageTS(reply.TS),
		ThreadTS:  ids.ThreadTS(a.threadPanel.ThreadTS()),
	}, true
}

// channelPaneLocation names the messages pane's selection in the
// active channel, a placeholder or an ephemeral by the message before it
// that Slack knows.
func (a *App) channelPaneLocation() (Location, bool) {
	loc, ok := a.selectedChannelPaneLocation()
	return a.sentLocation(loc), ok
}

// sentLocation is loc with a MessageTS Slack does not know as a message
// of the conversation (messages.KnownToSlack: a placeholder still
// sending, or an ephemeral) replaced by the nearest message before it in
// the same pane that Slack knows: a navigation must never ask Slack for
// a TS it never issued or does not return. With none, a thread location
// names the parent and a channel location the channel alone.
func (a *App) sentLocation(loc Location) Location {
	if loc.MessageTS == "" {
		return loc
	}
	ts := string(loc.MessageTS)
	rows := a.messagepane.Messages()
	parent := ids.MessageTS("")
	if loc.ThreadTS != "" {
		rows = a.threadPanel.Replies()
		parent = ids.MessageTS(loc.ThreadTS)
	}
	i := messages.FirstIndexOfTS(rows, ts)
	if i >= 0 && messages.KnownToSlack(rows[i]) || i < 0 && !messages.IsLocalTS(ts) {
		return loc
	}
	if i < 0 {
		i = len(rows)
	}
	loc.MessageTS = parent
	if j := messages.LastIndexBefore(rows, i, messages.KnownToSlack); j >= 0 {
		loc.MessageTS = ids.MessageTS(rows[j].TS)
	}
	return loc
}

// selectedChannelPaneLocation names the messages pane's selection in
// the active channel as it is, placeholder or not.
func (a *App) selectedChannelPaneLocation() (Location, bool) {
	msg, ok := a.messagepane.SelectedMessage()
	if !ok {
		return Location{}, false
	}
	return Location{
		TeamID:    ids.TeamID(a.activeTeamID),
		ChannelID: ids.ChannelID(a.activeChannelID),
		MessageTS: ids.MessageTS(msg.TS),
	}, true
}

// departurePosition is what a mark jump or a back-jump leaves, for the
// back-jump slot: what the user is looking at. In the Threads view that
// is the selected thread (currentLocation), not the hidden channel pane
// behind the list that currentPosition reads for history. Where nothing
// on screen is selected it falls back to currentPosition.
func (a *App) departurePosition() Location {
	if loc, ok := a.currentLocation(); ok {
		return loc
	}
	return a.currentPosition()
}

// currentPosition returns the user's current position as a Location,
// falling back to a channel-only location when no message is selected.
// The fallback carries just the workspace and channel so a
// position-less departure still degrades cleanly instead of being
// dropped. It is what history records for the active channel, so
// outside the thread panel it reads the messages pane even from the
// Threads and Activity views: that pane still holds the active
// channel's position, which is where walking back should return.
func (a *App) currentPosition() Location {
	var (
		loc Location
		ok  bool
	)
	if a.focusedPanel == PanelThread {
		loc, ok = a.currentLocation()
	} else {
		loc, ok = a.channelPaneLocation()
	}
	if ok {
		return loc
	}
	return Location{
		TeamID:    ids.TeamID(a.activeTeamID),
		ChannelID: ids.ChannelID(a.activeChannelID),
	}
}
