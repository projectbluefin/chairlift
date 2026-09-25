package firstrun

// FlowChoice represents an action path chosen by the user on the welcome screen.
type FlowChoice string

const (
	// FlowChoiceConfigure enters the optional configuration wizard.
	FlowChoiceConfigure FlowChoice = "configure"

	// FlowChoiceGetMoving exits setup immediately to the desktop.
	FlowChoiceGetMoving FlowChoice = "get-moving"
)

// PolicyRef retains the original configuration namespace of a real control.
// Setup task names never replace these keys.
type PolicyRef struct {
	Page  string
	Group string
}

// Choice identifies a delivered action or preference. Every policy reference
// must pass the shared floor. Rendering and explicitly invoking the action are
// the dialog's responsibility; navigation never invokes it.
type Choice struct {
	ID     string
	Title  string
	Policy []PolicyRef
}

// Step is an optional task containing independently filtered choices.
// Welcome is a separate entry screen, not a configuration decision.
type Step struct {
	ID          string
	Title       string
	Description string
	Choices     []Choice
}

// Stable task identifiers, independent of primary navigation.
const (
	StepIDWelcome = "welcome"
	StepIDTheme   = "theme"
	StepIDApps    = "apps"
	StepIDUpdates = "updates"
)

// Welcome copy has one owner; pageview re-exports it for the hero screen.
const (
	WelcomeStepTitle       = "Welcome to Bluefin"
	WelcomeStepDescription = "Your cloud-native developer workstation is ready. Choose how you'd like to get started."
)

// StepWelcome is the entry screen with flow selection, not a setup task.
var StepWelcome = Step{
	ID:          StepIDWelcome,
	Title:       WelcomeStepTitle,
	Description: WelcomeStepDescription,
}

// Only existing controls belong here. In particular, neither future artwork
// controls nor developer/AI/gaming activation is a prerequisite for setup.
var candidateSteps = []Step{
	{
		ID: StepIDTheme, Title: "Appearance",
		Description: "Choose the icons used on your desktop.",
		Choices: []Choice{
			{ID: "app-grid", Title: "App launcher icon", Policy: []PolicyRef{{"livery_page", "livery_app_grid_group"}}},
			{ID: "foundation", Title: "Top-bar icon", Policy: []PolicyRef{{"livery_page", "livery_foundation_group"}}},
			{ID: "dock", Title: "Files icon", Policy: []PolicyRef{{"livery_page", "livery_dock_group"}}},
		},
	},
	{
		ID: StepIDApps, Title: "Apps",
		Description: "Choose optional app and tool collections.",
		Choices: []Choice{
			{ID: "bundles", Title: "App and tool collections", Policy: []PolicyRef{{"applications_page", "brew_bundles_group"}}},
		},
	},
	{
		ID: StepIDUpdates, Title: "Update Preferences",
		Description: "Choose what to include when checking for updates.",
		Choices: []Choice{
			{ID: "applications-enabled", Title: "Applications", Policy: []PolicyRef{{"updates_page", "flatpak_updates_group"}}},
			{ID: "developer-tools-enabled", Title: "Developer tools", Policy: []PolicyRef{{"updates_page", "brew_updates_group"}}},
			{ID: "system-components-enabled", Title: "System components", Policy: []PolicyRef{{"features_page", "features_group"}}},
			{ID: "operating-system-enabled", Title: "Operating system", Policy: []PolicyRef{{"updates_page", "bootc_updates_group"}}},
		},
	},
}

// AssistantModel tracks the linear step progression and flow state of the assistant.
// It is pure Go and fully headless.
type AssistantModel struct {
	steps   []Step
	current int
}

// NewAssistantModel snapshots the session's shared capability.Compose predicate.
// Callers may further restrict it for desktop/async control readiness, but must
// never substitute config-only checks. Nil fails closed. Empty tasks are omitted;
// a welcome-only sequence exits immediately instead of opening an empty wizard.
func NewAssistantModel(floor func(page, group string) bool) *AssistantModel {
	active := []Step{StepWelcome}
	for _, candidate := range candidateSteps {
		step := candidate
		step.Choices = nil
		for _, choice := range candidate.Choices {
			if choice.enabled(floor) {
				step.Choices = append(step.Choices, cloneChoice(choice))
			}
		}
		if len(step.Choices) != 0 {
			active = append(active, step)
		}
	}
	return &AssistantModel{steps: active}
}

