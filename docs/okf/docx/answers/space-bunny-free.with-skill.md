- id: docx-model-01
  answer: "A .docx is a ZIP archive (an OPC / Open Packaging Conventions package) of\nXML parts plus binary\
    \ parts. The ZIP normally contains\n`[Content_Types].xml`, `_rels/.rels`, and under `word/`: `document.xml`\n\
    (the body), `styles.xml`, `settings.xml`, `numbering.xml`, `fontTable.xml`,\n`word/media/*` (images),\
    \ `word/_rels/document.xml.rels`, and possibly\n`word/header*.xml` / `word/footer*.xml`, `docProps/core.xml`,\n\
    `docProps/app.xml`. So a table's data lives as XML element text inside\n`word/document.xml` (or a\
    \ header/footer part) — there is no separate\nspreadsheet, and nothing is stored in the binary image\
    \ parts.\n\nThe WordprocessingML element tree for a table, top down:\n\n- `w:tbl` — the table itself\
    \ (child of `w:body` or of a `w:tc` for a nested table).\n- `w:tblPr` — table properties: `w:tblStyle`,\
    \ `w:tblW`, `w:jc`,\n  `w:tblInd`, `w:tblBorders`, `w:shd`, `w:tblLayout`, `w:tblCellMar`,\n  `w:tblLook`.\n\
    - `w:tblGrid` — the column grid; holds one `w:gridCol` per grid column,\n  each with a `w:w` width.\
    \ Exactly one `w:tblGrid` per table, immediately\n  after `w:tblPr`.\n- `w:tr` — a table row (optional\
    \ `w:trPr` for height, `w:tblHeader`,\n  `w:cantSplit`).\n- `w:tc` — a table cell (optional `w:tcPr`:\
    \ `w:tcW`, `w:gridSpan`,\n  `w:vMerge`, `w:shd`, `w:vAlign`, `w:tcBorders`, `w:tcMar`).\n- `w:p` —\
    \ a paragraph (inside a cell there must be at least one `w:p`);\n  `w:pPr` holds paragraph style,\
    \ alignment, spacing, numbering.\n- `w:r` — a run; `w:rPr` holds run properties (`w:b`, `w:i`, `w:sz`,\n\
    \  `w:color`, `w:rFonts`, `w:vanish`, ...).\n- `w:t` — the text node (`xml:space=\"preserve\"` when\
    \ leading/trailing\n  spaces matter). Siblings `w:tab`, `w:br`, `w:drawing`, `w:fldChar`/\n  `w:instrText`\
    \ (field codes), `w:ins`/`w:del` (tracked revisions),\n  `w:hyperlink`, `w:smartTag` also appear inside\
    \ runs or paragraphs.\n\nA cell's visible text is the concatenation of all its `w:t` descendants\n\
    (plus `w:tab`/`w:br` as characters); there is no single \"cell text\"\nattribute.\n"
- id: docx-model-02
  answer: "Because runs are not semantic text chunks. Word (and every producer: Word\nitself, python-docx,\
    \ converters, tracked revisions) splits a run wherever\nsomething changes at the *character-property*\
    \ or *revision* level, and it\nalso splits around proofing metadata (`w:proofErr`), `w:rsid` boundaries,\n\
    `w:lang`, spell-check state, bookmarks, and comments. \"Widget C\" can be\nstored as `<w:t>Widg</w:t></w:r><w:r><w:rPr>…</w:rPr><w:t>et\
    \ C</w:t></w:r>`,\nso the literal bytes \"Widget C\" never appear in the XML and\n`run.text == \"\
    Widget C\"` is never true. Also, runs nested inside\n`w:ins`, `w:del`, `w:hyperlink` or `w:smartTag`\
    \ are not returned by\n`paragraph.runs` at all, so those texts are invisible to a naive scan.\n\n\
    Reliable approach:\n\n1. Work at paragraph/cell level, not run level: compute the visible text\n \
    \  as the ordered concatenation of all `w:t` (and `w:tab`/`w:br`) in the\n   paragraph — `p.text`\
    \ in current python-docx, or explicitly\n   `''.join(r.text for r in p.iter_inner_content() if …)`,\
    \ or an XPath such\n   as `p._p.xpath('.//w:t')`. Compare with normalized whitespace\n   (`\" \".join(text.split())`)\
    \ if you need to tolerate odd spacing.\n2. To replace: locate the target by character offsets over\
    \ that\n   concatenation, map the offsets back to the specific `w:t` elements,\n   then rewrite minimally.\
    \ If the matching text lies inside one `w:t`,\n   just set `w:t.text` and set `xml:space=\"preserve\"\
    ` if needed. If it\n   spans several runs, first *assert* that the covering runs share an\n   identical\
    \ `rPr`; then put the whole new text in the first covering\n   `w:t` and blank the others (provably\
    \ lossless when `rPr` is equal), or\n   rebuild the runs with explicitly copied `rPr` if they differ.\n\
    3. Never blind `str.replace` on the raw XML: it can hit the same string in\n   a different cell, inside\
    \ `w:instrText` (a field code) or inside\n   `w:delText` (tracked deletion). Assert the hit count,\
    \ and skip\n   `w:instrText`/`w:delText` nodes.\n4. If you want a byte-minimal change, read the part\
    \ from the original zip,\n   edit with lxml, and re-zip preserving the other parts' `ZipInfo` — then\n\
    \   diff the decoded part with `difflib.SequenceMatcher` to prove nothing\n   else moved.\n"
- id: docx-model-03
  answer: "Widths and sizes use three different unit systems:\n\n- Twips (\"dxa\", twentieths of a point)\
    \ for table geometry: `w:tblW`,\n  `w:tblInd`, `w:tblGrid/w:gridCol/@w:w`, `w:tcPr/w:tcW`,\n  `w:trPr/w:trHeight`,\
    \ and `w:tblCellMar`. 1440 twips = 1 inch,\n  20 twips = 1 pt. `w:tcW/@w:type` may be `dxa`, `pct`\
    \ (in fiftieths of a\n  percent, so 5000 = 100%), `auto`, or `nil` — only `dxa` is absolute.\n- EMU\
    \ (English Metric Units) for drawing/object geometry:\n  `wp:extent/@cx,@cy` and `a:ext/@cx,@cy` in\
    \ the DrawingML inside a\n  `w:drawing`. 914400 EMU = 1 inch, 12700 EMU = 1 pt, 360000 EMU = 1 cm.\n\
    - Half-points for font size: `w:rPr/w:sz` and `w:szCs` are in half-points\n  (24 = 12 pt).\n- Also\
    \ 1/100 mm for some DrawingML attributes and raw pixel counts in the\n  image file itself.\n\nConversions:\
    \ inches = twips / 1440 = EMU / 914400; points = twips / 20 =\nEMU / 12700; twips = pt * 20; EMU =\
    \ pt * 12700.\n\npython-docx wraps EMU in `Length` (a subclass of `int`) with `.emu`,\n`.inches`,\
    \ `.pt`, `.cm`, `.mm`, `.twips`, and constructors\n`Inches()`, `Pt()`, `Twips()`, `Emu()`. Two traps:\
    \ an image's pixel\ndimensions are *only* a ratio — the correct height is\n`cy = cx * px_height //\
    \ px_width`, never `cx * px_height`; and arithmetic on\n`Length` objects returns a plain `int` (EMU),\
    \ so\n`section.page_width - section.left_margin - section.right_margin` must be\nre-wrapped with `Emu(...)`\
    \ before reading `.twips`.\n"
- id: docx-locate-01
  answer: "Finding the table: `doc.tables` gives top-level tables in body order (nested\ntables are not\
    \ included — reach them via `cell.tables` or recursion over\n`w:tbl`). Don't assume a fixed index;\
    \ score candidates and pick the best:\n\n1. Normalize the text of the first row (lowercase, collapse\
    \ whitespace,\n   strip non-alphanumerics) and compare against the expected header labels\n   (\"\
    Item\", \"Qty\", \"Unit price\", \"Amount\"). Require most labels to match.\n2. Cross-check shape:\
    \ column count == len(table.columns), a plausible row\n   count, and the presence of a Total/summary\
    \ row (cell text matching\n   \"total\"/\"subtotal\"/\"tax\"/\"amount due\").\n3. Look at context:\
    \ the nearest preceding paragraph, a caption\n   (\"Invoice\", \"Statement\"), or a heading, and any\
    \ following \"Total\" line.\n4. Prefer the table whose cells contain the expected value types (a\n\
    \   currency-formatted number, an integer quantity).\n5. Remember `w:tbl` can sit inside a `w:sdt`\
    \ (content control), a text box\n   (`w:txbxContent`), a header/footer, or a nested table — check\n\
    \   `document.xml` if `doc.tables` comes up empty.\n\nFinding the row: iterate `table.rows`, join\
    \ all `w:t` per row\n(`row.cells[i].text` or an XPath over `w:tr`), normalize, and match on the\n\
    Item column. Do not match individual runs. Require exactly one match; if\nzero or many, stop and report\
    \ rather than guessing.\n\nChecks before editing: print the target row's cell texts to confirm you\
    \ hit\nthe right one; check for `w:vMerge` / `w:gridSpan` in the table (merged\ncells break index\
    \ arithmetic and `row.cells` symmetry); check whether any\ntarget cell holds a field (`w:fldSimple`,\
    \ `w:fldChar`/`w:instrText`), a\nhyperlink, bookmark, or tracked revision (`w:ins`/`w:del`); recompute\
    \ and\nassert the printed total from the Amount column *before* touching anything\n(this catches bad\
    \ input data); note the table's style, column widths, and\nthe alternating `w:shd` banding so a new\
    \ row can match; verify the file is\nnot under `w:documentProtection`; and keep a byte copy of the\
    \ original for\nrollback.\n"
