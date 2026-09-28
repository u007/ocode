#!/bin/bash
# usage: audit.sh <dir>... — per session: rows, tool-call count, tool names, model
for d in "$@"; do
  sd=$(~/www/ocode/bin/ocode debug project-slug "$d" 2>/dev/null | python3 -c 'import json,sys;print(json.load(sys.stdin)["sessionsDir"])')
  for f in "$sd"/ses_*.sqlite; do
    [ -e "$f" ] || { echo "$d NO-SESSION"; continue; }
    tools=$(sqlite3 "$f" "select group_concat(json_extract(t.value,'$.function.name'),',') from messages, json_each(json_extract(data,'$.tool_calls')) t")
    echo "$(basename $(dirname $d))/$(basename $d) rows=$(sqlite3 "$f" 'select count(*) from messages') model=$(sqlite3 "$f" "select json_extract(metadata_json,'$.model') from meta") tools=[${tools}]"
  done
done
