# ChairLift Configuration

ChairLift can be configured to show or hide specific feature groups, making it more portable across different Linux distributions.

## Configuration File Location

ChairLift searches for the configuration file in the following locations (in order):

1. `/etc/chairlift/config.yml` (system-wide configuration - highest priority)
2. `/usr/share/chairlift/config.yml` (maintainer defaults, installed by source
   `make install` or by an OS image from the release archive's `config.yml`)
3. `config.dev.yml` beside the ChairLift executable, or in the current working
   directory when no executable-relative file exists (source-checkout fallback)
4. `config.yml` beside the ChairLift executable, or in the current working
   directory when no executable-relative file exists (legacy development fallback)

If no configuration file is found, all features default to enabled, except
`maintenance_cleanup_group` and `reset_group`, which default to disabled.

The repository's shipped [`config.yml`](config.yml) enables only
`brew_bundles_group` on Apps. It disables `applications_installed_group`,
`flatpak_user_group`, `flatpak_system_group`, `brew_group`, and
`brew_search_group`; all six Apps groups are enabled by built-in defaults.
Administrators can enable `brew_group` for installed Homebrew inventory and
the Brewfile exporter independently of collections.

Whoever installs the `/usr/share` defaults owns them and may replace them
during an upgrade. Administrators should put local changes in
`/etc/chairlift/config.yml`; ChairLift's install paths never create or
overwrite that file.
The repository root includes `config.dev.yml` so `go run ./cmd/chairlift` (or
`make run`) from a checkout can load unprivileged development defaults before
the package default `config.yml`.

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

Visibility also has a host-capability floor: configuration can hide a group,
but cannot make a missing backing tool or asset available. The same composed
predicate determines sidebar pages and their groups; asynchronous readiness
checks can further explain or hide controls after construction.

## Legacy System page compatibility

Older files may still contain `system_page`. Its `bootc_status_group` and
`channel_group` settings are applied to the corresponding groups under
`updates_page`, preserving `enabled: false`. When both locations specify a
field, a non-null value under `updates_page` wins; omitted or null fields
inherit the legacy value. The retired `system_info_group` and `health_group`
are accepted but have no runtime effect. The System page is not restored.

All four legacy groups still undergo ordinary field, type, and action
validation, including trusted-path restrictions on `sudo: true`, even for
retired or superseded values. Unknown names still fail closed. This is a
compatibility rule for `system_page`, not general acceptance of obsolete keys
elsewhere in an old file. No file is rewritten: administrators can move the
two surviving groups to `updates_page` and remove `system_page` when convenient.

## Available Pages and Groups

### Agents Page (`agents_page`)

- `agents_group`: Agent Mode — llmman installed with Homebrew and served as a systemd user unit on `127.0.0.1:17434` in the invoking user's own account; shown where Homebrew is present. Nothing here crosses a privilege boundary. It has no options beyond `enabled`. The page also presents Goose Desktop launch with verified Linux diagnostic tools, the "Show Ask Bluefin in menu" shortcut preference, and Contribute to Bluefin via `ujust contribute`.

### Updates Page (`updates_page`)

- `automatic_updates_group`: The automatic-background-updates switch, and nothing else — whether this system installs updates on its own schedule. Updating now is the update shell's single primary action and has no configuration key. Hidden entirely on a host with no unattended-update timer
- `bootc_updates_group`: System-wide bootc updates
- `flatpak_updates_group`: Available Flatpak application updates (user and system)
- `brew_updates_group`: Homebrew package updates and outdated packages
- `brew_trust_group`: Untrusted Homebrew taps with installed packages (Homebrew 6 tap trust); only shown when there is something to trust
- `channel_group`: Release channel and graphics-driver switching. Both replace the operating system and require a restart, so they sit with updates. Shown only when `/usr/share/ublue-os/image-info.json` is present
- `bootc_status_group`: Compact system-version readout, with build identifiers behind a details row (when available)

### Applications Page (`applications_page`)

When enabled and supported, Apps orders the external catalog launcher,
Homebrew search/results, installed Flatpak applications, installed Homebrew
casks, app collections, explicitly requested formulae, then Brewfile export.
All six configuration groups remain supported; none is retired.

- `applications_installed_group`: External software catalog launcher
  - `app_id`: Application launched by "Browse all apps"; defaults to `io.github.kolunmi.Bazaar`
- `flatpak_user_group`: Installed applications for the invoking account, with confirmed uninstall actions
- `flatpak_system_group`: Installed applications for every account, with scope-aware uninstall confirmation
- `brew_search_group`: Search Homebrew formulae and casks, with typed install actions
- `brew_group`: Installed Homebrew casks and explicitly requested formulae,
  with uninstall actions and formula pin/unpin actions, plus the Brewfile exporter
- `brew_bundles_group`: Curated Homebrew package bundles, displayed after installed casks
  - `bundles_paths`: Array of directories searched for immediate
    `*.Brewfile` entries. The built-in default (`internal/config/config.go`,
    used when no configuration file supplies the field) is
    `['/usr/share/ublue-os/homebrew', '/usr/share/chairlift/bundles', '/etc/chairlift/bundles']`;
    the shipped `config.yml` narrows it to `['/usr/share/ublue-os/homebrew']`.
    Missing directories are ignored so one configuration can cover multiple
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
- `maintenance_freespace_group`: One routine cleanup action composing the shared post-update maintenance runner; removes cached downloads and unused supporting software, never installed apps, documents, or containers
- `reset_group`: Recovery reset actions (disabled by default); gates user-scope Powerwash (removes user Flatpaks and Distrobox containers) and Factory Reset (`bootc install reset --experimental`). Roll Back and Published versions (Pin, Return to stream) on the same Recovery screen are gated by `updates_page.bootc_updates_group`, not this key.

### Features Page (`features_page`)

- `desktop_integrations_group`: Tailscale Integration and Sync Folder Integration GNOME extension switches. Enabled by default; missing extensions or an unavailable GNOME session leave the switches insensitive with an explanation. Existing GNOME preferences are read on load and only explicit user actions change them.
- `features_group`: System features managed by updex (requires `updex` command)
- `dx_group`: Developer Mode; adds the invoking account to container, VM, and serial-device groups (shown only when `/usr/share/ublue-os/image-info.json` is present)
  - `wsl_backend`: Backend for WSL Mode (`nsl` or `lima`). Defaults to `nsl`; any other value is a configuration error
  - `install_pulp`: After a confirmed enable, install the Pulp feed reader (`org.gnome.gitlab.cheywood.Pulp`) as a user-scope Flatpak. Defaults to `false`. Unprivileged and opt-in: it installs for the invoking account only, and a failure here is reported as its own failure rather than rolling back developer access
  - `stage_feeds`: After a confirmed enable, write the curated developer feed catalog to `~/.local/share/chairlift/developer-feeds.opml` so the user can import it into their reader. Defaults to `false`. ChairLift writes the file and stops — nothing is imported automatically, and Pulp's own database is never touched. Disabling Developer Mode never removes Pulp, the staged file, or anything already imported from it
- `gaming_group`: Selective Gaming applications and runtime extensions, with installed user/system states, preserved system entries and visible partial failures (shown only when `/usr/share/ublue-os/image-info.json` is present)
- `printers_group`: Printer applications; one switch per driver family (Ghostscript, HPLIP, Gutenprint), each a rootless Podman quadlet under `~/.config/containers/systemd` driven with `systemctl --user`, with no `pkexec` route (shown only when `podman` is on `$PATH`). A family can be turned on only when its image's web administration can be authenticated or disabled ([ADR-0016](docs/adr/0016-printer-app-admin-denied-until-authenticated.md)); until the published images accept that setting, new enables are locked and say so. An existing unit remains manageable so it can be turned off. The rows evaluate systemd state, journal logs, and container images to diagnose and surface actionable failures (device access, image availability, plugin verification, service crash) rather than a false enabled indicator

The Developer group also offers WSL Mode (nsl by default, with Lima as alternative),
Docker, and individually selected IDEs and terminal editors, with one JetBrains
Toolbox entry. nsl requires an x86-64 Linux host; Lima supports amd64 and arm64.
Both require hardware virtualization and access to `/dev/kvm`. Missing fixed
helper actions disable only affected switches, not their discoverability.
KVM permission changes require a new login; Docker needs an accessible daemon
socket for this session. No user-configurable privileged argv is accepted.
The **WSL Backend** chooser changes the backend used in the current window;
`wsl_backend` configures the initial choice. With the nsl default, an existing
Lima Ubuntu machine and no nsl machine select Lima instead of creating another.


### Livery Page (`livery_page`)

Who you are, who you stand with, and what you roll with. Each livery section
shadows one icon-theme name inside the user's own theme, and the profile
picture goes through AccountsService as the invoking user, so nothing here is
privileged. Apart from the profile picture (staged in `$XDG_CACHE_HOME`, with a
face-file fallback in `$HOME`), nothing is written outside `$XDG_DATA_HOME` and
`$XDG_CONFIG_HOME`.

- `account_group`: Profile Picture; the account's picture, chosen from Project Bluefin's dinosaur artwork. Nothing is downloaded until a picture is picked, and it is set only on Apply, through AccountsService (`busctl`, unprivileged) with a `~/.face.icon`/`~/.face` fallback that takes effect at the next sign-in
- `livery_app_grid_group`: App Launcher Icon; the Show Applications mark on GNOME or the configured Kickoff applets on KDE Plasma, fetched from simpleicons.org by brand name. Set once — there is no rotation; unavailable on Plasma if no Kickoff applet is configured
- `livery_foundation_group`: Top Bar Icon; the GNOME top-bar menu mark, optionally advancing at each login (requires the Custom Command Menu GNOME extension; omitted on Plasma)
- `livery_dock_group`: Files Icon; the Files application icon, set to a CNCF project's own color mark from cncf/artwork and chosen with a searchable picker, optionally advancing at each login. GNOME stores one icon per application, so this changes Files everywhere it is drawn, not only on the dock

### Help Page (`help_page`)

- `troubleshooting_group`: Enhanced Troubleshooting; AI diagnostic assistant installed via Homebrew (moved here from Features, issue #249; shown only when Homebrew is present)
- `help_resources_group`: Help and support resources
  - `website`: URL to the project website
  - `issues`: URL to the issue tracker for bug reports and feature requests
  - `chat`: URL to the documentation. The key is named `chat` for backward compatibility; the link is titled "Documentation"

## Example: Disabling Homebrew Features

To create a distribution-specific configuration that disables all Homebrew features:

```yaml
updates_page:
  automatic_updates_group:
    # The unattended-update timer is the host's own; ChairLift can only turn
    # it on or off, not narrow what it touches, so a Homebrew-free
    # distribution hides the switch rather than offering an updater it
    # cannot scope.
    enabled: false
  bootc_updates_group:
    enabled: true
  flatpak_updates_group:
    enabled: true # Keep Flatpak updates
  brew_updates_group:
    enabled: false # Hide Homebrew updates, and drop the "Developer tools" source from the update run
  brew_trust_group:
    enabled: false # Hide Homebrew tap trust

applications_page:
  brew_group:
    enabled: false # Hide Homebrew packages
  brew_search_group:
    enabled: false # Hide Homebrew search and installs
  brew_bundles_group:
    enabled: false # Hide Homebrew bundles

agents_page:
  agents_group:
    enabled: false # Agent Mode installs llmman through Homebrew

# Developer IDE/editor installation also uses Homebrew; this disables the
# whole Developer group, including its permission switches.
features_page:
  dx_group:
    enabled: false

maintenance_page:
  maintenance_freespace_group:
    enabled: false # Hide shared cleanup, which includes Homebrew when installed

help_page:
  troubleshooting_group:
    enabled: false # Hide Homebrew-backed troubleshooting assistant
  help_resources_group:
    enabled: true
```

## Deployment

### For System Administrators

To customize ChairLift for your distribution:

1. Create a custom `config.yml` with your desired settings
2. Install it to `/etc/chairlift/config.yml` for system-wide configuration
3. Package maintainers can include it in `/usr/share/chairlift/config.yml`

### For OS Image Maintainers

ChairLift ships release archives for Homebrew and OS-image integration, not
deb, rpm, or apk packages. Install the maintainer defaults from the archive
at `/usr/share/chairlift/config.yml`, or stage a reviewed custom profile:

```bash
install -D -m 644 config.yml "$DESTDIR/usr/share/chairlift/config.yml"
```

Privileged features additionally need their fixed helpers and matching
PolicyKit actions; see the [installation surface](docs/index.md#installation).
Do not install defaults over administrator-owned `/etc/chairlift/config.yml`.

## Notes

- Groups with `enabled: false` will be completely hidden from the UI
- A configuration file is applied as an overlay on top of the built-in
  defaults, field by field, not as a full replacement:
  - An omitted `enabled` key inherits that group's documented default
    (`true` for every group except `maintenance_cleanup_group` and
    `reset_group`, which default to `false`)
  - An omitted optional field (`app_id`, `website`, `issues`, `chat`,
    `actions`, `bundles_paths`, `install_pulp`, `stage_feeds`, `wsl_backend`) inherits its
    documented default value
  - An explicit YAML `null` also inherits the built-in field value; use an
    empty list or empty string to clear an optional collection or string
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
