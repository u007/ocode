---
model_id: deepseek-v4.1-flash
model_version: "4.1"
evaluated_via: opencode-go
evaluated_on: 2026-09-28
stack: pptx
stack_corpus_rev: 1
threshold: 0.9
validation: with-skill
---

# Scorecard — deepseek-v4.1-flash on pptx (with-skill validation, threshold 0.9)

> Valid ONLY for `deepseek-v4.1-flash` @ `4.1`. A version bump invalidates
> this scorecard — re-benchmark.

Answers: `pptx/answers/deepseek-v4.1-flash.with-skill.md` (closed-book with the current
`derived/pptx.deepseek-v4.1-flash.SKILL.md` prepended; audited zero tool calls).
Graded against `pptx/questions.yaml` corpus_rev 1 (python-pptx facts verified on 1.0.2).
Grading rule applied (same strictness as the baseline): rubric points are whole, fractions
only from a listed partial; a `point` whose core concept is present earns full credit
(minor missing sub-items are noted); a `point` stated with a wrong API/tool fact, or
satisfied only by a cover-up, earns 0. Equivalent correct approaches earn the point.
pptx-locate-01 point 2 requires BOTH header/content-based table selection AND a
unique-row assert.
Target tags (every baseline tag below 0.9): **pptx-model, table-locate, row-delete,
table-relayout, legacy-ppt, verify-safety**.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| pptx-model-01 | pptx-model | 3 | 2 | 2 | 1.00 | |
| pptx-model-02 | pptx-model | 2 | 2 | 2 | 1.00 | EMU 914400/in, 12700/pt; 16:9 = 12192000 × 6858000, python-pptx default 4:3; "never assume the size — read prs.slide_width / prs.slide_height" now stated |
| pptx-model-03 | pptx-model, image-replace | 2 | 2 | 2 | 1.00 | |
| pptx-locate-01 | table-locate | 3 | 2 | 2 | 1.00 | has_table + recursion into groups; picks the table by its first-row header text ("don't just take tables[0]"); exact stripped match; collects hits and asserts exactly one |
| pptx-locate-02 | table-locate, pptx-model | 2 | 2 | 2 | 1.00 | one a:tc per gridCol, gridSpan/rowSpan + hMerge/vMerge; is_merge_origin/is_spanned/span_*, merge()/split() correct; write to origin, fix spans on insert/delete |
| pptx-rowdel-01 | row-delete | 3 | 2 | 2 | 1.00 | no add/delete API, `tbl.remove(tbl.tr_lst[i])`; recompute totals + `graphic_frame.height = sum(row.height …)`; verify removed text absent |
| pptx-rowdel-02 | row-delete, table-relayout | 3 | 2 | 2 | 1.00 | |
| pptx-rowdel-03 | row-delete, cell-edit | 2 | 2 | 2 | 1.00 | style bandRow vs per-cell tcPr fills; re-apply explicit fills after the delete |
| pptx-rowdel-04 | row-delete, verify-safety | 2 | 2 | 2 | 1.00 | remove a:tr (+ totals, frame height); no "re-open and verify absence in every slide part" here (minor, as baseline) |
| pptx-cell-01 | cell-edit | 3 | 2 | 2 | 1.00 | |
| pptx-cell-02 | cell-edit, table-relayout | 2 | 2 | 2 | 1.00 | |
| pptx-cell-03 | cell-edit | 2 | 2 | 2 | 1.00 | |
| pptx-relayout-01 | table-relayout | 3 | 3 | 3 | 1.00 | deepcopy + `service_g_row._tr.addnext(new_tr)`, keep new_tr, fill via runs[0], recompute Total, re-band, frame height = sum of rows, bottom vs slide_height + shapes below. Listed alternative `tbl.tr_lst.insert(idx, new_tr)` is a no-op (tr_lst is a plain Python list, verified 1.0.2) — minor, primary method correct |
| pptx-relayout-02 | table-relayout | 3 | 3 | 3 | 1.00 | |
| pptx-tblins-01 | table-insert | 3 | 2 | 2 | 1.00 | |
| pptx-tblins-02 | table-insert | 2 | 2 | 1 | 0.50 | point 1 stated with a wrong fact: "an `a:tblStyle` element inside the table's `a:tblPr`" — the child is `a:tableStyleId` (verified 1.0.2); Medium Style 2 not named → 0. Copy tblPr / explicit fills → 1 (`table.band_row` does not exist, minor) |
| pptx-tblins-03 | table-insert, pptx-model | 1 | 2 | 2 | 1.00 | TablePlaceholder.insert_table(rows, cols); SlidePlaceholder has none → add_table at ph bbox and remove the placeholder element |
| pptx-imgrep-01 | image-replace | 3 | 3 | 2 | 0.67 | add_picture with old geometry, spTree index restore + remove old p:pic correct. Point 3 missed: no drop of the old image rel ("ensure the new image is a distinct image part" only) |
| pptx-imgrep-02 | image-replace | 2 | 2 | 2 | 1.00 | |
| pptx-imgrep-03 | image-replace, verify-safety | 2 | 2 | 2 | 1.00 | ref count sees only r:id, a:blip r:embed → always dropped; check //@r:embed / //@r:link first |
| pptx-imgins-01 | image-insert | 3 | 2 | 2 | 1.00 | |
| pptx-imgins-02 | image-insert | 2 | 2 | 2 | 1.00 | |
| pptx-legacy-01 | legacy-ppt | 3 | 2 | 2 | 1.00 | OLE .ppt unsupported; soffice → pptx, edit, `--convert-to ppt` back if needed, keep original. "template variants" overstated (1.0.2 accepts only .pptx/.pptm main parts), minor |
| pptx-legacy-02 | legacy-ppt, verify-safety | 2 | 2 | 2 | 1.00 | |
| pptx-verify-01 | verify-safety | 3 | 2 | 2 | 1.00 | unchanged-remainder assert not explicit (minor, as baseline) |
| pptx-verify-02 | verify-safety, pptx-model | 2 | 2 | 2 | 1.00 | now names duplicate cNvPr ids and tc without txBody |
| pptx-verify-03 | verify-safety, image-replace | 2 | 2 | 2 | 1.00 | drop rel after r:embed/r:link check, verify by listing ppt/media. "python-pptx does not garbage-collect unused parts on save" is wrong (after drop_rel the media is gone on save, verified 1.0.2), so the extra "delete the media part" step is unnecessary — minor, core steps correct |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| pptx-model | 1.00 | 6 | ok | target — passes |
| table-locate | 1.00 | 2 | low-n | target — passes |
| row-delete | 1.00 | 4 | ok | target — passes |
| cell-edit | 1.00 | 4 | ok | non-target |
| table-relayout | 1.00 | 4 | ok | target — passes |
| table-insert | 0.83 | 3 | low-n | non-target (single-sample; see notes) |
| image-replace | 0.91 | 5 | ok | non-target |
| image-insert | 1.00 | 2 | low-n | non-target |
| legacy-ppt | 1.00 | 2 | low-n | target — passes |
| verify-safety | 1.00 | 6 | ok | target — passes |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

