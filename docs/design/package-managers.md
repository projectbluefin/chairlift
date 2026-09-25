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
`brew_group`, so this path never assumes the formulae/casks expanders exist.
A successful live `BundleInstall` leaves the clicked row labelled `Installed`
and permanently insensitive, then requests `loadHomebrewPackages()` because a
bundle can install formulae and casks the current inventory snapshot predates.
That refresh is safe in both configurations: `loadHomebrewPackages` nil-guards
each expander, so it does nothing visible when `brew_group` is disabled, and
it takes a `brewPackagesRefresh` generation, so a slower bundle-triggered
refresh cannot overwrite newer rows. A failed install restores the `Install`
action. A successful dry-run uses
`actionmsg.BundleInstall(...).Complete == false`, shows an explicit preview,
and restores the action because nothing was installed — and for the same
reason it does not refresh the inventory. Each row owns a
`bundleview.InstallGate`, so a second callback cannot overlap a running
install even if invoked independently of GTK's insensitive-button guard.

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

The type is unwrapped by the classification, so the one type-based dependency in the whole UI — `errors.As(err, &trustErr)` at `internal/views/updates_page.go:298-300` — keeps working and redirects users to the Untrusted Taps UI rather than showing raw brew output.

The upgrade-failure toast text adapts to whether that UI is actually available: `trustmsg.UpgradeMessage(pkgName, trustGroupAvailable bool)` (`internal/views/trustmsg`, see "View-layer toast and decision helpers" below) is called from the outdated-packages row's upgrade click handler as `trustmsg.UpgradeMessage(pkgName, uh.brewTrustGroup != nil)`. `uh.brewTrustGroup` is only ever assigned once, in `buildUpdatesPage` on the main thread before any goroutine that could read it starts, so reading it from the upgrade goroutine is race-free. When the Untrusted Homebrew Taps group exists (`brew_trust_group` enabled and built), the message points there ("see Untrusted Homebrew Taps below"); when it doesn't (group disabled, or not yet built), the message is self-contained — it states the package can't be upgraded until its tap is trusted, with no reference to "below" or the section name, since there is nothing to point to. For bundle install failures blocked by untrusted taps, `trustmsg.BundleMessage(bundleName, tap string)` provides the toast directing the user to `brew trust <tap>`, remaining self-contained without pointing at sections that may be hidden or irrelevant for uninstalled packages.

**Cross-group nil-safety** — `trustTap` (`internal/views/updates_page.go`) refreshes the outdated-packages list after a successful trust, since newly-trusted packages may now show as outdated. That refresh (`loadOutdatedPackages`) is gated only on `brew_trust_group`, not `brew_updates_group`, so it must tolerate `brew_updates_group` being disabled — in which case `uh.outdatedExpander` was never built and is nil. `loadOutdatedPackages` guards on `uh.outdatedExpander == nil` as its first statement, before any homebrew call or `sgtk.RunOnMainThread`, consistent with the config-driven-visibility invariant: a disabled group's widget fields stay nil, and any code reachable from another group's async callback must nil-guard before touching them.

### View-layer page presentation (`internal/views/pageview`)

`internal/views/pageview` is one of the ten puregotk-free leaf packages under
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
- `UntrustedTap` combines formulae and casks in that order, strips each
  tap-qualified package prefix for display, and includes the installed count.
  `FlatpakUpdate` always includes the application ID, adds the version arrow
  only when a new version exists, and adds the user-installation suffix only
  for user updates.
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

Two of the nine small, puregotk-free packages under `internal/views/` (the others are `internal/views/actionstate`, `internal/views/badgestate`, `internal/views/bundleview`, `internal/views/rowset`, `internal/views/flatpakstatus`, `internal/views/featurestatus` and `internal/views/pageview`, each documented in its own subsection) hold the text and, at four call sites, the accompanying UI decision that view handlers use once a wrapper call returns. Both follow `docs/skills/gtk-headless-testing/SKILL.md`'s prescribed fix: `internal/views` itself cannot host a `_test.go` (puregotk panics resolving GTK/graphene shared libraries at package init, before any test runs), so the decidable logic is extracted into a pure package and table-tested there instead. Decision records: [ADR-0007](../adr/0007-pure-leaf-packages-route-around-untestable-gtk.md) (the leaf-package layout) and [ADR-0009](../adr/0009-dry-run-output-convention-and-single-decision-structs.md) (the decision-struct rule these packages implement).

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

