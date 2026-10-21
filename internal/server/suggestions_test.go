package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/agent"
)

// TestHandleSetFeaturesConfigPersistsSuggestedResponses pins that the third
// toggle round-trips through the existing features endpoint.
func TestHandleSetFeaturesConfigPersistsSuggestedResponses(t *testing.T) {
	h := testConfigHandler(t)

	body := `{"memory_enabled":true,"doc_prompt_enabled":false,"suggested_responses_enabled":false}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/config/ocode/features", strings.NewReader(body))
	h.HandleSetFeaturesConfig(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	h.mu.Lock()
	got := h.cfg.Ocode.SuggestedResponsesEnabled
	h.mu.Unlock()
	if got {
		t.Error("in-memory cfg not updated: suggested responses should be disabled")
	}
}

// TestHandleGetFeaturesConfigReportsSuggestedResponses pins that the GET
// reports the toggle, so the Settings form can render its current state.
func TestHandleGetFeaturesConfigReportsSuggestedResponses(t *testing.T) {
	h := testConfigHandler(t)
	h.mu.Lock()
	h.cfg.Ocode.SuggestedResponsesEnabled = true
	h.mu.Unlock()

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/config/ocode/features", nil)
	h.HandleGetFeaturesConfig(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp struct {
		MemoryEnabled             bool `json:"memory_enabled"`
		DocPromptEnabled          bool `json:"doc_prompt_enabled"`
		SuggestedResponsesEnabled bool `json:"suggested_responses_enabled"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if !resp.SuggestedResponsesEnabled {
		t.Error("suggested_responses_enabled = false, want true")
	}
}

// TestSuggestionsEventIsSessionScoped pins that the new event type is
// registered as session-scoped.
//
// A session-scoped event published without a session id is a loud error in
// the bus (the ambiguity would be silent), and the web client's
// SESSION_SCOPED_EVENTS list mirrors this one — so a missing registration
// means the client never subscribes and the row never appears.
func TestSuggestionsEventIsSessionScoped(t *testing.T) {
	if !sessionScopedEvents["suggestions"] {
		t.Error(`sessionScopedEvents["suggestions"] = false, want true`)
	}
}

// TestWireHeadlessAgentCallbacksPublishesSuggestions pins that an assistant
// message carrying suggestions produces a "suggestions" bus event with the
// parsed options.
func TestWireHeadlessAgentCallbacksPublishesSuggestions(t *testing.T) {
	h := testConfigHandler(t)
	ag := &agent.Agent{}

	sub := h.bus.Subscribe(nil)
	defer h.bus.Unsubscribe(sub)

	h.wireHeadlessAgentCallbacks("ses_test_suggestions", ag)
	ag.OnMessage(agent.Message{
		Role:        "assistant",
		Content:     "All done.",
		Suggestions: []string{"Run the tests", "Open a PR"},
	})

	env, ok := awaitSuggestionsEvent(t, sub)
	if !ok {
		t.Fatal("no suggestions event published")
	}
	se, ok := env.Data.(SuggestionsEvent)
	if !ok {
		t.Fatalf("suggestions frame data type = %T, want SuggestionsEvent", env.Data)
	}
	if len(se.Suggestions) != 2 {
		t.Fatalf("suggestions = %v, want 2 entries", se.Suggestions)
	}
	if se.Suggestions[0] != "Run the tests" || se.Suggestions[1] != "Open a PR" {
		t.Errorf("suggestions = %v, want [Run the tests Open a PR]", se.Suggestions)
	}
}

// awaitSuggestionsEvent drains sub until a "suggestions" envelope arrives.
func awaitSuggestionsEvent(t *testing.T, sub chan Envelope) (Envelope, bool) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case env := <-sub:
			if env.Event == "suggestions" {
				return env, true
			}
		case <-deadline:
			return Envelope{}, false
		}
	}
}

// TestWireHeadlessAgentCallbacksOmitsEmptySuggestions pins that a reply with
// no options publishes nothing: an empty frame would CLEAR the row the client
// is already showing.
func TestWireHeadlessAgentCallbacksOmitsEmptySuggestions(t *testing.T) {
	h := testConfigHandler(t)
	ag := &agent.Agent{}

	sub := h.bus.Subscribe(nil)
	defer h.bus.Unsubscribe(sub)

	h.wireHeadlessAgentCallbacks("ses_test_no_suggestions", ag)
	ag.OnMessage(agent.Message{Role: "assistant", Content: "All done."})

	// Drain briefly: any "suggestions" frame is a failure. A short window is
	// enough because the publish is synchronous — the only reason a frame
	// could arrive later is a bug in the callback, which is what we assert.
	deadline := time.After(150 * time.Millisecond)
	for {
		select {
		case env := <-sub:
			if env.Event == "suggestions" {
				t.Fatalf("published a suggestions frame with no suggestions: %+v", env.Data)
			}
		case <-deadline:
			return
		}
	}
}
