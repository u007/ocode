# step-5-preview-free — WITH-SKILL validation answers for rust (opencode-go, 2026-10-12); closed-book,
# derived/rust.step-5-preview-free.SKILL.md body prepended to _prompts/rust.md as active guidance.
- id: rust-ownership-01
  answer: |
    `let b = a;` moves the value: the stack metadata of the `String` (pointer, length,
    capacity) is copied to `b`, and `a` is treated as uninitialized thereafter. Any
    later use of `a` fails to compile ("borrow of moved value"). The reason is that a
    `String` owns a heap buffer; if both variables were usable, both would run
    `Drop` at scope end and free the same buffer twice. Only `Copy` types are
    duplicated instead of moved.
- id: rust-ownership-02
  answer: |
    `Copy` marks types whose values can be duplicated by a trivial bitwise copy with
    no destructor side effects; assignment and passing copies implicitly, silently,
    and the original stays usable (integers, `bool`, shared references `&T`, tuples
    of `Copy`). `Clone` is an explicit, possibly expensive duplication you must opt
    into with `.clone()`, free to do arbitrary work (deep-copy heap data). Types
    that implement `Drop` cannot be `Copy`, and the compiler rejects both: since
    `Copy` duplicates silently, the duplicate would also run its destructor, so the
    same resource would be released twice. No destructor is a precondition of `Copy`.
- id: rust-ownership-03
  answer: |
    Passing the `Vec<T>` by value moved it into the function; the callee took
    ownership and drops it when it returns, so using it after the call is a use of
    a moved value and is rejected at compile time. Options: pass a borrow
    (`&Vec<T>`, or better `&[T]`, which is read-only and not moved); pass `&mut Vec<T>`
    if the function must mutate it; clone before passing if the callee needs its own
    copy; have the function take and give it back (`fn f(mut v: Vec<T>) -> Vec<T>`)
    after a value-consuming transform; or restructure so ownership clearly transfers.
- id: rust-borrowing-01
  answer: |
    At any moment you may have **either** any number of immutable references
    (`&T`) **or** exactly one mutable reference (`&mut T`) — never both — and all
    references must always be valid (never dangle). Plus the mutation rule: data
    cannot be mutated through a shared reference. This prevents a whole class of
    bugs at compile time: data races (read while another writes), iterator
    invalidation and use-after-free (mutating a container while a borrow into it
    exists), and aliasing-plus-mutation surprises, without a runtime cost.
- id: rust-lifetimes-01
  answer: |
    `'a` names a *region* of code during which a reference is valid. An annotation
    is a constraint between references — input, output and storage — that tells the
    borrow checker how long validity must reach (e.g. that a returned `&'a str`
    outlives the call and borrows from the input). It does **not** change how long a
    value actually lives, cannot extend a lifetime, and has no runtime existence:
    lifetimes are erased after compilation. If the annotation's constraints conflict
    with actual borrows, it is rejected.
- id: rust-lifetimes-02
  answer: |
    Three elision rules: (1) every elided lifetime in an argument position becomes a
    distinct, fresh lifetime parameter; (2) if there is exactly one input lifetime,
    it is assigned to every elided output lifetime; (3) if there are several input
    lifetimes but one of them is `&self`/`&mut self` of a method, that lifetime is
    assigned to all elided output lifetimes. If none of the rules can determine the
    output, compilation fails asking for explicit annotation. `fn first(s: &str) ->
    &str` works by rule 2 — `&'a str -> &'a str` — asserting the result borrows from
    the input. Hence a two-argument `fn f(a: &str, b: &str) -> &str` needs manual
    lifetimes: rule 3 does not apply without a receiver.
- id: rust-lifetimes-03
  answer: |
    (a) `&'static T` is a reference valid for the entire program duration — a
    string literal, a `static`/`const` item (promoted where possible). (b) The bound
    `T: 'static` means the *type* T owns all its data, or only borrows things that
    live forever: no part of it holds a shorter non-static borrow. Importantly
    `T: 'static` does not mean `&'static T`: `String`, `Vec<i32>`, and owned structs
    are all `'static` without being static references. The bound appears because
    e.g. a boxed or spawned value must not secretly outlive its borrows.
