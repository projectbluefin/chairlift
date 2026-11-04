# Documentation

Read the page that answers the task; this is not a required reading list.

| Need | Reference |
| --- | --- |
| User overview, installation, shortcuts | [index.md](index.md) |
| Screens and supported features | [walkthrough.md](walkthrough.md) |
| Configuration | [CONFIG.md](../CONFIG.md) |
| Troubleshooting and behavior reference | [reference.md](reference.md) |
| Architecture and ownership | [design/overview.md](design/overview.md) |
| Provider execution and privilege boundaries | [design/package-managers.md](design/package-managers.md) |
| Printer operation and limitations | [design/printer-applications.md](design/printer-applications.md) |
| Feed catalog maintenance | [specs/developer-feeds.md](specs/developer-feeds.md) |
| Contribution, checks, review | [CONTRIBUTING.md](../CONTRIBUTING.md) |
| Specialized GTK/build procedures | [SKILL.md](SKILL.md) |
| CI and release signals | [quality.md](quality.md) |
| Metrics | [metrics.md](metrics.md) |
| AI security, risk and review | [SECURITY-AI.md](SECURITY-AI.md), [risk-tiers.md](risk-tiers.md), [review-rubric.md](review-rubric.md) |
| Decision history | [adr/](adr/), [org-adrs.md](org-adrs.md) |

Update existing documentation when behavior changes its claims. Link the source
owner instead of copying its APIs or inventories. Do not create changelogs,
completed plans, audit snapshots, or mandatory learning records as living docs;
Git history and the issue tracker already hold that information. Accepted ADRs
remain decision history, not current implementation instructions.

Root `README.md` is human-owned. There is no requirement to update documentation
or skills for an ordinary fix that leaves the documented contract unchanged.
