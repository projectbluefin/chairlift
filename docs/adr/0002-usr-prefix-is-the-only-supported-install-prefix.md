# 0002 — Support PREFIX=/usr as the only install prefix

- **Status:** Accepted
- **Date:** 2026-08-12
- **Amended:** 2026-09-27 — helper renamed to `chairlift-helper` and nFPM distro packaging retired in favor of Homebrew release archives (#388)

## Context

Two system facts, not ChairLift choices, constrain where a source install
can land. First, `polkitd` reads application policies from the compiled-in
directory `/usr/share/polkit-1/actions` — it consults neither `PREFIX` nor
`$XDG_DATA_DIRS` — so policies installed anywhere else are never loaded.
Second, `pkexec` matches each privileged helper against the absolute
`exec.path` annotation ([ADR-0001](0001-fixed-path-pkexec-privilege-boundary.md)),
which names `/usr/bin/chairlift-updex-helper` for updex and
`/usr/bin/chairlift-helper` for Bluefin-family writes. The Makefile's
`PREFIX` used to default to `/usr/local`, which put the polkit assets somewhere
polkit never looks and the helpers somewhere the policies never match.

## Decision

The Makefile defaults `PREFIX ?= /usr` (`Makefile:43`, with the rationale in
the comment above it) and `/usr` is the only supported prefix; overriding it
produces an install whose privileged operations do not work. `DESTDIR`
layers under the prefix for staged/packaged installs and remains fully
supported.

The invariant is test-enforced: `TestMakefileInstallUsesUsrPrefix`
(`internal/installcheck/makefile_test.go:142`) runs `make -n install` dry
runs for the default and explicit `PREFIX=/usr` and asserts both helper
binaries land under `DESTDIR/usr/bin` and all three policies land under
`DESTDIR` + `/usr/share/polkit-1/actions`;
`TestGoreleaserArchivesCarryTheInstallSurface` holds every archive to the
same `/usr` layout and helper inventory; and the E2E staged install
(`test/e2e/e2e_test.go`, `TestInstalledBundleAndHelperBoundary`)
verifies the real `make install` layout.

Migration from the old default is documented in `README.md:223-224`: before
reinstalling at `/usr`, remove a prior source install with
`sudo make uninstall PREFIX=/usr/local`.

## Consequences

- Source installs, release archives, and the polkit/pkexec constants all agree
  on one layout; a change to any of the three fails a test rather than
  silently drifting.
- Installing to `$HOME/.local` or `/usr/local` still "works" for the
  unprivileged binary but leaves every privileged operation broken; the
  Makefile comment says so instead of pretending other prefixes are
  supported.
- Users upgrading from a `/usr/local` install must uninstall the old copy
  once (documented in README.md), or stale binaries shadow the new ones.
- `DESTDIR` keeps distro packaging and the E2E staged install possible
  without root.

## Alternatives considered

- **Keep `PREFIX=/usr/local` (FHS default for source installs):** rejected —
  polkitd never reads `/usr/local/share/polkit-1/actions`, and the fixed
  `exec.path` would not match; privileged operations silently fail.
- **Derive the policy `exec.path` from `PREFIX` at install time:** rejected —
  the path is compiled into the Go binary and cross-checked by tests; a
  templated policy would break the single-source-of-truth constant and still
  not fix the polkitd actions directory.
- **Install policies to `/usr` while binaries follow `PREFIX`:** rejected —
  splits one logical install across prefixes and still breaks the
  `exec.path` match for the helper.

## References

- Shapes: [design/overview.md — Privileged operations](../design/overview.md#privileged-operations)
  (the "Why the helper path must be absolute, and why `PREFIX=/usr`"
  discussion), `README.md` (build/install and migration note)
- Builds on: [ADR-0001](0001-fixed-path-pkexec-privilege-boundary.md)
- Enforced by: `internal/installcheck/makefile_test.go`
  (`TestMakefileInstallUsesUsrPrefix`),
  `internal/installcheck/goreleaser_test.go`
  (`TestGoreleaserArchivesCarryTheInstallSurface`),
  `test/e2e/e2e_test.go` (`TestInstalledBundleAndHelperBoundary`)
