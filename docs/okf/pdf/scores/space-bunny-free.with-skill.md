---
model_id: space-bunny-free
model_version: "alpha"
evaluated_via: opencode-go
evaluated_on: 2026-09-28
stack: pdf
stack_corpus_rev: 1
threshold: 0.9
validation: with-skill
---

<!-- Filename: model_id with "/" flattened to "__" so it is one valid path
     segment. `space-bunny-free` has no slash, so the filename is unchanged. -->

# Scorecard — space-bunny-free on pdf (WITH derived skill — validation @ 0.9)

> Valid ONLY for `space-bunny-free` @ `alpha`. A version bump invalidates this
> scorecard — re-benchmark.

Answers: `pdf/answers/space-bunny-free.with-skill.md`. They were produced
closed-book with the current `derived/pdf.space-bunny-free.SKILL.md`
prepended (audited: zero tool calls). Graded against `pdf/questions.yaml` @
corpus_rev 1 at the same per-point strictness as the baseline
`scores/space-bunny-free.md`: a wrong API fact in the claim a point asks for
earns 0, and a wrong aside about a scenario the question does not pose is
noted but not deducted (baseline precedent: imgrep-01, imgins-01). Facts that
decided or were noted were checked on PyMuPDF 1.27.1:
- a redaction rect that covers only part of a glyph removes the WHOLE glyph,
  with no fragment left;
- redacting a shared image on one page (default `images=2`) leaves the other
  page's placement intact;
- on a `/Rotate` page, `insert_image(rect)` takes unrotated coordinates, so a
  rect built from the displayed `page.rect` lands off-page;
- `get_text_length` is a module function; `Page` has no such method;
- a span's `origin` is a tuple, so `.y` fails.

