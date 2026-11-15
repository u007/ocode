package tui

import (
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/u007/ocode/internal/auth"
	"github.com/u007/ocode/internal/config"
)

// handleVideoCmd implements the /video slash command: show status, toggle the
// videogen tool on/off, or edit its model, aspect ratio, resolution and timeout.
func (m *model) handleVideoCmd(args []string) tea.Cmd {
	if m.config == nil {
		m.messages = append(m.messages, message{role: roleAssistant, text: "Video generation requires a configuration. Run /connect first."})
		return nil
	}
	if len(args) == 0 || strings.ToLower(args[0]) == "status" {
		return m.showVideoStatus()
	}
	switch strings.ToLower(args[0]) {
	case "enable", "true", "yes", "on":
		m.config.Ocode.VideoGen.Enabled = true
		if err := config.SaveVideoGenConfig(m.config.Ocode.VideoGen); err != nil {
			m.messages = append(m.messages, message{role: roleAssistant, text: "Error: " + err.Error()})
			return nil
		}
		m.broadcastTUIStatus()
		m.messages = append(m.messages, message{role: roleAssistant, text: "Video generation: enabled."})
		return nil
	case "disable", "false", "no", "off":
		m.config.Ocode.VideoGen.Enabled = false
		if err := config.SaveVideoGenConfig(m.config.Ocode.VideoGen); err != nil {
			m.messages = append(m.messages, message{role: roleAssistant, text: "Error: " + err.Error()})
			return nil
		}
		m.broadcastTUIStatus()
		m.messages = append(m.messages, message{role: roleAssistant, text: "Video generation: disabled."})
		return nil
	case "model":
		if len(args) < 2 {
			m.messages = append(m.messages, message{role: roleAssistant, text: "Usage: /video model <model-id> (or /video model default)"})
			return nil
		}
		model := args[1]
		if strings.EqualFold(model, "default") || model == "-" {
			model = ""
		}
		m.config.Ocode.VideoGen.Model = model
		if err := config.SaveVideoGenConfig(m.config.Ocode.VideoGen); err != nil {
			m.messages = append(m.messages, message{role: roleAssistant, text: "Error: " + err.Error()})
			return nil
		}
		m.broadcastTUIStatus()
		m.messages = append(m.messages, message{role: roleAssistant, text: "Video model set: " + describeMediaModel(model, "gemini-omni-1.2-flash")})
		return nil
	case "aspect", "aspect-ratio":
		return m.setVideoGenOption(args, "aspect_ratio")
	case "resolution":
		return m.setVideoGenOption(args, "resolution")
	case "timeout":
		return m.setMediaGenTimeout(args, "Video", func(secs int) {
			m.config.Ocode.VideoGen.Timeout = secs
			_ = config.SaveVideoGenConfig(m.config.Ocode.VideoGen)
		})
	default:
		m.messages = append(m.messages, message{role: roleAssistant, text: "Usage: /video [status|enable|disable|model <id>|aspect 16:9|9:16|resolution 360p|720p|1080p|4k|timeout <seconds>]"})
		return nil
	}
}

// setVideoGenOption applies an aspect_ratio/resolution change and rejects
// unsupported values before they reach the API.
func (m *model) setVideoGenOption(args []string, field string) tea.Cmd {
	usage := "Usage: /video aspect 16:9|9:16"
	allowed := map[string]bool{"16:9": true, "9:16": true}
	if field == "resolution" {
		usage = "Usage: /video resolution 360p|720p|1080p|4k"
		allowed = map[string]bool{"360p": true, "720p": true, "1080p": true, "4k": true}
	}
	if len(args) < 2 {
		m.messages = append(m.messages, message{role: roleAssistant, text: usage})
		return nil
	}
	value := strings.ToLower(strings.TrimSpace(args[1]))
	if !allowed[value] {
		m.messages = append(m.messages, message{role: roleAssistant, text: usage})
		return nil
	}
	if field == "aspect_ratio" {
		m.config.Ocode.VideoGen.AspectRatio = value
	} else {
		m.config.Ocode.VideoGen.Resolution = value
	}
	if err := config.SaveVideoGenConfig(m.config.Ocode.VideoGen); err != nil {
		m.messages = append(m.messages, message{role: roleAssistant, text: "Error: " + err.Error()})
		return nil
	}
	m.broadcastTUIStatus()
	m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Video %s set: %s", strings.ReplaceAll(field, "_", " "), value)})
	return nil
}

