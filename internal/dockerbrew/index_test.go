package dockerbrew

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

const samplePayload = `[{"name":"runc"}]`

func TestVerifyIndexAcceptsHomebrewsSignatureShape(t *testing.T) {
	key := signingKey(t)
	for _, pad := range []bool{true, false} {
		doc := signJWS(t, key, samplePayload, jwsOpts{pad: pad})
		got, err := verifyIndexWith(doc, &key.PublicKey)
		if err != nil {
			t.Fatalf("pad=%v: %v", pad, err)
		}
		if string(got) != samplePayload {
			t.Fatalf("pad=%v: payload = %q", pad, got)
		}
	}
}

func TestVerifyIndexRejectsTamperedDocument(t *testing.T) {
	key := signingKey(t)
	doc := signJWS(t, key, samplePayload, jwsOpts{pad: true})

	var parsed map[string]any
	if err := json.Unmarshal(doc, &parsed); err != nil {
		t.Fatal(err)
	}
	parsed["payload"] = `[{"name":"runc","evil":true}]`
	tampered, _ := json.Marshal(parsed)
	if _, err := verifyIndexWith(tampered, &key.PublicKey); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("tampered payload: err = %v, want ErrBadSignature", err)
	}

	other, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verifyIndexWith(signJWS(t, other, samplePayload, jwsOpts{}), &key.PublicKey); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("foreign key: err = %v, want ErrBadSignature", err)
	}
}

func TestVerifyIndexRejectsUnexpectedProtectedHeader(t *testing.T) {
	key := signingKey(t)
	for name, o := range map[string]jwsOpts{
		"RS512":           {header: `{"alg":"RS512","b64":false,"crit":["b64"]}`},
		"none":            {header: `{"alg":"none","b64":false,"crit":["b64"]}`},
		"encoded payload": {header: `{"alg":"PS512","b64":true,"crit":["b64"]}`},
		"b64 missing":     {header: `{"alg":"PS512"}`},
		"b64 not crit":    {header: `{"alg":"PS512","b64":false}`},
		"other key id":    {kid: "homebrew-2"},
	} {
		t.Run(name, func(t *testing.T) {
			// Each document is validly signed over its own header, so only
			// the header policy can reject it.
			if _, err := verifyIndexWith(signJWS(t, key, samplePayload, o), &key.PublicKey); err == nil {
				t.Fatal("accepted")
			}
		})
	}
}

func TestVerifyIndexRejectsMalformedDocuments(t *testing.T) {
	key := signingKey(t)
	good := signJWS(t, key, samplePayload, jwsOpts{})
	var parsed map[string]any
	if err := json.Unmarshal(good, &parsed); err != nil {
		t.Fatal(err)
	}
	sig := parsed["signatures"].([]any)[0]
	two, _ := json.Marshal(map[string]any{"payload": samplePayload, "signatures": []any{sig, sig}})
	for name, doc := range map[string][]byte{
		"not json":       []byte("<html>"),
		"no payload":     []byte(`{"signatures":[]}`),
		"no signature":   []byte(`{"payload":"[]","signatures":[]}`),
		"two signatures": two,
	} {
		if _, err := verifyIndexWith(doc, &key.PublicKey); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestEmbeddedKeyIsHomebrew1(t *testing.T) {
	sum := sha256.Sum256(homebrewKeyPEM)
	if got := hex.EncodeToString(sum[:]); got != "ef2d2c9e0219d485df9f07fff7b037feadc36c93085be9ffefb1390f31a3de1d" {
		t.Fatalf("embedded key sha256 = %s", got)
	}
	pub, err := indexKey()
	if err != nil {
		t.Fatal(err)
	}
	if pub.N.BitLen() != 4096 {
		t.Errorf("embedded key is %d bits, want 4096", pub.N.BitLen())
	}
	// VerifyIndex uses the embedded key: a document signed by any other key
	// fails even though it is well formed.
	if _, err := VerifyIndex(signJWS(t, signingKey(t), samplePayload, jwsOpts{pad: true})); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("VerifyIndex with a foreign signature: err = %v", err)
	}
}

func TestSelectBottlesPicksTheFixedSetPerArch(t *testing.T) {
	payload := marshalPayload(t, indexFormulae())
	for _, arch := range []string{"x86_64_linux", "arm64_linux"} {
		got, err := SelectBottles(payload, arch)
		if err != nil {
			t.Fatalf("%s: %v", arch, err)
		}
		if len(got) != len(Formulae()) {
			t.Fatalf("%s: %d bottles", arch, len(got))
		}
		for i, b := range got {
			name := Formulae()[i]
			want := Bottle{Name: name, Version: fixtureVersions[name], SHA256: fakeSHA(name, arch), URL: bottleURL(name, fakeSHA(name, arch))}
			if b != want {
				t.Errorf("%s: bottle %d = %+v, want %+v", arch, i, b, want)
			}
		}
	}
	if _, err := SelectBottles(payload, "x86_64_darwin"); err == nil {
		t.Error("accepted an unsupported arch")
	}
}

func TestSelectBottlesRejectsDependencyDrift(t *testing.T) {
	for name, mutate := range map[string]func([]formula) []formula{
		"engine gains a dependency": func(fs []formula) []formula {
			find(fs, "docker-engine")["dependencies"] = []string{"containerd", "nftables", "runc", "tini", "a2ps"}
			return fs
		},
		"library gains a dependency": func(fs []formula) []formula {
			find(fs, "libseccomp")["dependencies"] = []string{"gperf"}
			return append(fs, newFormula("gperf", "3.1", 0, nil, map[string]string{"x86_64_linux": fakeSHA("gperf", "x")}))
		},
		"engine drops nftables": func(fs []formula) []formula {
			find(fs, "docker-engine")["dependencies"] = []string{"containerd", "runc", "tini"}
			return fs
		},
		"runc drops libpathrs": func(fs []formula) []formula {
			find(fs, "runc")["dependencies"] = []string{"libseccomp"}
			return fs
		},
		"linux variation adds a dependency": func(fs []formula) []formula {
			find(fs, "runc")["variations"] = map[string]any{"x86_64_linux": map[string]any{"dependencies": []string{"libpathrs", "libseccomp", "a2ps"}}}
			return fs
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := SelectBottles(marshalPayload(t, mutate(indexFormulae())), "x86_64_linux")
			if err == nil {
				t.Fatal("accepted a drifted dependency closure")
			}
		})
	}
}

