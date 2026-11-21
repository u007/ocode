package agent

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/u007/ocode/internal/auth"
	"github.com/u007/ocode/internal/crashguard"
	"github.com/u007/ocode/internal/pricing"
)

// kakiitCacheTTL bounds how long the live model list is reused before the
// gateway is asked again. Same value as the models.dev/OpenRouter caches:
// prices change server-side (the gateway owns them), so ocode re-reads
// periodically but never blocks a turn on it.
const kakiitCacheTTL = 5 * time.Minute

// kakiitModelInfo is one parsed /v1/models entry. Vendor extensions
// (x_kakiit_protocol, x_kakiit_free) drive the per-model codec choice and the
// picker's free badge; context_length and pricing feed the context gauge and
// usage ledger exactly like models.dev data does for other providers.
type kakiitModelInfo struct {
	ID            string        `json:"id"`
	ContextLength int64         `json:"context_length"`
	Protocol      string        `json:"x_kakiit_protocol"`
	Free          bool          `json:"x_kakiit_free"`
	Pricing       kakiitPricing `json:"pricing"`
}

// kakiitPricing mirrors the gateway's per-model pricing block (USD per 1M
// tokens as a JSON string, the OpenRouter/models.dev convention).
type kakiitPricing struct {
	Prompt     string `json:"prompt"`
	Completion string `json:"completion"`
}

// pricing converts the string prices to the float shape ocode consumes.
func (p kakiitPricing) modelPricing(free bool) pricing.ModelPricing {
	if free {
		return pricing.ModelPricing{}
	}
	in, _ := parseKakiitPrice(p.Prompt)
	out, _ := parseKakiitPrice(p.Completion)
	return pricing.ModelPricing{InputPerMillion: in, OutputPerMillion: out}
}

// parseKakiitPrice parses the per-million string into a float; 0 on malformed.
func parseKakiitPrice(s string) (float64, error) {
	return strconv.ParseFloat(strings.TrimSpace(s), 64)
}

// kakiitLiveData is the in-process live-model cache: the single source for
// the picker list, the per-model protocol routing (Task 3) and pricing
// lookups. Fetch failures leave it untouched; auth failures are recorded in
// authErr so callers can tell "unreachable" from "key rejected".
var kakiitLiveData struct {
	mu        sync.RWMutex
	models    []kakiitModelInfo
	lastFetch time.Time
	authErr   error
}

// kakiitModelsAuthError returns the recorded auth failure from the last
// fetch (invalid/revoked key), or nil. Non-auth fetch failures are NOT
// recorded here — an unreachable gateway reads as "no models", a rejected key
// must read as "your key is bad".
func KakiitModelsAuthError() error {
	kakiitLiveData.mu.RLock()
	defer kakiitLiveData.mu.RUnlock()
	return kakiitLiveData.authErr
}

// kakiitCacheFresh reports whether the cache is populated and within TTL, so
// main-loop callers can consult it without a network call.
func kakiitCacheFresh() bool {
	kakiitLiveData.mu.RLock()
	defer kakiitLiveData.mu.RUnlock()
	return kakiitLiveData.models != nil && time.Since(kakiitLiveData.lastFetch) < kakiitCacheTTL
}

// kakiitLiveModelsCopyLocked snapshots the cache entries; caller holds mu.
func kakiitLiveModelsCopyLocked() []kakiitModelInfo {
	if kakiitLiveData.models == nil {
		return nil
	}
	return append([]kakiitModelInfo(nil), kakiitLiveData.models...)
}

// kakiitModelEntry returns the cached entry for a bare model id.
func kakiitModelEntry(model string) (kakiitModelInfo, bool) {
	kakiitLiveData.mu.RLock()
	defer kakiitLiveData.mu.RUnlock()
	for _, m := range kakiitLiveData.models {
		if m.ID == model {
			return m, true
		}
	}
	return kakiitModelInfo{}, false
}

