# Pptx — Kaizen blind answer sheet (questions only)

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

Total questions: 27

---

### pptx-model-01

How is a table stored in a .pptx slide, and which XML elements hold its columns, rows, cells and text?

### pptx-model-02

What units does python-pptx / DrawingML use for positions and sizes, and what are the slide's dimensions in a default 16:9 deck?

### pptx-model-03

A company logo appears on every slide, but iterating `slide.shapes` on any slide does not find it. Where is it, and what are the consequences of editing it there?

### pptx-locate-01

How do you find the table on slide 3 and the row for "Gadget D" with python-pptx?

### pptx-locate-02

In a pptx table the Total label spans the first two columns. How is a merge represented, and what must you watch when editing or deleting cells around it?

### pptx-rowdel-01

How do you delete a row from a pptx table with python-pptx? Is there a table.rows.remove() or a delete API?

### pptx-rowdel-02

After removing an a:tr from a table, the graphicFrame's height (p:xfrm ext cy) is unchanged. Does that matter, and how do you fix it?

### pptx-rowdel-03

The table alternates row shading. After deleting a middle row, two adjacent rows have the same colour. Why might that happen in one deck and not another?

### pptx-rowdel-04

Instead of deleting a row, someone drags a white rectangle over it, or sets the row's text to white and its height to near zero. Why is that wrong?

### pptx-cell-01

A header cell has bold 14 pt white text. What happens to its formatting when you do `cell.text = "SKU"` in python-pptx, and how do you keep it?

### pptx-cell-02

You replace "Gadget D" with a much longer product name in a cell. What happens to the table's layout in PowerPoint, and how do you control it?

### pptx-cell-03

You change one line item's Qty from 5 to 12. What else must you update in the deck?

### pptx-relayout-01

Add a row after "Service G" and before the Total row of a pptx table, keeping the formatting. How?

### pptx-relayout-02

Add a "SKU" column after the Item column in a pptx table that already spans nearly the whole slide width. What XML changes are required, and how do you keep the table on the slide?

### pptx-tblins-01

Insert a new 3×2 table on an existing slide without overlapping existing content. How do you pick the position and create it with python-pptx?

### pptx-tblins-02

A table added with `add_table` looks different from the deck's existing tables (a blue default style). How do you make it match?

### pptx-tblins-03

The slide layout has an empty content placeholder where the table should go. What is the idiomatic way to put a table there?

### pptx-imgrep-01

python-pptx has no picture.replace(). How do you replace one picture on a slide with a new image file at the same position and size, and keep its stacking order?

### pptx-imgrep-02

To replace a logo you overwrite the picture's image-part bytes. The logo on slide 1 changes, but so does the one on slide 7, which should have stayed. Why?

### pptx-imgrep-03

After removing an old picture you call `slide.part.drop_rel(rId)` to get its image out of the file. Another picture on the same slide used the same image and is now broken. Why did python-pptx drop a relationship that was still in use?

### pptx-imgins-01

Insert signature.png 2 inches wide at the bottom-right of a slide, keeping its aspect ratio, without covering existing content. How?

### pptx-imgins-02

You insert a portrait photo into a picture placeholder with `placeholder.insert_picture(path)`. Part of the photo disappears. Why?

### pptx-legacy-01

The user gives you deck.ppt (PowerPoint 97–2003) and asks you to change a table. Can python-pptx open it? What is the workflow?

### pptx-legacy-02

After converting a .ppt to .pptx with LibreOffice, what should you check before and after editing, and what must you tell the user?

### pptx-verify-01

What is the safe save-and-verify procedure after editing deck.pptx with a script?

### pptx-verify-02

After XML edits to a table, PowerPoint says the file "needs to be repaired". Name common causes.

### pptx-verify-03

You removed a confidential image from a slide by deleting its p:pic element. Is it gone from the .pptx? How do you check?
