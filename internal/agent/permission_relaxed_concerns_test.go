package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
)

// relaxedJudgeAgent builds the judge harness with the user's opt-outs configured.
func relaxedJudgeAgent(t *testing.T, reply string, relaxed ...string) (*Agent, *typesafeJudgeHarness) {
	t.Helper()
	a, h := newTypesafeJudge(t, reply)
	a.config.Ocode.Permissions.Auto.RelaxedConcerns = relaxed
	return a, h
}

// The checkbox catalog must BE the rubric, minus "none" (the "no problem"
// answer, which is not a rule anyone can enforce). A drift here would let the
// settings UI offer a category the judge cannot name, or hide one it can.
func TestRelaxableConcernsMatchRubric(t *testing.T) {
	catalog := RelaxableConcerns()
	if len(catalog) != len(typesafeConcerns)-1 {
		t.Fatalf("catalog size %d, rubric %d (minus none)", len(catalog), len(typesafeConcerns)-1)
	}
	for i, c := range catalog {
		want := typesafeConcerns[i+1] // typesafeConcerns[0] is "none"
		if c.Key != want.Key || c.Label != want.Label {
			t.Fatalf("catalog[%d] = %+v, rubric %+v", i, c, want)
		}
		if !IsRelaxableConcern(c.Key) {
			t.Fatalf("IsRelaxableConcern(%q) = false", c.Key)
		}
	}
	if IsRelaxableConcern("none") {
		t.Fatal("\"none\" must not be relaxable — it is not a rule")
	}
	if IsRelaxableConcern("invented") {
		t.Fatal("unknown key accepted as relaxable")
	}
	// Every offered category must carry its caveat, so the UI can never overstate
	// what a switch does.
	for _, c := range catalog {
		if c.Note == "" {
			t.Fatalf("%s missing its note", c.Key)
		}
	}
}

// Config is hand-editable: unknown, duplicate and empty keys must never reach the
// prompt, and the order must be stable (sorted) for a diffable debug log.
func TestRelaxedConcernKeysFiltersAndSorts(t *testing.T) {
	a, _ := relaxedJudgeAgent(t, typesafeChoiceReply("allow", 0.9),
		"secrets", "invented", "secrets", "network", "")

	got := a.relaxedConcernKeys()
	if len(got) != 2 || got[0] != "network" || got[1] != "secrets" {
		t.Fatalf("relaxedConcernKeys = %#v, want [network secrets]", got)
	}
	if set := a.relaxedConcernSet(); !set["secrets"] || set["invented"] {
		t.Fatalf("relaxedConcernSet = %#v", set)
	}
}

// Nothing relaxed must leave the prompt byte-identical to today's, so the feature
// is inert for every existing user.
func TestRelaxedConcernsClauseEmptyWhenNothingRelaxed(t *testing.T) {
	if got := relaxedConcernsClause(nil); got != "" {
		t.Fatalf("empty clause expected, got %q", got)
	}
	if got := relaxedConcernsClause((&Agent{}).relaxedConcernKeys()); got != "" {
		t.Fatalf("empty clause expected for unconfigured agent, got %q", got)
	}
	clause := relaxedConcernsClause([]string{"secrets"})
	for _, want := range []string{"secrets", "switched OFF", "ONLY concern", "ALLOWED"} {
		if !strings.Contains(clause, want) {
			t.Fatalf("clause missing %q: %s", want, clause)
		}
	}
	if !strings.Contains(clause, typesafeConcernLabel("secrets")) {
		t.Fatal("clause should spell out the human-readable label")
	}
}

func a(keys []string) *Agent { return &Agent{} }

// so a model that reads state before instructions still sees them.
func TestRelaxedConcernsReachTheWire(t *testing.T) {
	a, h := relaxedJudgeAgent(t, typesafeChoiceReply("allow", 0.9), "secrets")

	a.consultPermissionModel("bash", json.RawMessage(`{"command":"n=$(cat .env)"}`), nil)

	h.mu.Lock()
	defer h.mu.Unlock()
	state := h.body["state"].(map[string]any)
	relaxed, ok := state["relaxed_concerns"].([]any)
	if !ok || len(relaxed) != 1 || relaxed[0] != "secrets" {
		t.Fatalf("state.relaxed_concerns = %#v", state["relaxed_concerns"])
	}
	qs := h.body["questions"].(map[string]any)
	for _, key := range []string{typesafeJudgeVerdictKey, typesafeJudgeConcernKey} {
		instr, _ := qs[key].(map[string]any)["instructions"].(string)
		if !strings.Contains(instr, "switched OFF enforcement") {
			t.Fatalf("%s question lacks the relaxed clause:\n%s", key, instr)
		}
	}
}

// The deterministic backstop: the rubric asks the judge to allow a call whose only
// concern is switched off, but when it denies anyway and names that category, the
// user's choice wins — after Go's own guards.
func TestRelaxedConcernDenyIsHonoured(t *testing.T) {
	a, _ := relaxedJudgeAgent(t, typesafeReplyWithConcern("deny", 0.99, "secrets"), "secrets")
	allowed, reason, _, consulted := a.consultPermissionModel("bash", json.RawMessage(`{"command":"cat .env"}`), nil)
	if !allowed || !consulted {
		t.Fatalf("relaxed deny should convert to allow: allowed=%v consulted=%v reason=%q", allowed, consulted, reason)
	}
}

