# ruby knowledge benchmark — answers
# Model: opencode-go/muse-spark-1.2   (provider-stripped id: muse-spark-1.2)
# CLOSED-BOOK via ocode2 run -dir /tmp/kaizen-ruby-answer -yolo -effort medium (isolated empty dir, no corpus access). corpus_rev 1.

- id: ruby-blocks-procvslambda-01
  answer: |
    The two main differences are:
    1. **Arity checking:** A `lambda` enforces strict argument count (like a method) and raises ArgumentError if wrong number of args is given. A `Proc` (from `Proc.new`/`proc`) is lenient - missing args become nil, extra args are ignored.
    2. **Return semantics:** `return` inside a `lambda` returns only from the lambda itself (to its caller). `return` inside a `Proc` returns from the enclosing *method* that defined the Proc (non-local return) and raises LocalJumpError if that method has already returned. Related: `break` also behaves differently. Lambdas are also distinguished by `lambda? == true` and are considered slightly more method-like.

- id: ruby-blocks-yield-02
  answer: |
    `yield` executes the block that was passed to the current method, optionally passing arguments: `yield(arg1, arg2)` calls the block with those args. It is implicit - no block parameter needed.
    If no block was given and you call `yield`, Ruby raises `LocalJumpError: no block given`.
    `block_given?` (also `Kernel#block_given?`) returns true if a block was passed to the current method. Guarding with `yield ... if block_given?` or `return enum unless block_given?` protects against that error and is the standard idiom to make a block optional or to return an Enumerator when none is given.

- id: ruby-blocks-ampblock-03
  answer: |
    In `def m(&block)`, `&` in the parameter list captures the implicit block passed to the method as an explicit `Proc` object named `block`. Without `&` you use `yield`; with `&` you get a reference you can store, pass on, or call with `block.call`. If no block is given, `block` is nil.
    In a call like `arr.map(&:to_s)`, `&` is the unary *block-conversion* operator: it calls `to_proc` on the object and converts the resulting Proc into a block for the method. `Symbol#to_proc` is defined as `proc { |obj, *args| obj.send(self, *args) }`, so `&:to_s` becomes `{ |e| e.to_s }`. It also works as `foo(&my_proc)` to pass an explicit Proc as a block.

- id: ruby-blocks-create-04
  answer: |
    Create lambda:
    ```ruby
    l1 = lambda { |x| x+1 }
    l2 = ->(x) { x+1 }          # stabby lambda, `->`
    l3 = -> x { x+1 }
    l4 = lambda do |x| x+1 end
    ```
    Create Proc (non-lambda):
    ```ruby
    p1 = Proc.new { |x| x+1 }
    p2 = proc { |x| x+1 }       # Kernel#proc
    ```
    Invoke:
    ```ruby
    p.call(1)      # preferred
    p.call(1,2)
    p.(1)          # syntactic sugar for call
    p[1]           # [] is alias for call
    p === 1        # case-equality, invokes proc (used in `case when`)
    yield          # only inside a method with a block, not for stored Proc
    instance_exec(&p) / instance_exec(*args, &p)
    ```
    `.call`, `[]`, `.()` and `===` all invoke; `call` allows explicit handling of arity.

- id: ruby-modules-includeextendprepend-01
  answer: |
    All three mix a Module's methods into a class but at different places in the ancestor chain:
    - `include Mod`: inserts Mod *above* the class in ancestors (just after the class). Instance methods of Mod become available as **instance methods** of the class. `Ancestors = [Class, Mod, ...]`.
    - `extend Mod`: extends the single object (the class object itself if done inside class body). Methods become **singleton/class methods** of the class, not instance methods. `class.extend(Mod)` is equivalent to `class.singleton_class.include(Mod)`.
    - `prepend Mod`: like include but inserts Mod *before* the class: `Ancestors = [Mod, Class, ...]`. Thus Mod's methods override the class's methods, and can call `super` to reach the class's original implementation. Used for wrapping/intercepting.
    Lookup is left-to-right in `ancestors`.

- id: ruby-modules-ancestors-super-02
  answer: |
    `super` (bare `super` forwards original args, `super()` forwards no args, `super(a,b)` forwards explicit args) calls the next method with the same name up the ancestor chain from the current method's defining location.
    Method lookup walks `Class.ancestors` linearly: singleton class -> prepended modules -> class itself -> included modules (in reverse order of inclusion, last included first) -> superclass -> its prepended/included modules -> ... up to BasicObject.
    So an `include`d module sits between the class and its superclass; a `prepend`ed module sits before the class. `super` will therefore find the next included module's version before the superclass version.

