# step-5-preview-free — WITH-SKILL validation answers for ruby (opencode-go, 2026-10-12); closed-book,
# derived/ruby.step-5-preview-free.SKILL.md body prepended to _prompts/ruby.md as active guidance.
- id: ruby-blocks-procvslambda-01
  answer: |
    Two differences: (1) Arity — lambdas check argument count like methods and raise
    ArgumentError on wrong count; non-lambda procs are lenient (missing args become
    nil, extra args silently dropped). (2) return semantics — `return` inside a
    lambda returns from the lambda only; `return` inside a non-lambda proc returns
    from the enclosing method where the proc was created. (Lambdas are made with
    `lambda`/`->`; procs with `proc`/`Proc.new` or by capturing a block.)
- id: ruby-blocks-yield-02
  answer: |
    `yield` transfers control to the block implicitly passed to the method — the
    block is not named in the parameter list. Arguments after `yield` are passed to
    the block's parameters, and the value of the `yield` expression is the block's
    return value; execution resumes after `yield` when the block finishes.
    `block_given?` returns true only if the caller actually supplied a block. It
    guards against LocalJumpError: calling `yield` with no block raises
    LocalJumpError, so optional block behavior is written as
    `yield(args) if block_given?`.
- id: ruby-blocks-ampblock-03
  answer: |
    In a definition, `&block` captures the block passed to the method as a Proc
    object bound to the parameter, so it can be stored, forwarded, or called with
    `block.call(...)`. In a call, `&` converts a Proc back into a block (it must be
    in the final argument position). `arr.map(&:to_s)` works because `Symbol#to_proc`
    turns `:to_s` into a proc that calls `.to_s` on each element, and `&` converts
    that proc into a block. So `&` toggles between "block" and "Proc object" forms.
- id: ruby-blocks-create-04
  answer: |
    Create a lambda with `lambda { |x| ... }` or the stabby form `->(x) { ... }`
    (`->(x) { ... }` and `lambda` enforce arity; `proc`/`Proc.new` create lenient
    non-lambda procs). Invoke a Proc/lambda with any of `p.call(args)`, `p.(args)`,
    or `p[args]`; a block converted to a proc can also be run implicitly with
    `yield`. Lambdas additionally satisfy `===` (so they work in `case`/`when`).
- id: ruby-modules-includeextendprepend-01
  answer: |
    `include M` mixes M's methods in as instance methods of the class; M is inserted
    into the ancestor chain between the class and its superclass:
    `[C, M, Object, ...]`. `prepend M` inserts M ahead of the class itself, so M's
    methods override C's own versions and M's methods can call the class version via
    `super`: `[M, C, Object, ...]`. `extend M` adds M's methods to one object's
    singleton class — used on a class it defines class methods (same effect as
    `class << self`); used on an instance it adds methods to that object only.
- id: ruby-modules-ancestors-super-02
  answer: |
    `super` calls the method of the same name further along the method lookup chain,
    starting after the class/module that defines the currently executing method.
    Lookup order is the ancestors list: singleton -> class -> included modules (in
    reverse inclusion order) -> superclass -> ... -> BasicObject. Included modules
    are ordinary ancestors, so lookup walks through them and `super` can reach a
    module's method from a class method and the other way around. `super` with no
    parentheses passes the current method's arguments; `super()` passes none.
- id: ruby-modules-namespace-03
  answer: |
    Namespacing: a module is a container of related classes, constants and helpers
    that avoids name clashes (`module Net; class HTTP; end; end` — referenced as
    `Net::HTTP`). Callable module function: `def self.util(x)` inside the module,
    called as `M.util(x)`. Also `module_function :name` (or bare `module_function`
    for subsequent defs), which copies the method onto the module's singleton class
    and makes the instance-method copy private; `extend self` is the other common way
    to get the same `M.name` call style.
- id: ruby-modules-refinements-04
  answer: |
    Reopening a class (monkey-patching) changes it globally and permanently for all
    code in the process — every gem, every file — with no scoping or undo.
    Refinements are lexically scoped: define them inside a module with
    `refine SomeClass do; def m; ...; end; end`, then activate them at the using
    site with `using M`. Activation only works at top level or in a class/module
    body and covers only the lexical scope from that point to the end of the
    file/class/module — not dynamically (e.g. not inside a method body). Outside
    that scope the original behavior is untouched.
