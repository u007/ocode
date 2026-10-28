package cdp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
)

// maxRefs bounds a RefTable's live refs. Without it the table only grows for
// the tab's lifetime, so a long session on a big page keeps every backend node
// id it ever minted.
const maxRefs = 2000

// RefTable mints the @eN element refs one agent holds for one tab. A ref is a
// CDP backendNodeId: it names the same node for the life of its document, so it
// survives scrolling, and resolveRef reports when the node is gone. The zero
// value is ready to use.
//
// The table is bounded at maxRefs live refs. Minting a new node at the cap
// clears the table first, which invalidates every ref held so far; that is safe
// because a stale ref already errors and the caller re-runs snapshot or find.
// The counter never resets, so a ref string is never reused.
type RefTable struct {
	mu     sync.Mutex
	next   int
	byRef  map[string]int64
	byNode map[int64]string
}

// Mint returns the ref for backendNodeID, minting a new one only the first
// time that node is seen, so repeated snapshots and finds stay stable. At
// maxRefs live refs a new node clears the table first; see the type comment.
func (r *RefTable) Mint(backendNodeID int64) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if ref, ok := r.byNode[backendNodeID]; ok {
		return ref
	}
	if r.byRef == nil {
		r.byRef = make(map[string]int64)
		r.byNode = make(map[int64]string)
	}
	if len(r.byRef) >= maxRefs {
		// Start a fresh generation. next is deliberately not reset, so the ref
		// strings minted after this clear are all new.
		r.byRef = make(map[string]int64)
		r.byNode = make(map[int64]string)
	}
	r.next++
	ref := "@e" + strconv.Itoa(r.next)
	r.byRef[ref] = backendNodeID
	r.byNode[backendNodeID] = ref
	return ref
}

// Lookup returns the backendNodeId behind ref. An unknown ref is an error; the
// caller must mint refs by snapshot or find before acting on them.
func (r *RefTable) Lookup(ref string) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id, ok := r.byRef[ref]
	if !ok {
		return 0, fmt.Errorf("unknown ref %q: mint refs with snapshot or find first", ref)
	}
	return id, nil
}

// cdpNoNodeMessage is the CDP error for a backendNodeId the browser no longer
// holds (the node was garbage-collected after its document changed).
const cdpNoNodeMessage = "No node with given id found"

// resolveRef turns a ref's backendNodeId into a live element handle. A node
// CDP no longer knows, one that resolves to nothing, or one removed from the
// document is a stale ref: the caller must re-run snapshot or find. Other
// errors (connection closed, timeout) pass through unchanged.
func (t *Target) resolveRef(ctx context.Context, backendNodeID int64) (string, error) {
	var res struct {
		Object struct {
			ObjectID string `json:"objectId"`
			Subtype  string `json:"subtype"`
		} `json:"object"`
	}
	err := t.conn.Call(ctx, t.sessionID, "DOM.resolveNode", map[string]any{"backendNodeId": backendNodeID}, &res)
	var cdpErr *CDPError
	if errors.As(err, &cdpErr) && strings.Contains(cdpErr.Message, cdpNoNodeMessage) {
		return "", staleRefError(backendNodeID)
	}
	if err != nil {
		return "", fmt.Errorf("DOM.resolveNode for backendNodeId %d: %w", backendNodeID, err)
	}
	if res.Object.ObjectID == "" || res.Object.Subtype == "null" {
		return "", staleRefError(backendNodeID)
	}
	// The browser keeps a removed node reachable through its backendNodeId
	// until it is collected, so resolution alone does not prove liveness.
	raw, err := t.callFunctionOn(ctx, res.Object.ObjectID, "function() { return this.isConnected === true; }")
	if err != nil {
		return "", fmt.Errorf("checking backendNodeId %d is connected: %w", backendNodeID, err)
	}
	var connected bool
	if err := json.Unmarshal(raw, &connected); err != nil {
		return "", fmt.Errorf("decoding isConnected for backendNodeId %d: %w", backendNodeID, err)
	}
	if !connected {
		return "", staleRefError(backendNodeID)
	}
	return res.Object.ObjectID, nil
}

func staleRefError(backendNodeID int64) error {
	return fmt.Errorf("stale ref: backendNodeId %d is no longer in the document (the page changed or navigated); re-run snapshot or find", backendNodeID)
}

// refCSSIdentity proves that a CSS path names the element behind objectID.
// Actions that go through the bundle by CSS path must run this first: a path
// that matches a different element, or none, would act on the wrong node.
func (t *Target) refCSSIdentity(ctx context.Context, objectID, path string) error {
	raw, err := t.callFunctionOn(ctx, objectID, "function(p) { return document.querySelector(p) === this; }", path)
	if err != nil {
		return fmt.Errorf("checking CSS path %q against the ref: %w", path, err)
	}
	var same bool
	if err := json.Unmarshal(raw, &same); err != nil {
		return fmt.Errorf("decoding CSS path check for %q: %w", path, err)
	}
	if !same {
		return fmt.Errorf("CSS path %q does not resolve back to the referenced element (it matches a different element or none); refusing to act on the wrong element", path)
	}
	return nil
}

