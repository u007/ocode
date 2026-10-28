package cdp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
)

// fakeEl is one element of the fake page, keyed by its CSS path. Its fields are
// what the page reports and what the actions change.
type fakeEl struct {
	backendID int64
	probe     elementProbe
	count     int // matches of path in the top document (default 1 when present)
	value     string
	checked   bool
	files     []string
	options   []selectOption
	selValue  string
	combo     [][]string // successive comboOptions replies; the last one repeats
	comboIdx  int
	comboText string // what comboValue reports
}

// fakePage answers the page-side calls the interact code makes, from a small
// model: elements by CSS path, the bundle's findAll by selector, and the
// nodes CDP can resolve. It is driven by scriptedChrome through handle.
type fakePage struct {
	mu sync.Mutex
	// els is keyed by CSS path; findAll maps a selector to the paths it matches.
	els     map[string]*fakeEl
	findAll map[string][]string
	// live maps a backendNodeId to the path refPathFn reports for it.
	live map[int64]string
	// hit answers the click hit test when covered is nil.
	hit bool
	// covered, when set, says whether an overlay sits at a point; the hit test
	// then answers "the target is on top" as !covered(x, y).
	covered func(x, y float64) bool
	// contentQuads is what DOM.getContentQuads reports (border boxes, one per
	// fragment); boxContent is the content quad DOM.getBoxModel reports. Nil means
	// the default box, a 20x10 box centred on (20,15).
	contentQuads [][]float64
	boxContent   []float64
	// hitPoints records every point the hit test was asked about, as "x,y".
	hitPoints []string
	// ignoreWrites makes fill/check/select do nothing, to test read-back.
	ignoreWrites bool
	// crossFrame makes the frame check report a cross-origin iframe.
	crossFrame bool
	// focusPath is the element Input.insertText types into.
	focusPath string

	actions  []string // "action path" for every bundle write, in order
	clicks   []string // "type x,y" for every mouse event
	keys     []string
	inserted []string
	setFiles []map[string]any
}

func newFakePage(t *testing.T, p *fakePage) *Target {
	t.Helper()
	tgt, _ := newPageTarget(t, p.handle)
	return tgt
}

// jsonQuote is the form a path takes inside an expression built by encoding/json.
func jsonQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// scopedArgs returns the JSON arguments of a runScopedIn wrapper. scopedExpr
// writes them as `(...[ARGS])` right before `)}; }`.
func scopedArgs(expr string) []json.RawMessage {
	start := strings.Index(expr, "(...[")
	end := strings.Index(expr, ")}; }")
	if start < 0 || end < start {
		return nil
	}
	var args []json.RawMessage
	if err := json.Unmarshal([]byte(expr[start+4:end]), &args); err != nil {
		return nil
	}
	return args
}

func argString(args []json.RawMessage, i int) string {
	var s string
	if i < len(args) {
		_ = json.Unmarshal(args[i], &s)
	}
	return s
}

// findEl returns the element a path names, or nil when the page has none.
func (p *fakePage) findEl(path string) *fakeEl {
	return p.els[path]
}

// elFromExpr finds the element an expression names: the first path key whose
// JSON form appears in the expression.
func (p *fakePage) elFromExpr(expr string) (string, *fakeEl) {
	for path, el := range p.els {
		if strings.Contains(expr, jsonQuote(path)) {
			return path, el
		}
	}
	return "", nil
}

