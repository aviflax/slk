// internal/ui/edit.go
//
// In-progress message edit state.
//
// Phase 2e of the SOLID refactor of internal/ui/app.go: extracts the
// editState struct (renamed editController) and its associated reads
// out of App. The orchestrators that mutate compose drafts and toggle
// modes (beginEditOfSelected, cancelEdit, submitEdit) stay on App
// because they couple to multiple sub-models — but they now go
// through this type for all state access.
//
// When active, the channel-pane or thread-pane compose box is
// repurposed: its existing draft is stashed, the message text is
// seeded, and Enter submits the edit (submitEdit) instead of sending.
// Cancellation (Esc, channel switch, panel switch, MessageDeletedMsg
// for the edited message, etc.) restores the stashed draft.
package ui

// editController tracks an in-progress message edit.
type editController struct {
	active       bool
	channelID    string
	ts           string
	panel        Panel // PanelMessages or PanelThread
	stashedDraft string
}

func newEditController() *editController { return &editController{} }

// IsActive reports whether an edit is currently in progress.
func (e *editController) IsActive() bool { return e.active }

// Panel returns the panel hosting the edit (PanelMessages or
// PanelThread). Meaningless when !IsActive.
func (e *editController) Panel() Panel { return e.panel }

// ChannelID returns the parent channel of the message being edited.
// For PanelThread edits this is the channel hosting the thread, not
// the threadTS. Meaningless when !IsActive.
func (e *editController) ChannelID() string { return e.channelID }

// TS returns the Slack timestamp of the message being edited.
// Meaningless when !IsActive.
func (e *editController) TS() string { return e.ts }

// StashedDraft returns the compose-box text that was present when the
// edit started; restored on Cancel.
func (e *editController) StashedDraft() string { return e.stashedDraft }

// Matches reports whether an active edit targets the given message.
// Returns false when no edit is in progress.
func (e *editController) Matches(channelID, ts string) bool {
	return e.active && e.channelID == channelID && e.ts == ts
}

// Begin records the start of an edit. The caller is responsible for
// seeding the compose's text, setting the placeholder override, and
// switching to insert mode — this method only records state.
func (e *editController) Begin(channelID, ts string, panel Panel, stashedDraft string) {
	e.active = true
	e.channelID = channelID
	e.ts = ts
	e.panel = panel
	e.stashedDraft = stashedDraft
}

// Clear resets the controller to its inactive zero state. The caller
// is responsible for restoring the compose draft and exiting insert
// mode — this method only clears state.
func (e *editController) Clear() { *e = editController{} }
