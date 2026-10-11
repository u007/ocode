package agent

import (
	"encoding/json"
	"sort"
	"testing"
	"time"
)

// clefTestSnapshot builds a wrapped registry payload containing a
// cloudflare-workers provider with exactly the given models. Used to prove the
// clef catalog is MERGED into whatever the snapshot already has, rather than
// replacing it.
func clefTestSnapshot(t *testing.T, models ...string) []byte {
	t.Helper()
	entry := providerEntry{
		ID:     cloudflareWorkersProvider,
		Models: make(map[string]modelEntry, len(models)),
	}
	for _, m := range models {
		entry.Models[m] = modelEntry{ID: m, Limit: modelLimit{Context: 4096}}
	}
	wrapped := registryFile{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Providers: map[string]providerEntry{
			cloudflareWorkersProvider: entry,
		},
	}
	b, err := json.Marshal(wrapped)
	if err != nil {
		t.Fatalf("marshal clef test snapshot: %v", err)
	}
	return b
}

// emptyTestSnapshot builds a valid registry payload with no providers at all, so
// the static catalogs are the only source of ids.
func emptyTestSnapshot(t *testing.T) []byte {
	t.Helper()
	wrapped := registryFile{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Providers:   map[string]providerEntry{},
	}
	b, err := json.Marshal(wrapped)
	if err != nil {
		t.Fatalf("marshal empty test snapshot: %v", err)
	}
	return b
}

// TestClefModelsAppearInProviderEnumeration pins that a clef model is offered by
// the picker even though models.dev does not list it. Without the static catalog
// the cloudflare-workers provider enumerates to nothing (its models.dev id is
// "cloudflare-workers-ai", a different namespace), so a judge could never be
// pointed at clef through the UI.
func TestClefModelsAppearInProviderEnumeration(t *testing.T) {
	withSandboxedModelsCache(t)
	withSnapshotBytes(t, emptyTestSnapshot(t))

	ids := allProviderModelsFromRegistry(false)
	for _, want := range []string{
		"cloudflare-workers/@cf/cloudflare/clef-flash",
		"cloudflare-workers/@cf/cloudflare/clef",
	} {
		if !containsString(ids, want) {
			t.Errorf("provider enumeration is missing %q; a clef judge would not be selectable", want)
		}
	}
}

// TestClefModelsMergeWithSnapshotEntries pins that the clef catalog is merged
// into the snapshot list rather than replacing it. Workers AI serves chat models
// from the same provider id, so a future snapshot entry must survive.
func TestClefModelsMergeWithSnapshotEntries(t *testing.T) {
	withSandboxedModelsCache(t)
	withSnapshotBytes(t, clefTestSnapshot(t, "@cf/meta/llama-3-8b-instruct"))

	ids := providerModelsFromRegistry(cloudflareWorkersProvider, false)
	for _, want := range []string{
		"@cf/meta/llama-3-8b-instruct", // the snapshot entry must not be lost
		"@cf/cloudflare/clef-flash",
		"@cf/cloudflare/clef",
	} {
		if !containsString(ids, want) {
			t.Errorf("cloudflare-workers models missing %q; got %v", want, ids)
		}
	}
	if !sort.StringsAreSorted(ids) {
		t.Errorf("model list is not sorted, which breaks deterministic picker order: %v", ids)
	}
}

// TestClefModelsDedupeAgainstSnapshot pins that a clef id already present in the
// snapshot is not listed twice. A duplicate id renders twice in the picker.
func TestClefModelsDedupeAgainstSnapshot(t *testing.T) {
	withSandboxedModelsCache(t)
	withSnapshotBytes(t, clefTestSnapshot(t, "@cf/cloudflare/clef-flash"))

	ids := providerModelsFromRegistry(cloudflareWorkersProvider, false)
	seen := 0
	for _, id := range ids {
		if id == "@cf/cloudflare/clef-flash" {
			seen++
		}
	}
	if seen != 1 {
		t.Errorf("clef-flash appears %d times, want exactly 1: %v", seen, ids)
	}
}

// TestClefModelCatalog_EveryEntryRoutes is the guard for the static catalog
// itself. A static list cannot be "missing" the way a remote source can, but it
// CAN contain a typo, and a typo would add an id to the picker that silently does
// not route to a decision backend. Assert every entry, fully qualified, is
// recognised by the same predicate the router uses.
func TestClefModelCatalog_EveryEntryRoutes(t *testing.T) {
	if len(clefModels) == 0 {
		t.Fatal("clefModels catalog is empty; the picker would offer no clef model")
	}
	for _, m := range clefModels {
		id := cloudflareWorkersProvider + "/" + m
		if !isDecisionModel(id) {
			t.Errorf("catalog entry %q (%s) does not route to a decision backend; the picker would offer an id that silently is not a judge", m, id)
		}
	}
}

// TestDecisionBackendName pins the display label the TUI uses for the judge-kind
// line. It exists because the TUI tested a literal "typesafe/" prefix, so a
// clef-backed judge was routed correctly but displayed with no kind at all.
func TestDecisionBackendName(t *testing.T) {
	cases := []struct {
		model string
		want  string
	}{
		{"typesafe/jev-latest", "typesafe"},
		{"cloudflare-workers/@cf/cloudflare/clef-flash", "clef"},
		{"cloudflare-workers/@cf/cloudflare/clef", "clef"},
		// A chat model on the SAME provider as clef must not be labelled a
		// decision backend — the provider id is shared, so the model decides.
		{"cloudflare-workers/@cf/meta/llama-3-8b-instruct", ""},
		// A bare "clef" from another provider must not be hijacked.
		{"openai/clef", ""},
		{"openai/gpt-4o", ""},
		{"", ""},
	}
	for _, tc := range cases {
		if got := DecisionBackendName(tc.model); got != tc.want {
			t.Errorf("DecisionBackendName(%q) = %q, want %q", tc.model, got, tc.want)
		}
	}
}

// TestDecisionBackendName_AgreesWithRouting pins that the display label and the
// routing predicate cannot drift: a non-empty label must mean the id routes, and
// an empty label must mean it does not.
func TestDecisionBackendName_AgreesWithRouting(t *testing.T) {
	for _, model := range []string{
		"typesafe/jev-latest",
		"cloudflare-workers/@cf/cloudflare/clef-flash",
		"cloudflare-workers/@cf/cloudflare/clef",
		"cloudflare-workers/@cf/meta/llama-3-8b-instruct",
		"openai/clef",
		"openai/gpt-4o",
		"",
	} {
		labelled := DecisionBackendName(model) != ""
		routes := isDecisionModel(model)
		if labelled != routes {
			t.Errorf("DecisionBackendName(%q) labelled=%v but isDecisionModel=%v; display and routing disagree", model, labelled, routes)
		}
	}
}
