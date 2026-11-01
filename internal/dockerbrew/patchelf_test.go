package dockerbrew

import (
	"bytes"
	"debug/elf"
	"os"
	"slices"
	"strings"
	"testing"
)

const (
	brewInterp = "@@HOMEBREW_PREFIX@@/lib/ld.so"
	brewRpath  = "@@HOMEBREW_PREFIX@@/lib:@@HOMEBREW_CELLAR@@/runc/1.5.2/lib"
)

func TestPatchELFRewritesHomebrewLoaderAndRunPaths(t *testing.T) {
	for _, tc := range []struct {
		arch    string
		machine elf.Machine
	}{
		{"x86_64_linux", elf.EM_X86_64},
		{"arm64_linux", elf.EM_AARCH64},
	} {
		t.Run(tc.arch, func(t *testing.T) {
			needed := []string{"libseccomp.so.2", "libpathrs.so.0", "libc.so.6"}
			orig := buildELF(t, elfSpec{
				machine: tc.machine,
				interp:  brewInterp,
				rpath:   brewRpath,
				runpath: "@@HOMEBREW_PREFIX@@/opt/libseccomp/lib",
				needed:  needed,
			})
			p := writeTemp(t, "runc", orig, 0o755)

			changed, err := PatchELF(p, Loader(tc.arch))
			if err != nil || !changed {
				t.Fatalf("PatchELF = %v, %v; want changed", changed, err)
			}
			facts := readELFFacts(t, p)
			if facts.interp != Loader(tc.arch) {
				t.Errorf("interpreter = %q, want %q", facts.interp, Loader(tc.arch))
			}
			if !slices.Equal(facts.rpath, []string{"$ORIGIN/../lib"}) || !slices.Equal(facts.runpath, []string{"$ORIGIN/../lib"}) {
				t.Errorf("rpath = %q, runpath = %q; want $ORIGIN/../lib", facts.rpath, facts.runpath)
			}
			if !slices.Equal(facts.needed, needed) {
				t.Errorf("needed = %q, want unchanged %q", facts.needed, needed)
			}
			data, _ := os.ReadFile(p)
			if len(data) != len(orig) {
				t.Errorf("file size changed from %d to %d", len(orig), len(data))
			}
			if bytes.Contains(data, []byte("@@HOMEBREW")) {
				t.Error("a Homebrew placeholder survived patching")
			}

			changed, err = PatchELF(p, Loader(tc.arch))
			if err != nil || changed {
				t.Fatalf("second PatchELF = %v, %v; want unchanged", changed, err)
			}
		})
	}
}

func TestPatchELFLeavesStaticFileUntouched(t *testing.T) {
	orig := buildELF(t, elfSpec{})
	p := writeTemp(t, "containerd", orig, 0o755)
	changed, err := PatchELF(p, Loader("x86_64_linux"))
	if err != nil || changed {
		t.Fatalf("PatchELF = %v, %v; want no-op", changed, err)
	}
	if data, _ := os.ReadFile(p); !bytes.Equal(data, orig) {
		t.Error("static file was modified")
	}
}

func TestPatchELFRefusesWhatItCannotMakeSafe(t *testing.T) {
	for _, tc := range []struct {
		name   string
		spec   elfSpec
		interp string
		want   string
	}{
		{
			name:   "loader longer than the slot",
			spec:   elfSpec{interp: "@@HOMEBREW/ld", needed: []string{"libc.so.6"}},
			interp: Loader("x86_64_linux"),
			want:   "does not fit",
		},
		{
			name:   "interpreter in the Homebrew prefix",
			spec:   elfSpec{interp: "/home/linuxbrew/.linuxbrew/lib/ld.so", needed: []string{"libc.so.6"}},
			interp: Loader("x86_64_linux"),
			want:   "unexpected interpreter",
		},
		{
			name:   "run path shorter than $ORIGIN/../lib",
			spec:   elfSpec{rpath: "/opt/lib", needed: []string{"libc.so.6"}},
			interp: Loader("x86_64_linux"),
			want:   "shorter",
		},
		{
			name:   "needed library named by path",
			spec:   elfSpec{interp: brewInterp, needed: []string{"/home/linuxbrew/.linuxbrew/lib/libc.so.6"}},
			interp: Loader("x86_64_linux"),
			want:   "bare library name",
		},
		{
			name:   "run path storage shared with another name",
			spec:   elfSpec{rpath: brewRpath, needed: []string{"libc.so.6"}, sonameInRpathTail: 3},
			interp: Loader("x86_64_linux"),
			want:   "shares storage",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			orig := buildELF(t, tc.spec)
			p := writeTemp(t, "bin", orig, 0o755)
			_, err := PatchELF(p, tc.interp)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("PatchELF err = %v, want %q", err, tc.want)
			}
			if data, _ := os.ReadFile(p); !bytes.Equal(data, orig) {
				t.Error("a refused file was modified")
			}
		})
	}
}

func TestPatchELFRejectsNonELF64LittleEndian(t *testing.T) {
	big := buildELF(t, elfSpec{interp: brewInterp})
	big[elf.EI_DATA] = byte(elf.ELFDATA2MSB)
	elf32 := buildELF(t, elfSpec{interp: brewInterp})
	elf32[elf.EI_CLASS] = byte(elf.ELFCLASS32)
	for name, data := range map[string][]byte{
		"text":       []byte("#!/bin/sh\necho hi\n"),
		"big-endian": big,
		"elf32":      elf32,
	} {
		t.Run(name, func(t *testing.T) {
			p := writeTemp(t, "f", data, 0o755)
			if _, err := PatchELF(p, Loader("x86_64_linux")); err == nil {
				t.Fatal("PatchELF accepted a file it cannot patch")
			}
			if got, _ := os.ReadFile(p); !bytes.Equal(got, data) {
				t.Error("rejected file was modified")
			}
		})
	}
}

func TestPatchELFDoesNotFollowSymlinks(t *testing.T) {
	target := writeTemp(t, "real", buildELF(t, elfSpec{interp: brewInterp}), 0o755)
	link := target + "-link"
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := PatchELF(link, Loader("x86_64_linux")); err == nil {
		t.Fatal("PatchELF followed a symlink")
	}
	if readELFFacts(t, target).interp != brewInterp {
		t.Error("symlink target was patched")
	}
}

func TestLoadersFitHomebrewInterpreterSlot(t *testing.T) {
	for _, arch := range []string{"x86_64_linux", "arm64_linux"} {
		if l := Loader(arch); l == "" || len(l)+1 > len(brewInterp)+1 {
			t.Errorf("Loader(%s) = %q does not fit %q", arch, l, brewInterp)
		}
	}
	if Loader("x86_64_darwin") != "" {
		t.Error("Loader returned a loader for an unsupported arch")
	}
}
