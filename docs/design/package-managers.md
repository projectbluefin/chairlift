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

All integrations read the single process-wide `dryrun.Enabled()` flag from
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

The four `updateflow.SourceID` values map to these production adapters and
configuration namespaces in [`internal/window/window.go`](../../internal/window/window.go):

| Source | Constructor | Policy group | Apply operation |
| --- | --- | --- | --- |
| `applications` | `NewFlatpak` | `updates_page.flatpak_updates_group` | Update the user/system scopes represented by pending items |
| `developer-tools` | `NewHomebrew` | `updates_page.brew_updates_group` | `homebrew.Update(ctx)` followed by `homebrew.Upgrade(ctx, "")` |
| `system-components` | `NewSystemComponents` | `features_page.features_group` | `updex.UpdateFeatures(ctx)` |
| `operating-system` | `NewOperatingSystem` | `updates_page.bootc_updates_group` | `bootc.StageUpdate(ctx, events)` |

`Provider` supplies `ID()`, `Available()`,
`Check(context.Context) (CheckResult, error)` and
`Apply(context.Context, []Item, func(Progress)) (ApplyResult, error)`.
`Item.ID` is execution identity, separate from `Name`; `Scope` preserves the
Flatpak installation or bootc transport. `CheckResult` carries pending items
and restart state. `ApplyResult` carries `Changed`, `Preview` and
`RestartRequired` independently.

[`Coordinator`](../../internal/updateflow/coordinator.go) checks enabled
sources concurrently and applies pending sources serially in provider order:
Flatpak, Homebrew, updex, bootc. `Policy.Configured` is administrator policy;
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
and the shell answers a preview Update all with `actionmsg.UpdateAllPreview`
instead of the completion notification.
An apply with `Changed=false` and no preview preserves pending items and sets
a retryable apply failure, not completion. Retry applies only failed sources;
maintenance can be retried separately. `PhaseRestartRequired` has
`ActionNone`: restart is the Operating system row's separately confirmed
**Restart now** action, not a page-level automatic step.

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
  re-list inside the bulk Apply method.
- Updex counts each affected feature once if any component has an update.
  Captured warnings fail the unified check as incomplete. Successful live
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
[`updatepresent`](../../internal/views/updatepresent/updatepresent.go). It
pulses one reusable GLib timer while checking/updating, independent of provider
output, and removes the timer at idle/disposal. Its visible toast overlay
announces each phase's one status line, which sits under the primary action.
`Presentation.ShowStatus` hides an empty header.
`SetSecondaryContent` mounts the existing Updates preferences beneath the
source rows; automatic updates, system version, unverified sources and
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

`applications_page.brew_bundles_group` is independent of `brew_group`.
Built-in `bundles_paths` contains `/usr/share/ublue-os/homebrew`,
`/usr/share/chairlift/bundles`, and `/etc/chairlift/bundles`; shipped
[`config.yml`](../../config.yml) replaces that list with the first directory
only. Group visibility is floored on Homebrew before discovery.
A per-path `bundleview.InstallGate` prevents overlapping collection installs.
Live success completes that row and refreshes installed packages; failure or
preview restores Install, and preview requests no inventory refresh.
Inline install progress pulses rather than inventing a percentage.
Each row's installed state is observed, not remembered:
`refreshBundleStatuses` (`internal/views/bundle_install.go`) runs
`BundleCheck` for every collection off the GTK thread once the rows are
built, again after every live collection install (success or failure), and
after a live Homebrew uninstall. `bundleview.ObservedInstalled` maps Installed
and Update Available to installed, Not Installed to not installed, and
Indeterminate to no change; `InstallGate.Observe` then closes an idle row as
Installed or reopens an Installed one, and never touches a running install.
An `actionstate.RefreshGate` generation lets only the newest refresh publish,
so a check begun before an install cannot overwrite its outcome.

### Tap trust

[`trust.go`](../../internal/homebrew/trust.go) combines
`brew tap-info --installed --json` with on-disk installation receipts:
`<prefix>/Cellar/<formula>/<version>/INSTALL_RECEIPT.json` and
`<prefix>/Caskroom/<token>/.metadata/INSTALL_RECEIPT.json`, reading `source.tap`.
Only untrusted taps with installed packages are actionable. A missing `trusted`
field on older Homebrew is treated as trusted, not false.
Every `tap-info` call runs with `HOMEBREW_NO_GITHUB_API=1`: its JSON also
reports whether each tap is private, which brew asks the GitHub API, and on
Linux that credential probe intermittently kills the command with
`Error: Broken pipe`. Trust is local, so nothing ChairLift reads is lost.

`TrustPackages` runs `brew trust --formula ...` and/or `--cask ...` with
qualified installed package names. Trust is per-user, never pkexec.
[`updates_page.go`](../../internal/views/updates_page.go) confirms source trust
before running it. Failed discovery retains a visible Retry action. Live
success removes only the trusted source row and starts the coordinator's
shared check; preview restores Trust without removing a row. The
configuration-gated **Unverified sources** group may not exist, so
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
healthy remotes' updates are returned alongside that error, but the
Applications source's check still fails: the coordinator keeps the last known
inventory on any check error rather than adopting a partial one. The
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
all features are current. The unified adapter separately treats captured
warnings as a failed check. Feature availability and read errors must remain
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
is the complete fourteen-command surface:
`channel-switch`, `dx-enable`, `dx-disable`, `restart`, `rollback`,
`auto-updates-enable`, `auto-updates-disable`, `driver-switch`,
`factory-reset`, `pin`, `unpin`, `kvm-enable`, `docker-enable`,
`docker-disable`. Every command has exactly one action in
[`data/io.projectbluefin.chairlift.ublue.policy`](../../data/io.projectbluefin.chairlift.ublue.policy).
The helper dispatcher rejects unhandled commands; parser acceptance alone is
not evidence a command executes.

