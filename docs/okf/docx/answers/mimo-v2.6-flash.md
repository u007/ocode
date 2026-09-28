- id: docx-model-01
  answer: "A .docx file is a ZIP archive (OPC — Open Packaging Conventions) containing\nXML parts plus\
    \ relationships and content types. The main document part is\nword/document.xml; other parts include\
    \ styles.xml, settings.xml, numbering.xml,\nheader/footer parts, theme, and media (e.g. word/media/image1.png)\
    \ for images.\nEach part is listed in [Content_Types].xml and connected by .rels files.\n\nA table's\
    \ data lives in the document part (word/document.xml) as a <w:tbl>\nelement. The XML chain from table\
    \ down to text is:\n\n  <w:tbl>            (table)\n    <w:tblPr>        (table properties: width,\
    \ borders, style, layout)\n    <w:tblGrid>      (column grid: <w:gridCol w:w=\"...\"/> per column)\n\
    \    <w:tr>           (row)\n      <w:trPr>       (row properties: height, header flag, etc.)\n  \
    \    <w:tc>         (cell)\n        <w:tcPr>     (cell properties: width, vMerge, shd shading, margins)\n\
    \        <w:p>        (paragraph)\n          <w:pPr>    (paragraph properties, including <w:r> inside\
    \ run props)\n          <w:r>      (run)\n            <w:rPr>  (run properties: bold, size, color,\
    \ etc.)\n            <w:t>    (text content)\n\nSo: tbl → tr → tc → p → r → t.\n"
- id: docx-model-02
  answer: "Because a cell's displayed text is usually split across multiple runs — Word\nbreaks runs at\
    \ formatting boundaries, spelling/proofing marks, autocorrect,\nedits from different sessions, field\
    \ results, or even arbitrarily. So\n\"Widget C\" may be stored as two or more runs (\"Widget\" + \"\
    \ \" + \"C\", or\n\"Widget\" / \" C\"), and no single run.text equals \"Widget C\". Run boundaries\n\
    are invisible in the rendered document. Also, a paragraph can contain\ncontent beyond plain runs (fields\
    \ like w:fldSimple, hyperlinks, etc.) where\ntext is not in w:r elements at all.\n\nReliable approaches:\n\
    1. Search the paragraph's full concatenated text: build\n   \"\".join(node.text or \"\" for node in\
    \ paragraph.runs) (or use\n   paragraph.text) and, if it matches, rebuild the paragraph — set the\n\
    \   first run's text to the new string and delete the remaining runs,\n   preserving the first run's\
    \ rPr formatting.\n2. For matches spanning runs but needing formatting preserved, walk the\n   runs\
    \ and detect the match at an offset, then split/merge run text.\n3. For cross-paragraph or whole-document\
    \ search/replace, operate on the\n   underlying XML by iterating all w:t elements (and remember text\
    \ can\n   also live in w:instrText, w:delText, text boxes, headers, footers,\n   footnotes, comments)\
    \ — python-docx only covers body paragraphs by default.\n"
- id: docx-model-03
  answer: "WordprocessingML uses:\n- Table and cell widths: twips (twentieths of a point, 1/1440 inch)\
    \ stored\n  as 32-bit values in the ST_Dmeasurement type — e.g. w:tblW, w:tcW,\n  w:gridCol all use\
    \ w:w=\"...\". 1 inch = 1440 twips; 1 point = 20 twips;\n  so twips / 1440 = inches, twips / 20 =\
    \ points, twips / 567 ≈ cm.\n- Image sizes (wp:extent, pic:spPr/xfrm/ext, w:drawing ext) use English\n\
    \  Metric Units (EMU): 1 inch = 914400 EMU; 1 point = 12700 EMU;\n  1 cm = 360000 EMU. So EMU / 914400\
    \ = inches, EMU / 12700 = points.\nAlso relevant: ST_Percentage for relative widths is in fiftieths\
    \ of a\npercent (w:tblW type=\"pct\" value=\"5000\" = 100%), and row heights (w:trHeight)\nare in\
    \ twips.\n"
- id: docx-locate-01
  answer: "Approach:\n1. Iterate doc.tables (only body-level; also consider tables nested in\n   cells,\
    \ or sections' headers/footers if relevant).\n2. Identify the invoice table by signature rather than\
    \ position — e.g. the\n   table whose first row's header cells contain \"Item\"/\"Qty\"/\"Price\"\
    \ or\n   that contains the marker cell \"Total\", or by a caption/preceding\n   heading paragraph.\
    \ Fall back to position only if it's documented as\n   stable. Keep an explicit failure if nothing\
    \ matches — do not silently\n   edit the wrong table.\n3. Find the row for \"Gadget D\" by scanning\
    \ rows and comparing concatenated\n   cell text (normalize whitespace, casefold) instead of single\
    \ runs, since\n   text may be split across runs (see docx-model-02). Use a dedicated\n   column index\
    \ derived from the header row rather than assuming index 1.\n4. Stop at the Total/summary rows (rows\
    \ with merged cells or field codes\n   like SUM) so they aren't mistaken for data rows.\n\nBefore\
    \ editing, check:\n- Whether the file is a .docx (python-docx can't open .doc or legacy .doc;\n  it\
    \ raises ValueError on non-DOCX zip parts).\n- Whether tracked changes exist: text inside w:ins /\
    \ w:del may be invisible\n  to python-docx's paragraph.text or duplicated; w:delText isn't plain text.\n\
    - Whether the target text lives in fields (e.g. w:fldSimple, MERGEBRAND or\n  SUM fields), where overwriting\
    \ the displayed result will be recomputed.\n- Whether cells are vertically/horizontally merged — row.cells\
    \ repeats the\n  same cell object (see docx-locate-02), so indexing by position can\n  duplicate or\
    \ miss cells.\n- The actual XML: make a backup copy, validate after edit (open/zip\n  integrity),\
    \ and confirm the document still opens in Word.\n- Read the current formatting/layout so you don't\
    \ disturb it (keep a copy\n  of run rPr/tcPr before rewriting text).\n"
