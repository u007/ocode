- id: rust-ownership-01
  answer: |
    `let b = a;` moves the `String` into `b`. Afterwards, `a` has been moved from and cannot be used, because Rust would otherwise have two owners responsible for freeing the same heap allocation, risking a double free. You can use `b`, or explicitly clone the value with `let b = a.clone();` to retain both variables.

- id: rust-ownership-02
  answer: |
    `Copy` permits an implicit shallow bitwise copy during assignment, function calls, and similar operations. Such types must not require custom resource cleanup because the original and copies remain independent. `Clone` is an explicit trait, usually implemented with a `clone` method, that can perform an arbitrarily expensive or custom copy.

    A type cannot implement both `Copy` and `Drop`: copying a resource-owning value could create two independently owned copies that are both destroyed, potentially releasing the same resource twice. The compiler therefore forbids that combination.

- id: rust-borrowing-01
  answer: |
    Rust permits either any number of shared references, `&T`, or one exclusive mutable reference, `&mut T`, to a location at a time, for the relevant region. Shared references permit reading but not mutation, while a mutable reference requires exclusive access.

    This rule prevents compile-time data races and related bugs such as simultaneous mutation, mutation while iterating, and reading data while another alias is mutating it. References may coexist with immutable reads, so ordinary concurrent reads are still possible.

- id: rust-lifetimes-01
  answer: |
    A lifetime such as `'a` is a generic compile-time region used to describe relationships between references. It does not store a duration, extend a value's actual lifetime, or cause a value to live longer. The borrow checker uses these relationships to ensure that a reference is not used after the data it refers to has been destroyed.

    Elided or inferred lifetimes can be shorter or longer than a named lifetime; `'a` is just a parameter whose region the compiler infers at each use.

- id: rust-lifetimes-02
  answer: |
    Lifetime elision applies these rules:

    1. Every elided lifetime in an input position, including the anonymous lifetime in `&str`, `&[T]`, or `&dyn Trait`, is initially treated as a distinct input lifetime.
    2. If there is exactly one input lifetime, it is assigned to every elided output lifetime.
    3. If the function is a method with a receiver such as `&self` or `&mut self`, that receiver's lifetime is assigned to every elided output lifetime.
    4. Otherwise, an elided output lifetime cannot generally be inferred and must be written explicitly.

    Thus, the output of `fn first(s: &str) -> &str` is inferred to borrow from `s`. Writing `'_` explicitly requests the same inference without naming the lifetime.

- id: rust-lifetimes-03
  answer: |
    `&'static T` is a reference whose referent is valid for the entire program. A string literal is an example, but static data is not the only possibility.

    `T: 'static` is a bound on the type `T`: every value of `T` can remain valid for the entire program because `T` contains no references whose referents have a shorter lifetime. An owned `String` satisfies this bound, while `&'a T` satisfies it only when `'a` is itself `'static`. This bound does not mean that `T` is allocated statically.

- id: rust-lifetimes-04
  answer: |
    A bare trait object normally defaults to a `'static` object bound, so `Box<dyn Error>` is shorthand for `Box<dyn Error + 'static>`. The concrete error and anything it borrows must therefore be valid for the whole program.

    If the concrete error stores a reference tied to an input, use the same relationship explicitly, for example `fn parse<'a>(text: &'a str) -> Result<(), Box<dyn Error + 'a>>`. The `+ 'a` permits the trait object to contain borrows limited to the lifetime of `text`; it must not outlive that data.

- id: rust-traits-01
  answer: |
    With generic static dispatch, `fn f<T: Trait>` is compiled separately for each concrete `T` used by callers. The call is resolved statically, often enabling inlining and aggressive optimization, but it increases compile time and potentially binary size.

    With `&dyn Trait`, the concrete type is selected at runtime and invoked through a vtable. This supports heterogeneous collections and keeps generated code smaller, but adds dynamic indirection and generally gives the optimizer less information. The choice is between per-type optimization and runtime flexibility.

