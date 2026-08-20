# rust knowledge benchmark — answers
# Model: opencode-go/muse-spark-1.2   (provider-stripped id: muse-spark-1.2)
# CLOSED-BOOK via ocode run -dir <empty> (no corpus access). corpus_rev 1.

```yaml
- id: rust-ownership-01
  answer: |
    `String` does not implement `Copy`. `let b = a;` *moves* ownership of the heap allocation from `a` to `b`. `a` is then considered uninitialized/moved-from and the compiler rejects any further use of `a` (`use of moved value: value used here after move`). This is to guarantee a single owner who will `Drop`/free the buffer, preventing double-free and use-after-free without needing a GC. If you need both, borrow (`let b = &a;`), clone (`let b = a.clone();`), or move and return.

- id: rust-ownership-02
  answer: |
    `Clone` is an explicit, possibly-expensive duplication via `.clone()` (may allocate). `Copy` is an implicit, cheap bitwise copy done on assignment/passing by value, with no custom logic (`let b = a;` still leaves `a` valid). `Copy` is a marker for `Clone` where the copy is trivial/memcpy.
    A type with `Drop` cannot be `Copy` because `Copy` means a bitwise duplication leaves two valid instances that will both be dropped. For a resource-owning type (like `String`, `Vec`, `File`) that would mean double-drop / double-free. The compiler therefore forbids `impl Copy for T` if `T: Drop`.

- id: rust-ownership-03
  answer: |
    `fn foo(v: Vec<T>)` takes ownership by value - the caller's `Vec` is *moved* into the function. After the call the caller no longer owns it and cannot use it; when `foo` returns it will be dropped unless returned.
    Options: 1) Borrow: `foo(&v)` / `foo(&mut v)` / `foo(&[T])` - keep ownership in caller. 2) Clone before calling: `foo(v.clone())`. 3) Have foo return ownership: `fn foo(v: Vec<T>) -> Vec<T>`. 4) Use `Borrow`/`AsRef` generics to accept both owned and borrowed. Idiomatic is to take `&[T]` or `&Vec<T>` if you don't need ownership.

- id: rust-borrowing-01
  answer: |
    Core rule (aliasing+XOR-mutability): At any given time, you may have EITHER exactly one `&mut T` OR any number of `&T`s to the same data, never both simultaneously. And all references must always be valid (no dangling).
    This prevents at compile time: data races, iterator invalidation, use-after-free, double-free, and mutable aliasing bugs. Enforced by the borrow checker.

- id: rust-lifetimes-01
  answer: |
    `'a` does not change how long a value *lives* or extend its destructor. It's a compile-time annotation describing a *relationship* / constraint between references: that some references are valid only as long as the data they borrow lives, and how long those borrows overlap. e.g. `fn foo<'a>(x: &'a str, y: &'a str) -> &'a str` says the returned reference lives no longer than the *shorter* of the inputs' valid regions. The checker verifies the actual scopes satisfy the constraint; it never extends runtime lifetime.

- id: rust-lifetimes-02
  answer: |
    Elision lets you omit `'a` when one of 3 rules can infer it:
    1. Each input reference gets its own distinct lifetime.
    2. If there is exactly one input lifetime, that lifetime is assigned to all output lifetimes. So `fn first(s: &str) -> &str` desugars to `fn<'a>(s: &'a str) -> &'a str`.
    3. If there are multiple inputs but one is `&self`/`&mut self`, the lifetime of `self` is assigned to all outputs.
    Otherwise lifetimes must be written explicitly.

- id: rust-lifetimes-03
  answer: |
    1. `&'static T` - a *reference* that is valid for the entire program execution. e.g. string literals (`"hi": &'static str`), leaked allocations, or consts. The referent never drops.
    2. `T: 'static` as a bound - means *type T contains no non-'static borrowed references*. It may be owned data or `&'static` refs only. It does NOT mean the value lives forever, only that if it does borrow, that borrow is `'static`, so the value could be held indefinitely (e.g. for spawning a `'static` thread). `&'static T` implies `T: 'static` but not vice versa.

- id: rust-lifetimes-04
  answer: |
    A bare trait object without a lifetime defaults to `'static`: `Box<dyn Error>` means `Box<dyn Error + 'static>`. It can only hold types that contain no short-lived borrows (owned types).
    If the trait object needs to borrow data (e.g. `Box<dyn Error + 'a>` holding a `&'a str` error), you must write `+'a` explicitly. Same for `&'a dyn Trait`. As of Rust 2018 the default for `&dyn Trait` is inferred from context, but `Box<dyn Trait>` / owned trait objects still default to `'static`.

