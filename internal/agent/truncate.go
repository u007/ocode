package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const maxToolResultLines = 100

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

// TruncateToolResult returns result unchanged when it has <= maxToolResultLines
// lines. Otherwise it writes the full result to a per-tool-call file and
// returns the first maxToolResultLines lines plus a notice describing how the
// model can retrieve the remaining content.
func TruncateToolResult(toolUseID, result string) string {
	if toolUseID == "" {
		return result
	}
	// Count lines without allocating a full split first.
	nl := strings.Count(result, "\n")
	totalLines := nl
	if len(result) > 0 && !strings.HasSuffix(result, "\n") {
		totalLines = nl + 1
	}
	if totalLines <= maxToolResultLines {
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

	// Build the head slice (first maxToolResultLines lines).
	head := result
	idx := 0
	for i := 0; i < maxToolResultLines; i++ {
		next := strings.IndexByte(result[idx:], '\n')
		if next < 0 {
			idx = len(result)
			break
		}
		idx += next + 1
	}
	head = result[:idx]
	head = strings.TrimRight(head, "\n")

	notice := fmt.Sprintf(
		"\n\n[output truncated: showing first %d of %d lines]\n"+
			"Full output saved to: %s\n"+
			"Retrieve remaining content with:\n"+
			"  read tool: {\"path\": %q, \"offset\": %d, \"limit\": <n>}\n"+
			"  or bash:   sed -n '%d,%dp' %s",
		maxToolResultLines, totalLines, path,
		path, maxToolResultLines+1,
		maxToolResultLines+1, totalLines, path,
	)
	return head + notice
}
