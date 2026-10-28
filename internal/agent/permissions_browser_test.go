package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/tool"
)

// TestPermissions_BrowserDefaultAsk pins the default rule: the browser tool
// asks, like computer and window, before anything it does.
func TestPermissions_BrowserDefaultAsk(t *testing.T) {
	pm := NewPermissionManager()
	if got := pm.Check("browser"); got != PermissionAsk {
		t.Fatalf("Check(\"browser\") = %s; want PermissionAsk", got)
	}
}

// TestPermissions_BrowserObserveActionsAllowed pins that the observe actions run
// without a prompt at the static layer: they read the page, its console and its
// requests, or move the viewport. Decide cannot see which page is targeted, so a
// read of a tab the agent did not open is asked about later, at consult time (see
// browserDecision and browser_read_gate_test.go).
func TestPermissions_BrowserObserveActionsAllowed(t *testing.T) {
	for _, action := range []string{"tabs", "screenshot", "text", "scroll", "snapshot", "find", "wait", "console", "network"} {
		t.Run(action, func(t *testing.T) {
			pm := NewPermissionManager()
			dec := pm.Decide("browser", json.RawMessage(`{"action":"`+action+`"}`))
			if dec.Level != PermissionAllow {
				t.Fatalf("action=%s: expected Allow, got %s", action, dec.Level)
			}
		})
	}
}

// TestPermissions_BrowserMutatingActionsAsk pins that every action which can
// change page state or send a request raises an ask with its own summary.
func TestPermissions_BrowserMutatingActionsAsk(t *testing.T) {
	cases := []struct {
		name string
		args string
		want string
	}{
		{"click", `{"action":"click","x":4,"y":9,"tab":"b1"}`, "click at 4,9 (tab b1)"},
		{"click target", `{"action":"click","target":"@e4"}`, `click "@e4"`},
		{"type", `{"action":"type","text":"hunter2"}`, "type 7 characters"},
		{"type target", `{"action":"type","target":"#q","text":"hunter2"}`, `type 7 characters into "#q"`},
		{"fill", `{"action":"fill","target":"#pw","value":"s3cret-pass"}`, `fill "#pw" (11 characters)`},
		{"check", `{"action":"check","target":"@e5"}`, `check "@e5"`},
		{"uncheck", `{"action":"uncheck","target":"@e5"}`, `uncheck "@e5"`},
		{"select", `{"action":"select","target":"#c","value":"CA"}`, `select "#c" (2 characters)`},
		{"pick", `{"action":"pick","target":"#cb","text":"Canada"}`, `pick "#cb" option "Canada"`},
		{"fill_form", `{"action":"fill_form","fields":[{"selector":"#a","value":"x"},{"selector":"#b","value":null}]}`, "fill_form (2 fields)"},
		{"fill_form file", `{"action":"fill_form","fields":[{"selector":"#f","value":["/work/a.pdf"]}]}`, "fill_form (1 fields), upload 1 file(s): /work/a.pdf (1 outside allowed roots)"},
		{"upload", `{"action":"upload","target":"#f","files":["/work/a.pdf"]}`, "upload 1 file(s): /work/a.pdf (1 outside allowed roots)"},
		{"key", `{"action":"key","key":"Enter"}`, "key Enter"},
		{"navigate", `{"action":"navigate","url":"https://example.com/"}`, "navigate https://example.com/"},
		{"open", `{"action":"open","url":"https://example.com/"}`, "open tab at https://example.com/"},
		{"open blank", `{"action":"open"}`, "open tab"},
		{"back", `{"action":"back"}`, "back"},
		{"forward", `{"action":"forward"}`, "forward"},
		{"reload", `{"action":"reload"}`, "reload"},
		{"unknown", `{"action":"eval"}`, "eval"},
		{"missing", `{}`, "browser"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pm := NewPermissionManager()
			dec := pm.Decide("browser", json.RawMessage(tc.args))
			if dec.Level != PermissionAsk || dec.Request == nil {
				t.Fatalf("expected Ask with a request, got %s", dec.Level)
			}
			if dec.Request.Rule != "tool.browser" {
				t.Errorf("rule = %q, want tool.browser", dec.Request.Rule)
			}
			if dec.Request.Command != tc.want {
				t.Errorf("command = %q, want %q", dec.Request.Command, tc.want)
			}
		})
	}
}

// TestPermissions_BrowserTypeSummaryHidesText pins that typed text, which may be
// a password, is never echoed into the permission dialog. The same holds for a
// fill value and a fill_form value.
func TestPermissions_BrowserTypeSummaryHidesText(t *testing.T) {
	for _, args := range []string{
		`{"action":"type","text":"s3cret-pass"}`,
		`{"action":"type","target":"#pw","text":"s3cret-pass"}`,
		`{"action":"fill","target":"#pw","value":"s3cret-pass"}`,
		`{"action":"select","target":"#pw","value":"s3cret-pass"}`,
		`{"action":"fill_form","fields":[{"selector":"#pw","value":"s3cret-pass"}]}`,
	} {
		pm := NewPermissionManager()
		dec := pm.Decide("browser", json.RawMessage(args))
		if dec.Request == nil || strings.Contains(dec.Request.Command, "s3cret") {
			t.Fatalf("summary for %s leaked the typed text: %+v", args, dec.Request)
		}
	}
}

// TestPermissions_BrowserDenyRuleOverridesObserve pins that a user deny on the
// tool is honoured even for the read-only actions.
func TestPermissions_BrowserDenyRuleOverridesObserve(t *testing.T) {
	pm := NewPermissionManager()
	pm.SetRule("browser", PermissionDeny)
	for _, action := range []string{"text", "snapshot", "find", "wait", "console", "network"} {
		if dec := pm.Decide("browser", json.RawMessage(`{"action":"`+action+`"}`)); dec.Level != PermissionDeny {
			t.Fatalf("observe action %s under deny = %s, want Deny", action, dec.Level)
		}
	}
	if dec := pm.Decide("browser", json.RawMessage(`{"action":"click","x":1,"y":1}`)); dec.Level != PermissionDeny {
		t.Fatalf("click under deny = %s, want Deny", dec.Level)
	}
}

// TestPermissions_BrowserAllowRuleSkipsAsk pins that an explicit allow rule
// lets mutating actions through without a prompt.
func TestPermissions_BrowserAllowRuleSkipsAsk(t *testing.T) {
	pm := NewPermissionManager()
	pm.SetRule("browser", PermissionAllow)
	if dec := pm.Decide("browser", json.RawMessage(`{"action":"reload"}`)); dec.Level != PermissionAllow {
		t.Fatalf("reload under allow = %s, want Allow", dec.Level)
	}
}

// TestFilterMainOnlyTools_KeepsBrowserForSubagents pins that subagents keep the
// browser tool: any agent may drive the browser, and asks still gate mutations.
func TestFilterMainOnlyTools_KeepsBrowserForSubagents(t *testing.T) {
	got := filterMainOnlyTools([]tool.Tool{namedTool("browser"), namedTool("read")})
	if len(got) != 2 {
		t.Fatalf("filterMainOnlyTools kept %d tools, want both browser and read", len(got))
	}
}

// namedTool is a no-op tool used only to exercise name-based filters.
type namedTool string

func (n namedTool) Name() string                                 { return string(n) }
func (n namedTool) Description() string                          { return "" }
func (n namedTool) Definition() map[string]interface{}           { return map[string]interface{}{} }
func (n namedTool) Execute(args json.RawMessage) (string, error) { return "", nil }
func (n namedTool) Parallel() bool                               { return true }
