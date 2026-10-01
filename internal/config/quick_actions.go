package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// Quick-action chip modes. `fill` puts the message in the composer for review;
// `send` dispatches it through the same pipeline as a typed message.
const (
	QuickActionModeFill = "fill"
	QuickActionModeSend = "send"
)

// Seed markers. They exist only on the three shipped starters and carry the
// derived behaviour the strip needs; they are never user-editable.
const (
	QuickActionSeedCompact  = "compact"
	QuickActionSeedContinue = "continue"
	QuickActionSeedRecap    = "recap"
)

const (
	// QuickActionsMaxChips bounds the strip above the composer and the settings
	// list. A longer strip wraps to several rows and becomes unusable.
	QuickActionsMaxChips = 20
	// QuickActionDefaultIcon is applied when a chip omits one, and is also the
	// render-time fallback if an icon key ever disappears from lucide-react.
	QuickActionDefaultIcon = "zap"
)

// quickActionIconAllowlist is the validation authority for chip icons. Every
// key is a real export of the pinned lucide-react version. The TS side keeps a
// key -> component map for rendering; a key present in one and not the other is
// a typecheck/test failure, never a silent blank.
var quickActionIconAllowlist = map[string]struct{}{
	"zap": {}, "archive": {}, "play": {}, "file-text": {}, "search": {},
	"refresh-cw": {}, "terminal": {}, "git-branch": {}, "hammer": {}, "bug": {},
	"flask-conical": {}, "shield-check": {}, "list-checks": {}, "wand-sparkles": {},
	"package": {}, "book-open": {}, "file-code": {}, "messages-square": {},
	"rocket": {}, "scissors": {}, "wrench": {}, "eye": {}, "gauge": {},
	"chart-no-axes-column": {},
}

// ValidQuickActionIcon reports whether icon is in the allowlist.
func ValidQuickActionIcon(icon string) bool {
	_, ok := quickActionIconAllowlist[icon]
	return ok
}

// QuickActionChip is one pill in the composer's quick-action strip. Label,
// Icon, Message and Mode are the user-controlled surface; Seed is a hidden
// marker carried only by the three shipped starters.
type QuickActionChip struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Icon    string `json:"icon"`
	Message string `json:"message"`
	Mode    string `json:"mode"`
	Seed    string `json:"seed,omitempty"`
}

// UnmarshalJSON rejects an unknown field so a typo'd key surfaces instead of
// silently disappearing. It is deliberately NOT applied to Mode/Icon: an
// omitted value stays empty so Normalize can fill it, and an explicitly
// invalid one stays invalid so Validate can reject it.
func (c *QuickActionChip) UnmarshalJSON(data []byte) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return fmt.Errorf("quick action chip must be an object")
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	allowed := map[string]bool{
		"id": true, "label": true, "icon": true, "message": true, "mode": true, "seed": true,
	}
	for key := range raw {
		if !allowed[key] {
			return fmt.Errorf("unknown quick action chip field %q", key)
		}
	}
	var plain struct {
		ID      string `json:"id"`
		Label   string `json:"label"`
		Icon    string `json:"icon"`
		Message string `json:"message"`
		Mode    string `json:"mode"`
		Seed    string `json:"seed"`
	}
	if err := json.Unmarshal(data, &plain); err != nil {
		return err
	}
	*c = QuickActionChip{
		ID: plain.ID, Label: plain.Label, Icon: plain.Icon,
		Message: plain.Message, Mode: plain.Mode, Seed: plain.Seed,
	}
	return nil
}

// QuickActionsConfig is the persisted strip. An empty Chips list is legal and
// means "no strip".
type QuickActionsConfig struct {
	Chips []QuickActionChip `json:"chips"`
}

// SeedQuickActions returns the three starter chips, reproducing the strip that
// shipped before this feature was configurable. A fresh slice is returned on
// every call so a handler's cached value cannot be mutated through it.
func SeedQuickActions() QuickActionsConfig {
	return QuickActionsConfig{Chips: []QuickActionChip{
		{ID: "compact", Label: "Compact", Icon: "archive", Message: "/compact", Mode: QuickActionModeSend, Seed: QuickActionSeedCompact},
		{ID: "continue", Label: "Continue", Icon: "play", Message: "continue", Mode: QuickActionModeSend, Seed: QuickActionSeedContinue},
		{ID: "recap", Label: "Recap", Icon: "file-text", Message: "/recap", Mode: QuickActionModeSend, Seed: QuickActionSeedRecap},
	}}
}

// NormalizeQuickActions fills omitted fields. It deliberately preserves
// unknown values so Validate can report them instead of silently repairing
// them. The chip slice is normalized in place and returned; callers must use
// the return value rather than assume their own copy was updated.
func NormalizeQuickActions(cfg QuickActionsConfig) QuickActionsConfig {
	for i := range cfg.Chips {
		if cfg.Chips[i].Icon == "" {
			cfg.Chips[i].Icon = QuickActionDefaultIcon
		}
		if cfg.Chips[i].Mode == "" {
			cfg.Chips[i].Mode = QuickActionModeSend
		}
	}
	return cfg
}

// Validate rejects a config that could not be rendered faithfully. Callers
// validate the normalized form.
func (c QuickActionsConfig) Validate() error {
	if len(c.Chips) > QuickActionsMaxChips {
		return fmt.Errorf("quick_actions: at most %d chips are allowed, got %d", QuickActionsMaxChips, len(c.Chips))
	}
	seen := make(map[string]struct{}, len(c.Chips))
	for i, chip := range c.Chips {
		where := fmt.Sprintf("chip %d", i)
		if strings.TrimSpace(chip.ID) == "" {
			return fmt.Errorf("quick_actions: %s id must not be empty", where)
		}
		if _, dup := seen[chip.ID]; dup {
			return fmt.Errorf("quick_actions: %s has duplicate id %q", where, chip.ID)
		}
		seen[chip.ID] = struct{}{}
		if strings.TrimSpace(chip.Label) == "" {
			return fmt.Errorf("quick_actions: chip %q must have a label", chip.ID)
		}
		if strings.TrimSpace(chip.Message) == "" {
			return fmt.Errorf("quick_actions: chip %q must have a message", chip.ID)
		}
		if !ValidQuickActionIcon(chip.Icon) {
			return fmt.Errorf("quick_actions: chip %q has unknown icon %q", chip.ID, chip.Icon)
		}
		if chip.Mode != QuickActionModeFill && chip.Mode != QuickActionModeSend {
			return fmt.Errorf("quick_actions: chip %q has unknown mode %q (want %q or %q)",
				chip.ID, chip.Mode, QuickActionModeFill, QuickActionModeSend)
		}
		switch chip.Seed {
		case "", QuickActionSeedCompact, QuickActionSeedContinue, QuickActionSeedRecap:
		default:
			return fmt.Errorf("quick_actions: chip %q has unknown seed %q", chip.ID, chip.Seed)
		}
	}
	return nil
}
