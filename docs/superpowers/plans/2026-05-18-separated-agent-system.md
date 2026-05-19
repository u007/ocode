# Separated Agent System Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add OpenCode-style separated Markdown agents so custom agents can be loaded from agent files and invoked by the LLM through `task` as isolated child-agent runs.

**Architecture:** Introduce a registry-backed agent model that unifies built-in and loaded agents. Move task subagent discovery from hard-coded slices to the registry, map supported OpenCode permissions into the existing permission manager, and persist task runs as child sessions linked to the parent.

**Tech Stack:** Go 1.23, existing `internal/agent`, `internal/session`, `internal/config`, and `internal/tool` packages.

---

## File Structure

- Create: `internal/agent/agent_registry.go` — unified `AgentDefinition`, registry construction, built-in registration, lookup, mode filtering, and deterministic ordering.
- Create: `internal/agent/agent_loader.go` — Markdown agent discovery, frontmatter parsing, validation, and load diagnostics.
- Create: `internal/agent/agent_permissions.go` — OpenCode permission group mapping into `PermissionManager` rules.
- Create: `internal/agent/child_session.go` — child session ID creation, metadata construction, and child transcript persistence helpers.
- Modify: `internal/agent/subagent.go` — update `TaskTool` to use registry-backed agents, child sessions, and per-agent permissions.
- Modify: `internal/agent/registry.go` — keep primary-agent behavior stable while sharing built-in definitions where practical.
- Modify: `internal/agent/registry_test.go` — replace hard-coded subagent assumptions with registry tests.
- Modify: `internal/session/session.go` — add small metadata helpers only if needed; avoid changing existing save/load behavior.
- Test: `internal/agent/agent_registry_test.go` — built-in registry, precedence, mode filtering, diagnostics, permissions, task schema, and child-session behavior.

## Task 1: Add Unified Agent Registry

**Files:**
- Create: `internal/agent/agent_registry.go`
- Modify: `internal/agent/registry_test.go`
- Test: `internal/agent/agent_registry_test.go`

- [ ] **Step 1: Write failing registry tests**

Add tests proving the registry contains built-in agents `build`, `plan`, `general`, `explore`, and `scout`, and that `general`, `explore`, and `scout` are returned by the subagent filter in deterministic order.

- [ ] **Step 2: Run focused tests and confirm failure**

Run: `go test ./internal/agent -run 'TestAgentRegistry|TestSubAgentSpecs' -v`

Expected: fail because the unified registry does not exist yet.

- [ ] **Step 3: Implement registry types and built-ins**

Create the registry file with a unified agent definition, mode constants for `primary`, `subagent`, and `all`, deterministic lookup methods, and built-in definitions that preserve current behavior for primary agents and subagents.

- [ ] **Step 4: Preserve existing public behavior**

Keep existing `FindAgentSpec`, `NextAgentSpec`, and current primary-agent switching behavior working while routing new subagent tests through the registry.

- [ ] **Step 5: Run focused tests**

Run: `go test ./internal/agent -run 'TestAgentRegistry|TestDefaultAgents|TestFindAgentSpec|TestNextAgentSpec|TestSubAgentSpecs' -v`

Expected: pass.

## Task 2: Load OpenCode-Style Markdown Agents

**Files:**
- Create: `internal/agent/agent_loader.go`
- Modify: `internal/agent/agent_registry.go`
- Test: `internal/agent/agent_registry_test.go`

- [ ] **Step 1: Write failing loader tests**

Add tests using temporary HOME and project directories that prove global agents load from `~/.config/opencode/agents/*.md`, project agents load from `.opencode/agents/*.md`, project agents override global agents, and custom agents override built-ins by name.

- [ ] **Step 2: Add validation tests**

Add tests proving missing prompt body, invalid mode, unsupported model/options fields, and malformed frontmatter produce diagnostics without blocking unrelated valid agents.

- [ ] **Step 3: Run focused tests and confirm failure**

Run: `go test ./internal/agent -run 'TestLoadMarkdownAgents|TestAgentRegistryPrecedence|TestAgentLoaderDiagnostics' -v`

Expected: fail because Markdown loading does not exist yet.

- [ ] **Step 4: Implement search paths and parsing**

Implement global and project agent discovery, parse OpenCode-style frontmatter, use filename as agent name, use Markdown body as system prompt, default missing mode to `all`, and sort loaded files deterministically.

- [ ] **Step 5: Implement diagnostics and precedence**

Store load diagnostics with file path and reason, keep valid agents loaded when another file is invalid, and apply precedence as project > global > built-in.

- [ ] **Step 6: Run focused tests**

Run: `go test ./internal/agent -run 'TestLoadMarkdownAgents|TestAgentRegistryPrecedence|TestAgentLoaderDiagnostics' -v`

Expected: pass.

## Task 3: Map Agent Permissions Safely

**Files:**
- Create: `internal/agent/agent_permissions.go`
- Modify: `internal/agent/permissions.go` only if a small exported helper is needed
- Test: `internal/agent/agent_registry_test.go`

