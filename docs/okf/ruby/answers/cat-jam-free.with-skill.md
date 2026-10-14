# cat-jam-free — WITH-SKILL closed-book answers for ruby (opencode-go, 2026-10-14)
# Delivery: derived/ruby.cat-jam-free.SKILL.md body (front matter stripped) prepended to _prompts/ruby.md; one
# `ocode run -m opencode-go/cat-jam-free -effort med` session, isolated HOME with every tool denied, cwd holding only the sheet;
# session audited in sqlite: 1 user + 1 assistant message, 0 tool calls.

- id: ruby-blocks-procvslambda-01
  answer: |
    The two main differences: (1) arity - Procs are lenient about argument counts, while lambdas enforce strict arity. (2) return semantics - return in a Proc returns from the enclosing method; return in a lambda returns from the lambda itself.

- id: ruby-blocks-yield-02
  answer: |
    yield invokes the block passed to the current method, forwarding any arguments. block_given? returns whether the method was called with a block; checking first avoids LocalJumpError when no block is supplied.

- id: ruby-blocks-ampblock-03
  answer: |
    In `def m(&block)`, `&` captures the implicit block as a Proc assigned to `block`. In `arr.map(&:to_s)`, `&` converts the object (here a Symbol) via `to_proc` into a block argument.

- id: ruby-blocks-create-04
  answer: |
    Lambdas can be created with `lambda { ... }`, `-> { ... }`, or `lambda { |x| ... }`. Procs can be created with `Proc.new { ... }` or `proc { ... }`. Both are invoked via `.call`, `.(...)`, `[...]`, or `===`, and can be passed as blocks with `&`.

- id: ruby-modules-includeextendprepend-01
  answer: |
    include makes module methods available as instance methods and places the module in the ancestor chain. extend makes module methods available as singleton/class methods. prepend places the module before the class in method lookup, so it can override or wrap the class's own methods.

- id: ruby-modules-ancestors-super-02
  answer: |
    super calls the next implementation along the method resolution order. Included modules are inserted into the ancestor chain, so lookup traverses the class, then relevant modules, then superclasses. `super` without parentheses forwards the current arguments; `super()` passes none.

- id: ruby-modules-namespace-03
  answer: |
    A module is also used as a namespace for constants, classes, and functions. A callable module function can be defined with `module_function :method_name` (often after a private method definition), or by defining `def self.method_name`.

- id: ruby-modules-refinements-04
  answer: |
    Refinements provide lexically scoped changes to classes, avoiding global monkey-patching and name conflicts. They are defined in a module with `refine(Class) { ... }` and activated with `using ModuleName` in the desired lexical scope.

- id: ruby-objects-methodmissing-01
  answer: |
    method_missing is called when a invoked method is not found, allowing dynamic behavior. It should handle only recognized names and call super otherwise so unknown calls still raise NoMethodError. respond_to_missing? must be defined to keep respond_to? and method lookup honest for handled names.

- id: ruby-objects-send-02
  answer: |
    send calls a method by name and can invoke private and protected methods. public_send calls only public methods. They differ in visibility enforcement: public_send raises for non-public methods.

- id: ruby-objects-attr-03
  answer: |
    attr_reader generates a getter, attr_writer generates a setter, and attr_accessor generates both. define_method defines a method dynamically at runtime, useful for metaprogramming.

- id: ruby-objects-visibility-04
  answer: |
    Methods are public by default. private methods cannot be called with an explicit receiver other than self; since Ruby 2.7, self.foo is allowed for any private method. protected methods may be called with an explicit receiver, but only within an instance method of the same class or subclass.

- id: ruby-enumerable-include-01
  answer: |
    To include Enumerable, implement `each` yielding each element. You then get traversal, searching, filtering, reduction, and transformation methods such as map, select, reduce, min, max, and include?.

- id: ruby-enumerable-reduce-02
  answer: |
    reduce/inject folds an enumerable into one value, with the block receiving an accumulator and element. each_with_object is better when mutating or building an object, because it returns the object and does not require returning the accumulator each iteration.

- id: ruby-enumerable-lazy-03
  answer: |
    .lazy returns a lazy enumerator that evaluates an enumerable chain one element at a time. This avoids building large intermediate collections and makes infinite or very large sequences usable with terminating methods like take, first, or find.

