# step-5-preview-free — WITH-SKILL validation answers for docx (opencode-go, 2026-10-12); closed-book,
# derived/docx.step-5-preview-free.SKILL.md body prepended to _prompts/docx.md as active guidance.
- id: docx-model-01
  answer: |
    A .docx is a ZIP container following the Open Packaging Conventions (OPC). Key parts: `[Content_Types].xml` (maps part extensions/overrides to content types), `_rels/.rels` (package-level relationship index), `word/document.xml` (main body content), plus `word/styles.xml`, `word/settings.xml`, `word/header*.xml` / `word/footer*.xml`, and the actual image bytes in `word/media/*`. Each XML part has its own `.rels` file mapping rIds to other parts.

    A table's data lives in `word/document.xml` (or a header/footer part), as block-level content:

    - `w:tbl` — the table
      - `w:tblPr` — table properties: `w:tblStyle`, `w:tblW` (width), `w:tblBorders`, `w:tblLayout`, `w:tblLook`
      - `w:tblGrid` — holds one `w:gridCol` per column, each with a `w:w` width attribute (in twips)
      - `w:tr` — one per row
        - `w:tc` — one per cell
          - `w:tcPr` — cell properties: `w:tcW` (width), `w:gridSpan`, `w:vMerge`, `w:shd` (shading), `w:vAlign`
          - block-level content: `w:p` (paragraphs; a cell must contain at least one)
            - `w:r` runs, each with optional `w:rPr` (character formatting) and a `w:t` (text) element
            - `w:pPr` (paragraph properties) sits at the top of `w:p`
- id: docx-model-02
  answer: |
    Word splits visible text across multiple runs at arbitrary boundaries — formatting changes, spell/language marks, rsid edit markers, a user typing mid-word. "Widget C" in the cell is typically stored as e.g. `<w:t>Widget</w:t></w:r><w:r>...<w:t> C</w:t>`, so `run.text == "Widget C"` never matches.

    Reliable approach: search at paragraph/cell level, where all runs are concatenated:
    - `"".join(r.text for r in paragraph.runs)` or simply `paragraph.text` / `cell.text`.
    - Locate with a substring test: `if "Widget C" in paragraph.text`.
    - Then edit run-wise: find the run span covering the substring, put the full replacement text into the first involved run (preserving its `w:rPr`), and clear the leftover runs. Verify the involved runs have identical `w:rPr` before collapsing them into one, or the retained run's formatting will silently change.
- id: docx-model-03
  answer: |
    Table and cell widths use TWIPS (twentieths of a point): the `w:w` attribute of `w:tblW`, `w:tcW`, `w:gridCol`, `w:tblInd`, and cell/table margins. 1440 twips = 1 inch; 20 twips = 1 point.

    Image (drawing) sizes use EMU (English Metric Units), in `wp:extent` and the mirrored `a:ext`. 914400 EMU = 1 inch; 12700 EMU = 1 point.

    Conversions: 1 inch = 72 pt = 1440 twips = 914400 EMU. EMU values must be integers (it is an `xsd:long`); compute with integer math like `cx = round(width_in * 914400)` and `cy = cx * px_h // px_w`.
- id: docx-locate-01
  answer: |
    Locate by content, never by index (`doc.tables[2]` breaks the moment the document changes):
    - Iterate `doc.tables`; inspect the first row's cells via `cell.text` (run-joined) for the invoice header ("Item", "Qty", "Unit Price", "Amount"), or a distinctive heading paragraph near the table. Assert you found exactly one candidate table.
    - For the "Gadget D" row, scan rows and compare the item cell's text. Normalize whitespace (`" ".join(cell.text.split())`) because runs are split arbitrarily and stray spaces are common; require exactly one match before editing.

    Check before editing: it is a body row, not a header; whether that row's cells are merged (row.cells repeats merged cells — see locate-02); that cell indexing genuinely points where you think; recompute the Amount column total and assert it equals the printed Total (catches a wrong table or mangled input before you propagate an error); and back up the original file.
