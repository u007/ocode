---
model_id: deepseek-v4.1-flash
model_version: "4.1"
evaluated_via: opencode-go
evaluated_on: 2026-09-28
stack: pdf
stack_corpus_rev: 1
threshold: 0.9
sample: full   # all 36 questions
---

# Scorecard — deepseek-v4.1-flash on pdf

> Valid ONLY for `deepseek-v4.1-flash` @ `4.1`. A version bump invalidates
> this scorecard — re-benchmark.

Answers: `../answers/deepseek-v4.1-flash.md`, produced closed-book (answerer saw
only `_prompts/pdf.md`; audited zero tool calls). Graded against
`questions.yaml` (corpus_rev 1; PyMuPDF facts verified on 1.27.1). Per the corpus
header, an equivalent correct technique in another library earns the point;
wrong PyMuPDF behaviour claims do not.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| pdf-model-01 | pdf-model | 3 | 2 | 2 | 1.00 | |
| pdf-model-02 | pdf-model, verify-safety | 3 | 2 | 2 | 1.00 | also notes redaction fill default |
| pdf-model-03 | pdf-model | 2 | 2 | 2 | 1.00 | |
| pdf-model-04 | pdf-model | 2 | 2 | 2 | 1.00 | |
| pdf-locate-01 | table-locate | 3 | 2 | 2 | 1.00 | |
| pdf-locate-02 | table-locate | 2 | 2 | 2 | 1.00 | |
| pdf-locate-03 | table-locate, cell-edit | 2 | 2 | 2 | 1.00 | |
| pdf-locate-04 | table-locate, table-relayout | 2 | 2 | 2 | 1.00 | |
| pdf-rowdel-01 | row-delete | 3 | 3 | 2 | 0.67 | redacts the row "sized to the row only", then stamps the content below one row higher with show_pdf_page without removing the original, so the content appears twice |
| pdf-rowdel-02 | row-delete, cell-edit | 3 | 2 | 1 | 0.50 | **factual error**: says the default is `graphics=PDF_REDACT_LINE_ART_NONE` (the real default is REMOVE_IF_COVERED, which deletes covered rules and shading). Images=PIXELS is right, and it does say to pass options explicitly |
| pdf-rowdel-03 | row-delete | 2 | 2 | 1 | 0.50 | show_pdf_page + clip from a copy is right, but it never says to redact the old region first, so the old region is duplicated |
| pdf-rowdel-04 | row-delete, verify-safety | 2 | 2 | 2 | 1.00 | |
| pdf-cell-01 | cell-edit | 3 | 3 | 3 | 1.00 | right-aligns to `cell_right` (no padding) here, though locate-04 gets the padding right; "explicit image/graphics options" is vague |
| pdf-cell-02 | cell-edit | 2 | 2 | 2 | 1.00 | the ~1pt epsilon pad is bounded by the neighbour; doesn't say to re-check neighbours afterwards |
| pdf-cell-03 | cell-edit, table-relayout | 2 | 2 | 1 | 0.50 | **factual error**: "writes only what fits and the overflow is lost" (it actually writes nothing). Options list is good |
| pdf-cell-04 | cell-edit | 2 | 2 | 2 | 1.00 | |
| pdf-relayout-01 | table-relayout | 3 | 3 | 3 | 1.00 | |
| pdf-relayout-02 | table-relayout | 3 | 3 | 2 | 0.67 | never removes the old table (no redact) before redrawing at the new x; no mention of shading |
| pdf-relayout-03 | table-relayout, pdf-model | 2 | 2 | 1 | 0.50 | risks are thorough, but it never suggests regenerating from the original source; calls the round-trip "acceptable" for simple docs rather than a last resort |
| pdf-relayout-04 | table-relayout, table-insert | 2 | 2 | 2 | 1.00 | |
| pdf-tblins-01 | table-insert | 3 | 2 | 2 | 1.00 | |
| pdf-tblins-02 | table-insert | 2 | 2 | 1 | 0.50 | missing the paint order inside the new table (fills first, then text/rules); repeats the "insert_textbox silently truncates" error |
| pdf-tblins-03 | table-insert, table-relayout | 2 | 2 | 2 | 1.00 | says nothing about moved content overflowing the page |
| pdf-imgrep-01 | image-replace | 3 | 3 | 3 | 1.00 | writes `doc.replace_image` (the API is `Page.replace_image`); concept correct |
| pdf-imgrep-02 | image-replace, verify-safety | 2 | 2 | 2 | 1.00 | doesn't say to verify there is exactly one image at that rect |
| pdf-imgrep-03 | image-replace | 2 | 2 | 2 | 1.00 | |
| pdf-imgins-01 | image-insert | 3 | 2 | 1 | 0.50 | anchors to `page.rect` minus a hard-coded 36pt margin, not to the real content edges (table x1, footer top); aspect ratio and the collision check are right |
| pdf-imgins-02 | image-insert | 2 | 2 | 1 | 0.50 | xref reuse is right; never mentions a `garbage>=3` dedup save |
| pdf-imgins-03 | image-insert, pdf-model | 2 | 2 | 2 | 1.00 | |
| pdf-fonts-01 | fonts | 3 | 2 | 2 | 1.00 | |
| pdf-fonts-02 | fonts | 2 | 2 | 2 | 1.00 | |
| pdf-fonts-03 | fonts, cell-edit | 2 | 2 | 2 | 1.00 | |
| pdf-verify-01 | verify-safety | 3 | 3 | 2 | 0.67 | re-extract and render are covered, but it never says a render alone can't tell an overlay from a real edit |
| pdf-verify-02 | verify-safety | 3 | 2 | 2 | 1.00 | |
| pdf-verify-03 | verify-safety | 2 | 2 | 2 | 1.00 | |
| pdf-verify-04 | verify-safety, pdf-model | 2 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| table-locate | 1.00 | 4 | ok | omit |
| image-replace | 1.00 | 3 | low-n | omit |
| fonts | 1.00 | 3 | low-n | omit |
| verify-safety | 0.94 | 7 | ok | omit |
| pdf-model | 0.94 | 7 | ok | omit |
| table-insert | 0.89 | 4 | ok | **derive** |
| cell-edit | 0.84 | 7 | ok | **derive** |
| table-relayout | 0.81 | 7 | ok | **derive** |
| row-delete | 0.65 | 4 | ok | **derive** |
| image-insert | 0.64 | 3 | low-n | **derive** (low-n) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 75.0 / 86 = 87.2%
```

## Live behavioural probe — editing a real PDF (2026-09-28)

Model driven with `ocode run -yolo` in a fresh git repo containing an invoice PDF: a ruled, shaded table, a
logo, and a page 2 that must stay unchanged. One task per run. The output is checked on the **text layer** by
`docs/okf/pdf/probe/check.py`, which rejects white-box overlays, so passing requires real removal, not just a
correct-looking render. Renders were also inspected by eye. The harness is in `docs/okf/pdf/probe/`.

| task | baseline | with skill (digest injected) |
|---|---|---|
| delete-row | PASS (292s) | PASS (5833s) |
| edit-cell | PASS (187s) | PASS (176s) |
| add-row | PASS (519s) | PASS (289s) |
| add-column | FAIL (152s) | PASS (161s) |
| insert-table | PASS (200s) | PASS (92s) |
| edit-cell-subset | PASS (151s) | PASS (167s) |
| replace-image | PASS (390s) | PASS (352s) |
| insert-image | PASS (157s) | PASS (154s) |

Baseline add-column: the new "SKU" header was drawn black on the navy header (every other header is white). Only the header-style check failed. Never loaded the bundled `pdf` skill. Baseline runs used a pre-fix binary, so no Kaizen digest was injected. The with-skill runs used a
binary built after the `ocode run` bundled-skills fix; digest injection was verified by having the model quote
the digest from its system prompt. Times above ~5,800s are wall clock across a host sleep; the session's
monotonic clock shows about 500s of actual work.

## Derivation targets

Tags below threshold (`< 0.9`): **row-delete (0.65), image-insert (0.64, low-n),
table-relayout (0.81), cell-edit (0.84), table-insert (0.89)** → feed into
`derived/pdf.deepseek-v4.1-flash.SKILL.md`. PyMuPDF claims below were re-checked
on 1.27.1.

### row-delete (0.65)

- **Wrong `apply_redactions` default (pdf-rowdel-02).** Quote: "graphics =
  PDF_REDACT_LINE_ART_NONE, i.e. vector line art is left untouched". This is
  false. The signature is `apply_redactions(images=2, graphics=1, text=0)`:
  `graphics=PDF_REDACT_LINE_ART_REMOVE_IF_COVERED` deletes shading and rules lying
  entirely inside the rect (a partly covered rule stays whole), and
  `images=PDF_REDACT_IMAGE_PIXELS` blanks overlapping pixels. To keep them, pass
  `graphics=0, images=0`. If the region is being rebuilt anyway, keep the
  defaults and redraw.
- **Region move without removing the source (pdf-rowdel-01, pdf-rowdel-03).**
  Quotes: "sized to the row only so neighbouring rows ... are untouched", then
  "stamp it into a target rect translated upward". The rows below are still on
  the page, so stamping a shifted copy duplicates them. Redact the deleted row
  AND the whole region that will move (or the whole table) first. Then stamp the
  region from an untouched copy of the source doc
  (`show_pdf_page(clip=...)`) one row higher, or redraw it.

### image-insert (0.64, low-n)

- **Placement from page edges, not content (pdf-imgins-01).** Quote:
  `x1 = page.rect.x1 - margin` with `margin = 36.0`. Anchor to the real
  content: right edge = the table's or text's x1, bottom = above the footer's
  top plus padding. Then check the rect against the text, drawing and image
  bboxes.
- **No dedup save (pdf-imgins-02).** The `xref=` reuse was right, but the
  answer never mentions a dedup save (`garbage>=3` per the rubric, plus
  `deflate`) that merges identical image streams and also fixes a file that is
  already bloated. Local check on 1.27.1: three identical images merged from
  separate docs stayed three streams at `garbage=3` and became one at
  `garbage=4`, so the skill names `garbage=4`.

### table-relayout (0.81)

- **Old table not removed before the redraw (pdf-relayout-02).** The re-layout
  plan ("move or redraw the vertical rules at the new x positions, reflow each
  existing cell's content") never redacts the old table, so the old text stays
  in the text layer under the redraw; it also never mentions redrawing the
  header fill and row shading at the new x positions.
- **Round-trip treated as acceptable (pdf-relayout-03).** Quote: "Acceptable
  when the document is simple and semantically structured". It is a last
  resort. The answer never says to ask for and regenerate from the original
  source (template, HTML, reportlab code, spreadsheet) first, then to edit the
  PDF surgically in place.
- **`insert_textbox` overflow (pdf-cell-03).** See cell-edit.
- Minor (full marks, noted): moving content down never considers content
  pushed past the page bottom (pdf-tblins-03).

### cell-edit (0.84)

- **`insert_textbox` overflow (pdf-cell-03).** Quote: "writes only what fits
  and the overflow is lost". False: when the text doesn't fit, it writes
  **nothing** and returns a negative number, with no exception. The return
  value must be checked. `insert_htmlbox` (default `scale_low=0`) shrinks the
  text to fit and returns `(spare_height, scale)`.
- **Wrong redaction default (pdf-rowdel-02).** Same error as row-delete: a
  cell redaction with defaults deletes the cell's shading when it lies inside
  the rect. Pass `graphics=0, images=0` to keep the grid.
- Minor (full marks, noted): right-aligns to `cell_right` with no padding
  (pdf-cell-01; the right edge is the existing value's x1); "explicit
  image/graphics options" never names the values; a ~1pt padded redaction rect
  with no re-check of the neighbouring cells afterwards (pdf-cell-02).

### table-insert (0.89)

- **No paint order (pdf-tblins-02).** Never says to draw fills first and then
  text and rules. A fill drawn after the text hides it in the render, while the
  text stays in the text layer.
- **`insert_textbox` overflow repeated (pdf-tblins-02).** Quote: "don't let
  `insert_textbox` silently truncate". It writes nothing, not a truncated
  string.

## Live behavioural probe — with the 0.9-threshold skill (2026-09-28)

The model is driven with `ocode run -yolo` in a fresh git repo, one task per run, and the output is checked by `docs/okf/pdf/probe/check.py`. "not run" means that model had no skill for that phase, or its provider was out of credit. The 0.9 runs used a binary built before two late skill edits (pdf `new_page(pno + 1)`, mimo pptx iteration 2).

| task | with 0.9 skill |
|---|---|
| delete-row | PASS (202s) |
| edit-cell | PASS (94s) |
| add-row | FAIL: horizontal rules = rows+1 (11) |
| add-column | PASS (246s) |
| insert-table | PASS (161s) |
| edit-cell-subset | PASS (419s) |
| replace-image | PASS (132s) |
| insert-image | PASS (187s) |
