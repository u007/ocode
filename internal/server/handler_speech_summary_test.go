package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/config"
)

// codeBlockBody is the JSON request body for a fenced-code message. Built with
// escapes rather than a raw literal because the payload itself contains
// backticks.
const codeBlockBody = "{\"text\":\"```go\\nretries = 3\\n```\"}"

// codeBlockText is what that body decodes to, asserted against the client's
// observed input.
const codeBlockText = "```go\nretries = 3\n```"

// speechSummaryStubClient is the planted session client: it answers a
// summarise request with canned prose and records what it was asked.
type speechSummaryStubClient struct {
	system  string
	user    string
	reply   string
	err     error
	replies int
}

func (c *speechSummaryStubClient) Chat(messages []agent.Message, _ []map[string]interface{}) (*agent.Message, error) {
	c.replies++
	for _, m := range messages {
		switch m.Role {
		case "system":
			c.system = m.Content
		case "user":
			c.user = m.Content
		}
	}
	if c.err != nil {
		return nil, c.err
	}
	return &agent.Message{Role: "assistant", Content: c.reply}, nil
}
func (c *speechSummaryStubClient) GetProvider() string { return "fake" }
func (c *speechSummaryStubClient) GetModel() string    { return "fake-model" }

// plantSpeechSummarySession registers a session whose agent is the stub.
//
// SpeechSummaryModel is left EMPTY and the small model disabled so the
// summariser resolves to the session's own client (the stub) rather than
// building a real provider client — the same fallback chain production uses for
// a user who has not picked a model.
func plantSpeechSummarySession(t *testing.T, h *Handler, id string, stub agent.LLMClient) {
	t.Helper()
	h.sessions.Register(id, t.TempDir())
	cfg := &config.Config{}
	cfg.Ocode.SmallModelEnabled = false
	ag := agent.NewAgent(stub, nil, cfg, nil)
	h.mu.Lock()
	h.agents[id] = &agentSession{agent: ag, model: "fake-model"}
	h.mu.Unlock()
}

