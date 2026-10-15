package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestExecuteCommand_EmptyIsNoop(t *testing.T) {
	a := NewApp()
	if cmd := executeCommand(a, "   "); cmd != nil {
		t.Fatal("empty command line should be a no-op")
	}
	if a.mode != ModeNormal {
		t.Fatalf("mode = %v, want ModeNormal", a.mode)
	}
}

func TestExecuteCommand_UnknownShowsToast(t *testing.T) {
	a := NewApp()
	cmd := executeCommand(a, "bogus")
	if cmd == nil {
		t.Fatal("unknown command should return the toast-clear cmd")
	}
	if out := a.statusbar.View(120); !strings.Contains(out, "Unknown command: bogus") {
		t.Fatalf("expected unknown-command toast, got:\n%s", out)
	}
}

func TestExecuteCommand_HelpOpensHelpOverlay(t *testing.T) {
	a := NewApp()
	_ = executeCommand(a, "help")
	if a.mode != ModeHelp || !a.help.IsVisible() {
		t.Fatalf("mode = %v, visible = %v; want ModeHelp and a visible overlay", a.mode, a.help.IsVisible())
	}
}

func TestExecuteCommand_QuitAliasesOpenQuitConfirm(t *testing.T) {
	for _, name := range []string{"quit", "qa"} {
		a := NewApp()
		_ = executeCommand(a, name)
		if !a.confirmPrompt.IsVisible() {
			t.Fatalf(":%s did not raise the quit confirmation", name)
		}
	}
}

func TestExecuteCommand_WSOpensWorkspaceFinder(t *testing.T) {
	a := NewApp()
	_ = executeCommand(a, "ws")
	if a.mode != ModeWorkspaceFinder {
		t.Fatalf("mode = %v, want ModeWorkspaceFinder", a.mode)
	}
}

func TestExecuteCommand_TrimsAndIgnoresArgs(t *testing.T) {
	a := NewApp()
	_ = executeCommand(a, "  ws   extra  ")
	if a.mode != ModeWorkspaceFinder {
		t.Fatalf("mode = %v, want ModeWorkspaceFinder", a.mode)
	}
}

func TestDispatchCommand_FieldsAndRest(t *testing.T) {
	var got commandArgs
	table := map[string]commandFunc{
		"echo": func(_ *App, args commandArgs) tea.Cmd { got = args; return nil },
	}
	_ = dispatchCommand(NewApp(), table, "  echo  one  two\nthree ", nil)
	if strings.Join(got.Fields, "|") != "one|two|three" {
		t.Errorf("Fields = %q, want [one two three]", got.Fields)
	}
	if got.Rest != "one  two\nthree " {
		t.Errorf("Rest = %q, want the raw remainder with inner whitespace kept", got.Rest)
	}
}

func TestDispatchCommand_UnknownFallback(t *testing.T) {
	var gotName string
	var gotArgs commandArgs
	_ = dispatchCommand(NewApp(), map[string]commandFunc{}, "nope a b", func(_ *App, name string, args commandArgs) tea.Cmd {
		gotName, gotArgs = name, args
		return nil
	})
	if gotName != "nope" || gotArgs.Rest != "a b" {
		t.Errorf("unknown(%q, %+v), want (nope, Rest \"a b\")", gotName, gotArgs)
	}
}

func TestDispatchCommand_EmptyIsNoop(t *testing.T) {
	called := false
	cmd := dispatchCommand(NewApp(), map[string]commandFunc{}, " \n ", func(*App, string, commandArgs) tea.Cmd {
		called = true
		return nil
	})
	if cmd != nil || called {
		t.Error("empty line should neither return a cmd nor reach unknown")
	}
}

// Rest and Fields agree on what whitespace is: a non-breaking space
// before the name or after it is skipped like an ASCII one.
func TestSplitCommandLine_UnicodeSpace(t *testing.T) {
	name, args, ok := splitCommandLine("\u00a0ws\u00a0a  b")
	if !ok || name != "ws" {
		t.Fatalf("splitCommandLine = (%q, %v), want ws", name, ok)
	}
	if args.Rest != "a  b" || strings.Join(args.Fields, "|") != "a|b" {
		t.Errorf("args = %+v, want Rest \"a  b\" and Fields [a b]", args)
	}
}
