# 02 — External analysis: what the "recursive harness" work actually does

Read 2026-11-05 from the primary sources (README, arXiv HTML, repo trees). Numbers are the papers' own claims.

## Harness² — google-research/harness2 (arXiv 2610.14224)

**Claim.** Improve the harness (everything around frozen weights) on open-world tasks that have no
ground-truth verifier, and carry what was learned to future tasks.

**Eight editable components**, each a file mounted in the runtime (OpenCode by default, Codex optional):

| # | component | file | activation |
|---|---|---|---|
| C1 | system prompt | `systemprompt.md` | every call |
| C2 | guardrails | `guardrails.md` | every call |
| C3 | memory | `LongTermMEMORY.md` | every call |
| C4 | tool guidance | `tool_descriptions/tool_guidance.md` | every call |
| C5 | subagents | `sub_agents/<name>/AGENT.md` | task tool |
| C6 | skills | `skills/<name>/SKILL.md` + scripts | on demand |
| C7 | scripts | `scripts/<name>.py` | via bash |
| C8 | plugin | `plugin/<name>.ts` | middleware in the tool loop; "enforces a behavior rather than instructs it" |

**Task-level loop: Propose → Probe → Compose.**
- Run the current harness once (reference run). Editor sees task, harness, reference trace, history and the
  library, and writes **two** candidates: an *invariant* edit (planning, tool use, context management,
  delegation) and a *domain* edit (knowledge, procedures, constraints of this domain). Every edit must cite a
  moment in the trajectory. Preferred form in order: skill, script, subagent, plugin, always-on prose last.
  `KEEP_BASE` is a legal answer.
- Probe: run each candidate on the **same task**, matched model and environment. Contrast trajectories and
  deliverables against the reference. Contrasts "inform composition rather than verify correctness".
- Compose: merge supported edits, adopt one candidate, or keep the base. Every decision cites a concrete
  observation. For `k>1` a selector ranks **anonymised** deliverables (delivery, requested shape, fidelity to
  source, consistency; length and polish earn nothing). Rubrics are hidden from editor and selector.
- Cost: `3k` rollouts per round (`k` reference + `2k` candidates) plus editor/composer sessions. `k=5` is 15
  rollouts of the task.

**Harnessing-level loop.** After the deliverable, an *experience reflector* reviews the whole record and
extracts lessons that execution contrasts support. The library entry = the full adopted harness tree +
`EXPERIENCE.md` (situation, lessons, evidence). Entries are reached through an index and inspected on demand;
components are **never inherited automatically**, they must pass through Propose again.

**Results and failure modes.** +4.5 to +11.8 points on three professional benchmarks for GLM-5 and Gemini 3.5
Flash. Ablations: removing the library costs ~2-3 points; removing scoping costs the most. **Direct refinement
without the reference/probe/library machinery degrades** (Code 73.4 → 63.2). Two prior methods (ReasoningBank,
Harness Scaling) fall *below* the base harness in many pairs. Probe evidence is noisy; more recursion
sometimes regresses. Limitations section: cost grows with budget; without supervision correctness and transfer
are not guaranteed.

## Recuris — Recursive Experiential–Working Memory Evolution (arXiv 2608.24876)

- Recursion confined to a **memory-control layer**; model, meta-agent, gate and harness stay fixed.
- Four evolving components: Experiential Memory (skills in agent-skill format), a Working-Memory spec (goals
  with status pending/done/blocked plus evidence), an invocation policy (which skill fires at which event:
  *call-time*, before a drafted state-changing tool call executes, or *boundary*, at turn start), and checkers
  (does an observation support a state change).
- Loop: structured trace → meta-agent **localises** the failure to one component → one scoped patch per
  implicated component → **gate**: admitted only if it fixes the failing task without regressing a held-out dev
  set that contains anchor tasks the current memory already solves.
- Findings that matter for design: a structured trace localises the responsible component 64.8 % of the time
  vs 13 % from outcome alone and 37 % from the raw transcript. **Working memory alone gives most of the gain
  (+23.9); experiential memory alone is not significant (+2.0)**; dumping the whole skill library into context
  scores below WM alone and costs more. Memory grows redundant (17 near-duplicate skills, no deprecations).
  Gate noise is real (small dev sets rejected patches that would have helped). Test-time adaptation gains were
  mostly the retry budget.
- The meta-agent is Claude Code; an independent re-implementation on DeepSeek Harness reproduced the gain.

## RHI — Recursive Harness Self-Improvement (arXiv 2607.15524)

