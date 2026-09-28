- id: pptx-model-01
  answer: "A table lives inside a slide part as a `<p:graphicFrame>` element on the slide's shape tree\n\
    (`<p:cSld><p:spTree>`). The graphicFrame contains an `<a:graphic>` whose\n`<a:graphicData>` has uri\
    \ = \"http://schemas.openxmlformats.org/drawingml/2006/table\",\nand inside that a `<a:tbl>` element\
    \ holds the whole table.\n- Columns: `<a:tblGrid>` containing one `<a:gridCol w=\"...\"/>` per column;\
    \ the w\n  attribute (in EMUs) is each column's width.\n- Rows: direct children `<a:tr h=\"...\">`\
    \ of `<a:tbl>`, in document order; h is the\n  row height in EMUs (a hint — PowerPoint may grow it\
    \ to fit text).\n- Cells: `<a:tc>` children of each `<a:tr>`, one per grid column position (including\n\
    \  cells consumed by a merge, which are kept as empty `<a:tc>` elements with a\n  gridSpan/vmerge\
    \ marker rather than being removed).\n- Text: each `<a:tc>` contains `<a:txBody>` (`<a:bodyPr/>`,\
    \ `<a:lstStyle/>`, then\n  `<a:p>` paragraphs), and each paragraph contains runs `<a:r>` with `<a:rPr>`\
    \ and\n  `<a:t>`. Cell-level formatting (fill, borders, margins) lives in `<a:tcPr>`;\n  row-level\
    \ banding flags are on `<a:tr>` (firstRow/bandRow etc. are on `<a:tblPr>`).\npython-pptx exposes this\
    \ as `shape.table` (GraphicFrame.table) with `table.columns`,\n`table.rows`, `row.cells`, and `cell.text_frame`.\n"
- id: pptx-model-02
  answer: 'python-pptx and DrawingML use English Metric Units (EMU): 1 inch = 914400 EMU,

    1 point = 12700 EMU, 1 cm = 360000 EMU. python-pptx wraps these in `Emu`/`Length`

    objects and offers convenience classes `Inches`, `Cm`, `Pt`, `Emu`. Because EMU is an

    integer unit and 914400 is divisible by many common factors, typical inch/point sizes

    convert exactly.

    Default 16:9 slide size in a default PowerPoint deck (and what python-pptx''s

    16:9 template produces) is 13.333 in × 7.5 in, i.e. 12192000 × 6858000 EMU

    (equivalently 33.867 cm × 19.05 cm). Older 4:3 decks are 10 in × 7.5 in

    (9144000 × 6858000 EMU).

    '
- id: pptx-model-03
  answer: "It is on the slide **layout** (and ultimately the slide **master**), not the slide\nitself.\
    \ `slide.shapes` only reflects the slide part's own `spTree`; placeholder-inherited\nor master-drawn\
    \ content lives in the layout/master parts (`slide_layout.shapes`,\n`slide_master.shapes`), so iterating\
    \ the slide's shapes misses it.\nConsequences of editing it there:\n- It is shared: the logo on the\
    \ master/layout feeds every slide that inherits it, so\n  one edit changes all of them at once — which\
    \ is exactly what you want for a global\n  logo, and a disaster if you only wanted one slide changed.\n\
    - You cannot fix or delete just one slide's copy by editing the slide; a slide that\n  needs a different\
    \ logo needs its own picture and, to override, typically a blank-ish\n  layout or explicit handling.\n\
    - Editing the master/layout can affect other slides, other layouts, and even other\n  files based\
    \ on the same template; masters are referenced by many slides.\n- If the slide itself contains a copy\
    \ (e.g., duplicated), you'd have to edit both to\n  keep them consistent.\n- python-pptx can edit\
    \ master/layout shapes (`slide.slide_master`, `slide_layout`),\n  but caution: a change there propagates\
    \ to every dependent slide and there is no\n  per-slide undo.\n"
- id: pptx-locate-01
  answer: "Find the table on slide 3:\n    from pptx import Presentation\n    prs = Presentation(path)\n\
    \    slide = prs.slides[2]          # slides are 0-indexed; slide 3 = index 2\n    table = next(sh.table\
    \ for sh in slide.shapes if sh.has_table)\n(`sh.has_table` is True only for graphicFrames wrapping\
    \ a table; alternatively check\n`sh.shape_type == MSO_SHAPE_TYPE.TABLE`.)\nFind the \"Gadget D\" row:\n\
    \    target = None\n    for row in table.rows:\n        # first cell's text, or check any cell\n \
    \       if row.cells[0].text.strip() == \"Gadget D\":\n            target = row\n            break\n\
    \    idx = [r for r in table.rows].index(target)   # if you need the index\nCaveat: `table.rows` has\
    \ no built-in search, so you scan and compare\n`cell.text`; if the label may span merged cells, check\
    \ the cell whose text\nmatches (the merged origin cell holds the text). You can then edit via\n`target.cells[i].text_frame`.\n"
