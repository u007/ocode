- id: docx-model-01
  answer: "A .docx is not a single binary document: it is a ZIP archive (an OPC package) holding\na set\
    \ of XML \"parts\" plus relationships. The main body part is `word/document.xml`;\nother parts are\
    \ `word/styles.xml`, `word/numbering.xml`, `word/settings.xml`,\n`word/headerN.xml` / `word/footerN.xml`,\
    \ `word/media/*` (images), and `[Content_Types].xml`\nplus `_rels/*.rels` relationship files. A table's\
    \ data lives as XML inside whichever part\ncontains it — normally `word/document.xml`, but a table\
    \ in a header/footer lives in that\nheader/footer part.\n\nThe WordprocessingML element chain for\
    \ a table (namespace prefix `w`) is:\n- `w:tbl` — the table element.\n  - `w:tblPr` — table properties\
    \ (style, width, borders, layout, alignment).\n  - `w:tblGrid` — the column grid; one `w:gridCol`\
    \ per column (each carries `w:w` width).\n  - `w:tr` — a table row (plus `w:trPr` for row properties\
    \ such as height and header-row).\n    - `w:tc` — a table cell (plus `w:tcPr` for cell width, shading,\
    \ borders, `w:gridSpan`,\n      `w:vMerge`, vertical alignment, margins).\n      - `w:p` — a paragraph\
    \ inside the cell (plus `w:pPr`).\n        - `w:r` — a run of characters sharing formatting (plus\
    \ `w:rPr`).\n          - `w:t` — the actual text characters.\nSo: table → row → cell → paragraph →\
    \ run → text. (Nested tables repeat the `w:tbl` chain\ninside a cell.)\n"
- id: docx-model-02
  answer: "Word splits text into runs arbitrarily, so a visible string is frequently broken across\nseveral\
    \ `w:r`/`w:t` elements. \"Widget C\" may exist as runs \"Widget \" + \"C\", or \"Wid\" +\n\"get C\"\
    , or with different `rPr` (e.g. a spell-check/highlight boundary), or the text may\nsit inside a hyperlink\
    \ (`w:hyperlink`), a field, or a `w:ins` revision. Comparing a single\n`run.text` to the whole phrase\
    \ therefore fails even though the cell renders the phrase.\n\nReliable approach:\n1. Work at the cell/paragraph\
    \ level, not the run level. Build the full visible text:\n   `''.join(p.text for p in cell.paragraphs)`\
    \ (or per `w:p`, concatenate all `w:t`\n   descendants so hyperlinks/fields are included).\n2. Locate\
    \ the target span in that concatenated string.\n3. To replace, get each paragraph's runs, compute\
    \ how much of the phrase falls in each\n   run, and rewrite: put the replacement text (plus the run's\
    \ leading prefix) into the\n   first affected run, clear the fully-covered middle runs (`run.text\
    \ = \"\"`), and put the\n   trailing suffix in the last affected run. This preserves each run's `rPr`.\n\
    4. Only collapse runs whose `rPr` is byte-identical (assert this first); if `rPr` differs,\n   don't\
    \ merge — strip and re-add text run-by-run.\n5. Do not use naive byte `str.replace` on `document.xml`:\
    \ the literal bytes of the phrase\n   may not exist contiguously and a replace can silently no-op\
    \ or corrupt XML. Parse with\n   lxml, prove you found the right cell by its concatenated text, then\
    \ splice.\n6. Verify after saving: reopen and re-read the concatenated cell text.\n"