- id: docx-locate-02
  answer: "`row.cells` is indexed by *grid column*, not by `w:tc`. python-docx expands\nthe row into one\
    \ entry per `w:gridCol`, repeating the same `_Cell` object\nfor each column a cell spans. So with\
    \ a first cell having\n`<w:gridSpan w:val=\"2\"/>`, `len(row.cells)` equals the number of grid\ncolumns\
    \ (not the number of `w:tc` elements), `row.cells[0] is\nrow.cells[1]`, and the real `w:tc` list is\
    \ shorter — get that from\n`row._tr.tc_lst` (or `table.rows[i]._tr.findall(qn('w:tc')))`.\n\nWhat\
    \ goes wrong:\n\n- Indexing \"the second cell\" gives you the *merged* cell again, so writing\n  to\
    \ it overwrites the first cell's content (twice) and deleting it via\n  `row.cells[1]._tc.getparent().remove(row.cells[1]._tc)`\
    \ removes the\n  whole `w:tc` covering two grid columns, leaving the row short by one\n  grid column\
    \ — Word may then report the file as corrupt or silently\n  relayout it.\n- Iterating `row.cells`\
    \ to count columns is accidentally right for the grid\n  count, but iterating it to process each cell\
    \ processes the spanning cell\n  two or more times, duplicating text and inflating any totals.\n-\
    \ Any positional math (`cells[2].text = …`, `del cells[i]`) is unreliable\n  in a table with merges.\n\
    \nCorrect approach: work in grid space. Use each cell's\n`grid_span` and `tc.grid_offset` (the `CT_Tc`\
    \ grid offset, which accounts\nfor `w:gridBefore` and preceding `w:gridSpan`; if unavailable in your\n\
    python-docx version, compute it by walking `w:tr` and accumulating\n`w:gridBefore` plus each preceding\
    \ cell's `w:gridSpan`). Identify cells by\ngrid offset, and when you must remove a cell, either decrement\n\
    `w:gridSpan` of a neighbour or remove the `w:tc` and also drop a\n`w:gridCol`, so that in every row\n\
    `gridBefore + Σ gridSpan + gridAfter == len(table.columns)` still holds.\n"
- id: docx-rowdel-01
  answer: 'No. There is no `table.delete_row()` and no `row.delete()` in python-docx

    (neither exists in the current release). You delete the underlying XML

    element:


    ```python

    tr = table.rows[i]._tr          # the w:tr element

    tr.getparent().remove(tr)       # parent is the w:tbl

    ```


    Capture the parent before removing, because after `remove()` the element''s

    parent is `None`. `Table.rows` re-reads the XML on every access, so

    re-acquire `table.rows` after each deletion rather than caching row

    objects or indices — deleting by descending index is safest.


    Also note: deleting a `w:tr` leaves `w:tblGrid` untouched (correct — the

    grid describes columns, not rows), but it may leave a table with no rows

    having `w:tblHeader`, may break vertical merges (see the next question),

    and will not update any `{=SUM(ABOVE)}` cached results. So after deleting,

    re-verify: `len(table.rows)`, the remaining item list, the recomputed

    total, and `unzip -t` on the saved file.

    '
- id: docx-rowdel-02
  answer: "A vertical merge is a *continuation chain*, not a single tall object: the\nfirst row's `w:tc`\
    \ carries `<w:vMerge w:val=\"restart\"/>` and each following\nrow's cell in the same grid column carries\
    \ `<w:vMerge/>` (continue). The\nvisually merged cell's content lives in the restart cell; the continuation\n\
    cells' content is normally hidden/ignored.\n\nIf you remove the restart row, the remaining rows now\
    \ begin with a\n`w:vMerge` that has no `w:val=\"restart\"` — an orphaned continuation. Word\ntreats\
    \ a continuation without a preceding restart as invalid\nWordprocessingML: it commonly reports the\
    \ document as corrupt (\"Word found\nunreadable content\"), and depending on the version it may drop\
    \ the cell,\nrepair the merge, or render the first continuation row as a plain cell\nwhile still treating\
    \ later rows as continuations. The merged cell's\ncontent (which lived in the deleted row) is lost,\
    \ and the visual cell\nboundaries shift. You can also get this from the other direction: deleting\n\
    a *continuation* row is harmless, but deleting the restart row is not.\n\nCorrect procedure (lxml,\
    \ since python-docx has no merge API):\n\n1. Find the `w:tc` to be removed and check its `w:tcPr/w:vMerge`.\
    \ If there\n   is no `restart`, deleting the row is safe.\n2. If it is a `restart`, walk the following\
    \ sibling `w:tr` elements while\n   their cell at the same grid offset has `w:vMerge` (continue).\
    \ Choose:\n   - *Promote*: move the restart cell's `w:tcPr` (minus the `w:vMerge`)\n     and its paragraphs\
    \ into the first continuation row's cell, then remove\n     the now-redundant continuation `w:tc`\
    \ from the remaining rows (or set\n     their `w:vMerge` to a normal cell with its own properties).\n\
    \   - *Unmerge then delete*: for each continuation row, drop its `w:tc` (or\n     its `w:vMerge`)\
    \ so the merge is broken, then remove the `w:tr` as in\n     the previous answer.\n3. Also fix any\
    \ `w:gridSpan` bookkeeping, then assert that for every row\n   `gridBefore + Σ gridSpan + gridAfter\
    \ == len(table.columns)`.\n4. Reopen the file and confirm Word/LibreOffice does not flag it, and that\n\
    \   the neighbouring rows' shading and widths still line up (a promoted\n   `w:tcPr` must not carry\
    \ over the deleted row's `w:shd` if the table is\n   banded).\n"
- id: docx-rowdel-03
  answer: "It shows the **old, wrong total**. A `{ =SUM(ABOVE) }` is a field: the\ninstruction lives either\
    \ in `w:fldSimple/@w:instr` or in\n`w:instrText` between `w:fldChar` runs, and the *last computed\
    \ result* is\ncached as ordinary `w:t` text between the `separate` and `end` field\ncharacters. python-docx\
    \ never evaluates fields and never updates the cached\nresult, so deleting a line-item row leaves\
    \ the stale number in place. Word\ndoes not recalculate `=SUM(ABOVE)` on open by default; the total\
    \ only\nchanges when something forces a field update (Ctrl+A then F9, printing /\nprint preview, or\
    \ a document with `w:updateFields` set in\n`word/settings.xml`). On some viewers the visible value\
    \ can even differ\nagain once the field does update — for example `SUM(ABOVE)` stops at the\nfirst\
    \ non-numeric cell and can include or skip the header label.\n\nHow to handle it:\n\n1. Never hand-edit\
    \ the cached result and leave the field in place — that\n   guarantees the document is wrong the moment\
    \ a field update happens.\n2. Compute the truth yourself: read every Amount cell (its visible text,\n\
    \   minus currency symbols/separators), sum them, and compare with the\n   total the document currently\
    \ claims. Do this *before* deleting so a bad\n   input table is caught.\n3. Either (a) add `<w:updateFields\
    \ w:val=\"true\"/>` to `word/settings.xml`\n   so Word refreshes all fields on open (it prompts, and\
    \ it also refreshes\n   unrelated fields), or (b) replace the field with a literal value: drop\n \
    \  the `w:fldChar`/`w:instrText` machinery, keep a single `w:r`/`w:t` with\n   the *same* `rPr` as\
    \ the old result, and write the recomputed number.\n   (b) is deterministic and needs no Word; (a)\
    \ preserves the field for\n   future line changes.\n4. If the cell is `w:fldSimple`, editing it means\
    \ changing the\n   `w:t` inside it or replacing the whole `w:fldSimple` element; if it is a\n   complex\
    \ field, you must keep the `begin`/`separate`/`end` `w:fldChar`\n   sequence balanced or Word will\
    \ report corruption.\n5. Same reasoning applies to any other derived value in the document\n   (subtotals,\
    \ tax lines, \"amount in words\", `=PRODUCT(LEFT)` line\n   amounts): recompute or force an update,\
    \ then verify by reopening the\n   saved file and re-reading the total.\n"
