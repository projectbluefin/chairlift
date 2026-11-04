# Documentation changes

Update a guide when a scoped change makes its existing claims inaccurate.
Do not require a documentation edit for every source change.

- Verify configuration against `internal/config` and `config.yml`; built-in
  defaults and image overrides are different inputs. [CONFIG.md](../CONFIG.md)
  is the configuration reference, not a second schema to maintain elsewhere.
- Verify commands, privilege paths, and install layout against live callers,
  `Makefile`, `.goreleaser.yaml`, and the matching PolicyKit policies.
- Prefer links to owners over copied versions, symbol inventories and code.
- Keep accepted ADRs as decision history. When retiring a referenced artifact,
  repair a historical citation with a revision-pinned history link rather than
  retaining an obsolete design or rewriting the decision.
- Fix inbound links when deleting docs. Do not replace removed material with
  another archive, correction ledger, audit snapshot, or compulsory skill update.
- Root `README.md` is human-owned. Report drift there rather than editing it.

Check the affected links and relevant gates. A test pinning incidental prose or
requiring duplicate inventories is not a reason to retain that prose.
