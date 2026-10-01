// Package liverystate owns the pure transition rules that decide whether a
// finished Livery operation may replace the confirmed UI state.
//
// Livery's page loads one confirmed selection and switch state, then runs an
// asynchronous save/fetch/apply/refresh for each user action. Adopting the
// candidate before that work lands makes an unlanded choice look applied: a
// fetch that fails leaves the summary row naming a mark that was never
// installed, and --dry-run, where every write is a no-op, leaves the page
// showing a selection nothing persisted. These rules keep the confirmed state
// truthful, and being GTK-free they are decidable in a headless test.
package liverystate

// Result records how far one Livery attempt got.
//
// A dry run never mutates, so it is carried separately rather than folded
// into Result: "would have saved" is not "saved", and treating it as such is
// exactly what lets a preview become the page's idea of the truth.
type Result struct {
	// Saved is true once the persisted setting or selection landed.
	Saved bool
}

// Outcome says what a finished attempt may publish to the confirmed state.
type Outcome struct {
	// Commit adopts the attempt's candidate value as confirmed, updating the
	// summary row and the switch this page compares against.
	Commit bool
}

// Selection resolves a brand, project, foundation, or custom-file attempt.
//
// A dry run previews without mutation, so nothing commits and reopening the
// chooser shows the real selection. A failure before the setting landed
// changed nothing, so the last confirmed selection stays. A failure after it
// landed — a fetch, apply, or refresh that did not finish — is a partial
// apply: the selection is real, so it commits, while the failure toast names
// the step that needs retrying.
func Selection(result Result, dryRun bool) Outcome {
	return commitConfirmed(result, dryRun)
}

// Toggle resolves an icon surface's on/off attempt.
//
// The switch the user flipped must not read as committed until the setting
// that backs it landed. A failed clear or refresh, or a dry-run preview,
// leaves the previous switch state in place rather than claiming success.
func Toggle(result Result, dryRun bool) Outcome {
	return commitConfirmed(result, dryRun)
}

// Rotation resolves a rotate-at-login attempt.
//
// The setting and the systemd user unit are one operation: only a fully saved
// attempt commits, and a preview or failure leaves the switch where it was.
func Rotation(result Result, dryRun bool) Outcome {
	return commitConfirmed(result, dryRun)
}

func commitConfirmed(result Result, dryRun bool) Outcome {
	if dryRun || !result.Saved {
		return Outcome{}
	}
	return Outcome{Commit: true}
}
