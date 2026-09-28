# Pdf — Kaizen blind answer sheet (questions only)

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

Total questions: 36

---

### pdf-model-01

A user asks you to "delete the third row of the table" in an existing PDF. What does a PDF actually contain for that table, and what does that imply about how the edit must be done?

### pdf-model-02

A quick way to "remove" text from a PDF is to draw a white rectangle over it and type the new text on top. Why is that not an edit, and what should be used instead?

### pdf-model-03

You want to edit PDF text by modifying the raw content stream (e.g. with pikepdf) and replace the string "22.50" with "54.00". Why might a plain search-and-replace on the stream bytes fail or corrupt the page?

### pdf-model-04

In PyMuPDF you read a word's bbox from `page.get_text("words")` and want to draw at that position. How do PyMuPDF coordinates relate to raw PDF coordinates, and what changes if the page has a /Rotate or a CropBox that is offset from the MediaBox?

### pdf-locate-01

How do you programmatically find a table in an existing PDF page and get the bounding box of each row and cell (not just the text)?

### pdf-locate-02

You have the text of the row you want ("Widget C"). Why is `page.search_for("Widget C")` alone not enough to define what to remove, and how do you get the full row region?

### pdf-locate-03

Before replacing a cell's text you want to match its original style. How do you read the font, size, colour and baseline of existing text in PyMuPDF?

### pdf-locate-04

You need to know the table's column boundaries and the x position where each right-aligned number ends, so that new or edited values line up. Where do you get that information?

### pdf-rowdel-01

Outline a correct procedure to delete one row from a ruled, shaded table in an existing PDF (rows below must move up, the Total must be updated) using PyMuPDF.

### pdf-rowdel-02

In PyMuPDF, what do the default arguments of `page.apply_redactions()` do to images and vector graphics, and why does that matter when you redact a table row or cell?

### pdf-rowdel-03

Instead of re-typing every row below a deleted row, is there a way in PyMuPDF to move an existing region of a page up, preserving its exact appearance?

### pdf-rowdel-04

After deleting a row from an invoice table, what else in the document is likely to need changing besides the row itself?

### pdf-cell-01

Replace one right-aligned number in a table cell (e.g. "22.50" → "54.00") in an existing PDF with PyMuPDF so it looks native. Give the steps.

### pdf-cell-02

PyMuPDF's `apply_redactions` removes "characters whose bbox overlaps the redaction rectangle". How should you size the rectangle when redacting one cell of a tight table, and why?

### pdf-cell-03

The new cell text is longer than the column is wide. What does PyMuPDF's `insert_textbox` do when the text does not fit, and what are your options?

### pdf-cell-04

`page.insert_text(point, text)` — what does `point` refer to, and what goes wrong if you pass the top-left corner of the old word's bbox?

### pdf-relayout-01

Add a new row to an existing ruled table just above its Total row in a PDF. What must happen to the Total row, the table rules and any content below the table?

### pdf-relayout-02

Add a new column to a table that already spans the full width between the page margins. What is the correct approach and what is the common mistake?

### pdf-relayout-03

Someone proposes: convert the PDF to DOCX (or HTML), edit the table there, and convert back to PDF. When is that acceptable and what are the risks?

### pdf-relayout-04

How do you check whether there is enough empty space on a page to grow a table or place new content without overlapping anything?

### pdf-tblins-01

Insert a new small ruled table (header + 2 rows) into free space on page 1 of an existing PDF. Describe two practical ways to do it in Python.

### pdf-tblins-02

When drawing a new table into an existing page, how do you make it look consistent with the document and avoid visual glitches?

### pdf-tblins-03

The new table must go between two existing paragraphs, but there is no gap there. What are your options?

### pdf-imgrep-01

Replace an existing logo image on page 1 of a PDF with a new PNG at the same position and size using PyMuPDF. How, and what is the catch with shared images?

### pdf-imgrep-02

You put a new image on top of an old logo (insert_image at the same rect) and it looks right. What is wrong with that?

### pdf-imgrep-03

`page.get_images()` shows no image, yet the logo is visible on the page. What could be going on, and how do you find and replace it?

### pdf-imgins-01

Insert a signature PNG at the bottom right of page 1, 150pt wide, keeping its aspect ratio, without covering text. How do you compute the rect and insert it with PyMuPDF?

### pdf-imgins-02

You insert the same logo on every page of a 200-page PDF with insert_image and the file grows by 200 copies of the image. How do you avoid that?

### pdf-imgins-03

You inserted an image with PyMuPDF but it does not appear, or appears underneath a filled background box. Name the likely causes.

### pdf-fonts-01

The table text uses an embedded subset font (name like "AAAAAA+Georgia"). You change an item name to text with letters that never appeared in the document. Why can reusing the embedded font fail, and what should you do?

### pdf-fonts-02

What are PyMuPDF's built-in base-14 font names for Helvetica regular and bold, and what limitation do base-14 fonts have for non-Latin or special characters?

### pdf-fonts-03

You edited a bold header cell but your new text comes out in regular weight and slightly different size. How do you detect and match the original weight and size?

### pdf-verify-01

After editing a table in a PDF, how do you verify the edit actually worked (not just that the script ran)?

### pdf-verify-02

You removed a confidential row with redaction and saved with `doc.save("same.pdf", incremental=True)`. Is the row gone from the file?

### pdf-verify-03

A user asks you to edit invoice.pdf. What file-safety practices should you follow?

### pdf-verify-04

You wrote a Python script that edits a PDF. The script ran without errors and printed "done". Why is that not evidence the PDF is correct?
