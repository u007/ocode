package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/u007/ocode/internal/browse"
	"github.com/u007/ocode/internal/browse/cdp"
)

// htrStub is a stand-in for the htrcli daemon. answer decides each command's
// result: a non-empty error text makes the command fail the way the daemon does.
type htrStub struct {
	tabs   []htrTab
	page   htrPage
	png    []byte
	answer func(cmd htrCommand) (any, string)
	// events answers GET /api/events by kind: the whole envelope data of each.
	events map[string]any

	mu           sync.Mutex
	commands     []htrRecorded
	eventQueries []string
}

// htrRecorded is one request the stub received as a command.
type htrRecorded struct {
	Path    string
	Command htrCommand
	Browser string
	Strict  bool
}

func (st *htrStub) recorded() []htrRecorded {
	st.mu.Lock()
	defer st.mu.Unlock()
	return append([]htrRecorded(nil), st.commands...)
}

// queries returns the raw query of each events read the stub received.
func (st *htrStub) queries() []string {
	st.mu.Lock()
	defer st.mu.Unlock()
	return append([]string(nil), st.eventQueries...)
}

func (st *htrStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer "+htrTestToken {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("forbidden"))
		return
	}
	envelope := func(data any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "data": data})
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/tabs":
		envelope(st.tabs)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/tabs/"):
		id, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/api/tabs/"))
		for _, t := range st.tabs {
			if t.ID == id {
				envelope(t)
				return
			}
		}
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "tab not found"})
	case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/tabs/") && strings.HasSuffix(r.URL.Path, "/command"):
		var body struct {
			Command htrCommand `json:"command"`
			Browser string     `json:"browser"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		st.mu.Lock()
		st.commands = append(st.commands, htrRecorded{Path: r.URL.Path, Command: body.Command, Browser: body.Browser})
		st.mu.Unlock()
		st.reply(envelope, body.Command)
	case r.Method == http.MethodPost && r.URL.Path == "/api/background/command":
		var body struct {
			Command       htrCommand `json:"command"`
			Browser       string     `json:"browser"`
			BrowserStrict bool       `json:"browserStrict"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		st.mu.Lock()
		st.commands = append(st.commands, htrRecorded{Path: r.URL.Path, Command: body.Command, Browser: body.Browser, Strict: body.BrowserStrict})
		st.mu.Unlock()
		st.reply(envelope, body.Command)
	case r.Method == http.MethodGet && r.URL.Path == "/api/page":
		envelope(st.page)
	case r.Method == http.MethodGet && r.URL.Path == "/api/screenshot":
		envelope(base64.StdEncoding.EncodeToString(st.png))
	case r.Method == http.MethodGet && r.URL.Path == "/api/events":
		st.mu.Lock()
		st.eventQueries = append(st.eventQueries, r.URL.RawQuery)
		st.mu.Unlock()
		data, ok := st.events[r.URL.Query().Get("kind")]
		if !ok {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "unknown event kind"})
			return
		}
		envelope(data)
	default:
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "not found"})
	}
}

// reply answers one command with the daemon's CommandResult shape.
func (st *htrStub) reply(envelope func(any), cmd htrCommand) {
	var data any
	errText := ""
	if st.answer != nil {
		data, errText = st.answer(cmd)
	}
	envelope(map[string]any{"id": cmd.ID, "success": errText == "", "data": data, "error": errText})
}

// newHTRTestSurfaces serves stub and returns the surfaces that talk to it.
func newHTRTestSurfaces(t *testing.T, stub *htrStub) htrAgentSurfaces {
	t.Helper()
	srv := httptest.NewServer(stub)
	t.Cleanup(srv.Close)
	return newHTRAgentSurfaces(srv.URL, htrTestToken)
}

// htrElementJSON is one findAll or find element.
func htrElementJSON(ref, tag, xpath string) map[string]any {
	return map[string]any{"ref": ref, "tag": tag, "xpath": xpath, "selector": xpath, "text": tag + " text"}
}

// htrPageTree is a snapshot with a dialog holding a textbox, a disabled button and
// a checkbox, plus a link the identity listing does not contain.
func htrPageTree(refs [3]string) map[string]any {
	return map[string]any{
		"role": "document", "name": "Sign in", "viewport": map[string]any{"w": 1280, "h": 800}, "scroll": map[string]any{"x": 0, "y": 0},
		"children": []any{
			map[string]any{"role": "dialog", "name": "Sign in", "children": []any{
				map[string]any{"role": "textbox", "name": "Email", "ref": refs[0], "value": "a@b.c", "state": map[string]any{}},
				map[string]any{"role": "button", "name": "Go", "ref": refs[1], "state": map[string]any{"disabled": true}},
				map[string]any{"role": "checkbox", "name": "Remember", "ref": refs[2], "state": map[string]any{"checked": true}},
			}},
			map[string]any{"role": "link", "name": "Home", "ref": "@e9"},
		},
	}
}

// htrPageAnswer serves the page tree and identity listing for the refs given.
func htrPageAnswer(refs [3]string) func(cmd htrCommand) (any, string) {
	return func(cmd htrCommand) (any, string) {
		switch cmd.Action {
		case "snapshot":
			return htrPageTree(refs), ""
		case "findAll":
			return []any{
				htrElementJSON(refs[0], "input", "/html/body/dialog/input"),
				htrElementJSON(refs[1], "button", "/html/body/dialog/button"),
				htrElementJSON(refs[2], "input", "/html/body/dialog/input[2]"),
			}, ""
		case "find":
			switch cmd.Target.Ref {
			case refs[0]:
				return map[string]any{"ref": refs[0], "tag": "input", "xpath": "/html/body/dialog/input", "value": "a@b.c"}, ""
			case refs[1]:
				return htrElementJSON(refs[1], "button", "/html/body/dialog/button"), ""
			case refs[2]:
				return htrElementJSON(refs[2], "input", "/html/body/dialog/input[2]"), ""
			}
			return nil, "stale ref: " + cmd.Target.Ref + " is not known on this page"
		case "getText":
			return "Sign in to continue", ""
		case "isChecked":
			// The page tree's checkbox stays checked, so every read-back says true.
			return map[string]any{"checked": true}, ""
		case "click", "fill", "check", "uncheck", "clear", "select", "type":
			return nil, ""
		}
		return nil, "unexpected action " + cmd.Action
	}
}

