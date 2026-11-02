package cdp

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/u007/ocode/internal/tool"
)

// axNodeMap builds one accessibility node for a stubbed tree.
func axNodeMap(id string, dom int64, role, name string, children ...string) map[string]any {
	n := map[string]any{
		"nodeId": id, "backendDOMNodeId": dom,
		"role": map[string]any{"value": role}, "name": map[string]any{"value": name},
	}
	if len(children) > 0 {
		n["childIds"] = children
	}
	return n
}

// withDisabled marks an accessibility node disabled.
func withDisabled(n map[string]any) map[string]any {
	n["properties"] = []any{map[string]any{"name": "disabled", "value": map[string]any{"value": true}}}
	return n
}

// axNodes converts node maps to the node list a stubbed reply carries.
func axNodes(nodes ...map[string]any) []any {
	out := make([]any, len(nodes))
	for i, n := range nodes {
		out[i] = n
	}
	return out
}

// controlsReply answers the reads Controls makes on a scripted Chrome: the
// settle, the accessibility tree, the URL and the page text. The text reply
// reports textTotal as the full length of the page text.
func controlsReply(nodes []any, settled bool, textTotal int) func(method string, params json.RawMessage) (any, string) {
	return func(method string, params json.RawMessage) (any, string) {
		switch method {
		case "Accessibility.getFullAXTree":
			return map[string]any{"nodes": nodes}, ""
		case "DOMSnapshot.captureSnapshot":
			return map[string]any{"documents": []any{map[string]any{
				"nodes":  map[string]any{"backendNodeId": []any{}},
				"layout": map[string]any{"nodeIndex": []any{}, "bounds": []any{}},
			}}}, ""
		case "Runtime.evaluate":
			var p struct {
				Expression string `json:"expression"`
			}
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, "decoding evaluate params: " + err.Error()
			}
			switch {
			case strings.Contains(p.Expression, "MutationObserver"):
				return evalReplyValue(map[string]any{"settled": settled}), ""
			case p.Expression == controlsWhereExpr:
				return evalReplyValue(map[string]any{"href": "https://shop.test/cart", "vw": 0, "vh": 0}), ""
			case strings.Contains(p.Expression, "innerText"):
				return evalReplyValue(map[string]any{"text": "Cart page", "total": textTotal}), ""
			}
		}
		return nil, "unexpected " + method
	}
}

// controlsOn runs Controls on a stubbed tree whose page has settled and whose
// text is short, and fails the test on error.
func controlsOn(t *testing.T, nodes []any, refs *RefTable) ControlsPage {
	t.Helper()
	tgt, _ := newPageTarget(t, controlsReply(nodes, true, 10))
	page, err := tgt.Controls(context.Background(), refs)
	if err != nil {
		t.Fatalf("Controls: %v", err)
	}
	return page
}

// TestControlsOperationsPerRole: each interactive role offers the operations the
// spec names, and a non-interactive role (heading) is not a control at all.
func TestControlsOperationsPerRole(t *testing.T) {
	want := map[string][]string{
		"button": {"click"}, "link": {"click"}, "tab": {"click"}, "menuitem": {"click"},
		"menuitemcheckbox": {"click"}, "menuitemradio": {"click"}, "option": {"click"}, "treeitem": {"click"},
		"textbox": {"click", "fill"}, "searchbox": {"click", "fill"}, "combobox": {"click", "fill"}, "spinbutton": {"click", "fill"},
		"checkbox": {"check", "uncheck"}, "radio": {"check", "uncheck"}, "switch": {"check", "uncheck"},
	}
	var nodes []map[string]any
	var children []string
	for i, role := range slices.Sorted(maps.Keys(want)) {
		id := fmt.Sprint(i + 1)
		children = append(children, id)
		nodes = append(nodes, axNodeMap(id, int64(i+1), role, role+" name"))
	}
	nodes = append(nodes, axNodeMap("h", 999, "heading", "Not a control"))
	children = append(children, "h")
	all := append([]map[string]any{axNodeMap("0", 0, "RootWebArea", "Roles", children...)}, nodes...)

	var refs RefTable
	page := controlsOn(t, axNodes(all...), &refs)
	if len(page.Controls) != len(want) {
		t.Fatalf("controls = %d, want %d (the heading must not be one)", len(page.Controls), len(want))
	}
	for _, c := range page.Controls {
		if !slices.Equal(c.Operations, want[c.Role]) {
			t.Errorf("%s operations = %v, want %v", c.Role, c.Operations, want[c.Role])
		}
	}
}

