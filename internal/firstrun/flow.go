package firstrun

// FlowChoice represents an action path chosen by the user on the welcome screen.
type FlowChoice string

const (
	// FlowChoiceConfigure enters the optional configuration wizard.
	FlowChoiceConfigure FlowChoice = "configure"

	// FlowChoiceGetMoving exits setup immediately to the desktop.
	FlowChoiceGetMoving FlowChoice = "get-moving"
)

// Step represents one screen or decision step in the onboarding assistant.
//
// Page and Groups name real entries in the configuration schema — the same
// strings config.IsGroupEnabled switches on — because that predicate is what
// filters the sequence in production. It defaults an unknown page and an
// unknown group to enabled, so a step naming anything else is never filtered
// at all: every step would show regardless of configuration.
// TestEveryStepNamesRealConfigGroups holds both halves against the schema.
type Step struct {
	ID          string
	Title       string
	Description string
	Page        string
	Groups      []string
}

// Well-known step identifiers.
const (
	StepIDWelcome   = "welcome"
	StepIDTheme     = "theme"
	StepIDApps      = "apps"
	StepIDDeveloper = "developer"
	StepIDAI        = "ai"
)

// Welcome screen copy. The hero screen and the welcome step are the same
// screen, so the strings have one owner here and internal/views/pageview
// re-exports them rather than restating them.
const (
	// WelcomeStepTitle is the prominent header on the onboarding hero screen.
	WelcomeStepTitle = "Welcome to Bluefin"

	// WelcomeStepDescription is the narrative copy under the welcome header.
	WelcomeStepDescription = "Your cloud-native developer workstation is ready. Choose how you'd like to get started."
)

var (
	// StepWelcome is the initial hero screen with branding and flow selection.
	StepWelcome = Step{
		ID:          StepIDWelcome,
		Title:       WelcomeStepTitle,
		Description: WelcomeStepDescription,
	}

	// Candidate optional configuration steps following the welcome screen.
	StepTheme = Step{
		ID:          StepIDTheme,
		Title:       "Appearance",
		Description: "Personalize system wallpaper, branding, and dinosaur avatars",
		Page:        "livery_page",
		Groups: []string{
			"livery_app_grid_group",
			"livery_foundation_group",
			"livery_dock_group",
		},
	}

	StepApps = Step{
		ID:          StepIDApps,
		Title:       "Applications",
		Description: "Discover curated Flatpak applications and Homebrew packages",
		Page:        "applications_page",
		Groups: []string{
			"applications_installed_group",
			"flatpak_user_group",
			"flatpak_system_group",
			"brew_group",
		},
	}

	StepDev = Step{
		ID:          StepIDDeveloper,
		Title:       "Developer Access",
		Description: "Configure development containers and developer environment mode",
		Page:        "features_page",
		Groups:      []string{"dx_group"},
	}

	StepAI = Step{
		ID:          StepIDAI,
		Title:       "AI Tools",
		Description: "Configure local AI models and hardware acceleration services",
		Page:        "agents_page",
		Groups:      []string{"agents_group"},
	}
)

// candidateSteps lists the sequence of optional onboarding steps.
var candidateSteps = []Step{
	StepTheme,
	StepApps,
	StepDev,
	StepAI,
}

// AssistantModel tracks the linear step progression and flow state of the assistant.
// It is pure Go and fully headless.
type AssistantModel struct {
	steps   []Step
	current int
}

// NewAssistantModel creates a step sequence filtered by group availability.
// The Welcome hero screen is always included as the first step.
//
// A step is offered when at least one of the groups behind it is enabled,
// because a step stands for a whole area of the application: Applications is
// worth offering while any one of its Flatpak or Homebrew groups survives,
// and drops out only once an administrator has disabled all of them.
func NewAssistantModel(filter func(page, group string) bool) *AssistantModel {
	active := []Step{StepWelcome}
	for _, s := range candidateSteps {
		if s.enabled(filter) {
			active = append(active, s)
		}
	}
	return &AssistantModel{
		steps:   active,
		current: 0,
	}
}

// enabled reports whether any group backing the step is enabled.
func (s Step) enabled(filter func(page, group string) bool) bool {
	if filter == nil || len(s.Groups) == 0 {
		return true
	}
	for _, group := range s.Groups {
		if filter(s.Page, group) {
			return true
		}
	}
	return false
}

// Steps returns the active step sequence.
func (m *AssistantModel) Steps() []Step {
	out := make([]Step, len(m.steps))
	copy(out, m.steps)
	return out
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
		return m.steps[m.current]
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
		return nil, true, DispositionSkipped
	}

	if len(m.steps) > 1 {
		m.current = 1
		step := m.steps[1]
		return &step, false, DispositionNotAddressed
	}

	return nil, true, DispositionCompleted
}

// Next advances to the next step, or reports completion if at the end.
func (m *AssistantModel) Next() (next *Step, finished bool, disp Disposition) {
	if m.current+1 < len(m.steps) {
		m.current++
		step := m.steps[m.current]
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
		step := m.steps[m.current]
		return &step, true
	}
	return nil, false
}
