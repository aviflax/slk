package slackclient

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"testing"
	"testing/synctest"
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

	// serve answers request n (0-based, from the cursor) of a paginated
	// method; hang makes page 2 never answer. Requests reach it in
	// memory, and each subtest runs in a synctest bubble, so the delays
	// and the bound are on a fake clock: the test does not depend on
	// how fast the machine is.
	serve := func(hang bool) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		})
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
			synctest.Test(t, func(t *testing.T) {
				client := func(h http.Handler) *Client {
					hc := handlerClient(h)
					return &Client{
						api:         slack.New("xoxc-test", slack.OptionAPIURL("http://slack.test/"), slack.OptionHTTPClient(hc)),
						pageTimeout: bound,
					}
				}
				if n, err := call(context.Background(), client(serve(false))); err != nil || n != pages {
					t.Errorf("slow pages: got %d items, err %v; want %d", n, err, pages)
				}
				// Without a per-request bound the hung page never returns
				// and synctest reports the bubble deadlocked.
				if _, err := call(context.Background(), client(serve(true))); err == nil {
					t.Error("a page that never answered succeeded")
				}
			})
		})
	}
}
