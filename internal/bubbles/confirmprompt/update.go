package confirmprompt

import (
	"unicode"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// Update handles one key press, in this order: a button's keys press it;
// Enter presses the highlighted button; Esc closes; h/l/arrows/Tab move the
// highlight; anything else closes when the choice cancels on other keys and
// is ignored otherwise. Enter-on-highlight and movement are off when the
// choice cancels on other keys, which keeps the yes/no preset exactly as it
// was: its keys are y/Y/Enter, n/N/Esc, and everything else cancels.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	m.confirmed = false
	press, ok := msg.(tea.KeyPressMsg)
	if !ok || !m.visible {
		return m, nil
	}
	for i := range m.choice.Buttons {
		if matches(press, m.button(i).Keys) {
			return m.press(i)
		}
	}
	free := !m.choice.CancelOnOtherKeys
	switch {
	case free && matches(press, m.KeyMap.Press):
		return m.PressHighlighted()
	case matches(press, m.KeyMap.Close):
		m.Close()
		return m, nil
	case free && key.Matches(press, m.KeyMap.Next):
		m.move(1)
		return m, nil
	case free && key.Matches(press, m.KeyMap.Prev):
		m.move(-1)
		return m, nil
	}
	if m.choice.CancelOnOtherKeys {
		m.Close()
	}
	return m, nil
}

// PressHighlighted presses the highlighted button, or does nothing when
// none is. Hosts use it for a mouse click after Highlight.
func (m Model) PressHighlighted() (Model, tea.Cmd) {
	m.confirmed = false
	if !m.visible || m.highlight < 0 {
		return m, nil
	}
	return m.press(m.highlight)
}

func (m Model) press(i int) (Model, tea.Cmd) {
	b := m.button(i)
	var cmd tea.Cmd
	if b.Action != nil {
		fn := b.Action
		cmd = func() tea.Msg { return fn() }
	}
	confirmed := m.preset && i == presetConfirm
	if b.Then != nil {
		m.OpenChoice(*b.Then)
	} else {
		m.Close()
	}
	m.confirmed = confirmed
	return m, cmd
}

func (m *Model) move(delta int) {
	n := len(m.choice.Buttons)
	if n == 0 {
		return
	}
	if m.highlight < 0 {
		if delta > 0 {
			m.highlight = 0
		} else {
			m.highlight = n - 1
		}
		return
	}
	m.highlight = ((m.highlight+delta)%n + n) % n
}

// matches reports whether press triggers b. A modifier on a non-printable
// key is ignored, so a stray shift on Enter still counts. On a printable key
// the modifier is part of the key: ctrl+y and alt+y are not y. The test is on
// the base key's Code, not on Text, because bubbletea leaves Text empty for a
// ctrl- or alt-modified letter too.
func matches(press tea.KeyPressMsg, b key.Binding) bool {
	if key.Matches(press, b) {
		return true
	}
	if press.Mod == 0 || printable(press.Code) {
		return false
	}
	press.Mod = 0
	return key.Matches(press, b)
}

// printable reports whether code is a printable character rather than a
// special key (Enter, Esc, arrows, ...). Special keys above unicode.MaxRune
// are never printable.
func printable(code rune) bool {
	return code <= unicode.MaxRune && unicode.IsPrint(code)
}
