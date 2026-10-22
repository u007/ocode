---
name: filetools-tuning-deepseek-v4.1-flash
description: File-tool discipline for deepseek-v4.1-flash — change, create, restore and delete files only through ocode's file tools (edit, multiedit, multi_file_edit, replace_lines, write, delete, undo_file_change), never through bash (git restore/checkout/stash, cp from a /tmp backup, cat > / cat >> heredocs, rm, python or node writes), including big files and restores to the last commit.
when_to_use: The active model id, provider-stripped, is exactly `deepseek-v4.1-flash` (e.g. `opencode-go/deepseek-v4.1-flash` → `deepseek-v4.1-flash`) — gate on the model id ONLY, never a stack marker. UNIVERSAL corpus with NO stack detection: applies in EVERY repo whenever this exact model is active. Do not load for any other model.
# --- Kaizen metadata ---
tuned_for: deepseek-v4.1-flash
tuned_version: "4.1-2026-10-21"
stack: filetools
source_scorecard: ../scores/deepseek-v4.1-flash.md
threshold: 0.9
revalidate_when: model_version changes   # STALE on any version bump — re-benchmark
---

# File-tool discipline — deepseek-v4.1-flash

<!-- kaizen:digest -->
**Every file change goes through a file tool; bash only reads and runs.** Create with `write`, change with `edit` / `multiedit` / `multi_file_edit` / `replace_lines`, remove with `delete`, revert your own change with `undo_file_change`. Never change a file from bash: no `git restore` / `git checkout -- <file>` / `git stash`, no `cp` back from a backup, no `cat > f <<EOF` or `cat >> f`, no `rm`, no `sed -i`, no python or node script that writes. This holds for big files, throwaway files and restores too. Reading via bash (`git show HEAD:f`, `git diff`, `cat`, `grep`) and running tests is fine.
1. **Restoring your own change:** `undo_file_change` with the `tool_call_id` of the edit, or `edit` the changed lines back. Do not back files up to /tmp first; every file-tool write is already snapshotted.
2. **Restoring to the last commit (no undo available):** `git diff -- <file>` to see what changed, then `edit` it back hunk by hunk (`replace_all: true` when the same line changed many times), or `write` the `git show HEAD:<file>` content for a small file. Never `git restore` / `git checkout --`, even when the user says "restore to the last commit".
3. **Big files:** find the spot with `grep` / `rgrep`, read only that range (`read` with `start_line` / `end_line`), then `edit` with a unique `search` anchor. Do not fall back to a python `s.replace(...)` because the file is too long to read whole.
4. **Throwaway and new files:** `write` them, run them, then `delete` them. Append to a file with `edit` anchored on its last lines, never `cat >>`.
5. **Moving code between files:** `write` the new file, then `multi_file_edit` (or one `edit` per file) for the removal and the imports. Never do the move in one bash heredoc.
<!-- /kaizen:digest -->

## restore

- Revert your own edit with `undo_file_change` (pass the `tool_call_id` of the
  edit, write or delete you want to reverse). It refuses if the file changed
  outside ocode since, and then `edit` the lines back instead.
- Do not make a `/tmp` backup before an edit "so you can restore it". The
  snapshot store already keeps the pre-edit state of every file-tool write,
  and copying the backup back with `cp` is a bash write.
- To check whether a restore is exact, compare read-only: `git diff -- <file>`
  or `git show HEAD:<file> | diff - <file>`. Never `git stash` to compare
  before/after test runs.
- When the user asks to restore a file to the last commit and there is no undo
  history (a new session, or changes someone else made): read the diff, then
  reverse it with `edit` (use `replace_all: true` for a line that changed many
  times) or `write` the committed content. `git restore`, `git checkout --` and
  `git checkout HEAD -- <file>` are not allowed, even on request.

## big-file

- `read` returns at most 250 lines per call. A long file is not a reason to
  edit it from a script: locate the target with `grep` / `rgrep`, `read` that
  range with `start_line` / `end_line`, and `edit` with a `search` string that
  is unique in the file. For several changes in one file use `multiedit`; for
  a block whose line numbers you just read, `replace_lines` works too.
- For many identical replacements in one file use `edit` with
  `replace_all: true`; across files use `multi_file_edit`.

## create

- Every new file, including a throwaway script you only run once, is created
  with `write`, never `cat > f <<EOF`, `echo > f` or `printf > f`.
- Remove a throwaway file with `delete`, never `rm`.
- Append to an existing file (a new README section, new tests at the end)
  with `edit`, anchored on the last lines before the insertion point. Never
  `cat >> f <<EOF` or `printf ... >> f`.

## multi-file / multi-line

- Moving a function: `write` the new file, `edit` the old file to remove the
  function, and update each importer with `multi_file_edit` (or one `edit`
  per file). Do not do the whole move in one bash command with heredocs.
