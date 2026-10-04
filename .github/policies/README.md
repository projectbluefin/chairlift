# Repository policies

This directory holds ChairLift's machine-readable QA governance policy.
The catalog-bound issue lifecycle is configured separately by
[`issue-policy.json`](../issue-policy.json) and [`prow.yaml`](../prow.yaml),
consumed by the managed first-party
[`issue-lifecycle.yml`](../workflows/issue-lifecycle.yml) caller. The secret-free,
read-only [`issue-policy-preview.yml`](../workflows/issue-policy-preview.yml)
previews catalog changes and full-history migration without applying them.
The [local lifecycle procedure](../../docs/skills/issue-lifecycle/SKILL.md)
defines maintainer acceptance, independent gates, native assignment and delivery
verification; descriptive labels and Prow commands do not authorize implementation.

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
