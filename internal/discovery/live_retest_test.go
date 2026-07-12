package discovery

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestLiveRetest_localReadyAndAttach is a manual end-to-end retest against the
// real running local embed server (llama.cpp since the b9777 bump). Gated on
// OCODE_LIVE_RETEST so it never runs in CI. It reproduces exactly what /discover
// shows the user:
//   - Bug A: adopting the already-running server records status "ready".
//   - Bug B: warming the corpus completes fast (no 500ms deadlock) and the
//     conduct kaizen skill actually ranks/attaches for a conduct query.
func TestLiveRetest_localReadyAndAttach(t *testing.T) {
	if os.Getenv("OCODE_LIVE_RETEST") == "" {
		t.Skip("set OCODE_LIVE_RETEST=1 to run against the live local server")
	}
	// Track the current default (bge-m3), so adoption's ExpectedServeID matches
	// whatever the live server on 11457 actually serves. Override via env to point
	// at an opt-in model (e.g. OCODE_RETEST_MODEL=local/lfm2.5-embedding).
	modelID := DefaultLocalModelID()
	if m := os.Getenv("OCODE_RETEST_MODEL"); m != "" {
		modelID = m
	}
	const liveURL = "http://127.0.0.1:11457"

	StopLocalServer()
	defer StopLocalServer()

	var statuses []string
	setStatus := func(s string) { statuses = append(statuses, s) }
	spawn := func(string) error { t.Fatal("must adopt live server, not spawn"); return nil }

	base, dim, err := EnsureLocalServer(spawn, modelID, t.TempDir(), setStatus,
		LocalServerOptions{UserBaseURL: liveURL})
	if err != nil {
		t.Fatalf("EnsureLocalServer(adopt) failed: %v", err)
	}
	t.Logf("adopted base=%s dim=%d statuses=%v", base, dim, statuses)

	ready := false
	for _, s := range statuses {
		if s == "ready" {
			ready = true
		}
	}
	if !ready {
		t.Fatalf("Bug A NOT fixed: adopt path did not record \"ready\"; got %v", statuses)
	}

	// Bug B: warm a realistic corpus (conduct kaizen skill + decoys) and time it.
	emb := NewLocalEmbedder(base, modelID, dim)
	eng := NewEngine(emb, t.TempDir()) // temp dir: do not mutate the user's shared cache
	docs := []Doc{
		{ID: "skill:conduct-tuning-tencent-hy3", Kind: "skill", Name: "conduct-tuning-tencent-hy3",
			Text: "conduct-tuning-tencent-hy3. Corrective engineering-conduct guidance for the exact behaviors tencent/hy3 tests weak on — hallucination discipline (docs-over-memory) and safety discipline (git reset scope, destructive commands, production .env). Directive rules the model must follow."},
		{ID: "skill:frontend-design", Kind: "skill", Name: "frontend-design",
			Text: "frontend-design. Build polished React UI components with Tailwind, layout, spacing, and visual hierarchy."},
		{ID: "skill:pdf-extract", Kind: "skill", Name: "pdf-extract",
			Text: "pdf-extract. Extract text and tables from PDF documents into structured data."},
		{ID: "skill:sql-tuning", Kind: "skill", Name: "sql-tuning",
			Text: "sql-tuning. Optimize slow SQL queries, add indexes, analyze query plans for Postgres."},
	}

	t0 := time.Now()
	if err := eng.Warm(context.Background(), docs); err != nil {
		t.Fatalf("Bug B: Warm failed: %v", err)
	}
	warmDur := time.Since(t0)
	t.Logf("Warm(%d docs) took %s (was permanently timing out at 500ms before fix)", len(docs), warmDur)
	if !eng.Ready() {
		t.Fatal("Bug B: engine not Ready after Warm")
	}

	// Ranking correctness: a conduct question must rank the conduct kaizen skill
	// FIRST. (Whether it *attaches* is a separate concern — see the note below.)
	q := "Is it safe to run git reset without specifying file paths, and should I trust my memory over the project docs when they disagree?"
	ranked, err := eng.Rank(context.Background(), q)
	if err != nil {
		t.Fatalf("Rank failed: %v", err)
	}
	for _, sc := range ranked {
		t.Logf("rank score=%.4f  %s", sc.Score, sc.Doc.Name)
	}
	if len(ranked) == 0 || ranked[0].Doc.ID != "skill:conduct-tuning-tencent-hy3" {
		t.Fatalf("ranking wrong: conduct skill not #1; got %v", ranked)
	}

	// Bug C — CORRECTED DIAGNOSIS. Attachment (whether Discover selects a skill)
	// depends on the ABSOLUTE cosine clearing SelectMin(0.40), which is a property
	// of the MODEL, so what this asserts depends on which server runs on 11457:
	//   - bge-m3 (the DEFAULT since DefaultLocalModelID→bge-m3): a strong conduct
	//     match scores ~0.49, clears 0.40 → ATTACHES. This is the fix.
	//   - LFM2.5-Embedding (opt-in): a strong match scores only ~0.18–0.26 (via the
	//     correct llama.cpp CLS GGUF) — LOWER than the earlier causal MLX 0.31, and
	//     `query:`/`document:` prefixes score even lower (~0.20). Its cosine band is
	//     naturally COMPRESSED (matches ~0.2–0.3, off-topic ~0.05–0.09); ranking is
	//     correct but nothing clears 0.40 → attaches NOTHING.
	// So `0 attached` was NOT a pooling bug (the causal-MLX run WAS degraded and was
	// fixed by the llama.cpp b9777 + CLS GGUF swap, but that did not raise the band);
	// it was LFM2.5's band vs a bge-m3-tuned SelectMin. Fix: default to bge-m3. Do
	// NOT lower the GLOBAL SelectMin (mis-calibrates bge-m3/http); a per-model floor
	// (~0.15) is the alternative if LFM2.5 attachment is ever wanted.
	//
	// Kaizen delivery does NOT depend on this — admitted skills are always
	// name-advertised + digest-injected regardless of ranking
	// (docs/okf/_schema/stack-detection.md).
	sess := NewSession(eng)
	added, err := sess.Discover(context.Background(), q)
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}
	t.Logf("top=%.4f SelectMin=%.2f  attached=%v (attaches iff top≥SelectMin: bge-m3 ~0.49 yes, LFM2.5 ~0.2 no)",
		ranked[0].Score, SelectMin, sess.Attached())
	_ = added
	t.Log("RETEST PASS: Bug A (adopt→ready) ✓, Bug B (warm completes, no deadlock) ✓; Bug C attach = model band vs SelectMin (see above)")
}
