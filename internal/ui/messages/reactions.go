package messages

import "slices"

// ApplyReaction applies one reaction event to msg, idempotently: the
// optimistic local update and the WebSocket echo of the same event must
// not double-count. A removal by a user not among the listed reactors is
// ignored, unless Slack truncated the list (Count > len(UserIDs)), in
// which case it is real. HasReacted tracks currentUserID only.
//
// msg.Reactions is replaced, never written in place: the thread pane's
// parent is a shallow copy of the channel pane's row, so the two share
// a backing array, and both apply every event.
func ApplyReaction(msg *MessageItem, emojiName, userID, currentUserID string, remove bool) {
	if remove {
		for j, r := range msg.Reactions {
			if r.Emoji != emojiName {
				continue
			}
			newIDs := RemoveUserID(r.UserIDs, userID)
			if len(newIDs) == len(r.UserIDs) && r.Count <= len(r.UserIDs) {
				return
			}
			r.UserIDs = newIDs
			r.Count--
			if userID == currentUserID {
				r.HasReacted = false
			}
			if r.Count <= 0 {
				msg.Reactions = slices.Delete(slices.Clone(msg.Reactions), j, j+1)
			} else {
				msg.Reactions = slices.Clone(msg.Reactions)
				msg.Reactions[j] = r
			}
			return
		}
		return
	}
	for j, r := range msg.Reactions {
		if r.Emoji != emojiName {
			continue
		}
		newIDs := AppendUserID(r.UserIDs, userID)
		if len(newIDs) != len(r.UserIDs) {
			r.UserIDs = newIDs
			r.Count++
		}
		if userID == currentUserID {
			r.HasReacted = true
		}
		msg.Reactions = slices.Clone(msg.Reactions)
		msg.Reactions[j] = r
		return
	}
	msg.Reactions = append(slices.Clip(msg.Reactions), ReactionItem{
		Emoji:      emojiName,
		Count:      1,
		HasReacted: userID == currentUserID,
		UserIDs:    AppendUserID(nil, userID),
	})
}

// ReactionNav is the reaction-pill cursor a pane keeps for its selected
// message. Index counts pills; index n (the number of reactions) is the
// trailing "+" pill. Invalidation stays with the pane.
type ReactionNav struct {
	Active bool
	Index  int
}

// Left moves one pill left over n reactions plus "+", wrapping.
func (r *ReactionNav) Left(n int) { total := n + 1; r.Index = (r.Index - 1 + total) % total }

// Right moves one pill right over n reactions plus "+", wrapping.
func (r *ReactionNav) Right(n int) { total := n + 1; r.Index = (r.Index + 1) % total }

// Clamp pulls the index back onto the "+" pill when reactions went away.
func (r *ReactionNav) Clamp(n int) {
	if total := n + 1; r.Index >= total {
		r.Index = total - 1
	}
}

// Selected returns the highlighted reaction, or isPlus for the "+" pill.
func (r ReactionNav) Selected(rs []ReactionItem) (emoji string, isPlus bool) {
	if r.Index >= len(rs) {
		return "", true
	}
	return rs[r.Index].Emoji, false
}
