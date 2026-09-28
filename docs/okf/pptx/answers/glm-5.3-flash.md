- id: pptx-model-01
  answer: "A table lives inside a <p:graphicFrame> in the slide's XML part\n(slideN.xml). The graphicFrame\
    \ carries <p:nvGraphicFramePr> (name/id),\n<p:xfrm> (position and size), and an <a:graphic><a:graphicData\n\
    uri=\"http://schemas.openxmlformats.org/drawingml/2006/table\"> wrapper\nholding the <a:tbl> element.\
    \ Inside <a:tbl>:\n  - <a:tblPr>: table properties (firstRow, bandRow flags, and usually an\n    <a:tableStyleId>\
    \ GUID pointing into tableStyles.xml).\n  - <a:tblGrid>: one <a:gridCol w=\"...\"/> per column (width\
    \ in EMUs).\n  - one <a:tr h=\"...\"/> per row (height in EMUs).\n  - inside each <a:tr>: one <a:tc>\
    \ per cell, containing <a:txBody> with\n    <a:bodyPr>, <a:lstStyle>, and <a:p> paragraphs; paragraphs\
    \ hold\n    <a:r> runs with <a:rPr> formatting and <a:t> text. Each cell also\n    has <a:tcPr> (fill,\
    \ anchor, margins, gridSpan/hMerge/rowSpan/vMerge).\nSo: columns = a:gridCol in a:tblGrid, rows =\
    \ a:tr, cells = a:tc,\ntext = a:t inside a:r inside a:p inside a:txBody.\n"
- id: pptx-model-02
  answer: 'DrawingML positions and sizes are in EMUs (English Metric Units):

    914400 EMU = 1 inch, 360000 EMU = 1 cm, 12700 EMU = 1 point.

    python-pptx exposes these as Emu and helpers Inches(), Cm(), Pt(),

    and shape .left/.top/.width/.height are Emu values.

    A default 16:9 deck is 13.333 in x 7.5 in, i.e. 12192000 x 6858000 EMU

    (set on <p:sldSz cx cy> in presentation.xml). The older default 4:3

    format is 10 in x 7.5 in = 9144000 x 6858000.

    '
- id: pptx-model-03
  answer: "The logo is inherited: it lives on the slide layout and/or the slide\nmaster, not on the slide\
    \ itself. Access it via\nslide.slide_layout.shapes or slide.slide_layout.slide_master.shapes\n(python-pptx\
    \ also exposes prs.slide_masters / each master's\nslide_layouts). Inheritance flows master -> layout\
    \ -> slide, so a shape\non the master or layout renders on every slide using it without being\npresent\
    \ in the slide's own shape tree.\nConsequences of editing it there:\n  - Blast radius is wide: changing\
    \ the master affects every layout and\n    every slide in the deck; changing a layout affects every\
    \ slide on\n    that layout.\n  - A slide that carries its own local copy of the logo will not be\n\
    \    updated by a master edit, so you can get inconsistent results.\n  - Master/layout changes can\
    \ disturb placeholder inheritance and get\n    carried into other decks if masters/themes are copied\
    \ around.\n"
- id: pptx-locate-01
  answer: "Tables in python-pptx appear as GraphicFrame shapes with has_table True:\n\n    from pptx import\
    \ Presentation\n    prs = Presentation(\"deck.pptx\")\n    slide = prs.slides[2]                 #\
    \ slide 3, 0-based index\n    tables = [sh.table for sh in slide.shapes if sh.has_table]\n    tbl\
    \ = tables[0]                       # or match by shape name/size\n\n    for r, row in enumerate(tbl.rows):\n\
    \        for c, cell in enumerate(row.cells):\n            if \"Gadget D\" in cell.text:\n       \
    \         print(\"found at row\", r, \"col\", c)\n\nNotes: tbl.rows[i].cells or tbl.cell(r, c) gives\
    \ cells; cell.text is the\nconcatenated paragraph text; with merged cells the text sits only in the\n\
    anchor cell. If several tables exist, disambiguate by number of rows,\nshape name, or presence of\
    \ the \"Item\"/\"Qty\" headers.\n"
