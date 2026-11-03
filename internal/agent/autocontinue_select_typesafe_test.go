package agent

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/u007/ocode/internal/config"
)

// selectJev is a fake /systemone that routes by question. The verdict question
// gets an "end" verdict and its state is recorded. Every noul question (one per
// message) is answered from nouls, defaulting to 0.9. missing ids get no answer
// and invalid ids get an out-of-range noul. failNoul makes every selection request
// a 500, and failOn does the same for a request whose batch holds that id.
// barrier, when set, holds each selection request until that many overlap
// (or 2s), so a serial implementation cannot pass a concurrency check.
type selectJev struct {
	t        *testing.T
	failNoul bool
	failOn   string
	barrier  int
	nouls    map[string]float64
	missing  map[string]bool
	invalid  map[string]bool

	mu           sync.Mutex
	verdictCalls int
	verdictState map[string]any
	nounCalls    int
	nounIDs      []string
	batchSizes   []int
	active       int
	maxActive    int
	gate         chan struct{}
	gateOnce     sync.Once
	// failFirstN fails the next N selection requests, to exercise the one-retry
	// path; it is decremented under mu.
	failFirstN int
}

func (j *selectJev) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		j.t.Errorf("decode request: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	questions, _ := body["questions"].(map[string]any)
	w.Header().Set("Content-Type", "application/json")
	if _, ok := questions[typesafeAutoContinueVerdictKey]; ok {
		j.mu.Lock()
		j.verdictCalls++
		j.verdictState, _ = body["state"].(map[string]any)
		j.mu.Unlock()
		_, _ = w.Write([]byte(autoContinueVerdictReply("end", 0.99, "finished")))
		return
	}

	j.mu.Lock()
	j.nounCalls++
	j.batchSizes = append(j.batchSizes, len(questions))
	fail := j.failNoul
	if j.failFirstN > 0 {
		j.failFirstN--
		fail = true
	}
	for id := range questions {
		j.nounIDs = append(j.nounIDs, id)
		if id == j.failOn {
			fail = true
		}
	}
	j.mu.Unlock()
	if fail {
		http.Error(w, "selection backend down", http.StatusInternalServerError)
		return
	}
	j.arrive()

	answers := make(map[string]any, len(questions))
	j.mu.Lock()
	for id := range questions {
		switch {
		case j.missing[id]:
		case j.invalid[id]:
			answers[id] = map[string]any{"type": "noul", "noul": 1.7}
		default:
			n, ok := j.nouls[id]
			if !ok {
				n = 0.9
			}
			answers[id] = map[string]any{"type": "noul", "noul": n}
		}
	}
	j.mu.Unlock()
	reply, err := json.Marshal(map[string]any{
		"model":   "jev-latest",
		"answers": answers,
		"usage":   map[string]any{"input_tokens": 1, "output_tokens": 1},
	})
	if err != nil {
		j.t.Errorf("marshal reply: %v", err)
	}
	_, _ = w.Write(reply)
}

// arrive records a selection request as in flight and, with barrier set, waits
// until barrier requests overlap.
func (j *selectJev) arrive() {
	if j.barrier == 0 {
		return
	}
	j.mu.Lock()
	j.active++
	if j.active > j.maxActive {
		j.maxActive = j.active
	}
	if j.active >= j.barrier {
		j.gateOnce.Do(func() { close(j.gate) })
	}
	j.mu.Unlock()
	select {
	case <-j.gate:
	case <-time.After(2 * time.Second):
	}
	j.mu.Lock()
	j.active--
	j.mu.Unlock()
}

// newSelectJev stands up the fake and an agent whose auto-continue judge and
// selection slot both resolve to it.
func newSelectJev(t *testing.T, barrier int) (*Agent, *selectJev) {
	t.Helper()
	j := &selectJev{
		t:       t,
		barrier: barrier,
		nouls:   map[string]float64{},
		missing: map[string]bool{},
		invalid: map[string]bool{},
		gate:    make(chan struct{}),
	}
	srv := httptest.NewServer(j)
	t.Cleanup(srv.Close)
	return newSelectAgentAt(t, srv.URL), j
}

// newSelectAgentAt returns an agent whose auto-continue verdict and selection
// both resolve to the decision backend at url.
func newSelectAgentAt(t *testing.T, url string) *Agent {
	t.Helper()
	cfg := &config.Config{}
	cfg.Ocode.AutoContinueModel = "typesafe/jev-latest"
	cfg.Ocode.AutoContinueEnabled = true
	a := newTestAgent(nil, nil, cfg, nil)
	prev := newClientFn
	t.Cleanup(func() { newClientFn = prev })
	newClientFn = func(_ *config.Config, _ string) LLMClient {
		return newTypesafeClient("k", "jev-latest", url)
	}
	return a
}

