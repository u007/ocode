---
model_id: deepseek-v4.1-flash
model_version: "4.1-2026-10-21"
evaluated_via: opencode-go
evaluated_on: 2026-10-22
stack: filetools
stack_corpus_rev: 1
threshold: 0.9
sample: 19 live tasks x 3 runs = 57 runs
---

# Scorecard — deepseek-v4.1-flash on filetools (baseline, no filetools skill)

> Valid ONLY for `deepseek-v4.1-flash` @ `4.1-2026-10-21`. A version bump invalidates this scorecard — re-benchmark.

Live probe, not closed-book: every run is a real `ocode run -yolo` session in a fresh git repo, scored by
`probe/check.py` from the session's tool calls (see `probe/README.md`). Per-run evidence (violating commands,
outcome problems, tool sequence) is in `../answers/deepseek-v4.1-flash/*.check`.

## Provenance

- Binary `bin/ocode` built 2026-10-20; `-effort med`; `-yolo`.
- Isolated HOME: only auth.json + models.json; no global CLAUDE.md/AGENTS.md; `advisor.enabled: false`;
  `filetools-tuning-deepseek-v4.1-flash` disabled (it did not exist yet). Other Kaizen skills for this model stayed on, as
  users get them.
- The DeepSeek provider prompt block (`internal/agent/prompts/deepseek.txt`) was present; it already says never to use `sed` to edit files and to use the edit tool.
- Checker hardened during this sweep (apply_patch attribution, quoted-string skeleton, scratch writes outside
  the repo are warnings, `.ocode/` state ignored); every run was re-graded with the final checker.

## Per-task results

| task | runs | clean | correct | score |
|------|-----:|------:|--------:|------:|
| append-test | 3 | 3 | 3 | 1.00 |
| big-bulk | 3 | 3 | 3 | 1.00 |
| big-edit | 3 | 3 | 3 | 1.00 |
| big-json | 3 | 3 | 3 | 1.00 |
| bulk-version | 3 | 3 | 3 | 1.00 |
| create-files | 3 | 2 | 3 | 0.67 |
| dup-anchor | 3 | 3 | 3 | 1.00 |
| generate-data | 3 | 3 | 3 | 1.00 |
| mf-rename | 3 | 3 | 3 | 1.00 |
| ml-rewrite | 3 | 3 | 3 | 1.00 |
| move-function | 3 | 2 | 3 | 0.67 |
| probe-cleanup | 3 | 1 | 3 | 0.33 |
| reindent | 3 | 3 | 3 | 1.00 |
| restore-deleted | 3 | 3 | 3 | 1.00 |
| restore-new-session | 3 | 0 | 3 | 0.00 |
| restore-own | 3 | 3 | 3 | 1.00 |
| restore-partial | 3 | 3 | 3 | 1.00 |
| sort-dedupe | 3 | 3 | 3 | 1.00 |
| try-and-restore | 3 | 1 | 3 | 0.33 |

## Per-tag subscores

| tag | subscore | n | action |
|-----|---------:|--:|--------|
| big-file | 0.67 | 15 | **derive** |
| bulk-replace | 1.00 | 9 | omit |
| create | 0.75 | 12 | **derive** |
| edit-recovery | 1.00 | 3 | omit (low-n) |
| multi-file | 0.89 | 9 | **derive** |
| multi-line | 0.89 | 9 | **derive** |
| restore | 0.61 | 18 | **derive** |
| transform | 1.00 | 6 | omit |

stack_score = mean(run score) = 0.842 over 57 runs

## Violations (every scripted write, verbatim, first 160 chars)

| task | kind | command or path |
|------|------|-----------------|
| create-files | shell redirect to a file | `cat >> README.md <<'EOF'⏎⏎## Notifications⏎⏎The `src/notify/` module provides `sendEmail` and `sendSms`, each returning a record of the delivery channel and rec` |
| move-function | shell redirect to a file | `cat > src/date.js <<'EOF'⏎export function parseDate(s) {⏎  const [y, m, d] = s.split("-").map(Number);⏎  if (!y \|\| !m \|\| !d) {⏎    throw new Error(`bad date` |
| move-function | changed by something other than a file tool | `src/date.js` |
| move-function | changed by something other than a file tool | `src/events.js` |
| move-function | changed by something other than a file tool | `src/invoices.js` |
| move-function | changed by something other than a file tool | `src/orders.js` |
| move-function | changed by something other than a file tool | `src/utils.js` |
| probe-cleanup | shell redirect to a file | `cd "/private/tmp/claude-501/-Users-james-www-ocode/25be0bc4-c44c-462b-a299-2b25c57773fa/scratchpad/ftprobe/runs/v2-deepseek/deepseek-v4.1-flash/probe-cleanup.r1` |
| probe-cleanup | shell redirect to a file | `cat > .probe_parsedate.mjs <<'EOF'⏎import { parseDate } from "./src/utils.js";⏎⏎for (const s of ["2024-02-30", "2024-02-29", "2024-13-01"]) {⏎  try {⏎    const ` |
| probe-cleanup | shell file operation | `rm .probe_parsedate.mjs && git status --short && echo "clean-check-done" && ls -a` |
| restore-new-session | git changes the working tree | `git restore --source=HEAD --worktree -- src/engine.js src/config.js && echo "---STATUS---" && git status --porcelain && echo "---DIFF (should be empty)---" && g` |
| restore-new-session | git changes the working tree | `git restore --source=HEAD --worktree -- src/engine.js src/config.js && echo RESTORED; git status --porcelain=v1; echo "--- verify ---"; git diff --exit-code -- ` |
| restore-new-session | git changes the working tree | `cd /private/tmp/claude-501/-Users-james-www-ocode/25be0bc4-c44c-462b-a299-2b25c57773fa/scratchpad/ftprobe/runs/v2-deepseek/deepseek-v4.1-flash/restore-new-sessi` |
| try-and-restore | shell file operation | `cp -p /tmp/engine.js.orig.91885 src/engine.js && shasum -a 256 src/engine.js && git status --porcelain src/engine.js && echo "--- re-run after restore ---" && n` |
| try-and-restore | shell file operation | `cp -p /tmp/engine.js.orig-backup src/engine.js && shasum -a 256 src/engine.js && echo "--- expected ---" && echo "1dcd25e0a4ff05dcd22fc5f15e7969b5cf818f300c23c2` |
