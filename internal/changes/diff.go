package changes

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// RenderDiff produces a unified diff string between backupPath (the pre-session
// state) and currentPath (the live file). It shells out to `diff -u`.
//
// Special cases:
//   - If backupPath does not exist (file was created in-session), returns
//     a synthetic unified diff showing the entire file as additions
//     (/dev/null → current file) so the preview shows the full content.
//   - If both files exist and are identical, diff -u exits 1 with no output.
//     We detect that and return "(file unchanged since session start)".
//
// The caller (the TUI) applies syntax styling via renderUnifiedDiff.
func RenderDiff(backupPath, currentPath string) (string, error) {
	// File added in-session: no backup exists — show entire file as diff.
	if backupPath == "" {
		return renderNewFilePreview(currentPath)
	}

	// Verify backup file exists.
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		return renderNewFilePreview(currentPath)
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

// renderNewFilePreview returns a synthetic unified diff for a newly created
// file. It reads currentPath and produces a diff with /dev/null as the old
// file so renderUnifiedDiff shows the entire file as added lines. Mirrors the
// git tab's 1 MB preview limit and binary detection.
func renderNewFilePreview(currentPath string) (string, error) {
	if _, err := os.Stat(currentPath); os.IsNotExist(err) {
		return "(file deleted since session start)", nil
	}
	const previewReadLimit = 1024 * 1024 // 1 MB, matches git tab preview
	fh, err := os.Open(currentPath)
	if err != nil {
		return "", fmt.Errorf("open new file: %w", err)
	}
	defer fh.Close()
	data, err := io.ReadAll(io.LimitReader(fh, previewReadLimit+1))
	if err != nil {
		return "", fmt.Errorf("read new file: %w", err)
	}
	probe := data
	if len(probe) > 512 {
		probe = probe[:512]
	}
	if strings.Contains(string(probe), "\x00") {
		return "[binary file]", nil
	}
	truncated := len(data) > previewReadLimit
	if truncated {
		data = data[:previewReadLimit]
	}
	content := string(data)
	lines := strings.Split(content, "\n")
	// Drop trailing empty split from final newline — diff counts lines without it.
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	var b strings.Builder
	b.WriteString("--- /dev/null\n")
	b.WriteString("+++ b/" + currentPath + "\n")
	b.WriteString(fmt.Sprintf("@@ -0,0 +1,%d @@\n", len(lines)))
	for _, l := range lines {
		b.WriteString("+" + l + "\n")
	}
	if truncated {
		b.WriteString("\n[truncated — 1MB limit]\n")
	}
	return b.String(), nil
}
