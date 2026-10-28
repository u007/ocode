package cdp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
)

// errFrameNotReady marks a frame that does not exist yet, is still loading, or
// has no main-world context announced. A wait keeps polling through it; every
// other frame error (bad selector, not an iframe, cross-origin) is final.
var errFrameNotReady = errors.New("frame not ready")

// Same-origin iframe targeting for the page-DOM helpers. A frame is named by a
// CSS selector that must match an <iframe> in the top document. Its main-world
// execution context comes from the Runtime.executionContextCreated events
// recorded by startContextTracking, so a call runs in the frame's own realm
// (the bundle's `window`/`document` bind to the frame) and is exempt from the
// frame document's CSP, as htrcli's --frame path is (htrcli cdp/frame.go).
//
// Cross-origin frames are rejected: their contentDocument is null to the parent
// page, and an out-of-process frame lives in its own CDP target.

// frameContexts maps a frame id to the id of its default (main-world)
// execution context. The zero value is ready to use.
type frameContexts struct {
	mu      sync.Mutex
	byFrame map[string]int64
}

func (f *frameContexts) record(frameID string, contextID int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.byFrame == nil {
		f.byFrame = make(map[string]int64)
	}
	f.byFrame[frameID] = contextID
}

// forget drops the mapping that points at contextID, if one does. A destroyed
// context that was already superseded by a newer one for the same frame
// leaves the newer mapping alone.
func (f *frameContexts) forget(contextID int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for frameID, id := range f.byFrame {
		if id == contextID {
			delete(f.byFrame, frameID)
		}
	}
}

func (f *frameContexts) clear() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.byFrame = nil
}

func (f *frameContexts) lookup(frameID string) (int64, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id, ok := f.byFrame[frameID]
	return id, ok
}

// startContextTracking records the default execution context of every frame as
// Chrome announces it. Runtime must already be enabled (Attach does that), and
// the subscriptions must be live before the frames are created: a context
// announced earlier is not replayed, so an iframe that existed before this
// tab was attached is reported as not resolvable rather than guessed at.
func (t *Target) startContextTracking() {
	chCreated, c1 := t.conn.SubscribeLossless(t.sessionID, "Runtime.executionContextCreated")
	chDestroyed, c2 := t.conn.SubscribeLossless(t.sessionID, "Runtime.executionContextDestroyed")
	chCleared, c3 := t.conn.SubscribeLossless(t.sessionID, "Runtime.executionContextsCleared")
	t.cancels = append(t.cancels, c1, c2, c3)
	go t.handleExecutionContextCreated(chCreated)
	go t.handleExecutionContextDestroyed(chDestroyed)
	go t.handleExecutionContextsCleared(chCleared)
}

func (t *Target) handleExecutionContextCreated(ch <-chan json.RawMessage) {
	for raw := range ch {
		var ev struct {
			Context struct {
				ID      int64 `json:"id"`
				AuxData struct {
					FrameID   string `json:"frameId"`
					IsDefault bool   `json:"isDefault"`
				} `json:"auxData"`
			} `json:"context"`
		}
		if err := json.Unmarshal(raw, &ev); err != nil {
			log.Printf("browse cdp: decoding Runtime.executionContextCreated: %v", err)
			continue
		}
		// Only the main world answers for a frame; isolated worlds (extensions,
		// devtools) carry isDefault=false and are never the page's realm.
		if !ev.Context.AuxData.IsDefault || ev.Context.AuxData.FrameID == "" {
			continue
		}
		t.frameCtx.record(ev.Context.AuxData.FrameID, ev.Context.ID)
	}
}

func (t *Target) handleExecutionContextDestroyed(ch <-chan json.RawMessage) {
	for raw := range ch {
		var ev struct {
			ExecutionContextID int64 `json:"executionContextId"`
		}
		if err := json.Unmarshal(raw, &ev); err != nil {
			log.Printf("browse cdp: decoding Runtime.executionContextDestroyed: %v", err)
			continue
		}
		t.frameCtx.forget(ev.ExecutionContextID)
	}
}

func (t *Target) handleExecutionContextsCleared(ch <-chan json.RawMessage) {
	for range ch {
		t.frameCtx.clear()
	}
}

