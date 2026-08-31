---
type: Decision
title: V1 Connection Cap Exclusion — Embedded Browser Panel
description: 'Decision to exclude the per-stateKey concurrent upstream connection cap (32) from v1 embedded browser panel, with follow-up note for semaphore around handleExternal/handleLocal. Updated 2026-08-31 to reflect scaffold landing.'
tags: [architecture, browse, decision, v1, external-mode]
timestamp: 2026-08-31T05:41:38Z
---
## Decision

The per-stateKey concurrent upstream connection cap (32, spec § External mode limits) is **excluded from v1 acceptance** of the embedded browser panel.

## Context

The original design spec (`docs/superpowers/specs/2026-08-30-embedded-browser-panel-design.md`) defines "External mode limits" that include a **per-stateKey concurrent upstream connection cap of 32**. During implementation, no part file in the embedded-browser-panel plan was assigned ownership of this cap — Part 03 shipped the external fetch path with transport idle-pool only, and Part 06 (local mode) is complete without it.

The TODO.md entry (2026-08-31) confirms:

> Embedded browser panel: spec § External mode limits include a "per-stateKey concurrent upstream connection cap 32". It is not assigned to any plan part. Part 03 ships the external fetch path without it (transport idle-pool only). Needs an owner (likely Part 06 local mode or a follow-up) before the panel ships.

The v1 non-goals list (`TODO.md:1789–1792`) explicitly marks this:

> Per-stateKey concurrent upstream connection cap (32, spec § External mode limits): explicitly EXCLUDED from v1 acceptance — no plan part owned it. Needs a follow-up that adds a semaphore around handleExternal/handleLocal upstream work.

## Rationale

- The cap was a spec requirement, but no plan part was assigned to implement it.
- Part 03 (external fetch) and Part 06 (local mode) shipped without it — the transport's native idle-pool is the only concurrency control today.
- Adding a semaphore is a follow-up task, not a blocker for v1.

## Follow-up

A future change should add a **semaphore-based concurrency cap** around `handleExternal`/`handleLocal` upstream work, keyed per-stateKey, to bound concurrent connections. The natural owner is a follow-up after the v1 browser panel ships, or the local-mode part if revisited.

### Status (2026-08-31)

The follow-up is **done** as of 2026-08-31. The per-stateKey concurrent upstream connection cap (32) is fully implemented and wired:

- **`connLimiter`** (`internal/browse/connlimit.go`): per-stateKey counting semaphore shared by external + local traffic. `acquire()` blocks up to 5 s (`upstreamSlotWait`) before returning `errUpstreamBusy`; entry cleanup at `refs==0` (no TTL sweep needed — closed tabs leave nothing behind). `Server.failBusy` responds with 503 + `Retry-After: 1` and closes the loading nav event for document requests (Part 07 contract).
- **Instantiation** (`internal/browse/server.go:61`): `s.conns = newConnLimiter(maxUpstreamConnsPerKey)` in `New()`.
- **External mode** (`internal/browse/external.go:48–54`): `handleExternal` calls `s.conns.acquire(r.Context(), t.StateKey)`, routes `errUpstreamBusy` to `s.failBusy(w, r, t, "proxied")`, and defers `release()`.
- **Local mode** (`internal/browse/local.go:110–116`): `handleLocal` calls `s.conns.acquire(r.Context(), t.StateKey)`, routes errors to `s.failBusy(w, r, t, "local")`, and defers `release()`. The slot is held for the full connection lifetime, including hijacked WebSocket tunnels.
- **Tests** (`internal/browse/connlimit_test.go`): waiter-wakes-on-release, cancellation/timeout no-ref-leak, per-stateKey independence.

The historical decision to exclude the cap from v1 acceptance remains accurate — the cap was indeed absent at v1 ship time. This Status section records that the prescribed follow-up has been completed.

## References

- `TODO.md` lines 1771–1792
- `docs/superpowers/specs/2026-08-30-embedded-browser-panel-design.md` (spec § External mode limits)
- `docs/superpowers/plans/2026-08-30-embedded-browser-panel/03-external-fetch-headers-cookiejar.md` (Part 03 — shipped without cap)
- `docs/superpowers/plans/2026-08-30-embedded-browser-panel/06-local-mode-ws.md` (Part 06 — shipped without cap)
- `internal/browse/connlimit.go` (per-stateKey semaphore — now wired into both handlers)
- `internal/browse/connlimit_test.go` (test coverage)
- `internal/browse/external.go` (handleExternal acquire/release)
- `internal/browse/local.go` (handleLocal acquire/release)
