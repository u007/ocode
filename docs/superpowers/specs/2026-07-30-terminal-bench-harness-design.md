# Terminal-Bench Harness + Token Reduction for `deepseek-v4-flash` (Design)

Date: 2026-07-30
Status: draft-pending-review

## Purpose

Two coupled goals:

1. **Measure** ocode's agentic terminal ability with Terminal-Bench (TB), so
   changes to prompts, tools, and context handling can be judged against a
   score instead of a hunch.
2. **Reduce token usage** when ocode runs on `opencode-go/deepseek-v4-flash`,
   without losing task-completion rate.

Neither goal is reachable today: ocode has no TB adapter, and `ocode run`
never reports how many tokens a run consumed.

## Why Terminal-Bench (and not something else)

TB measures what we actually care about: an agent driving a real shell in a
real container across many steps, scored pass/fail by the task's own tests.
That is the exact shape of ocode's job.

The usual alternative, SWE-bench Verified, measures *patch generation* against
a repo snapshot. It is a fine benchmark, but it is not a terminal-agency
benchmark — an agent can score well on it while being poor at multi-step shell
work. Wrong instrument for this question.

TB's real costs are accepted, not ignored: one Docker container per task, slow
runs, and high variance on small subsets. The variance is handled explicitly
below.

## Verified constraints (checked, not assumed)

- **The agent runs inside the task container.** TB's `AbstractInstalledAgent`
  requires three things: `_env` (host env vars forwarded into the container),
  `_install_agent_script_path` (a shell script that installs the agent in the
  container), and `_run_agent_commands(instruction)` (the headless invocation).
  The reference implementation is `ClaudeCodeAgent`.
- **Container agent-logs path is mounted to the host** — TB exposes
  `CONTAINER_AGENT_LOGS_PATH` for trajectory artifacts. This is where per-task
  token accounting will be written so the host can collect it.
- **API egress from the container is expected.** The Claude Code adapter
  forwards `ANTHROPIC_API_KEY` and calls the Anthropic API from inside the
  container; ocode will forward `OPENCODE_API_KEY` and call
  `https://opencode.ai/zen/go/v1` the same way. This is verified empirically in
  the smoke test (Phase 3) before anything is built on top of it.
- **`ocode run -format json` emits no token usage.** `outputJSONEvents`
  (`internal/runcli/run.go:391`) emits only `text` and `tool_use` events.
  Sessions do not persist usage either — `internal/session/` has no usage field.
- **The plumbing to fix that already exists.** `agent.TokenUsage`
  (`internal/agent/telemetry.go:10`) is parsed for both OpenAI- and
  Anthropic-shaped payloads, and `GenericClient.SetOnUsage`
  (`internal/agent/client.go:185`) is a usage callback hook that `runcli` can
  install.

## Architecture

Three components, built in order. Each is independently useful.

### 1. Token accounting in `ocode run` (prerequisite)

`runcli` installs a `SetOnUsage` callback that accumulates input and output
tokens across every model call in the run. At the end of a `-format json` run,
it emits one final event:

```
{"type":"usage","sessionID":"...","input_tokens":N,"output_tokens":N,"total_tokens":N,"model":"..."}
```

Scope discipline: this reports values ocode already parses. No new CLI flags,
no behavior changes. `-format default` and `-format summary` are untouched
except that `summary` gains a token line, since that is where a human reads
run cost.

Note on cache tokens: the existing `SetOnUsage` signature carries only input
and output counts. Cache-read/cache-write are parsed into `TokenUsage` but not
surfaced through that callback. This design does **not** widen the callback
signature — cache tokens are out of scope for round one, and are logged in
`TODO.md` as a follow-up. Prompt-cache effectiveness will be inferred from
input-token totals instead.

### 2. TB adapter (`bench/terminal-bench/`)

```
bench/terminal-bench/
  README.md            — how to run a bench sweep end to end
  ocode_agent.py       — OcodeAgent(AbstractInstalledAgent)
  ocode-setup.sh       — in-container install script
  subset.txt           — the frozen task-ID subset (one ID per line)
  runs/                — gitignored; tb output paths
```

**`ocode_agent.py`** implements the three required members:

- `_env` — forwards `OPENCODE_API_KEY` from the host, plus `OCODE_MODEL` set to
  `opencode-go/deepseek-v4-flash` (or whatever `--model` TB passes).
- `_install_agent_script_path` — points at `ocode-setup.sh`.
- `_run_agent_commands(instruction)` — a single blocking `TerminalCommand`:
  `ocode run -yolo -format json -m "$OCODE_MODEL" -p <shlex-quoted instruction>`,
  with stdout teed to `CONTAINER_AGENT_LOGS_PATH/ocode-run.jsonl` so the host
  can read the trailing `usage` event after the task finishes.

