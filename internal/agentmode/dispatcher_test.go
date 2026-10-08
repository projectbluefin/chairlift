package agentmode

import (
	"testing"

	"github.com/projectbluefin/chairlift/internal/troubleshoot"
)

const testModel = "unsloth/Qwen3-8B-GGUF:Q4_K_M"

// installedTools is a supported host with every package present.
func installedTools() troubleshoot.State {
	return troubleshoot.State{
		Supported:   true,
		ServerPath:  "/home/linuxbrew/.linuxbrew/bin/linux-mcp-server",
		DesktopPath: "/home/linuxbrew/.linuxbrew/bin/goose-desktop",
		LLMManPath:  "/home/linuxbrew/.linuxbrew/bin/llmman",
	}
}

func TestDispatchAskBluefin(t *testing.T) {
	without := func(edit func(*troubleshoot.State)) troubleshoot.State {
		tools := installedTools()
		edit(&tools)
		return tools
	}
	tests := []struct {
		name       string
		facts      ReadinessFacts
		wantAction DispatchAction
		wantModel  string
		wantState  State
		wantReason string
	}{
		{
			name:       "all ready -> dispatch launch",
			facts:      ReadinessFacts{DaemonHealthy: true, ActiveModel: testModel, Tools: installedTools()},
			wantAction: DispatchLaunch,
			wantModel:  testModel,
			wantState:  StateReady,
		},
		{
			name:       "daemon unavailable -> present agents",
			facts:      ReadinessFacts{ActiveModel: testModel, Tools: installedTools()},
			wantAction: DispatchPresentAgents,
			wantState:  StateDaemonUnavailable,
			wantReason: "Turn on Agent Mode to use Goose.",
		},
		{
			name:       "model unavailable -> present agents",
			facts:      ReadinessFacts{DaemonHealthy: true, Tools: installedTools()},
			wantAction: DispatchPresentAgents,
			wantState:  StateModelUnavailable,
			wantReason: "Choose a model in Agent Mode to use Goose.",
		},
		{
			name:       "goose missing -> present agents",
			facts:      ReadinessFacts{DaemonHealthy: true, ActiveModel: testModel, Tools: without(func(s *troubleshoot.State) { s.DesktopPath = "" })},
			wantAction: DispatchPresentAgents,
			wantState:  StatePackagesMissing,
			wantReason: "Goose isn't set up yet.",
		},
		{
			name:       "linux-mcp-server missing -> present agents",
			facts:      ReadinessFacts{DaemonHealthy: true, ActiveModel: testModel, Tools: without(func(s *troubleshoot.State) { s.ServerPath = "" })},
			wantAction: DispatchPresentAgents,
			wantState:  StatePackagesMissing,
			wantReason: "Goose isn't set up yet.",
		},
		{
			name:       "unsupported architecture -> present agents",
			facts:      ReadinessFacts{DaemonHealthy: true, ActiveModel: testModel, Tools: without(func(s *troubleshoot.State) { s.Supported = false })},
			wantAction: DispatchPresentAgents,
			wantState:  StateUnsupported,
			wantReason: "Goose isn't available for this kind of computer.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision := Dispatch(tt.facts)
			if decision.Action != tt.wantAction {
				t.Errorf("Dispatch().Action = %v, want %v", decision.Action, tt.wantAction)
			}
			if decision.Model != tt.wantModel {
				t.Errorf("Dispatch().Model = %q, want %q", decision.Model, tt.wantModel)
			}
			if decision.State != tt.wantState {
				t.Errorf("Dispatch().State = %v, want %v", decision.State, tt.wantState)
			}
			if decision.Reason != tt.wantReason {
				t.Errorf("Dispatch().Reason = %q, want %q", decision.Reason, tt.wantReason)
			}
		})
	}
}
