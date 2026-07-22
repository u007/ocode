package changes

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
)

// RenderDiff produces a unified diff string between backupPath (the pre-session
// state) and currentPath (the live file). It shells out to `diff -u`.
//
// Special cases:
//   - If backupPath does not exist (file was created in-session), returns
//     "(new file — no pre-session baseline)".
//   - If both files exist and are identical, diff -u exits 1 with no output.
//     We detect that and return "(file unchanged since session start)".
//
// The caller (the TUI) applies syntax styling via renderUnifiedDiff.
func RenderDiff(backupPath, currentPath string) (string, error) {
	// File added in-session: no backup exists.
	if backupPath == "" {
		return "(new file — no pre-session baseline)", nil
	}

	// Verify backup file exists.
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		return "(new file — no pre-session baseline)", nil
	}

	// Verify current file exists.
	if _, err := os.Stat(currentPath); os.IsNotExist(err) {
		return "(file deleted since session start)", nil
	}

	cmd := exec.Command("diff", "-u", backupPath, currentPath)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	// diff exits with code:
	//   0 — files identical (no output)
	//   1 — files differ (normal — output is the diff)
	//   2 — error (diff couldn't read files, etc.)
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if exitErr.ExitCode() == 1 {
				// Files differ — this is the expected "diff found"
				out := stdout.String()
				if out == "" {
					return "(file unchanged since session start)", nil
				}
				return out, nil
			}
			// Exit code >= 2 — real error
			stderrStr := stderr.String()
			if stderrStr != "" {
				return "", fmt.Errorf("diff error: %s", stderrStr)
			}
			return "", fmt.Errorf("diff exited with code %d", exitErr.ExitCode())
		}
		return "", fmt.Errorf("diff: %w", err)
	}

	// Exit code 0 — files identical
	return "(file unchanged since session start)", nil
}
