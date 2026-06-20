# Part 13 — Shared Local Model-Server Backend (MLX on Apple Silicon)

A single local embedding server per machine, spawned via ocode's process supervisor,
shared by the main agent and all sub-agents (and opportunistically across ocode
processes via probe-first). On Apple Silicon (darwin/arm64) it runs the **MLX**
build; elsewhere a llama.cpp/ONNX build. `localEmbedder` reuses the HTTP transport
from Part 02 pointed at `http://localhost:<port>`.

**Prerequisite:** Tasks 1–14 green (the HTTP backend + agent path work end to end).

---

## Task 17: Pin the artifacts (research → manifest)

This task produces a concrete, per-platform manifest. It is **research + data**, not
guesswork — do not invent URLs.

**Files:**
- Create: `internal/discovery/manifest.go`

- [ ] **Step 1: Research current facts**

Use `ctx7`/web to confirm, and record findings as comments in `manifest.go`:
1. The LFM2-5 **retriever** model artifact(s) — the HuggingFace repo, and whether an
   **MLX** build exists (e.g. an `mlx-community/...` conversion) plus a GGUF build for
   llama.cpp. Capture exact download URLs + sizes + **sha256**.
2. The local **embeddings server** per platform:
   - darwin/arm64 → an MLX embeddings server (e.g. `mlx-embeddings`/`mlx-lm`-based)
     that exposes an OpenAI-compatible `POST /v1/embeddings`.
   - linux/amd64, darwin/amd64 → `llama-server` (llama.cpp) run with `--embeddings`,
     which already serves OpenAI-compatible `/v1/embeddings`.
   Capture the server binary download URL + sha256 (or document that the server is a
   bundled script + a runtime dependency, if that's the chosen path), and the exact
   launch arguments (model path, port, `--embeddings`/equivalent flag).
3. The health endpoint to probe (e.g. `GET /v1/models` or `/health`) and the embed
   endpoint path.

- [ ] **Step 2: Encode the manifest**

Create `internal/discovery/manifest.go` with the typed manifest filled from Step 1:

```go
package discovery

import "runtime"

// Artifact is one downloadable file with integrity check.
type Artifact struct {
	URL    string
	SHA256 string
	// Dest is the filename under the local cache dir (Task 18 resolves the dir).
	Dest string
	Exec bool // chmod +x after download (server binaries)
}

// ServerManifest describes how to obtain and launch the local embed server for one
// platform, and how to talk to it.
type ServerManifest struct {
	OS, Arch   string
	ModelID    string // e.g. "local/lfm2-5-retriever"
	Dim        int
	Artifacts  []Artifact
	// LaunchArgv is the server command line, with placeholders {model} and {port}
	// substituted at spawn time. argv[0] is the server binary path under the cache dir.
	LaunchArgv []string
	HealthPath string // e.g. "/v1/models"
	EmbedPath  string // e.g. "/v1/embeddings"
}

// localManifests is filled from Task 17 research. One entry per supported platform.
// EXAMPLE SHAPE — replace URLs/SHAs/argv with the researched values:
var localManifests = []ServerManifest{
	{
		OS: "darwin", Arch: "arm64",
		ModelID: "local/lfm2-5-retriever", Dim: 0, // set Dim from the model card
		Artifacts: []Artifact{
			{URL: "<mlx-server-binary-url>", SHA256: "<sha>", Dest: "embed-server", Exec: true},
			{URL: "<lfm2-5-mlx-model-url>", SHA256: "<sha>", Dest: "lfm2-5-mlx"},
		},
		LaunchArgv: []string{"{bin}/embed-server", "--model", "{bin}/lfm2-5-mlx", "--port", "{port}", "--embeddings"},
		HealthPath: "/v1/models", EmbedPath: "/v1/embeddings",
	},
	// linux/amd64 + darwin/amd64 entries using llama-server + GGUF here.
}

// CurrentManifest returns the manifest matching the host, if supported.
func CurrentManifest() (ServerManifest, bool) {
	for _, m := range localManifests {
		if m.OS == runtime.GOOS && m.Arch == runtime.GOARCH {
			return m, true
		}
	}
	return ServerManifest{}, false
}
```

- [ ] **Step 3: Commit**

```bash
git add internal/discovery/manifest.go
git commit -m "feat(discovery): per-platform local model-server manifest (MLX on darwin/arm64)"
```

---

## Task 18: Local server manager + localEmbedder + ResolveEmbedder("local")

**Files:**
- Create: `internal/discovery/localserver.go`
- Create: `internal/discovery/download.go`
- Modify: `internal/agent/discovery_glue.go` (`ensureDiscovery` local branch)
- Modify: `internal/config/ocodeconfig.go` is already done (Task 1); `local_model_status` is updated by the manager via `config.SaveLocalModelStatus`
- Test: `internal/discovery/download_test.go`

**Interfaces:**
- Consumes: `Artifact`, `ServerManifest`, `CurrentManifest`, `NewHTTPEmbedder`, `HTTPModel`; ocode `tool.ProcessRegistry.StartBackground` (the supervised spawn path).
- Produces:
  - `func EnsureArtifact(a Artifact, dir string) error` (download.go)
  - `func EnsureLocalServer(spawn func(cmdline string) error, cacheDir string, setStatus func(string)) (baseURL string, dim int, err error)` (localserver.go)
  - `func NewLocalEmbedder(baseURL, modelID string, dim int) Embedder`

- [ ] **Step 1: Write the failing test (download verify + atomic)**

Create `internal/discovery/download_test.go`:

```go
package discovery

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureArtifactVerifiesAndCaches(t *testing.T) {
	payload := []byte("model-bytes")
	sum := sha256.Sum256(payload)
	hexSum := hex.EncodeToString(sum[:])

	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	dir := t.TempDir()
	art := Artifact{URL: srv.URL, SHA256: hexSum, Dest: "model.bin"}
	if err := EnsureArtifact(art, dir); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "model.bin"))
	if string(got) != string(payload) {
		t.Fatal("downloaded content mismatch")
	}
	// Second call: cached (sha matches) → no re-download.
	if err := EnsureArtifact(art, dir); err != nil {
		t.Fatal(err)
	}
	if hits != 1 {
		t.Fatalf("expected 1 download, got %d (cache not honored)", hits)
	}
}

func TestEnsureArtifactRejectsBadSHA(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("tampered"))
	}))
	defer srv.Close()
	if err := EnsureArtifact(Artifact{URL: srv.URL, SHA256: "deadbeef", Dest: "x.bin"}, t.TempDir()); err == nil {
		t.Fatal("must reject sha mismatch")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/discovery/ -run TestEnsureArtifact -v`
Expected: FAIL — `EnsureArtifact` undefined.

- [ ] **Step 3: Implement download.go**

Create `internal/discovery/download.go`:

```go
package discovery

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// EnsureArtifact downloads a into dir/a.Dest if missing or sha-mismatched, verifies
// sha256, and writes atomically (temp + rename). Re-download is skipped when the
// cached file already matches.
func EnsureArtifact(a Artifact, dir string) error {
	dest := filepath.Join(dir, a.Dest)
	if existing, err := os.ReadFile(dest); err == nil {
		if sha256Hex(existing) == a.SHA256 {
			return nil
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir artifact dir: %w", err)
	}
	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Get(a.URL)
	if err != nil {
		return fmt.Errorf("download %s: %w", a.URL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: status %d", a.URL, resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read %s: %w", a.URL, err)
	}
	if got := sha256Hex(data); got != a.SHA256 {
		return fmt.Errorf("sha256 mismatch for %s: got %s want %s", a.Dest, got, a.SHA256)
	}
	tmp := dest + ".tmp"
	mode := os.FileMode(0o644)
	if a.Exec {
		mode = 0o755
	}
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return fmt.Errorf("write artifact tmp: %w", err)
	}
	if err := os.Rename(tmp, dest); err != nil {
		return fmt.Errorf("rename artifact: %w", err)
	}
	return nil
}

func sha256Hex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}
```

- [ ] **Step 4: Implement localserver.go**

Create `internal/discovery/localserver.go`:

```go
package discovery

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const localServerPort = "11457" // fixed port so separate ocode processes share one server

var (
	localMu    sync.Mutex
	localBase  string // set once a server is confirmed up
)

func localBaseURL() string { return "http://localhost:" + localServerPort }

// probeLocalServer returns true if a server already answers the health endpoint
// (the FetchLMStudioModels pattern — enables cross-process sharing).
func probeLocalServer(base, healthPath string) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(base + healthPath)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// EnsureLocalServer guarantees a shared local embed server is running and returns
// its base URL + embedding dimension. Probe-first (cross-process share); otherwise
// download artifacts and spawn via the supplied supervised-spawn function. Singleton
// within the process via localMu + localBase.
func EnsureLocalServer(spawn func(cmdline string) error, cacheDir string, setStatus func(string)) (string, int, error) {
	man, ok := CurrentManifest()
	if !ok {
		return "", 0, fmt.Errorf("local embedding backend not supported on this platform (%s/%s)", goos(), goarch())
	}
	base := localBaseURL()

	localMu.Lock()
	defer localMu.Unlock()

	if localBase != "" || probeLocalServer(base, man.HealthPath) {
		localBase = base
		return base, man.Dim, nil
	}

	// Download artifacts (idempotent; cached by sha).
	if setStatus != nil {
		setStatus("downloading")
	}
	binDir := filepath.Join(cacheDir, "local-"+man.OS+"-"+man.Arch)
	for _, a := range man.Artifacts {
		if err := EnsureArtifact(a, binDir); err != nil {
			if setStatus != nil {
				setStatus("none")
			}
			return "", 0, fmt.Errorf("artifact %s: %w", a.Dest, err)
		}
	}

	// Build the launch command line from LaunchArgv with {bin}/{port} substituted.
	argv := make([]string, len(man.LaunchArgv))
	for i, a := range man.LaunchArgv {
		a = strings.ReplaceAll(a, "{bin}", binDir)
		a = strings.ReplaceAll(a, "{port}", localServerPort)
		argv[i] = a
	}
	cmdline := strings.Join(argv, " ")
	emitDiscoveryDebug("DISCOVERY", "spawning local embed server: "+cmdline)
	if err := spawn(cmdline); err != nil {
		if setStatus != nil {
			setStatus("none")
		}
		return "", 0, fmt.Errorf("spawn local embed server: %w", err)
	}

	// Wait for health (model load can take seconds).
	for i := 0; i < 60; i++ {
		if probeLocalServer(base, man.HealthPath) {
			localBase = base
			if setStatus != nil {
				setStatus("ready")
			}
			return base, man.Dim, nil
		}
		time.Sleep(time.Second)
	}
	if setStatus != nil {
		setStatus("none")
	}
	return "", 0, fmt.Errorf("local embed server did not become healthy on %s", base)
}

// NewLocalEmbedder wraps the HTTP embedder transport pointed at the local server.
func NewLocalEmbedder(baseURL, modelID string, dim int) Embedder {
	man, _ := CurrentManifest()
	embedPath := "/v1/embeddings"
	if man.EmbedPath != "" {
		embedPath = man.EmbedPath
	}
	return NewHTTPEmbedder(HTTPModel{
		ID:        modelID,
		Endpoint:  baseURL + embedPath,
		Dimension: dim,
	}, "") // local server needs no API key
}
```

Add tiny `goos()`/`goarch()` wrappers in `manifest.go` (so `localserver.go` doesn't
import `runtime` again):

```go
func goos() string  { return runtime.GOOS }
func goarch() string { return runtime.GOARCH }
```

- [ ] **Step 5: Wire the local branch in the agent**

In `internal/agent/discovery_glue.go` `ensureDiscovery`, handle `local` before the
generic resolve. The supervised spawn goes through the agent's process registry (the
same supervised path bash uses):