- id: docx-locate-02
  answer: "In python-docx, `row.cells` follows the table grid: for a cell with\nw:gridSpan=\"2\", the\
    \ same underlying _Cell object is returned twice in a\nrow. So `row.cells` returns len(columns) entries\
    \ where the merged cell\nappears at both grid positions (identity: row.cells[i] is row.cells[i+1]).\n\
    So:\n\n- Deleting \"the second cell\" by index: there is no reliable per-position\n  removal. If the\
    \ merged cell occupies positions 1 and 2, index 1 and\n  index 2 are the same cell object — deleting\
    \ the w:tc by index removes\n  the whole merged cell (both grid positions) and desynchronizes the\
    \ grid,\n  leaving the row with fewer w:tc elements than grid columns, which Word\n  may repair or\
    \ render incorrectly. There is no row.delete_cell(n) in\n  python-docx anyway; deletion must be done\
    \ at the XML level (remove the\n  <w:tc> element from <w:tr>).\n- Counting columns by iterating row.cells\
    \ gives the table's column count\n  (grid-based), not the number of physical w:tc elements — so it\
    \ overcounts\n  merged cells. To count real cells, iterate the underlying XML:\n  len(tr.tc_lst).\
    \ Conversely a row with a vertically merged continuation\n  cell still has a w:tc, so grid position\
    \ vs physical cell can diverge the\n  other way too.\n\nCorrect approach: inspect the XML (tc.grid_span,\
    \ vm elements) and\ndistinguish grid positions from physical cells; for grid-accurate mapping\nuse\
    \ python-docx's GridMapper (table._tbl.grid_span helpers) or compute\ncumulative gridSpan yourself.\n"
- id: docx-rowdel-01
  answer: "There is no `table.delete_row(i)` and no `row.delete()` in python-docx.\n(There is a `row.height`/`height_rule`\
    \ setter and `_Row._tr`, but deletion\nis manual XML removal.)\n\nTo delete a row: get the underlying\
    \ <w:tr> element and remove it from its\nparent <w:tbl>:\n\n    tr = table.rows[i]._tr\n    tr.getparent().remove(tr)\n\
    \nor equivalently `table._tbl.remove(tr)`. After removal, re-query\ntable.rows (the Rows view is a\
    \ live view over the XML). Remember\n`add_row` exists but `delete_row` does not — python-docx's public\
    \ API is\nasymmetric here.\n"
- id: docx-rowdel-02
  answer: "Simply removing the <w:tr> is structurally valid XML, but you break the\nvertical merge group:\
    \ the deleted row was the `restart` of a vMerge; the\nfollowing rows still carry `<w:vMerge/>` (which\
    \ means \"continue the merge\nabove\"), but there is no cell above them to continue from. Word then\
    \ treats\nthose continuation cells as orphaned — depending on version/repair it may\ndrop the merge,\
    \ show a stray cell, or flag the file for repair. The visual\nmerge semantics (one tall cell) collapse.\n\
    \nCorrect handling:\n1. Detect the situation: the deleted row's first cell has\n   <w:vMerge w:val=\"\
    restart\"/> and subsequent rows' cells have <w:vMerge/>\n   with no val (continuation).\n2. If deleting\
    \ the restart row, promote the merge to the next row: set the\n   next row's cell to be the restart\
    \ — i.e. change its <w:vMerge/> to\n   <w:vMerge w:val=\"restart\"/> (or insert a w:vMerge with w:val=\"\
    restart\"\n   in its tcPr), carrying over the restart cell's content and formatting\n   if the merge\
    \ content lived in the restart cell. Then remove the old\n   restart row.\n3. If instead the row you\
    \ delete is a continuation cell (bare w:vMerge),\n   just removing that row is fine — the group continues.\n\
    4. In general: remove any w:tc elements whose grid span/columns no longer\n   match, ensure every\
    \ row's total gridSpan equals the table grid, and\n   re-run any row-index bookkeeping after the edit.\
    \ Verify by reopening\n   in Word.\n"