- id: rust-lifetimes-04
  answer: |
    `Box<dyn Error>` is shorthand for `Box<dyn Error + 'static>`: a boxed trait
    object defaults to the `'static` object lifetime bound, so the underlying type
    may not contain non-`'static` borrows. Once an error type borrows — e.g. wraps a
    `&'a str` or a borrowed buffer — it is only valid for `'a`, so you must write
    `Box<dyn Error + 'a>` (or, for a borrowed object itself, `&'a (dyn Error + 'a)`),
    tying the object's validity to that region. The default is chosen by the
    reference/boxing position: behind a `&'a` argument the object lifetime elides to
    that argument's lifetime instead.
- id: rust-traits-01
  answer: |
    Generics use static dispatch: the compiler monomorphises, generating one
    specialised copy of `f` per concrete `T`, calling it directly and permitting
    inlining and cross-inlining — near-zero call overhead at the cost of code bloat
    and slower compiles. Trait objects use dynamic dispatch: a fat pointer (data +
    vtable) and an indirect virtual call, so no per-type code duplication and
    heterogeneous collections (`Vec<Box<dyn Trait>>`) are possible. Tradeoff is
    speed and binary size (generics) versus flexibility, smaller binaries and the
    ability to break recursion and store mixed types (dyn). Dyn also requires
    object safety and unsized, pointer-first storage.
- id: rust-traits-02
  answer: |
    In argument position `impl Trait` is sugar for an anonymous generic parameter:
    `fn f(x: impl Trait)` ≡ `fn f<T: Trait>(x: T)` — the **caller** picks the
    concrete type at each call site, monomorphised per instantiation (and you
    cannot write it with turbofish). In return position `impl Trait` is an opaque
    existential type: the **function** picks the concrete type, the caller only
    knows it implements the trait and cannot name it; dispatch is still static.
    This lets you return a closure or iterator type whose name is unnameable while
    keeping zero-cost calls.
- id: rust-traits-03
  answer: |
    The orphan rule (coherence) says you may only implement a trait for a type when
    the trait is local to your crate, or the type is local to your crate.
    `impl Display for Vec<T>` has neither: `Display` is std's, `Vec` is std's, and
    `Vec<T>` is not "covered" by any local type parameter — so it is rejected. Its
    purpose is global coherence: without it, two independent crates could both add
    an impl for the same foreign trait + foreign type, and a downstream crate using
    both would have contradictory duplicate implementations. The workaround is a
    local wrapper — `struct MyVec<T>(Vec<T>)` — which makes the type local.
- id: rust-error-01
  answer: |
    Use `Option<T>` when a value may simply be absent and the reason is uniform and
    unremarkable — looking up a key that may be missing, the next item of something
    that may be empty. Use `Result<T, E>` when an operation may fail and the failure
    kind, detail or recoverability matters — I/O, parsing, validation, user input —
    because the error value can be inspected, logged, retried or converted. Both
    compose with `?`; the dividing question is "is this an expected, information-free
    'none', or an error worth carrying a reason for?"
- id: rust-error-02
  answer: |
    In a function returning `Result<U, F>`, `expr?` on a `Result<T, E>`: on `Ok(v)`
    it unwraps and evaluation continues with `v`; on `Err(e)` it returns early from
    the enclosing function with `Err(From::from(e))` — that is, it requires (and
    calls) `impl From<E> for F` to convert the inner error into the function's own
    error type automatically. It also propagates `?`'s `?Sized`-agnostic *move*: `e`
    is consumed. If no `From` impl exists, compilation fails and you add a `.map_err`
    or the conversion. `?` generalises to `Option` (in functions returning `Option`,
    converting `None` into an early `None`).
- id: rust-error-03
  answer: |
    `panic!`/`unwrap()` suits unrecoverable programmer errors and broken
    invariants: an invariant that cannot hold (index out of bounds, unexpected
    `None` after a preceding check, `unreachable!()`, poisoned lock, failed `
    unwrap` of a value guaranteed by construction), plus tests, examples and
    prototypes. Return `Result` whenever the caller can plausibly recover or react:
    missing files, malformed user input, network failures, invalid configuration —
    and generally in libraries, where you should not decide the process must die.
    Middle ground: `expect("message")` for an invariant with a diagnostic, or
    `unwrap_or`/`unwrap_or_else`/`unwrap_or_default` for benign fallbacks.
