SECURITY-FOCUSED PRE-MERGE REVIEW — 131 files, ~4010 insertions / 6028 deletions (28 deleted spec docs)
Review standard: strict, teach-focused, urgency-labeled (HIGH = merge blocker, MEDIUM = fix soon, LOW = nice-to-improve).
Cross-cutting notes emitted to shared bus at bottom.

---

## 1. ❗ Critical Issues (HIGH — must fix before merge)

- **HIGH — None confirmed as a merge-blocking security regression.** The 131-file change set introduces no new sandbox bypass, no secret leakage in config fields, no unvalidated interpreter-script injection, and no missing `cmd.Dir` or inherited-terminal-fd spawning bugs. The most consequential security-related change (`sandboxSensitiveTargets` compound-command scoping) is a *fix*, not a regression.
- **HIGH — Confirmed: `sandbox` mode is now persistable (`HandleSetPermissionModeConfig` in `internal/server/handler_config.go:1370+`).** Before this change, sandbox was session-scoped only (`PUT /api/permissions/mode`); now `PUT /api/config/ocode/permissions-mode` writes `sandbox` to disk (`SavePermissionModeSwitch`). Sandbox remains **write-integrity only** — reads, network egress, and exec stay open; it does NOT provide confidentiality. Any document or UI suggesting sandbox protects secrets must be corrected before users rely on persistence. This is a design/policy blocker, not a code crash blocker.

---

## 2. 🧱 Design & Architecture

**HIGH:**
- **HIGH — Dependency-output injection in `task_dag` relies on truncation, not sanitization (`internal/agent/task_dag.go:740-839`).** `buildPredecessorContext` takes each predecessor's final result, applies `TruncateToolResult` (bounded by disk-cached truncation), labels it with the predecessor id, and injects it into the child's `Context` parameter (`dagContextFrom`). There is no content escaping, no malicious-string filtering, and no structural sanitization — a malicious subagent can craft predecessor text that influences the child's LLM decision when coalesced into the context message. Impact: a poisoned predecessor can steer a child subagent's behavior (e.g., instruct it to ignore safety rules embedded in its system prompt). Fix: document this explicitly as an intentional trust-boundary (predecessor output is treated as trusted work product within the same agent session), and consider adding a brief `[ocode:predecessor-output]` delimiter wrapper in `dagContextFrom` so the model clearly separates injected work from its own system instructions (already partially present via the `Predecessor task results:` header, but stronger structural isolation would reduce influence risk).

**MEDIUM:**
- **MEDIUM — Subagent timeout (`wait_timeout_seconds`) does not cancel the underlying goroutine (`internal/agent/subagent.go:932-947`).** `runSyncDispatchWithTimeout` launches `runSyncDispatch` inside `crashguard.Go` and selects on `doneCh` vs `time.After(timeout)`. When timeout fires, the function returns `run.ID` but the goroutine continues to completion, holding a concurrency slot (`max_concurrent_agents=2`, typically 2). Repeated timeouts can starve the agent pool. The `crashguard.Go` protects against panic crashes but does not release the slot. Fix: document that timeout is a caller convenience, not a cancellation mechanism; consider adding a cancellation context that propagates into `runSyncDispatch` when the parent call abandons (though this conflicts with the intended "keep running in background" design — confirm intentionally).
- **MEDIUM — Sandbox (`PermissionModeSandbox`) is now a valid persisted default (`internal/config/ocodeconfig.go:1370+`, `internal/server/handler_config.go:1370+`).** Before: session-only. After: `HandleSetPermissionModeConfig` allows `sandbox` in the config file. The comments correctly document that sandbox is integrity-only (`AGENTS.md` confirms: "write-integrity confinement only"; reads/network/exec open). Any user expecting sandbox to prevent secret exfiltration will be wrong. Fix: add an explicit warning message to the `HandleSetPermissionModeConfig` response and to any UI that surfaces the persisted mode, stating "sandbox does not restrict reads, network egress, or command execution — it only limits OS-level writes."
- **MEDIUM — `sandboxSensitiveTargets` compound-command parsing now uses `parseShellCommandLine` (`internal/agent/permissions.go:2708-2741`).** The fix correctly scopes `extractBashCommandPaths` to each `;`/`|`-separated fragment instead of applying the first fragment's prefix rules to unrelated words. This is a security improvement. However, the parse-fallback path (`err != nil`) falls back to `splitShellFields(command)` and passes only `fields[0]` to `extractBashCommandPaths` (`permissions.go:2710-2719`) — this is conservative (won't miss paths in malformed commands) and safe, but it may miss paths in malformed compound lines. This is acceptable because malformed commands are routed to `Ask` anyway; the fallback just ensures some path coverage rather than none.

