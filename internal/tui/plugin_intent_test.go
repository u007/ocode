package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/skill"
)

// `/plugin install @tanstack/intent` must reach the npm fetch, not the bundled
// or git installers, which both reject it.
func TestPluginInstallIntentStartsFetch(t *testing.T) {
	m := model{input: newTestTextarea(), config: &config.Config{}}
	cmd := runPluginCmd(&m, []string{"install", skill.IntentPackageName})
	if cmd == nil {
		t.Fatal("expected a Cmd for /plugin install @tanstack/intent")
	}
	if _, ok := cmd().(pluginIntentListMsg); !ok {
		t.Fatalf("expected pluginIntentListMsg, got %T", cmd())
	}
}

func TestIntentSkillPickerIsMultiSelectChecklist(t *testing.T) {
	m := model{input: newTestTextarea(), config: &config.Config{}, width: 120}
	m.openIntentSkillPicker(skill.IntentPackage{Version: "9.9.9", Skills: []skill.IntentSkill{
		{Name: "alpha", Description: "Use when alpha."},
		{Name: "beta", Description: "Use when beta."},
	}})
	if !m.showPicker || m.pickerKind != "tanstack-intent-install" || !m.pickerMultiSelect {
		t.Fatalf("picker not opened as multi-select: show=%v kind=%q multi=%v", m.showPicker, m.pickerKind, m.pickerMultiSelect)
	}
	if len(m.pickerValues) != 2 || m.pickerValues[0] != "alpha" || m.pickerValues[1] != "beta" {
		t.Fatalf("picker values = %v, want [alpha beta]", m.pickerValues)
	}
	if m.pickerItems[1] != "beta — Use when beta." {
		t.Fatalf("picker item = %q", m.pickerItems[1])
	}
}

// Confirming a checked skill hands the pending package to the installer and
// clears it. The returned Cmd is not run: it would write to the real ~/.claude.
func TestIntentSkillPickerConfirmDispatchesInstall(t *testing.T) {
	m := model{input: newTestTextarea(), config: &config.Config{}, width: 120}
	m.openIntentSkillPicker(skill.IntentPackage{Version: "9.9.9", Skills: []skill.IntentSkill{
		{Name: "alpha", Description: "Use when alpha."},
		{Name: "beta", Description: "Use when beta."},
	}})
	m.pickerChecked["beta"] = true

	_, cmd := m.selectPickerMultiConfirm()
	if cmd == nil {
		t.Fatal("confirm with a checked skill must return an install Cmd")
	}
	if len(m.pendingIntentPackage.Skills) != 0 {
		t.Fatal("pending package should be cleared once the install is dispatched")
	}
}

// The picker does not clamp rows. A description longer than the box must be
// cut to fit on the row, or it wraps and the list height is wrong.
func TestIntentSkillPickerRowStaysOnOneLine(t *testing.T) {
	m := model{styles: ApplyThemeColors("tokyonight"), width: 80, height: 30, input: newTestTextarea(), config: &config.Config{}}
	m.openIntentSkillPicker(skill.IntentPackage{Version: "0.5.6", Skills: []skill.IntentSkill{
		{Name: "skill-staleness-check", Description: "Use when reviewing an upstream TanStack webhook payload containing the package, commit SHA, and changed files for stale skill guidance."},
	}})
	rendered := stripANSI(m.renderPicker())
	if strings.Contains(rendered, "guidance.") {
		t.Fatalf("description tail wrapped onto the picker:\n%s", rendered)
	}
	for _, line := range strings.Split(rendered, "\n") {
		if w := lipgloss.Width(line); w > 80 {
			t.Fatalf("picker row is %d cells wide at width 80: %q", w, line)
		}
	}
}
