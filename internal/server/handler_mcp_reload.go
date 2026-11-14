package server

import (
	"log"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/crashguard"
	"github.com/u007/ocode/internal/plugins"
	"github.com/u007/ocode/internal/tool"
)

// The MCP reload is asynchronous: enumerating servers can take longer than a
// browser request should wait (an unreachable server costs the full
// bootstrapMCPTimeout), so POST returns 202 immediately and the caller polls
// GET. Proxy requests (remote projects) forward both through the existing
// /api/remote/{host}/api/* catch-all, so nothing else has to change.

// mcpReloadPhase is the lifecycle state reported by GET /api/mcp/reload.
type mcpReloadPhase string

const (
	mcpReloadIdle    mcpReloadPhase = "idle"
	mcpReloadRunning mcpReloadPhase = "reloading"
	mcpReloadDone    mcpReloadPhase = "reloaded"
	mcpReloadFailed  mcpReloadPhase = "failed"
)

// mcpReloadStatus is the observable state of the process-wide reload. It is
// guarded by Handler.mcpReloadMu, never h.mu: the reload holds h.mu on and off
// and a status read must never wait behind it.
type mcpReloadStatus struct {
	phase      mcpReloadPhase
	generation uint64
	tools      int
	errs       []string
	finishedAt time.Time
}

// mcpReloadDTO is the GET body. errors is always an array so a client can map
// it without a nil check.
type mcpReloadDTO struct {
	Status     string   `json:"status"`
	Generation uint64   `json:"generation"`
	Tools      int      `json:"tools"`
	Errors     []string `json:"errors"`
	FinishedAt string   `json:"finished_at"`
}

// mcpReloadOutcome is one completed load: everything the swap needs, produced
// off the request goroutine so no handler lock is held while enumerating.
type mcpReloadOutcome struct {
	cfg       *config.Config
	pluginMCP map[string]string
	cache     *mcpCache
	tools     []tool.Tool
	errs      []string
	timedOut  bool
	// loadErr short-circuits the run: the on-disk config could not be read, so
	// there is nothing to apply and the live cache must not move.
	loadErr error
}

// HandleReloadMCP starts a reload and reports 202. A second POST while one is
// in flight neither starts a second run nor errors (which would make a client
// retry-loop): it reports the SAME generation so pollers agree on which run
// they are watching.
func (h *Handler) HandleReloadMCP(w http.ResponseWriter, r *http.Request) {
	gen, started := h.beginMCPReload()
	if started {
		crashguard.Go(func() { h.runMCPReload(gen) })
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"status":     string(mcpReloadRunning),
		"generation": gen,
	})
}

// HandleGetMCPReload reports the current reload state. finished_at is empty
// until a run has completed, so a client cannot mistake "never ran" for
// "finished now".
func (h *Handler) HandleGetMCPReload(w http.ResponseWriter, r *http.Request) {
	st := h.mcpReloadSnapshot()
	finished := ""
	if !st.finishedAt.IsZero() {
		finished = st.finishedAt.UTC().Format(time.RFC3339)
	}
	writeJSON(w, http.StatusOK, mcpReloadDTO{
		Status:     string(st.phase),
		Generation: st.generation,
		Tools:      st.tools,
		Errors:     st.errs,
		FinishedAt: finished,
	})
}

// beginMCPReload claims the single reload slot. started is false when a run is
// already in flight, in which case gen is that run's generation.
func (h *Handler) beginMCPReload() (gen uint64, started bool) {
	h.mcpReloadMu.Lock()
	defer h.mcpReloadMu.Unlock()
	if h.mcpReload.phase == mcpReloadRunning {
		return h.mcpReload.generation, false
	}
	h.mcpReload.generation++
	h.mcpReload.phase = mcpReloadRunning
	h.mcpReload.tools = 0
	h.mcpReload.errs = nil
	h.mcpReload.finishedAt = time.Time{}
	return h.mcpReload.generation, true
}

// publishMCPReload records a completed run. A verdict from an older generation
// is dropped, so a late goroutine cannot overwrite a newer run's state.
func (h *Handler) publishMCPReload(gen uint64, phase mcpReloadPhase, tools int, errs []string) {
	h.mcpReloadMu.Lock()
	defer h.mcpReloadMu.Unlock()
	if h.mcpReload.generation != gen {
		return
	}
	h.mcpReload.phase = phase
	h.mcpReload.tools = tools
	h.mcpReload.errs = append([]string{}, errs...)
	h.mcpReload.finishedAt = time.Now()
}

