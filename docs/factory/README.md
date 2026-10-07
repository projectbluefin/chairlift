# Project Bluefin factory

ChairLift's local navigation point for factory-assigned work. This page is
intentionally a link map; the linked documents are the authorities.

Issues and pull requests are driven by Prow
([`prow.yml`](../../.github/workflows/prow.yml)), with approvers from
[`OWNERS`](../../OWNERS) and labels from the org config in
`projectbluefin/.project`. Start with the local contract and task router, then
load the matching local package below. Common remains the sidecar for
cross-repository process. Its documentation-only direct-push exception does not
bypass ChairLift's merge queue.

Assignment, queue, branch protection, and factory parity are live state, not a
table to freeze here. Check the current issue/PR, Hive assignment when relevant,
and GitHub repository settings before taking a state-changing action.

Issue acceptance (`triage/accepted`), the `needs-human` gate, contributor
assignment, Hive scheduling, PR approval, and delivery are independent facts.
An offered Hive queue item does not establish acceptance. A Homebrew app
release does not deliver image-installed helpers or policies; those ship with
the OS image.

| Topic | Authority |
| --- | --- |
| ChairLift contract | [`AGENTS.md`](../../AGENTS.md) |
| Local skill router | [`docs/SKILL.md`](../SKILL.md) |
| Local skill catalog | [`docs/skills/index.md`](../skills/index.md) |
| Prow workflow | [`prow.yml`](../../.github/workflows/prow.yml) |
| Prow approvers | [`OWNERS`](../../OWNERS) |
| ChairLift release and merge mechanics | [`factory-onboarding`](../skills/factory-onboarding/SKILL.md) |
| Common agentic model | [`docs/factory/agentic-model.md`](https://github.com/projectbluefin/common/blob/main/docs/factory/agentic-model.md) |
| Common human decision gates | [`docs/skills/human-gates.md`](https://github.com/projectbluefin/common/blob/main/docs/skills/human-gates.md) |
| Common workflow page (issues, PRs, labels, Prow) | [`docs/skills/label-workflow.md`](https://github.com/projectbluefin/common/blob/main/docs/skills/label-workflow.md) |
| Common skill improvement | [`docs/skills/skill-improvement.md`](https://github.com/projectbluefin/common/blob/main/docs/skills/skill-improvement.md) |
