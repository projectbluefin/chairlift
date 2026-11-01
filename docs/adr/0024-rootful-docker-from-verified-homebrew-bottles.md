# 0024 — Install rootful Docker from verified Homebrew bottles

- **Status:** Accepted
- **Date:** 2026-10-26

## Context

Bluefin DX installs Docker CE RPMs, enables `docker.socket`, and adds
administrators to the `docker` group. Users of images without those RPMs, such as
Dakota, expect the same system Docker from **Enable Docker**. The change before
this one (#635) used image units only. Those hosts had no Docker. v26.10.10 had
shipped rootless Docker from the user's Homebrew instead. That version lacked
the system socket and `docker` group that DX users rely on.

The requirement fixes Homebrew as the package source. However, the Homebrew
prefix (`/home/linuxbrew/.linuxbrew`) belongs to the user. Root executing or
loading files from it would let any process in the user's session replace code
that root later runs. Homebrew bottles are relocatable archives. Their dynamic
executables carry `@@HOMEBREW_PREFIX@@` placeholders for the interpreter and
library search path, so they cannot run unmodified outside a Homebrew prefix.

Homebrew publishes a signed formula index at
`https://formulae.brew.sh/api/formula.jws.json`. It is a JWS (PS512, key id
`homebrew-1`) whose payload maps every formula to its version, dependencies, and
per-architecture bottle URL and SHA-256. GHCR addresses each bottle by that
SHA-256. `brew` verifies this signature before installing from the API.

## Decision

The root-side runtime comes from Homebrew's verified bottles, not the user's
prefix. The fixed `docker-enable` helper command (ADR-0001) does all privileged
work itself, without caller-supplied values:

1. It fetches the signed index and verifies it with the `homebrew-1` public key
   compiled into ChairLift (`internal/dockerbrew`). It reads nothing from the
   user's Homebrew.
2. It selects one fixed daemon formula set: `docker-engine`, `containerd`,
   `runc`, `tini`, `libpathrs`, and `libseccomp`. Their dependency closure must
   equal that set plus system-provided `nftables`. Each bottle URL must be the
   GHCR blob address of its indexed SHA-256.
3. It downloads each blob anonymously and rejects a body whose SHA-256 differs.
4. It extracts only the mapped regular files into a root-owned staging
   directory under `/usr/local/libexec/docker`. Traversal, absolute paths, and
   devices are refused. Dynamic ELF files get the system loader as `PT_INTERP`
   and `$ORIGIN/../lib` as their search path. Static binaries are unchanged.
5. It smoke-runs `runc --version` and `dockerd --version`, writes a
   world-readable manifest of the source bottles, then atomically repoints
   `/usr/local/libexec/docker/current`. Older sets are pruned.
6. It writes `docker.service` and `docker.socket` into `/etc/systemd/system`.
   Each begins with a `# Managed by chairlift-helper` marker. An existing unit
   without the marker is never overwritten. An image-provided
   `/usr/lib/systemd/system/docker.service` takes precedence: the helper then
   removes only its marked units and enables the image's Docker.
7. It creates the `docker` group when absent, adds the invoking account, and
   relabels the runtime when SELinux is enforcing. It then enables the socket
   and starts the service.

The client tools (`docker`, `docker-buildx`, `docker-compose`) stay ordinary
user-scope Homebrew formulae installed without privilege. Root never runs
them. Updates use a fifteenth helper command, `docker-update`. It repeats steps
1–5 and restarts an active daemon only when the bottle set changed. The
Updates page's Developer tools source offers it when Homebrew's unsigned
per-formula API reports newer bottles. That unprivileged hint selects nothing;
root installs only what the signed index names.

## Consequences

- Root executes only bytes whose provenance it checked itself against
  Homebrew's signing key and content-addressed blobs. A compromised user
  session can still use Docker as a `docker` group member. That is Docker's
  documented root equivalence, the same as DX's. It cannot replace the
  daemon root starts.
- Patching changes verified bytes, but deterministically. The manifest records
  the source SHA-256 of every bottle, so the installed set can be traced.
- A Homebrew key rotation, index format change, new dependency, or renamed
  bottle member fails closed: verification, selection, or extraction reports
  an error and nothing is swapped. Recovery requires a ChairLift release.
- Enable and update download the full signed index (~35 MB) plus six bottles.
  Their helper budget is the 30-minute image-switch timeout.
- Only `x86_64_linux` and `arm64_linux` bottles are supported.
- ChairLift now owns an ELF patcher and a small systemd unit set. These are new
  maintenance surfaces covered by `internal/dockerbrew`'s tests and the live
  `make e2e-docker` gate.
- Enable disables v26.10.10's rootless user daemon and resets the CLI's
  `rootless` context. Disable stops both system units but keeps the runtime,
  container data, and group membership.
- A changed runtime restarts the daemon only when no containers are running
  (or none can be ruled out). Otherwise the restart waits for the next enable
  or update, or a reboot, and the restart-pending marker carries it.

## Alternatives considered

- **Run the daemon from the user's Homebrew prefix as root:** rejected. Root
  would execute user-writable files.
- **Copy the user's installed files into a root-owned path:** rejected. The
  copy has no provenance root can check, and the user could alter it first.
- **Have root run `brew`:** rejected. Homebrew refuses root, and its own code
  lives in the user-owned prefix.
- **Trust the unsigned per-formula API or GHCR tags for installs:** rejected.
  Only the JWS index binds formula names to SHA-256s under a signature; TLS
  alone does not authenticate Homebrew's content. The unsigned API is used
  only for the unprivileged update hint.
- **Keep rootless Docker:** rejected for lacking DX parity (system socket and
  `docker` group).
- **Require image units only:** rejected because hosts without Docker RPMs
  would stay without Docker.

## References

- Builds on: [ADR-0001](0001-fixed-path-pkexec-privilege-boundary.md)
- Shapes: [design/package-managers.md — Developer workstation](../design/package-managers.md#developer-workstation-and-gaming-components),
  [design/overview.md — Privileged operations](../design/overview.md#privileged-operations)
- Related: [ADR-0018](0018-distribute-via-homebrew-release-archives.md)
