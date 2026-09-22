package installcheck

import (
	"encoding/xml"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/projectbluefin/chairlift/internal/bootc"
	"github.com/projectbluefin/chairlift/internal/sysupdate"
	"github.com/projectbluefin/chairlift/internal/ublue"
	"github.com/projectbluefin/chairlift/internal/ubluehelper"
	"github.com/projectbluefin/chairlift/internal/updex"
	"github.com/projectbluefin/chairlift/internal/updexhelper"
)

const (
	execPathAnnotation  = "org.freedesktop.policykit.exec.path"
	execArgv1Annotation = "org.freedesktop.policykit.exec.argv1"
)

type policyDocument struct {
	XMLName xml.Name       `xml:"policyconfig"`
	Actions []policyAction `xml:"action"`
}

type policyAction struct {
	ID          string             `xml:"id,attr"`
	Description string             `xml:"description"`
	Message     string             `xml:"message"`
	Defaults    policyDefaults     `xml:"defaults"`
	Annotations []policyAnnotation `xml:"annotate"`
}

type policyDefaults struct {
	AllowAny      string `xml:"allow_any"`
	AllowInactive string `xml:"allow_inactive"`
	AllowActive   string `xml:"allow_active"`
}

type policyAnnotation struct {
	Key   string `xml:"key,attr"`
	Value string `xml:",chardata"`
}

type expectedPolicyAction struct {
	ID          string
	Description string
	Message     string
	Path        string
	Argv1       string
}

func loadPolicy(t *testing.T, name string) policyDocument {
	t.Helper()

	path := filepath.Join(RepoRoot(), "data", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}

	var document policyDocument
	if err := xml.Unmarshal(data, &document); err != nil {
		t.Fatalf("parsing PolicyKit XML %s: %v", path, err)
	}
	if document.XMLName.Local != "policyconfig" {
		t.Fatalf("%s root element = %q, want policyconfig", path, document.XMLName.Local)
	}
	return document
}

func assertPolicyActions(t *testing.T, name string, expected []expectedPolicyAction) {
	t.Helper()

	document := loadPolicy(t, name)
	if len(document.Actions) != len(expected) {
		t.Fatalf("%s has %d actions, want %d", name, len(document.Actions), len(expected))
	}

	byID := make(map[string]policyAction, len(document.Actions))
	for _, action := range document.Actions {
		if _, exists := byID[action.ID]; exists {
			t.Fatalf("%s repeats action id %q", name, action.ID)
		}
		byID[action.ID] = action
	}

	wantDefaults := policyDefaults{
		AllowAny:      "auth_admin",
		AllowInactive: "auth_admin",
		AllowActive:   "auth_admin_keep",
	}
	for _, want := range expected {
		action, ok := byID[want.ID]
		if !ok {
			t.Errorf("%s is missing action %q", name, want.ID)
			continue
		}
		if action.Description != want.Description {
			t.Errorf("%s action %q description = %q, want %q", name, want.ID, action.Description, want.Description)
		}
		if action.Message != want.Message {
			t.Errorf("%s action %q message = %q, want %q", name, want.ID, action.Message, want.Message)
		}
		if !reflect.DeepEqual(action.Defaults, wantDefaults) {
			t.Errorf("%s action %q defaults = %+v, want %+v", name, want.ID, action.Defaults, wantDefaults)
		}

		annotations := make(map[string]string, len(action.Annotations))
		for _, annotation := range action.Annotations {
			if _, exists := annotations[annotation.Key]; exists {
				t.Errorf("%s action %q repeats annotation %q", name, want.ID, annotation.Key)
			}
			annotations[annotation.Key] = annotation.Value
		}
		wantAnnotations := map[string]string{execPathAnnotation: want.Path}
		if want.Argv1 != "" {
			wantAnnotations[execArgv1Annotation] = want.Argv1
		}
		if !reflect.DeepEqual(annotations, wantAnnotations) {
			t.Errorf("%s action %q annotations = %v, want %v", name, want.ID, annotations, wantAnnotations)
		}
	}
}

