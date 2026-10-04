# ChairLift skill router

Read [`AGENTS.md`](../AGENTS.md) first. Load only the package matching the task,
then follow its source links when that branch needs them. The exhaustive local
catalog is [`skills/index.md`](skills/index.md); it is hand-maintained, not a
generated Common catalog.

| Task | Load |
| --- | --- |
| Start factory-assigned work, submit through the merge queue, or cut a release | [factory-onboarding](skills/factory-onboarding/SKILL.md) |
| File/triage a report, accept/assign work, change labels, or verify delivery | [issue-lifecycle](skills/issue-lifecycle/SKILL.md) |
| Reconcile docs, retire plans, or resolve conflicting current-state claims | [documentation-reconciliation](skills/documentation-reconciliation/SKILL.md) |
| Change a documented invariant or package inventory | [agent-contracts](skills/agent-contracts/SKILL.md), [leaf-package-inventory](skills/leaf-package-inventory/SKILL.md) |
| Plan acceptance checks or split implementation work | [automated-acceptance](skills/automated-acceptance/SKILL.md), then the planning entries in the catalog |
| Add unit tests, report coverage, or drive the GTK/AT-SPI suite | [gated-test-placement](skills/gated-test-placement/SKILL.md), [gtk-headless-testing](skills/gtk-headless-testing/SKILL.md) |
| Validate configuration, precedence, or YAML keys | [canonical-schema](skills/canonical-schema/SKILL.md), then the validation entries in the catalog |
| Render streamed external output | [bounded-stream-rendering](skills/bounded-stream-rendering/SKILL.md) |
| Change a container digest or the Dakota E2E image | [multi-arch-digest-pinning](skills/multi-arch-digest-pinning/SKILL.md) |
| Change a Make target | [phony-target-shadowing](skills/phony-target-shadowing/SKILL.md) |
| Preserve a durable lesson or edit a skill | [skill-improvement](skills/skill-improvement/SKILL.md) |

For cross-repository factory and lifecycle work, follow
[`factory/README.md`](factory/README.md) to Common's live procedures and verify
ChairLift's local implementation before changing state. Common's documentation-
only direct-push exception is not authority to bypass ChairLift's merge queue.
