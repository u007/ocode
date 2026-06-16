# Part 01 — Core Bus: log, seq, goroutine owner, watermarks, snapshots, persistence

**Design:** `docs/superpowers/specs/2026-06-16-shared-agent-notes-bus-design.md`

**Context:** This part builds the standalone, agent-agnostic `notebus` primitive: an
append-only ordered log owned by one goroutine, with channel-serialized appends,
monotonic sequence numbers, per-agent read watermarks, immutable snapshot reads, and
append-only sidecar persistence with reload + seq recovery. No agent wiring yet — that
is Part 02. Keep this package free of `internal/agent` imports so it stays unit-testable
in isolation.

---

### Task 1: Bus types and entry model

**Files:**
- Create: `internal/notebus/notebus.go`
- Test: `internal/notebus/notebus_test.go`

- [ ] **Step 1: Write the failing test**
  Assert the entry model and constructors exist:
  - An `Entry` carries: `Seq` (int64), `By` (string), `Kind` (note | touch | resolve),
    `At` (anchor, optional), `File`/`Act` (touch only), `Ref` (resolve only),
    `Body` (note only), `TS` (unix, injected — not read from a wall clock inside the
    package).
  - A `Bus` value can be constructed with a group id and an initial empty log.
  - `Kind` is an enum with the three values and rejects unknown kinds.

- [ ] **Step 2: Run focused tests and confirm they fail**
  `go test ./internal/notebus -run TestEntryModel -v` — fails (package/types absent).

- [ ] **Step 3: Implement the minimal types**
  Define `Entry`, `Kind`, and a `Bus` struct holding the ordered `[]Entry`, a seq
  counter, and a `map[string]int64` of per-agent watermarks. No goroutine yet.

- [ ] **Step 4: Verify** — focused test passes.

---

### Task 2: Single-owner goroutine + channel-serialized append

**Files:**
- Modify: `internal/notebus/notebus.go`
- Test: `internal/notebus/notebus_test.go`

- [ ] **Step 1: Write the failing test**
  - `Start(ctx)` launches the owner goroutine; `Append(entry)` sends on a channel and
    returns the assigned `Seq`; seqs are strictly monotonic starting at 1.
  - Concurrent `Append` from N goroutines yields N unique, gapless seqs (run with
    `-race`).
  - `Append` after `ctx` cancellation returns a clear error, does not panic, does not
    block forever.
  - The critical section never blocks: a slow persister (injected) must not stall
    `Append`'s seq assignment beyond the in-memory append.

- [ ] **Step 2: Run and confirm failure**
  `go test ./internal/notebus -run TestAppendOrdering -race -v`.

- [ ] **Step 3: Implement**
  Owner goroutine reads an append channel; for each request assigns the next seq,
  appends in-memory, then hands the stamped entry to a persistence sink (interface,
  defaulted to a no-op for this task). Bound the channel; on a full channel block
  briefly (appends are microsecond-cheap) but never hold across IO. No `sync.Mutex`
  on the log — the channel is the serialization point.

- [ ] **Step 4: Verify** — race test passes.

---

### Task 3: Snapshot reads + per-agent delta + watermark advance

**Files:**
- Modify: `internal/notebus/notebus.go`
- Test: `internal/notebus/notebus_test.go`

- [ ] **Step 1: Write the failing test**
  - `Snapshot()` returns an immutable copy that does not observe later appends and never
    races a concurrent `Append` (`-race`).
  - `Delta(agent)` returns entries where `seq > lastSeen[agent] AND by != agent`, in seq
    order, and **advances `lastSeen[agent]` to the current head** (so the agent's own
    entries are skipped from injection but never re-evaluated as new).
  - Two successive `Delta(agent)` calls with no intervening append return the first the
    new entries, the second empty.
  - `Delta` for an agent that authored every new entry returns empty but still advances
    the watermark.

- [ ] **Step 2: Run and confirm failure**
  `go test ./internal/notebus -run TestDelta -race -v`.

- [ ] **Step 3: Implement**
  Route `Snapshot`/`Delta` through the owner goroutine (request/response channel) or a
  copy-on-read guarded slice so reads never touch mutable state held by a writer.

- [ ] **Step 4: Verify** — passes under `-race`.

---

### Task 4: Append-only sidecar persistence + reload + seq recovery

**Files:**
- Create: `internal/notebus/persist.go`
- Test: `internal/notebus/persist_test.go`

- [ ] **Step 1: Write the failing test**
  - Appending entries writes one JSON object per line to `<dir>/<group>.notes.jsonl`,
    in seq order, append-only (existing lines never rewritten).
  - `Load(dir, group)` reconstructs the in-memory log, the per-agent watermarks, and
    sets the seq counter to `max(seq)+1` so a resumed bus never reissues a seq.
  - A truncated/garbled trailing line is tolerated (recover up to the last valid line)
    and logged via the standard `log` package — never silently dropped, never a panic.
  - Persistence does not write to the main session blob (assert the session file path is
    untouched).

- [ ] **Step 2: Run and confirm failure**
  `go test ./internal/notebus -run 'TestPersist|TestReload' -v`.

- [ ] **Step 3: Implement**
  Implement the persistence sink from Task 2 as an append-only file writer (buffered
  flush acceptable; flush on group teardown). Implement `Load`. Recover seq from the max
  seen line.

- [ ] **Step 4: Verify** — persist + reload tests pass; manual check that the sidecar is a
  separate file from the session transcript.

---

## Part 01 Done When

- `internal/notebus` builds with no `internal/agent` import.
- All Task 1–4 tests pass under `-race`.
- Monotonic gapless seqs under concurrency; deltas correct and watermark-advancing;
  sidecar round-trips with seq recovery; the persister never blocks seq assignment.
