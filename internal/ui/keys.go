// internal/ui/keys.go
package ui

import "charm.land/bubbles/v2/key"

type KeyMap struct {
	Up                  key.Binding `help:"Navigation"`
	Down                key.Binding `help:"Navigation"`
	Left                key.Binding `help:"Navigation"`
	Right               key.Binding `help:"Navigation"`
	Enter               key.Binding `help:"Navigation"`
	Escape              key.Binding `help:"Navigation"`
	InsertMode          key.Binding `help:"Composer"`
	CommandMode         key.Binding `help:"Commands"`
	SearchMode          key.Binding `help:"Search"`
	SearchNext          key.Binding `help:"Search"`
	SearchPrev          key.Binding `help:"Search"`
	WorkspaceSearch     key.Binding `help:"Search"`
	Tab                 key.Binding `help:"Navigation"`
	ShiftTab            key.Binding `help:"Navigation"`
	ToggleSidebar       key.Binding `help:"Windows"`
	SidebarGrow         key.Binding `help:"Windows"`
	SidebarShrink       key.Binding `help:"Windows"`
	ToggleThread        key.Binding `help:"Threads"`
	FuzzyFinder         key.Binding `help:"Navigation"`
	FuzzyFinderAlt      key.Binding `help:"Navigation"`
	Top                 key.Binding `help:"Navigation"`
	Bottom              key.Binding `help:"Navigation"`
	PageUp              key.Binding `help:"Navigation"`
	PageDown            key.Binding `help:"Navigation"`
	HalfPageUp          key.Binding `help:"Navigation"`
	HalfPageDown        key.Binding `help:"Navigation"`
	Quit                key.Binding `help:"App"`
	QuitConfirm         key.Binding `help:"App"`
	CloseThreadView     key.Binding `help:"Threads"`
	Reaction            key.Binding `help:"Messages"`
	ReactionNav         key.Binding `help:"Messages"`
	Edit                key.Binding `help:"Messages"`
	Delete              key.Binding `help:"Messages"`
	CopyMessage         key.Binding `help:"Messages"`
	CopyPermalink       key.Binding `help:"Messages"`
	ForwardMessage      key.Binding `help:"Messages"`
	OpenPreview         key.Binding `help:"Messages"`
	OpenLink            key.Binding `help:"Messages"`
	DownloadFile        key.Binding `help:"Messages"`
	MarkUnread          key.Binding `help:"Messages"`
	MarkSet             key.Binding `help:"Navigation"`
	JumpMark            key.Binding `help:"Navigation"`
	MarkBackJump        key.Binding `help:"Navigation"`
	MarksList           key.Binding `help:"Commands"`
	MarksDelete         key.Binding `help:"Commands"`
	NextUnread          key.Binding `help:"Navigation"`
	PrevUnread          key.Binding `help:"Navigation"`
	ActivityView        key.Binding `help:"Views"`
	ActivityUnread      key.Binding `help:"Views"`
	WorkspaceFinder     key.Binding `help:"Commands"`
	NewMessage          key.Binding `help:"Messages"`
	ThemeSwitcher       key.Binding `help:"App"`
	ThemeSwitcherGlobal key.Binding `help:"App"`
	ThemeNext           key.Binding `help:"App"`
	ThemePrev           key.Binding `help:"App"` // alt+Y: tmux extended-keys; alt+shift+Y: xterm modifyOtherKeys
	PresenceMenu        key.Binding `help:"App"`
	ToggleSection       key.Binding `help:"Navigation"`
	NavBack             key.Binding `help:"Navigation"`
	NavForward          key.Binding `help:"Navigation"`
	Help                key.Binding `help:"App"`
	SaveThread          key.Binding `help:"Threads"`
	RespondButtons      key.Binding `help:"Messages"`
	ListReactions       key.Binding `help:"Messages"`
	UserProfile         key.Binding `help:"Messages"`
	WindowPrefix        key.Binding `help:"Windows"`
	WinSplit            key.Binding `help:"Windows"`
	WinVSplit           key.Binding `help:"Windows"`
	WinNavigate         key.Binding `help:"Windows"`
	WinCycle            key.Binding `help:"Windows"`
	WinClose            key.Binding `help:"Windows"`
	WinOnly             key.Binding `help:"Windows"`
	ToggleBroadcast     key.Binding `help:"Threads"`
	OpenInEditor        key.Binding `help:"Composer"`
	ClearCompose        key.Binding `help:"Composer"`
}

