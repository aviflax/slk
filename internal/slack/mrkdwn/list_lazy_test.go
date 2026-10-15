package mrkdwn

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/slack-go/slack"
)

// listItemTexts returns the text of each item of every list in block,
// in order, and the text of every top-level section.
func listShape(t *testing.T, block *slack.RichTextBlock) (items, sections []string) {
	t.Helper()
	for _, e := range block.Elements {
		switch e := e.(type) {
		case *slack.RichTextList:
			for _, it := range e.Elements {
				j, _ := json.Marshal(it)
				items = append(items, string(j))
			}
		case *slack.RichTextSection:
			j, _ := json.Marshal(e)
			sections = append(sections, string(j))
		}
	}
	return items, sections
}

// A line typed right after a list, without the item's indentation, is a
// new line after the list, not more of the last item: CommonMark's lazy
// continuation, which Slack does not have. A line indented to the item's
// content still belongs to it.
func TestPlainLineAfterListEndsTheList(t *testing.T) {
	for _, tc := range []struct {
		in        string
		lastItem  string // a substring of the last item
		notInItem string // must not appear in any item ("" to skip)
		section   string // must appear in a section after the list ("" to skip)
	}{
		{"1. one\n2. two\nplain after", "two", "plain after", "plain after"},
		{"- a\nplain", "\"a\"", "plain", "plain"},
		{"- a\n  - nested\nplain", "nested", "plain", "plain"},
		{"1. one\n   kept in item", "kept in item", "", ""},
		{"1. one\n2. two", "two", "", ""},
	} {
		_, block := Convert(tc.in)
		items, sections := listShape(t, block)
		if len(items) == 0 || !strings.Contains(items[len(items)-1], tc.lastItem) {
			t.Errorf("%q: last item %v lacks %q", tc.in, items, tc.lastItem)
		}
		if tc.notInItem != "" {
			for _, it := range items {
				if strings.Contains(it, tc.notInItem) {
					t.Errorf("%q: %q absorbed into item %s", tc.in, tc.notInItem, it)
				}
			}
		}
		if tc.section != "" && !strings.Contains(strings.Join(sections, " "), tc.section) {
			t.Errorf("%q: no section after the list holds %q: %v", tc.in, tc.section, sections)
		}
	}
}
