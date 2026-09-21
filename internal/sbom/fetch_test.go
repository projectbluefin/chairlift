package sbom

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSplitReference(t *testing.T) {
	tests := []struct {
		ref                             string
		registry, repository, reference string
		wantErr                         bool
	}{
		{ref: "ghcr.io/ublue-os/bluefin:stable", registry: "ghcr.io", repository: "ublue-os/bluefin", reference: "stable"},
		{ref: "ghcr.io/ublue-os/bluefin", registry: "ghcr.io", repository: "ublue-os/bluefin", reference: "latest"},
		{ref: "ghcr.io/ublue-os/bluefin@sha256:abc", registry: "ghcr.io", repository: "ublue-os/bluefin", reference: "sha256:abc"},
		{ref: "registry.example.internal:5000/team/image:v1", registry: "registry.example.internal:5000", repository: "team/image", reference: "v1"},
		{ref: "bluefin", wantErr: true},
	}

	for _, tt := range tests {
		registry, repository, reference, err := SplitReference(tt.ref)
		if tt.wantErr {
			if err == nil {
				t.Errorf("SplitReference(%q) accepted an incomplete reference", tt.ref)
			}
			continue
		}
		if err != nil {
			t.Errorf("SplitReference(%q): %v", tt.ref, err)
			continue
		}
		if registry != tt.registry || repository != tt.repository || reference != tt.reference {
			t.Errorf("SplitReference(%q) = %q, %q, %q", tt.ref, registry, repository, reference)
		}
	}
}

// fakeRegistry serves the shape GHCR actually serves, including the 404 on
// the referrers API that makes the fallback tag load-bearing.
type fakeRegistry struct {
	referrersStatus int
	requested       []string
	sbom            []byte
	// served substitutes different bytes at the blob endpoint while the
	// manifest keeps advertising sbom's digest, modeling a registry that
	// answers a content-addressed request with other content.
	served []byte
}

func (f *fakeRegistry) handler(t *testing.T) http.Handler {
	t.Helper()

	const imageDigest = "sha256:aaaa"
	const sbomManifest = "sha256:bbbb"
	// The blob is fetched by content address and Fetch verifies what
	// arrives, so the fake must advertise the real digest of the bytes the
	// test expects back.
	sum := sha256.Sum256(f.sbom)
	sbomBlob := "sha256:" + hex.EncodeToString(sum[:])

	writeJSON := func(w http.ResponseWriter, value any) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(value); err != nil {
			t.Errorf("encoding fake response: %v", err)
		}
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.requested = append(f.requested, r.URL.Path)

		switch r.URL.Path {
		case "/token":
			writeJSON(w, map[string]string{"token": "test-token"})

		case "/v2/org/image/manifests/stable":
			if r.Header.Get("Authorization") != "Bearer test-token" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Header().Set("Docker-Content-Digest", imageDigest)
			w.WriteHeader(http.StatusOK)

		case "/v2/org/image/referrers/" + imageDigest:
			if f.referrersStatus != http.StatusOK {
				w.WriteHeader(f.referrersStatus)
				return
			}
			writeJSON(w, indexDocument{Manifests: []descriptor{
				{ArtifactType: artifactType, Digest: sbomManifest},
			}})

		// The specification's fallback tag: the digest with ":" replaced.
		case "/v2/org/image/manifests/sha256-aaaa":
			writeJSON(w, indexDocument{Manifests: []descriptor{
				{ArtifactType: "application/vnd.dev.sigstore.bundle.v0.3+json", Digest: "sha256:dddd"},
				{ArtifactType: artifactType, Digest: sbomManifest},
			}})

		case "/v2/org/image/manifests/" + sbomManifest:
			writeJSON(w, indexDocument{Layers: []descriptor{
				{MediaType: "application/vnd.oci.image.layer.v1.tar", Digest: sbomBlob},
			}})

		case "/v2/org/image/blobs/" + sbomBlob:
			payload := f.sbom
			if f.served != nil {
				payload = f.served
			}
			if _, err := w.Write(payload); err != nil {
				t.Errorf("writing fake blob: %v", err)
			}

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

func fetchFromFake(t *testing.T, fake *fakeRegistry) ([]byte, error) {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("testdata", "booted-syft.json"))
	if err != nil {
		t.Fatal(err)
	}
	fake.sbom = data

	server := httptest.NewTLSServer(fake.handler(t))
	t.Cleanup(server.Close)

	client := &RegistryClient{HTTP: server.Client()}
	// The fake serves TLS on a loopback address, so the reference names it
	// as the registry and every request stays on this machine.
	host := strings.TrimPrefix(server.URL, "https://")
	return client.Fetch(context.Background(), host+"/org/image:stable")
}

func TestFetchUsesTheReferrersAPIWhenItWorks(t *testing.T) {
	fake := &fakeRegistry{referrersStatus: http.StatusOK}

	data, err := fetchFromFake(t, fake)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if _, err := Parse(data); err != nil {
		t.Fatalf("fetched bytes do not parse: %v", err)
	}

	for _, path := range fake.requested {
		if path == "/v2/org/image/manifests/sha256-aaaa" {
			t.Error("Fetch used the fallback tag even though referrers worked")
		}
	}
}

// GHCR answers the referrers API with 404 for the images ChairLift manages,
// verified 2026-08-17. Without the fallback the feature silently finds no
// SBOM on the registry that publishes Bluefin.
func TestFetchFallsBackToTheTagSchemeWhenReferrersIs404(t *testing.T) {
	fake := &fakeRegistry{referrersStatus: http.StatusNotFound}

	data, err := fetchFromFake(t, fake)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	packages, err := Parse(data)
	if err != nil {
		t.Fatalf("fetched bytes do not parse: %v", err)
	}
	if packages["kernel"] == "" {
		t.Error("fetched SBOM carries no kernel package")
	}

	var usedFallback bool
	for _, path := range fake.requested {
		if path == "/v2/org/image/manifests/sha256-aaaa" {
			usedFallback = true
		}
	}
	if !usedFallback {
		t.Error("Fetch did not try the fallback tag after a 404")
	}
}

func TestFetchReportsAnImageWithNoSBOM(t *testing.T) {
	fake := &fakeRegistry{referrersStatus: http.StatusNotFound}
	data, err := os.ReadFile(filepath.Join("testdata", "booted-syft.json"))
	if err != nil {
		t.Fatal(err)
	}
	fake.sbom = data

	// A registry where neither discovery path finds the artifact.
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			if _, err := w.Write([]byte(`{"token":"t"}`)); err != nil {
				t.Error(err)
			}
		case "/v2/org/image/manifests/stable":
			w.Header().Set("Docker-Content-Digest", "sha256:aaaa")
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	client := &RegistryClient{HTTP: server.Client()}
	host := strings.TrimPrefix(server.URL, "https://")
	if _, err := client.Fetch(context.Background(), host+"/org/image:stable"); err == nil {
		t.Fatal("Fetch reported success for an image with no attached SBOM")
	}
}

func TestFetchRejectsABlobThatDoesNotMatchItsDigest(t *testing.T) {
	fake := &fakeRegistry{
		referrersStatus: http.StatusOK,
		served:          []byte(`{"spdxVersion":"SPDX-2.3","name":"tampered"}`),
	}
	_, err := fetchFromFake(t, fake)
	if err == nil {
		t.Fatal("Fetch accepted a blob that does not hash to the digest it was fetched by")
	}
	if !strings.Contains(err.Error(), "does not match its digest") {
		t.Fatalf("Fetch failed for the wrong reason: %v", err)
	}
}