func TestPolkitPoliciesMatchPrivilegedHelpers(t *testing.T) {
	commands := updexhelper.SupportedCommands()
	expectedCommands := []string{
		updexhelper.CommandEnableFeature,
		updexhelper.CommandDisableFeature,
		updexhelper.CommandUpdate,
	}
	if !reflect.DeepEqual(commands, expectedCommands) {
		t.Fatalf("updexhelper.SupportedCommands() = %v, want %v", commands, expectedCommands)
	}

	assertPolicyActions(t, "io.projectbluefin.chairlift.updex.policy", []expectedPolicyAction{
		{
			ID:          "io.projectbluefin.chairlift.updex.enable-feature",
			Description: "Enable a system feature using updex",
			Message:     "Authentication is required to enable a system feature",
			Path:        updex.HelperPath,
			Argv1:       updexhelper.CommandEnableFeature,
		},
		{
			ID:          "io.projectbluefin.chairlift.updex.disable-feature",
			Description: "Disable a system feature using updex",
			Message:     "Authentication is required to disable a system feature",
			Path:        updex.HelperPath,
			Argv1:       updexhelper.CommandDisableFeature,
		},
		{
			ID:          "io.projectbluefin.chairlift.updex.update",
			Description: "Update enabled system features using updex",
			Message:     "Authentication is required to update enabled system features",
			Path:        updex.HelperPath,
			Argv1:       updexhelper.CommandUpdate,
		},
	})

	ublueCommands := ubluehelper.SupportedCommands()
	expectedUblueCommands := []string{
		ubluehelper.CommandChannelSwitch,
		ubluehelper.CommandDXEnable,
		ubluehelper.CommandDXDisable,
		ubluehelper.CommandRestart,
		ubluehelper.CommandRollback,
		ubluehelper.CommandAutoEnable,
		ubluehelper.CommandAutoDisable,
		ubluehelper.CommandDriverSwitch,
		ubluehelper.CommandFactoryReset,
	}
	if !reflect.DeepEqual(ublueCommands, expectedUblueCommands) {
		t.Fatalf("ubluehelper.SupportedCommands() = %v, want %v", ublueCommands, expectedUblueCommands)
	}

	assertPolicyActions(t, "io.projectbluefin.chairlift.ublue.policy", []expectedPolicyAction{
		{
			ID:          "io.projectbluefin.chairlift.ublue.channel-switch",
			Description: "Switch the system release channel",
			Message:     "Authentication is required to switch the system release channel",
			Path:        ublue.HelperPath,
			Argv1:       ubluehelper.CommandChannelSwitch,
		},
		{
			ID:          "io.projectbluefin.chairlift.ublue.dx-enable",
			Description: "Enable developer mode",
			Message:     "Authentication is required to enable developer mode",
			Path:        ublue.HelperPath,
			Argv1:       ubluehelper.CommandDXEnable,
		},
		{
			ID:          "io.projectbluefin.chairlift.ublue.dx-disable",
			Description: "Disable developer mode",
			Message:     "Authentication is required to disable developer mode",
			Path:        ublue.HelperPath,
			Argv1:       ubluehelper.CommandDXDisable,
		},
		{
			ID:          "io.projectbluefin.chairlift.ublue.restart",
			Description: "Restart the system",
			Message:     "Authentication is required to restart the system",
			Path:        ublue.HelperPath,
			Argv1:       ubluehelper.CommandRestart,
		},
		{
			ID:          "io.projectbluefin.chairlift.ublue.rollback",
			Description: "Roll back to the previous system image",
			Message:     "Authentication is required to roll back to the previous system image",
			Path:        ublue.HelperPath,
			Argv1:       ubluehelper.CommandRollback,
		},
		{
			ID:          "io.projectbluefin.chairlift.ublue.auto-updates-enable",
			Description: "Enable automatic background updates",
			Message:     "Authentication is required to enable automatic background updates",
			Path:        ublue.HelperPath,
			Argv1:       ubluehelper.CommandAutoEnable,
		},
		{
			ID:          "io.projectbluefin.chairlift.ublue.auto-updates-disable",
			Description: "Disable automatic background updates",
			Message:     "Authentication is required to disable automatic background updates",
			Path:        ublue.HelperPath,
			Argv1:       ubluehelper.CommandAutoDisable,
		},
		{
			ID:          "io.projectbluefin.chairlift.ublue.driver-switch",
			Description: "Switch the system graphics driver image",
			Message:     "Authentication is required to switch the system graphics driver image",
			Path:        ublue.HelperPath,
			Argv1:       ubluehelper.CommandDriverSwitch,
		},
		{
			ID:          "io.projectbluefin.chairlift.ublue.factory-reset",
			Description: "Factory reset the system",
			Message:     "Authentication is required to factory reset the system",
			Path:        ublue.HelperPath,
			Argv1:       ubluehelper.CommandFactoryReset,
		},
	})

	assertPolicyActions(t, "io.projectbluefin.chairlift.bootc.policy", []expectedPolicyAction{
		{
			ID:          "io.projectbluefin.chairlift.bootc.stage",
			Description: "Download and stage a system image update",
			Message:     "Authentication is required to stage a system update",
			Path:        bootc.StageScriptPath,
		},
	})

	assertPolicyActions(t, "io.projectbluefin.chairlift.sysupdate.policy", []expectedPolicyAction{
		{
			ID:          "io.projectbluefin.chairlift.sysupdate.stage",
			Description: "Download and stage a system image update",
			Message:     "Authentication is required to stage a system update",
			Path:        sysupdate.StageScriptPath,
		},
	})
}

