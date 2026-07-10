# Rust Benchmark — Human Render

> **Generated from `questions.yaml` (corpus_rev 1). Do not grade from this file
> — `questions.yaml` is the source of truth.** Hand-synced; if they disagree,
> the YAML wins.

Legend: **W** = weight (1–3), **D** = difficulty. Rubric shows scored points
(`•`) and partial-credit levels (`~`).

---

### rust-ownership-01 · ownership · W3 · medium
**Q:** Assigning a `String` to another variable then using the original — what happens and why?
**A:** Assigning a non-`Copy` type moves ownership; the old binding is invalidated, so using it is a "borrow after move" error. Rust moves so exactly one owner frees the buffer — prevents double-free without a GC. `clone()` if you need two owners.
• assignment moves; old binding invalid • one owner / prevents double-free ~ "a is gone" without move/single-owner reason

### rust-ownership-02 · ownership · W2 · easy
**Q:** `Copy` vs `Clone`, and why can't a `Drop` type be `Copy`?
**A:** `Copy` = implicit cheap bitwise duplicate on assignment (integers etc.); `Clone` = explicit, maybe-deep `.clone()`. `Copy` requires `Clone`. A `Drop` type can't be `Copy` because implicit bit-copies would double-drop the same resource.
• Copy implicit/cheap, Clone explicit/deep • Copy = no move; requires Clone • Drop+Copy = double-drop ~ "Copy auto, Clone manual" no Drop/cost

### rust-ownership-03 · ownership, borrowing · W2 · medium
**Q:** Passed a `Vec` by value then can't use it — what happened, options?
**A:** Passing by value moved the `Vec` in; the function owns it now. Fix: borrow `&Vec`/`&[T]` (or `&mut`) so the caller keeps ownership, return it back, or `clone()`. Borrowing is idiomatic.
• pass by value moves ownership • fix: borrow (or return it back) ~ "just clone" without borrowing

### rust-borrowing-01 · borrowing · W3 · medium
**Q:** State Rust's core borrowing rule and what bugs it prevents.
**A:** Either many shared `&T` OR exactly one `&mut T` at a time — never both (aliasing XOR mutability). Statically prevents data races, use-after-free, and iterator invalidation: nobody mutates data while another holds a reference.
• many &T XOR one &mut T • prevents data races/UAF/invalidation at compile time ~ "one mutable borrow" without shared-XOR-mutable

### rust-lifetimes-01 · lifetimes · W2 · medium
**Q:** What does `'a` express? Does it change how long a value lives?
**A:** No — a lifetime is a compile-time-only label for the region a reference must stay valid. It relates input/output reference validity so the checker rejects a returned reference outliving its data. Erased before codegen.
• lifetime = region/constraint a ref is valid • does NOT change runtime lifetime ~ "how long a variable lives"

### rust-lifetimes-02 · lifetimes · W2 · medium
**Q:** The lifetime elision rules that let you skip `'a` on `fn first(s: &str) -> &str`?
**A:** (1) Each elided input ref gets its own lifetime; (2) one input lifetime → applied to all outputs; (3) if a method has `&self`, `self`'s lifetime is applied to outputs. Else annotate explicitly.
• each input ref its own lifetime • single input → all outputs • &self lifetime → outputs ~ "compiler infers it" no rules

### rust-lifetimes-03 · lifetimes, borrowing · W2 · hard
**Q:** Two meanings of `'static`: `&'static T` vs bound `T: 'static`?
**A:** `&'static T` = a reference valid for the whole program (literals, leaked/const data). `T: 'static` = the type holds no non-'static references (any owned type qualifies) — NOT "lives forever."
• &'static T = ref valid whole program • T: 'static = no short-lived borrows (owned OK) ~ conflates them / "lives forever"

### rust-lifetimes-04 · lifetimes, traits · W1 · hard
**Q:** Default lifetime bound on `Box<dyn Trait>`, and when do you need `+ 'a`?
**A:** Trait objects default to `+ 'static`, so the erased type may not hold short-lived borrows. If it does, spell the shorter bound: `Box<dyn Trait + 'a>` so it can't outlive the borrowed data.
• dyn/boxed trait objects default to + 'static • use + 'a for a shorter-lived borrow ~ notices dyn lifetime issue, not the default