- id: docx-rowdel-04
  answer: "Because none of that removes anything. Setting `w:trHeight` to 0 with\n`w:hRule=\"exact\"`\
    \ only *clips* the row (and `exact` at 0 can hide text\nwhile the row still reserves structure); white\
    \ text is a rendering\ninstruction that fails the moment the document is pasted, restyled,\nviewed\
    \ in a dark-mode or high-contrast reader, or has the text selected;\n`w:vanish` (hidden) text is *still\
    \ in the file* — it prints when \"Print\nhidden text\" is on and is fully recoverable with `strings`,\
    \ a hex dump, or\nany unzip of `word/document.xml`. In all cases:\n\n- the `w:tr`, its `w:tc`s, and\
    \ their `w:t` text remain in the XML, so\n  screen readers, search, accessibility tools, document\
    \ inspectors, and\n  any downstream text extraction still see the row;\n- a `{=SUM(ABOVE)}` field\
    \ still includes the row's amount, so the total\n  stays wrong;\n- tracked-change \"deletions\" (`w:del`\
    \ with `w:delText`) have the same\n  problem — the text is preserved on purpose;\n- the row can reappear\
    \ if the user clears formatting, changes the table\n  style, or turns on formatting marks / hidden-text\
    \ display.\n\nHow to prove a row is really gone:\n\n1. Structural: `len(table.rows)` before and after,\
    \ and the count of `w:tr`\n   elements in the saved part — both must drop by exactly one, with the\n\
    \   neighbouring row texts intact.\n2. Content: search the saved `word/document.xml` for the removed\
    \ item's\n   text and the removed amount; there must be **zero** matches (also zero\n   `w:delText`\
    \ occurrences). Do the same with `strings`/`grep` on the raw\n   `.docx` to catch anything left in\
    \ another part (headers, footers,\n   comments, `docProps`).\n3. Derived values: recompute the total\
    \ and assert it matches the value now\n   displayed in the Total cell.\n4. File integrity: `unzip\
    \ -t`, an OOXML schema/content-types/rels\n   validation, and a read-back with `python-docx` (and,\
    \ if available, a\n   LibreOffice round-trip) — the last is the only way to catch a merge\n   left\
    \ dangling, because XML validity alone will not.\n\nA genuine deletion is `tr.getparent().remove(tr)`;\
    \ if the requirement is a\n*tracked* deletion, wrap the row's runs in `w:del` (converting `w:t` to\n\
    `w:delText`) and accept that the text is then still physically present by\ndesign.\n"
- id: docx-cell-01
  answer: "`cell.text = \"SKU\"` is destructive to the paragraph/run layer. Its\nimplementation is roughly:\
    \ `tc.clear_content()` (drops every child of\n`w:tc` except `w:tcPr`), then `tc.add_p()`, `p.add_r()`,\
    \ `r.text = text`.\nSo:\n\nSurvives (because it lives in `w:tcPr`, which is preserved):\n- `w:tcW`\
    \ (cell width), `w:gridSpan`, `w:vMerge`, `w:vAlign`;\n- `w:shd` — so the dark header shading remains;\n\
    - `w:tcBorders`, `w:tcMar`, `w:noWrap`, `w:hideMark`;\n- the column geometry, the table style, and\
    \ therefore the overall\n  table look and the page fit.\n\nLost (because it lived in `w:p`/`w:r` and\
    \ the run is brand new):\n- all run formatting: bold, 9 pt `w:sz`, white `w:color`, `w:rFonts`,\n\
    \  `w:highlight`, `w:caps`, `w:spacing`, `w:lang`, `w:vanish`;\n- all paragraph formatting: alignment,\
    \ spacing, `w:pStyle`, numbering,\n  indentation, borders, tabs;\n- any extra paragraphs (the cell\
    \ is collapsed to exactly one);\n- any fields (`w:fldSimple`/`w:instrText`), hyperlinks, bookmarks,\
    \ comments,\n  content-control wrappers, and tracked-revision marks inside the cell.\n\nThe new run\
    \ has only the defaults from the document's `docDefaults` /\nNormal style, so the white 9 pt bold\
    \ text becomes default-size black text\non the dark shading — effectively invisible, which is the\
    \ classic symptom.\n\nFixes: (a) edit the text in place — set the first existing `w:t`'s text\n(or\
    \ `paragraph.runs[0].text`) and leave the rest of the structure alone;\n(b) if you must rewrite, deep-copy\
    \ the original run's `w:rPr` onto the new\nrun, or re-apply `run.font.bold`, `run.font.size = Pt(9)`,\n\
    `run.font.color.rgb = RGBColor(0xFF, 0xFF, 0xFF)`, plus\n`paragraph.alignment`; (c) remember `xml:space=\"\
    preserve\"` if the new text\nhas leading/trailing spaces. Always assert the `w:shd` and width survived\n\
    by re-reading the cell after saving.\n"
- id: docx-cell-02
  answer: "Qty never exists alone. If Amount = Qty × Unit price, then Amount, the\nline subtotal, any\
    \ percentage tax computed on the line, shipping, the\ninvoice total, the balance/amount due, a manually\
    \ typed grand total, and\npossibly the total written out in words and a duplicate total in a\nsummary\
    \ table, a header/footer, or a second page, all become stale. Any\n`{=SUM(ABOVE)}` / `{=PRODUCT(LEFT)}`\
    \ field keeps its old *cached* result\n(see the field answer) and any downstream narrative (\"5 units\
    \ ordered\")\nbecomes wrong.\n\nProcedure:\n\n1. Read all the amounts (visible text, strip currency\
    \ symbols, thousands\n   separators and parentheses-for-credit), sum them, and assert the sum\n  \
    \ equals the total the document currently claims — before editing. This\n   catches a bad input table\
    \ instead of silently propagating it.\n2. Decide which side is authoritative: if Unit price is fixed,\
    \ recompute\n   `amount = qty * unit_price`; if Amount is contractual, recompute\n   `unit_price =\
    \ amount / qty` and round to the currency's minor unit with\n   a documented rule (banker's or half-up)\
    \ so the printed rate is defensible.\n3. Write the new Qty and the recomputed Amount with formatting\
    \ preserved\n   (edit the existing `w:t`, or copy the run's `w:rPr`) rather than using\n   `cell.text\
    \ = …`.\n4. Recompute the tax and total in Python, then either set\n   `<w:updateFields w:val=\"true\"\
    />` in `word/settings.xml` (Word refreshes\n   fields on open) or replace the Total field with a literal,\
    \ recomputed\n   number while keeping its `rPr`. Update the \"in words\" text if present.\n5. Watch\
    \ for values held in content controls (`w:sdt`), bookmarks or\n   cross-references, and in `docProps/custom.xml`.\n\
    6. Verify after saving: reopen the file, re-read Qty, Amount, tax and\n   Total, and assert `Total\
    \ == sum(Amounts)` and that every Amount still\n   equals Qty × Unit price to the cent.\n"
- id: docx-cell-03
  answer: "Two separate problems.\n\n(1) python-docx writes no revision marks. Rewriting a `w:t` (or using\n\
    `cell.text = …`) changes the content in place, producing an *untracked*\nedit: your change appears\
    \ as ordinary text with no `w:ins`/`w:del` and no\nauthor/date, so Word's review pane shows nothing,\
    \ \"Reject All Changes\"\ncannot undo it, and the change is invisible to anyone auditing the file.\n\
    Worse, `w:trackRevisions` in `settings.xml` stays on, so the *next* edit a\nhuman makes in Word is\
    \ tracked while yours silently is not — the review\nhistory is now incomplete and misleading.\n\n\
    (2) Reading is unreliable while revisions exist. `paragraph.runs` and\n`Paragraph.text` only see `w:r`\
    \ elements that are direct children of `w:p`,\nso runs wrapped in `w:ins` (pending insertion) are\
    \ invisible, and runs in\n`w:del` are visible as ordinary runs even though their text is in\n`w:delText`\
    \ and will vanish on accept. Hyperlink and `w:smartTag` runs are\nlikewise skipped. So `cell.text`\
    \ may report the wrong thing, and a \"clean\nedit\" may land in the wrong place or on text that is\
    \ about to disappear.\n`w:rPrChange`/`w:pPrChange` may also carry the previous formatting.\n\nWhat\
    \ to consider and do:\n\n- Decide explicitly: do you want a *tracked* change or a clean one? The\n\
    \  user asked for a clean edit, so either turn tracking off (remove\n  `w:trackRevisions` from `settings.xml`)\
    \ or accept existing revisions\n  first — and say which you did.\n- To produce a real tracked change\
    \ you must do it by hand with lxml:\n  wrap the old run in `<w:del w:id=\"…\" w:author=\"…\" w:date=\"\
    …>` and\n  convert its `w:t` to `w:delText`; insert the new run inside a\n  `<w:ins …>`; use unique,\
    \ increasing `w:id`s. python-docx has no\n  revision API, and it also has no `accept`/`reject`.\n\
    - If you edit inside an existing `w:ins`, keep your new run inside the same\n  `w:ins` so accepting\
    \ still yields a coherent result; never leave a\n  `w:ins` whose only content is empty.\n- When reading,\
    \ use an XPath that collects `w:t` and excludes\n  `w:delText`/`w:instrText`, or \"accept\" a copy\
    \ of the XML in memory first.\n- Also check for `w:documentProtection` (read-only or tracked-changes-only\n\
    \  enforcement) and for `w:rsid` noise you do not need to touch.\n- Finally, verify: the saved file's\
    \ `settings.xml`, the presence/absence\n  of `w:ins`/`w:del` where expected, and that the visible\
    \ text is what you\n  intended.\n"
