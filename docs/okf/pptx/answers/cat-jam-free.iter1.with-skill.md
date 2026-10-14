# cat-jam-free — WITH-SKILL closed-book answers for pptx (opencode-go, 2026-10-14)
# Delivery: derived/pptx.cat-jam-free.SKILL.md body (front matter stripped) prepended to _prompts/pptx.md; one
# `ocode run -m opencode-go/cat-jam-free -effort med` session, isolated HOME with every tool denied, cwd holding only the sheet;
# session audited in sqlite: 1 user + 1 assistant message, 0 tool calls.

- id: pptx-model-01
  answer: |
    A table is stored as an a:tbl element inside a p:graphicFrame, under
    p:graphic/a:graphicData. Columns live in a:tblGrid/a:gridCol, rows in
    a:tr, cells in a:tc, and cell text in tc/txBody (a:p/a:r/a:t). Table
    properties are in a:tblPr.

- id: pptx-model-02
  answer: |
    DrawingML positions/sizes are EMUs: 914400 per inch and 12700 per point.
    A standard 16:9 slide is 13.333 x 7.5 in (12192000 x 6858000 EMU).
    python-pptx's built-in default presentation is commonly 4:3 (10 x 7.5 in),
    so read prs.slide_width/prs.slide_height instead of assuming either size.

- id: pptx-model-03
  answer: |
    The logo is usually on the slide master or slide layout, inherited by
    slides, so slide.shapes does not list it. Editing there changes every slide
    using that master/layout; per-slide exceptions need their own shapes or
    layout variants.

- id: pptx-locate-01
  answer: |
    Recurse through GroupShape.shapes, keep shapes with has_table, and select
    the table whose first row's stripped cell texts match the expected header. Then
    scan rows for row.cells[key_col].text.strip() == "Gadget D", collect all hits,
    and assert len(hits) == 1 before editing.

- id: pptx-locate-02
  answer: |
    In a merge, the origin a:tc carries gridSpan/rowSpan, while covered cells
    still exist with hMerge="1"/vMerge="1" and empty text. Keep one a:tc per
    a:gridCol in every row. Write text only to the origin; editing/deleting near
    a merge must adjust spans and add/remove matching covered cells together, then
    assert len(tr.tc_lst) == len(tbl.tblGrid.gridCol_lst) for every row.

- id: pptx-rowdel-01
  answer: |
    There is no table.rows.remove() API. Remove the underlying a:tr XML element,
    e.g. table._tbl.remove(table.rows[index]._tr). If merges are involved, fix
    gridSpan/rowSpan and covered cells in the same edit.

- id: pptx-rowdel-02
  answer: |
    Yes, the stale graphic frame height matters; it leaves incorrect geometry.
    Recompute and set gf.height = sum(row.height for row in table.rows), then
    check that shapes below do not overlap.

- id: pptx-rowdel-03
  answer: |
    With explicit a:tcPr/a:solidFill shading, deleting a physical row leaves the
    old color pattern, so two neighbours can match. With style bandRow banding,
    the renderer applies alternating colors by row index, so deletion stays clean.
    It depends on explicit fills versus style-driven banding.

- id: pptx-rowdel-04
  answer: |
    Hiding or recoloring is not a deletion: the text and row remain in the XML,
    remain searchable/exportable, can reappear in print/PDF/accessibility, and
    leave wrong layout. A real delete removes the a:tr and its text.

- id: pptx-cell-01
  answer: |
    cell.text = "SKU" replaces the text body with a new default paragraph/run,
    discarding the header's bold/colour/size run formatting. Preserve it by
    editing the existing first run: cell.text_frame.paragraphs[0].runs[0].text =
    "SKU", then removing any extra runs.

- id: pptx-cell-02
  answer: |
    PowerPoint keeps column widths fixed; the new text wraps and row height grows
    to fit, or overflows/clips if wrapping is disabled or limits are hit. Control
    it with column widths, row heights, autofit/table layout, font size, and
    verify overall frame height and overlaps.

- id: pptx-cell-03
  answer: |
    Update dependent totals/sums such as total quantity and total amount, plus
    tax, discount, or notes if applicable, since OOXML tables have no formulas.
    Also update table/row geometry if the change affects wrapped text height.

