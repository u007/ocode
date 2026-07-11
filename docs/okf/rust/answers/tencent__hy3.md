# rust knowledge benchmark — answers
# Model: novita-ai/tencent/hy3   (provider-stripped id: tencent/hy3)
# CLOSED-BOOK via ocode run -dir <empty> (no corpus access). corpus_rev 1.

- id: rust-ownership-01
  answer: |
    Assigning `let b = a;` where `a` is a `String` performs a *move*: ownership of the heap-allocated buffer (and the `String`'s bookkeeping) is transferred to `b`. `a` is then left in a logically uninitialized/invalid state, and any later use of `a` is a compile error ("use of moved value"). This is because `String` is not `Copy` — it owns heap data — and Rust's ownership model guarantees exactly one owner responsible for freeing that data. A move prevents double-free, use-after-free, and dangling pointers. If you instead wanted both names usable, you'd clone (`a.clone()`) or pass/keep a reference.

- id: rust-ownership-02
  answer: |
    `Copy` is an implicit, marker-style trait for types that can be duplicated simply by copying their bits (a bitwise/memcpy duplication with no cleanup needed), e.g. `i32`, `bool`, `(i32, f64)`. Copies happen automatically on assignment and passing. `Clone` is an explicit trait with a `.clone()` method that may do an arbitrary (often deep) duplication, e.g. `String`/`Vec` allocate fresh heap data.
    A type that implements `Drop` cannot be `Copy` because `Drop` means the type owns resources that must be cleaned up exactly once (freeing memory, closing handles). If it were also `Copy`, the value could be silently duplicated by any assignment, so the same resource would be freed/dropped multiple times (double free) or a use-after-free would occur. The compiler therefore forbids `Copy` together with `Drop`.

- id: rust-ownership-03
  answer: |
    Passing the `Vec<T>` by value hands ownership of the vector (and its heap buffer) to the function. After the call the caller no longer owns it, so using it afterward is a compile error (moved value).
    Options: (1) pass a reference instead — `&Vec<T>` or preferably `&[T]` — and borrow the contents; (2) have the function return the `Vec` back (e.g. as part of its return value) so ownership comes back; (3) pass `&mut Vec<T>` if the function needs to mutate it; (4) `.clone()` the `Vec` before passing (works but allocates/is costly); or (5) restructure so the function takes ownership deliberately and you no longer need it afterward.

- id: rust-borrowing-01
  answer: |
    Rust's core borrowing rule: at any given point you may hold either exactly one mutable reference (`&mut T`) to a piece of data, or any number of immutable references (`&T`), but never both at the same time. Additionally, every reference must be valid for its entire lifetime (no dangling references).
    This rules out whole classes of bugs at compile time: data races (two+ accesses with one write, no sync), iterator invalidation (mutating a collection while iterating), and use-after-free/dangling pointers, because the borrow checker rejects overlapping conflicting accesses before the program ever runs.

- id: rust-lifetimes-01
  answer: |
    A lifetime annotation like `'a` expresses a *relationship/constraint* between the lifetimes of references: it tells the borrow checker "this reference is valid for at least as long as the region `'a`" and ties the validity of one reference to another. Lifetimes are purely a compile-time construct.
    It does *not* change how long any value actually lives — the scope/lifetime of data is determined by where it's created/owned, not by annotations. Annotations only let you describe and verify that references do not outlive the data they point to.

- id: rust-lifetimes-02
  answer: |
    Lifetime elision rules (so you can omit explicit `'a` in common cases):
    1. Each elided input lifetime (each reference parameter without an explicit lifetime) gets its own distinct anonymous lifetime parameter.
    2. If there is exactly *one* input lifetime, that lifetime is assigned to all elided output lifetimes.
    3. If there are multiple input lifetimes but one of them is `&self` or `&mut self`, the lifetime of `self` is assigned to all elided output lifetimes.
    For `fn first(s: &str) -> &str`, rule 2 applies: the single input `&str` lifetime is used for the `&str` return, meaning the returned reference is valid as long as the input is.

- id: rust-lifetimes-03
  answer: |
    `&'static T` (a *reference* with a `'static` lifetime): the reference is guaranteed valid for the entire program run. The data it points to must live forever, e.g. string literals (`&'static str`) or memory that has been leaked. `T: 'static` (a *bound* on a type parameter): it means `T` contains no borrowed references that could outlive `'static` — i.e. `T` either owns all its data or only holds `'static` references. Crucially, `T: 'static` does NOT require `T` itself to live forever; a `String` or `Vec<u8>` satisfies `T: 'static` even though the value is dropped normally. The bound just says "this type doesn't borrow anything with a shorter lifetime."

- id: rust-lifetimes-04
  answer: |
    Trait objects have a default lifetime bound of `'static`. So `Box<dyn Error>` is shorthand for `Box<dyn Error + 'static>` — the compiler assumes the trait object owns all its data (or only holds `'static` references).
    You need an explicit `+ 'a` when the trait object *borrows* data with a shorter lifetime (e.g. it holds a `&'a str` internally or is tied to some local). Then you must write `Box<dyn Error + 'a>` and thread that `'a` through the type so the borrow checker can verify the trait object doesn't outlive the data it borrows. Omitting it yields an error that the borrowed data doesn't live long enough / doesn't satisfy `'static`.

- id: rust-traits-01
  answer: |
    Static dispatch via generics `fn f<T: Trait>(x: T)`: the compiler monomorphizes — it generates a specialized copy of the function for each concrete type used. Calls are resolved at compile time, fully inlined and zero-cost (no indirection), but it produces larger binaries and the concrete type is fixed at compile time.
    Dynamic dispatch via trait objects `&dyn Trait`: a single compiled function is used, and method calls go through a vtable looked up at runtime. This is more flexible (heterogeneous collections, runtime-chosen types, smaller code size) but incurs a small vtable indirection cost and prevents inlining/monomorphization optimizations.
    Tradeoff: performance and optimization (generics) versus flexibility and binary-size/size-of-values (trait objects).

- id: rust-traits-02
  answer: |
    Argument position `fn f(x: impl Trait)`: the `impl Trait` is an anonymous generic parameter — equivalent to `fn f<T: Trait>(x: T)`. The *caller* chooses the concrete type (any type implementing the trait may be passed), and each call site is monomorphized.
    Return position `fn f() -> impl Trait`: the function returns *some* concrete type implementing the trait, but the *implementer* (the function body) chooses which one, and that type is opaque to the caller. The caller only knows it implements the trait. It does not add a generic parameter; the function picks one concrete type (it must always return the same concrete type on every path). Useful for hiding concrete types (e.g. returning an iterator adapter or future) without boxing.

- id: rust-traits-03
  answer: |
    The orphan rule (part of coherence): you may implement a trait for a type only if *either* the trait *or* the type is defined in the current crate. This prevents two different crates from both implementing the same trait for the same type, which would make method resolution ambiguous.
    `Vec<T>` is defined in `alloc`/`std` and `Display` is defined in `std`, neither in your crate, so `impl Display for Vec<T>` violates the orphan rule — the compiler rejects it. To add behavior you must either define the trait or the type yourself, or use a newtype wrapper (which you own).

- id: rust-error-01
  answer: |
    Return `Option<T>` when an operation may simply *not produce a value* and there is no useful information to convey about the absence (it's an expected, normal "nothing here" outcome) — e.g. a lookup that might miss, or `first()` on a possibly-empty collection.
    Return `Result<T, E>` when the operation can *fail* and you want to explain *why* it failed, carrying an error value/type the caller can inspect, classify, or recover from — e.g. parsing, I/O, network. Rule of thumb: "absence is a value" → `Option`; "failure carries a reason" → `Result`.

- id: rust-error-02
  answer: |
    On a `Result<T, E>`, the `?` operator: if the value is `Ok(v)`, it evaluates to `v` and execution continues; if it is `Err(e)`, it immediately returns from the enclosing function with that error. Before returning, it performs an implicit conversion using `From`/`Into`: the error `e` is converted into the function's return error type via `From<E>::from(e)` (so differing error types are adapted). On `Option`, `?` returns early on `None` (and requires the function to return `Option`, or there is a `From` conversion for `NoneError` in legacy contexts). It's essentially a short-circuiting early-return with automatic `From` coercion.

- id: rust-error-03
  answer: |
    `panic!` / `.unwrap()` / `.expect()` are appropriate when the situation is truly unrecoverable: a violated invariant, a logic/programmer error ("this can't happen if the code is correct"), or a configuration that the program cannot meaningfully continue without. Examples: indexing an array the code has already proven in-bounds, or an internal invariant that should be mathematically impossible to break.
    Return a `Result` when failure is an *expected, recoverable* part of normal operation that the caller should decide how to handle — I/O, parsing user input, network, resource exhaustion. Libraries should almost always return `Result` rather than panicking on input that callers could legitimately encounter.

- id: rust-error-04
  answer: |
    A unified library error type needs to: (1) implement `std::error::Error` (which requires `Debug` and `Display`), (2) implement `Display` (and typically `Debug`), and (3) implement `From<E>` for each underlying error type `E` you want to wrap, so that `?` can automatically convert those into your error type. Often it also wraps an underlying source error via `source()`.
    Crates like `thiserror` provide a derive macro (`#[derive(thiserror::Error)]`) that automatically generates the `Display` impl (from `#[error("...")]` format strings), the `Error` impl, and the `From` conversions (via `#[from]` attributes), eliminating the boilerplate of writing these by hand.

- id: rust-iterators-01
  answer: |
    Because Rust iterators are *lazy*: `map` and `filter` are *adapter* methods that don't process elements — they just return new iterator structs wrapping the previous one, recording what to do later. No real work happens until the iterator is *consumed*.
    What makes it run is a consuming/terminal method that pulls items through the chain: `collect()`, `for` loops, `sum()`, `count()`, `fold()`, `any()`, etc. Only then does the iterator actually iterate and apply the closures. (Note: adapters are also typically fused so only one pass occurs when driven.)

- id: rust-iterators-02
  answer: |
    - `iter()` borrows the collection and yields items of type `&T` (shared references). The collection is left intact and still owned by the caller.
    - `iter_mut()` borrows the collection mutably and yields `&mut T`, letting you modify elements in place. The collection remains owned by the caller.
    - `into_iter()` consumes the collection by value (takes ownership of it) and yields `T` (owned items), moving each element out. Afterward the collection can no longer be used because it has been moved/consumed. (`iter`/`iter_mut` also differ in that for `into_iter` the collection itself must be owned/movable at the call site.)

- id: rust-iterators-03
  answer: |
    `collect()` is generic over its output type via `FromIterator`; the target type isn't determined by the input alone. Without a hint the compiler can't infer what you're building (a `Vec`? a `HashMap`? a `String`?), so it errors with "type annotations needed." You resolve it by annotating the destination: `let v: Vec<_> = iter.collect();` or with a turbofish `iter.collect::<Vec<_>>()`.
    Special case: `collect()` can also build `Result<Vec<T>, E>` (or `Option<Vec<T>>`) from an iterator of `Result`s/`Options`. In that mode it accumulates the `Ok`/`Some` values into the `Vec`, but short-circuits and returns the first `Err`/`None` it encounters — turning "iterator of results" into "result of vector" (this is implemented via `FromIterator` for `Result`/`Option`).

- id: rust-iterators-04
  answer: |
    By default a closure borrows the variables it captures. When you return the closure (or otherwise let it outlive the current scope) while it borrows a local, the borrow wouldn't satisfy the required lifetime, so the compiler suggests `move`. Adding `move` to the closure makes it *take ownership* of the captured variables (move them into the closure's environment) instead of borrowing. This lets the closure own its captured data and be returned/sent elsewhere independent of the original scope. (It can also force moving when you want the closure to be `FnOnce` or to transfer ownership of moved values.)

