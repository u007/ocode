package cdp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// scriptedChrome is a fake CDP server whose reply to each command comes from a
// handler. Unlike stubChrome it can sequence replies per call (the bundle
// install tests need sentinel, install, retry on one method) and can answer with
// a CDP error, which stubChrome never does.
type scriptedChrome struct {
	conn   *Conn
	rCh    *io.PipeReader
	wCh    *io.PipeWriter
	handle func(method string, params json.RawMessage) (result any, cdpErr string)

	mu    sync.Mutex
	calls []string
}

func newScriptedChrome(t *testing.T, handle func(method string, params json.RawMessage) (any, string)) *scriptedChrome {
	t.Helper()
	wR, wW := io.Pipe()
	rR, rW := io.Pipe()
	s := &scriptedChrome{conn: NewConn(rR, wW), rCh: wR, wCh: rW, handle: handle}
	t.Cleanup(func() {
		_ = s.conn.Close()
		_ = s.rCh.Close()
		_ = s.wCh.Close()
	})
	go s.serve()
	return s
}

// newPageTarget returns a Target on a scripted Chrome, with the page session
// the agent tests use.
func newPageTarget(t *testing.T, handle func(method string, params json.RawMessage) (any, string)) (*Target, *scriptedChrome) {
	t.Helper()
	s := newScriptedChrome(t, handle)
	return &Target{conn: s.conn, sessionID: "sess-page"}, s
}

func (s *scriptedChrome) serve() {
	var buf []byte
	tmp := make([]byte, 4096)
	for {
		n, err := s.rCh.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
			for {
				idx := bytes.IndexByte(buf, 0)
				if idx < 0 {
					break
				}
				frame := buf[:idx]
				buf = buf[idx+1:]
				s.reply(frame)
			}
		}
		if err != nil {
			return
		}
	}
}

