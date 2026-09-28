- id: pptx-model-01
  answer: "A table is not a first-class shape in PPTX the way it is in DOCX. On a slide it is a\n**graphic\
    \ frame** (`p:graphicFrame`) inside the slide's `p:spTree`, whose graphic payload\ncarries a DrawingML\
    \ table:\n\n```xml\n<p:graphicFrame>\n  <p:nvGraphicFramePr>...</p:nvGraphicFramePr>\n  <p:xfrm><a:off\
    \ x=\"..\" y=\"..\"/><a:ext cx=\"..\" cy=\"..\"/></p:xfrm>\n  <a:graphic>\n    <a:graphicData uri=\"\
    http://schemas.openxmlformats.org/drawingml/2006/table\">\n      <a:tbl>\n        <a:tblPr firstRow=\"\
    1\" bandRow=\"1\"><a:tableStyleId>{GUID}</a:tableStyleId></a:tblPr>\n        <a:tblGrid>\n       \
    \   <a:gridCol w=\"2286000\"/>   <!-- COLUMN: one per column, w = width -->\n          ...\n     \
    \   </a:tblGrid>\n        <a:tr h=\"370840\">          <!-- ROW: h = height (a MINIMUM height) -->\n\
    \          <a:tc>                  <!-- CELL (one a:tc per cell in the row) -->\n            <a:txBody>\
    \            <!-- CELL TEXT BODY -->\n              <a:bodyPr wrap=\"square\"/>\n              <a:lstStyle/>\n\
    \              <a:p>                <!-- paragraph -->\n                <a:pPr algn=\"ctr\"/>\n  \
    \              <a:r>              <!-- run -->\n                  <a:rPr lang=\"en-US\" sz=\"1400\"\
    \ b=\"1\">\n                    <a:solidFill><a:srgbClr val=\"FFFFFF\"/></a:solidFill>\n         \
    \         </a:rPr>\n                  <a:t>SKU</a:t>   <!-- the literal characters -->\n         \
    \       </a:r>\n                <a:endParaRPr .../>\n              </a:p>\n            </a:txBody>\n\
    \            <a:tcPr marL=\"..\" anchor=\"ctr\"><a:solidFill>...</a:solidFill></a:tcPr>\n        \
    \  </a:tc>\n          ...\n        </a:tr>\n      </a:tbl>\n    </a:graphicData>\n  </a:graphic>\n\
    </p:graphicFrame>\n```\n\nSo: columns = `a:tblGrid/a:gridCol/@w`; rows = `a:tbl/@a:tr` with `@h`;\
    \ cells = `a:tc`\nchildren of each `a:tr`; text = `a:tc/a:txBody/a:p/a:r/a:t`. Shading/merges live\
    \ in\n`a:tcPr` and on `a:tc`; overall look may come from `a:tblPr/@a:tableStyleId` pointing into\n\
    `ppt/tableStyles.xml`. The frame's `p:xfrm/a:ext` is a separate, parallel size that must\nagree with\
    \ the sum of column widths and row heights.\n"
- id: pptx-model-02
  answer: 'DrawingML (and therefore python-pptx) uses **English Metric Units (EMU)**, signed 32-bit

    integers. Conversions:


    - 914400 EMU = 1 inch

    - 12700 EMU = 1 point (72 pt/inch)

    - 360000 EMU = 1 cm

    - 9525 EMU = 1 px at 96 dpi


    In python-pptx, every length is a `Length` (an `int` subclass) with `.emu`, `.inches`,

    `.cm`, `.pt`, `.mm`. You can pass a plain `int` (EMU) or an `Emu`/`Inches`/`Pt`/`Cm`

    object; floats are rounded. Setting `shape.left = Inches(1)` and reading

    `shape.width.pt` both work. Note python-pptx''s own default template

    (`Presentation()` with no argument) is **4:3**, 10 x 7.5 in = 9144000 x 6858000 EMU.


    Default **16:9** deck ("Widescreen" / On-screen Show 16:9 in modern PowerPoint):

    13.333 in x 7.5 in = **12192000 x 6858000 EMU** (540 pt tall). The older "On-screen Show

    (16:9)" 10 x 5.625 in variant is 9144000 x 5143500 EMU. Always read the real values from

    `prs.slide_width` / `prs.slide_height` rather than hardcoding them.

    '
- id: pptx-model-03
  answer: "It is not on the slides — it is on the **slide layout** (`slide.slide_layout`) or the\n**slide\
    \ master** (`slide.slide_layout.slide_master`). `slide.shapes` enumerates only shapes\nphysically\
    \ present in that slide's own `p:spTree` part. Anything drawn on the layout or\nmaster is painted\
    \ underneath every slide that inherits it, and is invisible to python-pptx\non the slide. (Note the\
    \ partial exception: a *placeholder* defined on the layout does\nappear on the slide as a placeholder\
    \ shape when the slide inherits it — but a plain\npicture, shape or group on the layout/master does\
    \ not.)\n\nReach it via `slide.slide_layout.shapes` or `slide.slide_layout.slide_master.shapes`; to\n\
    modify, you must poke the XML (`shape._element`) because python-pptx has no public\nadd/edit API for\
    \ master and layout shapes (the master part does get saved with the\npackage, so edits do persist).\n\
    \nConsequences of editing there:\n\n- It changes **every** slide using that layout/master at once\
    \ — the blast radius is the\n  whole deck, not one slide.\n- A slide that has its own copy of the\
    \ image (or a `p:pic` overriding the inherited one)\n  will not change, so you can get inconsistent\
    \ branding without noticing.\n- Layouts are often shared by many slides for different sections; \"\
    fixing\" the logo on a\n  title layout can silently alter content layouts too.\n- Non-placeholder\
    \ master/layout graphics are not selectable in normal view, so a human\n  checking the slide in PowerPoint\
    \ may not find the thing you edited.\n- Editing the master through python-pptx is XML surgery; if\
    \ you produce invalid XML\n  (e.g. break `p:spTree` ordering) PowerPoint will show a repair prompt.\n\
    - If the deck is built from a template that others reuse, your master change propagates to\n  every\
    \ future deck made from it.\n\nPractical advice: decide explicitly whether the logo is per-slide content\
    \ (copy it onto\neach slide), layout-level chrome (edit the layout), or master chrome (edit the master),\n\
    and if in doubt check `for part in (slide, slide.slide_layout, slide.slide_layout.slide_master)`.\n"
- id: pptx-locate-01
  answer: "```python\nfrom pptx import Presentation\nfrom pptx.enum.shapes import MSO_SHAPE_TYPE\n\nprs\
    \ = Presentation(\"deck.pptx\")\nslide = prs.slides[2]            # slide 3 (0-based index)\n\ntables\
    \ = [sh for sh in slide.shapes if sh.has_table]\n# or: if sh.shape_type == MSO_SHAPE_TYPE.TABLE (==\
    \ 19)\n\ngf = tables[0]                   # the GraphicFrame\ntable = gf.table                 # _Table:\
    \ .rows, .columns, .cell(r, c)\n\nfor r_idx, row in enumerate(table.rows):\n    for c_idx, cell in\
    \ enumerate(row.cells):\n        if cell.text.strip() == \"Gadget D\":\n            print(r_idx, c_idx)\
    \  # e.g. 4 1\n            target_row, target_col = r_idx, c_idx\n```\n\nNotes and alternatives:\n\
    \n- `row.cells[i]` indexes the *physical* `a:tc` elements; if the table has merged cells use\n  `table.cell(r,\
    \ c)` for grid coordinates, and check `cell.is_merge_origin` /\n  `cell.is_spanned` before writing.\n\
    - `cell.text` returns the concatenated text of the cell's text frame, including soft line\n  breaks,\
    \ so compare with `.strip()` and tolerate trailing whitespace.\n- Search for a substring instead of\
    \ exact equality (`if \"Gadget D\" in cell.text`) if the\n  label is followed by a footnote or line\
    \ break.\n- Low-level alternative, useful if `has_table` misses something:\n  `tbl = slide._element.findall('.//{http://schemas.openxmlformats.org/drawingml/2006/main}tbl')`\n\
    \  or `slide._element.xpath('.//a:tbl')`.\n- If several tables exist on the slide, disambiguate by\
    \ `gf.name` (e.g. \"Table 3\") or by\n  position, and print `gf.left/.top/.width/.height` to confirm.\n\
    - Remember a table may also live on the layout/master, in which case `slide.shapes` will\n  not find\
    \ it at all.\n"