- id: docx-rowdel-03
  answer: "The field code `{ =SUM(ABOVE) }` is a field with an instruction\n(<w:instrText> \" SUM(ABOVE)\
    \ \") and a cached result in <w:t>. python-docx\ndoes not recalculate fields — it just writes XML.\
    \ So immediately after\nsaving, the Total still shows the OLD cached value (computed before the\n\
    row deletion), because deleting the row doesn't touch the field result.\nWhen Word opens the file\
    \ it may or may not update fields automatically\n(usually it updates on open/print depending on settings,\
    \ but not\nguaranteed — F9/update-on-open isn't certain), so the user can see a stale,\nnow-wrong\
    \ Total (the value includes the deleted row's amount).\n\nHandling:\n- Recompute the total yourself\
    \ in code (sum the remaining data rows'\n  amounts) and either write the new value into the field's\
    \ result <w:t>\n  (keeping the field intact so it can still be recalculated), or\n  force-update the\
    \ field.\n- Ensure the deleted row is ABOVE the field and that the field's range\n  still covers the\
    \ intended rows — SUM(ABOVE) sums contiguous cells in the\n  same column above the field, so a removed\
    \ row automatically changes what\n  Word would compute on update, but only when the field is updated.\n\
    - Best practice: after editing, either instruct/recalculate fields\n  (Word: Ctrl+A then F9; or set\
    \ <w:updateFields w:val=\"true\"/> in\n  settings.xml so Word updates fields on open), and verify\
    \ the displayed\n  cached value matches your recomputation.\n"
- id: docx-rowdel-04
  answer: "Because the row still exists in the XML: it remains part of the document\nstructure, still\
    \ shows up in table row counts, still participates in\nmerges, indexes, SUM(ABOVE) ranges, copy/paste,\
    \ accessibility, find,\ntracked changes, and can reappear (e.g. white-on-white text becomes visible\n\
    if shading changes or the file is converted; height \"exact 0\" can be reset\nby Word's layout or\
    \ when rows are autofit; hidden text (w:vanish) shows up\nwhen formatting marks are displayed). The\
    \ data is still in the file — it's\ncosmetic masking, not deletion, and it corrupts your row bookkeeping.\n\
    \nProve the row is gone by inspecting the XML and structure, not the render:\n- Count rows before/after:\
    \ len(table.rows) decreased by one, and\n  len(table.rows[i]._tr's siblings / table._tbl.tr_lst) decreased.\n\
    - Confirm the specific <w:tr> containing that item's text is absent:\n  search the XML for the row's\
    \ marker string (\"Gadget D\") and assert it no\n  longer exists in word/document.xml (unzip the saved\
    \ file and grep).\n- Assert no empty/zero-height/white/hidden row remains (no w:trHeight\n  val=\"\
    0\" exact with no content, no runs with w:color val=\"FFFFFF\" or\n  w:vanish used as camouflage).\n\
    - Reopen with python-docx (and ideally Word itself) and verify the row\n  count and cell values are\
    \ correct.\n"
- id: docx-cell-01
  answer: "`cell.text = \"SKU\"` in python-docx is destructive to the cell's runs: it\nclears the cell's\
    \ paragraphs and sets a single run's text (technically it\nreplaces paragraph content / creates a\
    \ new single-paragraph, single-run\ncell).\n\nSurvives (properties that live outside runs/paragraphs):\n\
    - Cell-level <w:tcPr> things: shading (<w:shd> — the dark background),\n  cell width, margins, vertical\
    \ alignment, borders, text direction,\n  gridSpan/vMerge — these are on the w:tc, so untouched.\n\
    - Table/row-level properties (row height, table style) — untouched.\n\nLost (everything on the paragraphs\
    \ and runs that get replaced):\n- Run formatting: bold (<w:b>), the 9 pt size (<w:sz>), white color\n\
    \  (<w:color>), font/latin typeface, italic, all-caps, etc. — so the new\n  text renders with default\
    \ style formatting (typically 11 pt, black or\n  style color, not bold).\n- Paragraph formatting inside\
    \ the cell: alignment/justification, spacing,\n  indentation, style.\n- Any prior runs and their content\
    \ (the old text is gone), plus inline\n  elements like fields, hyperlinks, bookmarks inside the cell.\n\
    \nNote: if the cell has a paragraph/character style, that style's formatting\nsurvives only if python-docx\
    \ preserves the pStyle (it sets a single\nparagraph; the style reference may be reset depending on\
    \ implementation).\nThe safe pattern: edit the existing run's text\n(`cell.paragraphs[0].runs[0].text\
    \ = \"SKU\"` after verifying run structure)\nor, if runs must be recreated, copy the original rPr\
    \ (deep-copy the\n<w:rPr> element) onto the new run.\n"
