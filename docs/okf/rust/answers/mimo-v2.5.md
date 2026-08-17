```yaml
- id: rust-ownership-01
  answer: |
    `String` is not `Copy`, so `let b = a;` *moves* the value from `a` into `b`. After the move, `a` is considered invalid. Using `a` after that produces a compile-time error: "value used after move." This happens because a `String` owns a heap allocation; allowing two owners to manage the same allocation would risk double-free. Rust's move semantics avoid that at zero runtime cost.

- id: rust-ownership-02
  answer: |
    `Copy` means the type is trivially bitwise-duplicable; assignment and passing by value produce an implicit, cheap bit-copy (it's on the stack only). `Clone` means you can explicitly duplicate a value via `.clone()`, which may involve heap allocation or other expensive work. A type with a `Drop` impl cannot be `Copy` because `Copy` implies that a bitwise copy is a perfectly valid independent owner, yet `Drop` would then run twice on the same underlying resource (double free). Rust forbids this statically: `Copy` and `Drop` are mutually exclusive.

- id: rust-ownership-03
  answer: |
    Passing a `Vec<T>` by value moves it into the function. After the call, the original binding is no longer usable. Your options: (1) pass by reference (`&Vec<T>` or `&[T]`) if you only need to read; (2) pass by mutable reference (`&mut Vec<T>`) to mutate; (3) call `.clone()` before passing; (4) take ownership and let the function return the `Vec` if it needs to give it back; (5) pass an iterator or slice instead.

- id: rust-borrowing-01
  answer: |
    The rule: at any given time, you can have *either* one mutable reference (`&mut T`) *or* any number of immutable references (`&T`) to a piece of data — never both simultaneously. This prevents data races and use-after-free at compile time. In particular it eliminates: concurrent mutation + reads (data race / tear), mutation through one alias while another alias is live (iterator invalidation), and undefined behavior from unsynchronized shared mutable state.

- id: rust-lifetimes-01
  answer: |
    A lifetime annotation `'a` is a relationship between references: it tells the compiler that the reference annotated with `'a` will not outlive the data it points to, and that the borrowed data will remain valid for at least as long as `'a`. It does *not* change how long a value lives. The value's actual lifetime is determined by where it was created and where it is dropped; the annotation only constrains what the compiler allows you to *do* with references to that value.

- id: rust-lifetimes-02
  answer: |
    Three lifetime elision rules: (1) Each input reference parameter gets its own lifetime (`fn foo(a: &str, b: &str)` implicitly becomes `fn foo<'a,'b>(a: &'a str, b: &'b str)`). (2) If there is exactly one input lifetime parameter, that lifetime is assigned to all output references (`fn foo(a: &str) -> &str` becomes `fn foo<'a>(a: &'a str) -> &'a str`). (3) If one of the input parameters is `&self` or `&mut self`, the lifetime of `self` is assigned to all output references. Elision only applies to reference types, not generic type parameters.

- id: rust-lifetimes-03
  answer: |
    As a reference bound, `&'static T` means the reference lives for the entire program (e.g. a string literal, leaked memory, or `lazy_static`). The data it points to never goes away. As a trait bound, `T: 'static` means that `T` does not contain any non-`'static` references — it either owns everything or borrows only `'static` data. This is broader: `String` is `'static` (it owns its data), even though a `&String` is not `'static` unless the string itself lives forever. The bound is used in trait objects, `thread::spawn`, and `Any`.

- id: rust-lifetimes-04
  answer: |
    `Box<dyn Error>` desugars to `Box<dyn Error + 'static>`: the trait object must own all its data (no borrowed references with a shorter lifetime). If you need a trait object that borrows data with lifetime `'a`, you must write `Box<dyn Error + 'a>` (e.g. `Box<dyn Error + 'a>`). For `Box<dyn Error + 'a>` to be object-safe the trait must be object-safe and `'a` must be long enough. The `'static` default exists because `Box<dyn Error>` is the common case (owned errors) and it simplifies the API.

