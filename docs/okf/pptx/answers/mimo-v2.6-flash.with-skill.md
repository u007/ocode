- id: pptx-model-01
  answer: "In a .pptx each slide is a part (ppt/slides/slideN.xml) and a table lives inside\na <p:graphicFrame>\
    \ shape on that slide, under\n<a:graphic><a:graphicData uri=\"http://schemas.openxmlformats.org/drawingml/2006/table\"\
    >.\n\nInside it:\n- <a:tbl> — the table root.\n- <a:tblPr> — table-level properties (style id, banding\
    \ flags firstRow/bandRow,\n  merge-style flags). Attributes, not child elements.\n- <a:tblGrid> with\
    \ one <a:gridCol w=\"...\"> per column — holds the column widths\n  (w in EMU). There is one gridCol\
    \ per logical column, regardless of merges.\n- One <a:tr h=\"...\"> per row — h is the row height\
    \ in EMU.\n- Inside each a:tr, exactly one <a:tc> per a:gridCol (merged cells still occupy\n  their\
    \ slots: the origin tc carries gridSpan/rowSpan, spanned slots are separate\n  a:tc elements with\
    \ hMerge=\"1\"/vMerge=\"1\").\n- Each <a:tc> may carry <a:tcPr> (fill, borders, margins, anchor, gridSpan\
    \ is on\n  a:tc not a:tcPr) and contains <a:txBody> with <a:p> paragraphs, <a:pPr>, and\n  <a:r> runs\
    \ whose <a:rPr> holds bold/size/colour and whose <a:t> holds the text.\n\npython-pptx exposes this\
    \ as shape.has_table → shape.table, table.rows (a:tr),\ntable.columns (a:gridCol), table.cell(r, c);\
    \ text is reached through\ncell.text_frame.paragraphs[i].runs[j].text.\n"
- id: pptx-model-02
  answer: 'Units: EMU (English Metric Units). 914400 EMU = 1 inch, 9144000 EMU = 10 inches,

    12700 EMU = 1 point. All offsets/sizes in python-pptx (shape.left/top/width/height,

    row.height, gridCol.w, a:xfrm/a:off and a:ext) are EMU integers. DrawingML also

    uses percent/em for text and scaling properties, and grid/fill units elsewhere,

    but layout geometry is EMU.


    A default 16:9 deck (the modern PowerPoint "Widescreen" template) is

    12192000 × 6858000 EMU, i.e. 13.333 × 7.5 inches (OOXML 16:9 can also be

    12192000 × 6858000 written as 12192000×6858000 exactly).


    Caveat: python-pptx''s Presentation() with no argument uses the built-in default

    template, which is 4:3 — 9144000 × 6858000 EMU (10 × 7.5 in). Never assume 16:9;

    read prs.slide_width / prs.slide_height.

    '
- id: pptx-model-03
  answer: "It is on the slide layout (ppt/slideLayouts/slideLayoutN.xml) or, more usually,\non the slide\
    \ master (ppt/slideMasters/slideMasterN.xml), stored as a plain\nnon-placeholder picture/graphicFrame\
    \ shape (or as part of the background).\nInherited layout/master shapes are rendered on every slide\
    \ that uses that\nlayout/master, but they are not members of slide.shapes, so iterating slide.shapes\n\
    never sees them.\n\nConsequences of editing it there:\n- The edit propagates to every slide using\
    \ that layout (master edits propagate to\n  every layout, hence to effectively the whole deck). It\
    \ is one shared image part,\n  so the change is global unless you first detach it (copy the shape\
    \ per slide or\n  give slides their own logo picture).\n- Layout/master content is drawn behind (under)\
    \ slide content, so z-order is fixed.\n- Placeholders inherit their formatting from the layout/master;\
    \ editing the\n  placeholder there changes every slide's inherited look.\n- To edit you must open\
    \ the layout/master part (SlideLayout/SlideMaster objects,\n  not slide.shapes); python-pptx can modify\
    \ them but offers no shape insertion API\n  beyond what the object exposes, and any image you add\
    \ shares relationships on\n  that part — dropping the old rel requires checking r:embed/r:link references\
    \ on\n  that part, not on the slide.\n- If even one slide has its own foreground copy of the logo,\
    \ that slide will not\n  change; verify all slides after editing.\n"