- id: pptx-relayout-01
  answer: |
    Deep-copy an existing data row's a:tr, insert it before the Total row in
    table._tbl, clear/write text through each cell's first run, and preserve
    merged-cell state if relevant. Set row height, update gf.height, and check
    vertical fit and overlap with shapes below.

- id: pptx-relayout-02
  answer: |
    Insert a new a:gridCol in a:tblGrid and a matching a:tc in every a:tr,
    including merge origins and covered cells, so each row still has one cell per
    grid column. Copy style and formatting, then redistribute widths so the total
    plus margins fits the slide. Ask if there is no room rather than overflowing.

- id: pptx-tblins-01
  answer: |
    Compute a free bounding box from existing shapes and slide dimensions, leaving
    margins. A simple safe choice is below existing content:
    top = existing_bottom + gap. Then call slide.shapes.add_table(rows, cols,
    left, top, width, height) and verify top + height <= slide_height and no
    shape overlap.

- id: pptx-tblins-02
  answer: |
    Copy the existing table's a:tblPr, including tableStyleId and firstRow/bandRow
    flags, plus its column grid widths, row heights, explicit a:tcPr fills, and
    run properties. add_table alone uses Medium Style 2 - Accent 1 with firstRow
    and bandRow on.

- id: pptx-tblins-03
  answer: |
    Check the placeholder type. A TablePlaceholder has insert_table(rows, cols).
    A generic content placeholder has none, so call
    slide.shapes.add_table(rows, cols, ph.left, ph.top, ph.width, ph.height) and
    then remove ph._element from its parent so no empty placeholder remains.

- id: pptx-imgrep-01
  answer: |
    Record the old picture's left/top/width/height and its position in spTree.
    Add the new image with one dimension supplied so aspect ratio is preserved,
    insert the new p:pic at the old element's index, remove the old p:pic, then
    drop the old image rel only if no other //@r:embed or //@r:link in that part
    uses it.

- id: pptx-imgrep-02
  answer: |
    Both pictures reference the same underlying image part/relationship, so
    overwriting ppt/media/... changes every user of that part. Give the replacement
    its own image part/relationship instead of mutating shared bytes.

- id: pptx-imgrep-03
  answer: |
    drop_rel reference-counts only //@r:id. Pictures use a:blip r:embed, which
    that scan misses, so the count is 0 and the rel is dropped even when another
    picture still uses it. Manually check //@r:embed | //@r:link first.

- id: pptx-imgins-01
  answer: |
    Preserve aspect by specifying width=Inches(2) and deriving height, or
    computing it from the image dimensions. Place it at bottom-right with margins:
    left = slide_width - width - margin and top = slide_height - height - margin.
    First verify that box does not overlap existing content or exceed the slide.

- id: pptx-imgins-02
  answer: |
    The picture placeholder imposes its own geometry and crop, so inserting a
    portrait photo into a differently proportioned placeholder causes cropping.
    Use add_picture with a fitted size, adjust the placeholder/crop, or choose a
    matching placeholder.

- id: pptx-legacy-01
  answer: |
    No; python-pptx opens only OOXML .pptx. Convert with
    soffice --headless --convert-to pptx --outdir out deck.ppt, inspect the
    converted tables/images/fonts, then edit. If the user needs .ppt, convert back
    and deliver .ppt while keeping the untouched original.

- id: pptx-legacy-02
  answer: |
    Before editing, compare the converted deck with the original for tables,
    images, fonts, and layout. After editing, reopen and verify both changes
    and unchanged content. Tell the user the round trip may be lossy, keep the
    original untouched, and deliver converted .ppt if .ppt was requested.

- id: pptx-verify-01
  answer: |
    Save to a new output file, reopen it, and assert the exact changed cell texts,
    order, totals, and frame geometry. Compare every other slide's shapes/XML/text
    with the original, search all parts for text that should be gone, hash-compare
    ppt/media when an image changed, and test that the file opens.

- id: pptx-verify-02
  answer: |
    Common causes include a row tc count differing from the gridCol count, bad
    merge spans or missing covered cells, invalid OOXML element order or missing
    required children, namespace problems, invalid style IDs, broken relationships,
    duplicate IDs, and malformed text runs or paragraphs.

- id: pptx-verify-03
  answer: |
    No, deleting p:pic can leave the image part and its relationship in the
    package. Check the zip media listing and search all slide XML and
    relationships for the old rId and r:embed/r:link references. Remove the
    relationship and part only when no references remain.
