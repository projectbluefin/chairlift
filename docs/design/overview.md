# ChairLift Overview

Current architecture and safety boundaries for **Control Center**, the GTK4/
Libadwaita GUI for Bluefin and other bootc images. ChairLift remains the binary,
repository and application-ID name; user-facing naming belongs to
`internal/branding.AppName`. Go builds without CGO; puregotk loads native
libraries at runtime. Dependency versions belong in [`go.mod`](../../go.mod).

## Architecture

The GUI's dependency direction is `cmd/chairlift → app → window → views →
providers`. Pure state and presentation packages sit below GTK wiring, so their
decisions can be tested without initializing native libraries.

| Boundary | Live owner |
| --- | --- |
| Process startup, headless intents and desktop settings backend | [`cmd/chairlift`](../../cmd/chairlift/) |
| Application lifecycle and command-line dispatch | [`internal/app`](../../internal/app/) |
| Window, page mounting, navigation and explicit setup | [`internal/window`](../../internal/window/) |
| Widget construction and event wiring | [`internal/views`](../../internal/views/) |
| Static widget trees and Go bindings | [`internal/ui`](../../internal/ui/) |
| Configuration schema/loading and host prerequisites | [`internal/config`](../../internal/config/), [`internal/capability`](../../internal/capability/) |
| Route identities, visibility and transitions | [`internal/navigation`](../../internal/navigation/navigation.go) |
| Update state, execution adapters and presentation | [`internal/updateflow`](../../internal/updateflow/), [`internal/updateproviders`](../../internal/updateproviders/), [`internal/views/updatepresent`](../../internal/views/updatepresent/) |

Provider execution and service-management details are in
[package-managers.md](package-managers.md). Operator-facing behavior is in the
[reference](../reference.md) and [walkthrough](../walkthrough.md). Decision
rationale belongs in [accepted ADRs](../adr/), not feature histories here.

### Pages

`internal/navigation` owns the sidebar order: **Updates, Apps, Agents, Features,
Livery, Maintenance, Help**. Configuration and host capabilities can omit a
functional page; Help remains. Do not copy route/group inventories into another
table: route references are page-qualified configuration keys, not display names.

The detail routes are **Developer Mode** under Features (`developer`, also
opened by `--dx-mode`) and **Powerwash** under Maintenance (`recovery`). Details
keep the parent's sidebar row selected and have no Alt+number shortcut.
Developer Mode owns WSL, Docker, Development Apps and developer setup activity;
Game Mode remains on Features; its image switch manages no optional gaming stack.

Every mouse, keyboard, Back and deep-link entry goes through
`Window.navigateToPage` and `navigation.Resolve`. The same visible inventory
feeds the sidebar and registered/advertised shortcuts. Hidden details fall back
to their visible parent, then Help; hidden primaries fall back to Help. Unknown
routes are rejected. Navigation itself executes no provider mutations.

## Key Patterns

- **GTK stays on its main thread.** Run commands, network calls and service
  queries in workers; marshal widget changes through `sgtk.RunOnMainThread`.
  Nil-guard cross-group widget access: configuration may have prevented a
  widget from being constructed.
- **Static UI is Blueprint.** Edit `.blp` sources and regenerate committed `.ui`
  files with `make blueprints`; never hand-edit generated XML. Go binds widgets
  through `ui.Bind` tags and connects signals once at construction. The native
  library floor and binding traps are in the [Blueprint skill](../skills/blueprint-ui/SKILL.md).
- **One page owns scrolling.** A destination's `AdwPreferencesPage` scrolls and
  clamps itself; do not wrap it in another scroller. Updates appends its extra
  groups to the shell's same preferences page. Banners belong in toolbar bars.
- **Callbacks are reusable.** Refreshing lists route buttons/dialogs through
  `buttonRoute`, `dialogRoute` and `signalroute`, not fresh per-row callbacks
  that exhaust purego's trampoline table. Switch mutations use guarded
  `GtkSwitch::state-set`, not generic property notifications.
- **State outlives failed work.** Reuse `actionstate` gates, refresh generations
  and row bookkeeping. Failures/previews restore retryable controls; stale
  workers cannot replace newer rows. A completed gate is only for a control
  that becomes permanently unavailable after live success.
- **Progress is bounded and honest.** Coalesce streamed output into bounded
  main-thread callbacks and retained rows (`progresslog`, `stageProgressSink`).
  Quiet operations keep a reusable pulsing indicator; opaque commands do not
  imply per-package progress. Render external text without markup.
- **Long work is visible from every page.** The window owns one
  `internal/views/activity` tracker; the update check, every update-shell
  admission (named by `beginMutation`'s title, with the system download
  forwarding each streamed line as its current step), and app collection
  installs report to it. A header bar above the sidebar/content split shows a
  spinner while anything runs, so a collapsed (narrow) window keeps it on
  every pane, and it opens the Activity dialog listing each task, its current
  step, a percentage when the work measures one, and a time estimate once the
  measured rate is stable. The tracker coalesces change notifications to one
  main-thread redraw at a time and never gates work.
- **Decisions stay GTK-free.** Put decidable presentation and transitions in
  pure packages, not widget handlers. Packages importing puregotk cannot host
  headless unit-test binaries. See [ADR-0007](../adr/0007-pure-leaf-packages-route-around-untestable-gtk.md).

### Dry-run mode

[`internal/dryrun`](../../internal/dryrun/) is the process-wide preview authority.
Providers suppress mutations independently of UI feedback. A nil return from a
suppressed operation is not installation, changed state or a completed live
stage: preserve confirmed inventories, switches and preferences. Read-only
observations remain real. The output/decision contract is
[ADR-0009](../adr/0009-dry-run-output-convention-and-single-decision-structs.md).

### Privileged operations

Root operations use PolicyKit with these fixed executable boundaries:

| Surface | Execution owner |
| --- | --- |
| `/usr/libexec/bootc-update-stage` | `internal/bootc` through `internal/stageexec`; implementation supplied by the OS image |
| `/usr/bin/chairlift-updex-helper` | `internal/updex` through `internal/helperexec`; validated operations in `internal/updexhelper` |
| `/usr/bin/chairlift-helper` | `internal/ublue` through `internal/helperexec`; validated operations in `internal/ubluehelper` |

Paths must match the installed policies' `exec.path`; command selection must
match `exec.argv1`. Helpers validate the remaining argv themselves. Never add
arbitrary privileged commands, configurable helper paths or passwordless rules.
Image switches accept validated words, not registry references; the helper
derives targets from trusted image metadata and tables. Accounts come from
`PKEXEC_UID`, never caller-supplied usernames. Image channel/driver tables are
system-owned and keyed by image, not just tag.

The existing trusted maintenance-script exception is separate: only the fixed
system configuration candidates may define `sudo: true` actions, with absolute
script paths. Development fallbacks cannot acquire this authority. Its executor
is `internal/maintenanceexec`; do not turn it into a general helper interface.

Privileged dispatch journals live and suppressed intent at shared execution
boundaries. Helper-reported executed argv is an audit aid, not independent
proof. Authentication dismissal is distinct from failure. Cancellation kills the
direct escalation child, not necessarily privileged descendants; feedback must
not claim root work has stopped. See the [helper executor contract](../../internal/helperexec/helperexec.go).

Homebrew installs, tap trust, appearance and user services are unprivileged.
System Flatpaks use Flatpak's own authorization, never a ChairLift pkexec wrapper.
The GUI and image-installed helpers ship separately; helper-backed controls
consult installed action support rather than assuming a GUI release installed
matching helpers. See [ADR-0001](../adr/0001-fixed-path-pkexec-privilege-boundary.md).

## Configuration

[`internal/config`](../../internal/config/) owns schema, defaults, validation
and legacy-input migration. [`CONFIG.md`](../../CONFIG.md) documents search
precedence and fields; do not derive configuration identities from route titles.
Only a missing candidate advances the search. An existing unreadable or invalid
file disables every configurable group, emits `CONFIGURATION ERROR` and produces
a persistent toast; it never falls through to a more permissive file.

`internal/capability` owns non-blocking tool/asset prerequisites. Configuration
may subtract from that floor, never add to it. Navigation and builders share the
same composed predicate. Runtime state requiring subprocess or network queries
remains asynchronous, not a second capability probe. User update preferences
are separate again: disabled by policy, unsupported, user-disabled and failed
observations are different states.

## Updates and system state

`updateflow.Coordinator` owns source inventory, phases, mutation admission and
the badge. `updateproviders` wraps existing integrations; the coordinator runs no
commands itself. `updatepresent` maps snapshots to text/actions; `UpdateShell`
renders them. Manual updates, staging and image switches share admission, not
parallel state owners. Admission waits for source readiness and excludes checks,
other mutations and an in-flight restart; controls remain gated across snapshot
refreshes. Failed checks preserve known state; partial applies keep only
failed/pending work for retry. Restart requires an observed requirement,
not merely a successful staging command.

OS reads are unprivileged. On composefs hosts `internal/bootc` reads deployment
state and registry digests; other hosts use bootc's read commands. Staging still
uses the image-owned fixed script, not a new `bootc upgrade` executor. SBOM
comparison fetches only on explicit Compare and rejects stale image-pair results.

Manual Free Up Space and post-update maintenance share
`updateproviders.CleanupGroup` and the typed cleanup runner. Report skipped,
failed and completed work distinctly; never invent reclaimed bytes. Powerwash
and Factory Reset are opt-in, require destructive confirmation and keep fixed
scope. Factory Reset is unavailable on composefs; rollback does not itself
restart the computer.

### App collections

Apps collections expose entry-by-entry checkboxes and a one-way Select All,
with one Install action for the destination's checked entries. Installed entries
are observed, not reinstall selections; failed inventory reads preserve known
state. A safe, fully selected collection retains its Brewfile/tap declarations
through `brew bundle`; partial selections install only selected entries with
per-item commands. Preview runs neither path and does not mark entries installed.
Selection, planning and observed status belong to
[`bundleview`](../../internal/views/bundleview/) and
[`bundle_install.go`](../../internal/views/bundle_install.go).

## Developer and hardware setup

[`internal/devsetup`](../../internal/devsetup/) sequences direct and implicit
Developer Mode promotion under one gate. WSL/Docker setup follows a confirmed
parent grant; failed authentication starts no child work, and later optional
failure never reverses granted access. Retries retain completed stages. Group
access awaiting a new login is not Ready and is not an installation failure.

WSL inventory and shell readiness are separate observations in
[`internal/devtools`](../../internal/devtools/): a failed live shell probe stays
in `WSLState.Diagnostic` without erasing `Running`. The Developer Mode detail's
WSL **Details** dialog retains action, inventory and probe errors; **Recheck**
repeats inventory and, in live mode, readiness checks under the shared developer
action gate, including after a failed read. Initial engine selection reuses each
engine's inventory and readiness result for that refresh, avoiding duplicate
probes. Preview skips both NSL and Lima shell probes, preserves observed running
state, and reports readiness as unchecked.
Live NSL's `run` probe may start a VM if it stops between inventory and probing;
Recheck is not guaranteed to be non-starting. Failed inventories preserve only
the selected engine's last confirmed display state and do not prove NSL is absent
for initial engine selection. Retained state is presentation-only: a live Stop
requires a fresh successful inventory read before stopping the managed guest.
Dry-run Stop is only a preview. ChairLift never automatically repairs or
migrates a guest's disk.

System Docker uses image units when present, otherwise the fixed helper installs
verified Homebrew bottles into root-owned storage. Root never executes or loads
from the user-owned Homebrew prefix. CLI tools remain user-scoped. Stop retains
data, runtime and account groups. Verification and restart policy belong to
[`internal/dockerbrew`](../../internal/dockerbrew/) and
[ADR-0024](../adr/0024-rootful-docker-from-verified-homebrew-bottles.md).

Game Mode stages the published gaming/non-gaming image counterpart through the
fixed `gaming-switch enable|disable` helper, which derives the target itself.
It shares update-shell admission, confirms the Steam/Gamescope/Open Gaming
Collective kernel change, and leaves restart separate. The switch reflects the
booted or staged image, not app inventory. It never installs, inventories,
uninstalls or cleans up gaming Flatpaks; existing apps remain untouched. There
is no optional Gaming Tools toggle or backend. The live owners are
[`features_page.go`](../../internal/views/features_page.go) and
[`ubluehelper.GamingSwitchArgs`](../../internal/ubluehelper/ubluehelper.go).

Development Apps installation is independent of enabling Developer Mode.
[`internal/developerapps/catalog.go`](../../internal/developerapps/catalog.go)
owns the supported packages, remotes and architectures: Homebrew IDEs use
ublue-os taps only; the verified Flatpak exceptions use their system remotes.
Do not substitute personal taps, invent package identities or treat inventory
errors as absence.

[`internal/hardware`](../../internal/hardware/) detects supported ASUS/Framework
machines through DMI and owns their OEM cask profiles. Ordinary activation only
reads status. Explicit initial setup installs missing packages and re-reads
installed versions before reporting Active. Only verified installation permits
`livery.EnsurePanelDefault` to apply the embedded OEM mark; existing choices and
opt-outs survive. Preview neither installs packages nor confirms readiness.

## Agent Mode

[`internal/aistack`](../../internal/aistack/) owns the local llmman user service
and selected-model alias. Readiness requires observed loopback HTTP health and
verified adoption of the owned unit, not file presence. Reconciliation accepts
a matching live invocation stamp or systemd proof that the loaded current unit
started strictly after its file was written. A proven natural restart is adopted
without restarting loaded models; an observed dirty manager gets a reload, not
an unnecessary daemon restart. Invocation-stamp writes reload the manager and
remove the stamp if reload fails. Unproven policy triggers safe reconciliation;
failure best-effort stops the managed daemon and cannot report Ready. These
boundaries belong to [`aistack/migration.go`](../../internal/aistack/migration.go).
llmman owns models and engine selection. Disable retains models/binaries and preserves management files
if the service cannot be proven stopped.

[`internal/agentmode`](../../internal/agentmode/) owns readiness and launch;
[`internal/troubleshoot`](../../internal/troubleshoot/) owns package setup and
ChairLift's isolated Goose profile. Never read or rewrite the user's Goose
configuration. The profile enables only the fixed read-only Linux tools and
online knowledge search; credentials are not persisted into it. Ask Bluefin
uses the same dispatcher and selected model. A supplied starting prompt stays
in memory, is not logged and is never tool/profile configuration.

## Appearance and local services

`internal/livery` owns icon-theme overrides, settings restoration and login
rotation; `internal/deskenv` selects supported desktop surfaces and the native
settings backend. Page load does not apply selections. Defaults and existing-unit
reconciliation use narrowly guarded paths, not general load-time repair.
`internal/avatar` applies profile pictures; `internal/valhalla` owns explicit
wallpaper setup. Neither feature promises readiness from package presence alone.

`internal/printerapp` owns rootless printer quadlets. Verify publishing identity
and pin the verified digest before writing/enabling or refreshing a unit. Bind
only loopback: unauthenticated administration must not be exposed to the LAN.
Service readiness is observed, not inferred from a unit file; failed stops keep
the management file. Device printing remains unverified.
See [printer-applications.md](printer-applications.md).

## Explicit setup flow

`--setup`, `--first-run` and the menu action reuse the existing visible pages
through `internal/firstrun` and `Window.PresentFirstRun`; normal activation opens
no wizard. Details remain navigable within their parent step. Setup explicitly
starts OEM enablement; Next from Features applies the Bash-only Bluefin CLI
selection. Back/navigation alone installs nothing. Finish/dismissal persist
disposition; crashes record nothing. Preview writes nothing. The flow owner is
[`internal/firstrun`](../../internal/firstrun/), with window wiring in
[`internal/window`](../../internal/window/).

## Build and Release

[`Makefile`](../../Makefile) owns build/check commands;
[`.goreleaser.yaml`](../../.goreleaser.yaml) owns release contents. `make ci`
runs host-independent gates, including the supported architecture builds.
Desktop checks and screenshots use the pinned Dakota harness with private
Wayland/D-Bus sessions, never the developer's live desktop. See
[GTK headless testing](../skills/gtk-headless-testing/SKILL.md).

Homebrew installs the GUI release archive in user scope. OS images install its
helpers and matching policies at the fixed system paths and supply the stage
script themselves. Source installation uses `PREFIX=/usr` for that same
privilege boundary; maintainer defaults never overwrite administrator-owned
`/etc/chairlift/config.yml`. See
[ADR-0018](../adr/0018-distribute-via-homebrew-release-archives.md).
