package auth

import (
	"slices"
	"testing"
)

// These tests pin the SHARED catalog both the TUI /connect dialog and the
// web/desktop Connectors endpoints now render from. Before the extraction
// each surface carried its own verbatim copy, which is how they drifted; a
// regression here means one surface will disagree with the other again.

// preserveProviderCredential saves and restores a provider's base credential
// around a test that mutates the process-global store.
func preserveProviderCredential(t *testing.T, providerID string) {
	t.Helper()
	prev, had := Get(providerID)
	t.Cleanup(func() {
		if had {
			_ = Set(providerID, prev)
		} else {
			_ = Remove(providerID)
		}
	})
}

// methodIDs reduces a Method list to its ids, the shape both surfaces key on.
func methodIDs(methods []Method) []string {
	out := make([]string, 0, len(methods))
	for _, m := range methods {
		out = append(out, m.ID)
	}
	return out
}

func TestMethodsForAlwaysOffersAPIKeyFirst(t *testing.T) {
	for i := range Providers {
		p := &Providers[i]
		preserveProviderCredential(t, p.ID)
		_ = Remove(p.ID) // unstored, so the result is the "not connected" case

		methods := MethodsFor(p)
		if len(methods) == 0 {
			t.Fatalf("%s: no methods offered", p.ID)
		}
		if methods[0].ID != "apikey" || methods[0].Label != "API Key" {
			t.Errorf("%s: first method = %+v, want apikey/API Key", p.ID, methods[0])
		}
		if methods[0].Kind != MethodAPIKey {
			t.Errorf("%s: apikey kind = %q, want %q", p.ID, methods[0].Kind, MethodAPIKey)
		}
	}
}

// Every method carries a kind. The web UI branches on it to choose a control,
// so a method with an empty Kind is a render bug on the server even though the
// TUI only shows labels.
func TestMethodsForEveryMethodCarriesAKind(t *testing.T) {
	valid := []MethodKind{MethodAPIKey, MethodOAuth, MethodPlugin, MethodRemove}
	for i := range Providers {
		p := &Providers[i]
		preserveProviderCredential(t, p.ID)
		for _, m := range MethodsFor(p) {
			if !slices.Contains(valid, m.Kind) {
				t.Errorf("%s: method %q has kind %q, want one of %v", p.ID, m.ID, m.Kind, valid)
			}
			if m.Label == "" {
				t.Errorf("%s: method %q has an empty label", p.ID, m.ID)
			}
		}
	}
}

func TestMethodsForBuiltInOAuthFlows(t *testing.T) {
	cases := []struct {
		provider string
		want     []string
	}{
		// Anthropic offers two distinct OAuth paths, not one.
		{"anthropic", []string{"apikey", "oauth_max", "oauth_console"}},
		{"openai", []string{"apikey", "oauth"}},
		{"google", []string{"apikey", "oauth"}},
		{"copilot", []string{"apikey", "oauth"}},
	}
	for _, tc := range cases {
		p := FindProvider(tc.provider)
		if p == nil {
			t.Fatalf("%s not in the catalog", tc.provider)
		}
		preserveProviderCredential(t, tc.provider)
		_ = Remove(tc.provider)
		if got := methodIDs(MethodsFor(p)); !slices.Equal(got, tc.want) {
			t.Errorf("%s: methods = %v, want %v", tc.provider, got, tc.want)
		}
	}
}

func TestMethodsForOAuthLabels(t *testing.T) {
	// provider -> method id -> expected label
	want := map[string]map[string]string{
		"anthropic": {
			"oauth_max":     "Claude Pro/Max (OAuth)",
			"oauth_console": "Anthropic Console (OAuth → API key)",
		},
		"openai":  {"oauth": "ChatGPT login (OAuth)"},
		"google":  {"oauth": "Google (OAuth)"},
		"copilot": {"oauth": "GitHub device flow"},
	}
	for providerID, byMethod := range want {
		p := FindProvider(providerID)
		if p == nil {
			t.Fatalf("%s not in the catalog", providerID)
		}
		preserveProviderCredential(t, providerID)
		_ = Remove(providerID)

		offered := map[string]string{}
		for _, m := range MethodsFor(p) {
			offered[m.ID] = m.Label
		}
		for methodID, wantLabel := range byMethod {
			if got, ok := offered[methodID]; !ok {
				t.Errorf("%s: method %q not offered", providerID, methodID)
			} else if got != wantLabel {
				t.Errorf("%s/%s: label = %q, want %q", providerID, methodID, got, wantLabel)
			}
		}
	}
}

// A provider with no OAuth flow and no plugin offers exactly one method.
func TestMethodsForPlainProviderOffersOnlyAPIKey(t *testing.T) {
	// opencode/zen has no OAuthFlow in the catalog.
	p := FindProvider("opencode")
	if p == nil {
		t.Skip("opencode provider not in the catalog")
	}
	if p.OAuthFlow != "" {
		t.Skipf("opencode now declares OAuthFlow %q; pick a different fixture", p.OAuthFlow)
	}
	preserveProviderCredential(t, "opencode")
	_ = Remove("opencode")
	if got := methodIDs(MethodsFor(p)); !slices.Equal(got, []string{"apikey"}) {
		t.Errorf("opencode: methods = %v, want [apikey]", got)
	}
}

// Removal appears only once a credential is stored, and always last.
func TestMethodsForOffersRemoveOnlyWhenStored(t *testing.T) {
	p := FindProvider("openai")
	if p == nil {
		t.Fatal("openai not in the catalog")
	}
	preserveProviderCredential(t, "openai")

	_ = Remove("openai")
	if ids := methodIDs(MethodsFor(p)); slices.Contains(ids, "remove") {
		t.Errorf("remove offered with no credential stored: %v", ids)
	}

	if err := Set("openai", Credential{Kind: KindAPIKey, Key: "sk-test"}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	methods := MethodsFor(p)
	ids := methodIDs(methods)
	if !slices.Contains(ids, "remove") {
		t.Errorf("remove not offered with a credential stored: %v", ids)
	}
	if last := methods[len(methods)-1]; last.ID != "remove" || last.Kind != MethodRemove {
		t.Errorf("last method = %+v, want remove/%s", last, MethodRemove)
	}
}

// A provider plugin REPLACES the built-in OAuth flows, and Grok's x.com
// subscription gets a dedicated id the UI dispatches outside the generic
// plugin path (it needs the cookies the user pastes).
func TestMethodsForGrokSubscriptionUsesDedicatedID(t *testing.T) {
	p := FindProvider("grok")
	if p == nil {
		t.Skip("grok not in the catalog")
	}
	// This package's test binary cannot link a provider plugin: every plugin
	// imports internal/auth (for the credential store), so importing one here
	// to trigger its init would be an import cycle. Provider plugins register
	// via init in packages that depend on auth, so Get returns false here even
	// though it is true in the TUI and server binaries. The plugin branch is
	// therefore asserted in internal/server, whose test binary does link grok
	// (TestConnectMethodsMatchSharedCatalog_GrokSubscription).
	if methods := MethodsFor(p); len(methods) != 1 || methods[0].ID != "apikey" {
		t.Fatalf("grok: expected the no-plugin shape here, got %v; if this now "+
			"passes a plugin in an auth-linked binary, move the assertion to internal/server", methodIDs(methods))
	}
}
