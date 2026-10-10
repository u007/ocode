package server

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/config"
)

// bashRulesHandler is permModeHandler plus config isolation. The endpoint
// persists to ocodeconfig.json, so HOME and XDG_CONFIG_HOME must point at a
// throwaway dir or the test would rewrite the developer's real global rules.
func bashRulesHandler(t *testing.T, n int) (*Handler, []*agent.Agent) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))
	h, agents, _ := permModeHandler(t, n)
	h.mu.Lock()
	h.cfg = &config.Config{}
	h.mu.Unlock()
	return h, agents
}

func putBashRules(t *testing.T, h *Handler, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	rec := httptest.NewRecorder()
	h.HandleSetBashRules(rec, httptest.NewRequest("PUT", "/api/permissions/bash-rules", bytes.NewReader(raw)))
	return rec
}

func persistedPrefixes(t *testing.T) map[string]string {
	t.Helper()
	cfg, err := config.LoadOcodeConfigCopy()
	if err != nil {
		t.Fatalf("LoadOcodeConfigCopy() error = %v", err)
	}
	return cfg.Permissions.Bash.Prefixes
}

// TestSetBashRulesAppliesSetAndRemoveEverywhere is the happy path: the delta
// lands in ocodeconfig.json, in the handler's in-memory config, and on every
// live agent's permission manager.
func TestSetBashRulesAppliesSetAndRemoveEverywhere(t *testing.T) {
	h, agents := bashRulesHandler(t, 2)
	// Seed one rule the delta will remove.
	if err := config.SaveSingleBashPrefixRule("sed", "deny"); err != nil {
		t.Fatalf("seed error = %v", err)
	}
	h.mu.Lock()
	h.cfg.Ocode.Permissions.Bash.Prefixes = map[string]string{"sed": "deny"}
	h.mu.Unlock()
	for _, ag := range agents {
		ag.Permissions().SetBashPrefixRule("sed", agent.PermissionDeny)
	}

	rec := putBashRules(t, h, map[string]any{
		"set":    map[string]string{"git push": "deny", "git status": "allow"},
		"remove": []string{"sed"},
	})
	if rec.Code != 200 {
		t.Fatalf("PUT = %d: %s", rec.Code, rec.Body.String())
	}

	onDisk := persistedPrefixes(t)
	if _, still := onDisk["sed"]; still {
		t.Fatalf("sed survived the delete on disk: %#v", onDisk)
	}
	if onDisk["git push"] != "deny" || onDisk["git status"] != "allow" {
		t.Fatalf("on-disk prefixes = %#v, want git push=deny git status=allow", onDisk)
	}

	h.mu.Lock()
	inMemory := h.cfg.Ocode.Permissions.Bash.Prefixes
	h.mu.Unlock()
	if _, still := inMemory["sed"]; still {
		t.Fatalf("sed survived the delete in h.cfg: %#v", inMemory)
	}

	for i, ag := range agents {
		rules := ag.Permissions().BashPrefixRules()
		if _, still := rules["sed"]; still {
			t.Fatalf("agent %d still holds sed: %#v", i, rules)
		}
		if rules["git push"] != agent.PermissionDeny {
			t.Fatalf("agent %d git push = %q, want deny", i, rules["git push"])
		}
	}

	// The response carries server truth so the editor can reload without a
	// second round trip, sorted by prefix.
	var out struct {
		BashRules []struct {
			Tool  string `json:"tool"`
			Level string `json:"level"`
		} `json:"bash_rules"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v (%s)", err, rec.Body.String())
	}
	if len(out.BashRules) != 2 {
		t.Fatalf("bash_rules = %+v, want 2 entries", out.BashRules)
	}
	if out.BashRules[0].Tool != "git push" || out.BashRules[1].Tool != "git status" {
		t.Fatalf("bash_rules not sorted by prefix: %+v", out.BashRules)
	}
}

// TestSetBashRulesRejectsBadPayloadWithoutWriting: the whole payload is
// validated BEFORE anything is persisted, so one bad entry cannot leave a
// half-applied rule set. This also pins the silent-discard bug class — a
// blanket `git`=allow used to return 200 while SetBashPrefixRule threw the rule
// away.
func TestSetBashRulesRejectsBadPayloadWithoutWriting(t *testing.T) {
	cases := []struct {
		name string
		body map[string]any
	}{
		{"invalid JSON", map[string]any{}},
		{"no changes", map[string]any{"set": map[string]string{}}},
		{"blank prefix", map[string]any{"set": map[string]string{"  ": "deny"}}},
		{"unknown level", map[string]any{"set": map[string]string{"sed": "maybe"}}},
		{"blanket git allow", map[string]any{"set": map[string]string{"git": "allow"}}},
		{"reserved in-root key", map[string]any{"set": map[string]string{"__inroot__:cat:/tmp": "allow"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, agents := bashRulesHandler(t, 1)
			body := tc.body
			if tc.name == "invalid JSON" {
				// A body the decoder rejects (set with a non-string value).
				body = map[string]any{"set": 42}
			}
			rec := putBashRules(t, h, body)
			if rec.Code != 400 {
				t.Fatalf("PUT = %d, want 400 (%s)", rec.Code, rec.Body.String())
			}
			if got := persistedPrefixes(t); len(got) != 0 {
				t.Fatalf("a rejected payload wrote rules: %#v", got)
			}
			if got := agents[0].Permissions().BashPrefixRules(); len(got) != 0 {
				t.Fatalf("a rejected payload reached the live agent: %#v", got)
			}
		})
	}
}

// TestSetBashRulesDeltaPreservesConcurrentExternalWrite is the reason the body
// is a delta and not a replacement map: a rule added by another surface (the
// TUI /ban handler or POST /api/permissions/bash-rule) after the editor loaded
// must survive the editor's save.
func TestSetBashRulesDeltaPreservesConcurrentExternalWrite(t *testing.T) {
	h, agents := bashRulesHandler(t, 1)
	if err := config.SaveSingleBashPrefixRule("sed", "deny"); err != nil {
		t.Fatalf("seed error = %v", err)
	}
	h.mu.Lock()
	h.cfg.Ocode.Permissions.Bash.Prefixes = map[string]string{"sed": "deny"}
	h.mu.Unlock()

	// Another surface bans something while the editor holds a stale list.
	if err := config.SaveSingleBashPrefixRule("curl", "deny"); err != nil {
		t.Fatalf("external ban error = %v", err)
	}
	h.mu.Lock()
	h.cfg.Ocode.Permissions.Bash.Prefixes["curl"] = "deny"
	h.mu.Unlock()
	// The other surface's own write path also pushes the rule to every live
	// agent (see HandleSetBashRule) — reproduce that, or the final assertion
	// would only prove the test never told the agent about curl.
	for _, ag := range agents {
		ag.Permissions().SetBashPrefixRule("curl", agent.PermissionDeny)
	}

	rec := putBashRules(t, h, map[string]any{
		"set":    map[string]string{"git push": "deny"},
		"remove": []string{"sed"},
	})
	if rec.Code != 200 {
		t.Fatalf("PUT = %d: %s", rec.Code, rec.Body.String())
	}

	onDisk := persistedPrefixes(t)
	if onDisk["curl"] != "deny" {
		t.Fatalf("the external ban was clobbered: %#v", onDisk)
	}
	if _, still := onDisk["sed"]; still {
		t.Fatalf("sed survived: %#v", onDisk)
	}
	if onDisk["git push"] != "deny" {
		t.Fatalf("editor's own rule missing: %#v", onDisk)
	}
	if agents[0].Permissions().BashPrefixRules()["curl"] != agent.PermissionDeny {
		t.Fatalf("live agent lost the external ban: %#v", agents[0].Permissions().BashPrefixRules())
	}
}

// TestSetBashRulesRemovingAbsentPrefixSucceeds: the editor's Remove is
// idempotent, so deleting a rule another surface already deleted is a no-op,
// not a 400.
func TestSetBashRulesRemovingAbsentPrefixSucceeds(t *testing.T) {
	h, _ := bashRulesHandler(t, 1)
	rec := putBashRules(t, h, map[string]any{"remove": []string{"never-existed", "never-existed"}})
	if rec.Code != 200 {
		t.Fatalf("PUT = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if got := persistedPrefixes(t); len(got) != 0 {
		t.Fatalf("unexpected writes: %#v", got)
	}
}

// TestSetBashRulesRenameIsASetNotADelete: re-adding a removed prefix at a new
// level must land as a set, even when the client sends both keys.
func TestSetBashRulesRenameIsASetNotADelete(t *testing.T) {
	h, agents := bashRulesHandler(t, 1)
	if err := config.SaveSingleBashPrefixRule("sed", "deny"); err != nil {
		t.Fatalf("seed error = %v", err)
	}
	h.mu.Lock()
	h.cfg.Ocode.Permissions.Bash.Prefixes = map[string]string{"sed": "deny"}
	h.mu.Unlock()

	rec := putBashRules(t, h, map[string]any{
		"set":    map[string]string{"sed": "allow"},
		"remove": []string{"sed"},
	})
	if rec.Code != 200 {
		t.Fatalf("PUT = %d: %s", rec.Code, rec.Body.String())
	}
	if got := persistedPrefixes(t)["sed"]; got != "allow" {
		t.Fatalf("sed = %q, want allow (the set must win over the remove)", got)
	}
	if got := agents[0].Permissions().BashPrefixRules()["sed"]; got != agent.PermissionAllow {
		t.Fatalf("live agent sed = %q, want allow", got)
	}
}

// TestSetBashRuleRejectsBlanketGitAllowWithAReason covers the single-rule write
// path (`/ban add` on web, and the settings form's per-row equivalent). It used
// to answer 200 while storing nothing, because rejection relied on
// SetBashPrefixRule silently dropping the rule.
func TestSetBashRuleRejectsBlanketGitAllowWithAReason(t *testing.T) {
	h, agents := bashRulesHandler(t, 1)
	raw, err := json.Marshal(map[string]string{"prefix": "git", "level": "allow"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	rec := httptest.NewRecorder()
	h.HandleSetBashRule(rec, httptest.NewRequest("POST", "/api/permissions/bash-rule", bytes.NewReader(raw)))

	if rec.Code != 400 {
		t.Fatalf("POST = %d, want 400 (%s)", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("always-allowed")) {
		t.Fatalf("400 body does not explain the rejection: %s", rec.Body.String())
	}
	if got := persistedPrefixes(t); len(got) != 0 {
		t.Fatalf("a rejected rule was written: %#v", got)
	}
	if got := agents[0].Permissions().BashPrefixRules(); len(got) != 0 {
		t.Fatalf("a rejected rule reached the live agent: %#v", got)
	}
}

// TestSetBashRuleStillAcceptsGranularGitRules: the fix must not narrow what a
// user may ban — a granular `git push` deny is the normal case.
func TestSetBashRuleStillAcceptsGranularGitRules(t *testing.T) {
	h, agents := bashRulesHandler(t, 1)
	raw, _ := json.Marshal(map[string]string{"prefix": "git push", "level": "deny"})
	rec := httptest.NewRecorder()
	h.HandleSetBashRule(rec, httptest.NewRequest("POST", "/api/permissions/bash-rule", bytes.NewReader(raw)))

	if rec.Code != 200 {
		t.Fatalf("POST = %d: %s", rec.Code, rec.Body.String())
	}
	if got := persistedPrefixes(t)["git push"]; got != "deny" {
		t.Fatalf("on-disk = %#v, want git push=deny", persistedPrefixes(t))
	}
	if got := agents[0].Permissions().BashPrefixRules()["git push"]; got != agent.PermissionDeny {
		t.Fatalf("live agent = %q, want deny", got)
	}
}
