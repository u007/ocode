- id: pptx-model-01
  answer: "A table is a single DrawingML graphic frame shape, not a group of shapes.\nA table only appears\
    \ in two places inside the package: a content placeholder\nin the slide layout (and optionally the\
    \ master) carries\n`<p:graphicFrame>` whose `<a:graphicData uri=\".../table\">` contains a\n`<a:tbl>`,\
    \ or the slide itself carries a `<p:graphicFrame>` with the same\ncontent.\n\nPart naming: `ppt/slides/slideN.xml`,\
    \ `ppt/slideLayouts/slideLayoutN.xml`,\n`ppt/slideMasters/slideMasterN.xml`.\n\nThe element tree:\n\
    \n```xml\n<p:graphicFrame>\n  <p:nvGraphicFramePr>          <!-- p:cNvPr id/name, p:cNvGraphicFramePr,\
    \ p:nvPr -->\n  <p:xfrm>                      <!-- position/size: a:off x,y + a:ext cx,cy -->\n  \
    \  <a:off x=\"...\" y=\"...\"/>\n    <a:ext cx=\"...\" cy=\"...\"/>\n  </p:xfrm>\n  <a:graphic>\n\
    \    <a:graphicData uri=\"http://schemas.openxmlformats.org/drawingml/2006/table\">\n      <a:tbl>\n\
    \        <a:tblPr firstRow=\"1\" bandRow=\"1\" rtl=\"0\">   <!-- band/bandRow flags -->\n        \
    \  <a:tableStyleId>                         <!-- optional style GUID -->\n        </a:tblPr>\n   \
    \     <a:tblGrid>                                <!-- the COLUMNS: one a:gridCol per column -->\n\
    \          <a:gridCol w=\"2286000\"/>\n          <a:gridCol w=\"2286000\"/>\n        </a:tblGrid>\n\
    \        <a:tr h=\"370840\">                          <!-- a ROW, height in EMU -->\n          <a:tc\
    \ gridSpan=\"2\" hMerge=\"0\" vMerge=\"0\" rowSpan=\"1\">\n            <a:txBody>                \
    \             <!-- the CELL TEXT -->\n              <a:bodyPr/>\n              <a:lstStyle/>\n   \
    \           <a:p>\n                <a:pPr algn=\"l\"/>\n                <a:r>\n                  <a:rPr\
    \ lang=\"en-US\" sz=\"1400\" b=\"1\">\n                    <a:solidFill><a:srgbClr val=\"FFFFFF\"\
    /></a:solidFill>\n                  </a:rPr>\n                  <a:t>SKU</a:t>\n                </a:r>\n\
    \                <a:endParaRPr lang=\"en-US\"/>\n              </a:p>\n            </a:txBody>\n \
    \           <a:tcPr marL=\"..\" marR=\"..\" marT=\"..\" marB=\"..\" anchor=\"ctr\">\n            \
    \  <a:lnL/><a:lnR/><a:lnT/><a:lnB/>\n              <a:solidFill><a:srgbClr val=\"F2F2F2\"/></a:solidFill>\n\
    \            </a:tcPr>\n          </a:tc>\n        </a:tr>\n      </a:tbl>\n    </a:graphicData>\n\
    \  </a:graphic>\n</p:graphicFrame>\n```\n\nSo the mapping is: `a:tbl` = the table, `a:tblGrid/a:gridCol`\
    \ = columns\n(with `w` = width in EMU), `a:tr` = rows (with `h` = height in EMU),\n`a:tc` = cells,\
    \ and text lives in `a:tc/a:txBody/a:p/a:r/a:t` with the run\nproperties in `a:r/a:rPr` and paragraph\
    \ properties in `a:p/a:pPr`. `a:tcPr`\nholds cell margins, anchor, borders and fill.\n\npython-pptx\
    \ mirrors this: `shape.has_table` / `shape.table`,\n`table.rows` -> `_Row` (`row.height`, `row.cells`),\
    \ `table.columns`\n(`column.width`), `table.cell(r, c)`, `cell.text_frame`, and\n`cell.merge()` /\
    \ `cell.split()`. The `graphic_frame.element` (or\n`graphic_frame._element`) is the `p:graphicFrame`,\
    \ and\n`graphic_frame.table._tbl` is the `a:tbl` for raw lxml work.\n\nOrdering and invariants that\
    \ matter: the child order inside `a:tc` is\n`a:txBody` then `a:tcPr` (a `tcPr` before `txBody` is\
    \ out of schema order and\nmakes PowerPoint offer to \"repair\"); inside `a:tcPr` the `a:ln*` elements\
    \ come\nbefore the fill; inside `a:tbl` it is `a:tblPr`, `a:tblGrid`, then the `a:tr`\nlist.\n"
- id: pptx-model-02
  answer: "Everything in DrawingML that python-pptx surfaces as a `Length` is in\n**English Metric Units\
    \ (EMU)**, an integer.\n\n* 1 inch = 914400 EMU\n* 1 point (pt) = 12700 EMU\n* 1 cm = 360000 EMU\n\
    * 1 mm = 36000 EMU\n* 1 pixel (at 96 dpi) = 9525 EMU\n\nIn practice most `pptx` values round numbers:\
    \ a common table cell width is\n2286000 EMU (2.5 in) or 1828800 (2 in), and typical font sizes in\
    \ `a:rPr sz`\nare in **hundredths of a point** (`sz=\"1400\"` = 14 pt). Slide size\n(`presentation.slide_width`,\
    \ `presentation.slide_height`) is also EMU.\n\nDefault deck sizes:\n* 4:3 = 9144000 x 6858000 EMU\
    \ (10 x 7.5 in, i.e. 25.4 x 19.05 cm) — the\n  classic \"On-screen Show (4:3)\" default.\n* 16:9 =\
    \ 12192000 x 6858000 EMU (13.333 x 7.5 in, 33.867 x 19.05 cm) — the\n  current PowerPoint \"Widescreen\"\
    \ default.\n* 16:10 = 12192000 x 7620000 EMU (13.333 x 8.333 in) is also seen.\n\npython-pptx gives\
    \ `Length` objects that are `int` subclasses with\nconvenience properties: `.inches`, `.cm`, `.mm`,\
    \ `.pt`, `.emu`, and you can\nwrite either (`Inches(2)`, `Emu(914400)`, or a plain int, which is treated\
    \ as\nEMU). Beware `font.size` returns a `Length` in EMU, so 14 pt is\n`Pt(14) == 177800`, not `14`\
    \ — assigning the bare number 14 yields a\n0.14 pt font.\n"
- id: pptx-model-03
  answer: "It is on the **slide master** (and/or the slide layout), not on the slide\npart. `slide.shapes`\
    \ only iterates the shapes actually present in\n`ppt/slides/slideN.xml`, and a logo inherited from\
    \ the layout/master has no\n`p:sp` on the slide, so it is invisible to that loop. You find it via\n\
    `slide.slide_layout.shapes` / `slide.slide_layout.placeholders` and\n`slide.slide_layout.slide_master.shapes`\
    \ (e.g. `slide_master.background`,\n`slide_master.shapes`); `slide.follow_master_background` /\n`slide_layout.follow_master_background`\
    \ control inheritance, and a layout\ncan suppress master content with `showMasterSp=\"0\"` on\n`p:sldLayout`\
    \ / `p:sld` attributes.\n\nConsequences of editing it there:\n\n* It is **shared**: every slide that\
    \ inherits that layout/master shows the\n  change. That is the point for a logo, but it is a global\
    \ blast radius, and\n  you cannot fix one slide without either overriding on that slide or making\n\
    \  a separate layout.\n* Editing through `slide_layout.shapes` / `slide_master.shapes` is the\n  supported\
    \ path: the shape object model still works, and python-pptx saves\n  the modified layout/master part.\n\
    * The media part is owned by the **layout/master part**, so\n  `pic._element.blip_rId` must be resolved\
    \ against\n  `pic.part` (`slide_layout.part` / `slide_master.part`), not the slide.\n  Dropping a\
    \ rel on the wrong part either fails or leaves a dangling\n  reference.\n* If you `deepcopy` a shape\
    \ from the master onto a slide you must assign a\n  fresh `p:cNvPr/@id` on the copy, otherwise duplicate\
    \ ids in one slide make\n  PowerPoint demand a repair.\n* Some python-pptx helpers are awkward here:\
    \ `Shapes` on a master/layout has\n  no add-picture clone of the slide's API in all versions, and\n\
    \  `placeholder` insertion often needs `insert_element_before` with the right\n  successor tag, because\
    \ the schema order in `p:spTree` is\n  `p:nvGrpSpPr, p:grpSpPr, shapes...`.\n* Design note: if the\
    \ logo really is per-slide (different crop or size on\n  one slide), the correct fix is to override\
    \ on that slide rather than to\n  diverge the master.\n"
