- id: docx-model-01
  answer: "A .docx is physically an OPC package: a ZIP archive whose entries are XML\nparts plus binary\
    \ resources. The main part is /word/document.xml; other\nparts include styles.xml, settings.xml, numbering.xml,\
    \ footnotes.xml,\ntheme1.xml, and /word/media/* for images, all wired together by\n_rels/.rels, word/_rels/document.xml.rels\
    \ and [Content_Types].xml.\n\nA table's data lives in word/document.xml inside <w:body>, in a <w:tbl>\n\
    element. The hierarchy from table down to text is:\n\n  w:tbl                (table)\n    w:tblPr\
    \            (table properties: style, width, borders, layout)\n    w:tblGrid          (grid definition)\n\
    \      w:gridCol        (one per logical column; w:w = width in twips)\n    w:tr               (row)\n\
    \      w:trPr           (row properties: height, cantSplit, tblHeader)\n      w:tc             (cell\
    \ — a real cell, not a grid position)\n        w:tcPr         (cell properties: tcW, gridSpan, vMerge,\
    \ shd, borders)\n        w:p            (paragraph)\n          w:r          (run)\n            w:rPr\
    \      (run properties: b, i, sz, color)\n            w:t        (the actual text)\n\nRows are siblings\
    \ under w:tbl, not nested in each other; cells are\nsiblings under w:tr; a cell always owns at least\
    \ one w:p (an empty cell\nis an empty paragraph).\n"
- id: docx-model-02
  answer: "Why: Word splits a logical string into as many runs as it needs — an edit,\na spell/grammar\
    \ break, a formatting change, a field, an autocorrect, or a\nrPr change mid-word all start a new run.\
    \ So \"Widget C\" may be stored as\n\"Widget\" + \" C\", or \"Wid\" + \"get C\", or spread across\
    \ two paragraphs of\nthe cell. No single run.text then equals the whole string, and a run-wise\nequality\
    \ test misses it. (Also note cell.text / run.text normalizes\nnothing: tabs, line breaks as <w:br/>,\
    \ and non-breaking spaces can make a\nvisually identical string unequal.)\n\nReliable approach: search\
    \ the cell's full text, not its runs:\n  hay = \"\".join(p.text for p in cell.paragraphs)   # or cell.text\n\
    \  if hay.strip() == \"Widget C\": ...\nand for a replace, rebuild the cell rather than patching a\
    \ run:\n  keep the target paragraph's pPr, remove its runs, add one run carrying\n  the paragraph's\
    \ original rPr, then set run.text = replacement.\nFor a true same-formatting replace, walk all runs\
    \ of all paragraphs,\n  accumulate a concatenated string, locate the offset there, and split\n  runs\
    \ at the boundaries so the replacement lands in one run.\n"
- id: docx-model-03
  answer: "Table and cell widths (w:tblW/@w:w, w:tcW/@w:w, w:gridCol/@w:w,\nw:trHeight/@w:val) are in\
    \ twips (dxa) — twentieths of a point:\n  1 twip  = 1/20 pt\n  20 twip = 1 pt\n  72 pt   = 1 inch\
    \ = 1440 twips\n  1 cm    = 567 twips (approximately)\n\nImage sizes are in EMUs (English Metric Units),\
    \ an integer unit designed\nso that conversions are exact:\n  1 EMU   = 1/360000 cm = 1/914400 inch\
    \ = 1/12700 pt\n  1 pt    = 12700 EMU\n  1 inch  = 914400 EMU\n  1 cm    = 360000 EMU\nThey appear\
    \ as wp:extent/@cx and @cy on inline and anchored drawings, and\nas a:ext/@cx and @cy on pictures\
    \ inside runs.\n\n(Word 2010+ also permits w:w with w:type=\"dxa\"|\"pct\"|\"nil\"|\"auto\";\npercentages\
    \ are 50ths of a percent — 5000 = 100%.)\n"