- id: pptx-locate-02
  answer: "Horizontal merge: the anchor <a:tc> carries gridSpan=\"2\" (number of grid\ncolumns spanned),\
    \ and each covered column still has its own <a:tc> in the\nrow marked hMerge=\"1\" with (normally)\
    \ an empty body. Vertical merge:\nrowSpan=\"n\" on the anchor and vMerge=\"1\" on the continuation\
    \ cells in the\nrows below. Every row must have one <a:tc> per grid column (counting\nspans), which\
    \ is what PowerPoint validates.\nWatch out when editing/deleting around merges:\n  - Only the anchor\
    \ cell's text matters; text in hMerge/vMerge cells is\n    ignored, so don't \"fix\" data in the covered\
    \ cells.\n  - Deleting a cell or column shifts indices; anchors and continuation\n    flags can point\
    \ at the wrong cells, corrupting the table geometry.\n  - Never delete a row containing the anchor\
    \ of a multi-row span without\n    fixing the surviving anchor's rowSpan and removing its vMerge\n\
    \    continuations; PowerPoint may otherwise report the file as damaged\n    and repair/alter the\
    \ table.\n  - After edits, the sum of gridSpan (+1 per continuation cell) in each\n    row must equal\
    \ the number of <a:gridCol> elements.\n"
- id: pptx-rowdel-01
  answer: "There is no public row-deletion API in python-pptx: table.rows is a\nread-only collection (indexing,\
    \ len) and there is no rows.remove() or\ndelete_row(). You drop the underlying <a:tr> element with\
    \ lxml:\n\n    from pptx.oxml.ns import qn\n    tbl = table._tbl\n    trs = tbl.findall(qn('a:tr'))\n\
    \    tbl.remove(trs[2])            # or: tr.getparent().remove(tr)\n\nEquivalently `table.rows[i]._tr.getparent().remove(table.rows[i]._tr)`.\n\
    (python-pptx also has no add_row; rows are added by deepcopying an\nexisting <a:tr> and inserting\
    \ it.) Remember to keep the graphicFrame\nheight in sync afterwards.\n"
- id: pptx-rowdel-02
  answer: "The table's true rendered height is the sum of its <a:tr h> values, not\nthe graphicFrame's\
    \ p:xfrm ext cy; PowerPoint draws the table from the\nframe's origin and mostly tolerates a stale\
    \ cy. So strictly it often\nstill renders fine. But a stale bounding box is sloppy and can bite:\n\
    the selection/bounding box is wrong, automated layout that trusts\nshape.height gets wrong numbers,\
    \ and other renderers (LibreOffice,\nexport pipelines) may honour the frame box rather than recomputing\
    \ it.\nFix it by shrinking the frame by the deleted row's height:\n\n    from pptx.util import Emu\n\
    \    shape.height = shape.height - Emu(deleted_tr_h)   # p:xfrm ext cy\n\n(and similarly extend the\
    \ height when adding rows).\n"
- id: pptx-rowdel-03
  answer: "Because \"alternating\" shading can come from two different mechanisms:\n  1. Style-driven\
    \ banding: <a:tblPr bandRow=\"1\"/> plus a table style\n     (tableStyleId) shades odd/even rows by\
    \ row index. Delete a row and\n     the parity recomputes automatically, so alternation stays intact\
    \ —\n     a deck using this shows no defect.\n  2. Explicit fills: each row's cells carry literal\
    \ <a:tcPr><a:solidFill>\n     (often left by a generator or after manual formatting). Those fills\n\
    \     are static; deleting a middle row leaves two adjacent rows that\n     were both, say, white\
    \ or both shaded.\nA mixed deck (banding but with rows that have explicit fill overrides,\ne.g. a\
    \ highlighted row) shows partial defects. Fix by re-deriving fills\nfrom the row index after deletion,\
    \ or clearing overrides so bandRow\nstyling takes over.\n"
