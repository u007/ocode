# Opt-in PII redaction Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans (default) or superpowers:subagent-driven-development (long plans of independent tasks) to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend ocode's secret redaction with six opt-in PII category groups backed by `github.com/u007/dataprotect` (piiscrub), without letting PII reach any judge or guard payload.

**Architecture:** A new detector in `internal/redact` builds a piiscrub analyzer from only the enabled groups and merges its spans into the existing OCSEC vault-token flow. `DetectSecrets` (renamed from `Detect`) stays the secrets-only entry point; a new `Detect` adds PII and returns an error. PII categories ride on `GenericClient` (default empty) and are written only onto the main client, so side and judge clients are secrets-only by construction.

**Tech Stack:** Go 1.26, `github.com/u007/dataprotect` v0.1.0 (piiscrub: `analyzer`, `recognizers/...`; stdlib-only), Charm TUI, React SPA.

**Spec:** `docs/superpowers/specs/2026-11-24-pii-redaction-design.md`

**Detail:** decisions, not code — the default; the implementer writes the implementation.

## Global Constraints

- Dependency: `github.com/u007/dataprotect` pinned at **v0.1.0**. The repo must be public and tagged before Task 2. Do not commit a `replace` directive.
- Category names, exactly: `contact`, `financial`, `identity`, `legal`, `network`, `money`. All off by default; dormant unless `security.redaction.enabled`.
- Detection floor **0.7**, **no context boost**, per group; recognizers for disabled groups are never constructed.
- Replacement: the existing OCSEC vault tokens only. No piiscrub mask operators, no second placeholder format.
- Judge/guard/explain-digest/commit-error sites call `DetectSecrets` and must never receive PII categories.
- Token order: sort spans by `(Start, End, Kind)`; resolve overlaps secret → longest → earliest.
- A PII detection error is fatal. No unredacted fallback in either `fail_mode`.
- Memoize `detectPII` only; store offsets and kinds, never matched text.
- Any test that overrides `HOME` uses the package's `setHomeTree(t, home)`.
- `docs/` is OKF-bundle-owned: only the `context`/`context-writer` agent edits the curated body and `docs/index.md`. Specs and plans under `docs/superpowers/` are authored directly, matching the existing repo convention; this exception is called out because `docs/.okfignore` excludes only `okf/`.
- The new `pii_categories` config key is documented in the concept page (Task 6). No new environment variables.
- `gofmt` and `go vet` clean; the full suite runs once at the end (Task 6).

## Review Focus

1. **URL/IP/numeric literals in code.** With `network` or `money` enabled, a coding prompt loses `https://pkg.go.dev/…`, `10.0.0.1`, or a price. Expected: only enabled groups act, and they are off unless the user opted in. Pinned in Task 2 (per-group negatives) and Task 5 (typed input with groups off).
2. **Existing OCSEC tokens on the next turn.** A token must not be re-detected, double-registered, or have its index churned. Expected: `Detect(Render(text))` finds nothing and the safety net is idempotent. Pinned in Task 2 and Task 3.
3. **Judge fidelity under PII.** With `contact` on, the permission judge must still see the raw email it is judging while a secret in the same payload stays masked. Pinned in Task 3.
4. **`/reset-id` with live tokens.** Tokens already in the transcript must still resolve and the vault must move. Pinned in Task 4.
5. **Per-request latency on large prompts.** Once a group is on, the egress safety net rescans history every request; the memo must make repeats free. Pinned in Task 2 (memo benchmark) and exercised in Task 3.

---

### Task 1: Category config and validation

**Risk:** standard — additive config with validation, no behavior change until a category is set.

