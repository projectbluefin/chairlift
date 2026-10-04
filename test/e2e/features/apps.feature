@apps
Feature: Apps destination
  The Apps page offers Homebrew collections first, installed packages next,
  and a Brewfile exporter last. Package mutations require confirmation.
  ChairLift runs with --dry-run: commands are previewed, controls restored,
  and known inventory preserved. Stubs record every actual invocation.
  Each scenario names its configuration: behave lists a Feature's tags after
  a Scenario's, so a Feature-level @config would override any scenario's.

  @config.apps-bundles @stub.apps-brew @stub.apps-flatpak @stub.apps-collections @env.CHAIRLIFT_CAPABILITIES=brew
  Scenario: Collections lead the Homebrew-only inventory and exporter
    Given ChairLift is running
    When I open the "Apps" page
    Then the Apps groups are ordered exactly
      | title                 |
      | App collections       |
      | Homebrew applications |
      | Command line tools    |
      | Packages from Homebrew |
    And I do not see "Firefox"
    And I do not see "Text Editor"
    And Flatpak was never asked to "list"
    Then the "Command line tools" apps group says "2 installed"
    And the "Homebrew applications" apps group says "1 installed"
    Then the "Command line tools" apps group shows exactly
      | title   |
      | jq      |
      | ripgrep |
    And the "jq" row says "1.7.1"
    And the "ripgrep" row says "14.1.1 • Pinned"
    And the "Pin" button in the "jq" row is sensitive
    And the "Unpin" button in the "ripgrep" row is sensitive
    And I do not see "libunistring"
    Then the "Homebrew applications" apps group shows exactly
      | title              |
      | visual-studio-code |

  @config.apps-bundles @stub.apps-brew
  Scenario: Cancelling an uninstall changes nothing and leaves the row usable
    Given ChairLift is running
    When I open the "Apps" page
    And I click the "Uninstall" button in the "jq" row
    Then a dialog titled "Uninstall jq?" is shown
    When I choose "Cancel" in the dialog
    Then no dialog is shown
    And the "Uninstall" button in the "jq" row is sensitive
    And the "Pin" button in the "jq" row is sensitive
    And the application log previews no "brew uninstall"
    And Homebrew was never asked to "uninstall"

  @config.apps-bundles @stub.apps-brew
  Scenario Outline: A confirmed uninstall of <name> is previewed and the known inventory is kept
    Given ChairLift is running
    When I open the "Apps" page
    And I click the "Uninstall" button in the "<name>" row
    Then a dialog titled "Uninstall <name>?" is shown
    When I choose "Uninstall" in the dialog
    Then the application log previews "<command>" exactly once
    And a toast on the Apps page says "[DRY-RUN] Preview: <name> would be uninstalled — no changes made"
    And the "Uninstall" button in the "<name>" row is sensitive
    And the "<list>" apps group says "<count>"
    And Homebrew was never asked to "uninstall"

    Examples:
      | list               | name               | count       | command                                  |
      | Command line tools | jq                 | 2 installed | brew uninstall jq                        |
      | Homebrew applications| visual-studio-code | 1 installed | brew uninstall --cask visual-studio-code |

  @config.apps-bundles @stub.apps-brew
  Scenario Outline: <action> on <name> is confirmed, previewed, and restores both row controls
    Given ChairLift is running
    When I open the "Apps" page
    And I click the "<action>" button in the "<name>" row
    Then a dialog titled "<action> <name>?" is shown
    When I choose "<action>" in the dialog
    Then the application log previews "<command>" exactly once
    And the "<action>" button in the "<name>" row is sensitive
    And the "Uninstall" button in the "<name>" row is sensitive
    And the "<name>" row says "<subtitle>"
    And Homebrew was never asked to "<verb>"

    Examples:
      | action | name    | command           | verb  | subtitle        |
      | Pin    | jq      | brew pin jq       | pin   | 1.7.1           |
      | Unpin  | ripgrep | brew unpin ripgrep | unpin | 14.1.1 • Pinned |

  @config.apps-bundles @stub.apps-brew-unreadable @stub.apps-collections
  Scenario: An unreadable Homebrew inventory leaves collections and export usable
    Given ChairLift is running
    When I open the "Apps" page
    Then the "Command line tools" apps group says "Could not read the list"
    And the "Homebrew applications" apps group says "Could not read the list"
    And the "Install" button in the "Team tools" row is sensitive
    And the "Export" button in the "Export package list" row is sensitive

  @config.apps-bundles @stub.apps-brew @env.CHAIRLIFT_CAPABILITIES=image-descriptor,flatpak
  Scenario: A host without Homebrew omits Apps even with Flatpak available
    Given ChairLift is running
    Then the sidebar omits "Apps"

  @config.apps-bundles @stub.apps-brew @stub.apps-collections
  Scenario: A confirmed app collection install is previewed and its button restored
    Given ChairLift is running
    When I open the "Apps" page
    Then the "Coding fonts" row says "Fixed-width fonts made for reading code. Includes 3 apps and tools."
    And the "Team tools" row says "Tools our team relies on every day. Includes 1 app or tool."
    When I click the "Install" button in the "Coding fonts" row
    Then the application log contains "[DRY-RUN] Would execute: brew bundle install --file="
    And the application log contains "/bundles/fonts-dev.Brewfile"
    And a toast on the Apps page says "[DRY-RUN] Preview: Coding fonts would be installed — no changes made"
    And the "Install" button in the "Coding fonts" row is sensitive
    And I do not see "Installing collection"
    And I do not see "Installing collection…"
    And Homebrew was never asked to "bundle"

  @config.apps-bundles @stub.apps-brew
  Scenario: A system with no app collections says none are offered
    Given ChairLift is running
    When I open the "Apps" page
    Then I see "No collections available"
    And I see "This system does not offer any app collections."

  @config.apps-collections-only @stub.apps-brew @stub.apps-collections
  Scenario: App collections install without the Homebrew package groups
    Given ChairLift is running
    When I open the "Apps" page
    Then the Apps page does not show the "Packages from Homebrew" group
    And the Apps page does not show the "Command line tools" group
    And the Apps page does not show the "Homebrew applications" group
    When I click the "Install" button in the "Team tools" row
    Then the application log contains "/bundles/team-tools.Brewfile"
    And the "Install" button in the "Team tools" row is sensitive
    And I do not see "Installing collection"
    And I do not see "Installing collection…"
    And the application log does not contain "panic"

  @config.apps-bundles @stub.apps-brew
  Scenario: Exporting the package list is previewed and writes nothing
    Given ChairLift is running
    When I open the "Apps" page
    And I click the "Export" button in the "Export package list" row
    Then the application log contains "[DRY-RUN] Would execute: brew bundle dump --file="
    And the application log contains "/home/Brewfile --force"
    And the "Export" button in the "Export package list" row is sensitive
    And the home directory has no "Brewfile"
    And Homebrew was never asked to "bundle"

  @config.apps-bundles @stub.apps-brew @stub.apps-collections
  Scenario: Every control on the Apps page, including every list row's, is named and operable
    Given ChairLift is running
    When I open the "Apps" page
    Then the "Pin" button in the "jq" row is sensitive
    And the "Install" button in the "Team tools" row is sensitive
    And the "Uninstall" button in the "visual-studio-code" row is sensitive
    And the "Export" button in the "Export package list" row is sensitive
    And every visible action control has an accessible name and an action
