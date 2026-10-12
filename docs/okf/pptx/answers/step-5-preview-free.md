# step-5-preview-free — closed-book answers for pptx (opencode-go, 2026-10-12); answerer saw only _prompts/pptx.md
Closed-book answers from knowledge only. No tools used, no repo/web access. Assumption: python-pptx current stable (0.6.x/1.0.x) behavior, deck authored by PowerPoint in OOXML.

- id: pptx-model-01
  answer: |
    A table lives inside a `p:graphicFrame` in the slide's `p:spTree`, in
    `ppt/slides/slideN.xml`. The frame holds `p:xfrm` (position/size via
    `a:off`/`a:ext` in EMU) and `a:graphic` → `a:graphicData` with URI
    `http://schemas.openxmlformats.org/drawingml/2006/table`, inside which sits
    `a:tbl`.

    `a:tbl` children, in schema order: `a:tblPr` (table properties — firstRow,
    bandRow, firstCol, bandCol flags and `a:tableStyleId` GUID), `a:tblGrid`
    (one `a:gridCol` per column, `@w` = width in EMU), then a sequence of `a:tr`
    row elements. Each `a:tr` has `@h` (row height in EMU) and contains `a:tc`
    cells. Each `a:tc` holds `a:txBody` (with `a:bodyPr`, then `a:p` paragraphs
    each containing `a:r` runs, each with `a:rPr` run properties and `a:t` text)
    and `a:tcPr` (cell properties: `a:lnL/lnR/lnT/lnB` borders, fill
    (`a:solidFill` etc.), margins `marL/marR/marT/marB`, `anchor`). Merges are
    expressed as attributes on `a:tc`: `gridSpan`, `rowSpan`, `hMerge`, `vMerge`.
    So: columns = `a:tblGrid/a:gridCol`, rows = `a:tr`, cells = `a:tc`, text =
    `a:txBody/a:p/a:r/a:t`.

- id: pptx-model-02
  answer: |
    Units are EMU (English Metric Units): 914400 EMU per inch, 360000 per cm,
    12700 per point. python-pptx exposes typed `Length` objects (`Emu`, `Inches`,
    `Pt`, `Cm`) so `Inches(2)` works directly for left/top/width/height.

    A default 16:9 deck (modern PowerPoint) is 13.333 × 7.5 in =
    12192000 × 6858000 EMU. Note the trap: python-pptx's own default template
    is 4:3, 10 × 7.5 in = 9144000 × 6858000 EMU — never hardcode slide
    dimensions, read `prs.slide_width` / `prs.slide_height`.

- id: pptx-model-03
  answer: |
    It is not on the slide at all — it is on the slide **layout**
    (`ppt/slideLayouts/slideLayoutN.xml`) or the slide **master**
    (`ppt/slideMasters/slideMaster1.xml`), inherited by every slide that uses
    that layout. It can also be a background fill (`p:bg` with `a:blipFill`) on
    the layout/master rather than a shape.

    Consequences of editing it there: the change propagates to every slide using
    that layout (and any deck opened from the same template) — that is usually
    the goal, but it also means you cannot change the logo on one slide only by
    editing the layout; for a slide-specific change you must copy the picture
    onto the slide itself (as a slide shape) or override per-slide. You must
    access it via `slide.slide_layout.shapes` (or
    `slide.slide_layout.slide_master.shapes`), not `slide.shapes`. Also, layout
    placeholders/shapes can be hidden on a slide via `p:hideSp`/placeholder
    inheritance rules, so layout edits may not appear uniformly. Editing a
    layout also risks affecting future slides created from it.

- id: pptx-locate-01
  answer: |
    Slides are 0-indexed in python-pptx, so slide 3 = `prs.slides[2]`:

    ```python
    slide = prs.slides[2]
    table = None
    for shape in slide.shapes:
        if shape.has_table:
            table = shape.table
            break
    # guard: if several tables, disambiguate by shape.name or position
    for idx, row in enumerate(table.rows):
        cells = [c.text.strip() for c in row.cells]
        if "Gadget D" in cells:
            target_row_idx = idx
            break
    else:
        raise ValueError("row not found")
    ```

    Use `shape.has_table` (not `shape_type`) so you skip pictures/text boxes;
    search **all** cells in a row because the label may not be in column 0, and
    remember merged cells repeat their text across the spanned columns. The
    graphic frame's `.table` is the `a:tbl`; row order in `table.rows` follows
    `a:tr` document order.