- id: pptx-locate-01
  answer: "Find the table by content, not by index:\n\n1. Recurse over slide.shapes (descend into GroupShape.shapes\
    \ when\n   shape.shape_type == MSO_SHAPE_TYPE.GROUP) and keep every shape where\n   shape.has_table\
    \ is True.\n2. Pick the one whose first row's cell texts match the expected header, e.g.\n   [c.text.strip()\
    \ for c in tbl.rows[0].cells] == [\"Item\", \"Qty\", \"Rate\", ...].\n   Assert exactly one table\
    \ matches (a slide can hold several, and the first\n   has_table shape is not necessarily \"the invoice\
    \ table\").\n3. Find the row by exact text, not substring:\n   hits = [r for r in tbl.rows\n     \
    \      if any(c.text_frame.text.strip() == \"Gadget D\" for c in r.cells)]\n   assert len(hits) ==\
    \ 1  # zero or several → stop and ask\n   row = hits[0]; idx = tbl.rows.index(row)  # or track i while\
    \ iterating\n\nDo the same header/row assertions against slide 3 specifically (verify you are on\n\
    slide 3 by its shapes or index prs.slides[2]), and keep the graphicFrame reference\n(the shape whose\
    \ has_table is True) for later height/geometry updates — Table has\nno _parent to get back to it.\n"
- id: pptx-locate-02
  answer: "Representation: a horizontal merge is expressed as ATTRIBUTES on the cell\nelement itself:\n\
    - The origin (top-left) cell: <a:tc gridSpan=\"2\" rowSpan=\"1\"> (and rowSpan for\n  vertical spans).\
    \ In python-pptx: cell.is_merge_origin, cell.span_width,\n  cell.span_height, cell.merge(other_cell)/cell.split().\n\
    - Every other slot in the spanned region is still its own <a:tc>, carrying\n  hMerge=\"1\" (or vMerge=\"\
    1\", or both) — cell.is_spanned. Covered cells are real\n  _Cell objects; text written to them is\
    \ stored but never displayed.\n\nThings to watch:\n- Every a:tr must still contain exactly one a:tc\
    \ per a:gridCol — deleting a\n  covered tc (or the origin) without fixing gridSpan/hMerge breaks the\
    \ count and\n  PowerPoint reports the file needs repair.\n- Write/read text only on the origin cell;\
    \ text put into a covered cell is hidden.\n- When inserting or deleting columns, the merge must be\
    \ updated too: the Total\n  row's gridSpan changes (e.g. 2→3), and you must add/remove a matching\
    \ covered\n  a:tc with hMerge=\"1\" so the slot count still equals the gridCol count.\n- gridSpan\
    \ + neighbours must add up to the gridCol count; otherwise \"repair\".\n- Banding/row shading is computed\
    \ per logical column, so merges plus explicit\n  fills interact — re-check fills after changing spans.\n\
    - Merges are attributes of a:tc, never children of a:tcPr, and never the literal\n  value \"continue\"\
    .\n"
- id: pptx-rowdel-01
  answer: "No — python-pptx (1.0.x) has no table.rows.remove(), no Table.delete_row(),\nno add_row()/add_column(),\
    \ no insert or delete API at all on Table, rows or\ncolumns. You delete at the XML level:\n\n  tbl\
    \ = table._tbl                     # the <a:tbl> lxml element\n  tr = table.rows[i]._tr          \
    \     # or tbl.tr_lst[i]\n  tbl.remove(tr)                       # rows below move up automatically\n\
    \nequivalently tr.getparent().remove(tr).\n\nThen clean up:\n- recompute any Total/derived cell that\
    \ included the removed row;\n- set the frame height: gf.height = sum(r.height for r in table.rows);\n\
    - re-alternate explicit row fills if banding was hand-applied;\n- verify no text from the deleted\
    \ row remains in any ppt/slides/slide*.xml part.\n"
