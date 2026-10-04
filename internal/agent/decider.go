package agent

import (
	"context"
	"fmt"
	"path"
	"strings"
)

// judgeSlot names one decision-judge call site. Each slot resolves to its own
// configured model, so a user can point the permission judge and the network
// guard at different backends.
type judgeSlot string

const (
	slotPermission   judgeSlot = "permission"
	slotAutoContinue judgeSlot = "auto_continue"
	slotDiscovery    judgeSlot = "discovery"
	slotDocSearch    judgeSlot = "doc_search"
	slotCodeSearch   judgeSlot = "code_search"
	slotNetworkGuard judgeSlot = "network_guard"
	slotContentGuard judgeSlot = "content_guard"
)

// defaultJudgeModel is what every slot resolves to when nothing is configured.
// It is deliberately identical to the three hardcoded judge constants
// (discoveryJudgeModel, networkGuardJudgeModel, contentGuardJudgeModel), so
// replacing them with slot lookups changes no behaviour.
const defaultJudgeModel = "typesafe/jev-latest"

// Decider is a decision backend: it answers typed questions (noul / choice /
// score) and never generates text.
//
// It is deliberately NARROWER than LLMClient, and that narrowness is the point:
// a decision-only backend must never be reachable from the chat, compaction,
// small-model, recap or task-contract paths. Declaring only the four decision
// methods makes that structural rather than a convention someone can forget —
// there is no Chat method to call by mistake. *TypesafeClient already declares
// all four, so widening a judge from *TypesafeClient to Decider is a signature
// change and not a rewrite.
type Decider interface {
	DecideCtx(ctx context.Context, state any, questions map[string]TypesafeQuestion) (*TypesafeResponse, error)
	Decide(state any, questions map[string]TypesafeQuestion) (*TypesafeResponse, error)
	GetProvider() string
	GetModel() string
}

// slotModel returns the model id configured for a slot.
//
// permission and auto_continue are deliberately NOT read here: those two already
// have their own long-standing keys (permissions.auto.model and
// auto_continue_model) and keep them, so existing configs and the settings UI
// keep working untouched. The other five read their own judge_model key.
//
// Every path falls back to defaultJudgeModel. A blank result would be a bug, not
// a "disabled" signal: resolveDecider turns a blank model into a nil client, and a
// nil client silently disables a judge the user never touched.
func (a *Agent) slotModel(slot judgeSlot) string {
	if a == nil || a.config == nil {
		return defaultJudgeModel
	}
	switch slot {
	case slotPermission:
		// autoPermissionModelName already handles its own override chain and
		// returns "unavailable" when nothing is configured.
		if m := a.autoPermissionModelName(); m != "" && m != "unavailable" {
			return m
		}
		return defaultJudgeModel
	case slotAutoContinue:
		if m := strings.TrimSpace(a.config.Ocode.AutoContinueModel); m != "" {
			return m
		}
		return defaultJudgeModel
	}

	oc := a.config.Ocode
	var v string
	switch slot {
	case slotDiscovery:
		v = oc.Discovery.JudgeModel
	case slotDocSearch:
		v = oc.DocSearch.JudgeModel
	case slotCodeSearch:
		// The code-search judge reads the `search` key. Slot name and key name
		// differ deliberately: `search` is also the tool name, and renaming the
		// slot to match would read as though the tool itself were being configured.
		v = oc.Search.JudgeModel
	case slotNetworkGuard:
		v = oc.NetworkGuard.JudgeModel
	case slotContentGuard:
		v = oc.ContentGuard.JudgeModel
	}
	if v = strings.TrimSpace(v); v != "" {
		return v
	}
	return defaultJudgeModel
}

