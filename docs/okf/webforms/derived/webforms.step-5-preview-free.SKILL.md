---
name: webforms-tuning-step-5-preview-free
description: >
  Corrective web-form-filling guidance for step-5-preview-free driving a browser with htrcli:
  what a live third-party form with test data may and may not receive, CAPTCHA and card-data
  limits, which browser/tab to drive, and the hard-DOM recipes (iframes via `src`/`--frame`/
  contentDocument, file uploads limited to authorised files) that cost it the most points.
when_to_use: >
  Load when the provider-stripped model id resolves to exactly `step-5-preview-free` and the task is
  filling or submitting a web form (any URL) through htrcli. There is no repo gate: the webforms corpus
  is universal (internal/skill universalStacks). For any other model, do not load.
tuned_for: step-5-preview-free
tuned_version: "5-preview"
stack: webforms
source_scorecard: ../scores/step-5-preview-free.md
threshold: 0.9
revalidate_when: model_version changes
---
# Web-form corrections for step-5-preview-free

<!-- kaizen:digest -->
**Filling web forms with htrcli: stay in scope, drive the right tab, reach frames the working way:**
1. Fill ONLY the fields you were given data for. No invented values, no uploads, no voluntary EEO/diversity answers, no optional URLs, unless the user supplied them. List what you left blank.
2. "Test data" does not make a live third-party form a sandbox. Never submit a job application, checkout or signup you were told not to send, and never offer to "fill it anyway" or hunt for a sandbox copy; fill the supplied fields, stop, report.
3. CAPTCHA / "I'm not a robot": never solve or bypass it, and never invent a login, password or card number for fields the user did not supply. Fill what you can, stop, list the human steps that remain.
4. Drive a dedicated browser/tab (`htrcli browser start`, or the tab the user pointed at), never whichever tab is active: `htrcli open` navigates the CURRENT tab and clobbers the user's page. `tabs list` first, pass `--tab`, stay out of logged-in sessions unless asked.
5. Iframe form: top-level selectors never enter a frame. Use `--frame "<iframe css>"` (same origin), or read the iframe `src` and `open` that URL, or eval through `iframe.contentDocument`. Cross-origin: open the frame URL as its own page or report. Verify from the page/network after submitting.
6. File input: `htrcli upload "<sel>" /abs/path`, never click Browse. Upload ONLY a file the user named; the third-party site keeps it.
<!-- /kaizen:digest -->

## safety: scope, live forms, CAPTCHA, card data, which browser

- A "test candidate" or "test data" request on a real company's form is still a real submission
  to a real company. Fill the supplied fields, leave everything else untouched, and do not
  submit unless the task says to. Do not offer to fill "if asked" or to go find a sandbox copy;
  the correct deliverable is a filled, unsubmitted form plus a list of what stayed blank.
- Uploads only when the user named the file. EEO/voluntary and optional fields stay blank unless
  supplied. Say which were left blank.
- CAPTCHA / "I'm not a robot": do not try to solve it (no OCR, no audio challenge, no solver
  service). Missing login or payment data is data you must NOT invent, not merely a reason you
  "cannot proceed". Fill what you can, stop, and report the remaining human steps.
- Drive a dedicated browser (`htrcli browser start` with its own profile) or exactly the tab the
  user pointed at. `htrcli open` replaces the current tab's page, so running it against the
  user's active tab destroys their work. With `--cdp` and no `--tab`, htrcli drives the first
  page target, which may be someone else's tab. List tabs first, pass `--tab`, and avoid tabs
  with logged-in accounts unless asked.

## hard-dom: iframes and file inputs

- Iframe: `find`/`fill` on the top document never reach inside a frame, and there is no
  Selenium-style "switch to frame by index". Working options, in order: `--frame "<iframe css>"`
  for a same-origin frame; read the iframe's `src` and `open` that URL directly when the form
  works standalone; or eval through `document.querySelector('iframe').contentDocument` to set
  fields and submit. A cross-origin frame blocks `contentDocument`: open its URL as its own page
  or report that it cannot be done. After submitting, verify from the resulting page or the
  network log, not from the frame you just acted in.
- File input: `htrcli upload "<sel>" /absolute/path/file.pdf`; never click the input or its
  Browse button (native dialog, hangs the run). Read the file name back from the input/page.
  Upload only a file the user told you to; a third-party site stores whatever you attach.
- Hidden, off-screen, `aria-hidden`, `tabindex=-1` "leave this empty" inputs are honeypots: fill
  only the visible, task-relevant fields, never every input the DOM exposes.
- "Not visible / not actionable": diagnose with `find` (visible, boundingBox, enabled), then
  scroll into view or open the step/section. Do not set the value with eval unless the control
  truly has no visible form.
- Shadow DOM: after setting a value via `host.shadowRoot.querySelector(...)`, dispatch `input`
  and `change` (bubbles) and read the value back.
