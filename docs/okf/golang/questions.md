# Go (Golang) Benchmark — Human Render

> **Generated from `questions.yaml` (corpus_rev 1). Do not grade from this file
> — `questions.yaml` is the source of truth.** Hand-synced; if they disagree,
> the YAML wins.

Legend: **W** = weight (1–3), **D** = difficulty. Rubric shows scored points
(`•`) and partial-credit levels (`~`). Version scope: Go ≥1.21.

---

### go-concurrency-01 · concurrency · W2 · medium
**Q:** Unbuffered vs buffered channel — when does a send block on each?
**A:** Unbuffered has no capacity: a send blocks until a receiver is ready (a rendezvous). Buffered (`make(chan T, n)`) holds n values; a send blocks only when the buffer is full, a receive only when empty. Buffering decouples up to n, it does not make sends non-blocking in general.
• unbuffered: send blocks until receiver ready • buffered: send blocks only when full ~ "buffered is async / never blocks"

### go-concurrency-02 · concurrency, goroutine-leaks · W2 · medium
**Q:** What does `select` do, how does `default` change it, and what if no case is ready and there's no default?
**A:** Waits on multiple channel ops, proceeds with one that's ready (random among several). `default` makes it non-blocking. No default + no ready case blocks; if nothing ever becomes ready the goroutine leaks. `select {}` blocks forever.
• proceeds with a ready case (random if several) • default = non-blocking • no default + nothing ready blocks (leak) ~ describes select but not default-vs-blocking

### go-concurrency-03 · concurrency, goroutine-leaks · W3 · medium
**Q:** Rules for closing a channel — who closes, what panics, why close?
**A:** Only the sender closes, once. Send on closed panics; closing a closed/nil channel panics. Receive from closed drains buffer then yields zero value + `ok=false`, which ends a `for range`. Never close from the receiver or from multiple uncoordinated senders.
• only sender closes once; send-on-closed panics • receive from closed → zero + ok=false (drains first) • close signals completion / ends `for range` ~ knows receive-from-closed is safe but misses send-on-closed panic

### go-concurrency-04 · concurrency, sync · W3 · medium
**Q:** Goroutine-per-iteration all saw the same final loop value — cause, and what changed in Go 1.22?
**A:** Pre-1.22 the loop variable was shared across iterations, so every closure captured the same variable (last value) — also a data race. Go 1.22 gives each iteration a fresh copy, fixing it. Pre-1.22 fix: shadow `v := v` or pass as an argument.
• pre-1.22 shared loop var → closures captured one variable • Go 1.22 per-iteration copy fixes it • pre-1.22 workaround `v := v` / pass as arg ~ describes bug but no version/fix

### go-sync-01 · sync · W3 · medium
**Q:** What is a data race, and the minimal correct way to protect a shared counter?
**A:** Concurrent access with ≥1 write and no synchronization → undefined behavior. Guard with `sync.Mutex` (`Lock/count++/Unlock`) or `sync/atomic`. The bare `count++` read-modify-write is the classic race.
• data race = concurrent access, ≥1 write, no sync (UB) • protect with mutex Lock/Unlock or atomic ~ "use a mutex" without the race condition

### go-sync-02 · sync, testing · W2 · easy
**Q:** What does `-race` do and what are its limits?
**A:** `go test -race` instruments memory accesses at runtime and reports races with conflicting stacks. It only finds races that actually execute (dynamic, not a proof) and adds heavy overhead — for tests/CI, not production.
• instruments accesses at runtime to detect races that occur • only catches races that run / has overhead ~ "detects races" with no dynamic caveat

### go-sync-03 · sync · W2 · medium
**Q:** `sync/atomic` vs `sync.Mutex` — when and trade-off?
**A:** Atomics: lock-free single-word ops (counter, flag, pointer), cheaper. Mutex: when several fields must stay consistent or a multi-step critical section — atomics can't group updates atomically. Go 1.19+ typed atomics (`atomic.Int64`) are safer.
• atomic for single-word lock-free ops • mutex for multiple fields / multi-step critical section ~ "atomic is faster" with no scope distinction

