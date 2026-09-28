- id: pptx-model-01
  answer: "A table on a slide is not a shape type of its own; it is a GraphicFrame\n(`p:graphicFrame`)\
    \ that wraps a DrawingML table. The structure is:\n\n  <p:graphicFrame>\n    <p:xfrm> ... </p:xfrm>\
    \                      # position/size of the frame\n    <a:graphic><a:graphicData\n         uri=\"\
    http://schemas.openxmlformats.org/drawingml/2006/table\">\n      <a:tbl>\n        <a:tblPr .../> \
    \                         # table style, banding flags\n        <a:tblGrid>\n          <a:gridCol\
    \ w=\"...\"/> ...              # ONE per column (width in EMU)\n        </a:tblGrid>\n        <a:tr\
    \ h=\"...\">                          # one per ROW (min height)\n          <a:tc gridSpan=\"..\"\
    \ rowSpan=\"..\" hMerge=\"1\" vMerge=\"1\">\n            <a:txBody><a:bodyPr/><a:lstStyle/>\n    \
    \          <a:p><a:r><a:rPr/><a:t>text</a:t></a:r></a:p>\n            </a:txBody>\n            <a:tcPr/>\
    \                           # cell fill/borders/margins\n          </a:tc> ...\n        </a:tr> ...\n\
    \      </a:tbl>\n    </a:graphicData></a:graphic>\n  </p:graphicFrame>\n\nSo columns live in `a:tblGrid/a:gridCol`\
    \ (the grid defines column count and\neach width), rows in repeated `a:tbl/a:tr`, cells in `a:tc`\
    \ inside each\n`a:tr`, and text in `a:tc/a:txBody/a:p/a:r/a:t` (bodyPr = insets/anchoring,\ntcPr =\
    \ fill/border). Cell order inside a row is positional and must line up\nwith the grid columns. In\
    \ python-pptx the frame is a GraphicFrame shape with\n`has_table == True`, exposing `.table` (Table),\
    \ `.rows`, `.columns`,\n`.cell(r,c)`.\n"
- id: pptx-model-02
  answer: 'DrawingML (and therefore python-pptx) uses EMU — English Metric Units.

    Conversions: 1 inch = 914400 EMU; 1 point = 12700 EMU; 1 cm = 360000 EMU;

    1 mm = 36000 EMU. python-pptx exposes Inches(), Pt(), Cm(), Emu() helpers,

    and everything is stored internally as an Emu int.


    A default 4:3 deck in the python-pptx default template is 10 x 7.5 in =

    9144000 x 6858000 EMU. A default 16:9 deck (the modern PowerPoint default)

    is 13.333 x 7.5 in = 12192000 x 6858000 EMU, i.e. 960 x 540 pt or

    33.87 x 19.05 cm. So for 16:9: width 12192000 EMU (13-1/3 in), height

    6858000 EMU (7.5 in). Note row heights (`a:tr/@h`), column widths

    (`a:gridCol/@w`) and frame extents are all in EMU too.

    '
- id: pptx-model-03
  answer: 'It is almost certainly not on the slide at all: it lives on the slide

    LAYOUT (`slide.slide_layout.shapes`) or on the slide MASTER

    (`slide.slide_layout.slide_master.shapes`), or as the layout/master

    background (`p:bg`) / a placeholder that the slide inherits but does not

    define. `slide.shapes` iterates only the shapes physically defined in the

    slide part, so inherited/placeholder content does not appear there. Access

    it via the layout/master, e.g. `slide.slide_layout.shapes` or

    `slide.slide_layout.slide_master.shapes` (or `prs.slide_masters`).


    Consequences of editing it there: the change applies to every slide using

    that layout (or every slide, if on the master) — you cannot tweak one

    slide''s logo. Deleting it removes it everywhere; moving/resizing it shifts

    it on all inheriting slides. It also sits behind (below) slide-local

    shapes in z-order, and python-pptx has limited support for master/layout

    shape editing (some inherited placeholder shapes have no run/format API).

    If instead you add a copy on the slide, you duplicate the logo and may get

    overlap/mismatch with the inherited one. The clean fix is to edit the

    layout/master once, or add an explicit logo shape to each slide if only

    some slides should differ.

    '
