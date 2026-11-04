---
name: blueprint-ui
description: Use when adding or changing a widget tree under internal/ui (Blueprint), binding one from Go with ui.Bind, or auditing the UI against the GNOME HIG.
version: 1.1.0
last_updated: 2026-11-04
tags:
  - gtk
  - blueprint
  - hig
metadata:
  type: reference
---

# Static widget trees are Blueprint; Go binds them and adds what is dynamic

**When it applies:** Any change to the window shell, a page's skeleton, a
dialog, or anything else whose shape does not depend on data; any HIG review.

ADR-0022 records the decision. The mechanics, and the traps behind them:

1. **Edit the `.blp`, then run `make blueprints`.** It compiles every
   `internal/ui/*.blp` inside the pinned Dakota image and copies the `.ui`
   beside it. Never edit a `.ui`: `make blueprints-check` (the E2E CI job)
   recompiles and fails on any difference. The script compiles into a fresh
   directory because `blueprint-compiler batch-compile` skips an output that
   looks newer than its source, which once kept a hand-edited `.ui` alive. It
   also appends the trailing newline the end-of-file gate requires and the
   compiler omits.
2. **No templates, no handler names.** puregotk cannot subclass from a
   GtkBuilder template, and GtkBuilder cannot find a Go handler by name. Give
   every object Go needs an id, bind it into a struct with `ui:"<id>"` tags
   and a blank `_ ui.File \`ui:"<blueprint>"\`` field, and connect signals in
   Go once at build time (the callback-table invariant in AGENTS.md still
   applies). `ui.Bind` builds a fresh copy each call, so one blueprint can back
   many pages (`page.blp`). It releases the builder before returning; each
   bound object keeps one reference that Go never drops (puregotk has no
   finalizers), so bind a dialog once and re-present it, as the About and
   shortcuts dialogs are, rather than binding one per open.
3. **`internal/installcheck` reads both sides without GTK.**
   `TestBlueprintBindingsMatchTheirUI` parses every binding struct's tags and
   wrapper types and the compiled XML: an id the blueprint lacks, or
   `*gtk.Label` bound to a `GtkButton`, fails `make ci` instead of panicking at
   startup. `TestBlueprintActionsAreRegistered` holds `action-name` and menu
   `action` values to a `gio.NewSimpleAction` in `internal/window` or
   `internal/app`.
4. **The libadwaita floor is 1.6 (Bluefin LTS).** GtkBuilder aborts the whole
   process on an unknown class, so a class from a newer release (the table in
   `TestBlueprintsStayWithinTheLibadwaitaFloor`) needs a runtime version check
   and a fallback blueprint, as `shortcuts.blp` (AdwShortcutsDialog, 1.8) has
   `shortcuts-legacy.blp`. The same floor is why labels keep `dim-label`: its
   replacement, `dimmed`, arrived in 1.7.
5. **The product name is set from Go** (`branding.AppName`);
   `TestBlueprintsDoNotSpellTheProductName` rejects it in a blueprint. The
   About menu item is appended to the menu's `app_section` for that reason.
6. **An `AdwPreferencesPage` scrolls and clamps itself.** Make it a toolbar's
   content directly; a `GtkScrolledWindow` around it nests a second scroller.
   A banner belongs in the toolbar's top bars, not in the scrolled content.
7. **An `AdwDialog` is not a window.** It presents inside its parent when it
   fits, as an AT-SPI `dialog` node, so the behave suite finds it through
   `chairlift_atspi.titled`, not the application's top-level children.

## Moving controls into a detail page

1. Keep the parent and Back route in `internal/navigation`; resolve a transition
   before applying the setup wizard's current-step guard. A detail belongs to
   the active step when its resolved `Back` names that step. Raw requested-route
   equality blocks the detail even when ordinary navigation works.
2. Exercise ordinary navigation and explicit setup separately: enter the detail,
   reject unrelated keyboard navigation, return to its parent, then advance the
   wizard. Include relocated collections and their disabled-detail fallback.
3. If setup writes preferences owned by an already-built page, reload that
   page's persisted state before announcing completion. Reload after partial
   write failures too; stale saved originals can otherwise be recaptured from
   ChairLift's own newly applied icon. Follow `refreshLiveryState`: read off the
   main thread and publish through `sgtk.RunOnMainThread`.

GTK's [threading reference](https://github.com/GNOME/gtk/blob/main/docs/reference/gtk/section-threading.md)
requires widget access on the creating main thread and worker results returned
through an idle or `GLib.MainContext.invoke` (Context7 source: `/gnome/gtk`).

## Auditing against the HIG

Use [gnome-gui-spec](https://github.com/hanthor/gnome-gui-spec)'s
`skills/hig-audit/SKILL.md` categories. Its `scripts/lint-app.sh` reads only
`.blp`/`.ui` and C-family sources, so it misses most strings and icons built in
Go: grep the Go sources for `-symbolic"` names and check them against its
`reference/icons.md`. The current state and open findings are in
[design/hig-conformance.md](../../design/hig-conformance.md).

## Running the Dakota harness on an unusual host

`test/e2e/dakota.sh` expects rootless podman on a Fedora-like host. Headless
Mutter refuses to run as uid 0 ("A connection to the bus can't be made"), so
a host where `--userns=keep-id` maps to root needs the container run as an
ordinary uid; and runc (unlike crun) refuses the `/proc/cmdline` bind mount.
Neither is a reason to change the harness.

**Learned from:** the Blueprint port and HIG audit of October 2026.
