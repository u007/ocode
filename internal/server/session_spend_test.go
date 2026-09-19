package server

import (
	"math"
	"testing"
	"time"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/session"
	"github.com/u007/ocode/internal/tool"
	"github.com/u007/ocode/internal/usage"
)

// The spend metadata key is a cross-surface contract: the TUI's
// sidebarTelemetry reads and writes "spend" (telemetryFromSessionMetadata /
// metadata()). Renaming it here would silently orphan every persisted
// session's history, so pin it.
func TestSpentUSDMetadataKeyMatchesTUI(t *testing.T) {
	if spentUSDMetadataKey != "spend" {
		t.Fatalf("spentUSDMetadataKey = %q, want %q (the TUI sidebarTelemetry key)", spentUSDMetadataKey, "spend")
	}
}

// TestAgentSessionSpendAccumulates pins the per-session spend accumulator:
// summing a Step's message Spend (never the whole transcript) and adding side
// usage must produce the session's own total.
func TestAgentSessionSpendAccumulates(t *testing.T) {
	as := &agentSession{}
	spend := func(v float64) *float64 { return &v }
	as.addSpendFromMessages([]agent.Message{
		{Spend: spend(0.25)},
		{Spend: nil},
		{Spend: spend(0.10)},
	})
	as.addSpendUSD(0.05) // side usage (advisor/compact)
	if got := as.spendUSD(); got < 0.3999 || got > 0.4001 {
		t.Fatalf("spendUSD = %v, want 0.40", got)
	}
}

// TestSeedSpendNeverLowers covers the history-restore rule: seeding from a
// stale persisted total must not move the gauge backwards, and a newer total
// must raise it.
func TestSeedSpendNeverLowers(t *testing.T) {
	as := &agentSession{}
	as.seedSpend(1.5)
	if got := as.spendUSD(); got != 1.5 {
		t.Fatalf("after seed 1.5: spendUSD = %v, want 1.5", got)
	}
	as.seedSpend(0.25) // stale metadata / older snapshot
	if got := as.spendUSD(); got != 1.5 {
		t.Fatalf("stale seed regressed the total: %v, want 1.5", got)
	}
	as.seedSpend(3) // newer authoritative total
	if got := as.spendUSD(); got != 3 {
		t.Fatalf("newer seed did not raise the total: %v, want 3", got)
	}
	as.seedSpend(0) // absent/zero metadata is a no-op
	if got := as.spendUSD(); got != 3 {
		t.Fatalf("zero seed changed the total: %v, want 3", got)
	}
}

