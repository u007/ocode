package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/tool"
)

// newPermissionAgent builds an agent wired for the plain auto-permission path
// with a scripted judge, returning the agent and the capturing client.
func newPermissionAgent(t *testing.T, responses ...string) (*Agent, *scriptedCaptureClient) {
	t.Helper()
	cfg := &config.Config{}
	cfg.Ocode.Permissions.Auto = &config.AutoPermissionConfig{Enabled: true, Model: "mock/model"}
	a := NewAgent(nil, nil, cfg, nil)
	a.Permissions().SetAutoPermissionEnabled(true)
	a.AddTools([]tool.Tool{&MockTool{name: "bash", result: "ran"}})
	client := &scriptedCaptureClient{Responses: responses}
	prev := newClientFn
	t.Cleanup(func() { newClientFn = prev })
	newClientFn = func(_ *config.Config, _ string) LLMClient { return client }
	return a, client
}

// longLoopCommand builds a multi-line loop whose body reaches the judge (the
// body command is unknown so it is not statically auto-allowed) and whose total
// size is comfortably under the judge budget but far over the old 500-byte cap.
func longLoopCommand(lines int) string {
	var sb strings.Builder
	sb.WriteString("for i in 1 2 3; do\n")
	for j := 0; j < lines; j++ {
		fmt.Fprintf(&sb, "  echo \"padding line %d padding padding padding\"\n", j)
	}
	sb.WriteString("  unknownbodycmd \"$i\"\n")
	sb.WriteString("done\n")
	return sb.String()
}

// A realistic multi-line loop must reach the judge in full. The old 500-byte
// cap on the raw (JSON-escaped) arguments cut the decisive tail off long before
// this size, so the judge ruled on a prefix.
func TestPermissionJudgeSeesFullMultiLineCommand(t *testing.T) {
	script := longLoopCommand(60)
	if len(script) <= 500 {
		t.Fatalf("test premise broken: script is only %d bytes", len(script))
	}
	if len(script) >= permissionArgBudget {
		t.Fatalf("test premise broken: script is %d bytes, budget is %d", len(script), permissionArgBudget)
	}

	a, client := newPermissionAgent(t, "ALLOW: benign padding loop")
	res, err := a.HandleToolCall("bash", json.RawMessage(`{"command":`+jsonStr(script)+`}`))
	if err != nil {
		t.Fatal(err)
	}
	if res != "ran" {
		t.Fatalf("expected execution, got %q", truncate(res, 160))
	}
	prompt := client.Prompts[0]

	// The tail of the block — the part that decides safety — must be present.
	for _, want := range []string{
		"for i in 1 2 3; do\n",
		`unknownbodycmd "$i"`,
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("judge prompt missing %q (command was cut before the tail)", want)
		}
	}
	if strings.Contains(prompt, "...(truncated)") {
		t.Errorf("judge prompt flagged truncation for a %d-byte command (budget %d)", len(script), permissionArgBudget)
	}
	// Bash arguments are rendered unescaped: JSON quoting must not inflate a
	// command or hide its real newlines from the judge.
	if !strings.Contains(prompt, `echo "padding line 0`) {
		t.Errorf("bash command was not rendered as written (quotes escaped?)")
	}
}

// A command longer than the budget is shown truncated and must NOT be
// auto-granted on the judge's word — a partial view cannot become a silent
// approval. It falls through to a human ask.
func TestPermissionJudgeTruncatedArgsCannotAutoGrant(t *testing.T) {
	huge := "unknownbodycmd " + strings.Repeat("x", permissionArgBudget+2000)
	if !permissionArgsTruncatedForTest(json.RawMessage(`{"command":` + jsonStr(huge) + `}`)) {
		t.Fatal("test premise broken: command is not over budget")
	}

	a, client := newPermissionAgent(t, "ALLOW: looks fine to me")

	asked := false
	a.OnPermissionAsk = func(req PermissionRequest) PermissionResponse {
		asked = true
		return PermissionResponse{Level: PermissionDeny}
	}

	res, err := a.HandleToolCall("bash", json.RawMessage(`{"command":`+jsonStr(huge)+`}`))
	if err != nil {
		t.Fatal(err)
	}
	if res == "ran" {
		t.Fatal("an over-budget command was auto-executed on a partial view")
	}
	if !asked {
		t.Fatalf("expected the human permission prompt to be consulted, got %q", truncate(res, 160))
	}
	if !strings.Contains(client.Prompts[0], "...(truncated)") {
		t.Error("judge prompt should mark the truncated view")
	}
	if !strings.Contains(client.Prompts[0], "TRUNCATED") {
		t.Error("judge prompt should warn the judge not to approve a partial view")
	}
}

// Small arguments keep their previous behavior: no truncation marker, judge
// ALLOW auto-executes.
func TestPermissionJudgeSmallArgsUnaffected(t *testing.T) {
	a, client := newPermissionAgent(t, "ALLOW: fine")
	res, err := a.HandleToolCall("bash", json.RawMessage(`{"command":"unknownsmallcmd arg"}`))
	if err != nil {
		t.Fatal(err)
	}
	if res != "ran" {
		t.Fatalf("expected execution, got %q", truncate(res, 120))
	}
	if strings.Contains(client.Prompts[0], "...(truncated)") {
		t.Error("small args must not be marked truncated")
	}
}

// permissionArgsPayload contract: bash renders the raw command (real newlines,
// no JSON escaping); every other tool renders the raw JSON args; anything over
// budget is cut and reported as truncated.
func TestPermissionArgsPayload(t *testing.T) {
	cmd := "for i in 1 2; do\n  echo \"$i\"\ndone\n"
	got, truncated := permissionArgsPayload("bash", json.RawMessage(`{"command":`+jsonStr(cmd)+`}`))
	if truncated {
		t.Error("bash command under budget reported truncated")
	}
	if got != cmd {
		t.Errorf("bash payload = %q, want the raw command %q", got, cmd)
	}

	jsonArgs := json.RawMessage(`{"path":"a.txt","content":"hi"}`)
	got, truncated = permissionArgsPayload("write", jsonArgs)
	if truncated || got != string(jsonArgs) {
		t.Errorf("write payload = %q truncated=%v, want raw JSON", got, truncated)
	}

	big := "x" + strings.Repeat("y", permissionArgBudget)
	got, truncated = permissionArgsPayload("bash", json.RawMessage(`{"command":`+jsonStr(big)+`}`))
	if !truncated {
		t.Fatal("over-budget payload not reported truncated")
	}
	if len(got) > permissionArgBudget+len("\n...(truncated)") {
		t.Errorf("truncated payload is %d bytes, over budget %d", len(got), permissionArgBudget)
	}
	if !strings.HasPrefix(got, "x"+strings.Repeat("y", 100)) {
		t.Error("truncated payload should keep the prefix")
	}
	if !strings.HasSuffix(got, "...(truncated)") {
		t.Error("truncated payload should end with the truncation marker")
	}
}

// A malformed/absent command falls back to the raw JSON rather than losing the
// arguments entirely.
func TestPermissionArgsPayloadFallsBackToJSON(t *testing.T) {
	raw := json.RawMessage(`not json`)
	got, truncated := permissionArgsPayload("bash", raw)
	if truncated {
		t.Error("unexpected truncation for a short payload")
	}
	if got != string(raw) {
		t.Errorf("fallback payload = %q, want %q", got, raw)
	}
}

func permissionArgsTruncatedForTest(args json.RawMessage) bool {
	_, truncated := permissionArgsPayload("bash", args)
	return truncated
}
