package tui

import (
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
)

func TestHandleSmallModelCmdAutoRespectsDisabledToggle(t *testing.T) {
	chdirTempForConfigTest(t)
	t.Setenv("HOME", t.TempDir())

	m := model{config: &config.Config{Ocode: config.OcodeConfig{SmallModelEnabled: false, SmallModel: "existing/model"}}}
	m.handleSmallModelCmd([]string{"auto"})

	if got := m.config.Ocode.SmallModel; got != "" {
		t.Fatalf("expected auto-resolve value to be cleared, got %q", got)
	}
	if len(m.messages) == 0 {
		t.Fatal("expected status message")
	}
	if !strings.Contains(m.messages[len(m.messages)-1].text, "disabled") {
		t.Fatalf("expected disabled message, got %q", m.messages[len(m.messages)-1].text)
	}
}
