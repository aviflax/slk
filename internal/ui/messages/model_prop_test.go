package messages

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// listMachine drives a messages.Model through its list-mutating API and
// keeps a reference: the TS order and which TS is selected. Messages
// arrive the way Slack delivers them: appends are newer than everything
// loaded, backfill is older, and an optimistic "local:N" placeholder
// sorts after every real TS.
type listMachine struct {
	m        *Model
	ts       []string
	selected string // TS of the selected message, "" when empty
	newest   int    // highest real TS number handed out
	oldest   int    // lowest real TS number handed out
	locals   int
}

func tsN(n int) string { return fmt.Sprintf("%010d.000100", n) }

func (s *listMachine) index(ts string) int { return slices.Index(s.ts, ts) }

func (s *listMachine) nextTS() string {
	s.newest++
	return tsN(s.newest)
}

func (s *listMachine) existing(t *rapid.T) (string, bool) {
	if len(s.ts) == 0 {
		return "", false
	}
	return rapid.SampledFrom(s.ts).Draw(t, "existing"), true
}

// insertReal puts a real TS into the reference where Insert puts it:
// in TS order among the real rows, ahead of the placeholders. It
// returns the index.
func (s *listMachine) insertReal(ts string) int {
	i := 0
	for j, x := range s.ts {
		if strings.HasPrefix(x, "local:") {
			continue
		}
		if x > ts {
			i = j
			break
		}
		i = j + 1
	}
	s.ts = slices.Insert(s.ts, i, ts)
	return i
}

func (s *listMachine) selectLast() {
	s.selected = ""
	if len(s.ts) > 0 {
		s.selected = s.ts[len(s.ts)-1]
	}
}

// atEnd reports whether the reference cursor is at the end: nothing
// selected, or no message Slack knows after the selected one.
func (s *listMachine) atEnd() bool {
	i := s.index(s.selected)
	if s.selected == "" || i < 0 {
		return true
	}
	return !slices.ContainsFunc(s.ts[i+1:], func(x string) bool { return !strings.HasPrefix(x, "local:") })
}

// remove applies a removal to the reference. Removing a message below
// the selection keeps the same message selected; removing the selected
// one leaves the choice to the model (checked only for range).
func (s *listMachine) remove(i int) (selectedGone bool) {
	gone := s.ts[i] == s.selected
	s.ts = slices.Delete(s.ts, i, i+1)
	return gone
}

func (s *listMachine) check(t *rapid.T) {
	got := s.m.Messages()
	gotTS := make([]string, len(got))
	for i, msg := range got {
		gotTS[i] = msg.TS
	}
	if !slices.Equal(gotTS, s.ts) {
		t.Fatalf("messages %v, want %v", gotTS, s.ts)
	}
	seen := map[string]bool{}
	for _, ts := range gotTS {
		if seen[ts] {
			t.Fatalf("TS %s twice in %v", ts, gotTS)
		}
		seen[ts] = true
	}
	sel, ok := s.m.SelectedMessage()
	if len(s.ts) == 0 {
		if ok {
			t.Fatalf("empty model selects %+v", sel)
		}
		return
	}
	idx := s.m.SelectedIndex()
	if idx < 0 || idx >= len(s.ts) || !ok {
		t.Fatalf("selected index %d out of range for %d messages", idx, len(s.ts))
	}
	if s.selected == "" {
		// The model chose; adopt its choice.
		s.selected = sel.TS
	}
	if sel.TS != s.selected {
		t.Fatalf("selected %s (index %d), want %s in %v", sel.TS, idx, s.selected, s.ts)
	}
}

