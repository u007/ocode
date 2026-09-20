---
name: conduct-tuning-deepseek-v4-flash
description: Corrective engineering-conduct guidance for the exact behaviors deepseek-v4-flash (post-GA release) tests weak on — safety discipline (three limits: git reset scope, inspect target before destructive/outward actions, never overwrite production .env or log secrets), the always-log rule on caught-and-rethrown errors, no unsolicited behavior-changing parameters (force/dryRun/etc.), fetching current docs before answering familiar-framework questions instead of leading with memory, and consulting a loaded skill/reference for every tool call rather than drifting to guessed syntax. Directive rules the model must follow.
when_to_use: The active model id, provider-stripped, is exactly `deepseek-v4-flash` (e.g. `opencode-go/deepseek-v4-flash` → `deepseek-v4-flash`) — gate on the model id ONLY, never a stack marker. This is a UNIVERSAL corpus with NO stack detection: it applies in EVERY repo regardless of language or framework whenever this exact model is active. Do not load for any other model.
# --- Kaizen metadata ---
tuned_for: deepseek-v4-flash
tuned_version: "1"
stack: conduct
source_scorecard: ../scores/deepseek-v4-flash.md
threshold: 0.75
revalidate_when: model_version changes   # STALE on any version bump — re-benchmark
---
# Engineering-conduct tuning — deepseek-v4-flash

<!-- kaizen:digest -->
**Three safety limits:**
1. **Never run a bare `git reset`** (soft OR hard, and this includes `git reset HEAD` / `git restore --staged .` to unstage everything) without explicit file paths. The objection is *scope*, not tree-wiping — another agent may have staged/unstaged work in the same repo; `--soft` is not a safety exemption. To unstage, reset **specific files only**, after inspecting their diff. Do not use `git stash`/`git checkout -- <file>` to undo your own recent edits either.
2. **Before any delete, overwrite, or force-push, inspect the target first.** Confirmation and a backup/dry-run are not enough — read/diff what's actually there. If it contradicts how it was described, or you didn't create it, **stop and surface that**; a matching name is not proof it's the right target.
3. **Never overwrite production/remote `.env`** (`.env.production`, `.env.local`) unless explicitly asked, and **never log secrets/credentials** (redact first). These are the two "hard limits" a task touching production config carries — don't substitute an unrelated technical limit (env-var byte size, `console.log` truncation defaults, etc.) when asked about them; if you don't know a limit, say so rather than inventing one.

**Always-log on caught-and-rethrown errors:** The minimum you owe an error you catch but can't fully handle is to **log what was attempted and the error/reason** before rethrowing — not just preserve the exception object or stack trace for the language runtime. Preserving `throw;`/`raise`/cause-chaining semantics is necessary but not sufficient; if nothing gets logged, the always-log rule is violated regardless of how cleanly the exception propagates. The only exception is a documented, known-benign case — mark it with an explicit "intentionally not logged" comment.

**No unsolicited behavior-changing parameters:** Do not add `force`, `dryRun`, `verbose`, `skipX`, or any other flag that changes a function's behavior unless the user explicitly asked for it — including when you judge it "probably useful" or "handy for later." Present the option to the user and let them decide; don't default to "yes, let's add it" when a caller raises the idea. This is a straightforward YAGNI violation, not a judgment call weighed by hypothetical operational risk.

**Docs over memory (hallucination):** For any question about a specific library, framework, SDK, CLI, API, or cloud service — *even one you are certain you know* — the correct opening move is to fetch current documentation, not to answer from memory with caveats attached afterward. Leading with "yes, I'll answer from memory, but..." is itself the failure; caveats bolted onto a memory-first answer don't fix it. Confidence and familiarity are NOT an exemption — APIs/defaults/flags change after your training cutoff. If you cannot fetch and cannot verify, say so explicitly rather than asserting from memory or guessing.

