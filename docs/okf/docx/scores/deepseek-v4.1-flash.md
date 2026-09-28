---
model_id: deepseek-v4.1-flash
model_version: "4.1"
evaluated_via: opencode-go
evaluated_on: 2026-09-28
stack: docx
stack_corpus_rev: 1
threshold: 0.9
---

# Scorecard — deepseek-v4.1-flash on docx

> Valid ONLY for `deepseek-v4.1-flash` @ `4.1`. A version bump invalidates
> this scorecard — re-benchmark.

Answers: `docx/answers/deepseek-v4.1-flash.md` (closed-book, audited zero tool calls).
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
| docx-locate-01 | table-locate | 3 | 2 | 2 | 1.00 | |
| docx-locate-02 | table-locate, docx-model | 2 | 2 | 2 | 1.00 | vMerge repeating the top cell not mentioned (minor) |
| docx-rowdel-01 | row-delete | 3 | 2 | 1 | 0.50 | remove `row._tr` correct; never says Word reflows (rows move up, no gap to close) nor that a dependent Total must be updated |
| docx-rowdel-02 | row-delete, table-locate | 2 | 2 | 2 | 1.00 | promotes next cell to restart but does not say to move the merged content into it (minor) |
| docx-rowdel-03 | row-delete, cell-edit | 2 | 2 | 2 | 1.00 | slight overstatement: updateFields "so Word recalculates fields when it opens the file" — Word prompts the user; primary fix (rewrite cached result) correct |
| docx-rowdel-04 | row-delete, verify-safety | 2 | 2 | 2 | 1.00 | greps document.xml only, not header/footer parts (minor) |
| docx-cell-01 | cell-edit | 3 | 2 | 2 | 1.00 | |
| docx-cell-02 | cell-edit, table-locate | 2 | 2 | 2 | 1.00 | repeats "updateFields so Word recomputes on open" (minor) |
| docx-cell-03 | cell-edit, verify-safety | 2 | 2 | 2 | 1.00 | notes delText can survive/orphan but no explicit "check w:delText no longer holds the old value" (minor) |
| docx-relayout-01 | table-relayout | 3 | 3 | 2 | 0.67 | add_row + deepcopy/addnext correct; third point missed: only "edit/clear its text" — no filling via existing runs, no banding/alternating-shading consideration |
| docx-relayout-02 | table-relayout | 3 | 3 | 3 | 1.00 | |
| docx-relayout-03 | table-relayout, docx-model | 2 | 2 | 2 | 1.00 | |
| docx-tblins-01 | table-insert | 3 | 2 | 2 | 1.00 | |
| docx-tblins-02 | table-insert | 2 | 2 | 2 | 1.00 | does not name "Table Grid" / an existing table's style explicitly, but "a style the template actually defines" + tblBorders covers it |
| docx-tblins-03 | table-insert, table-relayout | 2 | 2 | 2 | 1.00 | matches widths (autofit off, tblW, gridCol, tcW); omits reusing the table style and header formatting (minor) |
| docx-imgrep-01 | image-replace, docx-model | 3 | 3 | 2 | 0.67 | model + repoint correct; never removes the old rel/media, so the old image stays in the package. Also offers "replace *that part's* bytes" first — unsafe when the part is shared (it knows this in imgrep-02) |
| docx-imgrep-02 | image-replace | 2 | 2 | 2 | 1.00 | |
| docx-imgrep-03 | image-replace, table-locate | 2 | 2 | 2 | 1.00 | anchored (wp:anchor) pictures not mentioned; again suggests swapping the header part's bytes as an option (minor) |
| docx-imgins-01 | image-insert | 3 | 2 | 2 | 1.00 | |
| docx-imgins-02 | image-insert, table-relayout | 2 | 2 | 2 | 1.00 | `cell.width` can be None (minor) |
| docx-legacy-01 | legacy-doc | 3 | 2 | 2 | 1.00 | lists textutil as a converter without its lossiness here; no "convert back to .doc if needed"; positional `doc.tables[0]` (minor) |
| docx-legacy-02 | legacy-doc, verify-safety | 2 | 2 | 2 | 1.00 | |
| docx-legacy-03 | legacy-doc | 1 | 2 | 2 | 1.00 | |
| docx-verify-01 | verify-safety | 3 | 2 | 2 | 1.00 | no "search every XML part for text that should be gone" (minor) |
| docx-verify-02 | verify-safety, docx-model | 2 | 2 | 2 | 1.00 | duplicate wp:docPr ids not mentioned (minor) |
| docx-verify-03 | verify-safety, image-replace | 2 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| docx-model | 0.94 | 7 | ok | omit (strong) |
| table-locate | 1.00 | 6 | ok | omit (strong) |
| row-delete | 0.83 | 4 | ok | **derive** |
| cell-edit | 1.00 | 4 | ok | omit (strong) |
| table-relayout | 0.93 | 6 | ok | omit (strong) |
| table-insert | 1.00 | 3 | low-n | omit (strong) |
| image-replace | 0.89 | 4 | ok | **derive** |
| image-insert | 1.00 | 2 | low-n | omit (strong) |
| legacy-doc | 1.00 | 3 | low-n | omit (strong) |
| verify-safety | 1.00 | 6 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