// kakiitModelProtocol returns the declared upstream protocol for a bare
// kakiit model id. False when the model is unknown to the cache (fetch
// failed, never fetched, or id typo) — callers must treat that as a hard
// routing error, never a default (Task 3).
func kakiitModelProtocol(model string) (string, bool) {
	entry, ok := kakiitModelEntry(model)
	if !ok {
		return "", false
	}
	return entry.Protocol, entry.Protocol != ""
}

// kakiitModelFree reports the x_kakiit_free flag for a bare model id.
func kakiitModelFree(model string) bool {
	entry, ok := kakiitModelEntry(model)
	return ok && entry.Free
}

// KakiitModelCost exposes the gateway-declared pricing for a full
// "kakiit/<model>" id through the same pricing.ModelPricing shape the picker
// and usage ledger consume. Free models report zero prices.
func KakiitModelCost(modelID string) (pricing.ModelPricing, bool) {
	provider, model, ok := splitModelID(modelID)
	if !ok || provider != kakiitProvider {
		return pricing.ModelPricing{}, false
	}
	entry, found := kakiitModelEntry(model)
	if !found {
		return pricing.ModelPricing{}, false
	}
	return entry.Pricing.modelPricing(entry.Free), true
}

// KakiitModelWindow returns the gateway-declared context window for a full
// "kakiit/<model>" id, 0 when unknown.
func KakiitModelWindow(modelID string) int64 {
	provider, model, ok := splitModelID(modelID)
	if !ok || provider != kakiitProvider {
		return 0
	}
	entry, found := kakiitModelEntry(model)
	if !found {
		return 0
	}
	return entry.ContextLength
}

// kakiitModelsURL returns the live models endpoint for the effective base URL.
func kakiitModelsURL() string {
	base := KakiitBaseURL()
	// A per-credential override in auth.json wins over env/config defaults
	// (the standard override order NewClientWithProfile applies).
	if cred, ok := auth.Get(kakiitProvider); ok && cred.BaseURL != "" {
		base = strings.TrimRight(cred.BaseURL, "/")
	}
	return strings.TrimRight(base, "/") + "/v1/models"
}

