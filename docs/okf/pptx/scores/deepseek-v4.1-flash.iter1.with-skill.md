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

# Scorecard — deepseek-v4.1-flash on pptx (with-skill validation, threshold 0.9)

> Valid ONLY for `deepseek-v4.1-flash` @ `4.1-2026-10-21`. A version bump invalidates
> this scorecard — re-benchmark.

Answers: `pptx/answers/deepseek-v4.1-flash.with-skill.md` (closed-book, one fresh
`ocode run` session per question with the body of
`derived/pptx.deepseek-v4.1-flash.SKILL.md` prepended as "Active guidance"; all tools
denied; every session sqlite audited, zero tool calls).
Baseline: `pptx/scores/deepseek-v4.1-flash.md` (90.3%, same `model_version`).
Graded against `pptx/questions.yaml` corpus_rev 1 (python-pptx facts verified on 1.0.2).
Grading rule (same as the baseline): a `point` whose core concept is present earns full
credit (minor missing sub-items are noted); a `point` stated with a wrong API/tool fact,
or satisfied only by a cover-up, earns 0. Equivalent correct approaches earn the point.
Points are whole; a `partial` only applies when no point is matched. pptx-locate-01
point 2 requires BOTH header-based table selection AND a unique-row assert.
Target tags (below 0.9 in the baseline): **table-locate (low-n), table-relayout,
image-replace, verify-safety**.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| pptx-model-01 | pptx-model | 3 | 2 | 2 | 1.00 | correct default GUID shown |
| pptx-model-02 | pptx-model | 2 | 2 | 1 | 0.50 | EMU 914400/in, 12700/pt → 1. Point 2 stated with a wrong fact → 0: "Default 16:9 (the default `Presentation()` template): 12192000 × 6858000", 4:3 called the "legacy default". python-pptx 1.0.2's default template is 9144000 × 6858000 (4:3), verified. The baseline earned this point by saying the default is 4:3; same strictness as tblins-03. The 16:9 dims and "read prs.slide_width/height" are otherwise right (the baseline's 13,333,333 width is gone) |
| pptx-model-03 | pptx-model, image-replace | 2 | 2 | 2 | 1.00 | |
| pptx-locate-01 | table-locate | 3 | 2 | 2 | 1.00 | recursive group walk + has_table; picks the table by its first-row header tuple and asserts exactly one candidate; label column from header; exact stripped match, collects hits, asserts `len(hits) == 1`; explicitly rejects `next(...)` |
| pptx-locate-02 | table-locate, pptx-model | 2 | 2 | 2 | 1.00 | (side claim, hedged: writing to a covered cell "either raises (some versions) or …" — it does not raise in 1.0.2; ungraded, as baseline legacy-01) |
| pptx-rowdel-01 | row-delete | 3 | 2 | 2 | 1.00 | no remove/delete/add_row API; `tbl.remove(rows[i]._tr)`; frame height via `table._graphic_frame` (exists, verified) + totals |
| pptx-rowdel-02 | row-delete, table-relayout | 3 | 2 | 2 | 1.00 | |
| pptx-rowdel-03 | row-delete, cell-edit | 2 | 2 | 2 | 1.00 | |
| pptx-rowdel-04 | row-delete, verify-safety | 2 | 2 | 2 | 1.00 | now also re-opens and checks the text is absent from every `ppt/slides/slide*.xml` (baseline gap closed) |
| pptx-cell-01 | cell-edit | 3 | 2 | 2 | 1.00 | |
| pptx-cell-02 | cell-edit, table-relayout | 2 | 2 | 2 | 1.00 | |
| pptx-cell-03 | cell-edit | 2 | 2 | 2 | 1.00 | now names chart data/workbooks, speaker notes, number format |
| pptx-relayout-01 | table-relayout | 3 | 3 | 3 | 1.00 | deepcopy Service G `a:tr` + `addnext`; fill via runs[0]; recompute Total; `gf.height = sum(rows)`; bottom vs `prs.slide_height` + shift shapes below or ask; re-alternate explicit fills; states `add_row()` does not exist |
| pptx-relayout-02 | table-relayout | 3 | 3 | 3 | 1.00 | gridCol + tc per row (span-aware), deepcopy neighbour cell, rebalance widths ≤ available, `gf.width = sum`. No 16:9 width misquote |
| pptx-tblins-01 | table-insert | 3 | 2 | 2 | 1.00 | |
| pptx-tblins-02 | table-insert | 2 | 2 | 2 | 1.00 | judgement: `a:tableStyleId` + firstRow/bandRow flags, Medium Style 2 – Accent 1 named, copy tblPr / tcPr / run props — correct. The GUID in its snippet is wrong (`…-B151-600D1358B6C3`; actual `…-B048-85BDC9FD1C3A`, verified), but it is explicitly hedged ("read off a newly added table rather than trusted from memory") and the element/flags are right, so not zeroed |
| pptx-tblins-03 | table-insert, pptx-model | 1 | 2 | 1 | 0.50 | wrong fact persists from baseline: "there is no `placeholder.insert_table()` … `TablePlaceholder` is read-only", "`ph.insert_table(...)` raises AttributeError" (TablePlaceholder.insert_table exists in 1.0.2, verified). Fallback (add at ph bbox, remove placeholder) correct |
| pptx-imgrep-01 | image-replace | 3 | 3 | 3 | 1.00 | repoint `a:blip r:embed` via `get_or_add_image_part` (p:pic stays in place → geometry + z-order kept; add_picture + addnext + remove also given); drops the old rel only after checking `r:embed`/`r:link` in the part; "a left-behind image rel keeps the old ppt/media in the saved zip" — baseline misbelief corrected |
| pptx-imgrep-02 | image-replace | 2 | 2 | 2 | 1.00 | |
| pptx-imgrep-03 | image-replace, verify-safety | 2 | 2 | 2 | 1.00 | `_rel_ref_count` = `//@r:id` only → image rel always 0 → always dropped (matches 1.0.2 source); check `//@r:embed`/`//@r:link` first. Baseline's "substring count" misbelief gone |
| pptx-imgins-01 | image-insert | 3 | 2 | 2 | 1.00 | |
| pptx-imgins-02 | image-insert | 2 | 2 | 2 | 1.00 | |
| pptx-legacy-01 | legacy-ppt | 3 | 2 | 2 | 1.00 | OLE2, `PackageNotFoundError` (verified on an OLE-header file), soffice → pptx, convert back if needed |
| pptx-legacy-02 | legacy-ppt, verify-safety | 2 | 2 | 2 | 1.00 | judgement as baseline: tells user it is a lossy conversion, keeps original as master; does not say "return .ppt if required" here — minor, no wrong fact |
| pptx-verify-01 | verify-safety | 3 | 2 | 2 | 1.00 | new file, re-open from disk, exact asserts, shape-count diff, render via soffice |
| pptx-verify-02 | verify-safety, pptx-model | 2 | 2 | 2 | 1.00 | tc/gridCol mismatch, tc without txBody, spans; dup cNvPr ids, dangling rIds, schema order; python-pptx doesn't validate |
| pptx-verify-03 | verify-safety, image-replace | 2 | 2 | 2 | 1.00 | rel + media survive; drop after r:embed/r:link check; hash-compare `ppt/media/*` |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | Σ(norm×w) / Σw | n | trust | role |
|-----|---------:|---------------:|--:|-------|------|
| pptx-model | 0.875 | 10.5 / 12 | 6 | ok | non-target (below 0.9, see drift) |
| table-locate | 1.00 | 5 / 5 | 2 | low-n | **target — pass** |
| row-delete | 1.00 | 10 / 10 | 4 | ok | non-target |
| cell-edit | 1.00 | 9 / 9 | 4 | ok | non-target |
| table-relayout | 1.00 | 11 / 11 | 4 | ok | **target — pass** |
| table-insert | 0.92 | 5.5 / 6 | 3 | low-n | non-target |
| image-replace | 1.00 | 11 / 11 | 5 | ok | **target — pass** |
| image-insert | 1.00 | 5 / 5 | 2 | low-n | non-target |
| legacy-ppt | 1.00 | 5 / 5 | 2 | low-n | non-target |
| verify-safety | 1.00 | 13 / 13 | 6 | ok | **target — pass** |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight)
            = (65 − 1 [model-02: 2×0.5] − 0.5 [tblins-03: 1×0.5]) / 65
            = 63.5 / 65 = 97.7%
