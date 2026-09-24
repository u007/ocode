- id: ruby-blocks-procvslambda-01
  answer: |
    Both are callable Proc objects, but lambdas enforce their declared arity
    strictly, while ordinary procs are lenient: missing arguments generally
    become nil and extra arguments are ignored. Also, `return` inside a lambda
    returns from the lambda itself; `return` inside a non-lambda proc attempts
    to return from the method where the proc was defined, which can cause a
    LocalJumpError if that method has already finished.
- id: ruby-blocks-yield-02
  answer: |
    `yield` executes the block passed to the current method without requiring a
    block parameter. `block_given?` returns true when a block was supplied, so
    it can guard an optional block and prevent `yield` from raising
    LocalJumpError.
- id: ruby-blocks-ampblock-03
  answer: |
    In `def m(&block)`, `&` captures the supplied block as a Proc object in
    `block`; it is nil if no block was given. At a call site, `&value` converts
    a Proc, Symbol, or object responding to `to_proc` into a block.
    `arr.map(&:to_s)` uses `Symbol#to_proc`, so each element is passed to
    `to_s`.
- id: ruby-blocks-create-04
  answer: |
    Lambdas can be created with `lambda { |x| ... }` or `->(x) { ... }`.
    `proc { ... }` and `Proc.new { ... }` create ordinary, non-lambda Procs.
    Invoke either kind with `.call(args)`, `.(args)`, or `[args]`; a Proc can
    also be passed to another method with `&proc` or invoked as a block with
    `yield` when supplied to that method.
- id: ruby-modules-includeextendprepend-01
  answer: |
    `include M` adds M to the class's ancestors after the class, so its methods
    become instance methods and the class's own methods take precedence.
    `extend M` adds M to the singleton class of an object, making the methods
    available on that object itself, commonly as class methods. `prepend M`
    inserts M before the class, so M's methods take precedence and `super` in
    the class method can reach M. Later-included modules are normally searched
    before earlier-included modules.
- id: ruby-modules-ancestors-super-02
  answer: |
    `super` looks for the next definition of the current method in the
    receiver's ancestor chain, after the currently executing definition.
    Bare `super` forwards the same arguments, `super(args)` forwards explicit
    arguments, and `super()` forwards none. Included modules participate in
    this lookup after the class (with prepended modules before it), and
    superclass methods appear later in the chain.
- id: ruby-modules-namespace-03
  answer: |
    A module is also commonly used as a namespace for related constants,
    classes, and functions. A callable module function can be defined as an
    instance method and exposed as a singleton method:

        module Greeting
          def self.hello
            "hello"
          end
        end

    `module_function :name` is another way to make a defined method callable as
    `M.name`; it also makes the original instance method private.
- id: ruby-modules-refinements-04
  answer: |
    Refinements provide a scoped alternative to globally reopening and
    monkey-patching a class. They limit the modified method lookup to code that
    has activated the refinement, avoiding changes to unrelated callers. They
    are declared with `refine SomeClass do ... end` and activated lexically
    with `using TheRefinement` in the relevant class, module, or file scope.
- id: ruby-objects-methodmissing-01
  answer: |
    `method_missing` is called when normal method dispatch cannot find a method.
    It receives the missing method name, its arguments, and optionally its
    block, and can implement a dynamic API or call `super` to produce the
    normal NoMethodError. `respond_to_missing?` should also be defined so
    `respond_to?` and other reflection code recognize the dynamic methods and
    return the appropriate true or false value.
- id: ruby-objects-send-02
  answer: |
    `send` invokes a method by name regardless of whether it is public,
    protected, or private. `public_send` invokes only public methods. They
    differ for private and protected methods; `public_send` is useful when a
    call should respect encapsulation, while `send` can bypass it.
- id: ruby-objects-attr-03
  answer: |
    `attr_reader :name` generates an instance getter, `attr_writer :name`
    generates `name=`, and `attr_accessor :name` generates both. These methods
    normally access the corresponding instance variable and are public by
    default. `define_method` dynamically defines a method from a block or
    Proc, often allowing the block to capture local variables as a closure.
