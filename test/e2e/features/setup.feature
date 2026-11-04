@setup
Feature: Explicit first-run software setup
  Setup is never automatic. The requested wizard opens with a Welcome screen,
  then walks the existing Livery, Features, Apps, and Agents destinations without duplicating their controls.

  Scenario: Ordinary activation does not open setup
    Given ChairLift is running
    Then the setup wizard is not shown
    And the setup dry run would record no disposition
    And the action journal is empty

  Scenario: The menu starts with Welcome
    Given ChairLift is running
    When I open the main menu
    And I choose "Setup Assistant…" from the menu
    Then the setup wizard shows "Welcome"
    And the setup "Back" button is insensitive
    And the action journal is empty

  @args.--first-run @stub.livery-tools @stub.apps-brew @env.SHELL=/bin/bash
  Scenario: First-run walks existing pages in order with Back and Finish
    Given ChairLift is running
    Then the setup wizard shows "Welcome"
    And the setup has only one "Back" button
    And the setup "Back" button is insensitive
    When I use the setup "Next" button
    Then the setup wizard shows "Livery"
    And the setup "Back" button is sensitive
    When I use the setup "Back" button
    Then the setup wizard shows "Welcome"
    When I use the setup "Next" button
    Then the setup wizard shows "Livery"
    When I use the setup "Next" button
    Then the setup wizard shows "Features"
    And the switch in the "Bluefin CLI" row is on
    When I use the setup "Next" button
    Then the setup wizard shows "Apps"
    And the application log contains "[DRY-RUN] Would enable Bluefin CLI for Bash"
    When I use the setup "Next" button
    Then the setup wizard shows "Agents"
    When I use the setup "Back" button
    Then the setup wizard shows "Apps"
    When I use the setup "Next" button
    And I use the setup "Next" button
    Then the setup wizard shows "Conclusion"
    And the setup "Finish" button is sensitive
    And the setup dry run would record no disposition
    When I use the setup "Back" button
    Then the setup wizard shows "Agents"
    When I use the setup "Next" button
    Then the setup wizard shows "Conclusion"
    When I use the setup "Finish" button
    Then the setup wizard is not shown
    And the setup dry run would record disposition completed
    And the setup dry run would record the completed version
    And the action journal is empty

  @args.--setup
  Scenario: Setup uses the same explicit wizard
    Given ChairLift is running
    Then the setup wizard shows "Welcome"
    When I use the setup "Dismiss Setup" button
    Then the setup wizard is not shown
    And the setup dry run would record disposition skipped
    And the action journal is empty

  @args.--first-run
  Scenario: Escape intentionally dismisses setup
    Given ChairLift is running
    Then the setup wizard shows "Welcome"
    When I press "Escape"
    Then the setup wizard is not shown
    And the setup dry run would record disposition skipped

  @developer-collections @args.--setup @config.apps-bundles @stub.features-gaming-none @stub.features-developer @stub.apps-brew @stub.apps-collections
  Scenario: Wizard defaults select monospace fonts without installing them
    Given ChairLift is running
    Then the setup wizard shows "Welcome"
    When I use the setup "Next" button
    Then the setup wizard shows "Livery"
    When I use the setup "Next" button
    Then the setup wizard shows "Features"
    When I open the Developer Mode detail
    When I expand the "Developer Mode" list under "Developer"
    Then the "Monospace Fonts" collection is checked
    And the action journal is empty
    And the application log previews no "brew bundle install"
    When I toggle the "Monospace Fonts" collection checkbox
    And I use the setup "Next" button
    Then the setup wizard shows "Apps"
    When I use the setup "Back" button
    Then the setup wizard shows "Features"
    When I open the Developer Mode detail
    And the "Monospace Fonts" collection is unchecked
    And the application log previews no "brew bundle install"
    And the action journal is empty

  @args.--setup @stub.apps-brew @env.SHELL=/bin/bash
  Scenario: Bash opt-out survives Back and Next without enabling the feature
    Given ChairLift is running
    When I use the setup "Next" button
    Then the setup wizard shows "Livery"
    When I use the setup "Next" button
    Then the setup wizard shows "Features"
    And the switch in the "Bluefin CLI" row is on
    When I toggle the switch in the "Bluefin CLI" row
    Then the switch in the "Bluefin CLI" row is off
    When I use the setup "Back" button
    Then the setup wizard shows "Livery"
    When I use the setup "Next" button
    Then the setup wizard shows "Features"
    And the switch in the "Bluefin CLI" row is off
    When I use the setup "Next" button
    Then the setup wizard shows "Apps"
    And the application log contains "[DRY-RUN] Would disable Bluefin CLI for Bash"
    And the application log does not contain "[DRY-RUN] Would enable Bluefin CLI for Bash"

  @developer-first-run @args.--first-run @stub.livery-tools @stub.features-gaming-none @stub.features-devmenu
  Scenario: First-run Features opens its Developer detail without escaping the wizard
    Given ChairLift is running
    Then the setup wizard shows "Welcome"
    When I use the setup "Next" button
    Then the setup wizard shows "Livery"
    When I use the setup "Next" button
    Then the setup wizard shows "Features"
    When I open the Developer Mode detail
    Then the setup wizard shows "Development Apps"
    And the Developer Mode switch shows this account's developer-group membership
    When I press "<Alt>1"
    Then the setup wizard shows "Development Apps"
    When I capture the Developer Mode detail evidence
    And I click the "Back to Features" button
    Then the setup wizard shows "Features"
    When I use the setup "Next" button
    Then the setup wizard shows "Apps"
    And the action journal is empty
