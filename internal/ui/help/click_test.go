package help

import (
	"testing"

	"charm.land/lipgloss/v2"
)

func TestBoxSizeMatchesRender(t *testing.T) {
	m := New()
	m.SetEntries(sampleEntries())
	m.Open()

	w, h := m.BoxSize(80, 24)
	box := m.renderBox(80, 24)
	if gw := lipgloss.Width(box); w != gw {
		t.Errorf("BoxSize width = %d, rendered width = %d", w, gw)
	}
	if gh := lipgloss.Height(box); h != gh {
		t.Errorf("BoxSize height = %d, rendered height = %d", h, gh)
	}
}

func TestClickRowMovesHighlight(t *testing.T) {
	m := New()
	m.SetEntries(sampleEntries())
	m.Open() // 6 entries, selected starts at 0

	if !m.ClickRow(80, 24, m.listTopOffset()+2) {
		t.Fatal("ClickRow on a populated row should return true")
	}
	if m.selected != 2 {
		t.Errorf("ClickRow set selected=%d, want 2", m.selected)
	}
	if m.ClickRow(80, 24, m.listTopOffset()-1) {
		t.Error("ClickRow above the list should return false")
	}
	if m.ClickRow(80, 24, m.listTopOffset()+6) {
		t.Error("ClickRow past the last row should return false")
	}
}
