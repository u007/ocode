package session

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/jamesmercstudio/ocode/internal/agent"
	"github.com/jamesmercstudio/ocode/internal/version"
)

// AppendClaudeSession appends the provided conversation to the current
// project's Claude Code session history, stored as append-only JSONL.
// The session ID becomes the Claude filename, so repeated exports continue
// the same history file.
func AppendClaudeSession(sessionID string, messages []agent.Message) (string, error) {
	dir, err := getClaudeProjectDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if sessionID == "" {
		sessionID = time.Now().Format("2006-01-02-150405")
	}

	path := filepath.Join(dir, sessionID+".jsonl")
	parentUUID, err := lastClaudeUUID(path)
	if err != nil {
		return "", err
	}

	cwd, _ := os.Getwd()
	gitBranch := currentGitBranch()
	if gitBranch == "" {
		gitBranch = "unknown"
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return "", err
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	now := time.Now().UTC()
	baseUUID := sessionID
	if baseUUID == "" {
		baseUUID = now.Format("20060102T150405.000000000Z0700")
	}
	entryIndex := 0
	for _, msg := range messages {
		if msg.Role == "" || msg.Content == "" && len(msg.ToolCalls) == 0 && msg.ReasoningContent == "" {
			continue
		}
		if msg.Role != "user" && msg.Role != "assistant" && msg.Role != "tool" {
			continue
		}

		entryIndex++
		uuid := fmt.Sprintf("%s-%03d", baseUUID, entryIndex)
		entry := map[string]any{
			"type":       claudeEntryTypeForRole(msg.Role),
			"uuid":       uuid,
			"parentUuid": parentUUID,
			"timestamp":  now.Format(time.RFC3339Nano),
			"sessionId":  sessionID,
			"cwd":        cwd,
			"gitBranch":  gitBranch,
			"version":    version.Version,
			"message":    claudeMessageForExport(msg),
		}

		line, err := json.Marshal(entry)
		if err != nil {
			return "", err
		}
		if _, err := fmt.Fprintln(w, string(line)); err != nil {
			return "", err
		}
		parentUUID = uuid
	}
	if err := w.Flush(); err != nil {
		return "", err
	}
	return path, nil
}

func claudeEntryTypeForRole(role string) string {
	switch role {
	case "assistant":
		return "assistant"
	case "tool":
		return "user"
	default:
		return "user"
	}
}

func claudeMessageForExport(msg agent.Message) map[string]any {
	content := claudeContentBlocks(msg)
	out := map[string]any{"role": claudeEntryTypeForRole(msg.Role)}
	if len(content) == 1 {
		if content[0]["type"] == "text" {
			if s, ok := content[0]["text"].(string); ok {
				out["content"] = s
				return out
			}
		}
	}
	out["content"] = content
	if msg.Model != "" {
		out["model"] = msg.Model
	}
	return out
}

func claudeContentBlocks(msg agent.Message) []map[string]any {
	blocks := make([]map[string]any, 0, 4)
	if msg.Role == "tool" {
		if strings.TrimSpace(msg.ToolID) != "" {
			blocks = append(blocks, map[string]any{
				"type":        "tool_result",
				"tool_use_id": strings.TrimSpace(msg.ToolID),
				"content":     msg.Content,
			})
		}
		return blocks
	}
	if strings.TrimSpace(msg.ReasoningContent) != "" {
		blocks = append(blocks, map[string]any{"type": "thinking", "text": strings.TrimSpace(msg.ReasoningContent)})
	}
	if strings.TrimSpace(msg.Content) != "" {
		blocks = append(blocks, map[string]any{"type": "text", "text": msg.Content})
	}
	for _, tc := range msg.ToolCalls {
		var input any
		if tc.Function.Arguments != "" {
			json.Unmarshal([]byte(tc.Function.Arguments), &input) //nolint:errcheck
		}
		blocks = append(blocks, map[string]any{
			"type":  "tool_use",
			"id":    tc.ID,
			"name":  tc.Function.Name,
			"input": input,
		})
	}
	if len(blocks) == 0 && strings.TrimSpace(msg.Content) != "" {
		blocks = append(blocks, map[string]any{"type": "text", "text": msg.Content})
	}
	return blocks
}

func lastClaudeUUID(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	defer f.Close()

	var last string
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" {
			continue
		}
		var entry struct {
			UUID string `json:"uuid"`
		}
		if err := json.Unmarshal([]byte(line), &entry); err == nil && entry.UUID != "" {
			last = entry.UUID
		}
	}
	if err := s.Err(); err != nil {
		return "", err
	}
	return last, nil
}

func currentGitBranch() string {
	// intentionally not tracked: short-lived read-only git query
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
