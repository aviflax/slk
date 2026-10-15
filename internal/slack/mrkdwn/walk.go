package mrkdwn

import (
	"bytes"
	"strconv"
	"strings"

	"github.com/slack-go/slack"
	"github.com/yuin/goldmark/ast"
	extensionAST "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/util"
)

// walker accumulates two parallel outputs as it walks a goldmark AST:
// the mrkdwn fallback string (in mrkdwn) and a rich_text block (in block).
//
// The current rich_text section being assembled lives in curSection;
// inline element appenders (text, mention, link) push into it. Block-
// level appenders (paragraph, list, code-block) flush curSection into
// block.Elements first, then create a new container.
type walker struct {
	source []byte
	table  []token

	mrkdwn     strings.Builder
	block      *slack.RichTextBlock
	curSection *slack.RichTextSection

	// inheritedStyle is applied to every text element appended via
	// appendText. Inline-formatting walk methods toggle the relevant
	// flag for the duration of their child walk.
	inheritedStyle slack.RichTextSectionTextStyle

	// sectionLead holds newlines owed to the start of the next
	// paragraph's section: the blank lines typed between a list, code
	// block or quote and the paragraph after it.
	sectionLead string
}

func newWalker(source []byte, table []token) *walker {
	return &walker{
		source: source,
		table:  table,
		block:  slack.NewRichTextBlock(""),
	}
}

func (w *walker) walkDocument(n ast.Node) {
	var prev ast.Node
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		if prev != nil {
			w.separateBlocks(prev, c)
		}
		w.walkBlock(c)
		prev = c
	}
	w.flushSection()
}

// separateBlocks writes the line breaks the user typed between two
// top-level blocks. The mrkdwn fallback gets all of them. In the block,
// paragraphs share one rich_text_section with the breaks as text, the
// shape Slack's web client sends: Slack draws sibling sections with no
// break between them, so a section per paragraph lost every blank
// line. A list, code block or quote already starts a new line, so only
// the blank lines beyond that one go into the neighbouring section.
func (w *walker) separateBlocks(prev, next ast.Node) {
	n := w.lineBreaksBetween(prev, next)
	w.mrkdwn.WriteString(strings.Repeat("\n", n))

	_, prevPara := prev.(*ast.Paragraph)
	_, nextPara := next.(*ast.Paragraph)
	switch {
	case prevPara && nextPara:
		w.appendTextElement(strings.Repeat("\n", n))
	case prevPara:
		if n > 1 {
			w.appendTextElement(strings.Repeat("\n", n-1))
		}
		w.flushSection()
	case nextPara:
		w.sectionLead = strings.Repeat("\n", n-1)
	}
}

// walkBlock dispatches top-level block nodes. A paragraph extends the
// open section (separateBlocks has put the breaks before it); every
// other block closes it and adds its own element.
func (w *walker) walkBlock(n ast.Node) {
	switch n := n.(type) {
	case *ast.Paragraph:
		if w.curSection == nil {
			w.curSection = slack.NewRichTextSection()
		}
		w.appendTextElement(w.sectionLead)
		w.sectionLead = ""
		w.appendText(w.indentAt(n, lineStart(w.source, n.Lines().At(0).Start)))
		w.walkInlineChildren(n)
	case *ast.List:
		w.walkList(n, 0)
	case *ast.FencedCodeBlock:
		w.walkCodeBlock(n)
	case *ast.Blockquote:
		w.walkQuote(n)
	default:
		// newParser builds no other block types; walk children as
		// inline so nothing is lost if that ever changes.
		w.walkInlineChildren(n)
	}
}

// flushSection moves the current in-progress section into block.Elements.
func (w *walker) flushSection() {
	if w.curSection != nil && len(w.curSection.Elements) > 0 {
		w.block.Elements = append(w.block.Elements, w.curSection)
	}
	w.curSection = nil
}

// walkInlineChildren walks the children of n as inline content.
func (w *walker) walkInlineChildren(n ast.Node) {
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		w.walkInline(c)
	}
}