- id: pptx-locate-01
  answer: "```python\nfrom pptx import Presentation\n\nprs = Presentation(\"deck.pptx\")\nslide = prs.slides[2]\
    \                       # slide 3 is index 2\n\ntable = None\nfor shape in slide.shapes:\n    if shape.has_table:\
    \                     # GraphicFrame with an a:tbl\n        table = shape.table\n        break\n\n\
    target = None\nfor row in table.rows:\n    if row.cells[0].text.strip() == \"Gadget D\":   # or scan\
    \ all cells\n        target = row\n        break\n```\nNotes: `has_table` is the reliable test (a\
    \ table is a GraphicFrame, not a\n`p:sp`). If \"Gadget D\" is not in column 0, scan `for cell in row.cells`.\n\
    Be aware `row.cells` repeats the same Cell object for merged cells, so if\nthe item column can be\
    \ spanned, use `cell.text` plus dedup or index by the\ntable grid. `row.cells[i].text` returns the\
    \ concatenated text of the cell's\nparagraphs.\n"
- id: pptx-locate-02
  answer: "A horizontal merge (Total label spanning the first two columns) is\nrepresented as: the ORIGIN\
    \ cell carries `gridSpan=\"2\"` on its `a:tc`, and\nthe second physical cell still exists but is marked\
    \ `hMerge=\"1\"` (it has no\ncontent of its own). Vertical merges use `rowSpan=\"N\"` on the origin\
    \ and\n`vMerge=\"1\"` on the covered cells below. python-pptx does not expose a\nmerge/unmerge API\
    \ — you read `cell.span_width` / `cell.span_height` and\nmanipulate the XML for changes.\n\nWatch-outs\
    \ when editing/deleting around a merge:\n- The hidden merged cell is still present in the XML and\
    \ in `row.cells`;\n  iterating cells returns the SAME Cell object multiple times for a span.\n  Writing\
    \ text to any address in the span writes to the origin cell.\n- `gridSpan`/`rowSpan` count grid columns/rows,\
    \ so column indices and\n  positional cell <-> gridCol alignment shift around a span. Inserting or\n\
    \  deleting a `gridCol` or `a:tc` must keep every row's cell count and spans\n  consistent or PowerPoint\
    \ repairs/rejects the file.\n- Deleting a row that participates in a vertical merge requires clearing\
    \ or\n  decrementing `rowSpan`/removing `vMerge` cells, otherwise you leave a\n  dangling `vMerge=\"\
    1\"` with no origin (or an over-long `rowSpan`).\n- Editing the origin cell's text overwrites the\
    \ whole span; the covered\n  cells must stay empty. Reapplying fills to \"both\" cells is meaningless\n\
    \  since they render as one.\n"
- id: pptx-rowdel-01
  answer: 'No. python-pptx has no `table.rows.remove()`, no `_Row.delete()`, and no

    row/column deletion API (the same is true for columns). The public API only

    lets you add a row (`table.add_row()`) and read/format rows.


    To delete you must go to lxml and remove the `<a:tr>` element yourself:

    ```python

    from copy import deepcopy

    tbl = table._tbl            # CT_Table, the <a:tbl> element

    tr = table.rows[idx]._tr    # the <a:tr> to drop

    tbl.remove(tr)

    ```

    You then usually must also fix the graphicFrame''s height (see pptx-rowdel-02).

    Remember to account for merges: if the removed row was the origin of a

    `rowSpan`, decrement/clear the span; if it contained a `vMerge="1"` cell,

    removing it is fine only if the origin''s `rowSpan` is reduced accordingly.

    `a:tblGrid` (columns) is unaffected by row deletion.

    '
- id: pptx-rowdel-02
  answer: 'Yes, it matters. The `<a:xfrm>/<a:ext cy>` on the graphicFrame declares the

    frame''s bounding box, and PowerPoint uses it for selection, hit-testing,

    text auto-grow decisions and re-layout. If you remove a row but leave the

    frame height at the old value, the frame is taller than the remaining rows;

    PowerPoint may shrink/repair it on open, show empty space/border below the

    table, misplace the selection outline, or place following content wrongly.


    Fix: after removing the `a:tr`, recompute the height and write it back.

    The natural value is the sum of the remaining rows'' `h` attributes (the

    table''s `a:tblPr`/row heights), or the old ext cy minus the removed row''s

    height. In python-pptx:

    ```python

    from pptx.util import Emu

    gf = table_shape                       # the GraphicFrame shape

    removed_h = Emu(int(removed_tr.get(''h'')))   # if a:tr/@h is set

    gf.height = gf.height - removed_h       # or set to sum(remaining row heights)

    new_cy = int(gf.height)                # ensure xfrm ext.cy matches

    ```

    `gf.height = ...` updates `a:xfrm/a:ext/@cy`. If rows have no explicit `h`

    (auto), set the frame height to the sum of the desired row heights you do

    set on each `a:tr`, since PowerPoint treats `a:tr/@h` as a minimum and may

    grow the table anyway.

    '
