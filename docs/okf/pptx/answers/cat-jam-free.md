# cat-jam-free — closed-book answers for pptx (opencode-go, 2026-10-14); answerer saw only _prompts/pptx.md
# Delivery: one `ocode run -m opencode-go/cat-jam-free -effort med` session, sheet attached via -f, empty scratch cwd;
# session audited in sqlite: 1 user + 1 assistant message, 0 tool calls.

- id: pptx-model-01
  answer: |
    A .pptx table is stored inside a slide's p:graphicFrame, whose a:graphic/a:graphicData points to an a:tbl. Columns are declared in a:tblGrid as a:gridCol elements; rows are a:tr; cells are a:tc. Text lives in a:tc/a:txBody/a:p/a:r/a:t. Merge state is carried on the originating a:tc (gridSpan/rowSpan); a:tblPr/a:tcPr carry style and cell properties.
- id: pptx-model-02
  answer: |
    DrawingML uses EMUs (English Metric Units): 1 inch = 914,400 EMU. python-pptx Length values are EMU-backed and can be expressed as inches or points. A default widescreen 16:9 deck is 13.333 x 7.5 inches (12,192,000 x 6,858,000 EMU); the common 4:3 default is 10 x 7.5 inches.
- id: pptx-model-03
  answer: |
    It is likely on the slide master or layout p:spTree, inherited by slides, rather than on an individual slide p:spTree. Therefore slide.shapes does not expose it. Editing it affects every slide using that master/layout, and master/layout shapes may need XML-level or master APIs rather than slide.shapes.
- id: pptx-locate-01
  answer: |
    Iterate prs.slides[2].shapes, test shape.has_table, then read shape.table. Rows come from table.rows; check the target cell text, e.g. row.cells[0].text.strip() == "Gadget D". Because runs can split visible text, for exact matching it is safer to concatenate a:t values in the cell. Row zero is the first row.
- id: pptx-locate-02
  answer: |
    A horizontal merge is represented by gridSpan on the originating a:tc, with rowSpan for vertical merges. Covered cells may be omitted or present as merged cells depending on the producer. When editing or deleting, you must respect the table grid: a spanned cell counts as multiple columns, and row/cell operations must keep each row's total grid width consistent.
- id: pptx-rowdel-01
  answer: |
    python-pptx has no table.rows.remove() or delete-row API. Delete at OOXML level by removing the row's a:tr from a:tbl, e.g. table._tbl.remove(row._tr). Then recalculate shading and frame height as needed.
- id: pptx-rowdel-02
  answer: |
    It can matter: the graphicFrame cy may leave extra empty space or clip the table if the table is now shorter. Fix it by summing the current row heights and assigning that value to the graphic frame/shape height, after accounting for units in EMUs/inches.
- id: pptx-rowdel-03
  answer: |
    If shading is stored as direct fills on cells or rows, deletion leaves formerly alternating neighboring rows with the same fill. If the table uses style-driven banding, the application can reapply conditional formatting based on the new row positions, so the artifact may not occur. The difference depends on direct fills versus table style rules.
- id: pptx-rowdel-04
  answer: |
    It only hides content visually while retaining data, table structure, and extractable text. That is fragile, inaccessible, bad for PDF/print and future edits, and can leave confidential content in the file. A real row deletion removes the data and keeps the table model consistent.
- id: pptx-cell-01
  answer: |
    cell.text = ... rebuilds the text body with a default paragraph/run and discards rPr formatting such as bold, size, and color. Keep formatting by editing an existing run's text (paragraphs[0].runs[0].text = ...) or by reading and reapplying font properties after assignment. Ensure at least one run exists.
- id: pptx-cell-02
  answer: |
    PowerPoint generally wraps the longer text within the existing cell width and increases the row height; column widths remain unless autofit changes them. Control layout by setting column widths, font size, text-frame wrapping, cell margins, and fixed versus automatic table layout, while ensuring the table stays within the slide bounds.
- id: pptx-cell-03
  answer: |
    Update all dependent values: the item's Amount, any subtotal, total, tax, discount, or grand total, plus any other slides, notes, charts, or documents that cite those figures. PPTX table text is static, so recalculation must be done manually or in the script.
- id: pptx-relayout-01
  answer: |
    There is no insert-row API. Deep-copy an existing a:tr (such as the Service G row or a template row), insert it before the Total row's a:tr with lxml, then replace text and recalculate totals, fills, and frame height. Copying the source row preserves formatting and cell structure.