- id: rust-smartptr-01
  answer: |
    `Box<T>` is Rust's simplest smart pointer: a heap-allocated, owned pointer with single ownership and (aside from a single pointer indirection) zero runtime overhead compared to a stack value. It owns the `T` and frees it when dropped.
    You genuinely need it when: (1) the type is recursive / self-referential and its size is unknown at compile time (e.g. `enum List { Cons(i32, Box<List>) }`) — you can't embed the type directly without infinite size; (2) you need a trait object (`Box<dyn Trait>`) for dynamic dispatch; (3) you want to move a large value cheaply by pointer, or transfer ownership across boundaries while erasing the concrete type.

- id: rust-smartptr-02
  answer: |
    Both provide reference-counted *shared ownership*. `Rc<T>` uses a non-atomic (plain integer) reference count and is not thread-safe — it is `!Send`/`!Sync` and can only be used within a single thread. `Arc<T>` uses an *atomic* reference count, making it thread-safe (`Send` + `Sync`) and safe to share across threads, at the cost of atomic operations.
    You shouldn't always use `Arc` because the atomic increments/decrements add real overhead on every clone/drop, and in single-threaded code that cost is pure waste. Use `Rc` when sharing is confined to one thread, `Arc` only when the data must cross thread boundaries.

- id: rust-smartptr-03
  answer: |
    Interior mutability is the pattern of being able to mutate data through an *immutable* reference (`&T`), which normally Rust forbids. It works by moving the borrow rules' enforcement from compile time to runtime.
    `RefCell<T>` provides this: you call `.borrow()` to get a `Ref<T>` (shared) or `.borrow_mut()` to get a `RefMut<T>` (exclusive). At runtime it checks the borrow rules; if you violate them (e.g. a mutable borrow while another borrow is active) it *panics* rather than refusing to compile. The cost vs a normal `&mut` is a small runtime bookkeeping cost (tracking the borrow count) and the possibility of runtime panics instead of compile-time guarantees; `RefCell` is also single-threaded (`!Sync`).