- id: pptx-rowdel-03
  answer: "It depends on HOW the alternating shading was applied.\n\n- If the deck relies on a TABLE STYLE\
    \ with banded rows: `a:tblPr` has\n  `bandRow=\"1\"` (and often `firstRow=\"1\"`), and the actual\
    \ colours come\n  from the style part referenced by `a:tblPr/a:tableStyleId` (or the\n  theme's default\
    \ table style). PowerPoint computes banding dynamically\n  from row POSITION, so after you delete\
    \ a middle row the remaining rows\n  simply re-band and still alternate — no fix needed.\n- If the\
    \ deck has HARD-CODED per-cell fills (`a:tc/a:tcPr/a:solidFill`)\n  painted row by row, the fills\
    \ are literal and do not move with the rows.\n  Delete a middle row and the two rows that become adjacent\
    \ both keep their\n  old colours, so they can end up the same → broken alternation. You must\n  re-apply\
    \ the fills (or convert to the banded style) after the delete.\n\nSo the difference is dynamic style-based\
    \ banding vs. explicit static cell\nfills (often produced by \"paste as values\", hand editing, or\
    \ a generator\nthat wrote fills instead of setting `bandRow`).\n"
- id: pptx-rowdel-04
  answer: "Both are cosmetic hacks that leave the row's data in the document.\n\n- A white rectangle is\
    \ a separate shape that does not belong to the table:\n  it does not move/resize with the table, breaks\
    \ selection and hit-testing,\n  sits on top of cell borders (so the grid lines around it look wrong),\n\
    \  prints or exports unpredictably, and leaves the actual `a:tr` intact.\n- White text plus near-zero\
    \ height still leaves the `<a:tr>`/`<a:tc>` and\n  its text in the XML. The text stays searchable,\
    \ copyable, accessible to\n  screen readers, and visible to any extractor/summary/report that reads\n\
    \  the table; `a:tr/@h` is a MINIMUM, so PowerPoint may refuse the near-zero\n  height and grow the\
    \ row to fit the font, making white text reappear or\n  clip/overflow. It also desynchronises the\
    \ graphicFrame height and breaks\n  row indexing.\n\nCorrect approach: physically delete the `a:tr`\
    \ element (and fix frame\nheight/merges), so nothing downstream sees the row.\n"
- id: pptx-cell-01
  answer: "`cell.text = \"SKU\"` is a whole-text-frame replacement. The setter clears\nthe cell's `<a:txBody>`\
    \ contents and rebuilds it with a single paragraph\ncontaining a single run. That new run has no explicit\
    \ run properties, so\nthe original run-level formatting (bold, 14 pt size, white colour, font)\nis\
    \ discarded; the text falls back to inherited/placeholder/default\nformatting. Any per-paragraph formatting\
    \ and multiple runs are also lost.\n\nWays to keep the formatting:\n- Mutate the existing run's text\
    \ instead of the whole frame:\n  ```python\n  run = cell.text_frame.paragraphs[0].runs[0]\n  run.text\
    \ = \"SKU\"\n  ```\n  This preserves the existing `<a:rPr>`.\n- Or set only the paragraph text/patch\
    \ the first run.\n- Or re-apply formatting after assigning `cell.text`:\n  ```python\n  cell.text\
    \ = \"SKU\"\n  r = cell.text_frame.paragraphs[0].runs[0]\n  r.font.bold = True\n  r.font.size = Pt(14)\n\
    \  r.font.color.rgb = RGBColor(0xFF, 0xFF, 0xFF)\n  ```\nThe same issue applies to `text_frame.text\
    \ = ...`. Prefer editing the\nexisting run when possible.\n"
