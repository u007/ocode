# nvidia/nemotron-3.5-super-vl — closed-book answers for ruby (openrouter, 2026-10-16); answerer saw only _prompts/ruby.md
- id: ruby-blocks-procvslambda-01
  answer: |
    A lambda enforces arity (a wrong argument count raises ArgumentError); a Proc is lenient, filling missing arguments with nil and ignoring extras. Also, `return` inside a lambda returns from the lambda itself, while `return` inside a Proc returns from the enclosing method (raising LocalJumpError if that method has already returned).
- id: ruby-blocks-yield-02
  answer: |
    `yield` transfers control to the block passed to the method, passing any arguments to it, and evaluates to the block's last expression; with no block given it raises LocalJumpError. `block_given?` lets you check whether a block was supplied before yielding, so a block can be optional.
- id: ruby-blocks-ampblock-03
  answer: |
    In `def m(&block)`, `&` captures the block passed to the method as a Proc object bound to `block`. In `arr.map(&:to_s)`, `&` converts the symbol `:to_s` into a Proc via `Symbol#to_proc` and passes it as the block argument to `map`.
- id: ruby-blocks-create-04
  answer: |
    Create a lambda with `lambda { |x| ... }`, `->(x, y) { ... }`, or `lambda do ... end`. Invoke a Proc or lambda with `.call(args)`, `.(args)`, `[args]`, or `===` (case equality). A block received by a method is invoked with `yield`.
- id: ruby-modules-includeextendprepend-01
  answer: |
    `include` mixes a module's instance methods into instances of the class; the module sits directly below the class in the ancestor chain. `extend` adds the module's methods as singleton (class) methods of the class. `prepend` is like `include`, but the module sits above the class in the chain, so its methods override the class's own methods and `super` inside them reaches the class method.
- id: ruby-modules-ancestors-super-02
  answer: |
    `super` looks up the ancestor chain for the next method with the same name, starting after the current method's owner; bare `super` automatically forwards all received arguments, while `super()` forwards none. Included modules are links in that chain, so `super` can reach a method defined in an included module or in the superclass.
- id: ruby-modules-namespace-03
  answer: |
    A module also serves as a namespace, grouping related classes, modules, and constants to avoid name collisions (for example `JSON::Parser`). Define a module function with `def self.method` inside the module, or define a method and then `module_function :method`, making it callable as `Module.method` and private when the module is included.
- id: ruby-modules-refinements-04
  answer: |
    Refinements scope monkey-patches so a redefinition only applies inside files or classes that activate it, preventing global class pollution and collateral damage to unrelated code. They are defined with `Module#refine` inside a `Module.new` block and activated lexically with `using RefinementName`.
- id: ruby-objects-methodmissing-01
  answer: |
    `method_missing(name, *args, &block)` is invoked when an object receives a message it has no method for, allowing dynamic handling. You should also define `respond_to_missing?` so `respond_to?` reports those dynamically handled methods truthfully, calling `super` for names you do not handle.
- id: ruby-objects-send-02
  answer: |
    `send(:method, args)` invokes any method by name, including private and protected ones. `public_send` invokes only public methods; they differ when the target is private or protected, which `send` can call but `public_send` cannot.
- id: ruby-objects-attr-03
  answer: |
    `attr_reader` generates a getter, `attr_writer` a setter, and `attr_accessor` both, for the named instance variables. `define_method(name) { ... }` defines a method dynamically at runtime from a block, useful when method names are not known statically or when `attr_*` is too rigid.
- id: ruby-objects-visibility-04
  answer: |
    `private` methods may only be called with an implicit receiver (self); any explicit receiver, even `self.method`, is forbidden. `protected` methods allow an explicit receiver only when the receiver is `self` or an instance of the same class (or subclass), so they can be called between instances of the same class.
- id: ruby-enumerable-include-01
  answer: |
    Implement `each` to yield elements one at a time, then `include Enumerable`; you get `map`, `select`, `reject`, `reduce`, `each_with_index`, `min`, `max`, `sort`, `grep`, `count`, `any?`, `all?`, `find`, and more.
- id: ruby-enumerable-reduce-02
  answer: |
    `reduce`/`inject` threads an accumulator through the collection, yielding [accumulator, element] each step and returning the final accumulator; you may supply an initial value or use the first element as the seed. `each_with_object` is a better fit when you mutate a single object (for example, build a hash) and want to return that object rather than reassign an accumulator every iteration.
- id: ruby-enumerable-lazy-03
  answer: |
    `.lazy` returns a lazy enumerator: subsequent operations are chained and performed element-by-element on demand instead of eagerly building intermediate arrays. It matters for large or infinite sequences because it avoids materializing huge collections and lets you take a finite prefix from an infinite chain.
- id: ruby-enumerable-comparable-04
  answer: |
    `include Comparable` and define the `<=>` (spaceship) method returning -1, 0, 1, or nil. That single comparison then enables `<`, `<=`, `==`, `>`, `>=`, `between?`, `clamp`, and sorting via `sort`.