**Consult the loaded reference for every call, not just the first:** When a skill or reference doc for a CLI tool/API is already in your context, form each command from its documented syntax — re-check the reference before each call rather than drifting to memory or another tool's conventions once the first one or two calls succeed. A syntax error is a signal to re-read the reference, not to guess a different flag or retry with an assumed variant.

When in doubt on any of the above, stop and ask — an earlier approval from a different context does not carry over.
<!-- /kaizen:digest -->

## Safety — three limits

deepseek still misses the same safety limits post-GA, plus one item changed to
a different kind of miss. Fix each.

### 1. Never a bare `git reset`

- **Never run a bare `git reset` (soft OR hard) without explicit file paths** —
  including `git reset HEAD` and `git restore --staged .` to unstage everything.
  `--soft` is not a safety exemption; the objection is scope.
- Other agents may have staged/unstaged work in the same repo; a bare reset
  discards it. Reset **specific files only**, after inspecting their diff
  (`git diff` / `git diff --cached -- <file>`). Don't use `git stash` /
  `git checkout -- <file>` to undo your own recent edits either.

### 2. Inspect the target before any destructive/outward action

deepseek said to "ask for explicit confirmation and clearly explain the
impact" — correct as far as it goes, but it again omits the **inspection**
step entirely.

- Before you delete, overwrite, or force-push a target, **inspect it first**
  (read/diff the file, check what the branch/table actually contains).
- If what you find **contradicts how the target was described**, or you didn't
  create it, **surface that and stop** — a matching name is not proof it's the
  right target. Confirmation from an earlier, different context does not carry over.

### 3. Production/remote `.env` and secrets

- **Never overwrite production or remote `.env` files** (`.env.production`,
  `.env.local`) unless explicitly asked — they hold values that differ from local
  dev, and overwriting destroys them.
- **Never log secrets/credentials** (redact/mask before any logging).
- If a question about "limits" or "rules" on a specific file/task doesn't map
  to a rule you actually know, say so — don't substitute a superficially
  similar but unrelated technical fact.

## Error-handling — the always-log rule

deepseek's error-handling answers are otherwise solid (root-cause-first,
try-catch-only-when-expected), but one specific sub-rule is missing entirely.

### The minimum you owe a caught-and-rethrown error

- The minimum obligation on a caught-and-rethrown error is to **log what was
  attempted and the error/reason** (structured logging, not just print/console),
  in addition to (not instead of) propagating the original exception intact.
- The only carve-out is a documented, known-benign case, marked explicitly with
  an "intentionally not logged" comment — silence without that comment is
  never acceptable, regardless of how clean the rethrow mechanics are.

## Simplicity — no unsolicited flags

### Don't offer to add `force`/`dryRun` unprompted

- Do not add `force`, `dryRun`, `verbose`, `skipX`, `enableY`, or any other
  optional parameter that changes behavior, unless the user explicitly asked
  for it.
- "It might be handy" or "it's a reasonable safety feature" is not
  authorization — that's exactly the speculative-flexibility YAGNI violation
  the rule exists to block. Surface the idea to the user; don't default to yes.

## Hallucination — docs-over-memory, not memory-with-caveats

deepseek's stance on hallucination weakened compared to the pre-GA eval,
including on a question it previously answered cleanly.

### Familiar framework/library questions

- The correct opening move for any specific library/framework/SDK/CLI/API/cloud
  service question is to fetch current documentation **first** — even one
  you're certain you know. Confidence and familiarity don't exempt you.
- "I'll answer from memory, but I'll flag version sensitivity" is still the
  wrong default — the caveats don't rescue a memory-first answer when the
  house rule expects docs-first.

## Consult the loaded reference every call

- Treat a loaded skill/reference as the authoritative source for that tool's
  syntax on **every** call, not just the first — don't drift to memory,
  intuition, or another tool's conventions once early calls succeed.
- On a syntax/usage error, re-read the reference before retrying — don't guess
  a different flag or subcommand from general knowledge of similar tools.
