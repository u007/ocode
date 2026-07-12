package discovery

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const localServerPort = "11457" // fixed port so separate ocode processes share one server

// Embedding backend identifiers (mirror manifest.Backend*; defined here so
// callers in this file don't reach across for the constants).
const (
	BackendLlamaCpp = "llamacpp"
	BackendMLX      = "mlx"
)

var (
	localMu      sync.Mutex
	localBase    string // set once a server is confirmed up
	localModelID string // model id currently served by localBase (guards model switch)
)

func localBaseURL() string { return "http://localhost:" + localServerPort }

// probeLocalServer returns true only if an OpenAI-compatible models endpoint
// answers (the FetchLMStudioModels pattern — enables cross-process sharing).
// It validates the response shape ({"data":[{"id":...}]}) rather than trusting a
// bare 200, so a foreign process squatting the fixed port is not adopted as the
// embed server (which would yield garbage embeddings with no error).
func probeLocalServer(base, healthPath string) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(base + healthPath)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return false
	}
	var models struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &models); err != nil {
		return false
	}
	return len(models.Data) > 0 && models.Data[0].ID != ""
}

// LocalServerOptions tunes EnsureLocalServer's probe + spawn behavior.
type LocalServerOptions struct {
	// UserBaseURL, when set, is the first probe target — checked before the
	// manifest port so the user can point at LM Studio (default :1234) or
	// any pre-existing llama-server. The probe validates the /v1/models
	// response shape (see probeLocalServer). Empty means "skip the
	// user-URL probe and use the manifest port".
	UserBaseURL string
}

