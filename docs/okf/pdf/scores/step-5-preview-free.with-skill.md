---
model_id: step-5-preview-free
model_version: "5-preview"
evaluated_via: opencode-go
evaluated_on: 2026-10-12
stack: pdf
stack_corpus_rev: 1
threshold: 0.9
validation: with-skill
---

# Scorecard — step-5-preview-free on pdf (with-skill validation)

> Valid ONLY for `step-5-preview-free` @ `5-preview`. A version bump invalidates
> this scorecard — re-benchmark.

> **WITH-SKILL VALIDATION RUN.** Answers: `pdf/answers/step-5-preview-free.with-skill.md`,
> produced closed-book with the current derived skill
> `derived/pdf.step-5-preview-free.SKILL.md` (`pdf-tuning-step-5-preview-free`)
> prepended to the `_prompts/pdf.md` sheet as active guidance (session audited:
> 1 user message, 1 assistant message, 0 tool calls). Baseline:
> `scores/step-5-preview-free.md`. No new derived skill is produced from this run.

> Target tags for this skill (baseline < 0.9): **pdf-model, row-delete,
> table-relayout, image-replace (low-n), image-insert (low-n)**.

Graded against `pdf/questions.yaml` @ corpus_rev 1 (PyMuPDF facts in the key
verified on 1.27.1). Grading rule applied (same strictness as the baseline): a
`point` whose core concept is genuinely present earns full credit (minor missing
sub-items are noted); a `point` stated together with a materially wrong claim
about the same construct is withheld and the note says "(judgment)". Points are
whole; fractions come only from a listed partial.

## History