func TestPolkitPasswordlessRulesAreAbsent(t *testing.T) {
	for _, name := range []string{
		"io.projectbluefin.chairlift.updex.rules",
		"io.projectbluefin.chairlift.bootc.rules",
		"io.projectbluefin.chairlift.sysupdate.rules",
		"io.projectbluefin.chairlift.ublue.rules",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(RepoRoot(), "data", name)
			if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("os.Stat(%q) error = %v, want file absent", path, err)
			}
		})
	}
}

func TestEveryUpdexCommandHasOnePolicyAction(t *testing.T) {
	document := loadPolicy(t, "io.projectbluefin.chairlift.updex.policy")
	counts := make(map[string]int)
	for _, action := range document.Actions {
		for _, annotation := range action.Annotations {
			if annotation.Key == execArgv1Annotation {
				counts[annotation.Value]++
			}
		}
	}

	for _, command := range updexhelper.SupportedCommands() {
		if counts[command] != 1 {
			t.Errorf("PolicyKit actions selecting argv1=%q = %d, want exactly 1", command, counts[command])
		}
		delete(counts, command)
	}
	for command, count := range counts {
		t.Errorf("PolicyKit policy selects unsupported helper command %q in %d action(s)", command, count)
	}
	if t.Failed() {
		t.Logf("supported commands: %v", updexhelper.SupportedCommands())
	}
}

// The ublue analogue of TestEveryUpdexCommandHasOnePolicyAction: pkexec
// selects an action from the executable path and argv1, so a command with no
// action cannot be authorized at all, and an action selecting a command the
// helper rejects authorizes a privilege for nothing.
func TestEveryUblueCommandHasOnePolicyAction(t *testing.T) {
	document := loadPolicy(t, "io.projectbluefin.chairlift.ublue.policy")
	counts := make(map[string]int)
	for _, action := range document.Actions {
		for _, annotation := range action.Annotations {
			if annotation.Key == execArgv1Annotation {
				counts[annotation.Value]++
			}
		}
	}

	for _, command := range ubluehelper.SupportedCommands() {
		if counts[command] != 1 {
			t.Errorf("PolicyKit actions selecting argv1=%q = %d, want exactly 1", command, counts[command])
		}
		delete(counts, command)
	}
	for command, count := range counts {
		t.Errorf("PolicyKit policy selects unsupported helper command %q in %d action(s)", command, count)
	}
	if t.Failed() {
		t.Logf("supported commands: %v", ubluehelper.SupportedCommands())
	}
}

// The two privileged helpers must stay distinct binaries with distinct
// action namespaces. A shared path would let an action authorized for one
// command surface select a subcommand of the other.
func TestPrivilegedHelperPathsAreDistinct(t *testing.T) {
	if ublue.HelperPath == updex.HelperPath {
		t.Fatalf("ublue.HelperPath and updex.HelperPath are both %q; each helper needs its own fixed path", ublue.HelperPath)
	}
	for _, path := range []string{ublue.HelperPath, updex.HelperPath} {
		if !filepath.IsAbs(path) {
			t.Errorf("helper path %q is not absolute; pkexec would fall back to the generic run-program action", path)
		}
	}
}