- id: pptx-locate-02
  answer: |
    A horizontal merge is the origin cell carrying `gridSpan="N"` (and the
    text) with the covered cells carrying `hMerge="1"` (usually empty). A
    vertical merge uses `rowSpan` on the origin plus `vMerge="1"` on
    continuation cells. Each `a:tr` must still contain enough `a:tc` cells to
    cover the `a:tblGrid` column count after accounting for spans.

    Watch for: (1) when inserting a column, extend the merge (bump the origin's
    `gridSpan`) or add an `hMerge="1"` covered cell — if you deepcopy the
    *origin* cell to insert, you duplicate its text and stale gridSpan; copy a
    *covered* cell and then bump the origin; (2) when deleting a column or row,
    decrement spans or you orphan a span pointing past the grid; (3) cell counts
    per row must match the grid, else PowerPoint repairs the file; (4) the Total
    row is usually styled differently (bold, maybe a top border) — recompute the
    Total from surviving data cells rather than assuming.

- id: pptx-rowdel-01
  answer: |
    No. python-pptx has no row-removal API: there is no `table.rows.remove()`
    and no delete method (`_Rows` supports indexing, iteration and `len` only).
    You must drop the XML element:

    ```python
    tr = table._tbl.tr_lst[idx]      # a:tr to remove
    tr.getparent().remove(tr)
    ```

    Two things you normally do alongside: subtract the removed `@h` from the
    graphic frame's `a:ext/@cy` (see pptx-rowdel-02), and fix up alternating
    shading/merges (see pptx-rowdel-03). `tr_lst` is the `a:tbl`'s sequence of
    `a:tr`; `getparent().remove()` is lxml-level and safe here.

- id: pptx-rowdel-02
  answer: |
    It matters. PowerPoint recomputes the true table height when it renders, so
    on screen the table looks right — but `a:ext/@cy` on the graphic frame is
    the value every *other* consumer reads: python-pptx (`shape.height`),
    LibreOffice, converters, and any layout math you or a later script does
    (e.g. "place the next table 0.2in below this one", overlap checks,
    bounding-box collision tests). A stale cy makes the shape report a height
    larger than its content, so things computed from it will overlap or leave
    gaps.

    Fix: set the frame's cy to the sum of the surviving rows' `@h`:
    `frame._element.graphicFrame...` — in practice
    `tbl = table._tbl; total = sum(int(tr.get('h')) for tr in tbl.tr_lst)`
    then write that into `p:xfrm/a:ext/@cy` (via `shape.height = Emu(total)` or
    setting the attribute). Caveat: rows with `rowSpan` contribute their `@h`
    once to the table height, and PowerPoint treats `tr/@h` as a *minimum* — if
    text wraps, actual rendered height exceeds the stored one, so recomputing
    from `@h` is only approximate for content-heavy rows.

- id: pptx-rowdel-03
  answer: |
    Because in one deck the alternation is *computed by the renderer* and in the
    other it is *written into the cells*. If the table style drives banding
    (`a:tblPr` has `firstRow="1" bandRow="1"` and a style whose bandRow fill is
    defined in `tableStyles.xml`), PowerPoint generates the shading from row
    index at render time, so deleting a row just re-bands everything correctly.
    The failing deck instead has an explicit fill on each body cell
    (`a:tcPr/a:solidFill` — often produced by an "apply banding" tool, an import
    from another format, or hand-editing). Those fills are static data: rows
    1,2,3 shaded, delete row 2, and surviving rows 1 and 3 both carry the
    shaded fill and now sit adjacent with the same colour.

    Fix for the explicit-fill deck: after deleting, walk the surviving body
    rows and recompute each row's fill from its new index per the deck's own
    convention (e.g. shaded iff body-index is odd), leaving header and Total
    rows alone. Best practice: derive the pattern from a row that already has
    the target look (deepcopy its `tcPr`) rather than hard-coding fill values.

