package server

import (
	"log"
	"net/http"
	"strings"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/session"
	"github.com/u007/ocode/internal/tool"
)

// btwMaxSteps caps a /btw side-query loop, mirroring the TUI's btwMaxSteps, so
// a runaway tool chain cannot burn tokens forever in the panel.
const btwMaxSteps = 8

// btwAskLoopFn runs the side-query loop for /btw. It is a package var so tests
// can substitute a deterministic loop: the production path (AskLoopAsync)
// builds a FRESH client from the session's model, which needs a live provider,
// while the handler's own contract — 202, frame phases, cancel, generations —
// is what the tests assert. Mirrors the saveCompactConfigPatch / revealPathFn
// seams elsewhere in this package.
var btwAskLoopFn = func(a *agent.Agent, msgs []agent.Message, opts agent.AskLoopOptions, onResult func(string, error)) func() {
	return a.AskLoopAsync(msgs, opts, onResult)
}

// btwRun is one in-flight /btw side query for a session.
type btwRun struct {
	generation uint64
	// cancel stops the loop. Nil once the run finished, was cancelled, or was
	// superseded.
	cancel func()
	// done is set when the run finished or was cancelled, so late OnMessage /
	// OnDelta / onResult callbacks for it publish nothing.
	done bool
}

// btwFrame is the session-scoped `btw` bus payload: one frame per phase.
//   - started  — question set; opens the panel
//   - activity — a tool-activity line (Text)
//   - delta    — streamed answer text (Text)
//   - done     — the final answer (Text)
//   - error    — a startup/loop failure (Error)
type btwFrame struct {
	Generation uint64 `json:"generation"`
	Phase      string `json:"phase"`
	Question   string `json:"question,omitempty"`
	Text       string `json:"text,omitempty"`
	Error      string `json:"error,omitempty"`
}

// HandleBtw starts an INDEPENDENT /btw side query for a session: the transcript
// plus the aside run through agent.AskLoopAsync on a child agent with its own
// client, tool-capable but non-interactive. Neither the aside, the tool
// activity, nor the answer is written to the transcript or the live turn — the
// aside is NOT injected and nothing is appended. Progress rides the session-
// scoped `btw` bus event.
//
// It replies 202 immediately (no connection pinning); the answer streams over
// the bus. A second /btw cancels the first; DELETE cancels the current one.
func (h *Handler) HandleBtw(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		Content string `json:"content"`
	}
	if err := readBodyJSON(r, &req); err != nil || req.Content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}
	content := req.Content

	as, err := h.getOrCreateAgentSession(id)
	if err != nil || as == nil || as.agent == nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	msgs := h.btwMessages(id, as)
	msgs = append(msgs, agent.Message{Role: "user", Content: content})

	// Register first so the run is visible to cancel/replace, then cancel the
	// previous one outside the lock (cancel may kill processes).
	gen, prevCancel := h.registerBtwRun(id)
	if prevCancel != nil {
		prevCancel()
	}
	h.publishBusEvent("btw", id, btwFrame{Generation: gen, Phase: "started", Question: content})

	cancel := h.startBtwLoop(as.agent, id, gen, msgs)
	h.recordBtwCancel(id, gen, cancel)

	writeJSON(w, http.StatusAccepted, map[string]any{"status": "started", "generation": gen})
}

