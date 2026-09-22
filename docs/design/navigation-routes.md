# Navigation routes and destination ownership

Living document. Rationale: [ADR-0012](../adr/0012-ship-as-control-center-keep-chairlift-code-name.md)
and the accepted ADRs it links.

## Overview

`internal/navigation` is the single authority for what is navigable, what each
route is called, which configuration namespaces a route consumes, and what
changing to a route does to the window. This document is the reviewed,
human-readable half of that table: the five task destinations of
[#233](https://github.com/projectbluefin/chairlift/issues/233), the routes that
serve them, the single owner of every mutating action, and the mounts the
[#201](https://github.com/projectbluefin/chairlift/issues/201) cutover deletes.

```
config.yml pages/groups ──► Config.IsGroupEnabled ─┐
                                                   ├─► navigation.VisibleRoutes ──► Window sidebar
navigation.Routes (Refs) ──────────────────────────┘                                     │
                                                                                         ▼
route name ──► navigation.Resolve(Env) ──► Transition ──► Window.navigateToPage
```

`internal/installcheck` holds both halves of the table to each other:
`TestRouteTableIsDocumented` fails when the route table gains a route or a
destination this document does not name, and
`TestPrivilegedActionsHaveADocumentedOwner` fails when a helper subcommand has
no owner here. `TestEveryConfigGroupHasASurvivingRoute` fails when a
configuration group has no destination that outlives the cutover, and
`TestEveryRouteRefNamesARealConfigGroup` fails when a ref names a group
`internal/config` does not declare.

## Destinations

The product is organized around five task destinations, in the order #201
mounts them. A destination is not itself navigable — routes are — but every
route names the destination it belongs to, which is what makes a detail's Back
target unambiguous.

| Destination | Ordinary task | Owns |
|---|---|---|
| `updates` | Know whether this computer is current; update; restart when required | aggregate status, settings, sources, changes |
| `apps` | Browse and manage apps; install a useful collection | installed apps, collections and gaming, Developer Tools, Local AI tools |
| `appearance` | Change profile picture, wallpaper and desktop icons | profile picture, wallpaper, icon surfaces |
| `system` | Understand this computer; manage storage and supported options | About, Storage, Graphics, distribution features, administrator maintenance, Recovery |
| `help` | Solve a problem or reach support | troubleshooting and support, capability explanations |

`updates`, `system` and `help` are served today by a primary mount of the same
name. `apps` and `appearance` are served by the `applications` and `livery`
mounts, whose *backend* names are already correct — only the label changes at
the cutover.

## Routes

Each route carries a set of `Refs` — `(config page, group)` pairs. The set is
the point: a rendered destination may consume several configuration
namespaces, and configuration identity is never inferred from a route's
display name. `update-settings` is the worked example, consuming
`update_all_group` from `updates_page` and the channel control from
`system_page`, because the Automatic Updates switch and early-release
selection answer the same question and would otherwise be named for two
different pages.

Multiple routes may claim one group, and that is deliberate.
`channel_group` gates both the channel control under `updates` and the driver
choice under `system`; `brew_group` contributes GUI inventory and specialist
detail without a second inventory loader.

### Primary mounts

| Route | Label | Serves | Configuration namespaces |
|---|---|---|---|
| `applications` | Applications | `apps` | `applications_page`: `applications_installed_group`, `flatpak_user_group`, `flatpak_system_group`, `brew_group`, `brew_search_group`, `brew_bundles_group` |
| `maintenance` | Maintenance | `system` | `maintenance_page`: `maintenance_cleanup_group`, `maintenance_brew_group`, `maintenance_flatpak_group`, `maintenance_optimization_group`, `reset_group` |
| `updates` | Updates | `updates` | `updates_page`: `update_all_group`, `bootc_updates_group`, `sysupdate_updates_group`, `flatpak_updates_group`, `brew_updates_group`, `brew_trust_group` |
| `system` | System | `system` | `system_page`: `system_info_group`, `bootc_status_group`, `channel_group`, `health_group` |
| `features` | Features | `apps`, `system`, `help` | `features_page`: `features_group`, `dx_group`, `gaming_group`, `ai_group`, `troubleshooting_group` |
| `livery` | Livery | `appearance` | `livery_page`: `livery_app_grid_group`, `livery_foundation_group`, `livery_dock_group` |
| `help` | Help | `help` | `help_page`: `help_resources_group` |

`help` is the fallback route: it is always visible, so a disabled or
unsupported destination lands on it rather than on nothing. It is the only
route carrying that flag.

### Detail routes

A detail is a focused screen reached from a primary. It has its own title and
a Back destination, is never a sidebar entry, and carries no Alt+number
accelerator. A declared detail is **not reachable until its widget is
mounted**: `navigation.Resolve` falls back to the ancestor that draws its
content, so a declared-but-unbuilt detail can never be an empty screen.

| Detail route | Label | Destination | Configuration namespaces |
|---|---|---|---|
| `update-settings` | Update Settings | `updates` | `updates_page`: `update_all_group`; `system_page`: `channel_group` |
| `update-sources` | Update Sources | `updates` | `updates_page`: `bootc_updates_group`, `sysupdate_updates_group`, `flatpak_updates_group`, `brew_updates_group` |
| `update-changes` | What's Changing | `updates` | `updates_page`: `bootc_updates_group`, `sysupdate_updates_group` |
| `trust` | Trust Decisions | `updates` | `updates_page`: `brew_trust_group` |
| `gaming` | Gaming Setup | `apps` | `features_page`: `gaming_group` |
| `developer-tools` | Developer Tools | `apps` | `applications_page`: `brew_group`, `brew_search_group`, `brew_bundles_group`; `features_page`: `dx_group` |
| `ai-tools` | AI Tools | `apps` | `features_page`: `ai_group` |
| `icons` | Icons | `appearance` | `livery_page`: `livery_app_grid_group`, `livery_foundation_group`, `livery_dock_group` |
| `about` | About This Computer | `system` | `system_page`: `system_info_group`, `bootc_status_group`, `health_group` |
| `storage` | Storage | `system` | `maintenance_page`: `maintenance_brew_group`, `maintenance_flatpak_group` |
| `graphics` | Graphics | `system` | `system_page`: `channel_group` |
| `distribution-features` | Distribution Features | `system` | `features_page`: `features_group` |
| `administrator-maintenance` | Administrator Maintenance | `system` | `maintenance_page`: `maintenance_cleanup_group` |
| `recovery` | Recovery | `system` | `maintenance_page`: `reset_group` |
| `troubleshooting` | Troubleshooting | `help` | `features_page`: `troubleshooting_group` |
| `capability-explanations` | Feature Availability | `help` | `help_page`: `help_resources_group` |

Appearance's avatar and wallpaper details are deliberately **not** declared
here. They have no configuration namespace yet — the work that introduces one
is [#222](https://github.com/projectbluefin/chairlift/issues/222)'s — and a
detail route with no refs would be ungated, which is the empty-placeholder
shape both #233 and #201 forbid. They are added with the namespaces that gate
them.

## Action and route ownership

One route owns each mutating action. Ownership is about *mutation*, not
placement: several routes may read the same group's state, but exactly one
route performs the write, so no control is reachable from two parents and no
action can be relocated twice.

| Action | Owner route | Privileged boundary |
|---|---|---|
| Update All (OS, Flatpak, Homebrew sequence) | `update-settings` | `internal/bootc` staging path; never a second privileged route |
| Automatic updates on/off | `update-settings` | `channel-switch` family via `chairlift-ublue-helper` |
| Per-provider update | `update-sources` | provider packages |
| Staged changelog / SBOM compare | `update-changes` | none (registry read on explicit request) |
| Homebrew tap trust | `trust` | none — deliberately per-user, never `pkexec` |
| Release-channel switch | `update-settings` | `channel-switch` |
| Graphics-driver switch | `graphics` | `driver-switch` |
| Developer access (dx) enable/disable | `developer-tools` | `dx-enable`, `dx-disable` |
| Package search, pin/unpin, export list | `developer-tools` | none (user-scope Homebrew) |
| App collection / bundle install | `developer-tools` | none |
| Gaming setup | `gaming` | none — user-scope Flatpaks only |
| Local AI runtime on/off | `ai-tools` | none — rootless quadlet in the invoking account |
| Free Up Space (Homebrew and Flatpak cleanup) | `storage` | platform authentication for system-scope work; no passwordless cleanup |
| Configured administrator scripts | `administrator-maintenance` | `pkexec` per the script's `sudo` flag, existing policy unchanged |
| Powerwash | `recovery` | none — runs in the invoking account |
| Factory Reset | `recovery` | `factory-reset` |
| Boot-entry cleanup | `administrator-maintenance` | configured script, existing policy unchanged |
| Restart into a staged image | `update-settings` | `restart` |
| Roll back a deployment | `recovery` | `rollback` |
| Feature enable/disable (updex) | `distribution-features` | `enable-feature`, `disable-feature` |
| Feature update (updex) | `distribution-features` | `update` |
| Assisted troubleshooting setup | `troubleshooting` | none — user-scope Homebrew |
| Icon marks (app grid, panel, Files) | `icons` | none — user icon theme and dconf only |

The privileged column is not decoration: `internal/installcheck` holds this
section to `ubluehelper.SupportedCommands()` and
`updexhelper.SupportedCommands()`, so every `channel-switch`, `dx-enable`,
`dx-disable`, `restart`, `rollback`, `auto-updates-enable`,
`auto-updates-disable`, `driver-switch`, `factory-reset`, `enable-feature`,
`disable-feature` and `update` subcommand must appear above.

## Temporary mounts deleted at the cutover

The route table marks these `Temporary`; #201 deletes them once every
capability above is mounted at its destination. They exist so the window stays
usable while the seam lands, not as a compatibility UI.

| Temporary mount | Why it is temporary |
|---|---|
| `maintenance` | Replaced by `storage`, `recovery` and `administrator-maintenance`. Its `maintenance_optimization_group` is an empty placeholder with no controls, has no destination, and is recorded as such in `routeGroupExemptions` — the exemption is deleted with the group. |
| `features` | Replaced by `gaming`, `developer-tools`, `ai-tools`, `troubleshooting` and `distribution-features`. |

The following are **not** deleted but are relabelled or recomposed at the same
cutover: `applications` becomes Apps, `livery` becomes Appearance, and the
`updates` primary hands its legacy composition to the unified update shell
[#233](https://github.com/projectbluefin/chairlift/issues/233) already
specifies, so the superseded Updates surface and its duplicate workers are
removed rather than left running out of sight.

## State and failure contract

`navigation.Resolve` is pure: it returns state to apply and never runs an
action. The transitions it can produce are exactly:

| Transition | Behaviour |
|---|---|
| Primary, visible and constructed | Selects its sidebar row, sets its title, reveals content in a collapsed layout |
| Detail, constructed, with an enabled ref and a visible ancestor | Same, plus the detail's own title and a Back target naming the ancestor |
| Detail not constructed, or every ref disabled | Falls back to the ancestor that draws its content; no empty screen |
| Detail with no reachable ancestor | Falls back to `help` |
| Primary known but omitted by configuration | Falls back to `help` |
| Unknown route | Rejected; the window does not navigate |

An unknown route is rejected rather than guessed at, because there is nothing
to guess from and navigating nowhere beats navigating wrong. A known route
always lands somewhere safe. Enabling, disabling or completing late worker
work never reindexes the visible primary inventory during a session, so a
shortcut that worked when the window opened still works after.

## Retired: the configuration-to-navigation one-to-one assumption

`internal/installcheck` previously asserted a bijection between navigation's
pages and `config.SchemaPages()`. That premise is retired: a route holds a set
of `Refs` that may span pages, and one group may be claimed by more than one
route. What replaced it keeps the edge the bijection existed to catch, without
the one-to-one premise — every configuration group must be claimed by a route
that survives the cutover, and every ref must name a group the schema
declares. The `Refs` field replaced `Item.ConfigPage` and `Item.Groups` in the
same change, so there is no surviving second representation to drift.

## Decision references

The workstation epic points at a `CONTEXT.md` and a workstation ADR-0013 that
are not in this repository, and asserts that **ADR-0013 is capability
visibility** ([#203](https://github.com/projectbluefin/chairlift/issues/203));
proposed ADR-0014 and ADR-0015 already belong to the onboarding and avatar
decisions ([#222](https://github.com/projectbluefin/chairlift/issues/222)).
None of those numbers is free, and the missing workstation ADR is not
invented here or given a reused number. The route contract in this document is
a design decision under `docs/design/`, not an accepted ADR: if the
maintainers want it recorded as one, it takes the next free number after those
claimed, and accepted ADRs on the subject are superseded by a new decision
rather than edited in place.

## References

- Product contract: [#233](https://github.com/projectbluefin/chairlift/issues/233)
- Route seam: [#241](https://github.com/projectbluefin/chairlift/issues/241)
- Cutover: [#201](https://github.com/projectbluefin/chairlift/issues/201)
- Capability visibility: [#203](https://github.com/projectbluefin/chairlift/issues/203)
- Architecture entry point: [overview.md](overview.md)