// chainTranscript builds n messages. Index 0 is the genuine user request, the last
// message is an assistant reply, and every other message is an assistant message
// named msg-<index>.
func chainTranscript(n int) []Message {
	msgs := make([]Message, n)
	for i := range msgs {
		msgs[i] = Message{Role: "assistant", Content: fmt.Sprintf("msg-%d", i)}
	}
	msgs[0] = Message{Role: "user", Content: "msg-0"}
	return msgs
}

// verdictTailContents returns the content of each entry the verdict call saw.
func verdictTailContents(t *testing.T, j *selectJev) []string {
	t.Helper()
	j.mu.Lock()
	defer j.mu.Unlock()
	tail, ok := j.verdictState["transcript_tail"].([]any)
	if !ok {
		t.Fatalf("transcript_tail = %T, want a list", j.verdictState["transcript_tail"])
	}
	out := make([]string, 0, len(tail))
	for _, e := range tail {
		out = append(out, e.(map[string]any)["content"].(string))
	}
	return out
}

func TestAutoContinueOriginalRequestIsLatestGenuineMessage(t *testing.T) {
	a, j := newSelectJev(t, 0)
	msgs := []Message{
		{Role: "user", Content: "Refactor the parser to use the new lexer"},
		{Role: "assistant", Content: "Step 1 done"},
		{Role: "user", Content: "Also add tests for the lexer edge cases"},
		{Role: "user", Content: "[ocode:selection] lexer.go"},
		{Role: "assistant", Content: "Tests added"},
		{Role: "user", Content: AutoContinueResumePrompt},
		{Role: "assistant", Content: "Continuing"},
		{Role: "user", Content: AutoContinueStepLimitPrompt},
		{Role: "assistant", Content: "Done"},
	}
	if _, _, err := judgeSync(a, msgs, nil); err != nil {
		t.Fatalf("judge: %v", err)
	}
	if got := j.verdictState["original_request"]; got != "Also add tests for the lexer edge cases" {
		t.Fatalf("original_request = %v, want the latest genuine request", got)
	}
}

func TestAutoContinueOriginalRequestIndexSkipsResumeAndMarkers(t *testing.T) {
	todoPrompt := strings.Replace(autoContinueActionPromptForTest(t, "continue_todo"), "{todo}", "t4", 1)
	cases := []struct {
		name string
		msgs []Message
		want int
	}{
		{"plain request", []Message{{Role: "user", Content: "fix it"}}, 0},
		{"resume prompt skipped", []Message{{Role: "user", Content: "fix it"}, {Role: "user", Content: AutoContinueResumePrompt}}, 0},
		{"todo prompt skipped", []Message{{Role: "user", Content: "fix it"}, {Role: "user", Content: todoPrompt}}, 0},
		{"ocode marker skipped", []Message{{Role: "user", Content: "fix it"}, {Role: "user", Content: "[ocode:goal] keep going"}}, 0},
		{"assistant never counts", []Message{{Role: "assistant", Content: "fix it"}}, -1},
		{"no genuine request", []Message{{Role: "user", Content: AutoContinueStepLimitPrompt}}, -1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := typesafeOriginalRequestIndex(tc.msgs); got != tc.want {
				t.Fatalf("typesafeOriginalRequestIndex = %d, want %d", got, tc.want)
			}
		})
	}
}

// autoContinueActionPromptForTest returns the fixed prompt of one action key.
func autoContinueActionPromptForTest(t *testing.T, key string) string {
	t.Helper()
	act, ok := autoContinueActionByKey(key)
	if !ok {
		t.Fatalf("no action %q", key)
	}
	return act.Prompt
}

func TestAutoContinueSelectSkippedAtOrUnderCap(t *testing.T) {
	a, j := newSelectJev(t, 0)
	// Blank messages do not count toward the cap: 20 real messages plus blanks is
	// still a transcript of 20.
	msgs := chainTranscript(autoContinueTranscriptCap)
	for i := 0; i < 5; i++ {
		msgs = append(msgs, Message{Role: "assistant", Content: "   "})
	}
	if _, _, err := judgeSync(a, msgs, nil); err != nil {
		t.Fatalf("judge: %v", err)
	}
	if j.nounCalls != 0 {
		t.Fatalf("selection calls = %d, want none at the cap", j.nounCalls)
	}
	if j.verdictCalls != 1 {
		t.Fatalf("verdict calls = %d, want 1", j.verdictCalls)
	}
	if got := len(verdictTailContents(t, j)); got != autoContinueTranscriptCap {
		t.Fatalf("verdict saw %d messages, want all %d", got, autoContinueTranscriptCap)
	}
}

