---
type: Gotcha
title: Bubble Tea Cleanup Race — Orphaned Goroutines on Shutdown
description: Bubble Tea runs tea.Cmd in untracked goroutines and does not wait for them during Program.Run shutdown, causing cleanup races with model-switch or /new commands that orphan agents or supervisors.
tags:
  - bubbletea
  - tui
  - goroutine
  - cleanup
  - shutdown
  - process-leak
timestamp: 2026-08-26T15:06:55Z
---
## The Problem

Bubble Tea runs every `tea.Cmd` in an untracked goroutine (via `go cmd()`). When `Program.Run()` returns, it closes the message channel but **does not wait** for in-flight commands to finish. This creates a race window during shutdown or session replacement:

1. A model-switch (`/model`) or session reset (`/new`) fires a cleanup command that tears down an agent or supervisor.
2. Concurrently, a new agent is installed.
3. The old agent's cleanup goroutine — or a late-arriving command from the old agent — touches shared state (event bus, process registry, MCP cache) after the new agent has already taken over.

The symptom is orphaned child processes (local model servers, background bash commands), stale event subscriptions, or "session stuck" states where the old agent's resources never fully release.

## Why It Happens

- `tea.Program.Send()` is fire-and-forget; no completion tracking.
- `Program.Kill()` / `Program.Quit()` closes the channel but does not `WaitGroup.Wait()` on in-flight commands.
- Model-switch and `/new` both retire the old agent asynchronously (`Agent.Shutdown()`) while installing a fresh one — the old agent's cleanup is itself a `tea.Cmd` that races with the new agent's initialization.

## Proposed Solution Pattern

A **replacement tracker** with explicit command registration, cancellation/join, and resource ownership:

### 1. Command Registration

Every long-lived `tea.Cmd` (cleanup, agent teardown, supervisor shutdown) registers itself in a `sync.WaitGroup`-backed tracker before starting work. On completion, it deregisters.

```go
type cmdTracker struct {
    wg sync.WaitGroup
}

func (t *cmdTracker) Register() func() {
    t.wg.Add(1)
    return t.wg.Done // caller defers this
}
```

### 2. Shutdown Join

Before installing a replacement agent, the TUI model joins all in-flight commands:

```go
func (m *model) replaceAgent(newAgent Agent) {
    // Cancel old agent context
    m.oldAgentCancel()

    // Wait for old agent's in-flight cmds to drain
    m.cmdTracker.Wait() // blocks until all registered cmds finish

    // Now safe to install the new agent
    m.agent = newAgent
}
```

### 3. Resource Ownership via Context

Each agent owns a `context.Context` with a cancellation function. Cleanup commands derived from that context are automatically cancelled when the agent is retired:

```go
ctx, cancel := context.WithCancel(context.Background())
defer cancel() // retire the agent

cmd := func() tea.Msg {
    // work that respects ctx.Done()
    select {
    case <-ctx.Done():
        return cleanupAbortedMsg{}
    default:
        // proceed with cleanup
    }
}
```

### 4. Deterministic Cleanup

Combined, the tracker ensures:
- **No orphaned goroutines**: `Wait()` blocks until cleanup finishes.
- **No stale mutations**: context cancellation aborts in-flight work before the new agent starts.
- **No process leaks**: the process registry's supervisor is shut down before the new agent creates its own.

## Related Gotchas

- [Agent Replacement — Input Queuing & Stream Event Epochs](agent-replacement-input-queuing.md) — the upstream problem this pattern solves for user input and stream events.
- [Foreground Bash Commands — Parent-Death Protection Before Start](foreground-bash-parent-death-protection.md) — process lifecycle protection that must coordinate with cleanup.
- [Local Model Limiter — Stale-Slot Reclamation Race](local-model-limiter-stale-slot-race.md) — another TOCTOU race in resource cleanup.

## Status

Proposed architecture. Not yet implemented. The existing epoch-tagged stream events and input-queuing during replacement (see `agent-replacement-input-queuing.md`) mitigate the symptom for user input and transcript events, but do not fully cover the goroutine-lifecycle gap described here.
