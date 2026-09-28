  - id: rust-ownership-01
    answer: |
      Assigning `let b = a;` moves the value from `a` to `b`. After the move, `a` is no longer valid — using it is a compile error ("use of moved value"). This happens because `String` owns heap data and does not implement `Copy`, so Rust invalidates the old binding to prevent a double-free.

  - id: rust-ownership-02
    answer: |
      `Copy` is a marker trait that allows implicit bitwise duplication (the original remains valid after assignment). `Clone` is an explicit trait with a `.clone()` method that performs a deep copy. A type that implements `Drop` cannot also be `Copy` because `Copy` semantics would allow the value to be duplicated after `Drop` has run, leading to a double-free or use-after-free — the two traits have contradictory ownership guarantees.

  - id: rust-ownership-03
    answer: |
      Passing a `Vec<T>` by value transfers ownership into the function; the caller's binding is moved and can no longer be used. Options: (1) pass by reference (`&Vec<T>` or `&[T]`) to borrow it, (2) pass a mutable reference (`&mut Vec<T>`) if the function needs to modify it, (3) clone the Vec before passing, or (4) have the function return the Vec back.

  - id: rust-borrowing-01
    answer: |
      The core rule: at any given time, you can have either one mutable reference (`&mut T`) or any number of immutable references (`&T`) to a value, but not both simultaneously. This prevents data races (concurrent read+write or write+write) and use-after-free bugs at compile time, without needing a garbage collector.

  - id: rust-lifetimes-01
    answer: |
      A lifetime annotation like `'a` expresses a constraint that the referenced data must live at least as long as the annotation requires — it describes a relationship between the lifetimes of references, not how long a value actually lives. It does not extend or shorten any value's actual lifetime; it only tells the compiler what relationships must hold for the code to be sound.

  - id: rust-lifetimes-02
    answer: |
      Three elision rules: (1) Each elided lifetime in the input becomes a distinct lifetime parameter. (2) If there is exactly one input lifetime, that lifetime is assigned to all elided output lifetimes. (3) If there are multiple input lifetimes but one of them is `&self` or `&mut self`, the lifetime of `self` is assigned to all elided output lifetimes. For `fn first(s: &str) -> &str`, rule 2 applies: the single input lifetime is assigned to the output.

  - id: rust-lifetimes-03
    answer: |
      `&'static T` is a reference that is valid for the entire duration of the program (e.g., string literals in the binary). `T: 'static` is a trait bound meaning the type `T` contains no non-static references — it either owns all its data or only contains `&'static` references. The first is about a reference's validity; the second is about a type's contents.

  - id: rust-lifetimes-04
    answer: |
      `Box<dyn Error>` defaults to `Box<dyn Error + 'static>`, meaning the trait object must own all its data (no borrowed references inside). If you need a trait object that contains a borrowed reference, you must specify a lifetime: `Box<dyn Error + 'a>`. The `+ 'a` bound tells the compiler the trait object may contain references valid for `'a`.

  - id: rust-traits-01
    answer: |
      Static dispatch via generics (`fn f<T: Trait>`) is monomorphized — the compiler generates a separate copy of the function for each concrete type, enabling inlining and zero-cost abstraction, but causing code bloat. Dynamic dispatch via trait objects (`&dyn Trait`) uses a vtable for runtime lookup — slight runtime overhead and no inlining, but only one copy of the function exists and you can store heterogeneous types in a collection.

  - id: rust-traits-02
    answer: |
      In argument position, `impl Trait` is syntactic sugar for a generic type parameter — the caller chooses the concrete type at the call site. In return position, `impl Trait` means the function chooses the concrete type, and the caller can only use the trait's interface without knowing the actual type (opaque type). The caller cannot name or depend on the concrete return type.

  - id: rust-traits-03
    answer: |
      The orphan rule (coherence) states you can only implement a trait for a type if either the trait or the type is defined in your crate. You cannot `impl Display for Vec<T>` in your own crate because both `Display` (from std) and `Vec<T>` (from std) are foreign — this prevents two crates from implementing the same trait for the same type and causing ambiguity.

  - id: rust-error-01
    answer: |
      Use `Option<T>` when a value may be absent and there is only one "failure" mode (e.g., a lookup that may not find anything). Use `Result<T, E>` when an operation can fail in multiple ways that carry information about what went wrong (e.g., I/O errors, parse errors). `Option` is for presence/absence; `Result` is for success/failure with an error payload.

  - id: rust-error-02
    answer: |
      The `?` operator on a `Result<T, E>` does: if the value is `Ok(v)`, it unwraps and yields `v`. If it's `Err(e)`, it early-returns from the function with `Err(From::from(e))` — it converts the error `e` into the function's return error type via the `From` trait. This allows automatic error type conversion when the return type implements `From<E>`.

  - id: rust-error-03
    answer: |
      Use `panic!` (or `.unwrap()`) for unrecoverable invariant violations, programmer errors, or situations where continuing would be unsound (e.g., indexing out of bounds in a known-safe context, or a prototype). Use `Result` for expected, recoverable failures that the caller should handle (e.g., file not found, network timeout, invalid user input). In libraries, prefer `Result`; `panic!` is appropriate for bugs, not for expected error conditions.

  - id: rust-error-04
    answer: |
      The error type needs to implement `std::error::Error` (which requires `Debug` + `Display`), and it needs `From<UnderlyingError>` implementations for each underlying error type so that `?` can auto-convert. Crates like `thiserror` generate this boilerplate via derive macros (`#[derive(Error)]`), automatically implementing `Display`, `Error`, and `From` for each variant, reducing manual impl code.

  - id: rust-iterators-01
    answer: |
      Iterators are lazy — `map`, `filter`, and other adapter methods just build up a chain of transformations without executing anything. No work happens until a consuming adapter is called, such as `collect()`, `for_each()`, `count()`, `sum()`, `fold()`, or a `for` loop. The consuming adapter drives the iteration and pulls values through the chain.

  - id: rust-iterators-02
    answer: |
      `iter()` yields immutable references `&T` and borrows the collection immutably. `iter_mut()` yields mutable references `&mut T` and borrows the collection mutably. `into_iter()` yields owned values `T` and consumes (moves) the collection, so the collection cannot be used afterward.

  - id: rust-iterators-03
    answer: |
      `collect()` is generic over its target type — the compiler often cannot infer what collection to build from the iterator alone. You fix it with a type annotation: `let v: Vec<_> = iter.collect()` or `iter.collect::<Vec<_>>()`. Collecting into `Result` is special because `Result` implements `FromIterator` — it short-circuits on the first `Err` and returns `Err(e)`, or collects all `Ok` values into `Ok(collection)`.

  - id: rust-iterators-04
    answer: |
      `move` forces the closure to take ownership of all variables it captures from the surrounding environment, rather than borrowing them. This is needed when the closure (or iterator) must outlive the scope where the captured variables are defined — e.g., returning a closure that uses a local `Vec`. Without `move`, the closure would hold a reference to a dropped variable.

  - id: rust-smartptr-01
    answer: |
      `Box<T>` provides heap allocation with single ownership — it's a smart pointer that owns a value on the heap and frees it when dropped. You genuinely need it for: (1) recursive types (e.g., a linked list or tree where a type contains itself), (2) trait objects (`Box<dyn Trait>`), (3) very large values you want on the heap to avoid stack overflow, or (4) transferring ownership of a dynamically-sized value.

  - id: rust-smartptr-02
    answer: |
      `Rc<T>` uses a non-atomic reference count and is not thread-safe — it's for single-threaded shared ownership. `Arc<T>` uses an atomic reference count, making it safe to share across threads, but with a small performance cost due to atomic operations. You don't always use `Arc` because the atomic overhead is unnecessary in single-threaded code, and `Rc` is cheaper and simpler.

  - id: rust-smartptr-03
    answer: |
      Interior mutability is the ability to mutate data through a shared (immutable) reference, bypassing the normal rule that mutation requires `&mut`. `RefCell<T>` provides this by performing borrow checking at runtime instead of compile time — it tracks active borrows and panics if you try to mutably borrow while another borrow is active. The cost is runtime overhead (borrow tracking) and the possibility of runtime panics instead of compile-time errors.

  - id: rust-smartptr-04
    answer: |
      `Rc<RefCell<T>>` combines shared ownership (`Rc` allows multiple owners) with interior mutability (`RefCell` allows mutation through shared references), making it the standard idiom for shared mutable state in single-threaded code. The multi-threaded equivalent is `Arc<Mutex<T>>` (or `Arc<RwLock<T>>`), where `Arc` provides thread-safe shared ownership and `Mutex` provides thread-safe interior mutability.

  - id: rust-concurrency-01
    answer: |
      `Send` is a marker trait indicating a type's ownership can be safely transferred to another thread. `Sync` indicates that a type can be safely shared between threads via references (i.e., `&T` is `Send`). Rust uses these traits at compile time: `tokio::spawn` and similar APIs require futures to be `Send`, which forces all captured types to be `Send` and `Sync`, making data races a compile error rather than a runtime bug.

  - id: rust-async-01
    answer: |
      Calling an `async fn` returns a `Future` — a state machine that represents the computation but does not execute it. To make it run, you need an executor (like Tokio or async-std) that polls the future. Polling drives the future forward until it completes, at which point the `Future` resolves to its output value.

  - id: rust-async-02
    answer: |
      `Rc` is not `Send` (its reference count is non-atomic, so sharing across threads would be a data race). `MutexGuard` is not `Send` (it holds a lock that must be released on the same thread). `tokio::spawn` requires the future to be `Send` because it may run on a different thread. Holding a non-`Send` type across an `.await` makes the future non-`Send`, so it cannot be spawned.

  - id: rust-async-03
    answer: |
      Blocking operations (like `std::thread::sleep` or heavy CPU work) block the entire async runtime thread, preventing other tasks from making progress on that thread. This can starve the runtime and cause severe latency issues. Instead, use `tokio::task::spawn_blocking` for blocking I/O or CPU-intensive work, or use async equivalents (e.g., `tokio::time::sleep` instead of `std::thread::sleep`).

  - id: rust-match-01
    answer: |
      `match` must be exhaustive — it must cover every possible value of the matched type. For enums, this means every variant must be handled (or a wildcard `_` must be present). This is especially valuable because adding a new variant to an enum will cause a compile error at every `match` site that doesn't handle it, forcing the programmer to consciously decide what to do with the new case.

  - id: rust-match-02
    answer: |
      Use `if let` when you care about exactly one pattern and want to do something only if it matches (with optional `else`). Use `let ... else` when you want to early-return or diverge if the pattern doesn't match (the else block must diverge — `return`, `break`, `panic!`, etc.). Use a full `match` when you need to handle multiple patterns with different actions, or when exhaustiveness checking is desired.

  - id: rust-match-03
    answer: |
      When you `match &opt { Some(x) => ... }`, `x` has type `&T` (a reference to the inner value). This is caused by "match ergonomics" (default binding modes) — Rust automatically dereferences the matched reference and binds inner values as references, so you don't need to write `Some(ref x)` or `&` patterns explicitly.

  - id: rust-match-04
    answer: |
      Destructuring breaks apart compound types (structs, enums, tuples, arrays) into their constituent parts in a pattern — e.g., `Point { x, y }` binds `x` and `y`. Match guards add a boolean condition to an arm with `if` — e.g., `Some(x) if x > 0 => ...` — allowing patterns that can't be expressed structurally. `@` bindings bind the whole value to a variable while also destructuring — e.g., `n @ 1..=5 => ...` binds the matched number to `n` while also checking it's in range.