- id: ruby-modules-namespace-03
  answer: |
    Besides mixins, the other primary use is **namespacing** - grouping related constants/classes/methods and avoiding name collisions: `module MyApp; class Foo; end; end` accessed as `MyApp::Foo`.
    Module "functions" - methods callable as `Mod.method` and also mixable as private instance methods:
    ```ruby
    module M
      def foo; "hi" end
      module_function :foo           # makes a copy as M.foo and privatizes instance version
      # or:
      # extend self
      # module_function
      # def bar; end
    end
    M.foo
    ```
    `module_function` creates a singleton copy and makes the instance version private. `extend self` is a simpler alternative but leaves it public.

- id: ruby-modules-refinements-04
  answer: |
    Reopening a class (monkey-patching) is global - every file sees the change, causing conflicts and surprise.
    Refinements allow a lexically scoped monkey patch: you add/modify methods on a class but they are only visible where explicitly activated, and only within that file/scope.
    ```ruby
    module StringExt
      refine String do
        def shout; upcase + "!" end
      end
    end
    using StringExt  # activates refinements in this file/scope (from this point to end of file/module)
    "hi".shout  # works only after `using`
    ```
    `using` must be at top-level or inside a module/class body and its effect is lexical, not global. Safer for libraries.

- id: ruby-objects-methodmissing-01
  answer: |
    When Ruby cannot find a method after walking the ancestor chain, it calls `method_missing(name, *args, &block)` on the receiver instead of immediately raising NoMethodError. By overriding it you can handle dynamic/delegated methods:
    ```ruby
    def method_missing(name, *args, &blk)
      if name.to_s.start_with?("find_by_")
        # ...
      else super end
    end
    ```
    Always call `super` for unhandled names so NoMethodError still occurs.
    You must also override `respond_to_missing?(name, include_private)` to return true for the names you handle, because `respond_to?` otherwise returns false (it does not trigger `method_missing`). This keeps `respond_to?` / `Object#methods` consistent and is required for `respond_to?` checks, delegation, and tools.

- id: ruby-objects-send-02
  answer: |
    Both invoke a method by name dynamically:
    ```ruby
    obj.send(:foo, 1, 2)
    obj.public_send(:foo, 1, 2)
    obj.__send__(:foo)
    ```
    `send` can call *any* method regardless of visibility - private and protected - bypassing encapsulation.
    `public_send` respects visibility and can only call public methods; it will raise NoMethodError for private/protected methods.
    Use `public_send` by default for dynamic dispatch from untrusted input; use `send`/`__send__` only when you intentionally need to bypass privacy (metaprogramming, testing).

- id: ruby-objects-attr-03
  answer: |
    ```ruby
    attr_reader :name   # def name; @name; end
    attr_writer :name   # def name=(v); @name=v; end
    attr_accessor :name # both
    ```
    They are macros that define getter/setter instance methods for an instance variable of the same name.
    `define_method(name) { |*args, &blk| ... }` defines an instance method dynamically at runtime (often inside `class << self` or `define_singleton_method` for class methods). Unlike `def`, its body is a block/closure so it closes over local variables, allowing metaprogrammed methods in a loop:
    ```ruby
    [:foo,:bar].each { |m| define_method(m) { @data[m] } }
    ```

- id: ruby-objects-visibility-04
  answer: |
    `private` methods can only be called with an *implicit* receiver (`self` is implied, not written) and only from within the same object (`self`). Since Ruby 2.7 private methods can be called with `self.` literal, but not with another object's receiver. Calling `obj.private_method` fails even if `obj` is `self` written explicitly (pre-2.7).
    `protected` methods can be called with an explicit receiver, but only when the receiver is `self` or an instance of the same class (or subclass) as the caller - i.e., within the family. This allows collaborative access: e.g., `def ==(other); @value == other.value; end` where `other.value` needs protected `value`.
    In short: private = no explicit receiver, protected = explicit receiver allowed but only for same-class objects.

