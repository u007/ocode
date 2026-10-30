package main

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
)

// globalDataDir mirrors internal/paths.GlobalDataDir, which is the source of truth
// (docs/concepts/data-storage-layout.md). The bridge is its own Go module and cannot import it.
//
//	Windows: %LOCALAPPDATA%\opencode
//	macOS:   ~/.local/share/opencode
//	Linux:   $XDG_DATA_HOME/opencode, or ~/.local/share/opencode
//
// KEEP THIS IN SYNC with internal/paths/paths.go (GlobalDataDir). If the platform rules change there, change
// them here too, or the bridge's state and workspaces land somewhere ocode never looks. Unlike internal/paths
// this does not create the directory: each caller creates what it writes.
func globalDataDir() (string, error) {
	const app = "opencode"
	switch runtime.GOOS {
	case "windows":
		base := os.Getenv("LOCALAPPDATA")
		if base == "" {
			return "", errors.New("LOCALAPPDATA not set")
		}
		return filepath.Join(base, app), nil
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".local", "share", app), nil
	default: // linux, freebsd, ...
		if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
			return filepath.Join(xdg, app), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".local", "share", app), nil
	}
}

// excelDataRoot is <globalDataDir>/excel: everything the Excel add-in keeps on this machine.
func excelDataRoot() (string, error) {
	g, err := globalDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(g, "excel"), nil
}

// defaultWorkspaceRoot is <globalDataDir>/excel/workspaces: one ocode project folder per workbook, used when
// --workspace-root (or EXCEL_BRIDGE_WORKSPACE) is not set.
func defaultWorkspaceRoot() (string, error) {
	r, err := excelDataRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(r, "workspaces"), nil
}

// defaultStateDir is <globalDataDir>/excel/state: the undo journal and session index, persisted across bridge
// restarts. Used when --data-dir (or EXCEL_BRIDGE_DATA) is not set.
func defaultStateDir() (string, error) {
	r, err := excelDataRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(r, "state"), nil
}
