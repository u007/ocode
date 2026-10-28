package cdp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Mutating page actions on one element: click, fill, type, check and select.
// The element is named by an @eN ref (from snapshot or find) or by a selector
// (css, name=, label=, text=), as FindElements reads them. Each action first
// resolves the target to exactly one element and proves that a CSS path names
// that element in the realm the action runs in. Only then does it act. A stale
// ref, an ambiguous selector or a path that does not come back to the element
// is an error. Nothing is retried.
//
// htrcli's fill, type, check, select and click are ported from its DOM bundle
// (bundle/htrcli-dom.js) and form kit (bundle/formkit.js), which run through
// execDOM and callKit in the target's realm. Deviations are noted per action.

const (
	// interactTextMax clips caller-supplied target text and page-supplied
	// values echoed in results and reports.
	interactTextMax = 200
	// interactErrMax clips page error text carried into an error.
	interactErrMax = 300
)

// interactTarget is one resolved element. path is a CSS path that resolves to
// exactly this element in realm contextID. backendID names the same element
// for CDP calls that take a node (refCenter, DOM.setFileInputFiles).
type interactTarget struct {
	raw       string
	contextID int64
	path      string
	backendID int64
	probe     elementProbe
}

// elementProbe is what the page reports about a resolved element.
type elementProbe struct {
	Tag      string `json:"tag"`
	Type     string `json:"type"`
	Role     string `json:"role"`
	Class    string `json:"cls"`
	Multiple bool   `json:"multiple"`
}

// probeFn reports the element at a CSS path. Runs in the element's realm.
const probeFn = `/*interact:probe*/ (p) => {
	const e = document.querySelector(p);
	if (!e) throw new Error("element " + p + " is no longer in the document");
	return {
		tag: e.tagName.toLowerCase(),
		type: (e.getAttribute("type") || "").toLowerCase(),
		role: (e.getAttribute("role") || "").toLowerCase(),
		cls: typeof e.className === "string" ? e.className : "",
		multiple: e.multiple === true,
	};
}`

// refPathFn builds a CSS path for the element it is called on, using
// tag:nth-of-type from <html> down. The path is unique in its document by
// construction; resolveInteract still checks it against the ref's backendNodeId.
const refPathFn = `function() {
	if (!(this instanceof Element)) throw new Error("ref is not an element");
	const parts = [];
	for (let el = this; el !== document.documentElement; el = el.parentElement) {
		if (!el.parentElement) throw new Error("the ref's element is not in the light DOM of a document (it is detached or inside a shadow root)");
		let idx = 1;
		for (let s = el.previousElementSibling; s; s = s.previousElementSibling) {
			if (s.localName === el.localName) idx++;
		}
		parts.unshift(el.localName + ":nth-of-type(" + idx + ")");
	}
	return parts.length ? "html > " + parts.join(" > ") : "html";
}`

// hitAtFn reports whether a click at (x, y) in the element's own realm reaches
// the element at a CSS path: the element itself or a descendant is on top there.
// x and y are that realm's viewport coordinates (frame-local for an iframe), not
// the top window's.
const hitAtFn = `(p, x, y) => {
	const e = document.querySelector(p);
	if (!e) return false;
	const h = document.elementFromPoint(x, y);
	return h !== null && (h === e || e.contains(h));
}`

// readCheckedFn reports an input's checked state, or aria-checked for an ARIA
// checkbox, read by path.
const readCheckedFn = `(p) => {
	const e = document.querySelector(p);
	if (!e) throw new Error("element " + p + " is no longer in the document");
	return e instanceof HTMLInputElement ? e.checked : e.getAttribute("aria-checked") === "true";
}`

// selectSetFn sets a <select>'s value the way the bundle's select action does
// (focus, value, input and change events). It is its own function, not the
// bundle's select action, because that action treats an empty value as
// missing, and an empty-valued placeholder option is a real option.
const selectSetFn = `(p, v) => {
	const e = document.querySelector(p);
	if (!(e instanceof HTMLSelectElement)) throw new Error("element " + p + " is not a <select>");
	e.focus();
	Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, "value").set.call(e, v);
	e.dispatchEvent(new Event("input", { bubbles: true }));
	e.dispatchEvent(new Event("change", { bubbles: true }));
	return true;
}`

