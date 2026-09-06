# ocode Remote — Phase 2 (Web Mode) + Phase 3 (WSL) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `ocode remote --web <target>` open a browser tab against a fully-functional remote-hosted ocode server (SSH tunnel, token auth, session resume), then extend the same transport abstraction to `wsl:<distro>` targets on Windows.

**Architecture:** `internal/remote` already implements Phase 1 (TUI passthrough over SSH) behind a `Transport` interface (`Exec`/`ExecStdin`/`ExecInteractive`/`Copy`/`Describe`), with provisioning, credential sync, multiplexer-resume, and staged-progress rendering all transport-agnostic. Phase 2 adds a new `serve.go`: discover-or-launch a detached `ocode serve --remote` process on the target, tunnel it to a local port over `ssh -N -L` (skipped entirely for WSL — Windows shares localhost with WSL2), and open the browser with a one-time token in the URL fragment. The server gains a `--remote` launch mode: always loopback-bound, always token-protected, writes a discovery state file. Phase 3 adds `wsl.go`, a second `Transport` implementation shelling out to `wsl.exe`, and removes the Phase-1 stub rejection of `wsl:` targets — `Connect`/`ConnectWeb` select the transport by `Target.Kind`, so every provisioning/sync/multiplex call written for Phase 1 works unchanged.

**Tech Stack:** Go (stdlib `net/http`, `flag`, `os/exec`, `crypto/rand`), `github.com/gorilla/websocket`, existing `internal/tool.ProcessSupervisor`, `internal/secretfile.WriteFileAtomic`; React/TypeScript frontend (`web/src/api/client.ts`, `web/src/components/Terminal/TerminalPanel.tsx`).

**Spec:** `docs/superpowers/specs/2026-08-29-remote-ssh/` — `01-architecture.md` (shared foundations, already implemented), `03-phase2-web.md` (this plan's Tasks 1-9), `04-phase3-wsl.md` (this plan's Tasks 10-13).

## Global Constraints

