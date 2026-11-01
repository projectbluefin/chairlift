package dockerbrew

import (
	"slices"
	"testing"
)

func TestArchMapsSupportedGOARCH(t *testing.T) {
	for goarch, want := range map[string]string{"amd64": "x86_64_linux", "arm64": "arm64_linux"} {
		if got, err := archFor(goarch); err != nil || got != want {
			t.Errorf("archFor(%s) = %q, %v", goarch, got, err)
		}
	}
	for _, goarch := range []string{"386", "riscv64", "ppc64le"} {
		if _, err := archFor(goarch); err == nil {
			t.Errorf("archFor(%s) succeeded", goarch)
		}
	}
}

func TestFormulaSetsAreDisjoint(t *testing.T) {
	for _, cli := range CLIFormulae() {
		if slices.Contains(Formulae(), cli) {
			t.Errorf("%s is in both the daemon and the CLI set", cli)
		}
	}
	// Every daemon formula has an extraction rule.
	for _, name := range Formulae() {
		_, bin := bottleFiles[name]
		_, lib := bottleLibs[name]
		if bin == lib {
			t.Errorf("%s: binary rule %v, library rule %v; want exactly one", name, bin, lib)
		}
	}
	if len(bottleFiles)+len(bottleLibs) != len(Formulae()) {
		t.Error("extraction rules name a formula outside the set")
	}
}

func TestSetIDIdentifiesTheBottleDigests(t *testing.T) {
	a := Manifest{Arch: "x86_64_linux", Bottles: []Bottle{{Name: "runc", SHA256: fakeSHA("runc", "1")}, {Name: "tini", SHA256: fakeSHA("tini", "1")}}}
	reordered := Manifest{Arch: a.Arch, Bottles: []Bottle{a.Bottles[1], a.Bottles[0]}}
	changed := Manifest{Arch: a.Arch, Bottles: []Bottle{a.Bottles[0], {Name: "tini", SHA256: fakeSHA("tini", "2")}}}

	if !setIDPattern.MatchString(a.SetID()) {
		t.Fatalf("SetID %q is not a set directory name", a.SetID())
	}
	if a.SetID() != reordered.SetID() {
		t.Error("SetID depends on bottle order")
	}
	if a.SetID() == changed.SetID() {
		t.Error("SetID ignores a changed digest")
	}
}

func TestOutdatedComparesDigestsPerFormula(t *testing.T) {
	installed := Manifest{Arch: "x86_64_linux", Bottles: []Bottle{{Name: "runc", SHA256: "a"}, {Name: "tini", SHA256: "b"}}}
	for name, tc := range map[string]struct {
		latest []Bottle
		want   bool
	}{
		"same":         {[]Bottle{{Name: "runc", SHA256: "a"}, {Name: "tini", SHA256: "b"}}, false},
		"new digest":   {[]Bottle{{Name: "runc", SHA256: "a"}, {Name: "tini", SHA256: "c"}}, true},
		"new formula":  {[]Bottle{{Name: "runc", SHA256: "a"}, {Name: "tini", SHA256: "b"}, {Name: "libseccomp", SHA256: "d"}}, true},
		"no knowledge": {nil, false},
	} {
		if got := Outdated(installed, tc.latest); got != tc.want {
			t.Errorf("%s: Outdated = %v, want %v", name, got, tc.want)
		}
	}
}
