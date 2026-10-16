# nvidia/nemotron-3.5-super-vl — with-skill answers for rust (openrouter, 2026-10-16); answerer saw only _prompts/rust.md; derived/rust.nvidia__nemotron-3.5-super-vl.SKILL.md body (front matter stripped) prepended to the sheet as active guidance
- id: rust-ownership-01
  answer: |
    `let b = a;` moves the `String` into `b`. After that, `a` is no longer valid and using it is a compile error ("value used here after move"). A `String` is a small stack struct (pointer, length, capacity) owning heap-allocated data. A plain bitwise copy would leave two variables owning the same heap buffer, leading to a double-free when both are dropped. Rust instead transfers ownership: `b` takes the heap data and `a` is invalidated.
- id: rust-ownership-02
  answer: |
    `Copy` is a marker trait: values of `Copy` types are duplicated bitwise on assignment or argument passing, and the original remains usable (`let b = a;` keeps `a` valid). `Clone` is an explicit trait with a `clone(&self) -> Self` method that you call (`a.clone()`); it can allocate, run arbitrary code, and may be infallible. `Copy` implies `Clone`. A type implementing `Drop` cannot be `Copy` because `Drop` runs when each owner goes out of scope; bitwise copying would create two owners, both expecting to run the destructor on the same resource (double-free, double-close, etc.).
- id: rust-ownership-03
  answer: |
    Passing by value moves the `Vec<T>` into the function: ownership transfers, the callee becomes the owner and drops it on return, so the caller can no longer use the variable (compile error if it tries). Options: pass by reference (`&Vec<T>` or `&[T]` for read-only use, `&mut Vec<T>` if the callee needs to mutate), `clone()` the vector before passing if the callee needs its own copy, have the function return the `Vec<T>` so ownership comes back, or pass a slice/borrowed subpart instead of the whole collection.
- id: rust-borrowing-01
  answer: |
    The core borrowing rule: at any given time, you may have either exactly one mutable reference (`&mut T`) or any number of immutable references (`&T`) to a piece of data — never both simultaneously — and every reference must be valid for the lifetime of the data it points to. This prevents dangling pointers/use-after-free and data races at compile time: multiple immutable readers can't conflict, and a mutable borrow is exclusive so no one else can read or write while it's active.
- id: rust-lifetimes-01
  answer: |
    A lifetime annotation like `'a` expresses a constraint about how long a reference is valid relative to other lifetimes — it says the borrow lasts at least for `'a`. It is a relationship between regions of code, not a directive to the allocator. It does not change how long a value lives; values are dropped based on scope and ownership. Annotations only restrict what the compiler will accept, and if they contradict reality the code fails to compile.
- id: rust-lifetimes-02
  answer: |
    Lifetime elision rules: (1) each elided input parameter gets its own distinct lifetime parameter; (2) if there is exactly one input lifetime (elided or explicit), all elided output lifetimes are assigned that lifetime; (3) if there are multiple input lifetimes but one is `&self` or `&mut self`, the elided output lifetime is that of `self`; (4) otherwise eliding an output lifetime is an error. In `fn first(s: &str) -> &str`, there is one input lifetime, so rule 2 ties the output to it, meaning the returned reference is valid for as long as the input `s` lives — no explicit annotation needed.
- id: rust-lifetimes-03
  answer: |
    `&'static T` means a reference that is valid for the entire program lifetime; it points to data that never gets dropped, such as a string literal or a `leak`ed value. `T: 'static` is a trait bound on a type meaning `T` contains no borrowed data with a non-static lifetime — everything it holds is owned or itself `'static`, so a value of type `T` can outlive any scope. The first is about how long a *reference* lasts; the second is a constraint on what a *type* may contain.