// TestControlsContextFromNearestNamedAncestor: a control's context is the name
// of its nearest named ancestor in the context roles. Unnamed containers are
// passed over, and a control under no named context has none.
func TestControlsContextFromNearestNamedAncestor(t *testing.T) {
	nodes := axNodes(
		axNodeMap("0", 0, "RootWebArea", "Cart", "2", "6", "10"),
		axNodeMap("2", 0, "region", "Account", "3"),
		axNodeMap("3", 0, "generic", "", "5"),
		axNodeMap("5", 5, "button", "Log out"),
		axNodeMap("6", 0, "form", "Checkout", "7", "8"),
		axNodeMap("7", 0, "heading", "Shipping", "9"),
		axNodeMap("9", 9, "textbox", "Street"),
		axNodeMap("8", 8, "button", "Place order"),
		axNodeMap("10", 10, "button", "Help"),
	)
	var refs RefTable
	page := controlsOn(t, nodes, &refs)
	want := map[string]string{"Log out": "Account", "Street": "Shipping", "Place order": "Checkout", "Help": ""}
	if len(page.Controls) != len(want) {
		t.Fatalf("controls = %d, want %d", len(page.Controls), len(want))
	}
	for _, c := range page.Controls {
		if c.Context != want[c.Label] {
			t.Errorf("%q context = %q, want %q", c.Label, c.Context, want[c.Label])
		}
	}
}

// TestControlsCapsAndOmitted: the listing keeps controlsMaxElements controls in
// document order, counts the rest, and mints refs only for the kept ones.
func TestControlsCapsAndOmitted(t *testing.T) {
	const total = 200
	nodes := axRootWithChildren(total, "button", func(i int) string { return fmt.Sprintf("b%d", i) })
	var refs RefTable
	page := controlsOn(t, nodes, &refs)
	if len(page.Controls) != controlsMaxElements {
		t.Fatalf("controls = %d, want %d", len(page.Controls), controlsMaxElements)
	}
	if page.Omitted != total-controlsMaxElements {
		t.Fatalf("omitted = %d, want %d", page.Omitted, total-controlsMaxElements)
	}
	if page.Controls[0].Label != "b0" {
		t.Fatalf("first control = %q, want document order from b0", page.Controls[0].Label)
	}
	if _, err := refs.Lookup(fmt.Sprintf("@e%d", controlsMaxElements)); err != nil {
		t.Fatalf("last kept control has no ref: %v", err)
	}
	if _, err := refs.Lookup(fmt.Sprintf("@e%d", controlsMaxElements+1)); err == nil {
		t.Fatalf("an omitted control minted a ref")
	}
}

// TestControlsRefsAreFromTheSharedTable: a control's ref is minted through the
// caller's RefTable and names the control's backendDOMNodeId.
func TestControlsRefsAreFromTheSharedTable(t *testing.T) {
	var refs RefTable
	refs.Mint(999)
	nodes := axRootWithChildren(3, "button", func(i int) string { return fmt.Sprintf("b%d", i) })
	page := controlsOn(t, nodes, &refs)
	if page.Controls[0].Ref != "@e2" {
		t.Fatalf("first ref = %q, want @e2 after the table already held @e1", page.Controls[0].Ref)
	}
	for i, c := range page.Controls {
		id, err := refs.Lookup(c.Ref)
		if err != nil || id != int64(i+1) {
			t.Errorf("Lookup(%s) = %d, %v; want backendDOMNodeId %d", c.Ref, id, err, i+1)
		}
	}
}

