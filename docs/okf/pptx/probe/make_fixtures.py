#!/usr/bin/env python3
"""Build the pptx probe fixtures (python-pptx). Output: fixtures/deck.pptx, *.png.

Built-in traps:
  * the table spans nearly the full slide width (a new column must rebalance);
  * header row: explicit dark fill + white bold runs;
  * explicit banded fills on body rows (delete/add must re-band);
  * "Gadget D" split across two runs;
  * Total label merged across Item..Unit Price (gridSpan=3 + hMerge);
  * a Notes box directly below the table (a new row must push it down);
  * the same logo image on slides 1 and 2 (one shared image part);
  * slide 2 must stay unchanged.
"""
import pathlib

from PIL import Image, ImageDraw
from pptx import Presentation
from pptx.dml.color import RGBColor
from pptx.enum.text import PP_ALIGN
from pptx.oxml.ns import qn
from pptx.util import Inches, Pt

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
M = Inches(0.6)
WIDTHS = [Inches(5.333), Inches(2.0), Inches(2.4), Inches(2.4)]    # = 12.133in
ROW_H = Inches(0.36)
TABLE_GRID = "{5940675A-B579-460E-94D1-54222C63F5DA}"               # No Style, Table Grid


def png(path, text, fill, size=(240, 80)):
    im = Image.new("RGB", size, fill)
    d = ImageDraw.Draw(im)
    d.rectangle([3, 3, size[0] - 4, size[1] - 4], outline="black", width=3)
    d.text((20, size[1] // 2 - 6), text, fill="black")
    im.save(path)


def textbox(slide, x, y, w, h, text, size=14, bold=False):
    tb = slide.shapes.add_textbox(x, y, w, h)
    tf = tb.text_frame
    tf.word_wrap = True
    r = tf.paragraphs[0].add_run()
    r.text = text
    r.font.size, r.font.bold, r.font.name = Pt(size), bold, "Arial"
    return tb


def put(cell, text, fill, bold=False, white=False, right=False, split=None):
    cell.fill.solid()
    cell.fill.fore_color.rgb = RGBColor.from_string(fill)
    p = cell.text_frame.paragraphs[0]
    p.alignment = PP_ALIGN.RIGHT if right else PP_ALIGN.LEFT
    for part in (split or [text]):
        r = p.add_run()
        r.text = part
        r.font.name, r.font.size, r.font.bold = "Arial", Pt(12), bold
        if white:
            r.font.color.rgb = RGBColor(0xFF, 0xFF, 0xFF)


png(OUT / "logo.png", "ACME OLD LOGO", "#f4c542")
png(OUT / "new_logo.png", "NEW BRAND LOGO", "#42b0f4")
png(OUT / "signature.png", "Signed: J. Smith", "#ffffff", (300, 100))

prs = Presentation()
prs.slide_width, prs.slide_height = Inches(13.333), Inches(7.5)
blank = prs.slide_layouts[6]

s1 = prs.slides.add_slide(blank)
textbox(s1, M, Inches(0.4), Inches(7), Inches(0.6), "Invoice INV-1042", 28, True)
s1.shapes.add_picture(str(OUT / "logo.png"), prs.slide_width - M - Inches(1.5), Inches(0.4), width=Inches(1.5))
textbox(s1, M, Inches(1.05), Inches(7), Inches(0.5), "Bill to: Acme Pty Ltd, 1 Example Street · Date: 2026-09-01")

lines = [HEADER] + [(n, str(q), f"{p:.2f}", f"{q * p:.2f}") for n, q, p in ROWS]
total = sum(q * p for _, q, p in ROWS)
lines.append(("Total", "", "", f"{total:.2f}"))
gf = s1.shapes.add_table(len(lines), 4, M, Inches(1.7), sum(WIDTHS), ROW_H * len(lines))
t = gf.table
gf._element.graphic.graphicData.tbl.tblPr.find(qn("a:tableStyleId")).text = TABLE_GRID
t.first_row = t.horz_banding = False
for j, w in enumerate(WIDTHS):
    t.columns[j].width = w
for i, row in enumerate(lines):
    t.rows[i].height = ROW_H
    for j, v in enumerate(row):
        if i == len(lines) - 1 and j in (1, 2):
            continue
        fill = "1F3B73" if i == 0 else ("E8EEF8" if i % 2 == 0 else "FFFFFF")
        put(t.cell(i, j), v, fill, bold=i == 0 or row[0] == "Total", white=i == 0, right=j > 0,
            split=["Gad", "get D"] if v == "Gadget D" else None)
t.cell(len(lines) - 1, 0).merge(t.cell(len(lines) - 1, 2))
bottom = gf.top + gf.height
textbox(s1, M, bottom + Inches(0.15), Inches(7), Inches(0.4), "Notes: Payment due within 30 days. Bank transfer only.", 12)
textbox(s1, M, Inches(6.8), Inches(7), Inches(0.4), "Thank you for your business.", 12)

s2 = prs.slides.add_slide(blank)
textbox(s2, M, Inches(0.4), Inches(9), Inches(0.6), "Terms and Conditions", 24, True)
s2.shapes.add_picture(str(OUT / "logo.png"), prs.slide_width - M - Inches(1.5), Inches(0.4), width=Inches(1.5))
textbox(s2, M, Inches(1.3), Inches(11), Inches(2),
        "1. Goods remain the property of the seller until paid in full.\n"
        "2. Late payments incur interest at 2% per month.\n"
        "3. Disputes must be raised within 14 days of the invoice date.", 16)
prs.save(OUT / "deck.pptx")
print("ok", sorted(p.name for p in OUT.iterdir()), "table bottom in", bottom / 914400)