- id: rust-traits-01
  answer: |
    Static dispatch (generics with `T: Trait`) monomorphizes: the compiler generates a separate copy of the function for each concrete type used. Result: zero-cost abstraction (inlining, devirtualization), but code bloat and inability to store heterogeneous types. Dynamic dispatch (`&dyn Trait`) uses a vtable pointer; the function is compiled once and called through indirection at runtime. Tradeoffs: dynamic dispatch lets you store mixed types in a collection and avoids monomorphization bloat, but you pay a vtable indirection cost per call, lose inlining opportunities, and cannot easily use the return position `impl Trait` style. Generics also require the type to be `Sized` by default, while trait objects are unsized.

- id: rust-traits-02
  answer: |
    In argument position (`fn f(x: impl Trait)`), the caller chooses the concrete type; it's sugar for a generic parameter (`fn f<T: Trait>(x: T)`). The compiler monomorphizes. In return position (`fn f() -> impl Trait`), the *caller* only sees the trait; the *callee* chooses (and fixes) the concrete type, and the compiler knows exactly what it is internally for optimization. You can return different concrete types from different branches as long as they're the same type. The key difference: in argument position the caller is free, in return position the caller is constrained to not know the concrete type.

- id: rust-traits-03
  answer: |
    The orphan rule (part of coherence) says: you can implement a trait for a type only if either the trait or the type is defined in your crate. You can't `impl Display for Vec<T>` in your own crate because both `Display` (from `std`) and `Vec` (from `std`) are foreign. This prevents conflicting implementations across crates — if two crates both impl'd `Display for Vec<T>`, there would be ambiguity. Workarounds: newtype patterns (wrap `Vec<T>` in your own struct), extension traits, or `#[fundamental]` (only `&T` and `&mut T` have this).

- id: rust-error-01
  answer: |
    Return `Option<T>` when the *absence* of a value is a normal, expected case and the caller doesn't need to know *why* (e.g. `Vec::get`, `HashMap::get`, parsing optional fields). Return `Result<T, E>` when absence is an error that the caller may want to handle, log, or propagate — i.e. there's a meaningful failure reason (network error, parse error, permission denied, etc.). The rule of thumb: `Option` = "maybe not found"; `Result` = "this can fail and you need to know why."

- id: rust-error-02
  answer: |
    The `?` operator on a `Result<T, E>` does: (1) if the value is `Ok(v)`, unwrap it and continue; (2) if it's `Err(e)`, it converts `e` using `From` (or `Into`) to the function's own error type, wraps it in `Err`, and returns early from the function. This means `?` performs an implicit error-type conversion. For `Option`, `?` converts `None` into the function's return type (which must itself be `Option` or `Result` with an appropriate conversion). The key: it's not just unwrapping — it's *conversion + early return*.