// resolveInteract resolves target (an @eN ref or a selector) to one element.
func (t *Target) resolveInteract(ctx context.Context, frame, target string, refs *RefTable) (interactTarget, error) {
	if target == "" {
		return interactTarget{}, errors.New("target is empty")
	}
	if strings.HasPrefix(target, "@e") {
		return t.resolveRefTarget(ctx, frame, target, refs)
	}
	return t.resolveSelectorTarget(ctx, frame, target)
}

// resolveRefTarget resolves an @eN ref. Refs resolve in the top document, so a
// frame cannot be combined with one (htrcli's rule as well).
func (t *Target) resolveRefTarget(ctx context.Context, frame, ref string, refs *RefTable) (interactTarget, error) {
	if frame != "" {
		return interactTarget{}, fmt.Errorf("%s is an element ref: refs resolve in the top document and cannot be combined with frame %q; use a selector inside the frame instead", ref, frame)
	}
	if refs == nil {
		return interactTarget{}, errors.New("nil RefTable; pass the agent's ref table")
	}
	backendID, err := refs.Lookup(ref)
	if err != nil {
		return interactTarget{}, err
	}
	objectID, err := t.resolveRef(ctx, backendID)
	if err != nil {
		return interactTarget{}, err
	}
	raw, err := t.callFunctionOn(ctx, objectID, refPathFn)
	if err != nil {
		return interactTarget{}, fmt.Errorf("building a CSS path for %s: %w", ref, err)
	}
	var path string
	if err := json.Unmarshal(raw, &path); err != nil {
		return interactTarget{}, fmt.Errorf("decoding the CSS path for %s: %w", ref, err)
	}
	// The path must come back to this very node in the top document, the realm
	// the action runs in. A ref minted inside an iframe fails here: its path,
	// evaluated in the top document, names some other element or none.
	topID, err := t.uniqueBackendID(ctx, 0, path)
	if err != nil {
		return interactTarget{}, fmt.Errorf("%s: %w", ref, err)
	}
	if topID != backendID {
		return interactTarget{}, fmt.Errorf("%s: its CSS path %s names a different element in the top document (is it inside an iframe?); refusing to act on the wrong element", ref, clipText(path, interactTextMax))
	}
	return t.interactFor(ctx, ref, 0, path, backendID)
}

// resolveSelectorTarget resolves a selector through the bundle's findAll and
// requires exactly one match. The bundle's CSS path for a match always matches
// that match (every step is a condition the element satisfies), so a path that
// matches exactly one element is the match. A path matching several is refused.
func (t *Target) resolveSelectorTarget(ctx context.Context, frame, selector string) (interactTarget, error) {
	contextID, err := t.realmFor(ctx, frame)
	if err != nil {
		return interactTarget{}, err
	}
	target, err := parseFindSelector(selector)
	if err != nil {
		return interactTarget{}, err
	}
	res, err := t.execDOMIn(ctx, contextID, domCommand{ID: "resolve", Action: "findAll", Target: &target})
	if err != nil {
		return interactTarget{}, err
	}
	if !res.Success {
		return interactTarget{}, fmt.Errorf("%q: %s", clipText(selector, interactTextMax), clipText(res.Error, interactErrMax))
	}
	var matches []struct {
		Selector string `json:"selector"`
	}
	if err := json.Unmarshal(res.Data, &matches); err != nil {
		return interactTarget{}, fmt.Errorf("%q: decoding matches: %w", clipText(selector, interactTextMax), err)
	}
	switch {
	case len(matches) == 0:
		return interactTarget{}, fmt.Errorf("no element matches %q%s", clipText(selector, interactTextMax), inFrameNote(frame))
	case len(matches) > 1:
		return interactTarget{}, fmt.Errorf("%q matches %d elements; use a selector that matches exactly one, or a ref from snapshot or find", clipText(selector, interactTextMax), len(matches))
	}
	path := matches[0].Selector
	if path == "" {
		path = ":root" // the bundle reports the document element as an empty path
	}
	backendID, err := t.uniqueBackendID(ctx, contextID, path)
	if err != nil {
		return interactTarget{}, fmt.Errorf("%q: %w", clipText(selector, interactTextMax), err)
	}
	return t.interactFor(ctx, selector, contextID, path, backendID)
}

func inFrameNote(frame string) string {
	if frame == "" {
		return ""
	}
	return " in frame " + clipText(frame, interactTextMax)
}