- id: ruby-metaprogramming-singleton-01
  answer: |
    A singleton class (eigenclass) is a hidden per-object class holding methods defined only on that particular object; it is consulted first in method lookup. `class << self` opens the singleton class of the current object — inside a class body, that is the class object itself — so you can define class methods there.
- id: ruby-metaprogramming-ivar-02
  answer: |
    `instance_variable_get(:@x)` reads an instance variable directly and `instance_variable_set(:@x, v)` writes one, even without accessors. They are appropriate for metaprogramming tools, debuggers, serializers, or generic wrappers, not for ordinary code where accessors are clearer.
- id: ruby-metaprogramming-definemethod-vs-mm-03
  answer: |
    `define_method` creates a real method on the class: it honors visibility, answers `respond_to?`, works with `super`, and is faster and easier to debug, but the names must be known when defined. `method_missing` handles arbitrary names lazily but is slower, defeats `respond_to?` unless paired with `respond_to_missing?`, and produces poorer stack traces.
- id: ruby-metaprogramming-classnew-04
  answer: |
    `Class.new` creates a new anonymous class, optionally with a given superclass. Assigning it to a constant gives the class a name (Ruby sets the constant's name as the class's name) and makes it referenceable like any other constant.
- id: ruby-error-standarderror-01
  answer: |
    `Exception` is the root and includes `Interrupt`, `SignalException`, `SystemExit`, `ScriptError`, and memory errors — not ordinary failures. Rescuing it swallows Ctrl-C, exits, and interrupts, preventing clean shutdown and masking fatal conditions. Rescue `StandardError` (the default of a bare `rescue`) instead.
- id: ruby-error-ensure-retry-02
  answer: |
    `ensure` always runs when the block exits — normal return, exception, break, or retry — no matter how control leaves. `retry` restarts the whole `begin` block from the top, usually from inside `rescue` after repairing state; use it carefully to avoid infinite loops.
- id: ruby-error-custom-03
  answer: |
    Define `class MyError < StandardError; end` (inherit from `StandardError`, not `Exception`). Raise with `raise MyError`, `raise MyError.new("msg")`, or plain `raise "msg"` (RuntimeError). `raise` forms include bare `raise` to re-raise the current exception, raising a string as RuntimeError, or raising a class or instance.
- id: ruby-error-elserescue-04
  answer: |
    A `rescue` at method level implicitly wraps the rest of the method in a begin. The optional `else` clause runs only when no exception was raised in the body (after any `rescue` is skipped), and it runs before `ensure`.
- id: ruby-strings-symbols-01
  answer: |
    A Symbol is an immutable, unique identifier — the same symbol is always the same object (same object_id). Strings are mutable sequences, and each literal is generally a distinct object. Use symbols as keys, method names, and identifiers; use strings for textual content you may mutate.
- id: ruby-strings-frozen-02
  answer: |
    The magic comment freezes all string literals in the file, so mutating one raises FrozenError and literals may be deduplicated. Subtlety: interpolated strings (`"#{x}"`) are still constructed at runtime, allocating a new object on each evaluation, so they do not share the identity or stability of static literals; mutating any frozen string still raises.
- id: ruby-strings-quotes-03
  answer: |
    Single-quoted strings recognize only `\'` and `\\` escapes. Double-quoted strings process full escape sequences (`\n`, `\t`, `\\`, etc.) and support interpolation. String interpolation embeds Ruby code in `#{...}`, evaluates it, and inserts its `to_s` result into the string.
- id: ruby-strings-percent-04
  answer: |
    `%w[...]` produces an Array of Strings split on whitespace; `%i[...]` produces an Array of Symbols (since Ruby 2.0).
- id: ruby-collections-hashdefault-01
  answer: |
    `Hash.new(0)` returns one shared default object (0) for every missing key and never stores it — dangerous with mutable defaults like `Hash.new([])`, where all missing keys share the same array. The block form `Hash.new { |h, k| h[k] = [] }` computes and stores an independent default per key.
- id: ruby-collections-kwargs-02
  answer: |
    Ruby 3 fully separated keyword arguments from positional arguments and from a trailing Hash; a Hash literal is no longer silently treated as keywords, and keywords are not passed as a Hash. To pass an existing Hash as keywords, splat it: `meth(**hash)`. Methods receive keywords via `kw:` and `**kwrest` parameters.
- id: ruby-collections-splat-03
  answer: |
    In definitions, `*args` collects extra positional arguments into an Array and `**kwargs` collects keyword arguments into a Hash. In calls, `*array` splats an Array into positional arguments and `**hash` splats a Hash into keyword arguments; `**` can also merge hashes in literals, like `{**a, **b}`.
- id: ruby-collections-safenav-data-04
  answer: |
    `&.` short-circuits: if the receiver is nil, the call returns nil without invoking the method; otherwise it calls normally. `Data.define` (Ruby 3.2) defines an immutable value class: keyword-initialized, read-only attributes, value-based `==`/`eql?`/`hash`, and no setters — a lighter alternative to Struct.