### rust-traits-01 · traits · W3 · medium
**Q:** Static dispatch via generics vs dynamic dispatch via `&dyn Trait` — the tradeoff?
**A:** Generics monomorphize: a specialized copy per type, static/inlinable (fast, code bloat). `dyn` is a fat pointer + vtable resolved at runtime — one code path, heterogeneous collections, but indirect non-inlined calls. dyn for runtime flexibility, generics for speed.
• generics = monomorphized/static/inlinable • dyn = fat pointer+vtable/runtime/heterogeneous • tradeoff speed vs flexibility ~ names both, no mechanism

### rust-traits-02 · traits · W2 · medium
**Q:** `impl Trait` in argument vs return position — who chooses the type?
**A:** Argument: anonymous generic, universal — caller picks the type. Return: existential — the callee returns one hidden concrete type (all branches must match). Return-position `impl Trait` returns unnameable closures/iterators without boxing.
• arg = anon generic, caller chooses • return = one hidden concrete type, callee chooses • return: same type all branches / no boxing ~ "trait bound shorthand" no arg/return

### rust-traits-03 · traits · W2 · medium
**Q:** The orphan rule (coherence) — why can't you `impl Display for Vec<T>`?
**A:** You may `impl Trait for Type` only if the trait or the type is local to your crate. `Display` and `Vec` are both foreign, so it's forbidden (would allow conflicting impls). Workaround: newtype wrapper.
• orphan rule: trait or type must be local • ensures coherence / no conflicting impls ~ mentions newtype, not the why

### rust-error-01 · error-handling · W2 · easy
**Q:** `Option<T>` vs `Result<T, E>` — when each?
**A:** `Option` = plain absence with no useful error (lookup miss, end of iteration). `Result` = failure carrying an error value `E` the caller may need to distinguish. No-error absence → Option; something failed → Result.
• Option = plain absence, no error info • Result = failure carrying E ~ "Option none, Result errors" no reasoning

### rust-error-02 · error-handling · W3 · medium
**Q:** What does `?` do on a `Result`, including the conversion?
**A:** `Ok(v)` → unwraps to `v` and continues; `Err(e)` → early-returns `Err(e)`, first converting via `From::from` into the function's error type. That `From` conversion is why `?` unifies different error types. On `Option`, `?` early-returns `None`.
• Ok unwrap/continue, Err early-return • converts error via From::from ~ "unwraps or returns error" misses From

### rust-error-03 · error-handling · W2 · medium
**Q:** When to `panic!`/`.unwrap()` vs return a `Result`?
**A:** `Result` for expected, recoverable failures (I/O, bad input, missing data). `panic`/`unwrap` for unrecoverable bugs/broken invariants, or throwaway code (tests, prototypes). A panic unwinds/aborts — not normal control flow.
• Result for recoverable errors • panic for bugs/invariants (or tests), not normal flow ~ "panic bad, use Result" no distinction

### rust-error-04 · error-handling, traits · W2 · medium
**Q:** A library error type that wraps several sources and works with `?` — what's needed, how does `thiserror` help?
**A:** A custom enum implementing (or deriving) `Error` + `Display`, plus `From` impls per source error so `?` auto-converts. `thiserror` derives `Error`/`Display`(`#[error]`)/`From`(`#[from]`). For apps that don't match variants, `Box<dyn Error>`/`anyhow`.
• enum implementing Error + Display • From impls per source so ? converts • thiserror derives it (or Box<dyn Error>/anyhow) ~ "make an enum" no machinery

### rust-iterators-01 · iterators · W3 · medium
**Q:** Why does a `.map().filter()` chain do no work by itself, and what runs it?
**A:** Adapters are lazy: they build a pipeline and compute nothing until pulled. A consumer drives it — `collect`, `for`, `sum`, `count`, `next`, `for_each` — pulling one element through the whole chain. No consumer → closures never run (`must_use` warning).
• adapters lazy — build pipeline, no compute • a consumer pulls element-by-element ~ "iterators are lazy" no consumer

