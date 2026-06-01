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

1. **Static config gaps.** We need a deterministic way to treat additional roots
   like `/tmp` as in-scope for both the permission engine and the actual file-tool
   confinement layer.
2. **Novel/compound commands.** Anything outside the allowlists always interrupts
   the user, even when the intent is obviously benign for the current project.

The goal is to (a) close the static gaps deterministically, and (b) add an optional
LLM layer that can auto-grant on the user's behalf, reducing interruptions.

## Non-Goals

- Replacing the static engine. The deterministic engine stays in the trust boundary.
- Letting the model override hard-blocks or the YOLO/locked modes.
- Making the LLM the default. Auto-permission is **off** by default.
- Letting the model invent or widen persisted rule scope.

## Design Principles (non-negotiable)

- **Hard-blocks are deterministic and final.** `isHardBlockedCommand` runs in
  `Decide()` and returns `Deny` before any model is consulted. The permission model
  never sees a hard-blocked command and can never approve one.
- **Fail closed.** Model timeout, error, malformed output, or no resolvable
  permission model ⇒ fall through to the human `Ask`.
- **The model can only `allow` or `ask`.** It cannot emit a `deny`-override,
  cannot escalate the permission mode, and cannot widen past the static guardrails.
- **Persisted scope is computed in Go, never by the model.** The model returns only
  a decision and reason. Any reusable grant that gets saved is derived from the
  parsed request by Go.
- **Every auto-grant is durable and auditable.** An auto-grant is only accepted if
  Go can persist it as a typed, greppable rule/config entry. Otherwise the flow
  falls back to human `Ask` instead of allowing once in RAM only.
- **Permission scope and tool confinement must share one root model.** If a path is
  considered in-scope by permissions, the file tools must also accept it.

---

## Phase 1 — Static config (ships standalone, zero LLM)

Unify allowed-root handling around the existing config architecture instead of
adding a parallel permission-only path system.

### Config shape

Keep using `ocodeconfig.json` / JSONC and the existing global+project merge path in
`internal/config/ocodeconfig.go`.

Use the existing top-level field as the single source of truth for additional roots:

```jsonc
{
  "extra_allowed_paths": ["/tmp"]
}
```

Semantics:

- `extra_allowed_paths []string` — absolute roots treated as additional allowed
  filesystem roots by both permission checks and file-tool confinement.
- Default value includes `"/tmp"`.
- Paths may come from global config, project config, or interactive persistence.
- Relative paths are not valid entries; roots are stored normalized/absolute.

This intentionally replaces the earlier idea of separate
`permissions.allowed_path_prefixes` and `permissions.extra_project_roots` fields.
A second root list would drift from the already-existing tool confinement model in
`internal/tool/file.go`.

### Engine + tool changes

#### `internal/tool/file.go`

- Continue to use `extra_allowed_paths` as the file-tool allowlist source.
- Ensure default config seeds `/tmp` so `confinedPath()` treats `/tmp/...` as
  allowed without requiring an interactive approval first.
- Preserve current normalization and symlink handling.

#### `internal/agent/permissions.go`

- Add `extraAllowedPaths []string` to `PermissionManager`.
- Populate it from `cfg.Ocode.ExtraAllowedPaths` when the manager is created.
- Replace the current workdir-only check in `isWithinWorkDir` with
  `isWithinAllowedRoots`, where allowed roots are:
  - the resolved workdir, and
  - every resolved `extra_allowed_paths` root.
- Preserve existing symlink-resolution and not-yet-existing-file behavior.
- No change to sensitive-path gating: a `.env` under `/tmp` is still sensitive.

#### Persistence

- Keep `extra_allowed_paths` persisted through `SaveOcodeConfig`, not through
  `PermissionManager.ExportConfig()`.
- Do **not** duplicate this data inside `PermissionConfig`.

### Tests

- `isWithinAllowedRoots` with workdir, `/tmp`, and an out-of-scope path.
- A `read`/`write`/`delete` under `/tmp` auto-allows; under a sensitive name still asks.
- A bash command writing to `/tmp/x` auto-allows; writing outside allowed roots still asks.
- `confinedPath()` accepts `/tmp/...` and rejects paths outside the union of workdir,
  `/tmp`, and existing cache roots.
- Config load/save round-trip for `extra_allowed_paths`.

This phase lands and ships independently of Phase 2.

---

## Phase 2 — LLM auto-permission

### Config shape

Stay inside `ocodeconfig.json`; do **not** introduce a separate TOML file.

Extend `config.PermissionConfig` with an `auto` block:

```jsonc
{
  "permissions": {
    "mode": "normal",
    "auto": {
      "enabled": false,
      "model": "",
      "allow_destructive": false,
      "prompt": "",
      "max_context_bytes": 4096,
      "max_context_sources": 2,
      "max_context_lines_per_source": 80
    }
  },
  "extra_allowed_paths": ["/tmp"]
}
```

Semantics:

- `enabled` — default `false`.
- `model` — provider/model id. Empty ⇒ resolve via `ResolveSmallModel(cfg)` at call
  time (`internal/agent/small_model.go`).
- `allow_destructive` — default `false`. If `true`, destructive ops may be auto-
  allowed, still bounded by hard-blocks and Go-side classification.
- `prompt` — optional free text appended to the system prompt only.
- `max_context_*` — hard caps enforced in Go for any untrusted consultation context.

### New runtime state

On the agent / permission manager side:

- `autoPermissionEnabled bool`
- `permissionModel string`
- `allowDestructive bool`
- bounded context limits loaded from config

### Consultation flow (`agent.go` `HandleToolCall`, the `Ask` branch)

Current code at `agent.go:766`: when `decision.Level == Ask`, it either invokes
`OnPermissionAsk` (sub-agents) or returns the `PERMISSION_ASK:` sentinel (main agent).
Insert consultation **before** that human fallback:

1. `isHardBlockedCommand` already converted hard-blocks to `Deny` in `Decide`.
2. If auto-permission is off → current behavior unchanged.
3. If mode is YOLO or locked → bypass the model entirely.
4. Resolve the permission model:
   - explicit `permissions.auto.model`, else
   - `ResolveSmallModel(cfg)`.
   - If resolution returns empty ⇒ skip consultation and use human `Ask`.
5. Build a minimized, structured consultation request in Go.
6. Call the model with a JSON-only schema:
   `{ decision: "allow" | "ask", reason: string }`
   using a short timeout (a few seconds).
7. Validate and apply the result:
   - `allow` is accepted only when Go can derive a durable, typed persisted grant.
   - if the op is destructive and `allow_destructive == false` ⇒ human `Ask`.
   - `ask`, timeout, error, malformed output, or unpersistable scope ⇒ human `Ask`.
8. Persist the derived grant, then execute exactly once.

### Consultation payload: best safe version

The model needs some context, but all context is untrusted and must be bounded.
The consultation payload should be structured like this:

```json
{
  "tool_name": "bash",
  "scope": "tool|bash_prefix",
  "rule": "bash.prefix.sed",
  "command": "sed -n '1,20p' /tmp/x",
  "args": {"command": "..."},
  "resolved_targets": ["/tmp/x"],
  "allowed_roots": ["<workdir>", "/tmp"],
  "is_destructive": false,
  "destructive_reason": "read_only",
  "context": {
    "sources": [
      {
        "kind": "tool_result|workspace_excerpt",
        "path": "...",
        "sha256": "...",
        "start_line": 1,
        "end_line": 40,
        "truncated": true,
        "excerpt": "..."
      }
    ],
    "omitted_sources": ["sensitive_path", "too_large", "non_text"]
  }
}
```

Rules for building `context` in Go:

- Treat every excerpt as **untrusted data**, never as instructions.
- Prefer **metadata first**: tool name, resolved paths, allowed roots, and
  destructive classification carry more weight than free-text context.
- Include at most `max_context_sources` sources and `max_context_bytes` total text.
- Strip ANSI/control bytes and reject binary data.
- Only include content from:
  - tool-result cache files, or
  - workspace files explicitly referenced by the pending command/request.
- Never include content from sensitive paths (`.env`, keys, `.git`, workflows, etc.).
- Never recursively ask the permission model to inspect arbitrary unrelated files.
- When a referenced file is too large, include a bounded excerpt plus metadata
  (`sha256`, path, line range, truncated=true).
- If no safe excerpt can be produced, send no excerpt; the model still sees the
  command, paths, and classification.

### Durable grant model

The model does **not** propose rule text. Go derives one persisted grant from the
normalized request. If no safe typed grant can be derived, the flow falls back to
human `Ask`.

Persisted auto-grants should be explicit typed entries under `permissions.auto.grants`:

```jsonc
{
  "permissions": {
    "auto": {
      "grants": [
        {
          "kind": "tool_args_exact",
          "tool": "read",
          "normalized_args": {"path": "/tmp/out.txt"}
        },
        {
          "kind": "bash_exact",
          "normalized_command": "sed -n 1,20p /tmp/out.txt",
          "destructive": false
        },
        {
          "kind": "webfetch_domain",
          "domain": "pkg.go.dev"
        }
      ]
    }
  }
}
```