- id: docx-cell-02
  answer: "Changing Qty from 5 to 12 invalidates everything derived from it:\n1. The line's amount/subtotal\
    \ for that row (Qty × Unit Price) must change\n   — likely a formula field like { =B2*C2 } or plain\
    \ stored text.\n2. The document Total ({ =SUM(ABOVE) } or similar) — its cached result is\n   stale\
    \ until recalculated.\n3. Tax, discounts, shipping, or percentage lines computed from the subtotal.\n\
    4. Any cross-references or summary text (\"total 5 units\", prose mentions,\n   balance due, \"amount\
    \ in words\").\n5. Possibly a running total or other tables referencing this figure.\n\nHow to make\
    \ numbers consistent:\n- Read the source values (unit price, qty) programmatically; recompute the\n\
    \  line amount, then recompute dependent totals/taxes in dependency order\n  (line → subtotal → tax\
    \ → grand total), not by editing strings ad hoc.\n- If the figures are Word field formulas, update\
    \ the cached <w:t> result\n  to your computed value AND force recalculation: set\n  <w:updateFields\
    \ w:val=\"true\"/> in settings.xml (Word updates on open) or\n  tell the user to select-all + F9;\
    \ verify the displayed values match.\n- If plain text, write the new numbers and verify by reading\
    \ the document\n  back and re-deriving the arithmetic (assert subtotal == qty*price and\n  total ==\
    \ sum of lines).\n- Round consistently (2 decimals, same rounding mode) to avoid off-by-cent\n  mismatches,\
    \ and re-check the Total row still matches the sum of the\n  remaining + changed rows.\n"
- id: docx-cell-03
  answer: "With <w:trackRevisions/> on in settings.xml, Word records edits made in\nthe UI as revisions\
    \ (w:ins/w:del wrappers), but that does NOT make\npython-docx produce tracked changes — python-docx\
    \ writes XML directly and\nbypasses revision tracking entirely. So if you just rewrite the w:t text:\n\
    \n- The edit happens silently and untracked: no w:ins/w:del markup is\n  generated, the change appears\
    \ as if it was always there, and the\n  \"clean\" tracked-changes document now contains an untracked\n\
    \  modification — inconsistent with the user's expectation that the edit\n  be reviewable/rejectable.\n\
    - Worse, if the cell's existing text is already inside revision markup\n  (w:ins or w:del/w:delText),\
    \ python-docx's run/paragraph views may not\n  expose w:delText as normal text, so you could edit\
    \ the wrong run,\n  duplicate content, or leave the old w:delText behind — producing a\n  document\
    \ that shows both old and new text depending on the review view.\n\nWhat to consider:\n- Inspect the\
    \ cell XML for w:ins / w:del / w:delText / w:moveFrom /\n  w:moveTo first; decide whether to edit\
    \ within the existing revision\n  wrappers or to resolve (accept) prior revisions before making your\
    \ edit.\n- Decide the intended semantics: if the user wants the change tracked, you\n  must wrap the\
    \ change manually — put new text in a run inside <w:ins\n  w:id=... w:author=... w:date=.../> and\
    \ the old text in a run inside\n  <w:del ...> with <w:delText> instead of <w:t>.\n- If they want a\
    \ clean edit (their stated goal), leave track changes on\n  for the app but write plain untracked\
    \ text, and be explicit that the\n  change won't appear in the revision history; alternatively warn\
    \ that\n  opening/saving in Word with tracking on will track the user's own\n  subsequent edits, not\
    \ yours.\n- Verify the result renders correctly in both \"All Markup\" and \"No\n  Markup\" views,\
    \ and that no stale old text remains.\n"
- id: docx-relayout-01
  answer: "`table.add_row()` appends a NEW row at the END of the table (after the\nlast row, e.g. below\
    \ the Total row). It creates a <w:tr> with one <w:tc>\nper grid column, cells empty (blank paragraph),\
    \ with no special\nformatting — it inherits no cell shading, no run formatting, and creates\ndefault\
    \ height (from table style). It does not copy the formatting of\nneighboring rows, and it does not\
    \ let you choose a position.\n\nTo insert a correctly formatted row in the middle (after \"Service\
    \ G\",\nbefore Total):\n1. Find the target row (the one containing \"Service G\") at the XML level.\n\
    2. Deep-copy an existing representative data row — e.g.\n   `import copy; new_tr = copy.deepcopy(data_row._tr)`\
    \ — this preserves\n   tcPr (widths, shading), row properties, run formatting, fonts.\n3. Insert it\
    \ after the target: `target_tr.addnext(new_tr)` (or\n   addprevious for before). lxml's addnext places\
    \ it correctly.\n4. Then set the copied row's cell texts to the new values — edit runs'\n   text in\
    \ place rather than using cell.text so formatting survives (or\n   clear and rebuild runs copying\
    \ rPr).\n5. Alternatively use table.add_row() and move it: append then\n   `table.rows[-1]._tr` →\
    \ addnext/ addprevious onto the target row, but\n   then you must manually copy cell/run formatting.\n\
    6. Recompute any field totals (SUM(ABOVE) range now includes the new row)\n   and verify row ordering\
    \ and merges afterward.\n"