### rust-iterators-02 · iterators, ownership · W2 · medium
**Q:** `iter()` vs `iter_mut()` vs `into_iter()` — what you get and what happens to the collection?
**A:** `iter()` → `&T` (immutable borrow); `iter_mut()` → `&mut T` (mutable borrow, edit in place); `into_iter()` → owned `T`, consuming/moving the collection. `for x in &v` = iter(); `for x in v` = into_iter().
• iter() &T, iter_mut() &mut T • into_iter() owned T, consumes collection ~ confuses into_iter's consuming behavior

### rust-iterators-03 · iterators · W2 · medium
**Q:** Why does `collect()` sometimes need type annotations, and what's special about collecting into a `Result`?
**A:** `collect` is generic over `FromIterator`, so the compiler can't infer the target — annotate (`let v: Vec<_>`) or turbofish (`.collect::<Vec<_>>()`). Collecting `Result`s into `Result<Vec<_>, E>` short-circuits on the first `Err`.
• generic over FromIterator, annotate target • Result iter → Result<Vec>, short-circuits first Err ~ annotation only, misses Result

### rust-iterators-04 · iterators · W2 · medium
**Q:** Returning/spawning a closure that captures a local — why does the compiler suggest `move`?
**A:** Closures borrow captures by default; `move` takes ownership of them. Needed when the closure outlives the scope — returned, `thread::spawn`ed, or stored — since a borrowing closure would dangle referencing a dropped local.
• default borrows captures; move takes ownership • needed when closure outlives scope (return/spawn/store) ~ "captures by value" no outlives reason

### rust-smartptr-01 · smart-pointers · W2 · easy
**Q:** What does `Box<T>` give you, and a case you genuinely need it?
**A:** Single-owner heap allocation with a fixed-size stack handle; drops the value on drop. Needed for recursive types (`Cons(i32, Box<List>)`), trait objects (`Box<dyn Trait>`), unsized types, or moving large values cheaply.
• single-owner heap, fixed-size handle • real need: recursive/trait object/unsized ~ "puts data on heap" no need

### rust-smartptr-02 · smart-pointers, concurrency · W2 · medium
**Q:** `Rc<T>` vs `Arc<T>` — difference, and why not always `Arc`?
**A:** Both are ref-counted shared ownership, freeing at the last drop. `Rc` = non-atomic counter, single-threaded (`!Send`/`!Sync`); `Arc` = atomic counter, thread-safe. Atomics cost more, so prefer `Rc` when single-threaded.
• both = ref-counted shared ownership • Rc non-atomic/single-thread, Arc atomic/thread-safe • atomics cost → prefer Rc single-threaded ~ "Arc is thread-safe one" no atomic/cost

### rust-smartptr-03 · smart-pointers, borrowing · W3 · medium
**Q:** What is interior mutability, how does `RefCell<T>` give it, and the cost vs `&mut`?
**A:** Interior mutability = mutating through a shared `&` reference (normally forbidden). `RefCell` moves the borrow check to runtime via `borrow()`/`borrow_mut()` guards. Cost: a borrow conflict panics at runtime instead of being a compile error.
• interior mut = mutate via shared & • RefCell checks borrows at runtime • conflict panics at runtime ~ "lets you mutate" no runtime/panic tradeoff

### rust-smartptr-04 · smart-pointers, concurrency · W3 · hard
**Q:** Why `Rc<RefCell<T>>` for shared mutable single-threaded state, and the multi-threaded equivalent?
**A:** `Rc` = shared ownership but immutable; `RefCell` = interior mutability. Combined = multiple owners that can each mutate — neither does alone. Single-threaded (non-atomic/`!Sync`). Multi-threaded: `Arc<Mutex<T>>` (or `Arc<RwLock<T>>`).
• Rc shared-ownership + RefCell interior-mut = shared mutation • single-threaded (non-atomic) • MT equivalent Arc<Mutex> ~ names combos, not why each layer

