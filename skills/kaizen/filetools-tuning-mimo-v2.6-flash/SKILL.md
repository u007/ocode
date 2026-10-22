---
name: filetools-tuning-mimo-v2.6-flash
description: File-tool discipline for mimo-v2.6-flash — change, create, restore and delete files only through ocode's file tools (edit, multiedit, multi_file_edit, replace_lines, write, delete, undo_file_change), never through bash (sed -i, git restore/checkout/stash, cp from a /tmp backup, redirects into the repo, rm of helper scripts), including big files, bulk replacements and restores to the last commit.
when_to_use: The active model id, provider-stripped, is exactly `mimo-v2.6-flash` (e.g. `opencode-go/mimo-v2.6-flash` → `mimo-v2.6-flash`) — gate on the model id ONLY, never a stack marker. UNIVERSAL corpus with NO stack detection: applies in EVERY repo whenever this exact model is active. Do not load for any other model.
# --- Kaizen metadata ---
tuned_for: mimo-v2.6-flash
tuned_version: "2.6"
stack: filetools
source_scorecard: ../scores/mimo-v2.6-flash.md
threshold: 0.9
revalidate_when: model_version changes   # STALE on any version bump — re-benchmark
---

# File-tool discipline — mimo-v2.6-flash

<!-- kaizen:digest -->
**Every file change goes through a file tool; bash only reads and runs.** Create with `write`, change with `edit` / `multiedit` / `multi_file_edit` / `replace_lines`, remove with `delete`, revert your own change with `undo_file_change`. Never change a file from bash: no `sed -i`, no `git restore` / `git checkout -- <file>` / `git stash`, no `cp` back from a backup, no `>` / `>>` redirect into the repo, no `rm`, no python or node script that writes. Reading via bash (`git show HEAD:f`, `git diff`, `cat`, `grep`) and running tests is fine.
1. **Bulk replacements:** the same change many times in one file is ONE `edit` with `replace_all: true` (`search` = the exact old text); across files use `multi_file_edit`. Never `sed -i` or `perl -i`, however many occurrences. `edit` takes exactly `path`, `search`, `replace` (the new text) and the boolean `replace_all`; there is no `replace_text`, and `replace` is never `"true"`/`"false"`.
2. **Restoring your own change:** `undo_file_change` with the `tool_call_id` of the edit, or `edit` the lines back. Do not back files up to /tmp first; every file-tool write is already snapshotted. Never `git stash` to compare before/after test runs.
3. **Restoring to the last commit (no undo available):** `git diff -- <file>` to see what changed, then `edit` it back (`replace_all: true` when the same line changed many times), or `write` the `git show HEAD:<file>` content for a small file. Never `git restore` / `git checkout --`, even when the user says "restore to the last commit".
4. **Helper and throwaway files:** create them with `write`, remove them with `delete`, never `rm`. **Verification is read-only too:** never write a scratch copy into the repo to compare against (`git show HEAD:f > .head.json`, `> src/x.js`, even if you delete it right after). Pipe instead: `git show HEAD:f | diff - f`, or `git show HEAD:f | node -e '…read stdin…'`.
5. **Big files:** find the spot with `grep` / `rgrep`, read only that range (`read` with `start_line` / `end_line`), then `edit` with a unique `search` anchor.
<!-- /kaizen:digest -->

## bulk-replace

- The same text replaced many times in one file is a single `edit` call with
  `replace_all: true`. Make `search` exact enough to match only the intended
  occurrences (include indentation or surrounding tokens), then confirm the
  count with a read-only `grep -c`.
- Across several files, use `multi_file_edit` with one `{path, search,
  replace}` entry per file (`replace_all` per entry where needed).
- `sed -i` / `perl -i` are not allowed for this, not even with `LC_ALL=C` or a
  backup suffix.
- `edit` parameters are `path`, `search`, `replace` (the new text) and the
  boolean `replace_all`. There is no `replace_text`; never pass `"true"` or
  `"false"` as `replace`.
- To verify a bulk change against the committed file, compare through a pipe
  (`git show HEAD:<file> | diff - <file>`, or count with `grep -c`). Never
  redirect the committed copy into a scratch file inside the repo.

## restore

- Revert your own edit with `undo_file_change` (pass the `tool_call_id` of the
  edit, write or delete). If it refuses, `edit` the lines back.
- Do not copy a file to `/tmp` before editing it and do not `cp` it back
  afterwards. The snapshot store already keeps every pre-edit state.
- To compare test results before and after a change, run the tests, then
  revert with `undo_file_change` and run them again. Never `git stash` /
  `git stash pop`.
- When the user asks to restore a file to the last commit and there is no undo
  history: read `git diff -- <file>`, then reverse it with `edit` (use
  `replace_all: true` for a line that changed many times) or `write` the
  committed content. `git restore`, `git checkout --` and
  `git checkout HEAD -- <file>` are not allowed, even on request.

## create

- Every new file, including a helper or verification script you only run
  once, is created with `write` and removed with `delete`. `rm -f helper.py`
  is a bash write.
- Append to an existing file with `edit`, anchored on its last lines.

## transform

- For a whole-file transform (re-indent, sort, dedupe), compute the result and
  `write` the file, or `replace_lines` over the affected range.
- To check the result against the committed version, compare read-only
  (`git show HEAD:<file> | diff - <file>`, or `node -e` that reads both). Do
  not redirect the committed copy into a new file inside the repo.

## big-file

- `read` returns at most 250 lines per call. Locate the target with `grep` /
  `rgrep`, `read` that range with `start_line` / `end_line`, and `edit` with a
  `search` string that is unique in the file; use `multiedit` for several
  changes in one file.
