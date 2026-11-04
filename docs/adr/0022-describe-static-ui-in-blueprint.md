# 0022 — Describe static UI in Blueprint, committed as compiled GtkBuilder XML

- **Status:** Proposed
- **Date:** 2026-10-15

## Context

Every widget tree in ChairLift was constructed imperatively in Go through
puregotk. GNOME applications describe their static widget trees declaratively,
almost always in [Blueprint](https://gnome.pages.gitlab.gnome.org/blueprint-compiler/),
and a HIG audit against the
[gnome-gui-spec](https://github.com/hanthor/gnome-gui-spec) corpus of 34
GNOME applications found structural problems that a declarative tree makes
visible and the imperative code hid: an `AdwPreferencesPage`, which scrolls
itself, nested in a second `GtkScrolledWindow` on every page; an Updates page
that walked libadwaita's private widget tree to unclamp a nested page; no
breakpoint, so the split view never collapsed; deprecated `AdwAboutWindow`
and a hand-built shortcuts window.

Constraints:

- The build is pure Go (`CGO_ENABLED=0`) and must stay so: the Homebrew cask
  ships a GoReleaser archive built without Python or GTK development files.
- puregotk cannot subclass a widget from a GtkBuilder template, and
  GtkBuilder cannot resolve a Go function by its handler name.
- `blueprint-compiler`'s output depends on its own release and on the GTK and
  libadwaita introspection data it validates against.
- GtkBuilder aborts the process on a class the running libadwaita does not
  know. Bluefin LTS ships libadwaita 1.6; Dakota ships 1.9.
- AGENTS.md said there were no generated files and no codegen step.

## Decision

Static widget trees are described in `internal/ui/<name>.blp`. Each is
compiled to `internal/ui/<name>.ui` by `scripts/blueprints.sh`
(`make blueprints`), which runs `blueprint-compiler` only inside the pinned
Dakota image, and both files are committed. The binary embeds the `.ui` files
(`go:embed`), so `go build` never runs the compiler.

A blueprint describes an object tree with ids and no signal handlers. Go code
binds it with `ui.Bind` into a struct whose fields carry `ui:"<id>"` tags and
whose blank `ui.File` field names the blueprint, then connects signals once,
at build time. Dynamic content — rows built from data, groups gated by
configuration — stays in Go and is added to bound containers. The product
name is set from Go, never spelled in a blueprint.

Blueprints use libadwaita 1.6 classes. A blueprint that needs a newer class
is bound only behind a runtime version check with a fallback blueprint
(`shortcuts.blp`, `shortcuts-legacy.blp`).

## Consequences

- The `.ui` files are generated and committed. CI recompiles every blueprint
  in Dakota (`make blueprints-check`, in the E2E job) and fails on a stale
  one; `internal/installcheck` checks, without GTK, that every blueprint is
  compiled, every binding struct names ids and classes its blueprint defines,
  every action a blueprint names is registered, the product name stays in
  `internal/branding`, and only version-gated blueprints exceed the
  libadwaita floor.
- Editing a `.blp` needs podman, like every E2E target. Editing Go does not.
- A renamed id or a changed widget type fails `make ci` rather than
  panicking at startup.
- Strings in a blueprint are marked translatable but, like the rest of the
  application, are not yet translated.
- Raising the libadwaita floor means editing `newerThanFloor` in
  `internal/installcheck/blueprint_test.go`.

## Alternatives considered

- **Compile at build time:** puts Python and blueprint-compiler in every
  build, including the release archive and a contributor's `go build`.
- **Hand-written GtkBuilder XML:** what Blueprint replaces in GNOME
  applications; verbose, and no compiler validates property names or types.
- **Keep building every tree in Go:** leaves the structural problems hard to
  see and diverges from how every GNOME application reviewed describes its UI.

## References

- Shapes: [design/overview.md](../design/overview.md),
  [design/hig-conformance.md](https://github.com/projectbluefin/chairlift/blob/fe698dd0946e077a6977c89161e0e5f65d54f5f8/docs/design/hig-conformance.md),
  [skills/blueprint-ui](../skills/blueprint-ui/SKILL.md)
- Builds on: [ADR-0007](0007-pure-leaf-packages-route-around-untestable-gtk.md),
  [ADR-0012](0012-ship-as-control-center-keep-chairlift-code-name.md)
