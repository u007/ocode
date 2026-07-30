# Terminal-Bench Harness + Token Reduction for `deepseek-v4-flash` (Design)

Date: 2026-07-30
Status: draft-pending-review (revised after verification pass)

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

## Verified constraints (read from the installed TB source, not assumed)

Verified against `terminal_bench` as installed by `uvx --from terminal-bench`.

- **The agent runs inside the task container.** `AbstractInstalledAgent`
  requires `_env` (host env forwarded in), `_install_agent_script_path`, and
  `_run_agent_commands(instruction)`. The reference implementations are
  `ClaudeCodeAgent` and — much closer to ocode — `OpenCodeAgent`, which is a
  Go/CLI agent invoked as `opencode --model <provider/model> run <instruction>`.
- **The install script is a Jinja2 template.** `_get_templated_script_path`
  renders `<name>-setup.sh.j2` from the agent's own directory with template
  variables (`version` by default), writes it to a temp file, and chmods it.
  So the file is `ocode-setup.sh.j2`, not a plain `.sh`.
- **TB copies arbitrary files into the container.** `perform_task` calls
  `session.copy_to_container(...)`, which accepts a path or list of paths with
  a target dir and filename. **This resolves the binary-delivery question** —
  see below.
- **The agent is driven through a tmux session**, via `session.send_keys` /
  `session.send_command`. Commands are typed into an interactive pane, so
  stdout is *not* a captured pipe. Any machine-readable output must be
  explicitly redirected to a file inside the container.
- **TB has a first-class token-reporting channel.** `AgentResult` carries
  `total_input_tokens` and `total_output_tokens`, and `harness.py` copies both
  onto the per-trial results record. `AbstractInstalledAgent.perform_task`
  hardcodes them to `0`. **This changes the metrics design** — see below.
- **Install failure is a distinct outcome.** `perform_task` detects
  `INSTALL_FAIL_STATUS` in the pane and returns
  `FailureMode.AGENT_INSTALLATION_FAILED`, so a broken install is never
  miscounted as a task failure. No custom handling needed.
- **`CONTAINER_AGENT_LOGS_PATH` is `/agent-logs`**, mounted back to the host
  `logging_dir` passed to `perform_task`.
- **Verified `tb run` flags**: `--agent-import-path <module:Class>`,
  `-t/--task-id` (repeatable, glob-capable), `--n-attempts`, `--n-concurrent`,
  `--output-path`, `--global-agent-timeout-sec`, and `--dataset name==version`.
  There is **no `--dataset-version` flag** — version is part of the `--dataset`
  value. `terminal-bench-core==0.1.1` is the current pinnable version
  compatible with this TB release.
- **API egress from the container is expected but unproven for our host.**
  Every installed agent calls its provider API from inside the container. The
  smoke test (Phase 3) confirms egress to `https://opencode.ai/zen/go/v1`
  before anything is built on top of it. TB's own docstring warns that
  installed agents "may fail due to properties of the task container rather
  than the agent's inability to perform the task (e.g. volume constraints,
  broken networking)" — so this is a real risk, not a formality.
- **`OPENCODE_API_KEY` is not in the host environment.** The credential lives
  in `~/.local/share/opencode/auth.json` under the `opencode-go` provider
  (`internal/auth/store.go:146`). The adapter must read it from there, not
  from `os.environ`. Forwarding it as an env var into the container is
  sufficient: `auth.HydrateEnv` (`internal/auth/providers.go:277`) treats an
  already-set env var as highest precedence, so no auth file needs to be
  seeded in the container.
- **`ocode run -format json` emits no token usage.** `outputJSONEvents`
  (`internal/runcli/run.go:391`) emits only `text` and `tool_use` events.
  Sessions do not persist usage either — `internal/session/` has no usage field.
