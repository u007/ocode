# deepseek-v4.1-flash — docx answers WITH SKILL (validation of the re-derived skill)

Model: `opencode-go/deepseek-v4.1-flash` (provider-stripped `deepseek-v4.1-flash`),
re-run 2026-10-21 after opencode-go updated the model behind this id
(`model_version: "4.1-2026-10-21"`). This REPLACES the earlier with-skill run for this id;
the previous answers are in git history.

Closed-book barrier:
- Each of the 29 questions in `docs/okf/_prompts/docx.md` was a fresh headless
  `ocode run -m opencode-go/deepseek-v4.1-flash -effort med` session (`bin/ocode`, built
  2026-10-20) under ocode's real agent system prompt.
- Fake HOME: no global CLAUDE.md/AGENTS.md, no memory, no persisted roots. Its only config
  denies every builtin tool and DISABLES the five existing `*-tuning-deepseek-v4.1-flash`
  Kaizen skills (plus `conduct-tuning-deepseek-v4-flash`), so the ONLY skill content the
  model saw is the prepended docx skill body (no double injection). Probe before the sweep confirmed: no tools advertised, no Model-Specific
  Directives block. The generic `[ocode:provider]` DeepSeek output-discipline block and the
  bundled skill-catalogue digest were still present (every user gets them).
- cwd: an empty dir named `work`; stdin `/dev/null`.
- Audit: every session sqlite checked after the sweep — 1 user + 1 assistant message,
  zero tool calls.
- Prompt per question: the body of docs/okf/docx/derived/docx.deepseek-v4.1-flash.SKILL.md (front matter stripped, as "Active guidance") + closed-book instruction + the one question; plain-text reply,
  wrapped into YAML below verbatim.

