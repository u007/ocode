# PLAN — `/mask mode` + sensitive-content tier-2 scanning

## Goal

Reduce how often the slow tier-2 secret-detection LLM is called, while *extending*
safety to sensitive file reads and DB/bash output. Two-tier scanning is retained.

The original complaint: tier-2 (the local LLM scanner) feels slow. Investigation
found the LLM only ever scans the **typed user message** (once per turn, gated by
`QuickScan`); file reads and tool results never reach it. So the work is twofold:

1. **Reducer (Phase 1):** make the user-input gate explicit and mode-driven so the
   LLM fires less in normal use.
2. **Safety extension (Phase 2):** add coverage the LLM currently misses — sensitive
   file reads (`.env`, keys) and secrets surfaced in DB/bash tool output — without
   recreating the latency problem.

## How it works (target behaviour)

| Surface | lenient (default) | full |
|---------|-------------------|------|
| Typed user message | tier-2 LLM only if input contains a sensitive keyword or a QuickScan value-pattern | tier-2 LLM **always** |
| Sensitive file read (`.env`, `*.pem`, …) | tier-2 **LLM** always | tier-2 **LLM** always |
| Other tool results (DB/bash/normal reads) | chat-mode **regex** only (no LLM) | chat-mode **regex** only (no LLM) |
| All messages, every step | tier-1 regex safety net (unchanged) | tier-1 regex safety net (unchanged) |

- **Mode** governs only the *typed user message* aggressiveness.
- **Sensitive file reads** always use the LLM in both modes — `.env` values are often
  `CUSTOM_NAME=<high-entropy>` with no keyword and no known format, which only the LLM
  catches.
- **DB/bash output** uses fast keyword+entropy regex only (decision: speed over
  completeness). Known gap: tabular output with no `=`/`:` delimiter
  (`| password | hunter2 |`) is missed. Documented, not covered by LLM.
- **No model configured:** if redaction is enabled but no tier-2 model/base_url is set,
  emit a one-time startup notice that scanning is regex-only (tier-1 + chat-mode on
  tool results); the LLM tiers are skipped.

## Key correctness rule (the anchor)

When the LLM (or chat-mode regex) finds a secret that the tier-1 known-format regex
**cannot** match, the safety net will NOT mask it — `applyRedactionSafetyNet` only calls
`Substitute` on a message when `Detect` finds a regex span in it. Therefore every new
scan point must **register AND substitute the value in-place** at the moment of scanning,
never relying on the net. This is the single most important invariant; it gets the first
test.

---

## Phase 1 — `/mask mode` (reduces LLM calls). Ship & verify first.

### 1. Config: add `Mode`
- Add `Mode string` (`json:"mode"`) to `RedactionConfig` and `*string` to
  `redactionConfigFile`; wire into `applySecurityConfig` and `defaultSecurityConfig`.
- Values: `"lenient"` (default when enabled) and `"full"`.
- Back-compat: `Mode` supersedes the legacy `skip_llm_if_clean`. When `Mode` is empty,
  derive it from `skip_llm_if_clean` (`false` → `full`, else `lenient`). `fail_mode` is
  orthogonal (it only controls behaviour on scanner *error*); fix the stale comment in
  `internal/tui/redaction.go` that claims `block` forces always-scan.
- Files: `internal/config/ocodeconfig.go`.
- Verify: config round-trips `mode`; legacy `skip_llm_if_clean=false` still yields `full`.

### 2. Gate predicate
- Add `redact.WarrantsLLMScan(text string) bool` = existing `QuickScan` value-patterns
  OR sensitive-keyword presence.
- Add a documented, sorted keyword set: `token`, `pass`/`password`, `secret`,
  `api[_-]?key`, plus prefix indicators `AWS_`, `ANTHROPIC_`, `GEMINI_`, `OPENAI_`,
  generic `*_API_KEY` / `*_TOKEN` / `*_SECRET`.
- Files: `internal/redact/scanner.go` (or new `gate.go`).
- Verify: table test — keyworded input warrants scan; benign prose does not; bare
  value-pattern (e.g. `AKIA…`) still warrants scan via QuickScan.

### 3. Apply mode to user-input scan
- `applyTier2Scan` (TUI) takes the mode. `full` → always scan; `lenient` → scan only if
  `WarrantsLLMScan(masked)`. Replace the current `skipLLMIfClean && !QuickScan` gate.
