package agent

import (
	"net/http"
	"testing"
)

// OpenRouter sticky routing keys on x-session-id (docs: guides/best-practices/
// prompt-caching, "Provider Sticky Routing"). Every OpenRouter request must
// carry the conversation's stable session id so turn N routes to the same
// provider as turn N-1 and prompt caching engages from the first turn.
func TestOpenRouterHeaders_SessionIDStickyRouting(t *testing.T) {
	c := &GenericClient{Provider: "openrouter"}
	c.setOpencodeSessionID("ses_test-sticky")

	req, _ := http.NewRequest(http.MethodPost, "https://openrouter.ai/api/v1/chat/completions", nil)
	c.setOpenRouterAttributionHeaders(req)

	if got := req.Header.Get("x-session-id"); got != "ses_test-sticky" {
		t.Fatalf("x-session-id = %q, want %q", got, "ses_test-sticky")
	}
	if req.Header.Get("HTTP-Referer") == "" || req.Header.Get("X-Title") == "" {
		t.Fatal("attribution headers missing")
	}

	// Stable across requests in one conversation.
	req2, _ := http.NewRequest(http.MethodPost, "https://openrouter.ai/api/v1/chat/completions", nil)
	c.setOpenRouterAttributionHeaders(req2)
	if req2.Header.Get("x-session-id") != req.Header.Get("x-session-id") {
		t.Fatal("x-session-id must be stable across turns")
	}
}

func TestOpenRouterHeaders_NoopForOtherProviders(t *testing.T) {
	c := &GenericClient{Provider: "anthropic"}
	req, _ := http.NewRequest(http.MethodPost, "https://api.anthropic.com/v1/messages", nil)
	c.setOpenRouterAttributionHeaders(req)
	if req.Header.Get("x-session-id") != "" {
		t.Fatal("x-session-id must not be set for non-openrouter providers")
	}
}
