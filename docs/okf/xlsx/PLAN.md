# Excel (xlsx) model assessment — run plan

Goal: find out whether a small model can do an office/admin clerk's spreadsheet work, where it is weak,
and (if it is weak) what a model-specific correction skill must say. Nothing here has been run against a
real model yet. The scoring tooling was verified with reference solutions only (`probe/selftest.sh`).

Everything referenced lives in `docs/okf/xlsx/`: `questions.yaml` (answer key, never shown to the model),
`../_prompts/xlsx.md` (answer-free sheet), `probe/` (13 live tasks), `meta.yaml` (tags, threshold 0.9).

## 0. Decide before starting

| decision | recommendation |
|---|---|
| Model(s) | The exact provider-stripped id, e.g. `mimo-v2.6-flash`, not a family. Run the host you normally use; note it as `evaluated_via`. |
| Baseline model | Run one stronger model through the live probe first (same sweep). If it fails a task, suspect the task or checker, not the small model. |
| Repetitions | 2 per task minimum, 3 if the first two disagree. A small model's single attempt is noisy. |
| Pass bar | `meta.yaml` threshold **0.9** per tag (same as pdf/docx/pptx). |

## 1. Preflight (about 15 minutes, no model needed)

1. Machine that runs `ocode` has `python3`, `openpyxl`, `lxml` and LibreOffice (`soffice` on PATH).
   `pip install openpyxl lxml`. The model's own shell needs `python3` and `openpyxl` too (or permission to
   `pip install`): if it cannot import openpyxl, every task fails for an environmental reason. Check with
   `python3 -c "import openpyxl"` in the same environment `ocode run` uses.
2. `cd docs/okf/xlsx/probe && ./selftest.sh` must end with 13 `good` = PASS and 15 covers = FAIL
   (about 2 minutes). Any `WRONG` line means the checker is broken on this machine; stop and fix first.
3. **Confirm the `ocode run` flags.** `probe.sh` calls `ocode run -yolo -m <model> -effort med -timeout 900 -p <prompt>`,
   copied from the docx probe and not re-checked against the current binary. Run `ocode run --help`; if a flag
   differs, edit the one `"$BIN" run ...` line in `probe.sh`.
4. Credentials: the script reads nothing from your shell profile. Export whatever `ocode run` needs
   (API key env var) before the sweep. Set `OCODE_BIN=/path/to/ocode` if it is not on PATH.
5. Smoke test one cheap task: `./probe.sh <provider/model> ledger-format smoke 1`, then read
   `runs/smoke/<model>/ledger-format.1.check`. Confirms the whole path (git init, prompt, output name, checker).
6. Copy `probe/` to a scratch directory for the sweep so `runs/` does not sit in the repo
   (it is gitignored either way).

## 2. Phase A — closed-book (what the model *says*)  ~30 min

Rule 0 of `HOW-TO-EVALUATE.md` applies: the answerer must never see `questions.yaml`, `questions.md`,
`scores/`, `derived/` or `meta.yaml`.

1. Give the model **only** `docs/okf/_prompts/xlsx.md` plus the answerer instruction in `HOW-TO-EVALUATE.md`
   ("answer from your own knowledge only … return a YAML list of {id, answer}"). No repo access, no web.
2. Save the reply to `docs/okf/xlsx/answers/<model-id-flattened>.md` (flatten `/` to `__`).
3. A **different** agent/session grades it against `questions.yaml` using the grader prompt in
   `HOW-TO-EVALUATE.md` (STACK = `xlsx`) and writes `docs/okf/xlsx/scores/<model-id-flattened>.md`.
4. Read the per-tag table. Expect `formulas`, `preserve` and `xlsx-model` to be the discriminating tags.

## 3. Phase B — live probe (what the model *does*)  ~1–3 h

```
export <credentials for ocode>
cd <scratch>/probe
python3 make_fixtures.py
OCODE_BIN=/path/to/ocode ./sweep.sh run1 <provider/model> 2 2     # label, model, reps, parallel
python3 summarize.py runs/run1
```
Budget: 13 tasks × 2 reps = 26 runs, each capped at 900 s by `-timeout`; typical runs are a few minutes,
so with 2 in parallel plan for 1–3 hours. Lower the parallelism if the provider rate-limits.

### The 13 test cases

Full prompt text is in `probe/probe.sh`; the traps and checks are tabulated in `probe/README.md`. Order below
is easy to hard by my estimate, and is the order to inspect failures in.

| # | task | clerk scenario | tag(s) | expected to separate models? |
|---|---|---|---|---|
| 1 | `ledger-format` | make the expense ledger presentable and printable, values untouched | formatting | low: pure openpyxl formatting |
| 2 | `attendance-summary` | department headcount / present / late / absent / % | formulas, summary | low–medium |
| 3 | `expense-summary` | spend vs budget by category with status and total | formulas, lookup, summary | medium |
| 4 | `sales-top3` | top 3 reps with live totals | formulas, summary | medium |
| 5 | `expense-pie` | pie chart of spend share (Total-row trap) | charts | medium |
| 6 | `budget-bar` | clustered column Spent vs Budget, titles, axes, legend | charts | medium–high (axes) |
| 7 | `attendance-status` | Name / Hours / Status per employee (blank punch trap) | formulas, lookup, dates-times | high |
| 8 | `orders-lookup` | customer, due date, overdue, status; unknown customer | formulas, lookup, dates-times | high |
| 9 | `audit-variance` | stock-count variance, flag, summary, conditional format | formulas, formatting | high |
| 10 | `messy-clean` | normalise a messy export, dedupe, new sheet | cleanup, dates-times | high |
| 11 | `sales-pivot` | Region × Month pivot, live | summary, formulas | high (no real pivot API) |
| 12 | `expense-daily` | date-driven daily report | formulas, summary, dates-times | high (`_xlfn.`, liveness) |
| 13 | `log-append` | add a row to an existing log without breaking it | preserve | highest |

