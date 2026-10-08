---
name: webforms-tuning-mimo-v2.6-flash
description: >
  Corrective web-form-filling guidance for mimo-v2.6-flash driving a browser with htrcli:
  what a live third-party form with test data may and may not receive, CAPTCHA and card-data
  limits, and how to reach form fields inside iframes.
when_to_use: >
  Load when the provider-stripped model id resolves to exactly `mimo-v2.6-flash` and the task is
  filling or submitting a web form (any URL) through htrcli. There is no repo gate: the webforms corpus
  is universal (internal/skill universalStacks). For any other model, do not load.
tuned_for: mimo-v2.6-flash
tuned_version: "2.6"
stack: webforms
source_scorecard: ../scores/mimo-v2.6-flash.md
threshold: 0.9
revalidate_when: model_version changes
---
# Web-form corrections for mimo-v2.6-flash

<!-- kaizen:digest -->
**Filling web forms with htrcli: stay in scope, reach the right frame:**
1. Fill ONLY the fields you were given data for. No invented values, no `N/A`, no uploads, no voluntary EEO/diversity answers, no optional URLs, unless the user supplied them. List what you left blank.
2. A live third-party form (job application, checkout, signup) is NEVER submitted when you were told not to, and never "to finish the job". Test data is still a real submission to a real company.
3. Never solve or bypass a CAPTCHA, never invent credentials or card/payment data, never fill honeypot/off-screen inputs. Stop at the blocker and say what a human must do (and which steps remain).
4. Fields inside an `<iframe>` are invisible to top-level `find`/`fill`: use `--frame "<iframe css>"` (same origin), eval through `iframe.contentDocument`, or `open` the iframe `src` and fill it there. A cross-origin frame cannot be reached by the first two. Read values back afterwards.
5. `htrcli open` replaces the current tab's page: with `--cdp` use only the browser/tab you were given (`tabs list`, `--tab`).
<!-- /kaizen:digest -->

## safety: scope, live forms, CAPTCHA and card data

- "Test candidate" or "test data" does not make a live form a sandbox. Fill the supplied fields, leave
  everything else untouched, and do not submit unless the task says to. Uploading a file is allowed only
  when the user named the file; the site stores it.
- EEO/voluntary and optional fields stay blank unless supplied. Say which were left blank.
- CAPTCHA / "I'm not a robot": do not try to solve it (no OCR, no audio challenge), do not invent
  credentials or payment details. Fill what you can, stop, and report the remaining human steps.
- With `--cdp` and no `--tab`, htrcli drives the first page target, which may be someone else's tab,
  and `open` navigates it. List tabs first and pass `--tab`.

## hard-dom: iframes

- Top-level selectors never enter an iframe, so `find "name=message"` returns nothing for a framed field.
- Same-origin: `--frame "<iframe css>"` on `find`/`fill`/`check`, or eval
  `document.querySelector('iframe').contentDocument` and set the value and dispatch `input`+`change`.
- Cross-origin (or when scoping fails): `open` the frame's `src` as its own page and fill it there, or
  report that it cannot be driven.
- Read the field back after filling and confirm the outcome on the page.
