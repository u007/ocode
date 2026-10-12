---
version: 1.0.0
name: standup
description: >
  Standup / catch-up summary of recent work: reviews the last 5 commits plus pending
  working-tree changes, LSP diagnostics and in-progress spec files, then reports in
  caveman style with WHAT DONE, sorted TODOs and MISSED STUBS. Trigger: /standup,
  "standup", "catch me up", "what did I do yesterday", "what's pending".
allowed-tools: bash, lsp_diagnostics, glob, read
---

# Standup

Caveman summary of recent commits + pending changes. Short, punchy, drop articles
and filler. One line of context per item, never bare bullets.

## 1. Gather context

Run these as separate plain commands (no pipes, redirects or `;` — they stay
read-only and auto-allowed):

```bash
git log -n 5 --stat --date=short
git status --short
git diff --cached --stat
git diff --stat
```

Then:

- For each changed file in the `--stat` output, run `git diff <path>` (or
  `git diff --cached <path>`) only when the stat alone does not tell you what the
  change does. Skip generated files, lockfiles and anything over ~300 changed lines
  (note them by name instead).
- Call `lsp_diagnostics` with `severity: "warning"`. If the project has no
  language server the tool returns nothing; move on.
- Glob for in-progress spec/design docs at depth ≤ 2 (`**/{SPEC,DESIGN,PLAN,TODO,
  ROADMAP,RFC,PROPOSAL,ADR,WIP,NOTES}*.md`, case-insensitive). Read the first 80
  lines of at most 5 of them.

Clean tree is a valid case: with no pending changes, summarise the commits alone and
say "Clean tree — no staged or unstaged changes." under Pending. Fail only when both
the commit log and the working tree are empty.

## 2. Pick the window

If several commits are dated yesterday (or today), focus on those. Otherwise cover the
last 5.

## 3. Report

Four sections, this order, these exact headings:

### WHAT DONE
What was built/fixed and what was decided. Group by purpose. One line each.

### TODO — EASY FIRST
Low-hanging fruit sorted easiest first: quick wins, small cleanups, trivial follow-ups
the changes imply.

### TODO — HIGH PRIORITY
Bigger or urgent tasks the work points to: incomplete features, risky gaps, anything
blocking shipping. Include any LSP errors here.

### MISSED STUBS
TODO/FIXME/XXX markers, unimplemented paths, `panic("not implemented")`, empty catch
blocks, obvious placeholders introduced in the recent work. Cite `file:line`. If none,
say "None found."

Extra words after `/standup` are a focus hint (e.g. `/standup web only`): narrow the
report to that area but keep all four headings.