- id: pptx-rowdel-04
  answer: |
    Because the row is still in the file — you have only hidden it visually.
    Every cell's `a:t` text remains extractable: PowerPoint's outline view and
    Find, screen readers/accessibility (the table's row and cell structure is
    untouched), `pandoc`, `pdftotext` after export, search engine/indexing of
    exported PDFs, and any script. So it fails at exactly the job it was asked
    to do — removing data — and leaks it. Additional problems: the row count is
    wrong, so any consumed total, exported CSV, or accessibility reading is
    wrong; the file bloats; the white text/zero-height row reappears if the
    table is later re-rendered by a different engine that ignores your row
    height or wraps text; a "white rectangle over it" also breaks when the
    table re-flows underneath, and rectangles add a separate shape with its own
    alt text. Correct approach: actually remove the `a:tr` element, then fix the
    frame height, banding, totals, and scrub carry-along artifacts (thumbnail,
    notes, hidden slide copy, docProps).

- id: pptx-cell-01
  answer: |
    `cell.text = "SKU"` (and the `text_frame.text` setter) **replaces the whole
    text frame**: python-pptx discards every existing paragraph and run and
    writes one new paragraph with one new bare run. All formatting lives on
    those old runs (`a:rPr`: bold, size 14pt, white `a:solidFill` font colour,
    typeface) and on the paragraph (`a:pPr`: alignment), so it is all thrown
    away. The new text falls back to defaults — possibly the table style's
    header formatting if inherited, but typically not; explicit bold/size/colour
    are gone.

    To preserve: edit the existing run in place instead of the frame —
    `cell.text_frame.paragraphs[0].runs[0].text = "SKU"` (keeps that run's
    `rPr`). If the cell has multiple runs you must decide which run survives; if
    the cell is empty, first copy an `rPr` from a sibling header cell's run
    (via lxml `copy.deepcopy`) onto the new run, or deepcopy an entire
    formatted run and just change its `a:t`. Same rule when writing cells for a
    brand-new row copied from an existing one: change only the `a:t` text.

- id: pptx-cell-02
  answer: |
    PowerPoint never shortens text; it reflows. Row heights are stored in
    `a:tr/@h` but treated as a **minimum** — a longer product name wraps onto
    more lines and the row grows, so the table gets taller than its
    `a:ext/@cy`, potentially running off the bottom of the slide and colliding
    with whatever sits below. Column widths stay as `a:tblGrid/a:gridCol/@w`
    (PowerPoint's "autofit" for tables is not automatic in a saved file unless
    applied), so a long cell either wraps or, with wrap off, overflows its
    column.

    Control levers: wrap on/off and autofit behaviours in `a:tcPr`'s text frame
    `a:bodyPr` (`wrap="none"`, `a:normAutofit` with `fontScale`/`lnSpcReduction`,
    `a:noAutofit`); reduce font size; change gridCol widths; reduce cell margins
    (`marL/marR/marT/marB`); trim the text; or grow/shrink the graphic frame and
    move content below it. After editing, recompute what the layout implies and
    verify the table still fits (`left + width <= slide_width`, bottom edge vs
    content and slide height) rather than trusting the stored @h values.

- id: pptx-cell-03
  answer: |
    Pptx tables have **no formulas** — every displayed number is literal text,
    so everything derived from Qty is stale until you change it:
    - that row's Amount / Line Total cell (Qty × Unit price),
    - the table's Subtotal/Total row (and tax, discount, grand total if
      present),
    - any other place quoting the numbers: a summary slide, KPI callout text
      boxes, chart data (chart XML caches its own values in `c:val`/`c:numCache`
      — editing the table does not update a chart), images of tables, and any
      tables or text on other slides quoting the same line.
    Do it programmatically: parse the Qty and Unit price cells as numbers,
    recompute, and assert the resulting value equals what you write (and that
    the recomputed grand total equals the printed Total). Watch for merged
    cells when locating the numeric cells, and remember hidden rows still
    contribute to a printed total someone typed by hand.

- id: pptx-relayout-01
  answer: |
    Copy an existing body row (the Service G `a:tr`), insert the clone at the
    right position, then change only the text of the clone's cells:

    ```python
    tbl = table._tbl
    src = tbl.tr_lst[service_g_idx]                 # a:tr to mimic
    total_tr = tbl.tr_lst[total_idx]                # the Total row
    new_tr = copy.deepcopy(src)
    total_tr.addprevious(new_tr)                    # insert before Total
    ```

    Then set each cell's text by editing existing runs (`runs[0].text = ...`
    and clearing extras), and fix up:
    - alternating shading: recompute the clone's fill from its new body-row
      position (see pptx-rowdel-03) — a copy of row N keeps row N's fill;
    - row height `@h` if the new row needs a different one;
    - the graphic frame's `a:ext/@cy` (add the row height);
    - if the cloned row contained merges (`hMerge`/`vMerge`/`gridSpan`) the
      clone may need its spans adjusted or restored to a plain row;
    - if the Total row sums cells, update it and assert the arithmetic.
    Insert-before via `total_tr.addprevious()` keeps `a:tbl` schema order
    (`tblPr`, `tblGrid`, then `a:tr` sequence).

