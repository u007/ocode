# step-5-preview-free — WITH-SKILL validation answers for pptx (opencode-go, 2026-10-12); closed-book,
# derived/pptx.step-5-preview-free.SKILL.md body prepended to _prompts/pptx.md as active guidance.
- id: pptx-model-01
  answer: |
    A table on a slide is not a "shape" of its own; it lives inside a graphic frame:
    `p:graphicFrame` (in the slide's `p:spTree`) → `p:nvGraphicFramePr`, `p:xfrm`
    (`a:off`/`a:ext` for left/top/cx/cy) → `a:graphic/a:graphicData`
    (uri = ".../drawingml/2006/table") → `a:tbl`.

    - **Table-level:** `a:tblPr` (flags like `bandRow`/`firstRow`, plus `a:tableStyleId`
      GUID) followed by `a:tblGrid`, then the `a:tr` rows.
    - **Columns:** `a:tblGrid`, which holds one `a:gridCol` per column, each with its
      `w:` width. The column count is `len(gridCols)`, always.
    - **Rows:** `a:tr`, each with an `h:` row height, containing cells in column order.
    - **Cells:** `a:tc`. Each `a:tc` contains `a:txBody` (`a:bodyPr`,
      `a:p` paragraphs with `a:r` runs, each run `a:rPr` + `a:t` text, `a:br`)
      optionally followed by `a:tcPr` (fills, `marL/marR/marT/marB`, anchor, borders),
      then `a:extLst`.
    - **Merges:** origin cell carries `gridSpan`/`rowSpan`; covered cells are separate
      empty `a:tc` with `hMerge`/`vMerge` — the `a:tc` count per row always equals the
      number of `a:gridCol`s.
- id: pptx-model-02
  answer: |
    Units are **EMU** (English Metric Units): 914400 EMU = 1 inch, 12700 EMU = 1 point
    (also 914400 = 2.54 cm exactly). Positions use `a:off` (`a:x`, `a:y`), sizes use
    `a:ext` (`a:cx`, `a:cy`); these must be non-negative integers — a float like
    `1371600.0` is schema-invalid and makes PowerPoint flag the file for repair.

    - **Default 16:9 deck:** 13.333 in × 7.5 in = **12192000 × 6858000 EMU**
      (`prs.slide_width`, `prs.slide_height`).
    - **Legacy default 4:3 deck:** 10 in × 7.5 in = 9144000 × 6858000 EMU.

    Never hard-code these; read `prs.slide_width` / `slide_height` from the presentation.
- id: pptx-model-03
  answer: |
    It is on the **slide layout or the slide master**, not on the slides themselves.
    `slide.shapes` iterates only that slide's own `p:spTree`; layout/master shapes are
    inherited and are reachable via `slide.slide_layout.shapes` (or
    `slide.slide_layout.slide_master.shapes`).

    Consequences: editing the logo on the layout/master changes it on **every slide
    that inherits from that layout** — you cannot make a change apply to one slide only
    by editing it there. For a per-slide change you must override it: copy the picture
    (or create a new one) into that slide's `spTree` with the intended image/crop. Also
    be careful with layout *placeholders*, since slides inherit those geometries too.
- id: pptx-locate-01
  answer: |
    Don't take the first `has_table` shape — a slide (or group) may hold several tables:

    1. Walk the slide's shapes, recursing into `GroupShape.shapes`, and collect every
       shape with `shape.has_table`.
    2. Identify the right table by its **header row**: compare
       `table.cell(0, c).text_frame.text.strip()` values against the expected headers,
       e.g. `["Item", "Qty", "Unit Price", "Amount"]`. Assert exactly one match.
    3. Find the row by matching the **key column** on exact stripped text:
       `cell.text_frame.text.strip() == "Gadget D"`. Collect all hits and assert
       `len(hits) == 1` (never `break` on the first hit).
    4. Remember merges: covered cells (`hMerge`) are empty and the text lives only in
       the origin, so match only on the column that actually holds the text.

    Note `slide.shapes` is 0-based; python-pptx has no "slide 3" index magic — get the
    slide from `prs.slides[2]`.
- id: pptx-locate-02
  answer: |
    The origin cell carries the span attribute — `gridSpan="2"` (horizontal) or
    `rowSpan="n"` (vertical) — exposed as `cell.is_merge_origin`, `cell.span_width`,
    `cell.span_height`. The next `a:tc` (per spanned column) is a **separate** cell
    element with `hMerge="1"` (or `vMerge`), and it is empty; `cell.is_spanned` is True.

    Key invariant: **every `a:tr` still has exactly one `a:tc` per `a:gridCol`** — a span
    does not reduce the cell count, so a 4-column grid with a `gridSpan="2"` Total label
    still has four `a:tc` in that row.

    When editing/deleting around a merge:
    - Write/read text only in the **origin** cell; never expect the label repeated in the
      covered cells.
    - Inserting a column inside a merge is two coordinated edits: bump the origin's
      `gridSpan` **and** insert a deep copy of a covered (`hMerge="1"`) cell.
    - Deleting/inserting a row inside a `rowSpan` likewise requires adjusting the span
      together with the covered `a:tc`s — never one or the other.
- id: pptx-rowdel-01
  answer: |
    No. python-pptx has **no `table.rows.remove()`** and no delete API — `_Rows` is a
    read-only sequence. You must delete the XML element:

    ```python
    tbl = table._tbl
    tr = tbl.tr_lst[idx]                 # or table.rows[idx]._tr
    tr.getparent().remove(tr)            # / tbl.remove(tr)
    ```

    That removes the `a:tr` from `a:tbl`. You then still have to fix the graphicFrame
    height (and possibly row shading) — the deletion itself is only the element removal.
- id: pptx-rowdel-02
  answer: |
    Yes, it matters. The frame's `p:xfrm/a:ext/@cy` was set when the table was created
    and python-pptx does **not** recompute it when you remove an `a:tr`, so the
    `p:graphicFrame` stays taller than the table it contains. The oversized frame affects
    selection/hit-testing, imports and any later geometry math, and PowerPoint won't
    necessarily shrink it for you.

    Fix: recompute from the surviving rows and write it back, e.g.
    `gf.height = sum(row.height for row in table.rows)` — then also verify
    `gf.top + gf.height <= prs.slide_height` (minus the deck's margin) and that the new
    bottom doesn't overlap shapes below the table; shrink row heights, move those shapes,
    or ask.
- id: pptx-rowdel-03
  answer: |
    Because the two decks band their rows differently:

    - **Explicit per-cell shading** — each body row's `a:tcPr/a:solidFill` was baked in
      (often by deep-copying template rows). Deleting a middle `a:tr` leaves the remaining
      rows' fills untouched, so two adjacent rows can end up with the same fill. You must
      recompute the fill of every surviving body row (e.g. shaded iff its body index is
      odd).
    - **Style-driven banding** — `a:tblPr` has `bandRow="1"` and an `a:tableStyleId` whose
      `band1H`/`band2H` alternates from the style definition. PowerPoint recomputes banding
      over the remaining rows when it renders/edits, so the pattern stays consistent with
      no per-cell fills to fix.

    So the symptom appears where shading was written into cells, not delegated to the table
    style.
- id: pptx-rowdel-04
  answer: |
    Because nothing is actually removed — the `a:tr` and its text stay in the file:

    - The data is still extractable: text extraction, outline view, accessibility tools,
      screen readers, find/replace, and copy-paste all still see it.
    - White-on-white or near-zero height is presentation-only. A theme/colour change, colour
      remap, or an unselected conthNormalPaste can reveal it again — it's obfuscation, not
      deletion (a real leak for confidential rows).
    - The table keeps phantom geometry, confusing editing and any later row/column math.
    - A floating rectangle on top is fragile: anything that moves, reorders, or is exported
      to another format exposes the row.

    The correct fix is to remove the `a:tr` element itself (plus recompute the frame height
    and any explicit row shading).
- id: pptx-cell-01
  answer: |
    `cell.text = "SKU"` **throws away the formatting**: python-pptx replaces the whole
    `a:txBody` content with a fresh single default run, so the bold, the 14 pt size and the
    white colour (which lived in the old run's `a:rPr`) are gone and the text falls back to
    the table-style defaults.

    To keep it, write the text into the existing run instead: take the first run, set
    `run.text = "SKU"`, and remove the extra runs — the original `a:rPr` is untouched. If
    you really must create a run, first `copy.deepcopy` the old run and change only its
    `a:t`. The same applies at cell level: preserve `a:rPr`/`a:tcPr`, replace only `a:t`.
- id: pptx-cell-02
  answer: |
    Column widths are fixed by the `a:tblGrid/a:gridCol/@w` values and the frame width by
    `a:ext/@cx`, so the table does **not** grow wider when the text gets longer. What
    actually happens: text wraps inside the cell and the **row grows taller** (rows hold a
    minimum `h:`) to fit the extra lines; if the row height is fixed, text can be clipped
    or overflow, and a table near the slide edge can push content out of the frame.

    To control it: deliberately edit `gridCol` widths (and keep Σ gridCol = frame width, or
    adjust `a:ext/@cx` within the margins), set `a:bodyPr` wrap/`normAutofit` behaviour on
    the cell, set `a:tcPr` margins (`marL`/`marR`), and if the table is near the slide edge
    re-check `gf.left + gf.width <= prs.slide_width`. Long text in a key column usually
    means rebalancing the other columns, not letting rows grow unbounded.
- id: pptx-cell-03
  answer: |
    Qty is a dependency, not just a number. After changing 5 → 12 you must update:

    1. **The line's amount** — `Qty × Unit Price` (12 × price) in the same row; if it is a
       static text value, recompute it (tables don't recalc).
    2. **The subtotal and the Total row** — recompute from the Amount column and assert the
       printed total equals the recomputed sum before saving, otherwise you silently ship a
       wrong invoice.
    3. **Every other occurrence of the same value elsewhere in the deck** — summary slides,
       other tables/rows referencing the same quantity, footnotes, alt text.
    4. Anything derived: discount thresholds, "n items" counts, or text that quotes the
       quantity.

    Then re-verify the frame height/slide bounds if any total text got longer.
- id: pptx-relayout-01
  answer: |
    Deep-copy an existing **body** row (not the Total row) so it inherits the `a:tcPr`
    fills, borders, fonts and margins, then splice it before the Total row:

    ```python
    total_tr = table._tbl.tr_lst[-1]
    total_tr.addprevious(copy.deepcopy(body_tr))
    ```

    Then edit the new row's runs' `a:t` text (keep each `a:rPr`), set its Qty/price/amount,
    update the Total row's text and value, recompute the printed total and assert it equals
    the sum of the Amount column. The edit is **not** finished there: set
    `gf.height = sum(row.height for row in table.rows)`, and check
    `gf.top + gf.height <= prs.slide_height` (minus the deck's margin) and that the new
    frame bottom doesn't overlap the shapes below it — shrink row heights, move those
    shapes, or ask.
- id: pptx-relayout-02
  answer: |
    XML changes required:

    1. **Columns:** add one `a:gridCol` to `a:tblGrid` (deep-copy a neighbouring column and
       set its `w:`).
    2. **Cells:** insert one `a:tc` into **every** `a:tr` at that index (deep-copy a
       neighbour cell to carry its `a:tcPr` formatting, then set its text).
    3. **Merges:** if the index lands inside a merged run, do *both*: bump the origin's
       `gridSpan` (`str(span + 1)`) **and** insert a deep copy of a covered cell
       (`hMerge="1"`) after the origin — in the same edit.

    Keeping it on the slide: don't grow the total — take the new width from an existing
    column (shrink Item) so `Σ gridCol` stays equal to the frame width, or otherwise adjust
    `a:ext/@cx`/`p:xfrm/a:off` so the frame stays inside the slide margins. The invariant to
    assert before saving is **`len(tr.tc_lst) == len(tblGrid.gridCol_lst)` for every row**.
- id: pptx-tblins-01
  answer: |
    Pick the position from the deck's own geometry, not a guess: enumerate the existing
    shapes' bounding boxes on the slide, find the empty band (e.g. below the lowest
    content, `max(top + height)`), keep the deck's margins (e.g. 0.5 in), and make sure
    `left + width <= prs.slide_width` and `top + height <= prs.slide_height`.

    Then create it with python-pptx:

    ```python
    gf = slide.shapes.add_table(rows, cols, left, top, width, height)
    table = gf.table
    ```

    Finally verify no overlap against every existing shape bbox, and set header text (gets
    `firstRow` emphasis if the style flags it).
- id: pptx-tblins-02
  answer: |
    `add_table` applies the **default** table style — the familiar blue banded look. To
    match the deck's existing tables, copy the style wiring from one of them:

    - Replace the new table's `a:tblPr` with a deep copy of an existing table's `a:tblPr`
      (its `firstRow`/`bandRow`/`firstCol` flags), which carries that deck's
      `a:tableStyleId` GUID.
    - Make sure the GUID actually exists in the presentation's `tableStyles.xml` part (it
      will if you copied a GUID from a table already in the same deck).
    - Match the rest of the look too: row heights, column widths, and the frame position,
      so it aligns with the surrounding content.

    Copying an existing `a:tbl` wholesale (then re-shaping it) is another reliable route.
- id: pptx-tblins-03
  answer: |
    Depends on the placeholder type:

    - If it is a real **table placeholder** (`TablePlaceholder`), use the idiomatic
      `placeholder.insert_table(rows, cols)` — it returns a `PlaceholderGraphicFrame` in
      the placeholder's own geometry, and the placeholder is consumed.
    - If it is a generic **content placeholder** (`SlidePlaceholder`, which has no
      `insert_table`), then `slide.shapes.add_table(rows, cols, ph.left, ph.top, ph.width,
      ph.height)` at the placeholder's bbox, and then remove the now-empty placeholder:
      `ph._element.getparent().remove(ph._element)` — so no stray "Click to add text" box
      remains on the slide.
- id: pptx-imgrep-01
  answer: |
    There is no `picture.replace()`, so swap the element:

    1. Read the old picture's geometry: `pic.left/top/width/height` (compute height from
       the old pic so the aspect matches the frame you're keeping).
    2. Note its z-order index in the slide's `spTree`.
    3. `new = slide.shapes.add_picture(path, left, top, width, height)` (the new picture is
       appended on top).
    4. Restore stacking order: move the new `p:pic` element to the old picture's index in
       `spTree` (remove it, then `spTree.insert(old_idx, new._element)`).
    5. Remove the old `p:pic` element, then **drop the old image relationship** only if
       nothing else in the part still references it — scan `//@r:embed | //@r:link` for the
       old rId yourself before calling `slide.part.drop_rel(old_rId)`.
- id: pptx-imgrep-02
  answer: |
    Because both pictures reference the **same image part**. python-pptx (and PowerPoint)
    de-duplicates identical images into one `ppt/media/imageN.png`; the `p:pic` elements on
    slides 1 and 7 are just relationships pointing at that single part. Overwriting the
    part's bytes therefore changes every reference to it — slide 7 included.

    To change only slide 1: insert the new image so it becomes its **own** image part, then
    repoint just slide 1's `a:blip/@r:embed` to the new rId, leaving slide 7's relationship
    untouched.
- id: pptx-imgrep-03
  answer: |
    `drop_rel`'s reference counting is the problem: `_rel_ref_count` scans the part for
    `//@r:id` only. A picture references its image with `a:blip/@r:embed` — not `r:id` — so
    the image rel always counted **0** users, and `drop_rel` removed it even though a second
    picture on the same slide still embeds that image via `r:embed`.

    Guard: before calling `drop_rel`, check the part yourself for any remaining
    `//@r:embed | //@r:link` uses of the old rId. `drop_rel` (or a `r:id`-based count) is
    not a safe gate for image rels.
- id: pptx-imgins-01
  answer: |
    Compute the size from the image's native pixel dimensions (aspect preserved), then
    place it in the bottom-right corner after checking for collisions:

    ```python
    w = Emu(2 * 914400)                      # 2 inches
    h = Emu(round(int(w) * px_h / px_w))     # same aspect ratio
    left  = prs.slide_width  - right_margin  - w
    top   = prs.slide_height - bottom_margin - h
    pic = slide.shapes.add_picture(path, left, top, width=w, height=h)
    ```

    Verify `left >= 0`, `top >= 0`, `left + w <= prs.slide_width`,
    `top + h <= prs.slide_height`, and that the bbox does not intersect any existing shape
    (footers, tables, logos) — if it does, shrink or reposition rather than covering content.
- id: pptx-imgins-02
  answer: |
    `insert_picture` crops the image to the placeholder's aspect ratio via `a:srcRect` — a
    landscape placeholder receiving a portrait photo loses its top and bottom so the
    picture fills the frame. Hence "part of the photo disappeared".

    Fixes: resize the picture (or the placeholder) to the photo's ratio before/when
    inserting; or use `slide.shapes.add_picture(path, ...)` and remove the placeholder
    element; or edit the inserted picture's `a:srcRect` crop values so the whole photo
    shows (letterboxing instead of cropping).
- id: pptx-legacy-01
  answer: |
    No. python-pptx only opens **OOXML** packages (`.pptx`, `.potx`, …). A `.ppt`
    (PowerPoint 97–2003 binary) cannot be opened by it.

    Workflow: convert first — `soffice --headless --convert-to pptx deck.ppt`
    (LibreOffice) or PowerPoint/Keynote automation — then edit the resulting `.pptx` with
    python-pptx. Always keep the original `.ppt` untouched as a backup, and warn that
    conversion is lossy.
- id: pptx-legacy-02
  answer: |
    Before editing: back up the original `.ppt`, and sanity-check the conversion — open the
    `.pptx` with python-pptx (slide count, text of each slide, table cell counts, image
    count) and, ideally, render it (e.g. LibreOffice to PDF) to confirm nothing is broken.

    After editing, re-verify the same things plus: table structure survived (cells per row,
    merged cells exact), fonts may have substituted, animations/effects/embedded objects may
    be dropped, layout may have shifted, no stray empty "Click to add text" placeholders, no
    dangling relationships, the file reopens cleanly and renders to PDF.

    You must tell the user: the deck was **converted**, not edited in place; the result is
    not pixel/styling identical to the original and some PowerPoint features may not
    survive LibreOffice conversion; the original `.ppt` is preserved as backup and should be
    visually checked.
- id: pptx-verify-01
  answer: |
    Safe procedure:

    1. **Never overwrite the only copy** — back up the original, or save the edited deck to
       a new path.
    2. Save with python-pptx, then **reopen the saved file** with python-pptx as an
       independent parse.
    3. Assert structure/content on the reopened file: row/cell counts per table, header
       text, the edited cell text, totals matching a recomputed sum, `Σ gridCol` vs frame
       width, `len(tr.tc_lst) == len(gridCol_lst)` per row, frame bounds inside the slide.
    4. Check package integrity: `unzip -t`, list `ppt/media/`, confirm no dangling
       `r:embed`/`r:id` in the rels, and that removed media is gone (hash compare).
    5. Open it in LibreOffice headless (convert to PDF) as a "does PowerPoint say repair?"
       smoke test, and diff the rendered output against expectations.
    6. Only after all checks pass, replace the original (and tell the user what changed).
- id: pptx-verify-02
  answer: |
    Common causes of "needs to be repaired":

    1. **`a:tc` count ≠ `a:gridCol` count** in some `a:tr` — a column insert that missed a
       row, or a merge where the origin's `gridSpan` was bumped without inserting the
       matching covered `hMerge="1"` cell (or vice versa).
    2. **Relationship damage** — deleting a `p:pic` but leaving the rel, or
       `drop_rel`-ing an rId that a `r:embed` still uses; a part added without its
       content-type; duplicate rIds.
    3. **Invalid numeric values** — non-integer or negative EMU in `a:off`/`a:ext`
       (e.g. `cx="1371600.0"`), negative `a:tr/@h`, `w:` on `a:gridCol`.
    4. **Schema order violations** — e.g. `a:tcPr` must follow `a:txBody`; wrong child order
       in `a:tbl` (`a:tblGrid` before `a:tr`, `a:tr` before `a:tblGrid`), `a:tblPr` position.
    5. **Bad span attributes** — `gridSpan`/`rowSpan` of 0 or out of range, a
       `hMerge`/`vMerge` cell that carries text.
    6. **XML/namespace breakage** — unescaped `&`/`<`, undefined prefixes used in an
       attribute, `mc:Ignorable` pointing at a prefix the root no longer declares (e.g.
       after `cleanup_namespaces()` pruned a "unused" declaration).
- id: pptx-verify-03
  answer: |
    **No.** Removing the `p:pic` element only detaches it from the slide's XML tree. The
    slide part's relationship to the image and the media part itself are still there:
    `ppt/media/imageN.png` is still inside the `.pptx` and the image bytes are still
    fully extractable — so confidential data still ships.

    How to check: unzip the saved file, list `ppt/media/`, and hash those files against the
    confidential image's hash; also grep the slide's `.rels` for the image rId and the XML
    for any remaining `r:embed` referencing it.

    To truly remove it: delete the `p:pic`, verify no other `//@r:embed | //@r:link` in the
    part uses that rId, call `slide.part.drop_rel(old_rId)`, re-save, and then confirm by
    hash that the file is absent from the new package's media listing.
