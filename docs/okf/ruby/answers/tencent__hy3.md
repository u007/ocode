# ruby knowledge benchmark — answers
# Model: novita-ai/tencent/hy3   (provider-stripped id: tencent/hy3)
# CLOSED-BOOK via ocode run -dir <empty> (no corpus access). corpus_rev 1.

- id: ruby-blocks-procvslambda-01
  answer: |
    Two main behavioral differences:
    1. Argument handling (arity). A lambda enforces the number of arguments strictly — passing the wrong count raises ArgumentError. A Proc is lenient: extra arguments are ignored and missing ones are supplied as nil.
    2. `return` / `break` semantics. In a lambda, `return` returns from the lambda itself (local return). In a Proc, `return` performs a non-local return — it returns from the method that enclosed the Proc's definition (and `break` similarly exits that method). The same contrast applies to `next`/`break` behavior.

- id: ruby-blocks-yield-02
  answer: |
    `yield` executes the block that was passed to the method (the implicit block), optionally passing arguments to it (`yield arg`) and capturing its return value. `block_given?` returns true only if a block was actually passed to the current method. It protects against calling `yield` when no block was supplied, which would otherwise raise a `LocalJumpError`.

- id: ruby-blocks-ampblock-03
  answer: |
    In `def m(&block)`, the `&` takes the implicitly-passed block and converts it into a Proc object bound to the `block` parameter (and conversely, `&block` at a call site turns a Proc back into a block).
    In `arr.map(&:to_s)`, `&` calls `to_proc` on the symbol `:to_s`, producing a Proc equivalent to `{ |x| x.to_s }`, which is then passed as the block to `map`. So `&:to_s` is shorthand for `map { |x| x.to_s }`.

- id: ruby-blocks-create-04
  answer: |
    Create a lambda: `lambda { |x| x * 2 }` or the shorthand `->(x) { x * 2 }`.
    Create a Proc: `Proc.new { |x| x }`, `proc { |x| x }`, or by capturing a block with `&block`.
    Invoke either a Proc or a lambda with any of: `obj.call(1)`, `obj[1]`, or `obj.(1)` (the `.(...)` sugar).

- id: ruby-modules-includeextendprepend-01
  answer: |
    - `include M`: inserts M into the ancestor chain just below the class, so its methods become instance methods of the class.
    - `extend M`: adds M's methods as singleton (class-level) methods — effectively `obj.extend M` adds them to that object's singleton class.
    - `prepend M`: inserts M ABOVE the class in the ancestor chain, so M's methods override the class's own methods of the same name.
    So `include`/prepend add instance methods (different chain position), while `extend` adds singleton/class methods.

- id: ruby-modules-ancestors-super-02
  answer: |
    Method lookup walks the ancestor chain in order. `super` calls the next definition of the same method name found higher up that chain, passing along arguments (or `super()` with none, or `super` with the original args). Included modules are inserted into the chain: `include` places them between the class and its superclass; `prepend` places them before the class. So `super` will resolve through these modules in chain order.

- id: ruby-modules-namespace-03
  answer: |
    Besides mixins, a module's other primary use is as a NAMESPACE to group related classes, constants, and methods under one name and avoid top-level name collisions (e.g. `MyApp::Config`).
    A callable module function is defined either as `def self.foo; end` (a singleton method on the module) or with `module_function`, which makes a method private inside the module but callable as a public module function via `ModuleName.method` (e.g. `Math.sqrt`).

- id: ruby-modules-refinements-04
  answer: |
    Refinements solve the global side effects of monkey-patching (reopening a class changes behavior everywhere, risking conflicts and surprising other code). Refinements scope the method overrides to a limited lexical area instead of globally.
    They are defined with `refine Klass do ... end` inside a module, and activated with `using MyRefinements` at file, class, or method scope — after which the refined behavior applies only in that lexical region.