- id: pptx-locate-02
  answer: "A horizontal merge is recorded on the cell itself, not as a separate object. The\n**origin**\
    \ `a:tc` carries a span attribute (PowerPoint normally writes\n`<a:tc gridSpan=\"2\">`; the same schema\
    \ also permits `hMerge`/`vMerge`, and vertical merges\nuse `rowSpan`/`vMerge`) and the spanned-away\
    \ grid positions are either omitted from the row\nor written as `hMerge=\"1\"` continuation cells.\
    \ python-pptx exposes\n`cell.is_merge_origin`, `cell.is_spanned`, `cell.span_width`, `cell.span_height`,\
    \ but has\n**no public merge/unmerge API** — you must edit XML.\n\nWhat to watch:\n\n- **The row must\
    \ still cover the whole grid.** Sum of `gridSpan` values (plus spanned\n  cells) must equal `len(a:tblGrid/a:gridCol)`.\
    \ If you delete a `a:tc` without\n  decrementing a neighbour's `gridSpan`, every later cell in that\
    \ row shifts one column\n  left and PowerPoint will flag the file as needing repair.\n- **`row.cells[i]`\
    \ is a physical-cell index, not a grid column index.** With the Total\n  label spanning columns 0-1,\
    \ `cells[0]` covers both, and `cells[1]` is really column 2.\n  Indexing by grid column without checking\
    \ `is_spanned` writes text into the wrong place.\n- **Text lives in the origin cell only.** Writing\
    \ to a spanned cell does nothing useful;\n  \"Total\" must be set on the merge origin.\n- **Vertical\
    \ merges break on row deletion.** If any cell uses `rowSpan`/`vMerge`, removing\n  an `a:tr` leaves\
    \ dangling continuation cells; convert them to standalone cells (drop\n  `rowSpan`/`vMerge`) or delete\
    \ the whole span.\n- **Grid widths are unaffected.** Deleting cells does not shrink `a:tblGrid/a:gridCol`\n\
    \  widths or the frame `ext cx`, so the table's declared width silently becomes wrong\n  until you\
    \ fix it.\n- When you later re-add a row by cloning a normal (unmerged) row, it will have one fewer\n\
    \  `a:tc` than the grid — clone a row with the same merge pattern or set `gridSpan`\n  explicitly.\n\
    - A merge whose origin is deleted also orphans the text; nothing moves it for you.\n"
- id: pptx-rowdel-01
  answer: "There is **no** `table.rows.remove()` and no delete-row API in python-pptx (still true in\n\
    1.0.x). `_Rows` only implements `__len__`, `__iter__` and `__getitem__`. You must remove\nthe `a:tr`\
    \ element from the XML:\n\n```python\nfrom pptx.util import Emu\n\ngf = next(sh for sh in slide.shapes\
    \ if sh.has_table)\ntable = gf.table\n\ni = 4                                  # index of the row\
    \ to delete\ntr = table.rows[i]._tr                 # CT_TableRow\nremoved_h = tr.get('h')       \
    \        # remember the row height (EMU as str)\ntr.getparent().remove(tr)              # == table._tbl.remove(tr)\n\
    \n# keep the frame box consistent (see pptx-rowdel-02)\ngf.height = Emu(gf.height - int(removed_h))\n\
    ```\n\nNotes:\n\n- `table.rows` is re-derived from the live `a:tbl` on every access, so after the\
    \ removal\n  the collection is immediately correct; no cache invalidation or `prs.save()` dance is\n\
    \  needed beyond saving the file.\n- Deleting a whole column (all `a:tc` at index j) plus the matching\
    \ `a:gridCol` is the\n  same pattern, but remember to fix `gridSpan` values.\n- If you delete a cell\
    \ (column) you must fix `gridSpan` on any cell that spans the removed\n  position, otherwise the row\
    \ under-fills the grid.\n- Renumbering cross references (e.g. \"Gadget D\" referenced in a footnote\
    \ or summary\n  slide) is your responsibility — python-pptx will not do it.\n- Alternatives that avoid\
    \ XML: rebuild the table by deleting the whole `p:graphicFrame`\n  and re-adding it with `shapes.add_table(rows,\
    \ cols, left, top, width, height)` and\n  re-applying all cell formatting. That is usually worse for\
    \ formatting fidelity, but it\n  is the only fully supported route.\n"
- id: pptx-rowdel-02
  answer: "Yes, it matters. The graphic frame's `p:xfrm/a:ext/@cy` is the shape's declared size and is\n\
    kept in parallel with the table geometry; the actual layout is driven by\n`sum(a:tr/@h)` and `sum(a:gridCol/@w)`.\
    \ PowerPoint maintains the invariant\n`ext.cx == sum(gridCol w)` and, in practice, `ext.cy == sum(tr\
    \ h)` for the declared state.\nA mismatch means the selection handles / size reported in the Size\
    \ and Position pane no\nlonger match what is drawn, autofit and text-shrink decisions get computed\
    \ against the\nwrong box, the shape overlaps or leaves a gap relative to other content, and PowerPoint\n\
    may re-normalise the frame (or, if the frame is smaller than the rows need, clip or\noverflow) the\
    \ next time the table is touched or resized.\n\nFix it explicitly after removing rows:\n\n```python\n\
    from pptx.util import Emu\nnew_h = sum(int(tr.get('h') or 0) for tr in table._tbl.tr_lst)\ngf.height\
    \ = Emu(new_h)          # sets p:xfrm/a:ext/@cy\n```\n\nCaveats:\n\n- `a:tr/@h` is a **minimum** height;\
    \ PowerPoint grows rows to fit text. So the sum is the\n  correct *declared* height, not necessarily\
    \ the final rendered height, and the frame can\n  legitimately differ after PowerPoint reflows.\n\
    - Adjust the new row's `@h` (or leave the source height) *before* recomputing; setting\n  `gf.height`\
    \ first and then changing row heights puts you back out of sync.\n- If the table sits in a placeholder,\
    \ PowerPoint may snap the frame to the placeholder\n  size on open, so consider un-placeholdering\
    \ or accepting that behaviour deliberately.\n- If the table now extends past the slide bottom, also\
    \ reduce `gf.top` / row heights, or\n  shorten the content — the height fix alone can push the shape\
    \ off-canvas.\n"
- id: pptx-rowdel-03
  answer: "The visible behaviour depends on whether the shading is **literal** or **style-derived** —\n\
    and that is exactly why it happens in one deck and not another.\n\n- **Style-driven (self-healing).**\
    \ `a:tblPr` has `bandRow=\"1\"` and a\n  `<a:tableStyleId>` pointing to a style in `ppt/tableStyles.xml`\
    \ whose\n  `band1Horz`/`band2Horz` (and `firstRow`, `firstCol`, `lastRow`, `lastCol`) define the\n\
    \  fills. PowerPoint re-bands the remaining rows automatically, so deleting a middle row\n  leaves\
    \ a correctly alternating table. Tables created by python-pptx's\n  `shapes.add_table()` ship in this\
    \ state (`<a:tblPr firstRow=\"1\" bandRow=\"1\"/>` plus a\n  style id).\n\n- **Literal (breaks).**\
    \ Each `a:tc` carries its own `a:tcPr/a:solidFill`. That colour is\n  data, not a rule, so nothing\
    \ recomputes it: delete a light row and you are left with two\n  dark rows adjacent. This is typical\
    \ of decks that came through converters/exporters\n  (Google Slides, older LibreOffice, some HTML-to-PPTX\
    \ pipelines) which \"flatten\" theme\n  colours into explicit fills.\n\n- **Mixed.** Row-level fills\
    \ on some rows plus style banding for the rest — you see the\n  artefact only where the literal fill\
    \ was present.\n\n- Also check `a:tblPr/@bandRow` (and `firstRow`): if `bandRow=\"0\"`, there is no\n\
    \  style-driven banding at all and every row's colour is explicit.\n\nDiagnosis: dump `a:tblPr` and\
    \ a couple of `a:tcPr` elements. If `a:tcPr` has `solidFill`,\ntreat the table's colours as literal\
    \ and re-alternate the remaining rows yourself (flip\nthe fill of the rows after the deletion point,\
    \ or set banding by removing the explicit\nfills and enabling `bandRow=\"1\"` with a proper `tableStyleId`).\
    \ Do not assume that\ndeleting a row \"shifts\" a manually striped table.\n"
- id: pptx-rowdel-04
  answer: "Because none of those tricks actually remove anything — they only disguise it, and they\nfail\
    \ in ways that are hard to notice:\n\n- **The data is still in the file.** Screen readers, PowerPoint's\
    \ outline/reading order,\n  \"Select All → Copy\", Find, third-party extractors (python-pptx, text-scraping\
    \ scripts,\n  accessibility checkers) and any PDF/HTML export will still see the row's text. A\n \
    \ supposedly hidden quantity is still discoverable and still wrong.\n- **A covering rectangle is a\
    \ separate shape.** It must be z-ordered above the table, it\n  does not move or resize with the table,\
    \ it does not participate in the table's geometry\n  (so column alignment, banding and borders of\
    \ the \"hidden\" row are still computed and\n  still drawn), and any later z-order change, \"Send\
    \ to Back\", group/ungroup, or a\n  PowerPoint re-render exposes it. It also breaks accessibility\
    \ reading order and makes the\n  table unselectable in the normal way.\n- **Near-zero height does\
    \ not collapse a row.** `a:tr/@h` is a *minimum*: PowerPoint grows\n  the row to fit its text, so\
    \ you get a thin sliver with clipped/overlapping glyphs, still\n  a grid row that keeps the column\
    \ geometry misaligned, still painted by the table style\n  (borders and band fill), and easily re-expanded\
    \ by an edit.\n- **White text is fragile.** It disappears on dark-mode/print backgrounds, is invisible\
    \ in\n  some exports, and any theme or fill change makes it visible again. White-on-white also\n \
    \ fails WCAG-style contrast checks and is a classic accessibility red flag.\n- **It does not update\
    \ any derived numbers** (totals, labels, cross-references), and it\n  leaves a maintenance trap for\
    \ the next person.\n\nCorrect approaches, in order of preference: delete the `a:tr` (and fix the frame\
    \ height as\nin pptx-rowdel-02); or, if the row must stay in the grid, blank its text, remove its\n\
    borders and set a genuinely minimal `@h` (accepting it is still present); or mark it as\n\"not used\"\
    \ with visible text. There is no row-level \"hidden\" flag in DrawingML tables.\n"
