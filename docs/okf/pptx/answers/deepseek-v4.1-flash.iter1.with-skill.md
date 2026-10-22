# deepseek-v4.1-flash — pptx answers WITH SKILL (validation of the re-derived skill)

Model: `opencode-go/deepseek-v4.1-flash` (provider-stripped `deepseek-v4.1-flash`),
re-run 2026-10-21 after opencode-go updated the model behind this id
(`model_version: "4.1-2026-10-21"`). This is the with-skill run for this id (any earlier one validated the previous model);
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
    A table is **not a separate part** — it is stored inline in the slide's XML (`ppt/slides/slideN.xml`), inside a `p:graphicFrame` shape in `p:cSld/p:spTree`. The frame carries the position/size and a `a:graphicData` whose `uri` is the DrawingML table namespace; the table itself is the `a:tbl` element. (Charts and SmartArt, by contrast, do live in their own parts.)

    ## Annotated structure

    ```xml
    <p:graphicFrame>                          <!-- the table shape on the slide -->
      <p:nvGraphicFramePr>
        <p:cNvPr id="4" name="Table 3"/>
        <p:cNvGraphicFramePr><a:graphicFrameLocks noGrp="1"/></p:cNvGraphicFramePr>
        <p:nvPr/>
      </p:nvGraphicFramePr>
      <p:xfrm>                                <!-- frame position + OVERALL size -->
        <a:off x="1524000" y="1143000"/>
        <a:ext cx="6096000" cy="1481680"/>
      </p:xfrm>
      <a:graphic>
        <a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/table">
          <a:tbl>
            <a:tblPr firstRow="1" bandRow="1"> <!-- style flags -->
              <a:tableStyleId>{5C22544A-7EE6-4342-B048-85BDC9FD1C3A}</a:tableStyleId>
            </a:tblPr>
            <a:tblGrid>                        <!-- COLUMNS: one gridCol per column -->
              <a:gridCol w="3048000"/>
              <a:gridCol w="3048000"/>
            </a:tblGrid>
            <a:tr h="370840">                  <!-- ROWS: one tr per row -->
              <a:tc>                           <!-- CELLS: one tc per grid column -->
                <a:txBody>                     <!-- TEXT: full text-body model -->
                  <a:bodyPr/><a:lstStyle/>
                  <a:p>
                    <a:r><a:rPr lang="en-US" sz="1800" b="1"/><a:t>Region</a:t></a:r>
                  </a:p>
                </a:txBody>
                <a:tcPr anchor="ctr" marL="91440" marR="91440">  <!-- CELL FORMAT -->
                  <a:solidFill><a:srgbClr val="4472C4"/></a:solidFill>
                </a:tcPr>
              </a:tc>
              <a:tc>...</a:tc>
            </a:tr>
            <a:tr h="370840"><a:tc>...</a:tc><a:tc>...</a:tc></a:tr>
          </a:tbl>
        </a:graphicData>
      </a:graphic>
    </p:graphicFrame>
    ```

    ## Which element holds what

    | Thing | Element / attribute |
    |---|---|
    | Table root | `a:tbl` (no row/column count attribute anywhere) |
    | Table style + flags | `a:tblPr`, `a:tableStyleId` (GUID), `firstRow`/`bandRow`/`firstCol`/`lastCol`/`bandCol` |
    | **Columns** | `a:tblGrid` → one `a:gridCol` each; width in `@w` (EMU) |
    | **Rows** | `a:tr` children of `a:tbl`; height in `@h` (EMU, a *minimum*) |
    | **Cells** | `a:tc` children of `a:tr`; merges via `@gridSpan`, `@rowSpan`, and `@hMerge="1"`/`@vMerge="1"` on covered cells |
    | **Text** | `a:tc/a:txBody` → `a:p` → `a:r` (`a:rPr` formatting) → `a:t`; also `a:br`, `a:fld`, `a:endParaRPr` |
    | Cell fill, borders, margins, alignment | `a:tc/a:tcPr` (`a:lnL/lnR/lnT/lnB`, a fill element, `marL/marR/marT/marB`, `anchor`) |
    | Position / total size | `p:xfrm/a:off` and `p:xfrm/a:ext` on the frame |

    Units are EMU (914 400 = 1 inch, 12 700 = 1 point). Namespaces: `a` = `…/drawingml/2006/main`, `p` = `…/presentationml/2006/main`.

    ## Semantics worth knowing

    - **Dimensions are implied, not declared.** The column count is the number of `a:gridCol`; the row count is the number of `a:tr`. Every row physically contains as many `a:tc` as there are grid columns — a merged cell is a merge origin (`gridSpan`/`rowSpan`) plus covered cells still present with `hMerge`/`vMerge` — so counting `a:tc` will not give the visual column count.
    - **Cell text is not an attribute.** It is a complete DrawingML text body, so a cell can have multiple paragraphs, runs with independent formatting, line breaks, bullets, fields. There is no `text="…"` on `a:tc`.
    - **`a:tr/@h` is a minimum, not the rendered height.** PowerPoint grows rows whose text overflows at layout time. The table's real total height is `p:xfrm/a:ext/@cy`, which is *not* derived automatically — after adding or deleting an `a:tr` in XML the frame keeps the old `cy` until something sets it (python-pptx never does), which is why frame height must be recomputed as `sum(row.height)` after structural edits.
    - **`a:tcPr` children are a fixed sequence** (`lnL`, `lnR`, `lnT`, `lnB`, `lnTlToBr`, `lnBlToTr`, `cell3D`, then the fill, then `headers`, `extLst`). Hand-inserted children out of order can make PowerPoint reject the file.
    - **The table style is only a reference.** `a:tableStyleId` holds a GUID naming a built-in style or one defined in the optional `ppt/tableStyles.xml` part (found via a relationship from the presentation part). Remove that part and the GUID still resolves to a built-in. Cell-level `a:tcPr` fills/borders override the style, so "banding" is often explicit per-cell fill, not just the style flags.
    - **python-pptx mapping:** `shape.has_table` / `shape.table` → `a:tbl`; `table.columns[i].width` → `a:gridCol/@w`; `table.rows[i].height` → `a:tr/@h`; `cell.text_frame` → `a:txBody`; `cell.fill` / `cell.margin_*` / `cell.vertical_anchor` → `a:tcPr`; `graphic_frame.width/height` → `p:xfrm/a:ext`. Its `Table`/`_Row` API is read-only for row/column counts (no `add_row()`), so structural changes are deepcopy-and-`addnext` work on `a:tr`, followed by the frame-height and overflow checks.

    *(Assumption: answering for ECMA-376 PresentationML as produced by PowerPoint and consumed by python-pptx 1.0.x; the element names and nesting are the same in both.)*
