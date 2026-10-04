## Summary

<!-- Explain what changed and why. Keep the pull request focused on one concern. -->

-

## Related issue

<!-- Use Refs for product reports awaiting delivery and reporter verification. Use Closes only for code-only work whose acceptance criteria are met at merge, or an already delivered and verified report. See docs/skills/issue-lifecycle/SKILL.md. -->

Refs #

## Delivery and verification

<!-- For product fixes, identify the actual fix, affected Homebrew app version and any separately image-installed helpers. A release or merged reference is not proof that the reporter can consume the fix. For code-only work, explain why merge completes acceptance. -->

- Affected app/package or image-installed component:
- Release/delivery owner and remaining steps:
- Reporter verification steps (or why not applicable):

## Change classification

<!-- Select the highest applicable tier from docs/risk-tiers.md. -->

- Risk tier:
- Rationale:

## Validation

<!-- List exact commands and results. Explain any check that could not run. -->

- [ ] `make ci`
- [ ] Additional relevant checks (including `make e2e` when applicable):

## Factory Self-Improvement & Two-Output Rule

<!-- Every task has two outputs: the work and the learning. -->

- [ ] Discovered a workaround, pattern, or convention?
- [ ] Skill package in `docs/skills/` updated (or created) in this same PR, or verified correction recorded in `.memory/`?

## Tests and documentation

<!-- Describe regression coverage for changed behavior, or explain why it is not needed. -->

- Tests:
- Documentation:

## Review readiness

- [ ] Title follows Conventional Commits format (`feat:`, `fix:`, `docs:`, `ci:`, etc.).
- [ ] AI-authored commits include attribution trailers (`Assisted-by:` / `Co-authored-by:`).
- [ ] The diff contains no unrelated refactors, session logs, or generated artifacts.
- [ ] Changed behavior has CI-enforced regression coverage, or this change does not alter behavior.
- [ ] Relevant repository invariants in `AGENTS.md` remain intact.
