# step-5-preview-free — closed-book answers for rust (opencode-go, 2026-10-12); answerer saw only _prompts/rust.md
Closed-book, no tools used. Answers below are from my own knowledge.

- id: rust-ownership-01
  answer: |
    The String is *moved* into `b`: assignment of a non-`Copy` value transfers ownership of the heap buffer (ptr/len/capacity) to `b`, leaving `a` uninitialized. Using `a` after that is a compile error ("borrow of moved value" / "value used after move"). The reason is single ownership: the compiler must statically know exactly one binding is responsible for freeing that heap allocation, so it can insert exactly one `drop`. If both `a` and `b` remained usable, the buffer would be freed twice (double free). Moves only happen on assignment/passing — `b.clone()` would instead make an independent copy and leave `a` valid.

- id: rust-ownership-02
  answer: |
    `Copy` is a marker trait for values duplicable by a trivial bitwise copy (integers, bools, floats, shared references, tuples/arrays of `Copy`s). On assignment or passing, the value is copied automatically, the source stays valid, and no destructor runs for the "moved" copy. `Clone` is a real method that produces an independent value — usually doing actual work (e.g. a heap allocation for `String`/`Vec`) — and must be invoked explicitly (`.clone()`).
    A `Drop` type cannot be `Copy` (the compiler rejects it: E0184). The reason is that `Copy` makes moving a no-op memcpy rather than a transfer of ownership; if a resource-owning type were copied, both copies would run their destructor and the resource would be released twice. `Copy` asserts "moving is just a bitwise copy", `Drop` means "moving is a semantic event" — they are mutually exclusive by design.

- id: rust-ownership-03
  answer: |
    Passing by value *moved* the `Vec` into the function: ownership of the heap buffer transferred, so your binding is uninitialized and the compiler refuses any later use — it can't prove the callee didn't consume or drop it.
    Options:
    - return ownership back from the function (hand it back or return a derived value),
    - pass by shared reference — `&Vec<T>`, and preferably `&[T]` if it only reads,
    - pass `&mut Vec<T>` if the function should mutate in place and you keep the collection,
    - `clone()` it if the callee genuinely needs its own independent copy you also keep using,
    - `std::mem::take(&mut v)`: hands the value over and leaves an empty `Vec` behind.

