package server

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/config"
)

// raceFixture is the goroutine-safe sibling of activationFixture. Every recorded
// judge and send call is guarded by mu, so the concurrent tests can run under
// -race. Tests that stub the judge with blocking or re-entrant behaviour rely on
// the judge being invoked without mu held.
type raceFixture struct {
	h *Handler

	mu       sync.Mutex
	judgeFn  func(cond string, ev []agent.PulseActivationEvent) agent.PulseActivationVerdict
	judgeLog [][]agent.PulseActivationEvent
	sendLog  [][]pulseStopIndicator
}

// pulseRaceChatID is the assistant chat the fixture mints in state.json.
const pulseRaceChatID = "pulse_activation_test"

func newRaceFixture(t *testing.T) *raceFixture {
	t.Helper()
	setHomeTree(t, t.TempDir())
	h := pulseTestHandler(t)
	h.mu.Lock()
	h.cfg = &config.Config{}
	h.cfg.Ocode.PulseAutoReceive = true
	h.cfg.Ocode.PulseAutoGapMs = 1
	h.mu.Unlock()

	root, err := pulseAssistantRoot()
	if err != nil {
		t.Fatal(err)
	}
	if err := writePulseState(root, pulseRaceChatID); err != nil {
		t.Fatal(err)
	}

	f := &raceFixture{h: h, judgeFn: skipVerdictStub}
	prevJudge, prevSend := pulseActivationJudge, pulseActivationSend
	t.Cleanup(func() { pulseActivationJudge, pulseActivationSend = prevJudge, prevSend })
	pulseActivationJudge = func(_ context.Context, _ *config.Config, cond string, ev []agent.PulseActivationEvent) agent.PulseActivationVerdict {
		cp := append([]agent.PulseActivationEvent(nil), ev...)
		f.mu.Lock()
		fn := f.judgeFn
		f.judgeLog = append(f.judgeLog, cp)
		f.mu.Unlock()
		// Called without mu held: a stub may block, or re-enter the gate's tools.
		return fn(cond, cp)
	}
	pulseActivationSend = func(_ *Handler, _ string, batch []pulseStopIndicator, _ string) error {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.sendLog = append(f.sendLog, append([]pulseStopIndicator(nil), batch...))
		return nil
	}
	return f
}

func skipVerdictStub(string, []agent.PulseActivationEvent) agent.PulseActivationVerdict {
	return agent.PulseActivationVerdict{Choice: "skip", Detail: "stub: skip"}
}

func wakeVerdictStub(string, []agent.PulseActivationEvent) agent.PulseActivationVerdict {
	return agent.PulseActivationVerdict{Activate: true, Choice: "wake", Detail: "stub: wake"}
}

func (f *raceFixture) setJudge(fn func(string, []agent.PulseActivationEvent) agent.PulseActivationVerdict) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.judgeFn = fn
}

func (f *raceFixture) judged() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.judgeLog)
}

func (f *raceFixture) judgeCalls() [][]agent.PulseActivationEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([][]agent.PulseActivationEvent, len(f.judgeLog))
	copy(out, f.judgeLog)
	return out
}

func (f *raceFixture) sent() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sendLog)
}

func (f *raceFixture) stop(kind, id, summary string) {
	f.h.pulseMu.Lock()
	defer f.h.pulseMu.Unlock()
	f.h.recordPulseStopIndicator(id, kind, summary, "proj")
}

func (f *raceFixture) setCondition(t *testing.T, text string) {
	t.Helper()
	if err := writePulseActivationPrompt(text); err != nil {
		t.Fatal(err)
	}
}

func (f *raceFixture) setGapMs(ms int) {
	f.h.mu.Lock()
	defer f.h.mu.Unlock()
	f.h.cfg.Ocode.PulseAutoGapMs = ms
}

// drain ticks at a time past every quiet gap until a pass judges nothing.
func (f *raceFixture) drain() {
	for {
		before := f.judged()
		f.h.pulseActivationTick(later())
		if f.judged() == before {
			return
		}
	}
}

func (f *raceFixture) recent(t *testing.T) []pulseActivationDecision {
	t.Helper()
	return f.h.pulseAct.recent()
}

