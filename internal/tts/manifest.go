// Package tts contains the server-side speech playback boundary.
//
// Local engine manifests are deliberately data, not guessed launch commands.
// An engine is advertised as ready only when a later change supplies pinned
// runtime/model artifacts and their checksums. Browser Native is the only
// enabled engine in the initial implementation.
package tts

import (
	"fmt"
	"runtime"
	"unicode/utf8"
)

type EngineID string

const (
	EngineBrowserNative EngineID = "browser-native"
	EnginePiper         EngineID = "piper"
	EngineKokoro        EngineID = "kokoro"
	EngineFishAudio     EngineID = "fish-audio"
	EngineBreeze        EngineID = "breeze"
)

type PlaybackMode string

const (
	PlaybackManual   PlaybackMode = "manual"
	PlaybackAtBottom PlaybackMode = "at-bottom"
)

type Availability string

const (
	AvailabilityReady       Availability = "ready"
	AvailabilityUnavailable Availability = "unavailable"
)

type Engine struct {
	ID           EngineID     `json:"id"`
	Label        string       `json:"label"`
	Availability Availability `json:"availability"`
	Reason       string       `json:"reason,omitempty"`
	BrowserOnly  bool         `json:"browser_only"`
	VoiceID      string       `json:"voice_id,omitempty"`
}

type Config struct {
	Engine EngineID     `json:"engine"`
	Voice  string       `json:"voice"`
	Mode   PlaybackMode `json:"mode"`
}

const maxVoiceIDRunes = 128

// ValidateVoiceID validates the user-supplied voice/model identifier before it
// is persisted or used as a cache path component. Empty is valid for engines
// that choose their default voice.
func ValidateVoiceID(voice string) error {
	if voice == "" {
		return nil
	}
	if !utf8.ValidString(voice) {
		return fmt.Errorf("voice must be valid UTF-8")
	}
	if utf8.RuneCountInString(voice) > maxVoiceIDRunes {
		return fmt.Errorf("voice must be at most %d characters", maxVoiceIDRunes)
	}
	for _, r := range voice {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			continue
		}
		return fmt.Errorf("voice may contain only letters, numbers, hyphens, underscores, and periods")
	}
	return nil
}

func DefaultConfig() Config {
	return Config{Engine: EngineBrowserNative, Mode: PlaybackManual}
}

func Catalog() []Engine {
	return []Engine{
		{ID: EngineBrowserNative, Label: "Browser Native", Availability: AvailabilityReady, BrowserOnly: true},
		{ID: EnginePiper, Label: "Piper", Availability: AvailabilityUnavailable, Reason: "No pinned, redistributable runtime and voice manifest is approved for this platform."},
		{ID: EngineKokoro, Label: "Kokoro", Availability: AvailabilityUnavailable, Reason: "No pinned cross-platform runtime/export and voice manifest is verified."},
		{ID: EngineFishAudio, Label: "Fish Audio", Availability: AvailabilityUnavailable, Reason: "Runtime packaging and Fish Audio Research License terms are not approved for managed distribution."},
		{ID: EngineBreeze, Label: "Breeze", Availability: AvailabilityUnavailable, Reason: "Breeze model licensing and a pinned runtime/model manifest require product approval."},
	}
}

func EngineForHost(id EngineID) (Engine, bool) {
	for _, engine := range Catalog() {
		if engine.ID == id {
			return engine, true
		}
	}
	return Engine{}, false
}

func Host() string {
	return runtime.GOOS + "/" + runtime.GOARCH
}
