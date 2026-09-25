- id: "rust-ownership-01"
  answer: |
    `let b = a;` moves the `String` from `a` into `b`. Afterwards, using `a` produces an error such as “borrow of moved value: `a`” unless `a` is explicitly initialized again.

    A `String` owns heap data, so allowing `a` and `b` to independently own the same allocation would risk a double free. Moving transfers sole ownership rather than duplicating it.

- id: "rust-ownership-02"
  answer: |
    `Copy` is a marker trait for values that can be implicitly copied when assigned or passed by value. A `String` is not `Copy` because duplicating its internal ownership would produce two owners of the same allocation.

    `Clone` defines explicit duplication through `clone()`. It can perform an inexpensive copy or an expensive deep copy.

    A type implementing `Drop` cannot also implement `Copy`. A copy would leave two values owning or managing the same resources, while only the original would be dropped after a move. The compiler cannot generally guarantee that such duplication and destruction would be valid.

- id: "rust-ownership-03"
  answer: |
    Passing the `Vec<T>` by value moves it into the function, so the caller no longer owns the vector and cannot use it unless the function gives ownership back.

    Options include:
    - Borrow it as `&[T]` when the function only needs to read it.
    - Borrow it as `&mut Vec<T>` when the function needs to modify it but not consume it.
    - Return the `Vec<T>` from the function if the function takes ownership.
    - Clone it before passing it, accepting the copying cost.
    - Move it into a longer-lived owner when ownership is intentionally being transferred.

- id: "rust-borrowing-01"
  answer: |
    Rust permits either any number of shared references to an immutable target or exactly one active mutable reference, but shared and mutable references to the same location cannot coexist.

    The ownership and lifetime checks prevent many aliasing bugs, including mutation through an alias, use-after-free, iterator invalidation, and thread data races. They do not prevent every logic error, but they make whole classes of memory-safety bugs compile-time errors.

- id: "rust-lifetimes-01"
  answer: |
    A lifetime such as `'a` describes a compile-time relationship between references and the values they may refer to. For example, the returned reference in:

    `fn longest<'a>(x: &'a str, y: &'a str) -> &'a str`

    must be valid for no longer than either input reference.

    A lifetime annotation does not extend a value's actual life or generate runtime code. It tells the borrow checker which borrows are related and how long the resulting reference may be used.

- id: "rust-lifetimes-02"
  answer: |
    The lifetime-elision rules are:

    1. Every elided lifetime in an input position is treated as a distinct lifetime.
    2. If there is exactly one input lifetime, an elided output lifetime is assigned that lifetime.
    3. If a method has `&self` or `&mut self`, an elided output lifetime is assigned the receiver's lifetime.
    4. If multiple input lifetimes exist and there is no receiver, the output lifetime cannot generally be inferred and must be written explicitly.

    Therefore `fn first(s: &str) -> &str` returns a reference tied to `s`, not an arbitrary reference.

- id: "rust-lifetimes-03"
  answer: |
    `&'static T` is a reference that remains valid for the entire program.

    `T: 'static` is a bound on a value type. It means `T` contains no non-static references and can be valid for the whole program. It does not require the value to be allocated statically, to exist forever, or itself to contain a reference. An owned value such as `String` satisfies `String: 'static`.

- id: "rust-lifetimes-04"
  answer: |
    `Box<dyn Error>` has the default object lifetime bound `Box<dyn Error + 'static>`. The concrete error type therefore cannot borrow from data with a shorter lifetime.

    If the trait object must represent an error borrowing data valid for `'a`, it must be written as `Box<dyn Error + 'a>`. The bound ties the trait object's valid references to `'a`; the object may still be used for a shorter lifetime.

- id: "rust-traits-01"
  answer: |
    With generic static dispatch such as `fn f<T: Trait>(x: T)`, Rust generates code specialized for the concrete types used. This enables monomorphization and aggressive inlining, but increases compilation time and generated code size.

    With dynamic dispatch through `&dyn Trait`, the concrete type is selected at runtime through a virtual-method table. One implementation serves many types and keeps code size smaller, but calls incur indirection and generally have less optimization opportunity.

- id: "rust-traits-02"
  answer: |
    In argument position, `fn f(x: impl Trait)` is shorthand for an anonymous generic parameter. The function operates generically, and the caller chooses the concrete argument type.

    In return position, `fn f() -> impl Trait` creates an opaque return type. The caller can use the promised trait capabilities but cannot name or choose the concrete type; the function's implementation determines it.

- id: "rust-traits-03"
  answer: |
    The orphan rule is part of Rust's coherence system. An implementation must be anchored by a local trait or an appropriate local type, with additional restrictions involving uncovered generic parameters.

    `Display` is defined in the standard library, and `Vec<T>` is also defined there. Implementing the foreign `Display` trait for the foreign `Vec<T>` is therefore illegal. A downstream crate must not be able to add an implementation that could overlap one the trait or type's owner might add later.

- id: "rust-error-01"
  answer: |
    Use `Option<T>` when the only relevant distinction is whether a value exists: `Some(value)` or `None`. No reason needs to be represented.

    Use `Result<T, E>` when an operation can fail and the caller needs information about the failure, such as an I/O error, parse error, or invalid argument. The successful value is in `Ok`, and the failure is in `Err`.

- id: "rust-error-02"
  answer: |
    On an `Ok(value)`, `?` evaluates to that inner value, allowing execution to continue.

    On an `Err(error)`, it immediately returns from the enclosing function. If the function returns a different error type `F`, the error is converted using `From`:

    `return Err(From::from(error));`

    Thus the expression requires a suitable `From<E> for F` implementation. If the error types are identical, the identity `From` implementation suffices. `?` does not panic; it propagates the error with the required conversion.

- id: "rust-error-03"
  answer: |
    `panic!` is appropriate for an unrecoverable violation of a programmer invariant, unreachable state, or a bug for which no meaningful recovery exists. `.unwrap()` and `.expect()` similarly assert that an `Option` is `Some` or a `Result` is `Ok`.

    A function handling expected runtime failures or untrusted input should generally return `Result` so callers can decide what to do. Libraries should avoid panicking for ordinary failures such as missing files, malformed input, or invalid arguments. Unwraps remain common in tests, examples, and startup code where the assumption is intentional.

- id: "rust-error-04"
  answer: |
    A useful library error type is commonly an enum with one variant for each important failure category. It should implement `std::error::Error`, which also requires `Debug` and `Display`, and provide conversions from underlying errors, typically through `From`.

    `thiserror` derives these implementations. A variant such as:

    `#[error("database failure: {0}")]`
    `Database(#[from] sqlx::Error)`

    declares the display text and generates `From<sqlx::Error>`, allowing `?` to convert that underlying error into the library's error type. `#[from]` also identifies the contained error as its source.