func (p *fakePage) handle(method string, params json.RawMessage) (any, string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	switch method {
	case "DOM.resolveNode":
		var a struct {
			BackendNodeID int64 `json:"backendNodeId"`
		}
		_ = json.Unmarshal(params, &a)
		if _, ok := p.live[a.BackendNodeID]; !ok {
			return nil, "No node with given id found"
		}
		return map[string]any{"object": map[string]any{"objectId": fmt.Sprintf("o-%d", a.BackendNodeID), "subtype": "node"}}, ""
	case "Runtime.callFunctionOn":
		var a struct {
			FunctionDeclaration string `json:"functionDeclaration"`
			ObjectID            string `json:"objectId"`
		}
		_ = json.Unmarshal(params, &a)
		var backend int64
		_, _ = fmt.Sscanf(a.ObjectID, "o-%d", &backend)
		switch {
		case strings.Contains(a.FunctionDeclaration, "nth-of-type"):
			path, ok := p.live[backend]
			if !ok {
				return nil, "fake: no path for node"
			}
			return evalReplyValue(path), ""
		case strings.Contains(a.FunctionDeclaration, "isConnected"):
			return evalReplyValue(true), ""
		case strings.Contains(a.FunctionDeclaration, "document.querySelector(p) === this"):
			return evalReplyValue(true), ""
		}
		return nil, "fake: unhandled callFunctionOn"
	case "DOM.describeNode":
		var a struct {
			ObjectID string `json:"objectId"`
		}
		_ = json.Unmarshal(params, &a)
		if a.ObjectID == fakeFrameObjectID {
			return map[string]any{"node": map[string]any{"frameId": fakeFrameID}}, ""
		}
		var backend int64
		_, _ = fmt.Sscanf(a.ObjectID, "o-%d", &backend)
		return map[string]any{"node": map[string]any{"backendNodeId": backend}}, ""
	case "DOM.getContentQuads":
		return map[string]any{"quads": p.quadsOrDefault()}, ""
	case "DOM.getBoxModel":
		return map[string]any{"model": map[string]any{"content": p.boxContentOrDefault()}}, ""
	case "DOM.scrollIntoViewIfNeeded":
		return map[string]any{}, ""
	case "DOM.setFileInputFiles":
		var a struct {
			BackendNodeID int64    `json:"backendNodeId"`
			Files         []string `json:"files"`
		}
		_ = json.Unmarshal(params, &a)
		p.setFiles = append(p.setFiles, map[string]any{"backendNodeId": a.BackendNodeID, "files": a.Files})
		for _, el := range p.els {
			if el.backendID == a.BackendNodeID {
				el.files = a.Files
			}
		}
		return map[string]any{}, ""
	case "Input.dispatchMouseEvent":
		var a struct {
			Type string  `json:"type"`
			X    float64 `json:"x"`
			Y    float64 `json:"y"`
		}
		_ = json.Unmarshal(params, &a)
		p.clicks = append(p.clicks, fmt.Sprintf("%s %.0f,%.0f", a.Type, a.X, a.Y))
		return map[string]any{}, ""
	case "Input.dispatchKeyEvent":
		var a struct {
			Type string `json:"type"`
			Key  string `json:"key"`
		}
		_ = json.Unmarshal(params, &a)
		if a.Type == "rawKeyDown" || a.Type == "keyDown" {
			p.keys = append(p.keys, a.Key)
		}
		return map[string]any{}, ""
	case "Input.insertText":
		var a struct {
			Text string `json:"text"`
		}
		_ = json.Unmarshal(params, &a)
		p.inserted = append(p.inserted, a.Text)
		if el := p.findEl(p.focusPath); el != nil && !p.ignoreWrites {
			el.value += a.Text
		}
		return map[string]any{}, ""
	case "Runtime.evaluate":
		var a struct {
			Expression string `json:"expression"`
		}
		_ = json.Unmarshal(params, &a)
		return p.evaluate(a.Expression)
	}
	return map[string]any{}, ""
}

// execEnvelopeOK is a bundle reply: success with data.
func execEnvelopeOK(data any) (any, string) { return execEnvelope(data), "" }

// execEnvelopeFail is a bundle reply the page rejected.
func execEnvelopeFail(msg string) (any, string) {
	return evalReplyValue(map[string]any{"ok": map[string]any{"id": "x", "success": false, "error": msg}}), ""
}

// envelope wraps a value for runScopedIn (its {ok: value} envelope).
func envelope(v any) (any, string) { return evalReplyValue(map[string]any{"ok": v}), "" }

