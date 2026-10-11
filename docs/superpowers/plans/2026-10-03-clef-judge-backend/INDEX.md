# Clef decision-judge backend — Implementation Plan

Goal: add Cloudflare `@cf/cloudflare/clef-flash` as a second decision-judge backend behind a `Decider` interface, and make the model independently configurable for each of the seven judges.

Spec: `docs/superpowers/specs/2026-10-03-clef-judge-backend-design.md`

## Phases

| Part | Phase | Behaviour change? |
| --- | --- | --- |
| `01-decider-seam.md` | Introduce the `Decider` interface; widen the judges from `*TypesafeClient` | No |
| `02-per-judge-config.md` | Five new config keys, per-slot resolution, delete three hardcoded constants | No, except doc_search/code_search become independently settable |
| `03-clef-client.md` | `ClefClient` and provider routing | No |
| `04-clef-guards.md` | Question-id sanitising/demux and the 64-question ceiling | No |
| `05-state-budget.md` | Shared state-size budget guard | **Yes — the only behaviour change** |
| `06-budget-regression-test.md` | Turn the spec's measurement table into assertions | No |
| `07-agreement-eval.md` | Skippable live agreement/latency/calibration eval | No |
| `08-model-attribution.md` | Make model attribution and spend accounting provider-agnostic | Correctness fix |
| `09-docs.md` | Update `CLAUDE.md` and four concept pages | No |

Parts 01–04 are each independently shippable and independently revertable. **05 is the only part that changes what a user experiences**; it is isolated so a reviewer can reject it without touching the rest.

## Status

Every part is implementable as written. One premise is unresolved and is recorded rather than assumed: **nobody has verified whether the incumbent truncates an oversized `state` or rejects it with 400.** That is server-side behaviour and needs a live probe. The code narrows it — there is no state-size guard in `internal/agent/typesafe.go` today, and a non-2xx already becomes a provider-status error that the permission judge reports as "model unavailable" and defers to the human — so both worlds end in a human being asked, differing only in severity. Parts 01–04, 06–09 are unaffected.

Deferrals are tracked in `TODO.md` under "Clef judge backend — open deferrals (2026-10-03)": the unrun agreement eval, this unresolved premise, the project-rather-than-refuse decision, the unwritten UI plan, the inferred rate limit, the pending anchor re-derivation, and the two documented Jev failure modes.

## Architecture

`TypesafeClient` already declares all four methods the interface needs (`Decide`, `DecideCtx`, `GetProvider`, `GetModel` in `internal/agent/typesafe.go`), so introducing `Decider` is a signature change rather than a rewrite. One resolver keyed by judge slot replaces five sites that currently assert to `*TypesafeClient`. `ClefClient` implements the same interface against Cloudflare's native `/ai/run/` endpoint, which is required because Clef is not served from the OpenAI-compatible chat path.

Guard ownership splits deliberately. Question-id sanitising and the question ceiling are Clef-specific and live in `clef.go`. The state-size budget is backend-independent — Jev's documented 32K `state` limit is stricter than Clef's flat 65,536 — so that guard lives in the shared seam.

## Global constraints

These apply to every part. They are duplicated verbatim into each part file, because parts do not cross-reference.

- **No new module dependencies.** Standard library plus what `internal/agent` already imports.
- **Decision-only, permanently.** Neither `ClefClient` nor `TypesafeClient` may become reachable from the chat, compaction, small-model, recap, or task-contract paths. Both expose a `Chat` method that returns a sentinel error. `Decider` is deliberately narrower than `LLMClient` specifically so a decision backend cannot be wired into a chat path by accident.
- **Fail-open is a hard invariant.** A judge may only hide a result, defer to a human, or defer by asking a human. It may never invent a result. Every error path ends in "keep everything" or "ask the human".
- **No silent backend fallback.** If a slot is configured for clef and clef fails, that is a visible failure. Never retry against the other backend.
- **Missing credential must be visible, not a silent nil.** A slot naming a model whose provider is not connected must surface a clear reason in debug output and, for the permission and auto-continue slots, a human-readable notice — not a silent "judge disabled".
- **Existing config keys stay authoritative.** `permissions.auto.model` and `auto_continue_model` are not refactored, moved, deprecated, or aliased.
- **Confidence floors are shared across backends.** The permission judge keeps `autoJudgeMinConfidenceDefault` (0.85) and the relevance judges keep `relevanceJudgeMinConfidenceDefault` (0.5). No per-backend thresholds. The falsification condition is in part 07.
- **Shared vocabulary is unchanged.** `TypesafeQuestion` and `TypesafeResponse` stay as they are. `TypesafeQuestion.Criteria` remains `map[string]string`; it cannot express Clef's `score` type (ordered array criteria) and that is acceptable because ocode uses only `noul` and `choice`. Comment it; do not add a `score` variant.
- **Every new config key defaults to `typesafe/jev-latest`**, so an unconfigured install behaves exactly as today.
- **Never hold a lock across a network call, and never call a resolver under a cache lock.** Compute the value, then take the lock, then store.
- **`gofmt` clean, `go vet` clean** on every file touched. Comments explain *why* and name the failure being prevented, matching surrounding style.
- **Test naming** follows `Test<Subject>_<Behaviour>`.

