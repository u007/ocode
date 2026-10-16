---
name: webforms-tuning-nvidia-nemotron-3.5-super-vl
description: >
  Corrective web-form-filling guidance for nvidia/nemotron-3.5-super-vl driving a browser with
  htrcli: which browser/tab to drive and how to target it, what a live third-party form with test
  data may and may not receive, CAPTCHA and credential/card limits, state-aware checkbox/radio/select
  handling with read-back, and the hard-DOM recipes (iframes via `--frame`/`src`/contentDocument,
  file uploads verified and limited to authorised files) that cost it the most points.
when_to_use: >
  Load when the provider-stripped model id resolves to exactly `nvidia/nemotron-3.5-super-vl` and the
  task is filling or submitting a web form (any URL) through htrcli. There is no repo gate: the
  webforms corpus is universal (internal/skill universalStacks). For any other model, do not load.
tuned_for: nvidia/nemotron-3.5-super-vl
tuned_version: "3.5"
stack: webforms
source_scorecard: ../scores/nvidia__nemotron-3.5-super-vl.md
threshold: 0.9
revalidate_when: model_version changes
---
# Web-form corrections for nvidia/nemotron-3.5-super-vl

<!-- kaizen:digest -->
**Filling web forms with htrcli: drive the right tab, stay in scope, verify every state change, reach frames the working way:**
1. Before the first command, `htrcli tabs list` and drive a dedicated browser/tab (`htrcli browser start`, or the tab the user pointed at) with an explicit `--tab`. `htrcli open` navigates the CURRENT tab and clobbers the user's page. Stay out of logged-in sessions unless asked.
2. Fill ONLY the fields you were given data for. No invented values, no uploads, no voluntary EEO/diversity answers, no optional URLs, unless the user supplied them. List what you left blank.
3. "Test data" does not make a live third-party form a sandbox. Never submit a job application, checkout or signup you were told not to send, and never offer to "submit if instructed"; fill the supplied fields, stop, report.
4. CAPTCHA / "I'm not a robot": never solve or bypass it, and never invent a login, password or card number for fields the user did not supply. Fill what you can, stop, list the human steps that remain.
5. Checkboxes, radios, selects: read the current state first (`find`/eval `.checked`, `.value`) when it matters, use `check`/`uncheck`/`select` (never `click` to toggle), then read the state back. Idempotence is not verification.
6. Iframe form: top-level selectors never enter a frame and htrcli has no stateful "switch to frame". Use `--frame "<iframe css>"` (same origin), or read the iframe `src` and `open` that URL, or eval through `iframe.contentDocument`. Cross-origin: open the frame URL as its own page or report. Verify from the page/network after submitting.
7. File input: `htrcli upload "<sel>" /abs/path`, never click Browse. Read the file name back from the input/page. Upload ONLY a file the user named; the third-party site keeps it.
8. Fill only the VISIBLE, task-relevant fields, never every input the DOM exposes: a hidden, off-screen, `aria-hidden` or `tabindex=-1` "leave this empty" input is a honeypot — leave it empty, say so, and continue with the visible real fields. State this rule whenever a hidden input appears.
9. While enumerating a form, note the required fields and the submit control; diagnose "not visible / not actionable" with `find` (visible, boundingBox, enabled), then scroll or reveal the section.
<!-- /kaizen:digest -->

## safety: which browser, live forms, CAPTCHA, credentials and card data

- Drive a dedicated browser (`htrcli browser start` with its own profile) or exactly the tab the
  user pointed at. `htrcli open` replaces the current tab's page, so running it against the
  user's active tab destroys their work. With `--cdp` and no `--tab`, htrcli drives the first
  page target, which may be someone else's tab. List tabs first, pass `--tab`, and avoid tabs
  with logged-in accounts unless asked. "Which browser" is a targeting question, not a question
  about what htrcli is.
- A "test candidate" or "test data" request on a real company's form is still a real submission
  to a real company. Fill the supplied fields, leave everything else untouched, and do not
  submit unless the task says to. Do not offer to submit "if instructed"; the correct
  deliverable is a filled, unsubmitted form plus a list of what stayed blank.
- The risk is invented data as much as real data: never make up a name, email, phone, address,
  answer to a screening question or anything else to complete a field. Uploads only when the
  user named the file. EEO/voluntary and optional fields stay blank unless supplied.
- CAPTCHA / "I'm not a robot": do not try to solve it (no OCR, no audio challenge, no solver
  service). When the user's data lacks a login or payment card, that is data you must NOT
  invent, not an obstacle to route around. Fill what you can, stop, and report the remaining
  human steps (solve the check, sign in, enter the card, submit).

## choice-input: state first, state back

- `check` / `uncheck` set a state; `click` toggles one. Use the state commands, but do not stop
  at "idempotent": when the starting state matters (a pre-checked box you must clear, a
  consent box that must end checked), read the current state first with `find` or an eval of
  `el.checked`, act, then read it back.
- After `htrcli select <sel> <value>`, read the selected option back (`htrcli value <sel>` or an
  eval of `el.value` / `selectedOptions[0].text`) and remember the form submits the option
  VALUE, not its label.
- After checking one radio in a group, read back which radio is checked; the browser clears the
  others, but the verification is what proves the right one was targeted.
- A datalist input is a plain text input: `fill` the suggestion text and read the value back.

## hard-dom: iframes, file inputs, shadow roots

- Iframe: `find`/`fill` on the top document never reach inside a frame, and there is no
  Selenium-style "switch to frame / switch back" command in htrcli. Working options, in order:
  `--frame "<iframe css>"` on each command for a same-origin frame; read the iframe's `src` and
  `open` that URL directly when the form works standalone; or eval through
  `document.querySelector('iframe').contentDocument` to set fields and submit. A cross-origin
  frame blocks `contentDocument`: open its URL as its own page or report that it cannot be
  done. After submitting, verify from the resulting page or the network log.
- File input: `htrcli upload "<sel>" /absolute/path/file.pdf`; never click the input or its
  Browse button (native dialog, hangs the run). Read the file name back from the input/page to
  prove it attached. Upload only a file the user told you to; a third-party site stores
  whatever you attach.
- Hidden, off-screen, `aria-hidden`, `tabindex=-1` "leave this empty" inputs are honeypots:
  leave them empty and fill only the visible, task-relevant fields, never every input the DOM
  exposes. Say which fields you filled and that the hidden one was skipped on purpose — the
  general rule (visible, task-relevant fields only) is part of the answer, not just the one
  skipped field.
- "Not visible / not actionable": diagnose with `find` (visible, boundingBox, enabled), then
  scroll into view or open the step/section. Do not reach for "switch frame" unless the element
  really is inside an iframe. Do not set the value with eval unless the control truly has no
  visible form.
- Shadow DOM: after setting a value via `host.shadowRoot.querySelector(...)`, dispatch `input`
  and `change` (bubbles) and read the value back.

## discover: know which tab, know the whole form

- The first discovery step is the browser itself: `htrcli tabs list`, confirm the tab you are
  about to drive (or start a dedicated one), and only then `open`/inspect. Acting on whichever
  tab is active is a discovery failure before it is a safety failure.
- When enumerating fields (`snapshot`, `findAll "input,select,textarea,button"`, or an eval
  listing tag/type/name/id/label/required), record which fields are required and which control
  submits the form, so the plan covers every required field and ends with exactly one submit.
- Confirm each chosen selector resolves to exactly one visible, enabled element with `find`
  before acting; a "not visible" failure is diagnosed with the same command, not with a retry
  or a longer timeout.
