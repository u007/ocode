---
model_id: space-bunny-free
model_version: "alpha"
evaluated_via: opencode-go
evaluated_on: 2026-09-28
stack: docx
stack_corpus_rev: 1
threshold: 0.9
---

# Scorecard — space-bunny-free on docx

> Valid ONLY for `space-bunny-free` @ `alpha`. A version bump invalidates
> this scorecard — re-benchmark.

Answers: `docx/answers/space-bunny-free.md` (closed-book, audited zero tool calls).
docx-verify-01..03 were skipped in the first run and re-asked alone, also
closed-book; their answers are graded identically to the rest.
Graded against `docx/questions.yaml` corpus_rev 1 (python-docx facts verified on 1.2.0).
Grading rule applied: a `point` whose core concept is present earns full credit
(minor missing sub-items are noted); a `point` stated with a wrong API fact, or
satisfied only by a cover-up (hidden/white text, zero-height rows, white boxes),
earns 0.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| docx-model-01 | docx-model | 3 | 2 | 2 | 1.00 | minor: "w:tbl must be followed by a w:p in the body" overstated |
| docx-model-02 | docx-model, table-locate | 3 | 2 | 2 | 1.00 | |
| docx-model-03 | docx-model, table-relayout | 2 | 2 | 2 | 1.00 | minor: "add_row()/add_column() accept a Length" (add_row takes no args) |
| docx-locate-01 | table-locate | 3 | 2 | 2 | 1.00 | uniqueness only implied ("no duplicate"); substring/regex match can hit "Gadget DX" (minor) |
| docx-locate-02 | table-locate, docx-model | 2 | 2 | 2 | 1.00 | |
| docx-rowdel-01 | row-delete | 3 | 2 | 2 | 1.00 | reflow not stated explicitly, but no manual shifting proposed; totals covered |
| docx-rowdel-02 | row-delete, table-locate | 2 | 2 | 2 | 1.00 | promotes next cell to restart (option c) but never moves the origin cell's content, though it notes that text is lost |
| docx-rowdel-03 | row-delete, cell-edit | 2 | 2 | 2 | 1.00 | |
| docx-rowdel-04 | row-delete, verify-safety | 2 | 2 | 2 | 1.00 | |
| docx-cell-01 | cell-edit | 3 | 2 | 2 | 1.00 | |
| docx-cell-02 | cell-edit, table-locate | 2 | 2 | 2 | 1.00 | |
| docx-cell-03 | cell-edit, verify-safety | 2 | 2 | 2 | 1.00 | no explicit "check w:delText no longer holds the old value" (minor) |
| docx-relayout-01 | table-relayout | 3 | 3 | 3 | 1.00 | minor wrong detail: add_row gives cells "no w:tcPr … no widths" (it sets tcW from each gridCol) — an under-claim in the rubric's parenthetical, harmless because the recipe deep-copies a row, so treated as a sub-item; banding not mentioned |
| docx-relayout-02 | table-relayout | 3 | 3 | 2 | 0.67 | pt1, pt3 ok; pt2 wrong on the "mind gridSpan" clause: inserts the cloned tc at the same tc index in every row and asserts "every row's w:tc count equals the grid count" (wrong for gridSpan rows such as a merged Total; contradicts its own locate-02); minor: add_column "returns None" (returns `_Column`) |
| docx-relayout-03 | table-relayout, docx-model | 2 | 2 | 2 | 1.00 | |
| docx-tblins-01 | table-insert | 3 | 2 | 2 | 1.00 | minor: "LAST child of the body" (it goes before the final sectPr) |
| docx-tblins-02 | table-insert | 2 | 2 | 2 | 1.00 | fallback `doc.styles['Table Grid']` called "guaranteed-in-Word" (it raises KeyError if absent from styles.xml); appends tblBorders at the end of tblPr despite noting schema order |
| docx-tblins-03 | table-insert, table-relayout | 2 | 2 | 0 | 0.00 | pt1 wrong fact: add_table "gets **no explicit width at all** … tblW 0/auto … width decided by Word" minus "0.16\" of default cell padding" (python-docx writes gridCol = text width / cols and a dxa tcW on every cell); pt2 wrong fact: `col.width = … # writes w:gridCol + each w:tcW` (`_Column.width` sets gridCol only); copy recipe assigns `new._tbl.tblPr = …` and inserts a 2nd tblGrid without removing the first; header shading/run formatting not copied. The prose does say "must set BOTH gridCol and each cell's tcW", but the only concrete mechanisms given are the wrong `col.width` claim and the two-tblGrid recipe, so pt2 = 0 deliberately |
| docx-imgrep-01 | image-replace, docx-model | 3 | 3 | 1 | 0.33 | pt1 ok; pt2 wrong: recommends "swap the *bytes* of the existing part … `image_part._blob = new_bytes` is the sanctioned trick" instead of a new part + repointed r:embed (changes every picture sharing the part — contradicts its own imgrep-02); pt3 (drop old rel/media) absent; aspect formula `Emu(int(cy * cx / w))` mixes EMU and pixels |
| docx-imgrep-02 | image-replace | 2 | 2 | 2 | 1.00 | |
| docx-imgrep-03 | image-replace, table-locate | 2 | 2 | 2 | 1.00 | replaces by overwriting the header part's `_blob` (fine here since all pages should change); helper takes the first wp:extent of the whole part (minor bug) |
| docx-imgins-01 | image-insert | 3 | 2 | 2 | 1.00 | primary recipe = doc.add_picture then move the w:r, which leaves an empty trailing paragraph at the end of the body; alt `doc.part.relate_to(path, RT.IMAGE)` is wrong (relate_to takes a Part) |
| docx-imgins-02 | image-insert, table-relayout | 2 | 2 | 2 | 1.00 | |
| docx-legacy-01 | legacy-doc | 3 | 2 | 2 | 1.00 | "renaming doesn't help" not stated; error named `zipfile.BadZipFile` first (python-docx 1.2 raises PackageNotFoundError) (minor) |
| docx-legacy-02 | legacy-doc, verify-safety | 2 | 2 | 2 | 1.00 | |
| docx-legacy-03 | legacy-doc | 1 | 2 | 2 | 1.00 | Python snippet checks `report.docx` in cwd, not in the --outdir (minor bug) |
| docx-verify-01 | verify-safety | 3 | 2 | 2 | 1.00 | re-asked alone; no explicit "search every part for text that should be gone" (minor) |
| docx-verify-02 | verify-safety, docx-model | 2 | 2 | 2 | 1.00 | re-asked alone; broken vMerge and duplicate wp:docPr ids not named (duplicate w14:paraId given instead) |
| docx-verify-03 | verify-safety, image-replace | 2 | 2 | 2 | 1.00 | re-asked alone |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| docx-model | 0.88 | 7 | ok | **derive** |
| table-locate | 1.00 | 6 | ok | omit (strong) |
| row-delete | 1.00 | 4 | ok | omit (strong) |
| cell-edit | 1.00 | 4 | ok | omit (strong) |
| table-relayout | 0.79 | 6 | ok | **derive** |
| table-insert | 0.71 | 3 | low-n | **derive** (mark low-n) |
| image-replace | 0.78 | 4 | ok | **derive** |
| image-insert | 1.00 | 2 | low-n | omit (strong) |
| legacy-doc | 1.00 | 3 | low-n | omit (strong) |
| verify-safety | 1.00 | 6 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

