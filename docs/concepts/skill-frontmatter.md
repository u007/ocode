---
type: Concept
title: SKILL.md Frontmatter Fields
description: 'The SKILL.md YAML frontmatter keys parsed by internal/skill — name/description/when_to_use aliases plus Kaizen tuned_for/stack — and the advisor opt-out skip_advisor (or advisor: false): truthy/falsy parsing, plan/done checkpoint suppression, current-turn-only scoping, the three detection paths, and why a direct path read is not detected.'
resource: internal/skill/loader.go; internal/agent/advisor_checkpoint.go; internal/agent/agent.go
tags:
  - skills
  - frontmatter
  - skill-loader
  - advisor
  - kaizen
  - agent-loop
timestamp: 2026-11-08T12:42:45Z
---
# SKILL.md Frontmatter Fields

Every `SKILL.md` may begin with a YAML frontmatter block. `internal/skill/loader.go` parses it in `parseSkillMetadata` (`:546`) using `parseFrontmatter` (`:723-748`), which **lowercases every key** (`frontmatter[strings.ToLower(key)] = value`, `:737`) and carries values through `cleanMetadataValue` (`:750`). Unknown keys are ignored; aliases are resolved with `firstNonEmpty`.

## Recognized keys

| Key | Aliases | Type | Effect |
|---|---|---|---|
| `name` | — | string | Skill identifier used by the `skill` / `load_skill` tool and the catalog. Falls back to the first `#` heading in the body (`:581-583`). |
| `description` | `purpose` | string | Short catalog description (`:551-555`). |
| `when_to_use` | `when-to-use`, `when` | string | "When to use" guidance in the catalog (`:556-560`). Also picked up from a body line such as `When to use: …` (`:591-597`), then clamped to 400 chars (`:604`). |
| `tuned_for` | `tuned-for` | model id | Marks the skill as **Kaizen** (per-model tuned). A non-empty value gates it by model and keeps it out of ungated listings and user toggles (`:564-567`). |
| `stack` | — | string | Kaizen stack gate (`react`, `conduct`, …). Empty (or a universal corpus) is active everywhere; otherwise gated on `stackdetect.Detect(root)` (`:568`). |
| `skip_advisor` | — | boolean | Opts the skill out of the advisor **plan** and **done** checkpoints for the turn (below). |
| `advisor` | — | boolean | Inverse shorthand: `advisor: false` is equivalent to `skip_advisor: true` (`:799-812`). |

Body-level markers are not frontmatter: the Kaizen digest is carved from the `<!-- kaizen:digest -->` … `<!-- /kaizen:digest -->` section (`extractDigest`, called at `:574`).

## `skip_advisor` / `advisor: false`

The field is `Skill.SkipAdvisor` (`internal/skill/loader.go:45-49`), populated at `:569-572` by `parseSkipAdvisor` (`:799-812`):

- `skip_advisor` is truthy only for `true`, `yes`, `1`, `on` (case-insensitive, trimmed). Any other value is false.
- `advisor` inverts the spelling: `false`, `no`, `0`, `off` mean skip. `advisor: true` does **not** opt in to anything — there is no positive form.
- When both keys are present, `skip_advisor` is consulted first and decides.

### Effect: the plan and done checkpoints stand down

`Step` resolves the flag twice. Before the round loop it scans a **pre-injection snapshot** of the transcript: `preInjectMessages` is copied right after `userGoal`, before any tail injector runs (`internal/agent/agent.go:1657-1661`), and `ckpt.skillSuppressed = a.turnDisablesAdvisor(ckpt, preInjectMessages)` (`internal/agent/agent.go:1718`). The injectors append user-role messages (`[ocode:plan]`, notes, dir docs, LSP), and scanning after them would make the injected tail the "current turn" and hide the human's skill prompt. Then, in every round, the response's own tool calls are checked before the plan checkpoint consults the flag: `ckpt.skillSuppressed = ckpt.skillSuppressed || a.toolCallsDisableAdvisor(ckpt, resp.ToolCalls)` (`internal/agent/agent.go:1935`), so a skill the model loads in round 0 stands the checkpoints down for the writes in round 1. Once set the flag is sticky for the rest of the `Step`.

