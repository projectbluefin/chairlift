// Package cleanupview owns the decidable half of the Maintenance page's
// single "Free up space" action: what the row says, how a set of per-provider
// step results becomes one honest sentence, and whether a measured change in
// free space is trustworthy enough to show the user a number.
//
// It lives apart from internal/views for the usual reason — that package
// imports puregotk and can host no test binary — and apart from
// internal/updateproviders because the cleanup inventory is that package's
// business and the words for it are not.
//
// The one rule this package exists to enforce: never claim more than
// happened. A provider that is absent was skipped, a provider that failed
// failed, an authentication the user dismissed cleaned nothing, and a number
// of bytes is shown only when both measurements succeeded and the difference
// is large enough not to be noise.
package cleanupview

import (
	"fmt"
	"os"
	"strings"
	"syscall"

	"github.com/projectbluefin/chairlift/internal/updateproviders"
)

// Text shown by the free-space group. One action, stated in terms of what
// the person gets and what it leaves alone — the two questions a
// destructive-sounding button has to answer before it is pressed.
const (
	GroupTitle       = "Storage"
	GroupDescription = "Remove files this computer no longer needs."
	RowTitle         = "Free up space"
	RowSubtitle      = "Removes old downloads and supporting software nothing uses any more. Your apps, files, and containers are left alone."
	// ButtonLabel carries no ellipsis: the button acts immediately, it does
	// not open a dialog.
	ButtonLabel = "Clean up"
	BusyLabel   = "Cleaning up…"
)

// Text shown by the administrator-script group. These scripts are opaque to
// ChairLift — it knows a title and a command, nothing about what either
// does — so the group says where they came from and never folds them into
// the one-button cleanup above.
const (
	ScriptsGroupTitle       = "Maintenance tasks"
	ScriptsGroupDescription = "Set up by whoever set up this computer."
	ScriptsAdminSubtitle    = "Asks for your administrator password."
	ScriptsButtonLabel      = "Run"
	ScriptsBusyLabel        = "Running…"
)

// MinReportableBytes is the floor below which a measured difference in free
// space is treated as noise rather than a result. Free space moves on a live
// system for reasons that have nothing to do with this action — a log write,
// a browser cache — so a few kilobytes is not evidence the cleanup did
// anything, and a cleanup that genuinely reclaimed less than a megabyte has
// nothing worth telling the user about anyway.
const MinReportableBytes = 1_000_000

// Space is a before/after reading of free disk space. Measured is false when
// either reading failed, which is the difference between "we cannot say" and
// "nothing was freed".
type Space struct {
	Before   int64
	After    int64
	Measured bool
}

// Reclaimed reports how much space the run freed, and whether that number is
// worth showing. A non-positive difference means something else on the system
// consumed at least as much as the cleanup freed, so the reading cannot be
// attributed to this action and no number is shown.
func (s Space) Reclaimed() (int64, bool) {
	if !s.Measured {
		return 0, false
	}
	freed := s.After - s.Before
	if freed < MinReportableBytes {
		return 0, false
	}
	return freed, true
}

// Outcome is what the page shows when a cleanup run finishes. Headline is the
// row's new subtitle, empty when the row must keep the text it already has.
type Outcome struct {
	Headline string
	Toast    string
	IsError  bool
}

// Label returns the user-facing name of one cleanup step, phrased to sit
// inside a sentence. An unknown step has no name and is left out rather than
// described wrongly.
func Label(id updateproviders.StepID) string {
	switch id {
	case updateproviders.StepOldDownloads:
		return "old downloads"
	case updateproviders.StepUnusedSupport:
		return "unused supporting software"
	default:
		return ""
	}
}

// Summarize turns the run's per-step results into one sentence.
//
// A clean run gets one line. Any run where a step failed or was abandoned
// gets a line per step, naming what actually happened to each, because the
// user's next decision — retry, or go looking for the failure — depends on
// knowing which half worked.
func Summarize(dryRun bool, results []updateproviders.StepResult, space Space) Outcome {
	if dryRun {
		return Outcome{Toast: "[DRY-RUN] Preview: nothing was removed — no changes made"}
	}

	var cleaned, failed, cancelled int
	for _, result := range results {
		switch result.Outcome {
		case updateproviders.OutcomeCleaned:
			cleaned++
		case updateproviders.OutcomeFailed:
			failed++
		case updateproviders.OutcomeCancelled:
			cancelled++
		}
	}

	if cleaned == 0 && failed == 0 && cancelled == 0 {
		return sameText("There was nothing to clean up.", false)
	}

	if failed == 0 && cancelled == 0 {
		if freed, ok := space.Reclaimed(); ok {
			return sameText(fmt.Sprintf("Freed %s.", FormatBytes(freed)), false)
		}
		return sameText("Cleanup finished.", false)
	}

	parts := make([]string, 0, len(results)+1)
	for _, result := range results {
		label := Label(result.ID)
		if label == "" {
			continue
		}
		switch result.Outcome {
		case updateproviders.OutcomeCleaned:
			parts = append(parts, fmt.Sprintf("Removed %s.", label))
		case updateproviders.OutcomeFailed:
			parts = append(parts, fmt.Sprintf("Could not remove %s.", label))
		case updateproviders.OutcomeCancelled:
			parts = append(parts, fmt.Sprintf("Removing %s was cancelled.", label))
		}
	}
	if freed, ok := space.Reclaimed(); ok && cleaned > 0 {
		parts = append(parts, fmt.Sprintf("Freed %s.", FormatBytes(freed)))
	}
	return sameText(strings.Join(parts, " "), true)
}

func sameText(text string, isError bool) Outcome {
	return Outcome{Headline: text, Toast: text, IsError: isError}
}

// FormatBytes renders a byte count the way GNOME does, in decimal units.
func FormatBytes(n int64) string {
	const unit = 1000
	if n < unit {
		return fmt.Sprintf("%d bytes", n)
	}
	units := []string{"kB", "MB", "GB", "TB", "PB"}
	value := float64(n)
	index := -1
	for value >= unit && index < len(units)-1 {
		value /= unit
		index++
	}
	if value >= 10 {
		return fmt.Sprintf("%.0f %s", value, units[index])
	}
	return fmt.Sprintf("%.1f %s", value, units[index])
}

// CachePaths returns one path per filesystem that routine cleanup can free
// space on: /var holds the system application repository and, on an
// image-based host, the developer-tools prefix and the user's home as well.
// $HOME is included for the case where it is mounted separately.
func CachePaths() []string {
	paths := []string{"/var"}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		paths = append(paths, home)
	}
	return paths
}

// FreeBytes totals the free space across the distinct filesystems backing
// paths, deduplicated by device so a host where every path is one filesystem
// is not counted several times. It reports false if any path could not be
// read — a partial total would silently understate the before reading and
// overstate what the cleanup reclaimed.
func FreeBytes(paths []string) (int64, bool) {
	if len(paths) == 0 {
		return 0, false
	}
	seen := make(map[uint64]struct{}, len(paths))
	var total int64
	for _, path := range paths {
		var stat syscall.Stat_t
		if err := syscall.Stat(path, &stat); err != nil {
			return 0, false
		}
		if _, duplicate := seen[uint64(stat.Dev)]; duplicate {
			continue
		}
		seen[uint64(stat.Dev)] = struct{}{}

		var fs syscall.Statfs_t
		if err := syscall.Statfs(path, &fs); err != nil {
			return 0, false
		}
		total += int64(fs.Bavail) * int64(fs.Bsize)
	}
	return total, true
}