- id: docx-relayout-01
  answer: "`table.add_row()` takes no arguments and always appends at the *bottom*\n(after the Total row).\
    \ It builds a `w:tr` from the `w:tblGrid`, giving each\nnew cell a `w:tcPr/w:tcW` equal to its `w:gridCol`'s\
    \ dxa width — and\nnothing else: no `w:trPr` (height, `w:cantSplit`), no `w:shd` banding, no\nparagraph\
    \ style, no run formatting, no alignment, no borders beyond the\ntable's own. So a row created that\
    \ way is functionally correct (grid\nconsistent, text renders) but visually wrong: wrong shading on\
    \ a banded\ntable, default font size/weight, default alignment, and the wrong position.\n\nTo insert\
    \ a correctly formatted row in the middle:\n\n1. Pick a donor row that already looks right — preferably\
    \ the adjacent\n   line-item row (\"Service G\" or the row before it), not the header.\n2. `new_tr\
    \ = copy.deepcopy(donor._tr)`. The deep copy carries `w:trPr`,\n   each `w:tcPr` (width, `w:shd`,\
    \ `w:gridSpan`, `w:vAlign`, borders) and\n   each `w:p`/`w:r`/`w:rPr` — i.e. the whole look, for free.\n\
    3. Replace the text in the copy by editing the existing `w:t` nodes (set\n   `w:t.text`, add `xml:space=\"\
    preserve\"` if needed) rather than using\n   `cell.text = …` or rebuilding runs. Keep the number of\
    \ `w:tc` and the\n   gridSpan pattern identical to the donor, and delete any donor-only\n   content:\
    \ field results, bookmark/comment ranges, `w:ins`/`w:del`,\n   hyperlinks, checkboxes, `w:proofErr`,\
    \ and `w:tblHeader` in `w:trPr`\n   (a repeated header row must not be duplicated).\n4. Insert it\
    \ in position with lxml: `reference_row._tr.addnext(new_tr)`\n   (or `addprevious` for \"before the\
    \ Total row\"). Do not build a new `w:tr`\n   from scratch.\n5. If the table alternates shading (`w:shd`\
    \ on odd/even rows), flip the\n   copied `w:shd` fill on the new row so the banding stays correct.\n\
    6. Verify: `len(new_row.cells) == len(table.columns)`, and for every row\n   `gridBefore + Σ gridSpan\
    \ + gridAfter == len(table.columns)`; confirm the\n   row landed between the intended neighbours;\
    \ then recompute the total and\n   re-read the saved file.\n"
- id: docx-relayout-02
  answer: "`table.add_column(width)` requires a `Length`. It appends one `w:gridCol` to\n`w:tblGrid` at\
    \ the far *right*, adds one new `w:tc` to the end of every\n`w:tr` (including rows whose cells are\
    \ merged), sets the new gridCol and\neach new cell's `w:tcW` to that width, and returns a `_Column`\
    \ whose\n`cells` you can use to write the header. It does **not** shrink the\nexisting columns, does\
    \ not rebalance anything, does not touch `w:tblW`, and\ndoes not guarantee the table still fits the\
    \ text width. So:\n\n1. Compute the available width first:\n   `avail = Emu(section.page_width - section.left_margin\
    \ -\n   section.right_margin)` (the subtraction yields a plain int, so wrap it\n   before `.twips`/`.emu`).\
    \ Decide the new column's share, then rebalance\n   *all* `w:gridCol/@w:w` **and** every `w:tcW` in\
    \ every row so the totals\n   equal `avail` exactly (a cell with `w:gridSpan=\"n\"` gets the sum of\
    \ its\n   n gridCols). Forgetting the `w:tcW` values is the classic bug: the grid\n   is only a hint;\
    \ the cells drive layout.\n2. `Column.width` sets only the `gridCol` — also set `cell.width` for each\n\
    \   cell. Verify the sum in twips.\n3. Position: `add_column` appends at the far right, so if the\
    \ new column\n   must sit after Item, move the `w:gridCol` to the target grid index and\n   move each\
    \ row's new `w:tc` to the matching *grid* offset. Do this by\n   grid offset, not `tc` index: if a\
    \ cell spans the insertion point, give\n   it `w:gridSpan = old + 1` instead of adding a new `w:tc`;\
    \ if a cell\n   starts at the insertion point, insert a deep copy of its left neighbour\n   from the\
    \ same row before it. Copy a neighbouring `w:gridCol` for the new\n   width instead of inventing a\
    \ gridCol from scratch.\n4. Make it look like its neighbours: copy the `w:shd` of the existing header\n\
    \   cells, write the header text through runs carrying the same `w:rPr`\n   (bold, size, colour, font),\
    \ copy cell `w:tcMar`/borders if set, and set\n   body alignment to match the neighbouring column.\
    \ If the table is banded,\n   fill the new cells' `w:shd` per row.\n5. Fix the layout mode: with `w:tblLayout\
    \ w:type=\"autofit\"` (python-docx\n   `table.autofit = True`) Word recomputes widths from content\
    \ and will\n   ignore your numbers — use fixed layout. Also make sure `w:tblW` is\n   consistent (fixed\
    \ dxa total, or auto) and that there is exactly one\n   `w:tblGrid`, immediately after `w:tblPr`.\n\
    6. Finally assert, for every row,\n   `gridBefore + Σ gridSpan + gridAfter == len(table.columns)`,\
    \ that the\n   total grid width equals the text width, and that the table still fits\n   the page\
    \ after Word repaginates.\n"
- id: docx-relayout-03
  answer: "Because the `w:gridCol` widths are only a hint; the real layout comes from\nthe cell widths\
    \ and the table's layout algorithm. In order of likelihood:\n\n1. **You did not update `w:tcW`.**\
    \ The grid supplies preferred widths, but\n   each cell's `w:tcPr/w:tcW` is what actually lays the\
    \ column out. Every\n   `w:tcW` in every row must be updated too (and a `w:gridSpan` cell's\n   `w:tcW`\
    \ must equal the sum of the gridCols it spans). This is also why\n   python-docx's `_Column.width`\
    \ setter is documented as setting only the\n   gridCol.\n2. **Autofit is on.** With `w:tblLayout w:type=\"\
    autofit\"` (or\n   `w:tblW w:type=\"auto\"`), Word recomputes column widths from content and\n   ignores\
    \ the stored widths entirely. Set fixed layout:\n   `table.autofit = False` and `<w:tblLayout w:type=\"\
    fixed\"/>` in `w:tblPr`.\n3. **Conflicting totals.** `w:tblW` (table width), `w:tblInd` (indent) and\n\
    \   `w:tblCellMar` (cell margins) are independent; if `w:tblW` is still the\n   old total, or the\
    \ grid total now differs from `w:tblW`, the result is\n   unpredictable. `w:tcW w:type` must be `dxa`\
    \ (not `pct`, `auto` or\n   `nil`).\n4. **Grid/cell mismatch.** A second `w:tblGrid`, a `w:tblGrid`\
    \ that is not\n   immediately after `w:tblPr`, or rows whose `gridBefore + Σ gridSpan +\n   gridAfter`\
    \ no longer equals the number of `w:gridCol`s, makes Word\n   repair the table and fall back to content-based\
    \ widths.\n5. **Style precedence.** A `w:tblStyle` in `styles.xml` can carry its own\n   layout/tblW\
    \ (including `w:tblStylePr` conditional formats) and, combined\n   with a compatibility-mode or old-format\
    \ flag, can keep the old look.\n   Direct `w:tcW` normally wins, so if it does not, suspect the style\
    \ or the\n   presence of explicit `w:tblBorders`/`w:tblLook` overrides.\n6. **Not actually saved /\
    \ stale view.** Confirm the change is in the\n   `word/document.xml` inside the file you reopened\
    \ (`unzip -p` and grep\n   the `w:gridCol` values), close the document in Word so it is not\n   showing\
    \ a cached layout, and force repagination (Ctrl+A, F9 / print\n   preview). Also remember the table\
    \ may live inside a text box or a frame\n   with its own width constraint.\n\nA quick regression check:\
    \ after saving, re-parse the part and assert that\n`sum(w:gridCol/@w:w)` equals the section text width,\
    \ that every `w:tcW`\nmatches the width of the gridCols it occupies, and that the row sums match\n\
    the grid count.\n"
- id: docx-tblins-01
  answer: '`doc.add_table(rows, cols)` always **appends the new table at the very end of the

    document body**, just before the final `w:sectPr` — never after an arbitrary

    paragraph in the middle, and it does not respect any "insertion point" you have in

    mind. It also creates a fresh table with default style/widths rather than copying

    anything about nearby tables.


    To place it immediately after a given paragraph, build the table at the end as

    usual and then *move* the underlying XML element with `addnext`:


    ```python

    from docx import Document

    doc = Document("in.docx")

    para = next(p for p in doc.paragraphs if p.text.strip() == "Notes:")


    table = doc.add_table(rows=2, cols=3)     # lands at end of body

    para._p.addnext(table._tbl)               # relocate right after that paragraph

    ```


    `para._p` is the CT_P element, `table._tbl` is the CT_Tbl element, and `addnext`

    is an lxml method that inserts the table as the paragraph''s immediate next

    sibling. If the paragraph is inside a table cell or a text box, you must walk to

    the correct container element instead of using `doc.paragraphs` (which only sees

    top-level body paragraphs); note also that `doc.paragraphs` does not include

    paragraphs inside tables, so for those you must search `cell.paragraphs` or use

    XPath such as

    `doc.element.body.xpath(''.//w:p[normalize-space(string(.))="Notes:"]'')`.


    Because `add_table` sizes and styles the table generically, after relocating it

    you should still fix the style, column widths and header formatting to match the

    surrounding tables, and re-save. Verify with `doc.paragraphs` ordering or by

    checking `table._tbl.getprevious() is para._p`.

    '