- id: ruby-objects-visibility-04
  answer: |
    A private method is normally called with an implicit receiver and cannot
    be called as `object.private_method`; an explicit receiver is disallowed,
    apart from special setter syntax. A protected method can be called with an
    explicit receiver when the call occurs within an instance of the defining
    class or a subclass and the receiver is an appropriate instance of that
    class hierarchy.
- id: ruby-enumerable-include-01
  answer: |
    Include `Enumerable` and implement an `each` method that yields the
    object's elements. The class then receives methods such as `map`,
    `select`, `reject`, `find`, `sort_by`, `include?`, and `to_a`, all based on
    iteration through `each`.
- id: ruby-enumerable-reduce-02
  answer: |
    `reduce`, also called `inject`, folds all elements into one accumulator:

        result = values.reduce(0) { |sum, value| sum + value }

    An optional initial value can be supplied. `each_with_object` is often
    better when building one object such as an Array or Hash: it passes the
    element and a memo object to the block and returns the memo object,
    avoiding manually carrying or reassigning an accumulator.
- id: ruby-enumerable-lazy-03
  answer: |
    `.lazy` returns an `Enumerator::Lazy` that postpones evaluation and
    composes the requested operations into a lazy chain. Values are computed
    only as they are requested, so the chain can work with very large or
    infinite sequences without materializing the entire result. Calling
    `each`, `take`, `force`, or another realizing operation eventually evaluates
    the needed values.
- id: ruby-enumerable-comparable-04
  answer: |
    Define `<=>` to compare an instance with another and return a negative
    number, zero, or a positive number, or nil when they are not comparable,
    then include `Comparable`:

        class Person
          include Comparable
          attr_reader :age
          def initialize(age); @age = age; end
          def <=>(other); age <=> other.age; end
        end

    This supplies methods such as `<`, `<=`, `>`, `>=`, `between?`, and
    `clamp`. A collection can be sorted with `sort` when its elements implement
    `<=>` and the collection includes `Enumerable`.
- id: ruby-metaprogramming-singleton-01
  answer: |
    Every object has a singleton class, also called its eigenclass, which holds
    methods specific to that object. Inside a class body, `self` is the class
    object, so `class << self` opens the class object's singleton class:

        class Example
          class << self
            def build
            end
          end
        end

    This defines a class method, not an instance method, and is roughly
    equivalent to `def self.build`.
- id: ruby-metaprogramming-ivar-02
  answer: |
    `instance_variable_get(:@name)` reads an instance variable from an object,
    and `instance_variable_set(:@name, value)` sets it. The name can be a
    symbol or string, and the variable need not have been defined beforehand.
    These methods are appropriate for reflection, debugging, serializers, and
    metaprogramming, but ordinary application code should usually use public
    accessors so invariants and encapsulation are preserved.
- id: ruby-metaprogramming-definemethod-vs-mm-03
  answer: |
    `define_method` creates a real method in the class's method table, so it is
    discoverable by `respond_to?`, participates in normal overriding and method
    lookup, and has some dispatch overhead. It is appropriate for a finite set
    of dynamically generated methods and can capture a block's local variables.
    `method_missing` handles names for which no real method exists, making it
    suitable for very large or open-ended DSLs with less method-table growth,
    but it is slower and requires `respond_to_missing?` for proper reflection.
- id: ruby-metaprogramming-classnew-04
  answer: |
    `Class.new(superclass = Object, &block)` creates a new class, optionally
    with the given superclass, and evaluates the block in the class's context.
    Assigning it to a constant, such as `Person = Class.new`, gives the class a
    stable name and makes it accessible by that name; without the constant it
    remains anonymous, even though the class object can still be used.
- id: ruby-error-standarderror-01
  answer: |
    A bare `rescue` and `rescue => e` handle exceptions under `StandardError`,
    which covers ordinary application errors. `Exception` is above that level
    and also includes conditions such as `SystemExit`, `Interrupt`,
    `NoMemoryError`, and `SyntaxError`. Rescuing it can prevent normal program
    termination, swallow serious failures, and hide bugs. Rescue specific
    expected exceptions, or re-raise exceptions that should not be handled.
- id: ruby-error-ensure-retry-02
  answer: |
    The `ensure` clause runs when control leaves the protected `begin` block,
    whether it completes normally, an exception is handled, or an exception
    continues propagating. It is intended for cleanup and does not by itself
    suppress the exception. `retry` is allowed in a `rescue` clause and causes
    the associated `begin` body to be executed again from its beginning,
    potentially repeating until the condition succeeds or a limit is reached.
