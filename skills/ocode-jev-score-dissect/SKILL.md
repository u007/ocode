---
name: ocode-jev-score-dissect
description: Dissect and test the Jev (typesafe/jev-latest) auto-permission score for a bash command to find which segment drags the confidence below the floor, and whether the Ask came from the judge or from a static gate. Use when an auto-permission still asks, defers, or "scores low".
when_to_use: When the user pastes a permission prompt/command and asks why it was asked, rejected, deferred, or scored low; "dissect the jev score"; "why does auto permission not allow this"; "test the judge on this command".
---

# Dissecting a low Jev auto-permission score

An Ask has two independent origins. Find out which one first, then dissect.

## 1. Who raised the Ask? (read the "Permission rule:" line)

| Rule shown | Origin | Fix lives in |
|---|---|---|
| `bash.prefix.sandbox.sensitive` | static sandbox gate (`sensitiveSandboxDecision`, `permissions.go`) — runs BEFORE the judge | gate itself; `relaxed_concerns: ["secrets"]` skips the secret-material rule only |
| `bash.path.out_of_scope` / `OutOfScopePath` | static path scope; also a `verifyAutoGrant` deterministic guard that overrides the judge | `extra_allowed_paths` (global `~/.config/opencode/ocodeconfig.json` or `.ocode/settings.json`) |
| `bash.env.sensitive_path` | env-var path gate | `permissions.go` |
| any rule + log outcome `deferred_below_floor` | judge answered `allow` under the floor (0.80, `autoJudgeMinConfidenceDefault`) | dissect (below) |
| log outcome `refused_deterministic_guard` | judge said allow, `verifyAutoGrant` vetoed | the guard named in the log reason |
| "harmful" / exfiltration | `isHarmfulBashCommandWithVars` | `permission_interpreter.go` |

Log: `~/.local/share/opencode/logs/permission-judge.log` (`[PERMJUDGE]` JSON lines: outcome, choice, confidence, concern). Check it before spending judge calls.

## 2. Dissect the score

`internal/agent/permission_judge_dissect_test.go` (integration tag, live and billed, skipped unless `OCODE_JEV_DISSECT_CMD` is set). It scores the WHOLE line, every top-level segment alone (`only #n`), and the line with each segment removed (`without #n`). Segments split on `&&`, `||`, `;`; pipelines stay whole.

```bash
cd /Users/james/www/ocode/internal/agent
OCODE_AGENT_TEST_HOME=1 \
OCODE_JEV_DISSECT_MODE=sandbox \          # normal|sandbox|yolo|locked (default normal); match the user's mode
OCODE_JEV_DISSECT_DIR=/path/to/project \  # the user's working dir (default: cwd)
OCODE_JEV_DISSECT_RUNS=3 \                # judge calls per variant (default 3; Jev varies ~0.04-0.1 per call)
OCODE_JEV_DISSECT_CMD='<the exact command>' \
go test -tags integration . -run TestPermissionJudgeDissect -count=1 -v 2>&1 | grep -v '^\['
```

`OCODE_AGENT_TEST_HOME=1` loads the real config (API key, `extra_allowed_paths`, `relaxed_concerns`) so the scores match production. Pipe through `sed 's/permission_judge_dissect_test.go:[0-9]*: //'` for a compact table.

Each row prints: `ok|LOW` (lowest run vs. floor), `static=` (what `Decide` said without the judge), `rule=`, `conf=min-max`, `choice`, `concern=` (rubric category Jev cited), `guard_ok=` (`verifyAutoGrant`).

## 3. Reading the table

- **Culprit = a segment whose `only` row is LOW.** Confirm with `without`: removing the culprit lifts the whole-line row.
- **`without #0` going LOW is not a culprit.** Removing the leading `cd` strips the working-directory context; the judge is scoring a different command.
- **WHOLE sits near the floor (within ~0.05) even though it passes** → flaky; a slightly worse call defers to the human. Treat as a failure to fix.
- **`concern=truncated_or_unknown`** → the judge cannot see the full effect (`2>&1 | head`, an unresolved binary such as `./bin/x`, a variable-built command). Fix the visibility, don't lower the floor.
- **`concern=none` but low confidence** → Jev is hesitant about the *shape*, not a rule. Known low shapes: `export PATH="…:$PATH"` (reads as environment tampering, ~0.6–0.75), unknown project binaries.
- **`static=ask` on a row** → the judge may not even be consulted in production for that variant; the Ask is the static gate (table in §1).
- **`guard_ok=false`** → `verifyAutoGrant` overrides any score; the reason is printed.

## 4. Fix options, cheapest first

1. Rewrite the command (drop the `export PATH=`, call the binary by path).
2. A static allow/prefix rule so the judge is never consulted (`permissions.go` rules, `/permissions` prefix allow).
3. Fix the static analyzer if it mis-flags (example: `echo`/`printf` once voided the numeric-port proof and made a loopback `curl` "harmful" — `opaqueVariableWriterForms`).
4. Relax a concern category (`auto.relaxed_concerns`). Never lower `permissions.auto.min_confidence` to paper over one command.

## 5. Gotchas

- The harness mirrors `consultPermissionModel`'s inputs (`buildTypesafePermissionState` + `typesafePermissionQuestions`); it does not replay the TUI's per-session state (earlier transcript, prior grants). A score that is only low in a live session may depend on that.
- Jev is stochastic: compare ranges across ≥3 runs, never single numbers.
- Never commit the user's real command with secrets/paths into a test; the dissect test takes it from env on purpose.
- Related: `ocode-permissions` skill §7 (auto-permission layer), `permission_judge_eval_test.go` (`OCODE_JEV_EVAL=1`, the regression corpus).
