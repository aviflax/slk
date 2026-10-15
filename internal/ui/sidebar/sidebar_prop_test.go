package sidebar

import (
	"fmt"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/proptest"
	"pgregory.net/rapid"
)

// customSections are the [sections.*] names items draw from. Built-in
// names are not among them: see TestOrderedSectionsCustomNamedLikeBuiltIn.
var customSections = []string{"Eng", "Ops", "Starred", "Design"}

// channelItems draws 0..n items with unique IDs (C0, C1, ...), a mix of
// types, custom sections, section and channel orders and mutes.
func channelItems(max int) *rapid.Generator[[]ChannelItem] {
	return rapid.Custom(func(t *rapid.T) []ChannelItem {
		n := rapid.IntRange(0, max).Draw(t, "items")
		out := make([]ChannelItem, n)
		for i := range out {
			sec := ""
			if rapid.IntRange(0, 2).Draw(t, "hasSection") == 0 {
				sec = rapid.SampledFrom(customSections).Draw(t, "section")
			}
			chOrder := 0
			if rapid.IntRange(0, 3).Draw(t, "annotated") == 0 {
				chOrder = rapid.IntRange(1, 4).Draw(t, "channelOrder")
			}
			out[i] = ChannelItem{
				ID:           fmt.Sprintf("C%d", i),
				Name:         fmt.Sprintf("ch%d", i),
				Type:         rapid.SampledFrom([]string{"channel", "private", "dm", "group_dm", "app"}).Draw(t, "type"),
				Section:      sec,
				SectionOrder: rapid.IntRange(0, 3).Draw(t, "sectionOrder"),
				ChannelOrder: chOrder,
				IsMuted:      rapid.IntRange(0, 3).Draw(t, "muted") == 0,
			}
		}
		return out
	})
}

// readStates draws a read state for some of items: unread or not, a
// mention count that may be negative or past the badge cap.
func readStates(items []ChannelItem) *rapid.Generator[map[string]core.ReadState] {
	return rapid.Custom(func(t *rapid.T) map[string]core.ReadState {
		out := map[string]core.ReadState{}
		_ = rapid.Bool().Draw(t, "seed") // rapid needs a draw even with no items
		for _, it := range items {
			if !rapid.Bool().Draw(t, "hasState") {
				continue
			}
			out[it.ID] = core.ReadState{
				HasUnread:    rapid.Bool().Draw(t, "unread"),
				MentionCount: rapid.IntRange(-2, 150).Draw(t, "mentions"),
			}
		}
		return out
	})
}

// allIdx is every index of items, as filtered would hold them with no
// filter applied.
func allIdx(items []ChannelItem) []int {
	out := make([]int, len(items))
	for i := range out {
		out[i] = i
	}
	return out
}

// formatMentionBadge is "" for n <= 0, the decimal number up to the cap,
// and "99+" past it, so the text is never longer than three cells.
func TestFormatMentionBadge(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(-1000, 100000).Draw(t, "n")
		got := formatMentionBadge(n)
		switch {
		case n <= 0:
			if got != "" {
				t.Fatalf("formatMentionBadge(%d) = %q, want empty", n, got)
			}
		case n > mentionBadgeCap:
			if got != "99+" {
				t.Fatalf("formatMentionBadge(%d) = %q, want 99+", n, got)
			}
		default:
			if back, err := strconv.Atoi(got); err != nil || back != n {
				t.Fatalf("formatMentionBadge(%d) = %q does not read back", n, got)
			}
		}
		if len(got) > 3 {
			t.Fatalf("formatMentionBadge(%d) = %q, longer than 3", n, got)
		}
	})
}

