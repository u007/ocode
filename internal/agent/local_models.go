package agent

import (
	"fmt"
	"sync"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/discovery"
	"github.com/u007/ocode/internal/tool"
)

// StartLocalModelInstance resolves id's deterministic port from a disk-fresh
// registered-id set (config.RegisteredLocalModelIDs — so concurrent ocode
// processes agree on the same port even if THIS process's in-memory config is
// stale) and spawns it through ag's supervised process registry. Blocks until
// the instance is healthy or the attempt fails (a cold model download can
// take minutes — see StartModelInstance's chatHealthPollAttempts) — callers
// on a UI thread must run this off the update loop.
func StartLocalModelInstance(ag *Agent, id string, maxParallel int) error {
	registeredIDs, err := config.RegisteredLocalModelIDs()
	if err != nil {
		return err
	}
	port, err := discovery.AssignChatPort(id, registeredIDs)
	if err != nil {
		return err
	}
	procs := ag.Procs()
	spawn := func(cmdline string) error {
		p := procs.StartBackground(cmdline)
		if p != nil && p.SnapshotStatus() == tool.ProcExited {
			return fmt.Errorf("local chat server process exited immediately on spawn")
		}
		// Recorded immediately, before StartModelInstance's health-poll loop
		// runs — a poll timeout must not leave a running process untracked
		// (see SetModelInstanceProcessID's doc comment).
		discovery.SetModelInstanceProcessID(id, p.ID)
		discovery.SetModelInstancePID(id, p.PID)
		return nil
	}
	if err := discovery.StartModelInstance(spawn, id, port, maxParallel, DiscoveryCacheDir()); err != nil {
		// StartModelInstance can fail after already spawning a process (e.g.
		// the health-poll timed out while the server was still loading) — if
		// this call owns that process, kill it instead of leaving it orphaned
		// and still holding/contending for the port, which is what let a
		// later retry (or a concurrent ocode session) spawn a SECOND
		// competing server on the same port.
		if discovery.OwnsModelInstance(id) {
			_ = discovery.StopModelInstance(procs, id)
		}
		return err
	}
	return nil
}

// autoStartLocalModelsOnce ensures the enabled-local-model auto-start scan
// (autoStartEnabledLocalModels) runs at most once per OS process, regardless
// of how many Agents get constructed (sub-agents, per-request agents in the
// web server, etc. all call NewAgent) — only the process's first Agent
// triggers it.
var autoStartLocalModelsOnce sync.Once

// autoStartEnabledLocalModels fires a background goroutine per model in
// cfg.Ocode.LocalModels with Enabled=true, so a model left enabled from a
// prior session comes back up without the user re-running
// "/localmodel enable" every time they start ocode. Each start is
// probe-before-spawn (StartModelInstance) and cross-process safe: if another
// ocode process already has the model's server running, this call adopts it
// (no download/spawn) instead of racing to start a second one. Failures are
// logged via emitDebug, not surfaced as UI messages, since this also runs in
// headless mode.
func autoStartEnabledLocalModels(ag *Agent, cfg *config.Config) {
	if cfg == nil || len(cfg.Ocode.LocalModels) == 0 {
		return
	}
	for id, lm := range cfg.Ocode.LocalModels {
		if !lm.Enabled {
			continue
		}
		if lm.MaxParallel <= 0 {
			emitDebug("LOCALMODEL", fmt.Sprintf("auto-start skipped for %s: max_parallel is %d (must be >0)", id, lm.MaxParallel))
			continue
		}
		id, lm := id, lm
		go func() {
			if err := StartLocalModelInstance(ag, id, lm.MaxParallel); err != nil {
				emitDebug("LOCALMODEL", fmt.Sprintf("auto-start failed for %s: %v", id, err))
				return
			}
			emitDebug("LOCALMODEL", fmt.Sprintf("auto-start: %s ready (max_parallel=%d)", id, lm.MaxParallel))
		}()
	}
}
