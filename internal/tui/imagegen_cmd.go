package tui

import (
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/u007/ocode/internal/config"
)

// handleImageCmd implements the /image slash command: show status, toggle the
// imagegen tool on/off, or select the provider/model.
func (m *model) handleImageCmd(args []string) tea.Cmd {
	if m.config == nil {
		m.messages = append(m.messages, message{role: roleAssistant, text: "Image generation requires a configuration. Run /connect first."})
		return nil
	}
	if len(args) == 0 || strings.ToLower(args[0]) == "status" {
		return m.showImageStatus()
	}
	switch strings.ToLower(args[0]) {
	case "enable", "true", "yes", "on":
		m.config.Ocode.ImageGen.Enabled = true
		if err := config.SaveImageGenConfig(m.config.Ocode.ImageGen); err != nil {
			m.messages = append(m.messages, message{role: roleAssistant, text: "Error: " + err.Error()})
			return nil
		}
		m.broadcastTUIStatus()
		m.messages = append(m.messages, message{role: roleAssistant, text: "Image generation: enabled."})
		return nil
	case "disable", "false", "no", "off":
		m.config.Ocode.ImageGen.Enabled = false
		if err := config.SaveImageGenConfig(m.config.Ocode.ImageGen); err != nil {
			m.messages = append(m.messages, message{role: roleAssistant, text: "Error: " + err.Error()})
			return nil
		}
		m.broadcastTUIStatus()
		m.messages = append(m.messages, message{role: roleAssistant, text: "Image generation: disabled."})
		return nil
	case "model":
		if len(args) > 1 {
			return m.handleImageModel(args[1])
		}
		return m.openImageModelPicker()
	case "timeout":
		if len(args) < 2 {
			m.messages = append(m.messages, message{role: roleAssistant, text: "Usage: /image timeout <seconds> (a positive integer)"})
			return nil
		}
		secs, err := strconv.Atoi(strings.TrimSpace(args[1]))
		if err != nil || secs <= 0 {
			m.messages = append(m.messages, message{role: roleAssistant, text: "Usage: /image timeout <seconds> (a positive integer)"})
			return nil
		}
		m.config.Ocode.ImageGen.Timeout = secs
		if err := config.SaveImageGenConfig(m.config.Ocode.ImageGen); err != nil {
			m.messages = append(m.messages, message{role: roleAssistant, text: "Error: " + err.Error()})
			return nil
		}
		m.broadcastTUIStatus()
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Image generation timeout: %d seconds.", secs)})
		return nil
	default:
		m.messages = append(m.messages, message{role: roleAssistant, text: "Usage: /image [status|enable|disable|model [provider/model]|timeout <seconds>]"})
		return nil
	}
}

// handleImageModel sets the imagegen provider + model from a "provider/model"
// spec (or just "model", keeping the current provider).
func (m *model) handleImageModel(spec string) tea.Cmd {
	provider, model := config.SplitProviderModel(spec)
	if provider == "" {
		provider = m.config.Ocode.ImageGen.Provider
		if provider == "" {
			provider = "gemini"
		}
	}
	if err := config.SaveImageGenModel(provider, model); err != nil {
		m.messages = append(m.messages, message{role: roleAssistant, text: "Error: " + err.Error()})
		return nil
	}
	m.config.Ocode.ImageGen.Provider = provider
	m.config.Ocode.ImageGen.Model = model
	m.broadcastTUIStatus()
	m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Image model set: %s/%s", provider, model)})
	return nil
}

// showImageStatus prints the current imagegen configuration.
func (m *model) showImageStatus() tea.Cmd {
	if m.config == nil {
		m.messages = append(m.messages, message{role: roleAssistant, text: "Image generation requires a configuration."})
		return nil
	}
	cfg := m.config.Ocode.ImageGen
	status := "disabled"
	if cfg.Enabled {
		status = "enabled"
	}
	provider := cfg.Provider
	if provider == "" {
		provider = "gemini"
	}
	modelText := "(provider default)"
	if cfg.Model != "" {
		modelText = cfg.Model
	}
	output := "(working directory)"
	if cfg.OutputPath != "" {
		output = cfg.OutputPath
	}
	timeoutText := fmt.Sprintf("%d seconds", cfg.Timeout)
	if cfg.Timeout <= 0 {
		timeoutText = "(built-in default)"
	}
	m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf(
		"Image generation: %s\nProvider: %s\nModel: %s\nOutput: %s\nTimeout: %s\n\nUse /image enable/disable to toggle, /image model to pick a model, or /image timeout <seconds> to set the request timeout.",
		status, provider, modelText, output, timeoutText)})
	return nil
}
