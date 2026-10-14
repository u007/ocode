# cat-jam-free — closed-book answers for ruby (opencode-go, 2026-10-14); answerer saw only _prompts/ruby.md
# Delivery: one `ocode run -m opencode-go/cat-jam-free -effort med` session, sheet attached via -f, empty scratch cwd;
# session audited in sqlite: 1 user + 1 assistant message, 0 tool calls.

- id: ruby-blocks-procvslambda-01
  answer: |
    A lambda checks its argument count (arity) and raises ArgumentError on mismatch, while a Proc is lenient and fills missing args with nil or ignores extras. A return inside a lambda returns from the lambda; a return inside a Proc returns from the enclosing method/scope.
- id: ruby-blocks-yield-02
  answer: |
    yield invokes the block passed to the method, forwarding any given arguments to that block. block_given? returns true only when a block was supplied; it prevents LocalJumpError from calling yield with no block.
- id: ruby-blocks-ampblock-03
  answer: |
    In def m(&block), & captures the passed block as a Proc object bound to block. In a call such as arr.map(&:to_s), & converts the Symbol :to_s to a Proc via to_proc, then passes it as the block to map.
- id: ruby-blocks-create-04
  answer: |
    Lambdas can be created with lambda { |x| ... } or ->(x) { ... }. Procs can be created with Proc.new { ... } or proc { ... }. Invoke any callable with call(args), the [] operator, or the .() operator; lambdas and procs can also be invoked via yield when passed as a block.
- id: ruby-modules-includeextendprepend-01
  answer: |
    include adds module methods as instance methods and inserts the module into the class ancestor chain just after the class. extend adds module methods as singleton/class methods by mixing into the receiver's singleton class. prepend inserts the module before the class in the ancestor chain, so its methods override the class methods while still allowing super to reach the class implementation.
- id: ruby-modules-ancestors-super-02
  answer: |
    super continues method lookup in the next ancestor after the module or class where the currently executing method was defined; included modules appear in that ancestor chain in insertion order. Plain super passes the received arguments, super() passes none, and super(...) passes explicit arguments.
- id: ruby-modules-namespace-03
  answer: |
    Besides mixins, modules are primarily namespaces for grouping constants, classes, and functions. A callable module function can be defined with def self.method_name, as a singleton method, or exposed with module_function so it works both as a module method and as a private instance method when included.
- id: ruby-modules-refinements-04
  answer: |
    Refinements avoid global monkey-patching by applying class changes only in a lexical scope. They are defined with refine ClassName, then activated with using ModuleName in the scope where the change should be visible.
- id: ruby-objects-methodmissing-01
  answer: |
    method_missing is called when an object receives a method it does not implement, allowing dynamic handling or delegation. respond_to_missing? must be defined so respond_to?, method caches, and introspection correctly report those dynamic methods.
- id: ruby-objects-send-02
  answer: |
    send invokes a named method, bypassing normal visibility, so it can call private or protected methods. public_send invokes only public methods. They differ when the target method is non-public.
- id: ruby-objects-attr-03
  answer: |
    attr_reader creates a getter, attr_writer creates a setter, and attr_accessor creates both. define_method dynamically defines a named method at runtime, usually from a block, for metaprogramming and generated APIs.
- id: ruby-objects-visibility-04
  answer: |
    Private methods may be called only with an implicit receiver, not with an explicit receiver such as self. Protected methods may be called with an explicit receiver when the receiver is an instance of the same class or subclass hierarchy.
- id: ruby-enumerable-include-01
  answer: |
    You must implement each, which yields each element. Including Enumerable then provides map, select, reject, reduce, any?, all?, sort-related methods when elements support comparison, and many other collection operations.
- id: ruby-enumerable-reduce-02
  answer: |
    reduce or inject combines elements into an accumulator, with an optional initial value. each_with_object is often better when accumulating into a mutable object such as a Hash or Array, because it yields that object with each element and returns it without manual accumulator reassignment.
- id: ruby-enumerable-lazy-03
  answer: |
    lazy returns a lazy Enumerator that evaluates chained transformations one element at a time rather than materializing intermediate collections. This matters for large or infinite sequences because it avoids unnecessary work and memory and allows chains terminated by take or first.