- **The plumbing to fix that already exists, and is verified live on this
  model's code path.** `runcli` builds a real `*agent.Agent`
  (`internal/runcli/run.go:218`), which exposes a public `OnUsage` field that
  `chatWithDelta` installs on the client for the duration of each call
  (`internal/agent/agent.go:503`). No client plumbing is needed — `runcli` just
  assigns the field. **`Agent.Step` is a full agentic loop**, not a single turn
  (`agent.go:843` iterates until a response has no tool calls, bounded by
  `maxSteps`, default 100), so `ocode run -p "<task>"` is a complete run — the
  premise the whole adapter rests on. For `opencode-go/deepseek-v4-flash` the request takes the
  OpenAI-completions branch (`usesAnthropicMessagesAPI` returns true only for
  `minimax-*`, `client.go:431`), which streams through
  `parseOpenAIChatCompletionsStream` and invokes `onUsage` per response
  (`client.go:1425`).
- **The gateway emits usage without `stream_options.include_usage`.** This was
  the main risk to the whole plan: `chatOpenAI` sets `"stream": true` but never
  requests `stream_options` (`client.go:755`), and most OpenAI-compatible APIs
  omit usage from streams unless asked. Verified by direct call to
  `https://opencode.ai/zen/go/v1/chat/completions` — the final chunk carries a
  full `usage` object (including `prompt_cache_hit_tokens` and
  `completion_tokens_details.reasoning_tokens`) with and without the flag. **No
  client change is required.**

## Architecture

Three components, built in order. Each is independently useful.

### 1. Token accounting in `ocode run` (prerequisite)

`runcli` assigns `ag.OnUsage` (the agent's public callback field) to an
accumulator that sums input and output tokens across every model call in the
run, and counts the calls. At the end of a `-format json` run, it emits one
final event:

```
{"type":"usage","sessionID":"...","input_tokens":N,"output_tokens":N,"total_tokens":N,"model_calls":N,"model":"..."}
```

`model_calls` is included because turn count is a primary optimization target
(see the levers section) and it is free to collect here.

On accumulation semantics: the OpenAI-style parser reports each response's
absolute prompt/completion counts, so summing across calls gives total tokens
billed for the run — the metric we want. (The Anthropic-style path reports
deltas instead; irrelevant for this model but worth not generalizing from.)

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

Python package name uses underscores so `--agent-import-path` can import it:

```
bench/terminal_bench/
  README.md            — how to run a bench sweep end to end
  __init__.py
  ocode_agent.py       — OcodeAgent(AbstractInstalledAgent)
  ocode-setup.sh.j2    — in-container install script (Jinja2 template)
  subset.txt           — the frozen task-ID subset (one ID per line)
  dist/                — gitignored; linux ocode binary built by make build-linux
  runs/                — gitignored; tb output paths
```

**`ocode_agent.py`** implements the three required members plus one override:

- `_env` — reads the `opencode-go` API key from
  `~/.local/share/opencode/auth.json` (falling back to `OPENCODE_API_KEY` if
  the user has exported it) and forwards it as `OPENCODE_API_KEY`, plus
  `OCODE_MODEL` from the `--model` TB passes.
- `_install_agent_script_path` — `self._get_templated_script_path("ocode-setup.sh.j2")`.
- `_run_agent_commands(instruction)` — one blocking `TerminalCommand`
  modelled on `OpenCodeAgent`:
  `ocode run -yolo -format json -m "$OCODE_MODEL" -p <shlex-quoted instruction> > /agent-logs/ocode-run.jsonl 2>/agent-logs/ocode-run.err`.
  The redirect is mandatory, not stylistic — the command runs in a tmux pane,
  so unredirected stdout is only recoverable by scraping the pane.
- `perform_task` override — calls `super().perform_task(...)`, then parses the
  trailing `usage` event from the host-side `logging_dir/ocode-run.jsonl` and
  returns an `AgentResult` with the real `total_input_tokens` /
  `total_output_tokens` instead of the base class's hardcoded zeros. It
  preserves `failure_mode` from the super call untouched. This is what makes
  token cost a first-class TB metric rather than something scraped later.

**`ocode-setup.sh.j2`** makes the copied binary executable at a known path,
puts it on `PATH`, and seeds a minimal `ocodeconfig.json` (model + provider,
permissions off). It does not download anything.

**Binary delivery is solved, not open.** `perform_task` already copies the
install script via `session.copy_to_container`, which takes arbitrary paths.
The adapter's `perform_task` override copies `dist/ocode-linux-<arch>` (built
by `make build-linux`) into the container before calling `super()`. No GitHub
release, no `go install`, no host-side file server — all three fallbacks from
the previous draft are dropped. The binary is built from a known commit and
its `ocode version` is recorded in the run label, since a floating binary makes
two runs non-comparable.

