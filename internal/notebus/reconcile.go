package notebus

import "sort"

// AgentStatus is the per-agent completion record passed to
// the pre-pass. Status is one of "completed", "failed",
// "cancelled". Partition is the dimension or file the agent
// was assigned (used in Unreviewed output so the user can
// see which slice of the work was missed).
type AgentStatus struct {
	Status    string
	Partition string
}

// AgentStatuses is the map keyed by agent id.
type AgentStatuses map[string]AgentStatus

// Cluster is one deduped note in the pre-pass output. Notes
// with the same (Anchor, Body) collapse into a single
// Cluster. The Authors list and the Seqs list preserve all
// provenance — the strong model can see "a1 and a2 both
// flagged this exact finding at seq 3 and 7". Different
// bodies on the same anchor produce different Clusters
// (that's a contradiction the model must settle).
type Cluster struct {
	Anchor  string   // file:snippet or "x.go" or whatever At the agent wrote
	Body    string   // the note body (entity-encoded)
	Authors []string // sorted unique list of agents
	Seqs    []int64  // sorted ascending
}

// Contradictions returns the set of anchors where the
// pre-pass produced multiple clusters (different bodies on
// the same anchor). These are the candidates the strong
// model must either settle (pick one) or escalate to a
// verify agent. The map is keyed by anchor; the value is
// the cluster ids in pre-pass order (1-indexed, matching
// the render).
//
// An anchor with one cluster has no contradiction; an
// anchor with two or more clusters has a contradiction
// the model must judge.
func (o ReconcileOutput) Contradictions() map[string][]int {
	out := map[string][]int{}
	byAnchor := map[string][]int{}
	for i, cl := range o.Clusters {
		byAnchor[cl.Anchor] = append(byAnchor[cl.Anchor], i+1)
	}
	for anchor, ids := range byAnchor {
		if len(ids) >= 2 {
			out[anchor] = ids
		}
	}
	return out
}

// ReconcileOutput is the structured handoff from the
// mechanical pre-pass to the strong-model reconcile pass.
// It is pure data — no methods, no shared state — so the
// strong model can ingest it deterministically across
// runs.
type ReconcileOutput struct {
	// Clusters: deduped notes the model must judge. Sorted
	// by (file, anchor, first-seen-seq) so the prompt
	// renders in a stable order.
	Clusters []Cluster
	// Touches: write-touches recorded during the run. The
	// strong model uses these to know which files were
	// modified (so a "no caller map for this function"
	// finding can be cross-checked against who actually
	// edited it). Sorted by (file, seq).
	Touches []Entry
	// Unreviewed: agent ids that did not complete. Sorted
	// ascending. The orchestrator must surface these to
	// the user — silent coverage gaps are the design's
	// non-negotiable failure mode.
	Unreviewed []string
}

// ReconcilePrepass is the mechanical pre-pass: a pure
// function that takes the bus log and per-agent statuses and
// returns a structured clustering. It performs three
// reductions:
//
//  1. Drop resolved notes (entries whose seq is in the
//     resolve set; the bus tracks resolves separately from
//     notes).
//  2. Cluster exact-duplicate notes (same anchor + same
//     body) and keep all authors and seqs in provenance.
//  3. List agents with non-completed status (failed or
//     cancelled) in the Unreviewed section.
//
// Output is sorted deterministically: clusters by (file,
// anchor, first-seen-seq), seqs ascending, authors sorted,
// touches by (file, seq), unreviewed sorted.
//
// The function is pure: no model call, no network, no
// goroutines, no shared state. It is the deterministic
// ground-truth for the strong-model pass that follows.
func ReconcilePrepass(log2 []Entry, statuses AgentStatuses) ReconcileOutput {
	// Build the set of resolved seqs.
	resolved := map[int64]bool{}
	for _, e := range log2 {
		if e.Kind == KindResolve {
			resolved[e.Ref] = true
		}
	}
	// Pass 1: notes, with resolved dropped, grouped by
	// (anchor, body). A separate bucket per key.
	type bucket struct {
		anchor  string
		body    string
		authors map[string]bool
		seqs    []int64
	}
	grouped := map[string]*bucket{}
	var keyOrder []string // preserve insertion order; we sort later
	for _, e := range log2 {
		if e.Kind != KindNote {
			continue
		}
		if e.By == BriefAuthor {
			// Orchestrator brief context (change-set summary,
			// partition assignments) — not an agent finding, so it
			// must not surface as a reconcile cluster.
			continue
		}
		if resolved[e.Seq] {
			continue
		}
		k := e.At + "\x00" + e.Body
		b, ok := grouped[k]
		if !ok {
			b = &bucket{anchor: e.At, body: e.Body, authors: map[string]bool{}}
			grouped[k] = b
			keyOrder = append(keyOrder, k)
		}
		if e.By != "" {
			b.authors[e.By] = true
		}
		b.seqs = append(b.seqs, e.Seq)
	}
	// Pass 2: emit Clusters sorted by (anchor, body) for
	// determinism.
	sort.Strings(keyOrder)
	out := ReconcileOutput{}
	for _, k := range keyOrder {
		b := grouped[k]
		cl := Cluster{
			Anchor: b.anchor,
			Body:   b.body,
		}
		for a := range b.authors {
			cl.Authors = append(cl.Authors, a)
		}
		sort.Strings(cl.Authors)
		sort.Slice(b.seqs, func(i, j int) bool { return b.seqs[i] < b.seqs[j] })
		cl.Seqs = b.seqs
		out.Clusters = append(out.Clusters, cl)
	}
	// Pass 3: touches, sorted by (file, seq). Touches are
	// evidence, not clusters — they are passed through as
	// a side channel.
	for _, e := range log2 {
		if e.Kind == KindTouch {
			out.Touches = append(out.Touches, e)
		}
	}
	sort.Slice(out.Touches, func(i, j int) bool {
		if out.Touches[i].File != out.Touches[j].File {
			return out.Touches[i].File < out.Touches[j].File
		}
		return out.Touches[i].Seq < out.Touches[j].Seq
	})
	// Pass 4: unreviewed agents (failed or cancelled),
	// sorted ascending. We iterate statuses in sorted key
	// order so the output is deterministic regardless of
	// map iteration order.
	if len(statuses) > 0 {
		keys := make([]string, 0, len(statuses))
		for k := range statuses {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			s := statuses[k]
			if s.Status == "failed" || s.Status == "cancelled" {
				out.Unreviewed = append(out.Unreviewed, k)
			}
		}
	}
	return out
}