- id: pptx-cell-02
  answer: "Column widths come from the fixed `a:gridCol/@w`; replacing the text with\na much longer name\
    \ makes the text wrap inside the same column width. Row\nheight set by `a:tr/@h` is a MINIMUM, so\
    \ PowerPoint auto-grows the row to\nfit the wrapped lines. Result: the row gets taller (pushing the\
    \ rest of the\ntable down), the table can extend past the slide bottom/footer or overlap\nother content,\
    \ and the graphicFrame's declared height becomes inconsistent\nwith the drawn table. If autofit/word-wrap\
    \ is off, text can overflow or be\nclipped.\n\nHow to control it:\n- Set a fixed column width: `table.columns[i].width\
    \ = Inches(...)`.\n- Widen the relevant column (and narrow neighbours) so the name fits on\n  fewer\
    \ lines; keep `sum(widths)` <= available frame width.\n- Reduce the font size of that cell's run (`run.font.size\
    \ = Pt(n)`).\n- Control wrapping with `cell.text_frame.word_wrap = True/False`; set\n  margins (`cell.margin_left`\
    \ etc.) and vertical anchor.\n- Set the row height (`table.rows[i].height`) as a minimum and re-set\
    \ the\n  graphicFrame height to match, but note PowerPoint may still grow the row\n  if the text needs\
    \ more lines.\n- Or shorten/truncate the displayed name if the column must stay narrow.\n"
- id: pptx-cell-03
  answer: 'The Qty is an input to the calculation, so changing it invalidates every

    derived value in the deck. At minimum you must recompute and rewrite:

    - the row''s line total (Qty x unit price),

    - the subtotal / sum of line totals,

    - any discount applied to the subtotal,

    - tax/VAT computed on the discounted subtotal,

    - shipping/adjustment if conditional,

    - the grand total / "Total" row (and any amount-in-words or currency note),

    - any summary KPI, callout, or caption that repeats the total or count,

    - any chart or table that plots quantities or totals,

    - any per-row or running totals below the changed row (row order matters),

    - and any linked/consistent figures elsewhere in the deck.


    Also re-check formatting (thousands separators, currency symbol, number

    format), row height/width if the new value changes wrapping, and that no

    stale hard-coded number is left. The key point: never edit Qty in

    isolation — all dependent aggregates and repeating figures must be updated

    consistently (ideally by recomputing from the underlying data rather than

    patching individual cells).

    '
- id: pptx-relayout-01
  answer: "python-pptx cannot insert a row; you clone an existing `a:tr` and insert it\nat the XML level\
    \ so it inherits the same formatting.\n\n```python\nimport copy\n\ntable = table_shape.table\nrows\
    \ = table.rows\n\n# locate indices: insert AFTER \"Service G\", i.e. before the Total row\nsvc_idx\
    \ = next(i for i, r in enumerate(rows)\n               if r.cells[0].text.strip() == \"Service G\"\
    )\ntotal_idx = next(i for i, r in enumerate(rows)\n                 if r.cells[0].text.strip() ==\
    \ \"Total\")\n\n# deep-copy a data row (not the Total row) to preserve tcPr/rPr/banding\ntemplate_tr\
    \ = copy.deepcopy(rows[svc_idx]._tr)\n\n# clean merge markers that would be wrong at the new position\n\
    for tc in template_tr.findall(\n        '{http://schemas.openxmlformats.org/drawingml/2006/main}tc'):\n\
    \    for attr in ('gridSpan', 'rowSpan', 'hMerge', 'vMerge'):\n        tc.attrib.pop(attr, None)\n\
    \n# insert immediately before the Total row\nrows[total_idx]._tr.addprevious(template_tr)\n\n# write\
    \ the new values into the inserted row's cells\nnew_tr = rows[total_idx - 1]      # may need to re-fetch\
    \ table.rows\nnew_tr.cells[0].text = \"Service H\"\n# ... fill other cells ...\n\n# grow the graphicFrame\
    \ by the new row height\nfrom pptx.util import Emu\nh = Emu(int(template_tr.get('h', 0))) or table_shape.height\
    \  # fallback\ntable_shape.height = table_shape.height + h\n```\n\nKeep the banding consistent: either\
    \ rely on `bandRow=\"1\"` (dynamic, colours\nrecompute) or re-apply alternating fills to the rows\
    \ now following the\ninsert. Also re-check `rowSpan`/`vMerge` on neighbouring rows if the table\n\
    uses merges.\n"
