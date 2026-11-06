package fsutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFileAtomicReplacesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	old, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	if err := WriteFileAtomic(path, []byte("new content"), 0o600); err != nil {
		t.Fatal(err)
	}
	oldData := make([]byte, 3)
	if _, err := old.Read(oldData); err != nil || string(oldData) != "old" {
		t.Fatalf("open old file = %q, %v", oldData, err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "new content" {
		t.Fatalf("new file = %q, %v", data, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 600", info.Mode().Perm())
	}
}

func TestWriteFileAtomicCleansUpOnRenameFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "destination")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileAtomic(path, []byte("data"), 0o644); err == nil {
		t.Fatal("expected rename to directory to fail")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "destination" || !entries[0].IsDir() {
		t.Fatalf("unexpected entries after failure: %v", entries)
	}
}
