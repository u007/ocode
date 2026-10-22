#!/bin/bash
# usage: probe.sh <provider/model> <task> <label> [rep]
# One fresh git repo per run (copied from fixtures/project). The model works through
# `ocode run -yolo`; two-turn tasks send turn 2 with `-c` into the same session.
# Env: FILETOOLS_HOME  isolated HOME (auth.json + ocodeconfig.json); required.
#      FILETOOLS_BIN   ocode binary (default ~/www/ocode/bin/ocode).
set -u
SP=$(cd "$(dirname "$0")" && pwd)
MODEL=$1 TASK=$2 LABEL=$3 REP=${4:-1}
: "${FILETOOLS_HOME:?set FILETOOLS_HOME to an isolated HOME}"
BIN=${FILETOOLS_BIN:-$HOME/www/ocode/bin/ocode}
export FILETOOLS_HOME FILETOOLS_BIN=$BIN
W="$SP/runs/$LABEL/$(basename "$MODEL")/$TASK.r$REP"
rm -rf "$W"; mkdir -p "$W"
# A re-run at the same path reuses ocode's project dir; drop the earlier run's sessions.
SD=$(env HOME="$FILETOOLS_HOME" "$BIN" debug project-slug "$W" | python3 -c 'import json,sys;print(json.load(sys.stdin)["sessionsDir"])')
rm -rf "$SD"
cp -R "$SP/fixtures/project/." "$W/"
cd "$W" || exit 1
git init -q && git add -A && git -c user.email=p@x -c user.name=p commit -qm fixture

T2=""
case $TASK in
  ml-rewrite) P='In src/report.js, rewrite formatReport so that: each row line is built with the template literal `${row.name} x${row.qty} @ ${row.price} ${CURRENCY}`; the rows are rendered with rows.map(...).join("\n"); and both separator lines use "-".repeat(PAGE_WIDTH) instead of "=". Also change PAGE_WIDTH to 100, HEADER to "ACME SALES REPORT" and FOOTER to "END OF REPORT". Do not change the helper functions.' ;;
  mf-rename) P='Rename the function fetchUser to loadUser everywhere in this repo: the definition, every import, every call site, and the docs.' ;;
  bulk-version) P='Bump the project version from 1.4.2 to 1.5.0 everywhere it appears as the current version. In CHANGELOG.md, rename the "Unreleased" heading to "1.5.0" and keep the history entries of older versions exactly as they are.' ;;
  big-json) P='In data/settings.json, change every service'"'"'s timeout_ms from 3000 to 5000. Leave retry_timeout_ms and everything else unchanged, including the formatting.' ;;
  create-files) P='Create a new module under src/notify/ with: index.js re-exporting sendEmail and sendSms; email.js with `export async function sendEmail(to, subject, body)` returning { channel: "email", to }; sms.js with `export async function sendSms(to, text)` returning { channel: "sms", to }. Also create test/notify.test.js with one test per function using node:assert. Finally add a "Notifications" section to the end of README.md describing the module in one sentence.' ;;
  generate-data) P='Create data/regions.json: a JSON array of objects with keys code, name, timezone, for these 12 regions in this order:
