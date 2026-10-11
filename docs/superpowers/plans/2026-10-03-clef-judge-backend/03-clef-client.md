# Part 03 — `ClefClient` and provider routing

A second backend that speaks the same System One wire format. **No behaviour change**: no slot points at clef until a user configures one.

## Constraints (apply to every part; duplicated here because parts do not cross-reference)

- No new module dependencies.
- Decision-only, permanently: never reachable from chat, compaction, small-model, recap, or task-contract paths. This client is served only from Cloudflare's native run endpoint, never the OpenAI-compatible chat path.
- Fail-open is a hard invariant; a judge may only hide a result or defer to a human.
- No silent backend fallback.
- Missing credential must be visible, not a silent nil.
- `permissions.auto.model` and `auto_continue_model` are not refactored or aliased.
- Confidence floors stay shared: 0.85 permission, 0.5 relevance.
- `TypesafeQuestion` and `TypesafeResponse` shapes are unchanged; `Criteria` stays `map[string]string`.
- Never call a resolver under a cache lock, and never hold a lock across a network call.
- `gofmt` clean, `go vet` clean. Tests named `Test<Subject>_<Behaviour>`.
- Do **not** touch `discoveryAllows` (`internal/agent/discovery_glue.go`).
- `internal/agent/client.go` is `MM` in git — stage only your own hunks, never blanket-commit the path. Never `git stash`, `reset`, `checkout --`, or `clean`.

## Files

- Create `internal/agent/clef.go`
- Create `internal/agent/clef_test.go`
- Modify `internal/agent/client.go` — `NewClient`, beside the existing `typesafe` decision-only branch
- Modify `internal/agent/decider.go` — extend `resolveDecider`'s keyless check to cover the new client

## Interface introduced

`ClefClient` — holds the API key, the Cloudflare account id, and the URL model id (for example `@cf/cloudflare/clef-flash`). Implements `Decider`. Its `Chat` method returns a new sentinel `ErrClefDecisionOnly`, mirroring `ErrTypesafeDecisionOnly`.

Plus a URL-mapping helper, a body-level selector helper, and a predicate deciding whether a model id names a Workers AI decision model.

## Two traps in this part — both were caught in review and both are silent

**Trap 1: splitting the model id.** The URL model id contains **two** slashes (`@cf/cloudflare/clef-flash`) while the body wants the bare selector (`clef-flash`). Splitting on the **first** slash yields `cloudflare/clef-flash`, which Cloudflare's schema rejects (its pattern accepts only `clef` or `clef-flash`, optionally surrounded by whitespace). Take the **last** path segment — `path.Base` — not a first-slash split. This affects both the request body and the predicate that decides whether routing applies at all; getting it wrong means clef is never routed and the backend silently does not exist.

**Trap 2: the provider prefix is load-bearing.** The predicate must confirm the id names the **cloudflare-workers** provider before treating it as a decision model. A bare `clef` id with no provider is ambiguous, and a same-named model from another provider must not be hijacked. Match on the full `<provider>/<model>` form.

## Steps

1. Write the transport tests first, against an `httptest` server: URL mapping for a base with the chat suffix, for one with a trailing slash, and for an empty base (where the account id alone determines the URL); a round-trip asserting `answers` and `usage` parse and that the body carries the bare selector; that `Chat` returns the sentinel; that a cancelled context aborts before sending; that the fallback timeout constant matches the TypeSafe one (30s) so a slot does not time out differently depending on which backend it points at. Give the client a test seam for the endpoint override — reuse an existing override pattern in the package if one exists rather than adding a second mechanism. Run; they fail because the client does not exist.

2. Write `clef.go`, mirroring `TypesafeClient.DecideCtx` line for line: the same deadline fallback that never extends a caller-supplied deadline, no client-level timeout (the context governs), the same 1 MiB response read cap, and the same `newProviderStatusError` on a non-2xx. Reuse the shared vocabulary types unchanged.

3. Implement the URL mapping. Truncate the stored Workers AI base at its `/ai/` suffix and append the native run path plus the model. **When trimming a path or query, cut at the first `/`; do not reset the port** — resetting the port makes a scheme-less `host:port/path` look portless and hides the very variable that decides the request.

4. Implement the selector and the predicate per the two traps above.

5. Route in `NewClient`: for the cloudflare-workers provider with a decision model id, build `ClefClient` and return it, alongside the existing TypeSafe branch. Confirm the account id is in scope there; `NewClient` already reads one for an unrelated provider, so follow how it gets there rather than adding a second lookup.

6. Extend `resolveDecider`'s credential check to cover `ClefClient`, requiring **both** an API key and an account id.

7. **Make a missing credential visible.** A slot naming clef with no Cloudflare credential must not be an indistinguishable nil. Emit a debug line naming the slot, the configured model, and the missing piece, and — for the permission and auto-continue slots — surface the existing human-readable "model unavailable" notice rather than silently proceeding as if no judge were configured. Add a test for this; a silent nil is what this step exists to prevent.

## Checks

- `go build ./...` and `go vet ./...` clean.
- `gofmt -l` prints nothing for touched files.
- `go test ./internal/agent/ -run 'TestClef|TestDecider' -v -count=1` passes.
- `go test ./internal/agent/ -count=1` passes — no existing judge test changed.

## Review focus

Assert the selector is the **bare** name, not the URL id and not a first-slash split. A test that only round-trips through the real server would catch it, but a unit test on the selector is cheaper and pins it.

Assert the predicate requires the provider, not just the model name.

Assert the missing-credential path emits something a user could act on.

## Done when

Clef clients build, route, and round-trip; the selector and predicate are correct for the two-slash id; and a misconfigured slot is visibly misconfigured.