- id: pptx-cell-01
  answer: "You lose the formatting. `cell.text = \"SKU\"` goes through `TextFrame.text`'s setter, which\n\
    does roughly:\n\n```python\ntxBody.clear_content()   # removes EVERY <a:p> (bodyPr and lstStyle survive)\n\
    p = txBody.add_p()\nr = p.add_r()\nr.text = text             # writes <a:t>, and does NOT create <a:rPr>\n\
    ```\n\nConsequences: the old `a:r` (and its `a:rPr` with `b=\"1\" sz=\"1400\"` and the white\n`a:solidFill`)\
    \ is deleted, and the replacement run has **no run properties at all**, so it\ninherits from the placeholder/list-style/table-style\
    \ defaults — typically 18 pt black. On a\ndark header band the word becomes invisible. Because *all*\
    \ `a:p` elements are removed, you\nalso lose the paragraph's `a:pPr` (alignment `algn`, bullet settings,\
    \ line spacing, indent)\nand any `a:endParaRPr` that was setting the \"next typed character\" formatting.\
    \ Cell-level\nproperties in `a:tcPr` (fill, margins, `anchor`, borders) are untouched, so the band\
    \ colour\nstays but the text formatting does not.\n\nWays to keep it:\n\n1. **Edit the existing run\
    \ in place** (best):\n   ```python\n   p = cell.text_frame.paragraphs[0]\n   p.runs[0].text = \"SKU\"\
    \        # keeps a:rPr and a:pPr\n   ```\n   (If the cell has several runs/paragraphs, clear or reassign\
    \ them deliberately, and watch\n   for leftover runs so you do not get \"SKUSKU\".)\n\n2. **Capture\
    \ and reapply**, if you must rebuild the paragraph:\n   ```python\n   import copy\n   old_rPr = p.runs[0]._r.get_or_add_rPr()\n\
    \   new_p = ...                    # build new paragraph\n   new_r = new_p.add_run(); new_r.text =\
    \ \"SKU\"\n   new_r._r.insert(0, copy.deepcopy(old_rPr))\n   ```\n\n3. **Set properties explicitly**\
    \ through the API:\n   `f = run.font; f.bold = True; f.size = Pt(14); f.color.rgb = RGBColor(0xFF,0xFF,0xFF)`\n\
    \   (accessing `run.font` creates the `a:rPr`). Also re-set `p.alignment` if you rely on it.\n\n4.\
    \ At XML level, just swap the `a:t` text node and leave everything else alone.\n\nIn all cases, re-check\
    \ the row height afterwards: a 14 pt run in a cell whose `@h` was\ntuned for the old content may change\
    \ the rendered height.\n"
- id: pptx-cell-02
  answer: "Column widths do **not** change — `a:gridCol/@w` is fixed, and python-pptx/PowerPoint never\n\
    re-fit columns to content. What changes is height: with `bodyPr@wrap=\"square\"` the longer\ntext\
    \ wraps onto more lines, and because `a:tr/@h` is a *minimum*, PowerPoint grows the row\nto fit the\
    \ text. The knock-on effects are:\n\n- The table's rendered height now exceeds `p:xfrm/a:ext/@cy`,\
    \ so the graphic frame's\n  declared box and the drawn content disagree (selection handles/shape size\
    \ wrong, autofit\n  computed against the wrong box).\n- The table can overflow the slide bottom or\
    \ overlap whatever sits below it, and the Total\n  row (and every other row's grid lines) ends up\
    \ misaligned relative to the content that\n  was sized for the old heights.\n- If the cell is narrow,\
    \ the last word may wrap alone, and if `wrap=\"none\"` the text spills\n  horizontally over the neighbouring\
    \ column.\n- Existing `<a:normAutofit fontScale=\"...\" lnSpcReduction=\"...\"/>` inside `a:bodyPr`\
    \ may\n  shrink the text (or, if PowerPoint recalculates it, re-grow the row).\n\nWays to control\
    \ it:\n\n- Decide the layout rule explicitly: fixed row heights with truncation, or auto-grow.\n \
    \ For auto-grow, set the new `@h` and then recompute the frame height\n  (`gf.height = Emu(sum(tr\
    \ h))`).\n- Manage autofit via `text_frame.auto_size = MSO_AUTO_SIZE.TEXT_TO_FIT_SHAPE`\n  (writes\
    \ `a:normAutofit`) or `text_frame.fit_text(max_size=Pt(11))` (python-pptx computes\n  a `fontScale`;\
    \ it needs a resolvable font file and PowerPoint may recompute it later, so\n  treat the scale as\
    \ a hint, not a guarantee).\n- `text_frame.word_wrap = True|False` maps to `bodyPr@wrap`.\n- Rebalance\
    \ widths: widen the Item column's `a:gridCol/@w` and shrink another, keeping\n  `sum(gridCol w) ==\
    \ gf.width` and the total ≤ slide width; add an explicit line break\n  instead; or shorten/curate\
    \ the product name (longest-content budgeting is usually the\n  real fix).\n- Consider `a:tcPr/@marL/marR/marT/marB`\
    \ and `anchor`, which affect the effective text\n  width and vertical position.\n- Then re-verify\
    \ the whole table bottom against the slide height.\n"
- id: pptx-cell-03
  answer: "At minimum the arithmetic, because PowerPoint tables have **no formula engine** (unlike\nWord\
    \ tables): every number, including totals, is literal text that nothing recomputes.\n\nChecklist:\n\
    \n- That line's extended amount = qty × unit price (5 → 12 changes it).\n- Any per-row subtotal for\
    \ its section/category, the column total, and the grand Total\n  cell (including the Total row whose\
    \ first cell is a merged label).\n- Any dependent derived values on the same slide or elsewhere in\
    \ the deck: a tax/VAT line,\n  discount, shipping, grand-total callout, \"N items\" counter, KPI tile,\
    \ summary table on a\n  later slide, or a chart that plots the total.\n- Speaker notes, alt text,\
    \ and any text box quoting the old figure.\n- Re-check geometry: a wider \"12\" can wrap in a tight\
    \ cell, changing that row's height —\n  then re-check the frame `ext cy` and the bottom of the table\
    \ against the slide.\n- If the deck is regenerated by a script, change the source data and regenerate\
    \ rather\n  than hand-editing, so totals stay consistent across all slides.\n- Re-export the PDF/print\
    \ version if one is distributed; PowerPoint tables in PDF are\n  static, so an out-of-date export\
    \ will disagree with the deck.\n- Sanity-check whether the quantity is referenced elsewhere as a *label*\
    \ (e.g. \"5 units of\n  Gadget D in the summary paragraph\") — that is a separate string, not a number\
    \ cell.\n\nA good discipline: after editing, dump all `a:t` values in the table and diff them against\n\
    the expected recalculated set.\n"