### 3. Sweep

A thin script (`bench/terminal_bench/sweep.sh`) wraps:

```
uvx --from terminal-bench==0.2.18 tb run \
  --agent-import-path bench.terminal_bench.ocode_agent:OcodeAgent \
  --model opencode-go/deepseek-v4-flash \
  --dataset terminal-bench-core==0.1.1 \
  --n-attempts 3 --n-concurrent 4 \
  --global-agent-timeout-sec 900 \
  --output-path bench/terminal_bench/runs/<label> \
  $(sed 's/^/-t /' subset.txt)
```

Run from the repo root. Verified: `--agent-import-path` resolves a repo-local
package under `uvx` because the working directory is on `sys.path` — a bogus
leaf module under `bench.terminal_bench` fails with "No module named
`bench.terminal_bench.<leaf>`", meaning the parent package imported fine. No
`PYTHONPATH` juggling needed.

**Everything that affects comparability is pinned**: the TB version
(`terminal-bench==0.2.18`), the dataset (`terminal-bench-core==0.1.1`), and the
ocode binary commit. A floating `uvx --from terminal-bench` would let a TB
upgrade change scoring between the baseline and a later run, which is exactly
the comparability problem the frozen subset exists to prevent. TB version and
`ocode version` are both recorded in the run label.

`--global-agent-timeout-sec` is the backstop against a hung run: the
`TerminalCommand` uses `max_timeout_sec=float("inf")` (copied from
`OpenCodeAgent`), so any tool that blocks on stdin would otherwise hang the
tmux pane forever. `-yolo` should prevent permission prompts; the smoke test
confirms no prompt-hang rather than assuming it.

Because the adapter reports tokens through `AgentResult`, TB's own per-trial
results carry pass/fail *and* token counts together. The sweep script only
aggregates that output into a per-config comparison table (task ID, pass rate
across attempts, mean input/output tokens, wall time) — it does not need to
scrape container logs for token data.

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

## What actually drives token cost on this model (measured)

Before picking levers, the cost shape was measured directly against the
gateway. Two results reframe the problem:

**Output tokens are almost entirely reasoning, and we cannot turn them down.**
For a trivial prompt ("write a bash one-liner to count lines in .txt files"),
`deepseek-v4-flash` spent a mean of ~1,900 reasoning tokens out of nearly all
its completion tokens. `reasoning_effort` is *accepted* by the gateway (no error) but showed no
measurable effect: over n=5 per level, means were 1912 (unset), 1531 (low),
and 1691 (high), with per-sample spreads of 762–3363 that overlap completely.
Five samples cannot prove the parameter is ignored, but they are more than
enough to drop it as a lever — any real effect is smaller than the noise.
(Related: `opencode-go` is not in
`providerSupportsReasoningEffort`, `client.go:1719`, so ocode never sends it
anyway — and there is now no reason to add it.)

**Therefore the controllable levers are input tokens and turn count.** Output
cost per turn is a fixed tax set by the model. What we control is how many
tokens we send it and how many times we pay that tax. Turn count is a
first-class metric, not a curiosity — halving turns roughly halves the
uncontrollable reasoning spend.

## Optimization levers (round one, in expected-value order)

Hypotheses to test against the measured baseline, not a commit list:

1. **Anything that reduces turn count** — better first-shot tool selection,
   fewer redundant reads, less flailing. Highest leverage because each turn
   re-pays the full reasoning tax, and it is the one lever that cuts output
   tokens at all.