- id: pptx-rowdel-02
  answer: "It matters. a:tr heights are removed with the row, so the sum of the remaining\nrows no longer\
    \ matches the graphicFrame's <a:xfrm><a:ext cy>. The frame is the\nshape's real geometry: an oversized\
    \ cy leaves a stale hit/selection region,\nmisreports the table's bottom edge (overlap checks against\
    \ shapes below become\nwrong), can push the perceived bottom past prs.slide_height, and some\nrenderers/pipelines\
    \ lay out from the frame while PowerPoint lays out from the\nrows — so the two disagree (clipping/overlap\
    \ in export, PDF, thumbnails).\nPowerPoint itself also rewrites cy when it saves, so the file looks\
    \ different\nbetween tools.\n\nFix: recompute from the rows and store it on the frame —\n  gf.height\
    \ = sum(r.height for r in table.rows)\n(i.e. set a:xfrm/a:ext/@cy = sum of a:tr/@h). Do not touch\
    \ a:off (top).\nThen assert gf.top + gf.height <= prs.slide_height and that gf does not now\noverlap\
    \ the shapes below (move them or ask).\n"
- id: pptx-rowdel-03
  answer: "Because the shading is implemented differently in the two decks:\n\n- Style-driven banding:\
    \ the table has a table style (a:tblPr/@firstRow,\n  bandRow=\"1\" plus a style GUID) and no per-cell\
    \ fills. PowerPoint derives the\n  colour of each row from its INDEX among the remaining rows, so\
    \ after a delete\n  the bands simply recompute and the alternation looks correct.\n- Explicit fills:\
    \ each a:tc's a:tcPr carries a hard-coded <a:solidFill> copied\n  when the row was created. The colours\
    \ travel with the cells, so deleting a\n  middle row leaves two neighbouring rows with the same fill\
    \ — the parity is\n  fixed in the data, not computed.\n\nSo a deck whose alternating look comes from\
    \ explicit fills (or from copied rows\nwhose fills were baked in) breaks, while a deck relying on\
    \ the table style's\nbandRow flag repairs itself. Related causes: bandRow/firstRow flags turned off,\n\
    or banding overridden by an explicit fill on one row only. Fix by re-applying\nfills to alternate\
    \ rows after the delete (or restoring banding and clearing the\nper-cell solidFill).\n"
- id: pptx-rowdel-04
  answer: "Because it hides the row instead of deleting it:\n\n- The text is still in the file: the a:tr/a:tc/a:t\
    \ content remains in the slide\n  XML, so it is extractable by unzip+grep, by screen readers, by copy/paste\
    \ and\n  by \"find\". Any check for removed content (or an anti-cheat diff) finds it —\n  a near-zero\
    \ height or white-on-white is cosmetic, not a delete.\n- PowerPoint will not honour a zero-height\
    \ row containing text: rows auto-grow to\n  fit their content when the file is opened/edited, so the\
    \ hidden row reappears,\n  overlaps its neighbours, and pushes the table taller.\n- Totals and references\
    \ still include (or silently exclude) the wrong rows —\n  the numbers stop matching the visible content,\
    \ and the banding parity is off.\n- A floating white rectangle is worse: it is a separate p:sp that\
    \ covers the row\n  but moves with nothing else, sits above content in z-order, prints as a blank\n\
    \  block, disappears if the table is moved/resized, and its own text/shape is\n  still in the XML.\n\
    - It also bloats the file and leaves stale geometry (frame cy vs row sum).\n\nCorrect approach: remove\
    \ the a:tr from a:tbl, then fix the Total, gf.height and\nbanding, and verify the row's text is absent\
    \ from every ppt/slides/slide*.xml.\n"
- id: pptx-cell-01
  answer: "You lose it. cell.text (and cell.text_frame.text) calls clear(): it removes the\nexisting runs/paragraphs\
    \ and creates a brand-new run with no <a:rPr>. Bold,\n14 pt and the white colour lived in that run's\
    \ rPr (and the paragraph's pPr), so\nthey are discarded and the text falls back to the table style\
    \ / theme defaults —\ntypically black, regular, default size. The cell itself keeps only tcPr-level\n\
    properties (fill, borders, insets, anchor), which is why the background may look\nunchanged while\
    \ the text clearly changed.\n\nWays to keep it:\n- Best: edit the existing run instead of replacing\
    \ the text frame:\n    p = cell.text_frame.paragraphs[0]\n    p.runs[0].text = \"SKU\"          #\
    \ rPr untouched\n  (if the text spans several runs, set the first and delete the rest).\n- Or copy\
    \ the formatting: before the assignment capture run.font (bold, size,\n  colour.rgb, name) / the rPr\
    \ element, then re-apply to the new run; or\n  deepcopy the original a:rPr onto the new run.\n- Or\
    \ build it manually: cell.text_frame.text = \"SKU\" then set\n  run.font.bold = True, run.font.size\
    \ = Pt(14), run.font.color.rgb = RGBColor(...).\n"
