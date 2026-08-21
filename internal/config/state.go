package config

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// opencode persists the user's recent / favourite model selections in
// ${XDG_STATE_HOME}/opencode/model.json. The file is owned by the upstream
// opencode CLI; ocode reads it for picker display and writes recent entries
// back when the user picks a model in the TUI so the two CLIs stay in sync.
// It is deliberately NOT consulted as a startup-model fallback — see Load()
// in config.go: the list churns from every tool/instance sharing it, and
// silently adopting whatever another session used last surfaced as surprise
// startup models the user never picked.

type modelStateEntry struct {
	ProviderID string `json:"providerID"`
	ModelID    string `json:"modelID"`
}

type modelState struct {
	Recent   []modelStateEntry      `json:"recent"`
	Favorite []modelStateEntry      `json:"favorite,omitempty"`
	Variant  map[string]string      `json:"variant,omitempty"`
	Extra    map[string]interface{} `json:"-"`
}

const recentCap = 25

// lockModelState takes an exclusive cross-process lock around model.json
// read-modify-write cycles. The upstream opencode CLI shares this file and
// does not honor the lock, but serializing ocode's own writers removes lost
// updates between concurrent ocode instances (e.g. many TUIs launched at
// once). Mirrors lockOcodeConfig: O_EXCL create, stale-lock steal after 10s,
// proceed unlocked after a 5s deadline rather than failing the save.
func lockModelState() (func(), error) {
	path, err := getModelStatePath()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	lockPath := path + ".lock"
	deadline := time.Now().Add(5 * time.Second)
	for {
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if err == nil {
			f.Close()
			return func() { os.Remove(lockPath) }, nil
		}
		if !os.IsExist(err) {
			return nil, err
		}
		// A crashed process can leave the lock file behind forever; steal it
		// once it's clearly stale rather than deadlocking every future save.
		if info, statErr := os.Stat(lockPath); statErr == nil && time.Since(info.ModTime()) > 10*time.Second {
			os.Remove(lockPath)
			continue
		}
		if time.Now().After(deadline) {
			log.Printf("config: model state lock %s still held after 5s; saving unlocked (a concurrent write may be lost)", lockPath)
			return func() {}, nil
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func getModelStatePath() (string, error) {
	if env := os.Getenv("XDG_STATE_HOME"); env != "" {
		return filepath.Join(env, "opencode", "model.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "windows" {
		if base := os.Getenv("LOCALAPPDATA"); base != "" {
			return filepath.Join(base, "opencode", "model.json"), nil
		}
	}
	return filepath.Join(home, ".local", "state", "opencode", "model.json"), nil
}

// LoadRecentModels returns recent opencode model selections as
// "provider/model" strings, most-recent first.
func LoadRecentModels() []string {
	path, err := getModelStatePath()
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var raw struct {
		Recent []modelStateEntry `json:"recent"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}
	out := make([]string, 0, len(raw.Recent))
	for _, e := range raw.Recent {
		if e.ProviderID == "" || e.ModelID == "" {
			continue
		}
		out = append(out, e.ProviderID+"/"+e.ModelID)
	}
	return out
}

// SaveRecentModel prepends the given "provider/model" id to the
// opencode recent list, dedupes, caps, and writes back. Preserves
// favorite / variant fields opencode owns.
func SaveRecentModel(providerModel string) error {
	provID, modelID := splitProviderModel(providerModel)
	if provID == "" || modelID == "" {
		return fmt.Errorf("invalid provider/model id: %q", providerModel)
	}
	return readWriteModelState(func(full map[string]json.RawMessage) error {
		var recent []modelStateEntry
		if raw, ok := full["recent"]; ok {
			_ = json.Unmarshal(raw, &recent)
		}

		filtered := make([]modelStateEntry, 0, len(recent)+1)
		filtered = append(filtered, modelStateEntry{ProviderID: provID, ModelID: modelID})
		for _, e := range recent {
			if e.ProviderID == provID && e.ModelID == modelID {
				continue
			}
			if e.ProviderID == "" || e.ModelID == "" {
				continue
			}
			filtered = append(filtered, e)
			if len(filtered) >= recentCap {
				break
			}
		}

		newRecent, err := json.Marshal(filtered)
		if err != nil {
			return err
		}
		full["recent"] = newRecent
		return nil
	})
}

func splitProviderModel(s string) (string, string) {
	for i := 0; i < len(s); i++ {
		if s[i] == '/' {
			return s[:i], s[i+1:]
		}
	}
	return "", ""
}

// LoadFavorites returns favorite models as "provider/model" strings.
func LoadFavorites() []string {
	path, err := getModelStatePath()
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var raw struct {
		Favorite []modelStateEntry `json:"favorite"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}
	out := make([]string, 0, len(raw.Favorite))
	for _, e := range raw.Favorite {
		if e.ProviderID == "" || e.ModelID == "" {
			continue
		}
		out = append(out, e.ProviderID+"/"+e.ModelID)
	}
	return out
}

func readWriteModelState(modify func(full map[string]json.RawMessage) error) error {
	path, err := getModelStatePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	// Serialize ocode's own read-modify-write cycles and give each writer its
	// own scratch file: a shared ".tmp" let two concurrent instances rename a
	// half-written file into place or silently drop each other's entries.
	unlock, err := lockModelState()
	if err != nil {
		return err
	}
	defer unlock()
	var full map[string]json.RawMessage
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &full); err != nil {
			full = nil
		}
	}
	if full == nil {
		full = make(map[string]json.RawMessage)
	}
	if err := modify(full); err != nil {
		return err
	}
	out, err := json.MarshalIndent(full, "", "  ")
	if err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.tmp.%d", path, os.Getpid())
	if err := os.WriteFile(tmp, out, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// SaveFavoriteModel adds a model to the favorites list. No-op if already favorited.
func SaveFavoriteModel(providerModel string) error {
	provID, modelID := splitProviderModel(providerModel)
	if provID == "" || modelID == "" {
		return fmt.Errorf("invalid provider/model id: %q", providerModel)
	}
	return readWriteModelState(func(full map[string]json.RawMessage) error {
		var fav []modelStateEntry
		if raw, ok := full["favorite"]; ok {
			_ = json.Unmarshal(raw, &fav)
		}
		for _, e := range fav {
			if e.ProviderID == provID && e.ModelID == modelID {
				return nil
			}
		}
		fav = append(fav, modelStateEntry{ProviderID: provID, ModelID: modelID})
		newFav, err := json.Marshal(fav)
		if err != nil {
			return err
		}
		full["favorite"] = newFav
		return nil
	})
}

// RemoveFavoriteModel removes a model from the favorites list.
func RemoveFavoriteModel(providerModel string) error {
	provID, modelID := splitProviderModel(providerModel)
	if provID == "" || modelID == "" {
		return fmt.Errorf("invalid provider/model id: %q", providerModel)
	}
	return readWriteModelState(func(full map[string]json.RawMessage) error {
		var fav []modelStateEntry
		if raw, ok := full["favorite"]; ok {
			_ = json.Unmarshal(raw, &fav)
		}
		filtered := make([]modelStateEntry, 0, len(fav))
		for _, e := range fav {
			if e.ProviderID == provID && e.ModelID == modelID {
				continue
			}
			filtered = append(filtered, e)
		}
		newFav, err := json.Marshal(filtered)
		if err != nil {
			return err
		}
		full["favorite"] = newFav
		return nil
	})
}

// IsFavorite checks whether the given "provider/model" is favorited.
func IsFavorite(providerModel string) bool {
	for _, f := range LoadFavorites() {
		if f == providerModel {
			return true
		}
	}
	return false
}