// IsStale is never true with the feature off, with unreads, for an item
// in a custom section, or for a last-read in the future or that does not
// parse; and once stale, an item stays stale as time passes or as the
// threshold shrinks.
func TestIsStaleGuardsAndMonotone(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		item := channelItems(1).Filter(func(s []ChannelItem) bool { return len(s) == 1 }).Draw(t, "item")[0]
		hasUnread := rapid.Bool().Draw(t, "hasUnread")
		lastRead := rapid.OneOf(
			proptest.TS(),
			rapid.Just(""),
			rapid.Just("0"),
			rapid.SampledFrom([]string{"abc", "-5.000001", "17.x", "1700000000.-1"}),
		).Draw(t, "lastRead")
		threshold := time.Duration(rapid.Int64Range(-int64(time.Hour), 400*24*int64(time.Hour)).Draw(t, "threshold"))
		now := time.Unix(rapid.Int64Range(1_000_000_000, 2_100_000_000).Draw(t, "now"), 0)

		stale := IsStale(item, hasUnread, lastRead, threshold, now)
		lr, parsed := parseSlackTS(lastRead)
		if stale && (threshold <= 0 || hasUnread || item.Section != "" ||
			(lastRead != "" && (!parsed || lr.After(now)))) {
			t.Fatalf("IsStale(%+v, unread=%v, %q, %v, %v) = true", item, hasUnread, lastRead, threshold, now)
		}
		later := now.Add(time.Duration(rapid.Int64Range(0, 1000*24*int64(time.Hour)).Draw(t, "later")))
		if stale && !IsStale(item, hasUnread, lastRead, threshold, later) {
			t.Fatalf("stale at %v but not at later %v", now, later)
		}
		if threshold <= 1 {
			return
		}
		smaller := threshold - time.Duration(rapid.Int64Range(0, int64(threshold-1)).Draw(t, "shrink"))
		if stale && !IsStale(item, hasUnread, lastRead, smaller, now) {
			t.Fatalf("stale at threshold %v but not at %v", threshold, smaller)
		}
	})
}

// orderedSectionsLegacy lists each section of the filtered items exactly
// once and nothing else; custom sections come first ordered by (smallest
// SectionOrder, first appearance), then the built-ins Direct Messages,
// Apps, Channels. Permuting the input changes only the order of custom
// sections that tie on SectionOrder.
func TestOrderedSectionsLegacy(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		items := channelItems(12).Draw(t, "items")
		filtered := rapid.Permutation(allIdx(items)).Draw(t, "filtered")
		filtered = filtered[:rapid.IntRange(0, len(filtered)).Draw(t, "keep")]
		got := orderedSectionsLegacy(items, filtered)

		want := map[string]bool{}
		minOrder := map[string]int{}
		first := map[string]int{}
		for pos, idx := range filtered {
			it := items[idx]
			s := sectionForLegacy(it)
			want[s] = true
			if it.Section == "" {
				continue
			}
			if o, ok := minOrder[s]; !ok || it.SectionOrder < o {
				minOrder[s] = it.SectionOrder
			}
			if _, ok := first[s]; !ok {
				first[s] = pos
			}
		}
		if len(got) != len(want) {
			t.Fatalf("sections %v, want the set %v", got, want)
		}
		for _, s := range got {
			if !want[s] {
				t.Fatalf("section %q listed but no item is in it: %v", s, got)
			}
		}
		builtin := map[string]int{defaultDMSection: 0, defaultAppsSection: 1, defaultChannelsSection: 2}
		for i := 1; i < len(got); i++ {
			a, b := got[i-1], got[i]
			_, ba := builtin[a]
			_, bb := builtin[b]
			switch {
			case ba && !bb:
				t.Fatalf("custom %q after built-in %q: %v", b, a, got)
			case ba && bb:
				if builtin[a] > builtin[b] {
					t.Fatalf("built-ins out of order: %v", got)
				}
			case !ba && !bb:
				if minOrder[a] > minOrder[b] || (minOrder[a] == minOrder[b] && first[a] > first[b]) {
					t.Fatalf("custom %q (order %d, first %d) before %q (order %d, first %d)",
						a, minOrder[a], first[a], b, minOrder[b], first[b])
				}
			}
		}

		// Reordering filtered keeps the set, and the order of everything
		// that does not tie on SectionOrder.
		again := orderedSectionsLegacy(items, rapid.Permutation(filtered).Draw(t, "shuffled"))
		if len(again) != len(got) {
			t.Fatalf("shuffled input gave %v, was %v", again, got)
		}
		for i := range got {
			a, b := got[i], again[i]
			if a == b {
				continue
			}
			_, ba := builtin[a]
			if ba || minOrder[a] != minOrder[b] {
				t.Fatalf("shuffled input gave %v, was %v", again, got)
			}
		}
	})
}