- id: rust-traits-01
  answer: |
    Static dispatch `fn f<T: Trait>(t: T)` / `impl Trait` monomorphizes: compiler generates a copy per concrete `T`, enables inlining and zero-cost, no indirection, but increases binary size and requires type known at compile time; can't have heterogeneous collection.
    Dynamic dispatch `&dyn Trait`/`Box<dyn Trait>` uses a vtable (fat pointer: data + vtable) and runtime indirect call. Allows heterogeneous collections (`Vec<Box<dyn Trait>>`), smaller binary, separate compilation, but has vtable lookup cost, prevents inlining, and trait must be object-safe.

- id: rust-traits-02
  answer: |
    `fn foo(arg: impl Trait)` in argument position is syntax sugar for `fn foo<T: Trait>(arg: T)` - a generic: the *caller* chooses the concrete type, `foo` must work for any `T: Trait`.
    `fn foo() -> impl Trait` in return position is an *opaque* existential: the *callee* chooses a single concrete type that implements Trait, hidden from the caller. All return paths must return the same concrete type (unlike `Box<dyn Trait>` which can return different types). Also `impl Trait` in return allows returning unnameable types like closures/iterators.

- id: rust-traits-03
  answer: |
    Orphan/coherence rule: you may implement `Trait` for `Type` only if either `Trait` or `Type` is defined locally in your crate. This prevents two crates both implementing the same foreign trait for the same foreign type (which would conflict when linked) and makes trait resolution coherent.
    You cannot `impl Display for Vec<T>` in your crate because both `Display` (std) and `Vec<T>` (std) are foreign; no local type involved. Workarounds: newtype wrapper `struct MyVec(Vec<T>)`, or define your own trait.

- id: rust-error-01
  answer: |
    `Option<T>` when failure is not an error but *absence* of a value with no additional reason to report: e.g. lookup miss, `first()`, `get()`. Caller handles `None`.
    `Result<T,E>` when operation can *fail* in a way the caller may need to diagnose/handle: `E` carries why it failed (IO error, parse error). Use `Result` for fallible operations that have multiple failure modes; `Option` for single "not found" semantics. Often `Option` can be converted to `Result` with context.

- id: rust-error-02
  answer: |
    `expr?` desugars roughly to `match expr { Ok(v) => v, Err(e) => return Err(From::from(e)) }` (and analogous for `Option` with `None`). Inside a `Result`-returning function, if `expr` is `Ok` it unwraps to the inner value; if `Err`, it converts `e` via `From`/`Into` to the function's error type (allowing error type coercion) and returns early from the enclosing function with that `Err`. Also works for `Option` and any `Try` type.

- id: rust-error-03
  answer: |
    Return `Result` for recoverable, expected failures that the caller can reasonably handle (IO, parsing, validation, network). `panic!`/`unwrap()`/`expect()` is for bugs, violated invariants, or unrecoverable situations where continued execution cannot be correct. Appropriate for: impossible states after internal checks, startup config that must be present, tests/examples/prototypes. Never unwrap on user input or in libraries; prefer `expect("msg")` over `unwrap()` if you must panic, to document why it's supposedly impossible.

- id: rust-error-04
  answer: |
    Needs an enum (or struct) that enumerates the underlying error variants and implements `std::error::Error` + `Display` + `Debug`, and implements `From` for each source error so `?` can auto-convert.
    Manually that's verbose. `thiserror` derives `Error`, `Display`, and `From` via `#[derive(Error)]` and `#[from]` / `#[error("...")]` attributes, generating the boilerplate. For binaries, `anyhow` provides a type-erased `anyhow::Error` with context chaining.

