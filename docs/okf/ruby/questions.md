# Ruby Benchmark — Human Render

> **Generated from `questions.yaml` (corpus_rev 1). Do not grade from this file
> — `questions.yaml` is the source of truth.** Hand-synced; if they disagree,
> the YAML wins. LANGUAGE corpus (Rails is the separate `ror` corpus).

Legend: **W** = weight (1–3), **D** = difficulty. Rubric shows scored points
(`•`) and partial-credit levels (`~`).

---

### ruby-blocks-procvslambda-01 · blocks-procs · W3 · medium
**Q:** Two main behavioral differences between a Proc and a lambda?
**A:** (1) `return`: from a lambda returns only the lambda (method-like); from a non-lambda Proc returns the enclosing method. (2) Arity: a lambda checks argument count and raises `ArgumentError`; a Proc is lenient — nil-fills missing, discards extra. `break`/`next` also differ.
• lambda return exits lambda, proc return exits enclosing method • lambda strict arity, proc lenient ~ "basically the same" with no return/arity

### ruby-blocks-yield-02 · blocks-procs · W2 · easy
**Q:** How does `yield` work, and what does `block_given?` protect against?
**A:** `yield` invokes the implicitly-passed block (passing args, returning its value). Calling `yield` with no block raises `LocalJumpError`; `block_given?` tests whether one was passed so you can branch.
• yield invokes the implicit block • block_given? guards; yield-without-block raises LocalJumpError ~ yield only, not block_given?

