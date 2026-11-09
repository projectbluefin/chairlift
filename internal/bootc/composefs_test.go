package bootc

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/projectbluefin/chairlift/internal/registrytags"
)

// Deployment IDs, digests, and file contents below are copied from a live
// Dakota host (ghcr.io/projectbluefin/dakota-gaming:testing, bootc 1.16.13,
// composefs backend) on 2026-09-26, where unprivileged `bootc status` and
// `bootc upgrade --check` both exit 1 with "must be executed as the root
// user".
const (
	bootedID   = "8964adf95b93187aa5a8dddd471faaafd2f43c3b7d2957cb0d20c67b225f94bab099c9aefb5d5ca62d24ed884c0de93c3981beac7980e442bfbcf7e66111a830"
	stagedID   = "a67ae7d803acf7d2eb6c586739dbae795f4bc717c1e8ca3b8bec842457f46bd2344d408ba3d5dccf4f69f29b56443e40e9c66f14713a4607599e0a7ef1ef1257"
	rollbackID = "99f5c50825f9ab0703cade0d09a5fa3791faa8ffe480a5d2e0b158493de74b7b738b55d67e03d140a7a552f6731232e4c45a5579bb92accf2c8a4bcea5380b96"

	bootedDigest   = "sha256:875dcb8011673eefb0014d69ca5c7ef3f7c21cd79e380658d89caad987a4c58b"
	stagedDigest   = "sha256:706a6e51c4dd347e9b17330aed55b7b168c5dadf5583b941fade6e4ab7461c25"
	rollbackDigest = "sha256:240a747f2940b904e3479a34463e34cb25393807a208ece7c0f36ce02475f4d1"
)

func origin(digest string) *fstest.MapFile {
	return &fstest.MapFile{Data: []byte("[origin]\n" +
		"container-image-reference = ostree-unverified-image:docker://ghcr.io/projectbluefin/dakota-gaming:testing\n\n" +
		"[boot]\nboot_type = bls\ndigest = b3158a10a65063e15191614583e8d877f2ebb5e3fddc49128953438d1164e6e2\n\n" +
		"[image]\nmanifest_digest = " + digest + "\n")}
}

// rollbackDeployed is when the rollback deployment was written on the host.
var rollbackDeployed = time.Date(2026, 9, 17, 20, 46, 14, 0, time.UTC)

func withModTime(file *fstest.MapFile, when time.Time) *fstest.MapFile {
	file.ModTime = when
	return file
}

func dakotaHost() fstest.MapFS {
	deploy := "sysroot/state/deploy/"
	return fstest.MapFS{
		"proc/cmdline":                                     {Data: []byte(`initrd=\EFI\Linux\bootc_composefs-4796\initrd composefs=` + bootedID + " rw quiet splash\n")},
		"run/composefs/staged-deployment":                  {Data: []byte(`{"depl_id":"` + stagedID + `","finalization_locked":false}`)},
		deploy + bootedID + "/" + bootedID + ".origin":     origin(bootedDigest),
		deploy + stagedID + "/" + stagedID + ".origin":     origin(stagedDigest),
		deploy + rollbackID + "/" + rollbackID + ".origin": withModTime(origin(rollbackDigest), rollbackDeployed),
		"usr/lib/os-release":                               {Data: []byte("NAME=\"Bluefin\"\nID=\"bluefin-dakota\"\nIMAGE_REF=\"ostree-image-signed:docker://ghcr.io/projectbluefin/dakota-gaming\"\nVERSION_ID=\"20260921\"\nIMAGE_VERSION=\"20260921\"\n")},
	}
}

func TestComposefsStatusReadsDeploymentsWithoutRoot(t *testing.T) {
	status, err := readComposefsStatus(dakotaHost())
	if err != nil {
		t.Fatalf("readComposefsStatus: %v", err)
	}
	if !status.Booted() {
		t.Fatal("Booted() = false on a composefs-booted host")
	}
	const ref = "ghcr.io/projectbluefin/dakota-gaming:testing"
	if got := status.Status.Booted.ImageRef(); got != ref {
		t.Errorf("booted image = %q, want %q", got, ref)
	}
	if got := status.Status.Booted.Digest(); got != bootedDigest {
		t.Errorf("booted digest = %q, want %q", got, bootedDigest)
	}
	if got := status.Status.Booted.Version(); got != "20260921" {
		t.Errorf("booted version = %q, want the image's IMAGE_VERSION 20260921", got)
	}
	if got := status.Status.Staged.Digest(); got != stagedDigest {
		t.Errorf("staged digest = %q, want %q", got, stagedDigest)
	}
	if got := status.Status.Rollback.Digest(); got != rollbackDigest {
		t.Errorf("rollback digest = %q, want %q", got, rollbackDigest)
	}
	// The origin's mtime is when the deployment was written, not when its
	// image was released; the Roll Back row labels the timestamp "released",
	// so it must stay empty rather than claim the deploy day (#521).
	if got := status.Status.Rollback.Timestamp(); got != "" {
		t.Errorf("rollback timestamp = %q, want empty (not the origin mtime %q)", got, rollbackDeployed.Format(time.RFC3339))
	}
	if got := status.Status.Booted.Timestamp(); got != "" {
		t.Errorf("booted timestamp = %q, want empty: a deployment time is not a release date", got)
	}
	if status.Spec.Image == nil || status.Spec.Image.Image != ref {
		t.Errorf("spec image = %+v, want %q", status.Spec.Image, ref)
	}
}

