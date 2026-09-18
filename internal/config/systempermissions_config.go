package config

import "github.com/u007/ocode/internal/sysperm"

// SystemPermissionsConfig holds the persisted intent for the OS permission
// manager (Settings → System Permissions). Only user intent is stored: which
// areas are enabled and any custom paths that were added. Live OS grant status
// is never persisted, because macOS TCC grants are keyed to the code signature
// and are invalidated by a rebuild — the desktop shell reconciles them at
// startup instead.
//
// The type lives in internal/sysperm (stdlib-only) and config imports it, so
// the package stays free of a config dependency.
type SystemPermissionsConfig = sysperm.Config

// SaveSystemPermissionsConfig persists the system-permissions sub-tree via
// load-modify-write; all other ocodeconfig.json fields are preserved.
func SaveSystemPermissionsConfig(cfg SystemPermissionsConfig) error {
	return withOcodeConfigLock(func(c *OcodeConfig) error {
		c.SystemPermissions = cfg
		return nil
	})
}
