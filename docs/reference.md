# Configuration Reference

ChairLift is configured via a YAML file that controls which UI groups are visible and their behavior.

## File Locations

Configuration files are searched in order (first found wins):

1. `/etc/chairlift/config.yml` — system-wide (highest priority)
2. `/usr/share/chairlift/config.yml` — package maintainer defaults
3. `config.dev.yml` — source-checkout fallback, beside the executable when
   present, otherwise relative to the current working directory
4. `config.yml` — legacy development fallback, beside the executable when
   present, otherwise relative to the current working directory

Only a missing candidate advances the search. The first existing candidate is
authoritative. If it cannot be read or fails YAML/schema validation, ChairLift
hides every feature group, logs a `CONFIGURATION ERROR`, and displays a
persistent toast with the path and cause. Fix the file and restart Control Center.
If no file is found, built-in defaults apply: all groups are enabled except
`maintenance_cleanup_group` and `reset_group`.

Source and nFPM installs provide the repository's maintainer defaults at
`/usr/share/chairlift/config.yml`. Package upgrades may replace that file.
Administrators should put local changes in `/etc/chairlift/config.yml`, which
has higher precedence and is never created or overwritten by ChairLift's
packages.
The repository root includes `config.dev.yml` so source checkouts load
unprivileged development defaults before the package default `config.yml`.

## Format

```yaml
page_name:
  group_name:
    enabled: true/false
    # Optional per-group fields (see below)
```

Groups with `enabled: false` are hidden from the UI. Missing entries inherit
their built-in values. Unknown page, group, or field names and wrong field
types are errors. Changes require restarting ChairLift.

When every builder-backed group on a functional page is disabled, the page is
also omitted from the sidebar, content stack, shortcuts dialog, and
Alt+number bindings. Alt+number is compacted over the remaining pages in the
order below. Help is always retained.

## Pages and Groups

### Updates Page (`updates_page`)

| Group | Key | Description |
|-------|-----|-------------|
| Automatic updates | `automatic_updates_group` | The switch deciding whether this system installs updates on its own schedule; hidden on a host with no unattended-update timer. Updating now is the Updates page's own action and has no config key. |
| bootc Updates | `bootc_updates_group` | Download and stage the next bootc system image update (applies on restart); shown only when bootc-booted and the fixed `/usr/libexec/bootc-update-stage` helper is present. A distribution must provide a trusted implementation there before enabling this group; ChairLift's system-integration package does not supply one. |
| Flatpak Updates | `flatpak_updates_group` | Pending Flatpak application updates |
| Homebrew Updates | `brew_updates_group` | Outdated Homebrew packages with upgrade buttons |
| Untrusted Taps | `brew_trust_group` | Untrusted Homebrew taps with installed packages (Homebrew 6 tap trust); trust a tap to resume its updates. Shown only when there is something to trust |
| Release Channel | `channel_group` | Release channel and graphics-driver switching. Both replace the operating system and require a restart, so they sit with updates. Shown only when `/usr/share/ublue-os/image-info.json` is present |
| System Version | `bootc_status_group` | Compact booted/staged version readout, with image reference and build identifiers behind a details row; shown only when `bootc.IsBootcBootedCached()` reports a booted deployment |

### Apps Page (`applications_page`)

Its sidebar title is "Apps"; `applications_page` is the configuration key.

| Group | Key | Description |
|-------|-----|-------------|
| Installed Apps | `applications_installed_group` | Launcher for the external Flatpak manager used for discovery and installation |
| User Flatpak | `flatpak_user_group` | User-installed Flatpak applications with uninstall actions |
| System Flatpak | `flatpak_system_group` | System-wide Flatpak applications with uninstall actions |
| Homebrew | `brew_group` | Installed Homebrew formulae/casks with uninstall actions and formula pin/unpin actions |
| Brew Search | `brew_search_group` | Search and install Homebrew formulae and casks with an explicit package-type confirmation |
| App Collections | `brew_bundles_group` | Install a curated set of apps and tools in one step, discovered as `*.Brewfile` definitions |

