---
model_id: deepseek-v4.1-flash
model_version: "4.1-2026-10-21"
evaluated_via: opencode-go
evaluated_on: 2026-10-21
stack: pptx
stack_corpus_rev: 1
threshold: 0.9
validation: with-skill
---

# Scorecard — deepseek-v4.1-flash on pptx (with-skill validation, iteration 2, threshold 0.9)

> Valid ONLY for `deepseek-v4.1-flash` @ `4.1-2026-10-21`. A version bump invalidates
> this scorecard — re-benchmark.

Answers: `pptx/answers/deepseek-v4.1-flash.with-skill.md` (iteration 2; closed-book, one
fresh `ocode run` session per question with the body of
`derived/pptx.deepseek-v4.1-flash.SKILL.md` prepended as "Active guidance"; all tools
denied; every session sqlite audited, zero tool calls).
Baseline: `pptx/scores/deepseek-v4.1-flash.md` (**90.8%**, corrected — imgrep-03 full = 2).
Iteration 1 is preserved alongside: `scores/deepseek-v4.1-flash.iter1.with-skill.md`
(answers `answers/deepseek-v4.1-flash.iter1.with-skill.md`, 97.7%).

Skill changes since iteration 1:
- the verify-safety section and its digest item were removed (verify-safety is 0.92 in the
  corrected baseline, so it is no longer a target);
- the slide-size bullet now states that python-pptx's default `Presentation()` template is
  4:3 (9144000 × 6858000) and says to read the deck, after iteration 1's 16:9 bullet led
  pptx-model-02 to call the default template 16:9.

