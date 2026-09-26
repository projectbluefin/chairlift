Feature: Main menu, shortcuts and About
  The header's main menu is the only pointer route to the keyboard shortcuts
  and the About dialog, so both must be reachable through AT-SPI.

  Scenario: The main menu offers its items
    Given ChairLift is running
    When I open the main menu
    Then the menu has 4 operable items
    And the menu offers "Keyboard Shortcuts"
    And the menu offers "About Control Center"

  @known_issue.347
  Scenario: Every main menu item has an accessible name
    Given ChairLift is running
    When I open the main menu
    Then every menu item has an accessible name

  Scenario: The shortcuts window lists every navigation shortcut
    Given ChairLift is running
    When I open the main menu
    And I choose "Keyboard Shortcuts" from the menu
    Then a window titled "Keyboard Shortcuts" is shown
    And the shortcuts window lists every navigation shortcut
    When I close the "Keyboard Shortcuts" window
    Then the "Updates" page is shown

  Scenario: The shortcuts window opens from the keyboard
    Given ChairLift is running
    When I press "<Control><Shift>slash"
    Then a window titled "Keyboard Shortcuts" is shown

  Scenario: The About dialog announces the product
    Given ChairLift is running
    When I open the main menu
    And I choose "About Control Center" from the menu
    Then a window titled "About" is shown
    And the "About" window lists "Control Center"
    And the action journal is empty
