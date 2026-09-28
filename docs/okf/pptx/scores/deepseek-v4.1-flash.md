---
model_id: deepseek-v4.1-flash
model_version: "4.1"
evaluated_via: opencode-go
evaluated_on: 2026-09-28
stack: pptx
stack_corpus_rev: 1
threshold: 0.9
---

# Scorecard — deepseek-v4.1-flash on pptx

> Valid ONLY for `deepseek-v4.1-flash` @ `4.1`. A version bump invalidates
> this scorecard — re-benchmark.

Answers: `pptx/answers/deepseek-v4.1-flash.md` (closed-book, audited zero tool calls).
Graded against `pptx/questions.yaml` corpus_rev 1 (python-pptx facts verified on 1.0.2).
Grading rule applied: a `point` whose core concept is present earns full credit
(minor missing sub-items are noted); a `point` stated with a wrong API/tool fact,
or satisfied only by a cover-up (white box, hidden shape, white text), earns 0.
Equivalent correct approaches earn the point.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| pptx-model-01 | pptx-model | 3 | 2 | 2 | 1.00 | |
| pptx-model-02 | pptx-model | 2 | 2 | 2 | 1.00 | correct 4:3 vs 16:9 defaults; no explicit "read prs.slide_width, don't assume" here (does read it in relayout-02/tblins-01) |
| pptx-model-03 | pptx-model, image-replace | 2 | 2 | 2 | 1.00 | |
| pptx-locate-01 | table-locate | 3 | 2 | 1 | 0.50 | has_table → .table correct (no group recursion, minor). Uniqueness point missed: takes the first `has_table` shape and `break`s, first matching row and `break`s; no header check, no unique-match assert. Also wrong: "`row.cells` repeats the same Cell object for merged cells" (python-docx behaviour, not python-pptx) |
| pptx-locate-02 | table-locate, pptx-model | 2 | 2 | 1 | 0.50 | representation (gridSpan/hMerge, rowSpan/vMerge) correct. Edit point stated with wrong facts: "python-pptx does not expose a merge/unmerge API" (cell.merge()/split() exist); "iterating cells returns the SAME Cell object … Writing text to any address in the span writes to the origin cell" (each _Cell wraps its own a:tc; writing a spanned cell writes the hidden tc) |
| pptx-rowdel-01 | row-delete | 3 | 2 | 1 | 0.50 | removal point stated with invented API: "The public API only lets you add a row (`table.add_row()`)" — no add_row exists. Frame-height follow-up present (totals not mentioned, minor) |
| pptx-rowdel-02 | row-delete, table-relayout | 3 | 2 | 2 | 1.00 | |
| pptx-rowdel-03 | row-delete, cell-edit | 2 | 2 | 2 | 1.00 | |
| pptx-rowdel-04 | row-delete, verify-safety | 2 | 2 | 2 | 1.00 | no "re-open and verify text absent from every slide part" (minor) |
| pptx-cell-01 | cell-edit | 3 | 2 | 2 | 1.00 | |
| pptx-cell-02 | cell-edit, table-relayout | 2 | 2 | 2 | 1.00 | |
| pptx-cell-03 | cell-edit | 2 | 2 | 2 | 1.00 | speaker notes / chart embedded workbook not named (minor) |
| pptx-relayout-01 | table-relayout | 3 | 3 | 1 | 0.33 | deepcopy + addprevious before Total correct. Fill point missed: fills new row with `cells[0].text = …` (drops run formatting, contradicts its own cell-01), `new_tr = rows[total_idx - 1]` after insert selects the Service G row, not the new one; Total never updated. Bottom-vs-slide/neighbour check absent (banding mentioned only) |
| pptx-relayout-02 | table-relayout | 3 | 3 | 3 | 1.00 | |
| pptx-tblins-01 | table-insert | 3 | 2 | 2 | 1.00 | |
| pptx-tblins-02 | table-insert | 2 | 2 | 2 | 1.00 | |
| pptx-tblins-03 | table-insert, pptx-model | 1 | 2 | 1 | 0.50 | wrong fact: "There is no `placeholder.insert_table()`" (TablePlaceholder.insert_table(rows, cols) exists). Fallback (add at placeholder bbox, remove placeholder) correct |
| pptx-imgrep-01 | image-replace | 3 | 3 | 3 | 1.00 | repoint blip r:embed via get_or_add_image_part keeps z-order; aspect-distortion risk not mentioned (minor) |
| pptx-imgrep-02 | image-replace | 2 | 2 | 2 | 1.00 | |
| pptx-imgrep-03 | image-replace, verify-safety | 2 | 2 | 1 | 0.50 | cause missed: "does not reliably reference-count across all usages" — never says the count sees only r:id, not a:blip r:embed, so image rels are always dropped. Check-before-drop correct |
| pptx-imgins-01 | image-insert | 3 | 2 | 2 | 1.00 | |
| pptx-imgins-02 | image-insert | 2 | 2 | 2 | 1.00 | |
| pptx-legacy-01 | legacy-ppt | 3 | 2 | 1 | 0.50 | OLE2 / not-a-zip correct. Workflow point stated with wrong fact: "There is no round-trip — you cannot write back to `.ppt`; deliver `.pptx`" (soffice --convert-to ppt does it) |
| pptx-legacy-02 | legacy-ppt, verify-safety | 2 | 2 | 1 | 0.50 | verification checklist correct. Tell-user point stated with wrong fact: "there is no `.ppt` output — only `.pptx`" → requested format not returned |
| pptx-verify-01 | verify-safety | 3 | 2 | 2 | 1.00 | |
| pptx-verify-02 | verify-safety, pptx-model | 2 | 2 | 2 | 1.00 | duplicate cNvPr ids, tc without txBody, "python-pptx doesn't validate" not named (minor) |
| pptx-verify-03 | verify-safety, image-replace | 2 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| pptx-model | 0.88 | 6 | ok | **derive** |
| table-locate | 0.50 | 2 | low-n | **derive** (mark low-n) |
| row-delete | 0.85 | 4 | ok | **derive** |
| cell-edit | 1.00 | 4 | ok | omit (strong) |
| table-relayout | 0.82 | 4 | ok | **derive** |
| table-insert | 0.92 | 3 | low-n | omit |
| image-replace | 0.91 | 5 | ok | omit |
| image-insert | 1.00 | 2 | low-n | omit (strong) |
| legacy-ppt | 0.50 | 2 | low-n | **derive** (mark low-n) |
| verify-safety | 0.85 | 6 | ok | **derive** |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 55.0 / 65 = 84.6%
```

## Derivation targets

Tags below threshold (`< 0.9`): **pptx-model, table-locate (low-n), row-delete,
table-relayout, legacy-ppt (low-n), verify-safety** → feed into
`derived/pptx.deepseek-v4.1-flash.SKILL.md`.

Above threshold, not derived: table-insert (0.92) and image-replace (0.91). Their
misses ("no `placeholder.insert_table()`", tblins-03; vague drop_rel cause, imgrep-03)
also carry the pptx-model / verify-safety tags and are covered there.

### pptx-model (0.88)
- Believes "python-pptx does not expose a merge/unmerge API" and that spanned cells are
  "the SAME Cell object" (locate-02). Actual (1.0.2): `cell.merge(other)` / `cell.split()`
  exist; every grid position is its own `_Cell` over its own `a:tc`.
- Believes "there is no `placeholder.insert_table()`" (tblins-03). Actual:
  `TablePlaceholder.insert_table(rows, cols)` exists; a generic content placeholder
  (`SlidePlaceholder`) has no such method, which is when the add-at-bbox fallback applies.
- Missing: read `prs.slide_width` / `prs.slide_height` instead of assuming a size
  (model-02; python-pptx's own default template is 4:3, 9144000 × 6858000).
- Missing from repair causes (verify-02): duplicate `p:cNvPr` ids after copying shapes,
  `a:tc` without `a:txBody`, and that python-pptx saves such XML without validating it.

### table-locate (0.50, low-n)
- Believes python-pptx `row.cells` / cell iteration "returns the SAME Cell object multiple
  times for a span" and that "writing text to any address in the span writes to the origin
  cell". Actual (1.0.2): every grid position is its own `_Cell` over its own `a:tc`; the
  covered cells are separate (`is_spanned`), so text must be written to the
  `is_merge_origin` cell explicitly.
- Believes "python-pptx does not expose a merge/unmerge API". Actual: `cell.merge(other)`
  and `cell.split()` exist; `is_merge_origin`, `is_spanned`, `span_width`, `span_height`
  describe merges.
- Missing: select the table by its header text (not the first `has_table` shape), recurse
  into group shapes, and assert exactly one matching row instead of `break`ing on the first hit.

### row-delete (0.85)
- Invents an add API: "The public API only lets you add a row (`table.add_row()`)"
  (rowdel-01). Actual: python-pptx has no row add or delete API at all (`Table`,
  `_RowCollection`, `_Row` expose none); rows are added by deep-copying an `a:tr` and
  removed with `table._tbl.remove(tr)`.
- Missing after a delete: update the Total / subtotals (rowdel-01), and verify by
  re-opening the saved file and checking the removed text is absent from every slide XML
  part (rowdel-04).

### table-relayout (0.82)
- Add-row script (relayout-01) fills the new row with `cells[0].text = …`, which drops the
  run's formatting (`rPr`); the text must go through the existing first run.
- Picks the wrong row after insert: `new_tr = rows[total_idx - 1]` after `addprevious`
  is the Service G row; the new row sits at the old Total index (keep the `new_tr`
  element reference instead).
- Total never recomputed; frame height grown by one template height rather than set to
  the sum of row heights; no check of the new bottom against `prs.slide_height` and the
  shapes below (banding only mentioned).

### legacy-ppt (0.50, low-n)
- Believes there is "no round-trip — you cannot write back to `.ppt`; deliver `.pptx`" /
  "there is no `.ppt` output — only `.pptx`". Actual: LibreOffice headless converts back
  (`soffice --headless --convert-to ppt --outdir out deck.pptx`); if the user asked for
  `.ppt`, return `.ppt` (keeping the untouched original), with the conversion caveat.

### verify-safety (0.85)
- drop_rel cause missed (imgrep-03): says it "does not reliably reference-count". Actual:
  `XmlPart._rel_ref_count` counts only `//@r:id`; pictures use `a:blip/@r:embed`, so an
  image rel counts 0 and is always dropped. Check `//@r:embed` / `//@r:link` yourself.