func (p *fakePage) evaluate(expr string) (any, string) {
	args := scopedArgs(expr)
	switch {
	case strings.Contains(expr, "interact:probe"):
		_, el := p.elFromExpr(expr)
		if el == nil {
			return nil, "fake: probe of an unknown element"
		}
		return envelope(el.probe)

	case strings.Contains(expr, "__htrcliDom"):
		var cmd struct {
			Action string `json:"action"`
			Value  string `json:"value"`
			Target struct {
				Selector string `json:"selector"`
			} `json:"target"`
		}
		_ = json.Unmarshal(args[0], &cmd)
		if cmd.Action == "findAll" {
			var out []map[string]any
			for _, path := range p.findAll[argSelector(args)] {
				out = append(out, map[string]any{"selector": path})
			}
			return execEnvelopeOK(out)
		}
		el := p.findEl(cmd.Target.Selector)
		if el == nil {
			return execEnvelopeFail("element " + cmd.Target.Selector + " is not in the document")
		}
		switch cmd.Action {
		case "prepareClick":
			return execEnvelopeOK(map[string]any{"x": 20, "y": 15})
		case "prepareKeys":
			p.focusPath = cmd.Target.Selector
			return execEnvelopeOK(map[string]any{"focused": true})
		case "fill", "clear", "check", "uncheck", "focus":
			p.actions = append(p.actions, cmd.Action+" "+cmd.Target.Selector)
			if cmd.Action == "focus" {
				p.focusPath = cmd.Target.Selector
				return execEnvelopeOK(nil)
			}
			if p.ignoreWrites {
				return execEnvelopeOK(nil)
			}
			switch cmd.Action {
			case "fill":
				el.value = cmd.Value
			case "clear":
				el.value = ""
			case "check":
				el.checked = true
			case "uncheck":
				el.checked = false
			}
			return execEnvelopeOK(nil)
		}
		return execEnvelopeFail("fake: unhandled action " + cmd.Action)

	case strings.Contains(expr, "__htrcliFormKit"):
		method := argString(args, 0)
		var callArgs []json.RawMessage
		if len(args) > 1 {
			_ = json.Unmarshal(args[1], &callArgs)
		}
		path := argString(callArgs, 0)
		el := p.findEl(path)
		if el == nil {
			return nil, "fake: form kit call on an unknown element " + path
		}
		switch method {
		case "comboPrepare":
			return envelope(map[string]any{"focused": true})
		case "comboOptions":
			if len(el.combo) == 0 {
				return envelope([]string{})
			}
			idx := el.comboIdx
			if idx >= len(el.combo) {
				idx = len(el.combo) - 1
			}
			el.comboIdx++
			return envelope(el.combo[idx])
		case "optionPoint":
			return envelope(map[string]any{"x": 10, "y": 20})
		case "comboValue":
			return envelope(map[string]any{"text": el.comboText, "from": "fake"})
		case "selectOptions":
			return envelope(el.options)
		case "readSelect":
			text := ""
			for _, o := range el.options {
				if o.Value == el.selValue {
					text = o.Text
				}
			}
			return envelope(map[string]any{"value": el.selValue, "text": text})
		case "readField":
			switch argString(callArgs, 1) {
			case kindText:
				return envelope(el.value)
			case kindCheckbox, kindRadio:
				return envelope(el.checked)
			case kindFile:
				names := []string{}
				for _, f := range el.files {
					names = append(names, baseName(f))
				}
				return envelope(names)
			case kindSelect:
				text := ""
				for _, o := range el.options {
					if o.Value == el.selValue {
						text = o.Text
					}
				}
				return envelope(map[string]any{"value": el.selValue, "text": text})
			case kindCombobox:
				return envelope(el.comboText)
			}
		}
		return nil, "fake: unhandled form kit method " + method

	case strings.Contains(expr, "els.length === 1"):
		_, el := p.elFromExpr(expr)
		if el == nil || el.count != 1 {
			return map[string]any{"result": map[string]any{"type": "object", "subtype": "null"}}, ""
		}
		return map[string]any{"result": map[string]any{"type": "object", "subtype": "node", "objectId": fmt.Sprintf("o-%d", el.backendID)}}, ""

	case strings.Contains(expr, "querySelectorAll(p).length"):
		_, el := p.elFromExpr(expr)
		n := 0
		if el != nil {
			n = el.count
		}
		return envelope(n)

	case strings.Contains(expr, "HTMLSelectElement.prototype"):
		path := argString(args, 0)
		if p.ignoreWrites {
			return envelope(true)
		}
		el := p.findEl(path)
		if el == nil {
			return nil, "fake: select on an unknown element"
		}
		p.actions = append(p.actions, "select "+path)
		el.selValue = argString(args, 1)
		return envelope(true)

	case strings.Contains(expr, "e.checked : e.getAttribute"):
		el := p.findEl(argString(args, 0))
		if el == nil {
			return nil, "fake: checked read of an unknown element"
		}
		return envelope(el.checked)

	case strings.Contains(expr, "h === e || e.contains(h)"):
		x, y := argFloat(args, 1), argFloat(args, 2)
		p.hitPoints = append(p.hitPoints, fmt.Sprintf("%.0f,%.0f", x, y))
		if p.covered != nil {
			return envelope(!p.covered(x, y))
		}
		return envelope(p.hit)

	case strings.Contains(expr, "window.innerWidth"):
		return evalReplyValue(map[string]any{"w": 1280, "h": 800}), ""

	case strings.Contains(expr, "const srcOrigin"):
		if p.crossFrame {
			return evalReplyValue(map[string]any{"frameError": `frame "#cross" is a cross-origin iframe (src="http://other"); its document is not reachable from the parent page`}), ""
		}
		return evalReplyValue(map[string]any{"ok": true}), ""

	case strings.HasPrefix(expr, "document.querySelector("):
		// resolveFrameID resolves the <iframe> element to a remote object.
		return map[string]any{"result": map[string]any{"type": "object", "subtype": "node", "objectId": fakeFrameObjectID}}, ""
	}
	return nil, "fake: unhandled expression: " + clipText(expr, 120)
}