func (w *walker) walkInline(n ast.Node) {
	switch n := n.(type) {
	case *ast.Text:
		seg := n.Segment
		// CommonMark backslash-escapes (\*, \_, etc.) appear in the
		// source segment as the literal "\X". Unescape so they emit
		// as literal X without inline formatting.
		w.appendText(string(util.UnescapePunctuations(w.source[seg.Start:seg.Stop])))
		if n.HardLineBreak() || n.SoftLineBreak() {
			// Slack chat preserves line layout; treat both hard and
			// soft breaks as literal newlines, keeping the spaces
			// around them that the parser trims.
			w.appendText(w.lineBreak(n))
		}
	case *ast.RawHTML:
		// Inline HTML (e.g. <span class=...>) — copy source bytes via
		// the segments slice so it appears as literal text. Same rationale
		// as walkRawHTMLBlock above.
		var b strings.Builder
		for i := 0; i < n.Segments.Len(); i++ {
			seg := n.Segments.At(i)
			b.Write(w.source[seg.Start:seg.Stop])
		}
		if s := b.String(); s != "" {
			w.appendText(s)
		}
	case *ast.Emphasis:
		if n.Level == 2 {
			w.walkBold(n)
			return
		}
		// Level 1: distinguish _italic_ from *italic*. Goldmark's
		// emphasis node doesn't expose the delimiter byte directly,
		// so we look at the byte immediately before the first child
		// text segment. Underscore is CommonMark italic; a single
		// asterisk pair is Slack's own native bold syntax, so treat
		// it as bold (same as walkBold) rather than reinterpreting
		// it as CommonMark italic — that keeps outgoing messages
		// consistent with both real Slack's rendering and slk's own
		// message view, which already displays "*word*" as bold.
		if w.emphasisDelimiter(n) == '_' {
			w.walkItalic(n)
		} else {
			w.walkBold(n)
		}
	case *ast.CodeSpan:
		w.mrkdwn.WriteString("`")
		prev := w.inheritedStyle
		w.inheritedStyle.Code = true
		w.walkInlineChildren(n)
		w.inheritedStyle = prev
		w.mrkdwn.WriteString("`")
	case *ast.Link:
		w.handleLink(n)
	case *ast.AutoLink:
		w.handleAutoLink(n)
	case *extensionAST.Strikethrough:
		w.mrkdwn.WriteString("~")
		prev := w.inheritedStyle
		w.inheritedStyle.Strike = true
		w.walkInlineChildren(n)
		w.inheritedStyle = prev
		w.mrkdwn.WriteString("~")
	default:
		// Other inline nodes are handled in later tasks. Walk
		// children to preserve text.
		w.walkInlineChildren(n)
	}
}

// walkBold emits *body* mrkdwn and sets the bold style flag for the
// duration of the inline-children walk. Save/restore inheritedStyle
// so nested formatting (e.g. **bold _italic_**) correctly composes
// the styles.
func (w *walker) walkBold(n *ast.Emphasis) {
	w.mrkdwn.WriteString("*")
	prev := w.inheritedStyle
	w.inheritedStyle.Bold = true
	w.walkInlineChildren(n)
	w.inheritedStyle = prev
	w.mrkdwn.WriteString("*")
}

