// internal/ui/open_channel.go
//
// Switching to a channel the user picked, shared by the channel finder
// and /open.
package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui/channelfinder"
)

// openChannel switches to a channel the user picked. One they are in is
// selected at once; one they are not is joined first, and
// ChannelJoinedMsg (reducer_channels) folds it into the sidebar and
// selects it.
func (a *App) openChannel(r channelfinder.ChannelResult) tea.Cmd {
	if r.Joined {
		a.sidebar.SelectByID(r.ID)
		return func() tea.Msg {
			return ChannelSelectedMsg{ID: r.ID, Name: r.Name, Type: r.Type}
		}
	}
	channels := a.channels
	id, name := ids.ChannelID(r.ID), r.Name
	return failNil(func() tea.Msg {
		return channels.Join(id, name)
	}, ChannelJoinFailedMsg{ID: r.ID, Name: r.Name, Err: errNoWorkspace})
}