- id: rust-iterators-01
  answer: |
    Iterator adaptors like `.map()`, `.filter()` are *lazy*: they return a new iterator that captures the closure but does no iteration. No element is produced until the iterator is *consumed*. Work happens when you call a consuming method / adaptor that drives it: `for`, `next()`, `collect()`, `sum()`, `count()`, `fold()` etc., which repeatedly calls `next()`.

- id: rust-iterators-02
  answer: |
    `iter()` borrows immutably: `Iterator<Item=&T>`, collection stays owned and usable.
    `iter_mut()` borrows mutably: `Iterator<Item=&mut T>`, allows mutating elements in place, collection stays owned but exclusively borrowed.
    `into_iter()` takes ownership: `Iterator<Item=T>` (consumes collection; data moved/dropped after iteration). Note: for `&Vec<T>` the `IntoIterator` impl yields `&T` for ergonomics; use `Vec::into_iter` explicitly for `T`.

- id: rust-iterators-03
  answer: |
    `collect()` is generic over `FromIterator` / `Into<Target>`: `fn collect<B>(self) -> B where B: FromIterator<Self::Item>`. Many types implement it (Vec, HashSet, String), so the compiler often cannot infer `B`.
    Disambiguate with turbofish `collect::<Vec<_>>()`, `collect::<HashSet<_>>()`, or annotation `let x: Vec<_> = ...collect();`
    Special: `Iterator<Item=Result<T,E>>.collect::<Result<Vec<T>,E>>()` (or `Result<HashMap<...>>`) short-circuits: returns first `Err` encountered, otherwise collects all `Ok` values. Similarly `Option`.

- id: rust-iterators-04
  answer: |
    By default a closure borrows captured variables (`|x| ...`). `move |x| ...` forces the closure to *take ownership* (move) of the captured variables (or copy if `Copy`). This is needed when returning a closure/iterator that may outlive the stack frame - otherwise you'd return a closure borrowing a local that will be dropped. `move` also makes the closure `'static` if it owns its captures, allowing `spawn`/`return`. It moves the variable as a whole, not per-use.

- id: rust-smartptr-01
  answer: |
    `Box<T>` is an owned, heap-allocated single owner with indirection (pointer-sized on stack, content on heap), with `Deref`/`DerefMut`. Gives: fixed-size ownership for unsized/dynamic size, cheap moves, recursive types.
    Genuine needs: recursive types (`struct Node { next: Option<Box<Node>> }`), large values you want to move cheaply or avoid stack overflow, trait objects (`Box<dyn Trait>`), transferring ownership to another thread / FFI boundary.

- id: rust-smartptr-02
  answer: |
    Both are reference-counted shared ownership (clone increments count, drop decrements, last drop frees). `Rc<T>` uses non-atomic counters: `!Send + !Sync`, cheaper, only for single-threaded.
    `Arc<T>` uses atomic counters: `Send + Sync` if `T: Send + Sync`, safe to share across threads. Always using `Arc` wastes atomic overhead (memory barriers) when you don't need thread safety. Use `Rc` single-threaded, `Arc` multi-threaded.

- id: rust-smartptr-03
  answer: |
    Interior mutability allows mutating data through a shared `&T` reference, bypassing the normal `&mut` exclusivity, checked at runtime or via atomics.
    `RefCell<T>` holds a `T` and runtime borrow flags. `.borrow()` gives `Ref<T>` (panics if mutably borrowed) and `.borrow_mut()` gives `RefMut<T>` (panics if already borrowed). Cost: runtime check + panic on violation (vs compile-time error for `&mut`), not `Sync`, and cannot be used across threads. `Cell` for `Copy` types avoids the borrow flag.

- id: rust-smartptr-04
  answer: |
    `&mut T` enforces single owner with exclusive borrow, so shared mutable requires both shared ownership and interior mutability. `Rc` gives shared ownership (cloneable handles), `RefCell` gives interior-checked mutable borrow - combined `Rc<RefCell<T>>` allows multiple owners to mutate same `T` in single thread with runtime borrow checks.
    Multi-threaded equivalent is `Arc<Mutex<T>>` (or `Arc<RwLock<T>>`): `Arc` for atomic shared ownership, `Mutex`/`RwLock` for thread-safe interior mutability via OS/locking (blocking rather than panicking). Holds a guard to access.

- id: rust-concurrency-01
  answer: |
    Both are auto, marker, unsafe traits. `Send`: owning a `T` can be safely transferred to another thread (move). `Sync`: `&T` can be safely shared between threads (i.e., `&T: Send`). `T: Sync` iff `&T: Send`.
    The compiler uses them to forbid data races: `thread::spawn` requires `F: Send`, sharing via `Arc<T>` requires `T: Send+Sync`, `Mutex<T>` is `Sync` only if `T: Send`. Types with interior non-thread-safe mutability (`Rc`, `RefCell`, `*mut T`) are `!Send`/`!Sync`, so you get a compile error trying to send/share them.

- id: rust-async-01
  answer: |
    An `async fn` (or `async {}` block) desugars to a state machine implementing `Future<Output=T>`; calling it synchronously just constructs and returns that future, doing none of the body.
    To execute it, the future must be polled to completion by an executor / runtime: `.await` inside another async context, or `futures::executor::block_on`, `tokio::runtime::block_on`, `tokio::spawn`. Without polling, nothing runs.

- id: rust-async-02
  answer: |
    `tokio::spawn` requires `Future + Send + 'static` because it may move the future to another worker thread. `Rc<T>` is `!Send`/`!Sync`, so any future capturing an `Rc` becomes `!Send` and cannot be spawned. `MutexGuard` from `std::sync::Mutex` is `!Send` (to prevent holding a non-async lock across threads) and holding it across `.await` makes the future `!Send` and may also deadlock the executor.
    Additionally, holding a synchronous `MutexGuard` across `.await` would block the thread while suspended. Solution: drop guard before await, use `tokio::sync::Mutex`, or use `Arc` instead of `Rc`.