- id: pptx-locate-01
  answer: "**Find the table by its header text, not by `tables[0]`, by shape name, or by\nposition.**\
    \ A slide can hold several tables (and shapes can be nested in\n`GroupShape`s), and the first `has_table`\
    \ shape is not necessarily the one you\nmean.\n\n```python\nfrom pptx import Presentation\nfrom pptx.enum.shapes\
    \ import MSO_SHAPE_TYPE\n\ndef iter_shapes(shapes):\n    for sh in shapes:\n        if sh.shape_type\
    \ == MSO_SHAPE_TYPE.GROUP:\n            yield from iter_shapes(sh.shapes)\n        else:\n       \
    \     yield sh\n\nprs = Presentation(\"deck.pptx\")\nslide = prs.slides[2]                      #\
    \ slide 3 -> index 2\n\nexpected = [\"Item\", \"Qty\", \"Unit Price\", \"Amount\"]\ntables = []\n\
    for sh in iter_shapes(slide.shapes):\n    if sh.has_table:\n        header = [c.text_frame.text.strip()\
    \ for c in sh.table.rows[0].cells]\n        if header == expected:              # or: header[:1] ==\
    \ [\"Item\"]\n            tables.append(sh.table)\nassert len(tables) == 1, f\"expected 1 table, found\
    \ {len(tables)}\"\ntable = tables[0]\n```\n\n**Find the row by exact, stripped cell text** (not a\
    \ substring, not\n`.startswith`):\n\n```python\nhits = [i for i, row in enumerate(table.rows)\n  \
    \      if row.cells[0].text_frame.text.strip() == \"Gadget D\"]\nassert len(hits) == 1, f\"expected\
    \ exactly 1 'Gadget D' row, found {hits}\"\nrow_idx = hits[0]\n```\n\nTwo traps worth stating:\n\n\
    * `row.cells[0]` may be a *covered* cell if the row is merged at column 0;\n  the text lives in the\
    \ merge **origin**. To be safe, find the row by\n  scanning every cell in the row:\n  `any(c.text_frame.text.strip()\
    \ == \"Gadget D\" for c in row.cells)`, or\n  locate the origin via `table.cell(r, c).is_merge_origin`.\n\
    * `c.text` on a `TableCell` returns the visible text of the cell; use\n  `cell.text_frame.text` when\
    \ you need the raw paragraph text.\n\nWhen you are editing the located object, keep a handle on the\n\
    `GraphicFrame` (the shape), not only the `Table`, because you may need\n`frame.height` and `frame.element`\
    \ afterwards.\n"
- id: pptx-locate-02
  answer: "**How a merge is represented.** There is no separate merge list: merges are\nimplied by attributes\
    \ on `a:tc`.\n\n* Horizontal: the origin `a:tc` carries `gridSpan=\"2\"` (number of grid\n  columns\
    \ covered) and carries the text. Every covered column in that row\n  gets its own `a:tc` element with\
    \ `hMerge=\"1\"` and empty/duplicate text.\n* Vertical: the origin carries `rowSpan=\"N\"`, and each\
    \ covered row has an\n  `a:tc` with `vMerge=\"1\"`; the first continuation row may use\n  `vMerge=\"\
    1\"` as the continue marker.\n* Both: `gridSpan` and `rowSpan` on the origin together, with `hMerge`/`vMerge`\n\
    \  on all the covered cells.\n\nThe invariant that makes this safe to reason about: **every `a:tr`\
    \ contains\nexactly one `a:tc` for every `a:gridCol`**, covered cells included. So the\n*n*-th `a:tc`\
    \ of a row is grid column *n*, which is why `row.cells[c]` and\n`table.cell(r, c)` refer to the same\
    \ grid position. In python-pptx:\n`cell.is_merge_origin`, `cell.is_spanned` (spanned = covered),\n\
    `cell.span_width`, `cell.span_height`; `cell.merge(other)` and\n`cell.split()` are the supported operations.\n\
    \n**What to watch when editing or deleting around it.**\n\n* Write text to the **origin**, never to\
    \ a covered cell. Text written to an\n  `hMerge`/`vMerge` cell is invisible in PowerPoint (it lives\
    \ in a hidden\n  cell), and the edit silently does nothing visible.\n* When you delete a row, a `rowSpan`\
    \ origin in a *previous* row must be\n  decremented (or its continuation `vMerge` cells removed),\
    \ otherwise the\n  merge now covers a different physical row than you intend.\n* When you insert a\
    \ row *inside* a vertically merged block, you must add\n  `vMerge=\"1\"` continuation cells for the\
    \ new row and bump the origin's\n  `rowSpan`, or the block is short by one row and PowerPoint's grid\n\
    \  disagrees with the file.\n* When you insert or delete a **column**, a horizontal merge whose\n\
    \  `gridSpan` crosses the insertion point must be adjusted (see\n  `pptx-relayout-02`), and you must\
    \ keep the per-row `a:tc` count equal to the\n  `a:gridCol` count. A mismatch here is one of the classic\
    \ \"PowerPoint needs\n  to repair\" causes, and python-pptx will not warn.\n* `table.cell(r, c)` is\
    \ a grid lookup, not a physical-cell lookup: it may\n  return a spanned cell. If you are slicing rows\
    \ or reordering, check\n  `is_merge_origin` before treating a cell as the real content.\n* `a:tc`\
    \ child order is `a:txBody` then `a:tcPr`; keep that when hand-editing\n  or when you build a replacement\
    \ `a:tc`.\n"
- id: pptx-rowdel-01
  answer: "There is **no** `table.rows.remove()` and no `table.delete_row()` /\n`remove_row()` API in\
    \ python-pptx. `_Rows` and `_Row` expose only iteration,\nindexing, `len`, and (on the collection)\
    \ the ability to change nothing else;\n`Table` gives you `add_row()` (which appends at the bottom)\
    \ and `cell()`.\nDeleting must go through the lxml element:\n\n```python\nfrom pptx.oxml.ns import\
    \ qn\n\ntbl = table._tbl\ntr = table.rows[row_idx]._tr        # or tbl.tr_lst[row_idx]\ntbl.remove(tr)\
    \                      # lxml Element.remove(parent)\nassert len(tbl.tr_lst) == len(table.rows)\n\
    ```\n\n(`Element.getparent().remove(Element)` also works, but here `tr`'s parent\n*is* the `a:tbl`.)\n\
    \nCaveats to handle in the same edit:\n\n* **Fix the frame height afterwards** — see `pptx-rowdel-02`.\n\
    * If the deleted row was a vertical-merge origin, fix `rowSpan` and the\n  `vMerge` cells (see `pptx-locate-02`).\
    \ The lowest row in a block is\n  sometimes marked with `vMerge=\"1\"` rather than a \"restart\" value,\
    \ so\n  blindly decrementing can leave a `vMerge` chain pointing at a row that no\n  longer exists.\n\
    * If you later re-insert (`addnext`), re-use the removed `a:tr` (optionally\n  after clearing its\
    \ text) so the `a:tc`/`a:gridCol` parity and the cell\n  formatting survive; that is the recommended\
    \ \"delete + insert\" for\n  `pptx-relayout-01`.\n* Do **not** use `table.add_row()` as a delete substitute:\
    \ new rows come with\n  default formatting and are appended at the end, and `add_row()` cannot be\n\
    \  positioned.\n"
- id: pptx-rowdel-02
  answer: "Yes, it matters. `p:graphicFrame/p:xfrm/a:ext/@cy` is the frame's stored\nheight. python-pptx's\
    \ `graphic_frame.height` is that attribute, and the\n`a:tr/@h` values are independent of it. python-pptx\
    \ does not recompute the\nframe from the rows: only assigning `row.height` updates the frame (it\n\
    adjusts the graphic frame extent to keep rows in place), and a raw\n`tbl.remove(tr)` / `tr.addnext(...)`\
    \ leaves the frame at its old value.\n\nConsequences:\n\n* PowerPoint renders rows using the `a:tr/@h`\
    \ heights but the shape's\n  geometry still claims the old box. In practice the table looks fine\n\
    \  (PowerPoint largely re-derives row layout), but the shape is wrong for\n  anything that reads geometry:\
    \ selection handles, the Shapes list, hit\n  testing / \"bring to front\" alignment, connectors and\
    \ anchored groups that\n  reference the frame's box, and export/scale operations. With repeated\n\
    \  programmatic edits the drift accumulates and the frame no longer matches\n  the table at all.\n\
    * After an *insert*, the same rule: the frame is too small, so a following\n  shape (Total row label,\
    \ footnote textbox, chart) can visually overlap or the\n  table can be clipped.\n\nFix — always finish\
    \ a row delete or insert with:\n\n```python\nframe = shape                       # the GraphicFrame\n\
    frame.height = sum(r.height for r in frame.table.rows)\n# or, summing the element attributes:\n# \
    \  frame.height = sum(int(tr.get('h')) for tr in table._tbl.tr_lst)\n```\n\nthen sanity-check that\
    \ the new bottom edge is still on the slide and does not\ncollide with whatever sits below:\n\n```python\n\
    bottom = frame.top + frame.height\nassert bottom <= prs.slide_height\nfor other in slide.shapes:\n\
    \    if other is frame and other.top > frame.top:\n        assert other.top >= bottom, \"table now\
    \ overlaps the shape below\"\n```\n\nNote that `row.height` sums can disagree with what PowerPoint\
    \ ends up\ndrawing (rows auto-grow to fit text), so the frame height is a best-effort\nvalue, not\
    \ a promise.\n"