- docx-model: (3+3+2+2+2+0.33×3+2) / 17 = 15 / 17
- table-relayout: (2+3+0.67×3+2+0+2) / 14 = 11 / 14
- table-insert: (3+2+0) / 7 = 5 / 7
- image-replace: (0.33×3+2+2+2) / 9 = 7 / 9

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 64 / 69 = 92.8%
```

(Σweight = 12×3 + 16×2 + 1×1 = 69; lost 1 on docx-relayout-02, 2 on docx-tblins-03, 2 on docx-imgrep-01.)

## Derivation targets

Tags below threshold (`< 0.9`): **docx-model**, **table-relayout**,
**table-insert (low-n)**, **image-replace** → feed into
`derived/docx.space-bunny-free.SKILL.md`.

### docx-model (0.88)
- Wrong recipe for the image-part model (imgrep-01, weight 3, 0.33): replaces
  ONE picture by overwriting the shared part's bytes ("`image_part._blob =
  new_bytes` is the sanctioned trick … Reusing the part is the cleanest way").
  Image parts are deduplicated by SHA1 across the whole package, so one part can
  back several blips, including a header's (verified on 1.2.0: the same PNG added
  in the body and a header resolves to one `/word/media/image1.png`). Overwriting
  the blob changes every one of them.
- Missing (imgrep-01): the old rel/media must be dropped when nothing references
  it any more. Not stated anywhere; `Part.drop_rel(rId)` counts only `r:id`
  attributes, so it deletes an image rel even while a blip still has `r:embed`
  pointing at it; count the `r:embed` references yourself first.
- Unit mix-up (imgrep-01): aspect fix `Emu(int(cy * cx / w))` multiplies EMU by
  EMU and divides by pixels; the right formula keeps cx and sets
  `cy = cx * px_height / px_width`.
- Minor stray API errors (not scored): model-03 "add_row()/add_column() accept a
  Length" (`add_row()` takes no arguments; `add_column(width)` requires one);
  model-01 overstates that a body `w:tbl` must be followed by a `w:p`.

### table-relayout (0.79)
- Wrong column-insert mechanics (relayout-02): inserts the cloned `w:tc` at the
  same tc index in every row and asserts "every row's `w:tc` count equals the grid
  count". A row with a `w:gridSpan` cell (merged Total) has fewer `w:tc` than
  `w:gridCol`, so the tc index is not the grid index: locate the cell whose grid
  offset equals the insert position, and widen (`gridSpan + 1`) a spanning cell
  that straddles it. The valid check is Σ gridSpan per row == gridCol count.
- Wrong API facts: `add_column` "returns None" (returns `_Column`, relayout-02);
  add_row cells get "no w:tcPr … no widths" (relayout-01; `add_row()` writes a
  dxa `tcW` from each `gridCol`); `_Column.width` "writes w:gridCol + each w:tcW"
  (tblins-03; gridCol only).
- 0 on tblins-03 (shared with table-insert): default width wrongly described as
  auto/"decided by Word" — see table-insert.

### table-insert (0.71, low-n)
- Wrong belief about the default width (tblins-03): "A table created with
  `doc.add_table(rows, cols)` gets **no explicit width at all** … `w:tblW w:w="0"
  w:type="auto"` … the final rendered width is decided by Word", minus "0.16\" of
  default cell padding". Actual (1.2.0): only tblW is auto; python-docx writes
  `w:gridCol w:w = text_width / cols` (e.g. 2880 twips each for 3 columns) and a
  `w:tcW type="dxa"` of the same value on every cell.
- Wrong belief about the column-width API (tblins-03): "`col.width = Inches(w_in)
  # writes w:gridCol + each w:tcW`". `_Column.width` sets only the gridCol; each
  cell's tcW must be set too (`cell.width = …` for every cell in the column),
  otherwise the default even-split tcW values stay and Word lays out from them.
