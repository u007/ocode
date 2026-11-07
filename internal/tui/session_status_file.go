package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/u007/ocode/internal/paths"
)

// writeSessionStatusFile records the TUI session's live turn state so external
// readers (the `ocode session` CLI, a remote server) can observe it even though
// the TUI has no HTTP listener.
func writeSessionStatusFile(sessionID string, turnActive bool) {
	if sessionID == "" {
		return
	}
	gd, err := paths.GlobalDataDir()
	if err != nil { return }
	dir := filepath.Join(gd, "session-status")
	_ = os.MkdirAll(dir, 0o755)
	data := map[string]any{
		"session_id":  sessionID,
		"turn_active": turnActive,
		"updated_at":  time.Now().UTC().Format(time.RFC3339),
	}
	b, err := json.Marshal(data)
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(dir, sessionID+".json"), b, 0o644)
}
