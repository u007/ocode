  - id: ruby-blocks-procvslambda-01
    answer: |
      The two main behavioral differences are: (1) Argument checking — a lambda enforces arity (raises ArgumentError if the wrong number of arguments are passed), while a Proc is lenient (missing arguments become nil, extra arguments are ignored). (2) Return semantics — a lambda returns from the lambda itself (the method continues executing), while a Proc returns from the enclosing method (the method exits at the point the Proc was called).

  - id: ruby-blocks-yield-02
    answer: |
      `yield` transfers control from the method to the block that was passed to the method, executing the block's code in the context of the method's local variables. `block_given?` returns true if a block was passed to the method. It protects against a LocalJumpError (or "no block given" error) that would occur if you call `yield` when no block was actually provided — you can use `if block_given?` to make block usage optional.

  - id: ruby-blocks-ampblock-03
    answer: |
      In `def m(&block)`, the `&` converts a passed block into a Proc object and binds it to the parameter `block`, making it a named variable you can call with `block.call` or pass along. In `arr.map(&:to_s)`, the `&` converts the symbol `:to_s` into a block by calling `to_proc` on the symbol, which returns a Proc that calls the method named by the symbol on its argument — so each element gets `to_s` called on it.

  - id: ruby-blocks-create-04
    answer: |
      Ways to create a lambda: `lambda { |x| x + 1 }`, `->(x) { x + 1 }` (stabby lambda), or `Proc.new { |x| x + 1 }` (in Ruby < 1.9, or with `lambda` keyword in modern Ruby). Ways to create a Proc: `Proc.new { ... }` or `proc { ... }`. To invoke: call `.call(args)` on the Proc/lambda, or use `.()` shorthand (`.()` is equivalent to `.call()`), or use `.yield(args)` (though `.yield` is less common). You can also use `Proc#===` in case statements.

  - id: ruby-modules-includeextendprepend-01
    answer: |
      `include` inserts the module into the ancestor chain just above the class itself, so the module's methods become instance methods of the class and are found after the class's own methods but before its superclass. `extend` adds the module's methods as singleton methods (class methods) of the receiver — it inserts the module into the singleton class's ancestor chain. `prepend` inserts the module at the very front of the ancestor chain (before the class itself), so the module's methods override the class's own methods — useful for decorating or wrapping existing methods.

  - id: ruby-modules-ancestors-super-02
    answer: |
      `super` searches the ancestor chain starting from the class/module *after* the current one where the method is defined, looking for a method with the same name. It does not call the method on the current class again — it calls the next matching method up the chain. Included modules are inserted into the ancestor chain between the class and its superclass, so `super` will find methods in an included module if the module is above the current class in the chain. The lookup order is: prepended modules → class itself → included modules → superclass → superclass's included modules → ... → Object → Kernel → BasicObject.

  - id: ruby-modules-namespace-03
    answer: |
      The other primary use of a module is as a namespace — to group related classes, methods, and constants under a common prefix to avoid naming collisions (e.g., `module MyApp; class User; end; end`). To define a callable module function, use `module_function` — it makes the instance method a private instance method and creates a public singleton (module-level) copy, so you can call it as `ModuleName.method_name`. Alternatively, you can define methods directly on the module's singleton class with `def self.method_name` or `class << self`.

  - id: ruby-modules-refinements-04
    answer: |
      Refinements solve the problem of monkey-patching's global scope — when you reopen a class, the changes are visible everywhere in the program, which can cause unexpected side effects and conflicts. Refinements allow you to modify a class within a limited scope (a module or file) using `refine` and activate it with `using`. The refinement is only active in the file/module where `using` is called, and does not leak into other parts of the program. This makes the changes localized and predictable.

  - id: ruby-objects-methodmissing-01
    answer: |
      `method_missing` is a hook that Ruby calls when it cannot find a method in the normal lookup path. You define it to handle calls to undefined methods — typically you inspect the method name and arguments, and either handle the call or call `super` to raise a NoMethodError. You must also define `respond_to_missing?` because `respond_to?` does not consult `method_missing` — without it, an object would claim it doesn't respond to a method that `method_missing` would actually handle, breaking duck typing and introspection. `respond_to_missing?` should return true for any method name that `method_missing` would handle.

  - id: ruby-objects-send-02
    answer: |
      `send` calls a method identified by a symbol or string name on an object, including private and protected methods — it bypasses visibility restrictions. `public_send` only calls public methods and will raise a NoMethodError if you try to call a private or protected method. They differ when the target method is non-public: `send` will invoke it, `public_send` will not. Use `public_send` when you want to respect encapsulation; use `send` only when you intentionally need to call non-public methods.

  - id: ruby-objects-attr-03
    answer: |
      `attr_reader :name` generates a getter method `def name; @name; end`. `attr_writer :name` generates a setter method `def name=(val); @name = val; end`. `attr_accessor :name` generates both. `define_method` is a metaprogramming method that defines an instance method dynamically at runtime using a block — it is useful when you need to create methods programmatically, when the method name is determined at runtime, or when you want to close over local variables in the method body (which `attr_*` cannot do).

  - id: ruby-objects-visibility-04
    answer: |
      `private` methods can only be called without an explicit receiver (except for setter methods which can use `self.` as receiver in Ruby 2.7+). `protected` methods can be called with an explicit receiver, but only if the receiver is an instance of the same class (or a subclass) as the caller. This means protected methods allow instances of the same class to call each other's methods, while private methods do not allow any explicit receiver. For example, in a `Person` class, a protected method could compare `self.age` with `other.age` where `other` is also a `Person`.

  - id: ruby-enumerable-include-01
    answer: |
      To make your own class include Enumerable, you must implement an `each` method that yields each element of your collection one at a time. In return, you get all of Enumerable's methods for free: `map`, `select`, `reject`, `reduce`, `include?`, `find`, `count`, `min`, `max`, `sort`, `to_a`, `group_by`, `partition`, `flat_map`, and many more — all implemented in terms of `each`.

  - id: ruby-enumerable-reduce-02
    answer: |
      `reduce` (also `inject`) combines all elements of a collection by applying a binary operation, accumulating a result. It takes an initial value (optional) and a block that receives the accumulator and each element, returning the new accumulator. Example: `[1,2,3].reduce(0) { |sum, n| sum + n }` → 6. `each_with_object` is a better fit when you are building up a mutable object (like a Hash or Array) and want to return that same object — it passes the object to the block and returns it at the end, avoiding the need to explicitly return the accumulator from each iteration. Example: `["a","b"].each_with_object({}) { |s, h| h[s] = s.upcase }`.

  - id: ruby-enumerable-lazy-03
    answer: |
      `.lazy` returns a lazy enumerator that defers evaluation of the chain until values are actually needed. Instead of computing the entire intermediate array at each step (e.g., `map` then `select` then `first`), it computes only as many elements as required. This matters for large or infinite sequences because it avoids creating massive intermediate arrays and allows you to work with infinite enumerators (e.g., `(1..Float::INFINITY).lazy.select(&:even?).first(10)`). Without `.lazy`, an infinite sequence would never terminate; with `.lazy`, you can take a finite portion.

  - id: ruby-enumerable-comparable-04
    answer: |
      To make instances of your class sortable and comparable, include the `Comparable` module and define the `<=>` (spaceship) operator method. `<=>` should return -1 if self is less than other, 0 if equal, 1 if greater, and nil if not comparable. Once `<=>` is defined and `Comparable` is included, you get `<`, `<=`, `==`, `>`, `>=`, `between?`, and `clamp` for free. For example: `class Person; include Comparable; attr_reader :age; def <=>(other); age <=> other.age; end; end`.

  - id: ruby-metaprogramming-singleton-01
    answer: |
      A singleton class (eigenclass) is a hidden, anonymous class that exists for every single object in Ruby. It is the object's personal class — methods defined on it are the object's singleton methods. When you call a method on an object, Ruby looks in the singleton class first, then the object's actual class, then ancestors. `class << self` inside a class body opens the singleton class of the class itself (the class's eigenclass), allowing you to define class methods (singleton methods on the class object) in a grouped block rather than repeating `def self.method_name` for each one.

  - id: ruby-metaprogramming-ivar-02
    answer: |
      `instance_variable_get(:@name)` returns the value of the instance variable `@name` on the object, bypassing any accessor methods. `instance_variable_set(:@name, value)` sets the instance variable directly. They are appropriate when you need to work with instance variables that don't have accessor methods, when the variable name is dynamic (determined at runtime), or when you intentionally want to bypass getter/setter logic. They should be used sparingly in production code as they break encapsulation, but are useful in metaprogramming, testing, and frameworks.

  - id: ruby-metaprogramming-definemethod-vs-mm-03
    answer: |
      `define_method` defines a real, named method at runtime — it appears in `methods`, is faster to call (no method_missing overhead), and is easier to debug. However, it must be called at class definition time (or explicitly on a class), and the method name must be known when the code runs. `method_missing` is a fallback that catches calls to any undefined method — it is more flexible (can handle arbitrary method names dynamically) but slower (every call goes through the full method lookup before failing), harder to debug, and doesn't appear in `methods`. Use `define_method` when you know the method names at definition time; use `method_missing` when you need to handle truly dynamic or unknown method names.

  - id: ruby-metaprogramming-classnew-04
    answer: |
      `Class.new` creates a new anonymous class. You can pass a superclass as an argument (`Class.new(ParentClass)`) and a block to define methods (`Class.new { def foo; end }`). Assigning it to a constant (`MyClass = Class.new`) gives the class a name (the constant name becomes the class's name) and makes it a proper named class that can be referenced, inherited from, and used with `is_a?`/`kind_of?` checks. Without assignment, the class is anonymous — its `name` returns nil until assigned to a constant.

  - id: ruby-error-standarderror-01
    answer: |
      Rescuing `Exception` is almost always wrong because `Exception` is the root of Ruby's exception hierarchy and includes non-standard exceptions that indicate serious, unrecoverable problems: `NoMemoryError`, `SignalException` (including `Interrupt` from Ctrl-C), `SystemExit` (from `exit`), `ScriptError`, and `SystemStackError`. Rescuing these can prevent the program from exiting cleanly, swallow user interrupts, mask fatal errors, and make debugging extremely difficult. `StandardError` and its subclasses represent expected, recoverable errors (like `ArgumentError`, `TypeError`, `RuntimeError`, `IOError`) — these are what you typically want to rescue.

  - id: ruby-error-ensure-retry-02
    answer: |
      `ensure` runs regardless of whether an exception was raised or not — it executes after the `begin` block completes normally, after a `rescue` block handles an exception, or even if an exception propagates uncaught. It is used for cleanup code (closing files, releasing resources). `retry` re-executes the `begin` block from the beginning — it can only be used inside a `rescue` clause. It is useful for retrying an operation that might fail transiently (e.g., network requests), but beware of infinite loops if the failure is persistent.

  - id: ruby-error-custom-03
    answer: |
      To define a custom exception, create a class that inherits from `StandardError` (or a more specific standard exception): `class MyError < StandardError; end`. To raise it: `raise MyError, "message"` or `raise MyError.new("message")`. Forms of `raise`: `raise` (re-raises the current exception in a rescue block), `raise "message"` (raises RuntimeError with the message), `raise MyError` (raises with no message), `raise MyError, "message"` (raises with message), `raise MyError.new("message")` (raises with an exception instance). You can also pass a backtrace as a third argument.

  - id: ruby-error-elserescue-04
    answer: |
      The `else` clause in a `begin/rescue/else/ensure` block runs only if no exception was raised — it executes after the `begin` block completes successfully and before `ensure`. It is useful for code that should only run when the `begin` block succeeds, keeping the `begin` block focused on the operation that might fail. For example, you might put the main logic in `begin`, error handling in `rescue`, success-only logic in `else`, and cleanup in `ensure`. The `else` clause is rarely used but helps separate success-path code from the operation that might raise.

  - id: ruby-strings-symbols-01
    answer: |
      A Symbol is an immutable, interned identifier — the same symbol always refers to the same object in memory (`:foo.object_id` is always the same). Symbols are used as method names, hash keys, and identifiers. A String is a mutable sequence of characters — each string literal creates a new object (even if the content is identical). Strings are used for text data. Key differences: identity (symbols are unique/interned, strings are not), mutability (symbols are frozen, strings are mutable), and memory (symbols are more memory-efficient for repeated use as keys/identifiers).

  - id: ruby-strings-frozen-02
    answer: |
      The `# frozen_string_literal: true` magic comment makes all string literals in the file frozen (immutable) by default. Attempting to mutate a frozen string raises a FrozenError. A subtlety about interpolated strings: even with the magic comment, string interpolation (`"hello #{name}"`) creates a new, unfrozen string each time it is evaluated — the magic comment only freezes literal strings without interpolation. So `"hello"` is frozen, but `"hello #{name}"` is not, because interpolation involves runtime evaluation that produces a new string object.

  - id: ruby-strings-quotes-03
    answer: |
      Single-quoted strings (`'hello'`) are literal — no interpolation, no escape sequences (except `\\` and `\'`). Double-quoted strings (`"hello"`) support interpolation (`"hello #{name}"`) and escape sequences (`\n`, `\t`, `\"`, etc.). String interpolation is the embedding of Ruby expressions inside double-quoted strings using `#{}` — the expression is evaluated and its result is converted to a string and inserted. Example: `"1 + 1 = #{1 + 1}"` → `"1 + 1 = 2"`.

  - id: ruby-strings-percent-04
    answer: |
      `%w[...]` produces an array of strings, splitting on whitespace: `%w[foo bar baz]` → `["foo", "bar", "baz"]`. `%i[...]` produces an array of symbols, splitting on whitespace: `%i[foo bar baz]` → `[:foo, :bar, :baz]`. Both are useful for creating arrays of short strings/symbols without typing quotes and commas. There are also `%W[...]` (interpolated) and `%I[...]` (interpolated symbols) variants.

  - id: ruby-collections-hashdefault-01
    answer: |
      `Hash.new(0)` sets a default value of 0 — when you access a missing key, it returns 0 but does NOT store the key in the hash. `Hash.new { |h, k| h[k] = [] }` sets a default value using a block — when you access a missing key, the block is called, which stores an empty array for that key and returns it. The difference matters because with `Hash.new(0)`, the hash remains empty after accessing missing keys (the default is returned but not stored), while with the block form, the hash is modified (new keys are added). The block form is essential for patterns like `hash[key] << value` where you need the default to be a mutable object that gets stored.

  - id: ruby-collections-kwargs-02
    answer: |
      In Ruby 3.0, keyword arguments were fully separated from positional hash arguments. Previously (Ruby 2.x), a trailing hash could be implicitly converted to keyword arguments and vice versa. In Ruby 3.0, if a method accepts keyword arguments, you must pass them as keywords (`method(key: value)`), not as a positional hash (`method({key: value})`). To pass a hash as keywords, use the double-splat: `method(**hash)`. If a method accepts a positional hash, you must pass it as a positional argument. This separation prevents ambiguity and bugs where a hash was silently treated as keywords or vice versa.

  - id: ruby-collections-splat-03
    answer: |
      The splat `*` in a method definition collects remaining positional arguments into an array: `def m(*args)` captures all positional args. In a call, `*` expands an array into individual arguments: `m(*[1,2,3])` is like `m(1,2,3)`. The double-splat `**` in a method definition collects remaining keyword arguments into a hash: `def m(**opts)`. In a call, `**` expands a hash into keyword arguments: `m(**{a: 1})` is like `m(a: 1)`. Double-splat also captures keywords that don't match named parameters when the method doesn't accept `**kwargs`.

  - id: ruby-collections-safenav-data-04
    answer: |
      The safe-navigation operator `&.` calls a method on an object only if the object is not nil; if the object is nil, it returns nil without raising a NoMethodError. Example: `user&.address&.city` — if `user` is nil, returns nil; if `user` exists but `address` is nil, returns nil. `Data.define` (Ruby 3.2) creates immutable value objects — similar to `Struct` but immutable and with better performance. Example: `Point = Data.define(:x, :y)` creates a class with readers `x` and `y`, and `Point.new(1, 2)` creates an instance. Instances are frozen and cannot be modified after creation.
