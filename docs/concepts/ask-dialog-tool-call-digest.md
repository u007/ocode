---
type: Concept
title: Ask-dialog "Explain this call" digest
description: Explain-this-call digest for a pending permission ask: a bounded, read-only tool loop that writes Markdown for a human, never a judge. Tools, budgets, coordination, surfaces.
resource: internal/agent/tool_digest.go
tags:
  - tui
  - web-ui
  - permissions
  - agent
  - tool-digest
timestamp: 2026-11-25T17:23:00Z
---
# Ask-dialog "Explain this call" digest

When a permission ask shows raw arguments (`cat .env`, `git push origin main --force`), the approver has to infer the effect from syntax. The **tool-call digest** explains the pending call in Markdown so the user can approve or deny with some idea of what it touches.

It is a bounded tool loop, not a single prompt. A small model can page through the exact masked parameters, inspect project files and a fixed set of read-only shell commands, and then answer. The pending call itself is never executed.

## Why it exists

An opaque payload (a heredoc, a base64 string, a downloaded script) offers no safe approximation. A read-only local file read is obvious; a destructive remote mutation is obvious only to the model. The digest is the compromise: a small model explains the call in plain English, labelled "not a safety verdict", and it can check the project before it claims anything about a file the call names.

## Not a judge

- The output is text for a human. It has no vote in the permission decision, and nothing it says can approve a call. The judge is documented in `docs/concepts/auto-permission-judge-eval.md`.
- Decision-only TypeSafe models are refused before any request: `runToolDigestLoop` returns an error when `isDecisionModel` matches (`internal/agent/tool_digest_loop.go:103`).
- The digest path never calls `Agent.Step`, never writes the transcript, and never calls `OnMessage`, `OnDelta`, plugin hooks, the content guard or the ask machinery. Its messages exist only inside the run.

## Entry and isolation

- Entry: `Agent.SummarizeToolCallWithKeyCtx` (`internal/agent/tool_digest.go:107`). `SummarizeToolCallCtx` (`:86`) calls it with an empty request id. If the `tool_digest` prompt is disabled in Settings, the call returns an error before any work (`:114`).
- One snapshot: `snapshotToolDigestState` (`internal/agent/agent.go:330`) copies the work dir, session id, profile, redaction hook and registry, the main client pointer and the config, then releases its locks. The lock order is `opencodeSessionMu` before `clientSnapshotMu` (`agent.go:594-595`). `applySpecModel` (`agent.go:6723`) swaps `a.client` under `clientSnapshotMu`.
- Side client: `toolDigestSideClient` (`tool_digest.go:192`) builds a fresh agent value from snapshot fields only, with a zero thinking budget. It uses the small model when the small-model gate is on, and otherwise a no-thinking clone of the main client. Its clients are bound with `bindSideClient` (`agent.go:388`), which attaches the session's tier-1 redaction hook. It never falls back to the live main client: when neither option exists, the run fails with an explicit error.
- Usage is reported through the snapshot's side-usage callback (`recordToolDigestUsage`, `tool_digest_loop.go:261`).

## Masking

- `toolDigestMaskFunc` (`tool_digest.go:146`) runs the session redaction hook's `ScanText` when the hook is enabled (`:149`), then `maskSecretsForDigest` (`tool_digest.go:251`).
- `maskSecretsForDigest` masks known-format credentials and assignment-style environment variables, unconditionally, whether or not redaction is enabled. It uses the secret detectors only. PII categories are never attached, so the digest's masking is secrets-only by construction.
- Inspection output is masked the same way before it reaches the model (`dispatch`, `tool_digest_loop.go:177`; shell output at `tool_digest_shell.go:1256`).

## Canonical payload

`buildToolDigestPayload` (`internal/agent/tool_digest_params.go:42`):

- The `args` value must be one strict JSON value. `scanToolDigestJSON` (`:160`) rejects duplicate object keys at every depth.
- Strings and keys are masked recursively (`toolDigestMaskValue`, `:210`).
- The canonical object has the keys in the order `tool`, `command`, `args`, `rule`, `scope`, with nested keys sorted.
- `Args` is authoritative over `Command`. The system prompt says so, because the displayed command can omit environment assignments.
- A manifest gives the total byte length, the SHA-256 and each field's byte length (`toolDigestManifest`, `tool_digest_loop.go:460`).

