package tui

import (
	"strings"
	"testing"
)

// Tests for the /docs and /doc-mode slash command.

func TestDocsCommandRegistered(t *testing.T) {
	foundDoc := false
	foundDocMode := false
	for _, spec := range commandSpecs {
		if spec.name == "/docs" {
			foundDoc = true
			if spec.handler == nil {
				t.Error("/docs has no handler")
			}
		}
		for _, alias := range spec.aliases {
			if alias == "/doc-mode" {
				foundDocMode = true
			}
		}
	}
	if !foundDoc {
		t.Error("/docs not found in commandSpecs")
	}
	if !foundDocMode {
		t.Error("/doc-mode not found as an alias in commandSpecs")
	}
}

func TestDocsCommandAlias(t *testing.T) {
	var docsSpec *commandSpec
	for i, spec := range commandSpecs {
		if spec.name == "/docs" {
			docsSpec = &commandSpecs[i]
			break
		}
	}
	if docsSpec == nil {
		t.Fatal("/docs not found in commandSpecs")
	}
	hasAlias := false
	for _, a := range docsSpec.aliases {
		if a == "/doc-mode" {
			hasAlias = true
			break
		}
	}
	if !hasAlias {
		t.Error("/docs should have /doc-mode as an alias")
	}
}

func TestHandleDocsCmd_status(t *testing.T) {
	m := model{
		ready:  true,
		width:  120,
		height: 40,
		input:  newTestTextarea(),
	}

	// With no args — should show status.
	m.messages = nil
	cmd := m.handleDocsCmd([]string{})
	if cmd != nil {
		t.Error("handleDocsCmd with no args should return nil cmd")
	}

	// With "status" arg.
	m.messages = nil
	cmd = m.handleDocsCmd([]string{"status"})
	if cmd != nil {
		t.Error(`handleDocsCmd(["status"]) should return nil cmd`)
	}
}

func TestHandleDocsCmd_on(t *testing.T) {
	m := model{
		ready:  true,
		width:  120,
		height: 40,
		input:  newTestTextarea(),
	}

	m.messages = nil
	cmd := m.handleDocsCmd([]string{"on"})
	if cmd != nil {
		t.Error(`handleDocsCmd(["on"]) should return nil cmd`)
	}
}

func TestHandleDocsCmd_off(t *testing.T) {
	m := model{
		ready:  true,
		width:  120,
		height: 40,
		input:  newTestTextarea(),
	}

	m.messages = nil
	cmd := m.handleDocsCmd([]string{"off"})
	if cmd != nil {
		t.Error(`handleDocsCmd(["off"]) should return nil cmd`)
	}
}

func TestHandleDocsCmd_unknownArg(t *testing.T) {
	m := model{
		ready:  true,
		width:  120,
		height: 40,
		input:  newTestTextarea(),
	}

	m.messages = nil
	cmd := m.handleDocsCmd([]string{"bogus"})
	if cmd != nil {
		t.Error(`handleDocsCmd(["bogus"]) should return nil cmd`)
	}
	if len(m.messages) == 0 {
		t.Fatal("expected usage message for unknown arg")
	}
	if !strings.Contains(m.messages[0].text, "Usage:") {
		t.Errorf("message should contain 'Usage:', got %q", m.messages[0].text)
	}
}

func TestHandleDocsCmd_variants(t *testing.T) {
	// All the "on" variants should work.
	for _, variant := range []string{"true", "yes", "enable"} {
		m := model{
			ready:  true,
			width:  120,
			height: 40,
			input:  newTestTextarea(),
		}
		m.messages = nil
		cmd := m.handleDocsCmd([]string{variant})
		if cmd != nil {
			t.Errorf(`handleDocsCmd(%q) should return nil cmd`, variant)
		}
	}

	// All the "off" variants should work.
	for _, variant := range []string{"false", "no", "disable"} {
		m := model{
			ready:  true,
			width:  120,
			height: 40,
			input:  newTestTextarea(),
		}
		m.messages = nil
		cmd := m.handleDocsCmd([]string{variant})
		if cmd != nil {
			t.Errorf(`handleDocsCmd(%q) should return nil cmd`, variant)
		}
	}
}
