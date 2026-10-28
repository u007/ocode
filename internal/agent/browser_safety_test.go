package agent

import (
	"encoding/json"
	"testing"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/tool"
)

// pageTool is a fake browser tool whose page context is fixed by the test.
type pageTool struct {
	page map[string]any
	ok   bool
}

func (p *pageTool) Name() string        { return "browser" }
func (p *pageTool) Description() string { return "fake" }
func (p *pageTool) Parallel() bool      { return false }
func (p *pageTool) Definition() map[string]interface{} {
	return map[string]interface{}{"name": "browser"}
}
func (p *pageTool) Execute(json.RawMessage) (string, error) { return "", nil }
func (p *pageTool) PageContext(json.RawMessage) (map[string]any, bool) {
	if !p.ok {
		return nil, false
	}
	out := map[string]any{}
	for k, v := range p.page {
		out[k] = v
	}
	return out, true
}

var _ tool.PageContextTool = (*pageTool)(nil)

// TestJudgeStateCarriesTargetPage pins that the auto-permission judge is told
// which page a browser action acts on: its URL, host and whether this agent
// opened it. Without that a click on the user's page would be judged blind.
func TestJudgeStateCarriesTargetPage(t *testing.T) {
	pt := &pageTool{ok: true, page: map[string]any{
		"targets_page": true, "tab_id": "b1", "opened_by_agent": false,
		"page_url": "https://bank.example/account", "page_host": "bank.example",
	}}
	ag := newTestAgent(nil, []tool.Tool{pt}, &config.Config{}, nil)
	state := ag.buildTypesafePermissionState("browser", json.RawMessage(`{"action":"click","tab":"b1","x":1,"y":2}`), nil)
	page, ok := state["browser_page"].(map[string]any)
	if !ok {
		t.Fatalf("judge state has no browser_page: %v", state)
	}
	if page["page_host"] != "bank.example" || page["page_url"] != "https://bank.example/account" ||
		page["opened_by_agent"] != false || page["resolved"] != true {
		t.Fatalf("browser_page = %v", page)
	}
}

// TestJudgeNotAskedAboutUnresolvedPage pins fail-closed: when the target page
// cannot be resolved the judge is not consulted at all, so a person decides.
func TestJudgeNotAskedAboutUnresolvedPage(t *testing.T) {
	ag := newTestAgent(nil, []tool.Tool{&pageTool{ok: false}}, &config.Config{}, nil)
	allowed, _, _, consulted := ag.consultPermissionModel("browser", json.RawMessage(`{"action":"click","x":1,"y":2}`), nil)
	if allowed || consulted {
		t.Fatalf("unresolved page consulted the judge: allowed=%v consulted=%v", allowed, consulted)
	}
}

// TestBrowserURLDenyReason pins the outright refusals: non-web schemes and
// link-local/metadata addresses. Loopback and LAN are deliberately not here.
func TestBrowserURLDenyReason(t *testing.T) {
	deny := []string{
		"file:///etc/passwd", "chrome://settings", "javascript:alert(1)",
		"data:text/html,hi", "chrome-extension://abc/x.html",
		"http://169.254.169.254/latest/meta-data/", "http://[fe80::1]/",
		"http://metadata.google.internal/computeMetadata/v1/",
	}
	for _, u := range deny {
		if tool.BrowserURLDenyReason(u) == "" {
			t.Errorf("tool.BrowserURLDenyReason(%q) allowed; want a refusal", u)
		}
	}
	for _, u := range []string{"https://example.com/", "http://localhost:3000/", "http://192.168.1.10/", ""} {
		if r := tool.BrowserURLDenyReason(u); r != "" {
			t.Errorf("tool.BrowserURLDenyReason(%q) = %q; want allowed", u, r)
		}
	}
}

// encodedMetadataURLs are the metadata address and link-local space in every
// form the OS resolver accepts. Each must be refused whatever the permission mode.
var encodedMetadataURLs = []string{
	"http://169.254.169.254/latest/meta-data/",
	"http://2852039166/",
	"http://0xa9fea9fe/",
	"http://0XA9FEA9FE/",
	"http://0251.0376.0251.0376/",
	"http://169.254.43518/",
	"http://169.16689662/",
	"http://[::ffff:169.254.169.254]/",
	"http://[::ffff:a9fe:a9fe]/",
	"http://169.254.169.254./",
	"http://[fe80::1]/",
	"http://100.100.100.200/",
	"http://[fd00:ec2::254]/",
	"http://metadata.google.internal./",
	"http://Metadata.Google.Internal/",
}

