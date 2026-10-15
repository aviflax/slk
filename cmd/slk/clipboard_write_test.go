package main

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"reflect"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestClipboardWriter(t *testing.T) {
	const text = "hello 世界 👋\nsecond line\n"
	for _, tt := range []struct {
		name       string
		goos       string
		env        map[string]string
		writeErr   error
		wantNative bool
	}{
		{name: "Terminal.app", goos: "darwin", env: map[string]string{"TERM_PROGRAM": "Apple_Terminal"}, wantNative: true},
		{name: "local tmux", goos: "darwin", env: map[string]string{"TMUX": "/tmp/tmux/default", "TERM_PROGRAM": "tmux"}, wantNative: true},
		{name: "other macOS terminal", goos: "darwin", wantNative: true},
		{name: "SSH connection", goos: "darwin", env: map[string]string{"SSH_CONNECTION": "client 123 server 22"}},
		{name: "SSH client", goos: "darwin", env: map[string]string{"SSH_CLIENT": "client 123 22"}},
		{name: "SSH tty", goos: "darwin", env: map[string]string{"SSH_TTY": "/dev/ttys001"}},
		{name: "Linux", goos: "linux"},
		{name: "Windows", goos: "windows"},
		{name: "native failure", goos: "darwin", writeErr: errors.New("pbcopy failed"), wantNative: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			writer := newClipboardWriter(tt.goos, func(key string) string { return tt.env[key] }, func(got string) error {
				calls++
				if got != text {
					t.Errorf("native payload = %q, want %q", got, text)
				}
				return tt.writeErr
			})
			cmd := writer(text)
			if calls != 0 {
				t.Fatal("clipboard write ran before command execution")
			}
			if cmd == nil {
				t.Fatal("clipboard writer returned no command")
			}
			got := cmd()
			wantCalls := 0
			if tt.wantNative {
				wantCalls = 1
			}
			if calls != wantCalls {
				t.Errorf("native calls = %d, want %d", calls, wantCalls)
			}
			if tt.wantNative && tt.writeErr == nil {
				if got != nil {
					t.Errorf("successful native write returned %T, want nil", got)
				}
			} else if want := tea.SetClipboard(text)(); !reflect.DeepEqual(got, want) {
				t.Errorf("OSC 52 message = %#v, want %#v", got, want)
			}
		})
	}
}

// pbcopy gets the text on stdin, byte for byte. The command is
// inspected, not run, so the test needs no pbcopy and starts no process.
func TestPbcopyCommand(t *testing.T) {
	const text = "hello 世界 👋\n'quotes' $literal `text`\n"
	cmd := pbcopyCommand(context.Background(), text)
	if want := []string{"pbcopy"}; !reflect.DeepEqual(cmd.Args, want) {
		t.Errorf("args = %q, want %q", cmd.Args, want)
	}
	if base := filepath.Base(cmd.Path); base != "pbcopy" {
		t.Errorf("path = %q, want pbcopy", cmd.Path)
	}
	stdin, err := io.ReadAll(cmd.Stdin)
	if err != nil {
		t.Fatal(err)
	}
	if string(stdin) != text {
		t.Errorf("stdin = %q, want %q", stdin, text)
	}
}

// Without pbcopy on PATH the write fails at lookup, before any process
// starts, so the caller falls back to OSC 52.
func TestWriteMacOSClipboardWithoutPbcopy(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if err := writeMacOSClipboard("text"); err == nil {
		t.Fatal("writeMacOSClipboard succeeded with no pbcopy on PATH")
	}
}
