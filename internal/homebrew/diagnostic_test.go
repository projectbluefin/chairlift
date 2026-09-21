package homebrew

import (
	"strings"
	"testing"
)

// bundleFailureStdout and bundleFailureStderr are the two streams a failing
// `brew bundle install` produces for the flatpak-only Brewfile in issue #140.
// Brew replays the flatpak subprocess's own output on stdout and prints its
// restatements on stderr, so the cause is on stdout and only progress precedes
// it — which is exactly the fragment the toast used to show.
const (
	bundleFailureStdout = `Installing io.podman_desktop.PodmanDesktop
error: Remote "flathub" not found`
	bundleFailureStderr = `Error: Installing io.podman_desktop.PodmanDesktop has failed!
Error: ` + "`brew bundle`" + ` failed! 1 Brewfile dependency failed to install`
)

func TestSummarizeDiagnosticPrefersTheInstallerErrorOverProgress(t *testing.T) {
	got := summarizeDiagnostic(joinCommandStreams(bundleFailureStdout, bundleFailureStderr))

	want := `error: Remote "flathub" not found`
	if got != want {
		t.Errorf("summarizeDiagnostic = %q, want %q", got, want)
	}
}

// Keeping stderr alone — what the runner did before issue #140 whenever stderr
// had any content — leaves only brew's restatement, which never names a cause.
func TestSummarizeDiagnosticOnStderrAloneLosesTheCause(t *testing.T) {
	got := summarizeDiagnostic(bundleFailureStderr)

	if strings.Contains(got, "flathub") {
		t.Fatalf("summary = %q; the fixture is meant to omit the cause", got)
	}
	if got != "Error: Installing io.podman_desktop.PodmanDesktop has failed!" {
		t.Errorf("summarizeDiagnostic = %q, want brew's first restatement", got)
	}
}

func TestSummarizeDiagnosticPrefersTheFirstErrorLine(t *testing.T) {
	output := strings.Join([]string{
		"==> Downloading https://example.invalid/bottle",
		"fatal: unable to access 'https://example.invalid/': Could not resolve host",
		"Error: Failure while executing; `git clone` exited with 128.",
	}, "\n")

	got := summarizeDiagnostic(output)

	want := "fatal: unable to access 'https://example.invalid/': Could not resolve host"
	if got != want {
		t.Errorf("summarizeDiagnostic = %q, want the first error line %q", got, want)
	}
}

func TestSummarizeDiagnosticFallsBackToAFailureLine(t *testing.T) {
	output := strings.Join([]string{
		"Installing mas-cli",
		"Installing mas-cli has failed!",
		"Homebrew Bundle failed! 1 Brewfile dependency failed to install.",
	}, "\n")

	got := summarizeDiagnostic(output)

	want := "Installing mas-cli has failed!"
	if got != want {
		t.Errorf("summarizeDiagnostic = %q, want %q", got, want)
	}
}

func TestSummarizeDiagnosticFallsBackToTheLastLine(t *testing.T) {
	output := "first line\nsecond line\nlast line\n\n"

	if got := summarizeDiagnostic(output); got != "last line" {
		t.Errorf("summarizeDiagnostic = %q, want %q", got, "last line")
	}
}

func TestSummarizeDiagnosticReturnsEmptyForBlankOutput(t *testing.T) {
	for _, output := range []string{"", "\n", "   \n\t\n"} {
		if got := summarizeDiagnostic(output); got != "" {
			t.Errorf("summarizeDiagnostic(%q) = %q, want an empty summary", output, got)
		}
	}
}

// Download progress rewrites one terminal line with carriage returns. Only the
// final state of such a line is meaningful, and a whole line of rewrites must
// not be mistaken for the last line of real output.
func TestSummarizeDiagnosticKeepsOnlyTheFinalStateOfARewrittenLine(t *testing.T) {
	output := "##O#- #\r####  10.0%\r#####  99.9%\nError: donor bottle is corrupt"

	if got := summarizeDiagnostic(output); got != "Error: donor bottle is corrupt" {
		t.Errorf("summarizeDiagnostic = %q, want the error line", got)
	}

	rewritesOnly := "####  10.0%\r#####  99.9%"
	if got := summarizeDiagnostic(rewritesOnly); got != "#####  99.9%" {
		t.Errorf("summarizeDiagnostic = %q, want the final rewrite state", got)
	}
}