- id: pptx-relayout-01
  answer: "python-pptx has no `insert_row()`, so clone the XML and then edit text in place (editing\n\
    `cell.text` would wipe the formatting — see pptx-cell-01):\n\n```python\nimport copy\nfrom pptx.util\
    \ import Emu\n\ngf = next(sh for sh in slide.shapes if sh.has_table)\ntbl = gf._element.graphic.graphicData.tbl\n\
    trs = tbl.findall(qn('a:tr'))\n\ni = next(k for k, tr in enumerate(trs)\n         if \"Service G\"\
    \ in \"\".join(t.text or \"\" for t in tr.iter(qn('a:t'))))\nsrc = trs[i]                      # the\
    \ Service G row: clone THIS (same styling)\nnew_tr = copy.deepcopy(src)       # copies a:trPr, every\
    \ a:tc/a:tcPr, and all runs+rPr\nsrc.addnext(new_tr)\nnew_tr.set('h', src.get('h'))    # keep the\
    \ source row height (a minimum)\n\n# fill it in: table.rows is re-read from the live XML, so the new\
    \ row is immediately visible\nrow = gf.table.rows[i + 1]\nvalues = [\"Service H\", \"SVC-008\", \"\
    Support retainer\", \"1\", \"250.00\"]\nfor cell, val in zip(row.cells, values):\n    para = cell.text_frame.paragraphs[0]\n\
    \    para.runs[0].text = val       # preserves a:rPr + a:pPr from the cloned row\n    for extra in\
    \ para.runs[1:]:  # drop surplus cloned runs\n        extra._r.getparent().remove(extra._r)\n    for\
    \ extra_p in cell.text_frame.paragraphs[1:]:\n        extra_p._p.getparent().remove(extra_p._p)\n\n\
    # frame height must grow by the new row\ngf.height = Emu(sum(int(tr.get('h') or 0) for tr in gf.table._tbl.tr_lst))\n\
    ```\n\nThings to get right:\n\n- **Clone a row with the same cell/merge structure.** The new row must\
    \ cover exactly\n  `len(tblGrid/a:gridCol)` grid columns; a cloned merged row inherits `gridSpan`\
    \ too, which\n  is usually what you want. Clone the row whose *visual* style you want, not just any\n\
    \  neighbour.\n- **Banding.** If the table is style-banded (`bandRow=\"1\"` + table style), the new\
    \ row\n  re-bands automatically and `deepcopy` colours are irrelevant. If colours are literal\n  fills,\
    \ the clone carries the wrong parity after the insertion point — flip the\n  `a:tcPr/a:solidFill`\
    \ of the new row (and of every row after it, if you want strict\n  alternation).\n- **Header/footer\
    \ semantics:** do not clone a `firstRow`/`lastRow`-styled row into the\n  body, and make sure the\
    \ Total row stays last.\n- **Update the numbers**: recompute the Total cell, and any totals/counters\
    \ in notes or on\n  other slides.\n- **Reflow check:** confirm the taller table still fits above the\
    \ slide bottom; if not,\n  reduce row heights or the surrounding layout, not just the frame `cy`.\n\
    - `a:tblGrid` does not change (no new column), but do confirm the sum of `gridCol/@w`\n  still equals\
    \ `gf.width`.\n"
- id: pptx-relayout-02
  answer: "There is no \"add column\" API, so this is XML surgery. Required changes:\n\n1. **`a:tblGrid`**\
    \ — insert a new `<a:gridCol w=\"...\"/>` at the desired position (order in\n   `a:tblGrid` defines\
    \ column geometry):\n   `<a:tblGrid><a:gridCol w=\"2286000\"/><a:gridCol w=\"1200000\"/>...</a:tblGrid>`\n\
    2. **Every `a:tr`** — insert a new `<a:tc>` at the same grid position, cloned from the\n   neighbouring\
    \ cell so it keeps `a:tcPr` (fill, `marL/marR/marT/marB`, `anchor`, borders):\n   ```xml\n   <a:tc>\n\
    \     <a:txBody><a:bodyPr/><a:lstStyle/><a:p><a:pPr algn=\"l\"/>\n       <a:r><a:rPr lang=\"en-US\"\
    \ sz=\"1200\"/><a:t></a:t></a:r></a:p></a:txBody>\n     <a:tcPr marL=\"91440\" anchor=\"ctr\"/>\n\
    \   </a:tc>\n   ```\n   Insert a matching cell into the Total row too (its first cell has `gridSpan=\"\
    2\"`; see\n   pptx-locate-02 for merge handling).\n3. **Fix spans across the insertion point** — any\
    \ cell whose span covers the new column\n   must have `gridSpan` incremented (e.g. `gridSpan=\"2\"\
    ` → `\"3\"`), or a `hMerge`\n   continuation cell added if the producer used that representation.\
    \ Rows must then cover\n   the full grid width again.\n4. **Width arithmetic (the part people forget).**\
    \ The invariant is\n   `sum(a:gridCol/@w) == p:xfrm/a:ext/@cx`. Since the table already spans nearly\
    \ the whole\n   slide, you must either:\n   - **Shrink existing columns** so the total width is unchanged\
    \ (keeps the frame, its\n     `a:off x`, and the surrounding layout identical — the safest option).\
    \ Expect more\n     wrapping in the Item column, so re-check every `a:tr/@h` and then set\n     `gf.height\
    \ = Emu(sum(tr h))`; or\n   - **Widen the frame and re-place it** — reduce `ext cx` or move `a:off\
    \ x` left/up (or\n     shift the whole table) so the new right edge is still within\n     `prs.slide_width`;\
    \ a table whose right edge is off-slide is clipped when presenting\n     and when printing/exporting.\
    \ Never leave `sum(gridCol w) > ext cx`; PowerPoint will\n     either re-normalise on save or render\
    \ inconsistently across PowerPoint/LibreOffice/\n   Keynote.\n5. **Set the header text** on the new\
    \ top cell. If `a:tblPr/@firstRow=\"1\"` with a table\n   style, the new header cell picks up header\
    \ formatting automatically — the same reason\n   new *body* cells usually need no explicit fill; if\
    \ the table's colours are literal\n   `a:tcPr/a:solidFill` values, cloning a neighbour preserves them.\n\
    6. **Recompute row heights and the frame `cy`**, then verify the whole table still fits\n   vertically\
    \ and does not collide with content below.\n\nPractical python-pptx sketch: `deepcopy` a representative\
    \ `a:tc`, insert it into each\n`a:tr` at the right index (`tr.insert(pos, new_tc)`), `deepcopy`/insert\
    \ a `a:gridCol`, bump\n`gridSpan`, redistribute `a:gridCol/@w`, then use the public properties\n`gf.width\
    \ = Emu(total)` / `gf.left = Emu(new_x)` / `gf.height = Emu(sum_h)` to keep the\nframe authoritative.\
    \ Re-open and re-save in PowerPoint once to let it normalise widths, and\ndiff the XML to confirm\
    \ it did not silently re-lay-out the table.\n"
- id: pptx-tblins-01
  answer: "Pick the position in EMU and pass it to `add_table()`; decide it *before* you\ncall, because\
    \ the graphic frame is appended at the end of the shape tree\n(top of the z-order) at the coordinates\
    \ you give.\n\n1. Get the canvas size and existing shape bounds so you can pick a free\n   region\
    \ instead of guessing:\n\n   ```python\n   from pptx.util import Inches, Emu\n\n   prs = Presentation(\"\
    deck.pptx\")\n   slide = prs.slides[0]\n\n   print(prs.slide_width, prs.slide_height)          # EMU\n\
    \   for sh in slide.shapes:                            # note the current layout\n       print(sh.shape_type,\
    \ sh.name, sh.left, sh.top, sh.width, sh.height)\n   ```\n\n2. Compute the target rect. 914400 EMU\
    \ = 1 inch. A \"bottom half\" table on a\n   13.33in-wide deck:\n\n   ```python\n   margin  = Inches(0.5)\n\
    \   width   = Inches(6)\n   height  = Inches(2)\n   left    = Emu(prs.slide_width  - width  - margin)\n\
    \   top     = Emu(prs.slide_height - height - margin)   # e.g. top of bottom half\n   ```\n\n3. Create\
    \ it (3 rows x 2 columns):\n\n   ```python\n   gf = slide.shapes.add_table(rows=3, cols=2,\n     \
    \                         left=left, top=top,\n                              width=width, height=height)\n\
    \   table = gf.table\n   table.cell(0, 0).text = \"Header A\"\n   table.cell(0, 1).text = \"Header\
    \ B\"\n   ```\n\n4. Verify non-overlap with a bounding-box test over the *other* shapes\n   (intersect,\
    \ do not just eyeball it):\n\n   ```python\n   def rect(sh):  return (sh.left, sh.top, sh.left + sh.width,\
    \ sh.top + sh.height)\n\n   def overlaps(a, b):\n       return not (a[2] <= b[0] or b[2] <= a[0] or\
    \ a[3] <= b[1] or b[3] <= a[1])\n\n   new = rect(gf)\n   for sh in slide.shapes:\n       if sh is\
    \ gf or sh.shape_type == MSO_SHAPE_TYPE.PLACEHOLDER and sh.is_placeholder and not sh.has_text_frame:\n\
    \           continue\n       if overlaps(new, rect(sh)):\n           print(\"overlap with\", sh.name)\n\
    \   ```\n\n   Better: scan a grid of candidate positions (slide inset by 0.5in, step\n   0.25in) and\
    \ take the first candidate whose box does not intersect any\n   existing shape; that is deterministic\
    \ and repeatable.\n\n5. Refine geometry afterwards, because `add_table` splits the width/height\n\
    \   you passed *evenly* across the columns/rows:\n   `table.columns[i].width = Inches(2)`, `table.rows[i].height\
    \ = Inches(0.6)`.\n   Row heights are minimums: PowerPoint grows a row if the text needs more\n  \
    \ space, so a table that looks the right height in python-pptx can overflow\n   and cover content\
    \ below it. Reserve headroom (or set a small font via\n   `cell.text_frame.paragraphs[0].font.size\
    \ = Pt(12)`), then re-check the box\n   with the final `sum(r.height for r in table.rows)`.\n\n6.\
    \ If the deck already contains a table, the most reliable \"does not overlap\"\n   answer is to reuse\
    \ that table's frame: copy `left/top/width/height` from\n   the existing `GraphicFrame` (or the whole\
    \ `<p:graphicFrame>` element) and\n   delete only the shape you are replacing, so the new table lands\
    \ exactly in\n   the vacated slot.\n"