- id: rust-traits-02
  answer: |
    In argument position, `impl Trait` is shorthand for an anonymous generic type parameter. The caller chooses the concrete argument type, subject to the implementation's stated bounds, and the compiler usually infers it from the argument.

    In return position, `impl Trait` declares an opaque concrete return type. The function's implementation determines the type, while callers can use it through the promised interface but cannot name that type directly. It is not the same as returning `Box<dyn Trait>`.

- id: rust-traits-03
  answer: |
    The orphan rule is Rust's coherence rule. A trait implementation must involve a trait or self type local to the current crate, while also satisfying restrictions on uncovered generic parameters. This prevents separate crates from introducing conflicting implementations that the compiler could not resolve consistently.

    `Vec<T>` is defined in the standard library, and `Display` is also a standard-library trait, so neither is local to your crate. A local newtype such as `struct MyVec<T>(Vec<T>)` can be given a `Display` implementation instead.

- id: rust-error-01
  answer: |
    Use `Option<T>` when the meaningful result is “a value or no value,” such as an optional lookup that found nothing. There is no additional error explanation to return.

    Use `Result<T, E>` when failure is meaningful and callers need to know or handle why the operation failed, such as parsing, I/O, or validation errors. If a lookup error can be meaningful, convert an absent result with methods such as `ok_or` or `ok_or_else`.

- id: rust-error-02
  answer: |
    For a `Result`, `expr?` evaluates `expr` and then inspects it:

    - `Ok(value)` is unwrapped, and the enclosing function returns `value` immediately.
    - `Err(error)` immediately returns from the enclosing function as an error.

    The returned error is converted into the enclosing function's error type using the `From` trait, conceptually through `From::from(error)`. Therefore, returning `Result<T, OuterError>` requires `OuterError: From<InnerError>`. In short, `?` provides early return plus automatic, explicitly supported error conversion; it does not automatically wrap arbitrary errors.

- id: rust-error-03
  answer: |
    Return `Result` for expected, recoverable failures that a caller could reasonably handle, especially in libraries. Examples include missing files, invalid input, network failures, and parsing errors.

    Use `panic!` when continuing would violate an invariant and the situation is considered a programming error or otherwise unrecoverable. `unwrap()` and `expect()` similarly assert success; they are reasonable in tests, prototypes, or for compiler-established invariants, but should not be used blindly for ordinary runtime failures. `expect()` is preferable when a custom failure message improves diagnosis.

- id: rust-error-04
  answer: |
    A common library error type is usually an enum with one variant for each underlying failure. To work with `?`, it should implement `From` for each source error that callers may propagate, converting a source error into the appropriate enum variant. It should generally implement `std::error::Error`, provide a useful `Display` implementation, and optionally expose underlying failures as an error `source` chain.

    `thiserror` reduces boilerplate with `#[derive(Error)]` and `#[from]` attributes. For example, a `FromIo` variant marked with `#[from]` automatically implements `From<std::io::Error>`, while the derive can generate the `Display` and `Error` implementations from annotations.

- id: rust-iterators-01
  answer: |
    The iterator adapters are lazy. Constructing `map` and `filter` creates an iterator describing the operations; it does not call `expensive` or `cond` yet.

    Work occurs when the iterator is consumed, such as by `for`, `while let`, `collect`, `sum`, `count`, or `find`. Consumption advances the chain and invokes the closures only for items that are actually traversed and pass earlier stages.

- id: rust-iterators-02
  answer: |
    `iter()` produces an iterator over shared references, `&T`, so the collection remains available afterward.

    `iter_mut()` produces mutable references, `&mut T`, allowing elements to be modified through the iterator while retaining the collection. The mutable borrow prevents incompatible access during iteration.

    `into_iter()` consumes the collection and, for common containers such as `Vec<T>`, produces owned `T` values. The original collection cannot be used afterward unless its particular `IntoIterator` implementation copies or otherwise preserves it rather than consuming it.

- id: rust-iterators-03
  answer: |
    `collect()` must construct a collection type with a known `FromIterator<Item>` implementation, but its target type often cannot be inferred from an expression such as `collect()`. Annotate the result (`let values: Vec<_> = ...`), use a function's return type, or specify the target explicitly with turbofish, such as `iter.collect::<Vec<_>>()`.

    Collecting an iterator whose items are `Result<T, E>` into `Result<Vec<T>, E>` uses `Result`'s special collection behavior. It returns the first encountered `Err` and discards the accumulator, or it returns all collected `T` values if every item is `Ok`. All items must therefore use a compatible error type `E`.