- Remote `ocode serve --remote` **always** binds `127.0.0.1` regardless of `-host`, and **always** generates and requires an API token — never starts with auth disabled. (`03-phase2-web.md`)
- Token is **never** accepted via query string in remote mode — header (`Authorization: Bearer`) or the WebSocket subprotocol pattern only. Query-string token support for the existing local/basic-auth and `/rc` paths is unchanged when `--remote` is not set. (`03-phase2-web.md`, `01-architecture.md` invariant 2)
- Every spawned process (ssh tunnel, `wsl.exe`, detached remote-serve launch) registers with `internal/tool.ProcessSupervisor` — no uncoordinated spawn sites. (`01-architecture.md`)
- Transport is shelled-out system `ssh`/`scp`/`wsl.exe` — never `x/crypto/ssh`. (`01-architecture.md`)
- WSL is a transport swap only: provisioning (`provision.go`), credential sync (`sync.go`), and multiplex-resume (`multiplex.go`) are reused completely unmodified. (`04-phase3-wsl.md`)
- Implement in order: Phase 2 fully before Phase 3 (Phase 3's web mode is explicitly "the Phase 2 design" reused). (`INDEX.md`)
- Out of scope, do not implement: desktop-shell auto-connect UI (selecting a bookmarked remote project in `ProjectSidebar.tsx` does **not** need to trigger a connect — that integration is explicitly listed out of scope in `INDEX.md`), full TUI thin client, session sync, SSH→Windows→WSL chaining, release-artifact download infrastructure.
- Failure output always names the failing stage and shows the underlying `ssh`/`scp`/`wsl.exe` stderr verbatim — reuse the existing `Progress`/`namedError` machinery, never a new ad hoc error path.

---

## Task 1: Server `--remote` launch mode (loopback bind + generated token)

**Files:**
- Modify: `internal/server/server.go` (`Server` struct ~line 81, `New` ~line 120, `Run` ~line 1227-1295)
- Test: `internal/server/server_test.go`

**Interfaces:**
- Produces: `func (s *Server) SetRemoteMode(v bool)`, field `Server.remoteMode bool`, and a `Run` flag `-remote`. Later tasks (2, 4, 5) read `s.remoteMode`.

- [ ] **Step 1: Write the failing test**

```go
func TestRunRemoteModeForcesLoopbackAndGeneratesToken(t *testing.T) {
	s := New("0.0.0.0:0", "", "", nil)
	if s.remoteMode {
		t.Fatal("remoteMode should default false")
	}
	s.SetRemoteMode(true)
	if !s.remoteMode {
		t.Fatal("SetRemoteMode(true) did not set the field")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/ -run TestRunRemoteModeForcesLoopbackAndGeneratesToken -v`
Expected: FAIL (compile error: `s.remoteMode` / `SetRemoteMode` undefined)

- [ ] **Step 3: Add the field, setter, and flag/token wiring**

In `Server` struct, add near `workDir`:

```go
	// remoteMode is true when the process was launched as `ocode serve
	// --remote` (see Run). It forces loopback-only binding, requires a
	// generated API token (never the OPENCODE_SERVER_* env vars), and
	// switches checkAuth into a stricter mode that rejects the ?token=
	// query-string path (see Task 2).
	remoteMode bool
```

Add the setter next to `SetWorkDir`-style methods:

```go
// SetRemoteMode marks the server as launched in `--remote` mode. Must be
// called before Serve; it only affects checkAuth behavior (Task 2), not
// binding — the caller (Run) is responsible for forcing the loopback
// address and generating the token before constructing the listener.
func (s *Server) SetRemoteMode(v bool) {
	s.remoteMode = v
}
```

In `Run`, add the flag and generate the token when set, before `New` is called:

```go
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	port := fs.Int("port", 4096, "Port to listen on")
	host := fs.String("host", "0.0.0.0", "Host to bind to")
	openBrowser := fs.Bool("open", false, "Open browser after starting")
	remoteFlag := fs.Bool("remote", false, "Remote mode: bind 127.0.0.1 only, require a generated API token, and write ~/.ocode/remote/serve.json for reconnect discovery")
	fs.Parse(args)

	if *remoteFlag {
		*host = "127.0.0.1"
	}
	addr := fmt.Sprintf("%s:%d", *host, *port)
	username := os.Getenv("OPENCODE_SERVER_USERNAME")
	password := os.Getenv("OPENCODE_SERVER_PASSWORD")
	if *remoteFlag {
		token, err := generateRemoteToken()
		if err != nil {
			return fmt.Errorf("generate remote API token: %w", err)
		}
		username = ""
		password = token
	}

	srv := New(addr, username, password, webFS)
	srv.SetRemoteMode(*remoteFlag)
```

Add the token generator near the bottom of `server.go` (alongside `openURL`):

```go
// generateRemoteToken returns a 256-bit random token, hex-encoded, for
// --remote mode. Generated fresh on every launch — never derived from or
// stored alongside a user-chosen password.
func generateRemoteToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
```

Add `"crypto/rand"` and `"encoding/hex"` to the import block (note: this file may already import a different `rand` — check for `math/rand` collisions before adding; if present, import as `cryptorand "crypto/rand"` and call `cryptorand.Read`).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/server/ -run TestRunRemoteModeForcesLoopbackAndGeneratesToken -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/server/server.go internal/server/server_test.go
git commit -m "feat(server): add --remote launch mode scaffolding (loopback bind, generated token)"
```

---

## Task 2: Strict token auth in remote mode (no query string; refuse to start unauthenticated)

**Files:**
- Modify: `internal/server/server.go` (`checkAuth` ~line 453, `authMiddleware` ~line 470, `Run` from Task 1)
- Test: `internal/server/server_test.go`

**Interfaces:**
- Consumes: `Server.remoteMode` (Task 1)
- Produces: `checkAuth` behavior change consumed by Task 5 (WS subprotocol auth) and by the frontend fragment-bootstrap work (Tasks 7-9), which must never rely on `?token=` when talking to a `--remote` server.

- [ ] **Step 1: Write the failing tests**

```go
func TestCheckAuthRemoteModeRejectsQueryStringToken(t *testing.T) {
	s := New("127.0.0.1:0", "", "tok123", nil)
	s.SetRemoteMode(true)

	r := httptest.NewRequest("GET", "/api/sessions?token=tok123", nil)
	if s.checkAuth(r) {
		t.Fatal("remote mode must reject a query-string token even when it matches")
	}

	r2 := httptest.NewRequest("GET", "/api/sessions", nil)
	r2.Header.Set("Authorization", "Bearer tok123")
	if !s.checkAuth(r2) {
		t.Fatal("remote mode must still accept a matching Bearer header")
	}
}

func TestRunRemoteModeRefusesEmptyToken(t *testing.T) {
	s := New("127.0.0.1:0", "", "", nil)
	s.SetRemoteMode(true)
	// authMiddleware must never become a no-op (username=="" && password=="")
	// short-circuit in remote mode — an empty password would otherwise
	// disable auth entirely, which contradicts "always requires a token."
	r := httptest.NewRequest("GET", "/api/sessions", nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("remote mode with no password configured must still deny access, got %d", w.Code)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/server/ -run 'TestCheckAuthRemoteModeRejectsQueryStringToken|TestRunRemoteModeRefusesEmptyToken' -v`
Expected: FAIL — `TestCheckAuthRemoteModeRejectsQueryStringToken` fails because `checkAuth` still accepts `?token=`; `TestRunRemoteModeRefusesEmptyToken` fails because `authMiddleware` short-circuits to `next` when `password == ""`.

- [ ] **Step 3: Implement**

Update `checkAuth`:

```go
func (s *Server) checkAuth(r *http.Request) bool {
	// Bearer token header (used by frontend fetch calls)
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		return auth[7:] == s.password
	}
	// ?token= query param (used by EventSource, which can't set headers).
	// Forbidden in --remote mode: query strings reach access logs and
	// intermediary proxies, which the remote token model treats as a leak.
	if !s.remoteMode {
		if tok := r.URL.Query().Get("token"); tok != "" {
			return tok == s.password
		}
	}
	// HTTP Basic Auth
	user, pass, ok := r.BasicAuth()
	if ok {
		return (s.username == "" || user == s.username) && pass == s.password
	}
	return false
}
```

Update `authMiddleware` so remote mode never takes the "auth disabled" fast path even if `password` were somehow empty:

```go
func (s *Server) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	if !s.remoteMode && s.username == "" && s.password == "" {
		return next
	}
	return func(w http.ResponseWriter, r *http.Request) {
```

(rest of the function body unchanged — `checkAuth` already returns `false` for every scheme when `s.password == ""` and no header/basic-auth matches, so the wrapped handler now correctly 401s instead of the middleware not being installed at all.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/server/ -run 'TestCheckAuthRemoteModeRejectsQueryStringToken|TestRunRemoteModeRefusesEmptyToken|TestAuthMiddleware|TestNoAuthWhenEmpty' -v`
Expected: PASS (including the two pre-existing tests, to confirm no regression to non-remote behavior)

- [ ] **Step 5: Commit**

```bash
git add internal/server/server.go internal/server/server_test.go
git commit -m "fix(server): remote mode rejects query-string tokens and never disables auth"
```

---

## Task 3: `GET /api/health` endpoint

**Files:**
- Create: `internal/server/handler_health.go`
- Modify: `internal/server/server.go` (route registration, near the other unauthenticated-safe routes — this one is intentionally registered **without** `authMiddleware`, since Task 6's reuse probe calls it locally before the client has a token)
- Test: `internal/server/handler_health_test.go`

**Interfaces:**
- Produces: `GET /api/health` → `200 {"version": "<version.Version>"}`. Consumed by Task 6 (`ServerAlive`'s exec-based curl probe) and generally useful for the reconnect page.

- [ ] **Step 1: Write the failing test**

```go
package server

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/u007/ocode/internal/version"
)

func TestHandleHealth(t *testing.T) {
	s := New("127.0.0.1:0", "user", "pass", nil) // even an authenticated server...
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/health", nil) // ...answers /api/health with no credentials
	s.mux.ServeHTTP(w, r)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var body struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON body: %v", err)
	}
	if body.Version != version.Version {
		t.Errorf("version = %q, want %q", body.Version, version.Version)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/ -run TestHandleHealth -v`
Expected: FAIL with 404 (route not registered)

- [ ] **Step 3: Implement**

`internal/server/handler_health.go`:

```go
package server

import (
	"net/http"

	"github.com/u007/ocode/internal/version"
)

// handleHealth answers GET /api/health unauthenticated (deliberately not
// wrapped in authMiddleware): it exists so a --remote server's reuse check
// (internal/remote's ServerAlive, run as a short-lived exec probe on the
// remote host itself, before any tunnel or token has been established) can
// tell "process alive and this ocode's HTTP stack is actually serving" from
// "process alive but wedged/still booting", without needing the token.
// Never expose anything beyond the version here.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"version": version.Version})
}
```

Register the route in `server.go`'s `registerRoutes` (near the top, unauthenticated group):

```go
	s.mux.HandleFunc("GET /api/health", s.handleHealth)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/server/ -run TestHandleHealth -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/server/handler_health.go internal/server/handler_health_test.go internal/server/server.go
git commit -m "feat(server): add unauthenticated GET /api/health for remote reuse probing"
```

---

## Task 4: Remote-mode state file (`~/.ocode/remote/serve.json`)

**Files:**
- Modify: `internal/server/server.go` (`Run`, after `srv.Listen()` succeeds)
- Test: `internal/server/server_test.go`

**Interfaces:**
- Produces: on disk, `0600`, atomic (temp+rename via `internal/secretfile.WriteFileAtomic`): `{"pid":<int>,"port":<int>,"token":"<hex>","version":"<semver>","startedAt":"<RFC3339>"}` at `~/.ocode/remote/serve.json`. Consumed by Task 6's `DiscoverServer`.

- [ ] **Step 1: Write the failing test**

```go
func TestRunRemoteModeWritesStateFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	s := New("127.0.0.1:0", "", "sometoken", nil)
	s.SetRemoteMode(true)
	ln, err := s.Listen()
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()

	if err := s.writeRemoteStateFile(ln); err != nil {
		t.Fatalf("writeRemoteStateFile: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(home, ".ocode", "remote", "serve.json"))
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}
	info, _ := os.Stat(filepath.Join(home, ".ocode", "remote", "serve.json"))
	if info.Mode().Perm() != 0600 {
		t.Errorf("state file mode = %o, want 0600", info.Mode().Perm())
	}
	var state struct {
		PID     int    `json:"pid"`
		Port    int    `json:"port"`
		Token   string `json:"token"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if state.Token != "sometoken" {
		t.Errorf("token = %q, want %q", state.Token, "sometoken")
	}
	if state.PID != os.Getpid() {
		t.Errorf("pid = %d, want %d", state.PID, os.Getpid())
	}
	_, boundPortStr, _ := net.SplitHostPort(ln.Addr().String())
	boundPort, _ := strconv.Atoi(boundPortStr)
	if state.Port != boundPort {
		t.Errorf("port = %d, want %d", state.Port, boundPort)
	}
}
```

Add `"os"`, `"path/filepath"` to the test file's imports if not already present (`"strconv"` and `"net"` are already imported per the existing test file header).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/ -run TestRunRemoteModeWritesStateFile -v`
Expected: FAIL (compile error: `writeRemoteStateFile` undefined)

- [ ] **Step 3: Implement**

Add to `server.go`:

```go
// remoteServeState is the JSON shape written to ~/.ocode/remote/serve.json
// by a --remote launch, and read back by internal/remote's reconnect
// discovery (DiscoverServer). Field names are the wire contract with that
// package — do not rename without updating both sides.
type remoteServeState struct {
	PID       int       `json:"pid"`
	Port      int       `json:"port"`
	Token     string    `json:"token"`
	Version   string    `json:"version"`
	StartedAt time.Time `json:"startedAt"`
}

// writeRemoteStateFile persists this server's identity so a later
// `ocode remote --web` reconnect can discover and reuse it instead of
// launching a duplicate. Called once, right after Listen succeeds, only
// when remoteMode is set. 0600 + temp-file-then-rename (secretfile's atomic
// writer) — never partially visible, never world-readable (it carries the
// bearer token).
func (s *Server) writeRemoteStateFile(ln net.Listener) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home dir for remote state file: %w", err)
	}
	dir := filepath.Join(home, ".ocode", "remote")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	_, portStr, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		return fmt.Errorf("parse bound address %s: %w", ln.Addr().String(), err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return fmt.Errorf("parse bound port %q: %w", portStr, err)
	}
	state := remoteServeState{
		PID:       os.Getpid(),
		Port:      port,
		Token:     s.password,
		Version:   version.Version,
		StartedAt: time.Now(),
	}
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal remote state: %w", err)
	}
	return secretfile.WriteFileAtomic(filepath.Join(dir, "serve.json"), data, 0600)
}
```

Add imports: `"github.com/u007/ocode/internal/secretfile"`, `"github.com/u007/ocode/internal/version"` (if not already imported), `"path/filepath"`, `"strconv"`.

Wire it into `Run`, right after the existing `ln, err := srv.Listen()` block:

```go
	ln, err := srv.Listen()
	if err != nil {
		return err
	}
	if *remoteFlag {
		if err := srv.writeRemoteStateFile(ln); err != nil {
			return fmt.Errorf("write remote state file: %w", err)
		}
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/server/ -run TestRunRemoteModeWritesStateFile -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/server/server.go internal/server/server_test.go
git commit -m "feat(server): write ~/.ocode/remote/serve.json in --remote mode for reconnect discovery"
```

---

## Task 5: WebSocket subprotocol token auth for `/api/terminal/ws` in remote mode

**Files:**
- Modify: `internal/server/server.go` (`checkAuth`)
- Modify: `internal/server/handler_terminal.go` (`HandleTerminalWS`'s `Upgrade` call, ~line 193)
- Test: `internal/server/server_test.go`, `internal/server/handler_terminal_test.go` (create if it doesn't already have WS coverage — check first with `ls internal/server/handler_terminal*test*`)

**Interfaces:**
- Consumes: `Server.remoteMode` (Task 1), `checkAuth` (Task 2)
- Produces: a WS handshake bearing `Sec-WebSocket-Protocol: ocode.bearer.<token>` is accepted in remote mode and the server echoes the same subprotocol string back (required by the WebSocket spec for the browser's connection to complete). Consumed by Task 9 (frontend `TerminalPanel.tsx`).

- [ ] **Step 1: Write the failing test**

```go
func TestCheckAuthRemoteModeAcceptsWebSocketSubprotocolToken(t *testing.T) {
	s := New("127.0.0.1:0", "", "tok123", nil)
	s.SetRemoteMode(true)

	r := httptest.NewRequest("GET", "/api/terminal/ws", nil)
	r.Header.Set("Sec-WebSocket-Protocol", "ocode.bearer.tok123")
	if !s.checkAuth(r) {
		t.Fatal("remote mode must accept a matching Sec-WebSocket-Protocol bearer token")
	}

	r2 := httptest.NewRequest("GET", "/api/terminal/ws", nil)
	r2.Header.Set("Sec-WebSocket-Protocol", "ocode.bearer.wrong")
	if s.checkAuth(r2) {
		t.Fatal("remote mode must reject a mismatched subprotocol token")
	}
}

func TestRemoteWebSocketSubprotocolNotAcceptedOutsideRemoteMode(t *testing.T) {
	s := New("127.0.0.1:0", "", "tok123", nil)
	// remoteMode left false: the ?token= path (Task 2) is how non-remote WS
	// auth works today. The subprotocol path should still parse harmlessly
	// but must not be treated as authoritative outside remote mode, since
	// non-remote checkAuth never reaches this branch — assert via the
	// negative: no Authorization header, no ?token, only the subprotocol,
	// non-remote mode.
	r := httptest.NewRequest("GET", "/api/terminal/ws", nil)
	r.Header.Set("Sec-WebSocket-Protocol", "ocode.bearer.tok123")
	if s.checkAuth(r) {
		t.Fatal("non-remote mode must not authenticate via the WS subprotocol token")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/server/ -run 'TestCheckAuthRemoteModeAcceptsWebSocketSubprotocolToken|TestRemoteWebSocketSubprotocolNotAcceptedOutsideRemoteMode' -v`
Expected: FAIL — `checkAuth` has no subprotocol branch yet, so the first test fails and the second passes vacuously (acceptable — it will stay green once Step 3 lands, since the branch is gated on `s.remoteMode`).

- [ ] **Step 3: Implement**

Add a helper and extend `checkAuth` in `server.go`:

```go
// remoteWSProtocolPrefix namespaces the token carried in Sec-WebSocket-
// Protocol so it can't collide with a real subprotocol a future WS endpoint
// might negotiate.
const remoteWSProtocolPrefix = "ocode.bearer."

// remoteWSToken extracts the bearer token from a Sec-WebSocket-Protocol
// header value (which may list multiple comma-separated protocols, per
// RFC 6455 — the browser API takes an array), or "" if none match the
// ocode.bearer. prefix.
func remoteWSToken(header string) string {
	for _, p := range strings.Split(header, ",") {
		p = strings.TrimSpace(p)
		if tok, ok := strings.CutPrefix(p, remoteWSProtocolPrefix); ok {
			return tok
		}
	}
	return ""
}
```

Extend `checkAuth` (only reachable in remote mode, since the browser cannot set an `Authorization` header on a WebSocket handshake and remote mode forbids `?token=`):

```go
	// ?token= query param (used by EventSource, which can't set headers)...
	if !s.remoteMode {
		if tok := r.URL.Query().Get("token"); tok != "" {
			return tok == s.password
		}
	}
	// WebSocket subprotocol token (remote mode only — the browser WebSocket
	// API can't set Authorization or use ?token= safely under the remote
	// token model, but it can offer a Sec-WebSocket-Protocol list).
	if s.remoteMode {
		if tok := remoteWSToken(r.Header.Get("Sec-WebSocket-Protocol")); tok != "" {
			return tok == s.password
		}
	}
```

In `handler_terminal.go`, make the upgrade echo back the negotiated subprotocol so the browser's `new WebSocket(url, [protocol])` call succeeds (per spec, the client fails the connection if the server doesn't select one of the offered subprotocols):

```go
	var respHeader http.Header
	if proto := r.Header.Get("Sec-WebSocket-Protocol"); proto != "" {
		if tok := remoteWSToken(proto); tok != "" {
			respHeader = http.Header{"Sec-WebSocket-Protocol": {remoteWSProtocolPrefix + tok}}
		}
	}
	ws, err := terminalUpgrader.Upgrade(w, r, respHeader)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/server/ -run 'TestCheckAuthRemoteModeAcceptsWebSocketSubprotocolToken|TestRemoteWebSocketSubprotocolNotAcceptedOutsideRemoteMode' -v`
Expected: PASS

- [ ] **Step 5: Run the full server package test suite to check for regressions**

Run: `go test ./internal/server/... -v`
Expected: PASS (all)

- [ ] **Step 6: Commit**

```bash
git add internal/server/server.go internal/server/handler_terminal.go internal/server/server_test.go
git commit -m "feat(server): support WebSocket subprotocol bearer token for remote-mode terminal auth"
```

---

## Task 6: `internal/remote/serve.go` — state-file discovery, reuse-vs-fresh, tunnel, free port

**Files:**
- Create: `internal/remote/serve.go`
- Create: `internal/remote/serve_test.go`

**Interfaces:**
- Consumes: `Transport` (existing), `tool.ProcessSupervisor`/`tool.StartSupervised` (existing)
- Produces: `type ServeState struct { PID int; Port int; Token string; Version string; StartedAt time.Time }`, `func DiscoverServer(t Transport) (ServeState, bool)`, `func ServerAlive(t Transport, state ServeState, localVersion string) bool`, `func StartFreshServer(t Transport, ver string) (ServeState, error)`, `func EnsureRemoteServer(t Transport, ver string) (state ServeState, reused bool, err error)`, `func FreeLocalPort() (int, error)`, `func StartTunnel(sup *tool.ProcessSupervisor, target Target, localPort, remotePort int) (*exec.Cmd, error)`. Consumed by Task 7 (`ConnectWeb`).

- [ ] **Step 1: Write the failing tests**

```go
package remote

import (
	"encoding/json"
	"net"
	"testing"
	"time"
)

func TestDiscoverServerMissing(t *testing.T) {
	ft := newFakeTransport()
	ft.execResults[remoteStateCatCmd] = ExecResult{Stdout: "", ExitCode: 0}
	_, ok := DiscoverServer(ft)
	if ok {
		t.Fatal("expected ok=false for empty state file output")
	}
}

func TestDiscoverServerParsesState(t *testing.T) {
	ft := newFakeTransport()
	want := ServeState{PID: 123, Port: 4096, Token: "tok", Version: "0.8.85", StartedAt: time.Unix(1700000000, 0).UTC()}
	data, _ := json.Marshal(want)
	ft.execResults[remoteStateCatCmd] = ExecResult{Stdout: string(data), ExitCode: 0}

	got, ok := DiscoverServer(ft)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestDiscoverServerCorruptFileTreatedAsMissing(t *testing.T) {
	ft := newFakeTransport()
	ft.execResults[remoteStateCatCmd] = ExecResult{Stdout: "not json", ExitCode: 0}
	if _, ok := DiscoverServer(ft); ok {
		t.Fatal("corrupt state file must be treated as missing, not surfaced as an error")
	}
}

func TestServerAliveRequiresPidAndVersionAndHealth(t *testing.T) {
	state := ServeState{PID: 42, Port: 4096, Token: "tok", Version: "0.8.85"}

	ft := newFakeTransport()
	ft.execResults[pidAliveCmd(42)] = ExecResult{ExitCode: 0}
	ft.execResults[healthProbeCmd(4096, "tok")] = ExecResult{Stdout: "200"}
	if !ServerAlive(ft, state, "0.8.85") {
		t.Fatal("expected alive: pid alive, version matches, health 200")
	}

	ftDeadPid := newFakeTransport()
	ftDeadPid.execResults[pidAliveCmd(42)] = ExecResult{ExitCode: 1}
	if ServerAlive(ftDeadPid, state, "0.8.85") {
		t.Fatal("expected not alive: pid check failed")
	}

	ftVersionMismatch := newFakeTransport()
	ftVersionMismatch.execResults[pidAliveCmd(42)] = ExecResult{ExitCode: 0}
	if ServerAlive(ftVersionMismatch, state, "0.9.0") {
		t.Fatal("expected not alive: version mismatch")
	}

	ftBadHealth := newFakeTransport()
	ftBadHealth.execResults[pidAliveCmd(42)] = ExecResult{ExitCode: 0}
	ftBadHealth.execResults[healthProbeCmd(4096, "tok")] = ExecResult{Stdout: "000"}
	if ServerAlive(ftBadHealth, state, "0.8.85") {
		t.Fatal("expected not alive: health probe did not return 200 (curl missing or server wedged)")
	}
}

func TestEnsureRemoteServerReusesLiveMatchingServer(t *testing.T) {
	existing := ServeState{PID: 42, Port: 4096, Token: "tok", Version: "0.8.85"}
	data, _ := json.Marshal(existing)

	ft := newFakeTransport()
	ft.execResults[remoteStateCatCmd] = ExecResult{Stdout: string(data)}
	ft.execResults[pidAliveCmd(42)] = ExecResult{ExitCode: 0}
	ft.execResults[healthProbeCmd(4096, "tok")] = ExecResult{Stdout: "200"}

	state, reused, err := EnsureRemoteServer(ft, "0.8.85")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reused {
		t.Error("expected reused=true")
	}
	if state != existing {
		t.Errorf("got %+v, want %+v", state, existing)
	}
	for _, c := range ft.execCalls {
		if c == launchServerCmd("0.8.85") {
			t.Error("EnsureRemoteServer launched a fresh server when an alive one was discovered")
		}
	}
}

func TestEnsureRemoteServerStartsFreshWhenStale(t *testing.T) {
	stale := ServeState{PID: 42, Port: 4096, Token: "oldtok", Version: "0.8.85"}
	data, _ := json.Marshal(stale)
	fresh := ServeState{PID: 999, Port: 4097, Token: "newtok", Version: "0.8.85", StartedAt: time.Now()}
	freshData, _ := json.Marshal(fresh)

	ft := newFakeTransport()
	ft.execResults[remoteStateCatCmd] = ExecResult{Stdout: string(data)}
	ft.execResults[pidAliveCmd(42)] = ExecResult{ExitCode: 1} // dead

	// After the launch command runs, the poll for the state file must see
	// the fresh content. The fake is call-order-agnostic (map keyed by exact
	// command), so make every subsequent "cat state file" call return the
	// fresh state by overwriting the same key the launch stage's poll loop
	// reads from.
	ft.execResults[launchServerCmd("0.8.85")] = ExecResult{ExitCode: 0}
	origExec := ft.execResults[remoteStateCatCmd]
	_ = origExec
	// Simulate "file now exists" by having a second fakeTransport wrapper
	// switch its answer after the launch call is observed.
	state, reused, err := EnsureRemoteServer(&pollAfterLaunchFake{fakeTransport: ft, freshStateJSON: string(freshData)}, "0.8.85")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reused {
		t.Error("expected reused=false")
	}
	if state != fresh {
		t.Errorf("got %+v, want %+v", state, fresh)
	}
}

// pollAfterLaunchFake wraps fakeTransport so that DiscoverServer's polling
// loop (in StartFreshServer) sees the "missing/dead" state until the launch
// command has been issued at least once, then sees the fresh state — without
// needing StartFreshServer to expose its retry internals to the test.
type pollAfterLaunchFake struct {
	*fakeTransport
	freshStateJSON string
	launched       bool
}

func (p *pollAfterLaunchFake) Exec(command string) (ExecResult, error) {
	if command == launchServerCmd("0.8.85") {
		p.launched = true
	}
	if command == remoteStateCatCmd && p.launched {
		return ExecResult{Stdout: p.freshStateJSON}, nil
	}
	return p.fakeTransport.Exec(command)
}

func TestFreeLocalPort(t *testing.T) {
	port, err := FreeLocalPort()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if port <= 0 || port > 65535 {
		t.Fatalf("got invalid port %d", port)
	}
	// The port must actually be free to bind immediately after.
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("port %d not actually free: %v", port, err)
	}
	ln.Close()
}
```

Add `"fmt"` to the test file's imports for `TestFreeLocalPort`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/remote/ -run 'TestDiscoverServer|TestServerAlive|TestEnsureRemoteServer|TestFreeLocalPort' -v`
Expected: FAIL (compile errors — none of `serve.go`'s symbols exist yet)

- [ ] **Step 3: Implement `internal/remote/serve.go`**

```go
package remote

import (
	"encoding/json"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/u007/ocode/internal/tool"
)

// ServeState mirrors internal/server's remoteServeState JSON shape — the
// wire contract for ~/.ocode/remote/serve.json. Field names/JSON tags must
// stay in sync with internal/server/server.go's remoteServeState; the two
// packages don't share a type (server can't import remote without a cycle:
// remote's CLI-side code is what drives server.Run in the first place).
type ServeState struct {
	PID       int       `json:"pid"`
	Port      int       `json:"port"`
	Token     string    `json:"token"`
	Version   string    `json:"version"`
	StartedAt time.Time `json:"startedAt"`
}

const remoteStateFilePath = "~/.ocode/remote/serve.json"

var remoteStateCatCmd = "cat " + shellQuotePath(remoteStateFilePath) + " 2>/dev/null || true"

// DiscoverServer reads and parses the remote state file. A missing file (no
// stdout), a read error, or a corrupt/partial JSON body all return
// ok=false — every one of those is "no reusable server," never a fatal
// connect error.
func DiscoverServer(t Transport) (ServeState, bool) {
	res, err := t.Exec(remoteStateCatCmd)
	if err != nil {
		return ServeState{}, false
	}
	out := strings.TrimSpace(res.Stdout)
	if out == "" {
		return ServeState{}, false
	}
	var state ServeState
	if err := json.Unmarshal([]byte(out), &state); err != nil {
		return ServeState{}, false
	}
	return state, true
}

func pidAliveCmd(pid int) string {
	return fmt.Sprintf("kill -0 %d 2>/dev/null", pid)
}

func healthProbeCmd(port int, token string) string {
	// Best-effort: curl ships on the overwhelming majority of target
	// systems ocode already requires (git, go toolchain era Linux/macOS).
	// A missing curl makes this probe report "000" (curl's own placeholder
	// for "no response"), which ServerAlive correctly treats as not-alive —
	// degrading to a fresh server start rather than failing the connect.
	return fmt.Sprintf(
		"curl -s -o /dev/null -w '%%{http_code}' -H %s http://127.0.0.1:%d/api/health",
		shellQuote("Authorization: Bearer "+token), port,
	)
}

// ServerAlive decides whether a discovered ServeState describes a server
// this client can reuse: the process must still be running, its version
// must match the connecting client's version exactly (a stale binary is
// never reused — same rule as the TUI's ActivateAndVerify), and it must
// actually answer /api/health with 200 (catches "process alive but HTTP
// stack wedged," and degrades gracefully when curl is unavailable).
func ServerAlive(t Transport, state ServeState, localVersion string) bool {
	if state.Version != localVersion {
		return false
	}
	if res, err := t.Exec(pidAliveCmd(state.PID)); err != nil || res.ExitCode != 0 {
		return false
	}
	res, err := t.Exec(healthProbeCmd(state.Port, state.Token))
	if err != nil {
		return false
	}
	return strings.TrimSpace(res.Stdout) == "200"
}

func launchServerCmd(ver string) string {
	remoteOcode := shellQuotePath(RemoteBinaryPath(ver))
	logPath := shellQuotePath("~/.ocode/remote/serve.log")
	// Redirect all three standard fds explicitly and disown the child: a
	// backgrounded process that still holds the ssh session's stdout/stderr
	// pipe open makes the outer non-interactive `ssh host cmd` hang waiting
	// for those fds to close, even after this shell returns. </dev/null
	// plus explicit redirects plus `disown` fully detaches it so `ssh`
	// returns as soon as this command's own shell exits.
	return fmt.Sprintf(
		"mkdir -p %s && nohup %s serve --remote --host 127.0.0.1 --port 0 </dev/null >%s 2>&1 & disown; echo launched",
		shellQuotePath("~/.ocode/remote"), remoteOcode, logPath,
	)
}

// serveStatePollInterval/Attempts bound how long StartFreshServer waits for
// the just-launched server to write its state file. Package-level vars
// (not consts) so tests can shrink them instead of taking seconds per run.
var (
	serveStatePollInterval = 250 * time.Millisecond
	serveStatePollAttempts = 40 // 10s total at the default interval
)

// StartFreshServer launches a detached `ocode serve --remote` on t and
// blocks until its state file appears (bounded by
// serveStatePollAttempts × serveStatePollInterval), returning the parsed
// state. Used when DiscoverServer finds nothing reusable.
func StartFreshServer(t Transport, ver string) (ServeState, error) {
	if _, err := t.Exec(launchServerCmd(ver)); err != nil {
		return ServeState{}, fmt.Errorf("launch remote server: %w", err)
	}
	for i := 0; i < serveStatePollAttempts; i++ {
		if state, ok := DiscoverServer(t); ok {
			return state, nil
		}
		time.Sleep(serveStatePollInterval)
	}
	return ServeState{}, fmt.Errorf("remote server did not write its state file within %s — check ~/.ocode/remote/serve.log on the remote", time.Duration(serveStatePollAttempts)*serveStatePollInterval)
}

// EnsureRemoteServer implements the reuse-vs-fresh decision table: discover
// → alive+matching-version → reuse; anything else (missing, dead,
// version-mismatched, unhealthy) → start fresh. reused reports which path
// was taken, for progress reporting.
func EnsureRemoteServer(t Transport, ver string) (state ServeState, reused bool, err error) {
	if existing, ok := DiscoverServer(t); ok && ServerAlive(t, existing, ver) {
		return existing, true, nil
	}
	fresh, err := StartFreshServer(t, ver)
	if err != nil {
		return ServeState{}, false, err
	}
	return fresh, false, nil
}

// FreeLocalPort asks the OS for an ephemeral free TCP port on 127.0.0.1 by
// binding to :0 and immediately releasing it — standard technique, with the
// usual (accepted) TOCTOU caveat that something else could grab it before
// the tunnel binds; ssh reports that failure directly and the caller
// retries once with a new port (see ConnectWeb, Task 7).
func FreeLocalPort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	_, portStr, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(portStr)
}

// StartTunnel starts `ssh -N -L localPort:127.0.0.1:remotePort <target>`
// under sup's supervision. It does not wait for the tunnel to establish or
// for it to exit — see ConnectWeb for the foreground supervise loop. Only
// meaningful for KindSSH targets; WSL never tunnels (Task 13).
func StartTunnel(sup *tool.ProcessSupervisor, target Target, localPort, remotePort int) (*exec.Cmd, error) {
	cmd := exec.Command("ssh", "-N", "-L", fmt.Sprintf("%d:127.0.0.1:%d", localPort, remotePort), target.String())
	if _, err := tool.StartSupervised(sup, cmd, tool.ProcessRegistration{
		ID:      "remote-tunnel",
		Name:    "ssh-tunnel",
		Command: cmd.String(),
		Kind:    tool.ProcessKindRemote,
	}); err != nil {
		return nil, err
	}
	return cmd, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/remote/ -run 'TestDiscoverServer|TestServerAlive|TestEnsureRemoteServer|TestFreeLocalPort' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/remote/serve.go internal/remote/serve_test.go
git commit -m "feat(remote): add serve-state discovery, reuse-vs-fresh launch, and tunnel helpers"
```

---

## Task 7: `ConnectWeb` — full web-mode connect flow

**Files:**
- Modify: `internal/remote/connect.go` (extract shared prepare stages; add `ConnectWeb`)
- Test: `internal/remote/connect_web_test.go`

**Interfaces:**
- Consumes: `runPrepareStages` (extracted here), `EnsureRemoteServer`/`StartTunnel`/`FreeLocalPort` (Task 6)
- Produces: `func ConnectWeb(opts ConnectOptions) error`. Consumed by Task 8 (`remotecli`).

- [ ] **Step 1: Refactor `Connect` to extract shared stages (no behavior change)**

In `connect.go`, extract stages 1-6 (reachability through credential sync) into a helper both `Connect` and `ConnectWeb` call. It owns and returns the `*tool.ProcessSupervisor` too, since `ConnectWeb` needs the same supervisor instance later to register the tunnel process:

```go
// runPrepareStages executes the shared reachability → platform-detect →
// ensure-binary → credential-sync stages (1-4 of the spec's numbering; TUI
// mode additionally runs multiplex-detect as its own stage 5, web mode
// never does — see 01-architecture.md "Session resume on disconnect").
// Returns the constructed Transport plus the supervisor it was built with,
// so ConnectWeb can register the tunnel process on the same supervisor
// (and Connect can pass it through to ExecInteractive unchanged, exactly
// as before this refactor). The caller owns shutting the supervisor down.
func runPrepareStages(opts ConnectOptions, progress *Progress) (Transport, *tool.ProcessSupervisor, error) {
	sup := tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{})
	transport, err := newTransportForTarget(opts.Target, sup)
	if err != nil {
		progress.Fail(err, "")
		return nil, sup, err
	}

	progress.Start("reachable", transport.Describe()+" reachable")
	if res, err := transport.Exec("true"); err != nil {
		progress.Fail(namedError("reachable", transport.Describe(), res.Stderr, err), "check the host, your ssh_config, and known_hosts")
		return nil, sup, err
	}
	progress.Done("")

	progress.Start("platform", "platform detect")
	goos, goarch, err := DetectPlatform(transport)
	if err != nil {
		progress.Fail(err, "")
		return nil, sup, err
	}
	progress.Done(goos + "/" + goarch)

	ver := version.Version
	if BinaryExists(transport, ver) {
		progress.Start("build", fmt.Sprintf("ocode v%s", ver))
		progress.Done("already installed")
	} else {
		progress.Start("build", fmt.Sprintf("building ocode v%s for %s/%s", ver, goos, goarch))
		build, err := PrepareLocalBuild(goos, goarch, opts.ModuleDir)
		if err != nil {
			progress.Fail(err, "install Go, or run from an ocode source checkout")
			return nil, sup, err
		}
		if !build.Reused {
			defer os.Remove(build.Path)
			progress.Done("cross-compiled")
		} else {
			progress.Done("reused local binary")
		}

		progress.Start("upload", "uploading")
		if err := UploadBinary(transport, ver, build.Path); err != nil {
			progress.Fail(err, "")
			return nil, sup, err
		}
		progress.Done("")

		progress.Start("verify", "installing + verifying")
		if err := ActivateAndVerify(transport, ver); err != nil {
			progress.Fail(err, "")
			return nil, sup, err
		}
		progress.Done("")

		_ = GCVersions(transport)
	}

	if opts.NoSync {
		progress.Start("sync", "credentials synced")
		progress.Warn("skipped (--no-sync)")
	} else if err := runSyncStage(progress, transport, opts.Target.String(), ver); err != nil {
		_ = err
	}

	return transport, sup, nil
}

// newTransportForTarget builds the Transport implementation for t.Kind,
// after validating the target is usable on this OS (KindWSL requires
// Windows — see target.go's validateTargetOS, added in Task 12).
func newTransportForTarget(t Target, sup *tool.ProcessSupervisor) (Transport, error) {
	if err := validateTargetOS(t.Kind, runtime.GOOS); err != nil {
		return nil, err
	}
	switch t.Kind {
	case KindWSL:
		return NewWSLTransport(t.Distro, sup), nil
	default:
		return NewSSHTransport(t, sup), nil
	}
}
```

Rewrite `Connect` to use it, keeping the original cleanup semantics (supervisor shut down 5s after the TUI exits, same as before this refactor):

```go
func Connect(opts ConnectOptions) error {
	out := opts.Out
	if out == nil {
		out = os.Stdout
	}
	if opts.Path == "" {
		return fmt.Errorf("internal error: Connect requires a resolved Path")
	}

	progress := NewProgress(out, fmt.Sprintf("Connecting to %s…", opts.Target.String()))
	transport, sup, err := runPrepareStages(opts, progress)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = sup.Shutdown(ctx)
	}()
	if err != nil {
		return err
	}

	progress.Start("multiplex", "checking for tmux/screen")
	mux := DetectMultiplexer(transport)
	if warn := ResumeWarning(mux); warn != "" {
		progress.Warn(warn)
	} else {
		progress.Done(mux.String())
	}

	progress.Start("launch", "launching remote TUI")
	remoteCmd := shellQuotePath(RemoteBinaryPath(version.Version)) + " " + shellQuotePath(opts.Path)
	launchCmd := WrapLaunch(mux, opts.Path, remoteCmd)
	progress.Done("")

	return transport.ExecInteractive(launchCmd)
}
```

`ConnectWeb` (Step 5 below) uses the same `runPrepareStages` return values, but does **not** defer `sup.Shutdown` immediately after prepare — it needs `sup` alive for the tunnel's whole lifetime, so its own `superviseTunnel` helper owns that shutdown instead (see Step 5).

- [ ] **Step 2: Run existing tests to confirm the refactor is behavior-preserving**

Run: `go test ./internal/remote/... -v`
Expected: PASS (no existing test exercises `Connect` end-to-end since it requires real ssh — this step is a compile/lint check plus the full unit suite for `provision.go`/`multiplex.go`/`sync.go`/`target.go`)

- [ ] **Step 3: Write the failing test for `ConnectWeb`**

`internal/remote/connect_web_test.go` — since `ConnectWeb` orchestrates real `os/exec` (ssh tunnel, browser open) end-to-end, unit-test the pieces it composes rather than the whole function; add one integration-shaped test gated the same way the spec's own "Integration (flag-gated)" tests are, using a build tag:

```go
package remote

import "testing"

// TestConnectWebRequiresResolvedPath mirrors Connect's own internal-error
// guard — cheap to test without any transport at all.
func TestConnectWebRequiresResolvedPath(t *testing.T) {
	err := ConnectWeb(ConnectOptions{Target: Target{Kind: KindSSH, Host: "h"}})
	if err == nil {
		t.Fatal("expected error for empty Path")
	}
}
```

- [ ] **Step 4: Run test to verify it fails**

Run: `go test ./internal/remote/ -run TestConnectWebRequiresResolvedPath -v`
Expected: FAIL (compile error: `ConnectWeb` undefined)

- [ ] **Step 5: Implement `ConnectWeb` in `connect.go`**

```go
// ConnectWebOptions extends ConnectOptions with web-mode-only knobs.
type ConnectWebOptions struct {
	ConnectOptions
	// OpenBrowser is called with the local URL to open once the tunnel (or,
	// for WSL, the direct localhost port) is ready. Defaults to the
	// platform opener; overridable in tests.
	OpenBrowser func(url string) error
}

// ConnectWeb runs the shared prepare stages, then discovers-or-launches a
// detached remote server, tunnels it to a local port (skipped for WSL —
// Windows forwards WSL2 localhost natively), and opens the browser with a
// one-time token in the URL fragment. Unlike Connect, it does not block on
// the remote process: it blocks supervising the local tunnel (SSH targets)
// until the user disconnects (Ctrl-C) or the tunnel dies. The remote server
// itself is a detached long-lived process and outlives this call by design.
func ConnectWeb(opts ConnectOptions) error {
	out := opts.Out
	if out == nil {
		out = os.Stdout
	}
	if opts.Path == "" {
		return fmt.Errorf("internal error: ConnectWeb requires a resolved Path")
	}

	progress := NewProgress(out, fmt.Sprintf("Connecting to %s (web)…", opts.Target.String()))
	transport, sup, err := runPrepareStages(opts, progress)
	if err != nil {
		return err
	}

	progress.Start("server", "discovering or starting remote server")
	state, reused, err := EnsureRemoteServer(transport, version.Version)
	if err != nil {
		progress.Fail(err, "check ~/.ocode/remote/serve.log on the remote")
		return err
	}
	if reused {
		progress.Done("reusing existing server")
	} else {
		progress.Done("started fresh")
	}

	if opts.Target.Kind == KindWSL {
		progress.Start("open", "opening browser")
		url := fmt.Sprintf("http://localhost:%d/#token=%s", state.Port, state.Token)
		if err := openBrowserURL(url); err != nil {
			progress.Fail(err, "")
			return err
		}
		progress.Done("")
		fmt.Fprintln(out, "Remote server running inside WSL; this command can now exit — the server keeps running.")
		return nil
	}

	progress.Start("tunnel", "opening SSH tunnel")
	localPort, tunnelCmd, err := startTunnelWithRetry(sup, opts.Target, state.Port)
	if err != nil {
		progress.Fail(err, "")
		return err
	}
	progress.Done(fmt.Sprintf("localhost:%d → remote:%d", localPort, state.Port))

	progress.Start("open", "opening browser")
	url := fmt.Sprintf("http://localhost:%d/#token=%s", localPort, state.Token)
	if err := openBrowserURL(url); err != nil {
		progress.Fail(err, "")
		return err
	}
	progress.Done("")

	fmt.Fprintln(out, "Tunnel active. Press Ctrl-C to close it (the remote server keeps running).")
	return superviseTunnel(sup, tunnelCmd)
}
```

Add the small supporting pieces (also in `connect.go`):

```go
// openBrowserURL opens url in the platform default browser. Duplicated
// (deliberately, it's five lines) rather than exported from
// internal/server, to avoid remotecli/remote depending on the server
// package for one helper.
func openBrowserURL(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		return fmt.Errorf("unsupported platform %q for opening a browser", runtime.GOOS)
	}
	return cmd.Start()
}

// startTunnelWithRetry picks a free local port and starts the tunnel; per
// the spec's error-handling table, a bind failure (port grabbed by another
// process between FreeLocalPort's probe and ssh's actual bind — an
// accepted, narrow TOCTOU race) gets exactly one retry with a fresh port
// before the failure is surfaced with ssh's own stderr.
func startTunnelWithRetry(sup *tool.ProcessSupervisor, target Target, remotePort int) (localPort int, tunnelCmd *exec.Cmd, err error) {
	for attempt := 0; attempt < 2; attempt++ {
		localPort, err = FreeLocalPort()
		if err != nil {
			return 0, nil, err
		}
		tunnelCmd, err = StartTunnel(sup, target, localPort, remotePort)
		if err == nil {
			return localPort, tunnelCmd, nil
		}
	}
	return 0, nil, fmt.Errorf("open ssh tunnel after retry: %w", err)
}

// superviseTunnel blocks until either the tunnel process exits on its own
// (reported as an error — the connection is gone) or the user sends
// SIGINT/SIGTERM (reported as nil — a clean, requested disconnect). The
// tunnel's own process group (StartSupervised via setProcGroup) is separate
// from this process's, so a terminal Ctrl-C does not reach it automatically
// — this signal handler explicitly kills it on the way out.
func superviseTunnel(sup *tool.ProcessSupervisor, tunnelCmd *exec.Cmd) error {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	waitCh := make(chan error, 1)
	go func() { waitCh <- tunnelCmd.Wait() }()

	select {
	case err := <-waitCh:
		if err != nil {
			return fmt.Errorf("tunnel closed unexpectedly: %w", err)
		}
		return fmt.Errorf("tunnel closed unexpectedly")
	case <-sigCh:
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = sup.Shutdown(ctx)
		<-waitCh
		return nil
	}
}
```

Add imports to `connect.go`: `"os/signal"`, `"runtime"`, `"syscall"`.

- [ ] **Step 6: Run test to verify it passes**

Run: `go test ./internal/remote/ -run TestConnectWebRequiresResolvedPath -v`
Expected: PASS

- [ ] **Step 7: Run the full package suite**

Run: `go test ./internal/remote/... -v`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add internal/remote/connect.go internal/remote/connect_web_test.go
git commit -m "feat(remote): implement ConnectWeb (discover/launch remote server, tunnel, open browser)"
```

---

## Task 8: `ocode remote --web` CLI flag

**Files:**
- Modify: `internal/remotecli/remotecli.go`
- Test: `internal/remotecli/remotecli_test.go`

**Interfaces:**
- Consumes: `remote.ConnectWeb` (Task 7)
- Produces: `ocode remote --web <target> [path] [--no-sync]` dispatches to `ConnectWeb` instead of `Connect`.

- [ ] **Step 1: Read the existing test file's conventions**

Run: `cat internal/remotecli/remotecli_test.go` to match its exact style (fake-based or arg-parsing-only tests) before writing new ones — `parseArgs` is very likely already unit-tested directly; extend that table rather than inventing a new pattern.

- [ ] **Step 2: Write the failing test**

```go
func TestParseArgsWebFlag(t *testing.T) {
	target, path, noSync, web, err := parseArgs([]string{"--web", "host", "/proj"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !web {
		t.Error("expected web=true")
	}
	if target.Host != "host" || path != "/proj" || noSync {
		t.Errorf("got target=%+v path=%q noSync=%v", target, path, noSync)
	}
}

func TestParseArgsWebFlagAnyPosition(t *testing.T) {
	_, _, _, web, err := parseArgs([]string{"host", "--web"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !web {
		t.Error("expected web=true regardless of flag position")
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/remotecli/ -run TestParseArgsWebFlag -v`
Expected: FAIL (compile error: `parseArgs` returns 4 values today, test expects 5)

- [ ] **Step 4: Implement**

Update `parseArgs`'s signature and body:

```go
func parseArgs(args []string) (target remote.Target, path string, noSync bool, web bool, err error) {
	var positional []string
	for _, a := range args {
		if a == "--no-sync" {
			noSync = true
			continue
		}
		if a == "--web" {
			web = true
			continue
		}
		if len(a) > 0 && a[0] == '-' {
			return remote.Target{}, "", false, false, fmt.Errorf("unknown flag %q (usage: ocode remote <[user@]host> [path] [--web] [--no-sync])", a)
		}
		positional = append(positional, a)
	}
	if len(positional) == 0 {
		return remote.Target{}, "", false, false, fmt.Errorf("usage: ocode remote <[user@]host> [path] [--web] [--no-sync]")
	}
	target, err = remote.ParseTarget(positional[0])
	if err != nil {
		return remote.Target{}, "", false, false, err
	}
	if len(positional) > 1 {
		path = positional[1]
	}
	if len(positional) > 2 {
		return remote.Target{}, "", false, false, fmt.Errorf("too many arguments (usage: ocode remote <[user@]host> [path] [--web] [--no-sync])")
	}
	return target, path, noSync, web, nil
}
```

Update `Run` to use the new return value and dispatch:

```go
func Run(args []string) error {
	target, path, noSync, web, err := parseArgs(args)
	if err != nil {
		return err
	}

	store, _, err := projects.NewStore()
	if err != nil {
		store = nil
	}

	hostKey := target.String()
	if path == "" {
		if store != nil {
			if p, ok := store.FindLastRemote(hostKey); ok {
				path = p.Path
			}
		}
		if path == "" {
			path = "~"
		}
	}

	connectOpts := remote.ConnectOptions{
		Target: target,
		Path:   path,
		NoSync: noSync,
		Out:    os.Stdout,
	}
	if web {
		err = remote.ConnectWeb(connectOpts)
	} else {
		err = remote.Connect(connectOpts)
	}

	if store != nil {
		_ = store.AddRemote(hostKey, path)
	}

	return err
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/remotecli/... -v`
Expected: PASS (including all pre-existing tests, since `parseArgs`'s signature changed — every existing caller/test of it must be updated to the 5-return-value form)

- [ ] **Step 6: Commit**

```bash
git add internal/remotecli/remotecli.go internal/remotecli/remotecli_test.go
git commit -m "feat(remotecli): add --web flag, dispatch to remote.ConnectWeb"
```

---

## Task 9: Frontend — URL fragment token bootstrap

**Files:**
- Modify: `web/src/api/client.ts` (~line 151-164)
- Test: `web/src/api/client.test.ts` (create if none exists for this module — check with `ls web/src/api/*.test.ts` first)

**Interfaces:**
- Produces: `_token` is now resolved from (in order) the URL fragment (`#token=...`, one-time, then stripped and cached), `sessionStorage` (persisted across reloads within the tab), then the existing `?token=` query string. New export `isRemoteSession(): boolean` — true iff the token came from the fragment/sessionStorage path (i.e., this tab is talking to a `--remote` server). Consumed by Task 10 (reconnect page) and Task 11 (WS subprotocol auth).

- [ ] **Step 1: Write the failing test**

```ts
import { describe, it, expect, beforeEach } from "vitest";

// client.ts reads window.location at module-eval time, so each test needs a
// fresh module instance with a fresh location/sessionStorage — vi.resetModules
// plus dynamic import per test.
describe("remote token bootstrap", () => {
  beforeEach(() => {
    sessionStorage.clear();
    window.history.replaceState(null, "", "/");
  });

  it("reads a fragment token, strips the fragment, and caches it in sessionStorage", async () => {
    window.history.replaceState(null, "", "/#token=abc123");
    const { authToken, isRemoteSession } = await import("./client?bootstrap-test-1");
    expect(authToken()).toBe("abc123");
    expect(isRemoteSession()).toBe(true);
    expect(window.location.hash).toBe("");
    expect(sessionStorage.getItem("ocode.remoteToken")).toBe("abc123");
  });

  it("falls back to a cached sessionStorage token on a plain reload", async () => {
    sessionStorage.setItem("ocode.remoteToken", "cached123");
    const { authToken, isRemoteSession } = await import("./client?bootstrap-test-2");
    expect(authToken()).toBe("cached123");
    expect(isRemoteSession()).toBe(true);
  });

  it("falls back to the existing ?token= query string when neither is present", async () => {
    window.history.replaceState(null, "", "/?token=rctoken");
    const { authToken, isRemoteSession } = await import("./client?bootstrap-test-3");
    expect(authToken()).toBe("rctoken");
    expect(isRemoteSession()).toBe(false);
  });
});
```

(The `?bootstrap-test-N` query suffixes on the dynamic import path force Vite/Vitest to treat each as a distinct module instance, since `_token` is computed once at module-eval time — check the project's existing convention for this by grepping `vi.resetModules` usage elsewhere in `web/src`; if the codebase already has a standard pattern for re-evaluating a module-level singleton per test, use that instead of the query-suffix trick.)

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && npx vitest run src/api/client.test.ts`
Expected: FAIL (`isRemoteSession` not exported; fragment not read at all today)

- [ ] **Step 3: Implement**

Replace the `_token` block in `client.ts`:

```ts
// Auth token resolution, in priority order:
//   1. URL fragment (#token=...) — set by `ocode remote --web`'s one-time
//      browser-open URL. Read once, cached to sessionStorage, then the
//      fragment is stripped via history.replaceState so it never survives
//      a copy-paste of the URL or shows up in browser history.
//   2. sessionStorage (ocode.remoteToken) — the fragment token, cached
//      across reloads within the same tab session.
//   3. ?token=... query string — the existing /rc (remote control) path.
// Fragments are never sent in HTTP requests, so (1)/(2) never reach server
// or proxy logs; (3) is a weaker, pre-existing mechanism kept for /rc.
const REMOTE_TOKEN_STORAGE_KEY = "ocode.remoteToken";

function resolveInitialToken(): { token: string; isRemote: boolean } {
  const hashParams = new URLSearchParams(window.location.hash.replace(/^#/, ""));
  const fragmentToken = hashParams.get("token");
  if (fragmentToken) {
    try {
      sessionStorage.setItem(REMOTE_TOKEN_STORAGE_KEY, fragmentToken);
    } catch {
      // sessionStorage unavailable (privacy mode, etc.) — the token still
      // works for this page load via the returned value; it just won't
      // survive a reload. Not fatal.
    }
    const url = new URL(window.location.href);
    url.hash = "";
    window.history.replaceState(null, "", url.toString());
    return { token: fragmentToken, isRemote: true };
  }

  let cached: string | null = null;
  try {
    cached = sessionStorage.getItem(REMOTE_TOKEN_STORAGE_KEY);
  } catch {
    cached = null;
  }
  if (cached) {
    return { token: cached, isRemote: true };
  }

  const queryToken = new URLSearchParams(window.location.search).get("token") ?? "";
  return { token: queryToken, isRemote: false };
}

const { token: _token, isRemote: _isRemoteSession } = resolveInitialToken();

/** True when this tab's token came from a `--remote` server's URL fragment
 *  (or its sessionStorage cache) rather than the legacy /rc ?token= path.
 *  Used to pick stricter, header-only auth for endpoints that also accept
 *  query-string tokens today (see TerminalPanel's WS connection). */
export function isRemoteSession(): boolean {
  return _isRemoteSession;
}

/** Returns auth headers for fetch() calls. Exported for components that use raw
 *  fetch or EventSource (which cannot set headers). */
export function authHeaders(): Record<string, string> {
  return _token ? { Authorization: `Bearer ${_token}` } : {};
}

/** Returns the auth token string. Useful for EventSource URLs. */
export function authToken(): string {
  return _token;
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd web && npx vitest run src/api/client.test.ts`
Expected: PASS

- [ ] **Step 5: Run the full frontend test suite for regressions**

Run: `cd web && npx vitest run`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add web/src/api/client.ts web/src/api/client.test.ts
git commit -m "feat(web): bootstrap remote-mode auth token from URL fragment"
```

---

## Task 10: Frontend — reconnect page on remote-session auth failure

**Files:**
- Create: `web/src/components/RemoteReconnect.tsx`
- Modify: `web/src/App.tsx` (top-level boot/render logic)
- Test: `web/src/components/RemoteReconnect.test.tsx`

**Interfaces:**
- Consumes: `isRemoteSession()`, `authToken()` (Task 9)
- Produces: a minimal full-page fallback rendered instead of the normal app shell when this is a remote session and the token is missing/invalid.

- [ ] **Step 1: Write the failing test**

```tsx
import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import RemoteReconnect from "./RemoteReconnect";

describe("RemoteReconnect", () => {
  it("renders a minimal reconnect message with no interactive app chrome", () => {
    render(<RemoteReconnect />);
    expect(screen.getByText(/reconnect/i)).toBeInTheDocument();
    expect(screen.getByText(/ocode remote/i)).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && npx vitest run src/components/RemoteReconnect.test.tsx`
Expected: FAIL (module doesn't exist)

- [ ] **Step 3: Implement `RemoteReconnect.tsx`**

```tsx
export default function RemoteReconnect() {
  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        alignItems: "center",
        justifyContent: "center",
        height: "100vh",
        gap: "0.75rem",
        fontFamily: "system-ui, sans-serif",
        textAlign: "center",
        padding: "2rem",
      }}
    >
      <h1 style={{ fontSize: "1.25rem", fontWeight: 600 }}>Session expired — reconnect from your terminal</h1>
      <p style={{ color: "#666", maxWidth: "32rem" }}>
        This tab's link to the remote ocode server is no longer valid. Run{" "}
        <code>ocode remote --web &lt;host&gt;</code> again to open a fresh, working tab.
      </p>
    </div>
  );
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd web && npx vitest run src/components/RemoteReconnect.test.tsx`
Expected: PASS

- [ ] **Step 5: Wire into `App.tsx`**

Find `App.tsx`'s top-level render/boot sequence (read the file first — it likely does an initial `authedFetch`/health-style call before rendering the main shell; match its existing pattern for a boot-time check rather than inventing a new one). Add, near the top of the component body, before the main render path:

```tsx
import { isRemoteSession, authToken } from "@/api/client";
import RemoteReconnect from "@/components/RemoteReconnect";

// ... inside the component, before the normal render:
if (isRemoteSession() && !authToken()) {
  return <RemoteReconnect />;
}
```

This covers the "missing token" half of the spec's requirement synchronously (no API round-trip needed — `isRemoteSession()` is only true if a fragment/cached token existed at some point, and `authToken()` is empty only if `resolveInitialToken` found nothing usable, e.g. sessionStorage was cleared). The "invalid token" half (server responds 401 despite a present token) is already handled by each API caller's existing `ApiError`/401 path — do **not** add a second, duplicate global 401 interceptor here; if `App.tsx` does not already have one, that is out of scope for this task (existing behavior for a 401 today is per-caller, and stays that way — this task only adds the synchronous no-token case).

- [ ] **Step 6: Run the frontend test suite**

Run: `cd web && npx vitest run`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add web/src/components/RemoteReconnect.tsx web/src/components/RemoteReconnect.test.tsx web/src/App.tsx
git commit -m "feat(web): show a minimal reconnect page when a remote session has no token"
```

---

## Task 11: Frontend — WebSocket subprotocol auth for the terminal panel in remote mode

**Files:**
- Modify: `web/src/components/Terminal/TerminalPanel.tsx` (~line 572-585)
- Test: `web/src/components/Terminal/TerminalPanel.test.tsx` (extend existing, or create if terminal WS construction isn't already covered — check first)

**Interfaces:**
- Consumes: `isRemoteSession()`, `authToken()` (Task 9)
- Produces: in remote mode, the terminal WS is opened with `new WebSocket(url, ["ocode.bearer.<token>"])` and no `?token=` query param; in non-remote mode, behavior is byte-for-byte unchanged.

- [ ] **Step 1: Read the current implementation**

Read `web/src/components/Terminal/TerminalPanel.tsx` lines 560-590 to get the exact surrounding variable names (`token`, `params`, `query`, `url`) before editing — the plan's Step 2 snippet below assumes those names based on this plan's earlier research; adjust to match if they differ.

- [ ] **Step 2: Write the failing test**

```tsx
import { describe, it, expect, vi, beforeEach } from "vitest";

vi.mock("@/api/client", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api/client")>();
  return { ...actual, isRemoteSession: vi.fn(), authToken: vi.fn() };
});

// Capture WebSocket construction args instead of opening a real socket.
class FakeWebSocket {
  static lastArgs: [string, string | string[] | undefined] | null = null;
  constructor(url: string, protocols?: string | string[]) {
    FakeWebSocket.lastArgs = [url, protocols];
  }
  close() {}
  send() {}
}

describe("terminal WS auth", () => {
  beforeEach(() => {
    FakeWebSocket.lastArgs = null;
    vi.stubGlobal("WebSocket", FakeWebSocket);
  });

  it("uses a Sec-WebSocket-Protocol bearer token and no ?token= query param in remote mode", async () => {
    const client = await import("@/api/client");
    (client.isRemoteSession as ReturnType<typeof vi.fn>).mockReturnValue(true);
    (client.authToken as ReturnType<typeof vi.fn>).mockReturnValue("tok123");

    // Exercise whatever exported function TerminalPanel uses to build the
    // WS connection — see the actual export name after Step 1's read;
    // this test targets that function directly rather than mounting the
    // full component, matching how connection-URL logic is unit-tested
    // elsewhere in this file (check for an existing `buildTerminalWsUrl`-
    // style helper before adding a new one).
    const { openTerminalSocket } = await import("./TerminalPanel");
    openTerminalSocket({ projectPath: "/proj" });

    expect(FakeWebSocket.lastArgs).not.toBeNull();
    const [url, protocols] = FakeWebSocket.lastArgs!;
    expect(url).not.toContain("token=");
    expect(protocols).toEqual(["ocode.bearer.tok123"]);
  });
});
```

Note: this test assumes an exported `openTerminalSocket` — if the existing code instead builds the socket inline inside a hook/component with no exported seam, the concrete Step 1 read determines the right refactor (extract a small pure function that takes the current params and returns `{url, protocols}`, so it's testable without mounting xterm). Do that extraction as part of Step 3 below rather than testing through the full component.

- [ ] **Step 3: Run test to verify it fails**

Run: `cd web && npx vitest run src/components/Terminal/TerminalPanel.test.tsx -t "terminal WS auth"`
Expected: FAIL (new helper doesn't exist yet)

- [ ] **Step 4: Implement**

Extract and modify the WS URL/protocol construction (replacing the existing inline block around the read lines):

```ts
import { isRemoteSession } from "@/api/client";

/** Builds the WebSocket connection args for /api/terminal/ws: the URL and,
 *  in remote mode, the Sec-WebSocket-Protocol list carrying the bearer
 *  token (server-side: internal/server's checkAuth + HandleTerminalWS,
 *  Task 5). Non-remote mode is unchanged: token travels as ?token= because
 *  that server never forbids it. Exported for unit testing without
 *  mounting the full xterm-backed component. */
export function buildTerminalWsConnection(opts: { projectPath?: string }): {
  url: string;
  protocols: string[] | undefined;
} {
  const params = new URLSearchParams();
  if (opts.projectPath) params.set("project_path", opts.projectPath);

  const token = authToken();
  let protocols: string[] | undefined;
  if (token && isRemoteSession()) {
    protocols = [`ocode.bearer.${token}`];
  } else if (token) {
    params.set("token", token);
  }

  const query = params.toString();
  const url = apiWsPath(`/api/terminal/ws${query ? `?${query}` : ""}`);
  return { url, protocols };
}
```

Update the call site to use it:

```ts
const { url, protocols } = buildTerminalWsConnection({ projectPath });
const sock = new WebSocket(url, protocols);
```

- [ ] **Step 5: Run test to verify it passes**

Run: `cd web && npx vitest run src/components/Terminal/TerminalPanel.test.tsx`
Expected: PASS

- [ ] **Step 6: Run the full frontend suite**

Run: `cd web && npx vitest run`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add web/src/components/Terminal/TerminalPanel.tsx web/src/components/Terminal/TerminalPanel.test.tsx
git commit -m "feat(web): use WebSocket subprotocol bearer auth for the terminal panel in remote mode"
```

---

## Task 12: `internal/remote/target.go` — accept `wsl:` targets, OS-gate check

**Files:**
- Modify: `internal/remote/target.go`
- Modify: `internal/remote/target_test.go`

**Interfaces:**
- Produces: `ParseTarget("wsl:Ubuntu")` / `ParseTarget("wsl:")` now succeed (pure syntax parsing — no OS check here); `func validateTargetOS(kind Kind, goos string) error`, called from `newTransportForTarget` (Task 7). Consumed by Task 13 (`wsl.go`) and Task 7's dispatch.

- [ ] **Step 1: Update the failing/obsolete tests first**

Replace `TestParseTargetWSLRejectedInPhase1` in `target_test.go`:

```go
func TestParseTargetWSL(t *testing.T) {
	cases := []struct {
		in         string
		wantDistro string
	}{
		{"wsl:Ubuntu", "Ubuntu"},
		{"wsl:", ""},
	}
	for _, c := range cases {
		got, err := ParseTarget(c.in)
		if err != nil {
			t.Fatalf("ParseTarget(%q): unexpected error: %v", c.in, err)
		}
		if got.Kind != KindWSL || got.Distro != c.wantDistro {
			t.Errorf("ParseTarget(%q) = %+v, want Kind=KindWSL Distro=%q", c.in, got, c.wantDistro)
		}
	}
}

func TestValidateTargetOS(t *testing.T) {
	if err := validateTargetOS(KindSSH, "linux"); err != nil {
		t.Errorf("ssh target should be valid on any OS, got %v", err)
	}
	if err := validateTargetOS(KindSSH, "windows"); err != nil {
		t.Errorf("ssh target should be valid on any OS, got %v", err)
	}
	if err := validateTargetOS(KindWSL, "windows"); err != nil {
		t.Errorf("wsl target should be valid on windows, got %v", err)
	}
	for _, goos := range []string{"linux", "darwin"} {
		if err := validateTargetOS(KindWSL, goos); err == nil {
			t.Errorf("wsl target should be rejected on %s", goos)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/remote/ -run 'TestParseTargetWSL|TestValidateTargetOS' -v`
Expected: FAIL — `TestParseTargetWSL` fails because `ParseTarget` still rejects `wsl:`; `TestValidateTargetOS` fails to compile (`validateTargetOS` undefined)

- [ ] **Step 3: Implement**

Replace the `wsl:` rejection branch in `ParseTarget`:

```go
func ParseTarget(s string) (Target, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Target{}, fmt.Errorf("remote target is required (usage: ocode remote <[user@]host> [path])")
	}

	if rest, ok := strings.CutPrefix(s, "wsl:"); ok {
		return Target{Kind: KindWSL, Distro: rest, Raw: s}, nil
	}

	if strings.Contains(s, "/") || strings.ContainsAny(s, " \t\n") {
		return Target{}, fmt.Errorf("invalid remote target %q: expected [user@]host", s)
	}
	// ... rest unchanged
}
```

Add near the bottom of `target.go`:

```go
// validateTargetOS enforces "wsl: targets are only valid when the local OS
// is Windows" without hard-coding runtime.GOOS, so it's unit-testable on
// any platform (there is no Windows CI — see 04-phase3-wsl.md's Testing
// section). Callers pass runtime.GOOS; only tests pass a literal.
func validateTargetOS(kind Kind, goos string) error {
	if kind == KindWSL && goos != "windows" {
		return fmt.Errorf("wsl targets are only supported when ocode is running on Windows (this machine is %s)", goos)
	}
	return nil
}
```

Also update `Kind`'s doc comment on `KindWSL` (`// KindWSL targets a local Windows Subsystem for Linux distro. Phase 1 rejects wsl: targets; Phase 3 implements the transport.`) to drop the now-stale "Phase 1 rejects" clause:

```go
	// KindWSL targets a local Windows Subsystem for Linux distro, launched
	// via wsl.exe. Only valid when ocode itself is running on Windows —
	// see validateTargetOS.
	KindWSL
```

And update the package doc comment at the top of `target.go` if it still says "(and, in a later phase, WSL)" — check `connect.go`'s package doc too (both may reference the old phasing language) and drop the "later phase" wording now that Phase 3 has landed.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/remote/ -run 'TestParseTarget|TestValidateTargetOS|TestTargetString' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/remote/target.go internal/remote/target_test.go
git commit -m "feat(remote): accept wsl: targets in ParseTarget, add OS-gate check"
```

---

## Task 13: `internal/remote/wsl.go` — WSL Transport implementation

**Files:**
- Create: `internal/remote/wsl.go`
- Create: `internal/remote/wsl_test.go`

**Interfaces:**
- Produces: `type WSLTransport struct{...}` implementing `Transport`; `func NewWSLTransport(distro string, sup *tool.ProcessSupervisor) *WSLTransport`. Consumed by Task 7's `newTransportForTarget` (already wired in Task 7 — this task makes that reference compile and work).

- [ ] **Step 1: Write the failing tests**

```go
package remote

import (
	"bytes"
	"testing"
)

func TestWSLTransportDescribe(t *testing.T) {
	if got := NewWSLTransport("Ubuntu", nil).Describe(); got != "wsl Ubuntu" {
		t.Errorf("got %q, want %q", got, "wsl Ubuntu")
	}
	if got := NewWSLTransport("", nil).Describe(); got != "wsl (default distro)" {
		t.Errorf("got %q, want %q", got, "wsl (default distro)")
	}
}

func TestWSLTransportExecCommandConstruction(t *testing.T) {
	// This test only verifies the *exec.Cmd shape (Path/Args), never
	// actually running wsl.exe — the package has no Windows CI (see
	// 04-phase3-wsl.md's Testing section), so exec.Command's argv is
	// asserted directly via wslExecArgs, the pure helper factored out for
	// exactly this reason.
	got := wslExecArgs("Ubuntu", "echo hi")
	want := []string{"-d", "Ubuntu", "--", "sh", "-c", "echo hi"}
	if !equalStrings(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}

	gotDefault := wslExecArgs("", "echo hi")
	wantDefault := []string{"--", "sh", "-c", "echo hi"}
	if !equalStrings(gotDefault, wantDefault) {
		t.Errorf("got %v, want %v", gotDefault, wantDefault)
	}
}

func TestWSLTransportInteractiveArgs(t *testing.T) {
	got := wslInteractiveArgs("Ubuntu", "ocode ~")
	want := []string{"-d", "Ubuntu", "--", "ocode", "~"}
	if !equalStrings(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func equalStrings(a, b []string) bool {
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

func TestWSLTransportCopyStreamsViaStdin(t *testing.T) {
	// Copy shells out for real (there's no fake exec.Cmd runner in this
	// package), so this test only runs where a "cat"-like receiver is
	// available to observe stdin — skip on Windows where wsl.exe itself
	// would need to exist. Use the transport's run() indirection isn't
	// exposed for interception, so instead assert the constructed command
	// via the same pure-args pattern as Exec/ExecInteractive.
	got := wslCopyArgs("Ubuntu", "~/.ocode/bin/0.1.0/.ocode.partial")
	want := []string{"-d", "Ubuntu", "--", "sh", "-c", "cat > " + shellQuotePath("~/.ocode/bin/0.1.0/.ocode.partial")}
	if !equalStrings(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

var _ = bytes.MinRead // placeholder import use removed once a real Copy content test is added if the platform allows it
```

(Drop the trailing `var _ = bytes.MinRead` line and the `"bytes"` import — it's a placeholder to remind the implementer there is no cross-platform way to test `Copy`'s actual byte-streaming without `wsl.exe` present; the three `*Args` pure-function tests are what's actually testable on any OS, matching the phase3 spec's own testing section verbatim: "Unit (run on any OS): wsl: target parsing, wsl command construction for Exec/ExecInteractive/Copy, non-Windows rejection, cache-key derivation.")

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/remote/ -run TestWSLTransport -v`
Expected: FAIL (compile error — `wsl.go` doesn't exist)

- [ ] **Step 3: Implement `internal/remote/wsl.go`**

```go
package remote

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync/atomic"

	"github.com/u007/ocode/internal/tool"
)

// WSLTransport implements Transport by shelling out to wsl.exe — only
// meaningful when ocode itself is running on Windows (enforced by
// validateTargetOS before a WSLTransport is ever constructed, not by this
// type itself). See docs/superpowers/specs/2026-08-29-remote-ssh/04-phase3-wsl.md.
type WSLTransport struct {
	// Distro names the target distro; "" means wsl.exe's default distro.
	Distro     string
	Supervisor *tool.ProcessSupervisor

	seq atomic.Int64
}

var _ Transport = (*WSLTransport)(nil)

func NewWSLTransport(distro string, sup *tool.ProcessSupervisor) *WSLTransport {
	return &WSLTransport{Distro: distro, Supervisor: sup}
}

func (w *WSLTransport) Describe() string {
	if w.Distro == "" {
		return "wsl (default distro)"
	}
	return "wsl " + w.Distro
}

func (w *WSLTransport) nextID(prefix string) string {
	return fmt.Sprintf("remote-wsl-%s-%d", prefix, w.seq.Add(1))
}

// run mirrors SSHTransport.run exactly (same supervisor bookkeeping
// contract) — duplicated rather than shared because the two types differ
// in exec.Command construction, and the run/wait/MarkExited plumbing is
// only five lines; factor out only if a third transport needs it too.
func (w *WSLTransport) run(cmd *exec.Cmd, kindLabel string) error {
	if w.Supervisor == nil {
		return cmd.Run()
	}
	id := w.nextID(kindLabel)
	if _, err := tool.StartSupervised(w.Supervisor, cmd, tool.ProcessRegistration{
		ID:      id,
		Name:    kindLabel,
		Command: cmd.String(),
		Kind:    tool.ProcessKindRemote,
	}); err != nil {
		return err
	}
	waitErr := cmd.Wait()
	if waitErr != nil {
		code := -1
		var exitErr *exec.ExitError
		if asExitError(waitErr, &exitErr) {
			code = exitErr.ExitCode()
		}
		w.Supervisor.MarkExited(id, code)
		return waitErr
	}
	w.Supervisor.MarkExited(id, 0)
	return nil
}

// wslExecArgs builds the wsl.exe argv for a non-interactive command,
// wrapping it in `sh -c` for the same reason ssh's "ssh host command" form
// does: the command string may itself contain shell operators (&&, quoting
// from shellQuotePath, etc.) that must be interpreted by a real shell
// inside the distro, not split by wsl.exe's own argv handling. Factored out
// (pure function, no exec.Cmd) so argv shape is unit-testable without
// wsl.exe present.
func wslExecArgs(distro, command string) []string {
	args := []string{}
	if distro != "" {
		args = append(args, "-d", distro)
	}
	return append(args, "--", "sh", "-c", command)
}

// wslInteractiveArgs builds the wsl.exe argv for ExecInteractive. Per the
// spec, this form deliberately does NOT wrap in `sh -c`: wsl.exe allocates
// the console/pty natively for a foreground command, and TUI launch
// commands built by WrapLaunch are already a single properly-shell-quoted
// string handed to the distro's default shell by wsl.exe itself.
func wslInteractiveArgs(distro, command string) []string {
	args := []string{}
	if distro != "" {
		args = append(args, "-d", distro)
	}
	return append(args, "--", command)
}

func wslCopyArgs(distro, destPath string) []string {
	return wslExecArgs(distro, "cat > "+shellQuotePath(destPath))
}

func (w *WSLTransport) Exec(command string) (ExecResult, error) {
	cmd := exec.Command("wsl.exe", wslExecArgs(w.Distro, command)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := w.run(cmd, "exec")
	res := ExecResult{Stdout: stdout.String(), Stderr: stderr.String()}
	if runErr == nil {
		res.ExitCode = 0
		return res, nil
	}
	var exitErr *exec.ExitError
	if asExitError(runErr, &exitErr) {
		res.ExitCode = exitErr.ExitCode()
		return res, fmt.Errorf("wsl %s %q: exit %d: %s", w.Describe(), command, res.ExitCode, stderr.String())
	}
	return res, fmt.Errorf("wsl %s %q: %w", w.Describe(), command, runErr)
}

func (w *WSLTransport) ExecStdin(command string, stdin io.Reader) (ExecResult, error) {
	cmd := exec.Command("wsl.exe", wslExecArgs(w.Distro, command)...)
	cmd.Stdin = stdin
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := w.run(cmd, "exec-stdin")
	res := ExecResult{Stdout: stdout.String(), Stderr: stderr.String()}
	if runErr == nil {
		res.ExitCode = 0
		return res, nil
	}
	var exitErr *exec.ExitError
	if asExitError(runErr, &exitErr) {
		res.ExitCode = exitErr.ExitCode()
		return res, fmt.Errorf("wsl %s %q: exit %d: %s", w.Describe(), command, res.ExitCode, stderr.String())
	}
	return res, fmt.Errorf("wsl %s %q: %w", w.Describe(), command, runErr)
}

func (w *WSLTransport) ExecInteractive(command string) error {
	cmd := exec.Command("wsl.exe", wslInteractiveArgs(w.Distro, command)...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return w.run(cmd, "tui")
}

// Copy streams src into destPath inside the distro via `cat > destPath`
// over wsl.exe's stdin — chosen over the \\wsl$\<distro>\... UNC path per
// the spec's explicit "pick one in implementation" latitude, since the
// stdin-stream approach reuses this file's existing Exec-style plumbing
// exactly (no path-translation code, no UNC-availability detection) and
// the spec calls UNC-with-stdin-fallback, not UNC-only.
func (w *WSLTransport) Copy(src io.Reader, size int64, destPath string) error {
	cmd := exec.Command("wsl.exe", wslCopyArgs(w.Distro, destPath)...)
	cmd.Stdin = src
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := w.run(cmd, "copy"); err != nil {
		return fmt.Errorf("wsl copy to %s (%s): %w: %s", destPath, humanBytes(size), err, stderr.String())
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/remote/ -run TestWSLTransport -v`
Expected: PASS

- [ ] **Step 5: Run the full package suite**

Run: `go test ./internal/remote/... -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/remote/wsl.go internal/remote/wsl_test.go
git commit -m "feat(remote): implement WSLTransport (Phase 3 transport swap)"
```

---

## Task 14: Wire WSL end-to-end through `Connect`/`ConnectWeb` and `remotecli`

**Files:**
- Modify: `internal/remote/connect.go` (verify `newTransportForTarget`'s `KindWSL` branch from Task 7 is exercised)
- Test: `internal/remote/connect_wsl_test.go`

**Interfaces:**
- Consumes: `newTransportForTarget` (Task 7), `WSLTransport` (Task 13), `validateTargetOS` (Task 12)

- [ ] **Step 1: Write the failing test**

```go
package remote

import "testing"

func TestNewTransportForTargetSelectsWSL(t *testing.T) {
	transport, err := newTransportForTarget(Target{Kind: KindWSL, Distro: "Ubuntu"}, nil)
	if runtime.GOOS != "windows" {
		if err == nil {
			t.Fatal("expected OS-gate error on non-Windows")
		}
		return
	}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wsl, ok := transport.(*WSLTransport)
	if !ok {
		t.Fatalf("expected *WSLTransport, got %T", transport)
	}
	if wsl.Distro != "Ubuntu" {
		t.Errorf("Distro = %q, want %q", wsl.Distro, "Ubuntu")
	}
}

func TestNewTransportForTargetSelectsSSH(t *testing.T) {
	transport, err := newTransportForTarget(Target{Kind: KindSSH, Host: "h"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := transport.(*SSHTransport); !ok {
		t.Fatalf("expected *SSHTransport, got %T", transport)
	}
}
```

Add `"runtime"` to the test file's imports.

- [ ] **Step 2: Run test to verify it fails (or passes vacuously on non-Windows CI)**

Run: `go test ./internal/remote/ -run TestNewTransportForTarget -v`
Expected: on non-Windows (this repo's CI), PASS immediately if Task 7's `newTransportForTarget` was implemented as specified — this task's real purpose is the explicit regression-test coverage for the dispatch-by-Kind behavior that Task 7 introduced inline without a dedicated test. If it fails, Task 7's implementation is incomplete — fix it there, not here.

- [ ] **Step 3: Run the full `internal/remote` suite**

Run: `go test ./internal/remote/... -v`
Expected: PASS — every test file in the package, confirming Phase 2 and Phase 3 changes coexist without regressing Phase 1 (`TestParseTarget`, `TestTargetString`, `TestDetectPlatform`, `TestGCVersions*`, `TestBuildSyncPayload`-style tests in `sync_test.go`, `TestWrapLaunch`-style tests in `multiplex_test.go`, `TestProgress*` in `progress_test.go`).

- [ ] **Step 4: Commit**

```bash
git add internal/remote/connect_wsl_test.go
git commit -m "test(remote): cover transport dispatch by Target.Kind (ssh vs wsl)"
```

---

## Task 15: Full-repo verification pass

**Files:** none (verification only)

- [ ] **Step 1: Run the full Go test suite**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: PASS, no vet warnings, no build failures

- [ ] **Step 2: Run the full frontend suite and typecheck**

Run: `cd web && npx vitest run && bun run typecheck` (or the project's pinned `tsgo`-based typecheck command per its CLAUDE.md — confirm the exact script name in `web/package.json` first)
Expected: PASS

- [ ] **Step 3: Manual smoke test (documented, not automated — no local SSH-reachable Linux/macOS box is assumed available in this environment)**

Record as a checklist in the PR description, to be run against a real reachable host before merge, per `04-phase3-wsl.md`'s own "Manual verification matrix... Recorded as a checklist in the PR" precedent:

- [ ] `ocode remote --web user@host` opens a browser tab; the terminal panel and chat both work against the remote filesystem.
- [ ] Closing the tab and re-running the same command reuses the existing remote server (`serve.json`'s `startedAt` is unchanged; no duplicate `ocode serve` process on the remote).
- [ ] Killing the tunnel (Ctrl-C in the terminal running `ocode remote --web`) leaves the remote server running; rerunning the command reconnects.
- [ ] A stale/corrupted `~/.ocode/remote/serve.json` on the remote is treated as missing (fresh server starts, old file is overwritten).
- [ ] On a Windows machine with WSL2 + a named distro: `ocode remote wsl:Ubuntu` opens the remote TUI; `ocode remote --web wsl:Ubuntu` opens the browser directly against `localhost:<port>` with no tunnel process.
- [ ] `ocode remote wsl:Ubuntu` on macOS/Linux fails immediately with the "only supported on Windows" error (no wsl.exe invocation attempted).

- [ ] **Step 4: Commit any fixes found during verification, then update `CHANGES.md`**

Follow the existing `CHANGES.md` entry style (see the "Web/desktop sharing and terminal links" entry near the top for the format: bold one-line title, prose paragraph, trailing file-path list in backticks). Add one entry summarizing Phase 2 + Phase 3 together.

```bash
git add CHANGES.md
git commit -m "docs: log ocode Remote Phase 2 (web mode) + Phase 3 (WSL) in CHANGES.md"
```