func TestHTRSurface_TabKeysOpenCloseAndUnknownTab(t *testing.T) {
	stub := &htrStub{
		tabs: []htrTab{{ID: 3, URL: "https://a.example/", Title: "A", Browser: "firefox"}, {ID: 7, URL: "https://b.example/", Browser: "chrome"}},
		answer: func(cmd htrCommand) (any, string) {
			if cmd.Action == "openTab" {
				return map[string]any{"tabId": 41, "browser": "firefox"}, ""
			}
			if cmd.Action == "networkCapture" {
				return map[string]any{"armed": true, "durationMs": 120000, "transport": "cdp"}, ""
			}
			return nil, ""
		},
	}
	h := newHTRTestSurfaces(t, stub)
	ctx := context.Background()

	surf, note, err := h.Surface(ctx, "tab:7", "https://ignored.example/")
	if err != nil || note != "" || surf == nil {
		t.Fatalf("Surface(tab:7) = (%v, %q, %v); want a surface, no note", surf, note, err)
	}
	if got := surf.(htrSurface).browser; got != "chrome" {
		t.Fatalf("surface browser = %q, want the tab's own browser chrome", got)
	}

	_, _, err = h.Surface(ctx, "tab:99", "")
	if err == nil || !errors.Is(err, cdp.ErrNoTarget) || !strings.Contains(err.Error(), "client tabs: 3, 7") {
		t.Fatalf("Surface(unknown) error = %v; want ErrNoTarget naming the client tabs 3, 7", err)
	}

	if _, _, err := h.Surface(ctx, "tab:abc", ""); err == nil || !strings.Contains(err.Error(), "not a client browser tab id") {
		t.Fatalf("Surface(non-numeric key) error = %v", err)
	}

	key, err := h.Open(ctx, "https://c.example/")
	if err != nil || key != "tab:41" {
		t.Fatalf("Open = (%q, %v); want tab:41", key, err)
	}
	// Surface(tab:7) armed the tab's network capture first, so the open is the last command.
	recs := stub.recorded()
	opened := recs[len(recs)-1]
	if opened.Path != "/api/background/command" || opened.Command.Action != "openTab" || opened.Command.Options["url"] != "https://c.example/" || opened.Strict {
		t.Fatalf("openTab request = %+v; want the background route, not strict", opened)
	}

	if err := h.Close(ctx, "tab:41"); err == nil || !strings.Contains(err.Error(), "not") {
		// tab 41 is not in the stub's list, so the close must report it gone.
		t.Fatalf("Close of a tab the daemon does not list = %v; want an error wrapping ErrNoTarget", err)
	}
	stub.tabs = append(stub.tabs, htrTab{ID: 41, Browser: "firefox"})
	if err := h.Close(ctx, "tab:41"); err != nil {
		t.Fatalf("Close(tab:41) = %v", err)
	}
	closed := stub.recorded()
	last := closed[len(closed)-1]
	if last.Command.Action != "closeTab" || last.Browser != "firefox" || !last.Strict {
		t.Fatalf("closeTab request = %+v; want strict routing to the tab's browser", last)
	}
}

func TestHTRSurface_UnreachableDaemonNamesTheForward(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	base := srv.URL
	srv.Close()
	h := newHTRAgentSurfaces(base, htrTestToken)
	_, _, err := h.Surface(context.Background(), "tab:7", "")
	if err == nil || !strings.Contains(err.Error(), "not reachable") || !strings.Contains(err.Error(), base) {
		t.Fatalf("unreachable daemon error = %v; want it to name the daemon address and the forward", err)
	}
}

func TestHTRSurface_BadTokenIsNamed(t *testing.T) {
	stub := &htrStub{tabs: []htrTab{{ID: 7, Browser: "firefox"}}}
	srv := httptest.NewServer(stub)
	t.Cleanup(srv.Close)
	h := newHTRAgentSurfaces(srv.URL, "wrong")
	_, _, err := h.Surface(context.Background(), "tab:7", "")
	if err == nil || !strings.Contains(err.Error(), "refused the bearer token") {
		t.Fatalf("bad token error = %v", err)
	}
}

func TestHTRSurface_ControlsRolesOperationsAndOmitted(t *testing.T) {
	refs := [3]string{"@e1", "@e2", "@e3"}
	stub := &htrStub{
		tabs:   []htrTab{{ID: 7, Browser: "firefox"}},
		page:   htrPage{URL: "https://login.example/", Title: "Sign in", ReadyState: "complete"},
		answer: htrPageAnswer(refs),
	}
	surf := htrSurface{h: newHTRTestSurfaces(t, stub), tab: 7, browser: "firefox"}
	table := &cdp.RefTable{}
	page, err := surf.Controls(context.Background(), table)
	if err != nil {
		t.Fatalf("Controls: %v", err)
	}
	if len(page.Controls) != 3 || page.Omitted != 0 {
		t.Fatalf("controls = %d (omitted %d), want 3 with none omitted: %+v", len(page.Controls), page.Omitted, page.Controls)
	}
	byRole := map[string]cdp.Control{}
	for _, c := range page.Controls {
		byRole[c.Role] = c
		if _, err := table.Lookup(c.Ref); err != nil {
			t.Fatalf("control %s ref %s is not in the agent's table: %v", c.Role, c.Ref, err)
		}
	}
	if got := byRole["textbox"]; strings.Join(got.Operations, ",") != "click,fill" || got.Value != "a@b.c" || got.Context != "Sign in" {
		t.Fatalf("textbox control = %+v", got)
	}
	if got := byRole["button"]; len(got.Operations) != 0 || got.States[0] != "disabled" {
		t.Fatalf("disabled button must offer no operations: %+v", got)
	}
	if got := byRole["checkbox"]; strings.Join(got.Operations, ",") != "check,uncheck" || strings.Join(got.States, ",") != "checked" {
		t.Fatalf("checkbox control = %+v", got)
	}
	if page.Settled != true || page.Fingerprint == "" || page.Title != "Sign in" {
		t.Fatalf("page metadata = settled %v fingerprint %q title %q", page.Settled, page.Fingerprint, page.Title)
	}

	// The same page under renumbered daemon refs keeps its fingerprint.
	stub.answer = htrPageAnswer([3]string{"@e41", "@e42", "@e43"})
	again, err := surf.Controls(context.Background(), &cdp.RefTable{})
	if err != nil {
		t.Fatalf("Controls after renumbering: %v", err)
	}
	if again.Fingerprint != page.Fingerprint {
		t.Fatalf("fingerprint changed on renumbered refs: %s vs %s", page.Fingerprint, again.Fingerprint)
	}
}

func TestHTRSurface_ControlsCapsAndCountsOmitted(t *testing.T) {
	const total = cdp.ControlsMaxElements + 10
	var kids []any
	var finds []any
	for i := 1; i <= total; i++ {
		ref := "@e" + strconv.Itoa(i)
		kids = append(kids, map[string]any{"role": "button", "name": "B" + ref, "ref": ref})
		finds = append(finds, htrElementJSON(ref, "button", "/html/body/button["+strconv.Itoa(i)+"]"))
	}
	stub := &htrStub{
		tabs: []htrTab{{ID: 7, Browser: "firefox"}},
		page: htrPage{URL: "https://many.example/", ReadyState: "loading"},
		answer: func(cmd htrCommand) (any, string) {
			switch cmd.Action {
			case "snapshot":
				return map[string]any{"role": "document", "viewport": map[string]any{"w": 1280, "h": 800}, "scroll": map[string]any{"x": 0, "y": 0}, "children": kids}, ""
			case "findAll":
				return finds, ""
			case "getText":
				return "", ""
			}
			return nil, ""
		},
	}
	surf := htrSurface{h: newHTRTestSurfaces(t, stub), tab: 7, browser: "firefox"}
	page, err := surf.Controls(context.Background(), &cdp.RefTable{})
	if err != nil {
		t.Fatalf("Controls: %v", err)
	}
	if len(page.Controls) != cdp.ControlsMaxElements || page.Omitted != 10 {
		t.Fatalf("kept %d, omitted %d; want %d kept and 10 omitted", len(page.Controls), page.Omitted, cdp.ControlsMaxElements)
	}
	if page.Settled {
		t.Fatal("a page still loading must not report settled")
	}
}

