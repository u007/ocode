# LLM Auto-Permission (`/permission-model` + `/permission-auto`)

**Date:** 2026-06-01
**Status:** Approved, pending implementation plan

## Problem

The static permission engine (`internal/agent/permissions.go`) already covers most
real cases: workdir path scoping, sensitive-path gating, bash command parsing
(compound commands, redirections, env vars, subshells), auto-allow prefixes with
read/mutating modes, and subcommand allowlists. Anything it doesn't recognize falls
to `Ask` (human interrupt).

Two gaps remain:

1. **Static config gaps.** No way to declare additional allowed path prefixes
   (e.g. `/tmp`) or extra project roots beyond the working directory. These force
   needless `Ask` interrupts today.
2. **Novel/compound commands.** Anything outside the allowlists always interrupts
   the user, even when the intent is obviously benign for the current project.

The goal is to (a) close the static gaps deterministically, and (b) add an optional
LLM layer that can auto-grant on the user's behalf, reducing interruptions.

## Non-Goals

- Replacing the static engine. The deterministic engine stays in the trust boundary.
- Letting the model override hard-blocks or the YOLO/locked modes.
- Making the LLM the default. Auto-permission is **off** by default.

## Design Principles (non-negotiable)

- **Hard-blocks are deterministic and final.** `isHardBlockedCommand` runs in
  `Decide()` and returns `Deny` before any model is consulted. The permission model
  never sees a hard-blocked command and can never approve one.
- **Fail closed.** Model timeout, error, or malformed output ⇒ fall through to the
  human `Ask`. The security check never fails open.
- **The model can only `allow` or `ask`.** It cannot emit a `deny`-override, cannot
  escalate the permission mode, and cannot widen past the static guardrails.
- **Every grant is a real, auditable rule.** When the model allows, the decision is
  persisted as a normal `PermissionManager` rule — greppable, diffable, revocable —
  not an ephemeral model judgment.

---

## Phase 1 — Static config (ships standalone, zero LLM)

Extend the config and `PermissionManager` with two declarative fields.

### Config (`config.PermissionConfig`)

- `allowed_path_prefixes []string` — default `["/tmp"]`. Absolute-path prefixes that
  are treated as in-scope (like the working directory) for path-scoped tools and
  bash path arguments.
- `extra_project_roots []string` — additional directories treated as
  workdir-equivalent for the session.

### Engine changes (`internal/agent/permissions.go`)

- `PermissionManager` gains `allowedPathPrefixes []string` and `extraProjectRoots
  []string`, populated in `LoadFromOcode`.
- `isWithinWorkDir` additionally returns true when the resolved path is the workdir,
  under the workdir, under any `extraProjectRoots` entry, or under any
  `allowedPathPrefixes` entry. The existing symlink-resolution and
  not-yet-existing-file handling is preserved.
- No change to sensitive-path gating: a `.env` under `/tmp` is still sensitive.
- `ExportConfig` round-trips the two new fields.

### Tests

- `isWithinWorkDir` with `/tmp/foo`, an extra root, and an out-of-scope path.
- A `read`/`write`/`delete` under `/tmp` auto-allows; under a sensitive name still asks.
- A bash command writing to `/tmp/x` auto-allows; writing outside still asks.
- Config round-trip (`LoadFromOcode` → `ExportConfig`).

This phase lands and ships independently of Phase 2.

---

## Phase 2 — LLM auto-permission

### New state

On `PermissionManager` (or the agent, wherever the mode currently lives):

- `autoPermission bool` — default **off**.
- `permissionModel string` — provider/model id. Empty ⇒ resolved via
  `ResolveSmallModel(cfg)` at call time (`internal/agent/small_model.go`).

### Config file: `~/.config/opencode/permission_auto.conf`

Declarative fields plus an optional free-text prompt:

```toml
allow_destructive   = false        # default false; flippable per project
extra_project_roots = ["..."]      # merges with Phase 1 roots
prompt              = "..."        # optional; appended to the system prompt only
```

- Static fields are enforced in Go.
- `prompt` only nudges the model. It can never widen past the static guardrails or
  the hard-block list.
