# Part 02 — internal/desktop: server boot helper + run-state watcher

Global constraints from `INDEX.md` apply. This package is pure Go — it MUST NOT import Wails (keeps unit tests cgo-free and the boundary clean).

**Interfaces consumed (produced by Part 01):**

```go
// package github.com/u007/ocode/web
func FS() fs.FS // built SPA, index.html at root, nil if unavailable

// package github.com/u007/ocode/internal/server
func New(addr, username, password string, webFS fs.FS) *Server
func (s *Server) SetWorkDir(dir string)
func (s *Server) Listen() (net.Listener, error)
func (s *Server) Serve(ln net.Listener) error
type RunState struct {
	SessionID string
	ID        string
	Name      string
	Ended     bool
	Failed    bool
}
func (s *Server) RunStates() []RunState
```

Auth facts (verified in `internal/server/server.go:199-204`): the middleware accepts `Authorization: Bearer <token>` and `?token=<token>`; `server.New(addr, "ocode", token, webFS)` is the exact pattern the TUI `/rc` command uses (`internal/tui/model.go:16198`).

Port fact: `Server.Listen()` with port `0` binds a random port but writes the *requested* candidate string back to `s.addr` — so the real port MUST be read from `ln.Addr()`, never from `s.Addr()`.

---

### Task 3: Server boot helper

**Files:**
- Create: `internal/desktop/boot.go`
- Create: `internal/desktop/boot_test.go`

**Interfaces:**
- Produces (Part 03 relies on these exact names):

```go
// package github.com/u007/ocode/internal/desktop
type Handle struct {
	URL   string // e.g. "http://127.0.0.1:52341" (no trailing slash)
	Token string // hex-encoded 16-byte random token
	Srv   *server.Server
}
func StartServer(webFS fs.FS, workDir string) (*Handle, error)
```

- [ ] **Step 1: Write the failing test**

`internal/desktop/boot_test.go`:

```go
package desktop

import (
	"net/http"
	"testing"
	"time"
)

func TestStartServerServesAuthedAPI(t *testing.T) {
	h, err := StartServer(nil, t.TempDir()) // nil webFS: API still works, SPA 404s
	if err != nil {
		t.Fatalf("StartServer: %v", err)
	}
	if h.Token == "" || len(h.Token) != 32 {
		t.Fatalf("expected 32-char hex token, got %q", h.Token)
	}

	client := &http.Client{Timeout: 2 * time.Second}

	// Authed request succeeds.
	req, _ := http.NewRequest("GET", h.URL+"/api/models", nil)
	req.Header.Set("Authorization", "Bearer "+h.Token)
	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("authed request failed: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("authed /api/models: got %d, want 200", res.StatusCode)
	}

	// Unauthed request is rejected.
	res2, err := client.Do(&http.Request{Method: "GET", URL: mustParse(t, h.URL+"/api/models")})
	if err != nil {
		t.Fatalf("unauthed request failed: %v", err)
	}
	res2.Body.Close()
	if res2.StatusCode == http.StatusOK {
		t.Fatalf("unauthed /api/models must not return 200")
	}
}
```

Add the tiny helper in the same test file:

```go
func mustParse(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return u
}
```

(imports: `net/url` too). If `/api/models` turns out to require config state and errors 500 in a bare test env, switch both requests to `GET /api/tui-status` — pick one endpoint and use it for both assertions.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/desktop/ -v`
Expected: FAIL — package/function undefined.

- [ ] **Step 3: Implement `internal/desktop/boot.go`**

```go
// Package desktop contains the pure-Go core of the ocode desktop shell:
// in-process server boot and run-state watching. It must not import Wails
// so it stays unit-testable without cgo.
package desktop

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io/fs"
	"log"
	"net"

	"github.com/u007/ocode/internal/server"
)

// Handle describes a running in-process ocode server owned by the desktop
// shell. The server dies with the process (internal/server has no graceful
// shutdown API; process exit is the designed teardown — see spec).
type Handle struct {
	URL   string // e.g. "http://127.0.0.1:52341" (no trailing slash)
	Token string // hex-encoded 16-byte random auth token
	Srv   *server.Server
}

// StartServer boots internal/server on a random loopback port with a fresh
// auth token and returns once the listener is bound (the API is servable
// immediately after Listen; no readiness poll needed).
func StartServer(webFS fs.FS, workDir string) (*Handle, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("desktop: generate auth token: %w", err)
	}
	token := hex.EncodeToString(b)

	srv := server.New("127.0.0.1:0", "ocode", token, webFS)
	srv.SetWorkDir(workDir)

	ln, err := srv.Listen()
	if err != nil {
		return nil, fmt.Errorf("desktop: bind loopback listener: %w", err)
	}
	// Listen() writes the *requested* addr back on port-0 binds; the real
	// port only exists on the listener.
	port := ln.Addr().(*net.TCPAddr).Port

	go func() {
		if err := srv.Serve(ln); err != nil {
			log.Printf("desktop: embedded server exited: %v", err)
		}
	}()

	return &Handle{
		URL:   fmt.Sprintf("http://127.0.0.1:%d", port),
		Token: token,
		Srv:   srv,
	}, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/desktop/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/desktop/boot.go internal/desktop/boot_test.go
git commit -m "feat(desktop): in-process server boot helper

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 4: Run-state watcher (poll + diff)

Mirrors `HandleRunsStream`'s poll-and-diff pattern (750ms ticker, emit only on change) against `Server.RunStates()`.

**Files:**
- Create: `internal/desktop/watch.go`
- Create: `internal/desktop/watch_test.go`

**Interfaces:**
- Consumes: `server.RunState` (Part 01).
- Produces (Part 03 relies on these exact names):

