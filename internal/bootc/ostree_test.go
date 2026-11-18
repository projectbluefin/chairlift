package bootc

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/projectbluefin/chairlift/internal/registrytags"
)

// The ostree layout below follows a Utah host (bootc 1.17.4, ostree 2026.4)
// where unprivileged `bootc status` exits 1 with "must be executed as the
// root user". The boot entry is a symlink to the deployment directory; the
// fake tree uses the derived path so fstest.MapFS can hold it.
const (
	ostreeOS           = "bluefin"
	ostreeBootedCsum   = "1111111111111111111111111111111111111111111111111111111111111111"
	ostreeStagedCsum   = "2222222222222222222222222222222222222222222222222222222222222222"
	ostreeRollbackCsum = "3333333333333333333333333333333333333333333333333333333333333333"
	ostreeSerial       = "0"
	ostreeRef          = "ghcr.io/projectbluefin/utah:testing"
)

var (
	ostreeStagedAt   = time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	ostreeRollbackAt = time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
)

func ostreeOrigin(ref string) *fstest.MapFile {
	return &fstest.MapFile{Data: []byte("[origin]\ncontainer-image-reference=" + ref + "\n")}
}

func ostreeHost() fstest.MapFS {
	deploy := "ostree/deploy/" + ostreeOS + "/deploy/"
	return fstest.MapFS{
		"proc/cmdline":                            {Data: []byte("BOOT_IMAGE=/vmlinuz root=UUID=1 ostree=/ostree/boot.1/" + ostreeOS + "/" + ostreeBootedCsum + "/" + ostreeSerial + " rw\n")},
		"run/ostree/staged-deployment":            {Data: []byte("binary gvariant state")},
		deploy + ostreeBootedCsum + ".0/":         {Mode: fs.ModeDir},
		deploy + ostreeStagedCsum + ".0/":         {Mode: fs.ModeDir},
		deploy + ostreeRollbackCsum + ".0/":       {Mode: fs.ModeDir},
		deploy + ostreeBootedCsum + ".0.origin":   ostreeOrigin("ostree-unverified-registry:" + ostreeRef),
		deploy + ostreeStagedCsum + ".0.origin":   withModTime(ostreeOrigin("ostree-unverified-registry:"+ostreeRef), ostreeStagedAt),
		deploy + ostreeRollbackCsum + ".0.origin": withModTime(ostreeOrigin("ostree-unverified-registry:"+ostreeRef), ostreeRollbackAt),
		"usr/lib/os-release":                      {Data: []byte("NAME=\"Bluefin\"\nIMAGE_VERSION=\"20261117\"\n")},
	}
}

// withOstreeDigest replaces the repository read. A gated test must never run
// a real ostree: the developer's host may or may not have one.
func withOstreeDigest(t *testing.T, read func(commit string) (string, error)) {
	t.Helper()
	previous := ostreeManifestDigest
	ostreeManifestDigest = read
	t.Cleanup(func() { ostreeManifestDigest = previous })
}

// noOstreeDigest is the host whose repository digest cannot be read. It is
// the state every ostree reader test starts from unless it supplies a digest.
func noOstreeDigest(t *testing.T) {
	t.Helper()
	withOstreeDigest(t, func(commit string) (string, error) {
		return "", errors.New("no ostree repository on this host")
	})
}

func TestOstreeStatusReadsDeploymentsWithoutRoot(t *testing.T) {
	noOstreeDigest(t)
	status, err := readOstreeStatus(ostreeHost())
	if err != nil {
		t.Fatalf("readOstreeStatus: %v", err)
	}
	if !status.Booted() {
		t.Fatal("Booted() = false on an ostree-booted host")
	}
	if got := status.Status.Booted.ImageRef(); got != ostreeRef {
		t.Errorf("booted image = %q, want %q", got, ostreeRef)
	}
	if got := status.Status.Booted.Version(); got != "20261117" {
		t.Errorf("booted version = %q, want IMAGE_VERSION 20261117", got)
	}
	if got := status.Status.Staged.ImageRef(); got != ostreeRef {
		t.Errorf("staged image = %q, want %q", got, ostreeRef)
	}
	if got := status.Status.Rollback.ImageRef(); got != ostreeRef {
		t.Errorf("rollback image = %q, want %q", got, ostreeRef)
	}
	if status.Spec.Image == nil || status.Spec.Image.Image != ostreeRef {
		t.Errorf("spec image = %+v, want %q", status.Spec.Image, ostreeRef)
	}
	// An ostree origin records no OCI manifest digest, and this host's
	// repository read failed: the update check falls back to bootc rather
	// than comparing an empty digest.
	if got := status.Status.Booted.Digest(); got != "" {
		t.Errorf("booted digest = %q, want empty when the repository read fails", got)
	}
}

