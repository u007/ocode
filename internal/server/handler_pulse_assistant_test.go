package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/auth"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/tool"
)

const pulseTestModel = "opencode-go/deepseek-v4-flash"

// newPulseAssistantHandler builds a Handler over an isolated HOME with a model
// and credential, so buildAgentSession can construct a real client.
func newPulseAssistantHandler(t *testing.T, home string) *Handler {
	t.Helper()
	credVersion := auth.CredentialVersion()
	t.Cleanup(func() { auth.SetCredentialVersionForTest(credVersion) })
	prevCred, hadPrev := auth.Get("opencode-go")
	t.Cleanup(func() {
		if hadPrev {
			_ = auth.Set("opencode-go", prevCred)
		} else {
			_ = auth.Remove("opencode-go")
		}
	})
	if err := auth.Set("opencode-go", auth.Credential{Kind: auth.KindAPIKey, Key: "pulse-test-key"}); err != nil {
		t.Fatalf("set credential: %v", err)
	}
	h := NewHandler()
	ready := make(chan struct{})
	close(ready)
	h.mcpCache = &mcpCache{ready: ready, tools: []tool.Tool{}, errs: nil}
	h.mu.Lock()
	h.cfg.Model = pulseTestModel
	h.cfg.Ocode.PulseModel = ""
	h.mu.Unlock()
	t.Cleanup(func() {
		h.mu.Lock()
		agents := make([]*agentSession, 0, len(h.agents))
		for _, as := range h.agents {
			agents = append(agents, as)
		}
		h.mu.Unlock()
		for _, as := range agents {
			if as != nil && as.agent != nil {
				as.agent.Shutdown()
			}
		}
	})
	return h
}

func getPulseAssistant(t *testing.T, h *Handler) (int, map[string]string, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.HandlePulseAssistant(rec, httptest.NewRequest(http.MethodGet, "/api/pulse/assistant", nil))
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return rec.Code, body, rec.Body.String()
}

func TestHandlePulseAssistantMintsPersistsAndIsStable(t *testing.T) {
	home := t.TempDir()
	setHomeTree(t, home)
	h := newPulseAssistantHandler(t, home)

	code, body, raw := getPulseAssistant(t, h)
	t.Logf("first GET /api/pulse/assistant -> %d %s", code, strings.TrimSpace(raw))
	if code != http.StatusOK {
		t.Fatalf("status = %d, body %s", code, raw)
	}
	id := body["session_id"]
	if !isPulseSession(id) {
		t.Fatalf("session_id %q lacks the %q prefix", id, pulseSessionPrefix)
	}
	if body["model"] != pulseTestModel {
		t.Fatalf("model = %q, want %q", body["model"], pulseTestModel)
	}

	// Persisted at <root>/state.json as {"session_id": ...}.
	root, err := pulseRootPath()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "state.json"))
	if err != nil {
		t.Fatalf("state.json not written: %v", err)
	}
	var st pulseAssistantState
	if err := json.Unmarshal(data, &st); err != nil || st.SessionID != id {
		t.Fatalf("state.json = %s (err %v), want session_id %q", data, err, id)
	}

	// GET does not pre-build the agent; the first message builds it through the
	// normal path, and a following message must not rebuild it.
	if h.lookupAgentSession(id) != nil {
		t.Fatal("GET must not pre-build the live agent")
	}
	as, err := h.getOrCreateAgentSession(id)
	if err != nil {
		t.Fatalf("build on first message: %v", err)
	}
	if reb, err := h.reconcileProfileAgent(id, as, h.effectiveSessionModel(id)); err != nil || reb != as {
		t.Fatalf("first message rebuilt the assistant (same=%v err=%v)", reb == as, err)
	}
	defs := as.agent.GetToolDefinitions()
	got := map[string]bool{}
	for _, d := range defs {
		got[toolDefName(t, d)] = true
	}
	want := []string{"pulse_board", "session_read", "session_recap", "terminal_tabs", "terminal_read", "terminal_screen", "agent_runs", "session_send", "session_command", "permission_resolve", "question_answer", "memory_read", "memory_write"}
	for _, n := range want {
		if !got[n] {
			t.Errorf("assistant is missing tool %q (has %v)", n, got)
		}
	}
	for n := range got {
		found := false
		for _, w := range want {
			if n == w {
				found = true
			}
		}
		if !found {
			t.Errorf("assistant has unexpected tool %q", n)
		}
	}
	if entry := h.sessions.Lookup(id); entry == nil || entry.ProjectRoot != root {
		t.Fatalf("registry entry = %+v, want ProjectRoot %q", entry, root)
	}

	// The prompt lists exactly the registered tools and is the cached system block.
	base := as.agent.BasePromptMessages()
	if len(base) != 1 || base[0].Role != "system" {
		t.Fatalf("base prompt = %+v, want one system message", base)
	}
	for _, n := range want {
		if !strings.Contains(base[0].Content, "- "+n+":") {
			t.Errorf("prompt does not list tool %q", n)
		}
	}

	// Second GET: same id.
	code2, body2, _ := getPulseAssistant(t, h)
	if code2 != http.StatusOK || body2["session_id"] != id {
		t.Fatalf("second GET = %d %v, want same id %q", code2, body2, id)
	}

	// A rebuilt Handler over the same data dir returns the same id.
	h2 := newPulseAssistantHandler(t, home)
	code3, body3, _ := getPulseAssistant(t, h2)
	if code3 != http.StatusOK || body3["session_id"] != id {
		t.Fatalf("after handler rebuild GET = %d %v, want same id %q", code3, body3, id)
	}
}