- id: pptx-rowdel-04
  answer: "It hides data instead of deleting it. The <a:tr> and all its runs remain\nin the file, so:\n\
    \  - The \"deleted\" line item is still extractable: PowerPoint search,\n    copy-paste, screen readers,\
    \ and any script (python-pptx, unzip +\n    grep of the slide XML, document converters) read it —\
    \ a privacy or\n    audit problem, and numbers may still be wrong in totals.\n  - The row still counts\
    \ in the table's row structure; code that walks\n    rows or computes derived values sees a phantom\
    \ row.\n  - A near-zero-height row can render as a hairline or cause layout\n    artifacts (min-heights,\
    \ borders, cell padding), and a white\n    rectangle is just another shape floating over content that\
    \ shows up\n    whenever rows move or the shape is selected/moved.\n  - Maintenance: the next editor\
    \ sees a table that claims to contain\n    data it shouldn't. Proper deletion is removing the <a:tr>\
    \ (and\n    adjusting the frame height).\n"
- id: pptx-cell-01
  answer: "The cell.text setter replaces the cell's whole text body: it clears the\nexisting paragraphs/runs\
    \ and writes a single paragraph with a single run\nthat has no <a:rPr>, so bold / 14 pt / white are\
    \ lost and the text falls\nback to theme defaults (usually dark, body-size).\nTo keep the formatting,\
    \ edit the existing runs instead of replacing the\ntext body — change only the <a:t> and leave <a:rPr>\
    \ untouched:\n\n    tf = cell.text_frame\n    p = tf.paragraphs[0]\n    if p.runs:\n        p.runs[0].text\
    \ = \"SKU\"\n        for r in p.runs[1:]:\n            r._r.getparent().remove(r._r)\n    else:\n\
    \        p.add_run().text = \"SKU\"\n\nAlternatively, capture the old run's rPr and reapply it, or\
    \ set\nrun.font.bold/size/color explicitly.\n"
- id: pptx-cell-02
  answer: "Column widths are fixed by <a:gridCol w>; editing text does not\nredistribute widths. The longer\
    \ name wraps onto more lines and the row\ngrows: PowerPoint treats the stored <a:tr h> as a minimum\
    \ and recomputes\nthe row height from the wrapped text (updating it once the table is\nedited/rendered),\
    \ so the table gets taller and can overflow the bottom\nof the slide or push subsequent content. A\
    \ single very long word may\nwrap mid-word rather than widen the column.\nControl it by:\n  - setting\
    \ column widths explicitly (table.columns[i].width = Emu(...)\n    writes gridCol w),\n  - reducing\
    \ font size or cell margins,\n  - wrap behaviour via the cell bodyPr (wrap=\"none\" disables wrapping,\n\
    \    causing overflow instead),\n  - and re-checking the graphicFrame height (sum of row heights)\
    \ so the\n    bounding box matches the new table height and stays on-slide.\n"
- id: pptx-cell-03
  answer: "PPTX tables contain static text — there are no formulas, so nothing\nrecalculates by itself.\
    \ You must update:\n  - that line item's line total (Qty x unit price),\n  - the table's Total row\
    \ (subtotal, plus tax if computed on line\n    totals, and grand total),\n  - any other place in the\
    \ deck showing the affected numbers: summary\n    slides, callouts/text boxes, charts (both the c:numCache\
    \ cached\n    values and their embedded workbook), and speaker notes,\n  - and keep number formatting\
    \ consistent (currency symbol, thousand\n    separators, decimals).\nIf the deck repeats the same\
    \ table elsewhere, all copies need the same\nedit. Best practice is to recompute the numbers programmatically\
    \ rather\nthan retyping them.\n"
