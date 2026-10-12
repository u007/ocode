---
name: rust-tuning-step-5-preview-free
description: >
  Corrective Rust async knowledge for step-5-preview-free: when explaining why
  an `async fn` does nothing on its own or why a future fails a `Send` bound,
  state the underlying mechanism (std ships no executor; every value alive
  across an `.await` is stored inside the future's generated state machine),
  not only the observable effect.
when_to_use: >
  Load when the provider-stripped model id (see stack-detection.md) resolves
  to exactly `step-5-preview-free` (e.g. `opencode-go/step-5-preview-free` →
  `step-5-preview-free`) AND the repository is a Rust project (a `Cargo.toml`
  file — per meta.yaml detection). For any other model or non-Rust repo, do
  not load.
tuned_for: step-5-preview-free
tuned_version: "5-preview"
stack: rust
source_scorecard: ../scores/step-5-preview-free.md
threshold: 0.85
revalidate_when: model_version changes
---
# Rust corrections for step-5-preview-free

<!-- kaizen:digest -->
**Rust async — state the mechanism, not only the effect:**
1. **The standard library ships no executor.** `std` defines `Future`, `Poll`, `Context` and `Waker` but nothing that polls a future to completion. Naming `block_on`/`tokio::spawn` is not enough: say explicitly that std has no runtime, which is why Tokio, async-std, smol or `futures::executor::block_on` is mandatory.
2. **Every local alive across an `.await` is stored as a field of the future's generated state machine.** That is why one `!Send` value (`Rc`, `std::sync::MutexGuard`) held across an await makes the whole future `!Send` and `tokio::spawn` rejects it. Name the state-machine capture, not just "must be Send across await".
<!-- /kaizen:digest -->

## async: the standard library has no executor

- When asked what calling an `async fn` returns and what makes it run, do not
  stop at "a lazy future that something must poll" plus a list of Tokio APIs.
  State the missing piece explicitly: `std` has no built-in executor, so a
  third-party runtime (`#[tokio::main]`, `Runtime::block_on`, async-std, smol,
  `futures::executor::block_on`) is required to drive a top-level future.
- A future is driven either by `.await` inside another async context or, at the
  top level, by an executor polling it; both ultimately rest on an executor
  that std does not provide.

## async: values held across `.await` become part of the future

- The compiler lowers an `async fn`/block into a state machine; every value
  still alive at an `.await` point is saved as a field of that state machine so
  it can be restored when the future is polled again.
- Therefore the future is `Send` only if every such held value is `Send`. An
  `Rc`, a `RefCell` borrow guard, or a `std::sync::MutexGuard` alive across an
  await makes the entire future `!Send`.
- `tokio::spawn` on the multi-threaded runtime requires `Send` because the task
  may resume on a different worker thread. Explain in this order: capture
  (held value becomes a field of the future), consequence (future is `!Send`),
  bound (`spawn` requires `Send`), fix (drop or scope the value before the
  `.await`, switch to `Arc` / `tokio::sync::Mutex`, or use `spawn_local` on a
  `LocalSet`).