// Fail closed when the deny cannot be ATTRIBUTED to an opted-out category.
func TestRelaxedConcernDenyStandsWhenNotAttributable(t *testing.T) {
	cases := []struct {
		name    string
		concern string
		relaxed []string
	}{
		{"concern still enforced", "secrets", []string{"network"}},
		{"concern none", "none", []string{"secrets"}},
		{"nothing relaxed", "secrets", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, _ := relaxedJudgeAgent(t, typesafeReplyWithConcern("deny", 0.99, tc.concern), tc.relaxed...)
			allowed, reason, _, consulted := a.consultPermissionModel("bash", json.RawMessage(`{"command":"cat .env"}`), nil)
			if allowed {
				t.Fatalf("deny must stand (concern=%q relaxed=%v)", tc.concern, tc.relaxed)
			}
			if !consulted || !strings.Contains(reason, "chose deny") {
				t.Fatalf("expected a consulted deny, got consulted=%v reason=%q", consulted, reason)
			}
		})
	}
}

// The relaxed backstop must not become a way past Go's deterministic guards: an
// out-of-scope path is refused even when the denied concern is switched off.
func TestRelaxedConcernDenyStillSubjectToVerifyAutoGrant(t *testing.T) {
	a, _ := relaxedJudgeAgent(t, typesafeReplyWithConcern("deny", 0.99, "secrets"), "secrets")
	req := &PermissionRequest{
		ToolName:       "bash",
		Command:        "cat /etc/hosts",
		Rule:           "bash.prefix.sandbox.sensitive",
		OutOfScopePath: "/etc/hosts",
	}
	allowed, reason, _, consulted := a.consultPermissionModel("bash", json.RawMessage(`{"command":"cat /etc/hosts"}`), req)
	if allowed {
		t.Fatal("relaxed category must not bypass the out-of-scope path guard")
	}
	if !consulted || !strings.Contains(reason, "outside allowed roots") {
		t.Fatalf("expected the out-of-scope refusal, got consulted=%v reason=%q", consulted, reason)
	}
}

// A relaxed `destructive` must not convert a judge verdict into a grant for a
// force/recursive rm outside the allowed scope: dangerousRmReason's contract is
// that such a command always needs a human, and its Ask carries no
// OutOfScopePath for the check above to catch.
func TestRelaxedDestructiveCannotGrantDangerousRm(t *testing.T) {
	for _, reply := range []string{
		typesafeReplyWithConcern("deny", 0.99, "destructive"),
		typesafeChoiceReply("allow", 0.99),
	} {
		a, _ := relaxedJudgeAgent(t, reply, "destructive")
		a.permissions = NewPermissionManager()
		a.permissions.workDir = "/Users/james/www/proposal"
		allowed, reason, _, _ := a.consultPermissionModel("bash",
			json.RawMessage(`{"command":"rm -rf ../"}`),
			&PermissionRequest{ToolName: "bash", Command: "rm -rf ../", Rule: "bash.prefix.rm"})
		if allowed {
			t.Fatalf("dangerous rm must never be auto-granted (reply=%s)", reply)
		}
		if !strings.Contains(reason, "dangerous rm") {
			t.Fatalf("expected the dangerous-rm refusal, got %q", reason)
		}
	}
}

// The chat judge has no category to attribute a deny to, so it gets the clause in
// its prompt (instruction-only parity) and nothing more. Capture the real prompt
// the gatekeeper sends rather than asserting on a helper.
func TestChatJudgePromptCarriesRelaxedSection(t *testing.T) {
	cfg := &config.Config{}
	cfg.Ocode.Permissions.Auto = &config.AutoPermissionConfig{
		Enabled:         true,
		Model:           "openai/gpt-4o-mini",
		RelaxedConcerns: []string{"network"},
	}
	prev := newClientFn
	t.Cleanup(func() { newClientFn = prev })
	cap := &chatJudgeCapture{}
	newClientFn = func(_ *config.Config, _ string) LLMClient { return cap }

	a := NewAgent(nil, nil, cfg, nil)
	allowed, reason, consulted := a.askPermissionModel("bash", json.RawMessage(`{"command":"curl https://example.com"}`), nil)
	if !allowed || !consulted {
		t.Fatalf("expected the captured allow verdict, got allowed=%v consulted=%v reason=%q", allowed, consulted, reason)
	}
	if !strings.Contains(cap.prompt, "Relaxed concern categories") {
		t.Fatalf("chat judge prompt lacks the relaxed section:\n%s", cap.prompt)
	}
	if !strings.Contains(cap.prompt, "network ("+typesafeConcernLabel("network")+")") {
		t.Fatalf("relaxed section should name the category and its label:\n%s", cap.prompt)
	}
}

// chatJudgeCapture records the gatekeeper prompt and replays an allow verdict.
type chatJudgeCapture struct{ prompt string }

func (c *chatJudgeCapture) Chat(messages []Message, _ []map[string]interface{}) (*Message, error) {
	if len(messages) > 0 {
		c.prompt = messages[0].Content
	}
	return &Message{Role: "assistant", Content: "ALLOW: read-only request"}, nil
}

func (c *chatJudgeCapture) GetProvider() string { return "mock" }
func (c *chatJudgeCapture) GetModel() string    { return "mock-model" }
