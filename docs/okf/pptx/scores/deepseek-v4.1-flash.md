---
model_id: deepseek-v4.1-flash
model_version: "4.1-2026-10-21"
evaluated_via: opencode-go
evaluated_on: 2026-10-21
stack: pptx
stack_corpus_rev: 1
threshold: 0.9
---

# Scorecard — deepseek-v4.1-flash on pptx

> Valid ONLY for `deepseek-v4.1-flash` @ `4.1-2026-10-21`. A version bump invalidates
> this scorecard — re-benchmark. This replaces the `4.1` baseline of 2026-09-28:
> opencode-go silently swapped the model served under this id.

Answers: `pptx/answers/deepseek-v4.1-flash.md` (closed-book, audited zero tool calls).
Graded against `pptx/questions.yaml` corpus_rev 1 (python-pptx facts verified on 1.0.2).
Grading rule (same as the previous pptx scorecard): a `point` whose core concept is
present earns full credit (minor missing sub-items are noted); a `point` stated with a
wrong API/tool fact, or satisfied only by a cover-up (white box, hidden shape, white
text), earns 0. Equivalent correct approaches earn the point. Points are whole (no
fractional awards); a `partial` only applies when no point is matched.

## Provenance

From the answers-file header: each of the 27 questions in `_prompts/pptx.md` was asked
in its **own fresh** headless `ocode run -m opencode-go/deepseek-v4.1-flash -effort med`
session (`bin/ocode` built 2026-10-20) under ocode's real agent system prompt. Fake HOME
(no global CLAUDE.md/AGENTS.md, no memory), every builtin tool denied, and the five
`*-tuning-deepseek-v4.1-flash` Kaizen skills plus `conduct-tuning-deepseek-v4-flash`
disabled — a pre-sweep probe confirmed no tools and no Model-Specific Directives block.
The generic `[ocode:provider]` DeepSeek output-discipline block and the bundled
skill-catalogue digest were still present. cwd an empty dir `work`, stdin `/dev/null`.
Every session sqlite was audited afterwards: 1 user + 1 assistant message, zero tool calls.

