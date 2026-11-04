package aistack

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
)

func TestNodeStatusModels(t *testing.T) {
	status := NodeStatus{
		Stored: map[string]json.RawMessage{
			"hf.co/unsloth/Ministral-3-8B-Instruct-2512-GGUF:Q4_K_M": nil,
			"hf.co/ornith-ai/Ornith-1.5-9B-GGUF:BF16":                nil,
		},
	}
	got := status.Models()
	want := []string{
		"hf.co/ornith-ai/Ornith-1.5-9B-GGUF:BF16",
		"hf.co/unsloth/Ministral-3-8B-Instruct-2512-GGUF:Q4_K_M",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("Models() = %v, want %v", got, want)
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
	if len(status.Stored) != 1 {
		t.Errorf("Stored length = %d, want 1", len(status.Stored))
	}
}

func TestConfigureActiveModel(t *testing.T) {
	h := newHost(t)
	unit, _ := RenderUnit(h.exe)
	path, _ := UnitPath()
	if err := writeAtomic(path, unit); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"memory":17179869184,"loaded":{},"stored":{"hf.co/unsloth/Qwen2.5-7B-Instruct-GGUF:Q4_K_M":{}}}`))
	}))
	defer srv.Close()
	origNodeURL := nodeURL
	nodeURL = srv.URL
	defer func() { nodeURL = origNodeURL }()

	err := ConfigureActiveModel(context.Background(), "hf.co/unsloth/Qwen2.5-7B-Instruct-GGUF:Q4_K_M")
	if err != nil {
		t.Fatalf("ConfigureActiveModel failed: %v", err)
	}

	wantCall := "llmman config set aliases.bluefin-active hf.co/unsloth/Qwen2.5-7B-Instruct-GGUF:Q4_K_M"
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
}

func TestModelSelectionStampReloadFailureLeavesTheUnitUnverified(t *testing.T) {
	h := newHost(t)
	unit, _ := RenderUnit(h.exe)
	path, _ := UnitPath()
	if err := writeAtomic(path, unit); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("manager rejected model invocation stamp")
	baseRun := run
	run = func(ctx context.Context, name string, args ...string) (string, error) {
		// Fail only the reload after restart and the successful ID query,
		// not the pre-start reload that proves the runtime took this unit.
		if name == "systemctl" && strings.Join(args, " ") == "--user show "+ServiceName+" --property=InvocationID --value" {
			h.fail["systemctl --user daemon-reload"] = failure
		}
		return baseRun(ctx, name, args...)
	}
	if err := ConfigureActiveModel(context.Background(), "example/model:Q4_K_M"); !errors.Is(err, failure) {
		t.Fatalf("model selection accepted a failed stamp reload: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != unit {
		t.Fatalf("model selection retained a verified stamp or lost its unit: %q, %v", got, err)
	}
}
