- id: docx-model-01
  answer: 'A .docx is physically a ZIP archive (an OPC package), not a flat XML file. Typical members:
    `[Content_Types].xml`, `_rels/.rels`, `word/document.xml` (the body), `word/styles.xml`, `word/settings.xml`,
    `word/numbering.xml`, `word/_rels/document.xml.rels` (r:id links to media), `word/media/*`, `docProps/*`.
    (Legacy .doc is an OLE compound binary, entirely different.)


    A table''s data lives inline in the document part''s `w:body` (or a header/footer/footnote part) —
    there is no separate table part.


    Element stack, outer to inner:

    - `w:tbl` — must be followed by a `w:p` in the body.

    - `w:tblPr` — `w:tblStyle`, `w:tblW`, `w:tblLayout`, `w:tblBorders`, `w:tblLook`.

    - `w:tblGrid` — one `w:gridCol` per grid column (the layout grid).

    - `w:tr` (row) — with `w:trPr` for `w:trHeight`, `w:tblHeader` (repeating header), `w:cantSplit`.

    - `w:tc` (cell) — `w:tcPr` (`w:tcW` width, `w:gridSpan` horizontal merge, `w:vMerge` vertical merge,
    `w:shd` shading, `w:tcBorders`, `w:vAlign`) then block-level content: normally one or more `w:p`.

    - `w:p` (paragraph) — `w:pPr` then runs.

    - `w:r` (run) — `w:rPr` (b, sz in half-points, color, rFonts, …) then `w:t` (text), plus `w:tab`,
    `w:br`, `w:drawing`, `w:instrText`, `w:delText` inside `w:del`.


    A `w:tc` must contain at least one `w:p` and must end with a paragraph, so a nested table can never
    be a cell''s last block. Merges are encoded as `w:gridSpan` and `w:vMerge` (val="restart" then continuations),
    never as missing cells.

    '
- id: docx-model-02
  answer: 'Word is free to split or merge runs at arbitrary points: proofing/spell-check boundaries (`w:proofErr`),
    rsid revisions, language/style changes, tracked-change boundaries (`w:ins`/`w:del`), bookmarks, fields,
    smart tags, and content controls. "Widget C" is frequently stored as run "Widget" + run " C" (or even
    "W" + "idget C"). The visible paragraph text is the *concatenation* of its runs'' `w:t` values, so
    `run.text == "Widget C"` is the wrong test.


    Reliable approach:

    1. Build the text first. For a cell use `cell.text`; per paragraph use `Paragraph.text` (in python-docx
    1.1+ this includes hyperlink runs) or `"".join(r.text for r in p.runs)`, and prefer `Paragraph.iter_inner_content()`
    which yields runs and hyperlinks so nothing is missed.

    2. Locate the match in that combined string, then map character offsets back to the run(s).

    3. Edit the run(s). If wholly inside one run, set `run.text = "Widget C"` — the `Run.text` setter
    replaces the run''s content with a single `w:t` but *keeps* `w:rPr`, so formatting survives (it does
    lose `w:tab`/`w:br`, so beware). If the match spans runs, put the replacement in the first affected
    run and strip the covered characters from the rest. To split a run: `copy.deepcopy` the run element,
    set the copies'' text to before/after, and `addnext` the copy.


    Text this method will not find: field results (`w:instrText`/`w:fldChar`), content inside `w:sdt`
    (content controls), text boxes/drawings, footnotes, and runs nested inside `w:ins`/`w:del` (`p.runs`
    only returns direct `w:r` children). Walk the full XML subtree for those cases.

    '
- id: docx-model-03
  answer: 'Table/cell widths: twips (twentieths of a point), used by `w:tblW/@w:w`, `w:gridCol/@w:w`,
    `w:tcW/@w:w`, and `w:trHeight`. With `w:type="dxa"` the value is twips: 1 pt = 20 twips, 1 inch =
    1440 twips = 72 pt = 1440 dxa. The other types are `w:type="pct"` = fiftieths of a percent (5000 =
    100%, 2500 = 50% of available width) and `auto`/nil.


    Image sizes: EMU (English Metric Units) in DrawingML — `wp:inline`/`wp:anchor` → `wp:extent@cx`,`@cy`
    (and `a:ext` in the picture part): 1 inch = 914400 EMU, 1 cm = 360000 EMU, 1 mm = 36000 EMU, 1 pt
    = 12700 EMU. The legacy VML fallback stores sizes in points inside a CSS style string (`style="width:...pt;height:...pt"`).
    Native image DPI matters only when a size is expressed in pixels, not for cx/cy.


    python-docx models these as an int subclass `Length` with helpers `Inches()`, `Pt()`, `Cm()`, `Mm()`,
    `Emu()` (`Inches(1) == Pt(72) == 914400`), conversions `.twips/.pt/.emu/.inches/.cm`, and `add_row()`/`add_column()`
    accept a `Length`.

    '
- id: docx-locate-01
  answer: 'Match on content, not position.


    1. Enumerate candidates. `doc.tables` returns only top-level tables (it misses tables nested in cells,
    in headers/footers, or in text boxes). For a full walk use `doc.element.body.iter(qn(''w:tbl''))`
    and record the ancestor chain so nesting is visible.

    2. Score each table against expected header labels. For every row build a normalized text (casefold,
    strip, collapse whitespace, join cell texts with `|`) and pick the table *and header-row index* where
    the required labels ("item"/"description", "qty", "unit price", "amount"/"total") all appear. Headers
    are often not row 0 — a caption/title row may precede — so don''t assume. Also use `table.style.name`,
    nearby caption paragraphs, numeric cells, and a repeating header row (`w:trPr/w:tblHeader`) as tie-breakers.

    3. Map columns from header text to indices instead of hardcoding 0/1/2.

    4. Find the row: for each row compute normalized text and search for the item within the Item column
    with substring/regex against `cell.text` (labels get split across runs, carry trailing spaces, or
    include currency/quantity). Because `row.cells` repeats merged cells, use `row._tr.tc_lst` when the
    column mapping matters.


    Check before editing: correct table and header row identified; column index resolved from headers;
    the row''s real `w:tc` count and any `w:gridSpan`/`w:vMerge` known; the item is not already present
    (no duplicate); the file is a real .docx (not legacy .doc or encrypted); the table is not inside an
    `sdt` content control that would be regenerated; the `w:tblGrid` column count matches; no bookmark/REF
    field elsewhere points into the target row; Track Changes state (`w:trackRevisions`) is decided up
    front; and work on a copy or committed revision, because a mis-identified delete is unrecoverable.

    '
- id: docx-locate-02
  answer: 'Horizontal merge is `w:gridSpan`. python-docx''s `Row.cells` is built from the table''s *grid*,
    so a row whose first cell has `w:gridSpan w:val="2"` yields one `_Cell` per grid column: the list
    is longer than the number of `w:tc` elements and the same underlying `w:tc` is repeated. For a two-column
    grid with the first cell spanning both, `row.cells[0]` and `row.cells[1]` are the same cell (identical
    element).


    What goes wrong:

    - Writing to "the second cell" by index silently edits the merged cell — `cell.text = ...` clears
    or rewrites the one merged cell, not a second cell.

    - Deleting by index (`row.cells[1]._tc.getparent().remove(...)`) removes the single real cell and
    shifts the row left, corrupting the grid alignment.

    - `len(row.cells)` is the grid-column count, not the cell count, so it over-reports columns for that
    row and disagrees with the naive per-row counting you may have built elsewhere. With vertical merges,
    the repeated entry is the merge *origin*, so the same cell appears in consecutive rows.

    - `table.columns[i].cells` is not well-defined for irregular (mixed-span) tables and can misbehave.


    Reliable approach: work with the real elements — `row._tr.tc_lst` (or `tr.findall(qn(''w:tc''))`)
    for the actual cells, then read `w:tcPr/w:gridSpan/@w:val` to know how many grid columns a cell occupies
    and `w:tcPr/w:vMerge` for vertical merge state. Derive per-row cells from `tc_lst` rather than `cells`.

    '
- id: docx-rowdel-01
  answer: "No. python-docx has neither `table.delete_row()` nor `row.delete()`. The library is a thin\
    \ wrapper over lxml, so you remove the `w:tr` element:\n\n```python\ndef delete_row(table, row):\n\
    \    tr = row._tr            # or row._element\n    tr.getparent().remove(tr)\n```\n\nEquivalently\
    \ `table._tbl.remove(row._tr)`. Also `row._tr.getparent()` after removal is `None`; any `Row`/`_Cell`\
    \ objects you still hold for that row become stale, so re-fetch via `table.rows` / `table.cell(r,\
    \ c)`.\n\nCaveats: deleting the first row drops the repeating-header flag (`w:tblHeader`) if that\
    \ was the first row; a row that starts a vertical merge leaves orphaned `w:vMerge` continuations;\
    \ a row whose numbers feed a `{ =SUM(ABOVE) }` field leaves a stale cached result; bookmarks, comments,\
    \ `w:ins`/`w:del` and cross-reference anchors inside the row are destroyed — check for `REF` fields\
    \ pointing at bookmarks in that row first. Verify after saving by re-reading `len(table.rows)` and\
    \ the table's text, and by grepping the saved `word/document.xml` for the removed strings.\n"
- id: docx-rowdel-02
  answer: 'What happens if you just remove the row: the two surviving rows keep first cells containing
    `w:vMerge` with no `w:val` (i.e. "continue") but their merge *origin* is gone. Word then treats the
    first surviving continuation cell as the new start of the merge, so the merged region silently shrinks
    from three rows to two, and the deleted row''s cell text (which lived in the origin cell) is lost.
    If the deleted row was the last of the group, you are left with a dangling "continue" with no restart.
    The table still renders, so the corruption is silent.


    Correct handling — treat the merge group as a unit:

    (a) If the whole group must go: locate the origin (`w:vMerge/@w:val == "restart"`, or a `w:vMerge`
    element with no val meaning restart), then walk forward through `table.rows` consuming every following
    row whose corresponding first cell has a `w:vMerge` without `val` (or `val="continue"`), and remove
    all of those `w:tr` elements.

    (b) If only the origin row should go and the others should become ordinary cells: un-merge first —
    remove the `w:vMerge` element from the origin cell and from the continuation cells, so each stands
    alone, then delete the target row.

    (c) If the remaining rows should still look merged: after deleting the origin row, set `w:vMerge w:val="restart"`
    on the first surviving cell and leave the later ones as continuations.


    Implementation detail: read cells from `row._tr.tc_lst[0]` and inspect `tcPr/w:vMerge` (absent element
    = no merge; element without `w:val` = continue; `w:val="restart"` = origin). After the edit, verify
    the remaining rows'' `w:tc` counts and `w:gridSpan` values still match `w:tblGrid`, and render to
    PDF to confirm the vertical merge is correct.

    '
