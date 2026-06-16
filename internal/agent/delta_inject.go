package agent

import (
	"fmt"
	"sort"
	"strings"

	"github.com/u007/ocode/internal/notebus"
)

// ocLogMarker is the prefix we use on the injected block.
// It is a system-role message (not user/assistant) so the
// LLM treats it as a system-side signal, but we use a
// dedicated marker instead of one of the existing
// [ocode:*] markers so callers can grep for it and so the
// "this is volatile" intent is clear.
const ocLogMarker = "[ocode:notes]"

// injectNotesTail returns base + (optionally) one system
// message at the very tail that contains a single
// <oc-log since="N">…</oc-log> block with the agent's
// per-loop delta. The block is appended ONLY when the delta
// is non-empty — the empty-delta case is the cache-stability
// invariant, and the function never alters base.
//
// The block is the LAST system message so:
//   - All stable content (system prompt, transcript, prior
//     <oc-log> blocks) precedes it.
//   - Cached prefix is byte-identical across loops with no
//     new entries.
//   - The LLM sees the latest deltas as "the most recent
//     thing the system said", which is the natural reading
//     order.
//
// The agent's own notes never appear in its own delta. The
// bus handles this at Delta() time (it skips entries where
// By == agent); this helper just renders the result.
//
// base is not mutated. The returned slice is a new slice
// (the agent loop's caller pattern).
func injectNotesTail(base []Message, a *Agent) []Message {
	if a == nil || a.noteBus == nil || a.noteAgentID == "" {
		return base
	}
	delta := a.noteBus.Delta(a.noteAgentID)
	if len(delta) == 0 {
		return base
	}
	// Render the block. We always render in seq order, which
	// is the order the bus handed the slice in. A separate
	// sort is defensive but should be a no-op.
	sort.SliceStable(delta, func(i, j int) bool { return delta[i].Seq < delta[j].Seq })
	// Compute the head seq to put in the "since" attribute.
	// This is informational; the bus's Delta() function uses
	// its own internal state to decide what to return.
	head := a.noteBus.HeadSeq()
	rendered := renderOcLog(delta, head)
	out := make([]Message, 0, len(base)+1)
	out = append(out, base...)
	out = append(out, Message{Role: "system", Content: ocLogMarker + "\n" + rendered})
	return out
}

// renderOcLog builds the wire-form <oc-log> block. The block
// is one line of attributes followed by one entry per line.
// The format matches the design spec's example:
//
//	<oc-log since="42">
//	<oc-note  seq="43" by="a2" at="auth/token.go:tokenFromHeader">token empty -> panic, no nil check</oc-note>
//	<oc-touch seq="44" by="a3" file="internal/tool/patch.go" act="edit"/>
//	<oc-resolve seq="45" by="a3" ref="43"/>
//	</oc-log>
//
// The body of each note is the entity-encoded form (the bus
// stored it that way on parse). Touches and resolves carry
// only their structural fields — no bodies, no anchors
// beyond the file/act or ref.
func renderOcLog(delta []notebus.Entry, head int64) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<oc-log since=%q>\n", fmt.Sprintf("%d", head))
	for _, e := range delta {
		switch e.Kind {
		case notebus.KindNote:
			fmt.Fprintf(&b, `<oc-note  seq=%q by=%q at=%q>%s</oc-note>`+"\n",
				fmt.Sprintf("%d", e.Seq), e.By, e.At, e.Body)
		case notebus.KindTouch:
			fmt.Fprintf(&b, `<oc-touch seq=%q by=%q file=%q act=%q/>`+"\n",
				fmt.Sprintf("%d", e.Seq), e.By, e.File, e.Act)
		case notebus.KindResolve:
			fmt.Fprintf(&b, `<oc-resolve seq=%q by=%q ref=%q/>`+"\n",
				fmt.Sprintf("%d", e.Seq), e.By, fmt.Sprintf("%d", e.Ref))
		}
	}
	b.WriteString("</oc-log>")
	return b.String()
}
