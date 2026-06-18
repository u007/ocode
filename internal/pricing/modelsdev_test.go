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
