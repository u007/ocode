package server

import (
	"encoding/json"
	"log"
	"time"

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
