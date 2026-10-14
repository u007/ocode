# cat-jam-free — closed-book answers for rust (opencode-go, 2026-10-14); answerer saw only _prompts/rust.md
# Delivery: one `ocode run -m opencode-go/cat-jam-free -effort med` session, sheet attached via -f, empty scratch cwd;
# session audited in sqlite: 1 user + 1 assistant message, 0 tool calls.

- id: rust-ownership-01
  answer: |
    `String` is not `Copy`, so `let b = a;` moves ownership from `a` to `b`. After the move, `a` is no longer valid and the compiler errors if you try to use it, because its heap buffer now belongs to `b`.
- id: rust-ownership-02
  answer: |
    `Copy` is an implicit bitwise duplication marker for types where copying does not require custom resource handling. `Clone` is an explicit (possibly expensive) cloning method. A type cannot be both `Copy` and `Drop` because `Copy` types are duplicated automatically instead of moved, which would break unique-ownership cleanup and could cause double-free/use-after-free.
- id: rust-ownership-03
  answer: |
    Passing by value moves the `Vec` into the function, so the caller loses ownership. Options include passing `&Vec<T>` or `&[T]` to borrow immutably, `&mut Vec<T>` to borrow mutably, returning the `Vec` from the function, cloning it first, or using shared ownership such as `Rc`/`Arc` when truly shared.
- id: rust-borrowing-01
  answer: |
    Rust allows either many shared immutable references or exactly one mutable reference, but not both at the same time (references must be disjoint in scope). This prevents aliasing-related bugs such as data races, iterator invalidation, and use-after-free-style mutation of shared data.
- id: rust-lifetimes-01
  answer: |
    A lifetime annotation expresses a relationship/constraint between references and how long a borrow must remain valid. It does not change the actual value's lifetime or extend storage; it helps the compiler verify references do not outlive their referents.
- id: rust-lifetimes-02
  answer: |
    Elision rules are: each input reference gets a distinct lifetime; if exactly one input lifetime, it is used for all output lifetimes; for methods, if there are multiple inputs and one is `&self` or `&mut self`, the self lifetime is used for outputs. Thus `fn first(s: &str) -> &str` has one input lifetime, assigned to the output.
- id: rust-lifetimes-03
  answer: |
    `&'static T` means the reference lives for the entire program duration and points to static data. `T: 'static` is a type bound meaning the type contains no borrowed data shorter than `'static`; it can still be an owned value that may be dropped at runtime. The former is about storage duration; the latter is about borrow constraints.
- id: rust-lifetimes-04
  answer: |
    A trait object in an owning position such as `Box<dyn Trait>` defaults to `+ 'static`. If the trait object may contain or be backed by borrowed data of lifetime `'a`, write `Box<dyn Trait + 'a>`; use the same explicit bound for references storing such objects, e.g. `&'a (dyn Trait + 'a)`.
- id: rust-traits-01
  answer: |
    Generics monomorphize the function for each concrete type, giving static dispatch with no runtime overhead but larger binary size and one concrete type at the call site. Trait objects erase the concrete type and use a vtable, enabling heterogeneous values and smaller code size but incurring runtime dispatch and limiting some optimizations.
- id: rust-traits-02
  answer: |
    In argument position, `impl Trait` is sugar for a generic parameter chosen by the caller. In return position, it is an opaque type chosen by the implementing function; the caller knows only the trait API, and all return paths must use the same concrete type.
- id: rust-traits-03
  answer: |
    The orphan rule says you can implement a foreign trait only if at least one type involved is local to your crate. You cannot `impl Display for Vec<T>` because both `Display` and `Vec` are foreign, which prevents conflicting implementations across crates. The usual workaround is a newtype wrapper.
- id: rust-error-01
  answer: |
    Use `Option<T>` when failure means simple absence with no useful diagnostic. Use `Result<T, E>` when an operation can fail for identifiable reasons and callers should be able to inspect or propagate the error.
- id: rust-error-02
  answer: |
    `?` checks a `Result`: on `Ok`, it unwraps the value; on `Err`, it returns early from the enclosing function with an error converted using `From` from the source error type to the function's declared error type. It can also return `None` early for `Option` in an `Option`-returning function.
- id: rust-error-03
  answer: |
    `panic!` or `unwrap()` are for unrecoverable bugs, violated invariants, or impossible states; they are also acceptable in tests/prototypes. Return `Result` for expected, recoverable failures such as bad input, I/O, parsing, or network errors.
- id: rust-error-04
  answer: |
    The error type needs variants wrapping each source error, `Display`, `std::error::Error`, and `From` conversions for each source so `?` can convert. Crates like `thiserror` derive these impls and messages from attributes such as `#[from]` and `#[error(...)]`, removing boilerplate.
