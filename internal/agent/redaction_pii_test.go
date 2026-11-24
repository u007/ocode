package agent

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/notebus"
	"github.com/u007/ocode/internal/redact"
)

// fakeScanner is a redact.Scanner that finds nothing.
type fakeScanner struct{}

func (fakeScanner) Scan(string) ([]redact.Span, error) { return nil, nil }

// TestPermissionJudgePayloadKeepsRawPII pins the carve-out on the exact
// helpers the permission judge uses: redactText is the arguments path and
// redactFileText is the project-context path in permission_typesafe.go. They
// take a registry, not an agent, so no PII categories can reach them: a judge
// payload must carry the raw email while a credential in it is masked.
func TestPermissionJudgePayloadKeepsRawPII(t *testing.T) {
	reg := redact.NewRegistry("judgetest")
	payload := `{"email":"jane@example.com","aws":"AKIAIOSFODNN7EXAMPLE"}`

	for name, fn := range map[string]func(string, *redact.Registry) string{
		"redactText":     redactText,
		"redactFileText": redactFileText,
	} {
		out := fn(payload, reg)
		if !strings.Contains(out, "jane@example.com") {
			t.Errorf("%s masked PII in a judge payload: %q", name, out)
		}
		if strings.Contains(out, "AKIAIOSFODNN7EXAMPLE") {
			t.Errorf("%s let an AWS key through a judge payload: %q", name, out)
		}
	}
}

// TestSafetyNetAppliesPIIOnlyWhenConfigured proves the egress safety net is
// secrets-only unless GenericClient.PIICategories is set.
func TestSafetyNetAppliesPIIOnlyWhenConfigured(t *testing.T) {
	reg := redact.NewRegistry("net")
	c := &GenericClient{Redaction: &redact.NetHook{Registry: reg, Enabled: true}}
	raw := "contact jane@example.com please"
	msgs := []Message{{Role: "user", Content: raw}}

	out, err := c.applyRedactionSafetyNet(msgs)
	if err != nil {
		t.Fatalf("safety net (no PII): %v", err)
	}
	if !strings.Contains(out[0].Content, "jane@example.com") {
		t.Fatalf("email masked with no PII categories configured: %q", out[0].Content)
	}

	c.PIICategories = []redact.PIICategory{redact.PIIContact}
	out, err = c.applyRedactionSafetyNet(msgs)
	if err != nil {
		t.Fatalf("safety net (contact): %v", err)
	}
	if strings.Contains(out[0].Content, "jane@example.com") {
		t.Fatalf("email survived the PII safety net: %q", out[0].Content)
	}
	if !strings.Contains(out[0].Content, "OCSEC") {
		t.Errorf("email was not replaced by a vault token: %q", out[0].Content)
	}
}

// TestSafetyNetIsIdempotent: re-running the net over already-tokenized
// messages changes nothing, and the same input yields byte-identical output
// across calls (the cached prefix stays stable).
func TestSafetyNetIsIdempotent(t *testing.T) {
	reg := redact.NewRegistry("idem")
	c := &GenericClient{
		Redaction:     &redact.NetHook{Registry: reg, Enabled: true},
		PIICategories: []redact.PIICategory{redact.PIIContact},
	}
	msgs := []Message{
		{Role: "system", Content: "reach jane@example.com"},
		{Role: "user", Content: "again jane@example.com"},
	}
	first, err := c.applyRedactionSafetyNet(msgs)
	if err != nil {
		t.Fatalf("first pass: %v", err)
	}
	if strings.Contains(first[0].Content, "jane@example.com") {
		t.Fatalf("first pass left the email: %q", first[0].Content)
	}

	second, err := c.applyRedactionSafetyNet(first)
	if err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Errorf("second pass changed tokenized messages:\nfirst  = %#v\nsecond = %#v", first, second)
	}

	again, err := c.applyRedactionSafetyNet(msgs)
	if err != nil {
		t.Fatalf("repeat pass: %v", err)
	}
	if !reflect.DeepEqual(first, again) {
		t.Errorf("same input produced different output across calls:\nfirst = %#v\nagain = %#v", first, again)
	}
}

