# nvidia/nemotron-3.5-super-vl — closed-book answers for rust (openrouter, 2026-10-16); answerer saw only _prompts/rust.md
- id: rust-ownership-01
  answer: |
    Assigning `let b = a;` moves the `String`: the pointer, length, and capacity are copied to `b`, but `a` is invalidated so the heap allocation has exactly one owner. Using `a` afterward is a compile-time error ("use of moved value") because two owners could both try to free the same memory (double-free).
- id: rust-ownership-02
  answer: |
    `Copy` is implicit and bitwise (a plain memory copy) for cheap scalar types; the original stays usable after assignment. `Clone` is explicit via `.clone()` and may do arbitrary expensive work like deep copies and allocations. A type implementing `Drop` cannot be `Copy` because `Copy` would silently duplicate the value, causing `drop` to run twice on the same resource (double-free/double-close).
- id: rust-ownership-03
  answer: |
    Passing by value moves the `Vec<T>` into the function; the caller loses ownership and the vector is dropped when the function returns unless it is returned. Options: borrow it (`&Vec<T>` or `&[T]`) so the caller keeps it, clone it if the callee needs its own copy, take it and return it, or redesign so the function deliberately consumes it.
- id: rust-borrowing-01
  answer: |
    At any given time you may have either exactly one mutable reference (`&mut T`) or any number of shared references (`&T`) to a piece of data, never both simultaneously, and all references must be valid (no dangling). This compile-time rule prevents data races, use-after-free, dangling pointers, and iterator invalidation.
- id: rust-lifetimes-01
  answer: |
    `'a` names a region and expresses a constraint: a reference with that lifetime is valid for at least the duration `'a`. It is descriptive, not prescriptive — it does not change how long a value actually lives and has no runtime effect.
- id: rust-lifetimes-02
  answer: |
    Elision rules: (1) each elided input lifetime becomes a distinct lifetime parameter; (2) if there is exactly one input lifetime, it is assigned to all elided output lifetimes; (3) if there are multiple inputs but one is `&self`/`&mut self`, that lifetime is used for elided outputs; otherwise eliding an output lifetime is an error. `fn first(s: &str) -> &str` works by rule 2, tying the output lifetime to the input.
- id: rust-lifetimes-03
  answer: |
    `&'static T` is a reference valid for the entire program run (e.g., string literals). `T: 'static` is a bound on a type meaning it contains no borrowed data with a non-`'static` lifetime — it owns everything it references, so a value of type `T` can live for the whole program. Owned types like `String` satisfy `T: 'static`.
- id: rust-lifetimes-04
  answer: |
    `Box<dyn Error>` defaults to `Box<dyn Error + 'static>`: the erased type must own all its data so the trait object is `'static`. You need `Box<dyn Error + 'a>` (or `&dyn Trait + 'a`) when the underlying type borrows data with lifetime `'a`, such as an error holding a `&'a str`. In most positions, trait objects default to `+ 'static`.
- id: rust-traits-01
  answer: |
    Static dispatch via generics monomorphizes a copy per concrete type at compile time — fast, with inlining possible, but larger binaries and types fixed at compile time. Dynamic dispatch via `&dyn Trait` uses a vtable with one shared code path and one pointer indirection per call, requires object-safe traits, and allows heterogeneous collections and runtime choice at the cost of speed and optimizability.
- id: rust-traits-02
  answer: |
    Argument-position `impl Trait`: the caller chooses the concrete type; the function is generic over any type implementing the trait. Return-position `impl Trait`: the callee chooses an opaque concrete type; the caller only knows it implements the trait and cannot name it. Callers choose in argument position; the function chooses in return position.
- id: rust-traits-03
  answer: |
    The orphan rule (coherence): you may `impl` a trait for a type only if at least one of the trait or the type is defined in your crate. You cannot `impl Display for Vec<T>` because both `Display` and `Vec` are defined in `std` (foreign to your crate), otherwise any crate could impl foreign traits on foreign types and create conflicting impls.
- id: rust-error-01
  answer: |
    Return `Option<T>` when the absence of a value is a normal outcome with no extra error information. Return `Result<T, E>` when an operation can fail and callers need to know why, so `E` describes the failure and can be handled or propagated.
- id: rust-error-02
  answer: |
    `?` on a `Result`: if `Ok`, it unwraps the value and continues; if `Err`, it returns early from the enclosing function with that error, converting it via `From::from` into the function's return error type. Effectively a `match` with early return plus error conversion.
- id: rust-error-03
  answer: |
    Use `panic!`/`.unwrap()` for programmer errors, impossible states, or cases where failure indicates a bug and continuing is unsafe (assertions, trusted indexing). Return `Result` for recoverable, expected failures the caller should handle — I/O, parsing, validation, network requests.
- id: rust-error-04
  answer: |
    The error type needs `Debug`, `Display`, and `std::error::Error`, plus `From<underlying error>` impls so `?` can convert errors into it. `thiserror` derives `Error`/`Display` from an enum and generates `From` impls via `#[from]` and error source chains, cutting boilerplate.