- id: pptx-relayout-02
  answer: "A column is defined twice and both must be updated in lock-step, because\ncell position in\
    \ a row is positional against the grid:\n\n1. Insert a new `<a:gridCol w=\"...\"/>` into `<a:tblGrid>`\
    \ at the column\n   index (right after the Item column's `gridCol`). This sets the new\n   column's\
    \ width and increments the column count.\n2. Insert a new `<a:tc>` into EVERY `<a:tr>` at the SAME\
    \ index (so the\n   new cell sits right after the Item cell in every row), giving it the\n   same\
    \ `a:tcPr`/`a:txBody` structure (bold header for the header row,\n   normal cells for data rows).\
    \ Missing it in any row desynchronises\n   cells from the grid and PowerPoint will repair the file.\n\
    3. Update the graphicFrame's `a:xfrm/a:ext/@cx` to the new total width\n   (`xfrm` is on the graphicFrame,\
    \ and in python-pptx `shape.width = ...`\n   writes `ext/@cx`). The table's effective width is the\
    \ sum of\n   `a:gridCol/@w`, so the frame width must equal that sum or PowerPoint\n   snaps/repairs\
    \ it.\n4. Handle merges: if any row has `gridSpan`, those spans (and the hidden\n   `hMerge` cells)\
    \ must be adjusted for the extra column, or the Total\n   label spanning two columns will no longer\
    \ line up.\n\nKeeping it on the slide: the original table already \"spans nearly the whole\nslide\
    \ width\", so adding width would overflow. Therefore the new column\nmust be paid for by shrinking\
    \ others so the total stays within the slide\n(<= 12192000 EMU for 16:9) and within the frame's left\
    \ offset + width. Two\napproaches: subtract the new column's width from the Item column (or\nproportionally\
    \ from all data columns), or scale all widths so\n`sum(new gridCol widths) == old total width` (or\
    \ <= slide width minus\nmargins). Concretely:\n```python\nfrom pptx.util import Emu, Inches\nslide_w\
    \ = prs.slide_width          # 12192000 for 16:9\nmax_w = slide_w - table_shape.left - Inches(0.5)\
    \   # right margin\n# choose new col width, then reduce one/more existing gridCol @w values\n# so\
    \ sum(col widths) <= max_w, then set table_shape.width = sum(col widths)\n```\nVerify visually that\
    \ text still fits after narrowing and that the frame's\n`off x + ext cx` does not exceed the slide\
    \ width, otherwise the table runs\noff the right edge (python-pptx will not auto-shift it).\n"
- id: pptx-tblins-01
  answer: "Use `shapes.add_table(rows, cols, left, top, width, height)` on the slide\n(returns a GraphicFrame;\
    \ the table is `gf.table`). The key is choosing the\nfree rectangle first: iterate `slide.shapes`,\
    \ read each shape's `.left`,\n`.top`, `.width`, `.height`, and compute occupied regions. Then pick\
    \ a\nposition, e.g. below the lowest existing shape:\n    from pptx.util import Inches, Emu\n    max_bottom\
    \ = max((s.top + s.height) for s in slide.shapes)\n    left, top = Inches(1), Emu(max_bottom) + Inches(0.25)\n\
    \    gf = slide.shapes.add_table(3, 2, left, top, Inches(6), Inches(2))\nor place it in the largest\
    \ empty area / beside existing content. Always\nconvert to EMU (Inches/Emu/Pt), keep width/height\
    \ within slide bounds\n(`prs.slide_width`, `prs.slide_height`), and check the computed rectangle\n\
    does not intersect any existing shape before creating it. Populate via\n`table.cell(r, c).text = ...`.\
    \ If you only need to position relative to a\nplaceholder, read its left/top/width/height instead\
    \ (see tblins-03).\n"
