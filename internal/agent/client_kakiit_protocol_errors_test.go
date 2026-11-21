package agent

import (
	"net/http"
	"strings"
	"testing"
)

// The gateway's 402/409 envelopes must be recognised on EVERY protocol the
// client can route a kakiit model to. The chat-completions transport labels its
// status errors with the provider, but the native transports used to label them
// with the PROTOCOL ("anthropic", "openai responses"), so the classifier never
// matched and the user saw raw JSON. These tests pin all three routes.

const kakiit402BodyProtocol = `{"error":{"message":"Insufficient credits for this request","type":"payment_required","code":"llm_insufficient_credits"}}`

const kakiit409BodyProtocol = `{"error":{"message":"this request id has already been used","type":"duplicate_request_id","code":"duplicate_request_id"}}`

// chatErrorForModel drives a real ChatWithContext turn for one model id with a
// stubbed transport, so protocol ROUTING is exercised (not the classifier
// alone). Returns the turn error.
func chatErrorForModel(t *testing.T, modelID string, code int, body string) error {
	t.Helper()
	seedKakiitProtocols(t)
	stubLLMHTTP(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return statusResponse(code, body), nil
	}))
	c := newKakiitTestClient("https://example.test/api/llm")
	c.Model = modelID
	_, err := c.ChatWithContext(t.Context(), []Message{{Role: "user", Content: "hi"}}, nil)
	return err
}

func TestKakiit402MapsOnAllThreeProtocols(t *testing.T) {
	cases := []struct{ name, model string }{
		{"chat-completions", "deepseek-v4-flash"},
		{"anthropic-messages", "claude-sonnet-4-6"},
		{"openai-responses", "gpt-5.6-luna"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := chatErrorForModel(t, tc.model, http.StatusPaymentRequired, kakiit402BodyProtocol)
			if err == nil {
				t.Fatal("expected the 402 to fail the turn")
			}
			if msg := kakiitUserFacingError(err); msg != KakiitTopUpMessage {
				t.Fatalf("%s 402 message = %q, want %q (raw err: %v)", tc.name, msg, KakiitTopUpMessage, err)
			}
		})
	}
}

func TestKakiit409MapsOnAllThreeProtocols(t *testing.T) {
	cases := []struct{ name, model string }{
		{"chat-completions", "deepseek-v4-flash"},
		{"anthropic-messages", "claude-sonnet-4-6"},
		{"openai-responses", "gpt-5.6-luna"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := chatErrorForModel(t, tc.model, http.StatusConflict, kakiit409BodyProtocol)
			if err == nil {
				t.Fatal("expected the 409 to fail the turn")
			}
			if msg := kakiitUserFacingError(err); msg != KakiitDuplicateRequestMessage {
				t.Fatalf("%s 409 message = %q, want %q (raw err: %v)", tc.name, msg, KakiitDuplicateRequestMessage, err)
			}
		})
	}
}

// TestNonKakiitProvidersKeepProtocolLabels guards the change: a non-kakiit
// client must still label its status errors by protocol, so nothing else starts
// claiming to be the gateway.
func TestNonKakiitProvidersKeepProtocolLabels(t *testing.T) {
	c := &GenericClient{Provider: "anthropic", Model: "m"}
	if got := c.statusProviderLabel("anthropic"); got != "anthropic" {
		t.Fatalf("non-kakiit label = %q, want anthropic", got)
	}
	if !strings.HasPrefix(KakiitDuplicateRequestMessage, "kakiit:") {
		t.Fatalf("the duplicate-id message must name the provider: %q", KakiitDuplicateRequestMessage)
	}
}
