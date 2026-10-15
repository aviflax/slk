package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// ctrlB is the default ToggleSidebar binding.
func ctrlB() tea.KeyMsg { return keyMod('b', tea.ModCtrl) }

// The sidebar gives way on narrow terminals so the channel keeps 60
// columns; the rail stays.
func TestNarrow_SidebarAutoHides(t *testing.T) {
	tests := []struct {
		width            int
		sidebarW, msgW   int
		sidebarEnd, msgE int
	}{
		{60, 0, 52, 6, 60},
		{80, 0, 72, 6, 80},
		{99, 0, 91, 6, 99},
		{100, 30, 60, 38, 100},
		{120, 30, 80, 38, 120},
		{140, 30, 100, 38, 140},
		{160, 30, 120, 38, 160},
		{200, 30, 160, 38, 200},
	}
	for _, tt := range tests {
		a := stackedApp(t, tt.width)
		f := a.computeFrame()
		if f.RailWidth != 6 || f.SidebarWidth != tt.sidebarW || f.MsgWidth != tt.msgW {
			t.Errorf("width %d: rail %d sidebar %d msg %d, want 6 / %d / %d",
				tt.width, f.RailWidth, f.SidebarWidth, f.MsgWidth, tt.sidebarW, tt.msgW)
		}
		if a.layout.sidebarEnd != tt.sidebarEnd || a.layout.msgEnd != tt.msgE {
			t.Errorf("width %d: bands sidebarEnd %d msgEnd %d, want %d / %d",
				tt.width, a.layout.sidebarEnd, a.layout.msgEnd, tt.sidebarEnd, tt.msgE)
		}
		if !a.sidebarVisible {
			t.Errorf("width %d: auto-hiding cleared the user's sidebarVisible", tt.width)
		}
	}
}

// Thread open: below 120 the panes stack; from 120 they sit side by
// side, the sidebar yielding until 152 leaves room for all three.
func TestNarrow_ThreadSideBySideThresholds(t *testing.T) {
	tests := []struct {
		width                   int
		sideBySide              bool
		sidebarW, msgW, threadW int
	}{
		{80, false, 0, 0, 72},
		{110, false, 30, 0, 70},
		{119, false, 30, 0, 79},
		{120, true, 0, 50, 60},
		{140, true, 0, 50, 80},
		{151, true, 0, 61, 80},
		{152, true, 30, 50, 60},
		{160, true, 30, 50, 68},
		{200, true, 30, 78, 80},
	}
	for _, tt := range tests {
		a := stackedApp(t, tt.width)
		updateAndRender(t, a, keyCode(tea.KeyEnter))
		if !a.threadVisible || a.focusedPanel != PanelThread {
			t.Fatalf("width %d: Enter did not open and focus the thread", tt.width)
		}
		assertFront(t, a, tt.sideBySide, true)
		f := a.computeFrame()
		if f.SidebarWidth != tt.sidebarW || f.MsgWidth != tt.msgW || f.ThreadWidth != tt.threadW {
			t.Errorf("width %d: sidebar %d msg %d thread %d, want %d / %d / %d",
				tt.width, f.SidebarWidth, f.MsgWidth, f.ThreadWidth, tt.sidebarW, tt.msgW, tt.threadW)
		}

		// Closing the thread gives a yielded sidebar back.
		updateAndRender(t, a, keyCode(tea.KeyEscape))
		if got, want := a.computeFrame().SidebarWidth > 0, sidebarFits(tt.width, 6, 30, false); got != want {
			t.Errorf("width %d: after Esc sidebar shown = %v, want %v", tt.width, got, want)
		}
	}
}