func toolDefName(t *testing.T, d map[string]interface{}) string {
	t.Helper()
	if n, ok := d["name"].(string); ok {
		return n
	}
	if fn, ok := d["function"].(map[string]interface{}); ok {
		if n, ok := fn["name"].(string); ok {
			return n
		}
	}
	t.Fatalf("tool definition without a name: %v", d)
	return ""
}

func TestHandlePulseAssistantRejectsCorruptState(t *testing.T) {
	home := t.TempDir()
	setHomeTree(t, home)
	h := newPulseAssistantHandler(t, home)
	root, err := pulseAssistantRoot()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "state.json"), []byte(`{"session_id":"ses_nope"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, raw := getPulseAssistant(t, h)
	if code != http.StatusInternalServerError {
		t.Fatalf("status = %d (%s), want 500 for a state file naming a non-pulse session", code, raw)
	}

	// A pulse_ id that is not a safe path segment is refused the same way, and
	// never reaches the transcript probe.
	if err := os.WriteFile(filepath.Join(root, "state.json"), []byte(`{"session_id":"pulse_/../../x"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, raw = getPulseAssistant(t, h)
	if code != http.StatusInternalServerError {
		t.Fatalf("status = %d (%s), want 500 for a state file naming a traversal id", code, raw)
	}
}

func TestPulseModelSlotRoundTrip(t *testing.T) {
	home := t.TempDir()
	setHomeTree(t, home)
	h := newPulseAssistantHandler(t, home)

	get := func() string {
		rec := httptest.NewRecorder()
		h.HandleGetPulseModel(rec, httptest.NewRequest(http.MethodGet, "/api/config/pulse-model", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET status %d", rec.Code)
		}
		var b map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil {
			t.Fatal(err)
		}
		return b["model"]
	}
	put := func(body string) int {
		rec := httptest.NewRecorder()
		h.HandleSetPulseModel(rec, httptest.NewRequest(http.MethodPut, "/api/config/pulse-model", strings.NewReader(body)))
		return rec.Code
	}

	if got := get(); got != "" {
		t.Fatalf("unset slot = %q, want empty", got)
	}
	if code := put(`{"model":"opencode-go/other"}`); code != http.StatusOK {
		t.Fatalf("PUT status %d", code)
	}
	if got := get(); got != "opencode-go/other" {
		t.Fatalf("after set = %q", got)
	}
	// Persisted through the targeted saver: a fresh load from disk sees it.
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if cfg.Ocode.PulseModel != "opencode-go/other" {
		t.Fatalf("persisted pulse_model = %q", cfg.Ocode.PulseModel)
	}

	// The assistant's effective model follows the slot; a per-session override wins.
	id := pulseSessionPrefix + "x"
	if got := h.effectiveSessionModel(id); got != "opencode-go/other" {
		t.Fatalf("effective model for assistant = %q, want the slot", got)
	}
	if got := h.effectiveSessionModel("ses_normal"); got != pulseTestModel {
		t.Fatalf("effective model for a normal session = %q, want the chat model", got)
	}

	if code := put(`{"model":""}`); code != http.StatusOK {
		t.Fatalf("clear status %d", code)
	}
	if got := get(); got != "" {
		t.Fatalf("after clear = %q", got)
	}
	cfg, err = config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Ocode.PulseModel != "" {
		t.Fatalf("persisted pulse_model after clear = %q", cfg.Ocode.PulseModel)
	}
	if got := h.effectiveSessionModel(id); got != pulseTestModel {
		t.Fatalf("effective model after clear = %q, want the chat model", got)
	}
	if code := put(`{}`); code != http.StatusBadRequest {
		t.Fatalf("body without model: status %d, want 400", code)
	}
}

func TestPulseAutoReceiveRoundTrip(t *testing.T) {
	home := t.TempDir()
	setHomeTree(t, home)
	h := newPulseAssistantHandler(t, home)

	get := func() bool {
		rec := httptest.NewRecorder()
		h.HandleGetPulseAutoReceive(rec, httptest.NewRequest(http.MethodGet, "/api/config/pulse-auto-receive", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET status %d", rec.Code)
		}
		var b map[string]bool
		if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil {
			t.Fatal(err)
		}
		return b["enabled"]
	}
	put := func(enabled bool) int {
		rec := httptest.NewRecorder()
		body := `{"enabled":` + strconv.FormatBool(enabled) + `}`
		h.HandleSetPulseAutoReceive(rec, httptest.NewRequest(http.MethodPut, "/api/config/pulse-auto-receive", strings.NewReader(body)))
		return rec.Code
	}

	if get() {
		t.Fatalf("default auto-receive = true, want false")
	}
	if code := put(true); code != http.StatusOK {
		t.Fatalf("PUT status %d", code)
	}
	if !get() {
		t.Fatalf("after set true = false, want true")
	}
	if code := put(false); code != http.StatusOK {
		t.Fatalf("PUT false status %d", code)
	}
	if get() {
		t.Fatalf("after set false = true, want false")
	}
	if code := put(true); code != http.StatusOK {
		t.Fatalf("PUT true status %d", code)
	}
	// Persisted through the targeted saver.
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if !cfg.Ocode.PulseAutoReceive {
		t.Fatalf("persisted pulse_auto_receive = false, want true")
	}
}

func TestPulseAutoGapMsRoundTrip(t *testing.T) {
	home := t.TempDir()
	setHomeTree(t, home)
	h := newPulseAssistantHandler(t, home)

	get := func() int {
		rec := httptest.NewRecorder()
		h.HandleGetPulseAutoGapMs(rec, httptest.NewRequest(http.MethodGet, "/api/config/pulse-auto-gap-ms", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET status %d", rec.Code)
		}
		var b map[string]int
		if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil {
			t.Fatal(err)
		}
		return b["gap_ms"]
	}
	put := func(ms int) int {
		rec := httptest.NewRecorder()
		body := `{"gap_ms":` + strconv.Itoa(ms) + `}`
		h.HandleSetPulseAutoGapMs(rec, httptest.NewRequest(http.MethodPut, "/api/config/pulse-auto-gap-ms", strings.NewReader(body)))
		return rec.Code
	}

	if got := get(); got != 2000 {
		t.Fatalf("default gap_ms = %d, want 2000", got)
	}
	if code := put(5000); code != http.StatusOK {
		t.Fatalf("PUT status %d", code)
	}
	if got := get(); got != 5000 {
		t.Fatalf("after set = %d, want 5000", got)
	}
	if code := put(-1); code != http.StatusBadRequest {
		t.Fatalf("negative gap_ms status %d, want 400", code)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if cfg.Ocode.PulseAutoGapMs != 5000 {
		t.Fatalf("persisted pulse_auto_gap_ms = %d, want 5000", cfg.Ocode.PulseAutoGapMs)
	}
}

func TestPulseAutoCompactRoundTrip(t *testing.T) {
	home := t.TempDir()
	setHomeTree(t, home)
	h := newPulseAssistantHandler(t, home)

	get := func() bool {
		rec := httptest.NewRecorder()
		h.HandleGetPulseAutoCompact(rec, httptest.NewRequest(http.MethodGet, "/api/config/pulse-auto-compact", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET status %d", rec.Code)
		}
		var b map[string]bool
		if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil {
			t.Fatal(err)
		}
		return b["enabled"]
	}
	put := func(enabled bool) int {
		rec := httptest.NewRecorder()
		body := `{"enabled":` + strconv.FormatBool(enabled) + `}`
		h.HandleSetPulseAutoCompact(rec, httptest.NewRequest(http.MethodPut, "/api/config/pulse-auto-compact", strings.NewReader(body)))
		return rec.Code
	}

	if get() {
		t.Fatalf("default auto-compact = true, want false")
	}
	if code := put(true); code != http.StatusOK {
		t.Fatalf("PUT status %d", code)
	}
	if !get() {
		t.Fatalf("after set true = false, want true")
	}
	if code := put(false); code != http.StatusOK {
		t.Fatalf("PUT false status %d", code)
	}
	if get() {
		t.Fatalf("after set false = true, want false")
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if cfg.Ocode.PulseAutoCompact {
		t.Fatalf("persisted pulse_auto_compact = true, want false")
	}
}

func TestPulseCompactThresholdsRoundTrip(t *testing.T) {
	home := t.TempDir()
	setHomeTree(t, home)
	h := newPulseAssistantHandler(t, home)

	get := func() (int, int) {
		rec := httptest.NewRecorder()
		h.HandleGetPulseCompactThresholds(rec, httptest.NewRequest(http.MethodGet, "/api/config/pulse-compact-thresholds", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET status %d", rec.Code)
		}
		var b struct {
			TokenThreshold int `json:"token_threshold"`
			PctThreshold   int `json:"pct_threshold"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil {
			t.Fatal(err)
		}
		return b.TokenThreshold, b.PctThreshold
	}
	put := func(token, pct *int) int {
		rec := httptest.NewRecorder()
		parts := []string{`{`}
		if token != nil {
			parts = append(parts, `"token_threshold":`+strconv.Itoa(*token))
		}
		if pct != nil {
			if len(parts) > 1 {
				parts = append(parts, ",")
			}
			parts = append(parts, `"pct_threshold":`+strconv.Itoa(*pct))
		}
		parts = append(parts, `}`)
		body := strings.Join(parts, "")
		h.HandleSetPulseCompactThresholds(rec, httptest.NewRequest(http.MethodPut, "/api/config/pulse-compact-thresholds", strings.NewReader(body)))
		return rec.Code
	}

	token, pct := get()
	if token != 250000 {
		t.Fatalf("default token_threshold = %d, want 250000", token)
	}
	if pct != 80 {
		t.Fatalf("default pct_threshold = %d, want 80", pct)
	}
	tk := 300000
	pc := 75
	if code := put(&tk, &pc); code != http.StatusOK {
		t.Fatalf("PUT status %d", code)
	}
	token, pct = get()
	if token != 300000 {
		t.Fatalf("after set token_threshold = %d, want 300000", token)
	}
	if pct != 75 {
		t.Fatalf("after set pct_threshold = %d, want 75", pct)
	}
	// Empty body is invalid.
	if code := put(nil, nil); code != http.StatusBadRequest {
		t.Fatalf("empty body status %d, want 400", code)
	}
	// Negative values are invalid.
	neg := -5
	if code := put(&neg, nil); code != http.StatusBadRequest {
		t.Fatalf("negative token_threshold status %d, want 400", code)
	}
	neg = -5
	if code := put(nil, &neg); code != http.StatusBadRequest {
		t.Fatalf("negative pct_threshold status %d, want 400", code)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	// Negative puts were rejected, so the last accepted values (300000/75) persist.
	if cfg.Ocode.PulseCompactTokenThreshold != 300000 {
		t.Fatalf("persisted token_threshold = %d, want 300000 (negative puts rejected)", cfg.Ocode.PulseCompactTokenThreshold)
	}
	if cfg.Ocode.PulseCompactPctThreshold != 75 {
		t.Fatalf("persisted pct_threshold = %d, want 75 (negative puts rejected)", cfg.Ocode.PulseCompactPctThreshold)
	}
}

// Driving the real mux catches a missing or shadowed route (a 404 from the SPA
// fallback), which calling the handlers directly cannot.
func TestPulseAssistantRoutesAreRegistered(t *testing.T) {
	setHomeTree(t, t.TempDir())
	srv := New("127.0.0.1:0", "", "", nil)
	h := srv.serveHandler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/config/pulse-model", strings.NewReader(`{"model":"opencode-go/x"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /api/config/pulse-model = %d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/config/pulse-model", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"opencode-go/x"`) {
		t.Fatalf("GET /api/config/pulse-model = %d %s", rec.Code, rec.Body.String())
	}
	t.Logf("GET /api/config/pulse-model -> %d %s", rec.Code, strings.TrimSpace(rec.Body.String()))
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/pulse/assistant", nil))
	// No model/credential in this harness: the route must answer from the handler
	// (400 "no model configured" or 200), never the SPA fallback.
	if rec.Code == http.StatusNotFound || !strings.Contains(rec.Header().Get("Content-Type"), "json") {
		t.Fatalf("GET /api/pulse/assistant = %d (%s) %s, route missing", rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
	}
}

func TestPulseAssistantSessionIsReadableAfterMintAndRemintedWhenGone(t *testing.T) {
	home := t.TempDir()
	setHomeTree(t, home)
	h := newPulseAssistantHandler(t, home)
	_, body, _ := getPulseAssistant(t, h)
	id := body["session_id"]

	// Through the real mux the server's own handler is a different instance, but
	// the transcript is on disk and the registry resolves the pulse root.
	srv := New("127.0.0.1:0", "", "", nil)
	srv.handler = h
	mux := srv.serveHandler()
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/sessions/"+id, nil))
	t.Logf("GET /api/sessions/%s -> %d %s", id, rec.Code, strings.TrimSpace(rec.Body.String()))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var detail struct {
		ID       string            `json:"id"`
		Title    string            `json:"title"`
		Messages []json.RawMessage `json:"messages"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Title != pulseAssistantTitle || len(detail.Messages) != 0 {
		t.Fatalf("detail = %+v, want title %q and zero messages", detail, pulseAssistantTitle)
	}

	// Transcript deleted: the next GET re-mints instead of returning a dead id.
	root, _ := pulseRootPath()
	h2 := newPulseAssistantHandler(t, home)
	matches, _ := filepath.Glob(filepath.Join(filepath.Dir(root), "project", "*", "sessions", id+".*"))
	if len(matches) == 0 {
		t.Fatal("could not locate the persisted transcript file to delete")
	}
	for _, m := range matches {
		if err := os.Remove(m); err != nil {
			t.Fatal(err)
		}
	}
	_, body2, _ := getPulseAssistant(t, h2)
	if body2["session_id"] == id || !isPulseSession(body2["session_id"]) {
		t.Fatalf("expected a re-minted id, got %q (old %q)", body2["session_id"], id)
	}
}
