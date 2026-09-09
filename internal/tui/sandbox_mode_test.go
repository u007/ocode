package tui

import (
	"strings"
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

// TestPermClickCycleIncludesSandbox locks the cycle order:
// normal(auto on) → yolo → locked → sandbox → sandbox·auto → normal. The
// status-bar permission text only exists when the mode label is non-empty,
// so the model starts in normal·auto (visible "normal · auto-permission on"
// label).
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
	// Click 3: locked → sandbox
	m = clickPerm(m)
	if pm.Mode() != agent.PermissionModeSandbox {
		t.Fatalf("after click 3 mode=%s, want sandbox (cycle must reach it)", pm.Mode())
	}
	// Entering sandbox must not resurrect auto-permission (yolo forced it off;
	// Decision 9 — a mode change alone never touches AutoPermissionEnabled).
	if pm.AutoPermissionEnabled() {
		t.Fatal("entering sandbox re-enabled auto-permission")
	}
	// Click 4: sandbox → sandbox·auto (the new step — reachable by cycling
	// alone, mirroring the normal·auto stop, not a side effect of the mode
	// change itself).
	m = clickPerm(m)
	if pm.Mode() != agent.PermissionModeSandbox {
		t.Fatalf("after click 4 mode=%s, want sandbox (still sandbox, auto toggled)", pm.Mode())
	}
	if !pm.AutoPermissionEnabled() {
		t.Fatal("after click 4, expected auto-permission enabled (sandbox·auto step)")
	}
	// Click 5: sandbox·auto → normal (full wrap-around), turning auto back off.
	m = clickPerm(m)
	if pm.Mode() != agent.PermissionModeNormal {
		t.Fatalf("after click 5 mode=%s, want normal (full cycle)", pm.Mode())
	}
	if pm.AutoPermissionEnabled() {
		t.Fatal("after click 5, expected auto-permission disabled (wrap to normal)")
	}
}

// TestSidebarPermClickCycleIncludesSandboxAuto locks the sidebar "Allowed"
// header's click-cycle handler (handleMouseAction, sidebarAllowedHeaderForClick
// branch) against the same normal → normal·auto → yolo → locked → sandbox →
// sandbox·auto → normal order as the status-bar handler. The two handlers
// duplicate the same switch — this guards them from silently diverging if only
// one copy is edited in the future.
func TestSidebarPermClickCycleIncludesSandboxAuto(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // isolate persist writes

	a := agent.NewAgent(nil, nil, nil, nil)
	m := &model{
		agent:       a,
		ready:       true,
		width:       140,
		height:      40,
		activeTab:   tabChat,
		showSidebar: true,
		input:       newTestTextarea(),
		styles:      ApplyThemeColors("tokyonight"),
	}
	m.layout()
	pm := m.agent.Permissions()
	pm.SetAutoPermissionEnabled(true)

	clickSidebarAllowed := func(m *model) *model {
		t.Helper()
		rows := strings.Split(stripANSI(m.renderContent()), "\n")
		sidebarX := m.panelWidth() + 1
		allowedY := -1
		for y, r := range rows {
			if strings.Contains(r, "Allowed") {
				allowedY = y
				break
			}
		}
		if allowedY < 0 {
			t.Fatal("Allowed header not found in rendered sidebar")
		}
		upd, _ := m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: sidebarX, Y: allowedY})
		m = modelPtr(upd)
		upd, _ = m.Update(tea.MouseReleaseMsg{Button: tea.MouseNone, X: sidebarX, Y: allowedY})
		return modelPtr(upd)
	}

	// Click 1: normal(auto on) → yolo
	m = clickSidebarAllowed(m)
	if pm.Mode() != agent.PermissionModeYOLO {
		t.Fatalf("after click 1 mode=%s, want yolo", pm.Mode())
	}
	// Click 2: yolo → locked
	m = clickSidebarAllowed(m)
	if pm.Mode() != agent.PermissionModeLocked {
		t.Fatalf("after click 2 mode=%s, want locked", pm.Mode())
	}
	// Click 3: locked → sandbox
	m = clickSidebarAllowed(m)
	if pm.Mode() != agent.PermissionModeSandbox {
		t.Fatalf("after click 3 mode=%s, want sandbox", pm.Mode())
	}
	if pm.AutoPermissionEnabled() {
		t.Fatal("entering sandbox re-enabled auto-permission")
	}
	// Click 4: sandbox → sandbox·auto
	m = clickSidebarAllowed(m)
	if pm.Mode() != agent.PermissionModeSandbox || !pm.AutoPermissionEnabled() {
		t.Fatalf("after click 4 mode=%s auto=%v, want sandbox·auto", pm.Mode(), pm.AutoPermissionEnabled())
	}
	// Click 5: sandbox·auto → normal (full wrap-around)
	m = clickSidebarAllowed(m)
	if pm.Mode() != agent.PermissionModeNormal || pm.AutoPermissionEnabled() {
		t.Fatalf("after click 5 mode=%s auto=%v, want normal (auto off)", pm.Mode(), pm.AutoPermissionEnabled())
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

// TestSandboxPersistsAsDefault locks the current contract: Decision 2
// ("sandbox must never be the persisted default") was superseded by explicit
// owner request on 2026-09-09 — see
// docs/superpowers/plans/2026-08-31-shell-sandbox/INDEX.md. A live sandbox
// mode now persists as `sandbox`, like any other mode. Cron jobs are
// unaffected: they resolve their own per-job permission mode independently
// (internal/server/scheduler_runner.go resolveCronPermissionMode).
func TestSandboxPersistsAsDefault(t *testing.T) {
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
	if cfg.Permissions.Mode != "sandbox" {
		t.Fatalf("persisted mode = %q, want sandbox", cfg.Permissions.Mode)
	}
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