### go-sync-04 · sync, concurrency · W2 · medium
**Q:** Using `sync.WaitGroup`, and the common misuse?
**A:** `Add(n)` before launching, each goroutine `defer wg.Done()`, `wg.Wait()` blocks to zero. Bug: `Add(1)` inside the goroutine races with `Wait`. A negative counter panics; don't copy a WaitGroup after use.
• Add before launch, Done per goroutine, Wait blocks to zero • misuse: Add inside goroutine races with Wait (or negative panics) ~ Add/Done/Wait without the Add-before-launch rule

### go-errors-01 · errors · W3 · medium
**Q:** What does `%w` in `fmt.Errorf` do vs `%v`/`%s`?
**A:** `%w` wraps and keeps a link to the original so `Unwrap`/`Is`/`As` still reach it. `%v`/`%s` only interpolate text, breaking the chain. `%w` since Go 1.13 (multiple `%w` since 1.20).
• %w wraps and preserves the unwrappable chain • %v/%s only format text, break the chain ~ "%w wraps" without the lost-chain contrast

### go-errors-02 · errors · W3 · medium
**Q:** `errors.Is` vs `errors.As`?
**A:** Both walk the chain. `Is(err, target)` tests equality against a sentinel value. `As(err, &target)` finds the first error of a concrete type and assigns it so you can read its fields. Is = compare to a value, As = extract a type.
• Is compares against a sentinel value in the chain • As matches/extracts a concrete type via pointer ~ knows both walk chain but swaps value-vs-type

### go-errors-03 · errors · W2 · medium
**Q:** What is a sentinel error, how declared, and how should callers compare?
**A:** A package-level exported error value signaling a condition: `var ErrNotFound = errors.New(...)` (e.g. `io.EOF`). Compare with `errors.Is`, not `==`, so it survives `%w` wrapping. Prefer typed errors when you must carry data.
• package-level exported error value (errors.New) • compare with errors.Is not == (survives wrapping) ~ defines sentinel but says compare with ==

### go-errors-04 · errors, interfaces · W3 · hard
**Q:** A func returns `*MyError` typed as `error`; even returning a nil pointer, `err != nil` is true. Why?
**A:** An interface is a (type, value) pair, nil only when both are nil. A nil `*MyError` boxed as `error` has a non-nil type slot → the interface isn't nil. Fix: return literal `nil`, never a typed nil pointer.
• interface is nil only if both type and value are nil • typed nil pointer has non-nil type slot → not nil • fix: return untyped nil ~ notices surprise but can't explain (type,value)

### go-interfaces-01 · interfaces · W2 · easy
**Q:** How does a type satisfy an interface, and what is "accept interfaces, return structs"?
**A:** Implicit/structural: a type satisfies an interface by having its methods — no `implements`. Accept interface params (flexible input), return concrete types (full API, no premature narrowing).
• implicit/structural satisfaction — has the methods • accept interface params, return concrete types ~ knows it's implicit but not the accept/return guidance

### go-interfaces-02 · interfaces · W2 · medium
**Q:** The empty interface (`any`), and `x.(int)` vs the comma-ok form?
**A:** `interface{}` (aliased `any` since 1.18) holds any type. `x.(int)` panics on type mismatch; `v, ok := x.(int)` never panics — `ok=false`, zero value on mismatch. Use comma-ok (or a type switch) when the type isn't guaranteed.
• any/interface{} holds any type • single assertion panics; comma-ok returns ok=false safely ~ defines any but misses panic-vs-comma-ok

### go-generics-01 · generics, interfaces · W2 · medium
**Q:** Generics (type params) vs a plain interface parameter — when each?
**A:** Interface: fixed method set, runtime polymorphism, concrete type irrelevant. Generics: preserve the type relationship — return the type you got, work over slice/map element types, enforce two args share a type; keeps static types and avoids `any`+assertions.
• interface: fixed method set / runtime polymorphism • generics: preserve type relationship, avoid any+assert ~ "generics avoid repetition" with no distinction

### go-generics-02 · generics · W2 · medium
**Q:** Shape of a generic function, and what is a constraint?
**A:** `func Map[T any, U any](s []T, f func(T) U) []U` — type params in brackets after the name. The constraint is an interface bounding the permitted type set and operations. `any` allows all; `cmp.Ordered` (Go 1.21) permits `<`/`>`. You can only do what the constraint guarantees.
• type params in brackets: func F[T Constraint](...) • constraint = interface bounding allowed types/ops ~ shows [T any] but can't explain the constraint

