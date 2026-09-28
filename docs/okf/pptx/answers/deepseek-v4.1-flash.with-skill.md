- id: pptx-model-01
  answer: "A table is a DrawingML graphic frame inside the slide part (ppt/slides/slideN.xml):\np:graphicFrame\
    \ > p:xfrm (off/ext = position and size in EMU) > a:graphic > a:graphicData\n(uri \".../drawingml/2006/table\"\
    ) > a:tbl.\n\nInside a:tbl:\n- a:tblPr: table properties (a:tableStyleId, and boolean attributes firstRow,\
    \ firstCol,\n  lastRow, lastCol, bandRow, bandCol).\n- a:tblGrid: the columns; one a:gridCol per column,\
    \ each with a w attribute (width in EMU).\n  There is no separate row grid; row count = number of\
    \ a:tr.\n- a:tr: one per table row; contains one a:tc per grid column (so len(a:tc) should equal\n\
    \  len(a:gridCol)).\n- a:tc: a cell, holding a:tcPr (cell props: a:solidFill fill, borders a:lnL/lnR/lnT/lnB,\n\
    \  margins marL/marR/marT/marB, anchor) and a:txBody.\n- a:txBody: a:bodyPr (+ optional a:lstStyle)\
    \ and one or more a:p paragraphs; each a:p holds\n  a:r runs; each a:r has a:rPr (b, i, sz in centipoints,\
    \ a:solidFill, latin typeface) and\n  an a:t text element. A paragraph's own properties live in a:pPr.\n\
    \nMerged cells: the merge origin a:tc carries gridSpan (horizontal) and/or rowSpan (vertical)\nattributes;\
    \ covered cells still exist as real a:tc elements but carry hMerge=\"1\" / vMerge=\"1\"\nand their\
    \ text is ignored.\n"
- id: pptx-model-02
  answer: 'DrawingML / python-pptx use EMU (English Metric Units): 914400 EMU = 1 inch, 360000 EMU = 1
    cm,

    12700 EMU = 1 pt. Font size (sz) is in centipoints (hundredths of a point, so 14 pt = 1400).

    A Length is an int number of EMU; helpers like Inches(), Pt(), Cm(), Emu() convert.


    Slide dimensions come from p:sldSz in ppt/presentation.xml (prs.slide_width / prs.slide_height).

    The common 16:9 deck is 12192000 x 6858000 EMU = 13.333 in x 7.5 in. (The 4:3 default that

    ships with python-pptx''s own template is 9144000 x 6858000 = 10 in x 7.5 in.) Never assume the

    size - read prs.slide_width / prs.slide_height.

    '
- id: pptx-model-03
  answer: 'It is not on the slide: it lives on the slide layout and/or the slide master (or is a

    header/footer/placeholder inherited from there, or part of the layout background). python-pptx''s

    slide.shapes only enumerates the shapes physically present in that slide''s spTree; shapes

    inherited from the layout/master are not listed.


    Consequences: (a) editing it requires going to slide.slide_layout.shapes or

    slide.slide_layout.slide_master.shapes (and support for writing masters/layouts in python-pptx

    is limited, often needs raw XML); (b) the edit is global - it changes every slide that uses that

    layout/master, not just the one you are looking at; (c) a slide that has locally overridden the

    placeholder/shape will keep its own copy and won''t pick up the master change; (d) decks with

    multiple masters/layouts may show different logos. To change it on one slide only, add/override a

    copy on the slide; to change it everywhere, edit the layout/master once.

    '