// TestHandleGetSpeechSummaryConfigReportsTheStoredPair: the GET is a pure
// pass-through of the handler's config, and `enabled` must always be serialised
// (even as true) so the UI never has to guess between "off" and "unknown".
//
// The "a fresh install defaults ON" claim is NOT asserted here: testConfigHandler
// builds a zero-valued OcodeConfig, whereas production loads
// defaultOcodeConfig(). That default is pinned by
// TestSpeechSummaryEnabledDefaultsOn in internal/config.
func TestHandleGetSpeechSummaryConfigReportsTheStoredPair(t *testing.T) {
	h := testConfigHandler(t)
	h.mu.Lock()
	h.cfg.Ocode.SpeechSummaryModel = "anthropic/claude-haiku-4-5"
	h.cfg.Ocode.SpeechSummaryEnabled = true
	h.mu.Unlock()

	w := httptest.NewRecorder()
	h.HandleGetSpeechSummaryConfig(w, httptest.NewRequest("GET", "/api/config/ocode/speech-summary", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var got speechSummaryConfigResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Enabled {
		t.Error("enabled must be reported as stored")
	}
	if got.Model != "anthropic/claude-haiku-4-5" {
		t.Errorf("model = %q", got.Model)
	}
	if !strings.Contains(w.Body.String(), `"enabled":true`) {
		t.Errorf("enabled must be serialised explicitly, body=%s", w.Body.String())
	}
}

// TestHandleSetSpeechSummaryConfigPartialUpdatePreservesTheOtherField is the
// important one: the sidebar checkbox and the model picker are separate
// controls, and each must leave the other's field alone.
func TestHandleSetSpeechSummaryConfigPartialUpdatePreservesTheOtherField(t *testing.T) {
	h := testConfigHandler(t)
	// Seed the persisted block too, not just the in-memory copy: the merge runs
	// against the config re-read from disk under the lock.
	model := "anthropic/claude-haiku-4-5"
	on := true
	if _, _, err := config.SaveOcodeSpeechSummary(&model, &on); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	h.mu.Lock()
	h.cfg.Ocode.SpeechSummaryModel = model
	h.cfg.Ocode.SpeechSummaryEnabled = on
	h.mu.Unlock()

	// 1. Flipping only the gate must not clear the model.
	w := httptest.NewRecorder()
	h.HandleSetSpeechSummaryConfig(w, httptest.NewRequest("PUT", "/api/config/ocode/speech-summary", strings.NewReader(`{"enabled":false}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	h.mu.Lock()
	got := h.cfg.Ocode
	h.mu.Unlock()
	if got.SpeechSummaryEnabled {
		t.Error("enabled must be false")
	}
	if got.SpeechSummaryModel != "anthropic/claude-haiku-4-5" {
		t.Errorf("a gate-only write cleared the model: %q", got.SpeechSummaryModel)
	}

	// 2. Changing only the model must not flip the gate back on.
	w = httptest.NewRecorder()
	h.HandleSetSpeechSummaryConfig(w, httptest.NewRequest("PUT", "/api/config/ocode/speech-summary", strings.NewReader(`{"model":"openai/gpt-4o-mini"}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	h.mu.Lock()
	got = h.cfg.Ocode
	h.mu.Unlock()
	if got.SpeechSummaryModel != "openai/gpt-4o-mini" {
		t.Errorf("model = %q", got.SpeechSummaryModel)
	}
	if got.SpeechSummaryEnabled {
		t.Error("a model-only write flipped the gate")
	}

	// The response reports the merged pair, which is what the UI renders from.
	var resp speechSummaryConfigResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Model != "openai/gpt-4o-mini" || resp.Enabled {
		t.Errorf("response must be the merged pair, got %+v", resp)
	}
}

// TestHandleSetSpeechSummaryConfigClearsModelWithExplicitEmpty pins the
// absent-vs-zero distinction: "Clear" must be able to empty the model.
func TestHandleSetSpeechSummaryConfigClearsModelWithExplicitEmpty(t *testing.T) {
	h := testConfigHandler(t)
	h.mu.Lock()
	h.cfg.Ocode.SpeechSummaryModel = "openai/gpt-4o-mini"
	h.mu.Unlock()

	w := httptest.NewRecorder()
	h.HandleSetSpeechSummaryConfig(w, httptest.NewRequest("PUT", "/api/config/ocode/speech-summary", strings.NewReader(`{"model":""}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	h.mu.Lock()
	got := h.cfg.Ocode.SpeechSummaryModel
	h.mu.Unlock()
	if got != "" {
		t.Errorf("an explicit empty model must clear it, got %q", got)
	}
}

func TestHandleSetSpeechSummaryConfigRejectsEmptyAndMalformedBodies(t *testing.T) {
	h := testConfigHandler(t)
	for _, body := range []string{`{}`, `{"unrelated":1}`, `{`} {
		w := httptest.NewRecorder()
		h.HandleSetSpeechSummaryConfig(w, httptest.NewRequest("PUT", "/api/config/ocode/speech-summary", strings.NewReader(body)))
		if w.Code != http.StatusBadRequest {
			t.Errorf("body %s: status = %d, want 400", body, w.Code)
		}
	}
}

// TestHandleSessionSpeechSummaryHappyPath drives the whole server capability:
// the session's agent rewrites the text and the spoken prose comes back.
func TestHandleSessionSpeechSummaryHappyPath(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	h := NewHandler()
	stub := &speechSummaryStubClient{reply: "It raises the retry limit to three."}
	plantSpeechSummarySession(t, h, "sess-speak", stub)

	w := httptest.NewRecorder()
	h.HandleSessionSpeechSummary(w, httptest.NewRequest("POST", "/api/sessions/sess-speak/speech-summary",
		strings.NewReader(codeBlockBody)), "sess-speak")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	var got map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["summary"] != "It raises the retry limit to three." {
		t.Errorf("summary = %q", got["summary"])
	}
	if stub.user != codeBlockText {
		t.Errorf("the raw message must be summarised, got %q", stub.user)
	}
	if stub.system == "" {
		t.Error("no system prompt was sent")
	}
}

// TestHandleSessionSpeechSummaryEmptyOnSummariserFailure is the degradation
// contract the web layer relies on: a provider failure is reported as an empty
// summary in a 200 — NOT as an error — so the browser speaks the full text
// instead of failing.
func TestHandleSessionSpeechSummaryEmptyOnSummariserFailure(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	h := NewHandler()
	stub := &speechSummaryStubClient{err: errSpeechStub}
	plantSpeechSummarySession(t, h, "sess-speak-fail", stub)

	w := httptest.NewRecorder()
	h.HandleSessionSpeechSummary(w, httptest.NewRequest("POST", "/api/sessions/sess-speak-fail/speech-summary",
		strings.NewReader(`{"text":"hello"}`)), "sess-speak-fail")

	if w.Code != http.StatusOK {
		t.Fatalf("a summariser failure must not be an HTTP error, got %d body=%s", w.Code, w.Body.String())
	}
	var got map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["summary"] != "" {
		t.Errorf("expected an empty summary, got %q", got["summary"])
	}
}

func TestHandleSessionSpeechSummaryRejectsBlankText(t *testing.T) {
	h := NewHandler()
	plantSpeechSummarySession(t, h, "sess-speak-blank", &speechSummaryStubClient{reply: "x"})

	w := httptest.NewRecorder()
	h.HandleSessionSpeechSummary(w, httptest.NewRequest("POST", "/api/sessions/sess-speak-blank/speech-summary",
		strings.NewReader(`{"text":"   "}`)), "sess-speak-blank")

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestHandleSessionSpeechSummaryUnknownSessionIsNotFound(t *testing.T) {
	h := NewHandler()

	w := httptest.NewRecorder()
	h.HandleSessionSpeechSummary(w, httptest.NewRequest("POST", "/api/sessions/nope/speech-summary",
		strings.NewReader(`{"text":"hi"}`)), "nope")

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

// errSpeechStub is a sentinel so the failure test cannot pass by matching an
// unrelated error message.
var errSpeechStub = errSpeechStubType{}

type errSpeechStubType struct{}

func (errSpeechStubType) Error() string { return "stub provider failure" }

// TestHandleSessionSpeechSummarySkipsWhileTheTurnIsActive is the regression for
// the Speak-during-a-turn stall: runTurn holds as.mu for the whole turn, so a
// summary that took as.mu would block for the turn's full duration. The handler
// must instead degrade immediately to an empty summary (the web layer speaks
// the full text), and must resume summarising once the turn ends.
func TestHandleSessionSpeechSummarySkipsWhileTheTurnIsActive(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	h := NewHandler()
	stub := &speechSummaryStubClient{reply: "must not run while busy"}
	plantSpeechSummarySession(t, h, "sess-busy", stub)
	h.sessions.setTurnActive("sess-busy", true)

	w := httptest.NewRecorder()
	h.HandleSessionSpeechSummary(w, httptest.NewRequest("POST", "/api/sessions/sess-busy/speech-summary",
		strings.NewReader(`{"text":"hello"}`)), "sess-busy")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	var got map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["summary"] != "" {
		t.Errorf("an active turn must yield an empty summary, got %q", got["summary"])
	}
	if stub.replies != 0 {
		t.Fatalf("the summariser must not run while the turn is active, replies=%d", stub.replies)
	}

	// The guard is a deferral, not a permanent disable: after the turn ends the
	// same session summarises normally.
	h.sessions.setTurnActive("sess-busy", false)
	w2 := httptest.NewRecorder()
	h.HandleSessionSpeechSummary(w2, httptest.NewRequest("POST", "/api/sessions/sess-busy/speech-summary",
		strings.NewReader(`{"text":"hello"}`)), "sess-busy")
	var got2 map[string]string
	if err := json.Unmarshal(w2.Body.Bytes(), &got2); err != nil {
		t.Fatal(err)
	}
	if got2["summary"] != "must not run while busy" {
		t.Errorf("after the turn ends the summary must run, got %q", got2["summary"])
	}
}
