# deepseek-v4.1-flash — pptx answers (re-benchmark after provider update)

Model: `opencode-go/deepseek-v4.1-flash` (provider-stripped `deepseek-v4.1-flash`),
re-run 2026-10-21 after opencode-go updated the model behind this id
(`model_version: "4.1-2026-10-21"`). This REPLACES the earlier baseline for this id;
the previous answers are in git history.

Closed-book barrier:
- Each of the 27 questions in `docs/okf/_prompts/pptx.md` was a fresh headless
  `ocode run -m opencode-go/deepseek-v4.1-flash -effort med` session (`bin/ocode`, built
  2026-10-20) under ocode's real agent system prompt.
- Fake HOME: no global CLAUDE.md/AGENTS.md, no memory, no persisted roots. Its only config
  denies every builtin tool and DISABLES the five existing `*-tuning-deepseek-v4.1-flash`
  Kaizen skills (plus `conduct-tuning-deepseek-v4-flash`), so this is a true no-skill
  baseline. Probe before the sweep confirmed: no tools advertised, no Model-Specific
  Directives block. The generic `[ocode:provider]` DeepSeek output-discipline block and the
  bundled skill-catalogue digest were still present (every user gets them).
- cwd: an empty dir named `work`; stdin `/dev/null`.
- Audit: every session sqlite checked after the sweep — 1 user + 1 assistant message,
  zero tool calls.
- Prompt per question: closed-book instruction + the one question; plain-text reply,
  wrapped into YAML below verbatim.

```yaml
- id: pptx-model-01
  answer: |
    ## Where a table lives

    A `.pptx` is an OPC/ZIP package. The table itself is **not a separate part** — unlike a chart or an embedded workbook. It is inline XML inside the slide part:

    ```
    ppt/slides/slide1.xml
    ```

    (discovered through `ppt/presentation.xml` → `p:sldIdLst` → `ppt/slides/_rels/slide1.xml.rels`, and declared in `[Content_Types].xml` as `application/vnd.openxmlformats-officedocument.presentationml.slide+xml`).

    Inside the slide, shapes live in the shape tree:

    ```
    p:sld
    └─ p:cSld
       └─ p:spTree
          ├─ p:sp          (text box, autoshape)
          ├─ p:pic         (picture)
          ├─ p:graphicFrame  ← a table is one of these
          └─ p:grpSp
    ```

    A table is wrapped in a **`p:graphicFrame`**, not a `p:sp`. The frame carries position/size (`p:xfrm` → `a:off`, `a:ext`, in EMU) and an `a:graphic` whose `a:graphicData/@uri` identifies the payload:

    ```xml
    <p:graphicFrame>
      <p:nvGraphicFramePr>
        <p:cNvPr id="5" name="Table 4"/>
        <p:cNvGraphicFramePr><a:graphicFrameLocks noGrp="1"/></p:cNvGraphicFramePr>
        <p:nvPr/>
      </p:nvGraphicFramePr>
      <p:xfrm>
        <a:off x="1524000" y="1524000"/>
        <a:ext cx="4064000" cy="1143000"/>
      </p:xfrm>
      <a:graphic>
        <a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/table">
          <a:tbl> ... </a:tbl>
        </a:graphicData>
      </a:graphic>
    </p:graphicFrame>
    ```

    The `uri="…/drawingml/2006/table"` is the discriminator. Charts use `…/chart`, SmartArt uses `…/diagram`, OLE objects use `…/ole`.

    Namespace shorthand: `a` = `http://schemas.openxmlformats.org/drawingml/2006/main`, `p` = `http://schemas.openxmlformats.org/presentationml/2006/main`.

    ## The `a:tbl` element tree

    ```xml
    <a:tbl>
      <a:tblPr firstRow="1" bandRow="1">
        <a:tableStyleId>{5C22544A-7EE6-4342-B048-85BDC9FD1C3A}</a:tableStyleId>
      </a:tblPr>

      <a:tblGrid>
        <a:gridCol w="2032000"/>
        <a:gridCol w="2032000"/>
      </a:tblGrid>

      <a:tr h="370840">
        <a:tc>
          <a:txBody>
            <a:bodyPr/>
            <a:lstStyle/>
            <a:p>
              <a:r><a:rPr lang="en-US"/><a:t>Region</a:t></a:r>
            </a:p>
          </a:txBody>
          <a:tcPr marL="91440" marR="91440" marT="45720" marB="45720" anchor="ctr"/>
        </a:tc>
        <a:tc>
          <a:txBody>
            <a:bodyPr/><a:lstStyle/>
            <a:p><a:r><a:t>Revenue</a:t></a:r></a:p>
          </a:txBody>
          <a:tcPr anchor="ctr"/>
        </a:tc>
      </a:tr>

      <a:tr h="370840">
        <a:tc> ... </a:tc>
        <a:tc> ... </a:tc>
      </a:tr>
    </a:tbl>
    ```

    ### Columns — `a:tblGrid` / `a:gridCol`

    There is **no per-column wrapper element**. Columns exist only as a grid declaration:

    - `a:tblGrid` holds exactly one `a:gridCol` per column.
    - `a:gridCol/@w` is the column width in **EMU** (914400 EMU = 1 inch).
    - Column count = number of `a:gridCol` children. It must equal the number of `a:tc` in each `a:tr` (counting `gridSpan`).

    Column formatting (fill, borders) is not stored on the column — it is applied per cell in `a:tcPr`, even when it is identical across the column.

    ### Rows — `a:tr`

    - One `a:tr` per row, in visual order.
    - `a:tr/@h` is the row height in EMU, and it behaves as a **minimum**: PowerPoint grows the row if the cell text needs more space, so the stored value may not match the rendered height.

    ### Cells — `a:tc`

    - One `a:tc` per grid column, in left-to-right order within its `a:tr`.
    - Each cell has exactly two children, in this order:
      - `a:txBody` — the text content (required)
      - `a:tcPr` — cell properties (optional)
    - Merges are encoded here:
      - origin cell: `gridSpan="2"` (horizontal) and/or `rowSpan="2"` (vertical)
      - covered cells: `hMerge="1"` and/or `vMerge="1"`, with an empty `a:txBody`
      - the covered cells are still physically present in the grid, so naive row/cell iteration over-counts unless you honour the merge attributes.

    `a:tcPr` attributes: `marL`, `marR`, `marT`, `marB` (insets in EMU; defaults 91440 / 45720), `anchor` (`t|ctr|b`), `anchorCtr`, `horzOverflow`, `vertOverflow`, `rtl`.

    `a:tcPr` children: `a:lnL`, `a:lnR`, `a:lnT`, `a:lnB` (each a line with `w`, `cap`, `cmpd`, `algn` plus fill/dash), `a:lnTlToBr` / `a:lnBlToTr` (diagonals), `a:cell3D`, and a fill (`a:noFill` / `a:solidFill` / `a:gradFill` / `a:blipFill` / `a:pattFill` / `a:grpFill`).

    ### Text — `a:txBody`

    A cell's text uses the same DrawingML text model as any shape, and it is **text only** — a cell cannot contain a picture, a shape, or a nested table.

    ```
    a:txBody
    ├─ a:bodyPr        (required, first)
    ├─ a:lstStyle      (optional)
    └─ a:p  (1..n)     paragraphs
       ├─ a:pPr         (optional: algn, marL, indent, lvl, buNone/buChar/buAutoNum, defRPr)
       ├─ a:r            run: a:rPr + a:t
       ├─ a:br           line break
       ├─ a:fld          field (e.g. slidenum): a:rPr + a:pPr? + a:t
       └─ a:endParaRPr   (optional, final)
    ```

    - The actual characters are in **`a:t`**, always a child of `a:r` or `a:fld`.
    - `a:rPr` carries `lang`, `sz` (hundredths of a point), `b`, `i`, `u`, `strike`, `baseline`, and a fill (`a:solidFill`).
    - Each `a:p` is one paragraph; a soft line break inside a paragraph is `a:br`, not a new `a:p`.
    - `a:t` preserves leading/trailing spaces literally — there is no XML whitespace collapsing to worry about, but empty runs and empty paragraphs should be treated as blank lines.

    ## Table-level properties and styling

    `a:tblPr` (first child of `a:tbl`) has the flags:

    ```
    firstRow  firstCol  lastRow  lastCol  bandRow  bandCol  rtl
    ```

    and optionally `a:tableStyleId` — a GUID referencing an entry in `ppt/tableStyles.xml` (the presentation-level table style collection, itself listed from `presentation.xml` via `p:tableStyleIdLst`). Those flags tell the renderer which parts of the style to apply (header emphasis, banding, totals row). The style, not the cell XML, usually supplies the visible fill and borders — so a cell with no `a:tcPr` fill is not necessarily transparent.

    Default built-in style GUID commonly seen: `{5C22544A-7EE6-4342-B048-85BDC9FD1C3A}` (Medium Style 2 – Accent 1).

    ## Practical read recipe

    1. Unzip; resolve slide part from `p:sldIdLst` + rels.
    2. Parse XML; select `//p:graphicFrame/a:graphic/a:graphicData[@uri="…/drawingml/2006/table"]/a:tbl` (safer than `//a:tbl`, though `a:tbl` only appears in this position in practice).
    3. Columns: `a:tblGrid/a:gridCol/@w`.
    4. Rows: `a:tr`; height `@h`.
    5. Cells: `a:tc`; skip/dereference `hMerge`/`vMerge`, expand `gridSpan`/`rowSpan`.
    6. Cell text: concatenate `a:t` within `a:p` (joining runs, translating `a:br` to `\n`), and join paragraphs with `\n`.

    Library mapping, for reference: python-pptx exposes this as `shape.has_table` → `shape.table` (`Table`) with `.columns`/`.rows` (`_Column.width`, `_Row.height`) and `cell.text` / `cell.text_frame.paragraphs`; Apache POI uses `XSLFTable`, `XSLFTableRow`, `XSLFTableCell`. Both mirror the same XML: grid → columns, `a:tr` → rows, `a:tc` → cells, `a:txBody` → text frame.
- id: pptx-model-02
  answer: |
    **Short answer:** DrawingML stores positions and sizes as **EMUs (English Metric Units)** — a 64-bit integer unit defined so that 1 inch = 914,400 EMU, 1 point = 12,700 EMU, 1 cm = 360,000 EMU. python-pptx wraps these in its `Length` class (an `int` subclass, with `Emu`, `Inches`, `Pt`, `Cm`, `Mm` constructors and reciprocal properties), so `left`, `top`, `width`, `height` are all `Length` values whose raw value is EMU.

    **Default 16:9 deck:** 13.333 in × 7.5 in, i.e. **12,192,000 × 6,858,000 EMU** (33.87 cm × 19.05 cm, 960 × 540 pt).

    **Detail worth adding:** python-pptx's *default* template (`Presentation()` with no argument) is actually 4:3 — 10 in × 7.5 in = 9,144,000 × 6,858,000 EMU. You get 16:9 by setting `prs.slide_width = Inches(13.333)` and `prs.slide_height = Inches(7.5)`. PowerPoint's own 16:9 default is the 12,192,000 × 6,858,000 EMU figure.

    **Unit caveats:**
    - Font sizes in the XML are **centipoints** (`sz="1800"` = 18 pt); python-pptx's `font.size` is a `Pt`/`Length` value.
    - Some attributes (e.g. certain line/outline widths) are EMU too, but a few DrawingML fields use different scales — percentages as thousandths of a percent (opacity `50%` = `50000`) — so don't assume EMU universally.

    **Assumption:** I read "default 16:9 deck" as the standard PowerPoint widescreen size, and noted the python-pptx template discrepancy since it's a common gotcha.

    ---

    ASKED — DrawingML/python-pptx position/size units, plus default 16:9 slide dimensions.

    WORKED — BUILT nothing; answered from knowledge: EMU (914,400/in, 12,700/pt, 360,000/cm), 16:9 = 12,192,000 × 6,858,000 EMU (13.333 × 7.5 in).

    FOUND — python-pptx's stock template is 4:3 (9,144,000 × 6,858,000 EMU), so 16:9 must be set explicitly; font sizes are centipoints, not EMU.

    DECIDED — Gave both the 16:9 figure and the template caveat rather than a single number, since the two disagree.
    BOTTOM LINE: EMU everywhere for geometry; 16:9 is 12,192,000 × 6,858,000 EMU, but vanilla python-pptx hands you a 4:3 deck.

    NEXT — None — question was knowledge-only; no verification possible or permitted.
