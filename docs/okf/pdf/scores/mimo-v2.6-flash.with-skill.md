---
model_id: mimo-v2.6-flash
model_version: "2.6"
evaluated_via: opencode-go
evaluated_on: 2026-09-28
stack: pdf
stack_corpus_rev: 1
threshold: 0.9
validation: with-skill
---

# Scorecard: mimo-v2.6-flash on pdf (WITH derived skill)

> Valid ONLY for `mimo-v2.6-flash` @ `2.6`. A version bump invalidates
> this scorecard, so re-benchmark.

Validation run. Answers: `pdf/answers/mimo-v2.6-flash.with-skill.md`, produced
closed-book with the current `derived/pdf.mimo-v2.6-flash.SKILL.md` prepended
(audited: zero tool calls). pdf-model-04 was skipped once and re-asked alone
with the same skill. Graded against `pdf/questions.yaml` at corpus_rev 1 with
the same strictness per point as the baseline `scores/mimo-v2.6-flash.md`:
points are whole, fractions come only from a listed partial, and a point
stated with a wrong API fact, or satisfied only by a cover-up, earns 0. No new
derived skill is produced from this run.

PyMuPDF facts checked on 1.27.1 in this grading session:

- `page.rect` is the CropBox moved to a (0,0) top-left, and `get_text("words")`
  reports coordinates in that shifted frame (`cropbox_position` gives the
  offset). This confirms the model-04 CropBox description.
- `page.show_pdf_page(..., same_doc, ...)` raises "source document must not
  equal target", as rowdel-03 and tblins-03 claim.
- Text inside a Form XObject stamped with `show_pdf_page` is removed by
  `add_redact_annot` + `apply_redactions`. So rowdel-01's step 5 (edit the
  Total after restamping) works.
- `insert_text(color=0xFF0000)` raises ValueError: color must be a 1/3/4
  float sequence (fonts-03 claims an int works; not load-bearing).
- `Page.get_images(xref=True)` raises TypeError: no such keyword (imgrep-03;
  not load-bearing).

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| pdf-model-01 | pdf-model | 3 | 2 | 2 | 1.00 | |
| pdf-model-02 | pdf-model, verify-safety | 3 | 2 | 2 | 1.00 | Odd aside ("screenshot-plus-layer attack"). Not load-bearing. |
| pdf-model-03 | pdf-model | 2 | 2 | 2 | 1.00 | Still overstates the /Length issue, as in the baseline. |
| pdf-model-04 | pdf-model | 2 | 2 | 2 | 1.00 | *Re-asked alone.* Fixed: extract and insert stay in the UNROTATED frame, only page.rect is rotated, and it names rotation_matrix/derotation_matrix. The CropBox frame and cropbox_position are correct (verified). |
| pdf-locate-01 | table-locate | 3 | 2 | 2 | 1.00 | |
| pdf-locate-02 | table-locate | 2 | 2 | 2 | 1.00 | |
| pdf-locate-03 | table-locate, cell-edit | 2 | 2 | 2 | 1.00 | The colour-int conversion is vague ("sRGB to/from int"). The base-14 embedding myth is gone. |
| pdf-locate-04 | table-locate, table-relayout | 2 | 2 | 2 | 1.00 | |
| pdf-rowdel-01 | row-delete | 3 | 3 | 3 | 1.00 | Redacts the row and the moving region with default graphics, restamps from an untouched copy one row higher, and edits the Total with graphics/images NONE. The invented constants are gone. |
| pdf-rowdel-02 | row-delete, cell-edit | 3 | 2 | 2 | 1.00 | Defaults are correct: images=2 PIXELS, graphics=1 REMOVE_IF_COVERED (entirely inside only), text=0, plus TOUCHED=2. Gives graphics=0 to keep the grid. Minor: images=0 is not named for keeping images, and a later line calls the pixel effect "blurring" (it blanks them). |
| pdf-rowdel-03 | row-delete | 2 | 2 | 2 | 1.00 | Fixed: redacts the region that moves (not only the deleted row) before show_pdf_page(clip=) from a separate copy, else the block is duplicated. |
| pdf-rowdel-04 | row-delete, verify-safety | 2 | 2 | 2 | 1.00 | |
| pdf-cell-01 | cell-edit | 3 | 3 | 3 | 1.00 | Fixed: no fill, apply with IMAGE_NONE/LINE_ART_NONE, tight rect inside the cell, right-aligned at the span origin. |
| pdf-cell-02 | cell-edit | 2 | 2 | 2 | 1.00 | Fixed: any overlap removes the whole character. |
| pdf-cell-03 | cell-edit, table-relayout | 2 | 2 | 2 | 1.00 | |
| pdf-cell-04 | cell-edit | 2 | 2 | 2 | 1.00 | |
| pdf-relayout-01 | table-relayout | 3 | 3 | 3 | 1.00 | |
| pdf-relayout-02 | table-relayout | 3 | 3 | 2 | 0.67 | Points 1 and 3 are earned. Point 2 is lost: it never measures whether the widest content fits the narrower columns, and gives no font-size or wrap fallback. The baseline had this point. This is the same miss docked in the glm and deepseek with-skill cards. |
| pdf-relayout-03 | table-relayout, pdf-model | 2 | 2 | 2 | 1.00 | Fixed: "a surgical in-place edit ... or regenerating from the true source, is preferable". Its acceptance condition also requires the source to be unavailable. |
| pdf-relayout-04 | table-relayout, table-insert | 2 | 2 | 2 | 1.00 | |
| pdf-tblins-01 | table-insert | 3 | 2 | 2 | 1.00 | Fixed: states that insert_table does not exist. The stamp route is pymupdf.Story/reportlab → show_pdf_page. |
| pdf-tblins-02 | table-insert | 2 | 2 | 2 | 1.00 | Fixed: fills, then text, then rules. |
| pdf-tblins-03 | table-insert, table-relayout | 2 | 2 | 2 | 1.00 | Fixed: redact + restamp from a copy, spill to a new page, regenerate from source. Minor: no "place it in free space and tell the user". Its "shrink surrounding fonts" option is conditional, not the main answer. |
| pdf-imgrep-01 | image-replace | 3 | 3 | 3 | 1.00 | Fixed: replace_image keeps the old box, so a different aspect is stretched. |
| pdf-imgrep-02 | image-replace, verify-safety | 2 | 2 | 2 | 1.00 | No "verify exactly one image at that rect" (minor; same leniency as baseline). |
| pdf-imgrep-03 | image-replace | 2 | 2 | 2 | 1.00 | Invented `get_images(full=True, xref=True)` (TypeError, verified). Not load-bearing: full=True and get_image_info(xrefs=True) are correct. |
| pdf-imgins-01 | image-insert | 3 | 2 | 2 | 1.00 | Still anchors to page.rect minus a fixed margin, then checks text and drawings for collisions (same as baseline). |
| pdf-imgins-02 | image-insert | 2 | 2 | 2 | 1.00 | Fixed: re-save with garbage=4 to merge identical streams. |
| pdf-imgins-03 | image-insert, pdf-model | 2 | 2 | 2 | 1.00 | |
| pdf-fonts-01 | fonts | 3 | 2 | 2 | 1.00 | Fixed: says the extract_font buffer is still the subset. Recommends the original full TTF first, then base-14/Noto, with has_glyph and re-extraction. Minor: "subsetting is done ... by the producer, not by the library" ignores `Document.subset_fonts()`. |
| pdf-fonts-02 | fonts | 2 | 2 | 2 | 1.00 | helv/hebo are correct, but it rambles ("heit? no"). |
| pdf-fonts-03 | fonts, cell-edit | 2 | 2 | 2 | 1.00 | The bbox[3] baseline error is fixed (uses origin). Invented: `color=span["color"] as an int` is rejected by insert_text (verified). Not load-bearing for the weight/size points. |
| pdf-verify-01 | verify-safety | 3 | 3 | 2 | 0.67 | Same miss as baseline. It says the render catches "white boxes over shading" but never that a render alone can't tell an overlay from a real edit. The negative-search check is present (point 1). |
| pdf-verify-02 | verify-safety | 3 | 2 | 2 | 1.00 | |
| pdf-verify-03 | verify-safety | 2 | 2 | 2 | 1.00 | |
| pdf-verify-04 | verify-safety, pdf-model | 2 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | baseline |
|-----|---------:|--:|-------|---------:|
| pdf-model | 1.00 | 7 | ok | 0.88 |
| table-locate | 1.00 | 4 | ok | 1.00 |
| row-delete | 1.00 | 4 | ok | 0.75 |
| cell-edit | 1.00 | 7 | ok | 0.84 |
| table-insert | 1.00 | 4 | ok | 0.78 |
| image-replace | 1.00 | 3 | low-n | 0.86 |
| image-insert | 1.00 | 3 | low-n | 0.86 |
| fonts | 1.00 | 3 | low-n | 0.79 |
| verify-safety | 0.94 | 7 | ok | 0.94 |
| table-relayout | 0.94 | 7 | ok | 0.88 |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

