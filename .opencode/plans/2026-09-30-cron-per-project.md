# Per-project Cron (jobs + reminders + tasks)

The Cron surface is pinned to the **server's boot directory** (`s.workDir`), not the
selected project, so switching projects in the sidebar shows the same jobs, outbox,
targets, reminders and tasks. Make all of it project-scoped.

## Current state (verified by reading the code)

- The STORES are already per-project-by-slug: `scheduler.DefaultStorePath(workDir)` →
  `<GlobalDataDir>/scheduler/<base>-<fnv12hex>/jobs.json`, and `reminders.DefaultStorePath`
  puts `reminders.json` in that same dir. Only the SERVICE INSTANCE is process-wide.
- ONE service is started: `main.go:schedulerSetup()` / `internal/desktop/scheduler.go:AttachScheduler`
  from `os.Getwd()` / the desktop boot dir, plus my `AttachReminders(workDir, …)`.
- No cron route reads a project param. Frontend: `client.ts:1624-1668` sends none either,
  yet `App.tsx:313` keys the panel per project — so the key resets while the data does not.

## Patterns to mirror (already in the repo)

1. Per-project cache: `handler.go:334` `lspManagerFor` — `map[canonicalRoot]T` under a
   mutex, `root == ""` → `h.workDir` → `"."`.
2. Trust boundary: `handler_terminal.go:707` `resolveTerminalHistoryProject` —
   `ExpandHome` (local only), then EXACT membership in `h.allowedProjectRoots()` else 403.
3. Query helper: `client.ts:472` `projQuery(project, host)`.
4. Shutdown hook: `server.go:1636` `Server.Shutdown` already stops tts/tsShare.
5. Idle eviction shape: `SessionManager.EvictIdle` (`defaultSessionIdleTimeout` = 30 min).

## Decisions (advisor was disabled; these are mine, with reasoning)

- **Eager warm for every saved LOCAL project, lazy otherwise.** A reminder in a project
  whose tab nobody ever opened would silently never fire, and it would never reach the
  Telegram drainer. A reminder system that only works once you look at it is not one.
  Cost is one idle goroutine loop per project. Remote projects are skipped here on
  purpose: their traffic is reverse-proxied to the host, which warms its own list.
- **Per-project entry with its own mutex, not one global start-lock.** `sync.Once` would
  make a FAILED start permanently sticky, so the entry is removed from the map on error
  and the next request retries. Holding the registry lock only for map access means a
  slow start for project A does not block project B.
- **Idle-evict AND stop-all on shutdown.** Each cached service owns a run loop plus a
  drainer goroutine; without eviction a long-lived server accumulates one pair per project
  ever opened. `Service.Stop()` blocks on `<-s.done` but the loop exits promptly, so it is
  safe from a background sweeper. Eviction is lossless: the store is on disk and reloads.
- **The LLM `cron` tool stays default-project-only.** Threading a per-session scheduler
  into `buildAgentSession` is a much larger change. Deferred to TODO.md, documented.
- **No extra server-side `host` handling.** A remote project's request is proxied to the
  host's own server, which validates against ITS roots. Only the client threads `host`.

## Files

- `internal/server/cron_scope.go` (new) — the resolver + the two registries + warm +
  evict + stop-all.
- `internal/server/scheduler.go` — handlers resolve the project and use its services;
  `Shutdown` calls `stopAllCronServices`.
- `internal/server/reminders.go` / `reminders_host.go` — same, per project.
- `internal/server/server.go` — new fields; nothing else changes.
- `web/src/api/client.ts` — `project`/`host` on all 14 cron+reminder methods.
- `web/src/components/Cron/*` — thread `project`/`host` down from `App.tsx`.
- Tests: `internal/server/cron_scope_test.go` (isolation, 403, default, no-param).