- id: rust-async-03
  answer: |
    Async executors (tokio) run tasks cooperatively on a small thread pool. A blocking call like `thread::sleep`, `fs::read`, or heavy CPU loop blocks the worker thread, preventing it from polling other futures, starving the runtime and killing concurrency/latency.
    Instead: use async variants (`tokio::time::sleep`, `tokio::fs`), or offload blocking/CPU work to a dedicated blocking pool via `tokio::task::spawn_blocking` / `block_in_place`, or `spawn` CPU work onto `rayon`/`tokio::task::spawn`.

- id: rust-match-01
  answer: |
    Exhaustiveness means `match` must handle every possible value of the type. Compiler rejects non-exhaustive matches. This is especially valuable for enums: adding a new variant causes every `match` on that enum to fail to compile until you handle the new case, preventing silent bugs. Use `_` or `..` to be intentionally non-exhaustive, or `#[non_exhaustive]`.

- id: rust-match-02
  answer: |
    `if let PAT = expr { ... }` for one pattern you care about, optionally with `else`, concise when other cases are ignored.
    `let PAT = expr else { diverge; }` (let-else, 1.65+) when you want to extract and continue on success, diverging (return/break) on failure - avoids rightward drift.
    `match` when you need to handle multiple patterns exhaustively or produce a value from several branches. Use `matches!` for boolean check.

- id: rust-match-03
  answer: |
    Due to *match ergonomics* (RFC 2005, binding modes), matching on a reference does not move. `match &opt { Some(x) => ... }` where `opt: Option<T>`, `x` is inferred as `&T` (or `&mut T` for `&mut`). The pattern `Some(x)` automatically binds by reference to avoid moving out of borrowed content. Before ergonomics you wrote `Some(ref x)` or `Some(&x)`. So `x: &T`.

- id: rust-match-04
  answer: |
    Destructuring: `let Point { x, y } = p;` or `match (a,b) { (1, y) => ... }` extracts fields/variants into bindings.
    Match guards: `match x { n if n > 0 => ..., _ => ... }` adds extra `if` condition after pattern; pattern matches but guard may reject, falling through.
    @ bindings: `match msg { n @ 1..=12 => println!("month {n}"), ... }` or `Some(x @ 0..=100) =>` binds the whole matched value to `n`/`x` while also applying pattern inside. Lets test + capture.
```
