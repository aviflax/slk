package ui

import (
	"fmt"
	"testing"
)

// The chrome every layout case assumes: the 6-column workspace rail
// and the default 30-column sidebar (+2 border when shown).
const (
	testRailW    = 6
	testSidebarW = 30
)

func TestPanelLayoutCompute(t *testing.T) {
	type want struct{ msgW, msgB, thrW, thrB int }
	tests := []struct {
		name        string
		width       int
		sidebar     bool
		thread      bool
		threadFront bool
		want        want
	}{
		{"no thread", 120, true, false, false, want{80, 2, 0, 0}},
		{"80 stacked, thread front", 80, true, true, true, want{0, 0, 40, 2}},
		{"80 stacked, channel front", 80, true, true, false, want{40, 2, 0, 0}},
		{"120 stacked, thread front", 120, true, true, true, want{0, 0, 80, 2}},
		{"120 stacked, channel front", 120, true, true, false, want{80, 2, 0, 0}},
		{"140 stacked, thread front", 140, true, true, true, want{0, 0, 100, 2}},
		{"151 one below threshold, thread front", 151, true, true, true, want{0, 0, 111, 2}},
		{"151 one below threshold, channel front", 151, true, true, false, want{111, 2, 0, 0}},
		{"152 threshold, both minima", 152, true, true, true, want{50, 2, 60, 2}},
		{"152 threshold, front irrelevant", 152, true, true, false, want{50, 2, 60, 2}},
		{"160 thread squeezed toward 80", 160, true, true, false, want{50, 2, 68, 2}},
		{"200 thread lifted to 80", 200, true, true, false, want{78, 2, 80, 2}},
		{"300 35% already above 80", 300, true, true, false, want{167, 2, 91, 2}},
		{"sidebar hidden 119 stacked", 119, false, true, true, want{0, 0, 111, 2}},
		{"sidebar hidden 120 side by side", 120, false, true, true, want{50, 2, 60, 2}},
		{"sidebar hidden 140 side by side", 140, false, true, false, want{50, 2, 80, 2}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := newPanelLayout()
			f := l.Compute(tt.width, 30, testRailW, testSidebarW, tt.sidebar, tt.thread, tt.threadFront)
			got := want{f.MsgWidth, f.MsgBorder, f.ThreadWidth, f.ThreadBorder}
			if got != tt.want {
				t.Fatalf("widths = %+v, want %+v", got, tt.want)
			}
			sbEnd := testRailW
			if tt.sidebar {
				sbEnd += testSidebarW + 2
			}
			if l.sidebarEnd != sbEnd || l.msgEnd != sbEnd+got.msgW+got.msgB || l.threadEnd != tt.width {
				t.Errorf("bands = rail %d / sidebar %d / msg %d / thread %d, want sidebarEnd %d, msgEnd %d, threadEnd %d",
					l.railWidth, l.sidebarEnd, l.msgEnd, l.threadEnd, sbEnd, sbEnd+got.msgW+got.msgB, tt.width)
			}
			if f.ContentHeight != 29 {
				t.Errorf("ContentHeight = %d, want 29", f.ContentHeight)
			}
		})
	}
}

func TestPanelLayoutCompute_Sweep(t *testing.T) {
	const floorPaneW = 10
	for width := 20; width <= 300; width++ {
		for _, sidebar := range []bool{true, false} {
			for _, thread := range []bool{true, false} {
				for _, front := range []bool{true, false} {
					l := newPanelLayout()
					f := l.Compute(width, 30, testRailW, testSidebarW, sidebar, thread, front)
					area := width - testRailW
					if sidebar {
						area -= testSidebarW + 2
					}
					where := func() string {
						return fmt.Sprintf("width %d sidebar %v thread %v front %v", width, sidebar, thread, front)
					}
					if f.MsgWidth < 0 || f.ThreadWidth < 0 {
						t.Fatalf("%s: negative width %+v", where(), f)
					}
					msgDrawn, thrDrawn := f.MsgBorder > 0, f.ThreadBorder > 0
					if !msgDrawn && !thrDrawn {
						t.Fatalf("%s: no content pane drawn", where())
					}
					if !thread && thrDrawn {
						t.Fatalf("%s: thread drawn with no thread open", where())
					}
					sideBySide := thread && area-4 >= 110
					if thread && sideBySide != (msgDrawn && thrDrawn) {
						t.Fatalf("%s: side by side = %v, want %v", where(), msgDrawn && thrDrawn, sideBySide)
					}
					if sideBySide && (f.ThreadWidth < 60 || f.MsgWidth < 50) {
						t.Fatalf("%s: side by side below minima %+v", where(), f)
					}
					if thread && !sideBySide && thrDrawn != front {
						t.Fatalf("%s: stacked pane is thread=%v, want %v", where(), thrDrawn, front)
					}
					if area-2 >= floorPaneW && l.threadEnd != width {
						t.Fatalf("%s: bands end at %d, want %d", where(), l.threadEnd, width)
					}
				}
			}
		}
	}
}

// sidebarFits decides whether a sidebar the user has not hidden is
// drawn. Default chrome: 6-column rail, 30-column sidebar.
func TestSidebarFits(t *testing.T) {
	tests := []struct {
		width, sidebarW int
		thread          bool
		want            bool
	}{
		{0, testSidebarW, false, true}, // size not reported yet
		{60, testSidebarW, false, false},
		{80, testSidebarW, false, false},
		{99, testSidebarW, false, false},
		{100, testSidebarW, false, true}, // channel keeps exactly 60
		{120, testSidebarW, false, true},
		{140, testSidebarW, false, true},
		{160, testSidebarW, false, true},
		{200, testSidebarW, false, true},
		// A thread is open: the sidebar yields when hiding it lets the
		// thread sit beside the channel, and only then.
		{80, testSidebarW, true, false},
		{100, testSidebarW, true, true}, // stacked either way
		{119, testSidebarW, true, true},
		{120, testSidebarW, true, false},
		{140, testSidebarW, true, false},
		{151, testSidebarW, true, false},
		{152, testSidebarW, true, true},
		{160, testSidebarW, true, true},
		{200, testSidebarW, true, true},
		// A configured width moves the thresholds with it.
		{119, 50, false, false},
		{120, 50, false, true},
		{89, 20, false, false},
		{90, 20, false, true},
	}
	for _, tt := range tests {
		if got := sidebarFits(tt.width, testRailW, tt.sidebarW, tt.thread); got != tt.want {
			t.Errorf("sidebarFits(width %d, sidebar %d, thread %v) = %v, want %v",
				tt.width, tt.sidebarW, tt.thread, got, tt.want)
		}
	}
}
