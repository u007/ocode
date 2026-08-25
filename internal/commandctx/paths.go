package commandctx

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/paths"
	"github.com/u007/ocode/internal/session"
)

// PathsInfo builds the /paths report: workspace root, extra allowed paths,
// config file locations (with existence + size), auto-detected project root,
// global data directories, and the effective upload dir. Moved from internal/tui
// so the server's GET /api/paths endpoint and the TUI produce byte-identical
// output.
//
// activeOpenCodePath is the caller-resolved active opencode.json path ("" when
// unknown); the ocode-side paths are resolved here via the config package so
// both callers agree.
func PathsInfo(workDir string, extraAllowedPaths []string, uploadDirCfg string, activeOpenCodePath string) string {
	var b strings.Builder
	b.WriteString("## Paths\n\n")

	// Workspace root
	b.WriteString("**Workspace Root:**\n")
	b.WriteString(fmt.Sprintf("  %s\n\n", workDir))

	// Extra allowed paths
	b.WriteString("**Extra Allowed Paths:**\n")
	if len(extraAllowedPaths) > 0 {
		for _, p := range extraAllowedPaths {
			b.WriteString(fmt.Sprintf("  - %s\n", p))
		}
	} else {
		b.WriteString("  (none)\n")
	}
	b.WriteString("\n")

	// Config files
	b.WriteString("**Config Files:**\n")

	// Global opencode config
	if home, err := os.UserHomeDir(); err == nil {
		var globalCfg string
		if runtime.GOOS == "windows" {
			globalCfg = filepath.Join(os.Getenv("APPDATA"), "opencode", "opencode.json")
		} else {
			globalCfg = filepath.Join(home, ".config", "opencode", "opencode.json")
		}
		b.WriteString(fmt.Sprintf("  Global opencode config: %s\n", globalCfg))
		if info, err := os.Stat(globalCfg); err == nil {
			b.WriteString(fmt.Sprintf("    (exists, %d bytes)\n", info.Size()))
		} else {
			b.WriteString("    (not found)\n")
		}

		var globalOcodeCfg string
		if runtime.GOOS == "windows" {
			globalOcodeCfg = filepath.Join(os.Getenv("APPDATA"), "opencode", "ocodeconfig.json")
		} else {
			globalOcodeCfg = filepath.Join(home, ".config", "opencode", "ocodeconfig.json")
		}
		b.WriteString(fmt.Sprintf("  Global ocode config:   %s\n", globalOcodeCfg))
		if info, err := os.Stat(globalOcodeCfg); err == nil {
			b.WriteString(fmt.Sprintf("    (exists, %d bytes)\n", info.Size()))
		} else {
			b.WriteString("    (not found)\n")
		}
	} else {
		b.WriteString("  (cannot resolve home dir)\n")
	}

	if activeOpenCodePath != "" {
		b.WriteString(fmt.Sprintf("  Active opencode config: %s\n", activeOpenCodePath))
		if info, err := os.Stat(activeOpenCodePath); err == nil {
			b.WriteString(fmt.Sprintf("    (exists, %d bytes)\n", info.Size()))
		}
	}
	if p, err := config.ActiveOcodeConfigPath(); err == nil {
		b.WriteString(fmt.Sprintf("  Active ocode config:    %s\n", p))
		if info, err := os.Stat(p); err == nil {
			b.WriteString(fmt.Sprintf("    (exists, %d bytes)\n", info.Size()))
		}
	}

	if projectRoot := config.FindProjectRoot(); projectRoot != "" {
		b.WriteString(fmt.Sprintf("  Project root (auto-detect): %s\n", projectRoot))
		// Check for .opencode / .opencodes project config dirs
		for _, dirName := range []string{".opencode", ".opencodes"} {
			dir := filepath.Join(projectRoot, dirName)
			if info, err := os.Stat(dir); err == nil && info.IsDir() {
				b.WriteString(fmt.Sprintf("  Project config dir:       %s/\n", dir))
			}
		}
	}
	b.WriteString("\n")

	// Data directories
	b.WriteString("**Data Directories:**\n")
	if dataDir, err := paths.GlobalDataDir(); err == nil {
		b.WriteString(fmt.Sprintf("  Global data dir:   %s\n", dataDir))
		authPath := filepath.Join(dataDir, "auth.json")
		b.WriteString(fmt.Sprintf("  Auth:              %s\n", authPath))
		if info, err := os.Stat(authPath); err == nil {
			b.WriteString(fmt.Sprintf("    (exists, %d bytes)\n", info.Size()))
		}
		slug := session.ProjectSlug()
		b.WriteString(fmt.Sprintf("  Project sessions:  %s\n", filepath.Join(dataDir, "project", slug, "sessions")))
		b.WriteString(fmt.Sprintf("  Usage data:        %s\n", filepath.Join(dataDir, "usage")))
	} else {
		b.WriteString(fmt.Sprintf("  (error: %v)\n", err))
	}

	// Upload dir
	uploadDir := filepath.Join(workDir, ".ocode", "uploads")
	if uploadDirCfg != "" {
		uploadDir = uploadDirCfg
	}
	b.WriteString(fmt.Sprintf("  Upload dir:        %s\n", uploadDir))

	return b.String()
}
