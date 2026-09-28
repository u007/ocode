- id: docx-model-01
  answer: 'A .docx is a ZIP archive following the Open Packaging Conventions (OPC): it contains [Content_Types].xml,
    _rels/.rels, the main part word/document.xml, plus word/styles.xml, word/settings.xml, word/_rels/document.xml.rels,
    docProps/*, etc. A table''s data lives in the main document part (word/document.xml), inside the body,
    as a <w:tbl> element; nested tables live inside a table cell.

    Element chain from table to text:

    w:tbl (table) > w:tblPr (table properties), w:tblGrid (with w:gridCol entries), and one w:tr per row;

    w:tr > w:trPr, then one or more w:tc (cells);

    w:tc > w:tcPr (w:tcW width, w:gridSpan, w:vMerge, w:shd shading, borders) followed by block content;

    the cell''s block content is one or more w:p (paragraphs) > w:pPr, then w:r (runs) > w:rPr, and the
    actual characters are in w:t (with w:tab / w:br for tabs and breaks).

    So the full path is w:tbl > w:tr > w:tc > w:p > w:r > w:t.

    '
- id: docx-model-02
  answer: 'Because a visible string is usually split across multiple runs. Word breaks runs at formatting
    changes, proofing markers (w:proofErr), revision boundaries, and edit-history (rsid) boundaries, so
    "Widget C" can be stored as "Widget " + "C" (or even "Wid" + "get C"). run.text returns only the text
    of one run, so an equality test against the whole string fails. run.text also ignores text held in
    field instructions (w:instrText) and other non-w:t elements, and the stored characters can differ
    (spacing, curly vs straight quotes, capitalization).

    Reliable approach: build the combined text of all runs in the paragraph/cell (paragraph.text, or a
    per-run offset map), search the concatenated string, then map the match offsets back to the runs.
    To replace while keeping formatting: in the common case write prefix into the first matched run, the
    replacement into a single run (copying that run''s rPr), and suffix into the last matched run, blanking
    the runs in between; in the general case operate on w:t nodes via an offset-aware merge helper. Normalize
    whitespace/quotes as needed, and check for w:proofErr and w:ins/w:del inside the cell before rewriting.
    Never assume one run equals one visible string.

    '
- id: docx-model-03
  answer: 'Table and cell geometry — w:tblW, w:tcW, w:gridCol/@w:w, w:trHeight, page size and margins
    (w:pgSz, w:pgMar) — is expressed in twips (dxa), i.e. twentieths of a point. 1 twip = 1/20 pt; 1 pt
    = 20 twips; 1 inch = 1440 twips; 1 cm ≈ 567 twips. So a 4320-twip column is 216 pt = 3 in. Widths
    may instead be percent (w:type="pct", in fiftieths of a percent) or "auto"/"nil".

    Image/drawing sizes (wp:extent/@cx,cy, a:ext/@cx,cy, a:off) are in EMUs (English Metric Units). 1
    inch = 914400 EMU; 1 pt = 12700 EMU; 1 cm = 360000 EMU; 1 twip = 635 EMU; 1 EMU = 1/914400 in.

    (Note: font sizes in w:sz are half-points, a different unit again.)

    '
- id: docx-locate-01
  answer: 'Do not use positional indexing (document.tables[2]) — table order and positions shift, and
    tables can be nested in cells, headers, footers, or text boxes. Identify the table by a structural
    signature: iterate document.tables (and nested cell.tables) and match its header row text (e.g. first
    row contains "Item" and "Qty"/"Unit Price") or a nearby caption/preceding paragraph containing "Invoice".
    Then find the row by matching a data cell''s joined run text (paragraph.text.strip()) against "Gadget
    D", usually in the Item/Description column, remembering there may be a header row offset.

    Before editing check: the match is unique (count matches; add context such as a neighbouring column/row
    if more than one), the row has the expected grid-column count, whether cells are merged (gridSpan/vMerge)
    so indices are still meaningful, whether the row is a repeating header (w:tblHeader), whether tracked
    changes or hidden text create variants, whether the value is split across runs, and which table the
    row truly belongs to when several candidates exist. Also note that dependent totals/fields (subtotal,
    =SUM(ABOVE), totals) will need refreshing after the edit.

    '
- id: docx-locate-02
  answer: 'python-docx expands merged cells: for a cell with w:gridSpan="2", row.cells returns the SAME
    _Cell object twice, once per underlying grid column. Therefore len(row.cells) equals the number of
    grid columns, not the number of real <w:tc> elements.

    Consequences: "the second cell" by index is not a distinct physical cell — writing to it writes to
    the same underlying tc, so you silently overwrite the spanned cell''s text believing you edited a
    different cell; iterating row.cells to "count columns" overcounts (it reports grid columns, not cells)
    and misleads you about the row''s real structure; deleting an element at that index can corrupt the
    row.

    Use len(row.cells) only for grid-column arithmetic. To enumerate actual cells, walk the w:tc children
    of row._tr directly (or dedupe row.cells by underlying tc), and account for w:gridSpan when inserting
    or deleting cells.

    '
- id: docx-rowdel-01
  answer: 'No. python-docx has no public table.delete_row() and no row.delete() in the 1.x line. Row objects
    are thin wrappers over the <w:tr> element, so you delete by manipulating XML: tr = row._tr (alias
    row._element); tr.getparent().remove(tr); equivalently table._tbl.remove(row._tr). To delete by position,
    index the rows yourself, e.g. table._tbl.findall(qn(''w:tr''))[i]. Removing the w:tr element is the
    deletion; there is no higher-level method.

    '
- id: docx-rowdel-02
  answer: 'A vertical merge is encoded as a "restart" cell (<w:vMerge w:val="restart"/>, top of the merge)
    followed by "continue" cells (<w:vMerge/>, which contain no display text of their own and inherit
    the merge from the restart cell). If you simply remove the restart row, the following continue cells
    have no merge start: Word will typically merge them into whatever cell precedes the position (or render
    the column incorrectly/garbled, and some viewers drop the merge).

    Correct handling depends on intent. If the merged region should survive, promote the next row''s first
    cell to a restart by setting <w:vMerge w:val="restart"/> on it and leave the remaining rows'' cells
    as continue. If the whole merged region is being removed, strip the w:vMerge elements from the affected
    cells so they become ordinary cells. Keep the grid consistent (cell count, w:gridSpan, widths) either
    way. If the deleted row is merely one of the continuation rows, removing it is safe as long as the
    restart row and the remaining continue rows stay in order.

    '
- id: docx-rowdel-03
  answer: 'Word field results are cached in the runs between w:fldChar @w:fldCharType="separate" and "end".
    python-docx edits the XML but does not recalculate fields, and Word generally does not recompute table
    formulas on open. So after deleting the row the Total will still show the OLD, stale cached sum (as
    if the deleted row were still counted) until the field is updated (manual F9/print, or an automatic
    update).

    Handle it by either (a) computing the new sum yourself and rewriting the cached result run text while
    preserving the field structure (fldChar begin/separate/end), or (b) setting <w:updateFields w:val="true"/>
    in word/settings.xml so Word recalculates fields when it opens the file, or (c) replacing the formula
    with a literal number. Remember =SUM(ABOVE) sums the numbers above it in the same column, so the deleted
    row must be excluded; changing merges/cells can also change what "above" means. Re-read the cached
    value after saving to confirm the display.

    '
- id: docx-rowdel-04
  answer: 'Because the row, its cells, and its text still exist in the document XML/DOM. Setting height
    to 0 (exact) only clips rendering — the row and its borders can still appear and content can overflow
    depending on the height rule (exact vs atLeast) — and white text or hidden runs (w:vanish) are still
    stored. The text remains searchable/copyable, is still extracted by parsers and accessibility tools,
    can print if color is ignored, and reappears if formatting is reset or "show hidden text" is on. None
    of these tricks change table structure (w:tr count, grid, merges) or the stored data.

    To prove a real deletion, inspect the underlying XML: count the <w:tr> children of <w:tbl> (len(table.rows))
    before and after, confirm the cell text no longer appears in word/document.xml (unzip and grep), and
    check that row._tr.getparent() is None (element detached). A fresh round-trip parse that no longer
    finds the row is good evidence.

    '
- id: docx-cell-01
  answer: 'cell.text = "SKU" replaces the cell''s entire content with a single new paragraph holding one
    run with default (empty) rPr. All direct run formatting is lost: bold, 9 pt, and white font color
    are gone (text reverts to defaults such as 11 pt black), along with any highlight/underline and other
    rPr; paragraph-level properties (alignment, style, spacing) are also discarded because the old paragraphs
    are removed.

    What survives are the cell-level properties in w:tcPr, which the setter does not touch: shading (w:shd),
    borders, cell width (w:tcW), vertical alignment (w:vAlign), w:gridSpan, and w:vMerge.

    To preserve the look, do not use cell.text; edit an existing run instead (e.g. cell.paragraphs[0].runs[0].text
    = "SKU"), which keeps that run''s rPr and the paragraph''s pPr, or explicitly re-apply the bold/size/color
    to the new run.

    '
- id: docx-cell-02
  answer: 'Changing Qty 5 -> 12 is a source-data change, so every derived value must change with it: the
    line amount (qty x unit price), the subtotal, any discount, tax/VAT, shipping, the grand total/amount
    due, any balance/outstanding, and any running or cross-table totals. Also update text that repeats
    the quantity (e.g. "x5"), number-in-words, and cached Word field results such as { =SUM(ABOVE) } or
    w:fldSimple totals.

    Make them consistent by treating the quantity as the single source of truth and recomputing all dependents
    in one pass with explicit rounding rules (round line amounts first, then sum; match the document''s
    currency/decimal format), then write the values into the cells. If the line amount is itself a field,
    update its cached result too, and set w:updateFields (or otherwise refresh) so Word recomputes on
    open. After saving, re-parse the file and verify subtotal = sum(lines) and total = subtotal - discount
    + tax + shipping.

    '
- id: docx-cell-03
  answer: 'python-docx writes new text directly into w:t as ordinary content — it does not create tracked-change
    markup. So no <w:ins>/<w:del> (with w:author, w:date, w:id) is added, the edit is unattributed, and
    a reviewer sees the new value as though it were always there; this defeats the purpose of track changes
    and may violate review policy (Word may also treat it as an untracked edit). Worse, if the cell already
    contains tracked insertions/deletions, a naive rewrite can destroy or orphan that revision markup
    (e.g. w:delText inside w:del, or ins/del wrapping the run you edit), silently accepting/rejecting
    pending changes and risking malformed nesting.

    Consider: (a) if a clean, untracked edit is truly wanted, first accept/resolve existing revisions
    so the cell state is simple, then edit; or (b) if a genuine tracked edit is wanted, build <w:ins>/<w:del>
    elements yourself with author/date/id, preserving rPr, and keep w:rPrChange/w:pPrChange consistent.
    Also watch w:rsid values, comments anchored in the cell, and the w:trackRevisions flag in settings.xml.
    Validate the resulting XML, since hand-built revision markup is easy to get wrong.

    '
- id: docx-relayout-01
  answer: 'table.add_row() always appends a new <w:tr> at the END of the table (before any trailing elements)
    and creates one empty <w:tc> per grid column; it does not insert at a position and does not copy the
    styling of neighbouring rows (the new cells get default formatting).

    To insert in the middle: create the row (or deep-copy an existing data row with copy.deepcopy(row._tr)
    so it inherits shading, fonts, borders, and widths), edit/clear its text, then move the element into
    place with lxml — ref_tr.addnext(new_tr) to place it after "Service G" (addprevious to put it before),
    or tbl.insert(index, tr). If you used add_row(), detach the appended element from the end before/after
    inserting, or construct the tr yourself and never append it. After inserting, keep the grid consistent
    (cell count, w:gridSpan, w:vMerge), preserve any repeating header, and refresh dependent totals/fields
    (=SUM(ABOVE), subtotal, total). Verify the new row sits between the intended neighbours and before
    the Total row.

    '
- id: docx-relayout-02
  answer: 'table.add_column(width) appends a new column at the RIGHT end: it adds a <w:gridCol w:w="...">
    to w:tblGrid and a new empty <w:tc> to every row (all at the end), setting widths from the length
    you pass (converted to twips). It does not insert after the Item column, copy cell styling, or shrink
    the other columns.

    To do it properly: move the new gridCol and each new tc to the position after Item (lxml insert/addnext),
    keep gridCol order matching cell order, and copy formatting (tcPr/rPr/pPr) from the neighbouring column
    — header font/shading as well as body widths — and set the header text "SKU".

    For layout: the usable width is sectPr page width minus pgMar left/right (and any table indent), in
    twips. Since you added a column, rebalance the other columns'' gridCol widths and every cell''s w:tcW
    so their sum fits the usable width, set w:tblW to the new total, and set w:tblLayout w:type="fixed"
    so Word honors the grid instead of auto-fitting. Account for cell margins/borders, any w:gridSpan
    merges (a spanned cell''s tcW spans the summed grid columns), and refresh dependent totals. Verify
    sum(gridCol) <= available width and that the table renders inside the margins.

    '
- id: docx-relayout-03
  answer: 'Because w:gridCol widths are only a hint and are not authoritative for display. Word sizes
    columns primarily from each cell''s w:tcW (when set) together with the table width w:tblW and the
    layout algorithm. If w:tblLayout is absent or "autofit", Word recomputes column widths from content
    and available space and can ignore or override the gridCol values; a percent/auto w:tblW, a table
    style that fixes widths, or a w:tblGridChange revision record can also take precedence.

    Fix: set w:tblLayout w:type="fixed", update each cell''s w:tcW (dxa twips) to match its gridCol, keep
    sum(gridCol) equal to w:tblW (dxa) and within the usable page width, and ensure gridCol order/count
    matches the cells — including w:gridSpan merges, where the spanned cell''s tcW equals the sum of the
    merged grid columns. Clear or adjust conflicting table-style/revision width records. Then Word will
    show the new widths.

    '
- id: docx-tblins-01
  answer: '`doc.add_table()` always appends the table at the very end of the document body (after the
    last block, before the final `sectPr`) — it ignores your cursor/position, so it does not go after
    "Notes:" in the middle. To place it correctly, create the table and then move its XML element to the
    right spot: find the anchoring paragraph and insert the table element after it, e.g.

    `p = doc.paragraphs[i]  # the "Notes:" paragraph`

    `tbl = doc.add_table(rows, cols)`

    `p._p.addnext(tbl._tbl)`

    (or use `p._p.addnext(tbl._tbl)` / `next_paragraph._p.addprevious(tbl._tbl)`). This relocates the
    `w:tbl` so it renders immediately after that paragraph while keeping it in the body''s block sequence.

    '
- id: docx-tblins-02
  answer: 'python-docx resolves `table.style = "<name>"` by looking the name up in the document''s `styles.xml`
    (`w:styles`). "Grid Table 4 Accent 1" is a Word built-in style that is usually *latent* — it is not
    physically defined in a given template''s styles part until Word has saved/used it. If the style isn''t
    in `styles.xml`, the lookup raises `KeyError` (the name is unknown to that document). To get a ruled
    table you can either (a) use a style that the template actually defines, (b) add/enable the style
    definition in `styles.xml`, or (c) skip styles and apply borders directly via XML on `tblPr` (`w:tblBorders`:
    top/left/bottom/right/insideH/insideV with `w:val="single"`, size, color), or copy the style part
    from a document where the style exists. Option (c) is the reliable fallback when the template lacks
    the built-in style.

    '
- id: docx-tblins-03
  answer: 'By default `add_table(rows, cols)` creates a table sized to the full width of the usable text
    block (page width minus margins), with columns divided equally, preferences set to auto/autofit so
    Word may shrink/reflow to content. It does not inherit the existing invoice table''s geometry. To
    match the invoice table, read that table''s widths and copy them: set `table.autofit = False`, set
    the table width (`tblW`), copy the `w:tblGrid` grid column widths from the existing table, and set
    each column''s width and every cell''s width (`cell.width`) to the corresponding values. Matching
    the `tblGrid` plus per-cell widths is what makes the new table line up with the existing one.

    '
- id: docx-imgrep-01
  answer: 'The picture lives in `word/document.xml` as a `w:drawing` (containing `wp:inline`/`wp:anchor`
    → `a:graphic` → `pic:pic` → `pic:blipFill` → `a:blip`), and the `a:blip` carries `r:embed="rIdN"`.
    `rIdN` is a relationship in `word/_rels/document.xml.rels` that targets the bytes in `word/media/imageN.png`.
    To replace exactly one picture while keeping its position and size: locate the specific `a:blip`/`w:drawing`
    for the logo, resolve its `rId` to the image part, and replace *that part''s* bytes (or add a new
    media part and repoint only this `r:embed` to a new rId). Leave the surrounding drawing XML (extent,
    transform, anchoring) untouched so position/size are preserved; if the new image has a different aspect
    ratio, you may also need to adjust the `wp:extent`/`a:ext` values.

    '
- id: docx-imgrep-02
  answer: 'Because the same image part is shared: Word/python-docx deduplicate identical bytes into one
    `word/media/imageN.png` part referenced by a single relationship, and multiple `a:blip` elements can
    point at that same part (via the same or reused rId). Overwriting the part''s `_blob` in place therefore
    changes the bytes for *every* drawing that references it — including the unrelated picture on page
    3. Check whether the target rId/part is referenced by more than one drawing; if it is, don''t mutate
    the shared part. Instead add a new, unique image part and repoint only the logo''s `r:embed` to it,
    leaving the other picture on the original part.

    '
- id: docx-imgrep-03
  answer: 'It is not in the body at all — it is in a header (or footer), so it lives in `word/header1.xml`
    (etc.) with its own relationship part, not `word/document.xml`. `doc.inline_shapes` only enumerates
    inline shapes in the main document body, so header/footer drawings are invisible to it. Find it via
    the section header: `hdr = doc.sections[0].header` (check `hdr.is_linked_to_previous`), then walk
    the header''s XML (`hdr._element` / `hdr.paragraphs` → runs → `w:drawing` → `a:blip`) or its related
    parts to find the `r:embed`. Replace it the same way as a body image — swap the targeted header image
    part''s bytes, or add a new part and repoint that header blip''s rId — so the change appears on every
    page.

    '
- id: docx-imgins-01
  answer: '`doc.add_picture()` appends the picture in a new paragraph at the *end* of the document body,
    so it will not land after "Approved by:" on its own. To place it correctly, create a paragraph at
    the target position and add the picture to a run there, e.g. get the "Approved by:" paragraph, insert
    a following paragraph, then `run = new_p.add_run(); run.add_picture("signature.png", width=Inches(1.5))`
    (or `p._p.addnext(new_p._p)`). Keep the aspect ratio by passing only `width` (and not both width and
    height) — python-docx scales the height proportionally from the image''s native dimensions — or compute
    height = width × (native_height/native_width) yourself.

    '
- id: docx-imgins-02
  answer: 'python-docx does not auto-fit an image to the host cell; it inserts the image at the requested
    size regardless of the cell width. A 3-inch-wide image in a 1.2-inch cell will overflow — it renders
    wider than the cell/table, overlapping neighboring content or forcing/visually expanding the layout
    (the cell/table is not a clipping box). Size it explicitly to the cell''s available content width:
    read `cell.width` (or the effective column width from the table grid, minus cell margins) and pass
    that as `width=Inches(...)` to `add_picture`, letting height scale to keep the aspect ratio (or pass
    both width and height computed by the same ratio). Optionally also set the cell width and disable
    table autofit so Word honors it.

    '
- id: docx-legacy-01
  answer: 'No. python-docx only reads OOXML packages (.docx/.docm); `.doc` is the legacy binary OLE Compound
    File format and cannot be opened — it raises an error (not a valid zip / PackageNotFoundError). Workflow:
    convert the legacy file to .docx first (e.g. `soffice --headless --convert-to docx file.doc` with
    a private profile, or Word on Windows / `textutil` on macOS), verify the conversion preserved the
    table, then open the resulting .docx in python-docx, locate the table and delete the row (`tbl = doc.tables[0];
    tr = tbl.rows[i]._tr; tr.getparent().remove(tr)`), and save. Always keep the original `.doc` untouched.

    '
- id: docx-legacy-02
  answer: 'Risk: `textutil`''s old-binary→OOXML conversion is lossy for complex layouts. It can drop,
    merge, split, flatten, or restyle tables; lose merged/nested cells; change column widths; or alter
    text/whitespace — so a row you intended to delete may not exist as the same row, and resulting edits
    can corrupt the intended structure or silently discard content. Detect it by comparing structure before
    and after: convert, then open the .docx and compare table/cell/row counts, cell texts, and layout
    against the original (e.g. render both to PDF and diff, or inspect the converted table''s rows and
    grid vs. the source). If the row indices/structure don''t match, do not trust the conversion — use
    LibreOffice or Word instead.

    '
- id: docx-legacy-03
  answer: 'LibreOffice is single-instance by default: when a GUI `soffice` is already running, a new `--headless`
    invocation detects/forwards to the existing process (or collides on the shared user profile) and exits
    immediately, so no output is written. Run it reliably by giving the headless instance its own user
    profile directory so it starts a separate process, e.g. `soffice -env:UserInstallation=file:///tmp/lo_$$
    --headless --norestore --convert-to docx --outdir <dir> file.doc`, and wait for it to complete (check
    the exit status and that the output file exists) before proceeding. Alternatively close the GUI instance
    or run the conversion in a clean/isolated environment.

    '
- id: docx-verify-01
  answer: 'Safe procedure: (1) back up the original (copy `invoice.docx` aside) and never edit in place;
    (2) run the script on the working copy and save to a *new* temp path; (3) validate the output is a
    well-formed OOXML package — reopen it with python-docx, and/or `zipfile.ZipFile(...).testzip()` /
    `unzip -t`, and confirm required parts (`[Content_Types].xml`, `_rels`, `word/document.xml`) are present;
    (4) verify the intended changes (expected row/cell/text counts, target values) and that nothing else
    changed materially; (5) open it in Word/LibreOffice to confirm no "unreadable content" repair prompt;
    (6) only then replace the original with the validated temp file, keeping the backup until confirmed.

    '
- id: docx-verify-02
  answer: 'Common structural mistakes in raw-XML table edits that make Word report unreadable content:
    invalid child ordering per the WordprocessingML schema (e.g. `w:tblPr`/`w:tblGrid` before `w:tr`,
    `w:trPr` before `w:tc`, `w:tcPr` before cell content); a `w:tc` whose content no longer ends in a
    paragraph (`w:tc` must end with at least one `w:p` — deleting the last paragraph makes the cell invalid);
    malformed/unbalanced tags or bad nesting; missing or mismatched namespace declarations/prefixes; dangling
    `r:id`/`r:embed` references to relationships or parts that don''t exist (or orphan/inconsistent `.rels`);
    corrupting `[Content_Types].xml`; illegal `w:tbl` placement (inside a paragraph or where a block element
    isn''t allowed); and inconsistent merge/grid data (`w:gridSpan`/`w:vMerge`/`w:tblGrid` mismatches).
    Structural (schema-order and required-child) errors are the usual culprits — python-docx may still
    load the file because it is lax, while Word rejects it.

    '
- id: docx-verify-03
  answer: 'Removing the `w:drawing` from the XML does not necessarily remove the image bytes: the old
    image part can remain in `word/media/` (and its relationship in `word/_rels/document.xml.rels`) as
    an orphan. python-docx serializes parts reachable from the package graph, so if the relationship is
    still present the old bytes are kept; if the drawing was the only reference and the relationship was
    removed, the part becomes unreachable and is dropped on save. To be sure: treat the saved .docx as
    a zip (`zipfile`/`unzip`), list `word/media/` and check for the old image by name/size/hash, grep
    `document.xml` (and header XML) for any remaining `r:embed`/`r:id` pointing to it, and inspect `document.xml.rels`
    for orphan relationships. Confirm the confidential image part is absent (or contains none of the original
    bytes) — and if it lingers, delete the relationship/part (or rebuild the package) and re-verify. Also
    check headers/footers, since the logo may live there.

    '
