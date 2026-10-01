package config

import (
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestSeedQuickActionsIsTodayThreePills(t *testing.T) {
	got := SeedQuickActions().Chips
	if len(got) != 3 {
		t.Fatalf("seed chip count = %d, want 3", len(got))
	}
	want := []QuickActionChip{
		{ID: "compact", Label: "Compact", Icon: "archive", Message: "/compact", Mode: QuickActionModeSend, Seed: QuickActionSeedCompact},
		{ID: "continue", Label: "Continue", Icon: "play", Message: "continue", Mode: QuickActionModeSend, Seed: QuickActionSeedContinue},
		{ID: "recap", Label: "Recap", Icon: "file-text", Message: "/recap", Mode: QuickActionModeSend, Seed: QuickActionSeedRecap},
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("seed chip %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestSeedQuickActionsIsFreshEachCall(t *testing.T) {
	// The seed is returned into a handler's cached config; a shared backing
	// array would let one client's mutation leak into another's response.
	first := SeedQuickActions()
	first.Chips[0].Label = "mutated"
	if second := SeedQuickActions(); second.Chips[0].Label != "Compact" {
		t.Fatalf("seed leaked mutation across calls: %q", second.Chips[0].Label)
	}
}

func TestNormalizeQuickActionsFillsOmittedFields(t *testing.T) {
	got := NormalizeQuickActions(QuickActionsConfig{Chips: []QuickActionChip{{ID: "a", Label: "A", Message: "hi"}}})
	c := got.Chips[0]
	if c.Mode != QuickActionModeSend {
		t.Errorf("omitted mode = %q, want %q (send)", c.Mode, QuickActionModeSend)
	}
	if c.Icon != QuickActionDefaultIcon {
		t.Errorf("omitted icon = %q, want %q", c.Icon, QuickActionDefaultIcon)
	}
}

func TestNormalizeQuickActionsPreservesUnknownValuesForValidation(t *testing.T) {
	// Normalize must not silently repair a bad mode, or Validate can never
	// report it. Preserving it is what makes the rejection reachable.
	got := NormalizeQuickActions(QuickActionsConfig{Chips: []QuickActionChip{{ID: "a", Label: "A", Message: "m", Mode: "sideways"}}})
	if got.Chips[0].Mode != "sideways" {
		t.Fatalf("normalize rewrote an invalid mode to %q; it must preserve it for Validate", got.Chips[0].Mode)
	}
}

func TestNormalizeQuickActionsPreservesExplicitIcon(t *testing.T) {
	// Symmetric with the mode case: an unknown-but-present icon must reach
	// Validate too, or a stale lucide key would silently render as zap.
	got := NormalizeQuickActions(QuickActionsConfig{Chips: []QuickActionChip{{ID: "a", Label: "A", Message: "m", Icon: "retired-icon"}}})
	if got.Chips[0].Icon != "retired-icon" {
		t.Fatalf("normalize rewrote an invalid icon to %q; it must preserve it for Validate", got.Chips[0].Icon)
	}
}

func makeChips(n int, base QuickActionChip) []QuickActionChip {
	out := make([]QuickActionChip, 0, n)
	for i := 0; i < n; i++ {
		c := base
		c.ID = "chip-" + strconv.Itoa(i)
		out = append(out, c)
	}
	return out
}

func TestQuickActionsValidate(t *testing.T) {
	base := QuickActionChip{ID: "a", Label: "A", Icon: "zap", Message: "m", Mode: QuickActionModeSend}
	mutate := func(f func(*QuickActionChip)) QuickActionChip {
		c := base
		f(&c)
		return c
	}
	dup := QuickActionsConfig{Chips: []QuickActionChip{base, base}}
	// A stale client whose minted id collided with a shipped starter slug.
	// Two chips then share a React key, so one pill vanishes with no error.
	reservedClash := QuickActionsConfig{Chips: []QuickActionChip{
		{ID: "compact", Label: "Compact", Icon: "archive", Message: "/compact", Mode: QuickActionModeSend, Seed: QuickActionSeedCompact},
		{ID: "compact", Label: "New", Icon: "zap", Message: "hello", Mode: QuickActionModeSend},
	}}

	cases := []struct {
		name   string
		cfg    QuickActionsConfig
		wantIs string // substring the error must contain; "" means expect nil
	}{
		{"valid single chip", QuickActionsConfig{Chips: []QuickActionChip{base}}, ""},
		{"empty list is legal", QuickActionsConfig{Chips: []QuickActionChip{}}, ""},
		{"20 chips ok", QuickActionsConfig{Chips: makeChips(20, base)}, ""},
		{"21 chips rejected", QuickActionsConfig{Chips: makeChips(21, base)}, "at most 20"},
		{"empty id", QuickActionsConfig{Chips: []QuickActionChip{mutate(func(c *QuickActionChip) { c.ID = "" })}}, "id must not be empty"},
		{"empty label", QuickActionsConfig{Chips: []QuickActionChip{mutate(func(c *QuickActionChip) { c.Label = "" })}}, "label"},
		{"whitespace-only label", QuickActionsConfig{Chips: []QuickActionChip{mutate(func(c *QuickActionChip) { c.Label = "  \t " })}}, "label"},
		{"whitespace-only message", QuickActionsConfig{Chips: []QuickActionChip{mutate(func(c *QuickActionChip) { c.Message = "\n  " })}}, "message"},
		{"unknown icon", QuickActionsConfig{Chips: []QuickActionChip{mutate(func(c *QuickActionChip) { c.Icon = "nope-icon" })}}, "icon"},
		{"unknown mode", QuickActionsConfig{Chips: []QuickActionChip{mutate(func(c *QuickActionChip) { c.Mode = "sideways" })}}, "mode"},
		{"unknown seed", QuickActionsConfig{Chips: []QuickActionChip{mutate(func(c *QuickActionChip) { c.Seed = "invented" })}}, "seed"},
		{"duplicate id", dup, "duplicate"},
		{"minted id collides with a starter slug", reservedClash, "duplicate"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := NormalizeQuickActions(tc.cfg).Validate()
			if tc.wantIs == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() = nil, want an error containing %q", tc.wantIs)
			}
			if !strings.Contains(err.Error(), tc.wantIs) {
				t.Fatalf("Validate() = %q, want it to contain %q", err.Error(), tc.wantIs)
			}
		})
	}
}

func TestValidQuickActionIconCoversEverySeedIconAndRejectsTheRest(t *testing.T) {
	for _, chip := range SeedQuickActions().Chips {
		if !ValidQuickActionIcon(chip.Icon) {
			t.Errorf("seed icon %q rejected by the allowlist", chip.Icon)
		}
	}
	if !ValidQuickActionIcon(QuickActionDefaultIcon) {
		t.Errorf("default icon %q rejected by the allowlist", QuickActionDefaultIcon)
	}
	if ValidQuickActionIcon("definitely-not-an-icon") {
		t.Error("allowlist accepted an unknown icon")
	}
	if got := len(quickActionIconAllowlist); got != 24 {
		t.Errorf("allowlist size = %d, want 24", got)
	}
}

// wantQuickActionIconKeys is transcribed from the spec's "Icon allowlist"
// section independently of the map in quick_actions.go. The spec requires the
// Go set and the TS key->component map to be the same set and a mismatch to be
// a test failure, so this pins the exact membership: a size assertion alone
// cannot catch a key renamed on one side, which is precisely the silent-blank
// icon the allowlist exists to prevent. Task 5 pins the same list in TS.
var wantQuickActionIconKeys = []string{
	"zap", "archive", "play", "file-text", "search",
	"refresh-cw", "terminal", "git-branch", "hammer", "bug",
	"flask-conical", "shield-check", "list-checks", "wand-sparkles",
	"package", "book-open", "file-code", "messages-square",
	"rocket", "scissors", "wrench", "eye", "gauge",
	"chart-no-axes-column",
}

func TestQuickActionIconAllowlistMatchesTheSpecKeySet(t *testing.T) {
	want := make(map[string]bool, len(wantQuickActionIconKeys))
	for _, key := range wantQuickActionIconKeys {
		want[key] = true
	}
	if len(want) != len(wantQuickActionIconKeys) {
		t.Fatalf("the expected key list repeats an entry: %v", wantQuickActionIconKeys)
	}
	for _, key := range wantQuickActionIconKeys {
		if !ValidQuickActionIcon(key) {
			t.Errorf("allowlist is missing spec key %q", key)
		}
	}
	for key := range quickActionIconAllowlist {
		if !want[key] {
			t.Errorf("allowlist has key %q, which the spec does not list", key)
		}
	}
}

func TestQuickActionChipRejectsUnknownJSONField(t *testing.T) {
	// A typo'd key must surface, not vanish and take the user's setting with it.
	var cfg QuickActionsConfig
	err := json.Unmarshal([]byte(`{"chips":[{"id":"a","label":"A","icon":"zap","message":"m","mode":"send","colour":"red"}]}`), &cfg)
	if err == nil {
		t.Fatal("unknown field \"colour\" was accepted; it must be rejected")
	}
	if !strings.Contains(err.Error(), "colour") {
		t.Fatalf("error %q does not name the offending field", err.Error())
	}
}

func TestQuickActionsConfigMarshalMatchesTheSpecDataModel(t *testing.T) {
	// This exact shape is both the persisted `quick_actions` block and the GET
	// body, so the tag names, the tag order, and `seed` being the ONLY optional
	// field are all part of the contract. A custom chip must not persist
	// `"seed":""`, and a chip decoded from these tags must compare equal to what
	// went in, or a round-trip silently rewrites the user's settings.
	custom := QuickActionsConfig{Chips: []QuickActionChip{
		{ID: "chip-1", Label: "Tests", Icon: "flask-conical", Message: "run the tests", Mode: QuickActionModeFill},
	}}
	got, err := json.Marshal(custom)
	if err != nil {
		t.Fatalf("marshal custom chip: %v", err)
	}
	wantJSON := `{"chips":[{"id":"chip-1","label":"Tests","icon":"flask-conical","message":"run the tests","mode":"fill"}]}`
	if string(got) != wantJSON {
		t.Fatalf("custom chip JSON = %s, want %s", got, wantJSON)
	}

	seedJSON, err := json.Marshal(SeedQuickActions())
	if err != nil {
		t.Fatalf("marshal seed: %v", err)
	}
	if !strings.Contains(string(seedJSON), `"seed":"continue"`) {
		t.Fatalf("seed JSON = %s, want it to carry the continue seed marker", seedJSON)
	}
	var back QuickActionsConfig
	if err := json.Unmarshal(seedJSON, &back); err != nil {
		t.Fatalf("seed JSON does not survive a round trip: %v", err)
	}
	if !slices.Equal(back.Chips, SeedQuickActions().Chips) {
		t.Fatalf("round trip changed the seed: got %+v", back.Chips)
	}
}

func TestQuickActionsConfigRejectsNullChip(t *testing.T) {
	// Without the guard, `null` decodes as a zero chip and surfaces as a
	// confusing "id must not be empty" instead of naming the real problem.
	var cfg QuickActionsConfig
	err := json.Unmarshal([]byte(`{"chips":[null]}`), &cfg)
	if err == nil {
		t.Fatal("a null chip was accepted; it must be rejected")
	}
	if !strings.Contains(err.Error(), "must be an object") {
		t.Fatalf("error %q does not explain that a chip must be an object", err.Error())
	}
}

func TestQuickActionsConfigRejectsNullChips(t *testing.T) {
	var cfg QuickActionsConfig
	if err := json.Unmarshal([]byte(`{"chips":null}`), &cfg); err != nil {
		t.Fatalf("null chips rejected: %v", err)
	}
	if len(cfg.Chips) != 0 {
		t.Fatalf("null chips decoded to %d entries", len(cfg.Chips))
	}
}
