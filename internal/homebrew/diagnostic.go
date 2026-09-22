package homebrew

import "strings"

const (
	// diagnosticSummaryLimit bounds the distilled failure line carried in an
	// Error message. The message ends up in a toast, so an unbounded line
	// (brew retains up to commandOutputTailLimit bytes of output) would be
	// unreadable no matter how it is rendered.
	diagnosticSummaryLimit = 240
	// diagnosticSummaryTail is how much of an over-long line is kept from its
	// end. Elision takes the middle rather than the end because the two ends
	// carry different information: a diagnostic opens with what failed and
	// often closes with the path or reference it failed on.
	diagnosticSummaryTail = 60
	// diagnosticEllipsis marks where the middle of an over-long line was cut.
	diagnosticEllipsis = " … "
)

// joinCommandStreams combines a failed command's stdout and stderr tails into
// one diagnostic text, stdout first.
//
// The order matters for summarizeDiagnostic. `brew bundle` runs each entry's
// real installer (`flatpak`, `mas`, `whalebrew`) as a subprocess with its
// stderr merged into its stdout, buffers that output, and replays it on
// *stdout* when the entry fails (Homebrew's Bundle.system). Brew's own
// restatements — "<verb> <name> has failed!" and "`brew bundle` failed! N
// Brewfile dependencies failed to install" — go to stderr instead. Reading
// stdout first means the first error line found is the installer's root cause
// rather than brew's count of how many entries hit one.
func joinCommandStreams(stdout, stderr string) string {
	parts := make([]string, 0, 2)
	for _, part := range []string{stdout, stderr} {
		if strings.TrimSpace(part) != "" {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, "\n")
}

// summarizeDiagnostic reduces captured brew output to the single most
// informative line.
//
// A failing `brew bundle install` prints progress for every entry it attempts
// before it prints why one of them failed, so the raw text starts with lines
// like "Installing io.podman_desktop.PodmanDesktop" and buries the cause
// further down. Handing that whole block to a single-line toast showed the
// user the progress line and hid the cause (issue #140).
//
// Selection runs in three tiers: an explicit error line, then a line reporting
// a failure, then the last line of output — brew's closing summary, which is
// the best remaining guess when nothing matched.
func summarizeDiagnostic(output string) string {
	lines := diagnosticLines(output)
	if len(lines) == 0 {
		return ""
	}
	for _, match := range []func(string) bool{isErrorLine, isFailureLine} {
		for _, line := range lines {
			if match(line) {
				return elideMiddle(line)
			}
		}
	}
	return elideMiddle(lines[len(lines)-1])
}

// diagnosticLines splits captured output into trimmed, non-empty lines.
// Carriage returns are progress rewrites of a single terminal line, so only
// the final state of such a line is kept. A trailing CR is a line ending
// (CRLF), not a rewrite, so it is trimmed before the last rewrite is chosen;
// otherwise a CRLF line would collapse to nothing.
func diagnosticLines(output string) []string {
	var lines []string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimRight(line, "\r")
		if idx := strings.LastIndex(line, "\r"); idx >= 0 {
			line = line[idx+1:]
		}
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// isErrorLine reports whether a line is a tool's own error report. It covers
// brew ("Error: ..."), the installers brew delegates to — flatpak and the
// other extensions use lowercase "error: ..." — and git, which brew shells out
// to for taps and reports with "fatal: ...".
func isErrorLine(line string) bool {
	lower := strings.ToLower(line)
	return strings.HasPrefix(lower, "error:") ||
		strings.HasPrefix(lower, "fatal:") ||
		strings.Contains(lower, "failure while executing")
}

// isFailureLine reports whether a line states that something failed, without
// using an error prefix. `brew bundle` reports a failing entry this way
// ("Installing <name> has failed!") and names the entry, which its trailing
// "Homebrew Bundle failed!" count does not.
func isFailureLine(line string) bool {
	lower := strings.ToLower(line)
	return strings.Contains(lower, "failed") || strings.Contains(lower, "failure")
}

// elideMiddle shortens a line past diagnosticSummaryLimit by cutting its
// middle, keeping diagnosticSummaryTail runes of the end. It counts runes, not
// bytes, so a cut never lands inside a multi-byte character.
func elideMiddle(line string) string {
	runes := []rune(line)
	if len(runes) <= diagnosticSummaryLimit {
		return line
	}
	head := diagnosticSummaryLimit - diagnosticSummaryTail - len([]rune(diagnosticEllipsis))
	return string(runes[:head]) + diagnosticEllipsis + string(runes[len(runes)-diagnosticSummaryTail:])
}
