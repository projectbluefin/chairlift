@agents @stub.agents.host
Feature: Agents page
  Agent Mode runs a loopback model server as a systemd user unit. Its status
  comes from the executable, unit and node endpoint, never the unit alone.
  Model controls stay visible when unavailable. Under --dry-run every action
  is a preview and leaves software, files and model configuration unchanged.

  @env.CHAIRLIFT_CAPABILITIES=image-descriptor,flatpak,podman,bootc-stage
  Scenario: Agents is hidden on a host without Homebrew
    Given ChairLift is running
    Then the sidebar has no "Agents" row

  @config.agents-disabled
  Scenario: Agents is hidden when its group is disabled in configuration
    Given ChairLift is running
    Then the sidebar has no "Agents" row

  Scenario: A host that never set up Agent Mode offers to install it
    Given ChairLift is running
    When I open the "Agents" page
    Then the switch in the "Agent Mode" row is off
    And the "Agent Mode" row says "Turn on to install the model server"
    And the "Active Model" row says "Turn on Agent Mode to choose a model."
    And the model chooser is insensitive
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

  @stub.agents.llmman @stub.agents.unit @stub.agents.node
  Scenario: Every Agents control has an accessible name
    Given ChairLift is running
    When I open the "Agents" page
    Then every visible action control has an accessible name and an action
    And every visible toggle reports its state
