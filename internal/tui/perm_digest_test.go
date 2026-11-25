package tui

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

	tea "charm.land/bubbletea/v2"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/config"
)

// permDigestStubClient is a real OpenAI-compatible endpoint for the explainer.
// The explainer clones the session's *agent.GenericClient and sends over HTTP,
// so a Go stub client cannot stand in for the model: a non-GenericClient main
// model has no side client and the explainer refuses it. No request leaves the
// machine.
type permDigestStubClient struct {
	reply string
	fail  bool // answer 400 instead of streaming the reply

	srv     *httptest.Server
	arrived chan struct{} // one signal per request, buffered
	release chan struct{}
	once    sync.Once

	mu    sync.Mutex
	gate  string // a request whose user text contains gate is held
	calls int
	user  string // user-role text of the most recent request
}

// newPermDigestStub starts the endpoint for one test. The release channel is
// closed before the server, so held requests finish before Close waits on them.
func newPermDigestStub(t *testing.T, reply string) *permDigestStubClient {
	t.Helper()
	c := &permDigestStubClient{
		reply:   reply,
		arrived: make(chan struct{}, 32),
		release: make(chan struct{}),
	}
	c.srv = httptest.NewServer(http.HandlerFunc(c.serve))
	t.Cleanup(c.srv.Close)
	t.Cleanup(c.unblock)
	return c
}

func (c *permDigestStubClient) unblock() {
	c.once.Do(func() { close(c.release) })
}

// block holds every request whose user text contains substr until unblock or
// the request's own context ends.
func (c *permDigestStubClient) block(substr string) {
	c.mu.Lock()
	c.gate = substr
	c.mu.Unlock()
}

// client is the main model the session agent runs on.
func (c *permDigestStubClient) client() *agent.GenericClient {
	return &agent.GenericClient{
		Provider: "testprov",
		Model:    "test-model",
		BaseURL:  c.srv.URL,
		APIKey:   "test-key",
	}
}

