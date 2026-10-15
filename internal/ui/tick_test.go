package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// TestMain replaces the App's timer with one that fires at once. A test
// that schedules a tick and then runs the returned command gets the
// tick's message without sleeping through its duration; the order in
// which the test runs commands, not their durations, decides what
// arrives first. The duration the App asked for is not observable
// through it, so a test of a delay's value reads the constant itself.
func TestMain(m *testing.M) {
	tick = func(_ time.Duration, fn func(time.Time) tea.Msg) tea.Cmd {
		return func() tea.Msg { return fn(time.Time{}) }
	}
	os.Exit(m.Run())
}

// A timer armed with tea.Tick directly would run on the real clock in
// every test that drains its command.
func TestTimersGoThroughTick(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || f == "tick.go" {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(src), "\n") {
			code, _, _ := strings.Cut(line, "//")
			if strings.Contains(code, "tea.Tick(") {
				t.Errorf("%s:%d calls tea.Tick; use tick so tests do not wait on it", f, i+1)
			}
		}
	}
}