- id: docx-rowdel-03
  answer: 'What shows: the stale, pre-edit total. The formula is a field — either `w:fldSimple` with the
    instruction in `@w:instr` (` =SUM(ABOVE) `), or a complex field of `w:fldChar` begin/separate/end
    runs with the instruction in `w:instrText`. The number Word displays is the *cached field result*
    stored in the `w:t` between `separate` and `end`. python-docx only writes XML; it never evaluates
    or refreshes fields, so the cached result is untouched and Word shows the old total on open. Word
    does not recalculate body fields merely because the file changed — it updates them on Ctrl+A then
    F9, when a dependency is edited interactively, when printing with "update fields before printing",
    or on open if the document carries `<w:updateFields w:val="true"/>` in `word/settings.xml` or the
    field is marked dirty (`w:fldCharType="begin"` with `w:dirty="true"`, or `w:dirty` on `w:fldSimple`).
    So a user who simply opens and reads (or prints without that option) can be billed the wrong amount.


    How to handle it:

    1. Mark the field dirty: set `w:dirty="true"` on the begin `w:fldChar` (and on any other field you
    touched) so Word/LibreOffice refresh on open.

    2. Or set `<w:updateFields w:val="true"/>` in `word/settings.xml` for the whole document.

    3. Or compute the value yourself from the remaining line items in Python and overwrite the cached
    result: find the `w:instrText` containing `SUM(`, compute, then set the result run''s `w:t` to the
    formatted number (or delete the result runs so only the field code remains).

    4. If the total must be right regardless of reader, replace the field with static text.


    Also note `SUM(ABOVE)` semantics: it sums the contiguous numeric cells above in the same column and
    stops at the first blank/non-numeric cell, so deleting a row can change the summed range (a blank
    left in that column truncates the sum). Recompute from the actual line items rather than trusting
    the field. After saving, re-read the cached value and ideally render to confirm.

    '
- id: docx-rowdel-04
  answer: 'Why it is not a deletion: the `w:tr`, its `w:tc`, `w:p`, `w:r` and `w:t` all remain in `word/document.xml`.
    Consequences:

    - Any XML-level consumer — python-docx `cell.text`/`row.cells`, docx2txt, converters, search/indexing,
    downstream data extraction — still sees the row and its text.

    - Accessibility tools and screen readers read it; `w:vanish` hidden text is revealed by "show hidden
    text" and can be printed; white-on-white text is still selectable, copyable, and present in the file.

    - `w:trHeight` with `w:hRule="exact"` and `w:val="0"` is not honored: Word clamps to a minimum row
    height and still draws the cell borders/grid line, so you often get a visible sliver or stray rule;
    with `atLeast`/`auto` the height is recomputed from the still-present content.

    - The table''s grid, border drawing, row count, repeating-header logic, and any index-based row addressing
    still count the phantom row, so totals that index rows are wrong.

    - Stale bookmarks, comments and revisions stay attached to it.


    Proving the row is really gone:

    1. Structural: reopen with python-docx; assert `len(table.rows) == expected`, that the list of row
    texts no longer contains the removed values, that the expected following row (e.g. Total) is at the
    expected index, and that the number of `w:tr` children of that `w:tbl` equals the expected count.

    2. Byte level: unzip the .docx and grep every XML part (`word/document.xml`, headers, footers, footnotes)
    for the removed item name/amount — they must be absent — and confirm no `trHeight w:val="0"` or `w:vanish`
    remains in that table.

    3. Whole-document text diff: extract text before and after; the only differences should be the intended
    ones.

    4. Render: convert to PDF (LibreOffice) and confirm there is no extra row, gap, or orphaned line.

    5. Confirm dependent totals were recomputed (the SUM field''s cached value).

    The correct fix is removing the `w:tr` element, not styling it away.

    '
- id: docx-cell-01
  answer: 'What survives: everything on the *cell* — `w:tcPr` including `w:tcW` (width), `w:gridSpan`,
    `w:vMerge`, `w:shd` (the dark fill), `w:tcBorders`, `w:tcMar`, `w:vAlign`, `w:noWrap` — plus the table''s
    `w:tblPr`/`w:tblStyle` and the cell''s position in the grid.


    What is lost: all paragraph- and run-level formatting. python-docx''s text setter clears the cell''s
    content (removing every child of `w:tc` except `w:tcPr`) and adds one new `w:p` containing one new
    `w:r`. The old `w:p` and its `w:pPr` (alignment, `w:pStyle`, indentation, spacing, numbering) disappear,
    and the new run has no `w:rPr` — so `w:b` (bold), `w:sz` (9 pt = 18 half-points), `w:color` (white),
    `w:rFonts`, `w:highlight`, `w:i`, `w:u`, `w:vanish`, `w:lang`, `w:noProof` are all gone. Net result:
    dark shading with default black ~11 pt text, i.e. effectively unreadable. Also lost: any field, bookmark,
    comment anchor, hyperlink, image, list numbering and tracked-change markup inside the cell.


    Two safe approaches:

    (a) Edit in place — set the first existing run''s `run.text = "SKU"` (the `Run.text` setter keeps
    that run''s `w:rPr`, so bold/9 pt/white survive), then remove the cell''s remaining runs and extra
    paragraphs.

    (b) If you must rebuild, `copy.deepcopy` the original run''s `w:rPr` (and the paragraph''s `w:pPr`)
    *before* the clear, then insert the copies into the new run/paragraph, and preserve `w:tcPr`.


    Afterwards, assert the surviving run''s `rPr` contains `w:b`, `w:sz w:val="18"` and `w:color w:val="FFFFFF"`,
    and that `w:tcPr/w:shd` is still present.

    '
- id: docx-cell-02
  answer: 'Everything derived from that number must be recomputed and rewritten:

    - The line''s own extended amount: 12 × unit price instead of 5 × unit price.

    - The invoice subtotal (sum of line amounts).

    - Any discount, shipping, and tax/VAT line computed on the new base, and the tax amount itself if
    tax is per unit or the rate is flat.

    - The grand total.

    - Any duplicates of these: a summary block in the body, a header/footer "Invoice total: …" line, a
    per-customer summary table if the document holds many invoices, the amount in words, and any stated
    item/quantity count.

    - Cross-references: bookmarks with `REF` fields pointing at the old amount, or a total mentioned in
    prose.


    If the grand total is a Word field (`{ =SUM(ABOVE) }`) its cached result is stale and must be refreshed
    (see rowdel-03) — otherwise the document contradicts itself on open.


    How to keep it consistent: parse the line items into a structure, compute subtotal/tax/total once
    in Python (use integer cents or `Decimal` with the correct rounding rule for the tax regime), and
    write every affected cell from that single source of truth. Match the surrounding number formatting
    exactly (2 dp, thousands separators, currency symbol, alignment) so the edit doesn''t look hand-made.
    Then assert the invariant `sum(line amounts) == subtotal` and that the written numbers appear in the
    saved document text. Choose one path for the total — either mark the field dirty/`w:updateFields`
    so Word recomputes, or overwrite the cached result with your computed value — and don''t rely on a
    field whose value must be correct the instant the file is opened.

    '
- id: docx-cell-03
  answer: 'What happens: nothing is tracked, and the edit is silently clean. `w:trackRevisions` in `word/settings.xml`
    instructs Word''s *editing engine* to record revisions; python-docx bypasses that engine and mutates
    the XML directly. Rewriting `w:t` therefore produces an untracked change — no `w:ins`/`w:del`, no
    author, no date, no revision id, nothing in the Review pane, and the previous text is unrecoverable
    from the file. Worse, if the content you rewrite sits inside someone else''s `w:ins`/`w:del` (or has
    `w:rPrChange`/`w:pPrChange`/`w:tblPrChange` formatting revisions), clearing it silently accepts or
    destroys that reviewer''s change.


    A subtler trap: run lookup. `Paragraph.runs` and `Row.cells` only return direct `w:r` children of
    `w:p`, so runs nested inside `w:ins` — the normal shape after a tracked insertion — are invisible;
    you can "fail to find" text that visibly appears in Word, and writing to the cell can corrupt the
    revision markup. Also note `w:documentProtection` with enforced track changes blocks humans but not
    your XML edits.


    What to consider:

    - Decide explicitly whether the deliverable is a clean file or a reviewable change; don''t let it
    happen by accident.

    - If reviewable, build the markup yourself: wrap new content in `w:ins` (`w:id`, `w:author`, `w:date`)
    and mark removed content with `w:del` containing `w:delText` instead of `w:t`, at run granularity,
    walking the full subtree (including `w:ins`/`w:hyperlink` children) to find text.

    - If clean, first accept or deliberately strip existing revisions (promote `w:ins` content, drop `w:del`
    content) so you are not silently accepting others'' work, and decide whether to keep or remove `w:trackRevisions`.

    - For legal/contract/audit workflows, remember that an untracked script edit is indistinguishable
    from a human edit in the review pane and destroys the audit trail.

    '
- id: docx-relayout-01
  answer: 'What `add_row()` does: it **appends** a brand-new `w:tr` at the end of the `w:tbl` — after
    the last row, i.e. *after* the Total row — with one `w:tc` per grid column, each containing a single
    empty `w:p`, and with no `w:trPr`, no `w:tcPr`, no shading, no widths and no run formatting. It cannot
    insert in the middle, and it inherits nothing beyond the table style. It returns the new `Row`.


    Insert a correctly formatted row in the middle:

    1. Pick a template row and `new_tr = copy.deepcopy(template_row._tr)` — best is a structurally identical
    data row (a blank/spacer row is second best). The copy carries `w:trPr` (`w:trHeight`, `w:tblHeader`,
    `w:cantSplit`) and per cell `w:tcPr` (`w:tcW`, `w:gridSpan`, `w:shd`, borders, `w:vAlign`) plus paragraph
    and run formatting.

    2. Sanitize the copy per `w:tc`: keep only the first `w:p`, drop extra paragraphs, runs, hyperlinks,
    `w:bookmarkStart`/`w:bookmarkEnd`, `w:commentRangeStart`/`w:commentRangeEnd`, `w:proofErr`, `w:ins`/`w:del`,
    `w:drawing`, `w:fldChar`/`w:instrText`; leave a single empty `w:r` that retains the original `w:rPr`
    so you can write text with the right formatting.

    3. Generate fresh unique IDs — `w:bookmarkStart/@w:id` and `w14:paraId`/`w14:textId` must not be duplicated,
    so strip or renumber them.

    4. Insert in position with `service_g_row._tr.addnext(new_tr)` (lxml `addnext` makes it the immediately
    following sibling inside the same `w:tbl`, ahead of the Total row). Verify with `table.rows` that
    the order is Service G → new → Total and that `len(table.rows)` grew by one.

    5. Write the new values into the preserved runs (not `cell.text = ...`, which drops formatting), then
    recompute the totals — including the `SUM(ABOVE)` cached result — and re-check any numbering, bookmarks
    or cross-references that referenced the old row count.

    '
