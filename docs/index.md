# Control Center

Control Center is a GTK4/Libadwaita system management GUI for [Snow Linux](https://github.com/frostyard/snosi), written in Go using [puregotk](https://codeberg.org/puregotk/puregotk) bindings (no CGO). It provides a unified interface for managing Homebrew and Flatpak applications, bootc system updates, system features (via updex), and maintenance tasks.

The project, its repository, and its binaries are named ChairLift; Control
Center is the name the product ships under, so the paths, package names, and
application ID on this page keep the code name deliberately (see
[ADR-0012](adr/0012-ship-as-control-center-keep-chairlift-code-name.md)).

The UI is YAML-configuration-driven, making it portable to other Linux distributions by toggling feature groups on or off.

This page is the user-facing overview. Contributor- and agent-facing
documentation — architecture, ADRs, specs, and plans — is indexed in
[docs/README.md](README.md), with
[docs/design/overview.md](design/overview.md) as the architecture entry
point.

## Pages

Control Center provides seven configurable pages, in sidebar order:

| Page | Description |
|------|-------------|
| **Updates** | Update everything in one action or per provider: stage bootc or native A/B (systemd-sysupdate) system updates, apply Flatpak updates, upgrade Homebrew packages, trust Homebrew taps, read the booted/staged system version, and switch release channel or graphics-driver variant. |
| **Apps** | Search/install Homebrew formulae and casks; uninstall installed formulae/casks; pin/unpin formulae; install curated app collections. List/uninstall Flatpaks and launch the configured external manager for Flatpak discovery and installation. |
| **Agents** | Run a language model on this computer, served from a rootless container in your own account. |
| **Features** | Toggle system features managed by updex, plus Developer Mode, Gaming Mode, and Enhanced Troubleshooting. |
| **Livery** | Choose the marks shown on the app-grid button, the top-bar menu, and Files. |
| **Maintenance** | Free up space, run administrator-configured maintenance scripts, and — when an administrator opts in — Powerwash or Factory Reset. |
| **Help** | Links to the project website, issue tracker, and community documentation. |

A functional page is omitted when all of its groups are disabled. Help is
always retained so the window always has a valid destination.

## Keyboard Shortcuts

| Shortcut | Action |
|----------|--------|
| `Ctrl+Q` | Quit |
| `Ctrl+?` | Show shortcuts dialog |
| `Alt+1` … `Alt+N` | Open the first through Nth visible page in sidebar order; omitted pages leave no gaps |
| `F1` | Help |

## Command-Line Flags

| Flag | Description |
|------|-------------|
| `--dry-run`, `-d` | Run without making any changes to the system. Propagated to all package manager wrappers. |

## Optional Dependencies

Runtime visibility depends on the group:

- bootc status/staging groups and the native A/B staging group are hidden
  when their tool-specific runtime gates fail;
- the Homebrew untrusted-taps group stays hidden unless actionable taps exist;
- Agents keeps its group visible where Podman is absent but disables the
  switch and says the software it needs is not installed;
- Features replaces its main group with an explicit unavailable message when
  Updex is not configured;
- Applications and Updates groups for Homebrew and Flatpak remain visible and
  report that the tool is unavailable;
- Maintenance's "Free up space" action is always shown. Which package
  managers are installed is the cleanup runner's business, not something a
  user should have to learn from a group appearing or disappearing.

Page omission is separate and static: it depends only on which builder-backed
groups configuration enables, not on runtime tool availability.

| Tool | Used For |
|------|----------|
| Homebrew | Package management (formulae, casks, bundles) |
| Flatpak | Installed-application listing/uninstall and updates; new installs are delegated to the configured external manager |
| bootc + `/usr/libexec/bootc-update-stage` | Staged bootc system updates |
| `/usr/lib/snosi/native-ab` marker + `/usr/libexec/snosi-sysupdate-stage` | Staged native A/B (systemd-sysupdate) system updates |
| Updex | System feature toggles |
| Podman | The Agents page's local model container |
| `uupd.timer` systemd unit | The automatic-updates switch. ChairLift reads the unit's state and enables or masks it; it never executes the `uupd` binary |

## Building

```bash
make build
```

This produces three binaries in `build/`:

- `chairlift` — the main application
- `chairlift-updex-helper` — privileged helper for updex write operations
- `chairlift-ublue-helper` — privileged helper for Bluefin-family system writes

All are built with `CGO_ENABLED=0`.

### Installation

```bash
sudo make install
```

Installs binaries, desktop file, icons, PolicyKit policies, both privileged
helpers, and maintainer configuration defaults to `PREFIX` (default `/usr`).
The maintainer configuration is installed at `/usr/share/chairlift/config.yml`;
`/etc/chairlift/config.yml` is reserved for administrator overrides and is
never created or overwritten by ChairLift's source or nFPM packages. PolicyKit
integration requires the default prefix.

For distributions that install the GUI through a user-scoped Homebrew cask,
releases also provide a `projectbluefin-chairlift-system-integration` deb/rpm/apk.
It installs the fixed helper binaries at `/usr/bin/chairlift-updex-helper` and
`/usr/bin/chairlift-ublue-helper`; the four policies
`/usr/share/polkit-1/actions/io.projectbluefin.chairlift.bootc.policy`,
`/usr/share/polkit-1/actions/io.projectbluefin.chairlift.sysupdate.policy`,
`/usr/share/polkit-1/actions/io.projectbluefin.chairlift.updex.policy`, and
`/usr/share/polkit-1/actions/io.projectbluefin.chairlift.ublue.policy`;
`/usr/share/chairlift/config.yml`; and the documented channel-table example at
`/usr/share/doc/chairlift/channels.example.yml`, without installing the GUI. It
intentionally conflicts with the self-contained `projectbluefin-chairlift`
package. Bootc staging additionally requires the distribution to provide its
trusted implementation at `/usr/libexec/bootc-update-stage`; the integration
package does not supply one. Native A/B staging uses
`/usr/libexec/snosi-sysupdate-stage`, which ships with the OS image itself.

### Development

The [quality dashboard](quality.md) links to live CI, coverage, build-artifact,
and release signals and explains how to reproduce the enforced checks locally.
The [public metrics catalog](metrics/) collects the public, read-only sources
and documents their interpretation and agent-provenance boundaries.

```bash
make run      # Build and run with --dry-run
make dev      # Debug build with race detector
make test     # Run tests
make lint     # Run golangci-lint
```
