package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ui/wintree"
)

func TestStacked_WindowSplitWithThreadInFront(t *testing.T) {
	// Stacked with room to split the channel: at 140 the sidebar would
	// yield to a side-by-side thread, so show it with ctrl+b.
	a := stackedApp(t, 140)
	updateAndRender(t, a, keyCode(tea.KeyEnter))
	updateAndRender(t, a, keyMod('b', tea.ModCtrl))
	assertFront(t, a, false, true)

	bands := *a.layout
	want := wintree.Rect{W: 140 - testRailW - testSidebarW - 2, H: 29}
	if got := a.windowBounds(); got != want {
		t.Errorf("windowBounds = %+v, want %+v (the channel-in-front area the windows fill)", got, want)
	}
	if *a.layout != bands {
		t.Errorf("windowBounds overwrote the hit-test bands: %+v → %+v", bands, *a.layout)
	}

	pressCtrlW(a)
	_ = press(a, 'v')
	if a.wins.Len() != 2 {
		t.Fatalf("ctrl+w v with the thread in front: %d windows, want 2", a.wins.Len())
	}
	if a.focusedPanel != PanelMessages || !a.threadVisible {
		t.Fatalf("after split: focus=%v threadVisible=%v, want channel focused, thread open", a.focusedPanel, a.threadVisible)
	}
	_ = a.View()
	assertFront(t, a, true, false)
}

func TestStacked_MouseRoutesToThePaneInFront(t *testing.T) {
	a := stackedApp(t, 110)
	updateAndRender(t, a, keyCode(tea.KeyEnter))
	updateAndRender(t, a, keyCode(tea.KeyTab)) // thread → sidebar; thread stays in front
	assertFront(t, a, false, true)

	x, y := a.layout.sidebarEnd+5, 4
	if panel, px, py, ok := a.panelAt(x, y); !ok || panel != PanelThread || px != 4 || py != 3 {
		t.Fatalf("panelAt(%d,%d) = %v,%d,%d,%v; want PanelThread,4,3,true", x, y, panel, px, py, ok)
	}

	msgVer := a.messagepane.Version()
	updateAndRender(t, a, tea.MouseWheelMsg{X: x, Y: y, Button: tea.MouseWheelUp})
	if a.messagepane.Version() != msgVer {
		t.Error("a wheel over the thread scrolled the hidden channel pane")
	}

	updateAndRender(t, a, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	if a.focusedPanel != PanelThread {
		t.Fatalf("click over the thread focused %v, want PanelThread", a.focusedPanel)
	}

	updateAndRender(t, a, keyMod(tea.KeyTab, tea.ModShift)) // thread → channel
	assertFront(t, a, true, false)
	updateAndRender(t, a, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	if a.focusedPanel != PanelMessages || !a.threadVisible {
		t.Fatalf("click over the channel: focus=%v threadVisible=%v, want channel focused, thread open", a.focusedPanel, a.threadVisible)
	}
}