- id: rust-iterators-01
  answer: |
    Iterator adaptors such as `map` and `filter` are lazy: they only store the computation and produce items when a terminal consumer pulls them. Nothing runs until you consume the iterator with something like `collect`, `for`, `count`, or `sum`.
- id: rust-iterators-02
  answer: |
    `iter()` borrows the collection and yields `&T`; `iter_mut()` borrows mutably and yields `&mut T`, allowing element mutation; `into_iter()` consumes the collection and yields owned `T`, leaving the original unusable. For slices, `into_iter()` yields references because the slice does not own elements.
- id: rust-iterators-03
  answer: |
    `collect()` is generic over `FromIterator`, so the compiler needs the target type. Annotate the variable or use turbofish, e.g. `.collect::<Vec<_>>()`. Collecting into `Result<Vec<T>, E>` (or `Option`) short-circuits: it builds the collection only if all items are `Ok`, returning the first `Err`.
- id: rust-iterators-04
  answer: |
    `move` makes the closure take ownership of captured variables by value rather than borrowing them. This lets the closure/iterator outlive the original scope without dangling references; for `Copy` types, the values are copied.
- id: rust-smartptr-01
  answer: |
    `Box<T>` allocates `T` on the heap and owns it, dereferencing like a pointer. A genuine need is recursive data structures such as an enum containing a boxed node, or trait objects such as `Box<dyn Trait>`.
- id: rust-smartptr-02
  answer: |
    `Rc<T>` uses a non-atomic reference count for single-threaded shared ownership; `Arc<T>` uses atomic operations and is thread-safe. Prefer `Rc` in single-threaded code to avoid atomic overhead; use `Arc` only when sharing across threads.
- id: rust-smartptr-03
  answer: |
    Interior mutability allows mutation through a shared `&T` by checking borrowing rules at runtime. `RefCell<T>` tracks borrows dynamically and returns `Ref`/`RefMut` guards, panicking on violation. Compared with `&mut`, it has runtime checks and possible panic, and is not thread-safe.
- id: rust-smartptr-04
  answer: |
    `Rc<RefCell<T>>` combines shared ownership with runtime-checked mutable borrow, which is the common single-threaded idiom because neither type is thread-safe. The multi-threaded equivalent is `Arc<Mutex<T>>` or `Arc<RwLock<T>>`, using a lock for safe concurrent access.
- id: rust-concurrency-01
  answer: |
    `Send` means a value can be moved to another thread; `Sync` means a shared reference `&T` can be used from multiple threads safely. Rust uses them as bounds for threads and shared data, making data races and unsafe shared mutation compile errors unless synchronization provides the required guarantees.
- id: rust-async-01
  answer: |
    Calling an `async fn` returns a future representing the computation but does not execute it. It needs an executor or runtime such as Tokio/async-std, or `block_on`, to poll it; inside an async context, `.await` drives the future.
- id: rust-async-02
  answer: |
    `tokio::spawn` requires a `Send` future because tasks may move between threads; `Rc` is not `Send`, and holding a `MutexGuard` across `.await` can also be non-`Send` or cause lock-holding across yield points. Scope the guard, drop it before awaiting, use `Arc`, or use an async mutex such as Tokio's.
- id: rust-async-03
  answer: |
    Blocking or heavy synchronous work occupies an async executor thread, preventing other tasks from being polled and hurting concurrency; it can also deadlock. Move such work to `tokio::task::spawn_blocking`, a dedicated thread, or Rayon, and use async APIs instead of `std::thread::sleep`.
- id: rust-match-01
  answer: |
    Exhaustiveness means every possible value of the scrutinee must be covered by a match arm or wildcard. For enums this is especially valuable because adding a variant makes existing matches fail to compile, forcing callers to handle the new case explicitly.
- id: rust-match-02
  answer: |
    Use `if let` for a single pattern you care about while ignoring the rest; use `let ... else` when you want to bind one pattern and diverge (return/break/continue) on mismatch; use a full `match` for multiple branches, exhaustiveness, and clear handling of every case.
- id: rust-match-03
  answer: |
    When matching `&opt` where `opt: Option<T>`, `x` has type `&T`. This is caused by match ergonomics/default binding modes, which let patterns match through references and bind subvalues by reference.
- id: rust-match-04
  answer: |
    Destructuring binds fields or elements, e.g. `Some(x)`. Match guards add a runtime condition, e.g. `x if x > 0`. `@` bindings capture the whole matched value while also testing a subpattern, e.g. `name @ 1..=5`.