- id: pptx-tblins-02
  answer: "The blue look is not a python-pptx bug: `add_table()` writes a default\n`<a:tblPr firstRow=\"\
    1\" bandRow=\"1\"><a:tableStyleId>{5C22544A-7EE6-4342-B048-85BDC9FD1C3A}`\n(Office \"Medium Style\
    \ 2 - Accent 1\"), which resolves against the deck's\ntheme. Your existing tables use a different\
    \ style GUID, or no style at all\n(explicit cell fills/borders). Match one of two things.\n\nA. Copy\
    \ the style reference from an existing table in the same deck:\n\n```python\nimport copy\nfrom pptx.oxml.ns\
    \ import qn\n\ndef match_table_style(src_gf, dst_gf):\n    src = src_gf.table._tbl.find(qn('a:tblPr'))\n\
    \    dst = dst_gf.table._tbl.find(qn('a:tblPr'))\n    # a:tableStyleId must be the LAST child of a:tblPr\
    \ (schema order)\n    for e in dst.findall(qn('a:tableStyleId')):\n        dst.remove(e)\n    for\
    \ e in src.findall(qn('a:tableStyleId')):\n        dst.append(copy.deepcopy(e))\n    # banding flags\
    \ are attributes on tblPr and drive the style's stripes\n    for attr in ('firstRow', 'firstCol',\
    \ 'lastRow', 'lastCol',\n                 'bandRow', 'bandCol'):\n        if src.get(attr) is None:\n\
    \            if attr in dst.attrib:\n                del dst.attrib[attr]\n        else:\n       \
    \     dst.set(attr, src.get(attr))\n```\n\nB. If the deck's tables carry their look as direct formatting,\
    \ a style GUID\n   cannot reproduce it. Then copy the `a:tcPr` (fill, borders `a:lnL/lnR/\n   lnT/lnB`,\
    \ margins, `anchor`) and `a:trPr` from a template cell, or simply\n   clone a whole existing `<a:tbl>`\
    \ and rewrite its text — this is the\n   bulletproof way to get an indistinguishable table:\n\n```python\n\
    template = next(sh for sh in other_slide.shapes if sh.has_table)\nnew_tbl_el = copy.deepcopy(template.table._tbl)\n\
    new_gf = gf._element\nnew_gf.replace(new_tbl_el)      # swap the table inside the new graphic frame\n\
    # then fix up rows/cols and cell text to the shape you need\n```\n\nNotes that bite:\n- `a:tableStyleId`\
    \ position matters; appending it after other `tblPr` children\n  or inserting it before a fill/effect\
    \ element produces a \"needs repair\" file.\n- The GUID must exist in the master's `tableStyles.xml`;\
    \ if the deck has no\n  such part or no matching entry, PowerPoint falls back to its own default\n\
    \  and you will still see blue.\n- Set `table.first_row`, `table.horz_banding` via the API for readability,\n\
    \  but note they only mean something when a style is in play.\n- Cell-level text formatting (font,\
    \ size, bold, alignment, vertical anchor)\n  is never inherited from a table style reliably; set it\
    \ per cell, or clone\n  a fully formatted cell as above.\n"
- id: pptx-tblins-03
  answer: "Use `insert_table()` on the placeholder. python-pptx converts the placeholder\ninto a graphic\
    \ frame in place, inheriting position and size from the layout\n(or from the placeholder's own explicit\
    \ `left/top/width/height`):\n\n```python\nprs = Presentation(\"deck.pptx\")\nslide = prs.slides[0]\n\
    \nfor ph in slide.placeholders:                 # find the empty content ph\n    print(ph.placeholder_format.idx,\
    \ ph.placeholder_format.type, ph.name)\n\nph = slide.placeholders[1]                     # the body/content\
    \ placeholder\ngf = ph.insert_table(rows=3, cols=2)           # -> GraphicFrame\ntable = gf.table\n\
    table.cell(0, 0).text = \"Header A\"\n```\n\nWhat to know:\n- `insert_table()` is only on *slide*\
    \ placeholders (`_BaseSlidePlaceholder`),\n  and only makes sense for a body/object/content placeholder\
    \ (\"Click to add\n  text\" / \"Click to add content\"), not for a title, picture, or\n  date/footer/slide-number\
    \ placeholder.\n- It returns a `GraphicFrame`; the shape is no longer a placeholder\n  (`is_placeholder`\
    \ becomes False) and it is removed from the placeholder\n  collection. Layout inheritance, autofit\
    \ text behaviour, and \"click to add\"\n  editing are gone — you now own the geometry.\n- If the slide\
    \ has no placeholder instance (a blank layout), create one first:\n  `slide.shapes.clone_placeholder(slide.slide_layout.placeholders[1])`,\
    \ then\n  call `insert_table()` on the clone.\n- The inserted table still carries the default blue\
    \ style; apply the\n  style-matching steps (copy `a:tableStyleId` / clone a template `a:tbl`)\n  afterwards\
    \ if it must match the rest of the deck.\n- Alternative if you want full control: `slide.shapes.add_table(rows,\
    \ cols,\n  ph.left, ph.top, ph.width, ph.height)` — same visual result, but you must\n  delete/empty\
    \ the placeholder yourself or it will render as an empty\n  \"Click to add text\" prompt on top of\
    \ the table.\n- Do not use `add_table` and leave the placeholder in place: PowerPoint will\n  show\
    \ the empty prompt and export/print may include it.\n"
- id: pptx-imgrep-01
  answer: "There is no `Picture.replace()`, so do it as: add the new picture at the old\ngeometry, then\
    \ swap the two `<p:pic>` elements in the shape tree so the new\none inherits the old one's index (z-order),\
    \ then delete the old element and\ndrop its relationship only if it is no longer referenced.\n\n```python\n\
    from pptx.util import Emu\n\ndef replace_picture(old_pic, image_path, part=None):\n    part = part\
    \ or old_pic.part\n    left, top = old_pic.left, old_pic.top\n    width, height = old_pic.width, old_pic.height\n\
    \    rotation = old_pic.rotation\n    crop = (old_pic.crop_left, old_pic.crop_right,\n           \
    \ old_pic.crop_top, old_pic.crop_bottom)\n    name = old_pic._element._nvXxPr.cNvPr.get('descr')\n\
    \n    # 1. add the new picture (appended last => on top)\n    new = old_pic._parent.add_picture(image_path,\
    \ left, top, width, height)\n\n    # 2. carry over crop + rotation so it looks identical\n    new.crop_left,\
    \ new.crop_right, new.crop_top, new.crop_bottom = crop\n    new.rotation = rotation\n\n    # 3. same\
    \ z-order: move the new element into the old element's slot\n    old_el, new_el = old_pic._element,\
    \ new._element\n    old_el.addprevious(new_el)          # same index, immediately before old\n   \
    \ old_el.getparent().remove(old_el)   # now the old one is gone\n\n    # 4. optional: keep the alt\
    \ text / name\n    cNvPr = new_el.nvPicPr.cNvPr\n    if name:\n        cNvPr.set('descr', name)\n\
    \    return new\n```\n\nKey points:\n- Z-order is *only* document order of the shapes inside `<p:spTree>`.\n\
    \  `add_picture()` appends, so a newly added picture is always frontmost.\n  `old_el.addprevious(new_el)`\
    \ (or `spTree.insert(index, new_el)` with the\n  index you read beforehand) is what preserves the\
    \ stacking position.\n- Pass both `width` and `height` to force the exact old footprint; that\n  distorts\
    \ if the new file's aspect differs. If you must not distort, add with\n  `width=old_pic.width` only\
    \ (python-pptx derives height from the native\n  ratio), then re-anchor `left/top` yourself.\n- Copy\
    \ `crop_*` values over, otherwise a previously cropped image suddenly\n  shows its full frame (or\
    \ vice versa).\n- Do **not** overwrite the image part's bytes for this (see pptx-imgrep-02).\n- If\
    \ the old picture was inside a group shape, call `group.shapes.add_picture`\n  (or `GroupShapes.add_picture`)\
    \ and do the element swap within the group's\n  tree, not the slide's.\n- Only drop the old relationship\
    \ if nothing else in that slide part still\n  uses that rId; otherwise leave it (an unused rel is\
    \ harmless, a dangling\n  `r:embed` is not). Capture the rId *before* removing the element:\n  `rId\
    \ = old_pic._element.blip_rId`.\n"