// argFloat reads a numeric command argument, or 0 when absent.
func argFloat(args []json.RawMessage, i int) float64 {
	var f float64
	if i < len(args) {
		_ = json.Unmarshal(args[i], &f)
	}
	return f
}

// defaultBox is the box the fake reports when a test sets none: 20x10, centre (20,15).
var defaultBox = []float64{10, 10, 30, 10, 30, 20, 10, 20}

func (p *fakePage) quadsOrDefault() [][]float64 {
	if p.contentQuads != nil {
		return p.contentQuads
	}
	return [][]float64{defaultBox}
}

func (p *fakePage) boxContentOrDefault() []float64 {
	if p.boxContent != nil {
		return p.boxContent
	}
	return defaultBox
}

// argSelector reads the selector of a findAll command argument.
func argSelector(args []json.RawMessage) string {
	var cmd struct {
		Target struct {
			Selector string `json:"selector"`
		} `json:"target"`
	}
	_ = json.Unmarshal(args[0], &cmd)
	return cmd.Target.Selector
}

func baseName(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

// Common fixture paths. Each is unique in the fake document unless a test says
// otherwise.
const (
	pathSave  = "html > body > button:nth-of-type(1)"
	pathNm    = "html > body > input:nth-of-type(1)"
	pathSel   = "html > body > select:nth-of-type(1)"
	pathBox   = "html > body > input:nth-of-type(2)"
	pathFile  = "html > body > input:nth-of-type(3)"
	pathCombo = "html > body > input:nth-of-type(4)"
)

// actionLog returns the bundle writes a fake page recorded, in order.
func (p *fakePage) actionLog() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.actions...)
}

func (p *fakePage) clickLog() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.clicks...)
}

func TestInteractSelectorClicksTheOneMatch(t *testing.T) {
	page := &fakePage{
		els: map[string]*fakeEl{
			pathSave: {backendID: 7, count: 1, probe: elementProbe{Tag: "button"}},
		},
		findAll: map[string][]string{"#save": {pathSave}},
		live:    map[int64]string{7: pathSave},
		hit:     true,
	}
	tgt, s := newPageTarget(t, page.handle)
	got, err := tgt.ClickElement(context.Background(), "", "#save", nil)
	if err != nil {
		t.Fatalf("ClickElement: %v", err)
	}
	if got != "clicked #save" {
		t.Errorf("result = %q", got)
	}
	if got := strings.Join(page.clickLog(), "|"); got != "mouseMoved 20,15|mousePressed 20,15|mouseReleased 20,15" {
		t.Errorf("click events = %q, want move, press, release at the centre", got)
	}
	if s.callCount("DOM.getBoxModel") != 1 {
		t.Errorf("getBoxModel calls = %d, want 1 (centre of the one match)", s.callCount("DOM.getBoxModel"))
	}
}

func TestInteractClickRefusesCoveredElement(t *testing.T) {
	page := &fakePage{
		els:     map[string]*fakeEl{pathSave: {backendID: 7, count: 1, probe: elementProbe{Tag: "button"}}},
		findAll: map[string][]string{"#save": {pathSave}},
		live:    map[int64]string{7: pathSave},
		hit:     false,
	}
	tgt := newFakePage(t, page)
	_, err := tgt.ClickElement(context.Background(), "", "#save", nil)
	if err == nil || !strings.Contains(err.Error(), "covers the point") {
		t.Fatalf("ClickElement err = %v, want the covered-point refusal", err)
	}
	if n := len(page.clickLog()); n != 0 {
		t.Errorf("a covered element received %d mouse events", n)
	}
}

