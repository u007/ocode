package cdp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"slices"
	"strings"
	"time"
)

// Settle and Controls are the page reads the browser_run step loop makes after
// each action. Controls is a structured sibling of Snapshot: the same
// accessibility read and refusal, but JSON-shaped controls instead of text.

// settleExpr resolves {settled} once readyState has left "loading" and quiet
// milliseconds have passed with no DOM mutation, or resolves settled=false once
// budget milliseconds have passed. Every path disconnects the observer, clears
// both timers and removes the readystatechange listener.
const settleExpr = `new Promise((resolve) => {
	const budget = %d, quiet = %d;
	let timer = null, observer = null, begun = false, finished = false;
	const onState = () => { if (document.readyState !== "loading") begin(); };
	const finish = (settled) => {
		if (finished) return;
		finished = true;
		clearTimeout(timer);
		clearTimeout(cap);
		document.removeEventListener("readystatechange", onState);
		if (observer) observer.disconnect();
		resolve({settled: settled});
	};
	const armQuiet = () => { clearTimeout(timer); timer = setTimeout(() => finish(true), quiet); };
	const begin = () => {
		if (begun || finished) return;
		begun = true;
		observer = new MutationObserver(armQuiet);
		observer.observe(document, {subtree: true, childList: true, attributes: true, characterData: true});
		armQuiet();
	};
	const cap = setTimeout(() => finish(false), budget);
	document.addEventListener("readystatechange", onState);
	onState();
})`

// navigatedAwayMessages are the CDP errors an evaluation returns when the
// document it targets is replaced while it is pending or about to start.
var navigatedAwayMessages = []string{
	"Execution context was destroyed",
	"Cannot find default execution context",
}

// controlOperations maps an accessibility role to the operations a control with
// that role offers. Each list is sorted, and a disabled control offers none.
var controlOperations = map[string][]string{
	"button":           {"click"},
	"checkbox":         {"check", "uncheck"},
	"combobox":         {"click", "fill"},
	"link":             {"click"},
	"menuitem":         {"click"},
	"menuitemcheckbox": {"click"},
	"menuitemradio":    {"click"},
	"option":           {"click"},
	"radio":            {"check", "uncheck"},
	"searchbox":        {"click", "fill"},
	"spinbutton":       {"click", "fill"},
	"switch":           {"check", "uncheck"},
	"tab":              {"click"},
	"textbox":          {"click", "fill"},
	"treeitem":         {"click"},
}

// controlContextRoles are the roles whose named ancestors give a control its
// Context.
var controlContextRoles = map[string]bool{
	"dialog":     true,
	"form":       true,
	"group":      true,
	"heading":    true,
	"list":       true,
	"navigation": true,
	"region":     true,
	"table":      true,
}

// Control is one interactive element of the top document, as the browser_run
// decision loop reads it. Ref is valid for every element action.
type Control struct {
	Ref        string   `json:"ref"`
	Role       string   `json:"role"`
	Label      string   `json:"label"`
	Value      string   `json:"value,omitempty"`
	States     []string `json:"states"`
	Context    string   `json:"context,omitempty"`
	Operations []string `json:"operations"`
}

// ControlsPage is the result of Controls. Controls holds at most
// controlsMaxElements entries in document order; Omitted counts the rest.
// ViewportText is the page's visible text, cut to viewportTextMaxChars UTF-16
// units, and ViewportTruncated says whether it was cut. Settled reports whether
// the page went quiet before the settle bound ran out.
type ControlsPage struct {
	URL               string    `json:"url"`
	Title             string    `json:"title"`
	Fingerprint       string    `json:"fingerprint"`
	ViewportText      string    `json:"viewport_text"`
	ViewportTruncated bool      `json:"viewport_truncated"`
	Settled           bool      `json:"settled"`
	Controls          []Control `json:"controls"`
	Omitted           int       `json:"omitted"`
}

