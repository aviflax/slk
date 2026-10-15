package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui/channelfinder"
)

// The cmd/slk service closures return nil when no workspace is active.
// An action the user started must still end: each becomes its failure,
// so its spinner, in-flight flag or upload clears and the user is told.
func TestNilServiceResultsBecomeFailures(t *testing.T) {
	t.Run("mark unread", func(t *testing.T) {
		a := newTestApp(t, withActiveChannel("C1"))
		a.SetMessageService(core.NewMessageService(core.MessageServiceFuncs{
			MarkUnread: func(ids.ChannelID, ids.ThreadTS, ids.MessageTS, int) core.Msg { return nil },
		}))
		_, cmd := a.Update(MarkUnreadMsg{ChannelID: "C1", BoundaryTS: "1.000000"})
		if got := deliveredMsgs[MessageMarkedUnreadMsg](t, cmd)[0]; got.Err == nil || got.ChannelID != "C1" {
			t.Errorf("result = %+v, want a failure for C1", got)
		}
	})
	t.Run("older history", func(t *testing.T) {
		a := newTestApp(t, withActiveChannel("C1"))
		a.setOlderMessagesFetcherForTest(func(ids.ChannelID, ids.MessageTS) core.Msg { return nil })
		cmd := a.maybeFetchOlderHistory(true)
		got := deliveredMsgs[OlderMessagesLoadedMsg](t, cmd)[0]
		a.Update(got)
		if a.fetchingOlder["C1"] {
			t.Error("older-history fetch still marked in flight")
		}
	})
	t.Run("join", func(t *testing.T) {
		a := newTestApp(t, withActiveChannel("C1"))
		fns := channelFuncsForTest(a)
		fns.Join = func(ids.ChannelID, string) core.Msg { return nil }
		setChannelFuncsForTest(a, fns)
		cmd := a.openChannel(channelfinder.ChannelResult{ID: "C9", Name: "design"})
		if got := deliveredMsgs[ChannelJoinFailedMsg](t, cmd)[0]; got.ID != "C9" || got.Err == nil {
			t.Errorf("result = %+v, want C9's join to fail", got)
		}
	})
	t.Run("upload", func(t *testing.T) {
		a := newTestApp(t, withActiveChannel("C1"))
		a.focusedPanel = PanelMessages
		a.SetMode(ModeInsert)
		a.compose.AddAttachment(core.PendingAttachment{Filename: "a.png", Bytes: []byte("png"), Size: 3})
		a.setUploaderForTest(func(string, string, string, []core.PendingAttachment) tea.Cmd {
			return func() tea.Msg { return nil }
		})
		cmd := a.submitWithAttachments(&a.compose)
		got := deliveredMsgs[UploadResultMsg](t, cmd)[0]
		a.Update(got)
		if got.Err == nil || a.compose.Uploading() {
			t.Errorf("result = %+v, uploading = %v: want a failure that ends the upload", got, a.compose.Uploading())
		}
	})
}