- id: docx-model-03
  answer: "WordprocessingML uses two different unit systems.\n\nTable/cell geometry (widths, margins,\
    \ indents)\n- Unit: the **twentieth of a point**, called `dxa` or a \"twip\".\n- 20 dxa = 1 point;\
    \ 1440 dxa = 1 inch; 1 dxa = 1/1440 in.\n- `w:tblW/w:w`, `w:gridCol/w:w`, `w:tcW/w:w`, `w:tblCellMar`\
    \ values, and `w:trHeight`\n  heights are all in dxa (unless the width uses `w:type=\"pct\"`, which\
    \ is fiftieths of a\n  percent, or `w:type=\"auto\"`).\n- Conversion: points = dxa / 20; inches =\
    \ dxa / 1440.\n\nDrawingML / image sizes\n- Unit: the **EMU (English Metric Unit)**.\n- 914400 EMU\
    \ = 1 inch; 12700 EMU = 1 point (914400 / 72); 360000 EMU = 1 cm.\n- `wp:extent/@cx,cy`, `a:ext/@cx,cy`,\
    \ and `w:drawing` inline/anchor extents are EMU.\n- Conversion: points = EMU / 12700; inches = EMU\
    \ / 914400.\n\nIn python-docx, `Length` values are stored internally as EMU and can be read/written\
    \ as\n`Pt(...)`, `Inches(...)`, `Cm(...)`, `Twips`-style via `Emu` conversions; `docx.shared`\nprovides\
    \ `Pt`, `Inches`, `Cm`, `Mm`, `Emu`, `Twips`.\n"
- id: docx-locate-01
  answer: "Find the table by identity, not solely by index. Iterate `doc.tables` and match on\ndistinguishing\
    \ content:\n- Header row text: e.g. find the table whose first row's cells concatenate to something\n\
    \  containing \"Invoice\", \"Item\", \"Qty\", \"Rate\", \"Amount\", \"Total\".\n- Style name: `table.style.name`\
    \ (e.g. \"Table Grid\", a custom invoice style) narrows it.\n- Surrounding context: walk the body\
    \ elements (`doc.element.body`) to associate a caption\n  paragraph like \"Invoice 12345\" with the\
    \ following `w:tbl`.\n\nFind the row by matching the *concatenated* cell text, e.g. for each `row`\
    \ build\n`' '.join(''.join(p.text for p in c.paragraphs) for c in row.cells)` and test for\n\"Gadget\
    \ D\" (with normalization of whitespace/case). Don't rely on `run.text` equality\n(see docx-model-02).\
    \ Also skip the Total row by matching the Item column specifically\nrather than any cell.\n\nBefore\
    \ editing, check:\n- `row.cells` indices vs the actual grid: horizontal merges make `row.cells` repeat\
    \ the\n  same cell (see docx-locate-02), so dedupe by `cell._tc` identity.\n- Vertical merges: does\
    \ the target's first cell carry `w:vMerge`?\n- The target text may be split across runs, or inside\
    \ a hyperlink/field.\n- The target may be a field result (`w:fldSimple` or `instrText`) rather than\
    \ plain text.\n- Whether Track Changes is active (`w:trackRevisions` in settings).\n- Whether the\
    \ Total/derived cells will need recomputation after the edit.\n- Which row index you're editing and\
    \ that no other table shares the same headers.\n"
- id: docx-locate-02
  answer: "When a cell is horizontally merged, the underlying meaning is that one `w:tc` carries\n`w:tcPr/w:gridSpan[@w:val=\"\
    2\"]` and covers two grid columns. python-docx's `row.cells`\nis defined by the table *grid*, not\
    \ by physical cells: it returns one entry per grid\ncolumn, and for a `gridSpan` it returns the **same\
    \ `_Cell` object repeatedly** for each\nspanned column. So for a first row whose first cell spans\
    \ two columns, `row.cells[0]`\nand `row.cells[1]` are the same cell (same `_tc`), and `len(row.cells)`\
    \ equals the number\nof grid columns, not the number of distinct cells.\n\nConsequences:\n- Counting\
    \ columns by iterating `row.cells` overcounts distinct cells and misaligns every\n  later cell's index.\n\
    - Deleting \"the second cell\" by index would operate on the merged cell and either do\n  nothing\
    \ distinct or destroy a cell that other rows still expect.\n- Writing by index can double-write the\
    \ merged cell.\n- The correct count of physical cells requires deduping by `cell._tc` identity (or\
    \ reading\n  `w:gridSpan`), and any width/column math must use the grid columns covered.\n"
