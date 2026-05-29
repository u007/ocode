package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"
)

const maxToolResultLines = 100
const maxToolResultChars = 12000

// TruncationMarkerPrefix is the fixed prefix of the truncation notice appended
// by TruncateToolResult. Renderers use it to locate and strip the footer.
const TruncationMarkerPrefix = "[output truncated:"

func toolResultCacheDir() (string, error) {
	if env := os.Getenv("XDG_STATE_HOME"); env != "" {
		return filepath.Join(env, "opencode", "tool-results"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "windows" {
		if base := os.Getenv("LOCALAPPDATA"); base != "" {
			return filepath.Join(base, "opencode", "tool-results"), nil
		}
	}
	return filepath.Join(home, ".local", "state", "opencode", "tool-results"), nil
}

// TruncateToolResult returns result unchanged when it fits within the tool
// output budget. Otherwise it writes the full result to a per-tool-call file
// and returns a bounded prefix plus a notice describing how the model can
// retrieve the remaining content.
func TruncateToolResult(toolUseID, result string) string {
	if toolUseID == "" {
		return result
	}
	totalChars := utf8.RuneCountInString(result)
	// Count lines without allocating a full split first.
	nl := strings.Count(result, "\n")
	totalLines := nl
	if len(result) > 0 && !strings.HasSuffix(result, "\n") {
		totalLines = nl + 1
	}
	if totalLines <= maxToolResultLines && totalChars <= maxToolResultChars {
		return result
	}

	dir, err := toolResultCacheDir()
	if err != nil {
		return result
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return result
	}
	path := filepath.Join(dir, toolUseID+".txt")
	if err := os.WriteFile(path, []byte(result), 0o644); err != nil {
		return result
	}

	// Build the head slice bounded by both lines and characters.
	headEnd := len(result)
	if totalLines > maxToolResultLines {
		idx := 0
		for i := 0; i < maxToolResultLines; i++ {
			next := strings.IndexByte(result[idx:], '\n')
			if next < 0 {
				idx = len(result)
				break
			}
			idx += next + 1
		}
		headEnd = idx
	}
	if totalChars > maxToolResultChars {
		runeCount := 0
		charEnd := 0
		for i := range result {
			if runeCount == maxToolResultChars {
				charEnd = i
				break
			}
			runeCount++
		}
		if charEnd == 0 && runeCount < maxToolResultChars {
			charEnd = len(result)
		}
		if charEnd < headEnd {
			headEnd = charEnd
		}
	}
	head := strings.TrimRight(result[:headEnd], "\n")

	notice := fmt.Sprintf(
		"\n\n[output truncated: showing %d/%d lines, %d/%d chars]\n"+
			"Full output saved to: %s\n"+
			"Retrieve remaining content with:\n"+
			"  read tool: {\"path\": %q, \"start_line\": %d, \"end_line\": <n>}\n"+
			"  or bash:   sed -n '%d,%dp' %s",
		strings.Count(head, "\n")+func() int {
			if head == "" {
				return 0
			}
			return 1
		}(), totalLines,
		utf8.RuneCountInString(head), totalChars,
		path,
		path, maxToolResultLines+1,
		maxToolResultLines+1, totalLines, path,
	)
	return head + notice
}

// CleanupToolResults removes cached tool result files older than maxAge.
// It is safe to call from any goroutine.
func CleanupToolResults(maxAge time.Duration) error {
	dir, err := toolResultCacheDir()
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	cutoff := time.Now().Add(-maxAge)
	var firstErr error
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			if err := os.Remove(filepath.Join(dir, e.Name())); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}