This is a validation scorecard, so no new derived skill is written.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| pdf-model-01 | pdf-model | 3 | 2 | 2 | 1.00 | also names Flate + "regenerate from source template" |
| pdf-model-02 | pdf-model, verify-safety | 3 | 2 | 2 | 1.00 | |
| pdf-model-03 | pdf-model | 2 | 2 | 2 | 1.00 | Flate, hex/CMap, split runs; "a length change desynchronises the stream" overstated |
| pdf-model-04 | pdf-model | 2 | 2 | 2 | 1.00 | **fixed**: `/Rotate` bboxes stay in unrotated coords, `rect * page.rotation_matrix` for displayed, `~page.transformation_matrix` to raw user space incl. CropBox |
| pdf-locate-01 | table-locate | 3 | 2 | 2 | 1.00 | |
| pdf-locate-02 | table-locate | 2 | 2 | 2 | 1.00 | |
| pdf-locate-03 | table-locate, cell-edit | 2 | 2 | 2 | 1.00 | flag list now correct (1/2/4/8/16); colour int conversion shown |
| pdf-locate-04 | table-locate, table-relayout | 2 | 2 | 2 | 1.00 | right edge = cell.x1 − pad measured from a neighbour; `page.get_text_length` (it is `pymupdf.get_text_length`) |
| pdf-rowdel-01 | row-delete | 3 | 3 | 3 | 1.00 | **point 3 now earned**: the moving region includes the Total line and the notes/footer below the table; Total recomputed from the extracted column; overflow to a new page with `doc[pno]` re-fetched. `show_pdf_page(clip=…)` from an untouched copy. Residual: says default graphics removes the rules in the band, but full-height verticals are only partly covered, so they stay whole. Step 8 redraws the bottom rule but does not trim the vertical stubs below the new bottom |
| pdf-rowdel-02 | row-delete, cell-edit | 3 | 2 | 2 | 1.00 | **fixed**: graphics=1 = entirely inside, partly covered kept whole, 2 = touched; images=2 blanks pixels; no `add_text`; graphics=0/images=0 for cell edits. Stray wrong line: "text=0 … the only default that is not 'do nothing'" (contradicted by its own next two bullets) |
| pdf-rowdel-03 | row-delete | 2 | 2 | 2 | 1.00 | same-size target/clip for pure translation; redact first |
| pdf-rowdel-04 | row-delete, verify-safety | 2 | 2 | 2 | 1.00 | |
| pdf-cell-01 | cell-edit | 3 | 3 | 3 | 1.00 | baseline `origin`, `graphics/images=NONE`, has_glyph check. Pads the search rect 1–2 pt (not inset); `page.get_text_length`; `span["origin"].y` on a tuple |
| pdf-cell-02 | cell-edit | 2 | 2 | 1 | 0.50 | point 2 ok (keep far edges inside the cell, verify neighbours). Point 1 lost for **wrong behaviour** in the headline, same as the 0.75-era run: "Too small and you clip a glyph … a stray comma, a '0' … left behind. So pad the rect by a point or two on every side". Verified: any overlap removes the whole glyph, so a small rect leaves no fragment, and padding is what deletes the neighbours |
| pdf-cell-03 | cell-edit, table-relayout | 2 | 2 | 2 | 1.00 | negative return, nothing written, no exception; shrink / widen / taller row + restamp |
| pdf-cell-04 | cell-edit | 2 | 2 | 2 | 1.00 | baseline + "about one line too high" + span origin. Garbled mechanism: "the glyphs hang below the point" (they sit above the baseline) |
| pdf-relayout-01 | table-relayout | 3 | 3 | 3 | 1.00 | Total moved + updated, verticals/horizontals redrawn, parity striping, content below restamped, new-page spill. "keep the outer top/bottom borders where they are" is wrong for the bottom border |
| pdf-relayout-02 | table-relayout | 3 | 3 | 3 | 1.00 | **point 2 now earned**: measure every cell at its NEW width, shrink/wrap deliberately. Same total width; mistake = wider than margins; redact with default graphics, redraw all |
| pdf-relayout-03 | table-relayout, pdf-model | 2 | 2 | 2 | 1.00 | **fixed**: ask whether the source exists and regenerate; else surgical edit; round-trip last resort |
| pdf-relayout-04 | table-relayout, table-insert | 2 | 2 | 2 | 1.00 | text + drawings + images, margins, vertical budget |
| pdf-tblins-01 | table-insert | 3 | 2 | 2 | 1.00 | direct drawing + `insert_htmlbox` table (writes page content, no annotation) |
| pdf-tblins-02 | table-insert | 2 | 2 | 2 | 1.00 | |
| pdf-tblins-03 | table-insert, table-relayout | 2 | 2 | 2 | 1.00 | **fixed**: redact below, restamp lower, spill past the bottom onto `new_page(pno+1)`; free space + tell user; regenerate from source; never overlap |
| pdf-imgrep-01 | image-replace | 3 | 3 | 3 | 1.00 | xref + rect located; redact (page-local, verified) + `insert_image(keep_proportion=True)`; xref swap changes every page. `replace_image` never named ("replace the xref") |
| pdf-imgrep-02 | image-replace, verify-safety | 2 | 2 | 2 | 1.00 | |
| pdf-imgrep-03 | image-replace | 2 | 2 | 2 | 1.00 | says vector logos show up "in `get_text`" (dubious) |
| pdf-imgins-01 | image-insert | 3 | 2 | 2 | 1.00 | aspect height, occupied-rect intersection check, `insert_image(rect, filename=…)`. Anchors to `page.rect` − 36 pt, not the content edges (baseline tolerated this). **Wrong aside** (not deducted, since rotation is not posed): "On a `/Rotate` page, `page.rect` is the displayed size, which is what you want for placement". Verified: insert rects are unrotated coordinates |
| pdf-imgins-02 | image-insert | 2 | 2 | 2 | 1.00 | `xref=` reuse from the return value; `garbage=4` merges, lower levels keep copies |
| pdf-imgins-03 | image-insert, pdf-model | 2 | 2 | 2 | 1.00 | `overlay=False` = beneath, "not a no-op"; off-page/zero/CropBox rect; `get_image_info` |
| pdf-fonts-01 | fonts | 3 | 2 | 2 | 1.00 | `Font(fontfile=…).has_glyph` on the Font (fixed from 0.75-era xref misuse) |
| pdf-fonts-02 | fonts | 2 | 2 | 2 | 1.00 | "sans-serif/serif/monospace aliases" dubious |
| pdf-fonts-03 | fonts, cell-edit | 2 | 2 | 2 | 1.00 | bold `& 16`, italic `& 2`, exact `span["size"]`, `hebo` |
| pdf-verify-01 | verify-safety | 3 | 3 | 2 | 0.67 | re-extract + structure + render ok. Point 3 missing: never says a render cannot tell a white-box overlay from a real edit (it says "only a rendered look catches" glitches). DOCX leakage: `w:tblGrid`, pandoc, python-docx, `unzip -t` |
| pdf-verify-02 | verify-safety | 3 | 2 | 2 | 1.00 | |
| pdf-verify-03 | verify-safety | 2 | 2 | 2 | 1.00 | new file, verify then replace; metadata + page count. No signature/encryption; no explicit check of other pages' text |
| pdf-verify-04 | verify-safety, pdf-model | 2 | 2 | 2 | 1.00 | DOCX leakage (`<w:t>` runs, zip integrity) |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| pdf-model | 1.00 | 7 | ok | TARGET — passes |
| table-locate | 1.00 | 4 | ok | — |
| row-delete | 1.00 | 4 | ok | TARGET — passes |
| cell-edit | 0.94 | 7 | ok | TARGET — passes |
| table-relayout | 1.00 | 7 | ok | TARGET — passes |
| table-insert | 1.00 | 4 | ok | TARGET — passes |
| image-replace | 1.00 | 3 | low-n | — |
| image-insert | 1.00 | 3 | low-n | TARGET — passes |
| fonts | 1.00 | 3 | low-n | TARGET — passes |
| verify-safety | 0.94 | 7 | ok | — (non-target, above 0.9) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.
cell-edit = 15/16; verify-safety = 16/17.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight)
            = (86 − 0.5×2 [cell-02] − (1/3)×3 [verify-01]) / 86
            = 84 / 86 = 97.7%
