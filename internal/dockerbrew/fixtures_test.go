package dockerbrew

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"debug/elf"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

var (
	testKeyOnce sync.Once
	testKey     *rsa.PrivateKey
)

func signingKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	testKeyOnce.Do(func() {
		k, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			panic(err)
		}
		testKey = k
	})
	return testKey
}

const ps512Header = `{"alg":"PS512","b64":false,"crit":["b64"]}`

type jwsOpts struct {
	header string // protected header JSON; default ps512Header
	kid    string // default homebrew-1
	pad    bool   // pad the signature with '=' like Homebrew
}

// signJWS builds a JWS document the way Homebrew publishes the index.
func signJWS(t *testing.T, key *rsa.PrivateKey, payload string, o jwsOpts) []byte {
	t.Helper()
	if o.header == "" {
		o.header = ps512Header
	}
	if o.kid == "" {
		o.kid = homebrewKeyID
	}
	protected := base64.RawURLEncoding.EncodeToString([]byte(o.header))
	digest := sha512.Sum512([]byte(protected + "." + payload))
	sig, err := rsa.SignPSS(rand.Reader, key, crypto.SHA512, digest[:], &rsa.PSSOptions{SaltLength: rsa.PSSSaltLengthEqualsHash})
	if err != nil {
		t.Fatal(err)
	}
	enc := base64.RawURLEncoding.EncodeToString(sig)
	if o.pad {
		enc = base64.URLEncoding.EncodeToString(sig)
	}
	doc := map[string]any{
		"payload": payload,
		"signatures": []any{map[string]any{
			"header":    map[string]string{"kid": o.kid},
			"protected": protected,
			"signature": enc,
		}},
	}
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// useTestKey makes VerifyIndex trust the test key for the rest of the test.
func useTestKey(t *testing.T) {
	t.Helper()
	pub := &signingKey(t).PublicKey
	old := indexKey
	indexKey = func() (*rsa.PublicKey, error) { return pub, nil }
	t.Cleanup(func() { indexKey = old })
}

func sha256Hex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// formula is an index entry as Homebrew's API publishes it.
type formula map[string]any

func newFormula(name, version string, revision int, deps []string, shas map[string]string) formula {
	files := map[string]any{}
	for arch, sha := range shas {
		files[arch] = map[string]any{"cellar": ":any", "url": bottleURL(name, sha), "sha256": sha}
	}
	if deps == nil {
		deps = []string{}
	}
	return formula{
		"name":         name,
		"versions":     map[string]any{"stable": version, "head": nil, "bottle": true},
		"revision":     revision,
		"dependencies": deps,
		"bottle":       map[string]any{"stable": map[string]any{"rebuild": 0, "files": files}},
	}
}

var fixtureVersions = map[string]string{
	"docker-engine": "29.9.2", "containerd": "2.4.2", "runc": "1.5.2",
	"tini": "0.19.0", "libpathrs": "0.2.6", "libseccomp": "2.6.1",
}

var fixtureDeps = map[string][]string{
	"docker-engine": {"containerd", "nftables", "runc", "tini"},
	"runc":          {"libpathrs", "libseccomp"},
}

// fakeSHA is a deterministic stand-in digest per formula and arch.
func fakeSHA(name, arch string) string { return sha256Hex([]byte(name + "/" + arch)) }

// indexFormulae is today's shape of the six formulae plus nftables and an
// unrelated formula, with placeholder digests.
func indexFormulae() []formula {
	var out []formula
	for _, name := range Formulae() {
		out = append(out, newFormula(name, fixtureVersions[name], 0, fixtureDeps[name], map[string]string{
			"x86_64_linux": fakeSHA(name, "x86_64_linux"),
			"arm64_linux":  fakeSHA(name, "arm64_linux"),
		}))
	}
	out = append(out,
		newFormula("nftables", "1.1.7", 0, []string{"gmp", "jansson", "libmnl"}, map[string]string{"x86_64_linux": fakeSHA("nftables", "x")}),
		newFormula("a2ps", "4.15.8", 0, nil, nil))
	return out
}

func find(fs []formula, name string) formula {
	for _, f := range fs {
		if f["name"] == name {
			return f
		}
	}
	panic(name)
}

func marshalPayload(t *testing.T, fs []formula) []byte {
	t.Helper()
	data, err := json.Marshal(fs)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// tarEntry is one member of a synthetic bottle.
type tarEntry struct {
	name     string
	typ      byte
	body     []byte
	linkname string
}

func reg(name string, body []byte) tarEntry {
	return tarEntry{name: name, typ: tar.TypeReg, body: body}
}
func symlink(name, target string) tarEntry {
	return tarEntry{name: name, typ: tar.TypeSymlink, linkname: target}
}

func buildTgz(t *testing.T, entries ...tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		hdr := &tar.Header{Name: e.name, Typeflag: e.typ, Linkname: e.linkname, Mode: 0o555, Size: int64(len(e.body))}
		if e.typ != tar.TypeReg {
			hdr.Size = 0
		}
		if e.typ == tar.TypeChar || e.typ == tar.TypeBlock {
			hdr.Devmajor, hdr.Devminor = 1, 3
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if e.typ == tar.TypeReg {
			if _, err := tw.Write(e.body); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// bottleTgz builds a bottle shaped like Homebrew's for name at dir, with
// the same symlinks and extra files the real bottles carry.
func bottleTgz(t *testing.T, name, dir, arch string, tag string) []byte {
	t.Helper()
	p := name + "/" + dir + "/"
	machine := machineFor(arch)
	dyn := func(needed ...string) []byte {
		return buildELF(t, elfSpec{machine: machine, interp: brewInterp, rpath: brewRpath, needed: needed})
	}
	static := func(label string) []byte {
		// Static executables carry no loader metadata; the label makes each
		// file distinguishable.
		return append(buildELF(t, elfSpec{machine: machine}), []byte(label+tag)...)
	}
	switch name {
	case "docker-engine":
		return buildTgz(t,
			tarEntry{name: p, typ: tar.TypeDir},
			reg(p+"bin/dockerd", append(dyn("libc.so.6"), tag...)),
			reg(p+"bin/docker-proxy", append(dyn("libc.so.6"), tag...)),
			reg(p+"bin/dockerd-rootless.sh", []byte("#!/bin/sh\n")),
			reg(p+"sh.brew.docker-engine.service", []byte("[Unit]\n")))
	case "containerd":
		return buildTgz(t,
			reg(p+"bin/containerd", static("containerd")),
			reg(p+"bin/containerd-shim-runc-v2", static("shim")),
			reg(p+"bin/ctr", static("ctr")),
			reg(p+"bin/containerd-stress", static("stress")))
	case "runc":
		return buildTgz(t,
			reg(p+"sbin/runc", append(dyn("libseccomp.so.2", "libpathrs.so.0", "libc.so.6"), tag...)),
			reg(p+"share/man/man8/runc.8", []byte("man")))
	case "tini":
		return buildTgz(t,
			reg(p+"bin/tini", static("tini")),
			reg(p+"bin/tini-static", static("tini-static")),
			symlink(p+"bin/docker-init", "tini-static"))
	case "libpathrs":
		lib := append(buildELF(t, elfSpec{machine: machine, rpath: brewRpath, soname: "libpathrs.so.0", needed: []string{"libgcc_s.so.1", "libc.so.6"}}), tag...)
		return buildTgz(t,
			reg(p+"lib/libpathrs.so.0.2.5+dev", lib),
			symlink(p+"lib/libpathrs.so.0", "libpathrs.so.0.2.5+dev"),
			symlink(p+"lib/libpathrs.so", "libpathrs.so.0.2.5+dev"),
			reg(p+"lib/libpathrs.a", []byte("!<arch>\n")),
			reg(p+"include/pathrs.h", []byte("/* */")))
	case "libseccomp":
		lib := append(buildELF(t, elfSpec{machine: machine, rpath: brewRpath, soname: "libseccomp.so.2", needed: []string{"libc.so.6"}}), tag...)
		return buildTgz(t,
			reg(p+"lib/libseccomp.so.2.6.1", lib),
			symlink(p+"lib/libseccomp.so.2", "libseccomp.so.2.6.1"),
			symlink(p+"lib/libseccomp.so", "libseccomp.so.2.6.1"))
	}
	t.Fatalf("no fixture bottle for %s", name)
	return nil
}

// registry is a loopback stand-in for formulae.brew.sh and GHCR. Every
// request a Client makes is routed to it, whatever host the URL names.
type registry struct {
	mu       sync.Mutex
	index    []byte
	blobs    map[string][]byte // request path -> served body
	formulae map[string][]byte // name -> per-formula API JSON
	requests []string
	authz    map[string]string // request path -> Authorization header seen
}

func newRegistry(t *testing.T) (*registry, Client) {
	t.Helper()
	r := &registry{blobs: map[string][]byte{}, formulae: map[string][]byte{}, authz: map[string]string{}}
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	target, _ := url.Parse(srv.URL)
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		req = req.Clone(req.Context())
		req.URL.Scheme, req.URL.Host = target.Scheme, target.Host
		return http.DefaultTransport.RoundTrip(req)
	})
	return r, Client{HTTP: &http.Client{Transport: transport}}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func (r *registry) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p := req.URL.Path
	r.requests = append(r.requests, p)
	r.authz[p] = req.Header.Get("Authorization")
	if p == "/api/formula.jws.json" && r.index != nil {
		_, _ = w.Write(r.index)
		return
	}
	if name, ok := strings.CutPrefix(p, "/api/formula/"); ok {
		if body, ok := r.formulae[strings.TrimSuffix(name, ".json")]; ok {
			_, _ = w.Write(body)
			return
		}
	}
	if body, ok := r.blobs[p]; ok {
		_, _ = w.Write(body)
		return
	}
	http.NotFound(w, req)
}

func blobPath(name, sha string) string { return "/v2/homebrew/core/" + name + "/blobs/sha256:" + sha }

// addBlob serves body as name's bottle under its own digest.
func (r *registry) addBlob(name string, body []byte) string {
	sha := sha256Hex(body)
	r.serve(name, sha, body)
	return sha
}

// serve answers the bottle URL for sha with body, matching or not.
func (r *registry) serve(name, sha string, body []byte) {
	r.mu.Lock()
	r.blobs[blobPath(name, sha)] = body
	r.mu.Unlock()
}

func (r *registry) count(prefix string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, p := range r.requests {
		if strings.HasPrefix(p, prefix) {
			n++
		}
	}
	return n
}

func (r *registry) header(path string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.authz[path]
}

func machineFor(arch string) elf.Machine {
	if arch == "arm64_linux" {
		return elf.EM_AARCH64
	}
	return elf.EM_X86_64
}