func TestOstreeStatusWithoutStagedDeployment(t *testing.T) {
	noOstreeDigest(t)
	host := ostreeHost()
	delete(host, "run/ostree/staged-deployment")

	status, err := readOstreeStatus(host)
	if err != nil {
		t.Fatalf("readOstreeStatus: %v", err)
	}
	if status.Status.Staged != nil {
		t.Fatalf("Staged = %+v, want nil when nothing is staged", status.Status.Staged)
	}
	// Nothing staged is the steady state; the newest non-booted deployment is
	// still the rollback, so Recovery keeps offering it.
	if status.Status.Rollback == nil {
		t.Fatal("Rollback = nil, want the newest non-booted deployment when nothing is staged")
	}
	if got := status.Status.Rollback.ImageRef(); got != ostreeRef {
		t.Errorf("rollback image = %q, want %q", got, ostreeRef)
	}
}

// A host whose kernel command line names no ostree deployment is not
// answered here; the caller falls back to `bootc status`.
func TestOstreeStatusDefersOnOtherHosts(t *testing.T) {
	noOstreeDigest(t)
	for _, cmdline := range []string{
		"BOOT_IMAGE=/vmlinuz root=UUID=1 rw\n",
		"root=UUID=1 ostree=/ostree/boot.1/" + ostreeOS + "/" + ostreeBootedCsum + "\n",
		"root=UUID=1 ostree=/ostree/boot.1/" + ostreeOS + "/" + ostreeBootedCsum + "/x\n",
		"root=UUID=1 ostree=/ostree/boot.1/" + ostreeOS + "/not-hex/" + ostreeSerial + "\n",
	} {
		host := fstest.MapFS{"proc/cmdline": {Data: []byte(cmdline)}}
		if _, err := readOstreeStatus(host); !errors.Is(err, errNotOstree) {
			t.Errorf("%q: err = %v, want errNotOstree", cmdline, err)
		}
	}
}

