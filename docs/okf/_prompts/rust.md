# Rust — Kaizen blind answer sheet (questions only)

> **CLOSED-BOOK.** Answer every question from your own knowledge alone. You MUST
> NOT open, search, or otherwise access the Kaizen corpus — `questions.yaml`,
> `questions.md`, `scores/`, `derived/`, `meta.yaml`, or any file in this repo —
> nor look the answers up online. Doing so invalidates the evaluation.
>
> Answer each question **independently** (treat every item as a fresh context —
> no memory of earlier answers). If you are unsure, say so; do not guess to look
> complete. This measures what you actually know, not what you can retrieve.
>
> **Return format** — one YAML record per question so the grader can map answers
> back by id:
>
> ```yaml
> - id: <question-id>
>   answer: |
>     <your answer>
> ```

Total questions: 31

---

### rust-ownership-01

What happens when you assign a `String` to another variable (`let b = a;`) and then try to use `a`, and why?

### rust-ownership-02

What is the difference between the `Copy` and `Clone` traits, and why can't a type that implements `Drop` also be `Copy`?

### rust-ownership-03

You pass a `Vec<T>` to a function and then want to use it afterward, but the compiler complains. What did passing by value do, and what are your options?

### rust-borrowing-01

State Rust's core borrowing rule and explain what class of bugs it prevents at compile time.

### rust-lifetimes-01

What does a lifetime annotation like `'a` actually express? Does it change how long a value lives?

### rust-lifetimes-02

Rust often lets you write `fn first(s: &str) -> &str` with no explicit lifetimes. What are the lifetime elision rules that make this work?

### rust-lifetimes-03

Explain the two distinct meanings of `'static` in Rust: `&'static T` versus a bound `T: 'static`.

### rust-lifetimes-04

When you write `Box<dyn Error>` versus storing a trait object with a borrowed reference inside, what is the default lifetime bound and when do you need `+ 'a`?

### rust-traits-01

Contrast static dispatch via generics (`fn f<T: Trait>`) with dynamic dispatch via trait objects (`&dyn Trait`). What's the tradeoff?

### rust-traits-02

What does `impl Trait` mean in argument position versus return position, and who chooses the concrete type in each?

### rust-traits-03

What is the orphan rule (coherence), and why can't you `impl Display for Vec<T>` in your own crate?

### rust-error-01

When should a function return `Option<T>` versus `Result<T, E>`?

### rust-error-02

Explain exactly what the `?` operator does on a `Result`, including the conversion it performs.

### rust-error-03

When is it appropriate to `panic!` (or `.unwrap()`) versus returning a `Result`?

### rust-error-04

In a library you want one error type that can wrap several underlying errors and work with `?`. What does the error type need, and how do crates like `thiserror` help?

### rust-iterators-01

Why does `v.iter().map(|x| expensive(x)).filter(|x| cond(x))` do no work on its own, and what makes it run?

### rust-iterators-02

What's the difference between `iter()`, `iter_mut()`, and `into_iter()` on a collection, in terms of what you get and what happens to the collection?

### rust-iterators-03

`collect()` sometimes fails to compile with "type annotations needed." Why, and how do you tell it what to build? What's special about collecting into a `Result`?

### rust-iterators-04

You return a closure/iterator from a function that captures a local variable, and the compiler suggests `move`. What does `move` change about the closure?

### rust-smartptr-01

What does `Box<T>` give you, and name a case where you genuinely need it.

### rust-smartptr-02

`Rc<T>` and `Arc<T>` both give shared ownership. What's the difference, and why not always use `Arc`?

### rust-smartptr-03

What is interior mutability, and how does `RefCell<T>` provide it? What's the cost compared to a normal `&mut`?

### rust-smartptr-04

Why is `Rc<RefCell<T>>` the idiom for shared mutable state in single-threaded code, and what's the multi-threaded equivalent?

### rust-concurrency-01

Explain the `Send` and `Sync` marker traits. What does each guarantee, and how does Rust use them to make data races a compile error?

### rust-async-01

In Rust, calling an `async fn` doesn't run it. What does it return, and what is required to actually make it execute?

### rust-async-02

You hold a `Rc` (or `MutexGuard`) across an `.await` and then `tokio::spawn` the future — it won't compile. Why?

### rust-async-03

Why is calling a blocking operation (like `std::thread::sleep` or heavy CPU work) directly inside an async task harmful, and what should you do instead?

### rust-match-01

What does it mean that `match` must be exhaustive, and why is this especially valuable when matching an enum?

### rust-match-02

When would you use `if let` versus `let ... else` versus a full `match`?

### rust-match-03

When you `match` on a reference — `match &opt { Some(x) => ... }` — what is the type of `x`, and what feature causes that?

### rust-match-04

Show what destructuring, match guards, and `@` bindings each add to pattern matching.
