package selection

import (
	"testing"

	"pgregory.net/rapid"
)

func anchors() *rapid.Generator[Anchor] {
	return rapid.Custom(func(t *rapid.T) Anchor {
		return Anchor{
			MessageID: rapid.SampledFrom([]string{"", "1700000000.000001", "1700000000.000002", "1700000001.000000"}).Draw(t, "id"),
			Line:      rapid.IntRange(0, 3).Draw(t, "line"),
			Col:       rapid.IntRange(0, 3).Draw(t, "col"),
		}
	})
}

// LessOrEqual is a total order: reflexive, antisymmetric (a<=b and b<=a
// mean equal), total, and transitive.
func TestLessOrEqualIsTotalOrder(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		a, b, c := anchors().Draw(t, "a"), anchors().Draw(t, "b"), anchors().Draw(t, "c")
		if !LessOrEqual(a, a) {
			t.Fatal("not reflexive")
		}
		if LessOrEqual(a, b) && LessOrEqual(b, a) && a != b {
			t.Fatalf("not antisymmetric: %v %v", a, b)
		}
		if !LessOrEqual(a, b) && !LessOrEqual(b, a) {
			t.Fatalf("not total: %v %v", a, b)
		}
		if LessOrEqual(a, b) && LessOrEqual(b, c) && !LessOrEqual(a, c) {
			t.Fatalf("not transitive: %v %v %v", a, b, c)
		}
	})
}

// Normalize returns the same two endpoints in order, the same result
// whichever way round the range was built, and is idempotent.
func TestNormalize(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		a, b := anchors().Draw(t, "a"), anchors().Draw(t, "b")
		r := Range{Start: a, End: b, Active: rapid.Bool().Draw(t, "active")}
		lo, hi := r.Normalize()
		if !LessOrEqual(lo, hi) {
			t.Fatalf("lo %v > hi %v", lo, hi)
		}
		if !((lo == a && hi == b) || (lo == b && hi == a)) {
			t.Fatal("Normalize changed the endpoints")
		}
		lo2, hi2 := Range{Start: b, End: a}.Normalize()
		if lo2 != lo || hi2 != hi {
			t.Fatalf("order of construction matters: %v,%v vs %v,%v", lo, hi, lo2, hi2)
		}
		lo3, hi3 := Range{Start: lo, End: hi}.Normalize()
		if lo3 != lo || hi3 != hi {
			t.Fatal("not idempotent")
		}
	})
}

// Contains is the half-open interval [lo, hi): lo is in (unless the
// range is empty), hi is out, an empty range contains nothing, and a
// range is symmetric in its endpoints.
func TestContainsHalfOpen(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		a, b, x := anchors().Draw(t, "a"), anchors().Draw(t, "b"), anchors().Draw(t, "x")
		r := Range{Start: a, End: b}
		lo, hi := r.Normalize()
		if r.Contains(hi) {
			t.Fatal("contains hi")
		}
		if r.Contains(lo) == r.IsEmpty() {
			t.Fatalf("Contains(lo)=%v with IsEmpty=%v", r.Contains(lo), r.IsEmpty())
		}
		if r.IsEmpty() && r.Contains(x) {
			t.Fatal("empty range contains something")
		}
		if want := LessOrEqual(lo, x) && !LessOrEqual(hi, x) && lo != hi; r.Contains(x) != want {
			t.Fatalf("Contains(%v) = %v, want %v for [%v,%v)", x, r.Contains(x), want, lo, hi)
		}
		if (Range{Start: b, End: a}).Contains(x) != r.Contains(x) {
			t.Fatal("Contains depends on endpoint order")
		}
	})
}