- id: rust-iterators-01
  answer: |
    Iterators are lazy: `map` and `filter` only build new iterator wrapper types storing the closure; no elements are visited until a consuming adapter drives the iterator (`collect`, `for_each`, `fold`, `find`, `next`, or a `for` loop).
- id: rust-iterators-02
  answer: |
    `iter()` borrows the collection and yields `&T` (immutable references; collection untouched). `iter_mut()` yields `&mut T`. `into_iter()` consumes the collection and yields owned `T` values (for `Vec<T>`, elements are moved out). After `into_iter()` the collection is moved and no longer usable.
- id: rust-iterators-03
  answer: |
    `collect()` can build any `FromIterator` target (`Vec`, `String`, `HashMap`, `Result`, …), so the compiler cannot infer which one and needs a type annotation or turbofish (`collect::<Vec<_>>()` / `let x: Vec<_> = ...`). Collecting into `Result` is special: an iterator of `Result<T, E>` collects into `Result<Vec<T>, E>` and short-circuits on the first `Err`.
- id: rust-iterators-04
  answer: |
    `move` makes the closure take ownership of every variable it captures, moving them into the closure's environment instead of borrowing them. It is needed when the closure outlives the local (returned or sent to another thread) or must be `'static`, since borrowed captures would tie it to a disappearing stack frame.
- id: rust-smartptr-01
  answer: |
    `Box<T>` allocates `T` on the heap with a single owning pointer: it gives a fixed known size (`Box<dyn Trait>`), indirection for moving large values without copying, and ownership of recursive types. You genuinely need it for recursive data structures (e.g., a `Cons` list), trait objects, and transferring large heap data cheaply.
- id: rust-smartptr-02
  answer: |
    `Rc<T>` is single-threaded reference counting (non-atomic, cheap, `!Send`/`!Sync`). `Arc<T>` is atomic reference counting, safe across threads, but more expensive due to atomic operations. Don't always use `Arc` because atomics are slower than plain increments and single-threaded code does not need thread safety.
- id: rust-smartptr-03
  answer: |
    Interior mutability lets you mutate data through a shared reference (`&T`), bypassing the usual compile-time aliasing rules. `RefCell<T>` enforces Rust's borrow rules at runtime: `borrow()`/`borrow_mut()` track active borrows and panic if a rule is violated. Cost: runtime overhead and panic-on-violation instead of compile-time rejection; not thread-safe.
- id: rust-smartptr-04
  answer: |
    `Rc<RefCell<T>>` combines shared ownership (`Rc`) with interior mutability (`RefCell`) so multiple owners can mutate the value in single-threaded code. The multi-threaded equivalent is `Arc<Mutex<T>>` (or `Arc<RwLock<T>>` for shared read access).
- id: rust-concurrency-01
  answer: |
    `Send`: safe to transfer ownership of a value to another thread. `Sync`: safe to share a value between threads (`&T` can be sent to another thread). Both are auto traits used as bounds — `thread::spawn` requires `F: Send + 'static`, and futures held across `.await` on multi-threaded executors must be `Send`. Because unsynchronized shared mutation fails these bounds, data races become compile errors.
- id: rust-async-01
  answer: |
    Calling an `async fn` returns an `impl Future` — a state-machine object that does nothing until polled. To execute it you must `.await` it (inside an async context) or hand it to an executor/runtime like Tokio that drives it to completion.
- id: rust-async-02
  answer: |
    The future captures everything it holds into its state machine, including across `.await` points, so the `Rc` or `MutexGuard` lives as long as the future. `tokio::spawn` requires `'static + Send` futures: `Rc` is `!Send` and not `'static`, and `MutexGuard` is typically `!Send`; holding them across an await makes the captured future non-`Send`/non-`'static`, so it cannot be spawned.
- id: rust-async-03
  answer: |
    A blocking call stalls the executor thread, preventing it from polling other tasks and tanking concurrency and latency. Instead, offload blocking work to a dedicated thread pool — `tokio::task::spawn_blocking` — and `.await` its result.
- id: rust-match-01
  answer: |
    `match` must be exhaustive: every possible value of the scrutinee type must be covered by a pattern (all variants or a `_` wildcard). For enums this is especially valuable because adding a variant makes every `match` fail to compile until handled — a compile-time reminder that turns missing cases into errors rather than runtime bugs.
- id: rust-match-02
  answer: |
    Use `if let` when you care about exactly one pattern and want to ignore the rest concisely; `let ... else` when one pattern is the success path and the `else` must diverge (return/panic/break); full `match` when multiple patterns need distinct handling or exhaustiveness matters.
- id: rust-match-03
  answer: |
    With match ergonomics (default binding modes), `match &opt { Some(x) => ... }` binds `x` as `&T` — a reference into the option — because matching a reference against a non-reference pattern automatically binds by reference rather than moving or copying. The feature is called match ergonomics.
- id: rust-match-04
  answer: |
    Destructuring breaks a value into bindings (`Some(x)`, `Point { x, y }`, `(a, b)`). Match guards add an extra condition (`=> if x > 0`) checked after the pattern matches. `@` bindings (`x @ subpattern`) bind the whole matched value to `x` while also matching it against `subpattern` (e.g., `Some(x @ 1..=5)`).