- Broken copy-the-reference recipe (tblins-03): `new._tbl.tblPr = deepcopy(ref.tblPr)`
  (tblPr has no setter: AttributeError) and
  `new._tbl.insert(1, deepcopy(tblGrid))` without removing the existing tblGrid
  (two tblGrid = corrupt). Copying tblPr + tblGrid also leaves every new cell's
  tcW at the even split.
- Missing (tblins-03): header formatting of the existing table (cell shading in
  tcPr, bold/white run rPr) is never copied; text should go into runs with that
  formatting, not bare cell.text.
- Wrong fallback (tblins-02): `table.style = doc.styles['Table Grid']  #
  last-resort guaranteed-in-Word` — it raises the same KeyError when the template's
  styles.xml lacks "Table Grid"; the real last resort is explicit tblBorders. Its
  tblBorders snippet appends at the end of tblPr despite noting schema order
  (tblBorders goes after tblW/jc/tblCellSpacing/tblInd, before shd/tblLayout/
  tblCellMar/tblLook).
- Minor (tblins-01): add_table places the table "as the LAST child of the body";
  it is inserted before the final `w:sectPr`.

### image-replace (0.78)
- Wrong replacement method (imgrep-01, and the same `_blob` overwrite in
  imgrep-03): overwrite the part's bytes instead of adding a new image part
  (`rId, image = part.get_or_add_image(path)` on the part that owns the blip:
  document, header or footer) and repointing only that blip's `r:embed`. The model
  itself explains the SHA1 sharing in imgrep-02 but does not apply it.
- Missing (imgrep-01): drop the old rel when no `r:embed` in that part still uses
  it, so the old media leaves the package.
- Wrong aspect math (imgrep-01, see docx-model); minor (imgrep-03): the helper
  reads the first `wp:extent` of the whole header part rather than the extent of
  the drawing being replaced.

## Live behavioural probe — editing a real .docx (2026-09-28)

The model is driven with `ocode run -yolo` in a fresh git repo, one task per run, and the output is checked by `docs/okf/docx/probe/check.py`. "not run" means that model had no skill for that phase, or its provider was out of credit. The 0.9 runs used a binary built before two late skill edits (pdf `new_page(pno + 1)`, mimo pptx iteration 2).

| task | baseline | 0.75-era skill | 0.9 skill |
|---|---|---|---|
| delete-row | PASS (421s) | PASS (677s) | PASS (315s) |
| edit-cell | PASS (223s) | PASS (476s) | PASS (197s) |
| add-row | PASS (340s) | PASS (739s) | PASS (321s) |
| add-column | PASS (271s) | PASS (816s) | PASS (320s) |
| insert-table | PASS (342s) | PASS (652s) | PASS (266s) |
| rename-item | PASS (495s) | PASS (366s) | PASS (214s) |
| replace-image | PASS (936s) | PASS (203s) | PASS (612s) |
| insert-image | PASS (577s) | PASS (277s) | PASS (446s) |
