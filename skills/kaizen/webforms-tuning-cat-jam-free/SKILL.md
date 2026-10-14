---
name: webforms-tuning-cat-jam-free
description: >
  Corrective web-form-filling guidance for cat-jam-free driving a browser with htrcli: scope on
  live third-party forms, CAPTCHA and card-data limits, which browser/tab to drive, state-aware
  checkbox/datalist handling, iframe and upload recipes, selector confirmation, and the
  key-driven react-select path.
when_to_use: >
  Load when the provider-stripped model id resolves to exactly `cat-jam-free` and the task is
  filling or submitting a web form (any URL) through htrcli. There is no repo gate: the webforms
  corpus is universal (internal/skill universalStacks). For any other model, do not load.
tuned_for: cat-jam-free
tuned_version: "alpha"
stack: webforms
source_scorecard: ../scores/cat-jam-free.md
threshold: 0.9
revalidate_when: model_version changes
---
# Web-form corrections for cat-jam-free

<!-- kaizen:digest -->
**Filling web forms with htrcli: stay in scope, drive the right tab, confirm every target and state:**
1. Live third-party form with "test" data: do the job, but only the supplied part. Fill the supplied fields, leave uploads, voluntary EEO questions and everything else blank, do not submit, and list what you left blank. Do not refuse the whole task or send the user to a "test environment".
2. CAPTCHA: never solve or bypass it, and never invent a login, password or card number. Fill what you can, stop, list the remaining human steps.
3. Drive a dedicated browser (`htrcli browser start`) or the tab the user named. `htrcli open` replaces the current tab's page. `tabs list` first, pass `--tab`, stay out of logged-in sessions unless asked.
4. Honeypot (hidden / off-screen / `aria-hidden` / "leave this empty" input): leave it empty AND state the rule behind it: fill only visible, task-relevant fields, never every input the DOM exposes.
5. Before acting, confirm each selector with `htrcli find` and note required fields and the submit control.
6. Checkbox: read `.checked` first, `check`/`uncheck`, read it back. Datalist: it is a plain text input, `fill` the text.
7. Iframe: `--frame "<iframe css>"`, or read the iframe `src` and `open` it, or eval via `contentDocument`; cross-origin: open the frame URL or report. Verify the outcome after submit.
8. Upload: `htrcli upload "<sel>" /abs/path`, read the file name back, upload only files the user named.
9. react-select: focus the combobox input, `press ArrowDown`, press the option letters, `press Enter`; verify `.select__single-value`.
<!-- /kaizen:digest -->

## safety: scope, live forms, CAPTCHA, card data, which browser

- A "test candidate" on a real company's form: the deliverable is a filled, unsubmitted form.
  Fill only fields the user supplied data for. No invented values, no file uploads, no
  voluntary EEO/diversity answers unless supplied. Do not submit. Report which fields stayed
  blank. Refusing outright or redirecting to "an isolated test environment" is not the answer.
- CAPTCHA / "I'm not a robot": no solving, no OCR/audio challenge, no solver service. Missing
  login or payment data must never be invented. Fill the rest, stop, and list the steps a human
  must still complete.
- Drive a dedicated browser (`htrcli browser start`, own profile) or exactly the tab the user
  pointed at. `htrcli open` navigates the current tab and destroys whatever page the user had
  there. Run `htrcli tabs list` first, target with `--tab`, avoid tabs with logged-in accounts
  unless asked.
- Hidden, off-screen, `aria-hidden`, `tabindex=-1` "leave this empty" inputs are honeypots. The
  rule behind it: fill only fields a human can see and the task asks for, never every input the
  DOM exposes.

## choice-input: state-aware choices

- Checkbox: read the current state (`htrcli find` or eval `.checked`), then `htrcli check` /
  `htrcli uncheck` (idempotent; `click` toggles), then read the state back.
- Datalist (`<input list=...>`): a plain text input. The browser's suggestion popup is not in the
  DOM and cannot be clicked. `htrcli fill` the exact suggestion text; that text is what submits.
  Read it back with `htrcli value`.
- Radio: target the input by value or label and `htrcli check` it, then confirm which one in the
  group is checked.

## hard-dom: iframes and uploads

- Iframe: top-level selectors never enter a frame, and there is no frame index to "switch" to.
  Options: `--frame "<iframe css>"` for a same-origin frame; read the iframe `src` and `open` that
  URL when the form works standalone; or eval through
  `document.querySelector('iframe').contentDocument` to set fields and submit. A cross-origin
  frame blocks `contentDocument`: open its URL as its own page, or report it. After submitting,
  verify from the resulting page or the network log.
- File input: `htrcli upload "<sel>" /absolute/path/file.pdf`; never click the input. Read the
  attached file name back from the input/page. Upload only a file the user named; the
  third-party site keeps it.

## discover: confirm targets before acting

- After the snapshot, list the controls (`findAll "input,select,textarea,button"` or an eval
  returning tag, type, name, id, label, required) and note which are required and which control
  submits.
- Confirm every chosen selector with `htrcli find` (one match, visible, the field you meant)
  before filling. With opaque `name` attributes, target by `label=`/`placeholder=`/role or the
  `label[for]` → id map, never by DOM position or neighbouring structure, and confirm with `find`.

## widgets: react-select

- react-select comboboxes ignore clicks on the tiny input and ignore `htrcli type`. The path that
  works: scroll into view, focus the combobox input (click `.select__control` or eval
  `.focus()`), `htrcli press ArrowDown` to open the menu, press the option text one key at a time
  ("Y", "e", "s"), then `htrcli press Enter`. Verify by reading the `.select__single-value`
  element; the input's own value stays empty.