- id: rust-lifetimes-04
  answer: |
    `Box<dyn Error>` carries an implicit lifetime bound: it means `Box<dyn Error + 'static>`, because trait objects default to `'static` when no borrowed references appear inside them. You need `+ 'a` (e.g., `Box<dyn Error + 'a>` or `&dyn Trait + 'a`) when the trait object contains a reference with a non-static lifetime `'a` — for instance `dyn Error + 'a` where the error wraps `&'a str` — so the object's lifetime is tied to the borrowed data and cannot outlive it.
- id: rust-traits-01
  answer: |
    Static dispatch via generics (`fn f<T: Trait>`) monomorphizes: the compiler generates a copy of the function for each concrete `T`. Calls are direct, inlining is possible, and there is zero runtime cost, at the price of code bloat (larger binaries) and all concrete types must be known at compile time. Dynamic dispatch via `&dyn Trait` uses a vtable: one shared function body, calls go through an indirect pointer, and heterogeneous collections of different implementing types become possible, at the cost of one pointer indirection per call, lost inlining, and the requirement that the trait be object-safe. Tradeoff: monomorphization for speed/compile-time knowledge vs. `dyn` for runtime flexibility and smaller binaries.
- id: rust-traits-02
  answer: |
    `impl Trait` in argument position is sugar for an anonymous generic parameter: the caller chooses the concrete type (any type implementing the trait), and the function must work with all of them. `impl Trait` in return position means the function returns some specific type implementing the trait, but hides it: the caller/callee boundary is reversed — the function chooses the concrete type, and the caller only knows it implements the trait. So argument position: caller picks; return position: callee picks.
- id: rust-traits-03
  answer: |
    The orphan rule (coherence) says you may implement `Trait for Type` only if at least one of the trait or the type is local to your crate. You cannot `impl Display for Vec<T>` because both `Display` and `Vec` come from `std` — neither is local — so allowing it would let two crates implement the same trait for the same type, breaking coherence. The standard workaround is the newtype pattern: wrap `Vec<T>` in a local struct and implement `Display` for that wrapper.
- id: rust-error-01
  answer: |
    Return `Option<T>` when the absence of a value is a normal, expected outcome (e.g., a key not found in a map, an optional field) and no extra information about why it's missing is needed. Return `Result<T, E>` when an operation can fail and the caller needs to know why — the `E` carries error context. `Option` models "maybe nothing"; `Result` models "success or failure with a reason."
- id: rust-error-02
  answer: |
    `?` unwraps a `Result<T, E>`: on `Ok(v)` it evaluates to `v` and execution continues; on `Err(e)` it returns early from the enclosing function with `Err(From::from(e))`. The key conversion is `From`: the error `e` is converted into the function's own error type via `From::from` (or `Into`), so `?` works only when the underlying error type implements `Into<TheFunctionErrorType>`. It also works on `Option<T>` (returns early with `None`) but with no error conversion.
- id: rust-error-03
  answer: |
    Use `panic!` / `.unwrap()` for genuinely unrecoverable situations: violated internal invariants, programmer errors, unreachable code paths, or quick prototypes/tests/examples where failure handling is out of scope. It aborts the thread (or program on the main thread), so it is unsuitable for expected runtime failures. Return `Result` for recoverable, expected failures — I/O, parsing, network, validation — especially in library code, where the caller, not the library, should decide whether to retry, log, or abort.
- id: rust-error-04
  answer: |
    The error type needs `impl std::fmt::Display` (user-facing message) and `impl std::error::Error` (so it can be boxed as `dyn Error` and chained via `source()`), plus `From<UnderlyingError>` implementations so `?` can convert each underlying error into your type. `thiserror` helps by deriving `Display`/`Error` from `#[error("...")]` messages on an enum and generating `From` impls with `#[from]` attributes, so a single enum can wrap several underlying errors and work naturally with `?`. (For applications, `anyhow` offers a similar ergonomic wrapper without requiring a custom error type.)
