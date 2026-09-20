package agent

import (
	"net/http"
	"testing"
)

// TestRekeySessionChangesProviderConversationID is the load-bearing regression
// for /reset-id: after a rekey the outbound X-Opencode-Session header (and the
// OpenRouter x-session-id) must carry the NEW id, not the old one. This is the
// entire point of the feature — the provider groups cache/rate-limit/sticky
// routing by this value.
func TestRekeySessionChangesProviderConversationID(t *testing.T) {
	oldID := "ses_2026-01-02-030405-aaaaaaaa"
	newID := "ses_2026-01-02-030406-bbbbbbbb"

	for _, provider := range []string{"opencode", "opencode-go"} {
		client := &GenericClient{Provider: provider, Model: "gpt-test", BaseURL: "https://example.test/v1"}
		a := NewAgent(client, nil, nil, nil)
		a.SetSessionID(oldID)
		if got := a.OpenCodeSessionID(); got != oldID {
			t.Fatalf("%s: pre-rekey OpenCodeSessionID = %q, want %q", provider, got, oldID)
		}
		a.RekeySession(oldID, newID)
		if got := a.OpenCodeSessionID(); got != newID {
			t.Fatalf("%s: post-rekey OpenCodeSessionID = %q, want %q", provider, got, newID)
		}
		if got := a.sessionIDValue(); got != newID {
			t.Fatalf("%s: post-rekey debug session id = %q, want %q", provider, got, newID)
		}
		if got := client.opencodeSessionID(); got != newID {
			t.Fatalf("%s: client header id = %q, want %q", provider, got, newID)
		}
	}
}

// TestRekeyOpenCodeSessionLeavesDebugTagEmpty pins the TUI variant: the
// provider identity moves, but the debug-log session stays process-global
// (empty) exactly as SetOpenCodeSessionID leaves it.
func TestRekeyOpenCodeSessionLeavesDebugTagEmpty(t *testing.T) {
	client := &GenericClient{Provider: "opencode", Model: "gpt-test", BaseURL: "https://example.test/v1"}
	a := NewAgent(client, nil, nil, nil)
	a.SetOpenCodeSessionID("ses_old")
	a.RekeyOpenCodeSession("ses_old", "ses_new")
	if got := a.OpenCodeSessionID(); got != "ses_new" {
		t.Fatalf("OpenCodeSessionID = %q, want ses_new", got)
	}
	if got := a.sessionIDValue(); got != "" {
		t.Fatalf("debug session id = %q, want empty (TUI stays process-global)", got)
	}
}

// TestRekeySessionOutboundHeaderChanges drives a real (stubbed) HTTP request
// before and after the rekey and asserts the wire header value changes. This
// covers the transport wiring, not just the accessor.
func TestRekeySessionOutboundHeaderChanges(t *testing.T) {
	var seen []string
	stubLLMHTTP(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		seen = append(seen, req.Header.Get(opencodeSessionHeader))
		return statusResponse(http.StatusOK, openAIChatOKStream), nil
	}))

	client := &GenericClient{Provider: "opencode", Model: "gpt-test", BaseURL: "https://example.test/v1"}
	a := NewAgent(client, nil, nil, nil)
	msgs := []Message{{Role: "user", Content: "hi"}}

	a.SetSessionID("ses_before")
	if _, err := client.Chat(msgs, nil); err != nil {
		t.Fatalf("chat before rekey: %v", err)
	}
	a.RekeySession("ses_before", "ses_after")
	if _, err := client.Chat(msgs, nil); err != nil {
		t.Fatalf("chat after rekey: %v", err)
	}

	if len(seen) != 2 {
		t.Fatalf("captured %d requests, want 2", len(seen))
	}
	if seen[0] != "ses_before" {
		t.Fatalf("first header = %q, want ses_before", seen[0])
	}
	if seen[1] != "ses_after" {
		t.Fatalf("second header = %q, want ses_after (rekey must change the wire header)", seen[1])
	}
}