func TestInteractRefTargetsAndStaleRefIsRejected(t *testing.T) {
	page := &fakePage{
		els:  map[string]*fakeEl{pathSave: {backendID: 7, count: 1, probe: elementProbe{Tag: "button"}}},
		live: map[int64]string{7: pathSave},
		hit:  true,
	}
	tgt, s := newPageTarget(t, page.handle)
	var refs RefTable
	ref := refs.Mint(7)
	if _, err := tgt.ClickElement(context.Background(), "", ref, &refs); err != nil {
		t.Fatalf("ClickElement(ref): %v", err)
	}

	// The node is gone: CDP no longer knows its backendNodeId.
	delete(page.live, 7)
	_, err := tgt.ClickElement(context.Background(), "", ref, &refs)
	if err == nil || !strings.HasPrefix(err.Error(), "stale ref") {
		t.Fatalf("ClickElement(stale ref) err = %v, want a stale ref error", err)
	}
	before := len(page.clickLog())
	if _, err := tgt.FillElement(context.Background(), "", ref, "x", &refs); err == nil || !strings.HasPrefix(err.Error(), "stale ref") {
		t.Fatalf("FillElement(stale ref) err = %v, want a stale ref error", err)
	}
	if len(page.clickLog()) != before || len(page.actionLog()) != 0 {
		t.Errorf("a stale ref still reached the page: clicks=%v actions=%v", page.clickLog(), page.actionLog())
	}
	if s.callCount("Input.dispatchMouseEvent") != 3 {
		t.Errorf("only the first, live click should dispatch; got %d mouse events", s.callCount("Input.dispatchMouseEvent"))
	}
}

func TestInteractRefInFrameRejectedBeforeCDP(t *testing.T) {
	page := &fakePage{}
	tgt, s := newPageTarget(t, page.handle)
	var refs RefTable
	ref := refs.Mint(7)
	_, err := tgt.ClickElement(context.Background(), "#same", ref, &refs)
	if err == nil || !strings.Contains(err.Error(), "cannot be combined with frame") {
		t.Fatalf("ClickElement(ref, frame) err = %v, want the ref/frame refusal", err)
	}
	if len(s.calls) != 0 {
		t.Errorf("a ref with a frame made %d CDP calls, want none", len(s.calls))
	}
}

// TestInteractRefPathMustNameTheRefElement: a ref whose CSS path, evaluated in
// the top document, names a different element is refused rather than acted on.
func TestInteractRefPathMustNameTheRefElement(t *testing.T) {
	page := &fakePage{
		els:  map[string]*fakeEl{pathSave: {backendID: 99, count: 1, probe: elementProbe{Tag: "button"}}},
		live: map[int64]string{7: pathSave},
		hit:  true,
	}
	tgt := newFakePage(t, page)
	var refs RefTable
	ref := refs.Mint(7)
	_, err := tgt.ClickElement(context.Background(), "", ref, &refs)
	if err == nil || !strings.Contains(err.Error(), "names a different element") {
		t.Fatalf("ClickElement err = %v, want the different-element refusal", err)
	}
	if len(page.clickLog()) != 0 || len(page.actionLog()) != 0 {
		t.Errorf("a mismatched ref still acted: clicks=%v actions=%v", page.clickLog(), page.actionLog())
	}
}

func TestInteractSelectorAmbiguousAndNoMatchAreErrors(t *testing.T) {
	page := &fakePage{
		els: map[string]*fakeEl{
			pathNm:  {backendID: 3, count: 1, probe: elementProbe{Tag: "input", Type: "text"}},
			pathBox: {backendID: 4, count: 1, probe: elementProbe{Tag: "input", Type: "text"}},
		},
		findAll: map[string][]string{"input": {pathNm, pathBox}, "#gone": nil},
		live:    map[int64]string{3: pathNm},
	}
	tgt := newFakePage(t, page)
	_, err := tgt.FillElement(context.Background(), "", "input", "x", nil)
	if err == nil || !strings.Contains(err.Error(), "matches 2 elements") {
		t.Errorf("ambiguous selector err = %v, want a matches-2 error", err)
	}
	_, err = tgt.FillElement(context.Background(), "", "#gone", "x", nil)
	if err == nil || !strings.Contains(err.Error(), "no element matches") {
		t.Errorf("no-match selector err = %v, want a no-element error", err)
	}
}

