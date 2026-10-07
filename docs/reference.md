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

The shipped [`config.yml`](../config.yml) enables Apps collections but disables
the external catalog launcher, both Flatpak inventories, Homebrew inventory/export,
and Homebrew search. Built-in defaults enable all six Apps groups. Collections
remain independently configurable from inventory and export.
The full group inventory below describes available controls, not a promise
that every control appears with the shipped profile.

Source installs (`make install`) provide the repository's maintainer defaults
at `/usr/share/chairlift/config.yml`; an OS image may install the release
archive's `config.yml` there instead. Whoever installs that file may replace it
on upgrade. Administrators should put local changes in
`/etc/chairlift/config.yml`, which has higher precedence and is never created
or overwritten by ChairLift's install paths.
The repository root includes `config.dev.yml` so source checkouts load
unprivileged development defaults before the package default `config.yml`.

Appearance preferences share the desktop's native dconf store. Before GUI
startup or headless `--rotate-livery`, ChairLift discovers the installed native
GIO dconf module and appends its directory to `GIO_EXTRA_MODULES`, preserving
existing module entries and any explicit `GSETTINGS_BACKEND`. This avoids
Homebrew GLib using a separate keyfile store from the desktop or rotation unit.

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

The configuration is an overlay on built-in defaults: omitted or explicitly
null fields inherit, while an explicit empty list or empty string clears the
field. A non-empty list replaces the default rather than appending to it.
See [`CONFIG.md`](../CONFIG.md#notes) for the complete overlay rules.

When every builder-backed group on a functional page is disabled, the page is
also omitted from the sidebar, content stack, shortcuts dialog, and
Alt+number bindings. Alt+number is compacted over the remaining pages in the
order below. Help is always retained.

The host capability floor also participates in page visibility: enabling a
group cannot supply its missing backing tool or asset. Runtime readiness is
checked separately after the page is built.

## Pages and Groups

### Updates Page (`updates_page`)

| Group | Key | Description |
|-------|-----|-------------|
| Automatic updates | `automatic_updates_group` | The switch deciding whether this system installs updates on its own schedule; hidden on a host with no unattended-update timer. Updating now is the Updates page's own action and has no config key. |
| bootc Updates | `bootc_updates_group` | Download and stage the next bootc system image update (applies on restart); shown only when bootc-booted and the fixed `/usr/libexec/bootc-update-stage` helper is present. A distribution must provide a trusted implementation there before enabling this group; ChairLift's release archive does not supply one. |
| Flatpak Updates | `flatpak_updates_group` | Pending Flatpak application updates |
| Homebrew Updates | `brew_updates_group` | Outdated Homebrew packages with upgrade buttons |
| Untrusted Taps | `brew_trust_group` | Untrusted Homebrew taps with installed packages (Homebrew 6 tap trust); trust a tap to resume its updates. Shown only when there is something to trust |
| Release Channel | `channel_group` | Release channel and graphics-driver switching. Both replace the operating system and require a restart, so they sit with updates. Shown only when `/usr/share/ublue-os/image-info.json` is present |
| System Version | `bootc_status_group` | Compact booted/staged version readout, with image reference and build identifiers behind a details row; shown only when `bootc.IsBootcBootedCached()` reports a booted deployment |

### Apps Page (`applications_page`)

Its sidebar title is "Apps"; `applications_page` is the configuration key.

| Group | Key | Description |
|-------|-----|-------------|
| App collections | `brew_bundles_group` | Install a curated set of apps and tools in one step, discovered as `*.Brewfile` definitions; displayed first |
| Homebrew packages | `brew_group` | Installed apps (installed casks), then Command line tools (explicitly requested formulae), then Backup containing the Export app list row (a Brewfile export); uninstall and formula pin/unpin actions |

Both groups are enabled in the shipped `config.yml`. Dependency-only formulae
do not appear in the inventory. Apps has no Flatpak inventory, external catalog
launcher, or package search. The retired `applications_installed_group`,
`flatpak_user_group`, `flatpak_system_group`, and `brew_search_group` keys are
accepted and ignored for compatibility, as described in [CONFIG.md](../CONFIG.md).

`brew_bundles_group` supports:

- `bundles_paths` — directories searched, without recursion, for
  `*.Brewfile` collections. The built-in default in `internal/config/config.go`
  is `["/usr/share/ublue-os/homebrew", "/usr/share/chairlift/bundles", "/etc/chairlift/bundles"]`;
  the shipped `config.yml` narrows it to `["/usr/share/ublue-os/homebrew"]`.
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

A failed bundle install reports the cause rather than the progress that
preceded it: ChairLift reads both of brew's output streams, because
`brew bundle` replays a failing entry's own installer output on stdout while
printing its summary on stderr, and shows the first error line it finds in a
persistent toast. Error toasts wrap, so a long message stays readable. The
complete captured output — bounded to the last 64 KiB per stream — is written
to ChairLift's log, which is where to look when filing a bug report.

### Agents Page (`agents_page`)

| Group | Key | Description |
|-------|-----|-------------|
| Agent Mode | `agents_group` | llmman installed with Homebrew and served as the systemd user unit `chairlift-llmman.service` on `127.0.0.1:17434`, with `OLLAMA_HOST` published to new sessions through `~/.config/environment.d/10-chairlift-llmman.conf`. Offers a "Models and Chat" row that opens llmman's web UI while it is ready, and Contribute to Bluefin via `ujust contribute`. When llmman is missing, enabling taps `llmmanorg/tap` and trusts exactly `llmmanorg/tap/llmman` before installing it. Crosses no privilege boundary, so it has no `pkexec` route. Hidden where Homebrew is absent. See [ADR-0015](adr/0015-agent-mode-llmman.md) |
| Troubleshooting | `troubleshooting_group` | The Goose row and the "Show Ask Bluefin in menu" shortcut preference. **Set Up** installs `ublue-os/tap/linux-mcp-server`, `cpio`, and the `ublue-os/tap/goose-linux` cask (x86_64 only) with Homebrew; **Launch** writes ChairLift's own Goose profile under `$XDG_DATA_HOME/chairlift/troubleshooting` — `linux-tools` with `--toolset FIXED --host-mode LOCAL_ONLY --no-search-for-ssh-key` and the online Project Bluefin knowledge search, nothing else — then runs `llmman launch goose-desktop --model bluefin-active` inside it; if Goose is already running there, the request goes to that session and no second Goose starts (GNOME may show a "Goose is ready" notification instead of raising the window). `~/.config/goose` is never read or written. Crosses no privilege boundary. Hidden where Homebrew is absent. Formerly `help_page.troubleshooting_group`, still accepted |

### Features Page (`features_page`)

| Group | Key | Description |
|-------|-----|-------------|
| Desktop integrations | `desktop_integrations_group` | User-session GNOME extension switches for Tailscale and Sync Folder; unavailable extensions are explained, and existing GNOME choices are preserved |
| Features | `features_group` | Toggle system features managed by updex |
| Developer Mode | `dx_group` | Adds the invoking account to container, VM, and serial-device groups; a group the image does not define is skipped and named in a warning rather than reported as granted. A confirmed live enable also opens one developer documentation page and, when configured, runs the optional feed setup below. Shown only when `/usr/share/ublue-os/image-info.json` is present |
| Gaming Mode | `gaming_group` | Selectively installs chosen Flatpak applications and runtime extensions system-wide (`flatpak install --system`) from the system Flathub remote Bluefin-family images configure, authorized by Flatpak's own PolicyKit rather than by ChairLift; reports user and system installed scopes and persistent partial failures. A copy an earlier release installed per-user counts as installed. Remove Selected removes each selected app from every scope it is installed in, after a confirmation that system-wide copies go for every account, except a system copy the OS image declares it ships (Flatpak `preinstall.d` or `/usr/share/ublue-os/homebrew/system-flatpaks.Brewfile`), which is left in place; shown only when `/usr/share/ublue-os/image-info.json` is present |
| Printers | `printers_group` | Printer applications: one switch per driver family (Ghostscript, HPLIP, Gutenprint), each a rootless Podman quadlet under `~/.config/containers/systemd` driven with `systemctl --user`. Crosses no privilege boundary, so it has no `pkexec` route. Hidden where `podman` is absent. A family may be turned on only when its web administration is authenticated or absent ([ADR-0016](adr/0016-printer-app-admin-denied-until-authenticated.md)); until the published images accept that setting new enables are locked and the row says what is needed. Existing units remain manageable for disabling. The rows evaluate systemd state, journal logs, and container images to diagnose actionable failures (device access, image availability, plugin verification, service crash) rather than a false enabled indicator |

Developer options include WSL Mode (an Ubuntu machine in systemd-vmspawn via
nsl by default, or Ubuntu LTS in Lima). nsl needs an x86-64 Linux host;
Lima supports amd64 and arm64. Both require hardware virtualization and
`/dev/kvm` access. The group also offers the base image's
Docker daemon, and individually chosen IDEs/editors with one JetBrains Toolbox
entry. Missing fixed helper actions keep affected switches discoverable but
insensitive. KVM permission requires a new login; Docker readiness requires
the real daemon socket to be accessible, not just installed CLI tools. These
privileged operations use only `kvm-enable`, `docker-enable`, and
`docker-disable` with fixed argv and the account derived from `PKEXEC_UID`.

The **Virtual machine engine** chooser (Built-in for nsl, or Lima) changes the current window's backend without rewriting
YAML. `wsl_backend` supplies the initial choice; an existing Lima Ubuntu machine
with no ChairLift nsl machine resolves the nsl default to Lima. nsl keeps using
the `debian` machine an older ChairLift created instead of creating `ubuntu`
beside it, and the chooser then says the built-in engine runs Debian.

`dx_group` takes `wsl_backend`, the WSL Mode backend: `nsl` (default) or
`lima`; any other value is a configuration error. It also supports optional steps that run off the GTK main
thread after a confirmed live enable, and never on a disable, a page restore, a
failed helper call, or a `--dry-run` preview:

- `install_pulp` — installs the Pulp feed reader (`org.gnome.gitlab.cheywood.Pulp`) as a system-scope Flatpak from the system Flathub remote, unless a copy is already installed in either scope. No `pkexec`: like gaming mode, the `flatpak` CLI authorizes it through Flatpak's own PolicyKit
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
| App Launcher Icon | `livery_app_grid_group` | The Show Applications mark on GNOME, or the configured Kickoff applets on KDE Plasma; fetched from simpleicons.org by brand name and set once, never rotated. On Plasma, the group is unavailable when no Kickoff applet is configured |
| Top Bar Icon | `livery_foundation_group` | The GNOME top-bar menu mark, optionally advancing at each login; needs a GNOME session with the Custom Command Menu extension (omitted on Plasma) |
| Files Icon | `livery_dock_group` | The Files icon on GNOME or Dolphin on KDE Plasma, set to a CNCF project's color mark fetched from cncf/artwork via a searchable picker; on GNOME it changes Files everywhere GNOME draws it |

Selections persist in the `io.projectbluefin.chairlift.livery` GSettings
schema, one of the three ChairLift ships (the others,
`io.projectbluefin.chairlift.updates` and `io.projectbluefin.chairlift.firstrun`,
hold the user update preferences and the setup assistant's disposition). A
source build has no installed schema — run `make schemas` and export the
`GSETTINGS_SCHEMA_DIR` it prints, or the page reports its settings
unavailable. An install hits the same condition when it ships the schema XML
without compiling it where GSettings searches (`/usr/share/glib-2.0/schemas`,
the `glib-2.0/schemas` directory of any `$XDG_DATA_DIRS` entry, or the user's
`~/.local/share/glib-2.0/schemas`); `make install` recompiles the system cache
for a direct install, and a Homebrew cask has to compile the user directory
itself.

Livery publishes selections and master-switch state through the pure
`internal/views/liverystate` transitions. A failed save or dry-run preview
keeps the confirmed selection and switch; when a selection saves but its
artwork fails, the persisted selection is shown and a toast names the failed
step. Queued results recheck their shared serializer before publishing.
Rotation candidates overlay the confirmed pair while work is in flight.
Unlike an artwork selection, a rotation preference commits only after the
user manager accepts its unit; failure preserves the previous preference
pair and unit file, and dry-run restores both switches without writes.


### Maintenance Page (`maintenance_page`)

| Group | Key | Description |
|-------|-----|-------------|
| Storage | `maintenance_freespace_group` | The single "Free up space" action: `brew cleanup` plus `flatpak uninstall --unused`. The same key gates the post-update maintenance step of an update run, so cleanup cannot be on in one place and off in the other |
| Maintenance tasks | `maintenance_cleanup_group` | Administrator-configured scripts, listed separately and never folded into "Free up space" (disabled by default) |
| Powerwash | `reset_group` | Powerwash (user Flatpaks and Distrobox containers) and Factory Reset (`bootc install reset --experimental`), irreversible actions disabled by default. Roll Back and Published versions (Pin, Go back to regular updates) are gated by `updates_page.bootc_updates_group`; the detail retains its `recovery` route identity |

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
| Resources | `help_resources_group` | Links to project resources |

`help_page.troubleshooting_group`, Troubleshooting's former Help
key, is still accepted and moved to `agents_page.troubleshooting_group`
(below); a value set under `agents_page` wins.

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
  brew_bundles_group:
    enabled: false

agents_page:
  agents_group:
    enabled: false
  troubleshooting_group:
    enabled: false

features_page:
  dx_group:
    enabled: false # Also hides non-Homebrew Developer controls

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
```

All other groups remain enabled by default since they are not listed (except
`maintenance_cleanup_group` and `reset_group`, which default to disabled).
