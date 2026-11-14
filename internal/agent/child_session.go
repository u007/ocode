package agent

import (
	"fmt"
	"strings"
	"time"
	"unicode"
)

// childSessionID mints the child session id. The id doubles as a file name, and
// a plugin agent name can carry a colon ("okf:context"), which Windows reads as
// an alternate-data-stream separator. Every rune outside letters, digits, '-',
// '_' and '.' is replaced with '-' in the id only; the metadata keeps the name.
func childSessionID(parentSessionID, agentName string) string {
	safe := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == '.' {
			return r
		}
		return '-'
	}, agentName)
	ts := time.Now().Format("2006-01-02-150405")
	return fmt.Sprintf("%s_child_%s_%s", parentSessionID, safe, ts)
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