us-east | US East | America/New_York
us-west | US West | America/Los_Angeles
ca-central | Canada Central | America/Toronto
br-south | Brazil South | America/Sao_Paulo
eu-west | EU West | Europe/Dublin
eu-central | EU Central | Europe/Frankfurt
uk-south | UK South | Europe/London
me-central | Middle East | Asia/Dubai
ap-south | Asia Pacific South | Asia/Kolkata
ap-southeast | Asia Pacific Southeast | Asia/Singapore
ap-northeast | Asia Pacific Northeast | Asia/Tokyo
au-east | Australia East | Australia/Sydney
Also create data/regions.csv with the header line code,name,timezone and the same 12 rows in the same order.' ;;
  sort-dedupe) P='In src/constants.js, sort the FRUITS array alphabetically and remove duplicate entries. Keep the file'"'"'s formatting: one quoted entry per line, 2-space indent, trailing commas.' ;;
  reindent) P='Re-indent src/legacy-style.js from 4-space to 2-space indentation. Change nothing else.' ;;
  dup-anchor) P='In src/handlers.js, change only getInvoices so that a missing user gets status 403 with the body "forbidden". getOrders must keep returning 401 "unauthorized".' ;;
  move-function) P='Move parseDate out of src/utils.js into a new file src/date.js (same code, exported). Update the imports so everything still works: files that use parseDate import it from ./date.js, and keep importing clamp from ./utils.js.' ;;
  restore-own)
    P='In src/config.js change port to 9090 and add `debug: true,` after logLevel. In src/report.js change HEADER to "DRAFT".'
    T2='Revert every change you made in this session: src/config.js and src/report.js must be exactly as they were before you touched them.' ;;
  restore-partial)
    P='Rename the function fetchUser to loadUser everywhere in this repo: the definition, every import, every call site, and the docs.'
    T2='Undo your change to docs/api.md only, so it is exactly as it was before. Keep the rename everywhere else.' ;;
  restore-deleted)
    P='Delete src/legacy.js, and remove its import and its use from src/index.js.'
    T2='I changed my mind. Bring src/legacy.js back exactly as it was, and put src/index.js back exactly as it was before your change.' ;;
  big-edit) P='In src/engine.js, rewrite computeDiscount so the tier is chosen with if / else if: total > 5000 gives 0.2, otherwise total > 1000 gives 0.1, otherwise 0. Loyal customers add 0.05. Cap the result with Math.min(pct, DISCOUNT_CAP) and return round(order.total * pct). Also rename the constant MAX_DISCOUNT to DISCOUNT_CAP everywhere in src/engine.js. Change nothing else.' ;;
  big-bulk) P='In src/engine.js, every rule function that currently ends with `return x * 2;` must return `x * 3` instead. There are many of them. Change nothing else.' ;;
  append-test) P='In test/engine.test.js, add two new tests at the end of the test list (just before the final `export { cases };` line) using the same test(...) helper: one asserting e.computeTax(100) === 6, and one asserting e.computeDiscount({ total: 1500, loyal: false }) === 150. Do not change the existing tests. Then run `node test/run.js` and make sure everything passes.' ;;
  probe-cleanup) P='Does parseDate in src/utils.js accept "2024-02-30" (does it throw, or return a date, and which)? Find out by writing a small throwaway script in this repo and running it with node. Tell me the answer, then delete the throwaway script so the repo is left exactly as it was.' ;;
  try-and-restore) P='In src/engine.js change ROUNDING from 2 to 4, then run `node test/run.js`. If any test fails, put src/engine.js back exactly as it was and tell me which test failed.' ;;
  restore-new-session) P='Someone changed src/engine.js and src/config.js since the last commit, and those changes are wrong. Restore both files to exactly the last committed version.' ;;
  *) echo "unknown task $TASK"; exit 2 ;;
esac

# Setup for tasks that start from uncommitted changes the MODEL did not make. Copies of the set-up files
# go to $W.setup/ (outside the repo) so check.py does not blame the model for them.
if [ "$TASK" = restore-new-session ]; then
  perl -pi -e 's/TAX_RATE = 0\.06/TAX_RATE = 0.08/; s/^    return 0;$/    return -1;/' src/engine.js
  perl -pi -e 's/port: 8080/port: 9090/' src/config.js
  mkdir -p "$W.setup/src" && cp src/engine.js src/config.js "$W.setup/src/"
fi

run() { # $1 = prompt, $2 = extra flags
  env HOME="$FILETOOLS_HOME" "$BIN" run -yolo -m "$MODEL" -effort med -timeout 900 $2 -p "$1" < /dev/null >> "$W.out" 2>> "$W.err"
}
start=$(date +%s)
run "$P" ""; rc=$?
if [ -n "$T2" ] && [ $rc -eq 0 ]; then run "$T2" "-c"; rc=$?; fi
echo "rc=$rc secs=$(( $(date +%s) - start ))" > "$W.meta"
python3 "$SP/check.py" "$TASK" "$W" > "$W.check" 2>&1
echo "$LABEL $MODEL $TASK r$REP rc=$rc $(tail -1 "$W.check")"
