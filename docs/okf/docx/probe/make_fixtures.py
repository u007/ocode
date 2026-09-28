#!/usr/bin/env python3
"""Build the docx probe fixtures (python-docx). Output: fixtures/invoice.docx, *.png.

Built-in traps:
  * header row: dark shading + white bold runs (new header cells must match);
  * explicit banded shading on alternate body rows (delete/add must re-band);
  * "Gadget D" split across two runs;
  * Total label merged across Item..Unit Price (gridSpan=3);
  * logo in the page HEADER (not in doc.inline_shapes), same image part reused
    as a footer badge (a blob swap changes both);
  * a page 2 that must stay unchanged.
"""
import pathlib

from docx import Document
from docx.enum.table import WD_TABLE_ALIGNMENT
from docx.enum.text import WD_ALIGN_PARAGRAPH, WD_BREAK
from docx.oxml.ns import qn
from docx.shared import Inches, Pt, RGBColor
from PIL import Image, ImageDraw

OUT = pathlib.Path(__file__).resolve().parent / "fixtures"
OUT.mkdir(exist_ok=True)

ROWS = [
    ("Widget A", 2, 10.00),
    ("Widget B", 5, 4.50),
    ("Widget C", 1, 99.00),
    ("Gadget D", 3, 12.25),
    ("Gadget E", 10, 1.10),
    ("Service F", 1, 250.00),
    ("Service G", 4, 30.00),
]
HEADER = ("Item", "Qty", "Unit Price", "Amount")
HEAD_FILL, BAND_FILL = "1F3B73", "E8EEF8"
WIDTHS = [Inches(2.9), Inches(1.0), Inches(1.3), Inches(1.3)]   # = 6.5in text width


def png(path, text, fill, size=(240, 80)):
    im = Image.new("RGB", size, fill)
    d = ImageDraw.Draw(im)
    d.rectangle([3, 3, size[0] - 4, size[1] - 4], outline="black", width=3)
    d.text((20, size[1] // 2 - 6), text, fill="black")
    im.save(path)


def shade(cell, fill):
    tcPr = cell._tc.get_or_add_tcPr()
    tcPr.append(tcPr.makeelement(qn("w:shd"), {qn("w:val"): "clear", qn("w:color"): "auto", qn("w:fill"): fill}))


def put(cell, text, bold=False, white=False, right=False, split=None):
    p = cell.paragraphs[0]
    p.alignment = WD_ALIGN_PARAGRAPH.RIGHT if right else WD_ALIGN_PARAGRAPH.LEFT
    for part in (split or [text]):
        r = p.add_run(part)
        r.font.name = "Arial"
        r.font.size = Pt(10)
        r.bold = bold or None
        if white:
            r.font.color.rgb = RGBColor(0xFF, 0xFF, 0xFF)


png(OUT / "logo.png", "ACME OLD LOGO", "#f4c542")
png(OUT / "new_logo.png", "NEW BRAND LOGO", "#42b0f4")
png(OUT / "signature.png", "Signed: J. Smith", "#ffffff", (300, 100))

doc = Document()
sec = doc.sections[0]
sec.page_width, sec.page_height = Inches(8.5), Inches(11)
sec.left_margin = sec.right_margin = Inches(1)
hp = sec.header.paragraphs[0]
hp.alignment = WD_ALIGN_PARAGRAPH.RIGHT
hp.add_run().add_picture(str(OUT / "logo.png"), width=Inches(1.5))
fp = sec.footer.paragraphs[0]
fp.add_run("Acme Pty Ltd  ")
fp.add_run().add_picture(str(OUT / "logo.png"), width=Inches(0.6))

doc.add_heading("Invoice INV-1042", level=1)
doc.add_paragraph("Bill to: Acme Pty Ltd, 1 Example Street")
doc.add_paragraph("Date: 2026-09-01")

t = doc.add_table(rows=1, cols=4)
t.style = "Table Grid"
t.alignment = WD_TABLE_ALIGNMENT.LEFT
t.autofit = False                          # fixed layout: widths hold
for j, h in enumerate(HEADER):
    c = t.rows[0].cells[j]
    shade(c, HEAD_FILL)
    put(c, h, bold=True, white=True, right=j > 0)
for i, (n, q, p) in enumerate(ROWS):
    r = t.add_row()
    vals = (n, str(q), f"{p:.2f}", f"{q * p:.2f}")
    for j, v in enumerate(vals):
        c = r.cells[j]
        if i % 2 == 1:
            shade(c, BAND_FILL)
        put(c, v, right=j > 0, split=["Gad", "get D"] if v == "Gadget D" else None)
total = sum(q * p for _, q, p in ROWS)
r = t.add_row()
lab = r.cells[0].merge(r.cells[2])
put(lab, "Total", bold=True)
put(r.cells[3], f"{total:.2f}", bold=True, right=True)
for row in t.rows:
    for j, c in enumerate(row.cells):
        c.width = WIDTHS[min(j, 3)] if c is not lab else sum(WIDTHS[:3])
for g, w in zip(t._tbl.tblGrid.findall(qn("w:gridCol")), WIDTHS):
    g.set(qn("w:w"), str(w.twips))

doc.add_paragraph("Notes: Payment due within 30 days. Bank transfer only.")
doc.add_paragraph("Approved by:")
doc.add_paragraph("Thank you for your business.")
doc.paragraphs[-1].runs[-1].add_break(WD_BREAK.PAGE)
doc.add_heading("Terms and Conditions", level=2)
for s in ("1. Goods remain the property of the seller until paid in full.",
          "2. Late payments incur interest at 2% per month.",
          "3. Disputes must be raised within 14 days of the invoice date."):
    doc.add_paragraph(s)
doc.save(OUT / "invoice.docx")
print("ok", sorted(p.name for p in OUT.iterdir()))