- id: pptx-tblins-02
  answer: "`add_table` applies the default table style (`{5C22544A-7EE6-4342-B048-\n85DCB1A3C9A4}`, a\
    \ blue \"Medium Style 2 - Accent 1\"). The style is stored in\nthe table's XML as `<a:tblPr><a:tableStyleId>GUID</a:tableStyleId></a:tblPr>`.\n\
    python-pptx exposes no high-level style setter, so either copy the\n`tableStyleId` GUID from an existing\
    \ table in the deck, or apply explicit\nformatting to cells. To copy:\n    existing = <some existing\
    \ table's graphic frame>\n    style_id = existing.table._tbl.tblPr.find(qn('a:tableStyleId'))\n  \
    \  new_tbl = new_gf.table._tbl\n    new_tbl.tblPr.append(copy.deepcopy(style_id))   # ensure only\
    \ one\nor directly set/inject the `<a:tableStyleId>` child (namespace\n`http://schemas.openxmlformats.org/drawingml/2006/main`).\
    \ You may also turn\nbanding/first-row flags on/off with `table.first_row`, `table.horz_banding`,\n\
    etc., but those only toggle banding within whatever style is applied. If no\nmatching built-in style\
    \ exists, set per-cell fills, borders (`a:tcPr` with\n`a:lnL/R/T/B`), and text fonts/colors explicitly.\
    \ Match the existing deck's\nstyle GUID whenever possible rather than rebuilding formatting.\n"
- id: pptx-tblins-03
  answer: "There is no `placeholder.insert_table()`. The idiomatic approach is to read\nthe empty content\
    \ placeholder's geometry, create a table at exactly that\nposition/size, then remove the placeholder\
    \ element so it doesn't show the\n\"Click to add text\" prompt:\n    ph = slide.placeholders[idx]\
    \          # or find by .placeholder_format.type\n    left, top = ph.left, ph.top\n    width, height\
    \ = ph.width, ph.height\n    gf = slide.shapes.add_table(rows, cols, left, top, width, height)\n \
    \   ph._element.getparent().remove(ph._element)   # drop the empty placeholder\n(Alternatively clone\
    \ its `p:sp` geometry into the table frame, but the\nabove is the common pattern.) If you want the\
    \ table to inherit the\nplaceholder's position only, this preserves layout. Note the placeholder\n\
    must actually be empty; text placeholders are for text, not tables. Some\npeople instead `add_table`\
    \ first and then set its `left/top/width/height`\nto the placeholder's values and delete the placeholder.\n"
- id: pptx-imgrep-01
  answer: "python-pptx has no `picture.replace()`. Preferred way to keep position,\nsize, and z-order:\
    \ swap the image relationship the shape already points at,\nrather than deleting and re-adding the\
    \ shape.\n1. Find the picture and record `left, top, width, height` and its index in\n   the shape\
    \ tree for safety.\n2. Add/get a new image part and get its rId, e.g. via\n   `slide.part.get_or_add_image_part(image_file)`\
    \ (or add a temporary\n   picture then take its rId).\n3. Rewrite the picture's `<a:blip r:embed=\"\
    ...\">` to the new rId:\n       sp = picture._element\n       blip = sp.find('.//' + qn('a:blip'))\n\
    \       blip.set(qn('r:embed'), new_rId)\n4. Drop the old relationship only if nothing else references\
    \ it.\nBecause you edit the existing `p:pic` in place, the stacking order in the\n`p:spTree` is untouched.\
    \ Alternative (only when the image part is unique to\nthis picture): overwrite the ImagePart bytes\n\
    (`slide.part.related_part(old_rId)._blob = new_bytes`) — but beware shared\nparts (see imgrep-02).\
    \ Never delete the shape and re-add it in a loop, as\nthat changes z-order and often loses effects/alt\
    \ text.\n"
- id: pptx-imgrep-02
  answer: 'Because python-pptx (and PowerPoint packages generally) deduplicate images:

    identical image bytes are stored once as a single `ImagePart` under

    `ppt/media/`, and both pictures reference that one part (via their slide''s

    relationships). `get_or_add_image_part` keys on the image SHA1, so slides 1

    and 7 share the same media part. Overwriting the part''s bytes therefore

    changes every picture that references it — both logos. To change only one,

    create a *new* image part for the new bytes (new rId), point only slide 1''s

    blip at it, and leave slide 7''s blip/rel on the original part. Equally,

    never mutate a shared ImagePart in place; treat image parts as immutable.

    '