`internal/views/actionstate` is one of the ten puregotk-free leaf packages
under `internal/views`. It owns the state machines and complete outcome tables
for the Applications and Updates pages' Homebrew mutation controls:

- `Gate.TryStart` atomically moves idle to running and rejects every repeated
  callback while running; `Reset` makes a failed, previewed, or fully-refreshed
  action retryable; `Complete` permanently closes a live-upgraded row action.
- `RefreshGate.Begin` assigns an increasing generation to each metadata
  refresh and `IsCurrent` accepts only the newest, preventing a slower old
  query from publishing after a newer one.
- `PackageUpgrade(succeeded, dryRun)` returns exactly three outcomes: failure
  restores the control without changing rows; dry-run success also restores
  it without a refresh; live success requests both immediate row removal and
  a full outdated-metadata refresh.
- `PackageInstall`, `PackageUninstall`, and `PackagePin` share the installed
  inventory mutation outcomes: failure and dry-run success restore the row
  controls without a refresh; live success completes the old controls and
  requests a generation-guarded installed-package refresh.
- `MetadataUpdate(succeeded, dryRun)` returns exactly three outcomes: failure
  and dry-run success restore the top-level control without refreshing; live
  success requests a refresh and deliberately does not restore the control
  until that refresh completes.
- `OutdatedRefresh(succeeded, currentCount, discoveredCount)` returns exactly
  two outcomes: failure keeps `currentCount` and does not authorize row
  replacement; success authorizes replacement and adopts `discoveredCount`,
  including zero.
- `OutdatedPresentation(count)` returns `0 packages available` with expansion
  disabled for zero, `1 package available` with expansion enabled for one,
  and `%d packages available` with expansion enabled for larger counts.

`actionstate_test.go` table-tests every outcome, races 64 callers against one
action gate (requiring exactly one acquisition), and proves 64 concurrent
refresh requests receive unique generations with exactly one current.
`wiring_test.go` and `applications_wiring_test.go` statically check the
puregotk-importing views use those decisions, confirmation/progress states,
shared gates, row removal, count decrement, versioned refresh callbacks, and
clear/add bookkeeping; no `_test.go` is added to `internal/views`.

### View-layer update badge state (`internal/views/badgestate`)