- id: pptx-relayout-01
  answer: "python-pptx has no add-row API, so work at the XML level — clone an\nexisting data row and\
    \ insert it before the Total row:\n\n    from copy import deepcopy\n    from pptx.oxml.ns import qn\n\
    \    tbl = table._tbl\n    trs = tbl.findall(qn('a:tr'))\n    template = trs[1]                  \
    \     # a data row with good format\n    total_tr = trs[-1]                      # the Total row\n\
    \    new_tr = deepcopy(template)\n    total_tr.addprevious(new_tr)            # insert after Service\
    \ G\n\nThen set the texts by editing the cloned cells' <a:t> runs (keep <a:rPr>\nso formatting survives),\
    \ and fix the frame:\n  - shape.height += Emu(new_tr_h).\nIf banding is style-driven (bandRow=\"1\"\
    ) nothing else is needed; if rows\ncarry explicit fills, clone a row with the same band appearance\
    \ or\nadjust the new cells' tcPr fills to match.\n"
- id: pptx-relayout-02
  answer: "Required XML changes in <a:tbl>:\n  1. <a:tblGrid>: insert a new <a:gridCol w=\"...\"/> right\
    \ after the Item\n     column's gridCol.\n  2. Every <a:tr> must gain one <a:tc> at the matching position;\
    \ each\n     new <a:tc> needs an <a:txBody> (with <a:bodyPr>, <a:lstStyle>, and\n     at least an\
    \ empty <a:p>) and an <a:tcPr> with the fill/anchor the\n     neighbours use. Row tc counts must still\
    \ equal the gridCol count.\n  3. Merge bookkeeping: if the insert point falls inside a horizontally\n\
    \     merged cell (e.g. the Total label spanning columns 0-1), the\n     anchor's gridSpan must be\
    \ incremented and an hMerge=\"1\"\n     continuation tc inserted; for vertical spans crossing the\
    \ column,\n     rowSpan/vMerge must be adjusted accordingly. The header row needs\n     a real header-styled\
    \ cell, not a blank one.\n  4. Keep the table on the slide: the sum of gridCol widths must equal\n\
    \     (or stay within) the frame width, and the frame must remain inside\n     the slide. Since the\
    \ table already nearly fills the slide, shrink\n     the existing columns proportionally (or steal\
    \ width from flexible\n     columns) so the total width is unchanged, give the SKU column a\n    \
    \ sensible width, and leave p:xfrm ext cx either matching the new\n     sum or unchanged if the width\
    \ is preserved.\nIn python-pptx terms: after XML surgery, table.columns[i].width can be\nused to set\
    \ the final gridCol widths so the deck stays on-slide.\n"
- id: pptx-tblins-01
  answer: "Use slide.shapes.add_table(rows, cols, left, top, width, height) — it returns a\nGraphicFrame\
    \ whose .table is the Table. For a 3×2 table: add_table(3, 2, ...).\nAll offsets/sizes are EMU, so\
    \ build them with pptx.util helpers (Inches, Cm, Pt).\n\nTo avoid overlapping, survey the slide first:\
    \ iterate slide.shapes and read each\nshape's left/top/width/height, find the lowest bottom edge (shape.top\
    \ + shape.height)\nor a free gap, and place the table there with a margin. Slide bounds come from\n\
    prs.slide_width / prs.slide_height.\n\n    from pptx import Presentation\n    from pptx.util import\
    \ Inches\n\n    prs = Presentation(\"deck.pptx\")\n    slide = prs.slides[0]\n    bottoms = [s.top\
    \ + s.height for s in slide.shapes\n               if s.top is not None and s.height is not None]\n\
    \    top = (max(bottoms) + Inches(0.3)) if bottoms else Inches(1)\n    gf = slide.shapes.add_table(3,\
    \ 2, Inches(0.5), top, Inches(6), Inches(2))\n    table = gf.table\n\nRow heights/column widths are\
    \ auto-distributed from the width/height you pass;\nadjust table.columns[i].width / table.rows[i].height\
    \ afterwards if needed.\n"