`call_parameters` (`ExecuteCtx`, `tool_digest_loop.go:365`) returns exact windows of at most 8192 bytes (`toolDigestMaxPageBytes`, `tool_digest_params.go:15`), cut at UTF-8 boundaries by `page` (`:101`). There is no cumulative cap on parameter bytes. A page is admitted or refused whole by the context guard: a refused page is not appended, and the refusal names the uninspected byte ranges. The first page is included in the opening message only when the budget admits it (`buildToolDigestInitialMessages`, `tool_digest_loop.go:439`).

## Context guard

`toolDigestContextGuard` (`internal/agent/tool_digest_contract.go:80`) takes the model's context window and output reserve from the model registry. Unknown values fall back to a 100000-token window and an 8192-token reserve (`newToolDigestContextGuard`, `:91`).

`admit` (`:114`) refuses any candidate whose messages plus the reserve exceed the window. The refusal is a `toolDigestContextError` (`:126`) that carries `remaining_context_tokens`, a negative number. The guard never truncates. A model request that does not fit is not sent.

## Loop limits

The run loop is `runToolDigestLoop` (`tool_digest_loop.go:97`). It owns the toolbox, and every registered cleanup runs on success, error and cancellation.

| Limit | Value | Where |
|---|---|---|
| Whole run, queue time included | 80 s | `toolDigestRunTimeout` (`tool_digest_loop.go:24`) |
| One direct model request | 25 s | `toolDigestModelCallTimeout` (`:26`) |
| Model round trips | 6 | `toolDigestModelSteps` (`:28`), step loop at `:116` |
| Inspection calls (read, glob, grep, list, inspect_shell) | 12 | `toolDigestInspectionCalls` (`:30`), `toolDigestInspectionTools` (`:59`) |
| Masked evidence per result | 8 KiB, marked `[evidence truncated]` | `toolDigestEvidenceResultBytes` (`:35`), `capEvidence` (`:194`) |
| Masked evidence per run | 40 KiB | `toolDigestEvidenceTotalBytes` (`:37`) |
| Raw result read before masking | 64 KiB | `toolDigestEvidenceScanBytes` (`:54`) |

`call_parameters` is not an inspection call and does not count toward the 12.

Budget exhaustion is not an error. `dispatch` (`tool_digest_loop.go:153`) returns a tool result that tells the model to answer from the evidence it has. Only two things end a run with an error:

- The 6-step cap. The run fails with `explanation stopped after 6 model steps without a final answer` (`:139`).
- The context guard refusing the next request (`:117`).

The model's final text goes through `cleanToolDigest` (`tool_digest.go:310`). That step trims whitespace, removes one whole-output code fence and a boilerplate first line such as "Explanation:". It never truncates or clamps, so the full Markdown is shown. An empty answer fails the run.

## Tools

All six tools are read-only. The model sees only the tools registered on the run's toolbox.

1. **`call_parameters`**: the paging tool described above.
2. **`read`, `glob`, `grep`, `list`**: wrappers around the normal tools (`newToolDigestFileTools`, `internal/agent/tool_digest_files.go:248`).
   - Every path goes through `validateProjectPath` (`:58`). It resolves symlinks, compares against the canonical project root and refuses anything outside it.
   - `sensitivePathFilter` (`:129`) blocks secret-looking paths. It uses `redact.IsSensitiveFile`, `isSecretMaterialPath`, `isRepoMetadataPath` and `isLoginStatePath` through `sensitiveNamePath` (`:119`). The glob list is `toolDigestSensitiveIgnoreGlobs` (`:22`).
   - Grep passes the same excludes to ripgrep through `tool.WithSearchPolicy` (`tool_digest_files.go:234`, `internal/tool/search_policy.go:22`). They become `--iglob` arguments at `internal/tool/rgrep.go:276`.
   - The shared search tools honour context cancellation. rgrep runs in its own process group and kills it on cancel (`internal/tool/rgrep.go:305`, `killProcessGroup` at `internal/tool/proc_unix.go:46`).