`internal/views/badgestate` is one of the ten puregotk-free leaf packages
under `internal/views`. `Counts` replaces the three independent integer fields
that previously lived on `UserHome` with one mutex-protected owner for Bootc,
Flatpak, and Homebrew update counts. `Set(source, count)` models a completed
provider refresh and replaces that provider's prior value; `Add(source,
delta)` models an immediate row-level change such as a successful Homebrew
upgrade. Both clamp negative results to zero and return an atomic
`Snapshot{Count, Total}`. `Get` and `Total` provide locked reads.

The view still performs widget mutation through `sgtk.RunOnMainThread`; the
leaf package owns only integers and synchronization. `badgestate_test.go`
proves the zero value, multi-provider totals, replacement rather than
accumulation across repeated refreshes, decrement/clamping behavior, unknown
source rejection, and concurrent changes under the race detector.
`wiring_test.go` verifies `views.go` and `updates_page.go` route all three
providers and the displayed total through this owner, and rejects the retired
independent count fields.

### View-layer Brew bundle state (`internal/views/bundleview`)

`internal/views/bundleview` is one of the ten puregotk-free leaf packages
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

`internal/views/rowset` is one of the ten puregotk-free leaf packages under `internal/views/` (its siblings are `internal/views/actionmsg`, `internal/views/actionstate`, `internal/views/badgestate`, `internal/views/bundleview`, `internal/views/trustmsg`, `internal/views/flatpakstatus`, `internal/views/featurestatus`, `internal/views/progresslog` and `internal/views/pageview`). It holds single-row removal, clear-then-repopulate bookkeeping, and rolling-window eviction for rows a view adds to an expander, so a successful action can remove exactly its row, a later list reload does not accumulate stale rows, and a streamed log does not grow without bound. Like `actionmsg`, `actionstate`, `badgestate`, `bundleview` and `trustmsg`, it exists because `internal/views` itself cannot host a `_test.go` (puregotk panics resolving GTK/graphene shared libraries at package init, before any test runs — `docs/skills/gtk-headless-testing/SKILL.md`); unlike them it imports nothing at all outside the standard library.

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

### View-layer bounded staging output (`internal/views/progresslog`)

`internal/views/progresslog` is one of the ten puregotk-free leaf packages under `internal/views/`. It bounds what the two OS staging handlers render from a streamed run. Like `rowset` it imports nothing outside the standard library (`sync`, `time`) and names no widget type.

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

### View-layer Flatpak update status (`internal/views/flatpakstatus`)

`internal/views/flatpakstatus` is one of the ten puregotk-free leaf packages under `internal/views/`. It turns the outcome of the two Flatpak update queries — how many updates are known, and which of the user/system installations could not be checked — into the Flatpak updates expander's subtitle text plus whether the expander should be expandable. Like `actionmsg`, `actionstate`, `badgestate`, `bundleview`, `trustmsg`, `rowset` and `pageview` it exists because `internal/views` itself cannot host a `_test.go` (puregotk panics resolving GTK/Libadwaita/GLib/graphene shared libraries at package init, before any test runs — `docs/skills/gtk-headless-testing/SKILL.md`); like `rowset` it imports nothing at all outside the standard library (`fmt`).

Exported surface:

- `Result{Subtitle string; Expandable bool}` — the expander state for one update load.
- `Subtitle(count int, userFailed, systemFailed bool) Result` — derives that state. Both halves come from a single call so the wording and the expansion decision cannot drift apart, the same reason `actionmsg` returns `ScriptDecision`/`TapTrustDecision`/`FeatureToggleDecision` structs. Failure is taken as two `bool`s rather than `error` values, which is what keeps the package free of any dependency on `internal/flatpak`.

`Expandable` is `count > 0` in every case: a failed query never invents updates, so there is nothing to expand that the count does not already reflect, while the rows that *were* found in a partially failed load are real and stay reachable. The five distinguishable outcomes are: both queries ok with no updates → `All applications are up to date` (the only case that makes the up-to-date claim); both ok with updates → `1 update available` / `%d updates available`; exactly one query failed with no updates → `No updates found in the <ok> installation; the <failed> installation could not be checked`; exactly one failed with updates → the count followed by `; the <failed> installation could not be checked`; both failed → `Could not check for updates`, which makes no claim about update state at all. `<ok>`/`<failed>` are the literal words `user` and `system`. `flatpakstatus_test.go` has one subtest per row (with both the user-failed and system-failed variants of the one-failed rows), and additionally asserts that all the subtitles are pairwise distinct, that "up to date" appears in the first case and no other, and that singular and plural both read correctly.

The package is pure and holds no state, so it is safe to call from a worker goroutine or from inside an `sgtk.RunOnMainThread` closure.

`loadFlatpakUpdates` (`internal/views/updates_page.go`) is its only call site. It keeps both `flatpak.ListUpdates` errors as values — `userErr` and `systemErr`, still logged exactly as before via the two `log.Printf("Error loading {user,system} flatpak updates: %v", …)` lines — instead of dropping them once logged, and then calls `flatpakstatus.Subtitle(len(allUpdates), userErr != nil, systemErr != nil)` once on the worker goroutine, before entering `sgtk.RunOnMainThread`. Inside that closure (past the `if uh.flatpakUpdatesExpander == nil { return }` guard, which stays because `flatpak_updates_group` can be disabled and the expander then never gets built) the result is applied unconditionally: `SetSubtitle(result.Subtitle)` and `SetEnableExpansion(result.Expandable)` run on *every* path, including the zero-update path, and only the building of the per-update rows is skipped when there are none. The view holds no subtitle text and makes no decision of its own; the old hard-coded `"All applications are up to date"` and `fmt.Sprintf("%d updates available", …)` strings are gone from it.

The practical consequence is that a total failure — both installations unqueryable — no longer renders as an all-up-to-date message: `allUpdates` is empty for the same reason it is empty when everything really is current, and only the retained errors distinguish the two, so the expander reads `Could not check for updates`. A partial failure is identified as partial rather than silently under-reported: the rows that were found are shown and expandable, with the subtitle naming the installation that could not be checked. The badge deliberately remains a plain count — `uh.updateCounts.Set(badgestate.Flatpak, len(allUpdates))` carries no error state — so a total failure shows a badge contribution of `0` next to an honest subtitle rather than an invented number.

### View-layer feature update status (`internal/views/featurestatus`)

`internal/views/featurestatus` is one of the ten puregotk-free leaf packages under `internal/views/`. It owns every string and every decision the Features page's updex update check needs: a feature row's subtitle, whether that feature has an update, and the features group's description. Like `actionmsg`, `actionstate`, `badgestate`, `bundleview`, `trustmsg`, `rowset`, `flatpakstatus` and `pageview` it exists because `internal/views` itself cannot host a `_test.go` (puregotk panics resolving GTK/Libadwaita/GLib/graphene shared libraries at package init, before any test runs — `docs/skills/gtk-headless-testing/SKILL.md`). Unlike them it imports one non-standard-library package, `internal/updex`, for the `CheckResult` type; that is safe because `internal/updex` is itself puregotk-free (`go list -deps ./internal/updex | grep -c puregotk` prints `0`), and `go list -deps ./internal/views/featurestatus | grep -c puregotk` prints `0` too.

Exported surface:

- `Status{Subtitle string; HasUpdate bool; Incomplete bool}` — the row state for one feature.
- `Feature(name string, results []updex.CheckResult) (Status, bool)` — derives that state from *all* of the feature's components. Both halves come from a single call so the wording and the update decision cannot drift apart, the same reason `flatpakstatus.Subtitle` returns a `Result`. When `len(results) == 0` (e.g. per-component manifest/version lookup failed in updex), it returns a `Status` with subtitle `<name> — update check failed` and `Incomplete: true`.
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

`checkFeatureUpdates` (`internal/views/features_page.go`) is its only call site, and — as with `loadFlatpakUpdates` and `flatpakstatus` — the view holds no text of its own: every string in the update-check path now comes from `featurestatus`. `updex.CheckFeatures` still runs on the worker goroutine and returns any retained warnings alongside results, and all widget access still happens inside the single existing `sgtk.RunOnMainThread` closure. Inside it, warnings are logged first — `CheckFeatures` retains them even when it returns an error, so the failure path must not return before them — and then the features group's description is set on **every** outcome. When the check failed, the existing `log.Printf("Feature update check failed: %v", err)` is kept and the description becomes `featurestatus.GroupDescriptionCheckFailed(totalFeatures)` before returning, so the group no longer keeps reading `%d features available` — which looked like a completed check that found nothing. When partial-check warnings or empty results occur, the group description becomes `featurestatus.GroupDescriptionIncomplete(totalFeatures, updateCount)` instead of claiming all features are up to date. When the check succeeded without issues, the description is set unconditionally from `featurestatus.GroupDescription(totalFeatures, updateCount)`, including when `updateCount` is `0`, so "all up to date" is actually reported rather than the pre-check string being left in place. Per feature the view calls `featurestatus.Feature(check.Feature, check.Results)` over the whole `Results` slice — `check.Results[0]` is gone from the file, and with it the bug that a feature whose second component was outdated read as up to date — and counts one per feature with `status.HasUpdate`, so the description's two numbers are both feature counts. Both guards that config-driven visibility requires stay: `features_group` can be disabled, so every `SetDescription` (the failure one included) sits behind `uh.featuresGroup != nil` and the `uh.featureRows` lookup keeps its `!ok { continue }`.

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
| `ListUpdates(user)` | `flatpak remote-ls --updates --app --columns=name,application,version [--user\|--system]` | 30s | Separate calls for user/system; `--app` excludes runtimes |
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

### `GetStatus` (unprivileged)

`GetStatus(ctx)` runs `bootc status --format json` with **no** `pkexec` — this is a plain read, safe to call from any goroutine (`internal/bootc/bootc.go`). Output is unmarshaled into `Status{Spec, Status: {Booted, Staged, Rollback}}`, where each of `Booted`/`Staged`/`Rollback` is a `*Deployment` (nil-safe accessors: `ImageRef()`, `Version()`, `Timestamp()`, `Digest()`).

`GetStatus` is a one-line wrapper: `return getStatusFrom(ctx, bootcCommand)`. The unexported `getStatusFrom(ctx context.Context, name string) (*Status, error)` runs `<name> status --format json`, classifies the error, and parses the output. The executable name is a parameter purely so `bootc_test.go` can drive a `#!/bin/sh` script from `t.TempDir()` on a host with no `bootc` installed. The seam is unexported and its only production call site passes the fixed `bootcCommand` constant, so no caller-supplied or user-derived string can reach it.

