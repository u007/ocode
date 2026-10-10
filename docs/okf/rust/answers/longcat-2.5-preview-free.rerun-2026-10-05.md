- id: rust-ownership-01
  answer: |
    When you write `let b = a;` where `a` is a `String`, the ownership of the heap-allocated string data is moved from `a` to `b`. The variable `a` is no longer valid — using it afterward produces a compile-time error ("use of moved value"). This prevents double-free bugs: only `b` now owns the data and will free it when it goes out of scope.

- id: rust-ownership-02
  answer: |
    `Copy` is a marker trait that indicates a type can be duplicated by a simple bitwise copy — the original remains valid after assignment. It is implicit and happens automatically. `Clone` is an explicit trait with a `.clone()` method that performs a deep copy; the original remains valid but you must call the method. A type that implements `Drop` cannot also be `Copy` because `Copy` semantics mean the old value is still considered valid after a copy, which would cause the `Drop` implementation to run twice on the same resource (double-free).

- id: rust-ownership-03
  answer: |
    Passing a `Vec<T>` by value moves ownership into the function. The caller no longer owns the `Vec` and cannot use it afterward. Options: (1) pass by reference (`&Vec<T>` or `&[T]`) to borrow it, (2) pass a mutable reference (`&mut Vec<T>`) if the function needs to modify it, (3) clone the `Vec` before passing so the caller retains its own copy, or (4) have the function return the `Vec` back to the caller.

- id: rust-borrowing-01
  answer: |
    Rust's core borrowing rule: at any given time, you can have either any number of immutable references (`&T`) to a value OR exactly one mutable reference (`&mut T`), but never both simultaneously. This prevents data races (concurrent read/write or write/write access) and use-after-free bugs at compile time, because the compiler statically enforces that no reference can outlive the data it points to and that aliasing and mutation cannot coexist.

- id: rust-lifetimes-01
  answer: |
    A lifetime annotation like `'a` expresses a relationship between references — it states that the referenced data lives at least as long as the lifetime `'a`. It does NOT change how long a value actually lives; it is purely a compile-time constraint that the borrow checker uses to verify that no reference outlives the data it points to. Lifetimes describe scopes, not durations.

- id: rust-lifetimes-02
  answer: |
    The three elision rules: (1) Each elided lifetime in the input (parameters) becomes a distinct lifetime parameter. (2) If there is exactly one input lifetime, that lifetime is assigned to all elided output lifetimes. (3) If there are multiple input lifetimes but one of them is `&self` or `&mut self`, the lifetime of `self` is assigned to all elided output lifetimes. In `fn first(s: &str) -> &str`, rule 2 applies: the single input lifetime is assigned to the output.

- id: rust-lifetimes-03
  answer: |
    `&'static T` is a reference that is valid for the entire duration of the program — the data it points to will never be dropped (e.g., string literals in the binary). `T: 'static` as a bound means the type `T` contains no non-static references — it either owns all its data or only contains references that are themselves `'static`. The first is about a specific reference's validity; the second is a constraint on what a type may contain.

- id: rust-lifetimes-04
  answer: |
    `Box<dyn Error>` defaults to `Box<dyn Error + 'static>`, meaning the trait object must contain only `'static` data. If you want to store a trait object that borrows data with a shorter lifetime `'a`, you must write `Box<dyn Error + 'a>`. The `+ 'a` explicitly relaxes the `'static` bound to allow the trait object to hold references with lifetime `'a`.

- id: rust-traits-01
  answer: |
    Static dispatch via generics (`fn f<T: Trait>(x: T)`) is monomorphized — the compiler generates a separate concrete function for each type used. This enables inlining and is faster, but causes code bloat (larger binary). Dynamic dispatch via trait objects (`&dyn Trait`) uses a vtable to look up method calls at runtime. This has a small runtime overhead (indirection, no inlining) but produces smaller code and allows heterogeneous collections.

- id: rust-traits-02
  answer: |
    In argument position, `fn f(x: impl Trait)` is syntactic sugar for a generic `fn f<T: Trait>(x: T)` — the caller chooses the concrete type at the call site. In return position, `fn f() -> impl Trait` means the function chooses the concrete type it returns, and the caller can only use the trait's interface — the caller does not know (and cannot name) the concrete type.