- id: pptx-relayout-02
  answer: |
    Add an a:gridCol to a:tblGrid after the Item column and insert a matching a:tc into every a:tr at the same index, ideally copying an existing cell to inherit formatting. Keep the table on the slide by reducing a source column width so the total grid width still fits the slide's text area, and adjust the Total row's merge span if it must now cover the new column.
- id: pptx-tblins-01
  answer: |
    Read the bounding boxes of existing shapes to identify unused space within slide margins. Choose left/top and dimensions that do not overlap, then call slide.shapes.add_table(rows=3, cols=2, left, top, width, height) using inch or EMU values. Keep the resulting table inside the slide.
- id: pptx-tblins-02
  answer: |
    Match it by assigning or copying the existing table's table style, especially the a:tblPr tblStyle value, so both use the same table style. If needed, also align fonts and direct cell formatting; avoid claiming that add_table preserves deck-specific styling automatically.
- id: pptx-tblins-03
  answer: |
    Use the content placeholder's insertion API: get placeholder = slide.placeholders[index], then create with placeholder.insert_table(rows=..., cols=...) (returns a graphic frame with .table). This uses the placeholder's placement. Fallback to add_table at the placeholder's left/top/width/height.
- id: pptx-imgrep-01
  answer: |
    Record the old picture's position, size, and shape-tree index. Use slide.shapes.add_picture(new_path, left, top, width=old.width, height=old.height), then move the new p:pic XML element into the old shape's position in slide.shapes._spTree before removing the old picture. Alternatively update the existing p:pic blip to reference a new image part. This preserves geometry and z-order.
- id: pptx-imgrep-02
  answer: |
    Overwriting the bytes of a shared image part changes every picture whose relationship resolves to that same media part, including slide 7. Many generators reuse image parts across slides or references. Create or target a separate image part/relationship for the replacement instead of mutating shared bytes.
- id: pptx-imgrep-03
  answer: |
    drop_rel removes the slide-level relationship without checking whether other shapes still reference that rId or underlying part. If another picture used the same reference, it becomes dangling. Drop only after verifying no r:embed in that slide uses the relationship.
- id: pptx-imgins-01
  answer: |
    Determine the image's aspect ratio for a 2-inch width, so height = 2 inches * height_px / width_px. Place it with left = slide_width - margin - Inches(2) and top = slide_height - margin - height, then slide.shapes.add_picture(path, left, top, width=Inches(2)). Check occupied bounding boxes and move the picture to avoid overlap.
- id: pptx-imgins-02
  answer: |
    The photo's aspect ratio differs from the placeholder's, so PowerPoint scales and crops to fill the placeholder (or the placeholder crops overflow) rather than fitting the whole image. To keep the entire photo, match dimensions/aspect ratio, adjust crop values, or insert it as a free picture rather than through the placeholder.
- id: pptx-legacy-01
  answer: |
    No, python-pptx cannot open .ppt; it handles OOXML .pptx only. Convert the deck first, for example with LibreOffice headless or PowerPoint, then edit the resulting .pptx. Keep the original .ppt because conversion may not preserve every feature.
- id: pptx-legacy-02
  answer: |
    Before editing, compare the converted deck visually and structurally with the original for tables, images, fonts, charts, and layout. After editing, reopen the .pptx and validate it, optionally converting to PDF for inspection. Tell the user the result is .pptx, the original .ppt remains separate, and LibreOffice conversion may alter fidelity, styles, or unsupported features.
- id: pptx-verify-01
  answer: |
    Save to a new output file while keeping the original untouched. Then verify zip integrity (unzip -t), reopen with python-pptx and assert expected shapes/tables/text, and render to PDF if possible for a visual check. Promote the output only after these checks pass.
- id: pptx-verify-02
  answer: |
    Common causes include malformed XML, incorrect attribute values, invalid namespace or element order, mismatched table grid and merged-cell counts, dangling or duplicate relationship references, missing required child elements, invalid text encoding, and broken part references. These trigger Office's repair behavior.
- id: pptx-verify-03
  answer: |
    No, deleting the p:pic removes the reference but the image media part and relationship can remain in the .pptx package, including possibly thumbnails or other references. Check with unzip -l, Python zipfile, or relationship inspection across ppt/media and slide rels. Remove orphaned parts if confidentiality requires.