// Settle waits for the top document to go quiet after an action: readyState is
// no longer "loading", then settleQuietWindow passes with no DOM mutation. The
// wait is bounded by settleMaxWait. When the bound runs out first, Settle
// returns settled=false and no error, and the caller reads the page as it
// stands. A navigation that replaces the document mid-wait is logged, and the
// wait resumes on the new document within the same bound. Any other CDP error
// is returned.
func (t *Target) Settle(ctx context.Context) (bool, error) {
	deadline := time.Now().Add(settleMaxWait)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return false, nil
		}
		expr := fmt.Sprintf(settleExpr, remaining.Milliseconds(), settleQuietWindow.Milliseconds())
		reply, err := t.evaluateIn(ctx, 0, expr, true)
		if err == nil {
			var out struct {
				Settled *bool `json:"settled"`
			}
			if err := json.Unmarshal(reply.Result.Value, &out); err != nil {
				return false, fmt.Errorf("settle: decoding the settle result: %w", err)
			}
			if out.Settled == nil {
				return false, errors.New("settle: the settle result has no settled flag")
			}
			return *out.Settled, nil
		}
		if !isNavigatedAway(err) {
			return false, fmt.Errorf("settle: %w", err)
		}
		log.Printf("browse cdp: settle: the page navigated during the wait, waiting on the new document: %v", err)
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-time.After(waitPollInterval):
		}
	}
}

// isNavigatedAway reports whether err is a CDP error saying the evaluated
// document was replaced.
func isNavigatedAway(err error) bool {
	var cdpErr *CDPError
	if !errors.As(err, &cdpErr) {
		return false
	}
	for _, msg := range navigatedAwayMessages {
		if strings.Contains(cdpErr.Message, msg) {
			return true
		}
	}
	return false
}

// Controls settles the top document, then reads its interactive controls from
// the accessibility tree. The tree read and its refusal are the same as
// Snapshot's. Each control gets a ref from refs, and only the kept controls mint
// one. The result also carries the URL, the title (the root node's name), the
// viewport text and a fingerprint of the URL and the controls listing.
func (t *Target) Controls(ctx context.Context, refs *RefTable) (ControlsPage, error) {
	if refs == nil {
		return ControlsPage{}, errors.New("controls: nil RefTable; pass the agent's ref table")
	}
	settled, err := t.Settle(ctx)
	if err != nil {
		return ControlsPage{}, fmt.Errorf("controls: settling the page: %w", err)
	}
	nodes, err := t.axNodesFor(ctx, "controls", "")
	if err != nil {
		return ControlsPage{}, err
	}
	w := &controlsWalker{byID: axIndex(nodes), refs: refs, controls: []Control{}}
	w.walk(&nodes[0], false)

	var url string
	if err := t.topValue(ctx, "location.href", &url); err != nil {
		return ControlsPage{}, fmt.Errorf("controls: reading the page URL: %w", err)
	}
	var text struct {
		Text  string `json:"text"`
		Total int    `json:"total"`
	}
	if err := t.topValue(ctx, fmt.Sprintf(pageTextExpr, viewportTextMaxChars), &text); err != nil {
		return ControlsPage{}, fmt.Errorf("controls: reading the page text: %w", err)
	}
	fingerprint, err := controlsFingerprint(url, text.Text, w.controls, w.omitted)
	if err != nil {
		return ControlsPage{}, fmt.Errorf("controls: %w", err)
	}
	return ControlsPage{
		URL:               url,
		Title:             axString(nodes[0].Name),
		Fingerprint:       fingerprint,
		ViewportText:      text.Text,
		ViewportTruncated: text.Total > viewportTextMaxChars,
		Settled:           settled,
		Controls:          w.controls,
		Omitted:           w.omitted,
	}, nil
}

// topValue evaluates expr in the top document and decodes its by-value result
// into out.
func (t *Target) topValue(ctx context.Context, expr string, out any) error {
	reply, err := t.evaluateIn(ctx, 0, expr, true)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(reply.Result.Value, out); err != nil {
		return fmt.Errorf("decoding the evaluated value: %w", err)
	}
	return nil
}

// controlsWalker collects the controls of one accessibility tree in document
// order. contexts holds the names of the named context ancestors of the node
// being walked, outermost first.
type controlsWalker struct {
	byID     map[string]*axNode
	refs     *RefTable
	contexts []string
	controls []Control
	omitted  int
}

