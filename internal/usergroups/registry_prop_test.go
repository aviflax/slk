package usergroups

import (
	"strings"
	"testing"

	"pgregory.net/rapid"
)

func genMap() *rapid.Generator[map[string]string] {
	return rapid.MapOfN(rapid.StringMatching(`S[A-Z0-9]{0,4}`), rapid.StringMatching(`[a-z@]{0,5}`), 0, 6)
}

// Copy is Equal to its source and independent of it: mutating either
// side leaves the other alone. nil in, nil out.
func TestCopyIndependent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		m := genMap().Draw(t, "m")
		c := Copy(m)
		if !Equal(m, c) {
			t.Fatalf("Copy not Equal: %v vs %v", m, c)
		}
		c["NEW"] = "x"
		if _, ok := m["NEW"]; ok {
			t.Fatal("write to copy reached source")
		}
		for k := range m {
			m[k] = m[k] + "!"
		}
		if Equal(m, c) && len(m) > 0 {
			t.Fatal("write to source reached copy")
		}
	})
	if Copy(nil) != nil {
		t.Fatal("Copy(nil) != nil")
	}
}

// Equal is an equivalence relation, with nil equal to empty.
func TestEqualEquivalence(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		a, b, c := genMap().Draw(t, "a"), genMap().Draw(t, "b"), genMap().Draw(t, "c")
		if !Equal(a, a) {
			t.Fatal("not reflexive")
		}
		if Equal(a, b) != Equal(b, a) {
			t.Fatal("not symmetric")
		}
		if Equal(a, b) && Equal(b, c) && !Equal(a, c) {
			t.Fatal("not transitive")
		}
		if !Equal(nil, map[string]string{}) || !Equal(map[string]string{}, nil) {
			t.Fatal("nil != empty")
		}
		// Changing one value breaks equality.
		for k := range a {
			d := Copy(a)
			d[k] += "x"
			if Equal(a, d) {
				t.Fatal("changed value still Equal")
			}
			break
		}
	})
}

// Display always starts with exactly one "@"; a non-empty label wins
// over the map; with neither it is "@group".
func TestDisplay(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		groups := genMap().Draw(t, "groups")
		id := rapid.StringMatching(`S[A-Z0-9]{0,4}`).Draw(t, "id")
		label := rapid.StringMatching(`@{0,2}[a-z]{0,5}`).Draw(t, "label")
		got := Display(groups, id, label)
		if !strings.HasPrefix(got, "@") {
			t.Fatalf("Display = %q", got)
		}
		if label != "" {
			if want := "@" + strings.TrimPrefix(label, "@"); got != want {
				t.Fatalf("label %q: got %q", label, got)
			}
			return
		}
		if strings.HasPrefix(got, "@@") && !strings.HasPrefix(groups[id], "@") {
			t.Fatalf("Display = %q", got)
		}
		if groups[id] == "" && got != "@group" {
			t.Fatalf("Display = %q, want @group", got)
		}
	})
}
