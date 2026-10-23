package config

import (
	"encoding/json"
	"fmt"
)

// GlobalOcodeConfigPath is the path of the global ocodeconfig.json.
func GlobalOcodeConfigPath() (string, error) {
	return getGlobalOcodeConfigPath()
}

// ParsePermissionRules decodes the rule tables (tools and bash) from the
// "permissions" block of an ocodeconfig.json or .ocode/settings.json body. Mode
// and the auto-permission block are deliberately not returned: they are
// per-session state with their own update paths, not shared file rules.
func ParsePermissionRules(data []byte) (PermissionConfig, error) {
	var file struct {
		Permissions permissionConfigFile `json:"permissions"`
	}
	if err := json.Unmarshal(stripJSONCComments(data), &file); err != nil {
		return PermissionConfig{}, fmt.Errorf("parse permission rules: %w", err)
	}
	return PermissionConfig{
		Tools: file.Permissions.Tools,
		Bash:  file.Permissions.Bash,
	}, nil
}
