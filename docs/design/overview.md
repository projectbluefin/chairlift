# ChairLift Overview

Living architecture for the shipped application. Start here for ownership and
safety boundaries; load the linked subsystem documents only for the task at hand.
Decision history belongs in [ADRs](../adr/), not in current-state appendages.

## Purpose

Control Center is the GTK4/Libadwaita system-management GUI for [Bluefin](https://projectbluefin.io) and other bootc images. ChairLift is its repository/binary code name; user-visible naming belongs to `internal/branding.AppName`. Written in Go using [puregotk](https://codeberg.org/puregotk/puregotk), it loads native libraries at runtime without CGO. Existing providers manage Homebrew and Flatpak, OS staging through the image-owned fixed stage script, updex features and maintenance. Configuration may hide groups; it cannot create host capabilities or broaden privilege boundaries.

## Architecture

```
cmd/chairlift/main.go                 Entry point: version injection, app creation
cmd/chairlift-updex-helper/main.go    Privileged helper for updex write operations
cmd/chairlift-helper/main.go    Privileged helper for Bluefin-family system writes
        │
internal/app/app.go             GObject-registered Application (adw.Application subtype)
        │
internal/window/window.go       Main window: NavigationSplitView, sidebar, content stack
        │
internal/views/                 Page builders and event handlers (one file per page)
        │                       ├── internal/views/actionmsg/     ┐ puregotk-free leaf packages:
        │                       ├── internal/views/actionstate/   │ toast/decision text, async gates,
        │                       ├── internal/views/updatepresent/ │ update snapshot presentation,
        │                       ├── internal/views/bundleview/    │ row bookkeeping, expander status,
        │                       ├── internal/views/trustmsg/      │ Flatpak and feature update-status
        │                       ├── internal/views/rowset/        │ text and decisions, bounded
        │                       ├── internal/views/liverystate/   │ confirmed appearance state,
        │                       ├── internal/views/featurestatus/ │ unit-tested headlessly
        │                       ├── internal/views/progresslog/   │
        │                       ├── internal/views/signalroute/   │ shared-callback routing,
        │                       └── internal/views/pageview/      ┘
        │
        ├── internal/config/    YAML config loading, feature group enablement
        ├── internal/navigation/ Canonical route inventory (primaries and details), shortcuts, and pure navigation transitions
        ├── internal/capability/ What this host can back a page or group with, from non-blocking probes
        ├── internal/launcher/ Pure-Go async launcher start/wait helper for GTK callers
        ├── internal/legacydesktop/ Asynchronous startup cleanup of obsolete frostyard desktop launchers
        ├── internal/avatar/    Dinosaur avatar catalog, pinned fetch seam, and pure-Go WebP-to-PNG avatar transcoder (whole character fitted inside the 512x512 avatar circle, 4 MiB/16.8 Mpx input bound, 1 MiB output ceiling)
        ├── internal/homebrew/  Homebrew CLI wrapper (JSON output parsing)
        ├── internal/developerfeeds/ Pure-Go developer feed OPML catalog (go:embed) and offline validator
        ├── internal/flatpak/   Flatpak CLI wrapper (tabular output parsing)
        ├── internal/bootc/     bootc wrapper (status reads, fixed stage adapter)
        ├── internal/pkexec/    Sole owner of the privilege-escalation program name (`pkexec.Command`)
        ├── internal/deskenv/   Desktop-environment detection from session variables
        ├── internal/stageexec/ Pure-Go shared OS staging stream/event executor
        ├── internal/updex/     Updex feature manager (Go library reads, helper binary writes)
        ├── internal/updexhelper/ Puregotk-free argv-parsing/Options-building for cmd/chairlift-updex-helper
        ├── internal/devmenu/   Custom Command Menu tuple updater for developer tools and Ask Bluefin visibility
        ├── internal/printerapp/ Rootless printer-application quadlets, one per driver family, with the ADR-0016 enable gate and the pure readiness model behind the Features page's Printers group
        ├── internal/aistack/   Local llmman user service, observed health and canonical model aliases
        ├── internal/agentmode/ Goose Desktop readiness, profile-isolated launch, one-session guard and Ask Bluefin dispatch
        ├── internal/contribute/ Read-only contributor preflight and terminal command construction
        ├── internal/livery/    User icon-theme/settings mutations and login rotation
        ├── internal/firstrun/  Explicit existing-page setup selection and disposition
        ├── internal/ublue/     Bluefin-family system mutations through the ublue helper
        ├── internal/ubluehelper/ Puregotk-free argv parsing for cmd/chairlift-helper
        ├── internal/updateflow/ Pure unified update coordinator and state machine
        ├── internal/updateproviders/ Provider adapters for the unified update coordinator
        ├── internal/userprefs/ Pure user update preferences model
        ├── internal/settings/  GSettings adapter for user update preferences
        ├── internal/commands/  Shared action names (shortcuts live in navigation)
        └── internal/version/   Build metadata (ldflags injection)
```

### Dependency flow

`cmd → app → window → views → {config, launcher, homebrew, flatpak, bootc, updex}`.
`app` and `window` also depend on the pure `navigation` package.

External shared library: `github.com/frostyard/snowkit` (published module, pinned in go.mod) provides:

- `gobj` — GObject type registration and instance registry
- `sgtk.RunOnMainThread()` — main-thread dispatch for GTK safety

### Views coordinator (`internal/views/views.go`)

The `views.go` file defines the central `UserHome` struct that holds references to all page widgets, config, and the `ToastAdder` interface. It provides:

- `New(cfg, caps, toastAdder)` — constructor receiving the same capability set as navigation
- `ToastAdder` — toast, badge, and background-notification callbacks implemented by Window

`internal/views` imports puregotk, so it cannot host headless test binaries. Pure leaf packages own decidable presentation and state: `actionmsg`, `trustmsg`, `actionstate`, `bundleview`, `rowset`, `progresslog`, `featurestatus`, `signalroute`, `liverystate`, `cleanupview`, `updatepresent`, and `pageview`. The update inventory and badge belong to `internal/updateflow`, not a second view-side count store. `pageview` supplies shared presentation for all seven page builders, whose wiring test remains enforced. This layout is decision record
[ADR-0007](../adr/0007-pure-leaf-packages-route-around-untestable-gtk.md).

### Pages

The current UI defines seven primary pages. Static configuration and the host
capability floor may omit a functional page whose builder-backed groups are all
disabled or unavailable; Help is always retained. `internal/navigation` owns
this inventory, independently of the original YAML namespace names.

| Page | File under `internal/views/` | Current purpose |
| --- | --- | --- |
| Updates | `updates_page.go` | Aggregate updates, provider detail, automatic updates, channel/graphics controls and system version |
| Apps | `applications_page.go` | Homebrew collections first, installed casks and explicitly requested formulae next, then Brewfile export; no Flatpak inventory, external catalog launcher or package search |
| Agents | `agents_page.go`, `troubleshoot.go`, `contribute.go` | Agent Mode using llmman, Troubleshooting's Goose row (set up, then launch in ChairLift's own profile) and menu switch, and Contribute to Bluefin |
| Features | `features_page.go` (+ `printers_page.go`) | Distribution features, Developer Mode, Gaming Mode, and Printers |
| Livery | `livery_page.go` | Profile Picture, App Launcher Icon, Top Bar Icon, and Files Icon surfaces |
| Maintenance | `maintenance_page.go` | Free up space, administrator scripts and Powerwash entry |
| Help | `help_page.go` | Support links, diagnostics and capability explanations |

Powerwash is an existing detail (route `recovery`) built by `recovery.go` and reached from
Maintenance, with rollback, published-version reads with pin and return-to-stream
actions, and opt-in reset controls. There is no current System primary page;
machine-wide desktop settings belong to the desktop's own settings application.

### Architecture route map

The configuration-to-navigation one-to-one assumption is retired: a configuration
page is a stable YAML namespace, not a destination identity. The
[destination and action ownership matrix](destination-matrix.md) records current
groups/actions and their live owners. Routes retain original
`(configuration page, group)` references across primary/detail boundaries.
`internal/navigation` remains the sole route authority. The former five-section
proposal was superseded; it is neither a shipped destination nor a new policy key.

## Key Patterns

### GObject registration via snowkit

Application and Window are registered as GObject subtypes using `gobj.RegisterType()`. This returns a `gobject.Type` and an `*gobj.InstanceRegistry`. The pattern:

1. `init()` registers the type with `ClassInit` callback
2. `ClassInit` overrides `Constructed` to create the Go struct and pin it in the registry
3. Constructor (`New()`) calls `gobject.NewObject()` then retrieves the Go instance from the registry

See `internal/app/app.go` and `internal/window/window.go`.

`gobject.NewObject` forwards variadic arguments to GLib's C constructor.
Object-valued properties need the native pointer (`app.Application.GoPointer()`),
not the address of the Go binding wrapper (`&app.Application`), and the property
list ends with `uintptr(0)`. Passing the wrapper address produced a
`g_object_new_valist: invalid object type` critical on every window launch;
the E2E startup runs with `G_DEBUG=fatal-criticals` so it fails rather than
shipping a warning that normal GTK startup only logs.

### Async operations with main-thread dispatch

All external tool calls run in goroutines. UI updates are marshaled back via `sgtk.RunOnMainThread()`:

```go
go func() {
    result, err := homebrew.ListInstalledFormulae()
    sgtk.RunOnMainThread(func() {
        // update widgets here
    })
}()
```

Long-running action rows use the shared native spinner helpers in
`internal/views/widgets.go`. The row builds its spinner once; its existing
action gate or Livery serializer owns the start/stop lifetime. No progress
percentage is inferred. `Window.ShowToast` and `ShowErrorToast` preempt older
toasts with Libadwaita's high priority, retaining those older errors in the
queue rather than leaving every later result behind an infinite timeout. A
message carrying pkexec's dismissal text (`pkexec.MessageIsAuthDismissed`) is
not an error: `ShowErrorToast` shows a brief "Authentication cancelled" toast
instead of pinning raw stderr.

Connect reusable GTK signals once, outside refresh paths. `buttonRoute` and
`dialogRoute` in `internal/views/widgets.go` reuse stable callback variables
with `signalroute` dispatch tables; clear/forget routing alongside removed rows.
Per-row closures in refreshes exhaust purego's fixed trampoline table, even when
widgets are destroyed. Switch mutations use guarded `GtkSwitch::state-set`,
not generic property notifications; programmatic restores must not dispatch a
second mutation. Render untrusted command/provider text with markup disabled.

### Deferred visibility (async startup)

A group whose backing tool's *presence* is its whole prerequisite (Homebrew, Flatpak, Podman, the image descriptor, the stage scripts) is not deferred at all: `internal/capability` omits it at build time through `UserHome.groupEnabled`, so its loaders never render a "not installed" placeholder. What such a loader can still meet is a tool that is present but fails; that is a real failure and the row says so ("Couldn't load this list.", "Could not check for tool updates") while keeping the last known rows and counts.

Runtime query gates remain asynchronous: optional distribution features hide when no definitions exist; tap trust hides when there is nothing to trust; automatic updates stays hidden until its installed timer is observed. Query failures are not invented empty inventories. Discoverability is surface-specific: desktop integrations and unsupported Developer options deliberately remain visible with insensitive controls and explanations, and printer administration locks render visible off switches. Static navigation never reindexes after these workers finish.

The *startup* path must not probe update providers on the main thread. The status-first update shell's initial `Coordinator.Check` runs in a worker (`UpdateShell.StartCheck`), and every provider's `Available` probe — some of which are `sync.Once`-cached subprocess checks, such as `flatpak --version` — is evaluated inside that worker, never while building the page. The automatic-updates group on the Updates page is built as a hidden shell (`buildAutomaticUpdatesGroup`) because its two `systemctl` queries can each approach a multi-second timeout on a slow or wedged host; `loadAutomaticUpdatesGroup` runs `autoupdate.Detect` in a worker under a five-second bound and reveals the switch on the GTK main thread only when the unattended-update timer is installed.

### bootc boot gate

The Updates page's `bootc_status_group` and `bootc_updates_group` load asynchronously behind `bootc.IsBootcBootedCached()`, which reads status once and requires a booted deployment. On composefs hosts (`composefs=` on the kernel command line), status comes from world-readable deployment state; elsewhere it uses `bootc status --format json`. No password is requested for these reads. Neither `/run/ostree-booted` nor exit status alone establishes a booted deployment: composefs lacks that sentinel, and bootc can return success with null `booted`. Staging additionally requires the fixed stage script.

### Native desktop settings module

`cmd/chairlift` calls `deskenv.ConfigureSettingsModules` before `app.New()`
and before dispatching `--rotate-livery`. The helper searches installed native
GIO module directories for `libdconfsettings.so`, including the current
architecture's multiarch directory, and appends the first match to
`GIO_EXTRA_MODULES` without duplicating entries or overriding
`GSETTINGS_BACKEND`. Both Homebrew-loaded GLib and child settings tools can
therefore use the desktop's dconf store instead of an independent keyfile
backend; explicitly memory-backed test sessions retain their isolation.

### Dry-run mode

Decision record: [ADR-0009](../adr/0009-dry-run-output-convention-and-single-decision-structs.md)
(the `[DRY-RUN]` output convention and single decision structs).