// walk visits n and its subtree. suppressText has the same meaning as in
// snapshotWriter.walk, so a transparent node is passed through the same way.
func (w *controlsWalker) walk(n *axNode, suppressText bool) {
	role := axString(n.Role)
	if axTransparent(n, role, suppressText) {
		for _, c := range w.children(n) {
			w.walk(c, suppressText)
		}
		return
	}
	name := axString(n.Name)
	pushed := controlContextRoles[role] && name != ""
	if pushed {
		w.contexts = append(w.contexts, name)
	}
	// A control without a backendDOMNodeId has nothing to act on, so it is left
	// out of the listing and is not counted in Omitted.
	if ops, ok := controlOperations[role]; ok && n.BackendDOMNodeID != 0 {
		w.add(n, role, name, ops)
	}
	for _, c := range w.children(n) {
		w.walk(c, name != "")
	}
	if pushed {
		w.contexts = w.contexts[:len(w.contexts)-1]
	}
}

// children returns the nodes of n's ChildIDs that are present in the tree, in
// ChildIDs order.
func (w *controlsWalker) children(n *axNode) []*axNode {
	out := make([]*axNode, 0, len(n.ChildIDs))
	for _, id := range n.ChildIDs {
		if c := w.byID[id]; c != nil {
			out = append(out, c)
		}
	}
	return out
}

// add keeps a control while the listing is under controlsMaxElements and counts
// the rest in Omitted. Only a kept control mints a ref, so an omitted control
// leaves the ref table alone.
func (w *controlsWalker) add(n *axNode, role, name string, ops []string) {
	if len(w.controls) >= controlsMaxElements {
		w.omitted++
		return
	}
	states := axStates(n.Properties)
	c := Control{
		Ref:        w.refs.Mint(n.BackendDOMNodeID),
		Role:       role,
		Label:      clipText(name, controlLabelMax),
		Value:      clipText(axString(n.Value), controlValueMax),
		States:     append([]string{}, states...),
		Operations: []string{},
	}
	if len(w.contexts) > 0 {
		c.Context = clipText(w.contexts[len(w.contexts)-1], controlLabelMax)
	}
	if !slices.Contains(states, "disabled") {
		c.Operations = append(c.Operations, ops...)
	}
	w.controls = append(w.controls, c)
}

// controlFingerprintKey is what a fingerprint covers for one control: what the
// decision loop reads, without the ref. A ref changes when the page re-renders
// its nodes, and a re-render that leaves the page the same should not change
// the fingerprint.
type controlFingerprintKey struct {
	Role       string   `json:"role"`
	Label      string   `json:"label"`
	Value      string   `json:"value"`
	States     []string `json:"states"`
	Context    string   `json:"context"`
	Operations []string `json:"operations"`
}

// controlsFingerprint is the SHA-256 of the URL, the visible page text and the
// controls listing, cut to fingerprintHexLen hex digits. The text is included
// because a page can answer an action by changing only text (a validation error
// appearing, a cart total updating, a toast), and a fingerprint blind to that
// would make a real response look like a stall. A changed label, value, state,
// operation or visible text changes it.
func controlsFingerprint(url, text string, controls []Control, omitted int) (string, error) {
	keys := make([]controlFingerprintKey, 0, len(controls))
	for _, c := range controls {
		keys = append(keys, controlFingerprintKey{
			Role:       c.Role,
			Label:      c.Label,
			Value:      c.Value,
			States:     c.States,
			Context:    c.Context,
			Operations: c.Operations,
		})
	}
	raw, err := json.Marshal(struct {
		URL      string                  `json:"url"`
		Text     string                  `json:"text"`
		Omitted  int                     `json:"omitted"`
		Controls []controlFingerprintKey `json:"controls"`
	}{URL: url, Text: text, Omitted: omitted, Controls: keys})
	if err != nil {
		return "", fmt.Errorf("encoding the controls fingerprint: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])[:fingerprintHexLen], nil
}
