package help

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
	"github.com/charmbracelet/x/ansi"
)

func groupedEntries() []Entry {
	return []Entry{
		{Key: "j", Desc: "down", Group: "Navigation"},
		{Key: "k", Desc: "up", Group: "Navigation"},
		{Key: "r", Desc: "add reaction", Group: "Messages"},
		{Key: "Q", Desc: "quit", Group: "App"},
	}
}

func TestFromKeyMapGroupsByTagInGroupOrder(t *testing.T) {
	type sampleMap struct {
		A key.Binding `help:"App"`
		B key.Binding `help:"Navigation"`
		C key.Binding `help:"Navigation"`
	}
	km := sampleMap{
		A: key.NewBinding(key.WithHelp("a", "alpha")),
		B: key.NewBinding(key.WithHelp("b", "zeta")),
		C: key.NewBinding(key.WithHelp("c", "beta")),
	}
	got := FromKeyMap(km)
	want := []Entry{
		{"c", "beta", "Navigation"},
		{"b", "zeta", "Navigation"},
		{"a", "alpha", "App"},
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("entries = %v, want %v", got, want)
		}
	}
}

func TestHeadersRenderAndEmptySectionsAreHidden(t *testing.T) {
	m := New()
	m.SetEntries(groupedEntries())
	m.Open()
	out := ansi.Strip(m.renderBox(80, 24))
	for _, h := range []string{"Navigation", "Messages", "App"} {
		if !strings.Contains(out, h) {
			t.Errorf("missing header %q:\n%s", h, out)
		}
	}
	m.HandleKey("/")
	for _, r := range "react" {
		m.HandleKey(string(r))
	}
	out = ansi.Strip(m.renderBox(80, 24))
	if !strings.Contains(out, "Messages") || strings.Contains(out, "Navigation") || strings.Contains(out, "App") {
		t.Errorf("filter should leave only the Messages section:\n%s", out)
	}
}

func TestHeadersAreNotMatchedByFilter(t *testing.T) {
	m := New()
	m.SetEntries(groupedEntries())
	m.Open()
	m.HandleKey("/")
	for _, r := range "navigation" {
		m.HandleKey(string(r))
	}
	if n := len(m.VisibleEntries()); n != 0 {
		t.Errorf("typing a group name matched %d entries, want 0", n)
	}
}

func TestNavigationSkipsHeadersAndClickOnHeaderIsIgnored(t *testing.T) {
	m := New()
	m.SetEntries(groupedEntries())
	m.Open()
	top := m.listTopOffset()
	// Rows: Navigation, down, up, Messages, add reaction, App, quit.
	if m.ClickRow(80, 24, top) {
		t.Error("clicking a header must not select")
	}
	if !m.ClickRow(80, 24, top+4) || m.selected != 2 {
		t.Errorf("click on 'add reaction' selected=%d, want 2", m.selected)
	}
	m.selected = 1
	m.HandleKey("j")
	if m.selected != 2 {
		t.Errorf("j from last Navigation entry selected=%d, want 2", m.selected)
	}
}

func TestNoRedundantSearchHint(t *testing.T) {
	m := New()
	m.SetEntries(groupedEntries())
	m.Open()
	out := ansi.Strip(m.renderBox(80, 24))
	if strings.Contains(out, "Press / to search") {
		t.Errorf("footer already says '/ search':\n%s", out)
	}
	if !strings.Contains(out, "/ search") {
		t.Errorf("footer lost '/ search':\n%s", out)
	}
}

// A header row that is one cell too wide wraps and leaves a blank row.
func TestHeaderIsFollowedByItsFirstEntry(t *testing.T) {
	for _, w := range []int{40, 80} {
		m := New()
		m.SetEntries(groupedEntries())
		m.Open()
		lines := strings.Split(ansi.Strip(m.renderBox(w, 24)), "\n")
		for i, l := range lines {
			if strings.Contains(l, "Navigation") && !strings.Contains(lines[i+1], "down") {
				t.Errorf("width %d: row after header = %q", w, lines[i+1])
			}
		}
	}
}

func TestGroupedRenderNarrowWidths(t *testing.T) {
	for _, w := range []int{20, 30, 40, 60} {
		m := New()
		m.SetEntries(groupedEntries())
		m.Open()
		if bw, _ := m.BoxSize(w, 12); bw > w {
			t.Errorf("width %d: box is %d wide", w, bw)
		}
	}
}
