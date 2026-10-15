package messages

import (
	"testing"

	"github.com/gammons/slk/internal/proptest"
	"pgregory.net/rapid"
)

// UnreadStart is where the "── new ──" line goes. Property: it is -1
// or a row Slack knows, by someone else, later than the read cursor,
// with none of the user's own rows (nor placeholders) at or after it;
// and no earlier row would do. Sending counts as reading: nothing the
// user posted, and nothing before it, is unread.
func TestUnreadStartProperty(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		rows := drawRows(t)
		for i := range rows {
			rows[i].UserID = rapid.SampledFrom([]string{"USELF", "UOTHER"}).Draw(t, "user")
			if IsLocalTS(rows[i].TS) {
				rows[i].UserID = "USELF"
			}
		}
		lastRead := proptest.TS().Draw(t, "lastRead")
		own := func(r MessageItem) bool { return r.UserID == "USELF" || IsLocalTS(r.TS) }
		lastOwn := LastIndexBefore(rows, len(rows), own)
		eligible := func(i int) bool {
			return i > lastOwn && KnownToSlack(rows[i]) && CompareTS(rows[i].TS, lastRead) > 0
		}
		got := UnreadStart(rows, lastRead, "USELF")
		want := -1
		for i := range rows {
			if eligible(i) {
				want = i
				break
			}
		}
		if got != want {
			t.Fatalf("UnreadStart = %d, want %d (rows %+v, lastRead %s)", got, want, rows, lastRead)
		}
		if UnreadStart(rows, "", "USELF") != -1 {
			t.Fatal("no read cursor, yet an unread start")
		}
	})
}
