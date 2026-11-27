---
type: Design
title: Opt-in PII redaction via piiscrub (design)
description: Approved design for extending ocode's secret redaction with opt-in PII detection supplied by github.com/u007/dataprotect (piiscrub). Six category groups (contact, financial, identity, legal, network, money) start disabled; when enabled they cover the LLM-bound surfaces, reuse the OCSEC vault-token mechanism, and stay out of every judge and guard payload by construction. Covers the config schema, per-group analyzer construction, the memo that makes the egress safety net free, the DetectSecrets/Detect API split, the agent/client wiring matrix, the /reset-id vault fix, TUI and web settings, invariants, failure modes, performance, and the test matrix.
tags:
  - design-spec
  - redaction
  - pii
  - piiscrub
  - secrets
  - vault
  - security
timestamp: 2026-11-24T04:37:11Z
status: approved
---
# Opt-in PII redaction via piiscrub (design)

**Status:** Approved design (brainstorming architectural path), 2026-11-24. Implementation-ready; no product code is written by this page.

**Anchor convention:** every `file:line` citation is **as-of 2026-11-24**. Re-derive each anchor by symbol before relying on it; line numbers drift when code above a citation changes.

## 1. Goal and threat model

ocode already redacts **secrets** from text on its way to a model: API keys, tokens, PEM blocks, URL credentials, and name-gated env assignments. This design adds **PII** detection from `~/www/dataprotect` (module `github.com/u007/dataprotect`, package `piiscrub`, MIT): emails, phone numbers, card numbers, national IDs, bank/finance identifiers, legal case numbers.

The threat is a person's data reaching a cloud model or a durable artifact. Detection is offline (regex and checksums), so it needs no tier-2 model and adds no network call.

**Guarantees when a category is enabled:**

- A detected value is replaced by the existing per-session `OCSEC` token before the text leaves the host. The vault keeps the plaintext under the session's `0600` vault file, exactly as it does for secrets.
- The model can still use the value: tool arguments are resolved back from tokens, and the UI renders the real value.
- No judge, guard, or explain-digest payload ever receives a PII token or a PII-masked value. Those call sites are secrets-only by construction, not by convention.

**Cost when no category is enabled:** zero. `DetectSecrets` is unchanged, the memo is never consulted for PII, and no piiscrub recognizer is constructed.

## 2. Locked user decisions (do not re-open)

1. **Six opt-in groups:** `contact`, `financial`, `identity`, `legal`, `network`, `money`. All start off. Enabling a group only makes it available; it is still dormant unless `security.redaction.enabled` is true.
2. **Surfaces:** PII covers every LLM-bound surface that secrets cover — typed input, tool results, file reads, system/context messages, and the egress safety net.
3. **Replacement:** reuse the OCSEC vault tokens. Do not use piiscrub's mask operators and do not add a second placeholder mechanism.
4. **Module:** the dataprotect repo is made public and tagged `v0.1.0`. Until the tag exists, local development uses a `replace` directive that is **never committed**.
5. **Judge carve-out:** judge and guard payloads stay secrets-only — the permission-judge arguments and context, the permission shell-variable view, the permission judge log, the network-guard values, the explain digest, and the git commit-message error detail.

## 3. Non-goals and known gaps

- **No NLP entities.** piiscrub's `PERSON`, `ORGANIZATION`, and `LOCATION` come from the separate `sidecar/` module, which is not imported. ocode does not detect names or addresses.
- **No `CUSTOM_TERM` recognizer.** ocode's existing `custom_words` already covers caller-supplied terms; two mechanisms would double-register the same value.
- **No tier-2 change.** The local LLM scanner keeps its current prompt and behavior. PII is offline detection only.
- **No image or OCR PII.** Out of scope.
- **Cancellation is not possible mid-scan.** `piiscrub`'s `Analyze` takes a context but its recognizers ignore it. A 50 KB scan is bounded at tens of milliseconds (section 6), so this is accepted; it is not a blocking network call.
- **Network and money are noisy in code.** `https://…`, `10.0.0.1`, and numeric literals are legitimate coding content. They remain available because the user asked for them, but the UI labels both groups as high-noise and they are not default-on.
- **piiscrub is MIT.** Vendoring is not used; the module is a dependency. Attribution lives in `THIRD_PARTY`-style docs if one exists, otherwise in the concept page.

## 4. Architecture

### 4.1 Config

`internal/config/ocodeconfig.go`:

- `RedactionConfig` (`:1026`) gains `PIICategories []string` with `json:"pii_categories"`.
- `redactionConfigFile` (`:1544`) gains `PIICategories []string  json:"pii_categories"`.
- One validator, `ValidatePIICategories([]string) error`, is used on **both load and save**. An unknown name is an error in both places; there is no load-time leniency.
- `SaveSecurityRedaction` (`:4573`) is the only writer; never a whole-config snapshot save.
- `ResolveRedactionMode` (`:4561`) is unchanged.