// TestDecideRefusesEncodedMetadataURLsInEveryMode pins that no permission mode
// opens a metadata URL. The refusal is HardDeny, so no judge can overturn it.
func TestDecideRefusesEncodedMetadataURLsInEveryMode(t *testing.T) {
	for _, mode := range []PermissionMode{PermissionModeYOLO, PermissionModeLocked, PermissionModeNormal} {
		pm := NewPermissionManager()
		pm.SetMode(mode)
		for _, u := range encodedMetadataURLs {
			for _, action := range []string{"navigate", "open"} {
				args := json.RawMessage(`{"action":"` + action + `","url":"` + u + `"}`)
				dec := pm.Decide("browser", args)
				if dec.Level != PermissionDeny || !dec.HardDeny || dec.DenyReason == "" {
					t.Errorf("mode=%s %s %s = %s hard=%t reason=%q; want a hard deny with a reason",
						mode, action, u, dec.Level, dec.HardDeny, dec.DenyReason)
				}
			}
		}
	}
}

// TestDecideAllowsHTTPSNavigationByMode pins that an https URL is not refused by
// the URL check in any mode. Locked denies every mutating browser action for its
// own reason, so there the test only checks that the refusal is not the URL one.
func TestDecideAllowsHTTPSNavigationByMode(t *testing.T) {
	args := json.RawMessage(`{"action":"navigate","url":"https://example.com/"}`)
	yolo := NewPermissionManager()
	yolo.SetMode(PermissionModeYOLO)
	if dec := yolo.Decide("browser", args); dec.Level != PermissionAllow {
		t.Errorf("yolo https navigate = %s %q; want allow", dec.Level, dec.DenyReason)
	}
	normal := NewPermissionManager()
	normal.SetMode(PermissionModeNormal)
	if dec := normal.Decide("browser", args); dec.Level != PermissionAsk {
		t.Errorf("normal https navigate = %s %q; want ask", dec.Level, dec.DenyReason)
	}
	locked := NewPermissionManager()
	locked.SetMode(PermissionModeLocked)
	if dec := locked.Decide("browser", args); dec.Level == PermissionDeny && dec.HardDeny {
		t.Errorf("locked https navigate hard-denied by the URL check: %q", dec.DenyReason)
	}
}

// TestDecideRefusesBadBrowserURL pins that navigate and open with a refused URL
// are denied by the permission layer before any ask is raised.
func TestDecideRefusesBadBrowserURL(t *testing.T) {
	pm := NewPermissionManager()
	for _, args := range []string{
		`{"action":"navigate","url":"file:///etc/passwd"}`,
		`{"action":"open","url":"javascript:alert(1)"}`,
	} {
		dec := pm.Decide("browser", json.RawMessage(args))
		if dec.Level != PermissionDeny || dec.DenyReason == "" {
			t.Errorf("Decide(%s) = %s %q; want an outright deny", args, dec.Level, dec.DenyReason)
		}
	}
	if dec := pm.Decide("browser", json.RawMessage(`{"action":"navigate","url":"https://example.com/"}`)); dec.Level == PermissionDeny {
		t.Errorf("https navigate denied: %q", dec.DenyReason)
	}
}

// TestBrowserNavigateIsAnEgressTarget pins that navigate and open are judged by
// the network guard as egress, with the host and loopback flag set the way the
// webfetch path sets them. Other browser actions are not egress.
func TestBrowserNavigateIsAnEgressTarget(t *testing.T) {
	got := extractEgressTargets("browser", json.RawMessage(`{"action":"navigate","url":"https://example.com/a"}`))
	if len(got) != 1 || got[0].Host != "example.com" || got[0].Loopback {
		t.Fatalf("public navigate targets = %+v", got)
	}
	local := extractEgressTargets("browser", json.RawMessage(`{"action":"open","url":"http://localhost:3000/"}`))
	if len(local) != 1 || !local[0].Loopback {
		t.Fatalf("localhost open targets = %+v; want loopback", local)
	}
	if n := extractEgressTargets("browser", json.RawMessage(`{"action":"click","x":1,"y":2}`)); n != nil {
		t.Fatalf("click produced egress targets: %+v", n)
	}
}