`advisorCheckpointState.skillSuppressed` (`internal/agent/advisor_checkpoint.go:46-48`) short-circuits both checkpoints before any advisor work:

- plan checkpoint — `internal/agent/advisor_checkpoint.go:200-202`
- done checkpoint — `internal/agent/advisor_checkpoint.go:256-258`

The advisor **tool** itself is not removed or re-described. The field is the loop's contract — the turn is not advisor-gated; the model-facing "do not call the advisor tool" instruction lives in the skill's own body. `git-commit-push` does both: its frontmatter declares `skip_advisor: true` and its body states "Do NOT call the advisor tool — this skill runs with advisor checkpoints suppressed."

### Scoping: current user turn only

`turnDisablesAdvisor` scans only messages **after the last user-role message** of the pre-injection snapshot (`internal/agent/advisor_checkpoint.go:320-331`). A commit skill used in one turn cannot suppress the checkpoints for later, unrelated turns in the same session. There is no session-wide latch.

### Detection paths

Each candidate name goes through `skillDisablesAdvisor` (`internal/agent/advisor_checkpoint.go:402-420`), which resolves it with `skill.LoadSkillForRoot(name, a.WorkDir())` and memoizes the verdict in `skipAdvisorByName` (`internal/agent/advisor_checkpoint.go:49-52`), so each name costs one skill-directory scan per `Step` however often it is mentioned. A name that does not resolve, or a skill without the field, is **not** suppressed (fail-open); the load error is logged via `emitDebug`. Three sources yield the name:

1. **`skill` / `load_skill` tool calls** — `toolCallsDisableAdvisor` reads the JSON `name` argument of any tool call named `skill` or `load_skill` (`internal/agent/advisor_checkpoint.go:379-394`). It is called both from the pre-loop scan (`internal/agent/advisor_checkpoint.go:332-335`) and per response from the round loop.
2. **Auto-inject markers** — the discovery block beginning `auto-loaded skill "name"` (`autoInjectMarker = "auto-loaded skill"`, `internal/agent/discovery_autoinject.go:68-71`); the quoted name after the marker is checked (`internal/agent/advisor_checkpoint.go:337-350`).
3. **Slash-command prompts** — the `Run the **name** skill.` prompt generated for a skill command (`internal/agent/advisor_checkpoint.go:351-371`).

### Not detected: reading a SKILL.md by path

A skill body read directly (`read skills/foo/SKILL.md`, or any path-based load) carries **no name marker**, so none of the three paths fires and `SkipAdvisor` is not applied. Load the skill through the `skill` / `load_skill` tool, or let discovery auto-inject it, if the opt-out must take effect.

### Sub-agents never checkpoint

`advisorCheckpointEnabled` returns false for any agent with a non-nil `spec` (`internal/agent/advisor_checkpoint.go:118-121`); dispatched sub-agents are excluded because parallel children would contend on the advisor recursion guard. Skill suppression is therefore moot inside a sub-agent turn — it only matters for the top-level agent.

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
- `internal/agent/advisor_checkpoint_test.go:487-519` — `skillSuppressed` stands down both the plan and done checkpoints.
- `internal/agent/advisor_checkpoint_test.go:535-587` (`TestTurnDisablesAdvisor_Detection`) — all three detection paths, the missing-skill fail-open case, and the turn-boundary rule.

## Related

- [Per-Project Skill Enable/Disable](skill-enable-disable.md) — the separate discoverability gate (`skill.DisabledForRoot`); disabling a skill is not the same as advisor suppression.
- [Advisor Claude Code CLI backend](advisor-claude-code-backend.md) — what the advisor model is and how it is selected.
- [Pending ask suppresses advisor, auto-continue, and goal-resume](pending-ask-suppresses-advisor-auto-continue-goal.md) — the other suppression path for the same checkpoints.
- [Plugin System](../plugins.md) — plugin skills use the same `SKILL.md` frontmatter and are namespaced `<plugin>:<skill>`.