Group to recognizer mapping (all constructors exist in the pinned version):

| Group | Recognizers |
|---|---|
| `contact` | `regex.NewEmailRecognizer`, `regex.NewPhoneRecognizer` |
| `financial` | `checksum.NewLuhnValidator`, `checksum.NewIBANValidator`, `finance.NewSWIFTBICRecognizer`, `finance.NewUSRoutingRecognizer`, `finance.NewCardCVVRecognizer`, `finance.NewBankAccountRecognizer`, `finance.NewPINRecognizer`, `finance.NewLEIRecognizer`, `finance.NewISINRecognizer`, `finance.NewEthereumRecognizer`, `regex.NewBitcoinRecognizer` |
| `identity` | `regex.NewSSNRecognizer`, `identity.NewPassportRecognizer`, `identity.NewDriverLicenseRecognizer`, `identity.NewTaxIDRecognizer`, `identity.NewDateOfBirthRecognizer`, `national.NewSGNRICRecognizer`, `national.NewMYICRecognizer`, `national.NewTHIDRecognizer`, `national.NewPHTINRecognizer`, `national.NewINGSTINRecognizer`, `national.NewINPANRecognizer`, `national.NewSGUENRecognizer`, `national.NewMYSSMRecognizer`, `national.NewMYTINRecognizer`, `national.NewMYPassportRecognizer`, `national.NewSGPassportRecognizer` |
| `legal` | `legal.NewCaseNumberRecognizer` |
| `network` | `regex.NewIPRecognizer`, `regex.NewURLRecognizer` |
| `money` | `finance.NewMoneyRecognizer` |

Every group uses `analyzer.New(analyzer.WithMinConfidence(0.7))` with **no** context boost, matching `piiscrub.NewScrubber`. Recognizers for disabled groups are never constructed.

### 4.2 Detection engine

New file `internal/redact/pii.go`:

- `type PIICategory string`; constants `PIIContact`, `PIIFinancial`, `PIIIdentity`, `PIILegal`, `PIINetwork`, `PIIMoney`; `AllPIICategories`; `ParsePIICategories`.
- `analyzerFor(cats []PIICategory) *analyzer.Analyzer` — memoised in a package-level `sync.Map` keyed by the canonical sorted set. `Analyzer.Analyze` does not write to the receiver, so one instance is safe for concurrent use (pinned by a `-race` test).
- `detectPII(text string, cats []PIICategory, customWords []string) ([]Span, error)`:
  - Runs `Analyze(context.Background(), text)`.
  - Converts `recognizers.Entity` to `Span{Start, End, Kind: "pii:" + string(e.Type)}`.
  - Skips a span that overlaps an existing `TokenPattern` match.
  - A local `recover` converts a panic into an error (it does not re-panic; this is the "turn a panic into an error" contract, distinct from `crashguard`).
- **Memo:** `detectPII` only — never `DetectSecrets`, because secret detection depends on the session `Registry`. Key: `sha256(text) | canonical(cats) | canonical(customWords) | FileContent`. Bounded to 512 entries under a mutex; on overflow the map is cleared. It stores only offsets and kinds, never matched text, and returns copies. Tokenized text and raw text hash differently, so they are separate entries; there is no cross-session hazard because PII detection is independent of any registry.

### 4.3 API split

`internal/redact/detect.go`:

- Rename the current `Detect` (`:140`) to `DetectSecrets(text, customWords, opts) []Span`. It keeps today's behavior exactly.
- Add `Detect(text, customWords, opts) ([]Span, error)`. It always runs the secret pass, and when `opts.PIICategories` is non-empty it merges the PII spans. The error comes only from PII detection.
- `DetectOpts` (`:18`) gains `PIICategories []PIICategory`.

The split is the enforcement mechanism: a call site that must stay secrets-only calls `DetectSecrets` and **cannot** enable PII, because that function has no category input and does not read `DetectOpts.PIICategories`.

**Ordering and overlap.** After the merge, sort by `(Start, End, Kind)` for a total order. Resolve overlaps deterministically: secret first, then the longer span, then the earlier start. `Registry.GetOrAssign` (`internal/redact/registry.go:63`) assigns a single global monotonic index, so this sort is what keeps a value's token stable across turns and keeps the cached prompt prefix byte-stable.

### 4.4 Wiring

`internal/agent/agent.go`:

