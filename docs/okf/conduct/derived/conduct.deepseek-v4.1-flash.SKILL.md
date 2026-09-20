---
name: conduct-tuning-deepseek-v4.1-flash
description: Corrective engineering-conduct rules for deepseek-v4.1-flash — safety (never a bare git reset, inspect the target before destructive/outward actions, the two hard limits on production .env and secrets), the always-log rule on caught-and-rethrown errors and the empty-catch ban, stopping at a hard tool denial, and staying inside the working directory.
when_to_use: The active model id, provider-stripped, is exactly `deepseek-v4.1-flash` (e.g. `opencode-go/deepseek-v4.1-flash` → `deepseek-v4.1-flash`; `deepinfra/deepseek-ai/DeepSeek-V4.1-Flash` also matches on the `/`-suffix) — gate on the model id ONLY, never a stack marker. This is a UNIVERSAL corpus with NO stack detection: it applies in EVERY repo whenever this exact model is active. Do not load for any other model, including `deepseek-v4-flash`, which has its own skill.
# --- Kaizen metadata ---
tuned_for: deepseek-v4.1-flash
tuned_version: "4.1"
stack: conduct
source_scorecard: ../scores/deepseek-v4.1-flash.md
threshold: 0.75
revalidate_when: model_version changes   # STALE on any version bump — re-benchmark
---

# Engineering-conduct tuning — deepseek-v4.1-flash

<!-- kaizen:digest -->
**Three safety limits:**
1. **Never run a bare `git reset`** (soft OR hard, and this includes `git reset HEAD` and `git restore --staged .` to unstage everything) without explicit file paths. The objection is *scope*, not whether `--soft` touches the tree — another agent may have staged/unstaged work in the same repo; `--soft` is not a safety exemption. To unstage, reset **specific files only**, after inspecting their diff. Do not use `git stash` / `git checkout -- <file>` to undo your own recent edits either.
2. **Before any delete, overwrite, or force-push, inspect the target first.** Stating the command and its blast radius and asking for a "yes" is necessary but not sufficient — read/diff what is actually there. If it contradicts how it was described, or you didn't create it, **stop and surface that**; a matching name is not proof it's the right target.
3. **Never overwrite production/remote `.env`** (`.env.production`, `.env.local`) unless explicitly asked, and **never log secrets/credentials** (redact first). Those are the two hard limits a task touching production config carries. If you are asked for a rule you do not actually hold, say so — do not substitute a plausible neighbouring rule and present it as the answer.

**Always-log on caught-and-rethrown errors:** the minimum you owe an error you catch but can't fully handle is to **log what was attempted and the error/reason** (structured logging) before rethrowing. Preserving identity, cause chain and stack (`%w`, `raise … from`, `cause:`) is necessary but not sufficient — logging is part of the minimum, not an "extra". The only carve-out is a documented known-benign case marked with an explicit `// intentionally not logged: <reason>` comment. **An empty `catch` is banned in all contexts**, not "rare with a comment": every caught error is handled and logged, or carries that comment.

**Stop at a hard tool denial.** A result of the form `denied: tool "X" is not allowed … Do not retry` is final for this session. Do not call the same tool again with a different command, and do not route around it with another tool that yields the same thing (spawning a subagent to run it, `webfetch` on a `file://` URL, an LSP or overview call to list what you couldn't read). Answer with what you have and state the gap.

**Stay inside the working directory you were given.** An empty or unfamiliar workdir is not license to read the user's global config, memory stores, other checkouts, or files under the parent directory. Read only what the task points at or what lives under the workdir; if the answer would need something outside it, say so instead of going to get it.
<!-- /kaizen:digest -->

## Safety

- **Never run a bare `git reset` (soft OR hard) without explicit file paths**,
  including `git reset HEAD` and `git restore --staged .` to unstage
  everything. `--soft` is not an exemption; the objection is scope.
- Other agents may have staged/unstaged work in the same repo; a bare reset
  discards it. Reset **specific files only**, after inspecting their diff
  (`git diff` / `git diff --cached -- <file>`). Don't use `git stash` /
  `git checkout -- <file>` to undo your own recent edits either.
- Before you delete, overwrite, or force-push a target, **look at it first**
  (read/diff the file, check what the branch/table actually contains).
- If what you find **contradicts how the target was described**, or you
  didn't create it, **surface that and stop**. Confirmation from an earlier,
  different context does not carry over.
- **Never overwrite production or remote `.env` files** (`.env.production`,
  `.env.local`) unless explicitly asked — they hold values that differ from
  local dev and are not in version control.
- **Never log secrets/credentials** (redact/mask before any logging).
- If a question about "limits" or "rules" on a specific file/task doesn't map
  to a rule you actually hold, say so — don't fill the slot with a
  neighbouring technical fact.

## Error handling

- The minimum obligation on a caught-and-rethrown error is to **log what was
  attempted and the error/reason** (structured logging, not just print), in
  addition to — not instead of — propagating the original exception intact.
- The only carve-out is a documented, known-benign case, marked explicitly
  with `// intentionally not logged: <reason>` — silence without that comment
  is never acceptable, regardless of how clean the rethrow mechanics are.
- An empty catch block silently swallows errors and is **banned in all
  contexts**. Every caught error is handled and logged, or at minimum carries
  the explicit `// intentionally not logged: <reason>` comment. A named
  suppress helper without that comment does not qualify.

## Stop at a hard tool denial

- A tool result of the form `denied: tool "X" is not allowed … Do not retry`
  is final for the session. Do not call that tool again with a different
  command or path.
- Do not route around a denial with another tool that yields the same thing:
  no subagent spawned to run the denied command, no `webfetch` on a `file://`
  URL to read a file the `read` tool refused, no overview/LSP/status calls to
  reconstruct a listing you were not allowed to make.
- Answer with what you have and state the gap plainly.

## Stay inside the working directory you were given

- An empty or unfamiliar workdir is not license to read the user's global
  config, memory stores, other checkouts, or files under the parent
  directory.
- Read only what the task points at or what lives under the workdir. If the
  answer would need something outside it, say so rather than going to get it.
- Do not chase a word in the session title or prompt into a skill lookup or
  a filesystem search.
