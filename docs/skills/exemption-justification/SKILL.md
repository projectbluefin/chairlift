---
name: exemption-justification
description: Use when adding, keeping, or reviewing an exemption entry in a repository gate.
version: 1.0.0
last_updated: 2026-09-18
tags:
  - testing
  - authorization
metadata:
  type: reference
---

# An exemption whose stated reason is false is worse than no exemption, because the gate then certifies a property that is untrue at exactly the site the exemption covers

**When it applies:** Writing or reviewing any `internal/installcheck` gate —
or any other allowlist, skip list, or exemption map — that asserts a
repository-wide property and carves out one or more files from it. The risk
peaks when the carve-out's justification is "another test already covers this
site."

**What to do:** Treat an exemption's comment as a claim to be checked, not as
documentation. Open the test it names and confirm it really scans the exempted
file and really asserts the property. `internal/views/pageview/wiring_test.go`
is the exact trap: it computes `viewsDir` as the *parent* of its own directory
and table-drives over six page builders only —
`applications_page.go`, `updates_page.go`, `maintenance_page.go`,
`features_page.go`, `help_page.go`, `system_page.go`. It never reads
`internal/views/pageview/pageview.go`, and its one `"pkexec"` fragment sits in
the `retired` list for `maintenance_page.go`, where it asserts the pattern is
*absent* from a different file. A grep hit for the right string in a plausible
test proves nothing about the site you are exempting.

This is the companion failure to the one in
`docs/skills/frozen-allowlists/SKILL.md`: there the authorization is real but
narrower than the change assumes, here the entry exists but its justification
is not true at all. Read both before touching a gate's carve-out list.

Prefer deleting the exemption to explaining it. Where the exempted site can
simply adopt the owner symbol, do that: in PR #50's revision, once
`pageview.MaintenanceCommand` returned `Command{Name: pkexec.Command, ...}`
instead of the bare literal, the exemption map was empty, and the map, the
unused-entry detection, and the staleness loop around it became speculative
generality to delete.

That pull request is still open and held at the Security Gate as of
2026-09-18, so nothing it describes — `internal/pkexec`, `pkexec.Command`,
`internal/installcheck/pkexecowner_test.go` — exists on `main`. Read the rest
of this package as review guidance drawn from a proposed change, and verify
the current state of any symbol it names before relying on it.

When an exemption genuinely must stay, it must name the concrete force that
requires it and be verifiable from the repository in under a minute — and if
the exempted site is the security-relevant one, say so in the entry rather than
leaving a reader to discover it. The single literal exempted in that draft was
`pageview.MaintenanceCommand`, which feeds `UserHome.runMaintenanceAction` in
`internal/views/maintenance_page.go` — the one escalation that
`internal/installcheck/journalcontract_test.go` and `docs/design/overview.md`
both record as having *no* dedicated polkit action and falling back to
`org.freedesktop.policykit.exec`, the broadest privilege in the application. A
"single owner" invariant enforced by CI while being untrue at the most
privilege-sensitive call site is not a weaker gate than none; it is an actively
misleading one.

**Learned from:** PR #50 (`arch/refactor-pkexec-single-owner`, open and
security-gated at the time of writing), which proposes `internal/pkexec` and
`internal/installcheck/pkexecowner_test.go`, an AST gate failing on any
`"pkexec"` string literal in non-test Go under `internal/` and `cmd/`. Its
first draft exempted
`internal/views/pageview/pageview.go` on the stated grounds that
`wiring_test.go` asserted on that call site's source text; it does not. The
file's own header said nothing could be added without a reason a reader could
check, and its only entry broke that. The exemption was removed rather than
reworded.