- id: docx-locate-02
  answer: |
    python-docx's `row.cells` returns one entry per GRID column. A horizontally merged cell (`w:gridSpan="2"`) is returned twice — the same `_Cell` wrapping the same `w:tc` element, once for each spanned grid position. So `row.cells[1]` is not a "second cell"; it is the merged cell itself.

    Consequences: deleting "the second cell" by index removes the merge-origin `w:tc`, leaving that row with fewer `w:tc` elements than grid columns — Word reports unreadable content / repairs the table. Iterating `row.cells` to count columns overcounts the merged cell (you get grid positions, not physical cells).

    Fix: count and index physical cells with `row._tr.xpath("./w:tc")` (i.e. `w:tc` children of `w:tr`); use `row.cells[i]` only when you mean the i-th grid position.
- id: docx-rowdel-01
  answer: |
    No. python-docx has neither `table.delete_row()` nor `row.delete()`. You remove the XML element yourself:

    ```python
    tr = row._tr
    tr.getparent().remove(tr)      # or: table._tbl.remove(tr)
    ```

    The remaining `w:tr` elements are contiguous siblings, so Word reflows automatically — rows below move up, there is no gap to close or shift manually.

    But deletion is not finished there: every number derived from that row (Total, subtotal, tax, amount-in-words, figures repeated in body text or headers/footers) must be recomputed from the surviving cells and written back, since a `{ =SUM(ABOVE) }` field keeps its cached result until you rewrite it.
- id: docx-rowdel-02
  answer: |
    If you just remove the row, its first cell — the `w:vMerge w:val="restart"` that anchors the merge — disappears, leaving the continuation cells (`<w:vMerge/>` in the next two rows) with no restart above them: an orphaned vertical merge. Word may report unreadable content and the merged cell's content/layout renders unpredictably.

    Correct order:
    1. Move the restart cell's paragraphs into the next row's first cell (the first continuation).
    2. Set `w:vMerge w:val="restart"` on that cell; if no further continuation remains below it, drop `vMerge` from it entirely.
    3. Then remove the row element.
    4. Afterwards verify no continuation cell in that column lacks a restart above it.
- id: docx-rowdel-03
  answer: |
    The Total shows the STALE cached value — still including the deleted row. A field's displayed value is the cached result stored between the field's `separate` and `end` (or inside `w:fldSimple`); python-docx never calculates, and Word does not recalculate on open. `<w:updateFields w:val="true"/>` only prompts the user and is not a substitute.

    Handle it by computing the correct sum from the surviving Amount cells and writing it into the field's result run(s), keeping the field code (`fldChar begin / instrText / separate / result runs / end`) intact and keeping the cell's number format and alignment. Also recompute every other derived figure — subtotal, tax, grand total, amount-in-words, and totals repeated in body text or headers/footers — then re-open and assert the printed values equal the recomputed ones.
- id: docx-rowdel-04
  answer: |
    Setting row height 0 (exact) with white text, or marking runs hidden (`w:vanish`), only HIDES the row. The `w:tr` and all its `w:t` text remain in the XML and in the zip, so: the data is still searchable, copyable and extractable (screen readers read white-on-white text; hidden text reappears whenever "show hidden text" is enabled or formatting changes); the file size is unchanged; and any miscalculated totals still reflect the row's presence in the underlying data. It is a cosmetic trick, not a deletion, and it silently corrupts downstream assumptions.

    Prove the row is really gone from the SAVED file, not from memory: unzip and count the `w:tr` elements (must drop by exactly one); grep every XML part for the deleted row's text — it must appear in no `w:t` of any part (also check headers/footers and `w:delText`); and confirm the recomputed totals equal the printed ones.
- id: docx-cell-01
  answer: |
    `cell.text = "SKU"` destroys the cell's existing content: python-docx clears all paragraphs and inserts one new paragraph with one bare run of plain text.

    Survives: the cell-level `w:tcPr` — width (`w:tcW`), shading (`w:shd`), vertical alignment, margins, merge settings — because those live on the `w:tc` element, not on the content.

    Lost: all run formatting (bold, 9 pt, white), all paragraph properties, and any extra paragraphs/runs. The result renders in the default font/size/color (typically black ~11 pt), which on dark shading is effectively invisible.

    Fix: edit the existing run in place — keep `cell.paragraphs[0].runs[0]`, set its text, and clear the extra runs (moving their text in first if the string spans several runs, asserting identical `w:rPr` before collapsing).