Failures classify into exactly four distinct outcomes, checked in this order — `errors.Is` against `context.DeadlineExceeded`/`context.Canceled`, never `==` on `ctx.Err()`, so a wrapped cause still classifies:

| Condition | Result |
|-----------|--------|
| The context deadline expired | `*Error`, message `bootc status timed out`, unwrapping to `context.DeadlineExceeded` |
| The context was cancelled by its owner | `*Error`, message `bootc status was canceled`, unwrapping to `context.Canceled` |
| The command exited non-zero | `*Error`, message `bootc status failed (exit N): <stderr>`, unwrapping to the `*exec.ExitError` — matching neither context sentinel |
| The executable is missing (`exec.ErrNotFound` for a bare name, `fs.ErrNotExist` for an explicit path) | `*NotFoundError` (`bootc not found`) |

The deadline and cancellation messages differ, and neither surfaces as `signal: killed`: `exec.CommandContext` kills the child when the context ends, so the classified error replaces the raw wait error. `bootc.Error` carries an `Err error` field and an `Unwrap() error` method — matching `internal/homebrew` and `internal/flatpak` — so callers distinguish all four outcomes with `errors.Is`/`errors.As` while `Error` keeps its human-readable `Message`. `bootc_test.go` covers all four against fake scripts (plus the success parse), so the whole classification is exercised without a real `bootc` on the host.

