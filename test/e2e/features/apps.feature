@apps
Feature: Apps destination
  The Apps page lists what Homebrew and Flatpak report as installed, searches
  both Homebrew namespaces, and confirms every Homebrew package mutation.
  ChairLift runs with --dry-run, so a confirmed mutation is previewed in the
  log, never executed, and its controls come back usable with the known
  inventory unchanged. brew, flatpak, and gtk-launch are stubs serving a
  fixed catalog (fixtures/stubs_apps.py) that record every invocation.
  Each scenario names its configuration: behave lists a Feature's tags after
  a Scenario's, so a Feature-level @config would override any scenario's.

  @config.apps-bundles @stub.apps-brew @stub.apps-flatpak
  Scenario: Installed Homebrew formulae and casks are listed with their state
    Given ChairLift is running
    When I open the "Apps" page
    Then the "Command line tools" list under "Packages from Homebrew" says "2 installed"
    And the "Applications" list under "Packages from Homebrew" says "1 installed"
    When I expand the "Command line tools" list under "Packages from Homebrew"
    Then the "Command line tools" list under "Packages from Homebrew" shows exactly
      | title   |
      | jq      |
      | ripgrep |
    And the "jq" row says "1.7.1"
    And the "ripgrep" row says "14.1.1 • Pinned"
    And the "Pin" button in the "jq" row is sensitive
    And the "Unpin" button in the "ripgrep" row is sensitive
    When I expand the "Applications" list under "Packages from Homebrew"
    Then the "Applications" list under "Packages from Homebrew" shows exactly
      | title              |
      | visual-studio-code |

  @config.apps-bundles @stub.apps-brew @stub.apps-flatpak
  Scenario: Search covers formulae and casks, and a new search replaces the old results
    Given ChairLift is running
    When I open the "Apps" page
    And I search Homebrew for "lazy"
    Then the "Results" list under "Find more apps and tools" says "3 results"
    And Homebrew was asked to "search --formula lazy"
    And Homebrew was asked to "search --cask lazy"
    When I expand the "Results" list under "Find more apps and tools"
    Then the "Results" list under "Find more apps and tools" shows exactly
      | title      |
      | lazydocker |
      | lazygit    |
      | lazyterm   |
    And the "lazygit" row says "Command line tool"
    And the "lazyterm" row says "Application"
    When I search Homebrew for "chezmoi"
    Then the "Results" list under "Find more apps and tools" says "1 result"
    When I expand the "Results" list under "Find more apps and tools"
    Then the "Results" list under "Find more apps and tools" shows exactly
      | title   |
      | chezmoi |

  @config.apps-bundles @stub.apps-brew @stub.apps-flatpak
  Scenario: A search neither namespace matches reports no results, not a failure
    Given ChairLift is running
    When I open the "Apps" page
    And I search Homebrew for "zzz-nothing"
    Then the "Results" list under "Find more apps and tools" says "No results"

  @config.apps-bundles @stub.apps-brew-search-broken @stub.apps-flatpak
  Scenario: A search Homebrew cannot complete says so
    Given ChairLift is running
    When I open the "Apps" page
    And I search Homebrew for "lazy"
    Then the "Results" list under "Find more apps and tools" says "Search could not be completed"

  @config.apps-bundles @stub.apps-brew @stub.apps-flatpak
  Scenario: Cancelling an install changes nothing and leaves the result installable
    Given ChairLift is running
    When I open the "Apps" page
    And I search Homebrew for "lazy"
    And I expand the "Results" list under "Find more apps and tools"
    And I click the "Install" button in the "lazygit" row
    Then a dialog titled "Install lazygit?" is shown
    When I choose "Cancel" in the dialog
    Then no dialog is shown
    And the "Install" button in the "lazygit" row is sensitive
    When I click the "Install" button in the "lazygit" row
    Then a dialog titled "Install lazygit?" is shown
    When I choose "Cancel" in the dialog
    Then no dialog is shown
    And the application log previews no "brew install"
    And Homebrew was never asked to "install"

  @config.apps-bundles @stub.apps-brew @stub.apps-flatpak
  Scenario Outline: A confirmed install of a <kind> is previewed with its namespace
    Given ChairLift is running
    When I open the "Apps" page
    And I search Homebrew for "lazy"
    And I expand the "Results" list under "Find more apps and tools"
    And I click the "Install" button in the "<name>" row
    Then a dialog titled "Install <name>?" is shown
    And the dialog says "Downloads and installs this <kind> from Homebrew."
    When I choose "Install" in the dialog
    Then the application log previews "<command>" exactly once
    And a toast on the Apps page says "[DRY-RUN] Preview: <name> would be installed — no changes made"
    And the "Install" button in the "<name>" row is sensitive
    And Homebrew was never asked to "install"

    Examples:
      | kind              | name     | command                      |
      | command line tool | lazygit  | brew install lazygit         |
      | application       | lazyterm | brew install --cask lazyterm |

  @config.apps-bundles @stub.apps-brew @stub.apps-flatpak
  Scenario: Cancelling an uninstall changes nothing and leaves the row usable
    Given ChairLift is running
    When I open the "Apps" page
    And I expand the "Command line tools" list under "Packages from Homebrew"
    And I click the "Uninstall" button in the "jq" row
    Then a dialog titled "Uninstall jq?" is shown
    When I choose "Cancel" in the dialog
    Then no dialog is shown
    And the "Uninstall" button in the "jq" row is sensitive
    And the "Pin" button in the "jq" row is sensitive
    And the application log previews no "brew uninstall"
    And Homebrew was never asked to "uninstall"

  @config.apps-bundles @stub.apps-brew @stub.apps-flatpak
  Scenario Outline: A confirmed uninstall of <name> is previewed and the known inventory is kept
    Given ChairLift is running
    When I open the "Apps" page
    And I expand the "<list>" list under "Packages from Homebrew"
    And I click the "Uninstall" button in the "<name>" row
    Then a dialog titled "Uninstall <name>?" is shown
    When I choose "Uninstall" in the dialog
    Then the application log previews "<command>" exactly once
    And a toast on the Apps page says "[DRY-RUN] Preview: <name> would be uninstalled — no changes made"
    And the "Uninstall" button in the "<name>" row is sensitive
    And the "<list>" list under "Packages from Homebrew" says "<count>"
    And Homebrew was never asked to "uninstall"

    Examples:
      | list               | name               | count       | command                                  |
      | Command line tools | jq                 | 2 installed | brew uninstall jq                        |
      | Applications       | visual-studio-code | 1 installed | brew uninstall --cask visual-studio-code |

  @config.apps-bundles @stub.apps-brew @stub.apps-flatpak
  Scenario Outline: <action> on <name> is confirmed, previewed, and restores both row controls
    Given ChairLift is running
    When I open the "Apps" page
    And I expand the "Command line tools" list under "Packages from Homebrew"
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

  @config.apps-bundles @stub.apps-brew-unreadable @stub.apps-flatpak
  Scenario: An unreadable Homebrew inventory says so without hiding Flatpak apps
    Given ChairLift is running
    When I open the "Apps" page
    Then the "Command line tools" list under "Packages from Homebrew" says "Could not read the list"
    And the "Applications" list under "Packages from Homebrew" says "Could not read the list"
    And the "Applications" list under "Installed applications" says "2 installed"

  @config.apps-bundles @stub.apps-brew @stub.apps-flatpak @env.CHAIRLIFT_CAPABILITIES=image-descriptor,flatpak
  Scenario: A host without Homebrew shows no Homebrew groups
    Given ChairLift is running
    When I open the "Apps" page
    Then the Apps page shows the "Installed applications" group
    And the Apps page does not show the "Packages from Homebrew" group
    And the Apps page does not show the "Find more apps and tools" group
    And the Apps page does not show the "App collections" group

  @config.apps-bundles @stub.apps-brew @stub.apps-flatpak @stub.apps-collections
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
    And Homebrew was never asked to "bundle"

  @config.apps-bundles @stub.apps-brew @stub.apps-flatpak
  Scenario: A system with no app collections says none are offered
    Given ChairLift is running
    When I open the "Apps" page
    Then I see "No collections available"
    And I see "This system does not offer any app collections."

  @config.apps-collections-only @stub.apps-brew @stub.apps-flatpak @stub.apps-collections
  Scenario: App collections install without the Homebrew package groups
    Given ChairLift is running
    When I open the "Apps" page
    Then the Apps page does not show the "Packages from Homebrew" group
    And the Apps page does not show the "Find more apps and tools" group
    When I click the "Install" button in the "Team tools" row
    Then the application log contains "/bundles/team-tools.Brewfile"
    And the "Install" button in the "Team tools" row is sensitive
    And the application log does not contain "panic"

  @config.apps-bundles @stub.apps-brew @stub.apps-flatpak
  Scenario: Flatpak apps are listed with the installation they belong to
    Given ChairLift is running
    When I open the "Apps" page
    Then the "Applications" list under "Installed applications" says "2 installed"
    When I expand the "Applications" list under "Installed applications"
    Then the "Applications" list under "Installed applications" shows exactly
      | title       |
      | Firefox     |
      | Text Editor |
    And the "Firefox" row says "org.mozilla.firefox (140.0) • Installed for you"
    And the "Text Editor" row says "org.gnome.TextEditor (48.0) • Installed for everyone"

  @config.apps-bundles @stub.apps-brew @stub.apps-flatpak
  Scenario Outline: A confirmed removal of a Flatpak app installed <scope> is previewed in that installation
    Given ChairLift is running
    When I open the "Apps" page
    And I expand the "Applications" list under "Installed applications"
    And I click the remove button in the "<name>" row
    Then a dialog titled "Uninstall <name>?" is shown
    When I choose "Uninstall" in the dialog
    Then the application log previews "<command>" exactly once
    And a toast on the Apps page says "[DRY-RUN] Preview: <name> would be uninstalled — no changes made"
    And the "Applications" list under "Installed applications" says "2 installed"
    And the remove button in the "<name>" row is sensitive
    And Flatpak was never asked to "uninstall"

    Examples:
      | scope        | name        | command                                        |
      | for the user | Firefox     | flatpak uninstall -y --user org.mozilla.firefox |
      | for everyone | Text Editor | flatpak uninstall -y --system org.gnome.TextEditor |

  @config.apps-bundles @stub.apps-brew @stub.apps-flatpak @stub.apps-gtk-launch
  Scenario: Browse all apps opens the configured software catalog
    Given ChairLift is running
    When I open the "Apps" page
    And I open the "Browse all apps" row with the keyboard
    Then gtk-launch was asked to open "io.github.kolunmi.Bazaar"

  @config.apps-bundles @stub.apps-brew @stub.apps-flatpak @stub.apps-gtk-launch-missing
  Scenario: A software catalog that cannot be opened says so
    Given ChairLift is running
    When I open the "Apps" page
    And I open the "Browse all apps" row with the keyboard
    Then gtk-launch was asked to open "io.github.kolunmi.Bazaar"
    And a toast on the Apps page says "Could not open that application"

  @config.apps-bundles @stub.apps-brew @stub.apps-flatpak
  Scenario: Exporting the package list is previewed and writes nothing
    Given ChairLift is running
    When I open the "Apps" page
    And I click the "Export" button in the "Export package list" row
    Then the application log contains "[DRY-RUN] Would execute: brew bundle dump --file="
    And the application log contains "/home/Brewfile --force"
    And the home directory has no "Brewfile"
    And Homebrew was never asked to "bundle"

  @config.apps-bundles @stub.apps-brew @stub.apps-flatpak
  Scenario: The Homebrew search field has an accessible name
    Given ChairLift is running
    When I open the "Apps" page
    Then the Homebrew search field has an accessible name

  @config.apps-bundles @stub.apps-brew @stub.apps-flatpak @stub.apps-collections
  Scenario: Every control on the Apps page, including every list row's, is named and operable
    Given ChairLift is running
    When I open the "Apps" page
    And I expand the "Applications" list under "Installed applications"
    And I expand the "Command line tools" list under "Packages from Homebrew"
    And I expand the "Applications" list under "Packages from Homebrew"
    And I search Homebrew for "lazy"
    And I expand the "Results" list under "Find more apps and tools"
    Then the "Pin" button in the "jq" row is sensitive
    And the "Install" button in the "lazyterm" row is sensitive
    And the remove button in the "Firefox" row has an accessible name
    And the remove button in the "Text Editor" row has an accessible name
    And every visible action control has an accessible name and an action

  @config.apps-bundles @stub.apps-brew @stub.apps-flatpak
  Scenario: Removing a Flatpak app asks for confirmation first, like a Homebrew package
    Given ChairLift is running
    When I open the "Apps" page
    And I expand the "Applications" list under "Installed applications"
    And I click the remove button in the "Firefox" row
    Then a dialog titled "Uninstall Firefox?" is shown
    When I choose "Cancel" in the dialog
    Then no dialog is shown
    And the remove button in the "Firefox" row is sensitive
    And the application log previews no "flatpak uninstall"
    When I click the remove button in the "Firefox" row
    Then a dialog titled "Uninstall Firefox?" is shown
    When I choose "Cancel" in the dialog
    Then no dialog is shown
    And the application log previews no "flatpak uninstall"
    And Flatpak was never asked to "uninstall"