**`ocode-setup.sh`** installs a Linux `ocode` binary into the container and
seeds a minimal `ocodeconfig.json` (model + provider, permissions off). The
binary is produced by `make build-linux`. How the binary reaches the container
is the one open implementation question; resolved during Phase 3 smoke test,
in this preference order:

1. `curl` a pinned GitHub release asset (cleanest, needs a published release).
2. `go install github.com/u007/ocode@<pinned-sha>` if the task image has Go
   (fragile — most task images will not).
3. Host-side static file server reachable at `host.docker.internal` (works
   everywhere, but couples the run to a sidecar process).

Whichever wins is recorded in the adapter README. The version installed is
always pinned, never `latest` — a floating binary makes two runs
non-comparable.

### 3. Sweep + metrics collection

A thin script (`bench/terminal-bench/sweep.sh`) wraps:

```
uvx --from terminal-bench tb run \
  --agent-import-path bench.terminal_bench.ocode_agent:OcodeAgent \
  --model opencode-go/deepseek-v4-flash \
  --dataset terminal-bench-core --dataset-version <pinned> \
  --n-attempts 3 --n-concurrent 4 \
  --output-path bench/terminal-bench/runs/<label> \
  $(sed 's/^/-t /' subset.txt)
```

then walks the run's agent-logs directories, reads the trailing `usage` event
from each `ocode-run.jsonl`, and writes a per-config row: task ID, pass/fail
per attempt, input/output/total tokens, wall time, tool-call count.

## Handling variance (the part that decides whether this is useful)

A 12-task subset means one task flipping moves the score by ~8 points. Without
discipline, prompt iteration becomes noise-chasing. So:

- **The subset is frozen.** Task IDs are chosen once by stratified sample
  across TB domains, written to `subset.txt`, and never re-picked between
  configs. Changing the subset invalidates every prior comparison.
- **`--n-attempts 3` minimum** for any score acted on. Pass rate is always
  reported with its spread across attempts, never as a bare number.
- **Tokens are the primary fast signal; score is the slow confirmation.**
  Token-per-task is near-deterministic compared to pass@1, and it is half the
  actual goal. The tight iteration loop optimizes tokens and watches score for
  regression; the full-subset 3-attempt run is the periodic confirmation.
- **The full dataset is run only for a final number**, once the subset-level
  improvements have stabilized.

## Optimization levers (round one, in expected-value order)

These are hypotheses to test against the measured baseline, not a commit list:

1. **System-prompt weight for the DeepSeek family** —
   `internal/agent/prompts/deepseek.txt` and `deepseek-v4-flash.OCODE.md` are
   prepended to every request. Every token there is paid on every turn.
2. **Tool schema and description bloat** — tool definitions are re-sent each
   call; verbose descriptions are a per-turn tax.
3. **Tool-output truncation limits** — large `read`/`bash` outputs dominate
   context growth in long tasks.
4. **Compaction thresholds** (`internal/agent/compact.go`) — compacting too
   late wastes tokens; too early loses task state and costs a retry.
5. **Skill injection** — skills loaded but unused are pure overhead.

Each lever is changed one at a time, measured, and kept only if tokens drop
without a score regression outside the measured spread.

## Testing

- **Unit** — Go test in `internal/runcli/` asserting a `-format json` run emits
  exactly one trailing `usage` event with non-zero totals, using the existing
  test client fixtures.
- **Adapter smoke** — 2 TB tasks, `--n-attempts 1`, verifying: container gets
  the binary, `OPENCODE_API_KEY` reaches the model, egress to `opencode.ai`
  succeeds, and `ocode-run.jsonl` lands on the host with a `usage` event.
- **Baseline** — the frozen subset, `--n-attempts 3`, recorded in
  `bench/terminal-bench/README.md` as the number every later config is
  compared against.

## Error handling

- Missing `OPENCODE_API_KEY` on the host: the adapter fails at construction
  with a named error, not mid-run after Docker spin-up.
- No `usage` event in a task's log (crash, timeout, killed container): recorded
  as `tokens: unknown` for that task and **counted in the report**, never
  silently dropped or defaulted to zero — a zero would quietly flatter the
  token average.
- Install-script failure: surfaced as a TB agent-install error with the script's
  stderr, so it is not mistaken for a task failure.

## Non-goals (this round)

- Cache-read/cache-write token reporting (needs a wider `SetOnUsage`; deferred
  to `TODO.md`).
- Publishing results to the TB leaderboard.
- Optimizing any model other than `deepseek-v4-flash`.
- Auto-tuning: every prompt/config change is human-reviewed.

## Execution order

1. Token accounting in `ocode run` + unit test.
2. Adapter + install script; resolve binary-delivery method.
3. Two-task smoke test; confirm egress and log collection.
4. Freeze the subset; record baseline (3 attempts).
5. Iterate levers one at a time against the baseline.
