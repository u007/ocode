---
name: pptx-tuning-cat-jam-free
description: >
  Corrective pptx-editing guidance for cat-jam-free: one a:tc per gridCol
  through merges, choose tables by header with a unique-row assert, placeholder
  table insertion, finishing row deletes/inserts (fills, bottom check, runs),
  dropping replaced image rels, full save-and-verify, and returning legacy .ppt.
when_to_use: >
  Load when the provider-stripped model id (see stack-detection.md) resolves
  to exactly `cat-jam-free` AND the repository contains a PowerPoint deck (`*.pptx` or
  `*.ppt` at the root or up to two directories deep, per meta.yaml detection).
  For any other model or a repo without decks, do not load.
tuned_for: cat-jam-free
tuned_version: "alpha"
stack: pptx
source_scorecard: ../scores/cat-jam-free.md
threshold: 0.9
revalidate_when: model_version changes
---
# PowerPoint-editing corrections for cat-jam-free

<!-- kaizen:digest -->
**PowerPoint edits (python-pptx): a diagnosis is not an edit — finish, verify, deliver:**
1. Every `a:tr` has exactly one `a:tc` per `a:gridCol`, merges included. The origin carries `gridSpan`/`rowSpan`; each covered cell is still present with `hMerge="1"`/`vMerge="1"` and is empty. Covered cells are never omitted; spans never reduce the count. Write text to the origin only; a row/column edit through a merge changes the span AND adds/removes the covered cells together.
2. Pick the table by its HEADER text, not "the table on the slide"; recurse into groups. Collect every row whose stripped key cell matches and assert exactly one hit; otherwise stop and ask.
3. Read `prs.slide_width`/`prs.slide_height`; never assume 16:9 numbers.
4. Only a `TablePlaceholder` has `insert_table(rows, cols)`. A generic content placeholder has none: `add_table()` at its left/top/width/height, then remove the placeholder's `p:sp`. Match an existing table by copying its `a:tblPr` (`a:tableStyleId` plus firstRow/bandRow flags); `add_table` defaults to Medium Style 2 – Accent 1.
5. Every row-delete answer walks the whole checklist, not just the diagnosis: (a) remove the `a:tr`; (b) if rows carry explicit `a:tcPr` fills, re-apply the alternating fills to every row after the deleted one (or clear them all and let the style's bandRow do it) — say this fix, never stop at explaining the cause; (c) shrink the graphic frame height and recompute totals; (d) confirm the deleted text is absent from every slide XML part.
5b. Changing a cell value is not done until every figure that depends on it is updated in the same number format: in-table totals/subtotals, summary or agenda slides that quote it, chart data (the embedded workbook and the chart XML cache), and speaker notes/text mentions.
6. Row insert: write text through each cell's existing first run, update totals and `gf.height`, then check `gf.top + gf.height <= prs.slide_height` and no overlap with shapes below; move them or ask.
7. After replacing or removing a picture, drop the OLD image rel when no other `//@r:embed | //@r:link` in that part uses it. `drop_rel` does ref-count, but only `r:id`, so it always drops image rels — check yourself first.
8. Verify: re-open the output, assert the exact changed cells/totals, assert every other slide/shape is unchanged versus the original, search all parts for removed text.
9. Legacy `.ppt`: `soffice --headless --convert-to pptx`, edit, and when the user gave `.ppt`, convert back and deliver `.ppt` (keep the original). Never just hand back a `.pptx`.
<!-- /kaizen:digest -->

## pptx-model: merges, slide size

- A table row always has one `a:tc` for every `a:gridCol`. In a 4-column grid
  with a Total label spanning two columns, the row is: origin `a:tc
  gridSpan="2"`, covered `a:tc hMerge="1"` (empty), then two normal cells. No
  producer omits the covered cell; a row whose `a:tc` count differs from the
  gridCol count is invalid and triggers PowerPoint's repair prompt.
- python-pptx exposes this as `cell.is_merge_origin` / `cell.is_spanned`;
  `table.cell(r, c)` is grid-indexed. Write text only to the origin.
- Deleting or inserting a row or column inside a merge is two edits that go
  together: adjust the origin's `gridSpan`/`rowSpan` AND add or remove the
  matching covered cell(s). Afterwards assert
  `len(tr.tc_lst) == len(tbl.tblGrid.gridCol_lst)` for every row.
- Slide size comes from `prs.slide_width` / `prs.slide_height`. Never position
  against assumed 16:9 or 4:3 dimensions.

## table-locate: choose by content, prove uniqueness

- Filter shapes with `has_table` (recurse into `GroupShape.shapes`) and keep the
  table whose first-row `cell.text_frame.text.strip()` values match the
  expected header. A slide can hold more than one table.
- Match the row on exact stripped key-cell text, collect every hit, and assert
  `len(hits) == 1` before editing. Zero or several hits: stop and ask.

## table-insert: placeholders and matching style

- `insert_table(rows, cols)` exists only on a `TablePlaceholder`; it returns a
  `PlaceholderGraphicFrame` in the placeholder's geometry. A generic content
  placeholder (`SlidePlaceholder`) has no `insert_table`: call
  `slide.shapes.add_table(rows, cols, ph.left, ph.top, ph.width, ph.height)`
  and then `ph._element.getparent().remove(ph._element)` so no empty "Click to
  add text" box remains.
- A table's style is `a:tblPr/a:tableStyleId` (a GUID) plus the `firstRow`,
  `bandRow` (and related) flags on `a:tblPr`. `add_table` sets Medium Style 2 –
  Accent 1 with firstRow and bandRow on. To match an existing table, copy its
  `a:tblPr` (style id and flags), any explicit `a:tcPr` fills and run
  properties, and its column widths and row heights.

## row-delete and cell-edit: finish the delete

- When rows are shaded with explicit `a:tcPr/a:solidFill`, removing a middle
  `a:tr` leaves two same-coloured neighbours. Fix it in the same edit:
  re-assign the alternating fills to every row after the deleted one, or clear
  the explicit fills and rely on the style's `bandRow` banding.
- A delete is not done until the old text is verified gone: re-open the
  output and search every `ppt/slides/slideN.xml` (and notes) for it.

## table-relayout: finish a row insert

- Write each new cell's text by setting `cell.text_frame.paragraphs[0].runs[0].text`
  of the deep-copied row and removing extra runs; `cell.text = …` drops the
  copied formatting.
- After updating the total and `gf.height = sum(r.height for r in gf.table.rows)`,
  compare `gf.top + gf.height` with `prs.slide_height` (minus the deck's margin)
  and with the top of every shape below the table. Overflow or overlap: shrink
  rows, move the shapes below, or ask.

## image-replace: the old image must leave the package

- `add_picture(path, left, top, width, height)` with both dimensions stretches
  an image of a different aspect; pass one dimension, or fit the new aspect
  inside the old box.
- After the swap (new `p:pic` or repointed `a:blip r:embed`) the slide part
  still holds the relationship to the old image, so `ppt/media/imageN.*` is
  still saved. Find other users of the old rId in that part
  (`slide._element.xpath("//@r:embed | //@r:link")`); when none remain,
  `slide.part.drop_rel(old_rId)`.
- `drop_rel` does reference-count, but `_rel_ref_count` matches only
  `//@r:id`. Pictures use `a:blip r:embed`, so an image rel always counts 0 and
  is always dropped, even when another picture still uses it. Your own
  `r:embed`/`r:link` check before the call is the only guard.

## verify-safety: prove the change and the non-change

- Save to a new file; re-open it and assert the exact changed cell texts, their
  order, the totals and the frame geometry inside the slide.
- Assert the rest is unchanged: compare every other slide's shapes (XML or
  text) with the original deck.
- Search every slide part for text that should be gone, and list `ppt/media`
  in the saved zip (hash-compare) when an image was removed.

## legacy-ppt: deliver the format the user gave you

- `.ppt` is the binary OLE2 format; python-pptx opens only OOXML. Convert with
  `soffice --headless --convert-to pptx --outdir out deck.ppt`, check the
  converted tables, images and fonts, then edit.
- If the user supplied `.ppt` and needs `.ppt`, convert the edited deck back
  (`--convert-to ppt`) and deliver that, keeping the untouched original. Tell
  the user the file went through a conversion that may not be lossless.
