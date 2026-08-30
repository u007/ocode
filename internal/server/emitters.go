package server

import (
	"encoding/json"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/process"
	"github.com/u007/ocode/internal/debuglog"
	"github.com/u007/ocode/internal/usage"
)

// This file owns the server-push emitters that replace client-side polling:
// agent-run trees (runs), git working-tree status, and daily spending. All
// three are subscriber-aware — they only do work while at least one /api/events
// client is connected — so an idle server with no browsers attached burns no
// cycles. Runs events are session-scoped; git/spending are per-project /
// process-global and scoped to the projects subscribers declare they view.
//
// Every emitter loop self-exits ~30s after the last subscriber disconnects and
// is restarted on the next connection (guarded by an atomic flag), so handler
// instances created by tests never leak goroutines.

const (
	// runsPollInterval matches the legacy per-connection runs SSE poll.
	runsPollInterval = 750 * time.Millisecond
	// gitPollInterval mirrors the 10s client poll it replaces.
	gitPollInterval = 10 * time.Second
	// spendingPollInterval mirrors the 60s client poll it replaces.
	spendingPollInterval = 60 * time.Second
	// emitterIdleExit is how long a poller keeps ticking with zero
	// subscribers before exiting its loop.
	emitterIdleExit = 30 * time.Second
)

// startRunsEmitter launches the process-wide runs poller once per handler.
// Called from HandleEvents; safe to call repeatedly.
func (h *Handler) startRunsEmitter() {
	if !h.runsEmitterOn.CompareAndSwap(false, true) {
		return
	}
	go h.runsEmitterLoop()
}

// runsEmitterLoop polls the run registry of every session with a live agent and
// publishes a `runs` envelope whenever a session's serialised tree changes. The
// legacy HandleRunsStream polls per connection; this single poller makes the
// same events flow to /api/events consumers without a legacy stream open.
func (h *Handler) runsEmitterLoop() {
	ticker := time.NewTicker(runsPollInterval)
	defer ticker.Stop()
	last := make(map[string]string) // sessionID -> last serialised tree
	idleFor := time.Duration(0)
	for range ticker.C {
		if h.bus.SubscriberCount() == 0 {
			idleFor += runsPollInterval
			if idleFor >= emitterIdleExit {
				h.runsEmitterOn.Store(false)
				return
			}
			continue
		}
		idleFor = 0

		h.mu.Lock()
		ids := make([]string, 0, len(h.agents))
		for id := range h.agents {
			ids = append(ids, id)
		}
		h.mu.Unlock()

		// Prune cached trees for agents released since the last tick so the
		// map tracks live sessions only (otherwise it kept an entry — and its
		// serialized run tree — for every session id ever served).
		liveIDs := make(map[string]struct{}, len(ids))
		for _, id := range ids {
			liveIDs[id] = struct{}{}
		}
		for id := range last {
			if _, ok := liveIDs[id]; !ok {
				delete(last, id)
			}
		}

		for _, id := range ids {
			data, err := json.Marshal(h.runsSnapshot(id))
			if err != nil {
				// Our own DTOs must always marshal; log rather than spin.
				log.Printf("runs emitter: failed to encode run tree for %s: %v", id, err)
				continue
			}
			if string(data) == last[id] {
				continue
			}
			last[id] = string(data)
			project := ""
			if e := h.sessions.Lookup(id); e != nil {
				project = e.ProjectRoot
			}
			h.bus.Publish("runs", project, id, json.RawMessage(data))
		}
	}
}

// startWatchEmitters launches the git/spending watcher loop once per handler.
// Called from HandleEvents; safe to call repeatedly.
func (h *Handler) startWatchEmitters() {
	if !h.watchEmittersOn.CompareAndSwap(false, true) {
		return
	}
	go h.watchEmittersLoop()
}

