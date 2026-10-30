#!/bin/bash
# usage: sweep.sh <label> <provider/model> [reps] [parallel]
# Runs every task <reps> times (default 2), <parallel> at a time (default 2); one line per run.
# Tally with: python3 summarize.py runs/<label>
set -u
SP=$(cd "$(dirname "$0")" && pwd)
LABEL=$1 MODEL=$2 REPS=${3:-2} PAR=${4:-2}
TASKS="attendance-status attendance-summary expense-summary expense-pie budget-bar sales-pivot sales-top3 messy-clean audit-variance ledger-format orders-lookup expense-daily log-append"
mkdir -p "$SP/runs"
for t in $TASKS; do for r in $(seq 1 "$REPS"); do echo "$t $r"; done; done |
  PROBE="$SP/probe.sh" M="$MODEL" L="$LABEL" xargs -P "$PAR" -L 1 sh -c '"$PROBE" "$M" "$0" "$L" "$1"' |
  tee "$SP/runs/$LABEL.sweep.log"