- id: pptx-model-02
  answer: |
    **Units: English Metric Units (EMU).**

    In DrawingML — and therefore in python-pptx — every position and size (`a:off x/y`, `a:ext cx/cy`, row heights, font sizes converted to the XML side, etc.) is stored as a signed integer count of **EMUs**. One EMU is 1/914400 inch, so the conversion factors are:

    | Unit | EMU |
    |---|---|
    | 1 inch | 914400 |
    | 1 point | 12700 |
    | 1 cm | 360000 |
    | 1 mm | 36000 |
    | 1/100 mm | 360 |

    In python-pptx these surface as `Emu` values, with the `Length` subclass and helpers `Inches()`, `Pt()`, `Cm()`, `Mm()`, `Emu()`. `prs.slide_width` / `prs.slide_height` (and every shape's `left`, `top`, `width`, `height`) return `Emu` (an `int` subclass), and you can assign any `Length` to them — python-pptx stores the integer EMU. Note a subtlety in python-pptx: writing to `shape.width = Inches(2)` stores the raw EMU int; reading it back gives `Emu`, not `Inches`, so `.inches` / `.pt` / `.cm` properties must be used for human units.

    **Dimensions:**

    - **Default 16:9** (the default `Presentation()` template): **12192000 × 6858000 EMU** = 13⅓ in × 7½ in (33.87 cm × 19.05 cm).
    - **4:3** (legacy default): **9144000 × 6858000 EMU** = 10 in × 7½ in.

    A common trap: the 16:9 width is 12192000, *not* 13333333 (which is a rounded/decimal-inch approximation sometimes seen). Read `prs.slide_width` and `prs.slide_height` rather than assuming.

    Because the frame height (`p:graphicFrame/p:xfrm/a:ext @cy`) is independent of the table row heights, after adding/removing rows you must recompute `gf.height = sum(r.height for r in gf.table.rows)` and then verify `gf.top + gf.height` against `prs.slide_height` (6858000 EMU on 16:9) so the added row doesn't run off the slide or collide with shapes below.
- id: pptx-model-03
  answer: |
    ## Where the logo actually lives

    It is not in the slide part at all. It is inherited from **`slide.slide_layout`** and/or **`slide.slide_layout.slide_master`**, or it is a **background picture** (`p:bg` / `p:bgPr/a:blipFill`) rather than a shape.

    In python-pptx terms:

    ```python
    slide.shapes                              # per-slide shapes only
    slide.slide_layout.shapes                 # layout-level shapes
    slide.slide_layout.slide_master.shapes    # master-level shapes
    slide.background.fill                     # background may be a picture
    ```

    Rendering composites **master → layout → slide**. Only the shapes physically present in `ppt/slides/slideN.xml` show up in `slide.shapes`; everything inherited is resolved by the renderer at draw time, not copied into the slide XML. A logo can therefore appear on every slide while `slide.shapes` is empty of it.

    Assumptions: standard OOXML deck, python-pptx 1.0.x, logo visible on normal slides.

    ## Consequences of editing it there

    1. **The edit is global, not per-slide.**
       - Editing the **master** changes every slide using any layout of that master (that does not suppress master shapes).
       - Editing one **layout** changes only slides using that layout. A deck frequently has several content layouts, so you may need to edit each one, or the master.

    2. **`showMasterSp` can make your edit invisible.**
       `p:sldLayout/@showMasterSp="0"` (or `p:sld/@showMasterSp="0"`) hides master shapes. Slides/layouts with that flag will not show the modified master logo. Conversely, a layout or slide that already contains its own copy will now *double* the logo, or shadow your change.

    3. **You cannot select it in Normal view.**
       In PowerPoint you must use **View → Slide Master** (or **Slide Layout**) to touch it. In python-pptx you must reach it through `slide_layout` / `slide_master`; changing it through a slide handle is impossible because there is no such shape object on the slide.

    4. **Relationships and media are owned by the layout/master part.**
       The logo's `a:blip/@r:embed` lives in `ppt/slideLayouts/slideLayoutN.xml` or `ppt/slideMasters/slideMaster1.xml` (and its rels / `ppt/media/`). Replacing the image there changes every inheriting slide at once. If you repoint or delete the picture, the old media part stays in the saved package unless you drop the now-unused rel — and `drop_rel` only counts `//@r:id`, so an image rel (`r:embed`) always counts 0 and looks unused. Verify `//@r:embed` / `//@r:link` in that part yourself before dropping, then confirm the old bytes are gone from `ppt/media/*` after save.

    5. **Z-order and occlusion change character.**
       Master/layout shapes render **behind** slide shapes. Moving or enlarging a master logo can be covered by slide content, or cover placeholders depending on the layout's z-order. Title/content placeholders on the master define the stacking; the logo's position in `spTree` matters.

    6. **Placeholders are a trap.**
       If the logo is a **picture placeholder** on the layout/master, it renders as an empty "click icon to add picture" prompt on slides, and the visible logo is a per-slide picture inserted into that placeholder — editing the placeholder won't change those. If it is a normal picture shape or a shape with a picture fill, the inherited edit does propagate.

    7. **Per-slide overrides persist.**
       Any slide that promoted/duplicated the logo into its own `spTree` won't follow the master/layout edit. You get a mixed deck unless you find and remove the slide-level copies too.

    8. **Deletion is not proof of removal.**
       Removing it from the master still leaves it visible wherever a layout has its own copy (or where a slide has one). To prove removal you must re-open the saved file and check the layout, master, and every slide part, plus `ppt/media/*` for the old image bytes.

    **Bottom line:** the logo is inherited from the slide layout/master (or is a background fill), so editing it is a deck-wide change governed by inheritance and `showMasterSp` — not a local slide edit — and image replacement there carries rel/media cleanup that must be verified in the saved package.
- id: pptx-locate-01
  answer: |
    ## Finding it by content, not by position

    ```python
    from pptx import Presentation

    def walk(shapes):
        """Yield every leaf shape, descending into groups."""
        for sh in shapes:
            if getattr(sh, "shapes", None) is not None:  # GroupShape
                yield from walk(sh.shapes)
            else:
                yield sh

    prs = Presentation("deck.pptx")
    slide = prs.slides[2]                       # slide 3, zero-based

    # 1. Collect every table on the slide (including inside groups)
    tables = [sh.table for sh in walk(slide.shapes)
              if getattr(sh, "has_table", False) and sh.has_table]

    # 2. Identify the right table by its HEADER row
    def headers(tbl):
        return tuple(c.text_frame.text.strip() for c in tbl.rows[0].cells)

    candidates = [t for t in tables if headers(t) == ("Item", "Qty", "Price")]
    assert len(candidates) == 1, f"expected exactly 1 matching table, got {len(candidates)}"
    table = candidates[0]

    # 3. Locate the label column from the header, don't assume col 0
    label_col = headers(table).index("Item")

    # 4. Find the row by exact cell text; collect all hits, assert one
    hits = [(i, row) for i, row in enumerate(table.rows)
            if row.cells[label_col].text_frame.text.strip() == "Gadget D"]
    assert len(hits) == 1, f"expected exactly 1 'Gadget D' row, got {len(hits)}"
    row_index, row = hits[0]
    ```

    `row` is a `_Row`; `table.rows` is a read-only sequence, so use `row_index` for later edits (or keep the `row`/its `tr` XML element).

    ## Why each step matters

    - **Recurse into groups.** A table inside a `GroupShape` never appears in `slide.shapes` directly. A plain `next(sh for sh in slide.shapes if sh.has_table)` also silently picks the *first* table, which may be a different one.
    - **Match the header, not the position.** `<a:tbl>` can be the first, second, or third table, and grouped shapes reorder things. Header text is a stable key; position isn't.
    - **Assert uniqueness.** Zero matches means wrong deck/slide or a renamed header; two or more means the header alone isn't a key. Stop and ask rather than editing the wrong table.
    - **Derive the column from the header.** Hard-coding `cells[0]` breaks when the table's columns are reordered or the first column is a merged banner.
    - **Normalize with `.strip()` and use `cell.text_frame.text`**, not `cell.text`/runs. The visible string can be split across multiple runs (`"Gadget "` + `"D"`), and `text_frame.text` joins paragraphs with newlines — so compare stripped, and if the cell can be multi-paragraph, collapse whitespace:
      ```python
      def norm(cell):
          return " ".join(cell.text_frame.text.split())
      ```

    ## Traps to know about

    - **Merged cells duplicate text.** With a vertical merge, the origin's text is returned by every spanned `_Row`. Deduplicate by checking the cell's `tc` element identity, or skip spanned cells:
      ```python
      hits = [(i, r) for i, r in enumerate(table.rows)
              if not r.cells[label_col].is_spanned
              and r.cells[label_col].text_frame.text.strip() == "Gadget D"]
      ```
      Conversely, if the label cell is itself the merge origin, `is_spanned` is `False` and you get one hit — which is what you want.
    - **A merged banner row** (e.g., a title spanning all columns above the real header) means `rows[0]` is not the header. In that case match on the row whose cells contain the expected column names, and read the label column from there.
    - **`has_table` is safe on any shape** (False when not a graphic frame with a table), so `if sh.has_table` needs no type check.
    - **The frame is separate from the table.** `table.rows` reflects the XML by height, but if you go on to insert/delete rows, the surrounding `p:graphicFrame` extents do not follow — you must resync `graphic_frame.height` manually. Not needed for lookup alone.
- id: pptx-locate-02
  answer: |
    ## How the merge is represented

    There is no `<mergedCell>` element. The merge is encoded on the individual `a:tc` elements, and the row still contains one `a:tc` per grid column:

    ```xml
    <a:tr h="370840">
      <a:tc gridSpan="2">                 <!-- origin: holds "Total" -->
        <a:txBody>…<a:t>Total</a:t>…</a:txBody>
        <a:tcPr/>
      </a:tc>
      <a:tc hMerge="1">                   <!-- covered: empty placeholder -->
        <a:txBody/>                       <!-- often present but empty; may be omitted -->
        <a:tcPr/>
      </a:tc>
      <a:tc>…<a:t>123</a:t>…</a:tc>       <!-- value cell = third grid column -->
    </a:tr>
    ```

    - **Origin** cell carries `gridSpan="2"` (add `rowSpan="N"` for a vertical span). **Covered** cells carry `hMerge="1"` and/or `vMerge="1"`.
    - A 2×2 merge puts `gridSpan="2" rowSpan="2"` on the origin, `hMerge="1"` (plus a span count) on its horizontal neighbour, `vMerge="1"` (plus a span count) below, and both markers on the diagonal — so covered cells can themselves carry `gridSpan`/`rowSpan`. Check `is_spanned` before trusting attributes.
    - Column widths still come from `a:tblGrid/a:gridCol/@w`. A merged cell's visual width is the sum of the grid columns it covers; row heights are unchanged.
    - `len(a:tr/a:tc)` must still equal the number of `a:gridCol`s. Merging never removes cells.

    **python-pptx surface:** `cell.is_merge_origin`, `cell.is_spanned`, `cell.span_width`, `cell.span_height`, `cell.merge(other)`, `cell.split()`. `table.cell(r, c)` and `row.cells[c]` index the **grid**, so a merged-away position returns a `_Cell` wrapping the `hMerge` tc, not the origin. Reading text there yields `""`; the rendered text is only the origin's `txBody`.

    ## What to watch when editing around it

    1. **Edit the origin only.** The Total label lives in `tc[0]`; `tc[1]` is a placeholder. Writing text to `tc[1]` either raises (some versions guard `text_frame`) or writes a `txBody` that PowerPoint ignores — nothing appears. Likewise, `row.cells[total_idx + 1]` lands on the covered cell, not the value. Use `total_idx + cell.span_width`, or scan forward for the first `not c.is_spanned`.
    2. **Preserve the origin's run formatting.** Set `runs[0].text` rather than replacing the whole text frame, or you lose the deck's label styling.
    3. **Row insert via `copy.deepcopy(ref_tr)`** clones the merge flags if the reference row contains a merge (the Total row does). Strip `gridSpan`/`rowSpan`/`hMerge`/`vMerge` from the copy and give every grid column a plain `a:tc`; otherwise you get two merges, overlapping spans, or a row whose tc count no longer matches the grid.
    4. **Row delete.** Removing a horizontal-only merge row deletes origin and covered cells together — clean. But if the row participates in a vertical merge, `vMerge` cells in other rows are orphaned with no origin; split them, delete them, or promote a new origin with the correct `rowSpan`.
    5. **Insert/delete rows inside a vertical span.** Increment/decrement the origin's `rowSpan` and add/remove `vMerge="1"` placeholders in the new/removed row. `rowSpan` counts grid rows, not cells.
    6. **Column edits.** python-pptx has no add/delete column either. To delete one: drop the matching `a:gridCol`, drop the corresponding `a:tc` from every `a:tr`, and decrement `gridSpan` on any origin whose span crossed the removed column. A merge that loses its origin must be re-anchored or split. Inserting is the mirror (increment spans crossing the insertion point).
    7. **Never let tc count drift from grid column count** — PowerPoint will offer to repair the file.
    8. **Merge/unmerge API.** `merge()` requires a rectangular range with `self` as top-left; merging over an existing merge creates invalid overlap. `split()` is only valid on the origin; calling it on a spanned cell is an error.
    9. **Fills/banding.** A fill on the origin covers the whole merged area; a fill on a covered cell does nothing visible. If re-banding rows, apply `a:tcPr` fills on the origin.
    10. **Verify after saving.** Save to a new file, reopen, and check: the label appears exactly once in the slide XML (a duplicate `Total` means you wrote both origin and covered), the origin still has `gridSpan="2"`, and every `a:tr` still has `len(tc) == len(gridCol)`.
- id: pptx-rowdel-01
  answer: |
    ## Short answer

    No. python-pptx (through 1.0.2) has **no** `table.rows.remove()`, no `rows.delete()`, and no row-deletion method at all. `table.rows` is a read-only `Sequence` (`__getitem__`, `__len__`, iteration) — same story as `table.columns`. There is also no `table.add_row()` / `add_column()`. Row structure is edit-through-XML only.

    ## How to actually delete a row

    Reach the underlying `a:tr` element and remove it from its parent `a:tbl`:

    ```python
    from pptx import Presentation

    def delete_row(table, row_idx):
        tbl = table._tbl                 # <a:tbl>
        tr  = table.rows[row_idx]._tr    # <a:tr>
        tbl.remove(tr)                   # lxml: drop element from <a:tbl>
    ```

    That's the whole deletion. `table._tbl` is an lxml element, so `remove()` is plain lxml, not a python-pptx API. (Older snippets use `table._tbl.remove(table.rows[i]._tr)` — identical.)

    ## What that leaves broken — fix it yourself

    python-pptx does not follow the XML edit, so several things stay stale:

    **1. The graphic-frame height.** `p:graphicFrame/p:xfrm/a:ext/@cy` keeps the old total. Recompute from the surviving rows:

    ```python
    gf = table._graphic_frame
    gf.height = sum(r.height for r in table.rows)   # Emu
    ```

    Then check `gf.top + gf.height` against `prs.slide_height` and against any shape sitting below the table (move those or flag it). Note `Table` itself has no `height` setter — go through `_graphic_frame`.

    **2. Totals/subtotals.** If the table has a Total row, its cached text is now wrong; recompute the numeric cells from the remaining rows and write them into the existing runs (`cell.text_frame.paragraphs[0].runs[0].text`, dropping extra runs).

    **3. Banding.** If the table uses explicit `a:tcPr` fill on alternating rows (common in generated decks), the stripes do not re-alternate automatically after a delete — re-apply the fills if the look matters.

    **4. Never delete the last row.** The schema requires at least one `a:tr` (`minOccurs=1`); an empty `a:tbl` produces a file PowerPoint treats as corrupt. Guard with `if len(table.rows) <= 1: raise`.

    **5. Verify in the saved file.** Save to a new path, reopen, and confirm the removed row's cell text is gone from the slide part — e.g. scan `ppt/slides/slide*.xml` for the text rather than trusting the in-memory object. If you delete a picture in the same pass, also remember that `part.drop_rel(rId)` only counts `//@r:id`, so image rels (`a:blip/@r:embed`) always look unused and get dropped even when still referenced — check `//@r:embed` yourself first, and remember an undeleted rel keeps the media part alive in `ppt/media/`.

    ## If you need insertion while you're in there

    Same mechanism, mirrored: `copy.deepcopy(ref_tr)` then `ref_tr.addnext(new_tr)`, fill each cell through its existing first run, remove surplus runs, then run the same height/total/banding follow-ups above.

    ## Assumptions

    Stated for the record: python-pptx 1.0.2, a normal `GraphicFrame`-held table on a slide, and a table with at least two rows (so deletion is legal). If your version is newer, check `hasattr(table.rows, "remove")` — but I'm not aware of any release that added one.
- id: pptx-rowdel-02
  answer: |
    Yes — it matters. `p:graphicFrame/p:xfrm/a:ext/@cy` is the stored height of the frame shape, and that is what PowerPoint (and LibreOffice, image exporters, anything doing layout) uses for the shape's bounding box: selection handles, click target, spacing/overlap calculations, and positioning of anything below it. A `a:tbl` has no aggregate height attribute of its own — each `a:tr` carries `@h` — so after you `remove()` a row, the rows' sum shrinks but the frame keeps the old `cy`. The result is a frame taller than the visible table: dead space, a too-large selection box, and shapes below still laid out against the old bottom. PowerPoint may silently recompute on some render paths, but that is not guaranteed, and other consumers won't.

    Fix: recompute from the rows and write it back through python-pptx (the `height` setter writes `a:ext/@cy` in EMU):

    ```python
    gf  = shape                 # the GraphicFrame you located by header content
    tbl = gf.table
    # ... remove the a:tr ...
    gf.height = sum(tr.height for tr in tbl.rows)
    ```

    Same step is required after an insert (`addnext(copy.deepcopy(ref_tr))`), just in the other direction.

    Finish the operation, don't stop at `cy`:

    1. Recompute any Total/subtotal cells from the surviving rows.
    2. Set `gf.height = sum(...)` as above.
    3. Check `gf.top + gf.height` against `prs.slide_height` and against the tops of shapes below; move them (or ask) if the frame now collides or leaves a gap.
    4. If the table uses explicit `a:tcPr` banding fills, re-alternate them so the zebra pattern stays correct after the row disappears.

    Verify in the saved file, not in memory: save to a new path, reopen, confirm `shape.height == sum(r.height for r in shape.table.rows)` and that the removed row's text is gone from every `ppt/slides/slide*.xml`.

    Caveat: `a:tr/@h` is a minimum height in PowerPoint — text wrapping can make rendered rows taller than the declared value. Summing row heights is the right way to keep the frame consistent with the XML, but if exact pixel bounds matter you must also control row heights/autofit (or measure text) rather than trusting the sum to equal the final rendered height.
- id: pptx-rowdel-03
  answer: |
    Short answer: because "alternating shading" is not one thing. A table can get its stripes from two different places, and only one of them survives a row delete gracefully.

    **1. Style-driven banding (survives the delete).** The table carries `a:tblPr` with `bandRow="1"` (and usually `firstRow="1"`) plus an `a:tableStyleId` pointing at a table style part whose conditional formats `band1Horz` / `band2Horz` define the two fills. Here the colour is not stored on the row at all — the renderer computes parity from each row's ordinal position inside the band region at paint time. Delete an `a:tr` and the remaining rows are re-indexed, so the stripes re-flow and stay alternating. Nothing to fix.

    **2. Baked per-cell fills (breaks on delete).** Each row's cells carry their own `a:tcPr/a:solidFill`. The colour is frozen onto that row. In an alternation, rows two apart share a colour (row1 white, row2 grey, row3 white). Delete the middle row and row1 and row3 land next to each other with the *same* fill — a doubled band at the seam. No renderer rule repairs this, because as far as the file is concerned every cell was explicitly told its colour.

    So the same edit is invisible in deck A and obviously wrong in deck B depending on which mechanism that deck uses. Common sources of baked fills:

    - Exports/round-trips through Google Slides, LibreOffice, or other tools that flatten table styles into direct formatting.
    - Decks where someone manually filled cells (or filled a row to "fix" it once), which leaves mixed provenance: some rows explicit, some inheriting. Then the seam shows up only at those rows.
    - Rows copied from another table/slide — the deepcopy brings the source row's `tcPr` fill along.
    - Decks where `bandRow` is `0` (Banded Rows unchecked) and the author hand-painted the stripes.

    A related, subtler variant: `firstRow="1"` offsets the band origin, and horizontal merges (`gridSpan`) collapse cells so one fill covers what looked like two bands — worth checking if the deck's striping doesn't line up with row parity at all.

    **Practical fix:** don't trust the existing fill. Enumerate the body rows in order, then either (a) strip `a:tcPr` fills and let `bandRow="1"` do the work, or (b) explicitly re-alternate: set the fill on every body row from parity, and clear any stray explicit fill so nothing overrides it. Same discipline applies when inserting a row — `copy.deepcopy(ref_tr)` copies the reference row's fill, which is only correct if the insert lands on the same parity; otherwise re-band after the insert.
- id: pptx-rowdel-04
  answer: |
    Short answer: those tricks change **pixels**, not the **table**. The row never leaves the file, so everything that reads structure or text still sees it — and you've paid for the illusion with fragile formatting and a layout that no longer adds up.

    **The row is still there**
    - A white rectangle and white text don't touch `a:tbl`. The `a:tr` is still in `ppt/slides/slideN.xml`, with the same row count and the same index. Any code that iterates `table.rows`, any check that proves a row was removed by scanning the slide XML, and any "does this file still contain X" audit will still find it.
    - The cell text is unchanged. Copy/paste into Excel, "Save as CSV", text extraction, Find, the PDF text layer, screen readers and narration, and grep on the saved file all still return it. White is a color, not absence. If the row held confidential data, it is still shipped.
    - The row still participates in structure: column grid, cell margins, banding alternation, any Total/subtotal that was computed by hand, and row-index math. You've made the table lie about its size.

    **Why the two hacks fail on their own terms**
    - *White rectangle*: it's a separate floating shape, not clipped to or anchored to the table. Move or resize the table, insert/delete a row above, change the layout, and the rectangle stays where it was. It only works on a white background — a slide fill, a theme change, dark mode, high-contrast/accessibility mode, or a different template exposes the row again. It also steals clicks, can land in the wrong z-order, and travels into animations, selections, and exports as junk.
    - *White text*: same background problem, plus it overwrites the original color so you can't easily restore it, and it's invisible only to sighted users on that one background. High-contrast mode and dark themes routinely reveal it.
    - *Near-zero height*: you can't actually zero it. `a:tr@h` plus cell margins and the text itself leave residual height, and the text can spill from the shrunken cell and paint over neighboring content. Worse, the frame's `a:ext cy` no longer equals the sum of row heights, so shapes below either gap or overlap.

    **Why it's the wrong operation conceptually**
    Deleting a row is a **structural edit** to a shared model: the row, its cells, its content, its formatting, and its slot in the grid all go, and the remaining rows, totals, and frame height are recomputed to match. Masking is a **presentation edit** on one rendering of that model. Those are different layers, and the model is what gets copied, exported, diffed, searched, re-rendered, and read by collaborators and tools. A masking hack also makes review and undo dishonest: the diff shows "changed a fill/height," not "removed a row," so nobody can tell what actually happened.

    **Do this instead:** remove the `a:tr` from the table, recompute any totals, set `graphic_frame.height = sum(r.height for r in table.rows)`, re-apply explicit banding fills, and check the new bottom against `prs.slide_height` and the shapes below. Then prove it in the saved file: reopen it and confirm the removed text is absent from every `ppt/slides/slide*.xml`.

    *Assumption: "wrong" means it fails to accomplish the intent (the row is gone) and silently breaks the table's data/structure contract — not merely that it's inelegant.*
- id: pptx-cell-01
  answer: |
    ## What happens

    `cell.text = "SKU"` is shorthand for `cell.text_frame.text = "SKU"`. The `TextFrame.text` setter **clears the text body** — it removes every `a:p` (paragraph) and therefore every `a:r` (run) inside `a:txBody` — then creates one fresh paragraph with one fresh run holding `"SKU"`.

    Formatting lives on the *run*, in `a:r/a:rPr` (`b="1"`, `sz="1400"`, the white `a:solidFill`, typeface, etc.). Those elements are destroyed with the old runs, and the new run has **no `a:rPr` at all**, so the text falls back to inherited defaults (theme/table-style font — typically not bold, not 14 pt, not white). Paragraph-level properties (`a:pPr`: alignment, indent, bullets) are lost too, because the paragraphs themselves are recreated.

    What survives: anything in `a:tcPr` — cell fill, borders, margins, vertical anchor — plus column widths and row heights. Only the *text* formatting is lost.

    ## How to keep it

    Don't assign to `cell.text` (or `text_frame.text`). Edit the existing run so its `a:rPr` stays put:

    ```python
    tf = cell.text_frame
    p = tf.paragraphs[0]

    if p.runs:
        p.runs[0].text = "SKU"          # keeps that run's bold/size/color
        for r in p.runs[1:]:            # drop leftover runs (old text/formatting)
            r._r.getparent().remove(r._r)
    else:
        run = p.add_run()               # empty cell: nothing to inherit
        run.text = "SKU"
        run.font.bold = True
        run.font.size = Pt(14)
        run.font.color.rgb = RGBColor(0xFF, 0xFF, 0xFF)
    ```

    Caveats:

    - **Multiple runs / mixed formatting**: the original text may be split (e.g. bold `"SK"` + plain `"U"`). Setting `runs[0].text` keeps only the *first* run's formatting and you must delete the rest, as above, or decide which run's formatting should win.
    - **Multiple paragraphs**: delete `tf.paragraphs[1:]` via their `_p` elements if you want a single-line cell.
    - **Header cells with `endParaRPr`** (formatting carried on the paragraph mark for an empty/typed-at-end cell): for a non-empty header this is normally irrelevant, but if you must preserve it, deep-copy the old `a:rPr` and re-insert it as the first child of the new run — messier and rarely needed; prefer the run-level edit.
    - If you truly must use `cell.text`, snapshot the formatting first (`deepcopy(run._r.get_or_add_rPr())`) and reapply it to the new run's `_r` before saving, but that is strictly more fragile than editing the run in place.

    The same rule applies everywhere in python-pptx: `.text = ...` on a shape, text frame, or cell rebuilds the runs and loses run formatting; `.runs[i].text = ...` preserves it.
- id: pptx-cell-02
  answer: |
    ## What PowerPoint does (the default behavior)

    A table cell has **no shrink-to-fit by default**. When you put a long string into a cell:

    - **Columns do not widen.** Column geometry lives in `a:tbl/a:tblGrid/a:gridCol/@w`. PowerPoint never re-derives it from content, so the long name stays inside the same narrow column and **wraps** at word boundaries.
    - **The row grows downward.** `a:tr/@h` is a *minimum*, not a fixed height. Once the wrapped text needs more lines, PowerPoint increases that row's rendered height at layout time. `row.height` in python-pptx will still report the stale `h` you wrote, and the saved XML keeps the old `h` until PowerPoint itself re-saves the file.
    - **The table bottom moves down.** Total height = sum of rendered row heights, so the table now extends past where it used to end — over the shape below, or off `prs.slide_height`.
    - **The frame extent goes stale.** `p:graphicFrame/p:xfrm/a:ext/@cy` is not recomputed by python-pptx (and PowerPoint recomputes it lazily). So the selection box and the rendered table disagree, and anything you positioned using the old bottom is now wrong.
    - **An unbreakable token can escape the cell.** PowerPoint does not hyphenate or break mid-word by default, so a single long word wider than the column will overflow horizontally past the cell edge rather than wrap — it can visually collide with the next column's text.
    - Vertical anchor and cell margins stay as they were; extra lines just stack inside the cell, growing the row.

    ## How to control it

    **1. Give the column the space (the real fix).**
    ```python
    table.columns[0].width = Emu(...)   # sum of widths must stay == table width
    ```
    Reallocate `gridCol` widths so the name column takes space from a slack column, and keep `sum(col.width) <= prs.slide_width - left - right`. If the table is already full-width, you must either shrink other columns or accept multi-line rows.

    **2. Shrink the text instead of the row.** Reduce the font on the *existing* runs, not by assigning `cell.text` (that blows away run formatting):
    ```python
    p = cell.text_frame.paragraphs[0]
    p.runs[0].text = "Long Product Name"
    for r in p.runs:            # or just runs[0]
        r.font.size = Pt(10)
    ```
    Also claw back space with tighter cell margins (defaults are 0.1" left/right, 0.05" top/bottom) via `cell.margin_left/right/top/bottom`.

    **3. Turn wrapping off only if you accept overflow.** `cell.text_frame.word_wrap = False` keeps the row at its minimum height but lets text spill horizontally — a visual fix, not a layout fix. `a:normAutofit` (shrink-on-overflow) can be written into the cell `txBody/bodyPr`, but PowerPoint's UI doesn't expose it for table cells and it is unreliable, so don't depend on it.

    **4. Re-sync the frame and everything under it.** After any insert/delete/rename that changes row count or heights:
    ```python
    gf.height = sum(r.height for r in table.rows)
    assert gf.top + gf.height <= prs.slide_height
    ```
    then check/move the shapes below and re-apply explicit `a:tcPr` banding fills if the table uses them. For a *rename* the stored `h` values are unchanged, so also estimate the extra lines and bump the affected row's `h`, or the frame math will still be short.

    **5. If the cell truly must look fixed**, the honest options are: shorten the display name, insert a manual line break, move the long text to a footnote/callout textbox, or rebuild the row as shapes rather than forcing a table cell.

    **Verification:** the height is decided at render time, so inspect the actual rendering (open in PowerPoint / render to image) — a re-opened file's `a:tr/@h` will *not* show the grown height until PowerPoint re-saves it. Confirm the table's new bottom doesn't overlap the next shape or cross `slide_height`.

    Assumption: the table is a plain `GraphicFrame` table on a slide (no merged `gridSpan`/`rowSpan` on the edited row); merged cells would add their own width/height coupling.
- id: pptx-cell-03
  answer: |
    Assumption: the table has a Unit Price and an Amount/line-total column, plus a Total row, and the deck may repeat those figures elsewhere. I can't see the actual deck, so this is the dependency chain any correct edit has to walk.

    **What a Qty change 5 → 12 forces you to update**

    1. **That row's extended amount** — `Amount = Qty × Unit Price`. Qty alone is not the edit; the row's money cell is now stale. Reformat with the same number format/currency as the neighbouring rows (don't paste raw `12` over a formatted cell and lose decimals/currency).
    2. **The Total row (and any subtotal)** — recompute from the row values, not by adding the delta to the old total. If subtotal/tax/discount rows exist, they cascade too. Never hand-edit the total independently of the rows.
    3. **Any other place in the deck that quotes that number** — summary/exec slide KPI text, "12 units" callouts, chart data or embedded chart workbooks, speaker notes, appendix tables. A Qty change on slide N is usually mirrored somewhere; grep the saved file for the old value and the old total and fix each hit. This is the step people skip.
    4. **Nothing geometric unless the row geometry changed** — a numeric 5→12 doesn't add rows, so the frame height and `a:tcPr` banding are normally untouched. If you *do* add/remove rows, then you must recompute `gf.height = sum(r.height for r in table.rows)`, check the new bottom against `prs.slide_height` and the shapes below, and re-apply alternating banding.
    5. **Verify in the saved file, not in memory** — save to a new path, reopen, and confirm the row shows 12, the amount and total are consistent, and no stale copy of the old total remains anywhere in `ppt/slides/*.xml` or the charts.

    **Short version:** update the line amount, recompute the Total from the rows, and propagate the changed figures to every other slide/chart/note that repeats them — then reopen the saved file to prove it. Frame height and banding only come into play if the row count changed.

    If the deck stores the total as a hard-coded number rather than a formula, there is no automatic propagation — every derived figure is a manual edit and a manual check.
- id: pptx-relayout-01
  answer: |
    ## Short answer

    python-pptx has no `table.add_row()`. You add a row by cloning the `<a:tr>` of “Service G”, splicing it in immediately after that row (so it lands before Total), then refilling the cloned runs. After that, the insert is **not done** until you recompute the Total, resize the `p:graphicFrame`, and fix banding/overflow.

    Assumptions below: the label is column 0, “Total” is the row immediately after “Service G”, and the cells contain plain text runs.

    ## Step 1 — find the right table and the right row

    Do not take the first `has_table` shape. Match the header, recurse into groups, and assert uniqueness.

    ```python
    import copy
    from pptx import Presentation
    from pptx.util import Emu
    from pptx.enum.shapes import MSO_SHAPE_TYPE
    from pptx.oxml.ns import qn

    prs = Presentation(src)
    slide = prs.slides[k]

    def tables_in(shapes):
        for sh in shapes:
            if sh.shape_type == MSO_SHAPE_TYPE.GROUP:
                yield from tables_in(sh.shapes)
            elif getattr(sh, "has_table", False):
                yield sh

    HEADER = ("Service", "Qty", "Rate", "Amount")   # exact first-row texts
    cands = [
        gf for gf in tables_in(slide.shapes)
        if tuple(c.text_frame.text.strip() for c in gf.table.rows[0].cells) == HEADER
    ]
    assert len(cands) == 1, f"expected 1 table with header {HEADER}, got {len(cands)}"
    gf = cands[0]
    table = gf.table

    def find_row(label):
        hits = [i for i, r in enumerate(table.rows)
                if r.cells[0].text_frame.text.strip() == label]
        assert len(hits) == 1, f"{label!r} matched {len(hits)} rows"
        return hits[0]

    i_ref = find_row("Service G")
    i_tot = find_row("Total")
    assert i_tot == i_ref + 1, "Total is not directly after Service G — stop and ask"
    ```

    ## Step 2 — clone and splice the `<a:tr>`

    `copy.deepcopy` preserves the row height (`a:tr/@h`), every `a:tcPr` (fill, borders, margins, vertical alignment), and the existing `a:pPr` / `a:rPr` of the runs — which is exactly what “keeping the formatting” means here.

    ```python
    old_h = gf.height
    n_rows_before = len(table.rows)

    ref_tr = table.rows[i_ref]._tr
    new_tr = copy.deepcopy(ref_tr)
    ref_tr.addnext(new_tr)          # after Service G, before Total

    # rows are rebuilt from tr_lst, so re-read by index
    new_row = table.rows[i_ref + 1]
    assert len(table.rows) == n_rows_before + 1
    ```

    Do **not** use `table._tbl.append(new_tr)` — that puts the row after Total — and do not insert before `a:tblPr`.

    ## Step 3 — refill the runs, not the cell

    `cell.text = ...` / `text_frame.text = ...` deletes all paragraphs and runs and throws away run-level formatting. Edit the first run’s text and delete the extras, so the cloned `a:rPr` survives.

    ```python
    def set_cell(cell, text):
        tf = cell.text_frame
        for extra in list(tf.paragraphs[1:]):
            extra._p.getparent().remove(extra._p)
        p = tf.paragraphs[0]
        if p.runs:
            p.runs[0].text = text
            for r in list(p.runs[1:]):
                r._r.getparent().remove(r._r)
        else:
            # source cell had no run: nothing to inherit from; inherits table style
            p.add_run().text = text

    NEW_VALUES = ("Service H", "1", "0.00", "0.00")   # one per column
    for cell, text in zip(new_row.cells, NEW_VALUES):
        set_cell(cell, text)
    ```

    If the original cells had rich text/multiple runs on purpose, cloning a data row will not reproduce that intent — stop and ask rather than flattening it.

    ## Step 4 — recompute the Total (indices shifted)

    The insert was before Total, so Total moved from `i_tot` to `i_tot + 1`. Re-find it.

    ```python
    i_tot = find_row("Total")        # recompute after the insert

    amount_col = 3
    vals = []
    for r in table.rows[1:i_tot]:
        txt = r.cells[amount_col].text_frame.text.strip()
        txt = txt.replace("$", "").replace(",", "")
        if txt:
            vals.append(float(txt))

    total = sum(vals)
    prev = table.rows[i_tot].cells[amount_col].text_frame.text.strip()
    prefix = "$" if prev.startswith("$") else ""
    decimals = 2 if "." in prev else 0
    set_cell(table.rows[i_tot].cells[amount_col],
             f"{prefix}{total:,.{decimals}f}")
    ```

    Do the same for any other summed column and for any subtotal rows. If a Total cell is an `a:fld` (field) instead of literal text, leave the field alone or update only its cached `<a:t>` and let PowerPoint recalculate.

    ## Step 5 — sync the graphic frame

    XML edits do not move `p:graphicFrame/p:xfrm/a:ext`. Recompute from the rows.

    ```python
    row_h = sum((r.height or Emu(0)) for r in table.rows)
    gf.height = row_h if row_h else old_h + (new_row.height or Emu(0))

    old_bottom = gf.top + old_h
    new_bottom = gf.top + gf.height
    assert new_bottom <= prs.slide_height, (
        f"table bottom {new_bottom} exceeds slide height {prs.slide_height}"
    )
    ```

    If the table sits inside a group, `gf.top`/`gf.height` are in the group’s child coordinate space — compare against the group’s extents, not `prs.slide_height`. For a 16:9 deck the slide is 12192000 × 6858000 EMU, not 13333333 wide.

    Then handle anything underneath:

    ```python
    delta = new_bottom - old_bottom
    if delta > 0:
        for sh in slide.shapes:
            if sh.shape_id == gf.shape_id:
                continue
            horizontally_overlaps = (sh.left < gf.left + gf.width
                                     and sh.left + sh.width > gf.left)
            if horizontally_overlaps and sh.top >= old_bottom:
                sh.top += delta          # or shrink new_row.height instead
    ```

    If shifting would push something off the slide, or a shape overlaps the table’s new area, don’t guess — shrink the new row’s `height` or ask.

    ## Step 6 — re-apply explicit banding

    If the table uses a table style with `bandRow`, PowerPoint re-bands on render and there is nothing to do. If the rows carry explicit `a:tcPr/a:solidFill` fills (common in generated decks), parity below the insert is now wrong. Capture the two alternating fills before inserting and rewrite them after:

    ```python
    def solid_fill(tr):
        tcPr = tr.tc_lst[0].tcPr
        sf = tcPr.find(qn("a:solidFill")) if tcPr is not None else None
        return copy.deepcopy(sf) if sf is not None else None

    fill_a = solid_fill(table.rows[i_ref]._tr)
    fill_b = solid_fill(table.rows[i_ref - 1]._tr) if i_ref > 1 else fill_a

    if fill_a is not None:
        for n, r in enumerate(table.rows[1:i_tot]):        # data rows only
            src = fill_a if n % 2 == 0 else fill_b
            for tc in r._tr.tc_lst:
                tcPr = tc.get_or_add_tcPr()
                old = tcPr.find(qn("a:solidFill"))
                if old is not None:
                    tcPr.remove(old)
                if src is not None:
                    tcPr.append(copy.deepcopy(src))
    ```

    Leave the header and Total rows untouched so their styling is preserved.

    ## Step 7 — prove it in the saved file

    ```python
    out = "deck-out.pptx"
    prs.save(out)

    chk = Presentation(out)
    # locate the same table, then assert:
    #   len(rows) == n_rows_before + 1
    #   label order == [..., "Service G", "Service H", "Total"]
    #   the new row's a:tcPr XML equals the Service G row's a:tcPr XML
    ```

    The last check is the actual “formatting kept” proof: compare the `<a:tcPr>` elements of the new row and the Service G row, ignoring only the `<a:t>` text. Also re-open the saved file and confirm Total’s value and that `graphic_frame.height == sum(r.height for r in table.rows)`.

    ## Pitfalls that bite

    - `table.add_row()` / `add_column()` do not exist in 1.0.2; `Table.rows` is read-only.
    - Setting `cell.text` wipes run formatting — always edit `runs[0].text` and remove extra runs.
    - Splice with `ref_tr.addnext(new_tr)`, not append, or you land after Total.
    - Total’s index shifts by one after the insert — re-find it.
    - The frame height never follows XML edits; set `gf.height` explicitly.
    - Cloned `a:tr/@h` is a minimum. PowerPoint may render taller if text wraps; the frame uses the stored sum.
    - Explicit banding fills do not re-band themselves.
    - Tables inside groups use group coordinates, so slide-height checks must be adjusted.
- id: pptx-relayout-02
  answer: |
    ## What actually has to change

    A DrawingML table is three parallel structures that must stay in lockstep:

    ```xml
    <p:graphicFrame>
      <p:xfrm><a:off .../><a:ext cx="..." cy="..."/></p:xfrm>
      <a:graphic><a:graphicData uri=".../table">
        <a:tbl>
          <a:tblPr firstRow="1" bandRow="1">...</a:tblPr>
          <a:tblGrid>
            <a:gridCol w="1828800"/>   <!-- Item -->
            <a:gridCol w="1828800"/>   <!-- ... -->
          </a:tblGrid>
          <a:tr h="370840">
            <a:tc><a:txBody><a:p>...</a:p></a:txBody><a:tcPr/></a:tc>
            <a:tc>...</a:tc>
          </a:tr>
          ...
        </a:tbl>
      </a:graphic>
    </p:graphicFrame>
    ```

    `python-pptx` has no `add_column()` (and no `add_row()`): `Table`, `columns`, `rows`, `_Column`, `_Row` are read-only views over that XML. So adding "SKU" after "Item" is manual XML surgery on **all three** levels:

    1. **Grid** — insert one `<a:gridCol w="..."/>` immediately after the `Item` column's `gridCol`. The number of `gridCol` elements is the table's column count.

    2. **Every row** — insert one `<a:tc>` at the same logical position in *every* `<a:tr>`, including the header row and any total/footer row. The safest construction is `copy.deepcopy()` of the neighbouring `Item` cell, `ref_tc.addnext(new_tc)`, then clear its text and write into the existing first run (`runs[0].text`, delete extra runs/paragraphs), keeping its `a:tcPr` (fill, borders, margins, anchor). A cell you invent from scratch must still contain a well-formed `<a:txBody>` with at least one `<a:p>`.
       - If the table uses explicit banded fills in `a:tcPr` rather than relying on the table style, re-alternate the fills on the new cells.
       - Drop any `gridSpan`, `rowSpan`, `hMerge`, `vMerge` accidentally inherited from the copied cell.

    3. **Merged cells are the trap.** Cells don't map 1:1 to grid columns when `gridSpan > 1`. For each row, walk the cells accumulating `gridSpan` to find whether the insertion point is a cell boundary (insert a new `a:tc` there) or falls *inside* a horizontally merged cell (increment that cell's `gridSpan` by 1 instead of inserting). Getting this wrong gives a cell-count/`gridSpan` mismatch and PowerPoint offers to "repair" the file.

    4. **Frame geometry** — XML edits do not move the frame. `p:xfrm/a:ext/@cx` must equal the sum of the `gridCol` widths, and `cy` must equal the sum of `a:tr/@h`. After editing:

    ```python
    gf.width  = sum(c.width  for c in tbl.columns)   # writes/reads a:gridCol/@w
    gf.height = sum(r.height for r in tbl.rows)      # reads a:tr/@h
    ```

    (`gf.width`/`gf.height` are the only reliable way to sync `a:ext`; python-pptx won't recompute it for you.)

    ## Keeping it on the slide

    The table already spans nearly the full width, so a new column at the old widths is guaranteed to run off the slide. You cannot just append width — you must **redistribute**, not extend:

    - Read the available box: `prs.slide_width`, `gf.left`, and any right margin/padding. Available width ≈ `slide_width - gf.left - right_gap`.
    - Choose the new column's width, then shrink the existing columns proportionally so the **sum of all `gridCol/@w` stays ≤ available width** (or ≤ the original `cx` if you want the table to keep its exact footprint). Respect a sensible minimum per column so text doesn't collapse.
    - Write each new width back to `a:gridCol/@w` (or `tbl.columns[i].width`), then reset `gf.width = sum(...)`.
    - Assert `gf.left + gf.width <= prs.slide_width`; if the table must get wider, you also have to move `gf.left` (and check nothing sits to its left/right).
    - Check the shapes below/around it: a narrower Item/SKU column makes text wrap, and since `a:tr/@h` is an explicit height, wrapped text can overflow the row. Recompute row heights if needed, then re-sync `gf.height = sum(r.height)` and assert `gf.top + gf.height <= prs.slide_height` plus no collision with the shapes underneath.

    ## Verification

    After saving to a **new** file, reopen and assert: `len(tblGrid.gridCol_lst)` equals the number of columns implied by every row's `gridSpan` sum; `gf.width == sum(gridCol w)`; `gf.height == sum(tr h)`; `gf.left + gf.width <= prs.slide_width` and `gf.top + gf.height <= prs.slide_height`; and "SKU" appears exactly once per row in the saved slide XML. A round-trip open is the real test — count mismatches are the usual cause of PowerPoint's repair prompt.
- id: pptx-tblins-01
  answer: |
    ## Short answer

    Work in EMU against the slide's real surface, build a rectangle list of everything already on the slide (recursing into groups), search that list for a free rectangle of the table's size, then call `shapes.add_table(rows=3, cols=2, left=..., top=..., width=..., height=...)`. After that, set the row heights/column widths and re-sync `graphic_frame.height` to the sum of the row heights.

    Two things that must be right: "3×2" is **3 rows, 2 columns** in python-pptx (`rows` first), and the geometric overlap check must run **before** insertion, because a table added last is drawn on top and will simply hide whatever it covers.

    ## 1. Where the free space actually is

    Read the slide size, don't assume it:

    ```python
    sw, sh = prs.slide_width, prs.slide_height   # 16:9 is 12192000 x 6858000 EMU
    ```

    Then collect occupied boxes. Groups are the trap: a group child's `left/top` are in the group's *child* coordinate space, so they have to be mapped through `a:off`, `a:ext`, `a:chOff`, `a:chExt` before comparison.

    ```python
    from pptx.enum.shapes import MSO_SHAPE_TYPE
    from pptx.oxml.ns import qn

    def _xfrm(grp):
        return grp._element.find(qn('p:grpSpPr')).find(qn('a:xfrm'))

    def rects(shapes, tf=(0.0, 0.0, 1.0, 1.0, 0.0, 0.0)):
        """tf = (ox, oy, sx, sy, cx, cy): x -> ox + (x - cx) * sx"""
        out = []
        for s in shapes:
            if s.left is None or s.top is None or s.width is None or s.height is None:
                continue
            if s.shape_type == MSO_SHAPE_TYPE.GROUP:
                x = _xfrm(s._element)
                off, ext, chOff, chExt = x.off, x.ext, x.chOff, x.chExt
                ox, oy = (tf[0] + (off.x - tf[4]) * tf[2],
                          tf[1] + (off.y - tf[5]) * tf[3])
                sx = tf[2] * ext.cx / chExt.cx if chExt.cx else tf[2]
                sy = tf[3] * ext.cy / chExt.cy if chExt.cy else tf[3]
                out += rects(s.shapes, (ox, oy, sx, sy, chOff.x, chOff.y))
            else:
                x0 = tf[0] + (s.left - tf[4]) * tf[2]
                y0 = tf[1] + (s.top  - tf[5]) * tf[3]
                out.append((int(x0), int(y0),
                            int(x0 + s.width * tf[2]), int(y0 + s.height * tf[3])))
        return out
    ```

    Practical filter: skip **empty placeholders**. An unused body placeholder can cover the whole slide and will block every candidate even though nothing is painted there. Include placeholders that have text (the title is a real obstacle), and include pictures, charts, other tables, connectors, and freeform shapes.

    Then scan for a fit, with a margin off the slide edge and a gutter between the table and neighbours:

    ```python
    from pptx.util import Inches

    def free_spot(obst, sw, sh, w, h,
                  margin=Inches(0.5), gutter=Inches(0.12)):
        xs = sorted({margin} | {r[2] + gutter for r in obst})
        ys = sorted({margin} | {r[3] + gutter for r in obst})
        for y in ys:                      # top-to-bottom: fills gaps left above
            for x in xs:                  # left-to-right
                c = (x, y, x + w, y + h)
                if c[2] > sw - margin or c[3] > sh - margin:
                    continue
                if any(not (c[2] + gutter <= r[0] or r[2] + gutter <= c[0] or
                            c[3] + gutter <= r[1] or r[3] + gutter <= c[1])
                       for r in obst):
                    continue
                return c
        return None
    ```

    If you prefer "put it under the existing content" rather than "fill the highest gap", restrict `ys` to `{max(r[3] for r in obst) + gutter}` and fall back to the full scan. Either way, if `free_spot` returns `None` the slide is full: shrink the table (fewer/shorter rows, smaller width), move an existing shape down, or add a slide — do not insert anyway and hope.

    ## 2. Create it

    ```python
    from pptx.util import Inches, Pt

    W, H = Inches(6.0), Inches(1.5)          # frame size you asked the scan for
    spot = free_spot(rects(slide.shapes), sw, sh, W, H)
    if spot is None:
        raise RuntimeError("no free region for a 3x2 table on this slide")

    gf = slide.shapes.add_table(rows=3, cols=2,
                                left=spot[0], top=spot[1],
                                width=W, height=H)
    table = gf.table
    table.first_row = True                   # header banding from the table style

    table.columns[0].width = Inches(3.5)
    table.columns[1].width = Inches(2.5)
    table.rows[0].height = Inches(0.45)
    table.rows[1].height = Inches(0.45)
    table.rows[2].height = Inches(0.45)
    ```

    `add_table` distributes `height` equally across rows and `width` equally across columns, so once you set explicit row heights the frame no longer matches. Fix it:

    ```python
    gf.height = sum(r.height for r in table.rows)     # keep p:xfrm/a:ext cy honest
    assert gf.top + gf.height <= sh - Inches(0.25)
    ```

    Fill through the cell's existing paragraph/run rather than appending paragraphs:

    ```python
    data = [["Item", "Qty"], ["Widget A", "12"], ["Gadget D", "4"]]
    for r, row in enumerate(data):
        for c, val in enumerate(row):
            cell = table.cell(r, c)
            cell.text_frame.text = val
            for p in cell.text_frame.paragraphs:
                for run in p.runs:
                    run.font.size = Pt(14)
                    run.font.bold = (r == 0)
    ```

    ## 3. Points that bite

    - **Units.** Everything is integer EMU; `Inches()`/`Pt()` produce them. Don't mix float inches into `left/top`.
    - **Rotation/flip.** `left/top/width/height` is the unrotated box. For rotated shapes add slack (or use the rotated bounding box); the cheap version is `gutter` big enough to swallow it.
    - **Z-order.** New shapes go on top. The pre-check is what prevents "no overlap" from becoming "hidden content".
    - **Groups.** Recurse, and map coordinates as above. A grouped set of shapes at `left=0` may actually sit at x=4" on the slide.
    - **Table height is font-driven in practice.** If a cell wraps, the renderer grows the row; leave vertical slack, e.g. 0.45–0.5" for 14 pt.
    - **Verify after saving.** Re-open the file and assert no two shape boxes intersect (same rect helper), and that `gf.top + gf.height` is inside the slide. That catches the off-by-one-EMU and the "I forgot the title placeholder" cases.

    ## Assumptions

    "3×2" read as 3 data rows × 2 columns; a 0.5" slide margin and 0.12" gutter; empty placeholders treated as non-obstacles; single table added to an already-populated slide, no existing table to append rows to (that path needs `copy.deepcopy` of an `a:tr`, not `add_row()`, which python-pptx does not have).
- id: pptx-tblins-02
  answer: |
    ## Why it looks different

    `add_table()` doesn't inherit anything from the deck's other tables. It writes a fresh `<a:tbl>` whose `<a:tblPr>` contains a **built-in table style reference** — Medium Style 2 – Accent 1 — plus `firstRow="1"` and `bandRow="1"`:

    ```xml
    <a:tblPr firstRow="1" bandRow="1">
      <a:tableStyleId>{5C22544A-7EE6-4342-B151-600D1358B6C3}</a:tableStyleId>
    </a:tblPr>
    ```

    Accent 1 is the theme's blue, so you get a blue header + banded blue rows. (The exact GUID in your version is easiest read off a newly added table rather than trusted from memory.) python-pptx exposes only the *flags* (`table.first_row`, `table.horz_banding`, `table.last_row`, `table.first_col`, `table.last_col`, `table.vert_banding`) — there is **no `table.style` property**, so the style itself is XML work.

    Two cases, and you must check which one the deck is in:

    ## Case A — the existing tables reference a style

    Inspect an existing table:

    ```python
    tbl = existing_table_shape.table._tbl
    print(tbl.tblPr.xml)
    ```

    If it has an `<a:tableStyleId>` (your deck's own or another built-in), the fix is to copy the whole `a:tblPr` element onto the new table — style id and flags together:

    ```python
    from copy import deepcopy

    src_tbl = src_gf.table._tbl          # an existing, correct table
    dst_tbl = new_gf.table._tbl          # the add_table one

    dst_tbl.replace(dst_tbl.tblPr, deepcopy(src_tbl.tblPr))
    ```

    That alone usually ends the "blue" problem, because the new table now points at the same style and the same banding flags.

    ## Case B — the existing tables are directly formatted

    Very common in real decks: `tblPr` is empty (no style id, or `firstRow="0" bandRow="0"`) and all the look lives in per-cell `<a:tcPr>` — `a:solidFill`, `a:lnL/lnR/lnT/lnB`, margins, `anchor`. Explicit cell formatting **wins over** the table style, so changing the style id alone won't match; you have to copy the cells' `tcPr`.

    ```python
    src_tbl = src_gf.table._tbl
    dst_tbl = new_gf.table._tbl

    # 1) style + banding flags
    dst_tbl.replace(dst_tbl.tblPr, deepcopy(src_tbl.tblPr))   # or drop the style id entirely:
    # dst_tbl.tblPr.remove(dst_tbl.tblPr.find(qn('a:tableStyleId')))

    # 2) per-cell fill/borders/margins/anchor
    for r, src_tr in enumerate(src_tbl.tr_lst):
        for c, src_tc in enumerate(src_tr.tc_lst):
            dst_tc = dst_tbl.tr_lst[r].tc_lst[c]
            old = dst_tc.tcPr
            if old is not None:
                dst_tc.remove(old)
            if src_tc.tcPr is not None:
                # a:tcPr sits after a:txBody in a:tc
                dst_tc.txBody.addnext(deepcopy(src_tc.tcPr))
    ```

    `tcPr` carries no merge state — `gridSpan`, `rowSpan`, `hMerge`, `vMerge` are attributes on `a:tc`, so copying `tcPr` is safe. If row/column counts differ, sample one header cell and one body cell and replicate their `tcPr` (re-alternate band fills yourself if the source bands explicitly).

    ## Also copy the text formatting

    A copied `tcPr` gives you fill/borders, not the font. New cells get default run properties, so set size/name/colour/bold explicitly, ideally lifted from a source cell's first run:

    ```python
    src_run = src_tc.text_frame.paragraphs[0].runs[0]
    for p in dst_cell.text_frame.paragraphs:
        p.alignment = src_para.alignment
        for r_ in p.runs:
            r_.font.size   = src_run.font.size
            r_.font.name   = src_run.font.name
            r_.font.bold   = src_run.font.bold
            r_.font.color.rgb = src_run.font.color.rgb   # or theme colour XML
    ```

    Don't use `cell.text = "..."` afterwards if you care about formatting — it rebuilds the paragraph and wipes what you set. Write into the existing `runs[0].text` and delete extra runs.

    ## Alternative: clone the table outright

    If geometry (row/col counts, widths, row heights) is close, the most faithful route is to deep-copy the existing `<a:tbl>` element, splice it in place of the new one, then only rewrite the cell texts. You still have to fix the frame afterwards, because the copied `a:tblGrid`/row heights won't match the `p:graphicFrame` extent:

    ```python
    gf.height = sum(r.height for r in gf.table.rows)
    ```

    and check `gf.top + gf.height` against `prs.slide_height` and anything below it. Remember python-pptx has no `add_row()`/`add_column()` — adding or deleting rows/columns is `deepcopy(a:tr)` + `addnext`/`remove`, and the same height recompute applies.

    ## Verify, don't assume

    - Re-open the saved file and diff `tblPr` (and a couple of `tcPr`) between the new table and a reference table — they should be structurally identical apart from text.
    - Render it: PowerPoint is authoritative; LibreOffice ignores some built-in style GUIDs and can lie about whether the match worked. A PDF/PNG export is the practical check.
    - If you dropped the `tableStyleId` and the deck's `ppt/tableStyles.xml` only has the default entry, the table renders as unstyled (black text, no fill, no borders) — that's expected, but only correct if that's what the neighbours look like.

    Bottom line: `add_table` stamps a built-in blue style; either copy an existing table's `a:tblPr` (Case A) or copy `tblPr` **and** each cell's `a:tcPr` plus the run fonts (Case B), then fix the graphic-frame height.
- id: pptx-tblins-03
  answer: |
    Assumption: the question is about **python-pptx** (the `pptx-tblins-*` series).

    **Short answer:** there is no `placeholder.insert_table()`. python-pptx only gives placeholders insertion helpers for pictures and charts (`PicturePlaceholder.insert_picture`, `ChartPlaceholder.insert_chart`); `TablePlaceholder` is read-only — it just wraps a table that is already in a table placeholder. So the idiomatic move is: create a normal table with `shapes.add_table()` using the placeholder's inherited geometry, then (optionally) delete the empty placeholder.

    ```python
    ph = next(p for p in slide.placeholders          # find the empty content ph
              if p.placeholder_format.type == PP_PLACEHOLDER.OBJECT
              and p.has_text_frame and not p.text.strip())

    # geometry resolves through the layout if the slide sp has no a:xfrm
    gf = slide.shapes.add_table(rows, cols, ph.left, ph.top, ph.width, ph.height)
    tbl = gf.table

    # set real column/row sizes through the table, not the frame
    tbl.columns[0].width = Emu(...)
    tbl.rows[0].height = Emu(...)
    gf.height = sum(r.height for r in tbl.rows)   # keep the frame in sync

    # stop PowerPoint showing the "click to add content" affordance in edit mode
    ph._element.getparent().remove(ph._element)   # no shape.delete() in python-pptx
    ```

    Details that matter:

    - **Read `ph.left/top/width/height` before removing it.** These properties resolve inherited values from the layout placeholder, so they work even when the slide's `p:sp` has no explicit `<a:xfrm>`.
    - **The table is a `p:graphicFrame` sibling** of the placeholder `p:sp` in `p:spTree`; an `a:tbl` cannot legally live inside a placeholder `p:sp`. That is why "putting a table in the placeholder" is really "replacing the placeholder with a graphicFrame".
    - **True placeholder semantics** (so the table still tracks the layout) means hand-building the frame: after `add_table`, copy the `<p:ph>` element from the sp into the graphicFrame's `<p:nvGraphicFramePr><p:nvPr>` (`nvPr.append(deepcopy(ph._element.ph))`). That is what `insert_chart` does internally; no equivalent exists for tables.
    - **Don't** try `ph.insert_table(...)` — it raises `AttributeError`, and don't expect `add_table` to auto-size rows to the placeholder; the height stays whatever the row heights say.
    - Removing the sp is optional; an unfilled placeholder is not rendered in the slideshow, it just leaves an empty `p:sp` (and the layout placeholder stays behind, which is harmless).

    If you need the table to *be* the placeholder, your only route is XML: replace the `p:sp` with a `p:graphicFrame` carrying the same `<p:ph idx="..."/>`. python-pptx has no API for this at any version.
- id: pptx-imgrep-01
  answer: |
    ## Short answer

    Don't touch the `p:pic`. Only repoint its blip at a new image part. Because the `p:pic` element stays exactly where it is in `p:spTree`, its position, size, rotation, crop frame, and stacking order are all preserved for free.

    ## The mechanics

    A picture is:

    ```xml
    <p:pic>
      <p:spPr>
        <a:xfrm><a:off x=.. y=../><a:ext cx=.. cy=../></a:xfrm>   <!-- position + size -->
      </p:spPr>
      <p:blipFill>
        <a:blip r:embed="rId7"/>                                   <!-- the image reference -->
        <a:srcRect .../>                                           <!-- crop -->
      </p:blipFill>
    </p:pic>
    ```

    `r:embed` points at a relationship on the **slide part**, which points at an image part in `ppt/media/`. So "replace the picture" = add a new image part to the slide part, get its new `rId`, write that `rId` into the existing `a:blip`. Everything else in the XML is left untouched.

    ```python
    from pptx import Presentation
    from pptx.oxml.ns import qn
    from pptx.util import Emu

    R_NS = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"

    def replace_picture(pic, new_image_path, reset_crop=True, keep_aspect=True):
        """pic: a Picture shape (or any shape whose fill is a blip)."""
        part = pic.part                                  # SlidePart

        # 1) new image part + relationship -> new rId
        image_part, new_rId = part.get_or_add_image_part(new_image_path)

        blipFill = pic._element.blipFill                 # a:blipFill
        blip = blipFill.blip                             # a:blip

        old_embed = blip.get(f"{{{R_NS}}}embed")
        old_link  = blip.get(f"{{{R_NS}}}link")

        # 2) repoint
        blip.set(f"{{{R_NS}}}embed", new_rId)

        # 2b) if it was a LINKED picture, kill the link or PowerPoint keeps using it
        if old_link is not None:
            blip.attrib.pop(f"{{{R_NS}}}link", None)
            ext_lst = blip.find(qn("a:extLst"))          # may hold a14:imgProps for the link
            if ext_lst is not None:
                blip.remove(ext_lst)

        # 3) old crop would silently apply to the new image — drop it if unwanted
        if reset_crop:
            src = blipFill.find(qn("a:srcRect"))
            if src is not None:
                blipFill.remove(src)

        # 4) position and size are already correct (a:xfrm untouched).
        #    Only adjust if you actively want to preserve the NEW image's aspect ratio:
        #    pic.width / pic.height are plain properties backed by a:ext.

        # 5) drop the old relationship ONLY if nothing else in this part uses it.
        #    python-pptx's drop_rel counts //@r:id only, so an image rel always looks
        #    unused to it — and get_or_add_image_part reuses one rId per image part,
        #    so another picture on this slide may still need it.
        if old_embed and old_embed != new_rId:
            xml = part._element.xml                      # serialized slide XML
            still_used = (f'r:embed="{old_embed}"' in xml or
                          f'r:link="{old_embed}"' in xml)
            if not still_used:
                part.drop_rel(old_embed)                 # old media part then leaves the zip
        return pic
    ```

    ## Why order survives

    `p:spTree` children are painted in document order — last child is on top. Mutating `a:blip` in place never moves the `p:pic`, so z-order is unchanged.

    If for some reason you must build a fresh picture shape, use `add_picture()` (which appends at the end = topmost) and then move the element into the old slot:

    ```python
    new_pic = slide.shapes.add_picture(path, pic.left, pic.top, pic.width, pic.height)
    pic._element.addnext(new_pic._element)   # insert right after...
    pic._element.getparent().remove(pic._element)  # ...then delete the old one
    ```

    Better still: `copy.deepcopy(pic._element)`, repoint its blip, `pic._element.addnext(clone)`, then remove the original — that also copies rotation, crop, and any effects.

    ## Gotchas worth knowing

    - **Same position/size is automatic**, but if the new image has a different aspect ratio it will be *stretched* to `a:ext cx/cy`. If you want it letterboxed, recompute `pic.width`/`pic.height` from the image's pixel size and dpi (`pptx.parts.image.Image.from_file(p).size`), anchored at `pic.left`/`pic.top`, and reset the fill to `<a:stretch><a:fillRect/></a:stretch>`.
    - **Old crop persists** via `a:srcRect` — clear it unless you deliberately want the same fractional crop.
    - **Linked (not embedded) pictures** carry `r:link` plus `a14:imgProps`; you must remove both or PowerPoint prefers the link on open.
    - **Unused rels still inflate the file**: python-pptx saves every part reachable through a rel, so a left-behind image rel keeps the old `ppt/media/*` in the saved zip even though no `p:pic` shows it.
    - **Placeholders and shape fills**: same technique works for a picture placeholder or a shape whose fill is a blip (`spPr/a:blipFill`), just reach the `a:blip` from the right parent.
    - **Verify**: save to a new path, reopen, and confirm the blip's `r:embed` resolves to the new image part, and that `ppt/media/` no longer contains the old bytes (hash compare) if you dropped the rel.

    I'm confident on the XML/relationship model and the in-place blip swap; the exact `get_or_add_image_part` return signature (`(image_part, rId)`) is python-pptx-specific — check it if you're on an old version.
- id: pptx-imgrep-02
  answer: |
    Because the two pictures are not separate images. They are two references to **one image part**, and overwriting the part's bytes changes it for every reference.

    **Why it happens**

    - An OOXML package stores each distinct media file once under `/ppt/media/imageN.png`. Slide 1's `p:pic` has an `a:blip/@r:embed` rel pointing at that part; slide 7's `p:pic` has its own rel (different rId, same slide part or a different one) pointing at the *same* partname.
    - This is normal: PowerPoint dedupes identical media by content hash, so a logo pasted on slide 1 and again on slide 7 usually lands in one media file with two rels. python-pptx does the same — `get_or_add_image_part()` / `ImageParts` keys off the SHA-1 of the image bytes and returns the existing part when the hash matches.
    - So when you do something like `image_part._blob = new_bytes` (or otherwise write into that part), you are editing the single shared `ppt/media/imageN.png` in the zip. Nothing is per-slide about it. On the next render, slide 1 and slide 7 both resolve their rel to the mutated bytes.

    You can confirm it before changing anything: open the package, read each `p:pic`'s `blip/@r:embed`, resolve the rId through the owning slide part's rels, and compare the target partnames. If slide 1 and slide 7 resolve to the same `/ppt/media/imageN.*`, that is the whole story. Alternatively walk `prs.part.package.image_parts` and check `part.partname` / `part.sha1`.

    **How to do it correctly**

    Don't mutate the existing part. Add a *new* image part with the new bytes and repoint only the blip you want to change:

    ```python
    new_part, new_rId = slide.part.get_or_add_image_part(new_logo_path)  # new bytes -> new part
    blip = pic._element.blipFill.blip
    blip.rEmbed = new_rId   # python-pptx property; or blip.set(qn('r:embed'), new_rId)
    ```

    Then handle the old rel, bearing in mind the trap in my notes: `drop_rel(rId)` counts only `//@r:id`, so for pictures (`a:blip/@r:embed`) it always sees zero references and will always drop. Before dropping, query the part XML for `//@r:embed` and `//@r:link` equal to the rId and drop only if none remain — otherwise you can strip the rel that slide 7 still needs. And a rel you leave in place keeps the old media in `ppt/media/`, because python-pptx saves every part reachable through a rel.

    Verify in the saved file: unzip to a new path, confirm slide 1's blip now targets the new partname and slide 7's still targets the old one, and that `ppt/media/` contains both images with the expected hashes.
- id: pptx-imgrep-03
  answer: |
    **Because `drop_rel()`'s "is it still used?" test doesn't look where pictures keep their references.**

    The safety check in python-pptx is roughly:

    ```python
    def _rel_ref_count(self, rId):
        return len([r for r in self._element.xpath("//@r:id") if r == rId])

    def drop_rel(self, rId):
        if self._rel_ref_count(rId) < 2:   # "not essential"
            self._rels.pop(rId)
    ```

    That XPath only sees `r:id` attributes (hyperlinks, ole objects, some embedded parts). A picture references its image through `a:blip/@r:embed` (or `@r:link` for a linked image), **not** `r:id`. So for an image relationship the count is always `0`, which is `< 2`, and `drop_rel` concludes "nothing uses this" and removes it — unconditionally, every time.

    The second half of the problem is why the *other* picture broke too. python-pptx deduplicates image parts by content hash, and when two pictures resolve to the same image part, `relate_to`/`get_or_add` reuses the existing relationship instead of creating a second one. So both `<p:pic>` elements carry the **same** `r:embed` value pointing at one `rId`. Drop that single relationship and every picture sharing it dangles: on save the rel is gone from `slideN.xml.rels`, the `r:embed` points at a missing relationship, and the media part is unreachable from that slide so it isn't written (or the picture renders as a broken image). python-pptx cannot distinguish "the one I meant to remove" from "the others using the same rId" — it only knows the rId.

    **Safe procedure:** never treat `drop_rel` as authoritative for image rels. Before dropping, query the part yourself and require zero remaining references, including groups and any other `a:blip`:

    ```python
    rels = slide.part.rels
    if not slide.part._element.xpath(f'//@r:embed[.="{rId}"] | //@r:link[.="{rId}"]'):
        rels.pop(rId)   # or slide.part.drop_rel(rId) once you've proven it's unused
    ```

    Also remember the reference may live outside this part — another slide, a layout, or notes have their own relationships to the same image part. `drop_rel` on one slide's part only removes that slide's rel; the media survives if other parts still point at it, which is the correct behaviour, but it means "the file still contains the image" is not proof your drop failed. To prove an image is really gone, re-open the saved file and hash-compare `ppt/media/*`.
- id: pptx-imgins-01
  answer: |
    ## Steps

    1. **Read the real slide size** — never hardcode 10×7.5 in. A 16:9 deck is usually 12192000 × 6858000 EMU.
    2. **Add the picture with `width=` only.** python-pptx reads the image's native pixel size and DPI, computes the proportional height, and stores it in the shape. Do **not** pass both `width` and `height` — that stretches it.
    3. **Compute the bottom-right anchor** from the *actual* height python-pptx calculated, not from a guess.
    4. **Collision-check against every existing shape before you accept the placement.** python-pptx has no "does this cover something" API; you have to compare bounding boxes yourself (and recurse into groups). If the region is occupied, stop and ask / move the image above the occupied block — the 2 in width is fixed, so you cannot shrink your way out.

    ```python
    from pptx import Presentation
    from pptx.util import Inches

    prs = Presentation("deck.pptx")
    slide = prs.slides[0]

    MARGIN = Inches(0.25)
    W = Inches(2)

    # width-only => height is scaled by the native aspect ratio
    pic = slide.shapes.add_picture("signature.png", 0, 0, width=W)
    H = pic.height                      # proportional, computed from the image header

    left = prs.slide_width  - W - MARGIN
    top  = prs.slide_height - H - MARGIN
    pic.left, pic.top = left, top

    def rect(sh):
        return (sh.left, sh.top, sh.left + sh.width, sh.top + sh.height)

    def hit(a, b, pad=0):
        return not (a[2] + pad <= b[0] or b[2] + pad <= a[0]
                    or a[3] + pad <= b[1] or b[3] + pad <= a[1])

    def walk(shapes):
        for sh in shapes:
            if sh.shape_type == 6:          # MSO_SHAPE_TYPE.GROUP
                yield from walk(sh.shapes)
            else:
                yield sh

    cand = (left, top, left + W, top + H)
    occupied = [
        sh for sh in walk(slide.shapes)
        if sh.shape_id != pic.shape_id
        and sh.width and sh.height
        and hit(cand, rect(sh), pad=Inches(0.05))   # small pad for shadows/AA
    ]

    if occupied:
        # bottom-right is taken; do NOT silently overlap.
        # Either raise the image above the topmost blocker, or stop and ask.
        ...
    ```

    ## Details that matter

    - **`add_picture(..., width=W)` is the aspect-ratio guarantee.** After the call, assert `abs(pic.height / pic.width - native_h / native_w) < 1e-3`. Passing only one dimension is what keeps it undistorted; if the image has no DPI metadata python-pptx assumes 72 DPI, but because you set width explicitly, the *ratio* is still native.
    - **The anchor point is the shape's top-left**, so bottom-right placement is `left = slide_width − width − margin`, `top = slide_height − height − margin`. Compute it *after* insertion, using `pic.height`.
    - **Existing content includes group children.** `slide.shapes` does not descend into groups, so a logo inside a group is invisible to a top-level loop. Recurse as shown. Layout/master decorations are not in `slide.shapes` and generally should not block a signature, but empty placeholders that sit over the corner will show up — filter them out (`sh.has_text_frame and not sh.text_frame.text.strip() and sh.is_placeholder`).
    - **Z-order:** the picture is appended last, so it paints on top. Any undetected overlap becomes actual visual occlusion, which is why the bbox check is not optional.
    - **Shadows/glow** extend beyond the geometry box; the 0.05 in pad in the check covers that and antialiasing at the edge.
    - **If the corner is genuinely occupied**, options in order: (a) increase the margin if the slide edge allows, (b) move the image up so its bottom edge sits just above the highest blocker while staying right-aligned — but then it is no longer "bottom-right", so (c) is to **stop and ask**. Do not reduce the width below 2 in to make it fit; that violates the stated requirement.
    - **Verify in the saved file:** save, reopen, confirm the picture's `width == Inches(2)`, `ratio == native`, `top + height <= prs.slide_height`, `left + width <= prs.slide_width`, and that its rect does not intersect any other shape's rect.

    Plain-language summary: add it width-only so the height is derived, anchor it with `slide_width/height` minus its own size and a margin, then box-test it against every shape on the slide (groups included) and refuse to place it over anything — the only "free" part is the margin, not the 2 in width.

    ```recap
    ASKED — how to insert signature.png 2 in wide bottom-right, aspect preserved, no overlap.

    WORKED — BUILT a placement recipe: width-only add_picture, margin-anchored via slide_width/height, recursive bbox collision check, save-and-reopen verification. Code given.

    FOUND — python-pptx gives no collision detection and no auto-anchoring; groups are invisible to a top-level shape loop; passing both width+height is the distortion trap.

    DECIDED — Anchor on the computed pic.height, not a guessed one; treat overlap as a hard stop rather than silently covering, since width is fixed. BOTTOM LINE: add with width= only, position from slide dimensions minus the shape's own size and a margin, and box-test against all shapes including group children.

    NEXT — If the corner is occupied in the real deck, raise the image or ask; do not shrink below 2 in, since that breaks the requirement.
    ```
- id: pptx-imgins-02
  answer: |
    `insert_picture()` on a `PicturePlaceholder` **crops the image to fill the placeholder** — it behaves like CSS `object-fit: cover`, not `contain`.

    What happens under the hood:

    - The picture shape is given the placeholder's position and size (`p:spPr/a:xfrm`), so the frame stays where the placeholder was.
    - To keep the image from looking stretched, python-pptx scales it to cover that frame and puts the excess behind an `a:srcRect` crop in `p:blipFill`. For a portrait photo dropped into a wider/landscape placeholder, the overflow is on the top and/or bottom, so those parts vanish from view.
    - The bytes are still in the media part; the crop is only a viewing rectangle. That's why "part of the photo disappears" rather than the shape resizing.

    Verify:

    ```python
    pic = ph.insert_picture("portrait.jpg")
    print(pic.crop_left, pic.crop_right, pic.crop_top, pic.crop_bottom)
    ```

    One or more of those will be non-zero (fractions of the image extent; can be negative).

    Ways out, depending on intent:

    - **Show the whole photo**: zero the crops —
      `pic.crop_left = pic.crop_right = pic.crop_top = pic.crop_bottom = 0`.
      Caveat: the frame is still the placeholder's aspect ratio, so the image is now distorted unless you also resize the frame to match the image's aspect ratio or accept letterboxing by other means.
    - **Preserve aspect ratio properly**: use `shapes.add_picture(path, left, top, width=...)` (or `height=...`, not both) and position/size it yourself, or enlarge the placeholder to the photo's aspect ratio before calling `insert_picture`.
    - **Adjust the crop only**: nudge `crop_top`/`crop_bottom` to choose which part of the portrait is kept.

    Assumption: your placeholder's aspect ratio is not the photo's (typical portrait-into-default-placeholder case). If the placeholder were the same aspect ratio, no crop would be applied.
- id: pptx-legacy-01
  answer: |
    ## Short answer

    **No.** python-pptx cannot open `.ppt`. `Presentation("deck.ppt")` fails immediately with `pptx.exc.PackageNotFoundError: Package not found at 'deck.ppt'` (underneath it is just `zipfile.BadZipFile`, because python-pptx only speaks OPC/ZIP + PresentationML).

    A `.ppt` is a **binary OLE2 / Compound File Binary** container (`D0 CF 11 E0 A1 B1 1A E1`) holding a `PowerPoint Document` stream with a completely different record format. python-pptx has no reader for it, and no writer either — there is no "save back to `.ppt`". Nothing in the pure-Python ecosystem edits binary `.ppt` tables; you must convert.

    ## Step 0 — sniff the bytes, not the extension

    Files get mislabelled constantly. Before concluding anything:

    ```python
    head = open("deck.ppt", "rb").read(8)
    # b"PK\x03\x04..."  -> it is really a .pptx; python-pptx opens it as-is
    # b"\xd0\xcf\x11\xe0\xa1\xb1\x1a\xe1" -> real legacy .ppt, must convert
    ```

    python-pptx does not check the extension, so a ZIP-based deck named `.ppt` opens fine. Only the OLE2 case needs a conversion step.

    ## Workflow for a real binary `.ppt`

    **1. Convert to `.pptx` first (outside python-pptx).**

    ```bash
    soffice --headless --convert-to pptx:"Impress MS PowerPoint 2007 XML" \
            --outdir converted deck.ppt
    ```

    - Check exit code, that `converted/deck.pptx` exists and is non-zero, and that `Presentation()` on it succeeds. Conversion can "succeed" with an empty or partial file.
    - Alternatives if LibreOffice is unavailable: Windows COM via `pywin32` (`Presentations.Open(...)`, `SaveAs(..., ppSaveAsOpenXMLPresentation)`), Keynote AppleScript export on macOS, or simply ask the user to *File → Save As → .pptx*. Aspose.Slides reads/writes `.ppt` natively but is commercial.
    - Avoid `olefile`/hand-rolled record parsing for editing — it can inspect, not safely rewrite.

    **2. Re-establish facts about the deck after conversion.** This is not optional; the round-trip can change the file:

    - Slide count, `prs.slide_width` / `prs.slide_height` (a 16:9 deck is typically 12192000 × 6858000 EMU).
    - Re-find the target table **by header content**, recursing into `GroupShape.shapes`, and assert exactly one candidate. A legacy table may have arrived as a real `GraphicFrame` table, but it may also have become a group of shapes or a picture — if zero candidates match the expected header texts, stop and ask rather than editing the first `has_table` shape.
    - Match the row on exact stripped label text and assert `len(hits) == 1`.

    **3. Do the edit under the normal python-pptx constraints:**

    - No `table.add_row()` / `add_column()` exists in 1.0.2. Insert with `new_tr = copy.deepcopy(ref_tr)` + `ref_tr.addnext(new_tr)` (or `total_tr.addprevious(new_tr)`), fill via each cell's existing first run, drop extra runs.
    - Then finish the job: recompute any Total/subtotal, set `graphic_frame.height = sum(r.height for r in table.rows)` (the frame does **not** follow XML edits), check `graphic_frame.top + height` against `prs.slide_height` and the shapes below, and re-apply explicit `a:tcPr` banding fills if the table uses them.
    - For picture changes, hand-manage rels: `drop_rel` only counts `//@r:id`, so `a:blip/@r:embed` always counts 0 — query `//@r:embed`/`//@r:link` yourself before dropping, and drop unused rels or the old image stays in `ppt/media/`.

    **4. Verify in the saved file, not in memory.** Save to a new `.pptx`, reopen it, confirm the row text is present/absent in `ppt/slides/slide*.xml`, and for image removals list `ppt/media/*` and hash-compare.

    **5. Deliverable question — this is the real decision.** If the user needs `.ppt` back:

    ```bash
    soffice --headless --convert-to ppt --outdir out edited.pptx
    ```

    Warn that `.ppt` round-tripping degrades fidelity (table styling, theme fonts, SmartArt, embedded OLE, animations, sometimes text autofit). Recommend delivering `.pptx` unless the consumer genuinely requires the binary format. Also note you cannot re-verify the exported `.ppt` with python-pptx — your only proof is a render (convert to PDF/PNG and eyeball) or reopening it in PowerPoint/LibreOffice.

    ## Assumptions I'm making

    - LibreOffice (or PowerPoint/COM) is available for conversion; if not, the honest answer is "ask the user for a `.pptx`."
    - The target is a genuine binary `.ppt`; if the magic bytes say `PK`, skip conversion entirely.
    - "Change a table" means a row/cell edit, not building a new table — if the converted deck lost the table as a table, that becomes a rebuild-and-confirm-with-user task.

    **One-line summary:** python-pptx is `.pptx`-only (it fails on OLE2 `.ppt` with `PackageNotFoundError`), so the workflow is sniff → convert `.ppt`→`.pptx` with LibreOffice/COM → re-identify the table by header text → edit with deepcopy-row + frame-height sync → verify by reopening → optionally convert back to `.ppt` with an explicit fidelity warning.
- id: pptx-legacy-02
  answer: |
    Assume the original `.ppt` is the master and the `.pptx` is a derived working copy that we edit; no real PowerPoint is available for ground-truth rendering.

    ## Before editing (conversion audit)

    **Prove the file is loadable and structurally sane**
    - `unzip -t` the `.pptx` (conversion failures produce truncated zips), then load it with `python-pptx` (`Presentation(path)`). A LibreOffice file that unzips but throws on load is still unusable.
    - `soffice --headless --convert-to pdf` it and confirm the PDF renders — this catches corrupt slide parts that python-pptx tolerates.

    **Compare against the original, slide by slide**
    - Slide count, hidden slides, notes pages, and section order.
    - Every text string still present. LibreOffice can drop text in WordArt, OLE, and some SmartArt.
    - Tables: does each one still exist as a real `a:tbl` inside a `p:graphicFrame`? LibreOffice sometimes converts a PPT table into lines + text boxes, in which case there is no table to edit. Check for `has_table`, and recurse into `GroupShape.shapes` because tables can end up nested in groups.
    - Charts, SmartArt, OLE objects, equations: usually flattened to pictures or a group of shapes, losing editability.
    - Images: recompressed or converted (EMF/WMF → PNG, PNG → JPEG), DPI changed, transparency lost. Background fills and picture fills may not survive.
    - Animations, transitions, triggers, and slide timings: often simplified or dropped.
    - Effects: shadows, gradients, 3-D, and theme effects frequently change.

    **Check the things that break name/index-based code**
    - Shape names (`"Title 1"`, `"TextBox 5"`) and `shape_id`s are regenerated; layout and master names are rewritten, and LibreOffice may duplicate layouts/masters. So do **not** use `slide.shapes.title`, `slide.placeholders[...]`, `slide.slide_layout.name`, or shape index to find targets — match on **content** (header text, exact cell text) and assert uniqueness.
    - Text may be split into many runs (even one run per character) after conversion. Inspect `run` boundaries before you plan to write `runs[0].text` — you must clear the extra runs or formatting fragments.
    - Autofit may have been rewritten to `normAutofit` with an explicit `fontScale`, so text can silently shrink or overflow.

    **Check fonts and geometry**
    - Font substitution: Calibri→Carlito, Cambria→Caladea, etc. If the saved `.pptx` now references Carlito, the user's PowerPoint may not have it and the deck reflows. This is the single most common "it looked fine here" failure. Decide whether to remap to the original font names.
    - Slide size: classic `.ppt` is 4:3 = **9144000 × 6858000 EMU**; 16:9 is **12192000 × 6858000**. Read `prs.slide_width` / `prs.slide_height`; never assume.
    - Bullet/numbering XML, indentation, cell margins, and borders are commonly rewritten — check the specific table or list you're about to touch.

    **Housekeeping**
    - Work on a copy; confirm the original `.ppt` is untouched and backed up.
    - Note that password-protected or macro-bearing `.ppt` files may fail to convert or lose the macros.

    ## After editing

    - **Reopen the saved file** with python-pptx, and convert it to PDF to confirm it still renders. Don't trust the in-memory object.
    - **Prove every removal at the XML level**: save to a new path, reopen, and grep every `ppt/slides/slide*.xml` for the removed text. For a removed image, list `ppt/media/*` in the zip and hash-compare against the old bytes — an image rel you didn't drop keeps the media part alive.
    - **Re-sync the frame after any row add/delete**: recompute the Total (and subtotals), then set `graphic_frame.height = sum(r.height for r in table.rows)`, because `p:graphicFrame/p:xfrm/a:ext cy` does not follow XML edits. Confirm `top + height` is inside `prs.slide_height` and doesn't collide with shapes below (move them or ask).
    - **Re-alternate explicit banding fills** if the table uses `a:tcPr` fills, so the row colors stay correct.
    - **Check rels/media hygiene**: no orphaned rels, no duplicated media, file size didn't balloon, fonts and theme are what you intended.
    - **Visual diff**: render the edited slide to PNG/PDF and compare with the pre-edit render; confirm only the intended change moved.
    - Note honestly that a LibreOffice render is only a proxy — real PowerPoint may still prompt repair or lay out differently.

    ## What you must tell the user

    1. **The conversion is lossy.** State specifically what changed in *their* deck: fonts substituted, layouts/masters rebuilt, shape names lost, animations/transitions dropped or simplified, SmartArt/charts/OLE flattened to images, effects and table styling altered. Don't hand-wave with "formatting may vary."
    2. **The original `.ppt` is still the master.** The `.pptx` is a derived copy; re-converting from the `.ppt` discards the edits. Give them the exact output path and confirm nothing was overwritten.
    3. **Fonts matter.** If LibreOffice substituted fonts, the deck can look right locally and wrong on their machine — tell them which fonts must be installed, or that you remapped them.
    4. **What couldn't be edited faithfully** — e.g. "the table on slide 4 is now a picture, so a row insert isn't possible" — and ask before proceeding rather than silently doing something else.
    5. **What you verified and what you couldn't.** Say you reopened and rendered the file and proved the text/image removal in the XML; say you could not test in real PowerPoint.
    6. **Ambiguity stops the work.** If the target row or table matches zero or several candidates, say so and ask — don't pick the first match.
    7. **Preserved geometry facts** they should know: slide size/aspect retained or changed, and whether anything now overflows the slide.

    **Bottom line:** treat conversion as an untrusted rewrite — audit fidelity and identity (content, fonts, geometry, real-vs-flattened tables) before touching anything, verify the saved artifact rather than the in-memory object afterward, and tell the user plainly that the `.pptx` is a lossy derivative, what was lost, and that the `.ppt` remains the source of truth.
- id: pptx-verify-01
  answer: |
    ## Assumptions

    No deck.pptx is available in this run, so this is the procedure I would apply, not an execution against a specific file. It assumes python-pptx (1.0.x), a 16:9 deck read via `prs.slide_width` / `prs.slide_height`, and edits that may include row insert/delete, cell text changes, and picture replacement/removal.

    ## Principle

    **Never save over the input.** `prs.save(path)` rewrites the whole OPC zip in place, non-atomically. Write to a new path, then verify by re-opening *that file from disk*. In-memory objects (`graphic_frame.height`, `table.rows`) are not proof that the saved XML is right.

    ## Procedure

    **1. Save to a new file, keep the original.**

    ```python
    out = "deck.edited.pptx"
    prs.save(out)          # original deck.pptx untouched
    ```

    Back up the original (`shutil.copy2`) before you ever `mv` the result into place, and only do the `mv` after step 5 passes.

    **2. Re-open from disk in a fresh Presentation.**

    ```python
    prs2 = Presentation(out)   # throws if rels/content-types are broken
    ```

    Do not reuse the editing objects. Re-locating everything (e.g. table by header text) is itself a check that the structure survived.

    **3. Structural checks on what you changed.**

    - **Tables:** locate by header row text again (recurse groups), assert exactly one match; assert the expected row count; assert each expected cell text; recompute subtotal/Total from the rows and compare to the cell; assert
      `gf.height == sum(r.height for r in gf.table.rows)`
      and
      `gf.top + gf.height <= prs2.slide_height`,
      and that no shape you did not move now overlaps the new bottom. If the table uses explicit `a:tcPr` banding, assert the banding alternates after the insert/delete.
    - **Pictures:** assert the new `a:blip/@r:embed` resolves to the intended image part; assert the `p:pic` you removed is gone.

    **4. Relationship / package integrity.**

    - `zipfile.ZipFile(out).testzip() is None`.
    - Re-open succeeded (step 2) — that already catches dangling rel targets and missing parts.
    - Confirm `[Content_Types].xml` still has the defaults/overrides the media needs (e.g. `png`, `jpeg`).
    - Before any `slide.part.drop_rel(rId)`, count references yourself: `//@r:embed` and `//@r:link`, not just `//@r:id`. `drop_rel` only counts `r:id`, so an image rel always looks unused and is always dropped — only drop when no `r:embed`/`r:link` remains in the part.

    **5. Prove the removal in the saved bytes, not in memory.**

    For deleted text: unzip and inspect **every** `ppt/slides/slide*.xml` (and `ppt/notesSlides/*` if the text could have been mirrored), not just the edited slide. Caveat: text can be split across `a:r` runs, so a naive grep for the full phrase can give a false "absent". Better: re-open with python-pptx, walk every shape's `text_frame.text` (and table cells) across all slides and assert the string is absent; grep raw XML only for a distinctive, unlikely-to-be-split token. Assert zero hits.

    For a deleted/replaced image: list `ppt/media/*` in the saved zip and hash-compare against the old image bytes. If the old bytes are still present, a rel on this slide, a layout, or a notes part still points at it — find and drop it, then re-save and re-check.

    **6. Optional but cheap belt-and-braces.**

    - `soffice --headless --convert-to pdf deck.edited.pptx` (or open once in PowerPoint) — python-pptx tolerates some schema sloppiness that PowerPoint rejects; a conversion catches that.
    - Diff slide count and shape counts before/after to confirm you changed only what you intended.

    **7. Only now promote.**

    ```python
    shutil.copy2("deck.pptx", "deck.backup.pptx")
    os.replace("deck.edited.pptx", "deck.pptx")
    ```

    If any check in 3–5 fails, do not replace; fix and re-run from the untouched original.

    ## Failure modes this catches

    - Frame height left stale at the pre-insert `a:ext cy` (python-pptx does not follow XML row edits) → step 3 height assertion.
    - Table running past the slide bottom or into the footer → step 3 bounds/overlap check.
    - Removed row text still present on another slide or notes page → step 5 all-parts search.
    - Old image bytes still embedded because a rel was never dropped → step 5 media hash compare.
    - Dangling rel or missing content type that only surfaces on open → steps 2 and 4.

    If I don't know a specific detail of your deck (slide size, whether banding uses explicit `tcPr` fills), I'd read those from the file rather than assume — but the save-to-new-file, re-open, structural-assert, bytes-prove-removal sequence is the safe core.
- id: pptx-verify-02
  answer: |
    Answering from memory only — no files, no web, no key consulted.

    Assumption: the edits were done at the XML level (lxml/python-pptx deepcopy + insert/delete of `a:tr`, `a:tc`, `a:tcPr`, or blip/rId surgery), and the deck opens in python-pptx but PowerPoint offers "repair". That combination almost always means a **schema violation**, not corrupt bytes — python-pptx is lenient on re-open, so "it reopens fine" proves nothing.

    ## Most common causes, ranked

    **1. Cell count ≠ grid-column count.** Every `a:tr` must contain exactly as many `a:tc` as `a:tblGrid` has `a:gridCol` (merges use `gridSpan`, but the *cells* still must sum to the grid). Adding a row by deepcopying a row from a table with a different column count, adding a cell, or deleting a cell without deleting a `gridCol` is the single most frequent repair trigger.

    **2. Missing or misplaced `a:tblGrid` / `a:gridCol`.** Rebuilding a table by hand and forgetting the grid, or inserting an `a:tr` *before* `a:tblGrid` (rows must follow the grid), breaks it.

    **3. Child-order violations.** OOXML is sequence-validated, so order matters even when semantics look fine:
    - `a:tbl`: `a:tblPr?` → `a:tblGrid` → `a:tr+`. A `tr` inserted after `tblPr` but before `tblGrid` is invalid.
    - `a:tc`: `a:txBody` **then** `a:tcPr`. Writing `tcPr` first, or appending a second `txBody`, repairs.
    - `a:tcPr` line elements must precede fill properties (`lnL, lnR, lnT, lnB, …` then `solidFill`/`noFill`), and `a:extLst` must be last.
    - `a:tr`: `a:tc+` then optional `a:extLst` last.
    - `a:txBody`: `a:bodyPr` → `a:lstStyle?` → `a:p+`. A `p` inserted before `bodyPr` is invalid.

    **4. Missing required children.** `a:tblGrid` (not optional), at least one `a:gridCol`, `a:tc` needs a `a:txBody` with `a:bodyPr` and ≥1 `a:p`. A bare `<a:tc/>` or a cell whose body lost `a:bodyPr` repairs.

    **5. Inconsistent merge markers.** `gridSpan`/`rowSpan` must be ≥ 1 and the spanned continuation cells must carry `hMerge="1"`/`vMerge="1"`. Copy-pasting a merged row, or editing one side of a merge and not the other, produces a table PowerPoint can't reconcile.

    **6. Bad attribute values.** These are silent killers because they're well-formed XML:
    - `a:tr/@h`, `@sz`, `@gridSpan`, `@rowSpan`, `@marL/@marR/@marT/@marB`, `@cx/@cy` must be plain integers (EMU / hundredths of a point). `"12pt"`, `"1.5"`, `"-100"`, or a float where a long is expected repairs.
    - Booleans written by a language's default stringifier: `firstRow="True"`, `bandRow="False"`. Must be `1`/`0` (or `true`/`false`), never capitalized `True`.
    - `a:xfrm` on the parent `p:graphicFrame` missing `a:off`/`a:ext`, or with zero/negative extent.

    **7. Duplicate shape identity.** Every `p:cNvPr/@id` in a slide must be unique. Deepcopying a `p:graphicFrame` (or a picture) without renumbering the `cNvPr` id is a classic — PowerPoint usually repairs by deleting a shape, which looks like "my row vanished".

    **8. Dangling or duplicated relationships.** A `r:id` / `r:embed` / `r:link` pointing at an rId that no longer exists in the part's `.rels`, a rel whose target doesn't exist in the package, or two rels with the same rId. Very common after blip re-pointing and `drop_rel` work.

    **9. Package/container problems.** New or renamed parts not declared in `[Content_Types].xml` (missing `<Override>` or a missing `<Default Extension="xml">`), duplicate zip entries for the same part name, part names differing only by case, a slide not listed in `presentation.xml`'s `sldIdLst`, or an orphan part no rel reaches.

    **10. Text/encoding.** Unescaped `&` or `<` inside `a:t`; raw control characters (U+0000–U+0008, U+000B, U+000C, U+000E–U+001F) in cell text; or a fragment built from a string with an **undeclared namespace prefix** (e.g. `ns0:` or `a:` with no `xmlns:a` in scope) — that's a hard parse failure and an instant repair.

    **11. Wrong element identity in the frame.** `a:graphicData/@uri` must be the table URI (`http://schemas.openxmlformats.org/drawingml/2006/table`); a table grafted under the chart/picture URI, or extra children alongside `a:tbl`, invalidates the frame.

    ## Practical triage

    - The repair dialog's "Repaired Records / Repaired Parts" log names the offending part — read it, it usually points straight at `slideN.xml`.
    - Validate the edited part against the ECMA-376 XSDs (`xmllint --schema`) **before** opening; that catches items 1–6 and 10 deterministically.
    - Compare the edited slide XML against an untouched sibling slide with the same structure — diffing element order and attribute types finds the injected anomaly fast.
    - Distrust `python-pptx` re-open and `zipfile` integrity as proof; both accept schema-invalid XML.

    Where I'm less certain: the exact `minOccurs` on `a:tblPr` (I believe it's optional but always emitted in practice) and whether PowerPoint tolerates a missing `a:endParaRPr`. Neither is a usual repair cause, so don't spend time there first.
- id: pptx-verify-03
  answer: |
    No — not by deleting the `p:pic` alone.

    Deleting the picture element only removes the *reference* from that slide's XML. The slide part almost always still holds a relationship to the image part (`ppt/media/imageN.ext`), and python-pptx serializes every part reachable through a relationship, so the image bytes are still inside the saved `.pptx` and anyone can unzip it and pull them out.

    Three ways it survives:

    1. **The relationship is still there.** `p:pic` gone, `slide.part.rels[rId]` still points at the image part → part is written to the package.
    2. **The rId is shared.** python-pptx reuses one relationship per image part, so another picture/shape-fill (`a:blip/@r:embed`) in the same part may still legitimately use that rId.
    3. **Other parts reference it.** A layout, master, notes slide, or the `docProps/thumbnail.jpeg` can carry the same image or a render of it.

    Also note the trap: `drop_rel(rId)` counts only `//@r:id`, so an image rel counts 0 and is *always* dropped — it will happily kill a rel that another picture still uses via `r:embed`. Check embeds yourself before dropping.

    How to check (and prove it):

    ```python
    import hashlib, zipfile
    from pptx import Presentation

    prs = Presentation("in.pptx")
    slide = prs.slides[0]
    # ... delete the p:pic (recurse into GroupShape.shapes; p:pic can be nested) ...
    prs.save("out.pptx")

    secret_hash = hashlib.sha256(open("confidential.png", "rb").read()).hexdigest()

    with zipfile.ZipFile("out.pptx") as z:
        for n in z.namelist():
            if n.startswith("ppt/media/"):
                h = hashlib.sha256(z.read(n)).hexdigest()
                print(n, h, "<-- STILL THERE" if h == secret_hash else "")
        # any surviving pointer to the rId, anywhere in the package
        for n in z.namelist():
            if n.endswith(".xml"):
                x = z.read(n).decode("utf-8", "ignore")
                if 'r:embed="rId7"' in x or 'r:link="rId7"' in x:
                    print("still referenced in", n)
    ```

    Pre-save, inspect relationships directly:

    ```python
    part = slide.part
    xml = part.blob.decode()
    used = {rid for rid in part.rels
            if f'r:embed="{rid}"' in xml or f'r:link="{rid}"' in xml}
    for rid, rel in part.rels.items():
        if rel.reltype.endswith("/image"):
            print(rid, rel.target_part.partname, "in use" if rid in used else "unused")
    ```

    Only when the rId appears in none of `//@r:embed` / `//@r:link` on that part — and no layout/master/notes part points at the same image part — call `part.drop_rel(rId)`. Then save to a *new* file, re-open the zip, hash `ppt/media/*` against the original bytes, and confirm no entry matches. Hash comparison is the only real proof; grepping for the filename or eyeballing the media list is not.

    One more caveat: if you post-process the zip in place rather than letting python-pptx rewrite it, old entries can linger even after the rel is gone. Always write a fresh package.

    BOTTOM LINE: deleting `p:pic` hides the image, it does not remove it. Removing `p:pic` + dropping the now-unused image rel (after checking `r:embed`/`r:link` and other parts) removes it; verify by hashing `ppt/media/*` in the re-opened saved file.
```
