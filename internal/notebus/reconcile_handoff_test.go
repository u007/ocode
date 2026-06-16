package notebus

import "testing"

// TestReconcileHandoff_RenderIncludesClusters: a render of
// the reconcile output to text (the format the parent LLM
// reads) includes the clusters, the touches, and the
// unreviewed list. The strong model is the consumer; the
// shape of the hand-off is the API.
func TestReconcileHandoff_RenderIncludesClusters(t *testing.T) {
	out := ReconcileOutput{
		Clusters: []Cluster{
			{Anchor: "x.go:foo", Body: "missing nil check", Authors: []string{"a1", "a2"}, Seqs: []int64{1, 3}},
		},
		Touches: []Entry{
			Touch(2, "a2", "x.go", "edit", 0),
		},
		Unreviewed: []string{"a3"},
	}
	r := RenderReconcile(out)
	// Cluster anchor + body must appear.
	if !contains(r, "x.go:foo") {
		t.Errorf("render missing cluster anchor:\n%s", r)
	}
	if !contains(r, "missing nil check") {
		t.Errorf("render missing cluster body:\n%s", r)
	}
	// Authors must be visible (provenance).
	if !contains(r, "a1") || !contains(r, "a2") {
		t.Errorf("render missing author provenance:\n%s", r)
	}
	// Touches section must be present.
	if !contains(r, "Touches") && !contains(r, "touches") {
		t.Errorf("render missing touches section:\n%s", r)
	}
	// Unreviewed section must be present and named.
	if !contains(r, "Unreviewed") && !contains(r, "unreviewed") {
		t.Errorf("render missing unreviewed section:\n%s", r)
	}
	if !contains(r, "a3") {
		t.Errorf("render missing unreviewed agent (a3):\n%s", r)
	}
}

// TestReconcileHandoff_EmptyUnreviewed: when all agents
// completed, the unreviewed section is empty (or omitted
// with a "(none)" marker). The user must not see a phantom
// "unreviewed" warning when there is nothing to flag.
func TestReconcileHandoff_EmptyUnreviewed(t *testing.T) {
	out := ReconcileOutput{
		Clusters: []Cluster{{Anchor: "x.go", Body: "ok", Authors: []string{"a1"}, Seqs: []int64{1}}},
	}
	r := RenderReconcile(out)
	if !contains(r, "Unreviewed") {
		t.Errorf("render missing Unreviewed section header:\n%s", r)
	}
	// The unreviewed section must explicitly state none
	// (so the LLM does not infer failure from absence).
	if !contains(r, "(none)") && !contains(r, "no unreviewed") &&
		!contains(r, "all agents completed") {
		t.Errorf("render missing '(none)' marker in unreviewed section:\n%s", r)
	}
}

// TestReconcileHandoff_NoJudgmentInCode: the render is
// SHAPE only — it does not assign severity, does not pick
// winners between contradictions, and does not filter
// clusters. A cluster with two contradicting bodies must
// be rendered with both bodies visible (so the LLM, not
// the code, decides).
func TestReconcileHandoff_NoJudgmentInCode(t *testing.T) {
	out := ReconcileOutput{
		Clusters: []Cluster{
			{Anchor: "x.go:foo", Body: "looks fine", Authors: []string{"a1"}, Seqs: []int64{1}},
			{Anchor: "x.go:foo", Body: "missing nil check", Authors: []string{"a2"}, Seqs: []int64{2}},
		},
	}
	r := RenderReconcile(out)
	if !contains(r, "looks fine") {
		t.Errorf("render dropped body 'looks fine':\n%s", r)
	}
	if !contains(r, "missing nil check") {
		t.Errorf("render dropped body 'missing nil check':\n%s", r)
	}
	// The render must NOT say "REJECTED" or "ACCEPTED" or
	// pick a winner. The LLM is the only judge.
	for _, judgmentWord := range []string{"REJECTED", "ACCEPTED", "INVALID", "VALID", "TRUST", "DISTRUST"} {
		if contains(r, judgmentWord) {
			t.Errorf("render made a judgment (%q) — code must not:\n%s", judgmentWord, r)
		}
	}
}

// TestReconcileHandoff_ContradictionsDetected: a multi-
// cluster anchor (different bodies, same file/symbol)
// surfaces as a contradiction the strong model must judge.
// The pre-pass marks it; the render surfaces it.
func TestReconcileHandoff_ContradictionsDetected(t *testing.T) {
	out := ReconcileOutput{
		Clusters: []Cluster{
			{Anchor: "x.go:foo", Body: "looks fine", Authors: []string{"a1"}, Seqs: []int64{1}},
			{Anchor: "x.go:foo", Body: "missing nil check", Authors: []string{"a2"}, Seqs: []int64{2}},
		},
	}
	ctr := out.Contradictions()
	if len(ctr) != 1 {
		t.Fatalf("contradictions = %d, want 1", len(ctr))
	}
	if _, ok := ctr["x.go:foo"]; !ok {
		t.Errorf("contradictions missing x.go:foo: %v", ctr)
	}
}

// TestReconcileHandoff_Deterministic: same ReconcileOutput
// → same rendered text. The render is a pure function; the
// prompt cache depends on this.
func TestReconcileHandoff_Deterministic(t *testing.T) {
	out := ReconcileOutput{
		Clusters: []Cluster{
			{Anchor: "x.go:foo", Body: "b", Authors: []string{"a2", "a1"}, Seqs: []int64{2, 1}},
			{Anchor: "a.go:bar", Body: "c", Authors: []string{"a1"}, Seqs: []int64{3}},
		},
	}
	r1 := RenderReconcile(out)
	r2 := RenderReconcile(out)
	if r1 != r2 {
		t.Errorf("render is not deterministic:\n--- r1 ---\n%s\n--- r2 ---\n%s", r1, r2)
	}
}


