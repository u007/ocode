---
name: rust-tuning-nvidia-nemotron-3.5-super-vl
description: >
  Corrective Rust async knowledge for nvidia/nemotron-3.5-super-vl: when
  explaining why an `async fn` does nothing on its own, why a blocking call
  inside a task is harmful, or why a future fails `tokio::spawn`, state the
  underlying mechanism (std ships no executor; tasks are cooperative and yield
  only at `.await`; the failure is the `Send` bound, not `'static`), not only
  the observable effect.
when_to_use: >
  Load when the provider-stripped model id (see stack-detection.md) resolves
  to exactly `nvidia/nemotron-3.5-super-vl` (e.g.
  `openrouter/nvidia/nemotron-3.5-super-vl` → `nvidia/nemotron-3.5-super-vl`)
  AND the repository is a Rust project (a `Cargo.toml` file — per meta.yaml
  detection). For any other model or non-Rust repo, do not load.
tuned_for: nvidia/nemotron-3.5-super-vl
tuned_version: "3.5"
stack: rust
source_scorecard: ../scores/nvidia__nemotron-3.5-super-vl.md
threshold: 0.85
revalidate_when: model_version changes
---
# Rust corrections for nvidia/nemotron-3.5-super-vl

<!-- kaizen:digest -->
**Rust async — state the mechanism, not only the effect:**
1. **The standard library ships no executor.** `std` defines `Future`, `Poll`, `Context` and `Waker` but nothing that polls a future to completion. Naming Tokio or "an executor" is not enough: say explicitly that std has no runtime, which is why Tokio, async-std, smol or `futures::executor::block_on` is mandatory to run a top-level future.
2. **Async tasks are cooperatively scheduled: a task yields only at an `.await` that returns `Pending`.** A blocking call (`std::thread::sleep`, heavy CPU work, sync I/O) never reaches a yield point, so it monopolizes the worker thread and starves every other task on it. Lead with this cause, then the fix: the async equivalent (`tokio::time::sleep(..).await`) or `tokio::task::spawn_blocking` for CPU/blocking work.
3. **An `Rc` or `std::sync::MutexGuard` held across `.await` breaks `tokio::spawn` because the future becomes `!Send`, not because it is not `'static`.** `Rc<T>` is `'static` whenever `T: 'static`. Every value alive across an await is a field of the generated state machine; one `!Send` field makes the whole future `!Send`, and the multi-threaded runtime requires `Send` because the task may resume on another worker. Fix: drop or scope the value before the `.await`, switch to `Arc` / `tokio::sync::Mutex`, or use `spawn_local` on a `LocalSet`.
<!-- /kaizen:digest -->

## async: the standard library has no executor

- When asked what calling an `async fn` returns and what makes it run, do not
  stop at "a lazy state machine that must be polled" plus "hand it to a runtime
  like Tokio". State the missing piece explicitly: `std` has no built-in
  executor, so a third-party runtime (`#[tokio::main]`, `Runtime::block_on`,
  async-std, smol, `futures::executor::block_on`) is required to drive a
  top-level future.
- A future is driven either by `.await` inside another async context or, at the
  top level, by an executor polling it; both ultimately rest on an executor
  that std does not provide.

## async: blocking is harmful because scheduling is cooperative

- Name the mechanism before the symptom. Async runtimes do not preempt: a task
  gives control back to the executor only when an `.await` returns
  `Poll::Pending`. A synchronous blocking call contains no such point, so the
  worker thread is held until it returns.
- Then the consequence: every other task scheduled on that worker is starved;
  on a small or single-threaded runtime the whole program stalls.
- Then the fix, in two halves: use the async equivalent for waits and I/O
  (`tokio::time::sleep`, `tokio::fs`, async clients), and move CPU-bound or
  unavoidably blocking work to `tokio::task::spawn_blocking` (or a dedicated
  thread) and `.await` its handle.

## async: a `!Send` value across `.await` fails the `Send` bound, not `'static`

- The compiler lowers an `async fn`/block into a state machine; every value
  still alive at an `.await` point is stored as a field of that state machine
  so it can be restored when the future is polled again.
- Therefore the future is `Send` only if every such held value is `Send`. An
  `Rc`, a `RefCell` borrow guard, or a `std::sync::MutexGuard` alive across an
  await makes the entire future `!Send`.
- Do not attribute the error to `'static`. `tokio::spawn` requires
  `F: Future + Send + 'static`, and an `Rc<String>` or `Rc<Vec<u8>>` satisfies
  `'static` (it owns its data). Diagnose the real bound that fails: `Send`.
  `'static` fails only when the future borrows a local by reference.
- Explain in this order: capture (held value becomes a field of the future),
  consequence (future is `!Send`), bound (`spawn` requires `Send` because the
  task may resume on a different worker thread), fix (drop or scope the value
  before the `.await`, switch to `Arc` / `tokio::sync::Mutex`, or use
  `spawn_local` on a `LocalSet`).
