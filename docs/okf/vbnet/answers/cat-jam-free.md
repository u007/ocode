# cat-jam-free — closed-book answers for vbnet (opencode-go, 2026-10-14); answerer saw only _prompts/vbnet.md
# Delivery: one `ocode run -m opencode-go/cat-jam-free -effort med` session, sheet attached via -f, empty scratch cwd;
# session audited in sqlite: 1 user + 1 assistant message, 0 tool calls.

- id: vbnet-syntax-01
  answer: |
    Declare a variable with `Dim <name> As <Type>` (or an access-level keyword such as Public/Private). Statements are terminated by the end of line (newline); multiple statements on one line can be separated with a colon. A statement can be continued onto the next line with the line-continuation character `_` at the end of the line; modern VB also supports implicit line continuation after commas, operators, opening brackets, and some keywords.
- id: vbnet-syntax-02
  answer: |
    Option Explicit requires variables to be declared. Option Infer lets the compiler infer local variable types from their initializers. Option Strict controls unsafe type conversions: when On, implicit narrowing conversions, late binding, and explicit type narrowing errors become compile-time errors. Option Strict On matters because it catches type errors early, prevents accidental data loss, and improves performance/reliability.
- id: vbnet-syntax-03
  answer: |
    A Class is an instantiable blueprint that can have instance and shared members, inheritance, and interfaces. A Module is a singleton container whose members are implicitly Shared; it cannot be instantiated and is typically used for shared utilities, extension methods, and global-like helpers.
- id: vbnet-syntax-04
  answer: |
    A Sub performs an action and returns nothing. A Function returns a value of its declared type. A Function returns a value by using a `Return` expression, or by assigning to the function name before exiting.
- id: vbnet-props-01
  answer: |
    An auto-implemented property declares `Property Name As Type` and the compiler generates the backing field and simple Get/Set accessors. A full property declares a private backing field and implements Get/Set blocks manually, allowing validation, computed behavior, or extra logic.
- id: vbnet-props-02
  answer: |
    The backing field is compiler-generated (commonly seen as a private field named like `_PropertyName` or a generated hidden name). An auto-property can be initialized with a default value in its declaration, for example `Property Foo As String = "default"`; otherwise it gets the type's default.
- id: vbnet-props-03
  answer: |
    Use `ReadOnly Property ... As Type` or `WriteOnly Property ... As Type`. A ReadOnly or WriteOnly property cannot include the disallowed accessor. An auto-implemented property can be ReadOnly, for example `ReadOnly Property Foo As Integer = 5`, with no writable setter.
- id: vbnet-props-04
  answer: |
    A default property is declared with the `Default` keyword and has parameters, allowing an instance to be indexed like an array/collection. A class can have only one default property, and it must be parameterized; care is needed because default members can create ambiguity.
- id: vbnet-null-01
  answer: |
    `Nothing` is the default value for any type. For a reference type it means a null reference. For a value type it means the zero-initialized default value, such as 0 for numbers, False for Boolean, or the default struct state.
- id: vbnet-null-02
  answer: |
    Declare a nullable value type with `Dim x As Integer?` or `Nullable(Of Integer)`. Check it with `x.HasValue`; read the value with `x.Value` after confirming it has a value; use `GetValueOrDefault()` or a nullable-aware expression to read safely.
- id: vbnet-null-03
  answer: |
    `If(a, b)` is a language operator: the two-argument form returns `a` if it is not Nothing, otherwise `b`; the three-argument form selects between values. It short-circuits. `IIf(a, b, c)` is an ordinary function, so all arguments are evaluated before the call and it returns Object. `If` is preferred for null-coalescing and conditional selection because it avoids evaluating unwanted expressions and preserves stronger typing.
- id: vbnet-null-04
  answer: |
    `Is Nothing`/`IsNot Nothing` test reference identity and are safe and idiomatic. `= Nothing` invokes value equality and can involve implicit conversions or unexpected equality semantics, so it is not the correct null-reference test.
- id: vbnet-errors-01
  answer: |
    Structure: `Try` protected code, one or more `Catch` blocks, optional `Finally`, then `End Try`. The Finally block runs whenever execution leaves the Try or a matching Catch block, whether normally or because of an exception, and is used for cleanup such as closing resources.
- id: vbnet-errors-02
  answer: |
    A `Catch ... When condition` filter activates the handler only when both the exception type matches and the filter expression is true. It is preferable to catching and then re-checking because it avoids entering an inappropriate handler and can preserve the original exception flow more selectively.
- id: vbnet-errors-03
  answer: |
    `Throw` rethrows the current exception and preserves its original stack trace. `Throw ex` throws the same exception object but resets the stack trace to the current throw location, hiding where the error originally occurred. Use `Throw`; wrap with `Throw New ... (innerException:=ex)` only when adding context.
