---
name: pptx-tuning-deepseek-v4.1-flash
description: >
  Corrective pptx-editing guidance for deepseek-v4.1-flash: pick the table by
  header text with a unique-row assert, python-pptx has no add_row (deepcopy
  the a:tr), then recompute totals and sync the frame height and bottom,
  drop_rel ignores r:embed and an undropped image rel still ships the media.
when_to_use: >
  Load when the provider-stripped model id (see stack-detection.md) resolves
  to exactly `deepseek-v4.1-flash` AND the repository contains a PowerPoint deck (`*.pptx` or
  `*.ppt` at the root or up to two directories deep, per meta.yaml detection).
  For any other model or a repo without decks, do not load.
tuned_for: deepseek-v4.1-flash
tuned_version: "4.1-2026-10-21"
stack: pptx
source_scorecard: ../scores/deepseek-v4.1-flash.md
threshold: 0.9
revalidate_when: model_version changes
---
# PowerPoint-editing corrections for deepseek-v4.1-flash

<!-- kaizen:digest -->
**PowerPoint edits (python-pptx): find by content, sync the frame, drop rels by hand:**
1. Pick the table by its HEADER text (first-row cell texts), never `next(...)` / the first `has_table` shape; recurse into group shapes. Match the row by exact stripped cell text and assert exactly one match; zero or several = stop and ask.
2. python-pptx has NO `table.add_row()` (and no row delete): add a row with `copy.deepcopy(ref_tr)` + `ref_tr.addnext(new_tr)`, fill through each cell's existing first run.
3. After adding or removing an `a:tr`: recompute the Total, set `gf.height = sum(r.height for r in gf.table.rows)` (the frame does not follow XML edits), and check the new bottom against `prs.slide_height` and the shapes below.
4. `drop_rel(rId)` counts only `//@r:id`, so an image rel (`a:blip r:embed`) always counts 0 and is always dropped: check `//@r:embed` / `//@r:link` in the part yourself first. An image rel you do NOT drop keeps its media part in the saved zip.
<!-- /kaizen:digest -->

## table-locate: choose by content, prove uniqueness

- A slide (or its groups) can hold several tables; the first `has_table` shape is not
  "the table". Collect every `has_table` frame (recurse into `GroupShape.shapes`) and
  keep the one whose first-row texts match the expected header. Zero or several
  candidates = stop and ask.
- Match the row on `cell.text_frame.text.strip() == "Gadget D"` in the label column,
  collect all hits, and assert `len(hits) == 1` before editing. Never take the first hit
  from `next(...)`.

## table-relayout: adding a row is XML work plus follow-ups

- python-pptx 1.0.2 has no `table.add_row()` and no `add_column()`; `Table`,
  `table.rows` and `_Row` only read. Do not mention or offer `add_row()` as an option.
- Add: `new_tr = copy.deepcopy(ref_tr)`; `ref_tr.addnext(new_tr)` (or
  `total_tr.addprevious(new_tr)`); fill each cell via `runs[0].text` and remove extra
  runs.
- The insert is not finished until you:
  1. recompute the Total (and any subtotal) from the rows,
  2. set `graphic_frame.height = sum(r.height for r in table.rows)`,
  3. check `graphic_frame.top + graphic_frame.height` against `prs.slide_height` and
     the shapes below (move them or ask),
  4. re-alternate explicit `a:tcPr` banding fills if the table uses them.
- Slide sizes: read `prs.slide_width` / `prs.slide_height`. A 16:9 deck is typically
  12192000 × 6858000 EMU, not 13333333 wide. python-pptx's own default `Presentation()`
  template is 4:3 (9144000 × 6858000), so never assume either: read the deck.

## image-replace: relationships keep media alive

- `slide.part.drop_rel(rId)` drops when its reference count is under 2, and that count
  only looks at `//@r:id` attributes. Pictures use `a:blip/@r:embed`, so an image rel
  always counts 0 and is always dropped, even when another picture in the part still
  uses that rId (python-pptx reuses one rId per image part). Before dropping, query the
  part XML for `//@r:embed` and `//@r:link` equal to the rId; drop only if none remain.
- A rel you leave in place is not harmless: python-pptx saves every part reachable
  through a rel, so the old image stays in `ppt/media/` after its `p:pic` is gone. After
  repointing a blip or deleting a picture, drop the old rel once it is unused.

## Observed in live editing: keep the frame in sync with the rows

- `tbl.remove(tr)` / `addnext(copy)` leave `p:graphicFrame/p:xfrm/a:ext cy` at the
  old total. Always finish a row delete or insert with
  `gf.height = sum(r.height for r in gf.table.rows)`, then check the new bottom
  against `prs.slide_height` and the shapes below.
