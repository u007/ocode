# deepseek-v4.1-flash — pptx answers WITH SKILL (validation of the re-derived skill)

Model: `opencode-go/deepseek-v4.1-flash` (provider-stripped `deepseek-v4.1-flash`),
re-run 2026-10-21 after opencode-go updated the model behind this id
(`model_version: "4.1-2026-10-21"`). This is with-skill ITERATION 2 (skill edited after iter1: verify-safety section dropped, 4:3 default fact added; iter1 kept as `.iter1.with-skill.md`) — the with-skill run for this id (any earlier one validated the previous model);
the previous answers are in git history.

Closed-book barrier:
- Each of the 27 questions in `docs/okf/_prompts/pptx.md` was a fresh headless
  `ocode run -m opencode-go/deepseek-v4.1-flash -effort med` session (`bin/ocode`, built
  2026-10-20) under ocode's real agent system prompt.
- Fake HOME: no global CLAUDE.md/AGENTS.md, no memory, no persisted roots. Its only config
  denies every builtin tool and DISABLES the five existing `*-tuning-deepseek-v4.1-flash`
  Kaizen skills (plus `conduct-tuning-deepseek-v4-flash`), so the ONLY skill content the
  model saw is the prepended pptx skill body (no double injection). Probe before the sweep confirmed: no tools advertised, no Model-Specific
  Directives block. The generic `[ocode:provider]` DeepSeek output-discipline block and the
  bundled skill-catalogue digest were still present (every user gets them).
- cwd: an empty dir named `work`; stdin `/dev/null`.
- Audit: every session sqlite checked after the sweep — 1 user + 1 assistant message,
  zero tool calls.
- Prompt per question: the body of docs/okf/pptx/derived/pptx.deepseek-v4.1-flash.SKILL.md (front matter stripped, as "Active guidance") + closed-book instruction + the one question; plain-text reply,
  wrapped into YAML below verbatim.

```yaml
- id: pptx-model-01
  answer: |
    ## Where the table lives

    A `.pptx` is an OPC zip. There is **no separate part for a table** (unlike charts, which get `ppt/charts/chartN.xml`). The table is stored inline as a DrawingML graphic in the slide part, `ppt/slides/slideN.xml`, inside the shape tree:

    ```
    p:sld → p:cSld → p:spTree → p:graphicFrame
    ```

    Unlike a `p:sp` (which carries its own `p:txBody`), a table hides its payload under the generic graphic wrapper:

    ```
    p:graphicFrame
    ├── p:nvGraphicFramePr        (cNvPr id/name, cNvGraphicFramePr, nvPr)
    ├── p:xfrm                    (a:off x/y, a:ext cx/cy — position + overall size)
    └── a:graphic
        └── a:graphicData  uri="http://schemas.openxmlformats.org/drawingml/2006/table"
            └── a:tbl            ← the table itself
    ```

    The `uri` on `a:graphicData` is what marks it as a table rather than a chart (`.../chart`) or SmartArt (`.../diagram`).

    ## Inside `a:tbl`

    ```xml
    <a:tbl>
      <a:tblPr firstRow="1" bandRow="1">
        <a:tableStyleId>{5C22544A-7EE6-4342-B048-85BDC9FD1C3A}</a:tableStyleId>
      </a:tblPr>

      <a:tblGrid>                        <!-- COLUMNS -->
        <a:gridCol w="1524000"/>
        <a:gridCol w="1524000"/>
      </a:tblGrid>

      <a:tr h="370840">                  <!-- ROW (one per row) -->
        <a:tc>                           <!-- CELL -->
          <a:txBody>                     <!-- TEXT -->
            <a:bodyPr/>
            <a:lstStyle/>
            <a:p>
              <a:r>
                <a:rPr lang="en-US" sz="1800" b="1"/>
                <a:t>Gadget</a:t>
              </a:r>
            </a:p>
          </a:txBody>
          <a:tcPr marL="91440" marR="91440" marT="45720" marB="45720" anchor="ctr"/>
        </a:tc>
        ...
      </a:tr>
      ...
    </a:tbl>
    ```

    Element roles:

    | Level | Element | Holds |
    |---|---|---|
    | Table | `a:tbl` | the whole table: properties, grid, rows |
    | Properties | `a:tblPr` | style flags (`firstRow`, `firstCol`, `lastRow`, `lastCol`, `bandRow`, `bandCol`), `a:tableStyleId` GUID pointing into `ppt/tableStyles.xml` |
    | Columns | `a:tblGrid` → one `a:gridCol` per column | column width in **`w`** (EMU) |
    | Rows | one `a:tr` per row | row height in **`h`** (EMU); children are `a:tc` |
    | Cells | `a:tc` | attributes `gridSpan`, `rowSpan`, `hMerge`, `vMerge`; two children: `a:txBody` and `a:tcPr` |
    | Cell formatting | `a:tcPr` | margins (`marL/marR/marT/marB`), `anchor`, fill (`a:solidFill`/`a:gradFill`/`a:noFill`), borders (`a:lnL`, `a:lnR`, `a:lnT`, `a:lnB`), optional `a:headers` |
    | Cell text | `a:txBody` | `a:bodyPr`, `a:lstStyle`, then `a:p` paragraphs — the same text model as any shape |
    | Text detail | `a:p` → `a:pPr`, `a:r`, `a:br`, `a:fld`, `a:endParaRPr` | paragraph props (alignment, indent, bullets) |
    | Runs | `a:r` → `a:rPr` + `a:t` | run props (`lang`, `sz`, `b`, `i`, `u`, typefaces via `a:latin`, color via `a:solidFill`) and the literal characters in `a:t` |

    Key consequences:

    - **Text is never an attribute of the cell.** It always goes `a:tc/a:txBody/a:p/a:r/a:t`. An "empty" cell still has `a:txBody` with at least one `a:p` (often containing only `a:endParaRPr`).
    - **Column count = `len(a:tblGrid/a:gridCol)`**, not the length of any row. Merges are expressed on cells, not by removing grid columns.
    - **Merges are flags on existing cells**, not removed cells: the origin cell gets `gridSpan="2"` / `rowSpan="2"`, and the cells it covers remain in the XML as `a:tc` with `hMerge="1"` or `vMerge="1"` and empty text bodies.
    - **Units are EMU** (914400 per inch). `a:tr/@h` and `a:gridCol/@w` are the authoritative per-row and per-column geometry.
    - There is **no `a:tbl`-level row/column index**; order is document order. Rows and cells are positional, which is why locating a row must be done by matching text content, not by index or `next(...)`.
    - **`p:xfrm/a:ext/@cy` is a separate stored number** from the sum of `a:tr/@h`. PowerPoint generally keeps them in agreement, but editing `a:tr` elements directly does *not* update `a:ext`, so the frame can go stale and overlap shapes below — the overall height has to be written explicitly after a row insert/delete.

    ## python-pptx mapping

    `GraphicFrame.table` → `Table`; `table.columns[i]` → `_Column` (`gridCol/@w`); `table.rows[i]` → `_Row` (`tr/@h`); `cell.text_frame` → `TextFrame` over `a:txBody`; `cell.fill` / `cell.margin_*` / `cell.vertical_anchor` → `a:tcPr`. In python-pptx 1.0.2 there is **no `table.add_row()` or `add_column()`** — those objects are read-only for structure, so adding a row means `copy.deepcopy(ref_tr)` + `ref_tr.addnext(new_tr)` on the lxml tree, then fixing text, banding fills, the Total, and `graphic_frame.height`.

    Namespaces: `a` = `http://schemas.openxmlformats.org/drawingml/2006/main`, `p` = `.../presentationml/2006/main`, `r` = `.../officeDocument/2006/relationships`.

    **Caveat stated up front:** this is from memory of the DrawingML/OPC spec and python-pptx's object model, not checked against a file in this run. The element hierarchy and attribute names above are the stable parts; exact default EMU values and style GUIDs vary by deck.
- id: pptx-model-02
  answer: |
    **Units: EMU (English Metric Units)**

    Positions and sizes in python-pptx and DrawingML are stored as **EMUs**, a 64-bit integer unit defined so that both the metric and imperial systems convert exactly:

    - 1 inch = **914,400** EMU
    - 1 point = **12,700** EMU (1/72 in)
    - 1 cm = **360,000** EMU
    - 1 mm = 36,000 EMU

    python-pptx exposes these as `Length`, an `int` subclass, so `shape.left`, `shape.top`, `shape.width`, `shape.height`, `prs.slide_width`, `prs.slide_height`, and `font.size` are all EMU integers that carry helpers like `.inches`, `.pt`, `.cm`, and `.emu`. Constructors `Emu()`, `Inches()`, `Pt()`, `Cm()`, `Mm()` are provided. In the XML this appears as `a:xfrm/a:off` (`x`, `y`) and `a:xfrm/a:ext` (`cx`, `cy`), all in EMU (schema type `ST_Coordinate` / `ST_PositiveCoordinate`).

    **Default 16:9 slide dimensions**

    A 16:9 deck is **12,192,000 × 6,858,000 EMU**, i.e. **13.333 in × 7.5 in** (33.87 cm × 19.05 cm), ratio exactly 16:9.

    Caveat worth stating: python-pptx's own `Presentation()` template is **4:3 — 9,144,000 × 6,858,000 EMU (10 in × 7.5 in)**. So "default 16:9" refers to the standard PowerPoint widescreen template, not to a fresh python-pptx `Presentation()`. Always read `prs.slide_width` / `prs.slide_height` from the actual deck rather than assuming either size.
