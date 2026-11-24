package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/redact"
)

const (
	judgePIIEmail  = "jane@example.com"
	judgePIISecret = "AKIAIOSFODNN7EXAMPLE"
)

// newPIIJudgeAgent builds an agent with /mask live and the contact PII group
// enabled for the conversation, over a project whose notes.txt carries both an
// email and a known-format credential. The judge payloads are built from it.
func newPIIJudgeAgent(t *testing.T, cfg *config.Config) *Agent {
	t.Helper()
	tmp := t.TempDir()
	note := "contact " + judgePIIEmail + "\nkey " + judgePIISecret + "\n"
	if err := os.WriteFile(filepath.Join(tmp, "notes.txt"), []byte(note), 0o644); err != nil {
		t.Fatal(err)
	}
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWd) })
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}

	a := newTestAgent(nil, nil, cfg, nil)
	a.Permissions().SetWorkDir(tmp)
	a.redactionEnabled = true
	a.redactionRegistry = redact.NewRegistry("judge-pii")
	a.redactionPII = []redact.PIICategory{redact.PIIContact}
	return a
}

// judgeWriteArgs is one tool call carrying the PII value and the secret in its
// arguments, whose target file (notes.txt) carries them again for the context.
func judgeWriteArgs(t *testing.T) json.RawMessage {
	t.Helper()
	args, err := json.Marshal(map[string]string{
		"path":    "notes.txt",
		"content": "contact " + judgePIIEmail + "\nkey " + judgePIISecret + "\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	return args
}

// assertJudgePayloadSecretsOnly requires a judge payload to keep the email and
// withhold the credential, and proves the categories are genuinely live by
// masking the same email through the PII-capable helper.
func assertJudgePayloadSecretsOnly(t *testing.T, site, payload string, cats []redact.PIICategory) {
	t.Helper()
	if !strings.Contains(payload, judgePIIEmail) {
		t.Errorf("%s masked PII in a permission judge payload: %q", site, payload)
	}
	if strings.Contains(payload, judgePIISecret) {
		t.Errorf("%s leaked an AWS key to a permission judge payload: %q", site, payload)
	}
	masked, err := redactWithPII("contact "+judgePIIEmail, redact.NewRegistry("control"), cats)
	if err != nil {
		t.Fatalf("%s control redactWithPII: %v", site, err)
	}
	if strings.Contains(masked, judgePIIEmail) {
		t.Fatalf("%s control: contact PII did not mask the email, the pin is vacuous: %q", site, masked)
	}
}

// TestTypesafeJudgeStateStaysSecretsOnlyWithPIIActive pins the Jev
// (decision-backend) judge site: buildTypesafePermissionState masks its
// judgeArgs with redactText and its project_context with redactFileText, both
// secrets-only. The agent's PII groups are active here, so a move to
// redactWithPII would mask the email in the state Jev receives.
func TestTypesafeJudgeStateStaysSecretsOnlyWithPIIActive(t *testing.T) {
	a := newPIIJudgeAgent(t, nil)
	state := a.buildTypesafePermissionState("write", judgeWriteArgs(t), nil)

	argsOut, err := json.Marshal(state["arguments"])
	if err != nil {
		t.Fatalf("marshal judged arguments: %v", err)
	}
	ctx, ok := state["project_context"].(string)
	if !ok {
		t.Fatalf("project_context = %T, want string", state["project_context"])
	}
	if !strings.Contains(ctx, judgePIIEmail) {
		t.Fatalf("project_context did not carry the project file at all: %q", ctx)
	}

	assertJudgePayloadSecretsOnly(t, "arguments", string(argsOut), a.redactionPII)
	assertJudgePayloadSecretsOnly(t, "project_context", ctx, a.redactionPII)
}

// TestChatJudgePromptStaysSecretsOnlyWithPIIActive pins the chat-judge site:
// askPermissionModel masks its rendered tool arguments with redactText and its
// buildPermissionContext with redactFileText, both secrets-only. The captured
// prompt is the real one the gatekeeper sends.
func TestChatJudgePromptStaysSecretsOnlyWithPIIActive(t *testing.T) {
	cfg := &config.Config{}
	cfg.Ocode.Permissions.Auto = &config.AutoPermissionConfig{Enabled: true, Model: "openai/gpt-4o-mini"}

	cap := &chatJudgeCapture{}
	prev := newClientFn
	t.Cleanup(func() { newClientFn = prev })
	newClientFn = func(*config.Config, string) LLMClient { return cap }

	a := newPIIJudgeAgent(t, cfg)
	allowed, reason, consulted := a.askPermissionModel("write", judgeWriteArgs(t), nil)
	if !consulted || !allowed {
		t.Fatalf("chat judge was not consulted: allowed=%v consulted=%v reason=%q", allowed, consulted, reason)
	}
	if !strings.Contains(cap.prompt, judgePIIEmail) {
		t.Fatalf("captured prompt did not carry the PII value at all: %q", cap.prompt)
	}

	assertJudgePayloadSecretsOnly(t, "prompt", cap.prompt, a.redactionPII)
}