- id: pptx-cell-02
  answer: "Layout: the cell's width is fixed by its a:gridCol/@w, so the longer name wraps\nonto more\
    \ lines inside the same column width. DrawingML table rows have a minimum\nheight, not a fixed one\
    \ — PowerPoint grows that a:tr's height (@h) to fit the\nwrapped text. Consequences:\n- the row (and\
    \ therefore the whole table) becomes taller than the graphicFrame's\n  a:ext cy, which is now stale\
    \ in the XML;\n- the table's real bottom moves down, possibly past prs.slide_height (clipped/\n  off-slide\
    \ content) or overlapping the shapes below it;\n- the Total row and everything after it shift down,\
    \ and the column simply cannot\n  get wider unless you resize it — text never overflows into the next\
    \ cell.\n\nHow to control it:\n- Shorten/truncate the name, or move the long text to a comment/notes.\n\
    - Widen the Item column by raising its gridCol/@w (and shrinking neighbours /\n  other columns so\
    \ the sum of gridCol widths still equals the frame width), or\n  narrow other columns and let the\
    \ item column take the space.\n- Reduce the cell font size / character spacing so it fits on fewer\
    \ lines.\n- Set an explicit row.height afterwards — but note it is only a minimum;\n  PowerPoint will\
    \ still expand it if the text does not fit.\n- Afterwards recompute gf.height = sum(row.height for\
    \ row in table.rows), re-check\n  gf.top + gf.height <= prs.slide_height and no overlap with shapes\
    \ below, and\n  recompute any Total/derived figures if the edit touched them.\n"
- id: pptx-cell-03
  answer: "Qty 5 → 12 changes derived numbers, so you must update, at minimum:\n- That row's Amount/Line\
    \ Total: Quantity × Unit Price (e.g. 12 × rate), written\n  into the row's amount cell.\n- The invoice/table\
    \ Total row: re-sum all line amounts (and any Subtotal).\n- Any tax, discount, shipping or grand-total\
    \ lines derived from the subtotal\n  (they all move when one line amount moves).\n- Any other place\
    \ in the deck that repeats these figures: summary/overview\n  slides, charts or tables sourced from\
    \ the line items, notes pages, and any\n  duplicated copy of the table (e.g. a second slide or an\
    \ appendix).\n- If Qty feeds a per-line narrative (e.g. \"5 units delivered\") in body text or\n \
    \ speaker notes, update that wording too.\n\nThen re-verify: recompute the totals in code and compare\
    \ with the printed values\n(assert sum(line_amounts) == displayed_total) so no stale number survives,\
    \ and\nre-check the frame geometry if row heights changed.\n"
- id: pptx-relayout-01
  answer: "python-pptx has no add_row(), so copy an existing body row's XML and splice it in:\n\n  import\
    \ copy\n  ref_tr  = row_of_service_g._tr          # a:tr to clone (a normal body row)\n  total_tr\
    \ = total_row._tr\n  new_tr = copy.deepcopy(ref_tr)          # keeps a:tc count, fills, rPr, h\n \
    \ ref_tr.addnext(new_tr)                  # lands between Service G and Total\n  # equivalently: total_tr.addprevious(new_tr)\n\
    \nNever append-and-move, and note deepcopy duplicates nothing that needs unique\nids (a:tr/a:tc have\
    \ no ids) — but check any content that must stay unique.\n\nFill it through the existing runs so formatting\
    \ survives (cell.text rebuilds the\nparagraph and drops a:rPr):\n\n  from pptx.oxml.ns import qn\n\
    \  for cell, value in zip(table.rows[i].cells, values):\n      run = cell.text_frame.paragraphs[0].runs[0]\n\
    \      run.text = value\n\nThen:\n- recompute the Total row (sum of the line amounts);\n- fix the\
    \ stale frame height: gf.height = sum(r.height for r in table.rows)\n  (XML inserts never update a:xfrm/@cy\
    \ on their own);\n- re-alternate explicit banding fills if shading is baked per-cell (the copied\n\
    \  row carries Service G's fill);\n- check gf.top + gf.height <= prs.slide_height and no overlap with\
    \ shapes below;\n  slide them down or ask;\n- verify: reopen and assert the new row's cell texts,\
    \ its position (after Service\n  G, before Total) and the recomputed Total.\n"