## Cross-cutting invariants

- **Config writes are targeted load-modify-write**, never `SaveOcodeConfig(snapshot)`. This plan only reads config; the UI that writes these keys is a separate plan and inherits the rule.
- **No new goroutines** are introduced anywhere. The one concurrency change is in part 01. If an implementer adds a goroutine anyway it must go through `crashguard.Go` — a bare `go func` in `internal/agent` can panic past Bubble Tea's recovery and strand the terminal in the alt-screen.
- **Prompt-cache stability is unaffected.** Nothing here touches the main chat path's tool array or system block. Judge requests are side calls with their own bodies. Explicitly: do not make a `Decider` reachable from `GetToolDefinitions`, the compaction summary, or the small-model resolver.
- **No new environment variable.** Checked: this repository has no `.env.example` (nor `AGENTS.md`, `OCODE.md`, `.cursorrules`, or `.opencode/rules/`). Cloudflare credentials come from `auth.json` and the TUI `/connect` flow, which already captures the account id and stores the derived base URL. Clef reuses that credential, so there is nothing to add.
- **Documentation** to update is enumerated in part 09. After editing any page that cites `file.go:NNN`, re-derive every anchor on that page by printing the real source line.

## Working tree discipline

This tree is shared with concurrent sessions. At the time this plan was written, four files it modifies already carried a peer's staged changes (`MM`): `internal/agent/agent.go`, `internal/agent/client.go`, `internal/agent/permission_typesafe.go`, `internal/config/ocodeconfig.go`. `internal/agent/content_guard_typesafe.go` is `AM`.

- **Never** use `git stash`, `git reset`, `git checkout --`, or `git clean`.
- **Never** blanket-`git commit <path>` a file that shows `MM` or `AM`. `git commit <path>` commits working-tree content and would sweep the peer's staged edits into the commit. For those files, stage only your own hunks (diff the file, keep your hunks, apply them to the index) and commit what is staged. If your hunks cannot be separated cleanly, leave that file uncommitted and say so in the handoff.

## Verification gates

Run in addition to each part's own checks. A part is not done until its tests pass and the gate matching its blast radius passes.

- `go build ./...` and `go vet ./...` after every part.
- `gofmt -l` prints nothing for files you touched.
- `go test -race ./internal/agent/ -count=1` is a real gate after parts 01, 04 and 05, which change concurrency and the answer-demux path. The package takes roughly two minutes under `-race`; let it finish.
- **Mutation check** on the guard tests, because a passing test that never exercises the branch is worse than no test. Verify each mutant compiles before recording it as caught, per `docs/gotchas/mutation-check-mutants-must-compile.md`. Targets: the id length cap, the collision-suffix loop, the legality check, the answer-type validation branch, and the budget comparison operator. One of these may be an equivalent mutant — establish which before counting it as a coverage gap.

## Plan 1 → Plan 2 contract

The UI plan (seven model pickers, TUI and web) consumes exactly this surface and nothing else. It does not need `ClefClient` and must not import it.

| Element | Shape |
| --- | --- |
| Config keys | `discovery.judge_model`, `doc_search.judge_model`, `search.judge_model`, `network_guard.judge_model`, `content_guard.judge_model`; all `omitempty` |
| Untouched keys | `permissions.auto.model`, `auto_continue_model` |
| Model id grammar | `<provider>/<model>`, e.g. `typesafe/jev-latest`, `cloudflare-workers/@cf/cloudflare/clef-flash` |
| Slot names | `permission`, `auto_continue`, `discovery`, `doc_search`, `code_search`, `network_guard`, `content_guard` |
| Write path | a targeted per-key saver, never a whole-config save |
| Default shown | `typesafe/jev-latest` |

`code_search` reads the `search.judge_model` key — the slot name and key name differ deliberately, because `search` is also a tool name.

## Review focus

Inputs the spec implies that no happy-path test exercises. Each is pinned by a named test in the part that owns the code.

1. **Two question ids that sanitise to the same string.** `a/b.go` and `a_b.go` both reduce to `a_b.go`. A wrong demux map silently gives one candidate another's relevance verdict. Pinned in part 04.
2. **An id longer than 100 characters that collides after truncation.** Same failure, via truncation rather than substitution. Pinned in part 04.
3. **A state sized exactly at the budget.** The permission path must accept a state at the ceiling and reject only the first byte past it. An off-by-one either fails every large write or lets a real overflow through. Pinned in part 05 — and the test must construct a state whose *marshalled* size is exactly the budget, not whose payload is, or it passes vacuously.
4. **An answer present but of the wrong type.** A `choice` answer returned for an id asked as `noul` leaves `Noul` at zero, which reads as "definitely irrelevant" and silently vetoes — a fail-*closed* bug inside a fail-open system. Pinned in part 05.
5. **An out-of-range or unnormalised probability.** The floors assume a real distribution. A malformed one must be treated as "no verdict", never as a confident verdict. Pinned in part 05.
6. **A clef model configured with no Cloudflare credential.** Must be visible, not a silent nil. Pinned in part 03.