# Documentation Consistency Checklist

Current behavior is checked against live source, not old plans.
[ADR-0019](adr/0019-retire-obsolete-docs-to-git-history.md) preserves retired
proposals in Git history rather than as parallel instructions, superseding
[ADR-0010](adr/0010-docs-are-a-ci-gated-artifact.md)'s in-tree archival retention.
The original requirement to verify current-state documentation remains.

Use this checklist whenever behavior, configuration, dependencies, or
installation layout changes:

- Compare documented page/group keys with `config.yml`, `Config`, and the
  actual `IsGroupEnabled` guards. Run
  `go test ./internal/config ./internal/navigation`.
- Copy dependency versions from `go.mod`; do not retain a version claim from a
  plan or release note.
- Trace optional-tool visibility in the page builder and its async loader.
  Distinguish static config-driven page omission from runtime group hiding,
  unavailable placeholders, and visible error/status rows.
- Check commands and installation destinations against `Makefile`,
  `.goreleaser.yaml`, fixed helper constants, and PolicyKit annotations. Run a
  `make -n install` dry run when installation text changes.
- Current-state claims belong in `README.md` (human-owned), `CONFIG.md`,
  `AGENTS.md`, `CONTRIBUTING.md`, `SECURITY.md`, `docs/index.md`,
  `docs/reference.md`, `docs/walkthrough.md`, `docs/design/`, `docs/specs/`,
  and the process/quality docs indexed in `docs/README.md`. Reconcile every
  affected restatement, not just the first occurrence. Agents do not edit the
  root `README.md`; report any drift there for a human.
- Preserve accepted `docs/adr/` decisions and historical skill evidence.
  Living docs describe deviations and the current implementation. Repair a
  removed artifact's link to its revision-pinned Git history; do not silently
  rewrite a decision or treat an old source-line citation as current API syntax.
- Keep `docs/plans/` for active, issue-owned work only. Completed or superseded
  plans, the Go-port proposal, and the old superpowers artifacts are retired;
  their history remains in Git. Move durable contracts to the existing design
  or spec and operating lessons to the canonical skill before removing a plan.
  Unscheduled ideas stay in the issue tracker, not a "Later" backlog here.
- Follow [Common's skill authoring patterns](https://github.com/projectbluefin/common/blob/main/docs/skills/write-a-skill.md):
  task-specific routing, short procedures, canonical links, and executable
  verification of project facts. ChairLift's adopted catalog-bound lifecycle
  remains authoritative; link its shared runtime rather than duplicating it.
- Check every inbound local link when moving or removing a document. Verify
  links resolve, source citations exist, and the index names retained docs.
- Run `make ci`, then search current-state documentation for the obsolete term,
  key, version, or path that prompted the change. Report commands actually run.
