package firstrun

// Disposition models the recorded state of the onboarding assistant.
type Disposition string

const (
	// DispositionNotAddressed indicates setup has never been presented or decided.
	DispositionNotAddressed Disposition = "not-addressed"

	// DispositionSkipped records an explicit skip. AssistantModel.Dismiss
	// emits the same decision for intentional dismissal; the dialog adapter
	// owns wiring and persistence. A crash does not emit a decision.
	DispositionSkipped Disposition = "skipped"

	// DispositionCompleted indicates the user stepped through to completion.
	DispositionCompleted Disposition = "completed"
)

// IsSettled reports whether setup is finished (either skipped or completed).
func (d Disposition) IsSettled() bool {
	return d == DispositionSkipped || d == DispositionCompleted
}

// SkipPreserving returns the disposition to record when the user chooses
// "Get Moving" while current is already recorded.
//
// The assistant is reachable again from the menu and from --setup after setup
// finished, so "Get Moving" can be pressed by someone who already completed
// every step. Writing skipped unconditionally would regress that record,
// because GetDisposition prefers the disposition key over the completed
// version: a finished setup would read back as skipped forever after.
func SkipPreserving(current Disposition) Disposition {
	if current == DispositionCompleted {
		return DispositionCompleted
	}
	return DispositionSkipped
}