func TestHTRSurface_ClickByRefSendsTheDaemonRef(t *testing.T) {
	refs := [3]string{"@e1", "@e2", "@e3"}
	stub := &htrStub{tabs: []htrTab{{ID: 7, Browser: "firefox"}}, page: htrPage{URL: "https://login.example/"}, answer: htrPageAnswer(refs)}
	surf := htrSurface{h: newHTRTestSurfaces(t, stub), tab: 7, browser: "firefox"}
	table := &cdp.RefTable{}
	page, err := surf.Controls(context.Background(), table)
	if err != nil {
		t.Fatalf("Controls: %v", err)
	}
	var goRef string
	for _, c := range page.Controls {
		if c.Role == "checkbox" {
			goRef = c.Ref
		}
	}
	msg, err := surf.ClickElement(context.Background(), "", goRef, table)
	if err != nil || msg != "clicked "+goRef {
		t.Fatalf("ClickElement = (%q, %v)", msg, err)
	}
	got := stub.recorded()
	last := got[len(got)-1]
	if last.Path != "/api/tabs/7/command" || last.Command.Action != "click" || last.Command.Target == nil || last.Command.Target.Ref != "@e3" || last.Browser != "firefox" {
		t.Fatalf("click request = %+v; want tab 7, click, target ref @e3 (the daemon's ref)", last)
	}
}

func TestHTRSurface_StaleRefIsRefusedBeforeAnyAction(t *testing.T) {
	refs := [3]string{"@e1", "@e2", "@e3"}
	cases := []struct {
		name     string
		findTag  string
		findPath string
		findErr  string
	}{
		{name: "different element at the same daemon ref", findTag: "a", findPath: "/html/body/a[1]"},
		{name: "daemon no longer knows the ref", findErr: "stale ref: @e3 is not known on this page"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			answer := htrPageAnswer(refs)
			stub := &htrStub{tabs: []htrTab{{ID: 7, Browser: "firefox"}}, page: htrPage{URL: "https://login.example/"}}
			stub.answer = func(cmd htrCommand) (any, string) {
				if cmd.Action == "find" && cmd.Target.Ref == "@e3" {
					if tc.findErr != "" {
						return nil, tc.findErr
					}
					return htrElementJSON("@e3", tc.findTag, tc.findPath), ""
				}
				return answer(cmd)
			}
			surf := htrSurface{h: newHTRTestSurfaces(t, stub), tab: 7, browser: "firefox"}
			table := &cdp.RefTable{}
			page, err := surf.Controls(context.Background(), table)
			if err != nil {
				t.Fatalf("Controls: %v", err)
			}
			var checkRef string
			for _, c := range page.Controls {
				if c.Role == "checkbox" {
					checkRef = c.Ref
				}
			}
			before := len(stub.recorded())
			_, err = surf.SetChecked(context.Background(), "", checkRef, false, table)
			if err == nil || !strings.Contains(err.Error(), "stale ref") {
				t.Fatalf("SetChecked on a stale ref = %v; want a stale ref error that browser_run matches", err)
			}
			for _, c := range stub.recorded()[before:] {
				if c.Command.Action == "uncheck" || c.Command.Action == "check" {
					t.Fatalf("a stale ref still sent %s", c.Command.Action)
				}
			}
		})
	}
}

func TestHTRSurface_SelectorMustMatchExactlyOne(t *testing.T) {
	stub := &htrStub{
		tabs: []htrTab{{ID: 7, Browser: "firefox"}},
		answer: func(cmd htrCommand) (any, string) {
			if cmd.Action == "findAll" {
				return []any{htrElementJSON("@e1", "button", "/a"), htrElementJSON("@e2", "button", "/b")}, ""
			}
			return nil, ""
		},
	}
	surf := htrSurface{h: newHTRTestSurfaces(t, stub), tab: 7, browser: "firefox"}
	_, err := surf.ClickElement(context.Background(), "", "button", &cdp.RefTable{})
	if err == nil || !strings.Contains(err.Error(), "matches 2 elements") {
		t.Fatalf("ambiguous selector error = %v", err)
	}
	for _, c := range stub.recorded() {
		if c.Command.Action == "click" {
			t.Fatal("an ambiguous selector still clicked")
		}
	}
}

func TestHTRSurface_FillReadsBackAndClears(t *testing.T) {
	refs := [3]string{"@e1", "@e2", "@e3"}
	values := map[string]string{}
	stub := &htrStub{tabs: []htrTab{{ID: 7, Browser: "firefox"}}, page: htrPage{URL: "https://login.example/"}}
	base := htrPageAnswer(refs)
	stub.answer = func(cmd htrCommand) (any, string) {
		switch cmd.Action {
		case "fill":
			values[cmd.Target.Ref] = cmd.Value
			return nil, ""
		case "clear":
			values[cmd.Target.Ref] = ""
			return nil, ""
		case "find":
			if v, ok := values[cmd.Target.Ref]; ok && cmd.Target.Ref == refs[0] {
				return map[string]any{"ref": refs[0], "tag": "input", "xpath": "/html/body/dialog/input", "value": v}, ""
			}
		}
		return base(cmd)
	}
	surf := htrSurface{h: newHTRTestSurfaces(t, stub), tab: 7, browser: "firefox"}
	table := &cdp.RefTable{}
	page, err := surf.Controls(context.Background(), table)
	if err != nil {
		t.Fatalf("Controls: %v", err)
	}
	var emailRef string
	for _, c := range page.Controls {
		if c.Role == "textbox" {
			emailRef = c.Ref
		}
	}
	if msg, err := surf.FillElement(context.Background(), "", emailRef, "x@y.z", table); err != nil || msg != "filled "+emailRef {
		t.Fatalf("FillElement = (%q, %v)", msg, err)
	}
	if _, err := surf.FillElement(context.Background(), "", emailRef, "", table); err != nil {
		t.Fatalf("FillElement with empty value: %v", err)
	}
	var sent []htrCommand
	for _, c := range stub.recorded() {
		if c.Command.Action == "fill" || c.Command.Action == "clear" {
			sent = append(sent, c.Command)
		}
	}
	if len(sent) != 2 || sent[0].Action != "fill" || sent[0].Value != "x@y.z" || sent[0].Target.Ref != "@e1" || sent[1].Action != "clear" {
		t.Fatalf("fill requests = %+v; want fill x@y.z then clear on @e1", sent)
	}
}

func TestHTRSurface_FillFormReportsEachFieldAndRefusesFiles(t *testing.T) {
	refs := [3]string{"@e1", "@e2", "@e3"}
	stub := &htrStub{tabs: []htrTab{{ID: 7, Browser: "firefox"}}, page: htrPage{URL: "https://login.example/"}, answer: htrPageAnswer(refs)}
	surf := htrSurface{h: newHTRTestSurfaces(t, stub), tab: 7, browser: "firefox"}
	table := &cdp.RefTable{}
	page, err := surf.Controls(context.Background(), table)
	if err != nil {
		t.Fatalf("Controls: %v", err)
	}
	var emailRef, rememberRef string
	for _, c := range page.Controls {
		switch c.Role {
		case "textbox":
			emailRef = c.Ref
		case "checkbox":
			rememberRef = c.Ref
		}
	}
	fields := []cdp.FormField{
		{Selector: emailRef, Value: json.RawMessage(`"a@b.c"`)},
		{Selector: rememberRef, Value: json.RawMessage(`false`)},
		{Selector: emailRef, Value: json.RawMessage(`null`)},
		{Selector: emailRef, Value: json.RawMessage(`["/tmp/x.pdf"]`)},
	}
	reports, err := surf.FillForm(context.Background(), "", fields, table, func(string) error { return nil })
	if err != nil {
		t.Fatalf("FillForm: %v", err)
	}
	if reports[0].Status != cdp.FieldOK || reports[0].Kind != "text" {
		t.Fatalf("text field report = %+v", reports[0])
	}
	// The stub's checkbox stays checked, so unchecking reads back as a mismatch.
	if reports[1].Status != cdp.FieldMismatch || reports[1].Actual != "true" {
		t.Fatalf("checkbox report = %+v; want MISMATCH reading true", reports[1])
	}
	if reports[2].Status != cdp.FieldSkipped {
		t.Fatalf("null field report = %+v; want SKIPPED", reports[2])
	}
	if reports[3].Status != cdp.FieldFailed || !strings.Contains(reports[3].Error, "file fields") {
		t.Fatalf("file field report = %+v; want FAILED naming file fields", reports[3])
	}
}

