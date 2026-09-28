---
model_id: glm-5.3-flash
model_version: "5.3"
evaluated_via: ollama-cloud
evaluated_on: 2026-09-28
stack: pptx
stack_corpus_rev: 1
threshold: 0.9
---

# Scorecard — glm-5.3-flash on pptx

> Valid ONLY for `glm-5.3-flash` @ `5.3`. A version bump invalidates
> this scorecard — re-benchmark.

Answers: `pptx/answers/glm-5.3-flash.md` (closed-book, audited zero tool calls).
Graded against `pptx/questions.yaml` corpus_rev 1 (python-pptx facts verified on 1.0.2).
Grading rule applied: a `point` whose core concept is present earns full credit
(minor missing sub-items are noted); a `point` stated with a wrong API fact, or
satisfied only by a cover-up (white box, hidden shape, white text), earns 0.
Equivalent correct approaches earn the point.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| pptx-model-01 | pptx-model | 3 | 2 | 2 | 1.00 | |
| pptx-model-02 | pptx-model | 2 | 2 | 2 | 1.00 | doesn't name prs.slide_width/height; points to p:sldSz and notes the 4:3 variant (minor) |
| pptx-model-03 | pptx-model, image-replace | 2 | 2 | 2 | 1.00 | |
| pptx-locate-01 | table-locate | 3 | 2 | 1 | 0.50 | consistency pass (all 4 models graded alike): table picked positionally (`tables[0]`), not by header, and no unique-match assert → point 2 missed; substring match only |
| pptx-locate-02 | table-locate, pptx-model | 2 | 2 | 2 | 1.00 | is_merge_origin/is_spanned not named (minor) |
| pptx-rowdel-01 | row-delete | 3 | 2 | 2 | 1.00 | frame height mentioned; totals not (minor) |
| pptx-rowdel-02 | row-delete, table-relayout | 3 | 2 | 2 | 1.00 | fix = shrink frame by the deleted row's h (equivalent to sum of rows); downplays impact ("often still renders fine") |
| pptx-rowdel-03 | row-delete, cell-edit | 2 | 2 | 2 | 1.00 | |
| pptx-rowdel-04 | row-delete, verify-safety | 2 | 2 | 2 | 1.00 | no "verify old text absent from every slide part" step; grow-to-fit not stated (hairline/min-height instead) (minor) |
| pptx-cell-01 | cell-edit | 3 | 2 | 2 | 1.00 | |
| pptx-cell-02 | cell-edit, table-relayout | 2 | 2 | 2 | 1.00 | doesn't say to shrink other columns or check neighbour shapes (minor) |
| pptx-cell-03 | cell-edit | 2 | 2 | 2 | 1.00 | |
| pptx-relayout-01 | table-relayout | 3 | 3 | 2 | 0.67 | deepcopy + total_tr.addprevious (equivalent) and fill via runs + frame height correct; never updates the Total value (minor); **no check of the new frame bottom vs slide height / shapes below** → point 3 missed |
| pptx-relayout-02 | table-relayout | 3 | 3 | 3 | 1.00 | no wrap check on the narrowed columns (minor) |
| pptx-tblins-01 | table-insert | 3 | 2 | 2 | 1.00 | places below the lowest bottom edge but never checks the table still fits inside slide_height; no "ask / new slide" fallback (minor) |
| pptx-tblins-02 | table-insert | 2 | 2 | 2 | 1.00 | side claim wrong ("when that style isn't defined in tableStyles.xml PowerPoint falls back to Medium Style 2") — the default GUID written *is* Medium Style 2; the point (style = tableStyleId + tblPr flags, default Medium Style 2) is still stated, unlike imgrep-03 where the wrong mechanism *is* the point |
| pptx-tblins-03 | table-insert, pptx-model | 1 | 2 | 1 | 0.50 | wrong API fact: "There is no insert_table on placeholders — only picture placeholders have insert_picture()" (TablePlaceholder.insert_table exists) → point 1 = 0; add-at-bbox + remove placeholder correct |
| pptx-imgrep-01 | image-replace | 3 | 3 | 2 | 0.67 | repoint r:embed via get_or_add_image_part, and reinsert at old spTree index: correct; **never drops the old image rel when unused** → point 3 missed; no aspect-ratio caveat (minor) |
| pptx-imgrep-02 | image-replace | 2 | 2 | 2 | 1.00 | SHA1 not named; no "check who references the part first" (minor) |
| pptx-imgrep-03 | image-replace, verify-safety | 2 | 2 | 1 | 0.50 | wrong mechanism: "drop_rel() removes a relationship wholesale and does not count how many shapes use it" (it does ref-count, but only r:id, never a:blip r:embed) → point 1 = 0; scan all a:blip r:embed before dropping correct (r:link not named) |
| pptx-imgins-01 | image-insert | 3 | 2 | 2 | 1.00 | |
| pptx-imgins-02 | image-insert | 2 | 2 | 2 | 1.00 | |
| pptx-legacy-01 | legacy-ppt | 3 | 2 | 2 | 1.00 | |
| pptx-legacy-02 | legacy-ppt, verify-safety | 2 | 2 | 2 | 1.00 | |
| pptx-verify-01 | verify-safety | 3 | 2 | 2 | 1.00 | "unchanged remainder" only via slide count; no XML/text diff of untouched slides, no search for removed text (minor) |
| pptx-verify-02 | verify-safety, pptx-model | 2 | 2 | 2 | 1.00 | broken spans and "python-pptx doesn't validate" not stated (minor) |
| pptx-verify-03 | verify-safety, image-replace | 2 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| pptx-model | 0.96 | 6 | ok | omit (strong) |
| table-locate | 0.70 | 2 | low-n | **derive** |
| row-delete | 1.00 | 4 | ok | omit (strong) |
| cell-edit | 1.00 | 4 | ok | omit (strong) |
| table-relayout | 0.91 | 4 | ok | omit |
| table-insert | 0.92 | 3 | low-n | omit |
| image-replace | 0.82 | 5 | ok | **derive** |
| image-insert | 1.00 | 2 | low-n | omit (strong) |
| legacy-ppt | 1.00 | 2 | low-n | omit (strong) |
| verify-safety | 0.92 | 6 | ok | omit |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

