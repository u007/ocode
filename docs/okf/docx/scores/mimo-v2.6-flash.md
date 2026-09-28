---
model_id: mimo-v2.6-flash
model_version: "2.6"
evaluated_via: opencode-go
evaluated_on: 2026-09-28
stack: docx
stack_corpus_rev: 1
threshold: 0.9
---

# Scorecard — mimo-v2.6-flash on docx

> Valid ONLY for `mimo-v2.6-flash` @ `2.6`. A version bump invalidates
> this scorecard — re-benchmark.

Answers: `docx/answers/mimo-v2.6-flash.md` (closed-book, audited zero tool calls).
Graded against `docx/questions.yaml` corpus_rev 1 (python-docx facts verified on 1.2.0).
Grading rule applied: a `point` whose core concept is present earns full credit
(minor missing sub-items are noted); a `point` stated with a wrong API fact, or
satisfied only by a cover-up (hidden/white text, zero-height rows, white boxes),
earns 0. Points are binary (per rubric-guide.md): missing sub-items are noted
but keep the point; a sub-claim that contradicts the point's own text zeroes it.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| docx-model-01 | docx-model | 3 | 2 | 2 | 1.00 | |
| docx-model-02 | docx-model, table-locate | 3 | 2 | 2 | 1.00 | |
| docx-model-03 | docx-model, table-relayout | 2 | 2 | 2 | 1.00 | |
| docx-locate-01 | table-locate | 3 | 2 | 2 | 1.00 | fails loudly if no table matches, but no explicit "exactly one row match" assertion (minor); side claim that field results "will be recomputed" is wrong (not graded here) |
| docx-locate-02 | table-locate, docx-model | 2 | 2 | 2 | 1.00 | correct fix given (`tr.tc_lst` + cumulative gridSpan); also offers an invented "GridMapper (table._tbl.grid_span helpers)" as an alternative (not relied on) |
| docx-rowdel-01 | row-delete | 3 | 2 | 1 | 0.50 | removal via `row._tr` correct; never says Word reflows (no gap to close) or that the Total must be updated |
| docx-rowdel-02 | row-delete, table-locate | 2 | 2 | 2 | 1.00 | |
| docx-rowdel-03 | row-delete, cell-edit | 2 | 2 | 1 | 0.50 | point 1 = 0: says python-docx doesn't recompute (right) but "When Word opens the file … usually it updates on open/print depending on settings" — contradicts the point (Word does NOT recompute on open by default); writing the recomputed value into the result `w:t` + updateFields correct |
| docx-rowdel-04 | row-delete, verify-safety | 2 | 2 | 2 | 1.00 | searches only word/document.xml, not headers/footers (minor) |
| docx-cell-01 | cell-edit | 3 | 2 | 2 | 1.00 | |
| docx-cell-02 | cell-edit, table-locate | 2 | 2 | 2 | 1.00 | compute from data + consistent rounding; no mention of keeping currency/thousands format or cell alignment (minor); repeats "set updateFields … (Word updates on open)" — it only prompts |
| docx-cell-03 | cell-edit, verify-safety | 2 | 2 | 2 | 1.00 | |
| docx-relayout-01 | table-relayout | 3 | 3 | 3 | 1.00 | no banding/alternate-shading check on the copied row (minor) |
| docx-relayout-02 | table-relayout | 3 | 3 | 3 | 1.00 | |
| docx-relayout-03 | table-relayout, docx-model | 2 | 2 | 2 | 1.00 | |
| docx-tblins-01 | table-insert | 3 | 2 | 2 | 1.00 | |
| docx-tblins-02 | table-insert | 2 | 2 | 2 | 1.00 | |
| docx-tblins-03 | table-insert, table-relayout | 2 | 2 | 2 | 1.00 | copies the existing table's measurements (layout, tblW, gridCol widths); misses per-cell tcW, table style and header formatting (noted) |
| docx-imgrep-01 | image-replace, docx-model | 3 | 3 | 2 | 0.67 | wrong API on the replace step: `image_part, new_rId = doc.part.get_or_add_image_part('new_logo.png')` — DocumentPart has no such method; the real call is `rId, image = doc.part.get_or_add_image(path)` (point 2 = 0); blip→rels→media model and "remove old rel if unused" correct |
| docx-imgrep-02 | image-replace | 2 | 2 | 2 | 1.00 | |
| docx-imgrep-03 | image-replace, table-locate | 2 | 2 | 2 | 1.00 | |
| docx-imgins-01 | image-insert | 3 | 2 | 2 | 1.00 | |
| docx-imgins-02 | image-insert, table-relayout | 2 | 2 | 2 | 1.00 | |
| docx-legacy-01 | legacy-doc | 3 | 2 | 2 | 1.00 | "renaming doesn't help" not stated (minor); lists textutil as an alternative converter but flags its risk |
| docx-legacy-02 | legacy-doc, verify-safety | 2 | 2 | 2 | 1.00 | |
| docx-legacy-03 | legacy-doc | 1 | 2 | 2 | 1.00 | |
| docx-verify-01 | verify-safety | 3 | 2 | 2 | 1.00 | backup + temp output, re-open and assert the intended change; never checks the rest of the document is unchanged (diff) or searches all parts for removed text (noted) |
| docx-verify-02 | verify-safety, docx-model | 2 | 2 | 2 | 1.00 | duplicate `wp:docPr` ids not mentioned (minor) |
| docx-verify-03 | verify-safety, image-replace | 2 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| docx-model | 0.94 | 7 | ok | omit (strong) |
| table-locate | 1.00 | 6 | ok | omit (strong) |
| row-delete | 0.72 | 4 | ok | **derive** |
| cell-edit | 0.89 | 4 | ok | **derive** |
| table-relayout | 1.00 | 6 | ok | omit (strong) |
| table-insert | 1.00 | 3 | low-n | omit (strong) |
| image-replace | 0.89 | 4 | ok | **derive** |
| image-insert | 1.00 | 2 | low-n | omit (strong) |
| legacy-doc | 1.00 | 3 | low-n | omit (strong) |
| verify-safety | 1.00 | 6 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 65.5 / 69 = 94.9%
```

## Derivation targets

Tags below threshold (`< 0.9`): **row-delete**, **cell-edit**, **image-replace**
→ feed into `derived/docx.mimo-v2.6-flash.SKILL.md`.

### row-delete (0.72)
- Wrong belief about fields on open (rowdel-03): "When Word opens the file it
  may or may not update fields automatically (usually it updates on open/print
  depending on settings …)". Word does NOT recompute fields on open by default;
  the stale cached result (still including the deleted row) stays displayed
  until fields are updated. The fix is to write the recomputed value into the
  field's result run; `w:updateFields` in settings.xml only makes Word prompt
  the user on open.
- Same belief recurs elsewhere: locate-01 ("overwriting the displayed result
  will be recomputed") and cell-02 ("set updateFields … (Word updates on
  open)").
- Missing follow-through after removing the row (rowdel-01): gives the correct
  `tr.getparent().remove(tr)` but never states that Word reflows (rows below
  move up, nothing to shift) or that the Total / dependent numbers must be
  updated after the deletion.

### cell-edit (0.89)
- Lost only through rowdel-03 (shared with row-delete): the cached field
  result is treated as something Word will refresh on open.
- cell-02 repeats it from the value-change side: after changing Qty it says to
  write the cached `<w:t>` "AND force recalculation: set `<w:updateFields>` …
  (Word updates on open)". Writing the computed value into every dependent
  field's result run (row amount, subtotal, tax, total) is the fix;
  updateFields only prompts. Not graded down there, but the same error.
- Missing (cell-02, minor): keep the existing number format (decimals,
  thousands separator, currency) and the cell paragraph's alignment when
  writing the new figures.

### image-replace (0.89)
- Invented API (imgrep-01): `image_part, new_rId =
  doc.part.get_or_add_image_part('new_logo.png')`. DocumentPart has no such
  method; python-docx 1.2.0 is `rId, image = doc.part.get_or_add_image(path)`
  (returns `(str, Image)`; same bytes return the existing rId).
- Unconditional `del doc.part.rels[old_rId]` right after repointing one blip:
  if another picture still embeds the old rId (shared/deduplicated media, which
  imgrep-02 itself describes) that picture is left dangling. The rel may be
  removed only when no `a:blip/@r:embed` in the part still uses it.
  (`Part.drop_rel()` is not a safe check: it counts only `@r:id` references,
  not `r:embed`, verified on 1.2.0.)

Factual errors in passing tags, not derived:
- locate-02 offers an invented "GridMapper (table._tbl.grid_span helpers)" as
  an alternative (correct `tc_lst` + gridSpan path also given).

## Live behavioural probe — editing a real .docx (2026-09-28)

The model is driven with `ocode run -yolo` in a fresh git repo, one task per run, and the output is checked by `docs/okf/docx/probe/check.py`. "not run" means that model had no skill for that phase, or its provider was out of credit. The 0.9 runs used a binary built before two late skill edits (pdf `new_page(pno + 1)`, mimo pptx iteration 2).

| task | baseline | 0.75-era skill | 0.9 skill |
|---|---|---|---|
| delete-row | PASS (1261s) | PASS (944s) | PASS (751s) |
| edit-cell | PASS (403s) | PASS (572s) | PASS (228s) |
| add-row | PASS (721s) | PASS (403s) | PASS (819s) |
| add-column | FAIL: banded shading alternates across all body rows | PASS (1225s) | PASS (888s) |
| insert-table | PASS (668s) | PASS (1648s) | PASS (636s) |
| rename-item | PASS (1184s) | PASS (587s) | PASS (697s) |
| replace-image | PASS (1098s) | PASS (563s) | PASS (207s) |
| insert-image | PASS (431s) | PASS (1020s) | PASS (1411s) |