- id: docx-cell-02
  answer: |
    Everything derived from the quantity changes: that line's Amount (Qty x Unit Price, so 5 to 12 must be recomputed), the subtotal, any discount or tax computed on it, the grand Total — including the cached result of the `{ =SUM(ABOVE) }` field — the amount-in-words, and any figure repeated in body text, closing paragraphs, or headers/footers.

    Make it consistent by recomputing from the actual cells: read Qty, Unit Price and Amount for every surviving line, compute subtotal/tax/total in the script, write them back preserving each cell's number format (currency symbol, decimals, thousands separators) and alignment, rewrite the field's cached result, and update any text figures. Then re-open the saved file and assert the printed totals equal the recomputed ones.
- id: docx-cell-03
  answer: |
    python-docx edits the XML directly, so rewriting the `w:t` produces a SILENT change: no `w:ins`/`w:del` pair is created, and the user sees an ordinary edit with nothing to accept or reject — even though `w:trackRevisions` is on in settings.xml. If they expect a tracked, reviewable edit, you have not given them one.

    Also consider: the target text may sit inside existing `w:ins` runs (deleting a containing run leaves empty revision containers) or inside `w:del` content, whose text uses `w:delText` and will not be found by a `w:t` search — the visible string may span deleted and inserted runs.

    Decide intent first: for a tracked edit, build the `w:del` (runs with `w:delText`) plus `w:ins` runs with author/date/id yourself, or do the edit in Word with tracking on. For a clean final document, accept the existing revisions first (keep `w:ins` content, drop `w:del` content) and tell the user that is what you did.
- id: docx-relayout-01
  answer: |
    `table.add_row()` appends a row at the END of the table: a new `w:tr` with one bare, default-formatted `w:tc` per grid column — no content, no formatting copied from its neighbours.

    To insert in the middle, create the row then move the element, or copy a good one. The reliable approach for "correctly formatted": `copy.deepcopy` a sibling body row's `w:tr`, swap the `w:t` texts, then insert it before the Total row with `total_row._tr.addprevious(new_tr)`. (Equivalently `table.add_row()` followed by `total_row._tr.addprevious(new._tr)`, which loses formatting.)

    Locate both rows by their text, not by index. After inserting: re-check the alternating banding `w:shd` if the table is manually banded; verify any copied `w:gridSpan`/`w:vMerge` is still correct at the new position; and recompute the totals that must now include the new row.
- id: docx-relayout-02
  answer: |
    `table.add_column(width)` appends a new `w:gridCol` to `w:tblGrid` and appends one bare `w:tc` to EVERY row. Two problems: the new cells go at the END of each row's cell list, so the SKU column lands as the rightmost column rather than next to Item; and the table's total width grows by `width`, overflowing the right margin. Merged (gridSpan) rows also get an extra `w:tc` that breaks their grid mapping, and the new cells have default formatting.

    To do it properly: insert the new `w:tc` at the correct grid position inside each row (deep-copy an existing body cell to carry its `w:tcW`, margins, paragraph and run formatting, then set the text); insert the `w:gridCol` at the matching position; keep `sum(gridCol)` within the text width (page width minus left/right margins) by shrinking another column (e.g. Item) in BOTH its `w:gridCol` and every cell's `w:tcW`; and use fixed layout (`w:tblLayout w:type="fixed"` / `table.autofit = False`).
- id: docx-relayout-03
  answer: |
    Two causes:
    1. Each cell still carries its old `w:tcW`, which Word honors over `w:tblGrid`.
    2. With the default autofit layout (no `w:tblLayout w:type="fixed"`), Word recomputes column widths from the cell contents and the table width, effectively ignoring `w:gridCol` edits.

    Update: every cell's `w:tcW` (for merged cells, the SUM of the spanned gridCols), the table's `w:tblW` if the total changed, and set `w:tblLayout w:type="fixed"` (python-docx: `table.autofit = False`) so the grid and cell widths drive rendering. Verify numerically that `sum(gridCol)` equals page width minus margins.