- Tells the user "there is no `.ppt` output" (legacy-02) → requested format not returned.
- Missing: re-open and check the removed text is absent from every slide part
  (rowdel-04); duplicate `cNvPr` ids, `a:tc` without `a:txBody`, "python-pptx doesn't
  validate" (verify-02).

## Live behavioural probe — editing a real .pptx (2026-09-28)

The model is driven with `ocode run -yolo` in a fresh git repo, one task per run, and the output is checked by `docs/okf/pptx/probe/check.py`. "not run" means that model had no skill for that phase, or its provider was out of credit. The 0.9 runs used a binary built before two late skill edits (pdf `new_page(pno + 1)`, mimo pptx iteration 2).

| task | baseline | 0.75-era skill | 0.9 skill |
|---|---|---|---|
| delete-row | FAIL: frame height = sum of row heights | PASS (197s) | PASS (266s) |
| edit-cell | PASS (153s) | PASS (130s) | PASS (195s) |
| add-row | PASS (223s) | PASS (321s) | PASS (303s) |
| add-column | PASS (210s) | PASS (230s) | PASS (170s) |
| insert-table | PASS (204s) | PASS (191s) | PASS (248s) |
| rename-item | PASS (152s) | PASS (81s) | PASS (104s) |
| replace-image | PASS (293s) | PASS (137s) | PASS (181s) |
| insert-image | PASS (181s) | PASS (136s) | PASS (117s) |
