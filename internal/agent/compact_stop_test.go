package agent

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/config"
)

// A compaction pass that exhausts its retries leaves the transcript untouched,
// so the context stays over threshold and the very next step re-arms the same
// failing pass. These tests pin the stop-after-failure policy: a failed pass
// latches auto-compaction off, and only an explicit manual /compact re-arms it.

// failingSummaryClient makes every summary call fail — what a persistently
// broken compaction looks like from the pass's point of view.
type failingSummaryClient struct{}

func (failingSummaryClient) Chat([]Message, []map[string]interface{}) (*Message, error) {
	return nil, errors.New("provider refused the summary")
}
func (failingSummaryClient) GetProvider() string { return "mock" }
func (failingSummaryClient) GetModel() string    { return "mock-compact" }

type workingSummaryClient struct{}

func (workingSummaryClient) Chat([]Message, []map[string]interface{}) (*Message, error) {
	return &Message{Role: "assistant", Content: validSummaryText("recovered")}, nil
}
func (workingSummaryClient) GetProvider() string { return "mock" }
func (workingSummaryClient) GetModel() string    { return "mock-compact" }

func stopTestConfig() *config.Config {
	cfg := &config.Config{}
	cfg.Ocode.Compact.Enabled = true
	cfg.Ocode.Compact.KeepRecentTurns = 1
	cfg.Ocode.Compact.MinMessages = 1
	cfg.Ocode.Compact.MaxSummaryInputTokens = 100000
	cfg.Ocode.Compact.TokenThreshold = 0.0001
	cfg.Ocode.Compact.SummaryTimeoutSeconds = 2
	cfg.Ocode.Compact.SummaryFirstTokenTimeoutSeconds = 2
	cfg.Ocode.Compact.SummaryMaxRetries = 0
	return cfg
}

func stopTestMessages() []Message {
	msgs := []Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "original ask"},
	}
	for i := 0; i < 6; i++ {
		msgs = append(msgs, Message{Role: "assistant", Content: strings.Repeat("x", 2000)})
	}
	return append(msgs,
		Message{Role: "user", Content: "recent tail"},
		Message{Role: "assistant", Content: "tail response"},
	)
}

// The auto path is the loop: a failed pass must stop the next trigger from
// starting another one.
func TestAutoCompactionStopsAfterAFailedPass(t *testing.T) {
	a := &Agent{client: failingSummaryClient{}, config: stopTestConfig()}
	done := make(chan CompactResult, 4)
	a.OnCompact = func(r CompactResult) { done <- r }

	msgs := stopTestMessages()
	// Baseline first, so a later decline cannot be blamed on config: the same
	// agent DOES start a pass before anything has failed.
	if !a.MaybeCompactAsync(msgs) {
		t.Fatal("baseline: auto-compaction should start a pass")
	}
	select {
	case res := <-done:
		if res.Err == nil {
			t.Fatalf("expected the summary failure to fail the pass, got %+v", res)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("compaction pass never completed")
	}

	if !a.CompactFailed() {
		t.Fatal("a failed pass must latch auto-compaction off")
	}
	if a.MaybeCompactAsync(msgs) {
		t.Fatal("auto-compaction re-armed after a failed pass — this is the loop")
	}
}

// A pass that succeeded, or that had nothing to compact (OK=false, Err=nil),
// must leave auto-compaction armed. Otherwise a session that merely had no
// compactible middle would silently lose auto-compaction forever.
func TestSuccessfulAndNoOpPassesDoNotLatch(t *testing.T) {
	msgs := stopTestMessages()

	ok := &Agent{client: workingSummaryClient{}, config: stopTestConfig()}
	res, enabled := ok.Compact(msgs)
	if !enabled || !res.OK {
		t.Fatalf("expected a successful manual pass, enabled=%v res=%+v", enabled, res)
	}
	if ok.CompactFailed() {
		t.Fatal("a successful pass must not latch auto-compaction off")
	}

	// OK=false with no error is the "nothing to compact" short-circuit, not a
	// failure: it must not latch either. A manual pass is force=true, which
	// deliberately overrides the tail-fits short-circuit, so the only reachable
	// no-op is a transcript that is entirely prompt prefix — there is no middle
	// slice left to summarise.
	noop := &Agent{client: failingSummaryClient{}, config: stopTestConfig()}
	prefixOnly := []Message{{Role: "system", Content: "sys"}}
	res, enabled = noop.Compact(prefixOnly)
	if !enabled {
		t.Fatal("manual compaction should stay available")
	}
	if res.Err != nil {
		t.Fatalf("expected no error for a no-op pass, got %v", res.Err)
	}
	if noop.CompactFailed() {
		t.Fatal("a no-op pass (OK=false, Err=nil) must not latch auto-compaction off")
	}
}

// The escape hatch: a manual /compact always re-arms, so a user who fixed the
// cause (raised summary_timeout_seconds, changed summary_model) is never stuck
// with auto-compaction permanently disabled.
func TestManualCompactClearsTheFailedPassLatch(t *testing.T) {
	a := &Agent{client: failingSummaryClient{}, config: stopTestConfig()}
	msgs := stopTestMessages()

	res, enabled := a.Compact(msgs)
	if !enabled || res.Err == nil {
		t.Fatalf("expected the manual pass to fail, enabled=%v res=%+v", enabled, res)
	}
	if !a.CompactFailed() {
		t.Fatal("a failed manual pass must latch auto-compaction off too")
	}

	a.client = workingSummaryClient{}
	res, enabled = a.Compact(msgs)
	if !enabled || !res.OK {
		t.Fatalf("expected the manual retry to succeed, enabled=%v res=%+v", enabled, res)
	}
	if a.CompactFailed() {
		t.Fatal("a successful pass must clear the latch")
	}
	if !a.MaybeCompactAsync(msgs) {
		t.Fatal("auto-compaction must be armed again after a successful manual pass")
	}
}