### Boot gate semantics

`bootc status` exits 0 with a null `booted` field on hosts that aren't running a bootc deployment at all — so the gate cannot be the exit code. `Status.Booted()` returns `s.Status.Booted != nil`. `IsBootcBooted(ctx)` calls `GetStatus` and returns that boolean (treating any error as "not booted"). `IsBootcBootedCached()` wraps it in a `sync.Once` with a 5s timeout, computing the result once and caching it for the lifetime of the process — this lets multiple view goroutines call it during async startup without triggering redundant `bootc` invocations. **Do not use `/run/ostree-booted`** as a substitute gate: it is absent on snow's composefs-based deployments, so checking for it would hide bootc UI on every snow host.

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

`onBootcStageClicked()` drives the updates page's "System Update" expander directly (there is a single staging operation, so no shared cross-operation helper is needed) — it disables the button, spawns `bootc.StageUpdate` in a goroutine, and processes events on a second goroutine through the shared `stageProgressSink` (see [`internal/views/progresslog`](#view-layer-bounded-staging-output-internalviewsprogresslog)), restoring button state and showing a toast on completion. The system page's `loadBootcStatus()` is a separate, read-only path: it calls `bootc.GetStatus` to display the booted/staged/rollback deployment images, versions, and digests, with no staging controls — staging only happens from the Updates page.

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

Pinning to a dated tag is not implemented here and is not unblocked by this
package. `chairlift-ublue-helper` accepts no image reference (ADR-0001), so a
pin has to be a new privileged operation whose target the helper derives from
a validated grammar, as `channel-switch` already does for its own target.

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
(`onHomebrewSearch`, `internal/views/applications_page.go`) and per-app Flatpak
uninstall buttons (`loadFlatpakApplications`, both user and system branches)
show toasts built by `actionmsg.Install(dryRun, result.Name)` and
`actionmsg.Uninstall(dryrun.Enabled(), appID)`, rather than unconditional
completion claims. The Homebrew path restores its install control after a
dry-run and does not refresh, because nothing changed; only a live success
completes the control and starts the generation-guarded installed-package
refresh described above.

Installed Homebrew formula/cask rows follow the same decision path for
uninstall, and formula rows add pin/unpin. They confirm before starting,
disable every mutation control on the row while the worker runs, use
`actionmsg.Uninstall`/`actionmsg.Pin` for live versus preview wording, restore
on failure or dry-run, and refresh the installed inventory only after live
success. New Flatpak discovery and install deliberately remain in the
configured external manager; ChairLift's direct Flatpak UI lists and
uninstalls installed applications.

The Flatpak list refresh after uninstall remains unconditional because it
re-queries live state either way. Each successful loader branch first clears
the prior `flatpakUserRows` or `flatpakSystemRows` tracker and then rebuilds it
inside one main-thread closure. Homebrew's installed formula/cask loader now
uses the same separate-tracker clear-before-repopulate pattern, with an
additional refresh generation because multiple Homebrew actions can request
overlapping reloads. Not-installed and error branches change the subtitle and
preserve the last known rows.

The Updates page's per-package Homebrew upgrade button, per-app Flatpak update button, and the "Update Homebrew" self-update button (`internal/views/updates_page.go`) follow the same toast pattern: `actionmsg.Upgrade(dryRun, pkgName)`, `actionmsg.Update(dryrun.Enabled(), appID)`, and `actionmsg.SelfUpdate(dryRun, "Homebrew")` replace what were unconditional "upgraded"/"updated"/"updated successfully" toasts, since `upgrade` and `update` are both in their wrappers' `stateChangingCommands` and no-op under dry-run. The Flatpak update button's list refresh (`go uh.loadFlatpakUpdates()`) stays unconditional, same reasoning as the uninstall refresh above.