func TestInteractFillWritesAndReadsBack(t *testing.T) {
	page := &fakePage{
		els:     map[string]*fakeEl{pathNm: {backendID: 3, count: 1, probe: elementProbe{Tag: "input", Type: "text"}}},
		findAll: map[string][]string{"#nm": {pathNm}},
		live:    map[int64]string{3: pathNm},
	}
	tgt := newFakePage(t, page)
	got, err := tgt.FillElement(context.Background(), "", "#nm", "Ada", nil)
	if err != nil {
		t.Fatalf("FillElement: %v", err)
	}
	if got != "filled #nm" || page.els[pathNm].value != "Ada" {
		t.Errorf("result %q, value %q", got, page.els[pathNm].value)
	}

	// An empty value clears the field: the bundle's fill reads "" as missing.
	if _, err := tgt.FillElement(context.Background(), "", "#nm", "", nil); err != nil {
		t.Fatalf("FillElement(empty): %v", err)
	}
	if log := page.actionLog(); log[len(log)-1] != "clear "+pathNm {
		t.Errorf("empty fill action = %v, want clear", log)
	}

	// A field whose write does not land is an error, not a silent success.
	page.ignoreWrites = true
	if _, err := tgt.FillElement(context.Background(), "", "#nm", "Bob", nil); err == nil || !strings.Contains(err.Error(), "did not stick") {
		t.Errorf("FillElement(ignored write) err = %v, want a did-not-stick error", err)
	}
}

func TestInteractFillRefusesSelectAndCheckbox(t *testing.T) {
	page := &fakePage{
		els: map[string]*fakeEl{
			pathSel: {backendID: 5, count: 1, probe: elementProbe{Tag: "select"}},
			pathBox: {backendID: 6, count: 1, probe: elementProbe{Tag: "input", Type: "checkbox"}},
		},
		findAll: map[string][]string{"#sel": {pathSel}, "#box": {pathBox}},
		live:    map[int64]string{5: pathSel, 6: pathBox},
	}
	tgt := newFakePage(t, page)
	if _, err := tgt.FillElement(context.Background(), "", "#sel", "x", nil); err == nil || !strings.Contains(err.Error(), "not a text field") {
		t.Errorf("fill on <select> err = %v", err)
	}
	if _, err := tgt.FillElement(context.Background(), "", "#box", "x", nil); err == nil || !strings.Contains(err.Error(), "not a text field") {
		t.Errorf("fill on checkbox err = %v", err)
	}
	if len(page.actionLog()) != 0 {
		t.Errorf("refused fills still wrote: %v", page.actionLog())
	}
}

func TestInteractSetCheckedReadsBack(t *testing.T) {
	page := &fakePage{
		els:     map[string]*fakeEl{pathBox: {backendID: 6, count: 1, probe: elementProbe{Tag: "input", Type: "checkbox"}}},
		findAll: map[string][]string{"#box": {pathBox}},
		live:    map[int64]string{6: pathBox},
	}
	tgt := newFakePage(t, page)
	if _, err := tgt.SetChecked(context.Background(), "", "#box", true, nil); err != nil {
		t.Fatalf("SetChecked(true): %v", err)
	}
	if !page.els[pathBox].checked {
		t.Errorf("checkbox not checked")
	}
	if _, err := tgt.SetChecked(context.Background(), "", "#box", false, nil); err != nil {
		t.Fatalf("SetChecked(false): %v", err)
	}
	if page.els[pathBox].checked {
		t.Errorf("checkbox still checked after uncheck")
	}
}

func TestInteractTypeFocusesThenInsertsText(t *testing.T) {
	page := &fakePage{
		els:     map[string]*fakeEl{pathNm: {backendID: 3, count: 1, probe: elementProbe{Tag: "input", Type: "text"}}},
		findAll: map[string][]string{"#nm": {pathNm}},
		live:    map[int64]string{3: pathNm},
	}
	tgt := newFakePage(t, page)
	got, err := tgt.TypeIntoElement(context.Background(), "", "#nm", "Ada", nil)
	if err != nil {
		t.Fatalf("TypeIntoElement: %v", err)
	}
	if got != "typed 3 chars into #nm" || page.inserted[0] != "Ada" || page.els[pathNm].value != "Ada" {
		t.Errorf("result %q inserted %v value %q", got, page.inserted, page.els[pathNm].value)
	}
	if _, err := tgt.TypeIntoElement(context.Background(), "", "#nm", "", nil); err == nil {
		t.Errorf("empty type text was accepted")
	}
}