- id: pptx-rowdel-03
  answer: "The banding in a pptx table is normally **not per-row fill at all** — it is\ngenerated by the\
    \ table style. `a:tblPr` carries `firstRow=\"1\" bandRow=\"1\"`\nand the `<a:tableStyleId>` GUID of\
    \ one of the built-in `{5C22544A-...}`\nstyle families; the renderer alternates light/dark band colours\
    \ every\n*second* row. There is no per-row \"which parity am I\" data anywhere.\n\nThat means the\
    \ two adjacent same-coloured rows are simply the consequence of\ndeleting a row: the remaining rows\
    \ keep their own order but the renderer\nre-assigns parity by position, so the alternation flips relative\
    \ to what it\nwas.\n\n**Why it shows in one deck and not another:**\n\n* **Direct cell fill.** In\
    \ the hard-coded decks the author clicked \"banded\n  rows\" off and set `a:solidFill` in `a:tcPr`\
    \ on alternating rows, or\n  applied a row style. Those fills are *positional data on the rows that\n\
    \  survived*, so the surviving shading never changes when you delete a row —\n  and a middle row of\
    \ a given parity disappears without re-banding, which\n  looks \"wrong\" for the same reason. Some\
    \ fills may be on the paragraph/run\n  instead of the cell.\n* **`bandRow` flag or `tblPr` absent.**\
    \ If `a:tblPr` has no `bandRow=\"1\"`, or\n  the `tableStyleId` points to a style whose banding is\
    \ off, there is no\n  style banding to inherit; but the table style can still be a *major/minor*\n\
    \  theme style whose `band1`/`band2` fills resolve to the same colour\n  (e.g. `band2` not defined\
    \ in the tableStyles part) — then alternating rows\n  legitimately look identical.\n* **Theme/table-styles\
    \ part differences.** `ppt/tableStyles.xml` (and\n  `p:defaultTableStyle` in the master) can override\
    \ or supply a\n  non-default style; different masters in one file can carry different\n  table styles,\
    \ so the same table code renders differently on different\n  slides.\n* **Hard-coded per-row height/banding\
    \ written by a different tool.** Docs\n  produced by a generator often bake `a:solidFill` per row;\
    \ hand-edited decks\n  rely on the style.\n* **Table style per cell (`a:tcPr` `bandRow`-driven vs\
    \ `a:tblPr firstRow`)**:\n  the header (first row) is governed by `firstRow=\"1\"`, so deleting a\
    \ row\n  near the top can make the old second row become the styled \"header\" — a\n  different-looking\
    \ first band.\n\nIn short: parity is computed from row order when banding comes from the\nstyle, and\
    \ baked onto the rows when banding comes from explicit fills. Your\ndelete cannot fix the former without\
    \ re-banding deliberately, and it shifts\nthe former while leaving the latter alone.\n"
- id: pptx-rowdel-04
  answer: "It hides data without removing it, and it produces a file that is\nstructurally correct but\
    \ semantically wrong — the worst kind of pptx bug,\nbecause every structural check passes and the\
    \ damage only shows up later.\n\nSpecifically:\n\n* **The row is still in the file.** `ppt/slides/slideN.xml`\
    \ still contains\n  the `a:tr`, its `w:t` text (\"Gadget D\", its Qty, its price), the product\n \
    \ name, and any numbers. Anyone opening the zip, running a regex over the XML,\n  extracting text\
    \ with `python-pptx`/pandoc/LibreOffice, or reading the deck\n  with an accessibility tool gets the\
    \ \"deleted\" line item back. So it fails\n  redaction, fails data-removal requests, and fails any\
    \ test that asserts the\n  old text is gone. Deleting the `a:tr` is the only thing that removes it\n\
    \  from every part (see the verification step in the guidance: assert the old\n  text is absent from\
    \ *all* `ppt/slides/slideN.xml`, not just the edited one).\n* **A white rectangle is a separate shape**\
    \ that must be kept in sync: it\n  does not shrink or move when rows are added or deleted, and it\
    \ can cover\n  content the row was *supposed* to reveal. It also can be defeated by theme\n  or background\
    \ changes, by \"reset slide\" in PowerPoint, by\n  `p:showMasterSp`, or by selecting shapes.\n* **Near-zero\
    \ height is not a valid row.** `a:tr/@h` has a minimum\n  (PowerPoint effectively clamps to roughly\
    \ 0.25-0.3 in) and the row still\n  occupies that band, so a visually \"collapsed\" row is still present\
    \ in the\n  table structure — and it can reappear expanded if a user drags a row\n  border, or if\
    \ PowerPoint recalculates row heights to fit content.\n* **White text is a styling lie.** The `a:t`\
    \ is still there, still in the\n  reading order, still in the file; the text is just invisible on\
    \ white. It\n  stays selectable, it is what screen readers announce, and it shows up if\n  the table\
    \ style's band fill changes or the slide background changes.\n* **It also corrupts totals silently.**\
    \ If the row is really gone from the\n  arithmetic but still in the file, the file becomes an inconsistent\
    \ record:\n  you can no longer tell from the deck whether the item was never there or\n  was hidden.\n\
    \nThe right move is a real structural edit: remove the `a:tr` (and fix the\nframe height, merges and\
    \ band parity), and if a heading is needed where the\nrow was, insert a genuinely empty row or add\
    \ a text box — not an occluder.\n"
- id: pptx-cell-01
  answer: "`cell.text = \"SKU\"` **destroys the cell's formatting** and replaces the\ncontent with a single,\
    \ unformatted-ish paragraph.\n\nWhat `cell.text` does (it delegates to `text_frame.text`):\n\n* Clears\
    \ the `a:txBody` and writes one new `a:p` containing a single run.\n* Destroys the **old runs' formatting**:\
    \ the bold, the 14 pt size, the white\n  `a:solidFill`, the font (e.g. Arial), any underline/italic/caps.\
    \ The new\n  run gets whatever the *paragraph's* `a:endParaRPr` or the **table style /\n  table style's\
    \ `firstRow` band** resolves to — which for a header row in a\n  banded style is often plain black\
    \ body text, not white bold. So a header\n  cell that was white-on-dark becomes invisible black-on-dark.\n\
    * Destroys the **paragraph structure**: all but the first paragraph, line\n  breaks (`a:br`), tabs,\
    \ and any `a:pPr` (alignment, bullet, level,\n  indentation, spacing).\n* Destroys the **cell properties**?\
    \ No — `a:tcPr` (fill, borders, margins,\n  anchor) survives, because it is a sibling of `a:txBody`,\
    \ not part of it. So\n  the cell background and borders stay, but the text appearance does not.\n\
    * In python-pptx, the returned `cell.text_frame.paragraphs[0].runs[0]` has a\n  `font` with `size\
    \ is None`, `bold is None` — i.e. all formatting is\n  \"inherit from the style\", not \"keep 14 pt\
    \ bold white\".\n\n**How to keep the formatting:**\n\n```python\n# Preferred: rewrite only the first\
    \ run, leave the rest of the run props alone\npara = cell.text_frame.paragraphs[0]\nif para.runs:\n\
    \    para.runs[0].text = \"SKU\"          # keeps a:rPr verbatim\n    for extra in para.runs[1:]:\n\
    \        extra._r.getparent().remove(extra._r)   # drop stale extra runs\nelse:\n    cell.text = \"\
    SKU\"                  # no run to preserve; then set it up\n    r = cell.text_frame.paragraphs[0].runs[0]\n\
    \    r.font.bold = True\n    r.font.size = Pt(14)\n    r.font.color.rgb = RGBColor(0xFF, 0xFF, 0xFF)\n\
    ```\n\nImportant: `run.text = ...` *does* preserve `a:rPr` (only\n`TextFrame.text` / `cell.text` reset\
    \ the body). The tricky part is that the\ntext you see may be **split across several runs** (spell-check\
    \ state,\nproofing language runs, tracked edits), so a byte-level `str.replace` on\n`document`-style\
    \ XML is unreliable — always locate the cell by parsing, and\nwhen a single replacement string is\
    \ longer than the original, assert the\nneighbouring runs have identical `a:rPr` before collapsing\
    \ them, and\nremember `a:t` needs `xml:space=\"preserve\"` if the text has leading/trailing\nspaces.\n\
    \nIf you do have to rebuild the run, set everything explicitly —\n`r.font.size`, `.bold`, `.name`,\
    \ `.color.rgb` — rather than relying on\ninheritance, and keep the `a:rPr` in the first child position\
    \ of `a:r`.\n"
