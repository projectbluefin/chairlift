# Review a change

Use this prompt for a branch or pull-request review.

```text
Review [BRANCH, COMMIT, OR DIFF] for correctness and regressions in ChairLift.

Read AGENTS.md, .knowledge/README.md, .memory/README.md, docs/SKILL.md, and
the matching package from docs/skills/ first. Inspect surrounding code and
tests, not only the diff. Prioritize findings over summary.

Check specifically for:
- privileged mutations bypassing the fixed pkexec helper and PolicyKit policy;
- external commands or widget updates running on the wrong thread;
- stale async workers replacing newer state;
- disabled configuration groups leaving nil widgets that later code dereferences;
- package actions losing known rows/counts on failure or dry-run;
- navigation, page visibility, and shortcut inventories drifting apart;
- tests skipped by the repository's TestI/Integration CI name filter;
- current-state docs contradicting source, config, go.mod, or install files;
- retired plan links presented as current authority, or accepted ADR history
  mistaken for the live implementation.
- issue lifecycle claims confusing trusted acceptance with Hive readiness,
  analysis consent, PR approval or delivery; verify the local issue-policy
  catalog and shared Actions caller, preserving independent human/App gates.

For each finding, give the file and line, explain the concrete failure mode, and suggest the smallest safe correction. Distinguish blocking defects from optional improvements. If there are no findings, say so and identify any testing gap that still limits confidence.
```