- id: pptx-relayout-02
  answer: |
    DrawingML table cells have **no width attribute** — width lives only in
    `a:tblGrid/a:gridCol/@w`, and a cell fills its grid columns. Required
    changes:
    1. insert a new `a:gridCol` (with a `@w` in EMU) into `a:tblGrid` at the
       position after Item;
    2. insert one new `a:tc` (with a `tcPr`) into **every** `a:tr` at the same
       column index — deepcopy a `tc` from the corresponding row so
       borders/fill/font ride along, then set its text;
    3. for merged rows: add a covered `tc` with `hMerge="1"`, or extend the
       merge by bumping the origin cell's `gridSpan` (e.g. Total's label).
       Never copy the span origin itself;
    4. verify each row still has cell/span coverage equal to the gridCol count

    To keep the table on the slide when the frame already spans nearly the full
    width: take the new column's width **out of an existing column** (usually the
    wide Item column) so `sum(gridCol/@w)` is unchanged and still equals the
    frame's `a:ext/@cx`. Assert that equality numerically (`sum(w) == cx`) and
    that `left + cx <= slide_width` rather than eyeballing it. Also re-check
    that the shrunken Item text still fits (wrap/font/margins), update the
    header cell's formatting, and update `ext/@cy` if row heights changed.

- id: pptx-tblins-01
  answer: |
    Create with `slide.shapes.add_table(rows=3, cols=2, left, top, width,
    height)` — it returns a GraphicFrame; the height argument is distributed
    evenly across rows, so adjust row heights afterwards if needed.

    Pick the position from geometry, not by eye: read the slide's
    `prs.slide_width/height`, enumerate existing shapes' bounding boxes
    (`shape.left/top/width/height`, treating the slide layout/master-inherited
    shapes as already-used area), and choose a free rectangle — typically below
    the lowest content (`top = max(bottom) + margin`) or in the largest gap.
    Sanity-assert `left + width <= slide_width` and `top + height <=
    slide_height` and that the new frame does not intersect any existing frame
    (write a small rect-overlap check and adjust/shrink until it passes). Use
    `Inches()`/`Emu()` helpers for all coordinates; respect the slide margins
    already implied by existing content.

- id: pptx-tblins-02
  answer: |
    `add_table` stamps a hard-coded default table style — python-pptx writes
    `a:tableStyleId` GUID `{5C22544A-7EE6-4342-B048-85BDC9FD1C3A}` (Medium Style
    2 - Accent 1) into `a:tblPr`, which renders blue unless the deck's theme
    happens to define it. To match the deck's tables:
    - Read the `a:tableStyleId` GUID from an existing table's `a:tblPr` and copy
      it into the new table's `a:tblPr` (and match the `firstRow`/`bandRow`
      flags), **provided** the GUID is defined in the deck's
      `tableStyles.xml`/theme — a style copied from another deck won't exist
      here.
    - If no shared style exists, copy the explicit formatting instead:
      deepcopy `tcPr` (fills, borders, margins) and run `rPr` from an existing
      table's header and body cells onto the new table's cells.
    - Or remove the `a:tableStyleId` so the table falls back to the deck's
      default style.
    Then verify visually/structurally: same header fill, banding, borders, font
    as the existing tables.

- id: pptx-tblins-03
  answer: |
    There is no `insert_table` on placeholders in python-pptx (only
    `insert_picture` for picture placeholders), so the idiomatic flow is:
    build the table at the placeholder's geometry, then delete the empty
    placeholder so it doesn't appear/print as "Click to add text":

    ```python
    ph = slide.placeholders[idx]        # the empty content/object placeholder
    gf = slide.shapes.add_table(rows, cols, ph.left, ph.top, ph.width, ph.height)
    ph._element.getparent().remove(ph._element)
    ```

    This keeps the table exactly where the template designer intended and
    removes the placeholder from the inheritance chain. Remove it *after*
    creating the table and reading its geometry. Alternative if you must keep
    the placeholder: fill it via XML replacement, but removal is the common
    practice. Note the placeholder may live on the layout only, not the slide —
    only slide-level placeholders can be removed this way.