func TestAutoContinueSelectRunsOverCap(t *testing.T) {
	a, j := newSelectJev(t, 0)
	if _, _, err := judgeSync(a, chainTranscript(autoContinueTranscriptCap+1), nil); err != nil {
		t.Fatalf("judge: %v", err)
	}
	if j.nounCalls == 0 {
		t.Fatal("selection made no call one message over the cap")
	}
}

func TestAutoContinueSelectBatchesMergeInOrder(t *testing.T) {
	a, j := newSelectJev(t, 0)
	// 45 messages: index 0 (the request) and 44 (the last reply) are pinned, so 43
	// are scored in batches of 20, 20 and 3. Odd indices score 0.1 (dropped).
	for i := 1; i <= 43; i++ {
		score := 0.1
		if i%2 == 0 {
			score = 0.9
		}
		j.nouls[selectMessageID(i)] = score
	}
	if _, _, err := judgeSync(a, chainTranscript(45), nil); err != nil {
		t.Fatalf("judge: %v", err)
	}
	if j.nounCalls != 3 {
		t.Fatalf("selection calls = %d, want 3 batches", j.nounCalls)
	}
	total := 0
	for _, n := range j.batchSizes {
		if n > autoContinueTranscriptCap {
			t.Fatalf("batch of %d questions exceeds the cap %d", n, autoContinueTranscriptCap)
		}
		total += n
	}
	if total != 43 {
		t.Fatalf("scored %d messages, want the 43 unpinned ones", total)
	}

	want := []string{"msg-0"}
	for i := 2; i <= 42; i += 2 {
		want = append(want, fmt.Sprintf("msg-%d", i))
	}
	want = append(want, "msg-44")
	got := verdictTailContents(t, j)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("verdict tail = %v\nwant %v", got, want)
	}
}

