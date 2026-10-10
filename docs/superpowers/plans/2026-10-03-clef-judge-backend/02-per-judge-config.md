# Part 02 — Per-judge model configuration

Five new config keys and per-slot resolution. **No behaviour change**, with one deliberate exception noted below.

## Constraints (apply to every part; duplicated here because parts do not cross-reference)

- No new module dependencies.
- Decision-only, permanently: never reachable from chat, compaction, small-model, recap, or task-contract paths.
- Fail-open is a hard invariant; a judge may only hide a result or defer to a human.
- No silent backend fallback.
- Missing credential must be visible, not a silent nil.
- `permissions.auto.model` and `auto_continue_model` are not refactored or aliased.
- Confidence floors stay shared: 0.85 permission, 0.5 relevance. No per-backend thresholds.
- `TypesafeQuestion` and `TypesafeResponse` shapes are unchanged; `Criteria` stays `map[string]string`.
- Never call a resolver under a cache lock, and never hold a lock across a network call.
- Config writes are targeted load-modify-write, never a whole-config save. This part only reads.
- `gofmt` clean, `go vet` clean. Tests named `Test<Subject>_<Behaviour>`.
- Do **not** touch `discoveryAllows` (`internal/agent/discovery_glue.go`).
- Four files this plan modifies are `MM` in git: `internal/agent/agent.go`, `internal/agent/client.go`, `internal/agent/permission_typesafe.go`, `internal/config/ocodeconfig.go`. `content_guard_typesafe.go` is `AM`. Never blanket-commit those paths; stage only your own hunks. Never `git stash`, `reset`, `checkout --`, or `clean`.

## Files

- Modify `internal/config/ocodeconfig.go` — the file-schema struct `ocodeConfigFile`, the runtime struct `OcodeConfig`, `DiscoveryConfig`, `defaultDiscoveryConfig`, the defaults literal, and the apply functions beside `applyDiscoveryConfig`
- Modify `internal/config/ocodeconfig_test.go`
- Modify `internal/agent/decider.go` — replace `slotModel`'s body
- Modify `internal/agent/discovery_typesafe.go` — delete `discoveryJudgeModel`
- Modify `internal/agent/network_guard_typesafe.go` — delete `networkGuardJudgeModel`
- Modify `internal/agent/content_guard_typesafe.go` — delete `contentGuardJudgeModel`
- Modify `internal/agent/discovery_glue_test.go` — the assertion near line 848 that references `discoveryJudgeModel`
- Modify `internal/agent/discovery_glue.go` — the status field that reports the judge model

## Config surface added

Runtime: a small `JudgeModelConfig` type holding one `JudgeModel` string, with four new fields on `OcodeConfig` (`DocSearch`, `Search`, `NetworkGuard`, `ContentGuard`) plus a `JudgeModel` field on `DiscoveryConfig`.

File schema: four new blocks (`doc_search`, `search`, `network_guard`, `content_guard`), each with a single `judge_model` key marked `omitempty`, plus `judge_model` on the existing `discovery` block.

Every default is `typesafe/jev-latest` — the exact value of the three constants being deleted.

## Steps

1. Write the config tests first. One test asserts all five slots default to `typesafe/jev-latest`. One test writes a config file setting all five to different values and asserts each is read back independently — that is the test that proves the slots are genuinely independent, which is the point of this part. Follow the existing test file's own loader/setup idiom rather than inventing one; `applyDiscoveryConfig` and `defaultDiscoveryConfig` show the pattern. Run; they fail because the fields do not exist.

2. Add the runtime `JudgeModelConfig` type and the five fields. Give `DiscoveryConfig.JudgeModel` a comment noting that struct previously covered only the embedding model, so the two settings are not to be conflated. Set the default in `defaultDiscoveryConfig` and in the defaults literal.

3. Add the four file-schema blocks and the five file fields, all `omitempty`.

4. Add a small apply helper beside `applyDiscoveryConfig` that writes a slot's model only when the file value is non-blank, and wire it for all five. **A blank `judge_model` must not clear the slot** — clearing would silently disable a judge the user never touched, which fails open but invisibly.

5. Replace `slotModel`'s body. `permission` reads the existing auto-permission model (falling back through its own current resolution), `auto_continue` reads the existing `auto_continue_model`, and the other five read their new keys, all falling back to the shared default. **Do not move or rename the two existing keys** — they stay authoritative for their slots.

6. Delete the three constants and every reference, including the one in `discovery_glue_test.go` and the status field in `discovery_glue.go`. Grep for all three names and confirm zero hits remain.

## Deliberate behaviour change

`doc_search` and `code_search` are currently served by the **same client** as `discovery` — all three call `discoveryJudgeClient`. They become three independently defaulted slots, so setting `discovery.judge_model` no longer silently moves the other two. This is intended, and the commit message must say so. The discovery judge's client cache must be keyed on the model id (not memoised with a one-shot latch), so a change to any one of these three takes effect.

## Checks

- `go build ./...` and `go vet ./...` clean.
- Grep for the three deleted constant names returns nothing.
- `go test ./internal/config/ ./internal/agent/ -count=1` passes.
- `go test ./internal/agent/ -count=1` passes with no edits to existing judge test expectations.

## Review focus

Confirm the "setting one slot does not move another" test actually asserts three different values rather than three copies of one — a test that sets all five to the same string would pass while proving nothing.

Confirm a blank or whitespace-only `judge_model` leaves the default in place.

## Done when

All five keys load independently, all default to `typesafe/jev-latest`, the three constants are gone, and the two pre-existing keys are untouched.