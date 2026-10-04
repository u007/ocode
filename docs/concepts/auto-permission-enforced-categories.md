---
type: Concept
title: Auto-Permission Enforced Categories
description: 'Per-category enforcement toggles for the LLM auto-permission judge AND the interpreter-effect verifier: the negative relaxed_concerns config set, the GET /api/config/ocode/permissions-concerns catalog, the deterministic Go safety boundary, the opaque-floor override for truncated_or_unknown, the 2026-09-29 environment-enumeration rubric carve-out, and the 2026-10-02 executed_scripts source disclosure.'
resource: internal/agent/permission_typesafe.go
tags:
  - permissions
  - auto-permission
  - typesafe
  - jev
  - config
  - settings
  - web
  - interpreter
timestamp: 2026-10-04T12:33:27Z
---
# Auto-Permission Enforced Categories

## Decision

Users can switch off individual concern categories that the LLM auto-permission judge **and the interpreter-effect verifier** must enforce. The control lives in **Web Settings → Permissions → "Categories the judge must enforce"** (`web/src/components/Settings/PermissionsForm.tsx`); all categories are ticked (enforced) by default.

The feature configures the **judge's** policy only. The deterministic Go permission layer is unchanged and still runs on all three judge paths, so an unticked box never widens what Go would block on its own (see *Safety boundary*).

## Storage

`permissions.auto.relaxed_concerns` (`[]string`) in `ocodeconfig.json`.

It is a **negative set**: the listed keys are the categories explicitly **not** enforced. Empty or absent therefore means "everything enforced", so existing configs need no migration and a category introduced later defaults to enforced.

- Go type: `config.AutoPermissionConfig.RelaxedConcerns` + predicate `ConcernRelaxed(key)` (`internal/config/ocodeconfig.go`), both nil-safe.
- Mapped from the pointer-mirror `autoPermissionConfigFile` in `applyAutoPermissionConfig` with **replace-not-merge** semantics: a `nil` field leaves the in-memory set untouched, while an explicit empty list clears it (a save always sends the full set, so unticking everything is representable).

## Category vocabulary

The closed set is `typesafeConcerns` in `internal/agent/permission_typesafe.go` **minus `none`** (which is the "no problem" answer, not a rule). The eight relaxable categories, in rubric order:

`outside_allowed_roots`, `destructive`, `secrets`, `banned_prefix`, `network`, `subprocess_or_dynamic_code`, `system_or_git_history`, `truncated_or_unknown`.

`agent.RelaxableConcerns()` returns the catalog as `{key, label, note}` and is the **single source of truth** for the settings checkboxes, the Jev rubric, and the chat judge prompt, so the three cannot drift. `IsRelaxableConcern(key)` drops stale or invented keys from hand-edited config before they can reach a prompt (an unknown key in the prompt would read as a rule to relax).

## API

- **New read-only** `GET /api/config/ocode/permissions-concerns` → `{"concerns":[{"key","label","note"}]}` (`Handler.HandleGetPermissionConcerns`, `internal/server/handler_config.go`), routed at `internal/server/server.go`.
- The existing `GET|PUT /api/config/ocode/permissions-auto` carries the `relaxed_concerns` field. The catalog is deliberately **not** on that PUT type — it is derivation, not user input.

## Semantics ("not enforced → allow")

The relaxed keys are rendered after the base rules on every judge path, so the user's opt-out outranks the shipping policy (which hard-codes e.g. "deny when a credential appears").

