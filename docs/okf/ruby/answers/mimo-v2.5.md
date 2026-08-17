```yaml
- id: ruby-blocks-procvslambda-01
  answer: |
    1. Argument handling: A lambda enforces strict arity (raises ArgumentError on wrong number of arguments), while a Proc relaxes arity — extra arguments are silently ignored, and a single array argument is split when multiple parameters are expected.
    2. Return behavior: A lambda's `return` exits only the lambda itself, returning its value to the caller. A Proc's `return` exits the enclosing method that the Proc is defined in, acting like a non-local return.

- id: ruby-blocks-yield-02
  answer: |
    `yield` passes control (and any arguments given to the method) to the block supplied by the caller. If no block is given, `yield` raises a LocalJumpError. `block_given?` returns true only when a block is attached to the current method call, allowing you to guard the `yield` and avoid the error — or provide a default value/behavior when no block is supplied.

- id: ruby-blocks-ampblock-03
  answer: |
    In a parameter list (`def m(&block)`), `&` converts the attached block into a Proc object and assigns it to the parameter. It also strips leading underscores from the block name to name the local variable.

    In a call (`arr.map(&:to_s)`), `&` attempts to convert the object to a block: if the object is already a Proc it's used directly, otherwise it calls `to_proc` on it. `Symbol#to_proc` returns a Proc that sends the symbol as a method name to the yielded argument, so `&:to_s` effectively becomes `{ |x| x.to_s }`.

- id: ruby-blocks-create-04
  answer: |
    Create a lambda:
    - `my_lambda = lambda { |x| x + 1 }`
    - `my_lambda = -> (x) { x + 1 }`  (stabby lambda)

    Invoke a Proc/lambda:
    - `my_proc.call(args)` / `my_proc.[]` / `my_proc.yield(args)` / `my_proc.(args)` — all equivalent.
    - You can also convert a Proc to a block and call it via `&`: `my_method(&my_proc)`.

- id: ruby-modules-includeextendprepend-01
  answer: |
    - `include`: Inserts the module's methods as **instance methods** of the class. The module appears in the class's `ancestors` chain **above** the class itself (between superclass and class). Instance methods are available to objects of the class.
    - `extend`: Adds the module's methods as **class/module methods** (singleton methods) of the class. The module does **not** appear in the `ancestors` chain; it only appears in the singleton class's ancestors. Methods are called on the class itself (or on individual objects via `obj.extend`).
    - `prepend`: Inserts the module's methods **before** the class in the ancestor chain (module ancestors appear lower-numbered). This means the prepended module's methods take precedence over the class's own methods, useful for method overriding/wrapping while still being able to call `super`.

- id: ruby-modules-ancestors-super-02
  answer: |
    Ruby looks up a method by walking the **ancestor chain**: the class itself, then prepended modules, then included modules, then the superclass, then its included modules, all the way up to BasicObject. `super` (or `super(args)`) calls the next method with the same name found along that chain — the first ancestor "above" the current class/module that defines it. If you call `super` with no arguments, it forwards the same arguments as the current method; `super()` sends no arguments.

- id: ruby-modules-namespace-03
  answer: |
    The other primary use is as a **namespace** — grouping related constants, classes, and methods under a name to avoid polluting the global namespace (e.g., `module MyGem`). To define a callable module-level function, use `module_function` which makes the method callable as `MyModule.method_name` (and also makes it private when included).

- id: ruby-modules-refinements-04
  answer: |
    Refinements solve the problem of **monkey-patching** — reopening a class and redefining its methods globally, which affects all code in the process and can cause conflicts. Refinements let you temporarily override or add methods to a class but only within a limited lexical scope: activated by `using ModuleName` at the top of a file or class body, and only that code sees the refined methods. Outside that scope the original methods remain unchanged.

