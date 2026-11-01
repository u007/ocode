# Part 06 — documentation

Depends on part 05 (document what shipped, not the plan). The bundle is OKF (`docs/index.md` has
`okf_version`); write pages with frontmatter like the neighbours in `docs/concepts/`, and update
`docs/index.md` and append to `docs/log.md` the way the `okf` skill prescribes. Read each target file
before writing it (shared, concurrently edited files; prefer anchored edits over whole-file writes).

1. **New** `docs/concepts/browser-run-jev-step-loop.md`: what `browser_run` is, the one-step
   diagram in words, the status table, the Jev request shape (state keys, question ids, floors), the
   permission story (sub-steps re-enter `handleToolCallWithContext`; one ask per click in normal
   mode), what the loop never does (LLM calls, navigate, retry a mutation, transcript writes), the
   `controls` action and `Settle` rule (interactive + 200 ms quiet, 5 s cap, no spinner heuristics
   and why), limits and v2 list, measurement plan. Cite fastbrowse as the reference design with its
   published numbers marked as theirs.
2. `docs/concepts/browser-agent-tools.md`: add `controls` to the action table and the guarded
   outputs table; add a short "browser_run" cross-reference paragraph; re-derive every
   `file.go:NNN` anchor on that page that the parts above shifted (print each cited line and check
   it still names what the text says).
3. `CLAUDE.md`: one bullet block under the browser section (or next to "Goal condition loop")
   with the invariants: sub-steps go through `handleToolCallWithContext`, never a direct
   `ExecuteCtx`; the loop never calls an LLM, `navigate` or `open`; a mutation is never retried;
   `slotBrowserRun` has its own floors; `data` values never enter a Jev request.
4. `TODO.md`: a "browser_run v2" block listing the spec's out-of-scope items and the measurement
   task on the kaizen webforms corpus.
5. `skills/ocode-web/SKILL.md` (and any skill that lists the browser tool's actions): mention
   `controls` and `browser_run` in one line each, if the skill enumerates actions at all.
6. `CHANGES.md`: one entry.

## Verify

Print every `\w+\.go:\d+` anchor in the touched pages and confirm each line; `grep -n okf_version
docs/index.md` still present; `docs/log.md` has the new entry at the end.
