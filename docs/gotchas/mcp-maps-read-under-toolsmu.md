---
type: Gotcha
title: MCP tool maps are read under toolsMu, never bare
description: Why every read of a.tools, a.mcpTools and a.mcpServers takes toolsMu (snapshot under RLock, release, then call tool methods), why an unlocked read can kill the TUI, and the one loop that still reads a.tools without the lock.
resource: internal/agent/discovery_glue.go
tags:
  - gotcha
  - mcp
  - concurrency
  - tools
  - discovery
timestamp: 2026-11-01T05:28:00Z
status: active
---
# MCP tool maps are read under toolsMu, never bare

## Rule

After construction, `a.tools`, `a.mcpTools` and `a.mcpServers` are written under `a.toolsMu.Lock()`. The writers include `AddTools` and `SetWorkDir` (which swaps the advisor tool) in `internal/agent/agent.go`, `addMCPTools` and `RestoreMCPToolNames`, `markMCPFrom` on the child agent, and the `discover_more` registration in `ensureDiscovery`.

Every read takes `a.toolsMu.RLock()`, copies what it needs, and releases the lock before it calls `Description()`, `Definition()` or any other tool method. The reads that follow this rule are `pinnedIDs`, `mcpToolSnapshot`, `discoveryAllows`, `expandMCPServerGroups`, `markMCPFrom`, `MCPToolNames`, `MCPToolCount`, `contentGuardSourceFor`, `buildTypesafePermissionState`, and the `browser` tool lookup in `browserRunRun.subStep`.

## Why

Go's concurrent map read and write is a fatal runtime error, not a panic. `recover` cannot catch it, and deferred functions do not run. In the TUI this kills the process inside the alt screen before any terminal reset can happen. The write does not have to be slow to trigger it. A single unlocked iteration that overlaps one `addMCPTools` call is enough.

## Trigger

The TUI starts the async MCP load from `Init` and from `installAgent` (`internal/tui/model.go`), and its message handler calls `AddMCPTools` when the load finishes. `Step` calls `RunDiscoveryForMessages`, which runs `runDiscovery` and its pinned and expansion reads. The load can finish while a turn is inside `Step`, so the readers must take the lock.

## Test

`TestPinnedAndExpandMCPServersRaceAddMCPTools` in `internal/agent/discovery_glue_test.go` runs the discovery readers against a concurrent `AddMCPTools`. Run it with `-race`. An unlocked reader fails with a data race report.

## Open item

`GetToolDefinitions` in `internal/agent/agent.go` still ranges over `a.tools` without `toolsMu`, then calls `discoveryAllows`, which takes the lock. A concurrent `addMCPTools` can race that loop. The change that added this page does not fix it. Do not rely on the invariant for that path until it is fixed.

Related: [Discovery MCP Tool Gating](concepts/discovery-mcp-tool-gating.md).
