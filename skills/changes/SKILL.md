---
version: 1.0.0
name: changes
description: >
  Analyze the current repo change set: pending diffs grouped by purpose, LSP errors in
  the changed files, in-progress spec/design docs and risks. Trigger: /changes,
  "analyze my changes", "what's in the working tree", "what's left to finish".
allowed-tools: bash, lsp_diagnostics, glob, read
---

# Changes

Analyze what is pending in the working tree (staged and unstaged) and report it.
Concise, bullet points, no filler. Read the diffs; do not guess from file names.

## 1. Gather context

Run these as separate plain commands (no pipes, redirects or `;` — they stay
read-only and auto-allowed):

```bash
git status --short
git diff --cached --stat
git diff --stat
```

Then:

- For each changed file in the `--stat` output, run `git diff <path>` (or
  `git diff --cached <path>`) when the stat alone does not tell you what the change
  does. Skip generated files, lockfiles and anything over ~300 changed lines (note
  them by name instead).
- Call `lsp_diagnostics` with `severity: "warning"`. If the tool is unavailable in
  this session, or the project has no language server, it returns nothing: note
  "LSP: not available" under LSP Errors and move on.
- Glob for in-progress spec/design docs at depth ≤ 2 (`**/{SPEC,DESIGN,PLAN,TODO,
  ROADMAP,RFC,PROPOSAL,ADR,WIP,NOTES,ARCHITECTURE,IMPLEMENTATION,ENHANCEMENT,CHANGELOG}*.md`,
  case-insensitive). Read the first 80 lines of at most 5 of them.

Clean tree is a valid case: with no pending changes, say "Clean tree — no staged or
unstaged changes." under Changed Files and still check spec files and diagnostics. Stop
with one line only when the tree is clean and there are no spec files and no
diagnostics.

## 2. Report

Five sections, this order, these exact headings:

### 1. Overview
One or two sentences: what this change set is doing.

### 2. Changed Files
Group by purpose (e.g. "auth module", "UI fixes"). One line per file: path and what
changed. Mark staged files as `(staged)`.

### 3. LSP Errors
Diagnostics found in the changed files (path:line, severity, message). If none, say
"No LSP issues." If LSP was not available, say so.

### 4. Spec / Design Status
For each spec/design markdown file found, say whether it looks complete or carries
TODO/WIP markers. Flag anything that looks unfinished. If none, say "No spec files."

### 5. Risks & Suggestions
Missing error handling, incomplete implementations, broken patterns, anything that
needs follow-up. Cite `file:line` where you can.

Extra words after `/changes` are a focus hint (e.g. `/changes web only`): narrow the
report to that area but keep all five headings.