func (f *raceFixture) lastOutcome(t *testing.T) string {
	t.Helper()
	dec := f.recent(t)
	if len(dec) == 0 {
		t.Fatalf("no gate decisions recorded")
	}
	return dec[len(dec)-1].Outcome
}

// parkHeldWake judges one indicator as wake while the assistant is mid-turn, so
// the gate parks the wake. On return the assistant is still marked busy and the
// last decision is "deferred".
func (f *raceFixture) parkHeldWake(t *testing.T, at time.Time) {
	t.Helper()
	root, err := pulseAssistantRoot()
	if err != nil {
		t.Fatal(err)
	}
	f.setJudge(func(string, []agent.PulseActivationEvent) agent.PulseActivationVerdict {
		f.h.sessions.Register(pulseRaceChatID, root)
		f.h.sessions.setTurnActive(pulseRaceChatID, true)
		return wakeVerdictStub("", nil)
	})
	f.stop("permission_ask", "ses_held", "rm -rf build")
	f.h.pulseActivationTick(at)
	if n := f.sent(); n != 0 {
		t.Fatalf("send called %d times while the assistant was busy", n)
	}
	if got := f.lastOutcome(t); got != "deferred" {
		t.Fatalf("outcome while busy = %q, want deferred", got)
	}
	f.h.pulseAct.mu.Lock()
	held := f.h.pulseAct.held != nil
	f.h.pulseAct.mu.Unlock()
	if !held {
		t.Fatalf("deferred wake was not parked")
	}
}

// waitOrFail joins wg, failing the test instead of hanging when it does not
// finish in time.
func waitOrFail(t *testing.T, wg *sync.WaitGroup, d time.Duration, what string) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("%s did not finish within %s (deadlock?)", what, d)
	}
}

func raceStopID(i int) string { return fmt.Sprintf("ses_%03d", i) }

func TestPulseActivationRace_ConcurrentBurstJudgesEachIndicatorOnce(t *testing.T) {
	f := newRaceFixture(t)
	f.setCondition(t, "wake me for anything")

	const total, recorders, tickers = 100, 3, 4
	var recWG sync.WaitGroup
	for r := 0; r < recorders; r++ {
		recWG.Add(1)
		go func(r int) {
			defer recWG.Done()
			for i := r; i < total; i += recorders {
				f.stop("turn_done", raceStopID(i), "turn finished")
			}
		}(r)
	}
	recDone := make(chan struct{})
	go func() {
		recWG.Wait()
		close(recDone)
	}()

	var tickWG sync.WaitGroup
	for k := 0; k < tickers; k++ {
		tickWG.Add(1)
		go func() {
			defer tickWG.Done()
			for {
				select {
				case <-recDone:
					return
				default:
				}
				f.h.pulseActivationTick(time.Now())
			}
		}()
	}
	waitOrFail(t, &tickWG, 10*time.Second, "tick goroutines")
	waitOrFail(t, &recWG, 10*time.Second, "recorder goroutines")

	f.drain()

	seen := map[string]int{}
	events := 0
	for _, call := range f.judgeCalls() {
		if len(call) > agent.PulseActivationBatchMax {
			t.Errorf("judge call carried %d events, over the batch max %d", len(call), agent.PulseActivationBatchMax)
		}
		for _, ev := range call {
			seen[ev.ID]++
			events++
		}
	}
	if events != total || len(seen) != total {
		t.Errorf("judged %d events over %d distinct ids, want %d and %d", events, len(seen), total, total)
	}
	for i := 0; i < total; i++ {
		if n := seen[raceStopID(i)]; n != 1 {
			t.Errorf("indicator %s judged %d times, want exactly 1", raceStopID(i), n)
		}
	}
}