- `Agent` gains `redactionPII []redact.PIICategory` and `SetRedactionPIICategories(cats []redact.PIICategory)`.
- `SetRedactionHook` (`:3900`) and `SetRedactionPIICategories` both call one helper, `applyPIIToClient`, which writes the categories onto `a.client` **only when it is a `*GenericClient`**. `applySpecModel` (`:6643`) calls the same helper after it swaps the client, because that path currently calls `bindSideClient` and would otherwise drop PII.
- `internal/agent/client.go`: `GenericClient` gains `PIICategories []redact.PIICategory` (zero value = secrets-only). `applyRedactionSafetyNet` (`:5436`) passes them to `Detect` and returns `([]Message, error)`; its single caller at `:833` propagates.
- `scanToolResult` (`:3926`) returns `(string, error)` and uses a PII-capable helper for both the sensitive-read branch and the generic branch. Its four callers (`:2285`, `:2354`, `:2417`, `:6121`) propagate the error.
- `internal/agent/redaction_helpers.go`: rename `redactText` (`:8`) to `redactSecrets`; add `redactWithPII(text, reg, cats) (string, error)`.
- `internal/agent/ask.go`: a child inherits both the hook and `a.redactionPII`.
- `internal/notebus/redact.go`: add `RedactBodyWith(text, cats) (string, string)` — it keeps the existing `[REDACTED:kind]` form because the bus has no registry. `internal/agent/group_bus.go:72` installs a closure that captures the agent's categories.

**PII-enabled matrix (LLM-bound main conversation):**