// emphasisDelimiter returns the byte used to open the emphasis node n
// ('_' or '*'). The source contains a stack of opening delimiters
// before the first text descendant: outermost first, innermost last.
// To locate n's own opener we skip past the openers belonging to any
// emphasis nodes nested INSIDE n (each Level-1 emphasis consumes one
// byte; a Level-2 bold consumes two), then read the byte that n itself
// contributed. Returns '_' as a safe default for malformed input.
func (w *walker) emphasisDelimiter(n *ast.Emphasis) byte {
	first := findFirstTextDescendant(n)
	if first == nil {
		return '_'
	}
	// Sum of delimiter widths for emphasis nodes strictly between n
	// and the text descendant.
	innerWidth := 0
	for c := n.FirstChild(); c != nil; {
		em, ok := c.(*ast.Emphasis)
		if !ok {
			// Walk into non-emphasis container looking for the path
			// to first; if we find emphasis ancestors of first, they
			// add to innerWidth too. For our current grammar this
			// branch is unused (emphasis only nests directly), but
			// stay defensive.
			next := c.FirstChild()
			if next == nil {
				break
			}
			c = next
			continue
		}
		innerWidth += em.Level
		c = em.FirstChild()
	}
	pos := first.Segment.Start - innerWidth - 1
	if pos < 0 || pos >= len(w.source) {
		return '_'
	}
	b := w.source[pos]
	if b != '_' && b != '*' {
		return '_'
	}
	return b
}

// findFirstTextDescendant returns the leftmost *ast.Text node under n,
// or nil if there isn't one.
func findFirstTextDescendant(n ast.Node) *ast.Text {
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		if t, ok := c.(*ast.Text); ok {
			return t
		}
		if t := findFirstTextDescendant(c); t != nil {
			return t
		}
	}
	return nil
}

// walkItalic emits _x_ mrkdwn and sets Style.Italic for the duration
// of the inline-children walk.
func (w *walker) walkItalic(n *ast.Emphasis) {
	w.mrkdwn.WriteString("_")
	prev := w.inheritedStyle
	w.inheritedStyle.Italic = true
	w.walkInlineChildren(n)
	w.inheritedStyle = prev
	w.mrkdwn.WriteString("_")
}

// handleLink emits a CommonMark [label](url) as Slack mrkdwn
// <url|label> and a RichTextSectionLinkElement in the block.
//
// If the URL contains '|', we emit the bare-URL form <url> on the
// mrkdwn side (Slack's wire format has no escape mechanism for pipes
// in URLs; with a label, the parser would split on the first pipe
// and produce a wrong URL). The block element still carries URL and
// label as separate fields, so block-rendering Slack clients see the
// labeled link correctly.
func (w *walker) handleLink(n *ast.Link) {
	url := string(n.Destination)
	label := w.collectInlineText(n)

	if strings.Contains(url, "|") {
		w.mrkdwn.WriteString("<")
		w.mrkdwn.WriteString(url)
		w.mrkdwn.WriteString(">")
	} else {
		w.mrkdwn.WriteString("<")
		w.mrkdwn.WriteString(url)
		w.mrkdwn.WriteString("|")
		w.mrkdwn.WriteString(label)
		w.mrkdwn.WriteString(">")
	}

	if w.curSection == nil {
		w.curSection = slack.NewRichTextSection()
	}
	link := slack.NewRichTextSectionLinkElement(url, label, w.copyStyle())
	w.curSection.Elements = append(w.curSection.Elements, link)
}

// handleAutoLink emits a goldmark AutoLink (produced by the Linkify
// extension or by CommonMark `<https://…>` syntax that wasn't caught
// by the pre-goldmark wire-form tokenizer) as Slack mrkdwn and as a
// RichTextSectionLinkElement.
//
// AutoLink.URL() returns the canonical URL (with `http://` prepended
// for www-only matches and `mailto:` prepended for emails). Label()
// returns the bytes the user actually typed. When the two match we
// emit `<url>`; otherwise we emit `<url|label>` so the mrkdwn
// fallback preserves the original surface form.
func (w *walker) handleAutoLink(n *ast.AutoLink) {
	url := string(n.URL(w.source))
	label := string(n.Label(w.source))

	// goldmark's Linkify extension does not set Protocol for email
	// matches, so AutoLink.URL() returns the bare address. Slack
	// needs an explicit mailto: scheme on the link element to render
	// it as a clickable mail link.
	if n.AutoLinkType == ast.AutoLinkEmail && !strings.HasPrefix(url, "mailto:") {
		url = "mailto:" + url
	}

	w.mrkdwn.WriteString("<")
	w.mrkdwn.WriteString(url)
	if label != "" && label != url {
		w.mrkdwn.WriteString("|")
		w.mrkdwn.WriteString(label)
	}
	w.mrkdwn.WriteString(">")

	if w.curSection == nil {
		w.curSection = slack.NewRichTextSection()
	}
	link := slack.NewRichTextSectionLinkElement(url, label, w.copyStyle())
	w.curSection.Elements = append(w.curSection.Elements, link)
}