// The channel pane's list operations keep the selection on a message
// (0 <= index < len whenever there are messages), never hold a TS twice,
// and keep the selected message selected when other rows come and go:
// backfill shifts the index by the rows it adds, and removing a row
// above the selection does not move it to another message.
func TestModelListOperations(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		s := &listMachine{newest: 5000, oldest: 5000}
		var initial []MessageItem
		for range rapid.IntRange(0, 4).Draw(t, "initial") {
			ts := s.nextTS()
			initial = append(initial, MessageItem{TS: ts})
			s.ts = append(s.ts, ts)
		}
		s.m = New(initial, "general")
		s.selectLast()
		s.check(t)

		t.Repeat(map[string]func(*rapid.T){
			"SetMessages": func(t *rapid.T) {
				var msgs []MessageItem
				s.ts = nil
				for range rapid.IntRange(0, 4).Draw(t, "n") {
					ts := s.nextTS()
					msgs = append(msgs, MessageItem{TS: ts})
					s.ts = append(s.ts, ts)
				}
				s.m.SetMessages(msgs)
				s.selectLast()
			},
			"AppendNew": func(t *rapid.T) {
				atEnd := s.atEnd()
				ts := s.nextTS()
				s.m.AppendMessage(MessageItem{TS: ts})
				s.insertReal(ts)
				if atEnd {
					s.selected = ts // the newest is followed by a cursor at the end
				}
			},
			"AppendOlder": func(t *rapid.T) {
				// A message held while a send was pending, released
				// after newer ones showed: it lands in TS order and
				// the cursor stays on its message.
				if s.newest-s.oldest < 2 {
					return
				}
				ts := tsN(rapid.IntRange(s.oldest+1, s.newest-1).Draw(t, "older"))
				if s.index(ts) >= 0 {
					return
				}
				atEnd := s.atEnd()
				s.m.AppendMessage(MessageItem{TS: ts})
				i := s.insertReal(ts)
				if atEnd && !slices.ContainsFunc(s.ts[i+1:], func(x string) bool { return !strings.HasPrefix(x, "local:") }) {
					s.selected = ts // newest after all: followed
				}
			},
			"AppendDuplicate": func(t *rapid.T) {
				if ts, ok := s.existing(t); ok {
					s.m.AppendMessage(MessageItem{TS: ts, Text: "echo"})
				}
			},
			"AppendPlaceholder": func(t *rapid.T) {
				s.locals++
				ts := fmt.Sprintf("local:%d", s.locals)
				s.m.InsertLocal(MessageItem{TS: ts})
				s.ts = append(s.ts, ts)
				s.selectLast() // the user's own send is selected
			},
			"Prepend": func(t *rapid.T) {
				// Mostly older rows, plus some a raced backfill would
				// deliver that the buffer already covers.
				var msgs []MessageItem
				n := rapid.IntRange(0, 4).Draw(t, "n")
				for i := range n {
					msgs = append(msgs, MessageItem{TS: tsN(s.oldest - n + i)})
				}
				s.oldest -= n
				if ts, ok := s.existing(t); ok && rapid.Bool().Draw(t, "stale") {
					msgs = append(msgs, MessageItem{TS: ts})
				}
				var kept []string
				for _, msg := range msgs {
					if (len(s.ts) == 0 || msg.TS < s.ts[0]) && s.index(msg.TS) < 0 {
						kept = append(kept, msg.TS)
					}
				}
				wasEmpty := len(s.ts) == 0
				before := s.m.SelectedIndex()
				s.m.PrependMessages(msgs)
				s.ts = append(kept, s.ts...)
				if wasEmpty {
					s.selected = "" // nothing was selected; any row will do
				} else if got := s.m.SelectedIndex(); got != before+len(kept) {
					t.Fatalf("prepend of %d moved index %d to %d", len(kept), before, got)
				}
			},
			"SettlePlaceholder": func(t *rapid.T) {
				var locals, reals []string
				for _, ts := range s.ts {
					if strings.HasPrefix(ts, "local:") {
						locals = append(locals, ts)
					} else {
						reals = append(reals, ts)
					}
				}
				if len(locals) == 0 {
					return
				}
				local := rapid.SampledFrom(locals).Draw(t, "local")
				i := s.index(local)
				// The real TS is new, or one an echo already appended.
				if len(reals) > 0 && rapid.Bool().Draw(t, "echoFirst") {
					real := rapid.SampledFrom(reals).Draw(t, "echoed")
					s.m.Settle(local, MessageItem{TS: real, Text: "settled"})
					// The placeholder goes and the row that already
					// showed the message takes its place under the
					// cursor: a selected placeholder hands the
					// selection to it.
					if s.remove(i) {
						s.selected = real
					}
					if j := s.index(real); j >= len(s.m.Messages()) || s.m.Messages()[j].Text != "settled" {
						t.Fatalf("echoed row %s was not settled: %+v", real, s.m.Messages())
					}
					return
				}
				// A new real TS: the message moves ahead of any
				// placeholder sent before it, taking the cursor with it.
				real := s.nextTS()
				s.m.Settle(local, MessageItem{TS: real})
				s.remove(i)
				s.insertReal(real)
				if s.selected == local {
					s.selected = real
				}
			},
			"RemovePlaceholder": func(t *rapid.T) {
				var locals []string
				for _, ts := range s.ts {
					if strings.HasPrefix(ts, "local:") {
						locals = append(locals, ts)
					}
				}
				if len(locals) == 0 {
					return
				}
				local := rapid.SampledFrom(locals).Draw(t, "local")
				if !s.m.RemoveLocalSent(local) {
					t.Fatalf("RemoveLocalSent(%s) found nothing", local)
				}
				if s.remove(s.index(local)) {
					s.selected = ""
				}
			},
			"UpsertExisting": func(t *rapid.T) {
				if ts, ok := s.existing(t); ok {
					s.m.UpsertSelfSent(MessageItem{TS: ts, Text: "ours"})
					if got := s.m.Messages()[s.index(ts)].Text; got != "ours" {
						t.Fatalf("upsert of %s kept text %q", ts, got)
					}
				}
			},
			"UpsertNew": func(t *rapid.T) {
				atEnd := s.atEnd()
				ts := s.nextTS()
				s.m.UpsertSelfSent(MessageItem{TS: ts})
				s.insertReal(ts)
				if atEnd {
					s.selected = ts
				}
			},
			"RemoveByTS": func(t *rapid.T) {
				ts, ok := s.existing(t)
				if !ok || rapid.Bool().Draw(t, "missing") {
					if s.m.RemoveMessageByTS(tsN(1)) {
						t.Fatalf("removed a TS that is not there")
					}
					return
				}
				if !s.m.RemoveMessageByTS(ts) {
					t.Fatalf("RemoveMessageByTS(%s) found nothing", ts)
				}
				if s.remove(s.index(ts)) {
					s.selected = ""
				}
			},
			"IncrementReplyCount": func(t *rapid.T) {
				ts, ok := s.existing(t)
				if !ok {
					return
				}
				reply := tsN(rapid.IntRange(1, 3).Draw(t, "reply"))
				s.m.IncrementReplyCount(ts, reply)
				after := s.m.Messages()[s.index(ts)].ReplyCount
				s.m.IncrementReplyCount(ts, reply)
				if again := s.m.Messages()[s.index(ts)].ReplyCount; again != after {
					t.Fatalf("second count of reply %s on %s: %d -> %d", reply, ts, after, again)
				}
			},
			"SelectByTS": func(t *rapid.T) {
				if ts, ok := s.existing(t); ok {
					s.m.SelectByTS(ts)
					s.selected = ts
				}
			},
			"MoveUp": func(t *rapid.T) {
				s.m.MoveUp()
				if i := s.index(s.selected); i > 0 {
					s.selected = s.ts[i-1]
				}
			},
			"MoveDown": func(t *rapid.T) {
				s.m.MoveDown()
				if i := s.index(s.selected); i >= 0 && i < len(s.ts)-1 {
					s.selected = s.ts[i+1]
				}
			},
			"": func(t *rapid.T) { s.check(t) },
		})
	})
}