- id: docx-tblins-02
  answer: "`table.style = \"Grid Table 4 Accent 1\"` sets the table's style *by name*, and\npython-docx\
    \ resolves that name by looking it up in the document's own\n`styles.xml` part. A style is not built\
    \ into python-docx; it only exists if the\ntemplate that produced the .docx actually defined it. Many\
    \ company templates ship\na minimal `styles.xml` containing only Normal, a few paragraph styles, and\n\
    whatever table styles the authoring tool happened to use. \"Grid Table 4 Accent 1\"\nis one of Word's\
    \ *built-in* table styles — Word synthesises those lazily the first\ntime you apply them in the UI\
    \ and only then writes them into the document. So a\nfile created programmatically, or a template\
    \ that never had a ruled table, has no\nsuch `<w:style w:styleId=\"GridTable4-Accent1\">` entry, the\
    \ lookup misses, and\npython-docx raises `KeyError`. Note that this is a `KeyError`, not a\n\"style\
    \ not found\" message, and `\"Table Grid\"` behaves identically — the name is\njust a built-in, so\
    \ people wrongly assume it always resolves.\n\nWays to get a ruled table, best first:\n\n1. **Copy\
    \ the style from a table that already has it.** In a document that does\n   contain ruled tables:\
    \ `new_table.style = existing_table.style` (assigning the\n   style *object* is safer than assigning\
    \ a name string). This is the recommended\n   approach because it inherits borders, banding, font\
    \ and cell margins exactly.\n2. **List what actually exists** before choosing:\n   `print([s.name\
    \ for s in doc.styles if s.type == WD_STYLE_TYPE.TABLE])`, or\n   iterate `doc.styles` and check `style.type`.\
    \ Never guess a name, including\n   \"Table Grid\".\n3. **Write explicit borders** as the fallback,\
    \ which works regardless of\n   styles.xml. Add a `w:tblBorders` child to the table's `w:tblPr` (with\n\
    \   top/left/bottom/right/insideH/insideV `w:val=\"single\"`, a `w:sz` in eighths of\n   a point,\
    \ e.g. 4–8, and a `w:color`). Two constraints matter: `w:tblBorders`\n   must be inserted in the schema-legal\
    \ position inside `tblPr` — after\n   `w:tblW`, `w:jc`, `w:tblCellSpacing`, `w:tblInd` and before\
    \ `w:shd`,\n   `w:tblLayout`, `w:tblCellMar`, `w:tblLook` — because OOXML is sequence-ordered\n  \
    \ and Word rejects out-of-order children. And note `table._tbl.tblPr` has no\n   Python setter, so\
    \ you manipulate the element's children directly\n   (`tblPr.append(borders_el)` at the right index)\
    \ or `deepcopy` the child\n   elements from another table's `tblPr`.\n\nAlso, once the style resolves,\
    \ remember that a table style alone may not give\nvisible rules if the style's conditional formatting\
    \ band (`w:tblLook`) is absent\nor disabled; check `w:tblLook` / just set borders explicitly if in\
    \ doubt.\n"
- id: docx-tblins-03
  answer: "`doc.add_table(rows, cols)` gives you a table whose total width is the\n**available text width\
    \ of the section** — `section.page_width - section.left_margin\n- section.right_margin` — divided\
    \ equally among the columns. Concretely: `tblW` is\nset to **auto** (`w:type=\"auto\"`, `w:w=\"0\"\
    `), but python-docx writes a real dxa\nwidth into every `w:gridCol` in `w:tblGrid` *and* a matching\
    \ `w:tcW` on every\ncell, each equal to `text_width / cols`. So the columns are only \"even\" by\n\
    default; they do not inherit the target table's proportions, and there is no\nautofit to the document's\
    \ existing grid.\n\nTo match the existing invoice table:\n\n1. **Measure the target.** Get the text\
    \ width as an int in EMU:\n   `Emu(section.page_width - section.left_margin - section.right_margin).twips`\n\
    \   — note that subtracting Lengths yields a plain `int` in EMU, so wrap it before\n   reading `.twips`.\
    \ Or, more directly, read the existing table's actual\n   `gridCol` widths.\n2. **Copy the widths\
    \ from the existing table**, which preserves its proportions:\n   for each column index, set `w:w`\
    \ on the corresponding `w:gridCol` and set\n   `cell.width` for every cell in that column. `column.width\
    \ = w` only sets the\n   `gridCol` and does nothing to the cells' `tcW`, so you must set both or the\n\
    \   rendering will disagree with the grid.\n3. **Scale if the totals differ.** If the source table's\
    \ columns sum to a\n   different total than your section's text width, scale every gridCol and tcW\n\
    \   proportionally so the new table fills the same total.\n4. **Edit the existing `tblGrid` in place**\
    \ — never insert a second\n   `w:tblGrid`; a `w:tbl` may contain only one, and two will trigger a\
    \ repair\n   prompt.\n5. **Remember spans.** A cell with `w:gridSpan` occupies several grid columns,\
    \ so\n   its `tcW` should be the *sum* of the widths it spans, otherwise rows will not\n   line up.\
    \ Also set `table.autofit = False` and add a fixed `w:tblLayout\n   w:type=\"fixed\"` if you want\
    \ the widths to be honoured rather than\n   re-fitted by Word on open.\n6. **Match the look too**:\
    \ `new.style = existing.style`, and copy the header\n   cells' `w:shd` shading and run formatting\
    \ (bold, colour, font, size) if you\n   want the new table to be visually indistinguishable. Copy\
    \ children of `tblPr`\n   element by element with `deepcopy` — never assign `tblPr` wholesale.\n"
- id: docx-imgrep-01
  answer: "A picture is an inline (or anchored) `w:drawing` element inside a run. Deep inside\nit is a\
    \ `a:blip` (DrawingML picture) whose `r:embed` attribute holds a\n**relationship id (rId)** — e.g.\
    \ `r:embed=\"rId7\"`. That rId is looked up in the\n**relationship part of the part that owns the\
    \ drawing**: `word/document.xml` for\nbody pictures, or the header/footer part (`word/header1.xml`,\
    \ etc.) for pictures\nin a page header. The relationship is of type\n`.../relationships/image` and\
    \ its `Target` points at an image part under\n`word/media/`, e.g. `media/image3.png`. So the chain\
    \ is:\n`w:drawing` → `a:blip/@r:embed` → `_rels/document.xml.rels` entry → `word/media/*`\n→ part's\
    \ blob. The displayed size lives in `wp:extent` (`cx`/`cy` in EMU) and is\nmirrored in `a:ext` inside\
    \ `pic:spPr/a:xfrm`; the relationship itself carries no\nsize.\n\nTo replace **one** picture with\
    \ a new file while keeping its position and size:\n\n```python\nfrom docx.shared import Emu\nfrom\
    \ docx.oxml.ns import qn\n\npart = doc.part                      # the part that owns the blip you\
    \ are editing\nrId, image = part.get_or_add_image(\"new_logo.png\")\n\n# Locate the specific blip,\
    \ not just the first drawing in the part.\nblip = target_drawing.find(qn('a') and ...)  # e.g. drawing.xpath('.//a:blip')[0]\n\
    blip.set(qn('r:embed'), rId)        # rId is in the r: namespace\n\nextent = target_drawing.xpath('.//wp:extent')[0]\n\
    cx, cy = int(extent.get('cx')), int(extent.get('cy'))\n# keep cx; recompute cy only if the new aspect\
    \ ratio differs\nnew_cy = Emu(cx * image.px_height // image.px_width)\nextent.set('cx', str(cx));\
    \ extent.set('cy', str(new_cy))\n```\n\nKey points:\n\n* Reuse the existing `w:drawing` element so\
    \ the position in the text flow\n  (paragraph, run, alignment) is untouched; only swap the reference.\n\
    * `get_or_add_image` registers the new bytes and returns `(rId, Image)`. It\n  stores new bytes; it\
    \ does not touch the old part.\n* Keep the width and scale the height by the *ratio* of pixel dimensions\
    \ —\n  `cy = cx * px_height // px_width` — applied in **both** `wp:extent` and the\n  picture's `a:ext`.\
    \ For an inline shape, `InlineShape.width`/`.height` setters\n  write both for you, which is the easiest\
    \ route. Never multiply px by EMU:\n  914400 EMU per inch, 12700 per point, 1440 twips per inch; pixels\
    \ enter only as\n  a ratio.\n* Read the extent from the drawing you are actually replacing, not the\
    \ first\n  `wp:extent` in the part — a part can hold many pictures.\n* Drop the old relationship only\
    \ when nothing still points at it:\n  `len(part.element.xpath(f'//a:blip[@r:embed=\"{old_rId}\"]'))\
    \ == 0`, then\n  `part.drop_rel(old_rId)`. `drop_rel` only counts `r:id` attributes, never\n  `r:embed`,\
    \ so it will happily delete a still-in-use image relationship — hence\n  the manual count. An orphaned\
    \ image part is not written on save.\n* Get `cx`/`cy` as ints with `int(...)`; the attributes are\
    \ strings.\n"
