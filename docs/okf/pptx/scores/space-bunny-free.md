---
model_id: space-bunny-free
model_version: "alpha"
evaluated_via: opencode-go
evaluated_on: 2026-09-28
stack: pptx
stack_corpus_rev: 1
threshold: 0.9
---

# Scorecard — space-bunny-free on pptx

> Valid ONLY for `space-bunny-free` @ `alpha`. A version bump invalidates
> this scorecard — re-benchmark.

Answers: `pptx/answers/space-bunny-free.md` (closed-book, audited zero tool calls).
Graded against `pptx/questions.yaml` corpus_rev 1 (python-pptx facts verified on 1.0.2).
Grading rule applied: a `point` whose core concept is present earns full credit
(minor missing sub-items are noted); a `point` stated with a wrong API fact, or
satisfied only by a cover-up (white boxes, hidden/off-slide shapes, white text),
earns 0. Equivalent correct approaches in another library earn the point.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| pptx-model-01 | pptx-model | 3 | 2 | 2 | 1.00 | typo "a:tbl/@a:tr" (rows are child elements, shown correctly in the XML) |
| pptx-model-02 | pptx-model | 2 | 2 | 2 | 1.00 | |
| pptx-model-03 | pptx-model, image-replace | 2 | 2 | 2 | 1.00 | |
| pptx-locate-01 | table-locate | 3 | 2 | 1 | 0.50 | consistency pass (all 4 models graded alike): table picked positionally (`tables[0]`), not by header, and no unique-match assert → point 2 missed |
| pptx-locate-02 | table-locate, pptx-model | 2 | 2 | 1 | 0.50 | representation point 0: "spanned-away grid positions are either omitted from the row or written as hMerge", "`row.cells[i]` is a physical-cell index … `cells[1]` is really column 2", cloned unmerged row "will have one fewer a:tc than the grid" — wrong, every row always has one a:tc per gridCol; edit-origin / fix-spans point met |
| pptx-rowdel-01 | row-delete | 3 | 2 | 2 | 1.00 | frame height fixed; totals not named (only cross-references) (minor) |
| pptx-rowdel-02 | row-delete, table-relayout | 3 | 2 | 2 | 1.00 | |
| pptx-rowdel-03 | row-delete, cell-edit | 2 | 2 | 2 | 1.00 | |
| pptx-rowdel-04 | row-delete, verify-safety | 2 | 2 | 2 | 1.00 | removes a:tr; no "verify old text absent from every slide part" step (minor) |
| pptx-cell-01 | cell-edit | 3 | 2 | 2 | 1.00 | |
| pptx-cell-02 | cell-edit, table-relayout | 2 | 2 | 2 | 1.00 | |
| pptx-cell-03 | cell-edit | 2 | 2 | 2 | 1.00 | chart embedded workbook/cache not named (minor) |
| pptx-relayout-01 | table-relayout | 3 | 3 | 3 | 1.00 | |
| pptx-relayout-02 | table-relayout | 3 | 3 | 3 | 1.00 | also inserts a cell in the Total row while bumping its gridSpan (double-count; minor) |
| pptx-tblins-01 | table-insert | 3 | 2 | 2 | 1.00 | |
| pptx-tblins-02 | table-insert | 2 | 2 | 2 | 1.00 | sample `new_gf.replace(new_tbl_el)` swaps the wrong element (minor) |
| pptx-tblins-03 | table-insert, pptx-model | 1 | 2 | 1 | 0.50 | insert_table point 0: calls `insert_table()` on the "body/content placeholder", "only on slide placeholders (`_BaseSlidePlaceholder`)" — wrong, only `TablePlaceholder` has it (a content placeholder is a `SlidePlaceholder`, AttributeError); also "no longer a placeholder (is_placeholder becomes False)" wrong. add_table-at-bbox + remove placeholder point met |
| pptx-imgrep-01 | image-replace | 3 | 3 | 3 | 1.00 | |
| pptx-imgrep-02 | image-replace | 2 | 2 | 2 | 1.00 | |
| pptx-imgrep-03 | image-replace, verify-safety | 2 | 2 | 1 | 0.50 | mechanism point 0: says the guard counts references so shared blips give "count is 2 and `drop_rel()` correctly refuses", and that two pictures sharing an image "normally have different rIds" — wrong, `_rel_ref_count` only matches `//@r:id`, so a:blip r:embed always counts 0 and image rels are always dropped; own r:embed check before dropping met |
| pptx-imgins-01 | image-insert | 3 | 2 | 2 | 1.00 | |
| pptx-imgins-02 | image-insert | 2 | 2 | 2 | 1.00 | |
| pptx-legacy-01 | legacy-ppt | 3 | 2 | 2 | 1.00 | first command uses the .ppt export filter name for a pptx target (`pptx:"Impress MS PowerPoint 97"`) (minor) |
| pptx-legacy-02 | legacy-ppt, verify-safety | 2 | 2 | 2 | 1.00 | |
| pptx-verify-01 | verify-safety | 3 | 2 | 2 | 1.00 | |
| pptx-verify-02 | verify-safety, pptx-model | 2 | 2 | 2 | 1.00 | no duplicate cNvPr ids, no "python-pptx doesn't validate" (minor); schema order + dangling rId present |
| pptx-verify-03 | verify-safety, image-replace | 2 | 2 | 2 | 1.00 | no hash compare of media (lists/walks reachable parts instead) |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| pptx-model | 0.88 | 6 | ok | **derive** |
| table-locate | 0.50 | 2 | low-n | **derive** |
| row-delete | 1.00 | 4 | ok | omit (strong) |
| cell-edit | 1.00 | 4 | ok | omit (strong) |
| table-relayout | 1.00 | 4 | ok | omit (strong) |
| table-insert | 0.92 | 3 | low-n | omit |
| image-replace | 0.91 | 5 | ok | omit |
| image-insert | 1.00 | 2 | low-n | omit (strong) |
| legacy-ppt | 1.00 | 2 | low-n | omit (strong) |
| verify-safety | 0.92 | 6 | ok | omit |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

