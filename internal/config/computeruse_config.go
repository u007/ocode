package config

// ComputerUseConfig holds the persisted configuration for the computer tool.
type ComputerUseConfig struct {
	// Enabled toggles the computer tool on/off. When disabled the tool
	// is not advertised to the model at all (unlike OCR, which is always
	// registered but returns a notice when disabled).
	Enabled bool `json:"enabled"`
}

// DefaultComputerUseConfig returns the default computer use configuration.
func DefaultComputerUseConfig() ComputerUseConfig {
	return ComputerUseConfig{
		Enabled: false,
	}
}

// SaveComputerUseConfig persists the full computer use configuration via
// load-modify-write. Only the computer_use sub-tree is touched; all other
// fields are preserved from disk.
func SaveComputerUseConfig(cfg ComputerUseConfig) error {
	return withOcodeConfigLock(func(ocodeCfg *OcodeConfig) error {
		ocodeCfg.ComputerUse = cfg
		return nil
	})
}