- id: docx-rowdel-01
  answer: 'No. python-docx has no `table.delete_row()` and no `row.delete()` (nor `cell.delete()`).

    The row and cell APIs are read-mostly; removal is done by manipulating the underlying XML.


    Pattern: `tr = table.rows[idx]._tr; tr.getparent().remove(tr)` (equivalently

    `table._tbl.remove(tr)`). The remaining rows stay contiguous — nothing shifts and there

    is no gap to close. You must separately handle anything derived from the row: recompute

    and rewrite Total cells, and fix vertical/horizontal merges if the removed row was part

    of a merge group.

    '
- id: docx-rowdel-02
  answer: "A vertical merge is expressed on the *first cell of each row* in the group: the top row's\n\
    cell has `<w:vMerge w:val=\"restart\"/>` (the anchor that starts the merge and holds the\nvisible\
    \ content), and each subsequent row's first cell has `<w:vMerge/>` (i.e. continue).\nThe continuation\
    \ cells are typically empty.\n\nIf you remove only the restart row, you orphan the group: the next\
    \ rows' `w:vMerge` cells\nhave no origin. Word then has no anchor — behavior is undefined/flaky: the\
    \ continuation\ncells may render as empty standalone cells, the merge may collapse or appear broken,\
    \ and\nthe visible text that lived in the anchor is gone entirely. You also may leave a\n`vMerge`\
    \ continuation as the first row of the table.\n\nCorrect handling depends on intent:\n- If the whole\
    \ merged region (all rows of the group) is being removed, delete every row in\n  the group together,\
    \ not just the anchor.\n- If the merge must survive over the remaining rows, promote the first remaining\
    \ row's\n  cell to a new anchor: set its `w:vMerge` to `w:val=\"restart\"` (and give it the visible\n\
    \  text that the old anchor held).\n- If the merge should no longer exist, remove the `w:vMerge` elements\
    \ from all cells in\n  the group so they become ordinary cells.\nAfter the edit, verify the sequence\
    \ of `w:vMerge` values across the column is\nwell-formed (one `restart` followed by zero or more continues,\
    \ then a non-merged row).\n"
- id: docx-rowdel-03
  answer: "`{ =SUM(ABOVE) }` is a Word *field*, not static text. A field stores two things: the\nfield\
    \ instruction (usually in `w:instrText`, e.g. `=SUM(ABOVE)`) and a **cached result**\nrendered in\
    \ a `w:r`/`w:t`. python-docx does not evaluate fields and does not trigger\nWord's field update. When\
    \ the file is opened, Word generally displays the *cached* result\nuntil a recalculation happens (F9\
    \ / print preview / update-all on open is not guaranteed\nfor formula fields). So the Total will most\
    \ likely still show the old pre-deletion value,\nand even if Word recalculates it, `=SUM(ABOVE)` sums\
    \ the amounts physically above the cell\nso it will then correct itself — but you cannot rely on that,\
    \ and the cached value is\nwrong in any program that reads the field result without evaluating it.\n\
    \nHandling: don't trust the field. Recompute the total yourself from the remaining rows'\namount cells,\
    \ then either\n- replace the field with plain literal text equal to the recomputed total (ensure the\n\
    \  field instruction is removed so Word can't recalculate to something else), or\n- update the field's\
    \ cached result run to the new value while leaving the instruction,\n  accepting that Word may overwrite\
    \ it on the next update.\nIn practice, for a deterministic document, convert the computed total to\
    \ plain text.\nAlso note `=SUM(ABOVE)` depends on relative position, so it must be recomputed, never\n\
    carried over.\n"
