package messages

import (
	"github.com/gammons/slk/internal/mention"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/gammons/slk/internal/bubbles/entrylist"
	"github.com/gammons/slk/internal/ui/styles"
)

// WrapStyles are the lipgloss styles that turn a rendered message into
// list rows: the fill for the unselected row, the invisible and the
// tinted left border, and the mention bar and tint. Build them once per
// cache build (NewWrapStyles) and reuse them for every message, rather
// than allocating per message.
type WrapStyles struct {
	fill     lipgloss.Style
	invis    lipgloss.Style
	selected lipgloss.Style
	focused  bool

	// Mention marks (docs/superpowers/specs/2026-10-15-mention-highlight-design.md):
	// a broadcast gets mentionBar on the theme background; a direct
	// mention gets mentionTinted, its content repainted from bgParams to
	// mentionBgParams and filled with mentionFill.
	mentionBar      lipgloss.Style
	mentionTinted   lipgloss.Style
	mentionFill     lipgloss.Style
	bgParams        string
	mentionBgParams string

	// stats, when non-nil, collects per-sub-step durations (channel
	// pane perf instrumentation).
	stats *entryPerfStats
}

// NewWrapStyles returns the styles for the current theme; focused picks
// the selection colours.
func NewWrapStyles(focused bool) WrapStyles {
	return WrapStyles{
		fill: lipgloss.NewStyle().Background(styles.Background),
		invis: lipgloss.NewStyle().BorderStyle(thickLeftBorder).BorderLeft(true).
			BorderForeground(styles.Background).BorderBackground(styles.Background),
		selected: lipgloss.NewStyle().BorderStyle(thickLeftBorder).BorderLeft(true).
			BorderForeground(styles.SelectionBorderColor(focused)).
			BorderBackground(styles.SelectionTintColor(focused)).
			Background(styles.SelectionTintColor(focused)),
		focused: focused,
		mentionBar: lipgloss.NewStyle().BorderStyle(thickLeftBorder).BorderLeft(true).
			BorderForeground(styles.MentionBar).BorderBackground(styles.Background),
		mentionTinted: lipgloss.NewStyle().BorderStyle(thickLeftBorder).BorderLeft(true).
			BorderForeground(styles.MentionBar).BorderBackground(styles.MentionBg),
		mentionFill:     lipgloss.NewStyle().Background(styles.MentionBg),
		bgParams:        bgSGRParams(BgANSI()),
		mentionBgParams: bgSGRParams(bgANSIFor(styles.MentionBg)),
	}
}

// MentionVariant renders content as an unselected row that mentions the
// user (kind is not mention.None): filled to the row width and bordered
// with the mention bar, tinted for a direct mention. Split on "\n". For a
// row drawn outside WrapRows, such as the thread parent.
func (s WrapStyles) MentionVariant(content string, width int, kind mention.Kind) []string {
	return strings.Split(s.unselected(s.fill.Width(width-1).Render(content), content, width, kind), "\n")
}

// unselected borders filled, the row as drawn without the cursor on it:
// the invisible border, or the mention bar and tint for kind.
func (s WrapStyles) unselected(filled, content string, width int, kind mention.Kind) string {
	switch kind {
	case mention.Direct:
		if s.bgParams != "" && s.bgParams != s.mentionBgParams {
			content = substituteBgSGR(content, s.bgParams, s.mentionBgParams)
		}
		return s.mentionTinted.Render(s.mentionFill.Width(width - 1).Render(content))
	case mention.Broadcast:
		return s.mentionBar.Render(filled)
	}
	return s.invis.Render(filled)
}

// SelectedVariant renders content as the selected row: the selection
// tint repainted over inner spans, filled to the row width, then the
// selection border. Split on "\n".
func (s WrapStyles) SelectedVariant(content string, width int) []string {
	fill := lipgloss.NewStyle().Background(styles.SelectionTintColor(s.focused)).Width(width - 1)
	return strings.Split(s.selected.Render(fill.Render(RepaintBgToSelectionTint(content, s.focused))), "\n")
}

// WrappedRows are the rows of one message, bordered and filled, ready
// for an entrylist.Entry. A pane adds what is its own: ID, Ref, hits, and
// the channel pane's trailing spacer rows.
type WrappedRows struct {
	// Lines is the unselected row, with the invisible left border, or the
	// mention bar (and, for a direct mention, the mention tint).
	Lines []string
	// Selected is the selected row, with the tinted border and a
	// date-qualified header timestamp.
	Selected []string
	// Plain mirrors the unbordered, filled content, so the border column
	// never reaches clipboard text. Pair it with Entry.ColOffset = 1.
	Plain []entrylist.PlainLine
	// ShortSrc is the content the short-timestamp selected variant is
	// built from (EntryPayload.SelectedLines); empty when Selected
	// already carries the short form.
	ShortSrc string
}

// WrapRows fills and borders a rendered message for a content pane's
// list. Only the selected variant carries the date-qualified timestamp:
// Lines and Plain keep the short form, so clipboard text and the
// mouse-to-column mapping are unchanged. The short-timestamp selected
// variant is not built here (it would double the fill and border cost of
// every cache build); ShortSrc lets EntryPayload.SelectedLines build it on
// demand.
func WrapRows(msg MessageItem, r RenderedMessage, width int, s WrapStyles) WrappedRows {
	rendered := r.Content
	renderedSel := SelectedHeader(rendered, r.Header, msg.TS, msg.Timestamp, r.HeaderBudget)

	// Two filled variants: the theme background for the unselected row
	// and the selection tint for the selected one. Without per-variant
	// fills, the trailing whitespace of every wrapped line shows the
	// wrong background and the tint stops at the last character of
	// content. For the selected variant every theme-background escape
	// inside the content is also swapped for the tint (SelectedVariant),
	// or inner spans would show through as dark patches.
	var bwT0 time.Time
	if s.stats != nil {
		bwT0 = time.Now()
	}
	filledNormal := s.fill.Width(width - 1).Render(rendered)
	normal := s.unselected(filledNormal, rendered, width, r.Mention)
	selected := s.SelectedVariant(renderedSel, width)
	var shortSrc string
	if renderedSel != rendered {
		shortSrc = rendered
	}
	if s.stats != nil {
		s.stats.borderWrapTotal += time.Since(bwT0)
	}

	var plT0 time.Time
	if s.stats != nil {
		plT0 = time.Now()
	}
	plain := entrylist.PlainLines(filledNormal)
	if s.stats != nil {
		s.stats.plainLinesTotal += time.Since(plT0)
	}

	return WrappedRows{
		Lines:    strings.Split(normal, "\n"),
		Selected: selected,
		Plain:    plain,
		ShortSrc: shortSrc,
	}
}
