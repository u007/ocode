package cdp

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/tool"
)

// pageDOMFixtureHTML is the top document: a button, a labelled input, a node
// the test removes, a same-origin iframe and a cross-origin iframe. %s is the
// cross-origin frame URL.
const pageDOMFixtureHTML = `<!doctype html><html><head><title>Fixture</title></head><body>
<h1>Fixture page</h1>
<button id="save">Save</button>
<label for="nm">Name</label>
<input id="nm" name="fullname" type="text">
<p id="doomed">doomed</p>
<iframe id="same" src="/frame" width="400" height="200"></iframe>
<iframe id="cross" src="%s" width="400" height="200"></iframe>
</body></html>`

// pageDOMFixtureFrame is the same-origin iframe's document.
const pageDOMFixtureFrame = `<!doctype html><html><body>
<label for="in">Inner</label>
<input id="in" name="inner" type="text">
<button>Frame button</button>
</body></html>`

// TestPageDOMRealChrome drives the page-DOM helpers against a real headless
// Chrome: the bundle install, the accessibility snapshot, find in each selector
// form, wait, a same-origin iframe (contexts, snapshot, ref geometry), a
// cross-origin iframe, and a stale ref after its element is removed. Gated on
// OCODE_CHROME_PATH like TestIntegrationRealChrome, so CI without Chrome skips.
func TestPageDOMRealChrome(t *testing.T) {
	chromePath := os.Getenv("OCODE_CHROME_PATH")
	if chromePath == "" {
		t.Skip("OCODE_CHROME_PATH not set; skipping real-Chrome page-DOM test")
	}

	// The cross-origin frame is the same server reached as localhost: a
	// different origin with the same port, so the egress allowance still holds.
	var crossURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		switch r.URL.Path {
		case "/":
			fmt.Fprintf(w, pageDOMFixtureHTML, crossURL)
		case "/frame":
			fmt.Fprint(w, pageDOMFixtureFrame)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	crossURL = strings.Replace(srv.URL, "127.0.0.1", "localhost", 1) + "/frame"
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
	tgt, err := m.Attach(ctx, "tab:pagedom", &recordingSink{})
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if err := tgt.Navigate(ctx, srv.URL+"/"); err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	if err := tgt.Wait(ctx, "", WaitSpec{Selector: "#save", Timeout: 20 * time.Second}); err != nil {
		t.Fatalf("Wait for #save: %v", err)
	}

	var refs RefTable

	// Snapshot: the accessibility tree in htrcli's layout, with refs.
	snap, minted, err := tgt.Snapshot(ctx, "", &refs)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	for _, want := range []string{`document "Fixture"`, `button "Save"`, `textbox "Name"`} {
		if !strings.Contains(snap, want) {
			t.Errorf("snapshot missing %q:\n%s", want, snap)
		}
	}
	if len(minted) == 0 {
		t.Errorf("snapshot minted no refs")
	}

	// Find: label= and name= match exactly one input.
	for _, selector := range []string{"label=Name", "name=fullname"} {
		out, err := tgt.FindElements(ctx, "", selector, 5, &refs)
		if err != nil {
			t.Fatalf("FindElements(%q): %v", selector, err)
		}
		if len(out.Elements) != 1 || out.Elements[0].Tag != "input" || !strings.HasPrefix(out.Elements[0].Ref, "@e") {
			t.Errorf("FindElements(%q) = %+v, want one input with a ref", selector, out)
		}
	}
	// text= matches every element whose text contains it, containers included
	// (htrcli's behaviour), so the button is one of the matches, not the only one.
	textOut, err := tgt.FindElements(ctx, "", "text=Save", 50, &refs)
	if err != nil {
		t.Fatalf("FindElements(text=Save): %v", err)
	}
	sawButton := false
	for _, el := range textOut.Elements {
		if el.Tag == "button" && strings.HasPrefix(el.Ref, "@e") {
			sawButton = true
		}
	}
	if !sawButton {
		t.Errorf("FindElements(text=Save) did not return the button with a ref: %+v", textOut)
	}

	// The minted ref points at the button, and its centre hit-tests to it.
	save, err := tgt.FindElements(ctx, "", "#save", 1, &refs)
	if err != nil || len(save.Elements) != 1 {
		t.Fatalf("FindElements(#save) = %+v, %v", save, err)
	}
	saveID, err := refs.Lookup(save.Elements[0].Ref)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	x, y, err := tgt.refCenter(ctx, saveID)
	if err != nil {
		t.Fatalf("refCenter(button): %v", err)
	}
	hit, err := tgt.evaluateIn(ctx, 0, fmt.Sprintf(
		`((x, y) => document.elementFromPoint(x, y) === document.getElementById("save"))(%f, %f)`, x, y), true)
	if err != nil {
		t.Fatalf("hit test: %v", err)
	}
	if string(hit.Result.Value) != "true" {
		t.Errorf("button centre (%.1f,%.1f) does not hit the button", x, y)
	}

	// Form kit: installs on first use and answers by method name, in the top
	// document and inside the iframe (norm is a pure helper of formkit.js).
	for _, frame := range []string{"", "#same"} {
		raw, err := tgt.callKit(ctx, frame, "norm", "  Full   NAME ")
		if err != nil {
			t.Fatalf("callKit(%q, norm): %v", frame, err)
		}
		if string(raw) != `"full name"` {
			t.Errorf("callKit(%q, norm) = %s, want \"full name\"", frame, raw)
		}
	}

	// Same-origin iframe: its context resolves, wait/find/snapshot run inside it.
	if err := tgt.Wait(ctx, "#same", WaitSpec{Selector: "input[name=inner]", Timeout: 20 * time.Second}); err != nil {
		t.Fatalf("Wait inside #same: %v", err)
	}
	frameFind, err := tgt.FindElements(ctx, "#same", "label=Inner", 5, &refs)
	if err != nil || len(frameFind.Elements) != 1 || frameFind.Elements[0].Tag != "input" {
		t.Fatalf("FindElements in #same = %+v, %v", frameFind, err)
	}
	frameSnap, _, err := tgt.Snapshot(ctx, "#same", &refs)
	if err != nil {
		t.Fatalf("Snapshot(#same): %v", err)
	}
	if !strings.Contains(frameSnap, `textbox "Inner"`) || !strings.Contains(frameSnap, `button "Frame button"`) {
		t.Errorf("frame snapshot missing its elements:\n%s", frameSnap)
	}

	// The frame ref's centre, taken in top-viewport coordinates, must hit the
	// frame's own input once mapped through the iframe's box.
	innerID, err := refs.Lookup(frameFind.Elements[0].Ref)
	if err != nil {
		t.Fatalf("Lookup(frame ref): %v", err)
	}
	fx, fy, err := tgt.refCenter(ctx, innerID)
	if err != nil {
		t.Fatalf("refCenter(frame input): %v", err)
	}
	frameHit, err := tgt.evaluateIn(ctx, 0, fmt.Sprintf(`((x, y) => {
		const f = document.getElementById("same");
		const r = f.getBoundingClientRect();
		const hit = f.contentDocument.elementFromPoint(x - r.left - f.clientLeft, y - r.top - f.clientTop);
		return document.elementFromPoint(x, y) === f && !!hit && hit.id === "in";
	})(%f, %f)`, fx, fy), true)
	if err != nil {
		t.Fatalf("frame hit test: %v", err)
	}
	if string(frameHit.Result.Value) != "true" {
		t.Errorf("frame input centre (%.1f,%.1f) does not hit the frame input through the iframe box", fx, fy)
	}

	// Cross-origin iframe: a clear, final error with the open-the-frame hint.
	if _, _, err := tgt.Snapshot(ctx, "#cross", &refs); err == nil || !strings.Contains(err.Error(), "cross-origin") ||
		!strings.Contains(err.Error(), "Open the frame directly instead") {
		t.Errorf("Snapshot(#cross) err = %v, want the cross-origin error", err)
	}

	// Stale ref: mint a ref for #doomed, remove it, and the ref must refuse.
	doomed, err := tgt.FindElements(ctx, "", "#doomed", 1, &refs)
	if err != nil || len(doomed.Elements) != 1 {
		t.Fatalf("FindElements(#doomed) = %+v, %v", doomed, err)
	}
	doomedID, err := refs.Lookup(doomed.Elements[0].Ref)
	if err != nil {
		t.Fatalf("Lookup(doomed): %v", err)
	}
	if _, err := tgt.evaluateIn(ctx, 0, `document.getElementById("doomed").remove(); true`, true); err != nil {
		t.Fatalf("remove #doomed: %v", err)
	}
	if err := tgt.Wait(ctx, "", WaitSpec{Selector: "#doomed", Gone: true, Timeout: 5 * time.Second}); err != nil {
		t.Fatalf("Wait --gone #doomed: %v", err)
	}
	if _, _, err := tgt.refCenter(ctx, doomedID); err == nil || !strings.HasPrefix(err.Error(), "stale ref") {
		t.Errorf("refCenter on removed element err = %v, want a stale ref error", err)
	}
	if _, err := refs.Lookup("@e9999"); err == nil {
		t.Errorf("Lookup of an unminted ref succeeded")
	}
}
