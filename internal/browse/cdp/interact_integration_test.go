package cdp

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/tool"
)

// interactFixtureHTML is the top document: a button, a text input, a native
// select, a checkbox, a file input, a react-free combobox (an input plus a
// <ul role=listbox> that fills on input and ArrowDown), a same-origin iframe and
// a cross-origin iframe. %s is the cross-origin frame URL.
const interactFixtureHTML = `<!doctype html><html><head><title>Interact</title></head><body>
<button id="save" onclick="window.saved=(window.saved||0)+1">Save</button>
<label for="nm">Name</label>
<input id="nm" name="fullname" type="text">
<label for="country">Country</label>
<select id="country"><option value="">Choose</option><option value="my">Malaysia</option><option value="sg">Singapore</option></select>
<label><input id="terms" type="checkbox"> Accept terms</label>
<label for="cv">CV</label>
<input id="cv" type="file">
<label for="loc">Location</label>
<input id="loc" role="combobox" aria-autocomplete="list" aria-controls="loc-list" autocomplete="off">
<ul id="loc-list" role="listbox" style="display:none;margin:0;padding:0;list-style:none;position:relative;background:#fff;border:1px solid #888;width:240px"></ul>
<iframe id="same" src="/frame" width="400" height="160"></iframe>
<iframe id="cross" src="%s" width="400" height="160"></iframe>
<script>
const LOCS = ["Malaysia", "Malaysia Airlines", "Singapore", "Sabah"];
const loc = document.getElementById("loc");
const list = document.getElementById("loc-list");
function render(filter) {
  list.innerHTML = "";
  const f = (filter || "").toLowerCase();
  for (const name of LOCS) {
    if (f && name.toLowerCase().indexOf(f) < 0) continue;
    const li = document.createElement("li");
    li.setAttribute("role", "option");
    li.textContent = name;
    li.style.padding = "4px 8px";
    li.addEventListener("click", () => { loc.value = name; list.style.display = "none"; });
    list.appendChild(li);
  }
  list.style.display = list.children.length ? "block" : "none";
}
loc.addEventListener("keydown", (e) => {
  if (e.key === "ArrowDown") render("");
  if (e.key === "Escape") list.style.display = "none";
});
loc.addEventListener("input", () => render(loc.value));
</script>
</body></html>`

// interactFixtureFrame is the same-origin iframe's document: a text field, a
// checkbox, a button, and a combobox whose option list is inside the frame.
const interactFixtureFrame = `<!doctype html><html><body style="margin:0">
<label for="in">Inner</label>
<input id="in" name="inner" type="text">
<input id="fc" type="checkbox">
<button id="fb" onclick="window.fbClicks=(window.fbClicks||0)+1">Frame button</button>
<label for="fl">Frame location</label>
<input id="fl" role="combobox" aria-autocomplete="list" aria-controls="fl-list" autocomplete="off">
<ul id="fl-list" role="listbox" style="display:none;margin:0;padding:0;list-style:none;position:relative;background:#fff;border:1px solid #888;width:240px"></ul>
<script>
const FLOCS = ["Malaysia", "Singapore", "Sabah"];
const fl = document.getElementById("fl");
const flist = document.getElementById("fl-list");
function frender(filter) {
  flist.innerHTML = "";
  const f = (filter || "").toLowerCase();
  for (const name of FLOCS) {
    if (f && name.toLowerCase().indexOf(f) < 0) continue;
    const li = document.createElement("li");
    li.setAttribute("role", "option");
    li.textContent = name;
    li.style.padding = "4px 8px";
    li.addEventListener("click", () => { fl.value = name; flist.style.display = "none"; });
    flist.appendChild(li);
  }
  flist.style.display = flist.children.length ? "block" : "none";
}
fl.addEventListener("keydown", (e) => {
  if (e.key === "ArrowDown") frender("");
  if (e.key === "Escape") flist.style.display = "none";
});
fl.addEventListener("input", () => frender(fl.value));
</script>
</body></html>`