- id: ruby-error-custom-03
  answer: |
    Define a custom exception by subclassing an appropriate exception, commonly
    `StandardError`:

        class InvalidInput < StandardError
        end

        raise InvalidInput, "bad value"

    `raise` can re-raise the current exception when used without arguments,
    raise a class, raise a class with a message, raise an exception instance, or
    raise a string, which creates a RuntimeError. A form with a backtrace is also
    available.
- id: ruby-error-elserescue-04
  answer: |
    The optional `else` clause runs only when the protected body completes
    without raising an exception. It is skipped when an exception is raised,
    even if a `rescue` clause handles that exception. A method body may contain
    `rescue` and `else` directly without an explicit `begin` because those
    clauses are also allowed as part of the method's exception-handling syntax.
- id: ruby-strings-symbols-01
  answer: |
    A Symbol represents a name or identifier and is immutable; equal symbols
    such as `:name` have the same identity and are commonly used as Hash keys
    and method names. A String represents character data, is mutable by default,
    and compares by its contents, so separately created strings with the same
    text may have different identities. Symbols are converted with
    `to_s` and strings with `to_sym` when needed.
- id: ruby-strings-frozen-02
  answer: |
    The `# frozen_string_literal: true` magic comment makes ordinary,
    non-interpolated string literals in that file frozen, preventing accidental
    mutation. It does not freeze all strings obtained at runtime. A subtlety is
    that an interpolated string such as `"value: #{value}"` is dynamically
    constructed and remains unfrozen even when the magic comment is present; it
    must be explicitly frozen if immutability is required.
- id: ruby-strings-quotes-03
  answer: |
    Single-quoted strings are mostly literal: they do not perform interpolation,
    and only escapes for a backslash or single quote are specially interpreted.
    Double-quoted strings support escape sequences such as `\n` and `\t`, as
    well as interpolation using `#{expression}`. Interpolation evaluates the
    expression and inserts its result into the string.
- id: ruby-strings-percent-04
  answer: |
    `%w[one two three]` creates an Array of Strings. `%i[one two three]`
    creates an Array of Symbols. Both split their contents into whitespace-
    separated elements, while `%W` and `%I` are the corresponding interpolating
    forms.
- id: ruby-collections-hashdefault-01
  answer: |
    `Hash.new(0)` returns the integer `0` when a missing key is read; it does
    not insert that key. This is useful for counters because `counts[key] +=
    1` starts from zero and then assigns the result. `Hash.new { |h, k|
    h[k] = [] }` calls a block for a missing key and can create and store a
    per-key default value, which is useful for lists or nested collections.
    Because the block assigns to the Hash, the missing key is inserted.
- id: ruby-collections-kwargs-02
  answer: |
    Ruby 3.0 no longer automatically converts a final positional Hash into
    keyword arguments. A method can explicitly receive keywords with `def m
    name:, **options`, and a call such as `m(name: "Ada", active: true)` passes
    them as keywords. To pass a Hash as keywords, expand it with `m(**hash)`.
    Calling `m(hash)` instead passes the Hash as one positional argument, which
    is useful for methods that explicitly accept a positional options Hash.
- id: ruby-collections-splat-03
  answer: |
    In a method definition, `*args` collects extra positional arguments into
    an Array, while `**kwargs` collects remaining keyword arguments into a
    Hash. At a call site, `*array` expands an Array into separate positional
    arguments and `**hash` expands a Hash into keyword arguments:

        def call(*args, **kwargs)
        end
        call(*values, **options)

    In Ruby 3, these positional and keyword roles remain distinct.
- id: ruby-collections-safenav-data-04
  answer: |
    `object&.method` calls `method` only when `object` is not nil; if it is nil,
    the expression returns nil. It short-circuits only nil, not false, and is
    useful for chained calls such as `user&.profile&.name`. `Data.define` was
    added in Ruby 3.2 and creates a lightweight immutable value class:

        Point = Data.define(:x, :y)
        point = Point.new(x: 1, y: 2)

    Its instances are frozen, expose readers for the declared attributes, and
    provide value-style equality and pattern-matching support.
