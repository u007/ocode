package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
)

// An opaque request — one whose effects the judge cannot establish, so it
// labels the concern truncated_or_unknown (e.g. "$g --version") — clears the
// lower opaque confidence floor. A plain 0.80 allow was previously deferred to
// the human even though the judge had resolved everything it could.
func TestTypesafeOpaqueAllowClearsLowerFloor(t *testing.T) {
	a, _ := newTypesafeJudge(t, typesafeReplyWithConcern("allow", 0.80, concernTruncatedOrUnknown))

	allowed, reason, _, consulted := a.consultPermissionModel("bash", json.RawMessage(`{"command":"$g --version"}`), nil)
	if !allowed || !consulted {
		t.Fatalf("expected opaque allow at 0.80 to auto-grant, got allowed=%v consulted=%v reason=%q", allowed, consulted, reason)
	}
}

// The lower floor is exactly the opaque boundary: 0.75 grants, anything below
// still fails closed.
func TestTypesafeOpaqueFloorBoundary(t *testing.T) {
	a, _ := newTypesafeJudge(t, typesafeReplyWithConcern("allow", 0.75, concernTruncatedOrUnknown))
	if allowed, reason, _, consulted := a.consultPermissionModel("bash", json.RawMessage(`{"command":"$g --version"}`), nil); !allowed || !consulted {
		t.Fatalf("expected 0.75 opaque allow to auto-grant, got allowed=%v consulted=%v reason=%q", allowed, consulted, reason)
	}

	a2, _ := newTypesafeJudge(t, typesafeReplyWithConcern("allow", 0.74, concernTruncatedOrUnknown))
	allowed, reason, _, consulted := a2.consultPermissionModel("bash", json.RawMessage(`{"command":"$g --version"}`), nil)
	if allowed || !consulted {
		t.Fatalf("expected 0.74 opaque allow to defer, got allowed=%v consulted=%v", allowed, consulted)
	}
	if !strings.Contains(reason, "0.75") {
		t.Fatalf("reason should cite the opaque floor 0.75, got %q", reason)
	}
}

// The lower floor must not leak to every request: a below-floor allow that the
// judge could resolve still defers at the normal 0.85 floor.
func TestTypesafeResolvedConcernKeepsNormalFloor(t *testing.T) {
	for _, concern := range []string{"none", "secrets", "network"} {
		a, _ := newTypesafeJudge(t, typesafeReplyWithConcern("allow", 0.80, concern))
		allowed, reason, _, consulted := a.consultPermissionModel("bash", json.RawMessage(`{"command":"$g --version"}`), nil)
		if allowed || !consulted {
			t.Fatalf("concern %q: expected below-floor defer, got allowed=%v consulted=%v", concern, allowed, consulted)
		}
		if !strings.Contains(reason, "0.85") {
			t.Fatalf("concern %q: reason should cite the normal floor 0.85, got %q", concern, reason)
		}
	}
}

// A configured min_confidence above the opaque default still governs: an
// opaque allow that would clear 0.75 must defer when the user set 0.95.
func TestTypesafeOpaqueConfiguredFloorStillGoverns(t *testing.T) {
	a, _ := newTypesafeJudge(t, typesafeReplyWithConcern("allow", 0.80, concernTruncatedOrUnknown))
	a.config.Ocode.Permissions.Auto.MinConfidence = 0.95

	allowed, reason, _, consulted := a.consultPermissionModel("bash", json.RawMessage(`{"command":"$g --version"}`), nil)
	if allowed || !consulted {
		t.Fatalf("expected the strict configured floor to defer the opaque allow, got allowed=%v consulted=%v", allowed, consulted)
	}
	if !strings.Contains(reason, "0.95") {
		t.Fatalf("reason should cite the configured floor 0.95, got %q", reason)
	}
}

// resolveAutoJudgeOpaqueMinConfidence only supplies its lower default when
// min_confidence is unset: an explicit configured value always governs, so the
// opaque relaxation can never loosen (or raise) the user's own bar.
func TestResolveAutoJudgeOpaqueMinConfidence(t *testing.T) {
	tests := []struct {
		name       string
		configured float64
		wantNormal float64
		wantOpaque float64
	}{
		{"unset uses defaults", 0, autoJudgeMinConfidenceDefault, autoJudgeOpaqueMinConfidenceDefault},
		{"permissive config is honoured", 0.5, 0.5, 0.5},
		{"strict config still governs", 0.95, 0.95, 0.95},
		{"equal config unchanged", 0.75, 0.75, 0.75},
		{"default-valued config governs too", 0.85, 0.85, 0.85},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := NewAgent(nil, nil, &config.Config{}, nil)
			a.config.Ocode.Permissions.Auto = &config.AutoPermissionConfig{MinConfidence: tc.configured}
			if got := a.resolveAutoJudgeMinConfidence(); got != tc.wantNormal {
				t.Fatalf("normal floor = %v, want %v", got, tc.wantNormal)
			}
			if got := a.resolveAutoJudgeOpaqueMinConfidence(); got != tc.wantOpaque {
				t.Fatalf("opaque floor = %v, want %v", got, tc.wantOpaque)
			}
		})
	}
}