- id: pptx-cell-02
  answer: "PowerPoint lays pptx tables out with **fixed column widths** (`a:gridCol/@w`\nand the `a:tcW`\
    \ values): a column does not widen because its content got\nlonger. The table is a grid, not a flex\
    \ layout.\n\nSo the longer name goes through one of these paths, in practice in this\norder:\n\n*\
    \ **The row grows taller.** Text wraps at the column's usable width\n  (minus `a:tcPr` `marL`/`marR`),\
    \ so more text means more wrapped lines means\n  a taller `a:tr`. Word wraps at spaces, so a long\
    \ unbroken token can\n  overflow the cell instead of wrapping.\n* **The glyphs spill outside the cell.**\
    \ If the text has no break\n  opportunities, PowerPoint may let it render over the neighbouring cell\
    \ or\n  outside the table's outline. In a themed table the spill can be hidden by\n  the adjacent\
    \ cell's fill, which is the classic \"half my text disappeared\"\n  symptom.\n* **The row does not\
    \ auto-grow at all** in some cases (a fixed `a:tr/@h`\n  with text that cannot fit), so the text is\
    \ clipped and the neighbouring\n  rows' text is not pushed down.\n* **Text is scaled down** if the\
    \ table/cell has autofit\n  (`<a:normAutofit fontScale=\"...\" lnSpcReduction=\"...\"/>` inside the\n\
    \  `a:bodyPr` of the cell's `a:txBody`).\n* The `p:graphicFrame` `a:ext/@cy` still holds the pre-wrap\
    \ value, so the\n  stored geometry drifts from what is drawn (see `pptx-rowdel-02`).\n\n**How to control\
    \ it — decide up front which layout you want:**\n\n* *Absorb the growth (narrow other columns to pay\
    \ for it).* Set the new\n  cell's own text shorter, or re-balance: write the widths to `a:gridCol/@w`\n\
    \  and to every `a:tc/a:tcPr/a:tcW` in the column, keeping the row total equal\n  to the current `sum(gridCol/@w)`\
    \ so the table does not change width.\n  python-pptx: `table.columns[i].width = Emu(...)` plus\n \
    \ `cell.width` per cell; the heights follow from the re-wrap.\n* *Reflow at a fixed table width.*\
    \ After the edit, re-read\n  `sum(tr.get('h') for tr in table._tbl.tr_lst)` and set\n  `frame.height`;\
    \ or set the affected `row.height` explicitly (a `row.height`\n  assignment is what makes python-pptx\
    \ update the frame).\n* *Widen the table.* Add the extra width by taking it from columns that have\n\
    \  slack, and verify the right edge still fits:\n  `assert sum(gridCol widths) + frame.left <= prs.slide_width`.\n\
    * *Avoid the wrap.* Shorten the label (\"Gadget D\" -> \"Gadget D2 Pro Kit\"),\n  insert a manual\
    \ `a:br`, or reduce the font size for that cell.\n* *Stop the spill.* `cell.text_frame.word_wrap =\
    \ True` (writes\n  `wrap=\"square\"` on `a:bodyPr`) and set the cell's\n  `a:bodyPr` to `<a:normAutofit/>`\
    \ or give the cell explicit\n  `a:tcPr` margins; if you need to shrink, set `fontScale`.\n* If the\
    \ text must stay on one line regardless, that means widening the\n  column (or a smaller font) — the\
    \ table will not do it for you.\n"
- id: pptx-cell-03
  answer: "Editing the Qty text alone leaves the document internally inconsistent. In a\nreal invoice-style\
    \ slide you must update, at minimum:\n\n1. **The line total (Amount) on that row** — the product of\
    \ Qty and Unit\n   Price. This is usually a *static typed number*, not a formula: `a:tbl` has\n  \
    \ no formula support, so nothing recalculates. If it is wrong, the table is\n   simply wrong.\n2.\
    \ **The invoice/table total** (the Total row, and any subtotal, tax, and\n   grand-total figures)\
    \ — recompute from the line items and rewrite those\n   `a:t` values, in the same currency format\
    \ (thousands separators, 2 dp,\n   currency symbol, and whatever negative/parenthesis convention the\
    \ deck\n   uses).\n3. **The Total row's `w:t` sum** in the sheet: assert the sum of the Amount\n \
    \  column equals the printed total, and assert it *before* editing, so a\n   wrong input table is\
    \ caught rather than propagated.\n4. **Any quantity-driven narrative text**: \"for 5 units\", a quantity\
    \ in a\n   heading, a shipping/consignment note, a summary line above or below the\n   table, or the\
    \ number in a chart or text box that mirrors the table.\n5. **Per-row highlights/notes keyed to quantity**\
    \ — a \"bulk discount applied\"\n   annotation, or conditional formatting such as a quantity-threshold\
    \ fill or\n   a red \"over 10\" marker that the author applied by hand.\n\nMechanical steps: change\
    \ the Qty via the run-level edit from `pptx-cell-01`\n(so the number keeps its font/size/alignment),\
    \ keep the value's **format**\nconsistent with the other cells (right alignment, same decimals), and\
    \ if the\ndigit count changed, check that the cell does not now need to wrap or that\nthe column is\
    \ wide enough.\n\nIf the deck has a documented calculation chain (an `XLSX`-linked chart, an\nembedded\
    \ workbook, or an OLE object), that has to be refreshed too — but for\na plain `a:tbl` the numbers\
    \ are just text and you own them.\n"
- id: pptx-relayout-01
  answer: "Reuse a **body row you already own** rather than adding a blank one, because\n`table.add_row()`\
    \ appends at the bottom with default formatting.\n\n```python\nfrom copy import deepcopy\nfrom pptx.oxml.ns\
    \ import qn\n\n# 1) Locate the donor and the anchor rows by exact stripped text.\ndef row_index(table,\
    \ col, text):\n    hits = [i for i, r in enumerate(table.rows)\n            if r.cells[col].text_frame.text.strip()\
    \ == text]\n    assert len(hits) == 1, (text, hits)\n    return hits[0]\n\nservice_i = row_index(table,\
    \ 0, \"Service G\")\ntotal_i   = row_index(table, 0, \"Total\")\n\n# 2) Deep-copy the body row (its\
    \ runs, a:tcPr fills, borders, alignment).\ndonor_tr = table.rows[service_i]._tr\nnew_tr = deepcopy(donor_tr)\n\
    \n# 3) Rewrite the text of each origin cell, run by run, so formatting survives.\ndef set_cell_text(tc,\
    \ value):\n    # first paragraph, first run keeps its a:rPr; drop the surplus runs\n    for p in list(tc.findall(qn('a:p')))[1:]:\n\
    \        tc.remove(p)\n    p = tc.find(qn('a:p'))\n    runs = p.findall(qn('a:r'))\n    if not runs:\n\
    \        raise RuntimeError(\"donor cell has no run to reuse\")\n    runs[0].find(qn('a:t')).text\
    \ = value\n    for extra in runs[1:]:\n        p.remove(extra)\n\ncells = new_tr.findall(qn('a:tc'))\n\
    for tc, value in zip(cells, [\"Consulting\", \"1\", \"1,200.00\", \"1,200.00\"]):\n    set_cell_text(tc,\
    \ value)\n\n# 4) Insert it between the anchor and the Total row.\ntable.rows[total_i]._tr.addprevious(new_tr)\n\
    \n# 5) Parity checks before saving (a mismatch here = \"PowerPoint needs repair\").\ngrid = table._tbl.find(qn('a:tblGrid')).findall(qn('a:gridCol'))\n\
    for tr in table._tbl.tr_lst:\n    assert len(tr.findall(qn('a:tc'))) == len(grid), tr.get('h')\n\n\
    # 6) Keep the frame in sync: the new row needs room, take it from a spare\n#    row (or shrink the\
    \ table's footprint on the slide).\nfrom pptx.util import Emu\nspare = 0            # height you can\
    \ free up\nif spare:\n    donor_tr.set('h', str(int(donor_tr.get('h')) - spare))\nnew_tr.set('h',\
    \ '370840')                       # ~0.4 in, matches body rows\nframe.height = sum(r.height for r\
    \ in frame.table.rows)\nassert frame.top + frame.height <= prs.slide_height\n```\n\nPoints that matter:\n\
    \n* Anchor with **`addprevious` / `addnext`** on the lxml element, not on\n  python-pptx objects (there\
    \ is no `insert_row_before`).\n* Copy the **body** row, not the Total row, so you inherit banding\
    \ fill and\n  body fonts rather than bold totals formatting; and remember that if the\n  banding is\
    \ style-driven the inserted row's parity comes out right\n  automatically, while if banding is baked\
    \ into `a:tcPr` you must copy a\n  donor of the right parity (or set the `a:solidFill` yourself).\n\
    * **Vertical merges:** if the new row lands inside a `rowSpan` block, the\n  block is now short by\
    \ a row — add a `vMerge=\"1\"` `a:tc` to the new row and\n  bump the origin's `rowSpan`.\n* **`a:tc`\
    \ order:** keep `a:txBody` before `a:tcPr` (the deepcopy preserves\n  it, so do not reorder).\n* `a:t`\
    \ with leading/trailing spaces needs `xml:space=\"preserve\"`.\n* Finally, on the **saved** file,\
    \ assert the old text is gone / the new text\n  is present in `ppt/slides/slideN.xml`, `p:cNvPr` ids\
    \ are unique, and\n  `unzip -t` passes.\n"