```go
	dc := a.config.Ocode.Discovery
	var emb discovery.Embedder
	var err error
	if dc.EmbeddingBackend == "local" {
		spawn := func(cmdline string) error {
			reg := a.ProcessRegistry() // supervised registry (see note)
			if reg == nil {
				return fmt.Errorf("no process registry available for local server")
			}
			reg.StartBackground(cmdline)
			return nil
		}
		base, dim, e := discovery.EnsureLocalServer(spawn, discoveryCacheDir(), func(s string) {
			_ = config.SaveLocalModelStatus(s)
		})
		if e != nil {
			err = e
		} else {
			emb = discovery.NewLocalEmbedder(base, dc.EmbeddingModel, dim)
		}
	} else {
		emb, err = discovery.ResolveEmbedder(dc.EmbeddingBackend, dc.EmbeddingModel, keyForEnv)
	}
	if err != nil {
		emitDebug("DISCOVERY", fmt.Sprintf("disabled (fail-open): %v", err))
		a.disco = &discoveryState{enabled: false, initErr: err.Error()}
		return
	}
	// ... rest unchanged (build engine + session) ...
```

**Supervised-spawn accessor note:** `EnsureLocalServer` takes a `spawn(cmdline)`
function so the discovery package never touches `exec.Command` directly — the agent
supplies the supervised path. `ProcessRegistry.StartBackground(command string)` is
the existing supervised entry (it builds the `exec.Cmd`, registers it with the
`ProcessSupervisor`, and owns SIGTERM→SIGKILL teardown). If `*Agent` does not already
expose its `procs *tool.ProcessRegistry`, add a one-line accessor
`func (a *Agent) ProcessRegistry() *tool.ProcessRegistry { return a.procs }`. Verify
`StartBackground`'s signature/visibility (Part-13 recon: `internal/tool/process_supervisor.go`)
before wiring; if the public entry differs, use that one — do **not** add a raw
`exec.Command` path.

