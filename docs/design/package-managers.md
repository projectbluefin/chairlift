# Package Manager Wrappers

Provider boundaries for the shipped application. Use the linked source for API
signatures, command arguments, catalogs and timeout values; this document is
not a second reference manual. [Overview](overview.md) covers composition;
[AGENTS.md](../../AGENTS.md) holds contributor safety rules.

## Ownership and availability

| Owner | Boundary |
| --- | --- |
| [`homebrew`](../../internal/homebrew/) | User-scope Homebrew commands, bundles, metadata and package trust |
| [`flatpak`](../../internal/flatpak/) | Explicit user/system application and runtime operations |
| [`bootc`](../../internal/bootc/) | Unprivileged deployment observations and the fixed OS staging adapter |
| [`updex`](../../internal/updex/) | Library-backed feature reads and fixed-helper writes |
| [`ublue`](../../internal/ublue/) | Unprivileged host detection and fixed-helper system mutations |
| [`updateproviders`](../../internal/updateproviders/) | Update/cleanup adapters over those integrations, not another executor |

[`capability`](../../internal/capability/) owns the non-blocking tool/asset
floor shared by navigation and builders. Configuration may subtract from that
floor, never add capabilities. Runtime checks and service health belong in
workers, not synchronous GTK construction. A disabled group may have no widgets.

## Execution, privilege and previews

External commands run off the GTK main thread; widget changes return through
`sgtk.RunOnMainThread`. Callers retain cancellation/deadline ownership. Shared
execution plumbing must not be copied into a provider or view:

- [`helperexec`](../../internal/helperexec/) runs the updex and ublue helpers,
  classifies failures and journals invocations/outcomes. After authentication,
  an unprivileged caller cannot kill the root helper: it waits for the helper
  to exit, keeping action gates and update admission held. The helper's actual
  exit status wins over a racing caller cancellation or deadline.
- [`stageexec`](../../internal/stageexec/) owns OS staging output, cancellation,
  completion and channel closure. Views render streams with bounded/coalesced
  output rather than one permanent widget and callback per line.
- [`pkexec`](../../internal/pkexec/) owns the escalation program name and
  authentication-dismissal classification. Exit 126 or “Request dismissed” is
  cancellation; exit 127 is a failure, not a canceled prompt.

Root mutations use only the installed, fixed PolicyKit/helper pairs:

| Client | Fixed privileged entry point |
| --- | --- |
| bootc staging | `/usr/libexec/bootc-update-stage` supplied by the OS image |
| updex writes | `/usr/bin/chairlift-updex-helper` |
| Bluefin-family writes | `/usr/bin/chairlift-helper` |

Helper argv is validated against closed command shapes. Neither arbitrary
commands/paths nor image references or usernames cross the ublue boundary;
the helper derives targets and the invoking account from trusted host state.
The GUI's installed-helper support check matters because GUI and helpers ship
separately. Do not infer support from the GUI version.
[ADR-0001](../adr/0001-fixed-path-pkexec-privilege-boundary.md) records the
privilege boundary; helper parsers and policies own the accepted operations.

Flatpak system operations use Flatpak's own authorization, never a ChairLift
pkexec wrapper. Homebrew trust, user services and desktop settings stay
unprivileged. There are no passwordless ChairLift PolicyKit rules.

Every mutation respects [`dryrun.Enabled`](../../internal/dryrun/). Preview
suppresses commands and persistent writes, but read-only observations may still
run. A suppressed operation can return nil: callers must not treat that as
installation, remove inventory, change observed switches, or announce live
success. Failure and preview preserve known state and retryable controls.

Helper-side command budgets and GUI authentication allowances have separate
owners: [`ubluehelper`](../../internal/ubluehelper/ubluehelper.go) and
[`updexhelper`](../../internal/updexhelper/updexhelper.go) bound execution;
[`ublue`](../../internal/ublue/ublue.go) and
[`updex`](../../internal/updex/updex.go) add time for the PolicyKit prompt.
Image/bottle downloads and updex updates have download-sized budgets, not the
shorter configuration/read budget. A GUI upgrade cannot lengthen an older
installed helper's own timeout.

With `CHAIRLIFT_ACTION_JOURNAL` enabled, privileged dispatch records invocation
and live outcome. Helper-reported executed argv is reported evidence, not
independent execution proof; dry-run records suppression, not success.

## Unified updates

[`updateflow.Coordinator`](../../internal/updateflow/coordinator.go) owns source
inventories, aggregate phase, restart state, badge and mutation admission.
[`updateproviders`](../../internal/updateproviders/) implements its providers:

| Source | Adapter | Existing execution owner |
| --- | --- | --- |
| Applications | [Flatpak](../../internal/updateproviders/flatpak.go) | Flatpak, in the pending installation scopes |
| Developer tools | [Homebrew](../../internal/updateproviders/homebrew.go) | Homebrew metadata refresh/upgrade; fixed Docker update helper when pending |
| System components | [updex](../../internal/updateproviders/updex.go) | updex helper |
| Printer drivers | [printer apps](../../internal/updateproviders/printerapp.go) | Signature-verified user quadlet refresh |
| Operating system | [OS](../../internal/updateproviders/os.go) | bootc staging, never a new `bootc upgrade` route |

Checks run concurrently; applies run serially in provider order. Configuration,
host support, provider availability and user preference remain distinct facts.
The window supplies policy; providers report runtime unavailability rather than
inventing an empty successful check.

Failed observations preserve confirmed inventory. Partial checks name uncovered
work; partial applies retain failed items so Retry does not replay successful
mutations. Preview changes no inventory. An unchanged apply is not completion.
Source-specific check boundaries stay in their adapters:

- [Flatpak](../../internal/updateproviders/flatpak.go) offers found updates
  alongside named warnings only when the failures are exclusively per-remote.
  With no found updates, the original errors remain actionable failures;
  scope-discovery or cancellation errors fail the check even beside updates.
  Post-apply reconciliation must still succeed for every applied scope and
  verify the specific applied refs disappeared, not that the whole scope is
  empty: a skipped remote cannot prove completion.
- [updex](../../internal/updateproviders/updex.go) retains found feature updates
  beside component warnings and reports partial coverage, not a failed check.
  A top-level read failure remains an error; each affected feature counts once.
- [Homebrew](../../internal/updateproviders/homebrew.go) excludes pinned
  formulae from pending updates because bulk upgrade deliberately skips them.
  The Docker runtime item is an advisory comparison with the installed
  manifest; its fixed update helper re-verifies the signed index before acting.

OS restart state comes from observed deployments, not merely a successful
process exit.

[`UpdateShell`](../../internal/views/update_shell.go) renders snapshots through
[`updatepresent`](../../internal/views/updatepresent/); it owns no parallel
update counts or phase decisions. Individual updates, metadata refresh and OS
mutations use the same admission boundary. Restart is a separate Operating
System row action, not an automatic final step.

## Homebrew and Flatpak

[`homebrew/executable.go`](../../internal/homebrew/executable.go) is the single
Homebrew resolution owner: PATH first, then the Linuxbrew fallback. Execution
and availability share it. Child tools receive the resolved prefix's `bin` and
`sbin` through `WithBrewPath`; direct binary launches must work without the
wrapper's shell environment. The Homebrew runner cancels its process group so
child processes do not outlive a canceled operation.

Bundle parsing belongs to [`internal/homebrew`](../../internal/homebrew/);
selection and install planning belong to
[`bundleview`](../../internal/views/bundleview/entries.go), with execution in
[`bundle_install.go`](../../internal/views/bundle_install.go). Collections
select entries, not whole files: uninstalled supported entries start unchecked,
installed entries are checked and insensitive, and Select All is one-way.
Observed qualified package names, taps and Flatpak origins determine installed
state/source; failed inventory reads preserve that manager's known state.
This does not add a Flatpak inventory group to Apps.

Only a fully read, safe collection whose every entry is supported and checked
may execute its Brewfiles through `brew bundle`. Unknown directives, arbitrary
Ruby/options, truncation or read failures forbid that path. Otherwise only
selected pending entries install, deduplicated and batched per manager;
unsupported App Store/VS Code entries never execute implicitly. Flatpaks use
the existing system Flathub remote. Monospace Fonts shares this entry-selection
contract under Developer Mode, falling back to Apps when that group is disabled;
setup navigation preserves choices without installing them.

