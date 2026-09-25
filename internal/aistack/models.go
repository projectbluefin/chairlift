package aistack

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

// ActiveModelAlias is the llmman alias configured for the selected model.
const ActiveModelAlias = "bluefin-active"

// SafetyMarginBytes is the RAM margin reserved when checking if a model fits in memory.
// Model file size + SafetyMarginBytes must be <= reported node memory.
const SafetyMarginBytes int64 = 2 * 1024 * 1024 * 1024 // 2 GiB

// FallbackCatalogDate is the ISO date of the offline fallback catalog.
const FallbackCatalogDate = "2026-09-24"

// splitShard matches a single split-shard GGUF filename (e.g.
// model-Q4_K_M-00001-of-00002.gguf). A split shard is one part of a model
// that must be reassembled, so it is not a standalone file llmman can pull.
var splitShard = regexp.MustCompile(`-\d{5}-of-\d{5}\.gguf$`)

// Family represents one of the 5 supported model families.
type Family string

const (
	FamilyQwen     Family = "qwen"
	FamilyMistral  Family = "mistral"
	FamilyGemma    Family = "gemma"
	FamilyDeepSeek Family = "deepseek"
	FamilyGPTOss   Family = "gpt-oss"
)

// DefaultFamily is Qwen per issue #255.
const DefaultFamily = FamilyQwen

// Families returns the five supported model families in display order.
func Families() []Family {
	return []Family{
		FamilyQwen,
		FamilyMistral,
		FamilyGemma,
		FamilyDeepSeek,
		FamilyGPTOss,
	}
}

// DisplayName returns a human-friendly family name.
func (f Family) DisplayName() string {
	switch f {
	case FamilyQwen:
		return "Qwen (Recommended)"
	case FamilyMistral:
		return "Mistral / Ministral"
	case FamilyGemma:
		return "Gemma"
	case FamilyDeepSeek:
		return "DeepSeek"
	case FamilyGPTOss:
		return "GPT-OSS"
	default:
		return string(f)
	}
}

// CandidateModel describes a specific model artifact.
type CandidateModel struct {
	Family       Family `json:"family"`
	Repo         string `json:"repo"`
	File         string `json:"file"`
	Quant        string `json:"quant"`
	SizeBytes    int64  `json:"size_bytes"`
	Params       string `json:"params"`
	IsChat       bool   `json:"is_chat"`
	IsGGUF       bool   `json:"is_gguf"`
	IsMultimodal bool   `json:"is_multimodal"`
	Downloads    int    `json:"downloads"`
	Likes        int    `json:"likes"`
	Cached       bool   `json:"cached"` // True when returned from offline fallback catalog
}

// ModelRef returns the llmman pull reference (e.g. repo:quant or repo/file).
func (c CandidateModel) ModelRef() string {
	if c.Quant != "" {
		return fmt.Sprintf("%s:%s", c.Repo, c.Quant)
	}
	return fmt.Sprintf("%s/%s", c.Repo, c.File)
}

// NodeStatus is the parsed response from /llmman/node.
type NodeStatus struct {
	Memory int64                      `json:"memory"`
	Loaded map[string]json.RawMessage `json:"loaded"`
	Stored map[string]json.RawMessage `json:"stored"`
}

// FetchFunc represents an injected HTTP getter for testability.
type FetchFunc func(ctx context.Context, url string) ([]byte, error)

// DefaultFetch uses http.DefaultClient with a 15-second timeout.
var DefaultFetch FetchFunc = func(ctx context.Context, reqURL string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "BluefinModelResolver/1.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %s from %s", resp.Status, reqURL)
	}
	return io.ReadAll(resp.Body)
}