func (c *permDigestStubClient) serve(w http.ResponseWriter, r *http.Request) {
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

	c.mu.Lock()
	c.calls++
	c.user = text
	gate := c.gate
	c.mu.Unlock()

	select {
	case c.arrived <- struct{}{}:
	default:
	}
	if gate != "" && strings.Contains(text, gate) {
		select {
		case <-c.release:
		case <-r.Context().Done():
			return
		}
	}
	if c.fail {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"message":"upstream exploded"}}`)
		return
	}
	chunk, err := json.Marshal(map[string]any{"choices": []any{map[string]any{
		"delta":         map[string]any{"role": "assistant", "content": c.reply},
		"finish_reason": "stop",
	}}})
	if err != nil {
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", chunk)
}

// callCount is how many requests reached the endpoint.
func (c *permDigestStubClient) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

// lastUser is the user-role text of the most recent request.
func (c *permDigestStubClient) lastUser() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.user
}

// newPermDigestTestModel lays out a chat view with one pending bash ask whose
// session agent runs on the stub (small model disabled, so the digest falls back
// to the session client exactly like production's no-small-model arm). The
// owner is unique per test, and cleanup drops its cached explanations.
func newPermDigestTestModel(t *testing.T, stub *permDigestStubClient) model {
	t.Helper()
	m := newTestModel()
	m.agent = newTestAgent(stub.client(), nil, &config.Config{}, nil)
	owner := "ses_" + strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	m.agent.SetOpenCodeSessionID(owner)
	t.Cleanup(func() { agent.InvalidateToolDigests(owner) })
	m.ready = true
	upd, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 45})
	m = upd.(model)
	return deliverAsks(t, m, permAskToolMsg(t, "call_d", "bash", "rm -rf build"))
}

// bashAskWithArgs is a bash ask whose full Args carry more than its Command, so
// a test can tell whether Args survived the trip to the explainer.
func bashAskWithArgs(command string, args json.RawMessage) agent.PermissionRequest {
	return agent.PermissionRequest{
		ToolName: "bash",
		Command:  command,
		Args:     args,
		Scope:    agent.PermissionScopeBashPrefix,
		Rule:     "bash.prefix.rm",
	}
}

// runDigestCmd executes the tea.Cmd synchronously and folds its message back
// through Update, which is exactly what Bubble Tea does with it.
func runDigestCmd(t *testing.T, m model, cmd tea.Cmd) model {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected an explanation command, got nil")
	}
	msg, ok := cmd().(permDigestMsg)
	if !ok {
		t.Fatalf("command produced %T, want permDigestMsg", cmd())
	}
	upd, _ := m.Update(msg)
	return upd.(model)
}

// TestPermDialogExplainShowsTheModelsDigest is the happy path: `e` starts the
// side call WITHOUT answering the ask, and the reply renders inside the body.
func TestPermDialogExplainShowsTheModelsDigest(t *testing.T) {
	stub := newPermDigestStub(t, "Deletes the build output directory.")
	m := newPermDigestTestModel(t, stub)

	upd, cmd := m.handleChatKeys(tea.KeyPressMsg{Code: 'e', Text: "e"}, nil, nil)
	m = upd.(model)

	if !m.showPermDialog {
		t.Fatal("explain must not close the ask dialog")
	}
	if m.pendingToolCallID != "call_d" {
		t.Fatalf("pendingToolCallID = %q, want call_d (explain must not answer the ask)", m.pendingToolCallID)
	}
	if !m.permDigestBusy {
		t.Fatal("permDigestBusy = false, want true while the model runs")
	}
	m = runDigestCmd(t, m, cmd)

	if m.permDigest != "Deletes the build output directory." {
		t.Fatalf("permDigest = %q", m.permDigest)
	}
	if m.permDigestModel != "testprov/test-model" {
		t.Errorf("permDigestModel = %q, want the provider-qualified id", m.permDigestModel)
	}
	if stub.callCount() != 1 {
		t.Errorf("model calls = %d, want 1", stub.callCount())
	}
	if !strings.Contains(stub.lastUser(), "rm -rf build") {
		t.Errorf("the pending command did not reach the model: %q", stub.lastUser())
	}

	body := stripANSI(m.permissionBodyText(100))
	if !strings.Contains(body, "Deletes the build output directory.") {
		t.Errorf("digest missing from the dialog body:\n%s", body)
	}
	if !strings.Contains(body, "AI summary") || !strings.Contains(body, "not a safety verdict") {
		t.Errorf("digest must be labelled as an AI summary, not a verdict:\n%s", body)
	}
}

// TestPermDialogExplainShowsALoadingState proves the user gets immediate
// feedback between the keypress and the model's reply.
func TestPermDialogExplainShowsALoadingState(t *testing.T) {
	m := newPermDigestTestModel(t, newPermDigestStub(t, "x"))
	upd, cmd := m.handleChatKeys(tea.KeyPressMsg{Code: 'e', Text: "e"}, nil, nil)
	m = upd.(model)
	if cmd == nil {
		t.Fatal("expected a command")
	}
	if body := stripANSI(m.permissionBodyText(100)); !strings.Contains(body, "Asking the small model…") {
		t.Errorf("loading state missing from the body:\n%s", body)
	}
	_ = cmd // do not run it: the assertion is about the in-flight state
}

// TestSummarizeCancellationIsNotLabelledATimeout: closing the dialog (or
// navigating away) cancels the request, and the message must say so rather than
// claiming a 25 s deadline elapsed. The blocking client keeps the select parked
// on ctx.Done(), so the branch is deterministic.
func TestSummarizeCancellationIsNotLabelledATimeout(t *testing.T) {
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	a := newTestAgent(blockingPermDigestClient{release: block}, nil, &config.Config{}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := a.SummarizeToolCallCtx(ctx, agent.ToolCallDigestInput{Tool: "bash", Command: "ls"})
	if err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("err = %v, want a cancellation (not a timeout)", err)
	}
}

type blockingPermDigestClient struct{ release chan struct{} }

func (c blockingPermDigestClient) GetProvider() string { return "testprov" }
func (c blockingPermDigestClient) GetModel() string    { return "blocking" }
func (c blockingPermDigestClient) Chat([]agent.Message, []map[string]interface{}) (*agent.Message, error) {
	<-c.release
	return &agent.Message{Role: "assistant", Content: "late"}, nil
}

// TestPermDialogExplainShowsFailures covers the no-model/provider-error path:
// the dialog states why instead of silently doing nothing.
func TestPermDialogExplainShowsFailures(t *testing.T) {
	stub := newPermDigestStub(t, "unused")
	stub.fail = true
	m := newPermDigestTestModel(t, stub)

	upd, cmd := m.handleChatKeys(tea.KeyPressMsg{Code: 'e', Text: "e"}, nil, nil)
	m = upd.(model)
	m = runDigestCmd(t, m, cmd)

	if !strings.Contains(m.permDigestErr, "upstream exploded") {
		t.Fatalf("permDigestErr = %q", m.permDigestErr)
	}
	if body := stripANSI(m.permissionBodyText(100)); !strings.Contains(body, "Could not explain this call") {
		t.Errorf("failure not surfaced in the body:\n%s", body)
	}
	if !m.showPermDialog {
		t.Error("a failed explanation must leave the ask answerable")
	}
}

// TestPermDialogExplainDropsAStaleReply: a reply for an ask that is no longer
// current must never be shown under the new command.
func TestPermDialogExplainDropsAStaleReply(t *testing.T) {
	m := newPermDigestTestModel(t, newPermDigestStub(t, "old answer"))
	upd, cmd := m.handleChatKeys(tea.KeyPressMsg{Code: 'e', Text: "e"}, nil, nil)
	m = upd.(model)

	// The next ask is PROMOTED (the previous one was answered) before the old
	// reply returns. A merely queued ask does not change the displayed command,
	// so it must not clear the digest — promotion is the reset point.
	m.showPermAsk(queuedPermAsk{
		req:        agent.PermissionRequest{ToolName: "bash", Command: "ls", Scope: agent.PermissionScopeTool},
		toolCallID: "call_new",
	})
	msg := cmd().(permDigestMsg)
	upd, _ = m.Update(msg)
	m = upd.(model)

	if m.permDigest != "" {
		t.Fatalf("stale explanation leaked into the new ask: %q", m.permDigest)
	}
}

// TestPermDialogExplainResetsWhenTheAskChanges pins the reset on promotion.
func TestPermDialogExplainResetsWhenTheAskChanges(t *testing.T) {
	m := newPermDigestTestModel(t, newPermDigestStub(t, "list files"))
	upd, cmd := m.handleChatKeys(tea.KeyPressMsg{Code: 'e', Text: "e"}, nil, nil)
	m = upd.(model)
	m = runDigestCmd(t, m, cmd)
	if m.permDigest == "" {
		t.Fatal("setup: expected a digest")
	}

	m.showPermAsk(queuedPermAsk{
		req:        agent.PermissionRequest{ToolName: "bash", Command: "ls -la", Scope: agent.PermissionScopeTool},
		toolCallID: "call_next",
	})
	if m.permDigest != "" || m.permDigestModel != "" {
		t.Fatalf("digest survived a new ask: %q / %q", m.permDigest, m.permDigestModel)
	}
}

// TestPermDialogExplainIsUnavailableForContentAsks: a content ask is about a
// result that already ran, so there is no call to explain.
func TestPermDialogExplainIsUnavailableForContentAsks(t *testing.T) {
	m := newPermDigestTestModel(t, newPermDigestStub(t, "x"))
	m.pendingPermission.Scope = agent.PermissionScopeContent

	if m.permDigestActionAvailable() {
		t.Fatal("content ask must not offer the explain action")
	}
	if body := stripANSI(m.permissionBodyText(100)); strings.Contains(body, "Asking the small model") {
		t.Errorf("content ask rendered a digest section:\n%s", body)
	}
}

// TestPermDialogExplainHintAndClickRegion pins the affordance: the hint row
// names it, and its clickable region sits one line under the button row.
func TestPermDialogExplainHintAndClickRegion(t *testing.T) {
	m := newPermDigestTestModel(t, newPermDigestStub(t, "x"))

	if hint := stripANSI(m.renderPermHintRow()); !strings.Contains(hint, "[e] explain") {
		t.Fatalf("hint row does not offer the action: %q", hint)
	}

	m.updatePermButtonRegions()
	var explain, allow *permButtonRegion
	for i := range m.permButtonRegions {
		switch m.permButtonRegions[i].choice {
		case "e":
			explain = &m.permButtonRegions[i]
		case "y":
			allow = &m.permButtonRegions[i]
		}
	}
	if explain == nil {
		t.Fatal("no click region registered for the explain chip")
	}
	if allow == nil {
		t.Fatal("setup: no allow button region")
	}
	if explain.y1 != allow.y1+3 {
		t.Errorf("explain region y1 = %d, want %d (one line under the 3-line button row)", explain.y1, allow.y1+3)
	}
	if explain.x1 != allow.x1 {
		t.Errorf("explain region x1 = %d, want %d (same x origin as the buttons)", explain.x1, allow.x1)
	}
}

// TestPermDialogExplainCommandDoesNotBlockUpdate proves the model call is
// deferred into the command (the Update call returns before the model answers).
func TestPermDialogExplainCommandDoesNotBlockUpdate(t *testing.T) {
	stub := newPermDigestStub(t, "x")
	m := newPermDigestTestModel(t, stub)
	upd, cmd := m.handleChatKeys(tea.KeyPressMsg{Code: 'e', Text: "e"}, nil, nil)
	if cmd == nil {
		t.Fatal("expected a command")
	}
	if n := stub.callCount(); n != 0 {
		t.Fatalf("model called during Update (%d calls); the call must live in the tea.Cmd", n)
	}
	_ = upd
}

// TestPermDigest_FullStructAndRequestIDHandoff: the explainer receives the
// complete Args (not a rebuild from Command), and the pending tool-call id keys
// the run. The same command under a new call id is explained again.
func TestPermDigest_FullStructAndRequestIDHandoff(t *testing.T) {
	stub := newPermDigestStub(t, "Removes the build directory.")
	m := newPermDigestTestModel(t, stub)
	args := json.RawMessage(`{"command":"LD_PRELOAD=/tmp/evil.so rm -rf build"}`)
	m.showPermAsk(queuedPermAsk{req: bashAskWithArgs("rm -rf build", args), toolCallID: "call_f"})

	upd, cmd := m.handleChatKeys(tea.KeyPressMsg{Code: 'e', Text: "e"}, nil, nil)
	m = runDigestCmd(t, upd.(model), cmd)
	if !strings.Contains(stub.lastUser(), "LD_PRELOAD=/tmp/evil.so") {
		t.Fatalf("full Args did not reach the explainer (Args was rebuilt from Command?): %q", stub.lastUser())
	}

	// A new ask with the same command and a new call id is a new run: the call
	// id is part of the key, so the model is asked again.
	m.showPermAsk(queuedPermAsk{req: bashAskWithArgs("rm -rf build", args), toolCallID: "call_g"})
	upd, cmd = m.handleChatKeys(tea.KeyPressMsg{Code: 'e', Text: "e"}, nil, nil)
	m = runDigestCmd(t, upd.(model), cmd)
	if n := stub.callCount(); n != 2 {
		t.Fatalf("model calls = %d, want 2: a new call id must key a new run", n)
	}
	if m.permDigest != "Removes the build directory." {
		t.Errorf("permDigest = %q", m.permDigest)
	}
}

// TestPermDigest_MarkdownAndSanitize: model output renders through the Markdown
// renderer, and model-supplied control sequences never reach the frame.
func TestPermDigest_MarkdownAndSanitize(t *testing.T) {
	reply := "## Effect\n\n" +
		"- deletes \x1b[31mbuild\x1b[0m output\n" +
		"- clears the screen \x1b[2J\x1b[H now\n" +
		"- title \x1b]0;evil\x07 injected\n" +
		"- bidi \u202eesrever\u202c text\n\n" +
		"```sh\nrm -rf build\n```\n"
	stub := newPermDigestStub(t, reply)
	m := newPermDigestTestModel(t, stub)
	upd, cmd := m.handleChatKeys(tea.KeyPressMsg{Code: 'e', Text: "e"}, nil, nil)
	m = runDigestCmd(t, upd.(model), cmd)

	body := m.permissionBodyText(100)
	for _, bad := range []string{"\x1b]", "\x1b[2J", "\x1b[H", "\u202e", "\u202c"} {
		if strings.Contains(body, bad) {
			t.Errorf("model control sequence %q reached the dialog body", bad)
		}
	}
	plain := stripANSI(body)
	for _, want := range []string{"Effect", "deletes build output", "rm -rf build"} {
		if !strings.Contains(plain, want) {
			t.Errorf("rendered body missing %q:\n%s", want, plain)
		}
	}
	if strings.Contains(plain, "## Effect") {
		t.Errorf("heading markup was not rendered:\n%s", plain)
	}
}

// TestPermDigest_ScrollsFullOutput: a long explanation is never truncated. The
// viewport stays within the dialog's height budget and scrolls to its end.
func TestPermDigest_ScrollsFullOutput(t *testing.T) {
	long := strings.Repeat("A sentence describing one more effect of the command.\n\n", 40) + "FINAL-LINE-MARKER"
	m := newPermDigestTestModel(t, newPermDigestStub(t, long))
	upd, cmd := m.handleChatKeys(tea.KeyPressMsg{Code: 'e', Text: "e"}, nil, nil)
	m = runDigestCmd(t, upd.(model), cmd)

	if !strings.Contains(stripANSI(m.permissionBodyText(60)), "FINAL-LINE-MARKER") {
		t.Fatal("the full explanation must be present in the body; nothing may be truncated")
	}
	if h := m.permViewport.Height(); h > m.permDialogMaxBodyLines() {
		t.Fatalf("viewport height %d exceeds the dialog budget %d", h, m.permDialogMaxBodyLines())
	}
	if m.permViewport.TotalLineCount() <= m.permViewport.Height() {
		t.Fatal("setup: the explanation should overflow the viewport")
	}
	m.permViewport.GotoBottom()
	if !strings.Contains(stripANSI(m.permViewport.View()), "FINAL-LINE-MARKER") {
		t.Errorf("scrolling to the end did not reveal the last line:\n%s", stripANSI(m.permViewport.View()))
	}
}

// TestPermDigest_CancelsOnCloseAskChangeReset: closing the dialog, changing the
// ask and resetting each cancel the in-flight explanation, and the cancel is
// spent (the slot is cleared) so no second cancel runs.
func TestPermDigest_CancelsOnCloseAskChangeReset(t *testing.T) {
	cases := map[string]func(m *model){
		"close": func(m *model) { m.clearPendingPermAskSlot() },
		"ask change": func(m *model) {
			m.showPermAsk(queuedPermAsk{req: agent.PermissionRequest{ToolName: "bash", Command: "ls", Scope: agent.PermissionScopeTool}, toolCallID: "call_next"})
		},
		"reset": func(m *model) { m.resetPermDigest() },
	}
	for name, act := range cases {
		t.Run(name, func(t *testing.T) {
			stub := newPermDigestStub(t, "never delivered")
			stub.block("rm -rf build")
			m := newPermDigestTestModel(t, stub)
			upd, cmd := m.handleChatKeys(tea.KeyPressMsg{Code: 'e', Text: "e"}, nil, nil)
			m = upd.(model)
			if m.permDigestCancel == nil {
				t.Fatal("an explanation run must store its cancel func")
			}

			done := make(chan permDigestMsg, 1)
			go func() { done <- cmd().(permDigestMsg) }()
			mustArriveDigest(t, stub.arrived)

			act(&m)
			if m.permDigestCancel != nil {
				t.Fatal("the cancel func must be cleared once it has been called")
			}
			select {
			case msg := <-done:
				if msg.err == nil || !strings.Contains(msg.err.Error(), "cancel") {
					t.Fatalf("cancelled run returned %v, want a cancellation", msg.err)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("the explanation kept running after the dialog moved on")
			}

			act(&m) // a second reset must not panic or cancel anything again
		})
	}
}

// TestPermDigest_StaleReplyDropped: a reply from an earlier generation is
// dropped after the ask changed, even if it carries a summary.
func TestPermDigest_StaleReplyDropped(t *testing.T) {
	m := newPermDigestTestModel(t, newPermDigestStub(t, "fresh"))
	upd, cmd := m.handleChatKeys(tea.KeyPressMsg{Code: 'e', Text: "e"}, nil, nil)
	m = upd.(model)
	stale := cmd().(permDigestMsg)

	m.showPermAsk(queuedPermAsk{
		req:        agent.PermissionRequest{ToolName: "bash", Command: "ls", Scope: agent.PermissionScopeTool},
		toolCallID: "call_new",
	})
	upd, _ = m.Update(stale)
	m = upd.(model)

	if m.permDigest != "" || m.permDigestBusy {
		t.Fatalf("stale reply applied: digest=%q busy=%v", m.permDigest, m.permDigestBusy)
	}
	if strings.Contains(stripANSI(m.permissionBodyText(100)), "fresh") {
		t.Error("stale explanation rendered under the new ask")
	}
}

// mustArriveDigest waits for the stub endpoint to receive a request.
func mustArriveDigest(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(10 * time.Second):
		t.Fatal("the explainer never reached the model")
	}
}
