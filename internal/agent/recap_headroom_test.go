package agent

import (
	"testing"

	"github.com/u007/ocode/internal/config"
)

// TestRecapHeadroom_LowOccupancyUsesMainModel: when the transcript has barely
// touched the window, the recap runs on the session's own model.
func TestRecapHeadroom_LowOccupancyUsesMainModel(t *testing.T) {
	main := &GenericClient{Provider: "openai", Model: "gpt-4o", APIKey: "k"}
	cfg := &config.Config{Ocode: config.OcodeConfig{RecapModel: "openai/gpt-4o-mini"}}
	a := &Agent{client: main, config: cfg}
	a.lastInputTokens.Store(1000) // ~0.8% of 128k

	if !a.recapHasEnoughContextHeadroom() {
		t.Fatal("expected enough headroom at low occupancy")
	}
	if a.recapClient() != main {
		t.Fatalf("low occupancy: expected main client, got %T", a.recapClient())
	}
}

// TestRecapHeadroom_HighOccupancyUsesRecapModel: near-full window with under 50k
// headroom falls back to the configured recap model.
func TestRecapHeadroom_HighOccupancyUsesRecapModel(t *testing.T) {
	// The recap model is built through NewClient, which refuses to build a
	// keyless client, so it needs a credential to exist.
	t.Setenv("OPENAI_API_KEY", "test-openai-key")
	main := &GenericClient{Provider: "openai", Model: "gpt-4o", APIKey: "k"}
	cfg := &config.Config{Ocode: config.OcodeConfig{RecapModel: "openai/gpt-4o-mini"}}
	a := &Agent{client: main, config: cfg}
	a.lastInputTokens.Store(120000) // ~94% of 128k, ~8k headroom

	if a.recapHasEnoughContextHeadroom() {
		t.Fatal("expected insufficient headroom at high occupancy")
	}
	c := a.recapClient()
	if c == main {
		t.Fatal("high occupancy: expected recap model, got main")
	}
	if _, ok := c.(*GenericClient); !ok {
		t.Fatalf("expected *GenericClient, got %T", c)
	}
}

// TestRecapHeadroom_UnknownWindowFallsBack: an unregistered model cannot prove
// headroom, so the configured recap model is used.
func TestRecapHeadroom_UnknownWindowFallsBack(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-openai-key")
	main := &GenericClient{Provider: "openai", Model: "definitely-not-a-model", APIKey: "k"}
	cfg := &config.Config{Ocode: config.OcodeConfig{RecapModel: "openai/gpt-4o-mini"}}
	a := &Agent{client: main, config: cfg}
	a.lastInputTokens.Store(100)

	if a.recapHasEnoughContextHeadroom() {
		t.Fatal("expected no headroom proof for unknown window")
	}
	if a.recapClient() == main {
		t.Fatal("unknown window: expected recap model, got main")
	}
}

// TestRecapHeadroom_ZeroOccupancyUsesMainModel: a fresh session (no provider
// reading yet) is treated as empty context, so the main model is used.
func TestRecapHeadroom_ZeroOccupancyUsesMainModel(t *testing.T) {
	main := &GenericClient{Provider: "openai", Model: "gpt-4o", APIKey: "k"}
	cfg := &config.Config{Ocode: config.OcodeConfig{RecapModel: "openai/gpt-4o-mini"}}
	a := &Agent{client: main, config: cfg}
	// lastInputTokens left at 0

	if !a.recapHasEnoughContextHeadroom() {
		t.Fatal("expected enough headroom at zero occupancy")
	}
	if a.recapClient() != main {
		t.Fatalf("zero occupancy: expected main client, got %T", a.recapClient())
	}
}

// TestRecapHeadroom_CompactedTokensFallback: when LastInputTokens is 0 but a
// post-compaction estimate exists, it is used for the headroom decision.
func TestRecapHeadroom_CompactedTokensFallback(t *testing.T) {
	main := &GenericClient{Provider: "openai", Model: "gpt-4o", APIKey: "k"}
	cfg := &config.Config{Ocode: config.OcodeConfig{RecapModel: "openai/gpt-4o-mini"}}
	a := &Agent{client: main, config: cfg}
	a.compactedContextTokens.Store(120000)

	if a.recapHasEnoughContextHeadroom() {
		t.Fatal("expected insufficient headroom from compacted estimate")
	}
}
