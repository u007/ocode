package cdp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Read-only page actions: snapshot, find and wait. Each output is hard-capped
// and says so when the cap cut it short.

const (
	// snapshotMaxElements and snapshotMaxBytes cap one snapshot's tree.
	snapshotMaxElements = 400
	snapshotMaxBytes    = 32 * 1024
	// axNodeBudget caps how many accessibility nodes a snapshot will process.
	// The element and byte caps only apply once the whole tree has been read
	// into byID, so an enormous tree would still be walked and buffered in
	// full; a tree wider than this is refused instead of half-read.
	axNodeBudget = 20000
	// axMaxDepth is passed to Accessibility.getFullAXTree so Chrome bounds the
	// response it sends. Chrome may ignore it; axNodeBudget is the backstop.
	axMaxDepth = 200
	// findMaxElements is the largest max a find may ask for.
	findMaxElements = 200
	// findTextMax clips each found element's text.
	findTextMax = 200
	// waitMaxTimeout bounds one wait.
	waitMaxTimeout   = 30 * time.Second
	waitPollInterval = 100 * time.Millisecond
)

// findUniqueFn returns the element a CSS path matches when it matches exactly
// one, otherwise null.
const findUniqueFn = `((p) => { const els = document.querySelectorAll(p); return els.length === 1 ? els[0] : null; })`

// findCountFn counts the elements a CSS path matches (used to explain a null).
const findCountFn = `(p) => document.querySelectorAll(p).length`

// waitTextFn reports whether the realm's body text contains text.
const waitTextFn = `(text) => !!document.body && document.body.innerText.indexOf(text) !== -1`

// RefElement is one element a snapshot minted a ref for.
type RefElement struct {
	Ref  string `json:"ref"`
	Role string `json:"role"`
	Name string `json:"name,omitempty"`
}

// axValue, axProperty and axNode mirror the parts of Accessibility.getFullAXTree
// a snapshot reads.
type axValue struct {
	Value any `json:"value"`
}

type axProperty struct {
	Name  string  `json:"name"`
	Value axValue `json:"value"`
}

type axNode struct {
	NodeID           string       `json:"nodeId"`
	Ignored          bool         `json:"ignored"`
	Role             axValue      `json:"role"`
	Name             axValue      `json:"name"`
	Value            axValue      `json:"value"`
	Properties       []axProperty `json:"properties"`
	ChildIDs         []string     `json:"childIds"`
	BackendDOMNodeID int64        `json:"backendDOMNodeId"`
}

// snapshotStates lists the element states a snapshot line reports, in order.
var snapshotStates = []string{"disabled", "expanded", "checked", "selected", "pressed", "focused", "readonly"}

// Snapshot writes the accessibility tree of the top document (frame "") or of
// an <iframe> (a CSS selector) in the htrcli snapshot layout: one line per
// element, `role "name" value="..." @eN state`, indented by depth. The tree
// comes from Chrome's accessibility tree, not htrcli's DOM walk, because each
// node's backendNodeId is needed to mint a ref. Output stops at
// snapshotMaxElements or snapshotMaxBytes, and the text then ends with a
// [truncated: ...] line.
//
// The CDP response itself is depth-limited (axMaxDepth) and a page wider than
// axNodeBudget is refused, naming the count and the budget, rather than
// half-read: the caller should use find or text instead of snapshot.
func (t *Target) Snapshot(ctx context.Context, frame string, refs *RefTable) (string, []RefElement, error) {
	if refs == nil {
		return "", nil, errors.New("snapshot: nil RefTable; pass the agent's ref table")
	}
	params := map[string]any{"depth": axMaxDepth}
	if frame != "" {
		frameID, err := t.resolveFrameID(ctx, frame)
		if err != nil {
			return "", nil, err
		}
		params["frameId"] = frameID
	}
	var res struct {
		Nodes []axNode `json:"nodes"`
	}
	if err := t.conn.Call(ctx, t.sessionID, "Accessibility.getFullAXTree", params, &res); err != nil {
		return "", nil, fmt.Errorf("snapshot: Accessibility.getFullAXTree: %w", err)
	}
	if len(res.Nodes) > axNodeBudget {
		return "", nil, fmt.Errorf("snapshot: the accessibility tree has %d nodes, over the %d-node budget; use find or text instead of snapshot", len(res.Nodes), axNodeBudget)
	}
	if len(res.Nodes) == 0 {
		return "", nil, errors.New("snapshot: the accessibility tree is empty")
	}
	w := &snapshotWriter{byID: make(map[string]*axNode, len(res.Nodes)), refs: refs}
	for i := range res.Nodes {
		w.byID[res.Nodes[i].NodeID] = &res.Nodes[i]
	}
	w.walk(&res.Nodes[0], 0, false)
	return w.text(), w.minted, nil
}

