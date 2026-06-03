package main

import (
	"testing"

	providerplugin "github.com/jamesmercstudio/ocode/internal/plugin/provider"
)

// TestCodexPluginRegistered guards the blank import of internal/plugin/codex in
// main.go. Without it the plugin's init() never runs, providerplugin.Get("openai")
// returns ok=false, and ChatGPT OAuth tokens are misrouted to
// api.openai.com/v1/chat/completions (401 missing_scope: model.request) instead
// of the Codex backend. This test lives in package main so it exercises main.go's
// import set directly — a test that imported the codex package itself would pass
// trivially and not catch the import being dropped.
func TestCodexPluginRegistered(t *testing.T) {
	plugin, ok := providerplugin.Get("openai")
	if !ok {
		t.Fatal("openai provider plugin not registered; main.go is missing the blank import of internal/plugin/codex")
	}
	if !plugin.ModelAllowed("gpt-5.4-mini") {
		t.Errorf("ModelAllowed(\"gpt-5.4-mini\") = false; codex backend routing would be skipped for this model")
	}
}