- id: docx-relayout-02
  answer: 'What `add_column(width)` does: it appends a new `w:gridCol` to `w:tblGrid` and a new (empty)
    `w:tc` to the **end of every** `w:tr`, setting each new cell''s `w:tcW` to the width you pass; it
    returns `None`. So the column always lands at the far right — never "after the Item column" — and
    it knows nothing about the other columns'' widths, the page''s text width, or the header/border styling.


    To keep the table inside the margins and consistent:

    1. Position it yourself: insert the `w:gridCol` at the desired index (`tblGrid`''s `gridCol_lst[i].addprevious(new_col)`
    or an lxml `insert`), and insert a cloned `w:tc` at the same index in **every** `w:tr` (`tr.tc_lst[i].addprevious(new_tc)`
    / `tr.insert(i, new_tc)`). Clone a sibling cell from the same row so `w:tcPr` (width, borders, shading,
    `w:vAlign`) matches.

    2. Budget the widths: available = `section.page_width - section.left_margin - section.right_margin`.
    Rescale the existing `w:gridCol` widths so that old total + new width ≤ available, and write the numbers
    into every `w:gridCol/@w:w` and the matching `w:tcW` of every cell (for `w:gridSpan` cells, `tcW`
    must equal the sum of the grid columns they span).

    3. Make the widths stick: set `<w:tblLayout w:type="fixed"/>` in `w:tblPr` — otherwise autofit recomputes
    widths from content and your grid is ignored/overwritten — and set `w:tblW` to the new total in `dxa`
    (or `pct` 5000 for 100%).

    4. Style it: copy the header cell''s `w:tcPr` (including the dark `w:shd`) and the header run''s `w:rPr`
    into the new column''s header cell, copy the body cells'' `tcPr`/`rPr` from the neighbouring Item
    column, write the header text, and check `w:tblLook` (firstRow/lastRow/firstColumn/lastColumn/noVBand)
    so banding and edge-column formatting still apply to the new column. Keep `w:tblHeader` on the header
    row so it still repeats across pages.

    5. Verify: assert `sum(gridCol widths) <= available`, every row''s `w:tc` count equals the grid count,
    `w:gridCol` count equals header cell count, and render to PDF to confirm nothing spills past the margin.

    '
- id: docx-relayout-03
  answer: 'Because `w:tblGrid` is only a hint. Word uses the `w:gridCol` widths when the table layout
    is **fixed**; if the table is in autofit mode — no `w:tblLayout` element, or `<w:tblLayout w:type="autofit"/>`
    — Word recomputes column widths from the cell contents and the per-cell `w:tcW` values, and when it
    next saves the file it writes its own computed `w:gridCol` values back. So your edit is ignored, and
    then clobbered by the next save from Word.


    Beyond that, widths live in several places that must agree:

    1. Each cell''s `w:tcPr/w:tcW` (preferred width, `w:type="dxa"`); for cells with `w:gridSpan` the
    `tcW` must equal the sum of the grid columns the cell spans, or the cell-to-grid mapping is inconsistent.

    2. The table''s total `w:tblPr/w:tblW` (dxa, or `pct` where 5000 = 100%). If it disagrees with the
    sum of the grid, Word normalizes.

    3. `w:tblPr/w:tblLayout` must be `fixed`.

    4. `w:tblPr/w:tblInd` (indent) and `w:tblCellMar` reduce the usable width.

    5. The total must fit the section text width (`page_width - left_margin - right_margin`) or the table
    overflows/is rescaled.

    6. The table style''s band / first-column definitions affect appearance, not the stored widths.


    So to make new widths stick you must update, in order: every `w:gridCol/@w:w`, then every `w:tcW`,
    then `w:tblW`, and ensure `w:tblLayout` is `fixed` — changing `w:gridCol` alone on an autofit table
    is a no-op. Afterwards re-read the widths from the saved file and render (Word/LibreOffice → PDF)
    to confirm, because autofit tables get re-laid-out on open.

    '
- id: docx-tblins-01
  answer: "`doc.add_table(rows, cols)` appends the table as the LAST child of the document body —\ni.e.\
    \ at the very end of the document, after everything already there (headers/footers\nare separate parts\
    \ and are not touched). It is the same for `doc.add_paragraph()` and\n`doc.add_picture()`: every `add_*`\
    \ helper is an *append*, never an insert-before.\n\nTo place the table at an arbitrary point you have\
    \ to move the `<w:tbl>` element\nyourself, because python-docx has no public \"insert at\" API for\
    \ block items. The\nusual recipe:\n\n```python\nfrom docx.text.paragraph import Paragraph\nfrom docx.table\
    \ import Table\n\n# 1. find the anchor paragraph\ntarget = None\nfor p in doc.paragraphs:\n    if\
    \ p.text.strip() == \"Notes:\":\n        target = p\n        break\n\n# 2. add the table at the end\
    \ (so styles/numbering part relationships resolve)\ntable = doc.add_table(rows=2, cols=3)\ntable.style\
    \ = 'Table Grid'\n\n# 3. relocate the XML element to immediately after the anchor paragraph\nanchor\
    \ = target._p\nanchor.addnext(table._tbl)\n\n# 4. re-create a Paragraph proxy if you want to insert\
    \ more content after it\n#    (paragraphs are flattened again on the next save/reload)\n```\n\nNotes\
    \ / gotchas:\n- `addnext()` inserts as the immediately following sibling. If the anchor is the last\n\
    \  paragraph of a table cell, the table would be nested inside that cell — check the\n  parent's tag\
    \ if that matters.\n- Elements after the anchor that you wanted to keep *after* the table stay after\
    \ it\n  automatically, because you only changed the `tbl`'s position.\n- If you need to insert several\
    \ blocks in a row, keep an anchor cursor and chain\n  `anchor = new_element` after each `addnext()`.\n\
    - Alternative for \"immediately after paragraph P\" with no helper: create the element\n  via `parse_xml`\
    \ / `OxmlElement('w:tbl')` and insert it, rather than add-then-move.\n- Verify by re-opening the saved\
    \ file and re-walking `doc.paragraphs` /\n  `doc.element.body` order; do not trust in-memory order\
    \ alone.\n- `doc.add_table()` also does not set a style. A brand-new table in a default template\n\
    \  has no borders until you assign a table style (see docx-tblins-02).\n"
- id: docx-tblins-02
  answer: "`table.style = \"Grid Table 4 Accent 1\"` is a *style name lookup by string*, and\npython-docx\
    \ resolves it against the set of styles actually defined in the document\npart (`styles.xml`) of the\
    \ file you opened. python-docx ships a default template\n(default.docx) that contains a specific,\
    \ limited list of built-in table styles. Any\nname it does not contain — and, critically, ANY name\
    \ absent from your *company\ntemplate's* `styles.xml` — raises `KeyError: 'no <w:style> with name\
    \ \"...\"'`.\n\n\"Grid Table 4 – Accent 1\" is a real Word built-in style, but built-in styles are\
    \ not\nmagically materialised by python-docx; it only exposes names that are declared in the\ndocument's\
    \ style part. A company template may instead use the older naming\n(\"Grid Table 4 Accent 1\" without\
    \ the en-dash, vs \"Grid Table 4 - Accent 1\"), or may\nhave renamed/deleted it. Note also that a\
    \ *local* style you create yourself\n(`doc.styles.add_style('Grid Table 4 Accent 1', WD_STYLE_TYPE.TABLE)`)\
    \ will be\nlooked up by that exact name, and duplicates/case differences will still miss.\n\nRobust\
    \ pattern — try the name, fall back to what the template actually has:\n\n```python\nfrom docx.enum.style\
    \ import WD_STYLE_TYPE\n\nnames = {s.name for s in doc.styles}\nfor candidate in ('Grid Table 4 -\
    \ Accent 1', 'Grid Table 4 Accent 1',\n                   'Table Grid', 'Grid Table 4'):\n    if candidate\
    \ in names:\n        table.style = candidate\n        break\nelse:\n    table.style = doc.styles['Table\
    \ Grid']   # last-resort guaranteed-in-Word\n```\n\n(A defensive helper that tries a list and finally\
    \ falls back to `Table Grid` is the\nusual answer, plus logging which name actually resolved.)\n\n\
    Other ways to get a ruled/bordered table without depending on a named style:\n- Apply `Table Grid`\
    \ (widely present, defined in most templates).\n- Set borders directly on the table properties — fully\
    \ self-contained and immune to\n  template differences:\n\n  ```python\n  from docx.oxml.ns import\
    \ qn\n  tblPr = table._tbl.tblPr\n  borders = tblPr.makeelement(qn('w:tblBorders'), {})\n  for edge\
    \ in ('top', 'left', 'bottom', 'right', 'insideH', 'insideV'):\n      el = borders.makeelement(qn('w:'\
    \ + edge), {})\n      el.set(qn('w:val'), 'single')\n      el.set(qn('w:sz'), '4')      # eighths\
    \ of a point -> 0.5pt\n      el.set(qn('w:space'), '0')\n      el.set(qn('w:color'), 'auto')\n   \
    \   borders.append(el)\n  tblPr.append(borders)\n  ```\n  Ordering inside `tblPr` is schema-significant\
    \ (`tblBorders` must appear in the\n  right sequence position, roughly after `tblLayout`/`jc` and\
    \ before `shd`); see\n  docx-verify-02.\n- If the table must inherit from the invoice template, copy\
    \ the reference table's\n  `tblPr` element wholesale instead of looking up a name.\n\nDiagnostic when\
    \ a style lookup fails:\n```python\nprint([s.name for s in doc.styles if s.type == WD_STYLE_TYPE.TABLE])\n\
    ```\nThat tells you exactly which border/table styles the template really has.\n"
