package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/tool"
)

// digestModelServer is a real OpenAI-compatible endpoint for the explainer. The
// explainer clones the session's *agent.GenericClient and sends over HTTP, so a
// Go stub cannot stand in for the model: a non-GenericClient main model has no
// side client and the explainer refuses it.
type digestModelServer struct {
	reply string
	fail  bool // answer 400 instead of streaming the reply

	srv         *httptest.Server
	arrived     chan struct{} // one signal per request, buffered
	release     chan struct{}
	releaseOnce sync.Once

	mu         sync.Mutex
	gate       string   // a request whose user text contains gate is held
	userTexts  []string // user-role text of every request, arrival order
	lastUserTx string
}

// newDigestModelServer starts the endpoint for one test. The release channel is
// closed before the server, so held requests finish before Close waits on them.
func newDigestModelServer(t *testing.T, reply string) *digestModelServer {
	t.Helper()
	d := &digestModelServer{
		reply:   reply,
		arrived: make(chan struct{}, 32),
		release: make(chan struct{}),
	}
	d.srv = httptest.NewServer(http.HandlerFunc(d.serve))
	t.Cleanup(d.srv.Close)
	t.Cleanup(d.unblock)
	return d
}

func (d *digestModelServer) unblock() {
	d.releaseOnce.Do(func() { close(d.release) })
}

// block holds every request whose user text contains substr until unblock or
// the request's own context ends. The gate token must not occur elsewhere in
// the explainer payload or the main agent's continuation.
func (d *digestModelServer) block(substr string) {
	d.mu.Lock()
	d.gate = substr
	d.mu.Unlock()
}

func (d *digestModelServer) client(provider string) *agent.GenericClient {
	return &agent.GenericClient{
		Provider: provider,
		Model:    "fake-model",
		BaseURL:  d.srv.URL,
		APIKey:   "test-key",
	}
}

