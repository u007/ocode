---
type: Concept
title: Opt-in PII redaction
description: As-built opt-in PII redaction beside secret masking — six disabled-by-default piiscrub groups, the DetectSecrets/Detect split that keeps every judge and guard secrets-only, OCSEC vault-token reuse with /reset-id rekeying, the bounded memo, fatal detection errors, and measured cost and limits.
resource: internal/redact/pii.go
tags:
  - redaction
  - pii
  - piiscrub
  - security
  - vault
  - config
  - agent
timestamp: 2026-11-24T10:59:11Z
---
# Opt-in PII redaction

## Overview

ocode already redacts **secrets** on their way to a model. PII redaction adds a second, opt-in detector beside it, supplied by `github.com/u007/dataprotect` (package `piiscrub`, MIT, pinned `v0.1.0`). Detection is offline (regex and checksums), so it adds no network call and no tier-2 model.

A detected value is replaced by the same per-session `OCSEC` vault token secrets use. The model can still operate on it: `Registry.Resolve` (`internal/redact/registry.go:160`) restores tokens in tool arguments before execution (`internal/agent/agent.go:6227`), the registry renders them back for display (`Redactor.Render`, `internal/redact/redactor.go:133`), and the plaintext stays in the session's `0600` vault file.

The feature is **dormant** in two independent senses. A group that is not configured constructs no recognizer and no analyzer. A configured group still does nothing unless `security.redaction.enabled` is true: `GenericClient.applyRedactionSafetyNet` returns before scanning, and the group bus forces its category list to nil (`internal/agent/group_bus.go`).

The design rationale is the approved spec, [Opt-in PII redaction via piiscrub (design)](../superpowers/specs/2026-11-24-pii-redaction-design.md). This page records the shipped behavior; where the spec's proposed names differ, the as-built name is stated.

## Enabling it

The config key is `security.redaction.pii_categories`, an array of group names on `RedactionConfig.PIICategories` (`internal/config/ocodeconfig.go:1035`, `json:"pii_categories"`). It is empty by default and every group is opt-in. `ValidatePIICategories` (`internal/config/ocodeconfig.go:4586`) rejects an unknown or duplicate name on **both load and save**; `SaveSecurityRedaction` (`internal/config/ocodeconfig.go:4601`) is the only writer. There is no load-time leniency and no new environment variable.

- **TUI:** `/mask pii` lists each group and its state, `/mask pii on|off <group>` toggles and persists, `/mask pii none` clears. `handleMaskPIICmd` (`internal/tui/commands.go:1871`) validates against the closed set and reports a bad name verbatim; `maskPIIStatusText` (`internal/tui/commands.go:1660`) labels `network` and `money` as high-noise. `setRedactionPIICategories` saves through the config mutator and re-applies the runtime.
- **Web/desktop:** the Security settings form has a "PII (opt-in)" checkbox group (`web/src/components/Settings/SecurityForm.tsx:143`). The existing `GET /api/config/mask` and `PUT /api/config/mask/advanced` payloads carry `pii_categories`; no route was added. The server validates names and re-applies them to live sessions through `applyRedactionToAgent` (`internal/server/handler_config.go:2932`), which also fails closed to secrets-only if a live config somehow holds an invalid name.

## The six groups

| Group | Covers (representative recognizers) | Notes |
|---|---|---|
| `contact` | email, phone |  |
| `financial` | card (Luhn), IBAN, SWIFT/BIC, US routing, CVV, bank account, PIN, LEI, ISIN, Ethereum, Bitcoin |  |
| `identity` | SSN, passport, driver licence, tax ID, date of birth, SG NRIC/UEN, MY IC/SSM/TIN/passport, TH ID, PH TIN, IN GSTIN/PAN |  |
| `legal` | case number |  |
| `network` | IP address, URL | high-noise in code |
| `money` | currency amounts | high-noise in code |

`recognizersFor` (`internal/redact/pii.go:76`) maps a group to its recognizer constructors. `analyzerFor` (`internal/redact/pii.go:175`) builds one `analyzer.Analyzer` with `analyzer.WithMinConfidence(0.7)` and **no context boost**, adds only the enabled groups' recognizers, and caches the analyzer per canonical group set in a `sync.Map`. `Analyze` does not write to its receiver, so the shared instance is safe for concurrent use. A disabled group's recognizers are never constructed.

