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
case $TASK in
  edit-cell-subset) cp "$SP/fixtures/invoice_subset.pdf" invoice.pdf ;;
  *) cp "$SP/fixtures/invoice.pdf" invoice.pdf ;;
esac
case $TASK in
  replace-image) cp "$SP/fixtures/new_logo.png" . ;;
  insert-image) cp "$SP/fixtures/signature.png" . ;;
esac
git add -A; git -c user.email=p@x -c user.name=p commit -qm fixture

SAVE="Save the result as invoice_out.pdf in this directory and leave invoice.pdf unchanged."
case $TASK in
  delete-row) P="In invoice.pdf, delete the \"Widget C\" row from the table on page 1 and update the Total row to match. The rows below it must move up so there is no empty gap in the table. $SAVE" ;;
  edit-cell) P="In invoice.pdf, change the Qty of \"Widget B\" from 5 to 12 in the table on page 1, and update that row's Amount and the Total row to match. $SAVE" ;;
  add-row) P="In invoice.pdf, add a new row to the table on page 1, after \"Service G\" and before the Total row: Item \"Widget H\", Qty 2, Unit Price 15.00, Amount 30.00. Update the Total row to match. Keep the table's look (lines, shading) and make sure nothing below the table gets overlapped. $SAVE" ;;
  add-column) P="In invoice.pdf, add a new column named \"SKU\" to the table on page 1, directly after the Item column. Values top to bottom: SKU-001, SKU-002, SKU-003, SKU-004, SKU-005, SKU-006, SKU-007; leave the SKU cell of the Total row empty. The table must stay within the page's existing left and right margins. $SAVE" ;;
  insert-table) P="In invoice.pdf, insert a new ruled table on page 1 below the \"Notes:\" line, with a heading line \"Payment schedule\" above it. Columns: \"Due date\", \"Amount\". Rows: 2026-10-01 | 279.63 and 2026-11-01 | 279.62. It must not overlap any existing content. $SAVE" ;;
  edit-cell-subset) P="In invoice.pdf, rename the item \"Gadget D\" in the table on page 1 to \"Quokka Kit\". Keep everything else the same. $SAVE" ;;
  replace-image) P="In invoice.pdf, replace the logo image at the top right of page 1 with new_logo.png, at the same position and size. The old logo must be gone. $SAVE" ;;
  insert-image) P="In invoice.pdf, insert signature.png on page 1 at the bottom right, 150pt wide keeping its aspect ratio, right-aligned with the table's right edge and above the \"Thank you for your business.\" footer. It must not cover any text. $SAVE" ;;
  *) echo "unknown task"; exit 2 ;;
esac

start=$(date +%s)
env OPENCODE_API_KEY=$KEY "$BIN" run -yolo -m "$MODEL" -effort med -timeout 900 -p "$P" < /dev/null > "$W.out" 2> "$W.err"
rc=$?
echo "rc=$rc secs=$(( $(date +%s) - start ))" > "$W.meta"
python3 "$SP/check.py" "$TASK" "$W" > "$W.check" 2>&1
echo "$LABEL $MODEL $TASK rc=$rc $(tail -1 "$W.check")"