- id: pptx-imgrep-02
  answer: "Because in OOXML the image is a *part* (`/ppt/media/imageN.jpeg`) and many\n`<a:blip r:embed=\"\
    rIdX\">` elements — on any slide — can point at the *same*\npart. Images are de-duplicated by content\
    \ hash: when you add an image,\npython-pptx looks for an existing part with the same SHA-1 and reuses\
    \ it\n(`package.image_parts.get_or_add_image_part()`), so \"the logo\" is normally\none part referenced\
    \ by two slides. Overwriting that part's bytes (or its\n`_blob`/`blob`, or seeking into the part's\
    \ stream) mutates the single shared\npayload, and every `r:embed` that points at it — slide 1 and\
    \ slide 7 — shows\nthe new bytes. The relationship graph was never consulted; you bypassed it.\n\n\
    The correct fix is to change the *reference*, not the payload:\n\n```python\n# add the new image ->\
    \ may be a new part (new hash) with a new rId on this slide\nnew_rId, _ = slide.part.get_or_add_image_part(image_file)\n\
    pic._element.blip_rId                    # old rId, still valid for now\n# repoint only this picture's\
    \ blip:\npic._element.blipFill.blip.rEmbed = new_rId\n# then, if nothing else in this slide still\
    \ uses the old rId:\nslide.part.drop_rel(old_rId)\n```\n\nor the structural version: `add_picture()`\
    \ (which creates a fresh part\nbecause the bytes differ), move the new `<p:pic>` into the old one's\
    \ slot,\nremove the old `<p:pic>`, and drop the old rId — exactly the\npptx-imgrep-01 recipe. Either\
    \ way only the target picture changes.\n\nTwo related gotchas:\n- python-pptx does not garbage-collect\
    \ parts. After a save, an image part\n  that is no longer referenced by any relationship is dropped\
    \ when the\n  package is re-serialized from the relationship graph, but if you are\n  mutating parts\
    \ in place you may leave the old media file in `ppt/media/`.\n  Verify with `unzip -l`.\n- Conversely,\
    \ if the new image happens to be byte-identical to some other\n  image in the deck, the \"new\" part\
    \ will be the *existing* part — which is\n  correct and harmless.\n"
- id: pptx-imgrep-03
  answer: "Relationship ids are scoped to the *owning part*, and python-pptx's\n`drop_rel()` is a reference-counting\
    \ convenience that only knows about one\npart's XML:\n\n```python\ndef drop_rel(self, rId):\n    \"\
    \"\"Remove relationship identified by `rId` if its reference count is under 2.\"\"\"\n    if self._rel_ref_count(rId)\
    \ < 2:\n        del self.rels[rId]\n```\n\nSo the guard is \"is this rId still referenced in *this*\
    \ part's element tree?\",\nnot \"is this *image* still used anywhere?\". Two pictures on the same\
    \ slide\nthat share one image normally have *different* rIds (rId3 and rId4) pointing\nat one image\
    \ part, and dropping rId3 is correct. The breakage means the two\nblips shared a single rId and the\
    \ guard was bypassed or not effective:\n\n- The duplicate `<p:pic>` was produced by `copy.deepcopy()`\
    \ of the element, or\n  by hand-editing the XML — so both copies carry the *same* `r:embed=\"rId3\"\
    `.\n  That is the case where the count is 2 and `drop_rel()` correctly refuses;\n  if your code deleted\
    \ the relationship anyway (direct `part.rels` mutation,\n  `rels.pop(rId)`, `rel._target` fiddling,\
    \ an older/newer python-pptx whose\n  count check differs, or writing the `.rels` file yourself),\
    \ the surviving\n  blip is left pointing at a relationship that does not exist. At load time\n  the\
    \ part cannot resolve `r:embed`, so the picture renders as a missing-image\n  placeholder or PowerPoint\
    \ flags the file for repair.\n- The other reference lived in a part python-pptx does not count — e.g.\
    \ a\n  second `<a:blip>` in a *notes slide*, a `p:bg` fill, a table-cell fill, or a\n  picture inside\
    \ a group/OLE preview that you edited separately. The ref\n  count for that part's rId is 1 (or 0),\
    \ so the drop is allowed and that\n  part breaks.\n- `drop_rel()` counts only attributes in the part's\
    \ own XML; a reference\n  introduced *after* the drop (or in a part you re-parse later) is invisible\n\
    \  to it.\n\nCorrect procedure — count references yourself, per rId, in the part you are\nediting,\
    \ and treat the image part separately:\n\n```python\nRNS = 'http://schemas.openxmlformats.org/officeDocument/2006/relationships'\n\
    \ndef rel_is_used(part, rId):\n    n = 0\n    for el in part._element.iter():\n        for k, v in\
    \ el.attrib.items():\n            if k.startswith('{%s}' % RNS) and v == rId:\n                n +=\
    \ 1\n    return n\n\nspTree = slide.shapes._spTree\nrId = pic._element.blip_rId        # capture BEFORE\
    \ removing the element\nspTree.remove(pic._element)        # remove the shape first\nif rel_is_used(slide.part,\
    \ rId) == 0:\n    slide.part.drop_rel(rId)       # or: del slide.part.rels[rId]\n```\n\nTwo related\
    \ points: (a) if you cannot prove the count is zero, simply do not\ndrop the rel — an unreferenced\
    \ relationship is valid OPC and PowerPoint\nignores it; (b) dropping a relationship does not by itself\
    \ delete the image\npart, and python-pptx will not garbage-collect media, so confirm with\n`unzip\
    \ -l deck.pptx | grep media` that the file is actually gone before\ntelling anyone the image was removed.\n"
- id: pptx-imgins-01
  answer: "Compute the height from the image's own pixel dimensions (or add it with\nonly a width and\
    \ let python-pptx derive the height), then anchor it in the\nfree bottom-right corner:\n\n```python\n\
    from pptx.util import Inches, Emu\nfrom PIL import Image\n\npath = \"signature.png\"\nwith Image.open(path)\
    \ as im:\n    px_w, px_h = im.size\n\ntarget_w  = Inches(2)\ntarget_h  = Emu(int(target_w * px_h /\
    \ px_w))        # aspect preserved\n\nmargin = Inches(0.3)\nleft   = Emu(prs.slide_width  - target_w\
    \ - margin)\ntop    = Emu(prs.slide_height - target_h - margin)\n\npic = slide.shapes.add_picture(path,\
    \ left, top,\n                               width=target_w, height=target_h)\n```\n\nTwo-step variant\
    \ that avoids needing PIL (native aspect is honoured when you\npass only `width`):\n\n```python\n\
    pic = slide.shapes.add_picture(path, Inches(6), Inches(4), width=Inches(2))\n# pic.height is now the\
    \ aspect-correct height; position it last\npic.left = Emu(prs.slide_width  - pic.width  - Inches(0.3))\n\
    pic.top  = Emu(prs.slide_height - pic.height - Inches(0.3))\n```\n\nNon-overlap and z-order notes:\n\
    - `add_picture` appends, so the signature lands on top of everything. If it\n  must sit behind existing\
    \ content, move the element:\n  `pic._element.addprevious(other._element)` or reposition it in the\n\
    \  `<p:spTree>`.\n- \"Bottom-right without covering content\" needs an explicit check, because\n \
    \ the slide master/title/footer placeholders are not in `slide.shapes` in\n  all layouts. Compute\
    \ the free rectangle and test intersection against\n  every shape in the slide (and any obvious master\
    \ furniture):\n  `not (a_right <= b_left or b_right <= a_left or a_bottom <= b_top or b_bottom <=\
    \ a_top)`.\n  If the intersection is non-empty, step the box left/up until it is clear.\n- Use transparent\
    \ PNG for a signature so it reads cleanly over the footer;\n  for a photo, consider `pic.crop_*` rather\
    \ than distorting it.\n- If the image is a very high-resolution scan, size the *shape* as above;\n\
    \  python-pptx does not resample the pixels (no native DPI scaling), so a\n  3000px-wide image placed\
    \ at 2in is still crisp.\n"
- id: pptx-imgins-02
  answer: "That is by design: `placeholder.insert_picture()` implements PowerPoint's\n\"Fill\" picture-fill\
    \ behaviour — it *center-crops* the image to the\nplaceholder's aspect ratio instead of fitting it,\
    \ by writing an\n`<a:srcRect l=\"..\" t=\"..\" r=\"..\" b=\"..\">` crop into the picture's\n`<a:blipFill>`.\
    \ A portrait photo dropped into a landscape (or square)\ncontent placeholder therefore loses the top\
    \ and/or bottom of the image: the\nframe is filled edge-to-edge and the overflow is cropped away.\n\
    \nHow to confirm and what to do:\n\n```python\npic = ph.insert_picture(\"portrait.jpg\")\nprint(pic.crop_left,\
    \ pic.crop_right, pic.crop_top, pic.crop_bottom)  # non-zero\npic.crop_left = pic.crop_right = pic.crop_top\
    \ = pic.crop_bottom = 0  # show all\n```\n\nZeroing the crop values shows the whole image, but now\
    \ the blip is stretched\nto the placeholder's frame, so the photo is distorted — a third bad outcome.\n\
    Realistic options:\n\n1. Use an image whose aspect ratio matches the placeholder (or crop it\n   yourself\
    \ beforehand with PIL to the placeholder's width/height ratio, then\n   insert). This is what a human\
    \ would do in PowerPoint.\n2. Use `placeholder.insert_picture()` and then shrink/reposition the picture\n\
    \   inside the placeholder by hand (`pic.width`, `pic.left`, …) while keeping\n   the crop — you get\
    \ a cropped-but-correctly-proportioned image that no\n   longer covers the full placeholder.\n3. Skip\
    \ the placeholder and size the picture yourself, preserving aspect:\n\n   ```python\n   pic = slide.shapes.add_picture(\"\
    portrait.jpg\", ph.left, ph.top,\n                                  width=ph.width)   # height derived\n\
    \   # optionally centre it in the placeholder\n   pic.left = Emu(ph.left + (ph.width  - pic.width)\
    \  // 2)\n   pic.top  = Emu(ph.top  + (ph.height - pic.height) // 2)\n   ```\n\n4. For a \"fit inside\"\
    \ helper, compute the scale yourself:\n   `scale = min(ph.width / native_w, ph.height / native_h)`\
    \ and set\n   width/height from the native size, then centre it in the placeholder.\n\nThere is no\
    \ `insert_picture(..., fit='contain')` option in python-pptx;\nthe crop-preserving behaviour of `insert_picture`\
    \ is the only convenience it\noffers, so any \"fit\" behaviour you need you have to compute yourself.\n"
