# Part 04 — Question-id sanitising and the question ceiling

Two Clef-specific guards in `clef.go`. **No behaviour change** for the relevance judges' verdicts — but without these, every clef-backed relevance judge fails outright, because ocode's natural keys are ids Clef rejects.

## Constraints (apply to every part; duplicated here because parts do not cross-reference)

- No new module dependencies.
- Decision-only, permanently: never reachable from chat, compaction, small-model, recap, or task-contract paths.
- Fail-open is a hard invariant. Specifically: a judge may only **hide** a result. A candidate the judge could not answer for must keep its pre-judge state, never be dropped.
- No silent backend fallback.
- Missing credential must be visible, not a silent nil.
- `permissions.auto.model` and `auto_continue_model` are not refactored or aliased.
- Confidence floors stay shared: 0.85 permission, 0.5 relevance.
- `TypesafeQuestion` and `TypesafeResponse` shapes are unchanged; `Criteria` stays `map[string]string`.
- Never call a resolver under a cache lock, and never hold a lock across a network call.
- `gofmt` clean, `go vet` clean. Tests named `Test<Subject>_<Behaviour>`.
- Do **not** touch `discoveryAllows` (`internal/agent/discovery_glue.go`) — the MCP tool gate must never fail open. Only the per-candidate veto path changes here.
- `internal/agent/content_guard_typesafe.go` is `AM` in git — stage only your own hunks. Never `git stash`, `reset`, `checkout --`, or `clean`.

## What Clef requires

Its published input schema bounds the request two ways: `questions` allows at most 64 entries, and a question id may use only letters, digits, underscore, dot and hyphen, up to 100 characters.

ocode violates the charset everywhere: `discovery` keys its questions by a doc id of the form `skill:<name>` or `mcp:<Server>/<tool>`, and `doc_search` and code search key by file path. All three relevance judges therefore send ids clef rejects. The 64 ceiling is not currently binding — the real caps are 30 selected candidates and 40 search candidates — so it is insurance.

## The trap in this part — caught in review, and it would have broken every collision

The collision-disambiguation suffix must itself be composed only of legal characters. A suffix using tilde looks harmless and is not: `~` is outside clef's charset, so **every id that actually collides would be rejected** — turning a subtle wrong-verdict risk into a total failure of that request, and only on the exact inputs the guard exists to handle.

Use a hyphen-based numeric suffix. Add a test that asserts every produced id is legal, not merely distinct — a distinctness-only test passes while the request is still broken.

## Steps

1. Write the guard tests first:
   - Sanitising maps ids containing slashes and colons onto the legal charset, within the length limit.
   - **Two ids that differ only in illegal characters get distinct wire ids** (`a/b.go` against `a_b.go`).
   - **Two ids sharing a long prefix get distinct wire ids after truncation.**
   - Every produced wire id is individually **legal** (charset and length) — this is the assertion that catches the illegal-suffix trap.
   - The demux map inverts every wire id back to its original.
   - Sanitising is deterministic: the same input map yields the same mapping across calls.
   - The ceiling trims to 64 questions and reports how many were dropped.

2. Run; they fail because the guards do not exist.

3. Implement the charset mapper: replace every illegal rune with a legal substitute and cap the result at 100 characters, cutting on a rune boundary so the id stays valid UTF-8.

4. Implement the collision loop. Iterate the caller's ids in **sorted order** so the mapping is deterministic regardless of map iteration order, and give each collision a legal, length-preserving numeric suffix. Return both directions of the map.

5. Enforce the ceiling. When there are more than 64 questions, keep a deterministic 64 and report the number dropped. **Trim the wire map and the reverse map alongside the question map** — leaving a stale entry behind means an answer arriving for a dropped question could be attributed to a live candidate.

6. Demux the response. Answers arrive keyed by wire id; re-key them onto the caller's original ids and discard anything the caller did not send. An id the backend did not answer stays **absent** — which every judge already treats as "no verdict, keep the candidate". Absence must never be turned into a zero value: a zero `Noul` reads as "definitely irrelevant" and silently vetoes.

7. Wire it into the request builder in `clef.go`, so the guards apply to the bytes that actually go on the wire.

## Checks

- `go build ./...` and `go vet ./...` clean.
- `gofmt -l` prints nothing for touched files.
- `go test ./internal/agent/ -run TestClef -v -count=1` passes.
- `go test -race ./internal/agent/ -count=1` passes (≈2 min).
- **Mutation check** on this part specifically — see the INDEX verification gates. Targets: the length cap, the collision loop, and the legality check. Confirm each mutant compiles before recording it as caught. If the sorted-iteration mutant survives, establish whether it is equivalent for correctness while still affecting determinism; if so, the determinism test above is what pins it.

## Review focus

Try to construct two distinct caller ids that still collide, or a case where the reverse map loses an entry. The failure mode is one candidate silently receiving another's relevance verdict — a wrong veto with no error anywhere.

Confirm the dropped-question path leaves callers in the state they were in before the judge ran.

## Done when

All three relevance judges can address clef, every wire id is legal and unique, the ceiling trims without side effects, and no answer can be attributed to the wrong candidate.