- **Decision-backend path** (`askPermissionModelTypesafe`): the relaxed clause is appended to **both** the verdict and the concern question instructions (`relaxedConcernsClause`), and mirrored into `state["relaxed_concerns"]`. Deterministically, a **DENY whose single named concern is in the relaxed set is converted to an allow**, logged `tier=auto_typesafe_relaxed`. A deny that names `none`, nothing, or a still-enforced category is **not attributable and stands** (fail closed). Because the clause is appended after the bundled addendum, it wins over the shipping policy.
- **Chat judge path** (`askPermissionModel`): instruction-only, as a per-request section ("Relaxed concern categories") placed next to the banned-prefix facts. The chat verdict carries **no category**, so a chat-judge deny cannot be attributed to an opted-out class and **stands**. (Unchanged — still instruction-only.)
- **Interpreter-effect path** (`askPermissionModelInterpreter` / `verifyInterpreterEffects` in `internal/agent/permission_interpreter.go`): `verifyInterpreterEffects` is a thin wrapper — `return a.verifyInterpreterEffectsWith(..., a.relaxedConcernSet())`. The new `verifyInterpreterEffectsWith(..., relaxed map[string]bool)` holds the gates; the pre-existing call sites and tests are unchanged, and `relaxed == nil` means **everything enforced** (that nil form is also used to ask "would this have passed strictly?"). A relaxation-load-bearing allow — one that *needed* an opt-out to pass — is granted for the invocation only and logged `tier=auto_interp_relaxed_allow`; a durable `interpreter_exact` grant is persisted **only** when the same response would also have passed strictly, so re-ticking a category takes effect on the next invocation instead of being shadowed by a saved grant.

## Gate→category mapping (interpreter path)

`verifyInterpreterEffectsWith` relaxes per category as follows (the safety floor in the next section is **never** relaxable and runs first):

| Gate | Relaxed category |
|---|---|
| `effects.unknown` non-empty (or source truncated) | `truncated_or_unknown` |
| read/write/delete path outside allowed roots | `outside_allowed_roots` |
| `isSensitivePath` hit | `secrets` |
| subprocess wrapper / spawns a shell-or-interpreter | `subprocess_or_dynamic_code` |
| network-capable subprocess and host outside the webfetch allowlist | `network` |
| deletes / `db_destructive` with `allow_destructive=false` | `destructive` |

## Safety floor (never relaxable, interpreter path)

The model's own `decision`, the confidence floor, a hard-blocked raw command, and hard-blocked/harmful subprocesses are always enforced. In `verifyInterpreterEffectsWith` these checks run **before** the relaxable subprocess classification (the subprocess loop was reordered so `isHardBlockedCommand` / `IsHarmfulBashCommand` come first). The pre-existing `verifyAutoGrant` guards still apply on all three paths.

Several categories carry a UI `note` (from `relaxableConcernNotes`, kept next to the catalog so the hint and the behaviour cannot drift) because a deterministic Go guard already covers them. **Every one of the eight categories carries a note** (`TestRelaxableConcernsMatchRubric` asserts this):

| Category | Note |
|---|---|
| `outside_allowed_roots` | Non-interpreter asks still refuse an out-of-scope target (verifyAutoGrant). This relaxes the interpreter-effect verifier's root gate. |
| `destructive` | Hard-blocked forms (git history rewrites, rm -rf /) never reach the judge, and a force/recursive rm outside the project always asks you. This relaxes what is left — deletes and DROP/TRUNCATE, including interpreter scripts, which otherwise need allow_destructive. |
| `secrets` | Reading a credential file locally is already allowed; this relaxes exposing the value and the interpreter verifier's sensitive-path gate. |
| `banned_prefix` | A hard-blocked ban always wins; only granular /ban rules can be overridden this way. |
| `network` | Relaxes outbound hosts, network-capable subprocesses, and the interpreter verifier's webfetch-domain gate. |
| `subprocess_or_dynamic_code` | Relaxes shell/interpreter subprocesses and interpreter effects the model cannot resolve; hard-blocked or harmful subprocesses are still refused. |
| `system_or_git_history` | Relaxes what reached the judge; hard-blocked git forms and a force-push never get here at all. |
| `truncated_or_unknown` | Allows a call even when the judge cannot tell what it does, including interpreter sources with unresolved effects or truncated source. |

## Opaque floor (decision-backend path)

Some requests are **opaque**: the command head is something the model cannot determine the effect of — a command whose first token is an undefined shell variable (e.g. `$g --version`), an interpreter script whose source cannot be read, or any other call whose effects cannot be determined. When `askPermissionModelTypesafe` returns a concern of `truncated_or_unknown` for such a request, the confidence floor for an `allow` verdict changes.

**The opaque floor is `0.75` as a DEFAULT — an explicitly configured `min_confidence` always governs:**