- id: pptx-locate-02
  answer: "A horizontal merge is represented with `gridSpan` on the merged (origin) `<a:tc>`:\nthe first\
    \ cell gets `<a:tc ... gridSpan=\"2\">` and the second column's `<a:tc>` for\nthat row is retained\
    \ as a placeholder cell marked `hMerge=\"1\"` (it is not deleted).\nFor vertical merges, the origin\
    \ has `rowSpan=\"n\"` and the cells below carry\n`vMerge=\"1\"`; a continued vertical merge uses `<a:vMerge/>`\
    \ with restart implied by\nthe origin. python-pptx exposes `cell.merge(other_cell)` / `cell.split()`\
    \ which\nmanipulate these attributes.\nWhat to watch when editing/deleting around it:\n- The placeholder\
    \ `<a:tc hMerge=\"1\">` still exists and still counts toward the grid;\n  you must keep the cell count\
    \ equal to the number of `gridCol`s. Deleting an\n  `a:tc` (or removing a row without care) desynchronizes\
    \ the grid and PowerPoint\n  repairs/rejects the file.\n- Text, formatting, and fills belong to the\
    \ origin cell; writing to a merged-away\n  placeholder is meaningless/ignored.\n- Setting text on\
    \ the merged cell affects the whole spanned region — a \"Total\" label\n  spanning two columns is\
    \ one logical cell.\n- Deleting a column or row that the merge spans requires splitting first (or\n\
    \  removing the merge attributes and all placeholder tcs together), otherwise you\n  leave stale gridSpan/vMerge\
    \ values and an inconsistent grid.\n- Row/column count must match `tblGrid`/`tr` structure exactly\
    \ or the XML is invalid.\n"
- id: pptx-rowdel-01
  answer: "No — python-pptx has no `table.rows.remove()` and no public delete-row API.\n`Table.rows` is\
    \ a `_Row` sequence wrapper that only supports indexing, iteration,\nlen, and `add_row()`/`add_column()`\
    \ (append at the end only). To delete a row you go\nto the underlying XML and remove the `<a:tr>`\
    \ element:\n    from pptx.oxml.ns import qn\n    tbl = graphic_frame.table._tbl          # the a:tbl\
    \ element\n    trs = tbl.findall(qn('a:tr'))\n    tbl.remove(trs[row_index])              # remove\
    \ the a:tr for that row\nor, with lxml directly: `tr.getparent().remove(tr)`.\nAlternatively you can\
    \ get the `_Row`'s element: `row._tr.getparent().remove(row._tr)`.\nThen fix up the graphicFrame height\
    \ (p:xfrm ext cy) yourself, since nothing updates\nit automatically. Similar low-level removal applies\
    \ to columns (`a:gridCol` plus\neach row's corresponding `a:tc`s).\n"
- id: pptx-rowdel-02
  answer: "It doesn't break the file — the frame's `cy` is the *frame's* bounding box, while\neach `a:tr`\
    \ carries its own `h`. PowerPoint lays the table out from the sum of the\nrow heights; it will typically\
    \ render/shrink the table to fit its rows, or the frame\nmay end up taller than the content, leaving\
    \ dead space (and misaligned overlaps with\nanything positioned relative to the old table bottom,\
    \ e.g. a \"Total\" callout or\nfootnote placed just under the table).\nWhat matters:\n- If the frame\
    \ stays larger than the table, you get a gap; if other objects were\n  positioned using the old height,\
    \ spacing looks wrong.\n- The frame height should ideally equal (or be at least) the sum of row heights\
    \ plus\n  any table margins; PowerPoint tolerates mismatch but layout/overlap checks and\n  round-tripping\
    \ look cleaner if you fix it.\nFix it by recomputing cy as the sum of the remaining `a:tr/@h` values\
    \ and writing it\nback:\n    total = sum(int(tr.get('h')) for tr in tbl.findall(qn('a:tr')))\n   \
    \ gf = shape._element                       # p:graphicFrame\n    xfrm = gf.find(qn('p:xfrm'))   \
    \           # or gf.xfrm\n    ext = xfrm.find(qn('a:ext'))\n    ext.set('cy', str(total))\n(python-pptx:\
    \ `shape.height = Emu(total)` works because GraphicFrame exposes\nwidth/height via p:xfrm.) Note `h`\
    \ is a minimum hint; PowerPoint may have grown\nrows for wrapped text, so optionally re-read actual\
    \ heights or let PowerPoint\nrecompute on next edit. Also update any dependent layout (footnotes below)\
    \ and\nconsider re-banding the rows.\n"
