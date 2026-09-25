# Destination and action ownership matrix

## Overview

This is the source inventory and relocation contract for
[#344](https://github.com/projectbluefin/chairlift/issues/344), parcel 3 of
[#241](https://github.com/projectbluefin/chairlift/issues/241), under
[#233](https://github.com/projectbluefin/chairlift/issues/233).
The target organization below is proposed implementation work, **not the current
sidebar or an accepted ADR**. The current seven primary pages remain usable
until [#201](https://github.com/projectbluefin/chairlift/issues/201) performs
the explicit cutover. The [architecture overview](overview.md#pages) describes
those current mounts. No configuration keys, providers, or privileges change
with this inventory.

## Design

### Destination map

Names below are review labels, not a second route registry or new YAML keys.
Canonical route identity and parentage belong to `internal/navigation` as
extended by [#342](https://github.com/projectbluefin/chairlift/issues/342);
widget composition belongs to [#343](https://github.com/projectbluefin/chairlift/issues/343).

```text
Control Center
├── Updates
│   ├── Aggregate status and update action
│   ├── Settings (automatic updates, channel, source preferences)
│   ├── Sources (provider detail and tap trust)
│   └── Changes (SBOM compare)
├── Apps
│   ├── Installed apps
│   ├── Collections and gaming
│   ├── Developer Tools
│   └── Local AI tools
├── Appearance
│   ├── Profile picture
│   ├── Wallpaper
│   └── Icons (app grid, panel, Files)
├── System
│   ├── About
│   ├── Storage
│   ├── Graphics
│   ├── Distribution features
│   ├── Administrator maintenance
│   └── Recovery
└── Help
    ├── Troubleshooting and support
    └── Capability explanations
```

Wallpaper is a destination requirement, not an existing configurable group or
working control. System About is the proposed machine-information destination;
it must not be confused with the existing application About dialog. Neither
requires a placeholder page in the preparatory parcels.

### Configuration references and owners

**The configuration-to-navigation one-to-one assumption is retired.** Keep the
seven original YAML namespaces. Every group reference carries both its original
configuration page and its group; display names never determine configuration
identity. A destination can consume several namespaces, and a group can supply
more than one destination without creating a second mutation or state owner.

```text
(original config page, group) + capability floor
                       │
                       ▼
             one live action/state owner
                       │
                       ▼
              canonical destination
                       ▲
                       │
       primary entry / detail / deep link
```

The table is total over the 26 groups in `config.SchemaGroups`, derived from
`defaultConfig()` in `internal/config/config.go`. The action IDs join to the
next table. A row with multiple destinations is an explicit split of content,
not permission to run its builder twice.

| Original configuration page | Canonical group | Current mount | Target destination(s) | Action IDs |
| --- | --- | --- | --- | --- |
| `updates_page` | `automatic_updates_group` | Updates | Updates / Settings | U2 |
| `updates_page` | `bootc_updates_group` | Updates; Recovery detail | Updates / Sources, Changes; System / Recovery | U3, U4, S5, S6 |
| `updates_page` | `flatpak_updates_group` | Updates | Updates / Sources | U5 |
| `updates_page` | `brew_updates_group` | Updates | Updates / Sources | U6, U7 |
| `updates_page` | `brew_trust_group` | Updates | Updates / Sources | U8 |
| `updates_page` | `channel_group` | Updates | Updates / Settings; System / Graphics | U9, S1 |
| `updates_page` | `bootc_status_group` | Updates | System / About (version summary remains available to Updates) | S2 |
| `applications_page` | `applications_installed_group` | Apps | Apps / Installed apps | A1 |
| `applications_page` | `flatpak_user_group` | Apps | Apps / Installed apps | A2 |
| `applications_page` | `flatpak_system_group` | Apps | Apps / Installed apps | A3 |
| `applications_page` | `brew_group` | Apps | Apps / Installed apps; Apps / Developer Tools | A4, A5, A6 |
| `applications_page` | `brew_search_group` | Apps | Apps / Developer Tools | A7 |
| `applications_page` | `brew_bundles_group` | Apps | Apps / Collections and gaming | A8 |
| `features_page` | `features_group` | Features | System / Distribution features | S3, S4 |
| `features_page` | `dx_group` | Features | Apps / Developer Tools | A9, A10 |
| `features_page` | `gaming_group` | Features | Apps / Collections and gaming | A11 |
| `agents_page` | `agents_group` | Agents | Apps / Local AI tools | A12 |
| `livery_page` | `account_group` | Livery | Appearance / Profile picture | P1 |
| `livery_page` | `livery_app_grid_group` | Livery | Appearance / Icons | P2 |
| `livery_page` | `livery_foundation_group` | Livery | Appearance / Icons | P3 |
| `livery_page` | `livery_dock_group` | Livery | Appearance / Icons | P4 |
| `maintenance_page` | `maintenance_freespace_group` | Maintenance; post-update runner | System / Storage | S7 |
| `maintenance_page` | `maintenance_cleanup_group` | Maintenance | System / Administrator maintenance | S8 |
| `maintenance_page` | `reset_group` | Recovery detail | System / Recovery | S9, S10 |
| `help_page` | `troubleshooting_group` | Help | Help / Troubleshooting and support | H1, H2 |
| `help_page` | `help_resources_group` | Help | Help / Troubleshooting and support | H3 |

Explicit shared-group rules:

- `channel_group` continues to gate **both** channel switching in Updates and
  graphics switching in System. The controls have different actions (U9 and
  S1), each with one owner; neither gets a replacement configuration key.
- `brew_group` contributes GUI inventory to Installed apps and specialist
  formula detail/export to Developer Tools. `loadHomebrewPackages` remains
  the one installed Homebrew inventory loader, with its existing generation
  guard. Do not duplicate it or its mutation gates for the two presentations.
  `brew_bundles_group` stays independently configurable and nil-safe when
  `brew_group` is disabled.
- `bootc_updates_group` already spans staging, comparison and Recovery. Keep
  its original namespace for rollback/catalog reads after relocation too.
- `maintenance_freespace_group` gates both the manual cleanup and the composed
  post-update step through `updateproviders.CleanupGroup`. Both use S7's runner.

Legacy `system_page` aliases are migration input, not canonical groups or new
routes: `bootc_status_group` and `channel_group` migrate into Updates;
`system_info_group` and `health_group` are retired. The former
`maintenance_brew_group` and `maintenance_flatpak_group` are not current schema
groups. Do not resurrect their separate cleanup buttons.

### Action/route matrix

Each row records exactly one owning action controller (or state owner for a
read-only row). The provider column is the existing execution dependency, not
an additional UI owner. Names without a package prefix are `UserHome` methods
in `internal/views`. Bundled verbs in a row share the stated owner. Internal
refresh callbacks and confirmation/cancel paths stay with that action; they
are not new routes. A target route never creates a second copy of the owner.

| ID | Existing action / state | Target route | Single action/state owner | Existing provider or boundary |
| --- | --- | --- | --- | --- |
| U1 | Check, Update All, retry and aggregate progress | Updates | `updateflow.Coordinator` via `UpdateShell` | `internal/updateproviders` composes Flatpak, Homebrew, updex and bootc; no new executor |
| U2 | Enable/disable automatic updates | Updates / Settings | `onAutomaticUpdatesToggled` | `ublue.SetAutomaticUpdates`; state reads via `internal/autoupdate` |
| U3 | Stage OS update and show progress | Updates / Sources | `onBootcStageClicked` | `internal/bootc` via `internal/stageexec`; same stage path used by U1 |
| U4 | Compare running/staged SBOM; expand diff | Updates / Changes | `onChangelogClicked` and its pinned comparison state | `internal/sbom`; explicit read, no background fetch on navigation |
| U5 | Read pending Flatpak updates; update each app | Updates / Sources | `loadFlatpakUpdatesGeneration` row action | `internal/flatpak`; scope retained |
| U6 | Read outdated formulae/casks; upgrade each | Updates / Sources | `loadOutdatedPackagesGeneration` row action | `internal/homebrew` |
| U7 | Refresh Homebrew metadata | Updates / Sources | `updateHomebrew` | `homebrew.Update` |
| U8 | Discover untrusted taps; confirm trust | Updates / Sources | `trustTap` (confirmation in `confirmTrustTap`) | `homebrew.TrustPackages`; per-user, no pkexec |
| U9 | Change release channel | Updates / Settings | `onChannelToggled` | `ublue.SwitchChannel`; helper resolves image from channel word |
| U10 | Restart after update | Updates | `UpdateShell.StartRestart` | `ublue.Restart`; separate from staging/rollback |
| U11 | Toggle four update sources and post-update maintenance preference | Updates / Settings | `internal/settings` store, bound by `Window.buildPreferences` | Updates GSettings preferences; source policy/capability still floors execution |
| A1 | Launch configured software catalog (`app_id`, default Bazaar) | Apps / Installed apps | `launchApp` | External application launcher; installs belong to that catalog |
| A2 | List/uninstall user Flatpaks | Apps / Installed apps | `loadFlatpakApplications` user-scope row action | `internal/flatpak` |
| A3 | List/uninstall system Flatpaks | Apps / Installed apps | `loadFlatpakApplications` system-scope row action | `internal/flatpak`; retain system scope |
| A4 | List/uninstall Homebrew formulae and casks | Apps / Installed apps (GUI), Developer Tools (formulae) | `runHomebrewUninstall` with shared `loadHomebrewPackages` inventory | `homebrew.Uninstall`; typed formula/cask target |
| A5 | Pin/unpin formula | Apps / Developer Tools | `runHomebrewPin` | `homebrew.Pin` / `Unpin`; same row gate as uninstall |
| A6 | Export package list (replaces `~/Brewfile`) | Apps / Developer Tools | `onBrewBundleDumpClicked` | `homebrew.BundleDump`; handler is currently in `maintenance_page.go`, control is in Apps |
| A7 | Search formulae/casks; confirm install | Apps / Developer Tools | `onHomebrewSearch` / `installHomebrewSearchResult` search controller | `homebrew.Search` / `Install`; independent search generation, shared installed loader |
| A8 | Discover/check collections; install Brewfile | Apps / Collections and gaming | `loadBrewBundles` row install gate | `homebrew.AvailableBundles`, `BundleCheck`, `BundleInstall` |
| A9 | Enable/disable Developer Mode; update command-menu visibility | Apps / Developer Tools | `onDeveloperToggled` | `internal/ublue` group promotion; `internal/devmenu` user-session integration |
| A10 | Open developer onboarding; optional Pulp install and OPML staging after successful enable | Apps / Developer Tools | `startDeveloperFeedSetup` (onboarding launch via `openDeveloperOnboarding`) | `internal/developerfeeds`; `install_pulp` / `stage_feeds` opt in, no rollback of successful enable |
| A11 | Enable/disable Gaming Mode; show image-included gaming state | Apps / Collections and gaming | `onGamingToggled` / `refreshGamingState` gaming controller | `internal/gaming`; user Flatpaks, preserve system-installed components |
| A12 | Enable/disable Agent Mode; inspect readiness/details | Apps / Local AI tools | `onAgentModeToggled` / `showAgentModeState` Agent Mode controller | `internal/aistack`; user unit and environment fragment, llmman owns models |
| P1 | Open avatar chooser, preview, Apply | Appearance / Profile picture | `avatarPicker` controller in `profile_picture.go` | `internal/avatar.Applier`; AccountsService or face-file fallback, no pkexec |
| P2 | Search/select app-grid brand; enable/revert | Appearance / Icons | `onLiveryAppGridToggled` / `onLiveryBrandChosen` app-grid controller | `internal/livery`; user theme |
| P3 | Select panel foundation/custom file; enable/revert; toggle login rotation | Appearance / Icons | `livery.Panel` controller in `livery_actions.go` | `internal/livery`; user icon/dconf/preferences and rotation unit |
| P4 | Search/select Files project/custom artwork; enable/revert; toggle login rotation | Appearance / Icons | `livery.Dock` controller in `livery_actions.go` | `internal/livery`; Files application icon, not a separate dock-only icon |
| S1 | Select graphics driver variant | System / Graphics | `onDriverSwitchClicked` | `ublue.SwitchDriver`; fixed helper, retain `channel_group` gate |
| S2 | Read version, deployment and technical image details | System / About | `loadSystemVersion` (identity presentation in `buildImageIdentityGroup`) | `internal/bootc`, cached image identity; read-only |
| S3 | Enable/disable distribution feature | System / Distribution features | `onFeatureToggled` | `internal/updex` fixed helper |
| S4 | Check/update distribution features | System / Distribution features | `onUpdateFeaturesClicked` (reads via `checkFeatureUpdates`) | `internal/updex`; U1's system-components source uses same provider |
| S5 | Roll Back to previous deployment | System / Recovery | `onBootcRollbackClicked` | `ublue.Rollback`; existing target only, completes gate after live success, no restart |
| S6 | Check/Check Again published versions; expand list | System / Recovery | `onPublishedVersionsClicked` | `internal/registrytags.Catalog`; read-only, no pin/switch action |
| S7 | Free up space; post-update cleanup | System / Storage (also composed by U1) | `internal/updateproviders` cleanup runner | Typed cleanup steps; manual presentation via `onFreeUpSpaceClicked`, never configured scripts |
| S8 | Execute each configured `actions[]` entry, including default Clean Up Boot Old Entries | System / Administrator maintenance | `runMaintenanceAction` | `pageview.MaintenanceCommand` → `internal/maintenanceexec`; existing trusted-config sudo rules, five-minute timeout |
| S9 | Confirm Powerwash | System / Recovery | `onPowerwashClicked` | `internal/powerwash`; user-scope reset, opt-in `reset_group` |
| S10 | Confirm Factory Reset | System / Recovery | `onFactoryResetClicked` | `ublue.FactoryReset`; fixed helper with no target argument, opt-in `reset_group` |
| H1 | Set up Enhanced Troubleshooting | Help / Troubleshooting and support | `onTroubleshootClicked` setup gate | `internal/troubleshoot`; Homebrew/Goose user setup, readiness read from config |
| H2 | Start troubleshooting session | Help / Troubleshooting and support | `launchApp` | Existing Goose desktop application |
| H3 | Open configured website, issues, chat | Help / Troubleshooting and support | `openURL` | `xdg-open` through `internal/launcher` |
| H4 | Expand “Why is something missing?” | Help / Capability explanations | `pageview.UnavailableFeatures` | Read-only capability/config snapshot; no separate group |

Update counts remain owned by `internal/views/badgestate`; moving sources does
not introduce per-destination counters. The manual provider controls above and
the aggregate coordinator are distinct existing entry points, not a claim that
they already share a global concurrency gate. Relocation must preserve their
existing gates and provider calls, not create additional entry points.

Global actions have no invented configuration group:

| Existing action | Destination relationship | Single owner |
| --- | --- | --- |
| Sidebar/Alt+number/F1 Help; Recovery entry/Back | Primary or detail navigation only | `Window.navigateToPage`; current Recovery callbacks in `internal/window/window.go` must join the canonical transition in #342 |
| Preferences menu aliases; Check action | Opens Updates settings / invokes U1 | `Window.setupActions`, delegating to U11 / U1 rather than owning mutations |
| Help action | Opens configured website through GIO when present | `Window.setupActions` |
| Setup Assistant, `--setup`, Configure, Get moving, Back, Next/Finish, dismissal | Global onboarding; not an additional primary destination | `FirstRunAssistant` and its `internal/firstrun` model/store; disposition only, no replay of page mutations |
| Keyboard Shortcuts | Global dialog | `Window.onShowShortcuts`, inventory from `internal/navigation` |
| About Control Center | Global application About dialog | `Window.onShowAbout`; distinct from System / About |
| Quit / close while updates run | Application lifecycle | `internal/app` quit action; window close guard consults `UpdateShell.Busy` |

### Decision-reference reconciliation

Use filenames and recorded decisions, not numbers copied from issue prose:

| Actual reference | Subject | Consequence for #241 and #210 |
| --- | --- | --- |
| [ADR-0013](../adr/0013-rollback-catalog-reads-the-registry-live.md) | Live, read-only rollback catalog | Not capability visibility; catalog does not authorize switching to a dated tag |
| [ADR-0014](../adr/0014-capability-driven-visibility-as-a-floor.md) | Capability-driven visibility as a floor | This is the capability reference for routes, source settings and onboarding |
| [ADR-0015](../adr/0015-agent-mode-llmman.md) | Agent Mode using llmman | Not onboarding/avatar; changing its dedicated-page decision needs a superseding decision |
| [ADR-0016](../adr/0016-printer-app-admin-denied-until-authenticated.md) | Printer application admin authentication | Also allocated; do not reuse for workstation/navigation decisions |

The accepted ADR-0014 file itself still has a `0013` heading. That historical
heading does not change its filename identity and is not grounds to overwrite
an accepted decision. This reconciliation deliberately leaves accepted ADRs
untouched. #241 step 6 and #210's “ADR-0013 is capability-driven visibility”
claim should be read with the corrected links above. There is no accepted
onboarding/avatar or workstation-domain ADR in the current ADR inventory to
cite under those numbers. Do not invent one or reserve an already claimed
number. A future changed decision must follow [the ADR template](../adr/TEMPLATE.md),
use the then-next free number, and receive maintainer acceptance; this matrix
does not self-accept or supersede an ADR.

## Operational notes

- Apply `Configured && Available` using the original group reference at every
  destination. Navigation must not construct a disabled cross-group control.
  Runtime errors stay errors; an unavailable tool does not become a mutation.
- Build each live control once. Do not rerun builders into a second parent or
  overwrite `UserHome` handles. The original roots are temporary mounting
  points, not a second compatibility UI to keep indefinitely.
- Detail entry selects its primary ancestor, sets its own title and Back
  target, and reveals collapsed content. Back restores parent selection,
  scroll and sensible focus without rerunning an action. Details get no extra
  Alt+number accelerators. Hidden/unsupported links resolve to the nearest
  visible ancestor or Help; config failure keeps Help and its persistent error.
- Freeze the primary shortcut inventory for the session; first-run dismissal
  and asynchronous probes must not reindex it. These are #241's implementation
  requirements, not claims that every transition is implemented today.
- #201 must remove temporary mounts: Apps' specialist Homebrew content moves
  to Developer Tools; Agents to Local AI tools; Features splits between Apps
  and System; Livery becomes Appearance; Maintenance splits into Storage,
  administrator maintenance and Recovery; Updates' driver/technical identity
  content moves to System. Replace the current Maintenance→Recovery callbacks
  with System parentage. Keep update status/settings/sources/changes in Updates.
  Do not delete original YAML namespaces or hide still-working roots early.

## References

- Current architecture: [overview](overview.md), [provider ownership](package-managers.md).
- Existing rationale: [configuration schema ADR-0005](../adr/0005-config-schema-reflected-from-canonical-struct.md),
  [documentation ADR-0010](../adr/0010-docs-are-a-ci-gated-artifact.md), and the
  reconciled decisions above.
- Implementation plan/contract: [#241 steps 2, 3 and 6](https://github.com/projectbluefin/chairlift/issues/241),
  [#344 acceptance criteria](https://github.com/projectbluefin/chairlift/issues/344).
  No navigation Spektacular spec/clause identifier was supplied; these issue
  clauses are the contract for this documentation parcel, not a new accepted spec.
- Related delivered contract: [optional setup model](../specs/setup-model.md)
  (`original-policy` and `navigation-only`); it reuses original group references
  and does not introduce another mutation owner.
- Follow-on documentation: [#210](https://github.com/projectbluefin/chairlift/issues/210).
