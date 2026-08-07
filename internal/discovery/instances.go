package discovery

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

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
	// PID is the OS process id of the server this session spawned, or 0 when
	// unknown — e.g. for an instance discovered via ProbeModelInstance
	// (started by a different ocode process, so we never had its PID) or
	// before the spawn closure has recorded it.
	PID int
}

type chatInstance struct {
	info         InstanceInfo
	processID    string // tool.Process.ID, empty when stopped
	warmupCancel context.CancelFunc
}

// markInstanceState records state for modelID's in-process instance record.
// The record may legitimately be gone: StopModelInstance deletes it under
// instMu without holding the per-model start lock, so a stop racing a start
// that is still unwinding leaves nothing to update. That is not an error —
// but indexing the map blindly would panic on the nil *chatInstance.
func markInstanceState(modelID string, state InstanceState) {
	instMu.Lock()
	defer instMu.Unlock()
	if inst, ok := instances[modelID]; ok {
		inst.info.State = state
	}
}

var (
	instMu    sync.Mutex
	instances = map[string]*chatInstance{}

	// startMu guards startLocks — lazily created per-model mutexes that
	// serialize StartModelInstance calls for the same modelID within this
	// process. Without this, two goroutines racing to auto-start the same
	// model (e.g. two sub-agents, or a background auto-start racing a manual
	// /localmodel enable) would both pass the probe-before-spawn check at the
	// same time and try to bind the same port.
	startMu    sync.Mutex
	startLocks = map[string]*sync.Mutex{}
)

// chatBreakerThreshold/chatBreakerCooldown gate StartModelInstance's
// fail-fast circuit breaker: after chatBreakerThreshold consecutive
// health-poll failures for a modelID, further calls within chatBreakerCooldown
// of the last failure are rejected immediately instead of repeating a
// multi-minute spawn+poll that is very unlikely to succeed. A successful
// health check clears the counter.
const (
	chatBreakerThreshold = 2
	chatBreakerCooldown  = 10 * time.Minute
)

type chatFailureState struct {
	consecutiveFailures int
	lastFailureAt       time.Time
}

var (
	chatFailuresMu sync.Mutex
	chatFailures   = map[string]*chatFailureState{}
)

// chatBreakerRecordFailure records a health-poll failure for modelID.
func chatBreakerRecordFailure(modelID string) {
	chatFailuresMu.Lock()
	defer chatFailuresMu.Unlock()
	s, ok := chatFailures[modelID]
	if !ok {
		s = &chatFailureState{}
		chatFailures[modelID] = s
	}
	s.consecutiveFailures++
	s.lastFailureAt = time.Now()
}

// chatBreakerReset clears modelID's failure history after a successful
// health check or a successful adopt-of-already-healthy-instance.
func chatBreakerReset(modelID string) {
	chatFailuresMu.Lock()
	defer chatFailuresMu.Unlock()
	delete(chatFailures, modelID)
}

// chatBreakerRecentFailure reports whether modelID has failed at least once
// recently (within chatBreakerCooldown), used to shorten the health-poll
// budget on retry instead of repeating the full first-attempt budget.
func chatBreakerRecentFailure(modelID string) bool {
	chatFailuresMu.Lock()
	defer chatFailuresMu.Unlock()
	s, ok := chatFailures[modelID]
	return ok && s.consecutiveFailures > 0 && time.Since(s.lastFailureAt) < chatBreakerCooldown
}

// chatBreakerBlocked reports whether modelID has failed chatBreakerThreshold
// or more times in a row within chatBreakerCooldown, in which case
// StartModelInstance should fail fast instead of spawning/polling again.
func chatBreakerBlocked(modelID string) (blocked bool, failures int, lastFailureAt time.Time) {
	chatFailuresMu.Lock()
	defer chatFailuresMu.Unlock()
	s, ok := chatFailures[modelID]
	if !ok {
		return false, 0, time.Time{}
	}
	if s.consecutiveFailures >= chatBreakerThreshold && time.Since(s.lastFailureAt) < chatBreakerCooldown {
		return true, s.consecutiveFailures, s.lastFailureAt
	}
	return false, s.consecutiveFailures, s.lastFailureAt
}