// TestInteractRealChrome drives the interaction actions against a real headless
// Chrome: a ref and a selector, text fill with read-back, select by text and by
// an empty placeholder value, a checkbox, a live combobox (exact, ambiguous and
// no-match picks, each verifying what the widget shows), a file input, a stale
// ref, a same-origin iframe, a cross-origin iframe, and a fill-form with one
// bad field. Gated on OCODE_CHROME_PATH like TestPageDOMRealChrome.
func TestInteractRealChrome(t *testing.T) {
	chromePath := os.Getenv("OCODE_CHROME_PATH")
	if chromePath == "" {
		t.Skip("OCODE_CHROME_PATH not set; skipping real-Chrome interaction test")
	}

	// The cross-origin frame is the same server reached as localhost: a
	// different origin with the same port, so the egress allowance still holds.
	var crossURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		switch r.URL.Path {
		case "/":
			fmt.Fprintf(w, interactFixtureHTML, crossURL)
		case "/frame":
			fmt.Fprint(w, interactFixtureFrame)
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

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	tgt, err := m.Attach(ctx, "tab:interact", &recordingSink{})
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if err := tgt.Navigate(ctx, srv.URL+"/"); err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	if err := tgt.Wait(ctx, "", WaitSpec{Selector: "#save", Timeout: 20 * time.Second}); err != nil {
		t.Fatalf("Wait for #save: %v", err)
	}
	// evalValue reads expr in the top document (frame "") or in an iframe's
	// document. expr names that document as D.
	evalValue := func(frame, expr string) string {
		t.Helper()
		doc := "document"
		if frame != "" {
			doc = fmt.Sprintf(`document.querySelector(%q).contentDocument`, frame)
		}
		res, err := tgt.evaluateIn(ctx, 0, fmt.Sprintf(`(() => { const D = %s; return String(%s); })()`, doc, expr), true)
		if err != nil {
			t.Fatalf("evaluate %s: %v", expr, err)
		}
		return strings.Trim(string(res.Result.Value), `"`)
	}

	// Click by selector: the button's onclick runs once.
	if got, err := tgt.ClickElement(ctx, "", "#save", nil); err != nil || got != "clicked #save" {
		t.Fatalf("ClickElement(#save) = %q, %v", got, err)
	}
	if v := evalValue("", "window.saved"); v != "1" {
		t.Errorf("button click count = %s, want 1", v)
	}

	// Ref targeting: snapshot mints refs, then the text field is filled by ref.
	var refs RefTable
	if _, _, err := tgt.Snapshot(ctx, "", &refs); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	nm, err := tgt.FindElements(ctx, "", "name=fullname", 5, &refs)
	if err != nil || len(nm.Elements) != 1 {
		t.Fatalf("FindElements(name=fullname) = %+v, %v", nm, err)
	}
	nmRef := nm.Elements[0].Ref
	if _, err := tgt.FillElement(ctx, "", nmRef, "Ada", &refs); err != nil {
		t.Fatalf("FillElement(ref): %v", err)
	}
	if v := evalValue("", `D.getElementById("nm").value`); v != "Ada" {
		t.Errorf("text field after ref fill = %q, want Ada", v)
	}
	if _, err := tgt.TypeIntoElement(ctx, "", "#nm", " Lovelace", nil); err != nil {
		t.Fatalf("TypeIntoElement: %v", err)
	}
	if v := evalValue("", `D.getElementById("nm").value`); v != "Ada Lovelace" {
		t.Errorf("text field after type = %q, want \"Ada Lovelace\"", v)
	}

	// Native select by visible text, then by the empty placeholder value.
	if got, err := tgt.SelectElement(ctx, "", "#country", "Malaysia", nil); err != nil {
		t.Fatalf("SelectElement(Malaysia): %v (%s)", err, got)
	}
	if v := evalValue("", `D.getElementById("country").value`); v != "my" {
		t.Errorf("select value = %q, want my", v)
	}
	if _, err := tgt.SelectElement(ctx, "", "#country", "Choose", nil); err != nil {
		t.Fatalf("SelectElement(placeholder): %v", err)
	}
	if v := evalValue("", `D.getElementById("country").value`); v != "" {
		t.Errorf("select value after placeholder = %q, want empty", v)
	}

	// Checkbox on, read back.
	if _, err := tgt.SetChecked(ctx, "", "#terms", true, nil); err != nil {
		t.Fatalf("SetChecked(#terms): %v", err)
	}
	if v := evalValue("", `D.getElementById("terms").checked`); v != "true" {
		t.Errorf("checkbox = %q, want true", v)
	}

	// Combobox: the exact option is picked and the widget shows it.
	res, err := tgt.Pick(ctx, "", "#loc", "Singapore", nil)
	if err != nil {
		t.Fatalf("Pick(Singapore): %v", err)
	}
	if res.Mode != "combobox" || res.Selected != "Singapore" {
		t.Errorf("Pick(Singapore) = %+v, want combobox showing Singapore", res)
	}
	if v := evalValue("", `D.getElementById("loc").value`); v != "Singapore" {
		t.Errorf("combobox value = %q, want Singapore", v)
	}
	// A tie (two prefix matches) is an error that lists both and closes the menu.
	if _, err := tgt.Pick(ctx, "", "#loc", "Malay", nil); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("Pick(Malay) err = %v, want an ambiguous error", err)
	}
	if v := evalValue("", `D.getElementById("loc-list").style.display`); v != "none" {
		t.Errorf("menu display after the ambiguous pick = %q, want none (closed)", v)
	}
	// No match: the error lists what the menu offers without a query.
	_, err = tgt.Pick(ctx, "", "#loc", "Atlantis", nil)
	if err == nil || !strings.Contains(err.Error(), `"Malaysia"`) || !strings.Contains(err.Error(), `"Sabah"`) {
		t.Errorf("Pick(Atlantis) err = %v, want the unfiltered options listed", err)
	}

	// File input: the file's name lands in the input.
	cv := filepath.Join(t.TempDir(), "cv.pdf")
	if err := os.WriteFile(cv, []byte("%PDF-1.4 test"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := tgt.UploadFiles(ctx, "", "#cv", []string{cv}, nil, allowAll); err != nil {
		t.Fatalf("UploadFiles: %v", err)
	}
	if v := evalValue("", `D.getElementById("cv").files[0].name`); v != "cv.pdf" {
		t.Errorf("file input holds %q, want cv.pdf", v)
	}

	// Same-origin iframe: fill and check inside it, in its own realm.
	if _, err := tgt.FillElement(ctx, "#same", "input[name=inner]", "Grace", nil); err != nil {
		t.Fatalf("FillElement(#same): %v", err)
	}
	if v := evalValue("#same", `D.getElementById("in").value`); v != "Grace" {
		t.Errorf("iframe field = %q, want Grace", v)
	}
	if _, err := tgt.SetChecked(ctx, "#same", "#fc", true, nil); err != nil {
		t.Fatalf("SetChecked(#same #fc): %v", err)
	}
	if v := evalValue("#same", `D.getElementById("fc").checked`); v != "true" {
		t.Errorf("iframe checkbox = %q, want true", v)
	}
	// A click inside the frame lands on the frame's button.
	if _, err := tgt.ClickElement(ctx, "#same", "#fb", nil); err != nil {
		t.Fatalf("ClickElement(#same #fb): %v", err)
	}
	if v := evalValue("#same", `D.defaultView.fbClicks`); v != "1" {
		t.Errorf("frame button click count = %s, want 1", v)
	}
	// Typing into a frame field appends at the caret, inside the frame.
	if _, err := tgt.TypeIntoElement(ctx, "#same", "input[name=inner]", "!", nil); err != nil {
		t.Fatalf("TypeIntoElement(#same): %v", err)
	}
	if v := evalValue("#same", `D.getElementById("in").value`); v != "Grace!" {
		t.Errorf("frame field after type = %q, want \"Grace!\"", v)
	}
	// A combobox inside the frame: its option point is converted from the
	// frame's viewport to the top viewport before the click.
	fpick, err := tgt.Pick(ctx, "#same", "#fl", "Singapore", nil)
	if err != nil {
		t.Fatalf("Pick(#same #fl): %v", err)
	}
	if fpick.Mode != "combobox" || fpick.Selected != "Singapore" {
		t.Errorf("Pick(#same #fl) = %+v, want combobox showing Singapore", fpick)
	}
	if v := evalValue("#same", `D.getElementById("fl").value`); v != "Singapore" {
		t.Errorf("frame combobox value = %q, want Singapore", v)
	}
	// A ref cannot be combined with a frame.
	if _, err := tgt.ClickElement(ctx, "#same", nmRef, &refs); err == nil || !strings.Contains(err.Error(), "cannot be combined with frame") {
		t.Errorf("ClickElement(ref, #same) err = %v, want the ref/frame refusal", err)
	}

	// Cross-origin iframe: a clear, final error, nothing acted on.
	if _, err := tgt.FillElement(ctx, "#cross", "input", "x", nil); err == nil || !strings.Contains(err.Error(), "cross-origin") {
		t.Errorf("FillElement(#cross) err = %v, want the cross-origin error", err)
	}

	// Stale ref: remove the field, and the old ref refuses.
	if _, err := tgt.evaluateIn(ctx, 0, `document.getElementById("nm").remove(); true`, true); err != nil {
		t.Fatalf("remove #nm: %v", err)
	}
	if _, err := tgt.FillElement(ctx, "", nmRef, "Bob", &refs); err == nil || !strings.HasPrefix(err.Error(), "stale ref") {
		t.Errorf("FillElement(removed ref) err = %v, want a stale ref error", err)
	}

	// fill-form: one missing field fails, the others are still applied and read back.
	if err := tgt.Wait(ctx, "", WaitSpec{Selector: "#loc", Timeout: 5 * time.Second}); err != nil {
		t.Fatalf("Wait #loc: %v", err)
	}
	reports, err := tgt.FillForm(ctx, "", []FormField{
		{Selector: "#missing", Value: []byte(`"x"`)},
		{Selector: "#country", Value: []byte(`"sg"`)},
		{Selector: "#terms", Value: []byte(`false`)},
		{Selector: "#loc", Value: []byte(`"Sabah"`)},
		{Selector: "#cv", Value: []byte(fmt.Sprintf(`[%q]`, cv))},
	}, nil, allowAll)
	if err != nil {
		t.Fatalf("FillForm: %v", err)
	}
	wantStatus := []string{FieldFailed, FieldOK, FieldOK, FieldOK, FieldOK}
	for i, w := range wantStatus {
		if reports[i].Status != w {
			t.Errorf("fill-form row %d (%s) = %s (%s), want %s", i, reports[i].Selector, reports[i].Status, reports[i].Error, w)
		}
	}
}

// openRealChromePage serves pages (path to HTML), attaches a real Chrome tab and
// navigates it to "/". Gated on OCODE_CHROME_PATH: it skips when that is unset.
func openRealChromePage(t *testing.T, pages map[string]string) (context.Context, *Target) {
	t.Helper()
	chromePath := os.Getenv("OCODE_CHROME_PATH")
	if chromePath == "" {
		t.Skip("OCODE_CHROME_PATH not set; skipping real-Chrome interaction test")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := pages[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	sup := tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{GracePeriod: 2 * time.Second})
	m := NewManager(ManagerOptions{
		ChromePath: chromePath,
		Supervisor: sup,
		Dialer:     newTestGuardedDialer(strings.TrimPrefix(srv.URL, "http://")),
	})
	t.Cleanup(func() {
		cctx, ccancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer ccancel()
		_ = m.Close(cctx)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	t.Cleanup(cancel)
	tgt, err := m.Attach(ctx, "tab:"+t.Name(), &recordingSink{})
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if err := tgt.Navigate(ctx, srv.URL+"/"); err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	return ctx, tgt
}

const fillRefFixture = `<!doctype html><html><head><title>Fill by ref</title></head><body>
<label>Full name <input id="nm" type="text"></label>
<label><input id="terms" type="checkbox"> Accept terms</label>
</body></html>`

// TestFillFormRefRealChrome: snapshot the page, then fill-form with the @eN refs
// the snapshot minted. Both fields are written and read back OK.
func TestFillFormRefRealChrome(t *testing.T) {
	ctx, tgt := openRealChromePage(t, map[string]string{"/": fillRefFixture})
	if err := tgt.Wait(ctx, "", WaitSpec{Selector: "#nm", Timeout: 20 * time.Second}); err != nil {
		t.Fatalf("Wait #nm: %v", err)
	}
	var refs RefTable
	_, minted, err := tgt.Snapshot(ctx, "", &refs)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	refOf := map[string]string{}
	for _, e := range minted {
		refOf[e.Name] = e.Ref
	}
	nameRef, termsRef := refOf["Full name"], refOf["Accept terms"]
	if nameRef == "" || termsRef == "" {
		t.Fatalf("snapshot minted %+v, want refs for Full name and Accept terms", minted)
	}
	reports, err := tgt.FillForm(ctx, "", []FormField{
		{Selector: nameRef, Value: []byte(`"Ada"`)},
		{Selector: termsRef, Value: []byte(`true`)},
	}, &refs, allowAll)
	if err != nil {
		t.Fatalf("FillForm: %v", err)
	}
	for i, r := range reports {
		if r.Status != FieldOK {
			t.Errorf("row %d (%s) = %s (%s), want OK", i, r.Selector, r.Status, r.Error)
		}
	}
	res, err := tgt.evaluateIn(ctx, 0, `String(document.getElementById("nm").value + "|" + document.getElementById("terms").checked)`, true)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if got := strings.Trim(string(res.Result.Value), `"`); got != "Ada|true" {
		t.Errorf("page holds %q, want Ada|true", got)
	}
}

// clickPanel is a 120x40 content-box button whose padding runs to x=300 (content
// x 100..220, y 110..150), and an overlay that starts hidden. Its click counters
// record what the click reached. The bounding-rect centre is (200,130); the
// content centre, where a click lands, is (160,130).
const clickPanel = `<button id="pad" onclick="window.padClicks=(window.padClicks||0)+1" style="position:absolute;left:100px;top:100px;box-sizing:content-box;width:120px;height:40px;padding:10px 80px 10px 0;border:0;margin:0">Pad</button>
<div id="ov" onclick="window.ovClicks=(window.ovClicks||0)+1" style="position:absolute;display:none;margin:0;background:rgba(255,0,0,.4)"></div>`

const clickPointTop = `<!doctype html><html><head><title>Click point</title></head><body style="margin:0">
` + clickPanel + `
<iframe id="f" src="/frame" style="position:absolute;left:300px;top:200px;width:400px;height:300px;border:0"></iframe>
</body></html>`

const clickPointFrame = `<!doctype html><html><body style="margin:0">` + clickPanel + `</body></html>`

// TestClickPointRealChrome: the click's hit test and its dispatch use one point,
// in the top window and inside a same-origin iframe. An overlay over the padding
// (where the bounding-rect centre is) does not refuse the click; an overlay over
// the dispatched point refuses it and nothing under it is clicked; with nothing
// over the element the click lands.
func TestClickPointRealChrome(t *testing.T) {
	ctx, tgt := openRealChromePage(t, map[string]string{"/": clickPointTop, "/frame": clickPointFrame})
	if err := tgt.Wait(ctx, "", WaitSpec{Selector: "#pad", Timeout: 20 * time.Second}); err != nil {
		t.Fatalf("Wait #pad: %v", err)
	}
	if err := tgt.Wait(ctx, "#f", WaitSpec{Selector: "#pad", Timeout: 20 * time.Second}); err != nil {
		t.Fatalf("Wait #f #pad: %v", err)
	}

	// doc is the document a click targets: the top one, or the iframe's.
	doc := func(frame string) string {
		if frame == "" {
			return "document"
		}
		return fmt.Sprintf(`document.querySelector(%q).contentDocument`, frame)
	}
	read := func(frame, expr string) string {
		t.Helper()
		res, err := tgt.evaluateIn(ctx, 0, fmt.Sprintf(`(() => { const D = %s; return String(%s); })()`, doc(frame), expr), true)
		if err != nil {
			t.Fatalf("evaluate %s: %v", expr, err)
		}
		return strings.Trim(string(res.Result.Value), `"`)
	}
	// overlay shows the overlay at left,top with size w x h, or hides it when w is 0.
	overlay := func(frame string, left, top, w, h int) {
		t.Helper()
		expr := fmt.Sprintf(`(() => { const o = %s.getElementById("ov");
			if (%d === 0) { o.style.display = "none"; return true; }
			Object.assign(o.style, {display: "block", left: "%dpx", top: "%dpx", width: "%dpx", height: "%dpx"}); return true; })()`,
			doc(frame), w, left, top, w, h)
		if _, err := tgt.evaluateIn(ctx, 0, expr, true); err != nil {
			t.Fatalf("overlay %s: %v", frame, err)
		}
	}
	// padOverBoundingCentre covers (200,130), the bounding-rect centre only.
	padOverBoundingCentre := func(frame string) { overlay(frame, 190, 90, 40, 80) }
	// coverDispatched covers (160,130), the content centre the click dispatches at.
	coverDispatched := func(frame string) { overlay(frame, 150, 120, 20, 20) }

	for _, frame := range []string{"", "#f"} {
		name := "top"
		if frame != "" {
			name = "iframe"
		}
		clicks := func() string { return read(frame, "D.defaultView.padClicks||0") }
		if got := clicks(); got != "0" {
			t.Fatalf("%s: padClicks before = %s, want 0", name, got)
		}

		// (c) nothing over the element: the click lands.
		if _, err := tgt.ClickElement(ctx, frame, "#pad", nil); err != nil {
			t.Fatalf("%s (c) ClickElement: %v", name, err)
		}
		if got := clicks(); got != "1" {
			t.Errorf("%s (c) padClicks = %s, want 1", name, got)
		}

		// (a) overlay over the padding only: the click still lands on the content.
		padOverBoundingCentre(frame)
		if _, err := tgt.ClickElement(ctx, frame, "#pad", nil); err != nil {
			t.Fatalf("%s (a) ClickElement under a padding overlay: %v", name, err)
		}
		if got := clicks(); got != "2" {
			t.Errorf("%s (a) padClicks = %s, want 2", name, got)
		}
		if got := read(frame, "D.defaultView.ovClicks||0"); got != "0" {
			t.Errorf("%s (a) the overlay received %s clicks, want 0", name, got)
		}
		overlay(frame, 0, 0, 0, 0)

		// (b) overlay over the dispatched point: refused, and the overlay is not clicked.
		coverDispatched(frame)
		_, err := tgt.ClickElement(ctx, frame, "#pad", nil)
		if err == nil || !strings.Contains(err.Error(), "covers the point") {
			t.Errorf("%s (b) ClickElement err = %v, want the covered-point refusal", name, err)
		}
		if got := clicks(); got != "2" {
			t.Errorf("%s (b) padClicks = %s, want 2 (a refused click must not land)", name, got)
		}
		if got := read(frame, "D.defaultView.ovClicks||0"); got != "0" {
			t.Errorf("%s (b) the overlay received %s clicks, want 0", name, got)
		}
		overlay(frame, 0, 0, 0, 0)
	}
}
