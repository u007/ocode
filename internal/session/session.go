package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/jamesmercstudio/ocode/internal/agent"
)

type Session struct {
	ID       string          `json:"id"`
	Messages []agent.Message `json:"messages"`
}

func GetStorageDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	base := filepath.Join(home, ".local", "share", "opencode")
	if runtime.GOOS == "windows" {
		base = filepath.Join(os.Getenv("USERPROFILE"), ".local", "share", "opencode")
	}

	wd, _ := os.Getwd()
	slug := sanitizeSlug(wd)

	dir := filepath.Join(base, "project", slug, "storage")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	return dir, nil
}

func sanitizeSlug(path string) string {
	s := strings.ToLower(path)
	s = strings.ReplaceAll(s, "\\", "-")
	s = strings.ReplaceAll(s, "/", "-")
	s = strings.ReplaceAll(s, ":", "")
	return strings.Trim(s, "-")
}

func Save(id string, messages []agent.Message) error {
	dir, err := GetStorageDir()
	if err != nil {
		return err
	}

	if id == "" {
		id = time.Now().Format("2006-01-02-150405")
	}

	path := filepath.Join(dir, id+".json")

	s := Session{
		ID:       id,
		Messages: messages,
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
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

func List() ([]string, error) {
	dir, err := GetStorageDir()
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var ids []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
			ids = append(ids, strings.TrimSuffix(e.Name(), ".json"))
		}
	}
	return ids, nil
}