func TestComposefsStatusWithoutStagedDeployment(t *testing.T) {
	host := dakotaHost()
	delete(host, "run/composefs/staged-deployment")
	delete(host, "sysroot/state/deploy/"+stagedID+"/"+stagedID+".origin")

	status, err := readComposefsStatus(host)
	if err != nil {
		t.Fatalf("readComposefsStatus: %v", err)
	}
	if status.Status.Staged != nil {
		t.Fatalf("Staged = %+v, want nil when nothing is staged", status.Status.Staged)
	}
	if got := status.Status.Rollback.Digest(); got != rollbackDigest {
		t.Errorf("rollback digest = %q, want %q", got, rollbackDigest)
	}
}

// A host whose kernel command line names no composefs deployment is not
// answered here; the caller falls back to `bootc status`.
func TestComposefsStatusDefersOnOtherHosts(t *testing.T) {
	host := fstest.MapFS{"proc/cmdline": {Data: []byte("BOOT_IMAGE=/vmlinuz root=UUID=1 ostree=/ostree/boot.1/fedora/abc/0\n")}}
	if _, err := readComposefsStatus(host); !errors.Is(err, errNotComposefs) {
		t.Fatalf("err = %v, want errNotComposefs", err)
	}
}

// bootc 1.17 names the booted deployment with composefs.digest= after an
// upgrade (#738): Dakota testing moved to bootc 1.17.3, and missing that
// argument sent the reader to root-only `bootc status`, so the Early Updates
// and Game Mode switches said they couldn't verify the system update state.
func TestComposefsStatusReadsBootc117DigestArgument(t *testing.T) {
	for _, arg := range []string{
		"composefs.digest=v1-sha512-12:" + bootedID,
		"composefs.digest=?v1-sha512-12:" + bootedID,
		"composefs.digest=v2-sha512-12:" + bootedID,
	} {
		host := dakotaHost()
		host["proc/cmdline"] = &fstest.MapFile{Data: []byte(`initrd=\EFI\Linux\bootc_composefs-4796\initrd ` + arg + " rw quiet splash\n")}
		status, err := readComposefsStatus(host)
		if err != nil {
			t.Fatalf("%s: readComposefsStatus: %v", arg, err)
		}
		if got := status.Status.Booted.Digest(); got != bootedDigest {
			t.Errorf("%s: booted digest = %q, want %q", arg, got, bootedDigest)
		}
	}
}

// A UKI can carry the V1 digest followed by a composefs= fallback; the
// initramfs boots the first whose image exists, so the booted deployment is
// the first candidate with state on disk, not simply the first argument.
func TestComposefsBootedIDFollowsTheFallbackThatBooted(t *testing.T) {
	missing := strings.Repeat("ab", 64)
	host := dakotaHost()
	host["proc/cmdline"] = &fstest.MapFile{Data: []byte("composefs.digest=v1-sha512-12:" + missing + " composefs=" + bootedID + " rw\n")}
	id, err := composefsBootedID(host)
	if err != nil || id != bootedID {
		t.Fatalf("composefsBootedID = %q, %v; want the composefs= fallback %q", id, err, bootedID)
	}

	// With no candidate on disk the first is reported, so the status read
	// fails loudly instead of claiming the host is not composefs-booted.
	host["proc/cmdline"] = &fstest.MapFile{Data: []byte("composefs.digest=v1-sha512-12:" + missing + " rw\n")}
	if _, err := readComposefsStatus(host); err == nil || errors.Is(err, errNotComposefs) {
		t.Fatalf("err = %v, want a read failure for the missing deployment", err)
	}
}

// Only SHA-512 composefs.digest= values name a bootc deployment.
func TestComposefsBootedIDRejectsForeignDigests(t *testing.T) {
	for _, cmdline := range []string{
		"composefs.digest=v1-sha256-12:" + strings.Repeat("ab", 32),
		"composefs.digest=v9-sha512-12:" + bootedID,
		"composefs.digest=" + bootedID,
	} {
		host := fstest.MapFS{"proc/cmdline": {Data: []byte(cmdline + "\n")}}
		if _, err := composefsBootedID(host); !errors.Is(err, errNotComposefs) {
			t.Errorf("%s: err = %v, want errNotComposefs", cmdline, err)
		}
	}
}

