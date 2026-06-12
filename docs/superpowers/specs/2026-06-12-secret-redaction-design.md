# Secret Redaction Model — Design

Date: 2026-06-12
Status: Approved (brainstorm phase, revised after adversarial + feasibility review)

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
| Detection | Hybrid: regex/heuristics always + configured **local-only** security model (LM Studio listed first) for contextual scan |
| Security model unreachable | Block + permission-style prompt (Send regex-only / Cancel / Retry) |
| Storage | Placeholders on disk; originals in a per-session vault file under home dir |
| Tool calls containing placeholders | Unmask at execution, gated by secret-aware permission prompt + egress warning |
| File-content detection | Known-format detectors only (no entropy heuristics, no tier-2) |
| Placeholder format | `[[OCSEC:<6-hex session nonce>:<index>]]` — ASCII, per-session nonce, no escaping needed |
| Default state | **Disabled**; chokepoint net runs always-on in tripwire mode; sidebar toggle + `/secrets`; persisted to `ocodeconfig.json` |

## Architecture

### 1. Ingestion-time redaction (core)

Redact when text **enters session history** — not per-send. In-memory history
and session `.json` files therefore only ever contain placeholders, and all
model paths (main, advisor, quick/title/compact) read placeholder-bearing
history.

There are **four independent ingestion carriers**; a shared `Redactor` is
injected into each at construction:

1. TUI: user message assembly before `m.messages = append(...)`
   (internal/tui/model.go:1957).
2. ACP bridge: append site at internal/acp/bridge.go:105.
3. Web/SSE standalone: `agentSession` append at
   internal/server/handler_sse.go:225.
4. Task-tool child sessions: persist callback in
   internal/agent/child_session.go.

Tool results and attached file contents are redacted by the same `Redactor`
where they are converted into history messages.

**Call-site fix:** `GenerateTitleAsync` (internal/agent/title.go:30) receives
raw strings, not history — callers must pass placeholder-substituted text.

**Chokepoint safety net:** `GenericClient.ChatWithContext()`
(internal/agent/client.go:354) runs a regex-only scan over the **final
assembled payload including system prompt, context files (CLAUDE.md etc.),
and LSP-diagnostic injections** — the layer that covers transient fragments
ingestion never sees. Known limitation (documented): contextual (tier-2)
secrets inside CLAUDE.md/context files are out of regex reach. The net lives
inside `GenericClient` itself, so the advisor's separate client instance is
covered; non-`GenericClient` `LLMClient` implementations (tests) are not.

**Tripwire mode (feature disabled):** the chokepoint net still scans but does
NOT redact. On first high-confidence hit it shows a one-time prompt offering
to enable redaction. This keeps "default off" honest while ensuring users
discover the feature exactly when it matters.

### 2. Detection pipeline

Tier 1 — regex/heuristics (zero latency):

- Known key formats: AWS (`AKIA…`), GitHub (`ghp_`/`gho_`/`github_pat_`),
  Slack, Stripe (`sk_live_`), JWT (`eyJ…`), OpenAI/Anthropic (`sk-…`),
  private-key PEM blocks.
- High-entropy strings adjacent to keywords (`password`, `token`, `api_key`,
  `Authorization:`, `Bearer`) — **chat/paste text only**.
- Credentials in connection strings / URLs (`scheme://user:pass@host`).
- User-defined custom words.

Tier 2 — security model (**local-only, hard requirement**):

