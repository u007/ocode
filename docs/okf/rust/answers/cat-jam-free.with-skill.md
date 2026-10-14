# cat-jam-free — WITH-SKILL closed-book answers for rust (opencode-go, 2026-10-14)
# Delivery: derived/rust.cat-jam-free.SKILL.md body (front matter stripped) prepended to _prompts/rust.md; one
# `ocode run -m opencode-go/cat-jam-free -effort med` session, isolated HOME with every tool denied, cwd holding only the sheet;
# session audited in sqlite: 1 user + 1 assistant message, 0 tool calls.

- id: rust-ownership-01
  answer: |
    `let b = a;` moves ownership of the `String`'s heap buffer to `b`; `a` is
    then invalidated, so using `a` is a compile error. This keeps exactly one
    owner responsible for freeing that buffer on drop, which prevents a
    double-free without a garbage collector. Use `a.clone()` to get a real
    second owned copy.

- id: rust-ownership-02
  answer: |
    `Copy` is implicit bitwise duplication on assignment/pass by value (no
    move, source stays usable); `Clone` is an explicit, possibly expensive
    `.clone()`. `Copy: Clone`, so every `Copy` type must implement `Clone`.
    A type implementing `Drop` cannot be `Copy` because implicit copies would
    each run the destructor on the same resource.

- id: rust-ownership-03
  answer: |
    Passing a `Vec<T>` by value moves ownership into the function; the caller's
    binding is no longer usable. Options: borrow it (`&[T]` or `&mut [T]`),
    have the function return it back, clone it first, or redesign the API to
    take ownership intentionally.

- id: rust-borrowing-01
  answer: |
    At any time, either one mutable borrow `&mut T` or any number of immutable
    borrows `&T` may exist, never both; borrows must not outlive the borrowed
    value. This prevents use-after-free, dangling references, and mutable
    aliasing/data races at compile time.

- id: rust-lifetimes-01
  answer: |
    A lifetime annotation such as `'a` expresses a constraint on how long a
    reference must remain valid and how references relate to each other. It
    does not change when the value is actually dropped or how long it lives at
    runtime; it is a compile-time validity check.

- id: rust-lifetimes-02
  answer: |
    Lifetime elision rules: (1) each elided input lifetime becomes a distinct
    lifetime parameter; (2) if there is exactly one input lifetime, it is
    assigned to all output lifetimes; (3) if a method has `&self` or
    `&mut self`, that lifetime is assigned to all output lifetimes. Thus
    `fn first(s: &str) -> &str` gets one input lifetime assigned to the output.

- id: rust-lifetimes-03
  answer: |
    `&'static T` is a reference guaranteed valid for the entire program
    duration, usually to data in the binary or intentionally leaked memory.
    `T: 'static` is a bound saying the type contains no borrowed references
    with shorter lifetimes; owned types satisfy it. An owned `'static`-bounded
    value can still be dropped at runtime, unlike a true `&'static` borrow.

- id: rust-lifetimes-04
  answer: |
    Owned trait objects such as `Box<dyn Error>` default to `dyn Error + 'static`.
    If the concrete type may hold borrowed data with lifetime `'a`, annotate it
    as `Box<dyn Error + 'a>` (and similarly for other owners). The `+ 'a` is
    needed whenever captured/lifetime-bound references are possible.

- id: rust-traits-01
  answer: |
    Generics (`fn f<T: Trait>`) use static dispatch: the compiler
    monomorphizes, enabling inlining and zero runtime overhead but possible
    code bloat. Trait objects (`&dyn Trait`) use dynamic dispatch through a
    vtable: smaller binary and heterogeneous collections, but pointer
    indirection and no inlining.

- id: rust-traits-02
  answer: |
    In argument position, `impl Trait` lets the caller choose the concrete
    type that satisfies the bound. In return position, `-> impl Trait` means
    the callee chooses one concrete type; callers see only the trait interface.

- id: rust-traits-03
  answer: |
    The orphan rule allows implementing a trait for a type only if either the
    trait or the type is defined in the current crate. You cannot
    `impl Display for Vec<T>` in your own crate because both `Display` and
    `Vec` are foreign, which prevents conflicting implementations across
    crates.

- id: rust-error-01
  answer: |
    Return `Option<T>` when absence is an expected, ordinary outcome with no
    extra diagnostic. Return `Result<T, E>` when the operation can fail in a
    way callers may need to inspect, report, recover from, or propagate.

- id: rust-error-02
  answer: |
    On `Ok(x)`, `?` evaluates to `x`. On `Err(e)`, it immediately returns
    `Err(Into::into(e))` from the current function, where the conversion uses
    `From<E>` into the function's declared error type. The function must
    return a compatible `Result` (or `Option`).

- id: rust-error-03
  answer: |
    Use `panic!`/`unwrap` for unrecoverable programming errors or violated
    invariants, not expected runtime failures such as bad user input or missing
    files. Libraries should usually return `Result`; unwrap is acceptable only
    when the invariant is actually guaranteed.

