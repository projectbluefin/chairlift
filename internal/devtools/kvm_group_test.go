package devtools

import (
	"context"
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
)

func fakeKVMGroups(t *testing.T, getent, id string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range map[string]string{"getent": getent, "id": id} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	return dir
}

func TestNSLKVMGroupMemberUsesAccountGroups(t *testing.T) {
	account, err := user.Current()
	if err != nil {
		t.Skip(err)
	}
	fakeKVMGroups(t, "echo 'kvm:x:36:'\n", `[ "$1 $2 $3" = "-G -- `+account.Username+`" ] && echo '1000 36' || echo '1000'`+"\n")
	if member, err := NSLKVMGroupMember(); !member || err != nil {
		t.Fatalf("membership = %v, %v; want account database lookup by name", member, err)
	}
}

func TestNSLKVMGroupMember(t *testing.T) {
	for _, tc := range []struct {
		name, getent, id string
		want             bool
		wantErr          string
	}{
		{"member", "echo 'kvm:x:36:'\n", "echo '1000 36'\n", true, ""},
		{"not a member despite readable device", "echo 'kvm:x:36:'\n", "echo '1000 37'\n", false, ""},
		{"missing group", "exit 2\n", "echo '1000'\n", false, "look up kvm group"},
		{"malformed group", "echo 'kvm:x::'\n", "echo '1000'\n", false, "invalid kvm group entry"},
		{"group read failed", "echo 'kvm:x:36:'\n", "exit 2\n", false, "read account groups"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fakeKVMGroups(t, tc.getent, tc.id)
			got, err := NSLKVMGroupMember()
			if got != tc.want || (tc.wantErr == "" && err != nil) || (tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr))) {
				t.Fatalf("membership = %v, %v; want %v, %q", got, err, tc.want, tc.wantErr)
			}
		})
	}
}

func TestNSLEnableRequestsAccessWithoutKVMGroup(t *testing.T) {
	if !NSLHostSupported() {
		t.Skip("nsl requires Linux amd64")
	}
	granted := filepath.Join(t.TempDir(), "granted")
	dir := fakeKVMGroups(t, "echo 'kvm:x:36:'\n", "[ -e '"+granted+"' ] && echo '1000 36' || echo '1000 37'\n")
	if err := os.WriteFile(filepath.Join(dir, "nsl"), []byte("#!/bin/sh\necho doctor failed\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	previousAvailable, previousEnable := nslKVMAvailable, nslEnableKVMAccess
	t.Cleanup(func() { nslKVMAvailable, nslEnableKVMAccess = previousAvailable, previousEnable })
	nslKVMAvailable = func() (bool, bool) { return true, true }
	called := false
	nslEnableKVMAccess = func(context.Context) error { called = true; return os.WriteFile(granted, nil, 0o600) }
	var stages []string
	err := SetWSL(context.Background(), BackendNSL, true, func(stage string) { stages = append(stages, stage) })
	// A group grant on a writable device continues setup without a new login.
	if !called || err == nil || errors.Is(err, ErrNewLogin) || strings.Join(stages, "|") != "Asking for virtual machine access…|Checking this computer…" {
		t.Fatalf("helper called = %v, stages = %q, error = %v", called, stages, err)
	}
	if err := os.Remove(granted); err != nil {
		t.Fatal(err)
	}
	nslEnableKVMAccess = func(context.Context) error { return nil }
	if err := SetWSL(context.Background(), BackendNSL, true, nil); err == nil || errors.Is(err, ErrNewLogin) {
		t.Fatalf("grant without membership returned %v, want an error other than ErrNewLogin", err)
	}
	nslKVMAvailable = func() (bool, bool) { return true, false }
	if err := SetWSL(context.Background(), BackendNSL, true, nil); !errors.Is(err, ErrNewLogin) {
		t.Fatalf("device grant returned %v, want ErrNewLogin", err)
	}
	nslKVMAvailable = func() (bool, bool) { return true, true }
	denied := errors.New("authorization denied")
	nslEnableKVMAccess = func(context.Context) error { return denied }
	if err := SetWSL(context.Background(), BackendNSL, true, nil); !errors.Is(err, denied) {
		t.Fatalf("denied authorization returned %v, want %v", err, denied)
	}
}
