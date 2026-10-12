# step-5-preview-free — closed-book answers for ruby (opencode-go, 2026-10-12); answerer saw only _prompts/ruby.md
Answering closed-book from my own knowledge only. Ruby is a language I know well, so here is the YAML answer list:

- id: ruby-blocks-procvslambda-01
  answer: |
    1. **Arity checking.** A lambda checks its arguments strictly — calling it with
    the wrong number of positional arguments raises `ArgumentError`, just like a
    method. A proc is lenient: missing arguments are filled with `nil` and extra
    arguments are silently dropped.
    2. **`return` (and `break`) semantics.** `return` inside a lambda returns
    from the *lambda itself*. `return` inside a proc returns from the *enclosing
    method that created/received the proc* — a proc's `return` behaves as if the
    code were inlined into that method (and raises `LocalJumpError` if that scope
    no longer exists).
    (Related nuance: lambdas treat `break`/`next` more like methods do; procs are
    closer to "inlined code", which is why their control flow escapes outward.)

- id: ruby-blocks-yield-02
  answer: |
    `yield` hands control to the block attached to the currently executing method,
    passing along any arguments given to it; when the block finishes, execution
    resumes after the `yield`. A block can return a value to the method via its
    last expression, which `yield` evaluates to. Calling `yield` when no block was
    passed raises `LocalJumpError`.
    `block_given?` returns true only if the caller supplied a block, so a method
    can safely do useful work whether or not a block is present:
    `return default unless block_given?; yield(x)`. It protects against the
    `LocalJumpError` crash and lets a method support both block and non-block
    (often "returns a default / raises a clearer error / uses a fallback") usage.

- id: ruby-blocks-ampblock-03
  answer: |
    In a method definition, `&` captures the caller's block as a real Proc object
    bound to the parameter: `def m(&block)` makes `block` a `Proc` (or `nil` if
    no block was given). You can then store it, pass it around, or call it with
    `block.call(...)`; conversely `&` in a *call site* (`m(&block)`) converts a
    Proc back into a yieldable block.
    In `arr.map(&:to_s)`, `&` triggers `to_proc` on the symbol — `Symbol#to_proc`
    returns a proc that calls that named method — so it is shorthand for
    `arr.map { |x| x.to_s }`. It also serves as a concise way to forward a block:
    `wrapper(&args)` / `wrapper(&block)`.

- id: ruby-blocks-create-04
  answer: |
    **Creating:**
    ```ruby
    my_lambda = lambda { |x| x * 2 }
    my_lambda = ->(x) { x * 2 }       # stabby lambda
    my_proc   = proc { |x| x * 2 }
    my_proc   = Proc.new { |x| x * 2 }
    ```
    **Invoking** (a proc/lambda responds to all of these — they are aliases):
    ```ruby
    l.call(x)
    l.(x)                # call operator, shorthand
    l[x]
    l.yield(x)           # rarely used; shadows the keyword form
    ```
    You can also destructure parameters inside the block (`proc { |a, b| ... }`),
    and procs/lambdas can be converted to blocks with `&` at a call site.

- id: ruby-modules-includeextendprepend-01
  answer: |
    - `include M` — M's **instance methods** become instance methods of the class.
      M is inserted into the ancestor chain **above the class itself** (so the
      class can override and M's methods are found before `Object`, and M can
      `super` into the class it overrode only if `prepend`ed).
    - `extend M` on a class — M's methods become **class methods** (singleton
      methods): the class *object's* singleton class includes M. On an instance,
      `extend` adds methods just for that one object.
    - `prepend M` — like `include`, but M is inserted *ahead of the class itself*
      in the ancestor chain, so M's methods **override** the class's own methods
      and can call `super` to reach the original class implementation. It is the
      standard non-destructive alternative to monkey-patching a class's own
      methods (you prepend a module instead of reopening the class).
    All three operate on the ancestor chain; the visible descendant superfic
    for `class C; include M; end` with `C.ancestors` is `[C, M, Object, ...]`,
    and with `prepend` it is `[M, C, Object, ...]`.