- id: docx-locate-01
  answer: "Find the table: don't hard-code an index. Discriminate on content:\niterate doc.tables (and,\
    \ if needed, recurse into nested tables via\ncell.tables), take the first row or a caption paragraph,\
    \ and match a\ndistinctive marker — a header cell such as \"Invoice #\" / \"Total\", a\ncaption \"\
    Table 3: Invoice\", or a known column header set. Prefer a\nunique marker plus a column signature\
    \ (header texts) over \"the table\nwith N columns\". python-docx has no caption API, so if captions\
    \ exist,\nwalk document body elements in order and pair the w:p caption with the\nfollowing w:tbl.\n\
    \nFind the row: within the candidate table, scan table.rows and compare\nrow.cells[i].text (full cell\
    \ text, stripped) — a run-wise comparison\nwill miss text split across runs. Match on the item-name\
    \ column, and\nideally confirm with a second column (e.g. the quantity/price) so a\npartial match\
    \ elsewhere doesn't hit.\n\nCheck before editing:\n  - merged cells: row.cells repeats the same cell\
    \ for gridSpan/vMerge\n    positions, so index != unique cell; confirm against row._tr.tc_lst\n  \
    \  if you index by position.\n  - the cell's text may be a field or contain w:br/w:tab — verify with\n\
    \    the raw XML before rewriting.\n  - the row is not a header/repeat row (w:trPr/w:tblHeader) or\
    \ part of\n    a vertical merge.\n  - you matched the intended table (duplicate tables, nested tables).\n\
    \  - dependent numbers (line total, subtotal, tax, grand total) and any\n    { =SUM(ABOVE) } fields\
    \ that must be recomputed after the edit.\n"
- id: docx-locate-02
  answer: "row.cells is grid-position based, not cell-element based. A cell with\nw:gridSpan=\"2\" occupies\
    \ two grid columns, so row.cells returns that SAME\ncell object twice — e.g. for a 3-column grid where\
    \ cell 1 spans two\ncolumns you get [c0, c0, c2]. The list length equals the number of grid\ncolumns\
    \ (from w:tblGrid), not the number of w:tc elements.\n\nConsequences:\n  - \"the second cell\" by\
    \ index (index 1) is the merged cell itself, the\n    identical object as index 0. Deleting \"it\"\
    \ deletes the whole\n    two-column-wide cell and leaves the row one grid column short —\n    python-docx\
    \ will not raise, and row.cells afterwards returns fewer/\n    shifted entries. There is no per-grid-position\
    \ w:tc to remove.\n  - Iterating row.cells to count columns over-counts (you count grid\n    positions,\
    \ with duplicates), so any loop that edits \"each cell\"\n    writes to the merged cell multiple times\
    \ and skips nothing real.\nCorrect: count real cells with len(row._tr.tc_lst) (or\nrow._tr.tc_lst\
    \ / xpath w:tc), and address merged cells by their tc\nelement or by de-duplicating the objects (id())\
    \ from row.cells.\n"
- id: docx-rowdel-01
  answer: "There is no public deletion API: neither table.delete_row() nor\nrow.delete() exists in python-docx\
    \ (there is table.add_row() and\ntable.add_column(), but no counterparts for removing).\n\nDelete\
    \ the underlying <w:tr> element yourself:\n\n    tr = table.rows[i]._tr          # or row._tr\n  \
    \  tr.getparent().remove(tr)\n\nequivalently table._tbl.remove(tr). Word reflows the table when the\n\
    file is reopened, so no gap needs closing. Afterwards re-derive anything\nthe deleted row fed: cached\
    \ { =SUM(ABOVE) } results, subtotals, tax and\ngrand totals — those will otherwise still show the\
    \ old numbers.\n"
- id: docx-rowdel-02
  answer: "If you just remove the row, the vertical-merge group is left orphaned.\nThe rows that carried\
    \ <w:vMerge/> (continue, i.e. no w:val) now start\nthe group with no <w:vMerge w:val=\"restart\"/>\
    \ above them. Word then\neither treats the leftover continuation cells as one merged cell whose\n\
    text is taken from the first continuation cell (losing the deleted\nrow's contribution to the merge\
    \ and shifting content up), or renders\nthe merge inconsistently/broken — the restart is simply missing.\
    \ The\ndeleted row's content is gone either way, but the merge structure and\nwhat remains visible\
    \ are not what you intended.\n\nCorrect handling depends on intent:\n  - Deleting the whole merged\
    \ block: remove the restart row AND every\n    continuation row that belongs to the same merge group\
    \ (all rows\n    until the next row whose first cell has no w:vMerge, i.e. the\n    merge ends). Then\
    \ no vMerge tags are orphaned.\n  - Deleting only the restart row but keeping the continuation rows:\n\
    \    promote the next row's first cell to restart by setting\n    <w:vMerge w:val=\"restart\"/> on\
    \ it (and carry over any content you\n    want visible, since the restart cell supplies the group's\
    \ text).\n  - Ending the merge above the deleted row: instead remove/convert the\n    continuation\
    \ cells below, clearing w:vMerge so the remaining rows\n    become independent cells.\nAfter editing,\
    \ re-read the XML and verify every vMerge group still has\nexactly one restart followed by its continuations,\
    \ and recompute totals.\n"
