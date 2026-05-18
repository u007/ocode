package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// opencode persists the user's recent / favourite model selections in
// ${XDG_STATE_HOME}/opencode/model.json. We read it as a fallback for
// the default model and write back to it when the user picks a model
// in the TUI so the two CLIs stay in sync.

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

	path, err := getModelStatePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	var full map[string]json.RawMessage
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &full); err != nil {
			full = nil
		}
	}
	if full == nil {
		full = make(map[string]json.RawMessage)
	}

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

	out, err := json.MarshalIndent(full, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, out, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
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
	tmp := path + ".tmp"
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