- id: pptx-imgrep-03
  answer: '`Part.drop_rel(rId)` is a low-level operation: it removes the relationship

    identified by that rId from the part''s rels collection. It does not

    reliably reference-count across all usages, and because identical images are

    deduplicated, two pictures can share the same image part — often via the

    same single relationship/rId on the slide. When you drop that rId, the other

    picture''s `<a:blip r:embed="...">` now points at a rId that no longer

    exists, so PowerPoint can''t resolve the image and it breaks. The fix is to

    check the whole slide XML (all shapes, including nested groups) for

    remaining references to that rId before dropping it, and only drop when

    there are none. Use `part.rels` to inspect and iterate

    `part.rels[rId].target_part`; don''t call `drop_rel` blindly after deleting

    one shape. If the image is still used, either keep the rel or give the

    replacement its own part/rel.

    '
- id: pptx-imgins-01
  answer: "Add the picture at the right edge with only width specified; python-pptx\npreserves aspect\
    \ ratio when `height` is omitted:\n    from pptx.util import Inches, Emu\n    sw, sh = prs.slide_width,\
    \ prs.slide_height\n    pic = slide.shapes.add_picture('signature.png',\n                        \
    \           sw - Inches(2), sh,\n                                   width=Inches(2))\n    # height\
    \ is auto-computed from the image aspect ratio\n    margin = Inches(0.25)\n    pic.left = sw - pic.width\
    \ - margin\n    pic.top  = sh - pic.height - margin\nDetermine the image's intrinsic height first\
    \ if you want to position before\nadding (e.g. read `pic.height` after adding, or get the native size\
    \ from the\nimage part / Pillow). To avoid covering content, compute the free area from\nexisting\
    \ shapes' bounding boxes (like tblins-01) and place the signature in\nthe bottom-right margin/empty\
    \ region, or above the lowest overlapping shape.\nDo not pass both width and height, which would distort\
    \ the aspect ratio.\n"
- id: pptx-imgins-02
  answer: 'Picture placeholders crop to fill: `placeholder.insert_picture(path)` sizes

    the image to fill the placeholder''s fixed frame (which has a fixed aspect

    ratio, often landscape/square) and crops the overflow. A portrait photo

    therefore loses its top/bottom (or sides) because the visible area is the

    placeholder rectangle, not the full image. It is a crop, not data loss — the

    full image part is still in the package. Fixes: (a) don''t use the

    picture-placeholder insert; add a normal picture and size to fit / use

    `add_picture` with the desired dimensions; (b) change the placeholder''s

    width/height to match the portrait aspect ratio before inserting; or

    (c) clear the crop on the resulting PlaceholderPicture

    (`pic.crop_left = pic.crop_right = pic.crop_top = pic.crop_bottom = 0`)

    and resize the shape so the whole image shows.

    '
- id: pptx-legacy-01
  answer: "No. python-pptx only handles OOXML packages (.pptx/.pptm/.potx — the ZIP/OPC\nformat). The\
    \ binary PowerPoint 97–2003 `.ppt` format is not supported and\nopening it fails (PackageNotFoundError\
    \ / \"not a ZIP archive\"). Workflow:\nconvert first, then edit. Typical headless conversion:\n  \
    \  soffice --headless --convert-to pptx --outdir out deck.ppt\n(or open in PowerPoint and \"Save As\
    \ .pptx\"). Then verify the converted\n`.pptx` opens without a repair prompt, check layout/fonts/tables\
    \ survived,\nand only then open it with `Presentation('deck.pptx')`, edit, and save.\nThere is no\
    \ round-trip — you cannot write back to `.ppt`; deliver `.pptx`\n(or have the user re-save). Warn\
    \ that conversion can change formatting.\n"
- id: pptx-legacy-02
  answer: 'Before editing: confirm the conversion succeeded and produced a valid

    `.pptx`; keep the original `.ppt` untouched as the source of truth; open the

    `.pptx` once in real PowerPoint to confirm no "repair" prompt; check slide

    count/order, layouts/masters, fonts (LibreOffice often substitutes fonts),

    tables/charts/SmartArt/OLE, embedded images, hyperlinks, notes, and

    animations/transitions (frequently altered or dropped). Inspect structure

    with python-pptx (slide count, shapes) before mutating.

    After editing: re-open the saved file in PowerPoint, confirm no repair

    prompt, visually compare against the original, and verify the specific

    change (the table). Check the media/relationships didn''t change

    unexpectedly.

    Tell the user: binary `.ppt` can''t be read/written by python-pptx, so it was

    converted (e.g. via LibreOffice) to `.pptx`; conversion may alter fonts,

    layout, and effects, and some legacy/animation features may be lost or

    changed; the original `.ppt` is preserved; review the result, and there is

    no `.ppt` output — only `.pptx`. Mention macro-enabled decks become

    `.pptm`-relevant/unsupported accordingly.

    '
