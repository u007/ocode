---
model_id: glm-5.3-flash
model_version: "5.3"
evaluated_via: ollama-cloud
evaluated_on: 2026-09-28
stack: docx
stack_corpus_rev: 1
threshold: 0.9
---

# Scorecard — glm-5.3-flash on docx

> Valid ONLY for `glm-5.3-flash` @ `5.3`. A version bump invalidates
> this scorecard — re-benchmark.

Answers: `docx/answers/glm-5.3-flash.md` (closed-book, audited zero tool calls).
Graded against `docx/questions.yaml` corpus_rev 1 (python-docx facts verified on 1.2.0).
Grading rule applied: a `point` whose core concept is present earns full credit
(minor missing sub-items are noted); a `point` stated with a wrong API fact, or
satisfied only by a cover-up (hidden/white text, zero-height rows, white boxes),
earns 0. Equivalent correct approaches in another library earn the point.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| docx-model-01 | docx-model | 3 | 2 | 2 | 1.00 | |
| docx-model-02 | docx-model, table-locate | 3 | 2 | 2 | 1.00 | |
| docx-model-03 | docx-model, table-relayout | 2 | 2 | 2 | 1.00 | |
| docx-locate-01 | table-locate | 3 | 2 | 2 | 1.00 | no "assert exactly one match" (minor); no mention that header/footer tables are outside doc.tables |
| docx-locate-02 | table-locate, docx-model | 2 | 2 | 2 | 1.00 | uses tr's w:tc children directly, but no gridSpan-aware index mapping (minor); self-contradicts: count "equals the grid count" then "different rows can report different cell counts" |
| docx-rowdel-01 | row-delete | 3 | 2 | 1 | 0.50 | remove `row._tr` correct; never says Word reflows (no gap to close) nor that dependent totals must be updated |
| docx-rowdel-02 | row-delete, table-locate | 2 | 2 | 2 | 1.00 | promotes next cell to restart but does not move the restart cell's content down (only notes it is lost) |
| docx-rowdel-03 | row-delete, cell-edit | 2 | 2 | 2 | 1.00 | imprecise: `updateFields` "so Word recalculates when the document is opened" — Word only prompts the user |
| docx-rowdel-04 | row-delete, verify-safety | 2 | 2 | 2 | 1.00 | text search limited to word/document.xml, not headers/footers (minor) |
| docx-cell-01 | cell-edit | 3 | 2 | 1 | 0.50 | point 1 = 0 (wrong API fact): claims `cell.text` keeps "paragraph-level properties in w:pPr (alignment, spacing, and the paragraph's style reference)" — in 1.2.0 the setter calls `tc.clear_content()`, which removes every child but `w:tcPr`, so pPr is lost too; fix (edit first run) correct |
| docx-cell-02 | cell-edit, table-locate | 2 | 2 | 2 | 1.00 | |
| docx-cell-03 | cell-edit, verify-safety | 2 | 2 | 2 | 1.00 | no warning that old text survives in `w:delText` / paragraph.text skews with pending revisions, and no delText check (minor) |
| docx-relayout-01 | table-relayout | 3 | 3 | 3 | 1.00 | banding/alternating shading not mentioned (minor) |
| docx-relayout-02 | table-relayout | 3 | 3 | 3 | 1.00 | gridSpan cells (merged Total) not mentioned when inserting per-row tc (minor) |
| docx-relayout-03 | table-relayout, docx-model | 2 | 2 | 2 | 1.00 | |
| docx-tblins-01 | table-insert | 3 | 2 | 2 | 1.00 | |
| docx-tblins-02 | table-insert | 2 | 2 | 2 | 1.00 | |
| docx-tblins-03 | table-insert, table-relayout | 2 | 2 | 1 | 0.50 | point 1 = 0 (wrong API fact): "add_table() sets no explicit width … Word auto-fits the table based on content … can end up narrow" — 1.2.0 passes `_block_width`, splitting the text width evenly into gridCol + tcW; point 2 awarded on copying gridCol/tcW widths, but omits matching style and header formatting |
| docx-imgrep-01 | image-replace, docx-model | 3 | 3 | 2 | 0.67 | model + repoint correct (with single-reference check before overwriting the blob); never removes the old rel/media so the old image leaves the package |
| docx-imgrep-02 | image-replace | 2 | 2 | 2 | 1.00 | attributes the dedupe to "Word" rather than python-docx SHA1 dedupe (minor) |
| docx-imgrep-03 | image-replace, table-locate | 2 | 2 | 2 | 1.00 | anchored (wp:anchor) pictures not mentioned (minor) |
| docx-imgins-01 | image-insert | 3 | 2 | 2 | 1.00 | |
| docx-imgins-02 | image-insert, table-relayout | 2 | 2 | 2 | 1.00 | gives only the overflow outcome, not autofit column-stretch |
| docx-legacy-01 | legacy-doc | 3 | 2 | 2 | 1.00 | offers textutil as a fallback converter (lossy, see legacy-02); "renaming doesn't help" not stated |
| docx-legacy-02 | legacy-doc, verify-safety | 2 | 2 | 2 | 1.00 | |
| docx-legacy-03 | legacy-doc | 1 | 2 | 2 | 1.00 | |
| docx-verify-01 | verify-safety | 3 | 2 | 2 | 1.00 | no check that the rest of the document is unchanged / old text gone from all parts (minor) |
| docx-verify-02 | verify-safety, docx-model | 2 | 2 | 2 | 1.00 | duplicate docPr ids only implied ("duplicate ids") |
| docx-verify-03 | verify-safety, image-replace | 2 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| docx-model | 0.94 | 7 | ok | omit (strong) |
| table-locate | 1.00 | 6 | ok | omit (strong) |
| row-delete | 0.83 | 4 | ok | **derive** |
| cell-edit | 0.83 | 4 | ok | **derive** |
| table-relayout | 0.93 | 6 | ok | omit (strong) |
| table-insert | 0.86 | 3 | low-n | **derive** |
| image-replace | 0.89 | 4 | ok | **derive** |
| image-insert | 1.00 | 2 | low-n | omit (strong) |
| legacy-doc | 1.00 | 3 | low-n | omit (strong) |
| verify-safety | 1.00 | 6 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