3. **`inspect_shell`** (`newInspectShellTool`, `internal/agent/tool_digest_shell.go:974`):
   - Argv only, with no shell. The program allowlist is `inspectShellPrograms` (`:40`): `pwd`, `ls`, `stat`, `file`, `readlink`, `realpath`, `wc`, `head`, `tail`, the md5 and sha family, `find`, `go` and `git`. Programs resolve only from fixed directories (`inspectShellBinaryDirs`, `:92`).
   - The environment is built from scratch (`inspectShellEnv`, `:108`): a fixed `PATH`, `LC_ALL=C`, `GIT_CONFIG_GLOBAL=/dev/null`, `GIT_CONFIG_NOSYSTEM=1`, `GIT_OPTIONAL_LOCKS=0`, `GIT_TERMINAL_PROMPT=0` and `GOTOOLCHAIN=local`. `HOME` is passed only so `go env` resolves its defaults.
   - A mandatory no-write sandbox wraps every process. The tool is not registered when no sandbox backend is available, or when the backend refuses to wrap the probe (`:997`). It is disabled on Windows, which has no binary directories. On Linux the Landlock wrapper accepts only `<shell> -c <command>` argv, so the tool is effectively off there (see Known limits).
   - Each process runs under a supervisor with a 10 s deadline (`inspectShellDeadline`, `:28`). Output is capped at 8 KiB; on overflow the process is terminated and the result says so (`:1213`).
   - Git runs only with the fixed prefix in `inspectShellGitPrefix` (`:59`): `--no-pager -c core.fsmonitor=false -c core.pager=cat -c diff.external= -c core.hooksPath=/dev/null -c core.attributesFile=/dev/null --attr-source=<SHA-1 empty tree>`. The empty tree is `inspectShellEmptyTree` (`:53`). Diff forms add `--no-ext-diff --no-textconv` (`:573`).
   - Why the extra attribute flags exist: on Git 2.54 the `.gitattributes` clean and process filters still ran for status and metadata diff forms under the first prefix. The macOS sandbox denies writes but does not deny exec, so a filter could still run. `--attr-source` and `core.attributesFile` close that path.
   - A repository with a repository-local attributes file (`.git/info/attributes`) is refused. The check is `inspectRepoLocalAttributesPresent` (`:1094`), and the refusal is at `:1082`.
   - SHA-256 repositories fail closed: `--attr-source` names the SHA-1 empty tree, and the code comment at `:51` relies on Git refusing SHA-256 repositories.
   - Only metadata forms are allowed (status, name-only, stat, numstat, rev-parse, ls-files). Patch and content forms are not.

## Coordination

`toolDigestCoord` (`internal/agent/tool_digest_coordinator.go:18`) is one process-wide coordinator. The TUI and the web server share it, so its limits apply across surfaces.

- **Supersession.** The flight key is `toolDigestKey` (`internal/agent/tool_digest_contract.go:139`). With a request id it keys on the id, so a newer request for the same ask replaces the older run. Without one it keys on the masked payload hash. The replaced run is cancelled and its caller gets `ErrToolDigestSuperseded` (`tool_digest_coordinator.go:14`). That is benign: no error is shown. Runs for different asks are independent.
- **Cache.** Only successful, non-superseded results are cached (`storeLocked`, `:107`). The key is `toolDigestCacheKey` (`tool_digest_contract.go:152`): owner, request id and masked payload hash together. A provider that reuses a tool-call id for a different call therefore misses the cache. Entries last 2 minutes (`toolDigestCacheTTL`, `tool_digest_loop.go:41`) in an LRU of 128 (`toolDigestCacheEntries`, `:43`). A cache hit returns at once and takes no semaphore slot.
- **Concurrency.** Three runs at most (`toolDigestMaxConcurrent`, `:39`). The semaphore is acquired in `execute` (`tool_digest_coordinator.go:123`, slot at `:125`). The 80 s run deadline starts in `run` (`:79`), before the wait, so queue time spends the same budget.
- **Invalidation.** `InvalidateToolDigests(owner)` (`internal/agent/tool_digest.go:136`) cancels the owner's in-flight runs and drops its cache entries (`invalidate`, `tool_digest_coordinator.go:138`). The owner is the agent's `OpenCodeSessionID()`. The server calls it through `invalidateToolDigests` (`internal/server/handler_tool_digest.go:170`) when an ask resolves (`internal/server/handler_permissions_resolve.go:336` and `:595`), when a session closes (`internal/server/handler_close.go:70`) and when a session is evicted (`internal/server/handler.go:930`).
- The cache is in memory. A process restart drops it.