- id: docx-tblins-01
  answer: |
    `doc.add_table()` appends the `w:tbl` at the END of the body — after all existing content, just before the body's final `w:sectPr`. It does not insert after "Notes:".

    To place it correctly: find the paragraph whose text (run-joined) is "Notes:", create the table, then move the element:

    ```python
    notes_paragraph._p.addnext(table._tbl)
    ```

    Remember Word needs a paragraph between/around tables: if the new table would sit directly before `sectPr` or directly after another `w:tbl`, insert a paragraph. Then set its style/borders, its column widths (`w:gridCol` + each `w:tcW`), and fixed layout to match the surrounding tables.
- id: docx-tblins-02
  answer: |
    python-docx resolves `table.style` by NAME against styles actually DEFINED in that document's `styles.xml`; "Grid Table 4 Accent 1" is not defined there, so the lookup raises KeyError. Word's style gallery shows built-in/latent styles (from the default template or theme) that exist as gallery entries, not as real definitions in the file — and the UI display name can differ from the internal `w:name`.

    Fix: enumerate what the document really has — `[s.name for s in doc.styles if s.type == WD_STYLE_TYPE.TABLE]` — and use an existing style (e.g. "Table Grid" if present). If none suits, either add a proper `w:style` of type "table" to styles.xml, or skip styles entirely and draw the rules directly: set `w:tblBorders` (top, left, bottom, right, insideH, insideV as single lines with size and color) inside `w:tblPr` — a ruled table with zero style dependency.
- id: docx-tblins-03
  answer: |
    `doc.add_table(rows, cols)` produces a table spanning the full available text width (page width minus the left/right margins), split equally among the columns — each `w:gridCol` gets width/cols and `w:tblW` is set to the full width. It comes with no style, no borders, and default autofit layout. (Older python-docx versions required an explicit EMU width argument — confirm behavior for the installed version.)

    To match the existing invoice table: copy the invoice's `w:tblPr` (style, borders, `w:tblW`, `w:tblLayout`) and its `w:tblGrid` widths — set each gridCol and every new cell's `w:tcW` to the invoice's per-column widths — turn autofit off (fixed layout), and keep the grid sum equal to the invoice's so it stays within the margins.
- id: docx-imgrep-01
  answer: |
    document.xml never stores the bytes. The reference chain is `w:drawing` > `wp:inline` (or `wp:anchor`) > `a:graphic` > `pic:pic` > `pic:blipFill` > `a:blip r:embed="rIdN"`. rIdN is a relationship of the OWNING part — document.xml.rels for the body, header/footer rels for those parts — targeting `word/media/imageN.*`, whose content type is declared in `[Content_Types].xml`. Position and size live in `wp:extent`, `wp:positionH/V` and the mirrored inner `a:ext` (EMU), not in the image file.

    Replace ONE picture:
    1. Locate the target blip (the drawing belonging to that logo).
    2. On the owning part: `rId, image = part.get_or_add_image(new_path)` — the tuple returns the rId FIRST.
    3. Set that blip's `r:embed` to the new rId and keep `wp:extent`. If the aspect ratio changed, keep cx and set `cy = cx * image.px_height // image.px_width` as integers in both `wp:extent` and `a:ext`.
    4. Do NOT overwrite `image_part._blob`: image parts are deduplicated by SHA1 across the whole package, so one part can back several pictures.
    5. Drop the old rel (see imgrep/verify-03), then confirm from the saved zip.
- id: docx-imgrep-02
  answer: |
    Because image parts are deduplicated by SHA1 across the package, and ONE part can back MULTIPLE pictures: the page-3 picture referenced the same `word/media/imageN.*` part (it shared the original bytes), so overwriting `image_part._blob` repointed every picture that uses that part at once — the logo and the unrelated page-3 image both changed.

    Fix: never mutate the part. Add the replacement as its own part with `rId, image = part.get_or_add_image(new_path)` on the owning part, then set only the target blip's `r:embed` to the new rId, leaving the original part (still referenced by the other picture) intact.
- id: docx-imgrep-03
  answer: |
    It is not in the body — it lives in a HEADER part (or footer / first-page / even-page header), which is why `doc.inline_shapes`, which only walks the main document part, does not list it.

    Find it: for each `doc.sections`, check `section.header`, `section.first_page_header` and `section.even_page_header`, and search each part's XML for `w:drawing` / `a:blip`. Note `is_linked_to_previous`: the first section's header part holds the actual content; later sections usually just inherit it and have no part of their own.

    Replace: `rId, image = header.part.get_or_add_image(new_path)`, set the blip's `r:embed` to that rId on the header part, keep `wp:extent`, and drop the old relationship (after counting `a:blip[@r:embed=oldRId]` yourself) in EVERY header/footer part that carried the logo. Then verify from the saved zip that no `.rels` targets the old media and no XML part references the old rId.
