---
type: Concept
title: Auto-Permission Enforced Categories
description: '''Per-category enforcement toggles for the LLM auto-permission judge AND the interpreter-effect verifier: the negative relaxed_concerns config set, the GET /api/config/ocode/permissions-concerns catalog, and the deterministic Go safety boundary that still applies.'''
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
timestamp: 2026-09-21T06:45:33Z
---
---
type: Concept
title: Auto-Permission Enforced Categories
description: 'Per-category enforcement toggles for the LLM auto-permission judge AND the interpreter-effect verifier: the negative relaxed_concerns config set, the GET /api/config/ocode/permissions-concerns catalog, and the deterministic Go safety boundary that still applies.'
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
timestamp: 2026-09-21T05:33:15Z
---
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

- **Jev / TypeSafe path** (`askPermissionModelTypesafe`): the relaxed clause is appended to **both** the verdict and the concern question instructions (`relaxedConcernsClause`), and mirrored into `state["relaxed_concerns"]`. Deterministically, a **DENY whose single named concern is in the relaxed set is converted to an allow**, logged `tier=auto_typesafe_relaxed`. A deny that names `none`, nothing, or a still-enforced category is **not attributable and stands** (fail closed). Because the clause is appended after the bundled addendum, it wins over the shipping policy.
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

## UI

`PermissionsForm.tsx` loads the catalog and the auto-permission config in parallel. Each category is a checkbox with `aria-label="Enforce <key>"`; **ticked = enforced**, and saving writes `relaxed_concerns` = the **unticked** keys. `All` / `None` buttons set the array to `[]` / every key. The block renders a server-unavailable fallback when the catalog is empty.

For the interpreter path the judge prompt gained a guidance bullet (opt-out categories listed after the base rules) and the request JSON carries `relaxed_concerns`, mirroring the TypeSafe state field.

## Tests

- `internal/agent/permission_relaxed_concerns_test.go` — catalog/rubric parity, clause emptiness when nothing is relaxed, wire state + both question instructions, relaxed-deny honoured, deny stands when not attributable, out-of-scope guard still wins, dangerous rm refused on both the relaxed-deny and plain judge-allow routes (`TestRelaxedDestructiveCannotGrantDangerousRm`), chat prompt carries the section.
- `internal/agent/permission_interpreter_relaxed_test.go` — per-category strict-refuses / relaxed-allows table (plus "a different category must not allow it"), safety-floor subtests (model decision ask, confidence floor, hard-blocked raw command, hard-blocked/harmful subprocesses), config wiring via the `verifyInterpreterEffectsWith` wrapper, and the end-to-end grant rule through the `OnPermissionGrant` sink (`TestInterpreterRelaxedAllowDoesNotPersistGrant` — a relaxation-load-bearing allow does **not** persist a durable grant; `TestInterpreterStrictPathStillRefusesAndPersists` — strict refusal refuses and a clean strict allow persists its grant). Two mutations were verified to fail: removing the network relaxation, and letting relaxed grants persist.
- `internal/config/relaxed_concerns_test.go` — round-trip, replace-not-merge, explicit-empty clear, nil-safety.
- `internal/server/handler_config_test.go` — catalog endpoint and `permissions-auto` PUT round-trip.
- `web/src/components/Settings/PermissionsForm.concerns.test.tsx` — default-all-ticked, unticking sends the negative set.

## Related

- Judge contract: the TypeSafe/Jev concern question plus the confidence floor `permissions.auto.min_confidence` (default `0.85`).
- Design doc: `docs/superpowers/specs/2026-09-21-auto-permission-enforced-categories-design.md`.
- Modes and how the auto-permission layer sits among them: [Sandbox Permission Mode](concepts/sandbox-permission-mode.md).
- Shared Jev/TypeSafe vocabulary and confidence-floor conventions: [Discovery TypeSafe Relevance Judge](concepts/discovery-typesafe-judge.md).
- Go-side guards referenced above: [permission evaluation and unknown-tool guard](gotchas/permission-evaluation-and-unknown-tool-guard.md), [auto-permission judge withholds credentials](gotchas/auto-permission-judge-withholds-credentials.md).