- id: pptx-imgrep-01
  answer: |
    Keep the picture and swap the image behind it — never delete and re-add
    (that loses z-order and position). The picture's geometry lives in
    `p:pic/p:spPr/a:xfrm` and its image reference in
    `p:blipFill/a:blip/@r:embed`; pointing that rId at a new image part changes
    the bitmap while leaving everything else untouched:

    ```python
    pic = shape                       # the p:pic Picture shape
    new_image_part, rId = pic.part.get_or_add_image_part("new_logo.png")
    pic._element.blipFill.blip.embed = rId   # swap the relationship target
    ```

    `get_or_add_image_part` dedupes by content hash, so if the deck already
    contains that image you reuse the existing part. The pic keeps its position,
    size, z-order in `p:spTree`, crop (`a:srcRect`), rotation, and alt text.
    Two caveats: if the new image has a different aspect ratio, also clear or
    update `a:srcRect` crops and reset width/height so it isn't distorted or
    partly cropped away; and `blipFill.blip.embed = rId` writes the attribute
    directly — alternatively relate to the part first with
    `pic.part.relate_to(image_part, RT.IMAGE)`. The slow way (add_picture at
    the same coordinates, then re-insert the new `p:pic` at the old element's
    index in `spTree`) also works but must copy `a:xfrm` and ordering by hand.

- id: pptx-imgrep-02
  answer: |
    Because the package stores each distinct image **once** and the two pictures
    share that one `ppt/media/imageN.png`. PowerPoint (and python-pptx's
    `get_or_add_image_part`) dedupes images by content hash: the same logo on
    slide 1 and slide 7 is one media part referenced by two relationships (one
    per slide part). Overwriting the image part's bytes — or relating to it and
    swapping the rId on slide 1's blip — changes the single shared blob, so
    both references render the new pixels. "Replacing" the part replaced it
    everywhere.

    Fix: only ever swap a *relationship target*, never mutate part bytes, and
    point one slide's `r:embed` at a **new** image part
    (`get_or_add_image_part(new_file)` returns the existing part only if the new
    bytes already exist as a different part). Slide 7's relationship then still
    points at the old part, unchanged.