- id: rust-error-04
  answer: |
    The error type needs: a `Debug` impl (required by the machinery and `?`), an
    `impl std::error::Error` with a `Display` impl describing the failure, and —
    crucially for `?` — an `impl From<E> for YourError` for every underlying error
    type `E` it wraps. It should also be `'static` (and usually `Send + Sync`) so it
    can be boxed, erased or sent across threads. `thiserror` removes the
    boilerplate: `#[derive(Error, Debug)]` on the enum plus `#[error("...")]` string
    attributes per variant generate `Display`, and `#[from]` on a field generates
    the matching `From` impl (with `#[source]` wiring error chaining in
    `Error::source`).
- id: rust-iterators-01
  answer: |
    Iterator adapters are lazy: `map` and `filter` do not compute anything, they
    build an adapter struct that merely *records* the chain of closures. No closure
    runs until something drives the iterator — a consumer. Consumers pull items by
    repeatedly calling `next()`: `collect`, `for` loops, `for_each`, `sum`, `count`,
    `find`, `any`, `all`, `fold`, `nth`, `last`. Each `next()` pulls one item
    through the whole chain on demand, so work happens lazily and short-circuits.
    After monomorphisation and inlining the chain typically compiles to code
    comparable with a hand-written loop over the source.
- id: rust-iterators-02
  answer: |
    `iter()` yields `&T` — shared, read-only borrows; the collection is unchanged
    and remains usable and owned by you afterwards. `iter_mut()` yields `&mut T`,
    so you may mutate in place; the collection stays alive but is exclusively
    mutably borrowed, so you cannot touch it otherwise until iteration ends.
    `into_iter()` consumes the collection, yielding owned `T` values by value; the
    original is moved and dropped afterwards. Remember `for x in v` is sugar for
    `IntoIterator::into_iter(v)` (yields `T`), while `for x in &v`/`&mut v` yields
    `&T`/`&mut T`.
- id: rust-iterators-03
  answer: |
    The error appears when the target collection type is undetermined: `collect` is
    generic over `B: FromIterator<A>`, and there is nothing tying it to a concrete
    `B` (its result is discarded, or neither a turbofish nor a binding type names
    it). Fix it with a turbofish `collect::<Vec<i32>>()`, a type annotation
    `let v: Vec<_> = ...iter().collect();`, or a turbofish with `_` so only the
    container is fixed and element type is inferred. Collecting into a `Result` (or
    `Option`) is special: `FromIterator<Result<T, E>> for Result<Vec<T>, E>`
    short-circuits — it drops out at the **first** `Err` and returns that error
    immediately, and only if all items are `Ok` does it produce `Ok(vec)`.
- id: rust-iterators-04
  answer: |
    A closure captures by reference when possible (matching how the body uses the
    variable), so returning it outlives the local it borrows and the compiler
    rejects it (or suggests `move`/`'static`). `move` changes the *capture mode*:
    captured variables are moved (or copied, if `Copy`) into the closure's own
    struct, so the closure owns the data instead of borrowing the stack frame —
    that is what makes it returnable and `'static` where the data itself is
    `'static`. Consequences: the original local can no longer be used afterwards,
    and `move` does not make the closure `Copy` nor change its call semantics, only
    where the captured values live.
- id: rust-smartptr-01
  answer: |
    `Box<T>` is a single-owner pointer to a heap allocation with ~zero overhead:
    the pointee is dropped when the box goes out of scope, and moving the box moves
    only a pointer. It is genuinely needed (a) for unsized storage —
    `Box<dyn Trait>` (and `Box<str>`/`Box<[T]>` / `Box<dyn Future>`) are only
    possible behind a pointer, and (b) for recursive types, e.g.
    `enum List { Cons(i32, Box<List>), Nil }`, where omitting the `Box` would give
    the type an infinite, uncomputable size. Secondary uses: keep an enum variant's
    payload off the stack, or transfer ownership of a large value cheaply.