func TestHTRSurface_SnapshotRendersCDPLinesWithRefs(t *testing.T) {
	refs := [3]string{"@e1", "@e2", "@e3"}
	stub := &htrStub{tabs: []htrTab{{ID: 7, Browser: "firefox"}}, answer: htrPageAnswer(refs)}
	surf := htrSurface{h: newHTRTestSurfaces(t, stub), tab: 7, browser: "firefox"}
	text, minted, err := surf.Snapshot(context.Background(), "", &cdp.RefTable{})
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	for _, want := range []string{
		`document "Sign in"`,
		`  dialog "Sign in"`,
		`    textbox "Email" value="a@b.c" @e`,
		`    button "Go" @e`,
		` disabled`,
		`    checkbox "Remember" @e`,
		` checked`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("snapshot lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "@e9") || strings.Contains(text, `link "Home"`) == false {
		// The link has no identity, so it is shown without a ref.
		t.Fatalf("link without identity should appear without a ref:\n%s", text)
	}
	if len(minted) != 3 {
		t.Fatalf("minted %d refs, want 3 (the link has no identity)", len(minted))
	}
}

func TestHTRSurface_CoordinateClickFailureNamesTheCause(t *testing.T) {
	stub := &htrStub{tabs: []htrTab{{ID: 7, Browser: "firefox"}}, answer: func(cmd htrCommand) (any, string) {
		return nil, "Element {\"x\":10,\"y\":20} was not found"
	}}
	surf := htrSurface{h: newHTRTestSurfaces(t, stub), tab: 7, browser: "firefox"}
	err := surf.Click(context.Background(), 10, 20)
	if err == nil || !strings.Contains(err.Error(), "coordinate click over htrcli failed") || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("coordinate click error = %v", err)
	}
	if strings.Contains(err.Error(), "Firefox") {
		t.Fatalf("coordinate click error still names Firefox: %v", err)
	}
	got := stub.recorded()[0].Command.Target
	if got == nil || got.X == nil || *got.X != 10 || got.Y == nil || *got.Y != 20 {
		t.Fatalf("coordinate target = %+v; want x 10, y 20", got)
	}
}

func TestHTRSurface_ScreenshotAndPageText(t *testing.T) {
	png := solidPNG(t, 40, 20)
	long := strings.Repeat("ab", cdp.PageTextMaxChars)
	stub := &htrStub{
		tabs: []htrTab{{ID: 7, Browser: "firefox"}},
		page: htrPage{URL: "https://shot.example/", ViewportWidth: 1375, ViewportHeight: 921},
		png:  png,
		answer: func(cmd htrCommand) (any, string) {
			return long, ""
		},
	}
	surf := htrSurface{h: newHTRTestSurfaces(t, stub), tab: 7, browser: "firefox"}
	raw, w, h, err := surf.Screenshot(context.Background())
	if err != nil || w != 1375 || h != 921 || string(raw) != string(png) {
		t.Fatalf("Screenshot = (%d bytes, %d, %d, %v)", len(raw), w, h, err)
	}
	text, truncated, err := surf.PageText(context.Background())
	if err != nil || !truncated || len([]rune(text)) != cdp.PageTextMaxChars {
		t.Fatalf("PageText = (%d runes, %v, %v); want the cap and truncated", len([]rune(text)), truncated, err)
	}
}

func TestHTRSurface_WaitPollsForGoneAndText(t *testing.T) {
	calls := 0
	stub := &htrStub{tabs: []htrTab{{ID: 7, Browser: "firefox"}}, answer: func(cmd htrCommand) (any, string) {
		switch cmd.Action {
		case "findAll":
			calls++
			if calls == 1 {
				return []any{htrElementJSON("@e1", "div", "/div")}, ""
			}
			return []any{}, ""
		case "getText":
			return "Saved", ""
		}
		return nil, ""
	}}
	surf := htrSurface{h: newHTRTestSurfaces(t, stub), tab: 7, browser: "firefox"}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := surf.Wait(ctx, "", cdp.WaitSpec{Selector: "#spinner", Gone: true, Timeout: 3 * time.Second}); err != nil {
		t.Fatalf("gone wait: %v", err)
	}
	if err := surf.Wait(ctx, "", cdp.WaitSpec{Text: "Saved", Timeout: time.Second}); err != nil {
		t.Fatalf("text wait: %v", err)
	}
	err := surf.Wait(ctx, "", cdp.WaitSpec{Text: "Never", Timeout: 300 * time.Millisecond})
	if err == nil || !strings.Contains(err.Error(), "timeout after") {
		t.Fatalf("missing text wait = %v; want a timeout", err)
	}
}

func TestHTRSurface_ToolTabListingAndClosedGuard(t *testing.T) {
	stub := &htrStub{tabs: []htrTab{
		{ID: 12, URL: "https://two.example/", Title: "Two", Browser: "chrome"},
		{ID: 4, URL: "https://one.example/", Title: "One", Active: true, Browser: "firefox"},
	}}
	h := newHTRTestSurfaces(t, stub)
	tabs, err := h.Tabs(context.Background())
	if err != nil {
		t.Fatalf("Tabs: %v", err)
	}
	if len(tabs) != 2 || tabs[0].ID != 4 || tabs[1].ID != 12 {
		t.Fatalf("client tabs not sorted by id: %+v", tabs)
	}
	b, _ := newTestBrowserTool(t, &fakeSurface{}, 1568)
	b.surfaces = h
	out, err := browserCall(t, b, `{"action":"tabs"}`)
	if err != nil {
		t.Fatalf("tabs: %v", err)
	}
	if !strings.Contains(out, `- 4 (client browser tab, firefox, active) "One" https://one.example/`) ||
		strings.Index(out, "- 4 ") > strings.Index(out, "- 12 ") {
		t.Fatalf("tabs listing = %q", out)
	}
	if _, err := browserCall(t, b, `{"action":"close","tab":"4"}`); err == nil || !strings.Contains(err.Error(), "did not open") {
		t.Fatalf("close of a user's client tab = %v; want a refusal", err)
	}
}

func TestHTRSurface_ConcurrentCommandsGetDistinctIDs(t *testing.T) {
	seen := map[string]bool{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := htrNextCommandID()
			mu.Lock()
			defer mu.Unlock()
			seen[id] = true
		}()
	}
	wg.Wait()
	if len(seen) != 50 {
		t.Fatalf("command ids collided: %d distinct of 50", len(seen))
	}
}