- id: rust-smartptr-04
  answer: |
    `Rc<RefCell<T>>` is the idiom for shared mutable state in single-threaded code because `Rc` gives you multiple owners (shared ownership) while `RefCell` gives you mutability through those shared, immutable-looking `Rc` handles (interior mutability). Together they let many parts of the program hold and mutate the same value without the compile-time "only one `&mut`" rule blocking you — the rules are checked at runtime.
    The multi-threaded equivalent is `Arc<Mutex<T>>` (or `Arc<RwLock<T>>` when you want multiple concurrent readers): `Arc` for thread-safe shared ownership, `Mutex` for exclusive interior mutability guarded by a lock (with `RwLock` allowing concurrent reads).

- id: rust-concurrency-01
  answer: |
    `Send` is an auto trait marking types whose ownership can be safely *transferred* to another thread (moved across thread boundaries). `Sync` marks types that can be safely *shared* between threads via a reference — formally, `&T` is `Send` iff `T: Sync` (a type is `Sync` if it's safe to have a `&T` in multiple threads simultaneously).
    Rust uses these as compile-time gates: APIs that spawn threads / send work between threads (e.g. `thread::spawn`, `std::sync::mpsc::Sender::send`, `tokio::spawn`) require the moved values to be `Send` (and shared references to be `Sync`). Because types that aren't thread-safe simply don't implement `Send`/`Sync` (e.g. `Rc` is `!Send`), the compiler rejects moving/aliasing them across threads, turning data races into compile errors rather than runtime bugs. (The rules are enforced at compile time; the actual synchronization is done by primitives like `Mutex`.)

- id: rust-async-01
  answer: |
    Calling an `async fn` does *not* run its body. It immediately returns a `Future` — a state machine value representing the paused/deferred computation, including its captured state at the await points. The future does nothing until it is driven.
    To actually execute it you need a *runtime/executor* (e.g. tokio, async-std) that repeatedly *polls* the future (advancing it through its `.await` points) until it completes. Practically you do this by `.await`-ing it within an async context (another future on the executor) or by spawning it onto the runtime (e.g. `tokio::spawn`). The executor's event loop is what turns the future into real progress.

- id: rust-async-02
  answer: |
    `tokio::spawn` (and most spawn APIs) require the spawned future to be `Send`, because the runtime may move the future between worker threads between `.await` points. A `Rc` is `!Send`, so a future that holds an `Rc` across an `.await` is `!Send` and won't compile. Similarly, holding a `MutexGuard` across an `.await` makes the future non-`Send` (the guard isn't `Send`, and holding a lock across an await is also a deadlock risk), and even where the type is `Send`, holding a guard/general borrow across await is unsound because the lock could be held while the task is parked. The rule of thumb: don't hold non-`Send` types or locks across an `.await`; drop them first.

