package main

import (
	"testing"

	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/mention"
	"github.com/gammons/slk/internal/proptest"
	"github.com/gammons/slk/internal/ui/sidebar"
	"pgregory.net/rapid"
)

func genCacheMessages(self string) *rapid.Generator[[]cache.Message] {
	msg := rapid.Custom(func(t *rapid.T) cache.Message {
		user := proptest.ID("U").Draw(t, "user")
		switch rapid.IntRange(0, 2).Draw(t, "who") {
		case 0:
			user = self
		case 1:
			user = ""
		}
		return cache.Message{UserID: user, Text: proptest.SlackText([]string{self}, nil).Draw(t, "text")}
	})
	return rapid.SliceOfN(msg, 0, 12)
}

// countMentions never counts self-authored messages, never exceeds the
// message count, counts every non-self message in dm/group_dm/app, and
// in channels counts exactly the non-self messages mention.InText flags.
func TestCountMentionsProperties(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		self := proptest.ID("U").Draw(t, "self")
		msgs := genCacheMessages(self).Draw(t, "msgs")
		chType := rapid.SampledFrom([]string{"channel", "private", "dm", "group_dm", "app"}).Draw(t, "type")

		got := countMentions(msgs, chType, self)
		var others, mentioning int
		for _, m := range msgs {
			if m.UserID == self {
				continue
			}
			others++
			if mention.InText(m.Text, self) {
				mentioning++
			}
		}
		if got < 0 || got > len(msgs) || got > others {
			t.Fatalf("count %d out of range (msgs %d, non-self %d)", got, len(msgs), others)
		}
		want := mentioning
		if chType == "dm" || chType == "group_dm" || chType == "app" {
			want = others
		}
		if got != want {
			t.Fatalf("countMentions(%s) = %d, want %d", chType, got, want)
		}
	})
}

type railFixture struct {
	unread  []cache.UnreadChannel
	teams   []string
	byID    func(string) *WorkspaceContext
	threads map[string]bool
}

func genRailFixture() *rapid.Generator[railFixture] {
	return rapid.Custom(func(t *rapid.T) railFixture {
		teams := rapid.SliceOfNDistinct(rapid.SampledFrom([]string{"T1", "T2", "T3", "T4"}), 1, 4, func(s string) string { return s }).Draw(t, "teams")
		ctxs := map[string]*WorkspaceContext{}
		for _, id := range teams {
			if rapid.Bool().Draw(t, "connected") {
				w := &WorkspaceContext{UserID: "USELF"}
				for _, c := range []string{"C1", "C2", "C3"} {
					if rapid.Bool().Draw(t, "has") {
						w.Channels = append(w.Channels, sidebar.ChannelItem{ID: c, IsMuted: rapid.Bool().Draw(t, "muted")})
					}
				}
				ctxs[id] = w
			}
		}
		var unread []cache.UnreadChannel
		for range rapid.IntRange(0, 8).Draw(t, "rows") {
			unread = append(unread, cache.UnreadChannel{
				WorkspaceID: rapid.SampledFrom(teams).Draw(t, "ws"),
				ChannelID:   rapid.SampledFrom([]string{"C1", "C2", "C3"}).Draw(t, "ch"),
				State:       core.ReadState{HasUnread: true},
			})
		}
		threads := map[string]bool{}
		for _, id := range teams {
			threads[id] = rapid.Bool().Draw(t, "threads")
		}
		return railFixture{unread, teams, func(id string) *WorkspaceContext { return ctxs[id] }, threads}
	})
}

// railUnreadWorkspaces has no duplicates and only names workspaces that
// appear in the input; it lights a workspace with a thread unread, and
// one whose workspace context is nil and which has an unread row.
func TestRailUnreadWorkspacesProperties(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		f := genRailFixture().Draw(t, "fixture")
		got := railUnreadWorkspaces(f.unread, f.teams, f.byID, func(team, _ string) bool { return f.threads[team] })
		inInput := map[string]bool{}
		for _, id := range f.teams {
			inInput[id] = true
		}
		for _, u := range f.unread {
			inInput[u.WorkspaceID] = true
		}
		seen := map[string]bool{}
		for _, id := range got {
			if seen[id] {
				t.Fatalf("duplicate %q in %v", id, got)
			}
			seen[id] = true
			if !inInput[id] {
				t.Fatalf("%q not in input", id)
			}
		}
		for _, id := range f.teams {
			if f.threads[id] && !seen[id] {
				t.Fatalf("workspace %q with unread threads not lit: %v", id, got)
			}
		}
		for _, u := range f.unread {
			if f.byID(u.WorkspaceID) == nil && !seen[u.WorkspaceID] {
				t.Fatalf("nil-context workspace %q with an unread row not lit: %v", u.WorkspaceID, got)
			}
		}
	})
}

// railRowLights agrees with a reference: unknown workspace lights; a
// channel the sidebar cannot show never lights; otherwise lit iff the
// row is unread and not muted.
func TestRailRowLightsMatchesReference(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		f := genRailFixture().Draw(t, "fixture")
		for _, u := range f.unread {
			w := f.byID(u.WorkspaceID)
			want := true
			if w != nil {
				want = false
				for _, c := range w.Channels {
					if c.ID == u.ChannelID {
						want = !c.IsMuted && u.State.HasUnread
					}
				}
			}
			if got := railRowLights(u, w); got != want {
				t.Fatalf("railRowLights(%+v) = %v, want %v", u, got, want)
			}
		}
	})
}
