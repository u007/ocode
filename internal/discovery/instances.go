package discovery

import (
	"fmt"
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
