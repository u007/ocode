package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	// Mode and Seed are spelled as literals on purpose. Pinning them with the
	// Go constants would move expectation and implementation together, so
	// renaming QuickActionSeedCompact to any other string would leave this
	// green -- while the TypeScript store matches the literal "compact", making
	// that a silent cross-boundary break. An expectation must be an
	// independent transcription of the spec, never a restatement of the code.
	want := []QuickActionChip{
		{ID: "compact", Label: "Compact", Icon: "archive", Message: "/compact", Mode: "send", Seed: "compact"},
		{ID: "continue", Label: "Continue", Icon: "play", Message: "continue", Mode: "send", Seed: "continue"},
		{ID: "recap", Label: "Recap", Icon: "file-text", Message: "/recap", Mode: "send", Seed: "recap"},
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

func TestNormalizeQuickActionsDoesNotMutateItsArgument(t *testing.T) {
	// QuickActionsConfig copies by value but Chips is a slice header, so filling
	// through cfg.Chips[i] reaches the caller's backing array even though the
	// struct itself was copied. A handler that snapshots a cached config out
	// from under its mutex and then normalizes outside the lock would be racing
	// its own readers. The returned config must own its chips outright.
	in := []QuickActionChip{{ID: "a", Label: "A", Message: "m"}}
	cfg := QuickActionsConfig{Chips: in}
	got := NormalizeQuickActions(cfg)

	if cfg.Chips[0].Icon != "" || cfg.Chips[0].Mode != "" {
		t.Fatalf("NormalizeQuickActions mutated its argument: %+v", cfg.Chips[0])
	}
	if in[0].Icon != "" || in[0].Mode != "" {
		t.Fatalf("NormalizeQuickActions wrote through the caller's backing array: %+v", in[0])
	}
	if got.Chips[0].Icon != QuickActionDefaultIcon || got.Chips[0].Mode != QuickActionModeSend {
		t.Fatalf("normalized chip = %+v, want icon %q and mode %q",
			got.Chips[0], QuickActionDefaultIcon, QuickActionModeSend)
	}
	// And the other direction: writing to the result must not reach back in.
	got.Chips[0].Label = "mutated"
	if in[0].Label != "A" {
		t.Fatalf("the returned config still aliases the caller's array: %+v", in[0])
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
		// Without a positive row here, dropping the `QuickActionModeFill &&`
		// clause from the accept check -- which turns every fill chip into a
		// hard error and breaks half the feature -- fails no test, because
		// every other positive row is built on base, whose mode is send.
		// (Dropping the *Send* clause instead is a different mutant: it breaks
		// send chips, which the other positive rows already cover.)
		{"fill mode chip", QuickActionsConfig{Chips: []QuickActionChip{mutate(func(c *QuickActionChip) { c.Mode = QuickActionModeFill })}}, ""},
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

func TestQuickActionsConfigJSONShapeRoundTripAndSeedMarker(t *testing.T) {
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

func TestQuickActionsConfigToleratesNullChipsField(t *testing.T) {
	// `{"chips":null}` must NOT error -- an absent/null field is a legal "no
	// config yet", distinct from `{"chips":[null]}` in the sibling test, which
	// is a malformed chip and must be rejected.
	var cfg QuickActionsConfig
	if err := json.Unmarshal([]byte(`{"chips":null}`), &cfg); err != nil {
		t.Fatalf("null chips rejected: %v", err)
	}
	// Assert nil specifically, not len() == 0: nil is the load-bearing signal
	// that means "key absent -> seed the starters", so a loader that seeds on
	// a zero-length slice cannot be defended by this test. An empty-but-non-nil
	// slice means the user deleted every chip and must show no strip.
	if cfg.Chips != nil {
		t.Fatalf("null chips decoded to a non-nil slice of %d entries; nil is what preserves the seed-vs-deleted-all distinction", len(cfg.Chips))
	}

	// Normalize must not disturb that distinction: slices.Clone(nil) is nil.
	// Without the clone, or if it ever normalizes nil into an empty slice, the
	// loader contract documented on QuickActionsConfig silently breaks.
	if got := NormalizeQuickActions(cfg); got.Chips != nil {
		t.Fatalf("NormalizeQuickActions turned nil chips into a %d-entry slice; the nil marker was lost", len(got.Chips))
	}

	// The opposite state stays distinguishable: `[]` is a legal, deliberate
	// "the user removed all chips", and it must remain non-nil through normalize.
	var emptied QuickActionsConfig
	if err := json.Unmarshal([]byte(`{"chips":[]}`), &emptied); err != nil {
		t.Fatalf("empty chips rejected: %v", err)
	}
	if emptied.Chips == nil {
		t.Fatal(`"chips":[] decoded to nil; it must stay non-nil so an emptied strip is distinguishable from an absent one`)
	}
	if got := NormalizeQuickActions(emptied); got.Chips == nil || len(got.Chips) != 0 {
		t.Fatalf("NormalizeQuickActions on an emptied strip = %#v, want a non-nil empty slice", got.Chips)
	}
}

// ---------------------------------------------------------------------------
// Persist and load wiring.
//
// The house pattern for save-then-reload is speech_summary_test.go:81 —
// LoadOcodeConfig takes a *Config and returns only error, so values are read
// back through cfg.Ocode. HOME is redirected in every test below; omitting it
// writes into the developer's real ~/.config/opencode and clobbers it.
// ---------------------------------------------------------------------------

func TestSaveAndLoadOcodeQuickActionsRoundTrips(t *testing.T) {
	setHomeTree(t, t.TempDir())

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
	setHomeTree(t, t.TempDir())

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
	setHomeTree(t, t.TempDir())

	bad := QuickActionsConfig{Chips: []QuickActionChip{{ID: "a", Label: "A", Icon: "zap", Message: "m", Mode: "sideways"}}}
	if err := SaveOcodeQuickActions(bad); err == nil {
		t.Fatal("SaveOcodeQuickActions accepted an invalid mode")
	}
	// A rejected save must not leave a half-written file behind: nothing was
	// valid to persist, so a reload has to still report the untouched default.
	cfg := Config{}
	if err := LoadOcodeConfig(&cfg); err != nil {
		t.Fatalf("LoadOcodeConfig: %v", err)
	}
	if len(cfg.Ocode.QuickActions.Chips) != len(SeedQuickActions().Chips) {
		t.Errorf("rejected save changed the strip: %+v", cfg.Ocode.QuickActions.Chips)
	}
}

func TestLoadOcodeConfigSeedsQuickActionsWhenAbsent(t *testing.T) {
	// The out-of-the-box guarantee: an install that never touched settings
	// still renders the three starter pills. HOME is a fresh temp dir, so there
	// is no ocodeconfig.json at all — the no-file path, not just "file without
	// the key".
	setHomeTree(t, t.TempDir())

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

func TestSaveAndLoadOcodeQuickActionsPreservesEmptyStrip(t *testing.T) {
	// The resurrection bug. A user who deletes every chip must get no strip,
	// forever — a loader that branches on len(Chips) == 0 instead of Chips ==
	// nil silently restores all three starters on the next config load, which
	// looks like the app ignoring them. This is the one test that separates
	// "absent" from "deliberately emptied", so it must assert on the non-nil
	// marker, not just the length.
	setHomeTree(t, t.TempDir())

	emptied := QuickActionsConfig{Chips: []QuickActionChip{}}
	if err := SaveOcodeQuickActions(emptied); err != nil {
		t.Fatalf("SaveOcodeQuickActions(empty): %v", err)
	}
	cfg := Config{}
	if err := LoadOcodeConfig(&cfg); err != nil {
		t.Fatalf("LoadOcodeConfig: %v", err)
	}
	if cfg.Ocode.QuickActions.Chips == nil {
		t.Fatal("an emptied strip loaded as nil, so the loader will treat it as absent and resurrect the seeds")
	}
	if len(cfg.Ocode.QuickActions.Chips) != 0 {
		t.Fatalf("an emptied strip loaded %d chips: %+v", len(cfg.Ocode.QuickActions.Chips), cfg.Ocode.QuickActions.Chips)
	}
}

func TestLoadOcodeConfigSeedsWhenFileExistsWithoutQuickActionsKey(t *testing.T) {
	// The other absent-key shape: a real config file that predates the feature
	// and has every other key. It must seed too, and — the spec's Migration
	// guarantee — loading must not require the key to be present.
	setHomeTree(t, t.TempDir())

	if err := SaveOcodeChatVerbosity(ChatVerbosityConfig{Preset: ChatVerbosityQuiet}); err != nil {
		t.Fatalf("SaveOcodeChatVerbosity: %v", err)
	}
	cfg := Config{}
	if err := LoadOcodeConfig(&cfg); err != nil {
		t.Fatalf("LoadOcodeConfig: %v", err)
	}
	want := SeedQuickActions()
	if len(cfg.Ocode.QuickActions.Chips) != len(want.Chips) {
		t.Fatalf("file without quick_actions seeded %d chips, want %d", len(cfg.Ocode.QuickActions.Chips), len(want.Chips))
	}
	if cfg.Ocode.ChatVerbosity.Preset != ChatVerbosityQuiet {
		t.Errorf("chat_verbosity.preset = %q, want %q", cfg.Ocode.ChatVerbosity.Preset, ChatVerbosityQuiet)
	}
}

func TestLoadOcodeConfigSeedsQuickActionsWhenChipsIsJSONNull(t *testing.T) {
	// "absent chips" has two spellings on disk — the key missing, and an
	// explicit null. QuickActionsConfig's doc makes nil chips mean "absent",
	// and the spec lists it beside a missing key, so both must seed. Reading
	// null as "the user emptied the strip" would silently give a fresh-looking
	// config three pills the user never asked for.
	//
	// The fixture is written through the RESOLVED config path, never a
	// hand-built ~/.config/opencode: GlobalConfigDir returns %APPDATA%\opencode
	// on Windows and $XDG_CONFIG_HOME/opencode everywhere off darwin, so a
	// hardcoded macOS path writes a file the loader never reads — the loader
	// then takes the absent-key path, seeds three chips, and this test asserts
	// exactly what the absent path produces. It was a false green off macOS.
	// chat_verbosity in the same file is the guard: the loader read THIS file or
	// that preset is not quiet.
	home, xdg, appdata := t.TempDir(), t.TempDir(), t.TempDir()
	setHomeTree(t, home)
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("APPDATA", appdata)

	path := writeOcodeConfigFixture(t, `{"quick_actions":{"chips":null},"chat_verbosity":{"preset":"quiet"}}`)

	cfg := Config{}
	if err := LoadOcodeConfig(&cfg); err != nil {
		t.Fatalf("LoadOcodeConfig: %v", err)
	}
	if cfg.Ocode.ChatVerbosity.Preset != ChatVerbosityQuiet {
		t.Fatalf("chat_verbosity.preset = %q, want %q — the loader never read the fixture at %s, so the seed assertions below prove nothing",
			cfg.Ocode.ChatVerbosity.Preset, ChatVerbosityQuiet, path)
	}
	want := SeedQuickActions()
	if len(cfg.Ocode.QuickActions.Chips) != len(want.Chips) {
		t.Fatalf(`"chips":null loaded %d chips, want the %d seeds`, len(cfg.Ocode.QuickActions.Chips), len(want.Chips))
	}
	for i := range want.Chips {
		if cfg.Ocode.QuickActions.Chips[i] != want.Chips[i] {
			t.Errorf("seeded chip %d = %+v, want %+v", i, cfg.Ocode.QuickActions.Chips[i], want.Chips[i])
		}
	}
}

func TestLoadOcodeConfigRejectsHandEditedOverCapQuickActions(t *testing.T) {
	// The load path validates, so an over-cap strip cannot be smuggled in by
	// editing ocodeconfig.json. SaveOcodeQuickActions already refuses this shape
	// (it validates before taking the lock), so the editor is the only route —
	// and writeOcodeConfigFile would otherwise write the same invalid block
	// straight back on the next save, making it self-perpetuating. The cap is
	// the bound Task 3's PUT depends on, so it has to hold here too.
	//
	// The chip count is the only thing that changes between the two writes, and
	// the at-cap file is asserted to load cleanly first: that is what proves the
	// later failure is the CAP and not the loader choking on hand-written JSON
	// (the strict per-chip UnmarshalJSON is a different failure with a different
	// message).
	home, xdg, appdata := t.TempDir(), t.TempDir(), t.TempDir()
	setHomeTree(t, home)
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("APPDATA", appdata)

	writeOcodeConfigFixture(t, quickActionsFixture(QuickActionsMaxChips))
	atCap := Config{}
	if err := LoadOcodeConfig(&atCap); err != nil {
		t.Fatalf("LoadOcodeConfig rejected a valid %d-chip strip: %v — the fixture is not reaching the loader the way the over-cap write will", QuickActionsMaxChips, err)
	}
	if len(atCap.Ocode.QuickActions.Chips) != QuickActionsMaxChips {
		t.Fatalf("at-cap file loaded %d chips, want %d", len(atCap.Ocode.QuickActions.Chips), QuickActionsMaxChips)
	}

	writeOcodeConfigFixture(t, quickActionsFixture(QuickActionsMaxChips+1))
	cfg := Config{}
	err := LoadOcodeConfig(&cfg)
	if err == nil {
		t.Fatalf("LoadOcodeConfig accepted a %d-chip strip and loaded %d chips; the cap is bypassable by editing the file",
			QuickActionsMaxChips+1, len(cfg.Ocode.QuickActions.Chips))
	}
	if !strings.Contains(err.Error(), "quick_actions") {
		t.Errorf("error %q does not name the offending key", err)
	}
	if !strings.Contains(err.Error(), strconv.Itoa(QuickActionsMaxChips)) {
		t.Errorf("error %q does not name the cap (%d)", err, QuickActionsMaxChips)
	}
}

// quickActionsFixture renders a raw `{"quick_actions":{...}}` block holding n
// otherwise-valid chips. It is written as JSON text, never through
// SaveOcodeQuickActions, because that function is what the tests below need to
// be able to violate. Every chip is individually legal (unique id, label,
// message, default icon and mode) so the ONLY thing that can reject the file is
// the length check.
func quickActionsFixture(n int) string {
	chips := make([]string, 0, n)
	for i := 0; i < n; i++ {
		chips = append(chips, fmt.Sprintf(`{"id":"c%d","label":"Chip %d","message":"message %d"}`, i, i, i))
	}
	return fmt.Sprintf(`{"quick_actions":{"chips":[%s]}}`, strings.Join(chips, ","))
}

// writeOcodeConfigFixture writes body to the RESOLVED global ocodeconfig.json
// and returns the path, after checking that the resolved path lands inside the
// sandbox the test redirected HOME/XDG_CONFIG_HOME/APPDATA to. A hand-built
// path is the trap this exists to catch: it produces a green test that never
// touched the file the loader reads. Asserting containment here makes that
// mistake fail loudly on every platform instead of passing for the wrong reason.
func writeOcodeConfigFixture(t *testing.T, body string) string {
	t.Helper()
	path, err := ActiveOcodeConfigPath()
	if err != nil {
		t.Fatalf("ActiveOcodeConfigPath: %v", err)
	}
	for _, root := range []string{os.Getenv("HOME"), os.Getenv("XDG_CONFIG_HOME"), os.Getenv("APPDATA")} {
		if root != "" && strings.HasPrefix(path, root) {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatalf("MkdirAll(%s): %v", filepath.Dir(path), err)
			}
			if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
				t.Fatalf("WriteFile(%s): %v", path, err)
			}
			return path
		}
	}
	t.Fatalf("resolved config path %q is outside every sandbox root (HOME=%q XDG_CONFIG_HOME=%q APPDATA=%q); the fixture would be written where the loader never reads it",
		path, os.Getenv("HOME"), os.Getenv("XDG_CONFIG_HOME"), os.Getenv("APPDATA"))
	return ""
}