Trust remains per-user. Per-entry installs prepare missing taps and trust only
the selected package unless the collection explicitly declares that tap
`trusted: true`; existing trust is reused and changes are disclosed. Selecting
one package must not silently trust its entire tap. For an update failure,
use the [recovery procedure](../reference.md#recovering-a-failed-developer-tools-update)
rather than treating a reported workaround as a universal prerequisite.

Flatpak preserves installation scope and ref kind: applications and runtime
extensions are not interchangeable. System installs use existing system remotes;
feature setup does not add a per-user Flathub to work around a missing remote.
Removal targets observed scopes and retains image-shipped defaults where the
owning feature requires that protection.

## OS and system features

[`bootc`](../../internal/bootc/) reads status/checks without authentication.
Composefs hosts use world-readable deployment state and registry digest checks;
other hosts use bootc reads. Staging always retains the fixed OS-supplied entry
point. A current composefs deployment is checked before invoking a stager that
would otherwise fail on a no-op.

[`updex`](../../internal/updex/updex.go) reads through the Go library and requires
actual feature definitions for availability. An incomplete feature check is not
“up to date.” Writes remain behind the updex helper.

Image/channel/driver mappings belong to [`imageinfo`](../../internal/imageinfo/)
and trusted system channel tables, not user-writable configuration. Registry
readers do not authorize switch targets. The helper's day-word pin operations
remain separate from the GUI, which offers no published-versions calendar.

## Developer workstation and gaming components

[`devtools`](../../internal/devtools/) owns WSL backends, Docker's GUI-side
operations and editor setup; [`devsetup`](../../internal/devsetup/) sequences
promotion and optional follow-ons. Developer access must be confirmed before
child setup starts. Later optional failures never revoke access; retry keeps
completed work. Feed staging is an opt-in manual import, described in the
[feed curation procedure](../specs/developer-feeds.md).

System Docker uses image units when present. Otherwise the fixed helper uses
[`dockerbrew`](../../internal/dockerbrew/) to fetch signed Homebrew metadata,
verify bottles and install a root-owned runtime. Root never executes or loads
from the user-owned Homebrew prefix. CLI tools remain user-scope Homebrew;
disable retains runtime/data/access. See
[ADR-0024](../adr/0024-rootful-docker-from-verified-homebrew-bottles.md).

[`dockerbrew.Install`](../../internal/dockerbrew/install.go) serializes runtime
swaps, restart-marker changes and pruning with service activation under one
lock. Managed units start through `active/bin`, pinning the set adopted from
`current` at startup rather than following later swaps. Pruning keeps that
active set, so a deferred restart cannot remove tools the running daemon uses.
No caller holds the lock while waiting for systemd start/restart. Before an
existing runtime is changed, managed units are prepared and loaded; admission
under the install lock must prove a stable stopped service or a running daemon
whose actual process/invocation/PATH uses the retained active set.
[`CheckDockerRuntime`](../../internal/ubluehelper/docker_runtime.go) refuses
legacy, transitional or unreadable evidence rather than stopping workloads.
The GUI offers the explicit administrator stop prerequisite, including when
NoAccess makes the switch read off.

Running containers, or an inconclusive workload probe, defer a changed
runtime's restart. The active set remains usable and a later start adopts
current. The restart-pending marker names its set and is cleared under the
install lock only for that set (or an empty legacy marker), so an overlapping
newer install cannot lose its pending restart. Helper restart/admission policy
lives in [`ubluehelper`](../../internal/ubluehelper/docker.go), not the GUI.

WSL uses an explicit KVM permission floor and the fixed helper for access grants;
new-login requirements and observed machine state are not installation success.
Game Mode is an image-only switch through
[`ublue.SetGamingMode`](../../internal/ublue/ublue.go) and the fixed
`gaming-switch` helper, preserving the published stream and graphics driver.
It shares update-shell admission and requires reboot confirmation. The optional
Flatpak Gaming Tools control/backend is removed: existing user/system Flatpaks,
including apps installed by older releases, are neither inventoried nor removed
by Game Mode.

Agent Mode is an owned llmman user service in [`aistack`](../../internal/aistack/),
not a container stack. Readiness is an observed loopback health response.
[`agentmode`](../../internal/agentmode/) and
[`troubleshoot`](../../internal/troubleshoot/) launch Goose in ChairLift's own
profile, never the user's Goose configuration. No privileged route is involved.
Printer services have a separate [loopback/signature boundary](printer-applications.md).

## Typed cleanup and configured scripts

[`updateproviders/maintenance.go`](../../internal/updateproviders/maintenance.go)
is the shared routine-cleanup owner for Free Up Space and post-update
maintenance. Both use the same configuration key. Missing providers are skipped,
not cleaned; cancellation is not success. Reclaimed space is reported only from
valid before/after observations. Configured maintenance scripts remain a
separate explicit action, not an implicit provider precondition.

## View-layer page presentation (`internal/views/pageview`)

[`pageview`](../../internal/views/pageview/) holds pure display decisions;
GTK wiring stays in views. Refresh generations reject stale workers and shared
action gates serialize mutations without discarding confirmed state.
[ADR-0007](../adr/0007-pure-leaf-packages-route-around-untestable-gtk.md) explains
the headless-test seam.

### View-layer toast and decision helpers (`internal/views/actionmsg`, `internal/views/trustmsg`)

[`actionmsg`](../../internal/views/actionmsg/) and
[`trustmsg`](../../internal/views/trustmsg/) keep live/preview feedback and
follow-on decisions together. Reuse these owners rather than independently
wording success and deciding whether state changed.

## Install-path consistency (`internal/installcheck`)

[`Makefile`](../../Makefile), [release configuration](../../.goreleaser.yaml)
and [PolicyKit policies](../../data/) own installation details. Release archives
carry helpers and policies for OS integration; a user-scope Homebrew GUI install
cannot install root-owned files. The supported system prefix is `/usr`, matching
the fixed helper paths and PolicyKit action annotations. The OS supplies its own
trusted staging helper. Packaging rationale is in
[ADR-0018](../adr/0018-distribute-via-homebrew-release-archives.md).