- id: docx-rowdel-04
  answer: "Height-0, white-text, or hidden-run (`w:vanish`) tricks are *visual* obfuscations, not\ndeletions.\
    \ The `<w:tr>` element remains in the `w:tbl`, so the row still:\n- occupies a real row in the table\
    \ structure (`len(table.rows)` is unchanged);\n- contributes cells to the grid and to any merge logic;\n\
    - can still be included in field calculations such as `=SUM(ABOVE)` and can affect\n  column fit/autofit;\n\
    - reappears if formatting is cleared, hidden text is revealed, or the run color is\n  changed (e.g.\
    \ by a mail merge / style override), and may still be found by search,\n  accessibility tools, or\
    \ text extraction (pandoc, `docx2txt`, etc.);\n- may still be copied/found by other consumers, so\
    \ downstream tooling sees data the\n  author believed was deleted.\n\nProving real removal:\n1. Re-read\
    \ the saved file; confirm the table's row count dropped by exactly one.\n2. Search *every* XML part\
    \ (not just `word/document.xml` — headers/footers too) for the\n   row's unique content and confirm\
    \ no `w:tr` still contains it.\n3. Confirm there is no leftover `w:vanish`/zero-height row masquerading\
    \ as deleted.\n4. Optionally extract text (pandoc/python-docx) and confirm the row's text is absent.\n"
- id: docx-cell-01
  answer: "`cell.text = \"SKU\"` is implemented by clearing the cell's content and writing a single new\n\
    paragraph with a single run. Specifically it removes all existing `w:p` children of the\n`w:tc` and\
    \ adds one fresh `w:p` → `w:r` → `w:t`.\n\nWhat survives: the cell-level properties in `w:tcPr` —\
    \ because `clear_content()` keeps\n`w:tcPr`. So the dark shading (`w:shd`), cell width (`w:tcW`),\
    \ borders, `gridSpan`,\n`vMerge`, vertical alignment, and cell margins all remain.\n\nWhat is lost:\n\
    - All run-level formatting: bold, 9 pt size, and the white font color are gone; the new\n  run has\
    \ no `w:rPr`, so it inherits the paragraph/style/default formatting (typically\n  black, default size,\
    \ not bold). On a dark-shaded header this makes the new text\n  unreadable (black on dark).\n- All\
    \ paragraph-level formatting: alignment, spacing, and any `w:pPr` from the old\n  paragraph are lost\
    \ with the paragraph.\n- Any additional paragraphs in the cell (multi-paragraph cells collapse to\
    \ one).\n- Hyperlinks, fields, or revisions inside the cell are destroyed.\n\nCorrect approach: edit\
    \ only the `w:t` text of the existing run(s), preserving `rPr`; or\ncopy an existing run's `rPr` onto\
    \ the new run. Never use `cell.text = ...` when formatting\nmatters.\n"
- id: docx-cell-02
  answer: "Changing Qty from 5 to 12 changes the whole arithmetic chain that depends on it:\n- the line\
    \ amount for that item = qty × unit rate;\n- the subtotal / sum of line amounts;\n- any discount applied\
    \ (percentage or fixed) and the discounted subtotal;\n- tax (VAT/GST/sales tax) computed on the taxable\
    \ base;\n- the grand total / amount due;\n- any other derived cells (balance, amount in words, per-line\
    \ tax, shipping+tax).\n\nMaking the numbers consistent:\n1. Treat the source of truth as qty and unit\
    \ rate for each line.\n2. Recompute each line total = round(qty × rate, currency precision).\n3. Sum\
    \ line totals to get the subtotal.\n4. Apply discount (respecting whether it is before/after tax).\n\
    5. Recompute each tax on the correct base.\n6. Recompute grand total = base − discount + tax (+ shipping).\n\
    7. Write every affected cell so the document's printed numbers agree with each other, and\n   *assert*\
    \ your recomputed grand total equals the printed total before and (new total)\n   after — this catches\
    \ a bad input table instead of silently propagating it.\n8. Mind rounding policy: round per line vs\
    \ only on the total; match the document's existing\n   convention.\n9. If total cells are Word fields,\
    \ update/replace them (see docx-rowdel-03); don't rely on\n   Word to recompute.\n"