// uniqueBackendID returns the backendNodeId of the one element path names in
// realm contextID. A path that names none, or several, is an error.
func (t *Target) uniqueBackendID(ctx context.Context, contextID int64, path string) (int64, error) {
	raw, err := t.runScopedIn(ctx, contextID, findCountFn, path)
	if err != nil {
		return 0, err
	}
	var n int
	if err := json.Unmarshal(raw, &n); err != nil {
		return 0, fmt.Errorf("decoding match count for %s: %w", clipText(path, interactTextMax), err)
	}
	if n != 1 {
		return 0, fmt.Errorf("CSS path %s names %d elements, not one; refusing to act on a guess", clipText(path, interactTextMax), n)
	}
	backendID, note, err := t.elementForPath(ctx, contextID, path)
	if err != nil {
		return 0, err
	}
	if note != "" {
		return 0, errors.New(note)
	}
	return backendID, nil
}

// interactFor finishes a resolution: it proves the path names the element with
// refCSSIdentity, and reads what the element is.
func (t *Target) interactFor(ctx context.Context, raw string, contextID int64, path string, backendID int64) (interactTarget, error) {
	objectID, err := t.resolveRef(ctx, backendID)
	if err != nil {
		return interactTarget{}, err
	}
	if err := t.refCSSIdentity(ctx, objectID, path); err != nil {
		return interactTarget{}, err
	}
	probe, err := t.runScopedIn(ctx, contextID, probeFn, path)
	if err != nil {
		return interactTarget{}, fmt.Errorf("reading element %s: %w", clipText(raw, interactTextMax), err)
	}
	var p elementProbe
	if err := json.Unmarshal(probe, &p); err != nil {
		return interactTarget{}, fmt.Errorf("decoding element %s: %w", clipText(raw, interactTextMax), err)
	}
	return interactTarget{raw: raw, contextID: contextID, path: path, backendID: backendID, probe: p}, nil
}

// awaitActionable runs the bundle's prepareClick on the element: it waits for
// the element to be visible and enabled (5s), then scrolls it to the centre.
func (t *Target) awaitActionable(ctx context.Context, el interactTarget) error {
	res, err := t.execDOMIn(ctx, el.contextID, domCommand{ID: "await", Action: "prepareClick", Target: &domTarget{Selector: el.path}})
	if err != nil {
		return err
	}
	if !res.Success {
		return fmt.Errorf("%s: %s", clipText(el.raw, interactTextMax), clipText(res.Error, interactErrMax))
	}
	return nil
}

// callKitIn is callKit for a realm already resolved by the caller, so a poll
// does not re-resolve the frame on every call.
func (t *Target) callKitIn(ctx context.Context, contextID int64, method string, args ...any) (json.RawMessage, error) {
	if args == nil {
		args = []any{}
	}
	return t.withBundle(ctx, contextID, formKit, func() (json.RawMessage, error) {
		return t.runScopedIn(ctx, contextID, callKitFn, method, args)
	})
}

// ClickElement clicks the element at the point refCenter computes (the centre of
// its content box, in top-viewport CSS pixels). Before the click, that same point
// is hit-tested in the element's realm: if the element at it is not the target or
// a descendant, the click is refused and no mouse event is sent. Nothing that can
// move the page runs between the hit test and the dispatch.
func (t *Target) ClickElement(ctx context.Context, frame, target string, refs *RefTable) (string, error) {
	el, err := t.resolveInteract(ctx, frame, target, refs)
	if err != nil {
		return "", err
	}
	if err := t.awaitActionable(ctx, el); err != nil {
		return "", fmt.Errorf("click %w", err)
	}
	x, y, err := t.refCenter(ctx, el.backendID)
	if err != nil {
		return "", fmt.Errorf("click %s: %w", clipText(target, interactTextMax), err)
	}
	hx, hy := x, y
	if frame != "" {
		// Keep pick and click on one frame-origin helper: pick's frameToTop
		// adds this same origin, so a transformed iframe lands identically.
		ox, oy, err := t.frameOrigin(ctx, frame)
		if err != nil {
			return "", fmt.Errorf("click %s: %w", clipText(target, interactTextMax), err)
		}
		hx, hy = x-ox, y-oy
	}
	raw, err := t.runScopedIn(ctx, el.contextID, hitAtFn, el.path, hx, hy)
	if err != nil {
		return "", err
	}
	var hit bool
	if err := json.Unmarshal(raw, &hit); err != nil {
		return "", fmt.Errorf("decoding hit test: %w", err)
	}
	if !hit {
		return "", fmt.Errorf("click %s: another element covers the point it would click (%.0f, %.0f); refusing to click the wrong element", clipText(target, interactTextMax), x, y)
	}
	if err := t.Click(ctx, x, y); err != nil {
		return "", fmt.Errorf("click %s: %w", clipText(target, interactTextMax), err)
	}
	return "clicked " + clipText(target, interactTextMax), nil
}

