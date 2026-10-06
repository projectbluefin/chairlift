# Destination and action ownership matrix

## Overview

This is the shipped destination inventory, not a relocation plan. Route names,
titles, order, parentage and page-qualified configuration references are owned
by `internal/navigation`. Widget mounting is in `internal/window`; action
controllers remain in `internal/views` and their existing provider packages.
See [overview](overview.md#pages) for architecture and
[package-managers](package-managers.md) for execution boundaries.

## Destination map

The seven primaries, in canonical order, are **Updates, Apps, Agents, Features,
Livery, Maintenance, Help**. The `recovery` route, titled **Powerwash**, is the
only detail, owned by Maintenance;
it has no sidebar row or accelerator. There is no System, Appearance, Developer
Tools or Local AI tools route. Wallpaper is not a shipped control.

| Route | Builder under `internal/views` | Content |
| --- | --- | --- |
| `updates` | `updates_page.go`, `update_shell.go` | Unified status/action and source inventory; automatic updates; system version; staging and Compare; tap trust; channel and graphics controls |
| `applications` | `applications_page.go` | Homebrew collections, installed casks, explicitly requested formulae and Brewfile export, in that order; no Flatpak inventory, external catalog launcher or package search |
| `agents` | `agents_page.go`, `troubleshoot.go`, `contribute.go` | Local Agent Mode, model selection/presets, Models and Chat link, Troubleshooting's Goose row (Set Up, then Launch in ChairLift's own profile) and menu visibility, and Contribute to Bluefin |
| `features` | `features_page.go`, `developer_tools.go`, `shell_extensions.go`, `printers_page.go` | Distribution features, desktop integrations, Developer options, selected gaming components and locked printer applications |
| `livery` | `livery_page.go`, `livery_actions.go`, `profile_picture.go` | Profile Picture, App Launcher Icon, Top Bar Icon and Files Icon, with login rotation for the foundation surfaces |
| `maintenance` | `maintenance_page.go` | Free up space, trusted administrator scripts and Powerwash entry |
| `help` | `help_page.go` | Support links, diagnostics and capability explanations |
| `recovery` | `recovery.go`, `reset.go`, `versions.go` | Previous-deployment rollback, live published-version reads, confirmed pin/return-to-stream and opt-in reset actions |

## Configuration references and owners

A configuration page is a stable YAML namespace, not a display title. Each
`navigation.Ref` is `{Page, Group}`; Powerwash consumes both Updates and
Maintenance namespaces. A shared group does not authorize a second state owner
or a duplicated builder. This table covers the current `config.SchemaGroups`
inventory; derive schema additions from source rather than a frozen count.

| Configuration page | Group | Current mount / owner |
| --- | --- | --- |
| `updates_page` | `automatic_updates_group` | Updates; `onAutomaticUpdatesToggled`, observed through `internal/autoupdate`, writes via `ublue.SetAutomaticUpdates` |
| `updates_page` | `bootc_updates_group` | Updates staging/Compare and Powerwash rollback/catalog/pin/unpin; `onBootcStageClicked`, `onChangelogClicked`, `onBootcRollbackClicked`, `onPublishedVersionsClicked`, `confirmPin` / `runPin`, `confirmReturnToStream` / `runReturnToStream` |
| `updates_page` | `flatpak_updates_group` | Updates applications source; `updateflow.Coordinator` / `UpdateShell` |
| `updates_page` | `brew_updates_group` | Updates developer-tools source; `updateflow.Coordinator` / `UpdateShell` |
| `updates_page` | `brew_trust_group` | Updates; `confirmTrustTap` / `trustTap`, unprivileged `homebrew.TrustPackages` |
| `updates_page` | `channel_group` | Updates Advanced; `onChannelToggled` / `onDriverSwitchClicked`, fixed ublue helper |
| `updates_page` | `bootc_status_group` | Updates system-version readout; `loadSystemVersion`, read-only bootc state |
| `applications_page` | `brew_bundles_group` | Apps collections, shown first; `loadBrewBundles`, shared collection install/progress handling |
| `applications_page` | `brew_group` | Apps installed casks then explicitly requested formulae, followed by Brewfile export; `loadHomebrewPackages`, `runHomebrewUninstall`, `runHomebrewPin`, `onBrewBundleDumpClicked` |
| `features_page` | `features_group` | Features toggles/update checks plus unified system-components source; `onFeatureToggled`, `onUpdateFeaturesClicked`, `internal/updex` |
| `features_page` | `desktop_integrations_group` | Features Tailscale/Sync Folder switches; `internal/shellextensions`, observed GNOME state |
| `features_page` | `dx_group` | Features Developer Mode, WSL/Docker and selected IDE/editor installs; `onDeveloperToggled` / `onDeveloperOption`; fixed helper plus user-scope `internal/devtools` |
| `features_page` | `gaming_group` | Features selected gaming refs; `onGamingSelected` / `runGamingSelected`, `internal/gaming` |
| `features_page` | `printers_group` | Features Printers; `onPrinterAppToggled`, `internal/printerapp` readiness and authenticated-administration enable gate |
| `agents_page` | `agents_group` | Agents service/model/presets, Models and Chat link and contributor launch; `internal/aistack`, `internal/contribute` |
| `agents_page` | `troubleshooting_group` | Troubleshooting: Goose Set Up/Launch and Ask Bluefin menu visibility; `onGooseClicked`, `internal/agentmode`, `internal/troubleshoot`, `internal/devmenu`. Also accepted as legacy `help_page.troubleshooting_group` |
| `livery_page` | `account_group` | Livery profile picture; `avatarPicker`, `internal/avatar.Applier` |
| `livery_page` | `livery_app_grid_group` | Livery app-grid mark; app-grid controller and `internal/livery` |
| `livery_page` | `livery_foundation_group` | Livery panel mark/login rotation; `livery.Panel` controller |
| `livery_page` | `livery_dock_group` | Livery Files application mark/login rotation; `livery.Dock` controller, not a dock-only icon |
| `maintenance_page` | `maintenance_freespace_group` | Maintenance manual cleanup and unified post-update cleanup; `updateproviders.CleanupGroup`, one typed cleanup runner |
| `maintenance_page` | `maintenance_cleanup_group` | Maintenance configured `actions[]`; `runMaintenanceAction` → `pageview.MaintenanceCommand` → `internal/maintenanceexec` |
| `maintenance_page` | `reset_group` | Powerwash detail Powerwash/Factory Reset; `onPowerwashClicked` / `onFactoryResetClicked`, each confirmed before dispatch |
| `help_page` | `help_resources_group` | Help website/issues/chat; `openURL` through `internal/launcher` |