Each task passes only if every check passes; the fraction of checks passed is kept as partial credit.

## 4. Reading the results

Per run: open `<task>.<rep>.check` (first FAIL line is usually the cause), then `.out`/`.err`, then the
model's `*_out.xlsx`. Classify each failing run before drawing conclusions:

| class | meaning | action |
|---|---|---|
| model | wrong formula, pasted values, wrong chart range, lost chart … | counts against the model |
| environment | no openpyxl, no write permission, timeout, rate limit, `.err` shows a crash | fix and re-run; do not count |
| checker | a correct output rejected (compare with `ref.py good`) | fix `check.py`, re-run `selftest.sh`, re-score the stored outputs |
| prompt | ambiguous request produced a reasonable but different result | tighten the prompt in `probe.sh`, note the change, re-run all models |

Per-tag subscore = mean fraction over that tag's runs (`summarize.py`). A tag with fewer than 3 runs is
low-n.

### Verdict guide (suggested, adjust after seeing a stronger model's numbers)

| outcome | reading |
|---|---|
| tag ≥ 0.90 and ≥ 80% of that tag's runs fully pass | capable for that kind of work |
| 0.70–0.90 | usable with supervision; derive a correction skill for this tag |
| < 0.70, or a task that never passes | not capable yet for that kind of work; do not hand it that work unattended |
| closed-book high but live low | knows the facts, does not apply them: the skill should be short directive rules, not explanation |
| closed-book low but live high | knowledge gap that tools or retries cover; lower priority |

Safety-relevant failures weigh more than the score: **overwrote or damaged the original**, **silently
replaced formulas with numbers**, **reported success on a file with error cells**, **hid rows instead of
deleting**. Note any of these explicitly in the write-up even if the task score looks acceptable.

## 5. Phase C — derive and validate a correction skill (only for tags < 0.90)

1. Write `docs/okf/xlsx/derived/xlsx.<model-id-flattened>.SKILL.md` covering **only** below-threshold tags
   (rules and format in `HOW-TO-EVALUATE.md`; no eval bookkeeping inside the file; a compact
   `<!-- kaizen:digest -->` block). Because `xlsx` is catalog-only, the digest is advertised, not injected, so
   the skill description must make the model load it for any spreadsheet request.
2. Sync it into the embedded skills with `docs/okf/_tools/sync-derived-skills.py` as the other stacks do.
3. Re-run the same sweep with the skill enabled (label `run1-skill`), same reps, same fixtures. Compare
   per-task and per-tag against `run1`. Expect to keep the skill only if the weak tags reach 0.90 without
   lowering a strong tag.
4. Record both sweeps in `docs/okf/xlsx/scores/<model>.md` and `<model>.with-skill.md`.

## 6. Results record (copy per model)

```
model:            <provider-stripped id>      served via: <host>      date: <YYYY-MM-DD>
ocode version:    <ocode --version>           LibreOffice: <soffice --version>   openpyxl: <ver>
closed-book:      stack score <..>   tags: <tag=score ...>
live sweep:       label <run1>  reps <2>   runs fully passed <n/26>   mean fraction <..>
                  tags: formulas <..> lookup <..> dates-times <..> charts <..> summary <..>
                        cleanup <..> formatting <..> preserve <..>
failure classes:  model <n>  environment <n>  checker <n>  prompt <n>
safety findings:  <none | list>
verdict per tag:  <capable | supervise | not capable>
next step:        <derive skill for tags … | none>
```

## 7. Known limits to remember when interpreting numbers

- No real Excel: values come from LibreOffice 24.2, charts are judged from XML, not rendered. Render two or
  three outputs to PDF (`soffice --headless --convert-to pdf`) and look at them.
- `XLOOKUP` and dynamic arrays are forbidden in the prompts (LibreOffice 24.2 cannot calculate them).
- Fixtures are small; this measures correctness, not large-file performance.
- Legacy `.xls` and `.xlsm` are covered by closed-book questions only.
- Run the stronger baseline model before concluding anything about the small one.

## 8. Open items I could not settle without a model

1. The exact `ocode run` flags (step 1.3).
2. Whether the small model has `openpyxl` available in its shell by default (step 1.1): decides whether
   "no library" is a finding or an environment fix.
3. Whether 900 s per task is enough for this model; raise `-timeout` in `probe.sh` if runs end on the cap.
4. Prompt wording: if several models fail the same task the same way, suspect the prompt first.

## 9. Related: Excel add-in proposal

A side-pane add-in that chats with `ocode serve`, keeps Excel sessions with history/restore/compare, and gives the
agent live-workbook tools is proposed in `docs/plans/2026-10-30-excel-addin-sidepane.md`. Its acceptance suite is
the 13 tasks above, and it enables the useful comparison "same small model, tools vs openpyxl" (section 10 there).