- id: docx-tblins-03
  answer: "A table created with `doc.add_table(rows, cols)` gets **no explicit width at all**.\nIts `tblPr`\
    \ contains only a `tblW` (and often a `tblLayout`) left at the template's\ndefaults: `w:tblW w:w=\"\
    0\" w:type=\"auto\"` with autofit on. So it does NOT inherit a\nfixed measurement — it is auto-layout,\
    \ and the final rendered width is decided by\nWord from the available text-column width, the table's\
    \ `tblInd` (indent, usually\n`w:tblInd w:w=\"0\" w:type=\"dxa\"`), cell margins (`w:tblCellMar`, default\
    \ 0.08\" left +\n0.08\" right), and the content. Concretely: total page text width for Letter with\
    \ 1\"\nmargins is 6.5\", minus 0.16\" of default cell padding, and each column gets an equal\nshare.\
    \ On A4 with 2.54 cm margins it is ~16 cm instead. The result \"looks right\" in\none template and\
    \ wrong in another — which is exactly why you should set it\nexplicitly rather than rely on the default.\n\
    \nWhat to set so it matches the existing invoice table — in descending order of\nfidelity:\n\n1. **Copy\
    \ the reference table's properties** (best; inherits theme colours, cell\n   margins, indent, banding,\
    \ and column widths in one shot):\n\n   ```python\n   from copy import deepcopy\n   ref = invoice_table._tbl\n\
    \   new  = doc.add_table(rows=r, cols=c)\n   new._tbl.tblPr = deepcopy(ref.tblPr)        # replace,\
    \ don't append\n   new._tbl.insert(1, deepcopy(ref.find(\n       '{http://schemas.openxmlformats.org/wordprocessingml/2006/main}tblGrid')))\n\
    \   ```\n   Remember `tblPr` must be the *first* child of `w:tbl`, and `tblGrid` immediately\n   after\
    \ it — in that order. If `tblPr` is already present, replace its children\n   rather than appending\
    \ a second one (two `tblPr` = corrupt file).\n\n2. **Set an explicit total width and per-column widths**\
    \ in twips (dxa; 1440 twips\n   per inch):\n\n   ```python\n   from docx.shared import Inches, Twips\n\
    \   from docx.oxml.ns import qn\n\n   table.autofit = False                       # emits w:tblLayout\
    \ type=\"fixed\"\n   table.allow_autofit = False\n   tblPr = table._tbl.tblPr\n   for el in tblPr.findall(qn('w:tblW')):\n\
    \       tblPr.remove(el)\n   tblW = tblPr.makeelement(qn('w:tblW'), {})\n   tblW.set(qn('w:w'), str(Twips(Inches(6.5)).twips))\
    \   # total\n   tblW.set(qn('w:type'), 'dxa')\n   tblPr.append(tblW)                        # keep\
    \ schema order (see verify-02)\n\n   for col, w_in in zip(table.columns, (1.0, 4.0, 1.5)):\n     \
    \  col.width = Inches(w_in)              # writes w:gridCol + each w:tcW\n   ```\n   `Inches(x).twips`\
    \ is the reliable integer; `Emu`/`Length` arithmetic is not.\n   You must set BOTH the `tblGrid/w:gridCol`\
    \ widths (via `column.width`) and each\n   cell's `w:tcW` — and in fixed layout the `tblGrid` is what\
    \ actually governs, so\n   a mismatch is the usual cause of \"the columns drift after I set the widths\"\
    .\n\n3. **Read the reference table's numbers** and reuse them verbatim:\n\n   ```python\n   grid =\
    \ ref._tbl.find(qn('w:tblGrid'))\n   widths = [int(gc.get(qn('w:w'))) for gc in grid]\n   tblW   =\
    \ ref._tbl.tblPr.find(qn('w:tblW'))\n   print(widths, tblW.get(qn('w:w')), tblW.get(qn('w:type')),\n\
    \         ref._tbl.tblPr.find(qn('w:tblInd')).get(qn('w:w')))\n   ```\n   A `tblW` of `0/auto` on\
    \ the reference means the real widths live only in\n   `tblGrid` — copy `tblGrid` and set `tblLayout`\
    \ to `fixed`.\n\nAlso copy/align if the invoice table has them, or your table will be visibly off:\n\
    - `w:tblCellMar` (left/right default 108 twips = 0.075\"; heavy templates use 0\")\n- `w:tblInd` (indent\
    \ from the text margin)\n- `w:jc` (alignment)\n- header-row repeat: `trPr/tblHeader` on row 0\n- `w:tblLook`\
    \ (which conditional-format bands apply)\n\nFinally, verify in the *output*, not in memory: reopen\
    \ the saved file and re-read\n`tblW`, `tblGrid` and per-cell `tcW`; the numbers must match the invoice\
    \ table's.\nLibreOffice/Word also applies its own autofit-to-contents unless `tblLayout` is\n`fixed`,\
    \ which is the single most common reason an explicitly-sized table still\nrenders at the wrong width.\n"
- id: docx-imgrep-01
  answer: "A picture in `word/document.xml` is a `<w:drawing>` containing an\n**`<a:blip r:embed=\"rIdN\"\
    >`**. The `r:embed` value is a *relationship ID*, not a\nfilename and not a path. That relationship\
    \ lives in\n`word/_rels/document.xml.rels` and points at the image part:\n\n```xml\n<Relationship\
    \ Id=\"rId5\"\n  Type=\"http://schemas.openxmlformats.org/officeDocument/2006/relationships/image\"\
    \n  Target=\"media/image3.png\"/>\n```\n\nThe image bytes live in that part, e.g. `word/media/image3.png`,\
    \ inside the zip.\nThe drawing also carries the *cached* `<pic:blipFill><a:blip><a:extLst>`/`<a14:imgProps>`\n\
    and, crucially, the display extent `<wp:extent cx=\"...\" cy=\"...\"/>` plus the\ntransform in `<a:xfrm><a:ext\
    \ cx=\"...\" cy=\"...\"/>` — these are **EMU** (914,400 EMU\nper inch). Those two, not the file's\
    \ own DPI, are what determine the rendered size,\nwhich is why a replacement with a different aspect\
    \ ratio will be distorted unless you\nfix them.\n\nHow to replace ONE picture, keeping its position\
    \ and size — swap the *bytes* of the\nexisting part, not the element. Reusing the part is the cleanest\
    \ way to guarantee the\nposition, size, wrap mode, z-order, alt text and hyperlink stay identical:\n\
    \n```python\nfrom docx.shared import Emu\n\npart = doc.part  # or header_part / footer_part for images\
    \ in those\n# 1. find the specific image part used by the logo drawing\nimage_part = part.related_parts['rId5']\
    \      # rId from the blip\nnew_bytes  = open('new_logo.png', 'rb').read()\n\n# 2. verify the extension,\
    \ because the part name/extension is part of [Content_Types]\nif not image_part.partname.ext.lower()\
    \ in ('.png', '.jpg', '.jpeg', '.gif', '.bmp', '.tiff'):\n    raise ValueError(f'content-type mismatch:\
    \ {image_part.partname}')\n\n# 3. overwrite the blob\nimage_part._blob = new_bytes\n# (equivalently:\
    \ image_part.blob = new_bytes on a fresh python-docx,\n#  or image_part._blob = new_bytes + the package\
    \ writes it on save)\n```\n\nNotes:\n- `image_part._blob = ...` is the sanctioned trick: the `Part.blob`\
    \ property is\n  defined as `return self._blob`, and `Package.save` serialises the part from\n  `blob`,\
    \ so replacing `_blob` is sufficient. Do NOT try to set `part.blob`.\n- If the file size changes a\
    \ lot, that is fine — zip entry sizes are recomputed.\n- If the new image is a different format than\
    \ the old part's extension, you must also\n  change the partname and `[Content_Types].xml`; simplest\
    \ fix is to always supply a\n  file with the same format (PNG for PNG) or use a scratch document to\
    \ mint a\n  correctly-typed part.\n- To keep the *aspect ratio* as well, update the extents to the\
    \ new file's real\n  dimensions while preserving one axis (usually width):\n\n  ```python\n  from\
    \ PIL import Image\n  w, h = Image.open('new_logo.png').size\n  for run in doc.paragraphs:  # narrow\
    \ to the logo's run\n      pass\n  # inline shape API:\n  shape = doc.inline_shapes[i]\n  cx, cy =\
    \ shape.width, shape.height\n  shape.height = Emu(int(cy * cx / w))   # height follows new aspect\n\
    \  ```\n  If you truly want the on-page size frozen, just leave `wp:extent` untouched and\n  accept\
    \ the aspect change.\n- The alternative — delete the `w:drawing` and call `run.add_picture()` — works\
    \ but\n  re-lays out the paragraph and can change anchoring, wrapping, alt text, and the\n  relationship\
    \ graph; see docx-imgrep-03 for why that risks orphans.\n- Never hand-edit the `.rels` file. Let python-docx\
    \ own the relationship graph.\n"