// resolveFrameID returns the frame id of the <iframe> that frame names, after
// checking in the top document that it is a same-origin iframe whose document
// has finished loading. Every failure is an error naming the frame.
func (t *Target) resolveFrameID(ctx context.Context, frame string) (string, error) {
	selJSON, err := json.Marshal(frame)
	if err != nil {
		return "", fmt.Errorf("encoding frame selector: %w", err)
	}
	expr := fmt.Sprintf(`(() => {
	const sel = %s;
	let f;
	try { f = document.querySelector(sel); } catch (e) {
		return {frameError: "frame " + JSON.stringify(sel) + " is not a valid CSS selector: " + e.message};
	}
	if (!f) return {frameNotReady: "frame " + JSON.stringify(sel) + ": no element matches it in the top-level document"};
	if (f.tagName !== "IFRAME" && f.tagName !== "FRAME") {
		return {frameError: "frame " + JSON.stringify(sel) + " matches a <" + f.tagName.toLowerCase() + ">, not an <iframe>"};
	}
	const d = f.contentDocument;
	// An iframe whose src is cross-origin is cross-origin even before it has
	// navigated: until then contentDocument is the inherited about:blank.
	const srcOrigin = f.src && !f.src.startsWith("about:") ? new URL(f.src).origin : location.origin;
	if (!d || srcOrigin !== location.origin) {
		return {frameError: "frame " + JSON.stringify(sel) + " is a cross-origin iframe (src=" + JSON.stringify(f.src) +
			"); its document is not reachable from the parent page. Open the frame directly instead: navigate a tab to " + f.src};
	}
	if (d.readyState === "loading") {
		return {frameNotReady: "frame " + JSON.stringify(sel) + " is still loading"};
	}
	return {ok: true};
})()`, selJSON)
	check, err := t.evaluateIn(ctx, 0, expr, true)
	if err != nil {
		return "", fmt.Errorf("resolving frame %q: %w", frame, err)
	}
	var env struct {
		FrameError    *string `json:"frameError"`
		FrameNotReady *string `json:"frameNotReady"`
	}
	if err := json.Unmarshal(check.Result.Value, &env); err != nil {
		return "", fmt.Errorf("resolving frame %q: decoding check: %w", frame, err)
	}
	if env.FrameError != nil {
		return "", errors.New(*env.FrameError)
	}
	if env.FrameNotReady != nil {
		return "", fmt.Errorf("%w: %s", errFrameNotReady, *env.FrameNotReady)
	}
	el, err := t.evaluateIn(ctx, 0, fmt.Sprintf("document.querySelector(%s)", selJSON), false)
	if err != nil {
		return "", fmt.Errorf("resolving frame %q: %w", frame, err)
	}
	if el.Result.ObjectID == "" {
		return "", fmt.Errorf("resolving frame %q: the iframe disappeared while it was being resolved", frame)
	}
	var desc struct {
		Node struct {
			FrameID string `json:"frameId"`
		} `json:"node"`
	}
	if err := t.conn.Call(ctx, t.sessionID, "DOM.describeNode", map[string]any{"objectId": el.Result.ObjectID}, &desc); err != nil {
		return "", fmt.Errorf("resolving frame %q: DOM.describeNode: %w", frame, err)
	}
	if desc.Node.FrameID == "" {
		return "", fmt.Errorf("resolving frame %q: DOM.describeNode reported no frameId for the <iframe>", frame)
	}
	return desc.Node.FrameID, nil
}

// resolveFrame returns the main-world execution context id of the <iframe>
// that frame names. A frame whose context has not been announced yet (still
// loading, or created before this tab was attached) is an error, never a
// guess at which context to use.
func (t *Target) resolveFrame(ctx context.Context, frame string) (int64, error) {
	frameID, err := t.resolveFrameID(ctx, frame)
	if err != nil {
		return 0, err
	}
	contextID, ok := t.frameCtx.lookup(frameID)
	if !ok {
		return 0, fmt.Errorf("frame %q: %w: no main-world execution context recorded for frame %s (it may still be loading, or it existed before this tab was attached)", frame, errFrameNotReady, frameID)
	}
	return contextID, nil
}
