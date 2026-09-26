@features
Feature: Features page — Developer tools, Gaming, and the Custom Command Menu
  The Features page switches on capabilities of a Bluefin-family system.
  Developer tools is a privileged change through the fixed ublue helper;
  Gaming installs user-scope Flatpaks with no privilege at all; Developer
  Mode also drives the Custom Command Menu's developer entries. The
  application always runs with --dry-run here, so every switch must preview
  its change, run nothing, and come back to the state it restored on load.

  Updex's "Optional features" group is not covered: updex reads feature
  definitions only from root-owned sysupdate.d directories, which neither the
  Dakota container nor a CI runner has, so the group is hidden in every run.

  @stub.features-gaming-installed @stub.features-devmenu
  Scenario: Showing the machine's state on load runs nothing
    Given ChairLift is running
    When I open the "Features" page
    Then the switch in the "Gaming apps" row is on
    And the "Gaming apps" row says "Installed. Steam and everything that goes with it are ready to use."
    And the Developer tools switch shows this account's developer-group membership
    And the action journal is empty
    And the fake flatpak was never asked to "install"
    And the fake flatpak was never asked to "uninstall"
    And the fake dconf was never asked to "dump"
    And the application log does not contain "Custom Command Menu"

  @stub.features-gaming-none @stub.features-devmenu
  Scenario: Toggling Developer tools previews the fixed helper command and restores the switch
    Given ChairLift is running
    When I open the "Features" page
    Then the Developer tools switch shows this account's developer-group membership
    When I toggle the switch in the "Developer tools" row
    Then the Developer tools change is journalled as a dry run of the fixed helper
    And the Developer tools preview toast is shown
    And the Developer tools switch returns to its restored state

  @stub.features-gaming-none @stub.features-devmenu
  Scenario: Developer Mode previews the Custom Command Menu change without writing dconf
    Given ChairLift is running
    When I open the "Features" page
    Then the Developer tools switch shows this account's developer-group membership
    When I toggle the switch in the "Developer tools" row
    Then the Custom Command Menu change is previewed for the two developer entries only
    And the fake dconf was never asked to "write"
    And the fake dconf was never asked to "reset"

  @config.features-feeds @stub.features-gaming-none
  Scenario: A dry-run Developer Mode change starts no feed onboarding
    Given ChairLift is running
    When I open the "Features" page
    Then the Developer tools switch shows this account's developer-group membership
    When I toggle the switch in the "Developer tools" row
    Then the Developer tools preview toast is shown
    And the Developer feed onboarding never starts

  @stub.features-gaming-none
  Scenario: Turning Gaming on previews installing every component and restores the switch
    Given ChairLift is running
    When I open the "Features" page
    Then the switch in the "Gaming apps" row is off
    And the "Gaming apps" row says "Installs Steam and the tools that make Windows games run. This is a large download."
    When I toggle the switch in the "Gaming apps" row
    Then the application log previews "flatpak install" for every gaming component
    And the Gaming toast says "[DRY-RUN] Preview: gaming components would be installed — no changes made"
    And the switch in the "Gaming apps" row is off
    And the switch in the "Gaming apps" row accepts input
    And the Gaming inventory is read again after the change
    And the "Gaming apps" row says "Installs Steam and the tools that make Windows games run. This is a large download."
    And the fake flatpak was never asked to "install"
    And the action journal is empty

  @stub.features-gaming-partial
  Scenario: Finishing a partial Gaming install previews only the missing components
    Given ChairLift is running
    When I open the "Features" page
    Then the switch in the "Gaming apps" row is off
    And the "Gaming apps" row says "Partly set up — 1 of 6 gaming apps are installed. Turn this on to finish."
    When I toggle the switch in the "Gaming apps" row
    Then the application log previews "flatpak install" for every gaming component but "Steam"
    And the Gaming toast says "[DRY-RUN] Preview: gaming components would be installed — no changes made"
    And the switch in the "Gaming apps" row is off
    And the fake flatpak was never asked to "install"

  @stub.features-gaming-installed
  Scenario: Turning Gaming off previews removing every user-installed component and keeps it on
    Given ChairLift is running
    When I open the "Features" page
    Then the switch in the "Gaming apps" row is on
    When I toggle the switch in the "Gaming apps" row
    Then the application log previews "flatpak uninstall" for every gaming component
    And the Gaming toast says "[DRY-RUN] Preview: gaming components would be removed — no changes made"
    And the switch in the "Gaming apps" row is on
    And the switch in the "Gaming apps" row accepts input
    And the "Gaming apps" row says "Installed. Steam and everything that goes with it are ready to use."
    And the fake flatpak was never asked to "uninstall"
    And the action journal is empty

  @stub.features-gaming-system
  Scenario: Turning Gaming off leaves components the image installed system-wide
    Given ChairLift is running
    When I open the "Features" page
    Then the switch in the "Gaming apps" row is on
    When I toggle the switch in the "Gaming apps" row
    Then the Gaming inventory is read again after the change
    And the switch in the "Gaming apps" row is on
    And the application log does not contain "flatpak uninstall"
    And the fake flatpak was never asked to "uninstall"

  @known_issue.352 @stub.features-gaming-system
  Scenario: The Gaming preview does not promise to remove system-wide components
    Given ChairLift is running
    When I open the "Features" page
    Then the switch in the "Gaming apps" row is on
    When I toggle the switch in the "Gaming apps" row
    Then the Gaming toast says "[DRY-RUN] Preview: nothing to remove — 6 component(s) installed system-wide would be left in place"

  @stub.features-gaming-unlistable
  Scenario: Gaming fails closed when the installed apps cannot be listed
    Given ChairLift is running
    When I open the "Features" page
    Then the "Gaming apps" row says "Could not check which gaming apps are installed."
    And the switch in the "Gaming apps" row refuses input
    And the application log contains "views: gaming status unavailable"
    And the action journal is empty

  @stub.features-gaming-image @stub.features-gaming-none
  Scenario: An image that ships the gaming apps shows a note instead of the switch
    Given ChairLift is running
    When I open the "Features" page
    Then the Features page shows a "Gaming" group
    And I see "Steam and the tools that go with it are already part of this system."
    And the "Already set up" row offers no switch
    And I do not see "Gaming apps"
    And the Developer tools switch shows this account's developer-group membership
    And the application log contains "views: gaming group suppressed"

  @stub.features-no-descriptor @stub.features-gaming-none
  Scenario: A host without a ublue-os image descriptor offers neither Developer nor Gaming
    Given ChairLift is running
    When I open the "Features" page
    Then the Features page shows no "Developer" group
    And the Features page shows no "Gaming" group
    And I do not see "Developer tools"
    And I do not see "Gaming apps"
    And the application log does not contain "views: bluefin groups built"

  @env.CHAIRLIFT_CAPABILITIES=flatpak,brew,podman,bootc-stage @stub.features-gaming-none
  Scenario: The image-descriptor capability floors the Developer and Gaming groups
    Given ChairLift is running
    When I open the "Features" page
    Then the Features page shows no "Developer" group
    And the Features page shows no "Gaming" group
    And the application log does not contain "views: bluefin groups built"

  @config.features-no-dx @stub.features-gaming-none
  Scenario: Disabling dx_group in configuration removes only the Developer group
    Given ChairLift is running
    When I open the "Features" page
    Then the Features page shows a "Gaming" group
    And the switch in the "Gaming apps" row accepts input
    And the Features page shows no "Developer" group
    And I do not see "Developer tools"
    And the application log contains "dx_group=false gaming_group=true"

  @known_issue.352 @stub.features-no-descriptor @stub.features-gaming-none
  Scenario: A host with nothing to offer does not advertise a blank Features page
    Given ChairLift is running
    Then the Features destination is hidden or says why it offers nothing
