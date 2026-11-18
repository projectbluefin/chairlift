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

func TestOstreeStatusReadsDeploymentsWithoutRoot(t *testing.T) {
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
	// An ostree origin records no OCI manifest digest; the update check
	// falls back to bootc rather than comparing an empty digest.
	if got := status.Status.Booted.Digest(); got != "" {
		t.Errorf("booted digest = %q, want empty on an ostree origin", got)
	}
}

func TestOstreeStatusWithoutStagedDeployment(t *testing.T) {
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
	host := ostreeHost()
	delete(host, "ostree/deploy/"+ostreeOS+"/deploy/"+ostreeBootedCsum+".0.origin")
	_, err := readOstreeStatus(host)
	if err == nil || errors.Is(err, errNotOstree) {
		t.Fatalf("err = %v, want a read failure", err)
	}
}

func TestOstreeStatusMalformedOriginFails(t *testing.T) {
	host := ostreeHost()
	host["ostree/deploy/"+ostreeOS+"/deploy/"+ostreeBootedCsum+".0.origin"] = &fstest.MapFile{Data: []byte("[origin]\n")}
	_, err := readOstreeStatus(host)
	if err == nil || errors.Is(err, errNotOstree) {
		t.Fatalf("err = %v, want a read failure", err)
	}
}

func TestOstreeStatusStagedMarkerWithoutDeploymentFails(t *testing.T) {
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

// An ostree origin has no OCI manifest digest, so CheckUpdate must not
// compare an empty digest against the registry; it falls back to bootc.
func TestCheckUpdateOnOstreeFallsBackToBootc(t *testing.T) {
	argv := installFakeBootc(t, "echo 'No changes in: "+ostreeRef+"'\n")
	withHostRoot(t, ostreeHost())
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
