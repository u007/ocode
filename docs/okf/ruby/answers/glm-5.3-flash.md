```yaml
- id: ruby-blocks-procvslambda-01
  answer: |
    1) Arity: a lambda enforces argument count (raises ArgumentError on wrong number of args); a Proc is lenient (extra args discarded, missing ones become nil). 2) `return`: inside a lambda, `return` returns only from the lambda itself; inside a Proc, `return` returns from the enclosing method (raises LocalJumpError if there is no enclosing method). Lambdas behave like anonymous methods; Procs behave like blocks.
- id: ruby-blocks-yield-02
  answer: |
    `yield` invokes the block that was passed to the current method, passing any arguments to it. `block_given?` returns true if the caller supplied a block. It protects against calling `yield` with no block present, which raises LocalJumpError ("no block given (yield)") — so the idiom is `yield x if block_given?` or providing a fallback behavior.
- id: ruby-blocks-ampblock-03
  answer: |
    In `def m(&block)`, `&` captures the block passed to the method and converts it into a Proc object bound to the parameter `block` (callable via `block.call`); re-passing `&block` to another method converts it back into a block. In a call like `arr.map(&:to_s)`, `&` takes an object that responds to `to_proc` (here a Symbol) and converts it into a block — equivalent to `map { |x| x.to_s }`. Procs/lambdas can also be passed this way: `map(&my_proc)`.
- id: ruby-blocks-create-04
  answer: |
    Create a lambda: `lambda { |x| x * 2 }` or the stabby lambda `->(x) { x * 2 }`. Create a Proc: `Proc.new { |x| x }` or `proc { |x| x }`. Invoke with any of: `.call(args)` (or `call` with no args), the syntactic sugar `.()`, index syntax `.[]`, or `.yield` (alias of call). E.g. `f = ->(x) { x + 1 }; f.call(2)`, `f.(2)`, `f[2]`.
- id: ruby-modules-includeextendprepend-01
  answer: |
    `include M`: M's methods become instance methods of the class; M is inserted into the ancestor chain immediately above the class (before the superclass). `extend M`: M's methods become singleton methods of the receiver — for a class, they become class methods (they land in the receiver's singleton class). `prepend M`: M's methods also become instance methods, but M is inserted BELOW (ahead of) the class in the ancestor chain, so M's methods override the class's own same-named methods — typically with M's methods calling `super` to reach the class (decorator/wrapper pattern). Ancestor order roughly: class's singleton chain for extend; for instances: prepended modules, then the class, then included modules (last-included first), then superclass.
- id: ruby-modules-ancestors-super-02
  answer: |
    `super` calls the next matching method up the linearized ancestor chain (`Klass.ancestors`, which interleaves classes and included/prepended modules). Included modules sit between the class and its superclass; if several modules are included, they are searched most-recently-included first, so `super` walks into them in that order. Bare `super` forwards the current method's arguments unchanged; `super()` passes none; `super(a, b)` passes explicit args. If no further method exists, a NoMethodError (or RuntimeError for missing `initialize`) results.
- id: ruby-modules-namespace-03
  answer: |
    The other primary use of `module` is namespacing constants — grouping classes, modules, and constants under a qualified name (`module Payments; class Gateway; end; end` → `Payments::Gateway`). A callable module function is made with `module_function` (e.g., `module M; module_function; def helper; end; end`, or `module_function :helper`), which makes the method a private instance method AND a singleton method on the module, so you can call `M.helper`. `def self.helper` or `extend self` are common alternatives.
- id: ruby-modules-refinements-04
  answer: |
    Refinements solve the global-blast-radius problem of monkey-patching: reopening a class changes it for every consumer process-wide, but a refinement's patched behavior is only active where you explicitly opt in. Define one in a module with `refine SomeClass do ... end`, then activate it lexically with `using MyRefinement` — at the top of a file, or inside a class/module body, which scopes the change to that scope. `using` cannot be called inside a method, and the refinement is invisible outside the file/scope that activated it.
- id: ruby-objects-methodmissing-01
  answer: |
    When normal method lookup fails, Ruby calls `method_missing(name, *args, &block)` on the receiver, letting you intercept unknown calls (proxies, dynamic finders, DSLs). When you handle some names, you must also define `respond_to_missing?(name, include_private = false)` returning true for the handled names (and call `super` otherwise) so that `respond_to?`, duck-typing checks, serialization, and introspection agree with what the object actually accepts. Without it, `respond_to?(:some_dynamic_method)` returns false even though calling it works, and error reporting/tooling misbehaves.
- id: ruby-objects-send-02
  answer: |
    Both invoke a method on an object by name (Symbol or String) with the given arguments, enabling dynamic dispatch when the method name is computed at runtime. They differ when the target method is not public: `send` can invoke private and protected methods, while `public_send` raises NoMethodError for them, respecting visibility. Use `public_send` when you want to honor encapsulation (e.g., user-supplied method names); `send` when you legitimately need internals.
- id: ruby-objects-attr-03
  answer: |
    `attr_reader :x` defines a getter `x`; `attr_writer :x` defines a setter `x=`; `attr_accessor :x` defines both. Each returns the given value for the reader and the assigned value for the setter, backed by an instance variable. `define_method(:name) { |args| ... }` defines a method dynamically from a block (a Method created via closure) — used in metaprogramming to generate many methods in a loop, with the block closing over local variables, avoiding string-eval.
- id: ruby-objects-visibility-04
  answer: |
    `private` methods cannot be called with any explicit receiver — they are only callable in implicit-receiver form (`foo`), i.e., always on `self`. The one exception: since Ruby 2.7, `self.foo` (literal `self` as receiver) is allowed. `protected` methods can be called with an explicit receiver, but only when the receiver is an instance of the same class (or a subclass) as the caller — designed for cases like comparison methods where one object must read another's internal state. So `obj.private_method` raises NoMethodError, while `obj.protected_method` works if `self` is a compatible instance.
- id: ruby-enumerable-include-01
  answer: |
    You implement `each`, yielding each element of your collection (with `yield` or via a passed block). In exchange you get the entire Enumerable toolkit for free: `map`, `select`/`reject`, `reduce`, `flat_map`, `group_by`, `sort`, `min`/`max` (needs elements to be mutually comparable), `include?`, `count`, `each_slice`, `each_cons`, `tally`, `zip`, `lazy`, etc. If you also implement `<=>` and include Comparable, you get full sorting/comparison behavior.
- id: ruby-enumerable-reduce-02
  answer: |
    `reduce` (alias `inject`) folds a collection into a single value: with an initial value `coll.reduce(init) { |acc, el| ... }` the block's return value becomes the next accumulator; without an initial value, the first element is used as the seed (and the block receives it as acc on the first iteration). `each_with_object(obj) { |el, memo| ... }` iterates while building/mutating a separate accumulator object, which it returns; the block's return value is ignored and the argument order is element first, memo second. `each_with_object` fits better when the result is a mutable structure you accumulate into (a hash or array) by side effect rather than by returning a new accumulator each step.
- id: ruby-enumerable-lazy-03
  answer: |
    `.lazy` wraps the Enumerable chain in a Lazy enumerator: the chained `map`/`select`/etc. are composed into a pipeline and evaluated element-by-element only on demand, when a terminal operation (`first`, `take`, `force`/`to_a`, iteration) pulls values. Without it, each step materializes a full intermediate array. This matters for large or infinite sequences: `(1..Float::INFINITY).lazy.select { ... }.map { ... }.first(n)` terminates, whereas the eager version would hang building an infinite array; it also avoids the memory cost of intermediate collections.
- id: ruby-enumerable-comparable-04
  answer: |
    Include the `Comparable` module into your class and define `<=>` (the spaceship operator) returning -1, 0, 1, or nil for incomparable values. Comparable then defines `<`, `<=`, `==`, `>=`, `>`, `between?`, and `clamp` in terms of `<=>`, and instances sort correctly with `sort`/`min`/`max`. Example: `class Version; include Comparable; attr_reader :parts; def initialize(str) = @parts = str.split('.').map(&:to_i); def <=>(other) = parts <=> other.parts; end`.
- id: ruby-metaprogramming-singleton-01
  answer: |
    A singleton class (eigenclass) is the anonymous per-object class that stores methods defined for one particular object (`def obj.foo; end` goes there); it sits at the front of that object's lookup chain, and `obj.singleton_class` retrieves it. Inside a class body, `class << self ... end` opens the singleton class of the class itself, so methods defined there become class methods (callable as `Klass.method`). It's also where you attach class-level macros/attributes (e.g., `attr_accessor` on the singleton class) and where `extend` puts module methods.
- id: ruby-metaprogramming-ivar-02
  answer: |
    `instance_variable_get(:@x)` reads an instance variable directly by name (nil if unset; the symbol must include the `@`), and `instance_variable_set(:@x, value)` writes one; `instance_variables` lists them. They are appropriate in metaprogramming/framework code where variable names are computed dynamically (ORMs, serializers, builders) or for bulk introspection. In ordinary code, prefer accessors or direct references to preserve encapsulation and let readers/writers (validation, coercion) run.
- id: ruby-metaprogramming-definemethod-vs-mm-03
  answer: |
    `define_method` creates real, concrete methods: `respond_to?` works, method lookup and invocation are fast, and introspection sees them — but methods are created eagerly and consume memory per generated method, so you must anticipate the names. `method_missing` handles arbitrary/unknown names lazily with no per-name cost — good for open-ended proxies — but each call pays the full failed-lookup path (slower), ghost methods are invisible to `respond_to?` unless you also add `respond_to_missing?`, and you must remember to call `super` for names you don't handle or you'll break genuine NoMethodError reporting and debugging. Rule of thumb: known, bounded set of names → define_method; unbounded/dynamic → method_missing.
- id: ruby-metaprogramming-classnew-04
  answer: |
    `Class.new` creates an anonymous class at runtime, optionally with a superclass (`Class.new(Base)`), and you can pass a block that is class-eval'd to define methods/attributes. Assigning it to a constant names the class: a class's `name`/`to_s` derive from the first constant it is assigned to, and naming matters for introspection (`#name`, `#inspect`), Marshal/serialization, Rails-style resolution of class names from strings, and reloading. An unnamed anonymous class is hard to reference later and serializes poorly.