- id: pptx-rowdel-03
  answer: "Because the banding is *alternating by position*, and the shading is either\n(a) applied as\
    \ explicit per-row cell fills, or (b) generated by PowerPoint's\nbanding rules (`firstRow`/`bandRow`\
    \ flags on `<a:tblPr>`) that count row index\nparity.\n- If the fills were baked in as explicit `a:solidFill`\
    \ on each row's cells (or as\n  `a:tr`-level styling), deleting a middle row leaves whatever fills\
    \ the surviving\n  rows already had: row A (even/dark), row B (odd/light) — after deleting one, the\n\
    \  two neighbours that were previously separated by the deleted row now sit adjacent\n  and can end\
    \ up the same colour, or the pattern visibly flips from the deletion\n  point downward (all rows below\
    \ shift parity, so the \"zebra\" looks wrong after the\n  deleted position even if the two immediate\
    \ neighbours differ).\n- If banding is driven by `bandRow=\"1\"` in `<a:tblPr>`, PowerPoint recomputes\n\
    \  alternation from row index, so the colours self-correct — which is why one deck\n  (flag-driven,\
    \ or theme/`tblStyle` based) looks fine while another (hard-coded\n  per-cell fills, or styles pasted\
    \ as values, or the file written by a tool that\n  baked fills in) does not.\n- Also relevant: if\
    \ the deck uses a table style (`a:tableStyleId`) with banding, or\n  if the deleted row was the header-adjacent\
    \ row (firstRow band), parity changes.\nSo: it happens in decks where shading is stored explicitly\
    \ per row/cell rather than\nderived from a banding flag/style; the fix is to clear the stale fills\
    \ and either let\n`bandRow` regenerate them or re-apply fills in alternating order.\n"
- id: pptx-rowdel-04
  answer: "Because it hides the row instead of removing it — the data is still in the file:\n- A white\
    \ rectangle overlay leaves the original row text underneath; anyone who\n  clicks, selects, copies,\
    \ searches, or edits the shape sees the hidden content, and\n  moving/deleting the rectangle (or printing\
    \ with different settings, or a\n  non-visual renderer, or accessibility/screen-reader extraction,\
    \ or a PDF where\n  z-order changes) exposes it.\n- White text is still text: it's extractable by\
    \ copy/paste, find, outline view,\n  screen readers, and data-mining; on a different background or\
    \ printed in B/W it\n  becomes visible.\n- Zero/height-near-zero rows still exist in the XML (`a:tr`\
    \ with h≈0) and can\n  reappear or affect layout; PowerPoint may still reserve/repair space, and the\
    \ row\n  still participates in merges, counts, and calculations.\n- Semantically the row's data remains\
    \ part of the table, so totals, charts, or any\n  downstream consumer still sees it; it also breaks\
    \ the banding/alternation and\n  makes the file misleading to anyone auditing the source.\nThe correct\
    \ approach is to actually delete the row (remove the `a:tr`, adjust the\ngraphicFrame height, and\
    \ fix banding) so the content is gone from the document.\n"
- id: pptx-cell-01
  answer: "`cell.text = \"SKU\"` replaces the cell's entire text content: python-pptx clears the\ntext\
    \ frame's existing paragraphs/runs and writes a single run with the given text.\nThe old run's run-level\
    \ formatting (bold, 14 pt, white colour) is discarded along\nwith the run it was stored on, so the\
    \ text reverts to the default/inherited\nformatting (often black, default size, not bold). Paragraph-level\
    \ properties may\nsurvive depending on implementation, but run-level rPr does not — the new run is\n\
    created with no explicit rPr.\nTo keep it, either:\n- Edit the existing run instead of replacing:\n\
    \    run = cell.text_frame.paragraphs[0].runs[0]\n    run.text = \"SKU\"          # only changes the\
    \ string; formatting untouched\n- Or set the formatting explicitly after `cell.text`:\n    p = cell.text_frame.paragraphs[0]\n\
    \    r = p.runs[0]\n    r.font.bold = True; r.font.size = Pt(14); r.font.color.rgb = RGBColor(0xFF,0xFF,0xFF)\n\
    - Or use `cell.text_frame.text = ...`/clear-and-write while copying rPr from the old\n  run, or set\
    \ the defaults on the paragraph/`cell` level beforehand.\nNote also that `cell.text = ...` collapses\
    \ multi-paragraph cells to one paragraph.\n"