- id: pptx-relayout-02
  answer: "Adding a column in a table that already spans nearly the slide is a\nthree-part job: **new\
    \ gridCol, a new `a:tc` in every row (merges\nincluded), and a re-balancing of widths so the total\
    \ does not exceed the\nslide.**\n\n1. **Insert the `a:gridCol` in the right position.** `a:tblGrid`\
    \ children are\n   ordered columns, so the new one goes at the index of the new column:\n\n   ```python\n\
    \   from pptx.oxml.ns import qn\n   from lxml import etree\n\n   tbl = frame.table._tbl\n   grid =\
    \ tbl.find(qn('a:tblGrid'))\n   cols = grid.findall(qn('a:gridCol'))\n   new_col = deepcopy(cols[0])\n\
    \   cols[0].addnext(new_col)               # after the Item column\n   ```\n\n2. **Add one `a:tc`\
    \ to every row**, at the same index. Every `a:tr` must end\n   with exactly one `a:tc` per `a:gridCol`.\
    \ The trap: a row whose\n   *previous* cell is a horizontally merged origin spanning the new column's\n\
    \   position.\n\n   * Plain row: deep-copy any body `a:tc`, set its `w:tcW/@w` to the new\n     width,\
    \ and rewrite/clear its text.\n   * Row where a merge crosses the insertion point (e.g. \"Total\"\
    \ with\n     `gridSpan=\"2\"` over Item..Unit Price): **raise the origin's `gridSpan`\n     by 1 and\
    \ insert an `a:tc` with `hMerge=\"1\"` right after it.** That\n     covered cell is a real element,\
    \ not a comment:\n\n     ```python\n     tc = tr.findall(qn('a:tc'))[idx]\n     if tc.get('gridSpan'):\n\
    \         tc.set('gridSpan', str(int(tc.get('gridSpan')) + 1))\n         covered = deepcopy(tc)\n\
    \         covered.set('hMerge', '1')       # origin must NOT have hMerge=\"1\"\n         covered.set('gridSpan',\
    \ None)    # or remove the attribute\n         # clear its text so nothing leaks into the hidden cell\n\
    \         for p in covered.findall(qn('a:p')):\n             for r in p.findall(qn('a:r')):\n    \
    \             p.remove(r)\n         tc.addnext(covered)\n     else:\n         plain = deepcopy(template_tc)\n\
    \         tr.findall(qn('a:tc'))[idx].addnext(plain)\n     ```\n\n     (For a vertically merged origin\
    \ — `rowSpan` — add the new cell as a\n     `vMerge`-continuation in the covered rows as well, so\
    \ the block stays\n     rectangular.)\n   * Assert the invariant before saving:\n\n     ```python\n\
    \     ncol = len(grid.findall(qn('a:gridCol')))\n     for tr in tbl.tr_lst:\n         assert len(tr.findall(qn('a:tc')))\
    \ == ncol, tr.get('h')\n         for tc in tr.findall(qn('a:tc')):\n             gs = int(tc.get('gridSpan')\
    \ or 1)\n             assert 1 <= gs and not (tc.get('hMerge') and tc.get('gridSpan'))\n     ```\n\
    \n3. **Keep the table on the slide — take the extra width from the existing\n   columns.** Do not\
    \ add width: the table is already nearly full-width, and\n   `a:gridCol/@w` is not advisory. Rescale\
    \ so the total is unchanged (or at\n   least still fits):\n\n   ```python\n   frame_w = sum(int(c.get('w'))\
    \ for c in grid.findall(qn('a:gridCol')))\n   assert frame.left + frame_w <= prs.slide_width\n   new_w\
    \ = 900000                                   # ~0.98 in\n   others = [c for c in grid.findall(qn('a:gridCol'))\
    \ if c is not new_col]\n   donor = new_w // (len(others))\n   for c in others:\n       c.set('w',\
    \ str(int(c.get('w')) - donor))\n   ```\n\n   Then push the same numbers into the cells: for each\
    \ row, walk the\n   `a:tc` list tracking the running grid position, sum the widths of a\n   `gridSpan`\
    \ group, and write the result into that origin's\n   `a:tcPr/a:tcW/@w` (with `a:tcW/@type=\"dxa\"\
    `); covered cells get the width\n   of the single column they cover. python-pptx shortcut:\n   `table.columns[i].width\
    \ = Emu(...)` and `cell.width` per cell, which does\n   both `a:gridCol` and `a:tcW`.\n\n4. **Resync\
    \ the frame and re-verify.**\n\n   ```python\n   frame.height = sum(r.height for r in frame.table.rows)\n\
    \   # or grow it explicitly if the new column is narrower -> more wrapping\n   assert frame.left +\
    \ sum(c.width for c in frame.table.columns) \\\n          <= prs.slide_width\n   ```\n\n   If the\
    \ columns cannot shrink far enough (the description is the column\n   you cannot compress), the alternatives\
    \ are: reduce the cell text, drop\n   `marL`/`marR` in `a:tcPr`, shrink the font for that column,\
    \ narrow\n   `frame.left` if there is slack on the left, or reduce the table's font\n   size overall\
    \ so every column's minimum content width fits.\n\n5. **Final gates on the saved package:** `unzip\
    \ -t` clean; open as a zip and\n   assert the header text set matches the expected column list; confirm\n\
    \   `len(tbl.tr_lst)` unchanged; confirm no duplicate `p:cNvPr` ids (relevant\n   if you also touched\
    \ other shapes); and open in PowerPoint/PowerPoint\n   Online once, because the \"needs repair\" prompt\
    \ is the one failure mode\n   python-pptx will not raise.\n"
- id: pptx-tblins-01
  answer: "python-pptx has no \"auto-layout\" mode, so you compute the geometry yourself.\n\n1. Measure\
    \ the free space: iterate `slide.shapes` and, for each shape, get\n   `shape.left, shape.top, shape.width,\
    \ shape.height` and compute\n   `left + width` / `top + height`. (Recurse into `GroupShape.shapes`\
    \ if any\n   groups exist — a group's own `.left/.width` is its bounding box, but its\n   children\
    \ are what actually occupy space.)\n\n2. Pick a candidate rectangle, e.g. a full-width band that starts\
    \ just below\n   the lowest shape and above the footer, or an explicit margin box:\n\n       from\
    \ pptx.util import Inches, Pt\n       W, H = prs.slide_width, prs.slide_height\n       margin = Inches(0.6)\n\
    \       left, top = margin, Inches(4.2)\n       width, height = W - 2 * margin, Inches(1.6)   # 2\
    \ rows, 0.8\" each\n\n3. Guard against overlap explicitly rather than trusting your arithmetic:\n\
    \   build a list of occupied rects and assert that\n   `rect_left >= occ.right` or `rect_right <=\
    \ occ.left` or\n   `rect_top >= occ.bottom` or `rect_bottom <= occ.top` for every occupied\n   rect.\
    \ Iterate candidate `top` values and keep the first that passes.\n\n4. Create the table:\n\n     \
    \  graphic_frame = slide.shapes.add_table(3, 2, left, top, width, height)\n       table = graphic_frame.table\n\
    \n5. The frame height is only as tall as the rows: `add_table` divides `height`\n   by the row count,\
    \ so check `graphic_frame.height` afterwards and set\n   `graphic_frame.height` explicitly if you\
    \ changed `row.height` values.\n\nTwo extra notes: don't reuse a generic content placeholder's position\n\
    blindly (see pptx-tblins-03), and remember that python-pptx does not verify\nanything — the overlap\
    \ check is yours to write.\n"
- id: pptx-tblins-02
  answer: "`add_table` applies the default table style from the presentation's theme\n(`tableStyleId`\
    \ on `a:tblPr` in the table's XML, which python-pptx sets to\nthe built-in \"Medium Style 2 Accent\
    \ 1\" GUID\n`{5C22544A-7EE6-4342-B048-85BDC9FD1C3A}`), so on a deck with a dark or\ndifferently-themed\
    \ look it renders blue.\n\nA few options, in order of how well they keep the deck consistent:\n\n\
    1. Copy the styling from an existing table. Cheapest robust trick:\n   `deepcopy` the `a:tblPr` element\
    \ (and optionally `a:tblGrid`) from a\n   table that already looks right onto the new one, replacing\
    \ the new\n   table's own `tblPr`; then set `firstRow`/`bandRow` flags the same way.\n   Because this\
    \ is a `deepcopy`, remember to reassign a fresh unique\n   `p:cNvPr` id if you also copied the graphic\
    \ frame (duplicate ids are a\n   repair trigger).\n\n2. Point at a named style: set\n   `tbl._tbl.tblPr.find(qn('a:tableStyleId')).text`\
    \ to the `{GUID}` of a\n   built-in style, or remove the `a:tableStyleId` element entirely so the\n\
    \   table inherits whatever the theme's `tableStyles.xml` default is — that\n   last one often matches\
    \ an existing deck.\n\n3. Format explicitly: set `table.first_row = True/False`,\n   `table.horz_banding\
    \ = True/False`, then style the cells yourself —\n   `cell.fill.solid()` / `cell.fill.background()`,\
    \ `cell.fill.fore_color.rgb\n   = RGBColor(...)`, per-cell `cell.text_frame.paragraphs[0].font.name`,\n\
    \   `.size`, `.bold`, `.color.rgb`, and column widths via\n   `table.columns[i].width`. This is the\
    \ only option that is fully\n   independent of theme/style-guid availability.\n\n4. If the deck genuinely\
    \ has no table styles and you need byte-level control,\n   drop to lxml and add/replace the `a:tableStyleId`\
    \ (or write `a:tblPr`\n   `firstRow`/`bandRow` attributes) directly.\n"
