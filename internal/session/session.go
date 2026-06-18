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
	"sync"
	"time"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/paths"
	"github.com/u007/ocode/internal/tool"
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

const canonicalSessionPrefix = "ses_"

func NewSessionID() string {
	return canonicalSessionPrefix + time.Now().Format("2006-01-02-150405")
}

// GetStorageDir returns the per-project sessions directory under the
// global data dir. It always uses the cross-platform global path
// (see internal/paths.GlobalDataDir).
func GetStorageDir() (string, error) {
	slug := ProjectSlug()
	return paths.ProjectSessionsDir(slug)
}

// gitToplevelCache memoizes `git rev-parse --show-toplevel` per working dir.
// The repo root never changes within a session, so we avoid forking git on
// every session-list call (previously once per getProjectSlug + once per
// getClaudeProjectDir, both hit on every picker open).
var (
	gitToplevelMu    sync.Mutex
	gitToplevelCache = map[string]string{}
)

// workDirOverride overrides os.Getwd() for project slug resolution in
// GetStorageDir and getClaudeProjectDir. Set via SetWorkDir so that session
// storage follows the TUI's explicit workDir instead of the process-wide CWD
// (which can change under /cd or -dir without the session package noticing).
// Empty means fall back to os.Getwd().
var (
	workDirOverride   string
	workDirOverrideMu sync.RWMutex
)

// SetWorkDir sets the working directory used for project slug resolution.
// Symlinks are resolved so the stored path matches os.Getwd() behavior.
// Pass "" to revert to os.Getwd().
func SetWorkDir(dir string) {
	if dir != "" {
		if resolved, err := filepath.EvalSymlinks(dir); err == nil {
			dir = resolved
		}
	}
	workDirOverrideMu.Lock()
	workDirOverride = dir
	workDirOverrideMu.Unlock()
	// Invalidate the git toplevel cache — the resolved root may differ under
	// the new workDir.
	gitToplevelMu.Lock()
	clear(gitToplevelCache)
	gitToplevelMu.Unlock()
}

// effectiveWorkDir returns the configured work dir or falls back to os.Getwd().
func effectiveWorkDir() string {
	workDirOverrideMu.RLock()
	dir := workDirOverride
	workDirOverrideMu.RUnlock()
	if dir != "" {
		return dir
	}
	wd, _ := os.Getwd()
	return wd
}

func gitToplevel(wd string) string {
	gitToplevelMu.Lock()
	defer gitToplevelMu.Unlock()
	if v, ok := gitToplevelCache[wd]; ok {
		return v
	}
	result := wd
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = wd
	if output, err := cmd.Output(); err == nil {
		result = strings.TrimSpace(string(output))
	}
	gitToplevelCache[wd] = result
	return result
}

// ProjectSlug returns the stable slug for the current workspace root.
func ProjectSlug() string {
	return ProjectSlugForPath(effectiveWorkDir())
}

// ProjectSlugForPath returns the stable slug for the workspace containing wd.
func ProjectSlugForPath(wd string) string {
	if wd == "" {
		wd = effectiveWorkDir()
	}
	wd = gitToplevel(wd)
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
		id = NewSessionID()
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

	path, data, err := readSessionFile(dir, id)
	if err != nil {
		if os.IsNotExist(err) && shouldSearchOtherProjects(id) {
			fallbackPath, fallbackData, fallbackErr := readSessionFileAnyProject(id)
			if fallbackErr == nil {
				path = fallbackPath
				data = fallbackData
				err = nil
			} else if !os.IsNotExist(fallbackErr) {
				return nil, fallbackErr
			}
		}
	}
	if err != nil {
		return nil, err
	}

	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("session file %s is corrupt: %w", path, err)
	}
	s.Messages = removeIncompleteToolRequests(s.Messages)

	return &s, nil
}

func shouldSearchOtherProjects(id string) bool {
	if strings.HasPrefix(id, canonicalSessionPrefix) {
		return true
	}
	_, err := time.Parse("2006-01-02-150405", id)
	return err == nil
}

// readSessionFileAnyProject searches all project session dirs for a
// session with the given ID (used when resuming from a different cwd).
func readSessionFileAnyProject(id string) (string, []byte, error) {
	dataDir, err := paths.GlobalDataDir()
	if err != nil {
		return "", nil, err
	}

	projectRoot := filepath.Join(dataDir, "project")
	entries, err := os.ReadDir(projectRoot)
	if err != nil {
		return "", nil, err
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(projectRoot, e.Name(), "sessions")
		path, data, readErr := readSessionFile(dir, id)
		if readErr == nil {
			log.Printf("session: loaded %q from fallback project path %s", id, path)
			return path, data, nil
		}
		if !os.IsNotExist(readErr) {
			return "", nil, readErr
		}
	}

	return "", nil, os.ErrNotExist
}

