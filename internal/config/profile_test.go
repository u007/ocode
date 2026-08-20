package config

import "testing"

func TestValidateProfileName(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{"ok simple", "work", false},
		{"ok with dash underscore", "my-profile_1", false},
		{"ok max 32", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", false},
		{"empty", "", true},
		{"uppercase", "Work", true},
		{"dot", "a.b", true},
		{"space", "a b", true},
		{"too long", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", true}, // 33
		{"slash", "a/b", true},
	}
	for _, tt := range tests {
		err := ValidateProfileName(tt.in)
		if (err != nil) != tt.wantErr {
			t.Errorf("%s: ValidateProfileName(%q) err=%v wantErr %v", tt.name, tt.in, err, tt.wantErr)
		}
	}
}

func TestProfileOverrideCount(t *testing.T) {
	model := "openai/gpt-4"
	delta := ProfileDelta{Model: &model}
	if n := ProfileOverrideCount(delta); n != 1 {
		t.Fatalf("want 1 got %d", n)
	}
	delta.Provider = map[string]interface{}{"openai": map[string]interface{}{"apiKey": "x"}}
	if n := ProfileOverrideCount(delta); n != 2 {
		t.Fatalf("want 2 got %d", n)
	}
}

func TestEffectiveOcodeConfig(t *testing.T) {
	base := defaultOcodeConfig()
	base.SmallModel = "base-small"
	base.Profiles = map[string]ProfileDelta{
		"work": {SmallModel: ptr("work-small")},
	}
	eff := EffectiveOcodeConfig(&base, "work")
	if eff.SmallModel != "work-small" {
		t.Fatalf("effective small model %q want work-small", eff.SmallModel)
	}
	eff2 := EffectiveOcodeConfig(&base, "")
	if eff2.SmallModel != "base-small" {
		t.Fatalf("default effective %q want base-small", eff2.SmallModel)
	}
	eff3 := EffectiveOcodeConfig(&base, "missing")
	if eff3.SmallModel != "base-small" {
		t.Fatalf("missing profile should fallback base, got %q", eff3.SmallModel)
	}
	if eff.Profiles != nil {
		t.Fatalf("effective should clear Profiles")
	}
}

func TestEffectiveConfig(t *testing.T) {
	base := &Config{Model: "base-model"}
	profiles := map[string]ProfileDelta{
		"work": {Model: ptr("work-model")},
	}
	eff := EffectiveConfig(base, "work", profiles)
	if eff.Model != "work-model" {
		t.Fatalf("effective model %q want work-model", eff.Model)
	}
	eff2 := EffectiveConfig(base, "", profiles)
	if eff2.Model != "base-model" {
		t.Fatalf("default effective %q want base-model", eff2.Model)
	}
}

func ptr(s string) *string { return &s }
