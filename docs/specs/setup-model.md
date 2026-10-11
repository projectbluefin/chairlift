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

Welcome and conclusion bookend the selection pages with one shared layout and
the embedded, theme-aware Bluefin wordmark. Welcome presents introductory body
text and the website's Zavala quote. The conclusion quotes Edith Wharton from
the website's Mission section and names `branding.AppName` as the menu entry.

## Native window adapter

The main window keeps the same built page controls, widgets, and action owners.
Setup does not reparent pages, create duplicate feature/install/model/livery
controls, or add a settings actor. A footer built once offers Dismiss setup,
Back and Next, with **Launch Bazaar App Store** on the conclusion. Back is
insensitive on Welcome and returns there from the first selection page.
Selection navigation passes through `Window.navigateToPage`; bookends use the
setup stack. Other routes cannot leave the current step. The sidebar is
insensitive and its native Back route is disabled; dismissal restores both.

Moving Back or Next changes only navigation. It never installs software,
changes configuration, or writes disposition. Next from Livery opens the
conclusion. Its final action saves completion, then runs
`gtk-launch io.github.kolunmi.Bazaar` off the GTK thread and quits on success.
Save or launch errors show a toast and restore the retryable final action.
While finishing, duplicate actions, dismissal and window close cannot interrupt
the save/launch boundary. Escape, Dismiss and intentional window close otherwise
dismiss setup without closing the application.

## Disposition and dry run

Ordinary activation never reads disposition to decide whether to present setup,
including when the disposition is empty or its schema is absent. Finish records
`completed` and the application version through the existing GSettings store.
Dismiss uses `firstrun.RecordSkip`, preserving an existing completion. A crash
records nothing. Persistence runs off the GTK main thread and errors return as
a toast on the main thread. Under `--dry-run`, writes and Bazaar launch are
previews; the final application exit still runs after the preview completes.

## Verification

`internal/firstrun/flow_test.go` covers sequence order and filtering, including
an empty floor, and the explicit-only presentation decision. Store regression
tests cover persistence, dry-run suppression, and completion-preserving skips.
`test/e2e/features/setup.feature` covers both bookend Back transitions, all
selection pages, final exit, persistence preview, Bazaar suppression, both
launch options, menu presentation and dismissal on Dakota.
