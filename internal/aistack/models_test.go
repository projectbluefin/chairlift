package aistack

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFamiliesInventory(t *testing.T) {
	fams := Families()
	if len(fams) != 5 {
		t.Fatalf("Families() returned %d families, want 5", len(fams))
	}
	expected := []Family{FamilyQwen, FamilyMistral, FamilyGemma, FamilyDeepSeek, FamilyGPTOss}
	for i, f := range fams {
		if f != expected[i] {
			t.Errorf("Families()[%d] = %v, want %v", i, f, expected[i])
		}
		if f.DisplayName() == "" {
			t.Errorf("Family %s has empty display name", f)
		}
	}
	if DefaultFamily != FamilyQwen {
		t.Errorf("DefaultFamily = %s, want %s", DefaultFamily, FamilyQwen)
	}
}

func TestFitsInMemorySafetyMargin(t *testing.T) {
	// 2 GiB safety margin (SafetyMarginBytes)
	const gigabyte = int64(1024 * 1024 * 1024)

	// Candidate: 4 GiB
	candidateSize := 4 * gigabyte

	// Node memory: 5 GiB -> 4 + 2 = 6 GiB > 5 GiB -> false
	if FitsInMemory(candidateSize, 5*gigabyte) {
		t.Error("FitsInMemory(4GB, 5GB) should be false (needs 4+2=6GB)")
	}

	// Node memory: 6 GiB -> exactly 4 + 2 = 6 GiB -> true
	if !FitsInMemory(candidateSize, 6*gigabyte) {
		t.Error("FitsInMemory(4GB, 6GB) should be true (4+2=6GB)")
	}

	// Node memory: 8 GiB -> 4 + 2 = 6 GiB <= 8 GiB -> true
	if !FitsInMemory(candidateSize, 8*gigabyte) {
		t.Error("FitsInMemory(4GB, 8GB) should be true")
	}

	// Invalid inputs
	if FitsInMemory(candidateSize, 0) {
		t.Error("FitsInMemory with 0 memory should be false")
	}
	if FitsInMemory(0, 8*gigabyte) {
		t.Error("FitsInMemory with 0 candidate size should be false")
	}
}

func TestFilterEligibleModelsRejectsIncompatible(t *testing.T) {
	const gigabyte = int64(1024 * 1024 * 1024)
	nodeMem := 16 * gigabyte

	tests := []struct {
		name      string
		candidate CandidateModel
		wantPass  bool
	}{
		{
			name: "valid unsloth chat gguf",
			candidate: CandidateModel{
				Family:       FamilyQwen,
				Repo:         "unsloth/Qwen2.5-7B-Instruct-GGUF",
				File:         "model.gguf",
				SizeBytes:    4 * gigabyte,
				IsChat:       true,
				IsGGUF:       true,
				IsMultimodal: false,
			},
			wantPass: true,
		},
		{
			name: "non-unsloth repo",
			candidate: CandidateModel{
				Family:       FamilyQwen,
				Repo:         "someone-else/Qwen2.5-7B-Instruct-GGUF",
				File:         "model.gguf",
				SizeBytes:    4 * gigabyte,
				IsChat:       true,
				IsGGUF:       true,
				IsMultimodal: false,
			},
			wantPass: false,
		},
		{
			name: "non-gguf format",
			candidate: CandidateModel{
				Family:       FamilyQwen,
				Repo:         "unsloth/Qwen2.5-7B-Instruct-GGUF",
				File:         "model.safetensors",
				SizeBytes:    4 * gigabyte,
				IsChat:       true,
				IsGGUF:       false,
				IsMultimodal: false,
			},
			wantPass: false,
		},
		{
			name: "base-only / non-chat model",
			candidate: CandidateModel{
				Family:       FamilyQwen,
				Repo:         "unsloth/Qwen2.5-7B-Instruct-GGUF",
				File:         "model.gguf",
				SizeBytes:    4 * gigabyte,
				IsChat:       false,
				IsGGUF:       true,
				IsMultimodal: false,
			},
			wantPass: false,
		},
		{
			name: "multimodal vision model",
			candidate: CandidateModel{
				Family:       FamilyQwen,
				Repo:         "unsloth/Qwen2.5-7B-Instruct-GGUF",
				File:         "model.gguf",
				SizeBytes:    4 * gigabyte,
				IsChat:       true,
				IsGGUF:       true,
				IsMultimodal: true,
			},
			wantPass: false,
		},
		{
			name: "too large for node memory",
			candidate: CandidateModel{
				Family:       FamilyQwen,
				Repo:         "unsloth/Qwen2.5-7B-Instruct-GGUF",
				File:         "model.gguf",
				SizeBytes:    15 * gigabyte, // 15 + 2 = 17 > 16GB
				IsChat:       true,
				IsGGUF:       true,
				IsMultimodal: false,
			},
			wantPass: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := FilterEligibleModels([]CandidateModel{tt.candidate}, nodeMem)
			if tt.wantPass && len(res) == 0 {
				t.Errorf("candidate was filtered out, expected eligible")
			}
			if !tt.wantPass && len(res) > 0 {
				t.Errorf("ineligible candidate passed filter")
			}
		})
	}
}

func TestOfflineFallbackCatalogTotalCoverage(t *testing.T) {
	catalog := OfflineCatalog()
	if len(catalog) == 0 {
		t.Fatal("OfflineCatalog is empty")
	}

	covered := make(map[Family]bool)
	for _, m := range catalog {
		if !m.Cached {
			t.Errorf("OfflineCatalog entry %s is not marked Cached", m.Repo)
		}
		if !m.IsGGUF || !m.IsChat || m.IsMultimodal {
			t.Errorf("OfflineCatalog entry %s violates GGUF chat criteria", m.Repo)
		}
		if m.SizeBytes <= 0 {
			t.Errorf("OfflineCatalog entry %s has invalid size %d", m.Repo, m.SizeBytes)
		}
		covered[m.Family] = true
	}

	for _, fam := range Families() {
		if !covered[fam] {
			t.Errorf("OfflineCatalog missing coverage for family %s", fam)
		}
	}
}