- id: ruby-enumerable-comparable-04
  answer: |
    Include Comparable and implement `<=>`, returning -1, 0, 1, or nil. This enables `<`, `>`, `<=`, `>=`, `==`, `between?`, and `clamp`, with nil indicating incomparability.

- id: ruby-metaprogramming-singleton-01
  answer: |
    A singleton class (eigenclass) holds singleton methods for an object. Inside a class body, `class << self` opens that class's singleton class, so methods defined there become class methods.

- id: ruby-metaprogramming-ivar-02
  answer: |
    instance_variable_get and instance_variable_set read and write an instance variable by symbol or string name. They are appropriate for reflection and metaprogramming, but bypass normal encapsulation and should be used sparingly.

- id: ruby-metaprogramming-definemethod-vs-mm-03
  answer: |
    define_method creates real method entries ahead of time, improving introspection and performance for a known set of names. method_missing intercepts arbitrary unknown calls dynamically, but adds dispatch overhead and requires careful super and respond_to_missing? handling.

- id: ruby-metaprogramming-classnew-04
  answer: |
    Class.new creates an anonymous class, optionally with a superclass and block. Assigning it to a constant names the class, making its `name` reflect that constant and registering it normally.

- id: ruby-error-standarderror-01
  answer: |
    A bare rescue catches StandardError. Rescuing Exception is usually wrong because it also catches serious system-level errors such as NoMemoryError, SystemExit, Interrupt, and SignalException, which can prevent normal process control and termination.

- id: ruby-error-ensure-retry-02
  answer: |
    ensure runs whenever the begin/rescue block exits, whether normally, by exception, or via return. retry, used inside a rescue clause, restarts the begin block from the beginning and can easily create infinite loops if unguarded.

- id: ruby-error-custom-03
  answer: |
    Define a custom exception by subclassing StandardError or another exception class. raise can take a message string (creating RuntimeError), an exception class, a class plus message, an exception instance, or be called bare to re-raise the current exception.

- id: ruby-error-elserescue-04
  answer: |
    else runs only if the begin section completes without raising an exception. It separates success-path code from rescue handlers, unlike code after end, which always runs, and ensure, which also always runs.

- id: ruby-strings-symbols-01
  answer: |
    Symbols are immutable, interned identifiers with identity based on name; Strings are mutable text values and may be different objects with equal content. Symbols are commonly used for keys and identifiers, while Strings are used for text data.

- id: ruby-strings-frozen-02
  answer: |
    The `# frozen_string_literal: true` magic comment freezes string literals in the file, preventing mutation and enabling reuse. A subtlety is that interpolated string literals are also frozen fresh objects each evaluation, so mutating them still requires duplication.

- id: ruby-strings-quotes-03
  answer: |
    Single-quoted strings are mostly literal, handling only `\\` and `\'`. Double-quoted strings support backslash escapes and `#{...}` interpolation, which evaluates the expression and converts it to a string.

- id: ruby-strings-percent-04
  answer: |
    `%w[...]` produces an array of strings. `%i[...]` produces an array of symbols. Whitespace separates the elements.

- id: ruby-collections-hashdefault-01
  answer: |
    `Hash.new(0)` returns the same default value 0 for missing keys without storing them. `Hash.new { |h, k| h[k] = [] }` creates and stores a new empty array per missing key. This matters because a mutable shared default would be shared accidentally, while the block form gives each key its own object.

- id: ruby-collections-kwargs-02
  answer: |
    Ruby 3.0 no longer converts a trailing Hash into keyword arguments. To pass a hash as keywords, splat it explicitly with `method(**hash)`, typically with symbol keys.

- id: ruby-collections-splat-03
  answer: |
    In definitions, `*args` collects positional arguments into an array and `**opts` collects keyword arguments into a Hash. In calls, `*array` expands into positional arguments and `**hash` expands into keyword arguments.

- id: ruby-collections-safenav-data-04
  answer: |
    `&.` calls a method only when the receiver is not nil and otherwise returns nil. `Data.define` creates an immutable value class with positional and keyword initialization, introduced in Ruby 3.2.