// lockForStart returns the singleton mutex used to serialize
// StartModelInstance calls for modelID within this process.
func lockForStart(modelID string) *sync.Mutex {
	startMu.Lock()
	defer startMu.Unlock()
	l, ok := startLocks[modelID]
	if !ok {
		l = &sync.Mutex{}
		startLocks[modelID] = l
	}
	return l
}

// StartModelInstance resolves modelID's chat manifest, downloads any llama.cpp
// artifacts (idempotent, sha-pinned — same EnsureArtifact path as the
// embedder), and spawns it on port with maxParallel concurrent request slots
// via the supplied supervised-spawn function. Blocks until the health check
// passes or times out (300x1s poll — longer than EnsureLocalServer's 60x1s
// because first load downloads multi-GB weights; see chatHealthPollAttempts).
// Updates the in-process instance map on success.
//
// Probes port before spawning (mirrors EnsureLocalServer's adopt-before-spawn
// pattern in localserver.go) and adopts an already-healthy, matching server
// instead of spawning a second one. This matters once callers re-warm an
// "enabled" model after a restart: this process's in-memory instances map is
// empty then, but a DIFFERENT ocode process (or a previous run of this same
// session) may already have a healthy server bound to modelID's port. Without
// this check, StartModelInstance would try to bind the same port again, fail,
// then have its health-poll find and adopt the OTHER process's server anyway
// — but with the wrong processID recorded (this call's spawn never
// succeeded), leaving /localmodel disable unable to find the real process and
// /localmodel status reading a dead PID for memory.
func StartModelInstance(spawn func(cmdline string) error, modelID string, port int, maxParallel int, cacheDir string, hfToken string) error {
	lock := lockForStart(modelID)
	lock.Lock()
	defer lock.Unlock()

	man, ok := ChatManifestForHost(modelID)
	if !ok {
		return fmt.Errorf("no local chat manifest for model %q on %s/%s", modelID, goos(), goarch())
	}

	if info, ok := ProbeModelInstance(modelID, port); ok {
		instMu.Lock()
		info.MaxParallel = maxParallel
		instances[modelID] = &chatInstance{info: info} // processID stays empty — we don't own this process
		instMu.Unlock()
		chatBreakerReset(modelID)
		return nil
	}

	// A model that has already failed to become healthy chatBreakerThreshold
	// times in a row recently is very unlikely to succeed on yet another
	// blind retry (e.g. the bonsai-8b-1bit incident: a missing native
	// dependency made the server hang instead of erroring, and every caller
	// that needed the model — one per permission decision — independently
	// ate the full 15-minute poll budget). Fail fast instead of repeating
	// the same multi-minute hang until the cooldown lapses or the caller
	// manually restarts the model.
	if blocked, failures, lastFailureAt := chatBreakerBlocked(modelID); blocked {
		return fmt.Errorf("local chat server for %q failed to become healthy %d times recently (last failure %s ago) — not retrying automatically to avoid repeated hangs; fix the underlying issue (check the discovery debug log) and restart the model manually via /localmodel to retry",
			modelID, failures, time.Since(lastFailureAt).Round(time.Second))
	}

	instMu.Lock()
	instances[modelID] = &chatInstance{info: InstanceInfo{
		ModelID: modelID, State: InstanceStarting, Port: port, MaxParallel: maxParallel,
	}}
	instMu.Unlock()

	base := fmt.Sprintf("http://localhost:%d", port)

	// The in-process lockForStart mutex above only serializes calls within
	// THIS ocode process. A relaunched ocode process (e.g. the user retrying
	// after a slow/stuck cold download) has an empty in-memory instances map
	// and no idea another process's spawn is still in flight, so its own
	// ProbeModelInstance check above (not yet healthy) would otherwise fall
	// straight through to spawning a SECOND competing server on the same
	// port — both then contend for network/CPU and neither ever finishes.
	// acquireChatStartLock is a cross-process file lock so only one process
	// spawns; everyone else adopts by waiting on the same health poll below.
	acquired, release, lockErr := acquireChatStartLock(cacheDir, modelID)
	if lockErr != nil {
		markInstanceState(modelID, InstanceStopped)
		return lockErr
	}
	if !acquired {
		return waitForChatHealth(modelID, base, man)
	}
	defer release()

	// Holding the start lock means no other ocode process is legitimately
	// spawning this model right now, so a port that is bound but failed the
	// probe above belongs to an orphan from a dead ocode process. Reclaim it
	// here: the spawn below cannot bind an occupied port, and without this the
	// call would spawn nothing, poll a wedged server for 15 minutes, and fail.
	if _, reapErr := reapStrayChatServer(modelID, port, man); reapErr != nil {
		markInstanceState(modelID, InstanceStopped)
		chatBreakerRecordFailure(modelID)
		return reapErr
	}

	var err error
	switch man.Backend {
	case BackendMLX:
		err = spawnMLXChatServer(spawn, man, port, maxParallel, hfToken)
	default:
		err = spawnLlamaCppChatServer(spawn, man, cacheDir, port, maxParallel)
	}
	if err != nil {
		// A concurrent ocode process may have won the race and bound the port
		// first (e.g. two ocode instances launched at the same moment both
		// auto-starting the same enabled model) — re-probe once before
		// reporting failure, so this process adopts the winner's server
		// instead of erroring out.
		if info, probeOK := ProbeModelInstance(modelID, port); probeOK {
			instMu.Lock()
			info.MaxParallel = maxParallel
			instances[modelID] = &chatInstance{info: info}
			instMu.Unlock()
			return nil
		}
		markInstanceState(modelID, InstanceStopped)
		return err
	}

	return waitForChatHealth(modelID, base, man)
}