- id: rust-iterators-01
  answer: |
    Iterators are lazy: `map` and `filter` return adaptor values that merely remember the computation (the closure and the source iterator). Nothing is transformed or filtered until something calls `next()`. A consumer — `for` loops, `collect`, `sum`, `find`, `for_each`, etc. — drives the iterator by repeatedly calling `next()` until it returns `None`, and only then does the work run.
- id: rust-iterators-02
  answer: |
    `iter()` borrows the collection and yields `&T` (immutable references); the collection is not consumed and stays usable. `iter_mut()` borrows mutably and yields `&mut T`; again the collection is not consumed. `into_iter()` consumes the collection (for `Vec<T>` it yields owned `T` values), and after the call the collection has been moved and can no longer be used.
- id: rust-iterators-03
  answer: |
    `collect()` is generic over `FromIterator`, so the compiler cannot always infer what collection type to build ("type annotations needed") when nothing downstream pins the type down. You tell it with a type annotation (`let v: Vec<_> = ...`) or the turbofish (`.collect::<Vec<_>>()`). Collecting into a `Result<V, E>` is special: the `FromIterator` implementation for `Result` short-circuits — as soon as an `Err` is encountered, collection stops and that error is returned; otherwise it collects all the `Ok` values.
- id: rust-iterators-04
  answer: |
    `move` makes the closure take ownership of every variable it captures, moving or copying them into the closure's environment, instead of borrowing them. Without `move` the closure borrows locals, which dangles if the closure (or the iterator built from it) is returned from the function because the locals are dropped at the end of the scope. `move` lets the closure own its captures and outlive the function that created it.
- id: rust-smartptr-01
  answer: |
    `Box<T>` allocates `T` on the heap and stores a single owning pointer on the stack; the pointer size is known at compile time regardless of `T`. You genuinely need it for recursive types whose size isn't known at compile time (e.g., a `Cons` list where `enum List { Cons(i32, Box<List>), Nil }`), for trait objects (`Box<dyn Trait>`), to move large values without copying their contents, and to transfer ownership of heap data across contexts.
- id: rust-smartptr-02
  answer: |
    `Rc<T>` is reference-counted shared ownership for single-threaded code; it is not `Send` or `Sync` and uses cheap non-atomic reference counts. `Arc<T>` (atomic reference counting) is the thread-safe counterpart: it is `Send`/`Sync` and can be shared across threads, but its ref-count operations are atomic and therefore slower. You should not always use `Arc` because in single-threaded code the atomics are unnecessary overhead; `Rc` is faster and communicates that the value stays on one thread.
- id: rust-smartptr-03
  answer: |
    Interior mutability is the ability to mutate data through a shared (immutable) reference `&T`, where the borrow checker's usual rule of "one `&mut` or many `&`" would otherwise forbid it. `RefCell<T>` enforces Rust's borrowing rules at runtime instead of compile time: `borrow()` and `borrow_mut()` track active borrows and panic if the rules are violated. Compared to a normal `&mut`, the cost is runtime borrow-check overhead, the possibility of a runtime panic on rule violation, and no thread safety (`RefCell` is not `Sync`).
- id: rust-smartptr-04
  answer: |
    `Rc<RefCell<T>>` combines shared ownership (`Rc` lets multiple owners point to the same heap data) with interior mutability (`RefCell` lets you mutate through shared references), giving shared mutable state in single-threaded code. The multi-threaded equivalent is `Arc<Mutex<T>>` (shared ownership plus mutual exclusion across threads) or `Arc<RwLock<T>>` when many readers / few writers is the pattern.
- id: rust-concurrency-01
  answer: |
    `Send` means it is safe to transfer ownership of a value to another thread. `Sync` means it is safe to share a reference to the value between threads — equivalently, `&T` is `Send`. Both are auto traits: the compiler implements them automatically based on a type's composition. Rust uses them to make data races a compile error: spawning a thread or sending across a channel requires `Send`, so non-`Send` types like `Rc` cannot cross thread boundaries. Since shared mutation across threads requires `Sync` types (e.g., `Mutex`, `Arc<Mutex<T>>`) which enforce exclusive access, unsynchronized concurrent mutation cannot compile.