// snapshotWriter accumulates a snapshot's lines under the element and byte caps.
type snapshotWriter struct {
	byID   map[string]*axNode
	refs   *RefTable
	lines  []string
	bytes  int
	count  int
	minted []RefElement
	stop   string
}

// walk writes n and its subtree. suppressText is set when n's parent already
// carries the text in its name, so a StaticText child is not written twice.
func (w *snapshotWriter) walk(n *axNode, depth int, suppressText bool) {
	if w.stop != "" {
		return
	}
	role := axString(n.Role)
	transparent := n.Ignored || role == "none" || role == "generic" || role == "InlineTextBox" ||
		(role == "StaticText" && suppressText)
	if transparent {
		for _, id := range n.ChildIDs {
			if c := w.byID[id]; c != nil {
				w.walk(c, depth, suppressText)
			}
		}
		return
	}
	name := axString(n.Name)
	if w.count >= snapshotMaxElements {
		w.stop = fmt.Sprintf("element cap of %d reached", snapshotMaxElements)
		return
	}
	printed := role
	switch role {
	case "RootWebArea":
		printed = "document"
	case "StaticText":
		printed = "text"
	}
	ref := ""
	if printed != "text" && printed != "document" && n.BackendDOMNodeID != 0 {
		ref = w.refs.Mint(n.BackendDOMNodeID)
	}
	line := snapshotLine(depth, printed, name, axString(n.Value), ref, axStates(n.Properties))
	if w.bytes+len(line)+1 > snapshotMaxBytes {
		w.stop = fmt.Sprintf("byte cap of %d bytes reached", snapshotMaxBytes)
		return
	}
	w.lines = append(w.lines, line)
	w.bytes += len(line) + 1
	w.count++
	if ref != "" {
		w.minted = append(w.minted, RefElement{Ref: ref, Role: printed, Name: name})
	}
	for _, id := range n.ChildIDs {
		if c := w.byID[id]; c != nil {
			w.walk(c, depth+1, name != "")
		}
	}
}

func (w *snapshotWriter) text() string {
	out := strings.Join(w.lines, "\n")
	if w.stop != "" {
		out += fmt.Sprintf("\n[truncated: %s; %d elements shown]", w.stop, w.count)
	}
	return out
}

// snapshotLine formats one element as htrcli's formatSnapshotNode does.
func snapshotLine(depth int, role, name, value, ref string, states []string) string {
	parts := []string{role}
	if name != "" {
		parts = append(parts, fmt.Sprintf("%q", name))
	}
	if value != "" {
		parts = append(parts, fmt.Sprintf("value=%q", value))
	}
	if ref != "" {
		parts = append(parts, ref)
	}
	parts = append(parts, states...)
	return strings.Repeat("  ", depth) + strings.Join(parts, " ")
}

// axStates reports the set states of an accessibility node, in snapshotStates
// order: a true flag as its name, a tristate or string as name=value. A false
// string is the same as absent.
func axStates(props []axProperty) []string {
	vals := make(map[string]any, len(props))
	for _, p := range props {
		vals[p.Name] = p.Value.Value
	}
	var out []string
	for _, key := range snapshotStates {
		v, ok := vals[key]
		if !ok {
			continue
		}
		switch x := v.(type) {
		case bool:
			if x {
				out = append(out, key)
			}
		case string:
			if x != "" && x != "false" {
				out = append(out, key+"="+x)
			}
		case nil:
		default:
			out = append(out, fmt.Sprintf("%s=%v", key, x))
		}
	}
	return out
}

// axString reads an accessibility value as text. Non-string values are
// rendered with fmt, so a numeric value (a slider) still shows.
func axString(v axValue) string {
	switch x := v.Value.(type) {
	case nil:
		return ""
	case string:
		return x
	default:
		return fmt.Sprint(x)
	}
}

// FoundElement is one element a find matched.
type FoundElement struct {
	// Ref is the minted ref, empty when Note explains why none was minted.
	Ref      string `json:"ref,omitempty"`
	Tag      string `json:"tag"`
	Text     string `json:"text,omitempty"`
	Selector string `json:"selector"`
	Note     string `json:"note,omitempty"`
}