| Site | Anchor |
|---|---|
| TUI typed input | `internal/tui/redaction.go:249` (the TUI's own `redactText`), `:48` (`scrubExistingMessages`) |
| TUI tool-result display | `internal/tui/model.go:14879` |
| Agent tool results | `internal/agent/agent.go:3926` (`scanToolResult`) |
| Egress safety net | `internal/agent/client.go:5449` |
| Advisor advice | `internal/agent/advisor_checkpoint.go:226` |
| Notes bus | `internal/notebus/redact.go:29` |

**Secrets-only matrix (judge, guard, explain digest, error text):**

| Site | Anchor |
|---|---|
| Permission judge args | `internal/agent/permission_typesafe.go:679` |
| Permission judge context | `internal/agent/permission_typesafe.go:698`, `:803`, `:845` (via `redactFileText`) |
| Permission judge payload | `internal/agent/agent.go:4844`, `:4851` |
| Permission shell vars | `internal/agent/permission_shellvars.go:109`, `:112` |
| Permission judge log | `internal/agent/permission_judge_log.go:302`, `:324` |
| Network guard values | `internal/agent/network_guard_typesafe.go:1048` |
| Explain digest | `internal/agent/tool_digest.go:150`, `:256` |
| Git commit-message error | `internal/server/handler_git_message.go:96` |

All sites in the second table switch to `DetectSecrets` / `redactSecrets` / `redactFileText`. `redactFileText` stays secrets-only.

### 4.5 Vault and rekey

- `Kind` is `"pii:" + TYPE` (for example `pii:EMAIL`). The group is derivable from the type and is not stored separately.
- Registration order is the sorted span order from 4.3, so a repeated value keeps its index across turns.
- `/mask list` labels `pii:` entries and keeps them distinct from secret entries.
- **`/reset-id` fix:** add `redact.RekeyVault(base, slug, oldID, newID) error`. It moves `secrets/<old>.vault.json` to the new path and keeps the in-memory `Registry` (same nonce), so existing tokens in the transcript still resolve. The TUI handler is `internal/tui/model.go:12487`; the server handler is `handleResetSessionID`. This fixes a pre-existing bug: every other session-keyed surface is rekeyed and the vault was missed.
- Vault restore on resume already exists in `redact.NewSessionRedactor` (`internal/redact/redactor.go:203`).

### 4.6 UX

- **TUI:** `/mask pii` prints each group and its state; `/mask pii on|off <group>` toggles and persists; `/mask pii none` clears. `maskStatusText` (`internal/tui/commands.go:1601`) shows the enabled groups. `network` and `money` are labelled high-noise in the help text.
- **Web/desktop:** `SecurityForm` gains a "PII (opt-in)" checkbox group with the same warnings. The existing `GET /api/config/mask` / `PUT /api/config/mask/advanced` payloads gain `pii_categories`; no new route. The server validates names and re-applies to live sessions through `applyRedactionToAgent` (`internal/server/handler_config.go:2911`).
- **Validation errors** from `/mask pii on <bad>` and from the Settings save are shown verbatim; there is no silent ignore.

## 5. Invariants

1. **Judge and guard payloads never contain PII masking.** They call `DetectSecrets`; they cannot pass categories.
2. **PII attaches only to the main client.** `GenericClient.PIICategories` is empty everywhere else, and only `Agent.applyPIIToClient` writes it.
3. **`DetectSecrets` has no PII input.** The carve-out is enforced by the signature, not by discipline.
4. **The PII memo stores no text** — offsets and kinds only.
5. **Token order is total and deterministic** — sort `(Start, End, Kind)`; overlap resolution is secret, then longest, then earliest.
6. **A PII detection error is fatal.** No fallback to unredacted text, in either `fail_mode`. `fail_mode` continues to govern only the tier-2 scanner.
7. **PII is dormant unless `security.redaction.enabled`.** Configuring a category does not turn redaction on.
8. **Categories validate on load and save with the same function.**
9. **`/reset-id` carries the vault file and the in-memory registry.**
10. **No category means no cost.** No analyzer, no memo entry, no new code path in `DetectSecrets`.

## 6. Performance

Measured on the pinned piiscrub version, Apple M1 Max, 50 KB of prose:

| Analyzer | Per scan |
|---|---|
| `NewScrubber` (all 38 types) | 107 ms |
| contact + financial | 31 ms |
| contact only | 19 ms |
| short chat message (all types) | 0.71 ms |

`analyzer.DisableEntityTypes` does **not** save time — recognizers still run and only results are filtered. The design therefore builds the analyzer from the enabled groups' recognizers only.

The memo makes every repeat scan O(1). The egress safety net re-reads the whole message history on every request; without the memo an enabled category would add tens of milliseconds per request. With it, only text never seen before pays.

## 7. Failure modes

- **Recognizer panic:** the local `recover` in `detectPII` returns an error. `scanToolResult` and `applyRedactionSafetyNet` propagate it; the turn fails. The process survives.
- **Recognizer error:** same path. `Analyze` already returns `nil, err`; the error is propagated.
- **Invalid category in config:** startup fails validation (section 4.1). There is no partial configuration.
- **Vault write failure:** the existing redactor error path applies; the turn fails before unredacted text is sent.
- **Overlapping detections:** deterministic resolution per section 4.3; a token is never re-detected because token overlaps are skipped.

## 8. Testing

**Unit (`internal/redact`)**

- Group to recognizer mapping: each group detects its positives and not a random negative.
- Each group's positive and negative table (emails, phones, Luhn cards, IBANs, SSNs, national IDs, IPs, URLs, money).
- `DetectSecrets` ignores `DetectOpts.PIICategories` entirely.
- Custom words and PII coexist; the same value is registered once.
- A span overlapping `TokenPattern` is skipped; `Detect(Render(text))` returns zero spans.
- Memo: same input twice returns equal spans; changing categories, custom words, or `FileContent` misses the memo.
- `-race` test with concurrent `Detect` calls.
- Total order: shuffle the pass order and assert identical token indexes.
- Error propagation: a stub analyzer returning an error surfaces from `Detect`.

**Integration (`internal/agent`, `internal/server`, `internal/tui`)**

- Vault round-trip: a detected email becomes a token, the vault stores it, and `Render` restores it.
- Tool-argument resolution returns the real value.
- Child agent inherits categories and its own side clients do not.
- `scanToolResult` and the safety net return errors on detector failure.
- **Carve-out pin:** with PII on, a permission-judge payload still contains the raw email, while a secret in the same payload is still masked.
- `/mask pii on|off|none` persists and updates status.
- `GET/PUT /api/config/mask[/advanced]` round-trips `pii_categories`; an invalid name is rejected.
- `/reset-id` moves the vault file and keeps token resolution.
- PII tests that override `HOME` use `setHomeTree`.

**Performance**

- Benchmarks for the three analyzer sets at 50 KB and the memo hit path (target: sub-microsecond).
- A test asserting that no analyzer is constructed when categories are empty.

**Mutation checks** on the carve-out (a judge call site switched to `Detect` must fail the pin), on the overlap resolution, and on the memo key.

## 9. Documentation

- New `docs/concepts/redaction.md`: tiers, groups, vault, carve-outs, config, performance, limits. Authored through `okf:context-writer` because `docs/` is bundle-owned.
- `CLAUDE.md`: add the carve-out invariant (judge/guard payloads stay secrets-only) and the "PII attaches only to the main client" rule to the cross-cutting rules.
- `docs/index.md`: regenerated through the knowledge bundle, not hand-edited.
- `CHANGES.md`: user-facing entry.
- `TODO.md`: the deferred items below.

## 10. Migration and compatibility

- Existing users see no change: `pii_categories` is absent and `DetectSecrets` is byte-identical to today's `Detect`. `internal/redact` is an internal package, so the rename and the new `Detect` signature have no consumers outside this module.
- The module dependency is added only after `github.com/u007/dataprotect` is public and tagged `v0.1.0`. Until then, implementation uses a local `replace` that must not be committed.
- The `/reset-id` vault move is a bug fix; it also moves vaults created before this feature.

## 11. Deferred

- Per-group confidence tuning for `network` and `money` (start at 0.7; raise only if measured false positives justify it).
- NLP entities (`PERSON`, `ORGANIZATION`, `LOCATION`) via the piiscrub sidecar.
- Address and free-text location detection.
- Image/OCR PII.
- A user-visible report of how many values each group redacted per session.