Arithmetic: pptx-model (3+2+2+2+1+2)/12 = 12/12; table-locate 5/5; row-delete
(3+3+2+2)/10 = 10/10; cell-edit (2+3+2+2)/9 = 9/9; table-relayout (3+2+3+3)/11 = 11/11;
table-insert (3+1+1)/6 = 5/6 = 0.83; image-replace (2+2+2+2+2)/11 = 10/11 = 0.91;
image-insert 5/5; legacy-ppt 5/5; verify-safety (2+2+2+3+2+2)/13 = 13/13.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight)
            = (65 − 1 [tblins-02: 2×0.5] − 1 [imgrep-01: 3×1/3]) / 65
            = 63.0 / 65 = 96.9%
```

(Baseline: 55.0 / 65 = 84.6%.)

## Target tags: baseline → with-skill → pass(≥0.9)?

| tag | baseline | with-skill | n | pass (≥0.9)? |
|-----|---------:|-----------:|--:|--------------|
| pptx-model | 0.88 | 1.00 | 6 | pass |
| table-locate | 0.50 | 1.00 | 2 (low-n) | pass |
| row-delete | 0.85 | 1.00 | 4 | pass |
| table-relayout | 0.82 | 1.00 | 4 | pass |
| legacy-ppt | 0.50 | 1.00 | 2 (low-n) | pass |
| verify-safety | 0.85 | 1.00 | 6 | pass |

Validation: **pass** — every target tag ≥ 0.9. No target remains below threshold, so there
is no remaining wrong or missing target claim.

Non-target notes (single-sample, not acted on): table-insert 0.92 → 0.83 (tblins-02 names
`a:tblStyle` instead of `a:tableStyleId`); image-replace 0.91 → 0.91 (imgrep-01 no longer
drops the old image rel). If these recur in a fresh baseline, derive them separately.

## History

- 0.75-era with-skill run (targets table-locate, legacy-ppt; stack 90.8%): table-locate
  0.50 → 1.00, legacy-ppt 0.50 → 1.00, both pass; non-targets then at row-delete 0.75,
  table-insert 0.83, verify-safety 0.88, image-replace 0.86.
