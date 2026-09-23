package firstrun

// Disposition models the recorded state of the onboarding assistant.
type Disposition string

const (
	// DispositionNotAddressed indicates setup has never been presented or decided.
	DispositionNotAddressed Disposition = "not-addressed"

	// DispositionSkipped indicates the user selected "Get Moving" or dismissed setup.
	DispositionSkipped Disposition = "skipped"

	// DispositionCompleted indicates the user stepped through to completion.
	DispositionCompleted Disposition = "completed"
)

// IsSettled reports whether setup is finished (either skipped or completed).
func (d Disposition) IsSettled() bool {
	return d == DispositionSkipped || d == DispositionCompleted
}
