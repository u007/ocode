package tui

import (
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
)

func TestHandleVideoEnableStatusAndOptions(t *testing.T) {
	m := newImageTestModel(t)
	m.config.Ocode.VideoGen = config.DefaultVideoGenConfig()

	if m.config.Ocode.VideoGen.Enabled {
		t.Fatal("default should be disabled")
	}
	m.handleVideoCmd([]string{"enable"})
	if !m.config.Ocode.VideoGen.Enabled {
		t.Error("enable did not set Enabled")
	}

	m.handleVideoCmd([]string{"aspect", "9:16"})
	if m.config.Ocode.VideoGen.AspectRatio != "9:16" {
		t.Errorf("aspect not set: %q", m.config.Ocode.VideoGen.AspectRatio)
	}
	m.handleVideoCmd([]string{"resolution", "1080p"})
	if m.config.Ocode.VideoGen.Resolution != "1080p" {
		t.Errorf("resolution not set: %q", m.config.Ocode.VideoGen.Resolution)
	}

	// Unsupported values are rejected and leave the config unchanged.
	m.handleVideoCmd([]string{"aspect", "4:3"})
	if m.config.Ocode.VideoGen.AspectRatio != "9:16" {
		t.Errorf("invalid aspect mutated config: %q", m.config.Ocode.VideoGen.AspectRatio)
	}
	if !strings.Contains(lastMsg(m), "Usage: /video") {
		t.Errorf("invalid aspect message = %q", lastMsg(m))
	}
	m.handleVideoCmd([]string{"resolution", "8k"})
	if m.config.Ocode.VideoGen.Resolution != "1080p" {
		t.Errorf("invalid resolution mutated config: %q", m.config.Ocode.VideoGen.Resolution)
	}

	m.handleVideoCmd([]string{"model", "custom-video"})
	if m.config.Ocode.VideoGen.Model != "custom-video" {
		t.Errorf("model not set: %q", m.config.Ocode.VideoGen.Model)
	}
	m.handleVideoCmd([]string{"model", "default"})
	if m.config.Ocode.VideoGen.Model != "" {
		t.Errorf("default did not clear model: %q", m.config.Ocode.VideoGen.Model)
	}

	m.handleVideoCmd([]string{"timeout", "1200"})
	if m.config.Ocode.VideoGen.Timeout != 1200 {
		t.Errorf("timeout not set: %d", m.config.Ocode.VideoGen.Timeout)
	}
	m.handleVideoCmd([]string{"timeout", "0"})
	if m.config.Ocode.VideoGen.Timeout != 1200 {
		t.Errorf("invalid timeout mutated config: %d", m.config.Ocode.VideoGen.Timeout)
	}

	m.handleVideoCmd(nil)
	if !strings.Contains(lastMsg(m), "Video generation:") {
		t.Errorf("status message = %q", lastMsg(m))
	}
	m.handleVideoCmd([]string{"bogus"})
	if !strings.Contains(lastMsg(m), "Usage: /video") {
		t.Errorf("usage message = %q", lastMsg(m))
	}
}

func TestHandleAudioEnableStatusAndOptions(t *testing.T) {
	m := newImageTestModel(t)
	m.config.Ocode.AudioGen = config.DefaultAudioGenConfig()

	m.handleAudioCmd([]string{"enable"})
	if !m.config.Ocode.AudioGen.Enabled {
		t.Error("enable did not set Enabled")
	}
	m.handleAudioCmd([]string{"voice", "Puck"})
	if m.config.Ocode.AudioGen.Voice != "Puck" {
		t.Errorf("voice not set: %q", m.config.Ocode.AudioGen.Voice)
	}
	m.handleAudioCmd([]string{"model", "custom-tts"})
	if m.config.Ocode.AudioGen.Model != "custom-tts" {
		t.Errorf("model not set: %q", m.config.Ocode.AudioGen.Model)
	}
	m.handleAudioCmd([]string{"timeout", "90"})
	if m.config.Ocode.AudioGen.Timeout != 90 {
		t.Errorf("timeout not set: %d", m.config.Ocode.AudioGen.Timeout)
	}

	m.handleAudioCmd(nil)
	if !strings.Contains(lastMsg(m), "Audio generation:") {
		t.Errorf("status message = %q", lastMsg(m))
	}
	m.handleAudioCmd([]string{"voice"})
	if !strings.Contains(lastMsg(m), "Usage: /audio voice") {
		t.Errorf("missing voice message = %q", lastMsg(m))
	}
	m.handleAudioCmd([]string{"bogus"})
	if !strings.Contains(lastMsg(m), "Usage: /audio") {
		t.Errorf("usage message = %q", lastMsg(m))
	}
}