- id: ruby-enumerable-include-01
  answer: |
    Define `each` that yields each element in turn; if no block given, it must return an Enumerator (`return enum_for(:each) unless block_given?`). Then `include Enumerable`.
    ```ruby
    class MyCollection
      include Enumerable
      def each
        return enum_for(:each) unless block_given?
        @items.each { |e| yield e }
      end
    end
    ```
    You immediately get ~50 methods: `map`, `select`, `reject`, `find`, `any?`, `all?`, `count`, `group_by`, `sort_by`, `each_with_index`, `reduce`, `min`, `max`, etc., all built on `each`.

- id: ruby-enumerable-reduce-02
  answer: |
    `reduce(initial, sym)` / `inject` folds a collection to a single value by iterating and threading an accumulator:
    ```ruby
    [1,2,3].reduce(0) { |acc, elem| acc + elem } # => 6
    [1,2,3].reduce(:+)                            # symbol shorthand
    ```
    If no initial value given, first element is used as initial acc.
    `each_with_object(obj)` also threads an object but *always returns that same object* and does not reassign it each iteration; the block mutates the object: `evens = [1,2,3].each_with_object([]) { |e, a| a << e if e.even? }`. Prefer `each_with_object` when the accumulator is a mutable collection/hash you are shoveling into - `reduce` would require returning the accumulator each time (`a << e; a`) and fails if you use immutable reassign. `reduce` is better for numeric/immutable accumulators.

- id: ruby-enumerable-lazy-03
  answer: |
    `.lazy` returns a `Enumerator::Lazy` wrapper. Subsequent chaining methods (`map`, `select`, `reject`, `flat_map`, etc.) become lazy: they do not create intermediate arrays and evaluate element-by-element on demand, only when a terminal operation (`force`, `first`, `to_a`, `each`, `take`) pulls values.
    This avoids allocating huge intermediate arrays, allows chaining on infinite sequences (`(1..Float::INFINITY).lazy.select(&:odd?).map{...}.first(10)`), and enables short-circuiting so `select` + `map` won't process elements after the needed result is found. Without lazy, the whole collection is materialized eagerly at each step.

- id: ruby-enumerable-comparable-04
  answer: |
    Include `Comparable` and define the spaceship operator `<=>` returning -1, 0, +1, or nil (nil means incomparable):
    ```ruby
    class Person
      include Comparable
      attr_reader :age
      def <=>(other)
        age <=> other.age  # nil if other not Person
      end
    end
    ```
    You automatically get `<`, `<=`, `>`, `>=`, `==`, `between?`, `clamp`, and `sort`/`min`/`max` work. For sorting in Enumerable you can also just implement `<=>` or pass a block to `sort { |a,b| ... }` / `sort_by`.

- id: ruby-metaprogramming-singleton-01
  answer: |
    Every Ruby object has a hidden anonymous *singleton class* (eigenclass, `#singleton_class`) that sits at the front of its ancestor chain and holds methods defined only on that single object.
    `class << self` opens that singleton class:
    ```ruby
    class Foo
      class << self
        def bar; end # defines Foo.bar (singleton method)
      end
    end
    # equivalent to def self.bar
    ```
    Inside `class << obj`, `self` becomes the singleton class object, so `def` inside defines singleton methods. It is also used to include modules only for that one object.

- id: ruby-metaprogramming-ivar-02
  answer: |
    `obj.instance_variable_get(:@foo)` reads the instance variable `@foo` from `obj` (returning nil if not set). `obj.instance_variable_set(:@foo, value)` writes/creates it regardless of visibility.
    They bypass the getter/setter and directly access storage.
    Appropriate for metaprogramming, serialization/deserialization, testing, debugging, copying state, or generic code (e.g., `initialize` from hash). Not for normal business logic where accessors should be used, as they break encapsulation and expose internal representation.

- id: ruby-metaprogramming-definemethod-vs-mm-03
  answer: |
    `define_method`: defines a real method entry at class-definition time (or runtime). Introspection (`methods`, `respond_to?`, `method`) sees it, it is fast (normal dispatch), stack traces are clear. Cost is a method per name - if many possible names, it bloats class. Requires knowing names up front (or enumerating them).
    `method_missing`: handles *any* unknown name lazily with one handler, no per-name cost, good for truly open-ended/dynamic APIs (e.g., `find_by_*`). But it is slower, invisible to `respond_to?` unless you add `respond_to_missing?`, invisible to `methods`, harder to document/debug, and can mask typo NoMethodErrors. Use `define_method` (or `define_singleton_method`) when the set of names is known/enumerable; prefer it for performance/clarity; reserve `method_missing` for unbounded delegation.

