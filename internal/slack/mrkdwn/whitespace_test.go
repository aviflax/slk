package mrkdwn

import "testing"

// whitespaceCases is the whitespace contract of Convert: line breaks,
// blank lines, runs of spaces and indentation typed in the composer
// reach Slack as typed, in both the mrkdwn fallback and the rich_text
// block. Slack's own web client sends paragraphs as one
// rich_text_section whose text carries the "\n" runs, so that is the
// shape wanted here: sibling sections are not a paragraph break to
// Slack's renderer. TestSendPathsPreserveWhitespace (internal/slack)
// checks that chat.postMessage (new messages and thread replies),
// chat.update (edits) and chat.command carry the result.
var whitespaceCases = []struct {
	name   string
	in     string
	mrkdwn string
	block  string // the rich_text block's elements, as JSON
}{
	{
		name:   "single newline",
		in:     "a\nb",
		mrkdwn: "a\nb",
		block:  `[{"type":"rich_text_section","elements":[{"type":"text","text":"a\nb"}]}]`,
	},
	{
		name:   "blank line between paragraphs",
		in:     "para one\n\npara two",
		mrkdwn: "para one\n\npara two",
		block:  `[{"type":"rich_text_section","elements":[{"type":"text","text":"para one\n\npara two"}]}]`,
	},
	{
		name:   "several blank lines",
		in:     "a\n\n\n\nb",
		mrkdwn: "a\n\n\n\nb",
		block:  `[{"type":"rich_text_section","elements":[{"type":"text","text":"a\n\n\n\nb"}]}]`,
	},
	{
		name:   "runs of spaces",
		in:     "a   b  c",
		mrkdwn: "a   b  c",
		block:  `[{"type":"rich_text_section","elements":[{"type":"text","text":"a   b  c"}]}]`,
	},
	{
		name:   "leading indentation",
		in:     "  two\n    four\nnone",
		mrkdwn: "  two\n    four\nnone",
		block:  `[{"type":"rich_text_section","elements":[{"type":"text","text":"  two\n    four\nnone"}]}]`,
	},
	{
		// Slack's composer has no indented code blocks; four spaces
		// (what the textarea turns a pasted tab into) are indentation.
		name:   "four-space indent is not a code block",
		in:     "    indented\n        deeper",
		mrkdwn: "    indented\n        deeper",
		block:  `[{"type":"rich_text_section","elements":[{"type":"text","text":"    indented\n        deeper"}]}]`,
	},
	{
		// goldmark offers a 4+-space line to no parser but the
		// indented code one; without it the line used to vanish.
		name:   "one line indented four spaces",
		in:     "    alone",
		mrkdwn: "    alone",
		block:  `[{"type":"rich_text_section","elements":[{"type":"text","text":"    alone"}]}]`,
	},
	{
		name:   "indentation after a blank line",
		in:     "a\n\n  b",
		mrkdwn: "a\n\n  b",
		block:  `[{"type":"rich_text_section","elements":[{"type":"text","text":"a\n\n  b"}]}]`,
	},
	{
		name:   "spaces before a line break",
		in:     "end  \nnext",
		mrkdwn: "end  \nnext",
		block:  `[{"type":"rich_text_section","elements":[{"type":"text","text":"end  \nnext"}]}]`,
	},
	{
		name:   "backslash line break drops the backslash",
		in:     "a\\\nb",
		mrkdwn: "a\nb",
		block:  `[{"type":"rich_text_section","elements":[{"type":"text","text":"a\nb"}]}]`,
	},
	{
		name:   "styled paragraphs",
		in:     "**bold**\n\n_it_",
		mrkdwn: "*bold*\n\n_it_",
		block:  `[{"type":"rich_text_section","elements":[{"type":"text","text":"bold","style":{"bold":true}},{"type":"text","text":"\n\n"},{"type":"text","text":"it","style":{"italic":true}}]}]`,
	},
	{
		name:   "mentions on separate paragraphs",
		in:     "<@U1>\n\n<@U2>",
		mrkdwn: "<@U1>\n\n<@U2>",
		block:  `[{"type":"rich_text_section","elements":[{"type":"user","user_id":"U1"},{"type":"text","text":"\n\n"},{"type":"user","user_id":"U2"}]}]`,
	},
	{
		name:   "heading and rule markers stay text",
		in:     "# title\nbody\n---",
		mrkdwn: "# title\nbody\n---",
		block:  `[{"type":"rich_text_section","elements":[{"type":"text","text":"# title\nbody\n---"}]}]`,
	},
	{
		name:   "list items on separate lines",
		in:     "intro\n- one\n- two\n\nafter",
		mrkdwn: "intro\n• one\n• two\n\nafter",
		block: `[{"type":"rich_text_section","elements":[{"type":"text","text":"intro"}]},` +
			`{"type":"rich_text_list","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"one"}]},{"type":"rich_text_section","elements":[{"type":"text","text":"two"}]}],"style":"bullet","indent":0,"border":0,"offset":0},` +
			`{"type":"rich_text_section","elements":[{"type":"text","text":"\nafter"}]}]`,
	},
	{
		name:   "paragraphs inside one list item",
		in:     "- one\n\n  more\n- two",
		mrkdwn: "• one\n\nmore\n• two",
		block:  `[{"type":"rich_text_list","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"one\n\nmore"}]},{"type":"rich_text_section","elements":[{"type":"text","text":"two"}]}],"style":"bullet","indent":0,"border":0,"offset":0}]`,
	},
	{
		name:   "code block between lines",
		in:     "x\n```\ncode\n\n  indented\n```\ny",
		mrkdwn: "x\n```\ncode\n\n  indented\n```\ny",
		block: `[{"type":"rich_text_section","elements":[{"type":"text","text":"x"}]},` +
			`{"type":"rich_text_preformatted","elements":[{"type":"text","text":"code\n\n  indented"}],"border":0},` +
			`{"type":"rich_text_section","elements":[{"type":"text","text":"y"}]}]`,
	},
	{
		name:   "code block between paragraphs",
		in:     "x\n\n```\ncode\n```\n\ny",
		mrkdwn: "x\n\n```\ncode\n```\n\ny",
		block: `[{"type":"rich_text_section","elements":[{"type":"text","text":"x\n"}]},` +
			`{"type":"rich_text_preformatted","elements":[{"type":"text","text":"code"}],"border":0},` +
			`{"type":"rich_text_section","elements":[{"type":"text","text":"\ny"}]}]`,
	},
	{
		name:   "quote lines",
		in:     "> q1\n> q2\n\nafter",
		mrkdwn: "> q1\n> q2\n\nafter",
		block: `[{"type":"rich_text_quote","elements":[{"type":"text","text":"q1\nq2"}]},` +
			`{"type":"rich_text_section","elements":[{"type":"text","text":"\nafter"}]}]`,
	},
	{
		name:   "blank line inside a quote",
		in:     "> q1\n>\n>   q2",
		mrkdwn: "> q1\n>\n>   q2",
		block:  `[{"type":"rich_text_quote","elements":[{"type":"text","text":"q1\n\n  q2"}]}]`,
	},
	{
		name:   "list and code inside a quote",
		in:     "> - a\n> - b\n>\n> ```\n>   x\n> ```",
		mrkdwn: "> - a\n> - b\n>\n> ```\n>   x\n> ```",
		block:  `[{"type":"rich_text_quote","elements":[{"type":"text","text":"- a\n- b\n\n` + "```" + `\n  x\n` + "```" + `"}]}]`,
	},
	// In Slack a quote is only the lines that start with ">"; the
	// line after one is not pulled in (CommonMark's lazy continuation).
	{
		name:   "quote then reply",
		in:     "> what you said\nmy reply",
		mrkdwn: "> what you said\nmy reply",
		block: `[{"type":"rich_text_quote","elements":[{"type":"text","text":"what you said"}]},` +
			`{"type":"rich_text_section","elements":[{"type":"text","text":"my reply"}]}]`,
	},
	{
		name:   "multi-line quote then reply",
		in:     "> one\n> two\nreply\nmore reply",
		mrkdwn: "> one\n> two\nreply\nmore reply",
		block: `[{"type":"rich_text_quote","elements":[{"type":"text","text":"one\ntwo"}]},` +
			`{"type":"rich_text_section","elements":[{"type":"text","text":"reply\nmore reply"}]}]`,
	},
	{
		name:   "two quotes with a line between",
		in:     "> first\nmiddle\n> second",
		mrkdwn: "> first\nmiddle\n> second",
		block: `[{"type":"rich_text_quote","elements":[{"type":"text","text":"first"}]},` +
			`{"type":"rich_text_section","elements":[{"type":"text","text":"middle"}]},` +
			`{"type":"rich_text_quote","elements":[{"type":"text","text":"second"}]}]`,
	},
	{
		name:   "quote, blank line, reply",
		in:     "> q\n\nreply",
		mrkdwn: "> q\n\nreply",
		block: `[{"type":"rich_text_quote","elements":[{"type":"text","text":"q"}]},` +
			`{"type":"rich_text_section","elements":[{"type":"text","text":"\nreply"}]}]`,
	},
	{
		// Only the first ">" is markup, as on the receive side
		// (blockkit quoteToMrkdwn): the rest stays text.
		name:   "nested quote markers",
		in:     "> > inner\n> outer\nplain",
		mrkdwn: "> > inner\n> outer\nplain",
		block: `[{"type":"rich_text_quote","elements":[{"type":"text","text":"\u003e inner\nouter"}]},` +
			`{"type":"rich_text_section","elements":[{"type":"text","text":"plain"}]}]`,
	},
	{
		name:   "text then quote",
		in:     "said:\n> yes",
		mrkdwn: "said:\n> yes",
		block: `[{"type":"rich_text_section","elements":[{"type":"text","text":"said:"}]},` +
			`{"type":"rich_text_quote","elements":[{"type":"text","text":"yes"}]}]`,
	},
	{
		name:   "trailing newlines of the whole message are trimmed",
		in:     "a\n\nb\n\n\n",
		mrkdwn: "a\n\nb",
		block:  `[{"type":"rich_text_section","elements":[{"type":"text","text":"a\n\nb"}]}]`,
	},
}

func TestConvert_PreservesWhitespace(t *testing.T) {
	for _, tc := range whitespaceCases {
		t.Run(tc.name, func(t *testing.T) {
			mr, blk := Convert(tc.in)
			if mr != tc.mrkdwn {
				t.Errorf("mrkdwn = %q, want %q", mr, tc.mrkdwn)
			}
			if blk == nil {
				t.Fatal("block is nil")
			}
			want := `{"type":"rich_text","elements":` + tc.block + `}`
			if got := blockJSON(blk); got != want {
				t.Errorf("block =\n  %s\nwant\n  %s", got, want)
			}
		})
	}
}
