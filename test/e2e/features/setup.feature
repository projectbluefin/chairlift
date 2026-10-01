@setup
Feature: Explicit first-run software setup
  Setup is never automatic. The requested wizard walks the existing Features,
  Apps, Agents, and Livery destinations without duplicating their controls.

  Scenario: Ordinary activation does not open setup
    Given ChairLift is running
    Then the setup wizard is not shown
    And the setup dry run would record no disposition
    And the action journal is empty

  Scenario: The menu starts with Features
    Given ChairLift is running
    When I open the main menu
    And I choose "Setup Assistant…" from the menu
    Then the setup wizard shows "Features"
    And the setup "Back" button is insensitive
    And the action journal is empty

  @args.--first-run @stub.livery-tools
  Scenario: First-run walks existing pages in order with Back and Finish
    Given ChairLift is running
    Then the setup wizard shows "Features"
    And the setup has only one "Back" button
    When I use the setup "Next" button
    Then the setup wizard shows "Apps"
    When I use the setup "Next" button
    Then the setup wizard shows "Agents"
    When I use the setup "Back" button
    Then the setup wizard shows "Apps"
    When I use the setup "Next" button
    And I use the setup "Next" button
    Then the setup wizard shows "Livery"
    And the setup "Finish" button is sensitive
    And the setup dry run would record no disposition
    When I use the setup "Finish" button
    Then the setup wizard is not shown
    And the setup dry run would record disposition completed
    And the setup dry run would record the completed version
    And the action journal is empty

  @args.--setup
  Scenario: Setup uses the same explicit wizard
    Given ChairLift is running
    Then the setup wizard shows "Features"
    When I use the setup "Dismiss setup" button
    Then the setup wizard is not shown
    And the setup dry run would record disposition skipped
    And the action journal is empty

  @args.--first-run
  Scenario: Escape intentionally dismisses setup
    Given ChairLift is running
    Then the setup wizard shows "Features"
    When I press "Escape"
    Then the setup wizard is not shown
    And the setup dry run would record disposition skipped
