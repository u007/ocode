#!/usr/bin/env python3
"""sheet.py <label> <task>...: grid of page-1 crops, rows=models, cols=tasks."""
import sys, pathlib, pymupdf
from PIL import Image, ImageDraw
SP = pathlib.Path(__file__).resolve().parent
label, tasks = sys.argv[1], sys.argv[2:]
models = ["deepseek-v4.1-flash", "space-bunny-free", "glm-5.3-flash", "mimo-v2.6-flash"]
clip = pymupdf.Rect(40, 40, 555, 560)
Z = 0.55
cw, ch = int(clip.width * Z), int(clip.height * Z)
out = Image.new("RGB", (cw * len(tasks), (ch + 14) * len(models)), "white")
d = ImageDraw.Draw(out)
for r, m in enumerate(models):
    for c, t in enumerate(tasks):
        f = SP / "runs" / label / m / t / "invoice_out.pdf"
        x, y = c * cw, r * (ch + 14)
        d.text((x + 3, y), f"{m[:14]} {t}", fill="red")
        if f.exists():
            pix = pymupdf.open(f)[0].get_pixmap(matrix=pymupdf.Matrix(Z, Z), clip=clip)
            out.paste(Image.frombytes("RGB", (pix.width, pix.height), pix.samples), (x, y + 14))
        d.rectangle([x, y, x + cw - 1, y + ch + 13], outline="grey")
out.save(SP / f"sheet-{label}-{'_'.join(tasks)}.png")
print(out.size)