- id: ruby-objects-methodmissing-01
  answer: |
    `method_missing(symbol, *args)` is invoked on an object when you call a method that doesn't exist and isn't found through the normal lookup chain. You receive the method name and its arguments.

    You must also define `respond_to_missing?(symbol, include_private = false)` to return true when you handle a method in `method_missing`. This is necessary because standard `respond_to?` and `respond_to?(:some_method)` would return false for the dynamically handled method, breaking introspection, documentation tools, and code that relies on `respond_to?` before calling a method.

- id: ruby-objects-send-02
  answer: |
    Both call a method by name dynamically. `send(name, *args)` calls the method regardless of its visibility — it can invoke private methods. `public_send(name, *args)` respects visibility and raises a NoMethodError if the method is private or protected. Use `public_send` when you want safer dispatch that respects encapsulation; use `send` when you intentionally need to call a private method.

- id: ruby-objects-attr-03
  answer: |
    - `attr_reader(:x)` generates an instance method `x` that returns `@x`.
    - `attr_writer(:x)` generates an instance method `x=(val)` that sets `@x`.
    - `attr_accessor(:x)` generates both `x` and `x=`.
    - `define_method(:name) { |arg| ... }` dynamically defines an instance method at runtime using a block. It's useful when the method name or body is determined at runtime (metaprogramming).

