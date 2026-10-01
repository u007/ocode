package server

import (
	"context"
	"slices"
	"testing"

	"github.com/u007/ocode/internal/auth"
	providerplugin "github.com/u007/ocode/internal/plugin/provider"
)

// The /connect list endpoint and the TUI /connect dialog render from
// auth.MethodsFor (internal/auth/methods.go). Before the extraction each
// carried its own copy of that logic and drifted silently. These tests pin
// BOTH adapters to the single source: an adapter that grows local logic again
// fails here instead of disagreeing with the other surface at runtime.

// TestConnectMethodsMatchSharedCatalog asserts the wire payload is exactly the
// shared catalog, field for field, for every provider in the catalog. Runs over
// both the connected and unconnected shapes so the remove entry is covered.
func TestConnectMethodsMatchSharedCatalog(t *testing.T) {
	for _, stored := range []bool{false, true} {
		for i := range auth.Providers {
			p := &auth.Providers[i]
			preserveConnectCredential(t, p.ID)
			if stored {
				if err := auth.Set(p.ID, auth.Credential{Kind: auth.KindAPIKey, Key: "sk-parity"}); err != nil {
					t.Fatalf("%s: Set: %v", p.ID, err)
				}
			} else {
				if err := auth.Remove(p.ID); err != nil {
					t.Fatalf("%s: Remove: %v", p.ID, err)
				}
			}

			shared := auth.MethodsFor(p)
			got := connectMethodsFor(p)
			if len(got) != len(shared) {
				t.Fatalf("%s (stored=%v): %d methods on the wire, %d in the shared catalog (%v)",
					p.ID, stored, len(got), len(shared), got)
			}
			for i := range shared {
				if got[i].ID != shared[i].ID {
					t.Errorf("%s (stored=%v)[%d]: wire id %q, catalog id %q", p.ID, stored, i, got[i].ID, shared[i].ID)
				}
				if got[i].Label != shared[i].Label {
					t.Errorf("%s (stored=%v)[%d]: wire label %q, catalog label %q", p.ID, stored, i, got[i].Label, shared[i].Label)
				}
				// A kind the UI cannot switch on is a render bug, so the
				// adapter must not drop or blank it.
				if got[i].Kind == "" || string(got[i].Kind) != string(shared[i].Kind) {
					t.Errorf("%s (stored=%v)[%d]: wire kind %q, catalog kind %q", p.ID, stored, i, got[i].Kind, shared[i].Kind)
				}
			}
		}
	}
}

// The plugin branch of the shared catalog is asserted here rather than in
// internal/auth, because no real provider plugin is linked into any binary —
// plugins are loaded from disk at runtime, and every plugin imports
// internal/auth, so a test in that package could not register one even if it
// wanted to. Registering a stub is the established convention in this file's
// sibling tests (see stubConnectPlugin).
func TestConnectMethodsPluginBranchGrokSubscription(t *testing.T) {
	p := auth.FindProvider("grok")
	if p == nil {
		t.Skip("grok not in the catalog")
	}
	preserveConnectCredential(t, "grok")
	if err := auth.Remove("grok"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	stubConnectPlugin(t, "grok", []providerplugin.AuthMethod{
		{Label: "Grok Subscription (x.com)", Type: "oauth", Run: func(context.Context) (providerplugin.AuthResult, error) {
			return providerplugin.AuthResult{}, nil
		}},
		// No Run: the catalog must skip it, so a dead entry never reaches the UI.
		{Label: "Grok Broken Method", Type: "oauth"},
	})

	got := connectMethodsFor(p)
	ids := make([]string, 0, len(got))
	for _, m := range got {
		ids = append(ids, m.ID)
		if m.ID == "grok_subscription" && m.Kind != connectMethodPlugin {
			t.Errorf("grok_subscription kind = %q, want %q", m.Kind, connectMethodPlugin)
		}
		// The plugin owns the flow, so no built-in OAuth entry may appear
		// alongside it.
		if m.ID == "oauth" || m.ID == "oauth_max" || m.ID == "oauth_console" {
			t.Errorf("grok: built-in OAuth method %q offered alongside the plugin", m.ID)
		}
	}
	if !slices.Contains(ids, "grok_subscription") {
		t.Errorf("grok: methods = %v, want a grok_subscription entry", ids)
	}
	// The Run-less method is dropped entirely.
	if slices.Contains(ids, "plugin_Grok Broken Method") {
		t.Errorf("grok: a method with no Run was offered: %v", ids)
	}
}

// A plugin method that is not the Grok subscription keeps the generic
// "plugin_<label>" id.
func TestConnectMethodsPluginBranchGenericID(t *testing.T) {
	p := auth.FindProvider("grok")
	if p == nil {
		t.Skip("grok not in the catalog")
	}
	preserveConnectCredential(t, "grok")
	if err := auth.Remove("grok"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	stubConnectPlugin(t, "grok", []providerplugin.AuthMethod{
		{Label: "API Token", Type: "oauth", Run: func(context.Context) (providerplugin.AuthResult, error) {
			return providerplugin.AuthResult{}, nil
		}},
	})

	var found bool
	for _, m := range connectMethodsFor(p) {
		if m.ID == "plugin_API Token" {
			found = true
			if m.Kind != connectMethodPlugin {
				t.Errorf("plugin method kind = %q, want %q", m.Kind, connectMethodPlugin)
			}
		}
	}
	if !found {
		t.Errorf("grok: methods = %v, want a plugin_API Token entry", connectMethodsFor(p))
	}
}