- id: pptx-locate-01
  answer: 'Table on slide 3: prs.slides[2] (0-based). Iterate slide.shapes and keep shapes where

    shape.has_table; recurse into group shapes (MSO_SHAPE_TYPE.GROUP -> shape.shapes) because a table

    can sit inside a group. A slide can hold several tables, so don''t just take tables[0] - pick the

    one whose first row''s texts match the expected header.


    Row for "Gadget D": iterate table.rows, and for each row iterate row.cells, comparing

    cell.text_frame.text.strip() == "Gadget D" (compare parsed/concatenated text, not substrings and

    not raw bytes - text is split across runs arbitrarily). Collect all hits and assert exactly one

    before editing. Honours merges: each a:tr has one a:tc per a:gridCol, but a covered cell is

    hMerge/vMerge and only the merge origin carries the visible text (cell.is_merge_origin,

    cell.is_spanned, span_width/span_height), so check the origin cell. Watch that a cell may hold

    multiple runs/paragraphs - cell.text_frame.text joins them.

    '
- id: pptx-locate-02
  answer: 'In DrawingML a merge is expressed on grid cells, not with a dedicated merge object. The origin

    a:tc carries gridSpan="n" (horizontal span) and/or rowSpan="n" (vertical span). Every covered

    grid position still exists as its own a:tc in its a:tr, carrying hMerge="1" and/or vMerge="1"

    with ignored content. So a row''s cells always number exactly len(a:gridCol), and the spans must

    add up to that count. python-pptx exposes cell.is_merge_origin, cell.is_spanned, cell.span_width,

    cell.span_height, cell.merge(other) and cell.split().


    What to watch: (1) write text only to the merge origin - text written to a covered cell goes into

    a hidden cell and never displays; (2) never delete a row/column blindly - if you delete the row

    holding an origin while covered cells remain (or vice versa), the spans no longer add up and the

    file can be flagged for repair; (3) when inserting/deleting a column in the merged region, adjust

    the gridSpan value and add/remove the matching covered a:tc; (4) keep a:gridCol count equal to the

    number of a:tc per row; (5) cell.text returns only that cell''s text, not the merged neighbour''s,

    so read from the origin; (6) beware gridSpan values that no longer match after edits.

    '
- id: pptx-rowdel-01
  answer: "python-pptx has no row add/delete API: Table, table.rows and _Row provide no add, remove or\n\
    delete method. There is no table.rows.remove(). You must edit the XML: \n  tbl = table._tbl\n  tbl.remove(tbl.tr_lst[i])\n\
    where tbl.tr_lst is the list of a:tr children in order. (You can also get the row element via\ntable.rows[i]._tr\
    \ and call tbl.remove(...) or tr.getparent().remove(tr).) To add a row, deep-copy\nan existing a:tr\
    \ (copy.deepcopy) and place it with addnext/addprevious or tbl.insert(). Afterwards\nrecompute any\
    \ totals and, because the frame geometry does not follow XML edits, set\ngraphic_frame.height = sum(row.height\
    \ for row in table.rows). Verify by re-opening the saved file\nand confirming the removed text is\
    \ gone from every slide part.\n"
- id: pptx-rowdel-02
  answer: "Yes, it matters. Removing an a:tr only changes the table's row list; the drawing frame's size\n\
    lives separately in p:graphicFrame/p:xfrm/a:ext (cx/cy) and is not recalculated. The frame keeps\n\
    its old height, so its bounding box no longer matches the visible table: there is extra empty space\n\
    (or overlap), selection/hit-testing is off, and shapes or layout anchored to that frame can be\nmisplaced\
    \ or overflow. Some renderers also use a:ext for scaling.\n\nFix: after the XML edit, set the frame\
    \ height from the remaining rows:\n  gf = table_shape  # the GraphicFrame\n  gf.height = sum(row.height\
    \ for row in gf.table.rows)\n(The height setter updates a:ext/@cy; only the row.height setter normally\
    \ keeps it in sync.)\nAlso check that the new bottom (off.y + height) is within prs.slide_height and\
    \ does not collide\nwith shapes below. Rows whose h is unset (None) should be treated/handled explicitly.\n"