- id: docx-imgins-01
  answer: |
    `doc.add_picture(path, width, height)` appends a NEW paragraph containing an inline drawing at the END of the document body (it is implemented as an end-of-body paragraph with a run carrying the picture) — not after "Approved by:".

    Correct placement: add the picture INTO the target paragraph, or into a paragraph you insert after it:

    ```python
    run = approved_par.add_run()
    run.add_picture("signature.png", width=Inches(1.5))
    ```

    Passing only `width` makes python-docx scale the height automatically to preserve the aspect ratio (integer EMU math internally). If the image must occupy its own paragraph, insert a paragraph right after "Approved by:" (move a created `w:p` element with `addnext`) and add the run there. Verify rendered size by the image's ink bounding box, not the canvas, if the PNG carries a border or padding.
- id: docx-imgins-02
  answer: |
    Word does not auto-shrink an inline picture to its container: a 3-inch-wide image in a 1.2-inch cell overflows the cell edge into the neighbouring cell or page margin — it can overlap adjacent content or clip at the page edge, and the column does not grow to fit it.

    Size it deliberately: available width = cell width (its `w:tcW` / `w:gridCol`) minus the left and right cell margins (`w:tcMar`, default about 0.08 inch per side). Then `run.add_picture(path, width=Inches(available))` — height scales with the aspect ratio — or compute both dimensions in integer EMU (`cy = cx * px_h // px_w`) written to both `wp:extent` and `a:ext`. Alternatively widen the column, keeping the whole table inside the text width.
- id: docx-legacy-01
  answer: |
    No. python-docx only opens OOXML packages; a binary .doc (Word 97-2003) is not a ZIP and cannot be parsed, so opening it fails. The workflow is: convert first — `soffice --headless --convert-to docx report.doc` (or open and Save As .docx in Word) — then delete the row with python-docx on the converted file, recompute the totals from the surviving cells (the converter may flatten fields such as `{ =SUM(ABOVE) }` into static text), and save as .docx. Confirm with the user that .docx output is acceptable, or convert back to .doc if the old format is required. Verify the converted table's structure (merges, widths, fields) before trusting it.
- id: docx-legacy-02
  answer: |
    `textutil -convert docx` re-authors the document through macOS's text system rather than preserving it, so the table is rebuilt, not copied: vertical/horizontal merges (`w:vMerge`, `w:gridSpan`) can be flattened or restructured, column widths recomputed, cell shading and table styles changed, fonts altered, images re-encoded — and critically, FIELD CODES such as `{ =SUM(ABOVE) }` may be turned into static text or dropped. That silently invalidates assumptions behind a "simple" row delete, since merges, widths and live totals no longer exist in the form you expected.

    Detect it by inspecting the converted file: count `w:tbl`/`w:tr`/`w:tc`, check `w:gridCol` and `w:tcW` widths, grep for `w:vMerge`/`w:gridSpan`, check whether the Total is still a field (`w:fldSimple` or fldChar/`instrText` containing SUM), confirm `w:shd`/`w:rPr` survived, and diff text extraction (pandoc or textutil plain) before versus after.
- id: docx-legacy-03
  answer: |
    Because a LibreOffice instance is already running. The `soffice` launcher is a thin wrapper that forwards requests to the existing instance over the per-user-profile socket; that GUI instance (holding the profile lock) does not perform headless conversions, so the command returns immediately and writes no output. A stale lock in the default user profile produces the same symptom.

    Run it reliably from a script by ensuring no instance is running (or `pkill soffice` first / wait for exit) AND/OR by giving the headless invocation its own private user profile:

    ```text
    /Applications/LibreOffice.app/Contents/MacOS/soffice \
      -env:UserInstallation=file:///tmp/lo_profile_convert \
      --headless --convert-to docx --outdir /tmp/out file.doc
    ```

    The `-env` value must be a `file://` URL. Afterwards confirm the output file actually exists and check the exit status.
