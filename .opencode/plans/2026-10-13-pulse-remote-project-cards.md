# Pulse dashboard: remote SSH project cards

**Status: IMPLEMENTED and validated 2026-10-13.** Delivered 2026-10-13;
`docs/concepts/pulse-dashboard.md` §Remote fan-out carries the shipped design.

## User Expectation Checklist

1. A remote (SSH/WSL) project's sessions appear as Pulse cards, labelled with
   their host, in the **Needs you / Running / Recent** sections. — DONE
2. They appear in **both** scopes: Live (24h idle window) and All (7d + the
   remote host's own disk catalogue, which the remote server already builds). — DONE
3. A card click **jumps to the remote session** — activates the remote project
   (by host + path, never by path alone) and opens its tab, exactly like a
   local jump. — DONE (`jumpTargetFor` carries `row.host`).
4. Live SSH events update a remote card **without polling** — turn_started,
   turn_done, turn_error, permission, permission_resolved, question,
   question_resolved, todo_updated. — DONE (`applyPulseEvent` takes host).
5. A remote host that is down must **not** blank or stall the dashboard:
   local cards stay, last-known remote cards stay, the failure is visible. — DONE
   (`allSettled` + `hostErrors` + `role="status"` notice).
6. The local dashboard is unchanged when there are no open remote tabs. — DONE
7. Every remote row keeps the server's ordering rules (rank → updated_at →
   session id) once merged with local rows. — DONE.

## Root cause

`GET /api/pulse` is local-only by design (`docs/concepts/pulse-dashboard.md`
§Purpose: "v1 is local-server only"):

- `gatherLivePulseInputs` reads the in-process registry. Remote chat runs on
  the HOST's `ocode serve --remote`, so nothing is registered locally.
- `gatherDiskPulseInputs` skips `p.Host != ""` (`handler_pulse.go:227`).
- The web store then compounds it: `pulseEventSink` returns early on every
  host-tagged SSE frame (`pulseStore.tsx:338`).

Deferred, not forgotten — `TODO.md:1338-1347` ("Remote-host fan-out") and
`TODO.md:4117-4126` naming the two missing pieces: `host` on `PulseRow` and a
per-host fetch in the client.

## Approved design (user decisions, 2026-10-13)

- **Fan-out in the client**, through the existing `/api/remote/{host}`
  reverse proxy. Latency is not coupled across hosts and one dead host cannot
  delay or empty the dashboard.
- **Which hosts**: only hosts the SSE stream already relays, i.e. hosts with at
  least one open tab in this window (`eventBus`'s `hosts` set, declared by
  `App.tsx`). This is the cold-connect-safe choice — dialing an unsaved or
  disconnected host is the 2026-09-18 "sidebar hover hangs the app" class.
- **Scope**: both Live and All. The remote server already answers both, so the
  toolbar keeps the same meaning on remote rows.

## Design decisions

| # | Decision | Why |
|---|---|---|
| D1 | No `host` field on the server's `PulseRow` | A locally-resident session never has a remote root: remote chat is proxied and never reaches the local handler (`handler.go:1080`, `agent_session.go:416`), so the field would always be `""`. The client knows which proxy path it fetched and stamps it. |
| D2 | Row identity becomes `(session_id, host)` | Two hosts can mint the same session id in principle; `applyPulseEvent` already indexes by id alone, and accepting remote events without the host would let one host's event patch another's row. |
| D3 | Per-host cursors (`hostCursors`), `nextCursor` kept as the local source's | `Promise.allSettled` fan-out gives one cursor per source. `hasMore` becomes "any source has another page". |
| D4 | Remote row `project_path` is resolved to the locally-registered path | The remote server reports its own expanded root (`/home/james/www/aimsai2`) while this server registered it verbatim (`~/www/aimsai2`). A jump keyed on the host's form matches no saved project and is refused. Pure suffix resolve, and an unresolvable row simply keeps the host's path (degrades to today's behaviour). |
| D5 | Partial failure keeps last-known rows for that host | Matches the store's existing "never replace good rows with nothing" invariant; a remote host's outage must not empty the board. |

## Files

| Path | Change |
|---|---|
| `web/src/api/client.ts` | `getPulse(scope, cursor, limit, host?)` → `remoteApiBase(host)` prefix |
| `web/src/api/types.ts` | `PulseRow.host: string` |
| `web/src/lib/pulseRemoteRows.ts` | NEW: `stampPulseHost`, `resolvePulseProjectPath`, `pulseProjectKey` |
| `web/src/lib/eventBus.ts` | `currentHosts()` + `onHostsChange()` |
| `web/src/stores/pulseStore.tsx` | fan-out fetch, per-host cursors, host-aware sink |
| `web/src/components/Pulse/PulseCard.tsx` | host label + host in the jump target |
| `web/src/components/Pulse/PulseFocusPane.tsx` | host label + host in the jump target |
| `web/src/components/Pulse/pulseFilter.ts` | match the host string too |
| `web/src/components/Pulse/PulseView.tsx` | partial-failure notice |

## Explicitly out of scope (recorded, not silently dropped)

- `GET /api/pulse/terminals` — remote terminals are a separate gap
  (`docs/concepts/pulse-dashboard.md` §Known limits names it).
- Pulse assistant tools over remote projects (`pulse-assistant.md` §Known
  limits).
- Forcing a connect for a host with no open tab (the approved host set).

## Validation

- New unit tests: `pulseRemoteRows.test.ts`; `pulseStore.test.tsx` fan-out,
  host-aware sink, per-host paging, partial failure; `pulseFilter.test.ts`
  host match; `PulseCard.test.tsx` host label + jump target.
- `pulseStore.test.tsx` existing arity assertions updated to the 4th arg.
- Existing suites: `web/src/stores/pulseStore.test.tsx`,
  `web/src/components/Pulse/*`, `App.*`.
- `npm run typecheck` and `vite build`.
- No Go change, so no `go test ./...` gate for this feature.

## Shipped notes (2026-10-13)

Two things the implementation added that were not in the original design:

- **A saved-project cache** (`loadSavedProjects`, `GET /api/projects`) exists only
  to reconcile a remote host's expanded project path with the form this server
  registered. It is fetched only when a remote source exists, reset on
  reconnect, and exposes `__resetPulseSavedProjectsForTests` so a test cannot
  silently share another test's cache.
- **16 `App.*` test doubles gained the two new bus methods** (`currentHosts`,
  `onHostsChange`). The store calls them through the bus, so a partial mock
  crashes the provider rather than degrading — every eventBus double had to be
  complete. Mechanical insertion only, no assertion changed.

Pre-existing failure, NOT from this diff: `SessionDialog.test.tsx` (2 tests). Its
`vi.mock("../../stores/projectStore")` omits `findProjectPathForTab`, which
`useSessionHost.ts` calls. `projectStore.tsx`, `useSessionHost.ts` and
`SessionDialog.test.tsx` are unmodified and import nothing this feature touched.
