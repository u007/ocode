# Docx — Kaizen blind answer sheet (questions only)

> **CLOSED-BOOK.** Answer every question from your own knowledge alone. You MUST
> NOT open, search, or otherwise access the Kaizen corpus — `questions.yaml`,
> `questions.md`, `scores/`, `derived/`, `meta.yaml`, or any file in this repo —
> nor look the answers up online. Doing so invalidates the evaluation.
>
> Answer each question **independently** (treat every item as a fresh context —
> no memory of earlier answers). If you are unsure, say so; do not guess to look
> complete. This measures what you actually know, not what you can retrieve.
>
> **Return format** — one YAML record per question so the grader can map answers
> back by id:
>
> ```yaml
> - id: <question-id>
>   answer: |
>     <your answer>
> ```

Total questions: 29

---

### docx-model-01

What is a .docx file physically, and where does a table's data live? Name the XML elements that make up a Word table, from the table down to the text.

### docx-model-02

You search a table cell for "Widget C" by iterating paragraph runs and checking `run.text == "Widget C"`, and find nothing, although the cell clearly shows "Widget C". Why, and how do you find and replace the text reliably?

### docx-model-03

Which units does WordprocessingML use for table/cell widths and for image sizes, and how do they convert to points and inches?

### docx-locate-01

A report has several tables. How do you find "the invoice table" and the row for item "Gadget D" robustly with python-docx, and what should you check before editing?

### docx-locate-02

In python-docx, a table's first row has a cell that spans two columns. What does `row.cells` return for that row, and what goes wrong if you delete "the second cell" by index or iterate `row.cells` to count columns?

### docx-rowdel-01

How do you delete a row from a Word table with python-docx? Is there a table.delete_row() or row.delete() method?

### docx-rowdel-02

The row you must delete begins a vertical merge: its first cell has `<w:vMerge w:val="restart"/>` and the next two rows' first cells have `<w:vMerge/>`. What happens if you just remove the row, and how do you do it correctly?

### docx-rowdel-03

The invoice's Total cell contains a Word field `{ =SUM(ABOVE) }`. You delete a line-item row with python-docx and save. What does the Total show when the file is opened, and how should you handle it?

### docx-rowdel-04

To "delete" a row quickly, someone sets its height to 0 (exact) and makes its text white, or marks its runs hidden. Why is that not a deletion, and how do you prove the row is really gone?

### docx-cell-01

A header cell is bold, 9 pt, white text on dark shading. You change its text with `cell.text = "SKU"` in python-docx. What formatting survives and what is lost?

### docx-cell-02

You change Qty for one invoice line from 5 to 12. What else in the document must change, and how do you make the numbers consistent?

### docx-cell-03

The document has Track Changes turned on (w:trackRevisions in settings) and the user wants a clean edit of a cell. What happens if you just rewrite the w:t text with python-docx, and what should you consider?

### docx-relayout-01

You must insert a new row after "Service G" and before the Total row. What does python-docx's `table.add_row()` do, and how do you insert a correctly formatted row in the middle?

### docx-relayout-02

You add a "SKU" column after the Item column. What does `table.add_column(width)` do, and what must you do so the table stays inside the page margins and looks like the other columns?

### docx-relayout-03

You changed the w:gridCol widths of a table but Word still shows the old column widths. Why, and what must you update?

### docx-tblins-01

You must insert a new table right after the paragraph "Notes:" in the middle of a document. Where does python-docx's `doc.add_table()` put it, and how do you place it correctly?

### docx-tblins-02

You set `table.style = "Grid Table 4 Accent 1"` on a table added to an existing company template, and python-docx raises KeyError. Why, and how do you get a ruled table?

### docx-tblins-03

How wide is a table from `doc.add_table(rows, cols)` by default, and what should you set so it matches the existing invoice table?

### docx-imgrep-01

How does a picture in document.xml reference its image bytes, and how do you replace ONE picture (a logo) with a new file while keeping its position and size?

### docx-imgrep-02

You replace a logo by overwriting the bytes of its image part (`image_part._blob = new_bytes`). The logo changed, but so did a different picture on page 3. Why?

### docx-imgrep-03

The company logo is shown at the top of every page. `doc.inline_shapes` does not list it. Where is it, and how do you find and replace it?

### docx-imgins-01

Insert signature.png 1.5 inches wide right after the paragraph "Approved by:", keeping its aspect ratio. Where does `doc.add_picture()` put it, and how do you place it correctly?

### docx-imgins-02

You put a 3-inch-wide image into a table cell that is 1.2 inches wide. What happens, and how do you size it?

### docx-legacy-01

The user gives you report.doc (Word 97–2003) and asks you to delete a table row. Can python-docx open it? What is the workflow?

### docx-legacy-02

On macOS without LibreOffice you convert a .doc with `textutil -convert docx`. What risk does that carry for a table-editing task, and how do you detect it?

### docx-legacy-03

`soffice --headless --convert-to docx file.doc` exits immediately and no output file appears, while a LibreOffice window is open on the desktop. Why, and how do you run it reliably from a script?

### docx-verify-01

What is the safe save-and-verify procedure after editing invoice.docx with a script?

### docx-verify-02

After raw-XML edits a .docx still opens in python-docx, but Word says "Word found unreadable content". Name common structural mistakes in table edits that cause this.

### docx-verify-03

You replaced a confidential logo by removing its w:drawing and inserting a new picture. How do you make sure the old image is not still inside the .docx?
