package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ui/messages"
)

// sendPermalink delivers an OpenPermalinkMsg through Update and returns
// the messages its command produced and the reply.
func sendPermalink(t *testing.T, a *App, url string) ([]tea.Msg, error) {
	t.Helper()
	reply := make(chan error, 1)
	_, cmd := a.Update(OpenPermalinkMsg{URL: url, Reply: reply})
	msgs := drainCmd(cmd)
	select {
	case err := <-reply:
		return msgs, err
	default:
		t.Fatal("no reply sent")
		return nil, nil
	}
}

func TestOpenPermalink_OtherChannel_GoesThroughApplyLocation(t *testing.T) {
	app, opened := linkTestApp(t)
	app.activeChannelID = "CELSEWHERE"

	msgs, err := sendPermalink(t, app, "https://myteam.slack.com/archives/C054JFCBN69/p1779284733270139?thread_ts=1779284700.000100")
	if err != nil {
		t.Fatalf("reply = %v, want nil", err)
	}
	var sel *ChannelSelectedMsg
	for _, m := range msgs {
		if cs, ok := m.(ChannelSelectedMsg); ok {
			sel = &cs
		}
	}
	if sel == nil || sel.ID != "C054JFCBN69" || sel.FromHistory {
		t.Fatalf("ChannelSelectedMsg = %+v in %#v", sel, msgs)
	}
	p := app.pendingLinkNav
	if p == nil || p.MessageTS != "1779284733.270139" || p.ThreadTS != "1779284700.000100" || p.TeamID != "T1" {
		t.Errorf("pendingLinkNav = %+v", p)
	}
	if *opened != "" {
		t.Errorf("browser opened %q", *opened)
	}
}

func TestOpenPermalink_ActiveChannel_SelectsMessage(t *testing.T) {
	app, _ := linkTestApp(t)
	app.activeChannelID = "C054JFCBN69"
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1779284733.270139", Text: "target"},
		{TS: "1779284734.000000", Text: "newer"},
	})
	if _, err := sendPermalink(t, app, "https://myteam.slack.com/archives/C054JFCBN69/p1779284733270139"); err != nil {
		t.Fatalf("reply = %v", err)
	}
	if sel, ok := app.messagepane.SelectedMessage(); !ok || sel.TS != "1779284733.270139" {
		t.Errorf("selected = %+v ok=%v", sel, ok)
	}
}

// A link slk will not show is refused with a reason, and the browser is
// left to the sender: unlike OpenLinkMsg, the socket path never opens it.
func TestOpenPermalink_Declined_RepliesWithoutBrowser(t *testing.T) {
	for _, tc := range []struct{ name, url, want string }{
		{"foreign workspace", "https://otherteam.slack.com/archives/C054JFCBN69/p1779284733270139", `workspace "otherteam"`},
		{"unknown channel", "https://myteam.slack.com/archives/CUNKNOWN1/p1779284733270139", "CUNKNOWN1"},
		{"not a permalink", "https://github.com/foo/bar", "not a Slack message permalink"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app, opened := linkTestApp(t)
			msgs, err := sendPermalink(t, app, tc.url)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("reply = %v, want it to mention %q", err, tc.want)
			}
			if *opened != "" || len(msgs) != 0 || app.pendingLinkNav != nil {
				t.Errorf("declined link still acted: browser=%q msgs=%#v pending=%+v", *opened, msgs, app.pendingLinkNav)
			}
		})
	}
}

func TestOpenPermalink_NoActiveWorkspace(t *testing.T) {
	app, _ := linkTestApp(t)
	delete(app.workspaceDomains, "T1")
	_, err := sendPermalink(t, app, "https://myteam.slack.com/archives/C054JFCBN69/p1779284733270139")
	if err == nil {
		t.Fatal("reply = nil while no workspace is active")
	}
}

// A full or unbuffered reply channel must not block Update.
func TestOpenPermalink_UnreadReplyDoesNotBlock(t *testing.T) {
	app, _ := linkTestApp(t)
	app.Update(OpenPermalinkMsg{URL: "https://github.com/foo/bar", Reply: make(chan error)})
	app.Update(OpenPermalinkMsg{URL: "https://github.com/foo/bar"})
}

// A message the sender stopped waiting for (it already told its client
// "did not respond", e.g. while $EDITOR held the event loop) must not
// navigate when it finally arrives.
func TestOpenPermalink_AbandonedIsDropped(t *testing.T) {
	app, opened := linkTestApp(t)
	app.activeChannelID = "CELSEWHERE"
	reply := make(chan error, 1)
	_, cmd := app.Update(OpenPermalinkMsg{
		URL:   "https://myteam.slack.com/archives/C054JFCBN69/p1779284733270139",
		Claim: func() bool { return false },
		Reply: reply,
	})
	if msgs := drainCmd(cmd); len(msgs) != 0 {
		t.Errorf("abandoned link produced %#v", msgs)
	}
	if app.pendingLinkNav != nil || *opened != "" {
		t.Errorf("abandoned link acted: pending=%+v browser=%q", app.pendingLinkNav, *opened)
	}
	select {
	case err := <-reply:
		t.Errorf("abandoned link got a reply: %v", err)
	default:
	}
}

func TestOpenPermalink_ClaimedIsRouted(t *testing.T) {
	app, _ := linkTestApp(t)
	app.activeChannelID = "CELSEWHERE"
	claims := 0
	reply := make(chan error, 1)
	app.Update(OpenPermalinkMsg{
		URL:   "https://myteam.slack.com/archives/C054JFCBN69/p1779284733270139",
		Claim: func() bool { claims++; return true },
		Reply: reply,
	})
	if claims != 1 || app.pendingLinkNav == nil {
		t.Errorf("claims=%d pending=%+v", claims, app.pendingLinkNav)
	}
	if err := <-reply; err != nil {
		t.Errorf("reply = %v", err)
	}
}

// applyLocation refuses during an upload and shows a toast; the socket
// sender must hear that as a refusal, not "ok".
func TestOpenPermalink_DuringUpload_Declines(t *testing.T) {
	app, opened := linkTestApp(t)
	app.activeChannelID = "CELSEWHERE"
	app.compose.SetUploading(true)
	_, err := sendPermalink(t, app, "https://myteam.slack.com/archives/C054JFCBN69/p1779284733270139")
	if err == nil || !strings.Contains(err.Error(), "upload is in progress") {
		t.Errorf("reply = %v, want an upload refusal", err)
	}
	if app.pendingLinkNav != nil || *opened != "" {
		t.Errorf("declined link acted: pending=%+v browser=%q", app.pendingLinkNav, *opened)
	}
}

// o during an upload keeps its behaviour: the toast, not the browser.
func TestOpenLink_DuringUpload_ToastsWithoutBrowser(t *testing.T) {
	app, opened := linkTestApp(t)
	app.activeChannelID = "CELSEWHERE"
	app.compose.SetUploading(true)
	_, cmd := app.Update(OpenLinkMsg{URL: "https://myteam.slack.com/archives/C054JFCBN69/p1779284733270139"})
	drainCmd(cmd)
	if *opened != "" {
		t.Errorf("browser opened %q during an upload", *opened)
	}
	if got := statusbarText(app); !strings.Contains(got, "Upload in progress") {
		t.Errorf("status bar = %q, want the upload toast", got)
	}
}