// OfflineCatalog returns the dated last-known-good fallback models: text/chat
// GGUFs. Download/like counts are best-effort and may be approximate.
func OfflineCatalog() []CandidateModel {
	return []CandidateModel{
		// Qwen family
		{
			Family:    FamilyQwen,
			Repo:      "unsloth/Qwen2.5-3B-Instruct-GGUF",
			File:      "Qwen2.5-3B-Instruct-Q4_K_M.gguf",
			Quant:     "Q4_K_M",
			SizeBytes: 2_100_000_000,
			Params:    "3B",
			IsChat:    true,
			IsGGUF:    true,
			Downloads: 120_000,
			Likes:     450,
			Cached:    true,
		},
		{
			Family:    FamilyQwen,
			Repo:      "unsloth/Qwen2.5-7B-Instruct-GGUF",
			File:      "Qwen2.5-7B-Instruct-Q4_K_M.gguf",
			Quant:     "Q4_K_M",
			SizeBytes: 4_700_000_000,
			Params:    "7B",
			IsChat:    true,
			IsGGUF:    true,
			Downloads: 250_000,
			Likes:     980,
			Cached:    true,
		},
		{
			Family:    FamilyQwen,
			Repo:      "unsloth/Qwen2.5-14B-Instruct-GGUF",
			File:      "Qwen2.5-14B-Instruct-Q4_K_M.gguf",
			Quant:     "Q4_K_M",
			SizeBytes: 9_300_000_000,
			Params:    "14B",
			IsChat:    true,
			IsGGUF:    true,
			Downloads: 85_000,
			Likes:     410,
			Cached:    true,
		},
		// Mistral / Ministral
		{
			Family:    FamilyMistral,
			Repo:      "unsloth/Ministral-8B-Instruct-2410-GGUF",
			File:      "Ministral-8B-Instruct-2410-Q4_K_M.gguf",
			Quant:     "Q4_K_M",
			SizeBytes: 5_100_000_000,
			Params:    "8B",
			IsChat:    true,
			IsGGUF:    true,
			Downloads: 65_000,
			Likes:     310,
			Cached:    true,
		},
		{
			Family:    FamilyMistral,
			Repo:      "unsloth/Mistral-7B-Instruct-v0.3-GGUF",
			File:      "Mistral-7B-Instruct-v0.3-Q4_K_M.gguf",
			Quant:     "Q4_K_M",
			SizeBytes: 4_400_000_000,
			Params:    "7B",
			IsChat:    true,
			IsGGUF:    true,
			Downloads: 190_000,
			Likes:     820,
			Cached:    true,
		},
		// Gemma family
		{
			Family:    FamilyGemma,
			Repo:      "unsloth/gemma-2-2b-it-GGUF",
			File:      "gemma-2-2b-it-Q4_K_M.gguf",
			Quant:     "Q4_K_M",
			SizeBytes: 1_700_000_000,
			Params:    "2B",
			IsChat:    true,
			IsGGUF:    true,
			Downloads: 95_000,
			Likes:     380,
			Cached:    true,
		},
		{
			Family:    FamilyGemma,
			Repo:      "unsloth/gemma-2-9b-it-GGUF",
			File:      "gemma-2-9b-it-Q4_K_M.gguf",
			Quant:     "Q4_K_M",
			SizeBytes: 5_800_000_000,
			Params:    "9B",
			IsChat:    true,
			IsGGUF:    true,
			Downloads: 140_000,
			Likes:     610,
			Cached:    true,
		},
		// DeepSeek family
		{
			Family:    FamilyDeepSeek,
			Repo:      "unsloth/DeepSeek-R1-Distill-Qwen-1.5B-GGUF",
			File:      "DeepSeek-R1-Distill-Qwen-1.5B-Q4_K_M.gguf",
			Quant:     "Q4_K_M",
			SizeBytes: 1_100_000_000,
			Params:    "1.5B",
			IsChat:    true,
			IsGGUF:    true,
			Downloads: 58_380,
			Likes:     161,
			Cached:    true,
		},
		{
			Family:    FamilyDeepSeek,
			Repo:      "unsloth/DeepSeek-R1-Distill-Qwen-7B-GGUF",
			File:      "DeepSeek-R1-Distill-Qwen-7B-Q4_K_M.gguf",
			Quant:     "Q4_K_M",
			SizeBytes: 4_700_000_000,
			Params:    "7B",
			IsChat:    true,
			IsGGUF:    true,
			Downloads: 29_105,
			Likes:     107,
			Cached:    true,
		},
		{
			Family:    FamilyDeepSeek,
			Repo:      "unsloth/DeepSeek-R1-Distill-Qwen-14B-GGUF",
			File:      "DeepSeek-R1-Distill-Qwen-14B-Q4_K_M.gguf",
			Quant:     "Q4_K_M",
			SizeBytes: 9_300_000_000,
			Params:    "14B",
			IsChat:    true,
			IsGGUF:    true,
			Downloads: 47_668,
			Likes:     135,
			Cached:    true,
		},
		// GPT-OSS (Open Source GPT-style instruction models)
		{
			Family:    FamilyGPTOss,
			Repo:      "unsloth/gpt-oss-20b-GGUF",
			File:      "gpt-oss-20b-Q4_K_M.gguf",
			Quant:     "Q4_K_M",
			SizeBytes: 11624759488,
			Params:    "20B",
			IsChat:    true,
			IsGGUF:    true,
			Downloads: 520471,
			Likes:     840,
			Cached:    true,
		},
	}
}