// mcpReloadSnapshot copies the state so the caller never reads it unlocked.
func (h *Handler) mcpReloadSnapshot() mcpReloadStatus {
	h.mcpReloadMu.Lock()
	defer h.mcpReloadMu.Unlock()
	st := h.mcpReload
	st.errs = append([]string{}, st.errs...)
	if st.phase == "" {
		st.phase = mcpReloadIdle
	}
	return st
}

// runMCPReload performs one background reload. Config and plugin state are
// applied even when enumeration fails, because the on-disk config really did
// change; only the cache swap is conditional, and the published status is what
// tells the caller the tool set did not move.
func (h *Handler) runMCPReload(gen uint64) {
	// A panic must still publish a verdict: the only way back to "not
	// reloading" is a publish, so an unhandled panic would wedge the slot and
	// every later POST would report "reloading" forever.
	defer func() {
		if r := recover(); r != nil {
			log.Printf("mcp reload: panic: %v\n%s", r, debug.Stack())
			h.publishMCPReload(gen, mcpReloadFailed, 0, []string{"internal error during MCP reload"})
		}
	}()

	out := h.performMCPReload()
	if out.loadErr != nil {
		log.Printf("mcp reload: load config: %v", out.loadErr)
		h.publishMCPReload(gen, mcpReloadFailed, 0, []string{out.loadErr.Error()})
		return
	}

	h.mu.Lock()
	curCache := h.mcpCache
	h.mu.Unlock()
	oldTools := 0
	if curCache != nil {
		oldTools = curCache.toolsNow()
	}

	install := out.cache
	if !shouldSwapMCPCache(out.timedOut, len(out.tools), len(out.errs), oldTools) || install == nil {
		install = nil
	}
	h.applyLoadedConfig(out.cfg, out.pluginMCP, install)

	if install == nil {
		errs := out.errs
		if out.timedOut && len(errs) == 0 {
			errs = []string{"MCP enumeration did not finish within 30s; keeping the previous tool set"}
		}
		h.publishMCPReload(gen, mcpReloadFailed, len(out.tools), errs)
		return
	}
	// Only a swapped cache changed the tool set, so only then is a rebuild due.
	h.rebuildAgentsForFeatureChange("mcp reload")
	h.publishMCPReload(gen, mcpReloadDone, len(out.tools), out.errs)
}

// performMCPReload runs the loader, or the injected fake in tests. The real
// loader spawns MCP processes, so tests never reach it.
func (h *Handler) performMCPReload() mcpReloadOutcome {
	if fn := h.mcpReloadFn; fn != nil {
		return fn()
	}
	return h.loadMCPConfigForReload()
}

// loadMCPConfigForReload reads the config from disk, applies it to the agent
// registry, and enumerates MCP tools with ONE bounded wait (the same
// bootstrapMCPTimeout a session build uses). There is no second wait and no
// extra timeout: a straggler is reported as a timeout, not sampled later.
func (h *Handler) loadMCPConfigForReload() mcpReloadOutcome {
	cfg, err := config.Load()
	if err != nil {
		return mcpReloadOutcome{loadErr: err}
	}
	agent.ApplyAgentConfig(cfg)
	pluginMCP := plugins.MergeSharedMCPServers(cfg, h.workDir)

	cache := newMCPCache()
	cache.warm(cfg)
	tools, errs, timedOut := cache.waitTimeout(bootstrapMCPTimeout)
	return mcpReloadOutcome{
		cfg:       cfg,
		pluginMCP: pluginMCP,
		cache:     cache,
		tools:     tools,
		errs:      errs,
		timedOut:  timedOut,
	}
}

// shouldSwapMCPCache decides whether a finished load replaces the live cache.
// A timeout means enumeration never completed, and an all-errors load would
// replace a working tool set with nothing — both keep the old cache and report
// failure. Everything else swaps, including an empty config (removing every
// server is a real change).
func shouldSwapMCPCache(timedOut bool, tools, errs, oldTools int) bool {
	if timedOut {
		return false
	}
	if tools == 0 && errs > 0 && oldTools > 0 {
		return false
	}
	return true
}
