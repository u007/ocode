---
type: Concept
title: Bash Prefix Always-Allow Rules
description: Bash command-word prefixes derived from a permission ask, how they are offered in the web dialog (radio choices) and TUI (numbered list), the synthetic-prefix closure that stops a blanket-bash allow, and where the persistence lives.
resource: internal/agent/bash_prefix_candidates.go
tags:
  - permissions
  - bash
  - auto-permission
  - always-allow
  - prefix
timestamp: 2026-10-21T20:03:00Z
---

# Bash Prefix Always-Allow Rules

When a bash ask asks whether to persist an "always allow this rule", the permission system derives word-prefix candidates from the literal command, narrowest first. The user picks one (or takes the narrowest) and the persisted rule lives in `permissions.bash.prefixes` (keyed by literal word string, no `*` suffix).

## What the dialog offers

- Web (`PermissionDialog`, fed by `handler_permissions_resolve.go`): the confirmed ask renders each candidate as a radio choice shown `<prefix> *`. The `*` means "commands whose first N words are exactly these"; the stored value has no star.
- TUI: the confirmation step lists `[1]..[n]` on the same line; a digit picks and confirms that prefix; `y` confirms the narrowest (the current default); `n`/Back returns to the decision buttons (it does not deny).
- Empty selection (`prefix` field `""` in the POST body) resolves to the narrowest candidate.

## Derivation rules (`BashPrefixCandidates`, `bash_prefix_candidates.go`)

The derivation is literal: it takes the command words as `parseShellCommandLine` sees them, because `matchBashPrefixRuleWords` matches on literal word prefixes.

- Returns `nil` (no safe prefix) for: compound lines (`;` / `&&` / `||`), wrapper invocations (`env VAR=... cmd`, `sudo cmd`, `sh -c "..."`), shell control keywords (`for`, `while`, `case`, `select`), or any head that produces no literal command (`git` without a known subcommand).
- `git` is capped at two words (`git <subcommand>`): a bare `"git"` allow is rejected by `ValidateBashPrefixRule` (would blanket-approve destructive subcommands), and a three-word `git` rule (`git commit -m`) leaks an argument.
- Option tokens (`-...`) truncate the candidate: `ls -la` → `ls`; `rm -rf build` stops before `-rf` (so the reusable prefix is `rm`, not `rm -rf build`).
- A non-head word in a multi-word candidate must be a bare token (`isBareCommandWord`: no flag, path, glob, expansion syntax, or whitespace). The word list is cut at the first non-bare word, and later bare words never re-attach: `go test ./...` → `go test` / `go`; `cat README.md extra` → `cat`; `npm run "my script"` → `npm run` / `npm` (a quoted word with whitespace would split into two words when the rule is matched).
- The cap is three words (`maxBashPrefixCandidateWords`); the list is ordered narrowest → broadest.

Examples: `npm run build` → `["npm run build", "npm run", "npm"]`; `go test ./...` → `["go test", "go"]`; `ls -la` → `["ls"]`.

## The synthetic-prefix closure (`AlwaysRuleChoiceAvailable`)

`AlwaysRuleChoiceAvailable` now returns `false` for every bash ask without a real command prefix. That closes a previous blanket-bash bug: before, asks whose scope was `"tool"` (no literal command head, e.g. a `.sensitive_path`-scoped ask) or synthetic prefixes (`sandbox.sensitive`, `sandbox.exfiltration_risk`, `bash.interpreter.python3`, `permission.escalation`) could fall through to the `permissions.tools.bash = allow` tool-level rule and blanket-approve every future shell command. Out-of-scope path asks (persist the path root) and interpreter script grants (exact file + command + cwd) still offer their own always-allow path.

## Where the pieces live (file names, not line anchors)

- Derivation: `internal/agent/bash_prefix_candidates.go` (`BashPrefixCandidates`, `IsSyntheticBashPrefix`, `validBashPrefixCandidates`, `singleCommandWords`).
- Request field: `PermissionRequest.PrefixCandidates` in `internal/agent/permissions.go`; event serialization in `internal/server/handler_permissions_resolve.go` (`PermissionEvent.PrefixCandidates`, JSON `prefix_candidates`).
- Resolution + persistence: `agent.ResolveBashPrefixChoice` (`internal/agent/permissions.go`) validates the requested prefix against the ask's candidates; `config.SaveSingleBashPrefixRule` writes `permissions.bash.prefixes` (`internal/config/ocodeconfig.go`).
- Always-rule gate: `AlwaysRuleChoiceAvailable` (`internal/agent/permissions.go`); confirmation UI: web `PermissionDialog.tsx`, TUI `model.go`.
- Matching persisted rules: `matchBashPrefixRuleWords` honors broader rules (`permissions.bash.prefixes`) in addition to the exact key lookup, so a broader persisted prefix stops re-asking; `ValidateBashPrefixRule` is unchanged (`"git"` denied; `"rm"` / `"curl"` still persistable because destructive-rm and exfiltration gates run before prefix matching).

## Invariants

- A synthetic prefix (`sandbox.*`, `bash.interpreter.*`, `permission.*`, `""`) can never reach the always-rule branch; `IsSyntheticBashPrefix` is the gate.
- A bare `"git"` allow is rejected by `ValidateBashPrefixRule`; only `git <subcommand>` is a valid persisted prefix.
- The stored value never contains the `*` suffix; the `*` is UI-only.
- A broader persisted prefix (e.g. `"npm"`) stops asking for narrower invocations (`"npm run build"`), because the word-prefix matcher checks broader rules too.
