# Terminal-Bench Harness for ocode

Runs ocode (`opencode-go/deepseek-v4-flash`) against the
[Terminal-Bench](https://github.com/terminal-bench/terminal-bench) benchmark
and reports real token costs through TB's own `AgentResult`.

## Prerequisites

1. **Docker** — the adapter runs ocode inside task containers managed by
   Terminal-Bench. Verify with `docker version --format '{{.Server.Version}}'`.

2. **Linux ocode binary** — build it and copy into `dist/`:

   ```bash
   cd /path/to/ocode
   make build-linux
   mkdir -p bench/terminal_bench/dist
   cp ocode-linux-arm64 ocode-linux-amd64 bench/terminal_bench/dist/
   ```

   Both architectures are built because the smoke test may use either
   (Apple Silicon host → `arm64`, but task containers are `linux/amd64`
   unless overridden).

3. **Authenticated `opencode-go` provider** — the adapter reads the API key
   from `~/.local/share/opencode/auth.json` or the `OPENCODE_API_KEY`
   environment variable. Run `ocode` and authenticate the provider once, or
   export the variable.

## Running a Sweep

A sweep runs the frozen task subset (see `subset.txt`) with pinned versions
for reproducibility:

```bash
bench/terminal_bench/sweep.sh <label> [extra tb args...]
```

Examples:

```bash
# Record a baseline
bench/terminal_bench/sweep.sh baseline

# Record an experimental run with extra timeout
bench/terminal_bench/sweep.sh experiment-1 --global-agent-timeout-sec 1200
```

Results go to `bench/terminal_bench/runs/<label>/`.

## Pinned Versions

Every version is pinned in the sweep script so two runs are comparable:

| Component | Version | Why |
|-----------|---------|-----|
| `terminal-bench` | `0.2.18` | Harness framework |
| `terminal-bench-core` | `0.1.1` | Dataset (no `--dataset-version` flag — version is part of `--dataset` value) |
| ocode binary | Current commit | Built by `make build-linux`; never a floating/latest binary |
| Model | `opencode-go/deepseek-v4-flash` | Model under test |

## Frozen Subset

The subset in `subset.txt` is **frozen**. Re-picking it invalidates every
prior comparison. Chosen by stratified sample across
`terminal-bench-core==0.1.1` (80 tasks) to cover diverse domains while
keeping the sweep affordable.

## Smoke Test Results

### 2026-07-31 (verified end-to-end)

Three-task smoke test (`hello-world`, `sqlite-db-truncate`, `fix-permissions`)
with `--n-attempts 1 --n-concurrent 3`:

| Check | Status | Evidence |
|-------|--------|----------|
| Install succeeded | ✅ | `ocode version` printed `0.8.33` in every container; no `INSTALL_FAIL_STATUS` |
| Egress works | ✅ | LLM calls to `opencode.ai` succeeded inside every container (the design's one unproven assumption — confirmed) |
| Log landed | ✅ | `sessions/ocode-run.jsonl` exists per task with trailing `"type":"usage"` line |
| Tokens propagated | ✅ | `total_input_tokens` non-zero in results: hello-world 35,635 / sqlite-db-truncate 265,581 / fix-permissions 45,224 |
| No prompt-hang / session save | ✅ | All runs completed well under the 600s timeout; no `save session` errors in `ocode-run.err` |

Results: **3/3 resolved (100%)** with real token accounting.

### 2026-07-31 (first attempt — diagnostic)

The original two-task smoke (`chess-best-move`, `count-dataset-tokens`) hit the
900s global timeout — both tasks are intrinsically long (the chess agent
flailed on image analysis for ~109 messages; the dataset agent was tokenizing a
large HF dataset). Egress and install were confirmed even there (53 successful
LLM calls in the chess container, one transient Docker-internal DNS blip at the
end). This surfaced a real bug: the adapter wrote to `/agent-logs/`, but the
dataset's compose files only mount `/logs` (→ host `sessions/`), so no log
landed on the host and tokens stayed zero. Fixed by writing to `/logs/` and
parsing from `sessions/ocode-run.jsonl`.

## Key Design Decisions

- **No new CLI flags on `ocode run`.** Token reporting rides the existing
  `-format json` and `-format summary` paths.
- **`None` for unknown cost.** A crashed or timed-out task reports unknown
  token cost, never zero. Zeros would pull the mean token count down and make
  a broken run look cheap.
- **Fail fast on host.** `OcodeAgent.__init__` resolves both the API key and
  the Linux binary before Docker spins up, so a misconfigured host fails
  immediately.
- **Architecture check via `ocode version`.** The install script runs
  `ocode version` at the end. A wrong-architecture binary fails here as
  `AGENT_INSTALLATION_FAILED`, not as a task failure.
