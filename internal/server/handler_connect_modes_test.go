package server

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/auth"
)

// connectProviderRow returns the list row for one provider id.
func connectProviderRow(t *testing.T, h *Handler, id string) map[string]any {
	t.Helper()
	resp, code := connectDo(t, h.handleConnectList, "GET", "/api/auth/connect", nil, nil)
	if code != http.StatusOK {
		t.Fatalf("list: %d %v", code, resp)
	}
	providers, _ := resp["providers"].([]any)
	for _, p := range providers {
		m, _ := p.(map[string]any)
		if m["id"] == id {
			return m
		}
	}
	t.Fatalf("provider %q missing from list", id)
	return nil
}

// connectMethodRow returns one method row (by method id) of a provider row.
func connectMethodRow(t *testing.T, row map[string]any, methodID string) map[string]any {
	t.Helper()
	methods, _ := row["methods"].([]any)
	for _, m := range methods {
		mm, _ := m.(map[string]any)
		if mm["id"] == methodID {
			return mm
		}
	}
	t.Fatalf("provider %v missing method %q (has %v)", row["id"], methodID, row["methods"])
	return nil
}

// advertisedModes reads the completion modes a method claims to accept.
//
// A missing key and an explicit nil are both "no modes advertised" — the field
// is omitted rather than sent as null, so both are read here on purpose.
func advertisedModes(m map[string]any) []any {
	v, ok := m["modes"]
	if !ok || v == nil {
		return nil
	}
	out, _ := v.([]any)
	return out
}

// TestConnectListAdvertisesCompletionModesOnlyWhereHonoured pins which methods
// tell a client it may choose a completion mode.
//
// Only the OpenAI flow has two shapes: "auto" binds a loopback port, "manual"
// binds nothing and takes a pasted redirect. Every other flow has exactly one
// shape, so advertising a choice for it would be a lie the UI would render —
// e.g. an Anthropic paste-code flow that showed "open the page on this machine"
// next to "paste the redirect back" and then ignored the answer.
//
// The catalog is PLUGIN-SHADOWED, which is why the examples below are not
// hardcoded to one provider. auth.MethodsFor replaces a provider's built-in
// OAuth flow with a registered plugin's methods, so on a machine with the
// ChatGPT plugin installed `openai` offers two plugin_* methods and no `oauth`
// at all — verified against a live server, where `codex` was the provider that
// actually exposed the loopback flow. A test pinned to `openai`'s method ids
// would therefore pass or fail depending on the developer's installed plugins.
// The assertions below hold in both worlds.
func TestConnectListAdvertisesCompletionModesOnlyWhereHonoured(t *testing.T) {
	h := NewHandler()

	// The universal rule, stated without reference to the implementation's
	// predicate: only the plain "oauth" method ever offers a mode choice.
	t.Run("no method advertises modes unless it is the plain oauth method", func(t *testing.T) {
		resp, code := connectDo(t, h.handleConnectList, "GET", "/api/auth/connect", nil, nil)
		if code != http.StatusOK {
			t.Fatalf("list: %d %v", code, resp)
		}
		providers, _ := resp["providers"].([]any)
		if len(providers) == 0 {
			t.Fatal("no providers listed")
		}
		for _, p := range providers {
			row, _ := p.(map[string]any)
			methods, _ := row["methods"].([]any)
			for _, m := range methods {
				mm, _ := m.(map[string]any)
				if advertisedModes(mm) != nil && mm["id"] != "oauth" {
					t.Errorf("provider %v method %v advertises modes but is not the loopback oauth method",
						row["id"], mm["id"])
				}
			}
		}
	})

	// The positive case, and the reason the feature exists: at least one provider
	// really does reach the user with both modes. Asserting only the negative
	// half would pass with `modes` never set at all.
	//
	// The expected set is derived from the CATALOG, not from the handler's own
	// predicate: a provider qualifies when it declares OAuthFlow "openai" AND
	// actually offers the plain "oauth" method. Comparing the two sets is what
	// discriminates the three plausible predicates —
	//   - OAuthFlow == "openai"  → correct
	//   - provider id == "codex" → silently drops openai
	//   - method id == "oauth"   → wrongly adds google, which has no manual mode
	// A predicate-keyed assertion cannot do this: the earlier version of this
	// test asserted "openai and codex" and a mutant keyed on p.ID == "codex"
	// passed it, because in a plugin-free test binary openai offers oauth anyway.
	t.Run("exactly the loopback-flow providers advertise both modes", func(t *testing.T) {
		resp, code := connectDo(t, h.handleConnectList, "GET", "/api/auth/connect", nil, nil)
		if code != http.StatusOK {
			t.Fatalf("list: %d %v", code, resp)
		}
		providers, _ := resp["providers"].([]any)

		offersPlainOAuth := map[string]bool{}
		var got []string
		for _, p := range providers {
			row, _ := p.(map[string]any)
			id := fmt.Sprint(row["id"])
			methods, _ := row["methods"].([]any)
			for _, m := range methods {
				mm, _ := m.(map[string]any)
				if mm["id"] == "oauth" {
					offersPlainOAuth[id] = true
				}
				modes := advertisedModes(mm)
				if len(modes) == 2 && modes[0] == "auto" && modes[1] == "manual" {
					got = append(got, id)
				}
			}
		}

		var want []string
		for i := range auth.Providers {
			p := &auth.Providers[i]
			// OAuthFlow, not the id: openai and codex both declare "openai",
			// and a plugin can replace a provider's flow with plugin_* methods.
			if p.OAuthFlow == "openai" && offersPlainOAuth[p.ID] {
				want = append(want, p.ID)
			}
		}
		sort.Strings(got)
		sort.Strings(want)
		if len(got) == 0 {
			t.Fatalf("no provider advertises [auto manual]; manual mode is unreachable (catalog wants %v)", want)
		}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("mode-advertising providers = %v, want %v (OAuthFlow \"openai\" ∧ offers oauth)", got, want)
		}
	})

	t.Run("anthropic paste-code offers no choice", func(t *testing.T) {
		row := connectProviderRow(t, h, "anthropic")
		for _, id := range []string{"oauth_max", "oauth_console"} {
			if got := advertisedModes(connectMethodRow(t, row, id)); got != nil {
				t.Fatalf("anthropic %s modes = %v, want none", id, got)
			}
		}
	})

	t.Run("google offers no choice until manual mode exists", func(t *testing.T) {
		// Its method id is also "oauth", so this is what proves the predicate is
		// not keyed on the method id.
		m := connectMethodRow(t, connectProviderRow(t, h, "google"), "oauth")
		if got := advertisedModes(m); got != nil {
			t.Fatalf("google oauth modes = %v, want none", got)
		}
	})

	t.Run("copilot device flow offers no choice", func(t *testing.T) {
		m := connectMethodRow(t, connectProviderRow(t, h, "copilot"), "oauth")
		if got := advertisedModes(m); got != nil {
			t.Fatalf("copilot oauth modes = %v, want none", got)
		}
	})
}