**LOW:**
- **LOW — `permissions.go`: `isDangerousGitConfigKey` (`permissions.go:460-488`) does not cover newer git config vectors that enable arbitrary file read/execution (e.g., `core.sshCommand` with arbitrary file paths is covered, but `credential.useHttpPath`, `http.sslCAInfo` pointing to attacker-controlled cert files, or `protocol.ext.allow` variants with non-standard names are partially covered). The existing list is defensive and sufficient for common attack patterns; extending it is low urgency.

---

## 3. ✨ Readability & Maintainability

**MEDIUM:**
- **MEDIUM — `internal/agent/subagent.go`: `runSyncDispatchWithTimeout` mixes timeout logic with background-run lifecycle logic.** The timeout path (`WaitTimeoutSeconds > 0`) calls `runSyncDispatchWithTimeout` and prepends `fallbackWarning` (`subagent.go:613-622`). The function name and docstring (`subagent.go:925-931`) explain the design, but the dual path (`runSyncDispatch` vs `runSyncDispatchWithTimeout`) increases cognitive load. Keep as-is (design is sound) but consider extracting the timeout wrapper into its own named method to make the control flow explicit in `Execute`.
- **MEDIUM — `internal/agent/task_dag.go`: `buildPredecessorContext` (`task_dag.go:740-776`) constructs predecessor output without marking it as untrusted.** The label `Predecessor task results:` is present, but the injected text is treated as user-level content by the model. Adding an explicit structural delimiter (e.g., wrapping each predecessor result in a blockquote or code-fence-like marker) would improve readability and reduce the risk of the model interpreting injected output as direct instructions. This is a readability/refinement issue, not a security hole, because the trust model already treats predecessor output as trusted session work.

**LOW:**
- **LOW — `permissions.go`: compound command parsing (`parseShellCommandLine`) handles `&&`, `||`, `(`, `)`, `;`, `|`, and `$(...)` substitution (`permissions.go:4481-4537`).** The code has extensive comments explaining why `bodyIntroKeywords` (`if`, `while`, `until`, etc.) and `!` are dropped. This is well-documented and maintainable.

---

## 4. ⚡ Performance & Scalability

**MEDIUM:**
- **MEDIUM — Timeout goroutine retention under `runSyncDispatchWithTimeout` (`subagent.go:932-947`).** As noted above, the goroutine continues past timeout. Under sustained timeout usage (e.g., a script that launches many short-circuit subagents with `wait_timeout_seconds`), the concurrency limiter (`max_concurrent_agents`) can become a bottleneck. This is a scalability concern, not a crash, because the goroutine eventually completes. Monitor in production; consider cancellation propagation if it becomes a problem.
- **MEDIUM — `sandboxSensitiveTargets` now parses compound commands with `parseShellCommandLine`, which tokenizes the full command string (`permissions.go:2708`).** For very long compound commands (e.g., multi-line pipelines), this introduces a small parsing overhead compared to the previous flat split. Impact is negligible for normal bash commands; only very long lines could show latency. Acceptable.

**LOW:**
- **LOW — `internal/agent/task_dag.go`: `buildPredecessorContext` constructs a new `strings.Builder` for every node with dependencies (`task_dag.go:744`).** This is fine for small graphs; for very large batches (many dependencies), repeated string concatenation could be optimized with a pre-sized buffer. Not a security issue.

---

## 5. 💧 Resource & Memory Leaks

**MEDIUM:**
- **MEDIUM — Subagent timeout does not release the concurrency slot (`subagent.go:932-947`).** The goroutine continues to hold its `AgentRun` and its `releaseActive` callback until `runSyncDispatch` completes. If a subagent hangs (e.g., blocked on an external LLM server), the timeout only changes the caller's return value; the slot is never released until the hang resolves. With `max_concurrent_agents=2`, a single hung subagent that is repeatedly timed out can block all future subagent launches. This is a resource/starvation leak. Confirmed by code inspection: `releaseActive` is only called inside `runSyncDispatch`, not in the timeout branch.
- **MEDIUM — Browser surface retention (`App.browser.test.tsx` update confirms design).** The test (`App.browser.test.tsx:148-162`) verifies that `BrowserPanel` surfaces remain mounted with `data-active="false"` when tabs are switched. Each retained surface keeps its `BrowserPanel`, message listener, CDP socket (`useCdpSocket`), and Chrome target alive (`internal/browse/cdp/manager.go`). Combined with the new `HTREnabled` and `HTRExtensionDir` fields (`launch.go:83-176`), an unbounded accumulation of inactive tabs with preloaded extensions consumes memory and keeps WebSockets open. The existing design preserves state intentionally (`AGENTS.md` notes this), but there is still no live-surface limit or hibernation policy (`memory.md` also flags this). Confirmed: resource retention is by design, not a leak, but unbounded growth remains a risk.