- id: ruby-enumerable-comparable-04
  answer: |
    Include Comparable and define <=> returning -1, 0, or 1. This enables <, <=, ==, >, >=, between?, and clamp, and allows instances to be sorted.
- id: ruby-metaprogramming-singleton-01
  answer: |
    A singleton class, or eigenclass, is the per-object class that holds methods defined for one specific object. In a class body, class << self opens that class object's singleton class, so methods defined there become class methods.
- id: ruby-metaprogramming-ivar-02
  answer: |
    instance_variable_get and instance_variable_set read or write an instance variable by its symbol or string name. They are appropriate for reflection, serialization, frameworks, debugging, or metaprogramming, but they bypass ordinary encapsulation.
- id: ruby-metaprogramming-definemethod-vs-mm-03
  answer: |
    define_method creates real methods, giving better performance, caching, arity handling, and respond_to? support. method_missing handles arbitrary or unknown method names flexibly and is useful for proxies and delegation, but it is slower, less discoverable, and harder to debug.
- id: ruby-metaprogramming-classnew-04
  answer: |
    Class.new creates a new class, optionally subclassing a given superclass. Assigning it to a constant gives the class a name and places it in a namespace; an unassigned class remains anonymous.
- id: ruby-error-standarderror-01
  answer: |
    Rescuing Exception catches system-level and control-flow exceptions such as SystemExit, Interrupt, NoMemoryError, and SignalException. Rescuing it can hide serious failures and prevent normal process control, so normal error handling should rescue StandardError instead.
- id: ruby-error-ensure-retry-02
  answer: |
    ensure runs whenever the begin block exits, whether by normal completion or an exception, and is used for cleanup. retry, valid inside rescue, restarts the whole begin block from the beginning; it should be guarded by a changing condition to avoid infinite loops.
- id: ruby-error-custom-03
  answer: |
    Define a custom exception as a subclass of StandardError, such as class MyError < StandardError; end. raise can take a message string, an exception class, a class with a message, an exception instance, or an existing exception with a replacement message and backtrace.
- id: ruby-error-elserescue-04
  answer: |
    In a method, rescue may be written without begin. An optional else clause runs only when no exception was raised, keeping the success path separate from rescue handling before any ensure cleanup.
- id: ruby-strings-symbols-01
  answer: |
    Strings are mutable sequences of characters and each literal normally creates a new object. Symbols are immutable, interned identifiers, so equal names refer to the same object. Symbols are typically used for names, keys, and status values; strings are used for text and data.
- id: ruby-strings-frozen-02
  answer: |
    The # frozen_string_literal: true magic comment makes string literals in that file frozen by default, reducing allocations and preventing mutation. A subtlety is that interpolated strings are also frozen, so mutation requires dup or a unary plus string.
- id: ruby-strings-quotes-03
  answer: |
    Single-quoted strings are mostly literal, with only \\ and \' escapes; they do not support interpolation. Double-quoted strings support escape sequences and interpolation using #{...}, which evaluates an expression and inserts its string representation.
- id: ruby-strings-percent-04
  answer: |
    %w[...] produces an array of strings, splitting on whitespace, while %i[...] produces an array of symbols.
- id: ruby-collections-hashdefault-01
  answer: |
    Hash.new(0) returns the same default value 0 for every missing key, which is appropriate for counters but unsafe if the default value is mutable. Hash.new { |h, k| h[k] = [] } computes a fresh value per missing key and assigns it, giving each key its own distinct object.
- id: ruby-collections-kwargs-02
  answer: |
    Ruby 3.0 no longer automatically converts a trailing Hash argument into keyword arguments, or vice versa. To pass a Hash as keywords, use the double splat operator: foo(**hash).
- id: ruby-collections-splat-03
  answer: |
    In parameters, * collects excess positional arguments into an Array and ** collects keyword arguments into a Hash. In calls, * expands an array into positional arguments and ** expands a Hash into keyword arguments.
- id: ruby-collections-safenav-data-04
  answer: |
    The safe-navigation operator &. calls the method only when the receiver is not nil and returns nil otherwise. Data.define, available in Ruby 3.2, creates an immutable value object class similar to Struct, suitable for frozen data records.