- id: pptx-relayout-02
  answer: "There is no add_column(); you edit a:tbl by hand. For a table with N gridCols,\ninserting after\
    \ the Item column (index k) requires, for EVERY element:\n\n1. a:tblGrid: insert a new <a:gridCol\
    \ w=\"...\"> at position k+1 (width in EMU —\n   usually stolen from the existing columns, not added).\n\
    2. Every a:tr: insert a new <a:tc> at child index k+1, deep-copied from a\n   comparable existing\
    \ cell (so it carries a valid <a:txBody> and a:tcPr fills/\n   borders — an a:tc without a:txBody\
    \ breaks the file). Each row must end up\n   with exactly one a:tc per a:gridCol.\n3. Merged rows:\
    \ if the Total row's label spans columns (gridSpan), bump the\n   origin's gridSpan by 1 and insert\
    \ a matching covered cell <a:tc hMerge=\"1\">\n   in the right slot; every spanned row needs the extra\
    \ slot. gridSpan values\n   must still add up to the gridCol count.\n4. Fill the new cells through\
    \ the existing run (paragraphs[0].runs[0].text = ...)\n   to keep rPr formatting; add a blank run/txBody\
    \ if the copied cell had none.\n5. Table width bookkeeping: the graphicFrame width (a:xfrm/a:ext/@cx)\
    \ must equal\n   the sum of gridCol widths, so either set gf.width = sum(c.width for c in\n   table.columns)\
    \ after choosing the new widths, or set it explicitly.\n\nKeeping it on the slide: the table already\
    \ spans nearly the full width, so you\ncannot add width — steal it. Scale every gridCol down so the\
    \ total fits:\n   budget = prs.slide_width - gf.left - desired_margin\n   new_w_i = old_w_i * budget\
    \ / current_total     (integer EMU)\nthen set each gridCol's w, set gf.width = sum(new_w_i), and assert\n\
    gf.left + gf.width <= prs.slide_width. Alternatively shrink only the widest\ncolumns (or the columns\
    \ you choose), or first move gf.left to 0 to buy margin.\nNote gf.top/height are unaffected, but re-check\
    \ the frame's bottom against\nprs.slide_height and against shapes below, and verify by reopening:\
    \ one more\ngridCol than before, one more a:tc in every row, correct header text, merges\nintact,\
    \ totals unchanged, all other slides byte-identical.\n"
- id: pptx-tblins-01
  answer: "Position: first read the real slide geometry with prs.slide_width /\nprs.slide_height (never\
    \ assume 16:9 — Presentation()'s default template is\n4:3) and enumerate every shape on the slide\
    \ (recursing into groups) to get\nits left/top/width/height. Pick a free rectangle: either the bounds\
    \ of an\nempty content placeholder if one exists, or a gap you compute as\nmax(bottom of the lowest\
    \ shape) + a margin, asserting the new\nleft+width <= slide_width and top+height <= slide_height and\
    \ that the box\ndoes not intersect any existing shape's box.\n\nCreate it with:\n  from pptx.util\
    \ import Inches\n  gf = slide.shapes.add_table(3, 2, left, top, width, height)\nadd_table returns\
    \ a GraphicFrame; its .table gives you rows/cells. After\nfilling cells, set gf.height = sum(row.height\
    \ for row in table.rows) so\nthe frame matches the auto-grown row heights, and re-check the bottom\n\
    edge against the slide and the shapes below.\n"
- id: pptx-tblins-02
  answer: 'The look comes from the table style GUID, not from your cells: add_table

    writes a default style (the blue "Medium Style 2 Accent 1") into

    <a:tblPr><a:tableStyleId>. To match the deck, read the tableStyleId from

    an existing table''s XML (find a has_table shape, then

    table._tbl.tblPr.find(qn(''a:tableStyleId'')).text) and set the same GUID on

    the new table''s tblPr. Also copy the banding flags — table.first_row,

    table.horz_banding etc. live on a:tblPr, not on a:tr — and, for a

    pixel-exact match, copy the cell fill/font formatting from a reference body

    cell (existing tables may override the style with explicit a:solidFill /

    run properties). Verify by re-opening the saved file and comparing the

    tableStyleId of the new table with the reference tables.

    '