// collectInlineText concatenates the text content of n's children,
// stripping inline formatting markers. Used for link labels where
// Slack's wire form expects plain text after the '|'.
func (w *walker) collectInlineText(n ast.Node) string {
	var b strings.Builder
	var walk func(ast.Node)
	walk = func(c ast.Node) {
		if t, ok := c.(*ast.Text); ok {
			b.Write(w.source[t.Segment.Start:t.Segment.Stop])
			return
		}
		for cc := c.FirstChild(); cc != nil; cc = cc.NextSibling() {
			walk(cc)
		}
	}
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		walk(c)
	}
	// Restore Slack wire-form tokens that may live inside the label.
	return detokenizeText(b.String(), w.table)
}

// walkList emits a rich_text_list block (flat at Slack's level —
// nested CommonMark lists are emitted as separate top-level lists
// with increasing Indent). Mrkdwn fallback uses U+2022 bullets for
// unordered and "N. " for ordered, with two-space indent per level.
func (w *walker) walkList(n *ast.List, indent int) {
	w.flushSection()

	style := slack.RTEListBullet
	if n.IsOrdered() {
		style = slack.RTEListOrdered
	}

	list := slack.NewRichTextList(style, indent)

	// Nested lists from this list's children are emitted as sibling
	// top-level lists (Slack flat-with-indent shape). Collect them
	// here so they append AFTER this list in w.block.Elements.
	var nested []*slack.RichTextList

	itemIdx := 0
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		item, ok := c.(*ast.ListItem)
		if !ok {
			continue
		}
		sec := slack.NewRichTextSection()
		prev := w.curSection
		w.curSection = sec

		// Mrkdwn marker for the item.
		w.mrkdwn.WriteString(strings.Repeat("  ", indent))
		if n.IsOrdered() {
			w.mrkdwn.WriteString(strconv.Itoa(itemIdx + 1))
			w.mrkdwn.WriteString(". ")
		} else {
			w.mrkdwn.WriteString("• ")
		}
		itemIdx++

		// Walk item children: inline body then any nested lists.
		// Paragraphs of one item (a loose list) keep the breaks
		// typed between them.
		var prevText ast.Node
		for sub := item.FirstChild(); sub != nil; sub = sub.NextSibling() {
			switch sub := sub.(type) {
			case *ast.TextBlock, *ast.Paragraph:
				if prevText != nil {
					w.appendText(strings.Repeat("\n", w.lineBreaksBetween(prevText, sub)))
				}
				w.walkInlineChildren(sub)
				prevText = sub
			case *ast.List:
				w.mrkdwn.WriteString("\n")
				w.walkListInto(sub, indent+1, &nested)
			default:
				w.walkInline(sub)
			}
		}
		w.mrkdwn.WriteString("\n")

		w.curSection = prev
		list.Elements = append(list.Elements, sec)
	}
	// Trim the trailing newline added by the last item so that the
	// list doesn't add an extra blank line before the next block.
	trimTrailingNewline(&w.mrkdwn)

	w.block.Elements = append(w.block.Elements, list)
	for _, nl := range nested {
		w.block.Elements = append(w.block.Elements, nl)
	}
}

// walkListInto walks a nested list, appending its block-level result
// to the given slice (instead of w.block.Elements directly), so that
// the parent walkList can interleave nested lists in the right order.
func (w *walker) walkListInto(n *ast.List, indent int, out *[]*slack.RichTextList) {
	prev := w.block
	tmp := slack.NewRichTextBlock("")
	w.block = tmp
	w.walkList(n, indent)
	w.block = prev
	for _, e := range tmp.Elements {
		if l, ok := e.(*slack.RichTextList); ok {
			*out = append(*out, l)
		}
	}
}

