package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/u007/ocode/internal/paths"
)

type windowStateFile struct {
	Windows map[string]windowProfile `json:"windows"`
}

type windowProfile struct {
	ActiveProfile string `json:"activeProfile"`
}

func windowStatePath() (string, error) {
	base, err := paths.OcodeGlobalDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "window-state.json"), nil
}

func lockWindowState() (func(), error) {
	path, err := windowStatePath()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	lockPath := path + ".lock"
	deadline := time.Now().Add(5 * time.Second)
	for {
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			f.Close()
			return func() { os.Remove(lockPath) }, nil
		}
		if !os.IsExist(err) {
			return nil, err
		}
		if info, statErr := os.Stat(lockPath); statErr == nil && time.Since(info.ModTime()) > 10*time.Second {
			os.Remove(lockPath)
			continue
		}
		if time.Now().After(deadline) {
			return func() {}, nil
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func loadWindowState() (windowStateFile, error) {
	path, err := windowStatePath()
	if err != nil {
		return windowStateFile{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return windowStateFile{Windows: make(map[string]windowProfile)}, nil
		}
		return windowStateFile{}, err
	}
	if len(data) == 0 {
		return windowStateFile{Windows: make(map[string]windowProfile)}, nil
	}
	var st windowStateFile
	if err := json.Unmarshal(data, &st); err != nil {
		return windowStateFile{}, fmt.Errorf("parse window-state: %w", err)
	}
	if st.Windows == nil {
		st.Windows = make(map[string]windowProfile)
	}
	return st, nil
}

func writeWindowState(st windowStateFile) error {
	path, err := windowStatePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// GetActiveProfile returns the stored active profile for a window. Empty means Default.
// It does NOT consider OCODE_PROFILE env — callers that need effective should check env first.
func GetActiveProfile(windowID string) (string, error) {
	if windowID == "" {
		return "", nil
	}
	st, err := loadWindowState()
	if err != nil {
		return "", err
	}
	if wp, ok := st.Windows[windowID]; ok {
		return wp.ActiveProfile, nil
	}
	return "", nil
}

// GetEffectiveActiveProfile returns the effective active profile for a window, with
// OCODE_PROFILE env taking precedence (ephemeral).
func GetEffectiveActiveProfile(windowID string) string {
	if v := os.Getenv("OCODE_PROFILE"); v != "" {
		return v
	}
	p, _ := GetActiveProfile(windowID)
	return p
}

func SetActiveProfile(windowID, profile string) error {
	if windowID == "" {
		return fmt.Errorf("windowID required")
	}
	if profile != "" {
		if err := ValidateProfileName(profile); err != nil {
			return err
		}
		// ensure profile exists (empty delta is allowed if name exists)
		cfg, err := loadFullOcodeConfig()
		if err != nil {
			return err
		}
		if _, ok := cfg.Profiles[profile]; !ok {
			return fmt.Errorf("profile %q not found", profile)
		}
	}
	unlock, err := lockWindowState()
	if err != nil {
		return err
	}
	defer unlock()
	st, err := loadWindowState()
	if err != nil {
		return err
	}
	if st.Windows == nil {
		st.Windows = make(map[string]windowProfile)
	}
	if profile == "" {
		// Default — remove entry to keep file small or store empty
		delete(st.Windows, windowID)
	} else {
		st.Windows[windowID] = windowProfile{ActiveProfile: profile}
	}
	return writeWindowState(st)
}

// WindowStateForTest returns the raw map for testing.
func WindowStateForTest() (map[string]string, error) {
	st, err := loadWindowState()
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(st.Windows))
	for k, v := range st.Windows {
		out[k] = v.ActiveProfile
	}
	return out, nil
}

func RenameWindowStateProfile(oldName, newName string) error {
	unlock, err := lockWindowState()
	if err != nil {
		return err
	}
	defer unlock()
	st, err := loadWindowState()
	if err != nil {
		return err
	}
	changed := false
	for win, wp := range st.Windows {
		if wp.ActiveProfile == oldName {
			wp.ActiveProfile = newName
			st.Windows[win] = wp
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return writeWindowState(st)
}

func ClearWindowStateProfile(profile string) error {
	unlock, err := lockWindowState()
	if err != nil {
		return err
	}
	defer unlock()
	st, err := loadWindowState()
	if err != nil {
		return err
	}
	changed := false
	for win, wp := range st.Windows {
		if wp.ActiveProfile == profile {
			delete(st.Windows, win)
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return writeWindowState(st)
}