- id: pptx-tblins-03
  answer: "The two cases are different and it matters which one you hit, because only\none of them has\
    \ an `insert_table()` method.\n\nCase A — a real table placeholder. python-pptx's `TablePlaceholder`\
    \ (a\n`<p:ph type=\"tbl\"/>`) supports `insert_table(rows, cols)`. It returns a\n`PlaceholderGraphicFrame`\
    \ that is *still* a placeholder, sized to the\nplaceholder's box, with the default style applied.\
    \ The layout's placeholder\ngeometry is respected:\n\n    ph = slide.placeholders[1]           # a\
    \ TablePlaceholder\n    gf = ph.insert_table(4, 3)\n    table = gf.table\n\nCase B — the usual \"\
    Click to add content\" content placeholder\n(`<p:ph idx=\"1\"/>`, i.e. a `SlidePlaceholder`). It has\
    \ no `insert_table`, so\ncalling it raises `AttributeError`. Add a normal table at the placeholder's\n\
    geometry and delete the placeholder element so no empty prompt is left\nbehind:\n\n    ph = slide.placeholders[1]\n\
    \    gf = slide.shapes.add_table(4, 3, ph.left, ph.top, ph.width, ph.height)\n    table = gf.table\n\
    \    ph._element.getparent().remove(ph._element)     # drop the prompt\n\nIn both cases, identify\
    \ the placeholder by `ph.placeholder_format.type` /\n`.idx` rather than by index order, and remember\
    \ that a `SlidePlaceholder`\nwhose type is `OBJECT`/`BODY` is not a table placeholder even if it visually\n\
    looks like one — check for `type == PP_PLACEHOLDER.TABLE` (or `idx` matching\nthe layout's `tbl` placeholder)\
    \ before reaching for `insert_table`.\n"
- id: pptx-imgrep-01
  answer: "The clean way is to record the geometry, drop the old `p:pic`, and add a new\npicture with\
    \ the same `left/top/width/height`; then restore the original\nposition in the shape tree so the stacking\
    \ order is unchanged.\n\n    from pptx.util import Emu\n\n    def replace_picture(pic, new_path):\n\
    \        slide = pic.part   # actually: slide = pic._parent\n        left, top, width, height = pic.left,\
    \ pic.top, pic.width, pic.height\n        slide_el = pic._element\n\n        # remember where it sat\
    \ in z-order\n        index = list(slide.shapes._spTree).index(slide_el)\n\n        rId = pic._element.blip_rId\n\
    \        slide.shapes._spTree.remove(slide_el)\n        slide.part.drop_rel(rId)      # see pptx-imgrep-03\
    \ before doing this\n\n        new_pic = slide.shapes.add_picture(new_path, left, top, width, height)\n\
    \        # add_picture appended it last; move it back to the old index\n        new_el = new_pic._element\n\
    \        spTree = slide.shapes._spTree\n        spTree.remove(new_el)\n        spTree.insert(index,\
    \ new_el)\n        return new_pic\n\nNotes:\n\n* `slide.shapes.add_picture(...)` appends to the end\
    \ of the spTree, i.e. on\n  top. Re-inserting at the saved index preserves the original z-order. A\n\
    \  common equivalent is to temporarily move the new element with\n  `spTree.remove(new_el); spTree.insert(index,\
    \ new_el)`.\n* Passing both `width` and `height` distorts the new image. To keep the\n  original box\
    \ exactly, pass both. To keep the new image's aspect ratio\n  instead, pass only `width` (or only\
    \ `height`) and recompute the other\n  dimension yourself.\n* Cropping (`pic.crop_left` etc.), the\
    \ rotation, and the picture's `name` /\n  alt-text are *not* carried over. Copy them explicitly:\n\
    \  `new_pic.crop_left = pic.crop_left` (and right/top/bottom), plus\n  `new_pic.rotation = pic.rotation`,\
    \ `new_pic.name = pic.name`.\n* A cheaper \"same-shape swap\" alternative exists: overwrite the image\n\
    \  *part*'s blob in the package. It keeps geometry, crop, z-order and the\n  existing `rId` — but\
    \ it is global to the package, which is exactly the\n  trap in pptx-imgrep-02.\n* Reassign a fresh\
    \ unique `p:cNvPr` id only if you `deepcopy` a shape\n  element; here you are creating the picture\
    \ normally, so the id is already\n  unique.\n"
- id: pptx-imgrep-02
  answer: 'Because the image part is a *package-level* part, not a per-slide one. Every

    picture that references the same media part shares one

    `image1.png` inside `ppt/media/` and one relationship (rId) pointing at it.

    Overwriting the bytes of that part changes what every reference to it

    renders, on every slide — python-pptx gives you no per-use independence.


    Related trap: python-pptx de-duplicates by SHA1, so adding the same file

    twice normally reuses the *same* image part, and adding it once on slide 1

    and once on slide 7 with the same bytes will not even create a second part.

    That is why two "different" logos often collapse onto one part.


    How to check: after the overwrite, list `zipfile.ZipFile(path).namelist()` for

    `ppt/media/*`, hash each entry, and look at the relationships

    (`ppt/slides/_rels/slideN.xml.rels`) to see how many slides point at the

    part you modified.


    The fix: don''t mutate the shared part. Insert a genuinely new picture on

    slide 1 only (the swap described in pptx-imgrep-01) and then drop the old

    relationship/part *only after* confirming nothing else references it (see

    pptx-imgrep-03 and pptx-verify-03). If you must use the byte-overwrite

    trick, first copy the original bytes to a new part (e.g. via

    `slide.part.get_or_add_image_part(...)`) so slide 7 keeps the old part.

    '
- id: pptx-imgrep-03
  answer: "Because `Part.drop_rel(rId)`'s reference count is computed from the wrong\nXPath.\n\nIn `python-pptx`\
    \ (opc/package.py), `_rel_ref_count` counts only `//@r:id`\nattributes. Pictures, however, reference\
    \ their image through\n`a:blip/@r:embed` (and hyperlinks/media links use `@r:link`). `r:embed` is\n\
    not `r:id`, so an image relationship is counted as having **zero**\nreferences no matter how many\
    \ pictures use it. The check is\n`_rel_ref_count(rId) < 2` → 0 < 2 → it always drops, even while a\
    \ second\npicture on the same slide is still rendering from that relationship. The\nsurviving picture\
    \ keeps its `r:embed=\"rIdX\"`, the rel is gone, and\nPowerPoint reports a repair/error (or a missing\
    \ image).\n\nTwo further wrinkles that make this worse:\n\n* python-pptx reuses the existing rId when\
    \ you add the same image file twice\n  to the same slide, so \"the same image\" is literally the same\
    \ relationship —\n  there is no second rel to protect it.\n* Multiple slides can share the same image\
    \ part through the same or\n  different rels; the count you compute on one slide tells you nothing\
    \ about\n  the others.\n\nSafe procedure:\n\n    pic._element.blip_rId            # the rId actually\
    \ in use\n    slide.shapes._spTree.remove(pic._element)   # remove the p:pic first\n    if not part._element.xpath(\n\
    \            '//@r:embed[.=\"%s\"] | //@r:link[.=\"%s\"]' % (rId, rId)):\n        part.drop_rel(rId)\
    \           # only now\n\nAnd when the *same part* is referenced from other slides, also check the\n\
    whole package before relying on `prs.save()` to garbage-collect the media\nentry: hash every `ppt/media/*`\
    \ entry of the saved file and confirm none\nmatches the removed image (see pptx-verify-03).\n"
