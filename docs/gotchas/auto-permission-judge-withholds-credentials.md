---
type: Gotcha
title: Auto-Permission Judge — Credential Material Is Withheld from the Judge Context
description: |-
  buildPermissionContext never embeds credential-bearing file contents in the LLM permission judge's prompt: a sensitive target file gets a "(contents withheld: sensitive file)" marker, while executed custom scripts and referenced files are skipped entirely. Documents the full judge context inventory (project_context) and the withholding invariant.</description>
  <parameter name="tags">["security", "permissions", "auto-permission", "secrets", "judge", "gotcha"]
resource: internal/agent/agent.go:4305
timestamp: 2026-09-21T02:56:07Z
---
# Auto-Permission Judge — Credential Material Is Withheld from the Judge Context

## Why this exists

The auto-permission judge is an **LLM**. Shipping a credential-bearing file
(`.env`, `auth.json`, SSH keys, …) into its prompt is the exact exposure the
permission layer exists to prevent. `buildPermissionContext`
(`internal/agent/agent.go:4305`) therefore assembles the judge's
`project_context` while refusing to embed sensitive file contents — the same
predicate family `scanToolResult` uses for tool results, but here the raw
content never reaches the judge at all.

## What the judge context contains (`project_context`)

`buildPermissionContext(toolName, args, maxCtxBytes, maxSources, maxLinesPerSource)`
returns a newline-joined list of sections. The same value is used by the generic
chat judge (`askPermissionModel`) and by the TypeSafe judge state at
`internal/agent/permission_typesafe.go:198` (`"project_context"`).

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
`MaxContextLinesPerSource` 80 (`internal/config/ocodeconfig.go:993-995`);
the TypeSafe state builder uses 2048/3/40 unless overridden
(`internal/agent/permission_typesafe.go:165`). When nothing is collected the
function returns `"(no context available)"`.

## The withholding rule

```go
sensitiveContextFile := func(p string) bool {
    return redact.IsSensitiveFile(p) || isSecretMaterialPath(p)
}
```

`internal/agent/agent.go:4345`. `redact.IsSensitiveFile`
(`internal/redact/sensitive.go:19`) and `isSecretMaterialPath`
(`internal/agent/permissions.go:2862`) cover `.env` / `.env.*` (except the
committed `.env.example|.sample|.template|.dist`), `*.pem|*.key|*.p12|*.pfx|
*.secrets`, `id_rsa|id_dsa|id_ecdsa|id_ed25519`, `.npmrc|.netrc|.pypirc|
.pgpass`, `*credentials*`, `secrets.*`, `auth.json`, `ocodeconfig.json`,
`opencode.json`.

Behaviour differs by section, deliberately:

| Section | Sensitive file behaviour | Code |
|---|---|---|
| Target file (path-scoped tools) | Path still shown; contents replaced by `(contents withheld: sensitive file)`. The parent-directory listing is still emitted. | `agent.go:4383-4388` |
| Executed custom script | Skipped entirely — no `Executed custom script:` section is added. | `agent.go:4433-4435` |
| Referenced file | Skipped entirely — no `Referenced file:` section. | `agent.go:4498-4500` |
| Non-sensitive file | Included as before. | positive control |

The target-file marker exists so the judge does not misread a withheld file as
"empty/new file"; the path is already visible in the tool arguments. Scripts and
referenced files are skipped because a command merely *naming* a secret
(`grep … .env`, `psql "$(… .env …)"`) must not drag that file's contents into
the prompt.

## Rules for maintainers

- **Any new file-content section in `buildPermissionContext` must route through
  `sensitiveContextFile`** before reading. Adding a section that calls
  `readFileSnippet` directly re-opens the leak.
- Do not conflate this with `scanToolResult` (`agent.go:2933`) — that masks a
  tool result on the way to the chat model via the redaction registry. The judge
  path withholds raw content outright; it never sees it, even masked.
- Keep the target-file (marker) vs script/reference (skip) distinction: the
  first is adjudicating a read whose path is known; the second is refusing to
  expand a secret the command mentions in passing.
- Do not extend the judge context with credential values from config/auth
  stores; those are exactly what `IsSensitiveFile` already excludes.

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
