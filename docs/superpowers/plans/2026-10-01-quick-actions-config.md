# Configurable Quick-Action Chips — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the three hardcoded quick-action pills below the chat composer with a user-configurable, server-persisted, sortable list of up to 20 chips, each with a label, icon, message, and a `fill`-or-`send` click behaviour.

**Architecture:** Go owns the data model, the seed of the three starter chips, and validation; the server seeds the starters on GET when the key is absent, so the TypeScript side never holds a second authoritative copy. A module-level TS store (`lib/quickActions.ts`, mirroring `lib/chatVerbosity.ts`) is shared by the composer and the settings form. The existing presentational `QuickActionsBar` is unchanged; only its input changes, from a hardcoded array to the store's chips.

**Tech Stack:** Go 1.26 (`internal/config`, `internal/server`), React 19 + TypeScript (`web/src`), Vitest + Testing Library, `@dnd-kit` for reorder, `lucide-react@1.17.0` for icons.

**Spec:** `docs/superpowers/specs/2026-10-01-quick-actions-config-design.md` — the plan argues from the spec, so read it alongside this file. Where the two disagree, the spec wins and the discrepancy is a bug in this plan.

## Global Constraints

- **Cap: 20 chips.** The server rejects `len(chips) > 20`. The form disables Add at 20.
- **Modes:** exactly `fill` or `send`. Absent normalizes to `send`.
- **Seeds:** exactly `compact`, `continue`, `recap`, or absent.
- **Reserved ids:** `compact`, `continue`, `recap` are the starter slugs. A client-minted id must never collide with one.
- **Icon allowlist (24 keys, all verified present in `lucide-react@1.17.0`):** `zap archive play file-text search refresh-cw terminal git-branch hammer bug flask-conical shield-check list-checks wand-sparkles package book-open file-code messages-square rocket scissors wrench eye gauge chart-no-axes-column`
- **Starter definitions (Go is the single source; do not duplicate this list in TS):** `{id:"compact",label:"Compact",icon:"archive",message:"/compact",mode:"send",seed:"compact"}`, `{id:"continue",label:"Continue",icon:"play",message:"continue",mode:"send",seed:"continue"}`, `{id:"recap",label:"Recap",icon:"file-text",message:"/recap",mode:"send",seed:"recap"}`
- **No default is written to disk.** GET returns seeds when the key is absent; nothing persists until the user saves. A fresh install must render today's exact three pills in order.
- **`fill` replaces the draft** (does not append) and does not send, queue, or clear.
- **Resume wins over both modes:** `seed == "continue"` and the turn is interrupted ⇒ `handleResume()`.
- **Do not modify** `web/src/components/Chat/QuickActionsBar.tsx`.
- **Do not add per-turn content to `tools` or `system`** — this must not bust the Anthropic prompt cache.
- Commands run from the repo root unless a step says otherwise. Web tests run from `web/`.
- `git checkout -- <file>` is **denied** by `.claude/settings.json` in this repo. To restore a file, use `git show HEAD:<path> > /tmp/x && cp /tmp/x <path>`.
- **Never write a file from a value read from that same file in one expression.** Read into a variable, assert non-empty, then write. This exact idiom destroyed a 6446-line source file once.
- Commit with explicit paths: `git add <paths>` then `git commit -m "..."`. The working tree carries heavy concurrent uncommitted WIP; never `git add -A`.

## Review Focus

Five input classes the spec implies that a naive implementation would get wrong. Each is pinned by a test in the task that owns the code.

