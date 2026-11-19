package agent

import (
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

// kakiit402Body is the gateway's OpenAI-shaped 402 envelope per the spec
// (Error envelopes: OpenAI {error:{message,type,code}}; code
// llm_insufficient_credits ⇒ 402).
const kakiit402Body = `{"error":{"message":"Insufficient credits for this request","type":"payment_required","code":"llm_insufficient_credits"}}`

// TestKakiit402MapsToTopUpMessageVerbatim pins the user-visible mapping: a
// gateway 402 classified by status code + llm_insufficient_credits code
// surfaces the exact one-line top-up message.
func TestKakiit402MapsToTopUpMessageVerbatim(t *testing.T) {
	seedKakiitProtocols(t)

	err := chatErrorFromStub(t, http.StatusPaymentRequired, kakiit402Body)
	if err == nil {
		t.Fatal("expected the 402 to fail the turn")
	}
	msg := kakiitUserFacingError(err)
	if msg == "" {
		t.Fatalf("402 must map to a user-facing message, got none (err: %v)", err)
	}
	if msg != KakiitTopUpMessage {
		t.Fatalf("402 message = %q, want the verbatim top-up line %q", msg, KakiitTopUpMessage)
	}
}

// TestKakiit402IsNotRetried pins the no-auto-retry contract: a 402 fails the
// turn after exactly one attempt (the user decides to top up; retrying a
// payment failure can never succeed and would re-hold wallet funds).
func TestKakiit402IsNotRetried(t *testing.T) {
	seedKakiitProtocols(t)

	var calls int32
	stubLLMHTTP(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		atomic.AddInt32(&calls, 1)
		return statusResponse(http.StatusPaymentRequired, kakiit402Body), nil
	}))

	c := newKakiitTestClient("https://example.test/api/llm")
	c.Model = "deepseek-v4-flash"
	_, err := c.ChatWithContext(t.Context(), []Message{{Role: "user", Content: "hi"}}, nil)
	if err == nil {
		t.Fatal("expected failure")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("402 must fail the turn once, got %d attempts", got)
	}
	if !strings.Contains(err.Error(), "402") {
		t.Fatalf("error should carry the status: %v", err)
	}
}

// TestKakiit402ClassificationIsByStatusAndCodeNotProse pins that a NON-402
// response whose body merely mentions insufficient credits does NOT map to
// the top-up message (classification by status code + code, never body
// text), and a 402 lacking the llm_insufficient_credits code maps to the
// generic error path.
func TestKakiit402ClassificationIsByStatusAndCodeNotProse(t *testing.T) {
	seedKakiitProtocols(t)

	// 500 body mentioning credits → NOT the top-up mapping (and retried per
	// the standard 5xx policy, proving it took the generic path).
	err := chatErrorFromStub(t, http.StatusInternalServerError,
		`{"error":{"message":"upstream credits issue","type":"server_error","code":"llm_insufficient_credits"}}`)
	if err == nil {
		t.Fatal("expected failure")
	}
	if msg := kakiitUserFacingError(err); msg != "" {
		t.Fatalf("500 must not map to the top-up message even with the code in the body, got %q", msg)
	}

	// 402 with an unrelated code → classified 402 but not the credit mapping.
	err = chatErrorFromStub(t, http.StatusPaymentRequired,
		`{"error":{"message":"payment profile issues","type":"payment_required","code":"card_declined"}}`)
	if err == nil {
		t.Fatal("expected failure")
	}
	if msg := kakiitUserFacingError(err); msg == KakiitTopUpMessage {
		t.Fatalf("402 with a non-credit code must not map to the kakiit top-up message")
	}
}

// TestKakiitTopUpMessageSurfacesThroughErrorPath pins that the generic
// turn-failure surface (the one-liner the TUI chat and web banner render)
// carries the top-up message for a kakiit 402.
func TestKakiitTopUpMessageSurfacesThroughErrorPath(t *testing.T) {
	seedKakiitProtocols(t)

	err := chatErrorFromStub(t, http.StatusPaymentRequired, kakiit402Body)
	if err == nil {
		t.Fatal("expected failure")
	}
	detail := "turn failed: " + UserFacingLLMError(err)
	if !strings.Contains(detail, KakiitTopUpMessage) {
		t.Fatalf("the turn-failure one-liner must contain the top-up message, got: %s", detail)
	}
}

// chatErrorFromStub runs one kakiit chat request against a stub that answers
// the given status/body on every attempt and returns the resulting error.
func chatErrorFromStub(t *testing.T, code int, body string) error {
	t.Helper()
	stubLLMHTTP(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return statusResponse(code, body), nil
	}))
	c := newKakiitTestClient("https://example.test/api/llm")
	c.Model = "deepseek-v4-flash"
	_, err := c.ChatWithContext(t.Context(), []Message{{Role: "user", Content: "hi"}}, nil)
	return err
}
