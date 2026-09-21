# Auto-permission: per-category enforcement toggles (design)

Date: 2026-09-21
Status: approved (Approach A, web Settings only)

## Problem

The auto-permission judge (TypeSafe/Jev, and the chat judge) decides every Ask-level
permission request against one fixed rulebook. The only user-level controls are
`enabled`, `allow_destructive`, a free-form `prompt`, and the confidence floor. A
user who accepts a whole class of requests (e.g. outbound `curl`, or "spawn a
subprocess") has no way to say so: every such request is re-judged and can land
below the confidence floor, producing a prompt with no attributable reason.

## Goal

Per-category checkboxes in Settings → Permissions, ticked by default, naming the
*types of thing* the judge must enforce. Unticking a category means the judge may
not deny a call whose only concern is that category.

## Decisions (approved)

1. **Semantics.** Unticked = not enforced → a call whose *only* named concern is an
   unticked category is **allowed** (still labelled, so the decision is auditable).
   If any named concern is still enforced, the call still blocks. This is not
   "ignore the category when reporting" and not "keep enforcing but drop the floor".
2. **Storage.** A *negative* set, so today's behaviour is the empty value and
   categories added later default to enforced:
   `permissions.auto.relaxed_concerns: []string` (JSON `relaxed_concerns`).
3. **Vocabulary.** One source of truth: the existing closed set `typesafeConcerns`
   (`internal/agent/permission_typesafe.go`) minus `none` — 8 categories. The UI
   catalog is served from Go so the checkbox list, the Jev rubric and the chat
   judge prompt cannot drift.
4. **Surface.** Web Settings only (`PermissionsForm.tsx`, Auto-approval section).
   No TUI parity in this change. All three judge paths honour the toggles: Jev
   (hard), the chat judge (instruction-only), and the interpreter-effect verifier
   (see the follow-up section below).
5. **Safety boundary (unchanged).** These toggles configure the judge, not Go's
   deterministic guards. `verifyAutoGrant` runs on *both* judge paths
   (`agent.go:3746`, `permission_typesafe.go:157`), so an unticked box cannot
   auto-grant a hard-blocked command, a dangerous `rm`, an out-of-scope bash
   target, or truncated args/scripts. `IsHarmfulRequest && !allow_destructive`
   still bypasses the judge entirely. Hard-denied `/ban` prefixes still win. Each
   partly-gated category carries a `note` shown in the UI so the switch never
   claims more than it does.

   **Correction found while implementing (shipped in the same change):** the
   dangerous-`rm` half of that guarantee was FALSE at HEAD. `dangerousRmReason`
   raises its Ask with rule `bash.prefix.rm` and no `OutOfScopePath`, and a
   dangerous `rm` is not `IsHarmfulRequest`, so `verifyAutoGrant` returned true
   and the judge's word alone auto-granted an out-of-scope `rm -rf ../` on both
   judge paths. `verifyAutoGrant`'s bash branch now refuses any command where
   `dangerousRmReason` fires, restoring the function's documented contract
   ("should always require human approval, regardless of YOLO mode or any
   persisted rm bash-prefix rule"). Test: `TestRelaxedDestructiveCannotGrantDangerousRm`
   (covers the plain judge-allow and relaxed-deny routes). The `destructive`
   category's UI note was corrected accordingly — it is not gated on
   `allow_destructive`.

## Design

### Config (`internal/config/ocodeconfig.go`)

- `AutoPermissionConfig.RelaxedConcerns []string` (`json:"relaxed_concerns,omitempty"`).
- Mirror in `autoPermissionConfigFile` (file-facing pointer struct) + replace
  semantics in `applyAutoPermissionConfig` (same shape as `Grants`). Writer needs
  no change: `writeOcodeConfigFile` emits `cfg.Permissions` wholesale.
- `func (a *AutoPermissionConfig) ConcernRelaxed(key string) bool` — one predicate
  for both judges.

### Catalog + resolution (`internal/agent/permission_typesafe.go`)

- `RelaxableConcerns() []RelaxableConcern` where
  `RelaxableConcern{Key, Label, Note}` is derived from `typesafeConcerns` (minus
  `none`), with a `Note` on the partly-gated ones (`outside_allowed_roots`,
  `destructive`, `banned_prefix`, `secrets`, `truncated_or_unknown`).
- `func (a *Agent) relaxedConcernKeys() []string` (sorted, validated against the
  catalog so an unknown key is ignored).
- New read-only endpoint GET `/api/config/ocode/permissions-concerns` returning the
  catalog (with `note`), so the form has no hardcoded list to drift. The existing
  PUT stays a pure config write and never carries catalog fields.

### Judge — TypeSafe/Jev

- `buildTypesafePermissionState` gains `state["relaxed_concerns"]`.
- The verdict question's instructions become
  `typesafeJudgeInstructions + relaxedConcernsClause(keys)`: the user has switched
  off these categories, so a call whose only concern is one of them must be
  ALLOWed; the concern question must still name the category.
- **Deterministic backstop:** in `askPermissionModelTypesafe`, a `deny` whose
  single named concern is relaxed is converted to an allow (subject to the
  existing `verifyAutoGrant`), logged as `tier=auto_typesafe_relaxed`. A deny with
  `none`/missing/unrelaxed concern stands — fail closed when the reason cannot be
  attributed to an opted-out category.

### Judge — interpreter-effect verifier (follow-up, added 2026-09-21)

The third path (`askPermissionModelInterpreter`, used for bash interpreter
commands when the judge model is NOT TypeSafe) applies the toggles to its
deterministic gates via `verifyInterpreterEffectsWith`:

| gate | category |
|---|---|
| `effects.unknown` non-empty; truncated source | `truncated_or_unknown` |
| read/write/delete path outside allowed roots | `outside_allowed_roots` |
| `isSensitivePath` hit | `secrets` |
| subprocess wrapper / spawns shell-or-interpreter | `subprocess_or_dynamic_code` |
| network-capable subprocess; host outside the webfetch allowlist | `network` |
| deletes/`db_destructive` with `allow_destructive=false` | `destructive` |

`verifyInterpreterEffects` is kept as a thin wrapper that passes the agent's own
opt-outs, so the pre-existing call sites (and their tests) are unchanged, and the
`…With(…, nil)` form is the "would this have passed with everything enforced?"
question used for grants. Never relaxable: the model's `decision`, the confidence
floor, a hard-blocked raw command, and hard-blocked/harmful subprocesses — those
are now checked *before* the relaxable subprocess classification.

**Grants must not outlive the opt-out.** A durable `interpreter_exact` grant is
only persisted when the response would also have passed strictly; a
relaxation-load-bearing allow is granted for that invocation only
(`tier=auto_interp_relaxed_allow`), so re-ticking a category takes effect on the
next invocation instead of being shadowed by a saved grant.

### Judge — chat judge (`askPermissionModel`)

- Instruction-only parity: the relaxed keys are rendered into the existing prompt
  as a per-request section ("do not deny a call whose only issue is one of
  these"), since the chat verdict carries no category to attribute a deny to. If it
  denies anyway the deny stands (fail closed). Follow-up is a required
  `CONCERN:` line on the chat verdict.
- The clause is inserted after the bundled addendum (which hard-codes "Deny when a
  credential appears…") so the user's opt-out wins.

### UI (`web/src/components/Settings/PermissionsForm.tsx`)

- New block inside the Auto-approval section: "Categories the judge must enforce",
  8 checkboxes all ticked by default (empty `relaxed_concerns` = all enforced),
  "All / none" shortcuts, per-category `note` rendered as muted helper text.
- Saving sends `relaxed_concerns` = the unticked keys.

## Testing

- Go: `applyAutoPermissionConfig` replace semantics + on-disk round-trip;
  catalog == `typesafeConcerns` minus `none` (parity); `RelaxableConcerns` notes;
  relaxed deny→allow backstop (relaxed allow, unrelaxed deny, `none` deny);
  clause present in the wire request; catalog endpoint shape.
- Web: default all-ticked; unticking `secrets` sends `relaxed_concerns:["secrets"]`;
  re-ticking everything sends `[]`.
- Mutation-check each new guard (revert → test fails → restore).

## Out of scope

TUI toggles; chat-judge deny attribution; per-project scope (the auto-permission
block is global, matching the existing settings).