- id: docx-cell-03
  answer: "With `w:trackRevisions` on, a programmatic rewrite of `w:t` text does **not** create a\ntracked\
    \ revision. Editing the XML directly is equivalent to accepting a change silently:\nthere is no `<w:ins>`\
    \ / `<w:del>` markup, no author, no date, and no redline — yet the\ndocument is flagged as tracking\
    \ revisions, so the user's expectation (\"show my edit as a\ntracked change\") is violated. The edit\
    \ simply appears as if it had always been there.\nAlso, if the document already contains tracked insertions\
    \ (`w:ins`) and deletions\n(`w:del`/`w:delText`), naive edits can modify text inside those wrappers\
    \ or produce\ninconsistent state that Word flags or reconciles unexpectedly; the cached \"final\"\
    \ text and\nthe redline can diverge.\n\nWhat to consider / do:\n- Clarify intent: does the user want\
    \ the edit as a *tracked change*, or a *clean accepted*\n  edit? These lead to different implementations.\n\
    - For a genuine tracked change you must author the markup yourself: wrap the removed run\n  in `<w:del>`\
    \ and change its `w:t` to `w:delText`, add a sibling `<w:ins>` with the new\n  run, and set `w:author`,\
    \ `w:date`, and ideally matching `w:rsid`. This is non-trivial\n  and easy to get subtly wrong.\n\
    - For a clean edit, either accept all existing revisions first (convert `w:ins` content to\n  ordinary\
    \ runs, drop `w:del`) or preserve them, then edit the text normally — but be\n  explicit that the\
    \ result is an accepted edit, not a tracked one.\n- Note that enabling/disabling `w:trackRevisions`\
    \ in `settings.xml` only affects *Word's*\n  future edits; it does not retroactively track programmatic\
    \ XML changes.\n- Verify by reopening in Word (or inspecting the XML) that the expected revision markup,\n\
    \  author, and date are present if tracking was requested.\n"
- id: docx-relayout-01
  answer: "`table.add_row()` appends a new `w:tr` at the **end** of the table (before any trailing\n`w:tblPr`-adjacent\
    \ elements are irrelevant; it appends after the last row). It creates one\n`w:tc` per `w:gridCol`\
    \ in `w:tblGrid`, each cell containing a single empty `w:p`. It does\n**not** copy row formatting\
    \ (no `w:trPr` height/style), cell shading, borders, fonts, or\nheader semantics, and it does not\
    \ handle `gridSpan`/`vMerge` specially — merged layouts\ncan come out with the wrong number of cells.\n\
    \nTo insert a correctly formatted row after \"Service G\" and before the Total row:\n1. Locate the\
    \ anchor row (the \"Service G\" row) and the Total row by concatenated cell\n   text, not index.\n\
    2. Create the new row by `copy.deepcopy()` of an existing representative body row so it\n   inherits\
    \ `w:trPr`, `w:tcPr` (width, shading, borders), and run formatting, then replace\n   its `w:t` text\
    \ with the new line's values. If `w:shd` alternates per row, set the\n   correct shading (and remember\
    \ `w:shd` must follow `w:tcW` inside `w:tcPr`).\n3. Move it to the right position: `anchor_tr.addnext(new_tr)`\
    \ (lxml) inserts it\n   immediately after the anchor row; equivalently `total_tr.addprevious(new_tr)`.\n\
    4. Fix merges: if the inserted row's cells participate in a vertical merge, set\n   `w:vMerge` consistently;\
    \ ensure `gridSpan` counts match the grid.\n5. Recompute and rewrite every Total that changed. Don't\
    \ change `w:tblGrid` (column count\n   is unchanged).\n6. Verify order and formatting by re-reading\
    \ the table's rows and XML.\n"
