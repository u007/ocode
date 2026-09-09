# Local Model Instance Manager Design

## Problem

LM Studio can't serve Bonsai 8B 1-bit (a BitNet-style ternary-quant model). ocode
already runs one local embedding model as a supervised singleton process
(`internal/discovery/localserver.go`, `manifest.go`) — this design generalizes
that pattern to a small registry of independently manageable local chat/completion
models, starting with Bonsai 8B 1-bit, with per-model enable/disable and a
concurrency (parallel request slot) limit of 1 or 2.

## Non-goals

- No load-balancing across multiple *processes* of the same model. "Max serving
  N" means N concurrent request slots on one running process
  (`--parallel N` in llama.cpp / the MLX server's equivalent), not N separate
  OS processes.
- No automatic wiring of a registered local model into the agent's chat
  provider/model list. `/localmodel add` makes a model available to *start and
  query directly*; selecting it as an active chat model is a separate,
  future concern.
- No GUI. Slash command only.
- No support for arbitrary user-supplied model files in this iteration — only
  manifest-catalog entries (Bonsai 8B 1-bit to start). Adding "bring your own
  GGUF/MLX path" is a possible follow-up, not part of this design.

## Bonsai 8B 1-bit artifact

- **darwin/arm64**: MLX build — `prism-ml/Bonsai-8B-mlx-1bit`
- **other platforms**: llama.cpp GGUF build — `prism-ml/Bonsai-8B-gguf`

Both are registered in one `ServerManifest` entry (`local/bonsai-8b-1bit`),
platform-keyed exactly like the existing `local/bge-m3` /
`local/lfm2.5-embedding` rows. Backend selection (`mlx` vs `llamacpp`) is
automatic based on host, matching current behavior — no user-facing backend
flag.

## Architecture

Reuses the existing building blocks, generalized from "one embedder slot" to
"N named model slots":

- **Process spawn/supervision**: unchanged — still goes through
  `ProcessRegistry.StartBackground` (`internal/tool/process.go`,
  `process_supervisor.go`) for lifecycle tracking and teardown. No new spawn
  mechanism.
- **Artifact resolution**: unchanged — `EnsureArtifact` downloads/verifies
  against manifest URL+SHA256, same as today.
- **Health check / port adoption**: the existing `EnsureLocalServer` probe
  logic (check user override → check fixed port for a matching model →
  spawn new) is generalized to take a model id and a per-model port instead
  of being hardcoded to the embedder's fixed port 11457. Each enabled local
  model gets its own fixed port from a reserved range `11458-11465` (8 slots),
  assigned deterministically by sorting registered model ids alphabetically
  and taking the next free slot in that order — so multiple ocode processes
  on the same machine agree on the same port for the same model without
  needing a shared allocation file. `/localmodel add` beyond 8 registered
  models fails with an explicit "no free local-model port" error rather than
  silently reusing a port.

### New: instance registry (`internal/discovery/instances.go`)

Replaces the embedder-only `localMu`/`localBase`/`localModelID` package
globals with a registry keyed by model id:

```
type instanceSlot struct {
    modelID     string
    enabled     bool
    maxParallel int      // 1 or 2
    baseURL     string   // set once running
    process     *tool.ProcessHandle
}
```

- `EnsureLocalServer` becomes model-id-parameterized and looks up/creates the
  slot for that id instead of using single globals.
- Changing `maxParallel` while a slot is running requires a restart of that
  slot's process (stop, respawn with new `--parallel` value) — no live
  reconfiguration of an already-running llama-server/MLX process.
- The embedder's existing behavior (single model, port 11457) becomes just
  one entry in this registry, so no regression to current embedding flow.

### Config (`internal/config/ocodeconfig.go`)

New persisted field:

```go
LocalModels map[string]LocalModelConfig `json:"local_models,omitempty"`

type LocalModelConfig struct {
    Enabled     bool `json:"enabled"`
    MaxParallel int  `json:"max_parallel"` // 1 or 2, default 1
}
```

Keyed by model id (e.g. `"local/bonsai-8b-1bit"`). Loaded/saved through the
existing config load-modify-write savers (per
[[project_ocode_config_concurrent_writes]] — never
`SaveOcodeConfig(in-memory snapshot)`; use a targeted saver function so
concurrent sessions don't clobber each other's config writes).

### Slash command (`/localmodel`)

Registered in `internal/tui/commands.go` following the existing
`commandSpec` pattern (see `/discover`'s entry), handler logic in
`internal/tui/model.go`:

- `/localmodel list` — catalog entries (from manifest) plus any registered
  models, each showing: enabled/disabled, running state
  (stopped/starting/ready), configured max parallel.
- `/localmodel add <name>` — resolves `<name>` against the manifest catalog
  (initially only `bonsai-8b-1bit`), downloads the platform-appropriate
  artifact via `EnsureArtifact`, creates a `LocalModelConfig` entry
  (disabled by default, `max_parallel: 1`). Errors (unknown name, no
  artifact for this platform, download failure) are printed directly —
  no silent fallback.
- `/localmodel enable <name>` — sets `enabled: true`, starts the process
  (spawn + health check, same 60s/1s poll loop as the embedder).
- `/localmodel disable <name>` — sets `enabled: false`, stops the process
  via `ProcessRegistry` if running.
- `/localmodel limit <name> <1|2>` — sets `max_parallel`; if the slot is
  currently running, restarts it so the new value takes effect. Rejects
  values outside `{1, 2}` with an explicit error.
- `/localmodel status [name]` — health, PID, port, current max parallel for
  one model, or a summary table for all registered models when no name is
  given.

## Error handling

All failure paths (missing platform artifact, port conflict, download
failure, invalid limit value, unknown model name) surface as direct command
output, matching `/discover`'s existing error-surfacing convention. No
fallback substitution, no silent retry loops.

## Testing

- Unit tests for the generalized `instances.go` registry: enable/disable
  transitions, restart-on-limit-change, port assignment uniqueness across
  concurrent slots.
- Unit tests for manifest resolution of `local/bonsai-8b-1bit` on both
  darwin/arm64 (MLX) and a non-darwin/arm64 platform (llama.cpp GGUF).
- Existing embedder tests must continue to pass unchanged — the embedder's
  behavior is one instance of the generalized registry, not a special case.
- `/localmodel` command tests following the existing `/discover` command
  test pattern (`internal/tui/command_test.go`).