Arithmetic (Σ normalized×weight / Σ weight):
pptx-model 11.5/12; table-locate 3.5/5; row-delete 10/10; cell-edit 9/9;
table-relayout 10/11; table-insert 5.5/6; image-replace 9/11; image-insert 5/5;
legacy-ppt 5/5; verify-safety 12/13.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 60.0 / 65 = 92.3%
```

Weighted losses: pptx-locate-01 (−1.5), pptx-relayout-01 (−1.0), pptx-imgrep-01 (−1.0),
pptx-imgrep-03 (−1.0), pptx-tblins-03 (−0.5).

## Derivation targets

Tags below threshold (`< 0.9`): **table-locate** (0.70, low-n), **image-replace**
(0.82) → `derived/pptx.glm-5.3-flash.SKILL.md`.

### table-locate (0.70, low-n)
- Picks the table positionally (locate-01): `tbl = tables[0]  # or match by shape
  name/size`. Header text is mentioned only as an afterthought ("disambiguate by
  number of rows, shape name, or presence of the Item/Qty headers"), never done in
  code. Correct: compare each `has_table` frame's first-row texts to the expected
  header.
- Only iterates top-level `slide.shapes`: a table inside a group shape is missed
  (verified on 1.0.2: `has_table` is False for every top-level shape when the
  table sits in a group; recursing `GroupShape.shapes` finds it).
- Row match by substring (`"Gadget D" in cell.text`) over every cell, printing all
  hits, with no exactly-one-match assert. Correct: match
  `cell.text_frame.text.strip() == "Gadget D"` on the key column and assert
  `len(hits) == 1`.

### image-replace (0.82)
- Wrong mechanism (imgrep-03): "drop_rel() removes a relationship wholesale and
  does not count how many shapes use it". Actual (1.0.2): `drop_rel` drops when
  `_rel_ref_count(rId) < 2`, and that count only matches `//@r:id`. Pictures
  use `a:blip r:embed`, so an image rel always counts 0 and is always dropped,
  even when another `p:pic` on the slide shares it (verified: two add_picture
  calls of the same bytes share one rId; drop_rel after removing one leaves the
  other picture with no rel).
- Missing cleanup (imgrep-01): both procedures (repoint `r:embed`, or add_picture +
  remove old `p:pic`) stop without dropping the old image rel. Verified: after a
  repoint the old rel stays and the old media is still saved in the zip
  (2 media entries); after checking `//@r:embed | //@r:link` for other users and
  dropping it, 1 entry remains.
- Wrong advice (imgrep-03): "If in doubt, skip drop_rel: an unused relationship is
  harmless (it just keeps the bytes in the package)". For a confidential or
  replaced image that kept byte blob is the problem (verify-03: deleting `p:pic`
  alone leaves `ppt/media/imageN.*` in the saved zip).
- Minor (imgrep-01): no aspect-ratio caveat when repointing `r:embed` to an image
  of different proportions (the old extents stretch it).

## Live behavioural probe — editing a real .pptx (2026-09-28)

The model is driven with `ocode run -yolo` in a fresh git repo, one task per run, and the output is checked by `docs/okf/pptx/probe/check.py`. "not run" means that model had no skill for that phase, or its provider was out of credit. The 0.9 runs used a binary built before two late skill edits (pdf `new_page(pno + 1)`, mimo pptx iteration 2).

| task | baseline | 0.75-era skill | 0.9 skill |
|---|---|---|---|
| delete-row | FAIL: frame height = sum of row heights; banded fills alternate across all body rows | not run | not run |
| edit-cell | PASS (151s) | not run | not run |
| add-row | PASS (247s) | not run | not run |
| add-column | PASS (291s) | not run | not run |
| insert-table | PASS (261s) | not run | not run |
| rename-item | no result (provider 429) | not run | not run |
| replace-image | no result (provider 429) | not run | not run |
| insert-image | no result (provider 429) | not run | not run |
