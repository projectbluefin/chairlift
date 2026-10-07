@help
Feature: Help destination
  Help is the one page every configuration keeps. It lists the support
  links configuration names, in the order internal/views/pageview fixes,
  and explains which features this host cannot back. The xdg-open the
  links reach is stubbed and records instead of opening. Enhanced
  Troubleshooting, which Help once led with, is on the Agents page
  (agents.feature).

  Scenario: F1 opens Help from any page
    Given ChairLift is running
    And I open the "Maintenance" page
    When I press "F1"
    Then the "Help" page is shown

  @config.help-links
  Scenario: The support links come from configuration, in display order
    Given ChairLift is running
    When I press "F1"
    Then the Help resources are, in order
      | title                 | url                                     |
      | Visit project website | https://example.test/site               |
      | Report a problem      | https://example.test/issues?labels=help |
      | Browse documentation  | https://example.test/docs/#start        |

  @config.help-links-markup
  Scenario: A support URL containing "&" is shown exactly as configured
    Given ChairLift is running
    When I press "F1"
    Then the Help resources are, in order
      | title                 | url                                                |
      | Visit project website | https://example.test/site                          |
      | Report a problem      | https://example.test/issues?labels=help&state=open |
      | Browse documentation  | https://example.test/docs/                         |

  @config.help-links-markup @stub.help-xdg-open
  Scenario: A support URL containing "&" still opens verbatim
    Given ChairLift is running
    When I press "F1"
    And I open the "Report a problem" Help link
    Then xdg-open was asked to open "https://example.test/issues?labels=help&state=open"

  @config.help-links-partial
  Scenario: An empty link is dropped and the rest keep their order
    Given ChairLift is running
    When I press "F1"
    Then the Help resources are, in order
      | title                 | url                        |
      | Visit project website | https://example.test/site  |
      | Browse documentation  | https://example.test/docs/ |

  @config.help-links @stub.help-xdg-open
  Scenario Outline: Opening a support link hands its exact URL to xdg-open
    Given ChairLift is running
    When I press "F1"
    And I open the "<title>" Help link
    Then xdg-open was asked to open "<url>"
    And the application log contains "Opening URL: <url>"
    And I do not see "Couldn't open"
    And the Help page is still responsive
    And the action journal is empty

    Examples:
      | title                 | url                                     |
      | Visit project website | https://example.test/site               |
      | Report a problem      | https://example.test/issues?labels=help |
      | Browse documentation  | https://example.test/docs/#start        |

  @stub.help-xdg-open-fails
  Scenario: A link with no URL handler reports the failure and keeps the page
    Given ChairLift is running
    When I press "F1"
    And I open the "Report a problem" Help link
    Then xdg-open was asked to open "https://github.com/projectbluefin/dakota/issues"
    And I see "Couldn't open https://github.com/projectbluefin/dakota/issues."
    And the Help page is still responsive

  @config.help-links @stub.help-xdg-open-fails
  Scenario: A new failure is visible without dismissing the previous error
    Given ChairLift is running
    When I press "F1"
    And I open the "Visit project website" Help link
    Then a toast says "Couldn't open https://example.test/site."
    When I open the "Browse documentation" Help link
    Then a toast says "Couldn't open https://example.test/docs/#start."
    And the Help page is still responsive

  @config.help-no-resources
  Scenario: Disabling the resources group removes the links but keeps Help
    Given ChairLift is running
    When I select "Help" in the sidebar
    Then the Help page has no resource links
    And I do not see "Troubleshooting"
    And I see "System diagnostics"
    When I open the "Updates" page
    And I press "F1"
    Then the "Help" page is shown

  Scenario: Copying diagnostics confirms and re-enables the button
    Given ChairLift is running
    When I press "F1"
    And I click the "Copy" button in the "System diagnostics" row
    Then I see "Copied to the clipboard."
    And the "Copy" button in the "System diagnostics" row is sensitive
    And the action journal is empty