### ruby-blocks-ampblock-03 · blocks-procs · W2 · medium
**Q:** What does `&` do in `def m(&block)` and in `arr.map(&:to_s)`?
**A:** `&block` captures the block as an explicit `Proc`. `&obj` at a call site converts `obj` to the block via `to_proc` — `Symbol#to_proc` turns `:to_s` into `{ |x| x.to_s }`.
• &block captures block as a Proc • &obj converts via to_proc (Symbol#to_proc) ~ "passes the block" without to_proc

### ruby-blocks-create-04 · blocks-procs · W1 · easy
**Q:** Ways to create a lambda and to invoke a Proc/lambda?
**A:** Lambda: `->(a,b){ }` or `lambda { }`. Proc: `proc { }` / `Proc.new { }`. Invoke with `.call` / `.()` / `[]`. `->` is a lambda; `proc` isn't.
• -> / lambda vs proc / Proc.new • invoke via .call / .() / [] ~ one creation or one invocation form

### ruby-modules-includeextendprepend-01 · modules-mixins · W3 · hard
**Q:** Contrast `include`, `extend`, and `prepend`.
**A:** `include`: instance methods, module found after the class in lookup order (class wins). `prepend`: instance methods found before the class, so module overrides and can `super` into it (method wrapping). `extend`: adds methods to the singleton class (class/object level), not instance methods.
• include=instance, after class in lookup • prepend=instance, before class, super-able • extend=singleton methods ~ include/extend right, prepend missing

### ruby-modules-ancestors-super-02 · modules-mixins · W2 · medium
**Q:** How does `super` pick a method, and where do modules fit in lookup?
**A:** Lookup walks `ancestors`: singleton, prepends, class, includes (nearest first), superclass, up to BasicObject. `super` re-dispatches the same name to the next ancestor — which can be a module. Bare `super` forwards args; `super()` passes none.
• lookup walks the ordered ancestor chain • super = same name in NEXT ancestor (can be a module) ~ "super = parent class" only

### ruby-modules-namespace-03 · modules-mixins · W1 · easy
**Q:** Other primary use of `module` besides mixins, and a callable module function?
**A:** Namespacing — group constants/classes/methods under a name, referenced with `::`. Module-level callables via `module_function` or `def self.x` (e.g. `Math.sqrt`). Modules can't be instantiated.
• module as namespace (::) • module_function / def self.x ~ "groups code" without namespace/module_function

### ruby-modules-refinements-04 · modules-mixins · W1 · hard
**Q:** What do refinements solve vs monkey-patching, and how are they activated?
**A:** Monkey-patching changes a class globally (can break unrelated code). Refinements scope it: modifications go in `refine Klass do ... end`, active only where `using ThatModule` appears (lexically). Outside, the class is unchanged.
• monkey-patch = global change • refine + using = lexically-scoped ~ "scoped monkey-patch" without refine/using

### ruby-objects-methodmissing-01 · objects-methods · W3 · hard
**Q:** How does `method_missing` work, and why also `respond_to_missing?`?
**A:** When no method matches, Ruby calls `method_missing(name, *args, &block)`; handle it and `super` for the rest. `respond_to?`/`method()` don't know dynamic names, so override `respond_to_missing?` to keep them honest — else the object lies about what it responds to.
• method_missing intercepts unmatched calls; super for the rest • respond_to_missing? keeps respond_to?/method() honest ~ method_missing only, no respond_to_missing?

### ruby-objects-send-02 · objects-methods · W2 · medium
**Q:** What do `send` and `public_send` do, and when do they differ?
**A:** Both call a method by dynamic name (symbol/string) with args. `send` bypasses private/protected; `public_send` enforces visibility (raises for non-public). Prefer `public_send` for untrusted names.
• both call by dynamic name • send bypasses visibility, public_send enforces it ~ knows send, not the visibility difference

### ruby-objects-attr-03 · objects-methods · W2 · easy
**Q:** What do `attr_reader/writer/accessor` generate, and what is `define_method`?
**A:** `attr_reader` = getter, `attr_writer` = setter (`x=`), `attr_accessor` = both, over `@x`. `define_method` defines a method dynamically from a block at runtime, capturing scope — good for generating methods in a loop.
• attr_* accessor trio over @ivar • define_method = dynamic method from a block ~ one of the two

### ruby-objects-visibility-04 · objects-methods · W1 · medium
**Q:** `private` vs `protected`, and what it means for an explicit receiver?
**A:** Private: no explicit receiver (call bare `foo`; `self.foo=` setters allowed). Protected: explicit receiver allowed, but only from a method of the same class/subclass (e.g. comparing instances). Public is default; enforced at call time.
• private = no explicit receiver • protected = explicit receiver, same class/subclass only ~ "private = internal" without the distinction

### ruby-enumerable-include-01 · enumerable · W3 · medium
**Q:** What must you implement to `include Enumerable`, and what do you get?
**A:** Define `each` (yielding elements) and `include Enumerable`; you then get `map`/`select`/`reduce`/`sort`/`min`/`find`/… built on `each` (sort/min/max also need `<=>`).
• define each + include Enumerable • get map/select/reduce/sort/etc for free ~ "include Enumerable" without naming each

### ruby-enumerable-reduce-02 · enumerable · W2 · medium
**Q:** Explain `reduce`/`inject`, and when `each_with_object` fits better.
**A:** `reduce(init){ |acc,x| }` folds to one value — the block's return becomes the next `acc` (must return it); has `reduce(:+)` shorthand. `each_with_object(obj){ |x,memo| }` passes a fixed mutable object you mutate; its block return is ignored — safer for building a Hash/Array.
• reduce folds; block return = next acc • each_with_object mutates a fixed object, return ignored ~ reduce only, not the each_with_object advantage

### ruby-enumerable-lazy-03 · enumerable · W2 · medium
**Q:** What does `.lazy` do to a chain, and why does it matter for big/infinite sequences?
**A:** Eager chains build an intermediate array per step; `.lazy` pulls one element through the whole chain on demand, computing nothing until a terminal call (`first`/`to_a`). Enables infinite sequences and early stop: `(1..Float::INFINITY).lazy.select(&:even?).first(5)`.
• eager builds intermediate arrays; lazy pulls on demand • enables infinite/huge + early termination ~ "more efficient" without on-demand detail

### ruby-enumerable-comparable-04 · enumerable · W2 · medium
**Q:** How do you make instances sortable and comparable with `<`, `>`, `clamp`?
**A:** Define `<=>` returning -1/0/1 (nil if incomparable) and `include Comparable`, which builds `<`/`<=`/`==`/`>`/`>=`/`between?`/`clamp`. `sort`/`min`/`max` also use `<=>`.
• define <=> (-1/0/1, nil) • include Comparable for </between?/clamp; sort uses <=> ~ <=> OR Comparable, not both

### ruby-metaprogramming-singleton-01 · metaprogramming · W2 · hard
**Q:** What is a singleton class, and what does `class << self` in a class body do?
**A:** Every object has a hidden singleton (eigen)class holding its per-object methods. `class << self` opens `self`'s singleton class; in a class body `self` is the class, so it defines class methods — same as `def self.foo`, handy for grouping.
• singleton/eigenclass = per-object methods • class << self opens self's singleton; in a class body = class methods ~ "defines class methods" without singleton-class

### ruby-metaprogramming-ivar-02 · metaprogramming · W1 · medium
**Q:** What do `instance_variable_get/set` do, and when appropriate?
**A:** Read/write an ivar by name at runtime (`obj.instance_variable_get(:@x)`, `:@` included), bypassing accessors. Useful for serialization/introspection/test setup, but reaching into another object's ivars breaks encapsulation.
• get/set ivar by symbol name at runtime • bypasses accessors; introspection use but breaks encapsulation ~ knows it accesses ivars, not the by-name / encapsulation point

### ruby-metaprogramming-definemethod-vs-mm-03 · metaprogramming · W2 · hard
**Q:** Trade-off between `define_method` and `method_missing` for dynamic methods?
**A:** `define_method` makes real named methods up front — fast, introspectable, needs names known in advance. `method_missing` intercepts lazily — handles open-ended names but is slower, invisible to introspection (needs `respond_to_missing?`), harder to trace. Prefer `define_method` when names are known.
• define_method = real methods up front (fast/introspectable/known names) • method_missing = lazy/open-ended but slower/invisible ~ one side only, no trade-off

### ruby-metaprogramming-classnew-04 · metaprogramming · W1 · medium
**Q:** What does `Class.new` do, and how does constant assignment matter?
**A:** `Class.new(superclass){ ... }` creates an anonymous class at runtime (block = body). It stays anonymous until first assigned to a constant, which gives it its name (`Foo = Class.new` ⇒ `name == "Foo"`). `Module.new` is the module analog.
• Class.new = anonymous class at runtime (block is body) • gets its name on first constant assignment ~ "creates a class dynamically" without the naming detail

### ruby-error-standarderror-01 · error-handling · W3 · hard
**Q:** A bare `rescue` catches `StandardError`, not `Exception`. Why is rescuing `Exception` almost always wrong?
**A:** `Exception` is the root; `StandardError` is the recoverable subtree. Non-StandardError types — `SignalException`/`Interrupt` (Ctrl-C), `SystemExit` (`exit`), `NoMemoryError`, `ScriptError` — should pass through. Rescuing `Exception` swallows them (breaks Ctrl-C/exit). Use `rescue StandardError`; custom errors subclass it.
• bare rescue = StandardError; Exception is broader root • rescuing Exception swallows Signal/SystemExit (breaks Ctrl-C/exit) • so rescue StandardError / subclass it ~ "bad practice" with no reason

### ruby-error-ensure-retry-02 · error-handling · W2 · medium
**Q:** When does `ensure` run, and what does `retry` do?
**A:** `ensure` always runs on exit — normal, rescued, or propagating — for cleanup (close/unlock). `retry` (in a `rescue`) jumps back to the top of `begin` and re-runs it; needs a counter/backoff or it loops forever.
• ensure always runs on exit (cleanup) • retry re-runs begin from the top; needs a bound ~ one of the two

### ruby-error-custom-03 · error-handling · W2 · medium
**Q:** How do you define and raise a custom exception, and what forms can `raise` take?
**A:** Subclass `StandardError` (`class NotFoundError < StandardError; end`) so bare `rescue` catches it. Raise via `raise NotFoundError, "msg"` or `raise obj`. `raise "text"` = `RuntimeError`; bare `raise` in a rescue re-raises (keeps backtrace).
• subclass StandardError • raise Class,'msg' / raise obj; raise 'str' = RuntimeError; bare raise re-raises ~ subclasses Exception, or only raise 'string'

### ruby-error-elserescue-04 · error-handling · W1 · easy
**Q:** `rescue` works in a method body without `begin`. What does the `else` clause add?
**A:** `def`/blocks/class bodies have an implicit `begin`, so `rescue`/`ensure`/`else` work without one. `else` runs only if no exception was raised — the success path you deliberately keep outside the rescued region.
• method body has implicit begin • else runs only when no exception raised (success path) ~ one of the two

### ruby-strings-symbols-01 · strings-symbols · W3 · medium
**Q:** Symbol vs String — identity, mutability, typical use?
**A:** A String is mutable, usually a new object per literal. A Symbol (`:name`) is immutable and interned — every `:name` is the same object (fast identity compare, no allocation). Use symbols for fixed names/keys, strings for text/data. Modern symbols are GC-able.
• String mutable, new object per literal • Symbol immutable/interned — same object everywhere ~ "lightweight strings" without interned/mutable distinction

### ruby-strings-frozen-02 · strings-symbols · W2 · medium
**Q:** What does `# frozen_string_literal: true` do, and a subtlety about interpolation?
**A:** It freezes the file's plain string literals (immutable + reusable — perf/safety). Subtlety: since Ruby 3.0 interpolated literals like `"hi #{name}"` are NOT frozen under the pragma — only static literals. Use `.freeze` or `+"..."`/`String.new` to override explicitly.
• magic comment freezes the file's string literals • since 3.0 interpolated literals not frozen (only static) ~ knows it freezes literals, not the interpolation caveat

### ruby-strings-quotes-03 · strings-symbols · W1 · easy
**Q:** Single- vs double-quoted strings, and what is interpolation?
**A:** Double quotes support interpolation (`"Hi #{name}"`, inserts `to_s`) and escapes (`\n`, `\t`). Single quotes are literal — no interpolation, only `\\` and `\'` special. Use single for raw text, double for interpolation/escapes.
• double: interpolation + escapes • single: literal, only \\ and \' ~ mentions interpolation but claims single quotes interpolate too

### ruby-strings-percent-04 · strings-symbols · W1 · easy
**Q:** What do `%w[...]` and `%i[...]` produce?
**A:** Whitespace-split array literals: `%w[a b c]` → `["a","b","c"]` (strings), `%i[a b c]` → `[:a,:b,:c]` (symbols); no commas/quotes. `%W`/`%I` allow interpolation.
• %w = array of strings • %i = array of symbols ~ one of the two, or "array literal" without which is symbols

### ruby-collections-hashdefault-01 · collections-idioms · W2 · medium
**Q:** `Hash.new(0)` vs `Hash.new { |h,k| h[k] = [] }` — difference and why it matters?
**A:** `Hash.new(0)` = one shared default value (great for counters; a mutable default is shared/buggy). The block runs per missing key; `h[k] = []` returns AND stores a fresh object — the idiom for a hash of arrays. Note: a bare default doesn't insert the key.
• Hash.new(0) = one shared default (counters; mutable shared is buggy) • block runs per key; h[k]=[] returns and stores fresh ~ "sets a default" without shared/per-key distinction

### ruby-collections-kwargs-02 · collections-idioms · W3 · hard
**Q:** Ruby 3.0 separated keyword args from a trailing Hash. What changed, and how do you pass a hash as keywords now?
**A:** Pre-3.0 a trailing Hash auto-converted to/from keyword args; 2.7 deprecated it, 3.0 removed it — they're now separate. So `def m(key:)` no longer accepts a plain `{key: 1}` positional hash; splat it: `m(**hash)`. Collect arbitrary keywords with `def m(**opts)`.
• pre-3.0 trailing-Hash ↔ keyword auto-conversion; 3.0 separated them • pass a hash as keywords with ** (m(**hash)); def m(**opts) ~ knows ** but not the 3.0 change

### ruby-collections-splat-03 · collections-idioms · W2 · medium
**Q:** What do `*` and `**` do in method definitions and calls?
**A:** In params: `*args` gathers extra positionals into an Array, `**kwargs` gathers keywords into a Hash (no overlap). At a call site: `*arr` / `**hash` spread them back into args. `*` also destructures (`first, *rest = list`); `...` forwards all args (3.0+).
• params: *args = positionals Array, **kwargs = keywords Hash • call site: *arr / **hash spread ~ "handles variable args" without positional-vs-keyword / gather-vs-spread

### ruby-collections-safenav-data-04 · collections-idioms · W2 · medium
**Q:** What does `&.` do, and what is `Data.define` (Ruby 3.2)?
**A:** `a&.b` calls `b` only if `a` isn't nil, else short-circuits to nil (avoids `NoMethodError`). `Data.define(:x, :y)` (3.2) builds an immutable value-object class: frozen instances, readers only, positional-or-keyword construction, `#with` for updated copies — a leaner, strict `Struct`.
• &. calls only if receiver non-nil, else nil (avoids NoMethodError) • Data.define (3.2) = immutable value-object class (frozen, readers, #with) ~ &. only, Data.define wrong/missing or called mutable
