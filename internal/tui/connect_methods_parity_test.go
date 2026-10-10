package tui

import (
	"testing"

	"github.com/u007/ocode/internal/auth"
)

// The TUI /connect dialog and the web/desktop Connectors endpoints both render
// from auth.MethodsFor (internal/auth/methods.go). They used to carry separate
// verbatim copies of that logic. This test pins the TUI's adapter to the shared
// catalog, so the two surfaces cannot drift again — a divergence here is the
// user seeing a provider offered in one UI and not the other.

// preserveConnectCredential saves and restores a provider's base credential
// around a test that mutates the process-global auth store.
func preserveConnectCredential(t *testing.T, providerID string) {
	t.Helper()
	prev, had := auth.Get(providerID)
	t.Cleanup(func() {
		if had {
			_ = auth.Set(providerID, prev)
		} else {
			_ = auth.Remove(providerID)
		}
	})
}

// TestBuildMethodsMatchesSharedCatalog asserts the dialog's method list is the
// shared catalog plus the dialog's own trailing "cancel" affordance (chrome,
// not a way to connect, so it is absent from the catalog).
func TestBuildMethodsMatchesSharedCatalog(t *testing.T) {
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
			m := &model{connect: &connectDialog{provider: p}}
			got := m.buildMethods()

			if len(got) != len(shared)+1 {
				t.Fatalf("%s (stored=%v): %d dialog methods, want %d catalog + 1 cancel",
					p.ID, stored, len(got), len(shared))
			}
			for i := range shared {
				if got[i].id != shared[i].ID {
					t.Errorf("%s (stored=%v)[%d]: dialog id %q, catalog id %q", p.ID, stored, i, got[i].id, shared[i].ID)
				}
				if got[i].label != shared[i].Label {
					t.Errorf("%s (stored=%v)[%d]: dialog label %q, catalog label %q", p.ID, stored, i, got[i].label, shared[i].Label)
				}
			}
			if last := got[len(got)-1]; last.id != "cancel" || last.label != "Cancel" {
				t.Errorf("%s (stored=%v): last method = %+v, want cancel/Cancel", p.ID, stored, last)
			}
		}
	}
}
