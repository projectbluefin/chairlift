# Security Policy

## Supported Versions

ChairLift is developed against the current Bluefin-family images. Install the
latest [release](https://github.com/projectbluefin/chairlift/releases) and keep
it updated:

| Version | Supported      |
| ------- | -------------- |
| Latest release (Bluefin, Bluefin LTS, Dakota) | ✅ Yes |
| Older releases | ❌ No |

We recommend tracking the upstream release stream so you receive security
patches promptly. Older releases are no longer maintained and will not receive
security updates.

## Reporting a Vulnerability

ChairLift ships a privileged `pkexec` helper (`cmd/chairlift-ublue-helper`)
that runs as root, so a flaw in its input validation is high-impact. Thank you
for helping keep it safe.

**Please do not report security vulnerabilities through public GitHub issues.**

Instead, use GitHub's **Private Vulner Reporting** tool, which routes the
report directly to the maintainers under GitHub's security embargo policy:

<https://github.com/projectbluefin/chairlift/security>

If you cannot use that tool, email the maintainers privately and do not include
exploit details or secrets in any public place. Contact them through the
projectbluefin [community channels](https://github.com/projectbluefin) until a
publicly listed address is published.

### What to include

- A description of the issue and the affected version(s)
- Steps to reproduce, and a proof-of-concept if you have one
- Your assessment of the impact (e.g. privilege escalation at the helper
  boundary)

### What to expect

- We will acknowledge your report as soon as reasonably possible.
- We will keep you informed of progress and aim to resolve confirmed issues
  quickly.
- We will not publicly disclose exploit details until a fix is available, and
  we will credit reporters unless they ask to stay anonymous.

See [`docs/SECURITY-AI.md`](docs/SECURITY-AI.md#vulnerabilities-and-incidents)
for the project's policy on handling suspected vulnerabilities, secret exposure,
and unsafe automation discovered during AI-assisted work.
