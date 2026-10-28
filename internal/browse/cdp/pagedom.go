package cdp

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// htrcli's in-page DOM bundle and form kit, vendored verbatim (each file
// carries its provenance header; refresh with `make htrcli-bundle`). Both are
// installed once per realm and then called by name, so a warm realm costs one
// Runtime.evaluate per command.

//go:embed bundle/htrcli-dom.js
var domBundleSrc string

//go:embed bundle/formkit.js
var formKitSrc string

// pageBundle is a script installed once per realm. install returns the
// expression that registers it on window.
type pageBundle struct {
	name    string
	install func() string
}

var domBundle = pageBundle{name: "DOM bundle", install: func() string { return domBundleSrc }}

// formKit wraps the object literal in parentheses, so its trailing semicolon
// has to go: "(...);" as a whole-realm install is a SyntaxError.
var formKit = pageBundle{name: "form kit", install: func() string {
	return "window.__htrcliFormKit = (\n" + strings.TrimRight(strings.TrimSpace(formKitSrc), ";") + "\n)"
}}

// execDOMFn calls the DOM bundle, or answers the sentinel when it is not
// installed in this realm yet.
const execDOMFn = `(c) => typeof window.__htrcliDom === "object" ? window.__htrcliDom.exec(c) : {__htrcliNeedBundle: true}`

// callKitFn calls one form-kit method, or answers the sentinel.
const callKitFn = `(m, a) => typeof window.__htrcliFormKit === "object" ? window.__htrcliFormKit[m](...a) : {__htrcliNeedBundle: true}`

// domTarget mirrors htrcli's api.TargetSelector, field for field and tag for
// tag, so a command serialises exactly as htrcli would send it.
type domTarget struct {
	Selector      string   `json:"selector,omitempty"`
	XPath         string   `json:"xpath,omitempty"`
	ID            string   `json:"id,omitempty"`
	Name          string   `json:"name,omitempty"`
	Role          string   `json:"role,omitempty"`
	Label         string   `json:"label,omitempty"`
	Placeholder   string   `json:"placeholder,omitempty"`
	Text          string   `json:"text,omitempty"`
	TextMatch     string   `json:"textMatch,omitempty"`
	CaseSensitive *bool    `json:"caseSensitive,omitempty"`
	Tag           string   `json:"tag,omitempty"`
	Type          string   `json:"type,omitempty"`
	Index         *int     `json:"index,omitempty"`
	All           *bool    `json:"all,omitempty"`
	Visible       *bool    `json:"visible,omitempty"`
	Enabled       *bool    `json:"enabled,omitempty"`
	Ref           string   `json:"ref,omitempty"`
	X             *float64 `json:"x,omitempty"`
	Y             *float64 `json:"y,omitempty"`
}

// domCommand mirrors htrcli's api.Command.
type domCommand struct {
	ID      string         `json:"id"`
	Action  string         `json:"action"`
	Target  *domTarget     `json:"target,omitempty"`
	Value   string         `json:"value,omitempty"`
	Options map[string]any `json:"options,omitempty"`
}

// domPageInfo mirrors htrcli's api.PageInfo.
type domPageInfo struct {
	URL            string  `json:"url"`
	Title          string  `json:"title"`
	Domain         string  `json:"domain"`
	ReadyState     string  `json:"readyState,omitempty"`
	ScrollX        float64 `json:"scrollX"`
	ScrollY        float64 `json:"scrollY"`
	ViewportWidth  int     `json:"viewportWidth"`
	ViewportHeight int     `json:"viewportHeight"`
	DocumentHeight int     `json:"documentHeight"`
	DocumentWidth  int     `json:"documentWidth"`
	HistoryLength  int     `json:"historyLength,omitempty"`
}