- id: rust-iterators-04
  answer: |
    By default, a closure borrows captured variables as needed. Returning a closure that refers to a local can therefore produce a closure containing a borrow of that local, and the closure cannot outlive the local.

    `move` makes the closure take ownership of its captured variables when it is created. Non-`Copy` values are moved into the closure, while `Copy` values are copied. This changes capture mode, not the lifetime of the surrounding local, and does not by itself make the returned closure valid for `'static` use.

- id: rust-smartptr-01
  answer: |
    `Box<T>` provides unique ownership of a heap-allocated `T`. Moving the `Box` moves only the pointer, not the value, and the allocation is automatically freed when the last owning `Box` is dropped.

    One genuine case is a recursive data structure: `struct Node { next: Option<Box<Node>> }`. Without `Box`, the recursive field would make `Node` have an infinite size. `Box` is also commonly used for explicit indirection and sized trait objects such as `Box<dyn Error>`.

- id: rust-smartptr-02
  answer: |
    `Rc<T>` and `Arc<T>` both provide shared ownership and automatic reference-counted destruction. `Rc` uses ordinary, non-atomic counters and is intended for single-threaded use. `Arc` uses atomic counters, allowing shared ownership to cross threads, at the cost of more expensive increments and decrements.

    `Arc` is unnecessary overhead in code known to remain single-threaded and often cannot help unless the contained `T` satisfies the appropriate `Send` and `Sync` requirements. Using it everywhere would impose synchronization costs without providing a benefit.

- id: rust-smartptr-03
  answer: |
    Interior mutability allows mutation through a shared reference while the outer type still appears immutable. `RefCell<T>` enforces Rust's shared-or-exclusive borrowing rules at runtime rather than through the static borrow checker.

    `borrow()` creates a shared runtime guard, and `borrow_mut()` creates an exclusive one. Violating the rules, such as requesting a mutable borrow while a shared borrow remains active, causes a panic. Unlike a compile-time `&mut T`, its guarantees and overhead are dynamic, so it is useful for delayed, local, or conditional mutation where the compiler cannot express the access statically.

- id: rust-smartptr-04
  answer: |
    `Rc` supplies shared ownership, while `RefCell` supplies single-threaded interior mutability and runtime borrow checking. Together they allow several owners to access mutable state when no single owner could safely retain an ordinary `&mut T` for the state's whole lifetime.

    The usual multi-threaded equivalent is `Arc<Mutex<T>>`, with `Arc` providing shared ownership and `Mutex` serializing access across threads. `Arc<RwLock<T>>` is an alternative for state with many readers, while atomics or specialized concurrent types may be better for simple operations.

- id: rust-concurrency-01
  answer: |
    `Send` means a value's ownership can be safely transferred to another thread. It means, in particular, that there are no thread-affine resources such as a `Rc` or an active thread-bound guard.

    `Sync` means shared access is safe across threads. For a `&T` to cross a thread boundary, `T` must be `Sync`; equivalently, `&mut T` is `Send` when `T` is `Send`.

    These are usually automatically inferred marker traits. Thread-spawning APIs require the values transferred to be `Send`, and APIs sharing ordinary references require `Sync`. Consequently, attempting to share a non-`Sync` value through safe Rust or transfer an `Rc` to another thread becomes a compile-time error. These checks prevent data races, although incorrect `unsafe` implementations can violate the guarantees.

- id: rust-async-01
  answer: |
    Calling an `async fn` constructs and returns a future. The function's body is generally not executed at the call site; its state, arguments, and captured variables are held in that future.

    The body runs when the future is polled to completion or suspension by an executor, using mechanisms such as `.await`, `block_on`, or an async runtime. Merely creating the future and dropping it without polling performs none of its asynchronous work.

