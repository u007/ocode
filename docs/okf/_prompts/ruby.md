# Ruby — Kaizen blind answer sheet (questions only)

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

Total questions: 32

---

### ruby-blocks-procvslambda-01

What are the two main behavioral differences between a Proc and a lambda in Ruby?

### ruby-blocks-yield-02

How does `yield` work, and what does `block_given?` protect against?

### ruby-blocks-ampblock-03

What does the `&` do in `def m(&block)` and in a call like `arr.map(&:to_s)`?

### ruby-blocks-create-04

Show the ways to create a lambda and to invoke a Proc/lambda.

### ruby-modules-includeextendprepend-01

Contrast `include`, `extend`, and `prepend` for mixing a module into a class, in terms of where the methods land and the ancestor chain.

### ruby-modules-ancestors-super-02

How does `super` decide which method to call, and how do included modules fit into method lookup?

### ruby-modules-namespace-03

Besides mixins, what is the other primary use of a `module`, and how do you define a callable module function?

### ruby-modules-refinements-04

What problem do refinements solve compared with reopening a class ("monkey-patching"), and how are they activated?

### ruby-objects-methodmissing-01

How does `method_missing` work, and why must you also define `respond_to_missing?` when you use it?

### ruby-objects-send-02

What do `send` and `public_send` do, and when do they differ?

### ruby-objects-attr-03

What do `attr_reader`, `attr_writer`, and `attr_accessor` generate, and what is `define_method` for?

### ruby-objects-visibility-04

What is the difference between `private` and `protected` methods in Ruby, and what does that mean for calling them with an explicit receiver?

### ruby-enumerable-include-01

What do you have to implement to make your own class `include Enumerable`, and what do you get for it?

### ruby-enumerable-reduce-02

Explain `reduce`/`inject` and when `each_with_object` is a better fit.

### ruby-enumerable-lazy-03

What does `.lazy` do to an Enumerable chain, and why does it matter for large or infinite sequences?

### ruby-enumerable-comparable-04

How do you make instances of your class sortable and comparable with `<`, `>`, `clamp`, etc.?

### ruby-metaprogramming-singleton-01

What is a singleton class (eigenclass), and what does `class << self` inside a class body do?

### ruby-metaprogramming-ivar-02

What do `instance_variable_get` and `instance_variable_set` do, and when are they appropriate?

### ruby-metaprogramming-definemethod-vs-mm-03

When defining methods dynamically, what is the trade-off between `define_method` and `method_missing`?

### ruby-metaprogramming-classnew-04

What does `Class.new` do, and how does assigning it to a constant matter?

### ruby-error-standarderror-01

A bare `rescue` (or `rescue => e`) catches `StandardError`, not `Exception`. Why is rescuing `Exception` almost always wrong?

### ruby-error-ensure-retry-02

In a `begin/rescue/ensure` block, when does `ensure` run, and what does `retry` do?

### ruby-error-custom-03

How do you define and raise a custom exception, and what forms can `raise` take?

### ruby-error-elserescue-04

You can write `rescue` directly in a method body without `begin`. What does the optional `else` clause of a begin/rescue add?

### ruby-strings-symbols-01

What is the difference between a Symbol and a String in Ruby, especially regarding identity, mutability, and typical use?

### ruby-strings-frozen-02

What does the `# frozen_string_literal: true` magic comment do, and what is a subtlety about interpolated strings?

### ruby-strings-quotes-03

What is the difference between single- and double-quoted string literals, and what is string interpolation?

### ruby-strings-percent-04

What do the `%w[...]` and `%i[...]` literals produce?

### ruby-collections-hashdefault-01

What is the difference between `Hash.new(0)` and `Hash.new { |h, k| h[k] = [] }`, and why does the difference matter?

### ruby-collections-kwargs-02

Ruby 3.0 made keyword arguments a distinct thing from a trailing Hash. What changed, and how do you pass a hash as keywords now?

### ruby-collections-splat-03

What do the splat `*` and double-splat `**` operators do in method definitions and calls?

### ruby-collections-safenav-data-04

What does the safe-navigation operator `&.` do, and what is `Data.define` (Ruby 3.2)?