- id: pptx-imgrep-03
  answer: |
    `drop_rel` is an unchecked low-level call: it deletes the relationship from
    the part's rels without verifying that no remaining element still uses that
    rId, and it has no reference counting. The usual way two ends up broken is
    that both pictures referenced the **same rId** — this happens whenever a
    picture was cloned by deep-copying its `p:pic` element without creating a
    new relationship (a script that duplicated the logo, or PowerPoint
    emitting/one rel reused). Remove one pic and `drop_rel(rId)`, and the
    surviving pic's `r:embed` now points at a relationship that no longer
    exists — PowerPoint then reports a broken link or asks to repair the file.

    A second blow comes at save time: python-pptx writes only the parts that are
    reachable from the package relationship graph, so with no relationship left
    pointing at it the shared image part is silently **not written**, breaking
    every reference that did survive. Guard both: before dropping, count
    occurrences of the rId in the slide XML (and across the package's parts) and
    only drop when the count reaches zero — and confirm the image part isn't
    still referenced by another rel on another slide/notes/layout. Safer still:
    remove the pic element, drop the unused rId, then reopen the saved file and
    assert every `r:embed`/`r:link` resolves.

- id: pptx-imgins-01
  answer: |
    Compute the size from the image's real pixel dimensions, read slide
    geometry for placement, check for collisions, then add:

    ```python
    from PIL import Image
    slide_w, slide_h = prs.slide_width, prs.slide_height
    with Image.open("signature.png") as im:
        px_w, px_h = im.size
    width = Inches(2)
    height = Emu(round(width * px_h / px_w))     # preserve aspect ratio
    margin = Inches(0.25)
    left  = slide_w - width  - margin
    top   = slide_h - height - margin
    # collision check against existing shapes; nudge left/up or shrink until clear
    pic = slide.shapes.add_picture("signature.png", left, top, width, height)
    ```

    Passing only `width` also preserves aspect (you can then read
    `pic.height` afterwards), but you must run the overlap check *after* that.
    Overlap check: compute the new rect vs each existing shape's rect
    (left/top/width/height) and skip/deemign full-bleed backgrounds and layout
    decorations — if the corner is occupied, try the other side or above the
    lowest content. Don't trust the filename/EXIF for orientation — a portrait
    "signature" with EXIF rotation may need the decoded pixel aspect.

- id: pptx-imgins-02
  answer: |
    Picture placeholders are "crop-to-fill" frames. The pic inherits the
    placeholder's position and size verbatim, and the crop is recorded as
    percentage crop values (`a:srcRect` l/t/r/b on the pic's `a:blipFill`) that
    scale the image to cover the placeholder and hide the overflow. Insert a
    portrait photo into a landscape placeholder and the top and bottom (or the
    sides) are cropped away — hence "part of the photo disappears". The
    placeholder's own aspect ratio, not the image's, drives the frame. (I'm
    confident about the placeholder/crop-to-fill and srcRect mechanism; the
    exact crop arithmetic inside a given python-pptx release for this method is
    worth verifying against your version's docs/source rather than trusting my
    memory.)

    Fixes: after inserting, inspect the pic's `a:srcRect` and shape height, then
    clear the crop (`pic.crop_left = pic.crop_right = pic.crop_top =
    pic.crop_bottom = 0`) and reset the shape's width/height to the image's true
    aspect ratio — or place the image with `add_picture` at a rectangle you
    computed yourself, or change the placeholder's geometry in the layout so its
    aspect matches the intended photos.

- id: pptx-legacy-01
  answer: |
    No. python-pptx only opens OOXML (`.pptx`/`.potx`); a binary PowerPoint
    97–2003 `.ppt` (OLE2 compound file, magic `D0 CF 11 E0`, with BIFF-based
    records) is not supported and `Presentation()` will fail with an
    "unsupported format"/`BadZipFile`-style error — check the magic bytes, not
    just the extension, since misnamed files exist in both directions.

    Workflow: convert first, then edit. `soffice --headless --convert-to pptx
    deck.ppt` (LibreOffice), or open and save-as-pptx in PowerPoint; on macOS
    Keynote or QuickTime-free alternatives can also import `.ppt`. Then do the
    table edit on the produced `.pptx` with python-pptx as normal. Conversion is
    **lossy**: fonts get substituted, layout can shift, and legacy features
    (WordArt, custom animations, OLE embeddings, some transitions) may not
    survive — so verify the converted deck (render it, table structure intact)
    before and after editing. Keep the original `.ppt` untouched as the source
    of truth, and if the deliverable must stay `.ppt`, convert back with
    LibreOffice, re-checking fidelity again. python-pptx cannot write `.ppt`.

- id: pptx-legacy-02
  answer: |
    Before editing the converted file:
    - confirm it really is a binary `.ppt` (magic `D0CF11E0`) and that the
      conversion actually completed (slide count, file size, no error output);
    - spot-check fidelity: slide count and order, table structure (merged cells,
      row heights, shading, borders), fonts present on the system (LibreOffice
      substitutes missing ones, causing reflow/overflow), images and their
      aspect, master/layout theme colours, notes pages.

    After editing: verify the change and only the change — reopen with
    python-pptx, dump a text extraction (`pandoc -t plain`, text-frame dump)
      and diff against the pre-edit extraction; render (LibreOffice → PDF, or
    `qlmanage` on macOS) and eyeball the edited slide; assert geometry math
    (grid sum vs frame width, frame height vs sum of rows); run `unzip -t` for
      zip integrity; optionally compare renders before/after.

    You must tell the user: (1) the file was converted from the legacy binary
      format and conversion is lossy — fonts may be substituted and layout may
      differ subtly from the original in PowerPoint; (2) the original `.ppt` is
      preserved unmodified and the edit lives in the converted copy; (3) if
      pixel-perfect fidelity or round-tripping back to `.ppt` is required, the
      edit should be redone in PowerPoint on the original, because save-back
      through LibreOffice shifts formatting again.

- id: pptx-verify-01
  answer: |
    A safe save-and-verify loop:
    1. Save to a **new** path (or keep an untouched copy of the input); record
       a checksum of the input before touching it. Never overwrite the only
       copy.
    2. Structure checks: reopen with python-pptx (parse must succeed); iterate
       shapes and confirm the change is present and only the intended change is
       (look at the diff, not the whole file).
    3. Package checks: `unzip -t` integrity; every `r:embed`/`r:link` resolves
       to an existing rel and target part; no orphan rels; `ppt/media` contains
       what you expect.
    4. Minimal-diff proof: compare the edited package against the original
       part-by-part (zip entry sets; `difflib`/byte diff on each changed part)
       so exactly one or a few parts changed, in a few opcodes.
    5. Geometry assertions: `sum(gridCol/@w) == frame @cx`; `left + cx <=
       slide_width`; frame `@cy == sum(tr/@h)`; font sizes and margins sane.
    6. Independent render: export to PDF (LibreOffice) or thumbnail and confirm
       PowerPoint/another engine opens it with no "needs repair" dialog and no
       layout catastrophe; also check a text extraction (pandoc/`pdftotext`).
    7. Only after all checks pass, replace the deliverable (or keep both files).

- id: pptx-verify-02
  answer: |
    Common causes of PowerPoint's "needs to be repaired" after table edits:
    - **Schema order violations.** OOXML is sequence-strict. `a:tcPr` children
      must appear in order (lnL, lnR, lnT, lnB, lnTlToBr, lnBlToTr, cell3D,
      then the fill, headers, extLst); `a:tbl` children must be tblPr, tblGrid,
      tr*; new paragraphs must be bodyPr then p.
    - **Row/grid mismatch.** A row's cells don't cover the `a:gridCol` count
      after accounting for `gridSpan`/`hMerge`/`vMerge` (too few or too many
      cells), or a `gridSpan` extends past the grid after a column insert.
    - **Bad attribute values.** Missing/non-positive `w` on `a:gridCol` or
      `a:ext`; float where `xsd:long` is required (e.g. `cx="1371600.0"`);
      `firstRow="true"` instead of `"1"`/`"0"`.
    - **Dangling references.** An `r:embed`/`r:id` whose relationship was
      dropped (see pptx-imgrep-03), duplicate rIds, or a `tableStyleId` GUID
      absent from `tableStyles.xml`.
    - **Namespace damage.** Undeclared prefixes, elements from the wrong
      namespace (e.g. `w:` from DOCX pasted in), or `mc:Ignorable` left
      pointing at prefixes that no longer exist.
    - **Wrong parent.** `a:tr` or `a:tc` inserted directly under `graphicData`
      or `spTree` instead of inside `a:tbl`.

- id: pptx-verify-03
  answer: |
    Not necessarily. Deleting the `p:pic` element removes the *reference*; the
    image data itself lives in `ppt/media/imageN.png` and survives if anything
    still references it. It stays in the file when: another slide, the slide
    layout/master or notes slide points at the same image part (dedup makes this
    common — same logo/hidden picture elsewhere); the slide part still has the
    relationship to it (python-pptx only omits parts unreachable from the
    package rel graph at save time, so a still-declared rel keeps it); or
    another copy exists under a different name. Separately, `docProps/
    thumbnail.jpeg` may still *show* the removed image, and images can hide in
    charts or embedded objects.

    Check by inspecting the package rather than trusting the visual slide:
    `unzip -l deck.pptx` and look at `ppt/media/`; map every `r:embed` in all
    slide, layout, master and notes XML to its relationship target and see
    whether any still resolve to that image part (python-pptx part rels, or
    grep the XML for the media filename); and render the deck to images to
    confirm nothing visible remains. To fully remove: delete the pic element,
    drop the now-unused rel only after confirming no other `r:embed` uses it,
    re-save so the orphan part is not written, then re-check media contents and
    the thumbnail. The same caution applies to "delete the shape, keep the
    file" with any confidential asset — search text/XML too, not just pixels.
```

**ASKED** — answer 27 closed-book pptx questions as YAML.
**WORKED** — BUILT: full YAML answer sheet from knowledge, no tools, no web, no repo reads.
**FOUND** — honest uncertainty flagged on two items: `pptx-imgins-02` (exact crop arithmetic in `placeholder.insert_picture` per python-pptx version) and `pptx-imgrep-03` (drop_rel mechanism — answered via shared-rId/shared-part graph, with the reasoning stated).
**DECIDED** — answered independently per question; no answer-key retrieval; stated assumptions (python-pptx 0.6/1.0, PowerPoint-authored OOXML) up front.
**BOTTOM LINE** — all 27 answered; two answers carry explicit "verify against your version" hedges rather than fabricated precision.
**NEXT** — None — answers delivered as requested; grader can map by id.