func TestHTRRefStore_EvictsOldestAndNeverReusesKeys(t *testing.T) {
	s := newHTRRefStore()
	first := s.add(htrRefIdentity{tab: 1, daemonRef: "@e1"})
	for i := 0; i < htrRefStoreCap; i++ {
		s.add(htrRefIdentity{tab: 1, daemonRef: fmt.Sprintf("@e%d", i+2)})
	}
	if _, ok := s.get(first); ok {
		t.Fatal("the oldest identity survived the cap")
	}
	latest := s.add(htrRefIdentity{tab: 1, daemonRef: "@e-new"})
	if latest <= first {
		t.Fatalf("keys must keep increasing: %d after %d", latest, first)
	}
}

// TestWireRemoteHTRSurfaces_ReadsHostConfig pins the boot choice on a remote host:
// the client's daemon when the host has a token to reach it with, the host's own
// Chrome otherwise. Nothing is started either way.
func TestHTRSurface_TypeWithoutTargetSendsNoTarget(t *testing.T) {
	stub := &htrStub{tabs: []htrTab{{ID: 7, Browser: "firefox"}}}
	surf := htrSurface{h: newHTRTestSurfaces(t, stub), tab: 7, browser: "firefox"}
	if err := surf.InsertText(context.Background(), "hello"); err != nil {
		t.Fatalf("InsertText: %v", err)
	}
	got := stub.recorded()
	if len(got) != 1 || got[0].Command.Action != "type" || got[0].Command.Target != nil || got[0].Command.Value != "hello" {
		t.Fatalf("type request = %+v; want one type with no target and value hello", got)
	}
}

func TestHTRSurface_ScrollPageSendsScrollByFromTheViewport(t *testing.T) {
	stub := &htrStub{tabs: []htrTab{{ID: 7, Browser: "firefox"}}, page: htrPage{ViewportHeight: 500}}
	surf := htrSurface{h: newHTRTestSurfaces(t, stub), tab: 7, browser: "firefox"}
	ctx := context.Background()
	if err := surf.ScrollPage(ctx, 1); err != nil {
		t.Fatalf("ScrollPage down: %v", err)
	}
	if err := surf.ScrollPage(ctx, -1); err != nil {
		t.Fatalf("ScrollPage up: %v", err)
	}
	if err := surf.ScrollPage(ctx, 2); err == nil {
		t.Fatal("ScrollPage with direction 2 was not refused")
	}
	got := stub.recorded()
	if len(got) != 2 {
		t.Fatalf("scroll sent %d commands; want 2 scrollBy commands", len(got))
	}
	for i, want := range []float64{400, -400} {
		c := got[i].Command
		dy, _ := c.Options["dy"].(float64)
		dx, _ := c.Options["dx"].(float64)
		if c.Action != "scrollBy" || c.Target != nil || dx != 0 || math.Abs(dy-want) > 1e-6 {
			t.Fatalf("scroll %d = %+v; want scrollBy with dx 0, dy %v and no target", i, c, want)
		}
	}
}

func TestHTRSurface_ConsoleEntriesMapEventsNewestLast(t *testing.T) {
	stub := &htrStub{tabs: []htrTab{{ID: 7, Browser: "firefox"}}, events: map[string]any{
		"console": map[string]any{
			"entries": []any{
				map[string]any{"seq": 1, "kind": "console", "timestamp": 1000, "data": map[string]any{"level": "log", "args": []string{"first"}}},
				map[string]any{"seq": 2, "kind": "console", "timestamp": 2000, "data": map[string]any{"level": "error", "args": []string{"boom", "detail"}, "source": "https://x.example/app.js:3:4"}},
				map[string]any{"seq": 3, "kind": "console", "timestamp": 3000, "data": map[string]any{"level": "warn", "args": []string{"third"}}},
			},
			"dropped":            4,
			"oldestAvailableSeq": 5,
		},
	}}
	surf := htrSurface{h: newHTRTestSurfaces(t, stub), tab: 7, browser: "firefox"}
	entries, dropped, err := surf.ConsoleEntries(2)
	if err != nil || dropped != 4 || len(entries) != 2 {
		t.Fatalf("ConsoleEntries(2) = %d entries, dropped %d; want 2 and 4", len(entries), dropped)
	}
	if e := entries[0]; e.Level != "error" || e.Kind != "console" || e.Text != "boom detail" || e.Location != "https://x.example/app.js:3:4" || e.TS != 2000 {
		t.Fatalf("first entry = %+v; want the error at 2000 with its text and source", e)
	}
	if e := entries[1]; e.Level != "warn" || e.Text != "third" || e.Location != "" {
		t.Fatalf("second entry = %+v; want the warn last", e)
	}
	if q := stub.queries(); len(q) != 1 || q[0] != "kind=console&tab=7&since=0" {
		t.Fatalf("events query = %q; want kind=console&tab=7&since=0", q)
	}
}

func TestHTRSurface_NetworkEntriesMapStatusAndMissingStatus(t *testing.T) {
	stub := &htrStub{tabs: []htrTab{{ID: 7, Browser: "firefox"}}, events: map[string]any{
		"network": map[string]any{
			"entries": []any{
				map[string]any{"seq": 1, "kind": "network", "timestamp": 1000, "data": map[string]any{"requestId": "r1", "url": "https://a.example/x", "method": "GET", "status": 200, "durationMs": 12}},
				map[string]any{"seq": 2, "kind": "network", "timestamp": 2000, "data": map[string]any{"requestId": "r2", "url": "https://a.example/y", "method": "POST"}},
			},
			"dropped":            0,
			"oldestAvailableSeq": 1,
		},
	}}
	surf := htrSurface{h: newHTRTestSurfaces(t, stub), tab: 7, browser: "firefox"}
	entries, dropped, err := surf.NetworkEntries(10)
	if err != nil || dropped != 0 || len(entries) != 2 {
		t.Fatalf("NetworkEntries(10) = %d entries, dropped %d; want 2 and 0", len(entries), dropped)
	}
	ok := entries[0]
	if ok.RequestID != "r1" || ok.Method != "GET" || ok.URL != "https://a.example/x" || ok.Status != 200 || !ok.Finished || ok.Failed {
		t.Fatalf("completed entry = %+v; want r1 GET 200 finished and not failed", ok)
	}
	missing := entries[1]
	if missing.Failed || missing.Error != "" || missing.Status != 0 || !missing.Finished || missing.Method != "POST" {
		t.Fatalf("entry without a status = %+v; want finished with no status and not failed", missing)
	}
	if q := stub.queries(); len(q) != 1 || q[0] != "kind=network&tab=7&since=0" {
		t.Fatalf("events query = %q; want kind=network&tab=7&since=0", q)
	}
}

func TestHTRSurface_NetworkNoteNamesTheArmAndAnyGap(t *testing.T) {
	stub := &htrStub{tabs: []htrTab{{ID: 7, Browser: "chrome"}}}
	h := newHTRTestSurfaces(t, stub)
	surf := htrSurface{h: h, tab: 7, browser: "chrome"}
	armedAt := time.Now()
	h.armed.tabs[7] = htrArm{armedAt: armedAt, bounded: true, window: 120 * time.Second}
	if note := surf.NetworkNote(); !strings.Contains(note, "for a 120 s window") || strings.Contains(note, "stretch") {
		t.Fatalf("bounded arm note = %q; want the window length and no gap", note)
	}
	h.armed.tabs[7] = htrArm{armedAt: armedAt, bounded: true, window: 120 * time.Second, gaps: 1, gapFrom: armedAt, gapTo: armedAt.Add(time.Minute)}
	if note := surf.NetworkNote(); !strings.Contains(note, "1 stretch(es) had no capture") {
		t.Fatalf("gap note = %q; want the stretch named", note)
	}
	h.armed.tabs[7] = htrArm{armedAt: armedAt}
	if note := surf.NetworkNote(); !strings.Contains(note, "always on") {
		t.Fatalf("always-on note = %q; want it to say the capture is always on", note)
	}
}

