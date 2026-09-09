package agent

import (
	"fmt"
	"time"
)

func childSessionID(parentSessionID, agentName string) string {
	ts := time.Now().Format("2006-01-02-150405")
	return fmt.Sprintf("%s_child_%s_%s", parentSessionID, agentName, ts)
}

// childSessionMetadata describes a child session's provenance. status is
// "running" while the run streams and the terminal RunStatus string once it
// finishes, so a session listing can tell a live child from a finished one.
func childSessionMetadata(parentSessionID, agentName, status string) map[string]any {
	return map[string]any{
		"parent_session_id": parentSessionID,
		"agent_name":        agentName,
		"started_at":        time.Now().Format(time.RFC3339),
		"status":            status,
	}
}