- id: pptx-model-03
  answer: |
    The logo is not in the slide part at all. It lives in an inherited part, almost always the **slide master** (`ppt/slideMasters/slideMaster1.xml`) or a **slide layout** (`ppt/slideLayouts/slideLayoutN.xml`), in that part's own `<p:spTree>` (or as a background/blipFill on the master/layout, or inside a group shape on the layout). `slide.shapes` only walks the `<p:spTree>` of the slide part itself — python-pptx does not merge inherited shapes into a slide's collection. So the logo renders on every slide purely through OOXML inheritance: master → layouts → slides.

    To reach it you have to go through the parts:

    ```python
    master = prs.slide_master
    layout = slide.slide_layout
    for shp in master.shapes: ...
    for shp in layout.shapes: ...
    for shp in slide.slide_layout.slide_master.shapes: ...
    ```

    (and recurse into `shape.shapes` for groups; check `slide.background` / `master.background` if it's a background picture rather than a shape).

    Consequences of editing it there:

    - **Global blast radius.** One edit to the master changes the logo on every layout and every slide that doesn't override it; one edit to a layout changes every slide using that layout. There is no per-slide edit and no per-slide undo — you've changed the template for the whole deck.
    - **You can't tune it per slide.** A slide using the layout cannot delete or restyle an inherited layout/master shape. The only per-slide controls are `showMasterSp="0"` on the slide (a "hide background graphics" switch that hides *all* master shapes, not just the logo) and covering it with a slide-level shape, which leaves the logo in the file.
    - **Z-order matters.** Inherited master/layout shapes render behind the slide's own shapes, so a slide-level filled rectangle can mask it, but that's a visual patch, not a real removal.
    - **Loop hazards.** If you iterate slides and "fix the logo" each time via the shared master/layout, you'll hit the same shape repeatedly; if you delete it on the first pass, later lookups against the regenerated collection fail.
    - **Portability.** A slide copied into another presentation inherits *that* deck's master/layout, so the logo silently changes or vanishes. Conversely, editing a layout in a .pptx template propagates to every deck built from it.
    - **Shape IDs are per-part.** Master/layout shape IDs live in a separate namespace from slide shape IDs, so ID-based lookups from `slide.shapes` will never match the inherited logo even if the numbers coincide.
    - **Not everything is a shape.** If the logo is a background fill on the master/layout (`p:bg`/`a:blipFill`) or inside a group, even master/layout `.shapes` won't list it directly — you need the background API or to recurse into groups.
- id: pptx-locate-01
  answer: |
    ## Short answer

    python-pptx has no `slide.tables` collection and no text search. You iterate the shapes on the slide, keep the one where `shape.has_table` is true, then walk `table.rows` and compare cell text.

    ```python
    from pptx import Presentation

    prs = Presentation("deck.pptx")
    slide = prs.slides[2]                 # slide 3 -> index 2 (0-based)

    table = next(s.table for s in slide.shapes if getattr(s, "has_table", False))

    target = "Gadget D"
    row = next(
        (r for r in table.rows
         if any(c.text.strip() == target for c in r.cells)),
        None,
    )
    ```

    Then `row.cells[col].text` for the values, or `row.cells[0].text` if the label is known to be column 0.

    ## Details that matter

    **Finding the table**

    - `prs.slides` is 0-based, so "slide 3" is `prs.slides[2]`. If the deck has hidden slides or you mean the 3rd *visible* slide, filter with `slide._element.get("show") == "0"`-style checks first.
    - A table is a `GraphicFrame`, not a `Shape`. `GraphicFrame.has_table` is `True`; other shapes return `False` (the base class defines `has_table = False`). `shape.shape_type == MSO_SHAPE_TYPE.TABLE` also works, but `has_table` is the idiomatic test.
    - `shape.table` raises `ValueError` on a non-table frame, so guard with `has_table` (or `getattr(..., "has_table", False)` if you want to be defensive across versions).
    - If tables can be inside grouped shapes, recurse:

    ```python
    from pptx.enum.shapes import MSO_SHAPE_TYPE

    def walk(shapes):
        for s in shapes:
            if s.shape_type == MSO_SHAPE_TYPE.GROUP:
                yield from walk(s.shapes)
            else:
                yield s
    ```

    **Finding the row**

    - `table.rows` is a `_RowCollection`: supports `len()`, iteration, and `table.rows[i]`. `table.cell(r, c)` is the shortcut for `table.rows[r].cells[c]`.
    - Each `_Cell.text` returns the concatenated text of all paragraphs/runs, with `\v`/`\n` for line breaks — so `.strip()` before comparing, and consider `casefold()` if the deck's casing is inconsistent.
    - Searching **all** cells in the row (`any(... for c in r.cells)`) is safer than hardcoding `r.cells[0]`: it tolerates a label that isn't the first column, avoids mismatches when the first row is a merged header, and skips the "header row" ambiguity.
    - With merged cells, spanned (non-origin) cells return empty text, so the match lands on the merge origin — still correct.
    - Rows are 0-based, and the header is typically row 0, which is why matching on text beats `table.rows[3]`.

    **Why the one-liner can bite**

    `next(...)` raises `StopIteration` if there is no table / no matching row. In real code use `next(..., None)` (as above) or a `for`/`break` and handle `None` explicitly — the table may be a picture of a table, or a placeholder, in which case nothing will be found and you'll silently get an empty result.

    ## Assumptions

    Slide 3 means the third slide in `prs.slides` (index 2), and "the table" means the first table on that slide; if a slide can hold several, loop over all `has_table` shapes instead of taking the first.
- id: pptx-locate-02
  answer: |
    **Representation.** There is no "merge" element. A merge is encoded on the individual `<a:tc>` cells via attributes in the `a` namespace:

    ```xml
    <a:tr>                                  <!-- Total row -->
      <a:tc gridSpan="2">                   <!-- merge ORIGIN: owns the content -->
        <a:txBody>…<a:t>Total</a:t>…</a:txBody>
        <a:tcPr/>
      </a:tc>
      <a:tc hMerge="1">                     <!-- COVERED cell: spanned-over, empty -->
        <a:txBody><a:bodyPr/><a:p/></a:txBody>
        <a:tcPr/>
      </a:tc>
      <a:tc>…value…</a:tc>
    </a:tr>
    ```

    - Horizontal merge: origin has `gridSpan="2"`, the cell(s) it covers have `hMerge="1"`.
    - Vertical merge: origin has `rowSpan="N"`, covered cells below have `vMerge="1"`.
    - A rectangular merge combines both on the origin (`rowSpan` + `gridSpan`), with covered cells carrying the matching `hMerge`/`vMerge` flags.
    - Defaults are `rowSpan="1"`, `gridSpan="1"`, `hMerge="0"`, `vMerge="0"`, so unmerged cells carry no attributes.
    - Some generators also write `gridSpan` on covered cells; the `hMerge`/`vMerge` flags plus the origin's span are what actually matter, so trust the origin and rebuild.

    **Key invariant:** the grid still has every column. `<a:tblGrid>` keeps both `<a:gridCol>` entries, and each `<a:tr>` must still contain exactly one `<a:tc>` per grid column — merges change cell *width*, not cell *count*. That is the single biggest source of corruption.

    **What to watch when editing/deleting around the Total label:**

    1. **Content lives only in the origin.** The "second" cell is a placeholder with an (essentially) empty `txBody`. Editing/reading cell index 1 gets nothing; write text to the origin cell (index 0) only. Libraries that expose merged cells may return the origin for spanned indices, or may refuse writes — check before assuming.
    2. **Never delete the covered `<a:tc>` on its own.** To unmerge, set `gridSpan="1"` on the origin and remove `hMerge="1"` from the survivor; the `<a:tc>` element count must stay equal to the `gridCol` count. Deleting rows/columns means removing the matching `tc` from *every* row *and* the matching `gridCol`, then clamping any span that crossed it.
    3. **Never let a span exceed the grid** (`gridSpan` > remaining columns, or `rowSpan` past the last row). That yields PowerPoint's "repair" prompt rather than an error.
    4. **Vertical spans are the fragile case.** Inserting a row inside a `rowSpan` requires incrementing `rowSpan` *and* inserting a `vMerge="1"` placeholder cell in the new row; deleting such a row requires decrementing it. A `vMerge` cell with no origin above it is invalid.
    5. **Formatting is per-cell, not per-merge.** Fill/borders for the visible merged block are taken from the origin's `tcPr`; the covered cell keeps its own `tcPr`, which is usually empty. If you later unmerge, or if a consumer renders cell-by-cell, you get default/no formatting — copy or re-apply the origin's `tcPr` to covered cells before unmerging, and be aware some renderers (LibreOffice, previewers) differ on how span fills/borders are painted.
    6. **Column indices don't shift.** The grid position of the numeric total cells is unchanged by the label merge, so anything that "skips" the spanned cell or counts visible cells will be off by one on the value columns. Locate the value by grid index (or by header text) rather than by counting rendered cells.
    7. **Keep the empty `txBody`.** The schema requires a `txBody` (with `bodyPr` and at least one `a:p`) even in a covered cell; stripping it out is another repair-trigger.
- id: pptx-rowdel-01
  answer: |
    **Short answer: no.** python-pptx (through 1.0.x) has no supported row-delete API. There is no `table.rows.remove()`, no `del table.rows[i]`, and no `table.delete_row()` / `add_row()`. `table.rows` is a read-only sequence wrapper (`_Rows`) over the `<a:tr>` elements, and it only implements indexing, iteration, and `len()`.

    Rows (and columns) must be deleted by manipulating the underlying DrawingML XML. Every row is an `<a:tr>` child of the table's `<a:tbl>` element, and python-pptx exposes that element as `table._tbl` (an lxml element):

    ```python
    def delete_row(table, idx):
        tbl = table._tbl
        tr = table.rows[idx]._tr      # underlying <a:tr> element
        tbl.remove(tr)                # lxml: remove child from parent


    # usage
    delete_row(table, 2)              # removes the third row
    ```

    `tr.getparent().remove(tr)` is equivalent. This is the standard workaround used in python-pptx issue threads.

    Practical caveats:

    - **Indices shift** after deletion, so work from the highest index down if deleting several rows. Build a static list first if iterating (`list(table.rows)`), otherwise you can skip rows.
    - **Column structure is untouched.** Removing a row removes its `<a:tc>` cells along with the `<a:tr>`; `<a:tblGrid>` is unaffected because the column count is unchanged.
    - **Stale references.** Any `_Cell` objects you already fetched from that row become detached; don't reuse them.
    - **Table height.** PowerPoint normally recomputes the drawn height from the remaining `<a:tr h="...">` values, but the containing graphic frame's `a:ext cy` can be left at the old value. If layout matters, also update the frame height (or read the row heights and set the graphic frame height to their sum).
    - **Don't delete the last row.** Leave at least one row; if you want the table gone entirely, remove the whole graphic frame (`graphic_frame._element.getparent().remove(graphic_frame._element)`) rather than producing a zero-row `<a:tbl>`.
    - **Columns are harder.** Removing a column means deleting the matching `<a:gridCol>` from `<a:tblGrid>` *and* the corresponding `<a:tc>` at the same position in every `<a:tr>`, so it's easy to corrupt the table if you get the index wrong.

    Assumption: this refers to ordinary tables in a `GraphicFrame` (`<a:tbl>`), which is the only table type python-pptx supports; the answer applies to 0.6.x–1.0.x. I'm not aware of a newer release adding a row/column removal API, so if one exists now, that's the only part of this answer that could be outdated.
- id: pptx-rowdel-02
  answer: |
    Yes, it matters — but the nuance is worth stating precisely.

    **Why it matters**

    PowerPoint lays out table rows from the `a:tr/@h` values, so the table itself will often still *look* right with a stale frame height. The stale `cy` is nevertheless wrong data, and it causes real problems:

    - **Selection/hit-testing:** the graphicFrame's `a:ext` defines the object's bounding box. An oversized or undersized `cy` gives you a selection rectangle that doesn't match the table, and clickable empty space (or clipped click area) around it.
    - **Other consumers:** LibreOffice, Google Slides, and libraries that trust the frame ext may scale the table to fit the frame, distorting row heights or leaving a gap.
    - **Resize/scale operations:** PowerPoint uses the frame ext when you resize or scale the shape.
    - **Round-tripping/validation:** the file is internally inconsistent; PowerPoint may silently repair it on save, but you can't rely on that, and any pipeline that reads the stored size gets a wrong answer.

    **How to fix it**

    After deleting the `a:tr`, recompute the sum of the remaining row heights and write it to the `a:ext` inside the graphicFrame's `p:xfrm`:

    ```python
    from lxml import etree

    ns = {
        'a': 'http://schemas.openxmlformats.org/drawingml/2006/main',
        'p': 'http://schemas.openxmlformats.org/presentationml/2006/main',
    }

    tbl = graphic_frame.find('.//a:tbl', ns)
    cy = sum(int(tr.get('h')) for tr in tbl.findall('a:tr', ns))

    ext = graphic_frame.find('./p:xfrm/a:ext', ns)
    ext.set('cy', str(cy))
    ```

    Equivalent raw XML:

    ```xml
    <p:xfrm>
      <a:off x="..." y="..."/>
      <a:ext cx="..." cy="2286000"/>  <!-- sum of a:tr/@h -->
    </p:xfrm>
    ```

    **Notes / caveats**

    - Also keep `cx` in sync with `sum(a:gridCol/@w)` whenever you add or remove columns; a deleted column needs the `a:gridCol` removed *and* the corresponding `a:tc` removed from every row.
    - Subtracting the removed row's `h` from the old `cy` is fine only if no other row heights changed. Summing the current rows is safer.
    - `a:tr/@h` is required and includes cell margins, so the sum is the correct stored table height.
    - PowerPoint enforces a minimum rendered row height from font size and margins. If a stored `h` is below that minimum, the actual drawn height exceeds the sum; the best-effort fix is still the sum, but exact frame matching may require normalizing those `h` values too.
    - `p:xfrm` is technically optional in the schema; if it's absent you must synthesize it with `a:off` and `a:ext`.
    - A table must retain at least one `a:tr` (with at least one `a:tc`) and at least one `a:gridCol` — don't delete the last row/column.
    - If the graphicFrame is inside a group shape, also fix the group's `p:grpSpPr/a:xfrm/a:ext` and child offset, since group bounds are cached the same way.
- id: pptx-rowdel-03
  answer: |
    ## Short answer

    Because row "shading" in a PPTX table isn't stored as a property of the table. It's produced one of two ways, and only one of them re-computes after a row is deleted.

    ## The two mechanisms

    **1. Style-driven banding (dynamic).** The table carries `<a:tblPr bandRow="1" firstRow="1">` plus a `<a:tableStyleId>` pointing at a table style whose `band1H`/`band2H` entries define the two fills. Nothing per-cell is stored. The renderer derives each row's fill from its *current index* at paint time. Delete a row and every row below shifts up one, parity flips, alternation is preserved automatically. This is what a native PowerPoint-authored deck normally looks like.

    **2. Baked-in cell fills (static).** Each `<a:tc>` carries its own `<a:tcPr><a:solidFill>`. The stripe colour is literal data on the cell, not a rule. Delete a middle row and the remaining cells keep their original fills — row 2 (band A) is now followed by what was row 4 (also band A) — so two adjacent rows end up the same colour. This happens in decks produced by tools that don't implement table-style banding and instead paint the stripes themselves: Google Slides and Keynote exports, LibreOffice, many python-pptx/openpyxl-style generators, HTML-to-PPTX and PDF-to-PPTX converters, and decks where someone "pasted as table" from Excel or a web page.

    ## Why one deck and not another

    The difference is almost always one of these:

    - **Authoring tool.** PowerPoint keeps banding declarative. Google Slides, Keynote, LibreOffice, and most programmatic generators materialise it. Convert a deck through one of those and a previously dynamic table becomes a grid of hard-coded fills.
    - **Editing path.** PowerPoint's own Delete Row may re-normalise the row and let the style re-stripe; splicing out an `<a:tr>` element with a script or XML editor only removes the row and leaves the explicit fills untouched.
    - **Unresolved style.** If the `tableStyleId` doesn't exist in the deck's `tableStyles.xml` (common after copy/paste between files or after a generator writes a GUID that isn't defined), viewers ignore banding and fall back to whatever explicit fills exist — or to no fill at all.
    - **Manual overrides.** If a user ever hand-coloured a cell, PowerPoint stores an explicit fill that wins over the style. A table that is "mostly banded" can be a mix of style-driven and overridden rows, so deletion produces a mismatch only where the override sits.
    - **Header/parity conventions.** With `firstRow="1"` banding starts on the second row; with `firstRow="0"` it starts on the first. Different tools count the band origin differently, so a deck whose stripes were baked against one convention looks off-by-one when the other convention is later applied.
    - **Banded columns vs. rows.** If `bandCol="1"` is set and `bandRow` is not, the visual striping may be coming from a different rule entirely, and row deletion won't fix it.

    ## Diagnostic

    Look at the XML. If `tblPr` has `bandRow="1"` and a resolvable `tableStyleId`, and the cells have no `tcPr/solidFill`, banding is dynamic and deletion is safe. If the cells carry `solidFill` values that alternate, the stripes are data and will not survive a structural edit. The fix is to strip the per-cell fills and re-enable `bandRow` (or re-stripe the rows after deleting).

    *Assumption, since no user is available: "deck" means a .pptx file, and the deletion was a structural row removal, not a visual hide.*
- id: pptx-rowdel-04
  answer: |
    **Assumption:** the question is about OOXML `.pptx` tables (DrawingML `a:tbl` / `a:tr`) rendered by PowerPoint, where "row" means a real table row, not a set of loose shapes.

    ## Short answer

    A row is *structure*, not paint. "Delete" means removing an `a:tr` element from the table's XML. Both described tricks remove nothing — they only change what one renderer happens to draw on one slide in one theme. The data, the row count, and the reading order all survive, so every layer except the pixels disagrees with the picture. That's the definition of a lie in the render layer, and render layers are the least durable part of a deck.

    ## Why the white rectangle is wrong

    - **It isn't anchored to the table.** It's a sibling shape in the shape tree, positioned in absolute slide coordinates. Insert a row above, delete a row, or let text reflow and the table grows/shifts while the rectangle stays put — it now hides the wrong row or leaves the real one exposed.
    - **Z-order and re-render are fragile.** Any "bring to front," theme change, layout swap, or re-import (LibreOffice, Google Slides, Keynote) can put cells above the rectangle or shift its position. Hiding should not depend on draw order.
    - **It doesn't follow the background.** White-on-white works only on a white slide background. A gradient, picture, or dark-theme background makes the patch obvious; printing without backgrounds makes it a white hole.
    - **It's an extra semantic object.** It shows up in the Selection Pane, in "select all," in copy-paste, in accessibility trees as an unlabeled decorative shape, and it intercepts clicks so you can't edit the cells beneath it.
    - **It blocks nothing downstream.** Copy the table into Excel, export to CSV/PDF, or run text extraction and the row is fully present. The rectangle is invisible to all of it.

    ## Why white text + near-zero height is wrong

    - **`a:tr@h` is a minimum, not a maximum.** PowerPoint grows rows to fit the text plus cell margins (default ~0.05" top/bottom). Set `h` to 0 and the row snaps back to whatever the content needs. The hack doesn't even achieve its own goal, and it usually pushes the table past the slide edge.
    - **Fill and borders still draw.** A styled table renders the row's `solidFill` and cell strokes regardless of font color, so a band/border still advertises the row's existence. You'd then have to strip fill and borders too — more invisible state to maintain.
    - **White text is still text.** It's selectable, editable, searchable, spell-checked, translated, and extracted. It's in the tagged-PDF text layer, so "hidden" content ships in the exported PDF. It also becomes visible under a table style with banding (white on a shaded band), under dark/high-contrast display modes, or the moment anyone does Ctrl+A → reset font color.
    - **It breaks merges.** If the row participates in a vertical merge (`rowSpan`/`gridSpan` geometry), collapsing its height corrupts the span.
    - **It leaks data if the intent was concealment.** Speaker notes, appendix slides, or a "hidden" slide (`show="0"`) are the supported ways to keep content out of view; white paint is not a redaction.

    ## The structural consequence

    Downstream consumers index by row: `python-pptx` row objects, data extraction, diffing, template regeneration, "row 5" references in scripts and review comments. A visually masked row makes the *visual* row N differ from the *XML* row N, so every index-based operation is off by the number of masked rows, silently. You get mismatched exports, edits applied to the wrong row, and reviews that can't tell what changed.

    ## What to do instead

    - To remove a row: delete the `a:tr` element (what PowerPoint's Delete Row does) and let the table's row set be the truth. PowerPoint tables have no hidden-row state, so "not shown" means "not there."
    - To keep the data: move it to speaker notes, an appendix slide, or a slide with `show="0"`.
    - To vary content per audience/version: generate the table with the intended rows rather than shipping all rows and masking — conditional generation is cheap; unmasking by a recipient is also cheap.

    **Bottom line:** both tricks make the deck *look* right while leaving the document wrong, and neither survives reflow, theming, other renderers, or extraction — so they fail as deletion, fail as concealment, and add hidden state that a future editor has to reverse-engineer.
- id: pptx-cell-01
  answer: |
    **Short answer:** you lose it. `cell.text = "SKU"` is a destructive setter — it clears the cell's text body and builds a fresh paragraph/run with no explicit `a:rPr`, so bold, 14 pt, and the white font color are gone. The text falls back to whatever the table style / theme prescribes (typically the theme text color at the style's default size).

    Mechanics: `_Cell.text` delegates to `TextFrame.text`, which clears the text frame and creates one new run holding the string (newlines become paragraph breaks). Cell-level properties — fill, margins, vertical anchor — survive; only the run/paragraph-level character formatting does not.

    **Keep it — edit the existing run instead of the cell:**

    ```python
    run = cell.text_frame.paragraphs[0].runs[0]
    run.text = "SKU"            # rPr untouched: bold / 14pt / white preserved
    ```

    If the cell has multiple runs, collapse them onto the first (which keeps its formatting) and delete the rest:

    ```python
    p = cell.text_frame.paragraphs[0]
    p.runs[0].text = "SKU"
    for r in p.runs[1:]:
        r._r.getparent().remove(r._r)
    ```

    **Or re-apply after assignment** (simplest when you don't care about the old run):

    ```python
    from pptx.util import Pt
    from pptx.dml.color import RGBColor

    cell.text = "SKU"
    run = cell.text_frame.paragraphs[0].runs[0]
    run.font.bold = True
    run.font.size = Pt(14)
    run.font.color.rgb = RGBColor(0xFF, 0xFF, 0xFF)
    ```

    **Or copy the old `rPr` XML** onto the new run (useful for a generic helper that must survive any formatting):

    ```python
    from copy import deepcopy

    p = cell.text_frame.paragraphs[0]
    src_rPr = p.runs[0]._r.rPr if p.runs else None
    cell.text = "SKU"
    new_run = cell.text_frame.paragraphs[0].runs[0]
    if src_rPr is not None:
        old = new_run._r.rPr
        if old is not None:
            new_run._r.remove(old)
        new_run._r.insert(0, deepcopy(src_rPr))
    ```

    Caveats worth stating: `paragraphs[0].runs[0]` raises `IndexError` on an empty cell — guard with `if p.runs:` and fall back to `cell.text` + reapply. And `cell.text` is perfectly fine for cells that carry no explicit character formatting. Also note the table style can still make the header bold on its own (first-row `tcTxStyle`), which can mask the loss visually while your explicit 14 pt / white are still gone.
- id: pptx-cell-02
  answer: |
    **Short answer:** the row gets taller. PowerPoint table rows have a *minimum* height, not a fixed one — so when the new name wraps, the row expands vertically, the column width stays put, and the rest of the table shifts down.

    ## What actually happens

    - **Text wraps, it does not bleed sideways.** Unlike Excel, a table cell in PowerPoint clips/wraps at its column boundary; it won't spill into the neighbouring cell.
    - **Row height is a floor, not a ceiling.** Even if you typed an exact height in *Table Layout → Cell Size → Height*, PowerPoint silently grows the row when content needs more room. There is no "Exactly" option and no "shrink text on overflow" for table cells (that autofit setting exists for text boxes and placeholders, not tables).
    - **Column width is unchanged** unless you drag it. The extra length is absorbed entirely as vertical growth, so a long name can produce a tall, narrow, multi-line cell.
    - **The table's total footprint grows downward.** The table frame gets taller; anything below it on the slide does not reflow — it gets overlapped or the table runs off the bottom edge of the slide.
    - **A very long unbroken token** (no spaces, e.g. `SuperGadgetProMaxUltra2026`) will wrap mid-word rather than overflow, usually at an ugly point.

    ## How to control it

    1. **Widen the column** — drag the column border or use *Table Layout → Distribute Columns*. Usually the best fix; lets the name sit on one or two lines.
    2. **Shrink the font / pick a condensed face** for that cell only. Set the font size on the cell, not the whole table.
    3. **Trim cell margins** — *Table Layout → Cell Margins* (or Alignment group). Reducing left/right margins to ~0.03" buys several characters per line and reduces wrapping.
    4. **Set a target row height and distribute evenly** — *Table Layout → Distribute Rows* to make all rows match, so one tall cell doesn't make the table look lopsided.
    5. **Turn off word wrap** only if you genuinely want one long line — but the text will then be clipped at the cell edge, so this is rarely what you want.
    6. **Fix the overall geometry** — if the table must stay within a fixed region, pre-size the table to your slide safe area and budget for the longest label; otherwise accept that adding a long name means the table gets taller and you must move or resize what's beneath it.
    7. **Restructure the content** rather than fight the layout: put the long name in a footnote/key, use an abbreviation with a legend, or convert the table to shapes/text boxes where you *do* get autofit-to-shrink.

    **Assumption:** PowerPoint 2016/2019/365 behaviour. Word tables differ — Word offers a true "Exactly" row height that clips overflow, PowerPoint does not.
- id: pptx-cell-03
  answer: |
    I don't have this deck's contents (no file access in this run), so I can't name the exact cells or slide — but I can state what structurally must change.

    Changing one line item's Qty from 5 to 12 means you must update every value derived from that quantity:

    - **Line total / extended price** for that row (Qty × Unit Price = 12 × unit price, not 5 × unit price).
    - **Subtotal**, if it sums line totals.
    - **Discount, tax, and shipping** amounts, if any are computed as a percentage of the subtotal.
    - **Grand total / amount due**, and any "Total" cell on a summary or closing slide that mirrors it.
    - **Any KPI, chart, or callout** that shows units or revenue (e.g., "5 units", totals bars) — these often live on a separate slide and get missed.
    - **Narrative text** that references the old quantity or old total.

    The key principle: a quantity is an input; everything downstream of it is a dependent cell. The most commonly missed updates are the grand total on a different slide and the prose/callout mentions of "5".

    Assumption: the deck contains a pricing/line-item table with computed totals. If instead the deck has no derived totals, then only that row's Qty cell needs changing — and I can't confirm which case applies without seeing the file.
- id: pptx-relayout-01
  answer: |
    Assumption: "how" means programmatically on the OOXML (`python-pptx` or raw XML), since a UI copy/paste loses nothing but isn't reproducible. If you meant the PowerPoint UI: click the Service G row → **Table Layout → Insert Below** (or copy the row and paste), then retype the cells.

    **Core idea:** don't use `table.add_row()`. It appends at the bottom and creates a new `<a:tr>` carrying only table-style defaults — not the direct formatting (fills, borders, fonts, row height) that the existing rows have. Instead, **deep-copy the `<a:tr>` element of the row above the insertion point ("Service G"), insert the copy immediately after it, then replace only the text inside its runs.** Row order in a pptx table is purely positional (`<a:tr>` document order); there is no row index to renumber.

    ```python
    import copy
    from pptx import Presentation
    from pptx.table import _Cell

    prs   = Presentation("in.pptx")
    table = next(s.table for s in prs.slides[0].shapes if s.has_table)

    rows = list(table.rows)
    src_i   = next(i for i, r in enumerate(rows) if r.cells[0].text.strip().startswith("Service G"))
    total_i = next(i for i, r in enumerate(rows) if r.cells[0].text.strip().startswith("Total"))
    assert total_i == src_i + 1, "unexpected layout"

    # 1. Clone the row directly above Total: keeps a:trPr (height), a:tcPr (fill/border/margins), a:rPr (font)
    new_tr = copy.deepcopy(rows[src_i]._tr)

    # 2. Insert it right after Service G == immediately before Total
    rows[src_i]._tr.addnext(new_tr)

    # 3. Replace text run-by-run so run properties survive (do NOT do cell.text = ...)
    values = ["Service H", "$1,250", "4", "Active"]      # one per grid column
    for j, val in enumerate(values):
        cell = _Cell(new_tr.tc_lst[j], table)
        para = cell.text_frame.paragraphs[0]
        if para.runs:
            para.runs[0].text = val                      # rewrites <a:t>, keeps <a:rPr>
            for r in para.runs[1:]:                      # drop leftover runs
                para._p.remove(r._r)
        else:
            para.text = val                              # empty cell: no rPr to preserve

    prs.save("out.pptx")
    ```

    Why each step matters:

    - **`deepcopy` of the sibling row, not the Total row.** The Total row is usually bold/merged and often a different height; cloning Service G gives the new row identical body-row styling, and `addnext` puts it in the right place in one move.
    - **Copying the `<a:tr>` copies the whole subtree**: `<a:trPr>` (row height, hidden flag), each `<a:tcPr>` (solid fill, line borders, margins, vertical anchor), and each run's `<a:rPr>`. That is "keeping the formatting."
    - **Never set `cell.text` / `text_frame.text`** if the cell has multiple runs or explicit run properties — the setter rebuilds the paragraph and drops `<a:rPr>`. Edit the `<a:t>` node (`run.text = ...`) and delete extra runs instead.
    - **Column widths** come from `<a:tblGrid>`, which you did not touch, so the new row aligns automatically. Keep the cloned `h` on `<a:tr>` or remove the attribute to let the row auto-fit.
    - **Element order is schema-checked.** `<a:tbl>` children must stay `<a:tblPr>, <a:tblGrid>, <a:tr>…, <a:extLst>`. Using `addnext` on an existing `<a:tr>` preserves that; don't append blindly.

    Merges are the one real trap:

    - If "Service G" has a cell with `rowSpan="n"` (vertical merge running downward), inserting a row inside that span breaks it. You must either insert after the span ends, or increment that cell's `rowSpan` and give the new row a `<a:tc vMerge="1"><a:tcPr/></a:tc>` in that column.
    - If you insert a row that should span columns like its neighbour, the cloned `gridSpan`/`hMerge` attributes carry over correctly — but the number of `<a:tc>` elements must still equal the number of `<a:gridCol>` entries. `hMerge`/`vMerge` cells still count as cells, so a straight clone keeps the count valid.
    - If the Total row has a horizontally merged label, it is unaffected because you inserted above it.

    Alternative (weaker): `table.add_row()`, then move the new `<a:tr>` with `total_tr.addprevious(new_tr)`. Position ends up right, but formatting is table-style default only — acceptable only when the table relies on a style with banding and no direct cell formatting. For a table with explicit fills/borders/fonts, it visibly mismatches.

    Validation: reopen `out.pptx`, assert the text sequence is `…, Service G, Service H, Total`, and unzip `ppt/slides/slideN.xml` to confirm exactly one extra `<a:tr>` sitting before the Total `<a:tr>`; open once in PowerPoint to confirm no repair prompt (a repair prompt means a bad cell count or a broken merge).
- id: pptx-relayout-02
  answer: |
    Assume a native DrawingML table (`<a:tbl>`), not a picture/OLE object. The change is entirely inside the slide part — no new parts, relationships, or `[Content_Types].xml` entries.

    ## XML changes

    1. **Add a `<a:gridCol>` after the Item column** in `<a:tblGrid>`. Let `j` be Item’s zero-based grid-column index; insert at `j+1`.

    ```xml
    <a:tblGrid>
      <a:gridCol w="1600200"/> <!-- Item -->
      <a:gridCol w="900000"/>  <!-- new SKU column -->
      <a:gridCol w="2400000"/>
      ...
    </a:tblGrid>
    ```

    2. **Add a matching `<a:tc>` at the same position in every `<a:tr>`** — header row and all data rows. A minimal header cell:

    ```xml
    <a:tc>
      <a:txBody>
        <a:bodyPr/>
        <a:lstStyle/>
        <a:p>
          <a:r>
            <a:rPr lang="en-US"/>
            <a:t>SKU</a:t>
          </a:r>
        </a:p>
      </a:txBody>
      <a:tcPr/>
    </a:tc>
    ```

    For blank data cells, use an empty paragraph, e.g. `<a:p><a:endParaRPr lang="en-US"/></a:p>`.

    Copying the neighboring Item cell and changing only its text is usually safer because it preserves `<a:tcPr>`, margins, fill, borders, anchor, and run properties.

    3. **Update the table frame width** in `<p:xfrm>/<a:ext cx="...">` so it equals the sum of all `<a:gridCol w="...">` values. PowerPoint keeps these in sync; leaving them mismatched causes layout jumps or repair prompts.

    4. **Handle merges.** Normally each row has one `<a:tc>` per grid column, including covered merge cells. If the insertion point falls inside an existing horizontal merge, increment the merge origin’s `gridSpan` and mark the inserted cell with `hMerge="1"` / appropriate `vMerge="1"` rather than treating it as a normal independent cell. If Item itself is merged into the next column and you want a separate SKU column, unmerge first and re-merge explicitly.

    ## Keeping it on the slide

    Do **not** simply increase `a:ext/@cx` by the new column width when the table already nearly fills the slide. Instead:

    - Read the slide width: `<p:sldSz cx="..."/>` in `presentation.xml` — 13,333,333 EMU for 16:9, 9,144,000 EMU for 4:3.
    - Keep the right edge inside the slide:

    ```text
    xfrm/off/@x + xfrm/ext/@cx <= sldSz/@cx
    ```

    - Usually keep the table’s total width unchanged and subtract the new column’s width from existing columns — either proportionally or from the widest column(s), then adjust one column for rounding so the sum is exact.
    - Set `a:ext/@cx` to the final sum of the grid-column widths.
    - Leave `off/@x` alone unless you deliberately recenter; with a nearly full-width table there is usually no horizontal room to grow.
    - Adding a column does not change row heights or `ext/@cy`, unless narrower columns cause text to wrap. Recheck header wrapping and total table height afterward.

    ## Style and validation gotchas

    - `<a:tblPr>` attributes such as `firstCol`, `lastCol`, and `bandCol` refer to logical columns. If `lastCol="1"` and SKU becomes the new last column, it inherits last-column styling; set explicit cell properties or adjust the flags if that is unwanted.
    - The table style ID itself does not need to change.
    - Validate that every `<a:tr>` has the same number of `<a:tc>` elements as there are `<a:gridCol>` elements, that the grid-width sum matches `a:ext/@cx`, and that the table’s right edge is within the slide width. Then open it in PowerPoint and confirm there is no repair prompt.
- id: pptx-tblins-01
  answer: |
    **Short answer:** python-pptx has no "find me space" helper, so you do it yourself. Read every existing shape's bounding box (`left/top/width/height`, in EMU), search the slide for a rectangle that collides with none of them, then call:

    ```python
    graphic_frame = slide.shapes.add_table(rows, cols, left, top, width, height)
    table = graphic_frame.table
    ```

    `add_table` returns a `GraphicFrame`, not a `Table` — that's the most common mistake. Coordinates are EMU with origin at the slide's top-left; `Inches(...)` values are acceptable.

    A 3×2 table means **3 rows, 2 columns**: `add_table(3, 2, ...)`.

    ## Picking the position

    The reliable approach is a coarse grid scan over the slide, testing each candidate rectangle against all existing shape boxes, with a gap. Prefer top-most then left-most (reading order). Fall back to "below the lowest content" if the scan finds nothing, and fail loudly rather than overlapping.

    ```python
    from pptx import Presentation
    from pptx.util import Inches

    def rect(shape):
        """Bounding box of a shape, or None if it has no usable geometry."""
        if None in (shape.left, shape.top, shape.width, shape.height):
            return None
        if shape.width <= 0 or shape.height <= 0:
            return None
        return (shape.left, shape.top, shape.width, shape.height)

    def hits(a, b, gap=0):
        ax, ay, aw, ah = a
        bx, by, bw, bh = b
        return (ax < bx + bw + gap and bx < ax + aw + gap and
                ay < by + bh + gap and by < ay + ah + gap)

    def free_spot(slide, prs, w, h,
                  margin=Inches(0.4), gap=Inches(0.15), step=Inches(0.2)):
        sw, sh = prs.slide_width, prs.slide_height
        obstacles = [r for s in slide.shapes if (r := rect(s))]

        best = None
        y = margin
        while y + h <= sh - margin:
            x = margin
            while x + w <= sw - margin:
                cand = (x, y, w, h)
                if not any(hits(cand, o, gap) for o in obstacles):
                    score = (y, x)                 # top-most, then left-most
                    if best is None or score < best[0]:
                        best = (score, (x, y))
                x += step
            y += step
        return None if best is None else best[1]
    ```

    Then create and size it:

    ```python
    prs = Presentation("deck.pptx")
    slide = prs.slides[2]                          # existing slide

    w, h = Inches(6.0), Inches(1.5)                # desired table footprint
    pos = free_spot(slide, prs, w, h)
    if pos is None:
        raise RuntimeError("no free rectangle; shrink the table or add a slide")

    left, top = pos
    gf = slide.shapes.add_table(3, 2, left, top, w, h)
    table = gf.table

    # Columns and rows drive the real rendered geometry.
    table.columns[0].width = Inches(2.4)
    table.columns[1].width = Inches(3.6)           # sums to w = 6.0
    for row in table.rows:
        row.height = Inches(0.5)                   # 3 × 0.5 = h = 1.5

    table.first_row = True                         # header emphasis
    table.horz_banding = True

    table.cell(0, 0).text = "Key"
    table.cell(0, 1).text = "Value"
    for r in range(1, 3):
        table.cell(r, 0).text = f"Item {r}"
        table.cell(r, 1).text = ""

    prs.save("deck-out.pptx")
    ```

    ## Details that actually bite

    - **The frame and the grid are separate.** `w`/`h` set the GraphicFrame's `a:ext`, but PowerPoint renders from the `gridCol`/`gridRow` values. Keep column widths summing to `w` and row heights summing to `h`, or set `gf.width`/`gf.height` afterward to match, otherwise selection handles and rendering disagree.
    - **Rows grow at render time.** If text plus cell margins (`cell.margin_top/bottom`, default ~0.05") exceeds a row height, PowerPoint expands the row and your neatly computed clearance disappears. Either size rows from content (`max(0.4", lines * line_height + margins)`) or reserve extra bottom slack.
    - **Text uses paragraphs, not a plain string buffer.** `cell.text = "x"` replaces content with one paragraph; for formatting use `cell.text_frame.paragraphs[0]` and its runs.
    - **Some shapes have no geometry** (e.g., certain placeholders/connectors), and full-bleed background pictures cover every candidate. Skip shapes with `None` bounds, zero area, off-slide positions, and full-slide pictures (or accept that a background image must be ignored as an obstacle).
    - **Group shapes:** their `left/top/width/height` is already the group bounding box, so treat them as single obstacles; don't recurse.
    - **Placeholders:** `add_table` does not insert into a placeholder. If the slide has an empty body/content placeholder, you may reuse its geometry, but you must delete or hide the placeholder or its prompt text remains visible.
    - **Verify after insertion:** recompute the new frame's rect and assert no collision with the recorded obstacles. That catches arithmetic mistakes before rendering.
    - **Style:** a default table style is applied. To match a deck's theme, copy the `a:tableStyleId` from an existing table's `tblPr`, since python-pptx exposes only the banding/header flags, not the style ID directly.
    - **Fallbacks:** if the scan fails, place the table below `max(top+height)` of existing content and shrink `h`; if that still exceeds the slide, the honest answer is a new slide or resized content, not overlap.

    **Assumption:** "3×2" is 3 rows × 2 columns (python-pptx takes `rows` first), and the slide already has content whose `left/top/width/height` are populated.
- id: pptx-tblins-02
  answer: |
    **Short answer:** `add_table` doesn't inherit anything from the deck — it stamps in python-pptx's built-in default, which is *Medium Style 2 – Accent 1* (the blue one). Copy the existing table's style ID (and banding flags) onto the new table, or clone an existing table's XML outright.

    ## Why it's blue

    python-pptx's `shapes.add_table(...)` builds:

    ```xml
    <a:tblPr firstRow="1" bandRow="1">
      <a:tableStyleId>{5C22544A-7EE6-4342-B048-85BDC9FD1C3A}</a:tableStyleId>
    </a:tblPr>
    ```

    `{5C22544A-7EE6-4342-B048-85BDC9FD1C3A}` = "Medium Style 2 – Accent 1". That GUID is the blue default; it's baked into the generated XML, not pulled from the slide master or theme.

    ## Fix: copy the style from a real table

    Find a table already in the deck, read its `tblPr`, and transplant the style ID and the `firstRow`/`bandRow`/etc. flags:

    ```python
    from copy import deepcopy
    from pptx.oxml.ns import qn


    def first_table(slide):
        for sh in slide.shapes:
            if sh.has_table:
                return sh.table
        return None


    def match_table_style(new_tbl, ref_tbl):
        # header row / banding flags
        new_tbl.first_row    = ref_tbl.first_row
        new_tbl.first_col    = ref_tbl.first_col
        new_tbl.last_row     = ref_tbl.last_row
        new_tbl.last_col     = ref_tbl.last_col
        new_tbl.horz_banding = ref_tbl.horz_banding
        new_tbl.vert_banding = ref_tbl.vert_banding

        new_pr = new_tbl._tbl.tblPr
        ref_pr = ref_tbl._tbl.tblPr

        # drop inherited default style
        for el in new_pr.findall(qn('a:tableStyleId')):
            new_pr.remove(el)

        ref_style = ref_pr.find(qn('a:tableStyleId'))
        if ref_style is not None:
            new_pr.insert(0, deepcopy(ref_style))   # tableStyleId is first child
    ```

    Usage:

    ```python
    gf    = slide.shapes.add_table(4, 3, left, top, width, height)
    new   = gf.table
    ref   = first_table(slide)          # any table with the look you want
    match_table_style(new, ref)
    ```

    That's enough when the deck's tables use a **built-in or custom table style**. Because the ID resolves against the deck's theme and `ppt/tableStyles.xml`, the new table will render with the same fills, borders, and accent colors as its neighbors.

    ## When that still won't match

    - **Existing table was hand-formatted** (cell fills, borders, per-run fonts set explicitly, or `tableStyleId` absent). The style ID is then irrelevant. Either deep-copy the reference `<a:tbl>` and swap the cell text, or copy formatting per cell:

      ```python
      for r in range(rows):
          for c in range(cols):
              src, dst = ref.cell(r, c), new.cell(r, c)
              dst.fill.solid()
              dst.fill.fore_color.rgb = src.fill.fore_color.rgb
              # borders need XML: deepcopy a:lnL/lnR/lnT/lnB from src._tc.tcPr
      ```

    - **Explicit run fonts.** Table styles control fills/borders, not necessarily text. If the deck's tables set `Helvetica 11pt` on runs, set `run.font.name/size/color` on every new cell too, or clone the paragraph XML.

    - **Renderer differences.** PowerPoint honors `tableStyleId`; LibreOffice and some previewers only partially do (they may show plain gridlines or ignore banding). If your check is "does it look right", verify in PowerPoint, not just a converted PNG.

    - **Cloning whole tables.** The most faithful path for a same-shape table is replacing the new `<a:tbl>` with `deepcopy(ref_tbl)` and rewriting the `a:t` text nodes. It is exact but brittle if row/column counts differ, since you must also rebuild `a:tblGrid` and `a:tr`.

    ## Assumptions

    - `add_table` = python-pptx `SlideShapes.add_table`, per the blue "Medium Style 2 – Accent 1" symptom.
    - The target deck's tables share a consistent built-in/custom style; if they don't, per-cell copying is the reliable route.

    Core principle, if the library is something else: don't fight the default — read the style identifier/formatting from an existing table and apply it, or clone the existing table XML.
- id: pptx-tblins-03
  answer: |
    Short answer: you don't stuff the table "into" the placeholder. PowerPoint itself doesn't do that either — when you click the table icon in a content placeholder, it inserts a `<p:graphicFrame>` containing `<a:tbl>` as a *sibling* of the placeholder `sp`, sized/positioned to match it. The placeholder stays (usually empty) behind it.

    So the idiomatic pattern is: find the placeholder, steal its geometry, add the table at that geometry, and optionally delete the now-redundant placeholder.

    **python-pptx**

    ```python
    from pptx.enum.shapes import PP_PLACEHOLDER

    # 1. locate the content placeholder on the slide (not the layout)
    ph = next(
        p for p in slide.placeholders
        if p.placeholder_format.type in (PP_PLACEHOLDER.OBJECT, PP_PLACEHOLDER.BODY)
    )

    # 2. take its geometry
    left, top, width, height = ph.left, ph.top, ph.width, ph.height

    # 3. add the table as a normal graphic frame at that spot
    gf = slide.shapes.add_table(rows, cols, left, top, width, height)
    table = gf.table

    # 4. optional: remove the empty placeholder so it doesn't linger
    #    as a "Click to add text" box in edit mode
    ph._element.getparent().remove(ph._element)
    ```

    Key points / gotchas:

    - `shapes.add_table(...)` creates a graphic frame in the slide's shape tree. There is no `placeholder.insert_table()` API in python-pptx, and none is needed — this is exactly what PowerPoint produces.
    - Placeholder geometry can be `None` if the slide's `sp` has no explicit `<a:xfrm>` (position/size inherited from the layout). In that case fall back to the matching placeholder on `slide.slide_layout` (same `idx`) to read its `left/top/width/height`, or compute margins yourself.
    - Match by `idx` rather than by type if the layout has several content placeholders: `ph.placeholder_format.idx`.
    - If you keep the placeholder instead of deleting it, it renders as nothing during a slideshow but shows as an empty prompt in edit view. Deleting the `sp` element is the clean fix; z-order also stays sane because the table was added after it.
    - In raw OOXML, the equivalent is inserting a sibling `p:graphicFrame` with the `a:graphicData` URI `http://schemas.openxmlformats.org/drawingml/2006/table` at the placeholder's `a:off`/`a:ext`.

    If you were hoping for a "fill the placeholder with a table" primitive: it doesn't exist at the file-format level. The placeholder holds text; tables are always separate graphic frames.
- id: pptx-imgrep-01
  answer: |
    The clean way is to swap the image reference inside the existing `<p:pic>` element — not delete and re-add the shape. That keeps the shape element (and therefore its position, size, rotation, crop, alt text, hyperlink, animation targets) and its index in the shape tree, which is what determines stacking order.

    ```python
    from pptx import Presentation
    from pptx.util import Emu

    prs = Presentation("in.pptx")
    slide = prs.slides[0]
    pic = slide.shapes[3]          # must be a Picture

    # 1. Add the new image to this slide's part; get its relationship id.
    #    python-pptx dedupes identical images by hash automatically.
    image_part, new_rId = slide.part.get_or_add_image_part("new.png")

    # 2. Point the existing blip at the new relationship.
    blip = pic._element.blipFill.blip
    old_rId = blip.rEmbed
    blip.rEmbed = new_rId

    prs.save("out.pptx")
    ```

    Why this works:

    - `pic._element` is the `CT_Picture` (`<p:pic>`) XML node. You never touch it structurally, so `a:xfrm` (offset/extent), `a:prstGeom`, `p:spPr`, name, z-order in `p:spTree`, and any timing/hyperlink references all stay intact.
    - Only the `r:embed` attribute on `<a:blip>` changes, so the shape now renders a different image part.
    - Because position/size live on the shape and not on the image part, nothing needs to be recalculated. If the new image has a different aspect ratio, the existing `a:srcRect` crop still applies; clear it (`pic.crop_left = pic.crop_right = ... = 0`) or adjust it if you want the whole image shown.

    If the attribute setter isn't available in your version, do it explicitly:

    ```python
    from pptx.oxml.ns import qn
    blip.set(qn("r:embed"), new_rId)
    # or: pic._element.xpath(".//a:blip")[0].set(qn("r:embed"), new_rId)
    ```

    Notes / caveats:

    - The old relationship is left dangling on the slide part unless you drop it: `slide.part.drop_rel(old_rId)`. It's harmless — orphaned parts aren't written out — but dropping keeps the file tidy. Don't drop it if you're unsure whether another shape on the same slide uses the same rId.
    - Don't mutate `image_part._blob` in place instead. It's tempting but if the new file's format/content-type differs (PNG → JPEG) the part's content type becomes wrong and PowerPoint may complain.
    - The remove-and-reinsert alternative works too — record `idx = list(spTree).index(pic._element)`, add the new picture, then `spTree.remove(new); spTree.insert(idx, new); spTree.remove(old)` — but it loses crop/alt-text/hyperlink metadata and is easier to get wrong, so prefer the blip swap.
    - Works the same for pictures inside group shapes; just use the appropriate part (`pic.part`).

    ---

    ## Recap

    1. **ASKED** — How to replace a picture's image in python-pptx while preserving position, size, and z-order.
    2. **WORKED** — BUILT: recipe that adds a new image part and rewrites the `<a:blip r:embed>` on the existing `<p:pic>`, leaving the shape node untouched.
    3. **FOUND** — Since geometry and stacking live on the shape XML, swapping only the relationship id is sufficient; in-place blob mutation risks content-type corruption.
    4. **DECIDED** — Blip-swap recommended over delete/re-add, with optional `drop_rel` cleanup. BOTTOM LINE: change the image reference, never the shape.
    5. **NEXT** — None — answer is from knowledge; verify against your python-pptx version if `rEmbed` setter misbehaves (use `qn("r:embed")`).
- id: pptx-imgrep-02
  answer: |
    Because a PPTX doesn't store the logo per slide — it stores one image part in `/ppt/media/` and each slide just points at it through a relationship. Slide 1 and slide 7 are almost certainly referencing the **same** image part (same `r:embed` target, either literally the same `rId` in their `.rels` or two `rId`s that resolve to the same `/ppt/media/imageN.png`). PowerPoint deduplicates identical image bytes on save, so a logo reused on multiple slides collapses into a single media part.

    So when you overwrite `imageN.png` in place, you're not editing "slide 1's picture" — you're editing the one shared blob that both slides resolve to. Every slide that references that part changes at once. Slide 7 was never independent, so there was nothing to "leave alone."

    The same trap applies to slide layouts/masters and to any other part (e.g. a theme or background image) that happens to share the same target.

    Fix: don't mutate the existing part. Add a **new** media part (unique filename, e.g. `imageN+1.png`), add a **new** relationship in `ppt/slides/_rels/slide1.xml.rels` pointing at it, and change only slide 1's `<a:blip r:embed="rIdNew"/>`. Leave slide 7's relationship and `r:embed` untouched. Also make sure `[Content_Types].xml` has a `Default` entry for the new extension, and don't garbage-collect the old part while slide 7 still references it.

    Two related gotchas:

    - Rel IDs are per-part, not global. Reusing `rId2` in slide 1's rels is fine even if slide 7 also has an `rId2` — they're separate relationship namespaces. What matters is which part each one targets.
    - If you rebuild the package with a tool that re-deduplicates by content hash, your "new" part can be merged back into the old one if the bytes are identical. To force distinctness, either keep the bytes different or verify the writer doesn't collapse parts.

    Why the symptom looked like "slide 7 changed too": you were treating the media filename as if it were scoped to a slide. In OOXML it's package-scoped and reference-counted; slides own relationships, not images.
- id: pptx-imgrep-03
  answer: |
    Short version: relationships belong to the **part**, not to the picture, and python-pptx deduplicates images. So two pictures showing the same image on the same slide typically share a single image part *and a single rId*. `drop_rel(rId)` is a low-level OPC operation on the part's relationship dictionary — it has no notion of which `<p:pic>`/`<a:blip>` elements use that rId, so popping the rId strands every blip that references it.

    Mechanics:

    - `add_picture` / `get_or_add_image` hashes the image; an identical image returns the *same* `ImagePart`. Then `part.relate_to(image_part, RT.IMAGE)` goes through `Relationships.get_or_add`, which returns the existing rId for that (reltype, target) pair. Both `<p:pic>` elements end up with `r:embed="rIdN"` — one rel, two users.
    - `drop_rel("rIdN")` deletes that entry from `part.rels`. It does not scan the slide XML for other references, does not refcount per element, and does not consider that the image part may still be needed. The second picture's `r:embed` is now dangling; PowerPoint shows a missing image (and may prompt to repair).
    - The safety net in modern python-pptx is only a heuristic: `drop_rel` refuses when `_rel_ref_count(rId)` is 2 or more, where that count is a raw substring count of the rId bytes in the part's XML blob. That's not real usage tracking. It can under-count (stale/unsaved blob, older python-pptx without the guard, or a reference living outside the blob being counted), and it can over-count (`rId1` matches inside `rId10`). So it is easy to get into the "dropped while in use" state, and on old versions there is no guard at all.

    Important caveat: with current python-pptx and two blips in the same slide part's XML, `drop_rel` would normally see the rId twice and **no-op**, so if the other picture really broke, the likely causes are (a) an older/patched version without the ref-count guard, (b) the count looked at a blob that didn't yet contain the second reference, or (c) the image part itself was removed/pruned rather than just the rel.

    Right way to remove one picture: delete its shape element (`picture._element.getparent().remove(picture._element)`), then drop the rel only if nothing else references that rId — e.g., inspect `slide.part.rels` and count `r:embed="rIdN"` occurrences in the slide XML (accounting for the shared-image case). Or leave the now-orphaned rel/part and let the package writer prune parts that are no longer reachable from any relationship. Never assume an image rId is exclusive to the picture you're deleting.
- id: pptx-imgins-01
  answer: |
    ## Short answer

    Set the width to 2″ and let the height follow the image's native aspect ratio, then anchor the picture so its right and bottom edges sit a small margin in from the slide's right and bottom edges. Verify the resulting rectangle doesn't intersect any existing shape's bounding box; if it does, move/shrink into free space (or reserve a footer band) rather than overlapping.

    ## python-pptx

    `add_picture` preserves aspect ratio automatically when you pass only `width` — it computes `height = width × native_height/native_width`. Never pass both width and height with the 2″ value unless you want distortion.

    ```python
    from pptx import Presentation
    from pptx.util import Inches, Emu

    prs = Presentation("deck.pptx")
    slide = prs.slides[0]

    MARGIN     = Inches(0.25)
    TARGET_W   = Inches(2.0)          # 2 inches wide

    # 1) Insert; height is auto-derived from the PNG's aspect ratio
    pic = slide.shapes.add_picture("signature.png", 0, 0, width=TARGET_W)

    # 2) Anchor bottom-right
    pic.left = prs.slide_width  - pic.width  - MARGIN
    pic.top  = prs.slide_height - pic.height - MARGIN

    # 3) Collision check against existing shapes
    def box(s):
        return (s.left, s.top, s.left + s.width, s.top + s.height)

    def hits(a, b):
        return not (a[2] <= b[0] or b[2] <= a[0] or a[3] <= b[1] or b[3] <= a[1])

    target = box(pic)
    conflicts = [s for s in slide.shapes
                 if s != pic and s.has_text_frame is not None and hits(box(s), target)]
    ```

    If `conflicts` is non-empty, you have three good options:

    1. **Reserve a footer band** — shrink or reposition body content so all of it ends above `pic.top`. This is the cleanest, most deterministic fix.
    2. **Shrink the signature proportionally** — keep the 2″ width as a maximum, reduce until the box fits:
       ```python
       scale = 0.9
       while conflicts and pic.width > Inches(1.0):
           pic.width  = Emu(int(pic.width  * scale))
           pic.height = Emu(int(pic.height * scale))   # both scaled → ratio kept
           pic.left = prs.slide_width  - pic.width  - MARGIN
           pic.top  = prs.slide_height - pic.height - MARGIN
           conflicts = [s for s in slide.shapes if s != pic and hits(box(s), box(pic))]
       ```
       (Or re-insert with a smaller `width=` so the library recomputes height for you.)
    3. **Use a layout with a dedicated signature/footer region** (blank or "Title Only" + footer), or put it on the slide master so no body placeholder occupies that corner.

    Also exclude the background/full-bleed picture from the collision test, or it will always "conflict".

    ## PowerPoint UI

    1. **Insert → Pictures → This Device**, pick `signature.png`.
    2. **Picture Format → Size**: set **Width = 2″**, and tick **Lock aspect ratio** (in the Size dialog / Layout & Properties) so height updates proportionally.
    3. Drag the picture to the bottom-right; use **Align → Align Right** and **Align Bottom** (relative to Slide) for exact placement, plus a small nudge for margin.
    4. Confirm no overlap: **Home → Select → Selection Pane**, or **View → Guides**; if body text is in the way, either enable **AutoFit/shrink text** or reduce the content placeholder's height so the corner stays clear. **Arrange → Send to Back** only hides overlap visually — it does not satisfy "without covering content."

    ## Assumptions

    - The task is programmatic insertion into a `.pptx` (python-pptx), with the UI steps given as the equivalent manual path.
    - "Bottom-right" means inside the slide edge with a small (~0.25″) margin.
    - `signature.png` has a sensible native aspect ratio (transparent PNG, no baked-in whitespace); if it has padding, crop it first so the 2″ width reflects actual ink.
- id: pptx-imgins-02
  answer: |
    **Short answer:** `insert_picture()` doesn't fit the photo *inside* the placeholder — it *fills* the placeholder. Since that's a crop-to-fill operation, and your placeholder is presumably wider than it is tall (landscape) while the photo is taller than it is wide (portrait), the overflowing top/bottom (or sides) get cropped away.

    **Mechanics**

    `PicturePlaceholder.insert_picture(image_file)` sizes the picture to the placeholder's extents, scales the image proportionally to preserve its aspect ratio, then crops the overhang so nothing spills outside or gets letterboxed. The crop is written into the picture's `a:srcRect` element, which is why the file still contains the full image — PowerPoint just renders a window onto it.

    So:

    - Placeholder 4:3 landscape, photo 3:4 portrait → image is scaled so its *width* matches the placeholder, and the excess height is trimmed off top and bottom.
    - The crop amounts appear on the returned object as non-zero `crop_left` / `crop_top` / `crop_right` / `crop_bottom` (fractions of the image, 0.0–1.0). If you inspect those on the returned picture you'll see exactly how much vanished.

    **Confirm it yourself**

    ```python
    pic = ph.insert_picture("portrait.jpg")
    print(pic.crop_left, pic.crop_top, pic.crop_right, pic.crop_bottom)
    ```

    Non-zero values = cropped, by design, not a bug or a corrupt file.

    **Fixes, depending on what you want**

    1. **Show the whole photo, resized to fit the placeholder** (letterbox style): after insert, zero the crops and let the shape resize, or simply resize the placeholder to the image's aspect ratio *before* inserting.
       ```python
       pic.crop_left = pic.crop_top = pic.crop_right = pic.crop_bottom = 0
       ```
       With crops cleared the picture is stretched to the placeholder box, so if the aspect ratios differ you'll get distortion — which is why you normally combine this with resizing the shape.

    2. **Keep proportions, no crop, no distortion:** compute the target box yourself and set the shape's width/height to match the image aspect ratio, or bypass the placeholder entirely and use `slide.shapes.add_picture(path, left, top, width=..., height=...)` with only one dimension given (the other follows the aspect ratio).

    3. **Accept the crop but recenter it**, e.g. bias toward the face rather than the middle, by adjusting the crop values asymmetrically.

    **Why it's designed this way:** placeholders carry a fixed layout geometry that the deck's design depends on; silently letterboxing would leave ragged empty bands inside the placeholder frame. Cropping keeps the layout clean and is the same "fill" behavior as PowerPoint's own picture-fill. The trade-off is that portrait images in landscape placeholders always lose their top and bottom unless you explicitly undo it.
- id: pptx-legacy-01
  answer: |
    ## Short answer

    No. python-pptx cannot open `deck.ppt`. It only understands the Open XML (OPC/ZIP) family — `.pptx` (and in practice the macro-enabled variants, though macros are not preserved). `.ppt` is the PowerPoint 97–2003 **binary** format (an OLE2 / Compound File container, `[MS-PPT]` records), not a ZIP of XML parts, so python-pptx fails immediately:

    ```
    pptx.exc.PackageNotFoundError: Package not found at 'deck.ppt'
    ```

    It doesn't fail because the file is corrupt — it fails because `Presentation()` checks for a ZIP/OPC package and a CFB file isn't one. (Detection: `.ppt` starts with `D0 CF 11 E0 A1 B1 1A E1`; `.pptx` starts with `PK\x03\x04`. `file deck.ppt` reports "Composite Document File V2 … Microsoft PowerPoint".)

    There is also no pure-Python library that will *write* the binary `.ppt` format, and none that reliably round-trips its table structures. `olefile` can open the CFB container but gives you raw streams, not usable slides/tables.

    ## Workflow

    **0. Back up and clarify the deliverable.** `cp deck.ppt deck.orig.ppt`. Ask whether the client actually needs `.ppt` back or will accept `.pptx` — this decision drives everything below.

    **1. Try to get a `.pptx` at the source first.** Ask the author for the original `.pptx` (or a Google Slides / Keynote / LibreOffice source). Any conversion is lossy; the original is not.

    **2. Convert `.ppt` → `.pptx`.**

    - Best fidelity, if PowerPoint is installed (Windows, or macOS with PowerPoint) — COM automation, conversion done by PowerPoint itself:
      ```python
      import win32com.client as win32
      app = win32.Dispatch("PowerPoint.Application")
      pres = app.Presentations.Open(r"C:\work\deck.ppt", WithWindow=False)
      pres.SaveAs(r"C:\work\deck.pptx", 24)   # ppSaveAsOpenXMLPresentation
      pres.Close(); app.Quit()
      ```
    - Otherwise LibreOffice headless (macOS path: `/Applications/LibreOffice.app/Contents/MacOS/soffice`):
      ```bash
      soffice --headless --convert-to pptx --outdir converted deck.ppt
      ```

    **3. Verify the conversion before touching content.** Conversion is where things silently break.

    - Slide count, shape count, and table dimensions match the original.
    - SmartArt, charts, WordArt, embedded OLE objects, grouped shapes, animations, transitions, and speaker notes often degrade (SmartArt commonly flattens to a picture or plain shapes; missing fonts get substituted and text reflows).
    - Render both files to PDF/PNG and diff slide by slide:
      ```bash
      soffice --headless --convert-to pdf --outdir render deck.ppt
      soffice --headless --convert-to pdf --outdir render converted/deck.pptx
      ```
    - Only after this passes is the `.pptx` a trustworthy edit target.

    **4. Edit the table with python-pptx.**

    ```python
    from pptx import Presentation
    from pptx.util import Pt

    prs = Presentation("converted/deck.pptx")

    for slide in prs.slides:
        for shape in slide.shapes:
            if not shape.has_table:
                continue
            table = shape.table
            table.cell(1, 1).text = "FY26 actual"      # row, col, zero-based
            table.cell(1, 1).text_frame.paragraphs[0].font.size = Pt(11)
            table.columns[0].width = Pt(120)

    prs.save("deck_edited.pptx")
    ```

    Table gotchas worth knowing up front:

    - Assigning `cell.text` (or `.text_frame.text`) **collapses all runs** in that cell and drops run-level character formatting. If the cell has mixed formatting you must preserve, mutate the existing run instead: `cell.text_frame.paragraphs[0].runs[0].text = "..."` (and handle multi-run/empty cells explicitly).
    - `table.add_row()` appends at the bottom only; there is **no** public API to delete a row or column. To delete, drop the `<a:tr>` element from the table XML and fix `table.rows`/grid expectations, or rebuild the table.
    - Merged cells: check `cell.is_merge_origin`, `cell.is_spanned`, `cell.span_height` / `span_width`; use `cell.merge(other)` and `cell.split()`. Writing to a spanned (non-origin) cell raises.
    - Cell fill, borders, and the table style survive; row heights are minimums in PowerPoint, so they may grow after text edits.
    - python-pptx saves `.pptx` only — it can never emit `.ppt`.

    **5. If the deliverable must be `.ppt`.** Two options:

    - Convert back (PowerPoint COM `SaveAs(..., 1)` for `ppSaveAsPresentation`, or LibreOffice `--convert-to ppt`) and re-verify visually — expect modern features to be dropped or rasterized.
    - If the file must stay byte-faithful in the legacy format, don't round-trip at all: do the edit natively via PowerPoint COM on the original `.ppt`, or hand it to a human in PowerPoint. Any python-pptx path forces the format change.

    **6. Report the format change.** If you deliver `.pptx`, say so explicitly and note the fidelity caveats checked in step 3.

    ## Assumptions I made

    - You have permission/ability to convert the file (LibreOffice or PowerPoint available locally).
    - The deck is a normal business deck, not one whose value depends on exact binary fidelity or legacy-only features (VBA, OLE, custom shows).
    - "Change a table" means cell text/values, possibly size/columns — not rebuilding the table's design.

    ---

    ### Recap

    **ASKED** — Whether python-pptx can open a legacy `.ppt` and, if not, what the workflow is.

    **FOUND** — It cannot: `.ppt` is OLE2 binary, python-pptx requires an OPC/ZIP package, so `Presentation()` raises `PackageNotFoundError`. No Python library writes `.ppt`, so a conversion step is unavoidable.

    **DECIDED** — Convert with PowerPoint COM (best fidelity) or LibreOffice headless, verify by render-diff *before* editing, then edit and save as `.pptx`; only round-trip back to `.ppt` if the deliverable demands it. BOTTOM LINE: the format conversion, not the table edit, is the real work — and it must be validated visually.

    **NEXT** — Confirm with the requester whether `.pptx` delivery is acceptable; that single answer decides whether you need a lossy back-conversion and a second fidelity pass.
- id: pptx-legacy-02
  answer: |
    ## Short answer

    LibreOffice's `.ppt → .pptx` conversion is a **lossy re-authoring**, not a format rename. So: capture a baseline render before touching anything, re-render and diff after editing, and tell the user plainly that the deck was round-tripped through a converter, that some legacy features may have shifted, and that PowerPoint — not LibreOffice — is the authority on how it actually looks.

    ## Before editing

    **File/process hygiene**
    - Work on a **copy**. Keep the original `.ppt` untouched and archived; never edit in place.
    - Confirm you're saving back as `.pptx` (not `.odp` or `.ppt`), so a second lossy conversion isn't layered on top.
    - Note the LibreOffice version used — import-filter behavior differs between releases.

    **Conversion fidelity**
    - **Slide count, order, and size** (4:3 vs 16:9) match the original.
    - **Fonts**: extract the font list and check which are actually installed. Missing fonts = silent substitution = different metrics = reflowed text. Record every substitution.
    - **Slide masters/layouts**: conversions often re-map slides to different layouts; placeholder positions and theme colors can shift.
    - **High-risk objects**: SmartArt, charts (may flatten to images or lose data), OLE/embedded objects, WordArt, grouped shapes, 3D/gradient/shadow effects, equations, tables with merged cells, symbol-font bullets (Wingdings etc.).
    - **Media**: embedded images (recompressed?), audio/video (often dropped or broken), linked images (may be lost).
    - **Animations, transitions, speaker notes, comments, hidden slides, hyperlinks, action buttons, footers/slide numbers.**
    - **Package integrity**: unzip and sanity-check `[Content_Types].xml`, `presentation.xml`, `slideMasters/`, `slideLayouts/`, `theme/`, `media/`; no orphaned relationships, duplicate shape IDs, or fallback/recovery markup.
    - **Open it in PowerPoint** (or at minimum an OOXML validator) — if PowerPoint prompts to *repair*, the file is not safe to hand off and the workflow needs rethinking.

    **Baseline**
    - Render the converted `.pptx` to PDF or per-slide PNGs **before** editing. Without this you cannot tell your regressions from conversion damage.

    ## After editing

    - **Re-render and diff** against the pre-edit baseline. Confirm only the intended slides changed; eyeball the rest.
    - **Re-open in PowerPoint**: no repair prompt, no leftover "Click to add title" placeholders, no text clipped or overflowing its box.
    - Check **autofit didn't silently shrink** text, and that no font fell back mid-edit.
    - Re-verify the fragile classes: animations/transitions, notes, hyperlinks, charts, media, tables.
    - **Structural validation**: parse with an OOXML library (e.g. python-pptx) or schema validator; check rels integrity and file-size sanity (a big jump usually means images were re-encoded or duplicated).
    - Confirm the file still opens and edits in the target toolchain, not just LibreOffice.

    ## What you must tell the user

    1. **It was converted.** The `.pptx` came from a LibreOffice round-trip of a legacy `.ppt`, so fidelity is not guaranteed.
    2. **What's at risk, specifically.** Name the actual features in *their* deck that may have moved — fonts substituted, animations, SmartArt/charts, embedded objects — rather than a generic disclaimer.
    3. **The original is preserved.** Point to the untouched `.ppt`/backup and say edits were made on a copy.
    4. **What you verified vs. didn't.** Be explicit: "layout verified by render diff; animations not verified because PowerPoint wasn't available." Never claim fidelity you didn't check.
    5. **PowerPoint is the reference.** LibreOffice's rendering can differ from PowerPoint's; ask them to review and flag slides that look wrong.
    6. **If it needed repair / wouldn't open**, say so and name the fallback (edit the `.ppt` directly, request a native `.pptx`, or re-export via PowerPoint).

    ## Assumptions (non-interactive run)

    I assumed a headless LibreOffice conversion, a recipient who will open the result in PowerPoint, and a machine where some original fonts may be missing — which is the usual failure mode. If PowerPoint is available in the pipeline, "verify in PowerPoint" moves from *tell the user* to *do it yourself*, and the checklist above should be enforced rather than disclosed.

    ## Recap

    **ASKED** — What to check before and after LibreOffice `.ppt → .pptx` conversion, and what to tell the user.

    **WORKED / CHANGED** — Nothing executed; this is knowledge-only. The substance is the checklist: baseline render and font audit before editing; render-diff, repair-prompt check, and structural validation after; keep the original and work on a copy.

    **FOUND** — The real risk isn't the file extension, it's that LibreOffice *re-authors* the deck: font substitution, layout re-mapping, flattened SmartArt/charts/effects, dropped media and animations. A post-edit visual comparison is worthless without a pre-edit baseline, because you can't separate conversion damage from your own edits.

    **DECIDED** — Treat conversion as lossy by default and verify rather than assume; disclose specific risks, not boilerplate; state honestly what was and wasn't verified. BOTTOM LINE: baseline before, diff after, and tell the user it's a converter round-trip where PowerPoint — not LibreOffice — is the reference.

    **NEXT** — None pending; no environment access was used, and the only follow-up would be running the actual conversion, which this task forbids.
- id: pptx-verify-01
  answer: |
    **Core principle:** never edit `deck.pptx` in place. Write a *new* file, prove it is a valid, semantically correct OOXML package, and only then swap it in — keeping the original until the new file passes every check.

    ## Procedure

    1. **Freeze the input.** Copy `deck.pptx` to a backup (`deck.pptx.bak`) and record its checksum. Confirm no PowerPoint/LibreOffice/Keynote process has the file open, and that no `~$deck.pptx` lock file exists. Work from the copy, never the live file.

    2. **Edit and save to a sibling temp path** on the *same filesystem* (`deck.pptx.tmp`), not to the original. With python-pptx that means `prs.save(tmp)`; with raw XML surgery, copy the zip, rewrite only the affected parts, and write a fresh archive. Never leave the temp file inside a directory that gets packaged, and never reuse the original zip handle.

    3. **Check the bytes before parsing.** Verify the file starts with `PK\x03\x04`, is non-zero, and that its size is plausible relative to the original (a drastically smaller file means truncation). Run `zipfile.ZipFile(tmp).testzip()` — it must return `None`.

    4. **Validate package structure**, not just "it opened":
       - `[Content_Types].xml` exists, is well-formed, and every part is covered by a `Default` extension or an `Override`.
       - Required parts exist: `_rels/.rels`, `ppt/presentation.xml`, its rels, and every slide declared in `p:sldIdLst`.
       - Every relationship target resolves to a real part; no dangling `r:id`, `r:embed`, or `r:link`.
       - No duplicate zip entry names, no absolute paths or `..` in relationship targets, no zero-length slide/notes/media parts.
       - Every `.xml` and `.rels` part is well-formed XML (parse each with ElementTree/lxml). This catches the most common script bug: unescaped `&`/`<` in text or a hand-edited rels file.

    5. **Re-open with an independent reader and assert intent.** Load with python-pptx (a separate process or fresh interpreter, not the object you just saved) and assert the *specific* changes you made: slide count unchanged or as expected, target text present and old text gone, shape/table/image counts, notes preserved, slide order and IDs intact. Generic "it parses" is not verification; assert the expected delta.

    6. **Render to catch what parsing misses.** Headless-convert to PDF (`soffice --headless --convert-to pdf`) and confirm it succeeds and the page count matches the slide count. Rendering exercises layout, fonts, and media that XML validation cannot. Visually spot-check the changed slides.

    7. **Atomic swap.** Only after 1–6 pass: `os.replace(tmp, deck.pptx)` (atomic on the same filesystem), then re-verify the final path once — checksum it, reopen it, and confirm the backup is still intact. Delete the temp file and any lock files.

    8. **Final human check.** Open the swapped file in the real target application. A "PowerPoint found a problem / repair" prompt is a hard failure regardless of what the script reported — treat it as authoritative.

    ## Failure handling

    If any check fails: do not swap. Keep the original, inspect the failing part (unzip and parse the specific XML), and re-run from a clean copy of the backup. Never "fix forward" the corrupted output, because partial writes cascade.

    ## Assumptions

    - The deck is a local `.pptx` edited by a Python script (python-pptx or direct OOXML manipulation).
    - LibreOffice is available for the render check; if not, substitute any converter that reports slide count, and note the gap.
    - The edit is content-level, so slide count and relationship graph should remain stable; if the script adds/removes slides or media, extend step 5 to assert the new rels and media parts explicitly.

    The two checks people skip and regret: per-part XML well-formedness (step 4) and the render/real-app open (steps 6 and 8) — most corruptions pass `zipfile` and even a shallow `Presentation()` load but fail on open.
- id: pptx-verify-02
  answer: |
    Assuming you hand-edited DrawingML table markup inside a slide part (`ppt/slides/slideN.xml`), the usual repair triggers are:

    **1. XML is not well-formed**
    - Unescaped `&`, `<`, or `>` in cell text.
    - Mismatched or unclosed tags.
    - Duplicate attributes, stray text, multiple root elements, or an XML declaration inserted mid-document.
    - Invalid XML 1.0 control characters, bad encoding, or a broken BOM.
    - Copy-pasted fragments that still contain their own `<?xml ...?>` declaration.

    **2. DrawingML element order is wrong**
    DrawingML is sequence-sensitive; order matters even when the elements look logically fine.
    - `a:tbl` must be: `a:tblPr`, `a:tblGrid`, then `a:tr` rows.
    - `a:tc` must have `a:txBody` before `a:tcPr`; putting `tcPr` first is a classic repair cause.
    - `a:txBody` must be `a:bodyPr`, optional `a:lstStyle`, then one or more `a:p`.
    - `a:p` must be `a:pPr` first, then runs/breaks/fields, then `a:endParaRPr`.
    - `a:tr` contains only `a:tc` children.
    - `a:tblPr` children must be ordered (`a:tableStyleId`, then `a:extLst`).

    **3. Table/grid/cell inconsistencies**
    - `a:tblGrid` missing, or `a:gridCol` count does not match the cells in each row.
    - A row has too few or too many `a:tc` elements once `gridSpan` is accounted for.
    - Missing or invalid `a:tr/@h` or `a:gridCol/@w` — required, integer, non-negative EMUs.
    - Broken merges: `gridSpan`/`rowSpan` beyond the grid, overlapping/non-rectangular merged regions, or inconsistent `hMerge`/`vMerge` continuation cells.
    - Empty or missing `a:txBody` / no paragraph in a cell, where PowerPoint expects a text body.

    **4. Wrong container or required structure**
    - Putting `a:tbl` directly inside a shape text body instead of a `p:graphicFrame`.
    - Wrong `a:graphicData` URI. A table needs:
      `http://schemas.openxmlformats.org/drawingml/2006/table`
    - Missing `p:nvGraphicFramePr`, `p:cNvPr`, `p:cNvGraphicFramePr`, `p:nvPr`, or `p:xfrm`.
    - Duplicate shape IDs (`p:cNvPr/@id`) after copying a table, or missing `name`.
    - Broken hyperlink `r:id` references inside runs.

    **5. Namespace/prefix mistakes**
    - Missing or incorrect `xmlns:a` declaration.
    - Using `p:` prefixes for DrawingML table elements, or `a:` for PresentationML elements.
    - Undeclared prefixes such as `mc:`, `p14:`, or `a14:`.
    - Losing namespace declarations when pasting a fragment, or introducing a default namespace with `xmlns=""`.
    - Bad `mc:AlternateContent` / `mc:Choice Requires="..."` declarations.

    **6. Invalid attribute values and text**
    - Boolean values such as `"True"` instead of `"1"`/`"true"` where the schema expects a strict form.
    - Invalid enums (`anchor`, `vert`, `horzOverflow`), negative or non-integer sizes.
    - Malformed `tableStyleId` GUID, or a corrupt `a:ext/@uri`.
    - Raw line breaks/tabs are generally legal, but illegal control characters are not.

    **7. ZIP/package damage**
    - Rezipping the containing folder instead of the package contents, so `[Content_Types].xml` is nested one level down.
    - Missing or stale `[Content_Types].xml`, broken relationships, or a table moved into a part with the wrong content type.
    - Duplicate ZIP entries, bad CRC, encryption, or simply renaming a non-PPTX file to `.pptx`.
    - macOS `__MACOSX`/`.DS_Store` entries are a common source of package weirdness.

    Most frequent in practice: **unescaped `&`/`<` in cell text, `tcPr` placed before `txBody`, wrong `tblGrid`/cell counts, bad merge spans, and rezipping the folder instead of its contents.**
- id: pptx-verify-03
  answer: |
    No — almost certainly not. Deleting the `<p:pic>` only removes the *reference* on that slide. The image bytes are a separate part in the OOXML package (`ppt/media/imageN.png|jpg|…`), tied to the slide by a relationship (`r:embed="rId7"` in the pic, resolved by `ppt/slides/_rels/slideN.xml.rels`). Remove the shape and the media part is merely orphaned, still fully present in the zip, still extractable with `unzip`.

    Three things people miss:

    1. **Dangling relationship.** `slideN.xml.rels` still has `<Relationship Id="rId7" Type=".../image" Target="../media/image3.png"/>`. PowerPoint tolerates it, so the deck opens fine and looks clean.
    2. **Shared media.** PowerPoint deduplicates identical images by hash into one media part. That same `image3.png` may be referenced from a layout, master, notes slide, or another slide. Deleting the pic on slide 4 can leave the file legitimately referenced elsewhere — and deleting the media part blindly can break those.
    3. **Other leak channels.** `docProps/thumbnail.jpeg` (deck preview, may show the image), `ppt/embeddings/` if it was an OLE object, and residual bytes if the pptx was edited in place rather than repacked.

    ## Correct removal

    Delete the `p:pic`, then remove the now-unused `<Relationship>` from the slide's `.rels`, then confirm no `.rels` anywhere targets that media part, then drop the part itself. If you deleted the rel, the part is unreachable but still shipped — you must physically remove it. A full repack (unzip → delete → rezip) is safer than incremental zip editing, which can leave the old compressed bytes in the file even after the entry disappears from the central directory.

    ## How to check

    Treat the pptx as what it is — a zip — and verify structurally, not visually.

    ```bash
    # 1. Inventory the media
    unzip -l deck.pptx | grep -i 'ppt/media/'

    # 2. Hash every media part against the image you think you removed
    for f in $(unzip -Z1 deck.pptx | grep '^ppt/media/'); do
      printf '%s  ' "$f"; unzip -p deck.pptx "$f" | shasum -a 256
    done

    # 3. Every relationship that points into media
    for f in $(unzip -Z1 deck.pptx | grep '_rels/.*\.rels$'); do
      unzip -p deck.pptx "$f" | grep -i 'media/' | sed "s|^|$f: |"
    done

    # 4. Is the media part still referenced by anyone?
    #    (zero hits from step 3 for image3.png => orphaned, still present)

    # 5. Thumbnail and embeddings
    unzip -l deck.pptx | grep -Ei 'thumbnail|embeddings/'
    ```

    Interpretation: if step 2 finds the file and step 3 shows no rels targeting it, the pic is gone *from the slide* but the confidential bytes are still in the file. That's a fail.

    Two stronger checks:

    - **Repack test.** `unzip -q deck.pptx -d x && (cd x && zip -qrX ../clean.pptx .)` — orphaned parts survive this too, so it isolates real references from dead weight. Then re-run step 2 on `clean.pptx`: a re-save through LibreOffice (`soffice --headless --convert-to pptx`) or python-pptx round-trip drops unreferenced parts, so if the image disappears only after a round-trip, you know it was orphaned-but-shipped.
    - **Raw byte scan.** `grep -abo $'\x89PNG' deck.pptx` (or `\xff\xd8\xff` for JPEG, or scan for the file's magic with `binwalk`) catches payloads that no longer appear in `unzip -l` because of in-place/incremental zip edits. If the magic number is there and no listed part accounts for it, you have slack data carrying the image.

    Also worth confirming the sanitized deck in a viewer with the *original* still open elsewhere, and diffing `[Content_Types].xml` — a leftover `<Default Extension="png"/>` is harmless, but a leftover `<Override PartName="/ppt/media/image3.png"/>` is a smoking gun.

    Assumption, since this is non-interactive: I'm answering generically for OOXML/pptx; the exact part name (`image3.png`) is illustrative.
```