// FitsInMemory reports whether the model candidate fits within node memory with SafetyMarginBytes.
func FitsInMemory(candidateSizeBytes int64, nodeMemoryBytes int64) bool {
	if nodeMemoryBytes <= 0 || candidateSizeBytes <= 0 {
		return false
	}
	return candidateSizeBytes+SafetyMarginBytes <= nodeMemoryBytes
}

// FilterEligibleModels returns models from candidates that:
// 1. Are Unsloth GGUFs (repo starts with unsloth/ and ends with -GGUF or file ends in .gguf)
// 2. Are text/chat models (IsChat == true, IsMultimodal == false)
// 3. Fit in available node memory (SizeBytes + SafetyMarginBytes <= memory)
func FilterEligibleModels(candidates []CandidateModel, nodeMemoryBytes int64) []CandidateModel {
	var eligible []CandidateModel
	for _, c := range candidates {
		if !c.IsGGUF || !c.IsChat || c.IsMultimodal {
			continue
		}
		if !strings.HasPrefix(c.Repo, "unsloth/") {
			continue
		}
		if nodeMemoryBytes > 0 && !FitsInMemory(c.SizeBytes, nodeMemoryBytes) {
			continue
		}
		eligible = append(eligible, c)
	}
	return eligible
}

// RankCandidateModels sorts models descending by qualification signals:
// 1. Fits in memory
// 2. Downloads + Likes (adoption/qualification)
// 3. Larger parameter size within budget (closer to capability ceiling)
func RankCandidateModels(candidates []CandidateModel) []CandidateModel {
	res := make([]CandidateModel, len(candidates))
	copy(res, candidates)
	sort.Slice(res, func(i, j int) bool {
		// Rank by adoption first (downloads + likes): a more popular model is
		// the safer default. Size breaks ties so that, for equal popularity,
		// the larger fit within budget wins.
		scoreI := int64(res[i].Downloads) + int64(res[i].Likes*100)
		scoreJ := int64(res[j].Downloads) + int64(res[j].Likes*100)
		if scoreI != scoreJ {
			return scoreI > scoreJ
		}
		return res[i].SizeBytes > res[j].SizeBytes
	})
	return res
}

// Hugging Face API types for live metadata resolution.
type hfModelInfo struct {
	ID        string   `json:"id"`
	Tags      []string `json:"tags"`
	Pipeline  string   `json:"pipeline_tag"`
	Downloads int      `json:"downloads"`
	Likes     int      `json:"likes"`
}

