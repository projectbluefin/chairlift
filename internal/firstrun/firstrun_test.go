package firstrun

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/dryrun"
)

func TestMemoryStoreGetAndSet(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore(DispositionNotAddressed)

	disp, err := store.GetDisposition(ctx)
	if err != nil {
		t.Fatalf("GetDisposition: %v", err)
	}
	if disp != DispositionNotAddressed {
		t.Errorf("initial disp = %v, want %v", disp, DispositionNotAddressed)
	}

	if err := store.SetDisposition(ctx, DispositionSkipped); err != nil {
		t.Fatalf("SetDisposition: %v", err)
	}

	disp, err = store.GetDisposition(ctx)
	if err != nil {
		t.Fatalf("GetDisposition after set: %v", err)
	}
	if disp != DispositionSkipped {
		t.Errorf("disp = %v, want %v", disp, DispositionSkipped)
	}
}

// TestGSettingsStoreDryRunSpawnsNothing holds the dry-run contract where it
// matters: not that SetDisposition returns nil, but that no gsettings process
// is started. An earlier version of this test asserted only the nil error,
// which a regression that wrote for real would also have satisfied.
func TestGSettingsStoreDryRunSpawnsNothing(t *testing.T) {
	dryrun.Set(true)
	defer dryrun.Set(false)

	original := runCommand
	t.Cleanup(func() { runCommand = original })
	runCommand = func(_ context.Context, name string, args ...string) (string, error) {
		t.Fatalf("dry-run SetDisposition spawned %s %v", name, args)
		return "", nil
	}

	store := NewGSettingsStore()
	for _, disp := range []Disposition{DispositionSkipped, DispositionCompleted} {
		if err := store.SetDisposition(context.Background(), disp); err != nil {
			t.Fatalf("SetDisposition(%s) in dry-run mode failed: %v", disp, err)
		}
	}
}

// TestGSettingsStoreWritesTheKeysEachDispositionOwns pins the live write
// path: a skip writes the disposition alone, a completion also records the
// version that finished setup, and both values are quoted as GVariant
// strings so gsettings parses them.
func TestGSettingsStoreWritesTheKeysEachDispositionOwns(t *testing.T) {
	original := runCommand
	t.Cleanup(func() { runCommand = original })

	var calls [][]string
	runCommand = func(_ context.Context, name string, args ...string) (string, error) {
		if name != "gsettings" {
			t.Errorf("spawned %q, want gsettings", name)
		}
		calls = append(calls, args)
		return "", nil
	}
	store := NewGSettingsStore()

	if err := store.SetDisposition(context.Background(), DispositionSkipped); err != nil {
		t.Fatalf("SetDisposition(skipped): %v", err)
	}
	want := [][]string{{"set", SchemaID, KeyDisposition, `"skipped"`}}
	if !equalCalls(calls, want) {
		t.Fatalf("skipped wrote %v, want %v", calls, want)
	}

	calls = nil
	if err := store.SetDisposition(context.Background(), DispositionCompleted); err != nil {
		t.Fatalf("SetDisposition(completed): %v", err)
	}
	if len(calls) != 2 || !equalCalls(calls[:1], [][]string{{"set", SchemaID, KeyDisposition, `"completed"`}}) {
		t.Fatalf("completed wrote %v, want the disposition then the version", calls)
	}
	if got := calls[1]; len(got) != 4 || got[0] != "set" || got[1] != SchemaID || got[2] != KeyCompletedVersion || got[3] == `""` {
		t.Fatalf("completed-version write = %v, want set %s %s <quoted version>", got, SchemaID, KeyCompletedVersion)
	}
}

func equalCalls(got, want [][]string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if strings.Join(got[i], "\x00") != strings.Join(want[i], "\x00") {
			return false
		}
	}
	return true
}

// TestGSettingsStoreWriteFailuresAreClassified keeps a missing schema
// distinguishable from any other write failure: the caller logs the first as
// "reinstall so the schema is compiled" and the second as a bare error.
func TestGSettingsStoreWriteFailuresAreClassified(t *testing.T) {
	original := runCommand
	t.Cleanup(func() { runCommand = original })

	runCommand = func(context.Context, string, ...string) (string, error) {
		return "No such schema “" + SchemaID + "”", errors.New("exit status 1")
	}
	if err := NewGSettingsStore().SetDisposition(context.Background(), DispositionSkipped); !errors.Is(err, ErrSchemaMissing) {
		t.Fatalf("missing schema write error = %v, want ErrSchemaMissing", err)
	}

	boom := errors.New("dconf is not running")
	runCommand = func(context.Context, string, ...string) (string, error) {
		return "", boom
	}
	err := NewGSettingsStore().SetDisposition(context.Background(), DispositionSkipped)
	if !errors.Is(err, boom) || errors.Is(err, ErrSchemaMissing) {
		t.Fatalf("other write error = %v, want it to wrap %v and not ErrSchemaMissing", err, boom)
	}
}