- id: docx-relayout-02
  answer: "`table.add_column(width)` appends one `w:gridCol` (with the given width) to `w:tblGrid` and\n\
    adds one `w:tc` of that width to **every** existing row. The new cells contain empty\nparagraphs and\
    \ carry only the width — no shading, borders, bold header text, alignment,\nor other `w:tcPr` styling\
    \ copied from the neighbouring columns. It does not adjust\n`w:tblW`, and it does not rebalance the\
    \ other columns.\n\nTo keep the table inside the page margins and visually consistent:\n1. Compute\
    \ usable width = page width − left margin − right margin (`section.page_width`,\n   `section.left_margin`,\
    \ `section.right_margin`). The sum of all `w:gridCol` widths (and\n   cell `w:tcW` values) must fit\
    \ this.\n2. If adding the column would overflow, shrink the existing columns so the total fits,\n\
    \   and reduce the Item column or others as appropriate.\n3. Update `w:tblW` to the new total and\
    \ set `w:tblLayout` to fixed (`w:type=\"fixed\"`) if\n   you need the explicit widths honored; otherwise\
    \ Word autofits and may override you.\n4. Set each existing row's new cell `w:tcW` and give it the\
    \ same `w:tcPr` styling as its\n   column peers (copy `w:shd`, borders, font, alignment).\n5. Populate\
    \ the header cell with \"SKU\" and copy the header row's run formatting (bold,\n   size, color) and\
    \ paragraph alignment.\n6. For merged rows (gridSpan/vMerge), a new grid column changes the grid those\
    \ spans cover\n   — adjust `w:gridSpan` values and insert cells into each row at the correct position\
    \ so\n   the new \"SKU\" column lands after \"Item\", not appended at the far right.\n7. Verify: sum\
    \ the grid widths against the usable width, and reopen to confirm layout.\n"
- id: docx-relayout-03
  answer: "`w:gridCol` is only the *grid* definition. Word derives actual rendered widths from the\ncell\
    \ widths (`w:tcW`) and the table layout algorithm, so changing only the `w:gridCol`\nvalues leaves\
    \ the old widths in effect.\n\nSpecifically:\n- Each `w:tc` has its own `w:tcW w:w` (dxa). Word honors\
    \ these; if they disagree with the\n  grid, the cell widths win or the table is re-laid out, and the\
    \ visual widths don't match\n  the new grid.\n- If `w:tblLayout` is autofit (`w:type=\"autofit\"`,\
    \ the default when unset), Word\n  recalculates column widths from content and available space and\
    \ can ignore your grid\n  entirely.\n- The overall table width `w:tblW` and the usable page width\
    \ also constrain things.\n- Merged cells complicate it: a cell with `w:gridSpan=\"2\"` must have a\
    \ `w:tcW` equal to\n  the sum of the two grid columns it covers.\n\nTo actually change displayed widths\
    \ you must, together:\n1. Update every `w:gridCol/@w:w` in `w:tblGrid`.\n2. Update every corresponding\
    \ `w:tcW/@w:w` in every row (matching the grid, summing for\n   spans).\n3. Update/reconcile `w:tblW`.\n\
    4. Set `w:tblLayout` to `w:type=\"fixed\"` so Word stops autofitting and uses your widths.\n5. Keep\
    \ the sum within the usable page width (page width minus margins), and verify by\n   reopening.\n"
- id: docx-tblins-01
  answer: '`doc.add_table()` appends the table at the very end of the document body (after the last paragraph,
    before the final `w:sectPr`), not at the current cursor/selection. To place it right after "Notes:",
    add it and then move the XML element with lxml: find the paragraph (`p = <the ''Notes:'' paragraph>`)
    and call `p._p.addnext(tbl._tbl)` (or `paragraph._p.addnext(table._tbl)`). The same `addnext`/`addprevious`
    trick works for paragraphs and drawings too, since python-docx gives access to `_tbl`, `_p`, `_element`.

    '
