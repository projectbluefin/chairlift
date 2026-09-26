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