- id: pptx-rowdel-03
  answer: 'Because table "banding" can be done in two different ways. OOXML tables have no row-level fill

    attribute - cell shading lives on each a:tc/a:tcPr/a:solidFill. So a deck whose alternating look

    is hard-coded alternates the solidFill of every individual cell by row index. Delete a middle row

    and the pattern no longer lines up: the two rows that become adjacent were originally different

    parities, so they now show the same colour.


    In the other deck the alternation may come from the table style / a:tblPr bandRow="1" plus

    a:tableStyleId, where PowerPoint recomputes banding from the row''s position at render time. Delete

    a row there and the application re-bands automatically, so no two adjacent rows match. So the

    difference is hard-coded per-cell fills vs. style-driven banding (and sometimes firstRow/lastRow

    special formatting). Fix for the hard-coded case: re-apply the explicit fills to the rows after the

    deletion.

    '
- id: pptx-rowdel-04
  answer: 'Because the row is still in the file. A white rectangle is a separate floating shape: it does
    not

    move with the table when the table is edited, reflows, or is regenerated, and it can be occluded by

    other shapes or picked up by z-order changes. Setting the text white leaves the text present -

    still selectable, searchable, copyable, read by screen readers, and present in exports/other

    renderers or on a different theme/background. Setting the row height to near zero doesn''t remove
    it

    either: rows have minimum heights and don''t shrink below their content, and the text can overflow

    into the next row. Meanwhile any subtotal/total, row counts, charts or other slides that reference

    the row still count it, so the numbers and data stay wrong. The only correct fix is to remove the

    a:tr from a:tbl (and recompute totals and the frame height).

    '
- id: pptx-cell-01
  answer: "Formatting is lost. cell.text = \"SKU\" rebuilds the text body: it clears the existing paragraphs\n\
    and creates one new paragraph with a single run that has no rPr, so the bold and the 14 pt come from\n\
    the cell/placeholder defaults or table style instead - typically non-bold and a default size/colour\n\
    (often dark text, not white).\n\nTo keep it, edit the existing run rather than reassigning cell.text:\n\
    \  tf = cell.text_frame\n  p = tf.paragraphs[0]\n  p.runs[0].text = \"SKU\"        # keeps that run's\
    \ rPr (b, sz, solidFill, latin)\n  for r in p.runs[1:]:          # remove extra runs so old text doesn't\
    \ linger\n      r._r.getparent().remove(r._r)\nDo not use cell.text, which discards the run's rPr.\
    \ If the cell has no run, copy an rPr element\nfrom a well-formed sibling cell/run and reuse it.\n"
- id: pptx-cell-02
  answer: 'Cell text wraps by default (bodyPr wrap="square"), and column widths are fixed by a:tblGrid''s

    a:gridCol/@w. So a much longer name normally wraps onto multiple lines inside the same column

    width rather than widening the column; the row auto-grows taller. That can make the whole table

    taller, push it past prs.slide_height, and overlap shapes below; and if another column does

    auto-fit, the row lines can look ragged.


    Controls: explicitly set the column width via a:gridCol/@w (and keep the frame width a:ext/@cx in

    sync), set an explicit row height or text-frame autosize (a:bodyPr normAutofit / spAutoFit, or

    wrap="none" to avoid wrapping), reduce the font size, adjust cell margins (marL/marR), set vertical

    anchoring, or shorten/abbreviate the text. Because PowerPoint may still honour its own row autofit,

    treat explicit row heights as advisory and re-check the resulting frame height against the slide

    and the shapes below (moving them if needed).

    '
- id: pptx-cell-03
  answer: 'Anything derived from the quantity. The line amount = qty x unit price, so that row''s total
    changes;

    the table''s Subtotal/Total row must be recomputed; any VAT/tax, discount, or grand-total cells and

    any adjustments must be recalculated; the same figure may also appear elsewhere in the deck (a

    summary slide, a chart or embedded data, speaker notes, a text box). Also check layout effects: the

    wider number may change cell text width/wrap and therefore the row/table height, so re-verify the

    frame height and that the table still fits on the slide. In short: row amount, subtotal, tax,

    grand total, and every other place that mirrors the total.

    '