### go-generics-03 · generics · W2 · medium
**Q:** What does `comparable` allow, and what does `~int` mean?
**A:** `comparable` permits types supporting `==`/`!=` (usable as map keys). `~int` matches any type whose underlying type is `int` (e.g. `type Celsius int`), whereas plain `int` matches only `int`. `~` covers named types built on a base.
• comparable = types usable with ==/!= • ~T matches any type with underlying type T (named types) ~ gets one of comparable or ~ but not both

### go-generics-04 · generics · W1 · medium
**Q:** Type-argument inference — when must you specify explicitly?
**A:** The compiler infers type args from the value arguments (`Map(nums, f)`). Specify explicitly when a type param appears only in the return type / no value argument (`New[int]()`) or to disambiguate.
• compiler infers from value arguments • must specify when not determined by any arg (e.g. only in return) ~ "you can omit types" without the when-you-can't case

### go-context-01 · context, goroutine-leaks · W3 · medium
**Q:** How does context cancellation stop a goroutine, and whose job is it?
**A:** Cooperative: cancel closes `Done()` and propagates to child contexts. The goroutine must watch `<-ctx.Done()` / check `ctx.Err()` and return itself. Context can't force-kill a goroutine; ignoring `Done()` leaks.
• cancel closes Done() and propagates to children • goroutine must observe Done()/Err() and return (cooperative) • can't force-kill; ignoring it leaks ~ "context cancels it" implying automatic stop

### go-context-02 · context · W3 · medium
**Q:** `WithTimeout`/`WithDeadline` vs `WithCancel`, and why always call `cancel`?
**A:** `WithCancel` is manual; `WithTimeout` (relative) and `WithDeadline` (absolute) also self-cancel at the time. Always `defer cancel()` to release resources / stop the timer, else you leak the context and timer. Calling it after completion is harmless.
• timeout/deadline auto-cancel at a time; WithCancel is manual • always call cancel (defer) or leak resources/timer ~ knows the variants but not why cancel is mandatory

### go-context-03 · context · W2 · medium
**Q:** What is `context.WithValue` for, and what not?
**A:** Request-scoped data crossing boundaries (trace/request IDs, auth), keyed by an unexported custom type. Not for optional params/dependencies — values are untyped and invisible in the signature; real args belong in the signature.
• request-scoped data crossing boundaries, custom key type • not for optional params/deps (use explicit args) ~ "stores values" with no request-scoped caveat

### go-context-04 · context · W1 · easy
**Q:** Conventions for passing `context.Context`?
**A:** First parameter named `ctx`; don't store it in a struct. Never pass nil — use `context.TODO()` when unsure, `context.Background()` at the root. Contexts are immutable and concurrency-safe.
• first param ctx; don't store in a struct • don't pass nil — use TODO()/Background() ~ one convention only

### go-slices-01 · slices-maps · W3 · hard
**Q:** Why can `append` silently overwrite data seen through another slice, and why is its return value load-bearing?
**A:** A slice is a view over a backing array. With spare capacity, `append` writes in place, so aliasing slices see it; over capacity it reallocates and they diverge. Since it may or may not reallocate, assign `s = append(...)`. Use `copy` / `slices.Clone` for independence.
• slices share a backing array; in-capacity append mutates in place • append may reallocate → must assign return • copy/slices.Clone for an independent slice ~ "always s = append(...)" without the shared-array why

### go-slices-02 · slices-maps · W3 · easy
**Q:** Reading vs writing a nil map, and how does it differ from a nil slice?
**A:** Reading a nil map returns the zero value (no panic). Writing panics ("assignment to entry in nil map") — `make`/init first. A nil slice is usable: `append` allocates, `len`/`range` treat it as empty.
• reading a nil map returns zero value (no panic) • writing a nil map panics — must make first ~ knows write panics but claims reads panic too / conflates with slice