Graded against `pptx/questions.yaml` corpus_rev 1 (python-pptx facts verified on 1.0.2,
with a scratch script for the contested ones). Grading rule (same as the baseline and
iteration 1): a `point` whose core concept is present earns full credit (minor missing
sub-items are noted); a `point` stated with a wrong API/tool fact, or satisfied only by a
cover-up, earns 0. Snippet-level code defects that are not stated as facts are noted but
not zeroed (the iteration-1 tblins-02 precedent). Equivalent correct approaches earn the
point. Points are whole; a `partial` only applies when no point is matched.
pptx-locate-01 point 2 requires BOTH header-based table selection AND a unique-row assert.
Target tags (below 0.9 in the corrected baseline): **table-locate (low-n), table-relayout,
image-replace**.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| pptx-model-01 | pptx-model | 3 | 2 | 2 | 1.00 | correct default style GUID shown |
| pptx-model-02 | pptx-model | 2 | 2 | 2 | 1.00 | EMU 914400/in, 12700/pt, 360000/cm; 16:9 = 12192000 × 6858000 (13.333 × 7.5 in); "python-pptx's own `Presentation()` template is 4:3 — 9,144,000 × 6,858,000"; read `prs.slide_width/height`. **iter1 regression fixed** |
| pptx-model-03 | pptx-model, image-replace | 2 | 2 | 2 | 1.00 | master/layout/background; global scope, per-slide override, showMasterSp, multi-master |
| pptx-locate-01 | table-locate | 3 | 2 | 2 | 1.00 | recursive group walk + has_table; table chosen by first-row header list with `assert len(candidates) == 1`; label column from header; exact stripped match, `assert len(hits) == 1`; rejects `next(...)` |
| pptx-locate-02 | table-locate, pptx-model | 2 | 2 | 2 | 1.00 | one a:tc per gridCol, gridSpan/rowSpan origin, hMerge/vMerge covered; write origin only; adjust rowSpan/gridSpan on insert/delete through a merge |
| pptx-rowdel-01 | row-delete | 3 | 2 | 2 | 1.00 | no remove/delete/add_row API; `tr.getparent().remove(tr)`; frame height + Total + banding |
| pptx-rowdel-02 | row-delete, table-relayout | 3 | 2 | 2 | 1.00 | |
| pptx-rowdel-03 | row-delete, cell-edit | 2 | 2 | 2 | 1.00 | |
| pptx-rowdel-04 | row-delete, verify-safety | 2 | 2 | 2 | 1.00 | judgement: text stays in the model (search, screen readers, extraction); `h` is a preferred height that snaps back. Removes the a:tr but no longer says to re-open and check the text is absent from every slide part (iter1 had it; its source section was removed). Credited as minor, as in the baseline |
| pptx-cell-01 | cell-edit | 3 | 2 | 2 | 1.00 | |
| pptx-cell-02 | cell-edit, table-relayout | 2 | 2 | 2 | 1.00 | |
| pptx-cell-03 | cell-edit | 2 | 2 | 2 | 1.00 | chart cached values, speaker notes, summary tables; write via `runs[0].text` |
| pptx-relayout-01 | table-relayout | 3 | 3 | 3 | 1.00 | header-picked table, unique-row asserts; deepcopy Service G `a:tr` + `total._tr.addprevious`; fill via `runs[0]`, strip extra runs; recompute Total; `gf.height = sum(...)`; bottom vs `prs.slide_height` + shapes below; re-alternate explicit banding; states no `add_row()`. **Judgement:** the Total snippet is broken — `table.rows[i_new:]` raises TypeError (`_RowCollection` cannot be sliced, verified), and the range would sum only the new row. Not stated as a fact, so not zeroed (see close calls) |
| pptx-relayout-02 | table-relayout | 3 | 3 | 3 | 1.00 | gridCol + tc in every row, stop on spans; deepcopy the same row's Item cell; keep grid total constant, `gf.width = sum`; vertical re-sync. **Judgement:** the snippet then sets `table.cell(0, i).text_frame.text = "SKU"`, which drops the copied run's rPr (verified: bold → None) while the tcPr fill survives, contradicting its own cell-01 answer. Concept stated in prose; not zeroed (see close calls) |
| pptx-tblins-01 | table-insert | 3 | 2 | 2 | 1.00 | |
| pptx-tblins-02 | table-insert | 2 | 2 | 2 | 1.00 | correct GUID `{5C22544A-7EE6-4342-B048-85BDC9FD1C3A}` = Medium Style 2 – Accent 1, firstRow/bandRow; copy tblPr, tcPr, pPr, rPr |
| pptx-tblins-03 | table-insert, pptx-model | 1 | 2 | 0 | 0.00 | wrong fact: "`SlidePlaceholder.insert_table()` is the placeholder-aware API", and the code picks an OBJECT/BODY placeholder. In 1.0.2 only `TablePlaceholder` has `insert_table`; a layout-1 content placeholder is a `SlidePlaceholder` with no `insert_table` (verified, AttributeError) — exactly the rubric's trap → point 1 = 0. Point 2 absent: calls add-at-bbox an anti-pattern and never says to remove the empty placeholder. Fallback also wrong: `clone_placeholder` returns `None` (verified) and does not raise ValueError. The baseline's denial of `insert_table` has flipped into an overclaim |
| pptx-imgrep-01 | image-replace | 3 | 3 | 3 | 1.00 | repoint `a:blip r:embed` via `get_or_add_image_part` (tuple order correct); add_picture + `addprevious` + remove for the splice; aspect/stretch caveat; drop the old rel only after an `r:embed`/`r:link` check. (Summary line "old media silently survives" is loose but body and code are correct) |
| pptx-imgrep-02 | image-replace | 2 | 2 | 2 | 1.00 | |
| pptx-imgrep-03 | image-replace, verify-safety | 2 | 2 | 2 | 1.00 | `drop_rel` drops when count < 2; `_rel_ref_count` counts only `//@r:id` → image rel always 0 → always dropped (matches 1.0.2); same rId reused via `relate_to`; check `//@r:embed`/`//@r:link` first |
| pptx-imgins-01 | image-insert | 3 | 2 | 2 | 1.00 | |
| pptx-imgins-02 | image-insert | 2 | 2 | 2 | 1.00 | |
| pptx-legacy-01 | legacy-ppt | 3 | 2 | 2 | 1.00 | OLE2 magic bytes, not a zip, renaming doesn't help; soffice → pptx; back to .ppt if mandatory. (Side claim, hedged, ungraded: "typically `BadZipFile` … depending on version" — 1.0.2 raises `PackageNotFoundError`, `opc/serialized.py:144`) |
| pptx-legacy-02 | legacy-ppt, verify-safety | 2 | 2 | 2 | 1.00 | judgement as baseline: tells the user it is a lossy working copy, keeps the original; does not say to return `.ppt` if required — minor |
| pptx-verify-01 | verify-safety | 3 | 2 | 2 | 1.00 | backup, save to a new candidate path, re-open from disk, exact asserts + unchanged remainder, render via soffice, diff vs backup (then promotes over the original with the backup kept — concept present) |
| pptx-verify-02 | verify-safety, pptx-model | 2 | 2 | 2 | 1.00 | gridSpan/gridCol mismatch, txBody without a:p, merges; dup cNvPr ids, dangling rIds, schema order; python-pptx does no schema validation |
| pptx-verify-03 | verify-safety, image-replace | 2 | 2 | 2 | 1.00 | rel + media survive; hash ppt/media; drop after r:embed check. (Side: attributes `_rel_ref_count` to `Relationships`; it is on `Part` — mechanism stated correctly, minor) |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | Σ(norm×w) / Σw | n | trust | role |
|-----|---------:|---------------:|--:|-------|------|
| pptx-model | 0.92 | 11 / 12 | 6 | ok | non-target |
| table-locate | 1.00 | 5 / 5 | 2 | low-n | **target — pass** |
| row-delete | 1.00 | 10 / 10 | 4 | ok | non-target |
| cell-edit | 1.00 | 9 / 9 | 4 | ok | non-target |
| table-relayout | 1.00 | 11 / 11 | 4 | ok | **target — pass** (see close calls) |
| table-insert | 0.83 | 5 / 6 | 3 | low-n | non-target (below 0.9, see drift) |
| image-replace | 1.00 | 11 / 11 | 5 | ok | **target — pass** |
| image-insert | 1.00 | 5 / 5 | 2 | low-n | non-target |
| legacy-ppt | 1.00 | 5 / 5 | 2 | low-n | non-target |
| verify-safety | 1.00 | 13 / 13 | 6 | ok | non-target (section removed) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight)
            = (65 − 1 [tblins-03: 1×1.0]) / 65
            = 64 / 65 = 98.5%
