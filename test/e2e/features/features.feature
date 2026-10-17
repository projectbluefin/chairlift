@features
Feature: Features page — Developer Mode, WSL Mode, Docker, Gaming Mode, and Printers
  Optional tools are explicit choices. Gaming Mode installs gaming tools
  system-wide with a single toggle; the fixed helper grants only the access
  each developer option needs. Dry runs restore every control and preserve
  observed installed state.

  @stub.features-gaming-installed @stub.features-devmenu
  Scenario: Showing installed Gaming Mode and Developer Mode runs no mutation
    Given ChairLift is running
    When I open the "Features" page
    Then the Developer Mode switch shows this account's developer-group membership
    And I do not see "IDEs and terminal editors"
    And the application log does not contain "brew install devcontainer"
    And the switch in the "Gaming Mode" row is on
    And the action journal is empty
    And the fake flatpak was never asked to "install"
    And the fake flatpak was never asked to "uninstall"
    And the fake dconf was never asked to "dump"

  @stub.features-gaming-none @stub.features-devmenu
  Scenario: Developer Mode previews the fixed helper and restores the switch
    Given ChairLift is running
    When I open the "Features" page
    Then the Developer Mode switch shows this account's developer-group membership
    When I toggle the switch in the "Developer Mode" row
    Then the Developer Mode change is journalled as a dry run of the fixed helper
    And the Developer Mode preview toast is shown
    And the Developer Mode switch returns to its restored state
    And the Dev Container CLI install is previewed only when enabling

  @stub.features-gaming-none @stub.features-devmenu
  Scenario: Developer Mode previews only the two Custom Command Menu entries
    Given ChairLift is running
    When I open the "Features" page
    Then the Developer Mode switch shows this account's developer-group membership
    When I toggle the switch in the "Developer Mode" row
    Then the Custom Command Menu change is previewed for the two developer entries only
    And the fake dconf was never asked to "write"
    And the fake dconf was never asked to "reset"

  @config.features-feeds @stub.features-gaming-none
  Scenario: A dry-run Developer Mode change starts no feed onboarding
    Given ChairLift is running
    When I open the "Features" page
    Then the Developer Mode switch shows this account's developer-group membership
    When I toggle the switch in the "Developer Mode" row
    Then the Developer Mode preview toast is shown
    And the Developer feed onboarding never starts

  @stub.features-gaming-none @stub.features-developer
  Scenario: Disabling a running WSL machine previews stop without deleting data
    Given ChairLift is running
    When I open the "Features" page
    Then I see "Virtual Machine Engine"
    And the switch in the "WSL Mode" row is on
    When I toggle the switch in the "WSL Mode" row
    Then the application log contains "would stop the ubuntu or debian nsl machine WSL Mode manages"
    And the switch in the "WSL Mode" row is on
    And the switch in the "WSL Mode" row accepts input
    And the fake nsl was never asked to "shutdown"
    And the fake nsl was never asked to "remove"
    And the action journal is empty

  @stub.features-gaming-none @stub.features-developer
  Scenario: Docker CLI presence never claims a daemon is available
    Given ChairLift is running
    When I open the "Features" page
    Then the switch in the "Enable Docker" row is off
    And the switch in the "Enable Docker" row refuses input
    And the "Enable Docker" row says "Docker isn't available on this system, so containers can't run here."
    And the action journal is empty

  @stub.features-gaming-none
  Scenario: Gaming Mode toggle previews system install under dry run
    Given ChairLift is running
    When I open the "Features" page
    Then the switch in the "Gaming Mode" row accepts input
    And the switch in the "Gaming Mode" row is off
    When I toggle the switch in the "Gaming Mode" row
    Then the gaming preview installs all components into the system scope
    And the switch in the "Gaming Mode" row is off
    And the fake flatpak was never asked to "install"
    And the action journal is empty

  @stub.features-gaming-installed
  Scenario: Disabling Gaming Mode previews removal under dry run
    Given ChairLift is running
    When I open the "Features" page
    Then the switch in the "Gaming Mode" row accepts input
    And the switch in the "Gaming Mode" row is on
    When I toggle the switch in the "Gaming Mode" row
    Then a dialog titled "Turn Off Gaming Mode?" is shown
    When I choose "Remove" in the dialog
    Then the gaming preview removes all components from the system scope
    And the switch in the "Gaming Mode" row is on
    And the fake flatpak was never asked to "uninstall"
    And the action journal is empty


  @stub.features-gaming-unlistable
  Scenario: Gaming fails closed when installed components cannot be listed
    Given ChairLift is running
    When I open the "Features" page
    Then the "Gaming Mode" row says "Couldn't check which gaming apps are installed."
    And the switch in the "Gaming Mode" row refuses input
    And the application log contains "views: gaming status unavailable"
    And the action journal is empty

  @stub.features-gaming-image @stub.features-gaming-installed
  Scenario: A gaming image presents the Gaming Mode switch
    Given ChairLift is running
    When I open the "Features" page
    Then the switch in the "Gaming Mode" row is on
    And I see "Gaming Mode"
  @config.features-no-desktop @stub.features-no-descriptor @stub.features-gaming-none
  Scenario: Without a Bluefin descriptor Developer and Gaming are not offered
    Given ChairLift is running
    When I open the "Features" page
    Then the Features page shows no "Developer" group
    And the Features page shows no "Gaming" group
    And I do not see "Developer Mode"
    And I do not see "Gaming Mode"

  @env.CHAIRLIFT_CAPABILITIES=flatpak,brew,podman,bootc-stage @stub.features-gaming-none
  Scenario: The image-descriptor capability floors Developer and Gaming
    Given ChairLift is running
    When I open the "Features" page
    Then the Features page shows no "Developer" group
    And the Features page shows no "Gaming" group

  @config.features-no-dx @stub.features-gaming-none
  Scenario: Disabling dx_group removes all developer supporting options and keeps Gaming
    Given ChairLift is running
    When I open the "Features" page
    Then the Features page shows a "Gaming" group
    And the Features page shows no "Developer" group
    And I do not see "WSL Mode"
    And I do not see "Virtual Machine Engine"
    And I do not see "Enable Docker"

  @config.features-no-desktop @stub.features-no-descriptor @stub.features-gaming-none
  @env.CHAIRLIFT_CAPABILITIES=image-descriptor,flatpak,brew,bootc-stage
  Scenario: A host with nothing to offer does not advertise a blank Features page
    Given ChairLift is running
    Then the Features destination is hidden or says why it offers nothing
    And I see "Nothing to Set Up Here"
    And the application log contains "views: features page offers nothing on this system"

  # ------------------------------------------------------------ Printers
  #
  # Every family is offered with its switch off. Each runs on loopback only
  # (ADR-0020), so the subtitle says only this computer can use it.
  # Rendering the group runs nothing and writes nothing.

  @stub.printers @stub.features-gaming-none
  Scenario: Every printer family is shown off and ready to turn on
    Given ChairLift is running
    When I open the "Features" page
    Then the Features page shows a "Printers" group
    And the Printers group offers exactly these rows, each off and ready to turn on
      | row                  |
      | Ghostscript Printers |
      | HP Printers (HPLIP)  |
      | Gutenprint Printers  |
    And the application log contains "views: printers group built families=3"
    And the systemctl tool was never asked to mutate
    And no printer quadlet was written
    And the action journal is empty

  @env.CHAIRLIFT_CAPABILITIES=image-descriptor,flatpak,brew,bootc-stage @stub.features-gaming-none
  Scenario: Without Podman the Printers group is explained on Help, not shown
    Given ChairLift is running
    When I open the "Features" page
    Then the Features page shows no "Printers" group
    And I do not see "Ghostscript Printers"
    And the application log does not contain "views: printers group built"
    When I press "F1"
    And I ask the Help page why something is missing
    Then the Help page explains "Printers" with "Needs Podman"

  # Homebrew is withheld so the Help explainer has something to list; the
  # Printers row must not be among it, because a group the administrator
  # turned off is not a missing tool.
  @config.features-no-printers @stub.features-gaming-none
  @env.CHAIRLIFT_CAPABILITIES=image-descriptor,flatpak,bootc-stage
  Scenario: Disabling printers_group in configuration removes the group without calling it missing
    Given ChairLift is running
    When I open the "Features" page
    Then the Features page shows a "Gaming" group
    And the Features page shows no "Printers" group
    And I do not see "Ghostscript Printers"
    And the application log does not contain "views: printers group built"
    When I press "F1"
    And I ask the Help page why something is missing
    Then the Help page explains "Agent Mode" with "Needs Homebrew"
    And the Help page does not call "Printers" missing

  @stub.features-extensions
  Scenario: Desktop Integrations preserve their observed state in preview mode
    Given ChairLift is running
    When I open the "Features" page
    Then the switch in the "Tailscale Integration" row is on
    And the switch in the "Sync Folder Integration" row is off
    And the switch in the "Tailscale Integration" row accepts input
    When I toggle the switch in the "Tailscale Integration" row
    Then the application log contains "Would execute: gnome-extensions disable tailscale-gnome-qs@tailscale-qs.github.io"
    And the switch in the "Tailscale Integration" row is on
    When I toggle the switch in the "Sync Folder Integration" row
    Then the application log contains "Would execute: gnome-extensions enable syncthing-toggle@projectbluefin.io"
    And the switch in the "Sync Folder Integration" row is off
    And the fake gnome-extensions was never asked to "enable"
    And the fake gnome-extensions was never asked to "disable"

  @stub.features-extensions-reversed
  Scenario: Desktop Integrations restore existing GNOME choices instead of defaults
    Given ChairLift is running
    When I open the "Features" page
    Then the switch in the "Tailscale Integration" row accepts input
    And the switch in the "Tailscale Integration" row is off
    And the switch in the "Sync Folder Integration" row is on
    And the fake gnome-extensions was never asked to "enable"
    And the fake gnome-extensions was never asked to "disable"

  @stub.features-extensions-missing
  Scenario: Missing desktop extensions are explained without permitting changes
    Given ChairLift is running
    When I open the "Features" page
    Then the "Tailscale Integration" row says "Not installed on this computer."
    And the switch in the "Tailscale Integration" row refuses input
    And the "Sync Folder Integration" row says "Not installed on this computer."
    And the switch in the "Sync Folder Integration" row refuses input
    And the switch in the "Tailscale Integration" row is off
    And the switch in the "Sync Folder Integration" row is off