func TestAutoContinueSelectBatchesRunConcurrently(t *testing.T) {
	a, j := newSelectJev(t, 2)
	if _, _, err := judgeSync(a, chainTranscript(45), nil); err != nil {
		t.Fatalf("judge: %v", err)
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.maxActive < 2 {
		t.Fatalf("at most %d selection batch in flight; batches must run in parallel", j.maxActive)
	}
}

func TestAutoContinueSelectPinnedKeptWhenEverythingVetoed(t *testing.T) {
	a, j := newSelectJev(t, 0)
	for i := 1; i <= 43; i++ {
		j.nouls[selectMessageID(i)] = 0.1
	}
	if _, _, err := judgeSync(a, chainTranscript(45), nil); err != nil {
		t.Fatalf("judge: %v", err)
	}
	got := verdictTailContents(t, j)
	if strings.Join(got, ",") != "msg-0,msg-44" {
		t.Fatalf("verdict tail = %v, want only the original request and the last reply", got)
	}
	if v, _ := j.verdictState["original_request"].(string); v != "msg-0" {
		t.Fatalf("original_request = %q, want msg-0", v)
	}
}

func TestAutoContinueSelectInvalidOrMissingAnswerKeepsMessage(t *testing.T) {
	a, j := newSelectJev(t, 0)
	j.invalid[selectMessageID(5)] = true
	j.missing[selectMessageID(9)] = true
	j.nouls[selectMessageID(7)] = 0.1
	if _, _, err := judgeSync(a, chainTranscript(25), nil); err != nil {
		t.Fatalf("judge: %v", err)
	}
	got := strings.Join(verdictTailContents(t, j), ",")
	for _, keep := range []string{"msg-5", "msg-9"} {
		if !strings.Contains(","+got+",", ","+keep+",") {
			t.Fatalf("%s dropped; an untrustworthy or missing answer must keep it. tail=%s", keep, got)
		}
	}
	if strings.Contains(","+got+",", ",msg-7,") {
		t.Fatalf("msg-7 kept; its valid below-floor answer should drop it. tail=%s", got)
	}
}

func TestAutoContinueSelectErrorFailsClosed(t *testing.T) {
	a, j := newSelectJev(t, 0)
	j.failNoul = true
	dec, err := a.AutoContinueJudgeSync(chainTranscript(25), nil, nil)
	if err == nil {
		t.Fatal("selection failure was swallowed; the error must reach the caller")
	}
	if dec.Resume {
		t.Fatal("selection failure resumed the chain")
	}
	if !strings.Contains(dec.Detail, "selection failed") {
		t.Fatalf("detail = %q, want the selection failure", dec.Detail)
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.verdictCalls != 0 {
		t.Fatalf("verdict asked on an unselected transcript (%d calls); selection failure must not fall back", j.verdictCalls)
	}
}

func TestAutoContinueSelectStateFitsBudgetDroppingLowestScore(t *testing.T) {
	a, j := newSelectJev(t, 0)
	big := strings.Repeat("x", 3990)
	msgs := make([]Message, 30)
	msgs[0] = Message{Role: "user", Content: "msg-0"}
	for i := 1; i < 29; i++ {
		msgs[i] = Message{Role: "assistant", Content: fmt.Sprintf("msg-%d ", i) + big}
	}
	msgs[29] = Message{Role: "assistant", Content: "msg-29"}
	// Every scored message is 0.9 except msg-3, the lowest kept score: it goes first.
	j.nouls[selectMessageID(3)] = 0.51
	if _, _, err := judgeSync(a, msgs, nil); err != nil {
		t.Fatalf("judge: %v", err)
	}

	j.mu.Lock()
	state := j.verdictState
	j.mu.Unlock()
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) > decisionStateBudgetBytes {
		t.Fatalf("verdict state is %d bytes, over the %d budget", len(raw), decisionStateBudgetBytes)
	}
	if _, projected := state[stateProjectionKey]; projected {
		t.Fatal("state was projected; dropping by score should have fitted it without clipping")
	}
	for _, c := range verdictTailContents(t, j) {
		if strings.HasPrefix(c, "msg-3 ") {
			t.Fatal("msg-3, the lowest-scoring message, was kept while the budget was exceeded")
		}
	}
	if state["original_request"] != "msg-0" {
		t.Fatalf("original_request = %v, want the pinned request", state["original_request"])
	}
}

func TestFitTypesafeTranscriptNeverDropsPinnedOrUnscored(t *testing.T) {
	big := strings.Repeat("y", 4000)
	kept := []typesafeSelected{{msg: Message{Role: "user", Content: "orig"}, pinned: true, keep: true}}
	// Unscored (fail-open) and the lowest possible value: it must survive while
	// scored messages are dropped.
	kept = append(kept, typesafeSelected{msg: Message{Role: "assistant", Content: "unscored " + big}, keep: true})
	for i := 0; i < 40; i++ {
		kept = append(kept, typesafeSelected{
			msg:    Message{Role: "assistant", Content: fmt.Sprintf("s%d %s", i, big)},
			keep:   true,
			scored: true,
			score:  0.6,
		})
	}
	build := func(tail []Message) map[string]any {
		return map[string]any{"transcript_tail": typesafeTranscriptEntries(tail)}
	}
	tail, err := fitTypesafeTranscript(kept, build)
	if err != nil {
		t.Fatalf("fit: %v", err)
	}
	if len(tail) >= len(kept) {
		t.Fatalf("nothing dropped (%d of %d kept); the state should have exceeded the budget", len(tail), len(kept))
	}
	var sawOrig, sawUnscored bool
	for _, m := range tail {
		if m.Content == "orig" {
			sawOrig = true
		}
		if strings.HasPrefix(m.Content, "unscored ") {
			sawUnscored = true
		}
	}
	if !sawOrig || !sawUnscored {
		t.Fatalf("pinned kept=%v unscored kept=%v; both must survive", sawOrig, sawUnscored)
	}
}

// takeSelection returns how many selection requests and which message ids they
// carried since the last take, and resets both.
func takeSelection(j *selectJev) (int, []string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	calls, ids := j.nounCalls, j.nounIDs
	j.nounCalls, j.nounIDs = 0, nil
	sort.Strings(ids)
	return calls, ids
}

// selectIDs returns the selection ids for transcript indices lo through hi,
// sorted the way takeSelection sorts what it returns.
func selectIDs(lo, hi int) []string {
	ids := make([]string, 0, hi-lo+1)
	for i := lo; i <= hi; i++ {
		ids = append(ids, selectMessageID(i))
	}
	sort.Strings(ids)
	return ids
}

func TestAutoContinueSelectCacheSecondIdenticalCallScoresNothing(t *testing.T) {
	a, j := newSelectJev(t, 0)
	msgs := chainTranscript(25)
	if _, _, err := judgeSync(a, msgs, nil); err != nil {
		t.Fatalf("first judge: %v", err)
	}
	first := strings.Join(verdictTailContents(t, j), ",")
	takeSelection(j)

	if _, _, err := judgeSync(a, msgs, nil); err != nil {
		t.Fatalf("second judge: %v", err)
	}
	if calls, ids := takeSelection(j); calls != 0 || len(ids) != 0 {
		t.Fatalf("second identical call made %d selection calls scoring %v; want none", calls, ids)
	}
	if got := strings.Join(verdictTailContents(t, j), ","); got != first {
		t.Fatalf("verdict tail on a cache hit = %s, want %s", got, first)
	}
}

func TestAutoContinueSelectCacheScoresOnlyAppendedMessage(t *testing.T) {
	a, j := newSelectJev(t, 0)
	msgs := chainTranscript(25)
	if _, _, err := judgeSync(a, msgs, nil); err != nil {
		t.Fatalf("first judge: %v", err)
	}
	takeSelection(j)

	// An [ocode:] notice is neither a genuine request nor an assistant reply, so
	// both pins stay where they were and the appended message is the only new one.
	msgs = append(msgs, Message{Role: "user", Content: "[ocode:event] lint passed"})
	if _, _, err := judgeSync(a, msgs, nil); err != nil {
		t.Fatalf("second judge: %v", err)
	}
	calls, ids := takeSelection(j)
	if calls != 1 || strings.Join(ids, ",") != strings.Join(selectIDs(25, 25), ",") {
		t.Fatalf("appended message made %d calls scoring %v; want one call scoring only m25", calls, ids)
	}
}

func TestAutoContinueSelectCacheNewOriginalRequestRescoresAll(t *testing.T) {
	a, j := newSelectJev(t, 0)
	msgs := chainTranscript(25)
	if _, _, err := judgeSync(a, msgs, nil); err != nil {
		t.Fatalf("first judge: %v", err)
	}
	takeSelection(j)

	// A new human request changes the scope. The old request at index 0 is now an
	// ordinary unpinned message, and nothing scored under the old request is reused.
	msgs = append(msgs, Message{Role: "user", Content: "now do the other thing"}, Message{Role: "assistant", Content: "msg-26"})
	if _, _, err := judgeSync(a, msgs, nil); err != nil {
		t.Fatalf("second judge: %v", err)
	}
	_, ids := takeSelection(j)
	if strings.Join(ids, ",") != strings.Join(selectIDs(0, 24), ",") {
		t.Fatalf("new original request scored %v; want every unpinned message m0..m24", ids)
	}
}

func TestAutoContinueSelectCacheHitsAfterIndexShift(t *testing.T) {
	a, j := newSelectJev(t, 0)
	msgs := chainTranscript(25)
	if _, _, err := judgeSync(a, msgs, nil); err != nil {
		t.Fatalf("first judge: %v", err)
	}
	takeSelection(j)

	// Compaction drops msg-1, shifting every later index down, and two middle
	// messages trade places. Content is unchanged, so every unpinned message hits.
	shifted := append([]Message{msgs[0]}, msgs[2:]...)
	shifted[3], shifted[4] = shifted[4], shifted[3]
	if _, _, err := judgeSync(a, shifted, nil); err != nil {
		t.Fatalf("shifted judge: %v", err)
	}
	if calls, ids := takeSelection(j); calls != 0 || len(ids) != 0 {
		t.Fatalf("index-shifted transcript made %d calls scoring %v; want none", calls, ids)
	}
	if got := len(verdictTailContents(t, j)); got != len(shifted) {
		t.Fatalf("verdict saw %d messages, want all %d kept from cache", got, len(shifted))
	}
}

func TestAutoContinueSelectCacheFailedBatchCachesNothing(t *testing.T) {
	a, j := newSelectJev(t, 0)
	// Unpinned 1..43 form batches of 20, 20 and 3. Failing the first batch leaves
	// two successful batches whose outcomes must not be cached either.
	msgs := chainTranscript(45)
	j.failOn = selectMessageID(1)
	if _, _, err := judgeSync(a, msgs, nil); err == nil {
		t.Fatal("a failed selection batch was swallowed")
	}
	takeSelection(j)

	j.failOn = ""
	if _, _, err := judgeSync(a, msgs, nil); err != nil {
		t.Fatalf("retry judge: %v", err)
	}
	calls, ids := takeSelection(j)
	if calls != 3 || strings.Join(ids, ",") != strings.Join(selectIDs(1, 43), ",") {
		t.Fatalf("retry after a failed selection made %d calls scoring %v; want 3 calls scoring all 43", calls, ids)
	}
}

func TestAutoContinueSelectCacheRescoresUnscoredMessage(t *testing.T) {
	a, j := newSelectJev(t, 0)
	// Fail-open: m5 gets no answer, so it is kept but never cached.
	j.missing[selectMessageID(5)] = true
	msgs := chainTranscript(25)
	if _, _, err := judgeSync(a, msgs, nil); err != nil {
		t.Fatalf("first judge: %v", err)
	}
	takeSelection(j)

	if _, _, err := judgeSync(a, msgs, nil); err != nil {
		t.Fatalf("second judge: %v", err)
	}
	calls, ids := takeSelection(j)
	if calls != 1 || strings.Join(ids, ",") != selectMessageID(5) {
		t.Fatalf("second call made %d calls scoring %v; want one call scoring only m5", calls, ids)
	}
}

func TestAutoContinueSelectCacheResetRescoresAll(t *testing.T) {
	a, j := newSelectJev(t, 0)
	msgs := chainTranscript(25)
	if _, _, err := judgeSync(a, msgs, nil); err != nil {
		t.Fatalf("first judge: %v", err)
	}
	takeSelection(j)

	a.ResetSelectScores()
	if _, _, err := judgeSync(a, msgs, nil); err != nil {
		t.Fatalf("judge after reset: %v", err)
	}
	_, ids := takeSelection(j)
	if strings.Join(ids, ",") != strings.Join(selectIDs(1, 23), ",") {
		t.Fatalf("after reset scored %v; want every unpinned message m1..m23", ids)
	}
}

// TestAutoContinueSelectStopCancelsInFlightScoring stops the agent through Cancel,
// the same entry point Escape uses, while a selection batch is in flight. The
// judge must return an error without a verdict, and nothing may be cached.
func TestAutoContinueSelectStopCancelsInFlightScoring(t *testing.T) {
	started := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case started <- struct{}{}:
		default:
		}
		// Drain the body first: until it is read, the server does not notice
		// the client going away, so r.Context() would never be cancelled.
		_, _ = io.Copy(io.Discard, r.Body)
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	a := newSelectAgentAt(t, srv.URL)

	type outcome struct {
		resume bool
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		dec, err := a.AutoContinueJudgeSync(chainTranscript(45), nil, nil)
		done <- outcome{resume: dec.Resume, err: err}
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("selection never reached the backend")
	}
	a.Cancel()

	select {
	case got := <-done:
		if got.err == nil {
			t.Fatal("a stopped selection was reported as success")
		}
		if got.resume {
			t.Fatal("a stopped selection resumed the chain")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("judge did not return after Stop")
	}
	a.selectScores.mu.Lock()
	cached := len(a.selectScores.entries)
	a.selectScores.mu.Unlock()
	if cached != 0 {
		t.Fatalf("stopped selection cached %d scores; want none", cached)
	}
}

// TestAutoContinueSelectCacheMissesWhenLastReplyChanges: the last reply is part
// of the question Jev answers, so a score given under one reply is not reused
// under another, even when every other message is unchanged.
func TestAutoContinueSelectCacheMissesWhenLastReplyChanges(t *testing.T) {
	a, j := newSelectJev(t, 0)
	msgs := chainTranscript(25)
	if _, _, err := judgeSync(a, msgs, nil); err != nil {
		t.Fatalf("first judge: %v", err)
	}
	takeSelection(j)

	msgs[24] = Message{Role: "assistant", Content: "msg-24 revised reply"}
	if _, _, err := judgeSync(a, msgs, nil); err != nil {
		t.Fatalf("judge under a changed last reply: %v", err)
	}
	_, ids := takeSelection(j)
	if strings.Join(ids, ",") != strings.Join(selectIDs(1, 23), ",") {
		t.Fatalf("changed last reply scored %v; want every unpinned message m1..m23 rescored", ids)
	}
}

// greedyFitForTest is the one-drop-at-a-time reference that fitTypesafeTranscript
// replaces: drop the lowest scored message, re-measure, repeat.
func greedyFitForTest(t *testing.T, kept []typesafeSelected, build func([]Message) map[string]any) []Message {
	t.Helper()
	for {
		tail := make([]Message, 0, len(kept))
		for _, s := range kept {
			tail = append(tail, s.msg)
		}
		size, err := marshalledDecisionStateSize(build(tail))
		if err != nil {
			t.Fatalf("measure: %v", err)
		}
		if size <= decisionStateBudgetBytes {
			return tail
		}
		drop := -1
		for i, s := range kept {
			if s.pinned || !s.scored {
				continue
			}
			if drop < 0 || s.score < kept[drop].score {
				drop = i
			}
		}
		if drop < 0 {
			return tail
		}
		kept = append(kept[:drop:drop], kept[drop+1:]...)
	}
}

// TestFitTypesafeTranscriptMatchesGreedyDropWithTies checks the binary search drops
// exactly what the one-at-a-time drop dropped, with many tied scores, and that it
// measures the state a logarithmic number of times, not once per dropped message.
func TestFitTypesafeTranscriptMatchesGreedyDropWithTies(t *testing.T) {
	big := strings.Repeat("z", 3000)
	kept := []typesafeSelected{{msg: Message{Role: "user", Content: "orig"}, pinned: true, keep: true}}
	for i := 0; i < 80; i++ {
		score := 0.6
		if i%3 == 0 {
			score = 0.7
		}
		kept = append(kept, typesafeSelected{
			msg:    Message{Role: "assistant", Content: fmt.Sprintf("s%d %s", i, big)},
			keep:   true,
			scored: true,
			score:  score,
		})
	}
	build := func(tail []Message) map[string]any {
		return map[string]any{"transcript_tail": typesafeTranscriptEntries(tail)}
	}
	want := greedyFitForTest(t, kept, build)

	calls := 0
	got, err := fitTypesafeTranscript(kept, func(tail []Message) map[string]any {
		calls++
		return build(tail)
	})
	if err != nil {
		t.Fatalf("fit: %v", err)
	}
	if len(got) >= len(kept) {
		t.Fatalf("nothing dropped (%d of %d kept); the fixture must exceed the budget", len(got), len(kept))
	}
	gotContent := make([]string, 0, len(got))
	for _, m := range got {
		gotContent = append(gotContent, m.Content)
	}
	wantContent := make([]string, 0, len(want))
	for _, m := range want {
		wantContent = append(wantContent, m.Content)
	}
	if strings.Join(gotContent, "\n") != strings.Join(wantContent, "\n") {
		t.Fatalf("binary-search fit kept %d messages, greedy kept %d; the kept sets differ", len(got), len(want))
	}
	if calls > 10 {
		t.Fatalf("fit measured the state %d times for 80 droppable messages; want a logarithmic count", calls)
	}
}

// TestIsAutoContinueResumeTextRequiresTodoID: a {todo} prompt is recognised only
// when the placeholder holds a todo item id. Prose that shares the fixed text, or
// content shorter than the fixed text on both sides, is a genuine user message.
func TestIsAutoContinueResumeTextRequiresTodoID(t *testing.T) {
	const template = "Skip todo item {todo}, record it in TODO.md with the reason, and continue with the rest of the plan."
	head, tail, _ := strings.Cut(template, "{todo}")
	cases := []struct {
		name    string
		content string
		want    bool
	}{
		{"rendered with an item id", strings.ReplaceAll(template, "{todo}", "t3"), true},
		{"rendered with a prose target", strings.ReplaceAll(template, "{todo}", "the flaky login test"), false},
		{"id-shaped but not an id", strings.ReplaceAll(template, "{todo}", "t3x"), false},
		{"placeholder empty", head + tail, false},
		{"head alone", head, false},
		{"tail alone", tail, false},
		{"empty", "", false},
		{"fixed resume prompt", AutoContinueResumePrompt, true},
		{"step-limit prompt", AutoContinueStepLimitPrompt, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isAutoContinueResumeText(c.content); got != c.want {
				t.Fatalf("isAutoContinueResumeText(%q) = %v, want %v", c.content, got, c.want)
			}
		})
	}
}

