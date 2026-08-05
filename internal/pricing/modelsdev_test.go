package pricing

import "testing"

func TestLookupReturnsBundledPricing(t *testing.T) {
	got, ok := Lookup("gpt-4o")
	if !ok {
		t.Fatal("expected gpt-4o pricing to exist")
	}

	if got.InputPerMillion != 5 || got.OutputPerMillion != 15 {
		t.Fatalf("unexpected gpt-4o pricing: %+v", got)
	}
}

func TestLookupRejectsUnknownModel(t *testing.T) {
	if _, ok := Lookup("does-not-exist"); ok {
		t.Fatal("expected unknown model lookup to fail")
	}
}

func TestLookupNormalizesPrefixedAndVersionedModels(t *testing.T) {
	got, ok := Lookup("openai/gpt-4o-2024-05-13")
	if !ok {
		t.Fatal("expected normalized model lookup to succeed")
	}

	if got.InputPerMillion != 5 || got.OutputPerMillion != 15 {
		t.Fatalf("unexpected normalized pricing: %+v", got)
	}
}

func TestLookupFallsBackToMiniMaxM3Pricing(t *testing.T) {
	got, ok := Lookup("minimax/minimax-m3-20260531")
	if !ok {
		t.Fatal("expected minimax m3 pricing to fall back")
	}

	if got.InputPerMillion != 0.30 || got.OutputPerMillion != 1.20 {
		t.Fatalf("unexpected minimax m3 pricing: %+v", got)
	}
}

func TestLookupNormalizesCaseInsensitiveModelNames(t *testing.T) {
	got, ok := Lookup("MiniMax/MiniMax-M3")
	if !ok {
		t.Fatal("expected case-insensitive minimax lookup to succeed")
	}

	if got.InputPerMillion != 0.30 || got.OutputPerMillion != 1.20 {
		t.Fatalf("unexpected case-insensitive pricing: %+v", got)
	}
}

func TestLookupFallsBackToCodexGPT56Pricing(t *testing.T) {
	// The GPT-5.6 codex family (gpt-5.6-luna/sol/terra) must resolve even when
	// the models.dev snapshot is stale, so codex sessions don't silently bill $0.
	got, ok := Lookup("gpt-5.6-luna")
	if !ok {
		t.Fatal("expected gpt-5.6-luna pricing to exist")
	}
	if got.InputPerMillion != 0.20 || got.OutputPerMillion != 1.20 {
		t.Fatalf("unexpected gpt-5.6-luna pricing: %+v", got)
	}

	// Provider-prefixed form must normalize the same way.
	if _, ok := Lookup("openai/gpt-5.6-terra"); !ok {
		t.Fatal("expected openai/gpt-5.6-terra pricing to resolve")
	}
}
