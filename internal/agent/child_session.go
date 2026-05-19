package agent

import (
	"fmt"
	"time"
)

func childSessionID(parentSessionID, agentName string) string {
	ts := time.Now().Format("2006-01-02-150405")
	return fmt.Sprintf("%s_child_%s_%s", parentSessionID, agentName, ts)
}

func childSessionMetadata(parentSessionID, agentName string) map[string]any {
	return map[string]any{
		"parent_session_id": parentSessionID,
		"agent_name":        agentName,
		"started_at":        time.Now().Format(time.RFC3339),
		"status":            "completed",
	}
}