- id: pptx-cell-02
  answer: "PowerPoint does not reflow the table's column widths — the grid stays as-is (widths\nare fixed\
    \ in `a:gridCol/@w`). The long text instead wraps within the cell: the cell\ngets more lines, and\
    \ because each `a:tr/@h` is a *minimum* height, that row grows\ntaller. Consequences:\n- The row height\
    \ increases, pushing rows below it down; the table's total content\n  height exceeds the graphicFrame's\
    \ `p:xfrm ext cy`, so the table overflows the\n  frame and can run off the slide bottom or overlap\
    \ objects below (footnotes,\n  totals, page numbers).\n- Columns are unchanged, so a long name in\
    \ a narrow column can wrap into many lines,\n  making one row very tall and the layout ugly; text\
    \ may also hit the cell margins\n  and wrap early.\nHow to control it:\n- Shorten/abbreviate the name,\
    \ or allow it to wrap and deliberately grow the row —\n  then fix the frame height and reposition\
    \ anything below.\n- Widen that column (set `table.columns[i].width = Inches(...)`) and shrink others,\n\
    \  keeping the sum equal to the frame width.\n- Reduce cell margins (`cell.margin_left/right`) or\
    \ font size to fit one line.\n- Set row height (`table.rows[r].height`) — PowerPoint treats it as\
    \ a minimum, so it\n  only helps if you also reduce content/margins so text fits.\n- Ensure word wrap\
    \ settings on the cell (`bodyPr` wrap) behave as you intend; disable\n  wrap (`wrap=\"none\"`) to\
    \ keep one line (text may then overflow the cell visually).\n"
- id: pptx-cell-03
  answer: "The Qty is just one number in the table; the deck's derived values become stale and\nmust be\
    \ updated consistently:\n- Line total for that row: Qty × Unit price (that cell in the same row).\n\
    - Any subtotal / tax / discount / grand Total row at the bottom of the table that\n  includes this\
    \ row's amount.\n- Any repeated instance of the number elsewhere: summary/Overview slides, charts\
    \ or\n  chart data cached in the pptx (`embedded` workbook behind a chart), KPI callouts,\n  speaker\
    \ notes, bullet summaries, appendix tables.\n- Totals expressed in text elsewhere (e.g., \"17 units\
    \ shipped\") and any per-item\n  breakdown (allocated quantities, percentages, inventory/coverage\
    \ numbers).\n- If Qty affects price breaks or totals elsewhere (second table, footer note about\n\
    \  minimum order), those too.\nPractical check: recalculate every total/derived figure that consumes\
    \ this Qty and\nverify nothing else in the deck states the old value (5) or the old row total.\n"
- id: pptx-relayout-01
  answer: "python-pptx has no insert-at-position API; `table.add_row()` appends at the end.\nApproach:\
    \ append a new row, then move it into position in the XML, then copy\nformatting from a neighbour.\n\
    \    from copy import deepcopy\n    from pptx.oxml.ns import qn\n\n    table = shape.table\n    table.add_row()\
    \                      # creates a new a:tr at the end\n    tbl = table._tbl\n    trs = tbl.findall(qn('a:tr'))\n\
    \    new_tr = trs[-1]\n    tbl.remove(new_tr)\n\n    # find the index of the \"Total\" row, insert\
    \ before it\n    total_tr = <the a:tr whose first cell text is \"Total\">\n    total_tr.addprevious(new_tr)\
    \         # or index-based: tbl.insert(i, new_tr)\n\n    # copy formatting from the row above (Service\
    \ G's row)\n    src = <that a:tr>\n    # replace fills/rPr per cell: for each target a:tc, replace\
    \ a:tcPr and\n    # each paragraph's pPr/rPr with deepcopies from the source cell\nFormatting details\
    \ to carry over: per-cell `a:tcPr` (fill, borders, margins,\nanchors), and run/paragraph properties\
    \ (`a:rPr`, `a:pPr`) — deep-copy them cell by\ncell from the row above (mapping column by column),\
    \ since a bare `add_row` row\ninherits style only from banding, not explicit fills. Then set the new\
    \ row height\n(`new_tr.set('h', str(src_h))`) and update the graphicFrame height (`p:xfrm ext cy`)\n\
    to the new sum of row heights. Finally fill in the text for each cell\n(`cell.text_frame...`), and\
    \ if banding is explicit, ensure the alternating fills are\ncorrect for the new position (rows after\
    \ the insertion may need their fills flipped\nif shading is hard-coded rather than `bandRow`-driven).\n"
