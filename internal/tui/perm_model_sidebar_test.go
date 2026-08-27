package tui

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/textarea"
	fastviewport "github.com/u007/ocode/internal/tui/fastviewport"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/discovery"
)

// TestSidebarPermModelRowStaysSingleLine is a regression test for the sidebar
// perm row wrapping long model ids (e.g. "lmstudio/ternary-bonsai-8b-mlx")
// onto a second, detached-looking line that read as "the model is missing".
// The row must clamp the id with an ellipsis and stay on one line.
func TestSidebarPermModelRowStaysSingleLine(t *testing.T) {
	cfg := config.Config{}
	cfg.Ocode.Permissions.Auto = &config.AutoPermissionConfig{Enabled: true, Model: "lmstudio/ternary-bonsai-8b-mlx"}
	m := model{
		config:      &cfg,
		agent:       agent.NewAgent(retryTestClient{}, nil, &cfg, nil),
		input:       textarea.New(),
		ready:       true,
		width:       140,
		height:      40,
		showSidebar: true,
		viewport:    fastviewport.New(100, 20),
	}
	data := m.buildSidebarRenderData()
	found := false
	for _, l := range data.topLines {
		plain := stripANSI(l)
		if strings.HasPrefix(plain, "perm:") && strings.Contains(plain, "bonsa") {
			found = true
		}
	}
	if !found {
		t.Fatal("perm row with model name not found on a single line")
	}
	// The model name must not appear as a bare detached line of its own.
	for _, l := range data.topLines {
		if strings.TrimSpace(stripANSI(l)) == "lmstudio/ternary-bonsai-8b-mlx" {
			t.Fatal("perm model name wrapped onto its own line")
		}
	}
}

// TestPermissionModelCmdRegisteredLocalModel verifies /permissions model accepts
// a registered /localmodel entry even when its server is not currently running
// (previously it failed with a misleading "unknown provider or missing
// configuration" because NewClient only resolves local base URLs from a live
// server), persists it, and returns the warm-up cmd that starts the server.
func TestPermissionModelCmdRegisteredLocalModel(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	if err := config.SaveLocalModelConfig("local/bonsai-8b-1bit", true, 1, 50000); err != nil {
		t.Fatalf("SaveLocalModelConfig: %v", err)
	}
	cfg := config.Config{}
	cfg.Ocode.LocalModels = map[string]config.LocalModelConfig{
		"local/bonsai-8b-1bit": {Enabled: true, MaxParallel: 1},
	}
	cfg.Ocode.Permissions.Auto = &config.AutoPermissionConfig{Enabled: true}
	m := model{
		config: &cfg,
		agent:  agent.NewAgent(retryTestClient{}, nil, &cfg, nil),
		input:  textarea.New(),
		// The warm-up decision must not depend on whether a real local-model
		// server happens to be running on the assigned port (11458) of the
		// machine running the test — stub the live probe so the entry is always
		// treated as stopped, which is the scenario this regression covers.
		probeLocalModelInstance: func(string, int) (discovery.InstanceInfo, bool) {
			return discovery.InstanceInfo{}, false
		},
	}

	cmd := m.handlePermissionModelCmd([]string{"local/bonsai-8b-1bit"})
	if cmd == nil {
		var msgs []string
		for _, msg := range m.messages {
			msgs = append(msgs, msg.text)
		}
		t.Fatalf("expected warm cmd for registered-but-stopped local model, messages=%q", msgs)
	}
	if m.config.Ocode.Permissions.Auto == nil || m.config.Ocode.Permissions.Auto.Model != "local/bonsai-8b-1bit" {
		t.Fatalf("expected perm model saved, got %+v", m.config.Ocode.Permissions.Auto)
	}
	last := m.messages[len(m.messages)-1].text
	if !strings.Contains(last, "Starting local/bonsai-8b-1bit") {
		t.Fatalf("expected warm-up message last, got %q", last)
	}

	// An unregistered local model must give a clear error and not change config.
	before := m.config.Ocode.Permissions.Auto.Model
	cmd = m.handlePermissionModelCmd([]string{"local/never-registered"})
	if cmd != nil {
		t.Fatal("expected nil cmd for unregistered local model")
	}
	last = m.messages[len(m.messages)-1].text
	if !strings.Contains(last, "not a registered local model") {
		t.Fatalf("expected clear registration error, got %q", last)
	}
	if m.config.Ocode.Permissions.Auto.Model != before {
		t.Fatalf("config should be unchanged, got %q", m.config.Ocode.Permissions.Auto.Model)
	}
}

// TestPermissionModelCmdRejectsDisabledLocalModel guards that a registered but
// DISABLED /localmodel entry cannot be persisted as the permission model: the
// warm-up path only starts enabled entries, so accepting it would save a config
// NewClient can never build a client for and auto-permission would silently
// fall back every session.
func TestPermissionModelCmdRejectsDisabledLocalModel(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	if err := config.SaveLocalModelConfig("local/bonsai-8b-1bit", false, 1, 50000); err != nil {
		t.Fatalf("SaveLocalModelConfig: %v", err)
	}
	cfg := config.Config{}
	cfg.Ocode.LocalModels = map[string]config.LocalModelConfig{
		"local/bonsai-8b-1bit": {Enabled: false, MaxParallel: 1},
	}
	cfg.Ocode.Permissions.Auto = &config.AutoPermissionConfig{Enabled: true}
	m := model{
		config: &cfg,
		agent:  agent.NewAgent(retryTestClient{}, nil, &cfg, nil),
		input:  textarea.New(),
	}

	cmd := m.handlePermissionModelCmd([]string{"local/bonsai-8b-1bit"})
	if cmd != nil {
		t.Fatal("expected nil cmd for a disabled local model")
	}
	last := m.messages[len(m.messages)-1].text
	if !strings.Contains(last, "disabled") {
		t.Fatalf("expected clear disabled error, got %q", last)
	}
	if m.config.Ocode.Permissions.Auto.Model != "" {
		t.Fatalf("config should be unchanged, got %q", m.config.Ocode.Permissions.Auto.Model)
	}
}