### go-slices-03 · slices-maps · W2 · medium
**Q:** What does `copy(dst, src)` copy, and why isn't `s[1:3]` independent?
**A:** `copy` moves `min(len(dst), len(src))` elements — bounded by `dst`, so a too-short `dst` copies little; size it first. `s[1:3]` is a new header over the same backing array; writes affect the original. Detach with `copy`/`slices.Clone` (shallow — element pointers still shared).
• copy = min(len(dst),len(src)); dst must be sized • s[i:j] reuses the backing array; clone/copy to detach ~ "copy copies the slice" without min-length / shared-array

### go-slices-04 · slices-maps · W2 · medium
**Q:** Map iteration order, and why can't you take `&m[k]`?
**A:** Iteration order is deliberately randomized/unspecified — sort keys for stability. You can't take `&m[k]` because entries can move on rehash (dangling pointer); so `m[k].field = ...` on a struct value is a compile error — reassign the whole value or store pointers.
• iteration order randomized/unspecified — sort keys • can't take &m[k] / mutate struct-value field in place ~ knows order is random but not the addressability constraint

### go-defer-01 · defer-panic · W3 · medium
**Q:** When are deferred-call arguments evaluated, and in what order do defers run?
**A:** Args (and the function value) are evaluated at the `defer` statement, not at return — `defer fmt.Println(i)` captures `i` then; wrap in a closure to read a later value. Deferred calls run LIFO at return.
• deferred args evaluated at the defer statement, not at return • defers run LIFO ~ gets LIFO but thinks args evaluate at return

### go-defer-02 · defer-panic · W2 · medium
**Q:** How does `recover` work — where can it be called, and which goroutine?
**A:** `recover` stops a panic and returns its value only when called directly in a deferred function during the panic; elsewhere it returns nil. It only covers its own goroutine — an unrecovered panic in another goroutine crashes the program.
• recover only works inside a deferred function (else nil) • only covers the same goroutine (others crash the program) ~ "catches panics" without deferred-only / same-goroutine

### go-defer-03 · defer-panic, goroutine-leaks · W2 · medium
**Q:** Why is `defer file.Close()` in a long loop a problem, and the fix?
**A:** `defer` fires at function return, not per iteration, so loop defers stack up and resources (file handles) accumulate until the function returns — can exhaust limits. Close explicitly per iteration, or extract the body into its own function.
• defer fires at function return → loop defers accumulate • fix: close per iteration or extract the body into a function ~ notices pile-up but no correct fix

### go-defer-04 · defer-panic, errors · W2 · medium
**Q:** How can a deferred function change the return value, and what's it used for?
**A:** Only with named return values: a deferred closure assigns them and the caller sees it. Common use: wrap/annotate the error on the way out (`err = fmt.Errorf("...: %w", err)`) or convert a recovered panic to an error.
• requires named returns; deferred func assigns them • used to wrap/annotate error or convert recovered panic to error ~ "defer can change returns" without named-return requirement

### go-testing-01 · testing · W2 · easy
**Q:** What is a table-driven test, and why is it idiomatic?
**A:** A slice of case structs (name, inputs, expected) iterated with one assertion body, each run as `t.Run(tc.name, ...)`. One body covers many cases, adding a case is trivial, and each case fails in isolation with a clear name.
• slice of cases iterated with one assertion body • run each as named t.Run subtest for isolation ~ "loop over cases" without the subtest benefit

### go-testing-02 · testing · W2 · medium
**Q:** What does `t.Parallel()` do in a subtest, and the historical loop-var pitfall?
**A:** Marks the subtest to run concurrently: parallel subtests pause until the parent returns, then run together. Pre-1.22 the shared loop var meant parallel subtests captured the last case — fix `tc := tc`; unnecessary as of Go 1.22 (per-iteration variable).
• t.Parallel marks subtest to run concurrently (after parent returns) • pre-1.22 shared loop var captured last case; shadow tc := tc (not needed in 1.22+) ~ explains t.Parallel but misses the loop-var pitfall

### go-testing-03 · testing · W1 · easy
**Q:** What do `t.Cleanup` and `t.Helper` do?
**A:** `t.Cleanup(fn)` registers teardown run at test end in LIFO order (robust across helpers). `t.Helper()` marks a helper so failures report the caller's file:line, not the line inside the helper.
• t.Cleanup registers teardown run at test end (LIFO) • t.Helper points failure line at the caller, not inside the helper ~ gets one of the two