func (d *digestModelServer) serve(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return
	}
	var body struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var user strings.Builder
	for _, m := range body.Messages {
		var text string
		if m.Role == "user" && json.Unmarshal(m.Content, &text) == nil {
			user.WriteString(text)
			user.WriteString("\n")
		}
	}
	text := user.String()

	d.mu.Lock()
	d.userTexts = append(d.userTexts, text)
	d.lastUserTx = text
	gate := d.gate
	d.mu.Unlock()

	select {
	case d.arrived <- struct{}{}:
	default:
	}
	if gate != "" && strings.Contains(text, gate) {
		select {
		case <-d.release:
		case <-r.Context().Done():
			return
		}
	}
	if d.fail {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"message":"upstream exploded"}}`)
		return
	}
	chunk, err := json.Marshal(map[string]any{"choices": []any{map[string]any{
		"delta":         map[string]any{"role": "assistant", "content": d.reply},
		"finish_reason": "stop",
	}}})
	if err != nil {
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", chunk)
}

// lastUser is the user-role text of the most recent request.
func (d *digestModelServer) lastUser() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.lastUserTx
}

// callCount is how many requests reached the endpoint.
func (d *digestModelServer) callCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.userTexts)
}

// requestsContaining counts requests whose user text contains substr.
func (d *digestModelServer) requestsContaining(substr string) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	n := 0
	for _, text := range d.userTexts {
		if strings.Contains(text, substr) {
			n++
		}
	}
	return n
}

// plantDigestSession registers a session whose agent is a real GenericClient
// pointed at model. SetSessionID mirrors production, so the explainer owner is
// the session id, and cleanup drops that owner's runs and cache entries.
func plantDigestSession(t *testing.T, h *Handler, id string, model *digestModelServer) {
	t.Helper()
	h.sessions.Register(id, t.TempDir())
	cfg := &config.Config{}
	cfg.Ocode.SmallModelEnabled = false
	ag := agent.NewAgent(model.client("fake"), nil, cfg, nil)
	ag.SetSessionID(id)
	h.mu.Lock()
	h.agents[agentKey(id)] = &agentSession{agent: ag, model: "fake-model"}
	h.mu.Unlock()
	t.Cleanup(func() { agent.InvalidateToolDigests(id) })
}

// askMessage is the paused tool-role row a pending permission ask leaves behind.
func askMessage(t *testing.T, callID string, req agent.PermissionRequest) agent.Message {
	t.Helper()
	payload, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	return agent.Message{Role: "tool", ToolID: callID, Content: tool.SentinelPermissionAsk + string(payload)}
}

// mustArrive waits for the endpoint to receive a request.
func mustArrive(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(10 * time.Second):
		t.Fatal("the explainer never reached the model")
	}
}

func postToolDigest(h *Handler, sessionID, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/sessions/"+sessionID+"/tool-call-digest", strings.NewReader(body))
	h.HandleToolCallDigest(w, r, sessionID)
	return w
}

// postToolDigestAsync runs one explanation in the background and returns the
// channel its recorder arrives on.
func postToolDigestAsync(h *Handler, sessionID, body string) <-chan *httptest.ResponseRecorder {
	out := make(chan *httptest.ResponseRecorder, 1)
	go func() { out <- postToolDigest(h, sessionID, body) }()
	return out
}

// awaitRecorder returns the background explanation's response.
func awaitRecorder(t *testing.T, ch <-chan *httptest.ResponseRecorder) *httptest.ResponseRecorder {
	t.Helper()
	select {
	case w := <-ch:
		return w
	case <-time.After(10 * time.Second):
		t.Fatal("explanation did not return")
	}
	return nil
}

// TestHandleToolCallDigestReturnsTheExplanation is the happy path: the client's
// parameters reach the model and its prose comes back with the model id.
func TestHandleToolCallDigestReturnsTheExplanation(t *testing.T) {
	h := testConfigHandler(t)
	model := newDigestModelServer(t, "Lists the working directory.")
	plantDigestSession(t, h, "ses_digest", model)

	w := postToolDigest(h, "ses_digest",
		`{"tool":"bash","command":"ls -la","args":{"command":"ls -la"},"rule":"bash.prefix.ls","scope":"bash_prefix"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	var got map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["summary"] != "Lists the working directory." {
		t.Errorf("summary = %q", got["summary"])
	}
	if got["model"] != "fake/fake-model" {
		t.Errorf("model = %q, want the provider-qualified id", got["model"])
	}
	if !strings.Contains(model.lastUser(), "ls -la") {
		t.Errorf("command missing from the model payload: %q", model.lastUser())
	}
}

