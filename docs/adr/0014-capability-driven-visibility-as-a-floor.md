# 0013 — Capability-driven visibility as a floor

- **Status:** Proposed
- **Date:** 2026-09-20

## Context

ChairLift's navigation items (`internal/navigation`) and preferences groups
(`internal/config.SchemaGroups`) are currently gated by static configuration
via `config.IsGroupEnabled(page, group)`. At the same time,
`internal/autoupdate/autoupdate.go:43-46` declares ChairLift's intended policy:
"`StateUnavailable` means the unit is not installed. The switch is hidden
entirely rather than shown inert, matching how ChairLift hides every group
whose backing tool is absent."

In practice, absence of backing external tools (`brew`, `flatpak`, `podman`)
yielded inconsistent behaviors: some groups remained visible with inert
subtitles, others were disabled via error toasts, and only a few were omitted
entirely.

Furthermore, `window.go:118` executes synchronously on the GTK main thread
during window construction (`buildUI`). External process probes (such as
`brew --version` or `systemctl` checks with 5-second timeouts) cannot run on
the main thread without causing UI hangs and violating the repository invariant
that all external tool calls must run in goroutines.

## Decision

1. **Capability as a floor:** Group visibility is determined by the composition
   of static configuration and runtime host capability:
   `Visible = Configured && Available`. Static configuration may disable a
   supported feature, but cannot force an unsupported feature to render. The
   composed capability predicate is threaded through `views.New` and the
   `updateflow` coordinator map so sidebar navigation, view builders, and
   background workers share one authoritative availability floor.
2. **Synchronous page-level vs asynchronous group-level timing split:**
   - **Page-level capability:** Probed synchronously prior to `navigation.VisibleItems`
     using only non-blocking checks (`exec.LookPath`, `os.Stat`, environment
     variable reads). For Homebrew, the probe checks `LookPath("brew")` and falls
     back to `/home/linuxbrew/.linuxbrew/bin/brew`; the discovered absolute
     executable path is retained and consumed by `internal/homebrew.runBrewCommandAt`
     so visibility and command execution never diverge on direct binary launches.
   - **Group-level capability:** May be probed asynchronously in worker
     goroutines. Following PR #177, groups that depend on async probes are
     constructed as hidden shells and revealed only upon positive confirmation,
     preventing UI flashing and "construct-then-hide" artifacts.
3. **No mid-session flapping:** Capabilities are resolved once upon initial check
   and remain immutable for the session, preserving keyboard accelerators
   (`Alt+N`) and navigation indices.
4. **Pure leaf package:** Capability evaluation logic lives in a pure,
   headless-testable package under `internal/capability/` without importing
   `puregotk`.

## Consequences

- Inactive, inert placeholder expanders and subtitles for missing tools are
  eliminated across `internal/views` and `internal/updatepresent`.
- `internal/navigation` remains completely unchanged and pure, continuing to
  accept `enabled func(page, group string) bool`.
- `make screenshots` on headless or minimal CI environments requires an explicit
  e2e stub (`CHAIRLIFT_CAPABILITIES`) conforming to the capped and centralized
  stub invariant in `AGENTS.md` and `internal/installcheck/channels_test.go`.
- The Help page becomes the designated location to document features that are
  unavailable on the current host.

## Alternatives considered

- **Static configuration override (allowing config to force visible):** Rejected
  because rendering groups whose backing binaries are missing produces inert or
  failing controls.
- **Synchronous process execution at startup:** Rejected because multi-second
  timeouts on missing tools freeze the GTK main loop during window initialization.
- **Dynamic re-probing during session:** Rejected because dynamically adding or
  removing sidebar rows alters compacted `Alt+N` accelerators during user
  interaction.

## References

- Shapes: [design/overview.md](../design/overview.md)
- Builds on: [ADR-0003](0003-two-tier-config-with-fail-closed-semantics.md), [ADR-0007](0007-pure-leaf-packages-route-around-untestable-gtk.md), and the `AGENTS.md` capped-stub invariant