**LOW:**
- **LOW — `local_model_limiter.go`: `crashguard.Go` replaces raw `go func()` for the touch loop (`local_model_limiter.go:100`).** This improves panic recovery but does not change the resource lifecycle. The `ticker.Stop()` is deferred correctly (`local_model_limiter.go:111`), and the `Close()` method releases the advisory lock (`local_model_limiter.go:122-130`). No leak introduced.
- **LOW — `internal/agent/task_contract.go`: `crashguard.Go` replaces raw `go` (`task_contract.go:89`).** Same improvement; no resource impact.

---

## 6. 🧪 Testing Gaps

**MEDIUM:**
- **MEDIUM — No test verifies that `wait_timeout_seconds` does not starve the concurrency pool.** The `subagent.go` change introduces `runSyncDispatchWithTimeout`, but `subagent` tests don't cover concurrent timeout behavior under a limited `max_concurrent_agents`. Add a test that launches two subagents with short timeouts while limiting `max_concurrent_agents=1`, verifying that the second does not deadlock.
- **MEDIUM — `permissions_test.go`: `TestSandboxSensitiveTargets_PerFragmentScoping` (`permissions_test.go:2595-2620`) validates the fix correctly, but does not cover malformed compound lines with parse errors (the `parseShellCommandLine` error path at `permissions.go:2708-2719`).** Add a test with unbalanced quotes (`"cd /tmp; grep "`) that verifies the fallback `splitShellFields` path doesn't mis-scope paths.
- **MEDIUM — No cross-project browser lifecycle regression test (`App.browser.test.tsx` gap noted in memory).** The existing test verifies DOM identity (`data-key`) but does not verify WebSocket identity, grant minting, or `nav` message preservation when a tab is deactivated and reactivated across project switches. Confirmed: this is a known gap from the memory review (`memory.md: Browser Review`).

**LOW:**
- **LOW — `permissions_test.go`: `TestPermissions_AdvancedBashFeatures` (`permissions_test.go:1267`) adds a compound `find/sort/printf/git` pipeline (`permissions_test.go:1270-1280`).** The test verifies `PermissionAllow` for the full compound line. This validates the per-fragment allowlist logic. Good coverage for the fixed behavior.
- **LOW — `internal/browse/cdpsocket_test.go`: `fakeManager` stub (`cdpsocket_test.go:225`) adds `SetScreencastQuality`.** This is sufficient for the interface contract; no security gap.
- **LOW — `web/src/components/Settings/PermissionsForm.sandbox.test.tsx`: new file (`git status` shows it as added).** Confirmed present; covers sandbox mode toggling in the web settings form. No gap.

---

## 7. ✅ What's Done Well

- `sandboxSensitiveTargets` compound command fix (`permissions.go:2708-2741`) is a real security improvement: it prevents the previous bug where a flat `splitShellFields` applied the first subcommand's prefix rules to unrelated fragments, which could cause false negatives (missing sensitive paths) or false positives (incorrect path scoping). The fix uses `parseShellCommandLine` to split by operators (`;`, `|`, `&&`, `||`, `(`, `)`) and scopes `extractBashCommandPaths` per fragment. This directly addresses the compound-command handling concern raised in the review prompt.
- `permissions_test.go` includes targeted regression tests (`TestSandboxSensitiveTargets_PerFragmentScoping`, compound pipeline test) that document the intended behavior and guard against regression.
- `crashguard.Go` is used consistently for goroutines that were previously raw (`subagent.go`, `task_dag.go`, `local_model_limiter.go`, `task_contract.go`). This improves process stability and terminal-state recovery on panic.
- Config validation is strict: `validHTRNativeHostName` requires `com.ocode.` namespace (`ocodeconfig.go:342`); `NormalizeScreencastQuality` clamps 1-100 (`ocodeconfig.go:363-372`); `NormalizeFakeAgent` restricts to preset harness identities (`ocodeconfig.go:2516-2537`). The `HandleSetBrowserConfig` endpoint validates port ranges (`handler_config.go:1675-1677`) and rejects invalid native host names (`ocodeconfig.go:3344`).
- The `HandleSetPermissionModeConfig` endpoint clearly separates session-scoped (`PUT /api/permissions/mode`) from persisted (`PUT /api/config/ocode/permissions-mode`) mode changes (`handler_permissions.go:202`, `handler_config.go:1370+`). This avoids ambiguity about which mode is durable.
- The `chromeArgsFor` function (`launch.go:83-176`) validates extension directories (`hasExtensionDir`) and checks branded Chrome compatibility (`isBrandedChrome`) before applying `--load-extension`. The `launchChromeWithOptions` function captures the extension, socket path, and native host name separately and sets `cmd.Env` accordingly, avoiding unvalidated environment injection.

