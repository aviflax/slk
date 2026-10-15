package slackclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"
	"time"

	"github.com/slack-go/slack"
)

// TestSlackAPI_EveryMethodTakesContext pins that slack-go is only ever
// called through its *Context variants. The plain variants use
// context.Background() inside slack-go, so a Client method that takes a
// ctx and calls one drops it: the request can neither be cancelled nor
// time out, and the API http.Client has no Timeout of its own. A hung
// chat.postMessage then never returns, and the send it belongs to stays
// pending for the session. SlackAPI is the only route into slack-go
// (see TestSlackAPI_DeclaresNoWorkspaceEnumeration), so a method absent
// from it cannot be called at all.
func TestSlackAPI_EveryMethodTakesContext(t *testing.T) {
	ctxType := reflect.TypeOf((*context.Context)(nil)).Elem()
	iface := reflect.TypeOf((*SlackAPI)(nil)).Elem()
	for i := 0; i < iface.NumMethod(); i++ {
		m := iface.Method(i)
		if m.Type.NumIn() == 0 || m.Type.In(0) != ctxType {
			t.Errorf("SlackAPI declares %s without a leading context.Context; use slack-go's %sContext", m.Name, m.Name)
		}
	}
}

// TestClient_MethodsHonourContext calls every Client method that takes
// a ctx and reaches the Web API against a server that never answers,
// with a short deadline. Each must return an error once the deadline
// passes. One that hangs ignores its ctx somewhere on the way to the
// request.
func TestClient_MethodsHonourContext(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) })

	newClient := func() *Client {
		base := srv.URL + "/"
		return &Client{
			api:        slack.New("xoxc-test", slack.OptionAPIURL(base), slack.OptionHTTPClient(srv.Client())),
			apiBaseURL: base,
			httpClient: srv.Client(),
			token:      "xoxc-test",
		}
	}

	calls := map[string]func(ctx context.Context, c *Client) error{
		"Connect":                func(ctx context.Context, c *Client) error { return c.Connect(ctx) },
		"GetChannels":            func(ctx context.Context, c *Client) error { _, err := c.GetChannels(ctx); return err },
		"GetConversationInfo":    func(ctx context.Context, c *Client) error { _, err := c.GetConversationInfo(ctx, "C1"); return err },
		"GetUsersInConversation": func(ctx context.Context, c *Client) error { _, err := c.GetUsersInConversation(ctx, "C1"); return err },
		"JoinChannel":            func(ctx context.Context, c *Client) error { return c.JoinChannel(ctx, "C1") },
		"GetUserProfileContext":  func(ctx context.Context, c *Client) error { _, err := c.GetUserProfileContext(ctx, "U1"); return err },
		"GetBotInfo":             func(ctx context.Context, c *Client) error { _, err := c.GetBotInfo(ctx, "B1"); return err },
		"GetHistory":             func(ctx context.Context, c *Client) error { _, err := c.GetHistory(ctx, "C1", 10, ""); return err },
		"GetOlderHistory": func(ctx context.Context, c *Client) error {
			_, err := c.GetOlderHistory(ctx, "C1", 10, "1.0")
			return err
		},
		"GetHistoryAround": func(ctx context.Context, c *Client) error {
			_, err := c.GetHistoryAround(ctx, "C1", "1.0", 10)
			return err
		},
		"SearchMessages": func(ctx context.Context, c *Client) error { _, err := c.SearchMessages(ctx, "q", 10); return err },
		"GetHistorySince": func(ctx context.Context, c *Client) error {
			_, err := c.GetHistorySince(ctx, "C1", "1.0", 10)
			return err
		},
		"GetHistorySince/no oldest": func(ctx context.Context, c *Client) error {
			_, err := c.GetHistorySince(ctx, "C1", "", 10)
			return err
		},
		"GetUserGroups":   func(ctx context.Context, c *Client) error { _, err := c.GetUserGroups(ctx); return err },
		"ListCustomEmoji": func(ctx context.Context, c *Client) error { _, err := c.ListCustomEmoji(ctx); return err },
		"SendMessage":     func(ctx context.Context, c *Client) error { _, _, err := c.SendMessage(ctx, "C1", "hi"); return err },
		"OpenConversation": func(ctx context.Context, c *Client) error {
			_, _, err := c.OpenConversation(ctx, []string{"U1"})
			return err
		},
		"SendReply": func(ctx context.Context, c *Client) error {
			_, _, err := c.SendReply(ctx, "C1", "1.0", "hi", false)
			return err
		},
		"GetReplies": func(ctx context.Context, c *Client) error { _, err := c.GetReplies(ctx, "C1", "1.0"); return err },
		"EditMessage": func(ctx context.Context, c *Client) error {
			_, err := c.EditMessage(ctx, "C1", "1.0", "hi")
			return err
		},
		"RemoveMessage": func(ctx context.Context, c *Client) error {
			return c.RemoveMessage(ctx, "C1", "1.0")
		},
		"AddReaction":     func(ctx context.Context, c *Client) error { return c.AddReaction(ctx, "C1", "1.0", "x") },
		"RemoveReaction":  func(ctx context.Context, c *Client) error { return c.RemoveReaction(ctx, "C1", "1.0", "x") },
		"SetUserPresence": func(ctx context.Context, c *Client) error { return c.SetUserPresence(ctx, "away") },
		"GetUserPresence": func(ctx context.Context, c *Client) error { _, err := c.GetUserPresence(ctx, "U1"); return err },
		"SetSnooze":       func(ctx context.Context, c *Client) error { _, err := c.SetSnooze(ctx, 10); return err },
		"EndSnooze":       func(ctx context.Context, c *Client) error { _, err := c.EndSnooze(ctx); return err },
		"EndDND":          func(ctx context.Context, c *Client) error { return c.EndDND(ctx) },
		"GetDNDInfo":      func(ctx context.Context, c *Client) error { _, err := c.GetDNDInfo(ctx, "U1"); return err },
		"GetDNDTeamInfo": func(ctx context.Context, c *Client) error {
			_, err := c.GetDNDTeamInfo(ctx, []string{"U1"})
			return err
		},
		"GetPermalink": func(ctx context.Context, c *Client) error { _, err := c.GetPermalink(ctx, "C1", "1.0"); return err },
		"MarkChannel":  func(ctx context.Context, c *Client) error { return c.MarkChannel(ctx, "C1", "1.0") },
		"MarkThread":   func(ctx context.Context, c *Client) error { return c.MarkThread(ctx, "C1", "1.0", "2.0") },
		"MarkChannelUnread": func(ctx context.Context, c *Client) error {
			return c.MarkChannelUnread(ctx, "C1", "1.0")
		},
		"MarkThreadUnread": func(ctx context.Context, c *Client) error {
			return c.MarkThreadUnread(ctx, "C1", "1.0", "2.0")
		},
		"GetMutedChannels":   func(ctx context.Context, c *Client) error { _, err := c.GetMutedChannels(ctx); return err },
		"ShouldReload":       func(ctx context.Context, c *Client) error { _, err := c.ShouldReload(ctx); return err },
		"PostForm":           func(ctx context.Context, c *Client) error { _, err := c.PostForm(ctx, "x.y", url.Values{}); return err },
		"GetChannelSections": func(ctx context.Context, c *Client) error { _, err := c.GetChannelSections(ctx); return err },
		"GetStarredConvs":    func(ctx context.Context, c *Client) error { _, err := c.GetStarredConversations(ctx); return err },
		"ListThreadSubs":     func(ctx context.Context, c *Client) error { _, err := c.ListThreadSubscriptions(ctx); return err },
		"HistoryWithVersions": func(ctx context.Context, c *Client) error {
			_, err := c.HistoryWithVersions(ctx, "C1", HistoryOpts{})
			return err
		},
		"ForwardMessage": func(ctx context.Context, c *Client) error {
			_, _, err := c.ForwardMessage(ctx, "C1", "1.0", "C2")
			return err
		},
		"WalkHistory": func(ctx context.Context, c *Client) error {
			return c.WalkHistory(ctx, "C1", "", "", func([]slack.Message) error { return nil })
		},
		"GetRepliesBetween": func(ctx context.Context, c *Client) error {
			_, err := c.GetRepliesBetween(ctx, "C1", "1.0", "", "")
			return err
		},
		"GetActivityFeed": func(ctx context.Context, c *Client) error {
			_, err := c.GetActivityFeed(ctx, 10, "", false)
			return err
		},
		"SetActivityRead": func(ctx context.Context, c *Client) error {
			return c.SetActivityRead(ctx, "message", "k", "1.0", true)
		},
		"AttachmentAction": func(ctx context.Context, c *Client) error {
			return c.AttachmentAction(ctx, AttachmentActionRequest{})
		},
		"RunCommand": func(ctx context.Context, c *Client) error { return c.RunCommand(ctx, CommandRequest{}) },
	}

	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- call(ctx, newClient()) }()
			select {
			case err := <-done:
				if err == nil {
					t.Errorf("%s returned nil against a server that never answers", name)
				}
			case <-time.After(5 * time.Second):
				t.Errorf("%s ignored its ctx: still waiting 5s after a 50ms deadline", name)
			}
		})
	}
}