// HandleBtwCancel stops the in-flight side query for a session. Idempotent: a
// session with no run still answers 200. The run entry is RETAINED (marked
// done) so the generation stays monotonic; /reset-id deletes it.
func (h *Handler) HandleBtwCancel(w http.ResponseWriter, r *http.Request, id string) {
	h.cancelBtwRun(id, false)
	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

// btwMessages snapshots the session transcript for the side query. It uses
// as.mu.TryLock — never Lock — because a live turn holds as.mu for its whole
// duration and must not be blocked by this HTTP read; on contention it falls
// back to the persisted transcript. A failure to load is logged and yields an
// empty history (the side query still answers the aside on its own).
func (h *Handler) btwMessages(id string, as *agentSession) []agent.Message {
	var src []agent.Message
	if as.mu.TryLock() {
		src = make([]agent.Message, len(as.messages))
		copy(src, as.messages)
		as.mu.Unlock()
	} else {
		entry, err := h.sessions.Resolve(id)
		if err != nil {
			log.Printf("btw: resolve %s for transcript snapshot: %v", id, err)
			return nil
		}
		s, err := session.LoadForDir(entry.ProjectRoot, id)
		if err != nil || s == nil {
			log.Printf("btw: load %s transcript for snapshot: %v", id, err)
			return nil
		}
		src = s.Messages
	}
	// Neutralise the ask sentinels, as the TUI's snapshot does. `/btw`
	// mid-turn is precisely when the main turn may be parked on a
	// permission/question ask, and feeding the raw PERMISSION_ASK:… /
	// WAITING_FOR_USER_RESPONSE JSON to the side-query model leaks internal
	// protocol and degrades the answer.
	out := make([]agent.Message, 0, len(src))
	for _, m := range src {
		if strings.HasPrefix(m.Content, tool.SentinelPermissionAsk) {
			// Keep the tool message so the child's transcript sees a result
			// for the parked call (prevents recoverOrphanedToolCalls from
			// re-executing an unapproved tool), but replace the internal
			// protocol payload with a neutral placeholder.
			mc := m
			mc.Content = "[permission ask]"
			out = append(out, mc)
			continue
		}
		if m.Role == "tool" && strings.Contains(m.Content, tool.SentinelWaitingForUser) {
			mc := m
			mc.Content = "[waiting for user]"
			out = append(out, mc)
			continue
		}
		out = append(out, m)
	}
	return out
}

// startBtwLoop launches the side query and wires its callbacks to bus frames.
// Callbacks are gated on the run still being current, so a superseded or
// cancelled run publishes nothing.
func (h *Handler) startBtwLoop(a *agent.Agent, id string, gen uint64, msgs []agent.Message) func() {
	opts := agent.AskLoopOptions{
		// Tools nil -> the child gets the agent's current tool set; the
		// exclusion list then removes the non-interactive tools.
		ExcludedTools: agent.BtwExcludedTools,
		MaxSteps:      btwMaxSteps,
		OnMessage: func(am agent.Message) {
			if !h.btwRunActive(id, gen) {
				return
			}
			if line := agent.FormatSideQueryActivity(am); line != "" {
				h.publishBusEvent("btw", id, btwFrame{Generation: gen, Phase: "activity", Text: line})
			}
		},
		OnDelta: func(kind, text string) {
			if kind != "text" || text == "" || !h.btwRunActive(id, gen) {
				return
			}
			h.publishBusEvent("btw", id, btwFrame{Generation: gen, Phase: "delta", Text: text})
		},
	}
	return btwAskLoopFn(a, msgs, opts, func(content string, err error) {
		if !h.claimBtwRunFinish(id, gen) {
			return
		}
		if err != nil {
			h.publishBusEvent("btw", id, btwFrame{Generation: gen, Phase: "error", Error: err.Error()})
			return
		}
		h.publishBusEvent("btw", id, btwFrame{Generation: gen, Phase: "done", Text: content})
	})
}

// registerBtwRun installs a fresh run for id under btwMu, returning its
// generation and the previous run's cancel func (nil when there was none). The
// caller cancels the previous run after releasing the lock.
func (h *Handler) registerBtwRun(id string) (uint64, func()) {
	h.btwMu.Lock()
	defer h.btwMu.Unlock()
	if h.btwRuns == nil {
		h.btwRuns = make(map[string]*btwRun)
	}
	var prev func()
	if cur := h.btwRuns[id]; cur != nil {
		prev = cur.cancel
	}
	h.btwSeq++
	gen := h.btwSeq
	h.btwRuns[id] = &btwRun{generation: gen}
	return gen, prev
}

// recordBtwCancel stores the run's cancel func once the loop has started. A
// run that already finished or was superseded is not stored; a superseded one
// is cancelled immediately.
func (h *Handler) recordBtwCancel(id string, gen uint64, cancel func()) {
	h.btwMu.Lock()
	cur := h.btwRuns[id]
	if cur == nil || cur.generation != gen {
		h.btwMu.Unlock()
		if cancel != nil {
			cancel()
		}
		return
	}
	if cur.done {
		// The run already finished — possibly cancelled during the child-build
		// window (cancelBtwRun marks done without a cancel func). Call the
		// freshly returned cancel anyway: it is idempotent for a finished loop,
		// and it is the only way to stop a loop the early cancel missed.
		h.btwMu.Unlock()
		if cancel != nil {
			cancel()
		}
		return
	}
	cur.cancel = cancel
	h.btwMu.Unlock()
}

// btwRunActive reports whether id's current run is gen and still in flight.
func (h *Handler) btwRunActive(id string, gen uint64) bool {
	h.btwMu.Lock()
	defer h.btwMu.Unlock()
	cur := h.btwRuns[id]
	return cur != nil && cur.generation == gen && !cur.done
}

// claimBtwRunFinish atomically marks a run finished so its terminal frame is
// published exactly once and late callbacks are ignored. Returns false when
// the run is no longer current or has already finished.
func (h *Handler) claimBtwRunFinish(id string, gen uint64) bool {
	h.btwMu.Lock()
	defer h.btwMu.Unlock()
	cur := h.btwRuns[id]
	if cur == nil || cur.generation != gen || cur.done {
		return false
	}
	cur.done = true
	cur.cancel = nil
	return true
}

// cancelBtwRun marks id's run done and invokes its cancel func outside the
// lock. When removeEntry is true the map entry is deleted too — used by
// /reset-id so nothing is stranded under the deleted session id.
func (h *Handler) cancelBtwRun(id string, removeEntry bool) {
	h.btwMu.Lock()
	run := h.btwRuns[id]
	if run != nil {
		run.done = true
	}
	if removeEntry {
		delete(h.btwRuns, id)
	}
	var cancel func()
	if run != nil {
		cancel = run.cancel
		run.cancel = nil
	}
	h.btwMu.Unlock()
	if cancel != nil {
		cancel()
	}
}
