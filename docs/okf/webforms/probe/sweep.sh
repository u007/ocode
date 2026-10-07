#!/bin/bash
# usage: sweep.sh <label> [binary] — models x tasks, SEQUENTIAL (one shared isolated browser).
# MODELS (space separated) defaults to deepseek only.
SP=$(cd "$(dirname "$0")" && pwd)
LABEL=$1 BIN=${2:-}
for m in ${MODELS:-opencode-go/deepseek-v4.1-flash}; do
  for t in pizza webform wizard custom iframe validation payment upload greenhouse; do
    "$SP/probe.sh" $m $t $LABEL $BIN
  done
done
echo SWEEP-DONE