// A custom section that shares a built-in's name is still one section:
// [sections.Channels] holding #general must not list "Channels" twice
// when other channels fall back to it.
func TestOrderedSectionsCustomNamedLikeBuiltIn(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		items := channelItems(10).Draw(t, "items")
		name := rapid.SampledFrom([]string{defaultDMSection, defaultAppsSection, defaultChannelsSection}).Draw(t, "name")
		for i := range items {
			if rapid.IntRange(0, 2).Draw(t, "rename") == 0 {
				items[i].Section = name
			}
		}
		got := orderedSectionsLegacy(items, allIdx(items))
		seen := map[string]bool{}
		for _, s := range got {
			if seen[s] {
				t.Fatalf("section %q listed twice: %v", s, got)
			}
			seen[s] = true
		}
	})
}

// MentionBadge ignores mute, is never negative, and is 0 whenever the
// channel has no unreads; IsVisiblyUnread is exactly "unread and not
// muted".
func TestMentionBadgeAndVisiblyUnread(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		item := ChannelItem{ID: "C1", IsMuted: rapid.Bool().Draw(t, "muted")}
		st := core.ReadState{HasUnread: rapid.Bool().Draw(t, "unread"), MentionCount: rapid.IntRange(-1000, 1000).Draw(t, "mentions")}
		b := item.MentionBadge(st)
		flipped := item
		flipped.IsMuted = !item.IsMuted
		if flipped.MentionBadge(st) != b {
			t.Fatalf("muting changed the badge for %+v", st)
		}
		if b < 0 || (!st.HasUnread && b != 0) || (st.HasUnread && b != max(0, st.MentionCount)) {
			t.Fatalf("MentionBadge(%+v) = %d", st, b)
		}
		if item.IsVisiblyUnread(st) != (st.HasUnread && !item.IsMuted) {
			t.Fatalf("IsVisiblyUnread(%+v) muted=%v wrong", st, item.IsMuted)
		}
	})
}

// sortFilteredIdx, in config mode (every item's section is one
// orderedSectionsLegacy lists):
//   - permutes filtered;
//   - leaves each section's items contiguous, sections in sectionOrder;
//   - inside a section named in stars, channels and private channels
//     come before everything else;
//   - then ChannelOrder > 0 ascending, then input order;
//   - gives the same result whatever order filtered arrives in (the
//     comparator is a total order), so sorting again changes nothing.
func TestSortFilteredIdx(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		items := channelItems(14).Draw(t, "items")
		filtered := rapid.Permutation(allIdx(items)).Draw(t, "filtered")
		filtered = filtered[:rapid.IntRange(0, len(filtered)).Draw(t, "keep")]
		// KNOWN: sectionOrder here names every item's section, as config
		// mode guarantees. In Slack mode an item whose section the provider
		// does not list (a stale section ID, or "" when there is no
		// channels-type section) ranks 0 alongside the first section, and
		// the comparator stops being a strict weak order: with sectionOrder
		// empty, a starred "dm" with ChannelOrder 1 and two plain channels
		// form a cycle, and the result depends on the input order (seen as
		// [4 7 0 1 2 3 5 6 8] vs [1 2 3 8 4 7 0 5 6]). Such items are not
		// drawn, but a/A (NextUnread) walks them. Where they should sort is
		// a product call, so the generator does not drop sections.
		order := orderedSectionsLegacy(items, filtered)
		stars := map[string]bool{}
		for _, s := range order {
			if rapid.IntRange(0, 3).Draw(t, "star") == 0 {
				stars[s] = true
			}
		}

		got := slices.Clone(filtered)
		sortFilteredIdx(items, got, sectionForLegacy, order, stars)

		if !slices.Equal(slices.Sorted(slices.Values(got)), slices.Sorted(slices.Values(filtered))) {
			t.Fatalf("not a permutation: %v from %v", got, filtered)
		}
		rank := map[string]int{}
		for i, s := range order {
			rank[s] = i
		}
		isChannel := func(it ChannelItem) bool { return it.Type == "channel" || it.Type == "private" }
		for i := 1; i < len(got); i++ {
			a, b := items[got[i-1]], items[got[i]]
			sa, sb := sectionForLegacy(a), sectionForLegacy(b)
			if rank[sa] != rank[sb] {
				if rank[sa] > rank[sb] {
					t.Fatalf("section %q after %q", sa, sb)
				}
				continue
			}
			if stars[sa] && isChannel(a) != isChannel(b) {
				if !isChannel(a) {
					t.Fatalf("in starred %q, %s (%s) before %s (%s)", sa, a.ID, a.Type, b.ID, b.Type)
				}
				continue
			}
			oa, ob := a.ChannelOrder, b.ChannelOrder
			switch {
			case oa == 0 && ob > 0:
				t.Fatalf("unannotated %s before annotated %s", a.ID, b.ID)
			case oa > 0 && ob > 0 && oa > ob:
				t.Fatalf("%s (order %d) before %s (order %d)", a.ID, oa, b.ID, ob)
			case oa == ob && got[i-1] > got[i]:
				t.Fatalf("%s and %s tie but are out of input order", a.ID, b.ID)
			}
		}

		reshuffled := rapid.Permutation(filtered).Draw(t, "reshuffled")
		sortFilteredIdx(items, reshuffled, sectionForLegacy, order, stars)
		if !slices.Equal(reshuffled, got) {
			t.Fatalf("input order leaked: %v vs %v", reshuffled, got)
		}
		again := slices.Clone(got)
		sortFilteredIdx(items, again, sectionForLegacy, order, stars)
		if !slices.Equal(again, got) {
			t.Fatalf("not idempotent: %v then %v", got, again)
		}
	})
}