- id: pptx-legacy-01
  answer: "No. python-pptx only reads and writes OOXML packages: `.pptx` (and `.potx` /\n`.ppsx` variants).\
    \ `deck.ppt` is a legacy OLE2/CFB compound binary format\n(PowerPoint 97–2003), and python-pptx's\
    \ package reader expects a ZIP. You\nwill not get a clean \"unsupported format\" message — it typically\
    \ falls back\nto the directory-package reader or fails inside `zipfile`, so you may see\n`PackageNotFoundError`,\
    \ `IsADirectoryError`, or a `KeyError` about a missing\n`ppt/presentation.xml`. There is no \"legacy\
    \ mode\" and no recovery path for\nthe binary format.\n\nWorkflow:\n\n1. Convert the binary deck to\
    \ `.pptx` first, with LibreOffice headless\n   (LibreOffice reads `.ppt` through its \"Impress MS\
    \ PowerPoint 97\" import\n   filter):\n\n   ```bash\n   soffice --headless --convert-to pptx:\"Impress\
    \ MS PowerPoint 97\" \\\n           deck.ppt --outdir out/\n   # macOS: /Applications/LibreOffice.app/Contents/MacOS/soffice\n\
    \   # if auto-detection misfires:\n   soffice --headless --infilter=\"Impress MS PowerPoint 97\" --convert-to\
    \ pptx deck.ppt\n   ```\n\n   Notes: use `soffice` (not the `libreoffice` alias) and always `--headless`\n\
    \   on a machine with a clean user profile (`-env:UserInstallation=file:///tmp/lo`\n   avoids profile\
    \ lock errors). Convert a *copy*; never overwrite the `.ppt`.\n   If the deck is macro-enabled (`.pptm`\
    \ content inside a `.ppt`, or actual\n   `.pptm`), decide up front whether you need to keep the macros\
    \ — python-pptx\n   cannot preserve VBA, so a `.pptm` round trip through python-pptx will drop\n \
    \  them.\n\n2. Verify the conversion before you touch it (this is the important part):\n   open it\
    \ with `Presentation('out/deck.pptx')`, walk every slide and shape,\n   and diff against the original\
    \ in PowerPoint. Expect possible losses of\n   SmartArt, charts and their formatting, OLE/embedded\
    \ objects, equations,\n   animations and transitions, macros, custom layouts, embedded fonts, and\n\
    \   exact text metrics (missing fonts get substituted and text reflows).\n   LibreOffice rewrites\
    \ the package: everything is now plain OOXML shapes,\n   line/fill effects are approximated, and any\
    \ PowerPoint-only feature has\n   already been flattened.\n\n3. Now make the table edit with python-pptx\
    \ (usually\n   `table.cell(r, c).text = ...`, plus row height / merge / style fixes),\n   save as\
    \ `deck-edited.pptx`.\n\n4. If the user insists on getting `.ppt` back, convert again:\n   `soffice\
    \ --headless --convert-to ppt:\"Impress MS PowerPoint 97\" out/deck-edited.pptx`.\n   Expect a second,\
    \ cumulative loss of fidelity and tell the user this\n   explicitly; recommend they keep the `.pptx`\
    \ as the working format.\n"
- id: pptx-legacy-02
  answer: "Before editing (i.e. right after the LibreOffice conversion):\n\n- Confirm it is a valid package:\
    \ `Presentation('out/deck.pptx')` loads, and\n  `zipfile.ZipFile(p).testzip()` returns `None`.\n-\
    \ Structural diff against the original: slide count, per-slide shape count\n  and shape types, table\
    \ dimensions and cell text, chart count, picture\n  count, notes slides, section list. Anything that\
    \ dropped is a conversion\n  casualty, not your bug — record it *now*, because you cannot distinguish\n\
    \  it from your own damage later.\n- Visual diff: render the original and the converted file to images\n\
    \  (`soffice --headless --convert-to pdf`, then `pdftoppm -r 100`) and compare\n  page by page. This\
    \ is the only reliable way to catch font substitution and\n  reflow, which are the most common visible\
    \ regressions.\n- Inventory the risky features explicitly and flag each one: SmartArt,\n  charts (they\
    \ may have been converted to pictures or lost their data\n  links), OLE/embedded objects, equations,\
    \ animations/transitions, macros\n  (VBA), custom XML parts, embedded fonts, hyperlinks/triggers,\
    \ and any\n  PowerPoint-specific tables (e.g. \"linked picture\" tables).\n- Check fonts: any font\
    \ not installed on the converting machine is\n  substituted, which changes text metrics and can push\
    \ content off the\n  slide. Install the deck's fonts and re-convert if it looks wrong.\n\nAfter editing\
    \ (in addition to the safe-save procedure in pptx-verify-01):\n\n- Re-open the saved `.pptx` and assert\
    \ the specific edit: table dimensions,\n  cell text, merges, and that neighbouring shapes/positions\
    \ are unchanged.\n- Re-render to images and eyeball the edited slide; LibreOffice's renderer\n  is\
    \ more forgiving than PowerPoint, so open once in real PowerPoint too.\n- If you converted back to\
    \ `.ppt`, re-run the same checks on the `.ppt`:\n  the second conversion is where most of the remaining\
    \ damage shows up.\n\nWhat you must tell the user:\n\n- The original `.ppt` is never modified; the\
    \ edits live in a new `.pptx`\n  (and possibly a re-converted `.ppt`). Give them the exact file paths.\n\
    - LibreOffice conversion is lossy and irreversible for PowerPoint-only\n  features. List, for *their*\
    \ deck specifically, what was lost or flattened\n  (don't hand-wave \"some formatting may differ\"\
    ).\n- Recommend keeping the `.pptx` as the source of truth going forward; a\n  `.ppt` -> `.pptx` ->\
    \ edit -> `.ppt` round trip applies conversion loss\n  twice, and the second pass is usually the worse\
    \ one.\n- If macros exist, say plainly that VBA is not preserved by python-pptx and\n  must be re-attached\
    \ / the deck kept as `.pptm` and edited another way.\n- Mention fonts and any other environment assumptions\
    \ (LibreOffice version,\n  headless conversion on this machine) so they can reproduce the result.\n\
    - Be explicit about verification performed and its limits: you verified\n  structure and a rendered\
    \ diff; you cannot guarantee a PowerPoint repair\n  prompt is impossible without opening it in PowerPoint.\n"
- id: pptx-verify-01
  answer: "Never edit in place; keep the input, and verify structurally, semantically\nand visually in\
    \ that order.\n\n```python\nimport shutil, zipfile\nfrom pptx import Presentation\n\nsrc, work, out\
    \ = \"deck.pptx\", \"work.pptx\", \"out/deck-edited.pptx\"\nshutil.copyfile(src, work)          #\
    \ 1. never touch the original\n\nprs = Presentation(work)            # 2. load, edit ...\n# ... edits\
    \ ...\ntmp = out + \".tmp\"\nprs.save(tmp)                       # 3. save to a temp name, then rename\n\
    shutil.move(tmp, out)               #    (atomic-ish; no half-written file)\n```\n\nVerification steps:\n\
    \n1. Zip integrity: `zipfile.ZipFile(out).testzip() is None`, and confirm\n   `[Content_Types].xml`\
    \ plus every referenced part exists; a bad part graph\n   is the usual cause of \"needs to be repaired\"\
    .\n2. Re-open from disk with a fresh `Presentation(out)` and iterate all slides\n   and shapes. This\
    \ proves the package is loadable by the same library that\n   will consume it.\n3. Assert the *semantic*\
    \ result, not just \"it loaded\": for each edited\n   slide check shape count, table `rows`/`cols`\
    \ and cell text, and for each\n   edited picture check `left/top/width/height/crop`. Compare against\
    \ a\n   snapshot of those values taken before the edit.\n4. Re-parse every XML part you touched (and\
    \ let `lxml` complain):\n   `etree.fromstring(part.blob)`. If you hand-edited XML, also validate\n\
    \   against the ECMA-376 schema if you have it, or at minimum check child\n   element order against\
    \ the schema sequence.\n5. Check that the relationship graph is consistent: no `r:embed`/`r:id` in\n\
    \   any part points at a missing rId, and no orphan media remains\n   (`unzip -l out/deck.pptx | grep\
    \ media`).\n6. Render and look: `soffice --headless --convert-to pdf out/deck-edited.pptx`\n   then\
    \ `pdftoppm -r 110 -png` and inspect the affected pages. A structural\n   check will not catch a table\
    \ that overflows its frame or an image that now\n   covers the title.\n7. Open once in real PowerPoint\
    \ and confirm there is no \"needs to be\n   repaired\" prompt — PowerPoint is stricter than python-pptx\
    \ and\n   LibreOffice, and it is the only authority on this.\n8. Optionally diff the two packages\
    \ part-by-part\n   (`unzip` both to temp dirs and `diff -r`) to confirm only the intended\n   parts\
    \ changed. Expect unrelated churn (rewritten `docProps`, reordered\n   rels) and do not treat it as\
    \ a failure; judge semantically, not\n   byte-wise.\n9. Report honestly: what you verified, with what\
    \ tool, and what remains\n   unverified (e.g. \"opened in LibreOffice only; please confirm in\n  \
    \ PowerPoint before sending\").\n"
