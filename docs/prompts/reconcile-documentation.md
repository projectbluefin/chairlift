# Reconcile documentation

Use this prompt when behavior or configuration changed, or when documentation
may have drifted.

```text
Reconcile ChairLift's current-state documentation for [TOPIC].

Read AGENTS.md, docs/SKILL.md, docs/skills/documentation-reconciliation/SKILL.md,
and docs/documentation-consistency.md. Use live source for current behavior;
compare any remaining plan with its active issue scope before retaining it.

Trace the topic to its live sources:
- config.yml and internal/config for page/group keys and defaults;
- internal/navigation and page builders for visibility and shortcuts;
- go.mod for dependency versions;
- Makefile, .goreleaser.yaml, helper constants, and PolicyKit files for commands and install paths.

Update every relevant restatement in AGENTS.md, CONFIG.md, and docs/.
Root README.md is human-owned: report necessary corrections without editing it.
Accepted ADRs are immutable history; explain contradictions in living docs.
Remove retired plans after preserving durable contracts, and repair all inbound
links and indexes. Historical evidence belongs behind a pinned Git history link,
not in instructions claiming the old implementation still ships.

Search for old terms, source-path mentions, and contradictory claims rather than
only adding a new paragraph. Follow the existing local skill/frontmatter contract;
do not infer a generated catalog or workflow state from Common parity. Check
the current default-branch baseline: ChairLift consumes projectbluefin/actions
managed v1 through its local issue-lifecycle caller and policy catalog. Read
docs/skills/issue-lifecycle/SKILL.md, not Common's Common-only runtime commands,
for local intake, migration, independent Hive gates and delivery evidence.
After integration, run the applicable documentation gates and make ci once.
Report changed and reviewed-unchanged files, source evidence, unresolved active
scope, README corrections, and immutable ADR discrepancies.
```