Important shared boundaries:

- `channel_group` gates both release-channel and graphics-driver changes **in
  Updates**; no System destination or replacement configuration key exists.
- Homebrew has one installed inventory loader and generation guard; confirmed
  actions refresh installed rows only after live success. Collections remain independently configurable and nil-safe when
  `brew_group` is disabled. Export's handler file does not change its Apps mount.
- `bootc_updates_group` retains its Updates namespace for Powerwash reads/actions.
  The Powerwash detail is offered only when its Maintenance ancestor is visible and one
  of its page-qualified refs is effectively enabled.
- `maintenance_freespace_group` gates both manual and post-update cleanup.
  Configured administrator scripts are a separate opt-in surface, not cleanup
  steps in the unified run.
- Legacy `system_page` is validated migration input, not a current schema page
  or route: status/channel settings migrate to Updates; information/health
  groups have no runtime effect. Separate package-manager cleanup groups are
  retired and must not be revived. The retired Apps groups
  (`applications_installed_group`, `flatpak_user_group`, `flatpak_system_group`,
  `brew_search_group`) are likewise accepted, validated and stripped; they have
  no route or runtime effect.

## Action and state boundaries

The pure `updateflow.Coordinator` owns source inventory, phases and aggregate
counts. The shell is snapshot rendering and event wiring; production
`internal/updateproviders` wraps existing Flatpak, Homebrew, updex and bootc
entry points. `UpdateShell.beginMutation` admits manual item updates, metadata
refresh, dedicated staging and the unified run. Failed observations preserve
confirmed state; dry-run previews mutate none of it. Restart is offered from
the Operating system row only after an observed staged deployment, through the
fixed `ublue.Restart` action, not an invented update executor.

Provider-specific safety remains with each live owner:

- Staging uses the fixed bootc stage path and bounded streamed logs. Compare
  starts only on a click, with pinned image references and stale-result guards.
  Published versions are read-only registry observations. Pin and Return to stream
  require confirmation and installed helper support; only a validated day or
  fixed unpin word crosses pkexec, never a registry-supplied image reference.
  Roll Back uses the existing previous deployment and completes its gate after
  live success, without restarting.
- Homebrew uninstall/pin actions confirm intent and retain typed target
  identity. Failure and preview restore controls; refresh
  generations reject stale workers. Visible retryable controls reset their gates.
- Developer Mode opens one onboarding page and optional Pulp/feed work only after a
  confirmed live enable; developer groups the helper skipped are named in a
  warning. `install_pulp` and `stage_feeds` are opt-in user-scope
  work; their failures do not reverse or misreport the permission promotion.
  WSL defaults to nsl (Linux amd64), with Lima available explicitly and retained
  for an existing Lima-only machine. Both need KVM access; Docker requires an
  actually ready socket. Missing helper actions leave affected options visible
  but insensitive.
- Gaming validates selected refs, distinguishes applications/runtimes and
  user/system scope, preserves system installations and reports partial failures.