- `allow_destructive = false` (default): the model may auto-allow non-destructive
  ops (reads, writes/exec inside allowed paths) but destructive ops (e.g. `rm`,
  `delete`, overwrite outside scope) fall to the human `Ask` even with auto on.
- `allow_destructive = true`: the model may also auto-allow destructive ops, still
  bounded by the hard-block list. Higher prompt-injection risk; opt-in per project.

### Consultation flow (`agent.go` `HandleToolCall`, the `Ask` branch)

Current code at `agent.go:766`: when `decision.Level == Ask`, it either invokes
`OnPermissionAsk` (sub-agents) or returns the `PERMISSION_ASK:` sentinel (main agent).
Insert the consultation **before** that human fallback:

1. `isHardBlockedCommand` already converted hard-blocks to `Deny` in `Decide`.
2. If `autoPermission` is off → current behavior unchanged (human `Ask`).
3. If on → build a minimized, structured consultation request:
   - tool name, the command/args, the resolved target path(s),
   - the relevant context the command references (tool-result path or excerpt,
     allowed roots) so the model can reason about what the command is doing,
   - the classification of the op as destructive / non-destructive (computed in Go,
     not trusted from the model).
   Call the permission model with a JSON-only response schema:
   `{ decision: "allow" | "ask", reason: string, suggested_rule?: string }`.
   Use a short timeout (a few seconds).
4. Apply the result:
   - `allow` **and** (op is non-destructive **or** `allow_destructive == true`) →
     persist as a real `PermissionManager` rule (so it is auditable and not
     re-consulted), then execute.
   - `allow` but op is destructive and `allow_destructive == false` → human `Ask`.
   - `ask`, timeout, error, or malformed output → human `Ask` (fail closed).

### Guardrails (enforced in Go regardless of config)

- Hard-blocks stay deterministic `Deny`; the model never sees them.
- The model response is constrained to `allow` / `ask`; any other value ⇒ treated as
  `ask`.
- Destructive classification is computed in Go from the parsed command, not taken
  from the model's word.
- YOLO and locked modes bypass the model entirely (YOLO already allows; locked
  already denies non-read-only).

### Commands (reuse existing picker UI)

- `/permission-model` → reuse the async provider+model picker used by `/model`
  (`internal/tui/picker.go`), but store the selection separately as
  `permissionModel`. Picker default highlights a small/fast model.
- `/permission-auto on|off` → toggle `autoPermission`; default off. Bare
  `/permission-auto` prints current status.

### Init status display

On the session-start banner, add a status line:

- Off:
  ```
  Auto-permission: off
  ```
- On with explicit model:
  ```
  Auto-permission: on · model: <provider/model>
  ```
- On with no model set → resolve the small model and tag it:
  ```
  Auto-permission: on · model: <resolved-small-model> (small, auto)
  ```

### TUI safety

All status output goes through the normal render path / debug sink — never raw
`fmt.Print*` to stdout/stderr (alt-screen corruption rule in CLAUDE.md). The
consultation LLM call must not write to the terminal; capture/route via the agent
debug sink.

### Tests

- Auto off → behaves exactly as today (human Ask).
- Auto on, model returns `allow`, non-destructive → executes and a persisted rule
  exists afterward (no second consultation).
- Auto on, model returns `allow`, destructive, `allow_destructive=false` → human Ask.
- Auto on, model returns `allow`, destructive, `allow_destructive=true` → executes.
- Model timeout / error / malformed JSON → human Ask (fail closed).
- Hard-blocked command with auto on → `Deny`, model never consulted.
- Empty `permissionModel` resolves to small model.
- Config file parse (declarative fields + optional prompt).

## Open risks

- **Prompt injection via tool results** is the core risk of `allow_destructive=true`.
  Mitigation: default false, hard-block floor, destructive classification in Go,
  fail-closed. Documented so the user opts in knowingly.
- **Latency** on consulted calls: bounded by the short timeout and use of the small
  model by default.

## Rollout

1. Phase 1 (static config) — merge and ship.
2. Phase 2 (LLM layer + commands + init status) — merge behind the default-off flag.
