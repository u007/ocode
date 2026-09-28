# docx live behavioural probe

A closed-book sheet measures only what a model *states*. This probe has the model edit a real Word file via
`ocode run -yolo`, then checks the saved XML. Word itself is not available, so `check.py` is structural. It
rejects cover-ups: hidden runs or shapes, white boxes, collapsed rows, off-slide shapes, stale media left in the
zip, and a blob swap that also changes another picture sharing the image part.

Copy this folder to a scratch directory, never inside a repo, then run:

```
python3 make_fixtures.py                      # fixtures/: invoice.docx, logo/new_logo/signature.png
for m in good cover; do for t in delete-row edit-cell add-row add-column insert-table rename-item replace-image insert-image; do
  mkdir -p st/$m-$t && cp fixtures/invoice.docx st/$m-$t/ && python3 ref.py $m $t st/$m-$t && python3 check.py $t st/$m-$t | tail -1
done; done                                    # self-test: every "good" must PASS, every "cover" must FAIL
./sweep.sh <label> [path/to/ocode]            # 4 models x 8 tasks -> runs/<label>/<model>/<task>{,.check,.err,.meta}
../../pdf/probe/audit.sh runs/<label>/*/*/    # tools used per session, including skill loads
qlmanage -t -s 1000 -o . runs/<label>/<model>/<task>/invoice_out.docx   # macOS Quick Look render for an eye check
```

- **Tasks** (prompts in `probe.sh`): delete a row, edit a cell, add a row, add a column, insert a table,
  rename an item whose text is split across two runs, replace the logo, insert an image.
- **Fixture traps** (`make_fixtures.py`):
  - a dark header row with white bold runs;
  - explicit banded shading;
  - a Total label merged over three columns;
  - a logo image part shared with a second location;
  - a second page or slide that must not change.
- **Legacy formats** (`.doc`, `.ppt`) are covered closed-book only. A live probe needs LibreOffice, and
  macOS `textutil` drops tables when it converts.
