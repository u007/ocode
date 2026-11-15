package config

// VideoGenConfig holds the persisted configuration for the videogen tool.
type VideoGenConfig struct {
	// Enabled toggles the videogen tool on/off. When disabled, Execute
	// returns a NoticedError directing the user to /video enable.
	Enabled bool `json:"enabled"`
	// Model is the Gemini video model id. Empty means the provider default
	// (gemini-omni-1.2-flash).
	Model string `json:"model"`
	// OutputPath is the default directory/file to save generated videos.
	// Empty means the working directory with an auto-generated name.
	OutputPath string `json:"output_path,omitempty"`
	// Timeout is the per-request timeout in seconds. 0 means the built-in
	// default (see DefaultVideoGenConfig).
	Timeout int `json:"timeout,omitempty"`
	// AspectRatio is the default output aspect ratio ("16:9" or "9:16").
	AspectRatio string `json:"aspect_ratio,omitempty"`
	// Resolution is the default output resolution (360p, 720p, 1080p, 4k).
	Resolution string `json:"resolution,omitempty"`
}

// DefaultVideoGenConfig returns a sensible default videogen configuration.
func DefaultVideoGenConfig() VideoGenConfig {
	return VideoGenConfig{
		Enabled:     false,
		Timeout:     900,
		AspectRatio: "16:9",
		Resolution:  "720p",
	}
}

// SaveVideoGenConfig persists the full videogen configuration via
// load-modify-write. Only the videogen sub-tree is touched.
func SaveVideoGenConfig(cfg VideoGenConfig) error {
	return withOcodeConfigLock(func(c *OcodeConfig) error {
		c.VideoGen = cfg
		return nil
	})
}

// AudioGenConfig holds the persisted configuration for the audiogen tool.
type AudioGenConfig struct {
	// Enabled toggles the audiogen tool on/off. When disabled, Execute
	// returns a NoticedError directing the user to /audio enable.
	Enabled bool `json:"enabled"`
	// Model is the Gemini TTS model id. Empty means the provider default
	// (gemini-3.8-flash-tts).
	Model string `json:"model"`
	// Voice is the default prebuilt voice name for single-speaker output.
	Voice string `json:"voice,omitempty"`
	// OutputPath is the default directory/file to save generated audio.
	// Empty means the working directory with an auto-generated name.
	OutputPath string `json:"output_path,omitempty"`
	// Timeout is the per-request timeout in seconds. 0 means the built-in
	// default (see DefaultAudioGenConfig).
	Timeout int `json:"timeout,omitempty"`
}

// DefaultAudioGenConfig returns a sensible default audiogen configuration.
func DefaultAudioGenConfig() AudioGenConfig {
	return AudioGenConfig{
		Enabled: false,
		Voice:   "Kore",
		Timeout: 300,
	}
}

// SaveAudioGenConfig persists the full audiogen configuration via
// load-modify-write. Only the audiogen sub-tree is touched.
func SaveAudioGenConfig(cfg AudioGenConfig) error {
	return withOcodeConfigLock(func(c *OcodeConfig) error {
		c.AudioGen = cfg
		return nil
	})
}