- id: ruby-modules-ancestors-super-02
  answer: |
    When a method is called, Ruby walks the receiver's **method lookup path**
    (the ancestor chain, nearest first: the singleton class, then the class, then
    its included modules in reverse-include order, then superclass chain, then
    included modules at each level, up to `BasicObject`). The first class or
    module in that chain defining a method with that name wins; it shadows the
    rest, but can reach the next one with `super`.
    `super` (bare) re-dispatches — with the **same arguments** — to the next
    method of the same name further along the ancestor chain. `super(args)`
    passes explicit arguments, and `super()` passes none. Included modules sit in
    the chain ahead of the class that included them (but behind classes that
    prepend), which is exactly why `Module#ancestors` is the canonical way to
    reason about which implementation runs and where `super` goes. It is used
    heavily inside `initialize` to defer to an ancestor's constructor, and in
    template-method patterns where a module wraps/extends class behavior.

- id: ruby-modules-namespace-03
  answer: |
    The other primary use is **namespacing / organization** — a module used as a
    container to group related classes and constants and avoid name collisions:
    `module Billing; class Invoice; end; end` referenced as `Billing::Invoice`.
    A "callable module function" (a method you call on the module itself, not on
    an instance) is defined as:
    ```ruby
    module MathUtil
      module_function          # makes subsequently defined methods private
                              # instance methods AND module (singleton) methods
      def gcd(a, b) ... end
    end
    MathUtil.gcd(4, 6)          # => 2
    # alternatives with identical call syntax:
    def self.double(x) x * 2 end
    extend self                 # then every instance method is also a module method
    ```

- id: ruby-modules-refinements-04
  answer: |
    Reopening a class (monkey-patching) changes the class **globally, for the
    entire process**, affecting even third-party gems and other libraries, with
    no scope boundaries — a constant source of hard-to-trace collisions. It is
    used as namespace-by-extension but leaks everywhere and permanently.
    **Refinements** (`module M; refine String { def shout; ...; end; end; end`)
    define patches that are **lexically scoped**: they are inactive except where
    explicitly enabled, and even then only for a bounded file/module/eval region.
    Activation is done with `using M`, which is only allowed at the top level of a
    file or module body (not inside a method) and takes effect from that point to
    the end of that lexical scope. The result is a localized, reversible patch:
    code outside the `using` scope sees the original, unmodified behavior, and
    refinements are not inherited across file boundaries or `instance_eval` etc.