---

## 8. 🔧 Suggested Improvements

- **MEDIUM (security/policy):** Update any UI or documentation that mentions sandbox mode to include an explicit notice: "Sandbox mode limits OS-level writes; reads, network egress, and command execution remain open. It does not protect secrets." This is especially important now that sandbox is persistable (`HandleSetPermissionModeConfig`).
- **MEDIUM (security/resource):** Document the timeout behavior explicitly in the agent system prompt or `subagent` docs: `wait_timeout_seconds` returns control to the caller but does not stop the subagent; the concurrency slot is held until the subagent finishes. If resource exhaustion is observed, consider propagating `ctx` cancellation into `runSyncDispatch` when the timeout fires.
- **MEDIUM (security/design):** For `buildPredecessorContext`, add a structural delimiter (e.g., `<<<PREDECESSOR OUTPUT [id]>>>`) before each predecessor's result, so the model clearly separates injected work from its instructions. Currently the `Predecessor task results:` header provides some separation; stronger structural markers would further reduce influence risk.
- **LOW (security/test):** Add a `permissions_test.go` case for the malformed compound command fallback (`parseShellCommandLine` error path) to verify `splitShellFields` does not over-apply path rules.
- **LOW (security/test):** Add a `subagent` concurrency test that verifies timeout behavior under `max_concurrent_agents=1` or `2`, confirming that the slot is not prematurely released and that deadlock does not occur.
- **LOW (readability):** In `subagent.go`, split the `runSyncDispatchWithTimeout` body into two clearly named private methods (`runSyncWithTimeout` and `runSyncWithoutTimeout`) so the `Execute` method's control flow (`subagent.go:607-627`) is easier to trace.

---

## Cross-Cutting Notes (shared bus)

1. **Sandbox persistence policy change.** `sandbox` is now a valid persisted default (`internal/config/ocodeconfig.go:1370+`, `internal/server/handler_config.go:1370+`). It remains integrity-only (no confidentiality guarantee). Any downstream code or documentation that implies sandbox protects secrets must be corrected.
2. **Subagent timeout resource retention.** `wait_timeout_seconds` (`subagent.go:932-947`) does not cancel the underlying `runSyncDispatch`. The goroutine continues, consuming a concurrency slot (`max_concurrent_agents`). Monitor for slot starvation; consider cancellation propagation if needed.
3. **Dependency-output injection is trusted-work-product.** `task_dag.go:740-839` injects predecessor output (truncated via `TruncateToolResult`) directly into the child's `Context`. There is no sanitization beyond truncation. This is an intentional design (same-session trust boundary) but must be documented: malicious subagent output can influence child behavior.
4. **Compound command scoping fix validated.** `permissions.go:2708-2741` replaces flat `splitShellFields` with per-fragment `parseShellCommandLine` parsing. The regression tests (`permissions_test.go:2595-2620`) confirm correct scoping. This directly addresses the compound-command security concern from the review prompt.
5. **No new sandbox bypass (PATH shadowing, writable-root defeat, symlink escape).** The changed code does not modify sandbox backend discovery (`sandbox.Supported` remains unchanged), plugin removal validation (`filepath.EvalSymlinks` unchanged), or writable-root canonicalization (`pathscope` unchanged). The only sandbox-related change is the `sandboxSensitiveTargets` fix, which improves accuracy.
6. **No new interpreter-script bypass.** The `bashSubcommandAllow` list (`permissions.go:195-330`) and interpreter detection (`python`, `node`, `bun`, `npx`, `pnpm`, `yarn`, `vp`) are unchanged in this diff. The compound command fix does not alter interpreter-form handling.
7. **No secret leakage in new config fields.** `FakeAgent`, `HTRExtensionPath`, `HTRCliPath`, `HTRPort`, `HTRSocketPath`, `HTRNativeHostName`, `ScreencastQuality`, `TTS` (`Engine`, `Voice`, `Mode`) contain no secret data. Validation functions (`validHTRNativeHostName`, `NormalizeScreencastQuality`, `NormalizeFakeAgent`) restrict values appropriately. The `SyncURL` and `BackendURL` fields (existing) are preserved without new exposure.
8. **No unsafe subprocess spawning regression.** `launchChromeWithOptions` (`launch.go:177`) sets `cmd.Env` explicitly and uses `exec.Command(chromePath, ...)`. The `cmd.Dir` is not set to a specific workdir (it inherits from the process), but the process supervisor (`tool.ProcessSupervisor`) manages the lifecycle. No new missing `cmd.Dir` or inherited-terminal-fd issues were introduced.
