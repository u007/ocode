# Part 08 — Provider-agnostic model attribution

Correctness fix. Without it, clef spend and clef debug lines are recorded as TypeSafe.

## Constraints (apply to every part; duplicated here because parts do not cross-reference)

- No new module dependencies.
- Decision-only, permanently.
- Fail-open is a hard invariant.
- No silent backend fallback.
- Missing credential must be visible, not a silent nil.
- Confidence floors stay shared: 0.85 permission, 0.5 relevance.
- Never hold a lock across a network call.
- `gofmt` clean, `go vet` clean. Tests named `Test<Subject>_<Behaviour>`.
- Several files here are `MM`/`AM` in git: `internal/agent/agent.go`, `client.go`, `permission_typesafe.go`, `content_guard_typesafe.go`. Stage only your own hunks. Never `git stash`, `reset`, `checkout --`, or `clean`.

## The bug

`Decider` carries `GetProvider` and `GetModel`, but nothing in the judges uses them for attribution. Every site builds its label by concatenating the literal `typesafe/` with the model name. Once a slot can point at clef, that concatenation is **wrong** — clef's tokens are booked to TypeSafe in the usage ledger, so per-provider spend reporting is silently incorrect.

Verified sites, all of the form `RecordSideUsage(..., "typesafe/"+client.Model)`:

- `internal/agent/autocontinue_typesafe.go` (line 131)
- `internal/agent/network_guard_typesafe.go` (line 377)
- `internal/agent/relevance_typesafe.go` (line 65)
- `internal/agent/permission_typesafe.go` (line 247)
- `internal/agent/content_guard_typesafe.go` (line 490)

(Line numbers are from the working tree at plan time; several of these files are concurrently modified, so locate by content, not by number.)

A separate class of hardcoding that the seam does not fix by itself:

- `internal/agent/permission_typesafe.go` — the `isTypesafeModel` predicate tests the `typesafe/` prefix directly.
- `internal/tui/commands.go` — the judge's "kind" line is chosen by testing the `typesafe/` prefix, so a clef-backed judge shows no kind at all.
- `internal/agent/models_registry.go` — synthesises a `typesafe/`-prefixed id when enumerating models, so a configured clef judge may not appear in the model's list.
- `internal/agent/autocontinue_typesafe.go` — two debug strings and one model label built from the same literal.

## Steps

1. Add a small helper that builds a fully-qualified label from any `Decider` as provider, slash, model, with `GetProvider` taking precedence over a literal. Put it next to the other shared judge helpers.
2. Replace all five `RecordSideUsage` call sites to use it. This is the part that fixes the ledger, so do it even though the label is only a string today — the bug is that the ledger attributes spend by model id.
3. Replace the two debug strings and the model label in the auto-continue judge.
4. Rework `isTypesafeModel` into a provider-agnostic predicate — one that recognises any configured decision backend, not just the `typesafe/` prefix — and update its callers.
5. Update the TUI judge-kind display to recognise any decision model, so a clef-backed judge is labelled rather than blank.
6. Update the model registry's id enumeration so a clef judge appears alongside the TypeSafe one.
7. Add a test asserting the label for a clef client is provider-qualified and does **not** start with `typesafe/`. This is the regression guard; without it the concatenation creeps back.

## Checks

- `go build ./...` and `go vet ./...` clean. Note `internal/tui` is currently mid-refactor by another session and may not build; if so, report it rather than working around it.
- `gofmt -l` prints nothing for files you touched.
- Grep finds no remaining `RecordSideUsage` call concatenating a literal provider prefix.
- `go test ./internal/agent/ -count=1` passes.

## Review focus

Confirm the ledger label is derived from the client, not from the slot's configured string — the configured id may be an alias while the client reports a resolved version, and conflating them reintroduces the bug.

Confirm the TUI label change does not alter any permission decision. It is display only.

## Done when

A clef-backed judge books its tokens to clef, appears in model enumeration, and is labelled in the TUI — with a test that fails if the provider prefix is hardcoded again.