- id: docx-imgrep-02
  answer: 'Because **image parts are shared across the whole package via SHA1

    deduplication**. When python-docx (and the OPC layer beneath it) adds image

    bytes, it hashes them and, if an identical blob already exists in the package —

    including parts under `word/media/` referenced from **headers and footers** — it

    reuses that single part and hands back a new relationship that points at the

    *same* part. So the two visible pictures were never two independent blobs: they

    were two `r:embed` references resolving to one `ImagePart` object.


    Assigning `image_part._blob = new_bytes` mutates that one shared part in place.

    Every drawing anywhere in the document — body, header, footer, first-page or

    even-page variants — that referenced that part now renders the new bytes. Hence

    the page-3 picture changed too: it happened to be a byte-identical copy of the

    logo, so it shared the part.


    The correct fix is to **add a new image part and repoint the one blip** you mean

    to change, leaving the old part and its other references intact:


    ```python

    rId, image = part.get_or_add_image("new_logo.png")

    blip.set(qn(''r:embed''), rId)   # only this drawing now points at the new bytes

    ```


    Then, only if no `a:blip[@r:embed="old_rId"]` remains in that part, call

    `part.drop_rel(old_rId)`. Never write to `image_part._blob` to change a single

    picture; it is inherently a global operation. A related subtlety: if the *old*

    part becomes unreferenced by any relationship, python-docx will not serialise it

    on save, so a genuinely-orphaned image can silently disappear from the zip — check

    the output rather than assuming.

    '
- id: docx-imgrep-03
  answer: "It is almost certainly in a **header or footer part**, not the body. `doc.\ninline_shapes`\
    \ is built from the `w:drawing` elements that are descendants of\n`doc.part.element` (i.e. `word/document.xml`\
    \ body content) that are inline — a\npicture in `word/header1.xml` lives in a different part entirely,\
    \ so it is\ninvisible to that collection. Since the logo repeats on every page, it is a\nheader. (Other\
    \ possibilities: it is anchored/floating rather than inline, in\nwhich case it is in the body but\
    \ under `w:anchor` and also missing from\n`inline_shapes`; or it is a `w:pict`/VML legacy image; or\
    \ it is a shape fill.\nCheck each of these before concluding.)\n\nHow to find it and replace it:\n\
    \n```python\nfor section in doc.sections:\n    for hf in (section.header, section.first_page_header,\n\
    \               section.even_page_header, section.footer,\n               section.first_page_footer,\
    \ section.even_page_footer):\n        if hf is None:\n            continue\n        part = hf.part\
    \                      # the header/footer part owning the blip\n        for drawing in part.element.xpath('.//w:drawing'):\n\
    \            blips = drawing.xpath('.//a:blip')\n            if not blips:\n                continue\n\
    \            rId, image = part.get_or_add_image(\"new_logo.png\")\n            blips[0].set(qn('r:embed'),\
    \ rId)\n            ext = drawing.xpath('.//wp:extent')[0]\n            cx = int(ext.get('cx'))\n\
    \            ext.set('cy', str(Emu(cx * image.px_height // image.px_width)))\n```\n\nPractical points:\n\
    \n* Iterate **all** header/footer variants (`header`, `first_page_header`,\n  `even_page_header`,\
    \ and the footer equivalents) because \"different first page\"\n  or \"different odd/even\" settings\
    \ mean a separate part that also shows a logo.\n  Also be aware sections can be linked to the previous\
    \ section\n  (`is_linked_to_previous`), in which case the drawing lives in an earlier\n  section's\
    \ part and editing that part updates every linked section.\n* `hf.part` is the correct owner for `get_or_add_image`\
    \ and `drop_rel`; using\n  `doc.part` would add a relationship that the header never sees.\n* The\
    \ dedupe caveat from `imgrep-02` applies here in reverse and is *helpful*:\n  because identical bytes\
    \ share one part, replacing the header logo by adding\n  new bytes and repointing the blip will **not**\
    \ disturb a body picture that was\n  using the old logo bytes — but if you \"helpfully\" mutate `_blob`,\
    \ it will.\n* If the logo turns out to be floating, `doc.inline_shapes` will not list it;\n  enumerate\
    \ `doc.element.body.xpath('.//w:drawing')` or the header part's\n  equivalent and handle `wp:anchor`\
    \ (you may also want to convert or keep it\n  floating deliberately).\n* After replacing, confirm\
    \ the old bytes are gone from the package — see\n  `docx-verify-03` — since a shared or orphaned part\
    \ can still carry them.\n"
- id: docx-imgins-01
  answer: "`doc.add_picture(path, width=...)` is a convenience wrapper: it appends a **new\nparagraph\
    \ at the very end of the document body** and puts the inline picture run\ninside it. It has no notion\
    \ of \"after paragraph X\", so it never lands where you\nwant on the first try.\n\nThe reliable pattern\
    \ is to add the picture anywhere, then **move the new\nparagraph** into position:\n\n```python\nfrom\
    \ docx import Document\nfrom docx.shared import Inches\nfrom docx.oxml.ns import qn\n\ndoc = Document(\"\
    contract.docx\")\npara = next(p for p in doc.paragraphs if p.text.strip() == \"Approved by:\")\n\n\
    new_p = doc.add_paragraph()                     # or doc.add_picture(...) then take\nrun = new_p.add_run()\n\
    run.add_picture(\"signature.png\", width=Inches(1.5))\n\npara._p.addnext(new_p._p)               \
    \         # relocate the whole paragraph\n```\n\nTwo workable variants:\n\n* **Add directly into the\
    \ target paragraph** — if you do not need a separate\n  paragraph at all: `para.add_run().add_picture(\"\
    signature.png\",\n  width=Inches(1.5))`. This is often the nicest result (\"Approved by:\" followed\
    \ by\n  the signature on the same line) and avoids any move entirely.\n* **`add_picture` then move**:\
    \ `doc.add_picture(...)` returns an `InlineShape`;\n  grab its containing paragraph via `shape._inline.getparent().getparent()`\
    \ and\n  `addnext` that paragraph after the target. Less clear than the `add_paragraph`\n  + `add_run`\
    \ form.\n\nKeeping the aspect ratio: pass only **one** dimension to `add_picture` — either\n`width=Inches(1.5)`\
    \ or `height=Inches(...)` — and python-docx derives the other\nfrom the image's native pixel dimensions,\
    \ so the proportions are preserved. Never\npass both `width` and `height` unless you intend to distort.\
    \ If you must set them\nmanually, compute `height = Emu(width * image.px_height // image.px_width)`\
    \ and\nwrite it to both `wp:extent` (`cx`/`cy`) and the picture's `a:ext`, or use\n`InlineShape.width`\
    \ / `.height`, which update both for inline shapes.\n\nAlso: `doc.paragraphs` only lists top-level\
    \ body paragraphs, so if \"Approved by:\"\nsits inside a table cell, a text box, or a header, find\
    \ it via XPath on the\nrelevant part and insert relative to *that* element.\n"
- id: docx-imgins-02
  answer: "The image goes in at the size you ask for and the cell does **not** clip or\nauto-shrink it:\
    \ the picture becomes 3 inches wide inside a 1.2-inch cell and\n**overflows**, spilling across the\
    \ cell boundary and over whatever is in the\nneighbouring columns — and because the row's height is\
    \ driven by its content, a\ntall image can also blow the row and page layout apart. Word does not\
    \ \"fit to\ncell\" for you, and the grid is not renegotiated to accommodate the picture.\n\nSize it\
    \ explicitly to the cell's usable width:\n\n```python\nfrom docx.shared import Inches, Emu\n\ncell\
    \ = table.cell(r, c)\nusable = cell.width                      # the cell's own tcW, in EMU\n# account\
    \ for default cell margins (~0.08\" each side) if you want a margin\ntarget = usable - Inches(0.2)\n\
    \nshape = cell.paragraphs[0].add_run().add_picture(\"photo.png\", width=target)\n# aspect ratio preserved\
    \ automatically: height derived from px ratio\n```\n\nPractical advice:\n\n* `cell.width` reads the\
    \ cell's `w:tcW` (dxa internally, EMU on the API). For a\n  reliable value set/read both the `gridCol`\
    \ and each cell's `tcW`; if `tcW` is\n  `auto`/missing, fall back to the column width or compute\n\
    \  `Emu(section.page_width - section.left_margin - section.right_margin).twips`\n  divided by the\
    \ number of grid columns.\n* Pass **only** `width` (or only `height`) so the aspect ratio is derived\
    \ from\n  the image's native pixel size. If you need to constrain both dimensions,\n  compute one\
    \ from the pixel ratio and cap the other, choosing the *smaller* of\n  the width-derived and height-derived\
    \ result so the image fits inside the cell\n  rather than overflowing.\n* Subtract cell margins (`w:tblCellMar`,\
    \ roughly 0.08 inch per side by default)\n  from the available width, and remember that a `gridSpan`\
    \ cell is only as wide\n  as the sum of the `gridCol`s it spans.\n* Alternatively, change the picture's\
    \ wrapping to floating/anchored with\n  square or tight text wrapping, which lets it overlap rather\
    \ than displace the\n  grid — but that is usually the wrong choice in a table.\n* Never fix this by\
    \ scaling the *table*; scale the picture.\n"