- id: ruby-error-standarderror-01
  answer: |
    `rescue` without a class only catches StandardError and its subclasses — the errors applications are expected to recover from. Rescuing `Exception` also intercepts system-level errors you should almost never swallow: SignalException (Ctrl-C/Interrupt), SystemExit (`exit`), NoMemoryError, SystemStackError, ScriptError (SyntaxError/LoadError). Catching those prevents the process from responding to signals or shutting down cleanly and can hide fatal, unrecoverable conditions — your app becomes an unkillable zombie. If you must catch broadly, re-raise (`raise`) anything you don't actually handle.
- id: ruby-error-ensure-retry-02
  answer: |
    `ensure` runs unconditionally after the begin body and any rescue clauses — whether the body succeeded, raised, was rescued, or exits via return/break/next — making it the right place for cleanup (closing files/connections). Caveat: a `return` inside `ensure` overrides the body's result. `retry` (usable only inside a rescue clause) restarts execution of the entire begin block from the top; it must be guarded (e.g., a retry counter) or a persistently failing block loops forever.
- id: ruby-error-custom-03
  answer: |
    Define by subclassing: `class PaymentFailed < StandardError; end`, optionally adding attributes via a custom `initialize`/`attr_reader` or using `error.message`. `raise` forms: `raise` (bare, re-raises the current exception within a rescue), `raise "msg"` (RuntimeError), `raise SomeError` (no message), `raise SomeError, "msg"` (class plus message), and `raise SomeError, "msg", backtrace_array` (with explicit backtrace). Preceding a raise with `new` — `raise SomeError.new("msg")` — also works and allows custom constructor args.
