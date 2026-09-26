@livery
Feature: Livery
  The Livery page sets three marks — the app grid (who you are), the panel
  (who you stand with), and the Files icon (what you roll with) — plus the
  account's profile picture. Loading it must write nothing, every change is a
  dry run here, and the artwork services it fetches from are unreachable on
  purpose (the livery-offline stub) so no assertion depends on the network.
  The livery-tools stub replaces every program the page may run with a
  recorder and answers the Custom Command Menu extension as installed.

  @stub.livery-tools @stub.livery-saved-state @stub.livery-offline
  Scenario: Restoring a saved configuration on load writes nothing
    Given ChairLift is running
    When I open the "Livery" page
    Then the "Customize the App Grid Icon" switch in the Livery "App Grid Livery" section is on
    And the "Brand" row in the Livery "App Grid Livery" section says "GitLab"
    And the "Customize the Panel Icon" switch in the Livery "Foundational Livery" section is on
    And the "Mark" row in the Livery "Foundational Livery" section says "GNOME Foundation"
    And the "Rotate at Login" switch in the Livery "Foundational Livery" section is on
    And the "Customize the Files Icon" switch in the Livery "Dock Livery" section is on
    And the "Project" row in the Livery "Dock Livery" section says "Prometheus"
    And the "Rotate at Login" switch in the Livery "Dock Livery" section is on
    And the Livery page announced no dry-run change
    And no Livery fetch was attempted
    And no Livery command changed any setting
    And no icon was written under the home directory
    And no rotation unit was written under the home directory

  @stub.livery-tools
  Scenario: A fresh account starts with every mark off and its dependents locked
    Given ChairLift is running
    When I open the "Livery" page
    Then the Livery page has finished loading
    And the "Customize the Panel Icon" switch in the Livery "Foundational Livery" section is off
    And the "Mark" row in the Livery "Foundational Livery" section says "Cloud Native Computing Foundation"
    And the "Mark" row in the Livery "Foundational Livery" section is insensitive
    And the "Rotate at Login" switch in the Livery "Foundational Livery" section is insensitive
    And the "Brand" row in the Livery "App Grid Livery" section is insensitive
    And the "Project" row in the Livery "Dock Livery" section is insensitive
    And the "Rotate at Login" switch in the Livery "Dock Livery" section is insensitive
    And the Livery page announced no dry-run change

  @stub.livery-no-extension
  Scenario: Without the Custom Command Menu extension the panel section is hidden
    Given ChairLift is running
    When I open the "Livery" page
    Then the Livery page has finished loading
    And the Livery "Foundational Livery" section is not shown
    And the Livery "App Grid Livery" section is shown
    And the Livery "Dock Livery" section is shown

  @stub.livery-tools
  Scenario: Turning the panel mark on installs the default mark as a dry run
    Given ChairLift is running
    When I open the "Livery" page
    And I toggle the "Customize the Panel Icon" switch in the Livery "Foundational Livery" section
    Then the Livery dry run would set saved-panel-icon to ""
    And the Livery dry run would set panel-enabled to true
    And the Livery dry run would install the "chairlift-livery-cncf-symbolic" icon in the "hicolor" theme
    And the Livery dry run would point the panel at "chairlift-livery-cncf-symbolic"
    And the "Customize the Panel Icon" switch in the Livery "Foundational Livery" section is on
    And the "Customize the Panel Icon" switch in the Livery "Foundational Livery" section is sensitive
    And the "Mark" row in the Livery "Foundational Livery" section is sensitive
    And the "Rotate at Login" switch in the Livery "Foundational Livery" section is sensitive
    And no Livery command changed any setting
    And no icon was written under the home directory
    And the action journal is empty

  @stub.livery-tools @stub.livery-user-panel-icon
  Scenario: A panel icon the user set themselves is kept for revert
    Given ChairLift is running
    When I open the "Livery" page
    And I toggle the "Customize the Panel Icon" switch in the Livery "Foundational Livery" section
    Then the Livery dry run would set saved-panel-icon to "starred-symbolic"
    And the Livery page ran "dconf read -d /org/gnome/shell/extensions/custom-command-list/menuicon-setting"
    And the Livery dry run would point the panel at "chairlift-livery-cncf-symbolic"

  @stub.livery-tools @stub.livery-panel-on
  Scenario: Picking a foundation mark from the searchable chooser
    Given ChairLift is running
    When I open the "Livery" page
    And I activate the "Mark" row in the Livery "Foundational Livery" section
    Then the Livery chooser titled "Choose a Mark" is shown
    And the Livery chooser offers "Cloud Native Computing Foundation"
    And the Livery chooser offers "Custom SVG…"
    When I search the Livery chooser for "gnome"
    Then the Livery chooser offers "GNOME Foundation"
    And the Livery chooser does not offer "Cloud Native Computing Foundation"
    When I pick "GNOME Foundation" in the Livery chooser
    Then the Livery chooser is closed
    And the "Mark" row in the Livery "Foundational Livery" section says "GNOME Foundation"
    And the Livery dry run would set panel-foundation to "gnome"
    And the Livery dry run would install the "chairlift-livery-gnome-symbolic" icon in the "hicolor" theme
    And the Livery dry run would point the panel at "chairlift-livery-gnome-symbolic"
    And no icon was written under the home directory

  @stub.livery-tools @stub.livery-dock-on
  Scenario: A search that matches nothing offers nothing to pick
    Given ChairLift is running
    When I open the "Livery" page
    And I activate the "Project" row in the Livery "Dock Livery" section
    Then the Livery chooser titled "Choose a Project" is shown
    When I search the Livery chooser for "zzqxnothing"
    Then the Livery chooser offers only "No matching project"
    And the Livery chooser says "Nothing in cncf/artwork matches"
    When I pick "No matching project" in the Livery chooser
    Then the Livery chooser is still open
    And the Livery dry run would not set dock-foundation
    And the "Project" row in the Livery "Dock Livery" section says "Certified Kubernetes"

  @known_issue.350 @stub.livery-tools
  Scenario: The brand chooser's empty result does not blame cncf/artwork
    Given ChairLift is running
    When I open the "Livery" page
    And I toggle the "Customize the App Grid Icon" switch in the Livery "App Grid Livery" section
    And I activate the "Brand" row in the Livery "App Grid Livery" section
    Then the Livery chooser titled "Choose a Brand" is shown
    When I search the Livery chooser for "zzqxnothing"
    Then the Livery chooser offers only "No matching brand"
    And the Livery chooser does not say "cncf/artwork"

  @stub.livery-tools @stub.livery-offline
  Scenario: Picking a brand when simpleicons.org is unreachable fails closed
    Given ChairLift is running
    When I open the "Livery" page
    And I toggle the "Customize the App Grid Icon" switch in the Livery "App Grid Livery" section
    Then the Livery dry run would set app-grid-enabled to true
    And the "Customize the App Grid Icon" switch in the Livery "App Grid Livery" section is sensitive
    When I activate the "Brand" row in the Livery "App Grid Livery" section
    Then the Livery chooser titled "Choose a Brand" is shown
    When I search the Livery chooser for "gitlab"
    And I pick "GitLab" in the Livery chooser
    Then the Livery chooser is closed
    And the "Brand" row in the Livery "App Grid Livery" section says "GitLab"
    And the Livery dry run would set app-grid-slug to "gitlab"
    And a Livery error toast says "Livery: fetching that brand mark failed"
    And the Livery dry run would install no icon
    And no icon was written under the home directory

  @stub.livery-tools @stub.livery-dock-on @stub.livery-offline
  Scenario: Picking a CNCF project when cncf/artwork is unreachable fails closed
    Given ChairLift is running
    When I open the "Livery" page
    And I activate the "Project" row in the Livery "Dock Livery" section
    Then the Livery chooser titled "Choose a Project" is shown
    When I search the Livery chooser for "prometheus"
    And I pick "Prometheus" in the Livery chooser
    Then the Livery chooser is closed
    And the "Project" row in the Livery "Dock Livery" section says "Prometheus"
    And the Livery dry run would set dock-foundation to "prometheus"
    And a Livery error toast says "Livery: fetching that project's icon failed"
    And the Livery dry run would install no icon
    And no icon was written under the home directory

  @stub.livery-tools @stub.livery-panel-on
  Scenario: Rotation at login is scheduled as a dry run and writes no unit
    Given ChairLift is running
    When I open the "Livery" page
    And I toggle the "Rotate at Login" switch in the Livery "Foundational Livery" section
    Then the Livery dry run would set panel-rotate to true
    And the Livery dry run would write and enable the rotation unit
    And the "Rotate at Login" switch in the Livery "Foundational Livery" section is on
    And no rotation unit was written under the home directory
    When I toggle the "Rotate at Login" switch in the Livery "Foundational Livery" section
    Then the Livery dry run would set panel-rotate to false
    And the "Rotate at Login" switch in the Livery "Foundational Livery" section is off
    And no Livery command changed any setting
    And no rotation unit was written under the home directory

  @stub.livery-tools @stub.livery-offline
  Scenario: A profile picture that cannot be downloaded is never offered for Apply
    Given ChairLift is running
    When I open the "Livery" page
    Then the "No picture set" row in the Livery "Profile Picture" section says "Choose a dinosaur from Project Bluefin's artwork"
    When I press the "Choose…" button in the Livery "Profile Picture" section
    Then the Livery chooser titled "Choose a Profile Picture" is shown
    And the Livery "Apply" button is insensitive
    When I pick "Bluefin" in the Livery chooser
    Then the Livery chooser says "Could not download Bluefin. Your picture was not changed"
    And the Livery "Apply" button is insensitive
    And no profile picture was staged under the home directory
    And the action journal is empty

  @known_issue.350 @stub.livery-tools
  Scenario: The profile picture button is announced by what it does
    Given ChairLift is running
    When I open the "Livery" page
    Then the "Choose…" button in the Livery "Profile Picture" section is announced as "Choose…"

  @config.livery-sections-off @stub.livery-tools
  Scenario: Sections disabled in configuration are never built
    Given ChairLift is running
    When I open the "Livery" page
    Then the Livery page has finished loading
    And the Livery "Profile Picture" section is not shown
    And the Livery "App Grid Livery" section is not shown
    And the Livery "Foundational Livery" section is shown
    And the Livery "Dock Livery" section is shown
    And the Livery page announced no dry-run change
