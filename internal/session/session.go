package session

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/jamesmercstudio/ocode/internal/agent"
	"github.com/jamesmercstudio/ocode/internal/tool"
)

type Session struct {
	ID        string          `json:"id"`
	Title     string          `json:"title"`
	Messages  []agent.Message `json:"messages"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
	Metadata  map[string]any  `json:"metadata,omitempty"`
}

type Source string

const (
	SourceOcode  Source = "ocode"
	SourceClaude Source = "claude"
)

type Ref struct {
	ID        string
	Title     string
	UpdatedAt time.Time
	Source    Source
}

type sessionIndex struct {
	LastSessionID string            `json:"last_session_id"`
	Sessions      map[string]string `json:"sessions"` // ID -> Title
}

func GetStorageDir() (string, error) {
	localDir := filepath.Join(".ocode", "sessions")
	if _, err := os.Stat(localDir); err == nil {
		return localDir, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	base := filepath.Join(home, ".local", "share", "opencode")
	if runtime.GOOS == "windows" {
		base = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Local", "opencode")
	}

	slug := getProjectSlug()

	dir := filepath.Join(base, "project", slug, "sessions")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	return dir, nil
}

func getProjectSlug() string {
	wd, _ := os.Getwd()

	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	if output, err := cmd.Output(); err == nil {
		wd = strings.TrimSpace(string(output))
	}

	wd = filepath.Clean(wd)
	if runtime.GOOS == "windows" {
		wd = strings.ToLower(wd)
	}

	hash := sha256.Sum256([]byte(wd))
	return hex.EncodeToString(hash[:])[:12]
}

func Save(id string, title string, messages []agent.Message, metadata map[string]any) error {
	dir, err := GetStorageDir()
	if err != nil {
		return err
	}

	if id == "" {
		id = time.Now().Format("2006-01-02-150405")
	}

	path := filepath.Join(dir, id+".json")

	var s Session
	data, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(data, &s); err != nil {
			return fmt.Errorf("session file %s is corrupt: %w", path, err)
		}
	} else {
		s.ID = id
		s.CreatedAt = time.Now()
	}

	if title != "" {
		s.Title = title
	} else if s.Title == "" && len(messages) > 0 {
		// Auto-title from first user message
		for _, m := range messages {
			if m.Role == "user" {
				title = m.Content
				if len(title) > 40 {
					title = title[:37] + "..."
				}
				s.Title = title
				break
			}
		}
	}

	s.Messages = messages
	if metadata != nil {
		s.Metadata = metadata
	}
	s.UpdatedAt = time.Now()

	out, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}

	err = os.WriteFile(path, out, 0644)
	if err != nil {
		return err
	}

	return updateIndex(dir, id, s.Title)
}

func updateIndex(dir, id, title string) error {
	indexPath := filepath.Join(dir, "index.json")
	var idx sessionIndex
	data, err := os.ReadFile(indexPath)
	if err == nil {
		// Best-effort: ignore corrupt index (it will be rebuilt over time).
		json.Unmarshal(data, &idx) //nolint:errcheck
	}
	if idx.Sessions == nil {
		idx.Sessions = make(map[string]string)
	}
	idx.LastSessionID = id
	idx.Sessions[id] = title

	out, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal session index: %w", err)
	}
	return os.WriteFile(indexPath, out, 0644)
}

func Load(id string) (*Session, error) {
	dir, err := GetStorageDir()
	if err != nil {
		return nil, err
	}

	path := filepath.Join(dir, id+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	s.Messages = removeIncompleteToolRequests(s.Messages)

	return &s, nil
}

func removeIncompleteToolRequests(messages []agent.Message) []agent.Message {
	completedToolIDs := make(map[string]struct{})
	for _, msg := range messages {
		if msg.Role == "tool" {
			if msg.ToolID == "" {
				log.Printf("session: tool message with empty ToolID (content: %.80q) — treating as incomplete", msg.Content)
				continue
			}
			if !isIncompleteToolResult(msg.Content) {
				completedToolIDs[msg.ToolID] = struct{}{}
			}
		}
	}

	out := make([]agent.Message, 0, len(messages))
	for _, msg := range messages {
		if msg.Role == "tool" && isIncompleteToolResult(msg.Content) {
			continue
		}
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			completedCalls := make([]agent.ToolCall, 0, len(msg.ToolCalls))
			for _, call := range msg.ToolCalls {
				if _, ok := completedToolIDs[call.ID]; ok {
					completedCalls = append(completedCalls, call)
				}
			}
			msg.ToolCalls = completedCalls
			if msg.Content == "" && msg.ReasoningContent == "" && len(msg.ToolCalls) == 0 {
				continue
			}
		}
		out = append(out, msg)
	}
	return out
}

func isIncompleteToolResult(content string) bool {
	return strings.Contains(content, tool.SentinelWaitingForUser) || strings.HasPrefix(content, tool.SentinelPermissionAsk)
}

func List() ([]Session, error) {
	dir, err := GetStorageDir()
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var sessions []Session
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".json" && e.Name() != "index.json" {
			data, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err == nil {
				var s Session
				if err := json.Unmarshal(data, &s); err == nil {
					s.Messages = removeIncompleteToolRequests(s.Messages)
					sessions = append(sessions, s)
				}
			}
		}
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
	})

	return sessions, nil
}

func ListAll() ([]Ref, error) {
	ocodeSessions, err := List()
	if err != nil {
		return nil, err
	}
	refs := make([]Ref, 0, len(ocodeSessions))
	clonedClaude := make(map[string]struct{})
	for _, s := range ocodeSessions {
		refs = append(refs, Ref{ID: s.ID, Title: s.Title, UpdatedAt: s.UpdatedAt, Source: SourceOcode})
		if s.Metadata != nil {
			if originalID, ok := s.Metadata["claude_original_session_id"].(string); ok && originalID != "" {
				clonedClaude[originalID] = struct{}{}
			}
		}
	}

	claudeRefs, err := listClaudeSessions()
	if err != nil {
		return nil, err
	}
	for _, ref := range claudeRefs {
		if _, ok := clonedClaude[strings.TrimPrefix(ref.ID, "claude:")]; ok {
			continue
		}
		refs = append(refs, ref)
	}
	sort.Slice(refs, func(i, j int) bool {
		return refs[i].UpdatedAt.After(refs[j].UpdatedAt)
	})
	return refs, nil
}

// ListRefs returns Ref metadata for all sessions without loading full message content.
// It is significantly faster than List/ListAll for listing operations.
func ListRefs() ([]Ref, error) {
	dir, err := GetStorageDir()
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	// Lightweight struct for partial JSON decode (no Messages)
	type sessionMeta struct {
		ID        string    `json:"id"`
		Title     string    `json:"title"`
		UpdatedAt time.Time `json:"updated_at"`
		Metadata  map[string]any `json:"metadata,omitempty"`
	}

	refs := make([]Ref, 0, len(entries))
	clonedClaude := make(map[string]struct{})

	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" || e.Name() == "index.json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var meta sessionMeta
		if err := json.Unmarshal(data, &meta); err != nil {
			continue
		}
		refs = append(refs, Ref{
			ID:        meta.ID,
			Title:     meta.Title,
			UpdatedAt: meta.UpdatedAt,
			Source:    SourceOcode,
		})
		if meta.Metadata != nil {
			if originalID, ok := meta.Metadata["claude_original_session_id"].(string); ok && originalID != "" {
				clonedClaude[originalID] = struct{}{}
			}
		}
	}

	sort.Slice(refs, func(i, j int) bool {
		return refs[i].UpdatedAt.After(refs[j].UpdatedAt)
	})

	claudeRefs, err := listClaudeSessions()
	if err != nil {
		return refs, nil // best-effort: return ocode-only refs
	}
	for _, ref := range claudeRefs {
		if _, ok := clonedClaude[strings.TrimPrefix(ref.ID, "claude:")]; ok {
			continue
		}
		refs = append(refs, ref)
	}

	sort.Slice(refs, func(i, j int) bool {
		return refs[i].UpdatedAt.After(refs[j].UpdatedAt)
	})

	return refs, nil
}

func LoadAny(id string) (*Session, error) {
	if strings.HasPrefix(id, "claude:") {
		return CloneClaudeSession(strings.TrimPrefix(id, "claude:"))
	}
	return Load(id)
}

func CloneClaudeSession(id string) (*Session, error) {
	cloneID := "claude-" + id
	if s, err := Load(cloneID); err == nil {
		return s, nil
	}

	claudeSession, err := loadClaudeSession(id)
	if err != nil {
		return nil, err
	}
	claudeSession.ID = cloneID
	if claudeSession.Metadata == nil {
		claudeSession.Metadata = make(map[string]any)
	}
	claudeSession.Metadata["source"] = string(SourceClaude)
	claudeSession.Metadata["claude_original_session_id"] = id
	if err := Save(cloneID, claudeSession.Title, claudeSession.Messages, claudeSession.Metadata); err != nil {
		return nil, err
	}
	return Load(cloneID)
}

func listClaudeSessions() ([]Ref, error) {
	dir, err := getClaudeProjectDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	refs := make([]Ref, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".jsonl" {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".jsonl")
		path := filepath.Join(dir, e.Name())
		ref, err := claudeRefFromFile(id, path)
		if err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	return refs, nil
}

func getClaudeProjectDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	wd, _ := os.Getwd()
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	if output, err := cmd.Output(); err == nil {
		wd = strings.TrimSpace(string(output))
	}
	return filepath.Join(home, ".claude", "projects", claudeProjectSlug(wd)), nil
}

func claudeProjectSlug(path string) string {
	clean := filepath.ToSlash(filepath.Clean(path))
	return strings.ReplaceAll(clean, "/", "-")
}

func claudeRefFromFile(id, path string) (Ref, error) {
	s, err := parseClaudeSessionFile(id, path)
	if err != nil {
		return Ref{}, err
	}
	return Ref{ID: "claude:" + id, Title: s.Title, UpdatedAt: s.UpdatedAt, Source: SourceClaude}, nil
}

func loadClaudeSession(id string) (*Session, error) {
	dir, err := getClaudeProjectDir()
	if err != nil {
		return nil, err
	}
	return parseClaudeSessionFile(id, filepath.Join(dir, id+".jsonl"))
}

func parseClaudeSessionFile(id, path string) (*Session, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	s := &Session{ID: id, Metadata: map[string]any{"source": string(SourceClaude), "claude_path": path}}
	if err := parseClaudeJSONL(f, s); err != nil {
		return nil, err
	}
	if s.Title == "" {
		s.Title = id
	}
	if s.CreatedAt.IsZero() {
		if info, err := os.Stat(path); err == nil {
			s.CreatedAt = info.ModTime()
			s.UpdatedAt = info.ModTime()
		}
	}
	return s, nil
}

func parseClaudeJSONL(r io.Reader, s *Session) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		var entry struct {
			Type      string          `json:"type"`
			IsMeta    bool            `json:"isMeta"`
			Timestamp string          `json:"timestamp"`
			Message   json.RawMessage `json:"message"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			return err
		}
		if entry.IsMeta || (entry.Type != "user" && entry.Type != "assistant") {
			continue
		}
		role, content, model, ok := claudeMessage(entry.Message)
		if !ok || content == "" {
			continue
		}
		parsedTime, hasTime := parseClaudeTime(entry.Timestamp)
		if hasTime {
			if s.CreatedAt.IsZero() || parsedTime.Before(s.CreatedAt) {
				s.CreatedAt = parsedTime
			}
			if parsedTime.After(s.UpdatedAt) {
				s.UpdatedAt = parsedTime
			}
		}
		if s.Title == "" && role == "user" {
			s.Title = titleFromContent(content)
		}
		s.Messages = append(s.Messages, agent.Message{Role: role, Content: content, Model: model})
	}
	return scanner.Err()
}

func parseClaudeTime(value string) (time.Time, bool) {
	if value == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, value)
	return t, err == nil
}

func titleFromContent(content string) string {
	content = strings.TrimSpace(content)
	content = strings.ReplaceAll(content, "\n", " ")
	if len(content) > 40 {
		return content[:37] + "..."
	}
	return content
}

func claudeMessage(raw json.RawMessage) (role, content, model string, ok bool) {
	var msg struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
		Model   string          `json:"model"`
	}
	if err := json.Unmarshal(raw, &msg); err != nil || msg.Role == "" {
		return "", "", "", false
	}
	content = claudeContentText(msg.Content)
	return msg.Role, content, msg.Model, content != ""
}

func claudeContentText(raw json.RawMessage) string {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return strings.TrimSpace(text)
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &parts); err != nil {
		return ""
	}
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part.Type == "text" && strings.TrimSpace(part.Text) != "" {
			out = append(out, strings.TrimSpace(part.Text))
		}
	}
	return strings.Join(out, "\n\n")
}