1. **Whitespace-only `message` or `label`.** A user pasting a prompt with a trailing newline gets a dead pill that silently does nothing. A reasonable person expects a clear rejection, not a no-op button. → Task 1 (Go rejects), Task 6 (form surfaces the server's message).
2. **Duplicate `id`, including a minted id that collides with a reserved starter slug.** Two pills sharing a React key make one vanish with no error. A reasonable person expects either an error or a uniquified id, never a silently missing pill. → Task 1 (Go rejects), Task 5 (mint is collision-proof), Task 6 (form surfaces).
3. **A chip is edited or deleted while a click from it is still queued.** The queue holds resolved *text*, not a chip id, so the pending dispatch must still run with the text as it was at click time. A reasonable person expects their queued message to send unchanged. → Task 7.
4. **Two settings panes at 19 chips both add one (21 total).** The server rejects; the form must surface that message and keep the local list intact. A reasonable person expects their 21st chip to be *explained*, never silently truncated. → Task 1 (rejects >20), Task 3 (400 with a readable message), Task 6 (renders it, does not clear the draft).
5. **An icon key that stops existing after a `lucide-react` upgrade.** A reasonable person expects a working pill with a generic icon, not a blank pill or a crash. → Task 5 (render-time fallback to `zap`, no throw).

---

## File Structure

**New — Go**
- `internal/config/quick_actions.go` — chip model, icon allowlist, seed, normalize, validate, cap. Focused file matching the repo's `computeruse_config.go` / `systempermissions_config.go` convention.
- `internal/config/quick_actions_test.go`
- `internal/server/handler_quick_actions.go` — GET/PUT handlers.
- `internal/server/handler_quick_actions_test.go` — handler-level.
- `internal/server/handler_quick_actions_routes_test.go` — drives the **real mux**, because a handler-only test cannot catch a missing route (the `handler_terminal_tabs_routes_test.go` lesson).

**New — TypeScript**
- `web/src/lib/quickActions.ts` — types, pure helpers, icon map, module store, `useQuickActions`.
- `web/src/lib/quickActions.test.ts`
- `web/src/components/Settings/QuickActionsForm.tsx`
- `web/src/components/Settings/QuickActionsForm.test.tsx`

**Modified**
- `internal/config/ocodeconfig.go` — `QuickActions` on `ocodeConfigFile` and `OcodeConfig`; `delete(raw, "quick_actions")`; load-time application; the allowed-keys list at ~line 2471.
- `internal/server/server.go` — two routes beside the chat-verbosity pair (~line 456).
- `web/src/api/types.ts` — `QuickActionChip`, `QuickActionsConfig`, `QuickActionsResponse`.
- `web/src/api/client.ts` — `getQuickActionsConfig`, `setQuickActionsConfig`.
- `web/src/components/Settings/SettingsPanel.tsx` — union member (~line 46), section entry (~line 85), switch case (~line 142).
- `web/src/components/Chat/ChatInput.tsx` — replace the `quickActions` literal (line 691) and the `runQuickAction` switch (line 669); replace the `hasConversation &&` wrapper (line 1358).
- `web/src/components/Chat/ChatInput.quickActions.test.tsx` — **rewrite**; it asserts on the hardcoded pills and will fail the moment they come from the store.
- `skills/ocode-web/SKILL.md`, `CHANGES.md`.

**Docs bundle** — one concept page via the **context sub-agent** (`doc_write`), since `docs/` is OKF-bundle-owned and the main agent must not write there. Do not hand-edit `docs/index.md` or `docs/log.md`; they are auto-managed. Budget ~30 minutes for that dispatch.

---

### Task 1: Chip model, seed, and validation (Go)

**Files:**
- Create: `internal/config/quick_actions.go`
- Test: `internal/config/quick_actions_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `type QuickActionChip struct { ID, Label, Icon, Message, Mode, Seed string }` with json tags `id`, `label`, `icon`, `message`, `mode`, `seed,omitempty`.
  - `type QuickActionsConfig struct { Chips []QuickActionChip \`json:"chips"\` }`
  - `const QuickActionModeFill = "fill"`, `QuickActionModeSend = "send"`
  - `const QuickActionSeedCompact = "compact"`, `QuickActionSeedContinue = "continue"`, `QuickActionSeedRecap = "recap"`
  - `const QuickActionsMaxChips = 20`
  - `const QuickActionDefaultIcon = "zap"`
  - `var quickActionIconAllowlist = map[string]struct{}{...}` (24 keys) and `func ValidQuickActionIcon(icon string) bool`
  - `func SeedQuickActions() QuickActionsConfig`
  - `func NormalizeQuickActions(cfg QuickActionsConfig) QuickActionsConfig`
  - `func (c QuickActionsConfig) Validate() error`

- [ ] **Step 1: Write the failing test**

Create `internal/config/quick_actions_test.go`:

```go
package config

import (
	"encoding/json"
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

	cases := []struct {
		name   string
		cfg    QuickActionsConfig
		wantIs string // substring the error must contain; "" means expect nil
	}{
		{"valid single chip", QuickActionsConfig{Chips: []QuickActionChip{base}}, ""},
		{"empty list is legal", QuickActionsConfig{Chips: []QuickActionChip{}}, ""},
		{"20 chips ok", QuickActionsConfig{Chips: makeChips(20, base)}, ""},
		{"21 chips rejected", QuickActionsConfig{Chips: makeChips(21, base)}, "at most 20"},
		{"empty id", QuickActionsConfig{Chips: []QuickActionChip{mutate(func(c *QuickActionChip) { c.ID = "" })}}, "id"},
		{"empty label", QuickActionsConfig{Chips: []QuickActionChip{mutate(func(c *QuickActionChip) { c.Label = "" })}}, "label"},
		{"whitespace-only label", QuickActionsConfig{Chips: []QuickActionChip{mutate(func(c *QuickActionChip) { c.Label = "  \t " })}}, "label"},
		{"whitespace-only message", QuickActionsConfig{Chips: []QuickActionChip{mutate(func(c *QuickActionChip) { c.Message = "\n  " })}}, "message"},
		{"unknown icon", QuickActionsConfig{Chips: []QuickActionChip{mutate(func(c *QuickActionChip) { c.Icon = "nope-icon" })}}, "icon"},
		{"unknown mode", QuickActionsConfig{Chips: []QuickActionChip{mutate(func(c *QuickActionChip) { c.Mode = "sideways" })}}, "mode"},
		{"unknown seed", QuickActionsConfig{Chips: []QuickActionChip{mutate(func(c *QuickActionChip) { c.Seed = "invented" })}}, "seed"},
		{"duplicate id", dup, "duplicate"},
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
```

Also add, in the same file, the strict-decode and allowlist tests:

```go
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

func TestQuickActionsConfigRejectsNullChips(t *testing.T) {
	var cfg QuickActionsConfig
	if err := json.Unmarshal([]byte(`{"chips":null}`), &cfg); err != nil {
		t.Fatalf("null chips rejected: %v", err)
	}
	if len(cfg.Chips) != 0 {
		t.Fatalf("null chips decoded to %d entries", len(cfg.Chips))
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /Users/james/www/ocode && go test ./internal/config/ -run 'QuickAction' -count=1`
Expected: compile failure — `undefined: SeedQuickActions` etc.

- [ ] **Step 3: Write the implementation**

Create `internal/config/quick_actions.go`:

```go
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
// them.
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
```

**Do not add**

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /Users/james/www/ocode && go test ./internal/config/ -run 'QuickAction' -count=1 -v 2>&1 | tail -40`
Expected: PASS for every subtest of `TestQuickActionsValidate`, plus the seed/normalize/allowlist/strict-decode tests.

- [ ] **Step 5: Commit**

```bash
git add internal/config/quick_actions.go internal/config/quick_actions_test.go
git commit -m "feat(config): quick-action chip model, seed, and validation"
```

---

### Task 2: Persist and load wiring (Go)

**Files:**
- Modify: `internal/config/ocodeconfig.go`
- Test: `internal/config/quick_actions_test.go` (append)

**Interfaces:**
- Consumes: `QuickActionsConfig`, `NormalizeQuickActions`, `QuickActionsConfig.Validate` from Task 1.
- Produces: `OcodeConfig.QuickActions QuickActionsConfig` (field `quick_actions,omitempty`), `SaveOcodeQuickActions(cfg QuickActionsConfig) error`, and load-time seeding so an absent key yields `SeedQuickActions()`.

- [ ] **Step 1: Write the failing test**

Append to `internal/config/quick_actions_test.go`:

```go
func TestSaveAndLoadOcodeQuickActionsRoundTrips(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	saved := QuickActionsConfig{Chips: []QuickActionChip{
		{ID: "tests", Label: "Run tests", Icon: "flask-conical", Message: "run the test suite", Mode: QuickActionModeFill},
		{ID: "recap", Label: "Recap", Icon: "file-text", Message: "/recap", Mode: QuickActionModeSend, Seed: QuickActionSeedRecap},
	}}
	if err := SaveOcodeQuickActions(saved); err != nil {
		t.Fatalf("SaveOcodeQuickActions: %v", err)
	}
	cfg := Config{}
	if err := LoadOcodeConfig(&cfg); err != nil {
		t.Fatalf("LoadOcodeConfig: %v", err)
	}
	if len(cfg.Ocode.QuickActions.Chips) != 2 {
		t.Fatalf("loaded %d chips, want 2: %+v", len(cfg.Ocode.QuickActions.Chips), cfg.Ocode.QuickActions.Chips)
	}
	if cfg.Ocode.QuickActions.Chips[0] != saved.Chips[0] {
		t.Errorf("chip 0 = %+v, want %+v", cfg.Ocode.QuickActions.Chips[0], saved.Chips[0])
	}
	// Order IS the sort order; losing it silently scrambles the user's arrangement.
	if cfg.Ocode.QuickActions.Chips[1].ID != "recap" {
		t.Errorf("chip 1 = %q, want recap — order must survive the round trip", cfg.Ocode.QuickActions.Chips[1].ID)
	}
}

func TestSaveOcodeQuickActionsDoesNotDisturbSiblingKeys(t *testing.T) {
	// A one-key save must not read-modify-write its way through unrelated
	// config, or saving a chip list would clobber chat_verbosity.
	t.Setenv("HOME", t.TempDir())

	if err := SaveOcodeChatVerbosity(ChatVerbosityConfig{Preset: ChatVerbosityBalanced}); err != nil {
		t.Fatalf("SaveOcodeChatVerbosity: %v", err)
	}
	if err := SaveOcodeQuickActions(SeedQuickActions()); err != nil {
		t.Fatalf("SaveOcodeQuickActions: %v", err)
	}
	cfg := Config{}
	if err := LoadOcodeConfig(&cfg); err != nil {
		t.Fatalf("LoadOcodeConfig: %v", err)
	}
	if cfg.Ocode.ChatVerbosity.Preset != ChatVerbosityBalanced {
		t.Errorf("chat_verbosity.preset = %q, want %q — the quick-actions save clobbered a sibling key",
			cfg.Ocode.ChatVerbosity.Preset, ChatVerbosityBalanced)
	}
}

func TestSaveOcodeQuickActionsRejectsInvalidWithoutWriting(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	bad := QuickActionsConfig{Chips: []QuickActionChip{{ID: "a", Label: "A", Icon: "zap", Message: "m", Mode: "sideways"}}}
	if err := SaveOcodeQuickActions(bad); err == nil {
		t.Fatal("SaveOcodeQuickActions accepted an invalid mode")
	}
}

func TestLoadOcodeConfigSeedsQuickActionsWhenAbsent(t *testing.T) {
	// The out-of-the-box guarantee: an install that never touched settings
	// still renders the three starter pills.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	cfg := Config{}
	if err := LoadOcodeConfig(&cfg); err != nil {
		t.Fatalf("LoadOcodeConfig: %v", err)
	}
	want := SeedQuickActions()
	if len(cfg.Ocode.QuickActions.Chips) != len(want.Chips) {
		t.Fatalf("absent key seeded %d chips, want %d", len(cfg.Ocode.QuickActions.Chips), len(want.Chips))
	}
	for i := range want.Chips {
		if cfg.Ocode.QuickActions.Chips[i] != want.Chips[i] {
			t.Errorf("seeded chip %d = %+v, want %+v", i, cfg.Ocode.QuickActions.Chips[i], want.Chips[i])
		}
	}
}
```

**Use the exact save-then-read-back shape below, which is verified against this repo.** `LoadOcodeConfig` has signature `func LoadOcodeConfig(cfg *Config) error` — it takes a `*Config` and returns **only** `error`, so read the value back through `cfg.Ocode`. It is not `LoadOcodeConfig()` returning two values; that form does not exist and will not compile. `LoadOcodeConfigCopy() (*OcodeConfig, error)` also exists, but `internal/config/speech_summary_test.go:81` establishes `LoadOcodeConfig(&cfg)` as the house pattern for save-then-reload, so follow that.

Set **only** `HOME` via `t.Setenv`. Sibling config tests do not set `XDG_DATA_HOME`/`XDG_CONFIG_HOME`; adding them is harmless but inconsistent. `t.Setenv("HOME", t.TempDir())` is what keeps these tests from writing into the developer's real `~/.local/share/opencode` — omitting it is a known way these tests silently clobber real config.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /Users/james/www/ocode && go test ./internal/config/ -run 'OcodeQuickActions|LoadOcodeConfigSeeds' -count=1`
Expected: compile failure — `undefined: SaveOcodeQuickActions`, and `OcodeConfig` has no `QuickActions` field.

- [ ] **Step 3: Make the edits**

In `internal/config/ocodeconfig.go`, four edits.

**(a) `ocodeConfigFile` struct** — add beside `ChatVerbosity` (around line 1157):

```go
	QuickActions           *QuickActionsConfig         `json:"quick_actions,omitempty"`
```

**(b) `OcodeConfig` struct** — add the runtime field beside `ChatVerbosity` (search for `ChatVerbosity *chatVerbosityConfigFile` and add):

```go
	QuickActions           QuickActionsConfig          `json:"quick_actions,omitempty"`
```

**(c) `LoadOcodeConfig` / the apply block** — beside the `chat_verbosity` block at ~line 1581:

```go
	if _, ok := raw["quick_actions"]; ok {
		if file.QuickActions != nil {
			cfg.QuickActions = NormalizeQuickActions(*file.QuickActions)
		} else {
			cfg.QuickActions = SeedQuickActions()
		}
		delete(raw, "quick_actions")
	} else {
		// An absent key seeds the starters WITHOUT writing a default to disk,
		// so a fresh install's ocodeconfig.json stays byte-unchanged.
		cfg.QuickActions = SeedQuickActions()
	}
```

**(d) the allowed-keys list at ~line 2471** — add `|| k == "quick_actions"` so the unknown-key sweep does not reject a key we own.

**Plus the saver**, in the new `internal/config/quick_actions.go` (append) so it sits with its model:

```go
// SaveOcodeQuickActions persists the composer's quick-action strip, replacing
// only that key.
func SaveOcodeQuickActions(cfg QuickActionsConfig) error {
	cfg = NormalizeQuickActions(cfg)
	if err := cfg.Validate(); err != nil {
		return err
	}
	return withOcodeConfigLock(func(c *OcodeConfig) error {
		c.QuickActions = cfg
		return nil
	})
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /Users/james/www/ocode && go test ./internal/config/ -count=1 2>&1 | tail -20`
Expected: PASS. The whole package must be green — the `delete(raw, ...)` and allowed-keys edits touch shared load logic and can break unrelated config tests.

- [ ] **Step 5: Commit**

```bash
git add internal/config/ocodeconfig.go internal/config/quick_actions.go internal/config/quick_actions_test.go
git commit -m "feat(config): persist and load the quick-actions strip"
```

---

### Task 3: HTTP handlers and routes (Go)

**Files:**
- Create: `internal/server/handler_quick_actions.go`
- Create: `internal/server/handler_quick_actions_test.go`
- Create: `internal/server/handler_quick_actions_routes_test.go`
- Modify: `internal/server/server.go` (beside line 456)

**Interfaces:**
- Consumes: Task 1 + Task 2 config API.
- Produces: `HandleGetQuickActionsConfig`, `HandleSetQuickActionsConfig`, and bus event name `"quick_actions_changed"`.

- [ ] **Step 1: Write the failing handler test**

Create `internal/server/handler_quick_actions_test.go`. Use the existing `testConfigHandler(t)` fixture from `handler_config_test.go:24` — it already does `t.Setenv("HOME", t.TempDir())` and installs an empty `config.Config`, which is exactly the "key absent" state the seed test needs. **Do not invent a new fixture.** The file needs the `strconv` import for `itoaTest`:

```go
package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
)

func TestHandleGetQuickActionsSeedsWhenAbsent(t *testing.T) {
	h := testConfigHandler(t)
	w := httptest.NewRecorder()
	h.HandleGetQuickActionsConfig(w, httptest.NewRequest(http.MethodGet, "/api/config/ocode/quick-actions", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET = %d body=%s", w.Code, w.Body.String())
	}
	var resp config.QuickActionsConfig
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := config.SeedQuickActions()
	if len(resp.Chips) != len(want.Chips) {
		t.Fatalf("seeded %d chips, want %d: %+v", len(resp.Chips), len(want.Chips), resp.Chips)
	}
	for i := range want.Chips {
		if resp.Chips[i] != want.Chips[i] {
			t.Errorf("chip %d = %+v, want %+v", i, resp.Chips[i], want.Chips[i])
		}
	}
}

func TestHandleSetQuickActionsRoundTrips(t *testing.T) {
	h := testConfigHandler(t)
	body := `{"chips":[
		{"id":"tests","label":"Run tests","icon":"flask-conical","message":"run the test suite","mode":"fill"},
		{"id":"recap","label":"Recap","icon":"file-text","message":"/recap","mode":"send","seed":"recap"}
	]}`
	w := httptest.NewRecorder()
	h.HandleSetQuickActionsConfig(w, httptest.NewRequest(http.MethodPut, "/api/config/ocode/quick-actions", strings.NewReader(body)))
	if w.Code != http.StatusOK {
		t.Fatalf("PUT = %d body=%s", w.Code, w.Body.String())
	}

	g := httptest.NewRecorder()
	h.HandleGetQuickActionsConfig(g, httptest.NewRequest(http.MethodGet, "/api/config/ocode/quick-actions", nil))
	var resp config.QuickActionsConfig
	if err := json.Unmarshal(g.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Chips) != 2 || resp.Chips[0].ID != "tests" || resp.Chips[0].Mode != config.QuickActionModeFill {
		t.Fatalf("round trip lost data or order: %+v", resp.Chips)
	}
}

func TestHandleSetQuickActionsRejectsOverCapWithAReadableMessage(t *testing.T) {
	// Review Focus #4: the 21st chip must be EXPLAINED, never silently dropped.
	h := testConfigHandler(t)
	var sb strings.Builder
	sb.WriteString(`{"chips":[`)
	for i := 0; i < 21; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(`{"id":"c`)
		sb.WriteString(string(rune('a' + i%26)))
		sb.WriteString(itoaTest(i))
		sb.WriteString(`","label":"C","icon":"zap","message":"m","mode":"send"}`)
	}
	sb.WriteString(`]}`)
	w := httptest.NewRecorder()
	h.HandleSetQuickActionsConfig(w, httptest.NewRequest(http.MethodPut, "/api/config/ocode/quick-actions", strings.NewReader(sb.String())))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("PUT 21 chips = %d, want 400 body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "at most 20") {
		t.Fatalf("error body %q does not explain the cap", w.Body.String())
	}
}

func itoaTest(i int) string { return strconv.Itoa(i) }

func TestHandleSetQuickActionsRejectsInvalidBody(t *testing.T) {
	h := testConfigHandler(t)
	for name, body := range map[string]string{
		"bad json":  `{`,
		"bad icon":  `{"chips":[{"id":"a","label":"A","icon":"nope","message":"m","mode":"send"}]}`,
		"bad mode":  `{"chips":[{"id":"a","label":"A","icon":"zap","message":"m","mode":"sideways"}]}`,
		"dup id":    `{"chips":[{"id":"a","label":"A","icon":"zap","message":"m","mode":"send"},{"id":"a","label":"B","icon":"zap","message":"m","mode":"send"}]}`,
		"blank msg": `{"chips":[{"id":"a","label":"A","icon":"zap","message":"   ","mode":"send"}]}`,
		"unknown f": `{"chips":[{"id":"a","label":"A","icon":"zap","message":"m","mode":"send","colour":"red"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			w := httptest.NewRecorder()
			h.HandleSetQuickActionsConfig(w, httptest.NewRequest(http.MethodPut, "/api/config/ocode/quick-actions", strings.NewReader(body)))
			if w.Code != http.StatusBadRequest {
				t.Fatalf("PUT %s = %d, want 400 body=%s", name, w.Code, w.Body.String())
			}
		})
	}
}

func TestHandleSetQuickActionsAcceptsEmptyList(t *testing.T) {
	// "I deleted every chip" is a legitimate state and means no strip.
	h := testConfigHandler(t)
	w := httptest.NewRecorder()
	h.HandleSetQuickActionsConfig(w, httptest.NewRequest(http.MethodPut, "/api/config/ocode/quick-actions", strings.NewReader(`{"chips":[]}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("PUT empty = %d body=%s", w.Code, w.Body.String())
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd /Users/james/www/ocode && go test ./internal/server/ -run 'QuickActions' -count=1`
Expected: compile failure — `undefined: HandleGetQuickActionsConfig`.

- [ ] **Step 3: Write the handlers**

Create `internal/server/handler_quick_actions.go`:

```go
package server

import (
	"net/http"

	"github.com/u007/ocode/internal/config"
)

// HandleGetQuickActionsConfig reports the composer's quick-action strip. When
// the key is absent the seeded starters are returned so an install that never
// touched settings still renders the pre-existing three pills.
func (h *Handler) HandleGetQuickActionsConfig(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	cfg := config.SeedQuickActions()
	if h.cfg != nil {
		cfg = h.cfg.Ocode.QuickActions
	}
	h.mu.Unlock()

	normalized := config.NormalizeQuickActions(cfg)
	if err := normalized.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, normalized)
}

// HandleSetQuickActionsConfig validates and persists the strip, replacing only
// the quick_actions key. The event payload is diagnostic; clients re-fetch
// because the unified bus envelope carries no host identity.
func (h *Handler) HandleSetQuickActionsConfig(w http.ResponseWriter, r *http.Request) {
	var req config.QuickActionsConfig
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	normalized := config.NormalizeQuickActions(req)
	if err := normalized.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := config.SaveOcodeQuickActions(normalized); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save quick actions config: "+err.Error())
		return
	}
	h.mu.Lock()
	if h.cfg != nil {
		h.cfg.Ocode.QuickActions = normalized
	}
	h.mu.Unlock()
	h.bus.Publish("quick_actions_changed", "", "", map[string]any{
		"config": normalized,
	})
	writeJSON(w, http.StatusOK, normalized)
}
```

- [ ] **Step 4: Register the routes**

In `internal/server/server.go`, beside lines 456–457:

```go
	s.mux.HandleFunc("GET /api/config/ocode/quick-actions", s.authMiddleware(s.handler.HandleGetQuickActionsConfig))
	s.mux.HandleFunc("PUT /api/config/ocode/quick-actions", s.authMiddleware(s.handler.HandleSetQuickActionsConfig))
```

- [ ] **Step 5: Run the handler tests**

Run: `cd /Users/james/www/ocode && go test ./internal/server/ -run 'QuickActions' -count=1 -v 2>&1 | tail -30`
Expected: PASS.

- [ ] **Step 6: Write and run the route-registration test**

Create `internal/server/handler_quick_actions_routes_test.go`, following `handler_terminal_tabs_routes_test.go` — the handler tests above call the method directly and **cannot catch a missing route**, which is exactly how a config surface 404s into the SPA fallback while every handler test stays green:

```go
package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestQuickActionsRoutesAreRegistered(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	srv := New("127.0.0.1:0", "", "", nil)
	h := srv.serveHandler()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/config/ocode/quick-actions",
		strings.NewReader(`{"chips":[{"id":"a","label":"A","icon":"zap","message":"m","mode":"send"}]}`))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT = %d body=%s (a 404 here means the route is missing, not that the handler is wrong)",
			rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/config/ocode/quick-actions", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"id":"a"`) {
		t.Fatalf("GET body did not round-trip the saved chip: %s", rec.Body.String())
	}
}
```

Run: `cd /Users/james/www/ocode && go test ./internal/server/ -run 'QuickActionsRoutes' -count=1`
Expected: PASS.

If `New(...)` or `serveHandler()` signatures differ, copy them verbatim from `handler_terminal_tabs_routes_test.go` — do not guess.

- [ ] **Step 7: Commit**

```bash
git add internal/server/handler_quick_actions.go internal/server/handler_quick_actions_test.go internal/server/handler_quick_actions_routes_test.go internal/server/server.go
git commit -m "feat(server): quick-actions config endpoint"
```

---

### Task 4: API types and client (TypeScript)

**Files:**
- Modify: `web/src/api/types.ts` (after line 190)
- Modify: `web/src/api/client.ts` (beside `getChatVerbosityConfig` ~line 1060)
- Test: `web/src/api/client.quickActions.test.ts`

**Interfaces:**
- Consumes: the JSON shape from Task 3.
- Produces:
  - `type QuickActionMode = "fill" | "send"`
  - `type QuickActionSeed = "compact" | "continue" | "recap"`
  - `interface QuickActionChip { id: string; label: string; icon: string; message: string; mode: QuickActionMode; seed?: QuickActionSeed }`
  - `interface QuickActionsConfig { chips: QuickActionChip[] }`
  - `type QuickActionsResponse = QuickActionsConfig`
  - `api.getQuickActionsConfig(): Promise<QuickActionsResponse>` — **no `host` param**; Settings is a global surface by convention (see the same note at `SettingsPanel.tsx:123`).
  - `api.setQuickActionsConfig(cfg: QuickActionsConfig): Promise<QuickActionsResponse>`

- [ ] **Step 1: Write the failing test**

Create `web/src/api/client.quickActions.test.ts`:

```ts
import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { api } from "./client";

describe("quick actions config client", () => {
  const fetchMock = vi.fn();
  beforeEach(() => {
    fetchMock.mockReset();
    vi.stubGlobal("fetch", fetchMock);
  });
  afterEach(() => vi.unstubAllGlobals());

  const ok = (body: unknown) =>
    fetchMock.mockResolvedValue({
      ok: true,
      status: 200,
      text: async () => JSON.stringify(body),
      headers: { get: () => "application/json" },
    } as unknown as Response);

  it("GETs the quick-actions config", async () => {
    ok({ chips: [{ id: "a", label: "A", icon: "zap", message: "m", mode: "send" }] });
    const got = await api.getQuickActionsConfig();
    expect(got.chips).toHaveLength(1);
    expect(fetchMock.mock.calls[0][0]).toContain("/api/config/ocode/quick-actions");
  });

  it("PUTs the quick-actions config", async () => {
    ok({ chips: [] });
    await api.setQuickActionsConfig({ chips: [] });
    const [url, init] = fetchMock.mock.calls[0];
    expect(String(url)).toContain("/api/config/ocode/quick-actions");
    expect(init?.method).toBe("PUT");
    expect(JSON.parse(String(init?.body))).toEqual({ chips: [] });
  });
});
```

Read `web/src/api/client.ts` `fetchJSON` first and match how it derives the method for a PUT (it may use a `method` argument or a dedicated helper). Also check whether a cross-origin credentials field is required on the request init; mirror what `setChatVerbosityConfig` sends so the two are byte-identical in shape.

- [ ] **Step 2: Run to verify failure**

Run: `cd /Users/james/www/ocode/web && npx vitest run src/api/client.quickActions.test.ts`
Expected: FAIL — `getQuickActionsConfig is not a function`.

- [ ] **Step 3: Add the types**

In `web/src/api/types.ts`, after the `ChatVerbosityResponse` block (~line 190):

```ts
export type QuickActionMode = "fill" | "send";
export type QuickActionSeed = "compact" | "continue" | "recap";

export interface QuickActionChip {
  id: string;
  label: string;
  icon: string;
  message: string;
  mode: QuickActionMode;
  seed?: QuickActionSeed;
}

export interface QuickActionsConfig {
  chips: QuickActionChip[];
}

export type QuickActionsResponse = QuickActionsConfig;
```

- [ ] **Step 4: Add the client methods**

In `web/src/api/client.ts`, beside `getChatVerbosityConfig`/`setChatVerbosityConfig`:

```ts
  getQuickActionsConfig: () =>
    fetchJSON<QuickActionsResponse>("/api/config/ocode/quick-actions"),
  setQuickActionsConfig: (cfg: QuickActionsConfig) =>
    fetchJSON<QuickActionsResponse>("/api/config/ocode/quick-actions", {
      method: "PUT",
      body: JSON.stringify(cfg),
    }),
```

- [ ] **Step 5: Run to verify pass**

Run: `cd /Users/james/www/ocode/web && npx vitest run src/api/client.quickActions.test.ts`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add web/src/api/types.ts web/src/api/client.ts web/src/api/client.quickActions.test.ts
git commit -m "feat(web): quick-actions api client"
```

---

### Task 5: Store and pure helpers (TypeScript)

**Files:**
- Create: `web/src/lib/quickActions.ts`
- Test: `web/src/lib/quickActions.test.ts`

**Interfaces:**
- Consumes: `QuickActionChip` etc. from Task 4; `api.getQuickActionsConfig` / `api.setQuickActionsConfig`.
- Produces:
  - `QUICK_ACTION_ICONS: readonly string[]` (24 keys, same order as Go)
  - `quickActionIconComponent(key: string): LucideIcon` — never throws; falls back to `Zap`
  - `QUICK_ACTIONS_MAX = 20`
  - `isQuickActionSeed(value: unknown): value is QuickActionSeed`
  - `isQuickActionMode(value: unknown): value is QuickActionMode`
  - `normalizeQuickActionChip(input: unknown): QuickActionChip | null` — `null` when `id`/`label`/`message` are blank after trim
  - `normalizeQuickActions(input: unknown): QuickActionChip[]`
  - `SEED_CHIPS: readonly QuickActionChip[]` — the **degraded fallback only**; see the warning below
  - `chipDispatchesCompact(chip: QuickActionChip): boolean` — true when the trimmed message equals `/compact` or is `/compact <args>`
  - `chipRequiresHistory(chip: QuickActionChip): boolean` — `chip.seed != null`
  - `visibleChips(chips: QuickActionChip[], hasConversation: boolean): QuickActionChip[]`
  - `chipDispatchKind(chip: QuickActionChip): "command" | "message"` — `"command"` when the trimmed message starts with `/`
  - `mintQuickActionId(existing: readonly QuickActionChip[]): string` — collision-proof against existing ids **and** the reserved `compact`/`continue`/`recap` slugs
  - `interface QuickActionsState { chips: QuickActionChip[]; loading: boolean; error: string | null; revision: string }`
  - `useQuickActions(): QuickActionsState`
  - `saveQuickActions(chips: QuickActionChip[]): Promise<QuickActionsState>`
  - `__resetQuickActionsForTests(): void`

- [ ] **Step 1: Write the failing test**

Create `web/src/lib/quickActions.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import {
  QUICK_ACTION_ICONS,
  QUICK_ACTIONS_MAX,
  SEED_CHIPS,
  chipDispatchKind,
  chipDispatchesCompact,
  chipRequiresHistory,
  mintQuickActionId,
  normalizeQuickActionChip,
  normalizeQuickActions,
  quickActionIconComponent,
  visibleChips,
} from "./quickActions";

describe("quick action icon allowlist", () => {
  it("declares exactly 24 icons including every seed icon", () => {
    expect(QUICK_ACTION_ICONS).toHaveLength(24);
    for (const chip of SEED_CHIPS) {
      expect(QUICK_ACTION_ICONS).toContain(chip.icon);
    }
  });

  // Review Focus #5: a key that vanishes from lucide must not blank the pill.
  it("falls back instead of throwing for an unknown icon key", () => {
    expect(() => quickActionIconComponent("not-a-real-icon")).not.toThrow();
    expect(quickActionIconComponent("not-a-real-icon")).toBe(quickActionIconComponent("zap"));
  });
});

describe("normalizeQuickActionChip", () => {
  it("fills a missing icon and mode", () => {
    const chip = normalizeQuickActionChip({ id: "a", label: "A", message: "m" });
    expect(chip).toEqual({ id: "a", label: "A", icon: "zap", message: "m", mode: "send" });
  });

  // Review Focus #1: a pasted prompt with a trailing newline must be refused,
  // not turned into a pill that silently does nothing.
  it("returns null for a whitespace-only message", () => {
    expect(normalizeQuickActionChip({ id: "a", label: "A", message: "\n  \t " })).toBeNull();
  });
  it("returns null for a whitespace-only label", () => {
    expect(normalizeQuickActionChip({ id: "a", label: "   ", message: "m" })).toBeNull();
  });
  it("returns null for a missing id", () => {
    expect(normalizeQuickActionChip({ label: "A", message: "m" })).toBeNull();
  });
  it("trims label and message", () => {
    expect(normalizeQuickActionChip({ id: "a", label: "  A  ", message: "  m  " })?.label).toBe("A");
  });
  it("preserves an invalid mode so the server can reject it", () => {
    expect(normalizeQuickActionChip({ id: "a", label: "A", message: "m", mode: "sideways" })?.mode).toBe("sideways");
  });
  it("keeps the seed when present", () => {
    expect(normalizeQuickActionChip({ id: "continue", label: "C", message: "continue", seed: "continue" })?.seed).toBe("continue");
  });
  it("keeps a valid icon", () => {
    expect(normalizeQuickActionChip({ id: "a", label: "A", message: "m", icon: "bug" })?.icon).toBe("bug");
  });
});

describe("normalizeQuickActions", () => {
  it("drops a non-array chips value and falls back to nothing", () => {
    expect(normalizeQuickActions({ chips: "nope" })).toEqual([]);
  });
  it("drops malformed entries and keeps the good ones", () => {
    const chips = normalizeQuickActions({ chips: [{ id: "a", label: "A", message: "m" }, { id: "b", label: "  " }] });
    expect(chips).toHaveLength(1);
    expect(chips[0].id).toBe("a");
  });
  it("preserves array order, because order is the sort order", () => {
    const chips = normalizeQuickActions({
      chips: [
        { id: "z", label: "Z", message: "z" },
        { id: "a", label: "A", message: "a" },
      ],
    });
    expect(chips.map((c) => c.id)).toEqual(["z", "a"]);
  });
});

describe("derived chip state", () => {
  it("detects a compaction dispatch so the pill can dim", () => {
    expect(chipDispatchesCompact({ id: "a", label: "A", icon: "zap", message: "/compact", mode: "send" })).toBe(true);
    expect(chipDispatchesCompact({ id: "a", label: "A", icon: "zap", message: "  /compact  ", mode: "send" })).toBe(true);
    expect(chipDispatchesCompact({ id: "a", label: "A", icon: "zap", message: "/compact --x", mode: "send" })).toBe(true);
    expect(chipDispatchesCompact({ id: "a", label: "A", icon: "zap", message: "continue", mode: "send" })).toBe(false);
    expect(chipDispatchesCompact({ id: "a", label: "A", icon: "zap", message: "/compacted", mode: "send" })).toBe(false);
  });

  it("marks only seeded chips as needing history", () => {
    expect(chipRequiresHistory(SEED_CHIPS[0])).toBe(true);
    expect(chipRequiresHistory({ id: "x", label: "X", icon: "zap", message: "m", mode: "fill" })).toBe(false);
  });

  it("routes a leading slash to the command pipeline", () => {
    expect(chipDispatchKind({ id: "a", label: "A", icon: "zap", message: "/recap", mode: "send" })).toBe("command");
    expect(chipDispatchKind({ id: "a", label: "A", icon: "zap", message: "  /recap", mode: "send" })).toBe("command");
    expect(chipDispatchKind({ id: "a", label: "A", icon: "zap", message: "hello", mode: "send" })).toBe("message");
  });
});

describe("visibleChips", () => {
  it("hides seeded chips on an empty session but keeps custom ones", () => {
    const custom = { id: "x", label: "X", icon: "zap", message: "run tests", mode: "fill" as const };
    expect(visibleChips([...SEED_CHIPS, custom], false).map((c) => c.id)).toEqual(["x"]);
    expect(visibleChips([...SEED_CHIPS, custom], true)).toHaveLength(4);
  });
  it("returns an empty list when nothing is visible, so the strip unmounts", () => {
    expect(visibleChips([...SEED_CHIPS], false)).toEqual([]);
  });
});

describe("mintQuickActionId", () => {
  // Review Focus #2: two pills sharing a React key make one vanish silently.
  it("never returns a reserved seed slug", () => {
    const minted = mintQuickActionId([]);
    expect(["compact", "continue", "recap"]).not.toContain(minted);
  });
  it("never collides with an existing id", () => {
    const existing = [
      { id: "chip-1", label: "A", icon: "zap", message: "m", mode: "fill" as const },
    ];
    expect(existing.map((c) => c.id)).not.toContain(mintQuickActionId(existing));
  });
  it("stays unique across repeated mints", () => {
    const seen: string[] = [];
    for (let i = 0; i < 5; i++) {
      seen.push(mintQuickActionId(seen.map((id) => ({ id, label: "A", icon: "zap", message: "m", mode: "fill" as const }))));
    }
    expect(new Set(seen).size).toBe(5);
  });
});

describe("cap", () => {
  it("is 20", () => {
    expect(QUICK_ACTIONS_MAX).toBe(20);
  });
});
```

Then append the store tests to the SAME file. These need the `api` module
mocked, so add this at the very top of the file, above the other imports:

```tsx
import { api } from "@/api/client";
import { eventBus } from "@/lib/eventBus";

vi.mock("@/api/client", () => ({
  api: { getQuickActionsConfig: vi.fn(), setQuickActionsConfig: vi.fn() },
}));
vi.mock("@/lib/eventBus", () => ({
  eventBus: { on: () => () => {}, onReconnect: () => () => {} },
}));
```

```tsx
describe("quick actions store", () => {
  beforeEach(() => {
    __resetQuickActionsForTests();
    vi.clearAllMocks();
  });

  it("publishes the server's chips", async () => {
    vi.mocked(api.getQuickActionsConfig).mockResolvedValue({
      chips: [{ id: "x", label: "X", icon: "zap", message: "m", mode: "fill" }],
    });
    const state = await refreshQuickActions();
    expect(state.chips.map((c) => c.id)).toEqual(["x"]);
  });

  // The spec's degraded path: a failed fetch must fall back to the
  // pre-feature strip, NOT blank the chips out from under the user.
  it("falls back to the starter strip when the first fetch fails", async () => {
    vi.mocked(api.getQuickActionsConfig).mockRejectedValue(new Error("offline"));
    const state = await refreshQuickActions();
    expect(state.chips.map((c) => c.id)).toEqual(["compact", "continue", "recap"]);
    expect(state.error).toBe("offline");
  });

  it("retains the cached chips when a later refresh fails", async () => {
    vi.mocked(api.getQuickActionsConfig).mockResolvedValue({
      chips: [{ id: "x", label: "X", icon: "zap", message: "m", mode: "fill" }],
    });
    await refreshQuickActions();
    __resetQuickActionsForTests();
    vi.mocked(api.getQuickActionsConfig).mockRejectedValue(new Error("offline"));
    const state = await refreshQuickActions();
    expect(state.chips.map((c) => c.id)).toEqual(["x"]);
  });

  it("deduplicates concurrent refreshes into one request", async () => {
    vi.mocked(api.getQuickActionsConfig).mockResolvedValue({ chips: [] });
    await Promise.all([refreshQuickActions(), refreshQuickActions(), refreshQuickActions()]);
    expect(api.getQuickActionsConfig).toHaveBeenCalledTimes(1);
  });

  it("saveQuickActions publishes the server's response, not the local draft", async () => {
    vi.mocked(api.setQuickActionsConfig).mockResolvedValue({ chips: [] });
    const state = await saveQuickActions([
      { id: "x", label: "X", icon: "zap", message: "m", mode: "fill" },
    ]);
    // The server is authoritative: if it normalises or rejects, the client
    // must adopt its answer rather than keeping the local draft.
    expect(state.chips).toEqual([]);
  });
});
```

Update the import at the top of `quickActions.test.ts` to pull in the store
symbols too:

```tsx
import {
  QUICK_ACTION_ICONS,
  QUICK_ACTIONS_MAX,
  SEED_CHIPS,
  __resetQuickActionsForTests,
  chipDispatchKind,
  chipDispatchesCompact,
  chipRequiresHistory,
  mintQuickActionId,
  normalizeQuickActionChip,
  normalizeQuickActions,
  quickActionIconComponent,
  refreshQuickActions,
  saveQuickActions,
  visibleChips,
} from "./quickActions";
import { beforeEach, describe, expect, it, vi } from "vitest";
```

- [ ] **Step 2: Run to verify failure**

Run: `cd /Users/james/www/ocode/web && npx vitest run src/lib/quickActions.test.ts`
Expected: FAIL — module `./quickActions` not found.

- [ ] **Step 3: Write the implementation**

Create `web/src/lib/quickActions.ts`. The icon map must name real lucide exports; if any import fails to typecheck, fix the key name against `node_modules/lucide-react/dist/esm/icons/` rather than casting.

```ts
import { useEffect, useState } from "react";
import type { LucideIcon } from "lucide-react";
import {
  Archive,
  BookOpen,
  Bug,
  ChartNoAxesColumn,
  Eye,
  FileCode,
  FileText,
  FlaskConical,
  Gauge,
  GitBranch,
  Hammer,
  ListChecks,
  MessagesSquare,
  Package,
  Play,
  Rocket,
  RefreshCw,
  Scissors,
  Search,
  ShieldCheck,
  Terminal,
  WandSparkles,
  Wrench,
  Zap,
} from "lucide-react";
import { api } from "@/api/client";
import { eventBus } from "@/lib/eventBus";
import type { QuickActionChip, QuickActionMode, QuickActionSeed } from "@/api/types";

/** Validation cap. The server rejects more than this; the form stops at it. */
export const QUICK_ACTIONS_MAX = 20;

/**
 * Icon keys, mirroring `quickActionIconAllowlist` in
 * `internal/config/quick_actions.go`. Go is the validation authority; if one
 * list gains a key the other lacks, the allowlist-size test and the typecheck
 * are what catch it.
 */
export const QUICK_ACTION_ICONS = [
  "zap", "archive", "play", "file-text", "search", "refresh-cw", "terminal",
  "git-branch", "hammer", "bug", "flask-conical", "shield-check", "list-checks",
  "wand-sparkles", "package", "book-open", "file-code", "messages-square",
  "rocket", "scissors", "wrench", "eye", "gauge", "chart-no-axes-column",
] as const;

const ICON_COMPONENTS: Record<string, LucideIcon> = {
  zap: Zap, archive: Archive, play: Play, "file-text": FileText, search: Search,
  "refresh-cw": RefreshCw, terminal: Terminal, "git-branch": GitBranch,
  hammer: Hammer, bug: Bug, "flask-conical": FlaskConical, "shield-check": ShieldCheck,
  "list-checks": ListChecks, "wand-sparkles": WandSparkles, package: Package,
  "book-open": BookOpen, "file-code": FileCode, "messages-square": MessagesSquare,
  rocket: Rocket, scissors: Scissors, wrench: Wrench, eye: Eye, gauge: Gauge,
  "chart-no-axes-column": ChartNoAxesColumn,
};

/** Never throws: an unknown key renders the default icon rather than a blank pill. */
export function quickActionIconComponent(key: string): LucideIcon {
  return ICON_COMPONENTS[key] ?? Zap;
}

const SEEDS: readonly QuickActionSeed[] = ["compact", "continue", "recap"];
/** Ids the seeded starters occupy. A minted id must never collide with one. */
export const QUICK_ACTION_RESERVED_IDS: readonly string[] = ["compact", "continue", "recap"];

export function isQuickActionSeed(value: unknown): value is QuickActionSeed {
  return typeof value === "string" && (SEEDS as readonly string[]).includes(value);
}

export function isQuickActionMode(value: unknown): value is QuickActionMode {
  return value === "fill" || value === "send";
}

/**
 * DEGRADED FALLBACK ONLY — not a second authority. Go seeds the starters and
 * the server returns them; this exists so a failed fetch degrades to the
 * pre-feature behaviour instead of blanking the strip. If you change the
 * starters in Go, change them here too, and say why in the commit.
 */
export const SEED_CHIPS: readonly QuickActionChip[] = [
  { id: "compact", label: "Compact", icon: "archive", message: "/compact", mode: "send", seed: "compact" },
  { id: "continue", label: "Continue", icon: "play", message: "continue", mode: "send", seed: "continue" },
  { id: "recap", label: "Recap", icon: "file-text", message: "/recap", mode: "send", seed: "recap" },
];

const text = (value: unknown): string => (typeof value === "string" ? value : "");

/** null means "this chip is not renderable" — blank id, label, or message. */
export function normalizeQuickActionChip(input: unknown): QuickActionChip | null {
  if (!input || typeof input !== "object" || Array.isArray(input)) return null;
  const raw = input as Record<string, unknown>;
  const id = text(raw.id).trim();
  const label = text(raw.label).trim();
  const message = text(raw.message);
  if (!id || !label || !message.trim()) return null;
  const mode = raw.mode;
  const icon = text(raw.icon);
  const seed = raw.seed;
  return {
    id,
    label,
    icon: icon || "zap",
    message,
    // An explicitly invalid mode is preserved so the server can reject it
    // rather than this layer quietly repairing the user's mistake.
    mode: isQuickActionMode(mode) ? mode : (mode === undefined || mode === null || mode === "" ? "send" : (mode as QuickActionMode)),
    ...(isQuickActionSeed(seed) ? { seed } : {}),
  };
}

export function normalizeQuickActions(input: unknown): QuickActionChip[] {
  const chips = (input as { chips?: unknown } | null)?.chips;
  if (!Array.isArray(chips)) return [];
  return chips
    .map(normalizeQuickActionChip)
    .filter((chip): chip is QuickActionChip => chip !== null);
}

/** True when clicking this chip runs a compaction, so the pill can dim. */
export function chipDispatchesCompact(chip: QuickActionChip): boolean {
  const trimmed = chip.message.trim();
  return trimmed === "/compact" || trimmed.startsWith("/compact ");
}

export function chipRequiresHistory(chip: QuickActionChip): boolean {
  return chip.seed != null;
}

export function chipDispatchKind(chip: QuickActionChip): "command" | "message" {
  return chip.message.trim().startsWith("/") ? "command" : "message";
}

export function visibleChips(chips: readonly QuickActionChip[], hasConversation: boolean): QuickActionChip[] {
  return chips.filter((chip) => hasConversation || !chipRequiresHistory(chip));
}

export function mintQuickActionId(existing: readonly QuickActionChip[]): string {
  const taken = new Set<string>([...QUICK_ACTION_RESERVED_IDS, ...existing.map((c) => c.id)]);
  let n = 1;
  let candidate = `chip-${n}`;
  while (taken.has(candidate)) {
    n += 1;
    candidate = `chip-${n}`;
  }
  return candidate;
}

export interface QuickActionsState {
  chips: QuickActionChip[];
  loading: boolean;
  error: string | null;
  revision: string;
}

type Listener = (state: QuickActionsState) => void;

let cached: QuickActionsState | null = null;
let inFlight: Promise<QuickActionsState> | null = null;
const listeners = new Set<Listener>();

export function quickActionsRevision(chips: readonly QuickActionChip[]): string {
  return chips.map((c) => `${c.id}:${c.label}:${c.icon}:${c.message}:${c.mode}:${c.seed ?? ""}`).join("|");
}

function stateFromChips(chips: QuickActionChip[], error: string | null = null, loading = false): QuickActionsState {
  return { chips, error, loading, revision: quickActionsRevision(chips) };
}

function publish(next: QuickActionsState): QuickActionsState {
  cached = next;
  for (const listener of listeners) listener(next);
  return next;
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

export async function refreshQuickActions(): Promise<QuickActionsState> {
  if (inFlight) return inFlight;
  inFlight = api
    .getQuickActionsConfig()
    .then((response) => publish(stateFromChips(normalizeQuickActions(response))))
    .catch((error: unknown) => {
      const message = errorMessage(error);
      if (cached) {
        console.warn("[quick-actions] config request failed; retaining cached chips", { error });
        return publish({ ...cached, loading: false, error: message });
      }
      console.warn("[quick-actions] config request failed; using the pre-feature default strip", {
        error,
        reason: "no-cached-chips",
      });
      return publish(stateFromChips([...SEED_CHIPS], message));
    })
    .finally(() => {
      inFlight = null;
    });
  return inFlight;
}

export async function saveQuickActions(chips: QuickActionChip[]): Promise<QuickActionsState> {
  const response = await api.setQuickActionsConfig({ chips });
  return publish(stateFromChips(normalizeQuickActions(response)));
}

export function useQuickActions(): QuickActionsState {
  const [state, setState] = useState<QuickActionsState>(
    () => cached ?? stateFromChips([], null, true),
  );

  useEffect(() => {
    const listener: Listener = (next) => setState(next);
    listeners.add(listener);

    const offChanged = eventBus.on("quick_actions_changed", () => {
      void refreshQuickActions();
    });
    const offReconnect = eventBus.onReconnect(() => {
      void refreshQuickActions();
    });

    if (cached === null) void refreshQuickActions();

    return () => {
      listeners.delete(listener);
      offChanged();
      offReconnect();
    };
  }, []);

  return state;
}

export function __resetQuickActionsForTests(): void {
  cached = null;
  inFlight = null;
  listeners.clear();
}
```

**Add the two event names to the shared bus allowlists** or the subscription above is a silent no-op:
- `internal/server/event_bus.go` → `sessionScopedEvents`-style list, if the event needs to be session-scoped (it does not — it is global, so verify the correct list for global config events; `chat_verbosity_changed` is the model).
- `web/src/lib/sessionEvents.ts` → `SESSION_SCOPED_EVENTS` **only if** `chat_verbosity_changed` appears there. If it does not, `quick_actions_changed` must not either. Mirror `chat_verbosity_changed` exactly.

- [ ] **Step 4: Run to verify pass**

Run: `cd /Users/james/www/ocode/web && npx vitest run src/lib/quickActions.test.ts`
Expected: PASS.

- [ ] **Step 5: Typecheck**

Run: `cd /Users/james/www/ocode/web && npm run typecheck 2>&1 | tail -20`
Expected: no errors **in the files you touched**. This repo carries concurrent WIP that can produce unrelated errors — record them, do not fix them, and confirm none name `quickActions.ts`.

- [ ] **Step 6: Commit**

```bash
git add web/src/lib/quickActions.ts web/src/lib/quickActions.test.ts
git commit -m "feat(web): quick-actions store, helpers, and icon map"
```

---

### Task 6: Settings form (TypeScript)

**Files:**
- Create: `web/src/components/Settings/QuickActionsForm.tsx`
- Create: `web/src/components/Settings/QuickActionsForm.test.tsx`
- Modify: `web/src/components/Settings/SettingsPanel.tsx` (lines 46, 85, 142)

**Interfaces:**
- Consumes: everything from Task 5, plus `QuickActionChip` from Task 4.
- Produces: `default export function QuickActionsForm()`, and the new section id `"quick-actions"` labelled `"Quick actions"`.

- [ ] **Step 1: Write the failing test**

Create `web/src/components/Settings/QuickActionsForm.test.tsx`. Read `ChatDisplayForm.test.tsx` first and mirror its mocking style — it must mock `@/lib/chatVerbosity`; you mock `@/lib/quickActions`.

```tsx
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import QuickActionsForm from "./QuickActionsForm";
import { QUICK_ACTIONS_MAX, SEED_CHIPS, type QuickActionsState } from "@/lib/quickActions";

const saveQuickActions = vi.fn();
const state = vi.hoisted(() => ({ current: null as QuickActionsState | null }));

vi.mock("@/lib/quickActions", async () => {
  const actual = await vi.importActual<typeof import("@/lib/quickActions")>("@/lib/quickActions");
  return {
    ...actual,
    useQuickActions: () => state.current ?? actual.__stateForTest([], null, false),
    saveQuickActions: (...args: unknown[]) => saveQuickActions(...args),
  };
});

const chip = (id: string, label: string) => ({ id, label, icon: "zap", message: `run ${id}`, mode: "fill" as const });

beforeEach(() => {
  saveQuickActions.mockReset().mockResolvedValue(undefined);
  state.current = { chips: [...SEED_CHIPS], loading: false, error: null, revision: "r1" };
});

describe("QuickActionsForm", () => {
  it("lists the configured chips in order", () => {
    render(<QuickActionsForm />);
    const labels = screen.getAllByLabelText(/chip label/i).map((el) => (el as HTMLInputElement).value);
    expect(labels).toEqual(["Compact", "Continue", "Recap"]);
  });

  it("saves an edited label", async () => {
    render(<QuickActionsForm />);
    fireEvent.change(screen.getAllByLabelText(/chip label/i)[0], { target: { value: "Shrink" } });
    fireEvent.click(screen.getByRole("button", { name: /save/i }));
    await waitFor(() => expect(saveQuickActions).toHaveBeenCalled());
    expect(saveQuickActions.mock.calls[0][0][0].label).toBe("Shrink");
  });

  it("adds a chip with a collision-proof id", async () => {
    render(<QuickActionsForm />);
    fireEvent.click(screen.getByRole("button", { name: /add chip/i }));
    fireEvent.click(screen.getByRole("button", { name: /save/i }));
    await waitFor(() => expect(saveQuickActions).toHaveBeenCalled());
    const sent = saveQuickActions.mock.calls[0][0];
    expect(sent).toHaveLength(4);
    expect(["compact", "continue", "recap"]).not.toContain(sent[3].id);
  });

  it("deletes a chip", async () => {
    render(<QuickActionsForm />);
    fireEvent.click(screen.getAllByRole("button", { name: /delete chip/i })[0]);
    fireEvent.click(screen.getByRole("button", { name: /save/i }));
    await waitFor(() => expect(saveQuickActions).toHaveBeenCalled());
    expect(saveQuickActions.mock.calls[0][0]).toHaveLength(2);
  });

  it("disables Add at the cap and states the limit", () => {
    state.current = {
      chips: Array.from({ length: QUICK_ACTIONS_MAX }, (_, i) => chip(`c${i}`, `C${i}`)),
      loading: false, error: null, revision: "r",
    };
    render(<QuickActionsForm />);
    expect(screen.getByRole("button", { name: /add chip/i })).toBeDisabled();
    expect(screen.getByText(new RegExp(`${QUICK_ACTIONS_MAX}`))).toBeInTheDocument();
  });

  // Review Focus #4: the 21st chip must be EXPLAINED and the draft preserved.
  it("surfaces the server's cap message and keeps the draft intact", async () => {
    state.current = {
      chips: Array.from({ length: QUICK_ACTIONS_MAX }, (_, i) => chip(`c${i}`, `C${i}`)),
      loading: false, error: null, revision: "r",
    };
    saveQuickActions.mockRejectedValue(new Error("quick_actions: at most 20 chips are allowed, got 21"));
    render(<QuickActionsForm />);
    fireEvent.click(screen.getByRole("button", { name: /save/i }));
    await waitFor(() => expect(screen.getByRole("alert")).toHaveTextContent(/at most 20/));
    expect(screen.getAllByLabelText(/chip label/i)).toHaveLength(QUICK_ACTIONS_MAX);
  });

  it("surfaces a whitespace-only message error from the server", async () => {
    saveQuickActions.mockRejectedValue(new Error('quick_actions: chip "compact" must have a message'));
    render(<QuickActionsForm />);
    fireEvent.click(screen.getByRole("button", { name: /save/i }));
    await waitFor(() => expect(screen.getByRole("alert")).toHaveTextContent(/must have a message/));
  });

  it("offers an icon picker listing the allowlist", () => {
    render(<QuickActionsForm />);
    const first = screen.getAllByLabelText(/chip icon/i)[0] as HTMLSelectElement;
    expect(first.tagName).toBe("SELECT");
    expect(first.options.length).toBe(24);
  });

  it("offers fill and send as the only modes", () => {
    render(<QuickActionsForm />);
    const first = screen.getAllByLabelText(/chip mode/i)[0] as HTMLSelectElement;
    expect([...first.options].map((o) => o.value)).toEqual(["fill", "send"]);
  });
});
```

The mock above references an `__stateForTest` helper that does not exist yet. **Add it to `web/src/lib/quickActions.ts`** as an exported test seam:

```ts
/** Test seam: build a state object without going through the network. */
export function __stateForTest(chips: QuickActionChip[], error: string | null, loading: boolean): QuickActionsState {
  return stateFromChips(chips, error, loading);
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd /Users/james/www/ocode/web && npx vitest run src/components/Settings/QuickActionsForm.test.tsx`
Expected: FAIL — module not found.

- [ ] **Step 3: Write the form**

Create `web/src/components/Settings/QuickActionsForm.tsx`. Reordering uses `@dnd-kit` exactly as `UnifiedTabBar.tsx` does (`DndContext` + `SortableContext` + `arrayMove` + a `useSortable` row with `CSS.Transform.toString`), with `useSensor(PointerSensor, { activationConstraint: { distance: 5 } })` and `useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates })`.

Required accessible names, which the tests pin:
- every label input: `aria-label` containing `Chip label`
- every icon select: `aria-label` containing `Chip icon`, a `<select>` with exactly 24 options
- every mode select: `aria-label` containing `Chip mode`, a `<select>` with exactly `fill` and `send`
- one `Add chip` button, one `Save` button, and a delete button per row named `Delete chip`
- a `role="alert"` region for the save error
- the current count rendered as text containing the cap number

Component shape:

```tsx
import { useEffect, useState } from "react";
import { DndContext, closestCenter, KeyboardSensor, PointerSensor, useSensor, useSensors, type DragEndEvent } from "@dnd-kit/core";
import { arrayMove, rectSortingStrategy, SortableContext, sortableKeyboardCoordinates, useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useQuickActions, saveQuickActions, mintQuickActionId, QUICK_ACTION_ICONS, QUICK_ACTIONS_MAX, quickActionIconComponent, type QuickActionsState } from "@/lib/quickActions";
import type { QuickActionChip } from "@/api/types";
```

State: a local `draft: QuickActionChip[]` re-synced from the store on every store change (the `ChatDisplayForm` pattern), plus `saving` and `saveError`.

Reorder handler:

```tsx
  const handleDragEnd = useCallback((event: DragEndEvent) => {
    const { active, over } = event;
    if (!over || active.id === over.id) return;
    setDraft((prev) => {
      const oldIndex = prev.findIndex((c) => c.id === active.id);
      const newIndex = prev.findIndex((c) => c.id === over.id);
      if (oldIndex === -1 || newIndex === -1) return prev;
      return arrayMove(prev, oldIndex, newIndex);
    });
  }, []);
```

Add handler (collision-proof per Review Focus #2):

```tsx
  const addChip = useCallback(() => {
    setDraft((prev) => {
      if (prev.length >= QUICK_ACTIONS_MAX) return prev;
      const id = mintQuickActionId(prev);
      return [...prev, { id, label: "New chip", icon: "zap", message: "", mode: "fill" }];
    });
  }, []);
```

Save handler must surface the server's message verbatim and **not clear the draft on failure**:

```tsx
  const onSave = useCallback(async () => {
    setSaving(true);
    setSaveError(null);
    try {
      await saveQuickActions(draft);
    } catch (error) {
      setSaveError(error instanceof Error ? error.message : String(error));
    } finally {
      setSaving(false);
    }
  }, [draft]);
```

**Do not** pre-validate in the form to "helpfully" swallow the cap error. The server is the authority; a local copy of the rules is the duplication class this spec explicitly avoids.

- [ ] **Step 4: Register the section**

In `web/src/components/Settings/SettingsPanel.tsx`, three edits:
- union, beside `| "chat-display"` (~line 46): `  | "quick-actions"`
- sections list, beside `{ id: "chat-display", label: "Chat display" }` (~line 85): `  { id: "quick-actions", label: "Quick actions" },`
- switch, beside `case "chat-display":` (~line 142):

```tsx
    case "quick-actions":
      return <QuickActionsForm />;
```

- [ ] **Step 5: Run the form tests**

Run: `cd /Users/james/www/ocode/web && npx vitest run src/components/Settings/QuickActionsForm.test.tsx 2>&1 | tail -30`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add web/src/components/Settings/QuickActionsForm.tsx web/src/components/Settings/QuickActionsForm.test.tsx web/src/lib/quickActions.ts web/src/components/Settings/SettingsPanel.tsx
git commit -m "feat(web): quick-actions settings form"
```

---

### Task 7: Wire the composer strip (TypeScript)

**Files:**
- Modify: `web/src/components/Chat/ChatInput.tsx` (lines 669, 691, 1358)
- Rewrite: `web/src/components/Chat/ChatInput.quickActions.test.tsx`

**Interfaces:**
- Consumes: `useQuickActions`, `visibleChips`, `chipDispatchKind`, `chipDispatchesCompact`, `quickActionIconComponent` from Task 5.
- Produces: no new exports. The composer's strip becomes config-driven.

- [ ] **Step 1: Rewrite the test to target configured chips**

`web/src/components/Chat/ChatInput.quickActions.test.tsx` currently asserts the three hardcoded pills and **will fail** the moment the array stops being a literal. Rewrite it. Keep the existing harness (the `vi.hoisted` `chat` object, the `useChat` mock, the `composer()` helper, the `clearQueue`/`clearDraft`/`clearCompaction` setup) and change only what the strip needs.

Add a mock for the store:

```tsx
const quickActions = vi.hoisted(() => ({ chips: [] as unknown[] }));
vi.mock("../../lib/quickActions", async () => {
  const actual = await vi.importActual<typeof import("../../lib/quickActions")>("../../lib/quickActions");
  return {
    ...actual,
    useQuickActions: () => actual.__stateForTest(quickActions.chips as never, null, false),
  };
});
```

Then set `quickActions.chips` in `beforeEach` to the three starters so the existing assertions keep working, and add these cases:

```tsx
  it("renders the configured chips in configured order", () => {
    quickActions.chips = [
      { id: "z", label: "Zed", icon: "bug", message: "z", mode: "send" },
      { id: "a", label: "Ay", icon: "zap", message: "a", mode: "send" },
    ];
    render(composer());
    const names = screen.getAllByRole("button").map((b) => b.textContent ?? "");
    expect(names.indexOf("Zed")).toBeLessThan(names.indexOf("Ay"));
  });

  it("hides seeded chips on an empty session but shows a custom chip", () => {
    // Review Focus: the wrapper gate must not suppress an always-useful chip.
    quickActions.chips = [
      ...actual_SEEDS(),
      { id: "x", label: "Run tests", icon: "flask-conical", message: "run tests", mode: "fill" },
    ];
    chat.hasConversation = false;
    render(composer());
    expect(screen.queryByRole("button", { name: /compact/i })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: /run tests/i })).toBeInTheDocument();
  });

  it("unmounts the strip when nothing is visible", () => {
    quickActions.chips = [...actual_SEEDS()];
    chat.hasConversation = false;
    render(composer());
    expect(screen.queryByRole("toolbar", { name: "Quick actions" })).not.toBeInTheDocument();
  });

  // Review Focus #3: the queue holds resolved TEXT, so editing a chip while a
  // click is queued cannot corrupt the pending dispatch.
  it("queues the resolved text, so deleting a chip mid-queue does not cancel it", async () => {
    chat.streaming = true;
    quickActions.chips = [{ id: "x", label: "Run tests", icon: "zap", message: "go test ./...", mode: "send" }];
    render(composer());
    fireEvent.click(screen.getByRole("button", { name: /run tests/i }));
    expect(getQueue(A)).toEqual([{ kind: "message", text: "go test ./..." }]);
    quickActions.chips = [];
    chat.streaming = false;
    await act(async () => {});
  });

  it("a fill chip puts the text in the composer without sending", () => {
    quickActions.chips = [{ id: "x", label: "Draft it", icon: "zap", message: "write a test for quick_actions", mode: "fill" }];
    render(composer());
    fireEvent.click(screen.getByRole("button", { name: /draft it/i }));
    expect(screen.getByRole("textbox")).toHaveValue("write a test for quick_actions");
    expect(sendMessage).not.toHaveBeenCalled();
  });

  it("a send chip dispatches immediately", async () => {
    quickActions.chips = [{ id: "x", label: "Go", icon: "zap", message: "go test ./...", mode: "send" }];
    render(composer());
    fireEvent.click(screen.getByRole("button", { name: /^go/i }));
    await waitFor(() => expect(sendMessage).toHaveBeenCalledWith("go test ./..."));
  });

  it("routes a slash-command chip through the command pipeline, not as a message", async () => {
    quickActions.chips = [{ id: "x", label: "Review", icon: "zap", message: "/review", mode: "send" }];
    render(composer());
    fireEvent.click(screen.getByRole("button", { name: /review/i }));
    await waitFor(() => expect(onSlashCommand).toHaveBeenCalledWith("/review", A));
    expect(sendMessage).not.toHaveBeenCalled();
  });

  it("resumes an interrupted turn for the continue seed even in fill mode", () => {
    chat.interrupted = true;
    quickActions.chips = [{ id: "continue", label: "Continue", icon: "play", message: "continue", mode: "fill", seed: "continue" }];
    render(composer());
    fireEvent.click(screen.getByRole("button", { name: /continue/i }));
    expect(resume).toHaveBeenCalled();
    expect(sendMessage).not.toHaveBeenCalled();
  });

  it("dims a compaction chip while a compaction is running", () => {
    act(() => { setCompactionState(A, { status: "active", startedAt: Date.now() }); });
    quickActions.chips = [{ id: "c", label: "Compact", icon: "archive", message: "/compact", mode: "send" }];
    render(composer());
    expect(screen.getByRole("button", { name: /compact/i })).toBeDisabled();
  });
