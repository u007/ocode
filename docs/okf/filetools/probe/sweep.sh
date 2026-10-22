#!/bin/bash
# usage: sweep.sh <label> <provider/model> [reps] [parallel]
# Runs every task <reps> times (default 2), <parallel> at a time (default 3); one line per run.
# Tally with: python3 summarize.py runs/<label>
set -u
SP=$(cd "$(dirname "$0")" && pwd)
LABEL=$1 MODEL=$2 REPS=${3:-2} PAR=${4:-3}
TASKS="ml-rewrite mf-rename bulk-version big-json create-files generate-data sort-dedupe reindent dup-anchor move-function restore-own restore-partial restore-deleted big-edit big-bulk append-test probe-cleanup try-and-restore restore-new-session"
mkdir -p "$SP/runs"
for t in $TASKS; do for r in $(seq 1 "$REPS"); do echo "$t $r"; done; done |
  PROBE="$SP/probe.sh" M="$MODEL" L="$LABEL" xargs -P "$PAR" -L 1 sh -c '"$PROBE" "$M" "$0" "$L" "$1"' |
  tee "$SP/runs/$LABEL.sweep.log"