// TestGetDispositionParsesTheSchemaListing covers readAll's one input: the
// `gsettings list-recursively` listing, which may carry other schemas' lines,
// single- or double-quoted values, and values a newer build wrote.
func TestGetDispositionParsesTheSchemaListing(t *testing.T) {
	original := runCommand
	t.Cleanup(func() { runCommand = original })

	cases := []struct {
		name    string
		listing string
		want    Disposition
		wantErr error
	}{
		{
			name:    "single-quoted skip",
			listing: SchemaID + " " + KeyCompletedVersion + " ''\n" + SchemaID + " " + KeyDisposition + " 'skipped'\n",
			want:    DispositionSkipped,
		},
		{
			name:    "double-quoted completion",
			listing: SchemaID + " " + KeyDisposition + " \"completed\"\n" + SchemaID + " " + KeyCompletedVersion + " \"v26.09.0-alpha.2\"\n",
			want:    DispositionCompleted,
		},
		{
			name:    "another schema's lines are ignored",
			listing: "io.projectbluefin.chairlift.livery disposition 'completed'\n" + SchemaID + " " + KeyDisposition + " 'not-addressed'\n",
			want:    DispositionNotAddressed,
		},
		{
			name:    "a version without a disposition is an earlier build's completion",
			listing: SchemaID + " " + KeyCompletedVersion + " 'v0.12.2'\n" + SchemaID + " " + KeyDisposition + " ''\n",
			want:    DispositionCompleted,
		},
		{
			name:    "a value this build does not know is not addressed",
			listing: SchemaID + " " + KeyDisposition + " 'postponed'\n" + SchemaID + " " + KeyCompletedVersion + " ''\n",
			want:    DispositionNotAddressed,
		},
		{
			name:    "an empty listing means the schema is not installed",
			listing: "",
			want:    DispositionNotAddressed,
			wantErr: ErrSchemaMissing,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runCommand = func(_ context.Context, _ string, args ...string) (string, error) {
				if len(args) != 2 || args[0] != "list-recursively" || args[1] != SchemaID {
					t.Errorf("unexpected gsettings invocation %v", args)
				}
				return tc.listing, nil
			}
			got, err := NewGSettingsStore().GetDisposition(context.Background())
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("GetDisposition error = %v, want %v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Fatalf("GetDisposition = %v, want %v", got, tc.want)
			}
		})
	}
}

type failingStore struct {
	readErr  error
	writeErr error
	written  []Disposition
}

func (s *failingStore) GetDisposition(context.Context) (Disposition, error) {
	return DispositionNotAddressed, s.readErr
}

func (s *failingStore) SetDisposition(_ context.Context, d Disposition) error {
	if s.writeErr != nil {
		return s.writeErr
	}
	s.written = append(s.written, d)
	return nil
}

// TestRecordSkipPreservesACompletionAndWritesOtherwise is the read-decide-
// write behind Get Moving and a plain dismissal: a fresh account records a
// skip, a skipped account is left alone, and a completed account is never
// demoted — which is why a failed read refuses the write rather than
// guessing the state.
func TestRecordSkipPreservesACompletionAndWritesOtherwise(t *testing.T) {
	cases := []struct {
		name      string
		initial   Disposition
		want      Disposition
		wantWrote bool
	}{
		{"not addressed records a skip", DispositionNotAddressed, DispositionSkipped, true},
		{"already skipped writes nothing", DispositionSkipped, DispositionSkipped, false},
		{"completed is preserved", DispositionCompleted, DispositionCompleted, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := NewMemoryStore(tc.initial)
			got, wrote, err := RecordSkip(context.Background(), store)
			if err != nil {
				t.Fatalf("RecordSkip: %v", err)
			}
			if got != tc.want || wrote != tc.wantWrote {
				t.Fatalf("RecordSkip = (%v, %v), want (%v, %v)", got, wrote, tc.want, tc.wantWrote)
			}
			if after, _ := store.GetDisposition(context.Background()); after != tc.want {
				t.Fatalf("store holds %v after RecordSkip, want %v", after, tc.want)
			}
		})
	}

	t.Run("a read failure refuses the write", func(t *testing.T) {
		// With the current state unknown, writing "skipped" could overwrite
		// a completion; the read error is returned and nothing is written.
		boom := errors.New("dconf is not running")
		store := &failingStore{readErr: boom}
		got, wrote, err := RecordSkip(context.Background(), store)
		if !errors.Is(err, boom) || wrote || got != DispositionNotAddressed {
			t.Fatalf("RecordSkip = (%v, %v, %v), want (not-addressed, false, %v)", got, wrote, err, boom)
		}
		if len(store.written) != 0 {
			t.Fatalf("written = %v, want nothing", store.written)
		}
	})

	t.Run("a write failure is returned", func(t *testing.T) {
		boom := errors.New("no such schema")
		store := &failingStore{writeErr: boom}
		if _, wrote, err := RecordSkip(context.Background(), store); !errors.Is(err, boom) || wrote {
			t.Fatalf("RecordSkip = (wrote=%v, err=%v), want (false, %v)", wrote, err, boom)
		}
	})
}