```

Keep the existing file's four pill assertions, replacing their `getByRole` names with the exact strings from the table in Step 3(a) — `"Compact — /compact"`, `"Continue — continue"`, `"Continue — resume the interrupted turn"`, and `"Recap — /recap"`. Match them **exactly**. Do not loosen them to a loose regex to make a test pass: the file's own comment records that a `/^Resume/` style match is ambiguous with the send row's own Resume button, and a loose matcher is how a pill silently stops being clicked in production.

Define the seed accessor once, at the top of the test file:

```tsx
import { SEED_CHIPS } from "../../lib/quickActions";
const actual_SEEDS = () => SEED_CHIPS.map((c) => ({ ...c }));
```

- [ ] **Step 2: Run to verify failure**

Run: `cd /Users/james/www/ocode/web && npx vitest run src/components/Chat/ChatInput.quickActions.test.tsx 2>&1 | tail -30`
Expected: FAIL — the configured chips are not rendered.

- [ ] **Step 3: Rewrite the strip in `ChatInput.tsx`**

**(a) Replace the literal at line 691.** One memo produces the final, already-filtered item list. Do **not** build a second memo over it — two overlapping memos over the same chips is exactly the kind of thing a reviewer should reject, and it splits the ordering logic across two places.

```tsx
  const { chips: quickActionChips } = useQuickActions();

  const quickActions: QuickActionItem[] = useMemo(
    () =>
      visibleChips(quickActionChips, hasConversation).map((chip) => {
        // The Continue seed resumes an interrupted turn, so its title says so.
        // The LABEL stays as the user configured it — the spec makes the label
        // user-controlled, and only the seed's behaviour is locked. This is a
        // deliberate visible change from today, where the pill relabelled
        // itself to "Resume"; the tooltip carries the hint instead.
        const resumeVariant = chip.seed === "continue" && wasInterrupted;
        return {
          id: chip.id,
          label: chip.label,
          icon: quickActionIconComponent(chip.icon),
          title: resumeVariant
            ? `${chip.label} — resume the interrupted turn`
            : `${chip.label} — ${chip.message.trim()}`,
          disabled: compacting && chip.mode === "send" && chipDispatchesCompact(chip),
        };
      }),
    [quickActionChips, hasConversation, wasInterrupted, compacting],
  );
