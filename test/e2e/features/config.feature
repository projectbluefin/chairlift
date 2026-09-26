@config
Feature: Fail-closed configuration and configuration-driven visibility
  The first configuration file that exists is authoritative. A broken one
  disables every configurable group and says so in a persistent toast until
  it is fixed and the application restarted (internal/config.Load). A valid
  one decides which groups render, and internal/navigation drops a page whose
  groups are all off and compacts Alt+number over what remains. The host
  capability floor can only subtract from what configuration enables.

  Scenario Outline: A broken configuration fails closed with an actionable toast
    Given ChairLift is running
    Then the configuration error toast reports a "<kind>" error: "<detail>"
    And the application log contains "CONFIGURATION ERROR: config <kind> error"
    And the sidebar lists exactly "Help"
    And the "Help" page is shown
    And the action journal is empty

    @config.config-invalid-yaml
    Examples: YAML that does not parse
      | kind       | detail                                 |
      | parse/type | yaml: line 4: did not find expected ',' |

    @config.config-unknown-group
    Examples: a group name the schema does not know
      | kind   | detail                                |
      | schema | unknown name "brew_groop" (line 4)    |

    @config.config-untrusted-sudo
    Examples: a sudo action enabled from an untrusted path
      | kind   | detail                                                                     |
      | schema | group "maintenance_cleanup_group" enables sudo action "Clean Up Boot Old Entries" |

    @config.config-legacy-typo
    Examples: a typo inside the legacy system_page input
      | kind   | detail                               |
      | schema | unknown name "system_info_grup"      |

  @config.config-unknown-group
  Scenario: Fail-closed Help keeps only what needs no configuration
    Given ChairLift is running
    Then I see "System diagnostics"
    And I do not see "Help & Resources"
    And I do not see "Enhanced Troubleshooting"
    And I do not see "Why is something missing?"

  @config.config-unknown-group
  Scenario: Only Help keeps a keyboard shortcut when configuration is broken
    Given ChairLift is running
    When I press "<Control><Shift>slash"
    Then a window titled "Keyboard Shortcuts" is shown
    And the shortcuts window lists exactly these page shortcuts
      | page | shortcut |
      | Help | Alt+1    |
    When I close the "Keyboard Shortcuts" window
    And I press "<Alt>1"
    Then the "Help" page is shown

  @config.config-unknown-group
  Scenario: The configuration error toast persists until dismissed, and dismissing restores nothing
    Given ChairLift is running
    Then the configuration error toast is still shown after 5 seconds
    When I press "<Control><Shift>slash"
    Then a window titled "Keyboard Shortcuts" is shown
    When I close the "Keyboard Shortcuts" window
    Then the configuration error toast reports a "schema" error: "unknown name "brew_groop""
    When I dismiss the configuration error toast
    Then no configuration error toast is shown
    And the sidebar lists exactly "Help"
    And I do not see "Help & Resources"

  @config.config-invalid-yaml
  Scenario: A YAML parse error names its cause once
    Given ChairLift is running
    Then the configuration error toast states "did not find expected ','" once

  @config.config-no-agents-maintenance
  Scenario: Disabling every group of a page removes its row and compacts Alt+number
    Given ChairLift is running
    Then the application log does not contain "CONFIGURATION ERROR"
    And no configuration error toast is shown
    And the sidebar lists exactly "Updates, Apps, Features, Livery, Help"
    When I press "<Alt>4"
    Then the "Livery" page is shown
    When I press "<Alt>5"
    Then the "Help" page is shown
    When I press "<Alt>3"
    Then the "Features" page is shown
    When I press "<Control><Shift>slash"
    Then a window titled "Keyboard Shortcuts" is shown
    And the shortcuts window lists exactly these page shortcuts
      | page     | shortcut |
      | Updates  | Alt+1    |
      | Apps     | Alt+2    |
      | Features | Alt+3    |
      | Livery   | Alt+4    |
      | Help     | Alt+5    |

  @config.config-one-group-off
  Scenario: Disabling one Maintenance group hides only its rows
    Given ChairLift is running
    When I select "Maintenance" in the sidebar
    Then I see "Recovery"
    And I do not see "Free up space"
    And the "Clean up" button is not shown

  @config.config-one-group-off
  Scenario: Disabling one update source is reported as the administrator's choice
    Given ChairLift is running
    Then the "Applications" row says "Disabled by administrator"
    And the "Developer tools" source row does not say "Disabled by administrator"

  @env.CHAIRLIFT_CAPABILITIES=image-descriptor,podman,bootc-stage
  Scenario: The capability floor hides pages and groups that configuration enables
    Given ChairLift is running
    Then the sidebar lists exactly "Updates, Apps, Features, Livery, Maintenance, Help"
    When I select "Apps" in the sidebar
    Then I see "Browse all apps"
    And I do not see "App collections"
    When I select "Help" in the sidebar
    Then I do not see "Enhanced Troubleshooting"
    When I expand the "Why is something missing?" row with the keyboard
    Then the "Agent Mode" row says "Needs Homebrew"
    And the "App updates" row says "Needs Flatpak"
    And the "Recovery" row says "Needs Flatpak or Distrobox"

  @env.CHAIRLIFT_CAPABILITIES=image-descriptor,podman,bootc-stage
  Scenario: An update source the host cannot back is not blamed on the administrator
    Given ChairLift is running
    Then the "Applications" row says "Not available on this system"
    And the "Developer tools" row says "Not available on this system"

  @config.config-no-agents-maintenance @env.CHAIRLIFT_CAPABILITIES=image-descriptor,podman,bootc-stage
  Scenario: Help explains missing tools, never groups the administrator disabled
    Given ChairLift is running
    When I select "Help" in the sidebar
    And I expand the "Why is something missing?" row with the keyboard
    Then the "Packages from Homebrew" row says "Needs Homebrew"
    And the feature availability list omits "Agent Mode"
    And the feature availability list omits "Recovery"

  @env.CHAIRLIFT_CAPABILITIES=flatpak,brew,podman,bootc-stage
  Scenario Outline: The legacy system_page channel group migrates onto Updates
    Given ChairLift is running
    Then the application log does not contain "CONFIGURATION ERROR"
    When I select "Help" in the sidebar
    And I expand the "Why is something missing?" row with the keyboard
    Then the "Developer mode" row says "Needs /usr/share/ublue-os/image-info.json"
    And the feature availability list <verdict> "Release channel"

    @config.everything
    Examples: no legacy input
      | verdict  |
      | includes |

    @config.config-legacy-system-page
    Examples: legacy input disables it
      | verdict |
      | omits   |

    @config.config-legacy-current-wins
    Examples: the current updates_page field overrides the legacy one
      | verdict  |
      | includes |

  @config.config-legacy-enables-channel
  Scenario: The legacy system_page release-channel group surfaces on Updates
    Given ChairLift is running
    Then the application log does not contain "CONFIGURATION ERROR"
    And I see "Get updates early"
