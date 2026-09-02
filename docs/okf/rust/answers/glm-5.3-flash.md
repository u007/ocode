```yaml
- id: rust-ownership-01
  answer: |
    The String's ownership is *moved* into `b`: the stack representation (ptr/len/capacity) is bitwise copied to `b`, and `a` becomes invalid. Any later use of `a` is a compile-time error (E0382, "borrow of moved value"). This is deliberate: if both `a` and `b` believed they owned the same heap buffer, dropping both would double-free. Rust prevents this statically by invalidating the source of a move instead of doing a deep copy (copying is opt-in via `.clone()`).

- id: rust-ownership-02
  answer: |
    `Copy` means assignment/passing duplicates the value implicitly (bitwise copy) and the original remains valid. `Clone` means duplication is explicit via `.clone()`. Every Copy type is Clone (`trait Copy: Clone`), but not vice versa. A type implementing `Drop` cannot be `Copy` because implicit copies would make it impossible to guarantee the destructor runs exactly once — each implicit copy would own resources that need freeing, so a plain assignment could cause double-frees or leaked/duplicated cleanup. Copy is restricted to "plain data" (all fields Copy, no destructor).

- id: rust-ownership-03
  answer: |
    Passing `Vec<T>` by value *moved* ownership into the function; when the function returns, the Vec is dropped (unless returned), so the caller's variable is invalid — using it is the "value moved" error. Options: (1) pass a borrow `&Vec<T>` / better `&[T]` (or `&mut` if it must mutate); (2) clone the Vec before passing (`v.clone()`); (3) have the function return the Vec back; (4) restructure so the caller doesn't need it afterwards.

- id: rust-borrowing-01
  answer: |
    Core rule (aliasing XOR mutability): at any time a value may have either any number of shared/immutable references `&T`, or exactly one mutable reference `&mut T` — never both — and the referent must outlive all references. This prevents at compile time: data races (unsynchronized concurrent read+write), iterator invalidation, use-after-free/dangling references, and mutation-while-observed bugs that in other languages are runtime races or undefined behavior.

- id: rust-lifetimes-01
  answer: |
    A lifetime like `'a` is a generic *parameter over regions of the program*, used to describe and constrain for how long a reference is valid — it does not change how long any value actually lives. It expresses a constraint: the reference must not outlive the data it points to, and the borrow checker verifies at compile time that all uses fit within `'a`. Callers may be forced to keep the borrowed-from value alive long enough; the annotation never extends or shortens a value's real lifetime.

- id: rust-lifetimes-02
  answer: |
    Elision rules for function signatures: (1) each elided lifetime in the input positions gets its own fresh lifetime parameter; (2) if there is exactly one input lifetime, it is assigned to all elided output lifetimes; (3) if one of the parameters is `&self`/`&mut self`, self's lifetime is assigned to all elided outputs. So `fn first(s: &str) -> &str` elaborates to `fn first<'a>(s: &'a str) -> &'a str`. If the rules can't determine an output lifetime (e.g. two unrelated input references and a returned reference), the compiler errors and requires explicit annotations. (Return-position lifetimes in structs/impls have separate defaults.)

- id: rust-lifetimes-03
  answer: |
    `&'static T` is a *reference* whose validity region spans the entire program — it points at data that exists for the whole run, typically baked into the binary (string literals, lazily initialized statics) or leaked values (`Box::leak`). `T: 'static` is a *bound on a type*: T (including everything it contains) holds no references shorter than `'static` — i.e., T is either owned data or contains only `'static` references. Note a `String` is `String: 'static` even though it can be dropped at any time; the bound is about what the type contains, not how long a particular value lives. This is why `thread::spawn` requires `'static` closures: the thread may outlive the spawning frame.

