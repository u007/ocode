#!/bin/bash
# usage: sweep.sh <label> [binary]  — all models x tasks, 2 concurrent per model
SP=$(cd "$(dirname "$0")" && pwd)
LABEL=$1 BIN=${2:-}
TASKS="delete-row edit-cell add-row add-column insert-table edit-cell-subset replace-image insert-image"
for m in opencode-go/deepseek-v4.1-flash opencode-go/space-bunny-free ollama-cloud/glm-5.3-flash opencode-go/mimo-v2.6-flash; do
  ( set -- $TASKS
    while [ $# -gt 0 ]; do
      "$SP/probe.sh" $m $1 $LABEL $BIN & [ $# -gt 1 ] && "$SP/probe.sh" $m $2 $LABEL $BIN &
      wait; shift; [ $# -gt 0 ] && shift
    done ) &
done
wait
echo SWEEP-DONE