// domResult mirrors htrcli's api.CommandResult. A failed command is a result
// with Success=false, not a Go error (same as the extension transport). Data
// stays raw so each caller decodes only the shape it asked for.
type domResult struct {
	ID       string          `json:"id"`
	Success  bool            `json:"success"`
	Data     json.RawMessage `json:"data,omitempty"`
	Error    string          `json:"error,omitempty"`
	Duration int             `json:"duration,omitempty"`
	PageInfo *domPageInfo    `json:"pageInfo,omitempty"`
}

// evalReply is the subset of Runtime.evaluate / callFunctionOn's reply used
// here. A script exception is turned into a Go error by the callers, never
// surfaced as a value.
type evalReply struct {
	Result struct {
		Subtype  string          `json:"subtype"`
		ObjectID string          `json:"objectId"`
		Value    json.RawMessage `json:"value"`
	} `json:"result"`
	ExceptionDetails *struct {
		Text      string `json:"text"`
		Exception *struct {
			Description string `json:"description"`
		} `json:"exception"`
	} `json:"exceptionDetails"`
}

// scopedEnvelope is the outcome of runScopedIn's wrapper: exactly one of Ok,
// Error (the function threw) or FrameError (the frame lookup failed) is set.
type scopedEnvelope struct {
	Ok         json.RawMessage `json:"ok"`
	Error      *string         `json:"error"`
	FrameError *string         `json:"frameError"`
}

// evaluateIn runs expr in the realm contextID (0 = the top document's default
// context). byValue selects returnByValue; awaitPromise is always on so the
// bundle's async exec resolves.
func (t *Target) evaluateIn(ctx context.Context, contextID int64, expr string, byValue bool) (evalReply, error) {
	params := map[string]any{"expression": expr, "returnByValue": byValue, "awaitPromise": true}
	if contextID != 0 {
		params["contextId"] = contextID
	}
	var res evalReply
	if err := t.conn.Call(ctx, t.sessionID, "Runtime.evaluate", params, &res); err != nil {
		return evalReply{}, err
	}
	if d := res.ExceptionDetails; d != nil {
		msg := d.Text
		if d.Exception != nil && d.Exception.Description != "" {
			msg = d.Exception.Description
		}
		return evalReply{}, fmt.Errorf("page exception: %s", msg)
	}
	return res, nil
}

// scopedExpr wraps a call to fnSrc(args...) so a thrown error comes back in the
// envelope instead of as a script exception.
func scopedExpr(fnSrc string, args []any) (string, error) {
	if args == nil {
		args = []any{}
	}
	argsJSON, err := json.Marshal(args)
	if err != nil {
		return "", fmt.Errorf("encoding scoped call args: %w", err)
	}
	return fmt.Sprintf(`(async () => {
	try { return {ok: await (%s)(...%s)}; }
	catch (e) { return {error: String(e && e.message || e)}; }
})()`, fnSrc, argsJSON), nil
}

// runScopedIn calls fnSrc(args...) in realm contextID and returns its JSON
// value. A thrown error becomes a Go error.
func (t *Target) runScopedIn(ctx context.Context, contextID int64, fnSrc string, args ...any) (json.RawMessage, error) {
	expr, err := scopedExpr(fnSrc, args)
	if err != nil {
		return nil, err
	}
	res, err := t.evaluateIn(ctx, contextID, expr, true)
	if err != nil {
		return nil, err
	}
	var env scopedEnvelope
	if err := json.Unmarshal(res.Result.Value, &env); err != nil {
		return nil, fmt.Errorf("decoding scoped result: %w", err)
	}
	if env.FrameError != nil {
		return nil, errors.New(*env.FrameError)
	}
	if env.Error != nil {
		return nil, errors.New(*env.Error)
	}
	if env.Ok == nil {
		return json.RawMessage("null"), nil
	}
	return env.Ok, nil
}

