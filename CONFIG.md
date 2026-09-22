# ChairLift Configuration

ChairLift can be configured to show or hide specific feature groups, making it more portable across different Linux distributions.

## Configuration File Location

ChairLift searches for the configuration file in the following locations (in order):

1. `/etc/chairlift/config.yml` (system-wide configuration - highest priority)
2. `/usr/share/chairlift/config.yml` (package maintainer defaults installed by
   source and nFPM packages)
3. `config.dev.yml` beside the ChairLift executable, or in the current working
   directory when no executable-relative file exists (source-checkout fallback)
4. `config.yml` beside the ChairLift executable, or in the current working
   directory when no executable-relative file exists (legacy development fallback)

If no configuration file is found, all features default to enabled, except
`maintenance_cleanup_group` and `reset_group`, which default to disabled.

Packages own and may replace the `/usr/share` defaults during an upgrade.
Administrators should put local changes in `/etc/chairlift/config.yml`;
ChairLift's install and packaging paths never create or overwrite that file.
The `projectbluefin-chairlift-system-integration` package also provides the
`/usr/share` defaults for a user-scoped GUI installation.
The repository root includes `config.dev.yml` so `go run .` from a checkout can
load unprivileged development defaults before the package default `config.yml`.

The first file that exists in this order is authoritative. ChairLift does not
fall through to a lower-priority file when that file is unreadable, malformed,
or fails schema validation. Instead, it disables every feature group, logs a
high-signal `CONFIGURATION ERROR`, and displays a persistent error toast naming
the file and cause. Fix the authoritative file and restart Control Center.

These semantics are decision records
[ADR-0003](docs/adr/0003-two-tier-config-with-fail-closed-semantics.md)
(search order and fail-closed behavior) and
[ADR-0004](docs/adr/0004-configuration-error-diagnostic-vocabulary.md)
(the diagnostic vocabulary).

## Configuration Format

The configuration file uses YAML format with a simple structure:

```yaml
page_name:
  group_name:
    enabled: true/false
```

When every functional group on a page is disabled, that page is omitted from
the sidebar, content stack, shortcuts dialog, and Alt+number bindings. The
remaining Alt+number shortcuts compact in sidebar order. Help remains visible
even when `help_resources_group` is disabled, so the application always has a
valid page.

## Available Pages and Groups

### System Page (`system_page`)

- `system_info_group`: Operating system information from /etc/os-release
- `bootc_status_group`: System status information from bootc (when available)
- `channel_group`: Release channel and graphics-driver switching; shown only when `/usr/share/ublue-os/image-info.json` is present
- `health_group`: System health monitoring and performance tools
  - `app_id`: Application ID for the system monitoring tool (default: `io.missioncenter.MissionCenter`)

### Updates Page (`updates_page`)

- `update_all_group`: Multi-phase update sequencing (OS image, Flatpaks, Homebrew) and automatic background updates switch
- `bootc_updates_group`: System-wide bootc updates
- `sysupdate_updates_group`: System-wide native A/B (systemd-sysupdate) updates; shown only on native A/B installs
- `flatpak_updates_group`: Available Flatpak application updates (user and system)
- `brew_updates_group`: Homebrew package updates and outdated packages
- `brew_trust_group`: Untrusted Homebrew taps with installed packages (Homebrew 6 tap trust); only shown when there is something to trust

### Applications Page (`applications_page`)

- `applications_installed_group`: External Flatpak manager link for discovering
  and installing new applications; ChairLift itself lists and uninstalls
  installed Flatpaks but does not directly install them
  - `app_id`: Application ID for the Flatpak manager (default: `io.github.kolunmi.Bazaar`)
- `flatpak_user_group`: User-installed Flatpak applications with uninstall actions
- `flatpak_system_group`: System-wide Flatpak applications with uninstall actions
- `brew_group`: Installed Homebrew formulae/casks with uninstall actions and
  formula pin/unpin actions
- `brew_search_group`: Search and install Homebrew formulae and casks
- `brew_bundles_group`: Curated Homebrew package bundles
  - `bundles_paths`: Array of directories searched for immediate
    `*.Brewfile` entries (default: `['/usr/share/ublue-os/homebrew', '/usr/share/chairlift/bundles', '/etc/chairlift/bundles', '/usr/share/snow/bundles']`). Missing
    directories are ignored so one configuration can cover multiple
    distribution variants. Other unreadable paths are reported in the group
    while bundles from readable paths remain available. Repeating the same
    path does not duplicate a row; same-named Brewfiles in different
    directories remain distinct and show their full paths.

### Maintenance Page (`maintenance_page`)

- `maintenance_cleanup_group`: System cleanup utilities (disabled by default)
  - `actions`: Array of maintenance scripts that can be executed
    - `title`: Display name for the action
    - `script`: Absolute path to the script to execute. Required when `sudo: true`.
    - `sudo`: Boolean indicating if the script requires administrator privileges (uses pkexec). `sudo: true` is accepted only from trusted `/etc/chairlift/config.yml` or `/usr/share/chairlift/config.yml` configurations. The rule is applied to the effective configuration, so an untrusted file may not enable a group whose actions include a privileged one, even when it inherits that action from the built-in defaults rather than declaring `sudo: true` itself.
