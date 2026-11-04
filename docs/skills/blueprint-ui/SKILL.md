---
name: blueprint-ui
description: Use when editing static widget trees and their Go bindings.
---

# Blueprint UI

- Edit `internal/ui/*.blp`, then `make blueprints` in pinned Dakota. Commit the
  generated `.ui` beside it; never hand-edit XML. `make blueprints-check`
  recompiles into a fresh directory and detects stale output.
- Give bound objects IDs. Use `ui:"<id>"` struct tags plus a blank `ui.File`
  field naming the blueprint. GtkBuilder cannot find Go handlers; connect
  signals once in Go. Bind reusable dialogs once, not every time they open.
- Keep libadwaita 1.6 compatibility. Newer classes need a runtime version gate
  and fallback, as the shortcuts dialog does; unknown classes abort startup.
- Set the product name from `branding.AppName`, never a Blueprint literal.
- An `AdwPreferencesPage` scrolls/clamps itself; do not wrap it in another
  scroller. Banners belong in the toolbar's top bars.
- `AdwDialog` may be an in-window accessibility dialog rather than a top-level
  window. Use the existing AT-SPI dialog helpers.

`internal/installcheck` checks binding IDs/types, registered actions, compiled
artifacts and the class floor without importing GTK. These guards do not prove
visual layout; exercise the changed surface through the Dakota harness.

## Navigation and setup

Keep detail parents/Back routes in `internal/navigation`; apply setup guards to
the resolved transition, not raw requested-name equality. Exercise ordinary
navigation and explicit setup separately, including hidden-detail fallback.
Reload persisted page state off-thread after setup changes it, including partial
failures; otherwise stale originals may be captured as the user's preference.
Publish widget updates through `sgtk.RunOnMainThread`.

For HIG review, use [gnome-gui-spec](https://github.com/hanthor/gnome-gui-spec).
Its static linter does not see most Go-built strings/icons; inspect those too.
For actual runtime checks, use [GTK testing](../gtk-headless-testing/SKILL.md),
not a new runner or the live desktop.