// fetchKakiitLiveModels fetches the gateway's /v1/models and returns the bare
// model IDs, populating the cache used by the picker, protocol routing and
// pricing lookups. Returns nil when:
//   - no kakiit credential is configured (nothing to list),
//   - the fetch fails / body is malformed (empty list + debug line; callers
//     degrade to an empty picker, never a models.dev fallback — kakiit is
//     absent from models.dev by design),
//   - the gateway answers 401/403 (recorded in KakiitModelsAuthError so the
//     failure is NOT a silent empty list).
func fetchKakiitLiveModels() []string {
	kakiitLiveData.mu.RLock()
	if kakiitLiveData.models != nil && time.Since(kakiitLiveData.lastFetch) < kakiitCacheTTL {
		ids := make([]string, 0, len(kakiitLiveData.models))
		for _, m := range kakiitLiveData.models {
			ids = append(ids, m.ID)
		}
		kakiitLiveData.mu.RUnlock()
		return ids
	}
	kakiitLiveData.mu.RUnlock()

	key := auth.ResolveKey(kakiitProvider)
	if key == "" {
		emitDebug("AGENT", "kakiit models: no credential configured; skipping live fetch")
		return nil
	}

	modelsURL := kakiitModelsURL()
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest(http.MethodGet, modelsURL, nil)
	if err != nil {
		emitDebug("AGENT", fmt.Sprintf("kakiit models: build request: %v", err))
		return nil
	}
	req.Header.Set("Authorization", "Bearer "+key)

	resp, err := client.Do(req)
	if err != nil {
		emitDebug("AGENT", fmt.Sprintf("kakiit models: fetch failed (gateway unreachable?): %v", err))
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		err := fmt.Errorf("kakiit models: key rejected (HTTP %d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
		kakiitLiveData.mu.Lock()
		kakiitLiveData.authErr = err
		kakiitLiveData.mu.Unlock()
		emitDebug("AGENT", err.Error())
		return nil
	}
	if resp.StatusCode != http.StatusOK {
		emitDebug("AGENT", fmt.Sprintf("kakiit models: unexpected HTTP %d from %s", resp.StatusCode, modelsURL))
		return nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if err != nil {
		emitDebug("AGENT", fmt.Sprintf("kakiit models: read body: %v", err))
		return nil
	}
	var apiResp struct {
		Data []kakiitModelInfo `json:"data"`
	}
	if err := json.Unmarshal(body, &apiResp); err != nil {
		emitDebug("AGENT", fmt.Sprintf("kakiit models: malformed body: %v", err))
		return nil
	}

	entries := make([]kakiitModelInfo, 0, len(apiResp.Data))
	ids := make([]string, 0, len(apiResp.Data))
	seen := make(map[string]bool, len(apiResp.Data))
	for _, m := range apiResp.Data {
		if m.ID == "" || seen[m.ID] {
			continue
		}
		seen[m.ID] = true
		entries = append(entries, m)
		ids = append(ids, m.ID)
	}
	if len(entries) == 0 {
		emitDebug("AGENT", "kakiit models: gateway returned no visible models")
		return nil
	}
	sort.Strings(ids)

	kakiitLiveData.mu.Lock()
	kakiitLiveData.models = entries
	kakiitLiveData.lastFetch = time.Now()
	kakiitLiveData.authErr = nil
	kakiitLiveData.mu.Unlock()

	return ids
}

// kakiitProviderModels returns the kakiit/-prefixed picker list from the live
// cache, fetching when refresh allows. Never falls back to models.dev: the
// gateway owns the catalog (prices change server-side).
func kakiitProviderModels(refresh bool) []string {
	if !refresh && !kakiitCacheFresh() {
		return nil
	}
	ids := fetchKakiitLiveModels()
	if len(ids) == 0 {
		return nil
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, kakiitProvider+"/"+id)
	}
	return out
}

// PreloadKakiitModels fetches the live list in the background so kakiit
// models appear in the picker as soon as a credential exists. No-op without a
// key; degrades silently when the gateway is unreachable.
func PreloadKakiitModels() {
	crashguard.Go(func() { fetchKakiitLiveModels() })
}

// KakiitModelsLoaded reports whether the live list has been fetched at least
// once and is still fresh.
func KakiitModelsLoaded() bool { return kakiitCacheFresh() }

// The three upstream protocols the gateway declares per model
// (llm_model.upstream_protocol). The values are wire literals from
// x_kakiit_protocol — never rename.
const (
	kakiitProtocolAnthropic = "anthropic-messages"
	kakiitProtocolResponses = "openai-responses"
	kakiitProtocolChat      = "openai-chat"
)

// kakiitProtocolForModel resolves the transport protocol for a kakiit model
// and REFUSES to guess: a model absent from the cache (fetch never ran,
// failed, or an id typo) or carrying no/unknown x_kakiit_protocol is a hard
// error. Silently defaulting would reproduce the opencode-go routing bug —
// an Anthropic-protocol model posted to /chat/completions 500s
// deterministically, and a Responses model there streams broken tool calls.
func kakiitProtocolForModel(model string) (string, error) {
	protocol, ok := kakiitModelProtocol(model)
	if !ok {
		return "", fmt.Errorf("kakiit model %s reports no protocol; refusing to guess (run a model from /v1/models, or re-fetch after a gateway outage)", model)
	}
	switch protocol {
	case kakiitProtocolAnthropic, kakiitProtocolResponses, kakiitProtocolChat:
		return protocol, nil
	default:
		return "", fmt.Errorf("kakiit model %s reports unknown protocol %q; refusing to guess", model, protocol)
	}
}

// kakiitProtocolGuard is the pre-flight check the chat transports run before
// sending: it returns the refusing-to-guess error for a kakiit model with no
// usable protocol, nil when the model's protocol is known.
func kakiitProtocolGuard(model string) error {
	if _, err := kakiitProtocolForModel(model); err != nil {
		return err
	}
	return nil
}
