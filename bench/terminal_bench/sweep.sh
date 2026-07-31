#!/bin/bash
# Run the frozen Terminal-Bench subset against ocode.
# Usage: bench/terminal_bench/sweep.sh <label> [extra tb args...]
set -euo pipefail

if [ $# -lt 1 ]; then
  echo "usage: $0 <label> [extra tb args...]" >&2
  exit 1
fi

LABEL="$1"
shift

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$REPO_ROOT"

SUBSET="bench/terminal_bench/subset.txt"
if [ ! -s "$SUBSET" ]; then
  echo "error: $SUBSET is empty; freeze the task subset first" >&2
  exit 1
fi

TASK_ARGS=()
while read -r task; do
  [ -z "$task" ] && continue
  case "$task" in \#*) continue ;; esac
  TASK_ARGS+=(-t "$task")
done < "$SUBSET"

# Record exactly what produced these numbers. A floating harness or binary
# makes two runs incomparable.
echo "label:      $LABEL"
echo "tb:         0.2.18"
echo "dataset:    terminal-bench-core==0.1.1"
echo "ocode:      $(./ocode version 2>&1 | head -1)"
echo "commit:     $(git rev-parse --short HEAD)"

uvx --from terminal-bench==0.2.18 tb run \
  --agent-import-path bench.terminal_bench.ocode_agent:OcodeAgent \
  --model opencode-go/deepseek-v4-flash \
  --dataset terminal-bench-core==0.1.1 \
  --n-attempts 3 --n-concurrent 4 \
  --global-agent-timeout-sec 900 \
  --output-path "bench/terminal_bench/runs/$LABEL" \
  "${TASK_ARGS[@]}" \
  "$@"