- id: ruby-objects-methodmissing-01
  answer: |
    When you send a message that matches no method, Ruby calls
    `method_missing(name, *args, &block)` instead of immediately raising NoMethodError.
    Override it to implement dynamic behavior: proxies, builders, `find_by_*`-style
    APIs, DSLs. You must also override `respond_to_missing?(name, include_private =
    false)` to return true for those dynamic messages, because `respond_to?` (and all
    duck-typing / `method(:x)`-style checks) consults it — without it the object lies
    about its own interface. Always call `super` for messages you don't handle so
    genuine misses still raise NoMethodError.
- id: ruby-objects-send-02
  answer: |
    `send` (alias `__send__`) invokes a method by name (string or symbol) on an
    object, including private and protected methods. `public_send` invokes by name
    but only public methods — a protected/private name raises NoMethodError. They
    differ exactly when the named method is non-public. Prefer `public_send` when
    the method name comes from untrusted/user input.
- id: ruby-objects-attr-03
  answer: |
    `attr_reader :x` generates a getter `x` returning `@x`; `attr_writer :x`
    generates the setter `x=`; `attr_accessor :x` generates both. `define_method`
    (a private method on Module, called inside a class/module body) defines an
    instance method dynamically whose body is the given block — a closure that can
    capture local variables — for metaprogramming where names are computed at
    runtime.
- id: ruby-objects-visibility-04
  answer: |
    `private`: only callable with an implicit receiver — `obj.m` from outside raises
    NoMethodError (the sole exception: a private setter may be called as
    `self.x = v` since Ruby 2.7). `protected`: callable with an explicit receiver,
    but only when the receiver's class is the same as — or an ancestor/descendant
    of — `self`'s class. Practical split: private means "internal helper, never
    called from outside"; protected means "other instances of my kind may work on
    my state", e.g. `def ==(other); value == other.value; end`.
- id: ruby-enumerable-include-01
  answer: |
    The only method you must implement is `each`: it must yield every element to the
    supplied block, and by convention return an Enumerator when called without a
    block. In exchange, including `Enumerable` gives you the ~50 derived methods
    built on `each`: map, select/reject, find/detect, reduce, include?, sum, count,
    first/take, sort, min/max, group_by, partition, flat_map, each_with_index,
    each_with_object, tally, zip, and friends.
- id: ruby-enumerable-reduce-02
  answer: |
    `reduce`/`inject` folds the collection into one accumulated value:
    `list.reduce(0) { |acc, x| acc + x }` or the symbol form `list.reduce(:+)`.
    Without an explicit initial value, the first element becomes the accumulator.
    The block must return the accumulator each iteration. Use it when combining
    elements into a computed result (sum, join, nested structure).
    `each_with_object(memo)` is the better fit when you're building a collection
    (typically an Array or Hash for grouping): it hands the memo as a second block
    argument, ignores the block's return value, and returns the memo — no per-
    iteration bookkeeping, no accumulating-by-mistake bugs.
- id: ruby-enumerable-lazy-03
  answer: |
    `.lazy` wraps the enumerable in an `Enumerator::Lazy`, so chained operations
    (map, select, reject, take, drop, take_while, flat_map, with_index, ...) run
    element-by-element on demand instead of each step building a full intermediate
    array. That matters for infinite or huge sequences: you compute only what the
    consumer pulls, e.g.
    `(1..Float::INFINITY).lazy.map { |n| n * 2 }.select { |n| n % 7 == 0 }.first(5)`
    — an eager chain over an infinite range never terminates. Memory also stays
    flat on large finite collections, and the chain short-circuits as soon as the
    consumer stops taking elements.
- id: ruby-enumerable-comparable-04
  answer: |
    `include Comparable` and define the spaceship operator `<=>(other)`, returning
    -1, 0, or 1 (nil if the argument is not comparable). Comparable then supplies
    `<`, `<=`, `==`, `>`, `>=`, `between?`, and `clamp(min, max)` / `clamp(range)`.
    If `<=>` returns nil, the comparisons raise ArgumentError. As a bonus, an
    Enumerable with `<=>` also gets meaningful `sort`, `min`, `max`.
- id: ruby-metaprogramming-singleton-01
  answer: |
    Every object owns a hidden, anonymous singleton class (eigenclass) that sits at
    the head of its method lookup; methods defined there exist only for that object.
    Inside a class body, `class << self` opens the class's own singleton class:
    everything defined inside (def, attr_accessor, define_method) becomes a
    class-level member — i.e. class methods and class attributes. `class << obj`
    does the same for a specific object, and inside instance methods `class << self`
    reaches that instance's singleton class.
