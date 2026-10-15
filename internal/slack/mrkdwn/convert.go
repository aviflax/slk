package mrkdwn

import (
	"strings"

	"github.com/slack-go/slack"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// Convert parses CommonMark in input and returns the Slack-compatible
// payload pair: a mrkdwn fallback string and a rich_text block.
//
// input may contain Slack wire-form tokens (<@U…>, <#C…|name>,
// <!here>, <https://…|label>); these are preserved as opaque tokens
// in the mrkdwn fallback and become typed elements (user, channel,
// broadcast, link) in the block.
//
// Whitespace survives as typed: line breaks, blank lines, runs of
// spaces and indentation. Only trailing newlines of the whole message
// are dropped.
//
// For empty / whitespace-only input, returns ("", nil).
func Convert(input string) (string, *slack.RichTextBlock) {
	if strings.TrimSpace(input) == "" {
		return "", nil
	}

	tokenized, table := tokenize(input)

	md := goldmark.New(
		goldmark.WithParser(newParser()),
		goldmark.WithExtensions(
			extension.Strikethrough,
			extension.Linkify,
		),
	)
	doc := md.Parser().Parse(text.NewReader([]byte(tokenized)))

	w := newWalker([]byte(tokenized), table)
	w.walkDocument(doc)

	mr := strings.TrimRight(w.mrkdwn.String(), "\n")
	mr = detokenizeText(mr, table)

	if len(w.block.Elements) == 0 {
		return mr, nil
	}
	return mr, w.block
}

// newParser is goldmark's CommonMark parser cut down to the block
// syntax Slack's own composer has: lists, fenced code and quotes.
// The rest of CommonMark's block grammar rewrites what was typed:
// indented code blocks turn four leading spaces (or a pasted tab, which
// the composer stores as four spaces) into a code block, a setext
// heading swallows the "---" line under it, a link reference
// definition ("[x]: url") disappears, and thematic breaks, ATX
// headings and HTML blocks become nodes the walker has to turn back
// into text. Without those parsers the same lines are paragraph text.
func newParser() parser.Parser {
	return parser.NewParser(
		parser.WithBlockParsers(
			util.Prioritized(parser.NewListParser(), 300),
			util.Prioritized(parser.NewListItemParser(), 400),
			util.Prioritized(parser.NewFencedCodeBlockParser(), 700),
			util.Prioritized(parser.NewBlockquoteParser(), 800),
			util.Prioritized(indentedParagraphParser{parser.NewParagraphParser()}, 1000),
		),
		parser.WithInlineParsers(parser.DefaultInlineParsers()...),
	)
}

// indentedParagraphParser is goldmark's paragraph parser with two
// changes to where a paragraph may start.
//
// It opens on a line indented four or more spaces. goldmark offers
// such a line only to parsers that accept indented lines, which in
// CommonMark is the indented code block parser alone; with that parser
// gone the line would be dropped.
//
// It ends a quote at the first line without the quote's ">"
// markers. CommonMark's lazy continuation would add that line to the
// quote's last paragraph, but in Slack a quote is only its marked
// lines, so "> what you said\nmy reply" is a quote and then a reply.
// It ends a list item the same way, at the first line not indented to
// the item's content (lazyListLine).
// goldmark tries a lazy line as a paragraph continuation only when no
// parser that can interrupt a paragraph opens a block on it, so this
// parser claims to interrupt paragraphs and opens one exactly on such
// a lazy line. Everywhere else it opens only where the stock parser
// would.
type indentedParagraphParser struct{ parser.BlockParser }

func (indentedParagraphParser) CanAcceptIndentedLine() bool { return true }

func (indentedParagraphParser) CanInterruptParagraph() bool { return true }

func (p indentedParagraphParser) Open(parent ast.Node, reader text.Reader, pc parser.Context) (ast.Node, parser.State) {
	if last := pc.LastOpenedBlock().Node; last != nil && ast.IsParagraph(last) && !lazyQuoteLine(last, reader) && !lazyListLine(last, reader) {
		// An ordinary paragraph continuation: leave it to Continue.
		return nil, parser.NoChildren
	}
	return p.BlockParser.Open(parent, reader, pc)
}

// lazyQuoteLine reports whether the line reader is on would continue
// para, a paragraph inside a quote, without carrying that quote's ">"
// markers: CommonMark's lazy continuation line.
func lazyQuoteLine(para ast.Node, reader text.Reader) bool {
	depth := 0
	for n := para.Parent(); n != nil; n = n.Parent() {
		if _, ok := n.(*ast.Blockquote); ok {
			depth++
		}
	}
	if depth == 0 {
		return false
	}
	src := reader.Source()
	_, seg := reader.PeekLine()
	return quoteMarkers(src, lineStart(src, seg.Start)) < depth
}

// lazyListLine reports whether the line reader is on would continue
// para, a paragraph inside a list item, without the indentation that
// keeps a line in that item: CommonMark's lazy continuation line. Slack
// has none, so "1. one\nmy reply" is a list and then a line after it.
// Each item's Offset is its content column relative to the item that
// holds it, so their sum is the column the line must reach.
func lazyListLine(para ast.Node, reader text.Reader) bool {
	need, inList := 0, false
	for n := para.Parent(); n != nil; n = n.Parent() {
		if li, ok := n.(*ast.ListItem); ok {
			need += li.Offset
			inList = true
		}
	}
	if !inList {
		return false
	}
	src := reader.Source()
	_, seg := reader.PeekLine()
	indent := 0
	for i := lineStart(src, seg.Start); i < len(src); i++ {
		switch src[i] {
		case ' ':
			indent++
		case '\t':
			indent += 4 - indent%4
		default:
			return indent < need
		}
	}
	return indent < need
}

// quoteMarkers counts the ">" markers that open the line starting at
// i, each allowed up to three spaces before it.
func quoteMarkers(src []byte, i int) int {
	n := 0
	for {
		j := i
		for j < len(src) && j-i < 3 && src[j] == ' ' {
			j++
		}
		if j >= len(src) || src[j] != '>' {
			return n
		}
		n++
		i = j + 1
	}
}
