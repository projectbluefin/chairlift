package aistack

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/projectbluefin/chairlift/internal/dryrun"
)

func TestParsePeerAddressAcceptsDocumentedForms(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"bare hostname", "asahi", "asahi"},
		{"hostname with port", "asahi:17434", "asahi:17434"},
		{"IPv4", "10.0.0.5", "10.0.0.5"},
		{"IPv4 with port", "10.0.0.5:17434", "10.0.0.5:17434"},
		{"bracketed IPv6", "[fe80::1]", "[fe80::1]"},
		{"bracketed IPv6 with port", "[fe80::1]:17434", "[fe80::1]:17434"},
		{"bare IPv6 no port", "::1", "[::1]"},
		{"http scheme", "http://spark:17434", "http://spark:17434"},
		{"https scheme", "https://spark", "https://spark"},
		{"uppercase hostname lowercased", "SPARK.local", "spark.local"},
		{"whitespace trimmed", "  spark  ", "spark"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParsePeerAddress(tt.in)
			if err != nil {
				t.Fatalf("ParsePeerAddress(%q) unexpected error: %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("ParsePeerAddress(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestParsePeerAddressRejectsUnsupportedForms(t *testing.T) {
	tests := []string{
		"",
		"   ",
		"ftp://spark",
		"ssh://spark",
		"user:pass@spark",
		"http://user:pass@spark",
		"http://spark/path",
		"spark/path",
		"http://spark?x=1",
		"http://spark#frag",
		"spark:notaport",
		"spark:99999",
		"spark:0",
		"not a valid host!!",
		"fe80::1:17434", // unbracketed IPv6 cannot carry a port
		"[fe80::1",      // unterminated bracket
	}
	for _, in := range tests {
		t.Run(in, func(t *testing.T) {
			if _, err := ParsePeerAddress(in); err == nil {
				t.Errorf("ParsePeerAddress(%q) accepted, want error", in)
			}
		})
	}
}

func TestNodeURLDefaultsSchemeAndPort(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"spark", "http://spark:17434/llmman/node"},
		{"spark:9999", "http://spark:9999/llmman/node"},
		{"https://spark", "https://spark:17434/llmman/node"},
		{"https://spark:9999", "https://spark:9999/llmman/node"},
		{"10.0.0.5", "http://10.0.0.5:17434/llmman/node"},
		{"[fe80::1]", "http://[fe80::1]:17434/llmman/node"},
		{"[fe80::1]:9999", "http://[fe80::1]:9999/llmman/node"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := NodeURL(tt.in)
			if err != nil {
				t.Fatalf("NodeURL(%q) unexpected error: %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("NodeURL(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestNodeURLRejectsInvalidAddress(t *testing.T) {
	if _, err := NodeURL("ftp://spark"); err == nil {
		t.Error("NodeURL accepted an unsupported scheme")
	}
}

// peerHost isolates the peer-store and llmman-config seams for the mutator
// tests, mirroring host above but scoped to peers.go's own seams.
type peerHost struct {
	home  string
	exe   string
	calls []string
	fail  map[string]error
}

func newPeerHost(t *testing.T) *peerHost {
	t.Helper()
	ph := &peerHost{home: t.TempDir(), fail: map[string]error{}}
	bin := t.TempDir()
	ph.exe = filepath.Join(bin, "llmman")
	if err := os.WriteFile(ph.exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	oh, ol, ob, orc, orsc := homeDir, lookPath, brewPath, runConfigSet, runSecretConfigSet
	t.Cleanup(func() { homeDir, lookPath, brewPath, runConfigSet, runSecretConfigSet = oh, ol, ob, orc, orsc })

	homeDir = func() (string, error) { return ph.home, nil }
	lookPath = func(string) (string, error) { return ph.exe, nil }
	brewPath = func() string { return filepath.Join(bin, "brew") }
	runConfigSet = func(_ context.Context, exe, key, value string) error {
		call := "config set " + key + " " + value
		ph.calls = append(ph.calls, call)
		return ph.fail[call]
	}
	runSecretConfigSet = func(_ context.Context, exe, key, value string) error {
		// Record that the call happened and which key was touched, but
		// never the secret value itself — the same discipline the real
		// implementation owes the log.
		call := "secret-config-set " + key
		ph.calls = append(ph.calls, call)
		return ph.fail[call]
	}
	return ph
}

func TestAddPeerAppliesThenPersists(t *testing.T) {
	ph := newPeerHost(t)
	if err := AddPeer(context.Background(), "spark:17434"); err != nil {
		t.Fatalf("AddPeer: %v", err)
	}
	want := "config set aggregation.peers spark:17434"
	if len(ph.calls) != 1 || ph.calls[0] != want {
		t.Errorf("calls = %v, want [%s]", ph.calls, want)
	}
	peers, err := Peers()
	if err != nil {
		t.Fatal(err)
	}
	if len(peers) != 1 || peers[0].Address != "spark:17434" || !peers[0].Enabled {
		t.Errorf("Peers() = %+v", peers)
	}
}

func TestAddPeerRejectsDuplicate(t *testing.T) {
	newPeerHost(t)
	ctx := context.Background()
	if err := AddPeer(ctx, "spark"); err != nil {
		t.Fatal(err)
	}
	if err := AddPeer(ctx, "spark"); err == nil {
		t.Error("AddPeer of a duplicate address succeeded, want error")
	}
}

func TestAddPeerRejectsInvalidAddressWithoutTouchingConfigOrStore(t *testing.T) {
	ph := newPeerHost(t)
	if err := AddPeer(context.Background(), "ftp://nope"); err == nil {
		t.Error("AddPeer accepted an unsupported scheme")
	}
	if len(ph.calls) != 0 {
		t.Errorf("calls = %v, want none", ph.calls)
	}
	peers, err := Peers()
	if err != nil {
		t.Fatal(err)
	}
	if len(peers) != 0 {
		t.Errorf("Peers() = %+v, want empty", peers)
	}
}

func TestAddPeerConfigCommandFailureLeavesStoreUntouched(t *testing.T) {
	ph := newPeerHost(t)
	ph.fail["config set aggregation.peers spark"] = errors.New("llmman: config set failed")
	if err := AddPeer(context.Background(), "spark"); err == nil {
		t.Fatal("AddPeer succeeded despite a config-command failure")
	}
	peers, err := Peers()
	if err != nil {
		t.Fatal(err)
	}
	if len(peers) != 0 {
		t.Errorf("Peers() = %+v, want empty after a failed apply", peers)
	}
}

func TestRemovePeerAppliesRemainingThenPersists(t *testing.T) {
	ph := newPeerHost(t)
	ctx := context.Background()
	if err := AddPeer(ctx, "spark"); err != nil {
		t.Fatal(err)
	}
	if err := AddPeer(ctx, "asahi"); err != nil {
		t.Fatal(err)
	}
	ph.calls = nil

	if err := RemovePeer(ctx, "spark"); err != nil {
		t.Fatalf("RemovePeer: %v", err)
	}
	want := "config set aggregation.peers asahi"
	if len(ph.calls) != 1 || ph.calls[0] != want {
		t.Errorf("calls = %v, want [%s]", ph.calls, want)
	}
	peers, err := Peers()
	if err != nil {
		t.Fatal(err)
	}
	if len(peers) != 1 || peers[0].Address != "asahi" {
		t.Errorf("Peers() = %+v", peers)
	}
}

func TestRemovePeerRejectsUnknownAddress(t *testing.T) {
	newPeerHost(t)
	if err := RemovePeer(context.Background(), "spark"); err == nil {
		t.Error("RemovePeer of an unconfigured address succeeded, want error")
	}
}

func TestSetPeerEnabledExcludesDisabledFromAppliedListButKeepsItStored(t *testing.T) {
	ph := newPeerHost(t)
	ctx := context.Background()
	if err := AddPeer(ctx, "spark"); err != nil {
		t.Fatal(err)
	}
	if err := AddPeer(ctx, "asahi"); err != nil {
		t.Fatal(err)
	}
	ph.calls = nil

	if err := SetPeerEnabled(ctx, "spark", false); err != nil {
		t.Fatalf("SetPeerEnabled: %v", err)
	}
	want := "config set aggregation.peers asahi"
	if len(ph.calls) != 1 || ph.calls[0] != want {
		t.Errorf("calls = %v, want [%s]", ph.calls, want)
	}
	peers, err := Peers()
	if err != nil {
		t.Fatal(err)
	}
	var sawSpark bool
	for _, p := range peers {
		if p.Address == "spark" {
			sawSpark = true
			if p.Enabled {
				t.Error("disabled peer still reads enabled")
			}
		}
	}
	if !sawSpark {
		t.Error("disabling a peer must not remove it from the stored list")
	}
}

func TestEmptyPeerListAppliesAnEmptyValue(t *testing.T) {
	ph := newPeerHost(t)
	ctx := context.Background()
	if err := AddPeer(ctx, "spark"); err != nil {
		t.Fatal(err)
	}
	if err := RemovePeer(ctx, "spark"); err != nil {
		t.Fatal(err)
	}
	want := "config set aggregation.peers "
	if len(ph.calls) != 2 || ph.calls[1] != want {
		t.Errorf("calls = %v, want last call %q", ph.calls, want)
	}
	peers, err := Peers()
	if err != nil {
		t.Fatal(err)
	}
	if len(peers) != 0 {
		t.Errorf("Peers() = %+v, want empty", peers)
	}
}

func TestPeersOnAFreshHostIsEmptyNotAnError(t *testing.T) {
	newPeerHost(t)
	peers, err := Peers()
	if err != nil {
		t.Fatalf("Peers() on a fresh host: %v", err)
	}
	if len(peers) != 0 {
		t.Errorf("Peers() = %+v, want empty", peers)
	}
}

func TestSetPeerAPIKeyGoesThroughTheSecretSeamAndNeverAppearsInCalls(t *testing.T) {
	ph := newPeerHost(t)
	const secret = "s3kr3t-token-do-not-log-me"
	if err := SetPeerAPIKey(context.Background(), secret); err != nil {
		t.Fatalf("SetPeerAPIKey: %v", err)
	}
	for _, call := range ph.calls {
		if strings.Contains(call, secret) {
			t.Fatalf("recorded call leaked the API key: %q", call)
		}
	}
	if len(ph.calls) != 1 || ph.calls[0] != "secret-config-set aggregation.api_key" {
		t.Errorf("calls = %v", ph.calls)
	}
}

func TestSetPeerAPIKeyFailsCleanlyWhenLLMManIsNotInstalled(t *testing.T) {
	newPeerHost(t)
	lookPath = func(string) (string, error) { return "", errors.New("not on PATH") }
	brewPath = func() string { return "" }
	if err := SetPeerAPIKey(context.Background(), "x"); err == nil {
		t.Error("SetPeerAPIKey succeeded with no llmman installed")
	}
}

func TestPeerMutationsHonorDryrun(t *testing.T) {
	ph := newPeerHost(t)
	dryrun.Set(true)
	t.Cleanup(func() { dryrun.Set(false) })

	ctx := context.Background()
	if err := AddPeer(ctx, "spark:17434"); err != nil {
		t.Fatalf("AddPeer under dryrun: %v", err)
	}
	if err := SetPeerAPIKey(ctx, "secret-test-key"); err != nil {
		t.Fatalf("SetPeerAPIKey under dryrun: %v", err)
	}

	// Assert runner was never invoked
	if len(ph.calls) != 0 {
		t.Errorf("expected no config calls under dryrun, got: %v", ph.calls)
	}

	// Assert store file was not written
	storePath := filepath.Join(ph.home, ".local", "share", "chairlift", peersFileName)
	if _, err := os.Stat(storePath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("peers store %s exists or gave unexpected error: %v", storePath, err)
	}

	// Pre-populate store to test RemovePeer and SetPeerEnabled under dryrun
	dryrun.Set(false)
	if err := AddPeer(ctx, "spark:17434"); err != nil {
		t.Fatalf("AddPeer setup: %v", err)
	}
	ph.calls = nil
	dryrun.Set(true)

	if err := SetPeerEnabled(ctx, "spark:17434", false); err != nil {
		t.Fatalf("SetPeerEnabled under dryrun: %v", err)
	}
	if err := RemovePeer(ctx, "spark:17434"); err != nil {
		t.Fatalf("RemovePeer under dryrun: %v", err)
	}

	// Assert runner still not invoked
	if len(ph.calls) != 0 {
		t.Errorf("expected no config calls for SetPeerEnabled/RemovePeer under dryrun, got: %v", ph.calls)
	}

	// Assert store content remained unchanged from the AddPeer setup
	dryrun.Set(false)
	peers, err := Peers()
	if err != nil {
		t.Fatalf("Peers(): %v", err)
	}
	if len(peers) != 1 || peers[0].Address != "spark:17434" || !peers[0].Enabled {
		t.Errorf("Peers store modified under dryrun: %+v", peers)
	}
}

// --- ProbePeer ---

func TestProbePeerReachableParsesMemoryLoadedAndStored(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/llmman/node" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"memory": 137438953472,
			"loaded": map[string]int64{"gemma": 8100000000},
			"stored": map[string]int64{"gemma": 8100000000},
		})
	}))
	defer server.Close()

	status := ProbePeer(context.Background(), server.URL, "the-shared-key")
	if !status.Reachable {
		t.Fatalf("status = %+v, want Reachable", status)
	}
	if status.Memory != 137438953472 {
		t.Errorf("Memory = %d", status.Memory)
	}
	if status.Loaded["gemma"] != 8100000000 || status.Stored["gemma"] != 8100000000 {
		t.Errorf("Loaded/Stored = %+v / %+v", status.Loaded, status.Stored)
	}
	if gotAuth != "Bearer the-shared-key" {
		t.Errorf("Authorization header = %q", gotAuth)
	}
	if strings.Contains(status.Error, "the-shared-key") {
		t.Errorf("status.Error leaked the API key: %q", status.Error)
	}
}

func TestProbePeerUnauthorizedNeverLeaksTheKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	status := ProbePeer(context.Background(), server.URL, "another-secret-key")
	if status.Reachable {
		t.Error("a 401 must not be reported reachable")
	}
	if !status.Unauthorized {
		t.Error("status.Unauthorized = false, want true")
	}
	if strings.Contains(status.Error, "another-secret-key") {
		t.Errorf("status.Error leaked the API key: %q", status.Error)
	}
}

func TestProbePeerMalformedJSONIsReportedNotPanicked(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer server.Close()

	status := ProbePeer(context.Background(), server.URL, "")
	if status.Reachable {
		t.Error("malformed JSON must not be reported reachable")
	}
	if status.Error == "" {
		t.Error("status.Error is empty for a malformed response")
	}
}

func TestProbePeerTimeoutIsUnreachableNotEmptyData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(map[string]any{"memory": 1})
	}))
	defer server.Close()

	original := peerProbeTimeout
	peerProbeTimeout = 20 * time.Millisecond
	t.Cleanup(func() { peerProbeTimeout = original })

	status := ProbePeer(context.Background(), server.URL, "")
	if status.Reachable {
		t.Error("a timeout must not be reported reachable")
	}
	if status.Loaded != nil || status.Stored != nil {
		t.Errorf("a timeout must not synthesize empty maps as data: %+v", status)
	}
	if status.Error == "" {
		t.Error("status.Error is empty for a timeout")
	}
}

func TestProbePeerInvalidAddressIsReportedNotPanicked(t *testing.T) {
	status := ProbePeer(context.Background(), "ftp://nope", "")
	if status.Reachable {
		t.Error("an invalid address must not be reported reachable")
	}
	if status.Error == "" {
		t.Error("status.Error is empty for an invalid address")
	}
}