// TestControlsDisabledKeptWithoutOperations: a disabled control stays in the
// listing with its ref and state, offers no operations, and serializes its
// operations as [] rather than null.
func TestControlsDisabledKeptWithoutOperations(t *testing.T) {
	nodes := axNodes(
		axNodeMap("0", 0, "RootWebArea", "Pay", "1"),
		withDisabled(axNodeMap("1", 1, "textbox", "Card number")),
	)
	var refs RefTable
	page := controlsOn(t, nodes, &refs)
	if len(page.Controls) != 1 {
		t.Fatalf("controls = %d, want the disabled textbox kept", len(page.Controls))
	}
	c := page.Controls[0]
	if c.Ref == "" || !slices.Contains(c.States, "disabled") || len(c.Operations) != 0 {
		t.Fatalf("disabled control = %+v; want a ref, the disabled state and no operations", c)
	}
	raw, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"operations":[]`) || !strings.Contains(string(raw), `"states":["disabled"]`) {
		t.Fatalf("JSON shape = %s; want operations [] and states [\"disabled\"]", raw)
	}
}

// TestControlsSkipsNodeWithoutDOMNode: an interactive node with no
// backendDOMNodeId cannot be acted on, so it is left out and not counted.
func TestControlsSkipsNodeWithoutDOMNode(t *testing.T) {
	nodes := axNodes(
		axNodeMap("0", 0, "RootWebArea", "Shop", "1", "2"),
		axNodeMap("1", 0, "button", "ghost"),
		axNodeMap("2", 2, "button", "real"),
	)
	var refs RefTable
	page := controlsOn(t, nodes, &refs)
	if len(page.Controls) != 1 || page.Controls[0].Label != "real" || page.Omitted != 0 {
		t.Fatalf("controls = %+v omitted %d; want only the node with a DOM id", page.Controls, page.Omitted)
	}
}

// TestControlsLabelClipped: a label keeps controlLabelMax runes and a marker.
func TestControlsLabelClipped(t *testing.T) {
	nodes := axNodes(
		axNodeMap("0", 0, "RootWebArea", "Shop", "1"),
		axNodeMap("1", 1, "button", strings.Repeat("x", 300)),
	)
	var refs RefTable
	page := controlsOn(t, nodes, &refs)
	label := page.Controls[0].Label
	if got := utf8.RuneCountInString(label); got != controlLabelMax+1 || !strings.HasSuffix(label, "…") {
		t.Fatalf("label has %d runes ending %q; want %d runes and an ellipsis", got, label[len(label)-3:], controlLabelMax+1)
	}
}

// TestControlsValueClipped: a value keeps controlValueMax runes and a marker.
func TestControlsValueClipped(t *testing.T) {
	long := axNodeMap("1", 1, "textbox", "Notes")
	long["value"] = map[string]any{"value": strings.Repeat("v", 300)}
	nodes := axNodes(axNodeMap("0", 0, "RootWebArea", "Form", "1"), long)
	var refs RefTable
	page := controlsOn(t, nodes, &refs)
	value := page.Controls[0].Value
	if got := utf8.RuneCountInString(value); got != controlValueMax+1 || !strings.HasSuffix(value, "…") {
		t.Fatalf("value has %d runes; want %d runes and an ellipsis", got, controlValueMax+1)
	}
}

// TestControlsFingerprintStableAndLabelSensitive: two reads of the same page
// give the same fingerprint even through different ref tables, and a changed
// label gives a different one.
func TestControlsFingerprintStableAndLabelSensitive(t *testing.T) {
	tree := func(name string) []any {
		return axNodes(axNodeMap("0", 0, "RootWebArea", "Cart", "1"), axNodeMap("1", 1, "button", name))
	}
	first := controlsOn(t, tree("Save"), &RefTable{})
	var otherRefs RefTable
	otherRefs.Mint(42)
	second := controlsOn(t, tree("Save"), &otherRefs)
	if first.Controls[0].Ref == second.Controls[0].Ref {
		t.Fatalf("test setup: both reads minted %s, so the refs do not differ", first.Controls[0].Ref)
	}
	if first.Fingerprint != second.Fingerprint {
		t.Fatalf("fingerprints differ for identical pages: %s vs %s", first.Fingerprint, second.Fingerprint)
	}
	if len(first.Fingerprint) != fingerprintHexLen {
		t.Fatalf("fingerprint %q has %d digits, want %d", first.Fingerprint, len(first.Fingerprint), fingerprintHexLen)
	}
	if _, err := hex.DecodeString(first.Fingerprint); err != nil {
		t.Fatalf("fingerprint %q is not hex: %v", first.Fingerprint, err)
	}
	renamed := controlsOn(t, tree("Save now"), &RefTable{})
	if renamed.Fingerprint == first.Fingerprint {
		t.Fatalf("fingerprint did not change when the label changed")
	}
}

// TestControlsFingerprintIncludesPageText: a page that answers an action by
// changing only text must produce a different fingerprint, or a real response
// looks like a stall to the step loop.
func TestControlsFingerprintIncludesPageText(t *testing.T) {
	controls := []Control{{Ref: "@e1", Role: "button", Label: "Save", States: []string{}, Operations: []string{"click"}}}
	empty, err := controlsFingerprint("https://shop.test/cart", "Your cart is empty", controls, 0)
	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}
	again, err := controlsFingerprint("https://shop.test/cart", "Your cart is empty", controls, 0)
	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}
	if empty != again {
		t.Fatalf("fingerprint is not stable for the same page: %s vs %s", empty, again)
	}
	filled, err := controlsFingerprint("https://shop.test/cart", "1 item in your cart", controls, 0)
	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}
	if filled == empty {
		t.Fatal("fingerprint did not change when only the page text changed")
	}
}

// TestControlsViewportTextTruncationFlag: the flag follows whether the page
// text ran past viewportTextMaxChars.
func TestControlsViewportTextTruncationFlag(t *testing.T) {
	nodes := axNodes(axNodeMap("0", 0, "RootWebArea", "Cart"))
	for _, tc := range []struct {
		total     int
		truncated bool
	}{
		{total: 10, truncated: false},
		{total: viewportTextMaxChars, truncated: false},
		{total: viewportTextMaxChars + 1, truncated: true},
	} {
		tgt, _ := newPageTarget(t, controlsReply(nodes, true, tc.total))
		page, err := tgt.Controls(context.Background(), &RefTable{})
		if err != nil {
			t.Fatalf("Controls(total=%d): %v", tc.total, err)
		}
		if page.ViewportTruncated != tc.truncated {
			t.Errorf("total=%d: ViewportTruncated = %v, want %v", tc.total, page.ViewportTruncated, tc.truncated)
		}
	}
}

// TestControlsRefusesOversizedTree: a tree over axNodeBudget is refused with the
// same budget message Snapshot gives, naming controls as the action.
func TestControlsRefusesOversizedTree(t *testing.T) {
	nodes := axRootWithChildren(axNodeBudget, "button", func(int) string { return "b" })
	tgt, _ := newPageTarget(t, controlsReply(nodes, true, 0))
	_, err := tgt.Controls(context.Background(), &RefTable{})
	if err == nil ||
		!strings.Contains(err.Error(), "controls: the accessibility tree has") ||
		!strings.Contains(err.Error(), "use find or text instead of controls") {
		t.Fatalf("Controls over budget err = %v; want the budget refusal", err)
	}
}

// TestControlsPageMetadataAndSettledFlag: the page carries the URL, the title
// from the root node, the settle result and the viewport text.
func TestControlsPageMetadataAndSettledFlag(t *testing.T) {
	nodes := axNodes(axNodeMap("0", 0, "RootWebArea", "Cart", "1"), axNodeMap("1", 1, "link", "Checkout"))
	tgt, _ := newPageTarget(t, controlsReply(nodes, false, 12))
	page, err := tgt.Controls(context.Background(), &RefTable{})
	if err != nil {
		t.Fatalf("Controls: %v", err)
	}
	if page.URL != "https://shop.test/cart" || page.Title != "Cart" || page.ViewportText != "Cart page" {
		t.Errorf("page = url %q title %q text %q", page.URL, page.Title, page.ViewportText)
	}
	if page.Settled {
		t.Errorf("Settled = true, want false from the settle reply")
	}
	if len(page.Controls) != 1 || !slices.Equal(page.Controls[0].Operations, []string{"click"}) {
		t.Errorf("controls = %+v", page.Controls)
	}
}

// TestSettleReturnsThePageFlag: Settle reports the flag the page resolves with.
func TestSettleReturnsThePageFlag(t *testing.T) {
	for _, settled := range []bool{true, false} {
		tgt, _ := newPageTarget(t, func(method string, params json.RawMessage) (any, string) {
			return evalReplyValue(map[string]any{"settled": settled}), ""
		})
		got, err := tgt.Settle(context.Background())
		if err != nil || got != settled {
			t.Fatalf("Settle with page flag %v = %v, %v", settled, got, err)
		}
	}
}

// TestSettleResumesAfterNavigation: a navigation that destroys the evaluated
// context is logged and the settle is evaluated again on the new document.
func TestSettleResumesAfterNavigation(t *testing.T) {
	calls := 0
	tgt, s := newPageTarget(t, func(method string, params json.RawMessage) (any, string) {
		calls++
		if calls == 1 {
			return nil, "Execution context was destroyed, most likely because of a navigation."
		}
		return evalReplyValue(map[string]any{"settled": true}), ""
	})
	settled, err := tgt.Settle(context.Background())
	if err != nil || !settled {
		t.Fatalf("Settle after navigation = %v, %v; want settled", settled, err)
	}
	if n := s.callCount("Runtime.evaluate"); n != 2 {
		t.Fatalf("Runtime.evaluate calls = %d, want 2 (the destroyed attempt, then the retry)", n)
	}
}

// TestSettleReturnsOtherCDPErrorsWithoutRetry: an error that is not a
// navigation is returned at once, not retried.
func TestSettleReturnsOtherCDPErrorsWithoutRetry(t *testing.T) {
	tgt, s := newPageTarget(t, func(method string, params json.RawMessage) (any, string) {
		return nil, "Target closed"
	})
	if _, err := tgt.Settle(context.Background()); err == nil || !strings.Contains(err.Error(), "Target closed") {
		t.Fatalf("Settle err = %v, want the CDP error", err)
	}
	if n := s.callCount("Runtime.evaluate"); n != 1 {
		t.Fatalf("Runtime.evaluate calls = %d, want 1 (no retry)", n)
	}
}

// TestSettleReportsPageException: an exception thrown by the settle script is
// an error, not a settled page.
func TestSettleReportsPageException(t *testing.T) {
	tgt, _ := newPageTarget(t, func(method string, params json.RawMessage) (any, string) {
		return map[string]any{
			"result":           map[string]any{"type": "object"},
			"exceptionDetails": map[string]any{"text": "Uncaught boom"},
		}, ""
	})
	if _, err := tgt.Settle(context.Background()); err == nil || !strings.Contains(err.Error(), "page exception: Uncaught boom") {
		t.Fatalf("Settle err = %v, want the page exception", err)
	}
}

// TestSettleRealChrome: against a real Chrome, Settle waits out a page that
// mutates for 800 ms and then reports it quiet at least one quiet window after
// its last mutation, and a page that never goes quiet hits the bound with
// settled=false. Gated on OCODE_CHROME_PATH like TestPageDOMRealChrome.
func TestSettleRealChrome(t *testing.T) {
	chromePath := os.Getenv("OCODE_CHROME_PATH")
	if chromePath == "" {
		t.Skip("OCODE_CHROME_PATH not set; skipping real-Chrome settle test")
	}
	const busyPage = `<!doctype html><html><body><p>busy</p><script>
const t0 = Date.now();
const id = setInterval(() => {
	document.body.dataset.last = String(Date.now());
	if (Date.now() - t0 >= 800) clearInterval(id);
}, 50);
</script></body></html>`
	const foreverPage = `<!doctype html><html><body><script>
setInterval(() => { document.body.dataset.last = String(Date.now()); }, 50);
</script></body></html>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		switch r.URL.Path {
		case "/busy":
			fmt.Fprint(w, busyPage)
		case "/forever":
			fmt.Fprint(w, foreverPage)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	hostPort := strings.TrimPrefix(srv.URL, "http://")

	sup := tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{GracePeriod: 2 * time.Second})
	m := NewManager(ManagerOptions{
		ChromePath: chromePath,
		Supervisor: sup,
		Dialer:     newTestGuardedDialer(hostPort),
	})
	defer func() {
		cctx, ccancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer ccancel()
		_ = m.Close(cctx)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	tgt, err := m.Attach(ctx, "tab:settle", &recordingSink{})
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}

	if err := tgt.Navigate(ctx, srv.URL+"/busy"); err != nil {
		t.Fatalf("Navigate busy: %v", err)
	}
	settled, err := tgt.Settle(ctx)
	if err != nil || !settled {
		t.Fatalf("Settle(busy) = %v, %v; want settled once the mutations stop", settled, err)
	}
	reply, err := tgt.evaluateIn(ctx, 0, "Date.now() - Number(document.body.dataset.last)", true)
	if err != nil {
		t.Fatalf("reading time since the last mutation: %v", err)
	}
	var sinceMs float64
	if err := json.Unmarshal(reply.Result.Value, &sinceMs); err != nil {
		t.Fatalf("decoding time since the last mutation: %v", err)
	}
	// Settle returns only after a full quiet window, so the last mutation is at
	// least that far back. The slack allows for timer and clock granularity.
	if sinceMs < 150 {
		t.Fatalf("Settle returned %.0f ms after the last mutation; want at least the 200 ms quiet window", sinceMs)
	}

	if err := tgt.Navigate(ctx, srv.URL+"/forever"); err != nil {
		t.Fatalf("Navigate forever: %v", err)
	}
	start := time.Now()
	settled, err = tgt.Settle(ctx)
	elapsed := time.Since(start)
	if err != nil || settled {
		t.Fatalf("Settle(forever) = %v, %v; want settled=false at the bound", settled, err)
	}
	if elapsed < 4*time.Second || elapsed > 15*time.Second {
		t.Fatalf("Settle(forever) took %s; want about the %s bound", elapsed, settleMaxWait)
	}
}