// TestHandleToolCallDigestPrefersThePendingAsk proves the server does not trust
// client-posted parameters when the ask is live: request_id wins.
func TestHandleToolCallDigestPrefersThePendingAsk(t *testing.T) {
	h := testConfigHandler(t)
	model := newDigestModelServer(t, "Deletes the build directory.")
	plantDigestSession(t, h, "ses_ask", model)

	h.mu.Lock()
	as := h.agents[agentKey("ses_ask")]
	h.mu.Unlock()
	as.messages = []agent.Message{
		{Role: "assistant", Content: "Cleaning up."},
		askMessage(t, "call_1", agent.PermissionRequest{
			ToolName: "bash",
			Command:  "rm -rf build",
			Rule:     "bash.prefix.rm",
			Scope:    agent.PermissionScopeBashPrefix,
		}),
	}

	// The posted tool intentionally disagrees with the ask.
	w := postToolDigest(h, "ses_ask", `{"request_id":"call_1","tool":"read","command":"cat README.md"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(model.lastUser(), "rm -rf build") {
		t.Errorf("the pending ask's command must be explained, payload=%q", model.lastUser())
	}
	if strings.Contains(model.lastUser(), "cat README.md") {
		t.Errorf("client-posted parameters overrode the authoritative ask: %q", model.lastUser())
	}
}

// TestHandleToolCallDigest_AuthoritativeAskWins: a live parent ask is read from
// the transcript in full, raw env assignments included, from request_id alone;
// a posted conflict never replaces it.
func TestHandleToolCallDigest_AuthoritativeAskWins(t *testing.T) {
	h := testConfigHandler(t)
	model := newDigestModelServer(t, "Removes the build directory.")
	plantDigestSession(t, h, "ses_auth", model)

	as := h.lookupAgentSession("ses_auth")
	as.messages = []agent.Message{
		{Role: "assistant", Content: "Cleaning up."},
		askMessage(t, "call_1", agent.PermissionRequest{
			ToolName: "bash",
			Command:  "rm -rf build",
			Args:     json.RawMessage(`{"command":"LD_PRELOAD=/tmp/evil.so rm -rf build"}`),
			Rule:     "bash.prefix.rm",
			Scope:    agent.PermissionScopeBashPrefix,
		}),
		askMessage(t, "call_2", agent.PermissionRequest{
			ToolName: "bash",
			Command:  "npm test",
			Args:     json.RawMessage(`{"command":"npm test"}`),
			Rule:     "bash.prefix.npm",
			Scope:    agent.PermissionScopeBashPrefix,
		}),
	}

	if w := postToolDigest(h, "ses_auth", `{"request_id":"call_1"}`); w.Code != http.StatusOK {
		t.Fatalf("lightweight request status = %d, body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(model.lastUser(), "LD_PRELOAD=/tmp/evil.so") {
		t.Errorf("full Args with the raw env assignment must reach the model: %q", model.lastUser())
	}

	if w := postToolDigest(h, "ses_auth", `{"request_id":"call_2","tool":"read","command":"cat README.md"}`); w.Code != http.StatusOK {
		t.Fatalf("conflicting request status = %d, body=%s", w.Code, w.Body.String())
	}
	if strings.Contains(model.lastUser(), "cat README.md") {
		t.Errorf("a posted conflict replaced the live ask: %q", model.lastUser())
	}
	if !strings.Contains(model.lastUser(), "npm test") {
		t.Errorf("the live ask for call_2 was not explained: %q", model.lastUser())
	}
}

// TestHandleToolCallDigest_AskUnavailable404: an unknown ask with no usable
// fallback is refused before any explanation starts. A tool name alone is not
// a usable fallback: it has no arguments to explain.
func TestHandleToolCallDigest_AskUnavailable404(t *testing.T) {
	h := testConfigHandler(t)
	model := newDigestModelServer(t, "unused")
	plantDigestSession(t, h, "ses_gone", model)

	for _, body := range []string{
		`{"request_id":"gone"}`,
		`{"request_id":"gone","tool":"read"}`,
	} {
		w := postToolDigest(h, "ses_gone", body)
		if w.Code != http.StatusNotFound {
			t.Fatalf("body %s: status = %d, want 404, body=%s", body, w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), `"code":"ask_unavailable"`) {
			t.Errorf("body %s: missing ask_unavailable code: %s", body, w.Body.String())
		}
	}
	if n := model.callCount(); n != 0 {
		t.Fatalf("model was called %d times for an unavailable ask, want 0", n)
	}
}

// TestHandleToolCallDigest_BodyOverLimit413: an oversize body is refused
// explicitly, never truncated into a partial explanation.
func TestHandleToolCallDigest_BodyOverLimit413(t *testing.T) {
	h := testConfigHandler(t)
	model := newDigestModelServer(t, "unused")
	plantDigestSession(t, h, "ses_big", model)

	big := `{"tool":"bash","command":"` + strings.Repeat("a", 1<<20) + `"}`
	w := postToolDigest(h, "ses_big", big)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413, body=%s", w.Code, w.Body.String())
	}
	if n := model.callCount(); n != 0 {
		t.Fatalf("model was called %d times for an oversize body, want 0", n)
	}
}

// TestHandleToolCallDigest_Superseded409: a second run with the same key takes
// the key from the first, which returns 409 explanation_superseded. A run for a
// different ask in the meantime stays 200.
func TestHandleToolCallDigest_Superseded409(t *testing.T) {
	h := testConfigHandler(t)
	model := newDigestModelServer(t, "Reads the file.")
	model.block("digest-gate")
	plantDigestSession(t, h, "ses_sup", model)

	first := postToolDigestAsync(h, "ses_sup",
		`{"request_id":"call_1","tool":"read","args":{"path":"digest-gate.txt"}}`)
	mustArrive(t, model.arrived)

	other := postToolDigest(h, "ses_sup", `{"request_id":"call_2","tool":"read","args":{"path":"go.mod"}}`)
	if other.Code != http.StatusOK {
		t.Fatalf("a different ask must not be superseded: status = %d, body=%s", other.Code, other.Body.String())
	}

	second := postToolDigest(h, "ses_sup", `{"request_id":"call_1","tool":"read","args":{"path":"go.mod"}}`)
	if second.Code != http.StatusOK {
		t.Fatalf("replacing run status = %d, body=%s", second.Code, second.Body.String())
	}

	old := awaitRecorder(t, first)
	if old.Code != http.StatusConflict {
		t.Fatalf("superseded run status = %d, want 409, body=%s", old.Code, old.Body.String())
	}
	if !strings.Contains(old.Body.String(), `"code":"explanation_superseded"`) {
		t.Errorf("superseded run must carry explanation_superseded: %s", old.Body.String())
	}
}

// TestHandleToolCallDigest_CrossTabSuperseded: another surface (a second tab, or
// the TUI) explaining the same ask replaces the HTTP run. The losing request
// gets the benign 409, and the other surface still gets its digest.
func TestHandleToolCallDigest_CrossTabSuperseded(t *testing.T) {
	h := testConfigHandler(t)
	model := newDigestModelServer(t, "Reads the file.")
	model.block("digest-gate")
	plantDigestSession(t, h, "ses_tab", model)

	losing := postToolDigestAsync(h, "ses_tab",
		`{"request_id":"call_1","tool":"read","args":{"path":"digest-gate.txt"}}`)
	mustArrive(t, model.arrived)

	as := h.lookupAgentSession("ses_tab")
	other, err := as.agent.SummarizeToolCallWithKeyCtx(context.Background(),
		agent.ToolCallDigestInput{Tool: "read", Args: json.RawMessage(`{"path":"go.mod"}`)}, "call_1")
	if err != nil {
		t.Fatalf("the other surface's run failed: %v", err)
	}
	if other.Summary == "" {
		t.Error("the other surface got an empty digest")
	}

	w := awaitRecorder(t, losing)
	if w.Code != http.StatusConflict {
		t.Fatalf("losing tab status = %d, want 409, body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"code":"explanation_superseded"`) {
		t.Errorf("losing tab must see explanation_superseded: %s", w.Body.String())
	}
}