- id: pptx-imgins-01
  answer: "Two steps: work out the width in EMU and pass only the width so\npython-pptx computes the height\
    \ from the image's own aspect ratio; then\nchoose a bottom-right anchor that is provably free.\n\n\
    \    from PIL import Image            # or read the PNG/JPEG header yourself\n    from pptx.util import\
    \ Inches, Emu\n\n    target_w = Inches(2)                       # 2\" wide\n    with Image.open(\"\
    signature.png\") as im:\n        ratio = im.height / im.width           # h / w\n    target_h = Emu(int(round(target_w\
    \ * ratio)))\n\n    sw, sh = prs.slide_width, prs.slide_height\n    margin = Inches(0.3)\n    left\
    \ = sw - target_w - margin              # flush to the right\n    top  = sh - target_h - margin  \
    \             # flush to the bottom\n\n    # optional: verify nothing is already there\n    right,\
    \ bottom = left + target_w, top + target_h\n    for sh_ in slide.shapes:\n        if not (left   >=\
    \ sh_.left + sh_.width or right  <= sh_.left or\n                top    >= sh_.top  + sh_.height or\
    \ bottom <= sh_.top):\n            raise RuntimeError(\"would overlap %s\" % sh_.name)\n\n    pic\
    \ = slide.shapes.add_picture(\"signature.png\", left, top, width=target_w)\n\nPoints to get right:\n\
    \n* Pass `width=` alone (or `height=` alone) so python-pptx preserves aspect\n  ratio. Passing both\
    \ `width` and `height` forces the exact box and distorts\n  the image.\n* `left`/`top` are from the\
    \ top-left of the slide, so \"bottom-right\" is\n  `slide_width - width - margin` / `slide_height\
    \ - height - margin`. The\n  picture must also fit inside the slide: assert\n  `left >= 0 and top\
    \ >= 0 and left + width <= sw and top + height <= sh`.\n* `add_picture` appends to the end of the\
    \ shape tree, so the signature will\n  sit on top of everything. If it must be *behind* something,\
    \ move the\n  `p:pic` element to an earlier index in `slide.shapes._spTree`.\n* Setting `pic.left`/`pic.top`\
    \ after creation works too — just re-read\n  `.width`/`.height` if you adjust the size, and note that\
    \ writing\n  `.left/.top` does not change `.width/.height`.\n* python-pptx does not validate overlap\
    \ or bounds; the check above is the\n  only thing standing between you and an overlapping signature.\n"
- id: pptx-imgins-02
  answer: "The placeholder crops rather than scales. `PicturePlaceholder.insert_picture`\n(and the plain\
    \ `PicturePlaceholder` behaviour) uses \"fill\" semantics: the\nimage is inserted at the placeholder's\
    \ size *and* aspect ratio, and the\noverflow is hidden with a negative crop — the picture shape is\
    \ bigger than\nthe frame, and `crop_left`/`crop_right`/`crop_top`/`crop_bottom` are set so\nthe visible\
    \ window is the placeholder rectangle. python-pptx chooses a\ncentre crop, so a portrait (tall) photo\
    \ placed in a landscape placeholder\nloses the top and bottom; a landscape photo in a portrait placeholder\
    \ loses\nthe left and right.\n\nOptions:\n\n* Accept it if a centre crop is fine for the design (this\
    \ is what PowerPoint\n  itself does for picture placeholders).\n\n* Avoid the crop: don't use the\
    \ placeholder's own insert. Add a normal\n  picture sized to fit and remove the placeholder element:\n\
    \n      ph = slide.placeholders[idx]\n      pic = slide.shapes.add_picture(path, ph.left, ph.top,\n\
    \                                     width=ph.width, height=ph.height)\n      ph._element.getparent().remove(ph._element)\n\
    \n  (Passing both width and height distorts; scale to *contain* manually if\n  you care about the\
    \ ratio.)\n\n* Zero the crop after inserting so the whole image shows and the picture\n  extends past\
    \ the frame — usually not what you want visually, but it is\n  the escape hatch:\n\n      pic.crop_left\
    \ = pic.crop_right = pic.crop_top = pic.crop_bottom = 0\n\n* If you need a specific crop, set the\
    \ crop_* values yourself after\n  `insert_picture` (read them first, then adjust).\n"
- id: pptx-legacy-01
  answer: "No. python-pptx only opens the OOXML formats — `.pptx` and, for templates,\n`.potx`/`.ppsx`/`.thmx`/`.ppsm`.\
    \ The legacy binary formats are\n`.ppt`, `.doc`, `.xls` and friends; `prs = Presentation(\"deck.ppt\"\
    )` raises\n`PackageNotFoundError`/an \"not a zip file\"-style error because a .ppt is an\nOLE2 compound\
    \ file, not a zip package.\n\nWorkflow:\n\n1. Convert to `.pptx` with LibreOffice headless, in a scratch/output\n\
    \   directory (never over the user's original):\n\n       soffice --headless --convert-to pptx:\"\
    Impress MS PowerPoint 2007 XML\" \\\n               --outdir ./converted deck.ppt\n\n   On macOS the\
    \ binary may be at\n   `/Applications/LibreOffice.app/Contents/MacOS/soffice`. If soffice is\n   not\
    \ installed, say so — that is a hard blocker, and the other options\n   are Microsoft PowerPoint itself\
    \ (AppleScript / COM automation on\n   Windows), or a third-party converter.\n\n2. Open the *converted*\
    \ file with python-pptx and make the table edit there.\n\n3. Tell the user the original `.ppt` is\
    \ untouched and that the deliverable\n   is a `.pptx` (or, if they need `.ppt` back, re-convert with\n\
    \   `--convert-to ppt:\"Impress MS PowerPoint 97\"`, knowing that the round trip\n   loses some fidelity).\n\
    \nCaveats worth stating: LibreOffice's filter choice matters (there is a\n   `Impress MS PowerPoint\
    \ 2007 XML` pptx filter and an OOXML strict\nvariant; the default is usually fine), SmartArt, charts,\
    \ embedded media,\nsome fonts, VBA and complex tables may not survive the conversion\nperfectly, and\
    \ the converted file should be diffed against the original\nslide count/shape inventory before you\
    \ trust it.\n"
- id: pptx-legacy-02
  answer: "Before editing the converted `.pptx`:\n\n* Confirm the conversion actually happened and produced\
    \ a real package:\n  the file is a zip, `ppt/presentation.xml` exists, and\n  `Presentation(\"out.pptx\"\
    )` opens without error.\n* Inventory: slide count, and per slide the number/kind of shapes and the\n\
    \  table dimensions and cell texts — so you can tell whether anything was\n  dropped or mangled by\
    \ the converter.\n* Check the known-lossy areas: SmartArt (often rasterised or flattened),\n  charts\
    \ (may become images or lose the embedded workbook), embedded\n  objects/video/audio, VBA, custom\
    \ XML parts, macros, and fonts (text may\n  reflow if a font is missing).\n* Check the table you intend\
    \ to edit: the number of rows/columns, merged\n  cells, and the exact header text — this is the same\
    \ \"locate by content\"\n  discipline you would use on a native file.\n* Note anything that looks\
    \ already broken; if the *original* `.ppt` is what\n  the user will open, its fidelity is not yours\
    \ to fix.\n\nAfter editing, before handing it back:\n\n* Re-verify on the saved file (pptx-verify-01):\
    \ old text gone from every\n  `ppt/slides/slideN.xml`, media hashes, unique `p:cNvPr` ids, zip integrity\n\
    \  (`zipfile.testzip()` / `unzip -t`).\n* Round-trip sanity: re-open with python-pptx, and if available\
    \ render with\n  LibreOffice to PDF (`soffice --headless --convert-to pdf`) or extract text\n  with\
    \ pandoc to confirm nothing shifted.\n* Optionally convert the edited `.pptx` back to `.ppt` if the\
    \ user needs the\n  legacy format, and check that file too.\n\nWhat you must tell the user:\n\n* The\
    \ original `.ppt` was not modified; the deliverable is a new `.pptx`\n  (or a new `.ppt` if you converted\
    \ back), and the old-format file is\n  retained.\n* The edit you made and, honestly, the conversion's\
    \ known limitations —\n  rendering of SmartArt/charts/embedded media, fonts, animation timing,\n \
    \ and any VBA/macro loss — may differ from what they see in the original.\n* Whether any content was\
    \ lost or altered during conversion; list specifics\n  rather than saying \"should be fine\".\n* That\
    \ they should open the result once in PowerPoint to confirm before it\n  is relied on, and that python-pptx\
    \ validates nothing, so \"it saved without\n  error\" is not evidence the file is sound.\n"