```

(Baseline as published: 58.67 / 65 = 90.3%; corrected for the imgrep-03 full, 59 / 65 = 90.8% — see below.)

## Validation result

| target tag | baseline | with-skill | n | pass (≥ 0.9)? |
|------------|---------:|-----------:|--:|---------------|
| table-locate | 0.70 | 1.00 | 2 (low-n) | **PASS** |
| table-relayout | 0.82 | 1.00 | 4 | **PASS** |
| image-replace | 0.79 | 1.00 | 5 | **PASS** |
| verify-safety | 0.897 | 1.00 | 6 | **PASS** |

Validation: **PASS** — every target tag reaches 0.9. Each baseline misbelief the skill
names is gone in this run: header-based table selection + unique-row assert (locate-01),
no `table.add_row()` (stated absent in relayout-01, rowdel-01, relayout-02, legacy-01),
Total + frame height + bottom check after an insert (relayout-01), drop_rel `//@r:id`
mechanism (imgrep-03), "orphan rel is harmless" replaced by "a left-behind rel keeps the
media" (imgrep-01), and save → re-open → search every slide part (rowdel-04).

Baseline arithmetic note: the baseline row for pptx-imgrep-03 uses full = 3, but its
rubric has two 1-point points (+ a 0.5 partial), so full = 2 and its 1 awarded point is
0.50, not 0.33. Corrected baseline: image-replace 9 / 11 = 0.82 (still a target),
verify-safety 12 / 13 = 0.923 (would not have been a target), stack 59 / 65 = 90.8%. The
table above keeps the published baseline values because they defined the targets. This
scorecard uses full = 2; under the baseline's full = 3 the with-skill result is
still a pass (image-replace 0.94, verify-safety 0.95).

New wrong facts with the skill active (none in a target tag):
- pptx-model-02 — **introduced by the skill**: says python-pptx's default
  `Presentation()` template is 16:9; it is 4:3 (9144000 × 6858000, verified 1.0.2). The
  baseline had this right. Likely trigger: the skill's "A 16:9 deck is typically
  12192000 × 6858000 EMU" bullet. Costs model-02 point 2.
- pptx-tblins-02: wrong default style GUID in a snippet, explicitly hedged.
- pptx-tblins-03: the `TablePlaceholder.insert_table()` denial persists from the baseline
  (not new; table-insert is a non-target, 0.92).
- No invented `add_row()` anywhere.

Non-target drift (single sample, not acted on): **pptx-model 0.96 → 0.875** (below
0.9; model-02 default-template error, skill-induced, above), row-delete 1.00 → 1.00,
cell-edit 1.00 → 1.00, table-insert (low-n) 0.92 → 0.92, image-insert (low-n)
1.00 → 1.00, legacy-ppt (low-n) 1.00 → 1.00.

Observation (ungraded): skill content leaks into answers where it is not asked for — the
frame-height / drop_rel / re-open boilerplate is repeated in model-02, model-03,
cell-03, legacy-01 and tblins-02. Harmless for the score but adds length.
