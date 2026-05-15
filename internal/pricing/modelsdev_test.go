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