- id: rust-async-01
  answer: |
    Calling an `async fn` does not execute its body; it returns a lazy `Future` (an `impl Future<Output = T>`) representing work that may be done later. To make it actually run, something must poll the future to completion: `.await` inside another async context, or — at the top level — an executor/runtime such as Tokio (`#[tokio::main]`, `Runtime::block_on`), async-std, smol, or `futures::executor::block_on`. `std` provides the `Future`/`Poll`/`Context`/`Waker` machinery but ships no executor, so a third-party runtime is mandatory to drive a top-level future.
- id: rust-async-02
  answer: |
    An `async fn`/block is lowered into a state machine, and every value still alive across an `.await` becomes a field of that generated future so it can be restored when polling resumes. `Rc` (and `std::sync::MutexGuard`) are `!Send`, so holding one across an `.await` makes the entire future `!Send`. `tokio::spawn` requires `Future + Send + 'static` because the task may suspend and later resume on a different worker thread; the bound that fails is `Send`, not `'static` (`Rc<T: 'static` does satisfy `'static`). The error is sometimes misread as a lifetime problem — `'static` only fails when the future borrows a local by reference. Fixes: drop or scope the value before the `.await`, switch to `Arc` / `tokio::sync::Mutex`, or run it with `spawn_local` on a `LocalSet` (single-threaded).
- id: rust-async-03
  answer: |
    Async tasks are cooperatively scheduled: a task yields control back to the executor only when an `.await` returns `Poll::Pending`. A blocking call like `std::thread::sleep` or heavy CPU work contains no such yield point, so it holds the worker thread until it returns — starving every other task scheduled on that worker, and stalling the whole program on a small or single-threaded runtime. The fix is twofold: use async equivalents for waits and I/O (`tokio::time::sleep(...).await`, `tokio::fs`, async clients), and move CPU-bound or unavoidably blocking work to `tokio::task::spawn_blocking` (or a dedicated thread) and `.await` its `JoinHandle`.
- id: rust-match-01
  answer: |
    `match` must be exhaustive: every possible value of the matched type must be covered by some arm, either explicitly or with a catch-all wildcard `_` (patterns that are irrefutable like `x` also cover everything). The compiler rejects non-exhaustive matches. This is especially valuable with enums: if a new variant is added later, every existing `match` on that enum stops compiling until all variants are handled, so missed cases become compile errors rather than runtime bugs, and there is no implicit default that silently swallows new cases.
- id: rust-match-02
  answer: |
    Use `if let` when you only care about one pattern and want a concise single-case branch. Use `let ... else` when a single pattern must match and, if it doesn't, the block should exit early (`return`, `break`, `continue`, `panic`) — it also keeps the bindings in scope for the rest of the block. Use a full `match` when you have multiple cases to handle, when exhaustiveness checking matters, or when the match is an expression producing a value.
- id: rust-match-03
  answer: |
    `x` has type `&T` — a reference to the inner value (e.g., `match &opt { Some(x) => ... }` binds `x: &T` when `opt: Option<T>`). This happens because of match ergonomics (default binding modes): when matching on a reference, the compiler automatically binds by reference rather than attempting to move out of a borrowed value, so no explicit `ref` is needed.
- id: rust-match-04
  answer: |
    Destructuring breaks a value apart into its parts in a pattern: `Some(x)` pulls the inner value out of an `Option`, `Point { x, y }` extracts struct fields, `(a, b)` splits a tuple. Match guards add an extra boolean condition to an arm: `Some(x) if x > 0 => ...` only fires when both the pattern and the guard hold. `@` bindings bind the whole matched value to a name while also matching its structure: `e @ Enum::Variant { .. }` binds `e` to the entire `Enum::Variant` value, and `x @ 1..=5` binds `x` to the matched integer while restricting it to that range.