func (c Choice) enabled(floor func(page, group string) bool) bool {
	if floor == nil || len(c.Policy) == 0 {
		return false
	}
	for _, ref := range c.Policy {
		if !floor(ref.Page, ref.Group) {
			return false
		}
	}
	return true
}

func cloneChoice(c Choice) Choice {
	c.Policy = append([]PolicyRef(nil), c.Policy...)
	return c
}

func cloneStep(s Step) Step {
	choices := s.Choices
	s.Choices = make([]Choice, len(choices))
	for i, c := range choices {
		s.Choices[i] = cloneChoice(c)
	}
	return s
}

// Steps returns an independent snapshot, including nested policy references.
func (m *AssistantModel) Steps() []Step {
	out := make([]Step, len(m.steps))
	for i, s := range m.steps {
		out[i] = cloneStep(s)
	}
	return out
}

// Skip is available at every stage, even with no choices. It emits only the
// disposition to persist; it performs no settings or optional feature action.
func (m *AssistantModel) Skip(current Disposition) Disposition {
	return SkipPreserving(current)
}

// Dismiss treats an intentional dialog dismissal as Skip. A crash emits no
// decision. The dialog owns persistence, including dry-run and write failures.
func (m *AssistantModel) Dismiss(current Disposition) Disposition {
	return m.Skip(current)
}

// TotalSteps returns the count of active steps.
func (m *AssistantModel) TotalSteps() int {
	return len(m.steps)
}

// CurrentIndex returns the 0-based index of the active step.
func (m *AssistantModel) CurrentIndex() int {
	return m.current
}

// CurrentStep returns the step currently being presented.
func (m *AssistantModel) CurrentStep() Step {
	if m.current >= 0 && m.current < len(m.steps) {
		return cloneStep(m.steps[m.current])
	}
	return StepWelcome
}

// IsWelcome reports whether the assistant is on the opening hero screen.
func (m *AssistantModel) IsWelcome() bool {
	return m.current == 0
}

// CanGoBack reports whether navigation back to a previous step is possible.
func (m *AssistantModel) CanGoBack() bool {
	return m.current > 0
}

// HasNext reports whether there is a subsequent step in the sequence.
func (m *AssistantModel) HasNext() bool {
	return m.current+1 < len(m.steps)
}

// ForwardFinishes reports what advancing from the displayed step does: true
// when it finishes setup, false when it moves onto another step.
//
// The view labels one button with this answer. HasNext is the raw predicate
// and is deliberately not consulted by the view, because asking it around a
// move is what previously skipped the final step; this names the question the
// button actually asks.
func (m *AssistantModel) ForwardFinishes() bool {
	return !m.HasNext()
}

// SelectFlow processes the user's choice on the welcome screen.
//
// Choosing FlowChoiceGetMoving exits immediately, returning dismissed = true
// and DispositionSkipped without advancing into configuration steps.
//
// Choosing FlowChoiceConfigure advances to the first configuration step
// (if available) or completes if no configuration steps are active.
func (m *AssistantModel) SelectFlow(choice FlowChoice) (next *Step, dismissed bool, disp Disposition) {
	if choice == FlowChoiceGetMoving {
		return nil, true, m.Skip(DispositionNotAddressed)
	}

	if len(m.steps) > 1 {
		m.current = 1
		step := cloneStep(m.steps[1])
		return &step, false, DispositionNotAddressed
	}

	return nil, true, DispositionCompleted
}

// Next advances to the next step, or reports completion if at the end.
func (m *AssistantModel) Next() (next *Step, finished bool, disp Disposition) {
	if m.current+1 < len(m.steps) {
		m.current++
		step := cloneStep(m.steps[m.current])
		return &step, false, DispositionNotAddressed
	}
	return nil, true, DispositionCompleted
}

// Advance moves to the following step and reports what the view must display.
//
// Next reports finished only when there was no step left to move onto, so a
// move onto the *last* step returns that step with finished = false. Asking
// HasNext after the move instead answers false at that point — it describes
// the step after the one just reached — which closed the dialog without ever
// displaying the final step. Advance is the single answer both the view and
// this package's tests use, so that distinction cannot be re-derived wrong.
func (m *AssistantModel) Advance() (step Step, done bool) {
	next, finished, _ := m.Next()
	if finished || next == nil {
		return Step{}, true
	}
	return *next, false
}

// Previous moves back to the preceding step.
func (m *AssistantModel) Previous() (prev *Step, ok bool) {
	if m.current > 0 {
		m.current--
		step := cloneStep(m.steps[m.current])
		return &step, true
	}
	return nil, false
}