- `maintenance_brew_group`: Homebrew cleanup (runs `brew cleanup` to remove old versions and cache)
- `maintenance_flatpak_group`: Flatpak cleanup (runs `flatpak uninstall --unused` to remove unused runtimes)
- `maintenance_optimization_group`: System optimization tools
- `reset_group`: Powerwash (removes user Flatpaks and Distrobox containers) and Factory Reset (`bootc install reset --experimental`) utilities (disabled by default)

### Features Page (`features_page`)

- `features_group`: System features managed by updex (requires `updex` command)
- `dx_group`: Developer Mode; adds the invoking account to container, VM, and serial-device groups (shown only when `/usr/share/ublue-os/image-info.json` is present)
- `gaming_group`: Gaming Mode; toggles gaming optimizations (shown only when `/usr/share/ublue-os/image-info.json` is present)
- `ai_group`: Local AI language model served in a rootless container via Quadlet/Podman; shown when Podman is present
  - `ai_images`: Map of container image references per GPU vendor (`nvidia`, `amd`, `intel`, `none`)
  - `ai_model`: Model reference to serve (default: `ollama://qwen2.5:7b`)
- `troubleshooting_group`: Enhanced Troubleshooting; AI diagnostic assistant installed via Homebrew (shown only when Homebrew is present)

### Livery Page (`livery_page`)

Who you are, who you stand with, and what you roll with. Each section shadows
one icon-theme name inside the user's own theme, so nothing here is privileged
and nothing is written outside `$XDG_DATA_HOME` and `$XDG_CONFIG_HOME`.

- `livery_app_grid_group`: App Grid Livery; the Show Applications mark, fetched from simpleicons.org by brand name. Set once — there is no rotation
- `livery_foundation_group`: Foundational Livery; the top-bar menu mark, optionally advancing at each login (requires the Custom Command Menu GNOME extension)
- `livery_dock_group`: Dock Livery; the Files application icon, set to a CNCF project's own color mark from cncf/artwork and chosen with a searchable picker, optionally advancing at each login. GNOME stores one icon per application, so this changes Files everywhere it is drawn, not only on the dock

### Help Page (`help_page`)

- `help_resources_group`: Help and support resources
  - `website`: URL to the project website
  - `issues`: URL to the issue tracker for bug reports and feature requests
  - `chat`: URL to community chat or discussions

## Example: Disabling Homebrew Features

To create a distribution-specific configuration that disables all Homebrew features:

```yaml
updates_page:
  update_all_group:
    enabled: false # Hide Update All so it cannot run Homebrew updates
  bootc_updates_group:
    enabled: true
  flatpak_updates_group:
    enabled: true # Keep Flatpak updates
  brew_updates_group:
    enabled: false # Hide Homebrew updates
  brew_trust_group:
    enabled: false # Hide Homebrew tap trust

applications_page:
  applications_installed_group:
    enabled: true
  flatpak_user_group:
    enabled: true
  flatpak_system_group:
    enabled: true
  brew_group:
    enabled: false # Hide Homebrew packages
  brew_search_group:
    enabled: false # Hide Homebrew search
  brew_bundles_group:
    enabled: false # Hide Homebrew bundles

# Other pages remain fully enabled
system_page:
  system_info_group:
    enabled: true
  health_group:
    enabled: true

maintenance_page:
  maintenance_cleanup_group:
    enabled: true
  maintenance_brew_group:
    enabled: false # Hide Homebrew cleanup
  maintenance_flatpak_group:
    enabled: true
  maintenance_optimization_group:
    enabled: true

features_page:
  troubleshooting_group:
    enabled: false # Hide Homebrew-backed troubleshooting assistant

help_page:
  help_resources_group:
    enabled: true
```

## Deployment

### For System Administrators

To customize ChairLift for your distribution:

1. Create a custom `config.yml` with your desired settings
2. Install it to `/etc/chairlift/config.yml` for system-wide configuration
3. Package maintainers can include it in `/usr/share/chairlift/config.yml`

### For Package Maintainers

When packaging ChairLift for different distributions:

1. Copy the default `config.yml` to your package
2. Modify it to match your distribution's available features
3. Install it to the appropriate location during package installation

Example for Debian packaging:

```bash
install -D -m 644 config.yml debian/tmp/usr/share/chairlift/config.yml
```

## Notes

- Groups with `enabled: false` will be completely hidden from the UI
- A configuration file is applied as an overlay on top of the built-in
  defaults, field by field, not as a full replacement:
  - An omitted `enabled` key inherits that group's documented default
    (`true` for every group except `maintenance_cleanup_group` and
    `reset_group`, which default to `false`)
  - An omitted optional field (`app_id`, `website`, `issues`, `chat`,
    `actions`, `bundles_paths`, `ai_images`, `ai_model`) inherits its documented default value
  - An explicit empty list (e.g. `actions: []`) clears the field
  - A non-empty list, or an explicitly set scalar value, replaces the
    default outright
- Page names, group names, and field names must match the documented schema;
  unknown names and values of the wrong type are configuration errors (the
  schema is derived from the canonical config struct — decision record
  [ADR-0005](docs/adr/0005-config-schema-reflected-from-canonical-struct.md))
- An unreadable, malformed, or schema-invalid authoritative file fails closed:
  all feature groups are hidden, lower-priority files are ignored, and
  ChairLift reports the path and cause in both its log and a persistent toast
- Changes require restarting ChairLift to take effect