// selectionSignal returns the verdict state's transcript_selection object, or nil
// when the state carried none.
func selectionSignal(t *testing.T, j *selectJev) map[string]any {
	t.Helper()
	j.mu.Lock()
	defer j.mu.Unlock()
	raw, ok := j.verdictState["transcript_selection"]
	if !ok {
		return nil
	}
	sig, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("transcript_selection = %T, want an object", raw)
	}
	return sig
}

// intField reads a numeric field of the JSON-decoded signal.
func intField(t *testing.T, sig map[string]any, key string) int {
	t.Helper()
	f, ok := sig[key].(float64)
	if !ok {
		t.Fatalf("transcript_selection[%q] = %T, want a number", key, sig[key])
	}
	return int(f)
}

// A state with nothing dropped keeps its previous shape: no transcript_selection
// field. The marker must mean something, so it is absent when it would be zero.
func TestAutoContinueSelectNoSignalWhenNothingDropped(t *testing.T) {
	a, j := newSelectJev(t, 0)
	if _, _, err := judgeSync(a, chainTranscript(25), nil); err != nil {
		t.Fatalf("judge: %v", err)
	}
	if sig := selectionSignal(t, j); sig != nil {
		t.Fatalf("transcript_selection = %v, want none when nothing was dropped", sig)
	}
}