// frameOrigin returns where the viewport of the <iframe> frame names begins, in
// top-viewport CSS pixels: the top-left of the iframe's content box. A frame
// viewport starts there, so subtracting it turns a top-viewport point into the
// frame-local point a hit test in the frame's realm takes. The box comes from
// the frame owner's DOM.getContentQuads, so it reflects a CSS transform,
// sub-pixel placement, border and padding rather than a getBoundingClientRect
// estimate. It is one helper for both click and pick.
// Keep pick and click on one frame-origin helper.
func (t *Target) frameOrigin(ctx context.Context, frame string) (float64, float64, error) {
	frameID, err := t.resolveFrameID(ctx, frame)
	if err != nil {
		return 0, 0, err
	}
	var owner struct {
		BackendNodeID int64 `json:"backendNodeId"`
	}
	if err := t.conn.Call(ctx, t.sessionID, "DOM.getFrameOwner", map[string]any{"frameId": frameID}, &owner); err != nil {
		return 0, 0, fmt.Errorf("DOM.getFrameOwner for frame %q: %w", frame, err)
	}
	var res struct {
		Quads [][]float64 `json:"quads"`
	}
	if err := t.conn.Call(ctx, t.sessionID, "DOM.getContentQuads", map[string]any{"backendNodeId": owner.BackendNodeID}, &res); err != nil {
		return 0, 0, fmt.Errorf("DOM.getContentQuads for frame %q: %w", frame, err)
	}
	if len(res.Quads) == 0 || len(res.Quads[0]) != 8 {
		return 0, 0, fmt.Errorf("frame %q has no content box to measure", frame)
	}
	origin := quadBounds(res.Quads[0])
	return origin.x0, origin.y0, nil
}

// FillElement sets the value of a text input or textarea, then reads it back.
// The fill goes through the bundle's fill action (htrcli's semantics: native
// value setter, input, change and blur events), except that an empty value
// uses clear, because the bundle reads an empty string as a missing value.
// A field that does not hold the value afterwards is an error.
func (t *Target) FillElement(ctx context.Context, frame, target, value string, refs *RefTable) (string, error) {
	el, err := t.resolveInteract(ctx, frame, target, refs)
	if err != nil {
		return "", err
	}
	kind, err := elementKind(el.probe)
	if err != nil {
		return "", fmt.Errorf("fill %s: %w", clipText(target, interactTextMax), err)
	}
	if kind != kindText {
		return "", fmt.Errorf("fill %s: element is a %s, not a text field; use the action for it", clipText(target, interactTextMax), kind)
	}
	if err := t.writeText(ctx, el, value); err != nil {
		return "", fmt.Errorf("fill %w", err)
	}
	got, err := t.readText(ctx, el)
	if err != nil {
		return "", err
	}
	if normalizeTextForTag(el.probe.Tag, got) != normalizeTextForTag(el.probe.Tag, value) {
		return "", fmt.Errorf("fill %s did not stick: the field reads %q", clipText(target, interactTextMax), clipText(got, interactTextMax))
	}
	return "filled " + clipText(target, interactTextMax), nil
}

// writeText runs the bundle's fill (or clear, for an empty value). It does not
// verify; FillElement and FillForm read the field back themselves.
func (t *Target) writeText(ctx context.Context, el interactTarget, value string) error {
	cmd := domCommand{ID: "fill", Action: "fill", Target: &domTarget{Selector: el.path}, Value: value}
	if value == "" {
		cmd = domCommand{ID: "clear", Action: "clear", Target: &domTarget{Selector: el.path}}
	}
	res, err := t.execDOMIn(ctx, el.contextID, cmd)
	if err != nil {
		return err
	}
	if !res.Success {
		return fmt.Errorf("%s: %s", clipText(el.raw, interactTextMax), clipText(res.Error, interactErrMax))
	}
	return nil
}

