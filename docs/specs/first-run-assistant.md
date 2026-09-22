# Spec: First-Run Assistant & Setup Flow

This specification governs the contract for the first-run assistant dialog in
Control Center (`io.projectbluefin.chairlift`), consumed by `cmd/chairlift`,
`internal/firstrun`, and the GTK UI layer in `internal/views`.

## Interface

### CLI Options

The application binary (`cmd/chairlift`) accepts the following option:

| Option | Flag | Description |
| --- | --- | --- |
| `--setup` | none | Force display of the first-run assistant regardless of stored completion state |

### GSettings Schema

Stored under schema `io.projectbluefin.chairlift.firstrun`:

| Key | Type | Default | Description |
| --- | --- | --- | --- |
| `completed-version` | string | `""` | Version string of ChairLift that completed setup; empty means setup is pending |

### First-Run Step Model

The pure model lives in `internal/firstrun` and defines:

```go
type Step struct {
    ID          string
    Title       string
    Description string
    Page        string
    Group       string
}
```

## Rules

1. **Constructed Control Instances & Refresh Synchronization:** The assistant
   constructs fresh instances of the group controls via their respective builder
   logic rather than re-parenting live widgets from the main window's pages.
   When the assistant is dismissed (via Finish or Skip), any state modified during
   the assistant flow triggers the corresponding refresh hooks (e.g.
   `refreshGamingState`, `refreshLiveryState`) across the main window so that the
   sidebar pages remain synchronized with the underlying system state.
2. **Configuration Floor:** If `config.IsGroupEnabled(step.Page, step.Group)`
   is false, the step is omitted from the active assistant sequence. (Once
   ADR-0013 is implemented, the composed capability floor will additionally
   omit steps whose backing tools are unavailable).
3. **No Navigation Flapping:** Presenting or dismissing the assistant leaves
   `navigation.Items()` and `navigation.VisibleItems()` completely untouched.
4. **Idempotence & Skip:** The user may click "Skip" or "Finish" at any point.
   "Finish" records the running version in `completed-version`. "Skip" dismisses
   the dialog without writing to GSettings, allowing setup to prompt on the next
   launch unless dismissed explicitly.
5. **Dry-Run Safety:** Under `--dry-run`, closing the assistant writes no GSettings
   key, emitting `[DRY-RUN] would set completed-version to <version>` instead.

## References

- Rationale: [ADR-0014](../adr/0014-first-run-is-a-transient-assistant-not-a-navigation-mode.md)
- Context: [design/overview.md](../design/overview.md)