func (s *scriptedChrome) reply(frame []byte) {
	var msg struct {
		ID     *int64          `json:"id"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(frame, &msg); err != nil || msg.ID == nil {
		return
	}
	s.mu.Lock()
	s.calls = append(s.calls, msg.Method)
	s.mu.Unlock()
	result, cdpErr := s.handle(msg.Method, msg.Params)
	var out map[string]any
	if cdpErr != "" {
		out = map[string]any{"id": *msg.ID, "error": map[string]any{"code": -32000, "message": cdpErr}}
	} else {
		raw, _ := json.Marshal(result)
		out = map[string]any{"id": *msg.ID, "result": json.RawMessage(raw)}
	}
	b, _ := json.Marshal(out)
	b = append(b, 0)
	_, _ = s.wCh.Write(b)
}

// inject sends a CDP event on the page session.
func (s *scriptedChrome) inject(method string, params any) {
	raw, _ := json.Marshal(params)
	b, _ := json.Marshal(map[string]any{"method": method, "params": json.RawMessage(raw), "sessionId": "sess-page"})
	b = append(b, 0)
	_, _ = s.wCh.Write(b)
}

// callCount reports how many commands of method the server has answered.
func (s *scriptedChrome) callCount(method string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, m := range s.calls {
		if m == method {
			n++
		}
	}
	return n
}

// evalReplyValue is a Runtime.evaluate reply carrying a by-value result.
func evalReplyValue(v any) map[string]any {
	return map[string]any{"result": map[string]any{"value": v}}
}

// sentinelEnvelope is the reply a realm gives when the bundle is not installed.
func sentinelEnvelope() map[string]any {
	return evalReplyValue(map[string]any{"ok": map[string]any{"__htrcliNeedBundle": true}})
}

// execEnvelope wraps a CommandResult the way runScopedIn expects it.
func execEnvelope(data any) map[string]any {
	return evalReplyValue(map[string]any{"ok": map[string]any{"id": "x", "success": true, "data": data}})
}

// frameCheckOK is the by-value reply to the frame validation script.
func frameCheckOK() map[string]any { return evalReplyValue(map[string]any{"ok": true}) }

// TestExecDOMInstallsBundleOnceAndRetries drives the install-once path: the
// first exec answers the sentinel, the bundle is installed, and the retry
// returns the result.
func TestExecDOMInstallsBundleOnceAndRetries(t *testing.T) {
	var evals []string
	tgt, _ := newPageTarget(t, func(method string, params json.RawMessage) (any, string) {
		if method != "Runtime.evaluate" {
			return map[string]any{}, ""
		}
		var p struct {
			Expression string `json:"expression"`
		}
		_ = json.Unmarshal(params, &p)
		evals = append(evals, p.Expression)
		switch len(evals) {
		case 1:
			return sentinelEnvelope(), ""
		case 2:
			return map[string]any{"result": map[string]any{"subtype": "undefined"}}, ""
		default:
			return execEnvelope([]any{}), ""
		}
	})
	res, err := tgt.execDOM(context.Background(), "", domCommand{ID: "1", Action: "findAll", Target: &domTarget{Selector: "button"}})
	if err != nil {
		t.Fatalf("execDOM: %v", err)
	}
	if !res.Success {
		t.Fatalf("result not successful: %+v", res)
	}
	if len(evals) != 3 {
		t.Fatalf("Runtime.evaluate calls = %d, want 3 (exec, install, retry)", len(evals))
	}
	if !strings.Contains(evals[1], "window.__htrcliDom") {
		t.Errorf("second call is not the bundle install")
	}
	if !strings.Contains(evals[2], "__htrcliDom.exec") {
		t.Errorf("third call is not the retried exec")
	}
}

// TestExecDOMSecondSentinelIsError: a bundle that still does not register after
// the install is a loud error, never a second install loop.
func TestExecDOMSecondSentinelIsError(t *testing.T) {
	calls := 0
	tgt, _ := newPageTarget(t, func(method string, params json.RawMessage) (any, string) {
		if method != "Runtime.evaluate" {
			return map[string]any{}, ""
		}
		calls++
		if calls == 2 {
			return map[string]any{"result": map[string]any{"subtype": "undefined"}}, ""
		}
		return sentinelEnvelope(), ""
	})
	_, err := tgt.execDOM(context.Background(), "", domCommand{ID: "1", Action: "snapshot"})
	if err == nil || !strings.Contains(err.Error(), "did not register") {
		t.Fatalf("err = %v, want bundle did-not-register error", err)
	}
	if calls != 3 {
		t.Errorf("evaluate calls = %d, want exactly 3 (no further install attempts)", calls)
	}
}

// TestExecDOMReportsCommandFailureAsResult: a command the page rejects is a
// result with Success=false, not a Go error.
func TestExecDOMReportsCommandFailureAsResult(t *testing.T) {
	tgt, _ := newPageTarget(t, func(method string, params json.RawMessage) (any, string) {
		return evalReplyValue(map[string]any{"ok": map[string]any{"id": "1", "success": false, "error": "no element"}}), ""
	})
	res, err := tgt.execDOM(context.Background(), "", domCommand{ID: "1", Action: "find"})
	if err != nil {
		t.Fatalf("execDOM returned a Go error for a failed command: %v", err)
	}
	if res.Success || res.Error != "no element" {
		t.Fatalf("result = %+v, want Success=false with the page's error", res)
	}
}

// TestCallKitInstallsFormKitOnce mirrors the exec test for the form kit, and
// checks the install wraps the object literal without its trailing semicolon.
func TestCallKitInstallsFormKitOnce(t *testing.T) {
	var evals []string
	tgt, _ := newPageTarget(t, func(method string, params json.RawMessage) (any, string) {
		var p struct {
			Expression string `json:"expression"`
		}
		_ = json.Unmarshal(params, &p)
		evals = append(evals, p.Expression)
		switch len(evals) {
		case 1:
			return sentinelEnvelope(), ""
		case 2:
			return map[string]any{"result": map[string]any{"subtype": "undefined"}}, ""
		default:
			return evalReplyValue(map[string]any{"ok": map[string]any{"path": "#x"}}), ""
		}
	})
	raw, err := tgt.callKit(context.Background(), "", "describe", map[string]any{"a": 1})
	if err != nil {
		t.Fatalf("callKit: %v", err)
	}
	if string(raw) != `{"path":"#x"}` {
		t.Errorf("callKit result = %s", raw)
	}
	if len(evals) != 3 {
		t.Fatalf("evaluate calls = %d, want 3", len(evals))
	}
	if !strings.HasPrefix(evals[1], "window.__htrcliFormKit = (\n") {
		t.Errorf("form kit install prefix wrong: %q", evals[1][:min(len(evals[1]), 60)])
	}
	if strings.Contains(evals[1], ";\n)") {
		t.Errorf("form kit install still carries the trailing semicolon")
	}
}

// TestResolveFrameMapsIframeToMainWorldContext records execution contexts from
// events (an isolated world must be ignored), then resolves an iframe to its
// main-world context.
func TestResolveFrameMapsIframeToMainWorldContext(t *testing.T) {
	tgt, s := newPageTarget(t, func(method string, params json.RawMessage) (any, string) {
		switch method {
		case "Runtime.evaluate":
			var p struct {
				ReturnByValue bool `json:"returnByValue"`
			}
			_ = json.Unmarshal(params, &p)
			if p.ReturnByValue {
				return frameCheckOK(), ""
			}
			return map[string]any{"result": map[string]any{"type": "object", "subtype": "node", "objectId": "obj-f"}}, ""
		case "DOM.describeNode":
			return map[string]any{"node": map[string]any{"frameId": "F1"}}, ""
		}
		return map[string]any{}, ""
	})
	tgt.startContextTracking()
	s.inject("Runtime.executionContextCreated", map[string]any{
		"context": map[string]any{"id": 42, "auxData": map[string]any{"frameId": "F1", "isDefault": true}},
	})
	s.inject("Runtime.executionContextCreated", map[string]any{
		"context": map[string]any{"id": 43, "auxData": map[string]any{"frameId": "F1", "isDefault": false}},
	})
	waitFor(t, func() bool { _, ok := tgt.frameCtx.lookup("F1"); return ok })

	got, err := tgt.resolveFrame(context.Background(), "#f")
	if err != nil {
		t.Fatalf("resolveFrame: %v", err)
	}
	if got != 42 {
		t.Fatalf("context = %d, want 42 (the default world, not the isolated 43)", got)
	}
}

// TestResolveFrameCrossOriginNamesOpenHint: the validation script reports the
// cross-origin case, and the error carries the open-the-frame-directly hint.
func TestResolveFrameCrossOriginNamesOpenHint(t *testing.T) {
	var expr string
	tgt, _ := newPageTarget(t, func(method string, params json.RawMessage) (any, string) {
		var p struct {
			Expression string `json:"expression"`
		}
		_ = json.Unmarshal(params, &p)
		expr = p.Expression
		return evalReplyValue(map[string]any{"frameError": `frame "#x" is a cross-origin iframe (src="https://other.example/f"); ` +
			`its document is not reachable from the parent page. Open the frame directly instead: navigate a tab to https://other.example/f`}), ""
	})
	_, err := tgt.resolveFrame(context.Background(), "#x")
	if err == nil || !strings.Contains(err.Error(), "cross-origin") || !strings.Contains(err.Error(), "Open the frame directly instead") {
		t.Fatalf("err = %v, want the cross-origin error with the open hint", err)
	}
	if !strings.Contains(expr, "Open the frame directly instead") {
		t.Errorf("validation script does not carry the cross-origin hint")
	}
}

