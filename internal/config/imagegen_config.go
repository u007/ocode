package config

// ImageGenConfig holds the persisted configuration for the imagegen tool.
type ImageGenConfig struct {
	// Enabled toggles the imagegen tool on/off. When disabled, the tool's
	// Execute returns a NoticedError directing the user to /image enable.
	Enabled bool `json:"enabled"`
	// Provider selects the active backend: "gemini" (default), "openai",
	// "novita", "deepinfra", or any OpenAI-compatible endpoint via base_url.
	Provider string `json:"provider"`
	// Model is the model id. Empty means use the provider default
	// (e.g. gemini-3.1-flash-image for gemini).
	Model string `json:"model"`
	// OutputPath is the default directory/file to save generated images.
	// Empty means the working directory with an auto-generated name.
	OutputPath string `json:"output_path,omitempty"`
	// Timeout is the per-request timeout in seconds for image generation.
	// 0 means use the built-in default (see DefaultImageGenConfig).
	Timeout int `json:"timeout,omitempty"`
}

// DefaultImageGenConfig returns a sensible default imagegen configuration.
func DefaultImageGenConfig() ImageGenConfig {
	return ImageGenConfig{
		Enabled:  false,
		Provider: "gemini",
		Model:    "",
		// 15 minutes: image generation can be slow for high-quality or
		// multi-image requests, so allow generous headroom over the old
		// 10-minute built-in default.
		Timeout: 900,
	}
}

// SaveImageGenConfig persists the full imagegen configuration via
// load-modify-write. Only the imagegen sub-tree is touched; all other fields
// are preserved from disk.
func SaveImageGenConfig(cfg ImageGenConfig) error {
	return withOcodeConfigLock(func(c *OcodeConfig) error {
		c.ImageGen = cfg
		return nil
	})
}

// SaveImageGenEnabled persists just the imagegen enabled/disabled state.
func SaveImageGenEnabled(enabled bool) error {
	return withOcodeConfigLock(func(c *OcodeConfig) error {
		c.ImageGen.Enabled = enabled
		return nil
	})
}

// SaveImageGenModel persists the provider + model selection. An empty provider
// keeps the currently configured provider.
func SaveImageGenModel(provider, model string) error {
	return withOcodeConfigLock(func(c *OcodeConfig) error {
		if provider != "" {
			c.ImageGen.Provider = provider
		}
		c.ImageGen.Model = model
		return nil
	})
}
