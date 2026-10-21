# 0023 — Verify printer images before installing

- **Status:** Accepted
- **Date:** 2026-10-21
- **Supersedes:** ADR-0020's moving-image and auto-update decisions; its loopback boundary remains unchanged.

## Context

Printer application publishers sign their images with GitHub Actions keyless
identities. ADR-0020's moving tag and Podman auto-update do not check these
signatures on the receiving computer. Registry tag replacement can therefore
cause unchecked code to run in the user's session. Host container policy does
not supply the required workflow URI identity check.

Issue #582's recommendation A was approved by the maintainer through
`triage/accepted` (the approval notice records this on 2026-10-14).

## Decision

Verify before installation or refresh, not after execution. `Enable` and
`Refresh` run cosign with the GitHub Actions issuer and an anchored identity
expression for the selected family's publishing workflow. Keep cosign's
payload claim checking enabled. Parse only its successful JSON stdout and use
only the verified SHA-256 digest in the quadlet. Never resolve a tag again
between verification and runtime pull. Reject missing tools, verification
errors and malformed or inconsistent payloads before any filesystem or service
mutation. Mirrors and explicit pins retain the same signer requirement.

Do not emit `AutoUpdate=registry` or enable Podman's shared timer. Keep the
rootless account, state volume and loopback-only network boundary unchanged.

## Consequences

- Newly enabled printer applications run only a signature-verified image.
  A later tag change cannot alter their pinned image on restart.
- Missing cosign, network failures and rejected signatures prevent installation
  rather than silently falling back to an unchecked pull.
- This implementation is the first part of recommendation A. `Refresh` is a
  package API; scheduled verified refresh and Updates-page integration are
  not implemented. Users can disable/re-enable to receive a newer verified
  image. Existing units are not automatically migrated and still need that
  action; issue #582 remains open for migration and automatic verified updates.
- Verification failure during refresh preserves the existing unit and running
  service. Later reload/restart failure is reported but does not restore an
  old unit or image. Disabling leaves printer state intact.
- Signature verification proves the publishing identity, not that a compromised
  publishing workflow produced safe code. No additional SBOM or provenance
  attestation checks are claimed here.

## Alternatives considered

- **Check after Podman auto-update:** rejected because unchecked code runs
  before a failed check can stop it.
- **Document the risk only:** rejected by the approved direction; rootless
  execution still grants access to the user's network and printer state.
- **Disable the shared Podman timer:** rejected because other user containers
  may rely on it. Removing this application's auto-update label is sufficient
  for newly written units.

## References

- Builds on: [ADR-0020](0020-printer-apps-loopback-only-on-moving-stable-tag.md)
- Shapes: [Printer applications](../design/printer-applications.md)
- Approved direction: [issue #582](https://github.com/projectbluefin/chairlift/issues/582#issuecomment-6177236834)
