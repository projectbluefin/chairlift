@navigation
Feature: Navigation through the accessibility tree
  The sidebar and its accelerators are the application's spine. The page
  inventory comes from internal/navigation through CHAIRLIFT_NAVIGATION, so a
  page added there is asserted here without editing this file.

  Scenario: The application publishes a labelled sidebar
    Given ChairLift is running
    Then the sidebar lists every navigation page in order
    And the "Updates" page is shown

  Scenario: Every page is reachable by keyboard
    Given ChairLift is running
    Then every navigation page is reachable by its Alt+number shortcut

  Scenario: Every page is reachable by activating its sidebar row
    Given ChairLift is running
    Then every navigation page is reachable from the sidebar

  Scenario: Every page exposes named, operable controls
    Given ChairLift is running
    Then every navigation page exposes accessible controls

  # GtkListBox selects whichever row takes keyboard focus; the selection must
  # stay on the shown page until a row is activated.
  Scenario: Moving keyboard focus through the sidebar does not move its selection off the shown page
    Given ChairLift is running
    When I open the "Maintenance" page
    And keyboard focus is on the "Maintenance" sidebar row
    And I press "Tab"
    And I press "Tab"
    And I press "<Shift>Tab"
    And I press "<Shift>Tab"
    And I press "<Shift>Tab"
    And I press "Up"
    Then keyboard focus is on a sidebar row other than "Maintenance"
    And the "Maintenance" page is shown
