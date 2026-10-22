package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/session"
	"github.com/u007/ocode/internal/skill"
	"github.com/u007/ocode/internal/tool"
)

// writeServerSkill creates a discoverable project-local skill under root.
func writeServerSkill(t *testing.T, root, name string) {
	t.Helper()
	dir := filepath.Join(root, "skills", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: " + name + "\ndescription: " + name + " helper.\n---\n\n# " + name + "\nbody\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func skillTestHandler(t *testing.T) (*Handler, string) {
	t.Helper()
	setHomeTree(t, t.TempDir())
	skill.InvalidateSkillCache()
	t.Cleanup(skill.InvalidateSkillCache)
	h := NewHandler()
	h.workDir = t.TempDir()
	return h, h.workDir
}

func decodeManageResponse(t *testing.T, rec *httptest.ResponseRecorder) (string, []skillManageEntry) {
	t.Helper()
	var resp struct {
		ProjectRoot string             `json:"project_root"`
		Skills      []skillManageEntry `json:"skills"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal %s: %v", rec.Body.String(), err)
	}
	return resp.ProjectRoot, resp.Skills
}

// TestHandleListManageableSkillsOrdersEnabledFirst pins the sidebar contract:
// every discoverable non-Kaizen skill is listed (including disabled ones, so they
// can be switched back on), enabled skills come first, and names sort within each
// group.
func TestHandleListManageableSkillsOrdersEnabledFirst(t *testing.T) {
	h, root := skillTestHandler(t)
	for _, name := range []string{"zeta", "alpha", "mid"} {
		writeServerSkill(t, root, name)
	}
	if err := config.SaveSkillEnabled(root, "alpha", false); err != nil {
		t.Fatalf("disable alpha: %v", err)
	}
	skill.InvalidateSkillCache()

	req := httptest.NewRequest(http.MethodGet, "/api/skills/manage?path="+root, nil)
	rec := httptest.NewRecorder()
	h.HandleListManageableSkills(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	gotRoot, entries := decodeManageResponse(t, rec)
	if gotRoot != root {
		t.Errorf("project_root = %q, want %q", gotRoot, root)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 skills, got %d: %+v", len(entries), entries)
	}
	// Enabled first (mid, zeta), then the disabled one (alpha).
	wantOrder := []string{"mid", "zeta", "alpha"}
	for i, want := range wantOrder {
		if entries[i].Name != want {
			t.Fatalf("order[%d] = %q, want %q (all: %+v)", i, entries[i].Name, want, entries)
		}
	}
	if entries[2].Enabled {
		t.Errorf("alpha should be reported disabled")
	}
	if !entries[0].Enabled || !entries[1].Enabled {
		t.Errorf("enabled skills should be reported enabled: %+v", entries)
	}
	if entries[0].Description == "" {
		t.Errorf("description missing for %q", entries[0].Name)
	}
}

// TestHandleSetSkillEnabledPersistsAndHidesFromDiscovery is the end-to-end
// toggle: the handler persists the project preference, drops the caches, and the
// skill immediately disappears from the catalog and the model-aware listing.
func TestHandleSetSkillEnabledPersistsAndHidesFromDiscovery(t *testing.T) {
	h, root := skillTestHandler(t)
	writeServerSkill(t, root, "alpha")
	writeServerSkill(t, root, "beta")

	req := httptest.NewRequest(http.MethodPut, "/api/skills/alpha/disable?path="+root, nil)
	rec := httptest.NewRecorder()
	h.HandleSetSkillEnabled(rec, req, "alpha", false)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["name"] != "alpha" || resp["enabled"] != false {
		t.Fatalf("unexpected response: %v", resp)
	}

	// Persisted to the project layer, not the global one.
	data, err := os.ReadFile(config.ProjectSettingsPathForRoot(root))
	if err != nil {
		t.Fatalf("project settings not written: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["skills"]; !ok {
		t.Fatalf("settings.json has no skills block: %s", data)
	}

	// Discovery now excludes it on every advertisement surface.
	if cat := skill.BuildCatalogForModel(root, "test/model"); strings.Contains(cat, "alpha") {
		t.Fatalf("catalog still advertises the disabled skill:\n%s", cat)
	}
	for _, s := range skill.LoadSkillsForModel(root, "test/model") {
		if s.Name == "alpha" {
			t.Fatal("disabled skill still in LoadSkillsForModel")
		}
	}
	// ...and the agent tool refuses it.
	if s, err := skill.LoadEnabledSkillForRoot("alpha", root); err != nil || s != nil {
		t.Fatalf("LoadEnabledSkillForRoot(alpha) = (%v, %v), want (nil, nil)", s, err)
	}

	// Re-enabling through the same endpoint restores discovery.
	req = httptest.NewRequest(http.MethodPut, "/api/skills/alpha/enable?path="+root, nil)
	rec = httptest.NewRecorder()
	h.HandleSetSkillEnabled(rec, req, "alpha", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("enable status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if cat := skill.BuildCatalogForModel(root, "test/model"); !strings.Contains(cat, "alpha") {
		t.Fatalf("re-enabled skill missing from the catalog:\n%s", cat)
	}
}

func TestHandleSetSkillEnabledRejectsUnknownSkill(t *testing.T) {
	h, root := skillTestHandler(t)
	req := httptest.NewRequest(http.MethodPut, "/api/skills/nope/disable?path="+root, nil)
	rec := httptest.NewRecorder()
	h.HandleSetSkillEnabled(rec, req, "nope", false)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body %s)", rec.Code, rec.Body.String())
	}
	// No dead preference entry may be written.
	if _, err := os.Stat(config.ProjectSettingsPathForRoot(root)); err == nil {
		t.Fatalf("settings.json written for an unknown skill")
	}
}

func TestHandleSetSkillEnabledRejectsPathOutsideAllowedRoots(t *testing.T) {
	h, _ := skillTestHandler(t)
	outside := t.TempDir()
	req := httptest.NewRequest(http.MethodPut, "/api/skills/x/disable?path="+outside, nil)
	rec := httptest.NewRecorder()
	h.HandleSetSkillEnabled(rec, req, "x", false)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestHandleSetSkillEnabledRejectsKaizenSkill(t *testing.T) {
	h, root := skillTestHandler(t)
	dir := filepath.Join(root, "skills", "kaizen", "tuned")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: tuned\ndescription: tuned.\ntuned_for: test/model\n---\nbody\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	skill.InvalidateSkillCache()

	req := httptest.NewRequest(http.MethodPut, "/api/skills/tuned/disable?path="+root, nil)
	rec := httptest.NewRecorder()
	h.HandleSetSkillEnabled(rec, req, "tuned", false)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestSameSkillSet(t *testing.T) {
	cases := []struct {
		name string
		a, b map[string]bool
		want bool
	}{
		{"both nil", nil, nil, true},
		{"nil vs empty", nil, map[string]bool{}, true},
		{"same", map[string]bool{"a": true}, map[string]bool{"a": true}, true},
		{"different name", map[string]bool{"a": true}, map[string]bool{"b": true}, false},
		{"subset", map[string]bool{"a": true}, map[string]bool{"a": true, "b": true}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sameSkillSet(tc.a, tc.b); got != tc.want {
				t.Fatalf("sameSkillSet(%v, %v) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

// TestReconcileProfileAgentRebuildsOnSkillToggle proves a skill toggle lands in
// a LIVE chat: the catalog is baked into the agent at build time, so reconcile
// must rebuild when the effective disabled set moved. This also covers the
// mid-turn deferral path, which relies on that same comparison on the next turn.
func TestReconcileProfileAgentRebuildsOnSkillToggle(t *testing.T) {
	h, root := skillTestHandler(t)
	writeServerSkill(t, root, "alpha")
	id := session.NewSessionID()
	h.sessions.Register(id, root)

	ready := make(chan struct{})
	close(ready)
	h.mcpCache = &mcpCache{ready: ready, tools: []tool.Tool{}, errs: nil}

	as, stage, err := h.buildAgentSession(id, "opencode-go/deepseek-v4-flash", nil, root)
	if err != nil {
		t.Fatalf("build agent: %v (stage %s)", err, stage)
	}
	defer as.agent.Shutdown()
	h.replaceAgentSession(id, as)

	// No toggle: reconcile keeps the same agent.
	same, err := h.reconcileProfileAgent(id, as, as.model)
	if err != nil {
		t.Fatalf("reconcile (no toggle): %v", err)
	}
	if same != as {
		t.Fatalf("reconcile rebuilt with no skill change")
	}

	if err := config.SaveSkillEnabled(root, "alpha", false); err != nil {
		t.Fatalf("disable: %v", err)
	}
	skill.InvalidateSkillCache()

	reb, err := h.reconcileProfileAgent(id, h.lookupAgentSession(id), as.model)
	if err != nil {
		t.Fatalf("reconcile (after toggle): %v", err)
	}
	if reb == as {
		t.Fatalf("expected a rebuilt agent after the skill toggle")
	}
	defer reb.agent.Shutdown()
	if got := h.lookupAgentSession(id); got != reb {
		t.Fatalf("registry should hold the rebuilt agent")
	}
	if !reb.disabledSkills["alpha"] {
		t.Fatalf("rebuilt agent did not snapshot the disabled set: %v", reb.disabledSkills)
	}
}

// TestHandleListSkillsScoping pins the slash-list contract: an UNSCOPED request
// is the shared, project-agnostic list (global disable layer only, so one
// project's disable cannot hide a skill from every other project), while a
// request that names a project (?path= or ?session_id=) also honours that
// project's own disables — otherwise the slash menu would keep offering a skill
// the project switched off.
func TestHandleListSkillsScoping(t *testing.T) {
	home := t.TempDir()
	setHomeTree(t, home)
	skill.InvalidateSkillCache()
	t.Cleanup(skill.InvalidateSkillCache)

	// GetSkillStatus reads the user-global install dir, so install the fixture
	// there to make it appear in this endpoint's response.
	dir := filepath.Join(home, ".config", "opencode", "skills", "alpha")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: alpha\ndescription: alpha helper.\n---\n\nbody\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	h := NewHandler()
	project := t.TempDir()
	h.workDir = project

	listed := func(t *testing.T, query string) []string {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/skills"+query, nil)
		rec := httptest.NewRecorder()
		h.HandleListSkills(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
		var entries []struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &entries); err != nil {
			t.Fatalf("unmarshal %s: %v", rec.Body.String(), err)
		}
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name)
		}
		return names
	}
	scoped := "?path=" + url.QueryEscape(project)

	if names := listed(t, scoped); !containsString(names, "alpha") {
		t.Fatalf("precondition: alpha not listed: %v", names)
	}

	// A PROJECT-scoped disable hides the name for that project's slash list...
	if err := config.SaveSkillEnabled(project, "alpha", false); err != nil {
		t.Fatalf("project disable: %v", err)
	}
	skill.InvalidateSkillCache()
	if names := listed(t, scoped); containsString(names, "alpha") {
		t.Fatalf("a project-disabled skill is still offered in that project's list: %v", names)
	}
	// ...but not in the shared list, so another project is unaffected.
	if names := listed(t, ""); !containsString(names, "alpha") {
		t.Fatalf("a project-scoped disable leaked into the shared /api/skills list: %v", names)
	}

	// A GLOBAL disable is a statement about every project: it hides the name
	// everywhere, scoped or not.
	if err := config.SaveSkillEnabled("", "alpha", false); err != nil {
		t.Fatalf("global disable: %v", err)
	}
	skill.InvalidateSkillCache()
	for _, q := range []string{"", scoped} {
		if names := listed(t, q); containsString(names, "alpha") {
			t.Fatalf("a global disable was not applied to /api/skills%s: %v", q, names)
		}
	}

	// An unregistered ?path= is rejected, never silently widened to the default.
	req := httptest.NewRequest(http.MethodGet, "/api/skills?path="+url.QueryEscape(t.TempDir()), nil)
	rec := httptest.NewRecorder()
	h.HandleListSkills(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unregistered path: status = %d, want 400", rec.Code)
	}
}

// The unscoped fallback listing (used when the installer index is unreadable)
// must not pick up the process cwd's project skills: LoadSkillsForRoot("")
// resolves a blank root against the cwd, which on a multi-project server is an
// unrelated directory.
func TestLoadGlobalSkillsIgnoresProcessCwd(t *testing.T) {
	setHomeTree(t, t.TempDir())
	skill.InvalidateSkillCache()
	t.Cleanup(skill.InvalidateSkillCache)

	cwdProject := t.TempDir()
	writeServerSkill(t, cwdProject, "cwd-only")
	t.Chdir(cwdProject)

	for _, s := range skill.LoadGlobalSkills() {
		if s.Name == "cwd-only" {
			t.Fatalf("LoadGlobalSkills leaked the cwd project's skill: %s", s.Source)
		}
	}
	// Control: the cwd-resolving loader does see it, so the test can fail.
	found := false
	for _, s := range skill.LoadSkillsForRoot("") {
		found = found || s.Name == "cwd-only"
	}
	if !found {
		t.Fatal("control failed: LoadSkillsForRoot(\"\") should resolve against the cwd")
	}
}

// A toggle must not rebuild an agent out from under a turn that is running or
// about to start: the turn lock is the gate, and a refused rebuild leaves the
// resident session (and its transcript) untouched.
func TestRebuildAgentForPreferenceChangeRespectsTurnLock(t *testing.T) {
	h, root := skillTestHandler(t)
	id := session.NewSessionID()
	h.sessions.Register(id, root)
	ready := make(chan struct{})
	close(ready)
	h.mcpCache = &mcpCache{ready: ready, tools: []tool.Tool{}, errs: nil}

	as, stage, err := h.buildAgentSession(id, "opencode-go/deepseek-v4-flash", nil, root)
	if err != nil {
		t.Fatalf("build agent: %v (stage %s)", err, stage)
	}
	defer as.agent.Shutdown()
	h.replaceAgentSession(id, as)

	// A job holding the turn lock (running, or between persist and runTurn).
	lock := h.sessionTurnLock(id)
	lock.Lock()
	h.rebuildAgentForPreferenceChange(id, "skill")
	if got := h.lookupAgentSession(id); got != as {
		lock.Unlock()
		t.Fatal("agent was rebuilt while the session turn lock was held")
	}
	lock.Unlock()

	// A busy session (as.mu owned by another writer) is likewise left alone.
	as.mu.Lock()
	h.rebuildAgentForPreferenceChange(id, "skill")
	same := h.lookupAgentSession(id) == as
	as.mu.Unlock()
	if !same {
		t.Fatal("agent was rebuilt while the session mutex was held")
	}

	// Idle: the rebuild goes through and carries the transcript over.
	as.mu.Lock()
	as.messages = append(as.messages, agent.Message{Role: "user", Content: "hello"})
	as.mu.Unlock()
	h.rebuildAgentForPreferenceChange(id, "skill")
	reb := h.lookupAgentSession(id)
	if reb == as {
		t.Fatal("idle session was not rebuilt")
	}
	defer reb.agent.Shutdown()
	if len(reb.messages) != 1 || reb.messages[0].Content != "hello" {
		t.Fatalf("rebuilt session lost the transcript: %+v", reb.messages)
	}
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// TestHandleSetSkillEnabledGlobalScopeWritesTheGlobalLayer pins that the
// Settings → Skills form can actually reach the GLOBAL layer.
//
// WHY THIS NEEDS AN EXPLICIT SCOPE: the form is a machine-wide control ("a
// skill switched off here is disabled in every NEW chat on this machine"), but
// it cannot reach the global layer by OMITTING its params. skillPrefsRoot
// falls back to h.workDir, and h.workDir is never empty in a real server
// (NewHandler sets it to os.Getwd(), SetWorkDir overwrites it, and the
// desktop boot sets it to the last-used project). So an un-scoped request
// wrote the PROJECT layer of whatever project the server last used, and the
// form's promise was false.
//
// `?scope=global` is the explicit, unambiguous selector.
func TestHandleSetSkillEnabledGlobalScopeWritesTheGlobalLayer(t *testing.T) {
	h, root := skillTestHandler(t)
	writeServerSkill(t, root, "alpha")
	skill.InvalidateSkillCache()

	// Disable with the explicit global scope.
	req := httptest.NewRequest(http.MethodPut, "/api/skills/alpha/disable?scope=global", nil)
	rec := httptest.NewRecorder()
	h.HandleSetSkillEnabled(rec, req, "alpha", false)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		ProjectRoot string `json:"project_root"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal %s: %v", rec.Body.String(), err)
	}
	if resp.ProjectRoot != "" {
		t.Fatalf("project_root = %q, want \"\" (the global layer has no project root)", resp.ProjectRoot)
	}

	// The GLOBAL layer must hold the entry, and the PROJECT layer must NOT.
	//
	// EffectiveDisabledSkills("") resolves the global layer alone (an empty
	// root means "no project layer"); EffectiveDisabledSkills(root) is the
	// combined view a real chat would use.
	globalOnly, err := config.EffectiveDisabledSkills("")
	if err != nil {
		t.Fatalf("resolve global layer: %v", err)
	}
	if !globalOnly["alpha"] {
		t.Errorf("the global layer does not hold the disabled entry: %v", globalOnly)
	}
	// The PROJECT layer must be untouched. The project-scoped view is the union
	// of both layers, so it cannot tell them apart on its own — compare the
	// project layer by reading it back through the effective set with a
	// DISABLED global entry: if the project layer had also been written, the
	// project's own settings file would carry the name.
	projectSettings := config.ProjectSettingsPathForRoot(root)
	data, readErr := os.ReadFile(projectSettings)
	if readErr == nil && strings.Contains(string(data), "alpha") {
		t.Errorf("the global toggle leaked into the PROJECT layer: %s", string(data))
	}
}

// TestSkillPrefsRootRejectsUnknownScope pins that a typo'd scope is a 400,
// not a silent fall-through to the project layer.
func TestSkillPrefsRootRejectsUnknownScope(t *testing.T) {
	h, _ := skillTestHandler(t)
	req := httptest.NewRequest(http.MethodPut, "/api/skills/alpha/disable?scope=globl", nil)
	rec := httptest.NewRecorder()
	h.HandleSetSkillEnabled(rec, req, "alpha", false)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (an unknown scope must be rejected): %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}