// TestControlsPlacesBoxesAndFlagsTheViewport: each control takes the rect of its
// own DOM node, a rect that overlaps the viewport is in view, a rect wholly below
// it is not, and a node with no rect has no box.
func TestControlsPlacesBoxesAndFlagsTheViewport(t *testing.T) {
	nodes := axNodes(
		axNodeMap("0", 100, "RootWebArea", "Shop", "1", "2", "3", "4"),
		axNodeMap("1", 11, "textbox", "Email"),
		axNodeMap("2", 12, "button", "Buy"),
		axNodeMap("3", 13, "button", "Far below"),
		axNodeMap("4", 14, "button", "No layout"),
	)
	rects := map[int64][]float64{
		11: {10, 20, 200, 30},
		12: {10, 700, 80, 40},
		13: {10, 2000, 80, 40},
	}
	reply := controlsReply(nodes, true, 10)
	handle := func(method string, params json.RawMessage) (any, string) {
		switch method {
		case "DOMSnapshot.captureSnapshot":
			return domSnapshotOf(rects), ""
		case "Runtime.evaluate":
			var p struct {
				Expression string `json:"expression"`
			}
			if err := json.Unmarshal(params, &p); err == nil && p.Expression == controlsWhereExpr {
				return evalReplyValue(map[string]any{"href": "https://shop.test/cart", "vw": 1280, "vh": 800}), ""
			}
		}
		return reply(method, params)
	}
	tgt, _ := newPageTarget(t, handle)
	var refs RefTable
	page, err := tgt.Controls(context.Background(), &refs)
	if err != nil {
		t.Fatalf("Controls: %v", err)
	}
	if page.ViewportWidth != 1280 || page.ViewportHeight != 800 {
		t.Fatalf("viewport = %dx%d, want 1280x800", page.ViewportWidth, page.ViewportHeight)
	}
	byRole := map[string]Control{}
	for _, c := range page.Controls {
		byRole[c.Label] = c
	}
	email := byRole["Email"]
	if email.Box == nil || *email.Box != (ControlBox{X: 10, Y: 20, W: 200, H: 30}) || !email.InViewport {
		t.Fatalf("Email box = %+v in_viewport=%v, want its rect in view", email.Box, email.InViewport)
	}
	buy := byRole["Buy"]
	if buy.Box == nil || *buy.Box != (ControlBox{X: 10, Y: 700, W: 80, H: 40}) || !buy.InViewport {
		t.Fatalf("Buy box = %+v in_viewport=%v, want its rect in view", buy.Box, buy.InViewport)
	}
	far := byRole["Far below"]
	if far.Box == nil || far.InViewport {
		t.Fatalf("Far below box = %+v in_viewport=%v, want a box outside the viewport", far.Box, far.InViewport)
	}
	if none := byRole["No layout"]; none.Box != nil || none.InViewport {
		t.Fatalf("No layout box = %+v in_viewport=%v, want no box", none.Box, none.InViewport)
	}
}