- id: docx-legacy-01
  answer: "No. python-docx can only open **`.docx`** (and, read-only, the older\n`.docm`/`.dotx` families).\
    \ `Document(\"report.doc\")` raises `ValueError:\nfile 'report.doc' is not a Word file, content type\
    \ is 'application/msword'` or\na package/open error, because a legacy binary `.doc` is an OLE2 compound\
    \ file\nholding a proprietary binary format, not an OPC zip with `word/document.xml`. The\nsame applies\
    \ to the old binary `.xls`/`.ppt` families. `.rtf` and `.odt` are\nlikewise unsupported.\n\nThe workflow\
    \ is **convert first, then edit the .docx**:\n\n1. **Convert `.doc` → `.docx` with an external tool**,\
    \ then open the result with\n   python-docx:\n   * **LibreOffice**: `soffice --headless --convert-to\
    \ docx --outdir out\n     report.doc` (see `docx-legacy-03` for the reliable invocation).\n   * **macOS\
    \ `textutil`**: `textutil -convert docx report.doc -output\n     report.docx` (see `docx-legacy-02`\
    \ for the fidelity caveats).\n   * Word itself via AppleScript or a `Documents.Open` + `SaveAs` round-trip\
    \ is\n     the highest-fidelity option if it is available.\n2. **Open the converted file**: `doc =\
    \ Document(\"out/report.docx\")`.\n3. **Delete the row** — locate the table and the `w:tr`, then remove\
    \ the element:\n   ```python\n   for tr in table.rows:\n       if tr.cells[0].text.strip() == \"Total\"\
    :\n           tr._tr.getparent().remove(tr._tr)\n           break\n   ```\n   `tr._tr` is the `CT_Row`;\
    \ removing it from its parent is the whole operation\n   and shifts the rest up. Guard with a \"found\
    \ exactly one\" assertion so you\n   never delete the wrong row silently.\n4. **Check for merged cells\
    \ before addressing by index.** With `w:gridSpan` or\n   `w:vMerge`, a logical row can contain fewer\
    \ `w:tc` than the table has\n   `w:gridCol`, and `row.cells` repeats the spanned cell. Iterate\n \
    \  `row._tr.tc_lst` if you need true cell positions, and remember that\n   `gridBefore`/`gridAfter`\
    \ shift the grid offsets.\n5. **Save to a new file** (`doc.save(\"out/report-edited.docx\")`) — never\
    \ overwrite\n   the user's original .doc, and be explicit with the user that the deliverable is\n\
    \   a .docx, not a .doc.\n6. **Verify**: reopen the saved file, assert the row count dropped by one\
    \ and that\n   neighbouring rows' text is intact, and sanity-check the zip (`unzip -t`).\n7. **If\
    \ the user insists on a `.doc` deliverable**, convert the edited .docx back\n   (`soffice --headless\
    \ --convert-to doc`) as a final step, with the same fidelity\n   caveat: the round trip will not be\
    \ byte-clean and the .doc is a lossy target.\n"
- id: docx-legacy-02
  answer: "**The risk: `textutil` is a plain-text/rich-text converter, not a layout\nengine, and its .doc\
    \ reading and .docx writing are lossy.** It will happily\nproduce a file that opens fine but has been\
    \ silently restructured — most\ndamaging for a table-editing task:\n\n* Table structure can be flattened,\
    \ simplified, or converted to something that\n  is no longer a real `w:tbl` with a correct `w:tblGrid`\
    \ (or split/re-merged into\n  a different shape), so \"delete row 3\" may hit a different row than\
    \ it did in\n  the original.\n* Styles, numbering, headers/footers, fields, footnotes/endnotes, comments,\n\
    \  tracked changes, content controls, text boxes, floating shapes, and section\n  properties are commonly\
    \ dropped or downgraded to direct formatting.\n* Character and paragraph formatting is normalised,\
    \ so the document's *look*\n  changes even where the text survives.\n* The .docx it emits may not\
    \ carry the original `styles.xml`, which is exactly the\n  cause of the `KeyError` in `docx-tblins-02`.\n\
    \n**How to detect it:**\n\n1. **Compare structure, not bytes.** Load both the pre-conversion reference\
    \ and\n   the output and count: number of tables, rows per table, columns per table,\n   header/footer\
    \ text, and paragraph counts. Any drift is a red flag.\n2. **Inspect the raw XML** of the converted\
    \ file: check that each `w:tbl` has\n   exactly one `w:tblGrid` whose `gridCol` count matches the\
    \ widest row's\n   `Σ gridSpan + gridBefore + gridAfter`, and that `tcW` values are present. Also\n\
    \   list the style ids in `word/styles.xml` to see what survived.\n3. **Look for the tell-tale absences**:\
    \ no `styles.xml` table styles, no header\n   parts, no `numbering.xml`, missing section properties.\n\
    4. **Text-diff the extracted text** (e.g. `pandoc` or python-docx) between the\n   original render\
    \ and the converted file to confirm nothing was lost or\n   reordered.\n5. **Prefer higher-fidelity\
    \ converters when they are available**: LibreOffice\n   (`soffice --headless --convert-to docx`) is\
    \ far more faithful for tables and\n   layout than `textutil`; Word itself is best. Use `textutil`\
    \ only as a last\n   resort on a machine with neither.\n6. Whatever you use, do the edit on a **copy**\
    \ and always diff the table geometry\n   before and after, so you can tell whether a row vanished\
    \ because you removed\n   it or because the converter dropped it.\n"
- id: docx-legacy-03
  answer: "**Why:** `soffice` is a single-instance, profile-locking application. The\nalready-open LibreOffice\
    \ window owns the user profile directory\n(`~/Library/Application Support/LibreOffice/4/user` on macOS,\n\
    `~/.config/libreoffice/4/user` on Linux). A second `soffice` invocation notices\nthe running instance,\
    \ hands the request to it over IPC, and exits — often with\nstatus 0 and no output file of its own,\
    \ or it may fail with a lock/profile\nerror. A bare `file.doc` argument can also be silently mishandled\
    \ if the cwd is\nnot what you assume. The net effect is \"command returns immediately, no\n`.docx`\"\
    .\n\n**How to run it reliably from a script:**\n\n1. **Give it a private, disposable profile** so\
    \ it never contends with the GUI\n   instance — this is the single most important fix:\n   ```bash\n\
    \   soffice -env:UserInstallation=file:///tmp/lo_profile_$$ \\\n           --headless --norestore\
    \ --nolockcheck --nodefault --nofirststartwizard \\\n           --convert-to docx --outdir \"$PWD/out\"\
    \ \"$PWD/file.doc\"\n   ```\n   Use a unique directory per run (and a unique scratch dir overall).\n\
    2. **Pass absolute paths for both input and output.** Never rely on the current\n   working directory;\
    \ `--outdir` should be an absolute path that already exists.\n3. **Find the real binary.** On macOS\
    \ it is usually\n   `/Applications/LibreOffice.app/Contents/MacOS/soffice`; the `soffice` on `PATH`\n\
    \   may be a launcher that behaves differently. Discover it with\n   `ls -d /Applications/LibreOffice.app`\
    \ / `command -v soffice`.\n4. **Do not run it while the GUI is open at all** if you can avoid it —\
    \ quit the\n   desktop LibreOffice first. If you cannot, the private-profile flag above is\n   what\
    \ makes concurrent headless conversion safe.\n5. **Check the result, don't trust the exit code.**\
    \ `soffice` frequently exits 0\n   without producing anything, so assert that the expected output\
    \ file exists and\n   is non-empty (and that its mtime is newer than the run started) before\n   proceeding;\
    \ fail loudly otherwise.\n6. **Add `--convert-to \"docx:MS Word 2007 XML\"`** if the default filter\
    \ picks\n   something unexpected, and use `--infilter=\"MS Word 97\"` if the .doc is not\n   auto-detected.\n\
    7. **Serialize runs with a lock** if several scripts convert at once, and set a\n   generous timeout\
    \ — first-run profile creation can take tens of seconds.\n8. Verify the converted file structurally\
    \ (table geometry, styles.xml) before\n   editing, since headless conversion is a fidelity boundary\
    \ too.\n"
