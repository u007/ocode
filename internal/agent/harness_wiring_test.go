package agent

import (
	"os"
	"strings"
	"testing"
)

// TestSessionAffinityWiring pins the WIRING (not just the helper): every LLM
// chat transport that stamps the opencode session header must also stamp the
// harness-gated session-affinity headers, mirroring upstream
// (packages/opencode/src/session/llm/request.ts stamps the pair on every
// request; setOpencodeSessionHeader marks the call sites). This is a source
// assertion because the transports are large, live in client.go, and an
// integration test per transport would duplicate their retry/timeout logic.
// Mutation-verified: removing a call site from chatAnthropic fails this test.
func TestSessionAffinityWiring(t *testing.T) {
	src, err := os.ReadFile("client.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(src)

	// Each chat transport that calls setOpencodeSessionHeader is exactly the
	// set that must also carry affinity — either directly
	// (c.setSessionAffinityHeaders) or via the openrouter attribution helper
	// (c.setOpenRouterAttributionHeaders, which internally invokes the
	// affinity helper). Count both shapes in a window after each stamp.
	idx := 0
	sites := 0
	for {
		pos := strings.Index(text[idx:], "c.setOpencodeSessionHeader(req)")
		if pos < 0 {
			break
		}
		sites++
		window := text[idx+pos : min(idx+pos+400, len(text))]
		if !strings.Contains(window, "c.setSessionAffinityHeaders(req)") &&
			!strings.Contains(window, "c.setOpenRouterAttributionHeaders(req)") {
			t.Errorf("transport call site %d (setOpencodeSessionHeader at byte %d) does not stamp session-affinity headers within 400 bytes — upstream sends the pair on every request", sites, idx+pos)
		}
		idx += pos + 1
	}
	if sites < 5 {
		t.Fatalf("expected >=5 setOpencodeSessionHeader call sites, found %d — transport layout changed, update this test deliberately", sites)
	}
}