Difference from the previous baseline: the 2026-09-28 run sent the whole question sheet
in ONE session; this run is one session per question, so no answer can lean on another.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| pptx-model-01 | pptx-model | 3 | 2 | 2 | 1.00 | |
| pptx-model-02 | pptx-model | 2 | 2 | 2 | 1.00 | judgement: EMU and 12192000 × 6858000 correct; flags python-pptx's 4:3 default template instead of saying "read prs.slide_width" (the don't-assume concept is present) |
| pptx-model-03 | pptx-model, image-replace | 2 | 2 | 2 | 1.00 | |
| pptx-locate-01 | table-locate | 3 | 2 | 1 | 0.50 | has_table → .table, group recursion shown. Uniqueness point missed: `next(s.table for s in slide.shapes if has_table)` takes the FIRST table ("loop over all" only if several, no header check); `next(... any(cell == target))` takes the first matching row, no unique-match assert |
| pptx-locate-02 | table-locate, pptx-model | 2 | 2 | 2 | 1.00 | |
| pptx-rowdel-01 | row-delete | 3 | 2 | 2 | 1.00 | `table._tbl.remove(rows[i]._tr)`; frame-height follow-up present, totals not mentioned (minor) |
| pptx-rowdel-02 | row-delete, table-relayout | 3 | 2 | 2 | 1.00 | |
| pptx-rowdel-03 | row-delete, cell-edit | 2 | 2 | 2 | 1.00 | |
| pptx-rowdel-04 | row-delete, verify-safety | 2 | 2 | 2 | 1.00 | judgement: removes the `a:tr`, but never says to re-open and check the text is absent from every slide part (minor, consistent with previous grading) |
| pptx-cell-01 | cell-edit | 3 | 2 | 2 | 1.00 | |
| pptx-cell-02 | cell-edit, table-relayout | 2 | 2 | 2 | 1.00 | PowerPoint-UI-centric but both concepts present |
| pptx-cell-03 | cell-edit | 2 | 2 | 2 | 1.00 | speaker notes / chart workbook / keep number format not named (minor) |
| pptx-relayout-01 | table-relayout | 3 | 3 | 1 | 0.33 | deepcopy Service G `a:tr` + `addnext`, fills via existing first run — correct. Total never recomputed, frame height never set (point 2 = 0, judgement: 2 of its 3 parts absent); no bottom-vs-slide/neighbour or banding check (point 3 = 0). Also invents `table.add_row()` ("appends at the bottom", offered as a "weaker alternative") — no such API in 1.0.2 |
| pptx-relayout-02 | table-relayout | 3 | 3 | 3 | 1.00 | judgement: says to read `p:sldSz` and rebalance widths (correct), but cites the 16:9 width as "13,333,333 EMU" (wrong; 12192000) as an aside |
| pptx-tblins-01 | table-insert | 3 | 2 | 2 | 1.00 | |
| pptx-tblins-02 | table-insert | 2 | 2 | 2 | 1.00 | |
| pptx-tblins-03 | table-insert, pptx-model | 1 | 2 | 1 | 0.50 | wrong fact: "There is no `placeholder.insert_table()` API in python-pptx" (TablePlaceholder.insert_table(rows, cols) exists). Fallback (add at placeholder bbox, remove placeholder) correct |
| pptx-imgrep-01 | image-replace | 3 | 3 | 2 | 0.67 | repoint `a:blip r:embed` via `get_or_add_image_part` keeps geometry and z-order, notes the crop — correct. Rel point stated with a wrong fact: the leftover rel is "harmless — orphaned parts aren't written out". Verified on 1.0.2: while the rel exists the part is reachable and `ppt/media/image1.png` is saved |
| pptx-imgrep-02 | image-replace | 2 | 2 | 2 | 1.00 | |
| pptx-imgrep-03 | image-replace, verify-safety | 2 | 2 | 1 | 0.50 | cause stated wrongly: `_rel_ref_count` is "a raw substring count of the rId bytes in the part's XML blob", so with two blips drop_rel "would normally … no-op". Actual: it counts `//@r:id` only, so an `r:embed` image rel always counts 0 and is always dropped. Check-r:embed-before-drop correct |
| pptx-imgins-01 | image-insert | 3 | 2 | 2 | 1.00 | |
| pptx-imgins-02 | image-insert | 2 | 2 | 2 | 1.00 | |
| pptx-legacy-01 | legacy-ppt | 3 | 2 | 2 | 1.00 | (side claims, ungraded: repeats invented `table.add_row()`; "Writing to a spanned (non-origin) cell raises" — it does not, verified) |
| pptx-legacy-02 | legacy-ppt, verify-safety | 2 | 2 | 2 | 1.00 | judgement: tells the user it was converted and keeps the original; never says to deliver `.ppt` back if requested (steers to saving `.pptx`), credited as minor — no wrong fact stated |
| pptx-verify-01 | verify-safety | 3 | 2 | 2 | 1.00 | |
| pptx-verify-02 | verify-safety, pptx-model | 2 | 2 | 2 | 1.00 | "python-pptx doesn't validate" not named (minor) |
| pptx-verify-03 | verify-safety, image-replace | 2 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | Σ(norm×w) / Σw | n | trust | action |
|-----|---------:|---------------:|--:|-------|--------|
| pptx-model | 0.96 | 11.5 / 12 | 6 | ok | omit (strong) |
| table-locate | 0.70 | 3.5 / 5 | 2 | low-n | **derive** (mark low-n) |
| row-delete | 1.00 | 10 / 10 | 4 | ok | omit (strong) |
| cell-edit | 1.00 | 9 / 9 | 4 | ok | omit (strong) |
| table-relayout | 0.82 | 9 / 11 | 4 | ok | **derive** |
| table-insert | 0.92 | 5.5 / 6 | 3 | low-n | omit |
| image-replace | 0.82 | 9.00 / 11 | 5 | ok | **derive** |
| image-insert | 1.00 | 5 / 5 | 2 | low-n | omit (strong) |
| legacy-ppt | 1.00 | 5 / 5 | 2 | low-n | omit (strong) |
| verify-safety | 0.92 | 12.00 / 13 | 6 | ok | omit |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 59.00 / 65 = 90.8%
```

## Derivation targets

Tags below threshold (`< 0.9`): **table-locate (low-n), table-relayout, image-replace** → feed into `derived/pptx.deepseek-v4.1-flash.SKILL.md`.

Close calls: verify-safety is 0.92, just above 0.9. Zeroing legacy-02's tell-user point
(judgement above) would give legacy-ppt 0.80 and verify-safety 0.85.
table-relayout (0.82) rests on relayout-01 point 2 = 0: the cells are filled through the
existing runs, but the Total and the frame height are never updated, and the frame height
is the same failure the live probe caught. Crediting it would give 10/11 = 0.91 (no section).

### table-locate (0.70, low-n)
- Takes the first `has_table` shape and the first row whose cell matches; no header-text
  selection of the table and no assert that exactly one row matches.

### table-relayout (0.82)
- Invents `table.add_row()` (twice, in separate sessions). python-pptx 1.0.2 has no row
  add API.
- Add-row recipe stops after filling the cells: no Total recompute, no
  `graphic_frame.height = sum(row heights)`, no check of the new bottom against
  `prs.slide_height` and the shapes below, no banding re-check.
- 16:9 slide width quoted as 13,333,333 EMU (actual 12192000).

### image-replace (0.82)
- drop_rel mechanism inverted: believes it counts rId substrings and no-ops when two
  blips share the rId. Actual: counts only `//@r:id`, so image rels always drop.
