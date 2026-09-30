# Plan: Ship the premade Goose configuration

## Phase 1 — Ship the configuration

- Common owns `/usr/share/ublue-os/goose/config.yaml`.
- Keep the `linux-mcp-server` extension key, use the stable absolute Linuxbrew
  command, and pass `--toolset FIXED --no-search-for-ssh-key --verify-host-keys`.
- Leave provider selection to Goose and preserve existing user configuration.
- **Done when:** `ujust install-ai-tools` installs the premade configuration for
  a fresh user without adding a provider or SSH key.

## Phase 2 — Use it from ChairLift

- Recognize Common's extension and the existing `linux-tools` key.
- Copy the shipped configuration on Set Up instead of running a second setup
  script. Never overwrite an existing Goose configuration.
- Keep the existing desktop launch; no new launcher or runtime coordinator.
- **Done when:** Set Up uses the shipped configuration and a configured user
  can start Goose from the existing Help row.

## Phase 3 — Verify

- Run the complete affected Go and Bats modules, `make ci`, and Common's checks.
- Smoke fresh setup and preservation of an existing config in isolated paths.
- **Done when:** both setup paths agree, safety arguments are explicit, and
  existing provider/model configuration remains unchanged.

## References

- [Architecture](../design/overview.md#enhanced-troubleshooting)
- [Package-manager seams](../design/package-managers.md)