```yaml
- id: docx-model-01
  answer: |
    ## 1. What a .docx is, physically

    A `.docx` is **not** a single XML file. It is a **ZIP archive** built on OPC (Open Packaging Conventions): a flat bag of parts plus two kinds of relationship graphs. Unzip it and you get something like:

    ```
    [Content_Types].xml          # maps part names -> MIME-ish content types
    _rels/.rels                  # package-level rels: which part is the main document
    docProps/core.xml            # title/author/created (Dublin Core)
    docProps/app.xml             # app stats, company, word counts
    word/document.xml            # THE BODY: the actual text, tables, paragraphs
    word/_rels/document.xml.rels # rels FROM the body TO styles/images/headers/etc.
    word/styles.xml              # named styles, default fonts
    word/numbering.xml           # list definitions
    word/settings.xml            # compat flags, zoom, proofing
    word/fontTable.xml           # fonts used
    word/theme/theme1.xml        # theme colours/fonts
    word/header1.xml             # headers (one part each, own .rels)
    word/footer1.xml
    word/footnotes.xml / endnotes.xml / comments.xml
    word/media/image1.png        # binary media, referenced by rId
    customXml/*.xml              # optional data-bound parts
    ```

    Key facts:

    - The main document part's content type is `application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml`. `_rels/.rels` points at it with the `officeDocument` relationship type, which is how a reader finds `word/document.xml` (the path is convention, not law).
    - The markup inside is **WordprocessingML** (WordML), namespace `http://schemas.openxmlformats.org/wordprocessingml/2006/main`, conventionally prefix `w`. Strict OOXML uses a different namespace URI but the same element names.
    - **Relationships are not inline.** An image in the body appears as `<a:blip r:embed="rId7"/>`; `rId7` is resolved only by looking in `word/_rels/document.xml.rels`. The media part itself is just bytes in `word/media/`. Rels are per-part, and identical image bytes are often deduplicated into one media part shared by several `r:embed`s (and possibly by a header part too).
    - WordprocessingML is **not** a semantic model. It is a serialisation of a layout engine's inputs: there is no "cell value" object, only runs of text with properties.

    ## 2. Where a table's data lives

    Inside `word/document.xml`, under `w:document/w:body`, as a sibling of the paragraphs. `w:body` holds a sequence of block-level items — `w:p` and `w:tbl` (also `w:sdt`, `w:customXml`) — terminated by a single `w:sectPr` describing the section (page size, margins, columns).

    The table's text is in the `w:t` elements of the runs inside the paragraphs inside the cells. Nothing is stored out-of-line; there is no separate table part.

    ## 3. The element tree, table → text

    ```xml
    <w:tbl>                                  <!-- the table -->
      <w:tblPr>                              <!-- table properties -->
        <w:tblStyle w:val="TableGrid"/>      <!-- named style -->
        <w:tblW w:w="5000" w:type="pct"/>    <!-- preferred total width -->
        <w:jc w:val="center"/>               <!-- alignment -->
        <w:tblBorders>…</w:tblBorders>
        <w:tblLayout w:type="fixed"/>        <!-- fixed vs autofit -->
        <w:tblLook …/>
      </w:tblPr>

      <w:tblGrid>                            <!-- THE COLUMN GRID -->
        <w:gridCol w:w="2400"/>              <!-- twips, one per grid column -->
        <w:gridCol w:w="2400"/>
      </w:tblGrid>

      <w:tr>                                 <!-- table row -->
        <w:trPr>                             <!-- row props -->
          <w:cantSplit/>                     <!-- don't break across pages -->
          <w:trHeight w:val="400" w:hRule="atLeast"/>
          <w:tblHeader/>                     <!-- repeat as header row -->
        </w:trPr>

        <w:tc>                               <!-- table cell -->
          <w:tcPr>                           <!-- cell props -->
            <w:tcW w:w="2400" w:type="dxa"/> <!-- THIS cell's width (twips) -->
            <w:gridSpan w:val="2"/>          <!-- merged horizontally -->
            <w:vMerge w:val="restart"/>      <!-- merged vertically -->
            <w:shd w:fill="D9D9D9"/>         <!-- shading -->
            <w:tcBorders>…</w:tcBorders>
            <w:vAlign w:val="center"/>
          </w:tcPr>

          <w:p>                              <!-- paragraph; a cell MUST contain ≥1 -->
            <w:pPr>
              <w:jc w:val="right"/>
            </w:pPr>
            <w:r>                            <!-- run: a span of uniform formatting -->
              <w:rPr>
                <w:b/> <w:sz w:val="20"/> <w:color w:val="FF0000"/>
              </w:rPr>
              <w:t xml:space="preserve">Total: </w:t>   <!-- the actual text -->
            </w:r>
            <w:r>
              <w:rPr><w:b/></w:rPr>
              <w:t>1,250</w:t>
            </w:r>
          </w:p>

          <!-- cell may hold more w:p, a nested w:tbl, w:sdt, bookmark markers, etc. -->
        </w:tc>

        <w:tc> … </w:tc>
      </w:tr>

      <w:tr> … </w:tr>
    </w:tbl>
    ```

    So the chain is:

    `w:tbl` → `w:tr` → `w:tc` → `w:p` → `w:r` → `w:t`

    with the parallel property layers `w:tblPr`, `w:tblGrid`/`w:gridCol`, `w:trPr`, `w:tcPr`, `w:pPr`, `w:rPr`.

    ## 4. Consequences worth knowing

    - **A cell's text is not one string.** It is every `w:t` descendant, in document order, across however many runs, paragraphs (and nested tables) the cell holds. Runs split arbitrarily at formatting boundaries, spellcheck markers (`w:proofErr`), bookmark markers, and tracked-change wrappers (`w:ins`, `w:del`).
    - Other in-run content: `w:tab` (tab char), `w:br` (`w:type="page"` for page break), `w:cr`, `w:noBreakHyphen`. Text extraction that only reads `w:t` will silently drop tabs and breaks.
    - **A "value" can be a field, not text.** A Total might be `w:fldSimple w:instr=" SUM(ABOVE) "` or a complex field (`w:fldChar w:fldCharType="begin"` … `w:instrText` … `separate` … result runs … `end`). What you read is the **cached result** in the result runs' `w:t`; Word recomputes on open/print/update, so the cached text can be stale. If you edit a table, any field result summing it does not update itself.
    - **Two width sources, and Word trusts `w:tcW`.** `w:gridCol/@w:w` defines the grid (the layout skeleton, and what merge spans count against); each `w:tcW` declares that cell's preferred width. They can disagree — writing only the grid leaves rendering unchanged. A `gridSpan` cell's `tcW` should equal the sum of the grid columns it covers.
    - **Units:** `w:type="dxa"` is twips (1/20 pt, 1/1440 in); `pct` is fiftieths of a percent (5000 = 100%); `auto` means let the layout decide. Section text width is `w:pgSz/@w:w` minus `w:pgMar/@w:left` and `@w:right`.
    - **Cells must end with a block element**, practically a `w:p`. An "empty" cell contains an empty `w:p`, not nothing. Deleting every paragraph from a cell produces a file Word will repair.
    - `w:tbl` can nest inside `w:tc` (inner table is a sibling of `w:p` inside the cell), which is why naive `findall('.//w:tbl')` and `.//w:t` overlap.
    - Merges are not stored as spans of content: horizontal is `w:gridSpan` on the surviving cell; vertical is `w:vMerge w:val="restart"` on the top cell and `w:vMerge` (continuation, empty) on the cells below.

    **Short answer:** a `.docx` is a ZIP of OPC parts whose body is `word/document.xml` in WordprocessingML; a table lives there as a `w:tbl` in `w:body`, and its data is text inside `w:t` elements reached through `w:tbl → w:tr → w:tc → w:p → w:r → w:t`, decorated by `w:tblPr`, `w:tblGrid`/`w:gridCol`, `w:trPr`, `w:tcPr`, `w:pPr`, and `w:rPr`.
- id: docx-model-02
  answer: |
    ## Why the run loop misses it

    `run.text == "Widget C"` assumes Word stores text in word-sized runs. It does not.

    1. **Runs are arbitrary fragments, not words.** Word splits text into `w:r` elements at formatting changes, `w:rsid` save boundaries, proofing state (`w:proofErr`), language/spell-check spans, and ordinary editing. The cell can easily contain `"Widget "` + `"C"`, `"Wid"` + `"get C"`, or `"Widget"` + `" C"`. No single run equals the string, even though the rendered text does. A single run can also hold several `w:t` children.

    2. **`paragraph.runs` is shallow.** It returns only direct `w:p/w:r` children. Text inside `w:hyperlink`, `w:ins`, `w:smartTag`, `w:sdt`, `w:fldSimple`, or a nested table is invisible to that loop. Some python-docx versions also exclude hyperlinks from `paragraph.text`, so "the cell clearly shows it" and "the API sees it" can disagree.

    3. **Exact equality is too strict.** `run.text` maps `w:tab` to `\t` and `w:br`/`w:cr` to `\n`, ignores `w:instrText` and `w:delText`, and can carry trailing spaces, non-breaking spaces (U+00A0), smart quotes, or multiple paragraphs. `cell.paragraphs` also does not descend into nested tables.

    4. **Tracked changes/fields.** If markup is displayed, the visible text may be deleted text (`w:delText`, not `w:t`) or a cached field result that the next field update will overwrite.

    ## Find reliably

    Search the XML text nodes, not runs. Join the `w:t` descendants of the paragraph (which includes hyperlinks, `w:ins`, `w:sdt`, field results), normalize whitespace, then substring/regex match.

    ```python
    import re, unicodedata
    from docx.oxml.ns import qn

    def iter_text_nodes(p):
        # all w:t under the paragraph, incl. hyperlink/ins/sdt/fldSimple; skips instrText/delText
        return p._p.xpath(".//w:t")

    def norm(s):
        s = s.replace("\u00a0", " ")
        s = unicodedata.normalize("NFKC", s)
        return re.sub(r"\s+", " ", s)

    def find_in_cell(cell, needle, ignore_case=True):
        needle = norm(needle)
        for p in cell.paragraphs:                      # recurse into cell.tables if needed
            hay = norm("".join(n.text or "" for n in iter_text_nodes(p)))
            if (needle.lower() in hay.lower()) if ignore_case else (needle in hay):
                return p
        return None
    ```

    For a diagnostic, `cell.text` (paragraphs joined with `\n`) tells you whether the cell-level text contains the string at all; if it does but the run loop does not, fragmentation or a non-direct-child run is the cause.

    ## Replace reliably (span across text nodes)

    Map the match back to character offsets over the concatenated node texts, write the replacement into the first matched node, and splice the matched portion out of the rest. Keep each node's `xml:space="preserve"`.

    ```python
    def replace_in_paragraph(p, old, new):
        nodes = iter_text_nodes(p)
        texts = [n.text or "" for n in nodes]
        full  = "".join(texts)
        i = norm(full).find(norm(old))          # for ASCII/no-NBSP text this equals full.find(old)
        if i < 0:
            return False
        j = i + len(old)
        pos, first = 0, True
        for node, t in zip(nodes, texts):
            a, b = pos, pos + len(t)
            pos = b
            if b <= i or a >= j:
                continue
            ls, le = max(i - a, 0), min(j - a, len(t))
            if first:
                node.text = t[:ls] + new + t[le:]
                first = False
            else:
                node.text = t[:ls] + t[le:]
            node.set(qn("xml:space"), "preserve")
        return True
    ```

    Notes that matter:

    - The replacement inherits the formatting (`rPr`) of the run holding the first matched `w:t`. If the replaced text must keep its own formatting, clone the `rPr` of the run at the match start into a new `w:r`, split the boundary runs, and put the new text there.
    - Simpler alternatives, with trade-offs: consolidate runs first (merge adjacent `w:r` with identical `rPr`) and then replace; or overwrite the paragraph with one new run copying the first run's `rPr` — correct text, but it flattens intra-paragraph formatting.
    - Do **not** do `cell.text = ...`: it destroys run formatting, collapses/creates paragraphs, and drops hyperlinks/fields.
    - Do not replace inside `w:instrText` (field code) — `.//w:t` already skips it. Replacing a cached field result is fine until Word updates the field.
    - If the text exists only as `w:delText` or as an unaccepted revision, accept/reject revisions first; the body text you want is not in `w:t` yet.
    - Recurse through `cell.tables` (nested tables) and through `w:sdt` content if the cell uses content controls.

    ## Verify

    After saving, reopen the document, concatenate `w:t` under the cell's paragraphs, and assert the old string is absent and the new one present; also check the node count did not balloon and that `w:hyperlink`/rels survived if they were involved.
- id: docx-model-03
  answer: |
    ## Short answer

    - **Table and cell widths** (`w:tblW`, `w:tcW`, `w:gridCol/@w:w`, `w:tblInd`, and page size/margins) are in **twips** — twentieths of a point, normally flagged `w:type="dxa"`.
    - **Image sizes** inside `w:drawing` (DrawingML: `wp:extent/@cx`, `@cy`, `a:ext/@cx`, `@cy`) are in **EMUs** — English Metric Units, 1/914400 inch. Not twips, not pixels.

    ## Twips (table/cell widths)

    ```
    1 twip = 1/20 pt = 1/1440 in = 635 EMU
    pt  = twips / 20
    in  = twips / 1440
    cm  = twips / 566.929
    mm  = twips / 56.693
    ```

    Examples:
    - `w:tblW w:w="9360" w:type="dxa"` = 468 pt = 6.5 in (US-Letter text width at 1" side margins).
    - Two equal columns on that width: `w:gridCol w:w="4680"` and `w:tcW w:w="4680" w:type="dxa"` = 234 pt = 3.25 in each.
    - `w:pgSz w:w="12240" w:h="15840"` = 612 × 792 pt = 8.5 × 11 in.
    - `w:tblInd w:w="720"` = 36 pt = 0.5 in indent.

    **Percent variant:** a width with `w:type="pct"` is in **fiftieths of a percent** — 5000 = 100%, 2500 = 50%, 50 = 1%. `w:type="auto"` means the value is ignored. That fraction is a different unit from the dxa numbers and is easy to mix up.

    ## EMUs (image sizes)

    ```
    1 EMU = 1/914400 in = 1/12700 pt
    1 in  = 914400 EMU
    1 pt  = 12700 EMU
    1 twip = 635 EMU
    pt = emu / 12700
    in = emu / 914400
    cm = emu / 360000
    mm = emu / 36000
    ```

    Examples:
    - 1" × 1" picture: `cx="914400" cy="914400"`.
    - 2" × 1" picture: `cx="1828800" cy="914400"`.
    - A 192 × 96 px image at 96 dpi displayed at its natural size: 192/96 in = 2 in → `cx="1828800"`, `cy="914400"`.
    - `wp:extent` is the *displayed* size; the source pixel dimensions live in the image part. Resizing the drawing changes only the extent.

    ## Adjacent units that get confused with these

    | Thing | Attribute | Unit |
    |---|---|---|
    | Table/cell width, indents, spacing, page size, margins | `w:w`, `w:tblW/@w:w`, `w:tcW/@w:w` | twips |
    | Font size | `w:sz/@w:val`, `w:szCs` | half-points (24 = 12 pt) |
    | Border width | `w:top/@w:sz`, etc. | eighths of a point (4 = 0.5 pt) |
    | Border spacing | `w:top/@w:space` | points |
    | Percent table width | `w:type="pct"` value | fiftieths of a percent |
    | DrawingML image/shape size, offsets, line width | `wp:extent/@cx`, `a:ext/@cx`, `a:ln/@w` | EMU |
    | Legacy VML shape size | `v:shape style="width:72pt"` | points (not EMU) |

    Two practical notes:

    - **Legacy VML trap.** Images converted from old `.doc` files can appear as `v:shape`/`v:imagedata` with `style="width:...pt;height:...pt"` — those numbers are **points**, not EMUs. Same visual size, different arithmetic.
    - **Rounding.** Word stores integer twips, so a value derived from points/inches can drift by up to 1/20 pt (≈0.05 pt). Don't assert exact equality after a round trip; compare with a twip of tolerance.

    In python-docx terms: `Length` is EMU internally, and `Twips()`, `Pt()`, `Inches()`, `Emu()` convert between them. `cell.width`, `column.width`, and `row.height` come back as `Twips`; `InlineShape.width`/`.height` come back as EMU. Setting `cell.width = Inches(3.25)` works because the `Length` converts itself to twips on assignment.
- id: docx-locate-01
  answer: |
    ## Short answer

    Never address a table by `doc.tables[n]` or a row by `rows[k]`. Both shift the moment anyone inserts a table, adds a section header, or reorders rows. Locate the table by **content/caption identity**, locate the row by **a key column you resolved from the header row**, assert **exactly one match**, then inspect the target cell's XML before you write.

    ```python
    import re, unicodedata
    from docx import Document
    from docx.oxml.ns import qn

    def norm(s: str) -> str:
        s = unicodedata.normalize("NFKC", s)
        s = s.replace("\u00a0", " ").replace("\u200b", "").replace("\ufeff", "")
        s = s.replace("\u2018", "'").replace("\u2019", "'")
        return re.sub(r"\s+", " ", s).strip().casefold()
    ```

    ## 1. Find the invoice table

    `doc.tables` gives only **top-level body** tables, in document order. Tables in headers/footers (`section.header.tables`) and nested tables (`cell.tables`) are invisible to it — decide whether the invoice table could be in either and search those containers too. Tables in text boxes and `altChunk` content are not reachable at all.

    Prefer a semantic anchor, in this order:

    1. **Caption / alt text.** Walk the body in document order and pair each `w:tbl` with the preceding `w:p`. Match a paragraph whose style is `Caption` or whose text matches `^Table\s+\d+[:.]?\s*Invoice`, and/or use `w:tblPr/w:tblCaption` and `w:tblDescription` if the document sets them.
    2. **Header signature.** Require a header row to contain *several* expected keys, not one, so you can't hit "Invoice line items" or "Invoices outstanding":

    ```python
    def header_map(table, header_row=0):
        return {norm(c.text): i for i, c in enumerate(table.rows[header_row].cells)}

    REQUIRED = {"item", "qty"}          # pick the real pair for the document
    def looks_like_invoice(t):
        if len(t.columns) < 2:
            return False
        try:
            h = header_map(t)
        except IndexError:
            return False
        return REQUIRED <= set(h)

    candidates = [(i, t) for i, t in enumerate(doc.tables) if looks_like_invoice(t)]
    if len(candidates) != 1:
        raise LookupError(f"expected 1 invoice table, found {len(candidates)}")
    tbl_idx, table = candidates[0]
    ```

    3. **Style / fingerprint** (`table.style.name`, column count, header signature) only as a tiebreaker — styles are copied around and are not unique.

    Use `norm()` consistently: case, non-breaking spaces, zero-width characters, smart quotes, and double spaces are the usual reason a "correct" lookup misses. For a name like `Gadget D`, match the **whole normalized cell**, not a substring — `Gadget D` is a prefix of `Gadget D2` and `Gadget D (discontinued)`. If you must use `re`, anchor it: `^gadget d$`.

    ## 2. Find the "Gadget D" row

    Resolve which column holds the item name from the header row; do not assume column 0 (an index/number column is often first, and its numbers may be auto-numbering that never appears in `cell.text`).

    ```python
    h = header_map(table)
    key_col = h["item"]

    hits = []
    for r in range(1, len(table.rows)):            # skip header
        cell = table.cell(r, key_col)              # merge-aware
        if norm(cell.text) == norm("Gadget D"):
            hits.append(r)

    if len(hits) != 1:
        raise LookupError(f"'Gadget D' matched {hits!r}")
    row_idx = hits[0]
    ```

    Why `table.cell(r, c)` and not `row.cells[c]`: both are merge-aware, but `table.cell()` always returns the **origin** `w:tc` of a merged region. `row.cells` returns a tuple whose length equals the grid-column count with merged cells repeated, so `row.cells[3]` may be the same cell as `row.cells[2]`, and the "3rd logical column" may not exist. Compare `_tc` identity (`table.cell(r,c)._tc is table.cell(r,c2)._tc`) when it matters.

    Also: skip/flag rows that are structurally not line items — header repeats (`w:trPr/w:tblHeader`), totals ("Total", "Subtotal", "VAT"), notes and spacer rows. A total row can plausibly contain the string `Gadget D` in a description cell, so uniqueness on the key column is your real guard.

    If the document gives you a stable key (SKU, part number, invoice line ID), match on **that** column and treat the display name as a secondary check — display names get localized, re-cased, and re-spaced; IDs don't.

    ## 3. What to check before editing

    1. **Uniqueness, asserted.** One table, one row, and the key column index came from a header you actually saw. Fail loudly with the candidate list rather than editing the first hit.
    2. **Cell structure.** `cell.text` is a read-only aggregate. Inspect `len(cell.paragraphs)` and each `paragraph.runs`. Text is frequently split across runs (`Gadget` + ` D`, or a number split by spell-check/rsid), so matching on `cell.text` is right but writing on `cell.text` is wrong.
    3. **Fields.** A total/tax cell may be a `w:fldSimple` or a complex field (`w:fldChar` + `w:instrText`) whose *cached result* is what `cell.text` shows. Writing a run leaves the field in place and Word recomputes on open, discarding your value. Detect `fldChar`/`fldSimple`/`instrText` and either update the cached result deliberately or avoid the cell.
    4. **Content controls and tracked changes.** Text inside `w:sdt/w:sdtContent` or `w:ins` is not a direct child run of the paragraph, so `paragraph.runs` (and often `cell.text`) misses it. Treat such a cell as "not safely editable by python-docx" and fall back to raw lxml or refuse.
    5. **Merges.** Check `w:tcPr/w:gridSpan` and `w:vMerge`. Writing to a vertical-merge continuation row writes into the origin cell shared with other rows.
    6. **Lossless formatting.** `cell.text = "..."` **replaces all content with one paragraph and one plain run** — bold, font, size, colour, alignment, and any field are gone. If the cell is one paragraph with one run, set `run.text`. If multiple runs, edit the run that holds the value and keep the rest, or collapse deliberately and re-apply the saved formatting (`run.bold`, `.font.name`, `.font.size`, `.font.color.rgb`).
    7. **Value formatting.** Record the old text and match its convention exactly: currency symbol, thousands separators, decimal places, locale decimal comma, `%`, and sign/credit notation. `1,234.50` vs `1234.5` matters to humans and to downstream parsers.
    8. **Derived cells.** Nothing recalculates. After changing a qty or price, recompute every subtotal/total/tax/discount cell that depended on it and write each back through its existing run — same as the row-delete trap.
    9. **Work on a copy.** Snapshot the input, save to a new path, reopen and assert: the target cell now has the new value, the table still has the same row/column counts, and no other cell changed (diff a text dump of every cell before/after).

    ## Assumptions

    The invoice table is a top-level body table with an English header row naming an item column; the item name is unique within that column. If either doesn't hold, widen the search to header/footer and nested `cell.tables`, and match on a stable ID column instead of the label.
- id: docx-locate-02
  answer: |
    Short answer: `row.cells` returns a **layout-grid view**, not the row's actual `<w:tc>` elements. Its length is the table's grid-column count, and a cell with `gridSpan=2` appears **twice** in that tuple (normally the same `_Cell` object, wrapping the same `<w:tc>`). So "the second cell" by index may be the very cell you already saw at index 0, and counting `row.cells` counts grid slots, not real cells.

    How it works in python-docx (assumption: stock python-docx, no patched table internals):

    ```python
    # _Row.cells -> table.row_cells(idx) -> table.cell(r, c) for each grid column
    # Table._cells builds one entry per grid column:
    for tc in self._tbl.iter_tcs():
        for grid_span_idx in range(tc.grid_span):
            if tc.vMerge == ST_Merge.CONTINUE:
                cells.append(cells[-col_count])   # vertical merge: reuse cell above
            elif grid_span_idx > 0:
                cells.append(cells[-1])           # horizontal span: repeat same _Cell
            else:
                cells.append(_Cell(tc, self))
    ```

    Consequences:

    1. Deleting "the second cell" by index
       - If cell 0 spans columns 0–1, then `row.cells[1]` is the same cell as `row.cells[0]`. Removing `row.cells[1]._tc` deletes the merged cell itself (the content you wanted to keep), not a distinct second cell.
       - If the span sits elsewhere, index 1 still names whichever grid slot covers column 1, which may be a repeat of a neighbour — again the wrong `w:tc`.
       - Either way you end up with a `<w:tr>` holding fewer `<w:tc>` than the grid expects while some remaining cell still claims `gridSpan=2` (or the grid still has that column). The layout no longer matches `w:tblGrid`; subsequent row/cell indexing shifts, widths and borders land on the wrong cells, and Word typically shows a repaired/inconsistent table. python-docx also has no cell-delete API, so you are editing XML by hand with no consistency check.

    2. Counting columns by iterating `row.cells`
       - `len(row.cells)` overcounts: a row with 2 real cells (`tc1` spanning 2 grid columns, `tc2`) yields 3 entries. A merged header cell over three columns yields 3 entries for 1 real cell.
       - Any index arithmetic derived from that count is wrong for other rows, for header/data mapping, and for the XML (`row._tr.tc_lst`) — off-by-N errors that silently write the wrong column.
       - Iterating and writing is worse than counting: the duplicate entries point at the same `<w:tc>`, so the "second" write overwrites the first cell's text/formatting. A "clear all cells" loop thrashes one cell repeatedly and leaves real cells untouched. Column-width loops also set `tcW` twice on the merged cell (harmless-looking, but wrong when you intended per-grid-column widths).

    Do it correctly:

    - Real cells in document order: `row._tr.tc_lst` (list of `<w:tc>` elements), or `len(row._tr.tc_lst)` for the physical cell count.
    - Coverage: `cell.grid_span` (from `tc.gridSpan`, default 1). Sum of `grid_span` over `tc_lst` equals the grid-column count.
    - Grid width: `table._column_count` (derived from `w:tblGrid`), and `row.cells` length should equal it.
    - Duplicate detection: `row.cells[i] is row.cells[i+1]` (or `._tc is`) tells you a span starts at `i`.
    - Before deleting a cell from a spanning row, keep the grid consistent: either reduce that `tc`'s `gridSpan` by one, or remove the corresponding `w:gridCol` and fix every row — otherwise Word's layout and all your column indexes break. Recompute any Total/width values that depended on the removed cell (deletion updates nothing else).
    - Vertical merges have the same trap in the other direction: a `vMerge` continuation slot returns the cell from the row above, so `row.cells` can hand you a cell that isn't in that row at all.

    One-liner: treat `row.cells` as "one entry per grid column, with spanned cells repeated", use `row._tr.tc_lst` + `grid_span` when you mean physical cells, and never index-delete through `row.cells` without resolving the span first.
- id: docx-rowdel-01
  answer: |
    **No. python-docx exposes no `table.delete_row()` and no `row.delete()`.** `Table` has `add_row()`, `_Row` has no removal method, and there is no public API for deleting a row at all.

    You delete a row by removing its `<w:tr>` element from the table's XML:

    ```python
    def delete_row(table, idx: int) -> None:
        tr = table.rows[idx]._tr          # the <w:tr> element
        tr.getparent().remove(tr)         # parent is the <w:tbl>
    ```

    Equivalently `table._tbl.remove(table.rows[idx]._tr)`.

    Things to know:

    - **Nothing shifts.** The rows below simply become contiguous; there is no gap to close and no index fix-up. `table.rows` is rebuilt on each access, so re-read it after deleting rather than holding `_Row` objects (a held `row._tr` is now detached and its `.cells`/`.text` still work but are no longer in the document).
    - **Removal updates nothing else.** A Total cell that summed the deleted row keeps its old value — plain text or a field's cached result. Recompute and rewrite it yourself.
    - **Merged cells are fine.** Removing the `<w:tr>` removes whatever `gridSpan`/`vMerge` cells it contained; no extra cleanup needed.
    - **It is pure lxml**, so it works anywhere python-docx gives you the element (body tables, header/footer tables, nested tables — get the parent `tbl` from the row itself rather than assuming `doc.tables`).
    - If you want the row gone but its content preserved, that's a different operation (copy runs into the neighbouring row), not a delete.

    This is a known gap in python-docx, not a version issue: the library's table API has historically been additive only.
- id: docx-rowdel-02
  answer: |
    ## Short answer

    Deleting the row deletes the **anchor** of the merge. The `<w:vMerge/>` cells below become continuations with nothing to continue — the merged region loses its span and its text (the text lives only in the restart cell), and how the orphaned continuations render varies by consumer. You fix it by **moving the restart (and its content) down** to the new top cell before/after removing the row, or by **stripping `vMerge` everywhere** if the merge is meant to dissolve.

    ## What actually goes wrong

    A vertical merge is not a property of the table; it is a chain of per-cell flags in one grid column:

    ```xml
    <!-- row 1 (to be deleted) -->
    <w:tc><w:tcPr><w:vMerge w:val="restart"/></w:tcPr>
      <w:p>...the visible merged text...</w:p></w:tc>
    <!-- row 2 -->
    <w:tc><w:tcPr><w:vMerge/></w:tcPr><w:p/></w:tc>
    <!-- row 3 -->
    <w:tc><w:tcPr><w:vMerge/></w:tcPr><w:p/></w:tc>
    ```

    `w:vMerge` with no `w:val` (or `val="continue"`) means "continue the merge started above". The restart cell owns the span *and* the content; continuation cells are supposed to be empty and their content is not displayed while merged.

    Remove the restart row and you get:

    1. **An orphaned continuation.** Row 2's first cell is now the top of the column but still says "continue". There is no preceding restart, so the merge is structurally invalid.
    2. **The span collapses.** Word treats a dangling continuation as an ordinary cell (it usually does *not* prompt a repair, so don't expect an error — the damage is silent). The band that was one tall cell becomes separate cells, typically empty-looking. LibreOffice/Google Docs may render it differently again.
    3. **The content is gone.** The merged cell's text/paragraphs were stored in the deleted `w:tc`. It is not redistributed to the continuation cells, so anything the merged cell displayed is lost.
    4. **Latent text can resurface.** If the continuation cells weren't empty (some generators write text there despite the spec), that text was previously hidden and may now show up once the merge breaks.
    5. Anything computed from the rows (Totals, references) is stale — row removal updates nothing else.

    Note the contrast: deleting a row from the **middle** of a merge group is harmless. The remaining continuations still resolve to the same restart. Only deleting the restart row (or the whole group) breaks the chain.

    ## Correct procedure

    First decide the intent: **keep the merge (shrunk by one row)** or **dissolve it**.

    ### A. Keep the merge, re-anchored one row down (usual case)

    1. **Capture the restart cell** before deleting: its `w:tc` (content, `tcPr` shading/borders/`vAlign`, `tcW`, any `gridSpan`). Continuation cells are normally empty — verify rather than assume.
    2. **Delete the row:** `tr = row._tr; tr.getparent().remove(tr)` (python-docx has no row-delete API). Rows below close up automatically.
    3. **Promote the new top cell to the anchor.** In the new first row of the group, in the *same grid column*, set:
       ```xml
       <w:tcPr><w:vMerge w:val="restart"/> ... </w:tcPr>
       ```
       and move the saved content into it (preserve `pPr`/`rPr`). Two ways:
       - *Content-move:* insert the old cell's paragraphs into the new cell, delete the new cell's own (previously hidden) paragraphs, then set `vMerge` to `restart`.
       - *Cell-swap (cleaner, atomic):* deep-copy the old restart `w:tc` and put the copy in place of the continuation `w:tc` in the new top row — the copy already carries `val="restart"` and the content. It's the same grid column, so `tcW`/`gridSpan` still fit.
    4. **If only one row remains in the group**, there is no merge left to express: remove the `w:vMerge` element from that cell entirely (both `restart` and `continue` forms) so it renders as a normal cell with the content.

    ### B. Dissolve the merge

    Remove `w:vMerge` from **every** cell in that grid column across all remaining rows of the former group, including any explicitly written `w:val="continue"`. Then distribute the content deliberately (e.g. keep it in the new top cell, blank the rest) — otherwise previously hidden continuation text becomes visible.

    ### Gotchas either way

    - **`gridSpan`:** the `vMerge` applies to the whole spanned region. If the restart cell spanned N grid columns, the continuation cells must span the same N, and your new anchor must match. Mismatched spans corrupt the grid.
    - **Per-grid-column:** only the cells in the merged column carry the flag. Don't touch the row's other cells' `tcPr`, and don't try to "rebalance" widths while you're at it.
    - **Widths:** if you swap in a copied `w:tc`, keep `tcW` consistent with the rest of the column (set both `gridCol` and every cell's `tcW` if you change it).
    - **Recompute dependents:** Totals or fields that summed the row still hold the old value; rewrite them through their existing runs.

    ## Validation

    - Re-open the saved `.docx` and assert the invariant on the XML: every `w:vMerge[@w:val='restart']` in a grid column is followed, in consecutive rows, only by `continue` cells (or by nothing); no `continue` cell appears without a preceding `restart` in that column.
    - Confirm the merged text exists exactly once, in the new anchor cell.
    - Render-check in Word and one other consumer (LibreOffice/Google Docs) — this is the class of bug that passes a zip-level check and still looks wrong on screen.
    - If you deleted the row via code, re-run any row-derived totals and inspect one cell's `tcW` to confirm you didn't disturb widths.
- id: docx-rowdel-03
  answer: |
    **Short answer:** It shows the **old, stale total** — the number cached in the field when the document was last calculated. Deleting the row does not touch the field, and python-docx does not recalculate it or mark fields dirty. Word will not run `=SUM(ABOVE)` just because the file was opened.

    ## What actually happens

    A Word field is stored as three pieces of XML inside runs:

    - `w:fldChar w:fldCharType="begin"`
    - `w:instrText` — the code, ` =SUM(ABOVE) `
    - `w:fldChar w:fldCharType="separate"`
    - ordinary `w:t` runs — **the cached result** (the old number)
    - `w:fldChar w:fldCharType="end"`

    python-docx has no row-delete API; you remove the `<w:tr>` element directly (`tr.getparent().remove(tr)`). That removes the row's cells and nothing else. The Total cell's field XML, including its cached result, is untouched, so the document still displays the sum that included the deleted row.

    Recalculation only happens when something asks for it:

    - the user selects the field and presses **F9** (or Ctrl+A, F9 for the whole document),
    - Word prints with "update fields before printing" enabled,
    - the document's `settings.xml` contains `<w:updateFields w:val="true"/>`,
    - or the user toggles field codes and back / does an explicit update.

    So on a plain open, the answer is: **the stale cached value, not the new sum.** (LibreOffice may behave differently — it often recalculates on load — which is exactly why you should not rely on the viewer to fix it.)

    ## How to handle it

    Two independent things, and ideally do both:

    1. **Write the correct value into the cached result yourself.** Recompute the total from the remaining data (don't parse the field), then replace the text runs between the `separate` and `end` `fldChar`s. Keep the field structure intact so it remains a live field. If the result spans multiple runs, clear them and put the text in the first one. This guarantees the file shows the right number in every viewer, including ones that never recalculate.

    2. **Optionally force Word to recalculate on open** by adding `<w:updateFields w:val="true"/>` to `w:settings`. Caveats: it updates *all* fields in the document, not just this one, and it can surprise users or trip "this document contains fields that may refer to other files" prompts for certain field types. Writing the cached value in step 1 is the reliable fix; `updateFields` is a belt-and-braces measure.

    ## Gotchas

    - Don't just search for the literal string `=SUM(ABOVE)` and rewrite around it — the result lives after the `separate` marker, not next to the instruction text.
    - If the Total row itself is what you delete, you're removing the field, not updating it.
    - `=SUM(ABOVE)` sums numeric cells above the field in the same column, so once recalculated it will correctly exclude the deleted row — but only once recalculated.
    - Removing the row also does not rewrite any static paragraph text in the Total cell if the total is plain text rather than a field; handle that the same way.

    **If you have to pick one:** recompute and overwrite the cached field result. That is the only approach that is correct regardless of whether Word updates fields on open.
- id: docx-rowdel-04
  answer: |
    ## Why it isn't a deletion

    A Word table row is the `w:tr` element inside `w:tbl`. All three tricks leave that element in the XML:

    - `w:trHeight w:val="0" w:hRule="exact"` — a *formatting hint*. Word clamps exact heights to fit content (and to a minimum), and other renderers ignore it. The cells, their `w:p` and `w:r` children, `w:tcPr`, merges, and formulas are all still there.
    - White text (`w:color w:val="FFFFFF"`) — the text is still present, selected, copied, found by Find, spell-checked, read by screen readers, emitted in the PDF text layer, and exported by DOCX→HTML/text. It reappears under dark mode, high-contrast themes, selection highlight, or when someone changes the theme.
    - Hidden runs (`w:vanish`) — display-only formatting. Show/Hide ¶, Find, accessibility tools, and every text-extraction API still see it; it round-trips through save/open unchanged.

    Semantically the row is fully live: `len(table.rows)` is unchanged, `table.cell(r, c)` still addresses it, its row index and every row below keep their old indices, sorting/merging/conditional formatting treat it as data, and any `SUM`/field/cached Total still counts it. Deletion means removing the `w:tr` element (and then fixing whatever referenced it — Totals, bookmarks, comment ranges, content controls). Hiding is a *presentation* change; deletion is a *structural* one.

    ## How to prove it's gone

    Prove against the **saved and reopened** file, not the in-memory object.

    **1. Structural assertion (python-docx)**

    ```python
    from docx import Document

    before = [[c.text for c in r.cells] for r in table.rows]   # captured pre-edit
    doc = Document("out.docx")                                  # fresh open
    after  = [[c.text for c in r.cells] for r in table.rows]

    assert len(after) == len(before) - 1
    assert after == before[:i] + before[i+1:]        # exactly one row gone, order intact
    assert not any(sentinel in c.text for r in table.rows for c in r.cells)
    ```

    **2. Raw XML: no `w:tr`, and no hiding impostor**

    ```python
    from docx.oxml.ns import qn
    tbl = table._tbl
    print(len(tbl.findall(qn('w:tr'))))              # must be N-1
    xml = tbl.xml
    assert sentinel not in xml                       # text really absent
    assert 'w:hRule="exact"' not in xml or 'w:val="0"' not in xml
    assert 'w:vanish' not in xml
    assert 'w:color w:val="FFFFFF"' not in xml
    ```

    Count `w:tr` before/after rather than trusting `len(table.rows)` alone — it is the same element count, but checking both catches wrapper tricks.

    **3. Package-wide text check**

    Unzip and scan every story part, because the text could have been moved rather than removed:

    ```bash
    unzip -p out.docx word/document.xml | grep -c 'SENTINEL'     # must be 0
    # also: word/header*.xml, word/footer*.xml, footnotes.xml, endnotes.xml
    ```

    **4. Adjacency / no placeholder left behind**

    ```python
    assert removed_tr.getparent() is None            # detached from the tree
    assert prev_tr.getnext() is next_tr              # neighbours now contiguous
    ```

    Also assert there is no leftover empty trailing `w:tr`, which would produce a blank line even though the original row is gone.

    **5. Invariants that removal can silently break**

    - Each remaining `w:tr` has the expected number of `w:tc`, and `sum(gridSpan)` equals `len(w:tblGrid/w:gridCol)`.
    - No orphaned `w:bookmarkStart`/`w:bookmarkEnd`, comment ranges, or `w:sdt` anchors that lived in the removed row.
    - No `w:gridBefore`/`w:gridAfter` left inconsistent.
    - Every Total that summed the row was recomputed and now equals the sum of the surviving rows — removal itself updates nothing.

    **6. Optional render proof**

    ```bash
    soffice --headless --convert-to pdf out.docx
    pdftotext out.pdf - | grep -c 'SENTINEL'        # 0
    ```

    Also spot-check the PDF page count / row layout to confirm no zero-height or white-text artifact survived.

    The decisive evidence is (2)+(3): the `w:tr` is gone from `w:tbl`, and the string no longer exists in any document part. If either check fails, the row was hidden, not deleted.

    ---

    **Recap**

    ASKED — Explain why zero-height/white-text/hidden-run row "deletion" isn't deletion, and how to prove a row is actually gone.

    WORKED — BUILT a proof recipe: structural assertion on reopened file, raw-XML `w:tr`/impostor checks, package-wide text grep, neighbor adjacency, invariant checks, optional PDF render.

    FOUND — Hiding is purely presentational; the `w:tr` and its cells stay fully live (indices, Totals, copy, search, accessibility), and exact-0 height is a hint Word may clamp or ignore.

    DECIDED — Deletion = removing the element, so proof must be element-level plus text-absence across all story parts on the saved zip, not in-memory state.

    BOTTOM LINE: If `w:tr` count dropped by one and the text is absent from every `word/*.xml` part, it's deleted; otherwise it's just dressed up.

    NEXT — Recompute and rewrite any Total that included the row, and check for orphaned bookmarks/comments/content controls — removal fixes neither on its own.
- id: docx-cell-01
  answer: |
    Only **cell-level formatting** survives. In python-docx, `cell.text = "SKU"` clears every child of the `<w:tc>` except `<w:tcPr>`, then inserts a fresh paragraph and run. So:

    **Survives**
    - The cell's `<w:tcPr>` properties: dark shading/fill, borders, width/gridSpan, margins, vertical alignment.
    - Row-level properties: header-row repeat, row height, etc.
    - The table style and column layout.
    - Style-driven formatting that still applies through the table style (for example, if the shading or header bold comes from the table style rather than direct formatting).

    **Lost**
    - All direct run formatting: bold, 9 pt, white font color, font name, character spacing, etc.
    - All paragraph formatting: alignment, spacing, indents, paragraph style, numbering.
    - Everything else in the cell: additional paragraphs, hyperlinks, images, nested tables.

    The new “SKU” run gets default document formatting, usually the body font at default size and automatic/black color. The practical result is a dark-shaded header cell containing default black text — the dark background survives, but the white 9 pt bold text does not.

    To keep the formatting, write through the existing run (`cell.paragraphs[0].runs[0].text = "SKU"`) or copy the paragraph/run properties before clearing the cell.
- id: docx-cell-02
  answer: |
    ## What else has to change

    Changing Qty 5 → 12 on one line invalidates every value derived from it, not just the line:

    | Value | Why it changes |
    |---|---|
    | **Line amount** (Qty × Unit Price) | direct product |
    | **Line discount** (if per-line, % or fixed-per-unit) | scales with Qty |
    | **Subtotal** | sum of line amounts |
    | **Invoice-level discount** (if % of subtotal) | subtotal moved |
    | **Tax / VAT / GST** and each tax band | computed on the taxable base |
    | **Shipping / handling** (if per-unit or free-shipping threshold) | may cross a threshold or scale |
    | **Total** | subtotal − discounts + tax + shipping |
    | **Amount paid / balance due** | total moved |
    | **Amount in words** | must match the new total |
    | **Any duplicate presentation** | summary box, “N items” count, per-page totals, cover letter text, quantity totals row |
    | **Cached field results** | Word fields (`=SUM(...)`, `MERGEFIELD`, formula cells) hold stale cached text until updated |

    So: one physical edit, a cascade of dependent cells.

    ## How to make it consistent

    **1. Pick the authoritative inputs.** Qty and Unit Price are inputs; everything else is derived. Never patch a derived cell by guessing a delta — recompute it from scratch.

    **2. Recompute in dependency order**, with decimal arithmetic and a fixed rounding policy:

    ```
    line_amount[i] = round(qty[i] * unit_price[i], 2)
    subtotal       = sum(round(line_amount[i], 2))      # sum the *rounded* lines
    discount       = round(subtotal * disc_rate, 2)     # if percentage
    taxable        = subtotal - discount
    tax[j]         = round(taxable_share[j] * rate[j], 2)
    total          = subtotal - discount + sum(tax) + shipping
    balance_due    = total - amount_paid
    ```

    Rounding rules that keep the printed page internally consistent:
    - Use `Decimal`, not float (`Decimal(str(qty)) * Decimal(price_text)`).
    - Round each line to the currency’s minor unit with a documented mode (`ROUND_HALF_UP` is the usual invoice convention; some jurisdictions require half-even).
    - Sum the **rounded** line amounts for the subtotal, otherwise the visible lines won’t add up to the visible subtotal (the classic “off by 0.01”).
    - Compute tax on the rounded taxable base, and per tax band on that band’s rounded share.
    - Only round at each published boundary — don’t round intermediates twice.

    **3. Write the values through the existing formatting.** In python-docx, locate the cell by table + row label (not by a brittle numeric index, since rows may be merged), then set the text via the cell’s existing run so bold, font, currency symbol and number format survive. If a cell has multiple runs, write into the first and clear the rest; keep the same number format (`$1,234.56`, thousands separators, 2 dp). Preserve `wp`/`tcPr` untouched.

    **4. Handle fields explicitly.** python-docx does not evaluate Word fields. If totals are `w:fldSimple` or `fldChar` fields, replace the cached result between the `separate` and `end` runs, and set `<w:updateFields w:val="true"/>` in `settings.xml` so Word recalculates on open. Don’t leave the old cached number visible.

    **5. Update the words amount last.** Regenerate it from the final numeric total after all rounding, so “twelve” vs “five” and the spelled-out total both match.

    **6. Verify by re-reading the saved file.** Reopen the .docx and assert:
    - `sum(line_amounts) == subtotal`
    - `tax == round(taxable * rate, 2)`
    - `total == subtotal - discount + tax + shipping`
    - `balance_due == total - paid`
    - amount-in-words matches the numeric total
    - every occurrence of the old quantity (5) that should have changed is gone, and no orphan “5 × price” text remains in descriptions or summary blocks

    Only after these checks pass is the edit consistent; a visually plausible total that fails any identity above is still a bug.

    ### Assumptions
    I’m assuming a standard invoice layout (unit price unchanged, tax as a percentage, currency with 2 decimals, and derived values stored as plain text or Word fields rather than a live spreadsheet object). If tax is tax-inclusive rather than added on top, the algebra changes: back out the tax from the new gross line amount instead of adding it. If the invoice is driven by an embedded Excel/OLE object or a mail-merge data source, the correct fix is to change the source record and re-merge, not to hand-edit the rendered cells.
- id: docx-cell-03
  answer: |
    ## Short answer

    Nothing special happens. Rewriting `w:t` (or `run.text`, `paragraph.text`, `cell.text`) with python-docx writes the file directly — it is **not** intercepted by `w:trackRevisions`. Word only renders revisions that exist as markup in the XML (`w:ins`, `w:del`, `w:rPrChange`, `w:pPrChange`, `w:cellIns`/`w:cellDel`/`w:cellMerge`, `w:trPr/w:ins|w:del`, …). There is no diff engine that compares your output to the previous save. So your edit lands as untracked "final" text, even though the document still has tracking enabled and may contain old revisions elsewhere.

    ## What actually breaks

    - **You silently overwrite history.** If the text you touched was inside a `w:ins`, you have mutated a previously recorded insertion (now attributed to the original author/date). If it was inside a `w:del`, the old text lives in `w:delText`, not `w:t` — python-docx's `run.text` getter ignores `w:delText`, so that run looks empty, and writing `w:t` there does not resurrect or remove it. Word still treats it as deleted.
    - **`paragraph.runs` misses revision-wrapped runs.** It returns only direct `./w:r` children of `w:p`. Runs inside `w:ins`, `w:hyperlink`, `w:sdt`, `w:smartTag` are invisible. `paragraph.text = ...` / `cell.text = ...` then clears the direct content and adds one fresh run, leaving the revision-wrapped runs in place — you get duplicated or contradictory text.
    - **`cell.text = ...` is a wrecking ball.** It drops everything in the cell except `tcPr`: images, nested tables, fields, hyperlinks, bookmarks, comment anchors, and all revision markup.
    - **`run.text = ...` also erases `w:tab` / `w:br` / `w:cr`** and collapses multiple runs into one, losing per-run formatting.
    - **Fields and structures.** If the cell holds `w:fldSimple` or `w:instrText` field codes, or a `w:sdt` content control, blindly writing `w:t` corrupts the field/control.
    - **Word may still show both texts.** The doc keeps `w:trackRevisions` plus any existing revisions; the user sees insertions and strikethroughs until they accept/reject. Your "clean" edit isn't visually clean.

    ## What to consider / do instead

    1. **Decide what "clean edit" means.** Two distinct goals:
       - *The result should look final in Word* → you must **resolve existing revisions in XML** for that cell: delete `w:del` elements entirely, unwrap `w:ins` (hoist its `w:r`/`w:hyperlink` children into the parent), strip `w:rPrChange`/`w:pPrChange`/`w:cellIns`/`w:cellDel`, then edit plain text. Optionally clear `w:trackRevisions`.
       - *The edit should itself be a tracked revision* → you must **author** `w:ins`/`w:del` yourself.
    2. **Authoring tracked changes correctly** (illustrative):
       - Deleted run: move the original `w:r` into a `w:del` and rename its `w:t` → `w:delText` (keep `xml:space="preserve"`).
       - Inserted run: clone the `rPr` (so Word doesn't also report a formatting change), put it in `w:ins`, text in `w:t`.
       - Every `w:ins`/`w:del` needs `w:id` **unique within the document part** (use max existing id + 1; duplicate ids cause Word repair prompts), plus `w:author` and `w:date` in UTC W3CDTF (`2026-10-21T12:00:00Z`).
       - Paragraph-mark deletions go in `w:pPr/w:rPr/w:del`, not on a run.
    3. **Cell-level and row-level tracking is separate.** A cell can carry `w:tcPr/w:cellIns`, `w:cellDel`, `w:cellMerge`, and a row can carry `w:trPr/w:ins|w:del`. Editing text doesn't clear those; if the cell itself was tracked as inserted, accepting the text change won't change that.
    4. **Preserve the rest.** Edit through the existing `w:r` where possible rather than clearing the paragraph; keep `xml:space="preserve"` for leading/trailing spaces; don't reorder runs across `w:ins` boundaries — Word sometimes reports spurious "formatted" revisions when run ancestry changes.
    5. **Fallback that is much safer:** turn tracking off (`settings` element `w:trackRevisions` removed), let Word/LibreOffice/COM accept all revisions, then do a plain python-docx edit, and re-enable tracking afterwards if desired. Note removing `w:trackRevisions` does **not** delete existing revisions — they remain and still display.
    6. **Verify:** re-open the saved file and inspect `document.xml` — confirm no `w:ins`/`w:del` remain in the edited cell if you wanted a clean result, and that `w:id` values are unique if you authored revisions. Word's repair dialog is the practical test for malformed revision markup.

    ## Assumption

    I'm assuming "clean edit" means "final text, no revision marks left behind" and that you don't have Word/COM available to do the accept-all step. If instead you wanted the change recorded as a tracked revision, the answer flips to item 2: python-docx gives you no API, you must build the `w:ins`/`w:del` markup by hand.
- id: docx-relayout-01
  answer: |
    ## What `table.add_row()` actually does

    `Table.add_row()` is a thin, bottom-only convenience:

    ```python
    def add_row(self):
        tbl = self._tbl
        tr  = tbl.add_tr()                      # appends <w:tr> at the END of <w:tbl>
        for gridCol in tbl.tblGrid.gridCol_lst: # one cell per grid column
            tc = tr.add_tc()
            tc.width = gridCol.w                # writes w:tcW = that column's w:gridCol/@w:w
        return _Row(tr, self)
    ```

    So precisely:

    - It appends a new `<w:tr>` as the **last** row of the table. With a Total row already present, the new row lands **below Total** — wrong place.
    - It creates one `<w:tc>` per `<w:tblGrid>/<w:gridCol>` and sets each cell's `w:tcW` from the grid column. Widths therefore match the grid.
    - It copies **nothing else**: no `w:trPr` (`trHeight`, `cantSplit`, `tblHeader`), no `w:tcPr` (shading, borders, margins, `vAlign`, `gridSpan`), no paragraph/run formatting (`pPr`, `rPr`). The row only picks up whatever the table style / document defaults give it.
    - Consequence: if the body look is purely table-style-driven it may look acceptable, but any direct formatting on the neighbouring rows (bold, shading, currency alignment) is gone. With merged cells it is worse: it emits one cell per grid column, so a row whose siblings use `gridSpan`/`vMerge` will not line up.

    Moving that row up (`total_row._tr.addprevious(new_row._tr)`) fixes position only, not formatting — you would then have to rebuild `tcPr`/`rPr` cell by cell.

    ## Inserting a correctly formatted row in the middle

    Clone the row above (or any representative body row), then splice the clone in. A deep copy carries `trPr`, every `tcPr`, and the run/paragraph formatting; you only rewrite the text through the existing runs.

    ```python
    import copy
    from docx.oxml.ns import qn
    from docx.oxml import OxmlElement

    def set_cell_text_keep_format(tc, value):
        """Write into an existing cell without destroying pPr/rPr."""
        p = tc.find(qn('w:p'))
        runs = p.findall(qn('w:r'))
        if runs:
            runs[0].text = value            # CT_R.text setter keeps w:rPr
            for r in runs[1:]:
                p.remove(r)
        else:                               # empty cell: add a bare run
            r = OxmlElement('w:r'); t = OxmlElement('w:t')
            t.text = value; r.append(t); p.append(r)
        for extra in tc.findall(qn('w:p'))[1:]:   # keep a single paragraph
            tc.remove(extra)

    def insert_row_after(table, anchor_first_cell_text, values):
        anchor = next(r for r in table.rows
                      if r.cells[0].text.strip() == anchor_first_cell_text)

        new_tr = copy.deepcopy(anchor._tr)          # formatting comes along

        # do not duplicate identity/bookmark/revision nodes
        for tag in ('w:bookmarkStart', 'w:bookmarkEnd', 'w:commentReference'):
            for el in new_tr.findall('.//' + qn(tag)):
                el.getparent().remove(el)
        # if you cloned a header row, drop the repeat-header flag
        for trPr in new_tr.findall(qn('w:trPr')):
            for el in trPr.findall(qn('w:tblHeader')):
                trPr.remove(el)

        tcs = new_tr.findall(qn('w:tc'))
        assert len(tcs) == len(values)              # counts *cells*, not visible columns
        for tc, v in zip(tcs, values):
            set_cell_text_keep_format(tc, v)

        anchor._tr.addnext(new_tr)                  # exact slot: after Service G
        return new_tr
    ```

    Key points of that recipe:

    - `addnext` puts the `<w:tr>` immediately after the anchor row and before Total, which is the only schema-valid position needed (after `w:tblGrid`, among the `w:tr` children).
    - Deep copy preserves the per-column `w:tcW`, so widths already agree with the grid; no re-layout needed. If you had to change one column, remember `w:gridCol/@w:w` and every cell's `w:tcW` must be written (Word renders from `tcW`).
    - Text goes through the existing `w:r`, so bold/size/colour survive. Setting `cell.text = ...` would delete the paragraph content and lose `rPr`.
    - If the anchor uses `gridSpan`/`vMerge`, the clone inherits that geometry — matching the anchor's `<w:tc>` count (not the visible column count) is what the `zip` requires. A vertical merge copied this way will keep merging with the row above; strip or adjust `w:vMerge` first if that is not wanted.
    - Re-find rows by text after the insert (`table.rows` is generated from the live XML); cached indexes shift by one.

    ## Don't forget the Total row

    Insertion changes the sum, and Word does **not** recalculate cached field results for you:

    ```python
    total = next(r for r in table.rows
                 if r.cells[0].text.strip().lower().startswith('total'))
    # recompute from all body rows INCLUDING the new one
    for cell, value in zip(total.cells, new_totals):
        set_cell_text_keep_format(cell._tc, value)
    ```

    If the Total cell is a field (`w:fldSimple` or a complex field), rewrite only the cached result run(s) between `w:fldChar w:fldCharType="separate"` and `...="end"`, and leave `w:instrText` intact — replacing the whole cell text destroys the field. Also update any prose/header counts ("7 services") and renumber `Service N` labels if the insert shifts them.

    ## Verification

    Re-open the saved file and check, in order: row count went up by exactly one; cell text order is `Service G → new row → Total`; every row's sum of `w:tcW` equals the `w:gridCol` total; the new row's `w:tcPr` shading/borders XML equals the anchor's; the Total value equals the recomputed sum. If any Total was a field whose cached result you left stale, it will show the old number in Word until fields are refreshed.
- id: docx-relayout-02
  answer: |
    ## What `add_column(width)` actually does

    `table.add_column(width)` appends **one grid column at the far right** and one new cell to **every existing row**:

    - appends a `<w:gridCol w:w="...">` to `<w:tblGrid>`;
    - appends a `<w:tc>` to each `<w:tr>`, and writes that cell's `<w:tcW w:w="width"/>` (converted to twips);
    - it **does not** copy anything else — no `tcPr` shading/borders, no `trPr`, no paragraph or run formatting — and it **does not resize, move, or shrink** any existing column. Existing `gridCol`/`tcW` values stay put.

    So the table gets *wider* by `width`; it does not redistribute the space.

    ## Placing SKU "after Item", not at the end

    `add_column` only appends to the right edge. If `Item` is not the last column, appending puts SKU in the wrong place. You must then **move** it:

    - in `<w:tblGrid>`, move (or insert) the new `<w:gridCol>` to the index right after Item's grid column;
    - in every `<w:tr>`, move (or insert) the new `<w:tc>` to that same index.

    The grid column order and the per-row cell order must stay in lockstep, otherwise Word misaligns the table. If Item *is* the last column, plain `add_column` is correctly placed.

    ## Keeping it inside the page margins

    The usable text width is:

    ```
    page_width - section.left_margin - section.right_margin
    ```

    The sum of all `gridCol/@w:w` must be ≤ that. Since `add_column` doesn't rebalance, you must reclaim the SKU width from the existing columns:

    1. Compute the new target widths for **all** columns so the total fits (e.g. take `width` proportionally out of the existing columns).
    2. For every column set **both**:
       - `table.columns[i].width = w` (writes only `gridCol/@w:w`), **and**
       - `cell.width = w` on **every cell in that grid column, in every row** — Word renders from each cell's `tcW`, so the column setter alone changes nothing visible.
    3. Watch `gridSpan` cells: their `tcW` must be the **sum** of the grid columns they cover.
    4. Set `table.autofit = False` (fixed layout) so Word honours the widths instead of auto-fitting.
    5. Check `tblW` and `tblInd` too, if the source table sets them — an absolute `tblW` larger than the text width still overflows even with correct `gridCol`s.

    ## Making it look like the other columns

    `add_column` gives you a bare cell (default borders/shading from the table style, no run formatting). Copy from a sibling column:

    - **style**: the table already shares its style, so new cells inherit table-level borders/shading — but if the other columns set explicit `tcPr` (shading, borders, `vAlign`), copy that `tcPr` onto the new cells.
    - **header row**: write "SKU" through runs matching the existing header runs (bold, size, colour, font), and copy the header cell's shading/paragraph alignment. `add_column` copies none of this.
    - **body cells**: match paragraph formatting — alignment, spacing, and run properties (size/font) of the Item column's body cells, otherwise the new column looks visually offset.
    - **widths**: as above, the new column's cells already have `tcW = width` from `add_column`, but after rebalancing, re-set them explicitly.

    ## Bottom line

    `add_column(width)` only appends a `gridCol` and a `tcW`-bearing empty cell to each row at the right edge — no formatting, no rebalancing, no repositioning. To place SKU after Item you must reorder the `gridCol` and per-row `tc` elements; to stay in the margins you must shrink the other columns and write **both** `gridCol/@w:w` and every cell's `tcW` (summing for `gridSpan`), with `autofit = False`; to match the look you must copy the header's run formatting, shading, and cell/paragraph properties yourself.
- id: docx-relayout-03
  answer: |
    Because `w:gridCol/@w:w` is only the table grid definition — Word does not lay out columns from it. Word lays out each cell from its own `w:tcW` (the cell width in `tcPr`). Setting `table.columns[i].width = w` in python-docx writes only the `gridCol` value and leaves every cell's `tcW` at its old value, so Word keeps rendering the old widths. The grid can also disagree with the cells, and if the table is autofit Word may re-derive the layout anyway.

    What you must update:

    1. Every cell in that grid column, in every row: `cell.width = w` (i.e. write `w:tcW`). For a cell with `gridSpan`, `tcW` must be the sum of the grid columns it spans, not the single column width.
    2. Keep `gridCol` consistent with the cells (set both — grid value and `tcW`), so the grid and cell widths agree.
    3. Set `table.autofit = False` (fixed layout, `tblLayout` type `fixed`) so Word does not recompute widths from content.
    4. Keep the total of the grid column widths at or below the section text width (`page_width - left_margin - right_margin`), and also make the `tblW` consistent, otherwise Word may rescale or clip.

    In short: `gridCol` alone is inert for rendering — update the per-cell `tcW` (respecting `gridSpan`) on every row, plus fixed layout and a valid total width.
- id: docx-tblins-01
  answer: |
    `doc.add_table()` appends the new `<w:tbl>` at the end of the document body — just before the body's final `<w:sectPr>` — regardless of where the "current" paragraph is. python-docx has no cursor-relative `insert_table()`, and `add_table()` takes no position argument. So if you call it and stop there, the table lands after all existing content (or after the last content block before the section properties), not after "Notes:".

    To place it correctly, create it with `add_table()` and then move the XML element with `addnext()` on the target paragraph's `<w:p>`:

    ```python
    from docx import Document

    doc = Document("in.docx")

    notes_p = next(p for p in doc.paragraphs if p.text.strip() == "Notes:")

    tbl = doc.add_table(rows=3, cols=2)   # created at end of body

    # lxml addnext() MOVES the element, so this relocates the table
    notes_p._p.addnext(tbl._tbl)
    ```

    Key details:

    - `addnext()` moves the node rather than copying it, and the table element becomes the immediate next sibling of the "Notes:" paragraph, so it sits before whatever paragraph originally followed "Notes:".
    - `Table._tbl` is the `<w:tbl>` element; `Paragraph._p` is the `<w:p>`. Both are siblings in `<w:body>`, which is why this works. Placing after the *last* paragraph of the body still leaves `sectPr` last, so document validity is preserved.
    - `doc.tables` is computed by walking the body each access, so after the move its order reflects the new document order — no re-registration needed.
    - If the block that follows "Notes:" is itself a table, Word will render two adjacent tables as one. Insert an empty paragraph between them (`notes_p.insert_paragraph_before()` won't help here — add a paragraph after the moved table) if they must stay distinct.
    - If "Notes:" is inside a table cell, `addnext()` inserts into that cell instead of the body; then you want the cell's parent element or a different anchor.
    - `add_table()` produces a table with the document's default table style and an even column split; moving it does not inherit the surrounding table formatting. If it must match an existing table, copy that table's `style`, then set both `columns[i].width` and every cell's `width` (Word lays out from `tcW`, not `gridCol`), plus the header shading/bold runs.

    Assumption: "Notes:" is a normal body paragraph, and the document is a standard single-section body whose `sectPr` is the last `<w:body>` child.
- id: docx-tblins-02
  answer: |
    ## Why the `KeyError`

    `table.style = "..."` does not write the name into the document. The setter looks the name up in the **document's own styles part** (`word/styles.xml`) and copies out its `w:styleId`:

    ```python
    # roughly what the setter does
    style = document.styles.get_by_name("Grid Table 4 Accent 1", WD_STYLE_TYPE.TABLE)
    if style is None:
        raise KeyError("no style with name ...")
    tblPr.tblStyle.val = style.style_id
    ```

    So the `KeyError` means: *that name is not defined as a table style in this document's `styles.xml`.*

    Word's style gallery is mostly **latent** styles. The names appear in the UI because Word ships the definitions internally; they are only written into `styles.xml` when someone actually applies them in Word. A generated/company template typically contains only the styles it has used, so `"Grid Table 4 Accent 1"` (or whatever variant) is simply absent. python-docx has no built-in table-style library to synthesize it from — hence a lookup miss, not a rendering failure. The same would happen with python-docx's own default template for most accent table styles.

    Two secondary causes worth checking before blaming the template:

    - **Exact name.** Built-in names are matched exactly, case-sensitively. Word's name is usually `"Grid Table 4 - Accent 1"` (with a space-hyphen-space), while the `styleId` is `GridTable4-Accent1`. `"Grid Table 4 Accent 1"` can miss even where the style exists.
    - **Wrong style type.** A paragraph/character style with the same name doesn't satisfy a table-style lookup; the type filter rejects it.

    Diagnose with:

    ```python
    from docx.enum.style import WD_STYLE_TYPE

    print([s.name for s in doc.styles if s.type == WD_STYLE_TYPE.TABLE])
    print([s.style_id for s in doc.styles if s.type == WD_STYLE_TYPE.TABLE])
    ```

    ## Getting a ruled table

    ### 1. Use a table style the template actually defines

    ```python
    available = {s.name: s for s in doc.styles if s.type == WD_STYLE_TYPE.TABLE}
    table.style = available["Table Grid"]   # by object, avoids another lookup surprise
    ```

    Note `"Table Grid"` itself may be absent from a stripped template — check the list first. If it's there, this is the shortest path and it also brings the style's borders.

    ### 2. Materialize/import the built-in style

    Either apply the style once to a dummy table in Word and save the template (the definition lands in `styles.xml`), or copy the `w:style` element from a document that has it:

    ```python
    import copy
    from docx import Document
    from docx.enum.style import WD_STYLE_TYPE

    def import_table_style(dst, src_path, name):
        src = Document(src_path)
        style = next(s for s in src.styles
                     if s.type == WD_STYLE_TYPE.TABLE and s.name == name)
        dst.styles.element.append(copy.deepcopy(style.element))

    import_table_style(doc, "reference-with-style.docx", "Grid Table 4 - Accent 1")
    table.style = "Grid Table 4 - Accent 1"
    ```

    Caveats: the copied style may reference `basedOn`/`next`/`link` style IDs that must also exist, and you must not append a duplicate `styleId`. This preserves the real Word formatting (banding, accent colours, borders).

    Do **not** expect `doc.styles.add_style("Grid Table 4 - Accent 1", WD_STYLE_TYPE.TABLE)` to help: it registers an *empty* style definition (no `tblBorders`, no banding), so the lookup succeeds but the table still renders unruled. It only gives you a name to attach formatting to.

    ### 3. Skip styles and write borders directly (most reliable)

    Template-independent, and it always produces a ruled table:

    ```python
    from docx.oxml import OxmlElement
    from docx.oxml.ns import qn

    def add_grid_borders(table, sz=4, color="000000"):
        tblPr = table._tbl.tblPr
        for old in tblPr.findall(qn("w:tblBorders")):
            tblPr.remove(old)

        borders = OxmlElement("w:tblBorders")
        for edge in ("top", "left", "bottom", "right", "insideH", "insideV"):
            el = OxmlElement(f"w:{edge}")
            el.set(qn("w:val"), "single")
            el.set(qn("w:sz"), str(sz))     # eighths of a point: 4 = 0.5 pt
            el.set(qn("w:space"), "0")
            el.set(qn("w:color"), color)
            borders.append(el)

        # keep schema order inside w:tblPr
        tblPr.insert_element_before(
            borders,
            "w:shd", "w:tblLayout", "w:tblCellMar",
            "w:tblLook", "w:tblCaption", "w:tblDescription",
        )

    add_grid_borders(table)
    ```

    For a full "grid table with accent header" look without the style, also set header shading (`w:shd` in each header cell's `w:tcPr`) and bold header runs. For per-cell rules instead of table-wide rules, write `w:tcBorders` in each cell's `tcPr`.

    ### 4. Bypass the setter (works, but not portable)

    You can write the reference directly, since Word often resolves well-known built-in `styleId`s even without a `styles.xml` entry:

    ```python
    tblPr = table._tbl.tblPr
    tblStyle = OxmlElement("w:tblStyle")
    tblStyle.set(qn("w:val"), "GridTable4-Accent1")
    tblPr.insert(0, tblStyle)   # w:tblStyle is first child of w:tblPr
    ```

    Use only if you control the renderer: Word usually honours it, LibreOffice and some converters may fall back to the default (borderless) table. Adding the definition (option 2) or explicit borders (option 3) is deterministic.

    **Bottom line:** the exception is a name-lookup failure against the template's `styles.xml`, not a bad style string for Word. Check the exact name and the list of defined table styles, then either import/materialize the style definition or — safest for a template you don't control — set `w:tblBorders` (and header shading) yourself.
- id: docx-tblins-03
  answer: |
    ## Default width of `doc.add_table(rows, cols)`

    It is created at the **full text width of the last section** — `page_width - left_margin - right_margin` — with that width **divided evenly across the columns**. Concretely, python-docx computes `col_width = width // cols` and writes that same value to every `w:gridCol/@w:w` and every cell's `w:tcW`, and sets `tblW` to the full block width. The remainder from integer division is dropped, so the columns sum to at most the text width. The default table also has **no `tblStyle`** (Normal Table → no borders) and `autofit`/tblLayout is not fixed, so Word may re-flow it.

    ## To match the existing invoice table

    Copy all four things, not just the outer width:

    1. **Style** — `new.style = src.style` (e.g. the invoice's `Table Grid`/custom style).
    2. **Column widths** — for each column *i*:
       - `new.columns[i].width = src.columns[i].width` (this writes only `gridCol/@w:w`), **and**
       - `cell.width = src.columns[i].width` for **every cell** in that grid column, in every row (this writes `tcW`, which is what Word actually lays out from). For a cell with `gridSpan`, set the sum of the grid columns it covers.
    3. **Table-level width/indent/layout** — copy `tblW` and `tblInd` if the source sets them, and set `new.autofit = False` (fixed layout) so Word does not redistribute the widths.
    4. **Header formatting** — `add_table` gives plain cells; copy the source header row's `tcPr` shading/borders, `trPr`, and run formatting (bold/size/colour), or deep-copy the header `w:tr` and rewrite text through its existing runs.

    Column widths are the real control: once every `gridCol` and every `tcW` matches the invoice table, the overall width matches too, provided the grid total stays within the section's text width.
- id: docx-imgrep-01
  answer: |
    ## 1. How a picture references its bytes

    A DrawingML picture in `word/document.xml` is a chain of indirection, roughly:

    ```xml
    <w:drawing>
      <wp:inline>                        <!-- or <wp:anchor> for floating -->
        <wp:extent cx="1828800" cy="914400"/>     <!-- display size, EMU -->
        <wp:docPr id="1" name="Picture 1"/>
        <a:graphic>
          <a:graphicData uri=".../picture">
            <pic:pic>
              <pic:blipFill>
                <a:blip r:embed="rId7"/>      <!-- the pointer -->
    ```

    and then:

    ```
    a:blip/@r:embed="rId7"
       -> word/_rels/document.xml.rels : <Relationship Id="rId7"
              Type=".../image" Target="media/image1.png"/>
       -> the zip entry word/media/image1.png
       -> the actual PNG/JPEG bytes
    ```

    So three things are separate: the **rId** (a name local to the rels part that owns the blip), the **relationship** (rId -> target part), and the **media part** (the bytes). Headers/footers have their own rels parts and their own rIds, so the same image can be reached from `document.xml` and `header1.xml` through different rIds.

    Two consequences that matter:

    - Several `<a:blip>` elements can share one rId, and different rIds — even across different parts — can resolve to the *same* media part, because python-docx dedupes image parts by SHA1 of the bytes.
    - Legacy VML pictures (`<w:pict>/<v:shape>/<v:imagedata r:id="rId7"/>`) use `r:id` rather than `r:embed`; same indirection otherwise.

    ## 2. Why not overwrite the media bytes

    `image_part._blob = new_bytes` (or writing to `word/media/image1.png` directly) changes *every* picture that resolves to that part, in every header and footer too. Since parts are deduped, that can silently change a logo you never touched. It is not a "replace one picture" operation.

    ## 3. Replacing exactly one picture

    python-docx has no public `InlineShape.image = path` setter, so the supported approach is to add a new image part and repoint that one blip:

    ```python
    from docx.oxml.ns import qn

    def replace_picture(shape, new_path):
        # shape: an InlineShape/Shape from doc.inline_shapes, or found by walking XML
        blip = shape._inline.graphic.graphicData.pic.blipFill.blip
        old_rId = blip.get(qn('r:embed'))

        part = shape._parent.part          # doc.part for body; header.part for a header
        new_rId, _image_part = part.get_or_add_image(new_path)

        blip.set(qn('r:embed'), new_rId)
        # leave wp:extent / a:ext and the inline/anchor container untouched
    ```

    Key points:

    - `part.get_or_add_image(path)` returns `(rId, ImagePart)`. If the same bytes already exist anywhere in that part's rels it returns the existing rId; otherwise it creates a new media part. Either way the old part is left alone.
    - The owning part must be the one that actually contains the blip. For a body picture that is `doc.part`; for a header/footer logo it is `section.header.part` / `section.footer.part`. Using `doc.part` on a header blip gives you an rId that header1.xml cannot resolve.
    - Set only `r:embed` (or `r:id` for VML). Everything else in the drawing — the inline/anchor element, `wp:extent`, `a:ext`, `wp:docPr`, alignment, wrapping, distance attributes — is what encodes position and size, and you are not touching it.

    ## 4. Keeping position and size

    Position comes from the container (`wp:inline` vs `wp:anchor`, anchor offsets, `wp:positionH/V`, wrap settings) and size from `wp:extent cx/cy` plus `a:ext cx/cy` and the `a:srcRect` crop. Repointing the blip changes none of these, so the picture stays exactly where it was at exactly the same box.

    One caveat: if the new file has a different aspect ratio, the unchanged `wp:extent` will stretch it. If that matters, recompute one dimension from the new intrinsic size (keeping the box width, or the box height, or scaling to fit) and write both `wp:extent` and `a:ext`; optionally set `a:srcRect` to crop. If the ratio is the same, do nothing.

    ## 5. Cleaning up the old relationship

    Repointing does not delete the old rel or the old media part; the package keeps writing any part still reachable from a rel. Before dropping anything, count the remaining blips in that owning part whose `r:embed` still equals `old_rId`:

    ```python
    rels = part.rels
    still_used = any(
        blip.get(qn('r:embed')) == old_rId
        for blip in part.element.iter(qn('a:blip'))
    )
    if not still_used:
        part.drop_rel(old_rId)     # safe only after the count is 0
    ```

    `drop_rel` inspects only `r:id` attributes, not `r:embed`, so calling it while another blip still points at that rId will delete the rel and break the other picture. Also note `drop_rel` does not necessarily remove `word/media/...`: another part's rels may still reach the same deduped image part. Leaving the orphan rel is harmless and is the conservative choice.

    ## 6. Verifying

    Reopen the saved `.docx` as a zip and hash every `word/media/*` entry. The new file's SHA1 should be present; the old file's SHA1 should be gone if nothing else referenced it, and still present if it was legitimately shared. That check catches both failure modes: silently not replaced, and accidentally replaced everywhere.

    ## Caveats / assumptions

    - Assumes an OOXML `.docx` (DrawingML) picture, not an old `.doc`/binary or an embedded OLE object.
    - Assumes the picture is a normal inline or anchored image, not an `altChunk`, SmartArt preview, or chart-embedded blip (those have their own parts and rels).
    - If you need identical bytes reused across many documents, `get_or_add_image` dedupe behavior means two identical logos will collapse to one media part — which is fine, as long as you never mutate that part's bytes.
- id: docx-imgrep-02
  answer: |
    Because docx image parts are shared, not one-per-picture.

    python-docx (via the underlying OPC package) deduplicates image parts by content hash (SHA1 of the bytes). If the logo and the page-3 picture were identical or derived from the same bytes — e.g. the same file inserted twice, or one is a copy of the other — the document does not store two media entries. It stores one `word/media/imageN.png` and both `a:blip` elements point at it through the same relationship (or through two rels that resolve to the same part).

    Every picture on screen is just an `a:blip/@r:embed` reference into an image part. When you set `image_part._blob = new_bytes`, you mutate that single shared part. There is no per-picture copy to protect, so every reference to it — the logo, the page-3 picture, and any header/footer logo that resolved to the same part — renders the new bytes on next open. Nothing "spread" the change; there was only ever one blob.

    The correct fix is to add a new image part and repoint only the intended blip:

    ```python
    part = doc.part  # owning part of the blip (or section.header.part, etc.)
    new_rId, _ = part.get_or_add_image(new_logo_path)
    blip.set(qn("r:embed"), new_rId)   # only this picture
    # keep wp:extent / a:ext as-is unless the aspect ratio changed
    ```

    Then, if no blip in that part still uses the old rId, drop the old relationship — and check with a count over `r:embed`, because `drop_rel` only counts `r:id` and will happily delete a rel other blips still need. Verify by reopening the saved zip and hashing the media entries: the old image bytes should no longer appear if nothing else uses them.

    Assumption: the two pictures were byte-identical (or otherwise shared a part). If they were genuinely distinct images, overwriting one part could not affect the other — so in that case the premise would mean you overwrote the wrong part or repointed the wrong blip.
- id: docx-imgrep-03
  answer: |
    ## Where it is

    `doc.inline_shapes` only walks `inline` drawings (`wp:inline`) in the **main document part** (`word/document.xml`). A logo at the top of every page is almost always in a **header part** (`word/header1.xml`, etc.), reached through a section:

    - `section.header`
    - `section.first_page_header` (if "different first page" is on)
    - `section.even_page_header` (if "different odd/even" is on)

    Possibly also a watermark or VML picture in that same header part. Inside the header it may be `wp:inline`, `wp:anchor` (floating), or legacy VML (`v:imagedata`).

    Two related traps:

    - A `wp:anchor` (floating) drawing in the **body** is also invisible to `inline_shapes`.
    - The image relationship lives in `word/_rels/header1.xml.rels`, **not** `document.xml.rels`. So `doc.part.related_parts` won't show it.

    ## How to find it

    Walk sections, skip linked headers (they reuse a previous part), and scan each header/footer part's XML for blips. Dedupe by partname, since several sections can share one header part.

    ```python
    from docx.oxml.ns import qn

    def header_footer_parts(doc):
        seen = set()
        for s in doc.sections:
            for hf in (s.header, s.first_page_header, s.even_page_header,
                       s.footer, s.first_page_footer, s.even_page_footer):
                if hf.is_linked_to_previous:
                    continue                      # reuses an earlier part
                part = hf.part
                if part.partname in seen:
                    continue
                seen.add(part.partname)
                yield hf, part

    def find_logo_blips(doc):
        hits = []
        for hf, part in header_footer_parts(doc):
            root = part.element                    # w:hdr / w:ftr
            for blip in root.xpath('.//a:blip'):   # DrawingML (inline or anchor)
                hits.append((part, blip, 'embed'))
            for img in root.xpath('.//v:imagedata'):  # legacy VML / watermark
                hits.append((part, img, 'vml'))
        # body floating anchors that inline_shapes also misses
        for blip in doc.element.body.xpath('.//a:blip'):
            hits.append((doc.part, blip, 'body'))
        return hits
    ```

    `a:blip` carries `r:embed` (and sometimes `r:link`); `v:imagedata` carries `r:id`.

    ## How to replace it

    Re-point the rId on **that one blip** through the part that owns it. Never overwrite the image part's `_blob`: python-docx dedupes image parts by SHA-1, so one part can back the body logo, the header logo, and a footer icon simultaneously.

    ```python
    def replace_blip(part, blip, new_path, attr='r:embed'):
        old_rid = blip.get(qn(attr))
        new_rid, _image = part.get_or_add_image(new_path)   # BaseStoryPart
        blip.set(qn(attr), new_rid)

        # drop the old rel ONLY if nothing else in this part uses it
        still_used = part.element.xpath(f'.//a:blip[@r:embed="{old_rid}"]') \
                   + part.element.xpath(f'.//v:imagedata[@r:id="{old_rid}"]')
        if not still_used:
            part.drop_rel(old_rid)
        return old_rid, new_rid
    ```

    Notes that matter:

    - `part.get_or_add_image(...)` takes the `HeaderPart`/`FooterPart`/`DocumentPart` itself; it returns `(rId, image_part)`. Do **not** call it on `doc.part` for a header image — you'd add a rel to the wrong part.
    - Leave `wp:extent` / `a:ext` alone. Only adjust one dimension if the new logo's aspect ratio differs, otherwise Word stretches it.
    - `drop_rel` counts only `r:id` attributes, never `r:embed`. That's why you must count matching blips yourself before calling it; blindly dropping can blank other pictures that shared the rel.
    - Because header parts are shared across sections, fixing the one header part that all sections point to fixes every page.
    - Verify after `doc.save(...)`: reopen the `.docx` as a zip and confirm no `word/media/*` entry still hashes to the old image bytes. If the old hash survives, another blip (body, footer, or a second header) still references it.

    If the "logo" is actually a page background/watermark, it is still a VML `v:imagedata` (or `w:background`) inside the header part — the same find/re-point procedure applies, using `r:id` instead of `r:embed`.
- id: docx-imgins-01
  answer: |
    **Short answer:** `doc.add_picture()` does not insert at the paragraph you have in mind. It appends a brand-new paragraph to the end of the document body (last child position, just before the final `w:sectPr`, i.e. after any trailing table) and puts an inline picture in a run inside it. There is no "cursor" in python-docx, so if "Approved by:" is not the last paragraph, the signature lands on the last page instead. You must create the paragraph at the right XML position yourself and call `run.add_picture()` on it.

    ## Why `doc.add_picture` lands at the end

    It is a convenience wrapper:

    ```python
    Document.add_picture(...) -> body.add_picture(...)
    body.add_picture(...)     -> self.add_paragraph().add_run().add_picture(...)
    ```

    `CT_Body.add_p()` inserts a `w:p` before `w:sectPr`, so the picture paragraph is always the last block of the body. It also gets the default (Normal) paragraph style and default spacing, so it may not match the surrounding text.

    ## Correct placement: create the `w:p` after the anchor, then fill it

    Public API has `insert_paragraph_before()` but no "after", so go one level down:

    ```python
    from docx import Document
    from docx.shared import Inches
    from docx.text.paragraph import Paragraph
    from docx.oxml import OxmlElement

    doc = Document("in.docx")

    # Match on all descendant text: Paragraph.text skips runs inside w:hyperlink
    anchor = next(
        p for p in doc.paragraphs
        if "".join(p._p.itertext()).strip() == "Approved by:"
    )

    new_p = OxmlElement("w:p")
    anchor._p.addnext(new_p)                    # sibling immediately after anchor
    para = Paragraph(new_p, anchor._parent)     # wrap so the API works on it
    para.style = anchor.style                   # optional: match surrounding block
    para.paragraph_format.space_before = 0
    para.paragraph_format.space_after = 0

    shape = para.add_run().add_picture("signature.png", width=Inches(1.5))
    print(shape.width, shape.height)            # 1.5in, proportional
    ```

    `addnext` is valid even when the anchor is the last body paragraph: the new `w:p` goes between the anchor and `w:sectPr`, and `sectPr` stays last.

    Equivalent trick if you prefer to stay in the public API: if a paragraph follows the anchor, call `following.insert_paragraph_before()` and add the picture to that new paragraph. It has no answer for "anchor is last," which is why the `addnext` form is the general one.

    ## Keeping the aspect ratio

    Pass **only** `width` (or only `height`). python-docx reads the image's native pixel size and scales the other dimension proportionally — DPI only affects the "native" size, not the ratio. Passing both `width=` and `height=` stretches the image. You can also post-adjust, but it's unnecessary:

    ```python
    shape.height = int(shape.width * native_h / native_w)   # only if you want explicit control
    ```

    ## If "right after the paragraph" means inside it

    Appending to the existing paragraph's last run is simpler and avoids an extra empty-looking block:

    ```python
    anchor.add_run().add_picture("signature.png", width=Inches(1.5))
    ```

    The picture then sits inline at the end of the "Approved by:" line and inherits that paragraph's style/spacing. Use the new-paragraph form when the signature must be its own block (own alignment, own spacing, doesn't inherit a heading or bordered paragraph's formatting).

    ## Gotchas worth checking

    - **Anchor inside a table or text box:** `doc.paragraphs` only walks body-level `w:p`. Iterate `doc.element.body.iter(qn("w:p"))` and wrap with `Paragraph(el, doc._body)` (or use `cell.paragraphs` for cell content) — otherwise `next(...)` raises `StopIteration`.
    - **Matching text:** `paragraph.text` concatenates only direct `w:r` children, so text inside `w:hyperlink` or `w:smartTag` is invisible to it. Use `itertext()` as above, or accept `startswith`/`in` on `p.text` if you know the layout.
    - **Two matches:** "Approved by:" can appear in a TOC, header, or earlier boilerplate; pick the intended one deliberately (index, or scope to the last occurrence) rather than `next()` blindly.
    - **Inline vs floating:** `add_picture` produces an `InlineShape` — it flows with text, no wrap settings. Floating/anchored images with text wrapping are not supported by python-docx and require raw `wp:anchor` XML.
    - **Verify after save:** reopen the file and assert `len(doc.inline_shapes) == expected` and that the shape's width is `Inches(1.5)`; also confirm the paragraph order by walking `doc.element.body` children, since that is the only proof the image really sits after the anchor.
- id: docx-imgins-02
  answer: |
    ## What happens

    `add_picture()` with no size argument uses the image's **native size** — pixels ÷ DPI. A "3-inch" image is stored that way: `wp:extent cx="2743200"` (3 in × 914400 EMU). Nothing in the DOCX ties an inline picture's extent to the cell it sits in.

    So a 3-inch inline image in a 1.2-inch cell:

    - **Does not shrink.** Word has no "fit to cell" behavior for inline images. It renders at its extent.
    - **Overflows the cell.** In a fixed-layout table the picture is painted over the neighbouring cells and into the page margin. It is not clipped and nothing is cropped.
    - **In an autofit table, Word will try to widen the column** to the image width, which can blow the table past the page width; other columns get squeezed. Either way the layout is wrong.
    - **The row height grows** to the picture's height, because the image sets the line height of its paragraph.
    - **No error is raised.** The file is valid; this is purely a layout failure, so it survives a save/reopen and shows up only in Word.

    ## How to size it

    Compute the usable width first — a cell's width is not its content width:

    ```
    usable = cell_width − cell.left_margin − cell.right_margin
    ```

    Default `tblCellMar` is 0.08 in per side, so a 1.2 in cell gives ~1.04 in (≈ 950 976 EMU).

    **Preferred: pass width at insertion.** python-docx then scales height proportionally, preserving aspect ratio:

    ```python
    from docx.shared import Inches
    run.add_picture("logo.png", width=Inches(1.04))
    ```

    **Post-insert is riskier.** `InlineShape.width = Inches(1.04)` writes only `cx` (both `wp:extent/@cx` and `a:ext/@cx`); it does **not** touch `cy`, so the image distorts. If you must resize after the fact, set both and compute the height yourself from the native ratio.

    **Also pin the cell width**, or "1.2 inches" may not be what Word lays out:

    ```python
    table.autofit = False                 # fixed layout
    for row in table.rows:
        row.cells[i].width = Inches(1.2)  # tcW is what Word renders from
    table.columns[i].width = Inches(1.2)  # gridCol, needed as well
    ```

    (`columns[i].width` alone writes only `gridCol` and changes nothing visible.)

    ## Notes

    - To crop rather than scale, resize does not help — crop the file beforehand (PIL) or add `a:srcRect` to the `pic:blipFill`. Resizing only scales.
    - If you specify only `height`, the same proportional logic applies to width.
    - Very large pixel dimensions produce very large native sizes (e.g. a 3000 px image at 96 DPI is 31 in), so always pass an explicit width when the target is a table cell.
    - Centering the containing paragraph (`WD_ALIGN_PARAGRAPH.CENTER`) keeps a narrowed image tidy in the cell.
- id: docx-legacy-01
  answer: |
    **Short answer: no.** python-docx reads/writes only Office Open XML (`.docx`/`.docm`). A Word 97–2003 `.doc` is an OLE2 compound binary with a different content type, so `Document("report.doc")` fails at package-open time (typically `PackageNotFoundError` / "not a Word file, content type is ..."). python-docx also has no writer for `.doc`, so it cannot save back to the legacy format either.

    **Workflow**

    1. **Keep the original untouched** (byte-for-byte copy) — the conversion is lossy and you want a rollback.
    2. **Convert `.doc` → `.docx`** with a real Word engine, not a text extractor:
       - Preferred: LibreOffice headless
         ```bash
         soffice --headless --convert-to docx --outdir converted report.doc
         ```
       - Or Microsoft Word (COM/AppleScript "Save As" docx), which gives the best fidelity for complex tables/fields.
       - `antiword`/`catdoc`/`pandoc` are not substitutes: they extract text or fail, and destroy table structure.
    3. **Do the edit in python-docx** on the converted file. Row deletion is element removal; nothing else updates automatically:
       ```python
       from docx import Document
       doc = Document("converted/report.docx")
       table = doc.tables[0]
       row = table.rows[3]

       # capture anything you must recompute BEFORE removing
       tr = row._tr
       tr.getparent().remove(tr)   # remaining rows stay contiguous; no gap to close

       # removal does NOT update Totals/fields that summed the deleted row:
       # recompute from table.rows[...] and write back through the cell's existing run
       doc.save("converted/report.edited.docx")
       ```
       For a Total cell: read the remaining rows' values, sum them, then write the result through the existing run (`cell.paragraphs[0].runs[0].text = ...`) so formatting survives. If the total is a `w:fldSimple`/`FIELD`, the cached result is what Word shows until refresh — update the cached run, and note that Word will recalc on open/F9.
    4. **Validate**: reopen the saved file, assert `len(table.rows)` dropped by exactly one and the Total matches the remaining rows; unzip and inspect `word/document.xml` if you need proof.
    5. **If the deliverable must be `.doc`**: convert back headless (`--convert-to doc`). Treat this as a formatting-risky round trip — verify tables, fields, headers, and images in Word. Best practice is to deliver `.docx` and say why, since python-docx cannot author the legacy binary.

    **If conversion is forbidden**, the only real options are Word automation (COM on Windows, AppleScript/JXA on macOS) or a legacy binary library — not python-docx.
- id: docx-legacy-02
  answer: |
    Short answer: `textutil` is not a format wrapper — it re-authors the document through macOS's Cocoa text engine (NSAttributedString), then writes a fresh DOCX from that model. Anything the model can't represent is silently dropped or approximated, exit status stays 0, and you get no fidelity warning. For a table-editing task that means you may be editing a structurally different table than the one in the source.

    ## What typically goes wrong for tables

    - **Structure rebuilt, not copied.** Tables may be flattened to paragraphs with tabs, or kept as `w:tbl` but with a simplified grid.
    - **Merges lost or changed.** `w:gridSpan` (column merges) and `w:vMerge` (row merges) may collapse, so row/cell counts differ from the original. Header rows, repeated header rows (`tblHeader`), and nested tables are common casualties.
    - **Widths and layout re-derived.** `tblW`, `tblInd`, `tblLayout`, `gridCol`/`tcW` values are recomputed or defaulted; fixed layouts become autofit-ish. Any "Total" row/column alignment or wrapped-cell geometry can shift.
    - **Formatting stripped.** Cell shading (`w:shd`), borders (`tblBorders`/`tcBorders`), cell margins, vertical alignment, fonts/styles often reduced to defaults or a generic table style.
    - **Fields become text or vanish.** Formulas like `=SUM(ABOVE)`, cross-references, page numbers, and TOC fields lose `w:fldChar`/`w:instrText`; cached results may survive as static text, which is exactly the case where you must recompute totals yourself.
    - **Non-body content dropped.** Headers/footers (`word/header*.xml`, `word/footer*.xml`), footnotes, comments, tracked changes, floating shapes/text boxes, and some images (`word/media/*`) may be missing.
    - **Deliverable mismatch.** The converted DOCX is not the client's original artifact; even a perfect table edit won't save back to the original `.doc` losslessly. If the task is "edit this .doc's table," the fidelity contract is already broken.

    ## How to detect it before editing

    Do a structural audit of both renderings, not a text glance. The same engine can emit HTML, which is a useful second view of the source:

    ```bash
    textutil -info original.doc
    textutil -convert html -stdout original.doc > original.html   # source structure view
    textutil -convert docx  original.doc -output converted.docx    # check exit + stderr
    ```

    Then compare counts:

    ```bash
    # Source side, from the HTML view
    grep -o '<table' original.html | wc -l
    grep -o '<tr'    original.html | wc -l
    grep -o '<td'    original.html | wc -l
    grep -o 'colspan' original.html | wc -l
    grep -o 'rowspan' original.html | wc -l

    # Target side, from the DOCX package
    unzip -p converted.docx word/document.xml > document.xml
    for tag in w:tbl w:tr w:tc w:gridSpan w:vMerge w:gridCol w:tcW w:shd w:instrText w:fldSimple; do
      printf '%s ' "$tag"; grep -o "<$tag[ />]" document.xml | wc -l
    done

    unzip -l converted.docx | grep -E 'word/(header|footer|media|footnotes)' 
    ```

    Red flags for a table task:

    - Table/row/cell counts don't match the source, or `gridSpan`/`vMerge` counts are zero when the source HTML had `colspan`/`rowspan`.
    - A table region in the source HTML shows up as plain paragraphs (no `w:tbl`) or as a single-column table.
    - `w:tcW` absent or all equal to the even split; no `w:tblW`/`w:tblLayout`; no `w:shd`/`tblBorders` where the source had them.
    - No `w:instrText`/`w:fldSimple` although the source had formulas/page numbers.
    - No `word/header*.xml` or `word/media/*` although the source clearly had them.
    - Visible text diff: `textutil -convert txt -stdout original.doc` vs the text extracted from the DOCX — missing rows/cells show up here.

    Also sanity-check with python-docx: `len(doc.tables)`, `len(t.rows)`, `len(t.columns)`, `t.style`, `cell.width`/`tcW`, and whether expected merged cells are actually merged. If those numbers don't reconcile with the source, treat the conversion as lossy.

    ## What to do when lossy (practical stance)

    - Prefer `soffice --headless --convert-to docx` (LibreOffice) or Word itself; both preserve the table model far better. If neither exists, ask for a native `.docx` or the original table spec.
    - If you must use `textutil`, treat the output as a **content reference only**: rebuild the target table from scratch (style, grid widths on both `gridCol` and every cell's `tcW`, header shading/runs) and recompute any totals/fields yourself rather than editing the converted structure in place.
    - Never infer "conversion succeeded" from exit 0; the audit counts are the evidence.
- id: docx-legacy-03
  answer: |
    ## Why it happens

    `soffice` is single-instance per user profile. On startup it checks the profile directory (`~/.config/libreoffice/4`, or on macOS `~/Library/Application Support/LibreOffice/4`) for a running instance. If your desktop LibreOffice already owns that profile, the new process does **not** bootstrap a second instance: it forwards its command line over the profile's IPC socket to the existing process and exits. That is why you get an immediate exit (usually code 0) and no file.

    `--headless` is only a **startup hint for a fresh process**. It cannot make an already-running GUI instance headless. The GUI instance may:

    - ignore `--convert-to` entirely,
    - convert asynchronously after your script has already moved on,
    - or block on an open dialog / modal prompt, so the conversion never completes.

    So the exit status is meaningless here: 0 means "handed off", not "converted".

    ## How to run it reliably

    Force a **private user profile** for the conversion so it is a genuinely separate instance that cannot talk to the GUI one:

    ```sh
    #!/bin/sh
    set -eu

    in="$1"
    outdir="$2"
    mkdir -p "$outdir"

    profile="$(mktemp -d "${TMPDIR:-/tmp}/lo-profile.XXXXXX")"
    trap 'rm -rf "$profile"' EXIT INT TERM

    soffice --headless --norestore --nofirststartwizard \
      -env:UserInstallation="file://$profile" \
      --convert-to 'docx:MS Word 2007 XML' \
      --outdir "$outdir" \
      "$in"

    out="$outdir/$(basename "${in%.*}").docx"
    [ -s "$out" ] || { echo "conversion failed: $out missing/empty" >&2; exit 1; }
    ```

    Important details:

    - **`-env:UserInstallation` must be an absolute `file://` URI** and must be unique per concurrent job. A shared temp profile reintroduces the same single-instance collision, so do not use one fixed path across parallel conversions.
    - **Serialize if needed.** Wrap the call in `flock` (or a job queue) if several conversions could run at once; even separate profiles can be slow, and parallel cold starts are memory-hungry.
    - **Do not trust the exit code.** `soffice` frequently returns 0 on failure (bad filter, unwritable outdir, locked source). Verify the output file exists and is non-zero, and capture stderr.
    - **`--outdir` must already exist** and be writable; soffice will not create it. If omitted, output goes to the current working directory, which is a common "where did my file go" cause.
    - **A stale lock file next to the source** (`.~lock.file.doc#`) left by the GUI instance can also make the conversion fail. Close the document or remove the lock.
    - On macOS, invoke `/Applications/LibreOffice.app/Contents/MacOS/soffice` (or the `soffice` on `PATH` if the symlink exists); the profile argument syntax is identical.

    ## Better option for heavy scripting

    Start one persistent headless listener on a dedicated profile and submit jobs over UNO — e.g. `unoserver` (or `unoconv` in listener mode), or `soffice --headless --accept='socket,host=127.0.0.1,port=2002;urp;'` plus a Python UNO client. This avoids a cold LibreOffice start per file and gives you a real success/failure response instead of a fire-and-forget handoff.

    Bottom line: the GUI instance owns the default profile, so your command was delegated to it and returned immediately; give each conversion its own `-env:UserInstallation` profile and verify the output file yourself.
- id: docx-verify-01
  answer: |
    ## Safe save-and-verify after scripting `invoice.docx`

    The core idea: **never let the edit and the original share a file, and never trust the in-memory object — verify the bytes you actually wrote, with a reader that isn't python-docx.**

    ### 1. Save somewhere new, atomically

    ```python
    import os, tempfile

    out = "invoice.edited.docx"
    tmp = out + ".tmp"

    doc.save(tmp)                     # write fully to a sibling temp path
    with open(tmp, "rb") as f:        # force it to disk before the rename
        os.fsync(f.fileno())
    os.replace(tmp, out)              # atomic on the same filesystem
    ```

    - Don't `doc.save("invoice.docx")` in place. If the script dies or an assertion later fails, the original is gone and there's nothing to diff against or restore.
    - Don't save into a directory where the file is open in Word/Pages — a lock (`~$invoice.docx`) or a "file in use" can give you a half-written or shadow copy. Close the document first.
    - Cross-filesystem temp dirs break `os.replace`; keep the temp beside the output.

    ### 2. Re-open the saved file, not the object

    ```python
    from docx import Document
    check = Document(out)
    ```

    python-docx can hold a tree that looks right while serialization drops, duplicates, or reorders something. Only the reloaded document counts as evidence.

    ### 3. Structural validation of the container

    ```python
    import zipfile
    from lxml import etree

    with zipfile.ZipFile(out) as z:
        assert z.testzip() is None                      # CRC check every member
        names = set(z.namelist())
        for req in ("[Content_Types].xml", "_rels/.rels", "word/document.xml"):
            assert req in names, req
        etree.fromstring(z.read("word/document.xml"))    # well-formed XML
    ```

    Then an independent render, because python-docx accepts files Word rejects:

    ```bash
    soffice --headless --convert-to pdf --outdir /tmp invoice.edited.docx
    ```

    A non-zero exit / no PDF is a strong signal the package is malformed (bad `[Content_Types].xml` entry, dangling rel, invalid `tcW`). Also `xmllint --noout` on `word/document.xml` if you want a cheap second parser.

    ### 4. Assert the edit itself, on the reloaded doc

    Match by content, not by index, and make each assertion explicit:

    - **Deleted row:** target text is absent from all tables; row count is `old - expected_deletions`; the row that followed the deleted one is now directly after its predecessor.
    - **Recalculated total:** parse the remaining line items and the total with `Decimal` (not `float`), compare exactly, and compare against the *text* read back from the total cell — not the value you computed in memory. A removed row changes no Total by itself; if you didn't rewrite it, this assertion is what catches it.
    - **Widths:** for every table cell, `tcW` exists; per grid column, `gridCol/@w:w` equals the sum of `tcW` over that column's cells (dividing across `gridSpan`); `table.autofit is False`; total ≤ `page_width - left_margin - right_margin`.
    - **Fields vs text:** if a total is a Word field, check `w:instrText` is still intact *and* the cached `w:t` result equals your recomputation. Stale cached results and conflicting field formulas are the classic silent invoice bug.

    ### 5. Verify images by content, not by path

    If a picture was replaced:

    ```python
    import hashlib
    old_h = hashlib.sha256(old_bytes).hexdigest()
    with zipfile.ZipFile(out) as z:
        hashes = {n: hashlib.sha256(z.read(n)).hexdigest()
                  for n in z.namelist() if n.startswith("word/media/")}
    assert old_h not in hashes.values(), "old image still packaged"
    assert new_h in hashes.values(), "new image missing"
    ```

    Plus: every `a:blip/@r:embed` resolves in its owning part's rels, every image rel points at a member that exists, and `[Content_Types].xml` declares a `Default` for the new extension (`.png`, `.jpeg`). A new image part with an undeclared extension opens fine in python-docx and fails in Word. Remember image parts are shared across the body and headers — hash *all* of `word/media/`, and confirm no blip elsewhere still pointed at the old rId before you dropped the relationship.

    ### 6. Diff the whole document text, not just the changed region

    ```python
    def all_text(d):
        chunks = [p.text for p in d.paragraphs]
        for t in d.tables:
            for r in t.rows:
                for c in r.cells:
                    chunks.append(c.text)
        return "\n".join(chunks)
    ```

    Compare against the original's text and confirm the *only* differences are the intended ones. This catches collateral damage: a row removed from the wrong table, a `None` written into a cell, a header run overwritten, duplicate content from a badly copied `w:tr`.

    ### 7. Promote only after it passes

    Keep `invoice.docx` untouched until every check above is green, then swap it in and retain a timestamped backup:

    ```bash
    cp invoice.docx invoice.docx.bak-$(date +%Y%m%dT%H%M%S)
    os.replace("invoice.edited.docx", "invoice.docx")
    ```

    ### 8. Make it idempotent

    Guard the delete by matching the row's content, not by row number, and re-run the script against a fresh copy of the original. A second run must not delete a second row or double-append. Byte-identical output on repeat runs is the cleanest proof.

    **Hard rules that matter most:** separate output path + atomic rename; verify from a freshly opened copy; assert the recalculated total from the reloaded text, not from memory; hash the media entries rather than trusting filenames; and render once with a non-python-docx tool before you call it done.

    Uncertainty note: the python-docx API details (`_tr`, `_blob`, `get_or_add_image`, `drop_rel`) and the `tcW` vs `gridCol` behaviour are from my knowledge of the library, not from inspecting your specific `invoice.docx`. If the invoice's totals are Word fields, verify how your build of Word refreshes them on open — that's the one behaviour I can't assert for your deployment.
- id: docx-verify-02
  answer: |
    python-docx is a lenient lxml parser: it will happily open a part that Word's schema validator rejects. "Unreadable content" almost always means one of the structural invariants below was broken by string/lxml surgery.

    ## Element order and required children (`w:tbl` / `w:tr` / `w:tc`)

    - `w:tbl` children must be exactly: optional `w:tblPr` **first**, then `w:tblGrid`, then `w:tr`s. Appending a `w:tr` before `w:tblGrid`, or ending up with `w:tblPr` after a row, corrupts the table.
    - Missing `w:tblGrid` entirely (a table built by string concatenation), or two `w:tblGrid`/two `w:tblPr` after a botched insert.
    - `w:trPr` must be the first child of `w:tr`; `w:tcPr` must be the first child of `w:tc`. Inserting a cell or property element in the wrong slot is a schema violation.
    - Property children are order-sensitive (`w:tblPr`: `tblStyle, tblpPr, tblW, jc, tblInd, tblBorders, shd, tblLayout, tblCellMar, tblLook…`; `w:tcPr`: `tcW, gridSpan, vMerge, tcBorders, shd, …, vAlign`). Reordering or interleaving them fails validation even though the XML is well-formed.
    - Wrong container: `w:tc` appended directly to `w:tbl`, a `w:p` made a direct child of `w:tbl`, a `w:tbl` placed inside `w:p`, or `w:sectPr` moved into a row/cell.

    ## Grid consistency

    - `w:tblGrid` column count ≠ what the rows actually occupy. For every row: `gridBefore + Σ gridSpan + gridAfter` must equal the number of `w:gridCol`.
    - Deleting a *column* only by removing a `w:gridCol` (or only the cells) leaves every remaining cell's `w:tcW` and every `gridSpan` referring to a grid that no longer exists.
    - `gridSpan="0"`, a span that overflows the grid, or a nonzero `w:gridBefore`/`gridAfter` left behind after cells were removed.
    - Malformed widths: `w:tcW`/`w:gridCol` `w:w` must be an integer in twips. Writing `"50%"`, a float, a negative, or a unit-suffixed string, or omitting `w:type`, breaks the type.
    - Missing required attributes (`w:gridSpan` without `w:val`, `w:tcW` without `w:w`/`w:type`).

    ## Cell content model

    - An empty `w:tc` with no `w:p` at all. Every cell needs at least one block-level child; a run of text placed directly in `w:tc` is illegal.
    - A nested table is left as the **last** element of a cell with no trailing `w:p` — Word requires a paragraph after a nested table.
    - Removing the final paragraph of a cell while deleting a row/cell, leaving the cell "empty" in the schema sense.
    - Two tables left directly adjacent with no `w:p` between them: Word silently merges them (structure change, and a frequent source of "repaired" documents).

    ## Dangling cross-references

    - Vertical merge: a `w:vMerge w:val="continue"` whose restart cell was deleted in the row above; or a restart cell removed while continues remain. Also `vMerge` on a cell whose column no longer lines up with the restart.
    - Bookmarks, comments and tracked changes split by the edit: `w:bookmarkStart` without its `w:bookmarkEnd` (or duplicate `w:id`), `w:commentRangeStart` orphaned when its row was removed, unbalanced `w:ins`/`w:del`/`w:moveFrom` spanning rows, or `w:trPr`/`w:tcPr` revision elements left pointing at deleted content.
    - Relationship IDs: a `r:id`/`r:embed` still referenced by a run or blip after the corresponding `.rels` entry was dropped (`drop_rel` counts only `r:id`, so it can kill a rel still used by an `a:blip`).
    - `w:tblStyle`/`w:pStyle` pointing at a style ID no longer defined is usually tolerated, but a `w:tblLook` or conditional-formatting reference to a missing style is a common repair trigger.

    ## XML / namespace / encoding mistakes

    - Fragments parsed or spliced without declarations: `parse_xml` without `nsdecls('w','r','a',…)` produces `ns0:` prefixes, and — worse — namespaced *attributes* where WordML expects unqualified ones; Word rejects those.
    - Concatenating raw XML with an undeclared prefix (`w:val` with no `xmlns:w` in scope).
    - `mc:Ignorable="w14 wp14"` listing prefixes that are no longer declared on the root (very common after hand-editing the root or copying fragments between parts).
    - Unescaped `&`/`<` in inserted text, or XML-1.0-invalid control characters (0x0B, 0x01–0x08, etc.) copied from source text.
    - Mismatched/unclosed tags from regex or substring surgery — well-formedness errors are the most direct cause.

    ## Package-level (easy to confuse with table errors)

    - Writing a part without its `[Content_Types].xml` override, duplicate zip entry names, a malformed or missing `.rels` part, or re-zipping with the wrong paths/`word/document.xml` duplicated.

    **Practical note:** re-opening in python-docx proves only well-formedness of the parts it touches, not schema validity. Cheap checks that catch most of the above: unzip and run `xmllint --noout` on every part, assert `gridBefore + Σ gridSpan + gridAfter == len(tblGrid/gridCol)` per row, assert every `w:tc` contains ≥1 `w:p` (and that a nested `w:tbl` is followed by one), assert every `bookmarkStart` has a matching `bookmarkEnd`, and confirm every `r:id`/`r:embed` still resolves in the owning part's rels.
- id: docx-verify-03
  answer: |
    **Short answer:** deleting the `w:drawing` only deletes the *reference*. The image part, its relationship entry, and the `word/media/*` binary stay in the package, because python-docx saves every part reachable through the rels graph — it never garbage-collects orphaned media. So you must (a) actually drop the relationship, then (b) prove it against the saved zip.

    ## Why the drawing removal is not enough

    A picture is three things:

    1. the `w:drawing` in the story part (body/header/footer) containing `a:blip/@r:embed="rIdN"`;
    2. a relationship `rIdN -> media/imageK.png` in that part's `.rels`;
    3. the bytes in `word/media/imageK.png`.

    Removing the drawing kills only #1. As long as #2 exists, `doc.save()` writes #3 into the zip. And #3 may be shared: python-docx dedupes identical image bytes by SHA1, so the same `imageK.png` part can be referenced by the body *and* a header/footer. That is also why you must not "replace" it by overwriting `_blob`.

    ## Removal, done correctly

    On the part that owned the blip (`doc.part` for the body, `section.header.part` / `footer.part` for header logos):

    ```python
    new_rId, _ = part.get_or_add_image("new_logo.png")
    blip.set(qn("r:embed"), new_rId)   # or remove the whole w:drawing and add a new picture

    # only drop the old rel if no blip in THIS part still points at it
    old_rId = ...
    xml = part.element  # or part._element
    still_used = any(b.get(qn("r:embed")) == old_rId
                     for b in xml.iter(qn("a:blip")))
    if not still_used:
        part.drop_rel(old_rId)
    ```

    Caveat: `drop_rel()` only counts `r:id` attributes, never `r:embed`. If another blip still uses that rId, calling it blindly breaks that picture — hence the explicit blip count above. Also check `mc:AlternateContent` fallback branches and `w:del`/tracked-change content for a second copy of the drawing.

    ## Verification against the saved file (the part that counts)

    Do not trust the in-memory object. Re-open the saved `.docx` as a zip:

    ```python
    import zipfile, hashlib
    old_hash = hashlib.sha256(open("confidential_logo.png","rb").read()).hexdigest()

    with zipfile.ZipFile("out.docx") as z:
        for n in z.namelist():
            if n.startswith("word/media/"):
                h = hashlib.sha256(z.read(n)).hexdigest()
                print(n, "LEAK" if h == old_hash else "ok")
            # rels + story XML: no leftover target/rId
            if n.endswith(".rels") or n.endswith(".xml"):
                s = z.read(n)
                if b"imageK.png" in s or old_rId.encode() in s:
                    print("reference still present in", n)
    ```

    Checks to run:

    - **Every** `word/media/*` entry, not just the one you think you edited — body, `word/media` reached from `header*.xml.rels`/`footer*.xml.rels`, footnotes, comments, `word/embeddings/`, `customXml/`, `docProps/thumbnail.jpeg` (a thumbnail can contain the logo screenshot).
    - **All `.rels` files** (including `word/_rels/document.xml.rels`, `header1.xml.rels`, `_rels/.rels`) for the old target filename or old rId.
    - **All story XML** for the old rId string. A clean file has zero hits.
    - **`[Content_Types].xml`**: stale `<Default Extension="png">` is harmless; a stale `<Override PartName="/word/media/imageK.png">` is a leftover worth noting, though Word tolerates it.

    ## Two honest caveats

    - **Hash matching is only valid if the bytes are byte-identical.** If the old image was ever re-encoded (resized, JPEG recompressed, EXIF stripped), the SHA256 won't match even though the same picture is there. In that case open each media entry and compare visually, or compare dimensions + perceptual hash, rather than relying on the digest. A hash match proves a leak; no hash match does not by itself prove no leak.
    - **"No reference" is the real test, not "no file".** If the image part genuinely backs no remaining rel, python-docx will not serialize it on the next save. The robust sequence is: remove drawing → drop rel when the blip count is zero → save → verify the saved zip has neither the old rel/target nor the old bytes. If an orphaned media part still survives (e.g. you edited in place without re-saving through the rels graph), rebuild the package cleanly rather than patching the zip.

    **So:** verify by unzipping the *saved* `.docx` and proving (1) no `.rels` or story XML references the old rId or target, and (2) no `word/media/*` entry — body, header, footer, thumbnail included — still hashes to the old logo (with the byte-identity caveat above).
```