- Agent Mode is local and unprivileged: user service, loopback endpoint, no
  peer/offload controls. Readiness is observed HTTP health after the owned unit's
  restart/invocation-stamp boundary, not file presence or optimistic switch state.
  Goose Desktop launches through `llmman launch goose-desktop --model
  bluefin-active` only after architecture/package/daemon/model checks, in a
  profile ChairLift rewrites before every launch; `~/.config/goose` is never
  read or written, and one session runs at a time. Ask Bluefin uses the same
  readiness dispatcher.
  Contribute requires terminal/ujust recipe/Podman/registration preflight and
  launches `xdg-terminal-exec ujust contribute`, without a privileged route.
- Printers are rootless quadlets. New enables are refused until administration
  is authenticated or absent; the locked rows remain visible and off. Existing
  units stay disableable; a failed stop preserves the management file.
  Installed units are diagnosed from systemd, user journal and Podman image
  observations; plugin, device-access, crash and image failures remain distinct.
- Livery applies no mutations on load or preview, retains confirmed state across
  failed writes and serializes rotation scheduling. Catalog searches cancel
  older fetches and discard stale generations. Profile-picture application is
  unprivileged AccountsService or a disclosed face-file fallback.
- Powerwash and Factory Reset are opt-in and destructive-confirmed. Factory
  Reset sends only its fixed command word; no caller-supplied reset target
  crosses the helper boundary. Manual cleanup never claims skipped or failed
  steps succeeded, or invents reclaimed-byte figures.
- Troubleshooting's Set Up installs only missing packages (user-scope Homebrew)
  and previews without tapping; its Goose profile enables only the read-only
  Linux tools and the online knowledge search.
- Privileged intent is journalled at shared choke points. Live helper outcomes
  distinguish success, refusal, failure, timeout and cancellation and include
  self-reported derived argv where available; markers are an audit aid, not proof.

Livery uses an embedded, theme-adaptive foundation `GtkFlowBox` with one
activation signal. The shared catalog chooser fetches only its visible page of
at most twelve search matches plus Custom SVG; new searches cancel old fetches
and generation checks discard stale artwork. Rotation scheduling serializes
systemd changes and retains confirmed preferences when a unit change fails.

All external work runs off GTK; widget updates return through
`sgtk.RunOnMainThread`. Configuration and capability are distinct facts:
configuration may subtract from the host floor, never add to it. Nil-guard
cross-group widget access, connect reusable signals once, and retain one owner
for each mutation and observation.

## Global actions and navigation

| Action | Single owner / relationship |
| --- | --- |
| Sidebar, Alt+number, F1 Help, Powerwash entry/Back | `Window.navigateToPage` applies `navigation.Resolve`: selected primary row, visible child, title, collapsed-content reveal and recorded Back parent |
| Preferences menu aliases | `Window.buildPreferences`, shared Updates GSettings source/maintenance preferences; no new configurable group |
| Check | `UpdateShell.StartCheck`, same coordinator as the page action |
| Configured Help website | `Window.setupActions`, GIO launch when a URL is present |
| Setup Assistant, `--first-run`, `--setup`/`-s` | `Window.PresentFirstRun`; existing visible pages in Features → Apps → Agents → Livery order, no separate destination or copied controls |
| Keyboard Shortcuts | `Window.onShowShortcuts`; advertised and registered inventory from `internal/navigation` |
| About Control Center | `Window.onShowAbout`; application About dialog, not a machine-information route |
| `--ask-bluefin` | `internal/app` command-line handler and `internal/agentmode.Dispatch`; launch ready Goose (off the GTK thread) without presenting a window, otherwise open Agents with prerequisite, open-session, or start-failure feedback |
| Quit / close while updating | `internal/app` lifecycle and window guard consulting `UpdateShell.Busy` |

The visible primary inventory is fixed for the session and Alt+number compacts
over it. Details never acquire shortcuts. Powerwash keeps Maintenance selected;
Back resolves the recorded primary through the same navigation path. A known
hidden/unbuilt detail resolves to its visible ancestor, then Help; a known
hidden/unbuilt primary resolves to Help. Only an unknown route is rejected.
Navigation transitions execute no mutations. Setup Back/Next also mutate no
settings; Finish/dismissal record disposition asynchronously, and previews write
nothing. Ordinary activation never opens setup automatically.

## Decision references

Use actual filenames/subjects rather than copied issue numbering:

- [ADR-0013](../adr/0013-rollback-catalog-reads-the-registry-live.md) covers the
  live read-only registry catalog, not capability visibility.
- [ADR-0014](../adr/0014-capability-driven-visibility-as-a-floor.md) covers the
  capability floor. Its historical `0013` heading does not change file identity;
  accepted decision text remains untouched.
- [ADR-0015](../adr/0015-agent-mode-llmman.md) covers Agent Mode using llmman.
- [ADR-0016](../adr/0016-printer-app-admin-denied-until-authenticated.md) covers
  printer administration authentication.
- [Setup model](../specs/setup-model.md) records the existing-page, navigation-only
  flow. Future route proposals belong in live issues and require explicit
  implementation/decision changes, not a second proposed registry in this file.