type hfTreeItem struct {
	Type string `json:"type"`
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// escapeRepo escapes a "owner/name" repository for a URL path, preserving the
// slash between segments. url.PathEscape would encode that slash to %2F, which
// the Hugging Face API rejects.
func escapeRepo(repo string) string {
	parts := strings.Split(repo, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}

// ResolveFamilyLive attempts live resolution of an Unsloth model repository on Hugging Face.
func ResolveFamilyLive(ctx context.Context, family Family, repo string, fetch FetchFunc) ([]CandidateModel, error) {
	if fetch == nil {
		fetch = DefaultFetch
	}

	infoURL := "https://huggingface.co/api/models/" + escapeRepo(repo)
	body, err := fetch(ctx, infoURL)
	if err != nil {
		return nil, fmt.Errorf("fetch model info for %s: %w", repo, err)
	}

	var info hfModelInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, fmt.Errorf("unmarshal model info for %s: %w", repo, err)
	}

	// Validate it is chat/text-generation
	isChat := info.Pipeline == "text-generation" || info.Pipeline == "conversational"
	var isGGUF, isMultimodal bool
	for _, tag := range info.Tags {
		t := strings.ToLower(tag)
		if t == "gguf" {
			isGGUF = true
		}
		if t == "conversational" || t == "chat" {
			isChat = true
		}
		if t == "multimodal" || t == "image-to-text" || t == "vision" {
			isMultimodal = true
		}
	}
	if !strings.HasSuffix(strings.ToLower(repo), "-gguf") && !isGGUF {
		return nil, errors.New("repository is not GGUF")
	}
	if !isChat || isMultimodal {
		return nil, errors.New("model is not a compatible chat/text model")
	}

	// Fetch recursive tree
	treeURL := fmt.Sprintf("https://huggingface.co/api/models/%s/tree/main?recursive=true", escapeRepo(repo))
	treeBody, err := fetch(ctx, treeURL)
	if err != nil {
		return nil, fmt.Errorf("fetch tree for %s: %w", repo, err)
	}

	var treeItems []hfTreeItem
	if err := json.Unmarshal(treeBody, &treeItems); err != nil {
		return nil, fmt.Errorf("unmarshal tree for %s: %w", repo, err)
	}

	var candidates []CandidateModel
	for _, item := range treeItems {
		if item.Type != "file" || !strings.HasSuffix(item.Path, ".gguf") {
			continue
		}
		// Skip split shards (model-Q4_K_M-00001-of-00002.gguf): parts of one
		// model, not a standalone file llmman can pull.
		if splitShard.MatchString(item.Path) {
			continue
		}
		// Extract quant name (e.g. Q4_K_M from model-Q4_K_M.gguf). Files with
		// no quant token (e.g. model-F16.gguf) are not pullable quants.
		quant := extractQuant(item.Path)
		if quant == "" {
			continue
		}
		candidates = append(candidates, CandidateModel{
			Family:       family,
			Repo:         repo,
			File:         item.Path,
			Quant:        quant,
			SizeBytes:    item.Size,
			IsChat:       true,
			IsGGUF:       true,
			IsMultimodal: false,
			Downloads:    info.Downloads,
			Likes:        info.Likes,
			Cached:       false,
		})
	}

	if len(candidates) == 0 {
		return nil, errors.New("no .gguf files found in repository tree")
	}

	return candidates, nil
}

func extractQuant(filename string) string {
	base := strings.TrimSuffix(filename, ".gguf")
	parts := strings.Split(base, "-")
	if len(parts) > 1 {
		last := parts[len(parts)-1]
		if strings.HasPrefix(last, "Q") || strings.HasPrefix(last, "q") {
			return last
		}
	}
	parts = strings.Split(base, "_")
	if len(parts) > 1 {
		last := parts[len(parts)-1]
		if strings.HasPrefix(last, "Q") || strings.HasPrefix(last, "q") {
			return last
		}
	}
	return ""
}

// ResolveCandidate resolves the best eligible candidate model for a given family and node memory.
// It tries live HF resolution if fetch is provided; on failure, it falls back to the dated offline catalog.
func ResolveCandidate(ctx context.Context, family Family, nodeMemoryBytes int64, fetch FetchFunc) (CandidateModel, error) {
	// 1. Try live lookup for family
	primaryRepo := defaultFamilyRepo(family)
	if fetch != nil && primaryRepo != "" {
		liveModels, err := ResolveFamilyLive(ctx, family, primaryRepo, fetch)
		if err == nil {
			eligible := FilterEligibleModels(liveModels, nodeMemoryBytes)
			if len(eligible) > 0 {
				ranked := RankCandidateModels(eligible)
				return ranked[0], nil
			}
		}
	}

	// 2. Offline fallback catalog
	var familyFallback []CandidateModel
	for _, m := range OfflineCatalog() {
		if m.Family == family {
			familyFallback = append(familyFallback, m)
		}
	}

	eligible := FilterEligibleModels(familyFallback, nodeMemoryBytes)
	if len(eligible) == 0 {
		return CandidateModel{}, fmt.Errorf("no compatible model found for family %q with %d bytes memory", family, nodeMemoryBytes)
	}

	ranked := RankCandidateModels(eligible)
	return ranked[0], nil
}

func defaultFamilyRepo(f Family) string {
	switch f {
	case FamilyQwen:
		return "unsloth/Qwen2.5-7B-Instruct-GGUF"
	case FamilyMistral:
		return "unsloth/Ministral-8B-Instruct-2410-GGUF"
	case FamilyGemma:
		return "unsloth/gemma-2-9b-it-GGUF"
	case FamilyDeepSeek:
		return "unsloth/DeepSeek-R1-Distill-Qwen-7B-GGUF"
	case FamilyGPTOss:
		return "unsloth/gpt-oss-20b-GGUF"
	default:
		return ""
	}
}

// FetchNodeStatus queries /llmman/node and returns NodeStatus with reported memory.
func FetchNodeStatus(ctx context.Context) (NodeStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, nodeURL, nil)
	if err != nil {
		return NodeStatus{}, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return NodeStatus{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return NodeStatus{}, fmt.Errorf("HTTP %s from node endpoint", resp.Status)
	}
	var node NodeStatus
	if err := json.NewDecoder(resp.Body).Decode(&node); err != nil {
		return NodeStatus{}, err
	}
	return node, nil
}

// ConfigureActiveModel sets the active model alias `bluefin-active` via `llmman config set`.
func ConfigureActiveModel(ctx context.Context, modelRef string) error {
	exe := Executable()
	if exe == "" {
		return errors.New("llmman executable not found")
	}
	// Execute: llmman config set aliases.bluefin-active <modelRef>
	_, err := run(ctx, exe, "config", "set", "aliases."+ActiveModelAlias, modelRef)
	if err != nil {
		return fmt.Errorf("llmman config set alias: %w", err)
	}
	return nil
}

// ReadActiveModel returns the model ref configured as the active alias
// `bluefin-active` via `llmman config get`. It returns ("", nil) when no
// alias has been set yet.
func ReadActiveModel(ctx context.Context) (string, error) {
	exe := Executable()
	if exe == "" {
		return "", errors.New("llmman executable not found")
	}
	out, err := run(ctx, exe, "config", "get", "aliases."+ActiveModelAlias)
	if err != nil {
		return "", fmt.Errorf("llmman config get alias: %w", err)
	}
	return strings.TrimSpace(out), nil
}

// PullModel pulls the model using `llmman pull <modelRef>` and verifies it was stored in daemon.
func PullModel(ctx context.Context, modelRef string) error {
	exe := Executable()
	if exe == "" {
		return errors.New("llmman executable not found")
	}
	out, err := run(ctx, exe, "pull", modelRef)
	if err != nil {
		return fmt.Errorf("llmman pull %s: %w (%s)", modelRef, err, out)
	}

	// Verify model appears in daemon /llmman/node stored models
	return VerifyModelStored(ctx, modelRef)
}

// VerifyModelStored checks whether the model appears in stored models on
// /llmman/node. It returns an error unless the model is present, so a failed
// pull or a daemon that never stored the model is reported rather than silently
// accepted.
func VerifyModelStored(ctx context.Context, modelRef string) error {
	node, err := FetchNodeStatus(ctx)
	if err != nil {
		return fmt.Errorf("verify stored model: fetch node status: %w", err)
	}
	if len(node.Stored) == 0 {
		return fmt.Errorf("verify stored model: daemon reported no stored models")
	}
	// Check if modelRef or base name is key in stored map
	baseRef := modelRef
	if idx := strings.LastIndex(modelRef, "/"); idx >= 0 {
		baseRef = modelRef[idx+1:]
	}
	for k := range node.Stored {
		if strings.Contains(k, baseRef) || strings.Contains(modelRef, k) {
			return nil
		}
	}
	return fmt.Errorf("verify stored model: %s not found in daemon stored models", modelRef)
}