- id: ruby-error-elserescue-04
  answer: |
    A method body acts as its own implicit begin/end, so `def foo; ...; rescue; ...; end` works without an explicit `begin`. The optional `else` clause (placed after all rescue clauses, before ensure) runs only when the begin body completed WITHOUT raising anything. It separates the happy-path follow-up code from the protected region, so an exception raised by that follow-up code is NOT caught by the rescue clauses above it (which only guard the begin body), making control flow explicit.
- id: ruby-strings-symbols-01
  answer: |
    A Symbol is an immutable, interned identifier: the same symbol content is always the same object (equal object_id, cheap `==`/hash, hash-key-friendly). Symbols created dynamically are garbage-collectable since Ruby 2.2. A String is a mutable object; each literal is a new object (unless frozen), with a rich text-manipulation API. Typical use: Symbols for identifiers, hash keys, method names, internal program labels; Strings for actual text, user input/output, and content you build or mutate.
- id: ruby-strings-frozen-02
  answer: |
    The magic comment `# frozen_string_literal: true` (first meaningful line of a file) makes every plain string literal in that file frozen: mutating one raises FrozenError, and the runtime can deduplicate identical literals into one object, avoiding allocation churn. Subtlety: strings containing interpolation are NOT frozen by the comment — `"hi #{name}"` produces a fresh mutable string each evaluation, because interpolation is runtime construction, not a pure literal. So code relying on the comment for safety can still create unfrozen strings anywhere interpolation appears.