- pptx-model: (3+2+2+1.0+0.5+2) / 12 = 10.5 / 12
- table-locate: (1.5+1.0) / 5 = 2.5 / 5
- table-insert: (3+2+0.5) / 6 = 5.5 / 6
- image-replace: (2+3+2+0.667+2) / 11 = 9.667 / 11
- verify-safety: (2+0.667+2+3+2+2) / 13 = 11.667 / 13

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 61.0 / 65 = 93.8%
```

(Lost weight: locate-01 1.5, locate-02 1.0, tblins-03 0.5, imgrep-03 1.0.)

## Derivation targets

> Recomputed 2026-09-28: `full` for pptx-imgrep-03 and pptx-verify-02 is 2 (the sum of `point` scores; a partial never adds to full). With that fix image-replace (0.91) and verify-safety (0.92) are at/above 0.9 and are NOT targets; only pptx-model and table-locate are.

Tags below threshold (`< 0.9`): **pptx-model** (0.88), **table-locate** (0.50, low-n) → `derived/pptx.space-bunny-free.SKILL.md`.

- **table-locate:** picks the table positionally (`tables[0]`, or "by `gf.name` or
  by position" when several exist) instead of by its header text; offers a
  substring match (`"Gadget D" in cell.text`) and never asserts exactly one
  matching row.
- **pptx-model (merges):** believes covered cells of a merge may be omitted from
  the row, that `row.cells[i]` is a physical-cell index distinct from the grid
  column, and that a cloned unmerged row "will have one fewer a:tc than the
  grid". In fact every `a:tr` has exactly one `a:tc` per `a:gridCol` (covered
  ones carry `hMerge`/`vMerge`), so `row.cells[c]` is `table.cell(r, c)`.
- **pptx-model (placeholders):** calls `insert_table()` on a generic
  body/content placeholder and says it exists on every slide placeholder; only
  `TablePlaceholder` has it (a content placeholder is a `SlidePlaceholder` and
  raises AttributeError). Also claims the result stops being a placeholder; it
  is a `PlaceholderGraphicFrame` with `is_placeholder` True.
- *(not a target after recompute, reference only)* **image-replace / verify-safety (`drop_rel`):** believes `drop_rel` counts blip
  references and refuses when an image rId is shared ("count is 2 and
  `drop_rel()` correctly refuses"), and that two pictures of the same image on
  one slide normally have different rIds. `_rel_ref_count` only matches
  `//@r:id`, so `a:blip r:embed` always counts 0 and image rels are always
  dropped; python-pptx reuses the existing rId for a repeated image on a slide.
- *(not a target after recompute, reference only)* **verify-safety (minor omissions):** no "old text absent from every slide XML
  part" check after a row delete; no hash compare of saved media against the
  removed image; duplicate `p:cNvPr` ids after copying shapes and "python-pptx
  does not validate" not named as repair causes.

## Live behavioural probe — editing a real .pptx (2026-09-28)

The model is driven with `ocode run -yolo` in a fresh git repo, one task per run, and the output is checked by `docs/okf/pptx/probe/check.py`. "not run" means that model had no skill for that phase, or its provider was out of credit. The 0.9 runs used a binary built before two late skill edits (pdf `new_page(pno + 1)`, mimo pptx iteration 2).

| task | baseline | 0.75-era skill | 0.9 skill |
|---|---|---|---|
| delete-row | PASS (406s) | FAIL: frame height = sum of row heights | PASS (448s) |
| edit-cell | PASS (194s) | PASS (220s) | PASS (208s) |
| add-row | PASS (618s) | PASS (417s) | PASS (515s) |
| add-column | FAIL: table XML: tc per gridCol, txBody, spans | PASS (447s) | FAIL: table XML: tc per gridCol, txBody, spans; rerun PASS (983s); rerun PASS (612s) |
| insert-table | PASS (333s) | PASS (318s) | PASS (417s) |
| rename-item | PASS (230s) | PASS (272s) | PASS (419s) |
| replace-image | PASS (204s) | PASS (234s) | PASS (303s) |
| insert-image | PASS (225s) | PASS (208s) | PASS (431s) |