// watchEmittersLoop drives the subscriber-aware git-status and spending
// emitters on one 1s master ticker. Git status is computed per viewed project:
// an initial snapshot when the project becomes viewed, then on the 10s
// boundary whenever it changed. Spending is process-global, computed on the 60s
// boundary whenever it changed (usage records only move during turns).
func (h *Handler) watchEmittersLoop() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	lastGit := make(map[string]string) // project -> serialised GitStatus
	lastSpending := ""
	idleFor := time.Duration(0)
	ticks := 0
	for range ticker.C {
		if h.bus.SubscriberCount() == 0 {
			idleFor += time.Second
			if idleFor >= emitterIdleExit {
				h.watchEmittersOn.Store(false)
				return
			}
			continue
		}
		idleFor = 0

		viewed := h.bus.ViewedProjects()
		seen := make(map[string]bool, len(viewed))
		for _, p := range viewed {
			seen[p] = true
			if _, known := lastGit[p]; !known || ticks%int(gitPollInterval.Seconds()) == 0 {
				status := gitStatusForDir(p)
				data, _ := json.Marshal(status)
				if string(data) != lastGit[p] {
					lastGit[p] = string(data)
					h.bus.Publish("git_status", p, "", status)
				}
			}
		}
		// Drop state for projects no subscriber views anymore, so a returning
		// client gets a fresh initial snapshot.
		for p := range lastGit {
			if !seen[p] {
				delete(lastGit, p)
			}
		}

		if ticks%int(spendingPollInterval.Seconds()) == 0 {
			if total, records, err := computeSpending(); err == nil {
				data := map[string]any{"spending_usd": total, "records": records}
				raw, _ := json.Marshal(data)
				if string(raw) != lastSpending {
					lastSpending = string(raw)
					h.bus.Publish("spending", "", "", data)
				}
			}
		}
		ticks++
	}
}

// startLogsEmitter launches the subscriber-aware debug-log pump once per
// handler. Called from HandleEvents; safe to call repeatedly. Previously logs
// only reached the bus while a legacy /api/logs/stream consumer was connected;
// Part 04 removes that client, so this pump is the sole bus source.
func (h *Handler) startLogsEmitter() {
	if !h.logsEmitterOn.CompareAndSwap(false, true) {
		return
	}
	go h.logsEmitterLoop()
}

// logsEmitterLoop watches the in-memory debug log and publishes new entries as
// `logs` envelopes while at least one /api/events subscriber is connected.
// Self-exits ~30s after the last subscriber leaves, like the other emitters.
func (h *Handler) logsEmitterLoop() {
	notify := debuglog.Log.Notify()
	lastCount := len(debuglog.Log.Snapshot())
	idleFor := time.Duration(0)
	for {
		select {
		case <-notify:
			if h.bus.SubscriberCount() == 0 {
				continue // don't accumulate work while idle
			}
			entries := debuglog.Log.Snapshot()
			for _, e := range entries[lastCount:] {
				entry := map[string]string{
					"kind":    string(e.Kind),
					"message": e.Message,
				}
				h.bus.Publish("logs", "", "", entry)
			}
			if len(entries) > lastCount {
				lastCount = len(entries)
			}
		case <-time.After(2 * time.Second):
			// Heartbeat tick: exit when idle long enough and reset the count
			// watermark (the ring buffer may have wrapped while idle).
			if h.bus.SubscriberCount() == 0 {
				idleFor += 2 * time.Second
				if idleFor >= emitterIdleExit {
					h.logsEmitterOn.Store(false)
					return
				}
			} else {
				idleFor = 0
				if n := len(debuglog.Log.Snapshot()); n < lastCount {
					lastCount = 0 // ring wrapped — resync
				}
			}
		}
	}
}

// terminalProcsPollInterval is how often the terminal-processes emitter
// re-samples CPU/mem. Faster than gitPollInterval since the whole point is
// catching a runaway process while it's happening.
const terminalProcsPollInterval = 1500 * time.Millisecond

// terminalProcessStat is one row of the Processes tab: a terminal's id (as
// assigned by TerminalPanel) plus its process tree's summed CPU/mem.
type terminalProcessStat struct {
	ID         string  `json:"id"`
	PID        int32   `json:"pid"`
	CPUPercent float64 `json:"cpu_percent"`
	MemBytes   uint64  `json:"mem_bytes"`
	Command    string  `json:"command"`
}

// startTerminalProcessesEmitter launches the terminal CPU/mem poller once per
// handler. Called from HandleEvents; safe to call repeatedly.
func (h *Handler) startTerminalProcessesEmitter() {
	if !h.terminalProcsEmitterOn.CompareAndSwap(false, true) {
		return
	}
	go h.terminalProcessesEmitterLoop()
}

