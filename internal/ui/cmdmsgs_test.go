package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// deliveredMsgs runs each command the way the Bubble Tea runtime does
// (the members of a batch concurrently; ticks fire at once, see
// TestMain in tick_test.go) and returns, for each command in order, the
// first message of type T it delivers. Nothing runs until the helper is
// called, so a test that schedules ticks and then acts sees each tick
// fire after the actions, as it would in the app. Members that deliver
// other types, such as a toast's clear tick, finish in the background
// and are dropped.
//
// It opens tea.Batch but not tea.Sequence: a sequence arrives as
// Bubble Tea's unexported sequenceMsg, so a T inside one never comes
// and the helper blocks until go test's -timeout names it. It has no
// deadline of its own: a wall-clock limit would fail a slow machine
// rather than a broken test. No production code uses tea.Sequence today.
//
// Use it instead of building a tick's message by hand: a hand-built
// message cannot show what the real tick carries.
func deliveredMsgs[T any](t *testing.T, cmds ...tea.Cmd) []T {
	t.Helper()
	chans := make([]chan T, len(cmds))
	for i, cmd := range cmds {
		// A nil command delivers nothing; fail now rather than block.
		if cmd == nil {
			t.Fatalf("command %d is nil, want one that delivers %T", i, *new(T))
		}
		chans[i] = make(chan T, 1)
		go deliverFirst(cmd, chans[i])
	}
	out := make([]T, len(cmds))
	for i, ch := range chans {
		out[i] = <-ch
	}
	return out
}

// receivedNow returns what is already waiting on ch, without blocking.
// For a value a test's own synchronous calls must have produced: if it
// is not there, nothing is going to deliver it later.
func receivedNow[T any](ch <-chan T) (T, bool) {
	select {
	case v := <-ch:
		return v, true
	default:
		var zero T
		return zero, false
	}
}

// deliverFirst runs cmd, starts each member of a batch on its own
// goroutine, and sends the first message of type T on ch.
func deliverFirst[T any](cmd tea.Cmd, ch chan T) {
	if cmd == nil {
		return
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			go deliverFirst(c, ch)
		}
		return
	}
	if m, ok := msg.(T); ok {
		select {
		case ch <- m:
		default:
		}
	}
}