func TestPulseActivationRace_ChunksOldestFirstInBatchesOfMax(t *testing.T) {
	f := newRaceFixture(t)
	f.setCondition(t, "wake me for anything")
	const total = 120
	for i := 0; i < total; i++ {
		f.stop("turn_done", raceStopID(i), "turn finished")
	}
	f.drain()

	calls := f.judgeCalls()
	wantSizes := []int{50, 50, 20}
	if len(calls) != len(wantSizes) {
		t.Fatalf("judge calls = %d, want %d", len(calls), len(wantSizes))
	}
	next := 0
	for c, call := range calls {
		if len(call) != wantSizes[c] {
			t.Fatalf("chunk %d has %d events, want %d", c, len(call), wantSizes[c])
		}
		for _, ev := range call {
			if ev.ID != raceStopID(next) {
				t.Fatalf("chunk %d: got %s, want %s (oldest first, no gaps or repeats)", c, ev.ID, raceStopID(next))
			}
			next++
		}
	}
	if next != total {
		t.Fatalf("judged %d events, want %d", next, total)
	}
}

func TestPulseActivationRace_RepeatsCoalesceToOneEvent(t *testing.T) {
	f := newRaceFixture(t)
	f.setCondition(t, "wake me for anything")
	for i := 0; i < 5; i++ {
		f.stop("turn_done", "ses_same", "turn finished")
	}
	f.h.pulseActivationTick(later())

	calls := f.judgeCalls()
	if len(calls) != 1 {
		t.Fatalf("judge calls = %d, want 1", len(calls))
	}
	if len(calls[0]) != 1 {
		t.Fatalf("judge saw %d events, want 1 coalesced event", len(calls[0]))
	}
	if got, want := calls[0][0].Summary, "turn finished (x5)"; got != want {
		t.Fatalf("coalesced summary = %q, want %q", got, want)
	}
}

func TestPulseActivationRace_TrickleCannotStarveTheOldestIndicator(t *testing.T) {
	f := newRaceFixture(t)
	f.setCondition(t, "wake me for anything")
	f.setGapMs(60_000)

	f.stop("turn_done", "ses_old", "turn finished")
	f.h.pulseActivationTick(time.Now())
	if n := f.judged(); n != 0 {
		t.Fatalf("judged inside the quiet gap: %d calls", n)
	}

	// A newer indicator arrives, so the newest is still inside the gap. Only the
	// oldest has waited past pulseActivationMaxHold.
	f.stop("turn_done", "ses_new", "turn finished")
	f.h.pulseActivationTick(time.Now().Add(pulseActivationMaxHold + time.Second))
	calls := f.judgeCalls()
	if len(calls) != 1 {
		t.Fatalf("judge calls = %d, want 1 once the oldest exceeded the max hold", len(calls))
	}
	if len(calls[0]) != 2 || calls[0][0].ID != "ses_old" || calls[0][1].ID != "ses_new" {
		t.Fatalf("judged batch = %+v, want ses_old then ses_new", calls[0])
	}
}

func TestPulseActivationRace_TickIsNonReentrant(t *testing.T) {
	f := newRaceFixture(t)
	f.setCondition(t, "wake me for anything")

	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	f.setJudge(func(string, []agent.PulseActivationEvent) agent.PulseActivationVerdict {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release
		return skipVerdictStub("", nil)
	})

	f.stop("turn_done", "ses_a", "turn finished")
	aDone := make(chan struct{})
	go func() {
		defer close(aDone)
		f.h.pulseActivationTick(later())
	}()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("tick A never reached the judge")
	}

	bDone := make(chan struct{})
	go func() {
		defer close(bDone)
		f.h.pulseActivationTick(later())
	}()
	select {
	case <-bDone:
	case <-time.After(5 * time.Second):
		t.Fatal("tick B blocked behind tick A instead of returning")
	}
	if n := f.judged(); n != 1 {
		t.Fatalf("judge calls while A is in flight = %d, want 1", n)
	}

	close(release)
	select {
	case <-aDone:
	case <-time.After(10 * time.Second):
		t.Fatal("tick A did not finish after release (deadlock?)")
	}
	if n := f.judged(); n != 1 {
		t.Fatalf("judge calls after both ticks = %d, want exactly 1", n)
	}
}

