package session

import "github.com/u007/ocode/internal/agent"

// The agent package cannot import internal/session (session already imports
// agent for Message), so it exposes a small store hook. Registering it here
// makes every binary that imports session — TUI, server, CLI — persist and
// restore the Claude Code CLI session id with the ocode transcript.
func init() {
	agent.SetClaudeCodeSessionStore(&agent.ClaudeCodeSessionStore{
		Load: func(projectRoot, sessionID string) (string, error) {
			if projectRoot == "" || sessionID == "" {
				return "", nil
			}
			exists, err := ExistsForDir(projectRoot, sessionID)
			if err != nil {
				return "", err
			}
			if !exists {
				return "", nil
			}
			md, err := ReadMetadataForDir(projectRoot, sessionID)
			if err != nil {
				return "", err
			}
			id, _ := md[agent.ClaudeCodeSessionMetadataKey].(string)
			return id, nil
		},
		Save: func(projectRoot, sessionID, claudeSessionID string) error {
			if projectRoot == "" || sessionID == "" || claudeSessionID == "" {
				return nil
			}
			return UpdateMetadataForDir(projectRoot, sessionID, func(md map[string]any) {
				md[agent.ClaudeCodeSessionMetadataKey] = claudeSessionID
			})
		},
	})
}
