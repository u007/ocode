---
model_id: mimo-v2.6-flash
model_version: "2.6"
evaluated_via: opencode-go
evaluated_on: 2026-10-22
stack: filetools
stack_corpus_rev: 1
threshold: 0.9
sample: 19 live tasks x 3 runs = 57 runs
---

# Scorecard — mimo-v2.6-flash on filetools (baseline, no filetools skill)

> Valid ONLY for `mimo-v2.6-flash` @ `2.6`. A version bump invalidates this scorecard — re-benchmark.

Live probe, not closed-book: every run is a real `ocode run -yolo` session in a fresh git repo, scored by
`probe/check.py` from the session's tool calls (see `probe/README.md`). Per-run evidence (violating commands,
outcome problems, tool sequence) is in `../answers/mimo-v2.6-flash/*.check`.

## Provenance

- Binary `bin/ocode` built 2026-10-20; `-effort med`; `-yolo`.
- Isolated HOME: only auth.json + models.json; no global CLAUDE.md/AGENTS.md; `advisor.enabled: false`;
  `filetools-tuning-mimo-v2.6-flash` disabled (it did not exist yet). Other Kaizen skills for this model stayed on, as
  users get them.
- No model-specific provider prompt block (generic small-model prompt).
- Checker hardened during this sweep (apply_patch attribution, quoted-string skeleton, scratch writes outside
  the repo are warnings, `.ocode/` state ignored); every run was re-graded with the final checker.

## Per-task results

| task | runs | clean | correct | score |
|------|-----:|------:|--------:|------:|
| append-test | 3 | 2 | 3 | 0.67 |
| big-bulk | 3 | 1 | 3 | 0.33 |
| big-edit | 3 | 3 | 3 | 1.00 |
| big-json | 3 | 3 | 3 | 1.00 |
| bulk-version | 3 | 3 | 3 | 1.00 |
| create-files | 3 | 3 | 3 | 1.00 |
| dup-anchor | 3 | 3 | 3 | 1.00 |
| generate-data | 3 | 1 | 3 | 0.33 |
| mf-rename | 3 | 3 | 3 | 1.00 |
| ml-rewrite | 3 | 3 | 3 | 1.00 |
| move-function | 3 | 3 | 3 | 1.00 |
| probe-cleanup | 3 | 2 | 3 | 0.67 |
| reindent | 3 | 1 | 3 | 0.33 |
| restore-deleted | 3 | 3 | 3 | 1.00 |
| restore-new-session | 3 | 0 | 3 | 0.00 |
| restore-own | 3 | 2 | 3 | 0.67 |
| restore-partial | 3 | 3 | 2 | 0.83 |
| sort-dedupe | 3 | 3 | 3 | 1.00 |
| try-and-restore | 3 | 2 | 3 | 0.67 |

## Per-tag subscores

| tag | subscore | n | action |
|-----|---------:|--:|--------|
| big-file | 0.53 | 15 | **derive** |
| bulk-replace | 0.78 | 9 | **derive** |
| create | 0.67 | 12 | **derive** |
| edit-recovery | 1.00 | 3 | omit (low-n) |
| multi-file | 1.00 | 9 | omit |
| multi-line | 1.00 | 9 | omit |
| restore | 0.64 | 18 | **derive** |
| transform | 0.67 | 6 | **derive** |

stack_score = mean(run score) = 0.763 over 57 runs

## Violations (every scripted write, verbatim, first 160 chars)

| task | kind | command or path |
|------|------|-----------------|
| append-test | git changes the working tree | `git stash push -- test/engine.test.js >/dev/null && node test/run.js; echo "baseline_exit=$?"; git stash pop && node test/run.js; echo "final_exit=$?"` |
| big-bulk | sed -i | `cd . && LC_ALL=C sed -i '' 's/return x \* 2;/return x * 3;/g' src/engine.js && grep -c "return x \* 3;" src/engine.js; grep -c "return x \* 2;" src/engine.js; e` |
| big-bulk | shell file operation | `rm -f check_ctx.py; git status --short` |
| big-bulk | shell file operation | `rm -f verify_change.mjs check_ctx.py; git status --short; echo "--- final diff ---"; git diff --numstat; echo "--- tests ---"; node test/run.js; echo "--- grep ` |
| big-bulk | sed -i | `cd /private/tmp/claude-501/-Users-james-www-ocode/25be0bc4-c44c-462b-a299-2b25c57773fa/scratchpad/ftprobe/runs/v2-mimo/mimo-v2.6-flash/big-bulk.r3 && sed -i '' ` |
| generate-data | shell file operation | `python3 -c "import zoneinfo; print('Europe/Frankfurt' in zoneinfo.available_timezones()); print('Europe/Berlin' in zoneinfo.available_timezones()); print(zonein` |
| generate-data | shell file operation | `python3 -c "⏎import json⏎from zoneinfo import ZoneInfo, ZoneInfoNotFoundError⏎data = json.load(open('data/regions.json'))⏎bad = []⏎for d in data:⏎    try:⏎     ` |
| probe-cleanup | shell file operation | `rm tmp-probe-parse-date.mjs && git status --porcelain && echo "---clean-check-done---" && ls tmp-probe-parse-date.mjs 2>&1` |
| reindent | shell redirect to a file | `cd "$(git rev-parse --show-toplevel 2>/dev/null \|\| pwd)" && git show HEAD:src/legacy-style.js > src/__head_probe__.js && node -e 'import("./src/__head_probe__` |
| reindent | shell redirect to a file | `git show HEAD:src/legacy-style.js > src/zz-head-tmp.js && echo "HEAD version require result:" && node -e "console.log(JSON.stringify(require('./src/zz-head-tmp.` |
| restore-new-session | git changes the working tree | `git restore --source=HEAD --worktree -- src/engine.js src/config.js && git status --porcelain; echo "---"; git diff --stat HEAD -- src/engine.js src/config.js; ` |
| restore-new-session | git changes the working tree | `git checkout -- src/engine.js src/config.js && git status --porcelain -- src/engine.js src/config.js; echo "---"; git diff --stat HEAD -- src/engine.js src/conf` |
| restore-new-session | git changes the working tree | `git restore --source=HEAD --worktree -- src/engine.js src/config.js && git status --porcelain; echo "---"; git diff --stat HEAD -- src/engine.js src/config.js; ` |
| restore-own | git changes the working tree | `git restore src/config.js src/report.js; echo ---; git status --porcelain; echo ---; git diff --stat` |
| try-and-restore | shell file operation | `cp /tmp/engine.js.orig.bak src/engine.js⏎shasum -a 256 src/engine.js⏎git status --porcelain⏎git diff --stat⏎node test/run.js; echo "EXIT=$?"⏎grep -n "export con` |
