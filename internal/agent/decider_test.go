package agent

import "testing"

// TestDecider_TypesafeSatisfiesInterface pins the property that makes the whole
// Decider seam cheap: TypesafeClient already declares all four methods, so
// widening the judges to an interface is a signature change, not a rewrite.
// If a future refactor renames or re-signs any of these, this fails to compile.
func TestDecider_TypesafeSatisfiesInterface(t *testing.T) {
	var _ Decider = (*TypesafeClient)(nil)
}

// TestDecider_SlotModelDefaults pins the invariant that an unconfigured install
// behaves identically to today: every slot resolves to Jev. Part 02 of the plan
// replaces slotModel's body with a config lookup; this test is what proves that
// lookup defaults back to the same value rather than to "" (a "" model would make
// resolveDecider return nil and silently disable every judge).
func TestDecider_SlotModelDefaults(t *testing.T) {
	a := &Agent{}
	slots := []judgeSlot{
		slotPermission,
		slotAutoContinue,
		slotDiscovery,
		slotDocSearch,
		slotCodeSearch,
		slotNetworkGuard,
		slotContentGuard,
	}
	for _, s := range slots {
		if got := a.slotModel(s); got != defaultJudgeModel {
			t.Errorf("slot %s: slotModel = %q, want %q", s, got, defaultJudgeModel)
		}
	}
}

// TestDecider_ResolveDeciderNilWithoutConfig pins the "judge disabled" signal.
// resolveDecider returns nil rather than an error when there is no config,
// because "no judge configured" is a normal state every caller already handles —
// they keep every candidate. Returning an error here would tempt a caller to
// treat a missing judge as a failure.
func TestDecider_ResolveDeciderNilWithoutConfig(t *testing.T) {
	a := &Agent{}
	if got := a.resolveDecider(slotDiscovery); got != nil {
		t.Errorf("resolveDecider with nil config = %v, want nil", got)
	}
}

// TestDecider_ResolveDeciderNilOnNilAgent guards the nil receiver. A judge
// resolver can be reached from a status read on a partially built agent; a nil
// deref there would take down the status endpoint rather than reporting
// "no judge".
func TestDecider_ResolveDeciderNilOnNilAgent(t *testing.T) {
	var a *Agent
	if got := a.resolveDecider(slotPermission); got != nil {
		t.Errorf("resolveDecider on nil agent = %v, want nil", got)
	}
	if got := a.slotModel(slotPermission); got != defaultJudgeModel {
		t.Errorf("slotModel on nil agent = %q, want %q", got, defaultJudgeModel)
	}
}

// TestDecider_IsDecisionModel pins provider-prefix handling. The cloudflare-workers
// provider serves BOTH chat models and clef decision models, so a bare model name
// is not enough: the provider prefix is what distinguishes them. A predicate that
// matched on the name alone would hijack an unrelated provider's model.
func TestDecider_IsDecisionModel(t *testing.T) {
	tests := []struct {
		modelID string
		want    bool
	}{
		{"typesafe/jev-latest", true},
		{"typesafe/jev-1.13.0", true},
		{"cloudflare-workers/@cf/cloudflare/clef-flash", true},
		{"cloudflare-workers/@cf/cloudflare/clef", true},
		{"cloudflare-workers/@cf/zai-org/glm-4.7-flash", false},
		{"cloudflare-workers/@cf/meta/llama-3.3-70b-instruct-fp8-fast", false},
		{"openai/gpt-5", false},
		{"anthropic/claude-opus-4-8", false},
		{"", false},
	}
	for _, tc := range tests {
		if got := isDecisionModel(tc.modelID); got != tc.want {
			t.Errorf("isDecisionModel(%q) = %v, want %v", tc.modelID, got, tc.want)
		}
	}
}

// TestDecider_ClefSelectorUsesLastPathSegment pins the trap that makes clef
// routable. The Cloudflare URL model id contains TWO slashes
// ("@cf/cloudflare/clef-flash") while the request body wants the bare selector
// ("clef-flash"), and Cloudflare's schema pattern accepts only "clef" or
// "clef-flash". Splitting on the FIRST slash yields "cloudflare/clef-flash",
// which the schema rejects — and, because isDecisionModel uses the same split,
// it would also mean clef is never routed at all, so the backend would silently
// not exist. This test is the guard for both consequences.
func TestDecider_ClefSelectorUsesLastPathSegment(t *testing.T) {
	tests := []struct{ model, want string }{
		{"@cf/cloudflare/clef-flash", "clef-flash"},
		{"@cf/cloudflare/clef", "clef"},
		{"clef-flash", "clef-flash"},
		{"clef", "clef"},
		// A second slash must not be assumed absent: "@cf/cloudflare/..." already
		// contains one, and a naive strings.Cut would return "cloudflare/clef-flash".
		{"cloudflare-workers/@cf/cloudflare/clef-flash", "clef-flash"},
	}
	for _, tc := range tests {
		if got := clefBodySelector(tc.model); got != tc.want {
			t.Errorf("clefBodySelector(%q) = %q, want %q", tc.model, got, tc.want)
		}
	}
}
