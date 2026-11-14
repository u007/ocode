package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/session"
	"github.com/u007/ocode/internal/tool"
)

// mcpReloadTestHandler builds a handler whose config directory is an empty temp
// dir, so NewHandler's boot-time warm sees no MCP servers and spawns nothing.
// The reload loader is faked per test; nothing here starts a real MCP process.
func mcpReloadTestHandler(t *testing.T) *Handler {
	t.Helper()
	setHomeTree(t, t.TempDir())
	dir := t.TempDir()
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	config.SetWorkDir(dir)
	h := NewHandler()
	h.workDir = dir
	return h
}

type mcpReloadPostBody struct {
	Status     string `json:"status"`
	Generation uint64 `json:"generation"`
}

func doMCPReloadPost(t *testing.T, h *Handler) (int, mcpReloadPostBody) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.HandleReloadMCP(rec, httptest.NewRequest(http.MethodPost, "/api/mcp/reload", nil))
	var body mcpReloadPostBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal POST body %s: %v", rec.Body.String(), err)
	}
	return rec.Code, body
}

func readMCPReload(t *testing.T, h *Handler) mcpReloadDTO {
	t.Helper()
	rec := httptest.NewRecorder()
	h.HandleGetMCPReload(rec, httptest.NewRequest(http.MethodGet, "/api/mcp/reload", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var dto mcpReloadDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
		t.Fatalf("unmarshal GET body %s: %v", rec.Body.String(), err)
	}
	return dto
}

func waitMCPReloadPhase(t *testing.T, h *Handler, want mcpReloadPhase) mcpReloadDTO {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		dto := readMCPReload(t, h)
		if dto.Status == string(want) {
			return dto
		}
		if time.Now().After(deadline) {
			t.Fatalf("reload did not reach %q; last %+v", want, dto)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestHandleReloadMCPAsyncAndIdempotent pins the agreed contract: POST answers
// 202 immediately, and a second POST while the run is in flight reports the
// SAME generation without starting another run (a 409 or a second enumeration
// would make every polling client retry-loop and spawn duplicate subprocesses).
func TestHandleReloadMCPAsyncAndIdempotent(t *testing.T) {
	h := mcpReloadTestHandler(t)

	release := make(chan struct{})
	started := make(chan struct{})
	var calls atomic.Int32
	h.mcpReloadFn = func() mcpReloadOutcome {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return mcpReloadOutcome{cfg: &config.Config{}, cache: newMCPCache()}
	}

	code, first := doMCPReloadPost(t, h)
	if code != http.StatusAccepted {
		t.Fatalf("POST status = %d, want 202", code)
	}
	if first.Status != string(mcpReloadRunning) {
		t.Fatalf("POST status field = %q, want %q", first.Status, mcpReloadRunning)
	}
	if first.Generation != 1 {
		t.Fatalf("POST generation = %d, want 1", first.Generation)
	}
	<-started

	code, second := doMCPReloadPost(t, h)
	if code != http.StatusAccepted {
		t.Fatalf("second POST status = %d, want 202", code)
	}
	if second.Generation != first.Generation {
		t.Fatalf("second POST generation = %d, want the in-flight %d", second.Generation, first.Generation)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("reload loader ran %d times, want 1", got)
	}

	close(release)
	done := waitMCPReloadPhase(t, h, mcpReloadDone)
	if done.Generation != first.Generation {
		t.Fatalf("finished generation = %d, want %d", done.Generation, first.Generation)
	}
}

// TestHandleGetMCPReloadStates pins idle → reloaded and the reported fields,
// including a parsed finished_at that is empty before any run.
func TestHandleGetMCPReloadStates(t *testing.T) {
	h := mcpReloadTestHandler(t)

	idle := readMCPReload(t, h)
	if idle.Status != string(mcpReloadIdle) || idle.Generation != 0 || idle.Tools != 0 {
		t.Fatalf("initial state = %+v, want idle generation 0 tools 0", idle)
	}
	if idle.FinishedAt != "" {
		t.Fatalf("idle finished_at = %q, want empty", idle.FinishedAt)
	}
	if idle.Errors == nil {
		t.Fatal("errors must marshal as [] so clients need no nil check")
	}

	h.mcpReloadFn = func() mcpReloadOutcome {
		return mcpReloadOutcome{
			cfg:   &config.Config{},
			cache: newMCPCache(),
			tools: make([]tool.Tool, 2),
			errs:  []string{"beta: connection refused"},
		}
	}
	if code, _ := doMCPReloadPost(t, h); code != http.StatusAccepted {
		t.Fatalf("POST status = %d, want 202", code)
	}

	done := waitMCPReloadPhase(t, h, mcpReloadDone)
	if done.Tools != 2 {
		t.Fatalf("tools = %d, want 2", done.Tools)
	}
	if len(done.Errors) != 1 || done.Errors[0] != "beta: connection refused" {
		t.Fatalf("errors = %v, want the per-server failure", done.Errors)
	}
	finished, err := time.Parse(time.RFC3339, done.FinishedAt)
	if err != nil {
		t.Fatalf("finished_at %q is not RFC3339: %v", done.FinishedAt, err)
	}
	if time.Since(finished) > time.Minute {
		t.Fatalf("finished_at = %v, want a recent timestamp", finished)
	}
}

// TestMCPReloadKeepsCacheWhenEnumerationFails proves the failed path applies
// the config but does NOT swap the cache, and says so in the status. Applying
// the config is deliberate: the file really changed, so the next session build
// should see it even though this enumeration produced no usable tools.
func TestMCPReloadKeepsCacheWhenEnumerationFails(t *testing.T) {
	h := mcpReloadTestHandler(t)

	oldCache := &mcpCache{ready: make(chan struct{}), tools: make([]tool.Tool, 3)}
	close(oldCache.ready)
	h.mu.Lock()
	h.mcpCache = oldCache
	h.mu.Unlock()

	newCfg := &config.Config{Model: "reloaded"}
	newCache := newMCPCache()
	h.mcpReloadFn = func() mcpReloadOutcome {
		return mcpReloadOutcome{cfg: newCfg, cache: newCache, timedOut: true}
	}

	if code, _ := doMCPReloadPost(t, h); code != http.StatusAccepted {
		t.Fatalf("POST status = %d, want 202", code)
	}
	failed := waitMCPReloadPhase(t, h, mcpReloadFailed)
	if len(failed.Errors) == 0 {
		t.Fatal("failed reload must report why")
	}
	h.mu.Lock()
	gotCache := h.mcpCache
	gotCfg := h.cfg
	h.mu.Unlock()
	if gotCache != oldCache {
		t.Fatal("a timed-out enumeration must keep the previous cache")
	}
	if gotCfg != newCfg {
		t.Fatal("the on-disk config must be applied even when enumeration failed")
	}
}

// TestShouldSwapMCPCache pins the swap rule on its own, including the two cases
// that destroyed a working tool set before: a timeout, and an all-errors load
// over an already-populated cache.
func TestShouldSwapMCPCache(t *testing.T) {
	cases := []struct {
		name     string
		timedOut bool
		tools    int
		errs     int
		oldTools int
		wantSwap bool
	}{
		{name: "success swaps", timedOut: false, tools: 2, errs: 0, oldTools: 3, wantSwap: true},
		{name: "first load with tools", timedOut: false, tools: 1, errs: 0, oldTools: 0, wantSwap: true},
		{name: "timeout keeps", timedOut: true, tools: 0, errs: 0, oldTools: 3, wantSwap: false},
		{name: "timeout with partial tools keeps", timedOut: true, tools: 2, errs: 0, oldTools: 3, wantSwap: false},
		{name: "all errors over live tools keeps", timedOut: false, tools: 0, errs: 2, oldTools: 3, wantSwap: false},
		{name: "all errors with nothing live swaps", timedOut: false, tools: 0, errs: 2, oldTools: 0, wantSwap: true},
		{name: "empty config swaps (removing servers)", timedOut: false, tools: 0, errs: 0, oldTools: 3, wantSwap: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldSwapMCPCache(tc.timedOut, tc.tools, tc.errs, tc.oldTools); got != tc.wantSwap {
				t.Fatalf("shouldSwapMCPCache = %v, want %v", got, tc.wantSwap)
			}
		})
	}
}

// TestReconcileProfileAgentRebuildsAfterMCPCacheSwap covers both sides of the
// deterministic reconcile: a session keeps the cache pointer it was built with,
// so a reload leaves it alone until the pointer actually differs — then the next
// turn rebuilds and stamps the new one.
func TestReconcileProfileAgentRebuildsAfterMCPCacheSwap(t *testing.T) {
	h := mcpReloadTestHandler(t)
	root := t.TempDir()
	id := session.NewSessionID()
	h.sessions.Register(id, root)

	cacheA := &mcpCache{ready: make(chan struct{}), tools: []tool.Tool{}}
	close(cacheA.ready)
	h.mu.Lock()
	h.mcpCache = cacheA
	h.mu.Unlock()

	as, stage, err := h.buildAgentSession(id, "opencode-go/deepseek-v4-flash", nil, root)
	if err != nil {
		t.Fatalf("build agent: %v (stage %s)", err, stage)
	}
	defer as.agent.Shutdown()
	h.replaceAgentSession(id, as)
	if as.mcpCache != cacheA {
		t.Fatal("buildAgentSession must stamp the cache it enumerated from")
	}

	same, err := h.reconcileProfileAgent(id, as, as.model)
	if err != nil {
		t.Fatalf("reconcile (no reload): %v", err)
	}
	if same != as {
		t.Fatal("reconcile rebuilt although the cache pointer did not change")
	}

	cacheB := &mcpCache{ready: make(chan struct{}), tools: []tool.Tool{}}
	close(cacheB.ready)
	h.mu.Lock()
	h.mcpCache = cacheB
	h.mu.Unlock()

	reb, err := h.reconcileProfileAgent(id, h.lookupAgentSession(id), as.model)
	if err != nil {
		t.Fatalf("reconcile (after reload): %v", err)
	}
	if reb == as {
		t.Fatal("expected a rebuild after the process-wide cache was swapped")
	}
	defer reb.agent.Shutdown()
	if reb.mcpCache != cacheB {
		t.Fatal("the rebuilt session must carry the new cache pointer")
	}
}

// TestReconcileProfileAgentIgnoresUnrecordedMCPCache is the same "unset snapshot
// means unknown" rule the suggestions flag follows: a hand-assembled session has
// no cache pointer, and rebuilding it on every turn would be a 500 on a chat
// that has nothing to do with MCP.
func TestReconcileProfileAgentIgnoresUnrecordedMCPCache(t *testing.T) {
	h := mcpReloadTestHandler(t)
	root := t.TempDir()
	id := session.NewSessionID()
	h.sessions.Register(id, root)
	ready := make(chan struct{})
	close(ready)
	h.mu.Lock()
	h.mcpCache = &mcpCache{ready: ready, tools: []tool.Tool{}}
	h.mu.Unlock()

	// Build through the real path so every other built-with field matches the
	// current process state; only the MCP pointer is cleared. A hand-assembled
	// session fails in the full suite because profile/credential state differs.
	as, stage, err := h.buildAgentSession(id, "opencode-go/deepseek-v4-flash", nil, root)
	if err != nil {
		t.Fatalf("build base agent: %v (stage %s)", err, stage)
	}
	defer as.agent.Shutdown()
	h.replaceAgentSession(id, as)
	as.mcpCache = nil
	got, err := h.reconcileProfileAgent(id, as, as.model)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if got != as {
		t.Fatal("a session with no recorded cache pointer must not be rebuilt")
	}
}

// TestMCPReloadPublishesFailureOnPanic pins the wedge guard: a panic inside the
// background run must still publish a verdict, because leaving the phase at
// "reloading" makes every later POST report that same in-flight generation and
// the reload slot never frees up.
func TestMCPReloadPublishesFailureOnPanic(t *testing.T) {
	h := mcpReloadTestHandler(t)
	h.mcpReloadFn = func() mcpReloadOutcome { panic("boom") }

	if code, _ := doMCPReloadPost(t, h); code != http.StatusAccepted {
		t.Fatalf("POST status = %d, want 202", code)
	}
	failed := waitMCPReloadPhase(t, h, mcpReloadFailed)
	if len(failed.Errors) == 0 {
		t.Fatal("a panicking reload must report a failure reason")
	}

	// The slot must be free again: a new POST starts a new generation.
	var calls atomic.Int32
	h.mcpReloadFn = func() mcpReloadOutcome {
		calls.Add(1)
		return mcpReloadOutcome{cfg: &config.Config{}, cache: newMCPCache()}
	}
	code, body := doMCPReloadPost(t, h)
	if code != http.StatusAccepted || body.Generation != failed.Generation+1 {
		t.Fatalf("POST after a failed run = %d generation %d, want 202 generation %d", code, body.Generation, failed.Generation+1)
	}
	waitMCPReloadPhase(t, h, mcpReloadDone)
	if calls.Load() != 1 {
		t.Fatalf("reload loader ran %d times after recovery, want 1", calls.Load())
	}
}