- id: rust-async-02
  answer: |
    `tokio::spawn` requires its future to be both `Send` and `'static`. An `Rc` is not `Send`, and a mutex guard is generally not `Send`; in addition, a guard or reference can borrow a value whose lifetime does not satisfy `'static`.

    Because an `.await` can suspend while retaining captured locals, holding either across the suspension can make the generated future fail those bounds. Minimize the guard's scope, drop the `Rc` before awaiting, or use an `Arc` with a mutex when shared cross-thread access is actually needed. If deliberately executing a `!Send` future on one local thread, a local executor or scoped local task can be appropriate.

- id: rust-async-03
  answer: |
    Blocking work prevents the runtime worker from polling any other task assigned to that worker. One blocking task can therefore increase latency and reduce throughput even when the runtime has multiple worker threads. Long CPU-bound work similarly monopolizes a worker, while some blocking system calls can also consume resources inefficiently.

    Put thread-blocking operations in `tokio::task::spawn_blocking` and propagate their result through a channel or join handle. CPU-heavy parallel work may be better placed in a dedicated Rayon pool or another background-job system, with the async task waiting for its result. Limiting concurrent blocking work may also be necessary to prevent exhaustion of the blocking pool.

- id: rust-match-01
  answer: |
    An exhaustive `match` covers every possible value permitted by the matched type, so the compiler proves that every path produces a value. Patterns need not be non-overlapping; later arms can handle values not matched by earlier ones, but some arm must be unconditional unless the match is assigned to an uninhabited type.

    This is especially useful for enums because the compiler knows every possible variant. Adding a variant causes previously exhaustive matches to fail compilation, prompting the author to handle the new state explicitly. A wildcard may allow the match to compile, but it can conceal that important update.

- id: rust-match-02
  answer: |
    Use `if let` when exactly one refutable pattern is meaningful and all other cases can share one path. It is concise and idiomatic for simple `Option`, `Result`, or enum checks.

    Use `let ... else` when one successful pattern should produce bindings and the fallback should stop the surrounding operation. Its `else` block must diverge, for example with `return`, `break`, `continue`, or `panic!`. This avoids nesting and preserves a useful “valid data” path.

    Use a full `match` when several alternatives have distinct behavior, exhaustiveness must be proven, or guards, bindings, or multiple patterns are needed.

- id: rust-match-03
  answer: |
    Assuming `opt: Option<T>`, the binding `x` in `match &opt { Some(x) => ... }` has type `&T`, not `T`. The outer pattern matches the `Option` by shared reference, and the inner `x` refers to its contained value.

    This automatic behavior is called match ergonomics. Since Rust 2018, patterns on references can automatically adjust their binding mode so nested patterns bind through the reference instead of requiring explicit dereference patterns.

- id: rust-match-04
  answer: |
    Destructuring extracts fields or tuple positions into separate bindings. Match guards conditionally accept an arm after its pattern matches. An `@` binding names the whole value while also checking a subpattern:

    ```rust
    match point {
        (0, 0) => "origin",
        (x, 0) if x > 0 => "positive x-axis",
        (x, 0) => "non-positive x-axis",
        Point { x, y: 0 } => "on the x-axis",
        other @ Point { x, y } => {
            println!("point: ({x}, {y})");
            other
        }
    }
    ```

    Here, tuple and struct patterns destructure the value, the `if x > 0` guard adds a condition, and `other @ Point { x, y }` binds both the complete `Point` as `other` and its individual coordinates.

- id: rust-ownership-03
  answer: |
    Assuming the function takes `Vec<T>` by value, passing it moves ownership to the function. The function may mutate, replace, or drop the vector, and the caller can no longer use its original `Vec<T>` afterward. Rust does not make an implicit copy.

    Options include:
    - Borrow instead: use `&Vec<T>` or preferably `&[T]` for read-only access, or `&mut Vec<T>` if the function must modify it.
    - Return ownership: have the function return the `Vec<T>` and use the returned value.
    - Clone deliberately: pass `v.clone()` when an independent copy is needed, accepting the cloning cost.
    - Transfer intentionally: if the function needs to take ownership, capture the returned or transferred value rather than using the original afterward.
