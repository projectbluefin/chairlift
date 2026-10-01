# Package Manager Wrappers

Living design document (formerly `yeti/package-managers.md`; folded into
`docs/` per [frostyard/core ADR-0025](https://github.com/frostyard/core/blob/main/docs/adr/0025-consolidate-repository-docs-into-docs.md)).

Each wrapper lives in its own package under `internal/` and follows a consistent pattern: module-level dry-run flag, availability check with cached variant (`IsInstalledCached()` using `sync.Once`), and context-based timeouts in two classes — 30s for read-only commands, 30m for state-changing ones, selected per invocation by each package's `commandTimeout(args)` helper from its `stateChangingCommands` map. All are called from `internal/views/` page builders. The cached availability check is important for the deferred-visibility startup pattern — multiple goroutines may check the same tool, and the result should only be computed once.

## Homebrew (`internal/homebrew/homebrew.go`)

Wraps the `brew` CLI. Uses JSON output (`--json=v2`) for structured data where available.

### Key types

- **`Package`** — name, version, pinned status, outdated flag, `InstalledOnRequest` bool, `Dependencies` string slice (struct field exists but not populated by current parsing)
- **`SearchResult`** — name plus a required `PackageKind` (`Formula` or
  `Cask`). The kind is not cosmetic: it selects whether installation adds
  `--cask`.

### Operations

| Function | CLI command | Timeout | Notes |
|----------|------------|---------|-------|
| `ListInstalledFormulae()` | `brew info --installed --json=v2 --formula` | 30s | JSON parsed |
| `ListInstalledCasks()` | `brew info --installed --json=v2 --cask` | 30s | JSON parsed |
| `ListOutdated()` | `brew outdated --json=v2` | 30s | JSON parsed; returns both formulae and casks |
| `Search(query)` | `brew search --formula <query>`, then `brew search --cask <query>` | 30s each | Text output parsed into typed formula/cask results; one namespace's normal no-match exit is treated as empty |
| `Install(name, isCask)` | `brew install [--cask] <name>` | 30m | State-changing, dry-run aware |
| `Uninstall(name, isCask)` | `brew uninstall [--cask] <name>` | 30m | State-changing, dry-run aware |
| `Upgrade(ctx, name)` | `brew upgrade [<name>]` | 30m (or the caller's, whichever is nearer) | State-changing; empty name upgrades all formulae and casks; Update All passes its cancellation context |
| `Update(ctx)` | `brew update` | 30m (or the caller's, whichever is nearer) | State-changing; runs under the caller's context so Update All's cancellation stops it |
| `Pin(name)` / `Unpin(name)` | `brew pin/unpin <name>` | 30m | State-changing, dry-run aware |
| `Cleanup()` | `brew cleanup` | 30m | State-changing; returns output string |
| `BundleDump(path, force)` | `brew bundle dump [--file=<path>] [--force]` | 30m | State-changing; writes to file path |
| `BundleInstall(path)` | `brew bundle install [--file=<path>]` | 30m | State-changing, dry-run aware |
| `BundleCheck(path)` | `brew bundle check [--file=<path>]` (two-pass) | 30s | Read-only, active under dry-run; two-pass status discrimination |
| `AvailableBundles(paths)` | none | — | Discovers immediate `*.Brewfile` entries from every configured directory |

### State-changing commands

The `stateChangingCommands` map has eleven keys: `install`, `uninstall`, `remove`, `upgrade`, `update`, `pin`, `unpin`, `bundle`, `cleanup`, `trust`, `tap`. `isStateChanging(args)` classifies brew invocations: for `bundle`, subcommands `check` and `list` are read-only (30-second budget, active under dry-run), while `install`, `dump`, and bare bundle invocations remain state-changing (30m budget, dry-run skipped). First, when dry-run is active state-changing commands are skipped entirely and return a mock message. Second, `commandTimeout(args []string)` returns `mutationTimeout` (30 minutes) for state-changing commands and `readTimeout` (30 seconds) otherwise, including for empty args; `runBrewCommand` passes its result to `context.WithTimeout`. Read commands therefore keep the 30-second budget while installs, upgrades and bundle mutations — which download and build — get 30 minutes. `homebrew_test.go`'s `TestCommandTimeout` asserts command timeouts across both categories.

### Configured Brew bundle discovery

`AvailableBundles(paths []string) ([]Bundle, error)`
(`internal/homebrew/bundles.go`) does no Homebrew invocation. It resolves each
non-empty configured directory to an absolute path and scans only its immediate
entries whose names end exactly in `.Brewfile`. A candidate must resolve to a
regular file. Its display name is the filename without `.Brewfile`, its
absolute path is retained for `brew bundle install --file=...`, and a
first-line `#` comment becomes its optional description. Reading the first
line is bounded to 64 KiB, so a malformed file cannot force an unbounded
description allocation.

The outcomes are deliberately lossless and deterministic:

- a configured directory that does not exist contributes no rows and no
  error, allowing one config to name paths for several distribution variants;
- an empty path, unreadable/non-directory configured path, broken candidate,
  or unreadable Brewfile contributes a joined diagnostic, while bundles from
  other readable paths are still returned;
- a readable directory with no immediate `*.Brewfile` regular files
  contributes no rows and no error;
- repeating the same cleaned absolute file path contributes one row;
- same-named Brewfiles from different directories both remain visible, sorted
  by name and then absolute path, so configuration order never silently hides
  a distinct bundle.

`loadBrewBundles` on the Applications page calls discovery from a worker
goroutine and applies every widget change through one
`sgtk.RunOnMainThread` closure. `brew_bundles_group` is independent of
`brew_group`, so this path never assumes the formulae/casks lists exist.
A successful live `BundleInstall` leaves the clicked row labelled `Installed`
and permanently insensitive, then requests `loadHomebrewPackages()` because a
bundle can install formulae and casks the current inventory snapshot predates.
That refresh is safe in both configurations: `loadHomebrewPackages` nil-guards
each list, so it does nothing visible when `brew_group` is disabled, and
it takes a `brewPackagesRefresh` generation, so a slower bundle-triggered
refresh cannot overwrite newer rows. A failed install restores the `Install`
action. A successful dry-run uses
`actionmsg.BundleInstall(...).Complete == false`, shows an explicit preview,
and restores the action because nothing was installed — and for the same
reason it does not refresh the inventory. `ConnectBundleInstall` owns one
`bundleview.InstallGate` per collection path on Apps, so a second callback
cannot overlap a running install even if
invoked independently of GTK's insensitive-button guard. Each bound button
has a native spinner built once; the shared phase starts it only while
`Installing…` and stops it on success, failure, or preview completion.

Collection and search installs also display an inline native `GtkProgressBar`
beside the action. Homebrew returns no progress fraction, so the meter pulses
with operation text instead of displaying a made-up percentage. One reusable
GTK-thread timer serves every active install; completion, failure, and dry-run
remove each meter, and the last completion removes the timer. Destroying the
Apps page removes the timer too. No command-output line creates a GTK callback.

Installed Flatpak applications, Homebrew formulae/casks, and search results are
ordinary visible preference-group rows rather than collapsed expanders. Their
per-row actions use neutral styling; collection Install and Export buttons do
not compete as blue primary actions. The Homebrew search field is insensitive
while result installs run, and confirmation invalidates older search workers so
they cannot replace the row currently showing installation progress.

Discovery controls lead the Apps page: Browse all apps and Homebrew search,
then installed Flatpak and Homebrew applications, then app collections. The
command-line tool inventory and export action come last. The tool list includes
only formulae whose observed `Package.InstalledOnRequest` is true; Homebrew's
dependency-only formulae remain managed by Homebrew and do not obscure apps.
The export still includes the complete Homebrew inventory.


The Apps page's package-list export holds its own `actionstate.Gate`. It
disables Export and shows `Exporting…` with a native spinner until the worker
returns; every outcome stops the spinner and restores the action. A failed
home-directory lookup is an export error, not permission to write `/Brewfile`.

### Typed search and install

`Search` trims the query and delegates to the pure injected seam
`searchWith(run, query)`. It queries formulae and casks separately because
Homebrew's combined human-readable search output does not reliably label every
result, while `--formula` and `--cask` make the namespace unambiguous. Homebrew
exits non-zero when one namespace has no matches; only its specific
`No formulae or casks found` diagnostic becomes an empty category. Other
errors remain failures. `homebrew_test.go` drives a fake runner to assert both
argv sequences, type preservation, empty-category handling, error
propagation, blank-query behavior, and header filtering.

`onHomebrewSearch` assigns each query a `searchRefresh` generation so an older
slow query cannot replace newer results. Rows display `Formula` or `Cask`, and
the confirmation callback carries that type into
`homebrew.Install(result.Name, result.Kind == homebrew.Cask)`. Each result owns
an `actionstate.Gate`: cancel resets it, confirmation changes the button to
`Installing...`, failure and dry-run restore it, and a live success completes
it as `Installed`. `actionstate.PackageInstall` is the tested authority for
restore/complete/refresh decisions. Live success starts
`loadHomebrewPackages`; that loader has its own `brewPackagesRefresh`
generation, nil-guards the independently configurable installed-package
group, and clear-then-repopulates separate formula/cask `rowset.Tracker`
values.

### Installed Homebrew actions

Each installed formula row has Pin/Unpin and Uninstall controls; each cask row
has Uninstall. A formula's pinned state is visible in both its subtitle and
the Pin/Unpin label. The row shares one `actionstate.Gate` across all of its
controls, so a pin and uninstall cannot overlap. Every action requires an
Adwaita confirmation: pin/unpin is suggested, while uninstall is destructive
and identifies whether the target is a formula or cask.

After confirmation, all controls on the row become insensitive and the
primary control shows `Pinning...`, `Unpinning...`, or `Uninstalling...`;
only then does a worker goroutine call `homebrew.Pin`, `Unpin`, or
`Uninstall(name, isCask)`. `actionstate.PackagePin` and
`PackageUninstall` enumerate the outcomes. Failure restores the original
controls and reports the error, and dry-run success restores them with an
explicit preview toast. Live success completes the old controls and starts
the generation-guarded `loadHomebrewPackages` refresh. The refresh, rather
than the action callback, owns row replacement, so overlapping loads cannot
publish stale installed state.

### Executable resolution (`internal/homebrew/executable.go`)

`ExecutablePath() string` is the one place ChairLift decides *which* `brew` it
means, and both halves of the wrapper read it. It returns the `brew` that
`$PATH` resolves, or the Linuxbrew install path
`/home/linuxbrew/.linuxbrew/bin/brew` when `$PATH` has none, or `""` when the
host has no Homebrew at all.

Both halves matter because Homebrew is reachable by two launch routes.
`data/chairlift-wrapper.sh:5-8` evaluates `brew shellenv` before it exec's the
application, so a launch through the wrapper finds `brew` on `$PATH`; a direct
binary launch — the desktop entry, or `chairlift` run from a shell — does not,
even though the same Homebrew is installed. Resolving only `$PATH` therefore
reported an installed Homebrew as absent, and the fallback is what the wrapper
would have supplied. The fallback is tested with `os.Stat` plus
`Mode().IsRegular()`, the same `[ -f "$BREW_PATH" ]` test the wrapper performs,
so presence means the same thing in both places.

- **Visibility** — `IsInstalled()` resolves through this function and
  short-circuits to `false` when nothing resolves. The views never ask it:
  every Homebrew group is omitted by `internal/capability`'s floor, which
  resolves through `ResolveExecutable`, so the Applications, Updates, and Help
  pages agree on whether Homebrew is present. The update and cleanup providers
  read `IsInstalledCached()`.
- **Execution** — `runBrewCommandCtx` passes `brewExecutable()`, which is
  `ExecutablePath()` with the not-found case kept as the bare name `"brew"` so
  the failure keeps its `*NotFoundError` classification and its "Please install
  Homebrew first" message. Nothing resolves in that case, so the two halves
  still agree: both report no Homebrew.

A `brew` on `$PATH` wins over the fallback, so a user who customised their
`$PATH` keeps the Homebrew they chose. Resolution runs per call rather than
being memoized in a package variable: `exec.CommandContext` already resolves a
bare command name on every exec, so a cache would save nothing measurable while
freezing the answer ahead of any change on the host. The session-stable answer
the UI needs is `IsInstalledCached`'s, which caches the availability verdict
rather than the path. `lookPath` and `statFile` are function seams — the same
pattern as `internal/troubleshoot`'s `lookPath` — and `linuxbrewExecutable` is a
variable for the same reason, so a test can assert the fallback on a host
without Linuxbrew and assert absence on a host with one.
`internal/homebrew/executable_test.go` covers the precedence, the fallback, the
empty case, a directory at the fallback path, and both halves executing the
resolved path end to end.

### Error handling

`runBrewCommand` builds a context from `commandTimeout(args)` and delegates to `runBrewCommandCtx(ctx context.Context, args ...string) (string, error)`, which applies the dry-run skip before constructing an `exec.Cmd` and otherwise calls the unexported `runBrewCommandAt(ctx context.Context, exe string, args ...string) (string, error)` with the resolved `brewExecutable` path (see "Executable resolution" above). The executable path and context are parameters so `runner_test.go` can drive a fake script and control the deadline. `Update(ctx)` and `Upgrade(ctx, name)` narrow their caller's context with `context.WithTimeout(ctx, mutationTimeout)`, so Update All's cancellation stops both `brew update` and `brew upgrade`; other exported mutations get their own deadline. All paths use the same dry-run gate in `runBrewCommandCtx`.

`runBrewCommandAt` starts the command in its own process group (`cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}`) and sets `cmd.Cancel` to `syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)`, so brew's helper processes (git, curl, download workers) are killed with the command instead of being orphaned when only the direct child is signalled. `cmd.WaitDelay` (5s) bounds the wait, because those helpers inherit the stdout/stderr pipes and a straggler would otherwise hold `Wait` open indefinitely. `cmd.Run` still reaps the child. Read-only commands still capture full stdout for JSON/text parsers. State-changing commands wire stdout and stderr to 64 KiB tail writers instead: successful mutation output is discarded, and failures retain only bounded diagnostic context (stderr, or stdout when stderr is empty).

Failures classify into exactly five distinct outcomes, checked in this order — `errors.Is` against `context.DeadlineExceeded`/`context.Canceled`, never `==` on `ctx.Err()`, so a wrapped cause still classifies:

| Condition | Result |
|-----------|--------|
| The context deadline expired (`commandTimeout(args)` elapsed) | `*Error`, message `Command '<exe> <args>' timed out`, unwrapping to `context.DeadlineExceeded` |
| The context was cancelled by its owner | `*Error`, message `Command '<exe> <args>' was canceled`, unwrapping to `context.Canceled` |
| The command exited non-zero and its stderr matches `isUntrustedTapMessage` | `*UntrustedTapError` carrying the stderr text (see "Tap trust" below) |
| The command exited non-zero otherwise | `*Error` carrying stderr, or bounded stdout when stderr is empty for a state-changing command, unwrapping to the `*exec.ExitError` |
| The executable is missing (`exec.ErrNotFound` for a bare name, `fs.ErrNotExist` for an explicit path) | `*NotFoundError` ("Homebrew not found…") |

A deadline and a cancellation never produce the same message, and neither surfaces as `signal: killed` — the process is killed by ChairLift's own `Cancel` func, so the raw wait error is replaced by the classified one. `Error` carries an `Err error` field and an `Unwrap() error` method, so `errors.Is(err, context.DeadlineExceeded)` works for callers while `Error` keeps satisfying `error` and keeps its human-readable `Message`; nothing in the views switches on its concrete type. `internal/homebrew/runner_test.go` covers each outcome against a fake script, asserts the deadline and cancellation messages differ and contain no `signal: killed`, proves state-changing output is bounded/discarded, and `TestRunBrewCommandAtKillsProcessGroup` proves the process-group kill by having the fake script spawn a background `sleep` and then checking that PID is gone after cancellation.

### Tap trust (Homebrew 6) (`internal/homebrew/trust.go`)

Homebrew 6 introduced per-tap trust: formulae/casks from a tap that isn't marked trusted are invisible to normal `brew` operations. Critically, **`brew list`/`brew info` also refuse to load untrusted-tap formulae**, so there is no supported `brew` command that lists what's installed-but-untrusted — ChairLift has to reconstruct that set itself from on-disk state.

**Detection (`ListUntrustedTaps`)** combines three sources:
1. `brew tap-info --installed --json` — parsed for each tap's `name` and `trusted` flag (`parseUntrustedTapNames`); this is the only brew-provided signal, and it tells you *which taps* are untrusted but not *what's installed from them*.
2. Cellar keg receipts (`installedFormulaeByTap`) — walks `<prefix>/Cellar/<formula>/<version>/INSTALL_RECEIPT.json` and reads `.source.tap`, since brew's own listing commands can't see these formulae. One receipt per keg is enough to attribute the formula to a tap.
3. Cask install receipts (`installedCasksByTap`) — reads `<prefix>/Caskroom/<token>/.metadata/INSTALL_RECEIPT.json` and takes `.source.tap`, mirroring the formula path. That receipt (written by Homebrew's `Cask::Tab.create` into the cask's `.metadata` container) is the authoritative origin recorded at install time. The earlier scan of versioned `Caskroom/<token>/.metadata/*/*/Casks/*.json` metadata could not see tap-sourced casks at all: a cask installed from a tap is saved as a `.rb` Caskfile, so no `Casks/<token>.json` exists for it and exactly the untrusted-tap casks the remediation UI exists for were silently dropped.

Only untrusted taps with at least one installed formula or cask are returned (`UntrustedTap{Name, Formulae, Casks}`, package names fully qualified as `tap/name`, ready to pass straight to `brew trust`); taps with nothing installed aren't actionable and are dropped.

**`TrustPackages(tap)`** runs `brew trust --formula <formulae...>` and/or `brew trust --cask <casks...>` for the given tap. This is a **per-user** operation (state lives in `~/.homebrew/trust.json`) — it does not use `pkexec` and does not require root, unlike bootc staging or updex writes.

Because `brew trust --formula/--cask ...` has `args[0] == "trust"`, and `trust` is one of homebrew's ten `stateChangingCommands`, `TrustPackages` runs under the 30-minute mutation timeout rather than the 30-second read budget — trusting a tap can trigger substantial work — and, by the same map membership, already no-ops under dry-run at the exec layer (see "Cross-cutting: dry-run" below). But `trustTap` (`internal/views/updates_page.go`) used to always mutate the Untrusted Homebrew Taps UI on a successful (nil-error) call — removing the tap's row, hiding the group when empty, and refreshing outdated packages — even when nothing was actually trusted. That made a dry-run click visually remove the tap from the Untrusted Taps list as if it were now trusted, with no way to undo it from the UI. `trustTap` now computes `decision := actionmsg.TapTrust(dryrun.Enabled(), tap.Name)` once in its success branch and gates all three UI mutations on `decision.MutateUI` (exactly `!dryRun`): when true, behavior is unchanged from before; when false, the row stays, the group stays visible, `loadOutdatedPackages` is not re-queried, and the click's button is reset (`SetSensitive(true)`, `SetLabel("Trust")`) instead of being left stuck on "Trusting...". `decision.Toast` — a preview string under dry-run, the same "Trusted %s. Its packages can update again." string otherwise — is shown in both states. This mirrors the `actionmsg.MaintenanceScript`/`ScriptDecision` pattern: the UI-mutation gate itself, not just the toast wording, is what `actionmsg_test.go` asserts.

**`UntrustedTapError`** — `runBrewCommandAt` (`internal/homebrew/homebrew.go`) inspects failed commands' stderr for `"untrusted tap"` or `"taps are not trusted"` (`isUntrustedTapMessage`) and returns `*UntrustedTapError` instead of generic `Error` — only on the non-zero-exit path, and only after the deadline and cancellation branches have been ruled out, so a timed-out or cancelled command is never misreported as a trust problem.

**Anything read out of stdout is gated on argv.** A state-changing command's diagnostic is the join of both streams, and for `brew bundle install` the stdout half is a third-party installer's own replayed output — text an attacker-authored formula controls. Two things are therefore derived from it only when `isBundleInstall(args)` is true, which is decided from the argument vector no command output can influence: the `Error: ... from untrusted tap <user>/<tap>` line that fills `UntrustedTapError.Tap`, and the stdout-only reclassification of a failure whose stderr never mentions trust at all (real, because `brew bundle install` prints only its dependency-failure summary on stderr). Without that gate a formula could print a forged untrusted-tap line during an ordinary `brew install`/`upgrade`, turn an unrelated failure into a trust problem, and make the toast tell the user to run `brew trust` on a tap it chose; with it, a non-bundle command is classified only when brew's own stderr says so and takes its tap name only from stderr. `isBundleInstall` shares `bundleSubcommand` with `isStateChanging`, so both read the subcommand the same way (flags skipped, bare `brew bundle` meaning `install`). As second-line defence the tap capture is restricted to the characters GitHub allows in an owner/repository pair (`[A-Za-z0-9_-]+/[A-Za-z0-9._-]+`, with brew's sentence-ending period trimmed afterwards so a tap such as `foo/bar.baz` is not truncated), because the captured text ends up in a suggested `brew trust <tap>` command. `runner_test.go` holds both halves: the forged stdout line on `brew install`, and the stderr-corroborated `brew upgrade` whose stdout tap name is ignored.

The shell unwraps individual-update errors with `errors.As(err, &trustErr)`.
`trustmsg.UpgradeMessage` points at **Unverified sources** only when that
configuration-gated group exists; otherwise it explains the trust requirement
without referring to a hidden control. Bundle failures keep using the
self-contained `trustmsg.BundleMessage`.

Source discovery failures stay visible with a Retry action. A successful live
trust removes only that source row and starts the coordinator's shared check;
dry-run restores the Trust action without removing the source. The shell is
nil-guarded because source discovery and provider availability are independent.

### View-layer page presentation (`internal/views/pageview`)

`internal/views/pageview` is one of the puregotk-free leaf packages under
`internal/views/`. It owns the widget-independent presentation decisions shared
by all seven page builders. The GTK files create and mutate widgets, but no longer
reimplement the variable row text, status text, Help-link inventory, os-release
parsing, or maintenance invocation that this package returns. The leaf-package
layout itself is decision record
[ADR-0007](../adr/0007-pure-leaf-packages-route-around-untestable-gtk.md).

Its exported outcomes are:

- `FlatpakApplication` returns the application ID alone when no version is
  known and `ID (version)` otherwise; `HomebrewPackage` returns the version
  alone or appends the pinned marker; `BrewBundle` returns the path alone or
  `description — path`; and `SearchResult` preserves the result's typed
  Formula/Cask label.
- `UntrustedTap` names the source and combines its formula/cask count. Update
  item identity, versions and installation scope belong to `updatepresent`.
- `BootcUpdateSubtitle` distinguishes not staged, staged without a version,
  and staged with a version. `BootcStageResultSubtitle` returns that staged
  text after a staged action, otherwise preserves the stage script's final
  message when one exists and falls back to `System is up to date`.
- `Feature` maps the updex description/name to title/subtitle, while
  `FeatureGroupDescription` formats the loaded feature count.
- `HelpResources` emits Website, Report Issues, and Community Discussions in
  that fixed order while omitting every unconfigured URL.
- `DeveloperOnboardingTargets` returns the developer-mode onboarding links in
  their fixed order — Bluefin developer documentation, Project Bluefin
  training catalog, GNOME Developer Center — and returns nothing for every
  input combination other than a confirmed live enable, so a dry-run preview,
  a disable, and a failed group promotion each open no browser. The URLs live
  beside it as a package-level table, so the collection and its order are
  asserted without GTK.
- `MaintenanceCommand` returns a direct script invocation for an
  unprivileged action and the exact `pkexec <script>` shape for a privileged
  action.
- `ParseOSRelease` ignores comments, blank lines, and lines without `=`;
  splits a retained line at its first `=`; removes quote characters at the
  value's edges;
  title-cases the key; marks `*URL` fields as links; and returns scanner
  failures. `ShortDigest` leaves digests of 19 characters or fewer unchanged
  and truncates longer values to 19 characters plus an ellipsis.

`pageview_test.go` calls every exported function directly and table-tests every
branch above. `wiring_test.go` inventories all six page files and requires each
to call its corresponding `pageview` functions while rejecting the retired
inline implementations. This supplies headless enforcement without adding a
test binary to the puregotk-importing parent package.

### View-layer toast and decision helpers (`internal/views/actionmsg`, `internal/views/trustmsg`)

`internal/views/actionmsg` and `internal/views/trustmsg` hold completion and
trust feedback without importing GTK, following
`docs/skills/gtk-headless-testing/SKILL.md`. State decisions stay in the pure
coordinator or the action owner, rather than being inferred from toast text.

- **`internal/views/trustmsg`** (added for issue #57, extended for #266) — `UpgradeMessage(pkgName string, trustGroupAvailable bool) string` (toast shown when a Homebrew upgrade fails with an `*homebrew.UntrustedTapError`) and `BundleMessage(bundleName, tap string) string` (toast shown when bundle installation fails due to an untrusted tap); see "Tap trust" above.
- **`internal/views/actionmsg`** (added for issue #56, extended for issue #8 and issue #238) — builds the toast text for every state-changing view action across the maintenance, applications, updates, and features pages, and, where the view also has a second effect to gate, the decision itself: the execute/complete/mutate/confirm decision at the four call sites that mutate a row, group, or switch on success, and the developer feed setup's start/no-start admission and feedback classification (issue #238), so the same table-driven test in `actionmsg_test.go` that checks the toast also checks the gate (see "Dry-run mode" in [overview.md](./overview.md#dry-run-mode) for the general rule this implements). Exported surface:
  - `ScriptDecision{Execute bool; Toast string}` + `MaintenanceScript(dryRun bool, title string) ScriptDecision` — gates whether `runMaintenanceAction` constructs and runs the configured script's `exec.Cmd` at all (c1)
  - `BundleDump(dryRun bool, path string) string` — Homebrew Brewfile dump toast (c1)
  - `BundleInstallDecision{Complete bool; Toast string}` + `BundleInstall(dryRun bool, name string) BundleInstallDecision` — completes a successfully installed bundle row in live mode, or resets it after a dry-run preview (issue #8)
  - `Cleanup(dryRun bool, tool, output string) string` — Homebrew/Flatpak cleanup toast (c1)
  - `Install(dryRun bool, pkgName string) string` — Homebrew install toast (c2)
  - `Uninstall(dryRun bool, name string) string` — Homebrew or Flatpak uninstall toast (c2)
  - `Pin(dryRun bool, name string, pin bool) string` — Homebrew formula pin/unpin toast (c2)
  - `Upgrade(dryRun bool, pkgName string) string` — Homebrew per-package upgrade toast (c3)
  - `Update(dryRun bool, appID string) string` — Flatpak per-app update toast (c3)
  - `SelfUpdate(dryRun bool, tool string) string` — Homebrew self-update ("Update Homebrew" button) toast (c3)
  - `TapTrustDecision{MutateUI bool; Toast string}` + `TapTrust(dryRun bool, tapName string) TapTrustDecision` — gates whether `trustTap` removes the tap's row, hides the group, and refreshes outdated packages (c3)
  - `SystemStage(dryRun bool, staged bool) string` — bootc stage-button completion toast; string-only since subtitles stay live in both modes and there is no mutation left to gate (c4)
  - `FeatureToggleDecision{Confirm bool; Toast string}` + `FeatureToggle(dryRun, enable bool, name string) FeatureToggleDecision` — gates whether `onFeatureToggled`'s switch confirms the flip or reverts it (c5)
  - `FeatureUpdate(dryRun bool) string` — Features page "Update" button toast (c5)
  - `DeveloperFeedSetup{InstallPulp bool; StageFeeds bool}` + `DeveloperFeedSetupPlan(dryRun, enabled, succeeded, installPulp, stageFeeds bool) DeveloperFeedSetup` — the admission rule for the optional Pulp install and feed staging behind `dx_group`'s `install_pulp`/`stage_feeds` (issue #238). Only a confirmed live enable produces non-empty work, so `startDeveloperFeedSetup` spawns no worker for a preview, a disable, or a failed promotion, and an unconfigured group produces an empty plan
  - `DeveloperFeedOutcome{PulpReady bool; FeedsStaged bool; StagedPath string}` + `DeveloperFeedFeedback(setup DeveloperFeedSetup, outcome DeveloperFeedOutcome) DeveloperFeedResult{Failed bool; Message string}` — the single decision for the in-app banner that follows (ADR-0009 rule 3). `Failed` classifies the toast and the message wording comes from the same struct, so a failed optional install cannot read as a failed permission change; the staged sentence names the file and asks the user to import it, and never claims a subscription was imported

  The plain-`string` functions (`BundleDump`, `Cleanup`, `Install`, `Uninstall`, `Pin`, `Upgrade`, `Update`, `SelfUpdate`, `SystemStage`, `FeatureUpdate`) select toast wording only. Where an application action also changes row controls or requests an inventory refresh, the separate tested `actionstate` decision owns that UI-side effect. The five decision-struct functions in `actionmsg` exist because their call sites have no wrapper- or `actionstate`-level gate for the *second* effect: script execution has no wrapper package; a bundle row must distinguish a real completion from a dry-run wrapper success; tap-trust row removal and switch confirmation are view-local state that the wrapper's own dry-run skip does not touch; and the developer feed setup's worker must be admitted only for a confirmed live enable and must describe its own failure without touching the switch's outcome.

### View-layer update action state (`internal/views/actionstate`)

`internal/views/actionstate` owns the pure state machines for installed-package
controls, refresh ordering, and other repeatable view actions:

- `Gate.TryStart` atomically moves idle to running and rejects every repeated
  callback while running; `Reset` makes a failed, previewed, or fully-refreshed
  action retryable; `Complete` permanently closes a live-upgraded row action.
- `RefreshGate.Begin` assigns an increasing generation to each metadata
  refresh and `IsCurrent` accepts only the newest, preventing a slower old
  query from publishing after a newer one.
- `PackageInstall`, `PackageUninstall`, and `PackagePin` share the installed
  inventory mutation outcomes: failure and dry-run success restore the row
  controls without a refresh; live success completes the old controls and
  requests a generation-guarded installed-package refresh.

`actionstate_test.go` table-tests every outcome, races 64 callers against one
action gate (requiring exactly one acquisition), and proves 64 concurrent
refresh requests receive unique generations with exactly one current.
The remaining wiring guards cover confirmation and action lifetimes; native
surface behavior is exercised by the AT-SPI scenarios. No `_test.go` is added
to `internal/views`.

### Unified update surface (`internal/views/updatepresent`)

Updates has one summary and primary action beneath the small adaptive Bluefin
wordmark shared with `internal/firstrun`. System sources are grouped separately
from apps and tools. Source states and every pending item are visible action
rows, not provider-specific duplicate lists or nested essential controls.
`updatepresent` maps the immutable coordinator snapshot to text; disabled by
administrator, disabled in preferences, unavailable, check failure and apply
failure remain distinct.

The coordinator owns inventory and the sidebar count (`Snapshot.TotalUpdates`).
No secondary status reader writes a competing badge. A failed check preserves
known inventory and restart state. Restart is offered only for a source that
reports it, never inferred from a successful command.

`UpdateShell.beginMutation` admits Update All, individual app/tool updates,
metadata refresh and dedicated OS staging through one owner. Individual actions
call `updateproviders.UpdateItem`, retaining the provider execution identity in
`updateflow.Item.ID`, the display name, and installation scope. A zero exit is
not enough: the adapter rechecks the requested item before reporting Changed.
An unchanged result keeps the row pending, errors remain visible and retryable,
and a preview restores controls without modifying inventory. Verified live
completion requests the coordinator's generation-guarded check.

The primary action alone is suggested; individual actions are neutral. One
reusable GLib pulse callback runs while work is active, independently of provider
output. Idle and disposal remove its timer. The wordmark changes with Libadwaita
appearance and disconnects its theme observer on disposal.


### View-layer Brew bundle state (`internal/views/bundleview`)

`internal/views/bundleview` is one of the puregotk-free leaf packages
under `internal/views`. It owns the bundle group's load presentation and its
per-row concurrency state, leaving `applications_page.go` to construct and
update widgets only.

`Present(count, warning, homebrewAvailable) Presentation` enumerates the
group-level outcomes: zero bundles without a warning produces the
`No bundles available` placeholder; zero with a warning produces the
`Bundles unavailable` placeholder carrying that warning; one bundle uses a
singular available description; several use a plural description; partial
results append the warning while keeping their rows; and any of those states
with Homebrew unavailable appends that install actions are disabled. The view
composes none of this group/placeholder text itself.

`InstallGate` is zero-value-ready and has three states. `TryStart` atomically
moves idle to running and rejects every concurrent caller; `Reset` returns a
failed or dry-run action to idle; `Complete` permanently closes a live
successful action. `bundleview_test.go` races 64 callers against one gate and
asserts exactly one acquisition, then separately covers reset and completion.
The type has no GTK dependency and the callback still performs every actual
button mutation on the main thread.

### View-layer row bookkeeping (`internal/views/rowset`)

`internal/views/rowset` is a puregotk-free leaf for row removal,
clear/repopulate bookkeeping and bounded rolling-window eviction. It imports
only the standard library and never names a widget type.

Exported surface:

- `Tracker[T comparable]` — a generic, zero-value-ready value type holding the rows added since the last clear. It is generic and takes its removal actions as callbacks precisely so it never names a widget type, which is what keeps its dependency graph free of puregotk.
- `Add(row T)` — records a row that has just been added to the container.
- `Len() int` — how many rows are currently tracked.
- `Remove(row T, remove func(T)) bool` — removes the first matching tracked
  row, invokes the callback once, preserves the order of all other rows, and
  reports whether it found the row.
- `Clear(remove func(T))` — invokes the caller-supplied removal callback once per tracked row, in insertion order, then resets the slice to nil. A no-op on an empty or zero-value tracker.
- `TrimTo(limit int, remove func(T)) int` — evicts the oldest tracked rows until at most `limit` remain, invoking the callback once per evicted row in insertion order, and reports how many it evicted. A negative limit is treated as zero. This is the rolling-log counterpart to `Clear`: `stageProgressSink` calls it after every batch of staging output so the Details expander holds a bounded window of the most recent rows.

`Tracker` has no mutex, generation counter, or in-flight flag by design: GTK main-thread safety is a property of the call site, which keeps the clear-and-repopulate sequence inside a single `sgtk.RunOnMainThread` closure, so the tracker is only ever touched from the main thread. `rowset_test.go` drives several successive simulated loads (including an empty load after a non-empty one) against a fake, non-GTK container and asserts after every load that the container holds exactly that load's rows; a separate case appends rows one at a time with a `TrimTo` after each and asserts the container never exceeds the cap and always holds the newest rows in order.

### View-layer shared signal routing (`internal/views/signalroute`)

`internal/views/signalroute` is one of the puregotk-free leaf packages under `internal/views/`. It exists because puregotk turns each `Connect*` callback into a purego trampoline from a fixed 2000-slot table, reuses a slot only when the same func variable address is connected again, and never frees one when a widget is destroyed; a list that connected a fresh closure per row on every refresh leaked slots until the process panicked. `Table[A]` maps an emitting object's address to its action: `Bind` (replacing a reused address), `Unbind`, `Clear`, `Dispatch`, and `DispatchOnce` for single-shot emitters such as a dialog response. It imports nothing outside the standard library and names no widget type; the argument type is generic.

The GTK half is `buttonRoute` in `internal/views/widgets.go`: one retained
callback per rebuilt list, with an address-to-action table cleared beside its
rows. Apps inventories and unverified sources use it on `UserHome`; the single
update item list uses it on `UpdateShell`. Rebuilding a list must never connect
new callback identities per item. `dialogRoute` likewise dispatches each
confirmation once.

### View-layer bounded staging output (`internal/views/progresslog`)

`internal/views/progresslog` is one of the puregotk-free leaf packages under `internal/views/`. It bounds what the two OS staging handlers render from a streamed run. Like `rowset` it imports nothing outside the standard library (`sync`, `time`) and names no widget type.

It exists because of what the obvious rendering costs. `stageexec` emits one `EventMessage` per non-empty output line, and the handlers used to answer each one with its own `sgtk.RunOnMainThread` callback that created a permanent `AdwActionRow`. A stage helper that prints thousands of progress lines therefore queued thousands of main-thread callbacks and left thousands of heavyweight widgets behind for the life of the run — an unresponsive window and unbounded memory (issue #81). Neither half is fixable in isolation: capping rows alone still floods the main thread, and coalescing callbacks alone still accumulates widgets.

Exported surface:

- `DefaultLimit` — 200 retained lines. The stage helpers print tens of lines in the ordinary case, and a run verbose enough to exceed this is one whose earliest lines have already scrolled out of any usable reading position; the full output remains in the journal.
- `Line{Text string; At time.Time}` — one retained line and the moment it arrived. The timestamp is stamped on `Append`, not on render, so a batch that reaches the main thread late still reports when the helper actually printed.
- `Batch{Lines []Line; Total int}` — one hand-off: the lines accumulated since the previous `Drain` (never more than the limit), plus every line appended since the `Coalescer` was created, including the ones discarded to stay within the limit.
- `New(limit int) *Coalescer` — a coalescer retaining at most `limit` lines per batch; a limit below one is raised to one, since a progress view that retains no line cannot show even the most recent one.
- `(*Coalescer) Limit() int` — the retention cap, which is also the maximum number of rows a caller ever holds for one run.
- `(*Coalescer) Append(line string) bool` — records a line from the worker goroutine and reports whether the caller must schedule a `Drain`. It returns `true` only for the line that opens a batch, so a burst costs one main-thread callback rather than one per line; past the limit each new line evicts the oldest pending one.
- `(*Coalescer) Drain() Batch` — returns the pending batch on the main thread and clears the outstanding-callback marker so the next `Append` opens a new batch. Draining without a preceding `Append` reports an empty batch, which is why the completion event can drain unconditionally and lose no trailing line.

`Coalescer` is the one leaf package that *does* take a mutex, because unlike the others it is touched from both sides of the main-thread boundary by design: `Append` runs on the worker goroutine and `Drain` on the GTK main thread. The view half is `stageProgressSink` in `internal/views/updates_page.go`, which the bootc staging handler uses (`bootc.ProgressEvent` is `stageexec.ProgressEvent`): `consume` runs on the worker goroutine and touches no widget, `flush` runs on the main thread, renders one batch, calls `rowset.Tracker.TrimTo` to evict older rows from the expander, and sets the Details subtitle from `pageview.StagingLogSubtitle(shown, total)` so a window that hid older lines says so rather than reading like a complete log. `progresslog`'s own tests cover the batching, retention, arrival stamping, and concurrent append/drain; `wiring_test.go` reads `updates_page.go`'s source and fails if the handler goes back to the unbounded per-event shape.


### View-layer feature update status (`internal/views/featurestatus`)

`internal/views/featurestatus` is puregotk-free and owns feature subtitles,
update decisions and the Features group's description. It imports
`internal/updex` for the pure `CheckResult` type, never GTK.

Exported surface:

- `Status{Subtitle string; HasUpdate bool; Incomplete bool}` — the row state for one feature.
- `Feature(name string, results []updex.CheckResult) (Status, bool)` derives
  subtitle, availability and completeness from every component. Empty results
  are an incomplete check, not evidence that a feature is current.
- `GroupDescription(totalFeatures, featuresWithUpdates int) string` — the group description after a check that completed with all components checked.
- `GroupDescriptionIncomplete(totalFeatures, featuresWithUpdates int) string` — the group description when the check was incomplete (one or more enabled components could not be checked or partial warnings were emitted); it presents an incomplete state instead of claiming current.
- `GroupDescriptionCheckFailed(totalFeatures int) string` — the group description when the check itself failed; it makes no claim about update state.

**The ANY-component rule:** a feature has an update when **any** of its components reports one — not the first, not all. `Status.HasUpdate` is an OR across every element of `results`, and `featurestatus_test.go` asserts it by iterating every element of each case's slice rather than special-casing index 0, with cases placing the update first, last, in the middle, and in several components at once.

**The feature-counting rule:** `featuresWithUpdates` is a count of **features**, not of components — a feature with three outdated components counts once. The package doc comment states this explicitly, since the description's first number is a feature count and its second must be one too or the sentence is incoherent.

The six subtitle branches, for a feature named `<name>`:

| Situation | Subtitle |
|-----------|----------|
| zero components / check failed | `<name> — update check failed` |
| exactly one component has an update, non-empty `CurrentVersion` | `<name> — update available for <component> (v<cur> → v<new>)` |
| exactly one component has an update, empty `CurrentVersion` | `<name> — update available for <component> (→ v<new>)` |
| two or more components have updates | `<name> — updates available for <n> components` |
| no updates, every component agrees on a non-empty `CurrentVersion` | `<name> — v<version>` |
| no updates, components disagree or any `CurrentVersion` is empty | `<name> — up to date` |

No branch emits a bare `v` with nothing after it, and no branch presents one component's version as the feature's version unless every component agrees on it. The group descriptions are `%d features available — update check failed`, `%d features available — update check incomplete`, `%d features available — all up to date`, `%d features available (1 update)`, `%d features available (%d updates)`, `%d features available (1 update) — update check incomplete`, and `%d features available (%d updates) — update check incomplete`; the leading `%d features available` fragment is reproduced verbatim from `loadFeatures`' own pre-check string, including its non-pluralized `features`, so only the update tail differs — which is also what makes a completed check that found nothing (`— all up to date`) visibly distinguishable from the pre-check state. `featurestatus_test.go` covers each branch as a table subtest and additionally asserts that the six subtitle branches are pairwise distinct for a fixed feature name, that no subtitle renders a bare `v`, and that the group descriptions are distinct from each other and from the pre-check string.

The package is pure and holds no state, so it is safe to call from a worker goroutine or from inside an `sgtk.RunOnMainThread` closure.

`checkFeatureUpdates` uses this presentation from a worker and publishes through
`sgtk.RunOnMainThread`. Warnings and errors produce an incomplete or failed
description; only a complete successful read may claim all features are current.
It nil-guards the configuration-driven group and missing feature rows.

## Flatpak (`internal/flatpak/flatpak.go`)

Wraps the `flatpak` CLI. Parses tabular (tab-delimited, falling back to whitespace) output.

### Key types

- **`Kind`** — `KindApplication` (`"app"`) or `KindRuntime` (`"runtime"`); the ref shape an entry is, and therefore which `flatpak list` filter reports it
- **`Application`** — name, applicationID, version, installation (user/system), kind
- **`UpdateInfo`** — name, applicationID, newVersion, installation

`--app` and `--runtime` are mutually exclusive `flatpak list` filters: an
application listing never reports a runtime or a runtime extension, and the
reverse holds too. Anything shipped as a runtime extension — the MangoHud
Vulkan layer that `internal/gaming` installs, for one — is therefore
invisible to an application-only inventory no matter how it was installed.
That is why the listing functions come in both shapes and why `Kind` is
stamped from the filter the query was made with rather than read back out of
the `ref` column: a row that falls through to the whitespace-splitting
fallback may not have captured the ref at all, and it still has to be
classified.

### Operations

| Function | CLI command | Timeout | Notes |
|----------|------------|---------|-------|
| `ListUserApplications()` | `flatpak list --user --app --columns=name,application,version` | 30s | Tabular parsed; `--app` excludes runtimes and runtime extensions |
| `ListSystemApplications()` | `flatpak list --system --app --columns=name,application,version` | 30s | Tabular parsed; `--app` excludes runtimes and runtime extensions |
| `ListUserRuntimes()` | `flatpak list --user --runtime --columns=name,application,version` | 30s | Runtimes, SDKs, and runtime extensions only |
| `ListSystemRuntimes()` | `flatpak list --system --runtime --columns=name,application,version` | 30s | Runtimes, SDKs, and runtime extensions only |
| `ListUpdates(ctx, user)` | `flatpak remote-ls --updates --app --columns=name,application,version [--user\|--system]` | 30s, bounded by caller context | Separate calls for user/system; `--app` excludes runtimes; reconciliation is cancellable |
| `Install(appID, user)` | `flatpak install -y [--user\|--system] <appID>` | 30m | State-changing |
| `Uninstall(appID, user)` | `flatpak uninstall -y [--user\|--system] <appID>` | 30m | State-changing |
| `Update(ctx, appID, user)` | `flatpak update -y [--user\|--system] [<appID>]` | 30m (or the caller's, whichever is nearer) | State-changing; empty appID updates all; runs under the caller's context so Update All's cancellation stops it |
| `UninstallUnused()` | `flatpak uninstall --unused -y` | 30m | Maintenance cleanup |

### State-changing commands

`install`, `uninstall`, `remove`, `update` — exactly four keys. As in homebrew, the map selects both the dry-run skip (these are skipped entirely and return a mock message) and the timeout class: `commandTimeout(args []string)` returns `mutationTimeout` (30 minutes) when `args[0]` is one of the four keys and `readTimeout` (30 seconds) otherwise, including for empty args, and `runFlatpakCommand` passes it to `context.WithTimeout`. Flatpak's read budget matches homebrew's 30 seconds, so both wrappers agree. `flatpak_test.go`'s `TestCommandTimeout` iterates the real map and asserts `len(stateChangingCommands) == 4`.

### Error handling

`runFlatpakCommand` is a thin wrapper: it builds a context from `commandTimeout(args)` and delegates to `runFlatpakCommandCtx(ctx context.Context, args ...string) (string, error)`, which applies the dry-run skip (before any `exec.Cmd` exists) and otherwise calls the unexported `runFlatpakCommandAt(ctx context.Context, exe string, args ...string) (string, error)`, always passing `"flatpak"`. The executable path and context are parameters purely so `runner_test.go` can drive a `#!/bin/sh` script from `t.TempDir()` and control the deadline — the same seam `stageexec.Run` gives OS staging and `runBrewCommandAt` gives `internal/homebrew`. `Update(ctx context.Context, appID string, user bool) error` is the one exported function that takes a `context.Context`: it narrows the caller's context with `context.WithTimeout(ctx, mutationTimeout)` — whichever deadline is nearer wins — and passes it to the same `runFlatpakCommandCtx`, mirroring `homebrew.Update`, so Update All's cancellation stops `flatpak update`. Every other exported function gets a deadline, not cancellation, and the single dry-run gate in `runFlatpakCommandCtx` covers both paths. flatpak runs unprivileged; no `pkexec` is involved on this path.

`runFlatpakCommandAt` starts the command in its own process group (`cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}`) and sets `cmd.Cancel` to `syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)`, so flatpak's download helpers (download workers, ostree pulls) are killed with the command instead of being orphaned when only the direct child is signalled. `cmd.WaitDelay` (5s) bounds the wait, because those helpers inherit the stdout/stderr pipes and a straggler would otherwise hold `Wait` open indefinitely. `cmd.Run` still reaps the child. Read-only commands still capture full stdout for parsers. State-changing commands wire stdout and stderr to 64 KiB tail writers instead: successful mutation output is discarded, and failures retain only bounded diagnostic context (stderr, or stdout when stderr is empty).

Failures classify into exactly four distinct outcomes, checked in this order — `errors.Is` against `context.DeadlineExceeded`/`context.Canceled`, never `==` on `ctx.Err()`, so a wrapped cause still classifies:

| Condition | Result |
|-----------|--------|
| The context deadline expired (`commandTimeout(args)` elapsed) | `*Error`, message `Command '<exe> <args>' timed out`, unwrapping to `context.DeadlineExceeded` |
| The context was cancelled by its owner | `*Error`, message `Command '<exe> <args>' was canceled`, unwrapping to `context.Canceled` |
| The command exited non-zero | `*Error` carrying stderr, or bounded stdout when stderr is empty for a state-changing command ("Flatpak command failed: …"), unwrapping to the `*exec.ExitError` |
| The executable is missing (`exec.ErrNotFound` for a bare name, `fs.ErrNotExist` for an explicit path) | `*NotFoundError` ("Flatpak not found…") |

A deadline and a cancellation never produce the same message, and neither surfaces as `signal: killed` — the process is killed by ChairLift's own `Cancel` func, so the raw wait error is replaced by the classified one. `Error` carries an `Err error` field and an `Unwrap() error` method, so `errors.Is(err, context.DeadlineExceeded)` works for callers while `Error` keeps satisfying `error` and keeps its human-readable `Message`; nothing in the views switches on its concrete type. `internal/flatpak/runner_test.go` covers each outcome against a fake script, asserts the deadline and cancellation messages differ and contain no `signal: killed`, proves state-changing output is bounded/discarded, and `TestRunFlatpakCommandAtKillsProcessGroup` proves the process-group kill by having the fake script spawn a background `sleep` and then checking that PID is gone after cancellation.

`flatpak_test.go` separately drives every public CLI wrapper against a fake
`flatpak` on `PATH`, asserting exact user/system arguments, parsing, and query
failure propagation. It table-tests application and update parsing and
iterates all four real state-changing command keys to prove dry-run returns
before execution. This complements runner classification rather than chasing
a percentage in isolation.

### Update queries exclude runtimes

`ListUpdates` builds its argument list with the unexported pure helper
`updateListArgs(user bool) []string`, the only place the `remote-ls` command is
spelled. It passes `--app` — matching the precedent set by `listApplications` —
so runtimes and extensions are deliberately excluded from the results. Update
rows and the sidebar update badge therefore only ever describe applications,
which is what the user can act on from the applications page.

## Developer workstation options (`internal/devtools`)

Features keeps the **Developer Mode** access switch and presents **WSL Mode**,
**Enable Docker**, and an explicit IDE/terminal-editor chooser beneath it,
under the same `features_page` / `dx_group` policy. This is an in-place Bluefin
workstation setup, not a switch to a retired `-dx` image. No editor is installed
by flipping Developer Mode; each optional tool has its own Install action.

`devtools.Tools` follows Common's `ide.Brewfile`: VSCode Stable/Insiders,
VSCodium, Antigravity, one JetBrains Toolbox, Dev Container CLI, Neovim,
Helix and Micro; Vim is also offered by Common's `devmode` wizard. Toolbox's
current cask carries only an x86_64 archive and is unavailable on ARM64.
Installs use the existing typed Homebrew wrapper, tap only `ublue-os/tap`, and
trust only the chosen cask. Formula and cask inventories determine Installed;
a failed inventory is not treated as an absent package.

WSL Mode follows Common's `setup-lima` recipe: check actual `/dev/kvm`
read/write access, install `lima` (Homebrew supplies QEMU), prepend the Lima SSH
Include to the user's SSH config, create `ubuntu` with writable home mounts
from `template:ubuntu-lts`, enable its autostart and probe `limactl shell ubuntu
true`. Running alone never claims shell readiness. The only access mutation
is the fixed `kvm-enable` helper word, invoked only on an explicit enable;
after a group grant the row asks for a new login and remains off. Disable
attempts both autostart removal and stop, never deletes the VM or its data.
A failed stop is followed by a real state read, so a still-running VM stays on.
Lima's `list --json` is a JSON-object stream, not an array, and a successful
empty inventory emits a warning on stderr. Parse stdout only; merging that
warning into JSON would block first-time VM creation. The regression covers
that empty-inventory case. Mutation output retains a bounded diagnostic tail.

Enable Docker installs the CLI, Compose, LazyDocker and Dive in user Homebrew.
It refuses to enable without the base image's Docker daemon. The fixed
`docker-enable` helper grants only the invoking account's Docker group and
enables `docker.socket` and `docker.service`; `docker-disable` stops/disables
both, keeps tools/data and does not remove the user's group. Both fixed unit
names and every argument are helper-owned. Readiness additionally requires
Docker's local socket to answer `/_ping`; CLI presence never counts as ready.
No read asks for a password. Authentication or disable failure is followed by
an unprivileged state read and cannot flip a still-active daemon to off.

All developer controls share `developerGate`, including the initial read and
the finishing state read. Native activity spinners and stage subtitles are
restored on every outcome; widget updates and callbacks remain main-thread.
The editor rows and all signals are built once, with the existing button router.

## Selected gaming components (`internal/gaming`)

**Gaming Mode** lists `gaming.Components()` with unchecked selection controls
and verified per-component user/system installed state. Install Selected only
installs selected missing entries; Remove Selected is confirmed and removes
only selected user-scope entries. Unknown selections fail before any mutation,
duplicates run once, and system copies are never shadowed or removed. Gaming
images use the same verified inventory rather than assuming every optional
component ships. MangoHud is inventoried as a runtime extension, not an app.
Both kinds in both installation scopes must answer before classifying absence.

One `gamingGate` serializes selection, both actions and the finishing refresh.
Per-item failures do not abort other selected entries and each actual failure
is logged. The action summary retains partial success/failure after observed
component subtitles refresh; a failed refresh preserves the last observations
and says so. Dry runs mutate nothing and restore the original summary. No
gaming path adds privilege or removes game data.

## Shared OS stage executor (`internal/stageexec`)

`internal/stageexec` is a pure-Go, widget-free leaf package used by the
fixed bootc staging adapter. `ProgressEvent`, `EventMessage`, and
`EventComplete` are defined once there and exposed from `internal/bootc`
through type/constant aliases, so callers keep a provider-qualified API.

`Run(ctx, progressCh, name, args...)` owns the complete live process contract:
successful exit emits exactly one `EventComplete`; trimmed non-empty stdout and
stderr lines share one ordered `EventMessage` stream; non-zero exit includes the
exit code and last output line and emits no completion; deadline and
cancellation return distinct errors matching their context sentinels without
leaking `signal: killed`; missing bare names and explicit paths return
`*stageexec.NotFoundError`; and every path closes the channel. `DryRun` emits
the synthetic preview plus completion, closes the channel, and constructs no
`exec.Cmd`. `stageexec_test.go` covers each outcome once against local scripts;
the provider tests cover only the fixed-path/dry-run adapter.

Cancellation deliberately kills and reaps only the direct child. The caller
runs through `pkexec`, whose privileged child cannot be signaled by ChairLift as
an unprivileged process group. The unprivileged Homebrew and Flatpak executors
retain their separate process-group-kill behavior.

## bootc (`internal/bootc/`)

Wraps `bootc` for OSTree/composefs system updates, split across two files:
`bootc.go` (unprivileged status reads) and `stage.go` (fixed-path privileged
staging adapter). There is no separate CLI helper binary or Go client library;
status parsing uses `os/exec`, while stage execution delegates to
`internal/stageexec`.

### `GetStatus` and `CheckUpdate` (unprivileged)

Neither read ever uses `pkexec`. bootc 1.16 refuses both `bootc status` and `bootc upgrade --check` to an unprivileged caller ("Querying root privilege: This command must be executed as the root user" — verified 2026-09-26 on a Dakota composefs host, issue #381), so on a composefs host they are answered from world-readable state instead (`internal/bootc/composefs.go`):

| Field | Source |
|-------|--------|
| booted deployment | the `composefs=<id>` kernel argument in `/proc/cmdline` |
| staged deployment | `depl_id` in `/run/composefs/staged-deployment` |
| rollback deployment | the newest other deployment under `/sysroot/state/deploy/` |
| image reference, manifest digest | `/sysroot/state/deploy/<id>/<id>.origin` |
| booted version | `IMAGE_VERSION` (else `VERSION_ID`) in `/usr/lib/os-release` |
| rollback timestamp | the rollback origin file's modification time (its version label is root-only) |

`GetStatus` reads that state through `hostRoot` (an `fs.FS`, `os.DirFS("/")` in production); only a host whose command line names no composefs deployment (`errNotComposefs`) falls back to `getStatusFrom(ctx, bootcCommand)`, which runs `bootc status --format json`. A composefs host whose booted origin cannot be read is an error, not "not bootc". `CheckUpdate` on a composefs host resolves the booted tag through `internal/registrytags` (`registryTag`, the tests' seam) and reports an update when the registry's digest — this platform's child manifest when the tag is an index (`Tag.Platforms`) — is neither the booted nor the staged digest; its version is the manifest's created date (`YYYYMMDD`, how these images are versioned). Other hosts run `bootc upgrade --check`. Staged and rollback versions are not readable without root, so the Updates row reports a staged update without a version (`pageview.SystemVersionRow`'s `staged` flag) and Recovery names the rollback by its deployment date.

`getStatusFrom(ctx context.Context, name string) (*Status, error)` runs `<name> status --format json`, classifies the error, and parses the output. The executable name is a parameter purely so `bootc_test.go` can drive a `#!/bin/sh` script from `t.TempDir()` on a host with no `bootc` installed. The seam is unexported and its only production call site passes the fixed `bootcCommand` constant, so no caller-supplied or user-derived string can reach it.

Failures classify into exactly four distinct outcomes, checked in this order — `errors.Is` against `context.DeadlineExceeded`/`context.Canceled`, never `==` on `ctx.Err()`, so a wrapped cause still classifies:

| Condition | Result |
|-----------|--------|
| The context deadline expired | `*Error`, message `bootc status timed out`, unwrapping to `context.DeadlineExceeded` |
| The context was cancelled by its owner | `*Error`, message `bootc status was canceled`, unwrapping to `context.Canceled` |
| The command exited non-zero | `*Error`, message `bootc status failed (exit N): <stderr>`, unwrapping to the `*exec.ExitError` — matching neither context sentinel |
| The executable is missing (`exec.ErrNotFound` for a bare name, `fs.ErrNotExist` for an explicit path) | `*NotFoundError` (`bootc not found`) |

The deadline and cancellation messages differ, and neither surfaces as `signal: killed`: `exec.CommandContext` kills the child when the context ends, so the classified error replaces the raw wait error. `bootc.Error` carries an `Err error` field and an `Unwrap() error` method — matching `internal/homebrew` and `internal/flatpak` — so callers distinguish all four outcomes with `errors.Is`/`errors.As` while `Error` keeps its human-readable `Message`. `bootc_test.go` covers all four against fake scripts (plus the success parse), so the whole classification is exercised without a real `bootc` on the host.

### Boot gate semantics

`bootc status` exits 0 with a null `booted` field on hosts that aren't running a bootc deployment at all — so the gate cannot be the exit code. `Status.Booted()` returns `s.Status.Booted != nil`. `IsBootcBooted(ctx)` calls `GetStatus` and returns that boolean (treating any error as "not booted"). `IsBootcBootedCached()` wraps it in a `sync.Once` with a 5s timeout, computing the result once and caching it for the lifetime of the process — this lets multiple view goroutines call it during async startup without triggering redundant `bootc` invocations. **Do not use `/run/ostree-booted`** as a substitute gate: it is absent on composefs deployments (Dakota, snow), so checking for it would hide bootc UI on those hosts; the composefs reader keys on the `composefs=` kernel argument instead.

### `StageUpdate` (privileged, streaming)

`StageUpdate(ctx, progressCh)` (`internal/bootc/stage.go`) retains the fixed
`pkexec /usr/libexec/bootc-update-stage` command and adapts
`stageexec.Run`/`DryRun` errors back to `bootc.Error` and `bootc.NotFoundError`.
Its progress types are aliases of the shared contract.

Failures classify into exactly four distinct outcomes, checked in this order — `errors.Is` against `context.DeadlineExceeded`/`context.Canceled`, never `==` on `ctx.Err()`:

| Condition | Result |
|-----------|--------|
| The context deadline expired | `*Error`, message `Update staging timed out`, unwrapping to `context.DeadlineExceeded` |
| The context was cancelled by its owner | `*Error`, message `Update staging was canceled`, unwrapping to `context.Canceled` |
| The script exited non-zero | `*Error`, message `update staging failed (exit N): <last output line>`, unwrapping to the `*exec.ExitError` — matching neither context sentinel |
| `pkexec` itself is missing | `*NotFoundError` (`pkexec not found`) |

The deadline and cancellation messages differ, and neither surfaces as
`signal: killed`. Exhaustive process tests live in
`internal/stageexec/stageexec_test.go`; `bootc/stage_test.go` checks the
fixed-path dry-run adapter without a real `pkexec` or `bootc`.

The direct-child cancellation rationale is owned by `internal/stageexec` above.
`getStatusFrom` is separate: `bootc status` is unprivileged and short-lived, so
`exec.CommandContext`'s default direct-child kill suffices.

**Why a stage script instead of `bootc upgrade`:** upstream `bootc upgrade`'s registry-transport pull fails on snow's composefs images. The stage script works around this by using `podman pull` (whose pull path works) to fetch the image into containers-storage, then running `bootc switch --transport containers-storage` to stage the already-pulled image — `podman` does the pull, `bootc` does the switch. This keeps the actual workaround logic in one place (the OS-shipped script) instead of duplicating pull/switch orchestration inside ChairLift. The script is idempotent: it exits 0 without staging anything when the deployment is already current, so `StageUpdate` doubles as both "check for update" and "apply update".

### Event types

- `EventMessage` — one line of stage-script output
- `EventComplete` — sent once, after successful completion

This is intentionally flatter than a step/percent progress model, because the
stage script emits unstructured log lines, not a structured progress protocol.
Failures are returned by `StageUpdate` and handled once by the view after the
channel closes; there is no error event duplicating that path.

### Dry-run behavior

Unlike bootc's own dry-run flag (not used here), ChairLift's dry-run mode is handled entirely inside `StageUpdate`: if `dryRun` is set, it never invokes `pkexec` at all — it logs the command that would run, sends a synthetic `EventMessage` + `EventComplete`, closes the channel, and returns `nil`.

That part was already correct and already tested (`internal/bootc/stage_test.go`). What used to be wrong is downstream, in the view layer: `onBootcStageClicked` (`internal/views/updates_page.go`) always re-reads live `bootc.GetStatus()` after `StageUpdate` returns and used to show one of two completion-toned toasts — `"System update staged. Restart to apply."` or `"System is up to date"` — regardless of whether the click was a real stage or a dry-run no-op. Neither string said "preview", and `"System is up to date"` in particular read as a verified conclusion when, under dry-run, this click didn't actually check or change anything. The handler now computes `actionmsg.BootcStage(dryrun.Enabled(), staged)` for that toast: under dry-run it returns a single, unambiguous preview string regardless of `staged` (since `staged` reflects real system state from `GetStatus`, not anything this click did); otherwise it returns the same two completion strings as before. The `bootcStageExpander` subtitle is deliberately *not* changed by this — it intentionally keeps reflecting live `GetStatus()` output in both dry-run and live mode, since the subtitle is a persistent status display (what state the system is actually in right now), not a per-click completion claim. Only the toast, which is inherently about "what did this click just do", needed the dry-run-aware text.

### Operations

| Function | Command | Privilege | Timeout | Notes |
|----------|---------|-----------|---------|-------|
| `GetStatus(ctx)` | `bootc status --format json` | none | 30min (`DefaultContext`); views use the standard 30min context | JSON parsed into `Status` |
| `IsBootcBooted(ctx)` / `IsBootcBootedCached()` | (calls `GetStatus`) | none | 5s (cached variant) | Boot gate; cached variant memoizes via `sync.Once` |
| `StageUpdate(ctx, progressCh)` | `pkexec /usr/libexec/bootc-update-stage` | pkexec (`io.projectbluefin.chairlift.bootc.stage`) | 30min (`DefaultContext`) | Streaming; idempotent; dry-run aware |
| `StageScriptAvailable()` | `os.Stat(StageScriptPath)` | none | — | Used to hide the updates-page group when the script isn't installed |

### Streaming pattern

```go
progressCh := make(chan bootc.ProgressEvent)
go func() {
    err := bootc.StageUpdate(ctx, progressCh)
    // channel is closed when done
}()
for event := range progressCh {
    evt := event // capture for closure
    sgtk.RunOnMainThread(func() {
        switch evt.Type {
        case bootc.EventMessage:
            // append to log expander with timestamp
        case bootc.EventComplete:
            // mark streamed activity complete
        }
    })
}
// After the channel closes, handle the returned error or re-query GetStatus.
```

### Progress UI (`internal/views/updates_page.go`)

`onBootcStageClicked` is the dedicated **Download system update** secondary
action, admitted by the same shell mutation owner as Update All. It runs the
fixed stage helper away from GTK and renders its bounded output through
`stageProgressSink`. Compare stays visible outside the download disclosure;
only optional output/diff details expand. Completion re-reads status before
claiming a staged update and requests the unified check after a live success.

## Dated-build registry catalog (`internal/registrytags`)

`internal/registrytags` reads the tags an OCI registry publishes for one
repository and turns them into the dated builds a rollback or pin surface
would render. It is a pure-Go, puregotk-free leaf package with no privileged
surface: it takes no argument that reaches `pkexec` and returns nothing that
is handed to it. [ADR-0013](../adr/0013-rollback-catalog-reads-the-registry-live.md)
records why this reads the registry live rather than extending
`internal/imageinfo`'s hand-verified tables (ADR-0011), which stay the only
authority on a `bootc switch` target.

The registry reference is `registry/path` — the same spelling
`imageinfo.KnownImages()` returns — and it is validated before either half
reaches a URL: `repositoryPattern` rejects a host carrying a scheme,
userinfo, or a path separator, and each repository path segment must match
the distribution specification's component grammar. A reference carrying a
tag or digest is rejected rather than stripped, because stripping is how
`ghcr.io/ublue-os/bluefin:stable` would become a listing of a repository the
caller did not name.

Four exported entry points, each with one job:

- `Client.Tags(ctx, repository)` returns every tag the repository publishes.
  It follows the response's `Link: rel="next"` header verbatim rather than
  composing the next URL. Verified 2026-09-22: GHCR answers a 100-tag page
  with `/v2/<repo>/tags/list?last=…&n=0`, so the page size is the registry's
  to choose. A client that appends its own `n=` or rebuilds `last` from the
  tags it has seen either re-reads a page or stops early; the loop is bounded
  by `maxPages` so a registry that always sends a next link cannot spin it.
- `Client.Tag(ctx, repository, tag)` resolves one tag to its
  `Docker-Content-Digest` and its `org.opencontainers.image.created`
  annotation. It returns `ErrUnknownTag` when the registry answers 404, and a
  zero `Created` with no error when the tag exists but carries no annotation
  — signature tags (`sha256-<hex>.sig`) and architecture-suffixed stream tags
  (`lts-amd64`) are the second case, verified 2026-09-22, and "this tag is
  not a build" is an answer rather than a failure to answer. The annotation
  is read from whichever shape the registry returns, index or single-arch
  manifest: `stable-20260623` is served as an OCI image manifest and
  `lts-testing.20260621` as an index, and both carry the date, so a client
  that reads only one shape reports "no date" for half the family. The
  response's `Content-Type` header is the authority on which arrived, not the
  document's own `mediaType` field — GHCR omits that field entirely on the
  manifest it serves for a dated tag.
- `ParseBuild(tag)` reports whether a tag names a dated build. Both
  separators are in use and both spell the same build
  (`lts-testing.20260621` and `lts-testing-20260621`); the eight trailing
  digits must form a real calendar date, which is what excludes the signature
  and architecture-suffixed populations. The grammar is exact rather than
  greedy, so an unrecognized shape resolves to "no date" and the caller shows
  nothing rather than a day it inferred.
- `Builds(tags, since)` filters to the builds at or after `since`, newest
  first and in tag order within a day, so the order is total and a re-render
  does not shuffle the list. It returns every alias rather than collapsing
  them: verified 2026-09-22, `stable-20260623`, `44.20260623`,
  `stable-44.20260623`, `stable-daily-20260623`, `stable-daily-44.20260623`,
  `gts-20260623` and `gts-44.20260623` all resolve to the same digest and
  creation time, and which spelling to show depends on the stream the machine
  is booted into.

`Catalog` is the cache, and it is the only stateful part of the package. It
holds each repository's tag listing for `DefaultTTL` (15 minutes) and each
resolved tag for the same, bounded by `DefaultMaxEntries` (256) with the
oldest answer evicted at capacity, so a long-running window cannot grow
without bound. `Now` injects the clock for the TTL. A failed read is never
cached and there is no stale fallback: the catalog exists to show the
registry's current state, so an error is returned to the caller to render
rather than replaced with the last answer that worked. `Catalog` is safe for
concurrent use, because the pages that read it run their registry work off
the GTK main thread.

Every request goes through `Client.HTTP`, and a nil `HTTP` uses a client with
a timeout rather than `http.DefaultClient`, which has none. That is the same
seam `internal/sbom` uses, and it is what keeps the gated tests off the
network: `registrytags_test.go` drives a loopback `httptest` registry that
models GHCR's Link-header pagination, its Content-Type-only manifest media
type, and its 404 `MANIFEST_UNKNOWN` body.

### Privileged pin and unpin

`ublue.Pin(ctx, day)` and `ublue.Unpin(ctx)` dispatch through `runHelper`,
including its unconditional journal and dry-run handling. The fixed
`/usr/bin/chairlift-helper` accepts `pin <YYYYMMDD> [--dry-run]` and
`unpin [--dry-run]`. The day must be eight ASCII digits naming a real date
no later than today UTC. No image reference crosses pkexec (ADR-0001).
Recovery's selection UI is separate work in #360.

`ubluehelper.PinArgs` owns the derivation and resolver seam required by
[ADR-0017](../adr/0017-pin-through-a-validated-day-word.md). It recovers the
stream from the booted tag with `registrytags.ParseBuild`, or uses a plain
stream tag as-is, and requires `imageinfo.KnownStream` for the descriptor's
`CleanRef()`. Pin tries `<stream>-<day>` before `<stream>.<day>`; only
`ErrUnknownTag` permits the second lookup. Unpin requires a dated booted tag
and verifies `<stream>`. Both call only `Client.Tag`, never a listing or
catalog cache, and discard all returned registry strings. Missing builds,
registry failures, and timeouts return no command argv. Successful targets
retain `bootc switch --enforce-container-sigpolicy`.

Dry runs derive and print the first candidate without contacting the registry;
the preview is not evidence that the tag exists. Both commands fail closed
when the system channel table cannot load. Their two PolicyKit actions use
`auth_admin` / `auth_admin` / `auth_admin_keep` and the existing fixed helper
path. They ship in the existing ublue policy through `make install` and the
Homebrew release archive; there are no nFPM packages.

## Updex (`internal/updex/updex.go`)

Manages system features (add-on software/configuration modules). Unlike other wrappers, updex does **not** shell out to a CLI for reads. It uses the `github.com/frostyard/updex/updex` Go library directly for read operations, with a singleton `*updexapi.Client`. Write operations that require root are delegated via pkexec to the fixed absolute path `internal/updex.HelperPath` (`/usr/bin/chairlift-updex-helper`, built from `cmd/chairlift-updex-helper/main.go`) — never a bare, `$PATH`-resolved name, since `pkexec` matches the resolved absolute path against `data/io.projectbluefin.chairlift.updex.policy`'s `org.freedesktop.policykit.exec.path` annotation to select the right action; see [overview.md](./overview.md#privileged-operations) for the full rationale and the matching `PREFIX=/usr` Makefile requirement.

### Key types

Type aliases to `github.com/frostyard/updex/updex`:
- **`Feature`** (`FeatureInfo`) — name, description, enabled flag, documentation URL
- **`FeatureCheck`** (`CheckFeaturesResult`) — feature name plus `Results []CheckResult`, one entry per component, each carrying its own update-available flag and versions. There is no feature-level update flag: a feature has an update when *any* of its components does (see [`internal/views/featurestatus`](#view-layer-feature-update-status-internalviewsfeaturestatus))
- **`CheckResult`** — component name, current/available versions

### Operations

| Function | Implementation | Mode | Timeout | Notes |
|----------|---------------|------|---------|-------|
| `IsInstalled()` | Go library: `client.Features()` | Direct | 3s | Checks if updex features are configured (requires `err == nil` and `len(features) > 0`) |
| `IsInstalledCached()` | Cached `IsInstalled()` | Direct | — | `sync.Once`, runs check at most once |
| `ListFeatures()` | Go library: `client.Features()` | Direct | 5min | Returns `[]Feature` |
| `CheckFeatures()` | Go library: `client.CheckFeatures()` | Direct | 5min | Returns `([]FeatureCheck, []string, error)` (retains warnings) |
| `EnableFeature(name)` | `pkexec /usr/bin/chairlift-updex-helper enable-feature <name>` | pkexec | 5min | State-changing |
| `DisableFeature(name)` | `pkexec /usr/bin/chairlift-updex-helper disable-feature <name>` | pkexec | 5min | State-changing |
| `UpdateFeatures()` | `pkexec /usr/bin/chairlift-updex-helper update` | pkexec | 5min | Downloads enabled features |

`updex_test.go` drives all three public write operations through a fake
`pkexec` on `PATH` and asserts that argv always begins with the fixed
`HelperPath`, followed by the exact subcommand/name shape. It also covers
missing-pkexec, non-zero/stderr, timeout, default-context, and wrapper dry-run
outcomes. Multi-component update aggregation remains in the pure
`internal/views/featurestatus` package, where every result is inspected and
the full decision table is tested without constructing GTK widgets.

### Helper binary (`cmd/chairlift-updex-helper/main.go`)

A small standalone binary that accepts commands (`enable-feature`, `disable-feature`, `update`) and uses the updex Go library to perform privileged operations. It supports `--dry-run` for all three subcommands — `enable-feature`, `disable-feature`, and `update` — passing it through to the corresponding `updex.*Options.DryRun` field. Outputs JSON to stdout. Invoked via pkexec so that the main chairlift process does not need root.

`main.go` itself is thin dispatch only: strict `os.Args` parsing and each
subcommand's `Options` struct live in `internal/updexhelper`
(`internal/updexhelper/updexhelper.go`), a package with no puregotk import —
only stdlib plus `github.com/frostyard/updex/updex`. That's what makes the
logic testable at all: neither `gates_chunk` nor `make ci` ever runs `go test
./...`, both are scoped to `go test ./internal/...`, so a `_test.go` under
`cmd/chairlift-updex-helper` would never execute under any gate this repo
actually runs. `ParseInvocation` accepts only `enable-feature <name>
[--dry-run]`, `disable-feature <name> [--dry-run]`, and `update [--dry-run]`;
`SupportedCommands` is the complete first-argument surface matched by the
PolicyKit actions. `EnableOptions`, `DisableOptions`, and `UpdateOptions` set
`DryRun` exactly. Tests cover every accepted/rejected argv shape, the immutable
command inventory, and all three option builders.

## Cross-cutting: dry-run

Every wrapper reads the single process-wide flag, `dryrun.Enabled()`
(`internal/dryrun/dryrun.go`); none carries a package-level flag of its own.
Behavior varies by wrapper:

| Wrapper | Dry-run behavior |
|---------|-----------------|
| Homebrew | Skips state-changing commands, returns mock message |
| Flatpak | Skips state-changing commands, returns mock message |
| bootc | `StageUpdate` never invokes pkexec; emits synthetic `EventMessage`+`EventComplete` and returns. The Updates page's stage button shows an explicit `actionmsg.SystemStage(dryrun.Enabled(), staged)` preview toast, distinct from its normal staged/up-to-date toasts; the expander subtitle intentionally stays live (from `bootc.GetStatus()`) in both modes |
| Updex | Skips helper execution, returns empty results; the helper binary itself (`cmd/chairlift-updex-helper`, via `internal/updexhelper`) also honors `--dry-run` for all three subcommands, defense-in-depth even though `updex.runHelper` never invokes pkexec under dry-run |
| views (custom maintenance scripts) | `runMaintenanceAction` never constructs an `exec.Cmd` (no `pkexec`, no direct script exec); logs `[DRY-RUN] Would execute: ...` instead |

Custom maintenance scripts (config.yml `actions` entries) have no wrapper package of their own, so `internal/views` reads `dryrun.Enabled()` directly (`internal/views/maintenance_page.go:242`) rather than keeping a flag of its own. Unlike the other wrappers, the execution gate for this one is not just an `if dryrun.Enabled()` branch inline in the view: `internal/views/actionmsg.MaintenanceScript(dryRun, title)` returns a `ScriptDecision{Execute, Toast}` computed once, before the goroutine spawns, and both the "does it execute" question and the toast text come from that single tested function call — not two independently-maintained conditionals. See "View-layer toast and decision helpers" above for the full `actionmsg`/`trustmsg` function and type list.

The Applications page's configured Brew bundle rows use the same paired
decision pattern. `homebrew.BundleInstall` already skips `brew bundle install`
under dry-run because `bundle` is state-changing, but a nil wrapper error does
not mean the row should read `Installed`: `actionmsg.BundleInstall` returns
`BundleInstallDecision{Complete: false, Toast: <preview>}` for that outcome,
and the callback resets both its `bundleview.InstallGate` and button. A live
success returns `Complete: true`, permanently completes the gate, labels the
button `Installed`, and leaves it insensitive. A command failure resets the
gate and button without showing a success/preview toast. `TryStart` and
`SetSensitive(false)` both happen before the worker goroutine starts, so
repeated callbacks cannot overlap an install.

The Applications page's per-result Homebrew install button
(`onHomebrewSearch`, `internal/views/applications_page.go`) shows toasts built
by `actionmsg.Install(dryRun, result.Name)` rather than unconditional
completion claims. It restores its install control after a dry-run and does
not refresh, because nothing changed; only a live success completes the
control and starts the generation-guarded installed-package refresh described
above. The search entry itself carries `pageview.HomebrewSearchPlaceholder` as
its placeholder and `pageview.HomebrewSearchLabel` as its accessible label,
because the row it sits in is titled only "Search" and assistive technology
does not associate that title with the entry.

Installed Homebrew formula/cask rows follow the same decision path for
uninstall, and formula rows add pin/unpin. They confirm before starting,
disable every mutation control on the row while the worker runs, use
`actionmsg.Uninstall`/`actionmsg.Pin` for live versus preview wording, restore
on failure or dry-run, and refresh the installed inventory only after live
success. New Flatpak discovery and install deliberately remain in the
configured external manager; ChairLift's direct Flatpak UI lists and
uninstalls installed applications.

Flatpak uninstall (`loadFlatpakApplications`, both user and system scopes)
now takes the same shape. Each row's trash button owns one
`actionstate.Gate`; a click that wins `TryStart` opens
`confirmFlatpakUninstall`, an `AdwAlertDialog` whose title and body come from
`pageview.FlatpakUninstallConfirmation(name, userScope)` — the system-scope
body says the app is removed for everyone who uses the computer and that an
administrator password may be requested. Cancel resets the gate and runs
nothing. Confirm makes the button insensitive and runs `runFlatpakUninstall`
off the main thread, which decides through `actionstate.PackageUninstall`:
a failure or dry-run preview resets the gate and restores the button while
keeping the known rows, and only a live success completes the gate and starts
a list refresh. The toast is `actionmsg.Uninstall(dryRun, name)` on success
or preview, and an error toast on failure. `runFlatpakUninstall` is in
`internal/installcheck`'s `TestDestructiveActionsRequireConfirmation`
inventory beside `runPowerwash` and `runFactoryReset`: any function in
`internal/views` that calls it must also build an `AdwAlertDialog` and gate on
the `"confirm"` response, or `make ci` fails (#353).

The Flatpak loader keeps both scopes in one expander and one
`uh.flatpakRows` tracker (`rowset.Tracker`), guarded by the
`flatpakPackagesRefresh` generation; a successful load clears that tracker
and rebuilds it inside one main-thread closure, and a stale generation's
result is dropped. Homebrew's installed formula/cask loader uses the same
clear-before-repopulate pattern with separate `formulaeRows`/`caskRows`
trackers and its own refresh generation, because multiple Homebrew actions can
request overlapping reloads. Error branches change the subtitle ("Could not
read the list") and preserve the last known rows.

Individual updates and tool metadata refresh now live in the unified shell's
visible rows. They share the shell's mutation admission with Update All and
dedicated staging rather than each maintaining an independent gate, inventory
and badge. Preview feedback still comes from `actionmsg.Update`, `Upgrade` and
`SelfUpdate`, but inventory changes follow verified provider outcomes only.
Errors and unchanged outcomes leave controls retryable and known items/counts
intact; successful live actions request the coordinator's shared refresh.
An Update All provider that returns `Changed=false` without a preview retains
its pending items and records a retryable apply failure. It is counted in
`FailedSources`, so the page and desktop notification never report an
unapplied run as complete or the system as current.


The Updates page's bootc "Check for Updates" stage button (`onBootcStageClicked`, `internal/views/updates_page.go`) follows the same `actionmsg` pattern, with one difference from the buttons above: unlike `Install`/`Upgrade`/etc., whose completion text is selected purely by `dryrun.Enabled()`, `SystemStage(dryrun.Enabled(), staged)` also takes the live `staged` result from the post-`wg.Wait()` `bootc.GetStatus()` re-read, because the non-dry-run branch still needs to pick between the "staged" and "up to date" strings. Under dry-run, `staged` is ignored entirely and a single preview string is returned instead — see "Dry-run behavior" under bootc above for why. The expander's `SetSubtitle` calls in the same code block are *not* routed through `actionmsg`; they keep reading live `GetStatus()` output unconditionally, since the subtitle is a persistent status display rather than a per-click completion claim.

The Features page's per-feature switch (`onFeatureToggled`, `internal/views/features_page.go`) follows the same decision-struct pattern as maintenance-script execution and tap trust: on a successful `updex.EnableFeature`/`DisableFeature` call, `decision := actionmsg.FeatureToggle(dryrun.Enabled(), enabled, name)` is computed once, and the switch's visual state is driven solely by `decision.Confirm` — `toggle.SetActive(enabled)` (confirming the flip) when `Confirm` is true, `toggle.SetActive(!enabled)` (reverting to the pre-click state) when it is false. Under dry-run, `updex.runHelper` returns before ever invoking pkexec, so nothing was actually toggled and the switch must not visually confirm a change that did not happen — this is the other "switch/list implies a state change after a preview" bug (the tap-trust row-removal case is the same pattern in Homebrew's Untrusted Taps list). The Update button (`onUpdateFeaturesClicked`) has no equivalent mutation to gate — its `SetSensitive`/`SetLabel` reset is unconditional in both modes — so its toast is a plain string, `actionmsg.FeatureUpdate(dryrun.Enabled())`.

## Install-path consistency (`internal/installcheck`)

The source `make install` path (Makefile, `PREFIX` defaulting to `/usr`) and
the release archive GoReleaser builds from `.goreleaser.yaml` ship this
repository's privileged surface and maintainer configuration. ChairLift is
distributed only through Homebrew: the cask installs that archive,
`chairlift_<version>_linux_<arch>.tar.gz`, and there are no deb, rpm, or apk
packages. They are hand-maintained text — a Makefile recipe and a YAML file
list — with no shared code path, so nothing stops them (or
`internal/updex.HelperPath` and `internal/ublue.HelperPath`, the fixed absolute
paths `pkexec` matches against the policies' `exec.path` annotations) from
silently drifting apart.

ChairLift ships the bootc, updex, and ublue `.policy` files. It
no longer ships its old `.rules` files, which returned `YES` for every active
local member of the `sudo` group and bypassed authentication. Source
installation explicitly removes those legacy rule paths. Every policy uses normal
administrator authentication. The updex and ublue policies select one action for
each supported first argument, and the helpers validate the complete argv shape.

`make install` installs the repository's `config.yml` as maintainer defaults at
`/usr/share/chairlift/config.yml`, and never installs
`/etc/chairlift/config.yml`: that higher-precedence path belongs to the
administrator and must survive installation and upgrades unchanged.

The Homebrew cask installs in user scope and cannot place root-owned files, so
the release archive also carries the privileged pieces: both helper binaries
(`chairlift-updex-helper`, `chairlift-helper`), the three PolicyKit
policies in `data/`, `config.yml`, and `channels.example.yml`. An OS image that
wants the privileged features installs, from that archive, the helpers at
`/usr/bin/chairlift-updex-helper` and `/usr/bin/chairlift-helper` and the
policies at
`/usr/share/polkit-1/actions/io.projectbluefin.chairlift.bootc.policy`,
`/usr/share/polkit-1/actions/io.projectbluefin.chairlift.updex.policy`, and
`/usr/share/polkit-1/actions/io.projectbluefin.chairlift.ublue.policy`, and
provides its own trusted stager at `/usr/libexec/bootc-update-stage`; ChairLift
does not ship one. The two-package split ADR-0006 recorded is superseded.

`internal/installcheck` holds regression tests, not production code, that turn
"verified by inspection" into real, gated checks. The first two guard the
installed layout and the archive's inventory:

- **`TestMakefileInstallUsesUsrPrefix`** runs `make -n install
  DESTDIR=<t.TempDir()>` — a dry run, so no compilation, no writes outside
  the temp dir, and no root — once with no `PREFIX` override and once with
  `PREFIX=/usr`, and asserts the printed `install -Dm...` lines place both
  helper binaries under `DESTDIR/usr/bin` and every policy under the
  fixed `/usr/share/polkit-1/actions` directory PolicyKit reads,
  removes both legacy rules from `DESTDIR/usr/share/polkit-1/rules.d`,
  installs maintainer defaults at
  `DESTDIR/usr/share/chairlift/config.yml`, and never targets the
  administrator-owned `/etc/chairlift/config.yml`. It shells out to
  the real `make` rather than parsing the Makefile textually because `make`
  itself is the authority on what a given `PREFIX`/`DESTDIR` combination
  actually resolves to (variable derivation, `$(DESTDIR)$(BINDIR)`
  concatenation, recipe ordering) — a hand-rolled Makefile parser would just
  be a second, divergence-prone implementation of `make`'s own substitution
  rules, and would stop being a regression test for the exact thing that
  broke (the *installed* path) the moment it disagreed with real `make`
  output. If `make` is not installed, the check skips with an explicit
  diagnostic; when `make` is available, command or layout failures remain hard
  failures.
- **`TestGoreleaserArchivesCarryTheInstallSurface`** parses the real,
  repo-root `.goreleaser.yaml` (not a fixture) with the already-vendored
  `gopkg.in/yaml.v3` and, iterating **every** `archives[]` entry (not just
  `archives[0]`, so adding or reordering a second archive that drops a file
  still fails — per `docs/skills/collection-regressions/SKILL.md`), asserts
  each archive carries the GUI and both helpers — built under the exact file
  names `filepath.Base(updex.HelperPath)` and `filepath.Base(ublue.HelperPath)`
  and not filtered out by the archive's `ids` — plus `LICENSE`, `config.yml`,
  `channels.example.yml`, the wrapper, the desktop entry, and all three
  policies. It rejects any `.rules` file and any live channel table in an
  archive. `TestGoreleaserArchivesShipAllSchemas` holds every archive to every
  `data/*.gschema.xml`, and `TestEveryCommittedExampleConfigIsShipped` holds
  both the Makefile and every archive to each committed example config.

A regression test named `TestGoreleaserPublishesSystemIntegrationPackage`
once guarded the retired package split. `Integration` in its name matched the
`-skip "Integration"` half of the filter described below, so the filtered
unit-test step never selected it; `internal/installcheck`'s
`TestNoInternalTestNameIsExcludedByTheCIFilter` now rejects any test under
`internal/` that the filter would drop, so no other gate can be silently
inert the same way.

These tests fail — not skip — if the helper constants, the Makefile's
`PREFIX` default, or `.goreleaser.yaml`'s archive file list change
independently of one another. The package imports no puregotk, directly or
transitively, so it never trips `docs/skills/gtk-headless-testing/SKILL.md`'s
constraint, and it lives under `internal/...` so `gates_chunk`, `make ci`, and
CI's identical `go test ./internal/... -run "^Test[^I]" -skip "Integration"`
filter all exercise it on every run, per
`docs/skills/gated-test-placement/SKILL.md` — not just the
heavier, less-frequent `make ci` deep gate.

The license travels with the archive as the `LICENSE` file, whose text is
GPLv3 and matches `internal/window/window.go`'s about dialog
(`gtk.LicenseGpl30Value` — puregotk's "GPL 3.0 or later" enum value, distinct
from `LicenseGpl30OnlyValue`). GoReleaser OSS has no `metadata.license`
field, so there is no second license string in `.goreleaser.yaml` to drift.

A further regression test guards the **repository URL**. GoReleaser OSS
(unlike Pro) has no global `metadata:`
block and therefore no `metadata.homepage` to template a field from, so
`.goreleaser.yaml`'s `release.footer` carries the repository URL as a literal
in its "Full Changelog" line — `https://github.com/projectbluefin/chairlift`
plus the `/compare/{{ .PreviousTag }}...{{ .Tag }}` suffix. That literal text
is the **single source of truth** for the repository URL: there is no second,
disagreeing copy of it anywhere else in the config. The footer keeps the
`/compare/{{ .PreviousTag }}...{{ .Tag }}` suffix for the release-note
comparison link, and `{{ .ProjectName }}` is deliberately **not** concatenated
onto it — the homepage already ends in the repository name, so appending the
project name would produce a doubled path. The regression this guards: the
footer used to hardcode the repository's previous owner in that URL while the
package still lived under the current one, so a single file identified one
repository two disagreeing ways and every generated release note's Full
Changelog link pointed at the wrong owner. Keeping the URL as the single
literal in the footer removes the duplicate rather than policing it:

- **`TestGoreleaserReleaseFooterHasCanonicalRepoURL`** asserts the parsed
  `release.footer` has exactly one "Full Changelog" line, that it contains the
  canonical repository URL `https://github.com/projectbluefin/chairlift`, that
  it keeps the `/compare/{{ .PreviousTag }}...{{ .Tag }}` suffix, and that it
  contains no `.ProjectName`. An absent or empty footer, or any line count
  other than exactly one "Full Changelog" line, is a `t.Fatal`, not a silent
  pass, so the test cannot succeed vacuously against a config whose footer was
  deleted.

This asserts on the **template text** parsed out of the YAML — never a
rendered value. The footer is a Go template expanded by GoReleaser only at
release time, and GoReleaser OSS (this config sets no `pro:` block and no
`nightly:` block) is not installed on the gate host or in `make ci`; it runs
only in `.github/workflows/release.yml` via `goreleaser-action` with the
default `GITHUB_TOKEN`, which is enough to publish binaries and the release
archives straight to the GitHub Release for the tagged
commit — no Pro license or `GORELEASER_KEY` secret. Snapshot output is
governed separately by `.goreleaser.yaml`'s `snapshot:` block
(`version_template: "{{ .ShortCommit }}-snapshot"`), which sets the version
template for local `goreleaser release --snapshot` builds; it needs no credentials
and no workflow, so it is not gated here. `goreleaser check` is therefore
deliberately not run anywhere — locally, in `gates_chunk`, or in `make ci` —
and the test neither shells out nor renders anything. `ReleaseConfig.Footer`
in `internal/installcheck/installcheck.go` exists solely so `yaml.Unmarshal`
has somewhere to put the footer value; there is no
`MetadataConfig.Homepage`, because GoReleaser OSS exposes no `metadata.homepage`
— without the struct field yaml.v3 drops the footer and the test would pass
vacuously regardless of what the YAML says.

> **Switch to plain GitHub Releases.** This repository previously shipped its
> releases through GoReleaser Pro — a `pro: true` block, a `nightly:` block,
> a `metadata.homepage` templated into `release.footer`, a separate
> `.github/workflows/snapshot.yml`, and a `GORELEASER_KEY` secret. That
> configuration no longer exists; the live config is GoReleaser OSS with a
> hardcoded canonical URL in `release.footer`, `GITHUB_TOKEN` in
> `.github/workflows/release.yml`, and a local `snapshot:` block. The tests and
> structs above were rewritten to assert the OSS layout rather than the retired Pro one.

Three gates in `navigationschema_test.go` close the page/group contract's last
unenforced edge. `internal/config` owns the page/group grammar — it derives it
by reflection from `Config`'s yaml tags and `defaultConfig()` and publishes it as
`config.SchemaPages()` / `config.SchemaGroups(page)` — while `internal/navigation`
restates the same grammar as the `Refs` of its canonical route inventory. A `Ref`
is **page-qualified** — a `{Page, Group}` pair rather than a group under a
route-owned page field — because one rendered destination can consume several
configuration namespaces at once. The live case is the Recovery detail, whose
rollback controls are gated by `bootc_updates_group` on `updates_page` while its
reset controls are gated by `reset_group` on `maintenance_page`; a single page
field per route could not express that pair, and the alternative of inferring a
route's page from its display name is exactly the assumption the page-qualified
ref retires. **`TestNavigationPagesMatchConfigSchema`** holds the distinct config
pages the sidebar inventory consumes to `config.SchemaPages()` as a bijection,
rejecting an unqualified or undeclared ref and two primaries claiming one config
page; it deduplicates a single route's own repeated page first, because one route
consuming several groups on one page is the normal case and only two *different*
routes claiming one page is drift. **`TestNavigationGroupsMatchConfigSchema`**
holds the union of groups the inventory claims per page to
`config.SchemaGroups(page)` as set equality, rejecting a group claimed twice.
**`TestEveryNavigationRefNamesADeclaredGroup`** extends both checks to the detail
routes — it is the cross-namespace edge, so a detail that drew a group from the
wrong namespace fails here rather than silently gating itself on a pair nothing
else recognizes — and refuses to pass vacuously while the inventory declares no
detail at all. All three compare sets, not order: `config.SchemaGroups` sorts its
result, while navigation's slices carry sidebar presentation order, which is
navigation's own concern.

`capabilityschema_test.go` holds the page/group contract's third copy to the
same owner. `internal/capability` classifies every configurable group in its
prerequisites table — the host tool or asset that group needs, and the
`Supports` predicate
`internal/navigation.VisibleItems` accepts — and its failure mode is quieter
than navigation's: `Supports` reports an *unclassified* pair as supported, so
that a missing entry cannot silently hide a group at runtime. A group added to
`config.yml` and wired into a view would therefore render on hosts whose
backing tool is absent, with the application building and every other test
passing. **`TestCapabilityPrerequisitesMatchConfigSchema`** holds
`capability.Prerequisites()` and the `config.SchemaPages()`/`config.SchemaGroups(page)`
pairs to set equality in both directions, and
**`TestEveryConfigurableGroupIsClassifiedOnce`** reports the two failure shapes
separately — a group the table does not classify, and a pair it classifies
twice — because they need different fixes.