// TestResolveFrameRejectsNonIframeAndLoading covers the other lookup failures.
func TestResolveFrameRejectsNonIframeAndLoading(t *testing.T) {
	cases := map[string]string{
		"not an iframe": `frame "#d" matches a <div>, not an <iframe>`,
		"loading":       `frame "#f" is still loading; wait for it and retry`,
		"no match":      `frame "#n": no element matches it in the top-level document`,
	}
	for name, msg := range cases {
		t.Run(name, func(t *testing.T) {
			tgt, _ := newPageTarget(t, func(method string, params json.RawMessage) (any, string) {
				return evalReplyValue(map[string]any{"frameError": msg}), ""
			})
			_, err := tgt.resolveFrame(context.Background(), "#x")
			if err == nil || err.Error() != msg {
				t.Fatalf("err = %v, want %q", err, msg)
			}
		})
	}
}

// TestResolveFrameWithoutRecordedContextIsError: a frame whose main-world context
// was never announced to this tab is an error, not a guess.
func TestResolveFrameWithoutRecordedContextIsError(t *testing.T) {
	tgt, _ := newPageTarget(t, func(method string, params json.RawMessage) (any, string) {
		switch method {
		case "Runtime.evaluate":
			var p struct {
				ReturnByValue bool `json:"returnByValue"`
			}
			_ = json.Unmarshal(params, &p)
			if p.ReturnByValue {
				return frameCheckOK(), ""
			}
			return map[string]any{"result": map[string]any{"subtype": "node", "objectId": "obj-f"}}, ""
		case "DOM.describeNode":
			return map[string]any{"node": map[string]any{"frameId": "F9"}}, ""
		}
		return map[string]any{}, ""
	})
	_, err := tgt.resolveFrame(context.Background(), "#f")
	if err == nil || !strings.Contains(err.Error(), "no main-world execution context recorded for frame F9") {
		t.Fatalf("err = %v, want the missing-context error", err)
	}
}

