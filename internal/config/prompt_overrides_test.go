package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// isolatePromptOverrides gives the test a private HOME, a private working
// directory and an empty live store, and restores the empty store on cleanup.
func isolatePromptOverrides(t *testing.T) {
	t.Helper()
	chdirTempForConfigTest(t)
	setHomeTree(t, t.TempDir())
	SetPromptOverrides(nil)
	t.Cleanup(func() { SetPromptOverrides(nil) })
}

func TestPromptOverrideRoundTrip(t *testing.T) {
	isolatePromptOverrides(t)

	want := PromptOverride{Text: "Prefer British spelling."}
	if err := SavePromptOverride("title", want); err != nil {
		t.Fatalf("SavePromptOverride() error = %v", err)
	}

	got, err := LoadOcodeConfigCopy()
	if err != nil {
		t.Fatalf("LoadOcodeConfigCopy() error = %v", err)
	}
	if got.Prompts["title"] != want {
		t.Errorf("Prompts[title] = %+v, want %+v", got.Prompts["title"], want)
	}

	// A fresh process seeds the live store from the same file.
	SetPromptOverrides(nil)
	if err := LoadOcodeConfig(&Config{}); err != nil {
		t.Fatalf("LoadOcodeConfig() error = %v", err)
	}
	seeded, ok := PromptOverrideFor("title")
	if !ok || seeded != want {
		t.Errorf("PromptOverrideFor(title) = %+v, %v; want %+v, true", seeded, ok, want)
	}
}

func TestPromptOverrideDisabledRoundTrip(t *testing.T) {
	isolatePromptOverrides(t)

	want := PromptOverride{Disabled: true, Text: "kept for later"}
	if err := SavePromptOverride("goal_judge", want); err != nil {
		t.Fatalf("SavePromptOverride() error = %v", err)
	}

	got, err := LoadOcodeConfigCopy()
	if err != nil {
		t.Fatalf("LoadOcodeConfigCopy() error = %v", err)
	}
	if got.Prompts["goal_judge"] != want {
		t.Errorf("Prompts[goal_judge] = %+v, want %+v", got.Prompts["goal_judge"], want)
	}
}