// resolveDecider builds the decision client for a slot, or nil when the slot's
// model is unset or its provider has no usable credential.
//
// nil is the "judge disabled" signal, NOT an error. Every caller already handles
// a nil client by keeping every candidate (relevance judges) or by asking the
// human (permission), so "no judge configured" stays a normal state instead of a
// failure path. Note this deliberately does not fall back to any other backend:
// if a slot names one model, a failure of that model must be visible rather than
// silently answered by a different one.
//
// The client is built OUTSIDE any lock. Callers that cache the result are
// responsible for invalidating when the slot's model changes — see
// discoveryJudgeClient.
func (a *Agent) resolveDecider(slot judgeSlot) Decider {
	if a == nil || a.config == nil {
		return nil
	}
	client := newClientFn(a.config, a.slotModel(slot))
	if client == nil {
		return nil
	}
	// A keyless client would 401 on first use. Refuse to hand it back so callers
	// treat the judge as disabled instead of surfacing a deferred 401.
	//
	// Clef additionally needs an account id: its URL is account-scoped, so an API
	// key alone produces a request to a path with an empty account. Returning nil
	// here would make a clef slot look like "judge disabled" rather than
	// "half-configured", so the reason is logged instead.
	switch d := client.(type) {
	case *TypesafeClient:
		if d.APIKey == "" {
			return nil
		}
	case *ClefClient:
		if d.APIKey == "" {
			return nil
		}
		if d.AccountID == "" {
			emitDebug("AGENT", fmt.Sprintf("resolveDecider: slot=%s model=%s has a CLOUDFLARE_API_KEY but no account id; run /connect cloudflare-workers to add it. Judge is disabled.", slot, d.Model))
			return nil
		}
	}
	dec, ok := client.(Decider)
	if !ok {
		return nil
	}
	return dec
}

// deciderLabel returns the fully-qualified "provider/model" id for a decision
// backend, as the client reports it.
//
// It exists because the judges used to read the concrete TypesafeClient's Model
// field and hardcode a "typesafe/" prefix, which books every decision backend's
// tokens to TypeSafe in the usage ledger the moment a second one exists.
// GetProvider is authoritative over the configured id: the configured string may
// be an alias (for example "typesafe/jev-latest") while the client reports the
// version it actually resolved.
func deciderLabel(d Decider) string {
	if d == nil {
		return ""
	}
	provider, model := d.GetProvider(), d.GetModel()
	if provider == "" {
		return model
	}
	if model == "" {
		return provider
	}
	return provider + "/" + model
}

// clefBodySelectors maps each Workers AI decision model's URL id to the short
// selector its request body expects.
//
// The two identifiers are NOT the same and Cloudflare's schema treats them
// differently: the URL segment is "@cf/cloudflare/clef-flash" while the body
// field's pattern accepts only "clef" or "clef-flash". Sending the URL form in
// the body is rejected.
var clefBodySelectors = map[string]string{
	"@cf/cloudflare/clef":       "clef",
	"@cf/cloudflare/clef-flash": "clef-flash",
}

// clefBodySelector returns the body-level selector for a clef model id.
//
// It takes the LAST path segment, not the text after the first slash. The Cloudflare
// model id contains two slashes, so a first-slash split yields
// "cloudflare/clef-flash" — wrong for the body, and wrong for isDecisionModel too,
// which would mean clef is never routed at all and the backend silently does not
// exist. path.Base handles both the one-segment and two-segment spellings.
func clefBodySelector(model string) string {
	return path.Base(strings.TrimSpace(model))
}

// isCloudflareDecisionModel reports whether a model id names a Cloudflare
// Workers AI decision model.
//
// The provider prefix is load-bearing and is checked explicitly. The
// cloudflare-workers provider serves both chat models and clef, so the model
// name alone does not identify a decision model — and a bare "clef" from some
// other provider must not be hijacked. Accepts both the full
// "<provider>/<model>" form and a bare model id, since the latter is what the
// selector is keyed on.
func isCloudflareDecisionModel(modelID string) bool {
	provider, rest, found := strings.Cut(modelID, "/")
	if !found {
		return false
	}
	if provider != cloudflareWorkersProvider {
		return false
	}
	// Look up the part AFTER the provider prefix. The map is keyed on the
	// Workers AI model id ("@cf/cloudflare/clef-flash"), not on the full
	// "provider/model" string this function receives — looking up modelID here
	// never matches anything.
	_, ok := clefBodySelectors[rest]
	return ok
}

// cloudflareWorkersProvider is the provider id under which the Workers AI
// credential is stored. It serves chat models as well as clef, so it is not
// itself a decision provider.
const cloudflareWorkersProvider = "cloudflare-workers"

// isDecisionModel reports whether a provider/model id routes to a decision
// backend rather than a chat model. This is the single place that decides, so a
// new decision provider is taught here once.
func isDecisionModel(modelID string) bool {
	if strings.HasPrefix(modelID, "typesafe/") {
		return true
	}
	return isCloudflareDecisionModel(modelID)
}
