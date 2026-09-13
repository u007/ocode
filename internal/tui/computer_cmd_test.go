package tui

import (
	"os"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
)

func TestHandleComputerCmdEnableDisableStatus(t *testing.T) {
	chdirTempForConfigTest(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("APPDATA", home)

	cfg := config.Config{}
	m := model{config: &cfg}

	m.handleComputerCmd([]string{"enable"})
	if !cfg.Ocode.ComputerUse.Enabled {
		t.Fatal("enable did not update in-memory config")
	}
	if got := m.messages[len(m.messages)-1].text; got != "Computer use: enabled. Takes effect in new sessions." {
		t.Fatalf("enable message = %q", got)
	}

	path, err := config.ActiveOcodeConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("saved config missing: %v", err)
	}

	m.handleComputerCmd([]string{"disable"})
	if cfg.Ocode.ComputerUse.Enabled {
		t.Fatal("disable did not update in-memory config")
	}

	m.handleComputerCmd([]string{"status"})
	status := m.messages[len(m.messages)-1].text
	if !strings.Contains(status, "Computer use: disabled") || !strings.Contains(status, "Backend: ") {
		t.Fatalf("status message = %q", status)
	}
}

func TestHandleComputerCmdRejectsUnknownCommand(t *testing.T) {
	m := model{config: &config.Config{}}
	m.handleComputerCmd([]string{"wat"})
	if got := m.messages[len(m.messages)-1].text; got != "Usage: /computer [status|enable|disable]" {
		t.Fatalf("usage message = %q", got)
	}
}