- id: ruby-objects-methodmissing-01
  answer: |
    `method_missing(name, *args, &block)` is invoked automatically by Ruby when a message is sent to an object that has no matching method, letting you implement dynamic/dispatch behavior (e.g. dynamic attribute access).
    You must also define `respond_to_missing?(name, include_private)` and have it return true for those synthetic methods. Otherwise `respond_to?` will report false for capabilities the object actually handles, breaking the object's public contract and tools that rely on it.

- id: ruby-objects-send-02
  answer: |
    `send` (and `public_send`) invoke a method by name (given as a String or Symbol), passing arguments and a block: `obj.send(:foo, 1)`.
    They differ on visibility: `send` can call private and protected methods; `public_send` respects visibility and raises `NoMethodError` for private methods. Use `public_send` when you want to preserve access control.

- id: ruby-objects-attr-03
  answer: |
    `attr_reader :x` generates a getter `def x; @x; end`. `attr_writer :x` generates a setter `def x=(v); @x = v; end`. `attr_accessor :x` generates both. They can take a second boolean arg to make the getter also settable like a private writer.
    `define_method` is a metaprogramming primitive that defines a real named method dynamically at runtime, given a name and a block/Proc as the method body (so you can generate methods in loops or based on data).

- id: ruby-objects-visibility-04
  answer: |
    The practical difference is about the receiver. A `private` method cannot be called with an explicit receiver at all — not even `self.foo`; it must be called with an implicit `self`. A `protected` method CAN be called with an explicit receiver, but only when that receiver is `self` or another instance of the same class (or a subclass).
    So protected allows `other_instance.foo` inside the class body, while private forbids any explicit receiver.

- id: ruby-enumerable-include-01
  answer: |
    To make your class `include Enumerable`, you must implement `each`, which yields each element of the collection in turn. (Implementing `<=>` is also needed for ordering-related methods like `sort`, but `each` is the core requirement.)
    In return you get the entire Enumerable toolkit for free: `map`, `select`, `reject`, `find`, `reduce`, `include?`, `count`, `sort`, `group_by`, `min`/`max`, `to_a`, `to_h`, etc., all built on top of `each`.

- id: ruby-enumerable-reduce-02
  answer: |
    `reduce`/`inject` folds a collection into a single accumulated value: you supply an optional initial memo and a block `(memo, element) -> new_memo`; the final memo is returned. With a symbol it can use that symbol as a binary method (e.g. `inject(:+)`).
    `each_with_object(obj)` is better when you want to build or mutate a collector object (commonly a Hash or Array) and have it returned: the block receives `(element, memo)`, and the memo is returned automatically. It avoids having to return the memo at the end of every block iteration and sidesteps issues with non-commutative accumulation or when the accumulator must be mutated in place.

- id: ruby-enumerable-lazy-03
  answer: |
    `.lazy` wraps the enumerable in a lazy enumerator that evaluates elements one at a time and builds no intermediate arrays, so chained operations (map/select/etc.) are composed and applied per-element rather than eagerly producing a full array at each step.
    This matters for large or infinite sequences: it lets you take a finite result from an infinite source (e.g. `(1..Float::INFINITY).lazy.map{...}.select{...}.first(10)`) and avoids huge memory use, since work is done lazily and only as needed.

- id: ruby-enumerable-comparable-04
  answer: |
    `include Comparable` in your class and define the `<=>` ("spaceship") operator returning `-1`, `0`, or `1` (or `nil` for non-comparable). Comparable then provides `<`, `>`, `<=`, `>=`, `==`, `between?`, and (since Ruby 2.7) `clamp` for free, all derived from `<=>`.

- id: ruby-metaprogramming-singleton-01
  answer: |
    A singleton class (eigenclass) is the hidden, per-object class that holds an object's singleton methods (methods defined only for that one object). Every object has one.
    `class << self` inside a class body opens the singleton class of `self` (the class object itself), so methods defined within that block become class-level singleton methods — i.e. class methods. The same construct works on any object to add singleton methods to it.