- id: rust-borrowing-01
  answer: |
    The rule: at any moment you may have *either* exactly one mutable reference (`&mut T`) *or* any number of shared references (`&T`) to a given value, and every reference must always be valid (no dangling). Mutating through `&mut` also forbids anybody aliasing that value, and shared references can't be used to mutate.
    Bugs it prevents at compile time: data races (concurrent read+write, or two writes), iterator invalidation (mutating a collection while iterating it — the borrow checker forbids iterating and mutating simultaneously), use-after-free / returning references to locals, and aliasing `&mut`s (which is what unsafe optimizations like rustc/LLVM's `noalias` attribute and register caching rely on). It costs nothing at runtime — it's the same reference representation as in C, but the aliasing discipline is statically verified.

- id: rust-lifetimes-01
  answer: |
    `'a` *names a region of code* — an interval during which a reference must be valid — so that the compiler can talk about and check relationships between regions (e.g. "this returned reference outlives both of these inputs"). It does **not** change how long anything lives: lifetimes are purely a compile-time proof mechanism, erased during codegen with no runtime representation. How long a value actually lives is decided by ownership, scope, and `drop`; the annotation merely asserts constraints the borrow checker then verifies. You can write a longer `'a` than any real call can supply, and it just fails to compile at the call site where the region is too short.

- id: rust-lifetimes-02
  answer: |
    The three elision rules:
    1. Every elided lifetime in an *input* position becomes its own fresh lifetime parameter.
    2. If there is exactly **one** input lifetime, it is assigned to all elided *output* lifetimes.
    3. If there are several input lifetimes but one is `&self`/`&mut self`, the lifetime of `self` is assigned to all elided output lifetimes.
    So `fn first(s: &str) -> &str` desugars to `fn first<'a>(s: &'a str) -> &'a str`. If no rule resolves the output (e.g. multiple non-`self` inputs, elided output), elision does not apply and you must write the lifetime explicitly.

- id: rust-lifetimes-03
  answer: |
    `&'static T`: a *reference* that is valid for the entire program — string literals, `static` items, promoted constant borrows.
    `T: 'static`: a *bound* saying `T` itself contains no borrowed data with a shorter lifetime — it is self-contained/owned.
    These are frequently conflated, but they are different things. `T: 'static` does **not** mean stored forever: owned types like `String` or `Vec<u8>` satisfy `T: 'static` even though instances are dropped when their scope ends. It only promises the type never borrows something that doesn't live forever. That is why `thread::spawn` and `Box<dyn Trait + 'static>` require it: the callee must be sure the value cannot dangle into the caller's stack frame.

- id: rust-lifetimes-04
  answer: |
    `Box<dyn Error>` defaults to `Box<dyn Error + 'static>` — the erased concrete type must not borrow anything non-`'static`. If your error type holds e.g. a `&'a str`, or borrows data with lifetime `'a`, you must write the shorter bound explicitly: `Box<dyn Error + 'a>`. Rule of thumb: for *owned* holders (`Box<dyn Trait>`, trait objects stored by value) the object lifetime defaults to `'static`, and you narrow it with `+ 'a` whenever the hidden type borrows with a shorter lifetime. For a bare reference `&'a dyn Trait` there is no such problem: the object lifetime defaults to the reference's own lifetime `'a`, so it is bounded by the borrow automatically.

- id: rust-traits-01
  answer: |
    Static dispatch (`fn f<T: Trait>`) is monomorphization: the compiler emits a specialized concrete copy of `f` for every `T` used, so calls are direct, devirtualized and inlinable — normally fastest, with no vtable. Costs: code bloat (one copy per instantiation) and longer compile times, plus each `T` yields a *distinct* type, so you can't put heterogeneous instantiations in one collection or return a single unnameable type without boxing. Dynamic dispatch (`&dyn Trait`) is a fat pointer (data + vtable) with one compiled function and an indirect call, generally not inlinable — slower per call, but the type is erased and uniform: you can store different concrete types behind one interface, in one `Vec<Box<dyn Trait>>`, with smaller code. Tradeoff in short: per-call speed and inlinability versus type erasure, binary size, and runtime flexibility (plus the object-safety restrictions that trait objects need — no generic methods, limited `Self` use).

- id: rust-traits-02
  answer: |
    In argument position, `impl Trait` is just anonymous sugar for a generic parameter: `fn f(x: impl Trait)` == `fn f<T: Trait>(x: T)`. The **caller** chooses the concrete type at the call site, and the function is monomorphized per call type. (Note: two separate `impl Trait` parameters mean two *independent* type params, so they may be different types.)
    In return position, `impl Trait` is an **opaque type chosen by the implementation**: the caller only knows the return implements `Trait`, cannot name or choose the concrete type, and two functions returning `impl Trait` yield incompatible types. This is what lets you hand out closures or iterator adapters whose real types are unnameable, while still keeping the public signature concrete and allocation-free.

- id: rust-traits-03
  answer: |
    The orphan rule (a.k.a. coherence): you may implement a trait for a type only if the **trait or the type is local to your crate** (modulo `#[fundamental]` wrappers like `&`/`Box`). Its purpose is to guarantee coherence — that across the whole ecosystem there is at most one `impl` of a given trait for a given type — so the compiler always knows which impl applies, and a new impl somewhere can never silently change the meaning of somebody else's code.
    `impl Display for Vec<T>` is illegal because both `Display` (std) and `Vec` (std) are foreign; if it were allowed, two crates could each supply a conflicting impl for the same pair. The standard workaround is the newtype pattern: `struct MyVec(Vec<T>); impl fmt::Display for MyVec { ... }` — `MyVec` is local, so the impl is orphan-legal.

- id: rust-error-01
  answer: |
    Use `Option<T>` when the absence of a value is a normal, expected outcome that is not a failure: a lookup miss, end of input, an empty slot, "maybe nothing". Use `Result<T, E>` when an operation *failed* and the caller might reasonably want to know why, report it, or recover: I/O errors, parse failures, validation errors.
    The useful liability rule: whenever a genuine error occurred and its cause matters, return `Result` and carry the cause — don't flatten an error into `None` and throw the information away. Don't use `Option` to smuggle errors, and don't use `Result` where nothing can be done with the error anyway.

- id: rust-error-02
  answer: |
    In a function returning `Result<U, F>`, applied to a `Result<T, E>`: on `Ok(v)` it unwraps and evaluates to `v`, continuing the function; on `Err(e)` it returns early from the enclosing function with `Err(From::from(e))` — the conversion is done by `From`, mapping the source error `E` into the function's error type `F`. Precisely: `?` is sugar (via the `Try`/`FromResidual` traits) for `match r { Ok(v) => v, Err(e) => return Err(FromResidual::from_residual(Err(e))) }`, and `Result`'s `FromResidual` uses `From::from`.
    That conversion is why `?` "just works" whenever the target error implements `From<E>` for each source error, and why `anyhow::Error` and `Box<dyn Error>` interoperate with every error. It also works on `Option` (early-returns `None`) and other `Try` types. It requires the enclosing function's return type to be compatible — you can't use it in a `fn` returning `()` or an unresolvable error type.

- id: rust-error-03
  answer: |
    `panic!`/`.unwrap()` for programmer errors: broken invariants, `unreachable!()` states, violated preconditions, out-of-bounds arithmetic — situations where the program's logic is provably wrong and no caller could do anything useful. Also acceptable in tests, examples, prototypes, and at program startup where failing fast is correct, and prefer `.expect("reason")` so the message documents *why* panic is impossible.
    `Result` for anything expected and recoverable: invalid user input, missing files, network timeouts, parse failures — anywhere an operator or program could retry or degrade. Library code should almost never panic (a panic is a bug in *your* consumption contract that the caller cannot defend against); binaries may panic more liberally, and many teams additionally compile with `panic = "abort"` for binaries so a panic is fail-stop instead of an unwinding hazard. Never unwrap on untrusted input.

- id: rust-error-04
  answer: |
    What the error type needs to work with `?`: one variant per wrapped error, an `impl From<E>` for each underlying error `E` (because `?` converts via `From::from`), and `Display` + `std::error::Error` so it interoperates with the standard error machinery (`Box<dyn Error>`, `Error::source()`).
    `thiserror` generates all that boilerplate from a derive: `#[derive(thiserror::Error)]`, `#[error("...")]` strings for the `Display` impl, `#[from]` on a field to generate the `From` impl (and treat it as the `source`), and `#[source]` for explicit source chaining. The result is a concrete, zero-allocation, `match`-able enum error with a stable public API — which is what library consumers need.
    Contrast with `anyhow`, which is for *applications*: a single opaque boxed error with backtraces and `?`-everywhere convenience, but it deliberately hides the concrete type so callers can't match on it — wrong choice for a library's public API.

- id: rust-iterators-01
  answer: |
    Because iterators are lazy: `.map`/`.filter` are adapters that wrap the upstream iterator in a new adapter struct and store your closures — nothing is executed at that point. The chain only runs when a **consumer** starts calling `next()` on it: a `for` loop, `collect()`, `sum()`/`count()`/`max()`, a direct `.next()`, etc. Each `next()` pulls a single element end-to-end through the chain, so `filter`'s predicate runs for each item and `map`'s closure only for those that pass. This is also what makes `(0..).filter(...).take(5)` terminate even though the source is infinite, and lets the pipeline be fused into one per-element pass rather than building intermediate collections.

- id: rust-iterators-02
  answer: |
    `iter()` yields `&T` — shared references to each element. The collection is only borrowed and remains fully usable afterwards.
    `iter_mut()` yields `&mut T` — a mutable reference to each element in turn. The collection is not moved, but while iterating, nothing else may borrow it; you can mutate elements in place.
    `into_iter()` yields *owned* values (`T` for the owned form, e.g. `Vec<T> -> T`). It **consumes/moves** the collection, so the original binding is unusable afterwards. (Note `&Vec<T>` also implements `IntoIterator` yielding `&T`, which is why `for x in &vec` iterates by reference.)

- id: rust-iterators-03
  answer: |
    `collect()` is generic over its output type `B: FromIterator<Item>` — and often the element type is also unconstrained. When neither the target collection nor the element type can be inferred from context, the compiler has no way to pick an instantiation and asks for annotations. You fix it by naming the type: `let v: Vec<i32> = ...; v`, turbofish `it.collect::<Vec<_>>()` / `.collect::<BTreeSet<_>>()`, or passing the result to something with a declared type.
    What's special about `Result`: `FromIterator` is implemented for `Result<Vec<T>, E>`, so `it.collect::<Result<Vec<_>, E>>()` short-circuits. It returns `Ok(collected)` only if *every* element was `Ok`; on the first `Err` it stops immediately and returns that `Err`. This is the idiomatic way to both propagate an error and collect results (same mechanism gives you `Iterator::collect::<Option<Vec<_>>>`).

- id: rust-iterators-04
  answer: |
    By default a non-`move` closure captures variables by reference (`&`/`&mut`) wherever possible, so it borrows your locals. Returning such a closure from a function fails — it would hold a borrow of stack memory that is about to die (hence the compiler's `move` suggestion), and likewise `thread::spawn` would reject it.
    `move` changes the capture: all used variables are moved (or copied) *into* the closure's anonymous struct, so the closure owns them and no longer borrows anything from the enclosing scope. It doesn't change *how* the body reads them — the values are used exactly the same way — it only changes *ownership of the captures*. The trade-off: after a `move` capture you can generally no longer use those variables in the enclosing scope (unless they were `Copy`), and if you need to keep using an outer binding, capture a clone deliberately.

- id: rust-smartptr-01
  answer: |
    `Box<T>` gives you single-owner heap allocation: it's a pointer with no refcount or borrow state, it derefs to `T`, its own size is one machine word regardless of `T`'s size, it drops the heap value when it goes out of scope, and it has no runtime cost beyond the allocation itself.
    Cases where you genuinely need it:
    - recursive types, where direct embedding would make the size infinite/unknowable: `enum List { Cons(i32, Box<List>), Nil }`, tree/trie nodes — the indirection breaks the recursion,
    - storing an unsized trait object: `Box<dyn Trait>`,
    - handing off ownership of a big buffer cheaply (move the pointer, not the data),
    - boxed slices (`Box<[T]>`), allocator/FFI-specific placement.
    What it does *not* give: shared ownership or interior mutability.

- id: rust-smartptr-02
  answer: |
    `Rc<T>` is non-atomic reference counting for single-threaded shared ownership; `Arc<T>` uses atomics (CAS) and is thread-safe — `Arc<T>` is `Send`/`Sync` when `T` is, so it can be shared across tasks and threads, while `Rc<T>` is deliberately `!Send`/`!Sync`.
    Why not always use `Arc`: (1) `Rc`'s increments/decrements are plain loads and stores, while `Arc` touches an `AtomicUsize` — measurably more expensive per clone/drop, especially under contention; (2) `Arc` discards the compiler-enforced single-thread guarantee, letting latent races accumulate in code you believe is thread-confined; (3) `Rc`'s `!Send` doubles as a safety net that localization can't be broken by a later `thread::spawn`.
    Pick `Rc` when the data is provably confined to one thread; `Arc` only when sharing across threads is actually required (then `Arc<Mutex<T>>`/`Arc<RwLock<T>>` for mutation).

- id: rust-smartptr-03
  answer: |
    Interior mutability is mutation through a shared `&` reference — an escape hatch from the aliasing rules where the borrow checker's static guarantees are instead enforced at *runtime*.
    `RefCell<T>` keeps a runtime borrow flag: `borrow()` returns a `Ref` guard and panics if a `RefMut` is live; `borrow_mut()` returns a `RefMut` and panics if any other borrow is live; guards release on drop, keeping the count in sync.
    The cost versus a normal `&mut`: (a) the aliasing discipline is no longer compile-time, so a violation becomes a *panic* instead of a compile error; (b) small runtime overhead — flag check/increment per borrow plus guard bookkeeping; (c) `RefCell` is `!Sync`, so it can't be shared across threads at all; (d) a long-lived or leaked guard can poison the state or double-panic. `Cell<T>` is the cheaper sibling for `Copy` values that never need a reference-out.

- id: rust-smartptr-04
  answer: |
    `Rc<T>` only hands out `&T` (via `Deref`), so with many shared owners you cannot obtain a `&mut T` at all. `Rc<RefCell<T>>` splits the two concerns: the `Rc` layer provides shared ownership — N owners each hold a clone of the same `Rc`, all pointing at one allocation which is freed when the count hits zero — while the `RefCell` layer supplies mutation through those shared handles (interior mutability, runtime-checked). Because `Rc`'s refcount is non-atomic and `RefCell` is `!Sync`, the whole thing is confined to one thread, and single-threaded graphs with cycles/back-pointers usually make `Rc::new` + `Rc::clone` for each edge.
    Multi-threaded equivalent: `Arc<Mutex<T>>` (or `Arc<RwLock<T>>` for read-mostly workloads; `Arc<AtomicUsize>`/lock-free structures when that suffices). `Arc<RefCell<T>>` is still `!Sync` and therefore not thread-shareable — `Mutex`/`RwLock` are the correct swap-in because they serialize access and become `Sync` when `T: Send`.

- id: rust-concurrency-01
  answer: |
    `Send` (unsafe *auto* trait): ownership of `T` may be transferred to another thread — moving it cannot create unsafe concurrent access. Most owned data is `Send`; exceptions are `Rc<T>` (non-atomic refcount), raw pointers, `Cell`/`RefCell`, and `MutexGuard` (must see the same thread as its caller).
    `Sync`: `&T` may be *shared* across threads, i.e. many threads may hold `&T` simultaneously without data races — equivalently, `T: Sync` iff `&T: Send`. Data with non-atomic interior mutability (`Cell`, `RefCell`, `Rc`) is `!Sync`; `Mutex<T>`/`RwLock<T>` are `Sync` when `T: Send`, because locking serializes access.
    Both are computed automatically from a struct's fields (the compiler derives them), overridable only in `unsafe` code. Rust uses them as compile-time bounds: `thread::spawn` and channel sends require `Send`; sharing `&T` across threads requires `Sync`. Combined with the borrowing rules ("one `&mut` or many `&`, never both"), this makes data races impossible to express — code that would race is rejected at compile time instead of corrupting memory later.

- id: rust-async-01
  answer: |
    Calling an `async fn` returns a `Future` — an inert value of an anonymous, unnameable type implementing `std::future::Future` that has captured the arguments. None of the body has run.
    To make it execute, something must **poll** it: typically you `.await` it inside another async task (task is a future driven by the runtime), or you hand it to an executor — `tokio::runtime::Runtime::block_on`, `tokio::spawn` (which schedules it onto the runtime), or poll it manually via `poll()` with a `Context`. Each poll runs until the next `.await` point that isn't ready, returning `Poll::Pending`; the future must arrange for the stored `Waker` to be called when progress becomes possible, so the executor knows to poll it again. Without a runtime polling it, a future sits there doing nothing.

- id: rust-async-02
  answer: |
    It won't compile because `tokio::spawn` requires the future to be `Send + 'static`: the multi-threaded runtime may run the task on *any* worker thread and later move it, so everything alive across an `.await` point must be `Send`.
    Holding an `Rc` across an `.await` makes the future `!Send` — `Rc`'s non-atomic refcount would be mutated concurrently from two threads. A `std::sync::MutexGuard` is likewise deliberately `!Send`, because a guard must be locked and unlocked on the *same* thread; letting one migrate would break the mutex's mutual exclusion (and it holds the lock across a suspension point, which is usually wrong anyway even aside from `Send`).
    Fixes: use `Arc` instead of `Rc`; scope the guard so it's dropped *before* the `.await`; for lock-across-await, prefer `tokio::sync::Mutex` (its guard is `Send`), or run non-`Send` work with `spawn_local` on a `LocalSet` (a `!Send` future can only be spawned there).

- id: rust-async-03
  answer: |
    An async runtime is cooperative: a small set of worker threads runs many tasks, and each is expected to reach an `.await` quickly and yield the thread. A blocking call (`std::thread::sleep`, synchronous file I/O, a CPU-heavy loop, a `std::sync::Mutex` lock held during a wait) doesn't cooperate — the runtime sees the task as *running*, cannot preempt it, and cannot poll other ready tasks on that thread. So every task on that worker starves for the duration of the blocking call: a 100 ms sleep stalls hundreds of otherwise-ready tasks and defeats the concurrency the runtime exists to provide.
    What to do instead: use the async-native equivalents — `tokio::time::sleep`, async file/network I/O — and for genuinely blocking or CPU-heavy work, move it off the reactor with `tokio::task::spawn_blocking` (a separate, larger blocking thread pool) or `rayon` for CPU parallelism. In long CPU loops, insert `tokio::task::yield_now()` periodically so other tasks get a turn. The general rule: never hold the worker thread without making progress at an `.await` point.

- id: rust-match-01
  answer: |
    Exhaustive means every possible value of the scrutinee's type must be handled — for an enum, every variant must be covered by some arm, or a `_` catch-all must exist; otherwise it's a compile error ("non-exhaustive patterns").
    This is especially valuable for enums because you get a compiler-generated checklist: adding a new variant breaks compilation at *every* `match` on that enum, forcing the author to consciously handle the new case. That eliminates the classic C/C++ `switch` bug where a `default:` silently absorbs an added case and the new variant is mishandled until it fails at runtime — enum evolution becomes a compile-error list instead of a debugging session. (For non-enum types — integers, strings — exhaustiveness over all values is impractical, so a `_` arm is expected; and an external crate's enum is "non-exhaustive" for you, which is why you're forced to keep a `_`.)

- id: rust-match-02
  answer: |
    `if let P = e { .. } else { .. }` — sugar for a `match` with one interesting arm. Use when you only care about one variant and the rest is a no-op or uninteresting. It is deliberately *not* exhaustive, so the compiler will not remind you about the other cases.
    `let P = e else { .. }` — when the *happy path* depends on the pattern matching and failure must diverge (`return`, `continue`, `break`, `panic`, `?`). The `else` block must break control flow, not produce a value. It's the right replacement for `let x = if let Some(x) = o else { return }` and for `.unwrap()` in early-exit code, keeping the non-matching path flat instead of nesting the real logic a level deep.
    Full `match` — whenever you must handle several variants, want exhaustiveness checking (the compiler flags you when a variant is added), or need bindings/guards. Rule of thumb: `if let` for "only this one shape matters", `let ... else` for "must-have-or-bail", `match` when the number of choices or their completeness matters.

- id: rust-match-03
  answer: |
    With `match &opt { Some(x) => .. }`, `x` has type `&T` (e.g. `&i32`) — a shared reference to the inner value; nothing is moved or copied. This is "match ergonomics" (default binding modes), introduced in RFC 2005: when the scrutinee is a reference (`&T` or `&mut T`) and the pattern is a non-reference pattern like `Some(x)`, the compiler automatically dereferences the scrutinee and shifts the default binding mode of the sub-patterns to `ref` (or `ref mut` for `&mut T`), so inner bindings become references implicitly. That's why the older explicit `Some(ref x)` is now unnecessary — and why matching on `&mut opt` gives `x: &mut T`.
    (If you instead write the outer reference in the pattern — `&Some(x)` — it dereferences the reference itself and binds by value, which only compiles if the move is legal, e.g. `T: Copy`; you'd otherwise get "cannot move out of a shared reference".)

- id: rust-match-04
  answer: |
    Destructuring breaks a composite value into its parts inside the pattern itself, binding them directly instead of field-by-field access afterwards: structs `Point { x, y }` (with `x, ..` to take the rest implicitly), tuples `(a, b)`, enums `Some(v)` / `Err(e)`, slices `[head, .., tail]`, nested arbitrarily — and with `ref`/`ref mut` bindings when you want to borrow rather than move.
    Match guards add a condition after the pattern: `Some(x) if x > 10 => ..`. They express predicates patterns alone can't (comparisons, calls, referring to a variable bound earlier in the same pattern). If the guard is false, matching continues with later arms; guards cannot produce the value themselves, bindings from the pattern remain visible inside the guard, and a guarded arm still counts toward exhaustiveness only via its pattern (a false guard is not accounted for, which is why `_ => ..` is still needed).
    `@` bindings bind the *whole matched value* to a name while also testing a sub-pattern: `n @ 1..=5 => ..` gives both the range test and `n` holding the value; `msg @ Some(_)`, `x @ _ if cond(x)`. That's what a pattern or a guard alone cannot do — a range/wildcard gives no name, and a guard gives no pattern test — and the value must not have been partially moved for the binding to be usable.