// aggregateSection's figures for a section are the sums of the per-row
// predicates over that section's items: unread counts IsVisiblyUnread,
// mentions adds MentionBadge, and allMuted holds exactly when no
// unmuted row has a badge. Summed over all sections, unread equals the
// count over every filtered item, so no item is counted twice or lost.
func TestAggregateSectionSumsRows(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		items := channelItems(14).Draw(t, "items")
		state := readStates(items).Draw(t, "state")
		filtered := allIdx(items)
		totalUnread, totalMentions := 0, 0
		for _, sec := range orderedSectionsLegacy(items, filtered) {
			unread, mentions, allMuted := aggregateSection(items, filtered, state, sectionForLegacy, sec)
			wantU, wantM, wantAll := 0, 0, true
			for _, it := range items {
				if sectionForLegacy(it) != sec {
					continue
				}
				if it.IsVisiblyUnread(state[it.ID]) {
					wantU++
				}
				if b := it.MentionBadge(state[it.ID]); b > 0 {
					wantM += b
					wantAll = wantAll && it.IsMuted
				}
			}
			if unread != wantU || mentions != wantM || allMuted != wantAll {
				t.Fatalf("section %q: got (%d, %d, %v), want (%d, %d, %v)", sec, unread, mentions, allMuted, wantU, wantM, wantAll)
			}
			totalUnread += unread
			totalMentions += mentions
		}
		allU, allM := 0, 0
		for _, it := range items {
			if it.IsVisiblyUnread(state[it.ID]) {
				allU++
			}
			allM += it.MentionBadge(state[it.ID])
		}
		if totalUnread != allU || totalMentions != allM {
			t.Fatalf("sections sum to (%d, %d), items to (%d, %d)", totalUnread, totalMentions, allU, allM)
		}

		// The Model's tab-title count agrees with the section headers.
		m := New(items)
		m.SetReadStateReader(func() map[string]core.ReadState { return state })
		if got := m.UnreadChannelCount(); got != allU {
			t.Fatalf("UnreadChannelCount = %d, sections say %d", got, allU)
		}
	})
}

// nextUnread never returns afterID and finds something exactly when
// another visibly-unread item is in filtered. Stepping forward from an
// unread item visits every unread item exactly once before coming back,
// in filtered order, and a step back undoes a step forward.
func TestNextUnreadCycles(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		items := channelItems(14).Draw(t, "items")
		state := readStates(items).Draw(t, "state")
		filtered := rapid.Permutation(allIdx(items)).Draw(t, "filtered")
		var unread []string
		for _, idx := range filtered {
			if items[idx].IsVisiblyUnread(state[items[idx].ID]) {
				unread = append(unread, items[idx].ID)
			}
		}
		after := rapid.OneOf(rapid.Just("nope"), rapid.SampledFrom(append([]string{"C0"}, unread...))).Draw(t, "after")
		dir := rapid.SampledFrom([]int{-3, -1, 1, 2}).Draw(t, "dir")

		got, ok := nextUnread(items, filtered, state, after, dir)
		others := len(unread)
		if slices.Contains(unread, after) {
			others--
		}
		if ok != (others > 0) || (ok && got.ID == after) {
			t.Fatalf("nextUnread(after %s, dir %d) = %s %v with unread %v", after, dir, got.ID, ok, unread)
		}
		if ok && !slices.Contains(unread, got.ID) {
			t.Fatalf("nextUnread returned %s, not unread", got.ID)
		}
		if len(unread) < 2 {
			return
		}

		start := rapid.SampledFrom(unread).Draw(t, "start")
		pos := slices.Index(unread, start)
		cur := start
		for i := 1; i <= len(unread); i++ {
			next, ok := nextUnread(items, filtered, state, cur, 1)
			if want := unread[(pos+i)%len(unread)]; !ok || next.ID != want {
				t.Fatalf("step %d from %s: got %s %v, want %s (unread %v)", i, cur, next.ID, ok, want, unread)
			}
			back, _ := nextUnread(items, filtered, state, next.ID, -1)
			if back.ID != cur {
				t.Fatalf("back from %s gave %s, want %s", next.ID, back.ID, cur)
			}
			cur = next.ID
		}
	})
}