// handleAudioCmd implements the /audio slash command for the audiogen tool.
func (m *model) handleAudioCmd(args []string) tea.Cmd {
	if m.config == nil {
		m.messages = append(m.messages, message{role: roleAssistant, text: "Audio generation requires a configuration. Run /connect first."})
		return nil
	}
	if len(args) == 0 || strings.ToLower(args[0]) == "status" {
		return m.showAudioStatus()
	}
	switch strings.ToLower(args[0]) {
	case "enable", "true", "yes", "on":
		m.config.Ocode.AudioGen.Enabled = true
		if err := config.SaveAudioGenConfig(m.config.Ocode.AudioGen); err != nil {
			m.messages = append(m.messages, message{role: roleAssistant, text: "Error: " + err.Error()})
			return nil
		}
		m.broadcastTUIStatus()
		m.messages = append(m.messages, message{role: roleAssistant, text: "Audio generation: enabled."})
		return nil
	case "disable", "false", "no", "off":
		m.config.Ocode.AudioGen.Enabled = false
		if err := config.SaveAudioGenConfig(m.config.Ocode.AudioGen); err != nil {
			m.messages = append(m.messages, message{role: roleAssistant, text: "Error: " + err.Error()})
			return nil
		}
		m.broadcastTUIStatus()
		m.messages = append(m.messages, message{role: roleAssistant, text: "Audio generation: disabled."})
		return nil
	case "model":
		if len(args) < 2 {
			m.messages = append(m.messages, message{role: roleAssistant, text: "Usage: /audio model <model-id> (or /audio model default)"})
			return nil
		}
		model := args[1]
		if strings.EqualFold(model, "default") || model == "-" {
			model = ""
		}
		m.config.Ocode.AudioGen.Model = model
		if err := config.SaveAudioGenConfig(m.config.Ocode.AudioGen); err != nil {
			m.messages = append(m.messages, message{role: roleAssistant, text: "Error: " + err.Error()})
			return nil
		}
		m.broadcastTUIStatus()
		m.messages = append(m.messages, message{role: roleAssistant, text: "Audio model set: " + describeMediaModel(model, "gemini-3.8-flash-tts")})
		return nil
	case "voice":
		if len(args) < 2 {
			m.messages = append(m.messages, message{role: roleAssistant, text: "Usage: /audio voice <voice-name> (e.g. Kore, Puck)"})
			return nil
		}
		m.config.Ocode.AudioGen.Voice = args[1]
		if err := config.SaveAudioGenConfig(m.config.Ocode.AudioGen); err != nil {
			m.messages = append(m.messages, message{role: roleAssistant, text: "Error: " + err.Error()})
			return nil
		}
		m.broadcastTUIStatus()
		m.messages = append(m.messages, message{role: roleAssistant, text: "Audio voice set: " + args[1]})
		return nil
	case "timeout":
		return m.setMediaGenTimeout(args, "Audio", func(secs int) {
			m.config.Ocode.AudioGen.Timeout = secs
			_ = config.SaveAudioGenConfig(m.config.Ocode.AudioGen)
		})
	default:
		m.messages = append(m.messages, message{role: roleAssistant, text: "Usage: /audio [status|enable|disable|model <id>|voice <name>|timeout <seconds>]"})
		return nil
	}
}

// setMediaGenTimeout parses, applies and reports a video/audio timeout change.
func (m *model) setMediaGenTimeout(args []string, label string, apply func(int)) tea.Cmd {
	if len(args) < 2 {
		m.messages = append(m.messages, message{role: roleAssistant, text: "Usage: /" + strings.ToLower(label) + " timeout <seconds> (a positive integer)"})
		return nil
	}
	secs, err := strconv.Atoi(strings.TrimSpace(args[1]))
	if err != nil || secs <= 0 {
		m.messages = append(m.messages, message{role: roleAssistant, text: "Usage: /" + strings.ToLower(label) + " timeout <seconds> (a positive integer)"})
		return nil
	}
	apply(secs)
	m.broadcastTUIStatus()
	m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("%s generation timeout: %d seconds.", label, secs)})
	return nil
}

func describeMediaModel(model, defaultModel string) string {
	if model == "" {
		return "default (" + defaultModel + ")"
	}
	return model
}

func (m *model) showVideoStatus() tea.Cmd {
	cfg := m.config.Ocode.VideoGen
	state := "disabled"
	if cfg.Enabled {
		state = "enabled"
	}
	m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf(
		"Video generation: %s\n  model: %s\n  aspect: %s\n  resolution: %s\n  timeout: %ds\n  key: %s",
		state,
		describeMediaModel(cfg.Model, "gemini-omni-1.2-flash"),
		firstNonEmpty(cfg.AspectRatio, "16:9"),
		firstNonEmpty(cfg.Resolution, "720p"),
		firstNonZero(cfg.Timeout, 900),
		mediaKeyStatus(),
	)})
	return nil
}

func (m *model) showAudioStatus() tea.Cmd {
	cfg := m.config.Ocode.AudioGen
	state := "disabled"
	if cfg.Enabled {
		state = "enabled"
	}
	m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf(
		"Audio generation: %s\n  model: %s\n  voice: %s\n  timeout: %ds\n  key: %s",
		state,
		describeMediaModel(cfg.Model, "gemini-3.8-flash-tts"),
		firstNonEmpty(cfg.Voice, "Kore"),
		firstNonZero(cfg.Timeout, 300),
		mediaKeyStatus(),
	)})
	return nil
}

// mediaKeyStatus reports whether a Gemini API key is resolvable, so /video and
// /audio status explain a failure before the first generation attempt.
func mediaKeyStatus() string {
	_ = auth.LoadStore()
	// Provider id is "google"; "gemini" is only the tool-facing alias.
	if auth.ResolveKey("google") != "" {
		return "GOOGLE_API_KEY / auth store (found)"
	}
	return "missing — set GOOGLE_API_KEY or run /connect"
}

func firstNonEmpty(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func firstNonZero(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}
