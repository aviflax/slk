package ui

import (
	"reflect"
	"slices"
	"testing"

	"charm.land/bubbles/v2/key"

	"github.com/gammons/slk/internal/ui/help"
)

// TestEveryKeyBindingHasAHelpGroup makes the help overlay's sections a
// declaration-site requirement: a KeyMap binding without a valid
// `help:"<group>"` tag fails here, so it cannot land ungrouped.
func TestEveryKeyBindingHasAHelpGroup(t *testing.T) {
	typ := reflect.TypeOf(KeyMap{})
	bindingType := reflect.TypeOf(key.Binding{})
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if f.Type != bindingType {
			t.Errorf("KeyMap.%s is not a key.Binding", f.Name)
			continue
		}
		g := f.Tag.Get("help")
		if g == "" {
			t.Errorf("KeyMap.%s has no help:\"<group>\" tag", f.Name)
		} else if !slices.Contains(help.Groups, g) {
			t.Errorf("KeyMap.%s group %q is not in help.Groups %v", f.Name, g, help.Groups)
		}
	}
}

func TestHelpOverlayEntriesAreAllGrouped(t *testing.T) {
	for _, e := range help.FromKeyMap(DefaultKeyMap()) {
		if e.Group == "" {
			t.Errorf("entry %q (%s) has no group", e.Desc, e.Key)
		}
	}
}