- id: docx-relayout-02
  answer: "`table.add_column(width)` appends a new column at the END of the table\n(rightmost), adding\
    \ a <w:gridCol> to <w:tblGrid> and one <w:tc> (with the\ngiven width) to every existing row — including\
    \ header and total rows. It\ndoes NOT insert the column \"after the Item column\"; position must be\n\
    handled manually. The given width is applied to the new cells, but it\ndoesn't automatically re-fit\
    \ the rest of the table.\n\nTo insert after Item (i.e. at grid position 1):\n- Insert a <w:gridCol>\
    \ into <w:tblGrid> at the right index (before the\n  gridCol for the columns you want to shift), and\
    \ insert a <w:tc> at the\n  matching position in every <w:tr> (after the first tc), setting each\n\
    \  new tc's <w:tcW> and each affected row's gridSpan-consistent widths.\n  Or copy an existing column's\
    \ cells (deepcopy a tc) so formatting\n  (borders, shading) carries over, then set the text.\n\nTo\
    \ stay inside the page margins:\n- Compute available width = page width − left/right margins (in twips).\n\
    - Set the table's total width (tblW) and redistribute: shrink existing\n  columns (gridCol + tcW)\
    \ so old widths + new column width ≤ available\n  width; or set the table layout to fixed (<w:tblLayout\
    \ w:type=\"fixed\"/>)\n  to prevent Word from auto-expanding to content.\n- Keep tblGrid gridCol widths\
    \ summing to the table width.\n\nTo look like the other columns:\n- Copy header cell formatting (bold,\
    \ shading, borders, fonts, alignment)\n  from a neighboring header cell — deepcopy the tcPr and header\
    \ run's\n  rPr; likewise copy data-cell formatting (borders, paragraph style,\n  alignment) for body\
    \ rows, and handle the total row and any spanned/\n  merged cells specially (their tc count must still\
    \ match the grid).\n- Verify: reopen and check tblGrid sums to available width and every row\n  has\
    \ the right number of cells/gridSpans.\n"
- id: docx-relayout-03
  answer: "Because cell widths take precedence over the grid: Word lays out from\n<w:tcW> on each cell\
    \ (and the table's <w:tblW>/<w:tblLayout>), with\n<w:tblGrid>/<w:gridCol> acting only as the initial/supporting\
    \ grid — and\nif cells carry explicit tcW values (type=\"dxa\"), those win for rendering.\nAdditionally,\
    \ if <w:tblLayout> is \"autofit\" (the default), Word recomputes\ncolumn widths from content and ignores\
    \ your grid changes anyway.\n\nWhat you must update:\n- Every <w:tc>/<w:tcPr>/<w:tcW w:w=\"...\">\
    \ in every row for the affected\n  columns (python-docx: `cell.width = ...` sets tcW; column-wise\
    \ via each\n  row's cells, minding merged cells where one tc spans multiple gridCols).\n- The table\
    \ width <w:tblW> (and cell widths sum per row) to match the new\n  layout, keeping the total ≤ page\
    \ width − margins.\n- Set <w:tblLayout w:type=\"fixed\"/> if you want Word to honor your widths\n\
    \  instead of autofitting.\n- After editing, reopen the document (or resync python-docx's view by\n\
    \  re-reading) — python-docx caches nothing much, but Word renders from the\n  file it opens, so verify\
    \ the saved XML actually contains the new tcW\n  values; also note gridCol-only changes may be silently\
    \ \"repaired\"/\n  normalized by Word on save if they contradict tcW.\n"
- id: docx-tblins-01
  answer: "doc.add_table() does NOT put it after \"Notes:\" — it appends the new\nw:tbl to the end of\
    \ the document body (just before the final\nw:sectPr), i.e. at the very end of the document. Body-level\n\
    paragraphs and tables are siblings, so to place it correctly you\ncreate the table, then move its\
    \ XML element with addnext()/addprevious():\n  tbl = doc.add_table(rows, 2, cols=...)   # or rows/cols\n\
    \  notes_p._p.addnext(tbl._tbl)             # notes_p = the \"Notes:\" paragraph\nThe table element\
    \ is tbl._tbl (its CT_Tbl); moving the element, not\nre-adding content, is the fix. Note addnext inserts\
    \ immediately\nafter the paragraph, so if there must be a blank spacer, insert that\nparagraph element\
    \ too. (Never copy the paragraph's text to rebuild\nit — move elements, so you don't lose numbering/formatting.)\n"
- id: docx-tblins-02
  answer: "KeyError means the style is not in this document's styles part.\n\"Grid Table 4 Accent 1\"\
    \ is usually only a *latent* style (listed in\nw:latentStyles) in a blank/default document, and in\
    \ many company\ntemplates the built-in style is absent, renamed, or its w:styleId\ndiffers from the\
    \ UI w:name (e.g. styleId \"GridTable4-Accent1\").\npython-docx resolves table.style = \"...\" by\
    \ looking up a w:style\nelement of type table with that name, and raises KeyError when none\nexists\
    \ — latent styles are not materialized on lookup.\nFixes: (1) enumerate what's really there —\n  from\
    \ docx.enum.style import WD_STYLE_TYPE\n  [s.name for s in doc.styles if s.type == WD_STYLE_TYPE.TABLE]\n\
    \  and assign one of those; (2) or apply the style by its actual\n  styleId if it exists; (3) or get\
    \ a ruled (grid) table by direct\n  formatting instead of a style: set w:tblPr/w:tblBorders with\n\
    \  single/nil borders on top/left/bottom/right/insideH/insideV (w:val\n  \"single\", w:sz e.g. 4,\
    \ w:color auto) via OxmlElement/qn, plus\n  w:tblW and w:tblLayout as needed.\n"