```

This single `quickActions` value is the only list the strip consumes, so the
title strings below are **exactly** what the rewritten tests must query:

| Chip | Condition | Rendered `title` (and the `getByRole` name) |
| --- | --- | --- |
| `compact` | idle | `Compact — /compact` |
| `continue` | idle | `Continue — continue` |
| `continue` | interrupted | `Continue — resume the interrupted turn` |
| `recap` | idle | `Recap — /recap` |

Use these strings verbatim in `ChatInput.quickActions.test.tsx`. The em dash is
U+2014 with one space either side. Replace the file's four old lookups
(`"Compact conversation context (/compact)"`, `"Send 'continue' to keep the agent
going"`, `"Resume the interrupted turn"`, `"Generate session recap (/recap)"`)
with the new ones above. Keep them as exact matches — the file's own comment
records that a loose `/^Resume/` match is ambiguous with the send row's own
Resume button.

Add the imports: `useQuickActions`, `quickActionIconComponent`, `chipDispatchesCompact`, `chipDispatchKind`, `visibleChips` from `../../lib/quickActions`, and confirm `useMemo` is imported from React (it is used elsewhere in the file).

**(b) Replace the `switch` at line 669.** Chip behaviour is resolved by seed, then mode:

```tsx
  const runQuickAction = (id: string) => {
    const chip = quickActionChips.find((c) => c.id === id);
    if (!chip) return;
    // Resume wins over both modes: it is a turn-lifecycle action, not a
    // text dispatch, so "fill the box with continue" would be wrong here.
    if (chip.seed === "continue" && wasInterrupted) {
      handleResume();
      return;
    }
    if (chip.mode === "fill") {
      updateDraft(chip.message);
      textareaRef.current?.focus();
      return;
    }
    runQuickDispatch(chip.message, chipDispatchKind(chip));
  };
```

