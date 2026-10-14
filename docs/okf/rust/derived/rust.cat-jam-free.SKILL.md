---
name: rust-tuning-cat-jam-free
description: >
  Corrective Rust ownership and async knowledge for cat-jam-free: when
  explaining moves, Copy/Clone, why an async fn needs a runtime, why a future
  fails a Send bound, or why blocking in async is harmful, state the
  underlying mechanism, not only the observable effect.
when_to_use: >
  Load when the provider-stripped model id (see stack-detection.md) resolves
  to exactly `cat-jam-free` (e.g. `opencode-go/cat-jam-free` →
  `cat-jam-free`) AND the repository is a Rust project (a `Cargo.toml`
  file — per meta.yaml detection). For any other model or non-Rust repo, do
  not load.
tuned_for: cat-jam-free
tuned_version: "alpha"
stack: rust
source_scorecard: ../scores/cat-jam-free.md
threshold: 0.85
revalidate_when: model_version changes
---
# Rust corrections for cat-jam-free

<!-- kaizen:digest -->
**Rust — always give the mechanism, not just the effect:**
1. **Moves exist to keep exactly one owner.** A non-`Copy` value has a single owner that frees it on drop; moving instead of shallow-copying is what prevents a double-free without a GC. Saying "`a` is invalid, `b` owns it now" is not enough: say why.
2. **`Copy` requires `Clone`** (`trait Copy: Clone`). `Copy` = implicit bitwise duplicate, no move, both bindings stay usable; `Clone` = explicit, possibly deep. A `Drop` type cannot be `Copy` because implicit copies would run the destructor twice.
3. **The standard library ships no executor.** `std` defines `Future`/`Poll`/`Waker` but nothing that polls a future to completion, so a third-party runtime (Tokio, async-std, smol, `futures::executor::block_on`) is mandatory. Say "std has no executor" explicitly.
4. **Every value alive across an `.await` is stored in the future's generated state machine.** So one `!Send` value (`Rc`, `std::sync::MutexGuard`) held across an await makes the whole future `!Send`, and `tokio::spawn` (which requires `Send`) rejects it.
5. **Async scheduling is cooperative: a task yields only at an `.await` that returns `Pending`.** Blocking code never reaches a yield point, so it holds the worker thread and starves other tasks. Lead with this; starvation and `spawn_blocking` are the consequence and the fix.
<!-- /kaizen:digest -->

## ownership: give the reason for moves

- When explaining `let b = a;` for a `String`/`Vec`/`Box`, state three things:
  ownership moves to `b`; `a` is invalidated (using it is a compile error);
  this keeps exactly one owner responsible for freeing the heap buffer on drop,
  which is how Rust prevents double-free without a garbage collector.
  `a.clone()` is the explicit way to get two owners.
- When comparing `Copy` and `Clone`: `Copy` is an implicit bitwise duplicate on
  assignment/pass (no move, the source stays usable); `Clone` is an explicit,
  possibly expensive `.clone()`. State that `Copy` is a subtrait of `Clone`, so
  every `Copy` type must also implement `Clone`. A `Drop` type cannot be `Copy`
  because implicit copies would each run the destructor on the same resource.

## async: the standard library has no executor

- Calling an `async fn` returns a lazy `Future` (a compiler-generated state
  machine) and runs nothing. It is driven by `.await` inside another async
  context or, at the top level, by an executor polling it.
- State explicitly that `std` provides no executor, which is why a runtime
  (`#[tokio::main]`, `Runtime::block_on`, async-std, smol,
  `futures::executor::block_on`) is required. Listing runtimes as options is
  not the same as saying std has none.

## async: values held across `.await` become part of the future

- Every local still alive at an `.await` point is saved as a field of the
  future's state machine so it can be restored on the next poll.
- The future is therefore `Send` only if every such held value is `Send`. An
  `Rc`, a `RefCell` borrow guard, or a `std::sync::MutexGuard` alive across an
  await makes the entire future `!Send`.
- Explain in this order: capture (held value becomes part of the future),
  consequence (future is `!Send`), bound (`tokio::spawn` requires `Send`
  because the task may resume on another worker thread), fix (drop or scope
  the value before the `.await`, use `Arc` / `tokio::sync::Mutex`, or
  `spawn_local` on a `LocalSet`).

## async: blocking breaks cooperative scheduling

- Async runtimes do not preempt tasks. A task gives control back to the
  executor only when an `.await` returns `Poll::Pending`.
- `std::thread::sleep`, synchronous I/O, a blocking lock or a long CPU loop
  never reaches such a point, so it holds the worker thread and every other
  task on it starves (missed timers, latency spikes, possible deadlock).
- Fix: async equivalents (`tokio::time::sleep`, async I/O), or offload to
  `tokio::task::spawn_blocking`, a dedicated thread, or Rayon for CPU work.
