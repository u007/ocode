package agent

import (
	"os"
	"strings"
	"testing"
)

// withHarness runs fn under the given harness identity, restoring the
// previous one afterwards.
func withHarness(t *testing.T, harness string, fn func()) {
	t.Helper()
	old := ActiveHarness()
	t.Cleanup(func() { _, _ = SetActiveHarness(old); _ = os.Unsetenv("OCODE_FAKE_AGENT") })
	if _, err := SetActiveHarness(harness); err != nil {
		t.Fatal(err)
	}
	fn()
}

// TestEnvironmentPrompt_OpencodeExactModelID pins the harness-gated second
// sentence of the model line: real opencode's env prompt reads
// "You are powered by the model named X. The exact model ID is X."
// (packages/opencode/src/session/system.ts). The ocode harness keeps the
// single sentence — mutation-verified (removing the gate fails both runs).
func TestEnvironmentPrompt_OpencodeExactModelID(t *testing.T) {
	withHarness(t, "opencode", func() {
		a := &Agent{
			client:  providerStubClient{provider: "anthropic", model: "claude-opus-4-7"},
			workDir: func() string { d, _ := os.Getwd(); return d }(),
		}
		p := a.environmentPrompt()
		if !strings.Contains(p, "The exact model ID is anthropic/claude-opus-4-7.") {
			t.Errorf("opencode harness env prompt missing exact model ID line:\n%.200s", p)
		}
	})
	withHarness(t, "ocode", func() {
		a := &Agent{
			client:  providerStubClient{provider: "anthropic", model: "claude-opus-4-7"},
			workDir: func() string { d, _ := os.Getwd(); return d }(),
		}
		p := a.environmentPrompt()
		if strings.Contains(p, "The exact model ID is") {
			t.Errorf("ocode harness must not add the exact model ID line:\n%.200s", p)
		}
	})
}

// TestBasePrompt_OpencodeIdentityLine pins the harness-gated identity prefix
// on the mode fragment: under the opencode harness the system prompt opens
// by naming the agent opencode (real opencode's prompt files open with
// "You are opencode/OpenCode, ..."), and the mode prompt opening still
// follows. The ocode harness adds nothing. Mutation-verified: removing the
// gate or the prefix fails the matching run.
func TestBasePrompt_OpencodeIdentityLine(t *testing.T) {
	const identityLine = "You are opencode, an interactive CLI tool that helps users with software engineering tasks."

	withHarness(t, "opencode", func() {
		a := &Agent{
			client: providerStubClient{provider: "anthropic", model: "claude-opus-4-7"},
			mode:   ModeBuild,
		}
		msgs := a.BasePromptMessages()
		modeMsg := findMarker(msgs, promptModeMarker)
		if modeMsg == "" {
			t.Fatal("mode fragment missing")
		}
		if !strings.HasPrefix(modeMsg, promptModeMarker+"\n"+identityLine+"\n") {
			t.Errorf("opencode harness mode fragment must open with the identity line, got:\n%.200s", modeMsg)
		}
		if !strings.Contains(modeMsg, "You are in BUILD mode.") {
			t.Errorf("mode prompt opening lost under identity prefix:\n%.200s", modeMsg)
		}
	})
	withHarness(t, "ocode", func() {
		a := &Agent{
			client: providerStubClient{provider: "anthropic", model: "claude-opus-4-7"},
			mode:   ModeBuild,
		}
		msgs := a.BasePromptMessages()
		modeMsg := findMarker(msgs, promptModeMarker)
		if modeMsg == "" {
			t.Fatal("mode fragment missing")
		}
		if strings.Contains(modeMsg, identityLine) {
			t.Errorf("ocode harness must not add the identity line:\n%.200s", modeMsg)
		}
	})
}

// TestEnvironmentPromptCache_InvalidatedOnHarnessSwitch pins the cache-key
// fix: a cached <env> prompt built under one harness must be rebuilt after
// /fake-agent switches the identity — otherwise a mid-session switch keeps
// serving the previous harness's model line.
func TestEnvironmentPromptCache_InvalidatedOnHarnessSwitch(t *testing.T) {
	withHarness(t, "ocode", func() {
		a := &Agent{
			client:  providerStubClient{provider: "anthropic", model: "claude-opus-4-7"},
			workDir: func() string { d, _ := os.Getwd(); return d }(),
		}
		if strings.Contains(a.environmentPrompt(), "The exact model ID is") {
			t.Fatal("precondition: ocode env prompt should lack the exact-ID line")
		}
		_, _ = SetActiveHarness("opencode")
		p := a.environmentPrompt()
		if !strings.Contains(p, "The exact model ID is") {
			t.Error("env prompt cache was not invalidated by the harness switch; still serving the ocode-harness block")
		}
	})
}
