package notebus

import (
	"sort"
	"strconv"
	"strings"
)

// RenderReconcile formats a ReconcileOutput as text the
// orchestrator (main LLM) reads. The render is SHAPE only:
// it does not assign severity, does not pick winners
// between contradictions, and does not filter clusters. The
// strong model is the only judge.
//
// The format is:
//
//	## Reconciled notes (N clusters)
//
//	### Cluster C1: <anchor>
//	- Authors: a1, a2
//	- Seqs: 1, 3
//	- Body:
//	  <body>
//
//	### Cluster C2: ...
//
//	## Touches (N)
//	- seq N, by=ID, file=PATH, act=ACT
//
//	## Unreviewed partitions
//	- a3 (partition: ...)
//	(or "(none)" when all completed)
//
// The render is a pure function: same input → same output.
// The orchestrator's prompt cache depends on this.
func RenderReconcile(out ReconcileOutput) string {
	var b strings.Builder
	// Header.
	if len(out.Clusters) == 0 {
		b.WriteString("## Reconciled notes (0 clusters)\n\n")
	} else {
		b.WriteString("## Reconciled notes (")
		b.WriteString(strconv.Itoa(len(out.Clusters)))
		b.WriteString(" cluster")
		if len(out.Clusters) != 1 {
			b.WriteString("s")
		}
		b.WriteString(")\n\n")
	}
	// Clusters, numbered for citation. The order in
	// ReconcileOutput is already sorted (see
	// ReconcilePrepass); we render in that order.
	for i, cl := range out.Clusters {
		b.WriteString("### Cluster C")
		b.WriteString(strconv.Itoa(i + 1))
		b.WriteString(": ")
		b.WriteString(cl.Anchor)
		b.WriteString("\n")
		if len(cl.Authors) > 0 {
			b.WriteString("- Authors: ")
			b.WriteString(strings.Join(cl.Authors, ", "))
			b.WriteString("\n")
		}
		if len(cl.Seqs) > 0 {
			seqStrs := make([]string, len(cl.Seqs))
			for j, s := range cl.Seqs {
				seqStrs[j] = strconv.FormatInt(s, 10)
			}
			b.WriteString("- Seqs: ")
			b.WriteString(strings.Join(seqStrs, ", "))
			b.WriteString("\n")
		}
		b.WriteString("- Body:\n  ")
		b.WriteString(cl.Body)
		b.WriteString("\n\n")
	}
	// Touches.
	b.WriteString("## Touches (")
	b.WriteString(strconv.Itoa(len(out.Touches)))
	b.WriteString(")\n")
	if len(out.Touches) == 0 {
		b.WriteString("(none)\n")
	} else {
		for _, t := range out.Touches {
			b.WriteString("- seq ")
			b.WriteString(strconv.FormatInt(t.Seq, 10))
			b.WriteString(", by=")
			b.WriteString(t.By)
			b.WriteString(", file=")
			b.WriteString(t.File)
			b.WriteString(", act=")
			b.WriteString(t.Act)
			b.WriteString("\n")
		}
	}
	b.WriteString("\n")
	// Unreviewed partitions.
	b.WriteString("## Unreviewed partitions\n")
	if len(out.Unreviewed) == 0 {
		b.WriteString("(none — all agents completed)\n")
	} else {
		// Sort for determinism even if the upstream
		// already did (defense in depth).
		unreviewed := append([]string(nil), out.Unreviewed...)
		sort.Strings(unreviewed)
		for _, id := range unreviewed {
			b.WriteString("- ")
			b.WriteString(id)
			b.WriteString("\n")
		}
	}
	return b.String()
}
