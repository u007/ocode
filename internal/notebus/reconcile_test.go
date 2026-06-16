package notebus

import (
	"sort"
	"testing"
)

// TestReconcilePrepass_DropsResolved: a note that has been
// resolved is dropped from the reconcile output entirely. The
// bus's Delta() suppresses resolved notes on read; reconcile
// must agree so a resolved stale lead never reaches the
// orchestrator.
func TestReconcilePrepass_DropsResolved(t *testing.T) {
	log2 := []Entry{
		Note(1, "a1", "x.go:foo", "a1 finding", 0),
		Resolve(2, "a2", 1, 0),
		Note(3, "a3", "y.go:bar", "still active", 0),
	}
	statuses := AgentStatuses{}
	out := ReconcilePrepass(log2, statuses)
	if len(out.Clusters) != 1 {
		t.Fatalf("clusters = %d, want 1 (resolved note dropped)", len(out.Clusters))
	}
	if out.Clusters[0].Anchor != "y.go:bar" {
		t.Errorf("kept cluster anchor = %q, want y.go:bar", out.Clusters[0].Anchor)
	}
}

// TestReconcilePrepass_CollapsesExactDuplicates: two notes
// with the same file/symbol anchor and the same body text
// collapse to ONE cluster, but the Authors list records all
// authors. This is the dedup guarantee: a flood of identical
// findings from multiple agents does not produce N findings.
func TestReconcilePrepass_CollapsesExactDuplicates(t *testing.T) {
	log2 := []Entry{
		Note(1, "a1", "x.go:foo", "same body", 0),
		Note(2, "a2", "x.go:foo", "same body", 0),
		Note(3, "a3", "x.go:foo", "same body", 0),
	}
	out := ReconcilePrepass(log2, AgentStatuses{})
	if len(out.Clusters) != 1 {
		t.Fatalf("clusters = %d, want 1 (dedup)", len(out.Clusters))
	}
	// Authors is the sorted unique set.
	got := out.Clusters[0].Authors
	want := []string{"a1", "a2", "a3"}
	if !stringSlicesEqual(got, want) {
		t.Errorf("Authors = %v, want %v", got, want)
	}
	// The seqs are preserved as provenance — the strong
	// model can see who saw what and when.
	if len(out.Clusters[0].Seqs) != 3 {
		t.Errorf("Seqs len = %d, want 3", len(out.Clusters[0].Seqs))
	}
}

// TestReconcilePrepass_ExcludesBriefAuthor: orchestrator-seeded brief
// entries (By == BriefAuthor) are shared context, not agent findings,
// and must never surface as reconcile clusters. Only genuine agent
// notes cluster.
func TestReconcilePrepass_ExcludesBriefAuthor(t *testing.T) {
	log2 := []Entry{
		Note(1, BriefAuthor, "partition:a1", "review auth", 0),
		Note(2, BriefAuthor, "change-set:summary", "3 files changed", 0),
		Note(3, "a1", "x.go:foo", "real finding", 0),
	}
	out := ReconcilePrepass(log2, AgentStatuses{})
	if len(out.Clusters) != 1 {
		t.Fatalf("clusters = %d, want 1 (brief excluded)", len(out.Clusters))
	}
	if out.Clusters[0].Body != "real finding" {
		t.Errorf("cluster body = %q, want the agent finding", out.Clusters[0].Body)
	}
	for _, cl := range out.Clusters {
		for _, a := range cl.Authors {
			if a == BriefAuthor {
				t.Errorf("brief author %q leaked into clusters", BriefAuthor)
			}
		}
	}
}

// TestReconcilePrepass_DifferentBodiesNotDuped: notes that
// share an anchor but disagree on the body are NOT collapsed.
// This is the contradiction case — the strong model decides.
func TestReconcilePrepass_DifferentBodiesNotDuped(t *testing.T) {
	log2 := []Entry{
		Note(1, "a1", "x.go:foo", "looks fine", 0),
		Note(2, "a2", "x.go:foo", "missing nil check", 0),
	}
	out := ReconcilePrepass(log2, AgentStatuses{})
	if len(out.Clusters) != 2 {
		t.Fatalf("clusters = %d, want 2 (contradictions kept distinct)", len(out.Clusters))
	}
}

// TestReconcilePrepass_GroupsByFile: two notes on different
// files produce two clusters; two notes on the same file but
// different symbols produce two clusters. The grouping is
// (file, anchor) — both must match.
func TestReconcilePrepass_GroupsByFile(t *testing.T) {
	log2 := []Entry{
		Note(1, "a1", "x.go:foo", "a", 0),
		Note(2, "a1", "x.go:bar", "b", 0),
		Note(3, "a1", "y.go:foo", "c", 0),
	}
	out := ReconcilePrepass(log2, AgentStatuses{})
	if len(out.Clusters) != 3 {
		t.Errorf("clusters = %d, want 3 (one per file:anchor pair)", len(out.Clusters))
	}
}

