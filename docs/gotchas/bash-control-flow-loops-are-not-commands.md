---
type: Gotcha
title: Bash Control-Flow Loops Are Not Commands
description: Shell control-flow keywords (for/while/case/select) leaking as command prefixes in the auto-permission parser, causing benign loops to be refused by the LLM judge
tags:
  - security
  - permissions
  - auto-permission
  - bash
  - control-flow
  - gotcha
timestamp: 2026-09-17T06:16:39Z
---
# Bash Control-Flow Loops Are Not Commands

**Type:** Gotcha  
**Description:** Shell control-flow constructs (`for`, `while`, `until`, `case`, `select`) are syntax keywords, not executable commands. When `parseShellCommandLine` fails to recognise them, their header words leak into the command prefix, causing the auto-permission LLM judge to see "Execute 'for' (unknown command)" and refuse or fall back to a human ask — even for trivially benign loops. Arithmetic expansion `$((...))` has the same class of bug: tokenisation turns operands into apparent commands.  
**Tags:** security, permissions, auto-permission, bash, control-flow, gotcha  

---

## The Problem

Bash auto-permission classifies commands by splitting on `;`/`&&`/`||`/`|` and examining each fragment's first word. Two root causes allowed control-flow constructs to leak through:

### 1. Parser Leak (`parseShellCommandLine`)

`bodyIntroKeywords` (`do`, `then`, `else`, `elif`, `if`, `while`, `until`, `fi`, `done`, `esac`, `!`) are stripped from the start of a fragment before classification. But `for`, `select`, and `case` were **not** in that list. So:

```
for i in 1 2 3; do echo $i; done
```

produced a fragment with prefix `for` — an unknown command. Similarly:

```
case $x in a) echo a;; esac
```

produced prefix `case`.

`if`/`while` escaped only by luck: dropping the keyword left `[` or `true` as the fragment, which the capability allowlist covers.

`ShellControlKeywords` includes `for`/`case`/`select`, and `AlwaysRuleChoiceAvailable` returns false for them — so the human permission dialog could not persist an always-allow rule either. Every instance asked again forever.

**Bonus leak:** `$((...))` arithmetic expansion was tokenised via `parseParenthesis`, whose depth counting produced a bogus fragment (`i+1`) from `i=$((i+1))`. The arithmetic operand was treated as a standalone command.

### 2. Judge View + Rulebook

`explainBashCommand` only inspects `fields[0]`, so the judge's "Command analysis:" line literally read `Execute 'for' (unknown command)`.

`BundledAutoPermissionPromptBody` (v1.9.3) contained no control-flow rule at all. Its only relevant guidance was "if you cannot establish what the command does … require human approval". A cautious judge therefore refused.

---

## The Fix (v1.9.4)

### Parser — structured control-flow state machine

`parseShellCommandLine` now tracks state (`stNormal`/`stForHeader`/`stCaseHeader`/`stCasePattern`):

- **`for`/`select` header words** (loop variable, `in`, literal list values) are dropped; the arm body is still fully evaluated.
- **`case` subject and pattern labels** are dropped; arm bodies are still fully evaluated.
- **`;;`/`;&`/`;;&`** re-enter pattern state; **`esac`** closes (including the valid `; esac` single-arm form).
- **Command substitutions inside headers** (`for x in $(seq 1 10)`) are still recursed into — they execute.
- **Unterminated constructs fail the parse** so the caller asks rather than auto-allowing a partially-parsed command.

### Arithmetic expansion

New `tokArith` token + `parseArithmetic` handle `$((...))` so arithmetic operands never become a command. Nested `$(...)` inside arithmetic is still recursed via `appendSubstCommands`.

### Judge view

`explainBashCommand` now describes control-flow constructs (new `controlFlowExplanation`) instead of "unknown command".

### Rulebook

`BundledAutoPermissionPromptBody` gained a "Shell control-flow constructs" section and `BundledAutoPermissionPromptVersion` bumped 1.9.3 → 1.9.4. The new section tells the judge that `for`/`while`/`until`/`case`/`select` are syntax, not executables, and that loops with safe bodies should be approved.

---

## Rules

1. **Shell control-flow header words are syntax, not executables.** They must never become command prefixes in the parser's output. Any future additions to `bodyIntroKeywords` or `ShellControlKeywords` must keep the banned-prefix-inside-loop guarantee — verify against `TestPermissions_BashBanMatchesInsideLoopAndConditionalBodies`.

2. **Parser changes that affect fragment boundaries must fail closed.** An unterminated or partially-parsed construct should cause the parse to fail, not produce a best-guess prefix. The caller's ask path is safer than a wrong auto-allow.

3. **The judge's "Command analysis" line and the bundled rulebook both shape verdicts.** A novel construct with no rule gets refused by default. When adding new syntax support, update all three layers: parser, `explainBashCommand`, and the rulebook.

4. **`$((...))` is not a command.** Arithmetic expansion operands must be tokenised separately and never reach the command-classification path.

5. **Keep `parseShellCommandLine` and `ShellControlKeywords` in sync.** The keyword set governs both the parser's state transitions and the human-dialog's always-allow behaviour — a mismatch means the dialog cannot persist rules for constructs the parser handles. Tests in `permission_controlflow_test.go` pin both.

---

## Test Coverage

`internal/agent/permission_controlflow_test.go`:

- Header words never become prefixes (for, case, select, while with various bodies).
- Benign loops (`for i in 1 2 3; do echo $i; done`) auto-allow.
- Destructive bodies still hard-deny (nested subshell, case-arm with `rm`).
- Malformed/unterminated constructs fail safe (parse error, not auto-allow).
- Arithmetic expansion not treated as a command.
- Benign loop needs no judge consultation.

`internal/config/auto_permission_prompt_test.go::TestBundledAutoPermissionPrompt_ControlFlow`:

- Rulebook v1.9.4 contains the control-flow section.
- Version bump detected on sidecar upgrade.

All four parser tests were verified to **fail at pristine HEAD** before the fix.

---

## Related

- [Auto-Permission — Interpreter Scripts in Compound Commands](auto-permission-interpreter-scripts-in-compound-commands.md) — interpreter scripts after `&&`/`||` falling to the generic LLM path (distinct issue, same permission pipeline).
