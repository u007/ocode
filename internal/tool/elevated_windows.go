//go:build windows

package tool

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// errNoInteractiveDesktop is returned when elevation is requested from a
// session that cannot show the UAC consent prompt (services, CI, headless
// sessions). Failing closed is required: the command must never be silently
// re-run unprivileged.
var errNoInteractiveDesktop = errors.New("elevation is not supported on this host: native elevation needs an interactive Windows desktop (UAC prompt)")

// executeElevated is the Windows counterpart of the Unix PTY executor. Windows
// elevation is interactive (Start-Process -Verb RunAs triggers UAC), so no
// password is collected or typed here — Task 1 keeps NeedsPassword=false on
// this platform, and secret is ignored.
func executeElevated(ctx context.Context, cmd *exec.Cmd, _ []byte) (string, error) {
	if !hasInteractiveDesktop() {
		return "", errNoInteractiveDesktop
	}

	// Capture the elevated process's output via redirect files: an elevated
	// Start-Process runs in a different, elevated session, so its stdio is not
	// inherited by this (unelevated) process.
	dir, err := os.MkdirTemp("", "ocode-elevated-*")
	if err != nil {
		return "", fmt.Errorf("native elevation: temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	outPath := filepath.Join(dir, "stdout")
	errPath := filepath.Join(dir, "stderr")

	args := make([]string, 0, len(cmd.Args))
	for _, a := range cmd.Args {
		args = append(args, psQuote(a))
	}
	ps := fmt.Sprintf(
		"Start-Process -FilePath %s -ArgumentList @(%s) -Verb RunAs -Wait -RedirectStandardOutput %s -RedirectStandardError %s | Out-Null",
		psQuote(cmd.Path), strings.Join(args, ", "), psQuote(outPath), psQuote(errPath),
	)
	run := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", ps)
	if out, runErr := run.CombinedOutput(); runErr != nil {
		return "", fmt.Errorf("native elevation failed: %w: %s", runErr, strings.TrimSpace(string(out)))
	}

	stdout, _ := os.ReadFile(outPath)
	stderr, _ := os.ReadFile(errPath)
	return joinStdoutStderr(string(stdout), string(stderr)), nil
}

// hasInteractiveDesktop reports whether the current session can show a UAC
// prompt. SESSIONNAME is set for console/RDP sessions and empty for service
// and non-interactive sessions.
//
// shortcut: SESSIONNAME is a heuristic; a misconfigured session may be
// misdetected and get the clear error, but we never run unprivileged. Upgrade
// to a Win32 window-station query if false negatives are reported.
func hasInteractiveDesktop() bool {
	s := os.Getenv("SESSIONNAME")
	return s != "" && !strings.EqualFold(s, "Service")
}

// psQuote returns s as a PowerShell single-quoted string literal.
func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