- id: pptx-verify-02
  answer: "\"Needs to be repaired\" means the package or slide XML violates OPC or the\nECMA-376 schema.\
    \ The usual causes, roughly in order of frequency:\n\nSchema/structure\n- **Wrong child-element order.**\
    \ OOXML content models are strict `xsd:sequence`\n  and PowerPoint enforces them. In `a:tbl` the order\
    \ must be\n  `a:tblPr, a:tblGrid, (a:tr)+` (plus optional `a:extLst`); inside `a:tblPr`\n  the fill/effect\
    \ elements come first and **`a:tableStyleId` must be the last\n  child**; inside `a:tcPr` the `a:lnL/lnR/lnT/lnB`\
    \ line elements precede the\n  fill; inside `a:pPr` the sequence is `lnSpc, spcBef, spcAft, buClr*,\
    \ buSz*,\n  buFont*, bu*, tabLst, defRPr, extLst`; inside `a:tr` only `a:tc` (and\n  `a:extLst`) may\
    \ appear. Appending a new element with `append()` at the end\n  of a properties element is the single\
    \ most common bug here.\n- **Missing required attributes/elements**: `a:tbl` needs `id`; `a:gridCol`\n\
    \  needs `w`; `a:tr` needs `h`; `a:tc` needs `txBody`; a text body needs\n  `a:bodyPr`; `p:xfrm` children\
    \ must be `a:off` then `a:ext`. Dropping one\n  when rebuilding a row is enough.\n- **Table geometry\
    \ inconsistency**: the number of `a:gridCol` in `a:tblGrid`\n  not equal to the number of `a:tc` in\
    \ a row; a `w`/`h` of 0 or a\n  non-numeric value; `a:gridCol/@w` values that do not sum to the frame\n\
    \  width (PowerPoint usually tolerates this, but extreme values do not).\n- **Bad merges**: `a:tc/@hMerge=\"\
    1\"` continuation cells without a preceding\n  origin cell, `vMerge=\"1\"` continuations that are\
    \ not below a\n  `vMerge=\"rest\"` origin, `rowSpan`/`gridSpan` values out of range, or merges\n \
    \ left over from a table you resized by hand.\n- **Non-integer/ill-typed attribute values** (e.g.\
    \ `sz=\"14pt\"`, `marL`\n  without units, `dirty=\"yes\"`), or boolean attributes set to `true`/`false`\n\
    \  instead of `1`/`0` where the schema expects ST_OnOff.\n\nXML-level\n- Malformed XML, undeclared\
    \ namespace prefixes, a wrong namespace URI, a\n  `mc:AlternateContent` block left inconsistent (its\
    \ `mc:Choice` and\n  `mc:Fallback` disagree, or `Requires` names an undeclared prefix), text\n  content\
    \ inside element-only content, duplicate attributes, invalid control\n  characters, or a leading BOM/encoding\
    \ declaration that disagrees with the\n  bytes you wrote.\n- `xml:space=\"preserve\"` missing on an\
    \ `a:t` whose text has leading/trailing\n  spaces (usually cosmetic, but PowerPoint's strictness varies).\n\
    \nPackage/relationship level\n- A dangling relationship: you removed the `r:embed`-using element or\
    \ edited\n  `slideN.xml` but not `ppt/slides/_rels/slideN.xml.rels`, or you dropped a\n  rel that\
    \ is still referenced (see pptx-imgrep-03).\n- Added a part (image, chart, notes) without a matching\
    \ entry in\n  `[Content_Types].xml` or without a relationship from its owner; left an\n  orphan part\
    \ in `[Content_Types].xml`; renamed a part without updating the\n  rels that point at it.\n- Zip-level\
    \ problems: duplicate entry names, absolute or backslash paths,\n  entries written with the wrong\
    \ compression, a hand-rolled `zipfile`\n  rewrite that omits entries, or `[Content_Types].xml` not\
    \ present as the\n  first entry (required by OPC streaming consumers, tolerated by many).\n- `p:presentation`\
    \ child order changed, or `p:sldIdLst` containing an\n  `id` below 256 or a duplicate `id`.\n\nHow\
    \ to find yours: validate the offending part against the ECRA-376 XSD\n(`lxml` XMLSchema) — it names\
    \ the exact line and column of the offending\nelement; failing that, bisect by reverting to the last\
    \ good part, and\ncompare the `a:tblPr`/`a:tcPr` children of a table that PowerPoint accepts\nagainst\
    \ yours. Always open the result in real PowerPoint as the final check.\n"
- id: pptx-verify-03
  answer: "Deleting the `<p:pic>` element removes the *shape* from the slide; it does\nnot, by itself,\
    \ remove the image *part*. The picture's payload lives in\n`/ppt/media/imageN.*` and stays in the\
    \ package for as long as anything\nreferences it — the `r:embed` relationship in\n`ppt/slides/_rels/slideN.xml.rels`,\
    \ another picture on any slide, a\n`p:bg` fill, a table-cell fill, an OLE object's preview, a notes\
    \ slide, or\n`docProps/thumbnail.jpeg`. And if you dropped the relationship as well, the\npart becomes\
    \ an orphan that python-pptx may or may not prune depending on\nhow you produced the file (its serializer\
    \ walks the relationship graph, so a\ngenuine `prs.save()` normally drops unreachable parts — but\
    \ if you edited the\nzip in place, or replaced blobs in memory, the media file survives). So: no,\n\
    you cannot assume it is gone.\n\nCheck all of these:\n\n```bash\n# 1. is the media file still in the\
    \ zip?\nunzip -l deck.pptx | grep -i media\nunzip -l deck.pptx | grep -i thumbnail\n\n# 2. does any\
    \ part still reference it?\nmkdir -p /tmp/x && cd /tmp/x && rm -rf * && unzip -q -o /path/deck.pptx\n\
    grep -rl \"r:embed\" ppt/ ; grep -c . ppt/slides/_rels/slide1.xml.rels\n```\n\nProgrammatically, the\
    \ authoritative check is to walk the package graph from\nthe presentation part and collect every reachable\
    \ part:\n\n```python\nprs = Presentation(\"deck.pptx\")\nseen, stack = set(), [prs.part]\nwhile stack:\n\
    \    p = stack.pop()\n    if p.partname in seen: continue\n    seen.add(p.partname)\n    for rel in\
    \ p.rels.values():\n        if not rel.is_external and rel.target_part.partname not in seen:\n   \
    \         stack.append(rel.target_part)\nprint([str(n) for n in sorted(seen) if \"media\" in str(n)])\n\
    ```\n\nIf the target media part is in `seen`, it is still referenced somewhere and\nmust be traced\
    \ (`rel.is_external` / look at which part holds the rel).\nAdditionally, sanitise what you are shipping:\n\
    \n- Strip identifying metadata (`docProps/core.xml`: title, author, keywords,\n  lastModifiedBy; `docProps/app.xml`:\
    \ company, template) — it often leaks the\n  image's original filename.\n- Remove/replace `docProps/thumbnail.jpeg`,\
    \ which is a render of slide 1 and\n  frequently still shows the confidential picture.\n- Also check\
    \ `ppt/embeddings/` (OLE packages can contain images) and\n  `ppt/media/` for related screenshots.\n\
    - Rebuild the package from only the reachable parts (python-pptx's\n  `prs.save()` to a *new* file,\
    \ or re-zip only `seen`) rather than editing\n  the zip in place, then delete the original file —\
    \ an in-place zip edit\n  leaves the removed member's bytes in the archive, and rewriting a shorter\n\
    \  file over the old one can leave recoverable data in filesystem slack\n  space and in backups/version-control\
    \ history.\n\nFor real redaction, treat removal as \"rebuild + rotate the file\": produce a\nnew,\
    \ scrubbed `.pptx` on a new filename, have the user confirm it, and\nshred the original. Note honestly\
    \ in your report that a `.pptx` is a ZIP, so\n\"I deleted the element\" is not a statement about the\
    \ bytes on disk.\n"