// TestHandleToolCallDigest_CancelOnRequestAbort: when the client goes away the
// explanation stops, its slot is freed, and the transcript gets no row.
func TestHandleToolCallDigest_CancelOnRequestAbort(t *testing.T) {
	h := testConfigHandler(t)
	model := newDigestModelServer(t, "Reads the file.")
	model.block("digest-gate")
	plantDigestSession(t, h, "ses_abort", model)

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/ses_abort/tool-call-digest",
		strings.NewReader(`{"tool":"read","args":{"path":"digest-gate.txt"}}`)).WithContext(ctx)
	w := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		h.HandleToolCallDigest(w, req, "ses_abort")
		close(done)
	}()
	mustArrive(t, model.arrived)
	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("explanation kept running after the client aborted")
	}

	if as := h.lookupAgentSession("ses_abort"); len(as.messages) != 0 {
		t.Fatalf("an aborted explanation added transcript rows: %+v", as.messages)
	}
	if after := postToolDigest(h, "ses_abort", `{"tool":"read","args":{"path":"go.mod"}}`); after.Code != http.StatusOK {
		t.Fatalf("the aborted run must free its slot: status = %d, body=%s", after.Code, after.Body.String())
	}
}

// TestHandleToolCallDigest_InvalidatesOnAskResolve: resolving the ask drops the
// owner's in-flight explanation (409) and its cached explanations.
func TestHandleToolCallDigest_InvalidatesOnAskResolve(t *testing.T) {
	h := testConfigHandler(t)
	model := newDigestModelServer(t, "Echoes the marker.")
	model.block("digest-gate")
	plantDigestSession(t, h, "ses_inv", model)

	as := h.lookupAgentSession("ses_inv")
	as.messages = []agent.Message{
		{Role: "assistant", Content: "Echoing."},
		askMessage(t, "call_1", agent.PermissionRequest{
			ToolName: "bash",
			Command:  "echo digest-cache-marker",
			Args:     json.RawMessage(`{"command":"echo digest-cache-marker"}`),
			Rule:     "bash.prefix.echo",
			Scope:    agent.PermissionScopeBashPrefix,
		}),
	}

	// A completed explanation of the live ask is cached.
	if w := postToolDigest(h, "ses_inv", `{"request_id":"call_1"}`); w.Code != http.StatusOK {
		t.Fatalf("first explain status = %d, body=%s", w.Code, w.Body.String())
	}
	if w := postToolDigest(h, "ses_inv", `{"request_id":"call_1"}`); w.Code != http.StatusOK {
		t.Fatalf("cached explain status = %d, body=%s", w.Code, w.Body.String())
	}
	if n := model.requestsContaining("digest-cache-marker"); n != 1 {
		t.Fatalf("the second explain should be a cache hit: marker requests = %d, want 1", n)
	}

	// A different explanation is in flight when the ask resolves. Earlier
	// requests left arrival signals behind; drop them so the wait below means
	// THIS request reached the model.
	for drained := false; !drained; {
		select {
		case <-model.arrived:
		default:
			drained = true
		}
	}
	inflight := postToolDigestAsync(h, "ses_inv", `{"tool":"read","args":{"path":"digest-gate.txt"}}`)
	mustArrive(t, model.arrived)

	resolve := httptest.NewRequest(http.MethodPost, "/api/permissions/resolve",
		strings.NewReader(`{"request_id":"call_1","decision":"deny"}`))
	rw := httptest.NewRecorder()
	h.HandleResolvePermission(rw, resolve)
	if rw.Code < 200 || rw.Code >= 300 {
		t.Fatalf("resolve status = %d, body=%s", rw.Code, rw.Body.String())
	}

	if got := awaitRecorder(t, inflight); got.Code != http.StatusConflict ||
		!strings.Contains(got.Body.String(), `"code":"explanation_superseded"`) {
		t.Fatalf("in-flight explanation after resolve = %d %s, want 409 explanation_superseded", got.Code, got.Body.String())
	}

	// The ask is gone, so the same explanation is repeated from its fallback;
	// the cache was dropped, so the model is called again.
	if w := postToolDigest(h, "ses_inv", `{"request_id":"call_1","tool":"bash","command":"echo digest-cache-marker"}`); w.Code != http.StatusOK {
		t.Fatalf("post-resolve explain status = %d, body=%s", w.Code, w.Body.String())
	}
	if n := model.requestsContaining("digest-cache-marker"); n != 2 {
		t.Fatalf("resolve must drop the cached explanation: marker requests = %d, want 2", n)
	}
}