- id: rust-lifetimes-04
  answer: |
    The default object-lifetime bound follows the rule: a `Box<dyn Trait>` (owned pointer) defaults to `Box<dyn Trait + 'static>`; a `&'a dyn Trait` defaults to `&'a (dyn Trait + 'a)` — the reference's own lifetime. So `Box<dyn Error>` implies `'static`: it can only hold owned/statically-living error values. You need `+ 'a` when the trait object must be allowed to contain borrowed data with lifetime `'a` (e.g., an Error impl holding a `&'a str`), e.g. `Box<dyn Error + 'a>`. I'm reasonably confident of the 'static-for-owned-pointer / enclosing-lifetime-for-reference defaults.

- id: rust-traits-01
  answer: |
    Static dispatch: `fn f<T: Trait>` is monomorphized — the compiler emits a specialized copy per concrete type and resolves calls at compile time, enabling inlining and zero runtime cost. Cost: code bloat (each instantiation compiled separately), longer compiles, and you cannot put differently-typed values in one collection. Dynamic dispatch: `&dyn Trait` is a fat pointer (data pointer + vtable pointer); the call is an indirect vtable lookup at runtime. Cost: indirection, no inlining; benefit: one code path for many types, heterogeneous collections (`Vec<Box<dyn Trait>>`), smaller binaries. Also trait objects require object safety (no generic methods, no by-value `self`).

- id: rust-traits-02
  answer: |
    Argument position (`fn f(x: impl Trait)`): the *caller* chooses the concrete type — it is essentially an anonymous generic parameter (each distinct type at each call site instantiates separately; no turbofish). Return position (`fn f() -> impl Trait`): the *implementer* chooses one single concrete type, hidden behind an opaque alias; every return path must yield that same type, and the caller cannot know or specify which one. So APIT = caller's choice; RPIT = callee's fixed choice.

- id: rust-traits-03
  answer: |
    The orphan rule (coherence): a trait impl is allowed only if the trait or the target type is local to your crate (at least one side is yours; local newtypes count). `impl Display for Vec<T>` is illegal because both `Display` and `Vec` are foreign. Rationale: coherence guarantees there is at most one impl of a trait for a type across the whole ecosystem — otherwise two crates could provide conflicting `Display for Vec` impls, and upstream adding such an impl later would break or silently change downstream behavior. Workaround: the newtype pattern — `struct MyVec(Vec<T>)` and `impl Display for MyVec`.

- id: rust-error-01
  answer: |
    Use `Option<T>` when absence is a normal, expected outcome and there is no error detail to convey — "there simply is no value" (e.g., `HashMap::get`). Use `Result<T, E>` when an operation can fail and the caller should learn *why* — E carries the failure reason (IO error, parse error, etc.). Roughly: Option = "maybe nothing", Result = "may fail, with a reason". If callers would benefit from distinguishing failure causes, use Result even if it feels binary.

- id: rust-error-02
  answer: |
    On `Result<T, E>`, `?` desugars to: if the value is `Ok(v)`, evaluate to `v`; if `Err(e)`, return early from the enclosing function with `Err(From::from(e))` — i.e., the error is converted into the function's declared error type via the `From` trait (so `Box<dyn Error>`, custom errors with `From` impls, etc. work transparently). Requirements: the enclosing function must return `Result` (or `Option`, where `?` returns `None`, or another type implementing the `Try`/`FromResidual` trait), and the source error type must be convertible via `From`.

- id: rust-error-03
  answer: |
    `panic!`/`unwrap()` are for: programmer errors and broken invariants (assert-style contract violations), impossible states, examples/prototypes/tests, and cases where recovery is genuinely meaningless. `Result` is for all expected, plausible failures — bad input, IO errors, lock poisoning, resource exhaustion. Library code should almost always return `Result` for ordinary failure modes and document any panics; prefer `expect("why")` over bare `unwrap()` when a panic is intentional.

- id: rust-error-04
  answer: |
    Your error type needs: `impl std::error::Error` (which requires `Debug` + `Display`), and a `From<EachUnderlyingError>` impl for every underlying error so that `?` converts automatically into it. `thiserror` automates this: `#[derive(Error)]` implements `Error` (including `source()` via `#[source]`/`#[from]`), `#[error("...")]` generates the `Display` impl, and `#[from]` on a variant generates the `From` conversion. It's the standard choice for libraries; `anyhow` is the common choice for applications.

