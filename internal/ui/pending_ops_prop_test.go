package ui

import (
	"fmt"
	"reflect"
	"slices"
	"testing"

	"pgregory.net/rapid"

	"github.com/gammons/slk/internal/ui/messages"
)

// However sends in two conversations begin, echo and end (in any
// order, a send's echo before or after its result, sends failing with
// or without Slack having posted them, and the user's messages from
// other clients among them), each message is shown exactly once: a
// successful send by its result, never by its echo, and anything else
// by its echo, once released. A held message is never shown while a
// send is pending in its conversation, and once every send has ended
// nothing is held and no placeholder is pending.
func TestHoldAndReleaseShowsEachMessageOnce(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		p := newPendingOps()
		convs := []conversationKey{{"", "C1", ""}, {"", "C1", "5.000000"}}
		type send struct {
			ins      pendingInsert
			real     string
			ended    bool
			ok       bool
			echoed   bool
			posts    bool // Slack posted it (true unless it failed cleanly)
			resulted bool
		}
		var sends []*send
		var foreign []string
		shown := map[string]string{} // TS -> "result" or "echo"
		next := 0
		newTS := func() string { next++; return fmt.Sprintf("%010d.000000", next) }
		show := func(ts, by string) {
			if prev, ok := shown[ts]; ok {
				t.Fatalf("%s shown twice (by %s, then %s)", ts, prev, by)
			}
			shown[ts] = by
		}
		// deliver is reduceNewMessage's decision for one of the user's
		// messages.
		deliver := func(k conversationKey, ts string) {
			msg := NewMessageMsg{ChannelID: k.channelID, Message: messages.MessageItem{TS: ts, ThreadTS: k.threadTS}}
			if p.IsSelfSent(ts) || p.HoldEcho(msg) {
				return
			}
			show(ts, "echo")
		}
		pendingIn := func(k conversationKey) bool {
			return slices.ContainsFunc(sends, func(s *send) bool { return !s.ended && s.ins.conversation() == k })
		}
		for range rapid.IntRange(1, 40).Draw(t, "steps") {
			switch rapid.IntRange(0, 3).Draw(t, "action") {
			case 0: // begin
				k := rapid.SampledFrom(convs).Draw(t, "conv")
				s := &send{ins: pendingInsert{localTS: p.NextLocalTS(), channelID: k.channelID, threadTS: k.threadTS}, real: newTS()}
				p.BeginInsert(s.ins)
				sends = append(sends, s)
			case 1: // the echo of a send Slack posted
				var cands []*send
				for _, s := range sends {
					if !s.echoed && (!s.resulted || s.posts) {
						cands = append(cands, s)
					}
				}
				if len(cands) == 0 {
					continue
				}
				s := rapid.SampledFrom(cands).Draw(t, "echo")
				s.echoed, s.posts = true, true
				deliver(s.ins.conversation(), s.real)
			case 2: // a message from another client
				ts := newTS()
				foreign = append(foreign, ts)
				deliver(rapid.SampledFrom(convs).Draw(t, "conv"), ts)
			case 3: // a result
				var open []*send
				for _, s := range sends {
					if !s.ended {
						open = append(open, s)
					}
				}
				if len(open) == 0 {
					continue
				}
				s := rapid.SampledFrom(open).Draw(t, "result")
				s.ended, s.resulted = true, true
				s.ok = rapid.Bool().Draw(t, "ok")
				sentTS := ""
				if s.ok {
					sentTS, s.posts = s.real, true
				} else if !s.echoed {
					s.posts = rapid.Bool().Draw(t, "posted anyway")
				}
				ins, release, ok := p.EndInsert(s.ins.localTS, sentTS)
				if !ok || !reflect.DeepEqual(ins, s.ins) {
					t.Fatalf("EndInsert(%s) = %+v %v", s.ins.localTS, ins, ok)
				}
				if s.ok {
					show(s.real, "result")
				}
				if len(release) > 0 && pendingIn(s.ins.conversation()) {
					t.Fatalf("released %v while a send is pending in %v", release, s.ins.conversation())
				}
				for _, m := range release {
					if conversationOf(m.TeamID, m.ChannelID, m.Message) != s.ins.conversation() {
						t.Fatalf("released %+v for conversation %v", m, s.ins.conversation())
					}
					show(m.Message.TS, "echo")
				}
			}
		}
		// Every send ends, and every posted send's echo arrives.
		for _, s := range sends {
			if !s.ended {
				s.ended, s.ok = true, true
				_, release, _ := p.EndInsert(s.ins.localTS, s.real)
				show(s.real, "result")
				for _, m := range release {
					show(m.Message.TS, "echo")
				}
			}
		}
		for _, s := range sends {
			if s.posts && !s.echoed {
				deliver(s.ins.conversation(), s.real)
			}
		}
		for _, s := range sends {
			want := ""
			switch {
			case s.ok:
				want = "result"
			case s.posts:
				want = "echo"
			}
			if shown[s.real] != want {
				t.Fatalf("send %s (ok=%v posted=%v) shown by %q, want %q", s.real, s.ok, s.posts, shown[s.real], want)
			}
		}
		for _, ts := range foreign {
			if shown[ts] != "echo" {
				t.Fatalf("message %s from another client never shown", ts)
			}
		}
		if len(p.inserts) != 0 {
			t.Fatalf("inserts left: %+v", p.inserts)
		}
		for k, held := range p.held {
			if len(held) != 0 {
				t.Fatalf("held in %v after every send ended: %+v", k, held)
			}
		}
	})
}