// TestExecutionContextDestroyedForgetsFrame: when the context a frame used is
// destroyed, the frame no longer resolves to it.
func TestExecutionContextDestroyedForgetsFrame(t *testing.T) {
	tgt, s := newPageTarget(t, func(method string, params json.RawMessage) (any, string) { return map[string]any{}, "" })
	tgt.startContextTracking()
	s.inject("Runtime.executionContextCreated", map[string]any{
		"context": map[string]any{"id": 7, "auxData": map[string]any{"frameId": "F2", "isDefault": true}},
	})
	waitFor(t, func() bool { _, ok := tgt.frameCtx.lookup("F2"); return ok })
	s.inject("Runtime.executionContextDestroyed", map[string]any{"executionContextId": 7})
	waitFor(t, func() bool { _, ok := tgt.frameCtx.lookup("F2"); return !ok })
}

// waitFor polls cond until it holds or two seconds pass (events arrive on the
// reader goroutine, so a test must wait for them before asserting).
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not reached within 2s")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestResolveRefStaleCases: every way a ref can point at a gone node is a
// "stale ref" error; other CDP failures pass through unlabelled.
func TestResolveRefStaleCases(t *testing.T) {
	cases := []struct {
		name      string
		resolve   any
		resolveEr string
		connected any
		wantStale bool
		wantErr   bool
	}{
		{name: "resolves to nothing", resolve: map[string]any{"object": map[string]any{"subtype": "null"}}, wantStale: true, wantErr: true},
		{name: "cdp no node", resolveEr: "No node with given id found", wantStale: true, wantErr: true},
		{name: "removed from document", resolve: map[string]any{"object": map[string]any{"objectId": "o-1", "subtype": "node"}}, connected: false, wantStale: true, wantErr: true},
		{name: "other cdp error passes through", resolveEr: "Target closed", wantErr: true},
		{name: "live", resolve: map[string]any{"object": map[string]any{"objectId": "o-1", "subtype": "node"}}, connected: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tgt, _ := newPageTarget(t, func(method string, params json.RawMessage) (any, string) {
				switch method {
				case "DOM.resolveNode":
					return tc.resolve, tc.resolveEr
				case "Runtime.callFunctionOn":
					return evalReplyValue(tc.connected), ""
				}
				return map[string]any{}, ""
			})
			objectID, err := tgt.resolveRef(context.Background(), 99)
			if tc.wantErr != (err != nil) {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if tc.wantStale != (err != nil && strings.HasPrefix(err.Error(), "stale ref")) {
				t.Fatalf("stale = %v, want %v (err %v)", !tc.wantStale, tc.wantStale, err)
			}
			if err == nil && objectID != "o-1" {
				t.Fatalf("objectID = %q, want o-1", objectID)
			}
		})
	}
}