// trimTrailingNewline removes one trailing '\n' from b if present.
func trimTrailingNewline(b *strings.Builder) {
	s := b.String()
	if strings.HasSuffix(s, "\n") {
		b.Reset()
		b.WriteString(s[:len(s)-1])
	}
}

// walkCodeBlock emits a fenced code block as ```body``` mrkdwn plus a
// RichTextPreformatted block. The body is the source lines verbatim,
// blank lines and indentation included. The block's text carries no
// final newline: the closing fence ends the line, and a trailing "\n"
// draws as an empty last line of code.
func (w *walker) walkCodeBlock(n ast.Node) {
	w.flushSection()
	var b strings.Builder
	for i := 0; i < n.Lines().Len(); i++ {
		seg := n.Lines().At(i)
		b.Write(w.source[seg.Start:seg.Stop])
	}
	body := strings.TrimSuffix(b.String(), "\n")
	// Slack has no info strings: text typed after the opening fence
	// ("```hello\nworld```") is the code block's first line, and
	// CommonMark would otherwise drop it as the language.
	if fc, ok := n.(*ast.FencedCodeBlock); ok && fc.Info != nil {
		info := string(w.source[fc.Info.Segment.Start:fc.Info.Segment.Stop])
		if body == "" {
			body = info
		} else {
			body = info + "\n" + body
		}
	}

	w.mrkdwn.WriteString("```\n")
	w.mrkdwn.WriteString(body)
	w.mrkdwn.WriteString("\n```")
	if body == "" {
		// An empty text element is not valid rich text; an empty
		// code block is one empty line.
		body = "\n"
	}

	// Code-block contents must be literal: any sentinels embedded by
	// tokenize (emoji shortcodes, <@user> mentions, etc.) need to
	// round-trip back to their original surface form rather than
	// leak raw PUA bytes through to Slack. detokenizeText restores
	// all sentinels to their wire-form representation.
	pre := &slack.RichTextPreformatted{
		Type: slack.RTEPreformatted,
		Elements: []slack.RichTextSectionElement{
			slack.NewRichTextSectionTextElement(detokenizeText(body, w.table), nil),
		},
	}
	w.block.Elements = append(w.block.Elements, pre)
}

// walkQuote emits a blockquote as a rich_text_quote, with the quote's
// line breaks and blank lines kept as text the way paragraphs keep
// them, and as "> "-prefixed lines in the mrkdwn fallback. A list, code
// block or nested quote inside it is kept as its source text, without
// the outer "> " markers: rich_text_quote holds inline elements only.
func (w *walker) walkQuote(n *ast.Blockquote) {
	w.flushSection()
	mark := w.mrkdwn.Len()
	sec := slack.NewRichTextSection()
	w.curSection = sec

	var prev ast.Node
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		if prev != nil {
			w.appendText(strings.Repeat("\n", w.lineBreaksBetween(prev, c)))
		}
		if p, ok := c.(*ast.Paragraph); ok {
			w.appendText(w.indentAt(p, lineStart(w.source, p.Lines().At(0).Start)))
			w.walkInlineChildren(p)
		} else {
			w.appendText(w.quotedSource(c))
		}
		prev = c
	}
	w.curSection = nil

	if len(sec.Elements) > 0 {
		w.block.Elements = append(w.block.Elements, &slack.RichTextQuote{
			Type:     slack.RTEQuote,
			Elements: sec.Elements,
		})
	}

	// Prefix every line the quote wrote to the fallback.
	s := w.mrkdwn.String()
	lines := strings.Split(s[mark:], "\n")
	for i, l := range lines {
		if l == "" {
			lines[i] = ">"
		} else {
			lines[i] = "> " + l
		}
	}
	w.mrkdwn.Reset()
	w.mrkdwn.WriteString(s[:mark])
	w.mrkdwn.WriteString(strings.Join(lines, "\n"))
}