Kinds are `"pii:" + TYPE` (for example `pii:EMAIL`); the group is derived from the kind and is not stored separately.

## Detection pipeline

`internal/redact` exposes two entry points:

- `DetectSecrets` (`internal/redact/detect.go:146`) is the secrets-only pass. It has no category input and never reads `DetectOpts.PIICategories`, so a caller that must stay secrets-only cannot enable PII even by accident.
- `Detect` (`internal/redact/detect.go:198`) always runs the secret pass and, when `DetectOpts.PIICategories` is non-empty, merges in the PII spans. Its error comes only from PII detection.

`detectPII` (`internal/redact/pii.go:239`) runs the analyzer, drops spans that overlap an `OCSEC` token (`TokenPattern`), and converts a recognizer panic into an error with a local `recover` rather than re-panicking.

Merged spans are put in a **total order** — sort by `(Start, End, Kind)` — and overlaps are resolved secret first, then the longer span, then the earlier start, with kind as the final tiebreak (`mergeSpans`/`spanOutranks`/`resolveSpans`, `internal/redact/detect.go:212`, `:237`, `:254`). That total order is what keeps a repeated value's `Registry` index stable across turns and the cached prompt prefix byte-stable.

**Memo.** Only `detectPII` is memoized; `DetectSecrets` is not, because secret detection depends on the session registry. The cache is bounded to `piiMemoMaxSize` (512, `internal/redact/pii.go:196`) entries and is cleared on overflow, not evicted one at a time. The key is `sha256(text) | canonical(categories) | canonical(customWords) | FileContent`. Entries store offsets and kinds only, never matched text, and getters return copies. Tokenized and raw text hash differently, so they are separate entries; there is no cross-session hazard because PII detection is registry-independent.

## Where PII applies — and where it never does

**PII-capable (the conversation).** These surfaces use `Detect` with the agent's enabled groups:

| Surface | Site |
|---|---|
| TUI typed input, and re-scrub of existing user messages | `redactText` (`internal/tui/redaction.go:295`), `scrubExistingMessages` |
| Tool results | `scanToolResult` (`internal/agent/agent.go:3966`) through `piiRedactForToolResult` |
| Egress safety net over every outgoing message | `applyRedactionSafetyNet` (`internal/agent/client.go:5450`); its caller propagates the error |
| Advisor advice | `redactWithPII` (`internal/agent/advisor_checkpoint.go:227`) |
| Note bus bodies | `RedactBodyWith` (`internal/notebus/redact.go:34`), installed in `internal/agent/group_bus.go` |
| A `/btw` side-query child | inherits the parent's set (`internal/agent/ask.go:284`), because it is its own conversation |

As built, the secrets-only helper keeps its name `redactText` and is not called `redactSecrets`; the PII-capable sibling is `redactWithPII` (`internal/agent/redaction_helpers.go`). `piiRedactForToolResult` is a variable only so a test can force a detection failure and prove `scanToolResult` propagates it.

**Secrets-only by construction.** Judge, guard, explain-digest and commit-message-error payloads call `DetectSecrets`, `redactText` or `redactFileText`, none of which can accept a category:

| Payload | Site |
|---|---|
| Permission-judge arguments and context | `redactText` / `redactFileText` in `internal/agent/permission_typesafe.go` |
| Permission-judge shell variables | `redactText` in `internal/agent/permission_shellvars.go` |
| Permission-judge log | `redact.DetectSecrets` in `internal/agent/permission_judge_log.go` |
| Network-guard values | `redactText` in `internal/agent/network_guard_typesafe.go` |
| Explain digest | `redact.DetectSecrets` in `internal/agent/tool_digest.go` |
| Git commit-message error detail | `redact.DetectSecrets` in `internal/server/handler_git_message.go` |

The carve-out is a **masking** rule: those payloads stay secrets-only, so they can still carry raw PII (the permission judge is meant to see the email it is judging while a secret in the same payload stays masked). It is enforced by signatures, not discipline — `DetectSecrets` has no category parameter.