- id: docx-rowdel-03
  answer: "The Total still shows the OLD sum — including the deleted row. The field\nis stored as its\
    \ instruction plus a cached result (the runs between the\nw:fldChar separate and end, or the text\
    \ inside w:fldSimple). Word does\nnot recalculate it merely on open; python-docx certainly does not.\
    \ So\nthe saved file displays a stale total, and only a manual update by the\nuser (Ctrl+A F9, or\
    \ choosing to update fields) fixes it.\n\nHandle it yourself:\n  1. Compute the correct new total\
    \ from the surviving table data.\n  2. Keep the field (instruction and fldChars) intact; write the\
    \ new\n     value into the cached-result run — the w:t between separate and\n     end, or the w:t\
    \ inside the w:fldSimple — preserving the number\n     format (decimals, thousands separator, currency\
    \ symbol).\n  3. Never use cell.text = ... on the field cell: that clears the\n     content and destroys\
    \ the field, leaving plain text instead of a\n     live SUM.\n  4. Optionally set <w:updateFields\
    \ w:val=\"true\"/> in settings.xml;\n     it only prompts Word to ask the user about updating fields\
    \ on\n     open — it is not a substitute for writing the value.\nAlso fix every other dependent number\
    \ (subtotal, tax, grand total,\nfigures repeated in the body text) in dependency order.\n"
- id: docx-rowdel-04
  answer: "Because the row still exists. Setting w:trHeight w:hRule=\"exact\" to 0,\nwhite w:color, or\
    \ w:vanish on the runs only hides it visually; the\n<w:tr>, its w:tc elements and all their text remain\
    \ in\nword/document.xml. Therefore:\n  - the text is still found by search (Ctrl+F, python-docx, pandoc),\n\
    \    still copies, still counts for word count and for spell check;\n  - { =SUM(ABOVE) } and any other\
    \ aggregation still include it, so the\n    Total is wrong-by-concealment — it counts a row nobody\
    \ can see;\n  - screen readers, PDF export with hidden text shown, and anyone who\n    unhides or\
    \ changes the theme color sees the row; white-on-white\n    also survives as data and can leak.\n\
    A deletion must remove the element.\n\nProve it is really gone:\n  - assert len(table.rows) decreased\
    \ by one;\n  - assert no w:tr in the table (or document.xml) still contains the\n    row's identifying\
    \ text — e.g. read the XML part and check\n    \"Gadget D\" is absent from the <w:tr> elements;\n\
    \  - confirm no w:trHeight hRule=\"exact\" val=0 and no w:vanish remains;\n  - re-open the saved file\
    \ with a fresh python-docx Document and\n    search again — an in-memory check alone doesn't prove\
    \ what was\n    serialized;\n  - verify dependent totals were recomputed, since the deleted row's\n\
    \    numbers must no longer contribute.\n"
- id: docx-cell-01
  answer: "cell.text = \"SKU\" clears the cell's content (all paragraphs and runs,\nw:p/w:r and their\
    \ rPr/pPr) and writes a single plain run. So:\n\nSurvives (cell-level, stored in w:tcPr, untouched\
    \ by the setter):\n  - shading <w:shd fill=\"...\"> (the dark background),\n  - cell width w:tcW,\
    \ borders w:tcBorders, vertical alignment,\n  - gridSpan/vMerge, margins, table-level style/banding.\n\
    \nLost (content-level):\n  - bold (w:b) and the 9 pt size (w:sz) — the new run has no rPr, so\n  \
    \  it falls back to the style/default, typically 11 pt non-bold;\n  - white text color (w:color) —\
    \ likewise reset, so the text becomes\n    dark on the dark shading and is effectively unreadable;\n\
    \  - font/rPr of any kind, paragraph style, alignment, indentation;\n  - any extra paragraphs, fields,\
    \ tabs, breaks, or non-text content\n    that lived in the cell.\n\nNet: the cell still looks shaded,\
    \ but its typography is reset. To edit\nsafely, change the text of the existing run (or clone its\
    \ rPr onto the\nnew run) instead of assigning cell.text.\n"
- id: docx-cell-02
  answer: "Changing Qty 5 -> 12 invalidates everything derived from that quantity.\nIn dependency order\
    \ you must update:\n\n  1. The line's amount (qty x unit price) in that row's total cell.\n  2. The\
    \ subtotal / net amount.\n  3. Any tax or VAT computed on that subtotal.\n  4. The grand total / balance\
    \ due.\n  5. Any { =SUM(ABOVE) } or other Word field showing those figures —\n     their cached results,\
    \ written into the w:t between the separate\n     and end fldChars (or inside w:fldSimple), never\
    \ via cell.text,\n     which would delete the field.\n  6. The same numbers repeated in the prose\
    \ (e.g. \"Total: $1,432.00\n     is due...\"), in headers/footers, in a summary paragraph, and in\n\
    \     any other table or text box that repeats them.\n  7. Cross-references/bookmarks pointing at\
    \ those numbers, and\n     numbering if the row count changed.\n\nMethod: recompute all of it from\
    \ the table's own data in Python (do not\nhand-edit), then write each result into its cell/field in\
    \ the existing\nnumber format — same decimal places, thousands separator and currency\nsymbol — and\
    \ preserve the paragraph's alignment. Finally re-read the\nsaved file and assert the arithmetic ties\
    \ out (sum of line amounts ==\nsubtotal; subtotal + tax == total).\n"
