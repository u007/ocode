package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// wheelRenderInterval caps mouse-wheel–driven re-renders to the renderer's
// flush cadence (default 60fps). bubbletea's event loop calls model.View() —
// a full-screen renderContent() — after every message, but the cursed renderer
// only stores that view and paints on its FPS ticker, so any View() built
// faster than one frame is computed and immediately discarded. macOS trackpad
// momentum scrolling floods MouseWheelMsg events; this is most visible while
// pinned at the top/bottom of the transcript, where the view can't move so the
// user keeps swiping and sustains the flood. Without throttling, each event
// triggers a wasted full re-render and pegs a CPU core until momentum decays.
const wheelRenderInterval = time.Second / 60

// wheelThrottle coalesces a flood of mouse-wheel events down to at most one per
// frame. The clock is passed in (rather than read internally) so the policy is
// deterministically testable.
type wheelThrottle struct {
	last     time.Time
	interval time.Duration
}

// allow reports whether a wheel event arriving at now should be processed.
// Events within one interval of the last processed event are dropped — the
// frame they would render is discarded by the renderer anyway. A deliberate
// single notch (well over interval apart) always passes.
func (w *wheelThrottle) allow(now time.Time) bool {
	if !w.last.IsZero() && now.Sub(w.last) < w.interval {
		return false
	}
	w.last = now
	return true
}

// newInputFilter returns a bubbletea message filter that coalesces flooding
// mouse-wheel events. Returning nil from a filter makes the event loop skip
// both Update and the post-Update render for that message, which is exactly
// what bounds the render rate during a momentum flood. All other messages pass
// through untouched.
func newInputFilter() func(tea.Model, tea.Msg) tea.Msg {
	throttle := wheelThrottle{interval: wheelRenderInterval}
	return func(_ tea.Model, msg tea.Msg) tea.Msg {
		if _, ok := msg.(tea.MouseWheelMsg); ok {
			if !throttle.allow(time.Now()) {
				return nil
			}
		}
		return msg
	}
}
