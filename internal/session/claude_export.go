package session

import (
	"bufio"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/version"
)

// claudeNamespaceUUID is a fixed 16-byte opaque namespace value used for
// deterministic UUIDv5 derivation of Claude Code session filenames from ocode
// session IDs. Keep this value stable — changing it would cause re-exports of
// the same session to land in a new file instead of appending.
var claudeNamespaceUUID = [16]byte{
	0x6f, 0x63, 0x6f, 0x64, 0x65, 0x2d, 0x63, 0x63,
	0x2d, 0x65, 0x78, 0x70, 0x6f, 0x72, 0x74, 0x21,
}

// entryUUIDv5 derives a per-entry RFC 4122 v5 UUID from the session UUID and
// a 1-based entry index, so the uuid/parentUuid chain inside the file is made
// of valid UUIDs (matching Claude Code's format).
func entryUUIDv5(sessionUUID string, index int) string {
	h := sha1.New()
	h.Write([]byte(sessionUUID))
	h.Write([]byte(fmt.Sprintf(":%d", index)))
	sum := h.Sum(nil)
	var u [16]byte
	copy(u[:], sum[:16])
	u[6] = (u[6] & 0x0f) | 0x50
	u[8] = (u[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		u[0:4], u[4:6], u[6:8], u[8:10], u[10:16])
}

// sessionUUIDv5 derives an RFC 4122 v5 UUID from the namespace and the given
// session ID so that repeated exports of the same ocode session map to the
// same Claude Code session file.
func sessionUUIDv5(sessionID string) string {
	h := sha1.New()
	h.Write(claudeNamespaceUUID[:])
	h.Write([]byte(sessionID))
	sum := h.Sum(nil)
	var u [16]byte
	copy(u[:], sum[:16])
	u[6] = (u[6] & 0x0f) | 0x50 // version 5
	u[8] = (u[8] & 0x3f) | 0x80 // RFC 4122 variant
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		u[0:4], u[4:6], u[6:8], u[8:10], u[10:16])
}

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

	claudeSessionUUID := sessionUUIDv5(sessionID)
	path := filepath.Join(dir, claudeSessionUUID+".jsonl")
	parentUUID, existingCount, err := lastClaudeUUID(path)
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
	baseUUID := claudeSessionUUID
	entryIndex := existingCount
	for _, msg := range messages {
		if msg.Role == "" || msg.Content == "" && len(msg.ToolCalls) == 0 && msg.ReasoningContent == "" {
			continue
		}
		if msg.Role != "user" && msg.Role != "assistant" && msg.Role != "tool" {
			continue
		}

		entryIndex++
		uuid := entryUUIDv5(baseUUID, entryIndex)
		var parent any
		if parentUUID != "" {
			parent = parentUUID
		}
		entry := map[string]any{
			"type":       claudeEntryTypeForRole(msg.Role),
			"uuid":       uuid,
			"parentUuid": parent,
			"timestamp":  now.Format(time.RFC3339Nano),
			"sessionId":  claudeSessionUUID,
			"userType":   "external",
			"entrypoint": "cli",
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

func lastClaudeUUID(path string) (lastUUID string, count int, err error) {
	f, openErr := os.Open(path)
	if openErr != nil {
		if os.IsNotExist(openErr) {
			return "", 0, nil
		}
		return "", 0, openErr
	}
	defer f.Close()

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
		if scanErr := json.Unmarshal([]byte(line), &entry); scanErr == nil && entry.UUID != "" {
			lastUUID = entry.UUID
			count++
		}
	}
	if scanErr := s.Err(); scanErr != nil {
		return "", 0, scanErr
	}
	return lastUUID, count, nil
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
