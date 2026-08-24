package agent

import (
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
)

// TestAgentRuntimeOverrides pins the web→live-agent override semantics added
// so config toggles reach a bridged TUI agent (which holds its own config
// snapshot): nil override follows config, non-nil empty model override means
// "explicitly cleared this session" and must NOT resurrect a stale persisted
// value, and setters are safe on a nil-config agent.
func TestAgentRuntimeOverrides(t *testing.T) {
	cfg := &config.Config{}
	cfg.Ocode.SmallModel = "prov/small-a"
	cfg.Ocode.SmallModelEnabled = true
	cfg.Ocode.Permissions.Auto = &config.AutoPermissionConfig{Model: "prov/judge"}

	a := NewAgent(nil, nil, cfg, nil)

	// No overrides: everything falls through to config.
	if !a.SmallModelRuntimeEnabled() {
		t.Fatal("config-enabled small model must read enabled before any override")
	}
	if got := a.smallModelExplicitName(); got != "prov/small-a" {
		t.Fatalf("explicit name fallthrough = %q, want prov/small-a", got)
	}
	if got := a.autoPermissionModelName(); got != "prov/judge" {
		t.Fatalf("judge name fallthrough = %q, want prov/judge", got)
	}

	// Web clears the judge model: empty override wins over stale config value,
	// falling through to the small model.
	a.SetAutoPermissionModel("")
	if v, ok := a.AutoPermissionModelOverride(); !ok || v != "" {
		t.Fatalf("cleared override = (%q, %v), want (\"\", true)", v, ok)
	}
	if got := a.autoPermissionModelName(); got != "prov/small-a" {
		t.Fatalf("after clear, judge name = %q, want small-model fallback prov/small-a", got)
	}
	if got := a.autoPermissionModelDisplayName(); got != "prov/small-a (resolved small model)" {
		t.Fatalf("after clear, judge display = %q, want small-model fallback with suffix", got)
	}

	// Web sets an explicit judge model: it wins over both cleared state and
	// the stale config value.
	a.SetAutoPermissionModel("prov/judge-b")
	if got := a.autoPermissionModelName(); got != "prov/judge-b" {
		t.Fatalf("override judge name = %q, want prov/judge-b", got)
	}

	// Gate + name overrides drive the effective small-model resolution.
	a.SetSmallModelRuntimeEnabled(false)
	if a.SmallModelRuntimeEnabled() {
		t.Fatal("gate override false must win over config true")
	}
	a.SetSmallModelRuntimeEnabled(true)
	a.SetSmallModelRuntimeModel("prov/small-b")
	if got := a.resolveSmallModel(); got != "prov/small-b" {
		t.Fatalf("resolveSmallModel with override = %q, want prov/small-b", got)
	}
	// Clearing the name override skips the stale persisted name entirely and
	// falls through to registry resolution of the small model id.
	a.SetSmallModelRuntimeModel("")
	if got := a.resolveSmallModel(); got != "prov/small-a" {
		t.Fatalf("resolveSmallModel after clear = %q, want registry-resolved prov/small-a", got)
	}
}

// TestAgentRuntimeOverridesNilConfig guards the nil-config paths used by
// minimally-constructed agents in tests and headless bootstrap edges.
func TestAgentRuntimeOverridesNilConfig(t *testing.T) {
	a := NewAgent(nil, nil, nil, nil)
	if a.SmallModelRuntimeEnabled() {
		t.Fatal("nil config must report small model disabled")
	}
	if got := a.autoPermissionModelName(); got != "unavailable" {
		t.Fatalf("nil config judge name = %q, want unavailable", got)
	}
	// Setters must not panic without config.
	a.SetAutoPermissionModel("prov/x")
	if got, _ := a.autoPermissionModelDisplayName(), 0; got == "" {
		t.Fatal("display name must not be empty once overridden")
	}
	if !strings.Contains(a.autoPermissionModelName(), "prov/x") && a.autoPermissionModelName() != "unavailable" {
		t.Fatalf("unexpected name after override on nil config: %q", a.autoPermissionModelName())
	}
}
