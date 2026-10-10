# ocode Testing & Status

Track tested features, provider integrations, and known issues.

## Platform Support

| Platform | Status | Notes |
|----------|--------|-------|
| macOS | ✅ Tested | Full TUI support, all features working |
| Linux | ✅ Tested | Full TUI support, all features working |
| Windows | 🆘 Needs volunteer | WSL2 recommended; native Windows testing needed |

## Tested & Working ✅

### Provider Integrations
- [x] OpenAI API (ChatGPT subscription models)
- [x] Deepseek API
- [x] Xiaomi Coding (xiaoMi coding plan)
- [x] Zend Models (opencode zend)
- [x] Anthropic API (Claude models)

### Features
- [x] Compaction with small model (configurable summary provider/model)
- [x] Automatic permission decisions (`auto_permission_model`)
- [x] Extended thinking toggle (`Ctrl+T` on supported models)
- [x] Session auto-save and resume
- [x] MCP client (local + remote servers)
- [x] Git integration (status, diff, staging, commits, branches)
- [x] File browser with inline vim editor
- [x] LSP integration (hover docs, go-to-definition)
- [x] Theme system (tokyonight, catppuccin-mocha, etc.)
- [x] Permissions system (normal, yolo, locked modes)
- [x] Tool result truncation and on-disk retrieval
- [x] Context window tracking and telemetry
- [x] Foreground bash → background (`Ctrl+B`)
- [x] Async agent runs with transcript capture
- [x] Background process management (256KB circular buffer)
- [x] Interrupted-turn notice + Continue in chat (web/desktop, 2026-09-22)
- [x] Cross-client compaction indicator (manual + automatic, same server/remote-host bus, 2026-09-25; includes reconnect/server-restart generation reset)
- [x] Large-context compaction timeout policy (per-batch first-token/idle windows + 30-minute cap, 2026-09-25)
- [x] Deferred durable message rewind (web/headless + TUI `/rc` bridge, 2026-09-25)
- [x] Git conflict resolution + halted-operation recovery (local + SSH/WSL parity, 2026-09-25)
- [x] Shared web chat display settings (Full/Balanced/Quiet + category overrides, 2026-09-25)
- [x] Last-dispatched model status (backend response + turn-event fallback, 2026-09-25)
- [x] Reopen locally hidden question dialog (X/Escape vs Don't answer, 2026-09-25)
- [x] List-dialog keyboard navigation (shared real-focus hook across custom popups, 2026-09-25)
- [x] Cold-cache discovery gating + judged `discover_more` attachment (2026-09-26)
- [x] Project removal confirmation across sidebar entry points (2026-09-25)
- [x] TUI mouse-wheel scrolling from transcript over composer (2026-09-25)

### Agents
- [x] Advisor Tool
- [x] Explorer Agents

## Known Issues 🐛

- [ ] *Add known bugs, regressions, or edge cases here*

## Untested / TODO 🔄

### Provider Integrations
- [ ] Google Gemini API (full integration test)
- [ ] Z.AI API (production validation)
- [ ] Alibaba API (production validation)
- [ ] GitHub Copilot OAuth flow under edge conditions
- [ ] Multi-provider fallback chains

### Features
- [ ] Prompt caching hit rates across multiple sessions
- [ ] Thinking mode (o1/o3 models) under high context load
- [ ] Custom compaction model with different providers (e.g., summarize with Haiku while chatting with Opus)
- [ ] Permission rules with complex bash prefix combinations
- [ ] Session cloning from Claude Code (mixed provider scenarios)
- [ ] MCP remote server timeout handling
- [ ] External editor modes (tmux-split, tmux-window) on non-macOS
- [ ] Mouse selection in transcript across very long scrollback
- [ ] Undo/redo with large session histories

### Agents
- [ ] Parallel agent execution under load
- [ ] Agent timeout and cleanup after forced termination
- [ ] Subagent session isolation and cross-session state

### Integrations
- [ ] Skills system (registration, enable/disable, install/remove)
- [ ] HTTP server mode (`ocode serve`)
- [ ] Config hot-reload while TUI is running

### Edge Cases
- [ ] Empty/null tool results
- [ ] Tool results > 1MB
- [ ] Sessions with 10,000+ turns
- [ ] Rapid permission mode toggling (`Ctrl+O` spam)
- [ ] Terminal resize during active bash execution
- [ ] Switching providers mid-session
- [ ] YOLO mode with conflicting bash prefix rules
- [ ] Config merge conflicts (global vs project)

### Performance
- [ ] Compaction latency on 50KB+ context
- [ ] TUI render time with 1000+ file tree entries
- [ ] MCP client handling 100+ concurrent tool calls

## Test Running

```bash
# Unit tests
go test ./...

# Verbose
go test -v ./...

# Specific package
go test ./internal/tui -v

# Web suite — capped at 4 workers (see Testing Notes)
cd web && pnpm run test

# Override the cap when you want the whole machine
cd web && npx vitest run --maxWorkers=8

# Coverage
go test -cover ./...
```

### Testing Notes

**Parallelism is capped for the web suite, not the Go suite.** `pnpm run test`
is `vitest run --maxWorkers=4`. Vitest's default is roughly one worker per core,
which on a 10-core machine is 9 concurrent jsdom environments; 4 leaves
headroom for the editor and the rest of the machine.

**Capping the Go suite with `-p` does NOT make it green — do not reach for it
as a fix.** Measured on a 10-core machine, full `go test ./...`:

| Run | Result |
|---|---|
| `go test ./...` (`-p` defaults to 10) | 5 failures — 1 `browse/cdp`, 2 `server`, 2 `tui` |
| `go test -p 4 ./...` | 7 failures — all 7 in `server` |
| web `vitest run` (default workers) | 356 files, 0 failures, 72.41s |
| web `vitest run --maxWorkers=4` | 356 files, 0 failures, 93.98s |

Lowering `-p` did not reduce failures, it moved them. The cause is not
oversubscription between packages: it is **fixed wall-clock deadlines inside
tests that wait on async events**, and a slow machine or a busy one starves
them whichever way the package concurrency is set.

**When a full-suite failure is reported, re-run that test in isolation before
believing it.** Every Go failure in the runs above passes on its own:

```bash
go test ./internal/server/ -run 'TestResolveConflictOursThenTheirs' -count=1
go test ./internal/tui/ -run 'TestStreamStepOptsIntoFullToolOutput' -count=1
```

Treat a test as a real defect only when it fails in isolation. The full suite
takes ~9 minutes, so the isolated re-run is much cheaper than triaging from the
suite log alone.

Deadlines that produced the observed flakes, for reference when adding new
async-wait tests:

- `git status … timed out after 10s` (`internal/server`, git-conflict tests)
- 3s waits for a turn to start / a registry to fill
  (`TestCancelActiveTurn*`, `TestChildAgentAskIsVisibleAndResumable`)
- `sharedSpawnConfirmBudget = 500ms` (`internal/browse/cdp/htr.go`) — a freshly
  spawned daemon must be observed dead within this, and measured reap latency
  was 135–350ms, so the margin is thin on a loaded machine

**Prefer event-based waits over fixed sleeps when adding tests.** Most of these
failures share one shape: the test sleeps N seconds and hopes. A poll loop on
the condition being true, with a generous ceiling, fails for the real reason
when the machine is slow instead of masquerading as a logic bug.

**Known-flaky quarantine.** `.github/workflows/ci.yml` has a "Retry known-flaky
tests" step that re-runs named tests once before failing the job. When one of
those starts failing consistently, fix it and drop it from that list — a
permanently retried test is a silent hole in the gate.

**HTR tests and the `htr` build tag.** The HTR bundle is generated from
out-of-tree sources and is never committed, so it is embedded only under
`-tags htr`. A plain `go test ./...` therefore has no bundle, and any test that
needs a real HTR install fails with "this ocode binary has no HTR bundle". Use
`make prepare-htr-assets` plus `-tags htr` when you need those paths exercised.

## How to Add Tests

1. Identify untested feature/provider above
2. Move to "Tested & Working" with date and notes
3. If issues found, create a GitHub issue and link it
4. Update this file and commit

Example:
```markdown
- [x] Google Gemini API (tested 2026-06-03, fully working with custom rate limits)
```
