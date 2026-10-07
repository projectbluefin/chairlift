## Summary

<!-- Explain what changed and why. Keep the pull request focused on one concern. -->

-

## Related issue

<!-- Use Closes #N when this pull request resolves the issue, Refs #N when it is only related. See how issues and PRs work here: https://github.com/projectbluefin/common/blob/main/docs/skills/label-workflow.md -->

Closes #

## Delivery

<!-- For product fixes, name the affected Homebrew app and any image-installed helpers. Image-installed helpers reach users through the OS image, not the Homebrew release. For code-only work, write "not applicable". -->

- Affected app/package or image-installed component:
- Release/delivery remaining steps:

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