func TestInteractSelectByValueAndByText(t *testing.T) {
	page := &fakePage{
		els: map[string]*fakeEl{pathSel: {backendID: 5, count: 1, probe: elementProbe{Tag: "select"}, options: []selectOption{
			{Value: "", Text: "Choose"},
			{Value: "my", Text: "Malaysia"},
			{Value: "sg", Text: "Singapore"},
		}}},
		findAll: map[string][]string{"#country": {pathSel}},
		live:    map[int64]string{5: pathSel},
	}
	tgt := newFakePage(t, page)
	if got, err := tgt.SelectElement(context.Background(), "", "#country", "Malaysia", nil); err != nil || got != `selected "Malaysia" in #country` {
		t.Fatalf("SelectElement(text) = %q, %v", got, err)
	}
	if got, err := tgt.SelectElement(context.Background(), "", "#country", "sg", nil); err != nil || !strings.Contains(got, "Singapore") {
		t.Fatalf("SelectElement(value) = %q, %v", got, err)
	}
	// The empty-valued placeholder is a real option, not a missing value.
	if _, err := tgt.SelectElement(context.Background(), "", "#country", "Choose", nil); err != nil {
		t.Fatalf("SelectElement(empty value): %v", err)
	}
	if page.els[pathSel].selValue != "" {
		t.Errorf("select value = %q, want the empty placeholder", page.els[pathSel].selValue)
	}
	if _, err := tgt.SelectElement(context.Background(), "", "#country", "Atlantis", nil); err == nil || !strings.Contains(err.Error(), "no option matches") {
		t.Errorf("SelectElement(no match) err = %v", err)
	}
}

func TestInteractSelectRejectsNonSelect(t *testing.T) {
	page := &fakePage{
		els:     map[string]*fakeEl{pathNm: {backendID: 3, count: 1, probe: elementProbe{Tag: "input", Type: "text"}}},
		findAll: map[string][]string{"#nm": {pathNm}},
		live:    map[int64]string{3: pathNm},
	}
	tgt := newFakePage(t, page)
	if _, err := tgt.SelectElement(context.Background(), "", "#nm", "x", nil); err == nil || !strings.Contains(err.Error(), "not a <select>") {
		t.Errorf("SelectElement(input) err = %v", err)
	}
}

// TestInteractSetCheckedRejectsTextInput: checking a text input is refused, not
// written as a checked flag.
func TestInteractSetCheckedRejectsTextInput(t *testing.T) {
	page := &fakePage{
		els:     map[string]*fakeEl{pathNm: {backendID: 3, count: 1, probe: elementProbe{Tag: "input", Type: "text"}}},
		findAll: map[string][]string{"#nm": {pathNm}},
		live:    map[int64]string{3: pathNm},
	}
	tgt := newFakePage(t, page)
	if _, err := tgt.SetChecked(context.Background(), "", "#nm", false, nil); err == nil || !strings.Contains(err.Error(), "needs a checkbox") {
		t.Fatalf("SetChecked(text input) err = %v, want a refusal", err)
	}
	if len(page.actionLog()) != 0 {
		t.Errorf("a refused check still wrote: %v", page.actionLog())
	}
}

// TestInteractTypeNoOpIsAnError: a field whose text never lands is an error,
// even when the field already contained the text before typing.
func TestInteractTypeNoOpIsAnError(t *testing.T) {
	page := &fakePage{
		els:          map[string]*fakeEl{pathNm: {backendID: 3, count: 1, probe: elementProbe{Tag: "input", Type: "text"}, value: "Ada"}},
		findAll:      map[string][]string{"#nm": {pathNm}},
		live:         map[int64]string{3: pathNm},
		ignoreWrites: true,
	}
	tgt := newFakePage(t, page)
	if _, err := tgt.TypeIntoElement(context.Background(), "", "#nm", "Ada", nil); err == nil || !strings.Contains(err.Error(), "did not land") {
		t.Fatalf("TypeIntoElement(no-op) err = %v, want a did-not-land error", err)
	}
}

// The padded button: its content box is x 100..220, y 110..150, and its padding
// runs to x 300. The border box, which DOM.getContentQuads reports, has centre
// (200,130), inside the padding; the content-box centre the click uses is (160,130).
var (
	paddedBorderBox = []float64{100, 100, 300, 100, 300, 160, 100, 160}
	paddedContent   = []float64{100, 110, 220, 110, 220, 150, 100, 150}
)

