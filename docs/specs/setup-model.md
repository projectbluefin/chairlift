# Spec: Explicit software setup

Setup is an explicitly requested mode of the main Control Center window, not
an automatically presented welcome dialog. `--first-run`, `--setup`, `-s`, and
the Setup Assistant menu action all call `Window.PresentFirstRun`. GLib forwards
both long options to the primary instance. Repeated requests while setup is
active present the current step without resetting progress.

## Sequence and visibility

`internal/firstrun.Pages` selects Livery, Features, Apps, then Agents from the
window's existing visible primary inventory. Configuration and the shared host
capability floor therefore remove unavailable pages. Missing pages are omitted,
not advertised and redirected to Help. An empty sequence leaves the ordinary
window available without entering setup or writing a disposition.

Welcome and conclusion bookend the selection pages with one shared layout and
the embedded, theme-aware Bluefin wordmark. Welcome presents introductory body
text and the website's Zavala quote. The conclusion quotes Edith Wharton from
the website's Mission section and names `branding.AppName` as the menu entry.

Setup reuses the real page widgets and their action gates; `internal/firstrun`
owns page selection and disposition helpers, not duplicated configuration
controls. There is no choice inventory, `SetupHost` bridge, or Update
Preferences step.

## Native window adapter

The main window keeps the same built page controls, widgets, and action owners.
Setup does not reparent pages, create duplicate feature/install/model/livery
controls, or add a settings actor. A bottom bar built once offers Dismiss Setup,
Back and Next, with **Finish** on the conclusion. Back is
insensitive on Welcome and returns there from the first selection page.
Selection navigation passes through `Window.navigateToPage`; bookends use the
setup stack. Details belonging to the current step, including Developer Mode
under Features, remain accessible with their own Back control; unrelated routes
and shortcuts cannot leave the step. The sidebar is insensitive and its native
Back route is disabled; dismissal restores both while keeping the current page.
Each selection step opens at the top of its page.

Moving Back changes only navigation. Next from Features explicitly applies the
Bash-only Bluefin CLI choice, selected by default unless previously disabled.
An opt-out survives navigation; failure stays on Features for retry, and a
successful choice is not applied twice. Other Next actions only navigate.
Ordinary activation and showing Welcome perform no installation. Next from
Agents opens the conclusion. Its final action saves completion and returns to Control Center;
save errors show a toast. Escape, Dismiss and intentional window close
dismiss setup without closing the application.
`Ctrl+Q` quits in one press, setup included.

## Disposition and dry run

Ordinary activation never reads disposition to decide whether to present setup,
including when the disposition is empty or its schema is absent. Finish records
`completed` and the application version through the existing GSettings store.
Dismiss uses `firstrun.RecordSkip`, preserving an existing completion. A crash
records nothing. Persistence runs off the GTK main thread and errors return as
a toast on the main thread. Under `--dry-run`, writes are
previews.

## Verification

`internal/firstrun/flow_test.go` covers sequence order and filtering, including
an empty floor, and the explicit-only presentation decision. Store regression
tests cover persistence, dry-run suppression, and completion-preserving skips.
`test/e2e/features/setup.feature` covers both bookend Back transitions, all
selection pages, completion persistence preview,
both launch options, menu presentation and dismissal on Dakota.

## Source authority

- [`internal/firstrun/flow.go`](../../internal/firstrun/flow.go) — eligible-page
  sequence, not a catalog of choices.
- [`internal/window/window.go`](../../internal/window/window.go) —
  `PresentFirstRun`, footer navigation, intentional dismissal, and persistence.
- [`internal/firstrun/settings.go`](../../internal/firstrun/settings.go) —
  explicit-only presentation and dry-run-aware disposition/version storage.
- [`test/e2e/features/setup.feature`](../../test/e2e/features/setup.feature) —
  separate desktop interaction evidence; model tests do not replace it.