- id: pptx-relayout-02
  answer: "Required XML changes (per column added):\n1. In `<a:tblGrid>`, insert a new `<a:gridCol w=\"\
    ...\"/>` after the Item column's\n   gridCol (position matters: grid order defines column order).\n\
    2. In **every** `<a:tr>`, insert a new `<a:tc>` at the same column index (after the\n   Item cell),\
    \ i.e. each row must gain exactly one cell, so cell count per row still\n   equals the number of gridCols.\
    \ Each new tc needs `<a:txBody>` (bodyPr, lstStyle,\n   `<a:p>`) and an `<a:tcPr/>` (optionally copied\
    \ from a neighbour for fill/borders).\n3. Respect merges: if rows use gridSpan, the insertion index\
    \ must account for it —\n   a gridSpan cell occupies multiple grid columns, so you must split or adjust\n\
    \   gridSpan attributes and keep placeholder `hMerge`/`vMerge` tcs consistent with the\n   new grid.\n\
    4. Header cell gets \"SKU\" text; copy `a:rPr` from the other header cells for style.\nKeeping it\
    \ on the slide — the table already nearly spans the full slide width, so\nafter adding a column the\
    \ grid's total width (sum of `gridCol/@w`) exceeds the\nslide/frame width and would overflow the right\
    \ edge. Options:\n- Reuse width from existing columns: reduce the wide Item/Description column (and\n\
    \  others) so the new column's width comes out of the existing total, keeping\n  `sum(gridCol w)`\
    \ equal to the original frame width. Adjust the `a:gridCol/@w`\n  values directly (or `table.columns[i].width\
    \ = Emu(...)`).\n- Or widen the frame and move/scale it left: increase `p:xfrm ext cx` by the new\n\
    \  column width and, if needed, set `p:xfrm off x` smaller — but the frame must not\n  exceed the\
    \ slide width (`prs.slide_width`), so you'd have to shrink columns anyway.\n- Recommended: keep the\
    \ frame width unchanged (`cx` untouched), give the new column\n  a modest width taken from the Item\
    \ column, and set the new cells' text/wrap so the\n  rows don't grow taller; then verify sum(gridCol)\
    \ == frame cx exactly to avoid\n  PowerPoint rescaling the table on open.\n"
- id: pptx-tblins-01
  answer: 'Measure first, then place. Compute the table''s total height from row heights (each row''s
    height attribute plus internal margins) and its width from column widths, so you know its footprint
    before writing it. Scan the slide''s existing shapes (shape.left, shape.top, shape.width, shape.height)
    and find free space — e.g. iterate top-down until a y where no shape''s vertical span intersects the
    table''s proposed span within an overlapping horizontal band, or simply anchor below the lowest content.
    Then create it with:


    from pptx.util import Inches

    rows, cols = 3, 2

    left, top = Inches(1.0), Inches(4.0)   # chosen free spot

    width, height = Inches(5.0), Inches(1.5)

    gtable = slide.shapes.add_table(rows, cols, left, top, width, height).table


    Note add_table returns a GraphicFrame; the table itself is .table. Set column widths with gtable.columns[i].width
    and row heights with gtable.rows[i].height after creation, and remember python-pptx sets a default
    row height, so the actual rendered height may differ from what you passed — re-read the shapes'' bounds
    after insertion to verify no overlap. Guard against a table whose specified height is only a minimum
    (PowerPoint expands rows to fit text).

    '