func paddedButtonPage(covered func(x, y float64) bool) *fakePage {
	return &fakePage{
		els:          map[string]*fakeEl{pathSave: {backendID: 7, count: 1, probe: elementProbe{Tag: "button"}}},
		findAll:      map[string][]string{"#save": {pathSave}},
		live:         map[int64]string{7: pathSave},
		contentQuads: [][]float64{paddedBorderBox},
		boxContent:   paddedContent,
		covered:      covered,
	}
}

// (d) An inline element wrapped onto several lines has one fragment per line. The
// click goes to the centre of the largest fragment in the viewport, never to the
// union box (which may lie on no text), and never to a fragment off screen.
func TestInteractClickInlineFragmentsPicksLargestVisible(t *testing.T) {
	page := &fakePage{
		els:     map[string]*fakeEl{pathSave: {backendID: 7, count: 1, probe: elementProbe{Tag: "span"}}},
		findAll: map[string][]string{"#save": {pathSave}},
		live:    map[int64]string{7: pathSave},
		contentQuads: [][]float64{
			{0, 0, 10, 0, 10, 10, 0, 10},
			{50, 50, 250, 50, 250, 80, 50, 80},
			{60, 2000, 90, 2000, 90, 2030, 60, 2030},
		},
		hit: true,
	}
	tgt, s := newPageTarget(t, page.handle)
	if _, err := tgt.ClickElement(context.Background(), "", "#save", nil); err != nil {
		t.Fatalf("ClickElement: %v", err)
	}
	if got := strings.Join(page.clickLog(), "|"); got != "mouseMoved 150,65|mousePressed 150,65|mouseReleased 150,65" {
		t.Errorf("click events = %q, want the centre of the largest visible fragment (150,65)", got)
	}
	if n := s.callCount("DOM.getBoxModel"); n != 0 {
		t.Errorf("getBoxModel calls = %d, want 0 for a multi-fragment element", n)
	}
}

// overlayAt reports whether a point lies in the overlay box r (x1, y1 exclusive).
func overlayAt(r rect) func(x, y float64) bool {
	return func(x, y float64) bool { return x >= r.x0 && x < r.x1 && y >= r.y0 && y < r.y1 }
}

// (a) An overlay over the padding, where the bounding-rect centre lies, does not
// cover the content-box point the click dispatches at: the click succeeds, and the
// point hit-tested is the point dispatched.
func TestInteractClickHitTestsTheDispatchedPoint(t *testing.T) {
	page := paddedButtonPage(overlayAt(rect{x0: 190, y0: 90, x1: 230, y1: 170}))
	tgt := newFakePage(t, page)
	if _, err := tgt.ClickElement(context.Background(), "", "#save", nil); err != nil {
		t.Fatalf("ClickElement under a padding overlay: %v", err)
	}
	if got := strings.Join(page.hitPoints, "|"); got != "160,130" {
		t.Errorf("hit-test points = %q, want the dispatched point 160,130", got)
	}
	if got := strings.Join(page.clickLog(), "|"); got != "mouseMoved 160,130|mousePressed 160,130|mouseReleased 160,130" {
		t.Errorf("click events = %q", got)
	}
}

// (b) An overlay over exactly the dispatched point refuses the click, and no
// mouse event is sent.
func TestInteractClickRefusedWhenOverlayCoversDispatchedPoint(t *testing.T) {
	page := paddedButtonPage(overlayAt(rect{x0: 150, y0: 120, x1: 170, y1: 140}))
	tgt := newFakePage(t, page)
	_, err := tgt.ClickElement(context.Background(), "", "#save", nil)
	if err == nil || !strings.Contains(err.Error(), "covers the point it would click (160, 130)") {
		t.Fatalf("ClickElement err = %v, want the refusal naming the point 160, 130", err)
	}
	if n := len(page.clickLog()); n != 0 {
		t.Errorf("a covered point received %d mouse events", n)
	}
}

// (c) With nothing over the element, the click lands at the same point.
func TestInteractClickUncoveredPadded(t *testing.T) {
	page := paddedButtonPage(nil)
	page.hit = true
	tgt := newFakePage(t, page)
	if _, err := tgt.ClickElement(context.Background(), "", "#save", nil); err != nil {
		t.Fatalf("ClickElement: %v", err)
	}
	if got := strings.Join(page.hitPoints, "|"); got != "160,130" {
		t.Errorf("hit-test points = %q, want 160,130", got)
	}
	if got := strings.Join(page.clickLog(), "|"); got != "mouseMoved 160,130|mousePressed 160,130|mouseReleased 160,130" {
		t.Errorf("click events = %q", got)
	}
}