// TestScanToolResultPropagatesPIIError forces a PII detector failure and
// requires the error to surface with no unmasked content, on both the
// sensitive-read branch and the generic branch.
func TestScanToolResultPropagatesPIIError(t *testing.T) {
	orig := piiRedactForToolResult
	piiRedactForToolResult = func(string, *redact.Registry, []redact.PIICategory) (string, error) {
		return "", errors.New("pii detector failed")
	}
	defer func() { piiRedactForToolResult = orig }()

	a := &Agent{
		redactionEnabled:  true,
		redactionRegistry: redact.NewRegistry("toolerr"),
		redactionScanner:  fakeScanner{},
		redactionPII:      []redact.PIICategory{redact.PIIContact},
	}
	const raw = "jane@example.com"

	cases := map[string]struct{ tool, args string }{
		"generic":        {tool: "read", args: `{"path":"notes.txt"}`},
		"sensitive-read": {tool: "read", args: `{"path":".env"}`},
	}
	for name, tc := range cases {
		out, err := a.scanToolResult(tc.tool, tc.args, raw)
		if err == nil {
			t.Errorf("%s: PII failure was swallowed", name)
		}
		if strings.Contains(out, raw) {
			t.Errorf("%s: returned unmasked content after a PII failure: %q", name, out)
		}
	}
}

// TestChildInheritsPIIAndSideClientsDoNot: a side-query child carries the
// parent's PII categories and hook, while a bindSideClient product stays
// secrets-only.
func TestChildInheritsPIIAndSideClientsDoNot(t *testing.T) {
	reg := redact.NewRegistry("inherit")
	hook := redact.NetHookEnabled(reg)
	cats := []redact.PIICategory{redact.PIIContact}

	main := NewAgent(&MockClient{}, nil, nil, nil)
	gc := &GenericClient{}
	main.client = gc
	main.SetRedactionHook(hook)
	main.SetRedactionPIICategories(cats)
	if len(gc.PIICategories) != 1 || gc.PIICategories[0] != redact.PIIContact {
		t.Fatalf("main client PIICategories = %v, want %v", gc.PIICategories, cats)
	}

	child, err := main.newSideQueryAgent(AskLoopOptions{Client: &GenericClient{}})
	if err != nil {
		t.Fatalf("newSideQueryAgent: %v", err)
	}
	defer child.shutdownTransient()
	cgc, ok := child.client.(*GenericClient)
	if !ok {
		t.Fatalf("child client is %T, want *GenericClient", child.client)
	}
	if len(cgc.PIICategories) != 1 || cgc.PIICategories[0] != redact.PIIContact {
		t.Errorf("child client PIICategories = %v, want inherited %v", cgc.PIICategories, cats)
	}
	if cgc.Redaction != hook {
		t.Errorf("child client redaction hook = %v, want inherited hook", cgc.Redaction)
	}

	side, ok := main.bindSideClient(&GenericClient{}).(*GenericClient)
	if !ok {
		t.Fatalf("bindSideClient did not return *GenericClient")
	}
	if side.Redaction != hook {
		t.Errorf("side client lost the tier-1 hook")
	}
	if len(side.PIICategories) != 0 {
		t.Errorf("side client must stay secrets-only, got PII categories %v", side.PIICategories)
	}
}

// startNoteBusGroup builds a real bus through maybeBuildGroupBus so the test
// exercises the production wiring (not just a detached closure) and registers
// the production teardown.
func startNoteBusGroup(t *testing.T, a *Agent) *notebus.Bus {
	t.Helper()
	a.noteBusFactory = func(id string) *notebus.Bus { return notebus.NewBus(id) }
	calls := make([]ToolCall, 2)
	for i := range calls {
		calls[i].ID = fmt.Sprintf("call-%d", i)
		calls[i].Function.Name = "task"
		calls[i].Function.Arguments = `{"prompt":"x","shared_notes":true}`
	}
	bus, ids := a.maybeBuildGroupBus(calls, []int{0, 1})
	if bus == nil || len(ids) != 2 {
		t.Fatalf("maybeBuildGroupBus: bus=%v ids=%v", bus, ids)
	}
	t.Cleanup(func() { a.teardownGroupBus(bus) })
	return bus
}

