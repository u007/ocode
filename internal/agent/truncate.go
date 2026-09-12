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

	// Resume point: the line containing the cut (1-based). When the cut falls
	// exactly on a line boundary the next unseen line starts there; when the
	// char cap bit mid-line, that partially shown line must be re-read, so the
	// hint points at it rather than skipping ahead to a fixed line number.
	shownLines := strings.Count(head, "\n")
	if head != "" {
		shownLines++
	}
	resumeLine := shownLines + 1
	if headEnd > 0 && headEnd < len(result) && result[headEnd-1] != '\n' {
		resumeLine = shownLines
	}

	notice := fmt.Sprintf(
		"\n\n[output truncated: showing %d/%d lines, %d/%d chars]\n"+
			"Full output saved to: %s\n"+
			"Retrieve remaining content with:\n"+
			"  read tool (by line): {\"path\": %q, \"start_line\": %d, \"end_line\": <n>}\n"+
			"  read tool (by byte): {\"path\": %q, \"offset_bytes\": %d, \"max_bytes\": %d}\n"+
			"  or bash:             sed -n '%d,%dp' %s",
		shownLines, totalLines,
		utf8.RuneCountInString(head), totalChars,
		path,
		path, resumeLine,
		path, headEnd, maxToolResultChars,
		resumeLine, totalLines, path,
	)
	return head + notice
}

// MaxToolResultContentBudget is the hard upper bound on the size of a
// canonical tool result produced by TruncateToolResult: the bounded head
// (≤ maxToolResultChars) plus the truncation notice (the notice is a fixed
// template carrying a state-dir file path, so a few hundred bytes; 2KB of
// headroom is generous). Consumers that estimate the next request's prompt
// size use it to clamp live-streamed *provisional* tool output, which
// temporarily holds the full untruncated stream until the canonical
// (truncated) message replaces it.
func MaxToolResultContentBudget() int {
	return maxToolResultChars + 2048
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