## Server endpoint

`POST /api/sessions/{id}/tool-call-digest` is registered at `internal/server/server.go:584`. The handler is `HandleToolCallDigest` (`internal/server/handler_tool_digest.go:41`). It is synchronous. The response is `{summary, model}`.

Request body: `{request_id?, tool?, command?, args?, rule?, scope?}`.

- The body is capped at 1 MiB (`toolDigestBodyMaxBytes`, `:16`). An oversize body gets 413 (`:54`). It is never truncated.
- When `request_id` names a pending permission ask in this session, the parameters come from that ask (`pendingAskDigestInput`, `:126`). Posted fields are ignored. The lookup scans the whole trailing tool round and uses `TryLock`, so it never waits for a running turn.
- Without a usable pending ask, a posted fallback is accepted when it has a tool plus a command or args. A bare tool name with neither is a 400.
- A `request_id` that names no live ask, with no usable fallback, gets 404 `ask_unavailable` (`:75`). Nothing starts.
- A run superseded by a newer request for the same ask gets 409 `explanation_superseded` (`:105`).
- A provider or run failure gets 502 (`:111`).
- The request context is passed to the run (`:103`). Closing the dialog or navigating away cancels the provider call.
- There is no busy-session conflict. The run does not take `agentSession.mu`, so an explanation can run while a turn is active.

## Surfaces

**TUI** (`internal/tui/model.go`):

- The `[e] explain` hint is `permDigestActionLabel` (`:18251`). It is offered by `permDigestActionAvailable` (`:18256`), which hides it while the always-allow confirmation is showing or no permission call is pending.
- `startPermDigest` (`:18290`) passes the full input and the pending tool-call id as the request id (`callID`, `:18299`). It takes the run's cancel function, and `cancelPermDigest` (`:18281`) cancels the previous run before the new one starts (`:18303`).
- `permDigestCancel` (`:1691`) is cancelled when the dialog closes, when the ask changes, and on `resetPermDigest` (`:18270`). A superseded reply is silent (`:5254`). `permDigestSeq` (`:1690`) drops replies from an older ask or click.
- The reply is cleaned with `stripANSI`, then `sanitizeForTUI` (`:5267`). `renderPermDigest` (`:18326`) renders it with `renderMarkdown` and `wrapView` (`:18345`), inside the permission viewport, so a long explanation scrolls. The header reads "AI summary", the model id, and "not a safety verdict" (`:18337`).

**Web** (`web/src/components/Chat/PermissionDialog.tsx`):

- The control reads "Explain this tool call", or "Explain again" after a result (`:779-780`). A live ask sends `{request_id, tool}` (`:401`).
- On 404 `ask_unavailable` the dialog retries once with the full parameters, when it has them (`:408-414`).
- A 409 `explanation_superseded` is benign: no banner, and any digest already shown is kept (`:429`).
- A 413 shows "This call's parameters are too large to send for an explanation." (`explainFailureMessage`, `:83-84`).
- The answer renders with react-markdown and remark-gfm, with raw HTML skipped (`DIGEST_REMARK_PLUGINS` at `:50`, `DIGEST_COMPONENTS` at `:51`, `ReactMarkdown` at `:799-802`). Images render nothing (`:53`). Links go through `MarkdownLink`, which sends http(s) URLs through the URL confirmation chooser. The header is at `:793`.
- The panel scrolls (max height 16 rem). Closing the dialog or changing the ask aborts the request (`digestAbort`, `:306`, `:337`, `:505`).

## Known limits

Recorded in `TODO.md` under "permission explainer `inspect_shell` limits":

- `inspect_shell` is off on Linux, because the Landlock wrapper accepts only `<shell> -c <command>` argv. The probe at `tool_digest_shell.go:997` disables the tool there. It is also off on Windows.
- Timeout and cancel kill is SIGTERM to the process group, followed by a SIGKILL to the group leader only after 1 s. The supervisor does not escalate to a group SIGKILL, so a child that ignores SIGTERM can outlive the leader.
