@updates @env.CHAIRLIFT_CAPABILITIES=image-descriptor,flatpak,brew,podman
Feature: Updates
  The Updates destination is the status-first update shell: one status line,
  one primary action, and a row per update source. Flatpak and Homebrew are
  faked (fixtures/stubs_updates.py) so every source reports a known state.
  The capability set omits bootc-stage so the Operating system source is
  floored identically on every host; its staging path needs the fixed
  /usr/libexec/bootc-update-stage, which a hosted runner cannot provide.

  @stub.updates-flatpak-one-update @stub.updates-brew-current
  Scenario: A pending Flatpak update is summarised per source
    Given ChairLift is running
    Then the Updates status reads "Updates available"
    And I see "1 update is available."
    And the Updates page offers only the "Update all" action
    And the "Update all" button is sensitive
    And the "Applications" row says "1 update available"
    And the "Firefox" row says "Available: 131.0"
    And the "Developer tools" row says "Up to date"
    And the "System components" row says "Not available on this system"
    And the "Operating system" row says "Not available on this system"
    And the Updates sidebar badge shows "1"

  @stub.updates-flatpak-current @stub.updates-brew-current
  Scenario: With nothing pending the shell offers a fresh check
    Given ChairLift is running
    Then the Updates status reads "System is up to date"
    And I see "No updates are available. Last checked at"
    And the Updates page offers only the "Check again" action
    And the "Applications" row says "Up to date"
    And the "Developer tools" row says "Up to date"
    And the Updates sidebar row shows no badge
    And the action journal is empty

  @stub.updates-flatpak-slow-check @stub.updates-brew-current
  Scenario: The shell reports the check while it runs, then its result
    Given ChairLift is running
    Then the Updates status reads "Checking for updates"
    And the "Applications" row says "Checking for updates…"
    And the Updates page offers no primary action
    When the Flatpak update check is allowed to finish
    Then the Updates status reads "Updates available"
    And the "Applications" row says "1 update available"
    And the "Update all" button is sensitive

  @stub.updates-flatpak-check-fails @stub.updates-brew-current
  Scenario: A failed check is reported and can be retried
    Given ChairLift is running
    Then the Updates status reads "Unable to check for updates"
    And I see "Unable to load summary from remote flathub"
    And the "Applications" row says "Check failed:"
    And the Updates page offers only the "Try again" action
    When the Flatpak remote is reachable again
    And I click the "Try again" button
    Then the Updates status reads "System is up to date"
    And the "Applications" row says "Up to date"
    And the Updates page offers only the "Check again" action

  @stub.updates-flatpak-current @stub.updates-brew-current
  Scenario Outline: <control> discovers an update that appeared after the first check
    Given ChairLift is running
    Then the Updates status reads "System is up to date"
    And the Updates sidebar row shows no badge
    When Flatpak now offers an update for Firefox
    And I click the "<control>" button
    Then the Updates status reads "Updates available"
    And the "Applications" row says "1 update available"
    And the "Firefox" row says "Available: 131.0"
    And the Updates sidebar badge shows "1"

    Examples:
      | control     |
      | Check again |
      | Refresh     |

  @stub.updates-flatpak-one-update @stub.updates-brew-current
  Scenario: Update all previews only the sources with updates and keeps them pending
    Given ChairLift is running
    Then the Updates status reads "Updates available"
    When I click the "Update all" button
    Then the application log contains "[DRY-RUN] Would execute: flatpak update -y --user"
    And the "Update all" button is sensitive
    And the "Refresh" button is sensitive
    And the Updates status reads "Updates available"
    And the "Applications" row says "1 update available"
    And the application log does not contain "flatpak update -y --system"
    And the application log does not contain "Would execute: brew update"
    And the application log does not contain "Would execute: brew upgrade"
    And the flatpak tool was never asked to "update"
    And the action journal is empty

  @stub.updates-flatpak-current @stub.updates-brew-one-outdated
  Scenario: Update all previews a Homebrew refresh and upgrade without running brew
    Given ChairLift is running
    Then the Updates status reads "Updates available"
    And the "Developer tools" row says "1 update available"
    And the "jq" row says "Installed: 1.7.1"
    When I click the "Update all" button
    Then the application log contains "[DRY-RUN] Would execute: brew update"
    And the application log contains "[DRY-RUN] Would execute: brew upgrade"
    And the "Update all" button is sensitive
    And the "Developer tools" row says "1 update available"
    And the application log does not contain "Would execute: flatpak update"
    And the brew tool was never asked to "update"
    And the brew tool was never asked to "upgrade"
    And the action journal is empty

  @config.updates-no-flatpak @stub.updates-flatpak-one-update @stub.updates-brew-current
  Scenario: An administrator-disabled source is never checked
    Given ChairLift is running
    Then the Updates status reads "System is up to date"
    And the "Applications" row says "Disabled by administrator"
    And the "Developer tools" row says "Up to date"
    And the Updates sidebar row shows no badge
    And the flatpak tool was never asked to "remote-ls"

  # Issue #349 regressions: the Updates preferences page (buildUpdatesPage)
  # mounts beneath the update shell's sources, so automatic updates, the
  # system version, the per-source groups, and the Advanced group are part
  # of the Updates destination (docs/walkthrough.md, "Updates").

  # The automatic-updates switch reverts through guardedSwitch: a raw
  # gtk_switch_set_active re-emits ::state-set, which once journalled
  # auto-updates-disable/-enable in an endless loop from one dry-run toggle.
  @stub.updates-flatpak-current @stub.updates-brew-current
  Scenario: Turning automatic updates off in a dry run asks the helper once and keeps them on
    Given ChairLift is running
    Then the switch in the "Automatic updates" row is on
    When I toggle the switch in the "Automatic updates" row
    Then the action journal records "auto-updates-disable" as dry-run
    And the journalled command is "pkexec /usr/bin/chairlift-ublue-helper auto-updates-disable --dry-run"
    And I see "[DRY-RUN] Preview: automatic updates would be turned off — no changes made"
    And the switch in the "Automatic updates" row can be toggled again
    And the switch in the "Automatic updates" row is on
    And the action journal holds exactly 1 entry

  @stub.updates-flatpak-current @stub.updates-brew-current
  Scenario: Asking for early updates in a dry run journals the channel word only and stays on stable
    Given ChairLift is running
    Then the switch in the "Get updates early" row is off
    When I toggle the switch in the "Get updates early" row
    Then the action journal records "channel-switch" as dry-run
    And the journalled command is "pkexec /usr/bin/chairlift-ublue-helper channel-switch testing --dry-run"
    And I see "[DRY-RUN] Preview: would switch to the testing channel — no changes made"
    And the switch in the "Get updates early" row can be toggled again
    And the switch in the "Get updates early" row is off
    And the action journal holds exactly 1 entry

  @stub.updates-image-dakota-stable @stub.updates-flatpak-current @stub.updates-brew-current
  Scenario: Switching to the recommended graphics driver in a dry run journals the driver word and restores the button
    Given ChairLift is running
    Then the "Graphics driver" row says "Switch to the NVIDIA (proprietary) driver for your NVIDIA + Intel graphics"
    When I click the "Switch" button in the "Graphics driver" row
    Then the action journal records "driver-switch" as dry-run
    And the journalled command is "pkexec /usr/bin/chairlift-ublue-helper driver-switch nvidia --dry-run"
    And I see "[DRY-RUN] Preview: would switch to the NVIDIA (proprietary) image — no changes made"
    And the "Switch" button in the "Graphics driver" row is sensitive

  @stub.updates-flatpak-current @stub.updates-brew-current
  Scenario: A stream that publishes no NVIDIA variant offers no driver switch
    Given ChairLift is running
    Then the "Graphics driver" row says "Using the Standard driver for your NVIDIA + Intel graphics"
    And the "Switch" button is not shown

  @stub.updates-bootc-booted @stub.updates-flatpak-current @stub.updates-brew-current
  Scenario: The system version is read from the booted deployment
    Given ChairLift is running
    Then the "System version" row says "You are running version 42.20260920.0, released 20 September 2026"

  @stub.updates-flatpak-one-update @stub.updates-brew-current
  Scenario: Updating one app from its row in a dry run previews that app only and restores the row
    Given ChairLift is running
    When I expand the "Available updates" row in the "Apps" group
    Then the "Firefox" row says "Updates to version 131.0, for you only"
    When I click the "Update" button in the "Firefox" row
    Then the application log contains "[DRY-RUN] Would execute: flatpak update -y --user org.mozilla.firefox"
    And I see "[DRY-RUN] Preview: Firefox would be updated — no changes made"
    And the "Update" button in the "Firefox" row is sensitive
    And the flatpak tool was never asked to "update"

  @stub.updates-flatpak-current @stub.updates-brew-one-outdated
  Scenario: Checking Homebrew for new versions in a dry run previews the refresh and restores the button
    Given ChairLift is running
    When I click the "Check" button in the "Check for new versions" row
    Then the application log contains "[DRY-RUN] Would execute: brew update"
    And the "Check" button in the "Check for new versions" row is sensitive
    When I expand the "Available updates" row in the "Developer tools" group
    Then the "jq" row says "Version 1.7.1"
    And the brew tool was never asked to "update"