- id: docx-imgrep-02
  answer: "Almost certainly **image-part de-duplication / interning**. python-docx (via\n`docx.image.image.Image`\
    \ and `PartFactory`) hashes the image bytes and reuses an\nexisting image part when the same content\
    \ has already been stored in the package.\nThe reverse case is the one that bites: when *loading*\
    \ a document, if two `<a:blip>`\nelements in different parts (or the same part) reference the **same**\
    \ relationship /\nthe same `word/media/imageN.*` part — which is exactly what Word does for a logo\n\
    repeated in a header, or for a logo pasted repeatedly — then overwriting that one\npart's `_blob`\
    \ changes **every** picture that shares the part. You changed \"one\"\npicture in the document *tree*,\
    \ but the bytes live in one shared part, and you\noverwrote the shared part.\n\nIt is not that python-docx\
    \ \"found a similar image\" on the save path; it is that the\nrelationship graph has one node feeding\
    \ several `r:embed` references. Diagnose it:\n\n```python\nfrom docx.oxml.ns import qn\nfrom collections\
    \ import defaultdict\nusers = defaultdict(list)\nfor part in [doc.part] + [s._sectPr.find(qn('w:headerReference'))\
    \ for s in []]:  # see below\n    pass\n\n# every blip in the body\nfor i, blip in enumerate(doc.element.body.iter(qn('a:blip'))):\n\
    \    users[blip.get(qn('r:embed'))].append(('body', i))\n\n# and in every header/footer part, which\
    \ the body-only loop above misses\nfor sec in doc.sections:\n    for hf in list(sec.header.part.package.parts):\
    \  # or iterate sec.header/footer\n        pass\n\nfor rid, where in users.items():\n    if len(where)\
    \ > 1:\n        print('SHARED PART', rid, '->', doc.part.related_parts[rid].partname, where)\n```\n\
    \nA more reliable check is on the filesystem, which sidesteps python-docx entirely:\n\n```bash\nmkdir\
    \ -p x && cd x && unzip -o ../invoice.docx >/dev/null\n# 1. every reference to each media file\nfor\
    \ f in word/media/*; do\n  echo \"== $f  $(shasum -a1 \"$f\" | cut -c1-12) $(stat -f%z \"$f\") bytes\"\
    \n  grep -l \"$(basename \"$f\")\" word/_rels/*.rels word/_rels/*/_rels/*.rels 2>/dev/null \\\n  \
    \  | while read r; do echo \"   rels: $r  x$(grep -c \"$(basename \"$f\")\" \"$r\")\"; done\ndone\n\
    ```\n\nA file whose basename appears in more than one `Relationship`, or one `rId` that\nmore than\
    \ one `<a:blip>` uses, is shared.\n\nFixes, in order of preference:\n\n1. **Give the replacement its\
    \ own new part** and repoint only the logo's\n   relationship. python-docx does not expose a public\
    \ \"clone part\" API, so do it by\n   constructing the part through the package's own machinery and\
    \ dropping it into\n   `related_parts`:\n\n   ```python\n   from docx.opc.part import Part\n   from\
    \ docx.opc.packuri import PackURI\n   from docx.image.image import Image\n\n   def replace_picture_bytes_one_only(part,\
    \ old_rid, path):\n       old = part.related_parts[old_rid]\n       img = Image.from_file(path)\n\
    \       new_rid, new_part = part.get_or_add_image(path)   # hashes, may reuse!\n       if new_part\
    \ is old:                                # still interned\n           new_part = Part(\n         \
    \      PackURI('/word/media/logo_replacement%s' % img.ext),\n               old.content_type, img._blob,\
    \ part.package)\n           new_rid = part.relate_to(\n               new_part,\n               'http://schemas.openxmlformats.org/officeDocument/2006/relationships/image')\n\
    \       # repoint only the blip(s) you want\n       for blip in part.element.iter(qn('a:blip')):\n\
    \           if blip.get(qn('r:embed')) == old_rid and blip in YOUR_TARGETS:\n               blip.set(qn('r:embed'),\
    \ new_rid)\n       return new_rid\n   ```\n   Then the old part is still referenced by nothing — see\
    \ docx-verify-03 for\n   removing it.\n\n2. **Repoint, don't overwrite**: set the logo `blip`'s `r:embed`\
    \ to a *new* `rId`\n   that targets a fresh media part containing the new bytes. Same effect, no\n\
    \   collision.\n\n3. **If the two pictures were genuinely meant to be the same logo** — then this\n\
    \   \"bug\" is correct behaviour, and the right question is why you expected them to\n   differ.\n\
    \nSecondary things worth ruling out while you are in there, because they produce the\n   same symptom:\n\
    - Both pictures live in a **header/footer** and you edited `document.xml`'s part\n   instead (headers\
    \ have their own `.rels` and their own `image_part`). `doc.part`\n   is not the only part with `related_parts`.\n\
    - Your script iterated all blips and overwrote every hit instead of a single\n   element, so a `for`\
    \ loop updated more than you expected.\n- The template itself embeds the logo twice under the same\
    \ media file, so the\n   *template* is the shared-source problem, not your edit.\n\nPrevention: assert\
    \ single-use before overwriting —\n`assert len(list_of_blips_using_rid) == 1` — and if it is not,\
    \ clone the part.\n"
- id: docx-imgrep-03
  answer: "`doc.inline_shapes` is a narrow, misleading API. Its docstring is literally\n\"Inline shapes\
    \ in this document\", and its implementation is\n`InlineShapes(self._body._body, self)` — it only\
    \ walks the **body** part\n(`word/document.xml`) and only `<wp:inline>` drawings. A logo repeated\
    \ on every page\nlives in a **header** (and often a footer, and often all of them), which is a\ndifferent\
    \ part with its own `_element` and its own `.rels`. So the header logo is\nreal, renders on every\
    \ page, and is invisible to that collection. This is the most\ncommon python-docx \"phantom image\"\
    \ report.\n\nFind it by walking the sections and their header/footer parts:\n\n```python\nfrom docx.oxml.ns\
    \ import qn\nfrom docx.enum.section import WD_HEADER_FOOTER\n\ndef image_blips(part):\n    \"\"\"\
    Yield (rId, extent_cx, extent_cy) for every picture in a part.\"\"\"\n    out = []\n    for blip in\
    \ part.element.iter(qn('a:blip')):\n        rid = blip.get(qn('r:embed')) or blip.get(qn('r:link'))\n\
    \        ext = None\n        # inline vs anchored: both have <wp:extent>\n        for e in part.element.iter():\n\
    \            if e.tag in (qn('wp:extent'),):\n                ext = (e.get('cx'), e.get('cy')); break\n\
    \        out.append((rid, ext))\n    return out\n\nseen = set()\nfor i, sec in enumerate(doc.sections):\n\
    \    # is_linked_to_previous matters: a linked header is inherited, not duplicated\n    for kind,\
    \ hf in (('header', sec.header), ('footer', sec.footer),\n                     ('first_page_header',\
    \ sec.first_page_header),\n                     ('even_page_header', sec.even_page_header)):\n   \
    \     hf.is_linked_to_previous = hf.is_linked_to_previous  # read, don't set\n        if hf.is_linked_to_previous:\n\
    \            continue\n        part = hf.part\n        if part.partname in seen:\n            continue\n\
    \        seen.add(part.partname)\n        for rid, ext in image_blips(part):\n            ip = part.related_parts.get(rid)\n\
    \            print(i, kind, part.partname, rid, ip.partname, ip.content_type, ext)\n```\nRun that\
    \ before editing: it tells you the header part, the `rId`, the media\npartname, the content type,\
    \ and the current display size. Repeat for\n`even_page_header` / `first_page_header` because \"different\
    \ first page\" and\n\"different odd/even\" settings produce a *second*, separately-linked logo that\
    \ also\nshows on page 3 — a very common cause of \"it changed somewhere I didn't touch\".\n\nThen\
    \ replace it, keeping position and size, without cloning (you want ALL pages to\nchange):\n\n```python\n\
    part = doc.sections[0].header.part\nrid, (cx, cy) = image_blips(part)[0]\nip = part.related_parts[rid]\n\
    if not ip.partname.ext.lower() == '.png':\n    raise ValueError('keep the same format to avoid [Content_Types]\
    \ surgery')\nip._blob = open('new_logo.png', 'rb').read()\n# wp:extent / a:ext untouched -> identical\
    \ position, size, wrap, z-order\n```\n\nOther places a \"page-everywhere\" logo can hide, worth checking\
    \ before concluding\nanything:\n- A **floating (anchored) drawing** `<wp:anchor>` rather than `<wp:inline>`\
    \ — not in\n  `inline_shapes` even inside the body. Use the `a:blip` iteration above, which\n  catches\
    \ both.\n- A **VML/legacy shape** `<w:pict><v:shape><v:imagedata r:id=\"...\"/>` from an older\n \
    \ template. Search for `qn('v:imagedata')` too; its relationship type is `image`\n  but it is not\
    \ an `a:blip`.\n- A **background image** `<w:background>` / `<w:displayBackgroundShape/>`, or a\n\
    \  theme fill picture.\n- The image being a **linked** external file rather than embedded — then the\
    \ blip\n  carries `r:link` instead of `r:embed`, there is no part to overwrite, and you must\n  write\
    \ the file at the linked path (or convert it to embedded by replacing the\n  `r:link` with an `r:embed`\
    \ to a newly added part).\n\nVerification, because this whole class of bug is invisible by construction:\n\
    ```bash\nmkdir -p x && cd x && unzip -o ../invoice.docx >/dev/null\nls -l word/media/\n# which parts\
    \ reference which media, including header rels\nfor r in $(find word -name '*.rels'); do\n  echo \"\
    == $r\"; grep -o 'Target=\"media/[^\"]*\"' \"$r\" | sort | uniq -c\ndone\n```\nAnd confirm you edited\
    \ the part you think you did by comparing byte sizes before and\nafter in the extracted tree.\n"
