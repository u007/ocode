# webforms probe — forms, URLs and reuse

Live behavioural probe for **filling web forms with htrcli**. Each task gives a model a prompt, lets it drive a
browser through `htrcli`, and a server-side checker judges the outcome. Model-independent: any model id works.

## Reuse for another model

```
./htr_setup.sh                                   # once: isolated htrcli home + headless Chrome (:9555)
./selftest.sh                                    # optional: 9 good solutions PASS, 10 bad modes FAIL
MODELS="provider/model-a provider/model-b" ./sweep.sh <label> [path/to/ocode]
# or one task: ./probe.sh <provider/model> <task> <label> [binary]
```

Results: `runs/<label>/<model>/<task>{.check,.out,.err,.meta}` (`.check` last line = `SCORE n/m PASS|FAIL`).
The closed-book half is `docs/okf/_prompts/webforms.md` (see `docs/okf/HOW-TO-EVALUATE.md`, STACK=webforms).
Sweeps are sequential (one shared isolated browser); never run two at once. `fixtures/` and `htrhome/` are
generated and gitignored: `python3 fetch_fixtures.py` rebuilds the fixtures.

## Forms and URLs used

| task | form | source URL | kind | checked by |
|------|------|-----------|------|-----------|
| pizza | httpbin pizza order (text, tel, email, radio, checkbox group, time, textarea) | https://httpbin.org/forms/post | real public form, vendored to `pages/pizza.html` (sha256 pinned; action -> `/submit`) | submission record |
| webform | Selenium "Web form" (text, password, textarea, disabled/readonly, select, datalist, file, checkboxes, radios, colour, bootstrap-datepicker, range, hidden) | https://www.selenium.dev/selenium/web/web-form.html | real public form, vendored to `pages/webform.html` (sha256 pinned; action -> `/submit`; `value` added to checkboxes/radios) | submission record |
| wizard | 3-step membership signup with per-step validation | `pages/wizard.html` | authored | submission record |
| custom | `<div>` dropdown, calendar popup, ARIA switch | `pages/custom.html` | authored | submission record |
| iframe | contact form inside a same-origin iframe | `pages/iframe.html` + `pages/iframe_inner.html` | authored | submission record |
| validation | HTML5 `pattern`/`min`/`required` appointment request | `pages/validation.html` | authored | submission record |
| payment | checkout with a honeypot; fill shipping only, do NOT submit | `pages/payment.html` | authored | live field state (beacon), zero submissions |
| upload | multipart expense report with a PDF | `pages/upload.html` + `fixtures/receipt.pdf` | authored | file name + sha256 |
| greenhouse | live job application (react-select comboboxes, intl-tel phone, async location autocomplete, EEO) | https://job-boards.greenhouse.io/stackblitz/jobs/4005254009 | **live third-party**; filled with a dummy `fixtures/resume.pdf` attached, never submitted | live DOM read via CDP |

Local pages are served by `server.py` on a random port (`http://127.0.0.1:<port>/<page>.html`); it records
submissions and a per-page field-state beacon in a directory the model never sees.

External resources the vendored webform page loads at run time: `cdn.jsdelivr.net` (bootstrap 5.1.0),
`code.jquery.com` (jquery 3.6.0), `unpkg.com` (bootstrap-datepicker 1.9.0). The datepicker check needs network.

## Caveats for comparing models

- **Greenhouse is a moving target**: the posting can be closed or edited, or the form changed, at any time. The
  checker reads question ids (`question_4018616009` ...) that change with the posting; re-verify, or drop the
  task, before comparing runs made weeks apart. It is also slow (minutes) and hits a real employer's page, so it
  must never be submitted: no `bad-submit` mode exists for it.
- Pin the **same** `ocode` binary and htrcli version across the models you compare, and record them with the
  label; results drift when either changes.
- `probe.sh` runs the model with `-yolo` (see the SECURITY header in the script). Use a dev machine or a VM.
- `HTRCLI_BIN=/path/to/htrcli` pins the htrcli binary the model gets (default: the one on PATH); use it to compare htrcli builds.
- Credentials: `OPENCODE_API_KEY` (or the provider's own env var) from your environment; `PROBE_PROFILE` (default `plain`) overrides the ocode profile, which otherwise comes from the desktop's window state.
- A single run per task is noisy; repeat tasks that disagree before concluding.
