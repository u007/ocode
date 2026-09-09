package agent

import (
	"net/http"
	"os"
	"strings"
	"sync/atomic"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/version"
)

// Harness identity ids. Canonical values match config.NormalizeFakeAgent.
const (
	HarnessOcode      = "ocode"
	HarnessOpencode   = "opencode"
	HarnessClaudeCode = "claude-code"
	HarnessCline      = "cline"
	HarnessKiloCode   = "kilo-code"
	HarnessCodex      = "codex"
)

// harnessPreset describes the outbound fingerprints one harness presents.
// Providers gate models/rate limits on these signals: User-Agent, OpenRouter
// attribution (HTTP-Referer / X-Title), Anthropic beta flags, and the
// X-App marker Claude Code sends.
type harnessPreset struct {
	UserAgent string
	Referer   string
	Title     string
	// AnthropicBeta, when non-empty, is merged into the anthropic-beta
	// header (comma-joined, deduped) on Messages API requests.
	AnthropicBeta string
	// ExtraHeaders are stamped verbatim (Set, overriding provider defaults).
	ExtraHeaders map[string]string
}

func harnessPresets() map[string]harnessPreset {
	ocodeUA := "ocode/" + version.Version
	return map[string]harnessPreset{
		HarnessOcode: {
			UserAgent: ocodeUA,
			Referer:   "https://github.com/u007/ocode",
			Title:     "ocode",
		},
		HarnessOpencode: {
			UserAgent: "opencode/1.0.0",
			Referer:   "https://opencode.ai",
			Title:     "opencode",
		},
		HarnessClaudeCode: {
			UserAgent:     "claude-cli/2.1.0 (external, cli)",
			Referer:       "https://claude.ai",
			Title:         "claude-code",
			AnthropicBeta: "claude-code-20250219",
			ExtraHeaders:  map[string]string{"X-App": "cli"},
		},
		HarnessCline: {
			UserAgent: "cline/3.28.0",
			Referer:   "https://github.com/cline/cline",
			Title:     "cline",
		},
		HarnessKiloCode: {
			UserAgent: "kilo-code/1.0.0",
			Referer:   "https://kilocode.ai",
			Title:     "kilo-code",
		},
		HarnessCodex: {
			UserAgent: "codex-cli/0.30.0",
			Referer:   "https://github.com/openai/codex",
			Title:     "codex",
		},
	}
}

// HarnessNames returns the canonical harness ids in stable order.
func HarnessNames() []string {
	return config.FakeAgentPresets()
}

// activeHarness holds the process-live harness override set by /fake-agent,
// the settings API, or startup sync from config. Empty means "resolve from
// config/env on next read". Stored as string in an atomic for lock-free
// per-request reads on the LLM hot path.
var activeHarness atomic.Value // string

// harnessConfigured records whether a startup/config path has initialized the
// process identity. Once initialized, per-request command/API switches must
// not be overwritten by construction of a helper client.
var harnessConfigured atomic.Bool

func init() {
	activeHarness.Store(HarnessOcode)
}

// ActiveHarness resolves the harness identity for the next outbound LLM
// request. Precedence: OCODE_FAKE_AGENT env (not persisted, highest) >
// process-live override from /fake-agent or the settings API > persisted
// ocodeconfig.json fake_agent > default (ocode).
func ActiveHarness() string {
	if v := strings.TrimSpace(os.Getenv("OCODE_FAKE_AGENT")); v != "" {
		if normalized, err := config.NormalizeFakeAgent(v); err == nil {
			return normalized
		}
	}
	if v, ok := activeHarness.Load().(string); ok && v != "" {
		if normalized, err := config.NormalizeFakeAgent(v); err == nil {
			return normalized
		}
		return HarnessOcode
	}
	return HarnessOcode
}

// SetActiveHarness switches the process-live harness identity (applied to
// the very next LLM request; no client rebuild needed because headers are
// resolved per-request). The value is normalized; unknown names return an
// error and leave the current identity untouched.
func SetActiveHarness(raw string) (string, error) {
	normalized, err := config.NormalizeFakeAgent(raw)
	if err != nil {
		return "", err
	}
	activeHarness.Store(normalized)
	harnessConfigured.Store(true)
	return normalized, nil
}

