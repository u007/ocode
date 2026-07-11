package codex

import (
	"strings"
	"testing"

	providerplugin "github.com/u007/ocode/internal/plugin/provider"
)

func TestRequestHeadersCodexIdentityForGPT56(t *testing.T) {
	p := &CodexProvider{}
	for _, model := range []string{"gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna", "openai/gpt-5.6-luna"} {
		h := p.RequestHeaders(providerplugin.RequestContext{Model: model})
		if got := h.Get("originator"); got != "codex_cli_rs" {
			t.Errorf("originator for %q = %q, want codex_cli_rs", model, got)
		}
		if ua := h.Get("User-Agent"); !strings.HasPrefix(ua, "codex_cli_rs/") {
			t.Errorf("User-Agent for %q = %q, want codex_cli_rs/ prefix", model, ua)
		}
	}
}

func TestRequestHeadersOpencodeIdentityForOlderModels(t *testing.T) {
	p := &CodexProvider{}
	for _, model := range []string{"gpt-5.5", "gpt-5.4", "gpt-5.4-mini", "gpt-5.3-codex-spark"} {
		h := p.RequestHeaders(providerplugin.RequestContext{Model: model})
		if got := h.Get("originator"); got != "opencode" {
			t.Errorf("originator for %q = %q, want opencode", model, got)
		}
	}
}