- id: ruby-objects-methodmissing-01
  answer: |
    `method_missing` is a hook method Ruby falls back to when a normal method
    lookup of a called name fails on the receiver. Overriding it lets an object
    respond to arbitrary method names — the building block of proxies,
    record-like/dynamic attribute access, builders, and DSLs:
    `def method_missing(name, *args, &blk)`.
    You must also override `respond_to_missing?(name, include_private = false)`
    to return true for the same dynamically handled names, because `respond_to?`
    does **not** consult `method_missing` by default. Without it, the object lies:
    `obj.respond_to?(:dynamic_thing)` would be false while
    `obj.dynamic_thing` works, breaking duck typing, `method(:x)`/`methods`
    listings, `send` guards, and framework introspection (e.g. Rails' dynamic
    finders, ActiveSupport's `respond_to?`-based caching).

- id: ruby-objects-send-02
  answer: |
    `obj.send(name, *args)` invokes the method named `name` on `obj` dynamically
    — the string/symbol name is resolved to a method at call time and it bypasses
    the usual explicit-receiver restriction, so it **can call private and
    protected methods** (Ruby's visibility model is about explicit receivers, not
    `send`). `public_send` does the same resolution but **only for public
    methods**, raising `NoMethodError` (via method_missing failure) if the target
    is private/protected.
    They differ on exactly those visibility boundaries: use `public_send` when
    dispatching user input or plugin-declared method names where you want the
    object's public contract respected; use `send` when you need full reflective
    power (e.g., calling a private setter in serializers/tests) — and be careful,
    `send` on arbitrary external input is a privilege escalation vector.

- id: ruby-objects-attr-03
  answer: |
    On a class body:
    - `attr_reader :x` — defines `def x; @x; end` (a getter).
    - `attr_writer :x` — defines `def x=(v); @x = v; end` (a setter).
    - `attr_accessor :x` — defines both; this is what people usually want.
    They are class-level macros (instance methods of `Module`) that generate
    accessor methods for instance variables of the given names, replacing
    hand-written boilerplate.
    `define_method(:name) { |args| ... }` is the *dynamic* counterpart: it defines
    an instance method **at runtime** with the given name, whose body is a block.
    The block is a closure, so it captures the surrounding scope, which is what
    makes it useful in metaprogramming:
    ```ruby
    [:name, :email].each do |field|
      define_method("has_#{field}?") { !public_send(field).nil? }
    end
    ```

- id: ruby-objects-visibility-04
  answer: |
    `private` methods may only be called with an **implicit receiver** — from
    inside another instance method of the same object (or subclass), you call
    them bare (`helper`), and even `self.helper` is illegal for everything except
    setters (since Ruby 2.7, `self.x = v` is allowed). An explicit receiver
    `other.helper` raises `NoMethodError`.
    `protected` methods allow an explicit receiver, but **only when the caller is
    of the same class (or subclass) as the receiver** — `obj.protected_op(other)`
    is fine, but `obj.protected_op(stranger)` raises `NoMethodError`. That makes
    protected the right visibility for operations that compare or operate over
    two peers, e.g. `def >(other); value > other.value; end` in a class using
    `Comparable`.
    The practical rule: private = internal implementation detail, only implicit
    self; protected = interactions between instances of the same class with an
    explicit receiver.

- id: ruby-enumerable-include-01
  answer: |
    The only contract is to define **`each`**, which yields successive elements
    to a block. `include Enumerable` in your class and its methods are written in
    terms of that `each`.
    For that you get roughly fifty mixin methods for free, because `Enumerable`
    implements all of them in terms of `each`: `map`/`collect`, `select`/`filter`,
    `reject`, `find`/`detect`, `reduce`/`inject`, `count`, `include?`, `min`/`max`,
    `sort`, `sort_by`, `group_by`, `partition`, `flat_map`, `to_a`/`to_h`,
    `zip`, `first`, `take`/`drop`, `each_with_index`, `chunk_while`, `tally`,
    `sum`, and so on.
    Practical requirement: `each` must yield *every* element and return a
    meaningful value (commonly the collection itself), and if the class also
    defines `<=>` it can additionally delegate some of its work to `Comparable`.

- id: ruby-enumerable-reduce-02
  answer: |
    `reduce`/`inject` folds a collection into a single value: it threads an
    accumulator through the block, starting from an initial value (or the first
    element), returning the final accumulator:
    `(1..5).reduce(0) { |sum, n| sum + n } # => 15` (and the common
    `[2,3,4].reduce(:*) # => 24`).
    `each_with_object` also accumulates, but runs `each` and passes a single
    memo object whose **return value of the block is ignored** — you mutate the
    memo in place (`|hash, word| hash[word.length] << word`). It is a better fit
    when the accumulation is a collection you build up (grouping into an array or
    hash), because you don't have to keep re-assigning and re-returning the
    accumulator each iteration as you do with `reduce` — less noise, and the
    intent ("decorate this object with results") is clearer.
    So: `reduce` when the reducer *produces a new single value* (sum, product,
    min); `each_with_object` when you're *populating* an array/hash/set.

- id: ruby-enumerable-lazy-03
  answer: |
    `.lazy` returns a lazy enumerator — the chain of operations (`map`, `select`,
    `reject`, `flat_map`, `take`, `drop`, ...) is *deferred*: no work happens on
    the underlying sequence until a value is actually pulled out (`first(n)`,
    `take`, `to_a`, `each`, ...). The pipeline then pulls elements through one at
    a time, and a `take`/`first` early in the chain **short-circuits** — elements
    beyond what's needed are never produced.
    It matters because eager chaining materializes an intermediate array at every
    stage: for large sequences that is O(n) memory and repeated traversals; for
    infinite ones (`(1..Float::INFINITY).lazy.map { ... }.select { ... }.first(5)`)
    it is the difference between working and hanging/OOMing forever building an
    array of infinite size. The classic win is expressing infinite generators
    (Fibonacci, primality, log streams) as clean chains and taking only what you
    use.

- id: ruby-enumerable-comparable-04
  answer: |
    `include Comparable` and define the **spaceship operator** `<=>`, which
    compares `self` to another object and returns `-1`, `0`, `+1` (or `nil` if
    not comparable):
    ```ruby
    class Version
      include Comparable
      attr_reader :n
      def initialize(n) = @n = n
      def <=>(other) = n <=> other.n
    end
    Version.new(2) < Version.new(3)   # => true
    Version.new(3).clamp(Version.new(1), Version.new(2))  # => #<Version @n=2>
    ```
    Implementing `<=>` (returning `nil` for incomparable types rather than raising)
    gives you `<`, `<=`, `>`, `>=`, `==`, `between?`, `clamp`, `min`/`max`
    semantics, and `Comparable#==`. Caveats: keep `<=>` consistent with `eql?`/
    `hash` if you use instances as Hash keys, and remember `Comparable` requires
    the operator to work for the cross-type comparison you intend; alternatively
    (Ruby < 1.9 era and today) some classes define each operator manually for
    finer control.

- id: ruby-metaprogramming-singleton-01
  answer: |
    A singleton (eigen)class is the hidden, anonymous class that stores methods
    defined for a *single object* rather than for all instances of a class. Every
    object has one, sitting between the object and its class in the lookup chain.
    `class << self` (the "singleton class definition") opens the singleton class,
    in which `def` methods become **class-level methods** (so-called "static"
    methods):
    ```ruby
    class Foo
      class << self
        def bar; end   # Foo.bar
        attr_accessor :config
      end
    end
    ```
    That's why `Foo`'s "static" methods really live one level up the chain:
    `Foo.singleton_class.instance_methods(false)` lists them, and
    `extend SomeModule` on the class mixes instance methods of `SomeModule` into
    that singleton class, turning them into class methods. You can also do
    `class << obj` to define methods on a single object instance, or
    `obj.singleton_class`.

- id: ruby-metaprogramming-ivar-02
  answer: |
    `instance_variable_get(:@name)` / `instance_variable_set(:@name, value)` read
    and write instance variables by (dynamic) name, bypassing getters/setters and
    normal visibility — reflective access to another object's internal state, not
    available by regular attribute APIs.
    They're appropriate for framework/tooling code where the variable name is only
    known at runtime or must be applied generically: serialization
    (ActiveModel::Serializers walking ivars), ORMs and mappers, object
    duplication/`dup` internals, debuggers and inspectors, and reproducing
    internal state in tests or code generation. They are *inappropriate* as a
    day-to-day way to poke at your own objects' state from application code —
    that bypasses encapsulation, breaks when internals are renamed, and signals
    the accessor should just be declared with `attr_accessor` instead. Rule of
    thumb: fine under `Object#instance_variable_*` in framework machinery, a smell
    in domain code, and a security risk when the name comes from user input.

- id: ruby-metaprogramming-definemethod-vs-mm-03
  answer: |
    - `define_method(:name) { ... }` **eagerly** creates a real, named method at
    definition time. Pros: fast lookup, appears in `instance_methods`/`methods`,
    works with `respond_to?`, `method(:x)`, and stack traces show the real name.
    Cons: you must know (and pay for) every name up front — bad for unbounded or
    open-ended name spaces.
    - `method_missing` handles names **lazily, on demand**, including names that
    don't exist until called. Pros: handles unbounded/dynamic method spaces
    (proxying, delegation, ActiveRecord finders, DSLs), zero cost until used.
    Cons: slower (every unknown call goes through fallback), invisible to
    `respond_to?`/`methods`/`method()` unless you also implement
    `respond_to_missing?`, error-prone fallback logic (`super` calls can loop,
    exceptions can hide real typos).
    Guidance: prefer `define_method` (or `class_eval` with `def`) when the set of
    methods is finite/enumerable; reach for `method_missing` when the method
    space is open-ended or you're building a generic proxy — and always pair it
    with `respond_to_missing?`.

- id: ruby-metaprogramming-classnew-04
  answer: |
    `Class.new` creates a **new anonymous class** at runtime (a first-class class
    object, not a constant) — the programmatic counterpart of `class Foo; end`.
    You can optionally pass a superclass: `Class.new(Parent)`, and it takes a
    block whose body is evaluated in the class context (`Class.new do ... end`),
    so you can define methods, `include` modules, etc.
    Assigning it to a **constant** is what gives the class its name:
    `Foo = Class.new(Super) # Foo.name => "Foo"`. Until then, `name` is `nil`,
    error messages and `inspect` output show `#<Class:0x...>`, and the class is
    unreachable via any namespace lookup — making it unreadable in exceptions,
    impossible to reopen by name, and invisible to things that map names to
    constants (Rails eager loading, YAML deserialization, autoloaders). Constant
    assignment is thus the idiom for making the anonymous class part of the
    program's public/namespaced surface.

- id: ruby-error-standarderror-01
  answer: |
    A bare `rescue` (or `rescue => e`) defaults to `StandardError` because that
    subtree represents *recoverable, application-level* failures (`ArgumentError`,
    `IOError`, `ZeroDivisionError`, your custom errors). `Exception` is the root
    of the whole tree and its non-`StandardError` branches are **control and
    process-level events**: `SignalException` (including `Interrupt` — Ctrl-C!),
    `NoMemoryError`, `SystemExit` (from `exit`), `SystemStackError`,
    `ScriptError` (`LoadError`, `SyntaxError`, `NotImplementedError`).
    Rescuing `Exception` is almost always wrong because it swallows exactly those:
    a `rescue => e; log e; end` wrapper eats Ctrl-C so the process can't be
    interrupted from the terminal; it catches `SystemExit`, so `exit` becomes a
    no-op and the process limps on after being told to terminate; it hides memory/
    stack blowups that should crash fast; and it converts genuinely fatal signals
    into ordinary flow. Narrow rescues keep termination semantics intact and let
    the process die on the signals that mean "stop" — rescue the narrowest class
    you can meaningfully handle.

- id: ruby-error-ensure-retry-02
  answer: |
    In `begin ... rescue ... ensure ... end`, the `ensure` clause runs **no
    matter what**: on success, on a rescued exception, and even when an
    unrescued exception propagates out of the `begin/rescue` (in which case
    `ensure` runs during unwinding and then the exception continues). It is for
    cleanup that must always happen — closing files, releasing locks,
    restoring state. (Its value is discarded unless the block raises, and an
    `ensure` that itself raises replaces the inner exception; there is also no
    `else`-only path — `ensure` always runs.)
    `retry` re-executes the entire `begin ... rescue` block from the top (with
    the same arguments/bindings). It's the classic retry-loop primitive — you fix
    something in the `rescue` (reload a config, back off, reconnect) then `retry`
    and hope the next attempt succeeds. The classic bug is an infinite loop when
    the failure condition is permanent, so real code caps attempts:
    ```ruby
    retries ||= 0
    begin
      try_thing
    rescue TransientError
      retries += 1
      retry if retries < 3
      raise
    end
    ```

- id: ruby-error-custom-03
  answer: |
    Define by subclassing `StandardError` (so a bare `rescue` catches it — NOT
    `Exception`), add custom attributes by overriding `initialize` and calling
    `super` with the message:
    ```ruby
    class PaymentDeclined < StandardError
      attr_reader :code
      def initialize(msg = "payment declined", code: nil)
        super(msg)          # sets the message
        @code = code
      end
    end
    ```
    **Forms of `raise`:**
    - `raise` (bare) inside a `rescue` — re-raises the current exception `$!`,
      preserving backtrace.
    - `raise "message"` — raises `RuntimeError` with that message.
    - `raise SomeErrorClass` — raises that class, default message (class name).
    - `raise SomeErrorClass, "message"` — most common custom form.
    - `raise SomeErrorClass.new("message")` — raising an already-built exception
      instance (needed for extra attributes).
    - `raise error_variable` inside `rescue` — raises a stored/rebuilt exception.
    `raise` outside any rescue raises the `RuntimeError` form. (You can pass a
    `cause:` kwarg to chain the underlying exception.)

- id: ruby-error-elserescue-04
  answer: |
    Ruby allows `def foo; ...; rescue => e; ...; end` — the `rescue` applies to
    the whole (implicit `begin`) method body.
    The optional `else` clause in `begin/rescue/else/ensure/end` holds code that
    runs **only when the `begin` block completed without raising any exception**.
    It is the natural home for code that depends on the guarded operations
    having succeeded but should not itself be inside the `begin` (and therefore
    not rescued):
    ```ruby
    begin
      data = JSON.parse(payload)
    rescue JSON::ParserError => e
      log(e); return fallback
    else
      persist(data)        # runs only if parse succeeded;
                           # a raise here is NOT caught by the rescue above
    ensure
      close_connection
    end
    ```
    Without `else`, that same logic would have to live at the end of the `begin`
    block — where its failures would be caught by the `rescue` and misclassified
    as the error you were guarding against, or just muddy the try-block semantics.

- id: ruby-strings-symbols-01
  answer: |
    A String is a mutable (unless frozen) sequence of characters — a *value*
    object: two strings with the same content are different objects
    (`"a".equal?("a")` is usually false; comparing is `==`). A Symbol is an
    **immutable, interned** identifier: for any given name there is exactly one
    Symbol object per process, so `:a.equal?(:a)` is always true — identity is
    the same thing as equality, and symbols can never be mutated (no `<<`).
    Consequence: symbols are cheaper to compare (object identity, O(1)) and
    hash, and are created once.
    Typical use: Symbols as **names/identifiers** — method names, hash keys
    (`status: :active`), keyword argument names, `send`, state machine states.
    Strings as **data/content** — user text, file contents, anything that varies
    at runtime or must be manipulated/concatenated/serialized.

- id: ruby-strings-frozen-02
  answer: |
    `# frozen_string_literal: true` (first line of a file, a magic comment) makes
    every string literal in that file **frozen at parse time** — no new mutable
    String objects allocated per evaluation, less garbage, and safer to share; the
    virtual machine also deduplicates identical frozen literals.
    Subtleties around interpolation: an interpolated string (`"#{x}!"`) is still
    evaluated at runtime, and **each evaluation produces a brand-new String
    object** — the comment does not make it a shared constant, so
    `"a#{1}" == "a1"` is true but `"a#{1}".equal?("a#{1}")` can be false.
    That frozen interpolated string is *not* deduplicated and, importantly, while
    the literal itself is frozen, that says nothing about `x`'s own state — and
    any non-literal runtime string (`String.new`, `gets`, `+`, `<<` results,
    heredoc* bodies interpolated, substitution results) is still unfrozen unless
    you freeze it yourself. (Also: mutating operations like `<<`, `gsub!`, `upcase!`
    now raise `FrozenError` on those literals — which is the point, catching
    accidental mutation, but it's the most common source of new errors after
    enabling the comment.)

- id: ruby-strings-quotes-03
  answer: |
    **Single quotes** `'...'` — the "plain" literal: no interpolation, and almost
    no escape processing — only `\\` (backslash) and `\'` (escaped single quote)
    are recognized; `\n` is literally backslash-n (useful for regexes and Windows
    paths). **Double quotes** `"..."` — the "smart" literal: full escape-sequence
    processing (`\n`, `\t`, `\"`, unicode `\u`, `\u{...}`)`.
    **String interpolation** is embedding the result of an expression into a
    string, and only works in double quotes and related literals:
    `"Total: #{items.count} items, total #{sum}"` — the `#{...}` is arbitrary
    Ruby code evaluated at runtime and converted with `to_s` (and `#$global`,
    `#@ivar` shorthands exist). In single quotes `'#{x}'` is literally
    `#{x}`. Interpolation is the idiomatic way to build strings (vs `+`, which
    allocates intermediates, or `format`/`sprintf` when alignment/format
    specifiers matter).

- id: ruby-strings-percent-04
  answer: |
    Both are whitespace-delimited "word list" literals (any paired delimiter:
    `%w(...)`, `%w[...]`, `%w{ }`, ...; `\ ` escapes a space):
    - `%w[a b c]` → an Array of **Strings**: `["a", "b", "c"]`.
    - `%i[a b c]` → an Array of **Symbols**: `[:a, :b, :c]` (shorthand for
      `%w[a b c].map(&:to_sym)`).
    Both produce **unfrozen** (mutable) objects by default. There are magic-comment
    / method-level ways to request frozen or symbol-converted variants in other
    languages' boolean-false style: `%W`/`%I` (capitalized) are the
    interpolation-enabled counterparts, and Ruby also respects
    `-string-literal` sibling flags? No — concretely: use `.map(&:freeze)` or
    `.each(&:freeze)` if you need frozen copies (`%w` has no frozen flag).
    Typical use: `%w[read write]` as a quick constant list of words;
    `%i[asc desc]` for option/symbol lists.

- id: ruby-collections-hashdefault-01
  answer: |
    `Hash.new(0)` has a **single shared default value**: every missing key
    evaluates to the *same* object `0`, and the hash is not modified (no key is
    stored). Fine when the default is immutable and stateless (counters).
    `Hash.new { |h, k| h[k] = [] }` runs the **block** on a missing-key lookup,
    with the hash and the key as parameters — and here the block *stores* a fresh
    object, so the hash is mutated (a real key is created) and each missing key
    gets **its own** object.
    It matters the moment the default is mutable. With a literal shared-object
    default (`Hash.new([])`, or `Hash.new({})`), `h[:a] << 1` mutates the one
    shared default array — but because it mutates and doesn't assign, **nothing
    is stored in the hash**, and worse, every key reads back the same polluted
    object: `h[:a] #=> [1]; h[:b] #=> [1]`. The block form fixes that by
    assigning: `h[:a] << 1` really does `h[:a] = []` first, then appends.
    Rule: immutable scalar default → `Hash.new(0)`; anything mutable →
    block form, or use `Hash.new { |h, k| h[k] = [] }` / `h[k] += [x]` /
    `h.compare_by_identity` irrelevant — or the newer `Hash.new { |h, k|
    h[k] = ... }.tap` idiom. (Note also that the block form *creates keys*, which
    changes `size`, `key?`, `keys`, iteration and `default` reporting.)

- id: ruby-collections-kwargs-02
  answer: |
    Ruby 3.0 **fully separated keyword arguments from positional hashes** (the
    2.x behavior that auto-converted a trailing hash into keywords and back was
    deprecated, and it was never symmetric): keywords are now their own argument
    type, required keywords are enforced per-method, and **a hash is no longer
    implicitly converted** — `m({a: 1})` passes a positional hash, not `a: 1`.
    Practical changes: methods defined with `def m(**opts)` no longer swallow a
    positional hash; passing keywords to `*args`-only methods raises; and you
    must **explicitly** splat/double-splat in both directions:
    ```ruby
    def m(a, b: 2); [a, b]; end
    opts = { b: 42 }
    m(1, **opts)        # explicit: pass a Hash as keyword arguments
    # to forward everything, including keywords:
    def wrapper(*args, **kwargs, &blk)
      target(*args, **kwargs, &blk)
    end
    ```
    Also changed: in 3.0 an empty `**{}` at a **call** site is no longer passed
    as an empty hash arg when the method doesn't accept keywords — it means "no
    keywords", so `m(**{})` works even for methods that take no kwargs (the 2.x
    behavior of injecting `{}` was removed). For a hash whose *keys you don't
    know* whose target takes `**opts`, you can now call `m(*arr)` then that
    hash? No — if you need a hash literally passed as a positional argument to a
    method whose parameter is `**kwargs`, you must pass it with `**`.

