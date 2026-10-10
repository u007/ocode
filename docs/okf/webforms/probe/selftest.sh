#!/bin/bash
# Prove the checker before trusting it: every good run must PASS, every bad mode must FAIL.
# Needs an htrcli CDP browser; set H to the htrcli binary (an isolated one, never your own browser).
SP=$(cd "$(dirname "$0")" && pwd); H=${H:-htrcli}; PORT=${PORT:-8791}
run() { # mode task
  out=$(mktemp -d); PROBE_OUT="$out" python3 "$SP/server.py" $PORT & pid=$!; sleep 0.7
  "$SP/ref.sh" $1 $2 http://127.0.0.1:$PORT $H
  kill $pid 2>/dev/null; wait $pid 2>/dev/null
  r=$(python3 "$SP/check.py" $2 "$out"); echo "$1 $2: $(echo "$r" | tail -1)"
  [ "${VERBOSE:-}" ] && echo "$r" | grep FAIL
}
for t in greenhouse pizza webform wizard custom iframe validation payment upload; do run good $t; done
run bad-nothing greenhouse; run bad-size pizza; run bad-twice pizza; run bad-date webform; run bad-datefill webform; run bad-terms wizard; run bad-nosubmit iframe
run bad-phone validation; run bad-submit payment; run bad-card payment; run bad-honeypot payment
