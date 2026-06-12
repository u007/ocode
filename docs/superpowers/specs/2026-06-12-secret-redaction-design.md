# Secret Redaction Model — Design

Date: 2026-06-12
Status: Approved (brainstorm phase)

## Goal

Detect passwords/tokens/secrets in text before it reaches any LLM (main model,
advisor, quick/title/compact models), replace them with session-scoped indexed
placeholders, store messages with placeholders only, and re-render real values
in the TUI chat display. Feature is **off by default**, toggleable from the
sidebar and configurable via a `/secrets` slash command (mirroring
`/permissions`).

## Decisions (user-confirmed)

| Decision | Choice |
|---|---|
| Detection | Hybrid: regex/heuristics always + configured local security model (LM Studio listed first) for contextual scan |
| Security model unreachable | Block + permission-style prompt (Send regex-only / Cancel / Retry) |
| Storage | Placeholders on disk; originals in a per-session vault file under home dir |
| Tool calls containing placeholders | Unmask real value at execution time |
| Placeholder format | `[[OCSEC:<6-hex session nonce>:<index>]]` — ASCII, per-session nonce, no escaping needed |
| Default state | **Disabled**; sidebar toggle + `/secrets`; persisted to `ocodeconfig.json` |

## Architecture

### 1. Ingestion-time redaction (core)

Redact when text **enters session history** — user prompt, pasted content,
tool result, attached file content — not per-send. In-memory history and
session `.json` files therefore only ever contain placeholders.

- All model paths (main, advisor, quick/title/compact) read the same
  placeholder-bearing history → covered automatically with no per-path work.
- The security-model scan runs once per new message, not on every send.
- `GenericClient.ChatWithContext()` (internal/agent/client.go:354, the single
  outbound chokepoint) additionally runs a cheap **regex-only safety net**
  on the final payload — catches pre-feature sessions and plugin-injected
  text. Safety-net hits are redacted and registered like any other secret.

### 2. Detection pipeline

Tier 1 — regex/heuristics (always on when feature enabled, zero latency):

- Known key formats: AWS (`AKIA…`), GitHub (`ghp_`/`gho_`/`github_pat_`),
  Slack, Stripe (`sk_live_`), JWT (`eyJ…`), OpenAI/Anthropic (`sk-…`),
  private-key PEM blocks.
- High-entropy strings adjacent to keywords: `password`, `passwd`, `secret`,
  `token`, `api_key`, `Authorization:`, `Bearer`.
- Credentials inside connection strings / URLs (`scheme://user:pass@host`).
- User-defined custom words (from config + `/secrets` manual registration).

Tier 2 — security model (configured local LLM):

- Scans only the new text for contextual secrets regex misses
  ("my db password is hunter2").
- Model returns exact spans; ocode verifies each span exists **verbatim** in
  the source text before substituting — hallucinated spans are dropped and
  logged to the debug sink.

Fail mode: if a security model is configured but unreachable, show a
permission-style modal before the send: regex-redacted preview +
**Send (regex-only)** / **Cancel** / **Retry model**. If no security model is
configured, regex-only runs silently after a one-time notice.

### 3. Placeholder registry (session-scoped, accumulative)

`SecretRegistry` per session:

- `value → index` map and `index → {value, kind, source, firstSeenAt}`.
- Counter is monotonically increasing; the same secret value anywhere in the
  session reuses its existing index. Indexes are never reassigned.
- Exact-value matching only.

### 4. Placeholder token format

```
[[OCSEC:a3f9c2:1]]
```

- `OCSEC` namespace + 6-hex nonce (crypto/rand, generated once per session,
  stored in the vault header) + decimal index.
- The nonce makes collision with real text effectively impossible → **no
  escaping logic anywhere**.
- Pure ASCII. Deliberately avoids `<|…|>` (model special tokens), `{{…}}`
  (template engines), `${…}`/`$(…)` (shell expansion during unmask).
