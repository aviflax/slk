package wintree

import (
	"errors"
	"slices"
	"testing"

	"pgregory.net/rapid"
)

// checkTiling asserts the leaf rects tile bounds exactly: each rect lies
// inside bounds, no two overlap, and their areas sum to the bounds' area
// (so there is no gap either).
func checkTiling(t *rapid.T, tr *Tree, bounds Rect) map[LeafID]Rect {
	rects := tr.ComputeRects(bounds)
	if len(rects) != len(tr.Leaves()) {
		t.Fatalf("%d rects for %d leaves", len(rects), len(tr.Leaves()))
	}
	area := 0
	ids := tr.Leaves()
	for i, a := range ids {
		r := rects[a]
		if r.W <= 0 || r.H <= 0 || r.X < bounds.X || r.Y < bounds.Y ||
			r.X+r.W > bounds.X+bounds.W || r.Y+r.H > bounds.Y+bounds.H {
			t.Fatalf("leaf %d rect %+v outside bounds %+v", a, r, bounds)
		}
		area += r.W * r.H
		for _, b := range ids[i+1:] {
			s := rects[b]
			if overlap1D(r.X, r.W, s.X, s.W) > 0 && overlap1D(r.Y, r.H, s.Y, s.H) > 0 {
				t.Fatalf("leaves %d %+v and %d %+v overlap", a, r, b, s)
			}
		}
	}
	if area != bounds.W*bounds.H {
		t.Fatalf("areas sum to %d, want %d", area, bounds.W*bounds.H)
	}
	return rects
}

// onSide reports whether r lies entirely on side nd of cur.
func onSide(r, cur Rect, nd NavDir) bool {
	switch nd {
	case NavLeft:
		return r.X+r.W <= cur.X
	case NavRight:
		return r.X >= cur.X+cur.W
	case NavUp:
		return r.Y+r.H <= cur.Y
	default:
		return r.Y >= cur.Y+cur.H
	}
}

// touches reports whether r shares cur's edge on side nd.
func touches(r, cur Rect, nd NavDir) bool {
	switch nd {
	case NavLeft:
		return r.X+r.W == cur.X
	case NavRight:
		return r.X == cur.X+cur.W
	case NavUp:
		return r.Y+r.H == cur.Y
	default:
		return r.Y == cur.Y+cur.H
	}
}

// atEdge reports whether cur touches the bounds' edge on side nd.
func atEdge(cur, bounds Rect, nd NavDir) bool {
	switch nd {
	case NavLeft:
		return cur.X == bounds.X
	case NavRight:
		return cur.X+cur.W == bounds.X+bounds.W
	case NavUp:
		return cur.Y == bounds.Y
	default:
		return cur.Y+cur.H == bounds.Y+bounds.H
	}
}