// A composefs host whose booted origin cannot be read is a failure, not
// "not bootc": saying there is no operating-system update source would be
// the same false negative this reader exists to remove.
func TestComposefsStatusMissingBootedOriginFails(t *testing.T) {
	host := dakotaHost()
	delete(host, "sysroot/state/deploy/"+bootedID+"/"+bootedID+".origin")
	_, err := readComposefsStatus(host)
	if err == nil || errors.Is(err, errNotComposefs) {
		t.Fatalf("err = %v, want a read failure", err)
	}
}

func TestSplitImageReference(t *testing.T) {
	tests := []struct {
		in, repo, tag string
		ok            bool
	}{
		{"ghcr.io/projectbluefin/dakota-gaming:testing", "ghcr.io/projectbluefin/dakota-gaming", "testing", true},
		{"ghcr.io/ublue-os/bluefin:stable", "ghcr.io/ublue-os/bluefin", "stable", true},
		{"registry.example:5000/os/image:lts", "registry.example:5000/os/image", "lts", true},
		{"ghcr.io/ublue-os/bluefin@sha256:abc", "", "", false},
		{"ghcr.io/ublue-os/bluefin", "", "", false},
	}
	for _, tt := range tests {
		repo, tag, ok := splitImageReference(tt.in)
		if repo != tt.repo || tag != tt.tag || ok != tt.ok {
			t.Errorf("splitImageReference(%q) = %q, %q, %v; want %q, %q, %v", tt.in, repo, tag, ok, tt.repo, tt.tag, tt.ok)
		}
	}
}

func TestRegistryCheckComparesAgainstBootedAndStaged(t *testing.T) {
	status, err := readComposefsStatus(dakotaHost())
	if err != nil {
		t.Fatal(err)
	}
	created := time.Date(2026, 9, 26, 19, 12, 12, 0, time.UTC)
	for _, tt := range []struct {
		name   string
		digest string
		want   AvailableUpdate
	}{
		{"registry is the booted image", bootedDigest, AvailableUpdate{}},
		{"registry is the image already staged", stagedDigest, AvailableUpdate{}},
		{"registry has a newer image", "sha256:5576db3cf4c39633a37038bca86b5cb3b962ee92c1fe8cf7bb09176621736b7d",
			AvailableUpdate{Available: true, Version: "20260926", Digest: "sha256:5576db3cf4c39633a37038bca86b5cb3b962ee92c1fe8cf7bb09176621736b7d"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var asked string
			resolve := func(_ context.Context, repo, tag string) (registrytags.Tag, error) {
				asked = repo + ":" + tag
				return registrytags.Tag{Name: tag, Digest: tt.digest, Created: created}, nil
			}
			got, err := checkFromRegistry(context.Background(), status, resolve)
			if err != nil {
				t.Fatalf("checkFromRegistry: %v", err)
			}
			if asked != "ghcr.io/projectbluefin/dakota-gaming:testing" {
				t.Errorf("resolved %q, want the booted image's own tag", asked)
			}
			if got != tt.want {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestRegistryCheckReportsRegistryFailure(t *testing.T) {
	status, err := readComposefsStatus(dakotaHost())
	if err != nil {
		t.Fatal(err)
	}
	resolve := func(context.Context, string, string) (registrytags.Tag, error) {
		return registrytags.Tag{}, errors.New("dial tcp: no route to host")
	}
	_, err = checkFromRegistry(context.Background(), status, resolve)
	if err == nil || !strings.Contains(err.Error(), "no route to host") {
		t.Fatalf("err = %v, want the registry failure surfaced", err)
	}
}

func TestRegistryCheckUsesThisPlatformsManifestFromAnIndex(t *testing.T) {
	status, err := readComposefsStatus(dakotaHost())
	if err != nil {
		t.Fatal(err)
	}
	other := "arm64"
	if runtime.GOARCH == "arm64" {
		other = "amd64"
	}
	resolve := func(context.Context, string, string) (registrytags.Tag, error) {
		return registrytags.Tag{
			Digest: "sha256:index",
			Platforms: map[string]string{
				"linux/" + runtime.GOARCH: bootedDigest,
				"linux/" + other:          "sha256:otherarch",
			},
		}, nil
	}
	got, err := checkFromRegistry(context.Background(), status, resolve)
	if err != nil {
		t.Fatalf("checkFromRegistry: %v", err)
	}
	if got.Available {
		t.Fatalf("got %+v: an index whose %s child is the booted image is not an update", got, runtime.GOARCH)
	}
}

func TestComposefsBootedReadsTheKernelCommandLine(t *testing.T) {
	withHostRoot(t, dakotaHost())
	if !ComposefsBooted() {
		t.Error("ComposefsBooted() = false on a composefs host")
	}
	withHostRoot(t, fstest.MapFS{"proc/cmdline": {Data: []byte("root=UUID=1 ostree=/ostree/boot.1/x\n")}})
	if ComposefsBooted() {
		t.Error("ComposefsBooted() = true on an ostree host")
	}
}