func TestSelectBottlesHonorsLinuxDependencyVariations(t *testing.T) {
	fs := indexFormulae()
	// A macOS-only dependency in the base list that the Linux variation
	// removes must not count against the closure, and nftables' own
	// dependencies never join it.
	find(fs, "containerd")["dependencies"] = []string{"a2ps"}
	find(fs, "containerd")["variations"] = map[string]any{
		"x86_64_linux": map[string]any{"dependencies": []string{}},
		"arm64_linux":  map[string]any{"dependencies": []string{}},
	}
	find(fs, "tini")["variations"] = map[string]any{"x86_64_linux": nil}
	if _, err := SelectBottles(marshalPayload(t, fs), "x86_64_linux"); err != nil {
		t.Fatal(err)
	}
}

func TestSelectBottlesRejectsUntrustedBottleMetadata(t *testing.T) {
	sha := fakeSHA("runc", "x86_64_linux")
	for name, mutate := range map[string]func(formula){
		"foreign host": func(f formula) {
			setFile(f, "x86_64_linux", "https://example.com/v2/homebrew/core/runc/blobs/sha256:"+sha, sha)
		},
		"url for another digest": func(f formula) {
			setFile(f, "x86_64_linux", bottleURL("runc", fakeSHA("runc", "arm64_linux")), sha)
		},
		"url for another formula": func(f formula) {
			setFile(f, "x86_64_linux", bottleURL("tini", sha), sha)
		},
		"malformed digest": func(f formula) {
			setFile(f, "x86_64_linux", bottleURL("runc", "ABC"), "ABC")
		},
		"no bottle for arch": func(f formula) {
			f["bottle"] = map[string]any{"stable": map[string]any{"files": map[string]any{}}}
		},
		"no stable bottle": func(f formula) { f["bottle"] = map[string]any{} },
		"traversing version": func(f formula) {
			f["versions"] = map[string]any{"stable": "../../etc"}
		},
		"negative revision": func(f formula) { f["revision"] = -1 },
	} {
		t.Run(name, func(t *testing.T) {
			fs := indexFormulae()
			mutate(find(fs, "runc"))
			if _, err := SelectBottles(marshalPayload(t, fs), "x86_64_linux"); err == nil {
				t.Fatal("accepted")
			}
		})
	}
	t.Run("missing formula", func(t *testing.T) {
		var fs []formula
		for _, f := range indexFormulae() {
			if f["name"] != "libseccomp" {
				fs = append(fs, f)
			}
		}
		if _, err := SelectBottles(marshalPayload(t, fs), "x86_64_linux"); err == nil {
			t.Fatal("accepted")
		}
	})
	t.Run("duplicate formula", func(t *testing.T) {
		fs := indexFormulae()
		fs = append(fs, find(fs, "runc"))
		if _, err := SelectBottles(marshalPayload(t, fs), "x86_64_linux"); err == nil {
			t.Fatal("accepted")
		}
	})
}

func setFile(f formula, arch, url, sha string) {
	f["bottle"] = map[string]any{"stable": map[string]any{"files": map[string]any{arch: map[string]any{"url": url, "sha256": sha}}}}
}