// TestReconcilePrepass_Deterministic: same input → same
// output. The order of clusters is sorted by (file, anchor,
// seq), the order of seqs is sorted ascending. The
// orchestrator's reconcile pass must see a stable structure
// across runs (so the prompt cache hits).
func TestReconcilePrepass_Deterministic(t *testing.T) {
	mklog := func() []Entry {
		return []Entry{
			Note(1, "a2", "z.go:zzz", "z", 0),
			Note(2, "a1", "a.go:aaa", "a", 0),
			Note(3, "a3", "m.go:mmm", "m", 0),
		}
	}
	out1 := ReconcilePrepass(mklog(), AgentStatuses{})
	out2 := ReconcilePrepass(mklog(), AgentStatuses{})
	if len(out1.Clusters) != len(out2.Clusters) {
		t.Fatalf("cluster count differs across runs: %d vs %d", len(out1.Clusters), len(out2.Clusters))
	}
	for i := range out1.Clusters {
		if out1.Clusters[i].Anchor != out2.Clusters[i].Anchor {
			t.Errorf("cluster[%d] anchor differs: %q vs %q", i, out1.Clusters[i].Anchor, out2.Clusters[i].Anchor)
		}
	}
}

// TestReconcilePrepass_UnreviewedPartitions: agents whose
// status is failed or cancelled appear in the Unreviewed
// list with their assigned partition. The orchestrator
// surfaces this to the user so coverage is never silently
// implied.
func TestReconcilePrepass_UnreviewedPartitions(t *testing.T) {
	log2 := []Entry{
		Note(1, "a1", "x.go", "a1's note", 0),
		Note(2, "a2", "y.go", "a2's note", 0),
		Note(3, "a3", "z.go", "a3's note", 0),
	}
	statuses := AgentStatuses{
		"a1": AgentStatus{Status: "completed"},
		"a2": AgentStatus{Status: "failed"},
		"a3": AgentStatus{Status: "cancelled"},
	}
	out := ReconcilePrepass(log2, statuses)
	if len(out.Unreviewed) != 2 {
		t.Fatalf("unreviewed = %d, want 2 (a2 failed, a3 cancelled)", len(out.Unreviewed))
	}
	// Sort the strings for stable comparison.
	sort.Strings(out.Unreviewed)
	want := []string{"a2", "a3"}
	if !stringSlicesEqual(out.Unreviewed, want) {
		t.Errorf("unreviewed = %v, want %v", out.Unreviewed, want)
	}
}

// TestReconcilePrepass_CompletedAgentsHaveNoUnreviewed:
// when every agent completed, the Unreviewed list is empty.
func TestReconcilePrepass_CompletedAgentsHaveNoUnreviewed(t *testing.T) {
	log2 := []Entry{
		Note(1, "a1", "x.go", "a1", 0),
	}
	statuses := AgentStatuses{
		"a1": AgentStatus{Status: "completed"},
	}
	out := ReconcilePrepass(log2, statuses)
	if len(out.Unreviewed) != 0 {
		t.Errorf("unreviewed = %v, want empty", out.Unreviewed)
	}
}

// TestReconcilePrepass_DropsResolvesAndTouchesInCluster:
// the pre-pass output is the note-level clustering.
// Resolves and touches are NOT emitted as clusters — they
// are bookkeeping. Resolves suppress notes on the read
// side; touches are reconciliation evidence the strong
// model sees via a side channel. The pre-pass only emits
// note clusters the strong model needs to judge.
func TestReconcilePrepass_DropsResolvesAndTouchesInCluster(t *testing.T) {
	log2 := []Entry{
		// Two notes, one resolved. The pre-pass keeps
		// the un-resolved note as a cluster, drops the
		// resolved one entirely.
		Note(1, "a1", "x.go", "note 1", 0),
		Touch(2, "a2", "y.go", "edit", 0),
		Resolve(3, "a3", 1, 0),
		Note(4, "a4", "z.go", "note 4", 0),
	}
	out := ReconcilePrepass(log2, AgentStatuses{})
	if len(out.Clusters) != 1 {
		t.Errorf("clusters = %d, want 1 (only the un-resolved note)", len(out.Clusters))
	} else if out.Clusters[0].Anchor != "z.go" {
		t.Errorf("cluster anchor = %q, want z.go (note 4, the survivor)", out.Clusters[0].Anchor)
	}
	// The touch is preserved in a side channel for the
	// strong model to reference.
	if len(out.Touches) != 1 {
		t.Errorf("touches = %d, want 1 (touch preserved separately)", len(out.Touches))
	}
}

// stringSlicesEqual returns true iff a and b have the same
// length and identical sorted contents. (The test inputs
// are pre-sorted by the test cases; this is a defensive
// check, not a sort-and-compare.)
func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
