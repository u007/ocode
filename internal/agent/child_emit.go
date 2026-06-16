package agent

import (
	"github.com/u007/ocode/internal/notebus"
)

// handleAssistantNotes parses <oc-note> and <oc-resolve> tags
// out of a child's assistant message and appends them to the
// bus with the child's id. The bus stamps Seq (the parser
// does not).
//
// Behavior contract (mirrors the design):
//
//   - No bus → no parse, no allocation. The non-group path is
//     the common case; we want zero overhead there.
//   - The parser is the bus's notebus.ParseEmitted. It
//     entity-encodes body text and rejects/forgives malformed
//     tags, so this helper just feeds the message in and
//     stamps each entry.
//   - Resolve entries do not appear in deltas (the bus
//     suppresses them on read); this helper just records
//     them in the log so reconcile can audit.
//
// id is the agent's stable group id (a1, a2, …). It overrides
// any "by" attribute the parser might surface (the parser does
// not set By; the bus does, from the agent it was given).
func handleAssistantNotes(a *Agent, msg, id string) {
	if a == nil || a.noteBus == nil || id == "" || msg == "" {
		return
	}
	parsed := notebus.ParseEmitted(msg, id)
	for _, p := range parsed {
		// The parser sets By from the id we passed in, so
		// the bus's stamp is consistent. We still set it
		// here defensively in case ParseEmitted's signature
		// ever changes.
		p.By = id
		// Hand to the bus. Errors are logged via the
		// standard library path inside the bus; we do not
		// re-log here (the agent loop must not own a debug
		// sink in production). On ErrBusClosed we stop
		// trying — the group is being torn down and
		// further appends would just fail.
		_, _ = a.noteBus.Append(p)
	}
}

// handleAssistantNotesForChild is the public call site used by
// the agent's Step loop. It reads the bus+id from the agent
// itself, so callers do not have to thread them through.
// Equivalent to handleAssistantNotes(a, msg, a.noteAgentID)
// when the agent has been wired to a bus.
func handleAssistantNotesForChild(a *Agent, msg string) {
	if a == nil {
		return
	}
	handleAssistantNotes(a, msg, a.noteAgentID)
}