- id: pptx-verify-01
  answer: "The rule is: verify the *saved package on disk*, never the in-memory object\nmodel, because\
    \ python-pptx does no validation of any kind.\n\n    import zipfile, hashlib, collections\n    from\
    \ lxml import etree\n\n    def verify_pptx(path, must_be_absent=(), must_be_present=(),\n        \
    \            old_image_bytes=None):\n        z = zipfile.ZipFile(path)\n        assert z.testzip()\
    \ is None            # CRC check on every entry\n\n        names = z.namelist()\n        slides =\
    \ [n for n in names\n                  if re.match(r\"ppt/slides/slide\\d+\\.xml$\", n)]\n\n     \
    \   # 1. text/content assertions across ALL slides, not just the edited one\n        for n in slides:\n\
    \            xml = z.read(n).decode(\"utf8\", \"replace\")\n            for s in must_be_absent:\n\
    \                assert s not in xml, f\"{s!r} still present in {n}\"\n            for s in must_be_present:\n\
    \                assert s in xml, f\"{s!r} missing from {n}\"\n\n        # 2. unique p:cNvPr ids per\
    \ slide (deepcopy leaks duplicates)\n        for n in slides:\n            root = etree.fromstring(z.read(n))\n\
    \            ids = [e.get(\"id\") for e in root.iter(\n                \"{http://schemas.openxmlformats.org/presentationml/2006/main}cNvPr\"\
    )]\n            dupes = [i for i, c in collections.Counter(ids).items() if c > 1]\n            assert\
    \ not dupes, f\"duplicate cNvPr ids in {n}: {dupes}\"\n\n        # 3. every r:embed / r:link resolves\
    \ to a rel in that part's .rels\n        for n in slides:\n            root = etree.fromstring(z.read(n))\n\
    \            rels = etree.fromstring(z.read(n.replace(\"slides/\", \"slides/_rels/\")\n          \
    \                                + \".rels\"))\n            declared = {r.get(\"Id\") for r in rels}\n\
    \            for attr in (\"{.../officeDocument/2006/relationships}embed\",\n                    \
    \     \"{.../officeDocument/2006/relationships}link\",\n                         \"{.../officeDocument/2006/relationships}id\"\
    ):\n                for e in root.iter():\n                    v = e.get(attr)\n                 \
    \   if v:\n                        assert v in declared, f\"{n}: dangling {attr}={v}\"\n\n       \
    \ # 4. media redaction check\n        if old_image_bytes is not None:\n            h = hashlib.sha256(old_image_bytes).hexdigest()\n\
    \            for n in names:\n                if n.startswith(\"ppt/media/\"):\n                 \
    \   assert hashlib.sha256(z.read(n)).hexdigest() != h, \\\n                        f\"{n} still holds\
    \ the removed image\"\n\nAround that, the practical sequence:\n\n1. Work on a copy; never edit the\
    \ user's only file.\n2. Save with `prs.save(path)`, and confirm the file exists and is a valid zip\n\
    \   afterwards.\n3. Re-open the saved file with `Presentation(path)` — catches the loudest\n   structural\
    \ breakage.\n4. Run the assertions above.\n5. Table-specific: assert `len(tr.tc_lst) == len(gridCols)`\
    \ for every row\n   and that `gridSpan`/`rowSpan` sums still line up, and that\n   `graphic_frame.height`\
    \ equals `sum(r.height for r in table.rows)` (the\n   frame's `a:ext cy` does not follow raw XML edits).\n\
    6. Independent renderers where available: LibreOffice\n   (`soffice --headless --convert-to pdf`)\
    \ and `pandoc out.pptx -t markdown`\n   to diff the extracted text — the diff should show only the\
    \ intended\n   change.\n7. Keep the original next to the output so the run is reproducible, and say\n\
    \   what you verified versus what you could not verify (e.g. no renderer\n   available means no pixel-level\
    \ confirmation).\n"
- id: pptx-verify-02
  answer: "\"Needs to be repaired\" from PowerPoint almost always means the XML violates\nthe schema or\
    \ the package's relationship graph. The usual causes, in rough\norder of frequency:\n\n* Table cell/grid\
    \ mismatch. An `a:tr` must contain exactly one `a:tc` per\n  `a:gridCol` — including covered cells,\
    \ which still exist with\n  `hMerge=\"1\"`/`vMerge=\"1\"`. So a row with fewer `a:tc` than there are\n\
    \  `a:gridCol` is invalid. Deleting a row or inserting a column that lands\n  inside a merged cell\
    \ therefore requires: raise the origin's `gridSpan`\n  (and/or `rowSpan`) **and** add or remove the\
    \ covered `a:tc` elements so\n  the counts match again. This is the single most common cause.\n* Broken\
    \ merges: a `gridSpan`/`rowSpan` that runs past the last `a:gridCol`,\n  an `a:tc` with `hMerge=\"\
    1\"` whose origin does not carry a matching\n  `gridSpan`, `vMerge` continue cells with no origin\
    \ above them, or a\n  `rowSpan` on a covered (non-origin) cell.\n* An `a:tc` with no `a:txBody` child.\
    \ `a:tcPr` must be first, then\n  `a:txBody`; a cell whose only child is `a:txBody` is fine, a missing\
    \ one\n  is not.\n* Elements out of schema order, or invalid/unknown attributes. OOXML is a\n  strict\
    \ sequence: e.g. inside `a:tcPr` the order is roughly `a:lnL`,\n  `a:lnR`, `a:lnT`, `a:lnB`, `a:lnTlToBr`,\
    \ `a:lnBlToTr`, `a:cell3D`,\n  `a:noFill|a:solidFill|…`, `a:headers`, then `a:extLst`; inserting `a:shd`\n\
    \  *before* `a:tcW` (or after `a:headers`) trips the validator. Same class of\n  problem for `a:rPr`\
    \ child order and for `a:pPr`.\n* Duplicate `p:cNvPr` `id` values on a slide. This happens whenever\
    \ a shape\n  is `deepcopy`'d into a slide — the copy carries the original's id. Reassign\n  a fresh\
    \ unique id.\n* A dangling relationship: an `a:blip/@r:embed`, `@r:link` or `@r:id` that\n  points\
    \ at an rId not declared in the part's `.rels`, typically after a\n  premature `drop_rel()` (see pptx-imgrep-03).\
    \ A declared rel pointing at a\n  missing internal part is equally fatal.\n* Namespace damage: calling\
    \ `etree.cleanup_namespaces()` on the document\n  root can prune prefixes the root still declares\
    \ for `mc:Ignorable`\n  (`w14`, `wp14`, …), leaving `mc:Ignorable=\"w14 wp14 …\"` pointing at\n  undeclared\
    \ prefixes — Word/Excel flag that as corrupt. Don't do it.\n* Text that won't fit is *not* a cause\
    \ of repair prompts (that just clips or\n  overflows), and neither is a wrong `ext` height on the\
    \ graphic frame —\n  a stale frame height renders oddly but doesn't trigger repair.\n* Diagnostic\
    \ habit: `python-pptx` will save all of these silently, so\n  validate the saved zip yourself (see\
    \ pptx-verify-01) and, if you have the\n  Open XML SDK or `xmllint`/the ECMA schemas, run an XSD pass\
    \ over each\n  `ppt/slides/slideN.xml`.\n"
- id: pptx-verify-03
  answer: "Almost certainly not. Deleting the `p:pic` element only removes the shape\nfrom the slide.\
    \ The image part still lives in `ppt/media/`, the slide still\nhas a relationship pointing at it,\
    \ and the media entry is still written\ninto the zip — anyone who unzips the .pptx, or opens it in\
    \ an archive\nviewer or `strings`, can still recover the image. (If the file has already\nbeen distributed,\
    \ treat the image as disclosed and rotate/withdraw it.)\n\nTwo things must happen for the bytes to\
    \ actually leave the package:\n\n1. Drop the relationship — but only after removing the shape and\
    \ only if\n   nothing else references that rId:\n\n       rId = pic._element.blip_rId            #\
    \ read before deleting\n       slide.shapes._spTree.remove(pic._element)\n       if not slide.part._element.xpath(\n\
    \               '//@r:embed[.=\"%s\"] | //@r:link[.=\"%s\"]' % (rId, rId)):\n           slide.part.drop_rel(rId)\n\
    \n   Remember `drop_rel` counts only `//@r:id`, so an image rel always looks\n   unused to it — the\
    \ `xpath` above is your own check, and it must be run\n   across *all* slides/parts, because other\
    \ slides may reference the same\n   media part.\n\n2. Save and let the packaging drop the now-unreachable\
    \ part. `prs.save()`\n   walks the relationship graph, so once no rel points at\n   `ppt/media/imageN.png`,\
    \ that part is not written. (A stale part only\n   survives if something still references it, or if\
    \ you edited the zip by\n   hand without rebuilding it.)\n\nHow to check, on the saved file:\n\n \
    \   import zipfile, hashlib\n    z = zipfile.ZipFile(\"deck.pptx\")\n    media = [n for n in z.namelist()\
    \ if n.startswith(\"ppt/media/\")]\n    # hash the bytes you removed, then assert no entry matches\n\
    \    h = hashlib.sha256(old_png_bytes).hexdigest()\n    assert all(hashlib.sha256(z.read(n)).hexdigest()\
    \ != h for n in media)\n\nDo the check by **hash**, not by filename: the media part may be named\n\
    differently in the output than you expect. Additionally assert that no\n`ppt/slides/slideN.xml` (or\
    \ its `.rels`) still mentions the rId or the\nimage part, and that `unzip -t` / `zipfile.testzip()`\
    \ passes. A belt-and-\nbraces confirmation is to extract the text with `pandoc` and to grep the raw\n\
    zip for the image's bytes. And note there is no *guarantee* of absence —\nolder revisions of the file,\
    \ backups, thumbnails (`docProps/thumbnail.*`)\nor a previous copy on disk may still contain it.\n"