- id: pptx-tblins-02
  answer: 'add_table applies the default table style (medium style 2 accent 1, the blue one). To match
    the deck, copy the table style GUID from an existing table''s XML: parse an existing table with


    tbl = existing_shape.table._tbl

    tblPr = tbl.tblPr

    style_id = tblPr.find(qn(''a:tableStyleId''))


    then set the same tableStyleId on the new table''s tblPr. Also copy the tblPr attributes (firstRow,
    bandRow, lastRow, firstCol, etc. — e.g. tblPr.set(''firstRow'', ''1''), tblPr.set(''bandRow'', ''1''))
    so banding/header flags match. Beyond the style, manually copy cell formatting if the deck''s tables
    use per-cell fills: iterate cells and copy fill.fore_color.rgb, font name/size/bold/color, paragraph
    alignment, and cell margins (marT/marB/marL/marR) from a model table''s cells. The style GUID alone
    fixes the overall look; per-cell overrides are needed if the original tables were hand-formatted.

    '
- id: pptx-tblins-03
  answer: "Use the placeholder's insert_table method rather than adding a table to shapes and positioning\
    \ it by hand:\n\nph = None\nfor shape in slide.placeholders:\n    if not shape.has_table and shape.placeholder_format.idx\
    \ == TARGET_IDX:\n        ph = shape; break\nph.insert_table(rows, cols)\n\nIn python-pptx, a content\
    \ placeholder (PLACEHOLDER object type) exposes insert_table(), which returns a GraphicFrame and fills\
    \ the placeholder's own geometry, so position/size come from the layout automatically. Select the\
    \ right placeholder by placeholder_format.idx (the idx is what links it to the layout) or by placeholder_format.type,\
    \ and verify it is currently empty (it won't have a graphic frame yet — checking not ph.has_table\
    \ / that no existing shape occupies that idx). Then fill in the cells and, if needed, adjust widths/heights\
    \ on the returned table.\n"
- id: pptx-imgrep-01
  answer: 'There is no replace(); do it in three steps while preserving the spTree order.


    1. Read the old picture''s placement: pic = old_shape; left, top, width, height = pic.left, pic.top,
    pic.width, pic.height.

    2. Determine the image bytes (read the new file with open(path,''rb'') and pass to add_picture(path,
    left, top, width, height) — python-pptx infers type from the extension; if the format differs, use
    the file-object form so it doesn''t guess wrong).

    3. Add the new picture and move it into the old element''s position in the XML, then remove the old
    element.


    new_pic = slide.shapes.add_picture(path, left, top, width, height)

    old_el = pic._element

    old_el.addprevious(new_pic._element)   # insert new before old, keeping order

    old_el.getparent().remove(old_el)


    Because addprevious inserts the new element immediately adjacent to the old one, the stacking order
    (z-order = document order in spTree) is preserved exactly. If you only appended at the end, the picture
    would jump to the front. Keeping width and height identical to the originals also preserves aspect
    handling: note that passing both width and height stretches the image — if you must preserve the source
    aspect ratio, compute height = round(width * img_h / img_w) using the new image''s native dimensions
    and only pass width (or accept the old frame''s aspect, which is what "same position and size" means
    here).

    '
- id: pptx-imgrep-02
  answer: 'Because PowerPoint .pptx files deduplicate images: every picture that references the same source
    shares a single image part (ppt/media/imageN.ext) and a single relationship to it. python-pptx''s
    image handling caches by content hash — if you mutate the bytes of the ImagePart (or of the underlying
    package part) that several slides'' rels point at, you are editing the one shared part, so every picture
    using it updates. The logo on slide 7 is not a separate copy; it is another relationship to the very
    same image part.


    Fix: don''t overwrite shared bytes. Instead create a new image part (e.g. call slide_part.get_or_add_image_part(path),
    which hashes and dedups, or add a fresh picture and use the resulting rId), then repoint only the
    target picture''s blipfill: blip = pic._element.blipFill.blip (or pic._element.find(''.//a:blip'',
    NS)); blip.set(qn(''r:embed''), new_rId). Leave slide 7''s blip pointing at the old rId. Also be aware
    that parts shared across slides live at package scope, so the overwrite appears everywhere, not just
    on the same slide.

    '
