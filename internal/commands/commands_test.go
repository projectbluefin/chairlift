package commands

import (
	"reflect"
	"testing"
)

func TestCommandInventory(t *testing.T) {
	want := []Command{
		{
			Action:      "win.preferences",
			Accelerator: "<Primary>comma",
			Display:     "Ctrl+,",
			Title:       "Preferences",
			Group:       "general",
		},
		{
			Action:      "win.check",
			Accelerator: "<Primary>r",
			Display:     "Ctrl+R",
			Title:       "Check again",
			Group:       "general",
		},
		{
			Action:      "win.show-shortcuts",
			Accelerator: "<Primary>question",
			Display:     "Ctrl+?",
			Title:       "Keyboard Shortcuts",
			Group:       "general",
		},
		{
			Action:      "win.help",
			Accelerator: "F1",
			Display:     "F1",
			Title:       "Help",
			Group:       "general",
		},
		{
			Action:      "app.quit",
			Accelerator: "<Primary>q",
			Display:     "Ctrl+Q",
			Title:       "Quit",
			Group:       "general",
		},
		{
			Action:      "app.menu",
			Accelerator: "F10",
			Display:     "F10",
			Title:       "Open primary menu",
			Group:       "general",
			SystemOwned: true,
		},
	}

	got := All()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("All() = %#v, want %#v", got, want)
	}

	seenActions := make(map[string]struct{}, len(got))
	seenAccelerators := make(map[string]struct{}, len(got))
	for _, command := range got {
		if _, ok := seenActions[command.Action]; ok {
			t.Fatalf("duplicate action %q", command.Action)
		}
		seenActions[command.Action] = struct{}{}
		if _, ok := seenAccelerators[command.Accelerator]; ok {
			t.Fatalf("duplicate accelerator %q", command.Accelerator)
		}
		seenAccelerators[command.Accelerator] = struct{}{}
	}

	wantBindings := []Binding{
		{Action: "win.preferences", Accelerators: []string{"<Primary>comma"}},
		{Action: "win.check", Accelerators: []string{"<Primary>r"}},
		{Action: "win.show-shortcuts", Accelerators: []string{"<Primary>question"}},
		{Action: "win.help", Accelerators: []string{"F1"}},
		{Action: "app.quit", Accelerators: []string{"<Primary>q"}},
	}
	if got := Bindings(); !reflect.DeepEqual(got, wantBindings) {
		t.Fatalf("Bindings() = %#v, want %#v", got, wantBindings)
	}
}

func TestCommandInventoryReturnsDefensiveCopies(t *testing.T) {
	all := All()
	all[0].Action = "changed"
	all[0].Accelerator = "changed"

	bindings := Bindings()
	bindings[0].Action = "changed"
	bindings[0].Accelerators[0] = "changed"

	if got := All()[0]; got.Action != "win.preferences" ||
		got.Accelerator != "<Primary>comma" {
		t.Fatalf("All() returned shared storage: %#v", got)
	}
	if got := Bindings()[0]; got.Action != "win.preferences" ||
		!reflect.DeepEqual(got.Accelerators, []string{"<Primary>comma"}) {
		t.Fatalf("Bindings() returned shared storage: %#v", got)
	}
}
