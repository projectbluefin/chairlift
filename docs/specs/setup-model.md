# Spec: Explicit software setup

Setup is an explicitly requested mode of the main Control Center window, not
an automatically presented welcome dialog. `--first-run`, `--setup`, `-s`, and
the Setup Assistant menu action all call `Window.PresentFirstRun`. GLib forwards
both long options to the primary instance. Repeated requests while setup is
active present the current step without resetting progress.

## Sequence and visibility

`internal/firstrun.Pages` selects Features, Apps, Agents, then Livery from the
window's existing visible primary inventory. Configuration and the shared host
capability floor therefore remove unavailable pages. Missing pages are omitted,
not advertised and redirected to Help. An empty sequence leaves the ordinary
window available without entering setup or writing a disposition.

There is no welcome hero, choice inventory, `SetupHost` bridge, or Update
Preferences step. Setup reuses the real page widgets and their action gates;
`internal/firstrun` owns page selection and disposition helpers, not duplicated
configuration controls. The embedded wordmark loader remains available to Updates.

## Native window adapter

The main window keeps the same built page controls, widgets, and action owners.
Setup does not reparent pages, create duplicate feature/install/model/livery
controls, or add a settings actor. A footer built once offers Dismiss setup,
Back, and Next (Finish on the last page). Back is insensitive on the first page.
Navigation always passes through `Window.navigateToPage`; while setup is active,
other navigation requests cannot leave its current step. The sidebar is
insensitive, the split view shows content only, and the content page's native
Back-to-sidebar route is disabled. Exiting restores the prior collapsed mode,
sidebar sensitivity, and the native Back route while keeping the current page.

Moving Back or Next changes only navigation. It never installs software,
changes configuration, or writes setup disposition. Advancing onto Livery must
show it; only its Finish button completes setup. Escape, Dismiss setup, and an
intentional window close dismiss setup without closing the application.
`Ctrl+Q` quits in one press, setup included; only a running update refuses it.

## Disposition and dry run

Ordinary activation never reads disposition to decide whether to present setup,
including when the disposition is empty or its schema is absent. Finish records
`completed` and the application version through the existing GSettings store.
Dismiss uses `firstrun.RecordSkip`, preserving an existing completion. A crash
records nothing. Persistence runs off the GTK main thread and errors return as
a toast on the main thread. Under `--dry-run` writes are log lines only.

## Verification

`internal/firstrun/flow_test.go` covers sequence order and filtering, including
an empty floor, and the explicit-only presentation decision. Store regression
tests cover persistence, dry-run suppression, and completion-preserving skips.
`test/e2e/features/setup.feature` exercises the native window sequence, Back,
Finish, both launch options, menu presentation, and dismissal on Dakota.

## Source authority

- [`internal/firstrun/flow.go`](../../internal/firstrun/flow.go) — eligible-page
  sequence, not a catalog of choices.
- [`internal/window/window.go`](../../internal/window/window.go) —
  `PresentFirstRun`, footer navigation, intentional dismissal, and persistence.
- [`internal/firstrun/settings.go`](../../internal/firstrun/settings.go) —
  explicit-only presentation and dry-run-aware disposition/version storage.
- [`test/e2e/features/setup.feature`](../../test/e2e/features/setup.feature) —
  separate desktop interaction evidence; model tests do not replace it.