- id: ruby-objects-visibility-04
  answer: |
    - **private** methods cannot be called with an explicit receiver at all (not even `self`). They can only be called implicitly (i.e., `foo` inside the object's own methods, where `self` is the implicit receiver).
    - **protected** methods can be called with an explicit receiver, but **only** if the receiver is the same class (an instance of the same class). This allows objects of the same class to compare internal state (e.g., `self <=> other` calling `other.salary`) while keeping the method hidden from the outside world.

- id: ruby-enumerable-include-01
  answer: |
    You must implement the `each` method, which yields each element to a block. Once `include Enumerable` is mixed in, you get all of the Enumerable methods for free: `map`, `select`, `reject`, `reduce`, `find`, `any?`, `all?`, `sort`, `min`, `max`, `to_a`, `flat_map`, `group_by`, and many others.

- id: ruby-enumerable-reduce-02
  answer: |
    `reduce` (aliased as `inject`) accumulates a single value by applying a block to an accumulator and each element. You can pass an initial value: `[1,2,3].reduce(0) { |sum, n| sum + n }`. If no initial value, the first element is used as the initial accumulator.

    `each_with_object` is often better when the block mutates and returns the same object each iteration — the accumulator is passed as the second block parameter and doesn't require returning it explicitly: `[1,2,3].each_with_object([]) { |n, arr| arr << n }`. This avoids accidentally returning `nil` (the value of an assignment) as the accumulator.

- id: ruby-enumerable-lazy-03
  answer: |
    `.lazy` wraps an Enumerable in an `Enumerator::Lazy` object, deferring evaluation so each element is computed only when needed (on demand). Without `.lazy`, methods like `map`, `select`, etc. build an intermediate array before the next step processes it. With `.lazy`, the chain is evaluated one element at a time through the entire pipeline. This matters for large sequences (avoids allocating huge intermediate arrays) and infinite sequences (you can call `.first(n)` without generating everything).

- id: ruby-enumerable-comparable-04
  answer: |
    Include the `Comparable` module and define the `<=>` (spaceship) operator on your class, which returns -1, 0, or 1 (or `nil` for incomparable). `Comparable` then provides `==`, `!=`, `<`, `<=`, `>`, `>=`, `between?`, and `clamp`. Example:
    ```ruby
    include Comparable
    def <=>(other) <=> other.age end
    ```

- id: ruby-metaprogramming-singleton-01
  answer: |
    A singleton class (eigenclass/virtual class) is an anonymous class attached to a single object that holds methods defined specifically for that object. Each object has its own singleton class.

    `class << self` inside a class body opens the **class's singleton class** (since `self` there is the class itself). Methods defined inside that block become class-level methods (analogous to `def self.method_name`). You can also use `class << obj` on any object to define methods only on that particular instance.

- id: ruby-metaprogramming-ivar-02
  answer: |
    `instance_variable_get(:@name)` reads the value of an instance variable by symbol name, bypassing any `attr_reader` or accessor. `instance_variable_set(:@name, value)` writes directly to the instance variable. They're appropriate for metaprogramming where the variable name isn't known at coding time (e.g., serialization frameworks, ORMs mapping columns), and for testing/debugging. They should be avoided in ordinary code because they bypass encapsulation and make intent harder to follow.

- id: ruby-metaprogramming-definemethod-vs-mm-03
  answer: |
    - `define_method` creates a real method that the normal method-lookup finds. It has predictable performance, works with `super`, and appears in introspection (methods list). But you must know the method name and can't handle undefined methods.
    - `method_missing` is invoked only when no existing method matches. It's flexible for handling entirely dynamic or virtual method names, but it's slower, bypasses `super` chains, confuses `respond_to?` unless `respond_to_missing?` is also defined, and makes debugging harder.

    Trade-off: `define_method` for known, enumerable method names (faster, cleaner). `method_missing` for truly virtual methods where enumeration is impractical.

- id: ruby-metaprogramming-classnew-04
  answer: |
    `Class.new { ... }` creates an anonymous class (not assigned to any constant). You can open a block to add methods immediately. Assigning it to a constant (`MyClass = Class.new { ... }`) gives the class a name, makes it reachable, and lets it appear in stack traces and introspection with a meaningful name. Without a constant, it remains anonymous and can't be referenced by name elsewhere.

- id: ruby-error-standarderror-01
  answer: |
    Rescuing `Exception` catches *everything* including `SystemExit`, `SignalException` (Ctrl+C), `NoMemoryError`, `LoadError`, `Interrupt`, and other system-level errors. This can prevent the program from terminating when the user presses Ctrl+C, mask out-of-memory conditions, or hide load failures. `StandardError` is the common base class for application-level errors and leaves system-level exceptions alone, which is almost always what you want.

- id: ruby-error-ensure-retry-02
  answer: |
    `ensure` runs **always** — whether the `begin` block completes normally, raises an exception (even one you didn't rescue), or you return/break from it. It's used for cleanup (closing files, releasing locks). `retry` re-executes the entire `begin` block from the start, which is useful for transient failures (e.g., retrying a network call). Be careful to have a retry limit to avoid infinite loops.

- id: ruby-error-custom-03
  answer: |
    Define a custom exception by inheriting from `StandardError` (or `RuntimeError`):
    ```ruby
    class MyError < StandardError; end
    ```

    Forms of `raise`:
    - `raise MyError, "message"` — raises with a class and message.
    - `raise "message"` — raises `RuntimeError` (the default).
    - `raise MyError` — raises with no message.
    - `raise` (bare) — re-raises the current exception if inside a rescue, otherwise raises `RuntimeError`.

- id: ruby-error-elserescue-04
  answer: |
    The `else` clause runs **only if no exception was raised** in the `begin` block (or method body). It's useful for separating the "happy path" logic from the setup, especially when you want to avoid accidentally rescuing errors raised inside the happy path logic (since code after `rescue` but before `else` is still part of the rescue-handling context). Pattern:
    ```ruby
    begin
      # risky code
    rescue SomeError
      # handle error
    else
      # only runs if begin succeeded without exception
    ensure
      # always runs
    end
    ```

- id: ruby-strings-symbols-01
  answer: |
    - **Identity**: Symbols are interned — each unique symbol name corresponds to exactly one object in memory (`:foo.equal?(:foo)` is true). Strings are not interned by default; two different `"foo"` objects may be distinct (`"foo".equal?("foo")` is false unless one is `freeze`d).
    - **Mutability**: Symbols are immutable by design. Strings are mutable (you can `gsub!`, `replace`, etc.) unless you `freeze` them.
    - **Use**: Symbols are used as identifiers — hash keys, method names, attribute names, enum-like constants — because they're lightweight and equality checks are fast. Strings are used for text data that may change or needs manipulation.

- id: ruby-strings-frozen-02
  answer: |
    `# frozen_string_literal: true` tells Ruby to freeze all string literals in that file — they become immutable. Attempting to mutate a frozen string raises `FrozenError`. This is a performance optimization (Ruby can reuse frozen string objects) and a safety measure (prevents accidental mutation).

    The subtlety with interpolated strings (`"Hello #{name}"`): the interpolated result is a **new, mutable** string object, even when `frozen_string_literal: true` is active. So the resulting string from interpolation is not frozen, despite the comment.

- id: ruby-strings-quotes-03
  answer: |
    - **Single-quoted** (`'hello'`): Minimal processing. The only escape sequences recognized are `\\` (literal backslash) and `\'` (literal single quote). No interpolation occurs. Faster for strings that need no escaping.
    - **Double-quoted** (`"hello"`): Supports interpolation (`#{expr}`), full escape sequences (`\n`, `\t`, `\x41`, `\u0041`, etc.).
    - **String interpolation** (`"Hello #{name}"`): Replaces `#{...}` with the result of evaluating the expression inside, converted to a string via `to_s`.

- id: ruby-strings-percent-04
  answer: |
    - `%w[foo bar baz]` creates an **Array of strings**: `["foo", "bar", "baz"]`.
    - `%i[foo bar baz]` creates an **Array of symbols**: `[:foo, :bar, :baz]`.
    Both skip whitespace splitting and respect quoting if needed (`%w[one\ two]` gives `["one two"]`).

- id: ruby-collections-hashdefault-01
  answer: |
    - `Hash.new(0)` returns `0` for any missing key but does **not** store it. `h[:missing]` returns `0` but `h` remains empty — you must explicitly assign (`h[:missing] += 1`) to persist values.
    - `Hash.new { |h, k| h[k] = [] }` returns a new empty array for a missing key **and stores it** in the hash. So `h[:missing]` returns `[]` and also sets `h[:missing] = []`, meaning subsequent access gets the same array.

    The difference matters because with `Hash.new(0)`, `h[key] += 1` works but the default is never stored; with the block form, the default is stored and mutated in place, so `h[key] << item` accumulates correctly across calls.

- id: ruby-collections-kwargs-02
  answer: |
    Before 3.0, keyword arguments were automatically extracted from the last hash argument and vice-versa (blurred boundary). Ruby 3.0 **strictly separated** them:
    - A method calling `foo(key: val)` passes keyword arguments, not a hash.
    - A method calling `foo({key: val})` passes a positional hash argument.
    - Passing keyword arguments to a method that expects a positional hash now raises an error.

    To pass a hash as keyword arguments, you must use double-splat: `foo(**hash)`.

- id: ruby-collections-splat-03
  answer: |
    **In method definitions:**
    - `*args` collects all remaining positional arguments into an array.
    - `**kwargs` collects all remaining keyword arguments into a hash.
    - `*` alone (no name) is not valid in definitions but `*, ...` or just `**` alone is used for forwarding.

    **In method calls:**
    - `*array` expands an array into individual positional arguments (e.g., `foo(*[1,2,3])` is `foo(1,2,3)`).
    - `**hash` expands a hash into keyword arguments (e.g., `foo(**{a: 1})` is `foo(a: 1)`).
    - `*` and `**` can also be used together for forwarding: `def wrapper(*, **) end` or `foo(*, **)` to forward all args.

- id: ruby-collections-safenav-data-04
  answer: |
    **Safe-navigation operator (`&.`):** Calls the method on the left only if the receiver is **not nil**. If the receiver is nil, the entire expression short-circuits and returns nil. For example, `user&.address&.street` returns nil if `user` is nil, without raising NoMethodError. It replaces patterns like `user && user.address && user.address.street`.

    **`Data.define` (Ruby 3.2):** Creates an immutable, value-like class (similar to `Struct` but frozen by default) with keyword-initialized, equality-by-value semantics. Example:
    ```ruby
    Point = Data.define(:x, :y)
    p = Point.new(x: 1, y: 2)
    p.x  # => 1
    p.frozen?  # => true
    ```
    Instances have `==`, `hash`, `inspect`, and `deconstruct_keys` automatically.
```
