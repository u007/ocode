# Part 09 — Documentation

Update the documentation this change makes false. **No behaviour change.**

## Constraints (apply to every part; duplicated here because parts do not cross-reference)

- No new module dependencies.
- Decision-only, permanently.
- Fail-open is a hard invariant.
- No silent backend fallback.
- Missing credential must be visible, not a silent nil.
- `permissions.auto.model` and `auto_continue_model` are not refactored or aliased. `permissions.auto.min_confidence` keeps governing the opaque relaxation.
- Confidence floors stay shared: 0.85 permission, 0.5 relevance. The existing rule — never reuse the permission floor for a lower-stakes judge — stays and now applies within each backend too.
- Never hold a lock across a network call.
- `gofmt` clean, `go vet` clean on any Go file touched.
- **Read a target path before overwriting it.** `docs/` and `TODO.md` have many concurrent modifications; use `edit`, not a whole-file write, for existing files.
- Never `git stash`, `reset`, `checkout --`, or `clean`.

## Why this part is not optional

`CLAUDE.md` is loaded unconditionally at session start by `internal/agent/context.go::LoadContext`. A stale bullet there misleads every future session and every agent that reads it. Its `typesafe` bullet currently says the provider is **decision-only** and enumerates its consumers; both statements become incomplete once a second decision backend exists and each judge can select its own model.

## Files

- Modify `CLAUDE.md` — the `typesafe` bullet in the Tech Stack section
- Modify `docs/concepts/discovery-typesafe-judge.md`
- Modify `docs/concepts/doc-search-relevance-judge.md`
- Modify `docs/concepts/auto-permission-enforced-categories.md`
- Modify `docs/concepts/server-auto-continue.md`
- Modify `docs/index.md` — **only via the `context` agent**; it is auto-managed
- Modify `TODO.md` — the deferral entries

## Claim-by-claim: what goes stale, and why

| File | Stale claim |
| --- | --- |
| `CLAUDE.md` | "decision-only" for one provider with a fixed consumer list. Must become: two decision backends behind one interface; neither routable through chat, compaction, small-model or interpreter-effects paths; per-judge model selection defaulting to the incumbent; the shared-floors decision **and its falsification condition**; the per-judge floor rule retained. |
| `docs/concepts/discovery-typesafe-judge.md` | Names the discovery judge's model as a hardcoded constant that this change deletes. |
| `docs/concepts/doc-search-relevance-judge.md` | States the judge is fed by the shared discovery client. It now has its own config key and no longer follows `discovery`. |
| `docs/concepts/auto-permission-enforced-categories.md` | Describes the permission judge and its floor. Must add the state budget and the fail-open-to-human-ask behaviour for oversized state, plus the projected-state approach: keep security-relevant fields in full, project bulky content fields. |
| `docs/concepts/server-auto-continue.md` | Names the auto-continue judge's model source. Must note that its key now accepts any decision model id, and that model attribution is now provider-qualified rather than assuming TypeSafe. |

## Steps

1. Grep the docs for the three deleted constant names and fix every hit.
2. Rewrite the `CLAUDE.md` bullet. Keep it terse — that file is a briefing, not a reference. It must be true for both backends and must carry the falsification condition so a future session knows shared floors are an evidenced choice with a stated failure mode, not a permanent.
3. Update the four concept pages for the model source, the now-independent slots, the state budget, and the attribution change.
4. **Re-derive every `file.go:NNN` anchor on every page you touch**, by printing the actual source line for each. Parts 01, 02, 05 and 08 add and remove lines in `decider.go`, `permission_typesafe.go`, `relevance_typesafe.go` and the guard files, which moves anchors on all of these pages simultaneously. Check **both endpoints** of any range. Do not trust a sub-agent's list of corrected anchors — the mechanical sweep is the check.
5. **Do not add a Go test that reads `CLAUDE.md` or `docs/concepts/*.md`.** An earlier draft of this plan included one; on review it was dropped as brittle — it couples a unit test to prose that is edited for wording reasons, and those files sit in a dirty shared tree. If the claims need pinning, pin them in review, not in the build.
6. Route all `docs/` edits through the `context` sub-agent, passing paths **without** a `docs/` prefix (`doc_write` prepends it). `CLAUDE.md` is not bundle content and is edited directly. If `context` cannot run, stop and report rather than writing bundle pages yourself.
7. Add the `TODO.md` entries: the live eval has not been run; the UI plan (seven per-judge model pickers, TUI and web) is unwritten; and any premise left unresolved about whether the incumbent truncates an oversized state or rejects it.

## Checks

- Grep finds no deleted constant names anywhere in `CLAUDE.md` or `docs/`.
- Every anchor on every touched page prints the correct source line.
- `docs/index.md` is updated only by the `context` agent.
- `TODO.md` entries exist and were reported to the user.

## Review focus

Confirm the `CLAUDE.md` bullet is true for **both** backends, not just extended with a mention of the new one.

Confirm the falsification condition survived into `CLAUDE.md` — that is the one line that prevents a future session from treating shared floors as settled fact.

Confirm no anchor was adjusted without printing the line it points at.

## Done when

`CLAUDE.md` and the four concept pages describe shipped behaviour, `docs/index.md` lists the spec and plan, the anchors are verified, and the deferrals are in `TODO.md` and reported.