### rust-concurrency-01 · concurrency · W3 · hard
**Q:** `Send` and `Sync` marker traits — what each guarantees, and how they make data races a compile error?
**A:** `Send` = safe to transfer ownership to another thread; `Sync` = `&T` is `Send` (safe to share by reference). Auto-traits derived structurally. Thread APIs require these bounds, so unsafe-to-share types (`Rc` !Send, `RefCell` !Sync) won't compile cross-thread → no data races.
• Send = transfer to another thread • Sync = &T is Send / share by ref • auto-traits as bounds → races won't compile ~ "thread safety" no Send-vs-Sync

### rust-async-01 · async · W3 · medium
**Q:** Calling an `async fn` doesn't run it — what does it return, and what makes it execute?
**A:** It returns a lazy `Future` (a state machine) that does nothing until polled. Drive it by `.await` in an async context or hand the top-level future to an executor. Rust ships no executor — use a runtime (Tokio/async-std).
• async fn returns lazy Future, runs nothing • driven by .await or an executor • std has no executor, need a runtime ~ "returns a future" but implies it self-runs

### rust-async-02 · async, concurrency · W2 · hard
**Q:** Holding an `Rc`/`MutexGuard` across `.await` then `tokio::spawn` won't compile — why?
**A:** Values held across an `.await` live in the future's state machine, so a non-`Send` value alive across await makes the whole future `!Send`. `tokio::spawn` (multi-threaded) requires `Send` since it may resume on another thread. Fix: drop it before the await, or use `Arc`/an async mutex.
• values across .await captured in the state machine • non-Send across await → !Send future; spawn needs Send ~ "Rc not thread-safe" no held-across-await mechanism

### rust-async-03 · async · W2 · medium
**Q:** Why is a blocking call (e.g. `thread::sleep`, heavy CPU) inside an async task harmful, and the fix?
**A:** Async is cooperative — a task yields only at `.await`. A blocking call never yields, so it hogs the executor thread and stalls every other task on it. Use async equivalents (`tokio::time::sleep().await`) or offload to `spawn_blocking`.
• cooperative — yield only at .await, blocking never yields • stalls executor/other tasks; use async equiv or spawn_blocking ~ "it's slow" no cooperative-scheduling reason

### rust-match-01 · pattern-matching · W2 · easy
**Q:** What does `match` exhaustiveness mean, and why is it valuable for enums?
**A:** Every possible value must be covered or it won't compile (`_` for the rest). For enums, adding a variant later breaks every incomplete `match`, forcing you to handle the new case instead of silently falling through — "forgot to handle X" becomes a build error.
• match must cover all cases or fails to compile • new enum variant breaks incomplete matches → forces handling ~ "handle all cases" no add-a-variant benefit

### rust-match-02 · pattern-matching · W2 · medium
**Q:** `if let` vs `let ... else` vs a full `match`?
**A:** `if let` handles one pattern, ignores the rest. `let ... else` (let-else) binds for the happy path in the enclosing scope and requires the `else` to diverge — flattens early-return guards. Full `match` for several exhaustive cases.
• if let: single pattern, ignore rest • let-else: bind happy path, else diverges — flat guards ~ if let only, misses let-else

### rust-match-03 · pattern-matching, borrowing · W2 · hard
**Q:** `match &opt { Some(x) => ... }` — what's the type of `x`, and what causes it?
**A:** `x` is a reference (`&T`), not owned. Match ergonomics (default binding modes): matching a reference against a non-reference pattern auto-binds inner variables by reference — no `ref x` or manual deref needed. Binding by value would try to move out of a borrow.
• x is &T, not owned • match ergonomics / default binding modes auto-bind by ref ~ knows can't move out of borrow, not the auto-ref mechanism

### rust-match-04 · pattern-matching · W1 · medium
**Q:** What do destructuring, match guards, and `@` bindings each add?
**A:** Destructuring pulls apart structs/tuples/enum payloads in the pattern (`Point { x, y }`, `Some(n)`). A guard adds a runtime condition (`Some(n) if n > 0`). An `@` binding captures the whole value while testing its shape (`id @ 1..=5`).
• destructuring binds inner fields • guard = extra if; @ binds value while pattern-testing ~ destructuring only, misses guards/@