// readText reads a text field's value through the form kit's readField.
func (t *Target) readText(ctx context.Context, el interactTarget) (string, error) {
	raw, err := t.callKitIn(ctx, el.contextID, "readField", el.path, kindText)
	if err != nil {
		return "", err
	}
	var got string
	if err := json.Unmarshal(raw, &got); err != nil {
		return "", fmt.Errorf("decoding value of %s: %w", clipText(el.raw, interactTextMax), err)
	}
	return got, nil
}

// TypeIntoElement focuses a text field (or combobox input), then inserts text
// at the caret with Input.insertText, as a commit from an input method would.
// It does not clear the field first. The bundle's prepareKeys waits for the
// element and reports whether it took focus.
func (t *Target) TypeIntoElement(ctx context.Context, frame, target, text string, refs *RefTable) (string, error) {
	if text == "" {
		return "", errors.New("type: text is empty")
	}
	el, err := t.resolveInteract(ctx, frame, target, refs)
	if err != nil {
		return "", err
	}
	kind, err := elementKind(el.probe)
	if err != nil {
		return "", fmt.Errorf("type %s: %w", clipText(target, interactTextMax), err)
	}
	if kind != kindText && kind != kindCombobox {
		return "", fmt.Errorf("type %s: element is a %s; type needs a text field or combobox input", clipText(target, interactTextMax), kind)
	}
	res, err := t.execDOMIn(ctx, el.contextID, domCommand{ID: "focus", Action: "prepareKeys", Target: &domTarget{Selector: el.path}})
	if err != nil {
		return "", err
	}
	if !res.Success {
		return "", fmt.Errorf("type %s: %s", clipText(target, interactTextMax), clipText(res.Error, interactErrMax))
	}
	var focus struct {
		Focused bool `json:"focused"`
	}
	if err := json.Unmarshal(res.Data, &focus); err != nil {
		return "", fmt.Errorf("decoding focus result: %w", err)
	}
	if !focus.Focused {
		return "", fmt.Errorf("type %s: could not focus the element", clipText(target, interactTextMax))
	}
	before, err := t.readText(ctx, el)
	if err != nil {
		return "", err
	}
	if err := t.InsertText(ctx, text); err != nil {
		return "", fmt.Errorf("type %s: %w", clipText(target, interactTextMax), err)
	}
	got, err := t.readText(ctx, el)
	if err != nil {
		return "", err
	}
	// A field that already held the text would pass a Contains check alone, so
	// the value must also have changed.
	if got == before || !strings.Contains(got, text) {
		return "", fmt.Errorf("type %s: the field reads %q after typing (it read %q before); the text did not land", clipText(target, interactTextMax), clipText(got, interactTextMax), clipText(before, interactTextMax))
	}
	return fmt.Sprintf("typed %d chars into %s", len([]rune(text)), clipText(target, interactTextMax)), nil
}

// SetChecked checks or unchecks a checkbox or radio, or an ARIA checkbox
// (role=checkbox). The bundle's check and uncheck actions do the work, and the
// state is read back afterwards.
func (t *Target) SetChecked(ctx context.Context, frame, target string, checked bool, refs *RefTable) (string, error) {
	el, err := t.resolveInteract(ctx, frame, target, refs)
	if err != nil {
		return "", err
	}
	inputBox := el.probe.Tag == "input" && (el.probe.Type == "checkbox" || el.probe.Type == "radio")
	ariaBox := el.probe.Tag != "input" && el.probe.Role == "checkbox"
	if !inputBox && !ariaBox {
		return "", fmt.Errorf("check %s: element is <%s type=%q>; check needs a checkbox, radio or role=checkbox", clipText(target, interactTextMax), el.probe.Tag, el.probe.Type)
	}
	if err := t.writeChecked(ctx, el, checked); err != nil {
		return "", err
	}
	got, err := t.readChecked(ctx, el)
	if err != nil {
		return "", err
	}
	if got != checked {
		return "", fmt.Errorf("check %s: the element reads checked=%t after the action, want %t", clipText(target, interactTextMax), got, checked)
	}
	return fmt.Sprintf("checked=%t on %s", checked, clipText(target, interactTextMax)), nil
}