- [ ] **Step 1: Write failing permission mapping tests**

Add tests proving `read`, `edit`, `glob`, `grep`, `bash`, `task`, `webfetch`, `skill`, `question`, and `lsp` map to existing tool permissions, and unknown groups produce diagnostics and deny-by-default behavior.

- [ ] **Step 2: Add pattern-specific bash deferral test**

Add a test proving object-form bash permissions produce a diagnostic and map bash to `ask` for Phase 1.

- [ ] **Step 3: Run focused tests and confirm failure**

Run: `go test ./internal/agent -run 'TestAgentPermissionMapping' -v`

Expected: fail because agent permission mapping does not exist yet.

- [ ] **Step 4: Implement permission mapping**

Implement a mapper from parsed OpenCode agent permissions into a fresh `PermissionManager`, using existing `SetRule` behavior and applying edit permissions to all ocode edit/write tool names currently recognized by the permission manager.

- [ ] **Step 5: Run focused tests**

Run: `go test ./internal/agent -run 'TestAgentPermissionMapping|TestPermissionManager' -v`

Expected: pass.

## Task 4: Persist Separated Child Sessions

**Files:**
- Create: `internal/agent/child_session.go`
- Modify: `internal/session/session.go` only if existing save metadata support is insufficient
- Test: `internal/agent/agent_registry_test.go`

- [ ] **Step 1: Write failing child-session tests**

Add tests proving a child session ID is generated from the parent session ID, agent name, and timestamp source; child metadata records parent session ID, agent name, start time, and completion status; and child messages persist separately through the existing session storage.

- [ ] **Step 2: Run focused tests and confirm failure**

Run: `go test ./internal/agent -run 'TestChildAgentSession' -v`

Expected: fail because child session helpers do not exist yet.

- [ ] **Step 3: Implement child-session helpers**

Add helpers that construct child session IDs, child metadata, and save child transcripts through `session.Save` without changing existing parent session save behavior.

- [ ] **Step 4: Run focused tests**

Run: `go test ./internal/agent -run 'TestChildAgentSession' -v`

Expected: pass.

## Task 5: Wire Registry and Child Runs Into Task Tool

**Files:**
- Modify: `internal/agent/subagent.go`
- Modify: `internal/agent/agent.go` only if child-run context needs a small setter or accessor
- Test: `internal/agent/agent_registry_test.go`

- [ ] **Step 1: Write failing task schema tests**

Add tests proving `TaskTool.Definition()` lists loaded registry subagents, includes hidden agents in enum, excludes hidden agents from the visible description text, excludes `mode: primary` agents from task options, and defaults to `general` only when no agent is specified.

- [ ] **Step 2: Write failing task execution tests**

Add tests proving an unknown requested agent returns a clear error, selected agent prompt is used, selected agent permissions restrict available tools, and task execution returns parent-visible output containing the child session ID.

- [ ] **Step 3: Run focused tests and confirm failure**

Run: `go test ./internal/agent -run 'TestTaskTool' -v`

Expected: fail because `TaskTool` still reads `DefaultSubAgents` and runs inline.

- [ ] **Step 4: Replace hard-coded subagent lookup**

Update `TaskTool.Definition()` and `TaskTool.Execute()` to query the registry, use registry diagnostics where useful, and error clearly on unknown explicit agent names.

- [ ] **Step 5: Apply per-agent permissions and tools**

Create child agents with tools filtered by the selected registry definition and permissions built from the selected agent file. Preserve built-in read-only restrictions for `explore` and `scout`.

- [ ] **Step 6: Persist child transcript and parent result**

Save child messages separately after the run, include child session metadata, and return a concise result to the parent that includes the child session ID and assistant output.

- [ ] **Step 7: Run focused tests**

Run: `go test ./internal/agent -run 'TestTaskTool|TestChildAgentSession' -v`

Expected: pass.

## Task 6: Full Verification and Docs Check

**Files:**
- Modify: `docs/superpowers/specs/2026-05-18-separated-agent-system-design.md` only if implementation reveals a spec correction
- Modify: `TODO.md` only if any Phase 1 requirement is intentionally deferred

- [ ] **Step 1: Run all agent tests**

Run: `go test ./internal/agent -v`

Expected: pass.

- [ ] **Step 2: Run session tests**

Run: `go test ./internal/session -v`

Expected: pass.

- [ ] **Step 3: Run full test suite**

Run: `go test ./...`

Expected: pass.

- [ ] **Step 4: Manual compatibility check**

Create a temporary OpenCode-style Markdown subagent in a temp HOME or project `.opencode/agents` test fixture, verify task schema includes the agent, and verify a task call can select it.

- [ ] **Step 5: Document any incomplete work**

If implementation leaves any Phase 1 requirement incomplete, add a root `TODO.md` entry that explains what remains and why. If nothing is incomplete, do not create or modify `TODO.md`.