`updateDraft` is declared at line 684 — **after** `runQuickAction`. Either move `runQuickAction` below `updateDraft` or hoist `updateDraft`; do not leave a `const` used before its declaration, which is a runtime TDZ crash on the first click.

**(c) Replace the wrapper at line 1358.** The gate moves from the wrapper to the chip — one always-visible custom chip must not be suppressed by a wrapper written for the seeded three:

```tsx
{quickActions.length > 0 && (
  <QuickActionsBar actions={quickActions} onSelect={runQuickAction} />
)}
```

`hasConversation` is no longer read at the JSX level; it is consulted only
inside the `visibleChips` call in the memo. The strip now also unmounts when the
user deletes every chip, which today's three fixed pills never had to express.

Then delete the now-unused `Archive`, `Play`, `FileText` icon imports **only if** nothing else in `ChatInput.tsx` uses them — check with a grep before removing, since several are used by the slash-command menu or the send row.

- [ ] **Step 4: Run the composer tests**

Run: `cd /Users/james/www/ocode/web && npx vitest run src/components/Chat/ 2>&1 | tail -40`
Expected: PASS, including the existing `ChatInput.compaction.test.tsx`, `ChatInput.history.test.tsx`, and `ChatInput.autoGrow.test.tsx`.

**Pay attention to the compaction suite specifically.** It exercises the composer while a compaction is active, and the new store-driven strip plus the `visibleChips` call sit on the same render path. If it fails, the cause is more likely the store mock returning a fresh array each render (causing a memo to thrash) than a logic error — check for an infinite render before changing behavior.