```

(Corrected baseline 59 / 65 = 90.8%; iteration 1 63.5 / 65 = 97.7%.)

## Validation result

| target tag | baseline (corrected) | iter1 | iter2 | n | iter2 pass (≥ 0.9)? |
|------------|---------------------:|------:|------:|--:|---------------------|
| table-locate | 0.70 | 1.00 | 1.00 | 2 (low-n) | **PASS** |
| table-relayout | 0.82 | 1.00 | 1.00 | 4 | **PASS** (close call, below) |
| image-replace | 0.82 | 1.00 | 1.00 | 5 | **PASS** |

Validation: **PASS** — every target tag reaches 0.9. The misbeliefs the skill names stay
corrected: header-based table pick + unique-row assert (locate-01, also rowdel-01,
relayout-01), no `table.add_row()` anywhere, Total + frame height + bottom check after an
insert (relayout-01), drop_rel `//@r:id` mechanism (imgrep-03), and a left-behind image rel
keeps the media (imgrep-01, imgrep-02, verify-03).

**4:3 regression: gone.** pptx-model-02 now states the 16:9 size correctly, says
python-pptx's `Presentation()` default is 4:3 (9144000 × 6858000, verified), and says to read
the deck: 2/2 (iter1 1/2). Every echo of the new bullet (locate-01, locate-02, tblins-01,
tblins-02, legacy-02, imgins-01) is correct.

Close calls (table-relayout, Σw 11). Two snippet-level defects were credited under the
"stated wrong fact → 0" rule because neither is stated as a fact:
- relayout-01: the Total recompute slices `table.rows` (TypeError) and sums only the new row;
- relayout-02: the new header cell's text is set with `text_frame.text`, dropping the copied
  bold/colour rPr.

| strict call applied | table-relayout | result |
|---|---:|---|
| neither (as graded) | 11 / 11 = 1.00 | PASS |
| one of the two | 10 / 11 = 0.91 | PASS |
| both | 9 / 11 = 0.82 | FAIL |

The pass survives one strict call, not both.

New wrong facts with the skill active:
- None skill-induced. The skill says nothing about placeholders, slicing or `text_frame.text`.
- pptx-tblins-03 (not skill-induced): `SlidePlaceholder.insert_table()` on an OBJECT/BODY
  placeholder (only `TablePlaceholder` has it), and `clone_placeholder` "returns the
  placeholder / raises ValueError" (it returns `None`). The baseline denied
  `insert_table` existed; iter1 repeated the denial; iter2 overclaims instead.
- Ungraded side claims: legacy-01 `BadZipFile` (hedged; actual `PackageNotFoundError`),
  verify-03 `Relationships._rel_ref_count` (it is `Part._rel_ref_count`).

Non-target drift (single sample, not acted on; baseline → iter1 → iter2): pptx-model 0.96 → 0.875 → **0.92**,
table-insert (low-n) 0.92 → 0.92 → **0.83** (tblins-03, weight 1, above), verify-safety
0.92 → 1.00 → **1.00** (held without its section), row-delete, cell-edit, image-insert,
legacy-ppt all 1.00.

Observation (ungraded): skill boilerplate (frame-height sync, drop_rel `//@r:id`, 4:3
default) still leaks into answers where it was not asked (model-03, tblins-01, tblins-03,
legacy-01, legacy-02, relayout-02). It is harmless for the score but makes answers longer.