// domSnapshotOf builds a DOMSnapshot reply whose one document lays out the given
// rects, keyed by backend node id.
func domSnapshotOf(rects map[int64][]float64) map[string]any {
	ids := []any{}
	index := []any{}
	bounds := []any{}
	for id, rect := range rects {
		ids = append(ids, id)
		index = append(index, len(ids)-1)
		bounds = append(bounds, rect)
	}
	return map[string]any{"documents": []any{map[string]any{
		"nodes":  map[string]any{"backendNodeId": ids},
		"layout": map[string]any{"nodeIndex": index, "bounds": bounds},
	}}}
}

// TestControlsBoxesRealChrome checks the layout rects on a real Chrome: a control
// in the first screen is in view and a control far below is not, and after the
// page scrolls the far control comes into view. Gated on OCODE_CHROME_PATH like
// TestSettleRealChrome.
func TestControlsBoxesRealChrome(t *testing.T) {
	chromePath := os.Getenv("OCODE_CHROME_PATH")
	if chromePath == "" {
		t.Skip("OCODE_CHROME_PATH not set; skipping real-Chrome controls box test")
	}
	const page = `<!doctype html><html><body style="margin:0">
<input aria-label="Top field" style="position:absolute;top:10px;left:10px">
<button style="position:absolute;top:3000px;left:10px">Far button</button>
<button style="position:fixed;top:20px;left:300px">Fixed button</button>
</body></html>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, page)
	}))
	defer srv.Close()
	hostPort := strings.TrimPrefix(srv.URL, "http://")

	sup := tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{GracePeriod: 2 * time.Second})
	m := NewManager(ManagerOptions{
		ChromePath: chromePath,
		Supervisor: sup,
		Dialer:     newTestGuardedDialer(hostPort),
	})
	defer func() {
		cctx, ccancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer ccancel()
		_ = m.Close(cctx)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	tgt, err := m.Attach(ctx, "tab:boxes", &recordingSink{})
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if err := tgt.Navigate(ctx, srv.URL+"/"); err != nil {
		t.Fatalf("Navigate: %v", err)
	}

	var refs RefTable
	before, err := tgt.Controls(ctx, &refs)
	if err != nil {
		t.Fatalf("Controls before scroll: %v", err)
	}
	if before.ViewportWidth <= 0 || before.ViewportHeight <= 0 {
		t.Fatalf("viewport = %dx%d, want a visible viewport", before.ViewportWidth, before.ViewportHeight)
	}
	inFirstScreen, far := controlNamed(t, before, "Top field"), controlNamed(t, before, "Far button")
	if !inFirstScreen.InViewport || inFirstScreen.Box == nil {
		t.Fatalf("top field = %+v, want a box in view before scrolling", inFirstScreen)
	}
	if fixed := controlNamed(t, before, "Fixed button"); !fixed.InViewport || fixed.Box == nil || fixed.Box.Y != 20 {
		t.Fatalf("fixed button = %+v, want a fixed box at y 20 in view before scrolling", fixed)
	}
	if far.InViewport || far.Box == nil {
		t.Fatalf("far button box=%+v in_view=%v viewport=%dx%d, want a box outside the viewport before scrolling", *far.Box, far.InViewport, before.ViewportWidth, before.ViewportHeight)
	}

	if _, err := tgt.evaluateIn(ctx, 0, "window.scrollTo(0, 2900); true", true); err != nil {
		t.Fatalf("scroll: %v", err)
	}
	after, err := tgt.Controls(ctx, &refs)
	if err != nil {
		t.Fatalf("Controls after scroll: %v", err)
	}
	if before.ScrollY != 0 || after.ScrollY <= 0 {
		t.Fatalf("scroll_y before = %v, after = %v; want 0 then a positive offset", before.ScrollY, after.ScrollY)
	}
	if far := controlNamed(t, after, "Far button"); !far.InViewport {
		t.Fatalf("far button = %+v after scrolling, want it in view", far)
	}
	if fixed := controlNamed(t, after, "Fixed button"); !fixed.InViewport || fixed.Box == nil || fixed.Box.Y != 20 {
		t.Fatalf("fixed button = %+v after scrolling, want it still at y 20 in view", fixed)
	}
	if top := controlNamed(t, after, "Top field"); top.InViewport {
		t.Fatalf("top field = %+v after scrolling, want it out of view", top)
	}
}

// controlNamed returns the control with the given label, failing the test when the
// page has none.
func controlNamed(t *testing.T, page ControlsPage, label string) Control {
	t.Helper()
	for _, c := range page.Controls {
		if c.Label == label {
			return c
		}
	}
	t.Fatalf("no control labelled %q in %+v", label, page.Controls)
	return Control{}
}