- id: pptx-tblins-03
  answer: "It depends on the placeholder type.\n- A real TablePlaceholder (a p:ph type=\"tbl\") has insert_table():\
    \ call\n  ph.insert_table(rows, cols) — it returns a GraphicFrame already placed in\n  the placeholder.\n\
    - A content placeholder on a \"Title and Content\" layout (type OBJECT) is a\n  SlidePlaceholder with\
    \ NO insert_table. The idiomatic workaround is to\n  read the placeholder's left/top/width, call\n\
    \  slide.shapes.add_table(rows, cols, ph.left, ph.top, ph.width, height)\n  there, and then remove\
    \ the empty placeholder element\n  (ph._element.getparent().remove(ph._element)) so no \"Click to\
    \ add text\"\n  box is left behind. The table is then an ordinary shape positioned where\n  the placeholder\
    \ sat — it is not \"inside\" the placeholder.\n"
- id: pptx-imgrep-01
  answer: "1. Find the p:pic (recursing into groups if needed) and record its\n   left/top/width/height\
    \ and its position among the parent element's\n   children (its z-order index).\n2. Load the new image\
    \ with slide.shapes.add_picture(new_path, left, top,\n   width, height) — passing both width and height\
    \ keeps the old box exactly\n   (if you want to preserve the new file's aspect ratio instead, pass\
    \ only\n   one dimension and then set the other, or pre-crop).\n3. Preserve stacking order by moving\
    \ the new element into the old element's\n   slot: old_el.addnext(new_el) (or addprevious, depending\
    \ on the desired\n   order) then old_el.getparent().remove(old_el). Do not just append —\n   appended\
    \ shapes land on top.\n4. Clean up: the old image relationship is still on the slide part. Drop it\n\
    \   only after confirming nothing else uses it — count\n   part._element.xpath(\"//@r:embed | //@r:link\"\
    ) for that rId and drop it\n   only when the count is 0.\n"
- id: pptx-imgrep-02
  answer: 'Because image parts are shared, not per-picture. python-pptx keys an image

    by its content hash: two pictures on different slides that were created

    from identical bytes get the SAME image part (same /ppt/media/imageN.png,

    often the same rId within a part). Slide 1 and slide 7 therefore point at

    one part, so overwriting that part''s blob/_blob changes the image

    everywhere it is referenced. Additionally, blob overwriting does not change

    the part''s hash key, so the package ends up internally inconsistent with

    the dedup index.


    Safe replacement: add the new file as a genuinely new image

    (add_picture / a fresh image part) and swap the p:pic as in the previous

    answer, leaving other slides'' parts untouched. If you must reuse the part,

    you have changed it for every reference — which is exactly what you saw.

    '
- id: pptx-imgrep-03
  answer: 'Because part.drop_rel(rId) decides whether a relationship is "in use" by

    counting references of the form r:id only (//@r:id — used by notesSlides,

    hyperlinks, audio refs, etc.). Pictures reference their image through

    <a:blip r:embed="rIdN"> (and sometimes r:link), which the count does not

    see. So any image relationship appears to have zero references, the count

    is < 2, and drop_rel unconditionally removes it — even though other p:pic

    elements on the same slide still carry that r:embed. Those pictures then

    point at a relationship that no longer exists and PowerPoint reports a

    broken/repairable file.


    Correct procedure: before dropping, count

    slide_part._element.xpath("//@r:embed | //@r:link") entries equal to rId,

    and drop only when that count is 0.

    '
- id: pptx-imgins-01
  answer: "Width-first insertion, then anchor it, then verify:\n  pic = slide.shapes.add_picture(\"signature.png\"\
    ,\n          left=0, top=0, width=Inches(2))   # height omitted → native AR\n  pic.left = prs.slide_width\
    \  - pic.width  - margin\n  pic.top  = prs.slide_height - pic.height - margin\nPassing only width\
    \ keeps the aspect ratio (python-pptx computes height\nfrom the image's intrinsic dimensions); you\
    \ can read pic.width/pic.height\nafterwards to do the bottom-right math in EMU.\n\n\"Without covering\
    \ existing content\" is then an explicit check: iterate the\nother shapes (including groups) and assert\
    \ no intersection between the new\npicture's box (left, top, left+width, top+height) and theirs; if\
    \ it\nintersects, shrink the width (height follows proportionally) or nudge it\ninto the nearest free\
    \ corner and re-check, and also assert\npic.left >= 0, pic.top >= 0, left+width <= slide_width,\n\
    top+height <= slide_height.\n"