- id: docx-verify-01
  answer: "A disciplined save-and-verify loop — write to a **new** file, then prove it is\ngood with independent\
    \ tools before declaring success:\n\n1. **Never work on or overwrite the original.** Copy it first\
    \ and operate on the\n   copy; save with `doc.save(\"out/invoice-edited.docx\")`. If you have to touch\
    \ the\n   input, take a timestamped backup.\n2. **Pre-flight assertions on the in-memory document**\
    \ *before* saving: the\n   intended cell now holds the expected text; the table's `w:tbl` still has\n\
    \   exactly one `w:tblGrid`; `len(gridCol) == max over rows of (gridBefore + Σ\n   gridSpan + gridAfter)`;\
    \ totals recomputed from the Amount column equal the\n   printed total; relationship targets still\
    \ resolve. Fail the script here rather\n   than writing a broken file.\n3. **Save**, then **re-open\
    \ the saved file with python-docx** and assert the\n   change round-tripped (right text, right row/column\
    \ count). This catches\n   in-memory-only mistakes.\n4. **Check the package**: `unzip -t invoice-edited.docx`\
    \ (CRC integrity) and list\n   the parts with `unzip -l` to confirm nothing vanished and no unexpected\
    \ part\n   appeared.\n5. **Validate the XML** against the schema and relationships — the docx skill's\n\
    \   `ooxml/scripts/validate.py` takes an **unpacked directory** (not a .docx) plus\n   `--original\
    \ <file.docx>` and exits 0 only on a clean run. Parse every `.xml`\n   and `.rels` part with lxml\
    \ to catch well-formedness errors.\n6. **Text-level diff** with an independent parser: `pandoc invoice.docx\
    \ -t\n   markdown` and diff against the pre-edit extraction. Expect exactly the lines you\n   intended\
    \ to change and nothing else — this is the strongest cheap check that\n   you edited the right thing.\n\
    7. **Independent library read-back** (python-docx, and if available a second\n   parser) to confirm\
    \ structure and content.\n8. **Beware the round-trip trap:** the skill's `unpack.py`/`pack.py` are\
    \ *not*\n   byte-faithful — `unpack.py` rewrites the XML declaration and pretty-prints\n   (a condensed\
    \ 11 KB `document.xml` comes back at ~20 KB), and `pack.py` runs\n   `condense_xml()` over every part,\
    \ which strips whitespace-only nodes and can\n   corrupt `w:instrText` field codes. For minimal edits,\
    \ read the part straight\n   from the original zip and re-zip preserving each `ZipInfo`, or use the\n\
    \   skill's scripts read-only.\n9. **Report honestly**: if you could not render the document (no LibreOffice\
    \ /\n   no Word available), say so. A structural and textual validation is strong\n   evidence but\
    \ is *not* pixel-level confirmation — that gap should be stated,\n   not glossed over. If a renderer\
    \ is available, open the result and eyeball the\n   page as the final check.\n"
- id: docx-verify-02
  answer: "\"Word found unreadable content\" means the file is a valid zip but its XML violates\nthe OOXML\
    \ schema or the part relationships. For table edits the usual culprits are:\n\n* **A second `w:tblGrid`.**\
    \ A `w:tbl` may contain only one; `w:tblGrid` must be\n  the first child. If you copied a table wholesale\
    \ you may have appended another\n  grid. Edit the existing grid in place.\n* **Child order inside\
    \ `w:tblPr`.** OOXML is sequence-ordered. `w:tblBorders` must\n  come after `w:tblW`, `w:jc`, `w:tblCellSpacing`,\
    \ `w:tblInd` and before `w:shd`,\n  `w:tblLayout`, `w:tblCellMar`, `w:tblLook`. Appending a property\
    \ element at the\n  end of `tblPr` is one of the most common causes of this exact prompt.\n* **A `w:tc`\
    \ with no `w:tcPr`, or `w:tcPr` children out of order** (`w:tcW` must\n  come first; `w:shd` after\
    \ `w:tcW`/`w:gridSpan`).\n* **A `w:tr` whose cells no longer add up to the grid.** `gridBefore + Σ\
    \ gridSpan\n  + gridAfter` must equal the number of `w:gridCol`s in every row. Inserting a\n  column\
    \ by `tc` index instead of grid position, or forgetting that a\n  `w:gridSpan` cell covers several\
    \ grid columns, produces a mismatch.\n* **`w:gridSpan` of 0 or a `w:gridSpan` that pushes a row past\
    \ the grid width.**\n* **Bad or partial width attributes**: `w:w=\"auto\"` or empty on `w:gridCol`\
    \ /\n  `w:tcW` (use dxa integers), or a `w:tcW` of type `pct` with a value Word\n  rejects.\n* **Structural\
    \ edits done by index on the wrong node set** — e.g. deleting a\n  `w:tc` from a row and leaving the\
    \ rest of the row (and every other row) with a\n  different cell count; or removing a `w:tr` in a\
    \ way that also unparents a\n  required sibling.\n* **Namespace damage**: calling `lxml`'s `cleanup_namespaces()`\
    \ on a docx root\n  prunes prefixes it thinks are unused, but the root declares `w14`/`wp14`/etc.\n\
    \  and references them from `mc:Ignorable`; the result references undeclared\n  prefixes and Word\
    \ flags the file. Likewise inventing prefixes or reusing `w:t`\n  for a `w:tc` child.\n* **Two `w:sectPr`**\
    \ or a table placed after the final `w:sectPr` /\n  a `w:tbl` left in an illegal position.\n* **Invalid\
    \ relationship or content-type entries** after a manual re-zip —\n  a `Target` that no longer matches\
    \ a part, or a part missing from\n  `[Content_Types].xml`.\n* **Truncated or non-UTF-8 XML** from\
    \ sloppy byte splicing, or an unescaped `&`\n  in text.\n\nHow to catch these before Word does: `unzip\
    \ -t`; parse every part with lxml;\nrun the skill's `ooxml/scripts/validate.py` on an unpacked copy\
    \ (it checks XSD,\nrels and content types, and exits 0 only when clean); programmatically assert the\n\
    grid arithmetic in every table; and re-open with python-docx. Note that the\nskill's `pack.py` self-validation\
    \ **skips when LibreOffice/`soffice` is not\ninstalled**, so never treat its clean exit as proof.\n"
- id: docx-verify-03
  answer: "Removing the `w:drawing` element only removes the *reference*. The bytes may still\nbe in the\
    \ package, and you must check the actual file rather than assume.\n\n1. **List the media parts and\
    \ grep for the bytes.** Unzip the saved .docx and look\n   inside `word/media/`. A filename check\
    \ is not enough (the name tells you\n   nothing), and a PNG's signature or logo artwork is not a reliable\
    \ literal\n   string to search for. Do both: `unzip -l file.docx | grep word/media` to see\n   what\
    \ survived, and hash/compare the parts against the original confidential\n   file's bytes. Compare\
    \ the *part contents*, not just the names.\n2. **Check the relationships.** In every part's `.rels`\n\
    \   (`word/_rels/document.xml.rels`, and each `word/_rels/headerN.xml.rels`,\n   `footerN.xml.rels`,\
    \ etc.), confirm no relationship of type\n   `.../relationships/image` still points at the confidential\
    \ media part. An\n   orphaned part is a real leak even though nothing displays it.\n3. **Understand\
    \ the two ways bytes survive.**\n   * **Orphaned part still serialised.** Some writers keep unreferenced\
    \ parts in\n      the zip. Then the fix is to drop the relationship *and* ensure the part is\n   \
    \   not written — for python-docx, `part.drop_rel(rId)` only when\n      `len(part.element.xpath(f'//a:blip[@r:embed=\"\
    {rId}\"]')) == 0`, and remember\n      `drop_rel` counts only `r:id`, never `r:embed`.\n   * **Shared\
    \ part.** If the same image bytes were deduplicated against another\n      picture (e.g. the same\
    \ logo in the header, or an identical image elsewhere),\n      the part is legitimately still referenced\
    \ — and the confidential logo is\n      genuinely still inside the file. In that case you must replace\
    \ **every**\n      `r:embed` that names it (body *and* headers/footers) with a reference to a\n  \
    \    newly added part, then drop the old rel.\n4. **Search every part, not just `document.xml`.**\
    \ Headers, footers, first-page and\n   even-page variants each have their own rels file, and an image\
    \ can live only\n   there.\n5. **Check for other residue**: leftover `w:drawing`/`w:pict` in the XML,\
    \ cached\n   `w:docPr` names/descriptions, alt-text, hyperlinks to the old file, embedded\n   OLE\
    \ objects, and any `w:fldSimple`/field results containing the content.\n6. **If you rebuilt the zip\
    \ yourself**, preserve the `ZipInfo` metadata for parts\n   you keep (name order, `date_time`, `compress_type`,\
    \ `external_attr`,\n   `create_system`) and, when editing, re-zip from the original archive rather\n\
    \   than round-tripping through `unpack.py`/`pack.py` (which rewrite XML\n   declarations and pretty-print\
    \ every part).\n7. **Final gate**: `unzip -t` the result, re-open it with python-docx, run the\n \
    \  skill's `validate.py` on an unpacked copy, and — for a confidentiality claim —\n   explicitly report\
    \ that you inspected `word/media/` and the `.rels` files and\n   found no remaining copy of the old\
    \ image. If you cannot prove that from the\n   saved artifact, say so rather than claiming the logo\
    \ is gone.\n"
