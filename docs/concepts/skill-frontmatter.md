---
type: Concept
title: SKILL.md Frontmatter Fields
description: 'The SKILL.md YAML frontmatter keys parsed by internal/skill — name/description/when_to_use aliases plus Kaizen tuned_for/stack — and the advisor opt-out skip_advisor (or advisor: false): truthy/falsy parsing, how it stands down all three checkpoints (the restored ungated "plan" first-write design review and the Jev-gated "done" completion and before-commit moments), current-turn-only scoping, the three detection paths, and why a direct path read is not detected.'
resource: internal/skill/loader.go; internal/agent/advisor_checkpoint.go; internal/agent/agent.go
tags:
  - skills
  - frontmatter
  - skill-loader
  - advisor
  - kaizen
  - agent-loop
timestamp: 2026-11-17T06:36:58Z
---
# SKILL.md Frontmatter Fields

Every `SKILL.md` may begin with a YAML frontmatter block. `internal/skill/loader.go` parses it in `parseSkillMetadata` (`:546`) using `parseFrontmatter` (`:723-748`), which **lowercases every key** (`frontmatter[strings.ToLower(key)] = value`, `:737`) and carries values through `cleanMetadataValue` (`:750`). Unknown keys are ignored; aliases are resolved with `firstNonEmpty`.

## Recognized keys

| Key | Aliases | Type | Effect |
|---|---|---|---|
| `name` | — | string | Skill identifier used by the `skill` / `load_skill` tool and the catalog. Falls back to the first `#` heading in the body (`:582`). |
| `description` | `purpose` | string | Short catalog description (`:553`). |
| `when_to_use` | `when-to-use`, `when` | string | "When to use" guidance in the catalog (`:557`). Also picked up from a body line such as `When to use: …` (`:591`), then clamped to 400 chars (`:603-604`). |
| `tuned_for` | `tuned-for` | model id | Marks the skill as **Kaizen** (per-model tuned). A non-empty value gates it by model and keeps it out of ungated listings and user toggles (`:565`). |
| `stack` | — | string | Kaizen stack gate (`react`, `conduct`, …). Empty (or a universal corpus) is active everywhere; otherwise gated on `stackdetect.Detect(root)` (`:568`). |
| `skip_advisor` | — | boolean | Opts the skill out of all three advisor checkpoints for the turn — **plan** (first-write design review), **done** (completion review) and the **before-commit** gate (below). |
| `advisor` | — | boolean | Inverse shorthand: `advisor: false` is equivalent to `skip_advisor: true` (`:802-812`). |

Body-level markers are not frontmatter: the Kaizen digest is carved from the `<!-- kaizen:digest -->` … `<!-- /kaizen:digest -->` section (`extractDigest`, called at `:574`).

## `skip_advisor` / `advisor: false`

The field is `Skill.SkipAdvisor` (`internal/skill/loader.go:45-49`), populated at `:572` by `parseSkipAdvisor` (`:802-812`):

- `skip_advisor` is truthy only for `true`, `yes`, `1`, `on` (case-insensitive, trimmed). Any other value is false.
- `advisor` inverts the spelling: `false`, `no`, `0`, `off` mean skip. `advisor: true` does **not** opt in to anything — there is no positive form.
- When both keys are present, `skip_advisor` is consulted first and decides.

### Effect: all three advisor checkpoints stand down

`skip_advisor` stands down the restored `"plan"` first-write design review as well as the two `"done"`-gated moments (completion review and before-commit hold). `Step` resolves the flag at two points. Before the round loop it scans a **pre-injection snapshot** of the transcript. `preInjectMessages` is copied before any tail injector runs (`internal/agent/agent.go:1676`), and `ckpt.skillSuppressed = a.turnDisablesAdvisor(ckpt, preInjectMessages)` (`internal/agent/agent.go:1737`) reads it. The injectors append user-role messages (todo re-anchor, notes, dir docs, LSP), and scanning after them would make the injected tail the "current turn" and hide the human's skill prompt. After each response, the response's own tool calls are checked before the batch is counted, before the commit gate and before the plan checkpoint: `ckpt.skillSuppressed = ckpt.skillSuppressed || a.toolCallsDisableAdvisor(ckpt, resp.ToolCalls)` (`internal/agent/agent.go:1954`) — the per-response half still applies to `plan`. A skill the model loads in one round therefore stands the checkpoints down for the writes in the next. The done checkpoint runs only on a response with no tool calls (the `advisorDoneCheckpoint` call at `internal/agent/agent.go:1936`), so for it the flag comes from earlier rounds; the plan checkpoint runs after the write batch is applied (`advisorPlanCheckpoint`, `internal/agent/agent.go:2397`). Once set, the flag is sticky for the rest of the `Step`.

`advisorCheckpointState.skillSuppressed` (`internal/agent/advisor_checkpoint.go:77`) short-circuits all three consumers before any judge or advisor work:

- plan checkpoint — `advisorPlanCheckpoint`, `internal/agent/advisor_checkpoint.go:263`. It stands down on a skill opt-out just as it does on a pending ask; the first write batch still executes, only the design review is skipped.
- done checkpoint — `advisorDoneCheckpoint`, `internal/agent/advisor_checkpoint.go:321-323`.
- before-commit gate — `advisorCommitHold`, `internal/agent/advisor_checkpoint.go:384`. While the flag is set, the gate neither asks the judge nor holds the commit.

`"plan"` and `"done"` are now two independent, valid trigger names: `config.NormalizeAdvisorCheckpoints` (`internal/config/ocodeconfig.go:889`) canonicalizes `"plan"` (case-insensitive, lowercased) and keeps `"done"` instead of collapsing the former into the latter (see [Advisor gate judge](advisor-gate-judge.md)). A session pinning `["plan","done"]` gets both an ungated first-write review and the Jev-gated completion/commit review, and a skill's `skip_advisor` suppresses both.

