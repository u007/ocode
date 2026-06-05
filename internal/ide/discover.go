package ide

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Lock describes a Claude Code IDE lock file (~/.claude/ide/<port>.lock).
type Lock struct {
	Port             int
	AuthToken        string
	WorkspaceFolders []string
	mtime            time.Time
}

// Discover finds the best-matching Claude Code IDE lock for cwd: among locks
// whose workspace folders contain cwd, it prefers the longest matching folder,
// then the most recently modified lock. Returns (nil, false) if none match.
//
// Mirrors editor.ts resolveEditorLockFile / pathContainsLength.
func Discover(cwd string) (*Lock, bool) {
	dir := lockDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, false
	}

	var best *Lock
	bestLen := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".lock") {
			continue
		}
		l := readLock(filepath.Join(dir, e.Name()))
		if l == nil {
			continue
		}
		matchLen := 0
		for _, wf := range l.WorkspaceFolders {
			if n := pathContainsLength(wf, cwd); n > matchLen {
				matchLen = n
			}
		}
		if matchLen == 0 {
			continue
		}
		if best == nil || matchLen > bestLen || (matchLen == bestLen && l.mtime.After(best.mtime)) {
			best = l
			bestLen = matchLen
		}
	}
	return best, best != nil
}

func lockDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".claude", "ide")
	}
	return filepath.Join(home, ".claude", "ide")
}

func readLock(path string) *Lock {
	port, err := strconv.Atoi(strings.TrimSuffix(filepath.Base(path), ".lock"))
	if err != nil || port <= 0 || port > 65535 {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var raw struct {
		AuthToken        string   `json:"authToken"`
		Transport        string   `json:"transport"`
		WorkspaceFolders []string `json:"workspaceFolders"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}
	// Only the WebSocket transport is supported (empty == ws by default).
	if raw.Transport != "" && raw.Transport != "ws" {
		return nil
	}
	mtime := time.Time{}
	if fi, err := os.Stat(path); err == nil {
		mtime = fi.ModTime()
	}
	return &Lock{
		Port:             port,
		AuthToken:        raw.AuthToken,
		WorkspaceFolders: raw.WorkspaceFolders,
		mtime:            mtime,
	}
}

// pathContainsLength returns len(parent) if parent contains (or equals) child,
// else 0. Used to rank locks by how specifically their workspace folder matches
// the session directory.
func pathContainsLength(parent, child string) int {
	p, err := filepath.Abs(parent)
	if err != nil {
		return 0
	}
	c, err := filepath.Abs(child)
	if err != nil {
		return 0
	}
	rel, err := filepath.Rel(p, c)
	if err != nil {
		return 0
	}
	if rel == "." || (!strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel)) {
		return len(p)
	}
	return 0
}
