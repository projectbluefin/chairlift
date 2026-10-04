# Project Bluefin factory

ChairLift's local navigation point for factory-assigned work. This page is
intentionally a link map; the linked documents are the authorities.

ChairLift is onboarded: its local catalog and default-branch callers consume
the shared `projectbluefin/actions` lifecycle at managed `v1`. Start with the
local contract and task router, then load the matching local package below.
Common remains the sidecar for cross-repository process, not a replacement
catalog or a reason to run Common-only migration commands here. Its
documentation-only direct-push exception does not bypass ChairLift's merge queue.

Assignment, queue, branch protection, and factory parity are live state, not a
table to freeze here. Check the current issue/PR, Hive assignment when relevant,
and GitHub repository settings before taking a state-changing action.

Issue acceptance, analysis preference, contributor assignment, Hive scheduling,
PR approval, and delivery are independent facts. An offered Hive queue item
does not establish trusted acceptance; preserve `human-only` and independent
gates before acting. A Homebrew app release does not prove delivery of
image-installed helpers or policies. The local lifecycle package owns both
boundaries and its source links; adoption alone does not prove every live
historical label assignment has been migrated.

| Topic | Authority |
| --- | --- |
| ChairLift contract | [`AGENTS.md`](../../AGENTS.md) |
| Local skill router | [`docs/SKILL.md`](../SKILL.md) |
| Local skill catalog | [`docs/skills/index.md`](../skills/index.md) |
| ChairLift lifecycle, catalog and Prow | [`issue-lifecycle`](../skills/issue-lifecycle/SKILL.md) |
| ChairLift canonical catalog | [`.github/issue-policy.json`](../../.github/issue-policy.json) |
| ChairLift Prow allowlist configuration | [`.github/prow.yaml`](../../.github/prow.yaml) |
| ChairLift deployed reconciliation caller | [`issue-lifecycle.yml`](../../.github/workflows/issue-lifecycle.yml) |
| ChairLift read-only policy preview caller | [`issue-policy-preview.yml`](../../.github/workflows/issue-policy-preview.yml) |
| Shared released runtime | [`projectbluefin/actions` lifecycle](https://github.com/projectbluefin/actions/blob/v1/.github/workflows/reusable-issue-lifecycle.yml) |
| ChairLift release and merge mechanics | [`factory-onboarding`](../skills/factory-onboarding/SKILL.md) |
| Common factory onboarding | [`docs/skills/factory-onboarding.md`](https://github.com/projectbluefin/common/blob/main/docs/skills/factory-onboarding.md) |
| Common agentic model | [`docs/factory/agentic-model.md`](https://github.com/projectbluefin/common/blob/main/docs/factory/agentic-model.md) |
| Common human decision gates | [`docs/skills/human-gates.md`](https://github.com/projectbluefin/common/blob/main/docs/skills/human-gates.md) |
| Common label workflow | [`docs/skills/label-workflow.md`](https://github.com/projectbluefin/common/blob/main/docs/skills/label-workflow.md) |
| Common skill improvement | [`docs/skills/skill-improvement.md`](https://github.com/projectbluefin/common/blob/main/docs/skills/skill-improvement.md) |
