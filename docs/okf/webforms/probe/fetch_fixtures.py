#!/usr/bin/env python3
"""Fetch the two real public sample forms and adapt them for the local probe server.

pages/pizza.html    httpbin.org/forms/post (HTML5-spec pizza order: text, tel, email, radio,
                    checkbox group, time with min/max/step, textarea)
pages/webform.html  selenium.dev/selenium/web-form.html (text, password, textarea, disabled and
                    readonly inputs, select, datalist, file, checkboxes, radios, colour, bootstrap
                    datepicker text input, range, hidden)
Both are pinned by sha256 of the UPSTREAM bytes; a mismatch fails loudly because the form was
revised. Adaptations (the only edits): action -> /submit, and `value` attributes added to the
selenium checkboxes/radios, which upstream submits as an indistinguishable "on".
The authored hard-case pages (wizard, custom, iframe, validation, payment, upload) are committed.
"""
import hashlib
import pathlib
import subprocess

PAGES = pathlib.Path(__file__).resolve().parent / "pages"
PINNED = {
    "pizza.html": ("https://httpbin.org/forms/post", "d9cd9adbe7554d4e82a597d722dccdcea70c8dd918836670e3f9687531f54d74"),
    "webform.html": ("https://www.selenium.dev/selenium/web/web-form.html", "fbf64bd0731a21f7e77abfde1b3bfc01377215783952facaccc8ed5eb105c6c3"),
}


def main():
    PAGES.mkdir(exist_ok=True)
    for name, (url, want) in PINNED.items():
        # curl, not urllib: some hosts answer 403 to Python's TLS fingerprint.
        raw = subprocess.run(["curl", "-sfL", "-m", "30", url], capture_output=True, check=True).stdout
        got = hashlib.sha256(raw).hexdigest()
        if got != want:
            raise SystemExit(f"{name}: upstream sha256 {got} != pinned {want} (form revised; re-verify the probe)")
        html = raw.decode()
        if name == "pizza.html":
            html = html.replace('action="/post"', 'action="/submit"')
        else:
            html = html.replace('action="submitted-form.html"', 'action="/submit"')
            for i, v in (("my-check-1", "c1"), ("my-check-2", "c2"), ("my-radio-1", "r1"), ("my-radio-2", "r2")):
                html = html.replace(f'id="{i}"', f'id="{i}" value="{v}"')
        (PAGES / name).write_text(html)
    import pymupdf  # receipt.pdf for the upload task (content hash is what the checker compares)
    fx = PAGES.parent / "fixtures"
    fx.mkdir(exist_ok=True)
    d = pymupdf.open()
    d.new_page().insert_text((72, 100), "Receipt #4471  Total 42.50", fontsize=14)
    d.save(fx / "receipt.pdf")
    r = pymupdf.open()  # dummy fictional resume for the Greenhouse task
    pg = r.new_page()
    for i, line in enumerate(["Jordan Testwell", "Senior Software Engineer (test candidate, fictional)", "jordan.testwell@example.com", "Experience: 3+ years building web applications."]):
        pg.insert_text((72, 100 + 22 * i), line, fontsize=13)
    r.save(fx / "resume.pdf")
    print("fixtures ok")


if __name__ == "__main__":
    main()
