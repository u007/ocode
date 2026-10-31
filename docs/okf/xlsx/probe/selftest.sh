#!/bin/bash
# usage: selftest.sh [task ...]
# Proves the checker is not vacuous: every "good" reference must PASS, every cover-up/naive variant must FAIL.
# Runs in a scratch dir (never inside a repo) and stages each recalculation in ~/xlsx-probe-excel.
# Needs python3 + openpyxl + lxml and Microsoft Excel (macOS or Windows) with access to that folder.
set -u
SP=$(cd "$(dirname "$0")" && pwd)
[ -d "$SP/fixtures" ] || python3 "$SP/make_fixtures.py" > /dev/null
ST=$(mktemp -d -t xlsx-selftest-XXXX)
FIXTURE() { python3 - "$1" <<'EOF'
import sys; sys.path.insert(0, "."); from check import TASKS; print(TASKS[sys.argv[1]])
EOF
}
# task:modes that must FAIL (besides good, which must PASS)
CASES="attendance-status:values attendance-summary:values expense-summary:values expense-pie:badrange budget-bar:badrange sales-pivot:values sales-top3:values messy-clean:lazy,hide audit-variance:values ledger-format:partial orders-lookup:values,today expense-daily:values log-append:naive"
want="$*"; bad=0
cd "$SP"
for case in $CASES; do
  task=${case%%:*}; covers=${case#*:}
  [ -n "$want" ] && ! echo " $want " | grep -q " $task " && continue
  fx=$(FIXTURE "$task")
  for mode in good ${covers//,/ }; do
    d="$ST/$mode-$task"; mkdir -p "$d"; cp "fixtures/$fx" "$d/"
    python3 ref.py "$mode" "$task" "$d" || { echo "REF-ERROR $mode $task"; bad=1; continue; }
    res=$(python3 check.py "$task" "$d"); last=$(echo "$res" | tail -1)
    if [ "$mode" = good ]; then exp=PASS; else exp=FAIL; fi
    if echo "$last" | grep -q " $exp\$"; then echo "ok    $mode $task  $last"; else echo "WRONG $mode $task (want $exp)  $last"; echo "$res" | grep '^FAIL' | head -5; bad=1; fi
  done
done
rm -rf "$ST"
exit $bad
