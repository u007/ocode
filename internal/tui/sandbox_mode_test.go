package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/config"
)

// permClickModel builds a chat model with a live agent and a rendered status
// bar so the permission click region is populated.
func permClickModel(t *testing.T) *model {
	t.Helper()
	m := model{
		agent:  agent.NewAgent(nil, nil, nil, nil),
		ready:  true,
		width:  120,
		height: 40,
		styles: ApplyThemeColors("tokyonight"),
		input:  newTestTextarea(),
	}
	m.renderStatus()
	return &m
}

// clickPerm performs one press+release on the status-bar permission text.
func clickPerm(m *model) *model {
	statusTop := m.statusBarTopY()
	x := m.statusPermColStart + 1
	upd, _ := m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: x, Y: statusTop})
	m = modelPtr(upd)
	upd, _ = m.Update(tea.MouseReleaseMsg{Button: tea.MouseNone, X: x, Y: statusTop})
	return modelPtr(upd)
}

// modelPtr normalizes the tea.Model Update return to a *model. Some Update
// paths return the value type, others the pointer — both are seen in the wild
// depending on which handler branch runs — so a type switch is required.
func modelPtr(v tea.Model) *model {
	switch t := v.(type) {
	case *model:
		return t
	case model:
		return &t
	default:
		panic("modelPtr: unexpected Update return type")
	}
}

// TestPermClickCycleIncludesSandbox locks the new cycle order:
// normal(auto on) → yolo → locked → sandbox → normal. The status-bar
// permission text only exists when the mode label is non-empty, so the model
// starts in normal·auto (visible "normal · auto-permission on" label).
func TestPermClickCycleIncludesSandbox(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // isolate persist writes

	m := permClickModel(t)
	pm := m.agent.Permissions()
	pm.SetAutoPermissionEnabled(true) // visible status-bar label

	// Click 1: normal(auto on) → yolo
	m = clickPerm(m)
	if pm.Mode() != agent.PermissionModeYOLO {
		t.Fatalf("after click 1 mode=%s, want yolo (auto-on normal → yolo)", pm.Mode())
	}
	// Click 2: yolo → locked
	m = clickPerm(m)
	if pm.Mode() != agent.PermissionModeLocked {
		t.Fatalf("after click 2 mode=%s, want locked", pm.Mode())
	}
	// Click 3: locked → sandbox (the new step)
	m = clickPerm(m)
	if pm.Mode() != agent.PermissionModeSandbox {
		t.Fatalf("after click 3 mode=%s, want sandbox (cycle must reach it)", pm.Mode())
	}
	// Entering sandbox must not resurrect auto-permission (yolo forced it off).
	if pm.AutoPermissionEnabled() {
		t.Fatal("entering sandbox re-enabled auto-permission")
	}
	// Click 4: sandbox → normal (full wrap-around)
	m = clickPerm(m)
	if pm.Mode() != agent.PermissionModeNormal {
		t.Fatalf("after click 4 mode=%s, want normal (full cycle)", pm.Mode())
	}
}

// TestSandboxCommandSetsMode locks /sandbox on|off against the live agent.
func TestSandboxCommandSetsMode(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := model{
		agent:  agent.NewAgent(nil, nil, nil, nil),
		ready:  true,
		width:  120,
		height: 40,
		styles: ApplyThemeColors("tokyonight"),
		input:  newTestTextarea(),
	}

	upd, _ := m.handleCommand("/sandbox on")
	m = *upd.(*model)
	if m.agent.Permissions().Mode() != agent.PermissionModeSandbox {
		t.Fatalf("/sandbox on => mode %s, want sandbox", m.agent.Permissions().Mode())
	}
	upd, _ = m.handleCommand("/sandbox off")
	m = *upd.(*model)
	if m.agent.Permissions().Mode() != agent.PermissionModeNormal {
		t.Fatalf("/sandbox off => mode %s, want normal", m.agent.Permissions().Mode())
	}
}

// TestSandboxNotPersistedAsDefault locks Decision 2: a live sandbox mode is
// persisted as `normal` — sandbox never outlives the session as the durable
// default.
func TestSandboxNotPersistedAsDefault(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := model{
		agent:  agent.NewAgent(nil, nil, nil, nil),
		ready:  true,
		width:  120,
		height: 40,
		styles: ApplyThemeColors("tokyonight"),
		input:  newTestTextarea(),
	}
	m.agent.Permissions().SetMode(agent.PermissionModeSandbox)
	m.permDirty.mode = true
	m.persistPermissions()

	cfg, err := config.LoadOcodeConfigCopy()
	if err != nil {
		t.Fatalf("load persisted config: %v", err)
	}
	if cfg.Permissions.Mode != "normal" {
		t.Fatalf("persisted mode = %q, want normal (sandbox must not persist)", cfg.Permissions.Mode)
	}
	// The LIVE agent stays sandboxed — only the disk default is clamped.
	if m.agent.Permissions().Mode() != agent.PermissionModeSandbox {
		t.Fatalf("live mode = %s, want sandbox preserved", m.agent.Permissions().Mode())
	}
}

// TestShiftTabStillCyclesAgentMode locks the regression the review demanded:
// Shift+Tab still cycles agent focus/type via cycleAgentMode, unaffected by
// the sandbox click cycle.
func TestShiftTabStillCyclesAgentMode(t *testing.T) {
	m := permClickModel(t)
	specs := agent.PrimaryAgentSpecs()
	if len(specs) < 2 {
		t.Skip("fewer than two primary agent specs; nothing to cycle")
	}
	idxBefore := m.currentAgentIdx
	agentModeBefore := m.agent.Mode().String()
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	got := modelPtr(updated)
	if got.currentAgentIdx == idxBefore {
		t.Fatalf("Shift+Tab did not advance currentAgentIdx (still %d)", got.currentAgentIdx)
	}
	if got.agent.Mode().String() == agentModeBefore {
		t.Fatalf("Shift+Tab did not change the agent mode (still %q)", agentModeBefore)
	}
	// Permission mode untouched by the agent cycle.
	if got.agent.Permissions().Mode() != agent.PermissionModeNormal {
		t.Fatalf("agent cycle changed permission mode to %s, want normal", got.agent.Permissions().Mode())
	}
}