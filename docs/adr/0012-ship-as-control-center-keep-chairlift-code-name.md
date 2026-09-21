# 0012 — Ship as Control Center, keep ChairLift as the code name

- **Status:** Accepted
- **Date:** 2026-09-20

## Context

ChairLift ships in Bluefin as that distribution's configuration tool, and it
ships under the user-visible name **Control Center**. The repository, the
binaries, and the packages keep the name ChairLift.

Most of that split is not a preference. The application ID
`io.projectbluefin.chairlift` is load-bearing in four places that cannot move:

- the `org.freedesktop.policykit.exec.path` annotations in every
  `data/*.policy` file, which pkexec matches against the helper's absolute
  installed path ([ADR-0001](0001-fixed-path-pkexec-privilege-boundary.md));
- the install prefix and its fixed directories
  ([ADR-0002](0002-usr-prefix-is-the-only-supported-install-prefix.md));
- the nfpm package names and their `contents:` destinations in
  `.goreleaser.yaml`, including the system-integration split
  ([ADR-0006](0006-split-system-integration-package-with-mutual-conflicts.md));
- the desktop entry basename, which the icon theme name and the `Icon=` key
  both follow.

Renaming any of those is a failed authentication or a missing icon on a user's
machine, not a cosmetic change. So two names coexist permanently, and the
question is only which strings carry which.

Two further facts shaped the decision. First, the code name legitimately
appears in strings that are *not* user-visible: `"ChairLift activated"` is one
of the three E2E readiness markers and a public contract
([ADR-0008](0008-e2e-readiness-is-a-log-marker-contract.md)), and
`"ChairLiftWindow"`/`"ChairLiftApplication"` are GObject type names. A blanket
rename breaks `make e2e` and `make screenshots`.

Second, nothing enforced the boundary. The walkthrough check is deliberately
referential rather than pixel-based, so screenshots whose title bar reads
"ChairLift" pass CI while contradicting the shipped product, and no test read
the desktop entry's contents at all.

## Decision

`branding.AppName` is the sole owner of the product name. Every user-visible
spelling resolves through it: the window title, the sidebar navigation page,
the About dialog's application name, the "About …" menu item, the Help page
description, the Update All notification, and the fail-closed configuration
toast.

The code name stays everywhere else — the repository, the Go module path, the
binaries, the wrapper script, the package names, the application ID, the
GObject class names, the readiness log markers, `AGENTS.md`, the ADRs, and
`docs/design/`. Documentation picks the name that matches its audience:
`README.md` and `docs/walkthrough.md` say Control Center, agent- and
architecture-facing docs say ChairLift.

The desktop entry carries the discoverability surface, because GNOME Shell and
KRunner match against `Name`, `GenericName`, `Comment`, and `Keywords` — not
AppStream metainfo, which only feeds software centres. `Categories` gains the
registered `Settings` category, and `GenericName=System Management`
distinguishes the entry from `gnome-control-center`, which displays as
"Settings" on the same desktop.

Three gates in `internal/installcheck` hold the split.
`TestDisplayNameHasOneOwner` parses every non-test file under `internal/` and
`cmd/` and requires *every* string literal containing the code name to justify
itself — structurally, by an **anchored** predicate (it *starts* with `/`,
`https://`, or `github.com/`, so it is a path or URL; it starts with the
application ID or `CHAIRLIFT_`; it matches `^chairlift(-[a-z0-9.-]+)?$`
entirely, so it is a binary or unit name) or by an explicit entry in its
`codeNameExemptions` table stating why the code name is correct there.
Anchoring is the property that makes this gate work: an unanchored "contains a
slash" rule would let any sentence exempt itself with "and/or".
`TestCodeNameExemptionsAreAllLive` rejects an exemption whose string no longer
appears in the tree, so the table cannot accumulate unverifiable claims.
`TestDesktopEntryMatchesTheDisplayName` asserts the desktop entry's `Name`
equals `branding.AppName`, that `Type` and `Icon` are correct, that no key is
duplicated, that exactly one registered main category is present, and that the
search keys exist and are well-formed.

The gate is inverted — allowlist, not shape match — because the shape-matching
version was written first and missed two live user-visible strings on
consecutive attempts: a struct field literal
(`notify.Notification{Body: …}`) and a `fmt.Sprintf` format string
(`config.LoadError.ToastMessage`, the persistent fail-closed toast).
Enumerating the ways a string reaches a user is a losing game; requiring each
one to justify itself is not.

## Consequences

A new dialog cannot introduce a second spelling of the product name, whatever
shape it takes — setter argument, struct field, or format string — because the
gate starts from every literal rather than from a list of display APIs.

Renaming the product now means editing one constant, and the desktop gate
forces the shell-visible name to follow in the same change.

The screenshots remain the weak point. Their check is referential, so the six
PNGs in `docs/screenshots/` must be regenerated deliberately whenever the title
bar changes; nothing fails if they are stale. They are produced by the E2E
job's `walkthrough-screenshots` artifact, whose contents must be stripped of
capture byproducts before committing.

`branding` is a package holding one constant, which is ordinarily not worth a
package. It earns one here because its consumers are not all view code:
`internal/config` and `internal/notify` both need the product name, and
`internal/views/pageview` — the first home for the constant — transitively
imports `internal/sbom`, `internal/homebrew`, and `internal/troubleshoot`.
Routing a notification string through that would make a leaf package depend on
registry-SBOM parsing to obtain a noun. `branding` imports nothing.

## Alternatives considered

- **Rename the application ID too:** breaks pkexec's `exec.path` match, the
  polkit action IDs, and the installed package layout. A failed privileged
  operation on a user's OS, not a rename.
- **Retype the product name at each call site:** no owner, no gate, and the
  next dialog reintroduces the old name silently.
- **Blanket rename of every "ChairLift" string:** breaks the ADR-0008 readiness
  markers, which `test/e2e/e2e_test.go` and `capture_walkthrough.sh` both poll
  by exact literal.
- **Add AppStream metainfo in the same change:** metainfo feeds software
  centres, not shell search, so it does not serve the discoverability goal; it
  is deferred as independent work.
- **Keep `AppName` in `internal/views/pageview`:** it was there first, since
  `internal/views` and `internal/window` both already imported it. Abandoned
  once `internal/config` and `internal/notify` needed the constant too: neither
  should import a view-text package, and `pageview` pulls in SBOM parsing and
  Homebrew troubleshooting transitively.

## References

- Shapes: [design/overview.md](../design/overview.md),
  [walkthrough.md](../walkthrough.md)
- Builds on: [ADR-0001](0001-fixed-path-pkexec-privilege-boundary.md),
  [ADR-0002](0002-usr-prefix-is-the-only-supported-install-prefix.md),
  [ADR-0006](0006-split-system-integration-package-with-mutual-conflicts.md),
  [ADR-0008](0008-e2e-readiness-is-a-log-marker-contract.md),
  [ADR-0010](0010-docs-are-a-ci-gated-artifact.md)
