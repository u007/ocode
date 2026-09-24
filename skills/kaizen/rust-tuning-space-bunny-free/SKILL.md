---
name: rust-tuning-space-bunny-free
description: >
  Corrective Rust async knowledge for space-bunny-free, targeting the one gap
  this model showed on the closed-book rust benchmark: it explains the
  effects of async (a runtime is needed; a blocking call stalls the worker)
  without stating the underlying mechanism (std ships no executor; scheduling
  is cooperative and a task yields only at `.await`).
when_to_use: >
  Load when the provider-stripped model id (see stack-detection.md) resolves
  to exactly `space-bunny-free` (e.g. `opencode-go/space-bunny-free` →
  `space-bunny-free`) AND the repository is a Rust project (a `Cargo.toml`
  file — per meta.yaml detection). For any other model or non-Rust repo, do
  not load.
tuned_for: space-bunny-free
tuned_version: "alpha"
stack: rust
source_scorecard: ../scores/space-bunny-free.md
threshold: 0.75
revalidate_when: model_version changes
---
# Rust corrections for space-bunny-free

<!-- kaizen:digest -->
**Rust async — state the mechanism, not only the effect:**
1. An `async fn` returns a lazy `Future` state machine, and **the standard library ships no executor.** `std` defines `Future`, `Poll` and `Waker` but nothing that polls a future to completion, so a third-party runtime (Tokio, async-std, smol, or `futures::executor::block_on`) is mandatory, not merely conventional. Say "std has no executor" explicitly whenever explaining why a runtime is needed.
2. Async scheduling is **cooperative**: a task hands control back to the executor **only at an `.await` that returns `Pending`**. A blocking call (`std::thread::sleep`, sync IO, a CPU loop, a contended `std::sync::Mutex`) never reaches a yield point, so the worker thread is held and every other task on it starves. Lead with "cooperative, yields only at `.await`"; worker starvation and `spawn_blocking` are the consequence and the fix, not the explanation.
<!-- /kaizen:digest -->

## async: the standard library has no executor

- When explaining what calling an `async fn` does, do not stop at "it returns
  a future that runs when polled by an executor or async runtime." State why a
  runtime is required at all: **`std` provides the `Future` trait, `Poll` and
  `Waker`, but no executor.** Nothing in the standard library will ever poll a
  future to completion, so async code cannot run without Tokio, async-std,
  smol, or at minimum `futures::executor::block_on`.
- Mentioning "`block_on` or an async runtime" is not the same as stating that
  the standard library lacks one. Make the absence explicit: "std has no
  built-in executor, which is why `#[tokio::main]` / `block_on` is mandatory."

## async: blocking is a cooperative-scheduling failure, not just a stalled worker

- Rust async is **cooperative**. A task runs until it hits an `.await` whose
  future returns `Poll::Pending`; only then does control return to the
  executor so another task can run. There is no preemption.
- Therefore a blocking call inside an async task (`std::thread::sleep`,
  synchronous file or network IO, a long CPU loop, a contended
  `std::sync::Mutex`) **never reaches a yield point**. The executor cannot take
  the thread back, so every other task scheduled on that worker is stalled
  until the blocking call returns.
- When asked why blocking in async is harmful, answer in this order: the
  mechanism ("cooperative, tasks yield only at `.await`, blocking never
  yields"), then the consequence (worker starvation, latency spikes, stalled
  timers), then the fix (`tokio::time::sleep`, async IO,
  `tokio::task::spawn_blocking`, or rayon for CPU work). Giving only "the
  worker cannot poll other tasks" plus the fix leaves the explanation
  incomplete.
