package auth

import (
	"strings"

	providerplugin "github.com/u007/ocode/internal/plugin/provider"
)

// This file is the SINGLE source of truth for "how can this provider be
// connected?". Both the TUI /connect dialog (internal/tui/connect.go) and the
// web/desktop Connectors endpoints (internal/server/handler_connect.go) render
// from it, so the two surfaces cannot drift — they used to carry verbatim
// copies of this logic, including the Grok special case below.

// MethodKind classifies what choosing a Method does. The UI needs it to pick a
// control (text input vs "open this URL" vs a confirm), so it is derived ONCE
// here rather than re-inferred per caller.
type MethodKind string

const (
	MethodAPIKey MethodKind = "apikey" // prompt for a key, write to the base store
	MethodOAuth  MethodKind = "oauth"  // run an interactive sign-in flow
	MethodPlugin MethodKind = "plugin" // run a provider plugin's AuthMethod.Run
	MethodRemove MethodKind = "remove" // delete the stored credential
)

// Method is one selectable way to connect a provider.
type Method struct {
	ID    string
	Label string
	Kind  MethodKind
}

// MethodsFor returns the ways provider p can be connected, in display order.
//
// The shape mirrors the TUI's /connect method list and is deliberately
// unchanged by this extraction:
//
//   - API-key entry is always offered, first.
//   - If the provider ships a plugin, its AuthMethods replace the built-in
//     OAuth flows (the plugin owns the flow).
//   - Otherwise the built-in flow for p.OAuthFlow is offered, if any.
//   - Removal is offered only when a credential is actually stored.
//
// Callers that need an extra affordance add it themselves: the TUI appends its
// own "cancel" entry, which is dialog chrome rather than a way to connect, and
// the server omits it entirely.
func MethodsFor(p *Provider) []Method {
	out := []Method{{ID: "apikey", Label: "API Key", Kind: MethodAPIKey}}
	if plugin, ok := providerplugin.Get(p.ID); ok {
		for _, am := range plugin.AuthMethods() {
			if am.Run == nil {
				continue
			}
			id := "plugin_" + am.Label
			// Grok's x.com subscription needs cookies collected by the
			// UI, so it gets a dedicated method id handled outside the
			// generic plugin dispatch.
			if p.ID == "grok" && strings.Contains(am.Label, "Subscription") {
				id = "grok_subscription"
			}
			out = append(out, Method{ID: id, Label: am.Label, Kind: MethodPlugin})
		}
	} else {
		switch p.OAuthFlow {
		case "anthropic":
			out = append(out,
				Method{ID: "oauth_max", Label: "Claude Pro/Max (OAuth)", Kind: MethodOAuth},
				Method{ID: "oauth_console", Label: "Anthropic Console (OAuth → API key)", Kind: MethodOAuth},
			)
		case "openai":
			out = append(out, Method{ID: "oauth", Label: "ChatGPT login (OAuth)", Kind: MethodOAuth})
		case "google":
			out = append(out, Method{ID: "oauth", Label: "Google (OAuth)", Kind: MethodOAuth})
		case "copilot":
			out = append(out, Method{ID: "oauth", Label: "GitHub device flow", Kind: MethodOAuth})
		}
	}
	if _, ok := Get(p.ID); ok {
		out = append(out, Method{ID: "remove", Label: "Remove stored credential", Kind: MethodRemove})
	}
	return out
}
