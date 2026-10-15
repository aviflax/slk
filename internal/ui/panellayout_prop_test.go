package ui

import (
	"testing"

	"pgregory.net/rapid"
)

// layoutInput is one call's worth of Compute arguments.
type layoutInput struct {
	width, height, rail, sidebar               int
	sidebarVisible, threadVisible, threadFront bool
}

// drawLayoutInput draws a terminal size, rail and sidebar width in the
// ranges slk sees (the rail is 0 with one workspace; the sidebar's
// width is configurable).
func drawLayoutInput(t *rapid.T, minWidth int) layoutInput {
	return layoutInput{
		width:          rapid.IntRange(minWidth, 400).Draw(t, "width"),
		height:         rapid.IntRange(2, 120).Draw(t, "height"),
		rail:           rapid.SampledFrom([]int{0, 6}).Draw(t, "rail"),
		sidebar:        rapid.IntRange(10, 60).Draw(t, "sidebar"),
		sidebarVisible: rapid.Bool().Draw(t, "sidebarVisible"),
		threadVisible:  rapid.Bool().Draw(t, "threadVisible"),
		threadFront:    rapid.Bool().Draw(t, "threadFront"),
	}
}

func (in layoutInput) compute() (*panelLayout, panelLayoutFrame) {
	l := newPanelLayout()
	return l, l.Compute(in.width, in.height, in.rail, in.sidebar, in.sidebarVisible, in.threadVisible, in.threadFront)
}

// paneArea is the content area Compute divides between the channel and
// thread panes.
func (in layoutInput) paneArea() int {
	a := in.width - in.rail
	if in.sidebarVisible {
		a -= in.sidebar + paneBorder
	}
	return a
}

func (f panelLayoutFrame) total() int {
	return f.RailWidth + f.SidebarWidth + f.SidebarBorder + f.MsgWidth + f.MsgBorder + f.ThreadWidth + f.ThreadBorder
}

// Compute's frame obeys, for every input:
//   - a pane that is not drawn has width 0 and border 0, a drawn pane
//     has a 2-col border and at least floorPaneW columns;
//   - the channel pane is drawn unless the thread is open, stacked and
//     in front; the thread pane is drawn only when open, and then either
//     beside the channel or in front of it;
//   - side by side, the channel keeps minMsgWidth and the thread
//     minThreadW;
//   - whenever the area left for the panes holds one bordered pane at
//     the floor width, the widths add up to the terminal width exactly.
func TestComputeFrameInvariants(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// KNOWN: below the floor (paneArea < paneBorder+floorPaneW, e.g.
		// width 15 with a 6-col rail and no sidebar, or a pinned 30-col
		// sidebar at width 40) the floor wins and the frame is wider
		// than the terminal. No column budget is left to give, so this
		// is a product choice, not a one-line fix; the generator keeps
		// the exact-sum check to inputs above the floor.
		in := drawLayoutInput(t, 0)
		l, f := in.compute()

		if f.ContentHeight != in.height-1 {
			t.Fatalf("ContentHeight %d, want %d", f.ContentHeight, in.height-1)
		}
		if in.sidebarVisible != (f.SidebarBorder > 0) || (!in.sidebarVisible && f.SidebarWidth != 0) {
			t.Fatalf("sidebar visible=%v but frame %+v", in.sidebarVisible, f)
		}
		stackedFront := in.threadVisible && in.threadFront && !sideBySide(in.paneArea())
		msgDrawn := !stackedFront
		threadDrawn := in.threadVisible && (stackedFront || sideBySide(in.paneArea()))
		for _, p := range []struct {
			name         string
			drawn        bool
			width, bordr int
		}{{"messages", msgDrawn, f.MsgWidth, f.MsgBorder}, {"thread", threadDrawn, f.ThreadWidth, f.ThreadBorder}} {
			if p.drawn && (p.bordr != paneBorder || p.width < floorPaneW) {
				t.Fatalf("%s drawn with width %d border %d: %+v", p.name, p.width, p.bordr, f)
			}
			if !p.drawn && (p.width != 0 || p.bordr != 0) {
				t.Fatalf("%s hidden but width %d border %d: %+v", p.name, p.width, p.bordr, f)
			}
		}
		if msgDrawn && threadDrawn && (f.MsgWidth < minMsgWidth || f.ThreadWidth < minThreadW) {
			t.Fatalf("side by side below the minima: %+v", f)
		}
		if in.paneArea() >= paneBorder+floorPaneW && f.total() != in.width {
			t.Fatalf("widths sum to %d, terminal is %d: %+v", f.total(), in.width, f)
		}
		if l.RailWidth() != f.RailWidth || l.SidebarEnd()-l.RailWidth() != f.SidebarWidth+f.SidebarBorder ||
			l.MsgEnd()-l.SidebarEnd() != f.MsgWidth+f.MsgBorder || l.ThreadEnd()-l.MsgEnd() != f.ThreadWidth+f.ThreadBorder {
			t.Fatalf("bands rail %d sidebar %d msg %d thread %d disagree with frame %+v",
				l.RailWidth(), l.SidebarEnd(), l.MsgEnd(), l.ThreadEnd(), f)
		}
	})
}