- id: ruby-metaprogramming-ivar-02
  answer: |
    `instance_variable_get(:@x)` reads and `instance_variable_set(:@x, v)` writes an
    instance variable by name (symbol or string), bypassing encapsulation and method
    visibility; reading an unset variable yields nil. Appropriate in metaprogramming
    layers, debugging, test helpers, and (de)serialization or object-copy utilities.
    In ordinary application code they're a smell: they couple callers to internal
    naming and skip any validation in writers — prefer public accessors.
- id: ruby-metaprogramming-definemethod-vs-mm-03
  answer: |
    `define_method` creates real, eager methods: they show up in `methods`/
    `respond_to?`, have inspectable arity, appear normally in stack traces, and use
    fast normal dispatch — but the method names must be knowable/enumerable at
    definition time. `method_missing` handles any undefined message, so it covers
    open-ended or unbounded method sets (proxies, `find_by_*`, builders) with no
    up-front cost — but it only fires on misses (so `respond_to?` lies unless you
    also define `respond_to_missing?`), arity/visibility introspection is
    unavailable, miss dispatch is slower, and sloppy handling silently swallows
    typos (so raise NoMethodError via `super` for anything you don't handle).
- id: ruby-metaprogramming-classnew-04
  answer: |
    `Class.new` creates a new anonymous class — `Class.new(SomeSuper)` optionally
    sets the superclass — and the block you pass is its body (def methods, include
    modules, etc.). By itself the class has no name and prints as
    `#<Class:0x...>`. Assigning it to a constant (`Foo = Class.new { ... }`) names
    it: `Foo.name`, `inspect`, error messages, and repr all show that name, the
    constant becomes resolvable through constant lookup, and the class can be
    reopened like any named class.
- id: ruby-error-standarderror-01
  answer: |
    `Exception` is the root of the whole hierarchy, and much more than StandardError
    hangs directly under it: `SystemExit` (raised by `exit`), `Interrupt` (Ctrl-C),
    `SignalException`, `NoMemoryError`, and `ScriptError` (SyntaxError, LoadError),
    plus interpreter-fatal errors. A bare `rescue` already catches every
    StandardError, so rescuing `Exception` buys nothing for ordinary errors while
    swallowing exactly the events a program must never swallow — it can make the
    process ignore `exit` and Ctrl-C, and mask OOM/syntax errors as "handled". Rescue
    StandardError subclasses or specific classes; only catch Exception at top-level
    boundaries, and even then re-raise the exit/signal types.
- id: ruby-error-ensure-retry-02
  answer: |
    `ensure` runs whenever execution finally leaves the block — normal completion, an
    exception propagating out, or even a `return` from inside the block — making it
    the guaranteed cleanup hook (close files, release locks). It runs even when no
    rescue matched; and if the ensure block itself raises, its exception replaces any
    in-flight one. `retry`, written inside a rescue clause, re-executes the `begin`
    block from the top — it does not leave the construct, so ensure does not run
    between attempts, only once when the block is finally done. Bound retries with a
    counter or a genuinely changing condition or you loop forever.
- id: ruby-error-custom-03
  answer: |
    Define `class MyError < StandardError; end` — subclass StandardError so that a
    bare `rescue` still catches it — optionally with a custom `initialize` that calls
    `super(msg)` and stores extra context. `raise` forms: `raise MyError.new("boom")`,
    the shorthand `raise MyError, "boom"`, `raise "boom"` (RuntimeError), and a bare
    `raise` inside a rescue, which re-raises the current exception (`$!`) with its
    original backtrace.
- id: ruby-error-elserescue-04
  answer: |
    `def m; ...; rescue ...; end` wraps the entire method body in an implicit begin,
    so `rescue`, `else`, and `ensure` all work at the method level. The `else`
    clause is the success path: it runs only when the body completed without
    raising — after the body, before `ensure`. Its point is to hold code that must
    run only on success and must not itself be covered by that same rescue, since
    else sits outside the protected body. If an exception occurred, `else` is skipped
    whether or not a rescue handled it; if the exception propagates, ensure still
    runs but else does not.
- id: ruby-strings-symbols-01
  answer: |
    Symbols are immutable, interned, and unique: `:a` is the same object every time
    it is written (same object_id), so they are cheap identity tokens. Strings are
    mutable value objects — distinct instances with equal text are different objects
    but are `==`. Use Symbols as identifiers: hash keys, method names (`send(:x)`),
    states/enums, options. Use Strings for text data (names, content). Convert with
    `to_s`, and `to_sym` / `intern`; since Ruby 2.2 dynamic symbols are
    garbage-collectable.
- id: ruby-strings-frozen-02
  answer: |
    `# frozen_string_literal: true` (first line, or after a shebang and before any
    code) freezes every string literal in that file — the literals are frozen and
    reused rather than reallocated, and mutating one raises FrozenError. The subtlety
    about interpolation: an interpolated literal like `"foo#{bar}"` is built at
    runtime, so it is still a fresh, unfrozen String even with the magic comment —
    mutators like `<<` work on it. Only purely static literals are frozen. (Use
    `+"literal"` or `"literal".dup` when you explicitly need an unfrozen copy.)
- id: ruby-strings-quotes-03
  answer: |
    Single quotes are nearly raw: no interpolation and almost no escapes — only `\\`
    and `\'` are special. Double quotes interpret escape sequences (`\n`, `\t`,
    `\"`, ...) and string interpolation. Interpolation is `"#{expression}"`: it
    inserts the result of any expression, calling `to_s` on it; shorthand `#@ivar`
    and `#$global` forms also work. `%q` gives single-quote semantics, `%Q` gives
    double-quote semantics.
- id: ruby-strings-percent-04
  answer: |
    `%w[one two three]` produces an array of Strings: `["one", "two", "three"]` —
    whitespace-separated words, no interpolation, no escapes. `%i[one two three]`
    produces an array of Symbols: `[:one, :two, :three]`. The uppercase forms `%W`
    and `%I` add interpolation and full escape processing. The brackets can be
    replaced by almost any paired delimiter, e.g. `%w(a b c)` or `%w{a b c}`.
- id: ruby-collections-hashdefault-01
  answer: |
    `Hash.new(0)` has one shared default value: every read of a missing key returns
    0 and stores nothing, so `h.size`, `h.key?(:x)`, and `h.keys` are unchanged and
    every miss yields the same 0 object. `Hash.new { |h, k| h[k] = [] }` runs the
    block on each miss with the hash and the missing key, and because this block
    assigns, the key is created and stored — a fresh array per key — so `h.key?(:x)`
    becomes true, `h.size` grows, and later reads hit a real value. That matters when
    you only read with a default (0 is fine, nothing persists) versus building or
    grouping (entries must persist, each key with its own fresh object).
- id: ruby-collections-kwargs-02
  answer: |
    Since Ruby 3.0 keyword arguments are fully separated from a trailing positional
    Hash — the 2.x hash-to-keywords auto-conversion was removed. It changed in one
    direction only: a Hash passed positionally to a method that declares keywords
    (`def m(name:)` or `def m(**opts)`) is no longer converted and raises
    ArgumentError, so you pass `m(**hash)` — and `m(**{})` passes no keywords at all
    (it does not inject an empty positional Hash). The other direction still works:
    keywords passed to a method declaring no keywords (`def m(*a)` or `def m(a, b)`)
    are still gathered into a trailing positional Hash. Related: `**hash` also merges
    keyword arguments in a call, and `ruby2_keywords` keeps a method forwarding a
    flagged hash as keywords for legacy delegation.
- id: ruby-collections-splat-03
  answer: |
    In definitions, `*args` collects the remaining positional arguments into an
    Array, `**kwargs` collects the remaining keyword arguments into a Hash, and
    `&blk` collects the block as a Proc. In calls, `*arr` expands an Array into
    separate positional arguments and `**hash` expands into keyword arguments (hence
    `m(a: 1, **h)` merges). `*args` never captures keywords and `**opts` never
    captures positionals, so the forward-everything idiom is:
    `def wrapper(*args, **kwargs, &blk); target(*args, **kwargs, &blk); end`.
    Ruby 3.0 lets `def wrapper(...) = target(...)` forward all three; 3.2 lets
    anonymous params be forwarded bare: `def m(*, **) = target(*, **)`.
    `**` is not valid in multiple assignment — `first, **rest = hash` is a
    SyntaxError; use the splat for arrays only (`head, *tail = list`) and pattern
    matching for hashes: `case hash in {a:, **rest}`.
- id: ruby-collections-safenav-data-04
  answer: |
    `&.` is safe navigation: `x&.foo` calls `foo` on `x` only when `x` is not nil,
    otherwise the whole expression evaluates to nil instead of raising
    NoMethodError. It guards only nil (not false) and chains nicely:
    `user&.profile&.name`. `Data.define` (Ruby 3.2) builds an immutable value class:
    `Point = Data.define(:x, :y)` gives a class whose instances are constructed
    positionally or by keyword, expose readers but no writers (state is frozen), have
    value equality (`==`/`eql?`/`hash` over all members), support `to_h`/`members`,
    return updated copies via `with(x: ...)`, have a readable `inspect`, and work
    with pattern matching.
