# PDF live behavioural probe

A closed-book sheet measures only what a model *states*. This probe has the model actually edit a real PDF via
`ocode run -yolo`, then checks the result on the **text layer**. A white box drawn over old text looks right when
rendered but leaves that text extractable, and the checker catches it.

Copy this folder to a scratch directory, never inside a repo, then run:

```
python3 make_fixtures.py                      # fixtures/: invoice.pdf, invoice_subset.pdf, logo/new_logo/signature.png
for t in delete-row edit-cell add-row add-column insert-table edit-cell-subset replace-image insert-image; do
  mkdir -p st/$t && cp fixtures/invoice*.pdf st/$t/ && python3 ref.py good $t st/$t && python3 check.py $t st/$t | tail -1
done                                          # self-test: every "good" output must PASS; ref.py overlay must FAIL
./sweep.sh <label> [path/to/ocode]            # 4 models x 8 tasks -> runs/<label>/<model>/<task>{,.check,.err,.meta}
python3 sheet.py <label> <task>...            # contact sheet of page 1 for a visual check (shading, header colour)
./audit.sh runs/<label>/*/*/                  # tools used per session, including skill loads
```

- **Tasks** (prompts in `probe.sh`): delete a row, edit a cell, add a row, add a column within the margins,
  insert a ruled table, rename a cell using glyphs missing from the subset font, replace an image, insert an image.
- **Checker** (`check.py`) verifies:
  - the original is byte-identical and page 2 is unchanged;
  - no overlapping or off-page words;
  - the exact rows are present, in order;
  - an even row pitch and a horizontal-rule count of rows+1;
  - a single header style;
  - image content and position;
  - the ink ratio of the new glyphs.
- **Live with-skill runs** need an `ocode` built after 2026-09-28. Older `run` binaries never injected Kaizen
  digests.