The two Homebrew paths additionally use `actionstate.Gate` before spawning a
goroutine and immediately make the clicked button insensitive with an
`Updating...` or `Upgrading...` label, so a repeated callback cannot overlap
the operation even if it bypasses GTK's insensitive-button guard. A command
failure restores the original label/sensitivity and leaves
the Homebrew value in `updateCounts`, the tracked rows, and the sidebar badge
unchanged. A dry-run wrapper success shows the preview toast and restores the same control
without refreshing because no metadata or package state changed.

A live per-package success completes its gate, removes exactly its tracked row
through `rowset.Tracker.Remove`, decrements the Homebrew count (and therefore
the aggregate sidebar badge) through `updateCounts.Add(badgestate.Homebrew,
-1)`, and starts a full `ListOutdated` refresh. A live top-level `brew update`
keeps its button busy while that refresh runs and
restores it from the refresh's main-thread completion callback. Each request
takes a generation from `actionstate.RefreshGate`; its main-thread result
first proves that generation is still current, so a slower older query cannot
overwrite rows or counts produced by a newer request. A superseded request
still invokes its completion callback with failure, ensuring the control that
requested it is not stranded.

The refresh uses `actionstate.OutdatedRefresh`: only a successful current
query clears/rebuilds rows and adopts `len(packages)` as the count; a failed
current query changes the expander subtitle to
`Error refreshing updates: ...` but retains the last known rows/count. All
external Homebrew calls remain on worker goroutines and all widget mutations,
including row removal and control restoration, remain inside
`sgtk.RunOnMainThread`.

The Updates page's bootc "Check for Updates" stage button (`onBootcStageClicked`, `internal/views/updates_page.go`) follows the same `actionmsg` pattern, with one difference from the buttons above: unlike `Install`/`Upgrade`/etc., whose completion text is selected purely by `dryrun.Enabled()`, `SystemStage(dryrun.Enabled(), staged)` also takes the live `staged` result from the post-`wg.Wait()` `bootc.GetStatus()` re-read, because the non-dry-run branch still needs to pick between the "staged" and "up to date" strings. Under dry-run, `staged` is ignored entirely and a single preview string is returned instead — see "Dry-run behavior" under bootc above for why. The expander's `SetSubtitle` calls in the same code block are *not* routed through `actionmsg`; they keep reading live `GetStatus()` output unconditionally, since the subtitle is a persistent status display rather than a per-click completion claim.

