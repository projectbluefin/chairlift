@agents @stub.agents.host
Feature: Agents page
  Agent Mode is one unprivileged switch that runs llmman as a systemd user
  unit, and "Use another machine" configures llmman peers (ADR-0015). Its
  state is read from the llmman executable, ChairLift's unit file, and a
  loopback /llmman/node probe; fixtures/stubs_agents.py controls each of
  them. The application runs with --dry-run, so every action here must be
  a preview: nothing written under HOME, and llmman, systemctl and
  `brew bundle` never run.

  # ------------------------------------------------------------ visibility

  @env.CHAIRLIFT_CAPABILITIES=image-descriptor,flatpak,podman,bootc-stage
  Scenario: Agents is hidden on a host without Homebrew
    Given ChairLift is running
    Then the sidebar has no "Agents" row

  @config.agents-disabled
  Scenario: Agents is hidden when its group is disabled in configuration
    Given ChairLift is running
    Then the sidebar has no "Agents" row

  # ------------------------------------------------------------ initial state

  Scenario: A host that never set up Agent Mode offers to install it
    Given ChairLift is running
    When I open the "Agents" page
    Then the switch in the "Run AI models on this computer" row is off
    And the "Run AI models on this computer" row says "Turning this on installs llmman from Homebrew"
    And I see "No peers configured"

  @known_issue.355
  Scenario: Model preset controls stay hidden until Agent Mode is ready
    Given ChairLift is running
    When I open the "Agents" page
    Then the "Run AI models on this computer" row says "Turning this on installs llmman from Homebrew"
    And I do not see "Recommended Presets"
    And I do not see "Active Model"

  @stub.agents.llmman @stub.agents.unit @stub.agents.node @stub.agents.alias
  Scenario: An installed unit whose daemon answers reads as ready
    Given ChairLift is running
    When I open the "Agents" page
    Then the switch in the "Run AI models on this computer" row is on
    And the "Run AI models on this computer" row says "Ready."
    And the llmman node endpoint was probed
    And the "Active Model" row says "unsloth/Qwen3-8B-GGUF:Q4_K_M"
    And the "Switch…" button in the "Recommended Presets" row is sensitive

  @stub.agents.llmman @stub.agents.unit
  Scenario: An installed unit whose daemon does not answer reads as degraded
    Given ChairLift is running
    When I open the "Agents" page
    Then the switch in the "Run AI models on this computer" row is on
    And the "Run AI models on this computer" row says "Turned on, but the model server is not answering."
    And I do not see "Recommended Presets"

  # ------------------------------------------------------------ the switch

  Scenario: Turning Agent Mode on in a dry run installs and writes nothing
    Given ChairLift is running
    When I open the "Agents" page
    And I toggle the switch in the "Run AI models on this computer" row
    Then the application log shows Agent Mode would write its unit and fragment
    And the Agent Mode switch settles off and sensitive
    And the "Run AI models on this computer" row says "Turning this on installs llmman from Homebrew"
    And I see "[DRY-RUN] Preview: Agent Mode would be turned on — no changes made"
    And the Agent Mode unit does not exist
    And the Agent Mode environment fragment does not exist
    And brew was never asked to "bundle"
    And systemctl was never run
    And the action journal is empty

  @stub.agents.llmman
  Scenario: Turning Agent Mode back on in a dry run keeps it off and keeps llmman idle
    Given ChairLift is running
    When I open the "Agents" page
    Then the "Run AI models on this computer" row says "Off. The software and any downloaded models were kept."
    When I toggle the switch in the "Run AI models on this computer" row
    Then the application log shows Agent Mode would write its unit and fragment
    And the Agent Mode switch settles off and sensitive
    And the "Run AI models on this computer" row says "Off. The software and any downloaded models were kept."
    And the Agent Mode unit does not exist
    And llmman was never asked to "serve"
    And systemctl was never run

  @stub.agents.llmman @stub.agents.unit @stub.agents.node @stub.agents.alias
  Scenario: Turning Agent Mode off in a dry run keeps the unit and the ready state
    Given ChairLift is running
    When I open the "Agents" page
    And I note the Agents files on disk
    Then the "Run AI models on this computer" row says "Ready."
    When I toggle the switch in the "Run AI models on this computer" row
    Then the application log shows Agent Mode would remove its unit and fragment
    And the Agent Mode switch settles on and sensitive
    And the "Run AI models on this computer" row says "Ready."
    And I see "[DRY-RUN] Preview: Agent Mode would be turned off — no changes made"
    And the Agents files on disk are unchanged
    And systemctl was never run
    And the action journal is empty

  # ------------------------------------------------------------ model presets

  @stub.agents.llmman @stub.agents.unit @stub.agents.node @stub.agents.alias
  Scenario: Cancelling the preset chooser changes nothing and can be reopened
    Given ChairLift is running
    When I open the "Agents" page
    And I click the "Switch…" button in the "Recommended Presets" row
    Then a dialog titled "Switch Model Preset" is shown
    When I choose "Cancel" in the dialog
    Then no dialog is shown
    And the application log does not contain "would configure alias"
    When I click the "Switch…" button in the "Recommended Presets" row
    Then a dialog titled "Switch Model Preset" is shown

  @stub.agents.llmman @stub.agents.unit @stub.agents.node @stub.agents.alias
  Scenario: A preset in a dry run resolves a fitting model offline and pulls nothing
    Given ChairLift is running
    When I open the "Agents" page
    And I click the "Switch…" button in the "Recommended Presets" row
    Then a dialog titled "Switch Model Preset" is shown
    And the dialog says "Qwen (Recommended)"
    And the dialog says "Mistral / Ministral"
    And the dialog says "DeepSeek"
    And the dialog says "GPT-OSS"
    When I choose "Gemma" in the dialog
    Then the application log contains "[DRY-RUN] would configure alias bluefin-active to unsloth/gemma-3"
    And I see "[DRY-RUN] Would switch to unsloth/gemma-3"
    And llmman was never asked to "pull"
    And llmman was never asked to "config set"

  @known_issue.355
  @stub.agents.llmman @stub.agents.unit @stub.agents.node @stub.agents.alias
  Scenario: A dry-run preset leaves the Active Model row showing the model actually configured
    Given ChairLift is running
    When I open the "Agents" page
    And I click the "Switch…" button in the "Recommended Presets" row
    And I choose "Gemma" in the dialog
    Then the application log contains "[DRY-RUN] would configure alias bluefin-active to unsloth/gemma-3"
    And the "Active Model" row says "unsloth/Qwen3-8B-GGUF:Q4_K_M"

  # ------------------------------------------------------------ peers

  @stub.agents.llmman @stub.agents.node @stub.agents.peers
  Scenario: Configured peers show their probed status, and a disabled one is not probed
    Given ChairLift is running
    When I open the "Agents" page
    Then the "localhost:17434" row says "Reachable"
    And the "127.0.0.1:9" row says "Not reachable"
    And the "10.0.0.5" row says "Disabled — not checked"
    And the Agents peer "localhost:17434" switch is on
    And the Agents peer "10.0.0.5" switch is off

  Scenario: Adding a peer in a dry run normalises the address and stores nothing
    Given ChairLift is running
    When I open the "Agents" page
    And I open the Agents add-peer dialog
    Then a dialog titled "Add a peer" is shown
    When I enter "HTTPS://Spark.Local:8443" as the Agents peer address
    And I choose "Add" in the dialog
    Then the application log contains "[DRY-RUN] would configure llmman peers [https://spark.local:8443] and add https://spark.local:8443 to the peer store"
    And the Agent Mode peer store does not exist
    And the Agents page lists no peer "https://spark.local:8443"
    And I see "No peers configured"

  Scenario: A peer address with an unsupported scheme is rejected
    Given ChairLift is running
    When I open the "Agents" page
    And I open the Agents add-peer dialog
    Then a dialog titled "Add a peer" is shown
    When I enter "ftp://spark.local" as the Agents peer address
    And I choose "Add" in the dialog
    Then I see "Could not add that peer"
    And the application log does not contain "would configure llmman peers"
    And the Agent Mode peer store does not exist

  Scenario: Cancelling the add-peer dialog does nothing
    Given ChairLift is running
    When I open the "Agents" page
    And I open the Agents add-peer dialog
    Then a dialog titled "Add a peer" is shown
    When I enter "spark.local" as the Agents peer address
    And I choose "Cancel" in the dialog
    Then no dialog is shown
    And the application log does not contain "would configure llmman peers"
    And the Agent Mode peer store does not exist

  @stub.agents.llmman @stub.agents.node @stub.agents.peers
  Scenario: Disabling and enabling peers in a dry run leaves the store and switches as they were
    Given ChairLift is running
    When I open the "Agents" page
    And I note the Agents files on disk
    And I toggle the switch in the "localhost:17434" row
    Then the application log contains "[DRY-RUN] would configure llmman peers [localhost:17434 127.0.0.1:9 10.0.0.5] (setting localhost:17434 enabled=false) and persist the peer store"
    And the Agents peer "localhost:17434" switch is on
    When I toggle the switch in the "10.0.0.5" row
    Then the application log contains "(setting 10.0.0.5 enabled=true) and persist the peer store"
    And the Agents peer "10.0.0.5" switch is off
    And the "10.0.0.5" row says "Disabled — not checked"
    And the Agents files on disk are unchanged
    And llmman was never asked to "config set"

  @stub.agents.llmman @stub.agents.node @stub.agents.peers
  Scenario: Removing a peer asks first, and a dry-run removal keeps it
    Given ChairLift is running
    When I open the "Agents" page
    And I note the Agents files on disk
    And I press the remove button in the Agents peer "10.0.0.5" row
    Then a dialog titled "Remove 10.0.0.5?" is shown
    When I choose "Cancel" in the dialog
    Then no dialog is shown
    And the application log does not contain "would configure llmman peers"
    When I press the remove button in the Agents peer "10.0.0.5" row
    Then a dialog titled "Remove 10.0.0.5?" is shown
    When I choose "Remove" in the dialog
    Then the application log contains "[DRY-RUN] would configure llmman peers [localhost:17434 127.0.0.1:9] and remove 10.0.0.5 from the peer store"
    And the "10.0.0.5" row says "Disabled — not checked"
    And the Agents files on disk are unchanged
    And llmman was never asked to "config set"

  @known_issue.355
  @stub.agents.llmman @stub.agents.node @stub.agents.peers
  Scenario: A dry-run peer toggle previews the enabled peers llmman would be given
    Given ChairLift is running
    When I open the "Agents" page
    And I toggle the switch in the "localhost:17434" row
    Then the application log contains "[DRY-RUN] would configure llmman peers [127.0.0.1:9] (setting localhost:17434 enabled=false)"

  @known_issue.355
  @stub.agents.llmman
  Scenario: Pressing Return in the peer key field previews saving it without logging the key
    Given ChairLift is running
    When I open the "Agents" page
    And I enter "s3cret-peer-key" as the Agents peer key and press Return
    Then the application log shows the peer key would be set without revealing "s3cret-peer-key"
    And llmman was never asked to "config set"

  @known_issue.355
  @stub.agents.llmman @stub.agents.peers
  Scenario: Every Agents control has an accessible name
    Given ChairLift is running
    When I open the "Agents" page
    Then every visible action control has an accessible name and an action
    And every visible toggle reports its state