// waitForChatHealth polls modelID's chat server until it reports healthy or
// the poll budget is exhausted, updating the in-process instance map with
// the outcome. Shared by the process that actually spawned the server and by
// a relaunched process that lost the start-lock race (see
// acquireChatStartLock) and is just waiting on someone else's spawn.
func waitForChatHealth(modelID, base string, man ServerManifest) error {
	expect := man.ExpectedServeID()
	// Weights are already downloaded by the time this poll starts (MLX via
	// ensureMLXChatModelCached, llama.cpp via EnsureArtifact — both run
	// earlier in spawn*ChatServer's synchronous setup, each with its own
	// generous download budget). This loop only covers the server process
	// actually booting: reading cached weights off disk, initializing the
	// runtime, and binding the port. 15 minutes (vs the embedder's 60s) is a
	// deliberately wide safety margin for a large/1-bit-quant model on a slow
	// disk, not a download budget.
	//
	// That 15-minute budget is only warranted the FIRST time this modelID is
	// started in this process — a fair benefit of the doubt for a genuinely
	// slow cold load. A model that has already failed to become healthy
	// recently (chatBreakerRecentFailure) is far more likely stuck than slow
	// (see the bonsai-8b-1bit incident this guards against), so retries get a
	// much shorter budget instead of repeating the full 15-minute hang.
	const chatHealthPollAttemptsFirst = 900
	const chatHealthPollAttemptsRetry = 60
	attempts := chatHealthPollAttemptsFirst
	if chatBreakerRecentFailure(modelID) {
		attempts = chatHealthPollAttemptsRetry
	}
	for i := 0; i < attempts; i++ {
		if healthy, served := probeLocalServerModel(base, man.HealthPath, expect); healthy {
			if !modelMatches(served, expect) {
				markInstanceState(modelID, InstanceStopped)
				chatBreakerRecordFailure(modelID)
				return fmt.Errorf("spawned chat server on %s serves %v, not %s", base, served, modelID)
			}
			warmupCtx, warmupCancel := context.WithCancel(context.Background())
			instMu.Lock()
			if inst, ok := instances[modelID]; ok {
				if inst.warmupCancel != nil {
					inst.warmupCancel()
				}
				inst.info.State = InstanceReady
				inst.info.BaseURL = base
				inst.warmupCancel = warmupCancel
			}
			instMu.Unlock()
			chatBreakerReset(modelID)
			// Fire-and-forget: the warm-up is best-effort and only improves a
			// later RSS reading; a slow first forward pass (up to the client's
			// 60s timeout) must not stall health-ready reporting, so it runs
			// off the caller's critical path.
			go warmUpChatModel(warmupCtx, base, man)
			return nil
		}
		time.Sleep(time.Second)
	}
	markInstanceState(modelID, InstanceStopped)
	chatBreakerRecordFailure(modelID)
	return fmt.Errorf("local chat server for %q did not become healthy on %s", modelID, base)
}