- id: pptx-relayout-01
  answer: "There is no insert-row API, so clone an existing body row's XML so the formatting is carried\
    \ over:\n  1. Locate the table by header content and find the \"Service G\" row and the Total row\
    \ by exact\n     stripped cell text.\n  2. template_tr = service_g_row._tr  (or any body row); new_tr\
    \ = copy.deepcopy(template_tr).\n  3. Insert it where you want: service_g_row._tr.addnext(new_tr)\
    \  (or total_row._tr.addprevious(new_tr)\n     or tbl.tr_lst.insert(idx, new_tr)). Keep the new_tr\
    \ reference; re-indexing table.rows is\n     unreliable until the XML settles.\n  4. Fill each cell\
    \ through its existing first run: cell.text_frame.paragraphs[0].runs[0].text = value,\n     removing\
    \ any extra runs; never cell.text (it drops rPr). Walk the a:tc children of new_tr rather\n     than\
    \ table.rows, since indices shifted.\n  5. Recompute the Total from the rows, including the new line.\n\
    \  6. Re-apply explicit banding fills to the new row if the table uses hard-coded alternation.\n \
    \ 7. Sync the frame: graphic_frame.height = sum(row.height for row in table.rows), then check the\n\
    \     frame bottom against prs.slide_height and any shapes below.\n"
- id: pptx-relayout-02
  answer: "Adding a column means changing both the grid definition and every row, keeping them consistent:\n\
    \  - a:tblGrid: add one a:gridCol after the Item column with a w (width in EMU).\n  - every a:tr:\
    \ add one a:tc in the same index position as the new gridCol; each new a:tc needs a\n    a:tcPr, an\
    \ a:txBody with a:bodyPr, and a paragraph/run/rPr/a:t matching the surrounding cells'\n    formatting\
    \ (bold header vs body). Afterwards len(a:tc) per row must equal len(a:gridCol) - a\n    mismatch\
    \ is a repair trigger.\n  - merges: any gridSpan that covers the insertion point (e.g. a Total label\
    \ spanning the first two\n    columns) must increase by 1, and the corresponding covered a:tc with\
    \ hMerge/vMerge must be added\n    or removed so the spans still sum to the column count.\n  - widths/geometry:\
    \ the visible width is the sum of gridCol w values, and the frame's a:ext/@cx\n    should match it.\
    \ If the table already spans the slide, adding a column overflows; either shrink\n    the other gridCol\
    \ widths so the sum fits inside (prs.slide_width - off.x), or move/resize the\n    frame (and re-check\
    \ off.x + cx <= prs.slide_width), or accept a narrower layout. Then set\n    graphic_frame.width accordingly.\n\
    \  - finally recompute the total/frame height, and verify the file re-opens cleanly (correct a:tc\n\
    \    counts, fresh shape ids, no dangling relationships).\n"
- id: pptx-tblins-01
  answer: 'Use `slide.shapes.add_table(rows, cols, left, top, width, height)` (rows=3, cols=2), which
    returns a GraphicFrame whose `.table` you then use. To pick a non-overlapping position, iterate the
    existing shapes and compute each one''s box from `.left`, `.top`, `.width`, `.height` (recurse into
    group shapes), then choose a free rectangle — for example place it below the lowest existing shape
    (`top = max(s.top + s.height) + gap`) or in another empty region — and clamp to `prs.slide_width`/`prs.slide_height`.
    Test candidate rectangles for intersection with the existing boxes before committing. If the layout
    provides a content placeholder, prefer putting the table there instead (see pptx-tblins-03).

    '