- id: docx-cell-03
  answer: "python-docx does not participate in revision tracking: it has no notion\nof w:ins/w:del. Rewriting\
    \ the w:t yourself is a silent, untracked edit —\nWord will NOT show it as a change, even though w:trackRevisions\
    \ is on in\nsettings.xml (that flag only affects edits made inside Word). So you get\na \"clean\"\
    \ edit by construction, but you must consider several things:\n\n  - Existing revision markup in the\
    \ cell may be present. Runs inside\n    <w:ins> or <w:del> are not direct children of w:p, so paragraph.runs\n\
    \    and (for deletions) w:delText are invisible to naive iteration — you\n    may read a partial\
    \ text, and if you clear/rewrite you can destroy\n    someone's pending tracked history or produce\
    \ text that conflicts\n    with accepted/rejected revisions.\n  - Whether the user actually wants\
    \ it tracked. If yes, you must emit\n    the markup yourself (wrap the old text in w:del/w:delText\
    \ and the\n    new text in w:ins with author/date/id), or tell them to edit in\n    Word with tracking\
    \ on.\n  - Whether to turn tracking off first. Removing w:trackRevisions (or\n    asking the user)\
    \ avoids the confusion of a document that is\n    \"tracking\" but silently untracked, and avoids\
    \ Word later marking\n    your change when the user edits nearby.\n  - Unaccepted revisions elsewhere\
    \ mean the file may be in an\n    intermediate state; decide whether to accept/reject them before\n\
    \    editing, and record the edit (e.g. a comment) if auditability\n    matters.\nBottom line: the\
    \ edit applies cleanly to the XML, but track-changes\nstate, existing w:ins/w:del content, and the\
    \ user's expectation of\ntraceability all need to be settled first.\n"
- id: docx-relayout-01
  answer: "table.add_row() only APPENDS: it creates a new <w:tr> at the END of\nw:tbl (after the Total\
    \ row), with one empty <w:tc> per w:gridCol and no\ninherited formatting — no shading, no borders\
    \ from the style/banding\nbeyond what the table style gives, no content.\n\nTo insert a correctly\
    \ formatted row in the middle (after \"Service G\",\nbefore Total), deep-copy an existing body row\
    \ and move it:\n\n    import copy\n    src = table.rows[k]._tr            # a body row like the one\
    \ you want\n    new_tr = copy.deepcopy(src)\n    target_tr = table.rows[i]._tr       # the \"Service\
    \ G\" row\n    target_tr.addnext(new_tr)           # inserts immediately after it\n\nThen set the\
    \ new row's cell text run by run (clear the copied content,\nkeep the rPr/pPr so formatting matches),\
    \ and insert it in document order\n— addnext puts it exactly where you want without touching the Total\
    \ row.\nA manual alternative is constructing <w:tr>/<w:tc> XML and using\ntbl.insert(index, tr).\n\
    \nAfterwards: because the Total sits below, recompute and rewrite any\ncached { =SUM(ABOVE) }, subtotal,\
    \ tax and grand total, and check the\ncopied row's shading still matches its new band position.\n"
- id: docx-relayout-02
  answer: "table.add_column(width) appends a NEW <w:gridCol> at the END of\nw:tblGrid and appends one\
    \ <w:tc> (with w:tcW set from width) to the END\nof every <w:tr>. So the column appears on the far\
    \ RIGHT of the table,\nnot after the Item column; and it widens the table.\n\nTo get \"after Item\"\
    \ and keep it inside the page and looking like the\nother columns you must additionally:\n  - Move\
    \ the grid position: insert the new w:gridCol after the Item\n    gridCol (not just append it), since\
    \ grid order defines column order.\n  - Move each row's new w:tc to the position after the Item w:tc\
    \ inside\n    its w:tr — cell order within the row defines visual column order.\n  - Keep total width\
    \ within the text column: the table's\n    w:tblPr/w:tblW (and the sum of gridCol widths) must not\
    \ exceed the\n    printable width (page width minus margins, in twips). Shrink the\n    existing gridCol\
    \ widths (and matching w:tcW in every cell) to make\n    room, or distribute width across all columns.\n\
    \  - Match formatting: copy the target column's cell formatting into the\n    new cells — w:tcPr (borders,\
    \ w:shd banding per row, w:tcMar,\n    vertical alignment), and the paragraph/run formatting for header\
    \ vs\n    body; give the header cell the same bold/size/shading as its\n    neighbours and body cells\
    \ the row's banding fill.\n  - Respect layout mode: if w:tblLayout is fixed, tcW/gridCol drive\n \
    \   widths; if autofit, Word may recalculate from content, so set\n    explicit widths or switch to\
    \ fixed to keep the design.\nFinally verify: sum(gridCol) == tblW, every row has the same number of\n\
    real w:tc as the grid (accounting for gridSpan), and the table still\nfits inside the margins.\n"