func TestHTRSurface_SurfaceRearmsAndRecordsTheGap(t *testing.T) {
	stub := &htrStub{tabs: []htrTab{{ID: 7, Browser: "chrome"}}}
	stub.answer = func(cmd htrCommand) (any, string) {
		if cmd.Action == "networkCapture" {
			return map[string]any{"armed": true, "durationMs": 120000, "transport": "cdp"}, ""
		}
		return nil, ""
	}
	h := newHTRTestSurfaces(t, stub)
	ctx := context.Background()
	arms := func() int {
		n := 0
		for _, c := range stub.recorded() {
			if c.Command.Action == "networkCapture" {
				n++
			}
		}
		return n
	}
	if _, _, err := h.Surface(ctx, "tab:7", ""); err != nil {
		t.Fatalf("first Surface: %v", err)
	}
	// Older than the re-arm age but inside the window: re-armed, with no gap.
	arm := h.armed.tabs[7]
	arm.armedAt = time.Now().Add(-101 * time.Second)
	h.armed.tabs[7] = arm
	if _, _, err := h.Surface(ctx, "tab:7", ""); err != nil {
		t.Fatalf("re-arming Surface: %v", err)
	}
	if n := arms(); n != 2 || h.armed.tabs[7].gaps != 0 {
		t.Fatalf("after a re-arm inside the window: %d arms, %d gaps; want 2 and 0", n, h.armed.tabs[7].gaps)
	}
	// Older than the window: the window had closed, so the re-arm records a gap.
	arm = h.armed.tabs[7]
	arm.armedAt = time.Now().Add(-130 * time.Second)
	h.armed.tabs[7] = arm
	if _, _, err := h.Surface(ctx, "tab:7", ""); err != nil {
		t.Fatalf("re-arming Surface after the window: %v", err)
	}
	after := h.armed.tabs[7]
	if n := arms(); n != 3 || after.gaps != 1 || !after.gapTo.After(after.gapFrom) {
		t.Fatalf("after a re-arm past the window: %d arms, %d gaps, gap %v to %v; want 3 arms and one gap", n, after.gaps, after.gapFrom, after.gapTo)
	}
}

func TestHTRSurface_FailedEventReadIsAnError(t *testing.T) {
	stub := &htrStub{tabs: []htrTab{{ID: 7, Browser: "firefox"}}}
	surf := htrSurface{h: newHTRTestSurfaces(t, stub), tab: 7, browser: "firefox"}
	if _, _, err := surf.ConsoleEntries(5); err == nil || !strings.Contains(err.Error(), "read console events of client tab 7") {
		t.Fatalf("failed console read = %v; want an error naming the read", err)
	}
	if _, _, err := surf.NetworkEntries(5); err == nil || !strings.Contains(err.Error(), "read network events of client tab 7") {
		t.Fatalf("failed network read = %v; want an error naming the read", err)
	}
}

func TestHTRSurface_PickSendsTextAndMapsTheMode(t *testing.T) {
	stub := &htrStub{tabs: []htrTab{{ID: 7, Browser: "firefox"}}}
	stub.answer = func(cmd htrCommand) (any, string) {
		switch cmd.Action {
		case "findAll":
			return []any{htrElementJSON("@e1", "select", "/html/body/select")}, ""
		case "find":
			return htrElementJSON("@e1", "select", "/html/body/select"), ""
		case "pick":
			if cmd.Value == "Canada" {
				return map[string]any{"value": "Canada", "matched": "Canada"}, ""
			}
			return nil, `no option matched "Mars"; the options are Canada, Germany`
		}
		return nil, "unexpected action " + cmd.Action
	}
	surf := htrSurface{h: newHTRTestSurfaces(t, stub), tab: 7, browser: "firefox"}
	ctx := context.Background()
	table := &cdp.RefTable{}
	found, err := surf.FindElements(ctx, "", "select", 5, table)
	if err != nil || len(found.Elements) != 1 {
		t.Fatalf("FindElements = %+v, %v", found, err)
	}
	ref := found.Elements[0].Ref
	res, err := surf.Pick(ctx, "", ref, "Canada", table)
	if err != nil || res.Mode != "select" || res.Selected != "Canada" {
		t.Fatalf("Pick = (%+v, %v); want mode select showing Canada", res, err)
	}
	got := stub.recorded()
	last := got[len(got)-1].Command
	if last.Action != "pick" || last.Target == nil || last.Target.Ref != "@e1" || last.Value != "Canada" || last.Options != nil {
		t.Fatalf("pick request = %+v; want pick on daemon ref @e1 with value Canada and no frame", last)
	}
	if _, err := surf.Pick(ctx, "", ref, "Mars", table); err == nil || !strings.Contains(err.Error(), `no option matched "Mars"`) {
		t.Fatalf("unmatched Pick = %v; want the daemon's no-match error", err)
	}
}

