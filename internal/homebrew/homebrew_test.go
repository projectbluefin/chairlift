package homebrew

import (
	"testing"
	"time"
)

func TestCommandTimeout(t *testing.T) {
	if len(stateChangingCommands) != 11 {
		t.Fatalf("stateChangingCommands has %d entries, want 11: update this test when the map changes", len(stateChangingCommands))
	}

	for cmd := range stateChangingCommands {
		t.Run("state-changing/"+cmd, func(t *testing.T) {
			if got := commandTimeout([]string{cmd, "somepkg"}); got != mutationTimeout {
				t.Errorf("commandTimeout(%q) = %v, want %v", cmd, got, mutationTimeout)
			}
		})
	}

	readCases := []struct {
		name string
		args []string
	}{
		{name: "read/outdated", args: []string{"outdated", "--json=v2"}},
		{name: "read/info", args: []string{"info", "--installed", "--json=v2", "--formula"}},
		{name: "read/bundle_check", args: []string{"bundle", "check", "--file=/tmp/Brewfile"}},
		{name: "read/bundle_check_no_upgrade", args: []string{"bundle", "check", "--no-upgrade", "--file=/tmp/Brewfile"}},
		{name: "read/bundle_list", args: []string{"bundle", "list"}},
		{name: "read/bundle_list_file", args: []string{"bundle", "list", "--file=/tmp/Brewfile"}},
		{name: "empty args", args: nil},
	}

	for _, tc := range readCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := commandTimeout(tc.args); got != readTimeout {
				t.Errorf("commandTimeout(%v) = %v, want %v", tc.args, got, readTimeout)
			}
		})
	}

	stateChangingBundleCases := []struct {
		name string
		args []string
	}{
		{name: "bundle install", args: []string{"bundle", "install"}},
		{name: "bundle install with file", args: []string{"bundle", "install", "--file=/tmp/Brewfile"}},
		{name: "bundle dump", args: []string{"bundle", "dump"}},
		{name: "bundle dump with file", args: []string{"bundle", "dump", "--file=/tmp/Brewfile"}},
		{name: "bare bundle defaults to install", args: []string{"bundle"}},
		{name: "bundle with file defaults to install", args: []string{"bundle", "--file=/tmp/Brewfile"}},
	}

	for _, tc := range stateChangingBundleCases {
		t.Run("state-changing/"+tc.name, func(t *testing.T) {
			if got := commandTimeout(tc.args); got != mutationTimeout {
				t.Errorf("commandTimeout(%v) = %v, want %v", tc.args, got, mutationTimeout)
			}
		})
	}
}

func TestBundleSubcommandsStateClassification(t *testing.T) {
	cases := []struct {
		args  []string
		state bool
	}{
		{[]string{"bundle", "check"}, false},
		{[]string{"bundle", "check", "--file=/path"}, false},
		{[]string{"bundle", "check", "--no-upgrade", "--file=/path"}, false},
		{[]string{"bundle", "list"}, false},
		{[]string{"bundle", "list", "--file=/path"}, false},
		{[]string{"bundle", "install"}, true},
		{[]string{"bundle", "install", "--file=/path"}, true},
		{[]string{"bundle", "dump"}, true},
		{[]string{"bundle", "dump", "--file=/path"}, true},
		{[]string{"bundle"}, true},
		{[]string{"bundle", "--file=/path"}, true},
	}

	for _, tc := range cases {
		if got := isStateChanging(tc.args); got != tc.state {
			t.Errorf("isStateChanging(%v) = %v, want %v", tc.args, got, tc.state)
		}
	}
}

func TestTimeoutConstants(t *testing.T) {
	if readTimeout != 30*time.Second {
		t.Errorf("readTimeout = %v, want 30s", readTimeout)
	}
	if mutationTimeout != 30*time.Minute {
		t.Errorf("mutationTimeout = %v, want 30m", mutationTimeout)
	}
}