- id: ruby-metaprogramming-ivar-02
  answer: |
    `instance_variable_get(:@x)` reads the value of the named instance variable (even private), and `instance_variable_set(:@x, value)` writes it. Both take a Symbol (or String) naming the variable.
    They are appropriate in metaprogramming, serialization, ORM mappers, and dynamic attribute handling where the variable name is computed at runtime. In ordinary application code they should be avoided because they bypass encapsulation and the normal attribute API.

- id: ruby-metaprogramming-definemethod-vs-mm-03
  answer: |
    `define_method` creates a real method at definition time: it's fast per-call, visible to reflection and `respond_to?`, and enumerable, but every possible method must be known/generated up front (cost at definition).
    `method_missing` handles unknown calls at runtime dynamically: extremely flexible (can respond to arbitrary names) and cheap to set up, but slower per call, not listed by reflection, and requires `respond_to_missing?` to keep `respond_to?` honest. Trade-off: upfront concrete methods (performance/visibility) vs. on-demand dynamic dispatch (flexibility).

- id: ruby-metaprogramming-classnew-04
  answer: |
    `Class.new` creates a new, anonymous (unnamed) Class object at runtime; you can pass a superclass as `Class.new(Super)` and a block as the class body (e.g. `Class.new(Array) { def foo; end }`).
    Assigning it to a constant (`MyClass = Class.new`) gives the class a name — in Ruby a class gets its name from the constant it's bound to. Naming matters for error messages, `klass.name`, and it prevents anonymous classes from being garbage-collected; assigning is the normal way to make a usable named class.

- id: ruby-error-standarderror-01
  answer: |
    A bare `rescue` / `rescue => e` rescues `StandardError` and its subclasses, which are the "ordinary" runtime exceptions. `Exception` is the root of the hierarchy and includes things you almost never want to swallow: `SystemExit` (raised by `exit`), `SignalException` (Ctrl-C / kill), `Interrupt`, `NoMemoryError`, `LoadError`, and `ScriptError`.
    Rescuing `Exception` therefore traps program termination and interruption signals, prevents clean shutdowns, can hide catastrophic failures like out-of-memory, and makes the program impossible to kill normally — so it's almost always wrong.

- id: ruby-error-ensure-retry-02
  answer: |
    `ensure` runs unconditionally — whether the body completed normally, returned, or raised an exception — and runs before the method/program exits that scope; it's used for cleanup (closing files, releasing locks). It runs regardless, but cannot suppress an exception.
    `retry` (used inside a `rescue`) re-executes the entire `begin` block from the top, letting you attempt recovery and retry the operation (often with a counter to avoid infinite loops).

- id: ruby-error-custom-03
  answer: |
    Define a custom exception by subclassing an appropriate base, typically `StandardError`: `class MyError < StandardError; end`. Then raise it with `raise MyError, "message"` or `raise MyError.new("message")`.
    `raise` forms: `raise` (re-raises the current exception inside a rescue), `raise "msg"` (raises a RuntimeError with that message), `raise SomeError`, `raise SomeError, "msg"`, `raise SomeError.new("msg")`, and `raise obj, msg, backtrace` (where obj is an exception instance). Never rescue/rescue far above StandardError.

- id: ruby-error-elserescue-04
  answer: |
    Ruby implicitly wraps a method body in a `begin/rescue`, so you can write `rescue` directly in a method without an explicit `begin` — the whole method body is the protected region.
    The optional `else` clause runs only when the `begin` body completes with NO exception. It runs after the body and before `ensure`, and is useful for code that should run only on success and that itself should not be protected by the rescue.

- id: ruby-strings-symbols-01
  answer: |
    A Symbol (`:foo`) is an immutable, interned identifier: every occurrence of the same symbol refers to the same single object, so identity comparison is cheap. Strings (`"foo"`) are mutable sequences of characters compared by content.
    Symbols are typically used as keys, method names, and enum-like identifiers where uniqueness and immutability matter; strings for text/data. Symbols used to never be garbage-collected (before Ruby 2.2, all symbols; dynamic symbols since 2.2 are), while strings are collected normally. Prefer symbols for hash keys and internal names, strings for user-facing text.