// PanelAt partitions each content row: walking x left to right, the
// panel never goes back (rail, sidebar, messages, thread, then nothing),
// a pane's columns map to pane-local x running 0.. across its content
// (with -1 on its left border), and the status-bar row is no pane.
func TestPanelAtBandsPartitionTheRow(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		in := drawLayoutInput(t, 0)
		l, _ := in.compute()
		y := rapid.IntRange(0, in.height-1).Draw(t, "y")
		order := map[Panel]int{PanelSidebar: 1, PanelMessages: 2, PanelThread: 3}
		last, lastX := 0, map[Panel]int{}
		for x := range in.width {
			p, px, py, ok := l.PanelAt(x, y, in.height, in.sidebarVisible, in.threadVisible)
			if y == in.height-1 {
				if ok {
					t.Fatalf("status row (%d, %d) hit %v", x, y, p)
				}
				continue
			}
			rank := 0
			switch {
			case x < l.RailWidth():
				rank = 0
			case x < l.SidebarEnd():
				rank = 1
			case x < l.MsgEnd():
				rank = 2
			case x < l.ThreadEnd():
				rank = 3
			default:
				rank = 4
			}
			if ok {
				if order[p] != rank || py != y-1 {
					t.Fatalf("(%d, %d) -> %v (%d, %d) in band %d", x, y, p, px, py, rank)
				}
				if prev, seen := lastX[p]; seen && px != prev+1 {
					t.Fatalf("(%d, %d) -> %v x %d after %d", x, y, p, px, prev)
				} else if !seen && px != -1 {
					t.Fatalf("%v starts at pane x %d, want -1 (its border)", p, px)
				}
				lastX[p] = px
			} else if rank == 2 || rank == 3 {
				t.Fatalf("(%d, %d) in band %d hit nothing", x, y, rank)
			}
			if rank < last {
				t.Fatalf("band went back from %d to %d at x %d", last, rank, x)
			}
			last = rank
		}
	})
}

// sidebarFits agrees with Compute about the reason it gives:
//   - with no thread it is monotone in the width, and when it says yes
//     the channel pane keeps minSidebarMsgW columns beside the sidebar;
//   - with a thread, where it hides a sidebar that would fit without
//     one, hiding it is what lets the thread sit beside the channel;
//   - width 0 (no size reported yet) always fits.
func TestSidebarFitsAgreesWithCompute(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		in := drawLayoutInput(t, 1)
		if !sidebarFits(0, in.rail, in.sidebar, in.threadVisible) {
			t.Fatal("width 0 must fit")
		}
		fits := sidebarFits(in.width, in.rail, in.sidebar, false)
		if fits && !sidebarFits(in.width+rapid.IntRange(1, 200).Draw(t, "grow"), in.rail, in.sidebar, false) {
			t.Fatalf("fits at %d but not wider", in.width)
		}
		if fits {
			in.sidebarVisible, in.threadVisible = true, false
			if _, f := in.compute(); f.MsgWidth < minSidebarMsgW {
				t.Fatalf("sidebar fits at %d but leaves the channel %d cols", in.width, f.MsgWidth)
			}
		}
		withThread := sidebarFits(in.width, in.rail, in.sidebar, true)
		if fits && !withThread {
			in.sidebarVisible, in.threadVisible = false, true
			if _, f := in.compute(); f.MsgWidth == 0 || f.ThreadWidth == 0 {
				t.Fatalf("thread hid the sidebar at %d, but the panes still stack: %+v", in.width, f)
			}
		}
		if withThread {
			// With the channel in front, it either sits beside the thread
			// or keeps minSidebarMsgW columns on its own.
			in.sidebarVisible, in.threadVisible, in.threadFront = true, true, false
			if _, f := in.compute(); f.MsgWidth < minSidebarMsgW && f.ThreadWidth == 0 {
				t.Fatalf("sidebar fits beside a thread at %d but the channel gets %d cols stacked", in.width, f.MsgWidth)
			}
		}
	})
}