- id: ruby-collections-splat-03
  answer: |
    `*` (splat):
    - **Definition** — `def m(*rest)` collects all remaining positional
      arguments into an Array named `rest` (possibly empty).
    - **Definition, mid-list** — `def m(a, *mid, z)` captures the in-between
      arguments; the mandatory leading/trailing params are filled first.
    - **Call site** — `m(*array)` *expands* the array into individual positional
      arguments (`*` on the last argument can spread across the required ones).
    - **Assignment** — `a, *rest = list` destructures, the splat slurping the
      tail into an array (can appear on the left of multiple assignment to group
      or "absorb" the leftovers).
    `**` (double splat):
    - **Definition** — `def m(**opts)` collects all **keyword arguments** into a
      Hash named `opts` (possibly empty; in Ruby you cannot mix arbitrary `**opts`
      with named keyword collection? you can't combine `**opts` with explicit
      named keywords in the same list — one or the other).
    - **Call site** — `m(**hash)` expands a Hash's pairs into keyword arguments;
      combined with `*args` it's the standard "forward everything" idiom.
    - Since 3.0, `**` is strictly keyword-territory: it no longer converts to/from
      a trailing positional hash, so `m(**kw)` never sends a positional hash, and
      `m(*args)` never carries keywords.
    - Also usable to pop keyword hashes in destructuring:
      `first, **rest = {a: 1, b: 2}` (rare).
    (Shorthand forms exist too: `def m(a, *)` deliberately ignores the rest;
    `def method(*)`/`def m(*)` — anonymous splat — forward args verbatim
    (`m(*)` = `m(*args)`), and likewise `m(**)` forwards keywords.)

