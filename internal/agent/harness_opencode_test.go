package agent

import (
	"net/http"
	"os"
	"strings"
	"testing"
)

// TestApplyHarnessHeadersOpencodeFingerprint pins the opencode harness's
// outbound fingerprint against the real opencode wire contract
// (packages/opencode/src/installation/index.ts userAgent() +
// provider/provider.ts attribution + anthropic beta pair). Mutation-verified:
// reverting the HarnessOpencode preset fields fails every assertion below.
func TestApplyHarnessHeadersOpencodeFingerprint(t *testing.T) {
	old := ActiveHarness()
	t.Cleanup(func() { _, _ = SetActiveHarness(old); _ = os.Unsetenv("OCODE_FAKE_AGENT") })
	if _, err := SetActiveHarness("opencode"); err != nil {
		t.Fatal(err)
	}

	req, err := http.NewRequest("POST", "https://example.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	applyHarnessHeaders(req, "anthropic")

	// UA: opencode/<channel>/<version>/cli — real opencode release shape.
	ua := req.Header.Get("User-Agent")
	if !strings.HasPrefix(ua, "opencode/") || !strings.HasSuffix(ua, "/cli") {
		t.Errorf("User-Agent = %q, want opencode/<channel>/<version>/cli", ua)
	}
	parts := strings.Split(ua, "/")
	if len(parts) != 4 {
		t.Errorf("User-Agent = %q, want 4 segments", ua)
	} else if parts[1] != "latest" {
		t.Errorf("User-Agent channel = %q, want latest", parts[1])
	}
	if got := req.Header.Get("HTTP-Referer"); got != "https://opencode.ai/" {
		t.Errorf("HTTP-Referer = %q, want https://opencode.ai/", got)
	}
	if got := req.Header.Get("X-Title"); got != "opencode" {
		t.Errorf("X-Title = %q, want opencode", got)
	}
	if got := req.Header.Get("X-Source"); got != "opencode" {
		t.Errorf("X-Source = %q, want opencode (llmgateway attribution)", got)
	}
	if got := req.Header.Get("X-BILLING-INVOKE-ORIGIN"); got != "OpenCode" {
		t.Errorf("X-BILLING-INVOKE-ORIGIN = %q, want OpenCode (nvidia attribution)", got)
	}
	// Anthropic: the interleaved-thinking + fine-grained-tool-streaming pair
	// real opencode sends; must MERGE with ocode's own flag, not clobber it.
	beta := req.Header.Get("anthropic-beta")
	if !strings.Contains(beta, "interleaved-thinking-2025-05-14") {
		t.Errorf("anthropic-beta missing interleaved-thinking: %q", beta)
	}
	if !strings.Contains(beta, "fine-grained-tool-streaming-2025-05-14") {
		t.Errorf("anthropic-beta missing fine-grained-tool-streaming: %q", beta)
	}
}

// TestApplyHarnessPayloadOpencodeSendsNoMetadata pins the no-fabricated-
// metadata contract: real opencode carries conversation identity in headers
// (x-opencode-* zen / x-session-affinity + X-Session-Id elsewhere) and sends
// no metadata.user_id / metadata.harness on any provider. Mutation-verified:
// removing the HarnessOpencode early-return fails all three checks.
func TestApplyHarnessPayloadOpencodeSendsNoMetadata(t *testing.T) {
	old := ActiveHarness()
	t.Cleanup(func() { _, _ = SetActiveHarness(old); _ = os.Unsetenv("OCODE_FAKE_AGENT") })
	if _, err := SetActiveHarness("opencode"); err != nil {
		t.Fatal(err)
	}
	for _, provider := range []string{"anthropic", "openai", "openrouter", "opencode", "opencode-go"} {
		payload := map[string]interface{}{}
		applyHarnessPayload(payload, provider)
		if _, ok := payload["metadata"]; ok {
			t.Errorf("provider %s: opencode harness must not stamp metadata", provider)
		}
	}
	// Other harnesses keep their metadata stamps (existing contract).
	if _, err := SetActiveHarness("ocode"); err != nil {
		t.Fatal(err)
	}
	payload := map[string]interface{}{}
	applyHarnessPayload(payload, "anthropic")
	if _, ok := payload["metadata"]; !ok {
		t.Error("ocode harness should still stamp metadata.user_id")
	}
}

// TestSetSessionAffinityHeaders pins the opencode-harness session headers:
// non-zen providers get x-session-affinity + X-Session-Id (upstream
// session/llm/request.ts), zen (opencode*) providers get neither (they carry
// X-Opencode-Session), and the ocode harness sends neither. The id must be
// the stable per-conversation id, not a per-request value.
func TestSetSessionAffinityHeaders(t *testing.T) {
	old := ActiveHarness()
	t.Cleanup(func() { _, _ = SetActiveHarness(old); _ = os.Unsetenv("OCODE_FAKE_AGENT") })

	c := &GenericClient{Provider: "anthropic", Model: "claude-opus-4-7"}
	c.setSessionID("ses_test_123")

	must := func(req *http.Request) *http.Request {
		t.Helper()
		r, err := http.NewRequest("POST", "https://example.com", nil)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}

	if _, err := SetActiveHarness("opencode"); err != nil {
		t.Fatal(err)
	}
	// Non-zen provider: both headers, stable id.
	req := must(nil)
	c.setSessionAffinityHeaders(req)
	if got := req.Header.Get("x-session-affinity"); got != "ses_test_123" {
		t.Errorf("x-session-affinity = %q, want ses_test_123", got)
	}
	if got := req.Header.Get("X-Session-Id"); got != "ses_test_123" {
		t.Errorf("X-Session-Id = %q, want ses_test_123", got)
	}
	// Same id on a second request — conversation-stable, not per-request.
	req2 := must(nil)
	c.setSessionAffinityHeaders(req2)
	if req2.Header.Get("x-session-affinity") != req.Header.Get("x-session-affinity") {
		t.Error("affinity id changed between requests; must be conversation-stable")
	}
	// Zen provider: no affinity (upstream relies on x-opencode-session there).
	zen := &GenericClient{Provider: "opencode", Model: "claude-opus-4-7"}
	zen.setSessionID("ses_zen_1")
	zenReq := must(nil)
	zen.setSessionAffinityHeaders(zenReq)
	if zenReq.Header.Get("x-session-affinity") != "" || zenReq.Header.Get("X-Session-Id") != "" {
		t.Error("zen provider must not carry x-session-affinity/X-Session-Id")
	}

	// ocode harness: headers stay off — this is opencode's wire contract.
	if _, err := SetActiveHarness("ocode"); err != nil {
		t.Fatal(err)
	}
	off := must(nil)
	c.setSessionAffinityHeaders(off)
	if off.Header.Get("x-session-affinity") != "" || off.Header.Get("X-Session-Id") != "" {
		t.Error("ocode harness must not stamp session-affinity headers")
	}
}