// warmUpChatModel issues one minimal completion request so the memory figure
// /localmodel list reports (via RSSBytes, OS-level process RSS) reflects the
// model actually being resident rather than just the server process having
// bound its port. This matters most for the MLX backend: mlx_lm's arrays are
// lazily evaluated, so a freshly-spawned mlx_lm.server has only loaded the
// weight graph, not materialized it into unified memory — RSS stays near the
// bare Python/MLX interpreter footprint (a few hundred MB) until the first
// forward pass forces evaluation. llama.cpp's server loads and typically
// touches weight pages eagerly at startup, so this call is a no-op there in
// effect, but issuing it uniformly (rather than special-casing by backend)
// keeps the reported number consistent and meaningful across every backend
// and platform (MLX/darwin, llama.cpp GGUF on darwin/linux/windows) without
// depending on any OS- or backend-specific memory-accounting API.
//
// Best-effort: a failure here (slow first load, server hiccup) must not fail
// model startup. The request is cancelled when its instance is stopped or
// replaced, and failures are logged for diagnostics.
func warmUpChatModel(ctx context.Context, base string, man ServerManifest) {
	if man.CompletionPath == "" {
		return
	}
	body := fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hi"}],"max_tokens":1}`, man.ExpectedServeID())
	client := &http.Client{Timeout: 60 * time.Second}
	started := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+man.CompletionPath, bytes.NewBufferString(body))
	if err != nil {
		log.Printf("local model warm-up %q: build request: %v", man.ModelID, err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() == nil {
			log.Printf("local model warm-up %q after %s: %v", man.ModelID, time.Since(started).Round(time.Millisecond), err)
		}
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		log.Printf("local model warm-up %q after %s: HTTP %s", man.ModelID, time.Since(started).Round(time.Millisecond), resp.Status)
	}
}

// chatStartLockStaleAfter bounds how long a start-lock file may be held
// before a contending process treats it as abandoned (the holder crashed or
// was killed mid-spawn without cleaning up) and reclaims it. The lock is held
// across BOTH the model-weight download (ensureMLXChatModelCached's 30-minute
// budget, spawnLlamaCppChatServer's EnsureArtifact) and the post-download
// waitForChatHealth poll (15 minutes) — set above their combined worst case so
// a legitimately slow cold download is never stolen mid-flight.
const chatStartLockStaleAfter = 50 * time.Minute