The Features page's per-feature switch (`onFeatureToggled`, `internal/views/features_page.go`) follows the same decision-struct pattern as maintenance-script execution and tap trust: on a successful `updex.EnableFeature`/`DisableFeature` call, `decision := actionmsg.FeatureToggle(dryrun.Enabled(), enabled, name)` is computed once, and the switch's visual state is driven solely by `decision.Confirm` — `toggle.SetActive(enabled)` (confirming the flip) when `Confirm` is true, `toggle.SetActive(!enabled)` (reverting to the pre-click state) when it is false. Under dry-run, `updex.runHelper` returns before ever invoking pkexec, so nothing was actually toggled and the switch must not visually confirm a change that did not happen — this is the other "switch/list implies a state change after a preview" bug (the tap-trust row-removal case is the same pattern in Homebrew's Untrusted Taps list). The Update button (`onUpdateFeaturesClicked`) has no equivalent mutation to gate — its `SetSensitive`/`SetLabel` reset is unconditional in both modes — so its toast is a plain string, `actionmsg.FeatureUpdate(dryrun.Enabled())`.

## Install-path consistency (`internal/installcheck`)

The source `make install` path (Makefile, `PREFIX` defaulting to `/usr`) and
the two packaged nFPM (deb/rpm/apk) layouts GoReleaser builds from
`.goreleaser.yaml` ship this repository's privileged surface and maintainer
configuration. They are hand-maintained text — a Makefile recipe and YAML
blocks — with no shared code path, so nothing stops them (or
`internal/updex.HelperPath`, the fixed absolute path `pkexec` matches against
the policy's `exec.path` annotation) from silently drifting apart.

ChairLift packages the bootc, updex, and ublue `.policy` files. It
no longer ships its old `.rules` files, which returned `YES` for every active
local member of the `sudo` group and bypassed authentication. Source
installation explicitly removes those legacy rule paths; package upgrades
remove them as obsolete tracked files. Every policy uses normal
administrator authentication. The updex and ublue policies select one action for
each supported first argument, and the helpers validate the complete argv shape.

Every install layout installs the repository's `config.yml` as package-owned
maintainer defaults at `/usr/share/chairlift/config.yml`. None installs
`/etc/chairlift/config.yml`: that higher-precedence path belongs to the
administrator and must survive package installation and upgrades unchanged.

GoReleaser has two nFPM entries. `projectbluefin-chairlift` is self-contained and
selects the `chairlift`, `chairlift-updex-helper`, and
`chairlift-ublue-helper` builds. `projectbluefin-chairlift-system-integration`
selects both helper builds and packages only `/usr/bin/chairlift-updex-helper`,
`/usr/bin/chairlift-ublue-helper`,
`/usr/share/polkit-1/actions/io.projectbluefin.chairlift.bootc.policy`,
`/usr/share/polkit-1/actions/io.projectbluefin.chairlift.updex.policy`,
`/usr/share/polkit-1/actions/io.projectbluefin.chairlift.ublue.policy`,
`/usr/share/chairlift/config.yml`, and the channel-table example at
`/usr/share/doc/chairlift/channels.example.yml`, for pairing with a user-scoped
app installation. The two package names conflict to
prevent simultaneous ownership of the same fixed system files. The companion
does not provide the OS stager; a distro must provide a trusted implementation at
`/usr/libexec/bootc-update-stage`. This split is decision
record [ADR-0006](../adr/0006-split-system-integration-package-with-mutual-conflicts.md).

`internal/installcheck` holds regression tests, not production code, that turn
"verified by inspection" into real, gated checks. The first two guard the
installed layout itself:

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
- **`TestGoreleaserNfpmLayoutMatchesUsrPrefix`** parses the real, repo-root
  `.goreleaser.yaml` (not a fixture) with the already-vendored
  `gopkg.in/yaml.v3` and, iterating **every** `nfpms[]` entry (not just
  `nfpms[0]`, so adding or reordering a second package with the wrong layout
  still fails — per
  `docs/skills/collection-regressions/SKILL.md`),
  asserts each entry's `bindir` matches the fixed helper directory, its
  updex/ublue/bootc policy `contents[].dst` entries equal the fixed
  polkit-1 actions paths, their policy/config modes remain `0644`, and no
  `.rules` content remains. It also requires every package to
  map the repository `config.yml` to
  `/usr/share/chairlift/config.yml` and rejects any content entry targeting
  `/etc/chairlift/config.yml`.
- **`TestGoreleaserPublishesTheSystemCompanionPackage`** requires exactly one
  full package and one integration package, verifies their build filters,
  mutual conflicts, unique IDs, and the integration package's exact six
  content mappings. This prevents the companion from accidentally acquiring
  the GUI binary or losing one of the root-owned integration files.
  It was named `TestGoreleaserPublishesSystemIntegrationPackage` until
  2026-09-18 — the name ADR-0006 records, and the one still correct as that
  decision's historical context. `Integration` in the name matched the
  `-skip "Integration"` half of the filter described below, so despite being
  cited by AGENTS.md and the ADR as the enforcement for the
  system-integration split, the filtered unit-test step never selected it. Renaming it
  was the fix; `internal/installcheck`'s
  `TestNoInternalTestNameIsExcludedByTheCIFilter` now rejects any test under
  `internal/` that the filter would drop, so no other gate can be silently
  inert the same way.

Both tests fail — not skip — if `internal/updex.HelperPath`, the Makefile's
`PREFIX` default, or `.goreleaser.yaml`'s `nfpms` block change independently
of one another; each was hand-verified during development by reverting one
of the three at a time and confirming only the test(s) that source depends
on turn red. The package imports no puregotk, directly or transitively, so it
never trips `docs/skills/gtk-headless-testing/SKILL.md`'s constraint, and it
lives under `internal/...` so `gates_chunk`, `make ci`, and CI's identical
`go test ./internal/... -run "^Test[^I]" -skip "Integration"` filter all
exercise it on every run, per
`docs/skills/gated-test-placement/SKILL.md` — not just the
heavier, less-frequent `make ci` deep gate.

A third regression test, **`TestGoreleaserLicenseIsGPL`**, guards a related
but distinct drift: `.goreleaser.yaml` briefly declared `license: MIT` in
both its top-level `metadata:` block and its `nfpms[]` entry's `license`
field, while the project's actual license is GPLv3-or-later (`LICENSE`, and
`internal/window/window.go`'s about dialog, which sets
`gtk.LicenseGpl30Value` — puregotk's "GPL 3.0 or later" enum value, distinct
from `LicenseGpl30OnlyValue`). Like the layout test above, it parses the
real, repo-root `.goreleaser.yaml` via the shared `loadGoreleaserConfig`
helper (no fixture) and asserts `cfg.Metadata.License` and, iterating
**every** `nfpms[]` entry with a per-index `t.Run` (not just `nfpms[0]`, per
`docs/skills/collection-regressions/SKILL.md`),
each entry's `License` field equal the fixed SPDX identifier
`GPL-3.0-or-later`. `GoreleaserConfig.Metadata` (`MetadataConfig.License`)
and `NfpmConfig.License` (`internal/installcheck/installcheck.go`) exist
specifically to give `yaml.Unmarshal` somewhere to put these two values;
without those struct fields yaml.v3 silently drops them and the test would
pass vacuously regardless of what the YAML says. This test exists so a
future edit reintroducing MIT (or any other license) in either location —
the exact regression that motivated it — fails the gate instead of shipping
mislabeled deb/rpm/apk package metadata again.

A fourth regression test guards the same class of drift for the
**repository URL**. GoReleaser OSS (unlike Pro) has no global `metadata:`
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
default `GITHUB_TOKEN`, which is enough to publish binaries, archives, and the
rpm/deb/apk nFPM packages straight to the GitHub Release for the tagged
commit — no Pro license or `GORELEASER_KEY` secret. Snapshot output is
governed separately by `.goreleaser.yaml`'s `snapshot:` block
(`version_template: "{{ .ShortCommit }}-snapshot"`), which sets the version
template for local `goreleaser release --snapshot` builds; it needs no credentials
and no workflow, so it is not gated here. `goreleaser check` is therefore
deliberately not run anywhere — locally, in `gates_chunk`, or in `make ci` —
and the test neither shells out nor renders anything. As with the license
guard, `ReleaseConfig.Footer` in `internal/installcheck/installcheck.go` exists
solely so `yaml.Unmarshal` has somewhere to put the footer value; there is no
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

A sixth regression test guards the packages' **declared runtime
dependencies**. Until issue #89 the deb/rpm/apk metadata named no runtime
dependencies at all, so a minimal target-family image could install the
full package successfully and then fail to launch it: the GUI dlopens the
GTK4 and Libadwaita shared libraries at package-init time through puregotk
(`libgtk-4.so.1`, `libadwaita-1.so.0`), and the desktop entry
(`data/io.projectbluefin.chairlift.desktop`) always launches
`/usr/bin/chairlift-wrapper`, a Bash script (`data/chairlift-wrapper.sh`).
The full `projectbluefin-chairlift` package now declares those dependencies
per format in GoReleaser's `nfpms[]` `overrides` block, because the distro
package names differ per format: Debian names `libgtk-4-1` and
`libadwaita-1-0`, Fedora names `gtk4` and `libadwaita`, and Alpine names
`gtk4.0` and `libadwaita`, with `bash` in every format. A single
base-level `dependencies` list would carry one format's name into the other
two, and GoReleaser's merge of per-format overrides over the base fields
replaces a non-empty slice rather than appending to it (dario.cat/mergo
v1.0.2's `WithOverride`, verified against the exact version the release
workflow pins), so a base list coexisting with a per-format one is silently
dropped; the test rejects a base-level list outright and pins the exact
per-format set. The integration package declares none — it
ships only pure-Go helper binaries and root-owned data files, no GUI,
desktop entry, or wrapper script, and must stay installable on hosts that
carry no GTK stack at all. **`TestGoreleaserDeclaresMandatoryRuntimeDependencies`**
(`internal/installcheck/goreleaser_test.go`) holds both halves via the
shared `loadGoreleaserConfig` helper: `NfpmConfig.Dependencies` and the
new `NfpmOverrides` struct in `internal/installcheck/installcheck.go`
exist so `yaml.Unmarshal` has somewhere to put these values, and the
negative controls (dropping a per-format entry, adding a base-level list,
adding a dependency to the integration package) each turn the test red.

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