// TestHandleToolCallDigest_BusySessionNoConflict: a running chat turn holds
// as.mu. The explanation still runs and never answers 409 for the busy session.
func TestHandleToolCallDigest_BusySessionNoConflict(t *testing.T) {
	h := testConfigHandler(t)
	model := newDigestModelServer(t, "Lists files.")
	plantDigestSession(t, h, "ses_busy", model)

	as := h.lookupAgentSession("ses_busy")
	as.mu.Lock()
	defer as.mu.Unlock()

	w := postToolDigest(h, "ses_busy", `{"tool":"bash","command":"ls"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 while a turn holds the session, body=%s", w.Code, w.Body.String())
	}
}

func TestHandleToolCallDigestRejectsMissingParameters(t *testing.T) {
	h := testConfigHandler(t)
	plantDigestSession(t, h, "ses_missing", newDigestModelServer(t, "x"))

	w := postToolDigest(h, "ses_missing", `{}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "tool or a pending request_id") {
		t.Errorf("body should name the missing field: %s", w.Body.String())
	}
}

// TestHandleToolCallDigestRejectsBareToolNameWithoutRequestID: a bare tool name
// with no request id has no parameters to explain, so it is refused before the
// model is called.
func TestHandleToolCallDigestRejectsBareToolNameWithoutRequestID(t *testing.T) {
	h := testConfigHandler(t)
	model := newDigestModelServer(t, "unused")
	plantDigestSession(t, h, "ses_bare", model)

	w := postToolDigest(h, "ses_bare", `{"tool":"read"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", w.Code, w.Body.String())
	}
	if n := model.callCount(); n != 0 {
		t.Fatalf("model was called %d times for a bare tool name, want 0", n)
	}
}

func TestHandleToolCallDigestUnknownSession(t *testing.T) {
	h := testConfigHandler(t)
	w := postToolDigest(h, "ses_nope", `{"tool":"bash","command":"ls"}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404, body=%s", w.Code, w.Body.String())
	}
}