- id: docx-relayout-03
  answer: "Because w:gridCol is only one of the sources Word uses for column\nwidths. Word derives the\
    \ displayed width from the table layout as a\nwhole:\n\n  - w:tblPr/w:tblW (and w:tblInd) set the\
    \ table's overall width; if it\n    still says the old total, Word keeps the old total and redistributes.\n\
    \  - The per-cell w:tcPr/w:tcW values also participate — with fixed\n    layout Word takes the widths\
    \ from the tblGrid but reconciles them\n    with the cells' tcW (notably the first row's), so stale\
    \ tcW values\n    pull the columns back.\n  - If the table is in autofit mode (w:tblLayout w:type=\"\
    autofit\", or\n    no explicit fixed layout / tblW type=pct or auto), Word ignores your\n    grid\
    \ widths and recomputes from the cell content and available\n    space, so the old widths (or content-driven\
    \ ones) reappear.\n\nSo you must update, consistently:\n  1. every w:gridCol/@w:w in w:tblGrid;\n\
    \  2. the w:w (and w:type) of w:tblW in w:tblPr, so it equals the sum\n     of the grid columns and\
    \ fits within the page margins;\n  3. every cell's w:tcPr/w:tcW across all rows (or at least all rows\n\
    \     that Word consults — safest is all of them) to the new width;\n  4. w:tblLayout w:type=\"fixed\"\
    \ if you want Word to honour the numbers\n     rather than autofit;\n  5. any w:trHeight or column-spanning\
    \ cells affected by the resize.\nRe-open the saved file to confirm, since editing only the grid in\
    \ the\nXML is exactly the case where Word silently re-derives the widths.\n"
- id: docx-tblins-01
  answer: "doc.add_table() always appends the table at the END of the document body\n(immediately before\
    \ the final sectPr). python-docx has no \"insert at the\ncursor / after paragraph X\" API and the\
    \ \"Notes:\" paragraph is ignored.\nTo place it correctly, build the table and then move its XML element\
    \ in the\nlxml tree:\n    tbl = doc.add_table(rows=1, cols=3, style=<existing style id>)\n    p =\
    \ <the paragraph whose text is \"Notes:\">\n    p._p.addnext(tbl._tbl)\n(p._p.addprevious(tbl._tbl)\
    \ if it belongs before). addnext() physically\nrelocates the same w:tbl node, so the table, its grid\
    \ and its relationship\nids remain intact; no re-creation is needed. Afterwards confirm the order:\n\
    body element children should read ... paragraph(\"Notes:\"), w:tbl, ...\n"
- id: docx-tblins-02
  answer: "KeyError is raised because python-docx looks the name up in THIS document's\nstyles part (word/styles.xml)\
    \ and raises KeyError when no style with that\nname/styleId exists. Company templates typically carry\
    \ only a handful of\nlatent/used styles - \"Grid Table 4 Accent 1\" is a built-in style of Word's\n\
    default template, not of this file - so the lookup fails. It also would not\nexist until the document\
    \ is opened/saved once by Word.\nWays to get a ruled table:\n1. Use a style the document already defines\
    \ - iterate doc.styles (and\n   tbl.style of an existing ruled table) and reuse that name/styleId.\n\
    2. Deep-copy the w:tblPr (including w:tblStyle) from an existing ruled\n   table in the document onto\
    \ the new one.\n3. Set borders directly in w:tblPr/w:tblBorders (w:val=\"single\" for\n   insideH,\
    \ insideV, top, bottom, left, right with a thin w:sz), which is\n   template-independent.\n"
- id: docx-tblins-03
  answer: "By default the table spans the full text block width of the section - i.e.\npage width minus\
    \ the left and right margins (python-docx passes\nsection.page_width - left_margin - right_margin\
    \ into the w:tbl construction)\n- with w:tblW set to 100% of that area, autofit behaviour, and all\
    \ N columns\ndivided evenly. That is almost never the width/column layout of an existing\ninvoice\
    \ table.\nTo match the invoice table: set table.autofit = False, then copy the\nlayout from the reference\
    \ table, column by column:\n    for i, ref_col in enumerate(ref_table.columns):\n        table.columns[i].width\
    \ = ref_col.width\n(setting a column width writes w:tcW on every cell in it; also copy/set\nw:gridCol\
    \ widths in the new w:tblGrid, and take tblW/tblLayout/tblInd from\nthe reference w:tblPr, or simply\
    \ copy the whole tblPr/tblGrid). Verify with\nsum(column widths) == sum(reference widths) and that\
    \ each row's cell widths\nequal the reference row's.\n"
