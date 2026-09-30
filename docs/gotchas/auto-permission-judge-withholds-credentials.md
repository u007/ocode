---
type: Gotcha
title: Auto-Permission Judge — Credential Material Is Withheld from the Judge Context
description: |-
  buildPermissionContext never embeds credential-bearing file contents in the LLM permission judge's prompt: a sensitive target file gets a "(contents withheld: sensitive file)" marker, while executed custom scripts and referenced files are skipped entirely. Documents the full judge context inventory (project_context), the withholding invariant, and the side-task-client redaction chokepoint Agent.bindSideClient (nine call sites, the askPermissionModel registry-gated exception, the open permission_interpreter.go leak).</description>
  <parameter name="tags">["security", "permissions", "auto-permission", "secrets", "judge", "gotcha", "redaction", "side-task"]
resource: internal/agent/agent.go:4472
timestamp: 2026-09-29T04:31:04Z
---
# Auto-Permission Judge — Credential Material Is Withheld from the Judge Context

## Why this exists

The auto-permission judge is an **LLM**. Shipping a credential-bearing file
(`.env`, `auth.json`, SSH keys, …) into its prompt is the exact exposure the
permission layer exists to prevent. `buildPermissionContext`
(`internal/agent/agent.go:4472`) therefore assembles the judge's
`project_context` while refusing to embed sensitive file contents — the same
predicate family `scanToolResult` uses for tool results, but here the raw
content never reaches the judge at all.

## What the judge context contains (`project_context`)

`buildPermissionContext(toolName, args, maxCtxBytes, maxSources, maxLinesPerSource)`
returns a newline-joined list of sections. The same value is used by the generic
chat judge (`askPermissionModel`) and by the TypeSafe judge state at
`internal/agent/permission_typesafe.go:402` (`"project_context"`).

Metadata sections (only the byte budget applies, so file/script sources are not
starved):

1. `Working directory:` — `a.effectiveWorkDir()`, **not** the process cwd (see
   [auto-permission-judge-process-cwd.md](auto-permission-judge-process-cwd.md)).
2. `Pre-authorized paths (read/write/delete ALLOWED inside these roots; anything
   outside is OUT OF SCOPE):` — the allowed roots.
3. `Project type:` — `detectProjectType()`, when non-empty.
4. `Command analysis:` — `explainBashCommand()` for bash.

Source sections (counted against `maxSources`):

5. `Target file: <path> …` + `Directory <dir>:` (for path-scoped tools).
6. `Executed custom script: <path> …` and `Referenced file: <path> …` (bash).
7. `Fetch target:` (`Domain:` / `Path:`) for webfetch.

Defaults: `MaxContextBytes` 4096 / `MaxContextSources` 2 /
`MaxContextLinesPerSource` 80 (`internal/config/ocodeconfig.go:1352-1354`);
the TypeSafe state builder uses 2048/3/40 unless overridden
(`internal/agent/permission_typesafe.go:362`). When nothing is collected the
function returns `"(no context available)"`.

## The withholding rule

```go
sensitiveContextFile := func(p string) bool {
    return redact.IsSensitiveFile(p) || isSecretMaterialPath(p)
}
```

`internal/agent/agent.go:4512`. `redact.IsSensitiveFile`
(`internal/redact/sensitive.go:19`) and `isSecretMaterialPath`
(`internal/agent/permissions.go:2986`) cover `.env` / `.env.*` (except the
committed `.env.example|.sample|.template|.dist`), `*.pem|*.key|*.p12|*.pfx|
*.secrets`, `id_rsa|id_dsa|id_ecdsa|id_ed25519`, `.npmrc|.netrc|.pypirc|
.pgpass`, `*credentials*`, `secrets.*`, `auth.json`, `ocodeconfig.json`,
`opencode.json`.

Behaviour differs by section, deliberately:

| Section | Sensitive file behaviour | Code |
|---|---|---|
| Target file (path-scoped tools) | Path still shown; contents replaced by `(contents withheld: sensitive file)`. The parent-directory listing is still emitted. | `agent.go:4550-4555` |
| Executed custom script | Skipped entirely — no `Executed custom script:` section is added. | `agent.go:4600-4601` |
| Referenced file | Skipped entirely — no `Referenced file:` section. | `agent.go:4665-4666` |
| Non-sensitive file | Included as before. | positive control |

The target-file marker exists so the judge does not misread a withheld file as
"empty/new file"; the path is already visible in the tool arguments. Scripts and
referenced files are skipped because a command merely *naming* a secret
(`grep … .env`, `psql "$(… .env …)"`) must not drag that file's contents into
the prompt.

## Side-task LLM clients — `bindSideClient` is the redaction chokepoint (2026-09-29)

Redaction is **not** automatic for side-task calls. `NewClient` cannot know
about `a.redactionHook` — the hook is set on the agent *after* the main client
is built — so until this pass every freshly-built side-task client shipped
conversation content **UNREDACTED** to the side model whenever redaction was
enabled.

`Agent.bindSideClient` (`internal/agent/agent.go:281`) is the single chokepoint:

```go
func (a *Agent) bindSideClient(client LLMClient) LLMClient {
    return a.attachRedactionHook(a.bindOpenCodeSessionID(client))
}
```

`attachRedactionHook` (`agent.go:258`) writes the agent's tier-1 `redactionHook`
onto the `*GenericClient`'s `Redaction` field (`client.go:247`); a nil hook or a
non-`*GenericClient` implementation is returned unchanged, so mocks keep working.

Nine side-task sites route through it:

| Side task | Constructor | Site |
|---|---|---|
| Compaction | `smallModelOrMainClient` / `overrideModelClient` / `noThinkingClient` (all reached via `compactSummaryClient`, `agent.go:2748`) | `agent.go:2797`, `:2841`, `:2717` |
| Speech summary | `speechSummaryClient` (delegates to the same two compaction helpers) | `speech_summary.go:80` |
| Recap | `recapClient` | `agent.go:2380` |
| Auto-continue judge | `autoContinueJudgeClient` | `agent.go:2409` |
| Session title | `titleClients` | `title.go:143` |
| Advisor | `AdvisorTool.ExecuteCtx` | `advisor_tool.go:270` |
| Doc maintenance | `docMaintenanceClient` | `doc_maintenance.go:228` |
| Memory maintenance | `memoryMaintenanceClient` | `memory_maintenance.go:205` |
| Task contract | `TaskTool.verifierClient` | `task_contract.go:98` |

(The spec-model swap, `applySpecModel` at `agent.go:5397`, also binds on its
freshly-built client *before* installing it as the main client — its old
hand-rolled "re-wire the hook onto `a.client`" block was removed.)

**Rule:** any NEW freshly-built side-task client must go through
`Agent.bindSideClient`. Never call it on `a.client` itself — writing `Redaction`
onto the live main client would race with its own in-flight use (the hook is
already wired there by `SetRedactionHook`, `agent.go:2999`).

**Deliberate exception — the auto-permission judge.** `askPermissionModel`
(`agent.go:3655`) still uses `bindOpenCodeSessionID` alone (`agent.go:3684`) and
attaches the hook itself only when `a.judgeMaskRegistry() != nil`
(`agent.go:3691`): the mask hook only makes sense when there is a registry to
**unmask** with, so without one the judge must see raw text. The registry check
is load-bearing, not a shortcut.

**Open leak (TODO.md, 2026-09-29 — not fixed).**
`internal/agent/permission_interpreter.go:239` still builds its client with
`bindOpenCodeSessionID` only, yet it sends the command plus interpreter source
to the auto-permission model. Whether it becomes `bindSideClient` (privacy) or
stays intentionally unmasked (the judge needs raw text to judge exfiltration) is
an explicit decision that is still owed — with an `// intentional:` comment
either way, mirroring the registry-gated decision above.

Pinned by `TestSideClientsCarryRedactionHook`
(`internal/agent/agent_test.go:3485`): removing `attachRedactionHook` from
`bindSideClient` fails every case.

## Rules for maintainers

- **Any new file-content section in `buildPermissionContext` must route through
  `sensitiveContextFile`** before reading. Adding a section that calls
  `readFileSnippet` directly re-opens the leak.
- Do not conflate this with `scanToolResult` (`agent.go:3025`) — that masks a
  tool result on the way to the chat model via the redaction registry. The judge
  path withholds raw content outright; it never sees it, even masked.
- Keep the target-file (marker) vs script/reference (skip) distinction: the
  first is adjudicating a read whose path is known; the second is refusing to
  expand a secret the command mentions in passing.
- Do not extend the judge context with credential values from config/auth
  stores; those are exactly what `IsSensitiveFile` already excludes.
- **Any NEW freshly-built side-task LLM client must go through
  `Agent.bindSideClient`**, never `bindOpenCodeSessionID` alone (see the
  side-task section above). Never call it on `a.client`.

## Regression tests

`internal/agent/permission_context_secret_test.go`:

- `TestBuildPermissionContextWithholdsSensitiveTargetFile` — `.env` and
  `auth.json` targets carry the `withheld` marker, no secret bytes.
- `TestBuildPermissionContextWithholdsSensitiveReferenceInBash` — a
  `grep '^DATABASE_URL=' .env` command emits no `Referenced file: .env` section.
- `TestBuildPermissionContextWithholdsSensitiveExecutedScript` — `./secrets.sh`
  emits no `Executed custom script: ./secrets.sh` section.
- `TestBuildPermissionContextStillIncludesNonSensitiveReference` — positive
  control: `config.yaml` is still included in full.

## Related

- [auto-permission-judge-process-cwd.md](auto-permission-judge-process-cwd.md) — the judge must see the agent workDir, not the process cwd.
- [auto-permission-interpreter-scripts-in-compound-commands.md](auto-permission-interpreter-scripts-in-compound-commands.md) — the generic path (`askPermissionModel` + `buildPermissionContext`) and its truncation guard.
- [auto-permission-dependency-bin-policy.md](auto-permission-dependency-bin-policy.md) — the bundled gatekeeper prose that consumes this context.