// refCenter scrolls the ref's element into view and returns the point a mouse
// action dispatches at, in top-viewport CSS pixels. An element with one box is
// measured by DOM.getBoxModel: the centre of its content quad, which excludes
// padding and border. (DOM.getContentQuads reports the border box instead, so a
// button with padding would be clicked in its padding.) An inline element that
// wraps onto several lines has several fragment quads, and getBoxModel returns
// only their union, which may lie on no text; there the point is the centre of
// the largest fragment still in the viewport. The box is clipped to the viewport
// first, so the point is always on screen. The caller hit-tests this same point.
func (t *Target) refCenter(ctx context.Context, backendNodeID int64) (float64, float64, error) {
	if _, err := t.resolveRef(ctx, backendNodeID); err != nil {
		return 0, 0, err
	}
	if err := t.conn.Call(ctx, t.sessionID, "DOM.scrollIntoViewIfNeeded", map[string]any{"backendNodeId": backendNodeID}, nil); err != nil {
		return 0, 0, fmt.Errorf("DOM.scrollIntoViewIfNeeded for backendNodeId %d: %w", backendNodeID, err)
	}
	var res struct {
		Quads [][]float64 `json:"quads"`
	}
	if err := t.conn.Call(ctx, t.sessionID, "DOM.getContentQuads", map[string]any{"backendNodeId": backendNodeID}, &res); err != nil {
		return 0, 0, fmt.Errorf("DOM.getContentQuads for backendNodeId %d: %w", backendNodeID, err)
	}
	if len(res.Quads) == 0 {
		return 0, 0, fmt.Errorf("backendNodeId %d has no box to click (it is not rendered)", backendNodeID)
	}
	viewport, err := t.viewportRect(ctx)
	if err != nil {
		return 0, 0, err
	}
	var boxes []rect
	if len(res.Quads) == 1 {
		var box struct {
			Model struct {
				Content []float64 `json:"content"`
			} `json:"model"`
		}
		if err := t.conn.Call(ctx, t.sessionID, "DOM.getBoxModel", map[string]any{"backendNodeId": backendNodeID}, &box); err != nil {
			return 0, 0, fmt.Errorf("DOM.getBoxModel for backendNodeId %d: %w", backendNodeID, err)
		}
		if len(box.Model.Content) != 8 {
			return 0, 0, fmt.Errorf("DOM.getBoxModel for backendNodeId %d returned %d content coordinates, want a quad of 8", backendNodeID, len(box.Model.Content))
		}
		boxes = []rect{quadBounds(box.Model.Content)}
	} else {
		for _, q := range res.Quads {
			if len(q) != 8 {
				return 0, 0, fmt.Errorf("DOM.getContentQuads for backendNodeId %d returned a quad of %d coordinates, want 8", backendNodeID, len(q))
			}
			boxes = append(boxes, quadBounds(q))
		}
	}
	var best rect
	found := false
	for _, b := range boxes {
		r := b.clip(viewport)
		if r.empty() {
			continue
		}
		if !found || r.area() > best.area() {
			best, found = r, true
		}
	}
	if !found {
		return 0, 0, fmt.Errorf("backendNodeId %d has no part in the viewport to click; scroll it into view", backendNodeID)
	}
	x, y := best.centre()
	return x, y, nil
}

// viewportRect returns the top window's viewport in CSS pixels.
func (t *Target) viewportRect(ctx context.Context) (rect, error) {
	res, err := t.evaluateIn(ctx, 0, viewportSizeExpr, true)
	if err != nil {
		return rect{}, fmt.Errorf("reading the viewport size: %w", err)
	}
	var v struct {
		W float64 `json:"w"`
		H float64 `json:"h"`
	}
	if err := json.Unmarshal(res.Result.Value, &v); err != nil {
		return rect{}, fmt.Errorf("decoding the viewport size: %w", err)
	}
	return rect{x0: 0, y0: 0, x1: v.W, y1: v.H}, nil
}

// rect is an axis-aligned box in CSS pixels; x1 and y1 are exclusive.
type rect struct{ x0, y0, x1, y1 float64 }

// quadBounds is the bounding box of a quad given as x,y pairs.
func quadBounds(q []float64) rect {
	r := rect{x0: q[0], y0: q[1], x1: q[0], y1: q[1]}
	for i := 2; i+1 < len(q); i += 2 {
		r.x0 = math.Min(r.x0, q[i])
		r.y0 = math.Min(r.y0, q[i+1])
		r.x1 = math.Max(r.x1, q[i])
		r.y1 = math.Max(r.y1, q[i+1])
	}
	return r
}

func (r rect) clip(o rect) rect {
	return rect{x0: math.Max(r.x0, o.x0), y0: math.Max(r.y0, o.y0), x1: math.Min(r.x1, o.x1), y1: math.Min(r.y1, o.y1)}
}

func (r rect) empty() bool { return r.x1 <= r.x0 || r.y1 <= r.y0 }

func (r rect) area() float64 {
	if r.empty() {
		return 0
	}
	return (r.x1 - r.x0) * (r.y1 - r.y0)
}

func (r rect) centre() (float64, float64) { return (r.x0 + r.x1) / 2, (r.y0 + r.y1) / 2 }