- id: pptx-tblins-02
  answer: "The mismatch is the table style. add_table writes a default tableStyleId into\na:tblPr, and\
    \ when that style isn't defined in the deck (ppt/tableStyles.xml)\nPowerPoint falls back to its built-in\
    \ blue \"Medium Style 2 - Accent 1\".\npython-pptx exposes no table.style API, so match the deck by\
    \ copying the style\nid an existing table uses:\n\n    from pptx.oxml.ns import qn\n    ref_id = (existing_table._tbl.tblPr.find(qn('a:tableStyleId')).text)\n\
    \    new_pr = table._tbl.tblPr\n    # remove any existing a:tableStyleId child, then append one with\
    \ ref_id\n\na:tableStyleId must be the last child of a:tblPr per the schema. Also copy the\nlook flags\
    \ (table.first_row, table.horz_banding, etc.) and match column widths\nand cell text formatting. Robust\
    \ fallbacks: clone an existing table's graphicFrame\nXML wholesale and edit its cell contents, or\
    \ style manually\n(cell.fill.solid(); cell.fill.fore_color.rgb = RGBColor(...), font name/size/color)\n\
    to mimic the deck. If the deck defines custom styles in tableStyles.xml, reusing\ntheir id gives an\
    \ exact match.\n"
- id: pptx-tblins-03
  answer: "There is no insert_table on placeholders — only picture placeholders have\ninsert_picture(),\
    \ and a table is a GraphicFrame, not something a placeholder\ncontains. The idiomatic approach is\
    \ to borrow the placeholder's geometry and add\nthe table to the slide's shape tree at that position,\
    \ then delete the empty\nplaceholder so no \"Click to add text\" prompt remains:\n\n    ph = <the\
    \ empty content placeholder, found via slide.placeholders /\n          placeholder_format>\n    gf\
    \ = slide.shapes.add_table(3, 2, ph.left, ph.top, ph.width, ph.height)\n    table = gf.table\n   \
    \ ph._element.getparent().remove(ph._element)   # optional cleanup\n\nCaveat: an empty placeholder\
    \ often has no explicit position on the slide\n(it inherits from the layout), in which case ph.left/ph.top\
    \ are None — read the\nmatching layout placeholder's geometry (slide_layout placeholders) or fall\
    \ back\nto a sensible default box. Removing the placeholder is safe: the layout still\ndefines it,\
    \ only the slide instance drops it.\n"
- id: pptx-imgrep-01
  answer: "Two reliable options:\n\n1) Swap the image behind the existing picture shape. This keeps position,\
    \ size,\ncrop and z-order untouched — you only repoint the a:blip's relationship:\n\n    from pptx.oxml.ns\
    \ import qn\n    pic = <the Picture shape>\n    image_part, rId = pic.part.get_or_add_image_part(\"\
    new.png\")\n    pic._element.blipFill.blip.set(qn('r:embed'), rId)\n\n(get_or_add_image_part is the\
    \ same helper add_picture uses; pic.part is the\nslide part.)\n\n2) Or add the new picture and delete\
    \ the old one, preserving stacking order by\nreinserting the new element at the old element's index\
    \ in the spTree (shapes\nlater in spTree render on top):\n\n    new = slide.shapes.add_picture(\"\
    new.png\", pic.left, pic.top,\n                                   pic.width, pic.height)\n    spTree\
    \ = pic._element.getparent()\n    idx = list(spTree).index(pic._element)\n    spTree.remove(pic._element)\n\
    \    spTree.insert(idx, new._element)\n\nOption 1 is safest for z-order/crop fidelity; option 2 is\
    \ handy when the frame\nshould resize to the new image's natural size.\n"
- id: pptx-imgrep-02
  answer: 'Because the image bytes live in a package part (ppt/media/imageN.png) and that

    part is shared. A p:pic shape doesn''t own its image — it references an image

    part through a relationship. python-pptx deduplicates images: when the same

    image file is used again (anywhere — same slide, another slide), it reuses the

    existing image part and relationship rather than adding a copy. So slide 1''s

    logo and slide 7''s logo were both pointing at the same image part, and

    overwriting the part''s blob changed every picture that references it.

    Fix: give the picture you''re changing its own image part — create a new image

    part/rId and set only that picture''s a:blip r:embed to it, leaving the other

    picture''s relationship (and part bytes) alone.

    '