- id: rust-traits-03
  answer: |
    The orphan rule (coherence) states that you can only implement a trait for a type if either the trait or the type is defined in your own crate. You cannot `impl Display for Vec<T>` in your crate because both `Display` (from std) and `Vec<T>` (from std) are foreign. This prevents conflicting implementations across crates — if two crates could both implement a foreign trait for a foreign type, there would be no way to choose between them.

- id: rust-error-01
  answer: |
    Use `Option<T>` when a value may be absent and there is no meaningful error to report — e.g., looking up a key that may not exist. Use `Result<T, E>` when an operation can fail and the caller needs to know why — e.g., parsing input, I/O, or network requests. `Option` signals "there might be nothing"; `Result` signals "something went wrong, and here's the error."

- id: rust-error-02
  answer: |
    The `?` operator on a `Result<T, E>` does: if the value is `Ok(v)`, it unwraps and yields `v`. If the value is `Err(e)`, it early-returns from the function with `Err(From::from(e))` — it converts the error `e` into the function's return error type `E'` via the `From` trait. This allows automatic error type conversion when the function returns `Result<T, E'>` and `E: From<E'>`.

- id: rust-error-03
  answer: |
    Use `panic!` (or `.unwrap()`) for unrecoverable invariant violations, programming errors, or situations where continuing would be incorrect — e.g., unreachable code, broken internal contracts, or prototype code. Use `Result` for expected or recoverable failures that the caller should handle — e.g., user input validation, file not found, network errors. In libraries, prefer `Result`; reserve `panic!` for true bugs.

- id: rust-error-04
  answer: |
    The error type needs to implement the `std::error::Error` trait, which requires `Debug` and `Display`. It should also implement `From<E>` for each underlying error type so that `?` can convert them. The `source()` method (from `Error`) enables error chaining. Crates like `thiserror` generate this boilerplate via derive macros — `#[derive(Error)]` with `#[from]` and `#[source]` attributes — reducing manual impl code and ensuring correct trait implementations.

- id: rust-iterators-01
  answer: |
    Iterators in Rust are lazy — adapters like `map` and `filter` build up a chain of transformations but do not execute any of them until a consuming adapter is called. To make it run, you need a terminal operation such as `collect()`, `for_each()`, `sum()`, `count()`, or a `for` loop. Only then does the iterator pull items through the chain and execute the closures.

- id: rust-iterators-02
  answer: |
    `iter()` yields immutable references `&T` — the collection is borrowed and remains usable. `iter_mut()` yields mutable references `&mut T` — the collection is mutably borrowed and remains usable after. `into_iter()` yields owned values `T` — it consumes (moves) the collection, which cannot be used afterward unless the iterator is returned or the collection implements `IntoIterator` for references.

- id: rust-iterators-03
  answer: |
    `collect()` is generic over the target collection type, so the compiler often cannot infer what you want to build — you get "type annotations needed." Fix it with a type annotation (`let v: Vec<_> = ...`) or turbofish (`collect::<Vec<_>>()`). Collecting into a `Result` is special: it short-circuits on the first `Err`, returning that error immediately, and only returns `Ok(collection)` if all items are `Ok`.

- id: rust-iterators-04
  answer: |
    The `move` keyword forces the closure to take ownership of all variables it captures from the surrounding environment, rather than borrowing them. This is necessary when the closure (or iterator) must outlive the scope where those variables are defined — e.g., returning a closure from a function or spawning a thread. Without `move`, the closure would hold references to locals that would be dropped, causing a lifetime error.

- id: rust-smartptr-01
  answer: |
    `Box<T>` provides heap allocation with single ownership — it is a smart pointer that owns a value on the heap and frees it when dropped. You genuinely need it for: (1) recursive types (e.g., a linked list or tree node that contains itself), where the size would be infinite without indirection; (2) trait objects (`Box<dyn Trait>`) where the concrete type is unknown at compile time; (3) very large values you want on the heap to avoid stack overflow; (4) transferring ownership of a large value without copying it.

- id: rust-smartptr-02
  answer: |
    `Rc<T>` uses a non-atomic reference count and is restricted to single-threaded use — it is not `Send` or `Sync`. `Arc<T>` uses an atomic reference count, making it thread-safe (`Send + Sync`), but atomic operations have a performance cost. You should not always use `Arc` because the atomic overhead is unnecessary in single-threaded code, and `Rc` is cheaper and communicates the single-threaded intent.

