---
model_id: deepseek-v4.1-flash
model_version: "4.1-2026-10-21"
evaluated_via: opencode-go
evaluated_on: 2026-10-21
stack: docx
stack_corpus_rev: 1
threshold: 0.9
---

# Scorecard — deepseek-v4.1-flash on docx

> Valid ONLY for `deepseek-v4.1-flash` @ `4.1-2026-10-21`. A version bump invalidates
> this scorecard — re-benchmark.

Answers: `docx/answers/deepseek-v4.1-flash.md` (closed-book, audited zero tool calls).
Graded against `docx/questions.yaml` corpus_rev 1 (python-docx facts verified on 1.2.0;
the disputed API facts below were re-checked against the installed python-docx 1.2.0
source on 2026-10-21).
Grading rule applied (same as the previous baseline): each `point` is awarded whole.
A `point` whose core concept is present earns full credit (minor missing sub-items are
noted); a `point` whose core is stated with a wrong API fact, or satisfied only by a
cover-up (hidden/white text, zero-height rows, white boxes), earns 0. A wrong API fact
that sits beside a correct core is noted but not deducted. Equivalent correct approaches
in another library earn the point.

## Provenance

- Re-benchmark: opencode-go silently updated the model behind `deepseek-v4.1-flash`,
  so the 2026-09-28 baseline (`model_version: "4.1"`) no longer describes it.
- Each of the 29 questions was a fresh headless `ocode run -m opencode-go/deepseek-v4.1-flash
  -effort med` session (`bin/ocode` built 2026-10-20) under ocode's real agent system prompt.
  The previous baseline sent the whole sheet in one session; this run is one session per
  question, so answers are longer and self-contained.
- Fake HOME (no global CLAUDE.md/AGENTS.md, memory or persisted roots), every builtin
  tool denied, the existing `*-tuning-deepseek-v4.1-flash` Kaizen skills (and
  `conduct-tuning-deepseek-v4-flash`) disabled: a no-skill baseline. The generic
  `[ocode:provider]` DeepSeek block and skill-catalogue digest were present.
- cwd an empty dir `work`, stdin `/dev/null`. Every session's sqlite was audited:
  1 user + 1 assistant message, zero tool calls.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| docx-model-01 | docx-model | 3 | 2 | 2 | 1.00 | |
| docx-model-02 | docx-model, table-locate | 3 | 2 | 2 | 1.00 | maps match offsets onto w:t nodes, replacement in the start run (keeps its formatting) |
| docx-model-03 | docx-model, table-relayout | 2 | 2 | 2 | 1.00 | |
| docx-locate-01 | table-locate | 3 | 2 | 2 | 1.00 | also covers header/footer tables, nested tables |
| docx-locate-02 | table-locate, docx-model | 2 | 2 | 2 | 1.00 | vMerge repeating the top cell not mentioned (minor) |
| docx-rowdel-01 | row-delete | 3 | 2 | 1 | 0.50 | remove `row._tr` correct; second point missed: never says a dependent Total must be updated, and states "Nothing else ... needs cleanup for a plain row removal" (the opposite) |
| docx-rowdel-02 | row-delete, table-locate | 2 | 2 | 2 | 1.00 | promotes next cell to restart, moves anchor content, drops vMerge for a lone cell |
| docx-rowdel-03 | row-delete, cell-edit | 2 | 2 | 2 | 1.00 | |
| docx-rowdel-04 | row-delete, verify-safety | 2 | 2 | 2 | 1.00 | scans every XML part |
| docx-cell-01 | cell-edit | 3 | 2 | 2 | 1.00 | judgement call: fix is `cell.text` then re-setting bold/9 pt/white by hand rather than editing the existing run or copying rPr (font name and other run props still lost); core "restore the lost run formatting" present, and the run-preserving edit appears in locate-01/relayout-01 |
| docx-cell-02 | cell-edit, table-locate | 2 | 2 | 2 | 1.00 | number format/alignment not mentioned (minor) |
| docx-cell-03 | cell-edit, verify-safety | 2 | 2 | 2 | 1.00 | wrong API fact beside a correct core: says runs inside `w:ins` are "ordinary w:t, so python-docx reads it"; 1.2.0 `paragraph.text`/`.runs` skip them (probe: text `'A'`, 1 run). Not deducted: bypass-tracking and tracked/clean decision are right |
| docx-relayout-01 | table-relayout | 3 | 3 | 3 | 1.00 | wrong API fact beside a correct core: says add_row copies "no ... widths"; 1.2.0 sets each new `tc.width = gridCol.w`. Not deducted (append-at-end + no formatting copied is right) |
| docx-relayout-02 | table-relayout | 3 | 3 | 2 | 0.67 | point 3 stated with a wrong API fact: "`table.columns[i].width` ... writes gridCol.w and each cell's tcW" — the 1.2.0 setter writes only `_gridCol.w`, leaving every tcW stale (the exact bug its own relayout-03 answer describes). Also says add_column "does not set w:tcW" (it sets `tc.width = width`; noted, point 1 core right) |
| docx-relayout-03 | table-relayout, docx-model | 2 | 2 | 2 | 1.00 | |
| docx-tblins-01 | table-insert | 3 | 2 | 2 | 1.00 | |
| docx-tblins-02 | table-insert | 2 | 2 | 2 | 1.00 | |
| docx-tblins-03 | table-insert, table-relayout | 2 | 2 | 1 | 0.50 | default width correct; second point missed: copies widths only via `n_col.width = s_col.width  # sets gridCol AND every cell's tcW` (same wrong `_Column.width` fact), and never reuses the source table's style or header formatting |
| docx-imgrep-01 | image-replace, docx-model | 3 | 3 | 2 | 0.67 | model + new rel/repoint correct (overwrite bytes only when the media is used by the logo alone); never removes the old rel/media — "leave every other blip and relationship alone" keeps the old image in the package |
| docx-imgrep-02 | image-replace | 2 | 2 | 2 | 1.00 | |
| docx-imgrep-03 | image-replace, table-locate | 2 | 2 | 2 | 1.00 | header part + anchored + VML covered; Option A is again `part._blob = new_bytes` ("every header references the same part") without checking body references — contradicts its own imgrep-02 (minor here) |
| docx-imgins-01 | image-insert | 3 | 2 | 2 | 1.00 | |
| docx-imgins-02 | image-insert, table-relayout | 2 | 2 | 2 | 1.00 | |
| docx-legacy-01 | legacy-doc | 3 | 2 | 2 | 1.00 | |
| docx-legacy-02 | legacy-doc, verify-safety | 2 | 2 | 2 | 1.00 | |
| docx-legacy-03 | legacy-doc | 1 | 2 | 2 | 1.00 | |
| docx-verify-01 | verify-safety | 3 | 2 | 2 | 1.00 | |
| docx-verify-02 | verify-safety, docx-model | 2 | 2 | 2 | 1.00 | duplicate wp:docPr ids not mentioned; writes `w:tblLayout w:val` (attribute is `w:type`) (minor) |
| docx-verify-03 | verify-safety, image-replace | 2 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| docx-model | 0.94 | 7 | ok | omit (strong) |
| table-locate | 1.00 | 6 | ok | omit (strong) |
| row-delete | 0.83 | 4 | ok | **derive** |
| cell-edit | 1.00 | 4 | ok | omit (strong) |
| table-relayout | 0.86 | 6 | ok | **derive** |
| table-insert | 0.86 | 3 | low-n | derive (mark low-n) |
| image-replace | 0.89 | 4 | ok | **derive** |
| image-insert | 1.00 | 2 | low-n | omit (strong) |
| legacy-doc | 1.00 | 3 | low-n | omit (strong) |
| verify-safety | 1.00 | 6 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

