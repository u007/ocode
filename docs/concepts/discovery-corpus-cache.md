---
type: Concept
title: Discovery Corpus Cache
description: 'BuildCorpusCached persists the per-model discovery corpus to corpus-<model>.json with a cross-instance filelock: why a machine-global corpus needs one (process-local warming cannot coordinate), lock-free fast probe, re-check+write in one critical section, ErrCorpusLocked skip with zero edits outside internal/discovery, corpusLockWaitFor deadline clamp + 1ms floor, the pidlock fixed-port asymmetry, and the five cache_lock_test.go pins.'
tags:
  - discovery
  - corpus
  - cache
  - filelock
  - architecture
  - concurrency
  - embedding
  - pidlock
timestamp: 2026-10-01T17:09:06Z
---
# Discovery Corpus Cache

## Overview

Discovery embedding vectors persist per-model to `corpus-<model>.json` under the shared discovery cache dir, so every ocode instance on the machine reuses one on-disk corpus instead of re-embedding the doc set. `BuildCorpusCached` (`internal/discovery/cache.go:171`) orchestrates it as **lock-free probe → lock → re-check → embed → write**, guarded by a cross-instance `filelock` advisory lock (`withCorpusLock`, `cache.go:123`) whose contention is reported as `ErrCorpusLocked` (`cache.go:111`) rather than worked around.

This page records the design. The [MCP tool gate](concepts/discovery-mcp-tool-gating.md) explains why a cold corpus is the normal first turn and why the gate never fails open because of it; the [mutation-check gotcha](gotchas/mutation-check-mutants-must-compile.md) covers the `filelock` non-positive-timeout trap found by mutating this code.

**Working-tree state (verified 2026-09-29; `discovery_glue.go` line anchors re-derived 2026-10-02 after later edits shifted that file).** Every line number below was checked against the current working tree, which is what matters here because the locking change is **uncommitted**: `internal/discovery/cache.go` is modified vs HEAD and `internal/discovery/cache_lock_test.go` is a new, untracked file. `internal/agent/discovery_glue.go`, `internal/discovery/pidlock.go`, and `internal/filelock/filelock.go` are untouched by the change — which is what makes the zero-edits-outside-`internal/discovery` claim below true. (Unrelated concurrent WIP exists elsewhere in `internal/agent`.)

## On-disk layout and invalidation

`LoadCache(dir, modelID, dim)` (`cache.go:42`) resolves `filepath.Join(dir, "corpus-"+sanitizeModelID(modelID)+".json")` — `sanitizeModelID` (`cache.go:35`) maps `/`, `:`, and spaces to `_`. The cache struct carries `Version`, `Model`, `Dim`, and `Items map[string][]float32` keyed by `DocHash` (`cache.go:27-33`). `DocHash` (`internal/discovery/index.go:21`) is the hex of `sha256(kind + "\x00" + name + "\x00" + text)` — **one vector per doc**, so a doc whose text changes misses (and re-embeds) naturally.

Invalidation is a mismatch, not a sweep:

- **Missing file** → fresh empty cache (`cache.go:48-50`).
- **Corrupt JSON** → fresh cache plus a `WARN` debug line (`cache.go:55-59`) — the no-silent-recovery rule means the rebuild is logged, never implicit.
- **`Version`/`Model`/`Dim` mismatch or nil `Items`** → fresh cache (`cache.go:60-62`).

`cacheFormatVersion` is **2** (`cache.go:24`) and is bumped whenever the embedding pipeline changes in a way that changes vectors *for the same model id + dim* — the doc records two such cases: the local LFM2.5 backend moving MLX → llama.cpp pooling under the same `local/lfm2.5-embedding` id, and a bge-m3 llama.cpp build bump (b9747 → b9777). The id+dim check alone cannot catch those.

The cache dir is `<GlobalDataDir>/discovery` (`discoveryCacheDir()`, `internal/agent/discovery_glue.go:261`) — deliberately the global data dir (alongside sessions/auth/usage), not `os.UserConfigDir()`, with `os.TempDir()` only as a last-resort fallback. It is shared across instances precisely because it is machine-global: two ocode processes pointed at the same projects hit the same file. To be explicit: this file is **machine-GLOBAL, not per-project** — every ocode process on the host, whatever project it has open, reads and writes this one `corpus-<model>.json`.