- id: pptx-imgrep-03
  answer: "drop_rel(rId) only unlinks the relationship from that one part's .rels; it does not reference-count\
    \ usage elsewhere in the package. The mistake is assuming the relationship was uniquely owned by the\
    \ picture you deleted.\n\nTwo failures are going on: (a) other pictures on the same slide — and possibly\
    \ on other slides — share the same image part, and because two shapes on one slide blipFill-ing the\
    \ same image typically share the single slide-level relationship (python-pptx reuses an existing rId\
    \ for identical image bytes via get_or_add_image_part), removing that one rId severs the reference\
    \ for every blipFill that still says r:embed=\"rId7\"; (b) the media part itself may remain in the\
    \ package (drop_rel does not delete the part), so you end up with an orphaned part and dangling r:embed\
    \ references, which PowerPoint repairs by dropping the broken picture.\n\nCorrect approach: only drop\
    \ the rel if nothing else references it. Count usages first:\n\nrids = [b.get(qn('r:embed')) for b\
    \ in slide.shapes._spTree.iter(qn('a:blip'))]\nif rids.count(old_rid) <= 1:\n    pic._element.getparent().remove(pic._element)\n\
    \    slide.part.drop_rel(old_rid)\n\nAnd if the goal is to purge the bytes entirely, remove the image\
    \ part from the package after confirming no blip (and no hyperlinks/other rels of type image) still\
    \ point to it.\n"
- id: pptx-imgins-01
  answer: 'Compute, don''t guess. Signature is typically wide-and-short, so:


    1. Read native size: from PIL import Image; with Image.open(''signature.png'') as im: w0, h0 = im.size
    (or use python-pptx by adding the picture off-slide and reading .image.size, then removing it).

    2. Scale: width = Inches(2.0); height = Emu(int(width * h0 / w0)) — pass only width to add_picture
    and python-pptx derives height from the aspect ratio; passing both would distort it.

    3. Find free space in the bottom-right: compute margins, say right = slide_width - Inches(0.3), bottom
    = slide_height - Inches(0.3). Then check for collisions by intersecting the proposed rect (left =
    right - width, top = bottom - height, width, height) against every other shape''s (left, top, width,
    height); if it overlaps, walk top upward (or leftward) in small increments until it''s clear, or fall
    back to the largest clear region in that quadrant. Slide dimensions come from prs.slide_width / prs.slide_height.


    pic = slide.shapes.add_picture(''signature.png'', left, top, width=Inches(2.0))

    (height is auto-derived)


    If nothing fits without overlap, tell the user rather than covering content — or reduce the width
    until it fits the clear band.

    '
- id: pptx-imgins-02
  answer: 'Because a picture placeholder crops to the placeholder''s frame: insert_picture() fits the
    image into the placeholder''s fixed rectangle and, per PowerPoint''s behaviour, crops rather than
    squashes — so with a portrait photo in a landscape (or otherwise differently-proportioned) placeholder,
    the parts that don''t fit are cut off. The placeholder''s crop is applied to the blipFill (srcRect)
    based on the placeholder''s aspect ratio versus the image''s.


    Ways around it: adjust the crop yourself afterwards — pic = placeholder.insert_picture(path); then
    set pic.crop_left/.crop_right/.crop_top/.crop_bottom so the full image shows (compute the needed crop
    from image aspect vs placeholder aspect), or resize the placeholder to match the photo''s aspect ratio
    before inserting, or insert the image as a regular picture (slide.shapes.add_picture) instead of filling
    the placeholder if you want it uncropped and scaled to fit.

    '
- id: pptx-legacy-01
  answer: 'No — python-pptx only supports Office Open XML (.pptx/.potx/.ppsx); it cannot open the binary
    .ppt (97–2003) format and will raise on it. Workflow:


    1. Convert first: soffice --headless --convert-to pptx deck.ppt (LibreOffice) — or use PowerPoint
    itself / Apple Keynote to save as .pptx — producing deck.pptx in a chosen outdir.

    2. Verify the conversion before editing: open it, confirm slides, tables, fonts, images, and chart
    objects survived; LibreOffice''s importers can alter layouts, drop unsupported features, and re-flow
    text.

    3. Edit the converted .pptx with python-pptx (find the table via shape.has_table, rewrite cell text,
    then save to a new file — never in place).

    4. Deliver the .pptx, and tell the user it''s a converted file: format was upgraded, so some fidelity
    may differ from the original, and the original .ppt should be kept as the source of truth.


    There is no supported path for modifying .ppt directly with this library.

    '
