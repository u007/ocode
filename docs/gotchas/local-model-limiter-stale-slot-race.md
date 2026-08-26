---
type: Gotcha
title: Local Model Limiter — Stale-Slot Reclamation Race
description: 'TOCTOU race in local model slot-lock stale reclamation: reaper can delete a live lock between Stat and Remove, breaking MaxParallel limits'
tags:
  - local-model
  - limiter
  - slot-lock
  - race-condition
  - toctou
  - max-parallel
timestamp: 2026-08-26T03:26:21Z
---
## Slot-lock stale-reclamation race (TOCTOU)

**File:** `internal/agent/local_model_limiter.go` lines 113–115, 200–202

### The race

`acquireLocalModelSlot` spins on `os.Create` for an exclusive lock file. When the create fails (slot held), it checks staleness:

```go
if info, statErr := os.Stat(slotPath); statErr == nil && time.Since(info.ModTime()) > localSlotStaleAfter {
    os.Remove(slotPath)
}
```

Meanwhile the live holder's `touchLoop` refreshes mtime every 2 minutes:

```go
_ = os.Chtimes(b.slotPath, now, now)
```

Between the `Stat` (observing stale mtime) and the `Remove` (reclaiming), the holder's `touchLoop` can fire `Chtimes`, proving the lock is alive — but the reaper has already decided it's stale and deletes it. Two processes now hold the slot concurrently, breaking `MaxParallel` limits.

### Timing

- `localSlotTouchInterval` = 2 minutes (holder refresh cadence)
- `localSlotStaleAfter` = 7 minutes (3× touch + 1 min grace)

The 7-minute window is large enough that a slow CPU or GC pause can stretch the Stat→Remove gap past a touch interval, especially under load.

### Impact

- Dual slot acquisition under `MaxParallel=1` for local model servers
- Can cause mlx_lm.server or other backends to receive concurrent requests they weren't designed for
- Silent — no error, just a limit violation

### Fix direction

Ownership-safe stale detection instead of bare mtime comparison:

- **Advisory lock with owner identity:** Store PID/hostname in the lock file; reclaim only if owner is confirmed dead (e.g. `kill -0` fails).
- **Atomic conditional reclaim:** Use `os.Rename` or a compare-and-swap pattern so the reclaim only succeeds if the mtime hasn't changed since the Stat.
- **Lease-based locks:** Write a nonce at acquire, include it in touch, reclaim only if nonce is absent or mismatches.

### Source

Discovered during the LLM streaming timeout review (2026-08-27). See also `internal/agent/llm_stream_idle.go` for the stacked transport path that sits above the slot limiter.