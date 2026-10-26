package config

import (
	"fmt"
	"strings"
	"sync"
)

// MaxPromptOverrideBytes caps the stored text of one override. The text is
// appended to its prompt on every call, so an unbounded paste would inflate
// every system prompt and judge request that reads it.
const MaxPromptOverrideBytes = 32 * 1024

// PromptOverride is the user's refinement of one built-in system prompt. When
// Enabled and Text is non-blank, Text is appended after the built-in prompt;
// otherwise the built-in prompt is used unchanged.
type PromptOverride struct {
	Enabled bool   `json:"enabled,omitempty"`
	Text    string `json:"text,omitempty"`
}

// promptOverrides is the process-wide copy of OcodeConfig.Prompts that prompt
// call sites read. The web server hands every agent session the same *Config,
// so an agent reading cfg.Ocode.Prompts would race a settings save that mutates
// it. The store is replaced whole, never mutated in place: LoadOcodeConfig
// seeds it on every load and SavePromptOverride re-seeds it after a successful
// write.
var (
	promptOverridesMu sync.RWMutex
	promptOverrides   map[string]PromptOverride
)

// SetPromptOverrides replaces the process-wide prompt override store with a
// copy of m. A nil m clears it.
func SetPromptOverrides(m map[string]PromptOverride) {
	cp := make(map[string]PromptOverride, len(m))
	for k, v := range m {
		cp[k] = v
	}
	promptOverridesMu.Lock()
	promptOverrides = cp
	promptOverridesMu.Unlock()
}

// PromptOverrideFor returns the override stored for key and whether one exists.
func PromptOverrideFor(key string) (PromptOverride, bool) {
	promptOverridesMu.RLock()
	defer promptOverridesMu.RUnlock()
	o, ok := promptOverrides[key]
	return o, ok
}

// EffectivePromptOverride returns the trimmed override text for key and true
// when an enabled, non-blank override exists; otherwise "", false. It is the
// single definition of "an override is in effect" that ApplyPromptOverride
// and every cache salt derive from.
func EffectivePromptOverride(key string) (string, bool) {
	o, ok := PromptOverrideFor(key)
	if !ok || !o.Enabled {
		return "", false
	}
	text := strings.TrimSpace(o.Text)
	if text == "" {
		return "", false
	}
	return text, true
}

// ApplyPromptOverride returns builtin with the user's text appended after a
// blank line when the override for key is enabled and has non-blank text.
// Otherwise it returns builtin unchanged.
func ApplyPromptOverride(key, builtin string) string {
	text, ok := EffectivePromptOverride(key)
	if !ok {
		return builtin
	}
	return builtin + "\n\n" + text
}

// SavePromptOverride persists the override for key. An entry that is disabled
// and blank is removed from the file so it stays clean. On success the live
// store is re-seeded from the file contents that were written. Text over
// MaxPromptOverrideBytes is rejected before the file is touched.
func SavePromptOverride(key string, o PromptOverride) error {
	if len(o.Text) > MaxPromptOverrideBytes {
		return fmt.Errorf("prompt override %q is %d bytes, limit is %d", key, len(o.Text), MaxPromptOverrideBytes)
	}
	var written map[string]PromptOverride
	err := withOcodeConfigLock(func(cfg *OcodeConfig) error {
		if cfg.Prompts == nil {
			cfg.Prompts = make(map[string]PromptOverride)
		}
		if !o.Enabled && strings.TrimSpace(o.Text) == "" {
			delete(cfg.Prompts, key)
		} else {
			cfg.Prompts[key] = o
		}
		written = cfg.Prompts
		return nil
	})
	if err != nil {
		return err
	}
	SetPromptOverrides(written)
	return nil
}