Arithmetic: docx-model 16/17; row-delete 7.5/9; table-relayout 12/14;
table-insert 6/7; image-replace 8/9; all others lose nothing.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 64.5 / 69 = 93.5%
```

(Losses: rowdel-01 −1.5, relayout-02 −1.0, tblins-03 −1.0, imgrep-01 −1.0.)

## Change vs previous baseline

Previous: `model_version: "4.1"`, evaluated 2026-09-28, whole sheet in one session,
94.9% (65.5/69). New: `"4.1-2026-10-21"`, one session per question, 93.5% (64.5/69).

| tag | old (4.1) | new (4.1-2026-10-21) | change |
|-----|----------:|---------------------:|--------|
| docx-model | 0.94 | 0.94 | = |
| table-locate | 1.00 | 1.00 | = |
| row-delete | 0.83 | 0.83 | = (same rowdel-01 miss: no Total update) |
| cell-edit | 1.00 | 1.00 | = |
| table-relayout | 0.93 | 0.86 | ↓ now derived (relayout-02 wrong `_Column.width` fact; relayout-01 now full) |
| table-insert | 1.00 | 0.86 | ↓ now derived, low-n (tblins-03) |
| image-replace | 0.89 | 0.89 | = (same imgrep-01 miss: old rel/media kept) |
| image-insert | 1.00 | 1.00 | = |
| legacy-doc | 1.00 | 1.00 | = |
| verify-safety | 1.00 | 1.00 | = |

New failure class: the previous baseline stated no wrong python-docx API facts; this
model asserts that `_Column.width` also writes each cell's `tcW` (twice: relayout-02,
tblins-03), and misstates what `add_row`/`add_column` write and what python-docx shows
inside `w:ins`.

The previous scorecard's live behavioural probe (2026-09-28, all 8 tasks PASS) was run
against the earlier `4.1` model and is not carried forward; it has not been repeated for
`4.1-2026-10-21`.

## Derivation targets

Tags below threshold (`< 0.9`): **row-delete, table-relayout, table-insert (low-n),
image-replace** → feed into `derived/docx.deepseek-v4.1-flash.SKILL.md`.

Above threshold, not derived: docx-model (0.94, lost only via imgrep-01, covered under
image-replace).

### row-delete (0.83)
- Missing (rowdel-01): a Total (or any value) derived from the deleted row is not
  updated by removal and must be recomputed and rewritten; the answer says nothing
  else needs cleanup.

### table-relayout (0.86)
- Wrong API fact (relayout-02): `table.columns[i].width = w` writes only
  `w:gridCol/@w:w`; every cell's `w:tcW` keeps its old value, and Word lays out from
  `tcW`. Rebalancing must set `cell.width` for every cell in the column too.
- Wrong API facts noted (relayout-01, relayout-02): `add_row()` and `add_column()` do
  write `tcW` on the new cells (from the gridCol / the given width).

### table-insert (0.86, low-n)
- Missed (tblins-03): matching an existing table means reusing its table style and
  header formatting as well as its widths, and widths copied through `_Column.width`
  never reach the cells' `tcW`.

### image-replace (0.89)
- Missing (imgrep-01): never removes the old rel after repointing `r:embed`, so the old
  image stays in `word/media/`.
- Unsafe option (imgrep-03): `part._blob = new_bytes` offered as the first way to
  replace a header logo although image parts are shared.
