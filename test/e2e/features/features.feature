@features
Feature: Features page — Developer Mode, WSL Mode, Docker, Gaming Mode, Printers, and Compass
  Optional tools are explicit choices. Package disclosures are informational:
  expanding a row must never toggle it or start setup. Docker uses the system
  image's daemon through fixed administrator-authorized helpers. Enabling
  Docker or WSL first enables Developer Mode when needed. Dry runs restore
  every control and preserve observed installed state.

  @stub.features-gaming-none @stub.features-developer
  Scenario: Docker discloses five system-image RPMs without enabling anything
    Given ChairLift is running
    When I open the "Features" page
    And I record the Features switches and mutation evidence
    Then each Features switch is named and separately keyboard reachable
    And I expand the "Enable Docker" package disclosure with the keyboard
    Then the "Enable Docker" package disclosure shows exactly
      | package               |
      | containerd.io         |
      | docker-buildx-plugin  |
      | docker-ce             |
      | docker-ce-cli         |
      | docker-compose-plugin |
    And the "Enable Docker" package disclosure is informational
    And expanding disclosures changes no Features switch and requests no mutation
    And the Features page shows no "Setup activity" group

  @stub.features-gaming-none @stub.features-developer
  Scenario: Both WSL engines disclose only the packages their selected backend installs
    Given ChairLift is running
    When I open the "Features" page
    And I record the Features switches and mutation evidence
    And I expand the "WSL Mode" package disclosure with the keyboard
    Then the "WSL Mode" package disclosure shows exactly
      | package          |
      | frostyard/tap/nsl |
    And the "WSL Mode" package disclosure is informational
    When I choose the "Lima" WSL engine
    Then the "WSL Mode" package disclosure shows exactly
      | package |
      | lima    |
    And the "WSL Mode" package disclosure is informational
    When I choose the "Built-in" WSL engine
    Then the "WSL Mode" package disclosure shows exactly
      | package          |
      | frostyard/tap/nsl |
    And expanding disclosures changes no Features switch and requests no mutation

  @stub.features-gaming-none @stub.features-developer
  Scenario: Default Developer Mode discloses the Dev Container CLI but no optional installations
    Given ChairLift is running
    When I open the "Features" page
    And I record the Features switches and mutation evidence
    And I expand the "Developer Mode" package disclosure with the keyboard
    Then the "Developer Mode" package disclosure shows exactly
      | package      |
      | devcontainer |
    And the "Developer Mode" package disclosure is informational
    And expanding disclosures changes no Features switch and requests no mutation

  @config.features-packages @stub.features-gaming-none @stub.features-developer @stub.features-developer-packages
  Scenario: Developer Mode discloses Pulp and every configured developer collection entry
    Given ChairLift is running
    When I open the "Features" page
    And I record the Features switches and mutation evidence
    And I expand the "Developer Mode" package disclosure with the keyboard
    And I expand the "Dakota dev flatpaks" package disclosure with the keyboard
    Then the "Developer Mode" package disclosure shows exactly
      | package                              |
      | devcontainer                         |
      | org.gnome.gitlab.cheywood.Pulp         |
      | just                                 |
      | visual-studio-code                   |
      | io.podman_desktop.PodmanDesktop       |
    And the "Developer Mode" package disclosure is informational
    And the developer feed disclosure shows its manual-import file location
    And expanding disclosures changes no Features switch and requests no mutation
    And the Developer feed onboarding never starts

  @stub.features-gaming-none @stub.features-developer
  Scenario: Gaming Mode discloses all four components without installing any
    Given ChairLift is running
    When I open the "Features" page
    And I record the Features switches and mutation evidence
    And I expand the "Gaming Mode" package disclosure with the keyboard
    Then the "Gaming Mode" package disclosure shows exactly
      | package     |
      | ProtonPlus  |
      | Protontricks |
      | GOverlay    |
      | MangoHud    |
    And the "Gaming Mode" package disclosure is informational
    And expanding disclosures changes no Features switch and requests no mutation

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
    And Setup activity is the first Features preference group
    And the direct Developer activity matches the previewed action
    And Setup activity has no running progress or fabricated percentage

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

  @config.features-packages @stub.features-gaming-none @stub.features-developer @stub.features-developer-packages
  Scenario: Previewing Developer Mode never runs optional Pulp feeds or collection installations
    Given ChairLift is running
    When I open the "Features" page
    Then the Developer Mode switch shows this account's developer-group membership
    When I toggle the switch in the "Developer Mode" row
    Then the Developer Mode change is journalled as a dry run of the fixed helper
    And the Developer Mode preview toast is shown
    And the Developer Mode switch returns to its restored state
    And the Dev Container CLI install is previewed only when enabling
    And the direct Developer activity matches the previewed action
    And the Developer feed onboarding never starts
    And the Features CLI fixtures received no mutation commands

  @stub.features-gaming-none @stub.features-developer
  Scenario: Disabling a running WSL machine previews stop without deleting data
    Given ChairLift is running
    When I open the "Features" page
    Then the Developer Mode switch shows this account's developer-group membership
    And I expand the "WSL Mode" package disclosure with the keyboard
    Then I see "Virtual Machine Engine"
    And the switch in the "WSL Mode" row is on
    When I toggle the switch in the "WSL Mode" row
    Then the application log contains "would stop the ubuntu or debian nsl machine WSL Mode manages"
    And the switch in the "WSL Mode" row is on
    And the switch in the "WSL Mode" row accepts input
    And the fake nsl was never asked to "shutdown"
    And the fake nsl was never asked to "remove"
    And the action journal is empty
    And the Developer Mode switch returns to its restored state
    And Setup activity shows these stage statuses
      | stage             | status  |
      | Tools and service | Preview |
      | Verify readiness  | Preview |
    And Setup activity has no Developer access stage
    And Setup activity has no running progress or fabricated percentage

  @stub.features-gaming-none @stub.features-developer
  Scenario: Stopping Lima previews data-preserving shutdown without changing Developer Mode
    Given ChairLift is running
    When I open the "Features" page
    Then the Developer Mode switch shows this account's developer-group membership
    When I expand the "WSL Mode" package disclosure with the keyboard
    And I choose the "Lima" WSL engine
    Then the switch in the "WSL Mode" row is on
    And the switch in the "WSL Mode" row accepts input
    When I toggle the switch in the "WSL Mode" row
    Then the application log contains "would disable Ubuntu autostart and stop the VM without deleting data"
    And the switch in the "WSL Mode" row is on
    And the switch in the "WSL Mode" row accepts input
    And the Developer Mode switch returns to its restored state
    And Setup activity has no Developer access stage
    And the action journal is empty
    And the fake limactl was never asked to "stop"
    And the fake limactl was never asked to "delete"
    And the fake limactl was never asked to "autostart"

  @stub.features-gaming-none @stub.features-developer @stub.features-devmenu
  Scenario: System Docker previews parent promotion before its fixed helper and stays retryable
    Given ChairLift is running
    When I open the "Features" page
    Then the Developer Mode switch shows this account's developer-group membership
    And the switch in the "Enable Docker" row accepts input
    When I toggle the switch in the "Enable Docker" row
    Then system Docker enable is previewed 1 time
    And I see "[DRY-RUN] Preview: Enable Docker — no changes made"
    And the switch in the "Enable Docker" row is off
    And the switch in the "Enable Docker" row accepts input
    And the Developer Mode switch returns to its restored state
    And Setup activity is the first Features preference group
    And Setup activity shows these stage statuses
      | stage             | status  |
      | Tools and service | Preview |
      | Verify readiness  | Preview |
      | VS Code setup     | Preview |
    And the Developer access activity matches this account's required promotion
    And Setup activity has no running progress or fabricated percentage
    When I toggle the switch in the "Enable Docker" row
    Then system Docker enable is previewed 2 times
    And the switch in the "Enable Docker" row is off
    And the switch in the "Enable Docker" row accepts input
    And the Developer Mode switch returns to its restored state
    And Developer onboarding is previewed once per implicit promotion
    And the Docker activity contains only this run's stages
    And the Features CLI fixtures received no mutation commands
    And the systemctl tool was never asked to mutate

  @stub.features-gaming-none @stub.features-developer @stub.features-devmenu
  Scenario: Setup activity exposes real pending reads and retries a failed preview without changing switches
    Given ChairLift is running
    When I open the "Features" page
    Then the Developer Mode switch shows this account's developer-group membership
    And the switch in the "Enable Docker" row accepts input
    When Docker's system-unit check is held before failing
    And I toggle the switch in the "Enable Docker" row
    Then Setup activity is the first Features preference group
    And Setup activity shows these stage statuses
      | stage             | status   |
      | Tools and service | Preview  |
      | Verify readiness  | Working… |
      | VS Code setup     | Waiting  |
    And Setup activity shows running progress without a percentage
    And the switch in the "Developer Mode" row refuses input
    And the switch in the "Enable Docker" row refuses input
    When I release Docker's failed system-unit check
    Then Setup activity shows these stage statuses
      | stage             | status  |
      | Tools and service | Preview |
      | Verify readiness  | Failed  |
      | VS Code setup     | Preview |
    And Setup activity has no running progress or fabricated percentage
    And the "Retry" button is sensitive
    And the switch in the "Enable Docker" row is off
    And the Developer Mode switch returns to its restored state
    When Docker's system-unit check is available again
    And I click the "Retry" button
    Then system Docker enable is previewed 2 times
    And Setup activity shows these stage statuses
      | stage             | status  |
      | Tools and service | Preview |
      | Verify readiness  | Preview |
      | VS Code setup     | Preview |
    And the "Retry" button is not shown
    And Setup activity has no running progress or fabricated percentage
    And the Docker activity contains only this run's stages
    And the switch in the "Enable Docker" row is off
    And the switch in the "Enable Docker" row accepts input
    And the Developer Mode switch returns to its restored state
    And the systemctl tool was never asked to mutate
    And the Features CLI fixtures received no mutation commands

  @stub.features-gaming-none @stub.features-developer
  Scenario: Docker previews native VS Code setup without creating user configuration
    Given ChairLift is running
    When I open the "Features" page
    Then the Developer Mode switch shows this account's developer-group membership
    And the switch in the "Enable Docker" row accepts input
    When I toggle the switch in the "Enable Docker" row
    Then system Docker enable is previewed 1 time
    And VS Code setup is previewed without editing user settings
    And the switch in the "Enable Docker" row is off
    And the Developer Mode switch returns to its restored state
    And the Features CLI fixtures received no mutation commands

  @stub.features-gaming-none @stub.features-developer @stub.features-vscode-settings
  Scenario: Docker's editor preview preserves existing VS Code JSONC byte for byte
    Given ChairLift is running
    When I open the "Features" page
    Then the Developer Mode switch shows this account's developer-group membership
    And the switch in the "Enable Docker" row accepts input
    When I toggle the switch in the "Enable Docker" row
    Then system Docker enable is previewed 1 time
    And VS Code setup is previewed without editing user settings
    And the switch in the "Enable Docker" row is off
    And the Developer Mode switch returns to its restored state

  @stub.features-gaming-none @stub.features-developer @stub.features-vscode-bad-marker
  Scenario: An editor preview failure never claims Docker access changed or leaks raw diagnostics
    Given ChairLift is running
    When I open the "Features" page
    Then the Developer Mode switch shows this account's developer-group membership
    And the switch in the "Enable Docker" row accepts input
    When I toggle the switch in the "Enable Docker" row
    Then system Docker enable is previewed 1 time
    And the editor preview failure reports that no Docker changes were made
    And Setup activity shows these stage statuses
      | stage             | status  |
      | Tools and service | Preview |
      | Verify readiness  | Preview |
      | VS Code setup     | Failed  |
    And Setup activity has no running progress or fabricated percentage
    And the switch in the "Enable Docker" row is off
    And the switch in the "Enable Docker" row accepts input
    And the Developer Mode switch returns to its restored state
    And the Features CLI fixtures received no mutation commands

  @stub.features-gaming-none @stub.features-developer @stub.features-docker-active
  Scenario: An unresponsive system Docker daemon remains stoppable in preview without demoting Developer Mode
    Given ChairLift is running
    When I open the "Features" page
    Then the Developer Mode switch shows this account's developer-group membership
    And the switch in the "Enable Docker" row is on
    And the switch in the "Enable Docker" row accepts input
    And the "Enable Docker" row says "Docker is running, but its socket is not responding."
    When I toggle the switch in the "Enable Docker" row
    Then system Docker disable is previewed 1 time
    And the switch in the "Enable Docker" row is on
    And the switch in the "Enable Docker" row accepts input
    And the Developer Mode switch returns to its restored state
    And Setup activity has no Developer access stage
    And the fake brew was never asked to "uninstall"
    And the Features CLI fixtures received no mutation commands
    And the systemctl tool was never asked to mutate

  @stub.features-gaming-none @stub.features-developer @stub.features-docker-missing
  Scenario: Missing system Docker is an image prerequisite rather than a second daemon install
    Given ChairLift is running
    When I open the "Features" page
    Then I see "Enable Docker"
    And the switch in the "Enable Docker" row is off
    And the switch in the "Enable Docker" row refuses input
    And the "Enable Docker" row says "Docker CE must be provided by your system image. Ask your administrator to use a Docker-enabled image."
    And the action journal is empty
    And the Features CLI fixtures received no mutation commands

  @stub.features-gaming-none @stub.features-developer @stub.features-docker-masked
  Scenario: A masked system socket keeps Docker visible and explains administrator repair
    Given ChairLift is running
    When I open the "Features" page
    Then the switch in the "Enable Docker" row is off
    And the switch in the "Enable Docker" row refuses input
    And the "Enable Docker" row says "Docker's system units are unavailable. Ask your administrator to repair or unmask them."
    And the action journal is empty
    And the Features CLI fixtures received no mutation commands

  @stub.features-gaming-none @stub.features-developer @stub.features-docker-active-missing-socket
  Scenario: A running system service remains stoppable when its paired socket unit is missing
    Given ChairLift is running
    When I open the "Features" page
    Then the Developer Mode switch shows this account's developer-group membership
    And the switch in the "Enable Docker" row is on
    And the switch in the "Enable Docker" row accepts input
    When I toggle the switch in the "Enable Docker" row
    Then system Docker disable is previewed 1 time
    And the switch in the "Enable Docker" row is on
    And the switch in the "Enable Docker" row accepts input
    And the Developer Mode switch returns to its restored state
    And Setup activity has no Developer access stage
    And the Features CLI fixtures received no mutation commands

  @stub.features-gaming-none @stub.features-developer @env.XDG_RUNTIME_DIR=relative
  Scenario: The user runtime directory does not gate system Docker
    Given ChairLift is running
    When I open the "Features" page
    Then the switch in the "Enable Docker" row is off
    And the switch in the "Enable Docker" row accepts input
    And the action journal is empty

  @stub.features-gaming-none @stub.features-developer @stub.features-docker-no-manager
  Scenario: An unreadable system manager is an observation failure
    Given ChairLift is running
    When I open the "Features" page
    Then the switch in the "Enable Docker" row refuses input
    And the "Enable Docker" row says "Couldn't check Docker's system service."
    And the action journal is empty

  @stub.features-gaming-none @stub.features-developer @stub.features-docker-second-read-fails
  Scenario: One running unit cannot enable Docker's control when its paired read failed
    Given ChairLift is running
    When I open the "Features" page
    Then the switch in the "Enable Docker" row is off
    And the switch in the "Enable Docker" row refuses input
    And the "Enable Docker" row says "Couldn't check Docker's system service."
    And the action journal is empty

  @stub.features-gaming-none @stub.features-developer @stub.features-docker-active
  Scenario: A failed system read preserves the observed running Docker switch
    Given ChairLift is running
    When I open the "Features" page
    Then the switch in the "Enable Docker" row is on
    And the switch in the "Enable Docker" row accepts input
    When Docker's system manager becomes unavailable during an engine refresh
    Then the switch in the "Enable Docker" row refuses input
    And the switch in the "Enable Docker" row is on
    And the "Enable Docker" row says "Couldn't check Docker's system service."
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
    And Setup activity is the first Features preference group
    And Setup activity shows these stage statuses
      | stage             | status  |
      | Tools and service | Preview |
      | Verify readiness  | Preview |
    And Setup activity has no running progress or fabricated percentage

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
    And Setup activity shows these stage statuses
      | stage             | status  |
      | Tools and service | Preview |
      | Verify readiness  | Preview |
    And Setup activity has no running progress or fabricated percentage


  @stub.features-gaming-unlistable
  Scenario: Gaming fails closed when installed components cannot be listed
    Given ChairLift is running
    When I open the "Features" page
    Then the "Gaming Mode" row says "Couldn't check which gaming apps are installed."
    And the switch in the "Gaming Mode" row refuses input
    And the application log contains "views: gaming status unavailable"
    And the action journal is empty

  # ------------------------------------------------------------ Compass
  #
  # Compass installs system-wide from the image's FlatPark remote. Previews
  # restore the observed state and stay retryable.

  @stub.features-gaming-none
  Scenario: Compass previews a system install from FlatPark and stays retryable
    Given ChairLift is running
    When I open the "Features" page
    Then the Features page shows a "Compass" group
    And the switch in the "Compass" row accepts input
    And the switch in the "Compass" row is off
    When I toggle the switch in the "Compass" row
    Then the application log contains "[DRY-RUN] Would execute: flatpak install -y --system flatpark org.tunaos.compass"
    And the switch in the "Compass" row is off
    And the switch in the "Compass" row accepts input
    And the fake flatpak was never asked to "install"
    And the action journal is empty

  @stub.features-compass-installed
  Scenario: Removing Compass asks first and previews a system removal
    Given ChairLift is running
    When I open the "Features" page
    Then the switch in the "Compass" row accepts input
    And the switch in the "Compass" row is on
    When I toggle the switch in the "Compass" row
    Then a dialog titled "Remove Compass?" is shown
    When I choose "Cancel" in the dialog
    Then the switch in the "Compass" row is on
    And the application log does not contain "flatpak uninstall"
    When I toggle the switch in the "Compass" row
    Then a dialog titled "Remove Compass?" is shown
    When I choose "Remove" in the dialog
    Then the application log contains "[DRY-RUN] Would execute: flatpak uninstall -y --system org.tunaos.compass"
    And the switch in the "Compass" row is on
    And the fake flatpak was never asked to "uninstall"
    And the action journal is empty

  @stub.features-compass-no-remote
  Scenario: Without the FlatPark remote Compass explains why it cannot install
    Given ChairLift is running
    When I open the "Features" page
    Then the "Compass" row says "Compass installs from FlatPark, which isn't set up on this computer."
    And the switch in the "Compass" row refuses input
    And the switch in the "Compass" row is off
    And the fake flatpak was never asked to "remote-add"

  @stub.features-gaming-unlistable
  Scenario: Compass fails closed when installations cannot be listed
    Given ChairLift is running
    When I open the "Features" page
    Then the "Compass" row says "Couldn't check whether Compass is installed."
    And the switch in the "Compass" row refuses input
    And the application log contains "views: compass status unavailable"

  @config.features-no-compass @stub.features-gaming-none
  Scenario: Disabling compass_group in configuration removes the group
    Given ChairLift is running
    When I open the "Features" page
    Then the Features page shows a "Gaming" group
    And the application log does not contain "views: compass group built"
    And the Features page shows no "Compass" group

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
  @env.CHAIRLIFT_CAPABILITIES=image-descriptor,brew,bootc-stage
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