// quotedSource returns the source lines of n, a block inside a quote,
// with one level of "> " marker removed from each.
func (w *walker) quotedSource(n ast.Node) string {
	start, last := w.firstLineStart(n), w.lastLineStart(n)
	if start < 0 || last < start {
		var b strings.Builder
		collectRawInline(&b, n, w.source)
		return b.String()
	}
	end := lineEnd(w.source, last)
	lines := strings.Split(string(w.source[start:end]), "\n")
	for i, l := range lines {
		l = strings.TrimLeft(l, " ")
		l = strings.TrimPrefix(l, ">")
		lines[i] = strings.TrimPrefix(l, " ")
	}
	return strings.Join(lines, "\n")
}

// collectRawInline appends the source bytes of all *ast.Text descendants
// of n to b.
func collectRawInline(b *strings.Builder, n ast.Node, source []byte) {
	if t, ok := n.(*ast.Text); ok {
		b.Write(source[t.Segment.Start:t.Segment.Stop])
		return
	}
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		collectRawInline(b, c, source)
	}
}

// lineBreak returns the text for the line break after t: the newline
// plus the whitespace around it that goldmark trims from the inline
// text, so spaces before the break and the next line's indentation
// reach Slack. A backslash hard break loses its backslash: that is
// markup, not text.
func (w *walker) lineBreak(t *ast.Text) string {
	stop := t.Segment.Stop
	eol := lineEnd(w.source, stop)
	if eol >= len(w.source) {
		return "\n"
	}
	trailing := string(w.source[stop:eol])
	if t.HardLineBreak() {
		trailing = strings.TrimSuffix(trailing, "\\")
	}
	if strings.Trim(trailing, " \t") != "" {
		// Not whitespace: some markup goldmark consumed sits
		// between the text and the line end. Keep only the break.
		trailing = ""
	}
	return trailing + "\n" + w.indentAt(t, eol+1)
}

// indentAt returns the indentation of the source line starting at
// start, where n is the inline or paragraph node that continues on it.
// At the top level that is every leading space and tab. In a quote it
// is what follows the "> " marker (a lazy line without the marker has
// none). In a list item it is always empty: there the leading spaces
// are the item's structure, not text.
func (w *walker) indentAt(n ast.Node, start int) string {
	inQuote := false
	for p := n.Parent(); p != nil; p = p.Parent() {
		switch p.(type) {
		case *ast.ListItem:
			return ""
		case *ast.Blockquote:
			inQuote = true
		}
	}
	src := w.source
	i := start
	if inQuote {
		for i < len(src) && src[i] == ' ' {
			i++
		}
		if i >= len(src) || src[i] != '>' {
			return ""
		}
		i++
		if i < len(src) && src[i] == ' ' {
			i++
		}
	}
	j := i
	for j < len(src) && (src[j] == ' ' || src[j] == '\t') {
		j++
	}
	return string(src[i:j])
}

// lineBreaksBetween counts the line breaks in the source between the
// last line of prev and the first line of next: 1 for adjacent lines,
// 2 for one blank line between them, and so on. It falls back to 1
// when either position is unknown.
func (w *walker) lineBreaksBetween(prev, next ast.Node) int {
	a, b := w.lastLineStart(prev), w.firstLineStart(next)
	if a < 0 || b <= a {
		return 1
	}
	return max(1, strings.Count(string(w.source[a:b]), "\n"))
}

// firstLineStart returns the source offset of the start of n's first
// line (the opening fence, for fenced code), or -1 if n has no lines.
func (w *walker) firstLineStart(n ast.Node) int {
	if fc, ok := n.(*ast.FencedCodeBlock); ok {
		switch {
		case fc.Info != nil:
			return lineStart(w.source, fc.Info.Segment.Start)
		case fc.Lines().Len() > 0:
			return prevLineStart(w.source, lineStart(w.source, fc.Lines().At(0).Start))
		}
		return -1
	}
	if n.Type() != ast.TypeInline && n.Lines().Len() > 0 {
		return lineStart(w.source, n.Lines().At(0).Start)
	}
	if c := n.FirstChild(); c != nil {
		return w.firstLineStart(c)
	}
	return -1
}