// terminalProcessesEmitterLoop samples every registered terminal's process
// tree (shell + all descendants — a nested `ocode`, its `/rc` server, etc.)
// and publishes one `terminal_processes` envelope per project with that
// project's terminals. gopsutil's Percent(0) reports the delta since the last
// call *on the same *process.Process value*, so process handles are cached
// across ticks (a fresh NewProcess each tick would always read 0%); the cache
// is pruned each tick to drop pids that exited.
//
// gatherProcessStats is the single helper for walking a snapshot of terminal
// entries. Both the emitter (long-lived cache) and the REST handler (throwaway
// cache) share it so sumProcessTree fixes stay in one place. touched is the
// per-tick/per-request dedup for pid reuse and the cycle guard — it must be
// shared across all terminals in a single snapshot to avoid double-counting.
func gatherProcessStats(entries map[string]terminalProcEntry, cache map[int32]*process.Process, touched map[int32]bool) []terminalProcessStat {
	childPids := processChildrenSnapshot()
	stats := make([]terminalProcessStat, 0, len(entries))
	for id, entry := range entries {
		cpu, mem := sumProcessTree(entry.PID, cache, touched, childPids)
		cmd := terminalCommand(entry.PID, cache, childPids)
		stats = append(stats, terminalProcessStat{ID: id, PID: entry.PID, CPUPercent: cpu, MemBytes: mem, Command: cmd})
	}
	return stats
}

// processChildrenSnapshot reads the process table once and returns a
// ppid → child-pids map. gopsutil's Children() re-reads the entire process
// table on every call (SysctlKinfoProcSlice on darwin), and the tree walks
// below called it once per node per tick — the second-largest allocation
// source in the server. One snapshot per gatherProcessStats call replaces
// all of those reads.
func processChildrenSnapshot() map[int32][]int32 {
	procs, err := process.Processes()
	if err != nil {
		log.Printf("terminal processes: list process table: %v", err)
		return nil
	}
	childPids := make(map[int32][]int32, len(procs))
	for _, p := range procs {
		ppid, err := p.Ppid()
		if err != nil {
			// intentionally not logged: pid exited between listing and lookup
			continue
		}
		childPids[ppid] = append(childPids[ppid], p.Pid)
	}
	return childPids
}

// shellNames are base executable names we treat as the interactive shell itself
// rather than a command the user is actively running.
var shellNames = map[string]bool{
	"zsh": true, "bash": true, "sh": true, "fish": true, "dash": true,
	"ksh": true, "tcsh": true, "csh": true, "powershell": true, "pwsh": true,
	"cmd": true, "login": true, "tmux": true, "screen": true,
}

// isInteractiveShell reports whether cl looks like a shell started with only
// login/interactive flags (e.g. "zsh -i -l") rather than running a command.
func isInteractiveShell(cl string) bool {
	fields := strings.Fields(cl)
	if len(fields) == 0 {
		return false
	}
	// A leading dash (e.g. "-zsh") marks a login shell in process listings.
	base := strings.TrimPrefix(filepath.Base(fields[0]), "-")
	if !shellNames[base] {
		return false
	}
	for _, f := range fields[1:] {
		if !strings.HasPrefix(f, "-") {
			return false
		}
	}
	return true
}

// collectCmdlines walks pid and its descendants, returning every non-empty
// command line in the tree (depth-first). touched guards against cycles and the
// shared cache of process handles is populated as we descend. childPids is the
// per-call process-table snapshot from processChildrenSnapshot.
func collectCmdlines(p *process.Process, cache map[int32]*process.Process, touched map[int32]bool, childPids map[int32][]int32) []string {
	if touched[p.Pid] {
		return nil
	}
	touched[p.Pid] = true
	var out []string
	if cl, err := p.Cmdline(); err == nil && cl != "" {
		out = append(out, cl)
	}
	for _, cpid := range childPids[p.Pid] {
		c, ok := cache[cpid]
		if !ok {
			nc, err := process.NewProcess(cpid)
			if err != nil {
				// intentionally not logged: pid exited between snapshot and walk
				continue
			}
			c = nc
			cache[cpid] = c
		}
		out = append(out, collectCmdlines(c, cache, touched, childPids)...)
	}
	return out
}