- id: docx-imgins-01
  answer: "`Document.add_picture(image_path)` is `document.inline_shapes.add_picture(...)`,\nwhich is\
    \ `self._body.add_paragraph().add_run().add_picture(...)` — so it\n**appends a brand-new paragraph\
    \ at the end of the body** containing the image. It is\nnever inserted relative to anything. On a\
    \ real document that means the signature\nlands after the last paragraph, usually on a new page or\
    \ after the signature block,\nand the paragraph you targeted still has nothing in it. (The same is\
    \ true of\n`doc.add_paragraph()`.)\n\nThe correct pattern is: add to the end to let python-docx build\
    \ the part/relationship\ncorrectly, then **move the run's drawing element** to the target paragraph.\n\
    \n```python\nfrom docx.shared import Inches\n\ndef insert_picture_after(paragraph, path, width_in):\n\
    \    doc = paragraph.part.document          # or keep a reference to `doc`\n    # 1. append (creates\
    \ the image part + relationship + inline drawing)\n    shape = doc.add_picture(path, width=Inches(width_in))\n\
    \    drawing = shape._inline.getparent()    # the <w:drawing>'s <w:r> wrapper\n    run_el = drawing.getparent()\
    \           # the <w:r>\n    # 2. relocate that run into the target paragraph, at the end\n    paragraph._p.append(run_el)\n\
    \    return shape\n```\n`shape._inline` is the `<wp:inline>`; its parent is the `<w:drawing>`, whose\
    \ parent is\nthe `<w:r>`. Moving the whole `<w:r>` (not the `wp:inline` alone) keeps it in a valid\n\
    position — `w:drawing` is only legal inside a `w:r`.\n\nCleaner, if you can run the search first,\
    \ is to create the element in place and\n**build the relationship yourself**:\n\n```python\nfrom docx.opc.constants\
    \ import RELATIONSHIP_TYPE as RT\nfrom docx.text.run import Run\n\ntarget = next(p for p in doc.paragraphs\
    \ if p.text.strip() == 'Approved by:')\nrid = doc.part.relate_to(path, RT.IMAGE)          # part inferred\
    \ from file ext\ntarget._p.append(make_run_with_picture_xml(rid, cx, cy))\n```\nIn practice the add-then-move\
    \ version is less error-prone, and moving a `<w:r>`\nbetween paragraphs within the same part is schema-safe\
    \ (no relationship\nre-registration needed, because the `rId` lives in the part's `.rels`, which is\n\
    unchanged).\n\n**Keeping the aspect ratio**: pass only `width=` (or only `height=`) and\npython-docx\
    \ computes the other dimension from the image's own DPI and pixel size.\nPassing *both* `width` and\
    \ `height` forces exactly that box and will distort the\nimage if the ratio differs. If you need to\
    \ re-derive the ratio later (e.g. because\nthe aspect is wrong after a template change):\n\n```python\n\
    from PIL import Image\nfrom docx.shared import Emu\nw_px, h_px = Image.open(path).size\nshape.width\
    \  = Inches(1.5)\nshape.height = Emu(int(Inches(1.5).emu * h_px / w_px))\n```\n1.5 inches = 1,371,600\
    \ EMU. Anything the shape has already been given via\n`wp:extent` and `<a:xfrm><a:ext>` should match;\
    \ if the two disagree, some renderer\nwins and you get a subtly wrong size — set `shape.width`/`shape.height`\
    \ through the\nAPI rather than editing `wp:extent` alone.\n\nAlso worth knowing, because \"insert\
    \ after X\" usually means one of these:\n- Insert *between two paragraphs*: anchor on the paragraph\
    \ **before** the intended\n  position and use `addnext()`.\n- Insert after a specific **run** of text\
    \ (\"Approved by: ____\"): find the run, then\n  `run._r.addnext(new_run_element)` so the image lands\
    \ mid-paragraph.\n- Insert **inline in a table cell**: `cell.paragraphs[-1].add_run().add_picture(...)`\n\
    \  — there is no `cell.add_picture`, and the image must be in a paragraph inside the\n  cell.\n- If\
    \ the paragraph is inside a **text box** or a floating shape's `w:txbxContent`,\n  it is a separate\
    \ mini-document; move the run there and keep the relationship on the\n  *owning* part (`header.part`,\
    \ etc.), not `doc.part`.\n\nFinally, `paragraph.part.document` is a convenient way to reach `Document`\
    \ from a\nparagraph, but if the paragraph belongs to a header/footer part, `part.document`\ndoes not\
    \ exist — hold an explicit `doc` reference instead.\n"
- id: docx-imgins-02
  answer: "Two things happen, and both are surprising.\n\n1. **The picture keeps the size you asked for\
    \ and the cell does not grow to\n   accommodate it properly.** `add_picture(width=Inches(3))` writes\n\
    \   `wp:extent cx=2743200` and `<a:ext cx=2743200 cy=...>` into the XML. Word\n   *does* widen a single-cell\
    \ table's column to fit an oversized inline image (the\n   column's `w:gridCol` and the cell's `w:tcW`\
    \ are recalculated on layout), which\n   means the table silently grows ~1.8\" wider than the page\
    \ text area. The image then\n   overhangs the right margin, or the table is pushed onto its own page.\
    \ If the cell\n   is in a table with other rows, those rows keep their width and you get a column\n\
    \   of mismatched cell widths — the classic \"the whole invoice is crooked\" symptom.\n2. **The aspect\
    \ ratio is silently preserved, so height may blow up.** A 3\"-wide\n   16:9 screenshot in a 1.2\"\
    -wide cell becomes 3\" x 1.6875\" — the extra height\n   overflows the cell vertically and can clip\
    \ or overlap the text below.\n\nSize it explicitly, from the *cell's* width, not from the page:\n\n\
    ```python\nfrom docx.shared import Inches, Emu, Pt\nfrom docx.shared import Inches as _In\n\ncell\
    \ = table.cell(0, 0)\n\n# usable width inside the cell = column width - left/right cell margins\n\
    avail = cell.width                       # w:tcW, the column width\ntbl  = cell._tc.getparent().tblPr\n\
    # subtract default cell padding (0.08\" each side) if tcW is not already the text width\nusable =\
    \ Emu(avail.emu - Inches(0.16).emu)\n```\n\nThen set the picture from that, preserving ratio:\n\n\
    ```python\nimport io\nfrom PIL import Image\nfrom docx.image.image import Image as DocxImage\n\ndef\
    \ fit_picture_to_cell(run, path, max_w_emu, max_h_emu=None):\n    img = DocxImage.from_file(path)\n\
    \    w_px, h_px = img.px_width, img.px_height\n    # px_width/px_height are ints; ratio from pixels\
    \ is exact\n    scale = min(max_w_emu / w_px, (max_h_emu / h_px) if max_h_emu else 1e18)\n    return\
    \ run.add_picture(path, width=Emu(int(w_px * scale)))\n\np = cell.paragraphs[0]\nrun = p.add_run()\n\
    shape = fit_picture_to_cell(run, 'photo.png', usable)\n```\n`Image.from_file(...).px_width/.px_height`\
    \ gives the true pixel size, so the ratio is\nreliable even when the PNG carries no DPI chunk (which\
    \ is why trusting\n`img.width/img.height` inches can be off).\n\nSupporting changes that make it actually\
    \ stay inside the cell:\n- **Lock the table layout** so Word does not re-autofit and undo you:\n \
    \ `table.autofit = False` (emits `w:tblLayout w:type=\"fixed\"`), then set\n  `table.columns[0].width`\
    \ / `cell.width` explicitly in EMU/Inches and make the sum\n  of column widths equal the intended\
    \ total (`tblW`).\n- **Cell padding**: set `w:tblCellMar` to 0 (or an explicit value) so you know\
    \ the\n  text width, instead of relying on the 0.08\" default that differs from the template.\n- **Set\
    \ the paragraph to no-indent/no-spacing** in the cell, or the image will be\n  offset by the paragraph\
    \ indent and push the effective width over the limit.\n- **Turn off \"resize with cell\" ambiguity**\
    \ by also matching the cell's\n  `w:tcW` to the image width, and make sure `w:gridCol` agrees, or\
    \ a fixed-layout\n  table will clip the image at the grid boundary.\n- Optionally `shape.height` computed\
    \ from the ratio so the cell's row height\n  (`w:trHeight`) can be set with `height_rule = WD_ROW_HEIGHT_RULE.AT_LEAST`\
    \ rather\n  than EXACTLY, so the row grows instead of clipping.\n- For a logo that must not distort,\
    \ alternative is to crop-to-fill to the cell's\n  aspect ratio before embedding rather than scaling\
    \ non-uniformly.\n\nA quick post-save check that the geometry is self-consistent:\n\n```python\nfor\
    \ t in doc.tables:\n    for row in t.rows:\n        for c in row.cells:\n            tcW = c._tc.tcPr.find(qn('w:tcW'))\n\
    \            for ext in c._tc.iter(qn('wp:extent')):\n                print('cell w=', tcW.get(qn('w:w'))\
    \ if tcW is not None else None,\n                      'img cx=', int(ext.get('cx'))/914400)\n```\n\
    If image `cx` exceeds the cell's `tcW` (minus margins), it will overflow — that is\nthe condition\
    \ you are trying to avoid, and it is measurable rather than eyeballed.\n"
- id: docx-legacy-01
  answer: "No. python-docx is built on `python-docx`'s OPC/ZIP layer and supports only the\nOffice Open\
    \ XML `.docx` container (a ZIP of `word/document.xml` and friends). It\nuses `zipfile` and its own\
    \ `Document()` loader, which reads `word/document.xml` from\nthe package. A Word 97–2003 `.doc` is\
    \ an **OLE2 / Compound File Binary\n(`CFB`)** container with a WordDocument stream and a piece table\
    \ — a completely\ndifferent, undocumented binary format. The failure is loud and immediate:\n`zipfile.BadZipFile:\
    \ File is not a zip file` (or `PackageNotFoundError` for a\nnon-package path). There is no partial/legacy\
    \ read mode.\n\nWorkflow — convert first, edit, then be honest about fidelity:\n\n1. **Convert .doc\
    \ → .docx.** Any one of:\n   - LibreOffice CLI (best fidelity for tables, and scriptable):\n     `soffice\
    \ --headless --convert-to docx:\"MS Word 2007 XML\" --outdir out report.doc`\n     (see docx-legacy-03\
    \ for the profile/lock pitfalls).\n   - macOS `textutil -convert docx report.doc` — works, but see\
    \ docx-legacy-02.\n   - Word itself via AppleScript / COM automation (`win32com` on Windows) — highest\n\
    \     fidelity, but requires Word and is awkward to script portably.\n2. **Verify the conversion before\
    \ editing.** Unzip and sanity-check:\n   `unzip -l report.docx`, confirm `word/document.xml` exists,\
    \ confirm the expected\n   number of `<w:tbl>` elements, count `<w:tr>` rows in the target table,\
    \ and dump\n   the text: `python -c \"import docx;print(docx.Document('report.docx').paragraphs[:5])\"\
    `.\n   Compare against the original .doc's visible table shape.\n3. **Edit the converted .docx with\
    \ python-docx.**\n4. **Save under a new name** — never overwrite the `.doc`; keep the original as\
    \ the\n   source of truth and record the conversion tool/version in your notes.\n5. **Deliver the\
    \ .docx, not the .doc.** Round-tripping .docx → .doc (LibreOffice\n   `MS Word 97` filter) loses a\
    \ second layer of fidelity and is a good way to\n   surprise the user. If they must have a `.doc`,\
    \ produce it as a final step and say\n   that the deliverable was authored in `.docx`.\n6. **Re-open\
    \ the output in python-docx and in Word/LibreOffice** before declaring it\n   done (see docx-verify-01/02).\n\
    \nRelated limits worth stating up front to the user: `.doc` files containing tracked\nchanges, comments,\
    \ embedded OLE objects, macros (`.docm`), equation Editor 3.0\nobjects, legacy form fields, and ink\
    \ annotations are the classic casualties of\nconversion. If the table is simple (rows/cols, cell text,\
    \ borders), conversion is\nsafe. If it has floating shapes, anchored text boxes, or nested tables\
    \ with merged\ncells, verify each of those explicitly after conversion.\n\nIf you must stay in `.doc`,\
    \ python-docx is the wrong tool entirely — you would need\n`pywin32` (Windows) or AppleScript/JXA\
    \ (macOS) to drive Word itself, or a library\nthat can read the binary format. There is no pure-Python\
    \ `python-docx`-adjacent\nlibrary that usefully round-trips `.doc` tables.\n"
