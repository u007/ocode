package config

// LoadEffectiveForWindow returns the effective Config and OcodeConfig for the
// given windowID, merging the base with the window's active profile (or
// OCODE_PROFILE env if set). It also returns the effective profile name used
// (empty for Default). Errors from base config load are returned; missing
// profile falls back to base.
func LoadEffectiveForWindow(windowID string) (*Config, *OcodeConfig, string, error) {
	baseCfg, err := Load()
	if err != nil {
		return nil, nil, "", err
	}
	ocodeCfg, err := loadFullOcodeConfig()
	if err != nil {
		return nil, nil, "", err
	}
	profile := GetEffectiveActiveProfile(windowID)
	if profile != "" {
		if _, ok := ocodeCfg.Profiles[profile]; !ok {
			// profile not found — treat as Default, don't fail
			profile = ""
		}
	}
	effCfg := EffectiveConfig(baseCfg, profile, ocodeCfg.Profiles)
	effOcode := EffectiveOcodeConfig(ocodeCfg, profile)
	return effCfg, effOcode, profile, nil
}

// LoadEffectiveForProfile returns effective configs for an explicit profile name
// (ignoring window state and env). Used by CLI --profile handling.
func LoadEffectiveForProfile(profile string) (*Config, *OcodeConfig, error) {
	baseCfg, err := Load()
	if err != nil {
		return nil, nil, err
	}
	ocodeCfg, err := loadFullOcodeConfig()
	if err != nil {
		return nil, nil, err
	}
	if profile != "" {
		if _, ok := ocodeCfg.Profiles[profile]; !ok {
			return nil, nil, errProfileNotFound(profile)
		}
	}
	effCfg := EffectiveConfig(baseCfg, profile, ocodeCfg.Profiles)
	effOcode := EffectiveOcodeConfig(ocodeCfg, profile)
	return effCfg, effOcode, nil
}

func errProfileNotFound(name string) error {
	return errProfileNotFoundType{name}
}

type errProfileNotFoundType struct{ name string }

func (e errProfileNotFoundType) Error() string { return "profile " + e.name + " not found" }

func IsProfileNotFound(err error) bool {
	_, ok := err.(errProfileNotFoundType)
	return ok
}
