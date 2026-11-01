package dockerbrew

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
)

// tree lists every path under dir with its mode, so a test sees anything an
// extraction wrote, not only what it expected.
func tree(t *testing.T, dir string) map[string]os.FileMode {
	t.Helper()
	out := map[string]os.FileMode{}
	err := filepath.Walk(dir, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if p != dir {
			rel, _ := filepath.Rel(dir, p)
			out[rel] = fi.Mode()
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func keys(m map[string]os.FileMode) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func bottle(name string) Bottle {
	return Bottle{Name: name, Version: fixtureVersions[name]}
}

func TestExtractInstallsOnlyTheMappedFiles(t *testing.T) {
	for _, tc := range []struct {
		name string
		want map[string]os.FileMode
	}{
		{"docker-engine", map[string]os.FileMode{"bin": os.ModeDir | 0o755, "bin/dockerd": 0o755, "bin/docker-proxy": 0o755}},
		{"containerd", map[string]os.FileMode{"bin": os.ModeDir | 0o755, "bin/containerd": 0o755, "bin/containerd-shim-runc-v2": 0o755}},
		{"runc", map[string]os.FileMode{"bin": os.ModeDir | 0o755, "bin/runc": 0o755}},
		{"tini", map[string]os.FileMode{"bin": os.ModeDir | 0o755, "bin/docker-init": 0o755}},
		{"libpathrs", map[string]os.FileMode{"lib": os.ModeDir | 0o755, "lib/libpathrs.so.0": 0o644}},
		{"libseccomp", map[string]os.FileMode{"lib": os.ModeDir | 0o755, "lib/libseccomp.so.2": 0o644}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dest := t.TempDir()
			b := bottle(tc.name)
			if err := Extract(bytes.NewReader(bottleTgz(t, tc.name, b.Dir(), "x86_64_linux", "")), b, dest); err != nil {
				t.Fatal(err)
			}
			got := tree(t, dest)
			if !slices.Equal(keys(got), keys(tc.want)) {
				t.Fatalf("extracted %v, want %v", keys(got), keys(tc.want))
			}
			for p, mode := range tc.want {
				if got[p] != mode {
					t.Errorf("%s mode = %v, want %v", p, got[p], mode)
				}
			}
		})
	}
}

func TestExtractInstallsTiniStaticAsDockerInit(t *testing.T) {
	dest := t.TempDir()
	b := bottle("tini")
	if err := Extract(bytes.NewReader(bottleTgz(t, "tini", b.Dir(), "x86_64_linux", "")), b, dest); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dest, "bin/docker-init"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(data, []byte("tini-static")) {
		t.Error("docker-init is not tini-static")
	}
}

func TestExtractResolvesTheVersionedLibraryBehindItsSoname(t *testing.T) {
	dest := t.TempDir()
	b := bottle("libpathrs")
	if err := Extract(bytes.NewReader(bottleTgz(t, "libpathrs", b.Dir(), "x86_64_linux", "")), b, dest); err != nil {
		t.Fatal(err)
	}
	if got := readELFFacts(t, filepath.Join(dest, "lib/libpathrs.so.0")).soname; !slices.Equal(got, []string{"libpathrs.so.0"}) {
		t.Errorf("soname = %v", got)
	}
}

func TestExtractRefusesLibraryAmbiguityOrWrongSoname(t *testing.T) {
	p := "libseccomp/2.6.1/lib/"
	lib := func(soname string) []byte {
		return buildELF(t, elfSpec{rpath: brewRpath, soname: soname, needed: []string{"libc.so.6"}})
	}
	for name, tgz := range map[string][]byte{
		"wrong soname":   buildTgz(t, reg(p+"libseccomp.so.2.6.1", lib("libseccomp.so.3"))),
		"no soname":      buildTgz(t, reg(p+"libseccomp.so.2.6.1", buildELF(t, elfSpec{needed: []string{"libc.so.6"}}))),
		"only a symlink": buildTgz(t, symlink(p+"libseccomp.so.2", "/usr/lib64/libseccomp.so.2")),
		"not an ELF":     buildTgz(t, reg(p+"libseccomp.so.2.6.1", []byte("text"))),
	} {
		t.Run(name, func(t *testing.T) {
			if err := Extract(bytes.NewReader(tgz), bottle("libseccomp"), t.TempDir()); err == nil {
				t.Fatal("accepted")
			}
		})
	}
	// Two candidates are an ambiguity in the bottle, not a filesystem clash.
	two := buildTgz(t, reg(p+"libseccomp.so.2.6.1", lib("libseccomp.so.2")), reg(p+"libseccomp.so.2.7", lib("libseccomp.so.2")))
	if err := Extract(bytes.NewReader(two), bottle("libseccomp"), t.TempDir()); err == nil || !strings.Contains(err.Error(), "more than one") {
		t.Fatalf("err = %v, want an ambiguity refusal", err)
	}
}

func TestExtractRejectsEscapesAndSpecialFiles(t *testing.T) {
	good := reg("runc/1.5.2/sbin/runc", []byte("runc"))
	for name, bad := range map[string]tarEntry{
		"parent traversal":   reg("../../etc/cron.d/x", []byte("x")),
		"nested traversal":   reg("runc/1.5.2/../../../x", []byte("x")),
		"absolute path":      reg("/etc/passwd", []byte("x")),
		"character device":   {name: "runc/1.5.2/sbin/null", typ: tar.TypeChar},
		"block device":       {name: "runc/1.5.2/sbin/sda", typ: tar.TypeBlock},
		"fifo":               {name: "runc/1.5.2/sbin/fifo", typ: tar.TypeFifo},
		"symlink traversal":  symlink("../../x", "/etc"),
		"hard link absolute": {name: "/runc", typ: tar.TypeLink, linkname: "runc/1.5.2/sbin/runc"},
	} {
		t.Run(name, func(t *testing.T) {
			parent := t.TempDir()
			dest := filepath.Join(parent, "stage")
			if err := Extract(bytes.NewReader(buildTgz(t, good, bad)), bottle("runc"), dest); err == nil {
				t.Fatal("accepted")
			}
			for p := range tree(t, parent) {
				if p != "stage" && !strings.HasPrefix(p, "stage/") {
					t.Errorf("extraction wrote %s outside dest", p)
				}
			}
		})
	}
}

func TestExtractNeverFollowsOrCreatesSymlinks(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("precious"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("mapped member is a symlink", func(t *testing.T) {
		dest := t.TempDir()
		tgz := buildTgz(t, symlink("runc/1.5.2/sbin/runc", outside))
		if err := Extract(bytes.NewReader(tgz), bottle("runc"), dest); err == nil {
			t.Fatal("a symlinked member satisfied the map")
		}
		if len(tree(t, dest)) != 0 {
			t.Errorf("wrote %v", keys(tree(t, dest)))
		}
	})
	t.Run("mapped member is a hard link", func(t *testing.T) {
		tgz := buildTgz(t, tarEntry{name: "runc/1.5.2/sbin/runc", typ: tar.TypeLink, linkname: "runc/1.5.2/README.md"})
		if err := Extract(bytes.NewReader(tgz), bottle("runc"), t.TempDir()); err == nil {
			t.Fatal("a hard-linked member satisfied the map")
		}
	})
	t.Run("destination already holds a symlink", func(t *testing.T) {
		dest := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dest, "bin"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(dest, "bin/runc")); err != nil {
			t.Fatal(err)
		}
		tgz := buildTgz(t, reg("runc/1.5.2/sbin/runc", []byte("payload")))
		if err := Extract(bytes.NewReader(tgz), bottle("runc"), dest); err == nil {
			t.Fatal("wrote through an existing symlink")
		}
		if data, _ := os.ReadFile(outside); string(data) != "precious" {
			t.Error("symlink target was overwritten")
		}
	})
}

func TestExtractRequiresEveryMappedMemberOnce(t *testing.T) {
	b := bottle("containerd")
	only := buildTgz(t, reg("containerd/2.4.2/bin/containerd", []byte("c")))
	if err := Extract(bytes.NewReader(only), b, t.TempDir()); err == nil {
		t.Fatal("accepted a bottle missing the shim")
	}
	dup := buildTgz(t,
		reg("containerd/2.4.2/bin/containerd", []byte("c")),
		reg("containerd/2.4.2/bin/containerd", []byte("c2")),
		reg("containerd/2.4.2/bin/containerd-shim-runc-v2", []byte("s")))
	if err := Extract(bytes.NewReader(dup), b, t.TempDir()); err == nil {
		t.Fatal("accepted a duplicated member")
	}
	if err := Extract(bytes.NewReader(only), Bottle{Name: "ctr", Version: "1"}, t.TempDir()); err == nil {
		t.Fatal("accepted a formula outside the set")
	}
	if err := Extract(bytes.NewReader([]byte("not gzip")), b, t.TempDir()); err == nil {
		t.Fatal("accepted a non-gzip body")
	}
}

func TestExtractUsesTheRevisionDirectory(t *testing.T) {
	b := Bottle{Name: "runc", Version: "1.5.2", Revision: 1}
	plain := buildTgz(t, reg("runc/1.5.2/sbin/runc", []byte("r")))
	if err := Extract(bytes.NewReader(plain), b, t.TempDir()); err == nil {
		t.Fatal("took a member from the unrevised directory")
	}
	revised := buildTgz(t, reg("runc/1.5.2_1/sbin/runc", []byte("r")))
	if err := Extract(bytes.NewReader(revised), b, t.TempDir()); err != nil {
		t.Fatal(err)
	}
}
