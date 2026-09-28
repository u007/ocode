#!/usr/bin/env python3
"""Build the PDF probe fixtures (reportlab). Output: fixtures/*.pdf, *.png.

invoice.pdf        Helvetica (base-14), ruled table near full width, logo, page 2.
invoice_subset.pdf same layout, table body in a subset-embedded TTF (Georgia).
"""
import pathlib

from PIL import Image, ImageDraw
from reportlab.lib import colors
from reportlab.lib.pagesizes import A4
from reportlab.pdfbase import pdfmetrics
from reportlab.pdfbase.ttfonts import TTFont
from reportlab.pdfgen import canvas

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
W, H = A4
LEFT, RIGHT = 50, W - 50           # table spans the full text width
COLS = [LEFT, LEFT + 215, LEFT + 290, LEFT + 395, RIGHT]
TOP = H - 170
ROW_H = 22


def png(path, text, fill, size=(240, 80)):
    im = Image.new("RGB", size, fill)
    d = ImageDraw.Draw(im)
    d.rectangle([3, 3, size[0] - 4, size[1] - 4], outline="black", width=3)
    d.text((20, size[1] // 2 - 6), text, fill="black")
    im.save(path)


def build(path, body_font):
    c = canvas.Canvas(str(path), pagesize=A4)
    c.setTitle("Invoice INV-1042")
    c.drawImage(str(OUT / "logo.png"), RIGHT - 120, H - 90, width=120, height=40)
    c.setFont("Helvetica-Bold", 18)
    c.drawString(LEFT, H - 80, "Invoice INV-1042")
    c.setFont("Helvetica", 10)
    c.drawString(LEFT, H - 110, "Bill to: Acme Pty Ltd, 1 Example Street")
    c.drawString(LEFT, H - 124, "Date: 2026-09-01")

    lines = [HEADER] + [
        (n, str(q), f"{p:.2f}", f"{q * p:.2f}") for n, q, p in ROWS
    ]
    total = sum(q * p for _, q, p in ROWS)
    lines.append(("Total", "", "", f"{total:.2f}"))
    y = TOP
    for i, row in enumerate(lines):
        if i == 0:
            c.setFillColor(colors.HexColor("#1f3b73"))
            c.rect(LEFT, y - ROW_H, RIGHT - LEFT, ROW_H, stroke=0, fill=1)
            c.setFillColor(colors.white)
            c.setFont("Helvetica-Bold", 10)
        else:
            if i % 2 == 0:
                c.setFillColor(colors.HexColor("#e8eef8"))
                c.rect(LEFT, y - ROW_H, RIGHT - LEFT, ROW_H, stroke=0, fill=1)
            c.setFillColor(colors.black)
            bold = row[0] == "Total"
            c.setFont("Helvetica-Bold" if bold else body_font, 10)
        for j, cell in enumerate(row):
            if j == 0:
                c.drawString(COLS[0] + 6, y - 15, cell)
            else:
                c.drawRightString(COLS[j + 1] - 6, y - 15, cell)
        y -= ROW_H
    # ruling: horizontal + vertical lines
    c.setStrokeColor(colors.black)
    c.setLineWidth(0.6)
    for k in range(len(lines) + 1):
        c.line(LEFT, TOP - k * ROW_H, RIGHT, TOP - k * ROW_H)
    for x in COLS:
        c.line(x, TOP, x, TOP - len(lines) * ROW_H)

    c.setFont("Helvetica", 9)
    c.setFillColor(colors.black)
    ny = TOP - len(lines) * ROW_H - 30
    c.drawString(LEFT, ny, "Notes: Payment due within 30 days. Bank transfer only.")
    c.drawString(LEFT, 60, "Thank you for your business.")
    c.showPage()

    c.setFont("Helvetica-Bold", 14)
    c.drawString(LEFT, H - 80, "Terms and Conditions")
    c.setFont("Helvetica", 10)
    for i, t in enumerate([
        "1. Goods remain the property of the seller until paid in full.",
        "2. Late payments incur interest at 2% per month.",
        "3. Disputes must be raised within 14 days of the invoice date.",
    ]):
        c.drawString(LEFT, H - 110 - i * 16, t)
    c.showPage()
    c.save()


png(OUT / "logo.png", "ACME OLD LOGO", "#f4c542")
png(OUT / "new_logo.png", "NEW BRAND LOGO", "#42b0f4")
png(OUT / "signature.png", "Signed: J. Smith", "#ffffff", (300, 100))
pdfmetrics.registerFont(TTFont("Georgia", "/System/Library/Fonts/Supplemental/Georgia.ttf"))
build(OUT / "invoice.pdf", "Helvetica")
build(OUT / "invoice_subset.pdf", "Georgia")
print("ok", sorted(p.name for p in OUT.iterdir()))
