---
model_id: glm-5.3-flash
model_version: "5.3"
evaluated_via: ollama-cloud
evaluated_on: 2026-09-28
stack: pdf
stack_corpus_rev: 1
threshold: 0.9
---

# Scorecard — glm-5.3-flash on pdf

> Valid ONLY for `glm-5.3-flash` @ `5.3`. A version bump invalidates
> this scorecard — re-benchmark.

Answers: `pdf/answers/glm-5.3-flash.md` (closed-book, audited zero tool calls).
Graded against `pdf/questions.yaml` corpus_rev 1 (PyMuPDF facts verified on 1.27.1).
Grading rule applied: a `point` whose core concept is present earns full credit
(minor missing sub-items are noted); a `point` stated with a wrong PyMuPDF fact,
or satisfied only by a cover-up (paint/white box) instead of real removal, earns 0.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| pdf-model-01 | pdf-model | 3 | 2 | 2 | 1.00 | |
| pdf-model-02 | pdf-model, verify-safety | 3 | 2 | 2 | 1.00 | |
| pdf-model-03 | pdf-model | 2 | 2 | 2 | 1.00 | |
| pdf-model-04 | pdf-model | 2 | 2 | 2 | 1.00 | |
| pdf-locate-01 | table-locate | 3 | 2 | 2 | 1.00 | |
| pdf-locate-02 | table-locate | 2 | 2 | 2 | 1.00 | no uniqueness check of the hit (minor) |
| pdf-locate-03 | table-locate, cell-edit | 2 | 2 | 2 | 1.00 | |
| pdf-locate-04 | table-locate, table-relayout | 2 | 2 | 2 | 1.00 | |
| pdf-rowdel-01 | row-delete | 3 | 3 | 2 | 0.67 | removal point missed: primary method is "paint the whole affected band with the page background color (or redact it)"; claims "vector rules are NOT removed by default redactions — cover them with background paint" (wrong); paints the band *after* stamping the shifted copy |
| pdf-rowdel-02 | row-delete, cell-edit | 3 | 2 | 0.5 | 0.25 | wrong default: says `graphics=PDF_REDACT_LINE_ART_NONE` and rules/shading "left completely untouched by default" (actual default = LINE_ART_REMOVE_IF_COVERED); image-pixel default correct → partial only |
| pdf-rowdel-03 | row-delete | 2 | 2 | 1 | 0.50 | show_pdf_page+clip correct; old region only "painted/blanked", not removed → original text stays in the text layer (duplicated) |
| pdf-rowdel-04 | row-delete, verify-safety | 2 | 2 | 2 | 1.00 | |
| pdf-cell-01 | cell-edit | 3 | 3 | 2 | 0.67 | removal point missed: applies redactions "with defaults … line-art untouched so rules survive" (wrong); never passes graphics=0 / images=0 |
| pdf-cell-02 | cell-edit | 2 | 2 | 2 | 1.00 | no "verify neighbours after" (minor); repeats wrong "border lines survive by default" |
| pdf-cell-03 | cell-edit, table-relayout | 2 | 2 | 2 | 1.00 | no-exception not stated explicitly (implied by checking return ≥ 0) |
| pdf-cell-04 | cell-edit | 2 | 2 | 1 | 0.50 | baseline point correct; says top-left point makes text land "a full font-size too low" — it lands ~a line too HIGH |
| pdf-relayout-01 | table-relayout | 3 | 3 | 2 | 0.67 | never extends vertical rules through the new row strip (only redraws its top rule) |
| pdf-relayout-02 | table-relayout | 3 | 3 | 2 | 0.67 | no check that widest content fits the narrower columns (measure; font/wrap fallback) |
| pdf-relayout-03 | table-relayout, pdf-model | 2 | 2 | 2 | 1.00 | |
| pdf-relayout-04 | table-relayout, table-insert | 2 | 2 | 2 | 1.00 | |
| pdf-tblins-01 | table-insert | 3 | 2 | 2 | 1.00 | |
| pdf-tblins-02 | table-insert | 2 | 2 | 1 | 0.50 | no paint order (fills first, then text/rules) |
| pdf-tblins-03 | table-insert, table-relayout | 2 | 2 | 1 | 0.50 | "make room" = "white-out one paragraph (… or a white rect) and redraw it lower"; no restamp of everything below, no page-overflow handling |
| pdf-imgrep-01 | image-replace | 3 | 3 | 2 | 0.67 | wrong: "replace_image avoids that by pointing only this page at a new object" — replace_image is global, changes every use of the xref |
| pdf-imgrep-02 | image-replace, verify-safety | 2 | 2 | 2 | 1.00 | no "verify one image at that rect" (minor) |
| pdf-imgrep-03 | image-replace | 2 | 2 | 2 | 1.00 | misses inline image / annotation appearance (other causes given) |
| pdf-imgins-01 | image-insert | 3 | 2 | 2 | 1.00 | fixed 36pt page margin instead of content edges, but checks intersections and adjusts |
| pdf-imgins-02 | image-insert | 2 | 2 | 2 | 1.00 | |
| pdf-imgins-03 | image-insert, pdf-model | 2 | 2 | 2 | 1.00 | |
| pdf-fonts-01 | fonts | 3 | 2 | 2 | 1.00 | no glyph-coverage check (minor) |
| pdf-fonts-02 | fonts | 2 | 2 | 2 | 1.00 | |
| pdf-fonts-03 | fonts, cell-edit | 2 | 2 | 2 | 1.00 | |
| pdf-verify-01 | verify-safety | 3 | 3 | 2 | 0.67 | never says a render/visual check cannot tell an overlay from a real edit |
| pdf-verify-02 | verify-safety | 3 | 2 | 2 | 1.00 | |
| pdf-verify-03 | verify-safety | 2 | 2 | 2 | 1.00 | no signature awareness (minor) |
| pdf-verify-04 | verify-safety, pdf-model | 2 | 2 | 1 | 0.50 | verify-output point met; no "check return values in the script / fail fast" (e.g. insert_textbox < 0, empty search_for) |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| pdf-model | 0.94 | 7 | ok | omit (strong) |
| table-locate | 1.00 | 4 | ok | omit (strong) |
| row-delete | 0.58 | 4 | ok | **derive** |
| cell-edit | 0.73 | 7 | ok | **derive** |
| table-relayout | 0.81 | 7 | ok | **derive** |
| table-insert | 0.78 | 4 | ok | **derive** |
| image-replace | 0.86 | 3 | low-n | **derive** |
| image-insert | 1.00 | 3 | low-n | omit (strong) |
| fonts | 1.00 | 3 | low-n | omit (strong) |
| verify-safety | 0.88 | 7 | ok | **derive** |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 72.75 / 86 = 84.6%
```

## Live behavioural probe — editing a real PDF (2026-09-28)

Model driven with `ocode run -yolo` in a fresh git repo containing an invoice PDF: a ruled, shaded table, a
logo, and a page 2 that must stay unchanged. One task per run. The output is checked on the **text layer** by
`docs/okf/pdf/probe/check.py`, which rejects white-box overlays, so passing requires real removal, not just a
correct-looking render. Renders were also inspected by eye. The harness is in `docs/okf/pdf/probe/`.

| task | baseline | with skill (digest injected) |
|---|---|---|
| delete-row | PASS (387s) | PASS (5854s) |
| edit-cell | PASS (297s) | PASS (177s) |
| add-row | PASS (405s) | PASS (1007s) |
| add-column | PASS (619s) | PASS (171s) |
| insert-table | PASS (115s) | PASS (144s) |
| edit-cell-subset | PASS (255s) | PASS (122s) |
| replace-image | PASS (212s) | PASS (225s) |
| insert-image | PASS (118s) | PASS (101s) |

Run via ollama-cloud because the aihubmix balance was exhausted. Loaded the bundled `pdf` skill in all 8 runs. Baseline runs used a pre-fix binary, so no Kaizen digest was injected. The with-skill runs used a
binary built after the `ocode run` bundled-skills fix; digest injection was verified by having the model quote
the digest from its system prompt. Times above ~5,800s are wall clock across a host sleep; the session's
monotonic clock shows about 500s of actual work.


## Derivation targets

Tags below threshold (`< 0.9`): **row-delete, cell-edit, table-insert,
table-relayout, image-replace, verify-safety** → feed into
`derived/pdf.glm-5.3-flash.SKILL.md`.

Root cause shared by row-delete and cell-edit: the model believes
`apply_redactions()` leaves vector graphics alone by default, so it (a) never
passes `graphics=0` when it wants the rules/shading kept, and (b) falls back to
painting background-coloured boxes to "remove" rules, rows and paragraphs,
which is the white-box cover-up it correctly condemns in pdf-model-02. The same
cover-up reappears in table-insert (tblins-03).

### row-delete (0.58)
- Wrong default (rowdel-02): "Defaults … `graphics=PDF_REDACT_LINE_ART_NONE`" and
  "vector graphics / line-art (the table's rule lines, shading rectangles,
  borders) are left completely untouched by default". Actual (1.27.1):
  `images=PDF_REDACT_IMAGE_PIXELS (2)`, `graphics=PDF_REDACT_LINE_ART_REMOVE_IF_COVERED (1)`,
  `text=PDF_REDACT_TEXT_REMOVE (0)`: covered rules/shading ARE removed.
- Same error in the procedure (rowdel-01): "know vector rules are NOT removed by
  default redactions — cover them with background paint".
- Cover-up instead of removal (rowdel-01, rowdel-03): "paint the whole affected
  band … with the page background color (or redact it)"; "after
  painting/blanking the original band". Painting leaves the old rows' text in the
  text layer, so after a show_pdf_page shift every moved row is duplicated in
  extraction. Must redact (add_redact_annot + apply_redactions) the deleted row
  and the whole region being moved.
- Order bug (rowdel-01): stamps the shifted copy (step 3) then paints the band
  (step 4), which would cover the stamped copy. Remove first, then stamp.

### cell-edit (0.73)
- Wrong default again (cell-01): "`page.apply_redactions()` with defaults …
  line-art untouched so rules survive"; (cell-02): border lines "survive by
  default anyway". To keep rules/shading when editing a cell, pass
  `graphics=PDF_REDACT_LINE_ART_NONE` (0) and `images=PDF_REDACT_IMAGE_NONE` (0).
- Baseline direction wrong (cell-04): passing bbox top-left to `insert_text`
  makes the text land "a full font-size too low … descends below the intended
  line". It lands about one line too HIGH (the point is the baseline, glyphs
  are drawn above it).
- (rowdel-02 is also tagged cell-edit; see the default error above.)

### table-insert (0.78)
- No paint order (tblins-02): style sampling was correct, but it never says to
  draw cell fills first and text/rules after. A fill drawn after the text hides
  it (verified: the text renders invisible yet stays extractable).
- Cover-up to make room (tblins-03): "white-out one paragraph (redaction with
  white fill or a white rect) and redraw it lower". Only one paragraph is moved,
  not everything below the insertion point; a white rect is not removal; no
  handling of content pushed past the page bottom (spill to an inserted page).
  Correct: redact the whole region below the insertion point, restamp it lower
  with `show_pdf_page(..., clip=old_rect)`, and move overflow onto a new page
  (`doc.new_page(pno=...)`).

### table-relayout (0.81)
- Add row above Total (relayout-01): moves Total and content below and handles
  overflow, but only "redraw[s] its top rule" for the new strip. Never extends
  the vertical rules down through the new row, and never mentions keeping the
  alternating striping. Also says "after blanking the original band" rather
  than redacting it.
- Add column (relayout-02): redistributes widths within the margins and
  redraws everything, but never measures the widest existing text against the
  narrowed columns (`get_text_length`), and has no deliberate fallback (smaller
  font or wrap + taller row) when it does not fit.
- (tblins-03 is also tagged table-relayout; see the white-out error above.)

### image-replace (0.86, low-n)
- Factual error (imgrep-01): "replace_image avoids that by pointing only this
  page at a new object". `page.replace_image(xref, ...)` swaps the xref's
  object, so every page that uses that xref changes (verified on 1.27.1 with one
  xref on two pages). To replace one occurrence: redact its rect with
  `images=PDF_REDACT_IMAGE_REMOVE` (1), `graphics=0`, then `insert_image` the new
  file at that rect (verified: the other page keeps the old image).
- Minor (imgrep-02): no "verify exactly one image at that rect" after replacing.

### verify-safety (0.88)
- Overlay blind spot (verify-01): re-extraction and render checks are both
  listed, but it never says a render cannot tell a white-box overlay from a real
  edit (verified: a white rect over text renders identically to a blank page
  while `get_text`/`search_for` still find the text). The text-layer check is
  the one that proves removal.
- No fail-fast in the script (verify-04): lists silent failure modes but never
  says to check return values in the script itself: `insert_textbox` < 0 means
  nothing was written; `apply_redactions()` returns False when there were no
  redaction annots; `search_for` returns `[]` on a miss.
- Minor (verify-03): no signature awareness (editing breaks a digital signature).

## Live behavioural probe — with the 0.9-threshold skill (2026-09-28)

The model is driven with `ocode run -yolo` in a fresh git repo, one task per run, and the output is checked by `docs/okf/pdf/probe/check.py`. "not run" means that model had no skill for that phase, or its provider was out of credit. The 0.9 runs used a binary built before two late skill edits (pdf `new_page(pno + 1)`, mimo pptx iteration 2).

| task | with 0.9 skill |
|---|---|
| delete-row | not run |
| edit-cell | not run |
| add-row | not run |
| add-column | not run |
| insert-table | not run |
| edit-cell-subset | not run |
| replace-image | not run |
| insert-image | not run |