- id: pptx-imgins-02
  answer: 'Because insert_picture() does not merely place the photo — it crops the

    photo so that it fills the placeholder''s frame at the placeholder''s aspect

    ratio (the same behaviour PowerPoint shows when you "fill" a picture

    placeholder). The image is scaled to cover the frame and the overflow is

    stored as crop_left/crop_right/crop_top/crop_bottom. A portrait photo in a

    landscape frame therefore loses its top and bottom (and vice versa: a

    landscape photo in a portrait frame loses its left and right), so "part of

    the photo disappears".


    Fixes: pre-crop the image yourself to the placeholder''s aspect ratio;

    afterwards set the crop values back to 0 and accept that the picture will

    then not fill the frame exactly; or use a placeholder whose aspect ratio

    matches the photo; or add the picture as a normal shape instead of using

    insert_picture().

    '
- id: pptx-legacy-01
  answer: "No. python-pptx only reads/writes OOXML packages (.pptx/.potx/.ppsx — the\nZIP of XML parts).\
    \ A .ppt is the binary Compound File (OLE) format and\npython-pptx raises an error (its content-types\
    \ sniffing does not recognise\nit); there is no supported conversion path inside the library.\n\n\
    Workflow:\n1. Tell the user and keep the original .ppt untouched as the source of\n   truth (make\
    \ a backup copy first).\n2. Convert outside python-pptx, e.g. LibreOffice headless:\n     soffice\
    \ --headless --convert-to pptx deck.ppt\n   (or ask the user to \"Save As PowerPoint (.pptx)\" in\
    \ PowerPoint itself,\n   which is the highest-fidelity option).\n3. Verify the converted file opens\
    \ and round-trips (open it with\n   python-pptx, check slide count, tables, images, fonts, slide size\
    \ —\n   conversion is lossy).\n4. Edit the .pptx with python-pptx, save to a NEW file, and verify\
    \ per the\n   verify procedure.\n5. Report back: the deliverable is .pptx; if they still need .ppt,\
    \ re-export\n   with LibreOffice/PowerPoint and warn that another round trip may lose\n   more formatting.\n"
- id: pptx-legacy-02
  answer: "Before editing (on the freshly converted .pptx):\n- Keep the original .ppt as the untouched\
    \ master copy.\n- Open the converted file and inventory it: slide count and order, slide\n  dimensions,\
    \ all text (notes too), every table (cell text, merges,\n  totals), pictures and their positions,\
    \ fonts/theme, layouts and\n  placeholders, animations/transitions, embedded objects/OLE, macros\n\
    \  (a .ppt with macros must be converted to .pptm, and LibreOffice drops\n  VBA), hyperlinks, and\
    \ speaker notes. LibreOffice conversion commonly\n  loses or changes: fonts and font substitution,\
    \ complex merges, SmartArt,\n  animations, WordArt, charts (re-rendered), shadows/3D effects, embedded\n\
    \  objects, and precise text metrics (line wrapping can shift).\n- Record a before-snapshot (text\
    \ dump per slide, shape geometry) so you can\n  diff later, and check the file opens without a repair\
    \ prompt.\n\nAfter editing:\n- Save to a new file, reopen, diff every slide against the pre-edit\n\
    \  snapshot — the only differences must be the intended ones.\n- Check geometry stays inside prs.slide_width/slide_height,\
    \ tables are\n  well formed (a:tc count == a:gridCol count, merges add up, no duplicate\n  p:cNvPr\
    \ ids, no r:embed to dropped rels), and unzip -t / XML parse the\n  package.\n- Render or visually\
    \ inspect the edited slides (and any slide near the\n  edit) for layout drift caused by font substitution.\n\
    \nWhat to tell the user: the .ppt was converted, so fidelity is not\nguaranteed — some formatting/animations/fonts/embedded\
    \ objects may have\nchanged even though they did not touch them; the original .ppt remains the\nauthoritative\
    \ source; deliver both files and a list of anything observed to\nhave changed; if pixel-perfect fidelity\
    \ matters, edit in PowerPoint and\nre-save.\n"