func DefaultKeyMap() KeyMap {
	return KeyMap{
		Up:              key.NewBinding(key.WithKeys("k", "up"), key.WithHelp("k/up", "up")),
		Down:            key.NewBinding(key.WithKeys("j", "down"), key.WithHelp("j/down", "down")),
		Left:            key.NewBinding(key.WithKeys("h", "left"), key.WithHelp("h/left", "left")),
		Right:           key.NewBinding(key.WithKeys("l", "right"), key.WithHelp("l/right", "right")),
		Enter:           key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open/confirm")),
		Escape:          key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		InsertMode:      key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "insert mode")),
		CommandMode:     key.NewBinding(key.WithKeys(":"), key.WithHelp(":", "command mode")),
		SearchMode:      key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search")),
		SearchNext:      key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "next match")),
		SearchPrev:      key.NewBinding(key.WithKeys("N"), key.WithHelp("N", "prev match")),
		WorkspaceSearch: key.NewBinding(key.WithKeys("ctrl+f"), key.WithHelp("ctrl+f", "search workspace")),
		Tab:             key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next panel")),
		ShiftTab:        key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "prev panel")),
		ToggleSidebar:   key.NewBinding(key.WithKeys("ctrl+b"), key.WithHelp("ctrl+b", "toggle sidebar")),
		SidebarGrow:     key.NewBinding(key.WithKeys("]"), key.WithHelp("]", "widen sidebar")),
		SidebarShrink:   key.NewBinding(key.WithKeys("["), key.WithHelp("[", "narrow sidebar")),
		ToggleThread:    key.NewBinding(key.WithKeys("ctrl+]"), key.WithHelp("ctrl+]", "toggle thread")),
		FuzzyFinder:     key.NewBinding(key.WithKeys("ctrl+t"), key.WithHelp("ctrl+t", "switch channel")),
		FuzzyFinderAlt:  key.NewBinding(key.WithKeys("ctrl+p"), key.WithHelp("ctrl+p", "switch channel")),
		Top:             key.NewBinding(key.WithKeys("g"), key.WithHelp("gg", "top")),
		Bottom:          key.NewBinding(key.WithKeys("G"), key.WithHelp("G", "bottom")),
		PageUp:          key.NewBinding(key.WithKeys("pgup"), key.WithHelp("PgUp", "page up")),
		PageDown:        key.NewBinding(key.WithKeys("pgdown"), key.WithHelp("PgDn", "page down")),
		HalfPageUp:      key.NewBinding(key.WithKeys("ctrl+u"), key.WithHelp("ctrl+u", "half page up")),
		HalfPageDown:    key.NewBinding(key.WithKeys("ctrl+d"), key.WithHelp("ctrl+d", "half page down")),
		Quit:            key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "quit (confirm)")),
		QuitConfirm:     key.NewBinding(key.WithKeys("Q"), key.WithHelp("Q / :quit", "quit (confirm)")),
		CloseThreadView: key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "close thread view")),
		Reaction:        key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "add reaction")),
		ReactionNav:     key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "navigate reactions")),
		Edit:            key.NewBinding(key.WithKeys("E"), key.WithHelp("E", "edit message")),
		Delete:          key.NewBinding(key.WithKeys("D"), key.WithHelp("D", "delete message")),
		CopyMessage:     key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "copy message")),
		CopyPermalink:   key.NewBinding(key.WithKeys("Y", "C"), key.WithHelp("Y/C", "copy permalink")),
		ForwardMessage:  key.NewBinding(key.WithKeys("F"), key.WithHelp("F", "forward message")),
		OpenPreview:     key.NewBinding(key.WithKeys("O", "v"), key.WithHelp("O/v", "open image preview")),
		OpenLink:        key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "open link in message")),
		DownloadFile:    key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "download file in message")),
		MarkUnread:      key.NewBinding(key.WithKeys("U"), key.WithHelp("U", "mark unread")),
		MarkSet:         key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "set mark")),
		JumpMark:        key.NewBinding(key.WithKeys("'", "`"), key.WithHelp("'/`", "jump to mark")),
		// Keyless help-only entries (same trick as WorkspaceFinder
		// below). The back-jump is the doubled apostrophe consumed
		// inside the ' chord, and the two commands are dispatched from
		// the command registry, so none of them is a KeyMap binding —
		// but all three need to appear in the help overlay.
		MarkBackJump:   key.NewBinding(key.WithHelp("''", "back-jump to where you jumped from")),
		MarksList:      key.NewBinding(key.WithHelp(":marks", "list marks")),
		MarksDelete:    key.NewBinding(key.WithHelp(":delmarks", "delete marks by letter")),
		NextUnread:     key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "next unread channel")),
		PrevUnread:     key.NewBinding(key.WithKeys("A"), key.WithHelp("A", "prev unread channel")),
		ActivityView:   key.NewBinding(key.WithKeys("ctrl+a"), key.WithHelp("ctrl+a", "open activity")),
		ActivityUnread: key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "Activity: toggle unread-only")),
		// Keyless: ctrl+w is reserved as the window-command prefix
		// (window-management design §4). The keyless binding never
		// matches but keeps the help-overlay entry pointing at :ws
		// (1-9 also switch workspaces directly).
		WorkspaceFinder:     key.NewBinding(key.WithHelp(":ws", "switch workspace")),
		NewMessage:          key.NewBinding(key.WithKeys("ctrl+n"), key.WithHelp("ctrl+n", "new message")),
		ThemeSwitcher:       key.NewBinding(key.WithKeys("ctrl+y"), key.WithHelp("ctrl+y", "switch theme (per workspace)")),
		ThemeSwitcherGlobal: key.NewBinding(key.WithKeys("ctrl+shift+y"), key.WithHelp("ctrl+shift+y", "set default theme")),
		ThemeNext:           key.NewBinding(key.WithKeys("alt+y"), key.WithHelp("alt+y", "next theme (per workspace)")),
		ThemePrev:           key.NewBinding(key.WithKeys("alt+shift+y", "alt+Y", "alt+shift+Y"), key.WithHelp("alt+shift+y", "prev theme (per workspace)")),
		PresenceMenu:        key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("ctrl+s", "set status")),
		ToggleSection:       key.NewBinding(key.WithKeys("space"), key.WithHelp("space", "toggle section")),
		NavBack:             key.NewBinding(key.WithKeys("ctrl+h"), key.WithHelp("ctrl+h", "navigate back")),
		// ctrl+k was the original forward key. It stays bound for
		// existing users but is left out of the help overlay.
		NavForward:     key.NewBinding(key.WithKeys("ctrl+l", "ctrl+k"), key.WithHelp("ctrl+l", "navigate forward")),
		Help:           key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "show keybindings")),
		SaveThread:     key.NewBinding(key.WithKeys("S"), key.WithHelp("S", "save thread")),
		RespondButtons: key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "respond to message buttons")),
		ListReactions:  key.NewBinding(key.WithKeys("L"), key.WithHelp("L", "list reactions")),
		UserProfile:    key.NewBinding(key.WithKeys("K"), key.WithHelp("K", "show author's profile")),
		// Window commands (design §4). WindowPrefix is the only real
		// binding; the Win* entries are keyless help-only bindings
		// (same trick as WorkspaceFinder above) — actual dispatch of
		// the chord key happens in handleWindowChord.
		WindowPrefix: key.NewBinding(key.WithKeys("ctrl+w"), key.WithHelp("ctrl+w", "window commands")),
		WinSplit:     key.NewBinding(key.WithHelp("ctrl+w s / :sp", "split window")),
		WinVSplit:    key.NewBinding(key.WithHelp("ctrl+w v / :vsp", "vertical split window")),
		WinNavigate:  key.NewBinding(key.WithHelp("ctrl+w h/j/k/l", "focus window in direction")),
		WinCycle:     key.NewBinding(key.WithHelp("ctrl+w w", "cycle windows")),
		WinClose:     key.NewBinding(key.WithHelp("ctrl+w q / :q", "close window")),
		WinOnly:      key.NewBinding(key.WithHelp("ctrl+w o / :only", "close other windows")),
		// Insert mode, thread compose only: toggles Slack's
		// "Also send to #channel" checkbox for the next reply.
		// Alt+Enter sends and broadcasts in a single keystroke.
		ToggleBroadcast: key.NewBinding(key.WithKeys("ctrl+o"), key.WithHelp("ctrl+o / alt+enter", "also send reply to channel")),
		// ctrl+e is left to the textarea (end of line); ctrl+x hands the
		// draft to the external editor.
		OpenInEditor: key.NewBinding(key.WithKeys("ctrl+x"), key.WithHelp("ctrl+x", "edit message in $EDITOR")),
		// Insert mode: clears every line of the focused composer plus its
		// pending attachments (handled in mode_insert.go).
		ClearCompose: key.NewBinding(key.WithKeys("ctrl+u"), key.WithHelp("ctrl+u", "clear message input")),
	}
}