// TestRefCSSIdentityRejectsDifferentElement: a CSS path that does not resolve
// back to the ref's element is refused, never acted on.
func TestRefCSSIdentityRejectsDifferentElement(t *testing.T) {
	for _, same := range []bool{true, false} {
		tgt, _ := newPageTarget(t, func(method string, params json.RawMessage) (any, string) {
			return evalReplyValue(same), ""
		})
		err := tgt.refCSSIdentity(context.Background(), "o-1", "button.save")
		if same && err != nil {
			t.Fatalf("same element: err = %v", err)
		}
		if !same && (err == nil || !strings.Contains(err.Error(), "refusing")) {
			t.Fatalf("different element: err = %v, want refusal", err)
		}
	}
}

// TestRefTableMintDedupesAndRejectsUnknown: one node keeps one ref across mints,
// distinct nodes get distinct refs, and an unknown ref is an error.
func TestRefTableMintDedupesAndRejectsUnknown(t *testing.T) {
	var refs RefTable
	a := refs.Mint(101)
	if again := refs.Mint(101); again != a {
		t.Fatalf("second mint of the same node = %q, want %q", again, a)
	}
	b := refs.Mint(202)
	if a == b || a != "@e1" || b != "@e2" {
		t.Fatalf("refs = %q, %q, want @e1 and @e2", a, b)
	}
	id, err := refs.Lookup(b)
	if err != nil || id != 202 {
		t.Fatalf("Lookup(%s) = %d, %v; want 202", b, id, err)
	}
	if _, err := refs.Lookup("@e9"); err == nil || !strings.Contains(err.Error(), "unknown ref") {
		t.Fatalf("Lookup(@e9) err = %v, want unknown ref", err)
	}
}

// axRootWithChildren builds an accessibility tree: a document root with n
// children, each child named by name(i) and carrying backendDOMNodeId i+1.
func axRootWithChildren(n int, role string, name func(i int) string) []any {
	nodes := []any{map[string]any{
		"nodeId": "0", "role": map[string]any{"value": "RootWebArea"}, "name": map[string]any{"value": "Test page"},
		"childIds": childIDs(n),
	}}
	for i := 0; i < n; i++ {
		nodes = append(nodes, map[string]any{
			"nodeId": fmt.Sprint(i + 1), "backendDOMNodeId": i + 1,
			"role": map[string]any{"value": role}, "name": map[string]any{"value": name(i)},
		})
	}
	return nodes
}

func childIDs(n int) []string {
	ids := make([]string, n)
	for i := range ids {
		ids[i] = fmt.Sprint(i + 1)
	}
	return ids
}

// TestSnapshotCapsElementsWithTruncationMarker: a tree larger than the element
// cap is cut at the cap, and the text says so.
func TestSnapshotCapsElementsWithTruncationMarker(t *testing.T) {
	tgt, _ := newPageTarget(t, func(method string, params json.RawMessage) (any, string) {
		return map[string]any{"nodes": axRootWithChildren(1000, "button", func(i int) string { return fmt.Sprintf("b%d", i) })}, ""
	})
	var refs RefTable
	text, minted, err := tgt.Snapshot(context.Background(), "", &refs)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if !strings.Contains(text, "[truncated: element cap of 400 reached") {
		t.Fatalf("no element-cap marker in output tail: %q", text[max(0, len(text)-120):])
	}
	if len(minted) != snapshotMaxElements-1 {
		t.Fatalf("minted refs = %d, want %d (the document root carries none)", len(minted), snapshotMaxElements-1)
	}
	if !strings.HasPrefix(text, `document "Test page"`) || !strings.Contains(text, `button "b0" @e1`) {
		t.Fatalf("unexpected layout: %q", text[:min(len(text), 120)])
	}
}