func TestEmbeddedAssetsAreNonEmpty(t *testing.T) {
	wordmarkDark, err := Wordmark(true)
	if err != nil {
		t.Fatalf("Wordmark(dark): %v", err)
	}
	if len(wordmarkDark) == 0 {
		t.Error("Wordmark(dark) returned empty byte slice")
	}
	if !strings.Contains(string(wordmarkDark), "<svg") {
		t.Error("Wordmark(dark) does not contain SVG root element")
	}
	wordmarkLight, err := Wordmark(false)
	if err != nil {
		t.Fatalf("Wordmark(light): %v", err)
	}
	if len(wordmarkLight) == 0 {
		t.Error("Wordmark(light) returned empty byte slice")
	}
	if !strings.Contains(string(wordmarkLight), "<svg") {
		t.Error("Wordmark(light) does not contain SVG root element")
	}
}
func TestAssetPathResolvesExistingFile(t *testing.T) {
	path, err := AssetPath(AssetWordmarkDark)
	if err != nil {
		t.Fatalf("AssetPath: %v", err)
	}
	if path == "" {
		t.Fatal("AssetPath returned empty string")
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if info.Size() == 0 {
		t.Errorf("asset file %s is empty", path)
	}
}

func TestGetDispositionReportsAMissingSchema(t *testing.T) {
	original := runCommand
	defer func() { runCommand = original }()

	calls := 0
	runCommand = func(ctx context.Context, name string, args ...string) (string, error) {
		calls++
		return "No such schema “" + SchemaID + "”", errors.New("exit status 1")
	}

	disp, err := NewGSettingsStore().GetDisposition(context.Background())
	if !errors.Is(err, ErrSchemaMissing) {
		t.Errorf("err = %v, want ErrSchemaMissing", err)
	}
	if disp != DispositionNotAddressed {
		t.Errorf("disp = %v, want DispositionNotAddressed", disp)
	}
	if calls != 1 {
		t.Errorf("gsettings spawned %d times, want 1", calls)
	}
}

func TestGetDispositionReadsEveryKeyInOneSpawn(t *testing.T) {
	original := runCommand
	defer func() { runCommand = original }()

	calls := 0
	runCommand = func(ctx context.Context, name string, args ...string) (string, error) {
		calls++
		if len(args) < 2 || args[0] != "list-recursively" {
			t.Errorf("unexpected gsettings invocation %v", args)
		}
		return SchemaID + " " + KeyCompletedVersion + " '1.2.3'\n" +
			SchemaID + " " + KeyDisposition + " ''\n", nil
	}

	disp, err := NewGSettingsStore().GetDisposition(context.Background())
	if err != nil {
		t.Fatalf("GetDisposition: %v", err)
	}
	if disp != DispositionCompleted {
		t.Errorf("disp = %v, want DispositionCompleted (completed-version set by an earlier build)", disp)
	}
	if calls != 1 {
		t.Errorf("gsettings spawned %d times, want 1", calls)
	}
}

// TestSkipPreservingKeepsARecordedCompletion holds the re-entry case: the
// assistant reopens from the menu and from --setup after setup finished, and
// GetDisposition prefers the disposition key over the completed version, so
// writing skipped there would regress a finished setup permanently.
func TestSkipPreservingKeepsARecordedCompletion(t *testing.T) {
	for _, tc := range []struct {
		current Disposition
		want    Disposition
	}{
		{DispositionNotAddressed, DispositionSkipped},
		{DispositionSkipped, DispositionSkipped},
		{DispositionCompleted, DispositionCompleted},
	} {
		if got := SkipPreserving(tc.current); got != tc.want {
			t.Errorf("SkipPreserving(%q) = %q, want %q", tc.current, got, tc.want)
		}
	}
}

// TestCleanupAssetsRemovesTheExtractionDirectory keeps the extraction
// directory from outliving the process that created it.
func TestCleanupAssetsRemovesTheExtractionDirectory(t *testing.T) {
	path, err := AssetPath(AssetWordmarkDark)
	if err != nil {
		t.Fatalf("AssetPath: %v", err)
	}
	dir := filepath.Dir(path)

	if err := CleanupAssets(); err != nil {
		t.Fatalf("CleanupAssets: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("asset directory %s still present after cleanup (err = %v)", dir, err)
	}

	// A second call has nothing to remove and must not report failure.
	if err := CleanupAssets(); err != nil {
		t.Errorf("second CleanupAssets: %v", err)
	}

	// Cleanup does not disable extraction: a later presentation re-extracts.
	again, err := AssetPath(AssetWordmarkDark)
	if err != nil {
		t.Fatalf("AssetPath after cleanup: %v", err)
	}
	if _, err := os.Stat(again); err != nil {
		t.Errorf("stat re-extracted asset %s: %v", again, err)
	}
	t.Cleanup(func() { _ = CleanupAssets() })
}