func TestHTRSurface_UploadRefusesDisallowedPathBeforeAnyRead(t *testing.T) {
	stub := &htrStub{tabs: []htrTab{{ID: 7, Browser: "firefox"}}}
	surf := htrSurface{h: newHTRTestSurfaces(t, stub), tab: 7, browser: "firefox"}
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(path, []byte("secret"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	denied := errors.New("outside the upload roots")
	var seen string
	_, err := surf.UploadFiles(ctx, "", "input", []string{path}, &cdp.RefTable{}, func(p string) error {
		seen = p
		return denied
	})
	if err == nil || !errors.Is(err, denied) || !strings.Contains(err.Error(), "upload refused") {
		t.Fatalf("disallowed upload = %v; want an upload refused error wrapping the policy error", err)
	}
	if seen == "" {
		t.Fatal("the path policy was never asked")
	}
	if _, err := surf.UploadFiles(ctx, "", "input", []string{"relative.txt"}, &cdp.RefTable{}, func(string) error { return nil }); err == nil || !strings.Contains(err.Error(), "not an absolute path") {
		t.Fatalf("relative upload = %v; want the absolute path error", err)
	}
	if got := stub.recorded(); len(got) != 0 {
		t.Fatalf("a refused upload still sent %d command(s): %+v", len(got), got)
	}
}

func TestHTRSurface_UploadSendsBase64AndNeverAHostPath(t *testing.T) {
	stub := &htrStub{tabs: []htrTab{{ID: 7, Browser: "firefox"}}}
	stub.answer = func(cmd htrCommand) (any, string) {
		if cmd.Action == "findAll" {
			return []any{htrElementJSON("@e4", "input", "/html/body/input")}, ""
		}
		return nil, ""
	}
	surf := htrSurface{h: newHTRTestSurfaces(t, stub), tab: 7, browser: "firefox"}
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	msg, err := surf.UploadFiles(context.Background(), "", "input[type=file]", []string{path}, &cdp.RefTable{}, func(string) error { return nil })
	if err != nil || !strings.Contains(msg, "uploaded 1 file(s)") {
		t.Fatalf("UploadFiles = (%q, %v)", msg, err)
	}
	got := stub.recorded()
	last := got[len(got)-1].Command
	if last.Action != "uploadFiles" || last.Target == nil || last.Target.Ref != "@e4" {
		t.Fatalf("upload request = %+v; want uploadFiles on daemon ref @e4", last)
	}
	raw, _ := json.Marshal(last)
	if strings.Contains(string(raw), dir) {
		t.Fatalf("upload request carries a host path: %s", raw)
	}
	files, _ := last.Options["filesData"].([]any)
	if len(files) != 1 {
		t.Fatalf("filesData = %+v; want one file", last.Options["filesData"])
	}
	f, _ := files[0].(map[string]any)
	mimeType, _ := f["mimeType"].(string)
	if f["name"] != "notes.txt" || f["base64"] != base64.StdEncoding.EncodeToString([]byte("hello")) || !strings.HasPrefix(mimeType, "text/plain") {
		t.Fatalf("file entry = %+v; want notes.txt, text/plain and base64 of hello", f)
	}
}

func TestHTRSurface_FrameIsPassedAndRefsStayInTheirFrame(t *testing.T) {
	stub := &htrStub{tabs: []htrTab{{ID: 7, Browser: "firefox"}}}
	stub.answer = func(cmd htrCommand) (any, string) {
		switch cmd.Action {
		case "findAll":
			return []any{htrElementJSON("@e2", "button", "/html/body/button")}, ""
		case "find":
			return htrElementJSON("@e2", "button", "/html/body/button"), ""
		case "snapshot":
			return map[string]any{"role": "document", "name": "", "children": []any{}}, ""
		case "click":
			return nil, ""
		}
		return nil, "unexpected action " + cmd.Action
	}
	surf := htrSurface{h: newHTRTestSurfaces(t, stub), tab: 7, browser: "firefox"}
	ctx := context.Background()
	table := &cdp.RefTable{}
	if _, _, err := surf.Snapshot(ctx, "iframe#pay", table); err != nil {
		t.Fatalf("Snapshot in frame: %v", err)
	}
	for _, c := range stub.recorded() {
		if c.Command.Options["frame"] != "iframe#pay" {
			t.Fatalf("snapshot read %s without the frame: %+v", c.Command.Action, c.Command)
		}
	}
	found, err := surf.FindElements(ctx, "iframe#pay", "button", 5, table)
	if err != nil || len(found.Elements) != 1 {
		t.Fatalf("FindElements in frame = %+v, %v", found, err)
	}
	ref := found.Elements[0].Ref
	before := len(stub.recorded())
	if _, err := surf.ClickElement(ctx, "iframe#pay", ref, table); err != nil {
		t.Fatalf("ClickElement in frame: %v", err)
	}
	for _, c := range stub.recorded()[before:] {
		if c.Command.Options["frame"] != "iframe#pay" {
			t.Fatalf("click path sent %s without the frame: %+v", c.Command.Action, c.Command)
		}
	}
	before = len(stub.recorded())
	_, err = surf.ClickElement(ctx, "", ref, table)
	if err == nil || !strings.Contains(err.Error(), `minted in frame "iframe#pay", not frame ""`) {
		t.Fatalf("ref from a frame used at the top = %v; want a frame mismatch refusal", err)
	}
	if len(stub.recorded()) != before {
		t.Fatal("a frame mismatch still sent a command")
	}
}

func TestHTRSurface_SetCheckedReadsBackWithIsChecked(t *testing.T) {
	cases := []struct {
		name      string
		isChecked bool
		wantErr   string
		wantMsg   string
	}{
		{name: "reads back as set", isChecked: true, wantMsg: "(checked=true)"},
		{name: "does not stick", isChecked: false, wantErr: "did not stick"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &htrStub{tabs: []htrTab{{ID: 7, Browser: "firefox"}}}
			stub.answer = func(cmd htrCommand) (any, string) {
				switch cmd.Action {
				case "findAll":
					return []any{htrElementJSON("@e3", "input", "/html/body/input")}, ""
				case "find":
					return htrElementJSON("@e3", "input", "/html/body/input"), ""
				case "check", "uncheck":
					return nil, ""
				case "isChecked":
					return map[string]any{"checked": tc.isChecked}, ""
				}
				return nil, "unexpected action " + cmd.Action
			}
			surf := htrSurface{h: newHTRTestSurfaces(t, stub), tab: 7, browser: "firefox"}
			ctx := context.Background()
			table := &cdp.RefTable{}
			found, err := surf.FindElements(ctx, "", "input", 5, table)
			if err != nil || len(found.Elements) != 1 {
				t.Fatalf("FindElements = %+v, %v", found, err)
			}
			msg, err := surf.SetChecked(ctx, "", found.Elements[0].Ref, true, table)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("SetChecked = %v; want %q", err, tc.wantErr)
				}
			} else if err != nil || !strings.Contains(msg, tc.wantMsg) {
				t.Fatalf("SetChecked = (%q, %v); want %q", msg, err, tc.wantMsg)
			}
			got := stub.recorded()
			last := got[len(got)-1].Command
			if last.Action != "isChecked" || last.Target == nil || last.Target.Ref != "@e3" || last.Options != nil {
				t.Fatalf("read-back request = %+v; want isChecked on @e3 with no frame", last)
			}
		})
	}
}

func TestHTRSurface_SurfaceArmsNetworkCaptureOncePerTab(t *testing.T) {
	stub := &htrStub{tabs: []htrTab{{ID: 7, Browser: "firefox"}}}
	stub.answer = func(cmd htrCommand) (any, string) {
		if cmd.Action == "networkCapture" {
			return map[string]any{"armed": true, "durationMs": 120000, "transport": "cdp"}, ""
		}
		return nil, ""
	}
	h := newHTRTestSurfaces(t, stub)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if _, _, err := h.Surface(ctx, "tab:7", ""); err != nil {
			t.Fatalf("Surface %d: %v", i, err)
		}
	}
	armed := 0
	for _, c := range stub.recorded() {
		if c.Command.Action != "networkCapture" {
			continue
		}
		armed++
		if d, _ := c.Command.Options["durationMs"].(float64); d != htrNetworkWindowMs || c.Command.Target != nil {
			t.Fatalf("networkCapture = %+v; want durationMs %d and no target", c.Command, htrNetworkWindowMs)
		}
	}
	if armed != 1 {
		t.Fatalf("networkCapture sent %d times for one tab; want once", armed)
	}
}

func TestHTRSurface_FailedNetworkArmIsRetried(t *testing.T) {
	var mu sync.Mutex
	failing := true
	stub := &htrStub{tabs: []htrTab{{ID: 7, Browser: "firefox"}}}
	stub.answer = func(cmd htrCommand) (any, string) {
		mu.Lock()
		defer mu.Unlock()
		if cmd.Action != "networkCapture" {
			return nil, ""
		}
		if failing {
			return nil, "debugger already attached"
		}
		return map[string]any{"armed": true, "transport": "webRequest"}, ""
	}
	h := newHTRTestSurfaces(t, stub)
	ctx := context.Background()
	_, _, err := h.Surface(ctx, "tab:7", "")
	if err == nil || !strings.Contains(err.Error(), "arm network capture on client tab 7") || !strings.Contains(err.Error(), "already attached") {
		t.Fatalf("Surface with a failed arm = %v; want the arm error", err)
	}
	mu.Lock()
	failing = false
	mu.Unlock()
	if _, _, err := h.Surface(ctx, "tab:7", ""); err != nil {
		t.Fatalf("Surface after the arm recovers: %v", err)
	}
	armed := 0
	for _, c := range stub.recorded() {
		if c.Command.Action == "networkCapture" {
			armed++
		}
	}
	if armed != 2 {
		t.Fatalf("networkCapture sent %d times; want the failed arm and the retry", armed)
	}
}

