- id: rust-ownership-01
  answer: |
    Assigning a `String` to another variable (`let b = a;`) moves the ownership. The heap data that `a` pointed to is now owned by `b`; `a` is no longer valid. Attempting to use `a` afterwards produces a compile-time error: "use of moved value: `a`". This is because `String` does not implement `Copy` — it owns heap memory, and Rust enforces single ownership to guarantee memory safety without a garbage collector. The move is a bitwise copy of the pointer, length, and capacity; `a` is then marked as moved-from and the compiler forbids its use. If you need both to be valid, you can clone explicitly (`let b = a.clone();`) or borrow (`let b: &String = &a;`).

- id: rust-ownership-02
  answer: |
    `Copy` is a marker trait indicating that a type's duplication is a simple bitwise copy — no custom logic, no resources to release. Types like integers, floats, `bool`, `char`, and tuples/arrays of `Copy` types are `Copy`. When a `Copy` value is assigned or passed, the original remains valid. `Clone` is a trait with an explicit method `.clone()` that performs a deep copy and can have custom logic (e.g., `String` and `Vec` allocate new heap memory). A type that implements `Drop` cannot also be `Copy` because `Drop` defines custom cleanup logic (releasing resources, freeing memory). If a `Copy` type also had `Drop`, bitwise copies would leave multiple owners of the same resource, and when each copy goes out of scope `Drop` would run multiple times, causing double-frees or use-after-free. The language forbids this combination to maintain memory safety.

- id: rust-ownership-03
  answer: |
    Passing a `Vec<T>` by value to a function transfers ownership — the function now owns the vector and is responsible for dropping it. The original variable is moved and cannot be used afterwards, which is why the compiler complains about a use-after-move. Options: (1) Pass by reference: `fn f(v: &[T])` or `fn f(v: &Vec<T>)` borrows it, leaving ownership with the caller. (2) Pass by mutable reference if mutation is needed: `fn f(v: &mut Vec<T>)`. (3) Pass by value but return the vector back: `fn f(v: Vec<T>) -> Vec<T>` so the caller gets ownership back. (4) Clone before passing if you need the original too.

- id: rust-borrowing-01
  answer: |
    Rust's core borrowing rule: at any given point in a program, you can have either one mutable reference (`&mut T`) or any number of immutable references (`&T`) to a value, but not both simultaneously. References must always point to valid data — no dangling pointers. This prevents data races at compile time: a data race requires concurrent mutation and access without synchronization, and the borrow checker ensures that if a mutable reference exists, no other references (mutable or immutable) exist. This eliminates use-after-free, double-free, and data races as compile errors rather than runtime bugs.

- id: rust-lifetimes-01
  answer: |
    A lifetime annotation like `'a` expresses a relationship between the lifetimes of references — it states that the referenced data must live at least as long as the annotation denotes. It does not change how long a value actually lives; it is purely a compile-time constraint that the borrow checker verifies. For example, `fn longest<'a>(x: &'a str, y: &'a str) -> &'a str` says both input references must live at least as long as `'a`, and the returned reference is valid for `'a`. Lifetimes ensure that references never outlive the data they point to, preventing dangling references. They describe scope relationships, not runtime duration.

- id: rust-lifetimes-02
  answer: |
    The lifetime elision rules are three rules that the compiler applies to infer lifetimes when annotations are omitted: (1) Each elided lifetime in a function's input becomes a distinct lifetime parameter. (2) If there is exactly one input lifetime, that lifetime is assigned to all elided output lifetimes. (3) If there are multiple input lifetimes but one of them is `&self` or `&mut self`, the lifetime of `self` is assigned to all elided output lifetimes. In `fn first(s: &str) -> &str`, rule 1 applies: the input gets a lifetime, rule 2 then ties the output to that same single input lifetime. This makes the common pattern of returning a reference derived from the argument concise and readable.

