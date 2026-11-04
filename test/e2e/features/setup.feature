@setup
Feature: Explicit first-run software setup
  Setup is never automatic. The requested wizard opens with a Welcome screen,
  then walks the existing Features, Apps, Agents, and Livery destinations without duplicating their controls.

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

  @args.--first-run @stub.livery-tools
  Scenario: First-run walks existing pages in order with Back and Finish
    Given ChairLift is running
    Then the setup wizard shows "Welcome"
    And the setup has only one "Back" button
    And the setup "Back" button is insensitive
    When I use the setup "Next" button
    Then the setup wizard shows "Features"
    And the setup "Back" button is sensitive
    When I use the setup "Back" button
    Then the setup wizard shows "Welcome"
    When I use the setup "Next" button
    Then the setup wizard shows "Features"
    When I use the setup "Next" button
    Then the setup wizard shows "Apps"
    When I use the setup "Next" button
    Then the setup wizard shows "Agents"
    When I use the setup "Back" button
    Then the setup wizard shows "Apps"
    When I use the setup "Next" button
    And I use the setup "Next" button
    Then the setup wizard shows "Livery"
    And the setup "Next" button is sensitive
    When I use the setup "Next" button
    Then the setup wizard shows "Conclusion"
    And the setup "Finish" button is sensitive
    And the setup dry run would record no disposition
    When I use the setup "Back" button
    Then the setup wizard shows "Livery"
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
  Scenario: Wizard defaults select terminal enhancements and monospace fonts without installing them
    Given ChairLift is running
    Then the setup wizard shows "Welcome"
    When I use the setup "Next" button
    Then the setup wizard shows "Features"
    When I expand the "Developer Mode" list under "Developer"
    Then the "Enhanced Terminal Bling" collection is checked
    And the "Monospace Fonts" collection is checked
    And the action journal is empty
    And the application log previews no "brew bundle install"
    When I toggle the "Enhanced Terminal Bling" collection checkbox
    And I toggle the "Monospace Fonts" collection checkbox
    And I use the setup "Next" button
    Then the setup wizard shows "Apps"
    When I use the setup "Back" button
    Then the setup wizard shows "Features"
    And the "Enhanced Terminal Bling" collection is unchecked
    And the "Monospace Fonts" collection is unchecked
    And the application log previews no "brew bundle install"
    And the action journal is empty