2. **System-prompt weight for the DeepSeek family** —
   `internal/agent/prompts/deepseek.txt` and `deepseek-v4-flash.OCODE.md` are
   prepended to every request. Every token there is paid on every turn.
   (Note the tension with lever 1: trimming guidance that prevents flailing can
   raise turn count and cost more than it saves. Measure both.)
3. **Tool schema and description bloat** — tool definitions are re-sent each
   call; verbose descriptions are a per-turn tax.
4. **Tool-output truncation limits** — large `read`/`bash` outputs dominate
   context growth in long tasks.
5. **Prompt-cache hit rate** — the gateway reports `prompt_cache_hit_tokens`,
   which was 0 in every probe. If a stable prefix can be established across
   turns, this is a large input-token win. Investigate before assuming it is
   unavailable.
6. **Compaction thresholds** (`internal/agent/compact.go`) — compacting too
   late wastes tokens; too early loses task state and costs a retry.
7. **Skill injection** — skills loaded but unused are pure overhead.

Each lever is changed one at a time, measured, and kept only if tokens drop
without a score regression outside the measured spread.

## Testing

- **Unit** — Go test in `internal/runcli/` asserting a `-format json` run emits
  exactly one trailing `usage` event with non-zero totals, using the existing
  test client fixtures.
- **Adapter smoke** — 2 TB tasks, `--n-attempts 1`, verifying: container gets
  the binary, `OPENCODE_API_KEY` reaches the model, egress to `opencode.ai`
  succeeds, `ocode-run.jsonl` lands on the host with a `usage` event, and TB's
  own results record shows non-zero `total_input_tokens`. Also confirms two
  container-environment risks: that no tool blocks on stdin (which would hang
  the tmux pane), and that `session.Save` (`run.go:273`) can write — it runs
  *after* the work completes, so an unwritable `HOME` in the task image would
  fail the run only after all the tokens were spent.
- **Baseline** — the frozen subset, `--n-attempts 3`, recorded in
  `bench/terminal-bench/README.md` as the number every later config is
  compared against.

## Error handling

- No resolvable `opencode-go` credential on the host (neither
  `~/.local/share/opencode/auth.json` nor an exported `OPENCODE_API_KEY`): the
  adapter fails at construction with a named error, not mid-run after Docker
  spin-up.
- Missing `dist/ocode-linux-<arch>`: the adapter fails at construction telling
  the user to run `make build-linux`, rather than failing inside the container
  as a confusing install error.
- No `usage` event in a task's log (crash, timeout, killed container): recorded
  as `tokens: unknown` for that task and **counted in the report**, never
  silently dropped or defaulted to zero — a zero would quietly flatter the
  token average.
- Install-script failure: surfaced as a TB agent-install error with the script's
  stderr, so it is not mistaken for a task failure.

## Non-goals (this round)

- Cache-read/cache-write token reporting through the run's `usage` event (needs
  a wider `OnUsage` signature; deferred to `TODO.md`). Note this does not block
  lever 5 — cache hit rate can be investigated directly against the gateway.
- Adding `reasoning_effort` support for `opencode-go` — measured to have no
  effect on this model.
- Publishing results to the TB leaderboard.
- Optimizing any model other than `deepseek-v4-flash`.
- Auto-tuning: every prompt/config change is human-reviewed.

## Execution order

1. Token accounting in `ocode run` (`ag.OnUsage` accumulator + trailing `usage`
   event) with a unit test.
2. Adapter package, install-script template, and binary copy.
3. Two-task smoke test; confirm container egress, log collection, and non-zero
   tokens in TB's results.
4. Freeze the subset; record baseline (3 attempts).
5. Iterate levers one at a time against the baseline.

## Open risks

- **Container egress is the single unproven assumption.** If TB's task
  containers block outbound traffic to `opencode.ai`, the installed-agent
  approach cannot work and the design needs rethinking. Phase 3 exists to find
  this out before any optimization work is built on top. TB's own docs flag
  broken networking as a known failure mode for installed agents.
- **`subset.txt` is not yet populated.** The dataset is downloaded and contains
  80 tasks; the ~12-task subset is chosen once by stratified sample across task
  domains, written to `subset.txt`, and then frozen.