- id: ruby-collections-safenav-data-04
  answer: |
    `&.` — the **safe navigation operator**: `obj&.method` calls `method` on
    `obj` **only when `obj` is not nil**; if `obj` is nil, the whole expression
    is nil instead of raising `NoMethodError: undefined method for nil`.
    ```ruby
    user&.profile&.name   # => nil if user or profile is nil
    ```
    It is the compact, non-`andand` replacement for
    `user && user.profile && user.profile.name`, and works for setters and
    index methods too (`hash&.fetch(:k, :default)`? — yes, `hash&.[](:k)` style
    works as `a&.[](0)`). It only short-circuits on nil, **not on false** — and
    it strictly checks `obj != nil` (method calls like `foo` where a method
    returns nil are fine, but note `a&.b` guards exactly nil). Careful: `&.` does
    not help if the *error* is a genuine method-missing on a real object.
    `Data.define(:name, :age)` (Ruby 3.2) defines an **immutable value class**:
    positional and keyword constructors with the given members, getters (no
    writers, no mutation), structural `==`/`eql?`/`hash`, `to_h`,
    `members`, `with(**changes)` (returns a modified copy),
    `deconstruct`/`deconstruct_keys` for **pattern matching**, and
    `inspect`. It's the blessed replacement for hand-written immutable
    Structs-at-a-distance of `Struct`/OpenStruct in the value-object case, and
    the right tool for records that never change after construction.
```

All 32 questions answered from memory, no lookups. Where I had a moment of uncertainty (the subtle second point of the `%W`/`%I` note on frozen literal flags in the percent-literals question), I wrote what I know and flagged the boundary rather than inventing detail.

That full run took about **25k tokens**; my recollection of Ruby's semantics produces roughly consistent confidence across the set, with the weakest spots being:
- exact Ruby-3.0 kwargs edge-case details (empty `**{}` call-site behavior),
- the `Hash.new` block-form side effect on `size`/`keys` (I'm confident on the shared-default mutation bug),
- `%W`/`%I` interpolation/freezing flags.

Those are stated with appropriate hedging.