- id: rust-smartptr-02
  answer: |
    Both are reference-counted shared owners deallocated when the count hits zero.
    `Rc<T>` is not thread-safe: cloning/dropping uses plain non-atomic increments,
    so it is `!Send`/`!Sync` and single-threaded only — but cheap. `Arc<T>` uses
    atomic operations, is `Send + Sync` when `T` is, and can cross thread
    boundaries. You don't always use `Arc` because atomics cost more on every
    clone/drop (memory-ordering fences, cache-line contention) — meaningless in a
    one-thread program but measurable in hot paths — and `Arc` use signals
    cross-thread intent and imposes `T: Send + Sync` you may not need.
- id: rust-smartptr-03
  answer: |
    Interior mutability lets you mutate data *through* a shared `&` reference,
    moving the borrow check from compile time to runtime. `RefCell<T>` holds a
    counter of active borrows: `borrow()` returns a `Ref<T>` guard (panics if a
    mutable borrow is live), `borrow_mut()` returns a `RefMut<T>` (panics if any
    other borrow, mutable or shared, is live), and the borrow is released when the
    guard is dropped. The cost compared to `&mut`: the exclusivity invariant is
    checked dynamically and violations panic instead of being rejected by the
    compiler; there is a small runtime overhead; and unsafe patterns like holding a
    guard while re-borrowing can crash at runtime. (`Cell<T>` does the same for
    `Copy` data without guards, via `get`/`set`.)