func readSessionFile(dir, id string) (string, []byte, error) {
	paths := sessionLoadPaths(dir, id)
	var firstErr error
	for i, path := range paths {
		data, err := os.ReadFile(path)
		if err == nil {
			if i > 0 {
				log.Printf("session: fallback load for %q via %s", id, path)
			}
			return path, data, nil
		}
		if firstErr == nil {
			firstErr = err
		}
		if !os.IsNotExist(err) {
			return "", nil, err
		}
	}
	return "", nil, firstErr
}

func sessionLoadPaths(dir, id string) []string {
	ids := []string{id}
	switch {
	case strings.HasPrefix(id, canonicalSessionPrefix):
		legacyID := strings.TrimPrefix(id, canonicalSessionPrefix)
		if legacyID != "" && legacyID != id {
			ids = append(ids, legacyID)
		}
	default:
		ids = append(ids, canonicalSessionPrefix+id)
	}
	paths := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, candidate := range ids {
		if candidate == "" {
			continue
		}
		path := filepath.Join(dir, candidate+".json")
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}
	return paths
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

// ListRefs returns all session refs sorted by updated time (newest first).
// For paginated access, use ListRefsPaginated.
func ListRefs() ([]Ref, error) {
	refs, _, err := ListRefsPaginated(0, 0)
	return refs, err
}

// listWorkers bounds concurrency when reading session files for the list.
// File reads dominate listing cost; fanning them across cores turns a
// sequential walk of the whole session dir into a parallel one.
func listWorkers() int {
	n := runtime.NumCPU()
	if n > 12 {
		n = 12
	}
	if n < 1 {
		n = 1
	}
	return n
}

// mapDirEntries runs fn over each .json/.jsonl entry concurrently (bounded by
// listWorkers) and returns the successful results. Order is not preserved;
// callers sort afterwards. fn returns ok=false to drop an entry.
func mapDirEntries[T any](dir string, entries []os.DirEntry, ext string, fn func(string, os.DirEntry) (T, bool)) []T {
	sem := make(chan struct{}, listWorkers())
	var (
		wg  sync.WaitGroup
		mu  sync.Mutex
		out []T
	)
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ext || e.Name() == "index.json" {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(e os.DirEntry) {
			defer wg.Done()
			defer func() { <-sem }()
			v, ok := fn(filepath.Join(dir, e.Name()), e)
			if !ok {
				return
			}
			mu.Lock()
			out = append(out, v)
			mu.Unlock()
		}(e)
	}
	wg.Wait()
	return out
}

// ocodeMeta holds the cheap-to-extract fields needed to list a session,
// decoded without materializing the (potentially multi-MB) messages array.
type ocodeMeta struct {
	ID        string
	Title     string
	UpdatedAt time.Time
	CloneOf   string // metadata.claude_original_session_id; "" when not a clone
}

// readOcodeMeta streams a session file token-by-token, capturing only the list
// fields. The messages array is consumed as raw bytes and discarded, so we skip
// the dominant cost of unmarshalling thousands of agent.Message structs per
// file. It stays correct regardless of on-disk key order (older files store
// messages mid-object, newer ones may not), so dedup of cloned Claude sessions
// remains exact. modTime is the fallback for updated_at when the field is absent.
func readOcodeMeta(path string, modTime time.Time) (ocodeMeta, error) {
	f, err := os.Open(path)
	if err != nil {
		return ocodeMeta{}, err
	}
	defer f.Close()

	dec := json.NewDecoder(bufio.NewReader(f))
	// Consume the opening '{'.
	if _, err := dec.Token(); err != nil {
		return ocodeMeta{}, err
	}

	var meta ocodeMeta
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return ocodeMeta{}, err
		}
		key, _ := keyTok.(string)
		switch key {
		case "id":
			if err := dec.Decode(&meta.ID); err != nil {
				return ocodeMeta{}, err
			}
		case "title":
			if err := dec.Decode(&meta.Title); err != nil {
				return ocodeMeta{}, err
			}
		case "updated_at":
			if err := dec.Decode(&meta.UpdatedAt); err != nil {
				return ocodeMeta{}, err
			}
		case "metadata":
			var m map[string]any
			if err := dec.Decode(&m); err != nil {
				return ocodeMeta{}, err
			}
			if v, ok := m["claude_original_session_id"].(string); ok {
				meta.CloneOf = v
			}
		default:
			// Skip any other value (notably the heavy "messages" array) as raw
			// bytes — no struct allocation. Must consume exactly one value here
			// or the decoder desyncs for every subsequent key.
			var raw json.RawMessage
			if err := dec.Decode(&raw); err != nil {
				return ocodeMeta{}, err
			}
		}
	}
	if meta.UpdatedAt.IsZero() {
		meta.UpdatedAt = modTime
	}
	return meta, nil
}