func TestSavePromptOverrideDeletesDefaultEntry(t *testing.T) {
	isolatePromptOverrides(t)

	if err := SavePromptOverride("title", PromptOverride{Text: "x"}); err != nil {
		t.Fatalf("save with text: %v", err)
	}
	if err := SavePromptOverride("title", PromptOverride{Text: "   "}); err != nil {
		t.Fatalf("save blank text: %v", err)
	}

	got, err := LoadOcodeConfigCopy()
	if err != nil {
		t.Fatalf("LoadOcodeConfigCopy() error = %v", err)
	}
	if _, ok := got.Prompts["title"]; ok {
		t.Errorf("Prompts[title] still present after reset to default: %+v", got.Prompts["title"])
	}
	if _, ok := PromptOverrideFor("title"); ok {
		t.Error("live store still holds title after reset to default")
	}

	path, err := getGlobalOcodeConfigPath()
	if err != nil {
		t.Fatalf("getGlobalOcodeConfigPath() error = %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if strings.Contains(string(body), `"prompts"`) {
		t.Errorf("config file still has a prompts key after deleting the only entry:\n%s", body)
	}
}

func TestSavePromptOverrideKeepsDisabledEntry(t *testing.T) {
	isolatePromptOverrides(t)

	// A disabled prompt is a real setting even with no add-on text, so it must
	// survive the save. Enabled with no text is the default and is dropped.
	draft := PromptOverride{Disabled: true}
	if err := SavePromptOverride("goal_judge", draft); err != nil {
		t.Fatalf("SavePromptOverride() error = %v", err)
	}
	got, err := LoadOcodeConfigCopy()
	if err != nil {
		t.Fatalf("LoadOcodeConfigCopy() error = %v", err)
	}
	if got.Prompts["goal_judge"] != draft {
		t.Errorf("Prompts[goal_judge] = %+v, want kept disabled entry %+v", got.Prompts["goal_judge"], draft)
	}
	if text, ok := ResolvePrompt("goal_judge", "BUILTIN"); ok || text != "" {
		t.Errorf("ResolvePrompt with disabled prompt = %q, %v; want omitted", text, ok)
	}
}

func TestUnmarshalPromptOverrideMigratesLegacyEnabled(t *testing.T) {
	var store map[string]PromptOverride
	body := `{
		"enabled_with_text":  {"enabled": true,  "text": "add this"},
		"enabled_without":    {"enabled": true,  "text": ""},
		"disabled_with_text": {"enabled": false, "text": "inert text"},
		"new_schema":         {"disabled": true, "text": "kept"}
	}`
	if err := json.Unmarshal([]byte(body), &store); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	want := map[string]PromptOverride{
		"enabled_with_text":  {Text: "add this"},
		"enabled_without":    {},
		"disabled_with_text": {},
		"new_schema":         {Disabled: true, Text: "kept"},
	}
	for key, w := range want {
		if got := store[key]; got != w {
			t.Errorf("%s = %+v, want %+v", key, got, w)
		}
	}
}

func TestApplyPromptOverride(t *testing.T) {
	const builtin = "BUILTIN PROMPT"
	tests := []struct {
		name  string
		store map[string]PromptOverride
		key   string
		want  string
	}{
		{
			name:  "no entry",
			store: nil,
			key:   "title",
			want:  builtin,
		},
		{
			name:  "disabled with text is omitted",
			store: map[string]PromptOverride{"title": {Disabled: true, Text: "extra"}},
			key:   "title",
			want:  "",
		},
		{
			name:  "enabled with blank text",
			store: map[string]PromptOverride{"title": {Text: "  \n\t "}},
			key:   "title",
			want:  builtin,
		},
		{
			name:  "enabled with text is appended and trimmed",
			store: map[string]PromptOverride{"title": {Text: "  Prefer British spelling.\n"}},
			key:   "title",
			want:  builtin + "\n\nPrefer British spelling.",
		},
		{
			name:  "other key does not match",
			store: map[string]PromptOverride{"recap": {Text: "extra"}},
			key:   "title",
			want:  builtin,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			SetPromptOverrides(tt.store)
			t.Cleanup(func() { SetPromptOverrides(nil) })
			if got := ApplyPromptOverride(tt.key, builtin); got != tt.want {
				t.Errorf("ApplyPromptOverride(%q) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}

func TestResolvePrompt(t *testing.T) {
	const builtin = "BUILTIN"
	tests := []struct {
		name     string
		store    map[string]PromptOverride
		wantText string
		wantOK   bool
	}{
		{name: "no entry", store: nil, wantText: builtin, wantOK: true},
		{name: "disabled with text", store: map[string]PromptOverride{"k": {Disabled: true, Text: "x"}}},
		{name: "disabled without text", store: map[string]PromptOverride{"k": {Disabled: true}}},
		{name: "enabled with blank text", store: map[string]PromptOverride{"k": {Text: "  "}}, wantText: builtin, wantOK: true},
		{name: "enabled with text", store: map[string]PromptOverride{"k": {Text: " add "}}, wantText: builtin + "\n\nadd", wantOK: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			SetPromptOverrides(tt.store)
			t.Cleanup(func() { SetPromptOverrides(nil) })
			text, ok := ResolvePrompt("k", builtin)
			if text != tt.wantText || ok != tt.wantOK {
				t.Errorf("ResolvePrompt() = %q, %v; want %q, %v", text, ok, tt.wantText, tt.wantOK)
			}
		})
	}
}

func TestPromptDisabled(t *testing.T) {
	tests := []struct {
		name  string
		store map[string]PromptOverride
		want  bool
	}{
		{name: "no entry", store: nil},
		{name: "enabled", store: map[string]PromptOverride{"k": {Text: "x"}}},
		{name: "disabled", store: map[string]PromptOverride{"k": {Disabled: true}}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			SetPromptOverrides(tt.store)
			t.Cleanup(func() { SetPromptOverrides(nil) })
			if got := PromptDisabled("k"); got != tt.want {
				t.Errorf("PromptDisabled(k) = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSavePromptOverridePreservesUnknownKeys(t *testing.T) {
	isolatePromptOverrides(t)

	path, err := getGlobalOcodeConfigPath()
	if err != nil {
		t.Fatalf("getGlobalOcodeConfigPath() error = %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	seed := `{"future_feature":{"knob":42},"pulse_model":"openai/gpt-x"}`
	if err := os.WriteFile(path, []byte(seed), 0o600); err != nil {
		t.Fatalf("write seed config: %v", err)
	}

	if err := SavePromptOverride("recap", PromptOverride{Text: "Be terse."}); err != nil {
		t.Fatalf("SavePromptOverride() error = %v", err)
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("parse saved config: %v\n%s", err, body)
	}
	var future struct {
		Knob int `json:"knob"`
	}
	if err := json.Unmarshal(raw["future_feature"], &future); err != nil || future.Knob != 42 {
		t.Errorf("future_feature = %s (err %v), want preserved knob 42", raw["future_feature"], err)
	}
	var model string
	if err := json.Unmarshal(raw["pulse_model"], &model); err != nil || model != "openai/gpt-x" {
		t.Errorf("pulse_model = %s (err %v), want preserved", raw["pulse_model"], err)
	}
	if _, ok := raw["prompts"]; !ok {
		t.Errorf("saved config has no prompts key:\n%s", body)
	}
}

func TestSavePromptOverrideRejectsOversizeText(t *testing.T) {
	isolatePromptOverrides(t)

	atLimit := PromptOverride{Text: strings.Repeat("a", MaxPromptOverrideBytes)}
	if err := SavePromptOverride("title", atLimit); err != nil {
		t.Fatalf("text at the limit must save: %v", err)
	}

	over := PromptOverride{Text: strings.Repeat("a", MaxPromptOverrideBytes+1)}
	if err := SavePromptOverride("title", over); err == nil {
		t.Fatalf("SavePromptOverride() accepted %d bytes, limit is %d", len(over.Text), MaxPromptOverrideBytes)
	}
	if got, ok := PromptOverrideFor("title"); !ok || got != atLimit {
		t.Errorf("live store changed after rejected save: %+v; want the at-limit override", got)
	}
}

func TestEffectivePromptOverride(t *testing.T) {
	tests := []struct {
		name     string
		store    map[string]PromptOverride
		wantText string
		wantOK   bool
	}{
		{
			name:  "no entry",
			store: nil,
		},
		{
			name:  "disabled with text",
			store: map[string]PromptOverride{"title": {Disabled: true, Text: "extra"}},
		},
		{
			name:  "enabled with blank text",
			store: map[string]PromptOverride{"title": {Text: ""}},
		},
		{
			name:  "enabled with whitespace only",
			store: map[string]PromptOverride{"title": {Text: " \n\t "}},
		},
		{
			name:     "enabled with text is trimmed",
			store:    map[string]PromptOverride{"title": {Text: "  Prefer British spelling.\n"}},
			wantText: "Prefer British spelling.",
			wantOK:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			SetPromptOverrides(tt.store)
			t.Cleanup(func() { SetPromptOverrides(nil) })
			got, ok := EffectivePromptOverride("title")
			if got != tt.wantText || ok != tt.wantOK {
				t.Errorf("EffectivePromptOverride(title) = %q, %v; want %q, %v", got, ok, tt.wantText, tt.wantOK)
			}
		})
	}
}
