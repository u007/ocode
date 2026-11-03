package config

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// MaxPromptOverrideBytes caps the stored text of one override. The text is
// appended to its prompt on every call, so an unbounded paste would inflate
// every system prompt and judge request that reads it.
const MaxPromptOverrideBytes = 32 * 1024

// PromptOverride is the user's setting for one built-in system prompt.
//
// Disabled removes that prompt from every request that would carry it. A
// prompt whose instructions are the only thing a caller has (a judge rubric, a
// helper instruction) turns that capability off instead of sending an empty
// rubric: call sites ask ResolvePrompt, and the ones that cannot run without
// their prompt check PromptDisabled first.
//
// Text is the add-on appended to the built-in prompt while the prompt is
// enabled. It is kept while the prompt is disabled so re-enabling restores it,
// and it has no effect until then.
//
// Stored JSON is {"disabled":true,"text":"..."}. The schema this replaced was
// append-only and stored {"enabled":bool,"text":"..."}; UnmarshalJSON migrates
// those entries.
type PromptOverride struct {
	Disabled bool   `json:"disabled,omitempty"`
	Text     string `json:"text,omitempty"`
}

// promptOverrideFileJSON is the on-disk shape. It accepts both the current
// "disabled" field and the legacy "enabled" one.
type promptOverrideFileJSON struct {
	Enabled  *bool  `json:"enabled"`
	Disabled *bool  `json:"disabled"`
	Text     string `json:"text"`
}

// UnmarshalJSON reads both stored schemas. "disabled" wins when present, so a
// file written by this version round-trips exactly.
//
// A legacy entry with enabled:false meant "keep the built-in prompt, ignore the
// add-on text". The prompt therefore stays enabled and the inert text is
// dropped: the current schema has no state for text that is saved but switched
// off, and silently starting to apply that text would change the prompt without
// the user asking.
func (o *PromptOverride) UnmarshalJSON(data []byte) error {
	var raw promptOverrideFileJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	o.Disabled = false
	o.Text = raw.Text
	if raw.Disabled != nil {
		o.Disabled = *raw.Disabled
		return nil
	}
	if raw.Enabled != nil && !*raw.Enabled {
		o.Text = ""
	}
	return nil
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

// PromptDisabled reports whether the user switched the built-in prompt for key
// off. A key with no stored entry is enabled, which is the default.
func PromptDisabled(key string) bool {
	o, ok := PromptOverrideFor(key)
	return ok && o.Disabled
}

// EffectivePromptOverride returns the trimmed add-on text for key and true when
// an enabled, non-blank add-on exists; otherwise "", false. It is the input to
// the cache salts that must change when the user edits an add-on, and the
// single definition of "custom text is in effect".
func EffectivePromptOverride(key string) (string, bool) {
	o, ok := PromptOverrideFor(key)
	if !ok || o.Disabled {
		return "", false
	}
	text := strings.TrimSpace(o.Text)
	if text == "" {
		return "", false
	}
	return text, true
}

// ResolvePrompt returns the text to send for key and whether the prompt should
// be sent at all. A disabled prompt yields ("", false) so the caller omits the
// fragment or system message instead of sending an empty one. An enabled prompt
// yields the built-in text with the trimmed add-on appended after a blank line,
// or the built-in text unchanged when the add-on is blank.
func ResolvePrompt(key, builtin string) (string, bool) {
	o, ok := PromptOverrideFor(key)
	if !ok {
		return builtin, true
	}
	if o.Disabled {
		return "", false
	}
	text := strings.TrimSpace(o.Text)
	if text == "" {
		return builtin, true
	}
	return builtin + "\n\n" + text, true
}

// ApplyPromptOverride returns the text of the prompt for key, or "" when the
// user disabled it. Prefer ResolvePrompt where the prompt can be omitted; this
// helper exists for call sites that only need the text.
func ApplyPromptOverride(key, builtin string) string {
	text, _ := ResolvePrompt(key, builtin)
	return text
}

// SavePromptOverride persists the override for key. An enabled prompt with no
// add-on text is the default, so that entry is removed from the file to keep it
// clean. A disabled prompt is a real setting and is kept even with no text. On
// success the live store is re-seeded from the file contents that were written.
// Text over MaxPromptOverrideBytes is rejected before the file is touched.
func SavePromptOverride(key string, o PromptOverride) error {
	if len(o.Text) > MaxPromptOverrideBytes {
		return fmt.Errorf("prompt override %q is %d bytes, limit is %d", key, len(o.Text), MaxPromptOverrideBytes)
	}
	var written map[string]PromptOverride
	err := withOcodeConfigLock(func(cfg *OcodeConfig) error {
		if cfg.Prompts == nil {
			cfg.Prompts = make(map[string]PromptOverride)
		}
		if !o.Disabled && strings.TrimSpace(o.Text) == "" {
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