- id: rust-async-03
  answer: |
    Blocking calls (e.g. `std::thread::sleep`, synchronous file/network I/O, heavy CPU loops) inside an async task run on the executor's worker thread and do not yield back to the runtime. Because async executors are cooperative (a task runs until it `.await`s), a blocking call occupies that thread for its whole duration, starving all other tasks scheduled on the same thread and destroying the concurrency the runtime provides.
    Instead: for blocking I/O/CPU work, use `tokio::task::spawn_blocking` (or `block_in_place` with the right runtime) to run it on a dedicated blocking thread pool; for sleeps use the async `tokio::time::sleep`; for CPU-bound work, offload to a thread pool / `spawn_blocking` or a separate thread. Use async equivalents of I/O wherever available.

- id: rust-match-01
  answer: |
    "Exhaustive" means a `match` must have an arm covering *every* possible value of the matched expression; the compiler rejects it otherwise (you typically add a `_` wildcard or cover all cases). This is especially valuable for enums because an enum's set of variants is known to the compiler — if you later add a new variant, every non-exhaustive `match` becomes a compile error, forcing you to consciously handle the new case everywhere. This makes refactoring safe and prevents the "forgot to handle this case" class of bugs that in other languages surface only at runtime.

- id: rust-match-02
  answer: |
    - `if let`: use when you care about a single pattern and want to ignore/do nothing for all other cases, concisely (e.g. `if let Some(x) = opt { .. }`). It's a less-verbose alternative to a one-arm `match`.
    - `let ... else` (let-else): use when you want to bind a value by pattern and, if it *doesn't* match, run fallback code that diverges (returns, breaks, panics, etc.) — good for early returns / guard clauses: `let Some(x) = opt else { return; };`.
    - Full `match`: use when you need to handle *multiple* patterns or require exhaustiveness (all cases, e.g. an enum), so the compiler guarantees completeness and you get a value from each arm.