```

(baseline: 73.5 / 86 = 85.5%)

## Target tags: baseline → with-skill → pass(≥0.9)?

| tag | baseline | with-skill | pass (≥0.9)? |
|-----|---------:|-----------:|:------------:|
| pdf-model | 0.81 | 1.00 | PASS |
| row-delete | 0.65 | 1.00 | PASS |
| cell-edit | 0.72 | 0.94 | PASS |
| table-relayout | 0.75 | 1.00 | PASS |
| table-insert | 0.89 | 1.00 | PASS |
| image-insert (low-n) | 0.71 | 1.00 | PASS |
| fonts (low-n) | 0.86 | 1.00 | PASS |

**All seven target tags pass at 0.9.**

## Targets still below 0.9

None. The remaining errors on target tags do not block the verdict:
- **cell-02 (cell-edit, lost point 1).** "Too small and you clip a glyph …
  you end up with a fragment … So pad the rect by a point or two on every
  side". Any overlap removes the whole glyph, so a tight rect is safe and
  padding is the risk. This is the only lost point on a target tag, and it
  also appeared in the 0.75-era run. The skill never states the
  whole-glyph consequence.
- **imgins-01 (aside, not deducted).** It says that on a `/Rotate` page the
  displayed `page.rect` is "what you want for placement". Insert rects are
  unrotated coordinates. It also anchors to `page.rect` − margin instead of
  the content edges.
- **rowdel-01 / relayout-01.** Both claim that the default graphics removal
  takes the vertical rules in the band. Partly covered full-height verticals
  stay whole, so stubs remain below a shrunk table.
- **rowdel-02.** "text=0 … the only default that is not 'do nothing'".
- **cell-04.** "the glyphs hang below the point".
- **Several answers.** `page.get_text_length` (a module function) and
  `span["origin"].y` (a tuple).

## History

- 0.75-era with-skill run (threshold 0.75, earlier skill): the targets
  row-delete 0.65→0.75, cell-edit 0.72→0.84 and image-insert 0.71→1.00 all
  passed at 0.75, and the stack scored 77.5/86 = 90.1%.

## Contamination check

The answers echo the skill's phrasing almost verbatim, for example "not a
no-op", "even inside a padded block bbox", "re-fetch `doc[pno]`" and "writes
ordinary page content and creates no annotation". This is intended
absorption of the injected guidance, not copying of the key. They still
differ from the key:
- They miss key points the skill does not state: the overlay limitation of
  a render in verify-01, and `replace_image`.
- They keep errors the skill does not cover: glyph fragments in cell-02, and
  the vertical-rule stubs.
- They leak DOCX-stack material into PDF answers (`w:tblGrid`, `<w:t>`
  runs, pandoc, python-docx, `unzip -t` in verify-01 and verify-04).

This is consistent with genuine closed-book answers that absorbed the skill.