The advisor **tool** itself is not removed or re-described. The field is the loop's contract — the turn is not advisor-gated; the model-facing "do not call the advisor tool" instruction lives in the skill's own body. `git-commit-push` does both: its frontmatter declares `skip_advisor: true` and its body states "Do NOT call the advisor tool — this skill runs with advisor checkpoints suppressed."

### Scoping: current user turn only

`turnDisablesAdvisor` scans only messages **after the last user-role message** of the pre-injection snapshot (`internal/agent/advisor_checkpoint.go:554-561`). A commit skill used in one turn cannot suppress the checkpoints for later, unrelated turns in the same session. There is no session-wide latch.

### Detection paths

Each candidate name goes through `skillDisablesAdvisor` (`internal/agent/advisor_checkpoint.go:631-649`), which resolves it with `skill.LoadSkillForRoot(name, a.WorkDir())` and memoizes the verdict in `skipAdvisorByName` (`internal/agent/advisor_checkpoint.go:81`). Each name therefore costs one skill-directory scan per `Step`, however often it is mentioned. A name that does not resolve, or a skill without the field, is **not** suppressed (fail-open); the load error is logged via `emitDebug`. Three sources yield the name:

1. **`skill` / `load_skill` tool calls** — `toolCallsDisableAdvisor` (`internal/agent/advisor_checkpoint.go:608-624`) reads the JSON `name` argument of any tool call named `skill` or `load_skill`. It runs from the pre-loop scan (`turnDisablesAdvisor`, `internal/agent/advisor_checkpoint.go:563`) and per response from `Step` (`internal/agent/agent.go:1954`).
2. **Auto-inject markers** — the discovery block beginning `auto-loaded skill "name"` (`autoInjectMarker = "auto-loaded skill"`, `internal/agent/discovery_autoinject.go:71`); the quoted name after the marker is checked in `turnDisablesAdvisor` (`internal/agent/advisor_checkpoint.go:566-579`).
3. **Slash-command prompts** — the `Run the **name** skill.` prompt generated for a skill command (`internal/agent/advisor_checkpoint.go:580-599`).

### Not detected: reading a SKILL.md by path

A skill body read directly (`read skills/foo/SKILL.md`, or any path-based load) carries **no name marker**, so none of the three paths fires and `SkipAdvisor` is not applied. Load the skill through the `skill` / `load_skill` tool, or let discovery auto-inject it, if the opt-out must take effect.

### Sub-agents never checkpoint

`advisorCheckpointEnabled` returns false for any agent with a non-nil `spec` (`internal/agent/advisor_checkpoint.go:165-168`); dispatched sub-agents are excluded because parallel children would contend on the advisor recursion guard. Skill suppression is therefore moot inside a sub-agent turn — it only matters for the top-level agent.

## Example

The user-global `git-commit-push` skill (`~/.claude/skills/git-commit-push/SKILL.md`):

```yaml
---
name: git-commit-push
description: Use when the user wants to commit and push their work — "commit and push", "save my work", "ship it" — including updating existing project changelogs/docs from the diff before committing.
skip_advisor: true
---
```

## Tests

- `internal/skill/loader_test.go:220` (`TestParseSkillMetadataSkipAdvisor`) — `skip_advisor: true` / `false` parsing.
- `internal/agent/advisor_checkpoint_test.go` — `TestAdvisorPlanCheckpoint_SkillSuppressedStandsDown` (line 471): `skillSuppressed` stands down the restored plan checkpoint. `TestAdvisorDoneCheckpoint_SkillSuppressedStandsDown` (line 494): `skillSuppressed` stands down the done checkpoint. `TestAdvisorDoneCheckpoint_SkillLoadedInResponseStandsDown` (line 639): a skill loaded in the same response stands it down.
- `TestTurnDisablesAdvisor_Detection` (line 516) — all three detection paths, the missing-skill fail-open case, and the turn-boundary rule. `TestTurnDisablesAdvisor_PreInjectionSnapshotSeesSkillPrompt` (line 596) covers the pre-injection snapshot. `TestSkillDisablesAdvisor_ResolvesEachNameOncePerStep` (line 663) covers the memo.
- `internal/agent/advisor_gate_typesafe_test.go:350` (`TestAdvisorCommitHold_SkillSuppressedStandsDown`) — pins the before-commit gate's `skillSuppressed` short-circuit in `advisorCommitHold` (`internal/agent/advisor_checkpoint.go:384`): with the judge answering `review_now` at probability 1.0 for a commit batch with session changes, suppression leaves the judge unconsulted (`stub.calls` empty), the advisor unrun, no commit held, and `commitAsked` false; its control runs the identical setup without suppression and must hold the commit, so the assertions cannot pass vacuously. The done-checkpoint tests above cover the flag's other consumer.

## Related

- [Per-Project Skill Enable/Disable](skill-enable-disable.md) — the separate discoverability gate (`skill.DisabledForRoot`); disabling a skill is not the same as advisor suppression.
- [Advisor gate judge](advisor-gate-judge.md) — the judge that decides the done review and the before-commit hold.
- [Advisor Claude Code CLI backend](advisor-claude-code-backend.md) — what the advisor model is and how it is selected.
- [Pending ask suppresses advisor, auto-continue, and goal-resume](pending-ask-suppresses-advisor-auto-continue-goal.md) — the other suppression path for the same checkpoints.
- [Plugin System](../plugins.md) — plugin skills use the same `SKILL.md` frontmatter and are namespaced `<plugin>:<skill>`.