- id: rust-lifetimes-03
  answer: |
    `&'static T` is a reference with the `'static` lifetime — it points to data that lives for the entire program duration. This is typically string literals (stored in the binary's read-only data) or values promoted to `'static` via `Box::leak` or `const`/`static` items. It means the reference itself can be held indefinitely. In contrast, `T: 'static` as a trait bound means the type `T` does not contain any non-static references — either it owns all its data or only holds references that are themselves `'static`. It constrains what `T` can be, not the lifetime of a reference. For example, `fn f<T: 'static>(t: T)` means `T` has no borrowed data with a shorter lifetime than the program.

- id: rust-lifetimes-04
  answer: |
    `Box<dyn Error>` by default means `Box<dyn Error + 'static>` — the trait object contains no non-static references; it owns all its data. This is the common case for error types. When storing a trait object that borrows data with a shorter lifetime, you must specify it explicitly: `Box<dyn Error + 'a>` or a reference `&(dyn Error + 'a)`. The `+ 'a` syntax adds a lifetime bound to the trait object, saying the underlying data lives at least as long as `'a`. You need this when the trait object contains or wraps references with lifetimes shorter than `'static` — for instance, returning a boxed error that borrows from a local buffer.

- id: rust-traits-01
  answer: |
    Static dispatch via generics (`fn f<T: Trait>(t: T)`) uses monomorphization: the compiler generates a separate copy of the function for each concrete type used. This allows inlining and devirtualization, yielding zero-cost abstractions with the best possible performance — but it increases binary size and compile time. Dynamic dispatch via trait objects (`&dyn Trait` or `Box<dyn Trait>`) uses a vtable to look up method implementations at runtime. This has a small per-call indirection cost (vtable lookup) but allows heterogeneous collections (e.g., `Vec<Box<dyn Trait>>`) and reduces code bloat. Tradeoff: static dispatch is faster and more optimizable but less flexible; dynamic dispatch is more flexible with a small runtime cost.

- id: rust-traits-02
  answer: |
    In argument position, `fn f(x: impl Trait)` is syntactic sugar for a generic parameter `fn f<T: Trait>(x: T)`. The caller chooses the concrete type at the call site — the compiler monomorphizes for each distinct type used. In return position, `fn f() -> impl Trait` means the function returns some concrete type that implements `Trait`, but the caller does not know or choose that type — the function body (the callee) decides what concrete type to return. The caller can only use the type through the trait's interface. Argument-position generics are chosen by the caller; return-position impl Trait is chosen by the function implementation and is opaque to the caller.

- id: rust-traits-03
  answer: |
    The orphan rule (coherence) states that you can only implement a trait for a type if either the trait or the type is local to your crate. You cannot implement a foreign trait for a foreign type. So in your own crate, you cannot `impl Display for Vec<T>` because both `Display` (from std) and `Vec<T>` (from std) are foreign. This rule exists because the compiler must guarantee there is only one coherent implementation of a trait for any given type — without it, two crates could each implement `Display` for `Vec<T>` and the compiler would not know which to use. The workaround is the newtype pattern: define a wrapper struct `struct MyVec<T>(Vec<T>)` in your crate, then `impl Display for MyVec<T>`.

- id: rust-error-01
  answer: |
    Use `Option<T>` when the absence of a value is a normal, expected outcome — not an error. It signals "there may or may not be a value here" and the caller should handle both cases. Examples: looking up a key that might not exist in a map, finding the first element matching a predicate, parsing an optional field. Use `Result<T, E>` when the operation can fail and the failure carries meaningful information about what went wrong. The caller typically wants to know why something failed and may want to recover, retry, or propagate the error. `Option` has no error channel; `Result` carries a rich error type. In short: `Option` for presence/absence, `Result` for fallible operations with error details.

- id: rust-error-02
  answer: |
    The `?` operator on a `Result<T, E>` desugars roughly to: match the `Result` — if `Ok(v)`, unwrap and continue with `v`; if `Err(e)`, attempt to convert `e` into the function's error type via `From::from(e)`, then early-return `Err(converted_e)` from the enclosing function. The conversion uses the `From` trait, so if your function returns `Result<(), MyError>` and `MyError` implements `From<OtherError>`, then `?` on a `Result<_, OtherError>` will automatically convert. This is not just unwrapping — it performs the error conversion that makes error propagation ergonomic. For `Option`, `?` returns `None` early if the value is `None`.

- id: rust-error-03
  answer: |
    `panic!` (or `.unwrap()`, `.expect()`) is appropriate when continuing is impossible or meaningless — a violation of a fundamental invariant that the programmer must fix. Use it for: programming errors (index out of bounds, unreachable states, violated preconditions), situations where there is no reasonable recovery, or in examples/prototypes where returning a `Result` adds boilerplate. Return `Result` in library code, public APIs, and anywhere a caller might reasonably handle or recover from the failure — file not found, network timeout, parse failure, invalid user input. The general guidance: `panic!` for unrecoverable bugs and invariant violations; `Result` for expected or recoverable failures. Libraries should almost always return `Result` to give callers control over error handling.

- id: rust-error-04
  answer: |
    To wrap several underlying errors and work with `?`, the error type needs to implement the `std::error::Error` trait, which requires `Debug + Display`. It also needs `From` implementations (or a blanket impl) for each underlying error type so that `?` can convert them automatically. Additionally, the error should ideally provide a `source()` method for error chaining. `thiserror` helps by deriving the `Error` trait and `Display` impl via macros: `#[derive(Error, Debug)]` generates `Display` from `#[error("...")]` attributes, and `#[from]` generates `From` impls, making `?` work out of the box. It also generates `source()` from `#[source]` attributes, providing automatic error chain support.

- id: rust-iterators-01
  answer: |
    `v.iter().map(|x| expensive(x)).filter(|x| cond(x))` does no work on its own because iterators are lazy — they build a computation graph (a pipeline) but don't execute it until consumed. No elements are processed until a consumer pulls values through the chain. What makes it run is a consuming adapter like `.collect()`, `.for_each()`, `.sum()`, `.count()`, a `for` loop, or `.next()`. The consumer drives the iterator: for each element it needs, it calls `next()` on the outermost iterator (e.g., `Filter`), which in turn pulls from `Map`, which pulls from `Iter`. This lazy evaluation allows infinite iterators, short-circuiting (e.g., `.take(5)`), and avoiding intermediate allocations.

- id: rust-iterators-02
  answer: |
    `iter()` borrows the collection immutably, yielding `&T` references. The collection is unchanged and remains usable. `iter_mut()` borrows the collection mutably, yielding `&mut T` references that allow in-place mutation. The collection is borrowed for the iteration's duration. `into_iter()` takes ownership of the collection by value, yielding owned `T` values. The collection is consumed and cannot be used afterwards (unless the iterator returns it via `into_iter` semantics on certain types). For arrays, `into_iter` yields owned values; for `Vec<T>`, `into_iter` yields owned elements. `iter` and `iter_mut` borrow; `into_iter` moves.

- id: rust-iterators-03
  answer: |
    `collect()` fails with "type annotations needed" when the compiler cannot infer the target collection type from context. Iterators produce items of some type, but `collect()` can build many different types (`Vec`, `HashSet`, `String`, `Result`, etc.), so without a hint it doesn't know which. You fix it by providing a type annotation: `let v: Vec<i32> = iter.collect();` or with turbofish `iter.collect::<Vec<i32>>()`. Collecting into a `Result` is special: `Result<T, E>` implements `FromIterator`, so `collect()` can short-circuit — if any item is `Err`, the whole collection becomes that `Err`; if all are `Ok`, it's `Ok(collected)`. This is used with `?` for fallible collection operations.

- id: rust-iterators-04
  answer: |
    The `move` keyword on a closure changes what the closure captures: without `move`, the closure borrows captured variables (by reference if possible, by mutable reference if needed). With `move`, the closure takes ownership of the captured variables — it moves them into the closure. This is needed when returning a closure or iterator that captures a local variable that would otherwise be dropped when the function returns, leaving dangling references. `move` makes the closure self-contained, owning its captures, so it can be returned or sent across thread boundaries. The tradeoff is that the captured values are moved and cannot be used in the original scope afterwards.

- id: rust-smartptr-01
  answer: |
    `Box<T>` provides heap allocation with a single owning pointer. It gives you: (1) indirection — storing a large value on the heap while keeping a small pointer on the stack; (2) recursive types — types that contain themselves need indirection to have a finite size (e.g., `enum List { Cons(i32, Box<List>), Nil }`); (3) trait objects — `Box<dyn Trait>` for dynamic dispatch; (4) transferring ownership of large data without copying. A genuine need is a recursive data type: without `Box`, a type that refers to itself directly would have infinite size. Another case is when you need a type whose size is unknown at compile time but want ownership semantics, or when you want to ensure a large struct isn't copied on assignment.

- id: rust-smartptr-02
  answer: |
    `Rc<T>` (reference counting) is for single-threaded shared ownership — it uses non-atomic reference counting and is `!Send` and `!Sync`, so it cannot cross thread boundaries. `Arc<T>` (atomic reference counting) uses atomic operations for the reference count, making it `Send + Sync`, allowing shared ownership across threads. You don't always use `Arc` because atomic operations have a measurable performance cost (memory barriers, potential cache contention) compared to `Rc`'s non-atomic increments. In single-threaded code, `Rc` is cheaper and sufficient. Additionally, neither provides interior mutability — both need `RefCell` or `Mutex` for mutation through shared references, and `Rc<RefCell<T>>` has no atomic guarantee against logical races (runtime borrow checking).

- id: rust-smartptr-03
  answer: |
    Interior mutability is a pattern where you can mutate data through an immutable shared reference (`&T`), bypassing the normal rule that `&T` is read-only. `RefCell<T>` provides this at runtime: it enforces borrowing rules (one mutable borrow OR multiple immutable borrows) dynamically rather than at compile time. When you call `borrow()` you get a `Ref<T>` (immutable), and `borrow_mut()` gives `RefMut<T>` (mutable). The cost compared to a normal `&mut` is: runtime overhead (borrow flag tracking), potential panics at runtime if you violate the rules (two mutable borrows), and it breaks the compile-time guarantee — bugs that the compiler would catch with `&mut` become runtime panics. The compiler cannot statically verify `RefCell`'s borrow discipline, so violations panic at runtime instead.

- id: rust-smartptr-04
  answer: |
    `Rc<RefCell<T>>` combines shared ownership with interior mutability in single-threaded code: `Rc` allows multiple owners of the same data (shared ownership), and `RefCell` allows mutation through a shared reference (interior mutability). Together they let multiple parts of your code hold references to the same mutable data. The multi-threaded equivalent is `Arc<Mutex<T>>` or `Arc<RwLock<T>>`: `Arc` provides thread-safe shared ownership (atomic reference counting), and `Mutex`/`RwLock` provides thread-safe interior mutability with proper synchronization. Unlike `RefCell`, which panics on concurrent borrow violations, `Mutex` blocks or errors, making it safe for concurrent access.

- id: rust-concurrency-01
  answer: |
    `Send` is a marker trait indicating a type's ownership can be safely transferred to another thread — the type has no thread-local state or references that would become invalid. `Sync` indicates a type can be safely shared between threads via references — `T: Sync` means `&T` is `Send`. Every type is `Send + Sync` unless it contains non-`Send`/`Sync` components (like `Rc`, `Cell`, `RefCell`). Rust uses these traits at compile time: `spawn` requires the closure to be `Send`, and types like `Mutex<T>` are `Sync` only if `T: Send`. This makes data races a compile error: you cannot share a `!Sync` type across threads via `Arc`, and you cannot move a `!Send` type to another thread. The type system enforces that mutable data is either protected by synchronization or confined to one thread.

- id: rust-async-01
  answer: |
    Calling an `async fn` doesn't run it — it returns a value of an opaque `Future` type that represents the computation. The future is lazy; nothing happens until it is polled. The standard library ships no executor; it defines `Future`, `Poll`, `Context`, and `Waker` but nothing that polls a future to completion. To make it execute, you need a runtime that provides an executor: Tokio, async-std, smol, or `futures::executor::block_on`. The future is driven either by `.await` inside another async context or, at the top level, by an executor polling it. Without an executor, the future sits inert and never runs.

- id: rust-async-02
  answer: |
    The compiler turns an `async fn`/block into a state machine; every value that is still alive at an `.await` point is saved as a field of that state machine. So the future is `Send` only if every such held value is `Send`. An `Rc` is `!Send` (non-atomic refcount, not thread-safe), and a `std::sync::MutexGuard` is `!Send` (the guard unlocks the mutex on drop, and if moved to another thread it could unlock on the wrong thread). Holding either across an `.await` makes the whole future `!Send`. `tokio::spawn` on the multi-threaded runtime requires `Send` because the task may resume on a different worker thread. Fixes: drop or scope the value so it ends before the `.await`, switch to `Arc` / `tokio::sync::Mutex`, or use `spawn_local` / a `LocalSet`.

- id: rust-async-03
  answer: |
    Rust async has no preemption. A task runs until it reaches an `.await` whose future returns `Poll::Pending`; only then does the executor regain the thread. A blocking call (`std::thread::sleep`, sync file/network IO, a long CPU loop, a contended `std::sync::Mutex`) never reaches a yield point, so the worker thread is held and every other task scheduled on it stalls. This causes worker starvation, latency spikes, and stalled timers. Fixes: use `tokio::time::sleep` for delays, async IO (tokio's file/net APIs), `tokio::task::spawn_blocking` for blocking operations, or rayon for CPU-bound work. The mechanism is cooperative scheduling: tasks yield only at `.await` that returns `Pending`, so anything that blocks prevents yielding.

- id: rust-match-01
  answer: |
    `match` being exhaustive means the compiler requires every possible value of the matched type to be covered by at least one pattern. You cannot write a `match` that silently ignores some cases — the compiler will reject it. This is especially valuable for enums because the compiler knows all variants and can enforce that you handle each one. If you add a new variant to an enum, the compiler will flag all `match` expressions that don't handle it, forcing you to update them. This prevents the runtime bug of forgetting to handle a case. You can use a wildcard `_` catch-all to explicitly ignore remaining variants, but you must do so deliberately.

- id: rust-match-02
  answer: |
    Use `if let` when you only care about one specific pattern and want to ignore all others — it's syntactic sugar for a `match` with a single arm and a wildcard. Use `let ... else` (let-else) when you want to destructure a value and return/early-exit if the pattern doesn't match: `let Some(x) = opt else { return; };`. It keeps the extracted variable in scope for the rest of the function. Use a full `match` when you need to handle multiple distinct patterns with different logic for each, especially when matching on enums where exhaustiveness is required. `if let` is for one pattern; `let ... else` is for destructuring with early exit; `match` is for multi-pattern exhaustive handling.

- id: rust-match-03
  answer: |
    When you `match` on a reference — `match &opt { Some(x) => ... }` — the type of `x` is `&T` (a reference to the inner value), not `T`. This is caused by match ergonomics (default binding modes), a Rust 2018 feature. When you match on `&opt`, the compiler automatically dereferences the reference and switches the binding mode to `ref`, so patterns bind by reference. So `Some(x)` binds `x` as `&T`. If you wanted `T` by value you'd need to move out of the reference (which may not be possible) or match on `opt` directly (which moves). Match ergonomics eliminates the need for explicit `ref` and `ref mut` keywords in most common matching scenarios.

- id: rust-match-04
  answer: |
    Destructuring breaks compound values apart in patterns: `Point { x, y }` extracts fields, `Some(x)` extracts the inner value, `(a, b)` unpacks tuples, `Cons(head, tail)` unpacks enum variants. It lets you bind multiple sub-values at once and reach into nested structures. Match guards add an `if` condition to a pattern arm: `Some(x) if x > 0 => ...` — the arm only matches when the guard expression is `true`. Guards enable filtering beyond what patterns alone can express (e.g., range checks, arbitrary conditions). `@` bindings let you bind a variable to the whole value while also destructuring it: `n @ 1..=5 => ...` binds `n` to the entire matched value while also constraining it to the range 1-5. This gives you both the destructured parts and the original value in scope.
