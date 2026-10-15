package slackclient

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/slack-go/slack"
)

// Paginated calls bound each request, not the whole call. A long
// thread or channel on a slow link takes many pages, each well inside
// the bound, and must finish however long they take together; a page
// that hangs must still fail, once its own request has taken the
// bound, even when the caller's ctx has no deadline.
func TestPaginatedCallsBoundEachRequest(t *testing.T) {
	const pages, bound = 6, 150 * time.Millisecond
	const pageDelay = bound / 3 // six pages: twice the bound in all

	// page answers request n (0-based, from the cursor) of a paginated
	// method; hang makes page 2 never answer.
	serve := func(hang bool) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = r.ParseForm()
			n, _ := strconv.Atoi(r.Form.Get("cursor"))
			if hang && n == 2 {
				<-r.Context().Done()
				return
			}
			time.Sleep(pageDelay)
			next, more := "", n < pages-1
			if more {
				next = strconv.Itoa(n + 1)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"ok":true,"has_more":%v,"response_metadata":{"next_cursor":%q},
				"messages":[{"type":"message","ts":"%d.000000","text":"m"}],
				"channels":[{"id":"C%d"}],"members":["U%d"]}`, more, next, n+1, n, n)
		}))
	}
	calls := map[string]func(ctx context.Context, c *Client) (int, error){
		"GetReplies": func(ctx context.Context, c *Client) (int, error) {
			ms, err := c.GetReplies(ctx, "C1", "1.0")
			return len(ms), err
		},
		"GetRepliesBetween": func(ctx context.Context, c *Client) (int, error) {
			ms, err := c.GetRepliesBetween(ctx, "C1", "1.0", "", "")
			return len(ms), err
		},
		"WalkHistory": func(ctx context.Context, c *Client) (int, error) {
			n := 0
			err := c.WalkHistory(ctx, "C1", "", "", func(ms []slack.Message) error { n += len(ms); return nil })
			return n, err
		},
		"GetHistorySince": func(ctx context.Context, c *Client) (int, error) {
			r, err := c.GetHistorySince(ctx, "C1", "0.5", 1000)
			return len(r.Messages), err
		},
		"GetChannels": func(ctx context.Context, c *Client) (int, error) {
			chs, err := c.GetChannels(ctx)
			return len(chs), err
		},
		"GetUsersInConversation": func(ctx context.Context, c *Client) (int, error) {
			us, err := c.GetUsersInConversation(ctx, "C1")
			return len(us), err
		},
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			client := func(srv *httptest.Server) *Client {
				return &Client{
					api:         slack.New("xoxc-test", slack.OptionAPIURL(srv.URL+"/"), slack.OptionHTTPClient(srv.Client())),
					pageTimeout: bound,
				}
			}
			slow := serve(false)
			defer slow.Close()
			if n, err := call(context.Background(), client(slow)); err != nil || n != pages {
				t.Errorf("slow pages: got %d items, err %v; want %d", n, err, pages)
			}

			hung := serve(true)
			defer hung.Close()
			done := make(chan error, 1)
			go func() { _, err := call(context.Background(), client(hung)); done <- err }()
			select {
			case err := <-done:
				if err == nil {
					t.Error("a page that never answered succeeded")
				}
			case <-time.After(20 * bound):
				t.Error("a page that never answered hung the call: no per-request bound")
				hung.CloseClientConnections()
				<-done
			}
		})
	}
}
