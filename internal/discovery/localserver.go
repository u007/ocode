package discovery

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
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
	// StartBackground runs the result via `bash -c`, so each element is shell-quoted
	// — otherwise a cache path containing a space (e.g. "/Users/Jane Doe/...") would
	// split into multiple args.
	argv := make([]string, len(man.LaunchArgv))
	for i, a := range man.LaunchArgv {
		a = strings.ReplaceAll(a, "{bin}", binDir)
		a = strings.ReplaceAll(a, "{port}", localServerPort)
		argv[i] = shellQuote(a)
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

// shellQuote single-quotes a string for safe use in a `bash -c` command line,
// escaping embedded single quotes via the '\'' idiom.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
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
