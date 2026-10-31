package paths

import (
	"os"
	"path/filepath"
	"runtime"
)

// OfficeAddinPath is one well-known Microsoft Office add-in directory offered
// as a permission preset in Settings -> Paths & Uploads.
//
// A preset is only a shortcut for the existing extra_allowed_paths mechanism:
// granting one appends Path to that list, which is the same list the permission
// dialog writes when the user picks "always allow this path". No separate
// permission surface or config key exists for Office.
type OfficeAddinPath struct {
	// App is a stable id ("excel", "word", ...) used as a React key.
	App string `json:"app"`
	// Label is the user-facing application name.
	Label string `json:"label"`
	// Path is the absolute directory to grant.
	Path string `json:"path"`
	// Exists reports whether the directory is present right now. A missing
	// "wef" folder is still grantable; Office creates it on first sideload.
	Exists bool `json:"exists"`
	// Available reports whether the preset can be granted at all, i.e. its
	// parent directory exists. tool.NormalizeRootPath resolves a missing leaf
	// only when the immediate parent exists, so a preset for an uninstalled
	// app would look grantable in the UI but silently never reach the runtime
	// allowlist.
	Available bool `json:"available"`
	// Note carries a platform caveat for this preset, if any.
	Note string `json:"note,omitempty"`
	// Allowed is filled by the server when Path is already present in
	// extra_allowed_paths (after root normalization). The paths package does
	// not read config, so it stays false here.
	Allowed bool `json:"allowed"`
	// AllowedPath is the RAW extra_allowed_paths entry that matched (for
	// example "~/Library/.../wef/" while Path is the expanded absolute form).
	// The Settings form uses it to filter the stored entry out of the free-text
	// field, so a tilde or trailing-slash grant is not shown twice.
	AllowedPath string `json:"allowed_path,omitempty"`
}

// OfficeAddinPaths returns the Office add-in directories known for the
// platform ocode is running on. The Settings form renders these as permission
// presets; an empty result means the platform has no well-known folder (the
// user can still enter one by hand).
func OfficeAddinPaths() []OfficeAddinPath {
	home, _ := os.UserHomeDir()
	return officeAddinPathsFor(runtime.GOOS, home, os.Getenv("APPDATA"))
}

// officeAddinPathsFor is the testable core of OfficeAddinPaths. home and
// appData are passed in so a test can assert the exact paths on any host OS.
func officeAddinPathsFor(goos, home, appData string) []OfficeAddinPath {
	switch goos {
	case "darwin":
		if home == "" {
			return nil
		}
		// Microsoft's documented Office-on-Mac sideload folders: each app
		// reads every manifest placed in its own container's wef directory.
		apps := []struct {
			app    string
			label  string
			bundle string
		}{
			{"excel", "Excel", "com.microsoft.Excel"},
			{"word", "Word", "com.microsoft.Word"},
			{"powerpoint", "PowerPoint", "com.microsoft.Powerpoint"},
			{"outlook", "Outlook", "com.microsoft.Outlook"},
			{"onenote", "OneNote", "com.microsoft.onenote.mac"},
		}
		out := make([]OfficeAddinPath, 0, len(apps))
		for _, a := range apps {
			out = append(out, newOfficeAddinPath(
				a.app,
				a.label,
				filepath.Join(home, "Library", "Containers", a.bundle, "Data", "Documents", "wef"),
				"",
			))
		}
		return out
	case "windows":
		if appData == "" && home != "" {
			appData = filepath.Join(home, "AppData", "Roaming")
		}
		if appData == "" {
			return nil
		}
		// Windows has no per-app wef sideload folder: web add-ins are
		// registered through a trusted shared-folder catalog (or the registry),
		// not copied into Office's cache. Only classic add-in locations are
		// presets; the shared catalog folder is entered by hand.
		note := "Windows sideloads web add-ins through a trusted shared-folder catalog; add that folder below."
		return []OfficeAddinPath{
			newOfficeAddinPath("addins", "Office Add-ins (classic)", filepath.Join(appData, "Microsoft", "AddIns"), note),
			newOfficeAddinPath("word-startup", "Word startup folder", filepath.Join(appData, "Microsoft", "Word", "STARTUP"), ""),
		}
	default:
		return nil
	}
}

func newOfficeAddinPath(app, label, path, note string) OfficeAddinPath {
	return OfficeAddinPath{
		App:       app,
		Label:     label,
		Path:      path,
		Exists:    isExistingDir(path),
		Available: isExistingDir(filepath.Dir(path)),
		Note:      note,
	}
}

func isExistingDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