- id: rust-match-03
  answer: |
    In `match &opt { Some(x) => ... }`, `x` has type `&T` (a reference to the inner value), not `T`. This is due to *match ergonomics* (default binding modes, RFC 2005): when you match a reference with a pattern that isn't itself a reference, the compiler automatically adjusts the binding mode to "ref", so bindings introduced by the pattern become references that reborrow the matched data rather than moving/copying it out. (So matching `&Option<T>` with `Some(x)` gives `x: &T`; matching `&mut` gives `&mut T`.)

- id: rust-match-04
  answer: |
    - Destructuring: patterns can pull a value apart and bind its inner fields directly, e.g. `match p { Point { x, y } => ... }`, `(a, b) => ...`, or `Some(inner) => ...` — you bind sub-components instead of the whole.
    - Match guards: an extra `if` condition after a pattern, allowing finer filtering without committing to a separate arm: `match opt { Some(x) if x > 0 => ..., Some(_) => ... }`. Guards can reference bound variables and are evaluated only when the pattern matches.
    - `@` bindings: `@` binds the *entire* matched value (or sub-pattern) to a name while still testing a pattern, e.g. `match n { x @ 1..=10 => println!("{x} is small"), _ => ... }` — here `x` is bound to the whole value that matched the range, combining test and capture in one arm.