Arithmetic: docx-model 16/17; row-delete 7.5/9; table-relayout 13/14;
image-replace 8/9; all others lose nothing.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 65.5 / 69 = 94.9%
```

(Losses: rowdel-01 −1.5, relayout-01 −1.0, imgrep-01 −1.0.)

## Derivation targets

Tags below threshold (`< 0.9`): **row-delete, image-replace** → feed into
`derived/docx.deepseek-v4.1-flash.SKILL.md`.

Above threshold, not derived: docx-model (0.94, lost only via imgrep-01, covered
under image-replace) and table-relayout (0.93, relayout-01 filled the copied row by
"edit/clear its text" with no word on writing through existing runs or alternating
row shading).

No wrong python-docx API facts were stated (no invented delete_row, correct
cell.text / add_row / add_column / add_table / add_picture behaviour).

### row-delete (0.83)
- Missing (rowdel-01): after `tr.getparent().remove(tr)` the rows below move up by
  themselves; there is no gap to close or row to shift.
- Missing (rowdel-01): a Total (or any value) derived from the deleted row is not
  updated by removal and must be recomputed and rewritten.
- Minor (rowdel-02): promotes the next cell to `vMerge restart` but does not say to
  move the merged content into it.
- Minor (rowdel-04): searches document.xml only, not header/footer parts, when
  proving the row is gone.

### image-replace (0.89)
- Missing (imgrep-01): never removes the old rel after repointing `r:embed`, so the
  old image stays in `word/media/`.
- Wrong first option (imgrep-01, imgrep-03): "replace *that part's* bytes" / "swap the
  targeted header image part's bytes", although image parts are shared (SHA1 dedupe)
  and overwriting changes every picture using the part, as the model itself says in
  imgrep-02.
- Missing (imgrep-03): anchored (`wp:anchor`) pictures, which `doc.inline_shapes` also
  does not list (it matches body `wp:inline` only).

## Live behavioural probe — editing a real .docx (2026-09-28)

The model is driven with `ocode run -yolo` in a fresh git repo, one task per run, and the output is checked by `docs/okf/docx/probe/check.py`. "not run" means that model had no skill for that phase, or its provider was out of credit. The 0.9 runs used a binary built before two late skill edits (pdf `new_page(pno + 1)`, mimo pptx iteration 2).

| task | baseline | 0.75-era skill | 0.9 skill |
|---|---|---|---|
| delete-row | PASS (177s) | not run | PASS (281s) |
| edit-cell | PASS (99s) | not run | PASS (164s) |
| add-row | PASS (542s) | not run | PASS (408s) |
| add-column | PASS (717s) | not run | PASS (459s) |
| insert-table | PASS (412s) | not run | PASS (265s) |
| rename-item | PASS (220s) | not run | PASS (453s) |
| replace-image | PASS (126s) | not run | no output (bash `||` false hard-block); rerun PASS (273s) |
| insert-image | PASS (193s) | not run | PASS (550s) |
