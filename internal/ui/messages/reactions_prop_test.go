package messages

import (
	"slices"
	"testing"

	"pgregory.net/rapid"
)

var (
	propEmojis = []string{"tada", "heart", "+1", "eyes"}
	propUsers  = []string{"U_SELF", "U_A", "U_B", "U_C"}
)

const propSelf = "U_SELF"

// reactionModel is the reference: per emoji, the set of reactors in
// first-reaction order, with emojis in first-reaction order.
type reactionModel struct {
	order []string
	users map[string][]string
}

func (m *reactionModel) apply(emoji, user string, remove bool) {
	us, ok := m.users[emoji]
	if remove {
		if !ok || !slices.Contains(us, user) {
			return
		}
		us = slices.DeleteFunc(slices.Clone(us), func(u string) bool { return u == user })
		if len(us) == 0 {
			delete(m.users, emoji)
			m.order = slices.DeleteFunc(m.order, func(e string) bool { return e == emoji })
			return
		}
		m.users[emoji] = us
		return
	}
	if !ok {
		m.order = append(m.order, emoji)
	}
	if !slices.Contains(us, user) {
		m.users[emoji] = append(slices.Clone(us), user)
	}
}

// Over any sequence of add/remove events on an untruncated reaction
// list, ApplyReaction agrees with a set-per-emoji reference: the same
// emojis in first-reaction order, the same reactors, Count equal to the
// reactor count, no empty entries, and HasReacted true exactly when the
// current user is among the reactors. Repeating an event (the optimistic
// update and its WebSocket echo) never changes the result.
func TestApplyReactionMatchesSetModel(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		var msg MessageItem
		model := reactionModel{users: map[string][]string{}}
		n := rapid.IntRange(1, 40).Draw(t, "events")
		for range n {
			emoji := rapid.SampledFrom(propEmojis).Draw(t, "emoji")
			user := rapid.SampledFrom(propUsers).Draw(t, "user")
			remove := rapid.Bool().Draw(t, "remove")
			ApplyReaction(&msg, emoji, user, propSelf, remove)
			once := cloneReactions(msg.Reactions)
			ApplyReaction(&msg, emoji, user, propSelf, remove)
			if !reactionsEqual(once, msg.Reactions) {
				t.Fatalf("echo changed reactions:\nonce  %+v\ntwice %+v", once, msg.Reactions)
			}
			model.apply(emoji, user, remove)

			if len(msg.Reactions) != len(model.order) {
				t.Fatalf("reactions %+v, model order %v", msg.Reactions, model.order)
			}
			for i, r := range msg.Reactions {
				want := model.users[model.order[i]]
				if r.Emoji != model.order[i] || !slices.Equal(r.UserIDs, want) {
					t.Fatalf("reaction %d = %+v, want %s by %v", i, r, model.order[i], want)
				}
				if r.Count != len(r.UserIDs) || r.Count <= 0 {
					t.Fatalf("reaction %+v: Count does not match reactors", r)
				}
				if r.HasReacted != slices.Contains(r.UserIDs, propSelf) {
					t.Fatalf("reaction %+v: HasReacted wrong", r)
				}
			}
		}
	})
}

// When Slack truncated the reactor list (Count > len(UserIDs)), a
// removal by an unlisted user is real and decrements Count; no entry is
// ever left with Count <= 0, and Count never drops below the listed
// reactors.
func TestApplyReactionTruncatedCounts(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		listed := rapid.SliceOfNDistinct(rapid.SampledFrom(propUsers[1:]), 0, 3, rapid.ID[string]).Draw(t, "listed")
		extra := rapid.IntRange(1, 5).Draw(t, "hidden")
		msg := MessageItem{Reactions: []ReactionItem{{Emoji: "tada", Count: len(listed) + extra, UserIDs: listed}}}
		for range rapid.IntRange(1, 12).Draw(t, "removals") {
			user := rapid.SampledFrom(append(slices.Clone(propUsers), "U_UNLISTED")).Draw(t, "user")
			before := cloneReactions(msg.Reactions)
			ApplyReaction(&msg, "tada", user, propSelf, true)
			if len(before) == 0 {
				if len(msg.Reactions) != 0 {
					t.Fatalf("removal from nothing produced %+v", msg.Reactions)
				}
				continue
			}
			b := before[0]
			listedUser := slices.Contains(b.UserIDs, user)
			wantCount := b.Count
			if listedUser || b.Count > len(b.UserIDs) {
				wantCount--
			}
			if wantCount <= 0 {
				if len(msg.Reactions) != 0 {
					t.Fatalf("count reached %d but entry kept: %+v", wantCount, msg.Reactions)
				}
				continue
			}
			r := msg.Reactions[0]
			if r.Count != wantCount || r.Count < len(r.UserIDs) {
				t.Fatalf("remove %s from %+v gave %+v, want Count %d", user, b, r, wantCount)
			}
		}
	})
}