`internal/dryrun` (`internal/dryrun/dryrun.go`) is the single process-wide preview-mode authority. `app.New()` calls `dryrun.Set(true)` once at startup when `--dry-run`/`-d` is passed, and every integration — homebrew, flatpak, bootc, updex, avatar, and `internal/views` itself (for configured custom maintenance scripts, which have no wrapper package of their own) — reads `dryrun.Enabled()` rather than keeping a flag of its own.

Preview feedback must never claim a mutation succeeded or alter confirmed rows,
counts or switches. Pure `actionmsg`/`actionstate` decisions pair admission and
feedback wherever a successful action also changes UI state. Failures and
previews restore retryable controls; only a verified live success can complete
controls that become permanently unavailable. Provider wrappers independently
suppress external mutations as defense in depth. See
[package manager action patterns](package-managers.md) for the per-action seams.

**Intentional exception:** system staging completion **toasts** are dry-run-aware (`actionmsg.SystemStage`), but expander **subtitles** deliberately are not. The subtitle is a persistent status readout of live state — what deployment is actually staged/booted right now — not a per-click completion claim, so it stays accurate and unchanged in both dry-run and live mode. Only the toast, which inherently answers "what did this click just do," needed dry-run-specific wording; there is no mutation left to gate once the subtitle is deliberately excluded, which is why `SystemStage` is string-only rather than a decision struct. bootc staging follows that split: its toast comes from `actionmsg.SystemStage`, while its subtitle stays live in both modes.

Per-wrapper mechanics:

- **Homebrew/Flatpak**: state-changing commands are skipped entirely at the wrapper layer (return mock/empty results); ordinary package-action toasts use the plain `actionmsg` string functions (`Install`, `Uninstall`, `Pin`, `Upgrade`, `Update`, `SelfUpdate`, `BundleDump`, `Cleanup`). Installed Homebrew package uninstall/pin actions pair that text with `actionstate` decisions: live success completes the old row controls and refreshes the installed inventory, while failure or dry-run restores the controls. Brew bundle installation uses `BundleInstallDecision` with the same live-complete/dry-run-reset distinction.
- **Updex**: `EnableFeature`/`DisableFeature`/`UpdateFeatures` skip their `pkexec` call entirely under dry-run and return empty/nil results; the helper binary itself (`cmd/chairlift-updex-helper`, dispatch logic in `internal/updexhelper`) also honors `--dry-run` for `update`, matching `enable-feature`/`disable-feature`, as defense-in-depth even though it's unreachable from the wrapper today.
- **bootc**: `StageUpdate` short-circuits before invoking pkexec: it logs the would-be command, emits a synthetic `EventMessage` + `EventComplete` pair on the progress channel, and returns — the stage script is never actually run (see the exception above for the toast/subtitle split).
- **Homebrew tap trust**: `trustTap` (`internal/views/updates_page.go`) computes `decision := actionmsg.TapTrust(dryrun.Enabled(), tap.Name)` once, after a successful `homebrew.TrustPackages` call, and gates removing the tap's row, hiding the group, and refreshing outdated packages on `decision.MutateUI`.
- **views (custom maintenance scripts)**: `runMaintenanceAction` (`internal/views/maintenance_page.go`) calls `actionmsg.MaintenanceScript(dryrun.Enabled(), title)` once, before spawning its goroutine, to get a `ScriptDecision{Execute, Toast}`: when `Execute` is false no `exec.Cmd` is ever constructed (no `pkexec`, no direct script exec) — only a `[DRY-RUN] Would execute: ...` log line.
- **Features page switch confirmation**: `onFeatureToggled` (`internal/views/features_page.go`) computes `decision := actionmsg.FeatureToggle(dryrun.Enabled(), enabled, name)` once, after a successful `updex.EnableFeature`/`DisableFeature` call, and branches solely on `decision.Confirm` to decide whether the switch confirms the flip (`toggle.set(enabled)`) or reverts to its pre-click state (`toggle.set(!enabled)`); `set` is `guardedSwitch`'s programmatic move, which marks itself so the resulting `::state-set` is not treated as another user action.
- **avatar**: `Applier.Dispatch` (`internal/avatar/applier.go`) reads `dryrun.Enabled()` first and returns before constructing a process or opening a file, logging `[DRY-RUN] would set avatar to <id>`. A live dispatch reports which route took effect as an `avatar.Route`: `RouteBusctl` when AccountsService accepted `SetIconFile` over `busctl`, `RouteFaceFile` when that call failed or was unreachable and the icon was written to `~/.face.icon` and `~/.face` instead (picked up at the next session start, not immediately), and no route at all when both failed. A canceled context is reported as an error rather than being treated as an unreachable bus, so an abandoned action never writes into the user's home. The dispatch is deliberately unprivileged — AccountsService authorizes it for the caller's own account — so it takes no `pkexec` route and is classified as an unprivileged `os/exec` site in `internal/installcheck`'s journal contract. Its one caller is the Livery page's Profile Picture section (`account_group`, `internal/views/profile_picture.go`): opening the page only stats `avatar.CurrentPicture`'s candidates (AccountsService's icon, then `~/.face.icon`, then `~/.face`); picking a catalog row downloads and transcodes that one illustration for a preview, and only Apply writes the PNG to `$XDG_CACHE_HOME/chairlift/avatar.png` and dispatches it. Under `--dry-run` Apply writes no file and Dispatch logs the preview. A failed download or dispatch reveals a banner in the chooser and leaves the account's picture and the page row unchanged; the toast text for each route comes from `pageview.AvatarApplied`, so a face-file write never reads as a live change. The chooser is an `AdwDialog` built once with one row per catalog entry and one `row-activated` handler.
- **Developer onboarding page**: `openDeveloperOnboarding` (`internal/views/features_page.go`) is reached only from the success branch of `onDeveloperToggled`, and dispatches through `pageview.DeveloperOnboardingTargets(dryrun.Enabled(), enabled, succeeded)`. That pure function is the whole admission rule — a confirmed live enable is the only input combination that yields a target, and it yields exactly one (the Bluefin developer documentation, which links onward to training and platform material): opening three tabs from one switch flip stacked three app-choosers on a desktop with no default browser (#494). `--dry-run` opens no browser processes during `make screenshots`, a disable is a no-op, and a failed promotion opens nothing. The URL is asserted headlessly in `internal/views/pageview`; the dispatch itself reuses `UserHome.openURL`, the same asynchronous `xdg-open` path the Help page links use, so a browser that fails to start reports its own failure by toast without touching the group promotion or the switch. A promotion whose helper skipped developer groups the image does not define (`skipped group <name>:` on stderr, parsed by `ubluehelper.SkippedGroups`) still confirms, and `actionmsg.DeveloperMode`'s ordinary, self-expiring toast names each group not granted (#495), kept short enough that `ShowToast`'s single ellipsized line does not cut the names off. It is not a persistent error toast: images that ship neither group (Dakota) skip them on every enable, and a persistent "enabled" banner outlived a later disable.
- **Optional developer feed setup**: `startDeveloperFeedSetup` (`internal/views/features_page.go`) is called from that same success branch, and its admission rule is `actionmsg.DeveloperFeedSetupPlan(dryrun.Enabled(), enabled, succeeded, installPulp, stageFeeds)`. Only a confirmed live enable with at least one of `dx_group`'s `install_pulp`/`stage_feeds` set produces non-empty work, so a preview installs nothing during `make screenshots`, a disable is a no-op, and a failed promotion starts no worker. The plan is a value read from config on the main thread before the goroutine starts, so the worker touches no widget and no view state; it calls `internal/developerfeeds`'s `Provision` and `StageOPML` (both of which re-check `dryrun.Enabled()` as defense in depth) and marshals a single `actionmsg.DeveloperFeedFeedback` result back through `sgtk.RunOnMainThread` to one toast. `developerFeedGate` refuses a second setup while an install is in flight, and the toast is nil-guarded. The wording and the failure classification come from the same tested decision struct, which is what keeps a failed optional install from reading as a failed permission change and keeps "staged" from reading as "imported".
- **Legacy desktop launcher cleanup**: `legacydesktop.Clean` (`internal/legacydesktop/legacydesktop.go`) runs in a background goroutine at startup. Under `--dry-run`, it logs what would be removed without deleting the file.
### Configuration-driven UI visibility

Each preference group on every page checks `config.IsGroupEnabled(pageName, groupName)` before building its widgets. Groups default to enabled if not specified in config. Both `maintenance_cleanup_group` and `reset_group` default to disabled in the default config.

`internal/config/config.go` builds the effective `*Config` by overlaying a
parsed file onto `defaultConfig()` field by field, not by replacing it
wholesale — `mergeConfig` walks each page, `mergePage` walks each group within
a page, and `mergeGroup` walks each field within a group. The overlay is
driven by `rawConfig`/`rawPageConfig`/`rawGroupConfig`, a pointer-typed mirror
of `Config`/`PageConfig`/`GroupConfig` used only for YAML decoding: a `nil`
pointer means the file omitted (or explicitly nulled) that key, so
`defaultConfig()`'s value survives; a non-nil pointer means the file set that
key — including to an empty string or empty slice — so it replaces the
default outright. This is why `maintenance_cleanup_group` stays disabled
overall (and keeps its default `actions` entry) when a config file mentions
the group only to flip an unrelated field. When every search candidate is
absent, `Load()` returns `defaultConfig()`. An authoritative file that cannot
be read or validated instead returns `disabledConfig()`: the same defaults
for non-visibility fields, with every canonical group's `Enabled` field
forced to false.

**Legacy System-page input (`internal/config/legacy.go`).** After effective
YAML resolution, schema validation recognizes `system_page`'s four historical
groups without adding that page to `Config` or `SchemaPages`. All legacy fields
and actions are validated before migration, including retired groups and
values superseded by current settings. Before decoding, the two surviving
groups (`bootc_status_group`, `channel_group`) supply omitted/null Updates
fields; explicit current values take precedence. Information and health groups
are ignored by runtime decoding. Similarly, the retired `maintenance_page`
groups (`maintenance_brew_group`, `maintenance_flatpak_group`,
`maintenance_optimization_group`), `updates_page` groups
(`update_all_group`, `sysupdate_updates_group`), and `features_page` groups
(`ai_group` with its retired `ai_images`/`ai_model` fields,
`troubleshooting_group`) carried by pre-26.09 Bluefin releases (v0.12.x) and
removed by the 26.09-alpha Control Center reorganisation, plus the
`applications_page` groups retired when Apps became Homebrew-only
(`applications_installed_group`, `flatpak_user_group`,
`flatpak_system_group`, `brew_search_group`, still shipped by
projectbluefin/common's `config.yml`), are recognized as
known group names, validated alongside their canonical siblings, and stripped
prior to runtime decoding so existing host files do not fail closed;
`troubleshooting_group` first supplies omitted/null `help_page` fields and then
moves on to `agents_page` with the old Help address. Source files,
search precedence, and fail-closed handling for invalid inputs remain
unchanged.

**Strict loading and diagnostics.** The implementation lives in
`internal/config`; [the configuration reference](../reference.md) describes
operator-facing fields and defaults. Runtime loading follows one pipeline:

1. Resolve a candidate's path and trusted/untrusted provenance before reading.
   Only a genuinely missing candidate advances the search; a dangling
   authoritative symlink fails closed.
2. Parse exactly one YAML document. Empty input or top-level null is a no-op
   overlay; malformed YAML and additional documents are errors.
3. Validate the entire reachable source graph before resolving merges. Cycles,
   malformed nodes, invalid merge operands and duplicate explicit keys are
   rejected even in branches that merge precedence would later discard.
4. Emit a fresh alias-free, merge-free effective tree. Explicit keys beat merged
   values; earlier merge-sequence operands beat later ones. Source duplicate
   detection follows yaml.v3's Kind/Value identity, while effective scalar-key
   identity includes the resolved YAML tag. Alias-resolved explicit collisions
   are errors, not silently chosen winners. Shared merge inventories are memoized
   per resolution, so shared source graphs do not require exponential work.
5. Validate schema names, key shapes and declared types before decoding and
   overlaying defaults. Names come from the canonical structs and default group
   maps (`SchemaPages`, `SchemaGroups`, `SchemaGroupFields`,
   `SchemaActionFields`), not a second validator inventory. At every level,
   inspect key shape, name membership, then the known name's value. Actions
   must be mapping entries in a sequence; null entries are not zero actions.
6. Validate privileged-action provenance again after overlaying defaults.
   Only the fixed `/etc/chairlift/config.yml` and
   `/usr/share/chairlift/config.yml` candidates may define `sudo: true`
   actions. Executable-relative and working-directory fallbacks cannot acquire
   privileged execution through their contents or inherited enabled actions.
   Privileged script paths must also be absolute.

The source graph is bounded at 64 consecutive alias hops and 128 source-node
visits on a root-to-leaf path; expansion is bounded at 100,000 emitted nodes,
checked before allocation. Bounds and diagnostic attribution operate over
shared-node inventories rather than expanding every path. Diagnostics name a
positive source line, including deterministic attribution for synthetic nodes.

`LoadError` distinguishes read, parse/type, and schema failures, preserves
wrapped causes for `errors.Is`/`errors.As`, and renders a cause only once.
Any authoritative failure returns `disabledConfig()` with every configurable
group disabled. The `CONFIGURATION ERROR` log and persistent GTK-thread toast
name the source and cause and instruct the user to fix it and restart. No
lower-priority file can override a broken authoritative file.


### Desktop environment detection (`internal/deskenv`)

`internal/deskenv` is a pure, standard-library-only leaf package that answers
which desktop environment the current session is running. It exists because
some livery marks are GNOME surfaces — the GNOME app-grid icon is written into
the Adwaita theme and the panel mark into an `org.gnome.shell.extensions`
schema — so on an unsupported desktop every write can succeed while nothing
changes. The desktop-specific surface table prevents that silent no-op. It is
the detection half of the KDE support work in
[Epic #211](https://github.com/projectbluefin/chairlift/issues/211): the
livery surface table selects KDE's configured Kickoff applets for the app-grid
mark (#215) and `org.kde.dolphin` in `hicolor` for Files (#216), while GNOME
continues to use `view-app-grid-symbolic`, its panel extension and
`org.gnome.Nautilus`. Kickoff customization is available only when the user's
Plasma applet file contains a `plugin=org.kde.plasma.kickoff` section; the
`AppGridAvailable` probe keeps the group insensitive and explains the missing
target when no matching applet can be found.

Foundation previews embed symbolic artwork. Apache's mark uses the current
official ASF oak leaf from `https://www.apache.org/images/oakleaf.svg`, with
unchanged geometry adapted to monochrome `currentColor`. It is ASF trademark
artwork, not a Simple Icons CC0 asset; the source attribution distinguishes it.

`Classify(env)` is the pure decision and owns all three outcomes. It returns
`GNOME` when `XDG_CURRENT_DESKTOP` or `DESKTOP_SESSION` names GNOME or one of
the sessions built on it (`GNOME`, `gnome-xorg`, `GNOME-Classic:GNOME`,
`ubuntu:GNOME`, `pop:GNOME`, `gnome-flashback-metacity`); `KDE` when either
names Plasma (`KDE`, `plasma`, `plasmax11`, `kde-plasma`, a
`/usr/share/xsessions/plasma` session path) or, failing both, when
`KDE_FULL_SESSION` is the literal `true`; and `Unknown` when no variable
declares a desktop this package has a row for — a compositor with no ChairLift
surface (Sway, Hyprland, i3, Xfce, Cinnamon, MATE), a container or TTY where
all three are unset, or a marker set to a value other than `true`. `Detect()`
is the thin production entry point that reads those three variables from the
process environment; it composes no decision of its own. `Desktop.String()`
names all three constants and answers `Unknown` for any value outside the
enum.

Two properties are load-bearing rather than incidental. `Unknown` is the
enum's zero value, so a `Desktop` reached through a struct field or a map miss
fails closed instead of reading as a supported desktop. And detection is an
environment-variable read, never a process spawn: a detector that shelled out
to `plasmashell --version` or `gnome-shell --version` would report which
desktop is *installed* rather than which one is *running*, and would fail on a
minimal host. `internal/installcheck`'s `TestDesktopDetectionSpawnsNoProcess`
rejects an `os/exec` or `syscall` import and an `os.StartProcess` selector in
the package — including in its test files — so the property survives a later
rewrite that a PATH-based behavioral test would not catch.

The three variables are ranked, and a value the package does not recognize
does not end the search: each is an independent declaration, so `X-Cinnamon`
in the standard variable makes no claim about the two others, and one of them
may still be the specific answer. Rank matters in the other direction too,
which is why `XDG_CURRENT_DESKTOP` is read first — on Ubuntu it holds
`ubuntu:GNOME` while `DESKTOP_SESSION` holds only `ubuntu`, so reading the
session name first would find nothing and blame the search order for the miss.
`XDG_SESSION_DESKTOP` is deliberately not consulted: it duplicates the
standard variable wherever it is set, and every variable added to the decision
is another value that can disagree with the others and has to be ranked.
`deskenv_test.go`'s decision table covers Aurora, both Bazzite variants, stock
GNOME and Plasma on Wayland and Xorg, the distribution-prefixed GNOME
sessions, and the unrecognized desktops, with every `want` written out rather
than derived.

### Host capability floor (`internal/capability`)

`internal/capability` is the puregotk-free authority for what this host can
back: it answers, per page and group, whether the tool or asset that group
exists to drive is present. It exists because ChairLift's documented
degradation policy — a group whose backing tool is absent is hidden rather
than rendered inert — needs one read-only classification instead of the
mutually inconsistent answers the views layer once derived for itself.

A capability is the presence of a backing tool or asset, never a runtime
state. Flatpak, Distrobox and Podman resolve through `exec.LookPath`.
Homebrew uses `homebrew.ResolveExecutable`: PATH first, then the fixed Linuxbrew
fallback shared by visibility and execution. `BootcStage` and `ImageDescriptor`
resolve from `os.Stat` against `bootc.StageScriptPath` and
`imageinfo.DescriptorPath`.
Every probe is non-blocking by construction, which is the constraint page-level
resolution inherits: it runs synchronously on the GTK main thread during `buildUI`. That
is also the line between this package and the gates that stay asynchronous —
whether this machine is booted from a bootc deployment, whether updex has
features configured, and what `uupd.timer`'s systemd state is are all queries
rather than presence checks, so those groups keep their existing async gates,
build hidden shells, and are revealed once the query answers.

`Detect` resolves the host through the package's `Probe` seam, whose production
value is `exec.LookPath` and `os.Stat`; `SetProbe` replaces it, which is the
seam the `chairlift_e2e` walkthrough capability stub (`CHAIRLIFT_CAPABILITIES`)
uses. `DetectWith` is the pure core `Detect` wraps and what the tests drive
directly. Nothing here caches: the caller resolves once per session and keeps
the result, so sidebar accelerators and items cannot shift under the user's
cursor. That immutability
is a contract on the caller rather than a package-level cache, which would also
make a test's probe substitution order-dependent.

`Set.Supports(page, group)` is the `func(page, group string) bool` predicate
`navigation.VisibleItems` already accepts. It resolves the prerequisites table,
where a group is satisfied by **any one** of the listed capabilities:
`bootc_updates_group` needs the stage script, `agents_group` needs Homebrew,
`reset_group` needs Flatpak or Distrobox because powerwash's two steps
independently skip when their own tool is absent. A group with no capabilities
requires nothing of the host and is listed anyway, so that "no host
prerequisite" is a recorded decision rather than an omission; two of those
(`bootc_status_group` and `features_group`) are named in the table's own
comment because they read like omissions. `Compose(configured, set)` composes
the administrator's `Config.IsGroupEnabled` with a resolved set and pins the
floor's direction: configuration may subtract from the capability set and never
add to it, and a nil configuration predicate composes to false everywhere,
matching `VisibleItems`' treatment of a nil predicate and the repository's
fail-closed rule.

The table is total over the configuration schema, and both directions are
enforced. `internal/installcheck`'s `TestCapabilityPrerequisitesMatchConfigSchema`
holds `capability.Prerequisites()` and the `config.SchemaGroups(page)` pairs to
set equality, and `TestEveryConfigurableGroupIsClassifiedOnce` names the two
failure shapes separately — unclassified and duplicated — because they need
different fixes. The totality gate is what keeps the floor honest, because
`Supports` reports an *unclassified* pair as supported: that is deliberate, so a
missing entry cannot silently hide a group at runtime, which means the runtime
answer alone will not reveal the mistake. Without the gate, a group added to
`config.yml` and wired into a view would render on hosts whose backing tool is
absent — the exact degradation policy this package exists to enforce — and no
other gate would notice.

The Help page's "Why is something missing?" expander is the floor's one
explanation surface. `pageview.UnavailableFeatures(set, configured)` walks
`capability.Prerequisites()` with the window's already-resolved set — it never
re-probes — and lists each group that configuration enables but `Supports`
rejects, titled for a person and subtitled with the missing capability. A group
configuration disabled is the administrator's choice and is not listed. Its
title table is held total over the capability-gated groups by
`TestEveryCapabilityGatedGroupHasATitle`.

The package's own tests are table-driven and derive their cases from the tables
they cover, rather than restating them. `TestDetectWithResolvesEveryCapability`
walks every capability either probe table provides, across each single-tool
host, the two asset-capability splits, a fully capable host, and a probe that
cannot answer at all. `TestEveryRequiredCapabilityHasAProbe` rejects a
prerequisite naming a capability no probe resolves — a typo in the table still
compiles, because a `Capability` is a string. `TestSupportsAnyOneCapabilityOfItsGroup`
covers every prerequisites entry: the empty host, each of a multi-capability
group's capabilities in isolation, and a host holding only unrelated
capabilities. Running this file is itself the purity check ADR-0007 describes:
a puregotk import anywhere in the dependency graph would panic at package init,
before any test function ran.

Every entry path consumes the resolved set `Window.buildUI` detects once. The
sidebar (`navigation.VisibleItems`) and the first-run assistant take
`Window.effectiveEnabled`, which is `Compose(Config.IsGroupEnabled, set)`, and
`views.New` receives the same set so each view builder's `groupEnabled` composes
the identical predicate (#205). The update coordinator is the one consumer that
keeps the two facts apart: `buildUI`'s `sourcePolicy` hands
`updateflow.Coordinator.Check` a `map[SourceID]updateflow.Policy` whose
`Configured` is `Config.IsGroupEnabled` and whose `Supported` is
`Set.Supports`. A source is checked only when both hold and its provider is
available, exactly as the composed predicate would allow, but a source the host
cannot back is reported unavailable ("Not available on this computer") rather
than "Disabled by administrator", which `updatepresent` reserves for
`Configured` false. The views' own "not installed" placeholder branches that
the floor made unreachable were removed in #206.

### Package manager wrapper pattern

Each wrapper in `internal/` follows a consistent shape:

- Reads `dryrun.Enabled()` from the single process-wide `internal/dryrun` package (see "Dry-run mode" above) rather than keeping a package-level flag
- `IsInstalled()` to check tool availability, plus `IsInstalledCached()` (`sync.Once`) for use from views during async startup
- Homebrew, Flatpak, and Updex implement both `IsInstalled()` and `IsInstalledCached()`
- List/Install/Uninstall/Update functions
- Context-based timeouts. Homebrew and Flatpak both use a two-class model selected per invocation by an unexported `commandTimeout(args)` helper: 30s for read-only commands, 30m for state-changing ones (the keys of each package's `stateChangingCommands` map). updex uses 5min and bootc 30min.
- Custom error types where needed

### Shared OS staging progress (`internal/stageexec`)

`bootc.StageUpdate` retains its provider API and fixed command —
`pkexec /usr/libexec/bootc-update-stage` — but delegates execution to the
pure-Go `internal/stageexec` leaf package:

1. The caller creates the provider's `ProgressEvent` channel; the provider
   type is an alias of `stageexec.ProgressEvent`.
2. Each non-empty output line becomes an `EventMessage`; the channel is closed after either an `EventComplete` (success) or the function returning an error
3. Event types: `EventMessage` and `EventComplete` — deliberately simpler than
   a step/percent model because the stage script's own output is unstructured
   log lines, not a structured progress protocol. Failures return as errors;
   they are not duplicated into the event stream.
4. `stageexec.Run` owns merged stdout/stderr, non-zero exits with the last output
   line, deadline/cancellation classification, missing bare-name or absolute-path
   executables, direct-child kill/reap, the single success completion, and
   channel closure. `stageexec.DryRun` owns the synthetic preview/completion and
   closure without constructing an `exec.Cmd`.
5. The bootc view goroutine reads that event contract and
   dispatches UI updates to the main thread via `sgtk.RunOnMainThread`.

**Caller-visible outcomes.** The provider adapter preserves `bootc.Error`
and `NotFoundError` while carrying the shared executor's
message and cause. Both context-taking bootc functions classify failures with
`errors.Is` against the context sentinels, and `bootc.Error` has an `Err error`
field plus `Unwrap() error` so callers can tell them apart:

- `StageUpdate` — deadline: provider `*Error` "Update staging timed out" unwrapping to `context.DeadlineExceeded`; cancellation: provider `*Error` "Update staging was canceled" unwrapping to `context.Canceled`; non-zero exit: provider `*Error` "update staging failed (exit N): <last output line>" matching neither sentinel; missing `pkexec`: provider `*NotFoundError`.
- `GetStatus` — composefs status is read from world-readable deployment files; otherwise the CLI adapter preserves deadline/cancellation sentinels, non-zero exit diagnostics and missing-tool errors. `getStatusFrom` is the CLI test seam, not a bypass of composefs detection in the public entry point.

The deadline and cancellation messages differ in both functions, and neither ever surfaces as `signal: killed`.

**OS staging direct-kills; homebrew and flatpak kill the process group.** On
cancellation `stageexec.Run` kills only the direct child (`cmd.Process.Kill()`)
and sets no `Setpgid`, because both staging adapters run under `pkexec` and the
privileged child cannot be signaled as an unprivileged process group. The
unprivileged Homebrew and Flatpak runners instead kill their whole process
groups so download helpers are not orphaned. Making either privileged staging
path group-killable is a privilege-model change.

**`helperexec` bounds the wait; it does not stop the work.**
`internal/helperexec.Run` has the same privilege constraint and sets a finite
`WaitDelay` (5s, matching `internal/maintenanceexec`) so cancellation returns
even when a privileged descendant of the helper inherited stdout/stderr and
still holds those pipes open. Only the direct `pkexec` child is killed, so
that descendant may keep mutating the system after `Run` returns; the
cancellation message therefore reads "command canceled (privileged work
already started may still be running)" rather than implying the action
stopped, and surfaces rendering it must not imply otherwise. Coordinating
deadlines with root descendants (issue #82's broader ask) would require a
privilege-model change.

Because `WaitDelay` applies to every run, `cmd.Run` can report
`exec.ErrWaitDelay` for a helper that already finished. `Run` classifies from
the helper's own exit status in that case — success stays success (with
possibly truncated captured output) instead of inviting a retry of work that
already happened, and a non-zero exit keeps its "command failed (exit N)"
message. For the same reason a cancel or deadline racing a genuine helper
failure does not mask it: the context classification applies only when the
helper was killed rather than exiting on its own. `helperexec.Error` carries
an `Err error` with `Unwrap`, so "command timed out" and the cancellation
message match `context.DeadlineExceeded` / `context.Canceled` under
`errors.Is`, the convention `stageexec` already follows.

**Why a stage script instead of `bootc upgrade`:** upstream `bootc upgrade`'s registry-transport pull currently fails on snow's composefs images. The snow-shipped `/usr/libexec/bootc-update-stage` script works around this: `podman pull` fetches the image into containers-storage (podman's pull path works where bootc's does not), then `bootc switch --transport containers-storage` stages the already-pulled image as the next boot deployment. This keeps the actual upgrade logic in one place (the stage script) rather than duplicating pull/switch orchestration in ChairLift; ChairLift only invokes the script via pkexec and streams its output. The script is idempotent — it exits 0 without staging anything when the deployment is already current.

### bootc progress UI (updates page)

`onBootcStageClicked` admits staging through `UpdateShell.beginMutation`, disables its button and starts a native spinner. `stageProgressSink` coalesces streamed messages into bounded main-thread callbacks and caps permanent log rows at `progresslog.DefaultLimit`; the subtitle discloses omitted lines. Streamed text renders with markup disabled. Completion waits for channel closure and the staging worker, then re-reads bootc status rather than interpreting command output. `finishMutation` releases admission and restores controls; a successfully verified live stage starts a fresh coordinator check. A successful status read refreshes Compare references, while a failed read preserves known state and reports that the outcome could not be verified. A successful stage command alone never proves a restart is required: the stage script is idempotent.

### Update badge tracking

`internal/updateflow.Coordinator` owns the source inventory and aggregate badge.
The shell renders its immutable snapshot; manual update actions share its mutation
admission and request fresh observations after live success. Failed observations
preserve confirmed inventory instead of inventing a zero count. No separate
`UserHome` badge counter or Flatpak update-status owner exists.

### Privileged operations

Decision records: [ADR-0001](../adr/0001-fixed-path-pkexec-privilege-boundary.md)
(the fixed-path pkexec boundary and helper argv re-validation) and
[ADR-0002](../adr/0002-usr-prefix-is-the-only-supported-install-prefix.md)
(`PREFIX=/usr`). ADR-0006's two-package split is superseded: ChairLift ships
only the release archive described under "Privileged integration delivery"
below.

bootc staging, updex, and Bluefin-family system operations
require root for state-changing operations. They invoke commands through
`pkexec` (PolicyKit). bootc runs `pkexec /usr/libexec/bootc-update-stage`
directly (polkit action id `io.projectbluefin.chairlift.bootc.stage`), updex delegates to the fixed
absolute path `internal/updex.HelperPath` (`/usr/bin/chairlift-updex-helper`),
and Bluefin-family writes delegate to `internal/ublue.HelperPath`
(`/usr/bin/chairlift-helper`). Policy files are installed for all three
fixed surfaces: `data/io.projectbluefin.chairlift.bootc.policy`,
`data/io.projectbluefin.chairlift.updex.policy`, and
`data/io.projectbluefin.chairlift.ublue.policy`.

The fourteen ublue commands include fixed `kvm-enable`, `docker-enable`, and
`docker-disable` actions alongside pin/unpin. The parser accepts no arbitrary
account, service, image or command argv. Developer options stay visible when
their installed actions are missing, with the affected switches insensitive.
WSL Mode defaults to nsl on Linux amd64, with Lima as an alternative backend;
an existing Lima-only machine retains Lima. Both require accessible `/dev/kvm`;
a new permission grant needs a new login.
Docker reports ready only with an accessible live daemon socket. IDE/editor
installs are selective and contain one JetBrains Toolbox entry.

**Why the helper paths must be absolute, and why `PREFIX=/usr`:** `pkexec`
resolves the program it's asked to run to an absolute path and compares it
textually against the `org.freedesktop.policykit.exec.path` annotation on each
action. The updex policy's three actions annotate
`/usr/bin/chairlift-updex-helper`; the ublue policy's fourteen actions annotate
`/usr/bin/chairlift-helper`. Both helper policies use
`org.freedesktop.policykit.exec.argv1` to select exactly one action for the
first helper argument. PolicyKit does not validate the remainder of argv, so
the privileged helpers are a second boundary: `internal/updexhelper.ParseInvocation`
accepts only `enable-feature <name> [--dry-run]`, `disable-feature <name>
[--dry-run]`, and `update [--dry-run]`, while
`internal/ubluehelper.ParseInvocation` accepts only `channel-switch
<stable|testing> [--dry-run]`, `dx-enable [--dry-run]`, `dx-disable
[--dry-run]`, `restart [--dry-run]`, `rollback [--dry-run]`,
`auto-updates-enable [--dry-run]`, `auto-updates-disable [--dry-run]`,
`driver-switch <standard|nvidia|nvidia-open> [--dry-run]`, `factory-reset
[--dry-run]`, `pin <YYYYMMDD> [--dry-run]`, `unpin [--dry-run]`,
`kvm-enable [--dry-run]`, `docker-enable [--dry-run]`, and
`docker-disable [--dry-run]`. A bare, `$PATH`-resolved command name can resolve to a different
absolute path depending on the invoking process's `$PATH`, which makes the
path comparison miss and falls `pkexec` back to the generic, more restrictive
action. The wrapper packages therefore always invoke their fixed `HelperPath`
constants, never a bare name.

Two inputs deliberately never cross the pkexec boundary as arguments:

- **The target image reference.** Only a validated channel, driver, or day word is passed. The helper
  resolves the concrete reference itself, from the read-only image descriptor
  at `internal/imageinfo.DescriptorPath` and the channel table below. An
  authenticated caller therefore cannot direct `bootc switch` at an arbitrary
  registry. When `bootc switch` fails and the derived target is the image of
  the rollback deployment — switching back to the channel or driver the host
  just left, which composefs refuses as a duplicate fs-verity digest —
  `channel-switch` and `driver-switch` run the fixed `bootc rollback` argv
  instead, so the request still lands on that image at the next boot.
- **The username.** The helper resolves it from the `PKEXEC_UID` that pkexec
  sets on the invoking session (`internal/ubluehelper.TargetUID`, which
  rejects an absent, non-numeric, or root value), so an authenticated caller
  cannot add an unrelated account to the privileged developer groups.

**Privileged action journal (`internal/journal`):** Every privileged dispatch
point through `helperexec.Run` unconditionally journals before execution
(`journal.Record`), recording the action name, input arguments, and would-run
argv, marked as `suppressed: "dry-run"` in preview mode or `suppressed: "no"`
for live attempts. After a live command returns, `helperexec.Run` records the
execution outcome (`journal.RecordOutcome`) with `outcome` in `succeeded`,
`refused` (PolicyKit authentication dismissed or denied with pkexec exit status
126, or 127 when not an unexecutable helper execution error), `failed` (other
non-zero exit with exit code, including a missing or unexecutable helper),
`timed-out`, or `cancelled`. Before executing a derived privileged command
(such as a concrete `bootc switch` target), `cmd/chairlift-helper` prints a
machine-readable line (`chairlift-helper: exec <argv json>`); `helperexec.Run`
parses this line from output, strips it from caller-visible stdout/stderr, and
includes the concrete argv list as `executed` (`[][]string`) in the journal
outcome record. The marker is self-reported and can also be emitted by helper
children; it is an audit aid, not independent proof of execution.
`chairlift-updex-helper` calls the updex library in-process and runs no
subprocess, so its outcome records carry no `executed` list.

Gaming mode, the third Bluefin-family feature, adds no ChairLift privilege
route: every component is installed as a **system-scope** Flatpak with
`flatpak install --system` (#503), from the Flathub remote Bluefin-family
images configure system-wide — a `--user` install cannot resolve a ref there
at all (#501). The `flatpak` CLI authorizes system installs and uninstalls
itself through Flatpak's own PolicyKit actions
(`org.freedesktop.Flatpak.app-install`, `runtime-install`, and their
`-uninstall` counterparts), so there is no `pkexec`, helper subcommand, or
ChairLift PolicyKit action for gaming mode, and none may be added.

Its components are not all applications, and `internal/gaming` models the
difference rather than assuming it away. Each entry in the stack carries a
`flatpak.Kind` — five are `KindApplication`, and MangoHud
(`org.freedesktop.Platform.VulkanLayer.MangoHud`) is `KindRuntime`, because
it ships as a Vulkan-layer extension of `org.freedesktop.Platform` rather
than as an app. `flatpak list --app` and `flatpak list --runtime` are
mutually exclusive filters, so the inventory runs one query per
(scope, kind) pair — four in total — and keys its result on a
`gaming.Ref{Kind, ID}` rather than on the ID alone. An application-only
inventory reported MangoHud missing however it had been installed, which
made Enable reinstall it on every run and left Disable unable to remove the
ref ChairLift had put there (issue #75). Failure handling follows the same
shape one level up: every (scope, kind) query must answer, because an
unreadable scope could hide a copy and reporting its components missing is
exactly the loop that bug was.

MangoHud is also the one multi-branch component: Flathub publishes the layer
once per Platform release (21.08 through 26.08 today), so a bare
`flatpak install -y` or `uninstall -y` of its ID stops at flatpak's
"Which do you want to use?" prompt and, with no stdin, fails with "No ref
chosen". Its `Component.BranchOf` names Steam, which loads the layer: Enable
installs `ID//BRANCH` with the branch of the runtime Steam runs on
(`flatpak info --show-runtime`, or `flatpak remote-info --system --app
--show-runtime flathub` when Steam is not installed), and fails that one
component rather than guess when neither answers. The inventory records each
ref's installed branches per scope (`flatpak list --columns=…,branch`), and
Disable removes every installed branch by its qualified ref.

The Features page offers individual selections, not a single all-or-nothing
switch. Enable and Disable validate the selected IDs before mutation. Enable
installs only components missing from both scopes, so a copy an earlier
release installed per-user is not duplicated system-wide. Disable removes a
selected component from exactly the scopes it is installed in — `--user` for
a per-user copy, `--system` for a system copy, both when both exist — and
never touches an unselected component; its confirmation dialog says that a
system-wide copy goes for every account. The exception is a system copy the
OS image declares it ships — a `[Flatpak Preinstall]` group in
`/usr/share/flatpak/preinstall.d` or `/etc/flatpak/preinstall.d`, or a
`flatpak` entry in `/usr/share/ublue-os/homebrew/system-flatpaks.Brewfile`
(Flatseal on Bluefin and Dakota). That copy predates gaming mode and is left
in place: removing it would take a distro default from every account, and
Flatpak records that uninstall as a permanent opt-out, so `flatpak
preinstall` would not restore it. Such a component is reported as left in
place, not removed, and a declaration that cannot be read fails the removal
before anything runs. A component with a removable copy left is reported as a
failure, partial outcomes stay visible, and a dry-run keeps the confirmed
inventory unchanged.

### Desktop integration switches (`internal/shellextensions`)

`features_page.desktop_integrations_group` owns the Tailscale Integration and
Sync Folder Integration rows. The pure-Go provider reads `gnome-extensions
list` and `list --enabled`, preserving GNOME's choices, and permits only
`enable`/`disable` with the two fixed UUIDs from Dakota's Tailscale QS build and
Bluefin Bling's Sync Folder metadata. No service or privileged helper is called.
The view uses guarded switches, disables input during reads and writes, and
re-reads after mutations to detect rejected changes. Failed verification leaves
the switch insensitive; missing tools, sessions and extensions are explained.
Default presentation is Tailscale on and Sync Folder off, but existing GNOME
state always wins. OS-image extension defaults remain owned by the image.

### Custom Command Menu developer visibility (`internal/devmenu`)

`internal/devmenu` manages the Custom Command Menu GNOME Shell extension (`org.gnome.shell.extensions.custom-command-list`) visibility for developer tools (Terminal and Containers):

- **Tuple scanning:** Entries are stored across `command1` through `command99` as `(sssb)` GVariant tuples representing `(label, command, icon, visible)`. `devmenu.Apply` scans entries by label matching `Terminal` or `Containers` rather than hardcoding fixed command indices, since Bluefin ships Terminal at `command8` with Containers absent, while Dakota ships Terminal at `command8` and Containers at `command9`. The scan is one `dconf dump /org/gnome/shell/extensions/custom-command-list/` rather than a read per key: dconf resolves through the whole profile, so the dump already carries distro defaults as well as user overrides, and a toggle spawns a handful of processes instead of ~300. The only remaining per-key call is `dconf read -d` for a matched entry, needed to tell an override from a reset.
- **Field preservation:** Non-visibility tuple fields (`command`, `icon`) and unrelated commands are preserved intact.
- **Distro vs. user layer:** Shipped distro defaults come from `/etc/dconf/db/distro.d/`. Toggling visibility distinguishes enforcing policy from resetting to defaults: when the desired state matches the distro default (e.g. `visible=true` on developer enable), `devmenu.Apply` resets the user key (`dconf reset`) so future distro defaults continue to shine through. When turning Developer Mode off and the distro default has `visible=true`, it explicitly writes `visible=false` to the user layer because a reset would reveal the visible default.
- **Supported environments and preview:** A missing extension or non-GNOME environment (e.g. Plasma) is a supported no-op (`devmenu.Apply` returns `nil`), whereas operational read/write failures return an error and are not hidden as success. Under preview (`dryrun.Enabled()`), mutations are skipped and logged as `[DRY-RUN] would set Custom Command Menu <key> visible=<bool>` (or `[DRY-RUN] would reset Custom Command Menu <key> to default` for resets). The call site composes no tuple or visibility text of its own.

### Unified update run (`internal/updateflow`)

The Updates destination is `internal/views.UpdateShell`, a status-first
surface in three layers that must stay separate:

- `internal/updateflow` is the pure coordinator. `Coordinator.Check` checks,
  concurrently, every source that is configured, supported by the host
  capability floor, available, and enabled in the user's preferences;
  `Coordinator.UpdateAll` applies pending items serially in the provider order
  `Window.buildUI` constructs — applications (Flatpak), developer tools
  (Homebrew), system components (updex), then the operating system (bootc
  staging). Every published `Snapshot` is immutable and generation-guarded, so
  a stale worker cannot overwrite a newer state. A failed source does not
  abort the run; `ActionRetryFailed` re-applies only sources that still carry
  an apply error. Post-update maintenance (`updateproviders.NewMaintenance`,
  gated by `maintenance_freespace_group`) runs only after a clean live run and
  only when the user's `MaintenanceAfterUpdates` preference is set.
- `internal/updateproviders` holds the production `updateflow.Provider`
  values, which wrap `internal/flatpak`, `internal/homebrew`, `internal/updex`,
  and `internal/bootc`; the coordinator executes nothing itself. Flatpak
  reconciliation verifies that all applied refs for each executed scope have
  cleared, ignoring newly appeared updates.
- `internal/views/updatepresent` maps one snapshot to the shell's status
  line, failure detail line, and action label, and each source's row
  subtitle. Failures are said in plain words; the raw error is logged by the
  coordinator where it is recorded, never shown. The sentence after a failure
  comes from `updatepresent.FailureHint` and names a cause only when the
  error shows one: "Check your internet connection." for a Go network error
  or a tool message naming a failed connection (an unresolved host, a refused
  or timed-out connection), "It took too long." for an exhausted deadline,
  and otherwise "Details are in the log." — a local Flatpak, Homebrew, or
  bootc failure is not told to check a connection it never used. The shell's
  single-row update and tool-refresh failures use the same hint. A source
  whose policy has `Configured` false reads "Disabled by administrator"; one
  that is configured but not `Available` — the provider's own `Available`
  probe says no, or `Policy.Supported` is false because the capability floor
  cannot back it — reads "Not available on this computer". `ItemRows` decides
  which pending items get a child row: the Operating system source's one pending
  item is its deployment, so that source shows "Update available: <booted> → <new>"
  on its own row and gets no child row repeating its name. The fold is keyed on
  the source ID, never an item's name; every other source keeps one row per item,
  because Applications and Developer tools carry each item's only Update button
  on that row.

The shell learns each source's policy from `Window.buildUI`'s
`sourcePolicy`, a `map[SourceID]updateflow.Policy` with `Configured` from
`Config.IsGroupEnabled` and `Supported` from `capability.Set.Supports` (see
"Host capability floor" above). The four sources are keyed to
`bootc_updates_group`, `flatpak_updates_group`, and `brew_updates_group` on
`updates_page`, and `features_group` on `features_page`.

Everything else the Updates page owns is built by `buildUpdatesPage` into
`UserHome.updatesPrefsPage`; `Window.buildContentArea` mounts it below the
shell's source rows with `UpdateShell.SetSecondaryContent`. The secondary
groups, in order, are automatic updates, System version, System update details
(dedicated download and Compare), Unverified sources, and Advanced
channel/graphics controls. Application/tool source rows and item actions belong
to the shell rather than duplicated preference groups. Configuration,
capability and asynchronous runtime gates determine which groups are shown.
The automatic-updates switch uses `guardedSwitch`: programmatic rollback after
failure or preview must not request the opposite mutation.

Restart is the run's only privileged surface of its own. `PhaseRestartRequired`
is reached only when a source reports that a restart is required — the OS
provider reads it from `bootc status`'s staged deployment, because staging an
already-current system succeeds without staging anything (on composefs
`bootc.StageUpdate` answers from the registry and skips the script, whose
`bootc upgrade` fails on a current system) — and the header offers no
primary button; its one status line reads "Restart to finish updating".
The header is a plain box built once in `UpdateShell.build`: the wordmark,
then the primary action (or, while checking or installing, the pulsing
progress bar in its place), then one status line from
`updatepresent.Presentation.Status`, with a second `Detail` line only for
the two failure phases. `Presentation.ShowStatus` hides the header box
whenever a presentation leaves it empty, so it adds no spacing. The shell
uses 12px content spacing and a 12px top margin, and announces each phase's
status line from the visible toast overlay. The status line carries the
accessible description "Update status", which tells it apart from a source
row with the same words. The Operating
system row carries the action: its subtitle reads "Deployment
staged" and a "Restart now" suffix calls `UpdateShell.StartRestart`, which
sends `ublue.Restart` through the `chairlift-helper` `restart` subcommand.
Its argv is the fixed `systemctl reboot` (`ubluehelper.RestartArgs`) with no
delay and no target; scheduled restarts would each need their own action.

After a live run or a live single-row update (which reports its one source
as completed), `UserHome.OnUpdateFinished` refreshes the installed Homebrew
inventory when its source completed and, when the OS source completed,
re-reads status to refresh Compare references. The coordinator remains the
badge owner. A preview refreshes nothing.
`UpdateShell.notifyUpdateComplete` sends the single desktop notification
(see below) and skips previews.

### Action journal and desktop notifications

`internal/journal` is a port of finupdate's `action_journal.rs`: JSONL intent
records plus live helper outcome records, appended when `$CHAIRLIFT_ACTION_JOURNAL` is set,
a no-op otherwise. A dry-run invocation is recorded with
`Suppressed: SuppressedDryRun` and the argv that would have run, which is
what lets a test assert the fixed helper command and validated word arguments
without granting privilege; image references are derived inside the privileged
helper, not sent by the GUI.

ChairLift escalates through three choke points, and the record is written at
each of them rather than at the call sites that reach them:

| choke point | covers | polkit actions |
|---|---|---|
| `internal/helperexec.Run` | both fixed-path helper binaries, via `internal/ublue.runHelper` and `internal/updex.runHelper` | 14 `…ublue.*` + 3 `…updex.*` |
| `internal/stageexec.Stage` | the bootc stage script, via `internal/bootc.StageUpdate` | `…bootc.stage` |
| `views.UserHome.runMaintenanceAction` | config-declared maintenance scripts run `sudo` | none; falls back to `org.freedesktop.policykit.exec` |

The journal-contract gate in `internal/installcheck` classifies execution sites
as privileged or unprivileged and requires privileged dispatch to record both
live and suppressed invocation intent. New executors must not bypass it.

`internal/notify` sends exactly one desktop `GNotification`: the unified update
run's completion (`notify.UpdateAllComplete`, sent from
`UpdateShell.notifyUpdateComplete`), through `views.ToastAdder.NotifyBackground` (implemented by
`internal/window.Window`, the one place holding a `*gtk.Application` handle).
It is the only ChairLift action long enough that a user plausibly stepped
away before it finished; every other toggle completes in view and already has
a toast, so a second notification there would be noise the simple-interface
constraint rules out.

### Troubleshooting (`internal/troubleshoot`)

`internal/troubleshoot` is the engine behind the Agents page's Goose row
(`agents_page.troubleshooting_group`); `internal/agentmode` decides readiness
and owns the launch. Everything Goose reads is ChairLift's own: a session
runs in a dedicated profile under `$XDG_DATA_HOME/chairlift/troubleshooting`
(`troubleshoot.Profile`). `GOOSE_PATH_ROOT` points Goose's config, data, and
state at `goose/`; `XDG_CONFIG_HOME` points the desktop app's Electron
profile and single-instance lock at `desktop/`, where `Profile.Write`
symlinks the user's `mimeapps.list` and `dconf` so links open in the user's
browser and GSettings still apply. The user's `~/.config/goose` is never
read or written, and a Goose window the user already has open is never
handed this launch.

`Profile.Write` runs immediately before every launch and is atomic and
dry-run gated. `RenderConfig` is the complete tool surface: `linux-tools`
(the absolute `linux-mcp-server` with `--toolset FIXED --host-mode
LOCAL_ONLY --no-search-for-ssh-key`) and `bluefin-knowledge` (streamable HTTP
at `https://mcp.projectbluefin.io/mcp`, `available_tools: [search_knowledge]`)
are enabled; every other extension already in the profile file — one a later
Goose added — is carried over disabled, and Goose 1.53's platform extensions
(`developer`, `extensionmanager`, …) are written disabled, because Goose adds
a missing platform extension with its own default and keeps an `enabled`
value already present. No provider, model, or key is written: llmman hands
Goose its provider through the environment. `hints.md` becomes the profile's
`.goosehints`. Knowledge searches go online, so no copy claims a session's
questions stay on this computer. The hint explicitly names the `linux-tools`
tools (`get_system_information`, `get_disk_usage`, `get_cpu_information`,
…) so a small local model — the default Qwen3-8B preset — reaches for them
instead of answering from training data (issue #523); it also forbids
inventing system facts the tools would have returned.

`troubleshoot.Setup` installs what is missing, with `Needed` per step so a
half-done install resumes: `ublue-os/tap` (only when a package is missing),
`linux-mcp-server`, then `cpio` before the `goose-linux` cask — the cask's
preflight pipes its RPM through a `cpio` it does not declare and Bluefin
does not ship. Each `ublue-os/tap` package is trusted by its qualified name
(`brew trust --formula`/`--cask`) right before its install when brew reports
the tap untrusted, because Homebrew refuses packages from an untrusted tap.
Goose Desktop is published for x86_64 only, so Setup returns
`ErrUnsupported` elsewhere. Homebrew's `stateChangingCommands` includes
`tap`, so dry-run never changes package sources. Nothing crosses a privilege
boundary.

`internal/aistack` is Agent Mode's runtime owner; [ADR-0015](../adr/0015-agent-mode-llmman.md)
is the contract. [llmman](https://github.com/llmmanorg/llmman) chooses the
engine and backend for the hardware (container runtime, prebuilt binary, or a
`llama-server` on `$PATH`) and owns the model store. ChairLift presents active
model selection and recommended presets without a GPU-vendor stack matrix.
Enabling:

1. Renders `Brewfile(haveLLMMan)`, with `tap "llmmanorg/tap"` and
   `brew "llmmanorg/tap/llmman"` only when no executable resolves. In that
   case it first taps `llmmanorg/tap` and trusts exactly that formula
   (`brew trust --formula llmmanorg/tap/llmman`) when `brew tap-info --json`
   reports the tap untrusted — Homebrew refuses a formula from an untrusted
   tap, and Homebrew before 6 has neither tap trust nor `brew trust` — then runs the bundle through
   `homebrew.BundleInstall`. Runtime provisioning installs no chat
   client; Goose setup is the Goose row's Set Up.
2. Resolves `llmman` (`$PATH`, then beside `homebrew.ExecutablePath()`), and
   runs `llmman serve --pull-only`, which fetches the engine in the
   foreground and fails if it cannot — the one mode in which llmman treats a
   failed fetch as an error.
3. Atomically writes `~/.config/systemd/user/chairlift-llmman.service`
   (`RenderUnit`: `ExecStart=<abs> serve`, `LLMMAN_HOST=127.0.0.1:17434`,
   `LLMMAN_SHELL=off`, `LLMMAN_NOHISTORY=1`) and
   `~/.config/environment.d/10-chairlift-llmman.conf`
   (`OLLAMA_HOST=127.0.0.1:17434`), then `enable --no-reload` and the shared
   reload/restart/invocation-stamp boundary. A pre-start failure removes only
   what the call created. After a possible start, cleanup uses `Disable`'s
   stopped-state proof; an uncertain stop preserves both management files.
4. Best-effort `dbus-update-activation-environment --systemd
   OLLAMA_HOST=127.0.0.1:17434`, so processes started afterwards in this
   session see it. Running processes are not changed, and the ready subtitle
   says so.

The view then waits up to a minute for `GET /llmman/node` (`WaitHealthy`,
each probe bounded to two seconds). `Resolve(Facts)` is the pure state
function behind the row: unavailable (no Homebrew), unconfigured,
provisioning (in flight, or unit present and not yet probed), ready (unit
plus a JSON answer from `/llmman/node`), degraded (unit, no answer), and
disabled (llmman installed, no unit). On page build the non-blocking facts
render immediately and the health probe runs off the main thread.

Model selection saves the canonical `hf.co/...` target as `bluefin-active`,
restarts the owned user unit, and waits for `/llmman/node` readiness before
confirming the selection. llmman reads aliases at startup, so configuration
alone is not a serving-model guarantee. A missing alias renders no selection.
The owned unit also sets empty `LLMMAN_PEERS=` to override existing aggregation
configuration. Removing the remote-machine controls therefore cannot leave
hidden offload enabled; unrelated llmman configuration is not rewritten.

Before an installed service is reported ready, the startup worker calls
`ReconcileService`: compare the owned unit with `RenderUnit` and require its
recorded systemd `InvocationID` to match the running invocation. Disk equality
alone cannot prove adoption after a crash between write and restart. Missing
or mismatched stamps leave the new unit unstamped until the shared
`restartService` boundary reloads, restarts, reads a validated live ID, and
atomically records it as a unit comment. Enable and alias selection use the
same boundary. A verified invocation incurs no restart; a natural service
restart changes the ID and conservatively triggers one reconciliation.
Disabled hosts are not configured and previews write nothing. Failure keeps
the unstamped management unit and best-effort stops the old daemon, preventing
generic HTTP health from declaring unverified policy ready. Live startup
allows ten seconds for health; previews retain the bounded single probe.
No new state artifact or privileged read is introduced, and unrelated llmman
configuration remains intact.

Disabling runs `systemctl --user disable --now`, then removes the unit and
the fragment and `unset-environment OLLAMA_HOST`. A failed stop is accepted
only when `is-active` reports `inactive`, `failed`, or `unknown`; otherwise
both files stay and the UI says the service is still running. Binaries
and models are never removed. Nothing is privileged, so there is no helper
subcommand and no PolicyKit action.

#### Contribute to Bluefin (`internal/contribute`)

`internal/contribute` manages preflight and command construction for launching
a contributor session from the Agents page. It launches Common's merged `ujust
contribute` recipe through `xdg-terminal-exec`, running the foreground
contributor container appliance.

Preflight is read-only, uses injectable probe seams, and executes off the GTK thread (`Preflight`):
1. `xdg-terminal-exec` on `$PATH` to launch the terminal emulator.
2. `ujust` on `$PATH`.
3. `ujust --summary` containing the `contribute` recipe.
4. `podman` on `$PATH`.
5. Hive registration file present at `${HIVE_CONTRIBUTE_REGISTRATION:-$HOME/.config/hive/contributor.env}`.

When any check fails, the row displays an actionable subtitle (including a link
to registration setup when the registration file is missing) and leaves the
action button insensitive. Ready actions invoke `launcher.Start`, reporting
launch failures asynchronously through the UI toast surface. Previews under
`--dry-run` log the launch command without opening a terminal or spawning a worker.

### Printers (`internal/printerapp`)

`internal/printerapp` writes rootless quadlets under
`~/.config/containers/systemd`, driven with `systemctl --user` in the invoking
account, with a unit, host port and state volume per app.
[ADR-0016](../adr/0016-printer-app-admin-denied-until-authenticated.md)
is the contract; [printer-applications.md](printer-applications.md) records
the network surface, the family inventory (units, ports, volumes), and the
published-image state, and this section is the Control Center surface on
top of it. Nothing is privileged: there is no helper subcommand and no PolicyKit
action, and the package's one exec site is classified unprivileged in
`internal/installcheck`'s journal-contract inventory.

The Features page renders it as the **Printers** group (`printers_group`,
`internal/views/printers_page.go`), floored on the `Podman` capability —
`podman` on `$PATH`, since a quadlet is a Podman feature. One `guardedSwitch`
row per `printerapp.Families()` entry, titled by `pageview.PrinterFamilyRow`
and connected once at build time. What the row shows comes from the pure
readiness model, never from the unit file alone (#331, #361):

- `Observe(app, capable)` is the non-blocking half, safe on the GTK main
  thread: the unit file's presence and `CanEnable`'s answer.
- `ProbeActive(ctx, app)` is `systemctl --user is-active <service>`, off the
  main thread; it returns the state *word*, because systemctl's exit status
  is non-zero for every word but `active`, and treats multi-word output (no
  user manager, no bus) as a failed probe rather than a state. `WaitSettled`
  re-asks, bounded, while the word is `activating`/`reloading`, so a first
  start's image pull does not leave the row saying "Starting…" forever.
- `Resolve(Facts)` maps to `StateUnavailable` (no Podman), `StateBlocked`
  (`CanEnable` refused and no unit), `StateOff`, `StateStarting` (unit, not
  yet checked or activating), `StateReady` (active — the subtitle names
  `http://localhost:<port>/`, where PAPPL serves both IPP and the web page),
  or `StateFailed` (unit present but failed, inactive, or uncheckable). A
  present unit is never Blocked: the user turned it on, and turning it off
  must stay possible whatever the image's administration surface.
- `ProbeDiagnostics` is the view's runtime observation: it combines the
  readiness facts with systemd properties, recent user-journal output and
  container-image presence. `Diagnose` distinguishes plugin verification,
  rootless device access, service crash and image failures from generic failure;
  these diagnostics do not unlock administration or claim hardware testing.

`CanEnable(Family)` is the ADR-0016 enable condition as a queryable predicate
— an application may be enabled only when its web administration is
authenticated or absent — and `Enable` calls it before its dry-run branch, so
a preview never describes a forbidden change. No published image accepts the
setting yet. The image-side contract is specified in
ghostscript-printer-app#65 (mirrored in hplip-printer-app#51 and
gutenprint-printer-app#57): the entrypoint reads `PRINTER_APP_AUTH_SERVICE`,
`PRINTER_APP_ADMIN_GROUP`, and `PRINTER_APP_SERVER_OPTIONS` and forwards them
as `-o auth-service`, `-o admin-group`, and `-o server-options`. ChairLift
neither reads nor writes those names today; once an image ships them,
`CanEnable` returns nil for that family and `RenderUnit` gains the
`Environment=` lines that carry the values — that is the whole follow-up
that makes a switch live. Until then every family resolves to `StateBlocked`:
the row is shown with its switch off **and insensitive** and
`pageview.PrinterAppSubtitle` says it can't be turned on until its settings
(administration) page can be password-protected — that is, until the image
accepts an administrator credential. That is the actionable, non-enabled state the ADR asks
for — never a false enabled indicator and never a switch that silently does
nothing — and it encodes no unshipped environment variable. The toggle
handler (`onPrinterAppToggled`) is admitted by a per-family
`actionstate.Gate`, runs `Enable`/`Disable` off the main thread under a
bounded context, and settles the switch from `actionmsg.PrinterApp`: a dry
run and a failure both restore the switch and the row's last state, a failure
toasts `pageview.PrinterAppFailureToast`, and a failed disable keeps the unit
because the service could not be proven stopped. Structured marker:
`views: printers group built families=<n> blocked=<n>`, asserted by the E2E
walkthrough and the `@features` AT-SPI scenarios. Hardware behaviour —
printing through a real device, USB passthrough, mDNS coexistence — remains
unverified and unwired.

### Powerwash and Factory Reset

`internal/powerwash` is Powerwash's pure sequencer: `Runner.Run` executes the
two steps (removing every
user-scope Flatpak, removing every Distrobox container) through function
seams, and `Summarize` aggregates the outcome. A step whose tool is not
installed is `OutcomeSkipped`, not a failure — there is nothing for it to
remove. Both steps are unprivileged; `internal/flatpak.RemoveAllUser` and the
new `internal/distrobox` package (a minimal wrapper existing only to detect
Distrobox and remove every container) are the real implementations.

Factory Reset is `bootc install reset --experimental --apply`, dispatched
through a new `factory-reset` action on the existing `chairlift-helper`
— it takes no argument, since a factory reset has exactly one target, the
image already booted.

Both are gated by `maintenance_page`'s `reset_group`, which ships
`enabled: false` in `config.yml` (the same default as
`maintenance_cleanup_group`), and both require an `AdwAlertDialog`
confirmation with a destructive-styled response before anything runs — the
HIG's rule that destructive dialogs are reserved for genuinely non-undoable
actions. The confirmation text lives in `pageview.PowerwashConfirmation` and
`pageview.FactoryResetConfirmation`, table-tested to assert the Factory Reset
body names `--experimental` explicitly: `bootc`'s own reset path is not
stabilized upstream, and hiding that behind friendlier wording would be
exactly the kind of detail a confirmation dialog exists to surface.

Both reset controls release their in-flight gate when their buttons become
sensitive again, so a canceled PolicyKit prompt or dry-run preview can be
retried without restarting the application.

### Automatic background updates

`internal/autoupdate` classifies the state of `uupd.timer`, the unit
Universal Blue images ship for unattended updates. It is read-only; the
privileged writes are `auto-updates-enable` / `auto-updates-disable` on
`chairlift-helper`.

The package exists because ChairLift presents this as **one switch** where
bluefinctl presents a strategy enum, a schedule picker, per-layer switches,
and a focus mode. Collapsing several systemd states into a binary control is
only safe if the mapping is explicit, which is what `Classify` is:

- `is-enabled` returning `""` or `not-found` — the unit is not installed, so
  `StateUnavailable`, and the switch is not shown at all.
- `masked` or `masked-runtime` — `StateOff`, whatever `is-active` says. A
  masked timer cannot run. This is also how bluefinctl's "manual" strategy
  and its "focus mode" are both represented on disk, and neither is
  distinguishable to a user who only wants to know whether the machine
  updates itself.
- `enabled`/`enabled-runtime` with an active timer — `StateOn`.
- `enabled` with an inactive or failed timer — `StateOff`. An enabled but dead
  timer updates nothing, and calling it on is a claim the user disproves only
  by never receiving an update.
- anything else (`disabled`, `static`, `indirect`) — `StateOff`.

Turning the switch on unmasks before enabling, because "off" has those two
on-disk representations and a machine ever set to manual would otherwise
refuse to turn back on. Turning it off masks rather than merely disabling, so
a `systemctl preset` run during a package upgrade cannot quietly re-enable
what the user turned off. The unit name is fixed in the helper: a
caller-supplied unit would let an authenticated user enable or mask anything
on the machine.

### The release-channel table

`internal/imageinfo` owns the mapping from a running image and tag to the tag
its stable or testing counterpart is published under. The mapping is keyed on
the **registry path**, not on the tag alone, because the same tag word means
different things across images. Verified against GHCR by manifest request on
2026-08-17:

| Image | Stable streams | Testing streams |
| --- | --- | --- |
| `ghcr.io/ublue-os/bluefin` | `latest`, `stable`, `stable-daily`, `lts`, `lts-hwe` | `lts-testing`, `lts-hwe-testing` |
| `ghcr.io/projectbluefin/bluefin-lts` | `lts`, `stable` | `testing` |
| `ghcr.io/projectbluefin/dakota` | `latest`, `stable` | `testing` |

Bluefin has no GTS stream, and ChairLift offers none: `gts` classifies as an
unknown channel and has no driver variants.

Two consequences follow, both of which a tag-only mapping gets wrong:

- `ghcr.io/ublue-os/bluefin:testing` does not exist. A Bluefin Stable host on
  `latest` or `stable` has **no testing counterpart**, and the
  Testing Channel switch is correctly rendered insensitive there. Only the
  `lts` and `lts-hwe` streams on that image are switchable.
- `ghcr.io/projectbluefin/bluefin-lts:lts-testing` does not exist either; that
  image's testing stream is the bare `testing` tag.

bluefinctl's `bctl toggle-testing` uses a tag-only map that targets both of
those nonexistent references. ChairLift does not reproduce it.

An image outside the table resolves to no channel and no switch, rather than
to a guessed tag suffix. Other images — TunaOS, a downstream rebuild, a
private registry — are added by shipping a `channels.yml`, not by editing Go:

| Path | Owner |
| --- | --- |
| `/etc/chairlift/channels.yml` | administrator |
| `/usr/share/chairlift/channels.yml` | image maintainer |

Those two paths, in that order, are the only ones consulted. Unlike
`internal/config`'s search order they deliberately exclude the working
directory: the privileged helper resolves its `bootc switch` target through
this same table, so a user-writable table would let a local user redirect an
authenticated system switch. The GUI calls `imageinfo.LoadSystemTable()` at
startup so it can fail closed when a table-dependent control cannot resolve a
target. After validating argv, the helper loads that same table for
`channel-switch`, `driver-switch`, `pin`, and `unpin`; a malformed override
rejects those image-targeting operations but leaves unrelated fixed privileged
commands available. A file that fails validation is rejected whole — a half-applied
mapping is exactly the situation that produces a wrong switch target — and the
file must contain exactly one YAML document, so content after a `---` boundary
is rejected rather than silently ignored: the helper must never resolve a
mapping the GUI did not read. The file configures release channels under
`images:` and graphics-driver variants under `drivers:`. Driver entries map a
base image registry path to supported driver flavours (`standard` required,
`nvidia`, `nvidia-open`) and their published streams.
`channels.example.yml` documents both formats and is installed to
`/usr/share/doc/chairlift/`; no live table is ever installed or archived.

Separately, `polkitd` reads application policies from the fixed directory
`/usr/share/polkit-1/actions` — not `$XDG_DATA_DIRS`, not any
`$PREFIX`-derived path — so the Makefile's `install`/`uninstall` targets
require `PREFIX=/usr` (the default since issue #59) for a source install's
polkit assets to land somewhere polkit actually looks. These constraints are
system facts, not values ChairLift decides; the Makefile and
`internal/updex.HelperPath` exist to conform to them.

**Privileged integration delivery:** ChairLift is distributed only through
Homebrew. The cask installs GoReleaser's release archive,
`chairlift_<version>_linux_<arch>.tar.gz`, in user scope; there are no deb,
rpm, or apk packages. Because a user-scoped cask cannot place root-owned
files, the same archive carries the privileged pieces:
`chairlift-updex-helper`, `chairlift-helper`, and the bootc, updex, and
ublue PolicyKit policies in `data/`, together with `config.yml`,
`channels.example.yml`, the desktop entry, the wrapper, and every GSettings
schema XML. An OS image that wants the privileged features installs the
helpers at `/usr/bin/chairlift-updex-helper` and
`/usr/bin/chairlift-helper`, and the policies at
`/usr/share/polkit-1/actions/io.projectbluefin.chairlift.bootc.policy`,
`/usr/share/polkit-1/actions/io.projectbluefin.chairlift.updex.policy`, and
`/usr/share/polkit-1/actions/io.projectbluefin.chairlift.ublue.policy`, from
that archive. Those are the paths the helper constants and the policies'
`org.freedesktop.policykit.exec.path` annotations name, so the image must use
exactly them. `make install` places the same files at the same paths for a
source install, puts the schemas in `/usr/share/glib-2.0/schemas/`, and
compiles the schema cache on a direct install.
The accepted/rejected argv surface is exercised by the installed-helper E2E
tests; historical one-off verification does not replace that contract.

The cask and the image ship on separate schedules, so an image can be older
than the GUI running on it or carry no helper at all. The views therefore
offer a helper-backed control only when the image provides it:
`ublue.Status.Commands`, filled by `Detect`, is the set of helper commands for
which `/usr/bin/chairlift-helper` is installed and an installed PolicyKit
action names that path as `exec.path` and the command as `exec.argv1`.
`Status.Supports` gates helper-backed mutations. Developer options remain
discoverable but insensitive when their actions are missing; channel switching
also remains inert when no counterpart or installed action exists. Other
controls are shown only with the support their builders require. Restart
feedback can ask the person to restart manually when the helper is absent.
Reading the policies is a plain file read, needs no privilege, and is correct
for every image already shipped. A dry run never invokes the helper, so it
offers every command; that keeps the preview, the screenshot walkthrough, and
the AT-SPI suite rendering the full surface.

The archive does **not** ship `bootc-update-stage`. That
operation is distro policy, so an image that enables `bootc_updates_group`
must provide a trusted implementation at the existing fixed
`/usr/libexec/bootc-update-stage` path. Keeping the path fixed preserves the
PolicyKit executable boundary; making it a user-writable config value would
allow the GUI configuration to redirect a root execution. The page already
gates the group on its script-availability check
(`bootc.StageScriptAvailable`), so an absent distro helper hides the
operation.

### Maintenance action execution

Configurable maintenance scripts (from `config.yml` `actions` entries) are executed via `runMaintenanceAction()` in `internal/views/maintenance_page.go`. The pattern:

1. `decision := actionmsg.MaintenanceScript(dryrun.Enabled(), title)` is computed once, before the goroutine, from the single process-wide dry-run flag (see "Dry-run mode" above)
2. The button is disabled and labelled "Running…".
3. A worker constructs one `pageview.MaintenanceCommand` for both journal and
   execution. When `decision.Execute` is true, `maintenanceexec.Run` applies a
   five-minute bound; `sudo: true` uses the validated trusted-config pkexec
   route. Preview journals suppression and logs intent without constructing a
   process. This existing trusted script boundary is separate from fixed helper
   operations and must never be made available to untrusted fallback config.
4. On completion, the GTK thread restores the control and reports the preview
   or actual result.

### Keyboard shortcuts

`internal/navigation` is the single puregotk-free authority for sidebar page
order, titles, icons, advertised shortcuts, registered accelerators, and the
complete page-selection transition. Its canonical inventory is one table of
routes, each carrying a `Kind` and a page-qualified `Refs` list that names the
configuration namespaces whose groups build it. `navigation.VisibleItems`
filters that inventory using `Config.IsGroupEnabled`, always retains Help, and
assigns compacted Alt+number keys. `internal/window` uses the result for its
sidebar, stack, actions, initial selection, and shortcuts dialog. After
construction, `internal/app` registers `navigation.Bindings(window.NavigationItems())`,
so the registered and advertised keys use the exact same visible inventory.

The inventory holds two kinds of route. A **primary** is a sidebar
destination: it has a row, an Alt+number, and a shortcuts-dialog entry, and its
`Refs` are the groups that make it worth showing. A **detail** is a focused
screen reached from the primary that owns it — `Parent` names that primary, no
`Refs` entry of a detail is inferred from its display name, and it acquires
neither a row nor an accelerator: `Shortcuts` and `Bindings` skip it
structurally, so a detail can never be advertised or registered by accident.
Powerwash is the live detail. It is a content-stack child of Maintenance, whose
row stays selected while it is shown, and it draws on two configuration
namespaces at once — `bootc_updates_group` on `updates_page` for its rollback,
pin, and return-to-stream controls and `reset_group` on `maintenance_page` for its
Powerwash and Factory Reset controls — which
is why a route's refs are `{Page, Group}` pairs rather than one page field per
route. `navigation.VisibleRoutes` returns the visible primaries followed by the
details whose own refs are enabled and whose ancestor is itself visible; a
detail whose ancestor has no row has nowhere to return to, so `Resolve` falls
back rather than entering it.

The accelerators are:

- `Ctrl+Q` → quit
- `Ctrl+?` → show shortcuts dialog
- `Alt+1` through `Alt+N` → navigate to the first through Nth visible page in
  canonical order, with omitted pages leaving no gaps
- `F1` → navigate to Help (the same `win.navigate-help` action as Help's
  current compacted Alt+number binding)

Mouse row activation, keyboard navigation actions, and the Powerwash detail's
entry row and Back button all call `Window.navigateToPage`. That method calls
`navigation.Resolve` against `Window.navRoutes` — `navigation.VisibleRoutes`,
the visible primaries plus the details they offer, computed once beside the
sidebar's `navItems` — with the window's built pages as the construction seam
(the Powerwash ToolbarView is registered there like any primary), and enters a
route by applying all of its state changes: select the compacted sidebar row,
set the stack's visible child, update the content-page title, set
`NavigationSplitView.show-content` true so a collapsed layout reveals the
destination, and record the transition's `Back` primary in `Window.backRoute`.
A detail resolves to its ancestor's row index with its own title, its own
child name, and the ancestor its Back control returns to, so Powerwash keeps the
Maintenance row selected while it is shown; `navigateBack` resolves that
recorded primary through the same path and does nothing while a primary is
shown. `Resolve` rejects only a name the canonical inventory does not declare.
A detail the caller did not offer resolves to its ancestor and, failing that,
to Help; a known primary the caller cannot enter — disabled by configuration,
floored out by capability, or never built — has no ancestor and resolves
straight to Help (chairlift#343), so a deep link to a hidden page still opens
the window somewhere. No fallback can reveal a screen or fire a control the
user cannot see, because a `Transition` carries only the state a window
applies. `internal/navigation` tests every functional page with all of its
groups disabled, each builder-backed group individually enabled, the Help-only
fallback, compacted indices/accelerators, the hidden-primary and hidden-detail
fallbacks, unknown-name rejection, the complete advertised-to-registered
shortcut inventory, the F1 Help binding, and static app/window wiring —
including that the Powerwash callbacks resolve through `navigateToPage` rather
than setting the stack child or title directly. No `_test.go` is added to the
puregotk-importing `internal/window` or `internal/app` packages.

The sidebar selection belongs to `navigateToPage` alone. `GtkListBox` selects
whichever row receives keyboard focus, so Tab and the arrow keys would move the
highlight onto a page the content does not show without ever emitting
`row-activated`. `navigateToPage` records the resolved index in
`Window.shownRow`, and `buildSidebar` connects one `row-selected` handler, once
at build time, that re-selects that row whenever the selection lands anywhere
else; the focus ring stays where the user moved it, and Return (or a click)
still navigates through `row-activated`.

Note: `GtkShortcutsWindow` is not available in puregotk, so a custom `adw.Window` with `adw.PreferencesGroup` rows is used for the shortcuts dialog.

### URL opening

Help page links are opened via `xdg-open` using `exec.Command`. The process is started asynchronously and its exit is waited on in a goroutine to avoid zombie processes.

## Configuration

Decision records: [ADR-0003](../adr/0003-two-tier-config-with-fail-closed-semantics.md)
(config search order and fail-closed semantics),
[ADR-0004](../adr/0004-configuration-error-diagnostic-vocabulary.md)
(the `CONFIGURATION ERROR` diagnostic vocabulary), and
[ADR-0005](../adr/0005-config-schema-reflected-from-canonical-struct.md)
(the reflected schema and overlay semantics).

### Config file search order

1. `/etc/chairlift/config.yml` — system-wide (highest priority)
2. `/usr/share/chairlift/config.yml` — maintainer defaults installed by
   source `make install`, or by an OS image from the release archive
3. `config.dev.yml` — source-checkout fallback, beside the executable when
   present, otherwise relative to the current working directory
4. `config.yml` — legacy development fallback, beside the executable when
   present, otherwise relative to the current working directory

Only a missing candidate advances the search. The first existing candidate is
authoritative; a read, parse, type, or schema error disables every feature
group and produces both a high-signal log entry and a persistent toast. If no
file is found, all features default to enabled except
`maintenance_cleanup_group` and `reset_group`, which default to disabled. See
[CONFIG.md](../../CONFIG.md) for the full reference.

Source installs and image maintainers own only the `/usr/share` candidate and
may replace it on upgrade. Neither writes `/etc/chairlift/config.yml`; that
higher-precedence path remains administrator-owned. The user-scoped Homebrew
cask does not install either system configuration candidate.
The repository root's `config.dev.yml` shadows `config.yml` only in development
fallback loading; source installs use `config.yml` as the trusted
`/usr/share/chairlift/config.yml` maintainer default.

### Config structure

```yaml
page_name:
  group_name:
    enabled: true/false
    # Optional per-group fields:
    app_id: "..." # External app to launch
    actions: # Custom scripts (updates/maintenance)
      - title: "..."
        script: "/path/to/script"
        sudo: true/false
    bundles_paths: [...] # Homebrew bundle directories
    website: "..." # Help page URLs
    issues: "..."
    chat: "..."
```

### Key config groups

The [configuration reference](../reference.md) describes the current schema.
The [destination matrix](destination-matrix.md#configuration-references-and-owners)
provides the complete group inventory with original namespaces, current
mounts and action owners. Derive additions from `config.SchemaGroups`; do not
infer keys from the sidebar. In particular,
`channel_group` and `bootc_status_group` belong to `updates_page`, while
routine cleanup uses `maintenance_freespace_group`. Legacy System-page input
is handled by the migration described above, not a current System namespace.

## Build and Release

- **Build**: `make build` builds three binaries: `build/chairlift` (main app), `build/chairlift-updex-helper` (privileged updex helper), and `build/chairlift-helper` (privileged Bluefin-family helper), all with `CGO_ENABLED=0`
- **CI mirror**: `make ci` runs every host-independent gate from `.github/workflows/test.yml` in fail-fast order — go.mod tidy check, `go vet`, gofmt check, `golangci-lint`, unit tests (`./internal/...` under `-run "^Test[^I]" -skip "Integration"`), the race detector, and the build. Its build step reproduces CI's `linux/amd64` + `linux/arm64` matrix into `build/ci-linux-<arch>/` before rebuilding natively, so a compile failure on the non-host architecture cannot pass locally. The mill's deep gate (`.mill.toml`) calls this target. The runtime-dependent E2E job is deliberately separate, and every E2E test that needs a display runs inside the digest-pinned Dakota `testing` image selected by `test/e2e/dakota-image.sh`, through `test/e2e/dakota.sh`, so the host supplies only podman and Go. Each GUI runs under `test/e2e/wayland_session.sh` in a private D-Bus session: headless Mutter (`--headless --no-x11 --virtual-monitor`) plus PipeWire and WirePlumber for Mutter's ScreenCast API, with `GDK_BACKEND=wayland`, `XDG_SESSION_TYPE=wayland`, and `WAYLAND_DISPLAY` set to the compositor's absolute socket; there is no X server anywhere in the harness, which `internal/installcheck`'s `TestE2EHarnessHasNoX11Dependency` enforces. `make e2e` builds all three binaries, executes the application's `--help` path, boots the dry-run GTK window under that session, polls all three readiness markers for at most 30 seconds, requires one second of post-readiness stability, then sends `SIGTERM` and waits for every surviving member of its private session to exit before Go removes the temporary `HOME` those workers write into, captures the screenshot walkthrough, stages the real `make install` layout under a temporary `DESTDIR`, and executes the staged helper binaries' accepted and rejected argv. Its Go test package lives at `test/e2e`, imports no puregotk package, and is enforced by that explicit target rather than the `./internal/...` unit-test filter. The readiness markers are a log-line contract — decision record [ADR-0008](../adr/0008-e2e-readiness-is-a-log-marker-contract.md). `make e2e-atspi` runs the behave AT-SPI suite (`test/e2e/features/`, gated by `TestATSPIBehaveSuite` in `test/e2e/atspi_behave_test.go` and run by `test/e2e/run_atspi.sh`) the same way; `make e2e` skips that test because it alone takes most of the E2E time, and both run on Dakota because what the tree announces depends on the GTK/Libadwaita release (Ubuntu's Libadwaita 1.5 publishes preference groups differently from what Bluefin ships). The suite follows projectbluefin/testsuite's behave + dogtail shape against headless Mutter instead of a GNOME Shell VM; keyboard input is `dogtail.rawinput` through dogtail 2.1's `MutterInputBackend` (Mutter's RemoteDesktop API), installed by `features/lib/wayland_remote.py`, which also captures failure screenshots through Mutter's ScreenCast API. `features/environment.py` launches a fresh `--dry-run` ChairLift per scenario with its own HOME, XDG_RUNTIME_DIR, configuration fixture (`@config.<name>`, default `everything`, written as `config.dev.yml` beside a staged copy of the binary) and `$CHAIRLIFT_ACTION_JOURNAL`, so a step can assert which privileged command a click would have run without running it. Readiness still comes from the three log markers (ADR-0008); the accessibility bridge is enabled only inside the run's own private D-Bus session, the one place the suite departs from the walkthrough's deliberately a11y-free environment. The page and shortcut inventories arrive from `internal/navigation` as `CHAIRLIFT_NAVIGATION`/`CHAIRLIFT_SHORTCUTS`, so the suite cannot drift from them. Shared steps live in `features/steps/common.py`, tree helpers in `features/lib/chairlift_atspi.py`, prelaunch stubs in `features/fixtures/stubs*.py`; `TestATSPIFeaturesHaveNoUndefinedSteps` catches an undefined or ambiguous step without a display. `dakota.sh` builds the venv from `test/e2e/requirements-atspi.txt` (pinned to testsuite's behave) with the image's interpreter, sets `CHAIRLIFT_REQUIRE_ATSPI=1`, which turns a missing stack from a skip into a failure, and masks `/usr/share/chairlift`. In the test workflow the suite runs as a separate `atspi` job split into parallel tag-expression shards, which `internal/installcheck`'s `TestATSPIShardsCoverEveryFeatureOnce` holds to the feature files (every feature in exactly one shard), and each shard uploads `atspi-results-<shard>` (JUnit, logs, and accessibility-tree dumps plus screenshots for failed scenarios); the release and nightly workflows run the suite whole.
- **Dev build**: `make dev` builds with `CGO_ENABLED=1` and `-race` flag for race detection
- **Version**: Set via ldflags by goreleaser (`buildVersion`)
- **Distribution**: Homebrew only — the cask installs the release archive; GoReleaser builds no deb, rpm, or apk packages
- **Calendar versioning**: `make bump` tags the stable version printed by `scripts/next-version.sh` — `vYY.MM.N`, where N starts at 1 each calendar month and advances only over stable tags (`v26.10.1`, `v26.10.2`, …). Alpha/prerelease arguments are no longer supported; historical prerelease tags are retained but do not advance N. GoReleaser publishes these as full releases (`prerelease: false`). The zero-padded month is deliberate, which is why the release build injects `{{ .Tag }}`. `internal/installcheck.TestNextVersionStableMonthlySequence` exercises the real script in temporary repositories through its `NEXT_VERSION_SLOT` test override.
- **CI**: GitHub Actions workflows for test and release (`.github/workflows/`);
  the release workflow (`.github/workflows/release.yml`, job `goreleaser`) runs
  GoReleaser OSS with `GITHUB_TOKEN` to publish the tagged commit's artifacts
  straight to GitHub Releases. There is no separate snapshot workflow; the
  `snapshot:` block in `.goreleaser.yaml` only sets the version template for
  local `goreleaser release --snapshot` builds and is not used by any workflow.
  Third-party `uses:` references require full 40-character commit SHAs with
  readable version comments. Repository-local actions are exempt;
  first-party `projectbluefin/actions` uses the managed `v1` contract, with a
  narrowly scoped read-only, secret-free policy-preview candidate exception.
  `internal/installcheck.TestWorkflowActionsUseImmutableCommitSHAs` and the
  preview-authority gate enforce those distinct boundaries.
- **Release**: GoReleaser config at `.goreleaser.yaml` (GoReleaser OSS, run in
  `.github/workflows/release.yml` with `GITHUB_TOKEN` and `id-token: write`).
  Releases generate Syft SBOMs (`sboms:`) for published archives and sign
  archives and `checksums.txt` keyless using Sigstore bundles (`.sigstore.json`)
  via cosign. The repository URL is a literal in `release.footer`'s
  "Full Changelog" line — there is no `metadata.homepage` to template from,
  since OSS has no `metadata:` block — and that literal is the single source
  of truth for it; a static test guards it — see the "Install-path consistency
  (`internal/installcheck`)" section of
  [package-managers.md](./package-managers.md#install-path-consistency-internalinstallcheck).
  The `snapshot:` block in `.goreleaser.yaml` configures local
  `goreleaser release --snapshot` builds and needs no workflow.
  The tag workflow installs the same pinned golangci-lint version as test.yml
  before running `make ci`, avoiding a newer linter rejecting a previously
  validated commit only at release time.
- **Other targets**: `make fmt` (gofmt), `make lint` (golangci-lint), `make install`/`make uninstall` (system install including polkit policies, icons, and wrapper script; default `PREFIX=/usr`, the only prefix that matches where polkit reads policy files and the fixed pkexec exec-path annotations for both helper binaries — see "Privileged operations" above), `make build-linux-amd64`/`make build-linux-arm64` (cross-compilation)

### Runtime dependencies

- GTK 4 and libadwaita 1 (shared libraries loaded at runtime by puregotk)
- Homebrew (optional)
- Flatpak (optional)
- `bootc` + `/usr/libexec/bootc-update-stage` (both optional; UI gated on `bootc.IsBootcBootedCached()`, i.e. a booted deployment read from composefs state or `bootc status` — not on any sentinel file)
- Updex features configured on the system (optional; read via Go library, writes via `chairlift-updex-helper`)
- `/usr/share/ublue-os/image-info.json` (optional; absence floors out Developer Mode, Gaming Mode and channel/graphics controls)
- Fixed helper binaries and matching installed PolicyKit actions (optional; required for the corresponding privileged mutations)
- `bootc`, `usermod`/`gpasswd`, and systemd tools used by the fixed helper actions
- Homebrew and llmman for local Agent Mode; systemd user services and bounded HTTP health for observed readiness
- Podman and a systemd user manager for printer quadlets; current image administration locks still forbid new enables
- Flatpak with a system-scope Flathub remote for selected gaming components and optional Pulp installation, both installed system-wide under Flatpak's own PolicyKit
- GSettings schema XMLs, native dconf module/settings tools, icon-cache tool and desktop-specific assets for appearance/preferences

### Key external Go dependencies

| Module                           | Purpose                                                                                     |
| -------------------------------- | ------------------------------------------------------------------------------------------- |
| `codeberg.org/puregotk/puregotk` | GTK4/Adwaita bindings (no CGO)                                                              |
| `github.com/frostyard/snowkit`   | GObject registration, main-thread dispatch                                                  |
| `github.com/frostyard/updex`     | Updex Go library for feature reads and helper binary (currently pinned to v1.5.0 in go.mod) |
| `gopkg.in/yaml.v3`               | YAML config parsing                                                                         |
| `golang.org/x/image`             | WebP decoding and image scaling for avatar transcoding                                      |
| `github.com/frostyard/std` | Updex progress reporting |
| `github.com/leonelquinteros/gotext` | Translatable update presentation strings |

There is no separate Go client library dependency for bootc: status/stage types (`Status`, `Deployment`, `ProgressEvent`, etc.) are defined locally in `internal/bootc`, parsed directly from `bootc status --format json` and the stage script's line output.

### Headless and desktop gates

Decidable UI logic lives in pure-Go leaves under `internal/`; packages importing
puregotk cannot host ordinary headless test binaries. CI selects
`^Test[^I]` and skips `Integration`, so ordinary internal test names must avoid
both reserved shapes. `make e2e`, `make e2e-atspi` and `make screenshots` use
the one Dakota harness, `test/e2e/dakota.sh`, and private headless Mutter
Wayland/D-Bus sessions. Never run them against the developer's live runtime,
display or portal services. The helpers remain untagged; only the GUI gets
the centralized `chairlift_e2e` read-only display overrides. Screenshots and
the walkthrough are referentially gated documentation, not pixel snapshots
regenerated on every push. See [GTK headless testing](../skills/gtk-headless-testing/SKILL.md).

## Subsystem Details

- [Package Manager Wrappers](./package-managers.md) — Homebrew (including tap trust), Flatpak, bootc, and Updex wrapper details

### Agent Mode and Ask Bluefin (`internal/agentmode`, `internal/aistack`)

`internal/agentmode` coordinates Agent Mode's client integration, readiness evaluation, and the `chairlift --ask-bluefin` dispatcher.

Goose Desktop (`ublue-os/tap/goose-linux`) is the Agent Mode desktop GUI. `agentmode.Launch` writes ChairLift's own Goose profile (above), then runs
`llmman launch goose-desktop --model bluefin-active` (`troubleshoot.Command`) with the profile's `GOOSE_PATH_ROOT` and `XDG_CONFIG_HOME` and Homebrew on `PATH`, so llmman resolves the alias at launch and passes the endpoint through the invocation environment.

Readiness (`agentmode.Evaluate`), in the order the Goose row reports it:
- Goose Desktop is published for this architecture (x86_64); otherwise `StateUnsupported`.
- `linux-mcp-server` and `goose-desktop` resolve to absolute paths (`$PATH`, or beside `homebrew.ExecutablePath()`); otherwise `StatePackagesMissing`, the one state the row resolves itself with **Set Up**.
- The llmman daemon is healthy (`aistack.Healthy`) and `llmman` resolves; otherwise `StateDaemonUnavailable`.
- An active model is selected (`aistack.ReadActiveModel`); otherwise `StateModelUnavailable`.

Nothing on disk is verified: the profile is ChairLift's and is written at launch.

Once set up, Ask Bluefin just opens Goose. With no session running, `agentmode.Launch` writes the profile and starts Goose through llmman. With one running — the profile's Chromium `SingletonLock` names a live process on this host and was written during this boot (a lock left by a crash before a reboot may name a reused pid) — it starts `goose-desktop` again in the same profile (`troubleshoot.ReopenCommand`), and Goose's single-instance lock hands that request to the running session, so no second Goose starts. Whether the window is raised is the compositor's call: no activation token is passed, and on GNOME a minimized window stayed minimized while the Shell showed a "Goose is ready" notification instead (lab run `chairlift-wayland-lane-n7fsg`). llmman is not involved there: it refuses a launch while the lock is held.

`chairlift --ask-bluefin` is the entry point for Bluefin's Custom Command Menu and desktop shortcut. Cold invocations and running-application remote invocations behave identically:
- When all readiness conditions are met, Goose Desktop is launched off the GTK main thread (the launch writes the profile) without presenting the Control Center window.
- When any prerequisite is missing, or the launch fails to start, Control Center opens to the Agents page and displays the reason as a toast. A later asynchronous Goose exit is logged, not rerouted through the window.

The Troubleshooting group also offers a **Show Ask Bluefin in menu** preference. It manages the distro-owned Ask Bluefin entry in GNOME Shell's Custom Command Menu (`org.gnome.shell.extensions.custom-command-list`) via `internal/devmenu`, which recognizes the entry by its label and one of three commands: the web link, `chairlift --ask-bluefin`, or `/home/linuxbrew/.linuxbrew/bin/chairlift-wrapper --ask-bluefin` as Bluefin's distro layer ships it (projectbluefin/common#1396). ChairLift changes only the entry's visibility, never its command. When hidden, it writes a user-layer override (`visible=false`); when shown, it resets the key in the user layer to reveal the distro default without pinning it into user state.

## Explicit setup flow

Setup never opens automatically. `--setup`, its `--first-run` alias, and the
menu action start `internal/firstrun.Pages` over the existing visible navigation
inventory in Features → Apps → Agents → Livery order. The window hides its
sidebar and shows one footer with Back, Next, Finish, and Dismiss. Each step
uses the actual page controls and their existing gates; navigation goes through
`Window.navigateToPage`. There is no separate assistant dialog or settings
adapter. Missing pages are skipped; an empty inventory opens no flow.

Back and Next mutate no settings. Finish records completion and version;
intentional dismissal records a skip without demoting prior completion. Dry-run
logs disposition writes instead of persisting them. The `0-setup.png` capture
shows the Features start of this explicit flow, not a welcome screen.
