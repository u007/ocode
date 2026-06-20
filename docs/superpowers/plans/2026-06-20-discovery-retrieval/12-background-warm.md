# Part 12 — Background Corpus Warming

Today `RunDiscovery` warms the corpus **synchronously**, so the first turn blocks on
the cold batch-embed (worse with the local server, which also loads a model). This
part warms in a background goroutine; until the corpus is ready, the gate attaches
everything (no gating yet) so nothing blocks.

## Task 16: WarmAsync + ready-gated attachment

**Files:**
- Modify: `internal/discovery/engine.go` (add `WarmAsync`)
- Modify: `internal/agent/discovery_glue.go` (`RunDiscovery` uses `WarmAsync`; `discoveryAllows` short-circuits when not ready)
- Test: `internal/discovery/engine_test.go`

**Interfaces:**
- Produces: `func (eng *Engine) WarmAsync(docs []Doc)` — idempotent; warms once in a goroutine. `Ready()` already exists (Task 7).

- [ ] **Step 1: Write the failing test**

Append to `internal/discovery/engine_test.go`:

```go
func TestWarmAsyncBecomesReady(t *testing.T) {
	eng := NewEngine(FakeEmbedder{Dimension: 32}, t.TempDir())
	if eng.Ready() {
		t.Fatal("not ready before warm")
	}
	eng.WarmAsync(docsFixture())
	// Poll briefly for readiness (the goroutine completes near-instantly with the fake).
	deadline := 100
	for i := 0; i < deadline && !eng.Ready(); i++ {
		time.Sleep(time.Millisecond)
	}
	if !eng.Ready() {
		t.Fatal("WarmAsync should make the engine ready")
	}
	// Second call is a no-op (no panic, still ready).
	eng.WarmAsync(docsFixture())
	if !eng.Ready() {
		t.Fatal("idempotent WarmAsync must keep ready")
	}
}
```

(Add `"time"` to the engine_test.go imports if not present.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/discovery/ -run TestWarmAsync -v`
Expected: FAIL — `WarmAsync` undefined.

- [ ] **Step 3: Implement WarmAsync**

In `internal/discovery/engine.go`, add a `sync.Once` per doc-set to the `Engine`
struct and the method. Add a field:

```go
	warmOnce sync.Once
	warming  bool
```

Add the method:

```go
// WarmAsync warms the corpus in a background goroutine, once. Safe to call every
// turn; subsequent calls are no-ops while/after warming.
func (eng *Engine) WarmAsync(docs []Doc) {
	eng.warmOnce.Do(func() {
		eng.mu.Lock()
		eng.warming = true
		eng.mu.Unlock()
		go func() {
			if err := eng.Warm(context.Background(), docs); err != nil {
				emitDiscoveryDebug("WARN", "background warm failed: "+err.Error())
			}
			eng.mu.Lock()
			eng.warming = false
			eng.mu.Unlock()
		}()
	})
}
```

Note: `Warm` already guards against rebuilding when the doc-set is unchanged, and
`Ready()` reflects `corpus != nil`. If the doc-set changes after the first warm
(e.g. an MCP server connects late), call sites should reset discovery (the
`/discover model` path already does via `ResetDiscovery`); a within-session late MCP
addition is rare and picked up on the next session.

- [ ] **Step 4: Make the gate ready-aware + RunDiscovery non-blocking**

In `internal/agent/discovery_glue.go`, change `RunDiscovery` to kick the async warm
and only rank when ready:

```go
func (a *Agent) RunDiscovery(query string) {
	a.ensureDiscovery()
	if a.disco == nil || !a.disco.enabled || strings.TrimSpace(query) == "" {
		return
	}
	docs := a.discoveryDocs()
	if len(docs) == 0 {
		return
	}
	a.disco.engine.WarmAsync(docs)
	if !a.disco.engine.Ready() {
		emitDebug("DISCOVERY", "corpus still warming — all tools attached this turn")
		return
	}
	added, err := a.disco.session.Discover(context.Background(), query)
	if err != nil {
		emitDebug("DISCOVERY", fmt.Sprintf("rank failed (fail-open, all attached): %v", err))
		a.disco.enabled = false
		return
	}
	emitDebug("DISCOVERY", fmt.Sprintf("turn rank: %d newly attached, %d total (q=%.60q)",
		len(added), len(a.disco.session.Attached()), query))
}
```

And make `discoveryAllows` attach everything while the corpus isn't ready (otherwise
an empty sticky set would gate all MCP tools out during warming):

```go
func (a *Agent) discoveryAllows(name string) bool {
	if a.disco == nil || !a.disco.enabled {
		return true
	}
	if _, isMCP := a.mcpTools[name]; !isMCP {
		return true
	}
	if a.disco.engine == nil || !a.disco.engine.Ready() {
		return true // not warmed yet → don't gate
	}
	return a.disco.session.IsAttached("mcp:" + name)
}
```

- [ ] **Step 5: Run tests + build**

Run: `go test ./internal/discovery/ ./internal/agent/ -run 'TestWarmAsync|TestGate|TestDiscover' -v`
Expected: PASS (the `TestGateOnFiltersMCPOnly` test warms the engine synchronously in
its setup, so `Ready()` is true there and gating still applies).
Run: `go build ./...`
Expected: success.

- [ ] **Step 6: Commit**

```bash
git add internal/discovery/engine.go internal/discovery/engine_test.go internal/agent/discovery_glue.go
git commit -m "feat(discovery): background corpus warming; gate attaches all until ready"
```