`Cache.Save()` (`cache.go:136`) writes atomically (temp file + `Rename`) so a concurrent reader sees the old file or the new file, never a torn one.

## Why a lock at all

Before the lock existed (verified against HEAD's `BuildCorpusCached` via `git show HEAD:internal/discovery/cache.go`), the build embedded every missing doc and `Save()`d unconditionally. Two ocode instances warming at the same time each embedded the entire corpus and the last writer won the file — the loser's embedding cost thrown away, for exactly the same vectors.

Three facts made that likely rather than theoretical:

1. **The artifact is machine-global** (previous section): one `corpus-<model>.json` per model for the whole host.
2. **The embed server is already machine-global and pid-recorded** — `internal/discovery/pidlock.go` serialises spawn of the fixed-port local embed server across all processes. But that lock covers *server startup only*; it says nothing about who is writing the corpus file.
3. **In-process coordination does not cross processes.** `Engine.Warm`'s hot-path early-return (`internal/discovery/engine.go:49-56`) keys off that process's in-memory corpus, and `a.disco.warming` (`internal/agent/discovery_glue.go:38`) is a process-local `atomic.Bool` — single-flight *per process*, invisible to peers.

So `a.disco.warming` staying process-local after the locking change is **correct by design, not an oversight**: the flock — not that flag — is what serialises cross-instance work. The flag's only job is to stop a second *in-process* background warm while one is already running.

The flock's scope is deliberately narrow: it guards **re-read → embed → Save** and nothing else. Do not hold it across anything else — embed-server spawn in particular is `pidlock.go`'s territory, a different lock with different rules (see below).

## The five phases of BuildCorpusCached

```text
probe (no lock) → withCorpusLock → re-read → embed misses → Save
```

1. **Lock-free probe** (`cache.go:172-178`): `corpusHasMisses(docs, c)` (`cache.go:203`) reads only the in-memory cache — no shared mutable state, so it is safe outside the lock. Zero misses is the **steady state**: assemble from cache (`corpusFromCache`, `cache.go:214`, touches neither lock nor disk) and return. Unconditional locking would stall a second instance on every turn for work that does not exist. Pinned by `TestBuildCorpusCachedDoesNotLockWhenFullyCached` (`cache_lock_test.go:99`).
2. **Lock** (`withCorpusLock`, `cache.go:123`): `MkdirAll` of the cache dir **first**, then `filelock.WithFileLockTimeout(cachePath+".lock", wait, fn)`. The `MkdirAll` is load-bearing: `filelock` opens with `O_CREATE`, which creates the lock *file* but not its parent *directory* — without it every first-run lock attempt fails and each instance silently re-embeds the whole corpus. Pinned by `TestCorpusLockWorksOnAFreshCacheDir` (`cache_lock_test.go:271`).
3. **Re-read under the lock** (`cache.go:185-188`): `LoadCache` again. The probe was only a hint — a peer may have persisted these vectors while we waited. The re-read matters as much as the lock: without it, the winner of the lock re-embeds what the loser already wrote.
4. **Embed misses** (`embedCorpusMisses`, `cache.go:228`): embeds only docs absent from the *fresh* cache, persists on full success.
5. **Write inside the same critical section.** Re-check and write must be **one** section (`cache.go:182-191`): split them and two instances both see "missing" and both embed. This pairing is the whole point of the lock.

**Nothing else happens under the flock.** The critical section is exactly `cache.go:182-191`. The embed call inside it may spawn the local embed server, and *that* is serialised by `pidlock.go`'s spawn lock — two different locks, never held together.

## Why concurrent instances share the lock

`withCorpusLock`'s comment (`cache.go:116-120`) states why holding a flock **across an embed call** (seconds, not microseconds) is safe: a flock is **kernel-owned**, not pid-tracked. A holder that dies releases the lock automatically — no stale lock to detect, time out, or steal. This killed an earlier `ClaimAt`/`ClaimPid`/pid-liveness apparatus (see project memory); do not reintroduce pid-based claims.

**Missing the lock is the normal contended case, not a failure** (`cache.go:70-74`): another instance is embedding this very corpus, so there is nothing to recover — the holder persists the vectors either way, and the caller's background-retry path picks them up.

**Why `pidlock.go` is not symmetric with this lock.** The flock works here because contention is harmless *and* death is self-cleaning: the holder is doing this caller's own work, so skipping is always correct, and a kernel-owned flock releases on holder death — no pid bookkeeping, no stale-lock reclaim. `internal/discovery/pidlock.go` guards a **fixed port**, where breaking or stealing a lock lets two processes race onto one port and the loser's stray-reap path can then kill the winner's innocent server (`pidlock.go:13-24`) — so it *does* record owner pids, distinguishes live / dead / garbage owners, reclaims a dead owner immediately, and never breaks a live one's lock before staleness. Different resource, opposite bookkeeping: kernel auto-release is sufficient for a file corpus and fatal for a port. Do not "unify" the two.

## ErrCorpusLocked: skip, never work around

`BuildCorpusCached` distinguishes contention from real failure with `isLockTimeout` (`cache.go:265`) — a string match on filelock's `"timeout waiting for lock"` (`internal/filelock/filelock.go:60`) — and wraps it as `ErrCorpusLocked` (`cache.go:193-195`). Any *other* lock error (e.g. the `MkdirAll` failure) is a real problem and must not be reported as mere contention. The supported check is `errors.Is`: `isCorpusLocked` (`cache.go:113-114`) is literally `errors.Is(err, ErrCorpusLocked)`, so the sentinel survives the wrap.

The contract (`cache.go:107-110`): callers treat any Warm error as "retry in the background". `runDiscovery` does exactly that — a failed synchronous warm defers to `startBackgroundWarm` (`internal/agent/discovery_glue.go:397-400`, single-flight via `discoveryState.warming`, 20s `discoveryWarmTimeout`) and the turn attaches nothing while the gate holds. **A lock we cannot take is reported, not silently bypassed.**

That mapping is deliberately generic: `runDiscovery` defers on **any** warm error, with no `errors.Is` special case — which is precisely why this change required **zero edits outside `internal/discovery`**: the lock work never edited `internal/agent/discovery_glue.go` (it now carries unrelated auto-inject edits — see `git status`), nor `internal/filelock` nor `internal/discovery/pidlock.go`. A contended turn already emits `corpus warm deferred to background: ...`, and `startBackgroundWarm` picks up the holder's persisted vectors once the lock frees. **Falling through to an unlocked build on lock failure is precisely the duplication the lock prevents** — skip plus background retry is the only correct recovery.

## Bounded wait: the 10s filelock default is a trap

`corpusLockWaitFor(ctx)` (`cache.go:94`) derives the acquire budget from the caller:

- Base budget: `corpusLockTimeout`, an `atomic.Int64` initialized to **5s** (`cache.go:79-84`). Atomic, not a plain var, because tests shorten it while a peer goroutine may still read it — a plain read/write of a shared duration is a genuine data race. Production never writes it.
- **Clamp to the caller's deadline**: the per-turn Warm runs with a **500ms context** (`discovery_glue.go:394`) and `filelock` does not observe contexts, so without the clamp a contended turn would stall the full 5s instead of the slice the caller agreed to spend.
- **Floor at 1ms** (`cache.go:101-103`): `filelock.WithFileLockTimeout` treats a **non-positive timeout as "use the 10s package default"** (`filelock.go:49-51`, `Timeout = 10 * time.Second` at `filelock.go:21`). Returning `0` for an exhausted deadline would turn a zero budget into a ten-second stall — the exact opposite of the clamp.

Pinned by `TestCorpusLockWaitNeverExceedsTheCallersDeadline` (`cache_lock_test.go:258-264`), whose expired-context assertion (`wait > 0 && wait <= 1s`) was the genuine test gap a mutation exposed. The mutation-check story — why non-compiling mutants are false-positive kills and why this floor exists — is in [Mutation-check mutants must compile](gotchas/mutation-check-mutants-must-compile.md).

## Fail-open semantics

Two distinct fail-open rules interact here; do not conflate them:

- **The cache layer fails *safe*, not open**: a lock timeout is `ErrCorpusLocked` → skip → background retry. Corrupt cache → fresh cache + logged WARN. Persist failure after a successful embed → `WARN`, corpus usable in-memory this session (`cache.go:256-258`). None of these touch the gate.
- **The gate never fails open** (`discoveryAllows`' only escape is `disco == nil || !disco.enabled`) — see [Discovery MCP Tool Gating](concepts/discovery-mcp-tool-gating.md). A corpus lock timeout is just another Warm error: it costs the turn its attachments, never disables the gate.

## Warm-gate interaction (the 500ms budget)

The per-turn path in `runDiscovery` (`discovery_glue.go:379-401`):

1. Hot cache + unchanged doc-set → `Engine.Warm` is a microsecond hash-check early-return (`engine.go:50-56`); no lock, no disk.
2. Warm runs **synchronously under a 500ms budget** (`discovery_glue.go:394`) so a hot-cache turn attaches skills in the same turn.
3. Any error — embedder slowness, `ErrCorpusLocked`, anything — defers to `startBackgroundWarm` with a generous 20s deadline, emits `corpus warm deferred to background: ...`, and the turn attaches nothing while `warming` is set (`discovery_glue.go:391-400`).
4. Ranking gets its own separate 500ms budget (`discovery_glue.go:407`); a rank failure costs the turn its attachments, **never** sets `disco.enabled = false`.

The all-or-nothing persist (Save only on full success) is why a too-tight synchronous budget could never make incremental progress on a local embedder — the background warm with its generous deadline is what breaks that deadlock (`discovery_glue.go:388-390`). On the recovery side, `discover_more` warms with `context.Background()` — no timeout at all — so the on-demand path may take as long as the embedder needs, unlike the per-turn budgets.

The `discoveryState.warming` single-flight flag in step 3 is process-local by design (see "Why a lock at all"): it gates *this* process's background warm; the flock is the cross-instance layer.

## Tests

`internal/discovery/cache_lock_test.go` (new, untracked as of 2026-09-29) — five tests, one invariant each:

- `TestBuildCorpusCachedDoesNotLockWhenFullyCached` (`:99`) — **the fully-cached build must not touch the lock at all.** Build once to populate, then `holdCorpusLock` takes `<cache>.lock` and holds it open-ended; the second `BuildCorpusCached` must return in under 2s with 0 misses and 0 embed calls. An acquisition attempt would block on the held lock for the full 5s budget and surface as `ErrCorpusLocked`, failing both the elapsed-time and error assertions.
- `TestBuildCorpusCachedSkipsWhenAnotherInstanceHoldsTheLock` (`:144`) — contention (wait shortened to 200ms) reports `ErrCorpusLocked` per `isCorpusLocked`, embeds nothing, and leaves the cache file untouched — a skipped build must not clobber the holder's results.
- `TestBuildCorpusCachedConcurrentBuildsEmbedEachDocOnce` (`:177`) — the headline guarantee, with an adversarial schedule: instance A's embedder is gated on a channel (`countingEmbedder.block`), and the test waits on `entered` so A is **provably mid-embed — and therefore holding the lock — when instance B starts**. B must skip (`isCorpusLocked`, 0 embeds). A is then released and embeds exactly `len(docs)` texts; a third instance against the now-hot cache embeds nothing and needs no lock.
- `TestCorpusLockWaitNeverExceedsTheCallersDeadline` (`:239`) — three assertions: a 250ms-deadline caller gets ≤250ms despite a 30s base budget; `context.Background()` still gets the full ≥5s; an already-expired deadline yields a wait that is `> 0` and ≤1s (`:258-264`) — the positive floor, since filelock would turn a non-positive timeout into the 10s package default.
- `TestCorpusLockWorksOnAFreshCacheDir` (`:271`) — the `MkdirAll`-before-flock invariant: the cache dir path (`.../not/created/yet`) does not exist, and the first build must still succeed rather than fail every lock attempt.

## Related

- [Discovery MCP Tool Gating](concepts/discovery-mcp-tool-gating.md) — the gate this cache feeds; cold-turn zero-tools contract.
- [Discovery TypeSafe Relevance Judge](concepts/discovery-typesafe-judge.md) — what runs after the warm succeeds.
- [Mutation-check mutants must compile](gotchas/mutation-check-mutants-must-compile.md) — the `filelock` 10s-default trap and mutation-harness rules found by mutating this code.
- `internal/agent/md_discovery.go` — the same probe → lock → re-check → write shape applied earlier to the per-project markdown-summary index (`withMDCacheLock`, `md_discovery.go:656`; lock-free probe in `mdSummarizePass`, `:238-259`), carrying the same two traps: `MkdirAll` before the flock (`:661`) and never falling through to an unlocked pass on lock failure (`:253-255`). Shared shape, separate locks — pointer only, no retelling here.