// A random sequence of Split, Close, Only, Cycle and NavigateDir, run the
// way the App drives the tree (focus follows the returned window), keeps
// these invariants after every step:
//   - the leaf rects tile the bounds exactly, and every leaf meets the
//     minimum size (Split refuses what would break it);
//   - Len() == len(Leaves()), there is always at least one window, and
//     the focused window is one of them;
//   - Split's new id was never issued before, and a refused Split leaves
//     the leaves and their rects unchanged;
//   - Close of the last window is refused, otherwise it returns a
//     surviving window and removes exactly the closed one;
//   - Cycle by +1 Len() times returns to the start, and -1 undoes +1;
//   - NavigateDir returns a window entirely on the requested side, and
//     finds one exactly when the focused window is not on that edge of
//     the bounds (a guillotine tiling leaves no dead ends), and no other
//     window on that edge overlaps the focused one more.
func TestTreeStateMachine(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		bounds := Rect{
			X: rapid.IntRange(0, 5).Draw(t, "x"),
			Y: rapid.IntRange(0, 5).Draw(t, "y"),
			W: rapid.IntRange(MinWidth, 6*MinWidth).Draw(t, "w"),
			H: rapid.IntRange(MinHeight, 6*MinHeight).Draw(t, "h"),
		}
		tr, focus := New(Channel{ID: "C1"})
		issued := map[LeafID]bool{focus: true}

		pick := func(t *rapid.T) LeafID {
			return rapid.SampledFrom(tr.Leaves()).Draw(t, "leaf")
		}

		check := func(t *rapid.T) {
			rects := checkTiling(t, tr, bounds)
			for id, r := range rects {
				if r.W < MinWidth || r.H < MinHeight {
					t.Fatalf("leaf %d rect %+v below the minimum", id, r)
				}
			}
			if tr.Len() < 1 || tr.Len() != len(tr.Leaves()) {
				t.Fatalf("Len %d, Leaves %v", tr.Len(), tr.Leaves())
			}
			if !slices.Contains(tr.Leaves(), focus) {
				t.Fatalf("focus %d not among %v", focus, tr.Leaves())
			}
		}

		t.Repeat(map[string]func(*rapid.T){
			"": check,
			"split": func(t *rapid.T) {
				id := pick(t)
				dir := rapid.SampledFrom([]Dir{SplitStacked, SplitSideBySide}).Draw(t, "dir")
				before := tr.Leaves()
				beforeRects := tr.ComputeRects(bounds)
				nid, err := tr.Split(id, dir, bounds)
				switch {
				case errors.Is(err, ErrNoRoom):
					if !slices.Equal(tr.Leaves(), before) {
						t.Fatalf("refused split changed leaves %v -> %v", before, tr.Leaves())
					}
					after := tr.ComputeRects(bounds)
					for k, r := range beforeRects {
						if after[k] != r {
							t.Fatalf("refused split moved %d: %+v -> %+v", k, r, after[k])
						}
					}
				case err != nil:
					t.Fatalf("split %d: %v", id, err)
				default:
					if issued[nid] {
						t.Fatalf("split reused id %d", nid)
					}
					issued[nid] = true
					if tr.Len() != len(before)+1 {
						t.Fatalf("split: %d windows, want %d", tr.Len(), len(before)+1)
					}
					src, _ := tr.Channel(id)
					got, ok := tr.Channel(nid)
					if !ok || got != src {
						t.Fatalf("new window channel %+v, want clone %+v", got, src)
					}
					focus = nid
				}
			},
			"close": func(t *rapid.T) {
				id := pick(t)
				before := tr.Leaves()
				f, err := tr.Close(id)
				if len(before) == 1 {
					if !errors.Is(err, ErrLastWindow) {
						t.Fatalf("closing the last window: err %v", err)
					}
					return
				}
				if err != nil {
					t.Fatalf("close %d: %v", id, err)
				}
				want := slices.DeleteFunc(slices.Clone(before), func(l LeafID) bool { return l == id })
				if !slices.Equal(tr.Leaves(), want) {
					t.Fatalf("close %d: leaves %v, want %v", id, tr.Leaves(), want)
				}
				if !slices.Contains(tr.Leaves(), f) {
					t.Fatalf("close returned focus %d, not a window", f)
				}
				if id == focus {
					focus = f
				}
			},
			"only": func(t *rapid.T) {
				id := pick(t)
				ch, _ := tr.Channel(id)
				if err := tr.Only(id); err != nil {
					t.Fatalf("only %d: %v", id, err)
				}
				if got := tr.Leaves(); !slices.Equal(got, []LeafID{id}) {
					t.Fatalf("only %d left %v", id, got)
				}
				if got, _ := tr.Channel(id); got != ch {
					t.Fatalf("only changed the channel %+v -> %+v", ch, got)
				}
				focus = id
			},
			"cycle": func(t *rapid.T) {
				n := tr.Len()
				id := focus
				for range n {
					id = tr.Cycle(id, 1)
				}
				if id != focus {
					t.Fatalf("cycling %d times from %d ended at %d", n, focus, id)
				}
				d := rapid.IntRange(-2*n, 2*n).Draw(t, "delta")
				next := tr.Cycle(focus, d)
				if back := tr.Cycle(next, -d); back != focus {
					t.Fatalf("Cycle(%d) then Cycle(%d) went %d -> %d -> %d", d, -d, focus, next, back)
				}
				focus = next
			},
			"navigate": func(t *rapid.T) {
				nd := rapid.SampledFrom([]NavDir{NavLeft, NavDown, NavUp, NavRight}).Draw(t, "nav")
				rects := tr.ComputeRects(bounds)
				cur := rects[focus]
				got, ok := tr.NavigateDir(focus, nd, bounds)
				if ok == atEdge(cur, bounds, nd) {
					t.Fatalf("NavigateDir(%d, %v) ok=%v with focus %+v in %+v", focus, nd, ok, cur, bounds)
				}
				if !ok {
					if got != focus {
						t.Fatalf("failed navigation returned %d, want %d", got, focus)
					}
					return
				}
				if got == focus || !onSide(rects[got], cur, nd) {
					t.Fatalf("NavigateDir(%d, %v) = %d %+v, not on that side of %+v", focus, nd, got, rects[got], cur)
				}
				// Among the windows sharing the edge, none overlaps it more.
				ov := func(r Rect) int {
					if nd == NavLeft || nd == NavRight {
						return overlap1D(r.Y, r.H, cur.Y, cur.H)
					}
					return overlap1D(r.X, r.W, cur.X, cur.W)
				}
				for id, r := range rects {
					if id != focus && touches(r, cur, nd) && ov(r) > ov(rects[got]) {
						t.Fatalf("NavigateDir(%d, %v) = %d overlapping %d, but %d overlaps %d", focus, nd, got, ov(rects[got]), id, ov(r))
					}
				}
				focus = got
			},
		})
	})
}