// One transient selection-batch failure is retried once, so a backend hiccup does
// not switch auto-continue off for the turn.
func TestAutoContinueSelectRetriesOneTransientBatchFailure(t *testing.T) {
	a, j := newSelectJev(t, 0)
	j.failFirstN = 1
	if _, _, err := judgeSync(a, chainTranscript(25), nil); err != nil {
		t.Fatalf("judge after a transient batch failure: %v", err)
	}
	j.mu.Lock()
	calls := j.nounCalls
	j.mu.Unlock()
	// 23 unpinned messages = 2 batches, plus the one retry.
	if calls != 3 {
		t.Fatalf("selection calls = %d, want 3 (2 batches + 1 retry)", calls)
	}
	if got := len(verdictTailContents(t, j)); got != 25 {
		t.Fatalf("verdict saw %d messages, want all 25 after the retry", got)
	}
	if sig := selectionSignal(t, j); sig != nil {
		t.Fatalf("transcript_selection = %v, want none: the retry recovered every message", sig)
	}
}

// The selection cost per turn end is bounded: at most autoContinueSelectMaxBatches
// batches of autoContinueTranscriptCap messages. The oldest unpinned messages are
// left out and the omission is reported.
func TestAutoContinueSelectCapsTotalBatches(t *testing.T) {
	const n = 200
	a, j := newSelectJev(t, 0)
	if _, _, err := judgeSync(a, chainTranscript(n), nil); err != nil {
		t.Fatalf("judge: %v", err)
	}
	j.mu.Lock()
	calls := j.nounCalls
	sizes := append([]int(nil), j.batchSizes...)
	j.mu.Unlock()
	if calls != autoContinueSelectMaxBatches {
		t.Fatalf("selection calls = %d, want the %d-batch ceiling", calls, autoContinueSelectMaxBatches)
	}
	for _, size := range sizes {
		if size != autoContinueTranscriptCap {
			t.Fatalf("batch sizes = %v, want every batch full at %d", sizes, autoContinueTranscriptCap)
		}
	}
	// 198 unpinned messages, 160 scored -> the oldest 38 are left out.
	over := (n - 2) - autoContinueSelectMaxBatches*autoContinueTranscriptCap
	want := []string{"msg-0"}
	for i := 1 + over; i < n; i++ {
		want = append(want, fmt.Sprintf("msg-%d", i))
	}
	if got := verdictTailContents(t, j); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("verdict tail = %d messages (%s...), want %d (%s...)", len(got), got[0], len(want), want[0])
	}
	sig := selectionSignal(t, j)
	if sig == nil {
		t.Fatal("the batch ceiling dropped messages without signalling")
	}
	if got := intField(t, sig, "dropped"); got != over {
		t.Fatalf("transcript_selection.dropped = %d, want %d", got, over)
	}
	if got := intField(t, sig, "chain_messages"); got != n {
		t.Fatalf("transcript_selection.chain_messages = %d, want %d", got, n)
	}
}

