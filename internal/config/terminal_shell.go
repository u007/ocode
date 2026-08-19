package config

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// windowsCandidateShells is checked in preference order: PowerShell 7+ (pwsh)
// first since it is the actively developed shell, then the Windows PowerShell
// 5.1 that ships on every Windows install, then cmd.exe as the universal
// fallback, then WSL/Git-Bash for users who have them installed.
var windowsCandidateShells = []string{"pwsh.exe", "powershell.exe", "cmd.exe", "wsl.exe", "bash.exe"}

// AvailableShells returns the shell binaries the terminal can offer in a
// picker, most-preferred first. On Unix it reads /etc/shells (the standard
// registry of login shells); on Windows there is no equivalent registry, so
// it probes PATH for the common shells instead. The list is best-effort: a
// read/lookup failure yields an empty slice rather than an error, since the
// picker degrades gracefully to "use the default".
func AvailableShells() []string {
	if runtime.GOOS == "windows" {
		var shells []string
		for _, name := range windowsCandidateShells {
			if path, err := exec.LookPath(name); err == nil {
				shells = append(shells, path)
			}
		}
		return shells
	}

	data, err := os.ReadFile("/etc/shells")
	if err != nil {
		return nil
	}
	var shells []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if info, err := os.Stat(line); err == nil && !info.IsDir() {
			shells = append(shells, line)
		}
	}
	return shells
}

// DefaultTerminalShell picks the shell the interactive terminal starts when
// no explicit TerminalShell override is configured: $SHELL (Unix) /
// %COMSPEC% (Windows) if set, else the first entry AvailableShells finds,
// else a hardcoded last resort.
func DefaultTerminalShell() string {
	if runtime.GOOS == "windows" {
		if shell := os.Getenv("COMSPEC"); shell != "" {
			return shell
		}
		if shells := AvailableShells(); len(shells) > 0 {
			return shells[0]
		}
		return "cmd.exe"
	}
	if shell := os.Getenv("SHELL"); shell != "" {
		return shell
	}
	if shells := AvailableShells(); len(shells) > 0 {
		return shells[0]
	}
	return "/bin/sh"
}