Categories ride on `GenericClient.PIICategories` (`internal/agent/client.go:281`), whose zero value is secrets-only. Only `applyPIIToClient` (`internal/agent/agent.go:3939`) writes it, and only onto a `*GenericClient`; `SetRedactionPIICategories` (`internal/agent/agent.go:3931`) is the entry point. `bindSideClient`/`attachRedactionHook` write only the tier-1 `NetHook`, so judgement, compaction, small-model and interpreter side clients never receive categories. `applySpecModel` re-applies them after it swaps the client, because that path goes through `bindSideClient`.

## Vault and `/reset-id`

Replacement reuses the `OCSEC` token vault; no second placeholder format exists. The registry resolves tokens back for display (`Redactor.Render`, `internal/redact/redactor.go:133`), and the vault file is per session under `secrets/<session-id>.vault.json`.

`/reset-id` re-keys the conversation, so the vault must move with it: `redact.RekeyVault` (`internal/redact/vault.go:203`) renames the old vault file to the new session path and leaves the in-memory registry intact, so tokens already in the transcript still resolve. The TUI calls it in `internal/tui/model.go:12542`; the server in `internal/server/handler_reset_id.go:124`. This closed a pre-existing hole where every other session-keyed surface moved and the vault did not.

## Invariants

1. **Six groups, all off by default.** Enabling a group only makes it available; it is dormant unless `security.redaction.enabled` is true.
2. **One replacement mechanism.** PII values become `OCSEC` vault tokens; piiscrub's mask operators are not used.
3. **Detection errors are fatal.** No unmasked fallback, in either `fail_mode`. `fail_mode` continues to govern only the tier-2 LLM scanner's error path.
4. **Judges and guards stay secrets-only.** They cannot pass categories because `DetectSecrets` has no category input.
5. **PII attaches only to the conversation client.** Categories are written solely by `applyPIIToClient`, never by `bindSideClient`/`attachRedactionHook`.
6. **Token order is total and deterministic.** Sort `(Start, End, Kind)`; resolve overlaps secret → longest → earliest.
7. **The memo stores no text.** Offsets and kinds only; it is cleared, not LRU-evicted, at 512 entries.
8. **Categories validate identically on load and save.** Unknown or duplicate names are errors in both paths.

## Performance

Reference measurements with the shipped feature on an Apple M1 Max, 50 KB of prose: `contact` ≈ 19 ms, `contact` + `financial` ≈ 31 ms, all six groups ≈ 107 ms; a short chat message with all groups ≈ 0.71 ms. Exact wall time varies with machine and load, but the order of magnitude is tens of milliseconds for a large document.

A memo hit costs ≈ 23 µs: the `sha256` of the text plus the map probe. It is **not** sub-microsecond — hashing the input is unavoidable because the key is a content fingerprint. That still makes a repeated scan of unchanged history effectively free next to the initial tens of milliseconds.

With no category enabled there is no analyzer, no memo entry, and no new code path in `DetectSecrets`; the cost is zero.

## Limits and known gaps

- **No NLP entities.** `PERSON`, `ORGANIZATION` and `LOCATION` live in piiscrub's separate `sidecar/` module, which is not imported, so names, organizations and addresses are not detected.
- **No OCR and no image scanning.** `Message.Images` is never scanned; `applyRedactionSafetyNet` walks `Content` only.
- **`network` and `money` are noisy on code.** URLs, IPs and numeric literals are legitimate coding content; both groups are opt-in and labelled high-noise in the TUI and the web form.
- **No mid-scan cancellation.** piiscrub's recognizers ignore the context passed to `Analyze`; a scan is bounded by input size, not interruptible.
- **No `CUSTOM_TERM` recognizer.** `custom_words` already covers caller-supplied terms, and a second mechanism would double-register the same value.
- **PII is not masked in judge/guard payloads** by design (see the carve-out above); only the secrets on those payloads are masked.

## Related

- [Opt-in PII redaction via piiscrub (design)](../superpowers/specs/2026-11-24-pii-redaction-design.md) — the approved design, including the full recognizer table, the test matrix and the threat model.
- [Opt-in PII redaction implementation plan](../superpowers/plans/2026-11-24-pii-redaction.md) — the task breakdown and review focus.
- [Prompt cache stability](prompt-cache-stability.md) — why the deterministic token order and the user-role tail injectors matter.
- [Auto-permission enforced categories](auto-permission-enforced-categories.md) and [Auto-permission judge withholds credentials](../gotchas/auto-permission-judge-withholds-credentials.md) — the judge side of the secrets-only carve-out.