- id: docx-imgrep-01
  answer: "The image bytes live in a separate OPC part (word/media/imageN.ext). The\npart is not referenced\
    \ from the XML directly: document.xml.rels maps an\nrId (e.g. rId7) to that part, and the picture's\
    \ drawing XML points at it\nwith <a:blip r:embed=\"rId7\"/> inside\nwp:inline/wp:anchor > a:graphic\
    \ > pic:pic. Position/size come from\nwp:extent (and a:ext, pic:spPr/xfrm) - EMU values - not from\
    \ the image file.\nTo replace one logo while keeping position and size:\n    rId, image = doc.part.get_or_add_image(\"\
    new_logo.png\")\n    for blip in picture_element.iter(qn('a:blip')):\n        if <this is the target\
    \ picture>:\n            blip.set(qn('r:embed'), rId)\n(note: doc.part.get_or_add_image exists; DocumentPart\
    \ has NO\nget_or_add_image_part). Do NOT touch wp:extent, so on-page size/position are\npreserved;\
    \ only scale explicitly if the new art has a different aspect\nratio. Then, only if no a:blip in the\
    \ whole part still points at the old\nrId, drop it: del doc.part.rels[old_rId]. Do not use part.drop_rel()\
    \ - it\ncounts @r:id references only and will drop a picture rel that is still used.\n"
- id: docx-imgrep-02
  answer: 'Because the two pictures share one image part. python-docx''s

    get_or_add_image() deduplicates: if the new bytes already exist in the

    package it reuses the existing part/rId, and identical source images added

    anywhere in the file are stored once. The logo on page 3 therefore has an

    a:blip whose r:embed resolves to the SAME part (rId) as the logo you edited,

    so overwriting image_part._blob (or the part''s bytes) changes what every

    reference to that part renders - the page-3 picture changes too.

    The fix is never to mutate shared part bytes: add a NEW image (new bytes

    give a new part/rId), repoint ONLY the target picture''s a:blip/@r:embed,

    leave the old rId alone while anything still uses it, and delete the old

    rel only after scanning all a:blip elements in that part (including

    headers/footers) for remaining references.

    '
- id: docx-imgrep-03
  answer: "doc.inline_shapes only enumerates drawings in the MAIN document body part.\nA logo repeated\
    \ at the top of every page lives in a header part -\nsection.header (and possibly different first\
    \ page / even-and-odd headers,\nsection.different_first_page_header_footer, or a footer). It may be\
    \ an\ninline drawing inside the header paragraph, or a VML/w:pict anchored\nlegacy image, and it is\
    \ reached through the section's header part and its\nown relationship table, not document.xml.rels.\n\
    Find and replace it by:\n    for section in doc.sections:\n        for part in (section.header, section.first_page_header,\n\
    \                     section.even_page_header, section.footer, ...):\n            for blip in part._element.iter(qn('a:blip')):\n\
    \                old = blip.get(qn('r:embed'))\n                ... repoint to the new rId ...\n \
    \   rId, _ = part.part.get_or_add_image(\"new_logo.png\")  # per part\n(iterate a:blip in each header/footer\
    \ element, verify the target by its\nrId/extent/position, then set r:embed to the rId added to THAT\
    \ part's rels;\ncheck w:pict/v:m for legacy images too). Header parts must be touched for\nevery section\
    \ that links to or defines that header.\n"
- id: docx-imgins-01
  answer: "doc.add_picture() creates a NEW paragraph at the end of the document body\nand puts the picture\
    \ in a run in it - it never inserts at the current\nparagraph. To place it after \"Approved by:\"\
    :\n    p = <the \"Approved by:\" paragraph>\n    run = p.add_run()\n    run.add_picture(\"signature.png\"\
    , width=Inches(1.5))\n(or build it elsewhere and move it: new_p._p.addnext(p._p)). Because only\n\
    width is given, python-docx computes height from the image's native pixel\ndimensions and DPI, so\
    \ the aspect ratio is preserved; the drawing is inline\n(wp:inline) with wp:extent set to 1.5in x\
    \ proportional height. If the\nsignature must be on its own line, add a new paragraph after \"Approved\
    \ by:\"\n(p.insert_paragraph_before on the following paragraph, or add then move)\nand add the picture\
    \ run to it instead.\n"