`applications_installed_group` supports:

- `app_id` — Flatpak application ID to launch (default: `io.github.kolunmi.Bazaar`)

ChairLift does not directly discover or install new Flatpak applications.
Those operations belong to the configured external manager.

`brew_bundles_group` supports:

- `bundles_paths` — directories searched, without recursion, for
  `*.Brewfile` collections (default: `["/usr/share/ublue-os/homebrew"]`).
  Missing directories are ignored; other path errors are reported to the log
  while readable directories still contribute rows. Exact duplicate paths are
  collapsed, but same-named files in different directories remain separate.

Row text is resolved by `internal/views/bundleview`'s `Describe`, not taken
from the file: the collections Bluefin ships have tooling identifiers (`cli`,
`cncf`, `system-flatpaks`) and headings or provenance notes for comments, so
both the displayed name and the description come from a curated table. An
identifier that table does not know is humanized (`k8s-tools` → "K8s tools"),
and its leading `#` comment is used as the description only when it reads as
a human sentence rather than a heading or packaging jargon. Each row also
states how many apps and tools the definition installs, counting entry lines
other than `tap`.

### Agents Page (`agents_page`)

| Group | Key | Description |
|-------|-----|-------------|
| Agent Mode | `agents_group` | llmman installed with Homebrew (plus the Jan Flatpak on x86_64) and served as the systemd user unit `chairlift-llmman.service` on `127.0.0.1:17434`, with `OLLAMA_HOST` published to new sessions through `~/.config/environment.d/10-chairlift-llmman.conf`. Crosses no privilege boundary, so it has no `pkexec` route. Hidden where Homebrew is absent. See [ADR-0015](adr/0015-agent-mode-llmman.md) |

### Features Page (`features_page`)

| Group | Key | Description |
|-------|-----|-------------|
| Features | `features_group` | Toggle system features managed by updex |
| Developer Mode | `dx_group` | Adds the invoking account to container, VM, and serial-device groups; a confirmed live enable also opens the three developer onboarding tabs and, when configured, runs the optional feed setup below. Shown only when `/usr/share/ublue-os/image-info.json` is present |
| Gaming Mode | `gaming_group` | Toggles gaming optimizations; shown only when `/usr/share/ublue-os/image-info.json` is present |

`dx_group` supports two optional, default-off steps that run off the GTK main
thread after a confirmed live enable, and never on a disable, a page restore, a
failed helper call, or a `--dry-run` preview:

- `install_pulp` — installs the Pulp feed reader (`org.gnome.gitlab.cheywood.Pulp`) as a user-scope Flatpak from Flathub. No `pkexec`, no root: the same unprivileged posture as gaming mode
- `stage_feeds` — writes the curated catalog to `~/.local/share/chairlift/developer-feeds.opml` and says so in a toast that names the path. Importing it is the user's own action inside Pulp; ChairLift never writes to Pulp's sandboxed store and never claims a subscription was imported

The two outcomes are reported separately from developer access. The privileged
enable has already succeeded by the time these run, so a failed Flatpak install
says only that, and does not withdraw the groups — and disabling Developer Mode
is a clean no-op for Pulp, the staged file, and the feeds a user has imported
from it.

Feature operations (enable, disable, update) require administrator
authentication through PolicyKit and are performed by the fixed
`/usr/bin/chairlift-updex-helper` binary. ChairLift installs no passwordless
authorization rule.

### Livery Page (`livery_page`)

| Group | Key | Description |
|-------|-----|-------------|
| Profile Picture | `account_group` | The account's picture, chosen from Project Bluefin's dinosaur artwork; downloaded only when picked and set only on Apply, through AccountsService with a face-file fallback |
| App Grid Livery | `livery_app_grid_group` | The Show Applications mark, fetched from simpleicons.org by brand name; set once, never rotated |
| Foundational Livery | `livery_foundation_group` | The top-bar menu mark, optionally advancing at each login; needs a GNOME session with the Custom Command Menu extension (omitted on Plasma) |
| Dock Livery | `livery_dock_group` | The Files application icon, set to a CNCF project's color mark fetched from cncf/artwork via a searchable picker; changes Files everywhere GNOME draws it |

