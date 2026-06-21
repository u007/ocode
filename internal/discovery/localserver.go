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

var (
	localMu   sync.Mutex
	localBase string // set once a server is confirmed up
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
func EnsureLocalServer(spawn func(cmdline string) error, cacheDir string, setStatus func(string), opts LocalServerOptions) (string, int, error) {
	man, ok := CurrentManifest()
	if !ok {
		return "", 0, fmt.Errorf("local embedding backend not supported on this platform (%s/%s)", goos(), goarch())
	}

	localMu.Lock()
	defer localMu.Unlock()

	// In-process fast path.
	if localBase != "" {
		return localBase, man.Dim, nil
	}
	// 1) User-supplied server takes priority (LM Studio :1234, custom
	//    llama-server, etc.). We adopt any base URL whose /v1/models
	//    endpoint returns a non-empty model list.
	if opts.UserBaseURL != "" {
		if probeLocalServer(opts.UserBaseURL, man.HealthPath) {
			emitUserDiscoveryDebug("DISCOVERY", "adopted user embed server: "+opts.UserBaseURL)
			localBase = opts.UserBaseURL
			return localBase, man.Dim, nil
		}
		emitDiscoveryDebug("WARN", "user embed server did not respond at "+opts.UserBaseURL+" — falling back to bundled server")
	}
	// 2) Manifest port (cross-process share with other ocode instances).
	base := localBaseURL()
	if probeLocalServer(base, man.HealthPath) {
		emitUserDiscoveryDebug("DISCOVERY", "adopted shared embed server: "+base)
		localBase = base
		return localBase, man.Dim, nil
	}

	// Download artifacts (idempotent; cached by sha).
	emitUserDiscoveryDebug("DISCOVERY", fmt.Sprintf("downloading %d artifact(s) for local embed server", len(man.Artifacts)))
	if setStatus != nil {
		setStatus("downloading")
	}
	binDir := filepath.Join(cacheDir, "local-"+man.OS+"-"+man.Arch)
	for _, a := range man.Artifacts {
		if err := EnsureArtifact(a, binDir); err != nil {
			if setStatus != nil {
				setStatus("none")
			}
			return "", 0, err
		}
	}

	// Build the launch command line from LaunchArgv with {bin}/{port} substituted.
	// StartBackground runs the result via `bash -c`, so each element is shell-quoted
	// — otherwise a cache path containing a space (e.g. "/Users/Jane Doe/...") would
	// split into multiple args.
	argv := make([]string, len(man.LaunchArgv))
	for i, a := range man.LaunchArgv {
		a = strings.ReplaceAll(a, "{bin}", binDir)
		a = strings.ReplaceAll(a, "{port}", localServerPort)
		argv[i] = shellQuote(a)
	}
	// llama-server's release tarball puts the binary next to its sibling
	// shared libraries (libllama.*, libggml.*). The dynamic loader needs
	// DYLD_LIBRARY_PATH on macOS / LD_LIBRARY_PATH on Linux to find them
	// when the binary is invoked via an absolute path that isn't in $PATH.
	// Set the env var in the same `bash -c` line as the spawn — wrapping the
	// command itself, not the wrapper, so it's scoped to llama-server.
	libEnv := ""
	if libDir := findLibDir(binDir); libDir != "" {
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

// shellQuote single-quotes a string for safe use in a `bash -c` command line,
// escaping embedded single quotes via the '\'' idiom.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// findLibDir returns the absolute path of the directory that contains
// llama-server's sibling shared libraries. The llama.cpp release tarball
// extracts into binDir/<version>/ (e.g. binDir/llama-b9747/) and puts both
// the binary and the .dylib/.so files there. We detect this by globbing for
// a directory under binDir that already contains the extracted binary — this
// keeps the function version-agnostic (b9747, b9800, …) so the manifest only
// needs to track the SHA, not the directory name. Returns "" if not found
// (e.g. the binary hasn't been extracted yet, or a future release changes
// the layout to a flat binDir). The caller treats "" as "no library path
// needed".
func findLibDir(binDir string) string {
	// The most common case: a single versioned subdirectory. Walk one level
	// down and pick the first directory that has llama-server inside.
	entries, err := os.ReadDir(binDir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		candidate := filepath.Join(binDir, e.Name())
		if _, err := os.Stat(filepath.Join(candidate, "llama-server")); err == nil {
			return candidate
		}
	}
	// Fallback: maybe the binary was extracted directly into binDir.
	if _, err := os.Stat(filepath.Join(binDir, "llama-server")); err == nil {
		return binDir
	}
	return ""
}

// NewLocalEmbedder wraps the HTTP embedder transport pointed at the local server.
func NewLocalEmbedder(baseURL, modelID string, dim int) Embedder {
	embedPath := "/v1/embeddings"
	if man, ok := CurrentManifest(); ok && man.EmbedPath != "" {
		embedPath = man.EmbedPath
	}
	return NewHTTPEmbedder(HTTPModel{
		ID:        modelID,
		Endpoint:  baseURL + embedPath,
		Dimension: dim,
	}, "") // local server needs no API key
}
