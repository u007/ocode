# deepseek-v4.1-flash — docx answers (re-benchmark after provider update)

Model: `opencode-go/deepseek-v4.1-flash` (provider-stripped `deepseek-v4.1-flash`),
re-run 2026-10-21 after opencode-go updated the model behind this id
(`model_version: "4.1-2026-10-21"`). This REPLACES the earlier baseline for this id;
the previous answers are in git history.

Closed-book barrier:
- Each of the 29 questions in `docs/okf/_prompts/docx.md` was a fresh headless
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
- id: docx-model-01
  answer: |
    ## What a .docx physically is

    A `.docx` is **not** a single binary blob in the old `.doc` sense. It is a **ZIP archive** (starts with the `PK\x03\x04` signature) that implements Microsoft's **OPC** packaging standard — *Open Packaging Conventions*, ECMA‑376 / ISO‑29500 Part 2. Unzip it and you get a small filesystem of **parts** (mostly XML, plus binary media) glued together by **relationship** files.

    A typical minimal Word document looks like this:

    ```
    [Content_Types].xml          # maps every extension/part to a MIME type
    _rels/.rels                  # package-level rels: points to the main document + core props
    word/document.xml            # THE main document part — all body content lives here
    word/_rels/document.xml.rels # rels from document.xml to styles, numbering, images…
    word/styles.xml
    word/settings.xml
    word/numbering.xml
    word/fontTable.xml
    word/theme/theme1.xml
    word/media/image1.png        # images are separate binary parts
    docProps/core.xml            # title/author/dates (Dublin Core)
    docProps/app.xml             # app name, word count, page count
    ```

    Rules worth knowing:

    - **`[Content_Types].xml`** is required and is not itself referenced by any relationship. It declares `<Default Extension="xml" .../>` and `<Override PartName="/word/document.xml" .../>`.
    - Relationships are **directional** and typed by URI. `_rels/.rels` uses the `officeDocument` relationship type to say "the main part is `word/document.xml`". Nothing hardcodes that path — a consumer must resolve it through the rels, which is why the part can legally be renamed.
    - A part is just bytes; a **content type** plus a **relationship** is what gives it meaning.
    - `.docm` adds `word/vbaProject.bin`; `.dotx`/`.docm` differ only by content types and the main part's relationship type. That's the whole difference.
    - `word/document.xml` is the *body*; headers/footers/footnotes/endnotes are **separate parts** (`header1.xml`, `footnotes.xml`, …) pulled in by rels from `document.xml`. Comments live in `comments.xml`.

    ## Where a table's data lives

    In **`word/document.xml`**, inline in the document body. A table is *not* a separate part and has no side storage — it is a `w:tbl` element sitting in the body flow, interleaved with paragraphs. All of its text, structure, and most formatting are in that one XML tree.

    (The one exception is **styles**: named table styles referenced by `w:tblStyle` are defined in `word/styles.xml`. The table's *data* is never there.)

    ## Table XML hierarchy, top to bottom

    Namespace: `http://schemas.openxmlformats.org/wordprocessingml/2006/main`, conventionally prefix `w`.

    ```
    w:document
    └─ w:body
       └─ w:tbl                        ← the table
          ├─ w:tblPr                   ← table properties (style, width, borders, alignment, layout)
          │  ├─ w:tblStyle             ← reference to a style in styles.xml
          │  ├─ w:tblW                 ← preferred width (w:w, w:type: dxa | pct | auto)
          │  ├─ w:tblBorders
          │  ├─ w:tblLayout            ← fixed vs autofit
          │  └─ w:tblLook
          ├─ w:tblGrid                 ← the column grid: authoritative column definitions
          │  └─ w:gridCol  (one per column, w:w in twentieths of a point)
          └─ w:tr                      ← table row (repeats)
             ├─ w:trPr                 ← row properties: w:trHeight, w:tblHeader, w:cantSplit
             └─ w:tc                   ← table cell (repeats)
                ├─ w:tcPr              ← cell properties: w:tcW, w:gridSpan, w:vMerge, w:shd, w:tcBorders, w:vAlign
                └─ w:p                 ← paragraph — a cell MUST contain at least one
                   ├─ w:pPr            ← paragraph properties (alignment w:jc, spacing, style)
                   └─ w:r              ← run (a contiguous span of same-formatted text)
                      ├─ w:rPr         ← run properties (w:b, w:i, w:sz, w:color, w:rFonts)
                      └─ w:t          ← THE TEXT  (add xml:space="preserve" when it has leading/trailing spaces)
    ```

    So the shortest path from table to characters is:

    **`w:tbl` → `w:tr` → `w:tc` → `w:p` → `w:r` → `w:t`**

    and the "data" — the actual string a human reads — is the character data of **`w:t`**.

    ### Details that trip people up

    - **`w:tblGrid` is the real column model.** `w:gridSpan` on a cell means "this cell covers N grid columns", and `w:vMerge` (`restart` / `continue`) implements vertical merges. Merged cells are *not* removed from the grid — the grid stays rectangular and cells span it. This is why naive parsers misalign merged tables.
    - **A cell cannot be empty of paragraphs.** A visually blank cell is `<w:tc><w:tcPr/><w:p/></w:tc>`. There is no "no paragraph" state.
    - **Text is not always in `w:t`.** Also watch for:
      - `w:tab` (tab character), `w:br` / `w:cr` (line break) — these carry no text content.
      - `w:delText` — text inside a tracked deletion (`w:del`).
      - `w:instrText` — field instruction codes; the *displayed* result is in a nested `w:t` inside the field result run.
      - `w:sym` — symbol glyph by code point + font, not literal text.
    - **Nesting**: a `w:tbl` may appear inside a `w:tc`, so "table → cell → table" recursion is legal and must be handled.
    - **Whitespace is fragile.** Without `xml:space="preserve"`, XML whitespace collapsing can eat leading/trailing spaces in `w:t`. Most producers also split a sentence across multiple runs for no visible reason (spell-check boundaries, revision marks, formatting changes), so text extraction must *concatenate runs within a paragraph* rather than assume one run per sentence.
    - **Row/column order is document order** — there is no index attribute. Position is implied by sequence.

    ### Minimal example

    ```xml
    <w:tbl>
      <w:tblPr>
        <w:tblStyle w:val="TableGrid"/>
        <w:tblW w:w="0" w:type="auto"/>
      </w:tblPr>
      <w:tblGrid>
        <w:gridCol w:w="4675"/>
        <w:gridCol w:w="4675"/>
      </w:tblGrid>
      <w:tr>
        <w:tc>
          <w:tcPr><w:tcW w:w="4675" w:type="dxa"/></w:tcPr>
          <w:p><w:r><w:t>Region</w:t></w:r></w:p>
        </w:tc>
        <w:tc>
          <w:tcPr><w:tcW w:w="4675" w:type="dxa"/></w:tcPr>
          <w:p><w:r><w:t xml:space="preserve">Q3 Revenue </w:t></w:r></w:p>
        </w:tc>
      </w:tr>
      <w:tr>
        <w:tc><w:p><w:r><w:t>EMEA</w:t></w:r></w:p></w:tc>
        <w:tc><w:p><w:r><w:t>1,240</w:t></w:r></w:p></w:tc>
      </w:tr>
    </w:tbl>
    ```

    **In one sentence:** a `.docx` is an OPC ZIP package of XML parts joined by typed relationships, and a table is a `w:tbl` subtree living directly in `word/document.xml`, with the readable data sitting in `w:t` leaves reached via `w:tbl → w:tr → w:tc → w:p → w:r → w:t`.
- id: docx-model-02
  answer: |
    Assumption: the context is OOXML `.docx` and a run-level API such as `python-docx` (`run.text` maps to the text of a single `<w:r>`).

    ## Short answer

    `run.text == "Widget C"` fails because **a run is not a word or a phrase**. It is the smallest formatting-homogeneous chunk. Word freely splits the visible string `Widget C` across two or more runs, for example:

    ```xml
    <w:r><w:t>Widget</w:t></w:r>
    <w:proofErr w:type="spellStart"/>
    <w:r><w:t xml:space="preserve"> C</w:t></w:r>
    ```

    Rendering concatenates all runs, so the cell "clearly shows" `Widget C`, but no individual run contains that exact string. A run may also contain more than the target (`"Widget C and more"`), so exact equality is wrong even without splitting.

    ## Why Word splits runs

    Common causes:

    - formatting changes mid-string (bold, color, font, language)
    - spelling/grammar markers (`w:proofErr`)
    - revision save IDs (`w:rsidR`) and tracked changes (`w:ins`)
    - hyperlinks (runs live under `w:hyperlink`, not directly under `w:p`)
    - content controls (`w:sdt`), smart tags, bookmarks
    - fields, tabs, line breaks, non-breaking spaces (`\u00a0`), soft hyphens
    - Word simply deciding to split while editing

    Also, text may not be where you are looking:

    - `cell.paragraphs` only covers the cell's own paragraphs; **nested tables** are in `cell.tables`
    - text boxes (`w:txbxContent`) and content controls may hold the paragraph
    - runs inside hyperlinks are often missed by `paragraph.runs`
    - deleted text is `w:delText`, not `w:t`, so `run.text` will not see it

    Diagnostic: if `cell.text` also does not contain `"Widget C"` but Word shows it, the text is probably in a hyperlink, nested table, text box, content control, or field. If `cell.text` does contain it, the string is split across runs.

    ## Reliable approach

    Search at the **paragraph text-node level**, not the run level:

    1. Collect every `<w:t>` in the paragraph, in document order.
    2. Concatenate their text into one string.
    3. Find the match in that concatenated string.
    4. Map the match offsets back onto the `<w:t>` nodes.
    5. Rewrite the nodes: keep the prefix, insert the replacement in the node where the match starts, clear the matched portions of the middle nodes, keep the suffix.
    6. Preserve `xml:space="preserve"` when leading/trailing spaces remain.

    Editing `<w:t>` directly keeps the surrounding run formatting and also works for runs inside hyperlinks/content controls.

    ```python
    from docx.oxml.ns import qn

    W_T = qn("w:t")

    def replace_in_paragraph(p, old, new):
        """p is a <w:p> lxml element. Returns replacement count."""
        nodes = list(p.iter(W_T))
        texts = [t.text or "" for t in nodes]
        full = "".join(texts)

        if not old or old not in full:
            return 0

        # start offset of each w:t in the concatenated string
        starts, pos = [], 0
        for s in texts:
            starts.append(pos)
            pos += len(s)

        # non-overlapping match spans in the original concatenated text
        matches = []
        i = full.find(old)
        while i != -1:
            matches.append((i, i + len(old)))
            i = full.find(old, i + len(old))

        new_texts = []
        for s, txt in zip(starts, texts):
            e = s + len(txt)
            out, cur = [], s

            for m0, m1 in matches:
                if m1 <= s or m0 >= e:      # no overlap with this node
                    continue
                if m0 > cur:
                    out.append(full[cur:min(m0, e)])
                if s <= m0 < e:             # match starts in this node
                    out.append(new)
                cur = max(cur, min(m1, e))
                if cur >= e:
                    break

            if cur < e:
                out.append(full[cur:e])
            new_texts.append("".join(out))

        for t, txt in zip(nodes, new_texts):
            t.text = txt
            if txt != txt.strip():          # preserve significant spaces
                t.set(qn("xml:space"), "preserve")

        return len(matches)
    ```

    Usage for a cell, including nested tables and text boxes:

    ```python
    def replace_in_cell(cell, old, new):
        count = 0
        # _tc.iter(w:p) also reaches paragraphs in nested tables/textboxes
        for p in cell._tc.iter(qn("w:p")):
            count += replace_in_paragraph(p, old, new)
        return count
    ```

    For a whole document, apply the same function to every `<w:p>` in the body, and also in headers, footers, footnotes, endnotes, and comments—those are separate package parts.

    ## Practical caveats

    - **Replacement formatting** comes from the run containing the match start. If a match spans differently formatted runs, you must decide which formatting wins; the code above uses the start run, which is the usual convention.
    - **Tabs/breaks**: `w:tab` and `w:br` are not `w:t` text. If the target can span a tab, model them as `\t`/`\n` explicitly; otherwise the offsets will not line up and the tab element can remain behind.
    - **Whitespace**: normalize `\u00a0` to a plain space for matching, or search both variants. Do not silently run `.strip()` and then reuse the original offsets.
    - **Fields**: replacing the displayed field result may be undone when the field updates; edit the field source instead.
    - **Tracked deletions**: if the text is in `w:delText`, search those elements too, and decide whether editing deleted text is what you want.

    ## If you do not need formatting preservation

    A blunt but reliable shortcut for a uniformly formatted cell is to concatenate `paragraph.runs`, replace in that string, put the whole result into the first run, and delete the remaining runs. This is easy but destroys per-run formatting, so use it only when the cell is known to be uniform.

    For production-grade replacement across arbitrary documents, a library with a real cross-run find/replace—docx4j's `Range.replace`, the Open XML SDK at the text-element level, Aspose.Words, or Word automation—is less error-prone than hand-rolling OOXML offsets.
- id: docx-model-03
  answer: |
    ## WordprocessingML units

    Short version: **table and cell widths are in twips (`dxa`), image sizes are in EMUs.** Both are integers in the XML, and both are defined so you never have to store a fractional value.

    ---

    ### 1. Table / cell widths — twips (`dxa`)

    Attributes on `w:tblW`, `w:tcW`, `w:tblGrid/w:gridCol/@w:w`, `w:tblInd`, `w:tblCellSpacing`, and cell margins (`w:tblCellMar`, `w:tcMar`) are of type `ST_TwipsMeasure` — a plain integer meaning **twentieths of a point**.

    ```
    1 dxa (twip) = 1/20 pt
    1 pt  =  20 dxa
    1 in  =  72 pt = 1440 dxa
    1 cm  ≈ 566.93 dxa
    1 mm  ≈  56.69 dxa
    1 px @ 96 dpi = 15 dxa
    ```

    Conversions: `pt = dxa / 20`, `in = dxa / 1440`.

    Examples:
    - `2880` → 144 pt → 2 in
    - `9360` → 468 pt → 6.5 in (letter page with 1" margins)
    - `1440` → 72 pt → 1 in

    **Caveats on the same attributes:**
    - `w:type` selects the unit system: `dxa` (twips), `pct`, `auto`, `nil`. With `w:type="pct"`, the value is in **fiftieths of a percent**: `5000` = 100%, `2500` = 50%.
    - Newer ISO/IEC 29500 (`ST_MeasurementOrPercent`) also permits universal-measure strings such as `"2.5in"` or `"50%"`, but the twips integer form is what Word writes and what most consumers expect.
    - Don't confuse width units with **border widths**: `w:sz` on `w:tblBorders`/`w:tcBorders` is in **eighths of a point** (`8` = 1 pt), not twips.
    - Font size `w:sz` is in **half-points** (`24` = 12 pt) — another nearby trap.

    ---

    ### 2. Image sizes — EMUs (DrawingML)

    Inside `w:drawing`, the size lives in `wp:inline`/`wp:anchor` → **`wp:extent cx="…" cy="…"`**, and again in `pic:spPr/a:xfrm/a:ext`. Same for `a:off` and shape transforms. All of these are **EMUs** (English Metric Units), a 64-bit signed integer.

    ```
    1 in  = 914400 EMU
    1 pt  =  12700 EMU
    1 cm  = 360000 EMU
    1 mm  =  36000 EMU
    1 dxa =    635 EMU
    1 px @ 96 dpi = 9525 EMU
    ```

    Conversions: `pt = EMU / 12700`, `in = EMU / 914400`, `EMU = in × 914400`.

    Examples:
    - `914400` → 1 in; `1828800` → 2 in
    - `190500` → 15 pt → 0.2083 in
    - A 300×200 px image at 96 dpi: `300 × 9525 = 2857500`, `200 × 9525 = 1905000` → 3.125 in × 2.083 in

    EMU exists so DrawingML can express inches and centimetres exactly with integers (914400 is highly composite), which keeps layout arithmetic deterministic across Word, PowerPoint, and Excel.

    **Related EMU-based values:** crop rectangles `a:srcRect` are in **thousandths of a percent** (`100000` = 100%), while offsets/extents are EMU — easy to mix up.

    ---

    ### 3. Legacy path (VML)

    Older/embedded shapes under `w:pict` → `v:shape` use `style="width:72pt;height:36pt"`, i.e. **CSS-style lengths, canonically points** (with `in`, `cm`, `mm`, `px` also legal). If you're parsing a document produced by an older tool, check for `w:pict` alongside `w:drawing`, and don't apply the EMU divisor to those values.

    ---

    ### Quick reference

    | Quantity | Unit | To points | To inches |
    |---|---|---|---|
    | Table/cell width (`w:w`, `w:gridCol`) | twip (`dxa`) | ÷ 20 | ÷ 1440 |
    | Table width as percentage (`w:type="pct"`) | 1/50 % | — | value ÷ 5000 = fraction |
    | Image/shape extent (`wp:extent`, `a:ext`) | EMU | ÷ 12700 | ÷ 914400 |
    | Border width (`w:sz`) | 1/8 pt | ÷ 8 | ÷ 576 |
    | Font size (`w:sz`) | half-point | ÷ 2 | ÷ 144 |
    | VML style length | pt (CSS) | as written | ÷ 72 |
- id: docx-locate-01
  answer: |
    ## Short answer

    You cannot reliably find "the invoice table" by index (`doc.tables[3]`) or by position. Identify it by **content signature** (header labels + a sentinel value such as an invoice number or "Total"), then locate the row by **mapping the header row to column indices** and doing an **exact normalized match** on the item column. Require the match to be unique; raise rather than guess. Before writing, check merged-cell identity, tracked changes/content controls, whether the table is the one you think it is (body vs. header/footer/nested), and that your write will not destroy formatting, hyperlinks, or a total that depends on the cell.

    ## 1. Enumerating the right tables

    `document.tables` only returns **top-level body tables**. It misses:

    - tables in headers/footers (`section.header.tables`, `section.footer.tables`)
    - nested tables (a `w:tbl` inside another cell)
    - tables wrapped in content controls (`w:sdt`) or other body wrappers
    - tables in text boxes (python-docx does not expose these at all)

    For robustness, iterate the XML in document order and wrap each element:

    ```python
    from docx import Document
    from docx.table import Table
    from docx.oxml.ns import qn

    def iter_all_tables(doc):
        body = doc.element.body
        for tbl in body.iter(qn("w:tbl")):
            yield Table(tbl, doc)   # parent only supplies the part; doc is fine
    ```

    `iter()` is depth-first, so nested tables also appear. If you only need body tables and know the file is simple, `doc.tables` is fine — but say so explicitly in a comment, because it is the usual source of "why did my edit land in the wrong table".

    A Word table has no reliable "name" like an HTML `id`. Two possible anchors:

    - **Accessibility alt text**: Word stores Table Properties → Alt Text as `w:tblCaption` / `w:tblDescription` under `w:tblPr`. python-docx does not expose these, but you can read them:
      ```python
      cap = tbl.tblPr.find(qn("w:tblCaption"))
      title = cap.get(qn("w:val")) if cap is not None else None
      ```
      This is excellent when present, but not all documents have it, and support/round-tripping varies by producer.
    - **Style name** (`tbl.style.name`) is weak: many tables share "Table Grid".

    So use alt text if available, otherwise content.

    ## 2. Identify "the invoice table" by content, not order

    Normalize text first. Word inserts non-breaking spaces, tabs, line breaks inside cells, and case varies.

    ```python
    import re, unicodedata

    def norm(s: str) -> str:
        s = unicodedata.normalize("NFKC", s or "")
        s = s.replace("\xa0", " ").replace("\u200b", "")
        return re.sub(r"\s+", " ", s).strip().casefold()
    ```

    A practical signature:

    1. Find the header row: the first row whose cells contain recognizable column labels (`"item"`, `"description"`, `"qty"`/`"quantity"`, `"unit price"`, `"amount"`/`"total"`).
    2. Require the item column **and** at least one money/quantity column.
    3. Add a sentinel: a cell or nearby paragraph matching `invoice no`, `invoice #`, `invoice number`, or a `total` row. This separates the invoice table from a similar line-item table in an appendix.
    4. If zero or more than one candidate survives, **fail with a diagnostic** listing candidates (index, caption, header labels, first row). Do not silently take the first.

    ```python
    ITEM_LABELS = {"item", "item name", "product", "description", "sku"}
    QTY_LABELS  = {"qty", "quantity", "units"}
    AMT_LABELS  = {"amount", "line total", "total", "price", "unit price"}

    def header_map(row):
        m = {}
        for i, cell in enumerate(row.cells):
            k = norm(cell.text)
            if k and k not in m:      # merged header cells repeat; keep first
                m[k] = i
        return m

    def pick_col(hmap, labels):
        for k, i in hmap.items():
            if k in labels:
                return i
        return None
    ```

    Then the table finder:

    ```python
    def find_invoice_table(doc):
        hits = []
        for t in iter_all_tables(doc):
            if not t.rows:
                continue
            hmap = header_map(t.rows[0])
            item = pick_col(hmap, ITEM_LABELS)
            money = pick_col(hmap, AMT_LABELS)
            qty = pick_col(hmap, QTY_LABELS)
            if item is None or (money is None and qty is None):
                continue
            blob = norm(" | ".join(c.text for r in t.rows for c in r.cells))
            has_sentinel = ("invoice no" in blob or "invoice #" in blob
                            or "invoice number" in blob)
            if has_sentinel:
                hits.append((t, hmap, item))
        if len(hits) != 1:
            raise LookupError(f"expected 1 invoice table, found {len(hits)}")
        return hits[0]
    ```

    Caveats with the header approach:

    - The header row may be split across two rows, or may not be row 0 (title rows above it). Scan the first few rows for the highest header-label score rather than assuming row 0.
    - Header labels may be merged horizontally; `row.cells` then returns the same `_Cell`/`tc` repeatedly. Deduplicate by `id(cell._tc)` if you need logical columns.
    - Do not use substring matching for the header (`"item" in "itemized expenses"`) unless you control the document.

    ## 3. Find the "Gadget D" row

    Use the item column index derived from the header, and compare **normalized exact equality**, not `in`:

    ```python
    def find_item_row(table, item_col, target, start=0):
        want = norm(target)
        hits = []
        for ri, row in enumerate(table.rows[start:], start=start):
            cells = row.cells
            if item_col >= len(cells):
                continue
            if norm(cells[item_col].text) == want:
                hits.append(ri)
        if len(hits) != 1:
            raise LookupError(f"{target!r}: {len(hits)} matching rows")
        return hits[0]
    ```

    Why `==` matters: `"Gadget D" in "Gadget D2"` and `"Gadget Deluxe"` both match a substring search, and you will edit the wrong line. If the item names legitimately repeat (multiple shipments of the same SKU), you need a second key — invoice line number, SKU column, or date — and you should require the disambiguator rather than picking one.

    Also:

    - `cell.text` joins all paragraphs in the cell with `\n`. Normalizing whitespace is essential.
    - Empty or whitespace-only cells commonly occur; skip them.
    - `table.rows` iterates direct `w:tr` children. Rows wrapped in `w:sdt` (content-control rows) may be invisible. If the document was produced by a forms-heavy generator, fall back to `tbl.iter(qn("w:tr"))` and filter out rows belonging to nested tables.
    - Header detection must skip the header itself when searching data rows (`start = header_idx + 1`).

    ## 4. What to check before editing

    Ordered by how often they bite:

    1. **Uniqueness / identity.** Exactly one table and exactly one row matched. Log table index, caption, header labels, row text, and the target column label. If two tables match, stop.
    2. **Correct column.** Resolve column by header label, not by a hard-coded `cells[2]`. Confirm the column you are about to write is the intended one (e.g. writing a quantity into "Unit price" is a silent corruption).
    3. **Merged cells.** `table.cell(r, c)` returns a proxy over the underlying `w:tc`. With horizontal/vertical merges, several grid positions share one `tc`; writing to one writes to all, and `row.cells` may be longer or shorter than the visual row. Check `tc.tcPr.gridSpan` / `vMerge`, or compare `id(cell._tc)` across the row. Only edit the merge anchor, once.
    4. **Tracked changes.** If revision tracking is on, the visible text may be the combination of `w:ins` and `w:del` runs; python-docx's text extraction does not give you a faithful "what Word shows" view, and writing into a deleted/inserted run produces garbage. Inspect for `w:ins`, `w:del`, `w:delText` in the target row.
    5. **Content controls and protection.** Check for `w:sdt` around the cell/row (common in generated forms). Check `w:documentProtection` in `settings.xml` and whether the file is read-only; otherwise your save fails or Word rejects the edit.
    6. **Nested/duplicate tables.** An invoice line-item table repeated in the header, a summary table with the same headers, or a nested table inside a cell can all match your signature. Confirm the parent chain and section.
    7. **Formatting destruction.** `cell.text = "..."` (and `paragraph.text = ...`) discards runs, character styles, hyperlinks, fields, and extra paragraphs in that cell. To preserve formatting, edit the specific run or replace text while keeping the first run's `rPr`:
       ```python
       p = cell.paragraphs[0]
       if p.runs:
           p.runs[0].text = new_value
           for r in p.runs[1:]:
               r._r.getparent().remove(r._r)
       else:
           p.add_run(new_value)
       ```
       But first check whether the value is split across runs — replacing run 0's text and deleting the rest can mangle "Gadget" + " D" or a hyperlink.
    8. **Data type and locale.** `"1,234.50"`, `"€12"`, `"3 pcs"` are strings. If you write a raw Python value, decide explicitly whether it must be a plain number for downstream extraction, and keep formatting consistent with the column.
    9. **Dependent totals.** If you change quantity or price, the total row/paragraph is not a formula in a DOCX — it is stale text. Update it and re-verify the arithmetic, or flag it.
    10. **Save and verify.** Edit a **copy**, save to a new path, reopen with `Document(path)`, re-run the same lookup, and assert the new value and that no other cell changed. For anything important, diff `word/document.xml` before/after to prove the edit touched only the intended `w:tc`.

    ## Minimal end-to-end example

    ```python
    doc = Document("report.docx")
    table, hmap, item_col = find_invoice_table(doc)
    row_idx = find_item_row(table, item_col, "Gadget D", start=1)

    # confirm target cell is a real, unmerged, sole owner
    cell = table.cell(row_idx, hmap["qty"])
    if sum(1 for c in table.rows[row_idx].cells if c._tc is cell._tc) != 1:
        raise RuntimeError("target cell is merged; resolve the anchor first")

    print("editing:", [c.text for c in table.rows[row_idx].cells])

    # write preserving the first run
    p = cell.paragraphs[0]
    p.runs[0].text = "7" if p.runs else p.add_run("7").text

    doc.save("report.edited.docx")
    ```

    ## Assumptions I am making

    - A real `.docx` with genuine Word tables, not scanned images or PDF-converted text boxes.
    - English header labels; if the document is localized, the label sets must be localized too (this is the most common reason a "robust" finder returns zero matches).
    - Headers are in a consistent row near the top; if not, score candidate header rows instead of assuming row 0.
    - The `w:tblCaption` alt-text anchor works only when the producer wrote it; treat it as a bonus, not the primary key.

    If you cannot establish a unique table and row match plus a non-merged write target, the correct behavior is to abort with a report — not to fall back to `doc.tables[0]`, which is how invoice edits end up in the terms-and-conditions table.
- id: docx-locate-02
  answer: |
    Short answer: `row.cells` is indexed by **grid columns**, not by `<w:tc>` elements. A cell with `<w:gridSpan w:val="2"/>` shows up **twice** in that sequence — the same underlying cell (same `_tc` element, and in current python-docx literally the same `_Cell` proxy object) occupying both grid slots. So `row.cells[0] is row.cells[1]` (at minimum `==`, since `_Cell.__eq__` compares `_tc` identity).

    **Why**

    python-docx builds its cell grid in `_Cells._cells_build()` by walking `tr.tc_lst` and appending one entry per grid slot:

    ```python
    for tc in tr.tc_lst:
        for grid_span_idx in range(tc.grid_span):
            row_cells.append(row_cells[-1] if grid_span_idx else _Cell(tc, self._tbl))
    ```

    A horizontal merge (`cell_a.merge(cell_b)`) collapses the two `<w:tc>` elements into one with `gridSpan=2`; the second `<w:tc>` is gone from the XML. `row.cells` then "re-expands" it to keep grid coordinates consistent with `table.cell(r, c)` and `table.columns`.

    **Example**

    First row: one cell spanning columns 0–1, then a normal cell. XML has 2 `<w:tc>`, table grid has 3 columns, so:

    ```python
    row.cells          # (_Cell, _Cell, _Cell)  -> len == 3
    row._tr.tc_lst     # [<w:tc>, <w:tc>]        -> len == 2 (actual cells)
    ```

    **What goes wrong**

    1. **Deleting "the second cell" by index.** `row.cells[1]` is the merged cell itself, not a neighbor. Remove its `<w:tc>` (`cell._tc.getparent().remove(cell._tc)`) and you delete the *whole* spanning cell — including the content of the "first" logical cell you meant to keep. `row.cells[0]` is now a dangling proxy over a detached element, and the row's filled grid slots no longer match the table's `gridCol` width, so Word may render ragged or flag the file for repair.
    2. **Index shifts.** `row.cells` is rebuilt on each access, so after any removal, indexes silently refer to different cells.
    3. **Counting columns by iterating `row.cells`.** You get grid slots, not cells: 3 instead of 2 above. Any "number of cells", "last cell", or `range(len(row.cells))` logic overcounts every spanned column, and a loop that deletes/mutates each entry hits the same `<w:tc>` twice (second pass: parent already `None`, wrong deletion, or exception). Deduping by `is`/position is fragile; dedupe by `cell._tc` identity (or rely on `==`) if you must.

    **Instead**

    - Real cells in document order: `row._tr.tc_lst` (elements), or dedupe the proxies via their `_tc`.
    - Column count / coordinates: `len(table.columns)` / the `gridCol` grid, and keep using `table.cell(r, c)` — grid coordinates are the consistent model, as long as you know a spanned cell answers for multiple `c`.
    - To delete a cell, locate the specific `<w:tc>` you mean (usually `row._tr.tc_lst[i]`), remove it, and restore the row's grid coverage (adjust `gridSpan`/add a `tc`) so the row still spans the table's column count.

    Assumption for this non-interactive run: "spans two columns" means a horizontal merge expressed as `gridSpan=2` on the first cell's `<w:tc>`, not a nested table or a vertical merge.
- id: docx-rowdel-01
  answer: |
    No — python-docx has **no `table.delete_row()` and no `row.delete()`**. The public `Table` API only offers `add_row()`. Row objects (`_Row`) expose cells, height, etc., but no removal method. This is still true in current 1.x releases; deletion is intentionally left to XML-level manipulation.

    The standard workaround is to remove the row's `<w:tr>` element from the table:

    ```python
    def delete_row(table, idx):
        table._tbl.remove(table.rows[idx]._tr)
    ```

    Equivalently, from the row itself:

    ```python
    def delete_row(table, idx):
        row = table.rows[idx]
        row._element.getparent().remove(row._element)   # row._element is the <w:tr>
    ```

    `row._tr` and `row._element` are the same `<w:tr>` element; `row._element.getparent()` is the `<w:tbl>`.

    Practical caveats:

    - **Deleting while iterating**: `table.rows` is recomputed each access, so iterate over a snapshot (e.g., `list(table.rows)` or reversed indices) and delete by index/element, not by a stale row object list.
    - **Merged cells**: only delete the rows you actually want gone; removing a row that participates in a vertical merge leaves the surviving `vMerge` spans odd, so check the result.
    - **Empty table**: Word expects a table to have at least one row (and a `tblGrid`); removing every row can produce a document Word considers malformed. Leave one row, or delete the whole table instead (`table._tbl.getparent().remove(table._tbl)`).
    - Nothing else (paragraph marks, etc.) needs cleanup for a plain row removal.

    So: write a small helper around `table._tbl.remove(row._tr)`; there is no built-in method to call.
- id: docx-rowdel-02
  answer: |
    ## Short answer

    Removing the `<w:tr>` alone leaves the two continuation cells (`<w:vMerge/>`) with no `w:val="restart"` anchor above them. A `vMerge` continuation is defined as "continue the merge started above in this grid column"; with nothing above, the file is structurally inconsistent. Behavior is then consumer-dependent: Word usually self-heals by treating the topmost orphan continuation as the de-facto anchor, so the merge *looks* plausible but is one row shorter and its anchor content is gone; strict consumers (LibreOffice, Google Docs, validators, some renderers) may show two separate cells, misplace the merge, or repair the file. You have also silently deleted the text/formatting that lived in the anchor cell — in a vertical merge only the `restart` cell holds the visible content.

    ## Why it breaks

    - **Orphan continuations.** A `vMerge` run must be `restart` → one or more `continue` (a bare `<w:vMerge/>` is `continue`; `val` defaults to continue). Deleting the restart row leaves `continue, continue` with no start.
    - **Content loss.** The merge's paragraphs and run properties are in the anchor cell. Deleting the row deletes them; the remaining cells are normally empty placeholders, so the merged block becomes blank.
    - **Geometry loss.** The anchor cell's `tcPr` (`tcW`, `gridSpan`, `vAlign`, borders, shading) is what defines the merged block's width and appearance. The continuation cells may carry different `gridSpan`/`tcW`; a promoted continuation with the wrong `gridSpan` anchors the merge to fewer grid columns and the block goes crooked.
    - **Grid alignment.** `vMerge` works per *grid column*, not per cell index. If the row uses `gridSpan`, promoting "the first `<w:tc>`" is only correct if it covers the same grid columns.
    - **Last-row / table rules.** A `w:tbl` must retain at least one `w:tr`; don't delete the only row.

    ## Correct procedure

    Promote the next row (the first continuation row) to be the new restart, then remove the old restart row:

    1. Identify the merge by grid columns, not cell index (`gridSpan`). Do this for *every* vertically merged column in that row, not just the first.
    2. In the immediately following `<w:tr>`, for each corresponding cell:
       - replace `<w:vMerge/>` with `<w:vMerge w:val="restart"/>` (or add `vMerge` if absent), and
       - copy the old anchor's geometry/appearance into that cell's `tcPr` — at minimum `tcW` and `gridSpan`, plus `vAlign`, `tcBorders`, `shd` if you want the block to look identical.
       - respect `CT_TcPr` child order when inserting: `cnfStyle, tcW, gridSpan, hMerge, vMerge, tcBorders, shd, noWrap, tcMar, textDirection, tcFitText, vAlign, hideMark, …`. Inserting `vMerge` after `shd` produces a schema-invalid file that Word may reject.
    3. If the anchor cell's content must survive (e.g. the merged cell is a label and only the row's *other* content should go), move the anchor's `<w:p>` elements into the new anchor cell *before* deleting the row. Otherwise accept that the label is deleted with the row.
    4. Leave the remaining continuation rows untouched — their `<w:vMerge/>` now correctly continues the new anchor.
    5. Delete the `<w:tr>` from `<w:tbl>`. Do not delete individual `<w:tc>` elements (that changes the column count and breaks the grid); `tblGrid` is table-level and needs no change.
    6. Validate: in each grid column, every run of `continue` cells is preceded by a `restart`; no `continue` in the first row of the table or directly under a non-merged cell; at least one row remains.

    Before:

    ```xml
    <w:tr> <!-- row to delete -->
      <w:tc><w:tcPr><w:tcW w:w="2000" w:type="dxa"/><w:vMerge w:val="restart"/></w:tcPr>
        <w:p><w:r><w:t>Label</w:t></w:r></w:p></w:tc>
    </w:tr>
    <w:tr>
      <w:tc><w:tcPr><w:tcW w:w="2000" w:type="dxa"/><w:vMerge/></w:tcPr><w:p/></w:tc>
    </w:tr>
    <w:tr>
      <w:tc><w:tcPr><w:tcW w:w="2000" w:type="dxa"/><w:vMerge/></w:tcPr><w:p/></w:tc>
    </w:tr>
    ```

    After (promote row 2, then delete row 1):

    ```xml
    <w:tr>
      <w:tc><w:tcPr><w:tcW w:w="2000" w:type="dxa"/><w:vMerge w:val="restart"/></w:tcPr>
        <w:p><w:r><w:t>Label</w:t></w:r></w:p> <!-- only if preserved -->
      </w:tc>
    </w:tr>
    <w:tr>
      <w:tc><w:tcPr><w:tcW w:w="2000" w:type="dxa"/><w:vMerge/></w:tcPr><w:p/></w:tc>
    </w:tr>
    ```

    ## Edge cases

    - **Only two rows in the merge** (restart + one continuation): after deleting the restart, promote the single continuation to `restart`; the merge then spans one row, which is legal (a lone `restart` renders as a normal cell — you may prefer to just drop the `vMerge` element entirely so it's a plain cell).
    - **Deleting the whole merge block**: delete all rows of the run; nothing to promote.
    - **Deleting a continuation row instead**: just remove the `<w:tr>` — no promotion needed; the anchor above is still valid. Re-check that you didn't leave a lone `restart` where you intended zero-height.
    - **Library note**: `python-docx` (`tr.getparent().remove(tr)`) does no `vMerge` bookkeeping; that's exactly how the orphan bug gets created. Some tooling deletes the entire merged cell (all rows in the run) when you delete one row via a high-level "delete row" helper — verify what your library actually removed.
    - **UI caveat**: Word's own row-delete on a vertically merged selection can remove more than one row of the merged region; the XML edit above is the deterministic route.
- id: docx-rowdel-03
  answer: |
    Short answer: it shows the **old, stale total** — the cached field result that was in the document before the row was deleted (i.e. it still includes the deleted line item). Opening in Word does not fix it by itself.

    Why:

    - A Word field like `{ =SUM(ABOVE) }` is stored in the XML as a *complex field*: `w:fldChar type="begin"` → `w:instrText` containing ` =SUM(ABOVE) ` → `w:fldChar type="separate"` → the **cached result runs** (the number you see) → `w:fldChar type="end"`. (`w:fldSimple` is the one-element variant.)
    - python-docx only removes the `<w:tr>` element for the row. It does not evaluate fields, does not recalculate anything, and does not rewrite the cached result. It also does not set the `w:dirty` flag or `w:updateFields`.
    - Word does **not** recalculate formula fields on open by default. It displays the cached result until something explicitly updates the field (F9, print with "update fields" enabled, or an update flag in the file). So the Total renders the pre-deletion sum and the invoice is wrong by exactly the deleted row's amount.
    - Edge case: if the field had no cached result at all (never calculated), you may instead see the field code, a blank, or an error such as `!Syntax Error`, depending on the application.

    How to handle it:

    1. **Best/most deterministic — recompute in Python and write the new result.** Sum the line-item amounts yourself, then locate the field's result runs (between `separate` and `end`, or the text of the `w:fldSimple`) and replace the text with the new total, applying formatting to the first run and clearing the others. The field stays a real field, so later F9 still works, but the saved cache is correct.
    2. **Or force Word to recalculate.** Add `w:dirty="true"` to the `begin` `w:fldChar`, and/or add `<w:updateFields w:val="true"/>` to `word/settings.xml`. Word will then update fields on open (usually with a prompt). This is a good belt-and-braces addition even if you do (1), but it is not reliable in every renderer (LibreOffice, PDF converters, previewers), so don't rely on it alone.
    3. **Or tell the user to update fields** (`Ctrl+A`, then `F9`), or enable "Update fields before printing." Not acceptable for an automated invoice pipeline.
    4. For invoices, the robust design is: compute totals in code and write a plain value (or a bookmarked/text value), using `=SUM(ABOVE)` only as a convenience for interactive editing. `SUM(ABOVE)` is fragile anyway — it depends on column position, merged cells, blank/non-numeric cells, and whether rows were inserted rather than typed, so it can silently pick up the wrong range.

    Assumption: the field already had a cached result and the table layout was unchanged apart from the deleted row. Verify by unzipping the saved `.docx` and inspecting `word/document.xml` (or converting to PDF/opening in Word) to confirm the cached number and whether `w:dirty`/`w:updateFields` are present.
- id: docx-rowdel-04
  answer: |
    ## Short answer

    A DOCX row is an XML element, `<w:tr>`, inside a `<w:tbl>` in `word/document.xml`. Zero height, white text, and `w:vanish` are all *formatting properties* on that element or its runs. The element, its cells, its text, and everything anchored to it are still there. You prove deletion at the XML/structural level, not by looking at the rendered page.

    ## Why formatting is not deletion

    **The row still exists as a node.**

    ```xml
    <w:tr>
      <w:trPr>
        <w:trHeight w:val="0" w:hRule="exact"/>
      </w:trPr>
      <w:tc>
        <w:p>
          <w:r>
            <w:rPr>
              <w:color w:val="FFFFFF"/>
              <w:vanish/>
            </w:rPr>
            <w:t>Reimbursement total: $48,000</w:t>
          </w:r>
        </w:p>
      </w:tc>
    </w:tr>
    ```

    The `<w:t>` is intact. So the row still participates in:

    - text extraction (`python-docx`, `docx2txt`, `pandoc`, search/index pipelines)
    - copy/paste and Select All
    - spellcheck, word counts, and accessibility trees
    - find/replace, field evaluation (`REF`, `SEQ`, cross-references)
    - bookmark, comment, and footnote anchors
    - tracked-change and revision state
    - table grid math (row slot, `gridBefore`/`gridAfter`, `vMerge`)
    - export filters that don't honor `vanish`/color

    **Zero height is a hint, not a structure change.** `w:trHeight` with `hRule="exact"` is a rendering instruction that renderers may clamp, ignore, or override via style-level `trHeight`, cell margins, borders, or content that cannot fit. Word, LibreOffice, Google Docs, and PDF exporters all differ. Even a perfectly invisible row still occupies a row slot in the table model and can affect vertical merges, repeating headers (`tblHeader`), and pagination.

    **White text is trivially recoverable.** Select it, change the theme, switch to high-contrast/dark mode, "Reveal Formatting," or extract text programmatically. `w:color` is stored in `w:rPr`; it never deletes the run.

    **Hidden runs are still stored.** `<w:vanish/>` (and `w:specVanish` for TOC entries) means "don't display/print by default." The run and its text remain in the part, are revealed by Word's Show/Hide ¶ toggle, and are routinely extracted by tools that ignore `vanish`.

    **Tracked changes make it worse.** A row "deleted" with revisions on is typically `<w:trPr><w:del .../></w:trPr>` with `w:delText` in cells. It is visually gone but fully present until revisions are accepted.

    So: formatting changes *appearance*. Deletion changes *the tree*.

    ## What actual deletion requires

    1. Remove the `<w:tr>` node from its `<w:tbl>` parent (all rows, including nested tables).
    2. Fix table invariants: `w:tblGrid` column count stays the same; each remaining row's `w:tc` + `gridSpan` + `gridBefore`/`gridAfter` still sums correctly.
    3. Repair vertical merges. If the removed row was a `vMerge` continuation, the merge span shrinks; if it was the `vMerge` restart, the first continuation must become the restart or the merge must be removed entirely.
    4. Resolve dangling anchors: `w:bookmarkStart`/`w:bookmarkEnd`, `w:commentRangeStart`/`w:commentRangeEnd`, comment references, footnote/endnote references, and fields pointing into the removed content.
    5. If revisions are in play, accept/reject them before judging the result.
    6. Watch other parts: the same logical row can exist in `header*.xml`, `footer*.xml`, `footnotes.xml`, `endnotes.xml`, `comments.xml`, or inside `w:txbxContent` / `mc:AlternateContent` / `altChunk`.

    ## How to prove the row is really gone

    Use a proof hierarchy — the XML check is authoritative; rendering is only corroborating.

    **1. Structural check (strongest).** Unzip and parse with a real XML parser. Never regex the XML.

    ```bash
    unzip -p file.docx word/document.xml > /tmp/document.xml
    xmllint --xpath 'count(//*[local-name()="tbl"]/*[local-name()="tr"])' /tmp/document.xml
    ```

    ```python
    from lxml import etree
    W = "{http://schemas.openxmlformats.org/wordprocessingml/2006/main}"
    tree = etree.parse("/tmp/document.xml")
    rows = tree.findall(f".//{W}tbl/{W}tr")   # top-level rows
    # also check .//{W}tbl//{W}tr for nested tables
    for tr in rows:
        text = "".join(t.text or "" for t in tr.iter(f"{W}t"))
        if "Reimbursement total" in text:
            raise SystemExit("ROW STILL PRESENT")
    ```

    Assert both: (a) row count dropped by exactly one, and (b) no `<w:tr>` contains the target text. Anchoring on element identity (target text, `w14:paraId`, bookmark names) is better than a bare string search, because the same text may legitimately appear elsewhere.

    **2. Whole-package check.** Scan every XML part, not just `document.xml`.

    ```python
    import zipfile
    with zipfile.ZipFile("file.docx") as z:
        for name in z.namelist():
            if name.endswith(".xml"):
                data = z.read(name)
                if b"Reimbursement total" in data:
                    print("found in", name)
    ```

    **3. Reference/dependency check.** Confirm no orphaned `bookmarkStart`/`bookmarkEnd`, `commentRangeStart`/`End`, comment IDs, or footnote references were left behind, and that any `REF`/`PAGEREF` fields no longer target the row. Orphaned markers are a common sign of a sloppy "delete."

    **4. Table-integrity check.** Verify `tblGrid` is unchanged, row cell counts still match the grid, and every `vMerge` chain is still valid (exactly one restart followed by continuations).

    **5. Round-trip and extraction.** Open/save in Word and LibreOffice, then re-extract:

    ```bash
    pandoc file.docx -t plain | grep -F "Reimbursement total"
    python -c "import docx; d=docx.Document('file.docx'); print([c.text for t in d.tables for r in t.rows for c in r.cells])"
    ```

    **6. Render as a last check, not a proof.** Convert to PDF and `pdftotext`/OCR. Absence here is *not* proof of deletion — hidden or white text legitimately fails to render. Presence here *is* proof the "deletion" failed.

    **7. Best evidence: canonical XML diff.** Normalize both versions (`xmllint --c14n`) and diff. A real deletion shows exactly one `<w:tr>` subtree removed (plus expected `rsid`/revision noise). Anything else — a surviving `<w:trHeight>`, a changed `<w:color>`, a new `<w:vanish/>` — shows you only hid the row.

    ## Bottom line

    Zero height, white text, and hidden runs are presentational flags on a row that is still in the document. "Gone" means the `<w:tr>` element no longer exists in any relevant XML part, the table's grid/merge invariants still hold, and no bookmark, comment, or field reference dangles. Prove it by parsing the XML and diffing node trees; a clean PDF or a blank-looking page proves nothing.
- id: docx-cell-01
  answer: |
    `cell.text = "SKU"` clears everything inside the `<w:tc>` except `<w:tcPr>`, then creates a new paragraph and run containing `SKU`.

    **Survives**
    - Cell-level formatting in `<w:tcPr>`: the dark cell shading (`<w:shd>`), borders, cell width, margins, vertical alignment, text direction, and merge/grid-span properties.
    - Table/row-level properties: table style, header-row repeat, row height.
    - Formatting contributed by styles: if the bold/9 pt/white text comes from the table style’s header conditional formatting or another style, it may still be applied to the new paragraph/run, because the table style itself was not removed.

    **Lost**
    - All direct run formatting: bold, 9 pt size, white font color, font name, italic, underline, character spacing, etc.
    - All direct paragraph formatting: alignment, spacing, indents, tabs, paragraph shading/borders, and the original paragraph style.
    - All previous content: other runs, multiple paragraphs, line breaks, hyperlinks, fields, images, bookmarks, etc.

    So the dark shading normally remains, but direct bold + 9 pt + white font is gone. The new `SKU` run falls back to the paragraph/table/document defaults — often black, non-bold, default size. That can leave dark text on the dark cell fill, so you usually need to reapply the run formatting:

    ```python
    cell.text = "SKU"
    run = cell.paragraphs[0].runs[0]
    run.bold = True
    run.font.size = Pt(9)
    run.font.color.rgb = RGBColor(0xFF, 0xFF, 0xFF)
    ```

    If the white text or shading was applied through a paragraph style or paragraph-level shading rather than the table style or cell properties, that is also lost with the old paragraph.
- id: docx-cell-02
  answer: |
    **Direct answer:** the Qty change from 5 to 12 propagates through every derived figure in the invoice — the line amount and everything built on top of it.

    **What must change**

    1. **The line amount** for that row — `Qty × Unit Price`. With 5→12 it becomes 2.4× the old value (12/5). If unit price is unchanged, only this product changes.
    2. **Any total-quantity figure** — "Total items: 5", "5 units", package counts, or a units column summarised at the bottom.
    3. **The subtotal** — the sum of all line amounts, which now includes the new line amount.
    4. **Discounts**, if they are percentage-based (they scale with the subtotal) or quantity-based (tier/volume discount may now apply at 12).
    5. **Tax** — tax computed on the subtotal (or on the taxable line) changes; tax computed per line changes for that line.
    6. **Shipping/handling**, if it is weight- or quantity-tiered.
    7. **Grand total / amount due / balance** — `subtotal − discount + tax + shipping`.
    8. **Amount in words**, if the document has one, must match the new numeric total.
    9. **Any narrative or cross-reference** — e.g. "12 units at $X each", a cover note, a summary table, or an approval line quoting the old total.

    **Assumptions:** only Qty changed; Unit Price and tax rate are fixed; the row's amount is `Qty × Unit Price`; totals are additive (not e.g. a fixed contract sum).

    **How to make the numbers consistent**

    - **Treat derived cells as formulas, not text.** Recompute `amount = qty × price`, then `subtotal = Σ amounts`, `tax = rate × taxable base`, `total = subtotal − discount + tax + shipping`. Edit the source value (Qty) and let the chain recalculate — never hand-type the downstream cells independently.
    - **Work bottom-up in one pass:** line amount → subtotal → discount → tax → total → amount in words. Editing the total first is how mismatches creep in.
    - **Re-read the whole document for orphaned numbers:** header/summary tables, footnotes, "total items", and prose that quotes quantities or totals.
    - **Run arithmetic checks:** every line amount equals qty × price; line amounts sum exactly to the subtotal; tax equals rate × base (watch rounding — round per line or on the subtotal, but pick one convention and apply it everywhere); grand total equals its components; words match figures.
    - **Verify the final state visually** (cell by cell) and, if the file has fields/formulas, force a recalculation and confirm no stale cached values remain.

    The core discipline: one source edit, recompute every dependent figure from formulas, then reconcile the document against its own arithmetic before saving.
- id: docx-cell-03
  answer: |
    ## Short answer

    `w:trackRevisions` is an instruction to **Word's editing engine**, not a property of the file format. python-docx writes XML directly and never "performs an edit," so Word has nothing to record. Rewriting `w:t` produces a **silent, untracked modification**: the text changes, no `<w:ins>`/`<w:del>` is created, the Review pane shows nothing, and accept/reject cannot see or revert it. Worse, the same rewrite usually **destroys or falsifies the revision markup already in that cell**.

    ## Why it happens

    - Track Changes only generates revision elements when Word (or another word processor that implements the feature) does the edit itself. There is no retroactive marking when a file is reopened.
    - python-docx has no revision API. `run.text`, `paragraph.text`, and `cell.text` all emit plain `<w:t>`.
    - The `w:trackRevisions` flag stays `true`, so Word considers future edits tracked — while your edit sits there looking like original text. That asymmetry is exactly what a reviewer or a "Compare Documents" run will flag.

    ## What concretely breaks in the cell

    1. **No attribution.** The text appears to have always been there. `w:rsid` values are not updated either, so forensic/compare tools may flag the difference while the document's revision pane shows a clean file.
    2. **Runs already inside `<w:ins>`.** Their text is ordinary `w:t`, so python-docx reads it. If you overwrite it, the run stays inside the original `<w:ins>` and inherits the **original author, date, and `w:id`** — a false attribution. If you use `cell.text = ...`, the wrapper vanishes: a silent acceptance of someone else's insertion.
    3. **Runs inside `<w:del>`.** Deleted text lives in `<w:delText>`, which python-docx does not expose. So the classic `p.text = p.text` round-trip **drops deleted text entirely** — a silent acceptance of deletions, with the original wording permanently gone. Writing a `w:t` inside `w:del` is invalid markup; Word may repair or ignore it.
    4. **`cell.text = ...` is a nuke.** It clears every paragraph and table in the cell and adds one fresh paragraph. You lose `w:pPr`, numbering, per-run formatting, hyperlinks, bookmarks, comment ranges, cell-level revision marks (`w:cellIns`/`w:cellDel` in `tcPr`), and any `w:ins`/`w:del` wrappers. `paragraph.text = ...` is similar at paragraph scope. `run.text = ...` is gentler — it preserves `w:rPr` but still destroys tabs, breaks, drawings, fields, symbols, and footnote refs in that run.
    5. **Run fragmentation.** Word splits text across many runs; "one run = one cell" is false. Flattening to a single run silently loses mid-text formatting.
    6. **Containers python-docx skips.** `paragraph.runs` only sees direct `w:r` children. Text inside `w:hyperlink` or `w:sdt` is missed, so a naive rewrite leaves stale text behind or edits only half the cell.
    7. **Whitespace.** Raw `w:t` assignment needs `xml:space="preserve"` or leading/trailing spaces get collapsed.
    8. **Protection bypass.** If `w:documentProtection` enforces revisions, you are writing around a documented integrity control — a compliance problem regardless of intent.

    ## What to consider

    **Decide the semantics first.**

    - **Tracked (redline) edit** — author it yourself. Move the old text into `<w:del><w:r><w:delText xml:space="preserve">old</w:delText></w:r></w:del>` and the new text into `<w:ins w:id="N" w:author="…" w:date="…"><w:r><w:t xml:space="preserve">new</w:t></w:r></w:ins>`, in the same position within the paragraph. Allocate document-unique `w:id`s (max existing + 1). Copy `w:rPr` onto both runs so the strikethrough/underline matches. If you split or merge paragraphs, keep the paragraph-mark revision (`w:pPr/w:rPr/w:ins|w:del`) consistent — otherwise Word shows odd merge behavior.
    - **Clean (accepted) edit** — resolve revisions *deliberately* before writing: unwrap every `w:ins` (keep children), remove `w:del` (or convert `delText` back to `w:t` if you want to retain the wording), drop `w:rPrChange`/`w:pPrChange`/`w:moveFrom`/`w:moveTo`. Only clear `w:trackRevisions` if the user explicitly wants tracking off going forward — don't switch it off silently.
    - **Tooling** — for real redlines at scale, Word automation, LibreOffice UNO, docx4j, or Aspose.Words produce correct revision markup. python-docx can do it, but only if you write the `w:ins`/`w:del` XML by hand.

    **Implementation cautions:** edit the minimal `w:t` nodes rather than whole-paragraph text; copy `rPr` from the neighbouring run when creating runs; iterate `w:hyperlink`/`w:sdt` content via XPath; preserve `xml:space`; keep revision elements in document order; save to a new file and retain the original (hash it) — never overwrite in a review workflow.

    **Validate:** reopen in Word, switch Review to *All Markup*, confirm the change appears as an attributable insertion/deletion, and diff `word/document.xml` to confirm existing revision wrappers survived. Watch for Word's "unreadable content" repair prompt, which usually means a `w:t` landed inside `w:del` or an `w:id` collided.

    **Bottom line:** python-docx bypasses Track Changes entirely — it cannot create a tracked edit, and a careless rewrite silently accepts, falsifies, or erases the revisions already in the cell.
- id: docx-relayout-01
  answer: |
    ## What `add_row()` actually does

    `Table.add_row()` is **append-only**. It creates a new `<w:tr>` element and puts it at the end of the table's `<w:tbl>` (after the last existing `w:tr`, respecting the schema order where `w:tr` follows `w:tblGrid`). It returns a `docx.table._Row` wrapper around that new `<w:tr>`.

    Consequences:

    - There is **no public API for inserting mid-table**. `add_row()` and `add_column()` both append only. (Same for `Document.add_table()`, which builds rows via the same path.)
    - The new row is a **blank/default row**: one empty `<w:tc>` per grid column, no text, and **no direct formatting copied from its neighbours** — no `w:tcPr` shading/borders/widths, no `w:trPr` (`cantSplit`, `tblHeader`), no run formatting. It inherits only what the table-level `w:tblStyle` gives it. So an `add_row()` row jammed into the middle of a directly-formatted table usually looks visibly wrong (different borders, missing shading, wrong column widths).
    - `table.rows` is recomputed on each access, so indices shift after any insert.

    ## Recommended approach: clone a formatted row, then move it

    The reliable way to get a *correctly formatted* row is to deep-copy an existing real row and insert that. Copying a row copies its `w:trPr`, every `w:tc`, every `w:tcPr` (width, borders, shading, `gridSpan`, `vMerge`), and the paragraph/run properties inside.

    ```python
    from copy import deepcopy
    from docx import Document
    from docx.table import _Row

    doc = Document("in.docx")
    table = doc.tables[0]                      # or find it by index/content

    # 1. Locate the Total row (don't assume it's last)
    total_idx = next(
        i for i, r in enumerate(table.rows)
        if "".join(c.text for c in r.cells).strip().lower().startswith("total")
    )
    total_row = table.rows[total_idx]
    template  = table.rows[total_idx - 1]      # the "Service G" row = desired look

    # 2. Clone the template row (XML-level copy, keeps all formatting)
    new_tr = deepcopy(template._tr)

    # 3. Insert the clone immediately before the Total row.
    #    addprevious() MOVES an element, so no duplicate is left behind.
    total_row._tr.addprevious(new_tr)

    # 4. Wrap it so you can use the normal cell/paragraph API
    new_row = _Row(new_tr, table)
    new_row.cells[0].text = "Service H"
    new_row.cells[1].text = "2026-11-01"
    new_row.cells[2].text = "1,200.00"

    doc.save("out.docx")
    ```

    ### If you want to use `add_row()` anyway

    You can still lean on it for cell creation, then relocate the element — `addprevious` moves rather than copies, so you don't get a stray row at the bottom:

    ```python
    new_row = table.add_row()                    # lands after Total
    total_row._tr.addprevious(new_row._tr)       # move it into place

    # Now repair formatting manually, cell by cell
    for src_tc, dst_tc in zip(template._tr.tc_lst, new_row._tr.tc_lst):
        tcPr = src_tc.tcPr
        if tcPr is not None:
            dst_tc.insert(0, deepcopy(tcPr))     # tcPr must be first child of w:tc
    if template._tr.trPr is not None:
        new_row._tr.insert(0, deepcopy(template._tr.trPr))
    ```

    This works but is strictly more code and more fragile than deep-copying the row, because you have to reconstruct everything the copy would have carried for free.

    ## Gotchas that decide "correctly formatted"

    - **Cell count vs grid**: the inserted row must have as many `<w:tc>` as the row above/below it logically has columns. A deep copy guarantees this; hand-built rows are where "ragged table" corruption comes from.
    - **Merged cells**: if the template row has `w:gridSpan` or `w:vMerge`, its physical cells are fewer than logical columns. `_Row.cells` collapses merged cells (the same `_Cell` repeats), so set text via `new_row.cells` carefully, or work on `new_row._tr.tc_lst` for physical cells. Don't clone a merged row unless you want the same merge.
    - **Clearing the cloned text**: `cell.text = "..."` replaces the cell's content with a single new paragraph, which drops run-level formatting (bold, font size) while keeping `w:tcPr`. If you need to keep character formatting, do:
      ```python
      p = cell.paragraphs[0]
      if p.runs:
          p.runs[0].text = value
          for r in p.runs[1:]:
              r._r.getparent().remove(r._r)
      else:
          p.add_run(value)
      ```
    - **Wrong template**: if the Total row is bold/shaded and you want the new row to match the data rows, clone Service G (or a data row). If you want it to match the Total style, clone Total instead — the copy is your style choice.
    - **Banding / conditional formatting**: with `w:tblLook` banded rows, Word recomputes shading from position, so banding may flip on the rows below after insertion. That's normal; direct `w:tcPr` shading overrides it.
    - **Header repeat / page breaks**: if the Total row carried `w:trPr` like `cantSplit`, copying it inherits that; usually fine, but check if Total is meant to stay unsplittable.
    - **Private API**: `table._tbl`, `row._tr`, `tc.tcPr` are private and can change between python-docx versions (behaviour has shifted across 0.8.x → 1.x). Pin your python-docx version and re-verify after upgrades.
    - **Verification**: reopen the saved file and assert `len(table.rows)` grew by one, `table.rows[total_idx].cells[0].text == "Service H"` (new row now at the old Total index, and Total shifted down), and that the last row is still the Total row. Unzipping and eyeballing `word/document.xml` around `<w:tbl>` is the ground truth for element order.

    Bottom line: `add_row()` appends a blank, unformatted-by-neighbour row at the bottom; to get a formatted row between Service G and Total, deep-copy the Service G `<w:tr>` and insert it with `total_row._tr.addprevious(new_tr)`, then rewrite cell text (using the run-preserving clear if you need to keep character formatting).
- id: docx-relayout-02
  answer: |
    **What `table.add_column(width)` does**

    `width` is a length (`Inches(...)`, `Cm(...)`, or twips/EMU) applied to a single new `<w:gridCol>` appended at the **right end** of the table grid. python-docx then appends one empty `<w:tc>` to every `<w:tr>` so the row/grid stay rectangular. It returns the new `_Column`.

    What it does **not** do:

    - It does not insert after `Item`. It always appends at the far right; after "Item" is a different position, so you have to move the XML (the new `gridCol` and the new `tc` in every row) to the desired index, or rebuild the table.
    - It does not set `w:tcW` on the new cells, and it does not touch the widths of the existing columns or the table's total width (`w:tblW`).
    - It does not copy formatting. Style-derived formatting (borders/shading from the table style) will apply because it is table-level, but any direct formatting on the other cells (`tcPr` shading, margins, `vAlign`, paragraph/run properties, header bold) will not.

    **What you must do to stay inside the margins and match the other columns**

    1. Compute usable width: `section.page_width - section.left_margin - section.right_margin` (US Letter portrait with 1" margins = 6.5"). The sum of all `gridCol` widths must be ≤ that.
    2. Give the new column a width and take it out of the existing columns (usually shrink `Item` or the widest text column by the same amount) so the total does not grow past the usable width.
    3. Turn off autofit and pin the layout so Word does not silently redistribute: `table.autofit = False` and set `w:tblLayout w:type="fixed"` (python-docx has no direct property; add it to `tblPr`). Also set `w:tblW` explicitly.
    4. Set widths on **both** the grid and the cells: `table.columns[i].width = ...` (this writes `gridCol.w` and each cell's `tcW`), or set each `cell.width`, for every row including the header. A grid-only width with autofit on is what makes the table blow past the margin.
    5. Match appearance: copy the adjacent cell's `tcPr` (borders, shading, `tcMar`, `vAlign`) and its paragraph/run properties, set the same paragraph style/alignment, and fill in the header text and any data cells. Reuse the same table style and header-row properties rather than hand-rolling borders.

    **Assumption:** "other columns" are formatted via a consistent scheme (table style plus, possibly, direct cell formatting); if it is direct formatting, you must copy `tcPr`/`pPr`/`rPr` explicitly, since `add_column` never does it for you.
- id: docx-relayout-03
  answer: |
    ## Short answer

    `w:tblGrid`/`w:gridCol` is only the *declared* column grid. Word does not lay the table out from it — it lays out from the **cell-level preferred widths** (`w:tcPr/w:tcW`) and the table width (`w:tblW`), then recomputes/regenerates the grid on open. If you change only `gridCol`, every cell still carries its old `tcW`, so Word keeps rendering the old widths (and often rewrites `tblGrid` back to match the cells).

    Assumption: a normal WordprocessingML table in the document body, edited directly in the XML, opened in desktop Word.

    ## What is actually authoritative

    - `w:tblGrid/w:gridCol/@w:w` — abstract grid definition (twips). Used as a hint/fallback, for `gridSpan` bookkeeping, and by some other consumers (LibreOffice, converters). Word treats it as recomputable.
    - `w:tblPr/w:tblW` — overall table preferred width.
    - `w:tr/w:tc/w:tcPr/w:tcW` — per-cell preferred width. **This is what Word renders from.**
    - `w:tblPr/w:tblLayout` — `fixed` vs autofit. Autofit lets Word resize based on content and can silently override your intent.

    ## What you must update

    To make a column-width change stick in Word:

    1. **Every cell in the column, in every row**, set `w:tcW` to the new value with `w:type="dxa"` (twips; 1440 = 1", 567 ≈ 1 cm). That includes header rows, body rows, and `vMerge` continuation cells. A single stale row can pull the column back.
    2. **Merged cells (`gridSpan`)**: set the cell's `tcW` to the **sum** of the spanned grid columns, and make sure the following cells line up. Also handle `gridBefore`/`gridAfter`.
    3. **Keep `tblGrid` consistent** with those values and with the column count — same widths, right number of `gridCol` elements. It is not the driver, but a mismatch causes odd behaviour in Word and wrong output in other tools.
    4. **Fix the table width**: `w:tblW` (dxa) should equal the sum of the columns, and `w:tblInd` plus that sum must fit the page text width, or Word/autofit will rescale and it will look like the old widths came back.
    5. **Force fixed layout**: set `w:tblLayout w:type="fixed"` so Word stops autofitting to content. Also make sure no `tcW`/`tblW` remain `w:type="auto"`, and do not mix `dxa` cells with `pct` cells — percentages resolve against table width and will fight your twips.
    6. **Check overrides above the cell**: table style definitions, conditional formatting, and `w:tblPrEx` (row-level table property exceptions) can carry widths or layout. Nested tables need the same treatment independently.

    ## Practical rules

    - Change `gridCol` **and** all `tcW` values together, ideally from one source-of-truth width list, so they can never drift.
    - If you only have the XML and want minimal churn: compute new widths, write them to `tblGrid`, then walk `w:tr/w:tc` and rewrite `tcW` for the corresponding grid positions (accounting for `gridSpan`).
    - Beware `w:tblLayout` autofit and `tblW type="auto"` — these are the usual reason widths appear to "revert" after a correct-looking grid edit.
    - If the document is opened from a stale copy, an autorecovered file, or Word's own saved copy, that also explains "old widths", but in a pure XML-edit workflow the `tcW` explanation is the correct one.

    BOTTOM LINE: `gridCol` is a hint; `tcW` (plus `tblW` and fixed `tblLayout`) is what Word renders, so update every cell's `tcW` — including merged and continuation cells — and keep `tblGrid` in sync.
- id: docx-tblins-01
  answer: |
    **Where `doc.add_table()` puts it:** at the **end of the document body**, not at the cursor/selection. It appends a `<w:tbl>` to `document.element.body`, just before the trailing `<w:sectPr>` (section properties). python-docx has no notion of a "current insertion point" for tables, so "right after the paragraph" is never where it lands by itself.

    **How to place it correctly:** create the table, then move its XML element to sit immediately after the target paragraph.

    ```python
    from docx import Document

    doc = Document("in.docx")

    # find the paragraph whose text is "Notes:"
    target = next(p for p in doc.paragraphs if p.text.strip() == "Notes:")

    table = doc.add_table(rows=2, cols=3)   # created at end of body
    target._p.addnext(table._tbl)           # move it right after that paragraph
    ```

    `addnext` is lxml's method on the underlying `<w:p>` element, so the table ends up as the paragraph's next sibling — exactly "right after". If you need a blank paragraph between them, add one or move that instead.

    Equivalent, more explicit lxml forms:

    ```python
    target._p.addnext(table._tbl)

    # or positional insertion into the body:
    body = doc.element.body
    body.insert(list(body).index(target._p) + 1, table._tbl)
    ```

    Practical caveats:

    - Keep the table element and the body in the same tree; moving `table._tbl` preserves the `Table` proxy, so `table` remains usable for adding rows/cells afterward.
    - Don't try to insert *after* the final `<w:sectPr>` — content must precede it. `addnext` on a real paragraph is fine; appending to the body blindly can put a table after `sectPr`, which is invalid.
    - For "a paragraph then a table then more paragraphs" layouts, insert the following paragraph first or capture the element that currently follows `Notes:` and use `addprevious` on that element instead.
    - If you locate the anchor by text, match exactly and guard against duplicates (`next(...)` raises `StopIteration` if none matches).

    If you're unsure whether "Notes:" is a heading-style paragraph: `doc.paragraphs` includes headings, so the same lookup works; style doesn't affect the XML move.
- id: docx-tblins-02
  answer: |
    ## Why the `KeyError`

    `table.style = name` in python-docx never creates a style. It resolves the string against the styles part of the document you opened — roughly `document.styles[name]` → an XPath for `w:style[@w:name="…"]` — and raises `KeyError: "no style with name 'Grid Table 4 Accent 1'"` when no matching `w:style` element exists.

    That style element almost certainly does not exist in your company template, for three compounding reasons:

    1. **Word's gallery is not your file.** The table-style gallery (Grid Table 1–7, List Table, Plain Table, …) lives inside Word. A built-in style only gets written into `styles.xml` as a real `<w:style w:type="table">` once something in that document actually uses it. Until then it is *latent* — a name Word knows, not a definition your file carries. python-docx has no access to Word's internal catalogue, so it can only see what is physically in `styles.xml`.
    2. **Version gap.** "Grid Table 4 Accent 1" belongs to the Word 2013+ gallery. python-docx's bundled default template carries the older Word-2007-era set (`Table Grid`, `Light Shading/List/Grid …`, `Medium …`, `Dark …`, `Colorful …`), so even a plain `Document()` fails the same way. A template built from an older base inherits that gap.
    3. **The UI name is not always the stored name.** python-docx passes names through a small fixed alias table (BabelFish) that translates things like `Table Grid` → styleId `TableGrid`. Names it doesn't know are passed through unchanged, and lookup is by `w:name`, not by styleId. Non-English Word installs also store *localised* `w:name` values (e.g. German "Tabelle Raster 4 Akzent 1") while the styleId stays English — so an English UI string can miss a style that is actually present.

    Note `document.styles.add_style('Grid Table 4 Accent 1', WD_STYLE_TYPE.TABLE)` would make the name resolve, but it creates an empty user-defined style with no borders — not a ruled table.

    ## How to get a ruled table

    **1. Use a style you know is defined.** `Table Grid` ships in the default template and gives single borders on all four edges plus inside rules:

    ```python
    table.style = 'Table Grid'
    ```

    In a client template, enumerate instead of guessing — never hard-code a gallery name blindly:

    ```python
    from docx.enum.style import WD_STYLE_TYPE
    avail = [s.name for s in doc.styles if s.type == WD_STYLE_TYPE.TABLE]
    ```

    Then pick from `avail` (e.g. `'Light Grid Accent 1'`, `'Medium Grid 1 Accent 1'`).

    **2. If you need that exact look, materialise the style once.** Open the template in Word, insert a table, apply "Grid Table 4 Accent 1", save. Word then writes the full `w:style` (including its `w:tblStylePr` banding rules) into `styles.xml`, and the name resolves from python-docx. If you can't touch the template, copy the element from a document that has it:

    ```python
    from copy import deepcopy
    src = Document('has-the-style.docx')
    dst = Document('company-template.docx')      # then save as the new base
    el = src.styles.element.xpath(
        'w:style[@w:type="table" and @w:name="Grid Table 4 Accent 1"]')[0]
    dst.styles.element.append(deepcopy(el))
    ```

    **3. Most robust: skip styles, write the borders yourself.** No template dependency, no name resolution:

    ```python
    from docx.oxml import OxmlElement
    from docx.oxml.ns import qn

    def rule_table(table, sz=4, color='000000'):
        tblPr = table._tbl.tblPr
        for old in tblPr.findall(qn('w:tblBorders')):
            tblPr.remove(old)
        borders = OxmlElement('w:tblBorders')
        for edge in ('top', 'left', 'bottom', 'right', 'insideH', 'insideV'):
            e = OxmlElement(f'w:{edge}')
            e.set(qn('w:val'), 'single')
            e.set(qn('w:sz'), str(sz))      # eighths of a point: 4 = 0.5 pt
            e.set(qn('w:space'), '0')
            e.set(qn('w:color'), color)
            borders.append(e)
        tblPr.insert_element_before(
            borders, 'w:shd', 'w:tblLayout', 'w:tblCellMar',
            'w:tblLook', 'w:tblCaption', 'w:tblDescription')
    ```

    Use `insert_element_before` (not `append`): `tblBorders` must precede `w:shd`/`w:tblLayout`/`w:tblCellMar`/`w:tblLook` in `tblPr`. Word usually tolerates wrong order, LibreOffice sometimes doesn't.

    **4. Last-resort escape hatch — reference the built-in by styleId without defining it:**

    ```python
    el = OxmlElement('w:tblStyle')
    el.set(qn('w:val'), 'GridTable4-Accent1')   # verify the exact ID
    table._tbl.tblPr.insert(0, el)              # tblStyle must be the first child
    ```

    Word resolves built-in styleIds from its own catalogue, so this often renders correctly. But the ID must be exact, python-docx's `table.style` getter will still fail, and LibreOffice will likely ignore it.

    **Recommendation:** `table.style = 'Table Grid'` (or another style you verified is in the template), then add your own `tblBorders`/shading for branding. Deterministic, and it survives template swaps.

    Two related gotchas: `document.styles` lookup is case/string exact on `w:name`, and if the company template is a `.dotx`, `Document()` rejects it outright (`ValueError: … not a Word file, content type is …template.main+xml`) — copy it to `.docx` first.
- id: docx-tblins-03
  answer: |
    Short answer: **it spans the section's text column** — page width minus the left and right margins — with the columns divided equally. For a Letter page with 1" margins that is 6.5" (9360 twips / 16.51 cm) for a 2-column table, 3.25" per column. `python-docx` builds the `w:tblGrid` from `section._block_width` at `add_table` time, so the table is as wide as the text area of the section containing it, not the page.

    The existing invoice table's width is *not* knowable without looking at the source document — it could be narrower, indented, or have uneven columns. So "matching" means reading its geometry and copying it, not assuming 6.5".

    ```python
    from docx.shared import Twips

    src = doc.tables[0]                      # the existing invoice table
    new = doc.add_table(rows=n, cols=len(src.columns))

    # 1. Stop Word from re-autofitting to the text width
    new.autofit = False                      # emits <w:tblLayout w:type="fixed"/>

    # 2. Copy the grid column-by-column
    for s_col, n_col in zip(src.columns, new.columns):
        n_col.width = s_col.width            # sets w:gridCol/@w:w AND every cell's w:tcW

    # 3. Match total table width if the source uses an explicit tblW
    src_w = src._tbl.tblPr.tblW
    if src_w is not None and src_w.type != "auto":
        new._tbl.tblPr.tblW.type = src_w.type
        new._tbl.tblPr.tblW.w    = src_w.w
    ```

    Notes that matter in practice:

    - If `src.tblPr.tblW` is `type="auto"`, the real width lives in the `w:gridCol`/`w:tcW` values — step 2 is then the whole fix, and forcing `tblW` can actually make it diverge from the invoice.
    - Check `src.tblPr.tblInd` (left indent) too; a narrower invoice table is often just a normal-width table pushed right by an indent, and width alone won't line it up.
    - `table.autofit = False` is required — leaving autofit on lets Word redistribute/stretch columns regardless of the `w:w` values you wrote.
    - `python-docx` has no `Table.width` setter; width is expressed through per-column `width` (or `_Cell.width`) plus `tblW`, which is why you copy columns rather than assign one number.

    Caveat: the 6.5" figure assumes US Letter with 1" margins; the actual default is whatever the section's text width is (A4 with 2.54 cm margins gives 15.92 cm), so derive it from `section.page_width - section.left_margin - section.right_margin` rather than hard-coding.
- id: docx-imgrep-01
  answer: |
    In a `.docx`, the image bytes are **not** in `document.xml`. The XML only contains a *relationship ID* that points, through a `.rels` part, to a file under `word/media/`.

    ## How the reference works

    A modern Word picture looks roughly like this inside `word/document.xml`:

    ```xml
    <w:drawing>
      <wp:inline>
        <wp:extent cx="1828800" cy="914400"/>   <!-- display size, EMU -->
        <wp:docPr id="42" name="Logo" descr="Company logo"/>
        <a:graphic>
          <a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/picture">
            <pic:pic>
              <pic:nvPicPr>
                <pic:cNvPr id="0" name="logo.png"/>
                <pic:cNvPicPr/>
              </pic:nvPicPr>
              <pic:blipFill>
                <a:blip r:embed="rId7"/>
                <a:stretch><a:fillRect/></a:stretch>
              </pic:blipFill>
              <pic:spPr>
                <a:xfrm>
                  <a:off x="0" y="0"/>
                  <a:ext cx="1828800" cy="914400"/>
                </a:xfrm>
                <a:prstGeom prst="rect"><a:avLst/></a:prstGeom>
              </pic:spPr>
            </pic:pic>
          </a:graphicData>
        </a:graphic>
      </wp:inline>
    </w:drawing>
    ```

    The important part is:

    ```xml
    <a:blip r:embed="rId7"/>
    ```

    The `rId7` is resolved in `word/_rels/document.xml.rels`:

    ```xml
    <Relationship
      Id="rId7"
      Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image"
      Target="media/image1.png"/>
    ```

    So the actual bytes are in `word/media/image1.png`. The file type comes from `[Content_Types].xml`, usually:

    ```xml
    <Default Extension="png" ContentType="image/png"/>
    ```

    Position and size are also in `document.xml`:

    - Size: `wp:extent cx="..." cy="..."` and usually the matching `pic:spPr/a:xfrm/a:ext`. Units are EMU (914400 EMU = 1 inch).
    - Inline position: determined by the surrounding run/paragraph; there is no independent x/y.
    - Floating position: `wp:anchor` with `wp:positionH`, `wp:positionV`, wrap settings, etc.
    - Legacy/VML pictures use `<v:imagedata r:id="rId7"/>` instead of `a:blip`.

    ## Replacing one picture

    The safest method depends on whether that media part is used only by the logo.

    ### Case 1: The media file is used only by the logo

    This is the simplest and preserves position/size perfectly:

    1. Open the `.docx` as a ZIP.
    2. Find the target picture's `r:embed`, follow it through `word/_rels/document.xml.rels`, and get its `Target`.
    3. Overwrite that file, e.g. `word/media/image1.png`, with the new image bytes.
    4. Leave `document.xml`, the relations, `wp:extent`, and all positioning XML untouched.
    5. Rezip the package.

    Because the drawing still has the same extent and the same relationship, Word puts the new bytes in exactly the same place at exactly the same size.

    Caveat: if the new file has a different aspect ratio, Word will stretch it to the stored `wp:extent`. If you care about distortion, either:

    - adjust `wp:extent` and `a:ext` to a size with the new aspect ratio while keeping the same box/position, or
    - crop with `a:srcRect` rather than stretching.

    Also keep the format consistent with the extension/content type. Replacing a `.png` with PNG bytes is safe; replacing it with JPEG bytes under a `.png` name is not reliable.

    ### Case 2: The media part is shared, or the format changes

    If the same `rId`/media file is used by multiple pictures, overwriting it changes all of them. To replace only the logo:

    1. Add a new file, e.g. `word/media/logo_new.png`.
    2. Add a new relationship in `word/_rels/document.xml.rels`:

    ```xml
    <Relationship
      Id="rId200"
      Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image"
      Target="media/logo_new.png"/>
    ```

    3. Change **only** the logo's blip:

    ```xml
    <a:blip r:embed="rId200"/>
    ```

    4. Leave every other blip and relationship alone.
    5. If the new extension is not already declared, add a matching `<Default Extension="..." ContentType="image/..."/>` to `[Content_Types].xml`.
    6. Rezip.

    This is also the right approach when converting PNG to JPEG, SVG, etc.

    ## How to identify the correct picture

    Do not rely on document order. Use the picture's name or alt text:

    - `wp:docPr/@name`
    - `wp:docPr/@descr`
    - `pic:cNvPr/@name`

    Then take that element's `a:blip/@r:embed` and resolve it. That ensures you replace the logo and not an unrelated image.

    ## Position/size preservation summary

    - Keep the entire `w:drawing` / `wp:inline` or `wp:anchor` block unchanged.
    - Keep `wp:extent` and `a:ext` unchanged if you want the same displayed size.
    - Keep the relationship target and `r:embed` unchanged if you overwrite the media bytes.
    - If you add a new media part, change only `r:embed` and add the new relationship.
    - For headers/footers, the same structure applies, but the references live in `word/headerN.xml` and `word/_rels/headerN.xml.rels`, not `document.xml`.
    - If the picture uses `r:link` instead of `r:embed`, it is externally linked, not embedded; the replacement procedure is different.

    **Short version:** `document.xml` stores `a:blip r:embed="rIdN"`, the `.rels` file maps `rIdN` to `media/imageX.png`, and the bytes live there. To replace one logo, either overwrite that media file if it is unique, or add a new media file plus a new relationship and point only that one `r:embed` at it. Leave the drawing's extent and anchor/inline XML untouched to preserve size and position.
- id: docx-imgrep-02
  answer: |
    **Short answer:** because a DOCX doesn't store an image "per picture" — it stores image *parts*, and several pictures can reference the *same* part. `image_part._blob = new_bytes` mutates that one shared part, so every picture pointing at it renders the new bytes. Page 3's picture wasn't a separate blob; it was a second reference to the same image part as the logo.

    **Why the parts get shared**

    - python-docx deduplicates image parts by the SHA-1 of the image bytes (`ImageParts.get_or_add_image_part` → `_find_by_sha1`). If you add the same bytes twice (or Word already saved a doc where the logo appears more than once — repeated logo, watermark, header/footer copy, a copied/pasted picture), you get **one `ImagePart`** and two references to it.
    - References are made through relationships. Multiple `<a:blip r:embed="rId…"/>` elements can carry different rIds that resolve to the *same* target part (and `relate_to` may even reuse an rId). The rId is a property of the reference, not of the image.
    - Word itself also collapses byte-identical media on save, so a document you didn't build with python-docx can already have two visible pictures backed by one `word/media/imageN.ext`. You can hit this without ever having called `add_picture` twice.

    So mutating `_blob` is a part-level operation, not a shape-level one. It has no notion of "the logo on page 1"; it just swaps bytes inside the single stored part.

    **Secondary problem:** assigning `_blob` directly also bypasses the image-part bookkeeping — the partname, content type, and SHA-1 index still describe the old bytes, so the package is now internally inconsistent (usually harmless on save, but it can confuse dedupe and any later lookup by hash).

    **How to confirm:** walk `document.part.related_parts` (or `document.part.package.image_parts`) and compare the `r:embed` targets of the page-1 logo's `a:blip` and the page-3 picture's `a:blip`. If both resolve to the same part object/partname, that's the smoking gun.

    **Correct fix:** don't mutate the shared part. Materialize the new bytes as a *new* image part, relate it to the document, and re-point only the logo's blip:

    ```python
    from docx.opc.constants import RELATIONSHIP_TYPE as RT
    from docx.parts.image import ImagePart

    new_part = ImagePart.from_image(new_image, partname=next_free_partname)
    document.part.relate_to(new_part, RT.IMAGE)
    # then set only the logo's blip r:embed to the new rId
    logo_blip.set(qn('r:embed'), new_rId)
    ```

    Equivalently at the zip level: add `word/media/imageN+1.ext`, add a new relationship in `word/_rels/document.xml.rels`, and change just the logo's `r:embed`.

    **Assumption:** page 3's picture changed *to the new logo* (i.e., it rendered the same new bytes) — that's the signature of a shared part. If instead it changed in some unrelated way, the likely cause is that you looked the part up by index/partname rather than by the logo's actual rId, and grabbed a different picture's part.
- id: docx-imgrep-03
  answer: |
    ## Where the logo actually lives

    Two candidates, and the "top of every page" clue decides it:

    1. **A page header** — `w:hdr` part(s), reached through `doc.sections[n].header` (plus `.first_page_header` / `.even_page_header`). A logo that repeats on every page is almost always in the header story, not in the body.
    2. **A floating/anchored drawing** — `<wp:anchor>` instead of `<wp:inline>`.

    Reason `doc.inline_shapes` is empty: `InlineShapes` is built by walking **only** the document part's body (`document.xml`) and collecting **only** `wp:inline` elements. It never opens header/footer parts, and it ignores `wp:anchor`. So a header logo *and* an anchored body logo are both invisible to it. Your case is #1.

    A third possibility worth ruling out: an old-style **VML** image (`<v:imagedata r:id="rIdN">` inside `<w:pict>`), which often sits in `mc:AlternateContent`. That also never shows up in `inline_shapes`.

    ## Finding it

    ```python
    from docx import Document
    from docx.oxml.ns import qn

    doc = Document("in.docx")

    def image_refs(part):
        """Yield (rId, kind) for every image reference in a header/footer/body part."""
        root = part.element if hasattr(part, "element") else part._element
        for blip in root.xpath(".//a:blip"):                 # DrawingML
            rid = blip.get(qn("r:embed")) or blip.get(qn("r:link"))
            if rid:
                yield rid, "blip"
        for vml in root.xpath(".//v:imagedata"):             # legacy VML
            rid = vml.get(qn("r:id"))
            if rid:
                yield rid, "vml"

    for si, section in enumerate(doc.sections):
        for name in ("header", "first_page_header", "even_page_header"):
            hdr = getattr(section, name)
            if hdr.is_linked_to_previous:      # definition lives on an earlier section
                continue
            for rid, kind in image_refs(hdr.part):
                part = hdr.part.related_parts[rid]
                print(si, name, kind, rid, part.partname,
                      part.content_type, len(part.blob))
    ```

    Notes:
    - `is_linked_to_previous == True` means this header is empty and inherits — walk back to the first section whose header is *not* linked; that's the one holding the logo.
    - Check `section.different_first_page_header_footer` and `doc.settings.odd_and_even_pages_header_footer` so you don't miss an even-page or first-page variant.
    - If nothing prints, fall back to the body with `doc.element.body.xpath(".//a:blip | .//v:imagedata")` and look at the parent of each `a:blip` — `wp:anchor` vs `wp:inline` tells you it's a floating body image.
    - If you see two refs to the same `rId` under `mc:Choice` and `mc:Fallback`, that's one image described twice; replace both references or (simpler) replace the shared image part.

    ## Replacing it

    **Option A — swap the image part's bytes in place.** Best when the new logo is the same format and should keep the same frame/position. The `rId`, the `wp:extent` size, and the layout all stay untouched, and because every header references the same part, all pages update at once.

    ```python
    with open("new_logo.png", "rb") as f:
        new_bytes = f.read()

    part = hdr.part.related_parts[rid]
    assert part.content_type == "image/png"      # must match, or Word will complain
    part._blob = new_bytes                        # python-docx exposes blob read-only
    doc.save("out.docx")
    ```

    Keep the format identical (png→png, jpeg→jpeg). Changing png→jpg under the same part leaves a stale `Content-Type` in `[Content_Types].xml` and Word may refuse the file.

    **Option B — add a new image and re-point the relationship.** Cleaner when the format changes:

    ```python
    new_rId, _image = hdr.part.get_or_add_image("new_logo.jpg")  # StoryPart helper
    for blip in hdr.part.element.xpath(".//a:blip"):
        if blip.get(qn("r:embed")) == rid:
            blip.set(qn("r:embed"), new_rId)
    # VML counterpart, if present:
    for vml in hdr.part.element.xpath(".//v:imagedata"):
        if vml.get(qn("r:id")) == rid:
            vml.set(qn("r:id"), new_rId)
    doc.save("out.docx")
    ```

    **Option C — brute force at the zip level.** `word/media/image1.png` etc. Unzip, overwrite the media file with the new bytes under the *same filename and extension*, rezip. Dead simple and fixes every reference at once; breaks if the aspect ratio changes or if you swap formats.

    ## Gotchas

    - **Aspect ratio.** The drawing carries explicit `wp:extent cx="…" cy="…"` (and `a:ext` inside `a:xfrm`) in EMU. Replace bytes with a differently-shaped logo and Word stretches it. Recompute from the new image's pixel size and write both `cx`/`cy` and the inner `a:ext`, preserving the on-page width.
    - **Anchored logos** carry `<wp:anchor>` with `positionH`/`positionV`; if you rebuild the drawing rather than patch it, you lose the anchor offsets and it jumps into the text flow.
    - **`r:link` not `r:embed`** means the image is *linked*, not embedded — the bytes aren't in the package at all; you have to fix the external target or embed the file.
    - **Section-scoped headers**: with `odd_and_even_pages_header_footer` on, the running header is `even_page_header` on even pages. Half the pages will keep the old logo if you only patch `header`.
    - **Saving**: always `doc.save(new_path)`; the image part is only serialized into the package on save.

    Assumption: python-docx (0.8.11+/1.x). Everything above uses only public API plus the `_element`/`_blob` internals that image work in python-docx requires.

    ---

    ## Recap

    **ASKED** — Where a logo that appears on every page lives, given `doc.inline_shapes` doesn't list it, and how to find and replace it.

    **FOUND** — `inline_shapes` only scans the body of `document.xml` for `wp:inline`. Header stories are separate parts, and `wp:anchor`/VML images are skipped entirely — three independent reasons a logo can be invisible to it.

    **DECIDED** — It's in a header part; reach it via `doc.sections[*].header` (plus first-page/even-page variants), pull `rId` from `a:blip`/`v:imagedata`, resolve through `header.part.related_parts`. Replace by swapping `_blob` (same format) or re-pointing `r:embed` at a new image part. Mentioned the zip-level override as a fallback. BOTTOM LINE: patch the header part, not the body.

    **NEXT** — Two things to verify against the real file: whether the header is `is_linked_to_previous` (edit the owning section, not section 0), and whether the new logo's aspect ratio matches the existing `wp:extent` — otherwise Word stretches it.
- id: docx-imgins-01
  answer: |
    ## Where `doc.add_picture()` puts it

    `Document.add_picture(path, width=None, height=None)` **does not take a position**. It creates a *new paragraph* and appends it at the **end of the document body** — not at the cursor, not after any particular paragraph. Internally it's roughly:

    ```python
    self._body.add_inline(inline)   # -> body.add_p() + run.add_inline(...)
    ```

    `CT_Body.add_p()` inserts the new `<w:p>` before the trailing `<w:sectPr>` (if any), so the picture lands at the very end of the last section's content. The result is an **inline** (not floating/anchored) `InlineShape` that flows with the text of whatever paragraph it lives in.

    So a call like `doc.add_picture("signature.png", width=Inches(1.5))` will drop the signature at the bottom of the document, nowhere near "Approved by:".

    ## How to place it correctly

    `Paragraph` has no `add_picture()` method. The supported API is `Run.add_picture()` — you build the paragraph/run you want, then move that paragraph's XML element into position:

    ```python
    from docx import Document
    from docx.shared import Inches

    doc = Document("in.docx")

    target = next(p for p in doc.paragraphs if p.text.strip() == "Approved by:")

    # 1. Build the paragraph+run and put the picture in it
    new_p = doc.add_paragraph()                 # temporarily at end of body
    run = new_p.add_run()
    run.add_picture("signature.png", width=Inches(1.5))   # width only -> aspect ratio kept

    # 2. Move that <w:p> to immediately after the target paragraph
    target._p.addnext(new_p._p)                 # lxml addnext()
    ```

    Notes and variants:

    - **Same line vs. new line.** If you want the image inline on the *same* line as "Approved by:" (e.g. `Approved by: [sig]`), skip the move and just do `run = target.add_run(); run.add_picture(...)`. Add `run.add_break()` first if you want it on the next line within the same paragraph.
    - **Moving the picture instead of the paragraph.** `add_picture()` returns the `InlineShape`; its `_inline` element sits under `<w:r>/<w:drawing>`, so you'd have to walk up to the paragraph anyway. Building the paragraph explicitly (above) is cleaner.
    - **Insert before the *following* paragraph.** `next_p.insert_paragraph_before()` works too, but requires reliably finding the next paragraph; `addnext` on the target is more robust.
    - **Aspect ratio.** Pass **only** `width`. python-docx computes the height from the image's native pixel size and DPI, so scaling is proportional. Supplying both `width` and `height` always stretches/distorts, even if you try to compute the ratio yourself from `shape.width/shape.height`.
    - **Tables/headers.** If "Approved by:" lives in a table cell, use `cell.paragraphs` and the same `target._p.addnext(...)` move; if it's in a header/footer, work off `section.header.paragraphs` (or `header.add_paragraph()` + move). The body-end behavior of `Document.add_picture()` only applies to the main document.
    - **Multiple sections.** Because `add_p()` inserts before the final `sectPr`, an appended picture stays in the body of the last section; moving it into an earlier section is fine but check that section's page setup if it matters.

    ## Assumptions

    I assumed standard python-docx (0.8.x–1.1.x), that "Approved by:" is a top-level body paragraph found via `doc.paragraphs`, and that the target paragraph is the exact marker line. If it's a cell/header or has trailing whitespace, adjust the lookup accordingly.
- id: docx-imgins-02
  answer: |
    Short answer: nothing auto-fits it. The picture keeps its 3-inch extent and breaks the cell. You have to set the width explicitly, and the number you want is the cell width minus its left/right padding — not 1.2".

    ## What actually happens

    In DOCX an inline picture's displayed size lives in the drawing's `<wp:extent cx="…" cy="…"/>` (EMUs, 914400 per inch). That value is independent of the table cell. Cell width and picture width are two unrelated properties, so Word does not scale the image down to the cell — there is no "fit to cell" rule for inline images.

    Consequences depend on the table layout:

    - **Fixed layout (`<w:tblLayout w:type="fixed"/>`, fixed `tcW`)** — the column stays 1.2", the row grows to the image's scaled height, and the picture overflows the cell: it paints over the neighbouring cell or past the table edge. If the row height is set to "Exactly," it gets clipped vertically instead. Either way the layout is broken.
    - **AutoFit table** — Word may recompute column widths from content, so the column can stretch toward the 3", pushing the table past the text margin. This is the classic "image blew out my table" bug. It is also not reliable, so never depend on it.

    Floating/anchored images are worse: they ignore the cell flow entirely and float over the table. Keep table images inline.

    ## How to size it

    Compute the usable width first:

    ```
    usable = cell_width − left_margin − right_margin
    ```

    Word's default cell margins are 0.08" each side (≈108 twips), so for a 1.2" cell:

    ```
    1.2 − 0.08 − 0.08 = 1.04"
    ```

    Use ~1.0" for a safety gap, or 1.04" if you want it flush. Then supply **only the width** and let the height scale from the intrinsic aspect ratio — never set width and height to arbitrary numbers or you'll squash it.

    python-docx, inserting:

    ```python
    from docx.shared import Inches

    run = cell.paragraphs[0].add_run()
    run.add_picture("logo.png", width=Inches(1.0))   # height auto-scales
    ```

    python-docx, resizing an already-inserted picture (shape.width/height are independent, so compute the height yourself):

    ```python
    shape = doc.inline_shapes[-1]
    ratio = shape.height / shape.width          # native aspect ratio
    shape.width = Inches(1.0)
    shape.height = int(Inches(1.0) * ratio)
    ```

    Then lock the table so it cannot grow:

    ```python
    table.autofit = False
    cell.width = Inches(1.2)
    # plus a fixed tblLayout in tblPr if your generator doesn't already set it
    ```

    Templating engines (docxtpl etc.) take the same idea — pass the width with the image object, e.g. `InlineImage(tpl, path, width=Mm(25))`, rather than dropping in a raw 3" picture.

    Two extra notes: DPI metadata is irrelevant once `wp:extent` is set — a 300-dpi 3" image is still 3" on the page until you override the extent, and overriding it doesn't resample the bitmap (so it just gets downscaled at render time, which is fine for print). And if you reduce or zero the cell margins, recompute `usable` accordingly; hard-coding 1.0" is only safe while the margins stay near default.

    **Assumptions:** inline (not floating) picture, Word's default 0.08" left/right cell margins, and a 1.2" fixed cell width. If your template sets custom `tcMar`, substitute those values into the formula.
- id: docx-legacy-01
  answer: |
    **Short answer: No.** `python-docx` cannot open `report.doc`. It only understands Office Open XML (`PK`-zipped `.docx`/`.docm`). A Word 97–2003 file is an OLE2/CFB binary (magic bytes `D0 CF 11 E0`), so `Document("report.doc")` raises `PackageNotFoundError` / "not a Word file, content type is ..." — and renaming it to `.docx` does not help, because the contents, not the extension, are what matters.

    **Two things to verify first**

    1. Confirm it is really legacy binary: `file report.doc`, or check the first 8 bytes. `D0 CF 11 E0 A1 B1 1A E1` = OLE2 (true `.doc`). `50 4B 03 04` = it is actually a ZIP/`.docx` with the wrong name. If it is RTF, detect that too — different path.
    2. Check whether it is encrypted/password-protected; conversion tools will fail or prompt and hang headless.

    **Workflow**

    1. **Convert to `.docx` (mandatory step).** LibreOffice headless is the reliable, cross-platform choice:

       ```
       soffice --headless --convert-to docx --outdir out/ report.doc
       ```

       (Also `unoconv`, or `antiword`/`catdoc` if you only need text — but those give no document object model, so you cannot delete rows.)

       On Windows with Word installed, COM automation (`Documents.Open` + `SaveAs2` with `wdFormatXMLDocument`) preserves fidelity better. If the task is one-off and fidelity matters, that is the safer conversion.

    2. **Open the converted file with python-docx** and locate the target table/row. Do not assume `doc.tables[0]`; enumerate and match on content or position, and account for nested tables (they are not in `doc.tables`), merged cells, and header rows.

    3. **Delete the row — there is no public `delete_row()` API.** Use the XML layer:

       ```python
       from docx import Document
       doc = Document("out/report.docx")
       table = doc.tables[0]
       row = table.rows[3]
       row._tr.getparent().remove(row._tr)   # or: table._tbl.remove(row._tr)
       doc.save("out/report_edited.docx")
       ```

       Guard it: check `len(table.rows)`, handle merged cells (a vertically merged row's `_tr` removal can leave an inconsistent `vMerge` grid), and be aware this bypasses any public API guarantee, so pin your python-docx version.

    4. **Convert back if the user needs `.doc`:**

       ```
       soffice --headless --convert-to doc --outdir final/ out/report_edited.docx
       ```

       Only do this if the legacy format is a hard requirement — otherwise deliver `.docx`.

    **Caveats worth stating to the user**

    - Conversion is lossy in ways python-docx cannot repair: tracked changes, comments, embedded OLE objects, custom fields, and some styles/headers may shift. Review the round-trip output before handing it back.
    - `.doc` is a binary all-or-nothing format; there is no safe surgical in-place edit with python-docx, so the convert → edit → (convert back) cycle is the only sane route. Direct binary byte-patching of the CFB stream is not advisable.
    - If the run must be fully automated, LibreOffice is the only dependable headless converter; the Word COM route requires a Windows host with Office.

    **Bottom line:** treat `.doc` as a format conversion problem, not a python-docx problem — convert with LibreOffice (or Word COM), then delete the row via `table._tbl.remove(row._tr)`, then convert back only if the legacy format is genuinely required.
- id: docx-legacy-02
  answer: |
    **Short answer:** `textutil` is not a Word-compatible converter — it round-trips through macOS's TextEdit/NSAttributedString engine. For a legacy `.doc` it will often *look* fine while silently destroying table structure: tables flattened into tab/space-separated paragraphs, merged cells split or dropped, `gridSpan`/`vMerge` lost, column widths and borders reset, nested tables and header-row repeats removed, and sometimes rows/cells dropped or reordered. For a table-editing task that means you may edit the wrong cell map, write content into the wrong cells, or "successfully" produce a docx with no real tables at all.

    The dangerous part is that it usually exits 0 and writes a plausible file, so you get no error — just a lossy result.

    **How to detect it**

    Treat conversion as untrusted and validate structurally before editing:

    1. **Check exit status + stderr and file sanity** — non-zero exit, `textutil:` warnings, or an implausibly small/large `.docx` means abort.
    2. **Verify real table markup exists.** Unzip and count:
       - `<w:tbl>`, `<w:tr>`, `<w:tc>` in `word/document.xml`
       - `w:gridSpan` and `w:vMerge` (merges)
       - `w:tblGrid`/`w:gridCol` (column widths)
       - any `w:tbl` nested inside `w:tc` (nested tables)
    3. **Load with python-docx and assert the expected shape:** `len(doc.tables)` equals the count you expect from the source, plus per-table row/column counts and cell text. Zero tables, or a table with 1 column and many rows where the original had N columns, is the classic flattening signature.
    4. **Text-level diff against the original.** Extract text with `textutil -convert txt` (or `antiword`/`catdoc`) from the source and from the converted docx; confirm every expected cell string is present and in the right order. Cell text surviving in plain-text order while table structure collapses is exactly the failure mode to catch.
    5. **Spot-check high-risk features** explicitly: merged cells, spanned headers, nested tables, footnotes/endnotes, field codes, images, and numbers/dates. If the task depends on any of them and they're absent, don't proceed.
    6. **Round-trip / cross-check where possible:** convert the same source to HTML (`textutil -convert html`) and see whether `<table>` survives there; if HTML shows tables but docx has none, the docx path is lossy for this file.

    **Safer fallback if any check fails:** don't edit the textutil output. Get LibreOffice headless (`soffice --headless --convert-to docx`), do the conversion on a machine that has it, or ask for a native `.docx`. `antiword`/`catdoc` are fine for *reading* text but give you no table structure, so they don't rescue a table-editing task.

    **Assumption I'm making:** the editing step consumes the docx structurally (e.g. python-docx or raw OOXML), so lost table markup is fatal rather than cosmetic.
- id: docx-legacy-03
  answer: |
    ## Why it exits immediately

    LibreOffice is single‑instance **per user profile**. Every `soffice` process looks in a fixed profile directory (`~/.config/libreoffice/4` on Linux, `~/Library/Application Support/LibreOffice/4` on macOS). If a process is already using that profile — your open GUI window — the new `soffice` invocation does not start a second office. It hands its command line to the running instance over the profile's IPC socket and exits. The GUI instance is busy being interactive and, in practice, drops or ignores the `--headless --convert-to` request, so:

    - `soffice` returns almost instantly,
    - usually with **exit status 0**,
    - and **no output file** is written.

    So the failure is not "headless is broken" — it is the profile lock/single‑instance forwarding. Exit code 0 also means you cannot trust `&&` to tell you it worked.

    ## How to run it reliably

    Give each invocation its **own throwaway profile** via `-env:UserInstallation`, so it cannot join the running GUI instance. Then verify the artifact yourself, because soffice's exit status is unreliable.

    ```bash
    #!/usr/bin/env bash
    set -euo pipefail

    src="$(cd "$(dirname "$1")" && pwd)/$(basename "$1")"
    outdir="$(mktemp -d)"
    profile="$(mktemp -d)"
    trap 'rm -rf "$outdir" "$profile"' EXIT

    # -env: must point at an absolute dir as a file:// URL (three slashes)
    SAL_USE_VCLPLUGIN=svp \
    soffice \
      "-env:UserInstallation=file://${profile}" \
      --headless --norestore --invisible --nodefault --nofirststartwizard \
      --convert-to 'docx:MS Word 2007 XML' \
      --outdir "$outdir" \
      "$src"

    out="$outdir/$(basename "${src%.*}").docx"
    [ -s "$out" ] || { echo "conversion failed: no output for $src" >&2; exit 1; }

    rm -f "./$(basename "$out")"
    mv "$out" .
    ```

    Key points:

    - **`-env:UserInstallation=file:///abs/path`** is the actual fix. Use a fresh directory per run (a PID or `mktemp`), otherwise parallel runs collide with each other instead of with the GUI.
    - **`--outdir` must exist** and should be a separate temp directory. Converting in place can silently no‑op or clash with an existing `file.docx`; remove the target first if you do convert in place.
    - **Pass absolute paths**, quote everything, and avoid relying on the input's directory.
    - **Force the filter** with `--convert-to 'docx:MS Word 2007 XML'` when you need deterministic output.
    - **`SAL_USE_VCLPLUGIN=svp`** keeps it truly headless without needing an X display.
    - **Wait for the process and check the file.** `soffice` normally blocks until conversion finishes when it owns its profile, but the exit code can still be 0 on failure, so `[ -s "$out" ]` is the real test.
    - Wrap it in a timeout (`timeout 120 …`, `gtimeout` on macOS) and treat 124 as failure — first run can be slow while the profile is created, and a corrupt document can hang it.
    - If a stale `.~lock.file.doc#` exists, add `--nolockcheck`; with a private profile you usually won't need it.
    - On macOS call the real binary: `/Applications/LibreOffice.app/Contents/MacOS/soffice`. In cron/systemd, also be aware `HOME` differences cause the same profile‑resolution surprises — the explicit `-env:` avoids that too.

    If you genuinely need to share one office process, don't shell out per file; start it once with `--accept=socket,host=127.0.0.1,port=2002;urp;` and drive it over UNO (e.g. `unoconv`). For scripted batch conversion, the isolated‑profile recipe above is simpler and more robust.

    **BOTTOM LINE:** The running GUI owns the shared LibreOffice profile, so your second `soffice` forwards the request and exits 0 without converting. Run with a unique `-env:UserInstallation=file:///tmp/...` profile per invocation, then assert the output file exists rather than trusting the exit code.
- id: docx-verify-01
  answer: |
    Assuming `invoice.docx` is a normal OOXML package (a ZIP of XML parts) and your script edits it programmatically, the safe pattern is **copy → surgical edit → verify at three levels → atomic replace**. Never edit the original in place, and never trust "it opened once" as verification.

    ## 1. Before writing

    - **Close Word/LibreOffice** and remove any stale lock file (`~$invoice.docx`). Word holding the file will silently clobber your write.
    - **Snapshot the original**: copy to `invoice.docx.bak.<timestamp>` (or commit it). Keep it until verification passes.
    - **Work on a temp path** in the *same directory* (same filesystem, so the final rename is atomic): `invoice.docx.tmp`.
    - Prefer **surgical part replacement** over rebuilding the document: read the original archive, copy every entry byte-for-byte (same name, order, compression), and swap only the parts you intend to change (usually `word/document.xml`). A from-scratch re-zip is the #1 cause of dropped media, headers, styles, and custom XML.

    ## 2. Preserve package structure while writing

    - Keep `[Content_Types].xml` as the **first entry** (OPC requires it); do not store an ODF-style `mimetype` entry — that rule does not apply to `.docx`.
    - Preserve **all** parts: `word/styles.xml`, `numbering.xml`, `settings.xml`, `fontTable.xml`, headers/footers, `word/media/*`, `word/_rels/*`, `docProps/*`, `customXml/*`, comments, footnotes.
    - Keep entry names, casing, and the deflate method unchanged; no directory-entry weirdness, no duplicate names, no encryption.
    - Write valid XML: proper escaping (`&amp;`, `&lt;`), valid namespace prefixes (`w:`, `r:`, `wp:`), no illegal control characters, no NUL bytes, encoding declaration matching actual bytes.

    ## 3. Verify — level 1: the container

    - `unzip -t invoice.docx.tmp` → all CRCs OK, no truncated entries.
    - Confirm entry count and the list of part names match the original exactly (added/removed parts must be intentional).
    - Check checksums of every part you did **not** intend to touch — media, styles, rels, headers should be byte-identical.
    - Sanity-check file size: a small delta is expected; a large drop means parts were lost.

    ## 4. Verify — level 2: the XML and relationships

    - Parse **every** `.xml` and `.rels` part with a real XML parser (lxml/ElementTree), not regex or string search. This catches unclosed tags, bad entities, and stray characters.
    - **Relationship integrity**: every `r:id`/`r:embed` referenced in `document.xml` exists in `word/_rels/document.xml.rels`, and there are no dangling or duplicate relationship IDs.
    - **Content types**: every part extension has a `Default` or `Override` entry.
    - **Schema-level structure**: `w:body` ends with a single `w:sectPr`; every `w:tbl` has `w:tblGrid` and `w:tr`/`w:tc`; no duplicate `w:id` on bookmarks/comments/footnotes; existing field codes (`w:fldSimple`, `w:instrText`) and tracked changes still parse.
    - Re-open with `python-docx` and iterate paragraphs/tables — confirms the high-level model still loads, which a bare XML parse does not.

    ## 5. Verify — level 3: does it actually render and still say the right thing?

    - Convert headlessly (`soffice --headless --convert-to pdf invoice.docx.tmp`) and confirm it succeeds and page count looks right.
    - Extract text and assert on **business content**: invoice number, customer, line items, totals, dates, tax — the values you expected to change changed, and everything else is unchanged.
    - Diff the old vs. new extracted text; the diff should contain only your intended edit.
    - Ideally open it once in real Word (or the target app) and save a round-trip copy without a repair prompt.

    ## 6. Replace atomically, then keep the safety net

    - Only after all checks pass: `os.replace("invoice.docx.tmp", "invoice.docx")` (same directory → atomic on POSIX; on Windows use `ReplaceFile`/`MoveFileEx`), optionally `fsync` the file and directory.
    - Keep `invoice.docx.bak` at least until the document has been opened and confirmed by a human or a downstream step; delete the temp file on any failure and restore the backup.
    - Delete the lock file check again — a `~$` file left behind signals Word still has it open.

    ## Failure signals that mean "do not ship"

    Repair prompt on open, "unreadable content" errors, missing images, broken numbering/bullets, lost headers, changed fonts, text reflow across pages, or a document that opens but shows stale field values. Any of these → restore the backup, fix the writer, repeat.

    **Assumption stated:** the edit is a content/metadata change to a standards-compliant `.docx`, done by a script with ZIP + XML access (e.g., Python `zipfile` + `lxml`/`python-docx`), and the target application is Word or LibreOffice. If the file is actually `.doc` (binary) or password-protected, the procedure changes — those need the native library or the app itself, not ZIP surgery.
- id: docx-verify-02
  answer: |
    **Assumption:** the file is well‑formed XML (lxml/`python-docx` parsed it), so the failure is *schema validity* or *semantic consistency*, not broken syntax. `python-docx` only walks the tree by qualified name — it never validates element order, required children, or attribute values, and it will happily re‑serialize an invalid tree. Word validates against the WordprocessingML schema plus its own semantic rules and throws "unreadable content" when anything fails.

    ## 1. Sequence/order violations (most common)
    WordprocessingML uses `xsd:sequence` in many places, so order is mandatory even though `python-docx` looks elements up by tag:

    - **`w:tbl` children**: must be `w:tblPr`, then `w:tblGrid`, then rows (`w:tr`/`w:sdt`/`w:customXml`). Inserting a `w:tblPr`, `w:tblGrid`, or `w:tblCaption` after rows, or a row before `w:tblGrid`, is a classic corrupt‑but‑parseable edit.
    - **`w:tr` children**: `w:tblPrEx` (if present) must come **first**, then `w:trPr`, then cells. Putting `w:trPr` after the first `w:tc`, or `w:tblPrEx` anywhere but first, breaks it.
    - **`w:tc` children**: `w:tcPr` must be the **first** child; everything after it must be block‑level content (`w:p`, nested `w:tbl`, `w:sdt`).
    - **Inside `w:tblPr`**: roughly `tblStyle → tblpPr → tblOverlap → bidiVisual → tblStyleRowBandSize/ColBandSize → tblW → jc → tblCellSpacing → tblInd → tblBorders → shd → tblLayout → tblCellMar → tblLook → tblCaption → tblDescription`. Hand‑adding `w:tblW` before `w:tblStyle`, or `w:tblLook` before `w:tblBorders`, is a frequent repair trigger.
    - **Inside `w:tcPr`**: `tcW` before `gridSpan`/`hMerge`/`vMerge`, which come before `tcBorders`/`shd`/`noWrap`/`tcMar`/`textDirection`/`tcFitText`/`vAlign`. Ordering `vAlign` before `tcW` is a common slip.
    - **Inside `w:tcBorders`/`w:tblBorders`**: fixed order (top, left/start, bottom, right/end, insideH, insideV, tl2br, tr2bl) and **no duplicate sides** — two `<w:top/>` entries or reversed order fails validation.

    ## 2. Missing required children
    - **A `w:tc` with no `w:p`.** Every cell must contain at least one block‑level element, and a cell must **end** with a paragraph. Deleting the last paragraph while "cleaning up" leaves `<w:tc><w:tcPr/></w:tc>` — this one alone is a very common cause.
    - **A nested `w:tbl` as the last thing in a cell** with no trailing `w:p` after it.
    - **A `w:tr` with zero `w:tc`**, or a `w:tbl` with **no `w:tblGrid`** / an **empty `<w:tblGrid/>`** (`gridCol` is required, minOccurs 1).
    - **`w:gridCol` without `w:w`**, or `w:tblW`/`w:tcW` missing `w:w` and `w:type`.
    - **`w:p` with no children** is fine, but `w:r` with no `w:t`/`w:drawing` etc. is tolerated; don't confuse that with the cell‑paragraph rule.

    ## 3. Grid and merge inconsistency
    - **`w:gridCol` count ≠ the logical cells per row.** Cells per row + `w:gridBefore` + `w:gridAfter`, after expanding `w:gridSpan`, must equal the `tblGrid` column count. Removing a `gridCol` while cells still span it is the canonical break.
    - **`w:gridSpan` larger than the remaining grid columns**, or spans in a row not summing to the grid width.
    - **`w:vMerge` bookkeeping**: a `continue` cell with no `restart` above it, a `restart` with no matching `continue`, or a vertical merge group whose rows have **different `gridSpan`**.
    - **Legacy `w:hMerge` mixed with `w:gridSpan`** — deprecated horizontal merge must agree with spans across the row; inconsistent mixes get repaired/rejected.
    - Word's hard limit: **more than 63 columns** (gridCols) is rejected outright.

    ## 4. Wrong nesting / misplaced content
    - A **`w:tbl` directly inside another `w:tbl`** (sibling of `w:tr` instead of inside a `w:tc`), or a `w:tbl` inside a `w:p`.
    - A **`w:p` directly inside `w:tr`** (outside any cell) — happens when paragraphs are moved between cell and row level.
    - **Raw text nodes** or bare `w:r` elements placed directly in `w:tbl`/`w:tr`/`w:tc` instead of inside `w:p`. lxml keeps them; Word rejects.
    - **`w:sectPr` in the wrong place** — it belongs in `w:p/w:pPr/w:sectPr` (or `w:body`), never in `w:tblPr` or `w:tcPr`, and never duplicated.
    - **Content controls**: `w:sdt` without `w:sdtContent`, empty `w:sdtContent`, `w:sdtPr` not first, or an `w:sdt` that straddles row/cell boundaries (contains a `w:p` where only rows are allowed, or wraps half a row).

    ## 5. Duplicate singleton elements
    Two `w:tblPr`, two `w:tblGrid`, two `w:trPr` in one row, two `w:tcPr` in one cell, or repeated `w:tcW`/`w:gridSpan`/`w:vMerge` — typically from a copy‑paste or a `etree.SubElement` that didn't check for an existing child. All are maxOccurs=1 and fail validation.

    ## 6. Invalid attribute values
    - **Wrong enum**: `w:vAlign w:val="middle"` (valid: `top|center|bottom|both`), `w:vMerge w:val="true"` (valid: `restart|continue`), `w:jc w:val="justify"` on a table (table `jc` takes `start|end|center|left|right`), `w:tblLayout w:val="fixed"` is fine but `"true"` is not.
    - **Bad numbers**: `w:gridSpan w:val="0"` or negative, non‑numeric `w:w` on `gridCol`, negative `w:trHeight`.
    - **Percentages**: `w:type="pct"` widths are in **fiftieths of a percent** (5000 = 100%); values out of range and `w:type="pct"` paired with a stray `w:w` unit mix get rejected.
    - Missing `w:type` on `tblW`/`tcW` where the schema requires it.

    ## 7. Revision/markup bookkeeping
    - **`w:ins`/`w:del` wrapping a whole `w:tr`** — row insertion/deletion must be recorded as `w:trPr/w:ins` / `w:trPr/w:del`, not by wrapping the row.
    - **Unbalanced pairs after row deletion**: `w:bookmarkStart` without its `w:bookmarkEnd`, `w:commentRangeStart/End`, `w:proofErr`, or a `w:ins` opened in one row and closed in another. Word treats dangling range markup as corruption.
    - `w:cellIns`/`w:cellDel`/`w:cellMerge` placed in the wrong position within `w:tcPr`.

    ## 8. Namespace and declaration problems
    - Dropping or rebinding `xmlns:w`, leaving elements in **no namespace** (or the wrong one) so `w:tcW` isn't actually WordprocessingML — `python-docx` may still find it via its `nsmap`, Word won't.
    - Emitting `w14:`/`w15:`/`mc:` prefixed attributes without a matching declaration, or leaving `mc:Ignorable="w14 w15"` listing prefixes that aren't bound.

    ## Fast triage
    1. `unzip -o file.docx` — `xmllint --noout word/document.xml` only proves well‑formedness; it will pass on all of the above.
    2. Validate against `wml.xsd` (or diff the edited `<w:tbl>` against a Word‑authored table) to catch order/required‑child errors.
    3. Bisect: delete rows/cells from the suspect table in a copy until the error clears — the last removed node is usually the culprit (most often an empty `w:tc`, a stray `w:tblPr`, or a `gridSpan` exceeding the grid).

    **Uncertainty note:** the broad categories (order, missing `w:p` in a cell, grid/merge consistency, duplicate singletons, invalid enums, unbalanced revision markup, namespace loss) I'm confident about. The exact `tblPr`/`tcPr` child orderings above are from memory of the schema, not re‑checked against `wml.xsd`, so treat the precise sequence as approximate — the *direction* (properties before content, width before span before merge before borders/alignment) is right.
- id: docx-verify-03
  answer: |
    Short answer: removing the `w:drawing` only removes the *reference* in `document.xml`. The image bytes still live as a separate part in the OOXML package (`word/media/imageN.ext`), wired in through a relationship in `word/_rels/document.xml.rels`. So the only reliable check is to open the `.docx` as a ZIP and verify the package itself, not the visible document.

    Assumptions I'm making: it's a real `.docx` (OOXML, not legacy `.doc`), the picture is DrawingML (`w:drawing`/`a:blip r:embed`), and the old logo's bytes or a hash of them are available for comparison.

    **The check**

    1. Treat the file as a ZIP: copy it to `x.zip`, `unzip -l` (or `zipinfo`) to list every part. Don't rely on Word's UI.
    2. Hash every part and compare with the old logo's SHA-256. Any match in `word/media/` means the old image is still shipped. If you don't have the original hash, extract each `word/media/*` file and visually/dimensionally match — the logo is often still there under a different `imageN` name.
    3. Audit relationships, not just media. Read `word/_rels/document.xml.rels` and every other `.rels` (`header*.xml.rels`, `footer*.xml.rels`, `footnotes.xml.rels`, `endnotes.xml.rels`, `comments.xml.rels`, `glossary/document.xml.rels`). An `image` relationship whose `Target` points at a media part that no `r:embed`/`r:id`/`r:link` in the corresponding XML references is an orphan — still embedded, still extractable.
    4. Grep the XML parts for every reference form: `r:embed`, `r:id`, `r:link`, `v:imagedata`, `w:pict`, `w:drawing`, `a:blip`, `oleObject`, `w:object`. Confirm all IDs resolve and none point to the old media.
    5. Check the places people forget:
       - `docProps/thumbnail.jpeg` — Word frequently stores a document preview, which can show the old logo.
       - Tracked changes: a deleted picture can survive inside `w:del` (and the media part survives with it). Accept/reject all revisions, then re-check.
       - Headers, footers, footnotes, endnotes, comments, text boxes, legacy VML shapes, SmartArt (`word/diagrams/`), charts (`word/charts/`), and `word/embeddings/`.
       - Alt text and `wp:docPr name/descr` fields, plus `customXml/`, which can name the logo even without the image.
    6. Check `[Content_Types].xml`: if there's a per-part `Override` for the removed image, clean it up; the extension `Default` can stay.

    **The fix, if it's still there**

    Delete both the media part and its `Relationship` element in the owning part's `.rels`. Removing only the drawing (or only the media file) leaves a dangling relationship — Word may complain about unreadable content, or the bytes remain. Then re-zip, re-extract, and re-hash. Finally open in Word to confirm no repair prompt and that the new picture renders, and re-verify the hashes after that save — Word can rewrite the package and re-add parts (notably the thumbnail).

    **Caveat:** Word's "Inspect Document" and a plain Save As are not proof. Word often keeps unreferenced media parts, and the inspector doesn't guarantee removal of orphaned parts. Byte-level inspection of the ZIP is the authoritative test.

    If the old logo's original bytes/hash are unavailable, I can't claim certainty — you'd be reduced to visual matching of every media part, which is weaker evidence.
```