- id: pptx-legacy-02
  answer: 'Before editing: capture the baseline. Take the file size, slide count, thumbnail/notes counts,
    a screenshot or PDF export of every slide, and record any known-sensitive content and embedded objects
    (OLE, charts, SmartArt, animations, transitions, speaker notes, master/layout customizations, fonts,
    section names, hyperlinks). Confirm conversion fidelity slide by slide — check text overflow, table
    grid/borders, column widths, merged cells, images and their positions, theme colors, and any macros/VBA
    (which are lost).


    After editing: re-diff against the baseline — same slide count, nothing shifted, only the intended
    cells changed, no missing images, no text reflow elsewhere, no "repaired" prompt when opened. Open
    the result in actual PowerPoint (not just LibreOffice) to confirm it opens cleanly and renders as
    expected.


    Tell the user: the file was converted .ppt → .pptx, so it is now a different format; some fidelity
    may have been lost in conversion (formatting, fonts, embedded objects, animations, macros), so the
    original .ppt should be kept and the diff should be reviewed; also warn if any macro content or embedded
    binary objects were dropped by the converter.

    '
- id: pptx-verify-01
  answer: 'Save to a new file, then verify — never write over the input.


    1. Work on a copy: shutil.copy(src, work.pptx), or have the script read src and write out.pptx (prs.save(''out.pptx'')).

    2. Validate the package structurally before handing it off: open the saved file with python-pptx again
    (Presentation(''out.pptx'')) as a smoke test; check the zip is intact (zipfile.ZipFile(path).testzip()
    should return None); optionally run OPC/Content_Types sanity checks so missing parts or bad rels surface
    here rather than in PowerPoint.

    3. Optionally validate XML well-formedness of the changed parts (slideN.xml, table parts) with lxml/ElementTree
    parse.

    4. Render/open for real: open in PowerPoint (or soffice --headless --convert-to pdf) and check the
    edited slide visually; PowerPoint''s repair prompt is the real test of OPC validity.

    5. Compare before/after (slide count, shape count on the target slide) to confirm only intended changes
    occurred.


    Sequence: backup → edit in memory → save to new path → re-open & structural checks → visual render
    check → only then replace the original if requested (keeping the backup).

    '
- id: pptx-verify-02
  answer: 'PowerPoint''s "needs to be repaired" almost always means invalid OOXML or a broken relationship.
    Common causes:


    - Malformed XML: unclosed tags, duplicated or missing required elements, wrong namespace prefixes/URIs
    after hand-editing (e.g. using a bare prefix without xmlns:a declared).

    - Wrong element ordering: OOXML uses strict sequences — for a table, tblPr must come before gridCol/tr
    elements; within a tr, tc order matters; tblGrid must precede the rows. Inserting a <a:tc> or <a:tblPr>
    in the wrong position fails schema validation.

    - Missing/empty required containers: no tblGrid, zero gridCol entries, a tr without any tc, missing
    graphicFrame parent, missing a:tbl inside the graphicFrame''s a:graphic/a:graphicData with the correct
    table URI.

    - Duplicate primary keys: two shape ids (cNvPr id) equal on the same slide; duplicate rIds; relationship
    target missing (r:embed pointing to a dropped rel) or target with wrong mode/type.

    - Content_Types mismatch: a part added without a matching Override/Default entry, or a wrong extension.

    - Bad/missing fill on cells: invalid prstGeom, invalid srgbClr values (not 6 hex digits), a solidFill
    referencing a theme index that doesn''t exist.

    - Truncated file, or edited while open, or saved by a script with the file locked — the zip itself
    is corrupt.


    Debug by unzipping and xmllint/parsing each changed part, then checking rels and [Content_Types].xml.

    '
- id: pptx-verify-03
  answer: "No — not really. Deleting the p:pic element removes the shape from the slide, but the image\
    \ part (ppt/media/imageN.ext) usually remains inside the .pptx zip, because nothing in that operation\
    \ deletes the part itself. The slide's .rels may still contain the relationship too (unless you explicitly\
    \ drop_rel), and the same image is often shared with other slides, so it will definitely still be\
    \ there.\n\nHow to check: unzip the package and look, e.g.\n\n  unzip -l deck.pptx | grep ppt/media\n\
    \nor in Python:\n\n  import zipfile\n  z = zipfile.ZipFile('deck.pptx')\n  names = z.namelist()  \
    \        # look for ppt/media/<name>\n  # and inspect the slide's relationships:\n  print(z.read('ppt/slides/_rels/slide1.xml.rels'))\n\
    \nTo actually purge it: confirm no blip anywhere still references the rel (check all slides' blip\
    \ r:embed values), remove the relationship from the owning part (slide.part.drop_rel(rId)), and remove\
    \ the image part from the package / rebuild without it — then re-list ppt/media to confirm it's gone.\
    \ Also consider that images can be embedded via layouts/masters, oleObjects, or linked (external)\
    \ relationships, so grep the whole package, not just that slide.\n"
