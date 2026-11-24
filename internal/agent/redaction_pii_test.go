package agent

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/redact"
)

// fakeScanner is a redact.Scanner that finds nothing.
type fakeScanner struct{}

func (fakeScanner) Scan(string) ([]redact.Span, error) { return nil, nil }

// TestPermissionJudgePayloadKeepsRawPII pins the carve-out on the exact
// helpers the permission judge uses: redactText is the arguments path and
// redactFileText is the project-context path in permission_typesafe.go. With
// PII configured on the agent, a judge payload must still carry the raw email
// while a credential in the same payload is masked.
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