// FindOutput is the result of FindElements. Total is every match; Elements holds
// at most max of them, and Truncated says whether any were left out.
type FindOutput struct {
	Elements  []FoundElement `json:"elements"`
	Total     int            `json:"total"`
	Truncated bool           `json:"truncated"`
}

// FindElements finds the elements a selector names in the top document (frame
// "") or in an <iframe> (a CSS selector), and mints a ref for each. The selector
// forms are css (the default), name=, label= and text=, as htrcli's parser reads
// them. The bundle does the matching; the CSS path it reports for each element
// is resolved back in the same realm, and an element whose path matches more
// than one element gets no ref, only a note.
func (t *Target) FindElements(ctx context.Context, frame, selector string, max int, refs *RefTable) (FindOutput, error) {
	if refs == nil {
		return FindOutput{}, errors.New("find: nil RefTable; pass the agent's ref table")
	}
	if max <= 0 || max > findMaxElements {
		return FindOutput{}, fmt.Errorf("find: max must be between 1 and %d, got %d", findMaxElements, max)
	}
	target, err := parseFindSelector(selector)
	if err != nil {
		return FindOutput{}, err
	}
	contextID, err := t.realmFor(ctx, frame)
	if err != nil {
		return FindOutput{}, err
	}
	res, err := t.execDOMIn(ctx, contextID, domCommand{ID: "find", Action: "findAll", Target: &target})
	if err != nil {
		return FindOutput{}, err
	}
	if !res.Success {
		return FindOutput{}, fmt.Errorf("find %q: %s", selector, res.Error)
	}
	var infos []struct {
		Tag      string `json:"tag"`
		Text     string `json:"text"`
		Selector string `json:"selector"`
	}
	if err := json.Unmarshal(res.Data, &infos); err != nil {
		return FindOutput{}, fmt.Errorf("find %q: decoding matches: %w", selector, err)
	}
	out := FindOutput{Total: len(infos), Truncated: len(infos) > max}
	for i, info := range infos {
		if i >= max {
			break
		}
		// The bundle reports the document element as an empty path (its path
		// builder stops at documentElement); :root names the same element.
		path := info.Selector
		if path == "" {
			path = ":root"
		}
		el := FoundElement{Tag: info.Tag, Text: clipText(info.Text, findTextMax), Selector: path}
		backendID, note, err := t.elementForPath(ctx, contextID, path)
		if err != nil {
			return FindOutput{}, fmt.Errorf("find %q: %w", selector, err)
		}
		if note != "" {
			el.Note = note
		} else {
			el.Ref = refs.Mint(backendID)
		}
		out.Elements = append(out.Elements, el)
	}
	return out, nil
}

// elementForPath resolves a CSS path to the backendNodeId of the one element it
// matches, in realm contextID. A path that matches several elements returns a
// note instead, because the ref would then be a guess.
func (t *Target) elementForPath(ctx context.Context, contextID int64, path string) (int64, string, error) {
	pathJSON, err := json.Marshal(path)
	if err != nil {
		return 0, "", fmt.Errorf("encoding CSS path: %w", err)
	}
	res, err := t.evaluateIn(ctx, contextID, fmt.Sprintf("%s(%s)", findUniqueFn, pathJSON), false)
	if err != nil {
		return 0, "", err
	}
	if res.Result.Subtype == "null" {
		raw, err := t.runScopedIn(ctx, contextID, findCountFn, path)
		if err != nil {
			return 0, "", err
		}
		var n int
		if err := json.Unmarshal(raw, &n); err != nil {
			return 0, "", fmt.Errorf("decoding match count for %s: %w", path, err)
		}
		if n == 0 {
			return 0, "", fmt.Errorf("the matched element for CSS path %s is gone; the page changed while finding, retry", path)
		}
		return 0, fmt.Sprintf("ambiguous: CSS path %s matches %d elements, no ref minted", path, n), nil
	}
	var desc struct {
		Node struct {
			BackendNodeID int64 `json:"backendNodeId"`
		} `json:"node"`
	}
	if err := t.conn.Call(ctx, t.sessionID, "DOM.describeNode", map[string]any{"objectId": res.Result.ObjectID}, &desc); err != nil {
		return 0, "", fmt.Errorf("DOM.describeNode for %s: %w", path, err)
	}
	if desc.Node.BackendNodeID == 0 {
		return 0, "", fmt.Errorf("DOM.describeNode for %s returned no backendNodeId", path)
	}
	return desc.Node.BackendNodeID, "", nil
}