// TestSnapshotCapsBytes: long names hit the byte cap before the element cap, and
// that is reported as the byte cap.
func TestSnapshotCapsBytes(t *testing.T) {
	long := strings.Repeat("x", 300)
	tgt, _ := newPageTarget(t, func(method string, params json.RawMessage) (any, string) {
		return map[string]any{"nodes": axRootWithChildren(200, "link", func(int) string { return long })}, ""
	})
	var refs RefTable
	text, minted, err := tgt.Snapshot(context.Background(), "", &refs)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if !strings.Contains(text, "[truncated: byte cap of 32768 bytes reached") {
		t.Fatalf("no byte-cap marker: %q", text[max(0, len(text)-120):])
	}
	if len(text) > snapshotMaxBytes+200 {
		t.Fatalf("text = %d bytes, cap %d", len(text), snapshotMaxBytes)
	}
	if len(minted) >= 200 {
		t.Fatalf("minted %d refs; the cap should have stopped the tree early", len(minted))
	}
}

// TestFindElementsCapsAndNotesAmbiguousPath: matches beyond max are counted and
// flagged, the first resolvable match gets a ref, and a path matching two
// elements gets a note instead of a ref.
func TestFindElementsCapsAndNotesAmbiguousPath(t *testing.T) {
	findAll := []any{
		map[string]any{"tag": "button", "text": "Save", "selector": "#a"},
		map[string]any{"tag": "input", "text": "", "selector": "div > input"},
		map[string]any{"tag": "p", "text": "third", "selector": "p"},
	}
	evalReplies := []any{
		execEnvelope(findAll), // findAll through the bundle
		map[string]any{"result": map[string]any{"subtype": "node", "objectId": "o-a"}}, // #a unique
		map[string]any{"result": map[string]any{"subtype": "null"}},                    // div > input not unique
		evalReplyValue(map[string]any{"ok": 2}),                                        // its match count
	}
	tgt, _ := newPageTarget(t, func(method string, params json.RawMessage) (any, string) {
		switch method {
		case "Runtime.evaluate":
			if len(evalReplies) == 0 {
				return nil, "unexpected Runtime.evaluate: the scripted replies ran out"
			}
			next := evalReplies[0]
			evalReplies = evalReplies[1:]
			return next, ""
		case "DOM.describeNode":
			return map[string]any{"node": map[string]any{"backendNodeId": 11}}, ""
		}
		return map[string]any{}, ""
	})
	var refs RefTable
	out, err := tgt.FindElements(context.Background(), "", "button, input, p", 2, &refs)
	if err != nil {
		t.Fatalf("FindElements: %v", err)
	}
	if out.Total != 3 || !out.Truncated || len(out.Elements) != 2 {
		t.Fatalf("output = %+v, want total 3, truncated, 2 elements", out)
	}
	if out.Elements[0].Ref != "@e1" || out.Elements[0].Tag != "button" {
		t.Errorf("first element = %+v, want @e1 button", out.Elements[0])
	}
	if out.Elements[1].Ref != "" || !strings.Contains(out.Elements[1].Note, "matches 2 elements") {
		t.Errorf("ambiguous element = %+v, want no ref and a note", out.Elements[1])
	}
	if len(evalReplies) != 0 {
		t.Errorf("%d unexpected evaluations; the element past max must not be resolved", len(evalReplies))
	}
}

func TestParseFindSelectorForms(t *testing.T) {
	ok := map[string]domTarget{
		"button.save":   {Selector: "button.save"},
		"label=Name":    {Label: "Name"},
		"text=Save":     {Text: "Save"},
		"name=fullname": {Name: "fullname"},
		"label=a=b":     {Label: "a=b"},
	}
	for arg, want := range ok {
		got, err := parseFindSelector(arg)
		if err != nil || got != want {
			t.Errorf("parseFindSelector(%q) = %+v, %v; want %+v", arg, got, err, want)
		}
	}
	for _, bad := range []string{"", "@e3", "xy=1,2", "role=button", "id=x", "xpath=//a"} {
		if _, err := parseFindSelector(bad); err == nil {
			t.Errorf("parseFindSelector(%q) accepted an unsupported form", bad)
		}
	}
}