func TestResolveCandidateOfflineFallback(t *testing.T) {
	const gigabyte = int64(1024 * 1024 * 1024)

	// When fetch is nil or fails, offline fallback is used
	for _, fam := range Families() {
		t.Run(string(fam), func(t *testing.T) {
			// 16 GiB RAM
			model, err := ResolveCandidate(context.Background(), fam, 16*gigabyte, nil)
			if err != nil {
				t.Fatalf("ResolveCandidate(%s) failed: %v", fam, err)
			}
			if model.Family != fam {
				t.Errorf("got family %s, want %s", model.Family, fam)
			}
			if !model.Cached {
				t.Errorf("model from offline fallback must be marked Cached")
			}
			if model.ModelRef() == "" {
				t.Errorf("ModelRef() was empty")
			}
		})
	}
}

func TestResolveCandidateMemoryLimit(t *testing.T) {
	// Tiny memory (1 GiB) - no model can fit (min model size is > 1GB + 2GB margin = 3GB)
	const gigabyte = int64(1024 * 1024 * 1024)
	_, err := ResolveCandidate(context.Background(), FamilyQwen, 1*gigabyte, nil)
	if err == nil {
		t.Fatal("ResolveCandidate with 1GB memory should fail, got nil error")
	}
}

func TestResolveFamilyLiveWithMockServer(t *testing.T) {
	mockInfo := hfModelInfo{
		ID:        "unsloth/Qwen2.5-7B-Instruct-GGUF",
		Pipeline:  "text-generation",
		Tags:      []string{"gguf", "chat"},
		Downloads: 500000,
		Likes:     1500,
	}
	infoBytes, _ := json.Marshal(mockInfo)

	mockTree := []hfTreeItem{
		{Type: "file", Path: "Qwen2.5-7B-Instruct-Q4_K_M.gguf", Size: 4_700_000_000},
		{Type: "file", Path: "README.md", Size: 1024},
	}
	treeBytes, _ := json.Marshal(mockTree)

	fakeFetch := func(ctx context.Context, reqURL string) ([]byte, error) {
		if strings.Contains(reqURL, "/tree/") {
			return treeBytes, nil
		}
		return infoBytes, nil
	}

	candidates, err := ResolveFamilyLive(context.Background(), FamilyQwen, "unsloth/Qwen2.5-7B-Instruct-GGUF", fakeFetch)
	if err != nil {
		t.Fatalf("ResolveFamilyLive failed: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("got %d candidates, want 1", len(candidates))
	}
	cand := candidates[0]
	if cand.Quant != "Q4_K_M" {
		t.Errorf("quant = %s, want Q4_K_M", cand.Quant)
	}
	if cand.SizeBytes != 4_700_000_000 {
		t.Errorf("size = %d, want 4700000000", cand.SizeBytes)
	}
	if cand.Cached {
		t.Errorf("live resolved model should not be marked Cached")
	}
}

func TestFetchNodeStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"memory":17179869184,"loaded":{},"stored":{"unsloth/Qwen2.5-7B-Instruct-GGUF:Q4_K_M":{}}}`))
	}))
	defer srv.Close()

	origNodeURL := nodeURL
	nodeURL = srv.URL
	defer func() { nodeURL = origNodeURL }()

	status, err := FetchNodeStatus(context.Background())
	if err != nil {
		t.Fatalf("FetchNodeStatus error: %v", err)
	}
	if status.Memory != 17179869184 {
		t.Errorf("Memory = %d, want 17179869184", status.Memory)
	}
	if len(status.Stored) != 1 {
		t.Errorf("Stored length = %d, want 1", len(status.Stored))
	}
}

func TestConfigureActiveModelAndPull(t *testing.T) {
	h := newHost(t)

	// VerifyModelStored (via PullModel) reads /llmman/node, so point the
	// node server at a mock that reports the model stored.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"memory":17179869184,"loaded":{},"stored":{"unsloth/Qwen2.5-7B-Instruct-GGUF:Q4_K_M":{}}}`))
	}))
	defer srv.Close()
	origNodeURL := nodeURL
	nodeURL = srv.URL
	defer func() { nodeURL = origNodeURL }()

	// Configure active model
	err := ConfigureActiveModel(context.Background(), "unsloth/Qwen2.5-7B-Instruct-GGUF:Q4_K_M")
	if err != nil {
		t.Fatalf("ConfigureActiveModel failed: %v", err)
	}

	wantCall := "llmman config set aliases.bluefin-active unsloth/Qwen2.5-7B-Instruct-GGUF:Q4_K_M"
	var foundConfig bool
	for _, call := range h.calls {
		if call == wantCall {
			foundConfig = true
			break
		}
	}
	if !foundConfig {
		t.Errorf("expected call %q in %v", wantCall, h.calls)
	}

	// Pull model
	err = PullModel(context.Background(), "unsloth/Qwen2.5-7B-Instruct-GGUF:Q4_K_M")
	if err != nil {
		t.Fatalf("PullModel failed: %v", err)
	}

	wantPull := "llmman pull unsloth/Qwen2.5-7B-Instruct-GGUF:Q4_K_M"
	var foundPull bool
	for _, call := range h.calls {
		if call == wantPull {
			foundPull = true
			break
		}
	}
	if !foundPull {
		t.Errorf("expected call %q in %v", wantPull, h.calls)
	}
}
