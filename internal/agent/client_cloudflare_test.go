package agent

import "testing"

func TestStripMaxTokensForCloudflareGatewayOSeries(t *testing.T) {
	payload := map[string]interface{}{
		"model":       "o1",
		"max_tokens":  4096,
		"temperature": 0.7,
	}
	maybeStripMaxTokensForGateway("cloudflare-gateway", "o1", payload)
	if _, ok := payload["max_tokens"]; ok {
		t.Error("max_tokens should be stripped for o-series on cloudflare-gateway")
	}
	if _, ok := payload["temperature"]; !ok {
		t.Error("temperature should be preserved")
	}
}

func TestStripMaxTokensPreservesNonOSeries(t *testing.T) {
	payload := map[string]interface{}{
		"model":      "@cf/meta/llama-3",
		"max_tokens": 4096,
	}
	maybeStripMaxTokensForGateway("cloudflare-gateway", "@cf/meta/llama-3", payload)
	if _, ok := payload["max_tokens"]; !ok {
		t.Error("max_tokens should be preserved for non-o-series")
	}
}

func TestStripMaxTokensPreservesNonGateway(t *testing.T) {
	payload := map[string]interface{}{
		"model":      "o1",
		"max_tokens": 4096,
	}
	maybeStripMaxTokensForGateway("openai", "o1", payload)
	if _, ok := payload["max_tokens"]; !ok {
		t.Error("max_tokens should be untouched for non-gateway providers")
	}
}