// acquireChatStartLock is a cross-process mutex (O_CREATE|O_EXCL lock file —
// same primitive as config.lockOcodeConfig and the local-model concurrency
// slots in agent/local_model_limiter.go) guarding modelID's spawn step in
// StartModelInstance, so two ocode processes racing to auto-start the same
// enabled model (or a relaunch racing a still-starting prior process) don't
// both spawn a competing server on the same port. acquired is false when
// another live process already holds the lock; the caller should adopt that
// process's in-flight start (waitForChatHealth) instead of spawning.
func acquireChatStartLock(cacheDir, modelID string) (acquired bool, release func(), err error) {
	lockDir := filepath.Join(cacheDir, "locks")
	if err := os.MkdirAll(lockDir, 0755); err != nil {
		return false, nil, err
	}
	safeID := strings.ReplaceAll(modelID, "/", "_")
	lockPath := filepath.Join(lockDir, safeID+".start.lock")

	for attempt := 0; attempt < 2; attempt++ {
		f, openErr := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if openErr == nil {
			// Record the owner so a contender can tell "another ocode is
			// genuinely mid-spawn" from "the holder died without releasing".
			// Without this, a lock orphaned by a SIGKILLed process blocks
			// every contender behind the 50-minute mtime staleness rule —
			// including the stray-server reap below it, which is precisely
			// what a dead holder leaves behind. A write failure is not fatal:
			// the lock still works, it just falls back to mtime staleness.
			if _, writeErr := fmt.Fprintf(f, "%d", os.Getpid()); writeErr != nil {
				emitDiscoveryDebugAt("DISCOVERY", fmt.Sprintf("could not record owner pid in chat start lock %s: %v", lockPath, writeErr), false)
			}
			f.Close()
			released := false
			return true, func() {
				if released {
					return
				}
				released = true
				os.Remove(lockPath)
			}, nil
		}
		if !os.IsExist(openErr) {
			return false, nil, fmt.Errorf("create chat start lock %s: %w", lockPath, openErr)
		}
		if attempt == 0 {
			if chatStartLockOwnerDead(lockPath) {
				os.Remove(lockPath) // holder is gone — reclaim and retry
				continue
			}
			if info, statErr := os.Stat(lockPath); statErr == nil && time.Since(info.ModTime()) > chatStartLockStaleAfter {
				os.Remove(lockPath) // abandoned lock from a crashed holder — reclaim and retry
				continue
			}
		}
		break
	}
	return false, nil, nil
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
func spawnMLXChatServer(spawn func(cmdline string) error, man ServerManifest, port, maxParallel int, hfToken string) error {
	if err := ensureMLXChatModelCached(man.MLXRepo, hfToken); err != nil {
		return err
	}
	argv := filterMLXArgs(man.LaunchArgv, man.MLXRepo, port, maxParallel, mlxServerFlags())
	// Once cached locally, mlx_lm.server still hits the HF Hub API on every
	// launch to check for repo updates. That check has nothing to do with
	// whether the model is already running, and fails loudly (404/401) for
	// repos that aren't publicly reachable. Force offline mode so a cached
	// model launches without the hub round-trip.
	cmdline := "HF_HUB_OFFLINE=1 TRANSFORMERS_OFFLINE=1 " + strings.Join(argv, " ")
	emitUserDiscoveryDebug("DISCOVERY", "spawning MLX chat server: "+cmdline)
	if err := spawn(cmdline); err != nil {
		return fmt.Errorf("spawn MLX chat server: %w", err)
	}
	return nil
}

// ensureMLXChatModelCached runs huggingface_hub's snapshot_download for repo
// before the offline launch below. spawnMLXChatServer always sets
// HF_HUB_OFFLINE=1 for the actual mlx_lm.server launch (see its comment),
// which means mlx_lm.server itself can never perform a first-time download —
// it just fails with "Cannot find an appropriate cached snapshot folder ...
// and outgoing traffic has been disabled". snapshot_download always hits the
// Hub over the network to re-verify every file's metadata unless told not
// to — plain snapshot_download(repo_id=...) is NOT a network no-op once
// cached, contrary to what this comment used to claim, so it was re-running
// a "Fetching N files" pass against the network on every single launch. Try
// local_files_only=True first (pure local cache check, no network); only
// fall back to a real network download if that raises because something is
// actually missing.
func ensureMLXChatModelCached(repo, hfToken string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	emitUserDiscoveryDebug("DISCOVERY", fmt.Sprintf("ensuring MLX model %q is downloaded (no-op if already cached)", repo))
	script := fmt.Sprintf(`
from huggingface_hub import snapshot_download
try:
    snapshot_download(repo_id=%[1]q, local_files_only=True)
except Exception:
    snapshot_download(repo_id=%[1]q)
`, repo)
	cmd := exec.CommandContext(ctx, "python3", "-c", script)
	if hfToken != "" {
		cmd.Env = append(os.Environ(), "HF_TOKEN="+hfToken)
	}
	var out bytes.Buffer
	progress := &downloadProgressWriter{onLine: func(line string) {
		emitUserDiscoveryDebug("DISCOVERY", fmt.Sprintf("downloading %s: %s", repo, line))
	}}
	cmd.Stdout = io.MultiWriter(&out, progress)
	cmd.Stderr = io.MultiWriter(&out, progress)
	if err := cmd.Run(); err != nil {
		return mlxDownloadError(repo, err, out.String())
	}
	return nil
}

// downloadProgressWriter forwards huggingface_hub's snapshot_download output
// to onLine as it streams, so a multi-GB cold download shows live progress in
// the discovery debug log instead of going silent until the 30-minute
// deadline. tqdm (which huggingface_hub uses for its progress bars) rewrites
// its line in place with \r rather than \n, so lines are split on either, and
// throttled to one emission per second so a fast-updating bar doesn't flood
// the log with one entry per percent.
type downloadProgressWriter struct {
	onLine   func(string)
	buf      []byte
	lastEmit time.Time
}

func (w *downloadProgressWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		idx := bytes.IndexAny(w.buf, "\r\n")
		if idx < 0 {
			break
		}
		line := strings.TrimSpace(string(w.buf[:idx]))
		w.buf = w.buf[idx+1:]
		if line == "" || time.Since(w.lastEmit) < time.Second {
			continue
		}
		w.lastEmit = time.Now()
		w.onLine(line)
	}
	return len(p), nil
}