- Earlier with-skill scorecard: none (this is the first with-skill validation for
  step-5-preview-free on pdf).

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| pdf-model-01 | pdf-model | 3 | 2 | 2 | 1.00 | content stream of BT/ET, Tj/TJ, re/l paths; "edit is geometric, not structural"; move everything below up |
| pdf-model-02 | pdf-model, verify-safety | 3 | 2 | 2 | 1.00 | |
| pdf-model-03 | pdf-model | 2 | 2 | 2 | 1.00 | Flate, split Tj/TJ, hex + font encoding, TJ kerning drift, Form XObject, Type 3/outlines all covered |
| pdf-model-04 | pdf-model | 2 | 2 | 2 | 1.00 | baseline rotation error gone: extraction bboxes and insert/draw rects are in the UNROTATED frame, `page.rect` only swaps width/height, convert with `rotation_matrix`/`derotation_matrix`; CropBox via `transformation_matrix` |
| pdf-locate-01 | table-locate | 3 | 2 | 2 | 1.00 | find_tables strategy lines/text; get_drawings rule clustering + words-by-centre fallback |
| pdf-locate-02 | table-locate | 2 | 2 | 2 | 1.00 | rule-bounded row band × table x-extent; never says to check the hit is unique (same as baseline) |
| pdf-locate-03 | table-locate, cell-edit | 2 | 2 | 2 | 1.00 | flag bits correct; `sRGB_to_rgb` named; origin as baseline; extract_font + insert_font(fontfile) for subset reuse |
| pdf-locate-04 | table-locate, table-relayout | 2 | 2 | 2 | 1.00 | |
| pdf-rowdel-01 | row-delete | 3 | 3 | 3 | 1.00 | now the direct form: redact row + moving region, `show_pdf_page(old_rect − (0,row_h,0,row_h), src, pno, clip=old_rect)` from an untouched copy; Total recomputed from remaining amounts and rewritten with graphics=0, images=0 |
| pdf-rowdel-02 | row-delete, cell-edit | 3 | 2 | 2 | 1.00 | baseline wrong image default corrected: images=PDF_REDACT_IMAGE_PIXELS (2) blanks pixels only, REMOVE (1) is opt-in; graphics=REMOVE_IF_COVERED (1), 2 = touched, 0 = keep; explicit graphics=0, images=0 for a text-only cell edit |
| pdf-rowdel-03 | row-delete | 2 | 2 | 2 | 1.00 | direct show_pdf_page from an untouched copy; "no temp document or temp page is needed"; redact first |
| pdf-rowdel-04 | row-delete, verify-safety | 2 | 2 | 2 | 1.00 | |
| pdf-cell-01 | cell-edit | 3 | 3 | 3 | 1.00 | tight rect + explicit `apply_redactions(graphics=0, images=0)`; x = right_edge − get_text_length; origin baseline; font/size/colour from the span |
| pdf-cell-02 | cell-edit | 2 | 2 | 2 | 1.00 | overlap rule correct; "verify neighbours after" not explicit (same as baseline). Loose inference (minor): says "brushing the cell's border erases it" right after stating graphics=1 removes only line art lying ENTIRELY inside the rect |
| pdf-cell-03 | cell-edit, table-relayout | 2 | 2 | 1 | 0.50 | point 1 withheld (judgment): negative return and no exception are right, but "does not draw the overflow — the box ends up empty or **partially filled**" is wrong: insert_textbox is all-or-nothing and writes nothing when the text does not fit (the baseline answer had this right). Point 2 earned: measure first, shrink loop, wrap with taller row, relayout; `insert_htmlbox` scaling not offered |
| pdf-cell-04 | cell-edit | 2 | 2 | 1 | 0.50 | point 1 ok (baseline origin, bottom-left, not top-left). Point 2 withheld (judgment): says passing the top-left corner makes the text "drop down… renders too **low**". Wrong direction: the baseline lands at the old glyphs' top, so the text draws about a line too HIGH (baseline answer had this right). Also `bbox.y1 − descender × size` has the sign backwards (descender is negative) |
| pdf-relayout-01 | table-relayout | 3 | 3 | 3 | 1.00 | Total moved + recomputed; two new rules + vertical extension + banding recheck; stays inside usable area / repaginate if space runs out |
| pdf-relayout-02 | table-relayout | 3 | 3 | 3 | 1.00 | baseline point 2 recovered: measures the widest text per column at its new width with get_text_length, shrink-or-wrap decided per column; redacts the whole old table and redraws header fill, shading, all rules at the new x. "Appending right overflows the margin" is not stated as the mistake (core of point 1, same total width, is present) |
| pdf-relayout-03 | table-relayout, pdf-model | 2 | 2 | 2 | 1.00 | baseline gap closed: "first always ask whether the source exists and regenerate from it"; lossy "even when the PDF originally came from Word or HTML" |
| pdf-relayout-04 | table-relayout, table-insert | 2 | 2 | 2 | 1.00 | text + drawings + images bboxes, free-band subtraction, pixmap ink scan cross-check |
| pdf-tblins-01 | table-insert | 3 | 2 | 2 | 1.00 | direct draw (fills → rules → text, checks insert_textbox return); reportlab/fpdf2 one-pager → show_pdf_page; insert_htmlbox not named |
| pdf-tblins-02 | table-insert | 2 | 2 | 2 | 1.00 | paint order fills → rules → text now stated; the baseline `draw_rect` default aside is gone |
| pdf-tblins-03 | table-insert, table-relayout | 2 | 2 | 2 | 1.00 | redact-then-show_pdf_page shift with page-overflow handling; new page / ask the user / never overlap; regenerate-from-source not offered (same as baseline) |
| pdf-imgrep-01 | image-replace | 3 | 3 | 3 | 1.00 | baseline gaps closed: `get_image_info(xrefs=True)`/`get_image_rects` + `page.replace_image(xref, filename=…)`; shared xref changes every page; single occurrence = redact with images=PDF_REDACT_IMAGE_REMOVE then insert_image; warns that delete_image blanks the logo everywhere; aspect-ratio stretch |
| pdf-imgrep-02 | image-replace, verify-safety | 2 | 2 | 2 | 1.00 | replace_image or redact+insert; "verify exactly one image at that rect" not stated (same as baseline) |
| pdf-imgrep-03 | image-replace | 2 | 2 | 2 | 1.00 | Form XObject (`get_xobjects`), inline image, vector paths, Type 3; annotation appearance stream not listed (same as baseline). For vector art it names get_drawings but gives no explicit "redact graphics then insert" step; its generic redact+insert path removes covered paths under the default graphics flag anyway (minor) |
| pdf-imgins-01 | image-insert | 3 | 2 | 2 | 1.00 | height = 150 × h/w; anchors to real content bboxes, intersection check; rotated page handled in the unrotated frame (baseline `page.rect` confusion gone) |
| pdf-imgins-02 | image-insert | 2 | 2 | 2 | 1.00 | baseline errors corrected: `xref = page.insert_image(...)` then `insert_image(rect, xref=xref)`; `garbage=4` merges identical objects; rejects the xref_set_key/`Do` hand-edit. Aside `len(doc.xref_length())` is wrong (xref_length returns an int) — not a rubric construct |
| pdf-imgins-03 | image-insert, pdf-model | 2 | 2 | 2 | 1.00 | overlay=False; rotated-frame/CropBox/zero rect; verify via get_image_info; y-origin mix-up not named explicitly |
| pdf-fonts-01 | fonts | 3 | 2 | 2 | 1.00 | coverage check via extract_font + fontTools cmap; embed the full font or closest base-14; no explicit render verification (same as baseline). Unverified aside: `Font.has_glyph` false negatives on Mac-only cmaps |
| pdf-fonts-02 | fonts | 2 | 2 | 2 | 1.00 | helv/hebo (+ heit/hebi) correct |
| pdf-fonts-03 | fonts, cell-edit | 2 | 2 | 2 | 1.00 | |
| pdf-verify-01 | verify-safety | 3 | 3 | 3 | 1.00 | re-extraction (old gone, new present, geometry), render + pixel diff, independent parser, qpdf --check, raw-byte search for the removed string (text can survive what a render shows); "other pages unchanged" and find_tables row equality not explicit (same leniency as baseline) |
| pdf-verify-02 | verify-safety | 3 | 2 | 2 | 1.00 | the baseline "linearize afterwards" aside is gone; raw-byte + decompressed-stream verification, other hiding places |
| pdf-verify-03 | verify-safety | 2 | 2 | 1 | 0.50 | point 2 lost again: nothing about leaving other pages untouched (page count, other pages' text), signatures or encryption; metadata appears only as "strip it if sensitive", not preserve-unless-told. Same loss as the baseline |
| pdf-verify-04 | verify-safety, pdf-model | 2 | 2 | 2 | 1.00 | silent-failure list incl. rotated-page off-page rect and overlay=False; "check return values" implied via the list rather than stated (same as baseline) |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | target? | BASELINE | delta |
|-----|---------:|--:|-------|---------|---------:|------:|
| pdf-model | 1.00 | 7 | ok | **yes** | 0.88 | +0.12 |
| table-locate | 1.00 | 4 | ok | no | 1.00 | 0.00 |
| row-delete | 1.00 | 4 | ok | **yes** | 0.85 | +0.15 |
| cell-edit | 0.88 | 7 | ok | no | 0.91 | −0.03 |
| table-relayout | 0.94 | 7 | ok | **yes** | 0.88 | +0.06 |
| table-insert | 1.00 | 4 | ok | no | 1.00 | 0.00 |
| image-replace | 1.00 | 3 | low-n | **yes** | 0.86 | +0.14 |
| image-insert | 1.00 | 3 | low-n | **yes** | 0.86 | +0.14 |
| fonts | 1.00 | 3 | low-n | no | 1.00 | 0.00 |
| verify-safety | 0.94 | 7 | ok | no | 0.94 | 0.00 |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

Arithmetic: pdf-model 16/16; table-locate 9/9; row-delete 10/10; cell-edit 14/16
(cell-03 −1.0, cell-04 −1.0); table-relayout 15/16 (cell-03 −1.0); table-insert
9/9; image-replace 7/7; image-insert 7/7; fonts 7/7; verify-safety 16/17
(verify-03 −1.0).

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 83 / 86 = 96.5%
```

(Losses: cell-03 −1.0, cell-04 −1.0, verify-03 −1.0.) Baseline 78.5 / 86 = 91.3%.

## Target-tag verdict

| tag | baseline | with-skill | pass (≥ 0.9)? |
|-----|---------:|-----------:|---------------|
| pdf-model | 0.88 | 1.00 | **PASS** |
| row-delete | 0.85 | 1.00 | **PASS** |
| table-relayout | 0.88 | 0.94 | **PASS** |
| image-replace (low-n) | 0.86 | 1.00 | **PASS** |
| image-insert (low-n) | 0.86 | 1.00 | **PASS** |

Every target tag reaches 0.9. The skill is validated for step-5-preview-free @
5-preview. Every concrete wrong claim listed under "Derivation targets" in the
baseline scorecard is absent from the with-skill answers: rotated-page
coordinates (model-04, imgins-01, imgins-03), the apply_redactions image default
and the explicit keep flags (rowdel-02), the temp-page detour (rowdel-01/03),
column relayout without measuring (relayout-02), regenerate-from-source
(relayout-03), `replace_image` and the single-occurrence swap (imgrep-01), and
`insert_image` xref reuse plus `garbage=4` dedup (imgins-02). table-relayout's
only remaining loss is the non-target cell-03 point shared with cell-edit.

Non-target drift (not a reason to edit the skill): cell-edit 0.91 → 0.88 on two
single-sample slips the baseline run did not make — cell-03 "empty or partially
filled" (insert_textbox writes nothing) and cell-04 "renders too low" (it is too
high). The skill body says nothing about insert_text baselines or insert_textbox
behaviour, so this is resampling noise; per HOW-TO-EVALUATE.md, catch it in a
fresh baseline before deriving anything. verify-safety is unchanged at 0.94
(same verify-03 loss as the baseline).

## Provenance

Answerer: `ocode run -m opencode-go/step-5-preview-free` closed-book in a scratch
directory with the `_prompts/pdf.md` sheet and the body of
`derived/pdf.step-5-preview-free.SKILL.md` prepended as active guidance; the
session sqlite shows exactly 1 user message, 1 assistant message and 0 tool
calls, so no repo file, answer key or web page was read. All 36 ids present, none
truncated. Graded 2026-10-12 against `pdf/questions.yaml` corpus_rev 1 by the
same rubric and strictness as `scores/step-5-preview-free.md`.