- id: rust-smartptr-03
  answer: |
    Interior mutability is the ability to mutate data through a shared (immutable) reference, bypassing the usual rule that mutation requires `&mut`. `RefCell<T>` provides this by performing borrow checking at runtime instead of compile time: `borrow()` returns a `Ref` (immutable guard) and `borrow_mut()` returns a `RefMut` (mutable guard). The cost is runtime overhead (tracking active borrows) and the possibility of a runtime panic if you violate the borrowing rules (e.g., calling `borrow_mut` while a `Ref` is active).

- id: rust-smartptr-04
  answer: |
    `Rc<RefCell<T>>` combines shared ownership (`Rc` allows multiple owners) with interior mutability (`RefCell` allows mutation through shared references), making it the standard idiom for shared mutable state in single-threaded code. The multi-threaded equivalent is `Arc<Mutex<T>>` (or `Arc<RwLock<T>>`): `Arc` provides thread-safe shared ownership, and `Mutex` provides thread-safe interior mutability with locking.

- id: rust-concurrency-01
  answer: |
    `Send` is a marker trait indicating a type can be safely transferred (moved) to another thread — ownership of the value crosses thread boundaries. `Sync` is a marker trait indicating a type can be safely shared between threads — `&T` is `Send` when `T: Sync`. Rust uses these traits at compile time: `tokio::spawn` and similar APIs require the future to be `Send`, and sharing data across threads requires `Sync`. This makes data races a compile error rather than a runtime bug.

- id: rust-async-01
  answer: |
    Calling an `async fn` returns a `Future` — a state machine that represents the computation but does not execute it. To make it run, you need an executor (like Tokio, async-std, or smol) that polls the future. Within another async context, you can also `.await` the future, which polls it as part of the current task. Without an executor or `.await`, the future is never polled and the code inside never runs.

- id: rust-async-02
  answer: |
    `Rc` is not `Send` (it uses non-atomic reference counting, which is unsafe to transfer across threads). When you hold an `Rc` (or a `MutexGuard`, which is also not `Send`) across an `.await`, the future itself becomes non-`Send`. `tokio::spawn` requires the future to be `Send` because the runtime may move tasks between threads. The fix is to use `Arc` instead of `Rc`, and to scope the `MutexGuard` so it is dropped before the `.await`.

- id: rust-async-03
  answer: |
    Blocking operations (like `std::thread::sleep` or heavy CPU work) inside an async task block the executor's worker thread, preventing it from polling other tasks. This can starve the runtime, cause latency spikes, and reduce throughput. Instead, use `tokio::task::spawn_blocking` for blocking I/O or CPU-intensive work (which runs on a dedicated blocking thread pool), or use async equivalents like `tokio::time::sleep`. For CPU-bound work, consider `tokio::task::spawn_blocking` or a separate thread pool.

- id: rust-match-01
  answer: |
    `match` must be exhaustive — every possible value of the matched type must be covered by at least one arm. For enums, this means every variant must be handled. This is especially valuable because adding a new variant to an enum will cause a compile error at every `match` site that doesn't handle it, forcing the programmer to consciously decide what to do for the new case. This prevents forgotten cases and makes refactoring safer.

- id: rust-match-02
  answer: |
    Use `if let` when you care about exactly one pattern and want to execute code only for that case (with an optional `else`). Use `let ... else` when you want to early-return (or break/continue) if a pattern doesn't match — the else block must diverge. Use a full `match` when you need to handle multiple patterns, need exhaustiveness checking, or have complex multi-pattern logic.

- id: rust-match-03
  answer: |
    When you match on a reference like `match &opt { Some(x) => ... }`, the type of `x` is `&T` (a reference to the inner value), not `T`. This is caused by "match ergonomics" (default binding modes), a Rust 2018 feature: when matching on a reference, the compiler automatically dereferences and binds inner values as references, so you don't need to write `Some(ref x)` or `&` patterns manually.

- id: rust-match-04
  answer: |
    Destructuring breaks apart compound types (structs, enums, tuples, slices) into their constituent parts in a pattern — e.g., `Point { x, y }` binds `x` and `y`. Match guards add a boolean condition to an arm with `if` — e.g., `Some(x) if x > 0 => ...` — allowing patterns to be filtered by arbitrary logic. `@` bindings bind a variable to the whole value while also matching a sub-pattern — e.g., `n @ 1..=5 => ...` binds `n` to the matched value and also checks it is in the range 1–5.