- Scans new chat/paste text for contextual secrets ("my db password is
  hunter2"). Returns exact spans; ocode verifies each span exists verbatim
  before substituting; hallucinated spans dropped (debug sink logs offsets/
  kinds only — never raw text).
- The raw-text scan request may go **only to verified local/loopback
  endpoints** (LM Studio, Ollama, localhost). No cloud model may be selected
  as security model, and there is never a fallback to a cloud model. The
  `/secrets` picker simply does not offer non-local models.

**File-content rule:** content ingested from file reads / tool file output
uses **known-format detectors only** — no entropy/keyword heuristics, no
tier-2 scan. Rationale: round-trip safety; entropy heuristics hit git SHAs,
lockfile hashes, base64 fixtures, and a corrupted token written back into
source is worse than the leak class it prevents. On file write-back, if a
partial/corrupted OCSEC token is detected, **fail loudly** (block the write,
surface error) rather than writing a wrong value.

Fail mode: if the security model is configured but unreachable, show a
permission-style modal before send: regex-redacted preview +
**Send (regex-only)** / **Cancel** / **Retry model**. If no security model is
configured, regex-only runs silently after a one-time notice.

### 3. Placeholder registry (session-scoped, accumulative)

`SecretRegistry` per session:

- `value → index` and `index → {value, kind, source, firstSeenAt}`.
- Counter monotonically increasing; same value reuses its index; indexes
  never reassigned.
- **Concurrency:** get-or-assign is a single mutex-guarded critical section
  (parallel tool results ingest concurrently).
- **Matching:** values are registered trimmed of surrounding whitespace and
  symmetric quotes; substitution applies **longest-value-first** over
  non-overlapping spans in a single pass (handles `hunter2` inside
  `hunter2-prod` without partial leakage).

### 4. Placeholder token format

```
[[OCSEC:a3f9c2:1]]
```

- `OCSEC` + 6-hex nonce (crypto/rand, once per session, stored in vault
  header) + decimal index. Nonce makes collision with real text effectively
  impossible → no escaping logic anywhere.
- Pure ASCII. Avoids `<|…|>` (model special tokens), `{{…}}` (template
  engines), `${…}`/`$(…)` (shell expansion during unmask).
- Match regex: `\[\[OCSEC:[0-9a-f]{6}:\d+\]\]`. Tokens with a different
  session's nonce are left untouched.

### 5. Vault

- Path: `~/.local/share/opencode/project/<slug>/secrets/<ses_id>.vault.json`
  (Windows: under `AppData/Local/opencode`). **Always under home**, even when
  sessions are project-local — keeps secrets out of the repo.
- Dir `0700`, file `0600`.
- Contents: `{ "nonce": "...", "secrets": { "1": {"value", "kind", "source",
  "firstSeenAt"} } }`.
- **Write protocol:** atomic (temp file + fsync + rename), and the vault
  entry MUST be durable **before** the session message containing its
  placeholder is written. Crash between the two leaves plaintext nowhere and
  an unreferenced vault entry (harmless), never an orphaned token.
- Deleting a session deletes its vault.
- Session import (`CloneClaudeSession`) ingests plaintext JSONL through the
  normal redaction pipeline; no vault copying exists or is needed (there is
  no generic ocode-native session fork today).
- Accepted v1 risk: plaintext-at-0600 on disk (same class as `.env`); OS
  keychain backend is the v2 follow-up.

### 6. Display re-substitution

Real values are substituted at render time, styled distinctly (dim underline)
to signal "vaulted". Surfaces covered:

- Message text: `displayTextForAgentMessage` (internal/tui/model.go:5910).
- Tool-call argument rendering: `formatToolCallHint`
  (internal/tui/tool_render.go:25).
- Permission prompt body (internal/tui/model.go:8018) — except secret-aware
  mode, see §7.
- Web API (internal/server/handler.go:166): serves **placeholders as-is**;
  the web UI has no vault access (deliberate).
- `/export-claude`: exports placeholders as-is (vault never leaves home dir).

`/secrets` display toggle: show placeholders instead of values (screen-share
mode). Copy-to-clipboard copies the real value (documented note: OS clipboard
managers persist history).

Accepted, documented risk: rendering real values means terminal scrollback /
`tmux capture-pane` / terminal logging hold plaintext. Mitigation: streaming
output is redacted before first paint (ingestion precedes display), and
debug/log sinks never receive raw values.

### 7. Tool execution unmask (secret-aware gating)

- Single substitution point: `HandleToolCall` (internal/agent/agent.go:1089)
  rewrites `args` before `tool.Execute(...)`. Covers built-in and MCP tools.
- **Secret-aware permission prompt:** when tool input contains OCSEC tokens,
  the prompt switches to a high-visibility mode — names each secret (index,
  kind, masked preview `hun•••r2`) and states the real value will be
  injected. **Egress escalation:** commands matching network heuristics
  (curl/wget/nc/ssh/scp, URL-bearing args) get a hard warning banner. This
  prompt is never skipped by allowlists when tokens are present.
- MCP caveat (documented): unmasked values forwarded to an external MCP
  process leave ocode's control; MCP tool *results* are re-redacted at
  ingestion like any tool output.
- Tool output echoing a secret is re-redacted at ingestion (§1), so plaintext
  never lands in history.

### 8. Config + persistence

`ocodeconfig.json` (global + project layering):

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

- `enabled` defaults to **false** (tripwire net still runs, §1).
- Targeted load-modify-write saver `SaveSecurityRedaction(...)` (pattern:
  `SaveTUITheme`, internal/config/ocodeconfig.go:879) — never a full
  in-memory `SaveOcodeConfig` snapshot.
- Implementation gotcha: `writeOcodeConfigFile` builds its payload from an
  explicit map (ocodeconfig.go:566) — the `security` key must be added there
  or it silently never persists.

### 9. UI surfaces

**Sidebar**: "Secrets" row showing `off` / `on (<model short name>)`,
click-to-toggle, persisting immediately via the targeted saver. Follows the
advisor-toggle pattern exactly: `sidebarRenderData` row index fields
(model.go:11539), populate in `buildSidebarRenderData` (model.go:11799),
hit-test helper like `sidebarAdvisorToggleForClick` (model.go:12432), click
handler (model.go:4588).

**`/secrets` slash command** (modal, mirrors `/permissions`):

- Enable/disable toggle.
- Security-model picker: **local providers only** — LM Studio models first,
  then Ollama. Cloud models are not listed (see §2 tier-2 hard restriction).
- Session secret list: index, kind, masked preview, source. Sorted by index
  ascending; paginated if long.
- Manual "add secret word" entry.
- Display-mode toggle (real values vs placeholders).

### 10. Edge cases

- Partial/split secrets across messages: exact-value matches only; partial
  leakage is the regex tier's responsibility.
- Compaction/summarization: after compaction, validate every OCSEC token in
  the summary against the registry; on mangled/orphaned tokens, retain the
  affected original message un-summarized. (Summarizer models may paraphrase
  tokens.)
- Feature enabled mid-session: prior plaintext already persisted; offer a
  one-time "scrub existing session history" action that re-runs redaction
  over stored messages and rewrites the session file.
- Feature disabled mid-session: existing tokens still render and unmask via
  vault; only detection stops. Tripwire net continues (§1).

## Testing

Unit:
- Registry: accumulation, same-value reuse, never-reassign, **concurrent
  get-or-assign race test**, longest-first overlapping/substring substitution,
  quote/whitespace normalization.
- Regex corpus: known-format true-positives; false-positive guards (git SHAs,
  lockfile hashes, base64 fixtures).
- Token round-trip: redact → render; redact → tool unmask; corrupted-token
  detection blocks file write-back.
- Vault: perms, nonce stability, atomic write + ordering invariant
  (crash-recovery test: vault entry without message reference is harmless;
  orphaned token must be impossible).
- Targeted config saver preserves unrelated keys; payload map includes
  `security`.

Integration:
- Each of the four ingestion carriers: secret in → disk contains no
  plaintext.
- Quick/advisor/title payload inspection: placeholders only (title call-site
  fix verified).
- Chokepoint net scans assembled payload incl. system prompt; tripwire prompt
  fires when disabled.
- Bash tool unmask round-trip; secret-aware prompt appears; egress warning on
  curl-with-token; MCP output re-redacted.
- Fail-mode modal (security model unreachable); local-only picker excludes
  cloud models.
- Sidebar toggle persists across restart.

## Out of scope (v1)

- OS keychain vault backend.
- Web-UI vault access / re-substitution.
- Cross-session shared secret registry.
- Contextual (tier-2) scanning of CLAUDE.md/context files and file contents.
- Config option to hard-block unmask into network-egress commands (v2
  candidate if egress warnings prove insufficient).