- id: pptx-verify-01
  answer: "1. Never overwrite in place: write the script output to a NEW file (e.g.\n   deck_edited.pptx)\
    \ and leave deck.pptx untouched, so you can always fall\n   back and diff.\n2. Structural sanity:\
    \ unzip -t (or zipfile.testzip()) the output; parse\n   every XML part (lxml/ElementTree) to confirm\
    \ well-formedness.\n3. Re-open with python-pptx and assert the intended change EXACTLY: every\n  \
    \ cell text of the edited table in row/cell order, the recomputed Total,\n   shape counts, and geometry\
    \ (gf.top + gf.height <= prs.slide_height, no\n   overlap with shapes below).\n4. Search every ppt/slides/slide*.xml\
    \ part for text that must be gone (and\n   for text that must be present) — text hidden by white-on-white\
    \ or\n   near-zero-height rows still lives in the XML.\n5. Prove nothing else changed: compare each\
    \ untouched slide's XML (or its\n   text + shape geometry) with the original, and check that the media\
    \ parts\n   and relationships are as expected (no orphaned/dangling r:embed).\n6. Optionally render\
    \ the affected slides (LibreOffice → PDF/PNG) for a\n   visual check, and open the file in PowerPoint\
    \ to confirm no\n   \"needs repair\" prompt.\n"
- id: pptx-verify-02
  answer: "PowerPoint repairs are schema/consistency violations, commonly:\n- A row whose number of <a:tc>\
    \ elements differs from the number of\n  <a:gridCol>s (missing or extra cell after an insert/delete).\n\
    - An <a:tc> with no <a:txBody> (or a <a:txBody> with no <a:p>).\n- Merge markers inconsistent: gridSpan/hMerge\
    \ or rowSpan/vMerge that don't\n  add up across the row, a spanned cell missing, or merges written\
    \ as child\n  elements of a:tcPr instead of attributes of a:tc (and \"continue\" values,\n  which\
    \ don't exist).\n- Elements out of schema order (e.g. a:trPr must precede a:tc; a:txBody\n  before\
    \ a:tcPr ordering mistakes; tblPr properties in the wrong sequence).\n- Duplicate p:cNvPr ids — most\
    \ often from deepcopying a shape (or a table's\n  graphicFrame) without assigning a new id.\n- An\
    \ r:embed / r:id that points at a relationship that was dropped (the\n  classic drop_rel-of-an-image\
    \ bug), or a rels entry pointing to a missing\n  part.\n- A graphicFrame whose a:ext cy doesn't match\
    \ the sum of row heights is\n  usually tolerated, but a malformed a:tbl/a:gridCol widths sum or invalid\n\
    \  attribute values (negative sizes, out-of-range) also trigger repair.\n- Anything added to the package\
    \ without a matching [Content_Types].xml\n  entry or relationship.\n"
- id: pptx-verify-03
  answer: "Not necessarily. Deleting the <p:pic> element only removes the slide's\nreference to the image;\
    \ the image part itself (/ppt/media/imageN.png) and\nthe slide's relationship (ppt/slides/_rels/slideN.xml.rels\
    \ entry with\nType=.../image) remain in the package unless you remove them too. Most\ntools (including\
    \ python-pptx) do not garbage-collect unreferenced parts, so\nthe confidential image is still fully\
    \ recoverable from the .pptx — which is\nwhy \"delete the p:pic\" is not a privacy-safe removal.\n\
    \nCheck:\n1. Unzip the saved file and list ppt/media/ — the image file is still there\n   if its name\
    \ is present.\n2. grep every ppt/slides/slide*.xml for the rId of that image (and for the\n   image's\
    \ name) to confirm the slide no longer references it.\n3. grep ppt/slides/_rels/*.rels for the relationship\
    \ — if it is still\n   listed, the part is still reachable.\n4. Confirm no remaining //@r:embed or\
    \ //@r:link anywhere in the package\n   resolves to it (e.g. another slide or a layout/master may\
    \ use it).\n\nTo actually get rid of it: drop the relationship from every rels part that\nnames it,\
    \ ensure no r:embed/r:link anywhere references it, then remove the\n/ppt/media/imageN.png part from\
    \ the package, remove its Override/default\ncontent-type entry if needed, re-zip, and re-verify —\
    \ and remember the old\ncopies/backups also contain it.\n"