- id: pptx-imgrep-03
  answer: 'Because relationships are shared, not per-shape. When two pictures on the same

    slide use the same image, get_or_add_image_part() finds the image part is

    already related to the slide part and returns the existing rId — both p:pic

    elements'' a:blip r:embed reference the same relationship. drop_rel() removes

    a relationship wholesale and does not count how many shapes use it, so the

    surviving picture lost its image source and renders broken.

    Only drop the rId after scanning the slide''s XML (all a:blip r:embed values,

    also fills/backgrounds) and confirming nothing else uses it; other slides are

    unaffected because they hold their own relationships. If in doubt, skip

    drop_rel: an unused relationship is harmless (it just keeps the bytes in the

    package), and python-pptx''s save walks the relationship graph, so a part that

    becomes truly unreachable (all its rels dropped) is simply not written.

    '
- id: pptx-imgins-01
  answer: "Pass only width to add_picture — with height omitted, python-pptx computes it\nfrom the image's\
    \ native size (using its DPI), preserving the aspect ratio.\nSince the resulting height isn't known\
    \ up front, insert first, then anchor\nbottom-right:\n\n    from pptx import Presentation\n    from\
    \ pptx.util import Inches\n\n    prs = Presentation(\"deck.pptx\")\n    slide = prs.slides[-1]\n \
    \   margin = Inches(0.3)\n    pic = slide.shapes.add_picture(\"signature.png\", 0, 0, width=Inches(2))\n\
    \    pic.left = prs.slide_width - pic.width - margin\n    pic.top = prs.slide_height - pic.height\
    \ - margin\n\nTo avoid covering content, compute the final rectangle (left, top, width,\nheight) and\
    \ test it against every existing shape's bounding box on the slide;\nif it intersects any shape, shift\
    \ it (e.g. up above the lowest shape's bottom,\nor into the nearest free gap) rather than layering\
    \ it on top.\n"
- id: pptx-imgins-02
  answer: 'insert_picture() makes the picture fill the placeholder completely by

    center-cropping the image to the placeholder''s aspect ratio — it writes the

    crop as a srcRect in the blipFill rather than distorting the photo. A portrait

    photo in a wider (or shorter) placeholder therefore loses whatever falls

    outside that window, typically the top and bottom. The full image is still in

    the file; only the view is cropped. Fixes: adjust the crop on the returned

    picture (crop_left, crop_right, crop_top, crop_bottom are settable — e.g.

    set them back to 0), pick/size a placeholder whose aspect ratio matches the

    photo, or accept the crop and re-center it by tweaking the crop offsets.

    '
- id: pptx-legacy-01
  answer: "No. python-pptx only reads/writes the OOXML package format (.pptx and friends);\na binary PowerPoint\
    \ 97–2003 .ppt has no [Content_Types].xml / OPC structure and\nfails to open. Workflow:\n\n1. Never\
    \ edit the original in place — work on copies.\n2. Convert first: soffice --headless --convert-to\
    \ pptx deck.ppt\n   (optionally --outdir <dir>).\n3. Verify the conversion: open the .pptx and check\
    \ the table survived as a real\n   table (iterate slide.shapes, shape.has_table, read cell text).\
    \ LibreOffice\n   conversion sometimes mangles tables into images or loose text boxes; if so,\n  \
    \ rebuild the table rather than \"editing\" a picture.\n4. Edit the .pptx with python-pptx, save,\
    \ verify, and deliver the .pptx.\n5. Only if the user insists on 97–2003 output: convert back with\n\
    \   soffice --convert-to ppt and warn that fidelity may degrade.\n"
