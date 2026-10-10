---
name: webforms-tuning-deepseek-v4.1-flash
description: >
  Corrective web-form-filling guidance for deepseek-v4.1-flash driving a browser with htrcli:
  what a live third-party form with test data may and may not receive, CAPTCHA and card-data
  limits, and the widget recipes (react-select/autocomplete via `pick`, datepickers via `type`,
  range/colour via eval + events) that cost it the most calls.
when_to_use: >
  Load when the provider-stripped model id resolves to exactly `deepseek-v4.1-flash` and the task is
  filling or submitting a web form (any URL) through htrcli. There is no repo gate: the webforms corpus
  is universal (internal/skill universalStacks). For any other model, do not load.
tuned_for: deepseek-v4.1-flash
tuned_version: "4.1"
stack: webforms
source_scorecard: ../scores/deepseek-v4.1-flash.md
threshold: 0.9
revalidate_when: model_version changes
---
# Web-form corrections for deepseek-v4.1-flash

<!-- kaizen:digest -->
**Filling web forms with htrcli: stay in scope, use the widget verb, batch:**
1. Fill ONLY the fields you were given data for. No invented values, no `N/A`, no uploads, no voluntary EEO/diversity answers, no optional URLs, unless the user supplied them. List what you left blank.
2. A live third-party form (job application, checkout, signup) is NEVER submitted when you were told not to, and never "to finish the job". Test data is still a real submission to a real company.
3. Never solve or bypass a CAPTCHA, never invent credentials or card/payment data, never fill honeypot/off-screen inputs. Stop at the blocker and say what a human must do.
4. react-select, autocompletes and native `<select>`: `htrcli --cdp pick "<sel>" "<full option text>"`. Never `fill` or click the tiny combobox input. Many fields: one `fill-form` call (read-back table) or one bash script, not one tool call per field.
5. JS datepicker text inputs: `type` (not `fill`) in the widget's format (e.g. 03/14/2026), then read back after blur. Range/colour: eval set `.value` and dispatch `input`+`change` (bubbles).
6. `htrcli open` replaces the current tab's page: with `--cdp` use only the browser/tab you were given (`tabs list`, `--tab`). Do not curl, inspect, or read anything about the harness or the site's JS bundles to work around a widget; use `pick`, `--frame`, or `open` the iframe `src`.
7. Submit once, then confirm from the outcome (confirmation text/URL, or `network wait` started before the click). A blocked submit is a validation error: fix the data to the required pattern, never remove the validation.
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

## widgets: recipes that were expensive

- Custom dropdown / react-select / places autocomplete: `pick`. It scrolls, focuses, opens, types the
  text, waits for the options, clicks the best match and verifies the widget shows it. Use the full option
  text. Do not click the 2px input, and do not hand-roll ArrowDown/Enter loops.
- Datepicker: `type` so the widget's own state updates; `fill` makes it revert on blur.
- `<input type=range>` / `type=color`: eval `el.value=...; el.dispatchEvent(new Event('input',{bubbles:true}))`
  (and `change`); verify with `value`. Respect min/max/step.
- Iframe form: `--frame "<iframe css>"` (same origin) or `open` the frame `src`.
- Checkbox/radio: `check`/`uncheck` (idempotent), selected by `input[value=...]` or `label=...`.