- id: docx-tblins-03
  answer: "By default the table spans the full text width of the current\nsection — page width minus left/right\
    \ margins (python-docx builds it\nfrom the section's available width) — with the w:tblGrid columns\n\
    dividing that width equally, and typically w:tblW type=\"auto\"\n(100%/auto layout, so Word may also\
    \ stretch/shrink it). To match the\nexisting invoice table, don't guess: copy its measurements —\n\
    \  inv = doc.tables[0]                 # the invoice table\n  table.autofit = inv.autofit        \
    \  # same w:tblLayout (fixed/autofit)\n  # same total width: copy w:tblW (type + w) from inv._tbl.tblPr\n\
    \  for i, col in enumerate(inv.columns):\n      table.columns[i].width = col.width   # same w:gridCol\
    \ widths\n(Table has no single .width property; the authoritative values are\nw:tblPr/w:tblW, w:tblGrid/gridCol,\
    \ and w:tblPr/w:tblLayout. You can\ncopy those elements wholesale from invoice._tbl, or set fixed\n\
    w:gridCol widths after table.autofit = False. Also match w:tblInd /\nw:tblCellMar / w:tblLook if exact\
    \ alignment matters.)\n"
- id: docx-imgrep-01
  answer: "The bytes are NOT inline. document.xml holds a w:drawing →\nwp:inline (or wp:anchor) → a:graphic/a:graphicData\
    \ → pic:pic →\npic:blipFill/a:blip with r:embed=\"rId4\". That rId is resolved in\nword/_rels/document.xml.rels,\
    \ which maps it to media/image4.png\n(the actual bytes, plus a ContentType in [Content_Types].xml).\n\
    To replace one picture while keeping position and size:\n  drawing = ...find the target w:drawing\
    \ (match by rId or doc\n             properties/descriptions/extent)\n  blip = drawing.find('.//'\
    \ + qn('a:blip'))\n  old_rId = blip.get(qn('r:embed'))\n  # add a NEW image part, don't mutate the\
    \ old one:\n  image_part, new_rId = doc.part.get_or_add_image_part('new_logo.png')\n  blip.set(qn('r:embed'),\
    \ new_rId)\n  # remove the old relationship if now unused:\n  del doc.part.rels[old_rId]\nwp:extent\
    \ (cx/cy), the offset, and the run stay untouched, so\nposition/size are preserved. If the new file\
    \ is a different format\n(png→jpg), ensure its content type/partname extension is right —\nget_or_add_image_part\
    \ handles this. Note that if you instead want\nthe same physical bytes swapped, you can set the existing\n\
    image_part's blob, but that risks the sharing problem in\ndocx-imgrep-02; repointing r:embed is the\
    \ safe per-picture edit.\n"
- id: docx-imgrep-02
  answer: 'Because image parts are shared, not per-picture. Two pictures that

    have identical bytes get ONE media part (python-docx dedups by

    hashing image bytes when adding; Word itself dedups media by hash on

    save), so both drawings'' a:blip r:embed point to the same rId, and

    that rId resolves to the same media part. The page-3 picture

    references the same part as the logo (or the logo appears again in

    header/body), so overwriting image_part._blob changes every

    reference at once. Fix: don''t mutate a shared part — add a new image

    part for the replacement and repoint ONLY the target drawing''s

    a:blip r:embed to the new rId (and delete the old rId only if

    nothing else still references it — check other drawings, headers,

    footers first).

    '
- id: docx-imgrep-03
  answer: "It's not in the body: it lives in a header (header1.xml, possibly\nfirst/even page header variants)\
    \ — headers/footers are separate\nparts with their own rels file pointing at their own media.\ndoc.inline_shapes\
    \ only enumerates w:inline drawings in the main\nbody, so header images never show up; a floating/anchored\
    \ image\n(wp:anchor) or one inside a text box is also invisible to it.\nFind/replace:\n  for section\
    \ in doc.sections:\n      for hdr in (section.header, section.first_page_header,\n               \
    \   section.even_page_header):\n          if not hdr.is_linked_to_previous:\n              for blip\
    \ in hdr._element.findall('.//' + qn('a:blip')):\n                  rId = blip.get(qn('r:embed'))\n\
    \                  part = hdr.part  # its own rels, not doc.part.rels\n                  # add new\
    \ part on hdr.part, set new rId, drop old rel\nAlso scan all parts generically (walk doc.part.package\
    \ or all rels\nwith reltype .../header, .../footer, and footnotes/comments) and\ngrep for a:blip to\
    \ catch watermarks (v:shape/pict or anchored\ndrawing in the header) and linked (`a:blip r:link`)\
    \ images, which\nresolve through the header's rels too.\n"