- id: docx-tblins-02
  answer: 'The style name is not defined in the template''s `styles.xml`. python-docx resolves a style
    by name against the package''s styles part; only styles physically present (plus a few built-ins such
    as "Table Grid") exist. Word''s "Grid Table 4 Accent 1" is usually a latent style that a minimal/older
    template never writes out, so lookup raises KeyError. Options: (a) use a style that exists in the
    template, e.g. `table.style = "Table Grid"` (that gives ruled borders); (b) add the style definition
    to `styles.xml` (apply the style once in Word and save, or inject the `w:style` XML), then assign
    it; or (c) skip styles and set borders directly by inserting `w:tblBorders` into `w:tblPr`.

    '
- id: docx-tblins-03
  answer: 'By default `add_table(rows, cols)` produces a borderless "Table Normal" table that spans the
    block/text width (usable page width between margins) with the columns simply divided/auto-fit — no
    explicit fixed widths and no visible rules. To match the invoice table, copy the existing table''s
    formatting instead of relying on defaults: assign the same style (`table.style = existing.style`),
    copy `w:tblW`/`w:tblBorders`/alignment from the existing `w:tblPr`, and replicate the `w:tblGrid`
    column widths and each cell''s `w:tcW` so geometry matches.

    '
- id: docx-imgrep-01
  answer: 'A picture is stored as a `w:drawing` (`wp:inline` or `wp:anchor`) whose `a:blip` has `r:embed="rIdN"`;
    `rIdN` is a relationship in the rels of the part that owns the drawing (`document.xml`, or a header/footer
    part), resolving to an image part under `word/media/`. To replace exactly one picture while keeping
    position and size: on the owning part call `new_rid, _ = part.get_or_add_image(new_path)`, then set
    that blip''s `r:embed` attribute to `new_rid`, leaving `wp:extent` (and `a:ext`) untouched. Do not
    overwrite the image part''s bytes. Drop the old relationship only if no remaining blip in that part
    still references the old rId (`drop_rel` counts only `r:id`, not `r:embed`, so check blips yourself
    first).

    '
- id: docx-imgrep-02
  answer: 'Because python-docx dedupes image parts: identical image bytes are stored as a single `word/media/*`
    part that can be shared by several relationships/blips (and even referenced from different parts,
    e.g. body and header). Assigning `image_part._blob = new_bytes` mutates that one shared part, so every
    picture resolving to it — including the one on page 3 — renders the new image. The correct approach
    is per-picture: give the target blip a new `r:embed` via `get_or_add_image(path)` rather than editing
    the shared blob.

    '
- id: docx-imgrep-03
  answer: 'A logo on every page lives in a section header part (`section.header`, `section.first_page_header`,
    or `section.even_page_header`), sometimes a footer, and is often an anchored (`wp:anchor`) drawing.
    `doc.inline_shapes` only enumerates body-level `wp:inline` pictures, so it never sees header/footer
    pictures. Walk every section and its header/footer parts, find `a:blip` elements in those parts''
    XML, and replace the blip''s `r:embed` using that part''s own relationships (`part.get_or_add_image(path)`
    on the header/footer part, then repoint the embed; keep the extent).

    '
- id: docx-imgins-01
  answer: '`doc.add_picture()` creates a new paragraph containing the inline image and appends it at the
    end of the document body — it does not insert at the current position. To put it after "Approved by:",
    either add it and then move the new paragraph element (`doc.add_picture(path, width=Inches(1.5))`,
    then `target_p._p.addnext(doc.paragraphs[-1]._p)`), or insert a run in/after the target paragraph
    and add the picture there (`run = p.add_run(); run.add_picture(path, width=Inches(1.5))`). Passing
    only `width` preserves aspect ratio, since python-docx computes the height from the image''s native
    dimensions.

    '
- id: docx-imgins-02
  answer: '`add_picture` inserts at the image''s native pixel/dpi size, ignoring the cell width. A 3-inch
    image in a 1.2-inch cell overflows: Word typically widens/expands the cell (or the image spills past
    the margins), distorting the table layout. Size it explicitly by passing `width=` (and optionally
    `height=`) to `add_picture`/`run.add_picture`, e.g. `width=Inches(1.2)`; with width only the aspect
    ratio is preserved. Always supply a width for images destined for constrained cells.

    '
