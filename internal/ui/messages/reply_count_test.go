package messages

import "testing"

// A reply Slack already counted when the parent was loaded (at or
// before its latest_reply) is not counted again, however late it
// arrives: a reply held while the user's send was pending is released
// after a refetch that already includes it. A newer reply counts once.
func TestIncrementReplyCountSkipsRepliesSlackCounted(t *testing.T) {
	m := New([]MessageItem{{TS: "100.000000", ReplyCount: 2, LatestReply: "105.000000"}}, "general")
	m.IncrementReplyCount("100.000000", "104.000000")
	m.IncrementReplyCount("100.000000", "105.000000")
	if n := m.Messages()[0].ReplyCount; n != 2 {
		t.Fatalf("ReplyCount = %d after replies Slack counted, want 2", n)
	}
	m.IncrementReplyCount("100.000000", "106.000000")
	m.IncrementReplyCount("100.000000", "106.000000")
	if n := m.Messages()[0].ReplyCount; n != 3 {
		t.Fatalf("ReplyCount = %d after a new reply twice, want 3", n)
	}
	// A reply past latest_reply that arrives after a newer one (held,
	// then released) was not counted by Slack and still counts.
	m.IncrementReplyCount("100.000000", "105.500000")
	if n := m.Messages()[0].ReplyCount; n != 4 {
		t.Fatalf("ReplyCount = %d after an out-of-order new reply, want 4", n)
	}
}

// A reply whose parent is not shown changes nothing, and is not
// remembered as counted: once the parent shows (released, loaded), the
// reply counts.
func TestIncrementReplyCountWithoutParentRemembersNothing(t *testing.T) {
	m := New(nil, "general")
	m.IncrementReplyCount("200.000000", "201.000000")
	m.AppendMessage(MessageItem{TS: "200.000000"})
	m.IncrementReplyCount("200.000000", "201.000000")
	if n := m.Messages()[0].ReplyCount; n != 1 {
		t.Fatalf("ReplyCount = %d, want 1", n)
	}
}