- id: pptx-tblins-02
  answer: 'The blue look is the default table style: an `a:tblStyle` element inside the table''s `a:tblPr`
    pointing at a built-in style, plus banding flags. python-pptx has no high-level "set style" API, so
    either (a) copy the formatting of an existing table — deep-copy its `tblPr` and/or apply explicit
    cell fills, borders and run fonts (`cell.fill`, `runs[0].font`, `tcPr` XML) to every cell; or (b)
    edit the XML to reference the same style GUID: find/replace or append `a:tblStyle` inside `table._tbl.tblPr`
    with the id used by the deck''s tables. You can also remove `a:tblStyle` and turn off the decorative
    flags (`table.first_row`, `table.horz_banding`, `table.band_row`, etc.) and format explicitly.

    '
- id: pptx-tblins-03
  answer: 'Get the placeholder from the slide (`slide.placeholders`, matched by `idx`/type). If it is
    a `TablePlaceholder`, call `placeholder.insert_table(rows, cols)` and use the returned frame''s `.table`
    — this places the table exactly at the placeholder''s geometry and consumes the placeholder. A generic
    "Title and Content" (`SlidePlaceholder`) has no `insert_table`; in that case add the table yourself
    at the placeholder''s geometry with `slide.shapes.add_table(rows, cols, ph.left, ph.top, ph.width,
    ph.height)` and remove the now-empty placeholder element (`ph._element.getparent().remove(ph._element)`)
    so its prompt text does not show.

    '
- id: pptx-imgrep-01
  answer: 'Do not edit the shared image bytes. Instead capture the old picture''s geometry (`left`, `top`,
    `width`, `height`) and its z-order position in the shape tree (`spTree = slide.shapes._spTree; idx
    = list(spTree).index(old_pic._element)`). Add the new file (`slide.shapes.add_picture(new_path, left,
    top, width=old.width, height=old.height)`), then move the new element to the old index (`spTree.remove(new._element);
    spTree.insert(idx, new._element)`), and finally remove the old `p:pic` element. This preserves size,
    position and stacking order (an equivalent trick is `old_pic._element.addprevious(new._element)` before
    deleting the old element). Ensure the new image is a distinct image part.

    '
- id: pptx-imgrep-02
  answer: 'Because python-pptx de-duplicates identical image binaries into a single shared `ImagePart`.
    When the slide-1 logo and the slide-7 logo have the same bytes, both pictures'' `a:blip` elements
    point at the same part via their `r:embed` relationship. Overwriting that part''s bytes changes every
    reference at once, so slide 7 changes too. Fix: add a new, distinct image part containing the new
    bytes and repoint only slide 1''s blip `r:embed` to it; only drop an old relationship when no other
    picture still uses it. In-place overwrite is safe only when that part is referenced by exactly one
    picture.

    '
- id: pptx-imgrep-03
  answer: '`slide.part.drop_rel(rId)` decides a relationship is unused by counting occurrences of the
    `r:id` attribute only. Pictures reference images via `a:blip/@r:embed` (linked images via `r:link`),
    not `r:id`, so the relationship appears to have zero references and python-pptx drops it — even though
    another `p:pic` on the slide still uses it through that rId — leaving the picture broken. Before dropping,
    scan the slide part XML for `//@r:embed` / `//@r:link` equal to the rId and only drop when none remain;
    otherwise replace the image part''s bytes instead of removing the relationship.

    '