func TestWaitRejectsInvalidSpecs(t *testing.T) {
	tgt, _ := newPageTarget(t, func(method string, params json.RawMessage) (any, string) { return map[string]any{}, "" })
	cases := map[string]WaitSpec{
		"neither":      {Timeout: time.Second},
		"both":         {Selector: "a", Text: "b", Timeout: time.Second},
		"zero timeout": {Text: "b"},
		"over cap":     {Text: "b", Timeout: 31 * time.Second},
	}
	for name, spec := range cases {
		if err := tgt.Wait(context.Background(), "", spec); err == nil {
			t.Errorf("%s: Wait accepted an invalid spec", name)
		}
	}
}

func TestWaitTextSucceedsOnFirstPoll(t *testing.T) {
	tgt, s := newPageTarget(t, func(method string, params json.RawMessage) (any, string) {
		return execOrTextTrue(method)
	})
	if err := tgt.Wait(context.Background(), "", WaitSpec{Text: "Save", Timeout: 2 * time.Second}); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if s.callCount("Runtime.evaluate") != 1 {
		t.Errorf("polls = %d, want 1", s.callCount("Runtime.evaluate"))
	}
}

func execOrTextTrue(method string) (any, string) {
	if method == "Runtime.evaluate" {
		return evalReplyValue(map[string]any{"ok": true}), ""
	}
	return map[string]any{}, ""
}

// TestWaitGoneTimesOutWhileStillPresent: an element that never leaves is a
// timeout error, not success.
func TestWaitGoneTimesOutWhileStillPresent(t *testing.T) {
	tgt, _ := newPageTarget(t, func(method string, params json.RawMessage) (any, string) {
		return execOrTextTrue(method)
	})
	err := tgt.Wait(context.Background(), "", WaitSpec{Text: "Save", Gone: true, Timeout: 150 * time.Millisecond})
	if err == nil || !strings.Contains(err.Error(), "timeout after") {
		t.Fatalf("err = %v, want timeout", err)
	}
}

// TestWaitSelectorGoneSucceedsWhenNothingMatches: an empty findAll satisfies
// --gone.
func TestWaitSelectorGoneSucceedsWhenNothingMatches(t *testing.T) {
	tgt, _ := newPageTarget(t, func(method string, params json.RawMessage) (any, string) {
		if method == "Runtime.evaluate" {
			return execEnvelope([]any{}), ""
		}
		return map[string]any{}, ""
	})
	if err := tgt.Wait(context.Background(), "", WaitSpec{Selector: "#gone", Gone: true, Timeout: 2 * time.Second}); err != nil {
		t.Fatalf("Wait: %v", err)
	}
}

// TestWaitPollsThroughFrameNotReadyAndFailsFastOnCrossOrigin: a frame that is
// not ready is waited for until the timeout (the error still says why), while a
// cross-origin frame fails on the first poll without waiting.
func TestWaitPollsThroughFrameNotReadyAndFailsFastOnCrossOrigin(t *testing.T) {
	tgt, _ := newPageTarget(t, func(method string, params json.RawMessage) (any, string) {
		return evalReplyValue(map[string]any{"frameNotReady": "frame \"#f\" is still loading"}), ""
	})
	err := tgt.Wait(context.Background(), "#f", WaitSpec{Text: "x", Timeout: 150 * time.Millisecond})
	if err == nil || !errors.Is(err, errFrameNotReady) || !strings.Contains(err.Error(), "timeout after") {
		t.Fatalf("not-ready frame: err = %v, want a timeout wrapping errFrameNotReady", err)
	}

	tgt, _ = newPageTarget(t, func(method string, params json.RawMessage) (any, string) {
		return evalReplyValue(map[string]any{"frameError": "frame \"#x\" is a cross-origin iframe"}), ""
	})
	start := time.Now()
	err = tgt.Wait(context.Background(), "#x", WaitSpec{Text: "x", Timeout: 5 * time.Second})
	if err == nil || errors.Is(err, errFrameNotReady) || time.Since(start) > time.Second {
		t.Fatalf("cross-origin frame: err = %v after %s, want an immediate final error", err, time.Since(start))
	}
}
