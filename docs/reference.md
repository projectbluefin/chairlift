# Troubleshooting reference

[CONFIG.md](../CONFIG.md) owns configuration paths, precedence, group keys,
fields and examples. The [walkthrough](walkthrough.md) describes the screens.
This page keeps recovery procedures and operational limits, not another copy
of either inventory.

## Recovering a failed Developer tools update

If Homebrew reports an expected Cellar directory is missing, cleanup is a
reported workaround, not an established root-cause fix. Wait for the update to
finish; as your normal user, preview removals:

```sh
brew cleanup --dry-run
```

If acceptable, run:

```sh
brew cleanup
```

Cleanup removes old package versions and stale downloads; see
[Homebrew's cleanup reference](https://docs.brew.sh/Manpage#cleanup-options-formulacask-).
**Maintenance → Free Up Space** also runs Homebrew cleanup when enabled, plus
unused Flatpak runtime/extension removal. Wait for successful cleanup, then
choose **Updates → Retry Failed**, or check again if you left the failed run.

**Run maintenance after updates** does not run cleanup before an upgrade.
The scheduled updater `uupd` owns its separate Brew Upgrade stage; changing
that stage is not a Control Center change.

If recovery fails, retain the exact error and collect:

```sh
brew config
brew doctor --debug --verbose
brew list --versions chezmoi
journalctl -b -u uupd.service --no-pager -n 200
```

Replace `chezmoi` with the affected package. The journal is relevant to
scheduled-update failures. Include failed diagnostics after removing personal
information. Do not delete package directories to silence errors. Report
`fs-verity` corruption separately; package cleanup does not prove it repaired.

## Missing or unavailable controls

Configuration can hide a group; host capability can also prevent it from being
built. **Help → Feature Availability** explains configured groups missing a
backing tool or asset. An existing invalid configuration fails closed, logs
`CONFIGURATION ERROR`, and shows a persistent toast; fix the named file and
restart. It does not fall back to another configuration file.

The Homebrew GUI and image-installed privileged helpers ship separately.
A newer GUI cannot make an older helper support a new action. Update the OS
image when a control reports missing helper support. A dismissed authentication
prompt is not a completed action; previews do not change installed state.

## Livery settings unavailable

For source builds, run `make schemas` and use the `GSETTINGS_SCHEMA_DIR` it
prints. An installed schema XML must also be compiled in a directory searched
by GSettings; `make install` recompiles the system cache, while a Homebrew cask
must compile its user schema directory.

The GUI and headless rotation use the desktop's native dconf backend through
`internal/deskenv`. Explicit `GSETTINGS_BACKEND` selections are preserved.
Do not treat a Homebrew keyfile write as proof that GNOME adopted a preference,
and never point memory-backed test fixtures at the user's settings store.

## Migrating an older Homebrew Docker daemon

A daemon still running from `current/bin` cannot safely adopt a new runtime.
If the row reports this migration prerequisite, an administrator must stop it
through the existing fixed helper, then turn Docker on again:

```sh
pkexec /usr/bin/chairlift-helper docker-disable
```

**This stops running containers.** The terminal recovery also works when missing
account access makes the switch already read off. Refusal itself never stops
workloads or grants access. The replacement service starts through the locked
`active/bin` runtime; see [provider boundaries](design/package-managers.md).

## Service readiness and scope

- Docker requires a responding system socket, not just CLI packages or active
  units. Newly granted group access may require a new login. Disable stops the
  system units for all accounts but retains runtime, data and group membership.
- Optional VS Code setup does not install the editor or overwrite existing
  settings. Failure does not revoke Developer Mode; retry optional work rather
  than replaying a completed privileged enable.
- Agent Mode requires observed loopback health and a selected model. Goose uses
  ChairLift's isolated profile; knowledge searches go online. Package presence
  does not prove a window opened.
- Printer applications are rootless and loopback-only: no LAN printing or
  DNS-SD. Device printing and USB passthrough remain unverified. For verified-pin
  migration and diagnostics, see [printer applications](design/printer-applications.md).

Provider owners and privilege boundaries are in
[package-managers.md](design/package-managers.md); do not infer live readiness
or successful hardware operation from dry-run screenshots.
