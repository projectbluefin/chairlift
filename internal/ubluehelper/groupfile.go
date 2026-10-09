package ubluehelper

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
)

const (
	// EtcGroupPath is the mutable group database usermod edits.
	EtcGroupPath = "/etc/group"
	// LibGroupPath is the image's read-only group database, resolved through
	// nss-altfiles. rpm-ostree composes (Bluefin, Bluefin LTS) record the
	// groups their packages create here rather than in /etc/group.
	LibGroupPath = "/usr/lib/group"
)

// EnsureLocalGroup makes group editable by usermod. A group that only the
// image's /usr/lib/group defines resolves through NSS, but usermod refuses to
// add a member to it because it edits /etc/group alone; the documented fix
// (Fedora Atomic Desktops troubleshooting, "Unable to add user to group", and
// common's `ujust` devmode recipes) is to copy the group's line, GID included,
// into /etc/group first. copied reports whether that copy was needed.
//
// A group already in etcPath, or absent from libPath (or libPath itself
// absent), is left alone: usermod then succeeds or reports the missing group
// itself. dryRun reports what would be copied without writing.
func EnsureLocalGroup(etcPath, libPath, group string, dryRun bool) (copied bool, err error) {
	if group == "" || strings.ContainsAny(group, ":\n") {
		return false, fmt.Errorf("invalid group name %q", group)
	}
	local, err := os.ReadFile(etcPath)
	if err != nil {
		return false, err
	}
	if _, found := groupLine(local, group); found {
		return false, nil
	}
	image, err := os.ReadFile(libPath)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	line, found := groupLine(image, group)
	if !found {
		return false, nil
	}
	if dryRun {
		return true, nil
	}

	f, err := os.OpenFile(etcPath, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return false, err
	}
	entry := line + "\n"
	if len(local) > 0 && local[len(local)-1] != '\n' {
		entry = "\n" + entry
	}
	if _, err := f.WriteString(entry); err != nil {
		_ = f.Close()
		return false, err
	}
	return true, f.Close()
}

// groupLine returns the group(5) entry for group, which is the line whose
// first colon-separated field is exactly group.
func groupLine(data []byte, group string) (string, bool) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		if name, _, ok := strings.Cut(line, ":"); ok && name == group {
			return line, true
		}
	}
	return "", false
}