```go
type Summary struct {
	RunningCount int
	Finished     []server.RunState // transitioned running→ended since previous poll
}
func Diff(prev, cur []server.RunState) Summary
func Watch(ctx context.Context, interval time.Duration, source func() []server.RunState, onChange func(Summary))
```

`onChange` fires only when `RunningCount` changed vs. the last emitted summary OR `len(Finished) > 0`. `Watch` blocks until ctx is done (caller runs it in a goroutine).

- [ ] **Step 1: Write the failing tests**

`internal/desktop/watch_test.go`:

```go
package desktop

import (
	"context"
	"testing"
	"time"

	"github.com/u007/ocode/internal/server"
)

func rs(session, id string, ended bool) server.RunState {
	return server.RunState{SessionID: session, ID: id, Ended: ended}
}

func TestDiffCountsRunning(t *testing.T) {
	cur := []server.RunState{rs("s1", "a", false), rs("s1", "b", true), rs("s2", "c", false)}
	sum := Diff(nil, cur)
	if sum.RunningCount != 2 {
		t.Fatalf("RunningCount = %d, want 2", sum.RunningCount)
	}
	if len(sum.Finished) != 0 {
		t.Fatalf("nil prev must produce no Finished (first poll is baseline), got %d", len(sum.Finished))
	}
}

func TestDiffDetectsFinishedTransition(t *testing.T) {
	prev := []server.RunState{rs("s1", "a", false), rs("s1", "b", false)}
	cur := []server.RunState{rs("s1", "a", true), rs("s1", "b", false)}
	sum := Diff(prev, cur)
	if len(sum.Finished) != 1 || sum.Finished[0].ID != "a" {
		t.Fatalf("Finished = %+v, want exactly run a", sum.Finished)
	}
	if sum.RunningCount != 1 {
		t.Fatalf("RunningCount = %d, want 1", sum.RunningCount)
	}
}

func TestDiffKeysBySessionAndID(t *testing.T) {
	// Same run ID in two sessions must not cross-match.
	prev := []server.RunState{rs("s1", "a", false), rs("s2", "a", true)}
	cur := []server.RunState{rs("s1", "a", true), rs("s2", "a", true)}
	sum := Diff(prev, cur)
	if len(sum.Finished) != 1 || sum.Finished[0].SessionID != "s1" {
		t.Fatalf("Finished = %+v, want exactly s1/a", sum.Finished)
	}
}

func TestWatchEmitsOnChangeOnly(t *testing.T) {
	states := make(chan []server.RunState, 3)
	states <- []server.RunState{rs("s1", "a", false)} // baseline: 1 running
	states <- []server.RunState{rs("s1", "a", false)} // no change → no emit
	states <- []server.RunState{rs("s1", "a", true)}  // finished → emit

	var current []server.RunState
	source := func() []server.RunState {
		select {
		case s := <-states:
			current = s
		default: // keep returning last state once the script is exhausted
		}
		return current
	}

	got := make(chan Summary, 10)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go Watch(ctx, 10*time.Millisecond, source, func(s Summary) { got <- s })

	first := <-got // baseline emit: RunningCount 1
	if first.RunningCount != 1 || len(first.Finished) != 0 {
		t.Fatalf("first emit = %+v, want RunningCount 1, no Finished", first)
	}
	second := <-got // the finish transition
	if second.RunningCount != 0 || len(second.Finished) != 1 {
		t.Fatalf("second emit = %+v, want RunningCount 0, 1 Finished", second)
	}
	select {
	case extra := <-got:
		t.Fatalf("unexpected third emit: %+v", extra)
	case <-time.After(100 * time.Millisecond):
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/desktop/ -run 'TestDiff|TestWatch' -v`
Expected: FAIL — `Diff`/`Watch`/`Summary` undefined.

- [ ] **Step 3: Implement `internal/desktop/watch.go`**

```go
package desktop

import (
	"context"
	"time"

	"github.com/u007/ocode/internal/server"
)

// Summary is one observed change in the run-state world.
type Summary struct {
	RunningCount int
	// Finished lists runs that transitioned running→ended since the
	// previous poll. Nil prev (first poll) yields none: startup must not
	// replay history as notifications.
	Finished []server.RunState
}

type runKey struct{ session, id string }

// Diff compares two RunStates snapshots. Runs are keyed by
// (SessionID, ID) — run IDs are only unique per session registry.
func Diff(prev, cur []server.RunState) Summary {
	sum := Summary{}
	prevRunning := make(map[runKey]bool, len(prev))
	for _, p := range prev {
		if !p.Ended {
			prevRunning[runKey{p.SessionID, p.ID}] = true
		}
	}
	for _, c := range cur {
		if !c.Ended {
			sum.RunningCount++
			continue
		}
		if prevRunning[runKey{c.SessionID, c.ID}] {
			sum.Finished = append(sum.Finished, c)
		}
	}
	return sum
}

// Watch polls source on interval and invokes onChange when the running
// count changed or runs finished — the same poll-and-diff pattern
// HandleRunsStream uses over SSE. It always emits one baseline summary on
// the first poll, then stays quiet while nothing changes. Blocks until ctx
// is done.
func Watch(ctx context.Context, interval time.Duration, source func() []server.RunState, onChange func(Summary)) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var prev []server.RunState
	lastRunning := -1 // sentinel: force the baseline emit
	for {
		cur := source()
		sum := Diff(prev, cur)
		if sum.RunningCount != lastRunning || len(sum.Finished) > 0 {
			onChange(sum)
			lastRunning = sum.RunningCount
		}
		prev = cur

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/desktop/ -v`
Expected: all PASS (including Task 3's).

- [ ] **Step 5: Commit**

```bash
git add internal/desktop/watch.go internal/desktop/watch_test.go
git commit -m "feat(desktop): run-state watcher with poll-and-diff

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```
