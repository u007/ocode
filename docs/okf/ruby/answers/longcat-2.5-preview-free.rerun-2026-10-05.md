- id: ruby-blocks-procvslambda-01
  answer: |
    Two behavioral differences:
    1. Arity (argument checking): a lambda enforces its parameter list — calling it
       with the wrong number of arguments raises ArgumentError. A proc is lenient:
       missing arguments become nil, extra arguments are ignored (and, for a proc,
       extra args can be collected if it has a splat). `lambda { |a, b| }.arity` is 2,
       `proc { |a, b| }.arity` is also 2, but the strictness of the call differs.
    2. `return` semantics: `return` inside a lambda returns from the lambda itself
       (like a method). `return` inside a proc returns from the enclosing method that
       defined the proc; if that method has already returned, you get a LocalJumpError.
    Related: `lambda?` returns true/false; a lambda is a Proc whose `lambda?` is true.

- id: ruby-blocks-yield-02
  answer: |
    `yield` invokes the block that was passed to the enclosing method, transferring
    control to the block and returning the block's last expression as the value of
    `yield`. You can pass arguments: `yield(x)`. The block is not a named object
    unless you capture it with `&block`.
    `block_given?` returns true if a block was supplied to the current method. It
    protects against `LocalJumpError`, which is raised when you `yield` (or call the
    captured block) in a method that was called without a block. So the idiom is:
    `yield if block_given?` or make the block optional.

- id: ruby-blocks-ampblock-03
  answer: |
    In a definition, `def m(&block)` captures the passed block as a Proc and binds it
    to the local variable `block` (instead of just being implicitly yielded to). It
    lets you store it, pass it on, or call it with `block.call`.
    In a call, `arr.map(&:to_s)` converts the argument to a block: `&` calls
    `to_proc` on the object. A Symbol's `to_proc` produces a proc that calls that
    method on each yielded element (`&:to_s` => `{ |x| x.to_s }`). Likewise
    `&some_proc` converts a Proc into the block for the call.

- id: ruby-blocks-create-04
  answer: |
    Creating:
      l1 = lambda { |x| x + 1 }
      l2 = ->(x) { x + 1 }        # stabby lambda, preferred
      l3 = ->(x) { x + 1 }
      p1 = proc { |x| x + 1 }
      p2 = Proc.new { |x| x + 1 }
    Invoking a Proc/lambda (all equivalent):
      l1.call(1)
      l1.(1)
      l1[1]
      l1 === 1     # case-equality form (used by case/when)
    Note: `Proc.new` without a block inside a method that received a block captures
    that block (deprecated/removed in Ruby 3). `lambda`/`->` produce strict lambdas.

