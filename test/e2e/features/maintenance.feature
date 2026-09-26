@maintenance
Feature: Maintenance and its Recovery detail
  The Maintenance page holds one routine "Free up space" action, whatever
  maintenance tasks the administrator configured, and the entry to Recovery:
  the detail screen for going back to the previous system version and for
  the two irreversible resets, Powerwash and Factory Reset.

  The application runs with --dry-run, so every mutation must stay a preview:
  nothing privileged runs (the action journal records what would have), no
  package tool is asked to remove anything, and no control claims a result
  that did not happen. Controls that stay visible after a run must come back
  usable. Scenario ideas and the confirmation texts were first probed in
  projectbluefin/chairlift#373 (mrbobbytables).

  # ------------------------------------------------------------ Free up space

  @stub.maintenance_bootc_rollback @stub.maintenance_package_tools
  Scenario: Free up space previews its cleanup without removing anything or claiming space
    Given ChairLift is running
    When I open the "Maintenance" page
    And I click the "Clean up" button in the "Free up space" row
    Then I see "[DRY-RUN] Preview: nothing was removed — no changes made"
    And the application log contains "[DRY-RUN] Would execute: brew cleanup"
    And the application log contains "[DRY-RUN] Would execute: flatpak uninstall --unused -y --user"
    And the application log contains "[DRY-RUN] Would execute: flatpak uninstall --unused -y --system"
    And the stubbed "brew" answered a read-only query
    And the stubbed "brew" never ran "cleanup"
    And the stubbed "flatpak" answered a read-only query
    And the stubbed "flatpak" never ran "uninstall"
    And the "Free up space" row says "Removes old downloads and supporting software nothing uses any more."
    And I do not see "Freed"
    And I do not see "Cleanup finished."
    And the "Clean up" button in the "Free up space" row is sensitive
    And the action journal is empty

  # ------------------------------------------------------ configured tasks

  @stub.maintenance_bootc_rollback @stub.maintenance_package_tools
  Scenario: An administrator's maintenance task is journalled as a dry-run and its button restored
    Given ChairLift is running
    When I open the "Maintenance" page
    Then I see "Set up by whoever set up this computer."
    And I do not see "/usr/libexec/bls-gc"
    When I click the "Run" button in the "Clean Up Boot Old Entries" row
    Then I see "[DRY-RUN] Preview: Clean Up Boot Old Entries would run — no changes made"
    And the action journal records "bls-gc" as dry-run
    And the journalled command is "/usr/libexec/bls-gc"
    And the application log contains "[DRY-RUN] Would execute: /usr/libexec/bls-gc"
    And the "Run" button in the "Clean Up Boot Old Entries" row is sensitive

  # ------------------------------------------------------------ Recovery

  @stub.maintenance_bootc_rollback @stub.maintenance_package_tools
  Scenario: Recovery is a detail of Maintenance and Back returns there
    Given ChairLift is running
    When I open the "Maintenance" page
    And I open the Recovery detail
    Then I see "Go back to the previous version"
    And I see "Reset the system"
    When I go back from the Recovery detail
    Then the "Maintenance" page is shown
    And I see "Free up space"
    And I do not see "Reset the system"

  @known_issue.351 @stub.maintenance_bootc_rollback @stub.maintenance_package_tools
  Scenario: The Recovery back button names the page it returns to
    Given ChairLift is running
    When I open the "Maintenance" page
    And I open the Recovery detail
    Then the Recovery back button is named "Back to Maintenance"

  # ------------------------------------------------------------ Powerwash

  @stub.maintenance_bootc_rollback @stub.maintenance_package_tools
  Scenario: Cancelling Powerwash removes nothing and leaves it available
    Given ChairLift is running
    When I open the "Maintenance" page
    And I open the Recovery detail
    And I click the "Remove…" button in the "Remove apps you installed" row
    Then a dialog titled "Remove Everything I Installed?" is shown
    And the dialog says "This removes every Flatpak application and every Distrobox container for your account. It does not touch the system image or your files. This cannot be undone."
    When I choose "Cancel" in the dialog
    Then no dialog is shown
    And the "Remove…" button in the "Remove apps you installed" row is sensitive
    When I click the "Remove…" button in the "Remove apps you installed" row
    Then a dialog titled "Remove Everything I Installed?" is shown
    When I choose "Cancel" in the dialog
    Then no dialog is shown
    And the application log does not contain "Would execute: flatpak uninstall"
    And the application log does not contain "would execute: distrobox"
    And the application log does not contain "views: powerwash finished"
    And the stubbed "flatpak" never ran "uninstall"
    And the stubbed "distrobox" never ran "rm"
    And the action journal is empty

  @stub.maintenance_bootc_rollback @stub.maintenance_package_tools
  Scenario: Confirming Powerwash previews both removals, claims nothing, and can run again
    Given ChairLift is running
    When I open the "Maintenance" page
    And I open the Recovery detail
    And I click the "Remove…" button in the "Remove apps you installed" row
    And I choose "Remove Everything" in the dialog
    Then I see "[DRY-RUN] Preview: would remove your Flatpaks and Distrobox containers — no changes made"
    And no dialog is shown
    And the application log contains "[DRY-RUN] Would execute: flatpak uninstall --user --all -y"
    And the application log contains "[DRY-RUN] would execute: distrobox rm --all --force"
    And the stubbed "flatpak" never ran "uninstall"
    And the stubbed "distrobox" never ran "rm"
    And the "Remove apps you installed" row says "Your files, settings, and the system itself stay as they are."
    And I do not see "Removed the apps you installed"
    And the "Remove…" button in the "Remove apps you installed" row is sensitive
    And the action journal is empty
    When I click the "Remove…" button in the "Remove apps you installed" row
    Then a dialog titled "Remove Everything I Installed?" is shown

  # --------------------------------------------------------- Factory Reset

  @stub.maintenance_bootc_rollback @stub.maintenance_package_tools
  Scenario: Cancelling Factory Reset reaches no privileged helper and leaves it available
    Given ChairLift is running
    When I open the "Maintenance" page
    And I open the Recovery detail
    And I click the "Reset…" button in the "Reset the system" row
    Then a dialog titled "Factory Reset This System?" is shown
    And the dialog says "This discards every local change and reinstalls the current system image from scratch, using bootc's --experimental reset path. Your applications and home directory are not touched, but this cannot be undone. The reset applies at the next restart."
    When I choose "Cancel" in the dialog
    Then no dialog is shown
    And the "Reset…" button in the "Reset the system" row is sensitive
    When I click the "Reset…" button in the "Reset the system" row
    Then a dialog titled "Factory Reset This System?" is shown
    When I choose "Cancel" in the dialog
    Then no dialog is shown
    And the application log does not contain "chairlift-ublue-helper"
    And the action journal is empty

  @stub.maintenance_bootc_rollback @stub.maintenance_package_tools
  Scenario: Confirming Factory Reset journals the fixed helper command word and claims nothing
    Given ChairLift is running
    When I open the "Maintenance" page
    And I open the Recovery detail
    And I click the "Reset…" button in the "Reset the system" row
    And I choose "Factory Reset" in the dialog
    Then I see "[DRY-RUN] Preview: would factory reset this system — no changes made"
    And no dialog is shown
    And the action journal records "factory-reset" as dry-run
    And the journalled command is "pkexec /usr/bin/chairlift-ublue-helper factory-reset --dry-run"
    And the journalled action carries no argument
    And the action journal has no "rollback" entry
    And I do not see "Factory reset applied"
    And the "Reset…" button in the "Reset the system" row is sensitive

  # ------------------------------------------------------------ Roll Back

  @stub.maintenance_bootc_rollback @stub.maintenance_package_tools
  Scenario: Roll Back names the kept version, journals a dry-run, and stays available after the preview
    Given ChairLift is running
    When I open the "Maintenance" page
    And I open the Recovery detail
    Then the "Go back to the previous version" row says "Return to version 44.20260913, released 13 September 2026, the next time you restart"
    When I click the "Roll Back" button in the "Go back to the previous version" row
    Then I see "[DRY-RUN] Preview: would roll back to the previous system image — no changes made"
    And the action journal records "rollback" as dry-run
    And the journalled command is "pkexec /usr/bin/chairlift-ublue-helper rollback --dry-run"
    And the journalled action carries no argument
    And I do not see "The previous version starts the next time you restart"
    And the "Roll Back" button in the "Go back to the previous version" row is sensitive

  @stub.maintenance_bootc_no_rollback @stub.maintenance_package_tools
  Scenario: A host that keeps no previous version is not offered a rollback
    Given ChairLift is running
    When I open the "Maintenance" page
    And I open the Recovery detail
    Then I see "Published versions"
    And I see "Reset the system"
    And I do not see "Go back to the previous version"
    And the "Roll Back" button is not shown

  @stub.maintenance_bootc_rollback @stub.maintenance_package_tools @stub.maintenance_registry_unreachable
  Scenario: An unreachable registry lists no published versions and the check can be retried
    Given ChairLift is running
    When I open the "Maintenance" page
    And I open the Recovery detail
    And I click the "Check" button in the "Published versions" row
    Then I see "Could not read the published versions from the image registry"
    And the application log contains "published versions: "
    And the "Published versions" row says "See the latest versions the image registry still offers from the last 90 days."
    And the "Check Again" button in the "Published versions" row is sensitive
    And I do not see "versions from the last 90 days"
    And the action journal is empty

  # ------------------------------------------------ configuration & capability

  @config.maintenance-shipped @stub.maintenance_bootc_rollback @stub.maintenance_package_tools
  Scenario: The shipped configuration offers no reset and no administrator tasks
    Given ChairLift is running
    When I open the "Maintenance" page
    Then I see "Free up space"
    And I do not see "Maintenance tasks"
    When I open the Recovery detail
    Then I see "Go back to the previous version"
    And I do not see "Reset the system"
    And I do not see "Remove apps you installed"
    And the application log does not contain "views: reset group built"

  @env.CHAIRLIFT_CAPABILITIES=image-descriptor,brew,podman,bootc-stage
  @stub.maintenance_bootc_rollback @stub.maintenance_package_tools
  Scenario: Without Flatpak or Distrobox the enabled reset group stays hidden
    Given ChairLift is running
    When I open the "Maintenance" page
    And I open the Recovery detail
    Then I see "Go back to the previous version"
    And I do not see "Reset the system"
    And I do not see "Remove apps you installed"
    And the application log does not contain "views: reset group built"

  @config.maintenance-shipped @env.CHAIRLIFT_CAPABILITIES=image-descriptor,flatpak,brew,podman
  @stub.maintenance_bootc_rollback @stub.maintenance_package_tools
  Scenario: With no reset enabled and no bootc staging there is no Recovery entry
    Given ChairLift is running
    When I open the "Maintenance" page
    Then I see "Free up space"
    And I do not see "Recovery"