- id: docx-legacy-02
  answer: "`textutil` uses macOS's legacy **Cocoa text system** (`NSAttributedString` /\n`NSTextView`\
    \ importers) for `.doc`, not a Word-compatible OOXML engine. It is\ntherefore a *lossy best-effort*\
    \ converter. For a table-editing task the risk is\nconcrete and severe:\n\n- **Tables are flattened\
    \ or reconstructed as plain text / tab-delimited rows.**\n  This is the big one. NSAttributedString's\
    \ `.doc` importer represents Word tables as\n  runs with tab characters and paragraph breaks. Cell\
    \ boundaries, row/column structure,\n  `vMerge`/`gridSpan` (merged cells), nested tables, and repeating\
    \ header rows are\n  frequently lost. You can end up with a \"table\" that is one paragraph per line\
    \ with\n  literal tabs — and, critically, `doc.tables` in python-docx will be **empty or\n  short**,\
    \ so `doc.tables[3].rows[2]._tr` will raise `IndexError` while the document\n  still *looks* like\
    \ it has a table when you open it in Word. Silent structural loss\n  is the failure mode to fear,\
    \ not a hard error.\n- **No `w:tblPr` fidelity**: no theme-linked styles, no `tblW`/`gridCol` widths\n\
    \  (so column widths come out as autofit), no `tblLook`/`tblBorders`, no cell\n  margins, no shading,\
    \ no `tblLayout`, no `tblCaption`, no accessibility header-row\n  flags.\n- **Style names are lost\
    \ or renamed.** Styles arrive as direct formatting or\n  LibreOffice/macOS-generated names, so `table.style\
    \ = 'Grid Table 4 - Accent 1'`\n  or any named style lookup raises `KeyError` (see docx-tblins-02)\
    \ — and character\n  style names, heading levels, and numbering definitions are frequently gone or\n\
    \  demoted to direct formatting, which breaks the table of contents and heading\n  navigation.\n-\
    \ **Headers/footers and section properties** are approximated; the first-page /\n  even-odd settings\
    \ and column layouts are not preserved.\n- **Images and floating objects** can be lost, downsampled,\
    \ or converted; anchored\n  drawings may be re-created at different sizes or dropped.\n- **Numbering,\
    \ footnotes, comments, tracked changes, hyperlinks, and fields**\n  (TOC, PAGE, REF) are frequently\
    \ stripped or turned into static text. Any field you\n  touch afterwards will not update.\n- **Document\
    \ properties and custom XML** are not carried over.\n- The generated file is a *new* document, so\
    \ \"the template\" you were asked to\n  preserve is gone: theme, fonts, colours, logo, and the master\
    \ table geometry all\n  have to be re-created.\n\nHow to detect it — do not trust the extension or\
    \ the exit code (`textutil` exits 0\non lossy output):\n\n```bash\n# 1. structural census: does the\
    \ table survive as w:tbl?\nmkdir -p x && cd x && unzip -o ../report.docx >/dev/null\nfor f in word/document.xml\
    \ word/header*.xml word/footer*.xml; do\n  [ -e \"$f\" ] || continue\n  echo \"$f  tbl=$(grep -o '<w:tbl>'\
    \ \"$f\" | wc -l) \\\n              tr=$(grep -o '<w:tr[ >]' \"$f\" | wc -l) \\\n              tc=$(grep\
    \ -o '<w:tc>' \"$f\" | wc -l) \\\n              vMerge=$(grep -o 'w:vMerge' \"$f\" | wc -l) \\\n \
    \             gridSpan=$(grep -o 'w:gridSpan' \"$f\" | wc -l)\"\ndone\n\n# 2. did tables degrade to\
    \ tabs?\ngrep -c '<w:tab/>' word/document.xml\n```\n\n```python\n# 3. the decisive check, in python-docx\
    \ terms\nimport docx\nd = docx.Document('report.docx')\nprint('tables:', len(d.tables), [ (len(t.rows),\
    \ len(t.columns)) for t in d.tables ])\nprint('has tabs in cells:', any('\\t' in c.text for t in d.tables\
    \ for r in t.rows for c in r.cells))\nprint('table styles present:', [s.name for s in d.styles if\
    \ s.name and 'Grid' in s.name])\nprint('header images:', [p.partname for p in d.part.package.parts\n\
    \                         if 'header' in str(p.partname)])\n```\nIf `len(d.tables)` is 0 or the row/column\
    \ counts do not match the original, or you\nsee heavy tabbing, or a named style that you know exists\
    \ in the `.doc` is missing —\nthe conversion is lossy. Any of these is a hard stop: **re-do the conversion\
    \ with\nLibreOffice** (`soffice --headless --convert-to docx:...`, with\n`-env:UserInstallation=file:///tmp/lo_profile`\
    \ — see docx-legacy-03), which uses the\nreal Word filter and preserves `w:tbl`, `tblGrid`, merges,\
    \ styles and headers. If\nLibreOffice is unavailable, tell the user the conversion is lossy and ask\
    \ before\nediting, rather than silently producing a document with flattened tables.\n"
- id: docx-legacy-03
  answer: "The most likely cause is **profile/state contention with the already-running GUI\ninstance**.\
    \ LibreOffice is single-instance per *user profile*: the `soffice` launcher\ndetects a live process\
    \ using the same user-installation directory, hands the document\nto that running instance over IPC,\
    \ and then the CLI exits immediately — while the\nGUI instance, which was not asked to convert in\
    \ a way that writes to your `--outdir`,\nwrites nothing where you are looking. You see \"exits immediately,\
    \ no output file\".\nSecondary causes worth ruling out in the same breath:\n\n- **A `~/.config/libreoffice/4/user/.lock`\
    \ or `.~lock.report.doc#` file** left by a\n  crashed previous run, so a *new* profile also refuses\
    \ to start.\n- **`--headless` conflicting with a running soffice.bin** (e.g. launched via the app\n\
    \  bundle vs the CLI binary: `/Applications/LibreOffice.app/Contents/MacOS/soffice`\n  vs `/usr/bin/soffice`).\n\
    - **`--outdir` on a path the process cannot write**, or a relative path resolved\n  against a different\
    \ cwd.\n- **`--convert-to docx` needs the right filter name** to produce `.docx`; the bare\n  form\
    \ usually works, but on some builds you must pass\n  `docx:\"MS Word 2007 XML\"`.\n- **Zero exit status\
    \ but an exception in the user profile's stdout/stderr**, which\n  the script is discarding.\n\nHow\
    \ to run it reliably from a script — the key insight is to **never share a profile\nwith anything\
    \ that might be running**, and to **verify the output rather than trust\nthe exit code**:\n\n```bash\n\
    # 1. close the GUI, or just ignore it: use a private throwaway profile\nPROFILE=$(mktemp -d /tmp/lo_XXXXXX)\n\
    OUT=$(mktemp -d /tmp/loout_XXXXXX)\n\n# 2. a good wrapper, retrying a couple of times (first-run profile\
    \ creation is slow)\nconvert() {\n  local in=\"$1\"\n  for attempt in 1 2 3; do\n    rm -f \"$OUT/$(basename\
    \ \"${in%.*}\").docx\"\n    /usr/bin/soffice \\\n      -env:UserInstallation=\"file://$PROFILE\" \\\
    \n      --headless --norestore --nolockcheck --nodefault --nofirststartwizard \\\n      --convert-to\
    \ 'docx:MS Word 2007 XML' \\\n      --outdir \"$OUT\" \"$in\" >/dev/null 2>&1\n    [ -s \"$OUT/$(basename\
    \ \"${in%.*}\").docx\" ] && { echo \"$OUT/$(basename \"${in%.*}\").docx\"; return 0; }\n    sleep\
    \ 2\n  done\n  echo \"convert failed: $in\" >&2\n  return 1\n}\n```\n- `-env:UserInstallation=file://$PROFILE`\
    \ is the critical flag: it forces a private\n  profile directory so the running GUI instance is irrelevant\
    \ and no stale\n  `.lock` blocks you. Use a fresh `mktemp -d` per conversion (or per batch) and\n\
    \  `rm -rf` it afterwards.\n- `--norestore --nolockcheck --nodefault --nofirststartwizard` suppress\
    \ the\n  crash-recovery and first-run wizard paths that otherwise steal the process.\n- **Poll for\
    \ the file, don't trust exit 0** — see the loop above; also\n  `wait` for the process rather than\
    \ backgrounding it and reading too early, and add\n  a timeout so a wedged soffice cannot hang the\
    \ script.\n- **Kill stragglers** before starting:\n  `pkill -f soffice.bin` (optionally, and only\
    \ if you are willing to close the\n  user's open documents — otherwise use a per-script unique profile,\
    \ which is safer\n  and is why the private profile is the primary fix).\n- **If you must share the\
    \ default profile**, then quit the GUI first and confirm it\n  is gone: `osascript -e 'tell application\
    \ \"LibreOffice\" to quit'` then poll\n  `pgrep -f soffice` until empty.\n- Use the full bundle path\
    \ if `/usr/bin/soffice` is a stub:\n  `/Applications/LibreOffice.app/Contents/MacOS/soffice`.\n\n\
    ```python\n# 4. from Python, same discipline + a structural check on the result\nimport subprocess,\
    \ tempfile, shutil, time, os, pathlib, docx\n\nwith tempfile.TemporaryDirectory() as prof, tempfile.TemporaryDirectory()\
    \ as out:\n    target = pathlib.Path('report.docx')\n    for _ in range(3):\n        subprocess.run([\n\
    \            '/usr/bin/soffice', f'-env:UserInstallation=file://{prof}',\n            '--headless',\
    \ '--norestore', '--nolockcheck', '--nodefault',\n            '--convert-to', 'docx:MS Word 2007 XML',\
    \ '--outdir', out, 'report.doc',\n        ], timeout=180, capture_output=True)\n        if target.exists()\
    \ and target.stat().st_size > 0:\n            break\n        time.sleep(2)\n    else:\n        raise\
    \ RuntimeError('LibreOffice produced no output')\n    d = docx.Document(target)\n    assert len(d.tables)\
    \ > 0, 'suspicious: no tables survived conversion'\n```\nAnd if a private profile still will not convert\
    \ while the GUI is open, the honest\nescalation is: quit the GUI, convert, relaunch — plus switching\
    \ to LibreOffice's\nUNO/`unoconv` bridge, or `textutil` as a last resort with the lossy-conversion\n\
    warning from docx-legacy-02 attached.\n"
