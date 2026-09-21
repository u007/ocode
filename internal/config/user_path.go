package config

import (
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// userBinDirs returns the per-user executable directories ocode ensures are on
// PATH: ~/.local/bin (the XDG convention — where the Claude Code native
// installer and most pipx/uv/user-scoped installers land) and ~/bin (the
// long-standing Unix convention). Order is precedence.
func userBinDirs(home string) []string {
	return []string{
		filepath.Join(home, ".local", "bin"),
		filepath.Join(home, "bin"),
	}
}

// mergeUserBinPath returns current with dirs prepended in order, skipping empty
// dirs and any dir already present in current. Empty entries in current are
// preserved (to exec.LookPath an empty PATH element means the current
// directory). current is returned unchanged when there is nothing to add.
func mergeUserBinPath(current string, dirs []string) string {
	present := make(map[string]struct{})
	if current != "" {
		for _, d := range strings.Split(current, string(os.PathListSeparator)) {
			present[d] = struct{}{}
		}
	}
	prefix := make([]string, 0, len(dirs))
	for _, d := range dirs {
		if d == "" {
			continue
		}
		if _, ok := present[d]; ok {
			continue
		}
		present[d] = struct{}{}
		prefix = append(prefix, d)
	}
	if len(prefix) == 0 {
		return current
	}
	if current == "" {
		return strings.Join(prefix, string(os.PathListSeparator))
	}
	sep := string(os.PathListSeparator)
	return strings.Join(prefix, sep) + sep + current
}

// EnsureUserBinPath prepends the user's own bin directories (see userBinDirs)
// to the process PATH when they exist and are not already present. Idempotent;
// returns true when PATH changed.
//
// Why this is needed: those dirs are added by *interactive* shell rc files
// (~/.zshrc sources ~/.local/bin/env), not by the login profiles
// (/etc/zprofile, ~/.zprofile) that `<shell> -l -c` sources. So a process
// launched without the user's interactive shell — the desktop .app under
// launchd, a service/cron unit, a minimal container — never inherits them, and
// every shell ocode spawns (internal/shell.Build's `<shell> -l -c` for the `!`
// composer command and the TUI agent loop, internal/tool's login-shell bash
// tool, the advisor's `claude` subprocess) fails with
// `zsh:1: command not found: claude`.
//
// A login shell keeps an explicitly inherited PATH entry (`path_helper` appends
// the inherited PATH), so fixing the process environment is enough — and it
// covers every child, not just one spawn site. Called once at process start
// (main.go, cmd/ocode-desktop) BEFORE any shell is spawned. A no-op on Windows
// (no ~/.local/bin convention; cmd.exe resolves via %PATH%/%PATHEXT%) and for
// TUI/server runs launched from an interactive shell that already has them.
func EnsureUserBinPath() bool {
	if runtime.GOOS == "windows" {
		return false
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return false
	}
	existing := make([]string, 0, 2)
	for _, dir := range userBinDirs(home) {
		if info, statErr := os.Stat(dir); statErr == nil && info.IsDir() {
			existing = append(existing, dir)
		}
	}
	if len(existing) == 0 {
		return false
	}
	current := os.Getenv("PATH")
	merged := mergeUserBinPath(current, existing)
	if merged == current {
		return false
	}
	if err := os.Setenv("PATH", merged); err != nil {
		log.Printf("ensure user bin path: setting PATH failed: %v", err)
		return false
	}
	log.Printf("ensure user bin path: prepended %s", strings.Join(existing, string(os.PathListSeparator)))
	return true
}