- id: pptx-model-03
  answer: |
    ## Where it is

    A shape that "shows up on every slide" but is absent from `slide.shapes` isn't in the slide part at all. It lives in an inherited part, in one of three places:

    1. **The slide master** — `ppt/slideMasters/slideMaster1.xml`, shape tree at `p:cSld/p:spTree`. This is the usual answer, because a master is shared by every slide (typically the whole deck).
    2. **Each slide layout** — `ppt/slideLayouts/slideLayoutN.xml`. This only looks deck-wide if every slide uses a layout that carries its own copy (e.g., the logo was pasted onto each layout rather than the master).
    3. **A background fill** — `p:bg/p:bgPr/a:blipFill` on the master, a layout, or a slide. This is not a shape at all, so `slide.shapes` can never see it, and no shape API will find it.

    If it's grouped on the master, you also need to recurse `GroupShape.shapes`.

    ```python
    prs   = Presentation("deck.pptx")
    slide = prs.slides[0]

    master = slide.slide_layout.slide_master     # or prs.slide_masters[0]
    for shp in master.shapes:                    # master shape tree
        ...                                      # recurse groups; look for p:pic / p:sp

    # background-borne logo (not a shape):
    bg = master._element.find(qn("p:cSld") + "/" + qn("p:bg"))
    ```

    `slide.shapes` misses it because python-pptx performs **no inheritance resolution**: it gives you the XML children of one part's `spTree` and nothing else. It has no "effective/rendered shape" view that flattens master + layout + slide. That's also why a slide-level placeholder appears in `slide.shapes` while the visual it inherits does not.

    ## Consequences of editing it there

    **Scope is global, not per-slide.** One edit on the master changes the logo on every slide using that master — at once, with no per-slide opt-out. To vary it for one slide you must add a slide-level picture/shape (or use a different layout/master); you cannot "edit the logo on slide 3."

    **Loops break in both directions.** A script that iterates `prs.slides` and edits "the logo" will touch the *same shared shape* N times — usually harmless but idempotent — and a script that deletes it will succeed on the first pass and then fail or raise on the next, because the shape is gone for all slides. Any fix must move out of the per-slide loop into a single edit of the owning part.

    **Z-order is fixed and unfavourable.** Master shapes render behind layout shapes and behind all slide shapes. A full-bleed picture, a filled rectangle, or a table on a slide will bury the master logo. You cannot bring the master logo forward relative to slide content; the only way to be "in front" is to add the shape to each slide (or layout).

    **Overrides silently shadow your change.** A slide or layout that redefines the same placeholder (own `xfrm`, own text/fill, own `idx`) keeps its values, so editing the master can be invisible on exactly those slides. Conversely, editing a layout only affects slides bound to *that* layout.

    **`showMasterSp` can hide it.** `p:sld` and `p:sldLayout` carry `showMasterSp="0"` (the UI's "Hide background graphics"). It's not a first-class python-pptx property — you'd read/write the XML attribute — and it means "appears on every slide" isn't even structurally guaranteed. Some slides may legitimately not render the master at all.

    **Multi-master decks.** With several masters, a logo on master 1 doesn't exist on slides from master 2. "Every slide" may really mean "every slide of one master"; editing one master leaves the rest untouched.

    **Relationships and media ownership shift.** The picture's relationship lives in the *master part's* `.rels`, not the slide's. `slide.part.drop_rel(rId)` is meaningless here; you need `slide_master.part.drop_rel(...)`. And `drop_rel` counts only `//@r:id`, while pictures use `a:blip/@r:embed` — that count is always 0, so the rel is always dropped even if another picture in the master still uses that rId. Check `//@r:embed` (`//@r:link`) in the master part yourself before dropping. Conversely, a rel you *don't* drop keeps the old image alive in `ppt/media/` after you repoint or delete the `p:pic`, because python-pptx saves every part reachable by a rel. And because image parts are deduped package-wide, several parts' rels can point at the same media — deleting one part's rel only removes the file when nothing else references it.

    **Background-borne logos need a different API entirely.** There's no shape to mutate; you touch `p:bg/p:bgPr/a:blipFill` (or `_BaseSlide.background` / `follow_master_background`), and the image is painted in the background layer rather than the shape layer — so a shape-level edit targeting it does nothing at all.

    **Blast radius.** You're mutating a part shared by the whole deck. A half-finished edit or a broken rId corrupts the logo everywhere at once, and it won't be recoverable by undoing "on one slide."

    ## Assumption stated

    I'm assuming "appears on every slide" means inherited illustration from the master (the common case), and that by "editing it there" you mean editing the shared master/layout part programmatically. If the logo is actually on each layout, every consequence above holds but is scoped per layout instead of per deck.
- id: pptx-locate-01
  answer: |
    ## Locate the table: match by header, prove uniqueness

    ```python
    from pptx import Presentation
    from pptx.enum.shapes import MSO_SHAPE_TYPE

    def iter_shapes(shapes):
        """Walk groups too — a table can be nested inside a GroupShape."""
        for sh in shapes:
            if sh.shape_type == MSO_SHAPE_TYPE.GROUP:
                yield from iter_shapes(sh.shapes)
            else:
                yield sh

    prs = Presentation("deck.pptx")
    slide = prs.slides[2]          # slide 3, zero-based, presentation order

    EXPECTED_HEADER = ["Item", "Qty", "Unit", "Total"]

    candidates = [
        sh for sh in iter_shapes(slide.shapes)
        if sh.has_table
        and [c.text_frame.text.strip() for c in sh.table.rows[0].cells] == EXPECTED_HEADER
    ]
    assert len(candidates) == 1, f"expected 1 table, found {len(candidates)}"
    gf = candidates[0]             # the p:graphicFrame
    table = gf.table
    ```

    Why not `next(sh for sh in slide.shapes if sh.has_table)`: a slide can hold more than one table, and the first `has_table` shape in z-order is not "the table". Also, `slide.shapes` does not recurse into groups, so a flat scan can miss it entirely. Zero matches or several matches = stop and ask; never guess.

    Do not key off `sh.name` or `shape_id` — both are author- and re-save-dependent.

    ## Locate the row: exact stripped cell text, one hit

    ```python
    # find the label column from the header instead of hardcoding 0
    col = EXPECTED_HEADER.index("Item")

    hits = [
        r for r in table.rows
        if r.cells[col].text_frame.text.strip() == "Gadget D"
    ]
    assert len(hits) == 1, f"expected 1 'Gadget D' row, found {len(hits)}"
    row = hits[0]
    ```

    `table.rows` includes the header row and the Total row, so scope the match to the label column and compare the whole stripped cell — not `in`/`startswith`, which will also hit "Gadget D Pro". Same rule as the table: exactly one hit, otherwise stop.

    Useful handles on the found row: `row.cells[i]`, `row.height`, and `row._tr` for the underlying `a:tr` (needed if you insert/delete).

    ## If you are about to edit that row

    python-pptx 1.0.2 has no `table.add_row()` / `delete_row()`; `Table`, `table.rows`, `_Row` are read-only. Insert with:

    ```python
    import copy
    new_tr = copy.deepcopy(row._tr)
    row._tr.addnext(new_tr)        # or total_tr._tr.addprevious(new_tr) for "insert above Total"
    # fill: for each new cell, set runs[0].text and drop the extra runs
    ```

    Then the insert is not finished until you:

    1. recompute the Total (and any subtotals) from the data rows;
    2. set `gf.height = sum(r.height for r in table.rows)` — the `p:xfrm/a:ext cy` does not follow XML row edits;
    3. check `gf.top + gf.height` against `prs.slide_height` and the shapes below (move them or ask);
    4. re-apply explicit `a:tcPr` banding fills if the table uses them.

    Read the deck for its size (`prs.slide_width` / `prs.slide_height`); 16:9 is typically 12192000 × 6858000 EMU, and a bare `Presentation()` template is 4:3, so never assume.

    Assumption: the table is on the third slide in presentation order and the header row is row 0 (no merged banner row above it). If the deck has a title band inside the table, match the header on the first row that has the expected width, not blindly on index 0.
- id: pptx-locate-02
  answer: |
    ## How the merge is represented

    OOXML (DrawingML) tables have no row/column merge object. A table is `p:graphicFrame/a:graphic/a:graphicData/a:tbl`, with a fixed `a:tblGrid` of `a:gridCol` widths, and **every `a:tr` contains exactly one `a:tc` per grid column** — merged or not. The merge is expressed purely by attributes on the `a:tc` elements (and `a:tcPr` for the cell properties):

    ```xml
    <a:tr>
      <a:tc gridSpan="2">            <!-- origin cell: content lives here -->
        <a:txBody><a:p><a:r><a:t>Total</a:t></a:r></a:p></a:txBody>
        <a:tcPr/>
      </a:tc>
      <a:tc hMerge="1">              <!-- covered/spanned cell: still present -->
        <a:txBody><a:p/></a:txBody>
        <a:tcPr/>
      </a:tc>
      <a:tc>                         <!-- the remaining column, normal cell -->
        <a:txBody><a:p><a:r><a:t>1,234</a:t></a:r></a:p></a:txBody>
        <a:tcPr/>
      </a:tc>
    </a:tr>
    ```

    - `gridSpan="n"` on the **origin** cell (top-left of the merged region) says it covers `n` columns. `rowSpan="n"` is the vertical analogue.
    - Every other cell in the region carries `hMerge="1"` (horizontal) and/or `vMerge="1"` (vertical). These cells are *not* deleted; they stay in the XML as empty placeholders.
    - In python-pptx this is surfaced as `cell.is_merge_origin`, `cell.is_spanned`, `cell.span_width` / `cell.span_height`, plus `cell.merge(other)` (origin → bottom-right corner, rectangular regions only) and `cell.split()`. Grid indices never collapse: `table.cell(r, 1)` still exists and returns the spanned placeholder.
    - `gridSpan`/`rowSpan` are static integers. There is no automatic reflow.

    ## What to watch when editing around it

    1. **Write only to the origin.** Setting text/fill on an `hMerge`/`vMerge` cell is invisible at best and produces odd rendering or repair prompts at worst. The cleanest rule: if `cell.is_spanned`, skip it.
    2. **`runs[0]` can blow up on spanned cells.** Covered cells usually hold `<a:p/>` with no `a:r`, so `cell.text_frame.paragraphs[0].runs[0].text = ...` raises `IndexError`. Guard with `_spanned` checks or use `cell.text`.
    3. **Column arithmetic misleads you.** For a Total row spanning cols 0–1 of a 3-column table, `cells[0].text == "Total"`, `cells[1].text == ""`, `cells[2].text == value`. Index still equals grid column, but any "read the value in column k" logic, or any loop that assumes each cell holds that column's data, silently reads empties. Identify the Total row by the origin cell's text only.
    4. **Width/height of a merged cell.** Be careful: depending on version, a cell's reported `width`/`height` may be the originating `gridCol` width rather than the sum of the spanned columns. Verify against `span_width`/`span_height` and the `tblGrid` before relying on it for layout math. (I'm not fully certain of python-pptx's exact behavior here across versions — treat it as something to check, not assume.)
    5. **No row/column deletion API.** python-pptx 1.0.x has no `add_row`, `remove_row`, `add_column`, or `remove_column`; rows are added by `copy.deepcopy(ref_tr)` + `tr.addnext(...)`. Consequences:
       - **Vertical merges:** `rowSpan` is a literal. If you insert a row *inside* a vertical span, you must increment the origin's `rowSpan` and add a `vMerge="1"` cell at the right position in the new `a:tr`; if you delete a row inside it, decrement and remove that placeholder. Skip this and PowerPoint offers to repair the file.
       - **Horizontal merges:** a copied row inherits `gridSpan`/`hMerge`. If the new row should be normal cells, strip those attributes (and set each cell's text through its own first run, deleting extra runs).
       - **Deleting a spanned cell alone** breaks the invariant "cells per row == grid columns". Deleting a logical column means editing every `a:tr`, adjusting `gridSpan` sums and the `a:gridCol` list consistently — or, much safer, `cell.split()` first, then restructure.
    6. **Borders and fills.** Interior gridlines aren't drawn across a merged region, but explicit `a:tcPr` borders on the covered cell can still produce a stray line inside the merge. Fill/alignment are effectively taken from the origin; if the table uses manual banding, re-alternate the `tcPr` fills after any row insert so the Total row doesn't inherit a body-band fill.
    7. **Frame geometry after row changes.** Inserting/deleting an `a:tr` does not move `p:xfrm/a:ext cy`. Recompute the Total, set `graphic_frame.height = sum(r.height for r in table.rows)`, then check `top + height` against `prs.slide_height` (read it — 16:9 decks are commonly 12192000×6858000, and python-pptx's default template is 4:3) and against shapes below; move them or ask.
    8. **Totals.** Because the label cell is merged, recomputing totals by walking `row.cells` must target the explicit value cell (last column, usually `gridSpan="1"`), not "the cell after the label" — the cell after the label origin is the spanned placeholder.

    ### Assumptions stated
    - The deck is a standard DrawingML table inside `p:graphicFrame` (not a picture of a table, not a legacy VML table).
    - The merge is horizontal on the Total label; the Total *value* cell is a normal single-grid-column cell.
    - Points 4 and 6 involve behavior I'd confirm against the actual file and python-pptx version rather than assert; everything about `gridSpan`/`hMerge`/`vMerge` representation and the `rowSpan` maintenance burden is standard OOXML and not version-dependent.
- id: pptx-rowdel-01
  answer: |
    ## Short answer

    **No.** python-pptx has no `table.rows.remove()`, no `table.rows.delete()`, no `_Row.delete()`, and (in the 1.0.x line) no `add_row()`/`add_column()` either. `Table.rows` is read-only: `len()`, indexing, iteration, and `_Row.height`/`_Row.cells` are all you get. Row insert/delete is XML surgery on the `a:tbl` element.

    ## How to actually do it

    The table's XML element (`graphic_frame._element.graphic.graphicData.tbl`) is an lxml element, and each `a:tr` is its child. So remove the row element from its parent:

    ```python
    import copy
    from pptx import Presentation

    prs = Presentation("deck.pptx")
    slide = prs.slides[3]

    # 1. Find the RIGHT table by header text, recursing into groups.
    def iter_frames(shapes):
        for sh in shapes:
            if sh.shape_type == 6 or hasattr(sh, "shapes"):   # MSO_SHAPE_TYPE.GROUP
                yield from iter_frames(sh.shapes)
            elif sh.has_table:
                yield sh

    HEADER = ["Item", "Qty", "Price"]
    cands = [f for f in iter_frames(slide.shapes)
             if [c.text_frame.text.strip() for c in f.table.rows[0].cells] == HEADER]
    assert len(cands) == 1, f"expected 1 table, got {len(cands)}"
    gf = cands[0]
    tbl = gf.table
    tbl_el = gf._element.graphic.graphicData.tbl   # the <a:tbl>

    # 2. Find the row by exact content in the label column; prove uniqueness.
    hits = [r for r in tbl.rows if r.cells[0].text_frame.text.strip() == "Gadget D"]
    assert len(hits) == 1, f"expected 1 match, got {len(hits)}"
    row = hits[0]

    # 3. Delete: detach the <a:tr> from its parent.
    tr = row._tr
    tr.getparent().remove(tr)

    # 4. Frame does NOT follow XML edits — resync it.
    gf.height = sum(r.height for r in tbl.rows)

    # 5. Keep it inside the slide / clear of what's below.
    bottom = gf.top + gf.height
    assert bottom <= prs.slide_height, (bottom, prs.slide_height)
    ```

    Equivalent one-liner once you have the element: `tbl_el.remove(tr)`.

    ## The follow-ups people forget

    Deleting the `a:tr` only removes that row's XML. Four things do **not** happen automatically:

    1. **`p:graphicFrame/p:xfrm/a:ext cy` stays at the old height.** Always finish with `gf.height = sum(r.height for r in gf.table.rows)`, then check `gf.top + gf.height` against `prs.slide_height` and any shapes sitting below the table (move them, or stop and ask).
    2. **Totals go stale.** If the table has a Total/subtotal row, recompute it from the surviving rows.
    3. **Explicit `a:tcPr` banding breaks.** If the table uses hard-coded alternating fills rather than a table style, re-apply the banding so the stripes stay alternating.
    4. **`row.height` of neighbours is untouched** — that's fine for a delete (rows keep their own heights), unlike an insert where you must copy a reference row's height.

    ## What is *not* the answer

    - `table.rows.remove(row)` → `AttributeError`.
    - `del table.rows[i]` → not supported; `_Rows` doesn't implement deletion.
    - `row._element.getparent().remove(...)` is the same thing as above, just more verbose — both work because `_Row._tr` is the `a:tr`.
    - Deleting the `p:graphicFrame` deletes the **whole table**, not a row.

    One caveat if the deck is shared: if any shape or relationship targets that specific `a:tr` (rare — e.g. some accessibility/animation references), the reference dangles after removal. For ordinary table rows there is no such rel, so a bare `remove(tr)` is correct.
- id: pptx-rowdel-02
  answer: |
    **Short answer: it doesn't change the rendered cells, but yes it matters — and you fix it by recomputing the frame height from the surviving rows.**

    ## Why the height goes stale

    `p:graphicFrame/p:xfrm/a:ext/@cy` is a *cached* bounding box. python-pptx (1.0.2) has no row-delete API, so you delete the `a:tr` directly out of the XML:

    ```python
    tbl = graphic_frame.table
    tbl._tbl.remove(tr)          # lxml remove on the <a:tr>
    ```

    That mutates `a:tbl/a:tr` only. Nothing recalculates `a:ext/@cy`, so the frame keeps the old, taller extent.

    ## Why it matters

    - Anything that reads `graphic_frame.height` — your own collision/overlap checks, anchoring, "is it still on the slide" assertions, downstream shape placement — gets a wrong number.
    - The frame can now extend past `prs.slide_height` (or overlap the shapes below) *on paper* even though the visible table ends higher.
    - Some renderers/exports use the frame extent for clipping and hit-boxes; PowerPoint tends to re-layout the table from its rows, but you should not depend on that, and non-PowerPoint consumers (LibreOffice, PDF export, thumbnailers) vary.
    - If the table has a "Total" row, the value is now wrong too — that's a separate recompute.

    ## The fix

    After the `remove`, resync the frame from the rows:

    ```python
    gf = graphic_frame
    gf.height = sum(r.height for r in gf.table.rows)   # EMU; writes a:ext/@cy
    ```

    `r.height` reads each `a:tr/@h`, so the sum is the true new table height. Then finish the delete properly:

    1. Recompute Total/subtotal cell text from the remaining rows.
    2. `gf.height = sum(r.height for r in gf.table.rows)` (above).
    3. Check `gf.top + gf.height` against `prs.slide_height` and the shapes below — move them or ask, don't silently slide off the page.
    4. If the table uses explicit `a:tcPr` fill banding (alternating row colors), re-alternate the fills by new row index, since removing a row shifts parity.

    Same rule applies to row inserts (deepcopy + `addnext`): the `a:ext` never follows XML edits, so `graphic_frame.height = sum(r.height for r in table.rows)` is the mandatory last step either way.

    *(Assumption: "removing an `a:tr`" means direct XML removal, since python-pptx exposes no `table.remove_row()`.)*
- id: pptx-rowdel-03
  answer: |
    ## Short answer

    Alternating shading is only sometimes a *property of the row's position*. If the deck relies on the table style's banding (`a:tblPr/@bandRow` + a style with `band1Horz`/`band2Horz`), the renderer recomputes the stripe from each row's current index, so a deleted row just shifts the phase — no two neighbours collide. If the deck has the stripes **baked into each cell** as explicit `a:tc/a:tcPr/a:solidFill`, the fills are attached to cells, not to positions: the rows below slide up carrying their old colours, and wherever the old and new neighbours happen to match you get two identical adjacent rows.

    ## The mechanisms that differ between decks

    1. **Style-driven vs. baked banding.** Check `a:tbl/a:tblPr`: `bandRow="1"` with a valid `a:tableStyleId` = declarative, self-healing. Lots of `<a:tcPr><a:solidFill>` entries in the rows = baked, breaks on any structural edit unless you re-alternate manually. Decks produced by some exporters (Keynote, Google Slides, older tooling) come out pre-flattened, which is exactly the "one deck not another" split.

    2. **The style doesn't actually resolve.** If `ppt/tableStyles.xml` lacks the `tableStyleId` the table references (lost on a round-trip, or a hand-built file), PowerPoint falls back to a default/plain style. Banding then either disappears entirely or comes only from the baked fills — so the same deletion looks different in two decks with identical-looking tables.

    3. **`bandRow="0"` (or `firstRow`/`firstCol` flags) differs.** Some tables get their stripes purely because a human painted them; the style contributes only borders. Then there is no re-indexing at all.

    4. **Partial overrides.** Decks often bake a fill on *some* rows (a highlighted row, a manually shaded total). Deleting a middle row can put a baked row next to another baked row of the same colour even though the rest of the table still bands via the style.

    5. **First-row/header offset conventions.** With `firstRow="1"`, PowerPoint excludes the header from the band count; LibreOffice/Keynote-ish renderers may count from row 1. That's a parity/offset difference, though by itself it usually shifts the phase rather than duplicating a colour.

    6. **Merged cells.** A vertical merge (`rowSpan`, or a `gridSpan`/`vMerge` pattern) can make the renderer paint the continuation row with the origin row's fill, or with no fill. Delete a row and the merge spans the wrong rows, producing two same-coloured neighbours in one deck and not another depending on whether that table had merges.

    7. **The edit script itself.** A naive `copy.deepcopy(ref_tr)` + `addnext` copies the reference row's `tcPr` fills too, so the inserted/neighbouring row inherits a duplicate colour. Also, a fixer that re-alternates only the rows it touched leaves the parity broken elsewhere.

    ## What to do about it

    - Inspect `a:tblPr` attributes and whether any row contains `a:tcPr/a:solidFill`. If the table is style-banded, don't touch fills at all and don't set `bandRow="0"` — just delete the `a:tr`; the renderer re-stripes.
    - If the deck bakes fills, re-apply the alternation **explicitly after** the delete, by current row index (skip the header if it's first): clear `tcPr` fill on every body cell, then paint `band1`/`band2` on alternating rows. Keep the row heights/Total/frame sync unchanged (row delete needs `gf.height = sum(r.height ...)` regardless).
    - Pick one model per table: either `bandRow="1"` and no `tcPr` fills, or `bandRow="0"` and all fills explicit. Mixing them is what produces this class of bug.

    **Assumption:** answered from general python-pptx/OOXML knowledge as instructed — no files opened, no deck inspected. If you want the specific cause for a given deck, the check is `a:tblPr` banding flags plus a count of explicit `a:tcPr` `solidFill`s per row.
- id: pptx-rowdel-04
  answer: |
    **Short version:** neither trick deletes anything. Both leave the `a:tr` element in the table XML, so every structural consumer still sees the row — you've only made it hard for a human to notice.

    **1. The row is still in the model.**
    `a:tbl/a:tr` is untouched. Row count, `table.rows`, cell text, totals, stats, extraction scripts, copy-paste to Excel/Sheets, search, and screen readers all still see it. A downstream sum over the table comes out wrong, and a diff of the deck shows nothing was removed. Concealment ≠ deletion.

    **2. Near-zero height usually doesn't survive.**
    `a:tr/@h` is a *preferred* height. PowerPoint recalculates row height from the tallest cell's text plus `a:tcPr` margins (`marT`/`marB`) and a minimum line height. Set it to 100 EMU and it snaps back to something readable the moment the deck opens or the text is touched — suddenly your white rectangle is the only thing hiding it, and it's the wrong size. You also still get the row's borders, so the table outline shows a collapsed double line or a stray horizontal rule.

    **3. The graphic frame doesn't follow.**
    `p:graphicFrame/p:xfrm/a:ext cy` must equal `sum(r.height for r in table.rows)`. Shrinking a row without setting `graphic_frame.height` leaves the frame at its old height — a blank band at the bottom of the table, and the layout below isn't reclaimed (or worse, overlaps the shapes underneath). The "deleted" row's space is still reserved.

    **4. A white rectangle is a sibling shape, not part of the table.**
    - It's absolutely positioned in EMU. When a cell above wraps or a font substitutes, the table reflows but the rectangle doesn't — you get text leaking around the edges or a white slab over the wrong row.
    - Z-order is not guaranteed. Inserting shapes, or any edit that reorders the shape tree, can put the table above the cover and the row pops back into view.
    - It prints, exports to PDF, and appears in slide thumbnails as an opaque block. It also covers the table's gridlines and fill, so you see a white hole interrupting the table's own border and banding.
    - It only works if the background is pure white; on a themed, tinted, or dark background the rectangle's edge is visible as a patch.

    **5. White text is the same story, worse.**
    Invisible only on a white background. Dark themes, high-contrast/accessibility mode, color inversion, or a changed slide background reveal it instantly. It's still read aloud by a screen reader, still found by search, still copied, still counted. And if you also cleared the text to be safe, the information is gone — you can't restore the row later.

    **6. Banding and merged cells break.**
    Alternating row fills depend on row *ordinal*. A zero-height phantom row shifts the parity, so the striping pattern inverts or stutters from that point down. Same problem with `gridSpan`/`vMerge` if the hidden row participates in a merge.

    **7. It's undiscoverable and non-idempotent.**
    The next person to open the deck can't tell a row was "removed". Nobody can grep for it. If someone later adds a row, your cover is now misaligned and the original row may reappear underneath. You've created a maintenance trap instead of a table with one fewer row.

    **What deletion actually requires:** remove the `a:tr` element, recompute the Total (and any subtotal) from the remaining rows, set `graphic_frame.height = sum(r.height for r in table.rows)`, re-alternate the explicit `a:tcPr` band fills, and check `graphic_frame.top + graphic_frame.height` against `prs.slide_height` and the shapes below it. That's the only version that is structurally true.
- id: pptx-cell-01
  answer: |
    ## Short answer

    `cell.text = "SKU"` throws the run away and builds a brand-new one. The new run has **no `a:rPr` at all**, so your explicit bold / 14 pt / white formatting is gone — the text falls back to whatever the table style and theme give a first-row cell (usually the style's header look, so it may *look* unchanged until you change the theme or use the same code on a body cell).

    ## Why (mechanism)

    `_Cell.text` is just a pass-through to `TextFrame.text`. That setter does:

    ```python
    self._txBody.clear_content()      # deletes every <a:p> in the cell
    for p_text in text.split("\n"):   # one new <a:p><a:r> per line
        ...
    ```

    So what is lost:

    - the whole `<a:p>` → its `<a:pPr>` (alignment, level, indent, line spacing) is lost too;
    - the `<a:r>` and its `<a:rPr>` (`b="1"`, `sz="1400"`, `<a:solidFill>` white) are lost.

    What survives (they are siblings/ancestors of the paragraphs, untouched):

    - `<a:tcPr>` — cell fill, borders, margins, vertical anchor;
    - `<a:bodyPr>` — autofit, wrap, insets;
    - the `<a:tbl>`/`<a:tblPr>` style reference (`firstRow="1"`, etc.).

    So a header cell styled *only* by the table style will look fine, while a header cell with explicit run formatting will silently change.

    ## How to keep it

    **1. Edit the existing run's text (best, one line):**

    ```python
    p = cell.text_frame.paragraphs[0]
    p.runs[0].text = "SKU"
    # optional: drop any other runs so stale fragments don't remain
    for r in p.runs[1:]:
        r._r.getparent().remove(r._r)
    ```

    `run.text` only rewrites `<a:t>`; `rPr` and `pPr` stay byte-identical.

    **2. Save and re-apply the `rPr` if you must use the cell-level setter:**

    ```python
    from copy import deepcopy

    src = cell.text_frame.paragraphs[0].runs[0]
    saved_rPr = deepcopy(src._r.get_or_add_rPr())
    saved_pPr = deepcopy(cell.text_frame.paragraphs[0]._p.pPr)  # may be None

    cell.text = "SKU"

    new_r = cell.text_frame.paragraphs[0].runs[0]._r
    new_r.insert(0, saved_rPr)      # a:rPr must be the first child of a:r
    if saved_pPr is not None:       # a:pPr must be the first child of a:p
        cell.text_frame.paragraphs[0]._p.insert(0, saved_pPr)
    ```

    **3. Re-apply by hand** — `run.font.bold = True; run.font.size = Pt(14); run.font.color.rgb = RGBColor(0xFF,0xFF,0xFF)` — works but duplicates style knowledge in code, so it drifts.

    ## Gotchas

    - **Multi-line text:** `"A\nB"` with the setter creates *two* new paragraphs, each unformatted — you'd have to copy `rPr` into both. `\v` inside a single run gives a soft `<a:br>` and keeps one `rPr`.
    - **`paragraph.text = ...` is just as destructive** (clears runs, adds a fresh one) — only `run.text` is safe.
    - **`endParaRPr` trap:** sometimes the formatting lives on `<a:endParaRPr>` (empty paragraph / caret formatting), not on a run. If `p.runs` is empty, you must deep-copy `endParaRPr` into a new run's `rPr`; otherwise there is nothing to preserve.
    - **Row geometry is unchanged:** editing text doesn't touch `<a:tr h>`; a longer string can wrap and overflow because the row height is only a minimum — re-check `graphic_frame.height` / slide bounds if the text grows.
    - **Cell fill/borders are safe:** `tcPr` isn't touched, so if your header "formatting" was really a dark fill plus white text, only the white text is at risk.

    BOTTOM LINE: `cell.text = ...` is a document-editing nuke for that cell's paragraphs — it deletes `pPr` and `rPr` and leaves you with style-inherited defaults. Preserve formatting by writing to `runs[0].text` (or by deep-copying the `rPr`/`pPr` before the assignment and re-inserting them).
- id: pptx-cell-02
  answer: |
    ## What PowerPoint actually does

    **Row heights are minimums, not fixed heights.** Each row stores `a:tr/@h`, and the renderer treats that as the *least* the row may be. When the new longer name wraps to more lines than `h` allows, PowerPoint grows that row on open, which pushes every row below it down and makes the table taller than the frame's cached `p:xfrm/a:ext/@cy`.

    Consequences of that single cell change:

    - **Column widths do not change.** Widths live in `a:gridCol/@w` (a single `<a:tblGrid>`), independent of cell text. PowerPoint will wrap, never widen, and never rebalance columns.
    - **The whole row grows, not just the cell.** All cells in that `a:tr` get taller, so short values look vertically padded (default anchor is top, so they sit at the top of a taller row).
    - **The table grows downward.** Position stays anchored at `top`; the extra height goes below. Shapes underneath are **not** pushed — the table overlaps them, and at the slide edge it simply runs off the slide (`prs.slide_height` is not a constraint).
    - **Text never auto-shrinks.** PowerPoint exposes no "shrink text on overflow" for table cells in the UI, and writing `a:normAutofit` into a cell's `a:bodyPr` is not reliable across versions/renderers. Do not count on it. (`text_frame.fit_text()` in python-pptx is not PowerPoint autofit — it only computes a size locally and needs a font file.)
    - **Cached geometry goes stale.** If the file was last touched by python-pptx, `h` and `cy` still describe the short text; PowerPoint recalculates on open, but LibreOffice/Keynote/preview renderers may use the stored values. That mismatch is where "it looked fine until someone opened it" bugs come from.

    ## The controls, in order of usefulness

    1. **Let it grow, then own the consequence.** This is the normal, safest choice. Insert the taller row height, recompute the Total, and set the frame to match so the file is internally consistent:

    ```python
    gf.height = sum(r.height for r in gf.table.rows)   # frame does not follow XML edits
    ```

    Then check `gf.top + gf.height` against `prs.slide_height` and any shape below; move them down or shrink the row. The frame is a cache, not an auto-layout: it must be fixed by hand.

    2. **Widen the column** (`table.columns[i].width`) and take the space from a neighbour so the total table width is unchanged. Fewer wrapped lines, no downward growth. Best when the column is the one that's already generous.

    3. **Reduce the text box's usable width penalty:** `cell.margin_left/right` (defaults ~0.1"), `cell.margin_top/bottom`, and line spacing. Cheap way to buy a line without touching typography.

    4. **Reduce font size** on the run(s) — a deterministic, cross-renderer fix. Set the row height explicitly afterwards so the file matches the render.

    5. **Shorten or abbreviate the text** ("Gadget D" → "GD-4 Pro" rather than a 60-character SKU sentence). If the full name is required, put it in a footnote or the notes pane.

    6. **`cell.text_frame.word_wrap = False`** (writes `a:bodyPr wrap="none"`). The row then stays short and the text runs out of the cell horizontally, across/behind the neighbouring cells. It does **not** widen the column. Legitimate only when the cell is the last in the row and there's whitespace to spill into — otherwise it produces overlapping, unreadable columns.

    7. **Flatten to a picture** if the layout is contractual (a template, a print master). Nothing in the file can then reflow. Costs editability and searchability.

    ## Rule of thumb

    For a one-word-to-one-phrase swap, expect one or two extra lines: pick option 1, fix the Total and the frame height, verify the bottom edge, and move anything below it. If the new name is radically longer, don't fight autogrow in a fixed-width column — widen the column (option 2) or resize the type (option 4). Turning off wrap to "preserve the layout" is the trap: it preserves the row height and destroys the table.
- id: pptx-cell-03
  answer: |
    Nothing in a .pptx recalculates on its own — every number is a literal string in a run — so changing Qty 5 → 12 means hand-updating the whole dependency chain:

    **1. The row you touched**
    - Line Total = 12 × Unit Price, written through the cell's existing first run so currency/formatting/alignment survive (`runs[0].text`, not `cell.text`).

    **2. Every aggregate below it**
    - Subtotal = sum of all line totals.
    - Discount, if it's a percentage of subtotal.
    - Tax/VAT, if computed on subtotal − discount.
    - Grand Total / Total Due.
    - Amount-in-words or "N items" summary lines, if the deck has them.

    **3. Mirrored copies of the same figure**
    - KPI/stat callout shapes ("1,240 units shipped").
    - Summary or appendix tables repeating the line.
    - Chart data (bar/pie/column of quantities) — a chart's cached values don't follow the table either.
    - Speaker notes and any narrative sentence quoting the quantity or total.

    **4. Derived values that can flip**
    - Units total, average price, "% of total" shares, and any ranking/ordering if the qty change reorders line items.

    **5. Layout**
    - If "12" is wider than "5" and the row wraps or grows, the table frame does not follow the XML: set `graphic_frame.height = sum(r.height for r in table.rows)`, then check `top + height` against `prs.slide_height` and against any shapes sitting below the table.

    **Assumption:** the table has Qty, Unit Price, and Line Total columns plus a Total row, and the line was identified by its label cell (e.g. "Gadget D") with an asserted single match — not the first `has_table` shape. If any of those don't hold, the set of dependents changes; say so and I'll adjust.
- id: pptx-relayout-01
  answer: |
    **Short answer:** python-pptx has no `table.add_row()`. You add a row at the XML level (`copy.deepcopy` of an existing `a:tr`, insert with `addnext`/`addprevious`), then fill the cells through their existing runs so formatting survives, then fix the Total and the frame height by hand.

    ```python
    import copy
    from decimal import Decimal
    from pptx import Presentation
    from pptx.enum.shapes import MSO_SHAPE_TYPE

    prs = Presentation("deck.pptx")
    slide = prs.slides[0]

    # ---------- 1. find THE table by its header, not "first has_table" ----------
    def tables(shapes):
        for sh in shapes:
            if sh.shape_type == MSO_SHAPE_TYPE.GROUP:
                yield from tables(sh.shapes)
            elif sh.has_table:
                yield sh

    HEADER = ("Service", "Qty", "Rate", "Amount")     # exact first-row text
    frames = [gf for gf in tables(slide.shapes)
              if tuple(c.text_frame.text.strip() for c in gf.table.rows[0].cells) == HEADER]
    assert len(frames) == 1, f"expected 1 table, got {len(frames)}"
    gf = frames[0]
    table = gf.table

    # ---------- 2. locate anchor + Total, prove uniqueness ----------
    def row_where(label):
        hits = [r for r in table.rows
                if r.cells[0].text_frame.text.strip() == label]
        assert len(hits) == 1, f"{label!r}: {len(hits)} matches"
        return hits[0]

    ref   = row_where("Service G")     # the row whose formatting you copy
    total = row_where("Total")

    # ---------- 3. clone + insert between Service G and Total ----------
    new_tr = copy.deepcopy(ref._tr)    # copies tc count, a:tcPr, run rPr, h, etc.
    total._tr.addprevious(new_tr)      # same as ref._tr.addnext(new_tr) here;
                                       # addprevious is safer if Total isn't adjacent

    # ---------- 4. wrap the new element and fill via existing runs ----------
    rows = list(table.rows)            # re-read: the property rebuilds _Row objects
    new_row = next(r for r in rows if r._tr is new_tr)
    i_new   = rows.index(new_row)

    VALUES = ("Service H", "1", "$0.00", "$0.00")

    for i, (cell, value) in enumerate(zip(new_row.cells, VALUES)):
        p = cell.text_frame.paragraphs[0]
        if p.runs:
            p.runs[0].text = value                  # keeps that run's a:rPr
            for extra in p.runs[1:]:                # copied row may have several runs
                extra._r.getparent().remove(extra._r)
        else:
            # source cell was empty: add one run and borrow formatting from a sibling
            run = p.add_run()
            src = next((c.text_frame.paragraphs[0].runs[0]
                        for c in (ref.cells[i], *new_row.cells)
                        if c.text_frame.paragraphs[0].runs), None)
            if src is not None:
                rpr = src._r.find("{http://schemas.openxmlformats.org/drawingml/2006/main}rPr")
                if rpr is not None:
                    run._r.insert(0, copy.deepcopy(rpr))
            run.text = value

    # ---------- 5. recompute the Total from the data rows ----------
    def money(cell):
        t = cell.text_frame.text.strip().replace("$", "").replace(",", "")
        return Decimal(t) if t else Decimal(0)

    data = [r for r in table.rows[i_new:] if r.cells[0].text_frame.text.strip() != "Total"]
    amount_col = 3
    subtotal = sum(money(r.cells[amount_col]) for r in data)

    tp = total.cells[amount_col].text_frame.paragraphs[0]
    tp.runs[0].text = f"${subtotal:,.2f}"
    for extra in tp.runs[1:]:
        extra._r.getparent().remove(extra._r)

    # ---------- 6. frame does NOT follow the XML edit: resync ----------
    gf.height = sum(r.height for r in table.rows)   # Emu
    bottom = gf.top + gf.height
    assert bottom <= prs.slide_height, f"table overflows slide by {bottom - prs.slide_height} EMU"
    # shapes whose top is below gf.top + old height may now collide -> move or ask

    prs.save("deck.pptx")
    ```

    Key points that make it "keep the formatting":

    1. **Copy a data row, not the Total row.** `deepcopy(ref._tr)` inherits the data row's `a:tcPr`, fonts, alignment, and `h`, so the new row matches the band it lands in. Copying `Total` would carry total-row styling.
    2. **Write text into `p.runs[0].text`**, don't assign `cell.text = ...` — the `cell.text`/`text_frame.text` setter rebuilds paragraphs/runs and drops `a:rPr` (you lose font, size, bold, color).
    3. **Remove surplus runs** after the first, otherwise the copied string's tail remains.
    4. **Insert by element:** `total_tr.addprevious(new_tr)` or `ref_tr.addnext(new_tr)`. Both put the row after Service G and before Total; `addprevious` on Total is the robust form.
    5. **Recompute Total** in code — deepcopy duplicates the Total *text* only if you copied Total, and here you must recalc anyway since the row count changed.
    6. **Resync `p:xfrm/a:ext cy`:** set `gf.height = sum(r.height for r in table.rows)`, then check `gf.top + gf.height` against `prs.slide_height` and any shapes below; move them or stop and ask.
    7. **Explicit banding:** if the table paints alternating rows with direct `a:tcPr` fills (rather than a table style with `bandRow="1"`), re-alternate the fills yourself after the insert — the copied row otherwise duplicates the anchor's shade.
    8. Don't touch `a:tblGrid` for a row insert; the grid only holds columns. Merged cells (`gridSpan`/`vMerge`) survive the deepcopy unchanged.

    Assumptions: the table is on a slide reachable directly or via groups; the label column is column 0; "Service G" and "Total" each appear exactly once (the asserts enforce this instead of silently editing the wrong row); amounts are plain `$1,234.56` strings. If `r.height` is 0 because the rows carry no explicit `h`, set heights explicitly before summing or compute the frame height from the intended row height instead.
- id: pptx-relayout-02
  answer: |
    ## Short answer

    There is no `table.add_column()` / `add_row()` in python-pptx, so this is raw XML: insert one `a:gridCol` in `a:tblGrid` **and** one `a:tc` at the same index in **every** `a:tr`. Then, because the table already fills the slide, do **not** widen the frame — pay for the new column by shrinking existing columns so the grid total stays the same, and re-sync `p:xfrm/a:ext` and the vertical bottom.

    Assume: plain-text SKU, no merged cells, python-pptx 1.0.2. Read the real deck geometry (`prs.slide_width`, `prs.slide_height`) rather than assuming 4:3 or 16:9.

    ## XML changes required

    Inside `p:graphicFrame/p:graphic/a:graphicData/a:tbl`:

    1. **`a:tblGrid`** — insert a new `a:gridCol` directly after the Item column's `gridCol`, with an explicit `w` in EMU. The invariant is strict: `len(a:tblGrid/a:gridCol) == len(a:tr/a:tc)` for every row. A mismatch gets you a repair prompt in PowerPoint.
    2. **Every `a:tr`** — insert a new `a:tc` at the same index (after the Item cell). Deep-copy the Item cell of that *same row* so it inherits row-appropriate banding fill, borders, margins, and font; then replace its text. Keep child order `a:txBody` then `a:tcPr` (deepcopy preserves it).
    3. **`a:tblPr`** — unchanged. Don't flip `firstCol`/`bandRow` just because there's a new column; the header row's `firstRow` styling is unaffected.
    4. **Merges** — if any `a:tc` has `gridSpan` ≠ 1, `rowSpan` ≠ 1, `hMerge`, or `vMerge`, stop. A span crossing the insertion point must have its `gridSpan` incremented and the spanned-over cells adjusted; otherwise the grid is corrupt.
    5. **Relationships** — none for plain text. If you later add hyperlinks (`a:hlinkClick r:id`) or images, remember `drop_rel()` counts only `//@r:id`, so image rels (`a:blip/@r:embed`) always count 0 and get dropped. Not applicable to a SKU text column.

    ```python
    from copy import deepcopy
    from pptx.oxml.ns import qn
    from pptx.util import Emu, Inches

    gf   = table_frame                      # p:graphicFrame
    tbl  = gf._element.graphic.graphicData.tbl
    grid = tbl.tblGrid
    grid_cols = grid.findall(qn('a:gridCol'))
    trs  = tbl.findall(qn('a:tr'))
    item_idx = 1                            # 0-based index of "Item"
    old_total = sum(int(gc.get('w')) for gc in grid_cols)

    # 1) grid column right after Item
    for tr in trs:                          # sanity before editing
        assert len(tr.findall(qn('a:tc'))) == len(grid_cols)
    new_gc = deepcopy(grid_cols[item_idx])
    grid_cols[item_idx].addnext(new_gc)

    # 2) matching cell in every row
    for tr in trs:
        tcs = tr.findall(qn('a:tc'))
        new_tc = deepcopy(tcs[item_idx])    # same row band / fill / borders
        tcs[item_idx].addnext(new_tc)

    # 3) now that geometry exists, fill text via python-pptx
    table = gf.table
    table.cell(0, item_idx + 1).text_frame.text = "SKU"
    for r in range(1, len(trs)):
        table.cell(r, item_idx + 1).text_frame.text = ""   # or real SKU values
    ```

    ## Keeping it on the slide

    The frame will not follow XML edits, and you cannot grow a nearly-full-width table. Two rules:

    **Horizontally — keep the grid total constant.** Choose a SKU width, subtract it from the Item (or widest body) column, and leave `sum(gridCol/@w)` equal to the old total:

    ```python
    new_w   = int(Inches(1.0))
    item_gc = grid_cols[item_idx]
    item_w  = int(item_gc.get('w'))

    # don't let the donor column collapse; spread over neighbours or shrink font
    assert item_w - new_w >= int(Inches(0.6)), "no room: shrink font / ask user"

    item_gc.set('w', str(item_w - new_w))
    new_gc.set('w', str(new_w))

    total = sum(int(gc.get('w')) for gc in grid.findall(qn('a:gridCol')))
    assert total == old_total                      # frame width unchanged
    gf.width = Emu(total)                          # no-op if it was already right
    ```

    If instead you *do* widen, the hard ceiling is `gf.left + gf.width <= prs.slide_width` (minus a margin), and you must check for shapes sitting to the right in the same vertical band. With a nearly-full-width table that ceiling is already exhausted, so redistribution is the correct move, not growth. If the donor columns can't give up the width, reduce the header font / allow wrapping, or ask the user which column should shrink.

    **Vertically — rows can grow when text wraps.** Recompute the frame height and re-check the bottom:

    ```python
    hs = [int(tr.get('h')) for tr in trs if tr.get('h') is not None]
    if len(hs) == len(trs):
        gf.height = Emu(sum(hs))                   # a:tr/@h are minimums

    assert gf.left + gf.width  <= prs.slide_width
    assert gf.top  + gf.height <= prs.slide_height
    # then check shapes below/overlapping gf.top + gf.height
    ```

    Also verify: header text fits the chosen width, `a:gridCol` count still equals every row's `a:tc` count, and re-alternate any explicit `a:tcPr` banding fills if the table uses manual fills rather than a table style. If a merge or an overflow blocks the edit, stop and ask rather than forcing the XML.
- id: pptx-tblins-01
  answer: |
    Assumption: "3×2" means 3 rows × 2 columns, and "without overlapping" means the new table's rectangle must not intersect any visible existing shape.

    ## 1. Measure what is already on the slide

    python-pptx has no layout engine, so you compute the free area yourself.

    ```python
    from pptx.util import Emu
    from pptx.enum.shapes import MSO_SHAPE_TYPE

    def occupied(slide):
        boxes = []
        for sh in slide.shapes:
            # a group's own frame is in parent coordinates and is a valid bbox;
            # do NOT recurse into GroupShape.shapes (children use child coords)
            if sh.left is None or sh.top is None or sh.width is None or sh.height is None:
                continue                      # geometry-less placeholder
            if sh.is_placeholder and not sh.has_text_frame or (sh.is_placeholder and not sh.text_frame.text.strip()):
                continue                      # empty placeholder renders nothing
            boxes.append((sh.left, sh.top, sh.width, sh.height))
        return boxes
    ```

    Notes: clamp negative `top/left` (shapes off-slide still count), treat a `GroupShape` as one opaque box, and skip placeholders you know are empty — they don't paint anything. Read the deck size, never assume it: `prs.slide_width`, `prs.slide_height` (16:9 = 12192000×6858000 EMU; python-pptx's default template is 4:3 = 9144000×6858000).

    ## 2. Pick the position (decision ladder)

    Use a margin `M` (e.g. 0.5" = 457200 EMU) and a gutter `G` (e.g. 0.25" = 228600 EMU).

    1. **Bottom strip:** `top = max(t + h for ...) + G`, `left` = leftmost content edge, width = content width (or slide width − 2M). Accept if `top + needed_h <= prs.slide_height - M`.
    2. **Right strip:** `left = max(l + w for ...) + G`, `width = prs.slide_width - M - left`. Accept only if width is usable (≥ ~3" for two columns) and the vertical band is free or below the lower content.
    3. **Shrink/reflow:** reduce row heights or font, or drop to a narrower table; recompute.
    4. **Nothing fits:** put it on a new slide with the same layout (`prs.slides.add_slide(slide.slide_layout)`), or stop and ask — never overlap "a little".

    Provisional height for the fit test: roughly `header + 2 normal rows` (e.g. 3 × 0.4" ≈ 1097280 EMU); you verify the real height in step 4.

    Rect intersection test (with small tolerance for rounding):

    ```python
    def hits(a, b, tol=0):
        al, at, aw, ah = a; bl, bt, bw, bh = b
        return not (al + aw <= bl + tol or bl + bw <= al + tol or
                    at + ah <= bt + tol or bt + bh <= at + tol)
    ```

    Assert the chosen rect hits none of `occupied(slide)`; if it does, fall through the ladder.

    ## 3. Create it

    ```python
    rows, cols = 3, 2
    gf = slide.shapes.add_table(rows, cols, left, top, width, height)  # graphic frame
    tbl = gf.table
    tbl.columns[0].width = Emu(int(width * 0.4))
    tbl.columns[1].width = width - tbl.columns[0].width      # widths must sum to width
    tbl.rows[0].height = Emu(457200)                          # header
    for r in tbl.rows[1:]:
        r.height = Emu(342900)
    ```

    `add_table` appends to the shape tree, so it sits on top of existing shapes — good. It also brings the default style ("Medium Style 2 – Accent 1" via `a:tblPr/a:tableStyleId`); change that GUID text later if the deck uses another style.

    ## 4. Fill, then resync the frame

    Rows grow to fit text, so the height you asked for is a promise python-pptx does not keep and does not propagate to the frame.

    ```python
    data = [["Header A", "Header B"], ["a1", "b1"], ["a2", "b2"]]
    for r, row in enumerate(data):
        for c, val in enumerate(row):
            cell = tbl.cell(r, c)
            cell.text = val                      # or set runs[0].text and drop extra runs
            for p in cell.text_frame.paragraphs:
                for run in p.runs:
                    run.font.size = Pt(14)

    gf.height = sum(r.height for r in tbl.rows)   # frame does not follow XML edits
    assert gf.top + gf.height <= prs.slide_height - M
    ```

    Then re-run the intersection test with the *actual* rect. If it now collides with a shape below (or runs off the slide), move the table up by the overflow, shrink row heights/font, or move the offending shape down — don't leave it overlapping.

    ## Gotchas worth stating

    - `table.add_row()` / `add_column()` do not exist in python-pptx; sizing at creation is the only structural control (rows later require `copy.deepcopy(tr)` + `addnext`).
    - `gf.height` and `table.rows[i].height` are independent; always finish with `gf.height = sum(r.height ...)`.
    - Requested `height`/`width` are distributed across rows/columns; setting individual widths after creation is how you get a 40/60 split instead of 50/50.
    - Empty placeholders and off-slide shapes are the two usual false positives when computing the free area.
- id: pptx-tblins-02
  answer: |
    ## Why it happens

    `add_table()` writes a fixed default into the new `<a:tbl>`:

    ```xml
    <a:tblPr firstRow="1" bandRow="1">
      <a:tableStyleId>{5C22544A-7EE6-4342-B048-85BDC9FD1C3A}</a:tableStyleId>
    </a:tblPr>
    ```

    That GUID is **Medium Style 2 – Accent 1**. The style is only a *reference* — PowerPoint resolves it against the theme's `accent1`, so you get the blue banded look. Your deck's existing tables are almost always one of:

    1. a different `tableStyleId` (deck's own style),
    2. **no** `tableStyleId` at all, with every cell carrying explicit `a:tcPr` fills/borders,
    3. the same GUID but overridden by direct formatting on cells/runs.

    So don't try to guess a GUID. **Clone the formatting off a real table in the same deck** — table level plus cell level. A style GUID alone won't match a table that uses direct formatting, and direct formatting alone won't match one that relies on the style's banding.

    ## Recipe

    Order matters: **write the cell text first, then copy formatting** — `cell.text = ...` rewrites the `a:txBody` and would wipe any run properties you copied in beforehand.

    ```python
    from copy import deepcopy
    from pptx.oxml.ns import qn, nsdecls
    from pptx.oxml import parse_xml

    def match_table_style(new_gf, ref_gf):
        new_tbl = new_gf.table._tbl          # CT_Table
        ref_tbl = ref_gf.table._tbl

        # ---- 1. table level: tblPr (style GUID + firstRow/bandRow/lastRow flags) ----
        old = new_tbl.find(qn('a:tblPr'))
        if old is not None:
            new_tbl.remove(old)
        ref_tblPr = ref_tbl.find(qn('a:tblPr'))
        if ref_tblPr is None:
            new_tbl.insert(0, parse_xml('<a:tblPr %s/>' % nsdecls('a')))
        else:
            new_tbl.insert(0, deepcopy(ref_tblPr))   # tblPr is first child of a:tbl

        # ---- 2. grid widths ----
        new_grid = new_tbl.find(qn('a:tblGrid'))
        ref_grid = ref_tbl.find(qn('a:tblGrid'))
        for gc_new, gc_ref in zip(new_grid.findall(qn('a:gridCol')),
                                  ref_grid.findall(qn('a:gridCol'))):
            gc_new.set('w', gc_ref.get('w'))

        # ---- 3. per-cell geometry + formatting ----
        for tr_new, tr_ref in zip(new_tbl.findall(qn('a:tr')),
                                  ref_tbl.findall(qn('a:tr'))):
            tr_new.set('h', tr_ref.get('h'))
            for tc_new, tc_ref in zip(tr_new.findall(qn('a:tc')),
                                      tr_ref.findall(qn('a:tc'))):
                _copy_tc_format(tc_new, tc_ref)

        # ---- 4. frame geometry, then resync height from the rows ----
        new_gf.left, new_gf.top = ref_gf.left, ref_gf.top
        new_gf.width = ref_gf.width
        new_gf.height = sum(r.height for r in new_gf.table.rows)


    def _copy_tc_format(tc_new, tc_ref):
        # tcPr comes AFTER txBody inside a:tc -> append is the correct slot
        old = tc_new.find(qn('a:tcPr'))
        if old is not None:
            tc_new.remove(old)
        ref_tcPr = tc_ref.find(qn('a:tcPr'))
        if ref_tcPr is not None:
            tc_new.append(deepcopy(ref_tcPr))        # fill, borders, margins, anchor

        tb_new, tb_ref = tc_new.find(qn('a:txBody')), tc_ref.find(qn('a:txBody'))
        if tb_new is None or tb_ref is None:
            return
        ref_paras = tb_ref.findall(qn('a:p'))
        for p_new, p_ref in zip(tb_new.findall(qn('a:p')), ref_paras):
            old = p_new.find(qn('a:pPr'))
            if old is not None:
                p_new.remove(old)
            pPr_ref = p_ref.find(qn('a:pPr'))
            if pPr_ref is not None:
                p_new.insert(0, deepcopy(pPr_ref))
            ref_runs = p_ref.findall(qn('a:r'))
            for r_new, r_ref in zip(p_new.findall(qn('a:r')), ref_runs):
                old = r_new.find(qn('a:rPr'))
                if old is not None:
                    r_new.remove(old)
                rPr_ref = r_ref.find(qn('a:rPr'))
                if rPr_ref is not None:
                    r_new.insert(0, deepcopy(rPr_ref))
    ```

    If the new cell has more runs than the reference (mixed bold/colour text), copy only `ref_runs[0]`'s `a:rPr` onto the extra runs rather than leaving them unstyled.

    ## Practical notes

    - **Pick the reference table by content, not by position.** Collect every `has_table` frame (recurse into `GroupShape.shapes`), keep the one whose first-row header texts match, and assert exactly one candidate. Same for any row you use as a template. Zero or several matches → stop and ask.
    - **Geometry follows the same rule.** `add_table` also produces whatever width/column split you passed. Copy `gridCol/@w`, `tr/@h`, and the frame's `left/top/width`, then set `gf.height = sum(r.height for r in table.rows)` — the `a:xfrm/a:ext cy` does not follow XML row edits.
    - **Then check the bottom** `gf.top + gf.height` against `prs.slide_height` (read it — 16:9 is 12192000 × 6858000, python-pptx's own default template is 4:3) and against shapes below; move them or ask.
    - **If the deck's tables are "plain"** (case 2), the match is: `tblPr` with **no** `tableStyleId` child, plus explicit `a:tcPr` `a:solidFill` per cell. Keep re-applying that explicit banding on every row you add — there is no style to do it for you.
    - **Custom deck styles** live in `ppt/tableStyles.xml`. Copying the GUID works because you're in the same deck; it would break if you copied the table into another presentation.
    - **Most reliable option when row/column counts already match:** `deepcopy(ref_gf._element)`, append it to `slide.shapes._spTree`, then rewrite each cell's text. That clones style, grid, margins, fonts and flags in one shot. Caveat: it also clones cell hyperlink relationships (`a:hlinkClick/@r:id`) — copy only if you actually replace/repair those, otherwise you inherit the reference's link targets.

    You can't render from python-pptx, so verify by re-opening the saved file and comparing `tbl.tblPr` / `tcPr` XML against the reference, then eyeball it in PowerPoint or LibreOffice.
- id: pptx-tblins-03
  answer: |
    **Use the placeholder's `insert_table(rows, cols)` method — not `shapes.add_table(...)`.**

    `SlidePlaceholder.insert_table()` is the placeholder-aware API. It creates the `a:tbl` graphic frame using the placeholder's own `a:xfrm` (same left/top/cx/cy), replaces the empty placeholder with that frame, and returns the `GraphicFrame`:

    ```python
    from pptx.enum.shapes import PP_PLACEHOLDER

    slide = prs.slides[0]

    # Find the content placeholder by identity, not by list position.
    ph = next(
        p for p in slide.placeholders
        if p.placeholder_format.type in (PP_PLACEHOLDER.OBJECT, PP_PLACEHOLDER.BODY)
    )

    gf = ph.insert_table(rows=5, cols=4)   # rows/cols are counts, header row included
    table = gf.table

    # fill cells
    for r, row in enumerate(data, start=1):
        for c, value in enumerate(row):
            table.cell(r, c).text = str(value)
    ```

    Key points:

    - **Geometry is inherited.** You don't compute coordinates; the table lands exactly where the layout put the content placeholder. `gf.left/top/width/height` equal the old placeholder extents.
    - **The placeholder is consumed.** `insert_table` swaps the placeholder `p:sp` for a `p:graphicFrame` in the slide's shape tree, so there's no leftover "Click to add text" box and no need to delete it. Don't keep using `ph` afterward; keep the returned `gf`.
    - **Pick the placeholder by `placeholder_format.idx`/`type`,** not `slide.placeholders[1]` — ordering is not guaranteed and a slide can carry several placeholders.
    - **If the slide has no cloned placeholder yet** (deck built outside python-pptx, or placeholder removed), clone it from the layout first, then insert:
      ```python
      layout_ph = slide.slide_layout.placeholders[1]   # the content placeholder in the layout
      ph = slide.shapes.clone_placeholder(layout_ph)   # raises ValueError if one already exists
      gf = ph.insert_table(rows=5, cols=4)
      ```
    - **Row count is fixed at insert time.** python-pptx 1.0.2 has no `table.add_row()`, so pass the final row count to `insert_table`. If you must add rows later, deepcopy an `a:tr` and `addnext` it; then it's not finished until you recompute the Total, set `gf.height = sum(r.height for r in gf.table.rows)` (the frame does not follow XML edits), and check `gf.top + gf.height` against `prs.slide_height` and any shapes below.
    - **Styling comes from the table default**, not the placeholder's text styles. The generated `a:tblPr` uses the default table-style GUID with `firstRow`/`bandRow` set, so apply any deck-specific banding/`tcPr` fills yourself.

    Why not `slide.shapes.add_table(rows, cols, left, top, width, height)`? It works, but it creates a sibling shape at guessed coordinates and leaves the empty content placeholder on the slide (visible as an empty "click to add" region in edit mode). That's the anti-pattern the placeholder API exists to avoid.
- id: pptx-imgrep-01
  answer: |
    ## Short answer

    `python-pptx` has no replace API, so you either **swap the image relationship under the existing `<p:pic>`** (best: geometry, crop, rotation, alt text, shape id, animations all survive) or **add a new picture, splice it into the old element's exact slot in `p:spTree`, then delete the old one** (needed when you must change the frame too). Because z-order *is* the `p:spTree` child order, "same stacking order" == "same index among the parent's children", not "add it and hope".

    ## Method 1 — repoint `a:blip/@r:embed` (recommended)

    Nothing about the shape changes: same `a:xfrm`, same `a:srcRect`, same id, same z-order. Only the bytes behind the blip change.

    ```python
    from pptx import Presentation
    from pptx.oxml.ns import qn

    def rel_in_use(part, rId):
        """drop_rel() only counts //@r:id; pictures use @r:embed / @r:link."""
        for el in part._element.iter():
            if el.get(qn("r:embed")) == rId or el.get(qn("r:link")) == rId:
                return True
        return False

    prs = Presentation("in.pptx")
    slide = prs.slides[0]

    pic = next(s for s in slide.shapes
               if s.shape_type == MSO_SHAPE_TYPE.PICTURE and s.name == "Logo")
    blip   = pic._element.blipFill.blip
    old_rid = blip.get(qn("r:embed"))

    # returns (image_part, rId); python-pptx de-dupes by SHA1, so this may reuse a part
    _, new_rid = slide.part.get_or_add_image_part("new.png")
    blip.set(qn("r:embed"), new_rid)

    if new_rid != old_rid and not rel_in_use(slide.part, old_rid):
        slide.part.drop_rel(old_rid)   # else the old media stays in ppt/media/

    prs.save("out.pptx")
    ```

    Caveats: `get_or_add_image_part` is semi-internal and the tuple is `(image_part, rId)` — assert the order in your version. Also works for pictures nested in groups, which Method 2 does not.

    ## Method 2 — add, splice, delete

    ```python
    import copy
    from pptx.oxml.ns import qn

    left, top, w, h = pic.left, pic.top, pic.width, pic.height
    old_el  = pic._element
    old_rid = old_el.blipFill.blip.get(qn("r:embed"))
    old_id  = old_el.nvPicPr.cNvPr.get("id")
    descr   = old_el.nvPicPr.cNvPr.get("descr")

    new_pic = slide.shapes.add_picture("new.png", left, top, w, h)   # appended = top of z-order
    new_el  = new_pic._element
    new_pic.name = pic.name
    if descr:
        new_el.nvPicPr.cNvPr.set("descr", descr)

    # optional: keep animations/timing bindings that key off spid
    new_el.nvPicPr.cNvPr.set("id", old_id)

    # optional: carry rotation/flip and crop
    for attr in ("rot", "flipH", "flipV"):
        v = old_el.spPr.xfrm.get(attr)
        if v is not None:
            new_el.spPr.xfrm.set(attr, v)
    src = old_el.blipFill.find(qn("a:srcRect"))
    if src is not None:
        new_el.blipFill.insert_element_before(copy.deepcopy(src), "a:stretch", "a:tile")

    # splice: same parent, immediately before the old pic => identical index/z-order
    old_el.addprevious(new_el)          # lxml moves the element, no duplicate
    old_el.getparent().remove(old_el)

    if not rel_in_use(slide.part, old_rid):
        slide.part.drop_rel(old_rid)

    prs.save("out.pptx")
    ```

    Notes:

    - Passing both `width` and `height` from the old shape preserves the frame exactly, but **stretches** the new image if its aspect differs. Center-crop instead via `a:srcRect` if that matters.
    - Don't use `slide.shapes.add_picture` for a picture that lives inside a group; it lands at top level. Use Method 1 there.
    - Reusing `old_id` is safe only because the old element is removed before saving; leave duplicate ids in the tree and PowerPoint will complain.
    - There is no `table.add_row()`-style shortcut here either — no `picture.replace`, no `shape.replace`, and `Picture` has no settable `image` property.

    Verify after saving by reopening: `pic.image.sha1` / `pic.image.size` should be the new file's, and `list(slide.shapes).index(pic)` unchanged.

    ---

    **ASKED** — how to swap one picture's image, same left/top/width/height, same z-order.
    **BUILT** — two working recipes: blip-rel swap (preserves everything) and add/splice/delete (preserves position by index).
    **FOUND** — z-order in PPTX is `p:spTree` child order, and `drop_rel` counts only `@r:id`, so old media silently survives unless you check `@r:embed` yourself.
    **DECIDED** — blip swap is the default; splice only when the frame or element identity must change.
    **BOTTOM LINE:** repoint `a:blip/@r:embed`, then drop the orphaned rel — the old pic never moves, so stacking order is trivially preserved.
    **NEXT** — confirm tuple order of `get_or_add_image_part` in your installed python-pptx version; it decides Method 1's unpacking.
- id: pptx-imgrep-02
  answer: |
    **Short answer:** the two pictures point at the *same embedded image part*. Overwriting bytes on that part is a package-wide edit, so every relationship that targets it — slide 7's included — now resolves to the new logo. The rIds differ per slide (they're per-part), but the *target* is one shared `ppt/media/imageN.*` file.

    **Why it happens**

    - python-pptx deduplicates images package-wide: `ImagePartCollection.get_or_add_image_part()` hashes the image blob (SHA-1) and, if that hash already exists, hands back the existing `ImagePart` and just adds a new relationship to it. Two slides using the same logo bytes (even from separate `add_picture` calls, even under different filenames) end up with one media part and two rels, e.g. slide 1 `/slide1.xml` → `rId3`, slide 7 `/slide7.xml` → `rId2`, both → `/ppt/media/image4.png`.
    - The package serializes each reachable part exactly once. `image_part._blob = new_bytes` (or writing the zip entry by partname) mutates that single object, so the saved zip contains one file, and both slides render whatever is in it.
    - It's silent because nothing in the API warns you the part is shared; `pic.image` on either slide returns the same `Image`/`ImagePart`, and `blob` reads back the new bytes everywhere.
    - Crop/scale don't save you: `a:srcRect`, `a:stretch`, and `a:xfrm` live on the picture's `p:pic`, but the pixels live in the shared `a:blip` target.

    **How to replace only slide 1's logo**

    1. Add the new bytes as a *new* part and get a new rel on slide 1 only:
       ```python
       image_part, rId = slide1.part.get_or_add_image_part("new_logo.png")
       pic._element.blipFill.blip.rEmbed = rId   # a:blip/@r:embed
       ```
    2. Never mutate `image_part._blob` of a part you didn't just create.
    3. Only after repointing, consider dropping slide 1's old rel — and check users yourself first. `SlidePart.drop_rel(rId)` counts only `//@r:id`; an image is referenced through `a:blip/@r:embed` (sometimes `@r:link`), which that count never sees, so it reports "0 users" and drops a rel another picture in the same part may still need. Query the part XML for `//@r:embed` / `//@r:link` equal to that rId across all parts and drop only if none remain. An old rel you leave in place keeps the superseded logo in `ppt/media/` after the picture is gone; an old rel you drop too eagerly breaks slide 7.

    **Same trap in other disguises:** assigning `pic._element.blip_rId` to an rId that already exists elsewhere, or rewriting `ppt/media/image4.png` directly in the zip — both edit the shared target, not the picture. The picture is just a pointer; the bytes are the shared thing.

    *Assumption: python-pptx (1.0.x) semantics, since the question is framed around "overwrite the image-part bytes."*
- id: pptx-imgrep-03
  answer: |
    Because `drop_rel` doesn't understand pictures.

    Here's the mechanism:

    **1. `drop_rel` is gated on a reference count that doesn't know about images.**

    `slide.part.drop_rel(rId)` only actually drops the relationship when its reference count is below 2:

    ```python
    def drop_rel(self, rId):
        if self._rel_ref_count(rId) < 2:
            ...
    ```

    and `_rel_ref_count` is implemented as an XPath count restricted to `r:id` attributes:

    ```python
    def _rel_ref_count(self, rId):
        return len(self._element.xpath('//@r:id[.="%s"]' % rId))
    ```

    pictures reference their image through `a:blip/@r:embed` (and sometimes `@r:link`), never through `r:id`. So a picture-shaped relationship **always counts zero**, no matter how many `p:pic` elements are pointing at it. Zero is `< 2`, so the check passes unconditionally and the rel is removed. Nothing about "another picture still uses it" is ever consulted.

    **2. python-pptx deliberately deduplicates image parts.**

    When several pictures share the same image, `ImagePart.get_or_add_image_part()` reuses the already-loaded image part, and `relate_to()` returns the **same rId** for all of them. So two `p:pic` elements on the slide can carry identical `r:embed="rId7"` values. Removing that one rel therefore severs both pictures at once — the second one's blip now points at a dangling rId, and PowerPoint/PowerPoint-like renderers show a broken image (or drop the picture).

    **3. Why the media vanishes too.**

    `drop_rel` removes the `Relationship` entry from the slide part's rels. On save, python-pptx walks only parts reachable through rels, so the image part is no longer serialized into `ppt/media/`. That's the desired outcome for the picture you meant to delete — it's just applied one picture too eagerly.

    **Correct handling**

    Do the reference check yourself, against the actual picture attributes, before dropping:

    ```python
    xml = slide.part.element  # or slide._element
    still_used = xml.xpath(
        '//@r:embed[.="%s"] | //@r:link[.="%s"]' % (rId, rId)
    )
    if not still_used:
        slide.part.drop_rel(rId)
    ```

    Check both `r:embed` and `r:link`, and account for shapes inside groups (a group's `p:pic` lives in the same slide part, so a plain `//@r:embed` covers it). If the count is nonzero, leave the rel alone — the old image will stay in `ppt/media/`, which is the price of a still-referenced part. To actually reclaim space you must remove or repoint *every* picture using that rId, then drop the rel once the last `r:embed`/`r:link` for it is gone.

    Do **not** rely on `drop_rel`'s return value or its refcount to mean "unused" for anything image-shaped — that heuristic is written for `r:id`-style references (hyperlinks, embedded objects, slide references), and it is structurally blind to `a:blip`.
- id: pptx-imgins-01
  answer: |
    ## Short answer

    Add the picture with **only the width** so python-pptx derives the height from the image's own pixel ratio, then place it by its **right and bottom edges**, and only after checking the real bounding boxes of the existing shapes (including group children) that the target rectangle is free.

    ## 1. Aspect ratio

    `add_picture(path, left, top, width=...)` scales the missing dimension from the image's native size — documented behaviour, and it uses the source pixel ratio, not DPI:

    ```python
    from pptx.util import Inches

    pic = slide.shapes.add_picture("signature.png", left, top, width=Inches(2))
    # pic.height is now native_h/native_w * Inches(2). Do NOT pass height too.
    ```

    If you want the geometry *before* inserting (needed to compute `top`), read the image directly:

    ```python
    from pptx.parts.image import Image as PptxImage

    img = PptxImage.from_file("signature.png")   # .size -> (px_w, px_h)
    px_w, px_h = img.size
    w = Inches(2)
    h = int(round(w * px_h / px_w))
    ```

    Never pass both `width` and `height` unless you computed the matching value, or you distort the signature.

    ## 2. Bottom-right, from the deck's real size

    Read the deck, don't assume 4:3 or 16:9:

    ```python
    sw, sh = prs.slide_width, prs.slide_height   # e.g. 12192000 x 6858000
    margin = Inches(0.25)

    left = sw - margin - w
    top  = sh - margin - h
    ```

    Bottom-right means the **right edge** is `sw - margin` and the **bottom edge** is `sh - margin`, not "left = sw - w" by eye. Also check `top >= 0` and `w <= sw - 2*margin`; shrink proportionally (one scale factor on both `w` and `h`) if 2" cannot fit at all.

    ## 3. Prove it covers nothing

    Collect absolute rectangles for every visible shape and test intersection. `GroupShape` children store coordinates in the group's child space, so map them through `chOff`/`chExt` — do not compare raw child `.left`/`.top` to slide coordinates.

    ```python
    from pptx.enum.shapes import MSO_SHAPE_TYPE

    def flatten(shapes, ox=0, oy=0, sx=1.0, sy=1.0):
        for sh in shapes:
            if sh.left is None or sh.width is None:
                continue
            l, t = ox + sh.left * sx, oy + sh.top * sy
            w, h = (sh.width or 0) * sx, (sh.height or 0) * sy
            yield sh, (l, t, l + w, t + h)
            if sh.shape_type == MSO_SHAPE_TYPE.GROUP:
                x = sh._element.grpSpPr.xfrm
                if x is not None and x.chExt.cx and x.chExt.cy:
                    gsx, gsy = w / x.chExt.cx, h / x.chExt.cy
                    yield from flatten(sh.shapes,
                                       l - x.chOff.x * gsx,
                                       t - x.chOff.y * gsy,
                                       gsx, gsy)

    def hit(a, b, pad=0):
        return not (a[2] + pad <= b[0] or b[2] + pad <= a[0] or
                    a[3] + pad <= b[1] or b[3] + pad <= a[1])

    box = (left, top, left + w, top + h)
    overlaps = [(sh.name, r) for sh, r in flatten(slide.shapes) if hit(box, r, pad=12700)]  # ~1pt slack
    if overlaps:
        ...  # do not insert as-is
    ```

    (Images, tables, charts, connectors all land in `flatten`; `pad` absorbs 1pt strokes/glow so you don't get a "clean" pass that still looks like it touches a rule or table border.)

    ## 4. If it doesn't fit

    Two honest outcomes, in this order:

    1. **Shrink to the free space.** Find `content_bottom = max(r[3] for _, r in flatten(...))`; the largest legitimate height is `sh - margin - content_bottom - gap`. Scale `w` and `h` by the same factor so the ratio survives, then re-run the collision test — shrinking may still collide horizontally if a shape reaches the right edge, so iterate or fail.
    2. **Stop and report.** If the clear bottom-right area cannot hold the signature at a legible size, or two content rects leave no gap, do not silently overlap and do not crop the signature. Say which shape blocks it and ask for direction (move content, different corner, accept a smaller width, or put the signature on a new slide). Zero ambiguity check: if the target slide is not uniquely determined, ask rather than picking `prs.slides[0]`.

    ## 5. Verify after insertion

    ```python
    pic = slide.shapes.add_picture("signature.png", left, top, width=w)
    assert pic.width == w
    assert abs(pic.width / pic.height - px_w / px_h) < 0.01
    assert pic.left + pic.width <= sw and pic.top + pic.height <= sh
    assert not any(hit((pic.left, pic.top, pic.left+pic.width, pic.top+pic.height), r)
                   for _, r in flatten(slide.shapes) if r != (pic.left, pic.top, ...))
    ```

    Also worth knowing: the new picture is appended to `spTree`, so it is on top — fine when it overlaps nothing, but if it has to sit *behind* something, move its `p:pic` element rather than accepting a stack that hides the signature. And if `signature.png` is already used elsewhere in the deck, python-pptx reuses the existing image part/rId, so no duplicate media is written; there is no rel to drop for a clean insert.

    ## Assumptions made (no user available)

    - One explicit target slide is supplied; otherwise this is a "which slide?" question and I would ask.
    - Empty layout placeholders count as content (a signature over a title placeholder still collides in edit view). If you deliberately want to ignore empty body placeholders, drop them from `flatten` before testing and say so.
    - 0.25" margin from both edges; 2" width preserved unless the deck physically cannot hold it, in which case the conflict is reported rather than silently resolved.
    - The signature is a normal picture, not a watermark: no transparency, z-order, or cropping changes requested.

    **Bottom line:** width-only `add_picture` gives the ratio, slide-size arithmetic gives bottom-right, and a group-aware bounding-box intersection test — not eyeballing — is what proves "covers nothing"; if the free area can't hold 2", shrink uniformly or stop and ask.
- id: pptx-imgins-02
  answer: |
    Because `insert_picture()` does **not** scale the photo to fit the frame — it scales it to **cover** the frame, then crops the overflow.

    ## Mechanism

    `PicturePlaceholder.insert_picture()` replaces the empty placeholder with a `PlaceholderPicture`, reusing the placeholder's `<p:spPr>` — so the picture gets the placeholder's fixed position, extents, and geometry (inherited from the layout/master). The photo's aspect ratio is then reconciled with the placeholder's by:

    1. Scaling the image until it covers the placeholder rectangle, and
    2. Writing the excess into `<a:blip><a:srcRect l="…" t="…" r="…" b="…"/>` (crop percentages) on the picture.

    The python-pptx docstring states it plainly: *"The image is cropped to fill the entire space of the placeholder. A portion of the image will not be visible if its aspect ratio differs from that of the placeholder."*

    A portrait photo in the usual landscape picture placeholder (4:3 or 16:9) is scaled to match the width, so its **top and bottom are cropped**. A landscape photo in a portrait/square placeholder loses its left/right edges. Nothing is wrong with the file: the full-resolution image is still embedded in `ppt/media/`, uncropped — the crop is only a display attribute (`srcRect`), and `crop_left/right/top/bottom` are read/write in python-pptx.

    Note that clearing the crop alone isn't a fix: the picture still occupies the placeholder rectangle, so setting all four crop values to 0 stretches/distorts the image to fill the frame.

    ## Ways to keep the whole photo

    1. **Match the frame to the image before inserting** (best for placeholders). Set the placeholder's `width`/`height` to the image's aspect ratio, then insert — no crop is generated:
       ```python
       from PIL import Image
       img_w, img_h = Image.open(path).size
       ph = slide.placeholders[idx]          # must be PP_PLACEHOLDER.PICTURE
       ph.width  = int(ph.height * img_w / img_h)
       pic = ph.insert_picture(path)
       ```
    2. **Insert as a normal picture** instead, sizing one dimension and letting python-pptx derive the other from the native ratio (no crop):
       ```python
       slide.shapes.add_picture(path, left, top, width=frame_w)
       ```
       Then delete/ignore the empty placeholder.
    3. **Post-insert "contain" fit**: clear the crop and re-size the resulting `PlaceholderPicture` preserving `img_w/img_h`, centering it in the old frame (leaves letterboxing rather than cropping).

    Assumption: stock python-pptx 1.0.x behavior; the placeholder is a genuine picture placeholder (`PP_PLACEHOLDER.PICTURE`) whose size comes from the layout, and the image differs in aspect ratio from that frame. If the frame ratio matches the photo, no cropping occurs.
- id: pptx-legacy-01
  answer: |
    **Short answer: no.** `python-pptx` only understands OOXML PresentationML — `.pptx`/`.pptm`, which are ZIP/OPC packages. A `.ppt` is a legacy binary OLE2 compound file (first bytes `D0 CF 11 E0 A1 B1 1A E1`), so `Presentation("deck.ppt")` blows up in `zipfile` (typically `BadZipFile: File is not a zip file`, depending on version) — it is not a supported format, and renaming it to `.pptx` does not help. There is no built-in converter and none is planned.

    ## Workflow

    **0. Verify and back up.**
    ```bash
    file deck.ppt            # "Composite Document File V2" = real .ppt
    xxd -l 8 deck.ppt        # d0cf11e0a1b11ae1 = OLE2
    cp deck.ppt deck.ppt.bak
    ```
    Occasionally a file named `.ppt` is actually a `.pptx` (`50 4B 03 04`); then just copy it to `.pptx` and skip conversion.

    **1. Convert to `.pptx` out-of-place** (never in the same dir, so you never overwrite the original):
    ```bash
    soffice --headless \
      -env:UserInstallation=file:///tmp/lo-profile-$$ \
      --convert-to 'pptx:Impress MS PowerPoint 2007 XML' \
      --outdir /tmp/converted deck.ppt
    # macOS binary: /Applications/LibreOffice.app/Contents/MacOS/soffice
    ```
    The isolated `UserInstallation` profile avoids clashing with a running LibreOffice instance. Alternatives: Windows + PowerPoint COM (`Presentations.Open(...)`, `SaveAs(..., ppSaveAsOpenXMLPresentation)`) gives the best fidelity; Google Slides export works but degrades more. python-pptx is not involved in this step.

    **2. Validate the conversion before touching content.**
    - Open with python-pptx; assert slide count and enumerate shapes/tables; recurse into `GroupShape.shapes`.
    - Compare the target table's first-row header texts and every cell string against the source (extract the source text via `libreoffice --convert-to txt`/PDF, or read it out of PowerPoint if available).
    - Render both decks to PDF/PNG and eyeball the table.

    LibreOffice *will* reflow column widths and row heights, change font metrics/autofit, and can flatten SmartArt/WordArt to images, alter gradients, and drop animations, transitions and OLE objects. Legacy decks are the worst case for this — so the conversion is a lossy intermediate, not a byte-preserving step, and the table geometry may need fixing.

    **3. Edit the `.pptx` with python-pptx.** Table work is XML-level work:
    - Locate the table by header-row content (not the first `has_table` frame), assert exactly one candidate.
    - Match the row on stripped cell text, assert exactly one hit; zero/several → stop and ask.
    - No `add_row()` / `remove_row()`: `new_tr = copy.deepcopy(ref_tr)`, `ref_tr.addnext(new_tr)`, fill through each cell's existing first run, strip extra runs.
    - Finish the edit: recompute the Total/subtotal, `gf.height = sum(r.height for r in table.rows)`, check `gf.top + gf.height` against `prs.slide_height` and the shapes below, re-apply explicit `a:tcPr` banding.
    - Image rels: `drop_rel()` counts only `@r:id`, so `a:blip/@r:embed` always counts 0 — check `//@r:embed`/`//@r:link` yourself, and drop old rels once unused so stale media doesn't stay in `ppt/media/`.
    - Read `prs.slide_width`/`slide_height`; don't assume 4:3 or 16:9.

    **4. Decide the deliverable.**
    - Best: hand back `.pptx` and tell the user the `.ppt` was converted — OOXML is the durable format.
    - If `.ppt` is mandatory: `soffice --headless --convert-to 'ppt:MS PowerPoint 97' --outdir out edited.pptx`. Warn explicitly that this round-trip is lossy (animations, SmartArt, OLE, fonts, chart rendering), that the table may shift again, and that for production fidelity the final save to `.ppt` should be done in real PowerPoint, not LibreOffice.

    Assumptions: no PowerPoint on the editing host, LibreOffice available, and the deliverable can be `.pptx` unless the user insists otherwise (state that assumption when you report back).

    Two things not to do: don't try to unzip the `.ppt` (it's CFB, not ZIP), and don't promise a "clean" edit — everything downstream inherits LibreOffice's rendering choices.
- id: pptx-legacy-02
  answer: |
    ## Before editing (intake checks on the converted file)

    **1. Conversion sanity**

    - Confirm `soffice --convert-to pptx` actually succeeded: file is non-zero, `unzip -t` passes, every part parses, no truncated/empty media.
    - Open with `python-pptx`, count slides, walk every shape, and make sure nothing throws. If python-pptx can't read it, PowerPoint probably can't either — stop.
    - Keep the original `.ppt` untouched and edit a copy of the `.pptx`. The `.ppt` remains the fidelity reference; the converted file is a working copy.

    **2. Fidelity triage — real objects vs. flattened pictures**

    - Inventory every shape per slide, recursing into `GroupShape.shapes` and skipping groups correctly.
    - Classify: real `a:tbl` GraphicFrames (`has_table`), real charts (`has_chart`), pictures, OLE/embedded objects, freeforms.
    - LibreOffice typically flattens or mangles: SmartArt (group of shapes or a picture), charts (often a bitmap), equations/OLE (preview image), WordArt, some tables (drawn lines or an image instead of `a:tbl`), and text split into many small boxes. If the thing you must edit is a picture, you cannot edit its contents — that must go back to the user.
    - Check whether "the table" is actually one table: collect all `has_table` frames and match by header row text, not by "first shape with a table".

    **3. Geometry and slide size**

    - Read `prs.slide_width` / `prs.slide_height`; never assume 16:9 (12192000×6858000) or python-pptx's 4:3 default (9144000×6858000). Conversion can change page size.
    - Look for shapes off-slide, negative coordinates, zero-size frames, and overlapping content — conversion commonly produces these.

    **4. Text, fonts, and fits**

    - Check font names: LibreOffice substitutes fonts not installed on the converting machine, which changes metrics and can cause already-overflowing text.
    - Check autofit state (`a:normAutofit` `fontScale`/`lnSpcReduction`), bullets/numbering (real `a:buAutoNum` vs. bullets typed as literal characters), line spacing, tab stops, and text split across runs/boxes.
    - Verify glyph coverage for special characters (arrows, checkmarks, CJK, emoji) — missing glyphs become boxes.

    **5. Media and relationships**

    - Images may be re-encoded/downsampled or changed format (JPEG→PNG, etc.). Check resolution, transparency, crop, rotation/flip, and aspect ratio.
    - Check rels: duplicate media, orphan rels, external links still pointing at the original author's local paths, and embedded objects replaced by preview images.
    - Remember `drop_rel` only counts `//@r:id`; pictures use `a:blip/@r:embed`, so it always counts 0 and is always dropped. Before dropping any image rel, query the part XML for `//@r:embed` and `//@r:link` equal to that rId.

    **6. Structure that conversion quietly drops**

    - Master/layout rebuilt, theme fonts/colors replaced, placeholders turned into plain text boxes, notes/comments largely preserved but animations, transitions, custom shows, and sections usually lost.
    - Establish a baseline: render the converted deck to PDF/PNG per slide so you can diff after editing.

    ## After editing (validation before handing back)

    - Reopen the saved file with python-pptx and `unzip -t`; confirm PowerPoint would open it without a repair prompt.
    - Re-check geometry on everything you touched: position/size unchanged unless intended; for tables, `gf.height = sum(r.height for r in gf.table.rows)`, and `gf.top + gf.height` within `prs.slide_height` and clear of the shapes below.
    - Recompute totals/subtotals from the rows; re-apply explicit `a:tcPr` banding fills if the table uses alternation.
    - Text fit/overflow re-check, especially with substituted fonts and on rows where you added text.
    - Media hygiene: after deleting/repointing pictures, drop the old rel only if no `//@r:embed`/`//@r:link` still references it; confirm the old image is gone from `ppt/media/` and no dangling rels remain.
    - Render the edited deck to PDF/PNG and visually diff against the baseline; confirm slides you didn't touch are byte-identical in intent (no silent rewrites).
    - Save under a new filename; keep both the original `.ppt` and the pre-edit `.pptx`.

    ## What you must tell the user

    - The deck is a legacy `.ppt` run through LibreOffice, so the `.pptx` is an **approximate working copy**; the original `.ppt` is the source of truth for fidelity. Conversion is lossy by nature.
    - Specifically what got lost or flattened, slide by slide: SmartArt/charts/equations/OLE now images, tables that are drawn lines or bitmaps, text split into multiple boxes, fonts substituted, theme colors/effects approximated, animations/transitions gone, images recompressed. Anything flattened cannot be edited cell-by-cell and needs rebuilding.
    - Font substitution means rendering differs on their machine; they may need the original fonts installed to judge the result.
    - What you changed, on which slides, with what values, and that totals were recomputed — plus the filename you saved and that the original files were preserved.
    - Ask them to open it in PowerPoint (or the original authoring tool) to confirm no repair prompt and acceptable appearance. If pixel-level fidelity matters, edits should be redone in PowerPoint or the original `.ppt` rather than in the converted copy.
    - State assumptions instead of guessing when the conversion leaves ambiguity (several candidate tables, a "table" that is a picture, content pushed off-slide): stop and ask rather than silently restructuring.
- id: pptx-verify-01
  answer: |
    **Short answer:** never overwrite the only copy; save to a candidate path, re-open that candidate from disk and run structural + relationship + visual assertions against it, and only then atomically replace the original while keeping a backup. python-pptx does not validate against the OOXML schema on save, so re-opening and rendering are the real gates, not a clean `prs.save()` return.

    ## Procedure

    **1. Preserve the original**
    - Copy `deck.pptx` to a working path (`deck.bak.pptx` / temp dir) before doing anything.
    - Do all edits in memory (`Presentation(src)`), and write to a **new** path `candidate.pptx` in the same directory (same filesystem, so the final move can be atomic).

    **2. Save**
    ```python
    prs.save(candidate)   # candidate != src
    ```
    A successful save only means the zip was written. It is *not* proof the deck is valid or that your intent landed.

    **3. Package integrity (before trusting the file)**
    - `zipfile.ZipFile(candidate).testzip()` → `None`.
    - `[Content_Types].xml` exists and every part in `namelist()` has a content type (Default by extension or Override). This catches orphan media and missing `png`/`jpeg` defaults after image changes.
    - No zero-byte or duplicated part names.

    **4. Re-open from disk (the real parse gate)**
    ```python
    check = Presentation(candidate)
    ```
    This re-parses every part and relationship. Do the verification assertions on `check`, not on the in-memory `prs` — otherwise you're testing the objects you built, not the file you'll ship.

    **5. Structural assertions on the edit**
    - Locate the target table by **header text**, recursing into group shapes; assert exactly one candidate.
    - Assert the row count changed as intended, and match the edited row by exact stripped cell text; assert exactly one hit.
    - Recompute derived values (Total/subtotal) and assert the cell equals the sum of the rows.
    - **Frame sync:** `gf.height == sum(r.height for r in gf.table.rows)` (allow EMU rounding). The `p:graphicFrame/a:xfrm/a:ext cy` does not follow XML row edits.
    - **Bounds:** `gf.top + gf.height <= prs.slide_height`, and no collision with shapes below (compare against their `top`).
    - Re-alternate explicit `a:tcPr` banding fills if the table uses them; assert the new row's fill matches the pattern.
    - Assert nothing else changed: slide count, shape counts, and the other rows' texts.

    **6. Relationship / media hygiene**
    - Enumerate the edited part's rels. For each image rel you removed, confirm no `//@r:embed` or `//@r:link` in that part still references the rId **before** `drop_rel` — `drop_rel` only counts `//@r:id`, so picture rels always look unused and get dropped even when a second picture shares them.
    - Confirm every rel retained is still referenced, and that `ppt/media/` in the saved zip has no leftovers you meant to remove (python-pptx saves every part reachable via a rel).
    - Confirm every `r:id`/`r:embed` in the part resolves to an entry in the part's `.rels`.

    **7. Visual verification (strongest end-to-end check)**
    ```bash
    soffice --headless --convert-to pdf candidate.pptx
    pdftoppm -png -r 100 candidate.pdf slide
    ```
    Inspect the edited slide for clipping, overflow past the slide edge, broken banding, or a table overlapping the content below. A renderer is stricter than python-pptx's lenient parser and catches geometry the assertions can miss.

    **8. Diff against the backup**
    - Unzip both decks and `diff` the slide XML (`ppt/slides/slideN.xml`), `[Content_Types].xml`, and the `.rels`.
    - Only the intended slide part (plus the table's rels/media) should differ. No changes to masters/layouts/theme unless intended.

    **9. Promote atomically**
    - If and only if steps 3–8 pass: `os.replace(candidate, "deck.pptx")` (atomic on the same filesystem).
    - Keep the backup until the deck has been opened successfully in PowerPoint/Keynote.
    - On any failure: delete the candidate, keep the original untouched, and report which assertion failed.

    ## Assumptions
    - python-pptx (1.0.x) on a normal filesystem; `soffice` available for the render check.
    - "Verify" targets a table row edit (per the active guidance); the same shape of procedure applies to any edit — save to candidate, reopen, assert intent, assert rels/media, render, diff, atomic replace.

    **Bottom line:** a save is not a verification. The safe sequence is *backup → save to a new path → zip/content-type check → reopen from disk and assert structure, totals, frame height, bounds, and rels/media → render and eyeball → diff against backup → atomic replace*. Any failed gate means discard the candidate and keep the original.
- id: pptx-verify-02
  answer: |
    PowerPoint's "repair" prompt means its parser rejected the part XML or the package graph — it is much stricter than python-pptx, which does no schema validation on save. The usual culprits after hand-editing a table:

    ## 1. Element-order violations (most common)
    DrawingML is `xsd:sequence`, so order is mandatory, not cosmetic:
    - `a:tc` must be `a:txBody` → `a:tcPr` → `a:extLst?`. Putting `a:tcPr` first, or appending a fill after `a:extLst`, triggers repair.
    - `a:tr` must be `a:tc+` → `a:extLst?`.
    - `a:tbl` must be `a:tblPr?` → `a:tblGrid` → `a:tr*`. Appending/inserting a row before `a:tblGrid`, or a second `a:tblPr`, breaks it.
    - `a:tblPr` must be `a:tableStyleId?` → `a:extLst?`.
    - `a:tcPr` children are ordered too: `a:lnL`, `a:lnR`, `a:lnT`, `a:lnB`, `a:lnTlToBr`, `a:lnBlToTr`, `a:cell3D`, then one fill (`a:noFill`/`a:solidFill`/…), then `a:headers`, `a:extLst`. Inserting `a:solidFill` before the borders is a classic banding-fill mistake.
    - `a:txBody` must be `a:bodyPr` → `a:lstStyle?` → `a:p+`; `a:r` must be `a:rPr?` → `a:t` (one `a:t` only).

    ## 2. Missing required children or attributes
    - `a:tr/@h` is **required** and must be a plain integer EMU. A missing `h`, `h="0.37in"`, or `h="370840.0"` → repair.
    - `a:tblGrid` is required, with at least one `a:gridCol`, each carrying a required integer `w`.
    - `a:txBody` requires at least one `a:p`. Clearing a cell by deleting all paragraphs (rather than emptying the text) is a frequent repair cause.
    - `a:tr` requires at least one `a:tc`. An empty row, or a table with zero rows, is invalid.
    - `p:graphicFrame` requires `p:nvGraphicFramePr`, `p:xfrm` with `a:off`+`a:ext`, and `a:graphic/a:graphicData/@uri` exactly `http://schemas.openxmlformats.org/drawingml/2006/table`.

    ## 3. Grid and merge inconsistency
    - The sum of `gridSpan` across each row must equal the number of `a:gridCol` entries. A row with too many/few cells relative to the grid → repair.
    - Overlapping or unterminated merges: `rowSpan`/`vMerge` continuation cells missing, `hMerge`/`vMerge` set on a cell that also has a span > 1, spans running past the last column/row.
    - Adding a row by cloning a row that contains merged cells, without fixing the continuation cells in the cloned row.

    ## 4. Duplicate identifiers
    - `a:tc/@id` duplicated (a `copy.deepcopy` of an `a:tr` copies the ids). PowerPoint treats cell ids as unique within the table.
    - `a16:rowId` inside the row's `a:extLst` duplicated by the clone — strip the `extLst` or assign fresh GUIDs.
    - Duplicated `p:cNvPr/@id` on the slide (e.g., cloning a whole graphic frame instead of just the row).

    ## 5. Invalid lexical values / enums
    - Floats, units, or negatives where an integer/`ST_PositiveCoordinate` is required: `cy="-1"`, `cx="0.5"`, `gridSpan="0"`, `rowSpan="-1"`.
    - Booleans written as `"True"` instead of `1`/`0`/`true`/`false`.
    - Bad enum spellings: `algn="center"` instead of `ctr`, `sz` outside 100–400000, invalid `lang`, unknown `a:prstDash` token.
    - Malformed GUID in `a:tableStyleId` (not in `ppt/tableStyles.xml` usually just falls back; a syntactically invalid GUID string repairs).

    ## 6. Namespace problems
    - Elements created by string concatenation or `parse_xml` without `nsdecls`, ending up in the wrong or no namespace (`tbl` instead of `a:tbl`, `p:tcPr`).
    - Writing `p:` prefixes where `a:` is required, or undeclared prefixes left after naive string surgery.
    - `a:extLst` extensions lacking `mc:Ignorable` / proper `mc:AlternateContent` wrapping.

    ## 7. Invalid text content
    - Raw `&` or `<` in `a:t` (must be `&amp;`/`&lt;`).
    - Control characters illegal in XML 1.0 — especially `0x0B` (Word's soft line break), `0x0C`, `0x1F`, `0x00` — pasted into cell text. This is one of the most frequent real-world repairs.
    - Unpaired surrogate / invalid UTF-8 from a bad string write.
    - Newlines written literally inside `a:t` instead of as `a:br`; `a:br` placed outside `a:p`; `a:fld` missing its required `id`/`type`.

    ## 8. Relationship and package damage
    - A dangling `r:id`/`r:embed`: dropping a rel (`slide.part.drop_rel`) while the XML still references it — e.g., a hyperlink `a:hlinkClick/@r:id` or `a:blip/@r:embed` in a cloned row. Dangling references are fatal; orphaned media is not.
    - Rel target pointing at a part that does not exist, or a missing `Override`/`Default` in `[Content_Types].xml`.
    - Duplicate `rId` entries in a `.rels` part; malformed `.rels` XML.
    - Re-saving through a zip tool that leaves two copies of `ppt/slides/slideN.xml`, uses backslashes or leading slashes in entry names, or stores the `__MACOSX`/`.DS_Store` cruft.

    ## 9. Leftovers tied to deleted/replaced objects
    - Deleting a picture or row but leaving `p:timing`, animation targets, or `p14` extension entries that reference the removed shape id / rel.
    - Replacing a table's `a:tbl` wholesale while keeping the old `a:tblPr` style id or a stale `extLst`.

    ## Quick triage
    1. `unzip -o deck.pptx -d x` and run `xmllint --noout` on the slide part — catches well-formedness, escaping, and control characters immediately.
    2. Best real check: Open XML SDK `OpenXmlValidator` (or any ECMA-376 schema validator) on the saved file; it pinpoints element/order/attribute violations that python-pptx will happily write.
    3. Reopen with python-pptx (parses, does not validate) and LibreOffice (lenient) — passing both does **not** mean PowerPoint will accept it.
    4. Bisect: revert the last edit (row clone, rel drop, fill insert) and re-test; then diff the part against a known-good copy.
    5. On Windows, `%LOCALAPPDATA%\Temp\Diagnostics` often holds PowerPoint's repair log naming the offending part.

    For table edits specifically, my ranking of likelihood: element order in `a:tc`/`a:tcPr` → missing `a:tr/@h` or empty `a:p` → gridSpan/gridCol mismatch → duplicated `a:tc/@id`/`a16:rowId` → dangling `r:id`/`r:embed` → control characters in cell text.
- id: pptx-verify-03
  answer: |
    **Short answer: no — deleting the `p:pic` element does not remove the image from the `.pptx`.** The picture element is only a *reference*. The bytes live in a separate part (`ppt/media/imageN.png`) reached through a relationship in `ppt/slides/_rels/slideN.xml.rels`. python-pptx (and PowerPoint) write every part reachable from the package relationship graph, so as long as the slide still has an image rel — even one nothing points at anymore — the media part is still serialized into the saved zip.

    Worse, image parts are **deduplicated by content**: one image added to three slides is one `imageN` part with three rels pointing at it. Dropping the pic on one slide doesn't touch the part while the other two still reference it.

    Also: cropping does not help. python-pptx stores the full original blob; the cropped-away region is still in the file.

    ## How to check

    The zip is the only authoritative answer — not the rendered slide, not `slide.shapes`.

    **1. List and hash the media parts against the original:**

    ```python
    import hashlib, zipfile

    secret_sha = hashlib.sha256(open("confidential.png", "rb").read()).hexdigest()
    z = zipfile.ZipFile("deck.pptx")
    for n in z.namelist():
        if n.startswith("ppt/media/"):
            b = z.read(n)
            print(n, len(b), hashlib.sha256(b).hexdigest() == secret_sha)
    ```

    Any `True` = still there, under whatever name.

    **2. Check the relationships that could still reach it:**

    ```python
    for n in z.namelist():
        if n.endswith(".rels"):
            xml = z.read(n).decode()
            if "media/image" in xml:          # or match the exact target name
                print(n); print(xml)
    ```

    An entry like `<Relationship Id="rId2" Type=".../image" Target="../media/image3.png"/>` in `slideN.xml.rels` is what keeps the part alive.

    **3. Check that no XML part still paints it** — including places people forget:

    ```python
    for n in z.namelist():
        if n.endswith(".xml") and n.startswith("ppt/"):
            xml = z.read(n).decode()
            if "a:blip" in xml:
                print(n, xml.count("a:blip"))
    ```

    Grep for `r:embed` **and** `r:link` (an externally linked image has no media part but leaves the file path in the rels). Check slides, notesSlides, slideLayouts, slideMasters, and background `p:bg/a:blipFill`, not just the slide you edited. `docProps/thumbnail.jpeg` can also carry a preview of the slide.

    **4. Cross-check at the python-pptx level** — this mirrors exactly what will be written:

    ```python
    from pptx import Presentation
    from pptx.parts.image import ImagePart

    prs = Presentation("deck.pptx")
    for part in prs.part.package.iter_parts():
        if isinstance(part, ImagePart):
            print(part.partname, len(part.blob))
    ```

    `iter_parts()` walks the same rel graph the writer uses — anything it yields ends up in the file.

    ## How to actually remove it

    ```python
    rel = slide.part.rels[rId]
    target = rel.target_part

    # count real picture references yourself: drop_rel's ref count only looks at //@r:id
    still_used = any(
        r.reltype == RT.IMAGE and r.target_part is target
        for p in prs.part.package.iter_parts()
        for r in p.rels.values()
        if r is not rel
    )
    if not still_used and not slide.part.element.xpath('.//a:blip[@r:embed="%s"]' % rId):
        slide.part.drop_rel(rId)
    ```

    That `//@r:id` caveat matters: `Relationships._rel_ref_count` counts only `//@r:id`, so an image rel (`a:blip/@r:embed`) always counts 0 and is dropped unconditionally — you have to do the `r:embed`/`r:link` check yourself, on every part that shares the target. Then save and re-run step 1 on the **new** file. If you edited the zip directly with `zipfile` instead of python-pptx, the orphan media is guaranteed to still be there.
```