- id: docx-verify-01
  answer: "Safe procedure:\n\n1. Never edit in place. Copy the source to a work file, keep the original\
    \ byte-identical\n   as the reference artifact, and write every script output to a NEW path. Word\
    \ is the\n   one application that will \"helpfully\" rewrite and repair the file, so the untouched\n\
    \   original is the only trustworthy baseline for a diff.\n\n2. Prefer a library round-trip over raw\
    \ XML. python-docx (or docx-js / opc package-level\n   edits) will re-serialize valid XML for you.\
    \ Only drop to lxml/ElementTree when you must,\n   and then register every namespace prefix up front\
    \ and never rely on default namespace\n   behavior.\n\n3. Re-open the saved output programmatically,\
    \ not just visually:\n   - `Document(out)` must import with no exception.\n   - Assert structural\
    \ invariants against the original: paragraph count, table count,\n     per-table row/column counts,\
    \ section count, presence of headers/footers, number of\n     inline shapes, styles present. Any drift\
    \ is a bug, not a rounding error.\n   - Assert the specific edit landed: e.g. the cell text you wrote\
    \ is readable back, the\n     image run count changed by the expected delta.\n\n4. Validate the OPC\
    \ package itself:\n   - `zipfile.ZipFile(out).testzip()` returns None.\n   - Inspect `namelist()`:\
    \ no duplicate entry names, no absolute paths, no `..`\n     traversal, `[Content_Types].xml` present\
    \ exactly once, no stray temp/macOS files\n     (`._*`, `.DS_Store`).\n   - Every part extension has\
    \ a matching `Default` or `Override` in `[Content_Types].xml`.\n   - Every `r:id` / `r:embed` referenced\
    \ in any XML part resolves to a relationship in the\n     corresponding `_rels` file, and every relationship\
    \ `Target` (with `TargetMode=External`\n     treated separately) points at a part that actually exists\
    \ in the zip.\n   - No duplicate relationship Ids within a `.rels` part.\n   - Content types are correct\
    \ for media: PNG -> `image/png`, JPEG -> `image/jpeg`,\n     EMF/WMF likewise. A wrong content type\
    \ is a classic \"unreadable content\" trigger.\n\n5. Validate with a real consumer, because python-docx\
    \ is permissive and will happily load a\n   file Word rejects:\n   - `soffice --headless --convert-to\
    \ pdf out.docx` (LibreOffice) and confirm it exits 0 and\n     produces a non-trivial PDF.\n   - Then\
    \ actually open it in Word. The pass condition is not \"it opens\" but \"it opens\n     without the\
    \ repair dialog and without a recovery log\". If Word repairs the file, the\n     repair log names\
    \ the offending part/element — capture it, it is the fastest locator.\n\n6. Keep a change manifest\
    \ for the record: part list before vs after, added/removed parts,\n   relationship Ids added/removed,\
    \ and the assertion results from step 3. For an invoice\n   template this manifest is what lets an\
    \ auditor answer \"what changed and why\" later.\n"
- id: docx-verify-02
  answer: "The recurring theme is schema-order and cardinality violations: Word validates document.xml\n\
    against the wordprocessingml schema and repairs on the first mismatch, not the last.\n\nTable-specific\
    \ mistakes:\n\n- Child order inside `w:tc` is fixed: `w:tcPr` must come first, then block-level content\n\
    \  (`w:p`, `w:tbl`, `w:sdt`). Inserting `w:p` before `w:tcPr`, or splicing a bare `w:r` at\n  cell\
    \ level, is invalid.\n- A `w:tc` may not end with anything other than a paragraph. Deleting the last\
    \ `w:p` in a\n  cell, or leaving a `w:tc` whose only child is a nested `w:tbl`, is invalid — a `w:p`\
    \ must\n  follow the nested table.\n- `w:trPr` and `w:tcPr` children must appear in schema order (for\
    \ `w:trPr`: cnfStyle, divId,\n  gridBefore, gridAfter, wBefore, wAfter, cantSplit, trHeight, tblHeader,\
    \ tblCellSpacing,\n  jc, hidden). Correct elements in the wrong order still trigger repair.\n- `w:tblGrid`\
    \ is not updated to match the new cell layout. `gridCol` count must reconcile with\n  the widest row\
    \ after `w:gridSpan` is accounted for. Adding/removing a `w:tc` without\n  adding/removing the corresponding\
    \ `w:gridCol` (or adjusting spans) is a top cause.\n- Orphaned `w:gridBefore`/`w:gridAfter` or `w:gridSpan`\
    \ values left over from a deleted cell,\n  so the row's declared grid exceeds the table grid.\n- Structural\
    \ placement errors: a `w:tr` outside a `w:tbl`, a `w:tc` outside a `w:tr`, a\n  `w:tbl` nested directly\
    \ inside another `w:tbl` (needs an intervening `w:p`), a `w:tbl`\n  inside a `w:tr`, or a nested table\
    \ inside a cell's `w:tcPr` region.\n- Empty `w:tc` (no `w:p`) left behind by a deletion loop over\
    \ a row's cells.\n- Deleting rows/cells in reverse while holding stale indices, which silently removes\
    \ the\n  wrong element and leaves a malformed structure.\n\nXML/tooling mistakes that surface as \"\
    unreadable content\" in table work specifically:\n\n- Unregistered namespace prefixes on re-serialize:\
    \ ElementTree/lxml rewrites namespaces to\n  `ns0:`, `ns1:` unless you call `register_namespace` for\
    \ every one. If the root's\n  `mc:Ignorable=\"w14 w15 wp14\"` still names prefixes that are no longer\
    \ declared, the file\n  is invalid.\n- Losing `mc:Ignorable` / `mc:AlternateContent` declarations\
    \ when rebuilding the root element.\n- Copying a `w:tr` or `w:tc` from another document (or from a\
    \ template) without carrying the\n  namespace declarations and required attributes it depends on.\n\
    - Duplicate `w14:paraId` / `w14:textId` values, produced by deep-copying paragraphs inside a\n  cell.\
    \ These must be unique document-wide; duplicates are a known repair trigger.\n- Invalid attribute\
    \ values or types: non-integer `w:w`, `w:gridSpan` outside the allowed\n  range, `w:type` values not\
    \ in the enumeration, or `w:val` containing text where an\n  ST_OnOff boolean is expected.\n- Missing\
    \ required attributes on inserted elements, e.g. a `w:sdt` without `w:id`, or a\n  `w:tbl` without\
    \ the properties Word expects to resolve layout.\n- Text surgery (regex/string replace on the XML)\
    \ leaving unbalanced or mismatched tags,\n  duplicated `<w:p>` wrappers, or entity-escaping broken\
    \ so `&` appears raw.\n- Package-level: duplicate zip entry names, two `[Content_Types].xml` entries,\
    \ a media part\n  added without a `Default` content type for its extension, or a relationship pointing\
    \ to a\n  part that is not in the zip.\n\nDiagnostic method: bisect by rebuilding the document part-by-part\
    \ (document.xml, then each\n  header/footer), and read Word's repair log — it names the part and often\
    \ the element.\nSchema-validate document.xml against wml.xsd if you have it; that localizes the fault\
    \ far\nfaster than opening Word repeatedly.\n"
- id: docx-verify-03
  answer: "Treat the package as a graph and check both the parts and the edges. Removing the\n`w:drawing`\
    \ removes a *reference*, not the bytes.\n\n1. Confirm the old part is gone from the archive.\n   -\
    \ Unzip to a temp dir and list `word/media/`.\n   - Hash the old image's bytes (SHA-256) before the\
    \ edit, then hash every remaining media\n     file. A surviving match means the image is still in\
    \ the file, whether or not it is\n     referenced.\n   - Also check for the same content under a different\
    \ name/extension (e.g. re-encoded, or\n     a copy sitting in a header's media folder) by comparing\
    \ dimensions plus a perceptual\n     or exact byte comparison, not just filenames.\n\n2. Confirm every\
    \ reference to it is gone. Grep all XML parts — `word/document.xml`, every\n   `word/header*.xml`,\
    \ `word/footer*.xml`, `word/footnotes.xml`, `word/endnotes.xml`,\n   `word/comments.xml`, and any\
    \ `glossary/` or `customXml/` parts — for the old relationship\n   Id in `r:embed`, `r:link`, `r:id`,\
    \ `v:imagedata/@r:id`, or a VML `<w:pict>` fallback\n   inside `mc:AlternateContent`. Old drawings\
    \ frequently survive in an mc:Fallback branch\n   when only the mc:Choice branch was edited.\n\n3.\
    \ Confirm the relationships are gone. Inspect `word/_rels/document.xml.rels` and each\n   header/footer\
    \ `.rels`: the relationship whose `Target` was `media/<old>.png` must no\n   longer exist. Leaving\
    \ a dangling relationship to a missing part is itself a corruption\n   cause, so this must be removed,\
    \ not just the drawing.\n\n4. Check the non-obvious carriers:\n   - Theme fills / background images\
    \ referencing media.\n   - `docProps/thumbnail.jpeg`.\n   - OLE objects or embedded packages that\
    \ contain a copy of the image.\n   - Numbering/style parts, comments, and drawing canvas leftovers.\n\
    \   - Content types: remove a `Default`/`Override` entry for an extension no longer present,\n   \
    \  if you added a custom one (harmless to leave, but clean it for tidiness).\n\n5. If you edited via\
    \ python-docx: the image part is only written if it is still reachable\n   through the relationship\
    \ graph. Simply deleting the `w:drawing` in the tree is not enough\n   — you must also drop the relationship\
    \ (`part.drop_rel(rId)`) so the part becomes\n   unreachable and is not serialized. Conversely, note\
    \ that python-docx de-duplicates images\n   by content hash, so if the same bytes are used elsewhere\
    \ (e.g. a footer), one part is\n   shared by several relationships and removing one drawing will not\
    \ (and should not) delete\n   the part. Check the reference count before concluding the old image\
    \ survived.\n\n6. Positive confirmation: assert the new image exists in `word/media/`, has its own\n\
    \   relationship with a fresh Id, that its content type is declared, and that the total\n   `word/media`\
    \ entry count dropped by the expected delta.\n\n7. Final gates: `testzip()` clean, no dangling `r:embed`\
    \ anywhere, the document reopens in\n   python-docx, LibreOffice converts it, and Word opens it with\
    \ no repair dialog. If this is\n   a confidential logo, treat the source file as still sensitive until\
    \ step 1's hash check\n   proves the bytes are absent — and check the old file and any backups/temp\
    \ copies you made\n   separately, since the leak most often survives in the backup, not the deliverable.\n"