- Believes an un-dropped orphan image rel is harmless because "orphaned parts aren't
  written out". Actual: the rel keeps the part reachable and it is saved in the zip.

> **Correction (2026-10-21):** pptx-imgrep-03 was first scored with full = 3; its rubric is
> two 1-point items (full = 2). Corrected: imgrep-03 0.33 → 0.50, image-replace 0.79 → 0.82,
> verify-safety 0.897 → 0.92 (no longer derived), stack 90.3% → 90.8%. Caught by the
> with-skill grader.

## Change vs previous baseline

Previous: `4.1` @ 2026-09-28, whole sheet in one session, **84.6%**. Now: **90.8%**.

| tag | 4.1 (2026-09-28) → 4.1-2026-10-21 | Δ |
|-----|:---:|---:|
| pptx-model | 0.88 → 0.96 | +0.08 |
| table-locate (low-n) | 0.50 → 0.70 | +0.20 |
| row-delete | 0.85 → 1.00 | +0.15 |
| cell-edit | 1.00 → 1.00 | 0 |
| table-relayout | 0.82 → 0.82 | 0 |
| table-insert (low-n) | 0.92 → 0.92 | 0 |
| image-replace | 0.91 → 0.82 | −0.09 |
| image-insert (low-n) | 1.00 → 1.00 | 0 |
| legacy-ppt (low-n) | 0.50 → 1.00 | +0.50 |
| verify-safety | 0.85 → 0.92 | +0.07 |

Derived set changes from {pptx-model, table-locate, row-delete, table-relayout,
legacy-ppt, verify-safety} to {table-locate, table-relayout, image-replace}. The merge-API and `.ppt`-round-trip misbeliefs are gone; the invented
`add_row()`, `insert_table()` denial and drop_rel misunderstanding persist, and a new
"orphan parts aren't written" belief appears.

## Live behavioural probe

The previous scorecard's live `.pptx` editing probe (`pptx/probe/check.py`) was run
against the earlier model behind this id. It has **not been re-run** for
`4.1-2026-10-21`; its results are not carried over.
