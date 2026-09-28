---
name: rust-tuning-longcat-2.5-preview-free
description: >
  Corrective Rust async knowledge for longcat-2.5-preview-free: state the
  underlying mechanism of async behavior (std ships no executor; values held
  across `.await` live in the future's state machine; scheduling is
  cooperative and yields only at `.await`), not only its effects.
when_to_use: >
  Load when the provider-stripped model id (see stack-detection.md) resolves
  to exactly `longcat-2.5-preview-free` (e.g.
  `opencode-go/longcat-2.5-preview-free` → `longcat-2.5-preview-free`) AND the
  repository is a Rust project (a `Cargo.toml` file — per meta.yaml
  detection). For any other model or non-Rust repo, do not load.
tuned_for: longcat-2.5-preview-free
tuned_version: "2.5-preview"
stack: rust
source_scorecard: ../scores/longcat-2.5-preview-free.md
threshold: 0.75
revalidate_when: model_version changes
---
# Rust corrections for longcat-2.5-preview-free

<!-- kaizen:digest -->
**Rust async — state the mechanism, not only the effect:**
1. An `async fn` returns a lazy `Future` state machine, and **the standard library ships no executor.** A third-party runtime (Tokio, async-std, smol, or `futures::executor::block_on`) is mandatory. Say "std has no executor" explicitly.
2. Every local alive across an `.await` is **stored inside the future's generated state machine**, so one `!Send` value (`Rc`, `std::sync::MutexGuard`) held across an await makes the whole future `!Send`, and `tokio::spawn` rejects it.
3. Async scheduling is **cooperative**: a task yields to the executor **only at an `.await` that returns `Pending`**. A blocking call never yields, so it holds the worker thread and starves every other task on it.
<!-- /kaizen:digest -->

## async: the standard library has no executor

- `std` defines `Future`, `Poll`, `Context` and `Waker` but nothing that polls a
  future to completion. When explaining why an `async fn` call does nothing on
  its own, say explicitly that std has no built-in executor, which is why
  `#[tokio::main]`, `block_on`, or another runtime is required.
- A future is driven either by `.await` inside another async context or, at the
  top level, by an executor polling it.

## async: values held across `.await` become part of the future

- The compiler turns an `async fn`/block into a state machine; every value that
  is still alive at an `.await` point is saved as a field of that state machine.
- So the future is `Send` only if every such held value is `Send`. An `Rc`, a
  `RefCell` borrow guard, or a `std::sync::MutexGuard` alive across an await
  makes the entire future `!Send`.
- `tokio::spawn` on the multi-threaded runtime requires `Send` because the task
  may resume on a different worker thread. Fixes: drop or scope the value so it
  ends before the `.await`, switch to `Arc` / `tokio::sync::Mutex`, or use
  `spawn_local` / a `LocalSet`.

## async: blocking breaks cooperative scheduling

- Rust async has no preemption. A task runs until it reaches an `.await` whose
  future returns `Poll::Pending`; only then does the executor regain the thread.
- A blocking call (`std::thread::sleep`, sync file/network IO, a long CPU loop,
  a contended `std::sync::Mutex`) never reaches a yield point, so the worker
  thread is held and every other task scheduled on it stalls.
- Explain in this order: mechanism (cooperative, yields only at `.await`),
  consequence (worker starvation, latency spikes, stalled timers), fix
  (`tokio::time::sleep`, async IO, `tokio::task::spawn_blocking`, or rayon for
  CPU-bound work).
