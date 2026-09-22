# 0014 — First run is a transient assistant, not a navigation mode

- **Status:** Proposed
- **Date:** 2026-09-21

## Context

Control Center presents nine pages of system management at once. A user's first
launch is the worst moment to meet all of them: nothing on the host has been
configured yet, so every page is equally unfamiliar and none is obviously
first. The request is to show a simplified view on the first run and the full
feature set on subsequent runs.

The obvious reading — "the sidebar is shorter on run one and grows on run two"
— is a second visibility mode layered onto `internal/navigation`. Three
existing constraints make that reading expensive:

1. `compact()` (`internal/navigation/navigation.go:229-239`) derives every
   accelerator from the visible index: `"<Alt>" + strconv.Itoa(index+1)`. There
   is no `Alt+10`, which is why issue #201 caps the canonical inventory at nine
   pages and adds a test asserting `len(navigation.Items()) <= 9`. The
   inventory is 7 pages today; the developer page (#198) and agents page (#199)
   take it to exactly 9. Nothing is left.
2. [ADR-0013](0013-capability-driven-visibility-as-a-floor.md) decision 3
   states that capabilities "are resolved once upon initial check and remain
   immutable for the session, preserving keyboard accelerators (`Alt+N`) and
   navigation indices", and its alternatives section rejects dynamic re-probing
   for exactly that reason. A simplified inventory that is promoted to the full
   inventory when the user finishes setup is the same mid-session re-indexing
   under a different name.
3. `navigation.VisibleItems` takes one predicate,
   `enabled func(page, group string) bool`, backed by static configuration.
   ADR-0013's composed capability predicate is still **Proposed** and
   `internal/capability/` does not exist, so a first-run gate added today would
   be the second independent visibility term rather than a composition with an
   existing floor — and would have to be reconciled when ADR-0013 lands.

The alternative reading costs none of this: first run is a thing that happens
*before* the window is useful, not a different shape of the window.

## Decision

The first-run experience is a **transient assistant**: an `AdwDialog` presented
over the main window on first launch, owning its own linear step list, and
dismissed when the user finishes or skips it. It is not an entry in
`navigation.Items()`, it holds no accelerator, and it never changes the sidebar.

1. **`internal/navigation` is untouched.** No mode parameter, no second
   predicate, no first-run field on `Item`. The nine-page ceiling and the
   `Alt+1`–`Alt+9` mapping stand exactly as issue #201 specifies them.
2. **`internal/firstrun` is a new pure leaf package**
   ([ADR-0007](0007-pure-leaf-packages-route-around-untestable-gtk.md)) owning
   the assistant's step inventory, each step's title, body, and the identifier
   of the group whose controls that step surfaces. It imports no puregotk and
   is tested headlessly.
3. **The assistant builds its own dedicated step controls.** Group builders in
   `internal/views` store widget handles directly onto `UserHome` (such as
   `uh.gamingRow`, `uh.gamingSwitch`), and state refresh functions (such as
   `refreshGamingState`) write through those exact stored pointers. Re-running
   existing builders inside the assistant would overwrite those handles,
   orphaning the main window's widgets. Instead, the assistant constructs its
   own simple control rows for each step, executes the identical underlying
   action and helper functions, and triggers the existing `refresh*State()`
   routines on dismissal. This guarantees that main window pages stay
   synchronized without clobbering widget references.
4. **A step whose group is disabled or unavailable is omitted**, evaluated
   through the same `config.IsGroupEnabled(page, group)` predicate the sidebar
   uses. The assistant therefore inherits configuration-driven visibility
   rather than defeating it.
5. **Completion is recorded in GSettings**, in a new
   `io.projectbluefin.chairlift.firstrun` schema, as a single string key
   holding the application version that completed setup. Empty means never
   completed. This is genuinely unobservable state — no file on disk implies
   it — which is the bar `AGENTS.md` sets for adding a key.
6. **The assistant is re-enterable** through a `--setup` main option
   registered alongside `--dry-run` in `internal/app`, and through a menu item.
   It is never a one-shot a user can lose.
7. **Auto-presentation is suppressed under `--dry-run`**, so `make screenshots`
   captures the underlying pages unimpeded. The assistant can still be explicitly
   presented and exercised in dry-run mode by combining flags (`--dry-run --setup`).
   Mutating steps inside the assistant are individually gated by `dryrun.Enabled()`.
## Consequences

- The nine-page ceiling survives contact with this feature, and #201 needs no
  renegotiation on its account.
- ADR-0013 can land afterwards without reconciling a competing visibility term,
  because no new term was introduced.
- The assistant is an onboarding dialog rather than a navigable sidebar page.
  `internal/installcheck`'s `TestNoOrphanedScreenshots` enforces `found == len(navigation.Items())`,
  requiring exactly one screenshot per canonical page in `docs/screenshots/`.
  The assistant is documented in `docs/walkthrough.md` via prose and does not
  commit a standalone screenshot, avoiding breaking the strict 1:1 page-to-PNG
  equality gate.
- The assistant builds dedicated lightweight controls for its steps, avoiding
  handle collision on `UserHome`. The assistant dialog is constructed once and
  re-presented on subsequent invocations (following `internal/views/livery_page.go:332-343`),
  preventing puregotk callback-table exhaustion (`maxCB = 2000`). On dismissal,
  `refresh*State()` updates the underlying main window page widgets so state stays consistent.
- Adding a third GSettings schema requires updates across four enumeration sites
  to maintain packaging and build parity: Makefile `install`, Makefile `schemas`,
  Makefile `uninstall`, and `.goreleaser.yaml` nFPM contents. A parity test in
  `internal/installcheck` must enforce this synchronization.
- A user who never finishes the assistant sees it again on the next launch.
  That is deliberate: a partially configured host is the case the assistant
  exists to serve.

## Alternatives considered

- **A simplified sidebar mode in `internal/navigation`:** rejected on ADR-0013
  decision 3 (immutable per-session inventory) and on #201's ceiling. It also
  makes the "show me everything" transition a re-index of live widgets during
  user interaction, which is the failure ADR-0013's alternatives section
  already named.
- **A separate simplified window presented instead of the main window:**
  rejected because it duplicates window chrome, the about menu, and the
  shortcut inventory, and because dismissing it would have to construct the
  real window anyway.
- **A `first_run_page` in the ordinary page inventory:** rejected because it is
  the tenth page, and because a permanent page for a one-time task is clutter
  on every subsequent run.
- **Deriving "first run" from the absence of a config file or a marker file in
  `$XDG_CONFIG_HOME`:** rejected because ChairLift's configuration is
  administrator-owned and two-tier
  ([ADR-0003](0003-two-tier-config-with-fail-closed-semantics.md)); a host with
  a maintainer default at `/usr/share/chairlift/config.yml` has a config file
  on every first run, so its presence says nothing about the user.

## References

- Shapes: [specs/first-run-assistant.md](../specs/first-run-assistant.md)
- Builds on: [ADR-0003](0003-two-tier-config-with-fail-closed-semantics.md),
  [ADR-0007](0007-pure-leaf-packages-route-around-untestable-gtk.md),
  [ADR-0013](0013-capability-driven-visibility-as-a-floor.md)