`ubluehelper.Timeout` is the helper's per-command budget: 30 minutes for the
image-pulling `channel-switch`, `driver-switch`, `pin`, and `unpin` (the same
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
authentication refusal; an inaccessible helper at exit 127 is failure.

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
gates both **Free up space** and post-update maintenance. `Run` stops at the
first failure/cancellation; `RunSteps` attempts both independent steps and
returns typed cleaned/skipped/failed/cancelled outcomes. An absent provider is
skipped, not a success. Cleanup is not Powerwash and never absorbs an
administrator script or container removal.

The coordinator runs optional post-update maintenance only after live completed
source work with no failed/pending updates and with the user's
`MaintenanceAfterUpdates` preference enabled. While it runs, `Snapshot.Maintaining`
keeps the phase `PhaseUpdating` with no action, so the panel shows the cleanup
step and the progress bar instead of "System is up to date" before cleanup
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

## Developer workstation and selected gaming components

[`internal/devtools`](../../internal/devtools/devtools.go) owns optional
Homebrew editor installs, WSL backends and Docker CLI setup under
`features_page.dx_group`. The developer-access switch does not install every
editor or switch to a different OS image. `Tools()` is the current typed
inventory, including each tool's one-line description; architecture support
is checked per tool. Row text and the Install button's per-tool accessible
name come from `pageview.DeveloperTool`. The Developer group re-reads only the
tools' installed state each time it is shown (one `map` handler connected at
build), so an uninstall on Apps or in a terminal is reflected without a
restart. That passive read stands aside while `developerGate` is held, and
every gated action that publishes tool state begins a new
`developerToolRefresh` generation, so an older read never overwrites it.

`dx_group.wsl_backend` defaults to **nsl**, with **Lima** as an administrator
option and an in-session backend chooser. The first observation retains an
existing Lima Ubuntu machine when no ChairLift nsl machine exists; it does not
persist the chooser selection or silently migrate data. nsl requires Linux amd64;
Lima supports Linux amd64 and arm64. Both require actual invoking-session
`/dev/kvm` access; the fixed `kvm-enable` grant requires a new login before
setup continues. nsl installs the `frostyard/tap/nsl` cask and runs `nsl doctor`.
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

Docker readiness needs an accessible local socket answering `/_ping`, not
installed tools; fixed helper words own the daemon lifecycle and invoking
account's access. View mutations share `developerGate` and render post-action
observations instead of assuming the requested state.

[`internal/gaming`](../../internal/gaming/gaming.go) manages explicitly
selected refs from `Components()`. Enable installs missing refs in the
**system** scope (`Install(id, false)`, #503): Bluefin-family images
configure Flathub only as a system remote, and their policy is system-wide
Flatpaks. The `flatpak` CLI authorizes that through Flatpak's own PolicyKit
(`org.freedesktop.Flatpak.app-install`/`runtime-install`), not ChairLift's
`pkexec` boundary. Its inventory queries application/runtime kinds in both
scopes, so a per-user copy from an earlier release counts as installed and is
not duplicated. Disable removes each selected ref from exactly the scopes it
is observed in (`Uninstall(id, true)` for a user copy, `Uninstall(id, false)`
for a system copy); unselected refs are never touched, and a ref with a copy
left is a failure. A system copy the image declares it ships (Flatpak
`preinstall.d` or Bluefin's `system-flatpaks.Brewfile`, read by
`imageShipped`) is left in place and returned as kept rather than removed.
Invalid selections fail before mutation, duplicates run
once, and per-item failures preserve partial outcomes. The Features view
serializes action/refresh with `gamingGate`; failed refreshes preserve last
known state. Neither gaming nor per-user Homebrew trust adds a ChairLift
privilege route.

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
optimistic switch. Model/preset controls remain visible but insensitive until
ready, with their unmet prerequisite explained.

[`migration.go`](../../internal/aistack/migration.go) reconciles only an
existing owned service. Matching file bytes alone do not prove that the daemon
adopted local-only settings: `restartService` reloads, restarts, validates the
live systemd `InvocationID`, and only then stamps that ID as a unit comment.
A missing/mismatched stamp retries reconciliation; a verified current
invocation does not restart, and an absent unit stays disabled. Reconciliation
failure preserves the unstamped unit and best-effort stops the old daemon.
Live startup waits up to ten seconds for health after reconciliation; preview
performs one bounded probe and writes nothing.

[`models.go`](../../internal/aistack/models.go) owns candidate selection and
the `bluefin-active` alias. Candidate resolution uses injectable live Hugging
Face reads or the dated offline catalog, filters to supported text/chat GGUFs
fitting observed node memory plus the safety margin, and ranks eligible
artifacts. `PullModel` verifies storage through node observations.
`ConfigureActiveModel` saves the canonical `hf.co/...` alias target, invokes
the same restart/stamp boundary, and waits for health before confirming.
A missing alias is the ordinary no-selected-model state, not an error or an
invented default. Presets and service toggles share the view mutation gate;
refresh generations reject stale health/alias reads. Dry-run selection keeps
the displayed configured model.

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
`.goosehints` atomically before every launch, and links the user's
`mimeapps.list` and `dconf` into the desktop app's own `XDG_CONFIG_HOME`.

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
for the three shipped families, exact index pins, host-network administration
boundary, artifacts and readiness model. `CanEnable` currently refuses every
family before even the enable preview path. Existing units remain disableable;
`ProbeDiagnostics` uses systemd's state word, recent journal messages and local
image observations to classify failures, never unit presence as proof of
running. No image/source change is permission to bypass the authentication gate.

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