- Wire `m.redactMode` from config in `model.go` (alongside `redactFailMode`).
- Files: `internal/tui/redaction.go`, `internal/tui/model.go`.
- Verify: lenient + benign input → no LLM call; lenient + keyworded input → LLM call;
  full + benign input → LLM call.

### 4. `/mask mode` subcommand + status
- Add `mode [lenient|full]` to `runMaskCmd`: no arg shows current mode; arg sets &
  persists via `SaveSecurityRedaction`.
- Update the usage string, `maskStatusText` (show active mode), the `/mask` help entry,
  and the affected command tests.
- Files: `internal/tui/commands.go`, `internal/tui/command_test.go`.
- Verify: `/mask mode full` persists and status reflects it.

---

## Phase 2 — sensitive-file + DB/bash coverage (adds safety). Ship after Phase 1 verified.

### 5. Sensitive-file matcher
- Add `redact.IsSensitiveFile(path string) bool` — sorted, commented patterns:
  `.env` and `.env.*`, `*.pem`, `*.key`, `id_rsa*`/`id_dsa*`/`id_ecdsa*`/`id_ed25519*`,
  `*.pfx`, `*.p12`, `.npmrc`, `.netrc`, `*credentials*`, `secrets.*`, `.pgpass`.
- Files: new `internal/redact/sensitive.go` + test.
- Verify: table test of sensitive vs ordinary (`.go`, `.ts`, `.tsx`, `README.md`) paths.

### 6. Shared scan helper + agent scanner wiring
- Add `redact.ScanAndMask(content string, s Scanner, reg *Registry) (string, error)` =
  scan → register findings → **substitute in-place**; return masked content. Refactor the
  existing user-input path to use it (DRY).
- Add `Agent.SetRedactionScanner(redact.Scanner)` + field; TUI sets it when redaction is
  enabled and a scanner is configured.
- Files: `internal/redact/redactor.go` (or `scanner.go`), `internal/agent/agent.go`,
  `internal/tui/model.go`.
- **Test-first anchor:** register a novel value with no regex-detectable sibling →
  `ScanAndMask` returns content with it masked (proves no dependence on `Detect`).

### 7. Scan tool results at the aggregation loop
- In `agent.go` (~line 685, the single-threaded loop that appends `results` to
  `newMsgs`/`messages` and fires `OnMessage`): for each result, read the matching
  `resp.ToolCalls[i].Function.Arguments` to get the tool name + `path`.
  - If tool is `read` and `IsSensitiveFile(path)` and a scanner+registry are present →
    `ScanAndMask` via the **LLM** scanner (both modes).
  - Else → run tier-1 `Detect` in **chat mode** (keyword+entropy, no LLM) and substitute
    in-place. Catches `password=<value>` in DB/bash output.
  - Mutate the result content *before* it is appended and before `OnMessage`.
- Rationale for location: the net is file-mode and re-runs every step; scanning once here
  with chat-mode + in-place substitution is both correct and cheaper. Leave the net as-is.
- Files: `internal/agent/agent.go`.
- Verify: reading a stub `.env` registers/masks its values; a bash result containing
  `password=secretval` is masked; a normal `.go` read is untouched and triggers no LLM.

### 8. No-model startup warning
- On TUI init, when redaction is enabled but no tier-2 scanner is configured, append a
  one-time notice: scanning is regex-only (tier-1 + chat-mode tool-result regex); set a
  model with `/mask model` to enable LLM tier-2.
- Files: `internal/tui/model.go` (or wherever the startup banner/messages are appended).
- Verify: enabled + no model → notice shown once; enabled + model → no notice.

---

## Docs (required)
- Update `skills/ocode-usage/SKILL.md` and `README.md`: `/mask` usage incl. `mode`,
  the lenient-vs-full table above, the sensitive-file LLM behaviour, the DB/bash
  regex-only behaviour + its tabular gap, and the no-model regex-only fallback.
- Add a `CHANGES.md` entry.

## Known limitations (document, do not fix now)
- DB/bash secret detection is regex-only; tabular/CSV output without a `=`/`:` delimiter
  is not caught.
- Only the `read` tool gets sensitive-file LLM treatment; `bash cat .env` is treated as
  generic bash output (regex-only).
- A novel secret echoed by the model in a *later* message is not re-masked by the net
  (pre-existing behaviour; in-place substitution covers the scan point only).

## Sequencing
Phase 1 first — it fixes the original "it's slow" complaint and is independently
shippable. Phase 2 adds safety and can iterate without blocking Phase 1. The in-place
substitution test (task 6) lands before any Phase 2 wiring.