**Files:**
- Modify: `internal/config/ocodeconfig.go` (`RedactionConfig` `:1026`, `redactionConfigFile` `:1544`, `loadOcodeConfigFile` `:2034`, `SaveSecurityRedaction` `:4573`)
- Test: `internal/config/redaction_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `RedactionConfig.PIICategories []string` (`json:"pii_categories"`); `ValidatePIICategories([]string) error`; load and save both reject an unknown name.

- [ ] **Step 1: Write the failing tests.**
  - `TestPIICategoriesRoundTrip`: save `{contact, legal}`, reload, expect an equal slice.
  - `TestValidatePIICategories`: the six names pass; `CONTACT`, `bogus`, and a duplicate are errors.
  - `TestLoadRejectsUnknownPIICategory`: a file with `"pii_categories": ["bogus"]` makes `Load()` return an error.
  - `TestPIICategoriesDefaultEmpty`: an absent key loads as `nil`.

- [ ] **Step 2: Run them to verify they fail.** `go test ./internal/config -run 'PIICategories|ValidatePII'` — expected: undefined field/function.

- [ ] **Step 3: Implement.** Add the field to both structs, add `ValidatePIICategories` (exact lowercase match against the six names; reject duplicates), call it from `loadOcodeConfigFile` and from `SaveSecurityRedaction` before writing.

- [ ] **Step 4: Run to verify they pass.** `go test ./internal/config` — expected PASS.

---

### Task 2: Detector engine and the DetectSecrets/Detect split

**Risk:** high — security detection, branching overlap logic, a process-wide cache, and concurrency.

> **Prerequisite:** `github.com/u007/dataprotect` is public and tagged `v0.1.0`; run `go get github.com/u007/dataprotect@v0.1.0`. If it is not public yet, stop and tell the user.

**Files:**
- Create: `internal/redact/pii.go`
- Modify: `internal/redact/detect.go` (`DetectOpts` `:18`, `Detect` `:140`)
- Test: `internal/redact/pii_test.go` (new), `internal/redact/detect_test.go` (rename call sites only)

**Interfaces:**
- Consumes: `config.ValidatePIICategories` names (Task 1).
- Produces:
  - `type PIICategory string`; `PIIContact`, `PIIFinancial`, `PIIIdentity`, `PIILegal`, `PIINetwork`, `PIIMoney`; `AllPIICategories []PIICategory`; `ParsePIICategories([]string) ([]PIICategory, error)`.
  - `DetectOpts.PIICategories []PIICategory`.
  - `DetectSecrets(text string, customWords []string, opts DetectOpts) []Span` — today's behavior.
  - `Detect(text string, customWords []string, opts DetectOpts) ([]Span, error)` — secrets + PII; error only from PII.

- [ ] **Step 1: Write the failing tests.**
  - `TestPIICategoryDetection`: a table per group with one positive and one negative — for example contact detects `jane@example.com` with kind `pii:EMAIL` and detects nothing in `mail jane`.
  - `TestDetectSecretsIgnoresPIICategories`: `DetectSecrets` with categories set returns no PII span.
  - `TestDetectSkipsTokenOverlaps`: text containing `[[OCSEC:abc123:1]]` yields no span over the token.
  - `TestDetectIsIdempotentAfterRender`: detects, registers, substitutes, then `Detect` over the rendered text returns zero PII spans.
  - `TestDetectMemoKeyChanges`: the same text with different categories, custom words, or `FileContent` misses the memo.
  - `TestDetectTotalOrder`: two category orders produce identical Registry indexes for the same values.
  - `TestDetectErrorPropagates`: overriding the analyzer provider with an erroring analyzer makes `Detect` return the error.
  - `TestDetectConcurrent`: eight goroutines times fifty calls under `-race`, no panic, equal results.
  - `TestNoAnalyzerWhenCategoriesEmpty`: an empty category list never calls the analyzer provider (a counting stub proves zero calls).

- [ ] **Step 2: Run to verify they fail.** `go test ./internal/redact -race -run 'PII|Detect'` — expected: undefined types/functions.

- [ ] **Step 3: Implement.**
  - `pii.go`: the category type/constants/parse, `analyzerFor` memoised by canonical set in a `sync.Map` (`analyzer.New(analyzer.WithMinConfidence(0.7))` plus only the group's recognizers from the spec's table), `detectPII` (Analyze with `context.Background()`, entity → `Span{Kind: "pii:"+TYPE}`, skip token overlaps, local recover → error), and the bounded 512-entry memo over `sha256(text)|canonical(categories)|canonical(customWords)|FileContent` returning copies. Expose an unexported `var piiAnalyzerFor = analyzerFor` as the test seam.
  - `detect.go`: rename `Detect` to `DetectSecrets`; add `DetectOpts.PIICategories`; add `Detect` = secrets pass, then PII pass when categories are non-empty, then total sort and deterministic overlap resolution.
  - No other file changes. `net.go` keeps calling `DetectSecrets`.

- [ ] **Step 4: Run to verify they pass.** `go test ./internal/redact -race` — expected PASS. The existing `internal/redact` suite must also pass unchanged, proving `DetectSecrets` is behavior-identical to today's `Detect`.

- [ ] **Step 5: Record benchmarks.** Add `BenchmarkDetectPII_50KB` for contact, contact+financial, and all groups, plus a memo-hit benchmark. Run with `-bench . -benchtime 5x` and paste the numbers into the task's completion note.

---

### Task 3: Agent and client wiring, carve-out, error propagation

**Risk:** high — security boundary, signature changes on five call sites, and every judge path must stay secrets-only.

**Files:**
- Modify: `internal/agent/agent.go` (`:612` field, `attachRedactionHook` `:337`, `SetRedactionHook` `:3900`, `SetRedactionEnabled` `:3914`, `scanToolResult` `:3926` + callers `:2285`, `:2354`, `:2417`, `:6121`, `applySpecModel` `:6643`)
- Modify: `internal/agent/client.go` (`Redaction` `:275`, safety-net caller `:833`, `applyRedactionSafetyNet` `:5436`)
- Modify: `internal/agent/redaction_helpers.go` (`redactText` `:8`, `redactFileText` `:25`)
- Modify: `internal/agent/ask.go` `:280`, `internal/agent/advisor_checkpoint.go` `:226`
- Modify: `internal/agent/permission_typesafe.go` `:679`, `:698`, `:803`, `:845`; `internal/agent/permission_shellvars.go` `:109`, `:112`; `internal/agent/permission_judge_log.go` `:302`, `:324`; `internal/agent/network_guard_typesafe.go` `:1048`; `internal/agent/tool_digest.go` `:150`, `:283`
- Modify: `internal/notebus/redact.go`, `internal/agent/group_bus.go` `:72`
- Modify: `internal/server/handler_git_message.go` `:96`
- Test: `internal/agent/redaction_pii_test.go` (new), `internal/notebus/redact_test.go`

**Interfaces:**
- Consumes: `redact.Detect`, `redact.DetectSecrets` (Task 2).
- Produces:
  - `Agent.SetRedactionPIICategories([]redact.PIICategory)` and `Agent.applyPIIToClient(client LLMClient)`.
  - `GenericClient.PIICategories []redact.PIICategory` (zero value = secrets-only).
  - `func (a *Agent) scanToolResult(toolName, toolArgs, content string) (string, error)`.
  - `func (c *GenericClient) applyRedactionSafetyNet([]Message) ([]Message, error)`.
  - `func redactSecrets(text string, reg *redact.Registry) string`; `func redactWithPII(text string, reg *redact.Registry, cats []redact.PIICategory) (string, error)`.
  - `notebus.RedactBodyWith(text string, cats []redact.PIICategory) string`.

- [ ] **Step 1: Write the failing tests.**
  - `TestPermissionJudgePayloadKeepsRawPII`: with PII on, the judge payload still contains `jane@example.com` while an `AKIA…` key in the same payload is masked.
  - `TestSafetyNetAppliesPIIOnlyWhenConfigured`: `GenericClient.PIICategories` nil leaves the raw email; `{contact}` tokenizes it.
  - `TestSafetyNetIsIdempotent`: a second pass over tokenized messages returns them unchanged, and the same system message produces byte-identical output across calls (the cached prefix stays stable).
  - `TestScanToolResultPropagatesPIIError`: a detector failure returns a non-nil error and never returns the unmasked content.
  - `TestChildInheritsPIIAndSideClientsDoNot`: the child's main client carries the categories; a `bindSideClient` result carries none.
  - `TestNotebusRedactsPII`: `RedactBodyWith` with contact removes the raw email.

- [ ] **Step 2: Run to verify they fail.** `go test ./internal/agent ./internal/notebus -run '<names>'` — expected: undefined fields/functions.

- [ ] **Step 3: Implement.**
  - Add the agent field and setter; `applyPIIToClient` writes only onto `*GenericClient`; call it from `SetRedactionHook`, `SetRedactionPIICategories`, and `applySpecModel` after the swap.
  - Add `GenericClient.PIICategories`; `applyRedactionSafetyNet` passes them to `Detect` and returns the error; update its one caller.
  - `scanToolResult`: use `redactWithPII` on both branches, return the error, update the four callers.
  - Rename `redactText` → `redactSecrets`; add `redactWithPII`. `redactFileText` stays secrets-only.
  - Switch every secrets-only site listed in the spec §4.4 second table to `DetectSecrets`/`redactSecrets`/`redactFileText`.
  - PII-enabled sites: TUI typed input (Task 5), `scanToolResult`, the safety net, `advisor_checkpoint` (via `redactWithPII`), and the note bus (`RedactBodyWith` with the agent's categories through the `group_bus.go` closure).
  - `ask.go`: child inherits `SetRedactionHook` and `SetRedactionPIICategories`.
  - `bindSideClient`/`attachRedactionHook` keep attaching only the `NetHook`; do not add categories there.

- [ ] **Step 4: Run to verify they pass.** `go test ./internal/agent ./internal/notebus ./internal/server -run '<names>'` then `go test ./internal/agent ./internal/notebus` — expected PASS.

---

### Task 4: Vault rekey and `/mask list` separation

**Risk:** high — persistence, and the bug fix touches both reset-id handlers.

**Files:**
- Modify: `internal/redact/vault.go` (add `RekeyVault`)
- Modify: `internal/tui/model.go` `handleResetIDCmd` `:12487`; `internal/server` `handleResetSessionID`
- Modify: `internal/tui/commands.go` `/mask list` rendering
- Test: `internal/redact/vault_test.go`, `internal/tui/command_test.go`, the server reset-id test

**Interfaces:**
- Consumes: `redact.VaultPath`, `redact.SaveVault`, `redact.LoadVault`.
- Produces: `func RekeyVault(base, slug, oldID, newID string) error`.

- [ ] **Step 1: Write the failing tests.**
  - `TestRekeyVaultMovesFile`: write a vault for the old id, rekey, the old path is gone and the new path loads with the same nonce and entries.
  - `TestRekeyVaultMissingSourceIsNil`: no source file yields a nil error and creates nothing.
  - `TestRekeyVaultRefusesExistingTarget`: an existing destination yields an error and neither file changes.
  - `TestResetIDKeepsTokenResolution`: TUI registers a value, resets the id, `Render` still restores it, and the vault is under the new id.
  - `TestMaskListSeparatesPII`: a `pii:EMAIL` entry is listed under PII, not secrets.

- [ ] **Step 2: Run to verify they fail.** `go test ./internal/redact ./internal/tui -run 'Rekey|ResetID|MaskList'` — expected: undefined `RekeyVault`.

- [ ] **Step 3: Implement.** `RekeyVault` renames the file (no overwrite); both reset-id handlers call it with their old/new ids and keep the in-memory Registry; `/mask list` groups `pii:` kinds separately.

- [ ] **Step 4: Run to verify they pass.** `go test ./internal/redact ./internal/tui ./internal/server -run 'Rekey|ResetID|MaskList'` — expected PASS.

---

### Task 5: TUI and web/desktop settings

**Risk:** standard — command parsing and a settings form over an existing API.

**Files:**
- Modify: `internal/tui/commands.go` (`/mask` entry `:163`, `maskStatusText` `:1601`, `runMaskCmd` `:1761`), `internal/tui/redaction.go` (`initRedaction` `:25`, `syncRedactionRuntime` `:79`, `redactText` `:245`)
- Modify: `internal/server/handler_config.go` (`applyRedactionToAgent` `:2911`, the mask GET/PUT handlers)
- Modify: `web/src/components/Settings/SecurityForm.tsx`, `web/src/api/client.ts` (`getMaskAdvanced` `:2011`, `setMaskAdvanced` `:2021`)
- Test: `internal/tui/command_test.go`, `internal/tui/redaction_test.go`, `internal/server/handler_config_test.go`, `web/src/components/Settings/SecurityForm.test.tsx` (new)

**Interfaces:**
- Consumes: `redact.ParsePIICategories`, `Agent.SetRedactionPIICategories` (Tasks 1–3).
- Produces: `/mask pii [on|off <group>|none]`; `pii_categories` in the mask GET and advanced PUT payloads.

- [ ] **Step 1: Write the failing tests.**
  - `TestMaskPIICommand`: `pii` lists six off; `pii on contact` persists; `pii off contact` clears; `pii none` clears all; a bad group errors.
  - `TestTUITypedInputUsesPII`: with contact on, the sent message holds a token and the display holds the email; with contact off the text is unchanged.
  - `TestMaskAdvancedRoundTripsPII`: GET returns the slice, PUT sets it, and an invalid name is rejected with a 4xx.
  - `TestLiveReapplyKeepsPII`: applying the config to a live agent sets the categories on its client.
  - Web: `SecurityForm.test.tsx` asserts six unchecked checkboxes, and that toggling one and saving sends `pii_categories`.

- [ ] **Step 2: Run to verify they fail.** `go test ./internal/tui ./internal/server -run 'MaskPII|TUITyped|MaskAdvanced|LiveReapply'`; `cd web && npx vitest run src/components/Settings/SecurityForm.test.tsx` — expected FAIL.

- [ ] **Step 3: Implement.** Add the subcommand and status rows; set the agent categories in `syncRedactionRuntime` from `ResolveRedactionMode`'s config sibling; pass categories from the TUI `redactText` into `redact.Detect`; extend the server payload and live re-apply; add the six checkboxes and the warning copy to the form.

- [ ] **Step 4: Run to verify they pass.** The same commands, plus `cd web && npx tsc --noEmit`.

---

### Task 6: Documentation, full verification, mutation checks

**Risk:** standard — docs and verification, no product behavior.

**Files:**
- Create: `docs/concepts/redaction.md` (through `okf:context-writer` only)
- Modify: `CLAUDE.md` (cross-cutting invariants), `CHANGES.md`
- Regenerate: `docs/index.md` through the knowledge bundle, never by hand

**Interfaces:**
- Consumes: everything above.
- Produces: user-facing documentation and a green suite.

- [ ] **Step 1: Dispatch `okf:context-writer`** with the spec path and the as-built deltas. If the context agent still fails on the `openrouter` 404 model route, do not hand-edit `docs/`; record the gap in `TODO.md` and tell the user.
- [ ] **Step 2: Add the invariants to `CLAUDE.md`:** judge/guard payloads are secrets-only via `DetectSecrets`; PII attaches only to the main client; `detectPII` is the only memoized detector.
- [ ] **Step 3: Add the `CHANGES.md` entry.**
- [ ] **Step 4: Run the full suite.** `go test ./...` (expect the pre-existing failures recorded in `TODO.md`, notably `TestPermissions_TempDirAutoAllowed/read_tmp_tool`), `go vet ./...`, `gofmt -l` on changed files. Report any new failure as a blocker, not a caveat.
- [ ] **Step 5: Record the deferrals.** Add the spec §11 items to `TODO.md` under one `## Open: PII redaction follow-ups (2026-11-24)` section, each with the reason it is deferred.
- [ ] **Step 6: Mutation checks.** Run at least these and confirm each fails: (a) a judge call site switched from `DetectSecrets` to `Detect` with categories; (b) overlap resolution reversed; (c) the memo key without the category set; (d) `applyPIIToClient` applied in `attachRedactionHook`. Confirm the mutant compiles before judging it caught.
- [ ] **Step 6: Verify the documented anchors.** Re-derive every `file:line` in the spec and the concept page by symbol.