Selections persist in the `io.projectbluefin.chairlift.livery` GSettings
schema, one of the two ChairLift ships (the other,
`io.projectbluefin.chairlift.updates`, holds the user update preferences). A
source build has no installed schema — run `make schemas` and export the
`GSETTINGS_SCHEMA_DIR` it prints, or the page reports its settings
unavailable.

A failed bundle install reports the cause rather than the progress that
preceded it: ChairLift reads both of brew's output streams, because
`brew bundle` replays a failing entry's own installer output on stdout while
printing its summary on stderr, and shows the first error line it finds in a
persistent toast. Error toasts wrap, so a long message stays readable. The
complete captured output — bounded to the last 64 KiB per stream — is written
to ChairLift's log, which is where to look when filing a bug report.

### Maintenance Page (`maintenance_page`)

| Group | Key | Description |
|-------|-----|-------------|
| Storage | `maintenance_freespace_group` | The single "Free up space" action: `brew cleanup` plus `flatpak uninstall --unused`. The same key gates the post-update maintenance step of an update run, so cleanup cannot be on in one place and off in the other |
| Maintenance tasks | `maintenance_cleanup_group` | Administrator-configured scripts, listed separately and never folded into "Free up space" (disabled by default) |
| Recovery | `reset_group` | Powerwash (user Flatpaks and Distrobox containers) and Factory Reset (`bootc install reset --experimental`); irreversible actions disabled by default |

`maintenance_cleanup_group` supports:

- `actions` — list of scripts to offer:

```yaml
actions:
  - title: "Clean Up Boot Old Entries"
    script: "/usr/libexec/bls-gc"
    sudo: true
```

Each action has:

| Field | Description |
|-------|-------------|
| `title` | Display name |
| `script` | Absolute path to the script. Required when `sudo` is `true`. |
| `sudo` | If `true`, runs via `pkexec` for elevated privileges. Accepted only from trusted `/etc/chairlift/config.yml` or `/usr/share/chairlift/config.yml` configurations. The check covers the effective configuration: an untrusted file may not enable a group whose actions include a privileged one, including a privileged action inherited from the built-in defaults. |


### Help Page (`help_page`)

| Group | Key | Description |
|-------|-----|-------------|
| Enhanced Troubleshooting | `troubleshooting_group` | AI assistant for diagnosing system logs, services, and network; moved here from Features (issue #249); shown only when Homebrew is present |
| Resources | `help_resources_group` | Links to project resources |

Help also shows a **Feature availability** group — not configurable, and absent when empty — whose collapsed "Why is something missing?" row lists each group the configuration enables but the host cannot back, with the missing tool or file (`pageview.UnavailableFeatures`, from the capability set resolved at startup; issue #209).

`help_resources_group` supports:

| Field | Description |
|-------|-------------|
| `website` | Project website URL (default: `https://projectbluefin.io`) |
| `issues` | Issue tracker URL (default: `https://github.com/projectbluefin/dakota/issues`) |
| `chat` | Documentation URL (default: `https://docs.projectbluefin.io/`). The key is named `chat` for backward compatibility; the link is titled "Documentation" |

## Example

A configuration that disables all Homebrew features:

```yaml
applications_page:
  brew_group:
    enabled: false
  brew_search_group:
    enabled: false
  brew_bundles_group:
    enabled: false

updates_page:
  automatic_updates_group:
    enabled: false
  brew_updates_group:
    enabled: false
  brew_trust_group:
    enabled: false

maintenance_page:
  maintenance_freespace_group:
    enabled: false

help_page:
  troubleshooting_group:
    enabled: false
```

All other groups remain enabled by default since they are not listed (except
`maintenance_cleanup_group` and `reset_group`, which default to disabled).
