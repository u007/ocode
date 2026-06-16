package agent

import (
	"github.com/u007/ocode/internal/notebus"
)

// seedBrief appends the orchestrator's pre-computed brief to
// the bus. The brief is a list of note entries authored as
// by="main" — the change set summary, partition assignments,
// and any other context the orchestrator has already computed
// once. Children see the brief in their first-loop delta
// (since by != agent), and the bus's per-agent completion
// state is unaffected.
//
// The function is a thin wrapper: the bus's Append assigns
// Seq (the entries in the brief come in with Seq=0 and the
// bus stamps real seqs starting at the current head+1).
// Calling on a nil bus or with a nil/empty brief is a no-op
// (the common case for callers that do not pre-compute a
// brief).
//
// The caller is responsible for ordering: the brief must be
// seeded BEFORE any child appends, so its seqs are strictly
// less than any child entry. The group-spawn path enforces
// this by calling seedBrief immediately after bus.Start
// and before any child is spawned.
func seedBrief(bus *notebus.Bus, brief []notebus.Entry) error {
	if bus == nil || len(brief) == 0 {
		return nil
	}
	for _, e := range brief {
		// Force By to the reserved brief author defensively, in
		// case the orchestrator's brief literals used a different
		// id. The bus stamps Seq and stores the entry with By as
		// given; the reconcile pre-pass excludes this author.
		e.By = notebus.BriefAuthor
		if _, err := bus.Append(e); err != nil {
			return err
		}
	}
	return nil
}