- id: docx-imgins-02
  answer: "Nothing is resized for you: the drawing keeps its 3in wp:extent and, since\na fixed-width cell\
    \ (autofit off / explicit tcW) does not grow, the picture\noverflows the cell - it is drawn over the\
    \ neighbouring cell/column or is\nclipped, the row may look broken, and depending on wrapping it can\
    \ push the\nlayout around. The table will not shrink to fit it.\nSize it to the usable width: cell\
    \ width minus the cell margins (default\n~0.08in left/right), so pass width at creation:\n    run.add_picture(\"\
    img.png\", width=Inches(1.2 - 0.16))\nor afterwards: shape.width = Inches(1.0); and because changing\
    \ .width alone\ndoes NOT rescale .height in python-docx, set height yourself to keep the\nratio -\
    \ height = width * (native_h / native_w), using PIL\n(Image.open(path).size) or shape.image. Set the\
    \ cell's width/autofit as\nneeded and confirm sum of column widths is unchanged.\n"
- id: docx-legacy-01
  answer: "No. python-docx only reads/writes OOXML packages (.docx/.dotx): it opens a\nzip and parses\
    \ word/document.xml. A Word 97-2003 .doc is a binary OLE/CFB\ncompound file - python-docx raises an\
    \ error (it cannot open it at all, and\nthere is no supported path to its tables).\nWorkflow: convert\
    \ first, then edit, then verify - and tell the user the\n.doc itself will not be updated unless they\
    \ ask for a conversion back:\n1. LibreOffice (best fidelity): soffice --headless\n   -env:UserInstallation=file:///tmp/lo_prof\
    \ --convert-to docx report.doc\n2. On macOS: textutil -convert docx report.doc (lossier - see the\n\
    \   textutil risk question)\n3. Or Word: Open > Save As \"Word Document (*.docx)\".\nThen open the\
    \ result with python-docx, make the row deletion, and re-save\nas .docx. Validate before/after (table\
    \ count, row count, cell text) because\nconversion can alter formatting; if a .doc deliverable is\
    \ mandatory, convert\nback with LibreOffice/Word and re-verify.\n"
- id: docx-legacy-02
  answer: "textutil is Apple's lightweight text engine, not a Word renderer. For a\ntable-editing task\
    \ it can silently drop or mangle structure: merged cells\nbecome separate cells, nested tables flatten,\
    \ cell widths/row heights,\nborders and table styles are lost, column counts change, tables can be\n\
    turned into tab-separated text, images/fields/numbering can disappear, and\nrows can be reordered\
    \ or split - so your \"delete row X\" may target a\ndifferent or non-existent row, and the output\
    \ may not even be a valid\ndocx.\nDetect it by structural comparison of source vs converted file:\n\
    - parse both and compare table count, per-table row count, per-row cell\n  count, gridCol count, presence\
    \ of gridSpan/vMerge, and the text of the\n  target row;\n- unzip and inspect word/document.xml (well-formedness,\
    \ w:tbl/w:tr/w:tc,\n  w:gridSpan, vMerge, media files present in word/media);\n- check [Content_Types].xml\
    \ and all rels are intact;\n- if possible, open the result in Word/LibreOffice and confirm it renders\n\
    \  without a repair prompt.\nIf counts/merges differ, redo the conversion with LibreOffice or Word\n\
    instead of textutil.\n"
- id: docx-legacy-03
  answer: "LibreOffice is effectively single-instance: the soffice wrapper sees an\nalready-running GUI\
    \ instance holding the user profile lock and forwards the\nrequest to it (or simply exits without\
    \ doing the work), so the --headless\n--convert-to invocation terminates immediately with no conversion\
    \ and no\noutput file; the open desktop window is the instance that \"absorbed\" it\n(and may show\
    \ a modal dialog instead of converting).\nRun it reliably from a script by giving the conversion its\
    \ own profile so a\nfresh, independent headless process starts:\n    soffice --headless --norestore\
    \ --invisible \\\n            -env:UserInstallation=file:///tmp/lo_profile_$RANDOM \\\n          \
    \  --convert-to docx --outdir \"$OUT\" file.doc\n(a distinct -env:UserInstallation URL avoids the\
    \ lock regardless of any\nrunning window). Then script defensively: check the exit code, poll for\
    \ the\nexpected output file for N seconds, verify its size/zip integrity, and\nclean up the temp profile.\
    \ Alternatively kill/quit the running instance\nfirst, but the isolated profile is the dependable\
    \ approach.\n"