- id: "rust-iterators-01"
  answer: |
    `map`, `filter`, and similar iterator adaptors are lazy. Constructing the chain does not call `expensive` or `cond`; it only creates an iterator state machine.

    The work occurs when something consumes or polls the iterator, such as `collect()`, a `for` loop, `sum()`, or repeated calls to `next()`. Elements are then produced and filtered on demand.

- id: "rust-iterators-02"
  answer: |
    `iter()` borrows the collection immutably and yields `&T` elements. The collection remains owned by the caller.

    `iter_mut()` borrows the collection mutably and yields `&mut T` elements, allowing existing elements to be modified. The collection remains in the caller.

    `into_iter()` consumes the collection and yields owned `T` elements. The original collection is moved and cannot be reused afterward.

- id: "rust-iterators-03"
  answer: |
    `collect()` must choose a destination type implementing `FromIterator`, and the element type may be ambiguous. An empty iterator especially provides no element from which the compiler could infer the item type.

    Supply the destination with a binding annotation, an explicit return context, or a turbofish, for example:

    `let values: Vec<String> = iterator.collect();`
    `let values = iterator.collect::<Vec<String>>();`

    Collecting an iterator whose items are `Result<T, E>` has a special, short-circuiting implementation. `collect::<Result<Vec<T>, E>>()` gathers successful values and returns the first `Err` immediately if one is encountered.

- id: "rust-iterators-04"
  answer: |
    Normally, a closure captures each variable according to how it uses it: by shared reference for reading, by mutable reference for mutation, or by value for an owned operation.

    The `move` keyword forces captured variables to be taken by value. This commonly allows a closure to own captured data and be returned from a function; without ownership, a captured reference to a local variable would become invalid when the function returns. Cloning is an alternative when copying the data is appropriate.

- id: "rust-smartptr-01"
  answer: |
    `Box<T>` provides unique ownership of a heap-allocated `T`. Moving a `Box` transfers ownership without moving the allocated value, which also gives the value a stable address. It can hold unsized types and adds allocation and indirection overhead.

    A genuine use is a recursive type such as:

    `enum List { Node(i32, Box<List>), End }`

    Without `Box`, each `Node` would directly contain another `List` of the same infinite size. The indirection makes the type finite.

- id: "rust-smartptr-02"
  answer: |
    `Rc<T>` uses non-atomic reference counts and is confined to a single thread; it is neither `Send` nor `Sync`.

    `Arc<T>` uses atomic reference counts and can provide shared ownership across threads when `T` has the required `Send` and `Sync` properties.

    `Arc` is not always appropriate because atomic increments and decrements impose overhead. `Rc` also provides compile-time thread confinement, which can be safer when sharing across threads was never intended.

- id: "rust-smartptr-03"
  answer: |
    Interior mutability allows mutation through a shared reference while retaining rules about mutable access.

    `RefCell<T>` tracks active borrows at runtime. `borrow()` permits multiple shared borrows, while `borrow_mut()` requires that no other borrow is active. The guards returned by these methods provide `&T` or `&mut T`.

    A conflicting `borrow()` or `borrow_mut()` call panics. Unlike a normal `&mut T`, whose exclusivity is checked at compile time, `RefCell` performs dynamic borrow checking and has some runtime overhead.