- id: ruby-metaprogramming-classnew-04
  answer: |
    `Class.new(superclass = BasicObject)` dynamically creates a new anonymous Class object, optionally with a superclass and with a block that is evaluated as the class body:
    ```ruby
    klass = Class.new(ActiveRecord::Base) do
      def foo; end
    end
    ```
    Until assigned to a constant, the class is anonymous: `klass.name == nil`, its inspect is `#<Class:0x...>`. Assigning to a constant (`MyClass = klass` or `MyModule::MyClass = klass`) gives it a name (`MyClass.name == "MyClass"`) and registers it in `Module.constants`, making it inspectable, usable for deserialization, Rails autoloading, and `name`/`to_s`. Without a constant, it exists but is unnamed and may be GC'd if not referenced.

- id: ruby-error-standarderror-01
  answer: |
    Ruby's exception hierarchy: `Exception` is the root; `StandardError` is its child for normal application errors. Other sub-classes of `Exception` like `NoMemoryError`, `SignalException`/`Interrupt`, `SystemExit`, `fatal` are not meant to be caught in normal `begin/rescue` - catching them would swallow Ctrl-C, `exit`, out-of-memory, etc., preventing the process from terminating and masking fatal conditions.
    `rescue` without a class implicitly `rescue StandardError`; `rescue => e` is `rescue StandardError => e`. To catch `Exception` you must write `rescue Exception`. Doing so is almost always wrong; rescue the specific `StandardError` subclasses you expect (`rescue ArgumentError`, `rescue MyError`), or at least `StandardError`.

- id: ruby-error-ensure-retry-02
  answer: |
    `ensure` code runs *always* after the `begin`/`rescue`/`else` block, regardless of whether an exception was raised, rescued, re-raised, or a `return`/`break`/`next` occurred - typically for cleanup (closing files, unlocking). Even `return` inside `begin` still runs `ensure` before returning.
    `retry` (only valid inside `rescue`) restarts the entire `begin` block from the top (re-executes the `begin` section). Useful for transient retries:
    ```ruby
    attempts = 0
    begin
      do_call
    rescue Net::TimeoutError
      attempts += 1
      retry if attempts < 3
      raise
    end
    ```
    In Ruby 3+ retry is restricted and requires the rescued exception context.

- id: ruby-error-custom-03
  answer: |
    ```ruby
    class MyError < StandardError; end               # prefer StandardError
    class DetailedError < StandardError
      attr_reader :code
      def initialize(msg="oops", code: 0); super(msg); @code=code; end
    end
    ```
    Forms of raise:
    ```ruby
    raise MyError, "message"
    raise MyError.new("message")
    raise "message"                # raises RuntimeError with that message
    raise                          # inside rescue, re-raises current $! exception
    fail MyError, "msg"            # alias for raise
    ```

- id: ruby-error-elserescue-04
  answer: |
    You can write `def foo; rescue => e; end` or `def foo; ...; rescue; end` without `begin` - the method body acts as implicit `begin ... rescue ...`.
    The `else` clause (in `begin ... rescue ... else ... ensure ... end`) runs *only if no exception was raised* (i.e., the `begin` succeeded). It is for code that should run on success but should not be inside the `begin` where its own exceptions would be incorrectly caught by the `rescue`:
    ```ruby
    begin
      result = do_something
    rescue MyError => e
      handle(e)
    else
      # only on success, exceptions here are NOT rescued
      process(result)
    ensure
      cleanup
    end
    ```

- id: ruby-strings-symbols-01
  answer: |
    String: mutable (though often frozen), content-based object; each literal creates a new object (`"foo".object_id` differs). Used for data/text that changes.
    Symbol: immutable, interned, identity-based; `:foo` always same object id, compared by identity (fast). Symbols are not garbage-collected in older Rubies (now collectible but still intended for long-lived names). Typical use: for identifiers, hash keys, enum-like names, method names (`:to_s`). Strings for user data/content; symbols for labels/keys to avoid allocation and to convey intent.