// ctrl+b always flips what is on screen. At 80 columns the sidebar is
// auto-hidden, so ctrl+b shows it (squeezing the channel), and the
// choice holds until ctrl+b hides it again.
func TestNarrow_ExplicitToggleAtNarrowWidth(t *testing.T) {
	a := stackedApp(t, 80)
	if a.computeFrame().SidebarWidth != 0 {
		t.Fatal("precondition: sidebar auto-hidden at 80")
	}

	updateAndRender(t, a, ctrlB())
	f := a.computeFrame()
	if f.SidebarWidth != 30 || f.MsgWidth != 40 {
		t.Fatalf("ctrl+b at 80: sidebar %d msg %d, want 30 / 40", f.SidebarWidth, f.MsgWidth)
	}
	// Hit-testing lines up with what is drawn: rail, sidebar, channel.
	if p, _, _, ok := a.panelAt(3, 5); p != PanelWorkspace || ok {
		t.Errorf("x=3: panel %v ok %v, want the rail", p, ok)
	}
	if p, _, _, _ := a.panelAt(6, 5); p != PanelSidebar {
		t.Errorf("x=6: panel %v, want the sidebar", p)
	}
	if p, px, _, ok := a.panelAt(39, 5); p != PanelMessages || px != 0 || !ok {
		t.Errorf("x=39: panel %v paneX %d ok %v, want messages at 0", p, px, ok)
	}

	// Tab reaches the sidebar it shows.
	updateAndRender(t, a, keyCode(tea.KeyTab))
	if a.focusedPanel != PanelSidebar {
		t.Fatalf("tab: focus %v, want the shown sidebar", a.focusedPanel)
	}

	// A resize does not undo the explicit choice.
	updateAndRender(t, a, tea.WindowSizeMsg{Width: 70, Height: 30})
	if a.computeFrame().SidebarWidth != 30 || a.focusedPanel != PanelSidebar {
		t.Fatalf("resize to 70 hid the sidebar the user showed")
	}

	updateAndRender(t, a, ctrlB())
	if a.computeFrame().SidebarWidth != 0 || a.focusedPanel != PanelMessages {
		t.Fatalf("second ctrl+b: sidebar %d focus %v, want hidden and channel focused",
			a.computeFrame().SidebarWidth, a.focusedPanel)
	}
	if p, px, _, ok := a.panelAt(7, 5); p != PanelMessages || px != 0 || !ok {
		t.Errorf("x=7 after hiding: panel %v paneX %d ok %v, want messages at 0", p, px, ok)
	}

	// Hidden by hand stays hidden at any width.
	updateAndRender(t, a, tea.WindowSizeMsg{Width: 200, Height: 30})
	if a.computeFrame().SidebarWidth != 0 {
		t.Fatal("widening showed a sidebar the user hid")
	}
	// Shown by hand where it fits is not pinned: narrowing hides it.
	updateAndRender(t, a, ctrlB())
	updateAndRender(t, a, tea.WindowSizeMsg{Width: 80, Height: 30})
	if a.computeFrame().SidebarWidth != 0 {
		t.Fatal("sidebar shown at 200 did not auto-hide at 80")
	}
}

// Narrowing past the threshold while the sidebar has focus moves focus
// to the pane that stays on screen, so keys never go to a hidden pane.
func TestNarrow_AutoHideMovesFocusOffSidebar(t *testing.T) {
	a := stackedApp(t, 200)
	updateAndRender(t, a, keyMod(tea.KeyTab, tea.ModShift))
	if a.focusedPanel != PanelSidebar {
		t.Fatalf("precondition: focus %v, want sidebar", a.focusedPanel)
	}
	updateAndRender(t, a, tea.WindowSizeMsg{Width: 80, Height: 30})
	if a.focusedPanel != PanelMessages {
		t.Fatalf("focus %v after narrowing, want messages", a.focusedPanel)
	}

	// Same with a stacked thread in front: focus goes to the thread.
	b := stackedApp(t, 200)
	updateAndRender(t, b, keyCode(tea.KeyEnter))
	updateAndRender(t, b, keyCode(tea.KeyTab)) // thread -> sidebar
	updateAndRender(t, b, tea.WindowSizeMsg{Width: 90, Height: 30})
	if b.focusedPanel != PanelThread {
		t.Fatalf("focus %v after narrowing with the thread in front, want thread", b.focusedPanel)
	}
	assertFront(t, b, false, true)
}

// Growing the sidebar with ] at a width where it then no longer fits
// keeps it on screen: the user is resizing it, not asking it to leave.
func TestNarrow_GrowingSidebarDoesNotHideIt(t *testing.T) {
	a := stackedApp(t, 100)
	if a.computeFrame().SidebarWidth != 30 {
		t.Fatal("precondition: sidebar shown at 100")
	}
	updateAndRender(t, a, keyPress(']'))
	if got := a.computeFrame().SidebarWidth; got != a.sidebar.Width() || got <= 30 {
		t.Fatalf("after ]: drawn sidebar %d, model width %d, want the grown sidebar drawn", got, a.sidebar.Width())
	}
}
