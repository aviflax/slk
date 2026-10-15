package image

import (
	"testing"

	"pgregory.net/rapid"
)

// normalizeSiblings always returns count >= 1 and 0 <= idx < count, keeps
// an in-range pair as it was, and is idempotent.
func TestNormalizeSiblings(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		c := rapid.IntRange(-1000, 1000).Draw(t, "count")
		i := rapid.IntRange(-1000, 1000).Draw(t, "idx")
		count, idx := normalizeSiblings(c, i)
		if count < 1 || idx < 0 || idx >= count {
			t.Fatalf("normalizeSiblings(%d, %d) = %d, %d", c, i, count, idx)
		}
		if c >= 1 && i >= 0 && i < c && (count != c || idx != i) {
			t.Fatalf("in-range pair (%d, %d) changed to (%d, %d)", c, i, count, idx)
		}
		if c2, i2 := normalizeSiblings(count, idx); c2 != count || i2 != idx {
			t.Fatalf("not idempotent: (%d, %d) -> (%d, %d)", count, idx, c2, i2)
		}
	})
}