// EnsureLocalServer guarantees a shared local embed server is running and returns
// its base URL + embedding dimension. Probe-first (cross-process share) in this
// order:
//
//  1. opts.UserBaseURL (LM Studio, user-built llama-server) — if set + healthy.
//  2. The manifest port (11457) — adopted if already answering, even if it
//     was started by a different ocode process. The shared port means we
//     don't have to download or spawn our own when another process did.
//
// Otherwise: download artifacts and spawn via the supplied supervised-spawn
// function. Singleton within the process via localMu + localBase.
// EnsureLocalServer guarantees a shared local embed server is running and returns
// its base URL + embedding dimension. Unlike the original, it is model-aware:
// it only adopts/reuses a server that actually serves the requested modelID,
// so switching embedding models can never silently query a wrong-model server
// (which would produce garbage embeddings). Probe-first order:
//
//  1. opts.UserBaseURL (LM Studio, user-built llama-server, MLX server) — if set + healthy + matching model.
//  2. The manifest port (11457) — adopted if already answering with the right model
//     (even if started by a different ocode process).
//
// Otherwise: download artifacts (llamacpp) or write the MLX server script, then
// spawn via the supplied supervised-spawn function. Singleton within the process
// via localMu + localBase + localModelID.
func EnsureLocalServer(spawn func(cmdline string) error, modelID string, cacheDir string, setStatus func(string), opts LocalServerOptions) (string, int, error) {
	man, ok := ManifestForModel(modelID)
	if !ok {
		return "", 0, fmt.Errorf("no local embed manifest for model %q on %s/%s", modelID, goos(), goarch())
	}
	expect := man.ExpectedServeID()

	localMu.Lock()
	defer localMu.Unlock()

	// In-process fast path: only reuse if it serves the requested model.
	if localBase != "" {
		if localModelID == modelID {
			return localBase, man.Dim, nil
		}
		// A different model's server holds the slot; cannot reuse it.
		localBase = ""
	}

	base := localBaseURL()
	// 1) User-supplied server takes priority — but only if it serves the model.
	if opts.UserBaseURL != "" {
		if healthy, served := probeLocalServerModel(opts.UserBaseURL, man.HealthPath, expect); healthy {
			if !modelMatches(served, expect) {
				return "", 0, fmt.Errorf("user embed server at %s serves %v, not %s", opts.UserBaseURL, served, modelID)
			}
			emitDiscoveryDebug("DISCOVERY", "adopted user embed server: "+opts.UserBaseURL)
			localBase, localModelID = opts.UserBaseURL, modelID
			if setStatus != nil {
				setStatus("ready")
			}
			return localBase, man.Dim, nil
		}
		emitDiscoveryDebug("WARN", "user embed server did not respond at "+opts.UserBaseURL+" — falling back to bundled server")
	}
	// 2) Manifest port (cross-process share with other ocode instances).
	if healthy, served := probeLocalServerModel(base, man.HealthPath, expect); healthy {
		if !modelMatches(served, expect) {
			return "", 0, fmt.Errorf("local embed server already on %s serves %v, not %s; stop it or restart ocode to switch models", base, served, modelID)
		}
		emitDiscoveryDebug("DISCOVERY", "adopted shared embed server: "+base)
		localBase, localModelID = base, modelID
		if setStatus != nil {
			setStatus("ready")
		}
		return localBase, man.Dim, nil
	}

	// 3) Spawn our own.
	switch man.Backend {
	case BackendMLX:
		if err := spawnMLXServer(spawn, man, cacheDir, setStatus); err != nil {
			if setStatus != nil {
				setStatus("none")
			}
			return "", 0, err
		}
	default: // llamacpp (and empty Backend default)
		if err := spawnLlamaCppServer(spawn, man, cacheDir, setStatus); err != nil {
			if setStatus != nil {
				setStatus("none")
			}
			return "", 0, err
		}
	}

	// Wait for health (model load can take seconds). Reject a wrong-model server.
	for i := 0; i < 60; i++ {
		if healthy, served := probeLocalServerModel(base, man.HealthPath, expect); healthy {
			if !modelMatches(served, expect) {
				if setStatus != nil {
					setStatus("none")
				}
				return "", 0, fmt.Errorf("spawned embed server on %s serves %v, not %s", base, served, modelID)
			}
			localBase, localModelID = base, modelID
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

// spawnLlamaCppServer downloads the GGUF + server binary (idempotent, sha-pinned)
// and spawns the bundled llama-server via the supervised spawn function.
func spawnLlamaCppServer(spawn func(cmdline string) error, man ServerManifest, cacheDir string, setStatus func(string)) error {
	emitUserDiscoveryDebug("DISCOVERY", fmt.Sprintf("downloading %d artifact(s) for local embed server", len(man.Artifacts)))
	if setStatus != nil {
		setStatus("downloading")
	}
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
		a = strings.ReplaceAll(a, "{port}", localServerPort)
		if i == 0 {
			binPath = a // resolved server binary path (argv[0])
		}
		argv[i] = shellQuote(a)
	}
	libEnv := ""
	if libDir := libDirForBinary(binPath); libDir != "" {
		var name string
		if runtime.GOOS == "darwin" {
			name = "DYLD_LIBRARY_PATH"
		} else {
			name = "LD_LIBRARY_PATH"
		}
		libEnv = name + "=" + shellQuote(libDir) + " "
	}
	cmdline := libEnv + strings.Join(argv, " ")
	emitUserDiscoveryDebug("DISCOVERY", "spawning local embed server: "+cmdline)
	if err := spawn(cmdline); err != nil {
		return fmt.Errorf("spawn local embed server: %w", err)
	}
	return nil
}

// spawnMLXServer writes the bundled MLX server script (if needed) and spawns it
// via the supervised spawn function. The model is fetched by mlx_lm on first
// load, so there is no static artifact to download here.
func spawnMLXServer(spawn func(cmdline string) error, man ServerManifest, cacheDir string, setStatus func(string)) error {
	if setStatus != nil {
		setStatus("downloading") // mlx_lm fetches the model on first load
	}
	scriptPath, err := WriteMLXServerScript(cacheDir)
	if err != nil {
		return fmt.Errorf("write MLX server script: %w", err)
	}
	argv := make([]string, len(man.LaunchArgv))
	for i, a := range man.LaunchArgv {
		a = strings.ReplaceAll(a, "{script}", scriptPath)
		a = strings.ReplaceAll(a, "{port}", localServerPort)
		argv[i] = shellQuote(a)
	}
	cmdline := strings.Join(argv, " ")
	emitUserDiscoveryDebug("DISCOVERY", "spawning MLX embed server: "+cmdline)
	if err := spawn(cmdline); err != nil {
		return fmt.Errorf("spawn MLX embed server: %w", err)
	}
	return nil
}

// probeLocalServerModel probes the /v1/models endpoint and returns whether it
// answered with at least one model id, plus the served ids. Unlike probeLocalServer
// it surfaces the ids so callers can verify the served model matches the request.
func probeLocalServerModel(base, healthPath, expect string) (bool, []string) {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(base + healthPath)
	if err != nil {
		return false, nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return false, nil
	}
	var models struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &models); err != nil {
		return false, nil
	}
	ids := make([]string, 0, len(models.Data))
	for _, d := range models.Data {
		if d.ID != "" {
			ids = append(ids, d.ID)
		}
	}
	if len(ids) == 0 {
		return false, nil
	}
	return true, ids
}

// modelMatches reports whether one of the served ids contains expect. llama.cpp
// reports the GGUF path, so we substring-match the GGUF basename; the MLX server
// reports the discovery ModelID verbatim.
func modelMatches(served []string, expect string) bool {
	for _, id := range served {
		if strings.Contains(id, expect) {
			return true
		}
	}
	return false
}

// StopLocalServer forgets the in-process server handle. Call this when the
// embedding model changes so the next EnsureLocalServer re-probes instead of
// reusing a server that serves a different model. It does not kill a
// cross-process server; if one is squatting the port with the wrong model,
// EnsureLocalServer returns a clear error telling the user to restart ocode.
func StopLocalServer() {
	localMu.Lock()
	localBase = ""
	localModelID = ""
	localMu.Unlock()
}

// shellQuote single-quotes a string for safe use in a `bash -c` command line,
// escaping embedded single quotes via the '\” idiom.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// libDirForBinary returns the directory that holds the server binary and its
// sibling shared libraries (.dylib/.so) — simply the binary's own parent dir.
// The llama.cpp release tarball extracts into binDir/<version>/ (e.g.
// binDir/llama-b9777/) with the binary and its libraries together, and
// LaunchArgv[0] points straight at that binary, so filepath.Dir(binPath) is the
// exact lib dir for the version being launched.
//
// This is derived from the RESOLVED binary path, not by scanning binDir, on
// purpose: after a version bump binDir can contain MULTIPLE llama-b<ver>/ dirs
// (the old one plus the new one), and a directory scan would pair the new binary
// with an older version's libraries — an ABI mismatch that fails to load.
// Returns "" if binPath is empty or not a real file (the caller treats "" as
// "no library path needed").
func libDirForBinary(binPath string) string {
	if binPath == "" {
		return ""
	}
	if _, err := os.Stat(binPath); err != nil {
		return "" // not extracted yet, or a flat layout with no sibling libs
	}
	return filepath.Dir(binPath)
}

// NewLocalEmbedder wraps the HTTP embedder transport pointed at the local server.
func NewLocalEmbedder(baseURL, modelID string, dim int) Embedder {
	// Both the llama.cpp and MLX local backends expose OpenAI-compatible
	// /v1/embeddings, so the path is constant regardless of which model serves it.
	return NewHTTPEmbedder(HTTPModel{
		ID:        modelID,
		Endpoint:  baseURL + "/v1/embeddings",
		Dimension: dim,
	}, "") // local server needs no API key
}