- id: docx-imgins-01
  answer: "doc.add_picture() adds a NEW paragraph at the END of the document\nbody and puts the picture\
    \ in it (it creates the paragraph if the\nlast block isn't one) — not after \"Approved by:\". Correct\
    \ placement:\n  p = doc.add_paragraph()\n  p.add_run().add_picture('signature.png', width=Inches(1.5))\n\
    \  approved_p._p.addnext(p._p)      # move that paragraph into place\nAspect ratio is kept automatically:\
    \ python-docx reads the image's\nnative pixel size and DPI and computes the height from the width\
    \ you\npass, so specifying only width=Inches(1.5) is enough. If you want it\non the SAME line as the\
    \ text instead, add the run to the existing\nparagraph: approved_p.add_run().add_picture('signature.png',\n\
    width=Inches(1.5)).\n"
- id: docx-imgins-02
  answer: "Nothing shrinks it: the picture keeps its 3-inch width and overflows\n/ widens the 1.2-inch\
    \ column — with autofit the column expands (and\nthe whole table layout shifts/wraps), with fixed\
    \ layout the image\nspills out of the cell and overlaps neighbouring cells. It is never\nauto-scaled.\
    \ Size it explicitly:\n  run = cell.paragraphs[0].add_run()\n  shape = run.add_picture('img.png',\
    \ width=Inches(usable))\nwhere usable = cell width − left/right cell margins (default tcMar\nis 0.08\"\
    \ per side, so about 1.2 − 0.16 = 1.04\"). Get margins from\nthe cell/table w:tblCellMar (or set table.cell\
    \ margins yourself).\nFor an already-inserted shape: shape.width = Inches(...); shape.height\nis derived\
    \ to preserve aspect ratio if you set both proportionally\n(or compute height = width * native_h/native_w).\
    \ If you must keep the\ncolumn at 1.2\", also set table.autofit = False / w:tblLayout\ntype=\"fixed\"\
    \ so Word doesn't expand the column.\n"
- id: docx-legacy-01
  answer: "No. python-docx only reads OOXML packages (.docx/.dotx/.docm-ish\nzip containers); the binary\
    \ Word 97–2003 .doc (OLE/CFB) format is\nnot supported and open() will fail.\nWorkflow: convert first,\
    \ then edit, then verify:\n  1) soffice --headless --convert-to docx --outdir out report.doc\n   \
    \  (best table fidelity; see docx-legacy-03 for making it reliable)\n     — or open in Word/LibreOffice\
    \ GUI and \"Save As .docx\", or on\n     macOS textutil -convert docx (see docx-legacy-02 for its\
    \ table\n     risks).\n  2) Open the .docx with python-docx, locate the row (match on cell\n     text,\
    \ not index — conversion can reorder/add rows), delete it:\n     row._tr.getparent().remove(row._tr),\
    \ or tr.getparent().remove(tr).\n  3) Save to a NEW file (not over the only copy), keep the original\n\
    \     .doc as the source of truth, and verify (row counts, content,\n     ideally a PDF render) before\
    \ replacing anything.\n"
- id: docx-legacy-02
  answer: "Risk: textutil is Apple's text-engine conversion, not a faithful\nOOXML writer. For tables\
    \ it can lose or silently reshape structure:\ndropped tables, merged cells flattened (gridSpan/vMerge\
    \ lost),\nnested tables dissolved, cell/column widths and row heights\nchanged, styles/numbering replaced,\
    \ or the table degraded to plain\ntext/paragraphs — so \"delete one row\" may operate on a table that\
    \ no\nlonger matches the original (wrong row, wrong cells, wrong layout).\nDetection: compare structure\
    \ before and after —\n  - count tables/rows/cells in the converted file (len(doc.tables),\n    len(table.rows),\
    \ cells per row) and compare against the .doc\n    (open it in Word/LibreOffice, or convert with LibreOffice\
    \ and\n    compare that instead);\n  - check for w:gridSpan, w:vMerge, w:tblGrid gridCol count,\n\
    \    w:gridBefore/w:gridAfter presence in the source vs converted;\n  - render both to PDF (e.g. LibreOffice/Quick\
    \ Look) and visually\n    compare; and always re-open the result and confirm the target\n    row's\
    \ cell text still matches the original before deleting.\nIf fidelity matters for tables, prefer LibreOffice\
    \ or Word conversion\nover textutil.\n"
- id: docx-legacy-03
  answer: "LibreOffice is single-instance per user profile: if a GUI\nLibreOffice is already running,\
    \ `soffice` on the command line\nforwards the request to that existing process and the launcher exits\n\
    immediately. The running instance may ignore/queue the conversion\n(it wasn't started to handle it,\
    \ it's busy, or it's a different\nversion/profile), so the conversion never actually happens and no\n\
    output file appears — while the window stays open.\nReliable from a script — force a separate, headless\
    \ instance:\n  soffice --headless --norestore --invisible --nolockcheck \\\n    \"-env:UserInstallation=file:///tmp/lo_$$_profile\"\
    \ \\\n    --convert-to docx --outdir \"$OUT\" file.doc\nand don't trust the exit code: poll until\
    \ the expected output file\nexists and is non-empty (with a timeout), then verify it opens;\nclean\
    \ up the temp profile. Alternatives: kill/close the GUI\ninstance first, or always use a per-invocation\
    \ UserInstallation\nprofile so your script never collides with the desktop session.\n"