- id: ruby-strings-frozen-02
  answer: |
    `# frozen_string_literal: true` placed at the top of a file makes all (static) string literals in that file immutable (frozen), which avoids accidental per-use duplication and reduces allocations, improving performance and catching unintended mutation.
    Subtlety: interpolated strings (`"a#{x}b"`) are NOT frozen even with the magic comment — they are freshly allocated and mutable each evaluation. Also, strings built by other means or via String interpolation remain unfrozen, so don't assume every string in the file is frozen.

- id: ruby-strings-quotes-03
  answer: |
    Single-quoted strings are literal: they process only `\\` and `\'` escapes and do not support interpolation. Double-quoted strings support string interpolation (`#{expression}`) and a full set of escape sequences (`\n`, `\t`, `\"`, etc.).
    String interpolation embeds Ruby expressions inside `#{}` within a double-quoted (or other interpolating) string, evaluated at runtime and converted to string, e.g. `"Hello #{name}"`.

- id: ruby-strings-percent-04
  answer: |
    `%w[a b c]` is a whitespace-separated array literal that produces an Array of Strings: `['a', 'b', 'c']` (no interpolation, single-token entries).
    `%i[a b c]` is the same but produces an Array of Symbols: `[:a, :b, :c]`. Both avoid quotes/commas and are handy for short lists; brackets can be any matching delimiter (`%w(...)`, `%i{...}`, etc.).

- id: ruby-collections-hashdefault-01
  answer: |
    `Hash.new(0)` sets a fixed default RETURN value: accessing a missing key returns 0 but does NOT store the key, and the same default object is shared. `Hash.new { |h, k| h[k] = [] }` uses a default block that runs on missing-key access and, here, assigns a fresh empty array to that key, auto-vivifying it.
    The difference matters for grouping/counting: the block form creates a distinct per-key value and stores it, while the value form does not store entries (so `h.keys` won't reflect them) and a mutable default like `Hash.new([])` would be a single shared array mutated across all keys — a classic bug. Use the block form for auto-vivification; use the value form only for simple read defaults.

- id: ruby-collections-kwargs-02
  answer: |
    In Ruby 3.0 keyword arguments were fully separated from positional hashes (the 2.7 "delegation" deprecation became a hard separation). A trailing `Hash` passed positionally is no longer automatically treated as keyword arguments, and `**` is required to convert/forward keywords explicitly.
    To pass a hash's contents as keywords, expand it with `**`: `method(**my_hash)`. To accept arbitrary keywords, declare `def m(**opts)`. To capture both, combine `def m(*args, **kwargs, &block)`. This prevents accidental hash/keyword confusion that 2.x allowed.

- id: ruby-collections-splat-03
  answer: |
    The splat `*` collects extra positional arguments into an Array in a method definition (`def f(a, *rest)`) and expands an Array into positional arguments at a call site (`f(*arr)`). It can also be used to split arrays (`head, *tail = arr`).
    The double-splat `**` collects extra keyword arguments into a Hash in a definition (`def f(**kw)`) and expands a Hash into keyword arguments at a call (`f(**h)`). Together they handle variable positional and keyword argument bundling/forwarding.

- id: ruby-collections-safenav-data-04
  answer: |
    The safe-navigation operator `&.` calls the following method only if the receiver is not `nil` (and not `false`); if the receiver is nil, the whole chain short-circuits and returns nil instead of raising `NoMethodError`. E.g. `user&.address&.city`.
    `Data.define(:x, :y)` (Ruby 3.2) creates a lightweight, immutable value-object class with read-only accessors for the listed members — like a frozen Struct. Instances are created with `.new(x, y)`, are value-based (equal if members equal), and cannot be modified, making them ideal for immutable data carriers.
