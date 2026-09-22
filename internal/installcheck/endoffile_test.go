package installcheck

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// .pre-commit-config.yaml runs pre-commit-hooks' end-of-file-fixer, and
// .editorconfig sets insert_final_newline = true for every file in the tree.
// Both are rewriting hooks: `pre-commit run --all-files` does not report a
// file that ends without a newline and stop, it appends the newline and then
// fails on the resulting diff. A clean checkout therefore could not pass
// repository preflight — the run left three tracked files modified
// (issue #90): the two large application SVGs under data/icons/hicolor/
// scalable/apps/ and distrobox.ini, all of which ended at their last
// character.
//
// Normalizing those three files fixes today's tree but nothing stops the next
// design-tool export or hand-edited config from reintroducing the problem,
// and pre-commit is not part of `make ci` or the Tests workflow, so nothing
// enforced in CI would notice. This gate holds that line: it applies
// end-of-file-fixer's own rule to every tracked file the hook would select,
// so a file that would be rewritten fails here instead of in a contributor's
// working tree.
//
// The inventory comes from `git ls-files`, which is exactly the file list
// `pre-commit run --all-files` works from, rather than a filesystem walk that
// would also pick up build output and other untracked artifacts.

// textClassificationSampleSize is the number of leading bytes identify (the
// library pre-commit uses to resolve `types: [text]`) inspects when deciding
// whether a file is text or binary. Mirroring the sample size matters: a file
// whose first kilobyte is text is a text file to the hook regardless of what
// follows.
const textClassificationSampleSize = 1024

// isTextByte reports whether b is one of the byte values identify counts as
// text. The set is libmagic's, which identify copies verbatim: the whitespace
// and escape control codes that appear in ordinary text, printable ASCII
// (0x20 through 0x7E, so DEL is excluded), and every byte from 0x80 up, which
// covers UTF-8 lead and continuation bytes.
func isTextByte(b byte) bool {
	switch {
	case b >= 7 && b <= 13, b == 0x1b:
		return true
	case b >= 0x20 && b < 0x7f:
		return true
	case b >= 0x80:
		return true
	default:
		return false
	}
}

// looksLikeText applies identify's text/binary heuristic to a file's
// contents: one byte outside the text set anywhere in the leading sample
// makes the whole file binary, which is why the PNG screenshots under
// docs/screenshots/ are not offenders despite ending mid-chunk. An empty file
// is text, matching identify, though end-of-file-fixer leaves empty files
// alone in any case.
//
// identify resolves many paths by extension before it ever reads bytes
// (`.svg` and `.ini` are text there, `.png` is binary), and this content rule
// agrees with the extension table on every path in this tree. It is used
// alone because it needs no table to maintain and errs toward calling an
// oddly encoded file binary, which loses a gate rather than inventing a
// failure the hook would not produce.
func looksLikeText(data []byte) bool {
	sample := data
	if len(sample) > textClassificationSampleSize {
		sample = sample[:textClassificationSampleSize]
	}

	for _, b := range sample {
		if !isTextByte(b) {
			return false
		}
	}
	return true
}

// trackedFiles returns every path `git ls-files` reports, repo-relative and
// NUL-separated so a path containing whitespace or a newline survives intact.
// Paths recorded in the index but absent from the working tree (a deletion
// that is not staged yet) are dropped by the caller, not here.
func trackedFiles(t *testing.T) []string {
	t.Helper()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed; skipping end-of-file normalization check")
	}

	cmd := exec.Command("git", "ls-files", "-z")
	cmd.Dir = RepoRoot()

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("git ls-files -z failed: %v\nstderr:\n%s", err, stderr.String())
	}

	var paths []string
	for _, entry := range strings.Split(stdout.String(), "\x00") {
		if entry != "" {
			paths = append(paths, entry)
		}
	}
	if len(paths) == 0 {
		t.Fatal("git ls-files -z returned no paths; the gate would pass vacuously")
	}
	return paths
}

// TestTrackedTextFilesEndWithExactlyOneNewline is the gate proper.
// end-of-file-fixer's rule is that a file is either empty or ends with
// exactly one newline, so both an absent trailing newline and a run of blank
// lines at EOF are rewrites, and both fail here.
func TestTrackedTextFilesEndWithExactlyOneNewline(t *testing.T) {
	var missingNewline, extraNewlines []string

	for _, relative := range trackedFiles(t) {
		path := filepath.Join(RepoRoot(), relative)

		info, err := os.Lstat(path)
		if err != nil {
			if os.IsNotExist(err) {
				// Tracked but not present in the working tree; nothing to read.
				continue
			}
			t.Fatalf("stat %s: %v", relative, err)
		}
		// A symlink's blob is its target path, and a submodule (gitlink) is a
		// directory here. end-of-file-fixer sees neither as a regular text file.
		if !info.Mode().IsRegular() {
			continue
		}

		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading %s: %v", relative, err)
		}
		if len(data) == 0 || !looksLikeText(data) {
			continue
		}

		if data[len(data)-1] != '\n' {
			missingNewline = append(missingNewline, relative)
			continue
		}
		if trimmed := bytes.TrimRight(data, "\n"); len(data)-len(trimmed) > 1 {
			extraNewlines = append(extraNewlines, relative)
		}
	}

	sort.Strings(missingNewline)
	sort.Strings(extraNewlines)

	if len(missingNewline) > 0 {
		t.Errorf("tracked text files do not end with a newline, so end-of-file-fixer "+
			"would rewrite them and `pre-commit run --all-files` would fail on the "+
			"resulting diff:\n  %s", strings.Join(missingNewline, "\n  "))
	}
	if len(extraNewlines) > 0 {
		t.Errorf("tracked text files end with more than one newline, so "+
			"end-of-file-fixer would trim them and `pre-commit run --all-files` "+
			"would fail on the resulting diff:\n  %s", strings.Join(extraNewlines, "\n  "))
	}
}

// TestLooksLikeTextMatchesIdentifyHeuristic pins the classifier itself. The
// gate above is only as good as its text/binary split: were looksLikeText to
// call everything binary it would pass over an entire tree of offenders
// without reporting one.
func TestLooksLikeTextMatchesIdentifyHeuristic(t *testing.T) {
	highBytes := bytes.Repeat([]byte{0xc3, 0xa9}, 64) // "é" repeated, valid UTF-8

	cases := []struct {
		name string
		data []byte
		text bool
	}{
		{name: "empty", data: nil, text: true},
		{name: "ascii", data: []byte("[chairlift]\nroot=false\n"), text: true},
		{name: "utf-8 above ascii", data: highBytes, text: true},
		{name: "embedded NUL", data: []byte("plain text\x00more"), text: false},
		{
			name: "NUL past the sample window",
			data: append(bytes.Repeat([]byte("x"), textClassificationSampleSize), 0),
			text: true,
		},
		{name: "one stray control byte", data: []byte("mostly text\x01here"), text: false},
		{name: "DEL is not text", data: []byte("mostly text\x7fhere"), text: false},
		{name: "png header", data: []byte("\x89PNG\r\n\x1a\n"), text: false},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := looksLikeText(testCase.data); got != testCase.text {
				t.Errorf("looksLikeText(%q...) = %v, want %v", firstBytes(testCase.data), got, testCase.text)
			}
		})
	}
}

// firstBytes shortens a sample for failure messages so a kilobyte-long case
// does not dump a kilobyte into the test log.
func firstBytes(data []byte) []byte {
	if len(data) > 16 {
		return data[:16]
	}
	return data
}
