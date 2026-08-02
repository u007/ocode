package discovery

import (
	"context"
	"fmt"
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
	info      InstanceInfo
	processID string // tool.Process.ID, empty when stopped
}

var (
	instMu    sync.Mutex
	instances = map[string]*chatInstance{}
)

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
func StartModelInstance(spawn func(cmdline string) error, modelID string, port int, maxParallel int, cacheDir string) error {
	man, ok := ChatManifestForHost(modelID)
	if !ok {
		return fmt.Errorf("no local chat manifest for model %q on %s/%s", modelID, goos(), goarch())
	}

	if info, ok := ProbeModelInstance(modelID, port); ok {
		instMu.Lock()
		info.MaxParallel = maxParallel
		instances[modelID] = &chatInstance{info: info} // processID stays empty — we don't own this process
		instMu.Unlock()
		return nil
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
	// First load downloads the real weights (multi-GB on MLX, which fetches
	// inside mlx_lm.server before /v1/models responds; llama.cpp downloads
	// up front via EnsureArtifact). Allow 5 minutes instead of the embedder's
	// 60s so a cold first enable on a slow connection doesn't time out.
	const chatHealthPollAttempts = 300
	for i := 0; i < chatHealthPollAttempts; i++ {
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
	argv := filterMLXArgs(man.LaunchArgv, man.MLXRepo, port, maxParallel, mlxServerFlags())
	cmdline := strings.Join(argv, " ")
	emitUserDiscoveryDebug("DISCOVERY", "spawning MLX chat server: "+cmdline)
	if err := spawn(cmdline); err != nil {
		return fmt.Errorf("spawn MLX chat server: %w", err)
	}
	return nil
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
		// Running --help is cheap and side-effect free; the server exits
		// after printing usage. Bounded timeout in case python3/mlx_lm's
		// import chain is slow or python3 is missing from PATH — an
		// unbounded exec here would hang the caller indefinitely.
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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
