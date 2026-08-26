package main

import (
	"testing"

	providerplugin "github.com/u007/ocode/internal/plugin/provider"
)

func TestDesktopCodexPluginRegistered(t *testing.T) {
	plugin, ok := providerplugin.Get("openai")
	if !ok {
		t.Fatal("openai provider plugin not registered; desktop main.go is missing the blank import of internal/plugin/codex")
	}
	if !plugin.ModelAllowed("gpt-5.6-luna") {
		t.Error(`ModelAllowed("gpt-5.6-luna") = false; Codex routing would be skipped for this model`)
	}
}
