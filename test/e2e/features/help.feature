@help @stub.help-brew
Feature: Help destination
  Help is the one page every configuration keeps. It leads with Enhanced
  Troubleshooting, whose row reports what Goose's own configuration says
  rather than what a setup script's exit code implies, then lists the
  support links configuration names, in the order internal/views/pageview
  fixes. Every external program the page reaches is stubbed: links and the
  Goose launch are recorded instead of opened, and Homebrew records any call
  that escapes the dry run.

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

  @config.help-links-markup @known_issue.354
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
    And I do not see "Failed to open URL"
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
    And I see "Failed to open URL: https://github.com/projectbluefin/dakota/issues"
    And the Help page is still responsive

  @config.help-no-resources
  Scenario: Disabling the resources group removes the links but keeps Help
    Given ChairLift is running
    When I select "Help" in the sidebar
    Then the Help page has no resource links
    And I see "Enhanced Troubleshooting"
    And I see "System diagnostics"
    When I open the "Updates" page
    And I press "F1"
    Then the "Help" page is shown

  @stub.help-goose-all @stub.help-goose-wired-gemini @stub.help-gtk-launch
  Scenario: A wired Goose configuration offers a session and names its provider
    Given ChairLift is running
    When I press "F1"
    Then the Enhanced Troubleshooting status is "Ready — questions go to Google Gemini"
    And the "Set Up" button is not shown
    When I click the "Start Session" button in the "Enhanced Troubleshooting" row
    Then Goose was launched as "Goose"
    And the troubleshooting setup never ran "brew tap"
    And the troubleshooting setup never ran "brew install"
    And the action journal is empty

  @stub.help-goose-all
  Scenario Outline: The row names where questions go from GOOSE_PROVIDER
    Given ChairLift is running
    When I press "F1"
    Then the Enhanced Troubleshooting status is "Ready — <note>"

    @stub.help-goose-wired-ollama
    Examples: Local provider
      | note                           |
      | questions stay on this machine |

    @stub.help-goose-wired-anthropic
    Examples: Other provider
      | note                      |
      | questions go to anthropic |

    @stub.help-goose-wired-none
    Examples: No provider
      | note                         |
      | no AI service configured yet |

  @stub.help-goose-all
  Scenario Outline: A configuration that does not wire linux-tools is not ready
    Given ChairLift is running
    When I press "F1"
    Then the Enhanced Troubleshooting status is "Installed, but not connected to this system yet"
    And the "Set Up" button in the "Enhanced Troubleshooting" row is sensitive
    And the "Start Session" button is not shown

    @stub.help-goose-unwired-other-extension
    Examples: Only another extension
      | shape           |
      | other-extension |

    @stub.help-goose-unwired-disabled
    Examples: linux-tools disabled
      | shape    |
      | disabled |

    @stub.help-goose-unwired-no-command
    Examples: linux-tools without a command
      | shape      |
      | no-command |

    @stub.help-goose-unwired-mention-only
    Examples: linux-tools only in a comment
      | shape        |
      | mention-only |

    @stub.help-goose-unwired-malformed
    Examples: Malformed YAML
      | shape     |
      | malformed |

  @stub.help-goose-cli @stub.help-goose-wired-gemini
  Scenario: A wired host without the desktop app still offers Set Up
    Given ChairLift is running
    When I press "F1"
    Then the Enhanced Troubleshooting status is "Ready — questions go to Google Gemini"
    And the "Set Up" button in the "Enhanced Troubleshooting" row is sensitive
    And the "Start Session" button is not shown

  @stub.help-no-goose
  Scenario: Set Up on a fresh host previews every step and changes nothing
    Given ChairLift is running
    When I press "F1"
    Then the Enhanced Troubleshooting status is "Ask an AI assistant about your logs, services, and network"
    When I click the "Set Up" button in the "Enhanced Troubleshooting" row
    Then I see "[DRY-RUN] Preview: Enhanced Troubleshooting would be set up — no changes made"
    And the troubleshooting setup previewed exactly
      | command                                      |
      | brew tap ublue-os/tap                        |
      | brew install ublue-os/tap/linux-mcp-server   |
      | brew install --cask ublue-os/tap/goose-linux |
      | goose-mcp-setup                              |
    And the troubleshooting setup never ran "brew tap"
    And the troubleshooting setup never ran "brew install"
    And Goose's configuration file was not written
    And the "Set Up" button in the "Enhanced Troubleshooting" row is sensitive
    And the action journal is empty

  @stub.help-no-goose @known_issue.354
  Scenario Outline: A dry-run Set Up leaves the row describing the host as it was
    Given ChairLift is running
    When I press "F1"
    Then the Enhanced Troubleshooting status is "<before>"
    When I click the "Set Up" button in the "Enhanced Troubleshooting" row
    Then I see "[DRY-RUN] Preview: Enhanced Troubleshooting would be set up — no changes made"
    And the Enhanced Troubleshooting status is "<before>"

    Examples: Fresh host
      | before                                                     |
      | Ask an AI assistant about your logs, services, and network |

    @stub.help-goose-cli @stub.help-goose-unwired-other-extension
    Examples: Installed but not connected
      | before                                          |
      | Installed, but not connected to this system yet |

  @stub.help-goose-cli @stub.help-goose-unwired-other-extension
  Scenario: Set Up resumes a half-done install instead of repeating it
    Given ChairLift is running
    When I press "F1"
    Then the Enhanced Troubleshooting status is "Installed, but not connected to this system yet"
    When I click the "Set Up" button in the "Enhanced Troubleshooting" row
    Then I see "[DRY-RUN] Preview: Enhanced Troubleshooting would be set up — no changes made"
    And the troubleshooting setup previewed exactly
      | command                                      |
      | brew tap ublue-os/tap                        |
      | brew install --cask ublue-os/tap/goose-linux |
      | goose-mcp-setup                              |
    And the troubleshooting setup never ran "brew tap"
    And the troubleshooting setup never ran "brew install"
    And the "Set Up" button in the "Enhanced Troubleshooting" row is sensitive

  @env.CHAIRLIFT_CAPABILITIES=image-descriptor,flatpak,podman,bootc-stage
  Scenario: Without Homebrew the troubleshooting group is explained, not shown
    Given ChairLift is running
    When I press "F1"
    Then the Enhanced Troubleshooting group is not shown
    When I ask the Help page why something is missing
    Then the Help page explains "Enhanced Troubleshooting" with "Needs Homebrew"

  @config.help-no-troubleshooting
  @env.CHAIRLIFT_CAPABILITIES=image-descriptor,flatpak,podman,bootc-stage
  Scenario: Troubleshooting disabled by configuration is not called missing
    Given ChairLift is running
    When I press "F1"
    Then the Enhanced Troubleshooting group is not shown
    When I ask the Help page why something is missing
    Then the Help page explains "Agent Mode" with "Needs Homebrew"
    And the Help page does not call "Enhanced Troubleshooting" missing

  Scenario: Copying diagnostics confirms and re-enables the button
    Given ChairLift is running
    When I press "F1"
    And I click the "Copy" button in the "System diagnostics" row
    Then I see "System diagnostics copied to clipboard"
    And the "Copy" button in the "System diagnostics" row is sensitive
    And the action journal is empty