- [ ] **Step 6: Make the picker show local download status**

In `internal/tui/picker.go` `openEmbeddingModelPicker`, annotate the local entry with
the persisted status:

```go
	appendH("Local (downloaded on first use)")
	localLabel := "  local/lfm2-5-retriever"
	if st := m.config.Ocode.Discovery.LocalModelStatus; st != "" && st != "none" {
		localLabel += " (" + st + ")"
	}
	appendM(localLabel, "local/lfm2-5-retriever")
```

- [ ] **Step 7: Run tests + build**

Run: `go test ./internal/discovery/ -run 'TestEnsureArtifact' -v`
Expected: PASS.
Run: `go build ./...`
Expected: success.

- [ ] **Step 8: Manual verification (documented)**

On Apple Silicon, with artifacts pinned (Task 17): `/discover model local/lfm2-5-retriever`,
then `/discovery on`. First task: the Log tab shows `DISCOVERY spawning local embed
server …` then ranking lines; `/discover` shows `local: ready`; a second ocode window
enabling local attaches to the **same** server (no second download/spawn).

- [ ] **Step 9: Commit**

```bash
git add internal/discovery/localserver.go internal/discovery/download.go internal/discovery/download_test.go internal/agent/discovery_glue.go internal/tui/picker.go
git commit -m "feat(discovery): shared local model-server backend (probe-first, supervised spawn, MLX on Apple Silicon)"
```