// SyncHarnessFromConfig seeds the process-live identity from persisted
// config (called once at session/server startup so later per-request reads
// stay lock-free and file-IO-free). Empty/invalid config falls back to
// the ocode default.
func SyncHarnessFromConfig(fakeAgent string) string {
	normalized, err := config.NormalizeFakeAgent(fakeAgent)
	if err != nil || normalized == "" {
		normalized = HarnessOcode
	}
	activeHarness.Store(normalized)
	harnessConfigured.Store(true)
	return normalized
}

// syncHarnessFromConfigIfNeeded initializes the identity lazily for headless
// callers that construct a client without a TUI/server bootstrap.
func syncHarnessFromConfigIfNeeded(fakeAgent string) {
	if harnessConfigured.Load() || !harnessConfigured.CompareAndSwap(false, true) {
		return
	}
	normalized, err := config.NormalizeFakeAgent(fakeAgent)
	if err != nil || normalized == "" {
		normalized = HarnessOcode
	}
	activeHarness.Store(normalized)
}

// HarnessPresetFor returns the fingerprint preset for a harness id,
// falling back to the ocode default for unknown ids.
func HarnessPresetFor(harness string) harnessPreset {
	presets := harnessPresets()
	if p, ok := presets[harness]; ok {
		return p
	}
	return presets[HarnessOcode]
}

// applyHarnessHeaders stamps the active harness identity onto an outbound
// LLM request. It runs AFTER provider-specific headers so the harness
// impersonation wins for shared keys (User-Agent, Referer, Title), except
// Copilot which must keep its vscode-chat fingerprint to authenticate —
// there only the generic ocode UA is left untouched and attribution headers
// are skipped.
func applyHarnessHeaders(req *http.Request, provider string) {
	if req == nil {
		return
	}
	harness := ActiveHarness()
	preset := HarnessPresetFor(harness)
	if provider == "copilot" {
		// Copilot's backend keys auth on the vscode-chat UA family; keep it.
		return
	}
	if preset.UserAgent != "" {
		req.Header.Set("User-Agent", preset.UserAgent)
	}
	// OpenRouter-style attribution: honored by OpenRouter and harmless
	// elsewhere. Upstream opencode-compatible zen proxies also read these
	// for app grouping.
	if preset.Referer != "" {
		req.Header.Set("HTTP-Referer", preset.Referer)
		req.Header.Set("Referer", preset.Referer)
	}
	if preset.Title != "" {
		req.Header.Set("X-Title", preset.Title)
	}
	for k, v := range preset.ExtraHeaders {
		req.Header.Set(k, v)
	}
	if preset.AnthropicBeta != "" {
		mergeAnthropicBeta(req, preset.AnthropicBeta)
	}
}

// applyHarnessPayload stamps harness-identifying body fields. Anthropic
// accepts an opt-in metadata.user_id; OpenAI chat/completions and Responses
// accept a free-form metadata map. Only set when the caller has not already
// provided the key so explicit plugin values win.
func applyHarnessPayload(payload map[string]interface{}, provider string) {
	if payload == nil {
		return
	}
	harness := ActiveHarness()
	if harness == "" {
		harness = HarnessOcode
	}
	switch {
	case provider == "anthropic" || strings.HasPrefix(provider, "opencode"):
		if _, ok := payload["metadata"]; !ok {
			payload["metadata"] = map[string]interface{}{"user_id": "harness/" + harness}
		}
	case provider == "openai" || provider == "openrouter" || provider == "opencode-go" || provider == "opencode":
		if _, ok := payload["metadata"]; !ok {
			payload["metadata"] = map[string]interface{}{"harness": harness}
		}
	}
}

// mergeAnthropicBeta comma-merges a beta flag into anthropic-beta, deduped.
func mergeAnthropicBeta(req *http.Request, flag string) {
	existing := req.Header.Get("anthropic-beta")
	if existing == "" {
		req.Header.Set("anthropic-beta", flag)
		return
	}
	for _, part := range strings.Split(existing, ",") {
		if strings.TrimSpace(part) == flag {
			return
		}
	}
	req.Header.Set("anthropic-beta", existing+","+flag)
}