// ListRefsPaginated returns a page of session refs with optional limit and offset.
// If limit <= 0, returns all refs. Returns (refs, totalCount, error).
func ListRefsPaginated(limit, offset int) ([]Ref, int, error) {
	dir, err := GetStorageDir()
	if err != nil {
		return nil, 0, err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, 0, err
	}

	metas := mapDirEntries(dir, entries, ".json", func(path string, e os.DirEntry) (ocodeMeta, bool) {
		info, err := e.Info()
		if err != nil {
			log.Printf("session list: stat %s: %v", e.Name(), err)
			return ocodeMeta{}, false
		}
		meta, err := readOcodeMeta(path, info.ModTime())
		if err != nil {
			log.Printf("session list: read meta %s: %v", e.Name(), err)
			return ocodeMeta{}, false
		}
		return meta, true
	})

	allRefs := make([]Ref, 0, len(metas))
	clonedClaude := make(map[string]struct{})
	for _, meta := range metas {
		allRefs = append(allRefs, Ref{
			ID:        meta.ID,
			Title:     meta.Title,
			UpdatedAt: meta.UpdatedAt,
			Source:    SourceOcode,
		})
		if meta.CloneOf != "" {
			clonedClaude[meta.CloneOf] = struct{}{}
		}
	}

	sort.Slice(allRefs, func(i, j int) bool {
		return allRefs[i].UpdatedAt.After(allRefs[j].UpdatedAt)
	})

	claudeRefs, err := listClaudeSessions()
	if err == nil {
		for _, ref := range claudeRefs {
			if _, ok := clonedClaude[strings.TrimPrefix(ref.ID, "claude:")]; ok {
				continue
			}
			allRefs = append(allRefs, ref)
		}
		sort.Slice(allRefs, func(i, j int) bool {
			return allRefs[i].UpdatedAt.After(allRefs[j].UpdatedAt)
		})
	}

	total := len(allRefs)

	// Apply pagination
	if limit > 0 {
		start := offset
		if start > total {
			start = total
		}
		end := start + limit
		if end > total {
			end = total
		}
		allRefs = allRefs[start:end]
	}

	return allRefs, total, nil
}

// Delete removes a session file and updates the index.
func Delete(id string) error {
	dir, err := GetStorageDir()
	if err != nil {
		return err
	}

	path := filepath.Join(dir, id+".json")
	if err := os.Remove(path); err != nil {
		if !os.IsNotExist(err) {
			return err
		}
	}

	// Update index
	indexPath := filepath.Join(dir, "index.json")
	var idx sessionIndex
	data, err := os.ReadFile(indexPath)
	if err == nil {
		json.Unmarshal(data, &idx) //nolint:errcheck
	}
	if idx.Sessions == nil {
		idx.Sessions = make(map[string]string)
	}
	delete(idx.Sessions, id)

	out, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal session index: %w", err)
	}
	return os.WriteFile(indexPath, out, 0644)
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

	refs := mapDirEntries(dir, entries, ".jsonl", func(path string, e os.DirEntry) (Ref, bool) {
		info, err := e.Info()
		if err != nil {
			log.Printf("session list: stat claude %s: %v", e.Name(), err)
			return Ref{}, false
		}
		id := strings.TrimSuffix(e.Name(), ".jsonl")
		return claudeRefQuick(id, path, info.ModTime()), true
	})
	return refs, nil
}

// claudeRefQuick builds a list ref for a Claude transcript without parsing the
// whole .jsonl. It reads only until the first user message (for the title) and
// uses the file mtime for updated_at — the last append is the last activity, so
// mtime is an accurate sort key. Full transcripts (up to multi-MB) are only
// parsed when a session is actually opened (loadClaudeSession).
func claudeRefQuick(id, path string, modTime time.Time) Ref {
	title := ""
	f, err := os.Open(path)
	if err != nil {
		log.Printf("session list: open claude %s: %v", id, err)
	} else {
		defer f.Close()
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
		for scanner.Scan() {
			var entry struct {
				Type    string          `json:"type"`
				IsMeta  bool            `json:"isMeta"`
				Message json.RawMessage `json:"message"`
			}
			if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
				continue // skip malformed line; next line may still yield a title
			}
			if entry.IsMeta || entry.Type != "user" {
				continue
			}
			role, content, _, ok := claudeMessage(entry.Message)
			if ok && role == "user" && content != "" {
				title = titleFromContent(content)
				break
			}
		}
		if err := scanner.Err(); err != nil {
			log.Printf("session list: scan claude %s: %v", id, err)
		}
	}
	if title == "" {
		title = id
	}
	return Ref{ID: "claude:" + id, Title: title, UpdatedAt: modTime, Source: SourceClaude}
}

func getClaudeProjectDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	wd := effectiveWorkDir()
	wd = gitToplevel(wd)
	return filepath.Join(home, ".claude", "projects", claudeProjectSlug(wd)), nil
}

func claudeProjectSlug(path string) string {
	clean := filepath.ToSlash(filepath.Clean(path))
	return strings.ReplaceAll(clean, "/", "-")
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