Treats the harness as a prompt-level specification of the agent loop and refines it iteratively using
**pairwise feedback over the harness's own revision history**. A few iterations let a low-reasoning-effort agent
exceed the max-effort setting at up to 60 % lower inference cost; gains attributed to better task-specific
context management and inter-agent information flow, not longer reasoning. 30 synthetic ML-research tasks.
Not a DeepSeek paper.

## DeepSeek Harness — deepseek-ai/deepseek-harness (MIT, developer preview, Aug 2026)

The repo contains **no runtime learning loop**. What it does have, and what it teaches:

| package | what it is | lesson for ocode |
|---|---|---|
| `feedback/` (`command-feedback`, `message-feedback`) | `/feedback` session remark + per-message rating/category/note, stored as **log-only session events that never enter model history or telemetry** | capture the human signal, keep it out of the prompt: a signal about the output, never input to it |
| `context/` (`agent-instructions`, `session-reference`, `time-context`…) | instruction files and referenced other sessions enter history as **user-role messages** so they persist, replay and compact like content | same cache rule ocode already follows for volatile tails |
| `skill/` | filesystem skill discovery with a sorted durable session catalog; nothing authors skills | — |
| `goal/`, `plan/`, `todo/` | durable objective per session; plan mode; todo | parity with ocode's `/goal`, plan fragment, todo |
| `experimental/auto-review` | the model assesses each pending tool call before it runs; "can allow unsafe actions, deny useful work, spend tokens" | ocode's Jev permission judge, which is cheaper and decision-only |
| `experimental/session-search` | five read-only tools over earlier sessions, shared content index | the retrieval half of a lesson store |
| `experimental/personalized-recommendations` | a card under an answer proposing "keep this as a page/tool" or "enable this plugin"; nothing happens until the user clicks | the **approval-card** UX for candidate lessons |
| `experimental/ralph-bundle` | repeated work with fresh child agents | — |
| `.agents/notes/{proposed,implemented,rejected,archived}/<class>/<date>-topic.md` | **Agent Notes**: path-encoded decision records with a verified format, written by the agents that develop the harness; frozen archive, no central index, consolidation rules | the "recursive self-improvement" people attribute to DSH is this **development-process** loop (agents improving the harness source), not a runtime one. ocode's OKF bundle is the same idea |
| `.agents/skills/agent-experience` | rules for writing model-facing tool definitions ("start with minimal context, make discovery explicit, measure first-turn prompt tokens") | — |
| `docs/postmortem/NNNN-*.md` | numbered postmortems | — |

The Cordis kernel's unload-unwinds-side-effects property is what makes "swap your own plugins at runtime" safe
there; ocode has no equivalent and this plan does not propose one.

## What transfers to ocode, and what does not

**Transfers**
1. Lessons need **evidence**: a cited trajectory moment (Harness²), a structured trace (Recuris). Free-text
   "remember to be careful" entries are what makes memory not help (PAST-Bench, Recuris EM-alone).
2. **A gate that can say no**, with the evaluator outside the agent's writable scope (Schmid's taxonomy;
   Recuris's held-out anchors; Harness² hides rubrics from the editor). Without a gate, direct refinement
   degrades.
3. **Scoping**: edits target one component, prefer on-demand forms (skill, script) over always-on prose.
   Removing scoping cost Harness² the most.
4. **Working memory > experiential memory.** The cheapest win is a verified "what is still open" state, which
   ocode's persistent todo plan and goal loop already are. A lesson library is second-order.
5. **Never inherit automatically.** A library entry informs a fresh proposal; it is not pasted into every prompt.
6. **Human feedback is log-only** (DSH). Ratings steer the gate, never the model.
7. **Archive, don't delete**; dedupe aggressively (Recuris's 17 near-duplicates).

**Does not transfer as-is**
- Propose-Probe-Compose at 3k rollouts per task is an offline benchmark procedure. Interactive coding sessions
  are not re-runnable (side effects, user in the loop). It fits only where ocode re-runs the same task:
  **scheduled jobs** and the **Kaizen question corpus**.
- Harness²'s C8 plugin (TypeScript middleware the model writes) means model-authored code in the tool loop.
  ocode's hooks and `/ban` rules are the equivalent, and they stay human-authored.
- Editing C1 system prompt / C2 guardrails automatically. In ocode these are `CLAUDE.md`/`OCODE.md` and the
  permission rules, both behind the self-escalation guard. They stay out of the writable set.
