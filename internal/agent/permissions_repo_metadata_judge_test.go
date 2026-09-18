package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
)

// TestSandboxRepoMetadataReadsReachJudgeOrAllow mirrors the user's real setup:
// sandbox mode, auto enabled, a real (non-typesafe) judge client stubbed to
// approve. `ls .git/` must be a plain Allow (never even needing the judge);
// a repo-metadata WRITE must still consult the judge (Ask tier).
func TestSandboxRepoMetadataReadsReachJudgeOrAllow(t *testing.T) {
	cfg := &config.Config{}
	cfg.Ocode.Permissions.Auto = &config.AutoPermissionConfig{Enabled: true, Model: "anthropic/claude-sonnet-4-6"}
	a := NewAgent(nil, nil, cfg, nil)
	a.Permissions().SetWorkDir(t.TempDir())
	a.Permissions().SetMode(PermissionModeSandbox)
	a.Permissions().SetAutoPermissionEnabled(true)

	judgeCalls := 0
	prevClientFn := newClientFn
	t.Cleanup(func() { newClientFn = prevClientFn })
	newClientFn = func(_ *config.Config, _ string) LLMClient {
		judgeCalls++
		return &MockClient{Response: &Message{Role: "assistant", Content: "ALLOW: repo inspection"}}
	}
	a.OnPermissionAsk = func(req PermissionRequest) PermissionResponse {
		t.Fatalf("unexpected human prompt for %s (auto is on)", req.Command)
		return PermissionResponse{Level: PermissionDeny}
	}

	// Read: deterministic Allow, judge never consulted.
	res, err := a.HandleToolCall("bash", json.RawMessage(`{"command":"ls .git/"}`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(res, "denied:") {
		t.Fatalf("ls .git/ must not be denied, got %q", res)
	}
	if judgeCalls != 0 {
		t.Fatalf("ls .git/ consulted the judge %d times; expected a deterministic Allow", judgeCalls)
	}

	// Write: still an Ask, so the judge IS consulted (and may approve).
	judgeCalls = 0
	if _, err := a.HandleToolCall("bash", json.RawMessage(`{"command":"echo x > .git/config"}`)); err != nil {
		t.Fatal(err)
	}
	if judgeCalls == 0 {
		t.Fatal("a .git/config write must still Ask and reach the judge, but the judge was never consulted")
	}
}
