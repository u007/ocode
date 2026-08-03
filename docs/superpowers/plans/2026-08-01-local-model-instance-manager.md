# Local Model Instance Manager Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let ocode run and manage local chat/completion model instances (starting with Bonsai 8B 1-bit) that LM Studio can't serve, with per-model enable/disable and a 1-or-2 concurrent-request-slot limit, via a new `/localmodel` slash command.

**Architecture:** Generalize the existing embedder-only local-server pattern (`internal/discovery/localserver.go`, `manifest.go`) into a small model-id-keyed instance registry (`internal/discovery/instances.go`) that reuses the same artifact-download, health-check, and supervised-spawn building blocks. A new `ServerManifest.Kind` field distinguishes "embed" (existing) from "chat" (new) manifests so the embedding-model picker doesn't see chat models and vice versa.

**Tech Stack:** Go (existing `internal/discovery`, `internal/config`, `internal/tui` packages), llama.cpp `llama-server` (`--parallel N` flag), `mlx_lm.server` (Apple Silicon only).

> **Implementation notes (as-built, supersede the above where they conflict):**
> - `mlx_lm.server` accepts **no** `--max-sequences`/`--parallel`/`--decode-concurrency` equivalent (the `--decode-concurrency` flag doesn't exist). `max_parallel` for MLX chat models is enforced **client-side** by a cross-process counting semaphore (`internal/agent/local_model_limiter.go` — `localConcurrencyTransport`, lock files under `<cache-dir>/locks/`), because the deterministic port assignment means every ocode process talks to the same physical server and a per-process semaphore would undercount.
> - `StartModelInstance` additionally serializes spawns with a per-model in-process mutex (`lockForStart`) and a cross-process `O_CREATE|O_EXCL` start-lock file (`acquireChatStartLock`, 6-min stale reclaim) so concurrent ocode processes can't double-spawn on the same port; lock losers adopt the winner via `waitForChatHealth`, and `OwnsModelInstance` distinguishes spawned-vs-adopted for `/localmodel limit`.
> - MLX chat spawns are forced offline (`HF_HUB_OFFLINE=1 TRANSFORMERS_OFFLINE=1`) so a cached model launches without mlx_lm.server's per-launch HF Hub update check (which 404/401s for non-public repos).
> - As decided in the design's non-goal: `/localmodel` entries are **not** listed in the model picker; select via `/model local/<id>` (warms the server). The TUI warm-up path is `localModelNeedsWarm` + `startLocalModelCmd(modelID, switchModel)`; a terminal `InstanceStopped` record must not suppress a retry warm-up.
> - `local/` clients and `/localmodel status` no longer trust the in-memory instance record's cached "ready" state: `NewClient` live-probes the port and rewrites the request's `model` to the manifest's `ExpectedServeID` (the bare suffix like "bonsai-8b-1bit" makes mlx_lm.server try to dynamically load a nonexistent repo on a healthy server); permission paths block on `EnsureLocalModelRunning` before building a client so a dead/cold-starting auto-permission model can't burn its retry budget on a dead port.
> - MLX chat models are pre-downloaded via `snapshot_download` (`local_files_only=True` first — a pure local cache check, network only when something is missing) before the offline launch; gated/rate-limited repos authenticate via new `/localmodel hf-token <token>` (stored in the auth credential store). Health poll raised to 900 attempts (15 min); start-lock stale-after raised to 50 min so a lock spanning download + boot is never stolen mid-flight.

## Global Constraints

- Reserved local-model chat ports: `11458-11465` inclusive (8 slots). Port for a given model id = `11458 + index of modelID in the sorted list of all registered chat model ids`. Exceeding 8 registered models is a hard error, no silent port reuse.
- "Max serving N" (N ∈ {1, 2}) means concurrent request slots on ONE process (`llama-server --parallel N` / `mlx_lm.server --max-sequences N`), never multiple OS processes per model.
- No wiring of a registered local chat model into the agent's active chat provider/model list — `/localmodel` only starts/stops/queries the server process directly.
- No config field, function, or slash-command subcommand beyond what a task below specifies — do not add flags, dry-run modes, or extra subcommands.
- Every new persisted config field goes through `withOcodeConfigLock` (load-modify-write) — never `SaveOcodeConfig(in-memory snapshot)` (see `internal/config/ocodeconfig.go:1111-1129` for the existing pattern).
- Follow existing package conventions exactly: `internal/discovery` has no test-framework beyond stdlib `testing`; `internal/tui/commands.go`'s `commandSpec` registration pattern; `internal/config/ocodeconfig.go`'s `*ConfigFile` mirror-struct + `apply*Config` + `writeOcodeConfigFile` payload pattern (see `ExternalPlugins` for the map[string]struct precedent at lines 125, 312, 643-649, 1167).

---

### Task 1: `ServerManifest.Kind` field + chat/embed manifest filtering

**Files:**
- Modify: `internal/discovery/manifest.go`
- Test: `internal/discovery/manifest_test.go` (create if it doesn't already exist — check first with `ls internal/discovery/*_test.go`)

**Interfaces:**
- Produces: `ServerManifest.Kind string` (zero value `""` == embed, for backward compatibility with all 4 existing entries), `func ChatManifestsForHost() []ServerManifest`, updated `func LocalManifestsForHost() []ServerManifest` (now embed-only).

- [ ] **Step 1: Write the failing test**

Add to `internal/discovery/manifest_test.go`:

```go
func TestLocalManifestsForHostExcludesChatKind(t *testing.T) {
	for _, m := range LocalManifestsForHost() {
		if m.Kind == "chat" {
			t.Fatalf("LocalManifestsForHost returned a chat-kind manifest: %s", m.ModelID)
		}
	}
}

func TestChatManifestsForHostOnlyChatKind(t *testing.T) {
	for _, m := range ChatManifestsForHost() {
		if m.Kind != "chat" {
			t.Fatalf("ChatManifestsForHost returned a non-chat manifest: %s (kind=%q)", m.ModelID, m.Kind)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/discovery/... -run 'TestLocalManifestsForHostExcludesChatKind|TestChatManifestsForHostOnlyChatKind' -v`
Expected: FAIL — `ChatManifestsForHost` undefined (compile error), since it doesn't exist yet.

- [ ] **Step 3: Add the `Kind` field and `ChatManifestsForHost`, update `LocalManifestsForHost`**

In `internal/discovery/manifest.go`, add to the `ServerManifest` struct (after the `ModelID` field, around line 36):

```go
	// Kind distinguishes embedding manifests ("" or "embed", the existing
	// default) from chat/completion manifests ("chat"). Embedding-model
	// pickers (LocalManifestsForHost) only see "" / "embed"; the local-model
	// instance manager (ChatManifestsForHost) only sees "chat".
	Kind string
```

Replace `LocalManifestsForHost` (lines 239-249) with:

```go
// LocalManifestsForHost returns every embedding manifest that can run on this
// host (used by the embedding-model picker to list selectable local models).
// Chat-kind manifests (see ChatManifestsForHost) are excluded.
func LocalManifestsForHost() []ServerManifest {
	var out []ServerManifest
	for _, m := range localManifests {
		if m.OS == runtime.GOOS && m.Arch == runtime.GOARCH && m.Kind != "chat" {
			out = append(out, m)
		}
	}
	return out
}

// ChatManifestsForHost returns every chat/completion manifest that can run on
// this host (used by /localmodel to list catalog entries available to add).
func ChatManifestsForHost() []ServerManifest {
	var out []ServerManifest
	for _, m := range localManifests {
		if m.OS == runtime.GOOS && m.Arch == runtime.GOARCH && m.Kind == "chat" {
			out = append(out, m)
		}
	}
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/discovery/... -run 'TestLocalManifestsForHostExcludesChatKind|TestChatManifestsForHostOnlyChatKind' -v`
Expected: PASS (both tests pass trivially since no chat-kind manifest exists yet — that's fine, Task 2 adds one and these tests then also exercise the split).

- [ ] **Step 5: Run the full discovery package test suite to check for regressions**

Run: `go test ./internal/discovery/... -v`
Expected: PASS (no existing test references `Kind`, so this is additive).

- [ ] **Step 6: Commit**

```bash
git add internal/discovery/manifest.go internal/discovery/manifest_test.go
git commit -m "feat(discovery): add ServerManifest.Kind to split embed/chat manifests"
```

---

### Task 2: Bonsai 8B 1-bit manifest entries

**Files:**
- Modify: `internal/discovery/manifest.go`
- Test: `internal/discovery/manifest_test.go`

**Interfaces:**
- Consumes: `ServerManifest.Kind` (Task 1), `BackendLlamaCpp` / `BackendMLX` constants (`internal/discovery/localserver.go:20-23`).
- Produces: `localManifests` entries with `ModelID: "local/bonsai-8b-1bit"` for `darwin/arm64` (MLX) and `darwin/amd64` + `linux/amd64` (llama.cpp GGUF). A new `MLXCompletionPath`/reuse of `HealthPath` is NOT introduced here — chat manifests use the existing `HealthPath` field for `/v1/models` and a new `CompletionPath` field for `/v1/chat/completions` (added in this task).

- [ ] **Step 1: Resolve the exact GGUF artifact (filename + SHA256) from the pasted HF repo**

The user provided `https://huggingface.co/prism-ml/Bonsai-8B-gguf` for non-Mac hosts. List its files via the public HF API (no auth needed for a public repo) and download+hash the appropriate quant:

```bash
curl -s https://huggingface.co/api/models/prism-ml/Bonsai-8B-gguf | python3 -m json.tool
```

Pick the `.gguf` file listed (if multiple quantizations exist, pick the one closest to the existing bge-m3 convention — a widely-compatible mid quant, e.g. one containing `Q4` or the repo's only file if there's just one). Then:

```bash
curl -L -o /tmp/bonsai-8b.gguf "https://huggingface.co/prism-ml/Bonsai-8B-gguf/resolve/main/<exact-filename-from-api-response>.gguf"
shasum -a 256 /tmp/bonsai-8b.gguf
```

Record the exact filename and the SHA256 output — both are needed in Step 3. Do not proceed with a guessed filename or hash.

- [ ] **Step 2: Write the failing test**

Add to `internal/discovery/manifest_test.go`:

```go
func TestBonsaiManifestResolvesOnEveryDeclaredPlatform(t *testing.T) {
	cases := []struct {
		os, arch, wantBackend string
	}{
		{"darwin", "arm64", BackendMLX},
		{"darwin", "amd64", BackendLlamaCpp},
		{"linux", "amd64", BackendLlamaCpp},
	}
	for _, c := range cases {
		found := false
		for _, m := range localManifests {
			if m.ModelID == "local/bonsai-8b-1bit" && m.OS == c.os && m.Arch == c.arch {
				found = true
				if m.Backend != c.wantBackend {
					t.Errorf("%s/%s: backend = %q, want %q", c.os, c.arch, m.Backend, c.wantBackend)
				}
				if m.Kind != "chat" {
					t.Errorf("%s/%s: Kind = %q, want \"chat\"", c.os, c.arch, m.Kind)
				}
				if m.CompletionPath != "/v1/chat/completions" {
					t.Errorf("%s/%s: CompletionPath = %q, want /v1/chat/completions", c.os, c.arch, m.CompletionPath)
				}
			}
		}
		if !found {
			t.Errorf("no local/bonsai-8b-1bit manifest for %s/%s", c.os, c.arch)
		}
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/discovery/... -run TestBonsaiManifestResolvesOnEveryDeclaredPlatform -v`
Expected: FAIL — `m.CompletionPath` undefined (compile error) and no matching manifests.

- [ ] **Step 4: Add `CompletionPath` field and the Bonsai manifest entries**

In `internal/discovery/manifest.go`, add to `ServerManifest` (next to `EmbedPath`, around line 52):

```go
	// CompletionPath is the OpenAI-compatible chat completions path (chat-kind
	// manifests only), e.g. "/v1/chat/completions".
	CompletionPath string
```

Append to `localManifests` (after the last entry, before the closing `}` at line 199):

```go
	{
		// Bonsai 8B, 1-bit (ternary/BitNet-style quant). MLX build on Apple
		// Silicon: prism-ml/Bonsai-8B-mlx-1bit, run via mlx_lm's own
		// OpenAI-compatible server (mlx_lm.server). --max-sequences is
		// mlx_lm.server's concurrent-request-slot flag, substituted from
		// {parallel} by the instance manager (internal/discovery/instances.go).
		OS: "darwin", Arch: "arm64",
		ModelID: "local/bonsai-8b-1bit", Kind: "chat", Backend: BackendMLX,
		MLXRepo: "prism-ml/Bonsai-8B-mlx-1bit",
		LaunchArgv: []string{"python3", "-m", "mlx_lm.server",
			"--model", "{repo}",
			"--host", "127.0.0.1",
			"--port", "{port}",
			"--max-sequences", "{parallel}"},
		HealthPath:     "/v1/models",
		CompletionPath: "/v1/chat/completions",
	},
	{
		// Bonsai 8B, 1-bit — macOS Intel, via llama.cpp GGUF
		// (prism-ml/Bonsai-8B-gguf). --parallel is llama-server's
		// concurrent-request-slot flag (aka -np), substituted from
		// {parallel} by the instance manager.
		OS: "darwin", Arch: "amd64",
		ModelID: "local/bonsai-8b-1bit", Kind: "chat", Backend: BackendLlamaCpp,
		Artifacts: []Artifact{
			{
				URL:     "https://github.com/ggml-org/llama.cpp/releases/download/b9777/llama-b9777-bin-macos-x64.tar.gz",
				SHA256:  "6271bffb4aa142351f63fff1cb8e42bd16e7b9877f2b5bc5e49037f91f3f0897",
				Dest:    "llama-server",
				Exec:    true,
				Archive: ArchiveGZ,
			},
			{
				URL:    "https://huggingface.co/prism-ml/Bonsai-8B-gguf/resolve/main/<FILL FROM STEP 1>.gguf",
				SHA256: "<FILL FROM STEP 1>",
				Dest:   "bonsai-8b-1bit.gguf",
			},
		},
		LaunchArgv: []string{"{bin}/llama-b9777/llama-server",
			"-m", "{bin}/bonsai-8b-1bit.gguf",
			"--port", "{port}",
			"--parallel", "{parallel}",
			"--host", "127.0.0.1"},
		HealthPath:     "/v1/models",
		CompletionPath: "/v1/chat/completions",
	},
	{
		// Bonsai 8B, 1-bit — Linux x86_64, via llama.cpp GGUF (same artifact
		// as the darwin/amd64 entry, different llama-server tarball).
		OS: "linux", Arch: "amd64",
		ModelID: "local/bonsai-8b-1bit", Kind: "chat", Backend: BackendLlamaCpp,
		Artifacts: []Artifact{
			{
				URL:     "https://github.com/ggml-org/llama.cpp/releases/download/b9777/llama-b9777-bin-ubuntu-x64.tar.gz",
				SHA256:  "f1994e1d9904f318c8347b000e7ef5dfd49fa4a24de044887da85d9bbfe84811",
				Dest:    "llama-server",
				Exec:    true,
				Archive: ArchiveGZ,
			},
			{
				URL:    "https://huggingface.co/prism-ml/Bonsai-8B-gguf/resolve/main/<FILL FROM STEP 1>.gguf",
				SHA256: "<FILL FROM STEP 1>",
				Dest:   "bonsai-8b-1bit.gguf",
			},
		},
		LaunchArgv: []string{"{bin}/llama-b9777/llama-server",
			"-m", "{bin}/bonsai-8b-1bit.gguf",
			"--port", "{port}",
			"--parallel", "{parallel}",
			"--host", "127.0.0.1"},
		HealthPath:     "/v1/models",
		CompletionPath: "/v1/chat/completions",
	},
```

Replace both `<FILL FROM STEP 1>` occurrences per platform with the real filename/SHA256 recorded in Step 1 before committing — this is not optional and the code must not ship with the literal placeholder text.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/discovery/... -run TestBonsaiManifestResolvesOnEveryDeclaredPlatform -v`
Expected: PASS.

- [ ] **Step 6: Run the full discovery package test suite**

Run: `go test ./internal/discovery/... -v`
Expected: PASS, including Task 1's `TestChatManifestsForHostOnlyChatKind` now exercising a real chat entry on the current host's platform.

- [ ] **Step 7: Commit**

```bash
git add internal/discovery/manifest.go internal/discovery/manifest_test.go
git commit -m "feat(discovery): add local/bonsai-8b-1bit manifest (MLX + llama.cpp GGUF)"
```

---

### Task 3: Instance registry (`internal/discovery/instances.go`)

**Files:**
- Create: `internal/discovery/instances.go`
- Test: `internal/discovery/instances_test.go`

**Interfaces:**
- Consumes: `ManifestForModel` / `ChatManifestsForHost` (Task 1/2), `ServerManifest` fields incl. `CompletionPath`, `shellQuote` / `libDirForBinary` / `probeLocalServerModel` / `modelMatches` / `EnsureArtifact` (all existing unexported/exported helpers already in package `discovery`, `internal/discovery/localserver.go`).
- Produces:
  - `const chatPortRangeStart = 11458` / `const chatPortRangeSize = 8`
  - `func AssignChatPort(modelID string, registeredIDs []string) (int, error)`
  - `type InstanceState string` with values `InstanceStopped`, `InstanceStarting`, `InstanceReady`
  - `type InstanceInfo struct { ModelID string; State InstanceState; Port int; MaxParallel int; BaseURL string }`
  - `func StartModelInstance(spawn func(cmdline string) error, modelID string, port int, maxParallel int, cacheDir string) error`
  - `func StopModelInstance(procs *tool.ProcessRegistry, modelID string) error`
  - `func GetModelInstance(modelID string) (InstanceInfo, bool)`

- [ ] **Step 1: Write the failing tests for port assignment**

Create `internal/discovery/instances_test.go`:

```go
package discovery

import "testing"

func TestAssignChatPortDeterministicBySortedID(t *testing.T) {
	ids := []string{"local/bonsai-8b-1bit", "local/aardvark-1b", "local/zeta-70b"}
	// Sorted: aardvark-1b(0), bonsai-8b-1bit(1), zeta-70b(2) -> ports 11458,11459,11460
	port, err := AssignChatPort("local/bonsai-8b-1bit", ids)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if port != chatPortRangeStart+1 {
		t.Fatalf("port = %d, want %d", port, chatPortRangeStart+1)
	}
}

func TestAssignChatPortUnknownModelErrors(t *testing.T) {
	ids := []string{"local/aardvark-1b"}
	if _, err := AssignChatPort("local/not-registered", ids); err == nil {
		t.Fatal("expected error for a modelID not present in registeredIDs")
	}
}

func TestAssignChatPortRejectsMoreThanRangeSize(t *testing.T) {
	ids := make([]string, chatPortRangeSize+1)
	for i := range ids {
		ids[i] = string(rune('a' + i))
	}
	if _, err := AssignChatPort(ids[chatPortRangeSize], ids); err == nil {
		t.Fatal("expected 'no free local-model port' error beyond the reserved range")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/discovery/... -run TestAssignChatPort -v`
Expected: FAIL — `AssignChatPort` undefined (package doesn't compile).

- [ ] **Step 3: Implement `instances.go` — port assignment + instance state map**

```go
package discovery

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/u007/ocode/internal/tool"
)

// chatPortRangeStart/chatPortRangeSize reserve 11458-11465 for locally-managed
// chat/completion model instances (separate from the embedder's fixed port
// 11457 in localserver.go). Port assignment is deterministic: sort all
// registered model ids and take start+index, so multiple ocode processes on
// the same machine agree on the same port for the same model without a
// shared allocation file.
const (
	chatPortRangeStart = 11458
	chatPortRangeSize  = 8
)

// AssignChatPort returns the deterministic port for modelID given the full set
// of currently-registered chat model ids (registeredIDs need not be
// pre-sorted). Errors if modelID is not present in registeredIDs, or if there
// are more than chatPortRangeSize registered ids (the reserved range is
// exhausted).
func AssignChatPort(modelID string, registeredIDs []string) (int, error) {
	if len(registeredIDs) > chatPortRangeSize {
		return 0, fmt.Errorf("no free local-model port: %d models registered, only %d ports reserved (%d-%d)",
			len(registeredIDs), chatPortRangeSize, chatPortRangeStart, chatPortRangeStart+chatPortRangeSize-1)
	}
	sorted := append([]string{}, registeredIDs...)
	sort.Strings(sorted)
	for i, id := range sorted {
		if id == modelID {
			return chatPortRangeStart + i, nil
		}
	}
	return 0, fmt.Errorf("model %q is not in the registered id set", modelID)
}

// InstanceState is the lifecycle state of one managed chat model instance.
type InstanceState string

const (
	InstanceStopped  InstanceState = "stopped"
	InstanceStarting InstanceState = "starting"
	InstanceReady    InstanceState = "ready"
)

// InstanceInfo is a point-in-time snapshot of one managed chat model instance.
type InstanceInfo struct {
	ModelID     string
	State       InstanceState
	Port        int
	MaxParallel int
	BaseURL     string
}

type chatInstance struct {
	info      InstanceInfo
	processID string // tool.Process.ID, empty when stopped
}

var (
	instMu   sync.Mutex
	instances = map[string]*chatInstance{}
)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/discovery/... -run TestAssignChatPort -v`
Expected: PASS.

- [ ] **Step 5: Write the failing test for start/stop/status lifecycle**

Add to `internal/discovery/instances_test.go`:

```go
func TestStartModelInstanceUnknownManifestErrors(t *testing.T) {
	spawn := func(string) error { return nil }
	err := StartModelInstance(spawn, "local/does-not-exist", 19999, 1, t.TempDir())
	if err == nil {
		t.Fatal("expected error for a model id with no chat manifest")
	}
}

func TestGetModelInstanceUnknownReturnsFalse(t *testing.T) {
	if _, ok := GetModelInstance("local/never-started"); ok {
		t.Fatal("expected ok=false for a model that was never started")
	}
}

func TestStopModelInstanceNotRunningIsNoop(t *testing.T) {
	procs := tool.NewProcessRegistry()
	if err := StopModelInstance(procs, "local/never-started"); err != nil {
		t.Fatalf("stopping a never-started instance should be a no-op, got: %v", err)
	}
}
```

- [ ] **Step 6: Run test to verify it fails**

Run: `go test ./internal/discovery/... -run 'TestStartModelInstanceUnknownManifestErrors|TestGetModelInstanceUnknownReturnsFalse|TestStopModelInstanceNotRunningIsNoop' -v`
Expected: FAIL — `StartModelInstance`, `GetModelInstance`, `StopModelInstance` undefined.

- [ ] **Step 7: Implement start/stop/get, reusing existing spawn/health-check helpers**

Append to `internal/discovery/instances.go`:

```go
// StartModelInstance resolves modelID's chat manifest, downloads any llama.cpp
// artifacts (idempotent, sha-pinned — same EnsureArtifact path as the
// embedder), and spawns it on port with maxParallel concurrent request slots
// via the supplied supervised-spawn function. Blocks until the health check
// passes or times out (mirrors EnsureLocalServer's 60x1s poll loop in
// localserver.go). Updates the in-process instance map on success.
func StartModelInstance(spawn func(cmdline string) error, modelID string, port int, maxParallel int, cacheDir string) error {
	man, ok := ManifestForModel(modelID)
	if !ok || man.Kind != "chat" {
		return fmt.Errorf("no local chat manifest for model %q on %s/%s", modelID, goos(), goarch())
	}

	instMu.Lock()
	instances[modelID] = &chatInstance{info: InstanceInfo{
		ModelID: modelID, State: InstanceStarting, Port: port, MaxParallel: maxParallel,
	}}
	instMu.Unlock()

	base := fmt.Sprintf("http://localhost:%d", port)
	var err error
	switch man.Backend {
	case BackendMLX:
		err = spawnMLXChatServer(spawn, man, port, maxParallel)
	default:
		err = spawnLlamaCppChatServer(spawn, man, cacheDir, port, maxParallel)
	}
	if err != nil {
		instMu.Lock()
		instances[modelID].info.State = InstanceStopped
		instMu.Unlock()
		return err
	}

	expect := man.ExpectedServeID()
	for i := 0; i < 60; i++ {
		if healthy, served := probeLocalServerModel(base, man.HealthPath, expect); healthy {
			if !modelMatches(served, expect) {
				instMu.Lock()
				instances[modelID].info.State = InstanceStopped
				instMu.Unlock()
				return fmt.Errorf("spawned chat server on %s serves %v, not %s", base, served, modelID)
			}
			instMu.Lock()
			instances[modelID].info.State = InstanceReady
			instances[modelID].info.BaseURL = base
			instMu.Unlock()
			return nil
		}
		time.Sleep(time.Second)
	}
	instMu.Lock()
	instances[modelID].info.State = InstanceStopped
	instMu.Unlock()
	return fmt.Errorf("local chat server for %q did not become healthy on %s", modelID, base)
}

// spawnLlamaCppChatServer mirrors spawnLlamaCppServer (localserver.go) but
// targets an arbitrary port + parallel-slot count instead of the embedder's
// fixed port, and skips --embeddings (chat manifests omit it in LaunchArgv).
func spawnLlamaCppChatServer(spawn func(cmdline string) error, man ServerManifest, cacheDir string, port, maxParallel int) error {
	binDir := filepath.Join(cacheDir, "local-"+man.OS+"-"+man.Arch)
	for _, a := range man.Artifacts {
		if err := EnsureArtifact(a, binDir); err != nil {
			return err
		}
	}
	argv := make([]string, len(man.LaunchArgv))
	var binPath string
	for i, a := range man.LaunchArgv {
		a = strings.ReplaceAll(a, "{bin}", binDir)
		a = strings.ReplaceAll(a, "{port}", fmt.Sprintf("%d", port))
		a = strings.ReplaceAll(a, "{parallel}", fmt.Sprintf("%d", maxParallel))
		if i == 0 {
			binPath = a
		}
		argv[i] = shellQuote(a)
	}
	libEnv := ""
	if libDir := libDirForBinary(binPath); libDir != "" {
		name := "LD_LIBRARY_PATH"
		if goos() == "darwin" {
			name = "DYLD_LIBRARY_PATH"
		}
		libEnv = name + "=" + shellQuote(libDir) + " "
	}
	cmdline := libEnv + strings.Join(argv, " ")
	emitUserDiscoveryDebug("DISCOVERY", "spawning local chat server: "+cmdline)
	if err := spawn(cmdline); err != nil {
		return fmt.Errorf("spawn local chat server: %w", err)
	}
	return nil
}

// spawnMLXChatServer spawns mlx_lm.server directly (no bundled script — chat
// manifests set LaunchArgv[0]="python3", unlike the embedder's MLX path which
// runs the bundled mlx_embed_server.py via {script}).
func spawnMLXChatServer(spawn func(cmdline string) error, man ServerManifest, port, maxParallel int) error {
	argv := make([]string, len(man.LaunchArgv))
	for i, a := range man.LaunchArgv {
		a = strings.ReplaceAll(a, "{repo}", man.MLXRepo)
		a = strings.ReplaceAll(a, "{port}", fmt.Sprintf("%d", port))
		a = strings.ReplaceAll(a, "{parallel}", fmt.Sprintf("%d", maxParallel))
		argv[i] = shellQuote(a)
	}
	cmdline := strings.Join(argv, " ")
	emitUserDiscoveryDebug("DISCOVERY", "spawning MLX chat server: "+cmdline)
	if err := spawn(cmdline); err != nil {
		return fmt.Errorf("spawn MLX chat server: %w", err)
	}
	return nil
}

// GetModelInstance returns the last-known snapshot for modelID, or false if it
// has never been started this process.
func GetModelInstance(modelID string) (InstanceInfo, bool) {
	instMu.Lock()
	defer instMu.Unlock()
	inst, ok := instances[modelID]
	if !ok {
		return InstanceInfo{}, false
	}
	return inst.info, true
}

// StopModelInstance kills modelID's running process (if any) via procs and
// clears the in-process instance record. No-op (not an error) if the model
// was never started or is already stopped.
func StopModelInstance(procs *tool.ProcessRegistry, modelID string) error {
	instMu.Lock()
	inst, ok := instances[modelID]
	if !ok || inst.processID == "" {
		instMu.Unlock()
		return nil
	}
	procID := inst.processID
	instMu.Unlock()

	if procs != nil {
		if _, err := procs.Kill(procID); err != nil {
			return fmt.Errorf("stop local chat server for %q: %w", modelID, err)
		}
	}
	instMu.Lock()
	delete(instances, modelID)
	instMu.Unlock()
	return nil
}
```

Add `"os"`, `"path/filepath"`, and `"time"` to the import block alongside the ones already listed in Step 3 (needed for `filepath.Join`, `time.Sleep`).

Note: `spawn` in `StartModelInstance`'s callers (Task 5) is the same `func(cmdline string) error` closure shape used by `ensureDiscovery` in `internal/agent/discovery_glue.go:60-72` — it must call `procs.StartBackground(cmdline)` and store the returned `*tool.Process.ID` into `instances[modelID].processID` so `StopModelInstance` can find it later. Since `StartModelInstance` only receives the `spawn` closure (not the `*tool.Process` it creates), have the Task 5 caller's `spawn` closure capture and stash the process ID itself, e.g.:

```go
var lastProcID string
spawn := func(cmdline string) error {
	p := procs.StartBackground(cmdline)
	if p != nil && p.SnapshotStatus() == tool.ProcExited {
		return fmt.Errorf("local chat server process exited immediately on spawn")
	}
	lastProcID = p.ID
	return nil
}
if err := discovery.StartModelInstance(spawn, modelID, port, maxParallel, cacheDir); err != nil {
	return err
}
discovery.SetModelInstanceProcessID(modelID, lastProcID) // see below
```

Add one more small exported function to `instances.go` for this handoff:

```go
// SetModelInstanceProcessID records the tool.ProcessRegistry id for a running
// instance so StopModelInstance can find it later. Called by the /localmodel
// command handler right after a successful StartModelInstance, since
// StartModelInstance itself only sees the spawn closure, not the *tool.Process
// it creates.
func SetModelInstanceProcessID(modelID, processID string) {
	instMu.Lock()
	defer instMu.Unlock()
	if inst, ok := instances[modelID]; ok {
		inst.processID = processID
	}
}
```

- [ ] **Step 8: Run test to verify it passes**

Run: `go test ./internal/discovery/... -run 'TestStartModelInstanceUnknownManifestErrors|TestGetModelInstanceUnknownReturnsFalse|TestStopModelInstanceNotRunningIsNoop' -v`
Expected: PASS.

- [ ] **Step 9: Run the full discovery package test suite**

Run: `go test ./internal/discovery/... -v`
Expected: PASS.

- [ ] **Step 10: Commit**

```bash
git add internal/discovery/instances.go internal/discovery/instances_test.go
git commit -m "feat(discovery): add local chat-model instance registry (port assignment, start/stop/status)"
```

---

### Task 4: Config — `LocalModels` map + savers

**Files:**
- Modify: `internal/config/ocodeconfig.go`
- Test: `internal/config/ocodeconfig_test.go`

**Interfaces:**
- Produces: `type LocalModelConfig struct { Enabled bool; MaxParallel int }`, `OcodeConfig.LocalModels map[string]LocalModelConfig`, `func SaveLocalModelConfig(modelID string, enabled bool, maxParallel int) error`, `func DeleteLocalModelConfig(modelID string) error`.

- [ ] **Step 1: Write the failing test**

Add to `internal/config/ocodeconfig_test.go` (follow the file's existing pattern of using a temp `GlobalDataDir` override — check an existing `TestSaveDiscoveryEnabled`-style test in that file for the exact setup helper name and copy its temp-dir/env-override boilerplate verbatim):

```go
func TestSaveLocalModelConfigRoundTrips(t *testing.T) {
	// NOTE: copy the temp-config-dir setup from an existing Save*Config test
	// in this file (e.g. TestSaveDiscoveryEnabled) so this test uses the same
	// isolated ocodeconfig.json path convention.

	if err := SaveLocalModelConfig("local/bonsai-8b-1bit", true, 2); err != nil {
		t.Fatalf("SaveLocalModelConfig: %v", err)
	}
	cfg, err := loadFullOcodeConfig()
	if err != nil {
		t.Fatalf("loadFullOcodeConfig: %v", err)
	}
	got, ok := cfg.LocalModels["local/bonsai-8b-1bit"]
	if !ok {
		t.Fatal("local/bonsai-8b-1bit not present after SaveLocalModelConfig")
	}
	if !got.Enabled || got.MaxParallel != 2 {
		t.Fatalf("got %+v, want Enabled=true MaxParallel=2", got)
	}
}

func TestDeleteLocalModelConfigRemovesEntry(t *testing.T) {
	if err := SaveLocalModelConfig("local/bonsai-8b-1bit", false, 1); err != nil {
		t.Fatalf("SaveLocalModelConfig: %v", err)
	}
	if err := DeleteLocalModelConfig("local/bonsai-8b-1bit"); err != nil {
		t.Fatalf("DeleteLocalModelConfig: %v", err)
	}
	cfg, err := loadFullOcodeConfig()
	if err != nil {
		t.Fatalf("loadFullOcodeConfig: %v", err)
	}
	if _, ok := cfg.LocalModels["local/bonsai-8b-1bit"]; ok {
		t.Fatal("local/bonsai-8b-1bit still present after DeleteLocalModelConfig")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/... -run 'TestSaveLocalModelConfigRoundTrips|TestDeleteLocalModelConfigRemovesEntry' -v`
Expected: FAIL — `SaveLocalModelConfig` / `DeleteLocalModelConfig` undefined (compile error).

- [ ] **Step 3: Add the `LocalModelConfig` type, `OcodeConfig.LocalModels` field, and file-struct plumbing**

In `internal/config/ocodeconfig.go`:

1. Add near `DiscoveryConfig` (around line 96, after its closing brace):

```go
// LocalModelConfig is one user-registered local chat/completion model
// instance (see internal/discovery/instances.go). MaxParallel is the number
// of concurrent request slots (1 or 2) the running server process is
// launched with.
type LocalModelConfig struct {
	Enabled     bool `json:"enabled"`
	MaxParallel int  `json:"max_parallel"`
}
```

2. Add to `OcodeConfig` struct (around line 127, next to `ExternalPlugins`):

```go
	// LocalModels holds registered local chat/completion model instances
	// (see /localmodel), keyed by model id (e.g. "local/bonsai-8b-1bit").
	// Distinct from Discovery, which only covers the embedding model.
	LocalModels map[string]LocalModelConfig
```

3. Add to `ocodeConfigFile` struct (around line 312, next to `ExternalPlugins`):

```go
	LocalModels map[string]LocalModelConfig `json:"local_models,omitempty"`
```

4. In `loadOcodeConfigFile` (around line 643, mirroring the `external_plugins` block exactly):

```go
	if _, ok := raw["local_models"]; ok {
		if cfg.LocalModels == nil {
			cfg.LocalModels = make(map[string]LocalModelConfig, len(file.LocalModels))
		}
		for id, lm := range file.LocalModels {
			cfg.LocalModels[id] = lm
		}
		delete(raw, "local_models")
	}
```

5. In `writeOcodeConfigFile` (around line 1167, next to the `external_plugins` conditional):

```go
	if len(cfg.LocalModels) > 0 {
		payload["local_models"] = cfg.LocalModels
	}
```

6. In the "known top-level keys" allowlist check (around line 1226 — the `if k == "compact" || ...` line), add `|| k == "local_models"` to the list so an unrecognized-key warning/error path (whatever that check does today — read the surrounding function before editing) doesn't flag it.

- [ ] **Step 4: Add the savers**

Add near `SaveLocalModelStatus` (around line 1441 in the current file, after it):

```go
// SaveLocalModelConfig persists (creating or updating) one registered local
// chat model's enabled flag and concurrent-slot limit, using load-modify-write
// so it cannot clobber a concurrent session's other config.
func SaveLocalModelConfig(modelID string, enabled bool, maxParallel int) error {
	return withOcodeConfigLock(func(cfg *OcodeConfig) error {
		if cfg.LocalModels == nil {
			cfg.LocalModels = map[string]LocalModelConfig{}
		}
		cfg.LocalModels[modelID] = LocalModelConfig{Enabled: enabled, MaxParallel: maxParallel}
		return nil
	})
}

// DeleteLocalModelConfig removes a registered local chat model entirely
// (distinct from disabling it — this forgets the MaxParallel setting too).
func DeleteLocalModelConfig(modelID string) error {
	return withOcodeConfigLock(func(cfg *OcodeConfig) error {
		delete(cfg.LocalModels, modelID)
		return nil
	})
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/config/... -run 'TestSaveLocalModelConfigRoundTrips|TestDeleteLocalModelConfigRemovesEntry' -v`
Expected: PASS.

- [ ] **Step 6: Run the full config package test suite**

Run: `go test ./internal/config/... -v`
Expected: PASS (no existing test touches `LocalModels`, purely additive).

- [ ] **Step 7: Commit**

```bash
git add internal/config/ocodeconfig.go internal/config/ocodeconfig_test.go
git commit -m "feat(config): add LocalModels map + SaveLocalModelConfig/DeleteLocalModelConfig"
```

---

### Task 5: `/localmodel` slash command

**Files:**
- Modify: `internal/tui/commands.go` (register command + thin `run*Cmd` wrapper, mirroring `/discover` at line 162 and `runDiscoverCmd` at line 1630)
- Modify: `internal/tui/model.go` (handler logic, mirroring `handleDiscoverCmd` at line 8047)
- Test: `internal/tui/command_test.go`

**Interfaces:**
- Consumes: `discovery.ChatManifestsForHost()`, `discovery.ManifestForModel()`, `discovery.AssignChatPort()`, `discovery.StartModelInstance()`, `discovery.StopModelInstance()`, `discovery.GetModelInstance()`, `discovery.SetModelInstanceProcessID()` (Task 3); `config.SaveLocalModelConfig()`, `config.DeleteLocalModelConfig()` (Task 4); `m.config.Ocode.LocalModels` (Task 4); `m.agent.Procs()` (`internal/agent/agent.go:3569`).
- Produces: `/localmodel list|add <name>|enable <name>|disable <name>|limit <name> <1|2>|status [name]`.

- [ ] **Step 1: Register the command**

In `internal/tui/commands.go`, add to the `commandSpecs` slice literal (near the `/discover` entry, line 162):

```go
	{name: "/localmodel", usage: "/localmodel list|add <name>|enable <name>|disable <name>|limit <name> <1|2>|status [name]",
		help: "Manage locally-run chat/completion model instances (e.g. Bonsai 8B 1-bit) that LM Studio can't serve",
		handler: runLocalModelCmd},
```

Add the thin wrapper near `runDiscoverCmd` (line 1630):

```go
func runLocalModelCmd(m *model, args []string) tea.Cmd {
	return m.handleLocalModelCmd(args)
}
```

- [ ] **Step 2: Write the failing test for `list` (no models registered)**

Add to `internal/tui/command_test.go` (use the same `newTestModel`/setup helper an existing command test in this file uses — check `TestCommandHelpTextShowsAliasesAndArgs` at line 426 or another `/discover`-adjacent test for the exact constructor name and copy its boilerplate):

```go
func TestLocalModelListEmptyShowsCatalogOnly(t *testing.T) {
	m := newTestModel(t) // replace with this file's actual test-model constructor
	m.handleLocalModelCmd([]string{"list"})
	if len(m.messages) == 0 {
		t.Fatal("expected a message listing the catalog")
	}
	last := m.messages[len(m.messages)-1].text
	if !strings.Contains(last, "bonsai-8b-1bit") {
		t.Fatalf("expected catalog entry bonsai-8b-1bit in output, got: %s", last)
	}
}

func TestLocalModelLimitRejectsInvalidValue(t *testing.T) {
	m := newTestModel(t)
	m.handleLocalModelCmd([]string{"limit", "local/bonsai-8b-1bit", "3"})
	last := m.messages[len(m.messages)-1].text
	if !strings.Contains(last, "1") || !strings.Contains(last, "2") {
		t.Fatalf("expected error mentioning valid values 1/2, got: %s", last)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/tui/... -run 'TestLocalModelListEmptyShowsCatalogOnly|TestLocalModelLimitRejectsInvalidValue' -v`
Expected: FAIL — `handleLocalModelCmd` undefined (compile error).

- [ ] **Step 4: Implement `handleLocalModelCmd` and its subcommand helpers**

Add to `internal/tui/model.go` (near `handleDiscoverCmd`, after line 8171):

```go
// handleLocalModelCmd implements /localmodel list|add|enable|disable|limit|status.
func (m *model) handleLocalModelCmd(args []string) tea.Cmd {
	if len(args) == 0 {
		m.messages = append(m.messages, message{role: roleAssistant, text: "Usage: /localmodel list|add <name>|enable <name>|disable <name>|limit <name> <1|2>|status [name]"})
		return nil
	}
	switch strings.ToLower(args[0]) {
	case "list":
		m.showLocalModelList()
	case "add":
		if len(args) < 2 {
			m.messages = append(m.messages, message{role: roleAssistant, text: "Usage: /localmodel add <name>"})
			return nil
		}
		m.localModelAdd(args[1])
	case "enable":
		if len(args) < 2 {
			m.messages = append(m.messages, message{role: roleAssistant, text: "Usage: /localmodel enable <name>"})
			return nil
		}
		m.localModelSetEnabled(args[1], true)
	case "disable":
		if len(args) < 2 {
			m.messages = append(m.messages, message{role: roleAssistant, text: "Usage: /localmodel disable <name>"})
			return nil
		}
		m.localModelSetEnabled(args[1], false)
	case "limit":
		if len(args) < 3 {
			m.messages = append(m.messages, message{role: roleAssistant, text: "Usage: /localmodel limit <name> <1|2>"})
			return nil
		}
		m.localModelSetLimit(args[1], args[2])
	case "status":
		name := ""
		if len(args) > 1 {
			name = args[1]
		}
		m.showLocalModelStatus(name)
	default:
		m.messages = append(m.messages, message{role: roleAssistant, text: "Usage: /localmodel list|add <name>|enable <name>|disable <name>|limit <name> <1|2>|status [name]"})
	}
	return nil
}

// localModelID resolves a bare catalog name (e.g. "bonsai-8b-1bit") or a full
// model id (e.g. "local/bonsai-8b-1bit") to the manifest's ModelID.
func localModelID(name string) string {
	if strings.HasPrefix(name, "local/") {
		return name
	}
	return "local/" + name
}

func (m *model) showLocalModelList() {
	var b strings.Builder
	b.WriteString("Local model catalog:\n")
	for _, man := range discovery.ChatManifestsForHost() {
		status := "not added"
		if lm, ok := m.config.Ocode.LocalModels[man.ModelID]; ok {
			state := "disabled"
			if lm.Enabled {
				state = "enabled"
				if inst, ok := discovery.GetModelInstance(man.ModelID); ok {
					state = string(inst.State)
				}
			}
			status = fmt.Sprintf("%s, max_parallel=%d", state, lm.MaxParallel)
		}
		fmt.Fprintf(&b, "  %s (%s)\n", man.ModelID, status)
	}
	m.messages = append(m.messages, message{role: roleAssistant, text: b.String()})
}

func (m *model) localModelAdd(name string) {
	id := localModelID(name)
	man, ok := discovery.ManifestForModel(id)
	if !ok || man.Kind != "chat" {
		m.messages = append(m.messages, message{role: roleAssistant, text: "No local chat model catalog entry for " + id + " on this platform"})
		return
	}
	if _, exists := m.config.Ocode.LocalModels[id]; exists {
		m.messages = append(m.messages, message{role: roleAssistant, text: id + " is already registered"})
		return
	}
	if err := config.SaveLocalModelConfig(id, false, 1); err != nil {
		m.messages = append(m.messages, message{role: roleAssistant, text: "Error: " + err.Error()})
		return
	}
	if m.config.Ocode.LocalModels == nil {
		m.config.Ocode.LocalModels = map[string]config.LocalModelConfig{}
	}
	m.config.Ocode.LocalModels[id] = config.LocalModelConfig{Enabled: false, MaxParallel: 1}
	m.messages = append(m.messages, message{role: roleAssistant, text: id + " registered (disabled). Enable with /localmodel enable " + id})
}

func (m *model) registeredLocalModelIDs() []string {
	ids := make([]string, 0, len(m.config.Ocode.LocalModels))
	for id := range m.config.Ocode.LocalModels {
		ids = append(ids, id)
	}
	return ids
}

func (m *model) localModelSetEnabled(name string, enabled bool) {
	id := localModelID(name)
	lm, exists := m.config.Ocode.LocalModels[id]
	if !exists {
		m.messages = append(m.messages, message{role: roleAssistant, text: id + " is not registered. Run /localmodel add " + name + " first"})
		return
	}
	if enabled == lm.Enabled {
		m.messages = append(m.messages, message{role: roleAssistant, text: id + " is already " + map[bool]string{true: "enabled", false: "disabled"}[enabled]})
		return
	}
	if !enabled {
		if m.agent != nil {
			if err := discovery.StopModelInstance(m.agent.Procs(), id); err != nil {
				m.messages = append(m.messages, message{role: roleAssistant, text: "Error stopping " + id + ": " + err.Error()})
				return
			}
		}
		lm.Enabled = false
		if err := config.SaveLocalModelConfig(id, false, lm.MaxParallel); err != nil {
			m.messages = append(m.messages, message{role: roleAssistant, text: "Error: " + err.Error()})
			return
		}
		m.config.Ocode.LocalModels[id] = lm
		m.messages = append(m.messages, message{role: roleAssistant, text: id + ": disabled"})
		return
	}
	if err := m.startLocalModelInstance(id, lm.MaxParallel); err != nil {
		m.messages = append(m.messages, message{role: roleAssistant, text: "Error starting " + id + ": " + err.Error()})
		return
	}
	lm.Enabled = true
	if err := config.SaveLocalModelConfig(id, true, lm.MaxParallel); err != nil {
		m.messages = append(m.messages, message{role: roleAssistant, text: "Error: " + err.Error()})
		return
	}
	m.config.Ocode.LocalModels[id] = lm
	m.messages = append(m.messages, message{role: roleAssistant, text: id + ": enabled"})
}

// startLocalModelInstance resolves this model's deterministic port and spawns
// it through the agent's supervised process registry, mirroring the spawn
// closure shape used by ensureDiscovery (internal/agent/discovery_glue.go:60-72).
func (m *model) startLocalModelInstance(id string, maxParallel int) error {
	if m.agent == nil {
		return fmt.Errorf("no active agent to spawn the local model process")
	}
	port, err := discovery.AssignChatPort(id, m.registeredLocalModelIDs())
	if err != nil {
		return err
	}
	procs := m.agent.Procs()
	var lastProcID string
	spawn := func(cmdline string) error {
		p := procs.StartBackground(cmdline)
		if p != nil && p.SnapshotStatus() == tool.ProcExited {
			return fmt.Errorf("local chat server process exited immediately on spawn")
		}
		lastProcID = p.ID
		return nil
	}
	if err := discovery.StartModelInstance(spawn, id, port, maxParallel, discoveryCacheDir()); err != nil {
		return err
	}
	discovery.SetModelInstanceProcessID(id, lastProcID)
	return nil
}

func (m *model) localModelSetLimit(name, valueStr string) {
	id := localModelID(name)
	value, err := strconv.Atoi(valueStr)
	if err != nil || (value != 1 && value != 2) {
		m.messages = append(m.messages, message{role: roleAssistant, text: "Invalid limit " + valueStr + " — must be 1 or 2"})
		return
	}
	lm, exists := m.config.Ocode.LocalModels[id]
	if !exists {
		m.messages = append(m.messages, message{role: roleAssistant, text: id + " is not registered. Run /localmodel add " + name + " first"})
		return
	}
	wasEnabled := lm.Enabled
	if wasEnabled && m.agent != nil {
		if err := discovery.StopModelInstance(m.agent.Procs(), id); err != nil {
			m.messages = append(m.messages, message{role: roleAssistant, text: "Error stopping " + id + " to apply new limit: " + err.Error()})
			return
		}
	}
	lm.MaxParallel = value
	if wasEnabled {
		if err := m.startLocalModelInstance(id, value); err != nil {
			m.messages = append(m.messages, message{role: roleAssistant, text: "Error restarting " + id + " with new limit: " + err.Error()})
			return
		}
	}
	if err := config.SaveLocalModelConfig(id, lm.Enabled, value); err != nil {
		m.messages = append(m.messages, message{role: roleAssistant, text: "Error: " + err.Error()})
		return
	}
	m.config.Ocode.LocalModels[id] = lm
	m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("%s: max_parallel set to %d", id, value)})
}

func (m *model) showLocalModelStatus(name string) {
	var b strings.Builder
	ids := []string{}
	if name != "" {
		ids = append(ids, localModelID(name))
	} else {
		ids = m.registeredLocalModelIDs()
		sort.Strings(ids)
	}
	if len(ids) == 0 {
		m.messages = append(m.messages, message{role: roleAssistant, text: "No local models registered. Run /localmodel add <name>."})
		return
	}
	for _, id := range ids {
		lm, exists := m.config.Ocode.LocalModels[id]
		if !exists {
			fmt.Fprintf(&b, "%s: not registered\n", id)
			continue
		}
		fmt.Fprintf(&b, "%s\n  enabled: %v\n  max_parallel: %d\n", id, lm.Enabled, lm.MaxParallel)
		if inst, ok := discovery.GetModelInstance(id); ok {
			fmt.Fprintf(&b, "  state: %s\n  port: %d\n  base_url: %s\n", inst.State, inst.Port, inst.BaseURL)
		} else {
			fmt.Fprintf(&b, "  state: %s\n", discovery.InstanceStopped)
		}
	}
	m.messages = append(m.messages, message{role: roleAssistant, text: b.String()})
}
```

Add `"github.com/u007/ocode/internal/tool"` to `model.go`'s import block if not already present (check first — `internal/tui/commands.go` already imports several `internal/*` packages, `model.go` likely already imports `internal/agent` and may already import `internal/tool` transitively-visible symbols; verify with `grep -n '"github.com/u007/ocode/internal/tool"' internal/tui/model.go` before adding a duplicate import).

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/tui/... -run 'TestLocalModelListEmptyShowsCatalogOnly|TestLocalModelLimitRejectsInvalidValue' -v`
Expected: PASS.

- [ ] **Step 6: Run the full tui package test suite**

Run: `go test ./internal/tui/... -v`
Expected: PASS (check output carefully for any pre-existing test that enumerates all registered `commandSpecs` names/count and may need updating to include `/localmodel` — search first: `grep -n "commandSpecs\b" internal/tui/*_test.go`).

- [ ] **Step 7: Commit**

```bash
git add internal/tui/commands.go internal/tui/model.go internal/tui/command_test.go
git commit -m "feat(tui): add /localmodel command (list/add/enable/disable/limit/status)"
```

---

### Task 6: End-to-end build + full test suite verification

**Files:** none (verification only).

- [ ] **Step 1: Full build**

Run: `go build ./...`
Expected: no errors.

- [ ] **Step 2: Full test suite**

Run: `go test ./... -v 2>&1 | tail -100`
Expected: all packages PASS. If `internal/discovery` or `internal/tui` show failures unrelated to this feature, stop and report them rather than silently proceeding — do not touch unrelated failing tests without asking.

- [ ] **Step 3: Manual smoke test on the current host (documented, not automated — Bonsai's actual model weights are multi-GB and downloading them in CI/every test run would be both slow and a real download of live network resources)**

If running on an Apple Silicon Mac with `mlx-lm` pip-installed and `python3` on PATH:

```bash
go run ./cmd/ocode  # or the project's normal launch command — check README/AGENTS.md if unsure
# inside the TUI:
/localmodel list
/localmodel add bonsai-8b-1bit
/localmodel enable bonsai-8b-1bit   # this downloads/loads the real model — expect it to take a while
/localmodel status bonsai-8b-1bit
/localmodel limit bonsai-8b-1bit 2
/localmodel disable bonsai-8b-1bit
```

Confirm each command's output matches the design (list shows the catalog entry, add registers it disabled, enable transitions to `ready` after the model loads, status reports port/base_url, limit restarts and reports the new value, disable stops the process). Report the actual output back rather than assuming success.

- [ ] **Step 4: Commit any fixups discovered during manual smoke testing**

If Step 3 surfaces a bug, fix it, re-run the affected unit tests, and commit as a normal fixup commit (not squashed into earlier task commits — this repo's convention per CLAUDE.md is new commits, not amends).

---

## Self-Review Notes

- **Spec coverage:** Problem/non-goals (Tasks 3-5 respect them: no multi-process load-balancing, no chat-provider wiring, no GUI, catalog-only `add`), Bonsai artifact (Task 2), architecture reuse (Tasks 3 reuses `shellQuote`/`libDirForBinary`/`probeLocalServerModel`/`EnsureArtifact`), instance registry + config + slash command (Tasks 3-5), error handling (every subcommand handler prints the error directly, no fallback), testing (a test step in every task) — all spec sections have a task.
- **Placeholder scan:** The two `<FILL FROM STEP 1>` markers in Task 2 are intentional and flagged as non-optional, tied to an actual preceding step that produces the real values (a real SHA256 cannot be fabricated without downloading the file) — not a deferred "TBD".
- **Type consistency:** `InstanceState`/`InstanceInfo`/`AssignChatPort`/`StartModelInstance`/`StopModelInstance`/`GetModelInstance`/`SetModelInstanceProcessID` (Task 3) are used with identical names and signatures in Task 5's `model.go` additions. `LocalModelConfig{Enabled, MaxParallel}` (Task 4) matches its usage in Task 5.