// TestSessionSpendFromMetadataNumericShapes covers every shape a metadata
// round-trip can produce (encoding/json → float64; typed writers → int/int64/
// float32) plus absent/foreign values, which must read as 0 rather than panic.
func TestSessionSpendFromMetadataNumericShapes(t *testing.T) {
	cases := []struct {
		name string
		md   map[string]any
		want float64
	}{
		{"json float64", map[string]any{"spend": 1.25}, 1.25},
		{"float32", map[string]any{"spend": float32(0.5)}, 0.5},
		{"int", map[string]any{"spend": 2}, 2},
		{"int64", map[string]any{"spend": int64(3)}, 3},
		{"missing key", map[string]any{"other": 1.0}, 0},
		{"nil map", nil, 0},
		{"wrong type", map[string]any{"spend": "1.25"}, 0},
	}
	for _, tc := range cases {
		if got := sessionSpendFromMetadata(tc.md); got != tc.want {
			t.Errorf("%s: sessionSpendFromMetadata = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestApplySessionSpendingLiveAgent covers the live path: the snapshot carries
// the session's accumulated spend (never the process-wide daily total).
func TestApplySessionSpendingLiveAgent(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	id := session.NewSessionID()
	saveSessionToDir(t, proj, id)
	h.agents[id] = &agentSession{}
	h.agents[id].addSpendUSD(1.2345)

	var snap TUIStatus
	h.applySessionSpending(&snap, id)
	if snap.SpendingUSD < 1.2344 || snap.SpendingUSD > 1.2346 {
		t.Fatalf("SpendingUSD = %v, want 1.2345", snap.SpendingUSD)
	}
}

// TestApplySessionSpendingRestoredFromMetadata covers the restored/evicted
// path: with no live agent the persisted "spend" metadata is read back so a
// session that was created (or last used) in the TUI shows its real history in
// the web sidebar instead of starting at 0.
func TestApplySessionSpendingRestoredFromMetadata(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	h.SetWorkDir(proj)
	id := session.NewSessionID()
	saveSessionToDir(t, proj, id)
	if err := session.UpdateMetadataForDir(proj, id, func(md map[string]any) {
		md[spentUSDMetadataKey] = 7.5
	}); err != nil {
		t.Fatalf("seed metadata: %v", err)
	}

	var snap TUIStatus
	h.applySessionSpending(&snap, id)
	if snap.SpendingUSD != 7.5 {
		t.Fatalf("SpendingUSD = %v, want 7.5 (persisted)", snap.SpendingUSD)
	}
}

// TestApplySessionSpendingLedgerFallback covers the last-resort path: with no
// live agent AND no persisted metadata total, the spend is summed back from the
// usage ledger's rows attributed to this session, so history lost from
// metadata is still recoverable.
func TestApplySessionSpendingLedgerFallback(t *testing.T) {
	// Isolate the usage ledger into a temp dir (paths.UsageDir honours HOME).
	t.Setenv("HOME", t.TempDir())

	h := NewHandler()
	proj := t.TempDir()
	h.SetWorkDir(proj)
	id := session.NewSessionID()
	saveSessionToDir(t, proj, id)

	if err := usage.RecordUsageForSession(time.Now(), id, "m", "p", 10, 5, 0, 15, 0.4); err != nil {
		t.Fatalf("seed ledger: %v", err)
	}
	if err := usage.RecordUsageForSession(time.Now(), id, "m", "p", 10, 5, 0, 15, 0.2); err != nil {
		t.Fatalf("seed ledger 2: %v", err)
	}
	// Another session's rows must not leak in.
	if err := usage.RecordUsageForSession(time.Now(), "ses_other", "m", "p", 1, 1, 0, 2, 99); err != nil {
		t.Fatalf("seed other: %v", err)
	}

	var snap TUIStatus
	h.applySessionSpending(&snap, id)
	if snap.SpendingUSD < 0.5999 || snap.SpendingUSD > 0.6001 {
		t.Fatalf("SpendingUSD = %v, want 0.6 (ledger fallback)", snap.SpendingUSD)
	}
}

// TestApplySessionSpendingPrefersLiveAndMetadata pins the precedence: the live
// agent total wins over both metadata and the ledger (no double counting).
func TestApplySessionSpendingPrefersLiveAndMetadata(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	h := NewHandler()
	proj := t.TempDir()
	h.SetWorkDir(proj)
	id := session.NewSessionID()
	saveSessionToDir(t, proj, id)
	if err := session.UpdateMetadataForDir(proj, id, func(md map[string]any) {
		md[spentUSDMetadataKey] = 3.0
	}); err != nil {
		t.Fatalf("seed metadata: %v", err)
	}
	if err := usage.RecordUsageForSession(time.Now(), id, "m", "p", 1, 1, 0, 2, 50.0); err != nil {
		t.Fatalf("seed ledger: %v", err)
	}

	h.agents[id] = &agentSession{}
	h.agents[id].addSpendUSD(1.5)

	var snap TUIStatus
	h.applySessionSpending(&snap, id)
	if snap.SpendingUSD != 1.5 {
		t.Fatalf("SpendingUSD = %v, want 1.5 (live agent wins)", snap.SpendingUSD)
	}
}

// TestPersistThenRestoreSessionSpendRoundTrip exercises the real persistence
// function against the key the TUI reads: turn-end persist → cold snapshot
// read (no live agent) must return the same total, and the raw metadata must
// carry it under "spend".
func TestPersistThenRestoreSessionSpendRoundTrip(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	h.SetWorkDir(proj)
	id := session.NewSessionID()
	saveSessionToDir(t, proj, id)

	h.persistSessionSpend(id, 4.5)

	var snap TUIStatus
	h.applySessionSpending(&snap, id)
	if snap.SpendingUSD != 4.5 {
		t.Fatalf("restored SpendingUSD = %v, want 4.5", snap.SpendingUSD)
	}
	s, err := session.LoadForDir(proj, id)
	if err != nil {
		t.Fatalf("reload session: %v", err)
	}
	if got := sessionSpendFromMetadata(s.Metadata); got != 4.5 {
		t.Fatalf("metadata %q = %v, want 4.5", spentUSDMetadataKey, got)
	}
}

// TestBuildAgentSessionSeedsSpendFromMetadata is the regression for "spend
// history disappears": a freshly built agent starts at 0, so without the seed
// the gauge showed only the current turn's spend after a resume/rebuild.
func TestBuildAgentSessionSeedsSpendFromMetadata(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	h.SetWorkDir(proj)
	id := session.NewSessionID()
	saveSessionToDir(t, proj, id)
	if err := session.UpdateMetadataForDir(proj, id, func(md map[string]any) {
		md[spentUSDMetadataKey] = 3.25
	}); err != nil {
		t.Fatalf("seed metadata: %v", err)
	}
	ready := make(chan struct{})
	close(ready)
	h.mcpCache = &mcpCache{ready: ready, tools: []tool.Tool{}, errs: nil}

	as, stage, err := h.buildAgentSession(id, "opencode-go/deepseek-v4-flash", nil, proj)
	if err != nil {
		t.Fatalf("buildAgentSession: %v (stage %s)", err, stage)
	}
	defer as.agent.Shutdown()
	if got := as.spendUSD(); math.Abs(got-3.25) > 1e-9 {
		t.Fatalf("spend after build = %v, want 3.25 (persisted history)", got)
	}
}

// TestReplaceAgentSessionCarriesLiveSpend covers the rebuild path (model
// switch, profile reconcile, plugin reload): the outgoing agent may hold turn
// spend not yet persisted, so the replacement must inherit it.
func TestReplaceAgentSessionCarriesLiveSpend(t *testing.T) {
	h := NewHandler()
	id := session.NewSessionID()
	old := &agentSession{}
	old.addSpendUSD(2.5)
	h.agents[id] = old

	next := &agentSession{}
	h.replaceAgentSession(id, next)
	if got := next.spendUSD(); math.Abs(got-2.5) > 1e-9 {
		t.Fatalf("replacement spend = %v, want the outgoing 2.5 (history must not reset)", got)
	}
}
