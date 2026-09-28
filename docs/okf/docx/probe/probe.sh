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
cp "$SP/fixtures/invoice.docx" .
case $TASK in
  replace-image) cp "$SP/fixtures/new_logo.png" . ;;
  insert-image) cp "$SP/fixtures/signature.png" . ;;
esac
git add -A; git -c user.email=p@x -c user.name=p commit -qm fixture

SAVE="Save the result as invoice_out.docx in this directory and leave invoice.docx unchanged."
case $TASK in
  delete-row) P="In invoice.docx, delete the \"Widget C\" row from the invoice table and update the Total to match. Keep the table's alternating row shading. $SAVE" ;;
  edit-cell) P="In invoice.docx, change the Qty of \"Widget B\" from 5 to 12 in the invoice table, and update that row's Amount and the Total to match. Keep the cells' formatting. $SAVE" ;;
  add-row) P="In invoice.docx, add a new row to the invoice table after \"Service G\" and before the Total row: Item \"Widget H\", Qty 2, Unit Price 15.00, Amount 30.00. Update the Total. Keep the table's look (fonts, alignment, alternating row shading). $SAVE" ;;
  add-column) P="In invoice.docx, add a new column named \"SKU\" to the invoice table, directly after the Item column. Values top to bottom: SKU-001, SKU-002, SKU-003, SKU-004, SKU-005, SKU-006, SKU-007. The Total row's label must span every column before Amount. The new header cell must look like the other header cells, and the table must stay within the page margins. $SAVE" ;;
  insert-table) P="In invoice.docx, directly after the \"Notes:\" paragraph, insert a paragraph \"Payment schedule\" followed by a new table with borders. Columns: \"Due date\", \"Amount\". Rows: 2026-10-01 | 279.63 and 2026-11-01 | 279.62. It must fit within the page margins; everything else stays where it is. $SAVE" ;;
  rename-item) P="In invoice.docx, rename the item \"Gadget D\" in the invoice table to \"Quokka Kit\". Keep the cell's formatting and everything else the same. $SAVE" ;;
  replace-image) P="In invoice.docx, replace the logo in the page header with new_logo.png at the same size. The old header logo must be gone; the small logo in the footer must stay as it is. $SAVE" ;;
  insert-image) P="In invoice.docx, insert signature.png directly below the \"Approved by:\" line, 1.5 inches wide keeping its aspect ratio, right-aligned. $SAVE" ;;
  *) echo "unknown task"; exit 2 ;;
esac

start=$(date +%s)
env OPENCODE_API_KEY=$KEY "$BIN" run -yolo -m "$MODEL" -effort med -timeout 900 -p "$P" < /dev/null > "$W.out" 2> "$W.err"
rc=$?
echo "rc=$rc secs=$(( $(date +%s) - start ))" > "$W.meta"
python3 "$SP/check.py" "$TASK" "$W" > "$W.check" 2>&1
echo "$LABEL $MODEL $TASK rc=$rc $(tail -1 "$W.check")"
