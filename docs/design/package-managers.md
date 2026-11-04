# Package Manager Wrappers

Living design document for provider execution, update coordination, and
user-scoped service management. [Overview](overview.md) describes composition;
[AGENTS.md](../../AGENTS.md) holds repository safety rules. Source links below
are the current owners, not implementation instructions copied from old plans.

## Ownership and availability

| Owner | Responsibility | Privilege |
| --- | --- | --- |
| [`internal/homebrew`](../../internal/homebrew/homebrew.go) | Homebrew inventory, typed installs, bundles, updates and tap trust | Invoking account |
| [`internal/flatpak`](../../internal/flatpak/flatpak.go) | Application/runtime inventories and explicit user/system operations | Flatpak's own authorization; no ChairLift pkexec wrapper |
| [`internal/bootc`](../../internal/bootc/bootc.go) | Unprivileged deployment reads/checks and fixed stage adapter | Only staging uses pkexec |
| [`internal/updex`](../../internal/updex/updex.go) | Go-library feature reads and fixed helper writes | Writes use pkexec |
| [`internal/ublue`](../../internal/ublue/ublue.go) | Image/account detection and narrow system mutations | Writes use pkexec |
| [`internal/updateflow`](../../internal/updateflow/model.go) | Immutable update snapshots, inventory, badge and aggregate decisions | Executes nothing itself |
| [`internal/updateproviders`](../../internal/updateproviders/) | Adapters and typed cleanup over the existing integrations | Adds no privileged route |
| [`internal/aistack`](../../internal/aistack/aistack.go) | Local llmman runtime, selected-model alias and owned user unit | Invoking account |
| [`internal/printerapp`](../../internal/printerapp/printerapp.go) | Rootless printer quadlets, administration gate and observed diagnostics | Invoking account |
| [`internal/agentmode`](../../internal/agentmode/readiness.go) | Goose launch readiness, profile-isolated launch, one-session guard and Ask Bluefin dispatch | Invoking account |
| [`internal/troubleshoot`](../../internal/troubleshoot/troubleshoot.go) | Goose/Linux MCP installation and ChairLift's own Goose profile | Invoking account |
| [`internal/devtools`](../../internal/devtools/devtools.go) | WSL backends, system Docker status/enable (rootless cleanup, user-scope Docker CLI formulae), native VS Code setup and canonical package provenance | WSL/editor/CLI work is user-scoped; KVM and system Docker use the existing fixed helper |
| [`internal/dockerbrew`](../../internal/dockerbrew/) | Verified Homebrew-bottle Docker daemon runtime: signed index, bottle download and extraction, ELF patching, manifest, managed units | Pure library; its writes run only inside the fixed `docker-enable`/`docker-update` helper commands |
| [`internal/devsetup`](../../internal/devsetup/setup.go) | Ordered developer setup and retry outcomes over injected tasks | Adds no privileged route; initial Developer Mode promotion uses the existing ublue helper |
| [`internal/compass`](../../internal/compass/compass.go) | Compass launcher state and system install/removal from the image's FlatPark remote | Flatpak's own authorization; adds no remote and no pkexec route |
| [`internal/valhalla`](../../internal/valhalla/valhalla.go) | Livery More Dinosaurs installation, PNG artwork and observed manual-source readiness | User-scope Homebrew/files/settings and system Flatpak under Flatpak's own authorization; adds no remote or pkexec route |

Mutation wrappers read the single process-wide `dryrun.Enabled()` flag from
[`internal/dryrun`](../../internal/dryrun/dryrun.go). There are no per-wrapper
preview flags. A suppressed mutation may return nil, so nil alone must never
make a view remove a row, change an inventory, or claim installation.

[`internal/capability`](../../internal/capability/capability.go) owns the
non-blocking tool/asset floor used by navigation and view builders. It may
omit a group before its widgets exist. A runtime query is not a capability:
Homebrew/Flatpak executable checks, updex feature availability, booted-deployment
reads and service readiness run in workers. `IsInstalledCached` caches the
availability verdict for Homebrew, Flatpak and updex; it does not cache package
inventories or executable paths. Every widget update returns through
`sgtk.RunOnMainThread`, with nil guards for independently disabled groups.

## Unified updates

The five `updateflow.SourceID` values map to these production adapters and
configuration namespaces in [`internal/window/window.go`](../../internal/window/window.go):

| Source | Constructor | Policy group | Apply operation |
| --- | --- | --- | --- |
| `applications` | `NewFlatpak` | `updates_page.flatpak_updates_group` | Update the user/system scopes represented by pending items |
| `developer-tools` | `NewHomebrew` | `updates_page.brew_updates_group` | `homebrew.Update(ctx)` followed by `homebrew.Upgrade(ctx, "")`, then `ublue.UpdateDocker(ctx)` when the `docker-runtime` item is pending |
| `system-components` | `NewSystemComponents` | `features_page.features_group` | `updex.UpdateFeatures(ctx)` |
| `printer-drivers` | `NewPrinterApps` | `features_page.printers_group` | `printerapp.Refresh(ctx, app)` once per selected app |
| `operating-system` | `NewOperatingSystem` | `updates_page.bootc_updates_group` | `bootc.StageUpdate(ctx, events)` |

`Provider` supplies `ID()`, `Available()`,
`Check(context.Context) (CheckResult, error)` and
`Apply(context.Context, []Item, func(Progress)) (ApplyResult, error)`.
`Item.ID` is execution identity, separate from `Name`; `Scope` preserves the
Flatpak installation or bootc transport. `CheckResult` carries pending items,
restart state, and warnings naming parts a partial check could not cover.
`ApplyResult` carries `Changed`, `Preview` and `RestartRequired`; after a
partial error, `PendingItems` and `Warnings` let the coordinator retry only
the failed items and show their names.

[`Coordinator`](../../internal/updateflow/coordinator.go) checks enabled
sources concurrently and applies pending sources serially in provider order:
Flatpak, Homebrew, updex, printer drivers, bootc. `Policy.Configured` is administrator policy;
`Policy.Supported` is the capability floor. Provider availability and user
preferences remain separate from both. Unavailable, administrator-disabled,
user-disabled, check failure and apply failure must not collapse into one
boolean or one subtitle. `Available` evaluation is part of the worker check,
not GTK construction. A source that discovers runtime unavailability wraps
`updateflow.ErrUnavailable`; the OS adapter uses this when its stage helper
exists but the host is not bootc-booted.

The coordinator owns `Snapshot.TotalUpdates`, source inventories and restart
state. Generations reject stale publications. A failed check preserves the
previous items and restart observation; a preview leaves inventory unchanged,
and the shell answers a preview Update All with `actionmsg.UpdateAllPreview`
instead of the completion notification.
An apply with `Changed=false` and no preview preserves pending items and sets
a retryable apply failure, not completion. Retry applies only failed sources;
maintenance can be retried separately. `PhaseRestartRequired` has
`ActionNone`: restart is the Operating System row's separately confirmed
**Restart Now** action, not a page-level automatic step.

Adapter completion is source-specific:

- Flatpak checks both scopes and joins read failures. Apply runs only requested
  scopes, then re-lists them. Completion requires that the specific applied
  refs have disappeared, not that the whole scope is empty: newly published
  updates do not invalidate completed work. An exit-0 no-op retaining an
  applied ref reports `Changed=false`. Dry-run skips reconciliation and reports
  a preview.
- Homebrew checks `ListOutdated`. Apply runs metadata refresh **and** package
  upgrade under the caller's cancellation context. Its live success reports
  Changed; the subsequent shared check refreshes inventory. It does not
  re-list inside the bulk Apply method. When `dockerbrew.ReadManifest` finds
  a Homebrew-bottle Docker runtime and the installed helper supports
  `docker-update`, Check also compares Homebrew's unsigned per-formula API
  (`dockerbrew.Client.Latest`) with the manifest and adds a `docker-runtime`
  item named "Docker engine" only when `dockerbrew.Outdated` reports a
  difference. Apply then calls `ublue.UpdateDocker` after the brew upgrade;
  the root helper ignores that hint and re-verifies against the signed index
  ([ADR-0024](../adr/0024-rootful-docker-from-verified-homebrew-bottles.md)).