- id: ruby-strings-frozen-02
  answer: |
    `# frozen_string_literal: true` (magic comment on first line) makes every string literal in that file frozen (immutable) by default - `str.frozen?` true, mutation raises `FrozenError`. Reduces allocations and is Ruby 3+ best practice.
    Subtlety: interpolated strings (`"hello #{name}"`) are *not* frozen even with the magic comment because they are created dynamically at runtime (must allocate a new string to interpolate). Only non-interpolated literals (`"hello"`, `'hello'`) become frozen. Use `+"string"` or `String.new` / `.dup` to get a mutable copy when needed.

- id: ruby-strings-quotes-03
  answer: |
    Single-quoted `'...'` is mostly literal: no interpolation, minimal escapes (`\\` and `\'` only). `"a\nb"` vs `'a\nb'` - in single quotes `\n` is two characters.
    Double-quoted `"..."` interprets escape sequences (`\n`, `\t`, `\x`, `\u`) and performs **interpolation**: `#{expression}` is evaluated and `to_s` inserted: `"hello #{name.upcase}"`, `"sum #{1+2}"`. Also `#{}` works inside double-quoted but not single-quoted (and inside `%Q`, heredocs, etc.).

- id: ruby-strings-percent-04
  answer: |
    `%w[...]` creates an array of Strings by splitting whitespace, without needing quotes/comma: `%w[foo bar baz] # => ["foo","bar","baz"]`. Supports interpolation if capital `%W`.
    `%i[...]` creates an array of Symbols similarly: `%i[foo bar] # => [:foo, :bar]` (and `%I` for interpolated).

- id: ruby-collections-hashdefault-01
  answer: |
    `Hash.new(0)` - default *value* is the same object `0` (which is immutable, safe). `h = Hash.new(0); h[:a] += 1` does assignment, and reading missing key returns `0` without storing the key.
    `Hash.new([])` shares one mutable array object as default - mutating it (`h[:a] << 1`) mutates the default for all missing keys but does NOT store the key (`h.keys` stays empty) - classic bug.
    `Hash.new { |h,k| h[k] = [] }` uses a block/default proc: on miss it executes block, creating a *new* array per key and storing it (`h[k] = []`). So `h[:a] << 1` correctly creates `h[:a] == [1]`. Use the block form whenever default is mutable.

- id: ruby-collections-kwargs-02
  answer: |
    In Ruby 2, a trailing Hash was automatically converted to keyword arguments. In Ruby 3.0 they were **fully separated**: passing a Hash positionally no longer satisfies keywords, and vice versa.
    ```ruby
    def foo(a:, b: 1); end
    h = {a: 1}
    foo(h)        # Ruby 2: ok, Ruby 3: ArgumentError
    foo(**h)      # Ruby 3: explode hash into keywords
    foo(a: 1)     # keywords
    foo({a: 1})   # positional Hash - ArgumentError if method expects keywords
    ```
    To pass a hash as keywords, double-splat: `foo(**hash)`. In definitions, `def foo(**kwargs)` captures keywords. `**` (and `**nil`/`**empty`) controls separation. Library code needs `ruby2_keywords` for compatibility.

- id: ruby-collections-splat-03
  answer: |
    `*` (splat) expands/collects **positional** arguments:
    - In definition: `def foo(*args)` collects extra positionals into array `args`; `def foo(a, *rest, b)` etc.
    - In call: `foo(*arr)` expands array into positional args: `arr=[1,2]; foo(*arr) == foo(1,2)`.
    `**` (double-splat) for **keyword** arguments / hashes:
    - In definition: `def foo(**kwargs)` collects extra keywords into hash; `def foo(a:, **rest)`.
    - In call: `foo(**h)` expands hash into keywords: `h={a:1}; foo(**h) == foo(a:1)`.
    Both work for assignment too: `a, *b = [1,2,3]`.

- id: ruby-collections-safenav-data-04
  answer: |
    `&.` safe-navigation: `obj&.foo` calls `foo` only if `obj` is non-nil; if `obj` is `nil`, evaluates to `nil` without calling and without NoMethodError. Shorthand for `obj && obj.foo` but handles chaining: `user&.profile&.avatar_url`. Returns nil if intermediate is nil.
    `Data.define` (Ruby 3.2) defines an immutable value class like `Struct` but stricter:
    ```ruby
    Point = Data.define(:x, :y)
    p = Point.new(x: 1, y: 2)  # keyword init, p.x, p.y, ==, hash, deconstruct, with...
    p.with(x: 9)               # returns copy with change
    ```
    Instances are frozen, equality is by value, and they are suitable as value objects.