- id: rust-smartptr-04
  answer: |
    Single-threaded shared mutable state needs two things at once: multiple owners
    of one value, and mutation through those owners. `Rc<T>` supplies shared
    ownership but only hands out `&T`, and a `&T` cannot mutate anything; wrapping
    in `RefCell<T>` gives it `borrow()`/`borrow_mut()` interior mutability so every
    holder can mutate. Hence `Rc<RefCell<T>>` (`std::rc::Rc`, `std::cell::RefCell`).
    The multi-threaded equivalent is `Arc<Mutex<T>>` (or `Arc<RwLock<T>> when
    reads dominate; `Arc<tokio::sync::Mutex>` when guards must be held across
    `.await`). Watch out that a `std::sync::MutexGuard` held across an await makes
    the enclosing future `!Send`.
- id: rust-concurrency-01
  answer: |
    `Send` means ownership of a value can be transferred to another thread: it is
    safe for the value to be *moved* across thread boundaries (its `Drop` may then
    run on whichever thread owns it). `Sync` means shared references are
    transfer-safe: `&T` may be sent to another thread, i.e. several threads may
    concurrently hold `&T` to the same value without causing unsafety. Both are
    unsafe auto traits — the compiler implements them automatically for a composite
    type when all its parts are (`Rc`, `Cell`, `RefCell`, raw pointers opt out as
    `!Send`/`!Sync`). Rust uses them as bounds on the concurrency APIs:
    `thread::spawn` and its return require `Send`, channel items require `Send`,
    so handing an `Rc` to another thread is a *compile* error, not a race on its
    non-atomic refcount. Mutexes/atomics are `Sync` precisely because they supply
    the synchronisation `Cell`/`RefCell` lack.
- id: rust-async-01
  answer: |
    Calling an `async fn` returns an opaque, inert future (`impl Future<Output =
    T>`) — the lowered state machine that does nothing until polled; the body up
    to the first `.await` has not executed yet. To make it run, it must be
    `.await`ed inside another async context, or — at the top level — driven to
    completion by an executor that repeatedly calls `poll` on it and reacts to its
    `Waker`. Critically, `std` defines `Future`, `Poll`, `Context` and `Waker` but
    ships **no executor**: there is no built-in runtime to poll a future to
    completion, which is why a third-party one is mandatory — `#[tokio::main]` /
    `Runtime::block_on`, async-std, smol, or `futures::executor::block_on`. Both
    routes ultimately rest on such an executor.
- id: rust-async-02
  answer: |
    The compiler lowers the async fn/block into a state machine in which every
    local still alive at an `.await` point is saved as a *field of the generated
    future*, so it can be restored when the future is polled again. The `Rc` or
    `std::sync::MutexGuard` held across that await therefore becomes part of the
    future, and since those types are `!Send`, the whole future type becomes
    `!Send`. `tokio::spawn` on the multi-threaded runtime requires `Send + 'static`,
    because the task may resume on a *different* worker thread. Fixes, in this
    order: drop or scope the value before the `.await` (put it in an inner block so
    it is dead at the await), switch to something `Send` (`Arc`, or
    `tokio::sync::Mutex` whose guard is `Send`), or run the task on a `LocalSet`
    with `spawn_local`.
- id: rust-async-03
  answer: |
    A blocking call does not yield to the runtime, so the executor thread cannot
    poll any other task while it blocks. On a single-threaded (current-thread)
    runtime every other task is stalled — no timers, I/O or readiness events
    progress for anything; on the multi-threaded runtime a whole worker is pinned
    out, causing latency spikes, starvation and, if the blocked thread holds a lock
    other tasks need, outright deadlock. Throughput collapses long before the CPU
    is saturated. Instead: use async-aware alternatives — `tokio::time::sleep`
    instead of `thread::sleep`, `tokio::fs` instead of blocking file syscalls; and
    for genuinely blocking or CPU-heavy work, move it off the worker with
    `tokio::task::spawn_blocking` (bounded blocking pool) or `block_in_place`,
    chunking long CPU work and yielding with `tokio::task::yield_now()` between
    chunks.
- id: rust-match-01
  answer: |
    Exhaustive means every possible value of the scrutinee must be covered by some
    arm, otherwise compilation fails (unless you consciously add a `_` catch-all).
    With an enum this is especially valuable: each variant is a distinct case,
    often carrying different data, and adding a new variant breaks the build at
    every `match` that omits it. The compiler then becomes an automatic refactor
    checklist — you cannot silently forget to handle the new case and get a runtime
    fall-through or undefined behaviour. It also encourages you to check for
    invariants exhaustively rather than relying on defaults.
- id: rust-match-02
  answer: |
    `if let` is sugar for a match with a `_ => ()` arm: use it when you care about
    exactly one pattern and want to ignore the rest (`if let Some(x) = opt { ... }`).
    `let ... else` is guard-clause style: use it when a pattern must succeed for the
    remaining code to make sense; on failure the `else` block **must** diverge —
    `return`, `break`, `continue`, `panic!` — and the bound variables exist
    afterwards only in the success path (`let Some(cfg) = settings else { return; };`).
    Use a full `match` when you need two or more arms, must handle several variants
    (or bind data differently per variant), or want the exhaustiveness check to
    force you to deal with the other cases.
- id: rust-match-03
  answer: |
    `x` has type `&T` — a shared reference to the value inside the `Some` — because
    of *match ergonomics* (RFC 2005, default binding modes). When the scrutinee is
    a reference (`&Option<T>` here) and the pattern is a non-reference pattern
    (`Some(x)`), the compiler automatically dereferences one level and switches the
    binding mode to `ref`, so inner bindings are taken by reference. With a
    `&mut Option<T>` scrutinee the bindings become `&mut T`. If instead you match
    `Some(&x)` against `&Option<i32>`, `x` is a plain `i32`.
- id: rust-match-04
  answer: |
    Destructuring lets a pattern break apart composite values and bind their
    components directly: structs (`Point { x, y }`), tuples (`(a, b)`), enums
    (`Some(n)`, `Err(e)`), slices (`[first, .., last]`, `[a, b]`), and references,
    nestably. A **match guard** is an extra `if` condition attached to an arm
    (`n if n % 2 == 0`); when the pattern matches it is evaluated, may use the
    bound variables, and on failure matching simply continues with later arms —
    it expresses tests patterns alone cannot relate to bound data. An **`@`
    binding** (`binding @ subpattern`) names the *whole* matched value while also
    testing it against a sub-pattern, so you get both the value and the shape
    constraint, e.g. `n @ 1..=5` or `event @ Event::Click { .. }`.