Sums: docx-model 16/17; table-locate 14/14; row-delete 7.5/9; cell-edit 7.5/9;
table-relayout 13/14; table-insert 6/7; image-replace 8/9; image-insert 5/5;
legacy-doc 6/6; verify-safety 13/13.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 64.0 / 69 = 92.8%
```

## Derivation targets

Tags below threshold (`< 0.9`): **row-delete (0.83), cell-edit (0.83),
table-insert (0.86), image-replace (0.89)** → one corrective section each in
`derived/docx.glm-5.3-flash.SKILL.md`.

- **row-delete (0.83)**
  - Removal via `row._tr` correct, but never says the rows below close up on
    their own (no gap to shift) nor that dependent totals must be updated
    (docx-rowdel-01, 1/2).
  - vMerge: promotes the next cell to `restart` but does not move the deleted
    restart cell's content into it, only notes it is lost (docx-rowdel-02).
  - Fields: claims `<w:updateFields w:val="true"/>` makes "Word recalculate when
    the document is opened" — it only prompts; the fix is writing the new total
    into the cached result run (docx-rowdel-03).
- **cell-edit (0.83)**
  - Wrong API fact: `cell.text = …` keeps paragraph pPr (alignment, spacing,
    style). In 1.2.0 the setter calls `tc.clear_content()`, removing every child
    but `w:tcPr`, so pPr is lost too (docx-cell-01, point 1 = 0).
  - Track Changes: no warning that the old value survives in `w:delText`
    (not visible through `paragraph.text`), and no delText check (docx-cell-03).
- **table-insert (0.86)**
  - Wrong API fact: "add_table() sets no explicit width … Word auto-fits the table
    based on content". 1.2.0 splits the section text width evenly into every
    `gridCol` and every cell's `tcW` (dxa) (docx-tblins-03, point 1 = 0).
  - Matching an existing table: copies gridCol/tcW widths but omits reusing its
    style and header formatting (shading, run bold/colour) (docx-tblins-03).
- **image-replace (0.89)**
  - Never removes the old relationship/media after repointing the blip, so the
    old image stays in the package (docx-imgrep-01, point 3 = 0).
  - Attributes identical-image sharing to "Word" rather than python-docx's
    SHA1 dedupe in `get_or_add_image` (docx-imgrep-02, minor).
  - Floating `wp:anchor` pictures not considered when locating the target
    (docx-imgrep-03, minor).

Tags at/above 0.9 (docx-model 0.94, table-relayout 0.93, and the 1.00 tags) get
no section.

## Live behavioural probe — editing a real .docx (2026-09-28)

The model is driven with `ocode run -yolo` in a fresh git repo, one task per run, and the output is checked by `docs/okf/docx/probe/check.py`. "not run" means that model had no skill for that phase, or its provider was out of credit. The 0.9 runs used a binary built before two late skill edits (pdf `new_page(pno + 1)`, mimo pptx iteration 2).

| task | baseline | 0.75-era skill | 0.9 skill |
|---|---|---|---|
| delete-row | PASS (266s) | not run | not run |
| edit-cell | PASS (176s) | not run | not run |
| add-row | no result (provider 429) | not run | not run |
| add-column | no result (provider 429) | not run | not run |
| insert-table | no result (provider 429) | not run | not run |
| rename-item | no result (provider 429) | not run | not run |
| replace-image | no result (provider 429) | not run | not run |
| insert-image | no result (provider 429) | not run | not run |
