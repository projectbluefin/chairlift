package firstrun

// Disposition models the recorded state of the onboarding assistant.
type Disposition string

const (
	// DispositionNotAddressed indicates setup has never been presented or decided.
	DispositionNotAddressed Disposition = "not-addressed"

	// DispositionSkipped indicates the user selected "Get Moving".
	//
	// Closing the dialog is deliberately not this: the dialog can be closed
	// (Esc, or the header close button) and that records nothing, so the
	// assistant is presented again on the next launch. Only an explicit
	// choice settles setup.
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