// renderRows draws a rendered sidebar layout: rows the cursor can land
// on carry consecutive nav indices, separators carry -1.
func renderRows() *rapid.Generator[[]renderRow] {
	return rapid.Custom(func(t *rapid.T) []renderRow {
		n := rapid.IntRange(1, 60).Draw(t, "rows")
		rows := make([]renderRow, n)
		nav := 0
		for i := range rows {
			if rapid.IntRange(0, 4).Draw(t, "blank") == 0 {
				rows[i].navIdx = -1
				continue
			}
			rows[i].navIdx = nav
			nav++
		}
		return rows
	})
}

// pageRows, from any in-range offset:
//   - keeps the offset in [0, max(0, len-height)] and moves it by delta
//     where there is room;
//   - lands the cursor on a navigable row whenever one exists, and never
//     panics when none does;
//   - when the viewport moved, keeps the cursor's screen row if that row
//     is navigable; when it could not move, puts the cursor on the first
//     (paging up) or last (paging down) navigable row;
//   - never moves the cursor against the paging direction.
func TestPageRows(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		rows := renderRows().Draw(t, "rows")
		height := rapid.IntRange(1, 40).Draw(t, "height")
		maxOff := max(0, len(rows)-height)
		yOffset := rapid.IntRange(0, maxOff).Draw(t, "yOffset")
		delta := rapid.IntRange(-50, 50).Filter(func(d int) bool { return d != 0 }).Draw(t, "delta")

		var navRows []int
		lineOf := map[int]int{}
		for i, r := range rows {
			if r.navIdx >= 0 {
				navRows = append(navRows, r.navIdx)
				lineOf[r.navIdx] = i
			}
		}
		cursor := 0
		if len(navRows) > 0 {
			cursor = rapid.SampledFrom(navRows).Draw(t, "cursor")
		}

		c, off, ok := pageRows(rows, cursor, yOffset, height, delta)
		if len(navRows) == 0 {
			if ok && off == yOffset {
				// The cursor is on no row, so only the "can't scroll"
				// branch could have run; it must leave the cursor alone.
				if c != cursor {
					t.Fatalf("no navigable rows, cursor moved %d -> %d", cursor, c)
				}
			}
			return
		}
		if !ok {
			t.Fatalf("cursor %d is on a row but pageRows refused", cursor)
		}
		if off < 0 || off > maxOff {
			t.Fatalf("offset %d outside [0, %d]", off, maxOff)
		}
		if want := min(max(yOffset+delta, 0), maxOff); off != want {
			t.Fatalf("offset %d, want %d", off, want)
		}
		if _, nav := lineOf[c]; !nav {
			t.Fatalf("cursor landed on %d, not a navigable row", c)
		}
		if (delta > 0 && c < cursor) || (delta < 0 && c > cursor) {
			t.Fatalf("delta %d moved the cursor %d -> %d", delta, cursor, c)
		}
		if off == yOffset {
			want := navRows[0]
			if delta > 0 {
				want = navRows[len(navRows)-1]
			}
			if c != want {
				t.Fatalf("viewport stuck: cursor %d, want edge %d", c, want)
			}
			return
		}
		target := lineOf[cursor] + off - yOffset
		if target >= 0 && target < len(rows) && rows[target].navIdx >= 0 && c != rows[target].navIdx {
			t.Fatalf("screen row not kept: cursor line %d -> %d, want line %d", lineOf[cursor], lineOf[c], target)
		}
	})
}