// parseFindSelector maps a selector argument to the bundle's target. CSS is the
// default; name=, label= and text= are the htrcli forms this port supports.
// Refs, coordinates and the other htrcli forms are rejected rather than
// silently treated as CSS.
func parseFindSelector(arg string) (domTarget, error) {
	switch {
	case arg == "":
		return domTarget{}, errors.New("selector is empty")
	case strings.HasPrefix(arg, "@e"):
		return domTarget{}, fmt.Errorf("%q is an element ref, not a selector; pass the ref to the action directly", arg)
	case strings.HasPrefix(arg, "xy="):
		return domTarget{}, fmt.Errorf("%q: coordinate selectors are not supported here", arg)
	case strings.HasPrefix(arg, "name="):
		return domTarget{Name: strings.TrimPrefix(arg, "name=")}, nil
	case strings.HasPrefix(arg, "label="):
		return domTarget{Label: strings.TrimPrefix(arg, "label=")}, nil
	case strings.HasPrefix(arg, "text="):
		return domTarget{Text: strings.TrimPrefix(arg, "text=")}, nil
	case strings.HasPrefix(arg, "role="), strings.HasPrefix(arg, "placeholder="),
		strings.HasPrefix(arg, "id="), strings.HasPrefix(arg, "xpath="):
		return domTarget{}, fmt.Errorf("%q: this selector form is not supported here (use css, name=, label= or text=)", arg)
	}
	return domTarget{Selector: arg}, nil
}

// clipText shortens s to n runes, marking the cut with an ellipsis.
func clipText(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// WaitSpec is one wait condition. Exactly one of Selector (any selector form
// FindElements accepts) or Text is set. Gone inverts the sense: the wait
// succeeds once nothing matches or the text is absent.
type WaitSpec struct {
	Selector string
	Text     string
	Gone     bool
	Timeout  time.Duration
}

// Wait polls the top document (frame "") or an <iframe> until spec holds. The
// realm is re-resolved on every poll, so a frame that navigates mid-wait is
// followed. A timeout over waitMaxTimeout is an error, not a silent clamp.
func (t *Target) Wait(ctx context.Context, frame string, spec WaitSpec) error {
	if (spec.Selector == "") == (spec.Text == "") {
		return errors.New("wait: set exactly one of a selector or text")
	}
	if spec.Timeout <= 0 {
		return errors.New("wait: timeout must be > 0")
	}
	if spec.Timeout > waitMaxTimeout {
		return fmt.Errorf("wait: timeout %s exceeds the %s cap", spec.Timeout, waitMaxTimeout)
	}
	var target domTarget
	if spec.Selector != "" {
		var err error
		if target, err = parseFindSelector(spec.Selector); err != nil {
			return err
		}
	}
	deadline := time.Now().Add(spec.Timeout)
	for {
		present, err := t.waitProbe(ctx, frame, spec, target)
		if err != nil && !errors.Is(err, errFrameNotReady) {
			return err
		}
		if err == nil && present != spec.Gone {
			return nil
		}
		if !time.Now().Before(deadline) {
			if err != nil {
				return fmt.Errorf("wait: timeout after %s: %w", spec.Timeout, err)
			}
			return fmt.Errorf("wait: timeout after %s", spec.Timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(waitPollInterval):
		}
	}
}

// waitProbe reports whether the wait's element or text is present right now.
func (t *Target) waitProbe(ctx context.Context, frame string, spec WaitSpec, target domTarget) (bool, error) {
	contextID, err := t.realmFor(ctx, frame)
	if err != nil {
		return false, err
	}
	if spec.Text != "" {
		raw, err := t.runScopedIn(ctx, contextID, waitTextFn, spec.Text)
		if err != nil {
			return false, err
		}
		var found bool
		if err := json.Unmarshal(raw, &found); err != nil {
			return false, fmt.Errorf("wait: decoding text check: %w", err)
		}
		return found, nil
	}
	res, err := t.execDOMIn(ctx, contextID, domCommand{ID: "wait", Action: "findAll", Target: &target})
	if err != nil {
		return false, err
	}
	if !res.Success {
		return false, fmt.Errorf("wait %q: %s", spec.Selector, res.Error)
	}
	var items []json.RawMessage
	if err := json.Unmarshal(res.Data, &items); err != nil {
		return false, fmt.Errorf("wait %q: decoding matches: %w", spec.Selector, err)
	}
	return len(items) > 0, nil
}