- id: pptx-verify-01
  answer: "Never overwrite the only copy. Procedure:\n1. Back up the original (`deck.pptx` -> `deck.backup.pptx`)\
    \ or write to a\n   new name.\n2. `prs = Presentation('deck.pptx')`, make edits, `prs.save('deck.pptx')`\n\
    \   (or a new path).\n3. Reopen the saved file with python-pptx (`Presentation(saved)`) to prove\n\
    \    it still parses and that the expected change is present (e.g. check\n    `table.cell(0,0).text`).\n\
    4. Validate the package: `zipfile.ZipFile(saved).testzip()` and confirm the\n    `[Content_Types].xml`\
    \ and all `.rels` parse; optionally load/re-zip to\n    check XML well-formedness.\n5. Open the saved\
    \ file in real PowerPoint (and/or LibreOffice) and confirm\n    no \"needs to be repaired\" prompt\
    \ and that it renders correctly.\n6. Diff against the backup for unintended changes (slide count,\
    \ media,\n    relationships); keep the backup until the user confirms.\nAvoid saving in place during\
    \ development; use a temp output until verified.\n"
- id: pptx-verify-02
  answer: "Most common causes of \"needs to be repaired\" after hand-editing table XML:\n- Schema element-order\
    \ violations. DrawingML is order-sensitive: `a:tbl`\n  children must be `a:tblPr?`, `a:tblGrid`, `a:tr+`;\
    \ inside `a:tr` only\n  `a:tc+`; inside `a:tc` order is `a:txBody?`/`a:tcPr?` (and `a:tcPr` after\n\
    \  `a:txBody`). Appending in the wrong place triggers repair.\n- Grid/row mismatch: number of `a:tblGrid/a:gridCol`\
    \ must equal the cell\n  count per row and match `a:tr/a:tc` counts and any `gridSpan`.\n- Dangling\
    \ or duplicated relationship IDs (`r:embed`, `r:id`) or references\n  to rIds that no longer exist.\n\
    - Namespace problems: undeclared/incorrect prefixes, missing `a:`/`r:`\n  declarations, or editing\
    \ with the wrong namespace URI.\n- Malformed XML: unclosed/mismatched tags, illegal characters, unescaped\n\
    \  `&`/`<`, extra attributes, duplicate attributes, bad text encoding/BOM.\n- Writing XML that isn't\
    \ the expected part type, or breaking the OPC\n  structure/content types.\n- Using `.text =` on a\
    \ cell can flatten runs and strip required run\n  properties; better to edit `a:t` inside `a:r/a:rPr`\
    \ carefully.\nValidate by reparsing with lxml and (ideally) opening in PowerPoint.\n"
- id: pptx-verify-03
  answer: 'No — deleting the `p:pic` element only removes the *reference* on the

    slide. The image part (`ppt/media/imageN.*`) and, usually, the slide''s

    relationship to it remain in the package, so the confidential bytes are

    still inside the `.pptx` and recoverable by unzipping. On save python-pptx

    serializes parts reachable through relationships, so the media stays unless

    the relevant relationship is also removed and no other part/slide references

    that image. Steps to truly remove it: drop the slide''s relationship to the

    image part (only if no remaining shape, including nested groups, references

    that rId), or rewrite the package to exclude the `ppt/media` entry and its

    rels/`[Content_Types]` entries. Beware deduplication: another slide may

    share the same image part, in which case you must create a separate copy

    first. How to check: treat the `.pptx` as a ZIP — list `ppt/media/`, grep

    the slide `.rels` (and the whole archive) for the image filename/target, or

    use python-pptx to inspect `slide.part.rels` and `related_part(rId).blob`,

    and hash the bytes against the known confidential image. Also check

    `docProps/thumbnail.jpeg` and any embedded/linked copies, not just the

    slide. If any copy remains, the image is still in the file.

    '