- id: docx-verify-01
  answer: |
    Safe save-and-verify procedure:
    1. Back up first: `cp invoice.docx invoice.original.docx`. Validate the INPUT before touching it — extract the text, read the table, recompute the Amount column and assert it equals the printed Total, so a bad input is caught rather than propagated.
    2. Save to a new file (or a temp path moved into place) — never destroy the only copy until verification passes.
    3. Re-open the SAVED file from disk with python-docx: assert the `w:tr` count dropped by exactly one, the deleted row's text appears in no `w:t` of any part (document plus headers/footers), the intended edits are present, and the printed totals equal the recomputed ones.
    4. Validate the package: `unzip -t` for zip integrity and parse every XML part for well-formedness; confirm every `.rels` target still resolves and content types cover any added parts.
    5. Cross-check with independent readers — pandoc text extraction and, on macOS, `textutil -convert txt` — diffing against the original so only the intended changes appear.
    6. Keep the original until the user signs off.
- id: docx-verify-02
  answer: |
    python-docx only needs well-formed XML, but Word enforces the OOXML schema and package integrity. Common structural mistakes in table edits:
    - Wrong element ORDER inside `w:tcPr`, `w:tblPr`, `w:pPr`, `w:rPr` (strict sequences — e.g. `w:shd` before `w:tcW`, or `w:tblLayout` before `w:tblBorders`).
    - `w:gridSpan` exceeding the number of `w:gridCol`, or removing a `w:gridCol` that cells still map to.
    - Removing a `w:vMerge` restart, leaving orphaned `w:vMerge` continuations.
    - An empty `w:tc` with no block child (a cell must contain at least one `w:p`).
    - Two adjacent `w:tbl` with no `w:p` between them, or a table as the last body element with nothing between it and `sectPr`.
    - Invalid attribute values: non-integer EMU floats (e.g. `cx="1371600.0"`), out-of-range widths, typo'd `w:val` enums.
    - Relationship damage: dropping a rel still referenced by `r:id`/`r:embed`, duplicate rIds, or a missing content-type override for an added media part.
    - Unbalanced tracked-change markup (`w:del` runs must use `w:delText`, not `w:t`; empty `w:ins`/`w:del` wrappers).
    - Removing namespace declarations that `mc:Ignorable` still names.
- id: docx-verify-03
  answer: |
    Removing the `w:drawing` is not enough: python-docx writes every part reachable through a relationship, so `word/media/imageN.*` stays in the saved zip as long as the owning part's `.rels` still targets it — the old logo's bytes are still extractable.

    Prove it from the SAVED file: unzip it, hash every `word/media/*` and assert the old image's hash/bytes are absent; grep every part's `.rels` (document, each header/footer) for any relationship still targeting the old media; and grep every XML part for the old rId.

    To actually remove it: for each part that owns drawings, count `a:blip[@r:embed="oldRId"]` yourself (`Part.drop_rel` checks only `r:id`, and SHA1 dedup means the part may back other pictures). When the count is zero in a part, call `part.drop_rel(oldRId)` on that part; do this in every part that showed the picture. Save, then repeat the zip-level verification until no media part, no `.rels` entry and no XML reference to the old rId remains — verified against the saved zip, never inferred from an in-memory part count.

# Recap — ASKED: 29 docx/python-docx questions from the blind sheet, own knowledge only, YAML {id, answer} out.
# WORKED: BUILT — full 29-record YAML answer list.
# FOUND: answers rely on internals I know solidly (OPC package, w:tbl/tc/tr/p/r/t chain, twips vs EMU, merge/gridSpan hazards, cached-field trap, blip/r:embed + rel lifetime, autofit vs fixed). One genuine uncertainty flagged inline: exact default width of doc.add_table(rows, cols) across python-docx versions (answered full-text-width-equal-columns, noted version dependency).
# DECIDED: no tools called at all — sheet is closed-book; uncertainties stated, not guessed over.
# BOTTOM LINE: 29/29 answered from knowledge, one version-dependent hedge, one answer stating limits of certainty.
# NEXT: none — sheet complete; if grader wants version-pinned add_table behavior, that needs the python-docx source to confirm.
