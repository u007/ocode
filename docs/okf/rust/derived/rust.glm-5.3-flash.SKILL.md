---
name: rust-tuning-glm-5.3-flash
description: >
  Corrective Rust async knowledge for glm-5.3-flash, targeting the one gap
  this model showed on the closed-book rust benchmark: it describes the
  effects of async (futures need a runtime; blocking starves tasks) without
  stating the underlying mechanism (std ships no executor; scheduling is
  cooperative and yields only at `.await`).
when_to_use: >
  Load when the provider-stripped model id (see stack-detection.md) resolves
  to exactly `glm-5.3-flash` (e.g. `aihubmix/glm-5.3-flash` →
  `glm-5.3-flash`) AND the repository is a Rust project (a `Cargo.toml`
  file — per meta.yaml detection). For any other model or non-Rust repo, do
  not load.
tuned_for: glm-5.3-flash
tuned_version: "5.3"
stack: rust
source_scorecard: ../scores/glm-5.3-flash.md
threshold: 0.75
revalidate_when: model_version changes
---

# Rust corrections for glm-5.3-flash

glm-5.3-flash is strong across the stack. One area falls below threshold:
**async**. The sections below target only the specific mistakes it made
there — nothing else is restated here.

<!-- kaizen:digest -->
**Rust async — always name the mechanism, not just the effect:**
1. `async fn` returns a lazy `Future` state machine; **the standard library ships no executor.** `std` defines `Future`/`Poll`/`Waker` but nothing that drives them, so a third-party runtime (Tokio, async-std, smol, `futures::executor::block_on`) is mandatory, not merely conventional. Say "std has no executor" explicitly whenever explaining why a runtime is needed.
2. Async scheduling is **cooperative**: a task yields control back to the executor **only at an `.await` that returns `Pending`**. A blocking call (`std::thread::sleep`, sync IO, a CPU loop, `std::sync::Mutex` under contention) never reaches a yield point, so the worker thread is held and every other task on it starves. Lead with "cooperative / yields only at `.await`" — worker-thread starvation and `spawn_blocking` are the consequence and the fix, not the explanation.
<!-- /kaizen:digest -->

## async: the standard library has no executor

- When explaining what calling an `async fn` does, do not stop at "it returns
  a lazy future that must be polled by a runtime like Tokio." State the
  reason a runtime is required at all: **`std` provides the `Future` trait,
  `Poll`, and `Waker`, but no executor.** Nothing in the standard library
  will ever poll a future to completion, so an async program cannot run
  without pulling in Tokio, async-std, smol, or at minimum
  `futures::executor::block_on`.
- Listing runtimes by name is not the same as stating that the standard
  library lacks one. Make the absence explicit: "std has no built-in
  executor — that is why `#[tokio::main]` / `block_on` is mandatory."

## async: blocking is a cooperative-scheduling failure, not just a slow call

- Rust async is **cooperative**. A task runs until it hits an `.await` whose
  future returns `Poll::Pending`; only then does control return to the
  executor so another task can run. There is no preemption.
- Therefore a blocking call inside an async task — `std::thread::sleep`,
  synchronous file/network IO, a long CPU loop, a contended
  `std::sync::Mutex` — **never reaches a yield point**. The executor cannot
  take the thread back; every other task scheduled on that worker is stalled
  until the blocking call returns.
- When asked why blocking in async is harmful, lead with the mechanism
  ("cooperative — tasks yield only at `.await`, blocking never yields"),
  then the consequence (worker starvation, latency spikes, stalled
  timers), then the fix (`tokio::time::sleep`, async IO,
  `tokio::task::spawn_blocking` / rayon for CPU work). Giving only the
  consequence and the fix leaves the explanation incomplete.