- id: ruby-strings-quotes-03
  answer: |
    Single-quoted literals are mostly literal: no interpolation, no escape sequences except `\'` and `\\` (`'\n'` is backslash-n). Double-quoted literals support interpolation `#{expression}` (result of `to_s` is embedded) and escape sequences like `\n`, `\t`, `\u00E9`, plus more escape forms. Interpolation (`"Hello, #{user.name}"`) evaluates arbitrary Ruby code and splices its string form into the string.
- id: ruby-strings-percent-04
  answer: |
    `%w[...]` produces an Array of Strings, splitting the contents on whitespace (`%w[a b c]` → `["a", "b", "c"]`). `%i[...]` produces an Array of Symbols the same way (`%i[a b c]` → `[:a, :b, :c]`). Capitalized `%W` and `%I` variants additionally support interpolation.
- id: ruby-collections-hashdefault-01
  answer: |
    `Hash.new(0)` supplies a fixed default VALUE: reading a missing key returns 0 but stores nothing (key stays absent). `Hash.new { |h, k| h[k] = [] }` runs a block on miss, and the assignment inside persists the new array under that key. The difference matters because a fixed default returns the same object on every miss — with a mutable default like `Hash.new([])`, every missing key hands you the SAME array, so in-place mutation corrupts all keys and nothing is stored per key. The block form gives each key its own fresh (and persisted) value, which is why it's the standard `h[k] << x` accumulator idiom.
- id: ruby-collections-kwargs-02
  answer: |
    In Ruby 2, a trailing Hash literal could be silently absorbed into keyword parameters and keywords could leak back into a trailing positional Hash — ambiguous and error-prone. Ruby 3 made them distinct: passing a positional Hash where keywords are expected raises ArgumentError (need explicit `**hash`), and methods that accept a positional hash no longer swallow keyword arguments. To pass a hash as keywords, splat it: `foo(**my_hash)`; to collect arbitrary keywords, declare `def m(**kwargs)` and they arrive as a plain Hash; `m(hash)` alone no longer works for keyword params.
- id: ruby-collections-splat-03
  answer: |
    In method definitions: `*args` gathers surplus positional arguments into an Array (`def m(a, *rest)`), bare `*` swallows and discards them, and `**kwargs` gathers surplus keyword arguments into a Hash. In calls: `*array` expands an Array (or anything responding to to_a/to_ary) into separate positional arguments, and `**hash` expands a Hash into keyword arguments (`m(*[1,2], **{k: 1})` ≡ `m(1, 2, k: 1)`). Splats also appear in destructuring assignment and array literals (`a, *b = list`).
- id: ruby-collections-safenav-data-04
  answer: |
    `&.` (safe navigation) calls the method only when the receiver is not nil; on nil it short-circuits the whole expression to nil — `user&.address&.city` avoids `user && user.address && user.address.city`. It guards nil only (not other falsey values) and its operand must not be `false`-contexts people mistakenly assume. `Data.define` (Ruby 3.2) creates an immutable value class: `Point = Data.define(:x, :y)` gives you positional/keyword constructors, attribute readers, structural `==`/`eql?`/`hash`, pattern-matching support (`deconstruct`/`deconstruct_keys`), and `with` to derive a copy with some fields changed — an immutable, lighter-weight alternative to Struct for value objects.
```