- [ ] **Step 5: Typecheck and build**

Run: `cd /Users/james/www/ocode/web && npm run typecheck 2>&1 | tail -20 && npx vite build 2>&1 | tail -10`
Expected: no new errors naming `ChatInput.tsx` or `quickActions`. Record unrelated concurrent-WIP errors without fixing them.

- [ ] **Step 6: Commit**

```bash
git add web/src/components/Chat/ChatInput.tsx web/src/components/Chat/ChatInput.quickActions.test.tsx
git commit -m "feat(web): drive the composer quick-action strip from config"
```

---

### Task 8: Mutation verification, docs, and full-suite gates

**Files:**
- Modify: `skills/ocode-web/SKILL.md`
- Modify: `CHANGES.md`
- Create (via the context sub-agent): a concept page under `docs/`

**Interfaces:**
- Consumes: all of Tasks 1–7.
- Produces: no code.

- [ ] **Step 1: Mutation-verify the behavioural guards**

For each of the five Review Focus items and the fill/send/resume core, break the implementation and confirm the matching test fails. **Confirm each mutant compiles first** (`npx tsc --noEmit` for TS, `go build ./...` for Go) — a build-only failure is `INVALID`, not `CAUGHT`.

Mutants to try, one at a time, restoring between each:

| # | Break | Test that must fail |
| --- | --- | --- |
| 1 | In `normalizeQuickActionChip`, drop the `!message.trim()` null-return | `normalizeQuickActionChip > returns null for a whitespace-only message` |
| 2 | In `Validate`, drop the duplicate-id check | `TestQuickActionsValidate > duplicate id` |
| 3 | In `chipDispatchKind`, always return `"message"` | `routes a leading slash to the command pipeline` |
| 4 | In `runQuickAction`, remove the `fill` branch so it always dispatches | `a fill chip puts the text in the composer without sending` |
| 5 | In `runQuickAction`, remove the resume branch | `resumes an interrupted turn for the continue seed even in fill mode` |
| 6 | In `visibleChips`, return `chips` unfiltered | `hides seeded chips on an empty session but shows a custom chip` |
| 7 | In `Validate`, change the cap to 21 | `TestQuickActionsValidate > 21 chips rejected` |
| 8 | In `quickActionIconComponent`, return `undefined` for unknown keys | `falls back instead of throwing for an unknown icon key` |
| 9 | In `mintQuickActionId`, return `"chip-1"` unconditionally | `mintQuickActionId > never collides with an existing id` |
| 10 | In the form's `onSave`, clear the draft even on failure | `surfaces the server's cap message and keeps the draft intact` |

Record which mutants were caught. If a mutant survives, that is a coverage gap — add the test, do not accept the survivor. Guard against leaving a mutation in the tree: after the run, `git diff --stat` must be empty for the files you mutated.

- [ ] **Step 2: Update the skill file**

In `skills/ocode-web/SKILL.md`, add an item for the quick-action strip: the config key, the two endpoints, `lib/quickActions.ts` as the store, the Go-seeds/TS-degrades rule, the cap of 20, and the two derived states (compaction dim, requires-history). Add the three new test files to whatever regression-test list that file keeps.

- [ ] **Step 3: Update CHANGES.md**

Add a dated entry describing the configurable strip, the cap, the `fill`/`send` modes, and the fact that a fresh install is unchanged.

- [ ] **Step 4: Write the concept page via the context sub-agent**

Dispatch `task` with `agent="context"`, instructing it to write a concept page covering: the quick-actions config surface, why Go seeds and TS only degrades, the `seed` mechanism, the two derived states, the visibility gate moving from wrapper to chip, and the 20-chip cap. Pass the path as `concepts/quick-actions-chips.md` — **`doc_write` prepends `docs/` itself**, so passing `docs/concepts/...` creates `docs/docs/concepts/...`.

Then verify yourself: confirm the file is at `docs/concepts/quick-actions-chips.md`, that `docs/index.md` links it, and that no stale `file.go:NNN` anchors were introduced. If the sub-agent wanders, cancel it and do the write through it with a tighter prompt.

- [ ] **Step 5: Run the full gates**

```bash
cd /Users/james/www/ocode
go build ./... && go vet ./internal/config/ ./internal/server/ && gofmt -l internal/config/ internal/server/
go test ./internal/config/ ./internal/server/ -count=1 2>&1 | tail -20
cd web && npm run typecheck 2>&1 | tail -10
npx vitest run src/lib/quickActions.test.ts src/components/Settings/QuickActionsForm.test.tsx \
  src/components/Chat/ src/api/client.quickActions.test.ts 2>&1 | tail -25