// lastLineStart returns the source offset of the start of n's last
// line (the closing fence, for fenced code that has one), or -1 if n
// has no lines.
func (w *walker) lastLineStart(n ast.Node) int {
	if fc, ok := n.(*ast.FencedCodeBlock); ok {
		var last int
		switch {
		case fc.Lines().Len() > 0:
			last = lineStart(w.source, fc.Lines().At(fc.Lines().Len()-1).Start)
		case fc.Info != nil:
			last = lineStart(w.source, fc.Info.Segment.Start)
		default:
			return -1
		}
		next := lineEnd(w.source, last) + 1
		if next < len(w.source) {
			// " >" also skips the markers of a quote the code is in.
			l := strings.TrimLeft(string(w.source[next:lineEnd(w.source, next)]), " >")
			if strings.HasPrefix(l, "```") || strings.HasPrefix(l, "~~~") {
				return next
			}
		}
		return last
	}
	if n.Type() != ast.TypeInline && n.Lines().Len() > 0 {
		return lineStart(w.source, n.Lines().At(n.Lines().Len()-1).Start)
	}
	if c := n.LastChild(); c != nil {
		return w.lastLineStart(c)
	}
	return -1
}

// lineStart returns the offset of the start of the line holding pos.
func lineStart(src []byte, pos int) int {
	return bytes.LastIndexByte(src[:pos], '\n') + 1
}

// lineEnd returns the offset of the '\n' ending the line holding pos,
// or len(src) on the last line.
func lineEnd(src []byte, pos int) int {
	if i := bytes.IndexByte(src[pos:], '\n'); i >= 0 {
		return pos + i
	}
	return len(src)
}

// prevLineStart returns the start of the line before the one starting
// at start, or start itself on the first line.
func prevLineStart(src []byte, start int) int {
	if start == 0 {
		return 0
	}
	return lineStart(src, start-1)
}

// appendText writes s to both outputs with the current inherited
// style. Slack wire-form sentinels embedded in s are split out into
// typed rich_text elements (user / channel / broadcast / link) and
// restored to their original <...> form in the mrkdwn output.
func (w *walker) appendText(s string) {
	if s == "" {
		return
	}
	// Mrkdwn side: write as-is. The detokenize pass at the end of
	// Convert restores all sentinels in one go.
	w.mrkdwn.WriteString(s)

	// Block side: split on sentinels.
	if w.curSection == nil {
		w.curSection = slack.NewRichTextSection()
	}
	i := 0
	for i < len(s) {
		idx, end, ok := parseSentinel(s, i)
		if !ok {
			// Find the next sentinel boundary (or end of string)
			// and emit the run between [i, j) as a text element.
			j := nextSentinelStart(s, i)
			if j > i {
				w.appendTextElement(s[i:j])
			}
			i = j
			continue
		}
		if idx >= 0 && idx < len(w.table) {
			w.appendTokenElement(w.table[idx])
		} else {
			// Out-of-range index, emit raw bytes.
			w.appendTextElement(s[i:end])
		}
		i = end
	}
}

// appendTextElement adds a text element to curSection, coalescing
// with the trailing element when it is also a text element carrying
// the same style. The Linkify extension parses text token-by-token
// so consecutive words arrive as separate ast.Text nodes; without
// coalescing every space-separated word becomes its own RichTextSection
// element on the wire, which is wasteful and uglies up debug output.
func (w *walker) appendTextElement(text string) {
	if text == "" {
		return
	}
	if w.curSection == nil {
		w.curSection = slack.NewRichTextSection()
	}
	style := w.copyStyle()
	if n := len(w.curSection.Elements); n > 0 {
		if prev, ok := w.curSection.Elements[n-1].(*slack.RichTextSectionTextElement); ok {
			if styleEqual(prev.Style, style) {
				prev.Text += text
				return
			}
		}
	}
	te := slack.NewRichTextSectionTextElement(text, style)
	w.curSection.Elements = append(w.curSection.Elements, te)
}

