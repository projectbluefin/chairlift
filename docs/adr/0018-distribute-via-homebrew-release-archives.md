# 0018 — Distribute ChairLift exclusively through Homebrew release archives

- **Status:** Accepted
- **Date:** 2026-09-27

## Context

ChairLift originally packaged distribution assets as native distribution
packages (`deb`, `rpm`, and `apk`) through GoReleaser's nFPM generator,
splitting deliverables into a full package and a GUI-less system integration
companion ([ADR-0006](0006-split-system-integration-package-with-mutual-conflicts.md)).

ChairLift is distributed only through Homebrew: the cask installs GoReleaser's
release archive, and there are no deb, rpm, or apk packages. Maintaining
parallel nFPM packages added GoReleaser packaging complexity, post-install and
post-remove script maintenance, and complex package conflict matrices without
matching the actual target deployment model.

## Decision

1. **Retire all nFPM distro packaging.** ChairLift no longer builds or
   publishes `.deb`, `.rpm`, or `.apk` packages.
2. **Distribute exclusively through Homebrew release archives.** Releases
   publish the per-architecture release archives (`chairlift_<version>_linux_<arch>.tar.gz`),
   consumed by the Homebrew cask.
3. **Carry the complete install surface in the release archive.** The release
   archive includes the unprivileged GUI, both fixed-path privileged helpers
   (`/usr/bin/chairlift-updex-helper` and `/usr/bin/chairlift-helper`), the
   PolicyKit policies (`bootc`, `updex`, and `ublue`), the GSettings schemas,
   icons, desktop file, maintainer config, and the example channels table.
   OS images extract and install the privileged pieces from this archive.

## Consequences

- GoReleaser configuration is simplified: no nFPM blocks, overrides,
  or postinstall scripts.
- Packaging CI gates in `internal/installcheck` test the archive contents
  directly rather than inspecting simulated package manager metadata.
- Distribution packaging obligations are superseded: [ADR-0006](0006-split-system-integration-package-with-mutual-conflicts.md)
  is superseded.
- Distro images that incorporate ChairLift's privileged components install
  them directly from the published release tarballs at fixed `/usr` paths.

## Alternatives considered

- **Maintain dual distro packages alongside Homebrew archives:** Rejected —
  the deb/rpm packages were unused by the target operating systems, added
  maintenance overhead, and duplicate the release surface.
- **Ship privileged helpers via a separate repository:** Rejected — keeping
  the helpers in tree guarantees that PolicyKit action IDs, helper command
  parsers, and GUI dispatch stay synchronized across releases.

## References

- Shapes: [design/overview.md](../design/overview.md), [design/package-managers.md](../design/package-managers.md)
- Supersedes: [ADR-0006](0006-split-system-integration-package-with-mutual-conflicts.md)
- Builds on: [ADR-0001](0001-fixed-path-pkexec-privilege-boundary.md), [ADR-0002](0002-usr-prefix-is-the-only-supported-install-prefix.md)
- Enforced by: `internal/installcheck/goreleaser_test.go` (`TestGoreleaserArchivesCarryTheInstallSurface`)
