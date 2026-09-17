package config

import (
	"os"
	"os/exec"
	"runtime"

	"github.com/u007/ocode/internal/shell"
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

	return shell.SystemShells()
}

// ResolveTerminalShell returns a shell the terminal can actually start,
// preferring preferred (the configured terminal_shell override) and falling
// back to $SHELL and then the standard system locations. Every Unix candidate
// is validated as an executable file before it is returned.
//
// The validation is the point: terminal_shell is persisted in the *local*
// global config and travels with a synced config payload, so a value that names
// a shell the current host does not have (a Mac's /bin/zsh on a Linux remote)
// previously reached exec and surfaced as
// `fork/exec /bin/zsh: no such file or directory`. Windows keeps its own
// candidate list because $SHELL/POSIX paths do not apply there.
func ResolveTerminalShell(preferred string) string {
	if runtime.GOOS == "windows" {
		if preferred != "" {
			if _, err := exec.LookPath(preferred); err == nil {
				return preferred
			}
		}
		return DefaultTerminalShell()
	}
	return shell.Resolve(preferred)
}

// DefaultTerminalShell picks the shell the interactive terminal starts when
// no explicit TerminalShell override is configured: $SHELL (Unix) /
// %COMSPEC% (Windows) if set, else the first entry AvailableShells finds,
// else a hardcoded last resort. On Unix the result is validated so an
// unusable $SHELL falls through to an available shell instead of failing at
// exec time.
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
	return shell.Resolve("")
}