// terminalCommand returns a best-effort human-readable command for a terminal's
// process tree. It prefers a descendant that is actually running something
// (e.g. "npm run dev") over the interactive shell itself, and falls back to the
// shell's bare name when the terminal is idle (no current command). The walk
// reuses the shared handle cache but its own touched set so it never disturbs
// the CPU/mem cycle guard in sumProcessTree.
func terminalCommand(pid int32, cache map[int32]*process.Process, childPids map[int32][]int32) string {
	root, ok := cache[pid]
	if !ok {
		np, err := process.NewProcess(pid)
		if err != nil {
			return ""
		}
		root = np
		cache[pid] = root
	}
	touched := make(map[int32]bool)
	cmdlines := collectCmdlines(root, cache, touched, childPids)
	for _, cl := range cmdlines {
		if cl != "" && !isInteractiveShell(cl) {
			return cl
		}
	}
	// Idle: only the shell is present — report its bare name, not its argv
	// (e.g. "zsh", never "zsh -i -l").
	if name, err := root.Name(); err == nil && name != "" {
		return name
	}
	return ""
}

func (h *Handler) terminalProcessesEmitterLoop() {
	ticker := time.NewTicker(terminalProcsPollInterval)
	defer ticker.Stop()
	cache := make(map[int32]*process.Process)
	idleFor := time.Duration(0)

	publish := func() {
		entries := h.terminalProcs.snapshot()
		if len(entries) == 0 {
			return
		}
		touched := make(map[int32]bool)
		stats := gatherProcessStats(entries, cache, touched)
		byProject := make(map[string][]terminalProcessStat, len(entries))
		for _, s := range stats {
			// entries[s.ID] is safe: gatherProcessStats preserves every id.
			proj := entries[s.ID].Project
			byProject[proj] = append(byProject[proj], s)
		}
		for pid := range cache {
			if !touched[pid] {
				delete(cache, pid)
			}
		}
		for proj, stats := range byProject {
			h.bus.Publish("terminal_processes", proj, "", stats)
		}
	}

	// Push an immediate snapshot if a terminal is already registered when the
	// loop starts — avoids making a freshly opened terminal wait a full tick
	// for its first memory reading. The initial CPU% will be 0 (Percent needs
	// one prior sample), but memory is valid and the next tick corrects CPU.
	if h.bus.SubscriberCount() > 0 {
		publish()
	}

	for {
		select {
		case <-ticker.C:
			if h.bus.SubscriberCount() == 0 {
				idleFor += terminalProcsPollInterval
				if idleFor >= emitterIdleExit {
					h.terminalProcsEmitterOn.Store(false)
					return
				}
				continue
			}
			idleFor = 0
			publish()
		case <-h.terminalProcsWake:
			if h.bus.SubscriberCount() == 0 {
				continue
			}
			idleFor = 0
			publish()
		}
	}
}

// sumProcessTree walks pid and all its descendants, summing CPU% and RSS
// bytes. cache holds *process.Process handles across ticks so Percent(0) has
// a prior sample to diff against; touched records every pid visited this
// tick so the caller can prune cache entries for processes that exited. A
// pid that has already exited (or was never valid) contributes zero rather
// than erroring the whole terminal's row. childPids is the per-call
// process-table snapshot from processChildrenSnapshot.
func sumProcessTree(pid int32, cache map[int32]*process.Process, touched map[int32]bool, childPids map[int32][]int32) (cpuPercent float64, memBytes uint64) {
	if touched[pid] {
		return 0, 0 // cycle guard; process trees are acyclic in practice
	}
	touched[pid] = true

	p, ok := cache[pid]
	if !ok {
		np, err := process.NewProcess(pid)
		if err != nil {
			return 0, 0
		}
		p = np
		cache[pid] = p
	}

	if pct, err := p.Percent(0); err == nil {
		cpuPercent += pct
	}
	if mi, err := p.MemoryInfo(); err == nil && mi != nil {
		memBytes += mi.RSS
	}

	for _, cpid := range childPids[pid] {
		childCPU, childMem := sumProcessTree(cpid, cache, touched, childPids)
		cpuPercent += childCPU
		memBytes += childMem
	}
	return cpuPercent, memBytes
}

// computeSpending returns today's cumulative USD spend and record count, shared
// by the legacy GET /api/spending endpoint and the spending emitter.
func computeSpending() (total float64, records int, err error) {
	now := time.Now()
	from := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	recs, err := usage.Query(from, now)
	if err != nil {
		return 0, 0, err
	}
	for _, rec := range recs {
		total += rec.Spend
	}
	return total, len(recs), nil
}