- id: rust-error-03
  answer: |
    `panic!` / `.unwrap()` is appropriate when: a failure indicates a bug in your code (logic error, invariant violation), when the error is truly unrecoverable (you can't proceed at all), or in tests/prototypes/one-off scripts where simplicity matters. Return `Result` when: the caller could reasonably handle the failure, the error is expected/normal (network timeouts, user input validation), the failure is recoverable, or you're writing a library (libraries should almost never panic — it removes control from the caller). Also prefer `Result` when the error carries information the caller might need.

- id: rust-error-04
  answer: |
    Your error type must implement `std::error::Error` (and therefore `Display` and `Debug`). To work with `?`, it needs `From` implementations for each underlying error type it wraps. With `thiserror`, you annotate an enum with `#[derive(Error)]` and attribute each variant with `#[error("...")]` for `Display` and `#[from]` for automatic `From` impls. Alternatively, `anyhow` provides a dynamic, catch-all `Error` type that auto-wraps any `std::error::Error` via `?`. The key traits: `std::error::Error + Display + Debug + From<InnerError>` for each variant.

- id: rust-iterators-01
  answer: |
    `map` and `filter` return *lazy* iterator adapters — they produce a chain of zero-cost combinators but do nothing until consumed. No work happens until a *consumer* is called: `.collect()`, `.for_each()`, `.sum()`, `.count()`, `.next()`, etc. This lazy design lets the compiler fuse operations, avoids intermediate allocations, and lets you build complex pipelines with zero overhead. The pipeline is driven by pulling elements one at a time through the consumer.

- id: rust-iterators-02
  answer: |
    `.iter()` yields `&T` references; the collection is unchanged and still usable afterward. `.iter_mut()` yields `&mut T` mutable references; you can modify elements in place, but the collection is still borrowed while iterating. `.into_iter()` consumes the collection (takes ownership); it yields `T` by value and the collection can no longer be used. When called on a `Vec` directly (not via reference), `into_iter()` is default. For a `&Vec`, `into_iter()` gives references, not ownership of elements.

- id: rust-iterators-03
  answer: |
    `collect()` doesn't know what collection type you want, so when the type can't be inferred (e.g. you just write `let v = (0..5).collect();`), you get "type annotations needed." Fix: annotate (`let v: Vec<i32> = (0..5).collect();`) or use turbofish (`collect::<Vec<_>>()`). Collecting into a `Result` is special: `collect::<Result<Vec<_>, _>>()` from an iterator of `Result<T, E>` succeeds only if *all* items are `Ok`, returning the first `Err` early. This turns fallible iteration into a single `Result`.

- id: rust-iterators-04
  answer: |
    By default, a closure captures variables by reference (borrows them). If you try to return the closure from the function, the reference would dangle after the function returns. `move` transfers ownership of captured variables into the closure, so the closure owns the data and can outlive the original scope. The tradeoff: the original variable is no longer usable (moved), but the closure is now `'static` (if all captured data is `'static`) and can be returned, sent to another thread, or stored.

- id: rust-smartptr-01
  answer: |
    `Box<T>` provides heap allocation with a single owner and automatic deallocation on drop. It gives you: heap-allocated data (useful when the value is too large for the stack), recursive types (a `Box` contains a fixed-size pointer, enabling `enum List { Node(i32, Box<List>), Nil }`), trait objects (`Box<dyn Trait>`), and ownership transfer across function boundaries. You genuinely need it for recursive data structures and when you must put a dynamically-sized type (DST) behind a sized pointer.

- id: rust-smartptr-02
  answer: |
    `Rc<T>` uses single-threaded reference counting (non-atomic increment/decrement) and is not `Send`/`Sync`. `Arc<T>` uses atomic reference counting (thread-safe) and is `Send + Sync`. `Arc` has higher per-operation cost due to atomic instructions, so if you're only working on one thread, `Rc` is cheaper. Also, `Rc` lets you use `RefCell<T>` for interior mutability (which `Arc` can't — it requires `Mutex`/`RwLock`). Always use `Arc` when data may be shared across threads.

- id: rust-smartptr-03
  answer: |
    Interior mutability means mutating a value through a shared reference (`&T`) — normally forbidden by Rust's aliasing rules. `RefCell<T>` provides runtime-checked borrowing: `.borrow()` gives `Ref<T>` (like `&T`), `.borrow_mut()` gives `RefMut<T>` (like `&mut T`). If you violate the single-mutable-or-many-immutable rule at runtime, it panics. Cost: a borrow counter (one `isize` of runtime overhead), and the check happens at runtime (slight overhead, no compile-time optimization of inlining). Compare with normal `&mut` which has zero runtime cost and is checked entirely at compile time.

- id: rust-smartptr-04
  answer: |
    `Rc<RefCell<T>>` gives shared ownership (`Rc`) plus runtime-checked interior mutability (`RefCell`) — multiple owners can mutate the data one at a time. It's the single-threaded idiom because both `Rc` and `RefCell` are not thread-safe (`!Send`, `!Sync`). The multi-threaded equivalent is `Arc<Mutex<T>>` (or `Arc<RwLock<T>>`): `Arc` provides thread-safe shared ownership via atomic refcounting, `Mutex`/`RwLock` provides thread-safe exclusive/borrowed access.