// The boot entry is a symlink to the deployment directory on a real host;
// resolving it is what makes the bootcsum-to-csum mapping correct.
func TestOstreeStatusResolvesTheBootSymlink(t *testing.T) {
	noOstreeDigest(t)
	root := t.TempDir()
	write := func(name, content string) {
		t.Helper()
		full := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("proc/cmdline", "root=UUID=1 ostree=/ostree/boot.1/"+ostreeOS+"/"+ostreeBootedCsum+"/"+ostreeSerial+" rw\n")
	write("usr/lib/os-release", "IMAGE_VERSION=\"20261117\"\n")
	write("ostree/deploy/"+ostreeOS+"/deploy/"+ostreeBootedCsum+".0.origin", "[origin]\ncontainer-image-reference=ostree-unverified-registry:"+ostreeRef+"\n")
	bootDir := filepath.Join(root, "ostree", "boot.1", ostreeOS, ostreeBootedCsum)
	if err := os.MkdirAll(bootDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../../deploy/"+ostreeOS+"/deploy/"+ostreeBootedCsum+".0", filepath.Join(bootDir, ostreeSerial)); err != nil {
		t.Fatal(err)
	}

	status, err := readOstreeStatus(os.DirFS(root))
	if err != nil {
		t.Fatalf("readOstreeStatus: %v", err)
	}
	if got := status.Status.Booted.ImageRef(); got != ostreeRef {
		t.Errorf("booted image = %q, want %q", got, ostreeRef)
	}
}

func TestOstreeImageReferenceStripsTransports(t *testing.T) {
	for _, ref := range []string{
		"ostree-unverified-registry:" + ostreeRef,
		"ostree-unverified-image:docker://" + ostreeRef,
		"ostree-image-signed:docker://" + ostreeRef,
		"ostree-remote-registry:fedora:" + ostreeRef,
		"ostree-remote-image:fedora:docker://" + ostreeRef,
		"docker://" + ostreeRef,
		ostreeRef,
	} {
		got, err := ostreeImageReference(ref)
		if err != nil {
			t.Errorf("%q: %v", ref, err)
			continue
		}
		if got != ostreeRef {
			t.Errorf("%q = %q, want %q", ref, got, ostreeRef)
		}
	}
	for _, ref := range []string{
		"",
		"containers-storage:localhost/utah:testing",
		"ostree-remote-registry:fedora",
		"ostree-unverified-registry:",
	} {
		if _, err := ostreeImageReference(ref); err == nil {
			t.Errorf("%q: err = nil, want an unsupported-reference error", ref)
		}
	}
}

// A host that is ostree-booted but whose booted origin cannot be read is a
// failure, not "not bootc": saying there is no operating-system update
// source would be the same false negative this reader exists to remove.
func TestOstreeStatusMissingBootedOriginFails(t *testing.T) {
	noOstreeDigest(t)
	host := ostreeHost()
	delete(host, "ostree/deploy/"+ostreeOS+"/deploy/"+ostreeBootedCsum+".0.origin")
	_, err := readOstreeStatus(host)
	if err == nil || errors.Is(err, errNotOstree) {
		t.Fatalf("err = %v, want a read failure", err)
	}
}

func TestOstreeStatusMalformedOriginFails(t *testing.T) {
	noOstreeDigest(t)
	host := ostreeHost()
	host["ostree/deploy/"+ostreeOS+"/deploy/"+ostreeBootedCsum+".0.origin"] = &fstest.MapFile{Data: []byte("[origin]\n")}
	_, err := readOstreeStatus(host)
	if err == nil || errors.Is(err, errNotOstree) {
		t.Fatalf("err = %v, want a read failure", err)
	}
}

func TestOstreeStatusStagedMarkerWithoutDeploymentFails(t *testing.T) {
	noOstreeDigest(t)
	host := ostreeHost()
	delete(host, "ostree/deploy/"+ostreeOS+"/deploy/"+ostreeStagedCsum+".0.origin")
	delete(host, "ostree/deploy/"+ostreeOS+"/deploy/"+ostreeRollbackCsum+".0.origin")
	_, err := readOstreeStatus(host)
	if err == nil || errors.Is(err, errNotOstree) {
		t.Fatalf("err = %v, want a read failure", err)
	}
}

// GetStatus must answer from ostree state without running bootc, which
// refuses the read to an unprivileged caller.
func TestGetStatusUsesOstreeStateWithoutBootc(t *testing.T) {
	noOstreeDigest(t)
	argv := installFakeBootc(t, "echo 'error: must be executed as the root user' >&2; exit 1\n")
	withHostRoot(t, ostreeHost())

	status, err := GetStatus(testContext(t))
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	if got := status.Status.Booted.ImageRef(); got != ostreeRef {
		t.Errorf("booted image = %q, want %q", got, ostreeRef)
	}
	if _, err := os.Stat(argv); err == nil {
		t.Fatal("bootc was executed on an ostree host")
	}
}

// When the repository digest cannot be read, CheckUpdate must not compare an
// empty digest against the registry; it falls back to bootc.
func TestCheckUpdateOnOstreeFallsBackToBootc(t *testing.T) {
	argv := installFakeBootc(t, "echo 'No changes in: "+ostreeRef+"'\n")
	withHostRoot(t, ostreeHost())
	noOstreeDigest(t)
	withRegistryTag(t, func(context.Context, string, string) (registrytags.Tag, error) {
		t.Fatal("registry was queried for an ostree host with no manifest digest")
		return registrytags.Tag{}, nil
	})

	update, err := CheckUpdate(testContext(t))
	if err != nil {
		t.Fatalf("CheckUpdate: %v", err)
	}
	if update.Available {
		t.Fatalf("CheckUpdate = %+v, want no update", update)
	}
	if _, err := os.Stat(argv); err != nil {
		t.Fatal("bootc upgrade --check was not executed")
	}
}

// An ostree .origin records no OCI manifest digest, but the commit in the
// repository does and the repository is world-readable. Reading it is what
// keeps the update check on the registry path, which needs no root (#837).
func TestOstreeStatusReadsTheRepositoryManifestDigest(t *testing.T) {
	digests := map[string]string{
		ostreeBootedCsum:   "sha256:" + strings.Repeat("a", 64),
		ostreeStagedCsum:   "sha256:" + strings.Repeat("b", 64),
		ostreeRollbackCsum: "sha256:" + strings.Repeat("c", 64),
	}
	var asked []string
	withOstreeDigest(t, func(commit string) (string, error) {
		asked = append(asked, commit)
		digest, ok := digests[commit]
		if !ok {
			return "", errors.New("unknown commit")
		}
		return digest, nil
	})

	status, err := readOstreeStatus(ostreeHost())
	if err != nil {
		t.Fatalf("readOstreeStatus: %v", err)
	}
	if got := status.Status.Booted.Digest(); got != digests[ostreeBootedCsum] {
		t.Errorf("booted digest = %q, want %q", got, digests[ostreeBootedCsum])
	}
	// Staged matters as much: the check compares the registry digest against
	// it, so an empty staged digest would report an already-staged image as a
	// new update.
	if got := status.Status.Staged.Digest(); got != digests[ostreeStagedCsum] {
		t.Errorf("staged digest = %q, want %q", got, digests[ostreeStagedCsum])
	}
	if got := status.Status.Rollback.Digest(); got != digests[ostreeRollbackCsum] {
		t.Errorf("rollback digest = %q, want %q", got, digests[ostreeRollbackCsum])
	}
	if len(asked) != 3 {
		t.Errorf("repository reads = %v, want one per deployment", asked)
	}
}

// Values this reader cannot be sure of leave the digest empty so the caller
// falls back, rather than comparing a fabricated digest to the registry.
func TestParseOstreeManifestDigest(t *testing.T) {
	valid := "sha256:" + strings.Repeat("0123456789abcdef", 4)
	tests := []struct {
		name    string
		out     string
		want    string
		wantErr bool
	}{
		{"gvariant string", "'" + valid + "'\n", valid, false},
		{"bare digest", valid + "\n", valid, false},
		{"empty", "", "", true},
		{"whitespace only", "  \n", "", true},
		{"truncated", "'sha256:abc'\n", "", true},
		{"uppercase hex", "'sha256:" + strings.Repeat("A", 64) + "'\n", "", true},
		{"other algorithm", "'sha512:" + strings.Repeat("a", 128) + "'\n", "", true},
		{"unquoted trailing garbage", valid + " extra\n", "", true},
		{"two values", "'" + valid + "'\n'" + valid + "'\n", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseOstreeManifestDigest(tt.out)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseOstreeManifestDigest(%q) = %q, nil error; want a rejection", tt.out, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseOstreeManifestDigest(%q): %v", tt.out, err)
			}
			if got != tt.want {
				t.Fatalf("parseOstreeManifestDigest(%q) = %q, want %q", tt.out, got, tt.want)
			}
		})
	}
}

