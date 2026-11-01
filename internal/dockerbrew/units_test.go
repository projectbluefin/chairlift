package dockerbrew

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func unitLines(body string) map[string][]string {
	out := map[string][]string{}
	for _, l := range strings.Split(body, "\n") {
		if k, v, ok := strings.Cut(l, "="); ok && !strings.HasPrefix(l, "#") {
			out[k] = append(out[k], v)
		}
	}
	return out
}

func TestServiceUnitRunsTheEngineUnderRoot(t *testing.T) {
	body := ServiceUnit("/opt/x")
	if first, _, _ := strings.Cut(body, "\n"); first != ManagedMarker {
		t.Fatalf("first line = %q", first)
	}
	kv := unitLines(body)
	if got := kv["ExecStart"]; len(got) != 1 || got[0] != "/opt/x/current/bin/dockerd -H fd://" {
		t.Errorf("ExecStart = %q", got)
	}
	if got := kv["Environment"]; len(got) != 1 || got[0] != "PATH=/opt/x/current/bin:/usr/sbin:/usr/bin:/sbin:/bin" {
		t.Errorf("Environment = %q", got)
	}
	// Everything else is moby's unit.
	for k, want := range map[string]string{
		"Requires": "docker.socket", "Type": "notify", "Delegate": "yes",
		"KillMode": "process", "WantedBy": "multi-user.target", "Restart": "always",
	} {
		if got := kv[k]; len(got) != 1 || got[0] != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	if strings.Contains(body, "/usr/bin/dockerd") || strings.Contains(body, "--containerd") {
		t.Error("service still names moby's packaged paths")
	}
}

func TestSocketUnitIsMobysSocket(t *testing.T) {
	body := SocketUnit()
	if first, _, _ := strings.Cut(body, "\n"); first != ManagedMarker {
		t.Fatalf("first line = %q", first)
	}
	kv := unitLines(body)
	for k, want := range map[string]string{
		"ListenStream": "/run/docker.sock", "SocketMode": "0660", "SocketUser": "root",
		"SocketGroup": "docker", "WantedBy": "sockets.target",
	} {
		if got := kv[k]; len(got) != 1 || got[0] != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
}

func TestWriteUnitsWritesOnceAndRewritesOnChange(t *testing.T) {
	dir := t.TempDir()
	changed, err := WriteUnits(dir, Root)
	if err != nil || !changed {
		t.Fatalf("WriteUnits = %v, %v", changed, err)
	}
	for name, want := range map[string]string{ServiceName: ServiceUnit(Root), SocketName: SocketUnit()} {
		p := filepath.Join(dir, name)
		data, err := os.ReadFile(p)
		if err != nil || string(data) != want {
			t.Fatalf("%s: %v", name, err)
		}
		if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o644 {
			t.Errorf("%s mode = %v", name, fi.Mode())
		}
	}
	if changed, err := WriteUnits(dir, Root); err != nil || changed {
		t.Fatalf("second WriteUnits = %v, %v; want unchanged", changed, err)
	}
	if changed, err := WriteUnits(dir, "/opt/other"); err != nil || !changed {
		t.Fatalf("WriteUnits for another root = %v, %v", changed, err)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, ServiceName)); string(data) != ServiceUnit("/opt/other") {
		t.Error("managed service was not rewritten")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 2 {
		t.Errorf("left temporary files: %v", entries)
	}
}

func TestWriteUnitsRefusesUnitsItDoesNotManage(t *testing.T) {
	for name, setup := range map[string]func(dir string){
		"administrator's socket": func(dir string) {
			if err := os.WriteFile(filepath.Join(dir, SocketName), []byte("[Socket]\nListenStream=/run/docker.sock\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		},
		"marker not on the first line": func(dir string) {
			if err := os.WriteFile(filepath.Join(dir, ServiceName), []byte("[Unit]\n"+ManagedMarker+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		},
		"masked service": func(dir string) {
			if err := os.Symlink("/dev/null", filepath.Join(dir, ServiceName)); err != nil {
				t.Fatal(err)
			}
		},
		"symlink to a marked unit": func(dir string) {
			target := filepath.Join(t.TempDir(), ServiceName)
			if err := os.WriteFile(target, []byte(ServiceUnit("/elsewhere")), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, filepath.Join(dir, ServiceName)); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			setup(dir)
			before := snapshot(t, dir)
			if err := CheckUnits(dir); !errors.Is(err, ErrUnmanagedUnit) {
				t.Fatalf("CheckUnits err = %v, want ErrUnmanagedUnit", err)
			}
			if _, err := WriteUnits(dir, Root); !errors.Is(err, ErrUnmanagedUnit) {
				t.Fatalf("err = %v, want ErrUnmanagedUnit", err)
			}
			if after := snapshot(t, dir); after != before {
				t.Errorf("refusal changed the directory:\n%s\n->\n%s", before, after)
			}
		})
	}
}

func snapshot(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		if target, err := os.Readlink(p); err == nil {
			b.WriteString(e.Name() + " -> " + target + "\n")
			continue
		}
		data, _ := os.ReadFile(p)
		b.WriteString(e.Name() + ": " + string(data) + "\n")
	}
	return b.String()
}

func TestRemoveManagedUnitsDeletesOnlyMarkedFiles(t *testing.T) {
	dir := t.TempDir()
	if removed, err := RemoveManagedUnits(dir); err != nil || removed {
		t.Fatalf("empty dir: %v, %v", removed, err)
	}
	if _, err := WriteUnits(dir, Root); err != nil {
		t.Fatal(err)
	}
	theirs := "[Socket]\nListenStream=/run/docker.sock\n"
	if err := os.WriteFile(filepath.Join(dir, SocketName), []byte(theirs), 0o644); err != nil {
		t.Fatal(err)
	}

	removed, err := RemoveManagedUnits(dir)
	if err != nil || !removed {
		t.Fatalf("RemoveManagedUnits = %v, %v", removed, err)
	}
	if _, err := os.Lstat(filepath.Join(dir, ServiceName)); !errors.Is(err, os.ErrNotExist) {
		t.Error("managed service survived")
	}
	if data, _ := os.ReadFile(filepath.Join(dir, SocketName)); string(data) != theirs {
		t.Error("unmanaged socket was removed or changed")
	}

	// A symlink is never ours, even when what it points at carries the marker.
	marked := filepath.Join(t.TempDir(), "marked")
	if err := os.WriteFile(marked, []byte(SocketUnit()), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"/dev/null", marked} {
		linked := t.TempDir()
		if err := os.Symlink(target, filepath.Join(linked, SocketName)); err != nil {
			t.Fatal(err)
		}
		if removed, err := RemoveManagedUnits(linked); err != nil || removed {
			t.Fatalf("symlink to %s: %v, %v", target, removed, err)
		}
		if _, err := os.Lstat(filepath.Join(linked, SocketName)); err != nil {
			t.Errorf("symlink to %s was removed", target)
		}
	}
}

func TestCheckUnitsAcceptsMissingAndManagedUnits(t *testing.T) {
	dir := t.TempDir()
	if err := CheckUnits(dir); err != nil {
		t.Fatalf("empty dir: %v", err)
	}
	if _, err := WriteUnits(dir, Root); err != nil {
		t.Fatal(err)
	}
	if err := CheckUnits(dir); err != nil {
		t.Fatalf("managed units: %v", err)
	}
}
