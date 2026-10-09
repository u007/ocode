package tui

import (
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
)

// autoShareLastText returns the most recent assistant message, i.e. what the
// user actually sees echoed back after running the command.
func autoShareLastText(m *model) string {
	for i := len(m.messages) - 1; i >= 0; i-- {
		if m.messages[i].role == roleAssistant {
			return m.messages[i].text
		}
	}
	return ""
}

// TestAutoShareCmdDefaultsOffAndPersists covers the positive case: an explicit
// `on` must actually land in the config file. Without this the whole feature
// could be dead while every "defaults off" test still passed.
func TestAutoShareCmdDefaultsOffAndPersists(t *testing.T) {
	t.Setenv("OPENCODE_CONFIG_DIR", t.TempDir())

	cfg := config.Config{}
	m := &model{config: &cfg}

	if got := autoShareLastText(&model{config: &cfg}); got != "" {
		t.Fatalf("fresh model should have no messages, got %q", got)
	}

	runAutoShareCmd(m, []string{"on"})
	if !strings.Contains(autoShareLastText(m), "Auto share on start: on") {
		t.Fatalf("after on: %q", autoShareLastText(m))
	}

	persisted, err := config.LoadOcodeConfigCopy()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !persisted.AutoShareOnStart {
		t.Fatal("/auto-share on did not persist to disk")
	}
	if !m.config.Ocode.AutoShareOnStart {
		t.Fatal("/auto-share on did not update the in-memory config")
	}
}

// TestAutoShareCmdTogglesBothWays covers the bare-argument form and off.
func TestAutoShareCmdTogglesBothWays(t *testing.T) {
	t.Setenv("OPENCODE_CONFIG_DIR", t.TempDir())

	cfg := config.Config{}
	m := &model{config: &cfg}

	runAutoShareCmd(m, nil) // bare = toggle
	if !m.config.Ocode.AutoShareOnStart {
		t.Fatal("bare /auto-share from off should turn it on")
	}
	if !strings.Contains(autoShareLastText(m), ": on") {
		t.Fatalf("bare toggle message: %q", autoShareLastText(m))
	}

	runAutoShareCmd(m, nil)
	if m.config.Ocode.AutoShareOnStart {
		t.Fatal("second bare /auto-share should turn it off")
	}
	if !strings.Contains(autoShareLastText(m), ": off") {
		t.Fatalf("second toggle message: %q", autoShareLastText(m))
	}
}

// TestAutoShareCmdStatusDoesNotMutate pins that `status` is read-only — a
// status query must never flip the setting.
func TestAutoShareCmdStatusDoesNotMutate(t *testing.T) {
	t.Setenv("OPENCODE_CONFIG_DIR", t.TempDir())

	cfg := config.Config{}
	m := &model{config: &cfg}

	runAutoShareCmd(m, []string{"status"})
	if m.config.Ocode.AutoShareOnStart {
		t.Fatal("status must not enable auto-share")
	}
	if !strings.Contains(autoShareLastText(m), ": off") {
		t.Fatalf("status message: %q", autoShareLastText(m))
	}

	runAutoShareCmd(m, []string{"on"})
	runAutoShareCmd(m, []string{"status"})
	if !m.config.Ocode.AutoShareOnStart {
		t.Fatal("status must not disable auto-share")
	}
	if !strings.Contains(autoShareLastText(m), ": on") {
		t.Fatalf("status message when on: %q", autoShareLastText(m))
	}
}

// TestAutoShareCmdRejectsUnknownArg keeps a typo from silently meaning "off".
func TestAutoShareCmdRejectsUnknownArg(t *testing.T) {
	t.Setenv("OPENCODE_CONFIG_DIR", t.TempDir())

	// Start from ON so a mutation that writes the default (false) for an
	// unrecognised verb is visible; starting from OFF would make this assertion
	// pass no matter what the handler wrote.
	cfg := config.Config{}
	m := &model{config: &cfg}
	runAutoShareCmd(m, []string{"on"})
	before := len(m.messages)

	runAutoShareCmd(m, []string{"maybe"})

	got := ""
	for _, msg := range m.messages[before:] {
		if msg.role == roleAssistant {
			got = msg.text
		}
	}
	if !strings.Contains(got, "Usage: /auto-share") {
		t.Fatalf("expected a usage line, got %q", got)
	}
	if m.config.Ocode.AutoShareOnStart != true {
		t.Fatal("an unknown argument must not change the setting")
	}
	persisted, err := config.LoadOcodeConfigCopy()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !persisted.AutoShareOnStart {
		t.Fatal("an unknown argument must not change the persisted setting")
	}
}