- id: pptx-legacy-02
  answer: "Before editing:\n- Render/inspect the converted file (PowerPoint, LibreOffice, or convert to\n\
    \  PDF/images) and compare against the original .ppt: slide count and order,\n  slide size/orientation,\
    \ fonts (substitutions are common), images, and\n  especially whether the table survived as a GraphicFrame\
    \ with has_table=True\n  rather than an image or fragmented text boxes.\n- Confirm python-pptx round-trips\
    \ it: Presentation(\"converted.pptx\") opens and\n  the objects you need are addressable.\n- Keep\
    \ the original .ppt and the conversion as separate copies.\n\nAfter editing:\n- Re-render and visually\
    \ check the changed slides; reopen the saved file and\n  make sure there is no repair prompt.\n\n\
    Tell the user: the file was format-converted (binary .ppt → OOXML .pptx), the\noriginal is preserved\
    \ untouched, LibreOffice conversion can introduce small\nrendering differences (font substitution,\
    \ text autofit/spacing, effects,\nanimation fidelity), so they should review before relying on it,\
    \ and the\ndeliverable is .pptx unless they specifically need it converted back to .ppt\n(with the\
    \ same fidelity caveats).\n"
- id: pptx-verify-01
  answer: "1. Never overwrite the only copy in place — save to a new file\n   (prs.save(\"deck_edited-<date>.pptx\"\
    )) and keep the original read-only.\n2. Reopen the saved file with python-pptx and assert the change:\
    \ slide count\n   unchanged, the table/picture exists, expected text/values/geometry\n   (loop slide.shapes;\
    \ use shape.has_table, has_text_frame, etc.).\n3. Package sanity: the file must open as a valid zip\
    \ (python -m zipfile -t or\n   unzip -t) and python-pptx must reopen it without exceptions.\n4. Render\
    \ it (e.g. soffice --headless --convert-to pdf, then eyeball the PDF or\n   exported page images)\
    \ or open in PowerPoint — confirm the visuals and that\n   no \"needs repair\" dialog appears.\n5.\
    \ Only then deliver; keep both versions so you can diff or roll back.\n"
- id: pptx-verify-02
  answer: "Common causes after table/XML edits:\n- Schema order violations inside a:tbl: children must\
    \ be a:tblPr, then a:tblGrid,\n  then the a:tr rows; a:tableStyleId must be the last child of a:tblPr;\
    \ each\n  a:tc requires its child elements in order (a:txBody, a:tcPr, ...).\n- Grid/row mismatch:\
    \ a row's a:tc count != the number of a:gridCol elements;\n  missing gridCol widths, zero/negative\
    \ row heights, or zero/negative\n  graphicFrame ext cx/cy.\n- Invalid attribute values: wrong boolean\
    \ form (1 vs true), bad enum strings,\n  non-integer or out-of-range EMU values, duplicate shape ids\
    \ in p:cNvPr within\n  a slide.\n- Broken references: an r:embed or tableStyleId pointing at a relationship\
    \ or\n  style that doesn't exist; parts added to the zip without matching\n  [Content_Types].xml entries\
    \ or wrong content types.\n- String-surgery artifacts: unescaped & or < characters, invalid UTF-8,\
    \ broken\n  or renamed namespaces from regex edits.\n- Zip-level problems: saving over a partially\
    \ written file, path/case\n  mismatches (PPT/ vs ppt/), missing .rels files.\n"
- id: pptx-verify-03
  answer: 'Usually no. Deleting the p:pic element removes only the reference in the

    slide''s shape tree; the image part (ppt/media/imageN.png) and the slide part''s

    relationship to it remain in the package, so the image bytes are still inside

    the .pptx — and any other picture sharing that part still displays it.

    How to check: open the file as a zip (a .pptx is one) and inspect it —

    unzip -l deck.pptx | grep media to list media parts, and grep

    ppt/slides/_rels/slideN.xml.rels for the relationship; compare file sizes or

    hashes of ppt/media/* against the original image to identify it.

    To truly purge it: after removing the p:pic, call slide.part.drop_rel(rId) only

    after confirming no other shape, slide, layout, or master references that image

    part, then save — python-pptx serializes only parts reachable through the

    relationship graph, so the orphaned image is left out of the saved file. Also

    check docProps/thumbnail.jpeg, which may still show a rendered slide.

    '
