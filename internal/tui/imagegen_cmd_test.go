package tui

import (
	"os"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
)

// newImageTestModel builds a minimal model with an isolated config (temp HOME
// + cwd) so the command handlers' persistence calls don't touch the real
// global ocode config.
func newImageTestModel(t *testing.T) *model {
	t.Helper()
	tmpHome := t.TempDir()
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() { os.Setenv("HOME", oldHome) })
	wd, _ := os.Getwd()
	tmpWork := t.TempDir()
	_ = os.Chdir(tmpWork)
	t.Cleanup(func() { _ = os.Chdir(wd) })
	return &model{
		config: &config.Config{
			Ocode: config.OcodeConfig{
				ImageGen: config.DefaultImageGenConfig(),
			},
		},
	}
}

func lastMsg(m *model) string {
	if len(m.messages) == 0 {
		return ""
	}
	return m.messages[len(m.messages)-1].text
}

func TestHandleImageEnableDisable(t *testing.T) {
	m := newImageTestModel(t)
	if m.config.Ocode.ImageGen.Enabled {
		t.Fatal("default should be disabled")
	}
	m.handleImageCmd([]string{"enable"})
	if !m.config.Ocode.ImageGen.Enabled {
		t.Error("enable did not set Enabled")
	}
	if !strings.Contains(lastMsg(m), "enabled") {
		t.Errorf("enable message wrong: %q", lastMsg(m))
	}
	m.handleImageCmd([]string{"disable"})
	if m.config.Ocode.ImageGen.Enabled {
		t.Error("disable did not clear Enabled")
	}
}

func TestHandleImageModel(t *testing.T) {
	m := newImageTestModel(t)
	m.handleImageCmd([]string{"model", "gemini/gemini-3.1-flash-image"})
	if m.config.Ocode.ImageGen.Provider != "gemini" || m.config.Ocode.ImageGen.Model != "gemini-3.1-flash-image" {
		t.Errorf("provider/model not set: %+v", m.config.Ocode.ImageGen)
	}
	if !strings.Contains(lastMsg(m), "gemini/gemini-3.1-flash-image") {
		t.Errorf("model message wrong: %q", lastMsg(m))
	}
	// model only -> keep current provider
	m.handleImageCmd([]string{"model", "gpt-image-1"})
	if m.config.Ocode.ImageGen.Provider != "gemini" {
		t.Errorf("provider changed unexpectedly: %q", m.config.Ocode.ImageGen.Provider)
	}
	if m.config.Ocode.ImageGen.Model != "gpt-image-1" {
		t.Errorf("model not set: %q", m.config.Ocode.ImageGen.Model)
	}
}

func TestHandleImageStatus(t *testing.T) {
	m := newImageTestModel(t)
	m.handleImageCmd(nil)
	if !strings.Contains(lastMsg(m), "Image generation:") {
		t.Errorf("status message wrong: %q", lastMsg(m))
	}
	m.handleImageCmd([]string{"bogus"})
	if !strings.Contains(lastMsg(m), "Usage: /image") {
		t.Errorf("usage message wrong: %q", lastMsg(m))
	}
}
