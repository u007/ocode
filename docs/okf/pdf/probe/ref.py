#!/usr/bin/env python3
"""Reference solver (known-good) and naive overlay solver (known-bad).

usage: ref.py <good|overlay> <task> <workdir>
"""
import pathlib
import sys

import pymupdf

FIX = pathlib.Path(__file__).resolve().parent / "fixtures"
NAVY = (0x1f / 255, 0x3b / 255, 0x73 / 255)
SHADE = (0xe8 / 255, 0xee / 255, 0xf8 / 255)
ROW_H = 22


def rebuild(page, header, rows, total, widths=None, font="helv", fontfile=None):
    t = page.find_tables().tables[0]
    box = pymupdf.Rect(t.bbox)
    notes = [b for b in page.get_text("blocks") if b[4].startswith("Notes:")][0]
    nrect = pymupdf.Rect(notes[:4])
    page.add_redact_annot(box + (-3, -3, 3, 3))
    page.add_redact_annot(nrect)
    page.apply_redactions(images=pymupdf.PDF_REDACT_IMAGE_NONE,
                          graphics=pymupdf.PDF_REDACT_LINE_ART_REMOVE_IF_COVERED)
    left, right = box.x0, box.x1
    ncol = len(header)
    if widths is None:
        widths = [c[2] - c[0] for c in t.rows[0].cells]
    scale = (right - left) / sum(widths)
    xs = [left]
    for w in widths:
        xs.append(xs[-1] + w * scale)
    if fontfile:
        page.insert_font(fontname=font, fontfile=fontfile)
    lines = [header] + rows + [["Total"] + [""] * (ncol - 2) + [total]]
    y = box.y0
    for i, row in enumerate(lines):
        r = pymupdf.Rect(left, y, right, y + ROW_H)
        if i == 0:
            page.draw_rect(r, color=None, fill=NAVY, width=0)
        elif i % 2 == 0:
            page.draw_rect(r, color=None, fill=SHADE, width=0)
        for j, cell in enumerate(row):
            fn = "hebo" if i == 0 or row[0] == "Total" else font
            col = (1, 1, 1) if i == 0 else (0, 0, 0)
            tl = pymupdf.get_text_length(cell, fontname="helv" if fn == font and fontfile else fn, fontsize=10)
            if fontfile and fn == font:
                tl = pymupdf.Font(fontfile=fontfile).text_length(cell, fontsize=10)
            x = xs[j] + 6 if j == 0 else xs[j + 1] - 6 - tl
            page.insert_text((x, y + 15), cell, fontname=fn, fontsize=10, color=col)
        y += ROW_H
    for k in range(len(lines) + 1):
        page.draw_line((left, box.y0 + k * ROW_H), (right, box.y0 + k * ROW_H), width=0.6)
    for x in xs:
        page.draw_line((x, box.y0), (x, y), width=0.6)
    page.insert_text((nrect.x0, y + 30 + 7), notes[4].strip(), fontname="helv", fontsize=9)
    return y + 30 + 7


def data(page):
    ex = page.find_tables().tables[0].extract()
    return ex[0], ex[1:-1], ex[-1][-1]


def good(task, page, doc):
    header, rows, total = data(page)
    if task == "delete-row":
        rows = [r for r in rows if r[0] != "Widget C"]
        rebuild(page, header, rows, "460.25")
    elif task == "edit-cell":
        rows[1] = ["Widget B", "12", "4.50", "54.00"]
        rebuild(page, header, rows, "590.75")
    elif task == "add-row":
        rows.append(["Widget H", "2", "15.00", "30.00"])
        rebuild(page, header, rows, "589.25")
    elif task == "add-column":
        rows = [[r[0], f"SKU-00{i + 1}"] + r[1:] for i, r in enumerate(rows)]
        rebuild(page, ["Item", "SKU"] + header[1:], rows, total, widths=[150, 75, 55, 90, 90])
    elif task == "insert-table":
        notes = [b for b in page.get_text("blocks") if b[4].startswith("Notes:")][0]
        y = notes[3] + 25
        page.insert_text((50, y), "Payment schedule", fontname="hebo", fontsize=11)
        y += 8
        data2 = [["Due date", "Amount"], ["2026-10-01", "279.63"], ["2026-11-01", "279.62"]]
        for i, row in enumerate(data2):
            page.insert_text((56, y + 15), row[0], fontname="hebo" if i == 0 else "helv", fontsize=10)
            page.insert_text((256, y + 15), row[1], fontname="hebo" if i == 0 else "helv", fontsize=10)
            page.draw_line((50, y), (350, y), width=0.6)
            y += ROW_H
        page.draw_line((50, y), (350, y), width=0.6)
        for x in (50, 250, 350):
            page.draw_line((x, y - 3 * ROW_H), (x, y), width=0.6)
    elif task == "edit-cell-subset":
        rows[3][0] = "Quokka Kit"
        rebuild(page, header, rows, total, font="geor", fontfile="/System/Library/Fonts/Supplemental/Georgia.ttf")
    elif task == "replace-image":
        xref = page.get_images()[0][0]
        page.replace_image(xref, filename=str(FIX / "new_logo.png"))
    elif task == "insert-image":
        page.insert_image(pymupdf.Rect(395.28, 700, 545.28, 750), filename=str(FIX / "signature.png"))


def overlay(task, page, doc):
    """What a naive editor does: paint white boxes, type on top. Text stays underneath."""
    if task in ("delete-row",):
        r = page.search_for("Widget C")[0]
        page.draw_rect(pymupdf.Rect(50, r.y0 - 4, 545, r.y1 + 4), color=None, fill=(1, 1, 1))
    elif task in ("edit-cell", "edit-cell-subset", "add-row", "add-column", "insert-table"):
        target = {"edit-cell": "22.50", "edit-cell-subset": "Gadget D", "add-row": "559.25",
                  "add-column": "Qty", "insert-table": "Notes:"}[task]
        r = page.search_for(target)[0]
        page.draw_rect(r, color=None, fill=(1, 1, 1))
        page.insert_text((r.x0, r.y1 - 2), "54.00", fontsize=10)
    elif task == "replace-image":
        page.insert_image(pymupdf.Rect(425.28, 50, 545.28, 90), filename=str(FIX / "new_logo.png"))
    elif task == "insert-image":
        page.insert_image(pymupdf.Rect(50, 170, 200, 220), filename=str(FIX / "signature.png"))


def main():
    mode, task, work = sys.argv[1], sys.argv[2], pathlib.Path(sys.argv[3])
    src = work / ("invoice_subset.pdf" if task == "edit-cell-subset" else "invoice.pdf")
    doc = pymupdf.open(src)
    (good if mode == "good" else overlay)(task, doc[0], doc)
    doc.save(work / "invoice_out.pdf", garbage=3, deflate=True)


if __name__ == "__main__":
    main()
