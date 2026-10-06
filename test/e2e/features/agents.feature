@agents @stub.agents.host
Feature: Agents page
  Agent Mode runs a loopback model server as a systemd user unit. Its status
  comes from the executable, unit and node endpoint, never the unit alone.
  Model controls stay visible when unavailable. Troubleshooting's Goose
  row sits below it: it installs Goose and its read-only tools with Set Up, then
  launches Goose on the Agent Mode model in a profile ChairLift writes at
  launch, so nothing reads or writes a Goose configuration under HOME.
  Under --dry-run every action is a preview and leaves software, files and
  model configuration unchanged.

  @env.CHAIRLIFT_CAPABILITIES=image-descriptor,flatpak,podman,bootc-stage
  Scenario: Agents is hidden on a host without Homebrew
    Given ChairLift is running
    Then the sidebar has no "Agents" row

  @config.agents-disabled
  Scenario: Agents is hidden when both of its groups are disabled in configuration
    Given ChairLift is running
    Then the sidebar has no "Agents" row

  Scenario: A host that never set up Agent Mode offers to install it
    Given ChairLift is running
    When I open the "Agents" page
    Then the switch in the "Agent Mode" row is off
    And the "Agent Mode" row says "Turn on to install the model server"
    And the "Active Model" row says "Turn on Agent Mode to choose a model."
    And the model chooser is insensitive
    And I do not see "Models and Chat"
    And the "Goose" row says "Goose Desktop or linux-mcp-server is not installed."
    And the "Set Up" button in the "Goose" row is sensitive
    And the "Local connection" row says "http://127.0.0.1:17434/v1"

  @stub.agents.llmman @stub.agents.unit @stub.agents.node @stub.agents.alias
  Scenario: Only a responding daemon makes model controls available
    Given ChairLift is running
    When I open the "Agents" page
    Then the Agent Mode switch settles on and sensitive
    And the "Agent Mode" row says "Ready."
    And the llmman node endpoint was probed
    And the "Active Model" row says "unsloth/Qwen3-8B-GGUF:Q4_K_M"
    And the model chooser is sensitive
    And the "Models and Chat" row says "Pull, remove, and try models in llmman's own web interface"
    And the "Goose" row says "Goose Desktop or linux-mcp-server is not installed."
    And the "Set Up" button in the "Goose" row is sensitive

  @stub.agents.llmman @stub.agents.unit @stub.agents.node @stub.agents.alias @stub.help-xdg-open
  Scenario: Open llmman hands its loopback web interface to xdg-open
    Given ChairLift is running
    When I open the "Agents" page
    # The row is the button's label (AdwActionRow activatable widget), so the
    # "Open llmman" button announces itself as "Models and Chat".
    And I click the "Models and Chat" button in the "Models and Chat" row
    Then xdg-open was asked to open "http://127.0.0.1:17434/"
    And the action journal is empty

  @stub.agents.llmman @stub.agents.unit @stub.agents.node
  Scenario: A ready server with no alias does not invent an active model
    Given ChairLift is running
    When I open the "Agents" page
    Then the "Agent Mode" row says "Ready."
    And the "Active Model" row says "No model selected"
    And the model chooser is sensitive

  @stub.agents.llmman @stub.agents.unit
  Scenario: An installed unit whose daemon does not answer stays visibly unavailable
    Given ChairLift is running
    When I open the "Agents" page
    Then the Agent Mode switch settles on and sensitive
    And the "Agent Mode" row says "The model server is not answering."
    And the "Active Model" row says "Available when the model server is ready."
    And the "Recommended Presets" row says "Available when the model server is ready."
    And the model chooser is insensitive
    And I do not see "Models and Chat"

  Scenario: Turning Agent Mode on in a dry run installs and writes nothing
    Given ChairLift is running
    When I open the "Agents" page
    And I toggle the switch in the "Agent Mode" row
    Then the application log shows Agent Mode would write its unit and fragment
    And the Agent Mode switch settles off and sensitive
    And the "Agent Mode" row says "Turn on to install the model server"
    And I see "[DRY-RUN] Preview: Agent Mode would be turned on — no changes made"
    And the Agent Mode unit does not exist
    And the Agent Mode environment fragment does not exist
    And brew was never asked to "bundle"
    And the systemctl tool was never asked to mutate
    And the action journal is empty

  @stub.agents.llmman
  Scenario: Turning Agent Mode back on in a dry run keeps it off and keeps llmman idle
    Given ChairLift is running
    When I open the "Agents" page
    Then the "Agent Mode" row says "Off. Downloaded software and models were kept."
    When I toggle the switch in the "Agent Mode" row
    Then the application log shows Agent Mode would write its unit and fragment
    And the Agent Mode switch settles off and sensitive
    And the "Agent Mode" row says "Off. Downloaded software and models were kept."
    And the Agent Mode unit does not exist
    And llmman was never asked to "serve"
    And the systemctl tool was never asked to mutate

  @stub.agents.llmman @stub.agents.unit @stub.agents.node @stub.agents.alias
  Scenario: Turning Agent Mode off in a dry run keeps the unit and the ready state
    Given ChairLift is running
    When I open the "Agents" page
    And I note the Agents files on disk
    Then the "Agent Mode" row says "Ready."
    When I toggle the switch in the "Agent Mode" row
    Then the application log shows Agent Mode would remove its unit and fragment
    And the Agent Mode switch settles on and sensitive
    And the "Agent Mode" row says "Ready."
    And I see "[DRY-RUN] Preview: Agent Mode would be turned off — no changes made"
    And the Agents files on disk are unchanged
    And the systemctl tool was never asked to mutate
    And the action journal is empty

  @stub.agents.llmman @stub.agents.unit @stub.agents.node @stub.agents.alias
  Scenario: Dismissing the preset chooser changes nothing and can be reopened
    Given ChairLift is running
    When I open the "Agents" page
    And I click the "Choose…" button in the "Recommended Presets" row
    Then a dialog titled "Choose a Model" is shown
    When I dismiss the model chooser with Escape
    Then no dialog is shown
    And the application log does not contain "would configure alias"
    When I click the "Choose…" button in the "Recommended Presets" row
    Then a dialog titled "Choose a Model" is shown
    When I choose "Cancel" in the dialog
    Then no dialog is shown

  @stub.agents.llmman @stub.agents.unit @stub.agents.node @stub.agents.alias
  Scenario: A preset in a dry run resolves a fitting model offline and pulls nothing
    Given ChairLift is running
    When I open the "Agents" page
    And I click the "Choose…" button in the "Recommended Presets" row
    Then a dialog titled "Choose a Model" is shown
    And the dialog says "Qwen (Recommended)"
    And the dialog says "Mistral / Ministral"
    And the dialog says "DeepSeek"
    And the dialog says "GPT-OSS"
    When I choose "Gemma" in the dialog
    Then the application log contains "[DRY-RUN] would configure alias bluefin-active to unsloth/gemma-3"
    And I see "[DRY-RUN] Would switch to unsloth/gemma-3"
    And llmman was never asked to "pull"
    And llmman was never asked to "config set"
    And the Agent Mode switch settles on and sensitive
    And the model chooser is sensitive

  @stub.agents.llmman @stub.agents.unit @stub.agents.node @stub.agents.alias
  Scenario: A dry-run preset leaves the Active Model row showing the model actually configured
    Given ChairLift is running
    When I open the "Agents" page
    Then the "Active Model" row says "unsloth/Qwen3-8B-GGUF:Q4_K_M"
    When I click the "Choose…" button in the "Recommended Presets" row
    And I choose "Gemma" in the dialog
    Then the application log contains "[DRY-RUN] would configure alias bluefin-active to unsloth/gemma-3"
    And the "Active Model" row says "unsloth/Qwen3-8B-GGUF:Q4_K_M"

  # ------------------------------------------------------------ troubleshooting

  Scenario: Troubleshooting sits between Agent Mode and Contribute
    Given ChairLift is running
    When I open the "Agents" page
    Then the Agents page groups are, in order
      | group                    |
      | Local AI                 |
      | Troubleshooting |
      | Contribute               |
    And I see "Goose answers with your Agent Mode model and reads this computer's logs, services, and network with read-only tools. Knowledge searches go online to the Project Bluefin knowledge base."

  @config.agents-no-troubleshooting
  Scenario: Troubleshooting disabled by configuration leaves Agent Mode
    Given ChairLift is running
    When I open the "Agents" page
    Then the Agents page has no Troubleshooting group
    And I see "Contribute to Bluefin"
    And the application log does not contain "CONFIGURATION ERROR"

  @config.help-no-troubleshooting
  Scenario: The legacy help_page key still turns Troubleshooting off
    Given ChairLift is running
    Then the application log does not contain "CONFIGURATION ERROR"
    When I open the "Agents" page
    Then the Agents page has no Troubleshooting group
    When I press "F1"
    Then I do not see "Troubleshooting"

  Scenario: Set Up on a fresh host previews every step and changes nothing
    Given ChairLift is running
    When I open the "Agents" page
    Then the "Goose" row says "Goose Desktop or linux-mcp-server is not installed."
    When I click the "Set Up" button in the "Goose" row
    Then I see "[DRY-RUN] Preview: Goose would be set up — no changes made"
    And the Goose setup previewed exactly
      | command                                      |
      | brew tap ublue-os/tap                        |
      | brew install ublue-os/tap/linux-mcp-server   |
      | brew install cpio                            |
      | brew install --cask ublue-os/tap/goose-linux |
    And brew was never asked to "tap"
    And brew was never asked to "install"
    And the "Goose" row says "Goose Desktop or linux-mcp-server is not installed."
    And the "Set Up" button in the "Goose" row is sensitive
    And no Goose session was previewed
    And the Goose profile was not written
    And the home directory has no ".config/goose"
    And the action journal is empty

  @stub.agents.goose-server
  Scenario: Set Up with the tools installed only adds the Goose app
    Given ChairLift is running
    When I open the "Agents" page
    And I click the "Set Up" button in the "Goose" row
    Then I see "[DRY-RUN] Preview: Goose would be set up — no changes made"
    And the Goose setup previewed exactly
      | command                                      |
      | brew tap ublue-os/tap                        |
      | brew install cpio                            |
      | brew install --cask ublue-os/tap/goose-linux |

  @stub.agents.goose-desktop
  Scenario: Set Up with the Goose app installed only adds the tools
    Given ChairLift is running
    When I open the "Agents" page
    And I click the "Set Up" button in the "Goose" row
    Then I see "[DRY-RUN] Preview: Goose would be set up — no changes made"
    And the Goose setup previewed exactly
      | command                                    |
      | brew tap ublue-os/tap                      |
      | brew install ublue-os/tap/linux-mcp-server |

  @stub.agents.goose
  Scenario: Installed Goose with Agent Mode off points at Agent Mode
    Given ChairLift is running
    When I open the "Agents" page
    Then the "Goose" row says "Turn on Agent Mode to launch Goose."
    And the "Launch" button in the "Goose" row is insensitive

  @stub.agents.goose @stub.agents.llmman @stub.agents.unit @stub.agents.node
  Scenario: Agent Mode running without a model asks for one
    Given ChairLift is running
    When I open the "Agents" page
    Then the "Goose" row says "Choose a model to launch Goose."
    And the "Launch" button in the "Goose" row is insensitive

  @stub.agents.llmman @stub.agents.unit @stub.agents.node @stub.agents.alias @stub.agents.goose
  Scenario: A ready Agent Mode launches Goose in ChairLift's own profile, previewed
    Given ChairLift is running
    When I open the "Agents" page
    Then the Agent Mode switch settles on and sensitive
    And the "Active Model" row says "unsloth/Qwen3-8B-GGUF:Q4_K_M"
    And the "Goose" row says "Ready to launch with unsloth/Qwen3-8B-GGUF:Q4_K_M."
    And the "Launch" button in the "Goose" row is sensitive
    When I click the "Launch" button in the "Goose" row
    Then I see "[DRY-RUN] Would launch Goose Desktop"
    And the application log contains "[DRY-RUN] would launch Goose Desktop with model unsloth/Qwen3-8B-GGUF:Q4_K_M via llmman"
    And the Goose session previewed its profile and launch
    And llmman was never asked to "launch"
    And the Goose profile was not written
    And the home directory has no ".config/goose"
    And the Goose setup previewed exactly
      | command |
    And the action journal is empty

  @stub.agents.llmman @stub.agents.unit @stub.agents.node
  Scenario: Every Agents control has an accessible name
    Given ChairLift is running
    When I open the "Agents" page
    Then every visible action control has an accessible name and an action
    And every visible toggle reports its state

  @stub.agents.contribute.ready
  Scenario: Contribute to Bluefin is ready when all preflight checks pass
    Given ChairLift is running
    When I open the "Agents" page
    Then the "Contribute to Bluefin" row says "Run the Hive contributor appliance in a terminal."
    And the "Contribute" button in the "Contribute to Bluefin" row is sensitive
    When I click the "Contribute" button in the "Contribute to Bluefin" row
    Then the application log contains "[DRY-RUN] would launch xdg-terminal-exec ujust contribute"
    And I see "[DRY-RUN] Preview: would launch Contribute to Bluefin in a terminal"

  @stub.agents.contribute.noreg
  Scenario: Contribute to Bluefin explains missing Hive registration
    Given ChairLift is running
    When I open the "Agents" page
    Then the "Contribute to Bluefin" row says "Register this machine first — see https://github.com/projectbluefin/contribute#configuration"
    And the "Contribute" button in the "Contribute to Bluefin" row is insensitive

  @stub.agents.devmenu
  Scenario: Show Ask Bluefin in menu dry-run toggle previews and restores without writing dconf
    Given ChairLift is running
    When I open the "Agents" page
    Then the switch in the "Show Ask Bluefin in menu" row is on
    When I toggle the switch in the "Show Ask Bluefin in menu" row
    Then the application log contains "[DRY-RUN] would set Custom Command Menu command11 visible=false"
    And the switch in the "Show Ask Bluefin in menu" row is on
    And dconf was never asked to "write"
    And dconf was never asked to "reset"

  @stub.agents.goose
  Scenario: A second invocation with ask-bluefin lands on Agents when prerequisite is missing
    Given ChairLift is running
    When I open the "Apps" page
    And I run a second invocation with "--dry-run --ask-bluefin"
    Then the "Agents" page is shown
    And I see "Agent Mode is not running."

  @stub.agents.llmman @stub.agents.unit @stub.agents.node @stub.agents.alias @stub.agents.goose
  Scenario: A second invocation with ask-bluefin launches Goose when Agent Mode is ready
    Given ChairLift is running
    When I open the "Apps" page
    And I run a second invocation with "--dry-run --ask-bluefin"
    Then the application log contains "[DRY-RUN] would launch Goose Desktop with model unsloth/Qwen3-8B-GGUF:Q4_K_M via llmman"
    And the "Apps" page is shown
    And llmman was never asked to "launch"

  Scenario: A second invocation with ask-bluefin names missing Goose packages
    Given ChairLift is running
    When I open the "Apps" page
    And I run a second invocation with "--dry-run --ask-bluefin"
    Then the "Agents" page is shown
    And I see "Goose Desktop or linux-mcp-server is not installed."
    And no Goose session was previewed

  @stub.agents.devmenu.wrapper
  Scenario: The distro's chairlift-wrapper Ask Bluefin entry is the one the switch shows and hides
    Given ChairLift is running
    When I open the "Agents" page
    Then the switch in the "Show Ask Bluefin in menu" row is on
    When I toggle the switch in the "Show Ask Bluefin in menu" row
    Then the application log contains "[DRY-RUN] would set Custom Command Menu command11 visible=false"
    And the switch in the "Show Ask Bluefin in menu" row is on
    And dconf was never asked to "write"
    And dconf was never asked to "reset"
