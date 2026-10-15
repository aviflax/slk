package mrkdwn

import (
	"strings"

	"github.com/slack-go/slack"
	"github.com/yuin/goldmark"
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

// indentedParagraphParser lets a paragraph open on a line indented four
// or more spaces. goldmark offers such a line only to parsers that
// accept indented lines, which in CommonMark is the indented code
// block parser alone; with that parser gone the line would be dropped.
type indentedParagraphParser struct{ parser.BlockParser }

func (indentedParagraphParser) CanAcceptIndentedLine() bool { return true }
