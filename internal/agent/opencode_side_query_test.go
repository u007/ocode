package agent

import (
	"testing"

	"github.com/u007/ocode/internal/config"
)

// The side-query helpers below build their own *GenericClient from the small
// model chain, which very often resolves to an opencode* provider
// (SmallModelPriority leads with opencode-go/opencode). Each must inherit the
// main conversation identity so the X-Opencode-Session header stays stable
// across every request in the conversation (AGENTS.md prompt-cache /
// request-affinity contract). Compaction has coverage in agent_test.go; these
// mirror it for the other bound helpers.
//
// opencode-go is a key-optional provider, so these models construct clients
// without credentials in tests.

const sideQueryTestModel = "opencode-go/mimo-v2.5"

func TestRecapClientPreservesOpenCodeSessionID(t *testing.T) {
	a := &Agent{
		client: &GenericClient{Provider: "opencode-go", Model: "mimo-v2.5"},
		config: &config.Config{Ocode: config.OcodeConfig{RecapModel: sideQueryTestModel}},
	}
	a.SetOpenCodeSessionID("sess-recap")

	if got := opencodeHeaderForTest(t, a.recapClient()); got != "sess-recap" {
		t.Fatalf("recap client header = %q, want %q", got, "sess-recap")
	}
}

func TestRecapClientFallsBackToMainClientWithoutOverride(t *testing.T) {
	// No recap model configured and small model disabled: recapClient must
	// return the main client, whose identity is already bound.
	main := &GenericClient{Provider: "opencode-go", Model: "mimo-v2.5"}
	a := &Agent{client: main, config: &config.Config{}}
	a.SetOpenCodeSessionID("sess-main")

	if a.recapClient() != main {
		t.Fatal("expected recapClient to fall back to the main client")
	}
}

func TestAutoContinueJudgeClientPreservesOpenCodeSessionID(t *testing.T) {
	a := &Agent{
		client: &GenericClient{Provider: "opencode-go", Model: "mimo-v2.5"},
		config: &config.Config{Ocode: config.OcodeConfig{AutoContinueModel: sideQueryTestModel}},
	}
	a.SetOpenCodeSessionID("sess-autocont")

	if got := opencodeHeaderForTest(t, a.autoContinueJudgeClient()); got != "sess-autocont" {
		t.Fatalf("auto-continue judge client header = %q, want %q", got, "sess-autocont")
	}
}

func TestAutoContinueJudgeClientNilWhenUnset(t *testing.T) {
	a := &Agent{
		client: &GenericClient{Provider: "opencode-go", Model: "mimo-v2.5"},
		config: &config.Config{},
	}
	if got := a.autoContinueJudgeClient(); got != nil {
		t.Fatalf("autoContinueJudgeClient() = %T, want nil when AutoContinueModel is unset", got)
	}
}

func TestTitleClientsPreserveOpenCodeSessionID(t *testing.T) {
	// Use a builtins-only registry so a user's global "title" agent (or any
	// markdown override) cannot inject a different first model.
	saved := DefaultAgentRegistry
	DefaultAgentRegistry = NewAgentRegistry()
	defer func() { DefaultAgentRegistry = saved }()

	a := &Agent{
		client: &GenericClient{Provider: "opencode-go", Model: "mimo-v2.5"},
		config: &config.Config{Ocode: config.OcodeConfig{
			SmallModel:        sideQueryTestModel,
			SmallModelEnabled: true,
		}},
	}
	a.SetOpenCodeSessionID("sess-title")

	clients := a.titleClients()
	if len(clients) == 0 {
		t.Fatal("titleClients() returned no clients")
	}
	if got := opencodeHeaderForTest(t, clients[0]); got != "sess-title" {
		t.Fatalf("title client header = %q, want %q", got, "sess-title")
	}
}