Grant kinds:

- `tool_args_exact` — exact normalized tool arguments for a single tool call.
- `bash_exact` — exact normalized bash command after parsing, path resolution, and
  whitespace normalization.
- `webfetch_domain` — exact domain grant.

Notes:

- `extra_allowed_paths` remains the persisted root allowlist and is **not** duplicated
  into `auto.grants`.
- Exact grants are intentionally narrow and auditable.
- Existing coarse rules (`tool allow`, `bash prefix allow`) remain available for
  explicit human approval, but auto-permission does not invent broad grants.

### Matching exact grants

Before asking the model, Go checks for an existing exact auto-grant:

- For non-bash tools, normalize relevant args into a stable JSON object with
  resolved absolute paths.
- For bash, parse the command, resolve paths, canonicalize whitespace, preserve
  argument order, and serialize to a stable normalized string.
- If an exact grant matches, allow immediately with no consultation.

### Persistence write path

Current persistence is split:

- `PermissionManager.ExportConfig()` / `SaveOcodePermissions()` persist permission rules.
- `extra_allowed_paths` is saved separately via `SaveOcodeConfig()`.

Add one higher-level save path for all permission-related state, e.g.
`SavePermissionState(...)`, which persists:

- `permissions.mode`
- `permissions.tools`
- `permissions.bash`
- `permissions.auto`
- `extra_allowed_paths`

`HandleToolCall` must not write config directly. Instead it emits a typed
permission-update event/callback consumed by the main TUI/session layer, which then
updates in-memory state and persists it through the unified save path. This keeps
main-agent, sub-agent, and future headless flows consistent.

### Guardrails (enforced in Go regardless of config)

- Hard-blocks stay deterministic `Deny`; the model never sees them.
- The model response is constrained to `allow` / `ask`; any other value ⇒ `ask`.
- Destructive classification is computed in Go from the parsed command/tool request.
- YOLO and locked modes bypass the model entirely.
- No model-proposed rule strings.
- No auto-grant unless Go can derive and persist a narrow typed grant.

### Commands (reuse existing picker UI)

- `/permission-model` → reuse the async provider+model picker used by `/model`
  (`internal/tui/picker.go`), but store the selection separately as the
  auto-permission model.
- `/permission-auto on|off` → toggle auto-permission; default off. Bare
  `/permission-auto` prints current status.
- `/permission-auto destructive on|off` → toggle `allow_destructive` without
  editing config manually.

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
- On with no explicit model, resolved successfully:
  ```
  Auto-permission: on · model: <resolved-small-model> (small, auto)
  ```
- On with no resolvable model:
  ```
  Auto-permission: on · model: unavailable · fallback: ask
  ```

### TUI safety

All status output goes through the normal render path / debug sink — never raw
`fmt.Print*` to stdout/stderr (alt-screen corruption rule in `CLAUDE.md`). The
consultation LLM call must not write to the terminal; capture/route via the agent
debug sink.

### Tests

- Auto off → behaves exactly as today (human Ask).
- Empty `permissions.auto.model` resolves to small model.
- No resolvable small model ⇒ human Ask, with no fail-open path.
- Auto on, model returns `allow`, non-destructive, exact grant derivable → executes
  and a persisted exact grant exists afterward.
- Second identical request hits the exact grant and does not re-consult.
- Auto on, model returns `allow`, destructive, `allow_destructive=false` → human Ask.
- Auto on, model returns `allow`, destructive, `allow_destructive=true`, exact grant
  derivable → executes and persists.
- Model timeout / error / malformed JSON → human Ask.
- Hard-blocked command with auto on → `Deny`, model never consulted.
- Context builder excludes sensitive paths, binary files, and oversized sources.
- `SavePermissionState` round-trips `permissions.auto` plus `extra_allowed_paths`.

## Open risks

- **Prompt injection via tool results or referenced files** remains the core risk,
  especially with `allow_destructive=true`.
  Mitigations:
  - default `allow_destructive=false`
  - hard-block floor
  - destructive classification in Go
  - strict context caps and sensitive-path exclusion
  - no model-authored rule scope
  - exact-match grants instead of broad grants
- **Latency** on consulted calls: bounded by the short timeout and use of the small
  model by default.
- **Grant accumulation** from exact-match entries may grow over time; exact grants may
  need pruning/compaction later, but narrow scope is the right default.

## Rollout

1. Phase 1 (shared allowed-root model with `/tmp` default) — merge and ship.
2. Phase 2 (LLM layer + exact auto-grants + commands + init status) — merge behind
   the default-off flag.