- id: docx-legacy-01
  answer: 'No. python-docx opens only OOXML `.docx` packages; the binary Word 97–2003 `.doc` format is
    not a zip and raises an error (e.g. `PackageNotFoundError`/"not a zip file"). Workflow: convert `.doc`
    to `.docx` first — LibreOffice (`soffice --headless --convert-to docx`), Microsoft Word, or macOS
    `textutil -convert docx` — verify the converted document, then open the `.docx` with python-docx and
    delete the row as usual, keeping the original `.doc` as a backup.

    '
- id: docx-legacy-02
  answer: 'textutil is not a faithful OOXML converter: it can drop or flatten tables, merged cells, table
    styles/borders, headers/footers, images, and field codes, and may alter fonts and paragraph structure.
    For a table-row edit this risks editing a structurally different table (lost merges, changed row/column
    counts, changed cell partitioning). Detect it by inspecting after conversion, before editing: open
    the `.docx` in python-docx and compare table count, rows/columns, cell texts, and merge spans (`gridSpan`/`vMerge`),
    plus header/footer and media counts, against the original, and/or render both in Word/LibreOffice
    and compare.

    '
- id: docx-legacy-03
  answer: 'LibreOffice is single-instance: if a `soffice`/`libreoffice` process (the open desktop window)
    is already running, a new invocation just hands the request to that existing instance and exits immediately,
    so the headless `--convert-to` never runs and no output appears. Run it with an isolated user profile
    so it starts a separate instance, e.g. `soffice --headless --norestore -env:UserInstallation=file:///tmp/lo_profile
    --convert-to docx --outdir /tmp/out file.doc`; close/kill the GUI instance if needed, and wait for
    the process to exit before checking that the output file exists (use a timeout plus an existence check).

    '
- id: docx-verify-01
  answer: 'Save to a new/temp path first (keep the original as a backup). Then: (1) re-open the saved
    file with python-docx to confirm it parses; (2) validate the package with `zipfile.testzip()`/`unzip
    -t` and, if available, OOXML schema validation, checking `[Content_Types].xml` and the rels parts;
    (3) assert the intended edits — deleted row absent, every recomputed Total correct, replaced blip/rel/media
    updated and old media gone; (4) confirm no unintended parts changed (unzip and diff `document.xml`
    and header/footer parts); (5) open it in Word or LibreOffice to confirm it renders without a repair
    prompt. Only after all checks pass, replace the original.

    '
- id: docx-verify-02
  answer: 'Common causes: wrong child ordering inside `w:tblPr`/`w:trPr`/`w:tcPr` (the schema mandates
    a fixed sequence — e.g. `w:tcW` before `w:shd`, borders before shading/aspect rules); a row whose
    `w:tc` count no longer matches `w:tblGrid`, or a stale/incorrect `w:tblGrid` after edits; removing
    required elements or leaving empty/invalid `w:tr`/`w:tc`; `gridSpan` inconsistent with the grid; unbalanced
    or misplaced XML (e.g. stray elements, relocated `w:sectPr`); dangling or missing relationship ids
    (`r:embed`/`r:id`) or missing image parts; and namespace damage such as pruning prefixes still listed
    in `mc:Ignorable`. Invalid attribute enum values can also trigger "unreadable content".

    '
- id: docx-verify-03
  answer: 'Unzip the saved `.docx` and search every part for the old image: hash all `word/media/*` entries
    and confirm none matches the old image''s bytes, and grep the package for a unique byte signature/marker
    of the old image. Also confirm no `a:blip@r:embed` anywhere (document, headers, footers) still points
    at the old image relationship, and that its relationship was removed from the rels part. Note python-docx
    keeps an image part as long as any rel reaches it, and `drop_rel` counts only `r:id` (never `r:embed`),
    so verify blips yourself before dropping. The acceptable end state: no media entry and no rel referencing
    the confidential image.

    '