- Updex counts each affected feature once if any component has an update.
  Captured warnings make the unified check partial, not failed: the found
  items are kept, the warnings are logged, and `CheckResult.Warnings` says
  "some components" were not covered (#699). Successful live
  helper execution reports Changed; preview reports Preview instead.
- bootc performs a read-only check and reads status for restart state. Apply
  rejects unknown item scopes, stages through the fixed helper, then re-reads
  status. Restart is based on an actual staged deployment, not exit 0; the
  OS-shipped stager is idempotent and can succeed without staging anything;
  on composefs, where its `bootc upgrade` fails on a current system,
  `bootc.StageUpdate` asks the registry first and skips it.

[`updateproviders/item.go`](../../internal/updateproviders/item.go) owns
individual application/tool updates. `UpdateItem` validates Flatpak ID/scope
or Homebrew name, runs the existing wrapper and rechecks that requested item
before returning Changed. `RefreshDeveloperTools` refreshes metadata only.
Both are admitted by the shell's same `beginMutation` path as Update All and
dedicated OS staging. Failures, unchanged outcomes and previews keep controls
retryable and known inventory intact; verified live individual success starts
a shared check. Do not add a second count or update-state owner in `UserHome`.

[`UpdateShell`](../../internal/views/update_shell.go) renders snapshots with
[`updatepresent`](../../internal/views/updatepresent/updatepresent.go). Its
progress bar displays a determinate fraction when `snapshot.Determinate` is
true (`Presentation.ProgressDeterminate`), or pulses from one reusable GLib
timer during checking or unquantified operations, independent of provider
output; idle, determinate progress and disposal remove the timer. Its visible
toast overlay announces each phase's one status line, which sits under the
primary action.
`Presentation.ShowStatus` hides an empty header.
`updatepresent.ShowSourceRow` decides which sources get a row: one that
configuration disables for the whole machine, or that this host cannot back,
gets none, and a source group left empty is hidden with them (#675). The
remaining rows are exactly the sources the page checked, so its single status
line needs no qualifier. Preferences still lists every source with its lock
reason, so nothing the application does not update becomes undiscoverable.
`buildUpdatesPage` appends the rest of the Updates page to the shell's own
preferences page (`UpdateShell.PreferencesPage`), beneath the source rows;
automatic updates, system version, unverified sources and
Advanced channel/driver controls are not a second unreachable page.
The completion notification is the one long-running update notification, not
a notification per toggle.

## Homebrew (`internal/homebrew/homebrew.go`)

### Executable resolution

[`executable.go`](../../internal/homebrew/executable.go) is the one resolution
owner. `ExecutablePath` calls `ResolveExecutable`: `$PATH` wins, otherwise a
regular file at `/home/linuxbrew/.linuxbrew/bin/brew` is the fallback,
otherwise the answer is empty. The capability floor uses the same resolver.
[`data/chairlift-wrapper.sh`](../../data/chairlift-wrapper.sh) evaluates
`shellenv` from that fallback; direct binary launches must still find the same
installation without relying on the wrapper.

`IsInstalled()` resolves the executable and runs `--version` with a five-second
bound. `IsInstalledCached()` memoizes that verdict with `sync.Once`. Execution
resolves per call; the absent case uses the bare name only to retain
`NotFoundError` classification. Do not independently resolve brew at a call site.

### Public operations

`Package` carries installed version, pinned/outdated flags and
`InstalledOnRequest`; the parser does not populate `Dependencies`.

| API | Command / result |
| --- | --- |
| `ListInstalledFormulae()` | `brew info --installed --json=v2 --formula` |
| `ListInstalledCasks()` | `brew info --installed --json=v2 --cask` |
| `ListOutdated()` | `brew outdated --json=v2`; formulae and casks, installed version strings |
| `Tap(name)` | `brew tap <name>` |
| `Install(name, isCask)` / `Uninstall(name, isCask)` | `install` / `uninstall`, with `--cask` only for casks |
| `Upgrade(ctx, name)` | `brew upgrade [<name>]`; empty name upgrades all |
| `Update(ctx)` | `brew update`; metadata refresh, not installed-package upgrade |
| `Pin(name)` / `Unpin(name)` | Formula pin/unpin |
| `BundleDump(path, force)` | `brew bundle dump [--file=...] [--force]` |
| `BundleInstall(path)` | `brew bundle install [--file=...]` |
| `BundleCheck(path)` | Two read-only checks returning `BundleStatus` |
| `Cleanup()` | `brew cleanup`; live successful mutation output is discarded |
| `AvailableBundles(paths)` | Filesystem discovery returning `[]Bundle` plus joined diagnostics |
| `BundleContents(path)` | Parses a Brewfile returning `[]BundleItem` (Name + Kind) for every installable entry, deduped by (Kind, Name), capped at 256 entries per file |
| `ListUntrustedTaps()` / `TrustPackages(tap)` | Receipt-backed discovery / per-user package trust |

[`homebrew.go`](../../internal/homebrew/homebrew.go) classifies mutation argv
through `isStateChanging` and `stateChangingCommands`: install, uninstall,
remove, upgrade, update, pin, unpin, bundle, cleanup, trust and tap. Bundle
`check` and `list` are read-only; bare bundle, install, dump and unknown bundle
subcommands remain mutations. That classification selects both timeout and
preview behavior: reads get 30 seconds and still run under dry-run; mutations
get 30 minutes and are skipped before constructing an `exec.Cmd`.
`Update(ctx)` and `Upgrade(ctx, name)` narrow the caller's context to that
mutation budget; most other operations own their timeout context.

### Apps callers and action state

[`applications_page.go`](../../internal/views/applications_page.go) has
two configuration groups: `brew_bundles_group` owns collections, shown first;
`brew_group` owns installed casks, explicitly requested formulae and the
Brewfile exporter, in that order. Both are enabled in shipped `config.yml`.
Apps has no Flatpak inventory, external catalog launcher, or package search;
the four retired Apps keys are accepted and stripped as legacy configuration
(see [overview](overview.md)).

Installed uninstall/pin/unpin controls share a per-row action gate, use
`actionmsg.Uninstall`/`actionmsg.Pin` for live versus preview wording, restore
on failure or dry-run, and refresh the installed inventory only after live
success. The installed loader clears and repopulates separate
`formulaeRows`/`caskRows` trackers under its refresh generation; a stale
generation's result is dropped and a failed read preserves the last known rows.
Row gates come from each list's `actionstate.RowGates`: while any row's
action is running (including an open confirmation dialog), a rebuild of that
list is deferred rather than replacing the controls that show the action, and
`settleHomebrewRows` runs the owed reload once every gate is released.
Export restores its gate and spinner after every outcome.
Homebrew installs started elsewhere — Goose Set Up, enabling Agent Mode,
Features' developer tools, and collection installs, including one that
stopped partway — call `homebrewInventoryChanged` on the main thread, which
reloads the installed lists and re-observes every collection's status.

### Process and diagnostic contract

`runBrewCommandCtx` is the dry-run dispatch gate and calls the injected
executable/context seam `runBrewCommandAt`. The child gets its own process
group; cancellation kills the group, including ordinary download/build
helpers, and `WaitDelay=5s` bounds inherited-output-pipe waits. Read commands
retain full parser output. Mutations retain a 64 KiB tail **per stream** for
failures and discard successful output.

Deadline and cancellation classify distinctly and unwrap to their context
sentinels. Missing executables return `*NotFoundError`. Nonzero exits return
`*Error`, `*UntrustedTapError`, or, for `uninstall` argv only, a
`*DependentsError` naming the installed packages Homebrew's stderr refusal
says still need the package (closed name grammar; Apps names them in its
error toast through `actionmsg.UninstallFailure`). For mutations, stdout and stderr tails are
joined stdout-first, logged, and distilled by
[`diagnostic.go`](../../internal/homebrew/diagnostic.go) to one bounded error
line for UI feedback. Bundle installers replay their real failure on stdout,
so preferring non-empty stderr would drop the cause.

Trust classification is an argv-gated exception: only a bundle-install
invocation may derive a trust classification/tap from replayed stdout. Other
commands require brew's stderr to corroborate it and take the tap from stderr.
Tap captures use a closed owner/repository character grammar. A formula's
forged stdout must not turn an unrelated install/upgrade failure into a
suggestion to trust its chosen source. Context failures take precedence over
trust classification.

### Configured Brew bundle discovery

[`bundles.go`](../../internal/homebrew/bundles.go) scans immediate regular
`*.Brewfile` entries, resolving configured directories to cleaned absolute
paths. Missing directories are normal; empty/unreadable/non-directory paths
and unreadable candidates return joined diagnostics alongside usable results.
Duplicate absolute paths are emitted once; same-named files in distinct
directories remain separate, ordered by name then path.

A `Bundle` carries `Name`, `Description`, `Path` and `ItemCount`. Description
joins the leading comment block after any leading blank lines, or falls back
to a humanized filename. The scanner limits each description line to 64 KiB;
this is not a total comment-block byte cap. Item count is a best-effort scan
of recognized Brewfile entry prefixes, not execution or full Ruby parsing.
[`bundleview.Describe`](../../internal/views/bundleview/bundleview.go) owns
curated user-facing titles, usable-comment filtering and count wording;
`Present(count, warning)` owns empty/partial/error presentation without
showing raw paths in the UI.

`BundleCheck` reports Installed on first-pass success. Only an ordinary exit 1
permits `check --no-upgrade`: success then means Update Available, exit 1 means
Not Installed. Malformed manifests, cancellation, timeouts and other failures
remain Indeterminate with an error; dry-run still performs these reads. Read-only
`bundle check`/`bundle list` run with `HOMEBREW_NO_AUTO_UPDATE=1`, because brew
otherwise runs `brew update --auto-update` before every `bundle` subcommand.

`BundleContents(path)` parses a Brewfile into `[]BundleItem` (one entry per
recognised directive: `brew`, `cask`, `flatpak`, `mas`, `vscode`). Entries
are returned in source order, deduplicated by (Kind, Name), and capped at
256 items per file so a runaway Brewfile cannot dominate the UI.
`countBundleItems` runs the same `parseBundleItemLine` against the same
input and applies the same dedup, so a Brewfile's count and its expanded
list always agree (including when one entry repeats or is malformed).
[`bundleview.PresentContents`](../../internal/views/bundleview/bundleview.go)
turns those items into the rows the Apps page renders inside each
collection's `AdwExpanderRow`, with a kind label (Command-line tool, App,
Flatpak, App Store, VS Code extension) per row. A Brewfile with no
recognised entries shows a placeholder row instead; one longer than the
256-entry cap lists the first 256 followed by a "Showing N of M entries."
row. If the file cannot be read when the list is built, the rows read before
the failure are kept with no overflow claim. Tap directives are intentionally excluded because
they are not installable; a malformed entry (no quotes, or a quoted token
that does not close on the same line) is skipped silently so one bad
line cannot derail the rest of the list.

`applications_page.brew_bundles_group` is independent of `brew_group`.
Built-in `bundles_paths` contains `/usr/share/ublue-os/homebrew`,
`/usr/share/chairlift/bundles`, and `/etc/chairlift/bundles`; shipped
[`config.yml`](../../config.yml) replaces that list with the first directory
only. Group visibility is floored on Homebrew before discovery.
Each collection is rendered as an `AdwExpanderRow` with a checkbox prefix
whose title and subtitle are the curated name and description
`bundleview.Describe` produces, and whose expanded child rows are the package
list. Collections are selected with checkboxes (curated sets default checked,
while Full Desktop Suite defaults unchecked). An "Install" header action installs
all checked, uninstalled Apps collections in one step. Monospace Fonts
(`fonts-dev`) is offered only inside the initially collapsed Developer Mode
expander on Features, using an independent selection and explicit Install action
backed by the same collection installer and action gate. Its visibility still
requires `applications_page.brew_bundles_group` and Homebrew; installing it does
not grant developer access, and enabling Developer Mode does not install it.
Monospace Fonts defaults to selected in ordinary and explicit wizard sessions.
The wizard uses the same controls and preserves manual deselection across
Next/Back navigation; it adds no automatic installation path for this collection.
Bluefin CLI is not a discoverable `cli` collection: its Bash-only lifecycle belongs
to `internal/bluefincli` and one Features switch. Setup defaults that choice to
selected unless explicitly disabled and applies it on Next from Features.

Each row's installed state is observed: `refreshBundleStatuses`
(`internal/views/bundle_install.go`) runs `BundleCheck` for every collection
off the GTK thread once the rows are built, again after every live collection
install and after a live Homebrew uninstall. When installed, a collection's
checkbox is checked and made insensitive, and its subtitle indicates installed
status. Failed observations preserve known state; previews do not refresh it.

### Livery Wallpapers setup

Livery's `wallpapers_group` is directly below Profile Picture and before the
icon groups. The group is floored on Homebrew and offers a default-checked
More Dinosaurs checkbox and an explicit Install button. Showing the group
only observes readiness; it never installs, converts or configures anything.
The `wallpaper-slideshow` and `artwork` Brewfile collections are hidden from Apps,
so image definitions that still name Damask cannot install or advertise it.
[`internal/valhalla`](../../internal/valhalla/valhalla.go) owns this group's
fixed payload and complete setup through `Install(ctx, report)` and `Installed(ctx)`:

- Install `io.github.tunix.valhalla` system-wide from the existing `flatpark`
  remote, under Flatpak's own authorization. Never add a remote or a privileged
  ChairLift helper route.
- Install the user-scope `ublue-os/tap/bluefin-wallpapers-extra` Homebrew cask.
  The expanded row discloses Valhalla and `bluefin-wallpapers-extra`, regardless
  of what the old image Brewfile lists.
  Both metadata and Caskroom lookups use that fully qualified name: a bare
  cask token is ambiguous on a host with multiple taps publishing it.
- Convert extra SVG, JPEG XL, and KDE's AVIF artwork to PNG now, rather than expecting
  Valhalla to read unsupported formats. Converted files live in
  `$XDG_DATA_HOME/chairlift/wallpaper-slideshow/png`, falling back to
  `~/.local/share/chairlift/wallpaper-slideshow/png`; cask artwork is not rewritten.
  SVG conversion uses `rsvg-convert`; JPEG XL uses `djxl`; AVIF uses `avifdec`.
  Setup reuses installed tools, including Homebrew's resolved prefix, and installs
  `librsvg`, `jpeg-xl`, or `libavif` only when the corresponding tool is missing.
  Generated names include the installed cask version, so a pack upgrade makes
  setup retryable until its PNGs are regenerated. A successful batch removes
  obsolete generated PNGs, while failed conversion preserves the previous batch
  and unrelated files in the output directory are left alone.
  Native SVG/JPEG XL support is requested in
  [Valhalla #99](https://github.com/tunix/valhalla/issues/99).
- Grant Valhalla read-only sandbox access to the generated source, add it to
  `manual-folders` while retaining other folders, and set `active-source` to
  `manual`. Configuration runs through the installed FlatPark app's schema at
  `/app/extra/valhalla` (`GSETTINGS_SCHEMA_DIR` inside `flatpak run --system`),
  not the host's schema registry or the upstream-built Flatpak's schema layout.

The GTK worker calls `Installed` with a 30-second context each time the group is
shown and `Install` with a 30-minute context only after an explicit click. A
`RefreshGate` rejects observations overtaken by a newer read or installation;
the section's action gate resets after every outcome. Complete usable artwork
and configuration, not Flatpak presence alone, determine installed status.
An absent `ublue-os/tap` is ordinary not-set-up state, observed with the read-only
tap inventory before querying the fully qualified cask; an inventory failure is
not treated as proof the tap is absent.
A failed setup stays selectable for retry, and its row and error toast include
the cause. Live completion also refreshes Apps' Homebrew inventory, including
after partial failure. Dry-run previews setup but executes no install,
conversion, sandbox override, or settings write and preserves observed readiness.
Clicking Install with the checkbox unchecked skips setup entirely. Setup does
not launch Valhalla or directly apply a desktop wallpaper.

Wallpaper setup reuses Features' `setupActivity` checklist and `installProgress`
pulsing timer, with its own fixed rows directly below the wallpaper controls.
The provider reports five real phase boundaries: Install Valhalla, Install extra
wallpapers, Prepare wallpaper images, Configure slideshow, and Verify setup.
Each row becomes Working before its operation, then Complete only on success;
failure or cancellation stops the dependent phases, leaving earlier outcomes
visible. Preview marks the stages Preview without executing them. No percentage
or per-package completion is inferred from opaque installer output. The busy
button reads Installing…; completion, failure, and disposal stop the timer.
Source registry names stay in this technical contract, not the user-facing
wallpaper descriptions.

### Tap trust

[`trust.go`](../../internal/homebrew/trust.go) combines
`brew tap-info --installed --json` with on-disk installation receipts:
`<prefix>/Cellar/<formula>/<version>/INSTALL_RECEIPT.json` and
`<prefix>/Caskroom/<token>/.metadata/INSTALL_RECEIPT.json`, reading `source.tap`.
Packages already trusted on their own are dropped using `brew trust --json=v1`
(the only `brew trust` argv classified read-only), because per-package trust
leaves the tap itself untrusted in `tap-info`; Homebrew without that flag lists
every installed package. Only untrusted taps with installed, not yet trusted
packages are actionable. A missing `trusted`
field on older Homebrew is treated as trusted, not false.
Every `tap-info` call runs with `HOMEBREW_NO_GITHUB_API=1`: its JSON also
reports whether each tap is private, which brew asks the GitHub API, and on
Linux that credential probe intermittently kills the command with
`Error: Broken pipe`. Trust is local, so nothing ChairLift reads is lost.

`TrustPackages` runs `brew trust --formula ...` and/or `--cask ...` with
qualified installed package names. `TrustFormula`/`TrustCask` are the
per-package path that issue #537 asks for: each installed package inside a
tap's row carries a Trust button, and Homebrew 6's `brew trust --formula <name>`
adds only that one package to `~/.homebrew/trust.json`. The tap-level
**Trust Tap** button runs `TrustPackages`, trusting every *installed* package
from the tap at once; it does not run `brew trust --tap`. Rows are tracked by
package kind as well as name, because a tap may ship a formula and a cask with
the same name. A failed discovery row is a direct child of the group, never
inside a collapsed expander. Trust is per-user, never pkexec.
[`updates_page.go`](../../internal/views/updates_page.go) confirms source trust
before running it. Failed discovery retains a visible Retry action. Live
success removes only the trusted source row (and, for per-package trust, the
trusted package's row inside the parent expander) and starts the coordinator's
shared check. A per-package success also drops the package from the tap's
remaining `UntrustedTap` (`Without`), so the expander's count, the
**Trust Tap** dialog (`pageview.TapTrustConfirmation`), and a later tap-wide
trust cover only what is left. Preview restores Trust without removing a row. The
configuration-gated **Manage Source Trust** group may not exist, so
`trustmsg.UpgradeMessage(name, trustGroupAvailable)` must not direct the user
to a hidden control. Bundle failures use `trustmsg.BundleMessage` instead.

## Flatpak (`internal/flatpak/flatpak.go`)

Flatpak retains installation scope and ref kind. `KindApplication` and
`KindRuntime` select mutually exclusive `--app`/`--runtime` listings; the kind
is stamped from the query, not inferred from optional parsed columns.
`Application` carries name, ID, version, branch, installation and kind.
`UpdateInfo` carries application ID, display name, new version and installation.

| API | Scope / command |
| --- | --- |
| `ListUserApplications()` / `ListSystemApplications()` | `flatpak list --user/--system --app --columns=name,application,version,branch` |
| `ListUserRuntimes()` / `ListSystemRuntimes()` | Same query with `--runtime` |
| `ListUpdates(ctx, user)` | `remotes --columns=name,options`, then `remote-ls --updates --app --columns=name,application,version <remote>` per enabled remote, in the selected scope |
| `Install(appID, user)` / `InstallFromRemote(appID, remote, user)` | `install -y` in explicit scope, optionally naming a remote |
| `Uninstall(appID, user)` | `uninstall -y` in explicit scope |
| `AppRuntime(appID)` | `info --show-runtime <appID>` in any installation |
| `RemoteAppRuntime(remote, appID, user)` | `remote-info --user/--system --app --show-runtime <remote> <appID>` |
| `RefBranch(ref)` | Pure: last segment of `ID/ARCH/BRANCH` or `KIND/ID/ARCH/BRANCH` |
| `Update(ctx, appID, user)` | `update -y` in explicit scope; empty ID updates that scope |
| `UninstallUnused()` | Independently `uninstall --unused -y --user` and `--system`; errors joined |
| `RemoveAllUser()` | `uninstall --user --all -y`; Powerwash step, not routine cleanup |

The update inventory deliberately lists applications only. A runtime extension
such as Gaming's MangoHud requires the separate runtime inventory; it must
not be classified absent from an application-only query.

`ListUpdates` queries each enabled remote separately because a remote-less
`remote-ls --updates` fails the whole installation when any one remote is
unreachable: a remote left behind by an uninstalled application (issue #471)
hid every other remote's updates and failed the Updates check, although
`flatpak update` itself still succeeded. A remote whose query fails is checked
against `list --app --columns=origin`: when no installed application in that
scope comes from it, it is logged and ignored; otherwise it is reported as a
`*flatpak.RemoteError` naming that remote and installation. The origin check
matches the inventory's `--app` filter on purpose: a remote that still serves
only runtimes — the usual leftover when an application is uninstalled without
`--unused` — can hide no update this inventory would list, so it must not
fail the check either. A cancelled
caller context or a missing executable still aborts the whole query. The
healthy remotes' updates are returned alongside that error. The Applications
provider treats an error made only of `*flatpak.RemoteError`s as a partial
check: it offers those updates and names each unreadable remote ("the
<remote> remote") in `CheckResult.Warnings`, logging the cause (#699). Any
other member — the origin listing, the remote listing, cancellation — means
the scope was not checked, and the check fails, so the coordinator keeps the
last known inventory. The post-apply reconciliation still fails on a remote
error: an applied item does not record its origin remote, so a list that
skipped a remote cannot prove that remote's applied refs cleared. The
post-apply reconciliation and the single-item verification read through the
same function, so an ignored remote cannot fail them either.

Install, uninstall, remove and update are mutations: 30-minute timeout,
preview-skipped dispatch, successful output discarded and bounded diagnostic
tails. Other commands get 30 seconds and full parser output. `ListUpdates`
and `Update` accept caller cancellation and apply their read/mutation budgets
(`ListUpdates` per query, so one slow remote cannot starve the others);
other exported operations own their contexts. Availability runs `--version`
under five seconds, with a cached variant for providers.

The runner kills its unprivileged process group and bounds pipe draining with
five-second WaitDelay, as Homebrew does. Errors distinguish deadline,
cancellation, missing executable and nonzero exit. Unlike Homebrew's combined
mutation diagnostic, Flatpak prefers stderr and falls back to the stdout tail
only when stderr is empty. Neither runner reports its own cancellation merely
as `signal: killed`.

Apps has no Flatpak inventory or removal. Flatpak remains in Updates, Gaming,
and maintenance/Powerwash operations.

## Shared OS stage executor (`internal/stageexec`)

[`stageexec.Stage(ctx, events, pkexecName, scriptPath)`](../../internal/stageexec/stageexec.go)
is the dispatch boundary used by `bootc.StageUpdate`. It journals every live
or suppressed invocation, then selects `Run` or `DryRun` from the global flag.
`ScriptAvailable(path)` owns the local script-presence probe. The injected
program name is a test seam; production passes `pkexec.Command`.

`ProgressEvent{Type, Message}`, `EventMessage` and `EventComplete` have one
owner here; bootc exposes aliases. `Run` merges stdout/stderr into trimmed,
non-empty lines and emits one completion only after successful exit. Nonzero
exit returns the exit code and last line; scanner/start/wait errors return
errors, not an extra error event. Deadline/cancellation classify separately;
missing bare-name or absolute-path executables return `NotFoundError`.
Every return closes the event channel. `DryRun` emits preview/completion
without spawning a command, subject to context cancellation.

Cancellation kills and reaps only the direct child. Root descendants behind
pkexec cannot be controlled by the unprivileged process-group cancellation
used for Homebrew and Flatpak. Do not promise that canceling the UI stopped
privileged work already running.

The dedicated staging view uses `stageProgressSink` and
[`progresslog`](../../internal/views/progresslog/progresslog.go): `Append`
opens at most one pending main-thread drain callback per burst, pending lines
are capped, and `rowset.Tracker.TrimTo` caps the rendered window at
`progresslog.DefaultLimit` (200). `StagingLogSubtitle(shown, total)` discloses
omitted lines. Both callback and widget caps are required; do not copy a
per-event GTK callback example into a stream consumer. Command-output rows
keep markup disabled. The Details expander is built hidden and revealed by
the first flushed line: a stage helper may print nothing to the pipe (on
Dakota, `bootc upgrade` logged its progress to the journal), and an
expander that opens to nothing reads as lost output.

## bootc (`internal/bootc/`)

### Unprivileged reads

`GetStatus(ctx)` and `CheckUpdate(ctx)` never ask for a password. On composefs
hosts, [`composefs.go`](../../internal/bootc/composefs.go) reads world-readable
state through the `hostRoot` filesystem seam:

| Observation | Source |
| --- | --- |
| Booted deployment | `composefs=<id>` on `/proc/cmdline` |
| Staged deployment | `depl_id` in `/run/composefs/staged-deployment` |
| Reference and manifest digest | `/sysroot/state/deploy/<id>/<id>.origin` |
| Booted version | `IMAGE_VERSION`, falling back to `VERSION_ID`, in `/usr/lib/os-release` |
| Rollback identity | Newest non-booted/non-staged origin by modification time |

A broken booted/staged read is an error, not evidence the system is current.
Deployment IDs are validated before building paths. A missing/unreadable
rollback origin is not turned into a fabricated rollback version. Only the
not-composefs classification falls back to unprivileged
`bootc status --format json` / `bootc upgrade --check`. The fallback check
parser rejects unexpected or incomplete output instead of guessing.

Composefs checks resolve the booted image tag with `registrytags.Client.Tag`
and compare the current platform's child digest, when an index was returned,
against **both** booted and staged digests. No difference means no pending
update. A missing platform digest or an untagged/digest reference is an error.
When present, the created annotation supplies the available build date.
Staged/rollback version labels unavailable to the account remain absent rather
than introducing a privileged read. The rollback's origin mtime only selects
it: it is the deploy time, not a release date, so the rollback carries no
timestamp and the Roll Back row reads "Return to the previous version the
next time you restart" — a row shown only when the deployment exists never
says nothing is kept (#521).

`Status.Booted()` inspects the booted entry; exit 0 alone is not the gate.
`IsBootcBootedCached()` reads once under five seconds. A sentinel such as
`/run/ostree-booted` cannot substitute for the status model on composefs.
`DefaultContext()` is 30 minutes; reads honor the caller's supplied context.
`bootc.Error` and `NotFoundError` alias stageexec's types.

### Fixed-path staging

[`StageUpdate(ctx, progressCh)`](../../internal/bootc/stage.go) binds
`stageexec.Stage` to `pkexec.Command` and
`StageScriptPath=/usr/libexec/bootc-update-stage`. ChairLift ships no stager
implementation and does not add a `bootc upgrade` helper command. The
OS-provided script owns the podman-pull / containers-storage switch workaround;
ChairLift owns only dispatch and presentation.

Dedicated staging and unified OS Apply use this same path. The dedicated
handler re-reads `GetStatus` before reporting staged/current and refreshes
changelog references on a successful read. A failed verification reports the
failure rather than declaring current. Under preview, `actionmsg.SystemStage`
reports no change; the persistent subtitle may still show an already-staged
live deployment. A live successful stage requests the coordinator's check.

## Updex (`internal/updex/updex.go`)

Reads use `github.com/frostyard/updex/updex`, whose version is owned by
[`go.mod`](../../go.mod), not a CLI. `Feature`, `FeatureCheck` and `CheckResult`
are aliases of its types. The listing client is a singleton; checks create a
client carrying a per-call warning reporter through the same `newClient`
construction owner.

| API | Contract |
| --- | --- |
| `IsInstalled()` / `IsInstalledCached()` | Feature listing under three seconds; true only with no error and at least one definition |
| `ListFeatures(ctx)` | Read feature definitions through the library |
| `CheckFeatures(ctx)` | Return `([]FeatureCheck, []string, error)`, retaining component warnings even on failure |
| `EnableFeature(ctx, name)` / `DisableFeature(ctx, name)` | Fixed helper with `enable-feature <name>` / `disable-feature <name>` |
| `UpdateFeatures(ctx)` | Fixed helper with `update` |
| `DefaultContext()` | Five-minute context for callers |

These context-taking reads/writes do not secretly replace caller cancellation
with a new background context. Writes bind `helperexec.Run` to
`HelperPath=/usr/bin/chairlift-updex-helper`; the wrapper discards captured
helper output and returns the execution error.

[`internal/updexhelper`](../../internal/updexhelper/updexhelper.go) validates
exact argv and builds options for the thin
[`cmd/chairlift-updex-helper`](../../cmd/chairlift-updex-helper/main.go)
dispatcher. Enable, disable and update all accept a final `--dry-run`, which
sets the library options' DryRun field. The GUI's preview normally suppresses
pkexec entirely; helper-level preview is defense in depth and the installed
accepted-command test surface. Successful helper results are JSON on stdout.
A failed `update` writes one stderr line from
`updexhelper.UpdateFailureDetail`: updex's generic error followed by every
failed `feature/component: reason`, because updex records those only in its
results and the helper's client has no warning reporter.

[`featurestatus`](../../internal/views/featurestatus/featurestatus.go) owns
the Features-page aggregation: **any** component update makes its feature
outdated; group counts are features, not components. Empty results are an
incomplete check, not current. A shared non-empty version is displayed only
when every component agrees. Failed/incomplete group descriptions never claim
all features are current. The unified adapter separately keeps the found
items beside captured warnings and reports the check as partial. Feature availability and read errors must remain
distinct: no definitions hides the optional group; a failed listing stays
visible with an explanation.

## Bluefin-family helper and privilege boundary

[`internal/ublue`](../../internal/ublue/ublue.go) detects descriptor, channel,
driver, account groups and installed helper-action support without privilege.
`Status.Supports` is per-control support, not a new navigation capability.
Missing helper actions leave affected Developer controls discoverable but
insensitive. Writes bind `helperexec.Run` to
`HelperPath=/usr/bin/chairlift-helper`.

[`ubluehelper.SupportedCommands()`](../../internal/ubluehelper/ubluehelper.go)
is the complete fifteen-command surface:
`channel-switch`, `dx-enable`, `dx-disable`, `restart`, `rollback`,
`auto-updates-enable`, `auto-updates-disable`, `driver-switch`,
`factory-reset`, `pin`, `unpin`, `kvm-enable`, `docker-enable`,
`docker-disable`, `docker-update`. Every command has exactly one action in
[`data/io.projectbluefin.chairlift.ublue.policy`](../../data/io.projectbluefin.chairlift.ublue.policy).
The helper dispatcher rejects unhandled commands; parser acceptance alone is
not evidence a command executes.

`ubluehelper.Timeout` is the helper's per-command budget: 30 minutes for the
image-pulling `channel-switch`, `driver-switch`, `pin`, and `unpin`, and for
the bottle-downloading `docker-enable` and `docker-update` (the same
as OS staging's `bootc.DefaultTimeout`), 10 minutes for everything else. The
GUI's `ublue.ImageSwitchContext` and `ublue.DefaultContext` add a five-minute
`AuthenticationAllowance`, because the caller's clock also runs during the
PolicyKit prompt and the helper must be the one to report its outcome.

Only validated channel, driver or day words cross this boundary. The helper
resolves concrete image references from its own descriptor and the
[`internal/imageinfo`](../../internal/imageinfo/) channel/driver tables,
loaded from root-owned system paths. It resolves the account from `PKEXEC_UID`.
No image reference, username, unit name, reset/rollback target or scheduling
value arrives in argv. Channel/driver switches enforce container signature
policy. Rollback has one existing-deployment target; Factory Reset spells the
fixed `bootc install reset --experimental` argv in the helper (no `--apply`,
which reboots immediately; the reset applies at the next restart).
Reset and restart confirmations stay in the calling UI, with destructive
wording supplied by `pageview`.

[`internal/helperexec`](../../internal/helperexec/helperexec.go) owns live and
suppressed invocation journaling, stdout/stderr capture and shared error
taxonomy; ublue and updex alias its error types. `pkexec.Command` is the sole
owner of the escalation program name. `Run` bounds pipe draining with
five-second WaitDelay and kills only its direct child on cancellation. Its
cancellation message explicitly warns that privileged work may still run.
The child's own exit status wins when it exited normally, including a
successful exit whose descendant held output pipes open, or a real nonzero
exit racing cancellation. Do not misreport a completed privileged mutation
as retryable failure merely because output draining timed out.

### Action journal scope and evidence

[`helperexec.Run`](../../internal/helperexec/helperexec.go) is the journal
choke point reached by **both** `ublue.runHelper` and `updex.runHelper`. Every
invocation records intent before dispatch, independent of dry-run and the
`chairlift_e2e` build tag. GUI preview appends `--dry-run` to the recorded
`would_run` argv but starts no helper and emits no outcome. Live dispatch adds
a separate outcome record: `succeeded`, `refused`, `failed`, `timed-out` or
`cancelled`. Exit 126 and exit 127 without `Error accessing` classify as
authentication refusal; an inaccessible helper at exit 127 is failure. This
journal category is not the UI cancellation classifier: exit 127 with "Not
authorized" remains a Failed setup stage. Only exit 126 or "Request dismissed"
denotes a dismissed PolicyKit prompt and a Canceled stage.

`cmd/chairlift-helper` emits a JSON argv marker before each derived root
command. The executor strips those markers from human-readable output and
captures their command arrays as `executed` on the outcome. The updex helper
calls its library directly and currently emits no derived-argv markers.
`executed` is self-reported output, not authenticated proof: privileged child
output inherits the same pipes and can imitate a marker, and a marker precedes
the command's execution. `succeeded` means helper exit 0, not independent
verification of every requested effect; the views/providers retain their
post-action observations.

[`journal`](../../internal/journal/journal.go) writes best-effort JSONL only
when `CHAIRLIFT_ACTION_JOURNAL` is configured. Missing/unwritable sinks never
block a mutation; unset sinks take the cached atomic fast path. This is not a
universal process tracer: `stageexec.Stage` and configured maintenance scripts
record intent separately, without these helper outcome/derived-argv records.
Ordinary Homebrew, Flatpak and user-service commands do not flow through the
fixed-helper journal choke point.

### Dated-build registry reads and the helper's pin commands

[`internal/registrytags`](../../internal/registrytags/registrytags.go) is
read-only: `Client.Tags` follows validated same-host `Link: rel="next"`
pagination, and `Client.Tag` resolves a tag's digest, created time and platform
digests. Repository/tag inputs are validated before URL construction; the
response Content-Type determines manifest shape. A 404 returns
`ErrUnknownTag`; an absent creation annotation is not a transport failure.
`ParseBuild` recognizes real days ending a stream with `.` or `-`;
`Builds(tags, since)` keeps aliases and sorts newest day first, then tag.
Every request uses the injectable `Client.HTTP`; tests use loopback
registries. In the GUI, only composefs `bootc.CheckUpdate` reads the registry
(see above).

**The published-versions calendar is withdrawn (#522).** Powerwash used to
list the running stream's dated builds (Published versions, with a confirmed
per-day **Pin**) and offer **Return to stream** when booted on a dated tag.
The dated tags exist only for the deprecated `latest` stream, so Powerwash now
offers Roll Back and the reset group only. The view code, its in-process
`Catalog` cache, the `ublue.Pin`/`ublue.Unpin` wrappers and the shared
pin/unpin switch gate were removed; Roll Back keeps its own one-shot gate.

The privileged half stays, with no GUI caller. The helper validates the day
itself: `pin <YYYYMMDD> [--dry-run]` accepts a real day no later than today
UTC; `unpin [--dry-run]` accepts no target. In
[`ubluehelper.PinArgs`](../../internal/ubluehelper/pin.go), the booted tag
supplies the stream, which must pass `imageinfo.KnownStream`. Live pin tries
`<stream>-<day>` and only on `ErrUnknownTag` tries `<stream>.<day>`.
Unpin requires a dated booted tag and verifies the recovered stream tag.
Other registry errors refuse. The resolver's returned strings are discarded:
only a locally derived target reaches `bootc switch --enforce-container-sigpolicy`.
Both commands fail closed on a broken system channel table; preview derives
without a registry call and is not proof the target exists.

Rationale lives in [ADR-0011](../adr/0011-chairlift-owns-bluefin-family-rebasing.md),
[ADR-0013](../adr/0013-rollback-catalog-reads-the-registry-live.md) and
[ADR-0017](../adr/0017-pin-through-a-validated-day-word.md). Those records
contain historical implementation names; the current owners above determine
the executable paths and UI surface.

## Typed cleanup and configured scripts

[`updateproviders.Cleanup`](../../internal/updateproviders/maintenance.go)
owns routine cleanup: Homebrew old downloads/versions and unused Flatpak
supporting software in both scopes. `CleanupGroup=maintenance_freespace_group`
gates both **Free Up Space** and post-update maintenance. `Run` stops at the
first failure/cancellation; `RunSteps` attempts both independent steps and
returns typed cleaned/skipped/failed/cancelled outcomes. An absent provider is
skipped, not a success. Cleanup is not Powerwash and never absorbs an
administrator script or container removal.

The coordinator runs optional post-update maintenance only after live completed
source work with no failed/pending updates and with the user's
`MaintenanceAfterUpdates` preference enabled. While it runs, `Snapshot.Maintaining`
keeps the phase `PhaseUpdating` with no action, so the panel shows the cleanup
step and the progress bar instead of "Up to date · checked at …" before cleanup
(which may prompt for a password) has finished. Manual cleanup presents every
step through `cleanupview`; reclaimed bytes require two successful readings
and the minimum reportable difference. Preview never reports measured savings.
Cleanup wrapper calls have their own budgets, so the enclosing context is
checked between steps rather than replacing every underlying command context.

Configured tasks are a separate, default-disabled
`maintenance_cleanup_group`. Schema/provenance validation permits `sudo: true`
only from `/etc/chairlift/config.yml` or `/usr/share/chairlift/config.yml`,
with an absolute script path; development/relative configuration cannot opt
into inherited privileged actions. `pageview.MaintenanceCommand` returns the
validated direct or pkexec invocation. The view journals that exact argv and
uses `actionmsg.MaintenanceScript` to suppress execution under preview.
[`maintenanceexec.Run`](../../internal/maintenanceexec/maintenanceexec.go)
sets a process group and bounded wait; a root descendant is still outside the
unprivileged caller's signaling authority. This administrator-owned extension
surface is not permission to make the fixed provider helpers configurable.

## Developer workstation and gaming components

[`internal/devtools`](../../internal/devtools/devtools.go) owns WSL backends
and the GUI side of system Docker under `features_page.dx_group`, plus optional
native VS Code setup. [`internal/dockerbrew`](../../internal/dockerbrew/) owns
the daemon runtime the fixed helper installs.
[`internal/devsetup`](../../internal/devsetup/setup.go) owns the widget-free
ordered run and its stage outcomes. `PostStages` selects eligible post-promotion
work from a `PostOptions` snapshot; the view supplies execution callbacks.
`onDeveloperToggled` and the WSL/Docker handler share `developerGate`, covering
promotion, child setup, and configured post-enable work in one worker rather
than nesting switch handlers.
When Developer Mode is off, child enable first calls the existing
`ublue.SetDeveloperMode` fixed `dx-enable` helper. Authentication failure or
cancellation aborts the child. Only successful live promotion confirms the
parent; a later child or optional-install failure never revokes it. Child
disable never demotes the parent, and preview preserves all observed switches.
Retries retain successful optional stages rather than repeating installs.

The shared post-promotion path opens one developer documentation page and
installs the Dev Container CLI (`devcontainer`) when Homebrew is available.
Pulp, feed staging, and the developer app collection retain their opt-ins and
independent outcomes; editor selection/installation uses native Development Apps
rows in Developer Mode: `ublue-os` Homebrew Visual Studio Code, VSCodium, and
JetBrains Toolbox (no personal taps), system FlatPark Cursor and Antigravity,
and system Flathub Zed. Sublime Text is deliberately excluded for now by user
approval; the current catalog has six entries. Native VS Code configuration is
a separate optional Docker-enable step.
No OS image switch is involved. The run captures config on the
GTK main thread, does external work off-thread, and marshals widget changes
back through `sgtk.RunOnMainThread`. Failed or previewed work leaves the action
retryable and never invents a live installation or clears known inventories.

Gaming Mode leads Features. The **Setup activity** checklist below it has
bounded, reusable stage rows and a pulsing timer independent of provider output. Stage outcomes
distinguish complete, failed, canceled, preview, and awaiting-new-login work;
there is no estimated percentage. Awaiting login is not readiness or a generic
failure and does not offer Retry just to repeat the same permission grant.
Developer Mode, WSL Mode, Enable Docker, and Gaming Mode use native
expanders matching Apps collections. Header activation expands the package
disclosure; a separate suffix switch runs the action, with no
`SetActivatableWidget` binding to that switch. Developer Mode also contains
independent Monospace Fonts collection controls. WSL disclosure shares
`devtools.WSLPackage` with execution; Docker's `devtools.DockerPackages` lists
the daemon formulae as **Homebrew, installed for the whole computer** and the
CLI formulae as user-scope Homebrew formulae, not download progress.
Gaming uses `gaming.Components`. Developer Mode discloses
its Dev Container CLI and enabled
follow-ons, subject to the same capability/config gates as execution. An opaque
Homebrew bundle remains one operation: its static package disclosure is not a
live per-package completion stream. Signals and rows are built once, not per
refresh.

`dx_group.wsl_backend` defaults to **nsl**, with **Lima** as an administrator
option and an in-session backend chooser. The first observation retains an
existing Lima Ubuntu machine when no ChairLift nsl machine exists; it does not
persist the chooser selection or silently migrate data. nsl requires Linux amd64;
Lima supports Linux amd64 and arm64. Both require actual invoking-session
`/dev/kvm` access; nsl additionally requires the account's `kvm` group
membership in the account database (as nsl's own check reads it), even when the
device is writable. The fixed `kvm-enable` grant requires a new login before
setup continues only when `/dev/kvm` was not accessible; a membership-only grant
continues setup at once. nsl installs the
`frostyard/tap/nsl` cask and runs `nsl doctor`.
WSL Mode's nsl machine is `ubuntu`, or the `debian` machine an older ChairLift
created (`devtools.ParseNSLList` prefers `ubuntu`; other machine names are the
user's and are ignored). It creates an Ubuntu 26.04 machine (`ubuntu`) only when
neither exists — never a second machine beside `debian` — then starts the
managed machine and proves shell readiness with
`nsl run -m <machine> --cd / true`; the engine chooser says the built-in engine
runs Debian while that machine is the legacy one. Disable runs `nsl stop <machine>` on that machine only and keeps
data; `nsl shutdown` would stop every machine and nsl VM, the user's included
(#546), and the shared VM powers off by itself after its last machine. Lima installs
`lima`, adds its SSH include, creates/starts Ubuntu LTS with a writable home,
enables autostart and verifies `limactl shell ubuntu true`. Its disable removes
autostart and stops Ubuntu without deleting its disk. A listed/running VM
alone is not shell readiness.

**System Docker from verified Homebrew bottles.**
System Docker matches Bluefin DX: a rootful daemon behind `docker.socket`,
`docker` group membership, and a CLI with buildx and compose. Its packages
come from Homebrew, but root never executes, loads, or copies anything from the
user-owned `/home/linuxbrew/.linuxbrew` prefix
([ADR-0024](../adr/0024-rootful-docker-from-verified-homebrew-bottles.md)).

Enable runs in this order, after the Developer Mode prerequisite above:

1. **Daemon (fixed helper).** `devtools.SetDocker` calls `ublue.SetDocker`,
   that is `pkexec /usr/bin/chairlift-helper docker-enable`. No account,
   unit, formula, URL, or path crosses the boundary.
2. **Rootless cleanup (user scope), only after the helper succeeds.**
   v26.10.10 shipped rootless Docker. A user
   `~/.config/systemd/user/docker.service` whose `ExecStart` runs
   `dockerd-rootless.sh` is stopped with `systemctl --user disable --now
   docker.service`. A `docker context show` of `rootless` is switched back with
   `docker context use default`. Other user units and contexts are left alone.
   A dismissed prompt or failed install therefore leaves a working rootless
   daemon untouched; it uses its own socket, so the brief overlap is harmless.
3. **CLI (user scope).** With Homebrew available, `homebrew.Install` installs
   `dockerbrew.CLIFormulae` (`docker`, `docker-buildx`, `docker-compose`).
   `~/.docker/cli-plugins/docker-buildx` and `docker-compose` are linked to
   Homebrew's `lib/docker/cli-plugins` only when absent or already pointing
   into Homebrew; a user's own file is never replaced. This runs after the
   helper, so a CLI failure is reported while the daemon still works.

`docker-enable` has two branches:

- **Image units.** When `/usr/lib/systemd/system/docker.service` exists, as on
  Bluefin DX, the image's Docker wins. The helper removes only the units it
  wrote earlier (`dockerbrew.RemoveManagedUnits`) and reloads systemd if any
  were removed. It then grants the invoking account `docker` membership and
  runs `systemctl enable --now docker.socket docker.service`.
- **Homebrew bottles.** Otherwise `dockerbrew.Install` installs the daemon:
  1. It fetches `https://formulae.brew.sh/api/formula.jws.json` and verifies
     its PS512 JWS with the `homebrew-1` key compiled into the package.
  2. `SelectBottles` takes the fixed `dockerbrew.Formulae` set (`docker-engine`,
     `containerd`, `runc`, `tini`, `libpathrs`, `libseccomp`) for
     `x86_64_linux` or `arm64_linux`. The dependency closure must equal that set
     plus system `nftables`. Each URL must be the GHCR blob of its indexed
     SHA-256.
  3. `Client.Bottle` downloads each blob anonymously and rejects a SHA-256
     mismatch.
  4. `Extract` writes only mapped regular files: the daemon, proxy,
     containerd, its shim, `runc`, `tini-static` as `docker-init`, and the two
     libraries. It refuses traversal, absolute names, and devices, and never
     follows symlink members.
  5. `PatchELF` replaces the `@@HOMEBREW_PREFIX@@` interpreter with the system
     loader and every RPATH/RUNPATH with `$ORIGIN/../lib`. Static binaries are
     left alone.
  6. Under a lock, `Install` smoke-runs `runc --version` and
     `dockerd --version`. It writes `manifest.json`, renames the set to
     `/var/lib/chairlift/docker/<set-id>`, swaps the `current` link
     atomically, and prunes older sets (best-effort once the swap is done).
     Before the swap it writes a root-owned `.restart-pending` marker; while
     it exists, a rerun over the same set still reports a change, and the
     helper clears it only after `try-restart` succeeds. A reinstall of the
     same set moves the old directory aside for pruning, and a `current` whose
     target is missing reads as not installed. It refuses an install root or
     `current` that is a foreign symlink or not root-owned. Bottle downloads
     are capped at 1 GiB.

  The helper then copies an image-only `docker` group into `/etc/group` or
  creates it with `groupadd -f -r docker`. It writes `docker.service` and
  `docker.socket` to `/etc/systemd/system` (`dockerbrew.WriteUnits`); both
  `docker-enable` and `docker-update` run `dockerbrew.CheckUnits` before
  downloading, so an unmanaged unit is refused before any fetch. They
  are moby's upstream units, with `ExecStart` and `PATH` pointing at
  `current/bin`. Each begins with `# Managed by chairlift-helper`, and an
  existing file without that marker is never overwritten. Next it runs
  `systemctl daemon-reload`. When SELinux is enforcing it defines the
  equivalence rule `semanage fcontext -a -e /usr/libexec/docker
  /var/lib/chairlift/docker` if missing and runs `restorecon -RF
  /var/lib/chairlift/docker`, so container-selinux's runtime label applies;
  an enforcing host without both tools is refused before any download. When the runtime changed and the
  daemon on `/run/docker.sock` reports no containers running, it runs
  `systemctl try-restart docker.service`, which does nothing to a stopped
  service. If containers are running, or the query fails, it prints
  `docker runtime restart deferred: containers are running` instead; the
  `.restart-pending` marker stays, so the next enable or update retries the
  restart and a reboot starts the new runtime. Finally it adds the `PKEXEC_UID` account, enables `docker.socket`,
  and starts `docker.service`. It prints the manifest as
  `chairlift-helper: docker runtime <json>`.

`docker-update` prints `no Homebrew Docker runtime to update` and exits 0 when
image units exist or no manifest does. Otherwise it repeats the verified
install, rewrites the managed units, reloads systemd when either changed, and
when the bottle set changed relabels the new set under the same SELinux
condition before restarting an active daemon.
`docker-disable` runs `systemctl disable --now docker.socket docker.service`
and keeps the runtime, container data, and group membership. Every command's
`--dry-run` is offline: it prints the index it would fetch, the formula set,
the unit paths, and each `groupadd`, `usermod`, `restorecon`, and
`systemctl` line it would run. Both `dx-enable` and `docker-enable` can ask
for authentication in the same run. A refused parent prerequisite starts no
Docker work, and a later refusal never revokes access already granted.

`DockerStatus` reads `LoadState` and `ActiveState` for both system units without
escalation, then pings `/var/run/docker.sock`; only HTTP 200 `OK` is Ready.
`DOCKER_HOST` and `XDG_RUNTIME_DIR` do not redirect that probe. `not-found`
units mean Docker is not installed yet (`Available`, not `Installed`), and
without image units enabling also requires `docker-update` (`pageview.DockerCanEnable`),
so an older image helper stays insensitive (`pageview.DockerHelperOutdated`)
rather than failing after authentication. Masked or erroring units stay unavailable with an
image/administrator explanation, but a known running unit remains stoppable
with a supported disable action. A failed unit read or parse returns zero
state and the original error. Once both reads succeeded, a failed socket probe
keeps the observed `Active`/`Available`. The view preserves prior known state
on unreadable observations rather than inventing off. A socket permission
refusal sets `NeedsLogin` and wraps `ErrDockerNewLogin` only when the account
database (`user.LookupGroup` and the account's group IDs) lists the account in
`docker` but the session's `os.Getgroups` lacks it. That access needs a new
login and is not Ready. Otherwise the refusal sets `NoAccess`: the daemon runs,
but this account is not a member. The switch reads on whenever the daemon is
`Active` and not `NoAccess`, so an unresponsive daemon stays on and stoppable. For `NoAccess` it reads off with the subtitle "Docker is running,
but your account can't use it yet. Turn this on to get access." Turning it on
runs `docker-enable`. Live disable verifies that both units stopped, so socket
activation cannot silently restart the daemon. A dry-run disable ignores ping
errors once the units were read, makes no changes, and preserves switches.
The same `developerGate` covers the whole run. Widget observations are
rendered on the GTK main thread.

Docker enable orders system activation, readiness verification, then optional
native Microsoft VS Code setup and independent post-promotion opt-ins. Awaiting
login is an explicit verification outcome; optional editor failure is reported
separately and never reverses Docker/developer access. Retry for a failed optional
stage observes existing active Docker instead of replaying a privileged enable.
[`devtools.SetupVSCode`](../../internal/devtools/vscode.go) requires native `code`
but does not install an editor. It exclusively creates absent
`~/.config/Code/User/settings.json` with Bluefin DX defaults (custom title bar,
Cascadia Code font stack, and editor self-updates off). Existing settings,
including JSONC, symlinks, and files created concurrently, are left intact.
The extensions are `ms-vscode-remote.remote-containers`,
`ms-vscode-remote.remote-ssh`, and `ms-azuretools.vscode-containers`.

The upstream marker is `${SETUP_CHECKER_FILE:-$HOME/.local/share/ublue/setup_versioning.json}`
with `.version.user.vscode == "1"`. Upstream can write that marker before hook
completion, so ChairLift checks installed extension IDs before honoring it and
installs any missing IDs. It preserves unrelated marker fields and writes version
1 atomically only after successful setup; partial failures remain retryable.
Preview writes no settings or marker and starts no editor CLI. Missing native
`code` returns `devsetup.ErrSkipped`, so the step reads as skipped without an
error toast. Malformed marker data or an extension failure is an optional
setup failure, not a false Docker or Developer Mode failure.

`make e2e-docker` selects `TestIntegrationDockerSystem` in
[`docker_system_test.go`](../../test/e2e/docker_system_test.go). It runs the
pinned Dakota image unchanged, with no Docker binaries, units, or `docker`
group, so the real helper takes the Homebrew branch and downloads Homebrew's
signed index and bottles over the network. The fixture also plants a
v26.10.10-style rootless user unit. It uses private native managers in
rootless Podman's namespace and no host socket or bus. Its live assertions send
EOF to a real prerequisite authentication prompt, preserving the actual journal
outcome and exit code in the summary. Exit 127 with "Not authorized" proves
refusal/Failed, not live cancellation; Canceled is asserted only for exit 126
or "Request dismissed". The gate also covers ordered authentication, the
helper-reported argv, a root-owned runtime whose manifest names the six
formulae and whose ELF loader fields never reach the Homebrew prefix,
marker-bearing units, the retired rootless unit, awaiting login, a real `sg`
new-group session's HTTP 200 socket readiness from `current/bin/dockerd`, stop
preserving the runtime/data/groups and Developer Mode, and a no-op
`docker-update` (`changed=false`). The fixture has no Homebrew, so the CLI
step is not covered there. Dry-run AT-SPI scenarios and screenshots do not
establish live readiness.

[`internal/gaming`](../../internal/gaming/gaming.go) manages the gaming tools
stack (ProtonPlus, Protontricks, GOverlay, MangoHud). Enable installs missing
refs in the **system** scope (`Install(id, false)`, #503): Bluefin-family images
configure Flathub only as a system remote, and their policy is system-wide
Flatpaks. The `flatpak` CLI authorizes that through Flatpak's own PolicyKit
(`org.freedesktop.Flatpak.app-install`/`runtime-install`), not ChairLift's
`pkexec` boundary. Its inventory queries application/runtime kinds in both
scopes, so a per-user copy from an earlier release counts as installed and is
not duplicated. Disable removes each component from exactly the scopes it
is observed in (`Uninstall(id, true)` for a user copy, `Uninstall(id, false)`
for a system copy), and a ref with a copy left is a failure. A system copy the
image declares it ships (Flatpak `preinstall.d` or Bluefin's
`system-flatpaks.Brewfile`, read by `imageShipped`) is left in place and
returned as kept rather than removed. Per-item failures preserve partial
outcomes. The Features view serializes action/refresh with `gamingGate`;
failed refreshes preserve last known state. Neither gaming nor per-user
Homebrew trust adds a ChairLift privilege route.

## Agent Mode (`internal/aistack`)

Agent Mode runs llmman as a systemd **user service**, not a ChairLift-managed
container stack or Homebrew service. The Homebrew capability floors
`agents_page.agents_group`. The Agents page's Troubleshooting group
(`agents_page.troubleshooting_group`, also floored on Homebrew) offers the
Goose row — Set Up, then Launch — and Ask Bluefin menu visibility; runtime
enable does not itself install Goose. When llmman is missing, enable taps
`llmmanorg/tap` and trusts exactly `brew trust --formula
llmmanorg/tap/llmman` before the bundle when `brew tap-info --json` reports
the tap untrusted, because Homebrew refuses a formula from an untrusted tap;
Homebrew before 6 has no tap trust, so nothing is trusted there.
Rationale: [ADR-0015](../adr/0015-agent-mode-llmman.md).

[`aistack.go`](../../internal/aistack/aistack.go) owns three filesystem
artifacts and their removal policies:

| Artifact | Written | Removed |
| --- | --- | --- |
| Generated temporary installation Brewfile | Only when llmman does not resolve; installs `llmmanorg/tap/llmman` through Homebrew | Immediately after bundle execution |
| `~/.config/systemd/user/chairlift-llmman.service` | Enable and owned-unit reconciliation | Disable, only after stop is successful or proven inactive |
| `~/.config/environment.d/10-chairlift-llmman.conf` | Enable, containing only `OLLAMA_HOST=127.0.0.1:17434` | Disable together with the unit |

llmman owns engine choice and model storage; Homebrew owns the binary. Disable
keeps binaries/models. The paths use the user's config directory resolution,
not a hardcoded Homebrew prefix. `Executable()` uses an absolute llmman on
`$PATH`, then a regular file beside `homebrew.ExecutablePath()`.

`RenderUnit(exe)` rejects a non-plain/non-absolute executable path. The unit
serves loopback `127.0.0.1:17434` with `LLMMAN_SHELL=off`,
`LLMMAN_NOHISTORY=1` and empty `LLMMAN_PEERS=` overriding legacy aggregation.
It sets no wildcard CORS, OpenAI credential or session-wide OpenAI endpoint.
Only OLLAMA_HOST is best-effort published to future activation processes;
already-running applications are not claimed to have changed. Peer/offload
controls and their backend are absent; unrelated llmman configuration remains
untouched.

`Enable(ctx)` installs the missing runtime, runs `llmman serve --pull-only`
before writing management files, enables the unit, and uses the shared restart
boundary. Newly created files are rolled back on failure where safe; a
pre-existing management unit is preserved. `Disable(ctx)` uses
`disable --now`; if that fails, only an inactive/failed/unknown systemd result
permits removal. Otherwise it retains both files and the management handle.
Every mutation honors `dryrun.Enabled()`.

`Observe(capable)` supplies non-blocking executable/unit facts;
`Healthy(ctx)` requires HTTP 200 and a JSON-object response from `/llmman/node`
within two seconds. `Resolve(Facts)` distinguishes unavailable, unconfigured,
provisioning, ready, degraded and disabled. Unit presence or systemctl active
alone is not readiness. Failed toggles re-observe state rather than invert an
optimistic switch. The Active Model row stays visible but insensitive until
ready, with its unmet prerequisite explained. Model selection itself happens
in llmman's own models page (`http://127.0.0.1:17434/#/models`), which the
Agents page's Manage Models row opens (#568).

[`migration.go`](../../internal/aistack/migration.go) reconciles only an
existing owned service. Matching file bytes alone do not prove that the daemon
adopted local-only settings: `restartService` reloads, restarts, validates the
live systemd `InvocationID`, and only then stamps that ID as a unit comment.
A missing/mismatched stamp retries reconciliation; a verified current
invocation does not restart, and an absent unit stays disabled. Reconciliation
failure preserves the unstamped unit and best-effort stops the old daemon.
Live startup waits up to ten seconds for health after reconciliation; preview
performs one bounded probe and writes nothing.

The Active Model row lists models downloaded in llmman, queried live from
`/llmman/node`. `ConfigureActiveModel` sets `aliases.bluefin-active` via `llmman
config set` and restarts the daemon so the active model takes effect.
`ReadActiveModel` runs `llmman config get aliases.bluefin-active` and returns the
mapped reference. A missing alias is the ordinary no-selected-model state, not
an error or an invented default. Users can also manage and pull models in
llmman's own models page; refresh generations reject stale
health/alias reads.

### Goose, Linux MCP and Ask Bluefin

[`internal/troubleshoot`](../../internal/troubleshoot/troubleshoot.go) installs
`ublue-os/tap/linux-mcp-server` and the `ublue-os/tap/goose-linux` desktop
cask, with `cpio` installed first because the cask's preflight pipes its RPM
through a `cpio` it does not declare and Bluefin does not ship. Setup taps
`ublue-os/tap` only when a package is missing, resumes missing steps, and
returns `ErrUnsupported` off x86_64, the only architecture the desktop app is
published for. Preview performs no installation or write. Every brew call,
and the Goose launch, runs with `homebrew.WithBrewPath` so brew's children
and llmman find Homebrew's tools under a direct launch.

Goose never reads the user's `~/.config/goose`. `troubleshoot.Profile` is a
dedicated profile under `$XDG_DATA_HOME/chairlift/troubleshooting`;
`Profile.Write` rewrites its `config.yaml` (`RenderConfig`: `linux-tools`
with `--toolset FIXED --host-mode LOCAL_ONLY --no-search-for-ssh-key` and the
`bluefin-knowledge` search enabled, every other extension disabled) and
`.goosehints` atomically before every launch, sets missing or Goose-default
`focusWindow` and `quickLauncher` entries in Goose Desktop's `settings.json`
to null while preserving different choices and other settings, and links the user's
`mimeapps.list` and `dconf` into the desktop app's own `XDG_CONFIG_HOME`.
Without that, GNOME Wayland's GlobalShortcuts portal opens a keyboard-shortcut
approval dialog for Goose's default global shortcuts when Goose starts.

[`agentmode.ObserveLive`](../../internal/agentmode/readiness.go) evaluates,
in order: supported architecture, `linux-mcp-server` and `goose-desktop`
resolved to absolute paths, a healthy llmman, and a configured active-model
alias. Nothing on disk is verified. A missing package turns the Goose row's
button into Set Up; the Launch button rechecks readiness on each click.
[`agentmode.Launch`](../../internal/agentmode/launch.go) writes the profile,
then runs `llmman launch goose-desktop --model bluefin-active` with
`GOOSE_PATH_ROOT` and `XDG_CONFIG_HOME` inside it, through `launcher.Run`.
Both Goose launches write their stdout and stderr to ChairLift's own stderr,
so a Goose that fails to start (#544) leaves its reason in the same journal
stream as ChairLift's log rather than in `/dev/null`.
Launch lifetime is detached from the short preflight context. A fresh launch
keeps the Goose row busy until the session holds the profile's
single-instance lock, the process exits, or 15 seconds pass, so a second
click cannot start a second `llmman launch` that llmman would refuse; a
non-zero exit during that wait is the launch's error, and later failures
return through the GTK thread. When Goose is already running in the
profile, Launch starts `goose-desktop` there directly instead; Goose's
single-instance lock hands the request to the running session, so no second
Goose starts. GNOME may show a "Goose is ready" notification rather than
raise the window, since no activation token is passed. Launch reports which
of these happened (`agentmode.LaunchResult`), and the row's toast
(`pageview.GooseLaunchToast`) says "started", "still starting", or "already
running" rather than claiming a window opened: a Goose that cannot draw still
holds the profile, and every later click is handed to it.

`chairlift --ask-bluefin` uses the same pure
[`dispatcher`](../../internal/agentmode/dispatcher.go): launch when ready,
otherwise present Agents with the missing prerequisite. Cold and running
application requests share that dispatcher.
`internal/devmenu` changes only the existing distro-owned Ask Bluefin menu
tuple's visibility — recognizing the web link, `chairlift --ask-bluefin`,
and the distro's `/home/linuxbrew/.linuxbrew/bin/chairlift-wrapper
--ask-bluefin` — using the user-layer override/reset rule; it never rewrites
the command and does not run a privileged action. Because the extension
renders only slots listed in `command-order`, the entry counts as shown only
when its slot is listed there too, and showing it appends a missing slot.

The same Agents page's **Contribute to Bluefin** action uses
[`internal/contribute`](../../internal/contribute/contribute.go) to check
`xdg-terminal-exec`, `ujust`, the `contribute` recipe, Podman and an existing
Hive registration file, again each time the group is shown so a requirement
fixed outside ChairLift is picked up without a restart. Readiness launches
`xdg-terminal-exec ujust contribute` through the async launcher; a missing
prerequisite leaves an explained,
insensitive action, and missing registration adds a **Registration Guide**
button that opens the contributor configuration documentation. Registration is not created or validated with Hive here,
and dry-run launches nothing.

## Printer applications (`internal/printerapp`)

Printer applications, unlike Agent Mode, are rootless Podman quadlets.
[`printer-applications.md`](printer-applications.md) is the detailed owner
for the three shipped families, their signature-verified digest pins, the
loopback-only network boundary
([ADR-0020](../adr/0020-printer-apps-loopback-only-on-moving-stable-tag.md)),
artifacts and readiness model. `RenderUnit` must never use `Network=host` or
publish beyond `127.0.0.1`, because PAPPL's web administration is
unauthenticated. Existing units remain disableable; `ProbeDiagnostics` uses
systemd's state word, recent journal messages and local image observations to
classify failures, never unit presence as proof of running.

## View-layer page presentation (`internal/views/pageview`)

Pure-Go view decisions live next to GTK wiring rather than importing it into
headless tests. [ADR-0007](../adr/0007-pure-leaf-packages-route-around-untestable-gtk.md)
records the split. [`pageview`](../../internal/views/pageview/) owns shared
row text, confirmations, Help-link order, deployment identity, service
readiness wording and configured invocation shape. Current signatures include
`BootcStageResultSubtitle(staged, version)`; it does not render the helper's
last output line as the system's conclusion. `bundleview` owns collection
presentation, and `updatepresent` owns update snapshot/item presentation.
The source-wiring test inventories all seven page builders without importing
puregotk.

`actionstate.Gate.TryStart` admits one action; `Reset` keeps a visible action
retryable and `Complete` permanently closes a completed row. `RefreshGate`
rejects stale inventory workers. Apps shows Homebrew collections first, then
installed casks and explicitly requested formulae, then the Brewfile exporter.
Installed-package actions share one gate across a row's controls; uninstall/pin failure and preview
restore controls without refreshing, while live success refreshes known
inventory. Export always restores its gate and spinner, and a home-directory
failure is not permission to export to the filesystem root. List read errors
preserve known rows. These local package-action gates do not replace the
unified shell's shared update admission.

`rowset.Tracker` owns row removal/clear/trim on the main thread. `signalroute`
and GTK's retained `buttonRoute`/`dialogRoute` share callback identities across
rebuilt rows and single-shot confirmations; rebuilding lists must not spend a
fresh purego trampoline per item. `guardedSwitch.set` suppresses programmatic
restoration so a failed stop cannot trigger an unintended enable.

### View-layer toast and decision helpers (`internal/views/actionmsg`, `internal/views/trustmsg`)

[`actionmsg`](../../internal/views/actionmsg/) pairs preview wording with
execution/confirmation decisions where a handler has a second effect:
`MaintenanceScript.Execute`, `BundleInstall.Complete`, `TapTrust.MutateUI`,
feature/service switch confirmation and optional developer-feed admission.
String-only helpers select feedback when action state is owned elsewhere.
Current export feedback is `BundleDump(dryRun)`, not a path-taking signature.
[`trustmsg`](../../internal/views/trustmsg/) supplies actionable trust errors
without referring to a disabled group. Keep state decisions out of toast
parsing and avoid duplicating an exhaustive function catalog in prose; these
packages are the API reference and their tests cover the outcome branches.

## Install-path consistency (`internal/installcheck`)

[`Makefile`](../../Makefile) builds the GUI and two helper binaries with
`CGO_ENABLED=0`. `make install` defaults to `/usr`, placing helpers at the
fixed paths their policies authorize. `DESTDIR` stages the tree; another
prefix does not establish a working privileged installation. Maintainer
configuration lands at `/usr/share/chairlift/config.yml`; the
administrator-owned `/etc/chairlift/config.yml` is never installed over.
The channel-table example lands at
`/usr/share/doc/chairlift/channels.example.yml`, not a live override path.

The shipped policy inventory is bootc, updex and ublue:

- `/usr/share/polkit-1/actions/io.projectbluefin.chairlift.bootc.policy`
- `/usr/share/polkit-1/actions/io.projectbluefin.chairlift.updex.policy`
- `/usr/share/polkit-1/actions/io.projectbluefin.chairlift.ublue.policy`

Policies select fixed executable paths and, for helpers, a supported first
argument. Complete argv validation still belongs to the helpers. They use
normal administrator authentication (`auth_admin` / `auth_admin` /
`auth_admin_keep`); no passwordless rules ship. Source install removes the
legacy ChairLift rule files. ChairLift's own default staging path remains
`/usr/libexec/bootc-update-stage`, which the OS must provide.

Homebrew distribution consumes the GoReleaser archive
`chairlift_<version>_linux_<arch>.tar.gz`; there are no deb/rpm/apk packages or
GUI-less system-integration package. The user-scope cask cannot install
root-owned helper/policy files, so OS integration installs those fixed paths
from the archive. [` .goreleaser.yaml`](../../.goreleaser.yaml) carries the GUI,
both helpers, configuration/example, policies, wrapper, desktop/icon assets,
license and all shipped GSettings schema XML files. `make install` compiles
the complete system schema cache only on direct installation; a DESTDIR
assembler compiles its complete staged schema directory itself.

Release publishing uses GoReleaser OSS with `GITHUB_TOKEN`, archive/checksum
keyless signatures and archive SBOMs. Its canonical repository URL is the
literal in `release.footer`; local snapshots are configured by `snapshot`,
not a separate publishing workflow. The
[release workflow](../../.github/workflows/release.yml) waits for `make ci`,
`make e2e` and `make e2e-atspi` before the write-capable release job.
Current distribution rationale is
[ADR-0018](../adr/0018-distribute-via-homebrew-release-archives.md), which supersedes the
older package split in
[ADR-0006](../adr/0006-split-system-integration-package-with-mutual-conflicts.md).
[ADR-0001](../adr/0001-fixed-path-pkexec-privilege-boundary.md) and
[ADR-0002](../adr/0002-usr-prefix-is-the-only-supported-install-prefix.md)
record the fixed-path and `/usr` decisions; live packaging owns the inventory.

[`internal/installcheck`](../../internal/installcheck/) compares helper
constants, every policy action, the source-install layout and every archive's
inventory. The installed accepted-command and rejection cases also run the
real built helpers in E2E; tests under `cmd/` alone would not reach the unit
gate. Current APIs/paths come from those authorities, not retired test names
or package counts in an old plan.

## Verification commands

The [test workflow](../../.github/workflows/test.yml) and `make ci` use
`./internal/... -run '^Test[^I]' -skip Integration`. Pure provider/executor
and presentation tests drive local fake commands or loopback servers; they
must not depend on real host mutations, GTK initialization or outbound
registries. Focused checks after changing these contracts:

```sh
go test ./internal/homebrew ./internal/flatpak ./internal/bootc ./internal/updex ./internal/ublue ./internal/ubluehelper ./internal/helperexec ./internal/stageexec ./internal/journal ./internal/registrytags ./internal/updateflow ./internal/updateproviders ./internal/devtools ./internal/aistack ./internal/agentmode ./internal/troubleshoot ./internal/contribute ./internal/printerapp ./internal/installcheck -run '^Test[^I]' -skip Integration
make ci
```

Native UI and installed-helper checks run only in the isolated Dakota
Wayland harness, never the developer's live display/session:

```sh
make e2e
make e2e-atspi
```

For documentation reconciliation, compare the linked live owners and follow
[documentation consistency](../documentation-consistency.md); source and
configuration win over historical plans or frozen external parity lists.

## Collection package descriptions

Apps renders Brewfile entries immediately. On first expansion of a collection,
`homebrew.DescribeBundleItems` batches formula and cask metadata through
`brew info --json=v2`, sharing the read timeout across both commands. It does
not install packages or grant tap trust, and `brew info` never auto-taps: names
in untapped taps fail as unavailable rather than cloning the tap. Homebrew may
fetch metadata; an offline or failed query leaves package names and types
visible with “Description unavailable”. Because Homebrew emits no JSON when any
one name in a batch is unresolvable, a failed batch is retried one name at a
time under the same deadline so the resolvable entries keep their descriptions.
The row owns cancellation and rejects publication after destruction.

Full identifiers and formula aliases match the returned metadata; formula and
cask namespaces remain separate. Option-like tokens, URLs and Ruby paths are
excluded from metadata commands.

Flatpak entries take their summary from `flatpak.DescribeApps`, which runs
`flatpak search --columns=application,description <id>` once per application
ID under the same deadline. `flatpak search` reads only the remotes' cached
AppStream data, so it works offline and never refreshes, installs or adds a
remote. It matches substrings, so only the row whose application ID equals the
Brewfile entry is used. Entries that are not plain reverse-DNS application IDs
are never passed to flatpak. A missing `flatpak`, an uncached remote or a
failed search shows “Description unavailable” without affecting the Homebrew
descriptions. App Store and VS Code entries show their package type only until
their own metadata providers are implemented.