- id: rust-error-04
  answer: |
    The error type should be an enum/struct implementing `Debug`, `Display`,
    and `std::error::Error`, plus `From` conversions from each underlying
    error so `?` works. `thiserror` derives `Display`, `Error`, and those
    `From` implementations, removing boilerplate while preserving source
    chaining.

- id: rust-iterators-01
  answer: |
    Iterator adapters such as `map` and `filter` are lazy: they only build a
    new iterator description and perform no computation. Work runs when a
    consuming adapter such as `collect`, `fold`, `for_each`, or `next` pulls
    values.

- id: rust-iterators-02
  answer: |
    `iter()` yields `&T` and leaves the collection usable. `iter_mut()` yields
    `&mut T` and allows in-place mutation. `into_iter()` consumes the
    collection and yields owned `T`; after it, the original collection is
    generally unavailable.

- id: rust-iterators-03
  answer: |
    `collect` is generic in the target collection via `FromIterator`, so the
    compiler often cannot infer which concrete type to build. Specify it with a
    type annotation or turbofish, e.g. `collect::<Vec<_>>()`. Collecting into
    `Result` stops at the first `Err` and returns that error; otherwise it
    returns `Ok(collection)`.

- id: rust-iterators-04
  answer: |
    `move` makes the closure take ownership of its captured variables rather
    than borrowing them. This is needed when the closure must outlive the
    current scope, such as when returning it; `Copy` captures are copied.

- id: rust-smartptr-01
  answer: |
    `Box<T>` uniquely owns a heap-allocated `T` and dereferences to `T`.
    Genuine needs include recursive data structures such as cons lists/trees,
    trait objects, and moving large values cheaply behind a pointer.

- id: rust-smartptr-02
  answer: |
    `Rc<T>` uses non-atomic reference counting for single-threaded shared
    ownership; `Arc<T>` uses atomic refcounting and is thread-safe. `Arc` is
    not always preferable because atomic operations cost more; use `Rc` when
    sharing stays on one thread.

- id: rust-smartptr-03
  answer: |
    Interior mutability lets you mutate data through a shared reference.
    `RefCell<T>` enforces borrow rules at runtime via `borrow`/`borrow_mut`,
    panicking on violations. The cost is runtime checks and possibly panics,
    unlike normal `&mut`, which is compile-time exclusive access.

- id: rust-smartptr-04
  answer: |
    `Rc<RefCell<T>>` combines shared ownership with runtime-checked mutable
    borrows, the standard single-threaded shared-mutable-state idiom. The
    multi-threaded equivalent is `Arc<Mutex<T>>` or `Arc<RwLock<T>>`.

- id: rust-concurrency-01
  answer: |
    `Send` means a value can be moved between threads; `Sync` means `&T`
    can be shared between threads. Rust uses these markers in APIs such as
    `thread::spawn` and shared data access, making non-thread-safe types
    compile errors rather than data races.

- id: rust-async-01
  answer: |
    Calling an `async fn` returns a lazy future (a compiler-generated state
    machine) and runs nothing. It must be awaited within an async context or
    driven to completion by an executor/runtime, such as Tokio. `std`
    provides no executor.

- id: rust-async-02
  answer: |
    `Rc` is `!Send` because its refcount is non-atomic, and `std::sync::MutexGuard`
    is typically `!Send`. Values alive across `.await` become fields of the
    future's state machine, so such a value makes the whole future `!Send`.
    `tokio::spawn` requires `Send` because the task may resume on another
    worker thread. Fixes include dropping/scoping before await, `Arc`,
    `tokio::sync::Mutex`, or `spawn_local`.

- id: rust-async-03
  answer: |
    Async runtimes are cooperative: a task yields only at an `.await` that
    returns `Pending`. Blocking code never reaches a yield point, so it holds
    the worker thread and starves other tasks. Instead use async equivalents
    or offload work with `tokio::task::spawn_blocking`, a dedicated thread, or
    Rayon for CPU-bound work.

- id: rust-match-01
  answer: |
    `match` must cover every possible value of the scrutinee. The compiler
    rejects non-exhaustive matches. This is especially valuable for enums:
    adding a variant forces every match site to handle it, preventing
    forgotten cases.

- id: rust-match-02
  answer: |
    Use `if let` for one pattern plus an implicit else; `let ... else` to bind
    on success and diverge/return on failure; a full `match` when several
    patterns or exhaustive handling matter.

- id: rust-match-03
  answer: |
    With `match &opt { Some(x) => ... }`, `x` has type `&T` due to match
    ergonomics/default binding modes. The binding automatically becomes a
    reference because the scrutinee is a reference.

- id: rust-match-04
  answer: |
    Destructuring pulls fields out of structs/enums/tuples, e.g.
    `Point { x, y }`. Match guards add conditions after a pattern, e.g.
    `Some(x) if x > 0`. `@` binds the whole matched value while also
    matching a subpattern, e.g. `Some(x @ 1..=5)`.
