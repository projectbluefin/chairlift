@features
Feature: Features page — Developer Mode, WSL Mode, Docker, selective Gaming, and Printers
  Optional tools are explicit choices. Gaming installs system-wide and removes
  a selected app from every scope it is in, except a system copy the image
  ships; the fixed helper grants only the access each developer option needs.
  Dry runs restore every control and preserve observed installed state.

  @stub.features-gaming-installed @stub.features-devmenu
  Scenario: Showing installed gaming components and Developer Mode runs no mutation
    Given ChairLift is running
    When I open the "Features" page
    Then the Developer Mode switch shows this account's developer-group membership
    And each gaming component says "Installed system-wide"
    And no gaming component is selected
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
  Scenario: Developer options expose one Toolbox and individually chosen editor installs
    Given ChairLift is running
    When I open the "Features" page
    Then I see "Developer Mode"
    And I see "WSL Mode"
    And I see "WSL Backend"
    And I see "Enable Docker"
    When I expand the "IDEs and terminal editors" list under "Developer"
    Then the developer editor choices match the documented catalog
    And the "Install" button in the "VSCodium" row is sensitive
    When I click the "Install" button in the "VSCodium" row
    Then the application log contains "Would execute: brew install --cask ublue-os/tap/vscodium-linux"
    And the application log does not contain "Would execute: brew install --cask ublue-os/tap/jetbrains-toolbox-linux"
    And the "Install" button in the "VSCodium" row is sensitive
    And the action journal is empty

  @stub.features-gaming-none @stub.features-developer
  Scenario: Disabling a running WSL machine previews stop without deleting data
    Given ChairLift is running
    When I open the "Features" page
    Then I see "WSL Backend"
    And the switch in the "WSL Mode" row is on
    When I toggle the switch in the "WSL Mode" row
    Then the application log contains "would stop nsl machines and VM without deleting data"
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
    And the "Enable Docker" row says "This base image has no Docker daemon. Installing CLI tools alone cannot run containers."
    And the action journal is empty

  @stub.features-gaming-none
  Scenario: Gaming install requires explicit selection
    Given ChairLift is running
    When I open the "Features" page
    Then no gaming component is selected
    When I click the "Install Selected" button
    Then the application log does not contain "flatpak install"
    And the fake flatpak was never asked to "install"

  @stub.features-gaming-none
  Scenario: Installing one selected gaming app never installs the other components
    Given ChairLift is running
    When I open the "Features" page
    And I select the "Steam" gaming component
    And I click the "Install Selected" button
    Then the gaming preview installs only "Steam" into the system scope
    And the "Install Selected" button is sensitive
    And each gaming component says "Not installed"
    And the fake flatpak was never asked to "install"
    And the action journal is empty

  # Flathub publishes MangoHud once per Platform release, so a bare ID stops
  # at flatpak's "Which do you want to use?" prompt and fails.
  @stub.features-gaming-none
  Scenario: MangoHud is installed in the branch Steam's runtime uses
    Given ChairLift is running
    When I open the "Features" page
    And I select the "MangoHud" gaming component
    And I click the "Install Selected" button
    Then the gaming preview installs only "MangoHud" into the system scope
    And the application log contains "[DRY-RUN] Would execute: flatpak install -y --system org.freedesktop.Platform.VulkanLayer.MangoHud//26.08"
    And the fake flatpak was never asked to "install"
    And the action journal is empty

  @stub.features-gaming-partial
  Scenario: A selected already-installed component is not reinstalled
    Given ChairLift is running
    When I open the "Features" page
    And I select the "Steam" gaming component
    And I click the "Install Selected" button
    Then the Gaming inventory is read again after the change
    And the application log does not contain "flatpak install"

  @stub.features-gaming-installed
  Scenario: Removing one selected app is confirmed and restores its installed state under dry run
    Given ChairLift is running
    When I open the "Features" page
    And I select the "ProtonUp-Qt" gaming component
    And I click the "Remove Selected" button
    Then a dialog titled "Remove selected gaming apps?" is shown
    When I choose "Remove" in the dialog
    Then the gaming preview removes only "ProtonUp-Qt" from the system scope
    And each gaming component says "Installed system-wide"
    And the "Remove Selected" button is sensitive
    And the fake flatpak was never asked to "uninstall"

  # Dakota's /usr/share/ublue-os/homebrew/system-flatpaks.Brewfile ships
  # Flatseal system-wide, so undoing gaming mode must leave that copy alone.
  @stub.features-gaming-installed
  Scenario: A selected gaming app that came with the system is left in place
    Given ChairLift is running
    When I open the "Features" page
    And I select the "Flatseal" gaming component
    And I click the "Remove Selected" button
    And I choose "Remove" in the dialog
    Then the Gaming inventory is read again after the change
    And the application log contains "views: gaming component com.github.tchx84.Flatseal came with the system"
    And the application log does not contain "flatpak uninstall"
    And each gaming component says "Installed system-wide"
    And the "Remove Selected" button is sensitive

  @stub.features-gaming-user
  Scenario: Per-user gaming apps an earlier release installed are removed from your account
    Given ChairLift is running
    When I open the "Features" page
    Then each gaming component says "Installed for your account"
    When I select the "Steam" gaming component
    And I click the "Remove Selected" button
    And I choose "Remove" in the dialog
    Then the gaming preview removes only "Steam" from the user scope
    And the Gaming inventory is read again after the change
    And each gaming component says "Installed for your account"
    And the fake flatpak was never asked to "uninstall"

  @stub.features-gaming-unlistable
  Scenario: Gaming fails closed when installed components cannot be listed
    Given ChairLift is running
    When I open the "Features" page
    Then the "Gaming Mode" row says "Could not check which gaming apps are installed."
    And the "Install Selected" button is insensitive
    And the "Remove Selected" button is insensitive
    And the application log contains "views: gaming status unavailable"
    And the action journal is empty

  @stub.features-gaming-image @stub.features-gaming-installed
  Scenario: A gaming image lists its system-wide components
    Given ChairLift is running
    When I open the "Features" page
    Then each gaming component says "Installed system-wide"
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
    And I do not see "WSL Backend"
    And I do not see "Enable Docker"

  @config.features-no-desktop @stub.features-no-descriptor @stub.features-gaming-none
  @env.CHAIRLIFT_CAPABILITIES=image-descriptor,flatpak,brew,bootc-stage
  Scenario: A host with nothing to offer does not advertise a blank Features page
    Given ChairLift is running
    Then the Features destination is hidden or says why it offers nothing
    And I see "Nothing to set up here"
    And the application log contains "views: features page offers nothing on this system"

  # ------------------------------------------------------------ Printers
  #
  # ADR-0016: a printer application may be enabled only when its web
  # administration is authenticated or absent. No published image accepts
  # that setting yet, so every family is an actionable, non-enabled state:
  # the row is shown, its switch is off and locked, and the subtitle says
  # what is needed. Rendering the group runs nothing and writes nothing.

  @stub.printers @stub.features-gaming-none
  Scenario: Every printer family is shown locked until its image can secure its administration page
    Given ChairLift is running
    When I open the "Features" page
    Then the Features page shows a "Printers" group
    And the Printers group offers exactly these rows, each off and locked
      | row                  |
      | Ghostscript printers |
      | HP printers (HPLIP)  |
      | Gutenprint printers  |
    And the application log contains "views: printers group built families=3 blocked=3"
    And the systemctl tool was never asked to mutate
    And no printer quadlet was written
    And the action journal is empty

  @env.CHAIRLIFT_CAPABILITIES=image-descriptor,flatpak,brew,bootc-stage @stub.features-gaming-none
  Scenario: Without Podman the Printers group is explained on Help, not shown
    Given ChairLift is running
    When I open the "Features" page
    Then the Features page shows no "Printers" group
    And I do not see "Ghostscript printers"
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
    And I do not see "Ghostscript printers"
    And the application log does not contain "views: printers group built"
    When I press "F1"
    And I ask the Help page why something is missing
    Then the Help page explains "Agent Mode" with "Needs Homebrew"
    And the Help page does not call "Printers" missing

  @stub.features-extensions
  Scenario: Desktop integrations preserve their observed state in preview mode
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
  Scenario: Desktop integrations restore existing GNOME choices instead of defaults
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
    Then the "Tailscale Integration" row says "This GNOME extension is not installed."
    And the switch in the "Tailscale Integration" row refuses input
    And the "Sync Folder Integration" row says "This GNOME extension is not installed."
    And the switch in the "Sync Folder Integration" row refuses input