- id: rust-iterators-01
  answer: |
    `map` and `filter` are lazy *adaptors*: they don't process anything, they return new iterator structs that wrap the previous one and only transform items when pulled. No work happens until a *consumer* drives iteration — a `for` loop, `collect()`, `sum()`, `fold()`, `next()`, `count()`, etc. That laziness is what allows composing pipelines with no intermediate allocations.

- id: rust-iterators-02
  answer: |
    `iter()` yields `&T` — borrows the collection; the collection is unchanged and usable afterwards. `iter_mut()` yields `&mut T` — a mutable borrow, allowing in-place element modification; collection intact afterwards (it just can't be touched elsewhere during iteration). `into_iter()` yields `T` by value — it *consumes* (moves) the collection; elements are owned and the collection is unusable after the loop.

- id: rust-iterators-03
  answer: |
    `collect()` is generic over its destination: `fn collect<B: FromIterator<Self::Item>>() -> B`. If nothing pins down `B`, inference fails → "type annotations needed". Fixes: annotate the binding (`let v: Vec<i32> = it.collect();`), turbofish (`it.collect::<Vec<_>>()`), or rely on a typed function return. Special case: `Result<V, E>: FromIterator<Result<T, E>>`, so collecting an iterator of `Result<T, E>` into `Result<Vec<T>, E>` yields `Ok` with all values if every item was `Ok`, else the first `Err` — a standard short-circuiting idiom: `.collect::<Result<Vec<_>, E>>()?`.

- id: rust-iterators-04
  answer: |
    Closures capture their environment by reference (borrow) by default. A returned closure outlives the function's stack frame, so borrowed captures would dangle. `move` changes each capture to be *by value* — the closure takes ownership of the captured variables (moving them in), so the closure owns its data and is self-contained. (Note: `move` moves what the capture expression refers to; capturing `&x` explicitly under `move` still stores a reference.)

- id: rust-smartptr-01
  answer: |
    `Box<T>` puts `T` on the heap with single ownership: a pointer-sized handle that derefs like `T` and frees the allocation on drop. Genuinely needed for: recursive types where the size would otherwise be infinite (`enum List { Cons(i32, Box<List>), Nil }`), trait objects (`Box<dyn Trait>`) when the concrete type isn't known at compile time, and cheap moves of large values (moving a Box copies a pointer, not the data).

- id: rust-smartptr-02
  answer: |
    `Rc<T>` is non-atomic reference counting (single-threaded only; it is neither `Send` nor `Sync`). `Arc<T>` uses atomic counters so it's safe to share/clone across threads. You shouldn't always use `Arc` because atomics cost more on every clone/drop (even uncontended, they inhibit optimizations), and — more importantly — `Rc`'s non-thread-safety is a static guarantee: the compiler stops you from accidentally sharing it across threads, whereas `Arc` permits it. Also `Arc` requires `T: Send + Sync` to itself be `Send`.

- id: rust-smartptr-03
  answer: |
    Interior mutability is the ability to mutate through a shared (`&`) reference while upholding Rust's aliasing rules — enforcement moves from compile time to runtime. `RefCell<T>` keeps a runtime borrow counter: `borrow()`/`borrow_mut()` hand out `Ref`/`RefMut` guards, and a violating overlapping borrow panics at runtime ("already borrowed"). Cost vs a normal `&mut`: no compile-time guarantee (rule violations become panics instead of errors), per-access counter bookkeeping, and it is not thread-safe (`RefCell` is not `Sync`).

- id: rust-smartptr-04
  answer: |
    `Rc` provides multiple owners of one value; `RefCell` permits mutation through those shared handles at runtime-checked cost. Together: many parts of a program can hold a handle to the same value and mutate it on demand — shared *and* mutable. This works only single-threaded because `Rc` and `RefCell` are not `Send`/`Sync`. The multi-threaded equivalent is `Arc<Mutex<T>>` (or `Arc<RwLock<T>>` when readers dominate), where locking replaces the runtime borrow check.

- id: rust-concurrency-01
  answer: |
    `Send`: the type's ownership can be safely *transferred* to another thread. `Sync`: the type can be safely *shared* by reference across threads — formally, `T: Sync` means `&T: Send`. They are auto-derived marker traits: a type is Send/Sync if all its fields are. Rust uses them as compile-time constraints: `thread::spawn` requires the closure to be `Send + 'static`, and shared references moved to threads must be `Sync`. Since `Rc` is not `Send`/`Sync`, `RefCell` not `Sync`, etc., any program that would produce a data race (two threads, unsynchronized, at least one write) fails to compile.

- id: rust-async-01
  answer: |
    Calling an `async fn` returns a `Future` — an anonymous type implementing `Future<Output = T>` that does no work when created; futures are lazy. Execution requires being *polled* by an executor/runtime (tokio, async-std, smol, or `futures::executor::block_on`): inside async code you `.await` it (which polls and may suspend), and at the top level you need `#[tokio::main]`, `block_on`, or `tokio::spawn` to drive it to completion.

- id: rust-async-02
  answer: |
    `tokio::spawn` requires the future to be `'static` and `Send` (the multithreaded runtime may migrate it between threads at every await point). `Rc` is not `Send`, and `std::sync::MutexGuard` is not `Send`; holding either *across an `.await`* means it is alive across a yield point, so the future contains non-`Send` state and fails the `Send` bound. Borrowed locals across `.await` before `spawn` also fail the `'static` bound. Fixes: drop the guard/`Rc` before awaiting; use `Arc` instead of `Rc` (and `tokio::sync::Mutex`, which is designed to be held across awaits, for locks you must hold across `.await`).

- id: rust-async-03
  answer: |
    An async runtime multiplexes many tasks over a few worker threads; a blocking call (std::thread::sleep, heavy CPU loop, synchronous IO, blocking lock) stalls that worker thread entirely, so every other task scheduled on it is starved — latency spikes, missed timers, and potentially a fully stalled runtime. Instead: use async-aware APIs (`tokio::time::sleep`, async IO, `tokio::sync::Mutex`), and offload blocking/CPU-bound work with `tokio::task::spawn_blocking` (or `rayon` for CPU work) so a dedicated pool absorbs the blocking.

- id: rust-match-01
  answer: |
    Exhaustive means every possible value of the matched expression must be covered by some arm (including catch-alls like `_`); otherwise the code doesn't compile. For enums this is especially valuable: if you add a new variant, every `match` over that enum without a wildcard becomes a compile error, and the compiler points you to exactly the places that must learn to handle the new case — turning a whole class of "forgot to handle case X" bugs into compile errors.

- id: rust-match-02
  answer: |
    `if let` when you care about exactly one pattern and want to ignore the rest (`if let Some(x) = opt { ... }`). `let ... else` when the non-matching case should diverge (return/break/panic) — a guard-clause style that binds the value for the rest of the block without nesting: `let Some(x) = opt else { return; };`. Full `match` when you need to act on several patterns or the compiler's exhaustiveness checking is valuable. Rule of thumb: one case of interest → `if let`/`let-else`; multiple cases → `match`.

- id: rust-match-03
  answer: |
    With match ergonomics (default binding modes, RFC 2005), matching a reference automatically dereferences: the pattern `Some(x)` binds `x` *by reference*, so `x` is `&T` (a shared reference to the inner value), not an owned `T`. The binding mode cascades down subpatterns and flips to `ref mut`/by-move as appropriate. The feature is called match ergonomics / default binding modes.

- id: rust-match-04
  answer: |
    Destructuring: patterns can take values apart and bind their pieces — `Point { x, y }`, `Some(inner)`, `(a, b, c)`, slices `[first, rest @ ..]` — binding subcomponents directly. Match guards: an extra boolean condition per arm, e.g. `Some(n) if n > 0 => ...` — the arm fires only when both the pattern and guard match; guards don't count toward exhaustiveness. `@` bindings: bind the whole matched value to a name while simultaneously testing a subpattern/range: `n @ 1..=9 => println!("small: {n}")` — you get `n` and the range test in one arm. Together they let a single `match` decompose, filter, and bind values concisely.
```