// ApplyReaction must not write through to another MessageItem that
// shares msg's Reactions slice. The thread pane's parent is a shallow
// copy of the channel pane's row, and both apply every reaction event;
// a write through the shared backing array changes the other pane's row
// behind its back (and, for a removal, shifts its pills so one shows
// twice).
func TestApplyReactionDoesNotWriteThroughCopies(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		var msg MessageItem
		for range rapid.IntRange(0, 10).Draw(t, "setup") {
			ApplyReaction(&msg, rapid.SampledFrom(propEmojis).Draw(t, "emoji"), rapid.SampledFrom(propUsers).Draw(t, "user"), propSelf, false)
		}
		other := msg // shallow copy, as SetThread receives it
		snapshot := cloneReactions(other.Reactions)
		ApplyReaction(&msg, rapid.SampledFrom(propEmojis).Draw(t, "emoji"), rapid.SampledFrom(propUsers).Draw(t, "user"), propSelf, rapid.Bool().Draw(t, "remove"))
		if !reactionsEqual(snapshot, other.Reactions) {
			t.Fatalf("copy changed:\nbefore %+v\nafter  %+v", snapshot, other.Reactions)
		}
	})
}

// AppendUserID and RemoveUserID never modify their input, are
// idempotent, and are inverses for a user not already present.
func TestUserIDSetOps(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		ids := rapid.SliceOfNDistinct(rapid.SampledFrom(propUsers), 0, 4, rapid.ID[string]).Draw(t, "ids")
		user := rapid.SampledFrom(append(slices.Clone(propUsers), "")).Draw(t, "user")
		orig := slices.Clone(ids)

		added := AppendUserID(ids, user)
		removed := RemoveUserID(ids, user)
		if !slices.Equal(ids, orig) {
			t.Fatalf("input mutated: %v -> %v", orig, ids)
		}
		if !slices.Equal(AppendUserID(added, user), added) || !slices.Equal(RemoveUserID(removed, user), removed) {
			t.Fatalf("not idempotent for %q on %v", user, ids)
		}
		if user != "" {
			if !slices.Contains(added, user) || slices.Contains(removed, user) {
				t.Fatalf("append/remove %q on %v gave %v / %v", user, ids, added, removed)
			}
			if !slices.Contains(ids, user) && !slices.Equal(RemoveUserID(added, user), ids) {
				t.Fatalf("remove(append(%v, %q)) = %v", ids, user, RemoveUserID(added, user))
			}
		}
	})
}

// The pill cursor stays on a pill (0..n, n being "+") whatever moves are
// made, Left undoes Right, and Clamp keeps it there after reactions go.
func TestReactionNavStaysInRange(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(0, 6).Draw(t, "n")
		nav := ReactionNav{Active: true}
		for range rapid.IntRange(0, 30).Draw(t, "moves") {
			before := nav.Index
			switch rapid.IntRange(0, 2).Draw(t, "move") {
			case 0:
				nav.Left(n)
				nav.Right(n)
				if nav.Index != before {
					t.Fatalf("Left then Right moved %d to %d (n=%d)", before, nav.Index, n)
				}
			case 1:
				nav.Right(n)
			case 2:
				n = rapid.IntRange(0, 6).Draw(t, "newN")
				nav.Clamp(n)
			}
			if nav.Index < 0 || nav.Index > n {
				t.Fatalf("index %d out of 0..%d", nav.Index, n)
			}
		}
		rs := make([]ReactionItem, n)
		emoji, isPlus := nav.Selected(rs)
		if isPlus != (nav.Index == n) || (!isPlus && emoji != rs[nav.Index].Emoji) {
			t.Fatalf("Selected at %d of %d = %q, %v", nav.Index, n, emoji, isPlus)
		}
	})
}

func cloneReactions(rs []ReactionItem) []ReactionItem {
	out := make([]ReactionItem, len(rs))
	for i, r := range rs {
		r.UserIDs = slices.Clone(r.UserIDs)
		out[i] = r
	}
	return out
}

func reactionsEqual(a, b []ReactionItem) bool {
	return slices.EqualFunc(a, b, func(x, y ReactionItem) bool {
		return x.Emoji == y.Emoji && x.Count == y.Count && x.HasReacted == y.HasReacted && slices.Equal(x.UserIDs, y.UserIDs)
	})
}