- id: ruby-modules-includeextendprepend-01
  answer: |
    `include M` — mixes M's instance methods into the class as instance methods. M is
    inserted in the ancestor chain just above the class (so class's own methods win).
    `extend M` — adds M's methods as singleton methods of the receiver. On a class
    object this makes them class methods; on any instance it makes them methods only
    that object responds to.
    `prepend M` — like include but inserts M *before* the class in the ancestor chain,
    so M's methods override the class's own methods of the same name (the class's
    version is still reachable via `super`).
    Ancestors example for `class C; prepend P; include I; end`:
    `[P, C, I, Object, Kernel, BasicObject]` (prepended first, then class, then
    included modules most-recent-first).

- id: ruby-modules-ancestors-super-02
  answer: |
    `super` calls the method of the same name found further up the ancestor chain,
    starting from the next entry after the class/module in which the currently
    executing method is defined. Ruby's method lookup walks the ancestors list from
    the receiver's class upward; included modules appear between the class and its
    superclass (last included is nearest the class). `prepend` puts the module before
    the class.
    `super` (bare) forwards the current method's arguments; `super()` forwards none;
    `super(args)` forwards the given ones. `super` in a module method reaches the
    class/superclass method that the module was mixed into or overrides.

- id: ruby-modules-namespace-03
  answer: |
    The other primary use of a module is as a namespace: grouping related constants,
    classes, and methods under a name to avoid collisions, e.g.
    `module Aims; class Report; end; end` referenced as `Aims::Report`.
    A callable module function is defined with `def self.name` (a singleton method on
    the module), or with `module_function`, which makes the named methods callable as
    `Mod.name` and also makes the instance-method copies private. Example:
      module Math2
        def self.double(x) = x * 2
      end
      Math2.double(3)  # => 6

- id: ruby-modules-refinements-04
  answer: |
    Refinements solve the problem that reopening a class (monkey-patching) changes
    behavior globally for the whole process and can break other code or collide with
    other libraries. A refinement redefines methods only within a lexical scope.
    Define: `module M; refine String do; def shout = upcase + "!"; end; end; end`.
    Activate: `using M` — at top level or inside a module, and the refinement is
    active only in that file/lexical scope (and for `using` in a module, in that
    module's scope). Outside that scope the original method is unchanged. `using`
    cannot be called inside a method body.

- id: ruby-objects-methodmissing-01
  answer: |
    `method_missing(name, *args, &block)` is a private method that Ruby calls when a
    message is sent to an object and no method of that name is found. Overriding it
    lets you handle arbitrary/dynamic method names at runtime (build proxies, DSLs,
    delegate calls, etc.). It is the last resort in the lookup chain (it is itself a
    method of BasicObject).
    You should also define `respond_to_missing?(name, include_private = false)` so
    that `respond_to?` (and things built on it, like `method`, duck-typing checks,
    and many libraries/frameworks) report the dynamic methods correctly. Otherwise
    the object claims not to respond to methods it actually handles, breaking
    introspection and conditional dispatch. Always call `super` for unhandled names.

- id: ruby-objects-send-02
  answer: |
    `send(name, *args)` invokes the method named `name` on the receiver at runtime,
    bypassing the normal dot syntax; it can call private and protected methods.
    `public_send(name, *args)` does the same but respects visibility — it raises
    NoMethodError if the method is private.
    They differ only when the target method is private/protected: `send` works,
    `public_send` raises. Use `public_send` when you want to honor encapsulation;
    `send` is the metaprogramming escape hatch (and is also handy in tests to reach
    private methods). Symbol or string names are accepted.

- id: ruby-objects-attr-03
  answer: |
    `attr_reader :x` generates a getter method `x` returning `@x`.
    `attr_writer :x` generates a setter `x=(value)` assigning `@x`.
    `attr_accessor :x` generates both getter and setter.
    They are shorthand for the common pattern of exposing instance variables; the
    methods are created at class-definition time.
    `define_method(:name) { |args| ... }` defines a method dynamically from a block
    at runtime, with the block's parameters becoming the method's parameters and the
    block's closure available. It is used in metaprogramming to generate methods
    (often in a loop) where the method name is computed, and it produces a real
    method (unlike method_missing).

- id: ruby-objects-visibility-04
  answer: |
    `private` methods cannot be called with an explicit receiver — only with an
    implicit receiver (`self` is implied), i.e. `foo`, not `obj.foo` or even
    `self.foo`. (Exception: private attribute writer `self.x = 1` is allowed, since
    Ruby 2.7, to disambiguate from local-variable assignment.)
    `protected` methods may be called with an explicit receiver, but only when the
    caller (`self`) is an instance of the same class or a subclass/related class —
    i.e. from within the class hierarchy, typically for comparing/operating on
    another instance of the same type. Neither is callable from outside the class;
    the difference is the explicit-receiver rule inside the class.

- id: ruby-enumerable-include-01
  answer: |
    You must define an `each` method that yields successive elements of the
    collection (and returns the collection, conventionally). Including
    `Enumerable` builds the rest of the protocol on top of `each`.
    For that you get a large set of methods: `map`, `select`/`filter`, `reject`,
    `find`/`detect`, `reduce`/`inject`, `sort`/`sort_by`, `min`, `max`, `min_by`,
    `sum`, `count`, `include?`/`member?`, `first`, `take`, `drop`, `each_with_index`,
    `each_with_object`, `group_by`, `partition`, `zip`, `all?`, `any?`, `none?`,
    `each_slice`, `each_cons`, `tally`, `lazy`, and more.

- id: ruby-enumerable-reduce-02
  answer: |
    `reduce`/`inject` folds a collection into a single value. Forms:
      arr.reduce(init) { |memo, el| ... }   # explicit initial value
      arr.reduce { |memo, el| ... }         # uses first element as initial memo
      arr.reduce(:+)                        # symbol form: apply + cumulatively
    The block's return value becomes the new memo each iteration; the final memo is
    returned. `inject` is an alias.
    `each_with_object(obj)` is a better fit when you are building a single mutable
    object (Hash, Array, etc.) and want the *same* object passed through each
    iteration; the block's return value is ignored and the original `obj` is
    returned. It avoids the common reduce bug of forgetting to return the memo and
    reads more clearly for building hashes/arrays:
      arr.each_with_object({}) { |el, h| h[el] = el.to_s }  # => a Hash
    Use reduce when combining into a scalar; use each_with_object when accumulating
    into a container.

- id: ruby-enumerable-lazy-03
  answer: |
    `.lazy` returns an `Enumerator::Lazy` that builds the chain of operations without
    executing any of them until the result is actually consumed (e.g. by `first`,
    `take`, `each`, `force`, or converting with `to_a`). Once forced, it processes
    the source one element at a time, threading each element through the whole chain
    before pulling the next.
    This matters because (a) it avoids allocating large intermediate arrays for each
    step of a long `map`/`select`/`take` chain, and (b) it makes infinite or very
    large sequences usable — you can do
    `(1..Float::INFINITY).lazy.map { |x| x * 2 }.select(&:even?).first(5)`
    and only as many elements as needed are computed. Without `.lazy`, a method like
    `select` on an infinite range would never terminate and `map` would build huge
    arrays.

- id: ruby-enumerable-comparable-04
  answer: |
    Include the `Comparable` module and define the `<=>` (spaceship) method, which
    should return -1, 0, or 1 (or nil if incomparable):
      class Version
        include Comparable
        attr_reader :major
        def initialize(m) = @major = m
        def <=>(other) = major <=> other.major
      end
    `Comparable` then supplies `<`, `<=`, `==`, `>=`, `>`, `between?`, and `clamp`
    based on `<=>`. So you only implement one method to get the whole ordering
    protocol. (`==` from Comparable is based on `<=>` returning 0.)

- id: ruby-metaprogramming-singleton-01
  answer: |
    A singleton class (also called eigenclass or metaclass) is a hidden per-object
    class that holds methods defined only for that one object. Every object has one;
    methods defined on it are "singleton methods". For a class object, its singleton
    class holds the class methods.
    `class << self` inside a class body opens the class's singleton class, so methods
    defined inside become class methods:
      class Foo
        class << self
          def bar = "class method"
        end
      end
    It also lets you use `attr_accessor`, `alias`, `private`, etc. in the class-method
    context, which `def self.x` cannot do as directly. `self` inside `class << self`
    refers to the class object.

- id: ruby-metaprogramming-ivar-02
  answer: |
    `instance_variable_get(:@x)` reads the value of the instance variable named `@x`
    on the receiver, even if no accessor exists. `instance_variable_set(:@x, v)`
    assigns it. Both bypass normal encapsulation and generate/warn about undefined
    ivars (get returns nil and may warn).
    They are appropriate in metaprogramming (generic serialization, cloning, building
    objects from data, test helpers, framework internals) where the variable name is
    only known at runtime. Outside such cases they break encapsulation and are
    discouraged; prefer accessors or explicit methods.

- id: ruby-metaprogramming-definemethod-vs-mm-03
  answer: |
    `define_method` creates a real method at definition time. Pros: fast dispatch
    (normal method call), appears in `instance_methods`/`method`, `respond_to?` works
    automatically, arity is enforced, errors are normal NoMethodErrors. Cons: you
    must know (or enumerate) the method names when defining; methods are fixed after
    definition; defining inside a loop needs care to capture the loop variable
    correctly.
    `method_missing` handles calls to names you did not predefine, at call time. Pros:
    truly open-ended, can respond to arbitrary/unknown names (proxies, dynamic DSLs),
    no upfront enumeration. Cons: slower, requires `respond_to_missing?` for correct
    introspection, harder to document/discover, can swallow typos and mask real
    NoMethodErrors, and interacts awkwardly with `super` and method introspection.
    Rule of thumb: prefer `define_method` when the set of methods is knowable; use
    `method_missing` only for genuinely dynamic, unbounded names.

- id: ruby-metaprogramming-classnew-04
  answer: |
    `Class.new` creates a new anonymous class at runtime. You can pass a superclass
    (`Class.new(Super)`), and give it a block that is `class_eval`'d in the new class
    (so you can define methods, include modules, etc.):
      Foo = Class.new do
        def hello = "hi"
      end
    Assigning it to a constant matters because the first constant assignment gives
    the anonymous class a name: `Foo.name` becomes "Foo", and `inspect`/`to_s` use
    it. Before that it is anonymous (`name` is nil). Named constants also make it
    referenceable and are how Ruby resolves nested constant lookup. Assigning the
    same class to a second constant keeps the original name.

- id: ruby-error-standarderror-01
  answer: |
    A bare `rescue` or `rescue => e` rescues `StandardError` (the default). `Exception`
    is the root of the hierarchy and includes things that are not ordinary
    application errors: `SystemExit` (from `exit`/`exit!`), `Interrupt` (Ctrl-C),
    `SignalException`, `NoMemoryError`, `SystemStackError`, and `ScriptError`/
    `SyntaxError`/`LoadError`. Rescuing `Exception` would swallow those, so your
    program would ignore Ctrl-C, fail to exit when asked, and hide fatal or
    programmer errors — often turning a crash into a silent hang or corrupt state.
    The correct approach is to rescue `StandardError` (or a specific subclass) and let
    system-level exceptions propagate. Custom application errors should subclass
    `StandardError` for the same reason.

- id: ruby-error-ensure-retry-02
  answer: |
    `ensure` runs when control leaves the `begin`/`end` block, no matter how: normal
    completion, an exception being raised (whether rescued or not), or a `return`/
    `break`/`next`/`throw` from within. It runs after the `rescue` (or after the body
    if no exception) and after `else`. If an exception is propagating, `ensure` runs
    before it continues outward. It is used for cleanup (closing files, releasing
    locks). Note: `ensure` runs even if the method returns, and its own return value
    is discarded; avoid `return` inside `ensure` as it overrides the real return.
    `retry` (inside a `rescue`) re-executes the `begin` body from the start, which is
    used for transient failures (e.g. retrying a network call). It must be bounded or
    guarded, otherwise a persistent error causes an infinite loop.

- id: ruby-error-custom-03
  answer: |
    Define by subclassing StandardError (or a more specific error), conventionally
    with a message default:
      class MyError < StandardError; end
      class MyError < StandardError
        def initialize(msg = "default message") = super
      end
    Raise forms:
      raise                      # re-raise the current exception ($!)
      raise "message"            # RuntimeError with that message
      raise MyError              # MyError with its default message
      raise MyError, "message"   # MyError.new("message")
      raise MyError.new("msg")   # explicit instance
    Subclass StandardError (not Exception) so a bare `rescue` catches it. Group
    related errors under a common base (e.g. `class AppError < StandardError`).

- id: ruby-error-elserescue-04
  answer: |
    A method body (and `do...end`/`{}` blocks) can contain `rescue`/`else`/`ensure`
    directly without an explicit `begin`, because the method body is an implicit
    begin block.
    The `else` clause runs only if the body completed with **no exception** — it is
    the "success" branch, executed after the body and before any `ensure`. It is
    useful to separate the happy-path code from the rescue handling, so that an
    exception raised *inside a rescue clause* is not accidentally caught by another
    rescue, and to make clear which code runs only on success. Example:
      def f
        risky
      rescue SomeError
        handle
      else
        log_success   # only when no exception
      ensure
        cleanup
      end

- id: ruby-strings-symbols-01
  answer: |
    A Symbol is an immutable, interned identifier: the same symbol literal always
    refers to the same object (`:foo.equal?(:foo)` is true), so symbols are cheap to
    compare (identity) and are not garbage-collected in the same way (modern Ruby
    does GC them, but they are still interned). Symbols are typically used for names,
    identifiers, hash keys, and method names.
    A String is a sequence of characters that is mutable by default (unless frozen):
    two strings with the same content are different objects (`"foo".equal?("foo")`
    is false), and methods like `<<`, `upcase!`, `gsub!` mutate in place. Strings are
    used for textual data, user input, output, etc. Rule of thumb: use a Symbol for a
    fixed label/name, a String for text you manipulate or display.

- id: ruby-strings-frozen-02
  answer: |
    `# frozen_string_literal: true` (a magic comment at the top of a file) makes every
    string literal in that file frozen — attempting to mutate one (e.g. `s << "x"`)
    raises FrozenError. It saves allocations (one shared frozen object per literal
    occurrence per file) and guards against accidental mutation; it is the default
    for string literals from Ruby 3.4 onward (with a warning in 3.3).
    Subtlety: string **interpolation** produces a new, mutable string even with the
    magic comment — `"a#{b}"` is not frozen. Only plain literals are frozen. Also
    frozen literals cannot be mutated, so code that used `<<` to build strings must
    switch to `+`/`<<` on a dup or use `String.new`. `+"foo"` (unary plus) makes an
    unfrozen copy; `.freeze` freezes explicitly.

- id: ruby-strings-quotes-03
  answer: |
    Single-quoted strings are literal: only `\\` (backslash) and `\'` (escaped quote)
    are special; no interpolation and no other escape sequences (`'\n'` is backslash
    + n, two characters).
    Double-quoted strings support escape sequences (`\n`, `\t`, `\0`, `\uXXXX`, etc.)
    and interpolation: `#{expression}` evaluates the expression and converts the
    result to a string (via `to_s`) and embeds it. Interpolation is Ruby code, not
    just variable names: `"sum: #{1 + 2}"`. Equivalent construction is
    `"a" + b.to_s` or `format`. (There are also `%q`/`%Q` for single/double-quote-like
    behavior.)

- id: ruby-strings-percent-04
  answer: |
    `%w[...]` produces an array of Strings, splitting on whitespace:
      %w[foo bar baz]  # => ["foo", "bar", "baz"]
    `%i[...]` produces an array of Symbols, splitting on whitespace:
      %i[foo bar baz]  # => [:foo, :bar, :baz]
    Both are literal constructors — no interpolation unless you use the uppercase
    variants `%W[...]`/`%I[...]`, which do interpolate. Delimiters can be brackets,
    parens, braces, or any non-alphanumeric character (matched pairs for brackets).

- id: ruby-collections-hashdefault-01
  answer: |
    `Hash.new(0)` sets a **default value object** returned when a key is missing. It
    does not store the key: `h = Hash.new(0); h[:a] += 1` works because `h[:a]`
    returns 0, then `h[:a] = 0 + 1` stores 1. But the same object is shared: with
    `Hash.new([])`, every missing key returns the *same* array, so
    `h[:a] << 1` mutates the shared default and `h` still has no `:a` key — a classic
    bug.
    `Hash.new { |h, k| h[k] = [] }` uses a **default block** that runs per missing key;
    the example assigns and returns a fresh array each time, so each key gets its own
    array and the key is actually stored. The difference matters for mutable defaults
    (arrays, hashes, strings): the value form shares one object and doesn't store the
    key; the block form can create per-key values and store them. Use the block when
    the default is mutable or must be recorded.

- id: ruby-collections-kwargs-02
  answer: |
    Before Ruby 3.0, a trailing Hash passed to a method was automatically converted to
    keyword arguments and vice versa, which caused ambiguity and bugs (e.g. a hash
    meant as a positional argument being consumed as keywords).
    Ruby 3.0 separated them: a method with keyword parameters no longer automatically
    receives a trailing Hash as keywords, and a method with a trailing positional
    Hash parameter no longer captures keywords. To pass a hash as keywords you must
    splat it with `**`:
      def foo(a:, b:) = a + b
      opts = { a: 1, b: 2 }
      foo(**opts)          # OK — expands to keywords
      foo(opts)            # ArgumentError (positional hash, no kwargs) or positional
    To capture arbitrary keywords: `def foo(**kw)`. To forward: `def foo(...)` or
    `bar(**kw)`. `**nil` in a signature means "accepts no keywords". Also, in Ruby 3
    keyword arguments are not a positional Hash, so `Hash#` methods and `**` are the
    bridge.

- id: ruby-collections-splat-03
  answer: |
    In a **method definition**:
      `*args` collects extra positional arguments into an Array.
      `**opts` collects keyword arguments into a Hash.
      A bare `*` or `**` (or `**nil`) can be used to ignore/forward them.
    In a **method call**:
      `*array` expands an Array into separate positional arguments:
        `f(*[1, 2, 3])` == `f(1, 2, 3)`.
      `**hash` expands a Hash into keyword arguments:
        `f(**{a: 1})` == `f(a: 1)`.
    They can be combined with other parameters (`def f(a, *rest, k:, **opts)`), and
    the splat in a call also works with any object responding to `to_a`/`to_ary`.
    Since Ruby 3.0, `*` and `**` do not cross-convert (a splatted array is positional;
    a double-splatted hash is keywords).

- id: ruby-collections-safenav-data-04
  answer: |
    The safe-navigation operator `&.` calls a method only if the receiver is not nil;
    if the receiver is nil it short-circuits and returns nil instead of raising
    NoMethodError. Example: `user&.address&.city` returns nil if any link is nil.
    It only guards against nil (not against missing methods on non-nil objects), and
    it can be chained. `&.` also short-circuits the rest of the expression/arguments
    when nil.
    `Data.define` (Ruby 3.2) defines an immutable value-object class (similar to
    `Struct` but immutable and value-semantic):
      Point = Data.define(:x, :y)
      p = Point.new(x: 1, y: 2)   # or Point.new(1, 2)
      p.x          # => 1
      p.with(x: 5) # => new Point(5, 2) — returns a copy
    Instances are frozen, support equality by value and `deconstruct`/pattern
    matching, and have no setters. It is the modern way to make small immutable data
    types (replacing Struct where mutation is unwanted).