- table-relayout: (2+3+2+2+2+2+2) / (2+3+3+2+2+2+2) = 15 / 16 = 0.94
  (relayout-02 contributes 0.67×3 = 2).
- verify-safety: (3+2+2+2+3+2+2) / (3+2+2+3+3+2+2) = 16 / 17 = 0.94
  (verify-01 contributes 0.67×3 = 2).

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 84.0 / 86 = 97.7%
```

The only losses are relayout-02 (−1) and verify-01 (−1). Baseline: 74.0 / 86 = 86.0%.

## Target tags: baseline → with-skill → pass(≥0.9)?

| tag | baseline | with-skill | pass(≥0.9)? |
|-----|---------:|-----------:|:-----------:|
| pdf-model | 0.88 | 1.00 | pass |
| table-relayout | 0.88 | 0.94 | pass |
| image-replace (low-n) | 0.86 | 1.00 | pass |
| image-insert (low-n) | 0.86 | 1.00 | pass |
| cell-edit | 0.84 | 1.00 | pass |
| fonts (low-n) | 0.79 | 1.00 | pass |
| table-insert | 0.78 | 1.00 | pass |
| row-delete | 0.75 | 1.00 | pass |

Every target tag reaches 0.9. The validation passes.

## Remaining wrong or missing claims

No target tag is below 0.9. Two residual misses remain, for the record only:

- table-relayout (0.94, passing): relayout-02 never measures whether the
  widest cell text fits the narrower columns after redistributing the widths,
  and offers no deliberate fallback (slightly smaller font or wrap + taller
  row). The baseline had this. It is a single-sample regression, not a skill
  failure.
- verify-safety (non-target, 0.94): verify-01 still never says that a render
  alone cannot distinguish a white-box overlay from a real edit.

## History

- No earlier with-skill scorecard existed for mimo-v2.6-flash on pdf (no
  0.75-era validation to overwrite). This is the first with-skill run.