- id: "rust-smartptr-04"
  answer: |
    `Rc<RefCell<T>>` allows multiple owners to point to the same `T`. The shared `Rc` coordinates ownership, and the interior `RefCell` permits runtime-checked mutable access when one owner calls `borrow_mut()`.

    The multi-threaded equivalent is usually `Arc<Mutex<T>>`, where `Arc` shares ownership and `Mutex` serializes access. A `RwLock` can replace the mutex for workloads with many readers. The `Rc<RefCell<T>>` type is not thread-safe; it is intended for confined, single-threaded use.

- id: "rust-concurrency-01"
  answer: |
    `Send` means ownership of a value can safely be transferred to another thread.

    `Sync` means a shared reference to the value, `&T`, can safely be used from multiple threads. In simplified terms, `&T` is `Send` when `T` is `Sync`.

    Rust automatically derives these marker traits for suitable types. APIs such as `thread::spawn`, scoped threads, channels, and async thread-spawning functions require appropriate `Send` and `Sync` bounds. Consequently, sharing ordinary mutable state or a non-thread-safe pointer across threads is rejected at compile time rather than being allowed to cause an unsynchronized data race. Unsafe code can bypass these guarantees and becomes responsible for maintaining them.

- id: "rust-async-01"
  answer: |
    Calling an `async fn` constructs and returns a lazy `Future` state machine; the function body does not execute merely because it was called. Arguments are evaluated and captured as needed, but execution begins when the future is polled.

    `std` defines `Future`, `Poll`, and `Waker`, but `std` has no built-in executor, so it has nothing that polls a future to completion. A third-party executor or runtime such as Tokio, async-std, smol, or at minimum `futures::executor::block_on` is required. Constructs such as `#[tokio::main]` provide the runtime and poll the main future with an equivalent of `block_on`.

- id: "rust-async-02"
  answer: |
    `Rc<T>` is neither `Send` nor `Sync`. If the future retains it across an `.await`, that state remains held by the future, making the future non-`Send`. Likewise, a guard from `std::sync::MutexGuard` is not `Send` because the unlocking thread is not guaranteed to be the locking thread.

    `tokio::spawn` requires its future, captured state, and output to be `Send + 'static`, because the task may move between executor threads and must not borrow local data. Use `Arc` for deliberately shared cross-thread state, and avoid holding a standard mutex guard across an `.await` by ending its scope first or redesigning around a suitable asynchronous synchronization primitive. If the future must remain thread-confined, use a current-thread executor with `spawn_local` or a `LocalSet`. A separate failure to satisfy `'static` can also occur when the future captures borrowed locals.

- id: "rust-async-03"
  answer: |
    Rust's async scheduling is cooperative: a task yields control only when an `.await` produces `Poll::Pending`. There is no preemption. A blocking operation such as `std::thread::sleep`, synchronous I/O, a contended `std::sync::Mutex`, or a long CPU loop never reaches such a yield point.

    The executor therefore cannot reclaim that worker thread. Other tasks on it starve, latency and queued timers are delayed, and the entire runtime can appear stuck until the blocking operation returns.

    Use `tokio::time::sleep` rather than `std::thread::sleep`, use asynchronous I/O APIs, move isolated blocking work to `tokio::task::spawn_blocking`, and use a compute-oriented pool such as Rayon for substantial CPU-parallel work.

- id: "rust-match-01"
  answer: |
    Exhaustiveness means the compiler proves that the match handles every possible value of its input. A catch-all such as `_` can cover remaining values, but without one, an unmatched value causes a compile-time error.

    For an enum, the compiler knows the complete set of variants and their possible data. Adding a new variant therefore makes existing exhaustive matches fail to compile, alerting the programmer to code that may need updating. A deliberately broad wildcard arm can hide that notification, so exhaustive matches are especially valuable when a newly introduced enum variant must not be silently ignored.

- id: "rust-match-02"
  answer: |
    Use `if let` when exactly one pattern is interesting and all other cases can share one alternative branch.

    Use `let ... else` when one particular shape must be handled but the common successful path should continue afterward. If the pattern does not match, the `else` block must diverge, for example with `return`, `break`, `continue`, or `panic!`.

    Use a full `match` when there are several meaningful cases, when bindings or guards are needed, or when all inputs must be handled explicitly. A full match communicates the complete decision structure most clearly.

- id: "rust-match-03"
  answer: |
    In:

    `match &opt { Some(x) => ... }`

    where `opt: Option<T>`, `x` has type `&T`, not `T`.

    This is caused by match ergonomics, whose default binding modes automatically adjust patterns when the scrutinee is a reference. The outer reference is matched transparently, and bindings receive the appropriate reference type without moving data out of the option.

- id: "rust-match-04"
  answer: |
    Destructuring extracts fields from tuples, structs, enums, slices, and other structured values. For example, `Point { x, y }` binds the struct's fields, and `(a, b)` binds tuple elements.

    Match guards add a boolean condition after a pattern. For example, `Some(x) if x > 10` enters that arm only when the extracted value passes the condition; otherwise matching continues with later arms.

    An `@` binding both matches a subpattern and binds the whole matched value. For example, `n @ 1..=9` checks that the value is in the range while making it available as `n`.

    These can be combined, for example:

    `Message::Point(Point { x, y: n @ 1..=9 }) if x > y => { /* ... */ }`