// mlxDownloadError turns a failed snapshot_download into an actionable error.
// huggingface_hub's own errors for gated/private repos or anonymous rate
// limiting all mention one of these terms in their message; when they do, the
// fix is a token, not a retry, so point the user straight at /localmodel
// hf-token instead of surfacing a bare Python traceback.
func mlxDownloadError(repo string, err error, out string) error {
	lower := strings.ToLower(out)
	authSignals := []string{"401", "403", "gated", "not logged in", "access to model", "restricted", "authentication", "unauthorized"}
	for _, signal := range authSignals {
		if strings.Contains(lower, signal) {
			return fmt.Errorf(
				"model %q requires Hugging Face authentication to download: %w\n"+
					"Fix: get a read-access token from https://huggingface.co/settings/tokens, "+
					"then run \"/localmodel hf-token <token>\" and retry enabling this model "+
					"(or run \"huggingface-cli login\" / export HF_TOKEN in your shell instead)",
				repo, err)
		}
	}
	return fmt.Errorf("download MLX model %q: %w: %s", repo, err, strings.TrimSpace(out))
}

// filterMLXArgs expands {repo}/{port}/{parallel} placeholders and drops flags
// the installed mlx_lm.server doesn't understand (e.g. --decode-concurrency is
// absent from the mlx_lm 0.30.5 that the PrismML mlx fork pairs with) instead
// of failing the spawn. supported == nil means the flag probe itself failed
// (see mlxServerFlags) — fail OPEN in that case (keep every flag) rather than
// silently stripping required flags like --model/--host/--port along with the
// genuinely-unsupported ones.
func filterMLXArgs(launchArgv []string, repo string, port, maxParallel int, supported map[string]bool) []string {
	argv := make([]string, 0, len(launchArgv))
	for i := 0; i < len(launchArgv); i++ {
		a := launchArgv[i]
		a = strings.ReplaceAll(a, "{repo}", repo)
		a = strings.ReplaceAll(a, "{port}", fmt.Sprintf("%d", port))
		a = strings.ReplaceAll(a, "{parallel}", fmt.Sprintf("%d", maxParallel))
		if supported != nil && strings.HasPrefix(a, "--") && !supported[a] {
			emitUserDiscoveryDebug("DISCOVERY", fmt.Sprintf("mlx_lm.server does not support %s; dropping", a))
			// Drop the flag's value too (the next arg, if it is not itself a flag).
			if i+1 < len(launchArgv) && !strings.HasPrefix(launchArgv[i+1], "--") {
				i++
			}
			continue
		}
		argv = append(argv, shellQuote(a))
	}
	return argv
}

// mlxServerFlags lazily captures the flags supported by the installed
// mlx_lm.server (run once per process). The manifest's --decode-concurrency
// flag only exists in mlx_lm >= 0.31; the PrismML mlx fork pairs with
// mlx_lm 0.30.5, which lacks it. Instead of failing the spawn, drop any
// manifest flag the installed server doesn't understand and note it.
//
// Returns nil if the probe itself failed (python3 missing, mlx_lm not
// installed, --help hung past the timeout, etc.) — nil is a distinct signal
// from "probed successfully and found zero flags", and filterMLXArgs treats
// nil as "keep every flag" (fail open) rather than dropping everything
// including --model/--host/--port, which would silently produce a broken
// spawn command.
var (
	mlxFlagsOnce  sync.Once
	mlxServerFlag map[string]bool // nil until mlxServerFlags() resolves it
)

func mlxServerFlags() map[string]bool {
	mlxFlagsOnce.Do(func() {
		// Running --help is side-effect free (the server prints usage and
		// exits), but NOT cheap: mlx_lm.server's import chain (mlx, numpy,
		// transformers-like deps) can legitimately take longer than a naive
		// few-second timeout to even reach argparse, especially on a cold
		// bytecode cache — an earlier 5s timeout was observed killing this
		// probe in practice ("signal: killed"), which forced the fail-open
		// path (keep every manifest flag, including ones this server
		// actually rejects) on every run. 30s is generous because this only
		// runs once per process lifetime (sync.Once).
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, "python3", "-m", "mlx_lm.server", "--help").Output()
		if err != nil {
			emitUserDiscoveryDebug("DISCOVERY", "mlx_lm.server --help failed (keeping all manifest flags): "+err.Error())
			return // mlxServerFlag stays nil — fail open
		}
		flags := map[string]bool{}
		for _, line := range strings.Split(string(out), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "--") {
				name := strings.Fields(line)[0]
				flags[name] = true
			}
		}
		mlxServerFlag = flags
	})
	return mlxServerFlag
}