// TestHandleToolCallDigestAcceptsAStaleRequestID covers the click-after-resolve
// race: the ask is gone, so the posted parameters are used.
func TestHandleToolCallDigestAcceptsAStaleRequestID(t *testing.T) {
	h := testConfigHandler(t)
	model := newDigestModelServer(t, "Reads the file.")
	plantDigestSession(t, h, "ses_stale", model)

	w := postToolDigest(h, "ses_stale", `{"request_id":"gone","tool":"read","args":{"path":"README.md"}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(model.lastUser(), "README.md") {
		t.Errorf("posted args missing from the payload: %q", model.lastUser())
	}
}

// TestHandleToolCallDigestReportsModelFailure pins fail-fast: a provider error
// is an HTTP error with the reason, not a 200 carrying an `error` field.
func TestHandleToolCallDigestReportsModelFailure(t *testing.T) {
	h := testConfigHandler(t)
	model := newDigestModelServer(t, "unused")
	model.fail = true
	plantDigestSession(t, h, "ses_fail", model)

	w := postToolDigest(h, "ses_fail", `{"tool":"bash","command":"ls"}`)
	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502, body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "upstream exploded") {
		t.Errorf("error body should carry the provider reason: %s", w.Body.String())
	}
}

// TestHandleToolCallDigestDoesNotTouchTheTranscript is the isolation contract:
// an explanation is a side call and must leave the conversation untouched.
func TestHandleToolCallDigestDoesNotTouchTheTranscript(t *testing.T) {
	h := testConfigHandler(t)
	model := newDigestModelServer(t, "Lists files.")
	plantDigestSession(t, h, "ses_iso", model)

	h.mu.Lock()
	as := h.agents[agentKey("ses_iso")]
	h.mu.Unlock()
	as.messages = []agent.Message{{Role: "user", Content: "hello"}}

	if w := postToolDigest(h, "ses_iso", `{"tool":"bash","command":"ls"}`); w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	if len(as.messages) != 1 || as.messages[0].Content != "hello" {
		t.Fatalf("transcript mutated: %+v", as.messages)
	}
}