- Match regex: `\[\[OCSEC:[0-9a-f]{6}:\d+\]\]`. Tokens carrying a different
  session's nonce are left untouched (stale-clone safety).

### 5. Vault

- Path: `~/.local/share/opencode/project/<slug>/secrets/<ses_id>.vault.json`
  (Windows: under the existing `AppData/Local/opencode` base).
- **Always under home**, even when sessions are project-local
  (`.ocode/sessions/`) — keeps secrets out of the repo.
- Dir `0700`, file `0600`.
- Contents: `{ "nonce": "a3f9c2", "secrets": { "1": {"value": "...",
  "kind": "github_pat", "source": "regex|model|manual", "firstSeenAt": ...} } }`
- Deleting a session deletes its vault. Cloning/forking a session copies the
  vault under the new session id (nonce kept, so existing tokens keep
  resolving).

### 6. Display re-substitution

- `displayTextForAgentMessage()` (internal/tui/model.go) swaps tokens to real
  values at render time, styled distinctly (dim underline) to signal "vaulted".
- `/secrets` toggle: show placeholders instead of values (screen-share mode).
- Copy-to-clipboard copies the real value.

### 7. Tool execution unmask

- The tool-dispatch layer substitutes real values into tool inputs (Bash
  command string, file-write content, etc.) immediately before execution.
- Tool output echoing the secret is re-redacted at ingestion (§1), so
  plaintext never lands in history.
- Permission prompts and transcript continue to display the rendered
  (placeholder-styled) form.

### 8. Config + persistence

`ocodeconfig.json` (global + project layering, same as existing keys):

```json
"security": {
  "redaction": {
    "enabled": false,
    "model": "lmstudio/<model-id>",
    "failMode": "block",
    "customWords": []
  }
}
```

- `enabled` defaults to **false**.
- Persistence MUST use a targeted load-modify-write saver (pattern:
  `SaveTUITheme`) — e.g. `SaveSecurityRedaction(...)` — never a full
  `SaveOcodeConfig` snapshot (concurrent-session clobber risk).

### 9. UI surfaces

**Sidebar**: a "Secrets" row showing `off` / `on (<model short name>)`;
mouse-clickable (existing sidebar click-target pattern) to toggle
enable/disable. Toggling writes through the targeted saver immediately.

**`/secrets` slash command** (modal, mirrors `/permissions`):

- Enable/disable toggle.
- Security-model picker: LM Studio models listed first, then Ollama, then
  remaining providers; cloud models annotated with a "secrets leave your
  machine" warning.
- Session secret list: index, kind, masked preview (`hun•••r2`), source.
  Sorted by index ascending; paginated if long.
- Manual "add secret word" entry.
- Display-mode toggle (real values vs placeholders).

### 10. Edge cases

- Partial/split secrets across messages: only exact-value matches replace;
  partial leakage is the regex tier's responsibility.
- Compaction/summarization operates on placeholder text; tokens survive
  because the vault is session-keyed.
- Feature toggled on mid-session: only new ingestion is redacted; the
  chokepoint safety net partially covers older text on subsequent sends.
- Feature toggled off mid-session: existing tokens still render via vault
  (display + tool unmask stay active); only detection stops.

## Testing

Unit:
- Registry: accumulation, same-value reuse, never-reassign.
- Regex corpus: known key formats true-positives; benign high-entropy
  false-positive guards (git SHAs, base64 image chunks).
- Token round-trip: redact → render; redact → tool unmask.
- Vault: perms (0700/0600), nonce stability, clone-copy behavior.
- Targeted config saver preserves unrelated keys.

Integration:
- Ingest secret → session file on disk contains no plaintext secret.
- Quick/advisor/title payload inspection: placeholders only.
- Bash tool unmask round-trip; tool output re-redacted.
- Fail-mode modal flow (model unreachable).
- Sidebar toggle persists `enabled` across restart.

## Out of scope (v1)

- OS keychain vault backend.
- Cross-session shared secret registry.
- Redaction of file contents read by tools but never sent to the LLM.