// TestNoteBusRedactor pins the closure wired onto every group bus: a PII
// detection failure fails CLOSED (marker body, never the raw text), and the
// categories are niled out while redaction is disabled.
func TestNoteBusRedactor(t *testing.T) {
	orig := noteBusRedactBodyWith
	t.Cleanup(func() { noteBusRedactBodyWith = orig })

	const marker = "[REDACTED:pii-error]"
	const body = "jane@example.com"

	t.Run("PII failure fails closed", func(t *testing.T) {
		noteBusRedactBodyWith = func(string, []redact.PIICategory) (string, error) {
			return "", errors.New("pii detector down")
		}
		a := &Agent{
			redactionEnabled: true,
			redactionPII:     []redact.PIICategory{redact.PIIContact},
		}
		bus := startNoteBusGroup(t, a)

		if _, err := bus.Append(notebus.Note(0, "a1", "x.go", body, 0)); err != nil {
			t.Fatalf("Append: %v", err)
		}
		snap := bus.Snapshot()
		if len(snap) != 1 {
			t.Fatalf("snapshot entries = %d, want 1", len(snap))
		}
		if snap[0].Body != marker {
			t.Errorf("body = %q, want the fail-closed marker %q", snap[0].Body, marker)
		}
		if strings.Contains(snap[0].Body, body) {
			t.Errorf("raw body reached the bus after a PII failure: %q", snap[0].Body)
		}
	})

	t.Run("disabled redaction passes nil categories and the body through", func(t *testing.T) {
		var gotCats []redact.PIICategory
		noteBusRedactBodyWith = func(text string, cats []redact.PIICategory) (string, error) {
			gotCats = cats
			return text, nil
		}
		a := &Agent{
			redactionEnabled: false,
			redactionPII:     []redact.PIICategory{redact.PIIContact},
		}
		bus := startNoteBusGroup(t, a)

		if _, err := bus.Append(notebus.Note(0, "a1", "x.go", body, 0)); err != nil {
			t.Fatalf("Append: %v", err)
		}
		if gotCats != nil {
			t.Errorf("categories = %v, want nil while redaction is disabled", gotCats)
		}
		snap := bus.Snapshot()
		if len(snap) != 1 || snap[0].Body != body {
			t.Errorf("body = %v, want unchanged %q", snap, body)
		}
	})

	t.Run("enabled redaction forwards the agent's categories", func(t *testing.T) {
		var gotCats []redact.PIICategory
		noteBusRedactBodyWith = func(text string, cats []redact.PIICategory) (string, error) {
			gotCats = cats
			return text, nil
		}
		a := &Agent{
			redactionEnabled: true,
			redactionPII:     []redact.PIICategory{redact.PIIContact},
		}
		bus := startNoteBusGroup(t, a)

		if _, err := bus.Append(notebus.Note(0, "a1", "x.go", body, 0)); err != nil {
			t.Fatalf("Append: %v", err)
		}
		if len(gotCats) != 1 || gotCats[0] != redact.PIIContact {
			t.Errorf("categories = %v, want %v", gotCats, a.redactionPII)
		}
	})
}

// TestApplySpecModelReappliesPII: applySpecModel swaps the client through
// bindSideClient, which attaches only the tier-1 hook; the PII categories must
// be re-applied onto the new *GenericClient.
func TestApplySpecModelReappliesPII(t *testing.T) {
	prev := newClientFn
	t.Cleanup(func() { newClientFn = prev })
	swapped := &GenericClient{Model: "openai/gpt-4o", Provider: "openai"}
	newClientFn = func(*config.Config, string) LLMClient { return swapped }

	hook := redact.NetHookEnabled(redact.NewRegistry("spec"))
	cats := []redact.PIICategory{redact.PIIContact}
	a := NewAgent(&MockClient{}, nil, &config.Config{}, nil)
	a.SetRedactionHook(hook)
	a.SetRedactionPIICategories(cats)

	a.applySpecModel(&AgentSpec{Name: "spec-test", Model: "openai/gpt-4o"})

	got, ok := a.client.(*GenericClient)
	if !ok || got != swapped {
		t.Fatalf("client = %T, want the swapped *GenericClient", a.client)
	}
	if len(got.PIICategories) != 1 || got.PIICategories[0] != redact.PIIContact {
		t.Errorf("swapped client PIICategories = %v, want %v", got.PIICategories, cats)
	}
	if got.Redaction != hook {
		t.Errorf("swapped client redaction hook = %v, want the tier-1 hook", got.Redaction)
	}
}
