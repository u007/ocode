#!/bin/bash
# usage: probe.sh <provider/model> <task> <label> [binary]
# Fresh git repo per run; checker + fixtures stay outside it.
set -u
SP=$(cd "$(dirname "$0")" && pwd)
MODEL=$1 TASK=$2 LABEL=$3 BIN=${4:-$HOME/www/ocode/bin/ocode}
KEY=$(zsh -ic 'typeset -f ocode' 2>/dev/null | grep -o 'sk-[A-Za-z0-9]*')
W="$SP/runs/$LABEL/$(basename "$MODEL")/$TASK"
rm -rf "$W"; mkdir -p "$W"; cd "$W"
git init -q
cp "$SP/fixtures/deck.pptx" .
case $TASK in
  replace-image) cp "$SP/fixtures/new_logo.png" . ;;
  insert-image) cp "$SP/fixtures/signature.png" . ;;
esac
git add -A; git -c user.email=p@x -c user.name=p commit -qm fixture

SAVE="Save the result as deck_out.pptx in this directory and leave deck.pptx unchanged."
case $TASK in
  delete-row) P="In deck.pptx, delete the \"Widget C\" row from the table on slide 1 and update the Total row to match. Keep the table's alternating row shading. $SAVE" ;;
  edit-cell) P="In deck.pptx, change the Qty of \"Widget B\" from 5 to 12 in the table on slide 1, and update that row's Amount and the Total row to match. Keep the cells' formatting. $SAVE" ;;
  add-row) P="In deck.pptx, add a new row to the table on slide 1, after \"Service G\" and before the Total row: Item \"Widget H\", Qty 2, Unit Price 15.00, Amount 30.00. Update the Total row. Keep the table's look (fonts, alignment, alternating row shading) and make sure nothing below the table gets overlapped. $SAVE" ;;
  add-column) P="In deck.pptx, add a new column named \"SKU\" to the table on slide 1, directly after the Item column. Values top to bottom: SKU-001, SKU-002, SKU-003, SKU-004, SKU-005, SKU-006, SKU-007. The Total row's label must span every column before Amount. The new header cell must look like the other header cells, and the table must stay within its current left and right edges. $SAVE" ;;
  insert-table) P="In deck.pptx, on slide 1 below the \"Notes:\" text, add a heading \"Payment schedule\" and under it a new table. Columns: \"Due date\", \"Amount\". Rows: 2026-10-01 | 279.63 and 2026-11-01 | 279.62. Nothing may overlap existing content or leave the slide. $SAVE" ;;
  rename-item) P="In deck.pptx, rename the item \"Gadget D\" in the table on slide 1 to \"Quokka Kit\". Keep the cell's formatting and everything else the same. $SAVE" ;;
  replace-image) P="In deck.pptx, replace the logo at the top right of slide 1 with new_logo.png, at the same position and size. The old logo must be gone from slide 1; slide 2 must stay exactly as it is. $SAVE" ;;
  insert-image) P="In deck.pptx, insert signature.png on slide 1 at the bottom right, 2 inches wide keeping its aspect ratio, with its right edge aligned to the table's right edge and above the \"Thank you for your business.\" text. It must not cover anything. $SAVE" ;;
  *) echo "unknown task"; exit 2 ;;
esac

start=$(date +%s)
env OPENCODE_API_KEY=$KEY "$BIN" run -yolo -m "$MODEL" -effort med -timeout 900 -p "$P" < /dev/null > "$W.out" 2> "$W.err"
rc=$?
echo "rc=$rc secs=$(( $(date +%s) - start ))" > "$W.meta"
python3 "$SP/check.py" "$TASK" "$W" > "$W.check" 2>&1
echo "$LABEL $MODEL $TASK rc=$rc $(tail -1 "$W.check")"