// callFunctionOn invokes fnSrc with `this` = objectID and JSON-valued args.
// It is how a page function receives an element handle (a ref), which
// Runtime.evaluate cannot ship as JSON.
func (t *Target) callFunctionOn(ctx context.Context, objectID, fnSrc string, args ...any) (json.RawMessage, error) {
	callArgs := make([]map[string]any, 0, len(args))
	for _, a := range args {
		callArgs = append(callArgs, map[string]any{"value": a})
	}
	var res evalReply
	if err := t.conn.Call(ctx, t.sessionID, "Runtime.callFunctionOn", map[string]any{
		"functionDeclaration": fnSrc,
		"objectId":            objectID,
		"arguments":           callArgs,
		"returnByValue":       true,
		"awaitPromise":        true,
	}, &res); err != nil {
		return nil, err
	}
	if d := res.ExceptionDetails; d != nil {
		msg := d.Text
		if d.Exception != nil && d.Exception.Description != "" {
			msg = d.Exception.Description
		}
		return nil, fmt.Errorf("page exception: %s", msg)
	}
	return res.Result.Value, nil
}

// bundleNeeded reports whether a bundle call answered the sentinel, meaning
// the bundle is not installed in this realm. Checked before anything else so
// the sentinel is never mistaken for a result.
func bundleNeeded(raw json.RawMessage) bool {
	var probe struct {
		NeedBundle bool `json:"__htrcliNeedBundle"`
	}
	return json.Unmarshal(raw, &probe) == nil && probe.NeedBundle
}

// withBundle runs a realm call that depends on b. If the call answers the
// sentinel, b is installed and the call retried exactly once; a second
// sentinel means the install did not take and is an error.
func (t *Target) withBundle(ctx context.Context, contextID int64, b pageBundle, run func() (json.RawMessage, error)) (json.RawMessage, error) {
	raw, err := run()
	if err != nil {
		return nil, err
	}
	if !bundleNeeded(raw) {
		return raw, nil
	}
	if _, err := t.evaluateIn(ctx, contextID, b.install(), false); err != nil {
		return nil, fmt.Errorf("installing %s: %w", b.name, err)
	}
	if raw, err = run(); err != nil {
		return nil, err
	}
	if bundleNeeded(raw) {
		return nil, fmt.Errorf("installing %s: bundle did not register", b.name)
	}
	return raw, nil
}

// realmFor returns the execution context a command runs in: the top
// document's default context for frame "", otherwise the frame's main world.
func (t *Target) realmFor(ctx context.Context, frame string) (int64, error) {
	if frame == "" {
		return 0, nil
	}
	return t.resolveFrame(ctx, frame)
}

// execDOM runs one command through the DOM bundle in the realm of frame ("" =
// top document, otherwise a CSS selector naming an <iframe> of the top
// document). A command the page rejects comes back as Success=false.
func (t *Target) execDOM(ctx context.Context, frame string, cmd domCommand) (domResult, error) {
	contextID, err := t.realmFor(ctx, frame)
	if err != nil {
		return domResult{}, err
	}
	return t.execDOMIn(ctx, contextID, cmd)
}

// execDOMIn is execDOM for a realm already resolved by realmFor. Callers that
// make several bundle calls in one operation resolve the frame once, so every
// call lands in the same realm.
func (t *Target) execDOMIn(ctx context.Context, contextID int64, cmd domCommand) (domResult, error) {
	raw, err := t.withBundle(ctx, contextID, domBundle, func() (json.RawMessage, error) {
		return t.runScopedIn(ctx, contextID, execDOMFn, cmd)
	})
	if err != nil {
		return domResult{}, err
	}
	var res domResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return domResult{}, fmt.Errorf("decoding DOM command result: %w", err)
	}
	return res, nil
}

// callKit runs one form-kit method by name in the realm of frame, installing
// the kit on first use. The raw JSON value is returned for the caller to decode.
func (t *Target) callKit(ctx context.Context, frame, method string, args ...any) (json.RawMessage, error) {
	if args == nil {
		args = []any{}
	}
	contextID, err := t.realmFor(ctx, frame)
	if err != nil {
		return nil, err
	}
	return t.withBundle(ctx, contextID, formKit, func() (json.RawMessage, error) {
		return t.runScopedIn(ctx, contextID, callKitFn, method, args)
	})
}