// With the repository digest present, an ostree host answers the update check
// from the registry exactly as a composefs host does, and never asks the
// unprivileged bootc that refuses.
func TestCheckUpdateOnOstreeComparesTheRepositoryDigest(t *testing.T) {
	argv := installFakeBootc(t, "echo 'error: must be executed as the root user' >&2; exit 1\n")
	withHostRoot(t, ostreeHost())
	withOstreeDigest(t, func(string) (string, error) {
		return "sha256:" + strings.Repeat("a", 64), nil
	})
	published := "sha256:" + strings.Repeat("b", 64)
	var queries []string
	withRegistryTag(t, func(_ context.Context, repository, tag string) (registrytags.Tag, error) {
		queries = append(queries, repository+":"+tag)
		return registrytags.Tag{
			Digest:  published,
			Created: time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC),
		}, nil
	})

	update, err := CheckUpdate(testContext(t))
	if err != nil {
		t.Fatalf("CheckUpdate: %v", err)
	}
	want := AvailableUpdate{Available: true, Version: "20261117", Digest: published}
	if update != want {
		t.Fatalf("CheckUpdate = %+v, want %+v", update, want)
	}
	if len(queries) != 1 || queries[0] != ostreeRef {
		t.Fatalf("registry queries = %v, want [%q]", queries, ostreeRef)
	}
	if _, err := os.Stat(argv); err == nil {
		t.Fatal("bootc upgrade --check was executed although the repository records a digest")
	}
}

// A composefs host still resolves through the composefs reader, not the
// ostree one, even though both read the same kernel command line.
func TestComposefsHostStillResolvesThroughComposefs(t *testing.T) {
	withHostRoot(t, dakotaHost())
	status, err := GetStatus(testContext(t))
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	if got := status.Status.Booted.Digest(); got != bootedDigest {
		t.Errorf("booted digest = %q, want the composefs digest %q", got, bootedDigest)
	}
	if !strings.HasPrefix(status.Status.Booted.ImageRef(), "ghcr.io/projectbluefin/dakota-gaming") {
		t.Errorf("booted image = %q, want the composefs deployment", status.Status.Booted.ImageRef())
	}
}