// appendTokenElement converts a wire-form token into the right typed
// element (or, for emoji tokens within code style, a literal text
// element). Routes through appendTextElement when the result is a
// text element so style-coalescing still applies.
func (w *walker) appendTokenElement(t token) {
	if el := w.tokenElement(t); el != nil {
		w.curSection.Elements = append(w.curSection.Elements, el)
	}
}

// styleEqual reports whether two style pointers describe the same
// style. nil and an all-zero struct are treated as equal — both mean
// "no style flags set" on the wire.
func styleEqual(a, b *slack.RichTextSectionTextStyle) bool {
	var av, bv slack.RichTextSectionTextStyle
	if a != nil {
		av = *a
	}
	if b != nil {
		bv = *b
	}
	return av == bv
}

// nextSentinelStart returns the byte index of the next sentinelStart
// rune in s at or after i, or len(s) if none.
func nextSentinelStart(s string, i int) int {
	idx := strings.IndexRune(s[i:], sentinelStart)
	if idx < 0 {
		return len(s)
	}
	return i + idx
}

// tokenElement converts a wire-form token into the corresponding
// rich_text element with the current inherited style applied where
// the schema supports a style.
func (w *walker) tokenElement(t token) slack.RichTextSectionElement {
	style := w.copyStyle()
	switch t.kind {
	case tokUser:
		return slack.NewRichTextSectionUserElement(t.id, style)
	case tokChannel:
		return slack.NewRichTextSectionChannelElement(t.id, style)
	case tokBroadcast:
		// <!subteam^SID> is a usergroup (@team) mention, not a
		// broadcast: it has its own rich_text element type. Sending it
		// as a broadcast range gets the whole message rejected with
		// invalid_blocks.
		if id, ok := strings.CutPrefix(t.id, "subteam^"); ok {
			el := slack.NewRichTextSectionUserGroupElement(id)
			el.Style = style
			return el
		}
		// Slack only accepts here / channel / everyone as broadcast
		// ranges; anything else is invalid_blocks. reBroadcast matches
		// any <!word> form, so keep unknown ones as literal text
		// rather than letting them fail the send.
		switch t.id {
		case "here", "channel", "everyone":
			// Broadcasts don't carry a style on the wire (slack-go's
			// RichTextSectionBroadcastElement has no Style field).
			return slack.NewRichTextSectionBroadcastElement(t.id)
		}
		return slack.NewRichTextSectionTextElement(wireForm(t), style)
	case tokLink:
		text := t.label
		if text == "" {
			text = t.id
		}
		return slack.NewRichTextSectionLinkElement(t.id, text, style)
	case tokEmoji:
		// Inside a code span the user wants the literal :name: text
		// (e.g., yaml keys, IRC nicks). Emit a text element carrying
		// the Code style; the surrounding ``` mrkdwn fallback bytes
		// are already in place. Note: appendText is the call site
		// for non-code-block contexts; fenced code blocks go through
		// walkCodeBlock which detokenizes the body itself.
		if w.inheritedStyle.Code {
			return slack.NewRichTextSectionTextElement(":"+t.id+":", style)
		}
		return slack.NewRichTextSectionEmojiElement(t.id, 0, style)
	}
	return nil
}

// copyStyle returns a pointer to a copy of inheritedStyle, or nil if
// no flags are set (so we don't emit "style":{} on the wire).
func (w *walker) copyStyle() *slack.RichTextSectionTextStyle {
	s := w.inheritedStyle
	if s == (slack.RichTextSectionTextStyle{}) {
		return nil
	}
	return &s
}
