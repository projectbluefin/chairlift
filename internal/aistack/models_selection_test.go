package aistack

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const gib = int64(1024 * 1024 * 1024)

func TestQuantPriorityOrdersPreferredQuantizations(t *testing.T) {
	ordered := []string{
		"Q4_K_M", "Q5_K_M", "Q4_K_S", "Q5_K_S", "Q4_1", "Q3_K_M", "Q3_K_S",
		"Q6_K", "Q8_0", "Q4_K_XL", "Q5_K_XL", "Q8_K_XL",
	}
	for i := 1; i < len(ordered); i++ {
		prev, cur := quantPriority(ordered[i-1]), quantPriority(ordered[i])
		if prev >= cur {
			t.Errorf("quantPriority(%s)=%d, want it ahead of quantPriority(%s)=%d", ordered[i-1], prev, ordered[i], cur)
		}
	}
	if got, want := quantPriority("q4_k_m"), quantPriority("Q4_K_M"); got != want {
		t.Errorf("quantPriority is case-sensitive: q4_k_m=%d, Q4_K_M=%d", got, want)
	}
	for _, ud := range []string{"Q4_K_XL", "Q5_K_XL", "Q8_K_XL"} {
		if got, want := quantPriority("UD-"+ud), quantPriority(ud); got != want {
			t.Errorf("quantPriority(UD-%s)=%d, want %d (same as %s)", ud, got, want, ud)
		}
	}
	last := quantPriority(ordered[len(ordered)-1])
	for _, unknown := range []string{"", "F16", "BF16", "IQ4_XS", "Q2_K"} {
		if got := quantPriority(unknown); got <= last {
			t.Errorf("quantPriority(%q)=%d, want it behind every known quantization (>%d)", unknown, got, last)
		}
	}
}

func TestExtractQuantReadsTheTrailingQuantToken(t *testing.T) {
	tests := []struct {
		file string
		want string
	}{
		{"Qwen3-8B-Q4_K_M.gguf", "Q4_K_M"},
		{"gemma-3-4b-it-q4_0.gguf", "q4_0"},
		{"Q8_0/Qwen3-8B-Q8_0.gguf", "Q8_0"},
		{"Qwen3-8B-BF16.gguf", ""},
		{"Qwen3-8B-F16.gguf", ""},
		{"model.gguf", ""},
	}
	for _, tt := range tests {
		if got := extractQuant(tt.file); got != tt.want {
			t.Errorf("extractQuant(%q) = %q, want %q", tt.file, got, tt.want)
		}
	}
}