func TestHTRSurface_NetworkArmRefusesAnUnknownTransport(t *testing.T) {
	stub := &htrStub{tabs: []htrTab{{ID: 7, Browser: "chrome"}}}
	stub.answer = func(cmd htrCommand) (any, string) {
		return map[string]any{"armed": true, "durationMs": 120000, "transport": "debugger-proxy"}, ""
	}
	_, _, err := newHTRTestSurfaces(t, stub).Surface(context.Background(), "tab:7", "")
	if err == nil || !strings.Contains(err.Error(), `unknown transport "debugger-proxy"`) {
		t.Fatalf("Surface with an unknown transport = %v; want the transport refusal", err)
	}
}

func TestHTRSurface_ControlsMapBoxesAndTheViewport(t *testing.T) {
	tree := func(withViewport bool) map[string]any {
		root := map[string]any{"role": "document", "name": "Shop", "children": []any{
			map[string]any{"role": "button", "name": "Inside", "ref": "@e1", "state": map[string]any{},
				"box": map[string]any{"x": 10, "y": 10, "w": 100, "h": 20}},
			map[string]any{"role": "button", "name": "Outside", "ref": "@e2", "state": map[string]any{},
				"box": map[string]any{"x": 900, "y": 10, "w": 50, "h": 20}},
			map[string]any{"role": "button", "name": "Hidden", "ref": "@e3", "state": map[string]any{}},
		}}
		if withViewport {
			root["viewport"] = map[string]any{"w": 800, "h": 600}
			root["scroll"] = map[string]any{"x": 0, "y": 240}
		}
		return root
	}
	newStub := func(withViewport bool) *htrStub {
		stub := &htrStub{tabs: []htrTab{{ID: 7, Browser: "firefox"}}, page: htrPage{URL: "https://shop.example/", ReadyState: "complete"}}
		stub.answer = func(cmd htrCommand) (any, string) {
			switch cmd.Action {
			case "snapshot":
				return tree(withViewport), ""
			case "findAll":
				return []any{
					htrElementJSON("@e1", "button", "/html/body/button[1]"),
					htrElementJSON("@e2", "button", "/html/body/button[2]"),
					htrElementJSON("@e3", "button", "/html/body/button[3]"),
				}, ""
			case "getText":
				return "Inside Outside Hidden", ""
			}
			return nil, "unexpected action " + cmd.Action
		}
		return stub
	}

	stub := newStub(true)
	surf := htrSurface{h: newHTRTestSurfaces(t, stub), tab: 7, browser: "firefox"}
	page, err := surf.Controls(context.Background(), &cdp.RefTable{})
	if err != nil {
		t.Fatalf("Controls: %v", err)
	}
	if page.ViewportWidth != 800 || page.ViewportHeight != 600 {
		t.Fatalf("viewport = %dx%d; want 800x600", page.ViewportWidth, page.ViewportHeight)
	}
	if page.ScrollX != 0 || page.ScrollY != 240 {
		t.Fatalf("scroll = %v,%v; want 0,240", page.ScrollX, page.ScrollY)
	}
	if len(page.Controls) != 3 {
		t.Fatalf("controls = %+v; want three buttons", page.Controls)
	}
	inside, outside, hidden := page.Controls[0], page.Controls[1], page.Controls[2]
	if inside.Box == nil || *inside.Box != (cdp.ControlBox{X: 10, Y: 10, W: 100, H: 20}) || !inside.InViewport {
		t.Fatalf("inside control = box %+v in_viewport %t; want its box in view", inside.Box, inside.InViewport)
	}
	if outside.Box == nil || *outside.Box != (cdp.ControlBox{X: 900, Y: 10, W: 50, H: 20}) || outside.InViewport {
		t.Fatalf("outside control = box %+v in_viewport %t; want its box out of view", outside.Box, outside.InViewport)
	}
	if hidden.Box != nil || hidden.InViewport {
		t.Fatalf("hidden control = box %+v in_viewport %t; want no box and not in view", hidden.Box, hidden.InViewport)
	}

	noViewport := htrSurface{h: newHTRTestSurfaces(t, newStub(false)), tab: 7, browser: "firefox"}
	if _, err := noViewport.Controls(context.Background(), &cdp.RefTable{}); !errors.Is(err, cdp.ErrClientExtensionOutdated) || !strings.Contains(err.Error(), "carries no viewport") || !strings.Contains(err.Error(), "out of date") {
		t.Fatalf("Controls without a snapshot viewport = %v; want a refusal wrapping ErrClientExtensionOutdated", err)
	}
	// A snapshot with a viewport but no scroll offset is refused, not read as zero.
	noScroll := newStub(true)
	noScroll.answer = func(cmd htrCommand) (any, string) {
		if cmd.Action == "snapshot" {
			root := tree(true)
			delete(root, "scroll")
			return root, ""
		}
		return newStub(true).answer(cmd)
	}
	noScrollSurf := htrSurface{h: newHTRTestSurfaces(t, noScroll), tab: 7, browser: "firefox"}
	if _, err := noScrollSurf.Controls(context.Background(), &cdp.RefTable{}); !errors.Is(err, cdp.ErrClientExtensionOutdated) || !strings.Contains(err.Error(), "carries no scroll offset") || !strings.Contains(err.Error(), "out of date") {
		t.Fatalf("Controls without a snapshot scroll = %v; want a refusal wrapping ErrClientExtensionOutdated, not zero", err)
	}
}

func TestWireRemoteHTRSurfaces_ReadsHostConfig(t *testing.T) {
	cases := []struct {
		name     string
		config   string
		override string
		wantHTR  bool
		wantTok  string
	}{
		{name: "config token", config: `{"server":"http://127.0.0.1:3845","token":"tok-remote"}`, wantHTR: true, wantTok: "tok-remote"},
		{name: "ocode token overrides", config: `{"server":"http://127.0.0.1:3845","token":"tok-remote"}`, override: "tok-ocode", wantHTR: true, wantTok: "tok-ocode"},
		{name: "no config file", wantHTR: false},
		{name: "config without token", config: `{"server":"http://127.0.0.1:3845"}`, wantHTR: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			setHomeTree(t, home)
			if tc.config != "" {
				dir := filepath.Join(home, ".htrcli")
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
				if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(tc.config), 0o600); err != nil {
					t.Fatalf("write config: %v", err)
				}
			}
			s := New("127.0.0.1:0", "", "", nil)
			s.remoteMode = true
			s.EnableBrowse("http://127.0.0.1:0", browse.New("", nil))
			s.wireRemoteHTRSurfaces(tc.override)
			got, isHTR := s.handler.browseSurfaces.(htrAgentSurfaces)
			if isHTR != tc.wantHTR {
				t.Fatalf("browser surfaces = %T; want htr=%v", s.handler.browseSurfaces, tc.wantHTR)
			}
			if !tc.wantHTR {
				if _, isChrome := s.handler.browseSurfaces.(browseAgentSurfaces); !isChrome {
					t.Fatalf("without a token the host must keep its Chrome, got %T", s.handler.browseSurfaces)
				}
				return
			}
			if got.token != tc.wantTok || got.base != "http://127.0.0.1:3845" {
				t.Fatalf("htr surfaces = base %q token %q; want base 127.0.0.1:3845 token %q", got.base, got.token, tc.wantTok)
			}
		})
	}
}