- id: pptx-imgins-01
  answer: 'Pass only a width so python-pptx preserves the aspect ratio: `pic = slide.shapes.add_picture("signature.png",
    left, top, width=Inches(2))`; the returned `pic.height` is the scaled height (you can also read the
    native size with `pptx.util.Image` or PIL first). Place bottom-right from the slide size: `left =
    prs.slide_width - Inches(2) - margin`, `top = prs.slide_height - height - margin`, reading `prs.slide_width`/`prs.slide_height`
    rather than assuming a size. Check the resulting box against existing shapes (recurse into groups)
    and adjust if it overlaps, then verify by reading back `pic.left/top/width/height`.

    '
- id: pptx-imgins-02
  answer: '`insert_picture` scales the image to fill the placeholder and crops the overflow, rather than
    distorting it — it matches the picture to the placeholder''s aspect ratio (centered crop), so a portrait
    photo in a landscape placeholder loses the top and bottom (or, vice versa, the sides). The placeholder''s
    size is kept and the picture is cropped inside it. To show the whole image, re-proportion/resize the
    placeholder to the image''s aspect ratio, override the crop, or insert with `add_picture` at the size
    you want instead of using the placeholder.

    '
- id: pptx-legacy-01
  answer: 'No. python-pptx only reads/writes OOXML files (`.pptx`/`.pptm`/template variants); the binary
    OLE `.ppt` (PowerPoint 97–2003) format is not supported and cannot be opened. Workflow: keep the original
    untouched; convert `.ppt` to `.pptx` (e.g. `soffice --headless --convert-to pptx --outdir out deck.ppt`,
    ideally with a throwaway user profile), edit the `.pptx` with python-pptx, and if the user needs `.ppt`,
    convert the edited `.pptx` back with `soffice --headless --convert-to ppt`. Deliver the requested
    format plus the untouched original, and warn that the conversion round-trip can shift fonts and layout.

    '
- id: pptx-legacy-02
  answer: 'Before editing: confirm the conversion produced a file, open the `.pptx` and inspect it — slide
    size/aspect ratio, fonts, theme and layouts, images, and locate the target table by its content/header
    (not by index); keep copies of both the original `.ppt` and the converted `.pptx`. After editing:
    re-open the saved `.pptx` and verify by content (the intended change is present and any removed content
    is absent), check for repair triggers, and re-convert to `.ppt` if that is the required output. Tell
    the user explicitly that the deck was converted through LibreOffice (a different renderer), so formatting
    may differ from the original, state which format you are delivering, and note that the original was
    preserved.

    '
- id: pptx-verify-01
  answer: 'Work on a copy and keep the original. After the script writes the file: (1) validate the package
    — `unzip -t` and/or re-open with `Presentation(path)` so a parse/repair problem surfaces; (2) verify
    by content, not by API return values — unzip and grep `ppt/slides/*.xml` for the expected new text
    and for the absence of removed text; (3) check the slide `.rels` for dangling references (`r:embed`/`r:id`
    pointing at dropped rels); (4) optionally convert/render (e.g. LibreOffice to PDF) to eyeball it.
    Report success only when these checks pass; a clean `save()` proves nothing, because python-pptx will
    happily write invalid XML.

    '
- id: pptx-verify-02
  answer: 'Common repair triggers after XML edits: a table row with fewer `a:tc` cells than `a:gridCol`
    (or an add/remove of `a:tr` leaving the grid inconsistent); an `a:tc` without its required `a:txBody`;
    merge spans (`gridSpan`/`rowSpan`, `hMerge`/`vMerge`) that don''t add up or are written to the wrong
    cell; child elements in the wrong schema order or missing required elements; malformed/wrongly-namespaced
    XML (e.g. `mc:Ignorable` naming a prefix that `cleanup_namespaces` pruned); duplicate `p:cNvPr` ids
    after copying shapes; and dangling relationships — an `r:embed`/`r:id` pointing at a relationship
    that was dropped (for example an image rel removed while a picture still used it). Wrong content types
    can also trigger it.

    '
- id: pptx-verify-03
  answer: 'No — deleting the `p:pic` element does not remove the image from the `.pptx`. The media part
    under `ppt/media/` and its relationship are typically left orphaned, so the bytes are still embedded
    and recoverable. To truly remove it you must also drop the relationship (checking `@r:embed`/`@r:link`
    usages first so you don''t break another picture) and delete the media part. Verify by unzipping the
    saved file and listing/inspecting `ppt/media/` and the slide `.rels`/XML: if the image file is still
    present or still referenced, it is not gone. Note python-pptx does not garbage-collect unused parts
    on save, so simply re-saving will not strip it.

    '