- id: docx-verify-01
  answer: "Never overwrite the only copy and never trust \"it saved\":\n  1) Back up the original (cp\
    \ invoice.docx invoice.docx.bak) and\n     check nothing else has the file open (locks on Word/macOS).\n\
    \  2) Edit and save to a TEMP path: doc.save('invoice.tmp.docx'),\n     not the original.\n  3) Validate\
    \ the temp file before promoting it:\n     - zipfile.ZipFile(tmp).testzip() is None and the required\n\
    \       parts exist ([Content_Types].xml, _rels/.rels, word/document.xml);\n     - reopen with Document(tmp)\
    \ and assert the intended result\n       (row count, cell text, image present, table count);\n   \
    \  - optionally render to PDF (LibreOffice) and eyeball it;\n     - confirm file size > 0 and no zero-length\
    \ media entries.\n  4) Only then atomically replace the original:\n     os.replace(tmp, 'invoice.docx')\
    \ — atomic on the same volume, so\n     a crash can't leave a half-written file.\n  5) Keep the .bak\
    \ until the customer/Word has opened the result;\n     reopen once more after replacing to confirm.\n\
    Also: make sure the temp file is on the same filesystem as the\ntarget (os.replace requirement) and\
    \ that you don't leave .tmp files\nbehind.\n"
- id: docx-verify-02
  answer: "python-docx is a lenient XML wrapper — it will happily reopen\nstructurally invalid XML that\
    \ Word's stricter OOXML schema checks\nreject, triggering \"Word found unreadable content\" / repair.\n\
    Common table-edit mistakes:\n  - Row/cell count mismatch: a w:tr with a number of w:tc that\n    doesn't\
    \ fit the w:tblGrid — a cell with w:gridSpan exceeding the\n    grid's columns, or rows missing cells\
    \ without w:gridBefore /\n    w:gridAfter to account for the gap.\n  - Broken merges: w:vMerge with\
    \ w:val=\"restart\" but no following\n    w:vMerge continue (or continues without a restart), or a\n\
    \    vMerge/gridSpan cell that doesn't line up column-wise with the\n    rows above/below.\n  - Missing\
    \ or malformed w:tblGrid / w:gridCol (no grid, zero/dup\n    widths, wrong type).\n  - Invalid placement:\
    \ w:tbl or w:tr outside w:tbl / inside w:p;\n    w:tbl placed after the body's final w:sectPr; a w:tc\
    \ with no\n    block-level child (a cell must contain at least one w:p — an\n    empty cell still\
    \ needs one); a w:tr with zero w:tc.\n  - Wrong element ORDER inside w:tblPr / w:trPr / w:tcPr — the\n\
    \    schema requires a fixed sequence (e.g. tblStyle < tblW < jc <\n    tblBorders < tblLayout < tblCellMar\
    \ < tblLook; tcW before\n    gridSpan/vMerge/shd). Duplicate elements (two w:tblPr, two\n    w:tcPr)\
    \ are also fatal.\n  - Bad values/types: w:tblW with type=\"dxa\" but a percentage value\n    (or\
    \ w:type=\"pct\" with a non-50ths number), w:tblLayout val not\n    \"fixed\"/\"autofit\", negative/huge\
    \ twips.\n  - Raw-XML plumbing errors: mismatched/unclosed tags, wrong or\n    undeclared namespace\
    \ prefixes (w:, a: r:), double-escaped\n    entities, or bytes not written as UTF-8.\n  - Package-level:\
    \ corrupt ZIP, missing [Content_Types].xml, a rel\n    pointing at a missing part, or missing required\
    \ content type\n    for an added part.\n"
- id: docx-verify-03
  answer: "A .docx is a ZIP: deleting w:drawing from the XML removes the\n*reference*, not the bytes.\
    \ The old image can survive as an\norphaned word/media/imageN.png — kept alive by its relationship\
    \ in\nword/_rels/document.xml.rels (or header/footer rels), or simply\nnever garbage-collected because\
    \ you rezipped the old archive.\nCheck:\n  1) List the archive: zipfile.ZipFile(f).namelist() → all\n\
    \     word/media/* entries.\n  2) Collect every rId actually referenced: all a:blip/@r:embed\n   \
    \  (and r:link), plus v:imagedata/@r:id, across EVERY XML part\n     (document, headers, footers,\
    \ footnotes, comments).\n  3) Resolve each rId through that part's .rels; any media part\n     with\
    \ no referencing rId — or whose rId no longer appears in\n     the XML — is the leaked confidential\
    \ image. You can also\n     grep the whole package for a distinctive byte signature /\n     old filename\
    \ / old rId to confirm the bytes are gone.\nClean up properly: remove the drawing AND delete its relationship\n\
    (del doc.part.rels[rId], and the header's rels if it lives in a\nheader), then save through python-docx\
    \ to a NEW file — python-docx\nrebuilds the package from its reachable-part graph, so parts no\nlonger\
    \ reachable via relationships are dropped. If you rezip\nmanually instead, explicitly delete the media\
    \ entry and edit the\n.rels file (a dangling rel pointing at a removed part causes Word\nto complain).\
    \ Finally re-run steps 1–3 on the SAVED file before\ndistributing, and delete any temp/backup copies\
    \ that still contain\nthe logo.\n"