- id: docx-verify-01
  answer: "Save to a staging file, validate it, and only then replace the original:\n1. Back up the original\
    \ (copy invoice.docx -> invoice.docx.bak) before any\n   edit; never write the script's output over\
    \ the only copy.\n2. doc.save(\"invoice.new.docx\") - a different path, so a failed write\n   cannot\
    \ corrupt the original.\n3. Verify the new file before trusting it:\n   - zipfile.ZipFile(...).testzip()\
    \ -> None (no CRC errors) and confirm the\n     expected parts exist ([Content_Types].xml, _rels/.rels,\n\
    \     word/document.xml, word/_rels/document.xml.rels);\n   - re-open with python-docx (Document(\"\
    invoice.new.docx\")) - a clean open\n     proves the package and XML parse;\n   - re-read the table\
    \ data and assert the edit: row deleted, cell values,\n     recomputed subtotal/tax/total present\
    \ in the right number format,\n     table/row/cell counts match expectation;\n   - parse every XML\
    \ part with lxml to confirm well-formedness and check\n     no dangling r:id/r:embed/r:idref references;\n\
    \   - if available, round-trip through Word/LibreOffice (or pandoc) and\n     confirm it opens with\
    \ no \"unreadable content\" repair prompt.\n4. Only if all checks pass, atomically replace the original:\n\
    \   os.replace(\"invoice.new.docx\", \"invoice.docx\") (and keep the .bak until\n   the user confirms).\
    \ Re-run the verification read on the final file.\n"
- id: docx-verify-02
  answer: "Opening in python-docx proves little: python-docx/lxml are lenient and will\nparse XML that\
    \ Word rejects against the stricter OOXML schema. Common\ntable-edit mistakes that produce \"Word\
    \ found unreadable content\":\n- Wrong element ORDER inside w:tbl / w:tr / w:tc (CT_Tbl requires w:tblPr\n\
    \  first, then w:tblGrid, then w:tr; CT_Row requires w:trPr before w:tc;\n  CT_Tc requires w:tcPr\
    \ before content). Hand-appended nodes land at the\n  end and violate the sequence.\n- Mismatched\
    \ structure: a w:tr without any w:tc, a row whose cell count /\n  gridSpan does not reconcile with\
    \ w:tblGrid's w:gridCol count, a missing\n  or duplicated w:tblGrid, an orphaned w:tblPr/w:tblStyleRef,\
    \ or two\n  w:tblGrid elements.\n- Broken text/XML: unescaped &, <, > or a raw control character inside\n\
    \  w:t, truncated XML, mismatched tags, duplicate attribute, wrong/missing\n  namespace prefixes (w:,\
    \ r:, wp:, a:) or a bad xml:space value.\n- Broken relationships: an r:embed/r:id (drawing, hyperlink,\
    \ field,\n  bookmarked image) pointing to a relationship that was deleted, a\n  duplicated rId in\
    \ a .rels file, a rel whose target part is missing, or\n  a removed part still listed in [Content_Types].xml\
    \ (or a present part\n  missing from it).\n- Damaged field runs: deleted w:fldChar begin/separate/end\
    \ or an\n  instruction (w:instrText / w:fldSimple) left without its result run -\n  e.g. writing cell.text\
    \ = over a field cell.\n- Invalid numeric attributes: non-integer w:w/w:tcW/gridCol widths,\n  negative\
    \ or zero w:trHeight, bad w:shd fill values, invalid\n  dxa/pct/nil units.\nDetect by validating against\
    \ word/schema (or opening in Word) and by\nchecking element order and tblGrid reconciliation explicitly\
    \ after edits.\n"
- id: docx-verify-03
  answer: "Removing the w:drawing only removes the XML pointer. The image bytes stay\nin the package as\
    \ word/media/imageN.ext as long as any relationship points\nat them, so a confidential logo can still\
    \ be extracted from the file. To be\nsure it is gone:\n1. In EVERY part that could reference it (document\
    \ body AND all headers/\n   footers of every section, plus footnotes/comments), scan\n   part.element.iter(qn('a:blip'))\
    \ and w:pict/v:imagedata for the old\n   rId; also scan the drawing's docPr/name/descr for embedded\
    \ filenames.\n2. When nothing references it, drop the relationship from that part:\n   del part.rels[old_rId]\
    \ (do NOT use part.drop_rel(), which inspects\n   @r:id only and would drop a picture rel another\
    \ image still uses).\n3. Re-save the document. python-docx serializes only parts reachable\n   through\
    \ the relationship graph, so an orphaned image part is omitted\n   from the new package.\n4. Verify\
    \ on the SAVED file, not the in-memory one: unzip it and list\n   word/media - the old file must be\
    \ absent; grep all XML for the old rId,\n   the original r:embed, and the original docPr name; compare\
    \ SHA-256 of\n   every file in word/media against the confidential logo's hash; confirm\n   the logo's\
    \ bytes do not appear anywhere in the zip (e.g. search the\n   raw archive for a distinctive byte\
    \ sequence). Only then is it actually\n   removed.\n"