// A CRLF line ending is not a progress rewrite: the trailing CR must not
// make the line collapse to nothing, or CRLF-only output would summarize as
// an empty string and the toast would read "Brew command failed: ".
func TestSummarizeDiagnosticKeepsCRLFTerminatedLines(t *testing.T) {
	output := "Warning: tap is shallow\r\nError: donor bottle is corrupt\r\n"

	if got := summarizeDiagnostic(output); got != "Error: donor bottle is corrupt" {
		t.Errorf("summarizeDiagnostic = %q, want the CRLF-terminated error line", got)
	}

	rewritesThenCRLF := "####  10.0%\r#####  99.9%\r\n"
	if got := summarizeDiagnostic(rewritesThenCRLF); got != "#####  99.9%" {
		t.Errorf("summarizeDiagnostic = %q, want the final rewrite state of a CRLF line", got)
	}
}

// An over-long line is cut in the middle so both what failed and what it
// failed on survive: a bare head-truncation drops the reference at the end.
func TestSummarizeDiagnosticKeepsBothEndsOfAnOverlongLine(t *testing.T) {
	line := "Error: " + strings.Repeat("x", diagnosticSummaryLimit*2) + " /usr/share/ublue-os/homebrew/system-dx-flatpaks.Brewfile"

	got := summarizeDiagnostic(line)

	if len([]rune(got)) != diagnosticSummaryLimit {
		t.Errorf("summary length = %d runes, want %d", len([]rune(got)), diagnosticSummaryLimit)
	}
	if !strings.HasPrefix(got, "Error: xxx") {
		t.Errorf("summary = %q, want it to keep the head of the line", got)
	}
	if !strings.HasSuffix(got, "system-dx-flatpaks.Brewfile") {
		t.Errorf("summary = %q, want it to keep the tail of the line", got)
	}
	if !strings.Contains(got, diagnosticEllipsis) {
		t.Errorf("summary = %q, want it to mark the elided middle", got)
	}
}

// Elision counts runes, so a cut never lands inside a multi-byte character and
// the summary stays valid UTF-8 for the label that renders it.
func TestSummarizeDiagnosticCutsOverlongLinesOnRuneBoundaries(t *testing.T) {
	line := "Error: " + strings.Repeat("é", diagnosticSummaryLimit*2) + " done"

	got := summarizeDiagnostic(line)

	if strings.ContainsRune(got, '�') {
		t.Errorf("summary = %q, want no replacement characters", got)
	}
	if len([]rune(got)) != diagnosticSummaryLimit {
		t.Errorf("summary length = %d runes, want %d", len([]rune(got)), diagnosticSummaryLimit)
	}
}

func TestSummarizeDiagnosticLeavesAShortLineAlone(t *testing.T) {
	line := `Error: No formulae or casks found for "demo".`

	if got := summarizeDiagnostic(line); got != line {
		t.Errorf("summarizeDiagnostic = %q, want it unchanged", got)
	}
}

func TestJoinCommandStreamsReadsStdoutBeforeStderr(t *testing.T) {
	got := joinCommandStreams("from stdout", "from stderr")

	if got != "from stdout\nfrom stderr" {
		t.Errorf("joinCommandStreams = %q, want stdout first", got)
	}
}

func TestJoinCommandStreamsSkipsBlankStreams(t *testing.T) {
	for _, tt := range []struct {
		name           string
		stdout, stderr string
		want           string
	}{
		{name: "empty stderr", stdout: "out", stderr: "", want: "out"},
		{name: "blank stderr", stdout: "out", stderr: " \n", want: "out"},
		{name: "empty stdout", stdout: "", stderr: "err", want: "err"},
		{name: "both blank", stdout: "", stderr: "\n", want: ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := joinCommandStreams(tt.stdout, tt.stderr); got != tt.want {
				t.Errorf("joinCommandStreams = %q, want %q", got, tt.want)
			}
		})
	}
}
