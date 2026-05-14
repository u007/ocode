package session

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/jamesmercstudio/ocode/internal/agent"
)

type Session struct {
	ID        string          `json:"id"`
	Title     string          `json:"title"`
	Messages  []agent.Message `json:"messages"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
	Metadata  map[string]any `json:"metadata,omitempty"`
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

func Save(id string, title string, messages []agent.Message) error {
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

func Load(id string) ([]agent.Message, error) {
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

	return s.Messages, nil
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