npx vite build 2>&1 | tail -5
```

Expected: all green. Then run the **full** web suite once:

```bash
cd /Users/james/www/ocode/web && npx vitest run 2>&1 | tail -30
```

This tree carries concurrent uncommitted WIP, so the full suite may show failures unrelated to this work. **Compare the failing set against a pristine baseline before attributing anything to this change** — the reliable way is `git worktree add .worktrees/quick-actions-baseline HEAD`, then `cp internal/agent/models-snapshot.json` and `cp internal/browse/cdp/htr-assets.zip` into it (both are gitignored `//go:embed` inputs, and a worktree will not build without them). Record the baseline's failing set and confirm this change adds none.

- [ ] **Step 6: Commit**

```bash
git add skills/ocode-web/SKILL.md CHANGES.md docs/concepts/quick-actions-chips.md docs/index.md docs/log.md
git commit -m "docs: configurable quick-action chips"
```

---

## Definition of Done

- [ ] A fresh install with no `quick_actions` key renders the three starter pills, in order, with the same labels, icons, and behaviour as before this change.
- [ ] A user can add, edit, reorder, and delete chips in Settings, up to 20, and the settings survive a restart and appear in a second browser.
- [ ] `fill` populates the composer without sending; `send` dispatches through the existing queue pipeline; a slash-command chip routes to the slash pipeline, not as a literal message.
- [ ] The Continue starter still resumes an interrupted turn.
- [ ] A chip whose message is a compaction dims while a compaction is running.
- [ ] Seeded chips hide on an empty session; custom chips do not; the strip unmounts when nothing is visible.
- [ ] Every one of the five Review Focus items has a passing test, and its mutant was confirmed to compile and then caught.
- [ ] `go build ./...`, `go vet`, `gofmt -l`, `npm run typecheck`, and `vite build` are clean for the touched files.
- [ ] The full web suite introduces no new failure against a pristine baseline.