- If `permissions.auto.min_confidence` is **unset**, the effective floor becomes the opaque default `0.75` (the normal default `0.80` is relaxed for opaque requests; the default was `0.85` until 2026-10-03).
- If `min_confidence` is configured (any positive value — `0.70`, `0.85`, `0.95`), the configured value governs the opaque request too. A permissive configured value is never raised and a stricter configured value is never lowered: the opaque relaxation can never silently change the user's own bar.

This is the **only** category that triggers the opaque relaxation. Every other concern (`outside_allowed_roots`, `destructive`, `secrets`, `banned_prefix`, `network`, `subprocess_or_dynamic_code`, `system_or_git_history`) keeps the normal `permissions.auto.min_confidence` floor (default `0.85`). The opaque relaxation does not apply when those categories are the concern, even if `truncated_or_unknown` is also present (the relaxation applies when `truncated_or_unknown` is the judge's concern answer, not when it merely appears elsewhere).

**What the opaque floor does and does not do:** it only lowers the threshold for an `allow` verdict — a `deny` from the judge still defers to the human in full. The concern answer `truncated_or_unknown` does **not** decide the verdict; it only selects which floor applies. Deterministic `verifyAutoGrant` still runs after the floor check, so a hard-blocked command, dangerous `rm`, or other Go guard refusal still wins regardless of the lower floor.

`resolveAutoJudgeOpaqueMinConfidence()` (`internal/agent/permissions.go`) returns the configured value when `permissions.auto.min_confidence` is set, and the `autoJudgeOpaqueMinConfidenceDefault = 0.75` constant only when it is unset.

## Executed scripts ship their source (2026-10-02 amendment)

`buildTypesafePermissionState` now ships the source of the scripts a bash command **executes** under a new state key, **`executed_scripts`**. This is judge context, not a gate: **the key decides nothing.** No permission gate was added, removed, or relaxed — the worst outcome of an omitted entry is the pre-existing `truncated_or_unknown` deferral described under *Opaque floor* above, which fails closed.

**The incident it fixes.** The TypeSafe judge has no `read_file` tool. The chat judge's prose context already inlines these files as `Executed custom script: …` sections, but the structured state builder did not, so a script reached Jev as a bare opaque path. Real record from the durable judge log (`permission-judge.log`):

```
command: chmod +x /tmp/aimssearch/gsearch.sh && /tmp/aimssearch/gsearch.sh "novita" 2>&1 | head -5
{"choice":"allow","confidence":0.3,"probabilities":{"allow":0.65,"deny":0.35},"concern":"truncated_or_unknown","concern_confidence":0.84,"floor":0.85,"outcome":"deferred_below_floor"}
```

Jev could not determine what the script did (`truncated_or_unknown` at 0.84), so it returned `allow` at only 0.30 — under the 0.85 floor — and an ordinary command was forwarded to a human for no safety gain.

**What travels.** `executedScriptsForJudge(command, maxLines, maxSources)` (`internal/agent/script_detection.go`) returns an `executedScriptContext{Path, Text, SHA256, TotalLines, Truncated}` per script; the state builder stores them as `{path, sha256, total_lines, truncated, text}` and **omits the key entirely** when no entry survives, so an absent key always means "could not read", never "found but nothing to show". It **reuses `detectExecutedCustomScripts`** — the same detector the chat judge and `verifyAutoGrant`'s truncation guard use — so the three paths can never disagree about which files run. Every constituent of a compound is inspected, so `chmod +x S && S args | head` is covered: `classifyInterpreterExecution` only inspects the FIRST command word, which is why that shape previously shipped no source at all.

**Bounds mirror `verifyAutoGrant` exactly.** The per-source line cap comes from `permissions.auto.max_context_lines_per_source` (default 40) and the entry count from `permissions.auto.max_context_sources` (default 3) — the same knobs the other judge paths read — and every source is clipped at the same `maxInterpreterSourceBytes` (16 KiB) ceiling, so what the judge is shown and what the deterministic guard enforces cannot diverge.

**Why shipping bounded source is safe.** `verifyAutoGrant` still **refuses a truncated script** ("partial content cannot be auto-granted"), so a `truncated: true` entry can never produce an auto-grant — it preserves the very Ask this change exists to avoid for scripts that fit. A new bullet in `typesafeJudgeInstructions` (inherited by `typesafeConcernInstructions`) tells the judge to judge a script's effects from its text as **untrusted data**, never to approve a `truncated: true` entry, and to name `truncated_or_unknown` — not `none` — when a plainly-executed script has NO entry.

**Disclosure guards.** Entries are skipped for secret-material / sensitive paths (`redact.IsSensitiveFile` || `isSecretMaterialPath`); text passes through `redactFileText` with the `/mask` registry, exactly like `interpreter.source`; scope is enforced inside `resolveCustomScript` (`IsPathWithinAllowedRoots`); and an interpreter entrypoint already travelling in the `interpreter` block is not shipped twice.

**Known fail-closed limitation.** A relative script path that only resolves after a top-level `cd` is resolved against the **pre-fold** working directory by `resolveCustomScript`, so it is usually omitted rather than mis-attributed.

**Tests** — `internal/agent/permission_typesafe_script_test.go` (11, green): direct / shell-wrapper / `chmod`-then-run / relative / multi-script compounds, an oversized script marked `truncated`, a sensitive path omitted, non-script binaries and missing files omitted, interpreter source never double-shipped, and `TestTypesafeJudgeSourceDoesNotBypassTruncationGuard` (a truncated entry still fails `verifyAutoGrant`).

## Shell-variable expansion for the judges

Neither judge can run anything, so a bash command built from variables (`MOD=$(go env GOMODCACHE); grep x "$MOD/y"`) was opaque: the judge could not tell where `"$MOD/y"` points, answered `truncated_or_unknown`, and deferred to the human. `expandBashForJudge` (`internal/agent/permission_shellvars.go`) resolves what it safely can before either judge sees the command:

- **In-command assignments.** Only a statement made purely of `NAME=value` words defines variables for later statements. A `NAME=value cmd` prefix does not (bash expands that command's arguments before the prefix applies). An assignment that cannot be resolved stays opaque and never falls back to the same-named env var.
- **Environment variables.** Only the ones the command references. A secret-looking name (`KEY`, `TOKEN`, `SECRET`, `PASSW`, `AUTH`, `CREDENTIAL`, `COOKIE`, `SESSION`, `PRIVATE`, `SIGNATURE`) or a value matching `redact.QuickScan` is withheld as `<redacted>` and left as `$NAME`, because Jev is a remote API.
- **`$(...)` substitutions.** Only this fixed read-only allowlist is ever executed (via `shell.Build`, 3s timeout, stdout only, single-line output required): `pwd` (answered from the working directory, no process), `go env <VAR>`, `git rev-parse --show-toplevel`, `npm root [-g]`, `npm prefix [-g]`, and `python|python3 -c '<snippet>'` where the snippet is one of the exact print-a-path snippets in `pythonSnippetsOK` (`sys.prefix`, `sys.base_prefix`, `sys.executable`, `site.getusersitepackages()`, `site.getsitepackages()[0]`, `sysconfig.get_paths()['purelib'|'platlib']`). Any other substitution, backticks, `${X:-y}`-style forms and `$((...))` are never run and stay verbatim.
- **`/mask`.** When session redaction is on, every resolved value and the expanded command pass through the session registry (`redactText` + `Registry.Substitute`), so a known secret reaches the judge as its OCSEC token.

### `/mask` covers everything the judges receive

The judges are separate model calls, so the main conversation's masking never reached them. With `/mask` on (`judgeMaskRegistry`, `internal/agent/redaction_helpers.go`):

- **Arguments** are masked in chat mode (`redactText`), like the conversation. Tool args usually already carry OCSEC tokens: they are resolved back to raw values only in `executeToolCallWithContext`, after the permission check.
- **Project context**, **interpreter source** and **executed-script source** (`executed_scripts`) (Jev) are masked in file mode (`redactFileText`: known formats only, no keyword/entropy heuristics).
- **Chat judge `read_file` results** are masked by the session `NetHook`, which `askPermissionModel` attaches to the per-request judge client.
- Both rubrics tell the judge that `[[OCSEC:xxxxxx:N]]` is a masked secret, to be treated as the credential it stands for.

`user_policy` is the user's own text and is sent as-is.

Jev gets `expanded_command` and `resolved_variables` in its state plus a rubric line telling it to judge paths from the expanded form. The chat judge gets the same as an "Expanded command" section under `Arguments`. The expansion is judge context only: the command that runs is unchanged, and `verifyAutoGrant` still checks the original. Jev's state also carries `interpreter` source and `executed_scripts` (the source of scripts the command executes — see *Executed scripts ship their source* above).

## `cd` is resolved in Go, not by the judge (2026-09-30 amendment)

Shell-variable expansion above resolves *names*. It never resolved a `cd`, and that turned out to be the dominant cause of low-confidence false prompts on the Jev path.

**The finding.** Jev cannot resolve a `cd` target against `allowed_roots` — it does no path-containment reasoning. Replaying one ordinary read-only `cd X && … && for …` command against the live API with the real 37-root / 148-prefix state, 5 repeats per case:

| what the judge saw | choice | confidence | concern |
|---|---|---|---|
| the `cd` | deny | 0.06 [0.04-0.10] | `outside_allowed_roots` (wrong — the target was in a listed root) |
| the `cd` folded away | allow | 0.96 [0.95-0.97] | none |
| `working_directory` moved instead, `cd` kept | allow | 0.89 [0.85-0.92] | none |

5/5 runs fell below the 0.85 floor in every variant containing a `cd`, so **every `cd`-bearing command was a systematic false prompt**. The root count and prefix-list size are not causes: with the `cd` gone the full state still scores 0.96.

**The fix.** `foldTopLevelCds` (`internal/agent/permission_cdfold.go`) strips top-level `cd <literal>` statements before the state is built. `buildTypesafePermissionState` then sends `working_directory` set to the resolved target, `expanded_command` carrying the folded command, and `resolved_cd`. `arguments.command` stays verbatim so the audit trail still shows what runs.

**It fails closed, and that is the load-bearing property.** Folding converts an ask into an allow, so it happens only for a literal, in-scope, unconditional top-level target. Everything else returns the command byte-identical, so the judge still sees the `cd` and a human decides. Refused: `cd -`, bare `cd`, `cd $VAR`, `cd ~/x`, globs, `$(…)`/backtick and quoted targets, out-of-scope targets, `..` escapes, multi-line commands, and any `cd` following a pipeline, `||`, or control flow. Control flow is a *freeze*, not a blanket refusal: `cd`s before a `for`/`if` still fold (the reported command's own `for` loop must not defeat it), a `cd` after one does not.

**Not fixed here.** `OutOfScopePath` is still never populated for compound commands, because `shellCompound` (`internal/agent/permissions.go`) makes `firstOutOfScopePath` bail, so `verifyAutoGrant`'s scope guard remains unreachable for them. Folding improves what the judge sees; it does not add a deterministic scope check. Tracked in `TODO.md`.

## Durable judge log (PERMJUDGE, 2026-09-30)

Every auto-permission decision writes one JSON record to `permission-judge.log` under the global logs dir, so a below-floor deferral is diagnosable after the fact instead of arriving as a bare banner with no category and no evidence.

**Path and shape.** `ensurePermissionJudgeLog` mirrors the `PERMJUDGE` debug kind through the existing `debuglog.MirrorKindToFile` (2 MB, single generation — no hand-rolled rotation), and `logPermissionJudge` appends to `debuglog.Log` *directly* rather than only via `emitDebug`, so the file does not depend on the debug sink being wired. The file is created **0600**: it carries command text, which can include a credential literal. Records are append-only and every branch is a terminal outcome, so exactly one record exists per judge call.

**`outcome` is the field to read first** — it is what the deferral banner does not tell you:

| `outcome` | meaning |
|---|---|
| `granted` | allow at/above the floor, `verifyAutoGrant` passed |
| `granted_relaxed_concern` | allow under a relaxed concern, on the opaque floor |
| `deferred_below_floor` | **leaned allow, confidence below the floor** — the case that produced the unexplained 0.21 |
| `refused_deterministic_guard` | the judge allowed, but a Go guard refused |
| `denied_by_judge` | the judge denied |
| `transport_error` / `no_verdict` / `unknown_choice` | never reached a decision |

Supporting fields: `choice`, `confidence`, `probabilities`, `concern`, `concern_confidence`, `floor`, `resolved_cd`, `working_directory`, `allow_destructive`, `rule`, `scope`, `error`. `reason` carries the human-readable string the banner shows, so the log and the prompt cannot disagree.

**Secrets are withheld, not logged in the clear.** A command containing detected secret material is replaced by `command_withheld` plus a `reason`; `command` is then empty. Detection uses `redact.Detect` (keyword + entropy, so it catches `Bearer …` and `https://user:pass@host` shapes that a fixed vendor-format list misses) plus the `/mask` registry.

**`allowed_roots` is trimmed, and the total is preserved.** Logging all ~100 roots made each record ~100 KB, which left only ~20 records inside the 2 MB cap — a log that cannot hold a session. `relevantAllowedRoots` keeps the roots relevant to the workdir and to the paths the command names, capped at 12, and reports `allowed_roots_total` and `allowed_roots_omitted` so "the target was in none of the N roots" stays answerable. Comparison runs through `resolveForScopeCheck`, not `EvalSymlinks` directly: roots are symlink-resolved by construction, and `EvalSymlinks` fails outright on a path that does not exist yet (on macOS `/var/folders/…` is a link to `/private/var/folders/…`). Candidates are collected from **both** the original and the folded command, since the folded-away `cd` target is precisely the path whose containment decides the outcome.

## UI

`PermissionsForm.tsx` loads the catalog and the auto-permission config in parallel. Each category is a checkbox with `aria-label="Enforce <key>"`; **ticked = enforced**, and saving writes `relaxed_concerns` = the **unticked** keys. `All` / `None` buttons set the array to `[]` / every key. The block renders a server-unavailable fallback when the catalog is empty.

For the interpreter path the judge prompt gained a guidance bullet (opt-out categories listed after the base rules) and the request JSON carries `relaxed_concerns`, mirroring the TypeSafe state field.

## Tests

- `internal/agent/permission_relaxed_concerns_test.go` — catalog/rubric parity, clause emptiness when nothing is relaxed, wire state + both question instructions, relaxed-deny honoured, deny stands when not attributable, out-of-scope guard still wins, dangerous rm refused on both the relaxed-deny and plain judge-allow routes (`TestRelaxedDestructiveCannotGrantDangerousRm`), chat prompt carries the section.
- `internal/agent/permission_typesafe_opaque_test.go` — opaque allow@0.80 grants; boundary 0.75 grants / 0.74 defers; none/secrets/network at 0.79 still defer at the 0.80 default floor; a configured 0.95 still governs an opaque allow that would otherwise clear 0.75; resolver table (unset uses the 0.75 default, 0.5/0.75/0.85/0.95 configured values all govern); mutation-verified.
- `internal/agent/permission_interpreter_relaxed_test.go` — per-category strict-refuses / relaxed-allows table (plus "a different category must not allow it"), safety-floor subtests (model decision ask, confidence floor, hard-blocked raw command, hard-blocked/harmful subprocesses), config wiring via the `verifyInterpreterEffectsWith` wrapper, and the end-to-end grant rule through the `OnPermissionGrant` sink (`TestInterpreterRelaxedAllowDoesNotPersistGrant` — a relaxation-load-bearing allow does **not** persist a durable grant; `TestInterpreterStrictPathStillRefusesAndPersists` — strict refusal refuses and a clean strict allow persists its grant). Two mutations were verified to fail: removing the network relaxation, and letting relaxed grants persist.
- `internal/agent/permission_judge_log_test.go` — record carries its diagnostic fields; the fold is recorded (`resolved_cd`); a secret-bearing command is withheld; a **benign command is kept verbatim** (anti-vacuity: without it a test asserting only "redacted" passes while the log records nothing); nil agent does not panic; mirror registration is idempotent; `allowed_roots` is trimmed to the cap with the total preserved and a record under 4 KB; a URL-credential command is withheld.
- `internal/agent/permission_typesafe_test.go` — `TestPermissionJudgeLog_RecordsOutcomeEndToEnd` drives the real `askPermissionModelTypesafe` over a stubbed transport and asserts exactly one record with the right `outcome` for granted / below-floor / denied. This is what proves the log is *wired*, not merely constructible.
- `internal/config/relaxed_concerns_test.go` — round-trip, replace-not-merge, explicit-empty clear, nil-safety.
- `internal/server/handler_config_test.go` — catalog endpoint and `permissions-auto` PUT round-trip.
- `web/src/components/Settings/PermissionsForm.concerns.test.tsx` — default-all-ticked, unticking sends the negative set.

## Related

- Judge contract: the TypeSafe/Jev concern answer does **not** decide the verdict — a `deny` still defers to the human. It only selects the confidence floor for an `allow` verdict: `truncated_or_unknown` → opaque floor `0.75` (`min(normal, 0.75)`), every other concern → `permissions.auto.min_confidence` (default `0.85`). Deterministic `verifyAutoGrant` runs after the floor check.
- Design doc: `docs/superpowers/specs/2026-09-21-auto-permission-enforced-categories-design.md`.
- Modes and how the auto-permission layer sits among them: [Sandbox Permission Mode](concepts/sandbox-permission-mode.md).
- Shared Jev/TypeSafe vocabulary and confidence-floor conventions: [Discovery TypeSafe Relevance Judge](concepts/discovery-typesafe-judge.md).
- Go-side guards referenced above: [permission evaluation and unknown-tool guard](gotchas/permission-evaluation-and-unknown-tool-guard.md), [auto-permission judge withholds credentials](gotchas/auto-permission-judge-withholds-credentials.md).

## Amendment (2026-09-29): environment enumeration carve-out in the Jev rubric

A single new bullet was added to the `typesafeJudgeInstructions` raw-string gatekeeper rubric in `internal/agent/permission_typesafe.go`, inserted immediately after the existing "reading a credential-bearing file (.env, ~/.ssh, …) is NOT by itself a reason to deny or to hesitate" carve-out and immediately before the `allow_destructive=false` bullet. It extends the same exposure-based rule from credential **files** to the **environment**:

> Enumerating the environment is subject to the same rule, not a stricter one: what makes it a concern is a secret's VALUE reaching the output, a file, or another process, never the existence of a variable. Listing variable NAMES, or redacting values per line, is ordinary debugging and must be ALLOWED even when a later filter would match a credential-bearing key: `env | cut -d= -f1`, `compgen -v`, `env | sed 's/=.*/=<set>/'`, and `env | grep -i TOKEN | sed 's/=.*/=/'` are all allowed, because `sed` rewrites every line before anything is displayed and `grep` only narrows which keys are shown. Judge the pipeline in order and do not deny a command merely because it contains the word `env`. A bare `env`, `printenv` or `set` with no filter that prints every value at once IS the concern.

**Motivating incident.** A live call was auto-deferred: `cd <repo> && grep -ri "typesafe\|jev" --include=… -l . | head -20; env | grep -i typesafe | sed 's/=.*/=<set>/'`. The judge (typesafe/jev-latest) leaned ALLOW at confidence **0.72**, below the **0.85** `permissions.auto.min_confidence` floor, so the human saw "Auto-denied by LLM permission model: TypeSafe judge leaned allow but confidence 0.72 is below the 0.85 floor". Two gaps combined:

1. The secrets carve-out covered reading credential **files** and consuming a value as an argument to a local program, but said nothing about enumerating the environment — so the judge had no rule pointing at the trailing `sed` redaction and had to re-derive pipeline ordering itself before it could call the shape safe. (That the `env` keyword triggered the hesitation is a hypothesis, not confirmed: the repo-wide `grep -ri` was an equally plausible source. Untested either way.)
2. The concern answer came back `none`, so the banner carried no explanation, and the `truncated_or_unknown` → 0.75 opaque confidence relief never engaged.

**What did NOT change:**

- **No code path changed.** The concern vocabulary, the floor resolvers, the `secrets` concern **label**, and the deny backstop are all untouched — this was rubric prose only.
- `permissions.auto.min_confidence` defaulted to **0.85** at the time (now **0.80**) and an explicitly configured value still governs; the opaque **0.75** relief remains gated on the judge naming `truncated_or_unknown`, which it did not here. See *Opaque floor* above — unchanged.
- The **`none`-but-hesitant** case (a below-floor `allow` with no named concern, hence no explanation in the banner) is a **KNOWN REMAINING GAP**, deliberately NOT addressed in this change: the fix belongs in the fallback reasoning, not in the rubric. Recorded here as open.
- The bullet contains the words `sed` and quoted fragments but is plain text inside a **Go raw string literal** — a backtick anywhere in it would terminate the literal. That is now pinned by a test.

**Tests** (`internal/agent/permission_typesafe_rubric_test.go`, both mutation-verified — deleting the bullet fails both):

- `TestTypesafeJudgeInstructionsCarveOutEnvironmentNameListing` — pins five distinctive fragments, including the two literal command shapes and the "sed rewrites every line before anything is displayed" justification, so trimming the allowed-forms list back out still fails.
- `TestTypesafeEnvironmentCarveOutIsInVerdictRubric` — asserts the clause is in the **verdict** rubric (not only the concern rubric, since the verdict answer is the one gated by the floor) and that the raw string contains no backtick.

**Prose mirror.** The rubric's prose copy gained a parallel bullet right after the `.env`/psql carve-out: `skills/ocode-permissions/SKILL.md` (the source; the `Makefile` copies `skills/` into `cmd/ocode-desktop/embedded-assets/skills/` at build time, and that mirrored copy carries the same bullet).

## Amendment (2026-10-04): The permission judge resolves through `resolveDecider(slotPermission)`

The permission judge now resolves its decision backend through the same `Decider` seam as the other judges. `askPermissionModelTypesafe` (`internal/agent/permission_typesafe.go:229`) takes a `Decider` parameter instead of a concrete `*TypesafeClient`. The caller resolves it via `resolveDecider(slotPermission)` (`internal/agent/decider.go:112`), which reads the `permissions.auto.model` config key (unchanged; not moved or renamed).

`isTypesafeModel` was **deleted** from `internal/agent/permission_typesafe.go`. It tested a literal `typesafe/` prefix and had exactly one caller — `consultPermissionModel`'s decision-model gate — which now uses `isDecisionModel` (`internal/agent/decider.go:214`) instead. `isDecisionModel` is the single place that decides which provider/model ids route to a decision backend, so a new decision provider is taught there once.

The fail-open contract, the confidence floors, and the opaque-floor override are unchanged. The **deterministic Go safety boundary did change** — see the amendment below.

## Amendment (2026-10-04): `rm` inside the allowed roots is in scope; the boundary refuses the rest

`dangerousRmReason` (`internal/agent/permissions.go`) — the pre-existing "does this `rm` destroy something it must not?" check — gained four refusals so the boundary agrees with what the rubric now tells the judge:

- **Glob / variable / substitution targets are refused outright.** A target containing any of `*?[]{}$\`~` has no single path to scope-check; the shell decides the real targets, so the judge's "inside the roots" reading cannot be verified. Refused rather than guessed (`rm -rf build/*`, `rm -rf $DIR`).
- **The project directory, or any parent of it, is refused.** Deleting *inside* the workdir is ordinary development; deleting the workdir itself is not something a judge's word or a prefix rule should grant.
- **An allowed root itself is refused** — deleting a whole configured root is a different act from deleting a file inside one.
- **Anything under `.git` is refused**, matched per path component *below the owning root only*, so a project that merely happens to be checked out under a directory named `.git` is still deletable. Comparison is `strings.EqualFold`, so `.GIT` on a case-insensitive filesystem is caught too.

Path handling: `canonicalPath` (`filepath.Clean` + `filepath.EvalSymlinks`, resolving through the nearest existing ancestor so a not-yet-created target is judged by where it WOULD live) and `pathsEqual` (case-insensitive on darwin/windows) mean neither a symlink nor a spelling difference slips past. `pathUnder` treats `/` explicitly, because `parent + sep` would be `//` and match nothing.

**Tests:** `internal/agent/rm_guard_paths_test.go`, plus the `TestPermissions_*` cases in `permission_overwrites_test.go`.