func TestBottleDirCarriesRevision(t *testing.T) {
	if got := (Bottle{Version: "1.5.2"}).Dir(); got != "1.5.2" {
		t.Errorf("Dir = %q", got)
	}
	if got := (Bottle{Version: "1.5.2", Revision: 2}).Dir(); got != "1.5.2_2" {
		t.Errorf("Dir = %q", got)
	}
	fs := indexFormulae()
	find(fs, "runc")["revision"] = 1
	bottles, err := SelectBottles(marshalPayload(t, fs), "x86_64_linux")
	if err != nil {
		t.Fatal(err)
	}
	if bottles[2].Name != "runc" || bottles[2].Revision != 1 || bottles[2].Dir() != "1.5.2_1" {
		t.Errorf("runc bottle = %+v", bottles[2])
	}
}

func TestClientIndexVerifiesWhatItFetches(t *testing.T) {
	useTestKey(t)
	reg, c := newRegistry(t)
	payload := string(marshalPayload(t, indexFormulae()))
	reg.index = signJWS(t, signingKey(t), payload, jwsOpts{pad: true})
	got, err := c.Index(t.Context())
	if err != nil || string(got) != payload {
		t.Fatalf("Index = %d bytes, %v", len(got), err)
	}
	if reg.count("/api/formula.jws.json") != 1 {
		t.Errorf("index was not requested at the production URL path")
	}

	reg.index = []byte(strings.Replace(string(reg.index), "29.9.2", "29.9.3", 1))
	if _, err := c.Index(t.Context()); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("tampered index: err = %v", err)
	}
	reg.index = nil
	if _, err := c.Index(t.Context()); err == nil {
		t.Fatal("a 404 index was accepted")
	}
}

func TestClientBottleVerifiesDigest(t *testing.T) {
	reg, c := newRegistry(t)
	body := []byte("bottle bytes")
	sha := reg.addBlob("runc", body)
	b := Bottle{Name: "runc", Version: "1.5.2", SHA256: sha, URL: bottleURL("runc", sha)}

	var out strings.Builder
	if err := c.Bottle(t.Context(), b, &out); err != nil {
		t.Fatal(err)
	}
	if out.String() != string(body) {
		t.Errorf("body = %q", out.String())
	}
	if got := reg.header(blobPath("runc", sha)); got != "Bearer QQ==" {
		t.Errorf("Authorization = %q, want GHCR's anonymous bearer", got)
	}

	reg.serve("runc", sha, []byte("substituted bytes"))
	if err := c.Bottle(t.Context(), b, &strings.Builder{}); err == nil || !strings.Contains(err.Error(), "digest") {
		t.Fatalf("substituted bottle: err = %v", err)
	}

	other := fakeSHA("runc", "none")
	if err := c.Bottle(t.Context(), Bottle{Name: "runc", SHA256: other, URL: bottleURL("runc", other)}, &strings.Builder{}); err == nil {
		t.Fatal("a 404 bottle was accepted")
	}

	before := reg.count("")
	foreign := Bottle{Name: "runc", SHA256: sha, URL: "https://example.com/runc.tgz"}
	if err := c.Bottle(t.Context(), foreign, &strings.Builder{}); err == nil {
		t.Fatal("a foreign bottle URL was fetched")
	}
	if reg.count("") != before {
		t.Error("a refused bottle URL was still requested")
	}
}

func TestClientLatestReadsPerFormulaAPI(t *testing.T) {
	arch, err := Arch()
	if err != nil {
		t.Skip(err)
	}
	reg, c := newRegistry(t)
	for _, f := range indexFormulae() {
		data, _ := json.Marshal(f)
		reg.formulae[f["name"].(string)] = data
	}
	got, err := c.Latest(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(Formulae()) || got[2].SHA256 != fakeSHA("runc", arch) {
		t.Fatalf("Latest = %+v", got)
	}
	for _, name := range Formulae() {
		if reg.count("/api/formula/"+name+".json") != 1 {
			t.Errorf("%s was not read from the per-formula API", name)
		}
	}

	delete(reg.formulae, "libpathrs")
	if _, err := c.Latest(t.Context()); err == nil {
		t.Fatal("Latest succeeded with a formula missing")
	}
}

func TestClientBottleRefusesAnOversizedBody(t *testing.T) {
	reg, c := newRegistry(t)
	body := []byte("bottle bytes")
	sha := reg.addBlob("runc", body)
	b := Bottle{Name: "runc", Version: "1.5.2", SHA256: sha, URL: bottleURL("runc", sha)}
	old := maxBottleBytes
	maxBottleBytes = int64(len(body)) - 1
	t.Cleanup(func() { maxBottleBytes = old })
	var out strings.Builder
	if err := c.Bottle(t.Context(), b, &out); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized bottle: err = %v", err)
	}
	if int64(out.Len()) > maxBottleBytes+1 {
		t.Errorf("read %d bytes past the cap", out.Len())
	}
	maxBottleBytes = int64(len(body))
	if err := c.Bottle(t.Context(), b, &strings.Builder{}); err != nil {
		t.Fatalf("bottle at the cap: %v", err)
	}
}
