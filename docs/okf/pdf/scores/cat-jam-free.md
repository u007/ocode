---
model_id: cat-jam-free
model_version: "alpha"
evaluated_via: opencode-go
evaluated_on: 2026-10-14
stack: pdf
stack_corpus_rev: 1
threshold: 0.9
---

<!-- Filename: model_id with "/" flattened to "__" so it is one valid path
     segment. `cat-jam-free` has no slash, so the filename is unchanged. -->

# Scorecard — cat-jam-free on pdf

> Valid ONLY for `cat-jam-free` @ `alpha`. A version bump invalidates
> this scorecard — re-benchmark.

Answers: `pdf/answers/cat-jam-free.md` (closed-book, see Provenance).
Graded against `pdf/questions.yaml` @ corpus_rev 1. PyMuPDF facts in the key
were verified on 1.27.1; answers stating a wrong PyMuPDF behaviour do not earn
the affected point. Where the answer states a concept correctly but also makes a
materially wrong claim about the same construct, the point is withheld and the
note says "(judgment)".

## Provenance

Answerer: `ocode run -m opencode-go/cat-jam-free -effort med` in an empty
scratch directory with only the `_prompts/pdf.md` sheet attached. The session
sqlite shows exactly 1 user message, 1 assistant message and 0 tool calls, so
no repo file, answer key or web page was read. Headless runs inject ocode's
bundled skill-catalogue digest into the system prompt. The model publishes no
version (opencode-go free/alpha model) and self-identifies as "CatJam,
developed by LitterBox AI"; `model_version` is recorded as `alpha`. The model
emitted one stray line of thinking-aloud text after the closing YAML fence
("Need final no extra."); the harvester excluded it. It does not affect any of
the 36 graded entries.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| pdf-model-01 | pdf-model | 3 | 2 | 2 | 1.00 | "remove or cover that painted content" hedges toward overlay, but shift-below + update totals present |
| pdf-model-02 | pdf-model, verify-safety | 3 | 2 | 2 | 1.00 | |
| pdf-model-03 | pdf-model | 2 | 2 | 2 | 1.00 | split TJ, subset glyph ids, compression all covered; no remedy (parse tokens / redact+insert) offered |
| pdf-model-04 | pdf-model | 2 | 2 | 1 | 0.50 | y-flip ok. Rotation point withheld (judgment): "PyMuPDF generally normalizes so coordinates refer to the oriented page view". Verified on 1.27.1: `get_text` bboxes and insert/draw rects stay in the UNROTATED page frame; only `page.rect` swaps. No `rotation_matrix`/`derotation_matrix`; CropBox advice vague. Writes "PyMuFITZ" throughout |
| pdf-locate-01 | table-locate | 3 | 2 | 2 | 1.00 | |
| pdf-locate-02 | table-locate | 2 | 2 | 2 | 1.00 | never checks the hit is unique |
| pdf-locate-03 | table-locate, cell-edit | 2 | 2 | 2 | 1.00 | implies `origin` only in rawdict (dict spans carry it too) — minor |
| pdf-locate-04 | table-locate, table-relayout | 2 | 2 | 2 | 1.00 | |
| pdf-rowdel-01 | row-delete | 3 | 3 | 2 | 0.67 | point 1 withheld: redacts only the deleted row, then "shift all content below upward" with no removal of the content that moves — the old rows would stay and duplicate. Redraw-with-striping and recompute Total present |
| pdf-rowdel-02 | row-delete, cell-edit | 3 | 2 | 1 | 0.50 | point 1 withheld (judgment): never states the defaults; says graphics that "intersect" the rect are removed (default `REMOVE_IF_COVERED` removes only fully covered line art; images default to PIXELS blanking). Point 2 earned loosely via "choose conservative graphics handling"; never names `graphics=0` / `images=0` |
| pdf-rowdel-03 | row-delete | 2 | 2 | 0.5 | 0.25 | partial. **Wrong**: "PyMuPDF has no native move… that preserves editable vector text"; proposes pixmap or Form XObject that "may flatten content into an image". `show_pdf_page(clip=…)` from an untouched copy keeps vector text extractable. Never says to redact the old region first |
| pdf-rowdel-04 | row-delete, verify-safety | 2 | 2 | 2 | 1.00 | striping not mentioned here (covered in rowdel-01) |
| pdf-cell-01 | cell-edit | 3 | 3 | 3 | 1.00 | tight glyph rect; never passes `graphics=0, images=0` (works under default only because shading isn't fully covered) |
| pdf-cell-02 | cell-edit | 2 | 2 | 2 | 1.00 | no explicit "verify neighbours afterwards" |
| pdf-cell-03 | cell-edit, table-relayout | 2 | 2 | 1 | 0.50 | point 1 withheld: **wrong** — "insert_textbox returns the number of characters that did not fit; zero means all text fit". It returns a negative height shortfall and writes nothing. Options list (shrink, wrap, widen) ok; no `insert_htmlbox` scaling |
| pdf-cell-04 | cell-edit | 2 | 2 | 2 | 1.00 | hedges "too high or too low" but explains ascent and uses span origin |
| pdf-relayout-01 | table-relayout | 3 | 3 | 3 | 1.00 | Total value not explicitly recomputed; striping colour of the new row not mentioned |
| pdf-relayout-02 | table-relayout | 3 | 3 | 1 | 0.33 | point 1 earned (shrink existing columns, don't append past margin) though it also offers "add a page with larger effective width". Point 2 lost: no measuring text at the new widths, no shrink/wrap plan. Point 3 lost: only draws a new rule and new cells — no redaction of the old table, no redraw of all columns/header/shading at new x |
| pdf-relayout-03 | table-relayout, pdf-model | 2 | 2 | 1 | 0.50 | fidelity risks good; calls it acceptable for "simple" documents; never says prefer regenerating from original source / ask for it |
| pdf-relayout-04 | table-relayout, table-insert | 2 | 2 | 2 | 1.00 | includes annotations — good |
| pdf-tblins-01 | table-insert | 3 | 2 | 2 | 1.00 | reportlab → `show_pdf_page` ok; also suggests `insert_image` for the overlay; never says text stays real |
| pdf-tblins-02 | table-insert | 2 | 2 | 2 | 1.00 | paint order not stated; render-to-verify present |
| pdf-tblins-03 | table-insert, table-relayout | 2 | 2 | 1 | 0.50 | point 1 withheld: "moving later content down (difficult and risky)" with no redact + restamp mechanism and no overflow-to-new-page plan. Point 2 earned via whitespace / new page; no "tell the user", no "never overlap"; "overlaying with a separate page" is unclear |
| pdf-imgrep-01 | image-replace | 3 | 3 | 1 | 0.33 | does not know `page.replace_image(xref, filename=…)`; does insert_image over the rect then "remove or redact the old image XObject". Point 2 earned (shared xref changes every use; redact + insert for one placement). Point 3 lost: no aspect-ratio / stretch caveat |
| pdf-imgrep-02 | image-replace, verify-safety | 2 | 2 | 2 | 1.00 | no "verify exactly one image at that rect" |
| pdf-imgrep-03 | image-replace | 2 | 2 | 2 | 1.00 | |
| pdf-imgins-01 | image-insert | 3 | 2 | 2 | 1.00 | anchors to page width/height − margin rather than real content edges, then collision-checks |
| pdf-imgins-02 | image-insert | 2 | 2 | 2 | 1.00 | "using an existing xref" + dedup optimizer earn both points loosely; never names `insert_image(..., xref=)` or `garbage=4` |
| pdf-imgins-03 | image-insert, pdf-model | 2 | 2 | 2 | 1.00 | |
| pdf-fonts-01 | fonts | 3 | 2 | 2 | 1.00 | no render verification |
| pdf-fonts-02 | fonts | 2 | 2 | 2 | 1.00 | limitation correct; no "embed a TTF" remedy |
| pdf-fonts-03 | fonts, cell-edit | 2 | 2 | 2 | 1.00 | |
| pdf-verify-01 | verify-safety | 3 | 3 | 2 | 0.67 | point 3 lost: never says a visual check can't tell an overlay from a real edit (renders first, as primary check); other pages' text not checked |
| pdf-verify-02 | verify-safety | 3 | 2 | 2 | 1.00 | |
| pdf-verify-03 | verify-safety | 2 | 2 | 2 | 1.00 | page counts/metadata/permissions present; signatures and encryption not mentioned |
| pdf-verify-04 | verify-safety, pdf-model | 2 | 2 | 1 | 0.50 | point 1 earned (off-page coords, missing fonts, unremoved content, overlap). Point 2 lost: "explicit output checks" only; no checking return values / failing loudly in the script (`insert_textbox` < 0, empty `search_for`) |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| pdf-model | 0.81 | 7 | ok | **derive** |
| table-locate | 1.00 | 4 | ok | omit (strong) |
| row-delete | 0.60 | 4 | ok | **derive** |
| cell-edit | 0.84 | 7 | ok | **derive** |
| table-relayout | 0.69 | 7 | ok | **derive** |
| table-insert | 0.89 | 4 | ok | **derive** (borderline: 8/9) |
| image-replace | 0.71 | 3 | low-n | **derive** (mark low-n) |
| image-insert | 1.00 | 3 | low-n | omit (strong) |
| fonts | 1.00 | 3 | low-n | omit (strong) |
| verify-safety | 0.88 | 7 | ok | **derive** (borderline: 15/17) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.
Exact fractions: pdf-model 13/16, row-delete 6/10, cell-edit 13.5/16,
table-relayout 11/16, table-insert 8/9, image-replace 5/7, verify-safety 15/17.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 72 / 86 = 83.7%
```

## Derivation targets

Tags below threshold (`< 0.9`): **pdf-model, row-delete, cell-edit,
table-relayout, table-insert, image-replace (low-n), verify-safety** → feed into
`derived/pdf.cat-jam-free.SKILL.md`.

**pdf-model (0.81)** — rotated pages "normalized to the oriented view" (false:
extraction and insertion share the unrotated frame); round-trip answer never
prefers regenerating from source; silent-failure answer does not check return
values.

**row-delete (0.60)** — redacts only the deleted row and then "shifts" the rows
below without removing them; does not know the `apply_redactions` defaults or
the `graphics=0`/`images=0` keep flags; believes no region move preserves
vector text and reaches for a pixmap, missing
`show_pdf_page(new_rect, src_copy, pno, clip=old_rect)`.

**cell-edit (0.84)** — `insert_textbox` return value wrong (claims "characters
that did not fit, 0 = success"; real: negative height shortfall, nothing
written); redaction defaults unknown (shared with row-delete).

**table-relayout (0.69)** — adding a column: no measuring at new widths, no
redaction of the old table, no full redraw; no-gap insertion has no
redact-and-restamp mechanism; round-trip deemed acceptable for "simple"
documents without asking for the source.

**table-insert (0.89, borderline)** — no-gap insertion (shared with
table-relayout): no concrete way to move content down, no overflow page, no
"tell the user"/"never overlap".

**image-replace (0.71, low-n)** — does not know `page.replace_image`; no
aspect-ratio caveat.

**verify-safety (0.88, borderline)** — leads with a rendered visual check and
never says a render can't distinguish an overlay from a real edit; no check of
other pages' text; script-level return-value checks missing.

Contamination check: all 36 ids present, none truncated. Answers are short,
single-paragraph prose in the model's own wording, consistently misspell
PyMuPDF as "PyMuFITZ"/"MuFITZ", name tools the key never mentions (Camelot,
Tabula) and contradict the key on facts it states plainly (`insert_textbox`
return value, region-move flattening, rotated-page coordinates). No verbatim or
near-verbatim overlap with the reference answers. Consistent with a genuine
closed-book run.