// The budget fit drops messages to reach decisionStateBudgetBytes, and a dropped
// message must be visible to the verdict rather than silently missing.
func TestAutoContinueSelectSignalsBudgetFitDrops(t *testing.T) {
	const n = 22
	markup := strings.Repeat(`<div class="row">&nbsp;<span>x</span></div>`, 200)[:4000]
	a, j := newSelectJev(t, 0)
	msgs := make([]Message, n)
	for i := range msgs {
		msgs[i] = Message{Role: "assistant", Content: markup}
	}
	msgs[0] = Message{Role: "user", Content: markup}
	if _, _, err := judgeSync(a, msgs, nil); err != nil {
		t.Fatalf("judge: %v", err)
	}
	got := verdictTailContents(t, j)
	if len(got) >= n {
		t.Fatalf("the budget fit dropped nothing (%d messages); the fixture must exceed the budget", len(got))
	}
	sig := selectionSignal(t, j)
	if sig == nil {
		t.Fatal("the budget fit dropped messages without signalling")
	}
	if sent := intField(t, sig, "sent"); sent != len(got) {
		t.Fatalf("transcript_selection.sent = %d, want %d (the verdict's own tail)", sent, len(got))
	}
	if dropped := intField(t, sig, "dropped"); dropped != n-len(got) {
		t.Fatalf("transcript_selection.dropped = %d, want %d", dropped, n-len(got))
	}
}