func TestEscapeRepoKeepsTheOwnerSlash(t *testing.T) {
	tests := map[string]string{
		"unsloth/Qwen3-8B-GGUF": "unsloth/Qwen3-8B-GGUF",
		"unsloth/a b":           "unsloth/a%20b",
		"owner/na?me":           "owner/na%3Fme",
	}
	for in, want := range tests {
		if got := escapeRepo(in); got != want {
			t.Errorf("escapeRepo(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRankCandidateModels(t *testing.T) {
	refs := func(ms []CandidateModel) []string {
		out := make([]string, len(ms))
		for i, m := range ms {
			out[i] = m.Repo + ":" + m.Quant + ":" + m.File
		}
		return out
	}

	t.Run("within one repo the preferred quant wins over a larger file", func(t *testing.T) {
		in := []CandidateModel{
			{Repo: "unsloth/X-GGUF", Quant: "Q8_0", SizeBytes: 8 * gib},
			{Repo: "unsloth/X-GGUF", Quant: "Q5_K_M", SizeBytes: 6 * gib},
			{Repo: "unsloth/X-GGUF", Quant: "Q4_K_M", SizeBytes: 5 * gib},
		}
		got := RankCandidateModels(in)
		want := []string{"unsloth/X-GGUF:Q4_K_M:", "unsloth/X-GGUF:Q5_K_M:", "unsloth/X-GGUF:Q8_0:"}
		if strings.Join(refs(got), ",") != strings.Join(want, ",") {
			t.Fatalf("RankCandidateModels = %v, want %v", refs(got), want)
		}
		if in[0].Quant != "Q8_0" {
			t.Errorf("RankCandidateModels reordered its input slice")
		}
	})

	t.Run("across repos the larger model that fits wins", func(t *testing.T) {
		in := []CandidateModel{
			{Repo: "unsloth/small-GGUF", Quant: "Q4_K_M", SizeBytes: 2 * gib},
			{Repo: "unsloth/large-GGUF", Quant: "Q4_K_M", SizeBytes: 9 * gib},
			{Repo: "unsloth/mid-GGUF", Quant: "Q4_K_M", SizeBytes: 5 * gib},
		}
		got := refs(RankCandidateModels(in))
		want := []string{"unsloth/large-GGUF:Q4_K_M:", "unsloth/mid-GGUF:Q4_K_M:", "unsloth/small-GGUF:Q4_K_M:"}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("RankCandidateModels = %v, want %v", got, want)
		}
	})

	t.Run("popularity breaks a quant tie, likes weighted 100x", func(t *testing.T) {
		in := []CandidateModel{
			{Repo: "unsloth/X-GGUF", Quant: "Q4_K_M", File: "downloads", SizeBytes: 5 * gib, Downloads: 150},
			{Repo: "unsloth/X-GGUF", Quant: "Q4_K_M", File: "likes", SizeBytes: 5 * gib, Likes: 2},
		}
		if got := RankCandidateModels(in)[0].File; got != "likes" {
			t.Fatalf("top candidate = %s, want likes (2 likes = 200 > 150 downloads)", got)
		}
	})

	t.Run("size is the last tie-breaker", func(t *testing.T) {
		in := []CandidateModel{
			{Repo: "unsloth/X-GGUF", Quant: "Q4_K_M", File: "smaller", SizeBytes: 4 * gib},
			{Repo: "unsloth/X-GGUF", Quant: "Q4_K_M", File: "larger", SizeBytes: 5 * gib},
		}
		if got := RankCandidateModels(in)[0].File; got != "larger" {
			t.Fatalf("top candidate = %s, want larger", got)
		}
	})

	if got := RankCandidateModels(nil); len(got) != 0 {
		t.Errorf("RankCandidateModels(nil) = %v, want empty", got)
	}
}

func TestEveryFamilyHasALiveUnslothRepo(t *testing.T) {
	for _, fam := range Families() {
		repo := defaultFamilyRepo(fam)
		if !strings.HasPrefix(repo, "unsloth/") || !strings.HasSuffix(repo, "-GGUF") {
			t.Errorf("defaultFamilyRepo(%s) = %q, want an unsloth/*-GGUF repository", fam, repo)
		}
	}
	if got := defaultFamilyRepo(Family("unknown")); got != "" {
		t.Errorf("defaultFamilyRepo(unknown) = %q, want empty", got)
	}
	if got := Family("unknown").DisplayName(); got != "unknown" {
		t.Errorf("DisplayName of an unknown family = %q, want the raw family name", got)
	}
}

func TestModelRefFallsBackToTheFileWithoutAQuant(t *testing.T) {
	withQuant := CandidateModel{Repo: "unsloth/X-GGUF", File: "X-Q4_K_M.gguf", Quant: "Q4_K_M"}
	if got := withQuant.ModelRef(); got != "unsloth/X-GGUF:Q4_K_M" {
		t.Errorf("ModelRef() = %q, want unsloth/X-GGUF:Q4_K_M", got)
	}
	noQuant := CandidateModel{Repo: "unsloth/X-GGUF", File: "X.gguf"}
	if got := noQuant.ModelRef(); got != "unsloth/X-GGUF/X.gguf" {
		t.Errorf("ModelRef() = %q, want unsloth/X-GGUF/X.gguf", got)
	}
}

// hfFake serves a model-info document and a tree document, and records every
// URL it was asked for.
type hfFake struct {
	info     any
	tree     any
	infoErr  error
	treeErr  error
	requests []string
}

func (f *hfFake) fetch(_ context.Context, reqURL string) ([]byte, error) {
	f.requests = append(f.requests, reqURL)
	if strings.Contains(reqURL, "/tree/") {
		if f.treeErr != nil {
			return nil, f.treeErr
		}
		return encodeDoc(f.tree), nil
	}
	if f.infoErr != nil {
		return nil, f.infoErr
	}
	return encodeDoc(f.info), nil
}

func encodeDoc(doc any) []byte {
	if raw, ok := doc.(string); ok {
		return []byte(raw)
	}
	b, err := json.Marshal(doc)
	if err != nil {
		panic(err)
	}
	return b
}

func (f *hfFake) treeRequested() bool {
	for _, u := range f.requests {
		if strings.Contains(u, "/tree/") {
			return true
		}
	}
	return false
}

func chatInfo() hfModelInfo {
	return hfModelInfo{Pipeline: "text-generation", Tags: []string{"gguf"}, Downloads: 1000, Likes: 10}
}

func TestResolveFamilyLiveRejections(t *testing.T) {
	errNetwork := errors.New("network down")

	tests := []struct {
		name       string
		repo       string
		fake       hfFake
		wantErr    string
		wantIs     error
		wantNoTree bool
	}{
		{
			name:       "model info fetch fails",
			repo:       "unsloth/X-GGUF",
			fake:       hfFake{infoErr: errNetwork},
			wantErr:    "fetch model info for unsloth/X-GGUF",
			wantIs:     errNetwork,
			wantNoTree: true,
		},
		{
			name:       "model info is not JSON",
			repo:       "unsloth/X-GGUF",
			fake:       hfFake{info: "<html>"},
			wantErr:    "unmarshal model info for unsloth/X-GGUF",
			wantNoTree: true,
		},
		{
			name:       "repository is neither -GGUF nor tagged gguf",
			repo:       "unsloth/X",
			fake:       hfFake{info: hfModelInfo{Pipeline: "text-generation"}},
			wantErr:    "not GGUF",
			wantNoTree: true,
		},
		{
			name:       "pipeline and tags are not chat",
			repo:       "unsloth/X-GGUF",
			fake:       hfFake{info: hfModelInfo{Pipeline: "feature-extraction", Tags: []string{"gguf"}}},
			wantErr:    "not a compatible chat/text model",
			wantNoTree: true,
		},
		{
			name:       "vision tag rejects a text-generation repo",
			repo:       "unsloth/X-GGUF",
			fake:       hfFake{info: hfModelInfo{Pipeline: "text-generation", Tags: []string{"gguf", "vision"}}},
			wantErr:    "not a compatible chat/text model",
			wantNoTree: true,
		},
		{
			name:       "image-to-text tag rejects a chat-tagged repo",
			repo:       "unsloth/X-GGUF",
			fake:       hfFake{info: hfModelInfo{Tags: []string{"gguf", "chat", "image-to-text"}}},
			wantErr:    "not a compatible chat/text model",
			wantNoTree: true,
		},
		{
			name:    "tree fetch fails",
			repo:    "unsloth/X-GGUF",
			fake:    hfFake{info: chatInfo(), treeErr: errNetwork},
			wantErr: "fetch tree for unsloth/X-GGUF",
			wantIs:  errNetwork,
		},
		{
			name:    "tree is not JSON",
			repo:    "unsloth/X-GGUF",
			fake:    hfFake{info: chatInfo(), tree: "{}"},
			wantErr: "unmarshal tree for unsloth/X-GGUF",
		},
		{
			name: "tree has no pullable quant",
			repo: "unsloth/X-GGUF",
			fake: hfFake{info: chatInfo(), tree: []hfTreeItem{
				{Type: "file", Path: "README.md", Size: 1},
				{Type: "file", Path: "X-BF16.gguf", Size: 16 * gib},
				{Type: "file", Path: "mmproj-Q8_0.gguf", Size: gib},
			}},
			wantErr: "no .gguf files found",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := tt.fake
			got, err := ResolveFamilyLive(context.Background(), FamilyQwen, tt.repo, fake.fetch)
			if err == nil {
				t.Fatalf("ResolveFamilyLive succeeded with %d candidates, want error containing %q", len(got), tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want it to contain %q", err, tt.wantErr)
			}
			if tt.wantIs != nil && !errors.Is(err, tt.wantIs) {
				t.Errorf("error %q does not wrap %v", err, tt.wantIs)
			}
			if tt.wantNoTree && fake.treeRequested() {
				t.Errorf("tree was fetched after the model info was rejected: %v", fake.requests)
			}
		})
	}
}

func TestResolveFamilyLiveAcceptsAGGUFTagOrConversationalPipeline(t *testing.T) {
	tree := []hfTreeItem{{Type: "file", Path: "X-Q4_K_M.gguf", Size: 5 * gib}}
	cases := map[string]struct {
		repo string
		info hfModelInfo
	}{
		"gguf tag on a repo without the -GGUF suffix": {"unsloth/X", hfModelInfo{Pipeline: "text-generation", Tags: []string{"GGUF"}}},
		"conversational pipeline with no chat tag":    {"unsloth/X-GGUF", hfModelInfo{Pipeline: "conversational"}},
		"chat tag with no text pipeline":              {"unsloth/X-GGUF", hfModelInfo{Tags: []string{"Chat"}}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			fake := hfFake{info: tc.info, tree: tree}
			got, err := ResolveFamilyLive(context.Background(), FamilyQwen, tc.repo, fake.fetch)
			if err != nil {
				t.Fatalf("ResolveFamilyLive: %v", err)
			}
			if len(got) != 1 || got[0].Quant != "Q4_K_M" {
				t.Fatalf("candidates = %+v, want one Q4_K_M", got)
			}
		})
	}
}

func TestResolveFamilyLiveKeepsOnlyStandalonePullableQuants(t *testing.T) {
	info := chatInfo()
	info.Downloads, info.Likes = 4242, 17
	fake := hfFake{info: info, tree: []hfTreeItem{
		{Type: "directory", Path: "BF16"},
		{Type: "file", Path: "BF16/Qwen3-8B-BF16-00001-of-00002.gguf", Size: 9 * gib},
		{Type: "file", Path: "Q8_0/Qwen3-8B-Q8_0-00001-of-00002.gguf", Size: 4 * gib},
		{Type: "file", Path: "mmproj-Q8_0.gguf", Size: gib},
		{Type: "file", Path: "Qwen3-8B-F16.gguf", Size: 16 * gib},
		{Type: "file", Path: "README.md", Size: 100},
		{Type: "file", Path: "Qwen3-8B-Q4_K_M.gguf", Size: 5 * gib},
		{Type: "file", Path: "Qwen3-8B-Q8_0.gguf", Size: 8 * gib},
	}}

	got, err := ResolveFamilyLive(context.Background(), FamilyQwen, "unsloth/Qwen3-8B-GGUF", fake.fetch)
	if err != nil {
		t.Fatalf("ResolveFamilyLive: %v", err)
	}
	wantFiles := []string{"Qwen3-8B-Q4_K_M.gguf", "Qwen3-8B-Q8_0.gguf"}
	if len(got) != len(wantFiles) {
		t.Fatalf("got %d candidates %+v, want %v", len(got), got, wantFiles)
	}
	for i, c := range got {
		if c.File != wantFiles[i] {
			t.Errorf("candidate[%d].File = %q, want %q", i, c.File, wantFiles[i])
		}
		if c.Family != FamilyQwen || c.Repo != "unsloth/Qwen3-8B-GGUF" {
			t.Errorf("candidate[%d] = %s %s, want qwen unsloth/Qwen3-8B-GGUF", i, c.Family, c.Repo)
		}
		if !c.IsChat || !c.IsGGUF || c.IsMultimodal || c.Cached {
			t.Errorf("candidate[%d] flags = chat %v gguf %v multimodal %v cached %v", i, c.IsChat, c.IsGGUF, c.IsMultimodal, c.Cached)
		}
		if c.Downloads != 4242 || c.Likes != 17 {
			t.Errorf("candidate[%d] popularity = %d/%d, want 4242/17 from the model info", i, c.Downloads, c.Likes)
		}
	}

	wantURLs := []string{
		"https://huggingface.co/api/models/unsloth/Qwen3-8B-GGUF",
		"https://huggingface.co/api/models/unsloth/Qwen3-8B-GGUF/tree/main?recursive=true",
	}
	if strings.Join(fake.requests, " ") != strings.Join(wantURLs, " ") {
		t.Errorf("requested %v, want %v", fake.requests, wantURLs)
	}
}

func TestResolveCandidateLivePath(t *testing.T) {
	liveTree := []hfTreeItem{
		{Type: "file", Path: "Qwen3-8B-Q3_K_M.gguf", Size: 4_000_000_000},
		{Type: "file", Path: "Qwen3-8B-Q4_K_M.gguf", Size: 5_000_000_000},
		{Type: "file", Path: "Qwen3-8B-Q8_0.gguf", Size: 8_700_000_000},
	}

	t.Run("plenty of memory picks the preferred quant from the family's live repo", func(t *testing.T) {
		fake := hfFake{info: chatInfo(), tree: liveTree}
		got, err := ResolveCandidate(context.Background(), FamilyQwen, 16*gib, fake.fetch)
		if err != nil {
			t.Fatalf("ResolveCandidate: %v", err)
		}
		if got.ModelRef() != "unsloth/Qwen3-8B-GGUF:Q4_K_M" || got.Cached {
			t.Fatalf("got %s (cached %v), want live unsloth/Qwen3-8B-GGUF:Q4_K_M", got.ModelRef(), got.Cached)
		}
		if len(fake.requests) == 0 || !strings.HasPrefix(fake.requests[0], "https://huggingface.co/api/models/unsloth/Qwen3-8B-GGUF") {
			t.Errorf("first request = %v, want the Qwen family repo", fake.requests)
		}
	})

	t.Run("tight memory drops to a smaller quant of the live repo", func(t *testing.T) {
		// 6.5 GiB: Q4_K_M needs 5.0 GB + 2 GiB margin and does not fit; Q3_K_M does.
		fake := hfFake{info: chatInfo(), tree: liveTree}
		got, err := ResolveCandidate(context.Background(), FamilyQwen, 13*gib/2, fake.fetch)
		if err != nil {
			t.Fatalf("ResolveCandidate: %v", err)
		}
		if got.ModelRef() != "unsloth/Qwen3-8B-GGUF:Q3_K_M" || got.Cached {
			t.Fatalf("got %s (cached %v), want live unsloth/Qwen3-8B-GGUF:Q3_K_M", got.ModelRef(), got.Cached)
		}
	})

	t.Run("nothing live fits so the offline catalog answers", func(t *testing.T) {
		fake := hfFake{info: chatInfo(), tree: []hfTreeItem{{Type: "file", Path: "Qwen3-8B-Q8_0.gguf", Size: 8_700_000_000}}}
		got, err := ResolveCandidate(context.Background(), FamilyQwen, 8*gib, fake.fetch)
		if err != nil {
			t.Fatalf("ResolveCandidate: %v", err)
		}
		if !got.Cached || got.Repo != "unsloth/Qwen3-8B-GGUF" || got.Quant != "Q4_K_M" {
			t.Fatalf("got %s (cached %v), want offline unsloth/Qwen3-8B-GGUF:Q4_K_M", got.ModelRef(), got.Cached)
		}
	})

	t.Run("a live lookup failure falls back to the largest offline model that fits", func(t *testing.T) {
		fake := hfFake{infoErr: errors.New("offline")}
		got, err := ResolveCandidate(context.Background(), FamilyQwen, 16*gib, fake.fetch)
		if err != nil {
			t.Fatalf("ResolveCandidate: %v", err)
		}
		if !got.Cached || got.Repo != "unsloth/Qwen3-14B-GGUF" {
			t.Fatalf("got %s (cached %v), want offline unsloth/Qwen3-14B-GGUF", got.ModelRef(), got.Cached)
		}
	})

	t.Run("a live multimodal repo is not used", func(t *testing.T) {
		fake := hfFake{info: hfModelInfo{Pipeline: "text-generation", Tags: []string{"gguf", "multimodal"}}, tree: liveTree}
		got, err := ResolveCandidate(context.Background(), FamilyQwen, 16*gib, fake.fetch)
		if err != nil {
			t.Fatalf("ResolveCandidate: %v", err)
		}
		if !got.Cached {
			t.Fatalf("got live %s, want the offline fallback", got.ModelRef())
		}
	})

	t.Run("every family queries its own repo", func(t *testing.T) {
		for _, fam := range Families() {
			fake := hfFake{infoErr: errors.New("offline")}
			got, err := ResolveCandidate(context.Background(), fam, 32*gib, fake.fetch)
			if err != nil {
				t.Fatalf("ResolveCandidate(%s): %v", fam, err)
			}
			if got.Family != fam {
				t.Errorf("ResolveCandidate(%s) returned family %s", fam, got.Family)
			}
			want := "https://huggingface.co/api/models/" + defaultFamilyRepo(fam)
			if len(fake.requests) != 1 || fake.requests[0] != want {
				t.Errorf("ResolveCandidate(%s) requested %v, want [%s]", fam, fake.requests, want)
			}
		}
	})

	t.Run("an unknown family skips the live lookup and fails", func(t *testing.T) {
		fake := hfFake{info: chatInfo(), tree: liveTree}
		if _, err := ResolveCandidate(context.Background(), Family("unknown"), 32*gib, fake.fetch); err == nil {
			t.Fatal("ResolveCandidate(unknown) succeeded, want error")
		}
		if len(fake.requests) != 0 {
			t.Errorf("unknown family made live requests: %v", fake.requests)
		}
	})
}

func TestDefaultFetchSendsItsUserAgentAndRejectsNon200(t *testing.T) {
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		if r.URL.Path == "/missing" {
			http.Error(w, "nope", http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	body, err := DefaultFetch(context.Background(), srv.URL+"/ok")
	if err != nil {
		t.Fatalf("DefaultFetch: %v", err)
	}
	if string(body) != `{"ok":true}` {
		t.Errorf("body = %q", body)
	}
	if gotUA != "BluefinModelResolver/1.0" {
		t.Errorf("User-Agent = %q, want BluefinModelResolver/1.0", gotUA)
	}

	if _, err := DefaultFetch(context.Background(), srv.URL+"/missing"); err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("DefaultFetch on 404 = %v, want an HTTP 404 error", err)
	}
}

func TestFetchNodeStatusRejectsNon200AndMalformedBodies(t *testing.T) {
	tests := map[string]http.HandlerFunc{
		"non-200": func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "down", http.StatusServiceUnavailable)
		},
		"malformed": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"memory":`))
		},
	}
	for name, h := range tests {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(h)
			defer srv.Close()
			orig := nodeURL
			nodeURL = srv.URL
			defer func() { nodeURL = orig }()

			if _, err := FetchNodeStatus(context.Background()); err == nil {
				t.Fatal("FetchNodeStatus succeeded, want error")
			}
		})
	}
}