- id: vbnet-errors-04
  answer: |
    Legacy `On Error GoTo` uses labels and the global `Err` object for control flow and error information. Structured Try/Catch is preferred because it is type-based, scoped, clearer, supports Finally cleanup, and avoids brittle GoTo-driven error handling.
- id: vbnet-linq-01
  answer: |
    Shape: `Dim query = From item In source Where item.Field = value Select item.Projection`. Keywords include `From`, `In`, and optionally `Where` and `Select`.
- id: vbnet-linq-02
  answer: |
    Use `Group By key Into ... Group` plus aggregate functions. Form: `From x In source Group By x.Key Into Group, Count(), Sum(x.Amount)`; grouped results can be projected by naming aggregates, such as `Into Total = Sum(x.Amount)`.
- id: vbnet-linq-03
  answer: |
    `Aggregate` is a query keyword that computes an aggregate result over the source, for example `Aggregate n In numbers Into Sum(n)`. Unlike `From`, which introduces a range variable for filtering/projection/grouping, `Aggregate` produces a scalar aggregate expression.
- id: vbnet-linq-04
  answer: |
    Deferred execution means the query is not evaluated until it is enumerated, such as in a For Each loop or when a terminal method is called. Force immediate execution with methods like `ToList()`, `ToArray()`, `Count()`, `First()`, or `Single()`.
- id: vbnet-events-01
  answer: |
    A field declared with `WithEvents` refers to an object that exposes events. A method can then handle one of its events with a `Handles WithEventsVariable.EventName` clause; the compiler generates the delegate wiring, and access requires compatible parameters and signatures.
- id: vbnet-events-02
  answer: |
    `AddHandler` attaches a delegate to an event; `RemoveHandler` detaches it. `AddressOf` creates a delegate referring to a method. Use them instead of `Handles` for dynamic subscriptions, non-WithEvents sources, runtime conditions, lambdas, or when handlers must be added/removed manually.
- id: vbnet-events-03
  answer: |
    Declare `Event EventName(parameters)` in a class, then raise it with `RaiseEvent EventName(arguments)`. Handlers must match the event's delegate signature and receive the same parameters, either wired with `Handles` or `AddHandler ... AddressOf Handler`.
- id: vbnet-events-04
  answer: |
    Use WithEvents/Handles for simple, stable, compile-time-checked wiring, especially designer-generated code. Use AddHandler/RemoveHandler when subscriptions are dynamic, need cleanup to avoid leaks, involve multiple sources/instances, lambdas, or sources not declared as WithEvents.
- id: vbnet-oop-01
  answer: |
    `Inherits` declares one base class; VB does not support multiple class inheritance, so only one Inherits is allowed. `Implements` declares interfaces implemented by the type; multiple interfaces can be listed, separated by commas.
- id: vbnet-oop-02
  answer: |
    `Overridable` marks a base member as overridable. `Overrides` overrides that base member in a derived class. `MustOverride` declares an abstract member requiring implementation. `MustInherit` marks a class as abstract. `NotOverridable` prevents a member from being overridden further.
- id: vbnet-oop-03
  answer: |
    A `Shared` member belongs to the type itself and has one copy for all instances; it is accessed through the type name. An instance member belongs to each object instance and is accessed through an instance reference. Shared members cannot use `Me`.
- id: vbnet-oop-04
  answer: |
    `Me` is the current instance. `MyBase` provides access to the immediate base class implementation, often to call an overridden base method. `MyClass` refers to the current instance but resolves members to the current class definition rather than through virtual dispatch. `MyClass` differs from `Me` when a method is overridden: `MyClass` calls the current class version while `Me` follows runtime polymorphism.
- id: vbnet-convarr-01
  answer: |
    `CType` is a general compile-time conversion operation supporting widening/narrowing and user-defined conversions. `DirectCast` performs a direct type cast with no conversion and can be faster, but requires exact type compatibility. `TryCast` is like DirectCast for reference types and returns Nothing instead of throwing when the cast is impossible.
- id: vbnet-convarr-02
  answer: |
    A widening conversion cannot lose information and is allowed implicitly. A narrowing conversion can lose data or precision and, under Option Strict On, cannot be done implicitly; it requires an explicit conversion such as CType, DirectCast, or TryCast. Option Strict On therefore catches potentially unsafe narrowing at compile time.
- id: vbnet-convarr-03
  answer: |
    VB.NET arrays are 0-based. `Dim a(n)` declares an array whose valid indices are 0 through n, giving length n + 1. Resize while preserving contents with `ReDim Preserve` or `Array.Resize`.
- id: vbnet-convarr-04
  answer: |
    `CInt`, `CStr`, and `CDbl` are type-specific conversion functions. Use `&` rather than `+` for string concatenation because `&` always concatenates and converts operands to strings, whereas `+` may attempt numeric addition, leading to type mismatch or unexpected results under Option Strict On.
