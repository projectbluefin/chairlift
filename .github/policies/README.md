# Repository policies

This directory holds ChairLift's machine-readable QA governance policy.
Issues and pull requests are driven by Prow
([`prow.yml`](../workflows/prow.yml)); see
[how issues and PRs work here](https://github.com/projectbluefin/common/blob/main/docs/skills/label-workflow.md).

[`auto-qa-tuning.json`](auto-qa-tuning.json) defines allowed responses to changes
in [pull request acceptance](../../docs/metrics.md). It keeps required,
security, and coverage checks fixed; policy adjustments still require a pull
request and human review. It is a policy definition, not a scheduled metrics
collector or an automatic tuning service. The gate-enforced
[`internal/installcheck/autoqa_test.go`](../../internal/installcheck/autoqa_test.go)
verifies its schema version, signal definition, and required/security guardrails.

The human-readable policy sources remain:

- [`AGENTS.md`](../../AGENTS.md) for repository invariants;
- [`docs/SECURITY-AI.md`](../../docs/SECURITY-AI.md) for AI security
  boundaries;
- [`docs/risk-tiers.md`](../../docs/risk-tiers.md) for change
  classification; and
- [`docs/review-rubric.md`](../../docs/review-rubric.md) for approval
  standards.

Machine-readable policy never authorizes automation to approve, merge,
release, or deploy its own changes.