- id: rust-concurrency-01
  answer: |
    `Send` is a marker trait indicating that ownership of a value can be *transferred* to another thread. `Sync` indicates that a type can be *shared* (via `&T`) across threads. Formally: `T: Sync` iff `&T: Send`. Rust uses these at compile time: if you try to send a non-`Send` type (like `Rc`) to another thread via `thread::spawn` or channel, the compiler rejects it. Similarly, storing a non-`Sync` type behind a shared reference would be a data race. This makes data races a compile-time error with zero runtime cost.

- id: rust-async-01
  answer: |
    Calling an `async fn` returns a future (an `impl Future<Output = T>`). The future does nothing until polled. An executor (like `tokio::runtime::Runtime`, `block_on`, or `#[tokio::main]`) is required to drive the future by calling `.poll()` repeatedly until it returns `Poll::Ready(result)`. Without a runtime, the future is just a value — the code inside never executes.

- id: rust-async-02
  answer: |
    `tokio::spawn` requires the future to be `'static + Send`. `Rc<T>` is not `Send` (it's single-threaded reference counting), and a `MutexGuard` is also not `Send` (it holds a borrow of the mutex, which is tied to the thread that locked it). Holding them across `.await` means the future borrows them, making the future neither `'static` nor `Send`, violating `tokio::spawn`'s bounds. Fix: use `Arc<Mutex<T>>` for shared state, or `.lock().await` / `.await` to get the guard right before use, or restructure to avoid holding the guard across yield points.

- id: rust-async-03
  answer: |
    Async runtimes (like Tokio's `current_thread`) use cooperative scheduling: tasks yield only at `.await` points. A blocking call like `std::thread::sleep` or heavy CPU work occupies the thread for its entire duration, preventing the executor from polling other tasks. This stalls the entire runtime (or a worker thread), degrading throughput and latency. Instead: use async-compatible I/O (`tokio::time::sleep`), offload blocking work with `tokio::task::spawn_blocking`, or use a dedicated blocking thread pool. This keeps the executor responsive.

- id: rust-match-01
  answer: |
    Exhaustive matching means every possible value of the matched type must be covered by some arm. If you miss a variant of an enum, the compiler refuses to compile. This is especially valuable for enums because adding a new variant to an enum will break every `match` that doesn't handle it, acting as a compile-time reminder to handle the new case everywhere. It prevents silent logic bugs from forgotten branches and makes adding variants a compiler-guided refactor rather than a runtime surprise.

- id: rust-match-02
  answer: |
    `if let Some(x) = val { ... }` — shorthand for matching a single pattern when you only care about one case; no exhaustiveness needed. `let Some(x) = val else { return ...; }` — early-return style: if the pattern doesn't match, execute the else block and return/break/continue. It's for "unwrap or bail." `match` — full exhaustive matching of all patterns, with multiple arms, guards, and complex destructuring. Use `match` when you need to handle several cases; `if let` for a single case; `let...else` for early-exit guard patterns.

- id: rust-match-03
  answer: |
    When you `match &opt { Some(x) => ... }`, the pattern `Some(x)` matches against `&Option<T>`. Rust uses *match ergonomics* (auto-dereferencing and auto-binding) so `x` is bound as `&T` (a reference to the inner value), not `T`. This is due to the auto-ref/auto-deref behavior in pattern matching introduced in Rust 2018 — the compiler automatically inserts `&` patterns to match the reference, so you don't have to write `Some(&x)` explicitly. The type of `x` is `&T`.

- id: rust-match-04
  answer: |
    Destructuring extracts fields: `Point { x, y } => ...` pulls out `x` and `y` from a struct. Match guards add conditional logic within a pattern: `Some(x) if x > 5 => ...` — the arm only matches if the pattern matches AND the guard is true (guard failures fall through). `@` bindings capture a value while also testing it: `Some(x @ 1..=5) => ...` binds `x` to the matched value only if it's in the range 1..=5; `Some(x) => ...` would match but `x` covers all values, while `@` lets you both test a subset and bind the whole thing in one arm.
```
