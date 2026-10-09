package ubluehelper

import (
	"os"
	"path/filepath"
	"testing"
)

func writeGroupFiles(t *testing.T, etc, lib string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	etcPath := filepath.Join(dir, "etc-group")
	libPath := filepath.Join(dir, "lib-group")
	if err := os.WriteFile(etcPath, []byte(etc), 0o644); err != nil {
		t.Fatal(err)
	}
	if lib != "" {
		if err := os.WriteFile(libPath, []byte(lib), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return etcPath, libPath
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

const imageGroups = "root:x:0:\nkvm:x:36:qemu\ndocker:x:975:\nlibvirt:x:973:\n"

func TestEnsureLocalGroupCopiesImageOnlyGroupWithItsGID(t *testing.T) {
	etcPath, libPath := writeGroupFiles(t, "wheel:x:10:alice\nalice:x:1000:\n", imageGroups)

	copied, err := EnsureLocalGroup(etcPath, libPath, "docker", false)
	if err != nil || !copied {
		t.Fatalf("EnsureLocalGroup = %v, %v; want copied", copied, err)
	}
	if got, want := readFile(t, etcPath), "wheel:x:10:alice\nalice:x:1000:\ndocker:x:975:\n"; got != want {
		t.Fatalf("/etc/group = %q, want %q", got, want)
	}

	copied, err = EnsureLocalGroup(etcPath, libPath, "docker", false)
	if err != nil || copied {
		t.Fatalf("second EnsureLocalGroup = %v, %v; want no copy", copied, err)
	}
	if got := readFile(t, etcPath); got != "wheel:x:10:alice\nalice:x:1000:\ndocker:x:975:\n" {
		t.Fatalf("second call rewrote /etc/group: %q", got)
	}
}

func TestEnsureLocalGroupTerminatesAnUnterminatedLastLine(t *testing.T) {
	etcPath, libPath := writeGroupFiles(t, "wheel:x:10:alice", imageGroups)

	if copied, err := EnsureLocalGroup(etcPath, libPath, "kvm", false); err != nil || !copied {
		t.Fatalf("EnsureLocalGroup = %v, %v", copied, err)
	}
	if got, want := readFile(t, etcPath), "wheel:x:10:alice\nkvm:x:36:qemu\n"; got != want {
		t.Fatalf("/etc/group = %q, want %q", got, want)
	}
}

func TestEnsureLocalGroupLeavesFilesAloneWhenNoCopyApplies(t *testing.T) {
	for _, test := range []struct {
		name, etc, lib, group string
	}{
		{name: "already local", etc: "docker:x:900:alice\n", lib: imageGroups, group: "docker"},
		{name: "not in image", etc: "wheel:x:10:\n", lib: imageGroups, group: "dialout"},
		{name: "no image group file", etc: "wheel:x:10:\n", group: "docker"},
		{name: "prefix is not a match", etc: "wheel:x:10:\n", lib: "dockerroot:x:990:\n", group: "docker"},
	} {
		t.Run(test.name, func(t *testing.T) {
			etcPath, libPath := writeGroupFiles(t, test.etc, test.lib)
			copied, err := EnsureLocalGroup(etcPath, libPath, test.group, false)
			if err != nil || copied {
				t.Fatalf("EnsureLocalGroup = %v, %v; want no copy", copied, err)
			}
			if got := readFile(t, etcPath); got != test.etc {
				t.Fatalf("/etc/group changed to %q", got)
			}
		})
	}
}

func TestEnsureLocalGroupDryRunReportsWithoutWriting(t *testing.T) {
	etcPath, libPath := writeGroupFiles(t, "wheel:x:10:\n", imageGroups)

	copied, err := EnsureLocalGroup(etcPath, libPath, "libvirt", true)
	if err != nil || !copied {
		t.Fatalf("dry-run EnsureLocalGroup = %v, %v; want copy reported", copied, err)
	}
	if got := readFile(t, etcPath); got != "wheel:x:10:\n" {
		t.Fatalf("dry run wrote /etc/group: %q", got)
	}
}

func TestEnsureLocalGroupRefusesMalformedNamesAndMissingLocalFile(t *testing.T) {
	etcPath, libPath := writeGroupFiles(t, "wheel:x:10:\n", imageGroups)
	for _, group := range []string{"", "docker:x", "docker\nroot"} {
		if _, err := EnsureLocalGroup(etcPath, libPath, group, false); err == nil {
			t.Errorf("EnsureLocalGroup accepted group %q", group)
		}
	}
	if _, err := EnsureLocalGroup(filepath.Join(t.TempDir(), "missing"), libPath, "docker", false); err == nil {
		t.Error("EnsureLocalGroup accepted a missing /etc/group")
	}
}

func TestAccessGroupMatchesAccessArgs(t *testing.T) {
	for _, command := range []string{CommandKVMEnable, CommandDockerEnable} {
		group, ok := AccessGroup(command)
		_, args, argsOK := AccessArgs(command, "alice")
		if !ok || !argsOK || args[1] != group {
			t.Fatalf("AccessGroup(%s) = %q %v; AccessArgs = %v %v", command, group, ok, args, argsOK)
		}
	}
	for _, command := range []string{CommandDockerDisable, CommandDXEnable, "root"} {
		if group, ok := AccessGroup(command); ok {
			t.Errorf("AccessGroup(%s) = %q, want refusal", command, group)
		}
	}
}
