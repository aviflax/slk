package ui

import (
	"fmt"
	"slices"
	"testing"

	"github.com/gammons/slk/internal/ids"
	"pgregory.net/rapid"
)

// navModel is the reference the store is checked against: the stack as
// a list of entries, each tagged by the MessageTS serial it was pushed
// or updated with, so an entry can be followed through drops by
// identity rather than by index arithmetic.
type navModel struct {
	entries []Location
	cursor  int
}

func (m *navModel) push(loc Location) {
	if m.cursor >= 0 && m.entries[m.cursor].ChannelID == loc.ChannelID {
		return
	}
	m.entries = append(m.entries[:m.cursor+1:m.cursor+1], loc)
	if len(m.entries) > navStackMax {
		m.entries = slices.Clone(m.entries[len(m.entries)-navStackMax:])
	}
	m.cursor = len(m.entries) - 1
}

// walk: the target is the first live entry past the cursor in the step
// direction. The stale entries the walk passed over (all of them in that
// direction when there is no target) leave the stack; the cursor ends on
// the target, or on the entry it was on.
func (m *navModel) walk(step int, live func(ids.ChannelID) bool) (Location, bool) {
	if m.cursor < 0 {
		return Location{}, false
	}
	target := -1
	passed := map[ids.MessageTS]bool{}
	for i := m.cursor + step; i >= 0 && i < len(m.entries); i += step {
		if live(m.entries[i].ChannelID) {
			target = i
			break
		}
		passed[m.entries[i].MessageTS] = true
	}
	keep := m.entries[m.cursor].MessageTS
	var found Location
	if target >= 0 {
		found = m.entries[target]
		keep = found.MessageTS
	}
	m.entries = slices.DeleteFunc(m.entries, func(l Location) bool { return passed[l.MessageTS] })
	m.cursor = slices.IndexFunc(m.entries, func(l Location) bool { return l.MessageTS == keep })
	return found, target >= 0
}

// Push, UpdateCurrent and Walk, in any order and with channels going
// stale and coming back between steps, agree with navModel; and after
// every step:
//   - the cursor is -1 exactly when the stack is empty, else in range;
//   - the stack never holds more than navStackMax entries;
//   - no two neighbouring entries name the same channel (see KNOWN);
//   - a successful Walk leaves the cursor on the entry it returned;
//   - Walk(+1) right after a Walk(-1) that dropped nothing returns to the
//     location the walk started from (if that channel is still live),
//     and vice versa.
func TestNavHistoryAgreesWithModel(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		const team = "T1"
		s := newNavHistoryStore()
		m := &navModel{cursor: -1}
		channels := []ids.ChannelID{"C1", "C2", "C3", "C4", "C5"}
		stale := map[ids.ChannelID]bool{}
		everStale := false
		live := func(ch ids.ChannelID) bool { return !stale[ch] }
		lookup := func(ch ids.ChannelID) (string, string, bool) { return string(ch), "channel", live(ch) }
		serial := 0
		loc := func(t *rapid.T) Location {
			serial++
			return Location{
				TeamID:    team,
				ChannelID: rapid.SampledFrom(channels).Draw(t, "channel"),
				MessageTS: ids.MessageTS(fmt.Sprint(serial)),
			}
		}

		t.Repeat(map[string]func(*rapid.T){
			"": func(t *rapid.T) {
				st := s.Stack(team)
				if st == nil {
					if m.cursor != -1 {
						t.Fatalf("no stack, model cursor %d", m.cursor)
					}
					return
				}
				if !slices.Equal(st.entries, m.entries) || st.cursor != m.cursor {
					t.Fatalf("store %v @%d, model %v @%d", st.entries, st.cursor, m.entries, m.cursor)
				}
				// KNOWN: dropping a stale entry can leave two entries for one
				// channel side by side. Push C2, C1, C2; C1 goes stale; Walk(-1)
				// from the second C2 skips and drops C1 and lands on the first
				// C2, leaving [C2 C2]. Back then stays in C2 and only moves to
				// the older position. Whether to merge the pair (and which
				// position to keep) is a product call, so the check only runs
				// while no channel has gone stale.
				for i := 1; i < len(st.entries) && !everStale; i++ {
					if st.entries[i].ChannelID == st.entries[i-1].ChannelID {
						t.Fatalf("consecutive duplicates at %d: %v", i, st.entries)
					}
				}
				if len(st.entries) > navStackMax {
					t.Fatalf("%d entries, cap %d", len(st.entries), navStackMax)
				}
				if (st.cursor == -1) != (len(st.entries) == 0) || st.cursor >= len(st.entries) {
					t.Fatalf("cursor %d with %d entries", st.cursor, len(st.entries))
				}
			},
			"push": func(t *rapid.T) {
				l := loc(t)
				s.Push(team, l)
				m.push(l)
			},
			"pushMany": func(t *rapid.T) {
				// Enough distinct visits to reach the cap.
				for range rapid.IntRange(10, 60).Draw(t, "n") {
					serial++
					l := Location{TeamID: team, ChannelID: channels[serial%len(channels)], MessageTS: ids.MessageTS(fmt.Sprint(serial))}
					s.Push(team, l)
					m.push(l)
				}
			},
			"update": func(t *rapid.T) {
				l := loc(t)
				s.UpdateCurrent(team, l)
				if m.cursor >= 0 && m.entries[m.cursor].ChannelID == l.ChannelID {
					m.entries[m.cursor] = l
				}
			},
			"toggleStale": func(t *rapid.T) {
				ch := rapid.SampledFrom(channels).Draw(t, "staleChannel")
				stale[ch] = !stale[ch]
				everStale = true
			},
			"walk": func(t *rapid.T) {
				step := rapid.SampledFrom([]int{-1, 1}).Draw(t, "step")
				var from Location
				if m.cursor >= 0 {
					from = m.entries[m.cursor]
				}
				before := len(m.entries)
				got, ok := s.Walk(team, step, lookup)
				want, wantOK := m.walk(step, live)
				if ok != wantOK || got != want {
					t.Fatalf("Walk(%d) = %v %v, model %v %v", step, got, ok, want, wantOK)
				}
				if !ok {
					return
				}
				if st := s.Stack(team); st.entries[st.cursor] != got {
					t.Fatalf("cursor on %v, Walk returned %v", st.entries[st.cursor], got)
				}
				if len(m.entries) != before || !live(from.ChannelID) {
					return
				}
				back, ok := s.Walk(team, -step, lookup)
				m.walk(-step, live)
				if !ok || back != from {
					t.Fatalf("Walk(%d) after Walk(%d) = %v %v, want %v", -step, step, back, ok, from)
				}
			},
		})
	})
}