// writeChecked runs the bundle's check or uncheck action.
func (t *Target) writeChecked(ctx context.Context, el interactTarget, checked bool) error {
	action := "uncheck"
	if checked {
		action = "check"
	}
	res, err := t.execDOMIn(ctx, el.contextID, domCommand{ID: action, Action: action, Target: &domTarget{Selector: el.path}})
	if err != nil {
		return err
	}
	if !res.Success {
		return fmt.Errorf("%s %s: %s", action, clipText(el.raw, interactTextMax), clipText(res.Error, interactErrMax))
	}
	return nil
}

// readChecked reports the checked state of an input, or aria-checked for an
// ARIA checkbox.
func (t *Target) readChecked(ctx context.Context, el interactTarget) (bool, error) {
	raw, err := t.runScopedIn(ctx, el.contextID, readCheckedFn, el.path)
	if err != nil {
		return false, err
	}
	var got bool
	if err := json.Unmarshal(raw, &got); err != nil {
		return false, fmt.Errorf("decoding checked state of %s: %w", clipText(el.raw, interactTextMax), err)
	}
	return got, nil
}

// SelectElement selects an option of a native <select> by option value or by
// visible text, then verifies the <select> holds it.
func (t *Target) SelectElement(ctx context.Context, frame, target, want string, refs *RefTable) (string, error) {
	el, err := t.resolveInteract(ctx, frame, target, refs)
	if err != nil {
		return "", err
	}
	if el.probe.Tag != "select" {
		return "", fmt.Errorf("select %s: element is <%s>, not a <select>; use pick for a combobox", clipText(target, interactTextMax), el.probe.Tag)
	}
	shown, err := t.selectOption(ctx, el, want)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("selected %q in %s", clipText(shown.Text, interactTextMax), clipText(target, interactTextMax)), nil
}

// selectOption selects the option of the <select> el that want names (an exact
// option value first, else the best visible text) and verifies the <select>
// now holds that value. The returned option is what the <select> reports.
func (t *Target) selectOption(ctx context.Context, el interactTarget, want string) (selectOption, error) {
	raw, err := t.callKitIn(ctx, el.contextID, "selectOptions", el.path)
	if err != nil {
		return selectOption{}, err
	}
	var options []selectOption
	if err := json.Unmarshal(raw, &options); err != nil {
		return selectOption{}, fmt.Errorf("decoding <select> options: %w", err)
	}
	opt, err := resolveSelectOption(options, want)
	if err != nil {
		return selectOption{}, err
	}
	if err := t.awaitActionable(ctx, el); err != nil {
		return selectOption{}, fmt.Errorf("select %w", err)
	}
	if _, err := t.runScopedIn(ctx, el.contextID, selectSetFn, el.path, opt.Value); err != nil {
		return selectOption{}, err
	}
	raw, err = t.callKitIn(ctx, el.contextID, "readSelect", el.path)
	if err != nil {
		return selectOption{}, err
	}
	var got selectOption
	if err := json.Unmarshal(raw, &got); err != nil {
		return selectOption{}, fmt.Errorf("decoding <select> read-back: %w", err)
	}
	if got.Value != opt.Value {
		return selectOption{}, fmt.Errorf("selected %q (value %q) but the <select> now holds value %q (%q)",
			clipText(opt.Text, interactTextMax), clipText(opt.Value, interactTextMax), clipText(got.Value, interactTextMax), clipText(got.Text, interactTextMax))
	}
	return got, nil
}

// elementKind maps a resolved element to a field kind (htrcli's elementKind).
func elementKind(p elementProbe) (string, error) {
	switch {
	case p.Tag == "select":
		return kindSelect, nil
	case p.Role == "combobox" || strings.Contains(p.Class, "select__"):
		return kindCombobox, nil
	case p.Tag == "textarea":
		return kindText, nil
	case p.Tag == "input":
		switch p.Type {
		case "checkbox":
			return kindCheckbox, nil
		case "radio":
			return kindRadio, nil
		case "file":
			return kindFile, nil
		case "hidden", "submit", "button", "reset", "image":
			return "", fmt.Errorf("<input type=%s> cannot be filled", p.Type)
		default:
			return kindText, nil
		}
	default:
		return "", fmt.Errorf("unsupported element <%s> (expected input, textarea, select or a combobox)", p.Tag)
	}
}