func TestPulseActivationRace_HeldWakeDeliveredOnceTheAssistantIsFree(t *testing.T) {
	f := newRaceFixture(t)
	f.setCondition(t, "wake me for anything")

	t1 := later()
	f.parkHeldWake(t, t1)

	f.h.sessions.setTurnActive(pulseRaceChatID, false)
	f.setJudge(func(string, []agent.PulseActivationEvent) agent.PulseActivationVerdict {
		t.Errorf("judge called again for a held wake")
		return skipVerdictStub("", nil)
	})
	f.h.pulseActivationTick(later())

	if n := f.sent(); n != 1 {
		t.Fatalf("send calls = %d, want exactly 1", n)
	}
	if n := f.judged(); n != 1 {
		t.Fatalf("judge calls = %d, want 1 (the held wake is not re-judged)", n)
	}
	if got := f.lastOutcome(t); got != "woke" {
		t.Fatalf("last outcome = %q, want woke", got)
	}
}

func TestPulseActivationRace_VerdictStaleWhenConditionChangesWhileJudging(t *testing.T) {
	f := newRaceFixture(t)
	f.setCondition(t, "wake me for anything")

	f.setJudge(func(string, []agent.PulseActivationEvent) agent.PulseActivationVerdict {
		if _, err := runPulseTool(t, f.h, "activation_write", `{"prompt":"new"}`); err != nil {
			t.Errorf("activation_write during judge: %v", err)
		}
		return wakeVerdictStub("", nil)
	})
	f.stop("permission_ask", "ses_a", "rm -rf build")
	f.h.pulseActivationTick(later())

	if n := f.sent(); n != 0 {
		t.Fatalf("send called %d times for a verdict made under a replaced condition", n)
	}
	if got := f.lastOutcome(t); got != "stale" {
		t.Fatalf("last outcome = %q, want stale", got)
	}
}

// Suspected production gap: activation_write clears a held wake
// (pulse_activation.go:231) without recording a decision, so the drop is silent.
// The test asserts the spec's expected "stale" record.
func TestPulseActivationRace_HeldWakeDroppedWhenConditionIsRewritten(t *testing.T) {
	f := newRaceFixture(t)
	f.setCondition(t, "wake me for anything")

	f.parkHeldWake(t, later())

	if _, err := runPulseTool(t, f.h, "activation_write", `{"prompt":"rewritten condition"}`); err != nil {
		t.Fatalf("activation_write: %v", err)
	}
	f.h.sessions.setTurnActive(pulseRaceChatID, false)
	f.h.pulseActivationTick(later())

	if n := f.sent(); n != 0 {
		t.Fatalf("held wake sent after the condition was rewritten (%d sends)", n)
	}
	if n := f.judged(); n != 1 {
		t.Fatalf("judge calls = %d, want 1", n)
	}
	stale := false
	for _, d := range f.recent(t) {
		if d.Outcome == "stale" {
			stale = true
		}
	}
	if !stale {
		t.Fatalf("no stale decision recorded for the dropped held wake; decisions = %+v", f.recent(t))
	}
}

func TestPulseActivationRace_HeldWakeExpiresAfterTheTTL(t *testing.T) {
	f := newRaceFixture(t)
	f.setCondition(t, "wake me for anything")

	t1 := later()
	f.parkHeldWake(t, t1)

	f.h.sessions.setTurnActive(pulseRaceChatID, false)
	f.h.pulseActivationTick(t1.Add(pulseActivationHeldTTL + time.Second))

	if n := f.sent(); n != 0 {
		t.Fatalf("expired held wake was sent (%d sends)", n)
	}
	if got := f.lastOutcome(t); got != "stale" {
		t.Fatalf("last outcome = %q, want stale", got)
	}
}

func TestPulseActivationRace_ConcurrentConditionWritesAndTicks(t *testing.T) {
	f := newRaceFixture(t)
	f.setCondition(t, "start")
	writeTool := pulseToolByName(t, f.h, "activation_write")

	const iters = 50
	texts := []string{"condition alpha", "condition beta"}
	var wg sync.WaitGroup
	for w := 0; w < 2; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < iters; i++ {
				args, _ := json.Marshal(map[string]string{"prompt": texts[(i+w)%2]})
				_, _ = writeTool.Execute(json.RawMessage(args))
			}
		}(w)
	}
	for k := 0; k < 2; k++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iters; i++ {
				f.h.pulseActivationTick(time.Now())
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			f.stop("turn_done", raceStopID(i), "turn finished")
		}
	}()

	waitOrFail(t, &wg, 10*time.Second, "condition writes, ticks and recorders")
}
