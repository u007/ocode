---
name: webforms-tuning-deepseek-v4.1-flash
description: >
  Corrective web-form-filling guidance for deepseek-v4.1-flash driving a browser with htrcli:
  checkbox/radio state checks, programmatic read-back before a single submit, and what a live
  third-party form, a CAPTCHA, an upload or an unspecified browser tab may and may not receive.
when_to_use: >
  Load when the provider-stripped model id resolves to exactly `deepseek-v4.1-flash` and the task is
  filling or submitting a web form (any URL) through htrcli. There is no repo gate: the webforms corpus
  is universal (internal/skill universalStacks). For any other model, do not load.
tuned_for: deepseek-v4.1-flash
tuned_version: "4.1-2026-10-21"
stack: webforms
source_scorecard: ../scores/deepseek-v4.1-flash.md
threshold: 0.9
revalidate_when: model_version changes
---
# Web-form corrections for deepseek-v4.1-flash

<!-- kaizen:digest -->
**Filling web forms with htrcli: you are the agent driving htrcli, not a human in DevTools or the site's developer:**
1. Checkbox/radio: `check`/`uncheck` (never click to toggle). Read the live state before and after with eval `el.checked`, never `attr <sel> checked` (that is the default attribute) or `value` (a checkbox reads "on").
2. Before the one submit, read every field back programmatically (`htrcli value`, or one eval over the form / `new FormData(form)`) and compare with the user's data; check required-empty fields and checkbox/radio state. Never submit to "force" or "probe" validation.
3. `fill` already dispatches input/change; only key events are missing. Switch to `type` for key-driven widgets (masks, datepickers, autocompletes), clear first since `type` appends, and read the value back after blur.
4. Fill ONLY the fields you were given data for, leave the rest blank and list them. Live third-party form with test data: still fill the supplied fields; never submit, upload or answer voluntary EEO questions unless told to.
5. CAPTCHA: never solve or bypass it, and never invent credentials, login or card data. Fill what you can, stop, list the remaining human steps.
6. Upload only the exact file the user named (`htrcli upload <sel> <path>`); never generate or fabricate a document. Confirm the file name shows afterwards.
7. `htrcli open` navigates the current tab: use a dedicated browser/tab, `tabs list` first, then target with `--tab`; never act in a tab the user did not point you at.
<!-- /kaizen:digest -->

## choice-input: verify live checkbox/radio state

- Set state with `htrcli check` / `htrcli uncheck`; click toggles and flips a box already in the
  wanted state.
- Read the starting state first and the result afterwards from the live property:
  `htrcli eval "document.querySelector('<sel>').checked"`. `htrcli attr <sel> checked` returns the
  HTML attribute (the page's default), and a checkbox's `value` is "on" regardless of state.
- Radios: target the one input (`input[value=pro]`, `label=Pro`), `check` it, then read which member of
  the group is `:checked`.

## verify-submit: read back programmatically, submit once

- Before submitting, read every filled field back with `htrcli value <sel>` or one eval over the form
  (e.g. `Object.fromEntries(new FormData(form))`), compare with the user's data, and list required fields
  still empty and checkbox/radio states. Fix mismatches, then submit exactly once.
- Do not submit to surface validation errors, and do not send deliberate invalid or duplicate submissions
  to "test" the form. Find blocking errors with eval (`form.checkValidity()`, `:invalid`,
  `aria-invalid`, error text) instead.
- After the single submit, confirm from the outcome (confirmation text/URL, or the 2xx response via
  `network wait` started before the click); if unclear, inspect, do not resubmit.

## text-input: fill vs type and read-back

- `fill` clears the field, sets the value and dispatches input/change; it lacks only key events. Use
  `type` (trusted keystrokes) for masks, datepickers and autocompletes; `type` appends, so clear first.
- Confirm every value with `htrcli value <sel>` (re-read after blur for widgets), not by eye or screenshot.

## safety: scope, CAPTCHA, uploads and the target tab

- CAPTCHA / "I'm not a robot": do not solve it, and do not make up a login, password or card data for
  fields the user did not supply. Fill what you can, stop, and list the remaining steps for the user.
- A live third-party form with "test" data is still a task to do within limits: fill the supplied fields,
  leave everything else blank, do not submit, upload or answer voluntary EEO/diversity questions unless
  told to, and report which fields were left blank. Do not refuse to fill the supplied fields.
- Upload only a file the user named, by path, with `htrcli upload`. Never construct or generate a
  document to satisfy an upload field; if none was given, leave it empty and say so.
- `htrcli open` replaces the current tab's page. Drive a dedicated browser (`htrcli browser start`) or the
  tab the user pointed at: `htrcli tabs list` first, then pass `--tab`. Avoid tabs with the user's
  logged-in sessions unless asked.