// ProbeModelInstance checks whether modelID's chat server is already running
// and healthy at port, via a live HTTP probe — unlike GetModelInstance, this
// finds a server started by a DIFFERENT ocode process (this process's
// in-memory instances map only knows about servers it spawned itself).
// Mirrors EnsureLocalServer's probe-before-spawn pattern (localserver.go) so
// /localmodel status doesn't misreport "stopped" for a model another session
// already has running on its assigned port.
func ProbeModelInstance(modelID string, port int) (InstanceInfo, bool) {
	man, ok := ChatManifestForHost(modelID)
	if !ok {
		return InstanceInfo{}, false
	}
	base := fmt.Sprintf("http://localhost:%d", port)
	expect := man.ExpectedServeID()
	healthy, served := probeLocalServerModel(base, man.HealthPath, expect)
	if !healthy || !modelMatches(served, expect) {
		return InstanceInfo{}, false
	}
	return InstanceInfo{ModelID: modelID, State: InstanceReady, Port: port, BaseURL: base}, true
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

// SetModelInstanceStateForTest forces modelID's in-process instance record
// into state, creating it if absent. Test-only: production state transitions
// happen exclusively inside StartModelInstance/StopModelInstance — this exists
// so callers of GetModelInstance (e.g. the TUI's warm-up decision) can be
// regression-tested against stale/failed records without spawning a server.
func SetModelInstanceStateForTest(modelID string, state InstanceState) {
	instMu.Lock()
	defer instMu.Unlock()
	if inst, ok := instances[modelID]; ok {
		inst.info.State = state
		return
	}
	instances[modelID] = &chatInstance{info: InstanceInfo{ModelID: modelID, State: state}}
}

// StopModelInstance kills modelID's running process (if any) via procs and
// clears the in-process instance record. No-op (not an error) if the model
// was never started or is already stopped.
// OwnsModelInstance reports whether THIS process spawned modelID's running
// server (as opposed to having merely adopted one already running from a
// different ocode process via ProbeModelInstance). StopModelInstance is a
// no-op when this is false — callers that need to actually stop/restart the
// server (e.g. /localmodel limit changing max_parallel) must check this
// first and tell the user which process owns it instead of silently no-op'ing
// a stop and then reporting success on a config value the live server never
// picked up.
func OwnsModelInstance(modelID string) bool {
	instMu.Lock()
	defer instMu.Unlock()
	inst, ok := instances[modelID]
	return ok && inst.processID != ""
}

func StopModelInstance(procs *tool.ProcessRegistry, modelID string) error {
	instMu.Lock()
	inst, ok := instances[modelID]
	if !ok || inst.processID == "" {
		instMu.Unlock()
		return nil
	}
	procID := inst.processID
	warmupCancel := inst.warmupCancel
	instMu.Unlock()
	if warmupCancel != nil {
		warmupCancel()
	}

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

// SetModelInstancePID records the OS process id of a running instance (for
// memory reporting — see RSSBytes). Same call-site rule as
// SetModelInstanceProcessID: call it from inside the spawn closure right
// after the process starts, not after StartModelInstance returns.
func SetModelInstancePID(modelID string, pid int) {
	instMu.Lock()
	defer instMu.Unlock()
	if inst, ok := instances[modelID]; ok {
		inst.info.PID = pid
	}
}

// SetModelInstanceProcessID records the tool.ProcessRegistry id for a running
// instance so StopModelInstance can find it later. Must be called from inside
// the spawn closure passed to StartModelInstance, immediately after
// procs.StartBackground succeeds — NOT after StartModelInstance returns.
// StartModelInstance's health-poll loop can take minutes (cold model
// download) and return an error on timeout even though the process is
// genuinely running; recording the id only on success would leave that
// process un-trackable and un-killable (StopModelInstance would report
// nothing to stop while the real process keeps holding its port).
func SetModelInstanceProcessID(modelID, processID string) {
	instMu.Lock()
	defer instMu.Unlock()
	if inst, ok := instances[modelID]; ok {
		inst.processID = processID
	}
}
