# Spec: Optional setup step model

`internal/firstrun.AssistantModel` provides the pure navigation and choice
contract consumed by the transient setup dialog. This specifies issue #224's
model, not the dedicated GTK controls and persistence callbacks owned by #225.

## Interface

| Field/API | Contract |
| --- | --- |
| `Step.ID` | Stable task identity: `theme`, `apps`, `updates`; `welcome` is the entry screen |
| `Step.Choices` | Independently filtered delivered actions/preferences |
| `Choice.ID` | Stable control identity; never interpreted as an executable command |
| `Choice.Policy` | Original `(Page, Group)` references, all required |
| `NewAssistantModel(floor)` | Snapshot the session's shared `capability.Compose` predicate |
| `Steps`, `CurrentStep`, transition results | Independent copies including nested policy references |
| `Skip(current)`, `Dismiss(current)` | Emit a disposition without persistence or optional mutations |

## Rules

- **short-flow:** There MUST be at most three optional tasks, in Appearance,
  Apps, Update Preferences order. No dedicated developer, AI or gaming tour.
  The entry screen is separate; zero optional tasks MUST exit directly on
  Configure/forward without showing an empty wizard.
- **original-policy:** Every offered choice MUST pass each original policy
  reference through the shared capability floor. Nil MUST deny every choice.
  Filtering MUST remove individual denied choices and then empty tasks.
  System component update preferences MUST use `features_page/features_group`,
  not a synthetic Updates policy. Configuration cannot elevate host support.
- **delivered-controls:** The candidate inventory references existing icon
  operations, Homebrew collections, and source preferences only. No speculative
  avatar/wallpaper backend or optional activation is required. The adapter MUST
  further restrict offers for desktop/async readiness and omit controls it has
  not implemented; it MUST NOT substitute a config-only predicate.
- **navigation-only:** Construction, Configure, Next and Back MUST NOT run
  tools, change preferences, install software, or alter primary navigation or
  accelerators. Moving onto the final step MUST still present it; advancing
  beyond it emits `completed`. Intermediate advances emit `not-addressed`.
- **optional-exit:** Skip and intentional Dismiss MUST be available at any
  stage and emit the same remembered effect: `skipped`, preserving an existing
  `completed` disposition on explicit reopening. A crash emits no decision.
  The adapter owns persistence, dry-run suppression and write-error reporting.
- **snapshot:** Returned slices MUST NOT allow callers to alter internal
  choices, policy references, or later assistant sessions.

`flow_test.go` covers zero/all/subset sequences, each original policy exclusion
and capability truth table, cross-namespace filtering, compound requirements,
Skip/dismissal/Back/Next/Finish, snapshot isolation and unchanged sidebar bindings.
Tests run in the ordinary filtered headless unit gate; they do not certify GTK
interaction or an optional action's backend behavior.

## References

- Rationale: [capability floor ADR](../adr/0014-capability-driven-visibility-as-a-floor.md)
  and [pure leaf packages ADR](../adr/0007-pure-leaf-packages-route-around-untestable-gtk.md)
- Context: [architecture](../design/overview.md#optional-setup-model)
- Implementation plan: [#224](https://github.com/projectbluefin/chairlift/issues/224)
- Dialog adapter: [#225](https://github.com/projectbluefin/chairlift/issues/225)
