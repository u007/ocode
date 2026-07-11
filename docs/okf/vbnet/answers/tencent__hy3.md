# vbnet knowledge benchmark — answers
# Model: novita-ai/tencent/hy3   (provider-stripped id: tencent/hy3)
# CLOSED-BOOK via ocode run -dir <empty> (no corpus access). corpus_rev 1.

- id: vbnet-syntax-01
  answer: |
    Variable declaration uses the `Dim` keyword: `Dim x As Integer = 5` (the `As` clause gives the type, and an initializer is optional). Statements are terminated by the end of the line — there is no required statement terminator like a semicolon (a colon `:` can place multiple statements on one line). To continue a single logical statement across multiple physical lines you put a space followed by an underscore `_` at the end of the line (explicit line continuation); modern VB (VB 10/2010+) also allows implicit continuation at many natural break points (commas, operators, brackets), but `_` is the universal explicit mechanism.

- id: vbnet-syntax-02
  answer: |
    - `Option Explicit` controls whether variables must be declared before use; with it On, undeclared identifiers are compile errors (On is the default in new projects).
    - `Option Strict` controls implicit type conversions and late binding; with it On, implicit narrowing conversions (e.g. Long→Integer), late binding (calling members on Object without a known type), and implicit conversions to/from Object are disallowed and must be done explicitly with CType/DirectCast.
    - `Option Infer` controls local type inference: when On, `Dim x = 5` infers `Integer` from the initializer instead of requiring `As`.
    `Option Strict On` matters because it moves a whole class of runtime failures (silent data loss from narrowing, NullReference from late binding, invalid casts) to compile time, making code safer and more predictable.

- id: vbnet-syntax-03
  answer: |
    A `Module` is a static, non-instantiable type: it cannot be created with `New`, cannot inherit or be inherited (it is implicitly NotInheritable), cannot implement interfaces, and all of its members are implicitly `Shared`. A `Class` is an instantiable reference type that supports inheritance and interface implementation and can have both instance and Shared members. Use a Module for utility/helper functions and global-like procedures; use a Class when you need objects, state, polymorphism, or interfaces.

- id: vbnet-syntax-04
  answer: |
    A `Sub` performs an action and does not return a value; it is invoked as a statement. A `Function` returns a value to the caller. A Function returns its value either by assigning to the function's own name (`MyFunc = result`) or, preferably, with the `Return value` statement (`Return` also exits the function immediately). The return type is declared with `As` (`Function F() As Integer`).

- id: vbnet-props-01
  answer: |
    An auto-implemented property is declared as just `Public Property Name As String` (optionally with `= default`); the compiler automatically generates the private backing field and the default Get/Set accessors. A full (expanded) property explicitly declares a backing field (e.g. `Private _name As String`) and provides `Get`/`Set` blocks, which lets you add validation, computation, change notification, or access-level differences (e.g. Public Get, Private Set). The trade-off: auto-properties are concise but you cannot insert logic; full properties give full control.

- id: vbnet-props-02
  answer: |
    For an auto-implemented property `PropertyName`, the compiler-generated backing field is conventionally named `_PropertyName` (a leading underscore plus the property name) — though the exact mangled name is compiler-internal and you normally should not rely on it. You initialize an auto-property with a default value using an initializer on the declaration: `Public Property Name As String = "default"` (VB 2010+), which is applied in the constructor; otherwise assign in the constructor's `New`.

- id: vbnet-props-03
  answer: |
    A read-only property has only a `Get` accessor (`Public ReadOnly Property Name As String` / `Get ... End Get End Property`); a write-only property has only a `Set` accessor (`WriteOnly Property ... Set ... End Set`). A read-only auto-implemented property is allowed and must be given a value at declaration or in the constructor (`Public ReadOnly Property Name As String = "x"`); there is no write-only auto-property (you must use a full property for write-only).

- id: vbnet-props-04
  answer: |
    A default property is an indexed property that can be accessed without naming it, e.g. `obj(i)` instead of `obj.Item(i)`. The constraints are: it must have at least one parameter (it is always an indexed property), and a class may have at most one default property. It is declared with the `Default` keyword: `Default Public Property Item(index As Integer) As T`.

- id: vbnet-null-01
  answer: |
    `Nothing` is the default/uninitialized value of a type. For a reference type, `Nothing` means a null reference (no object). For a value type, `Nothing` means the type's default value — for Integer it is 0, for Boolean it is False, etc. — NOT a null reference (value types cannot be null unless wrapped in `Nullable(Of T)`). So testing a value type against `Nothing` checks for its default value, not for "no object."

- id: vbnet-null-02
  answer: |
    Declare a nullable value type with the `?` shorthand or `Nullable(Of T)`: `Dim x As Integer?` or `Dim x As Nullable(Of Integer)`. Check whether it has a value with `x.HasValue` (returns Boolean) or `x IsNot Nothing`. Read the value safely with `x.Value` after confirming `HasValue`, or use `x.GetValueOrDefault()` / `If(x, fallback)` to avoid an exception if it is `Nothing`.

- id: vbnet-null-03
  answer: |
    `IIf(condition, truePart, falsePart)` is a library function that evaluates BOTH the truePart and falsePart arguments eagerly (before choosing), so it always executes both sides — risky if an argument throws (e.g. dereferencing a null) or is expensive. The `If` operator (ternary `If(condition, truePart, falsePart)`, and the two-argument coalescing `If(value, fallback)`) is short-circuiting: it evaluates only the chosen branch. `If` is preferred because short-circuiting avoids NullReferenceException and wasted work, and it is type-safe/strongly typed rather than returning Object like IIf.

- id: vbnet-null-04
  answer: |
    For reference types you test null with `Is Nothing` / `IsNot Nothing` because `Is` is the reference-equality operator. Using `= Nothing` invokes the type's overloaded equality operator, which can be redefined to behave differently (e.g., a String comparison that never equals Nothing, or a custom type whose `=` always returns False), giving wrong results. `Is` is unambiguous reference comparison. For value types `= Nothing` is allowed (it checks the default value) but `Is Nothing` is the consistent, correct choice for reference null checks.

- id: vbnet-errors-01
  answer: |
    The structure is `Try` / `Catch` / `Finally` / `End Try`:
      Try
        ' code that may throw
      Catch ex As Exception [When filter]
        ' handle
      Finally
        ' cleanup
      End Try
    You can have multiple Catch blocks (most derived first) and the optional Finally runs whether or not an exception was thrown and even if a Catch does `Return` or `Exit` — it always executes before control leaves the block, which makes it the right place for releasing resources.

- id: vbnet-errors-02
  answer: |
    `Catch ex As Exception When <booleanExpression>` adds an exception filter: the Catch only handles the exception if the expression is True, and otherwise the exception propagates to the next handler. Advantages over catching and then re-checking inside the block: (1) you can inspect the exception (or ambient state) without actually catching it, so it still flows to an outer handler; (2) the original stack trace is preserved and not disturbed; (3) it keeps handler logic clean by separating "should I handle this?" from "how do I handle it?", and is supported at the runtime level for efficient, non-destructive filtering.

- id: vbnet-errors-03
  answer: |
    Inside a Catch, a bare `Throw` (no argument) rethrows the current exception and preserves its original stack trace. `Throw ex` rethrows but resets the exception's stack trace to the point of the rethrow, discarding the original call stack and making debugging much harder. To rethrow and preserve the stack, always use `Throw` (without `ex`). `Throw ex` should essentially never be used.

- id: vbnet-errors-04
  answer: |
    `On Error GoTo <label>` / `On Error Resume Next` with the legacy `Err` object is the old, unstructured (BASIC-style) error handling: a single global error handler, `Err.Number`/`Err.Description`, and `Resume`/`Resume Next`. It is error-prone (handler scope is global within the procedure), harder to read, mixes normal and error flow, and loses rich exception typing. Structured `Try/Catch` is preferred in modern VB because it is scoped, supports typed exceptions, multiple catches, filters, `Using`, preserves stack traces, composes cleanly, and integrates with the .NET exception hierarchy.

- id: vbnet-linq-01
  answer: |
    A basic query expression that filters and projects:
      Dim q = From x In source
              Where x.Age > 18
              Select x.Name
    The core keywords are `From` (defines the range variable and data source), `Where` (filter), and `Select` (projection). Other common clauses include `Order By`, `Distinct`, `Skip`, `Take`, and `Let`. This query syntax is translated by the compiler into method calls (LINQ extension methods) underneath.

- id: vbnet-linq-02
  answer: |
    Grouping and aggregation use `Group By ... Into`:
      Dim q = From c In customers
              Group c By c.Region Into g = Group, Count() = Count(), Total = Sum(c.Amount)
              Select Region, g, Count, Total
    Here `Group c By c.Region Into g = Group` creates a group `g` of the matching elements per key, and `Into` carries aggregate functions (`Count()`, `Sum(...)`, `Average(...)`, `Max(...)`, `Min(...)`). You then `Select` the key and the aggregate results.

- id: vbnet-linq-03
  answer: |
    `Aggregate` is a query keyword that computes an aggregate over a sequence (optionally seeded) and is typically terminal — it produces a single scalar result (or one result per group when nested). `Aggregate ... Into Sum(...), Count()` returns the aggregated value(s) directly. The difference from `From` is that `From` begins a query that yields a sequence of items, whereas `Aggregate` collapses the sequence into a computed result; `Aggregate` can also be used as a clause inside a `From` query to add an aggregate column alongside projected rows.

- id: vbnet-linq-04
  answer: |
    LINQ uses deferred (lazy) execution: building a query (From/Where/Select) does not execute it — it only defines it. The query actually runs when the result is enumerated (e.g. in a `For Each`, or when materialized). This means the data source can change between definition and enumeration, and the query re-evaluates each time it is enumerated. To force immediate execution you materialize the results with `ToList()`, `ToArray()`, `ToDictionary()`, or by calling an immediate aggregate like `Count()`, `Sum()`, `First()`, `Any()` — these run the query and return concrete values/collections.

- id: vbnet-events-01
  answer: |
    `WithEvents` + `Handles` is the declarative event-handling mechanism. You declare a field with `WithEvents`: `Private WithEvents btn As Button`. Then you write a method whose signature matches the event's delegate and tag it with a `Handles` clause: `Private Sub btn_Click(sender As Object, e As EventArgs) Handles btn.Click`. The compiler wires the handler to the event automatically at compile time, and it stays connected as long as the object reference is held. The handler is added/removed by the generated code based on object lifetime; it requires the event source to be a field known at compile time.

- id: vbnet-events-02
  answer: |
    `AddHandler eventName, AddressOf methodName` attaches a handler at runtime, and `RemoveHandler eventName, AddressOf methodName` detaches it; `AddressOf` produces a delegate pointing to the method. You must use `AddHandler`/`RemoveHandler` (rather than `Handles`) when the event source is not a `WithEvents` field known at compile time — e.g. objects created dynamically, handlers that must be added/removed at specific times, one method handling events from many sources, or handling events on objects whose lifetime you manage manually to avoid memory leaks.

- id: vbnet-events-03
  answer: |
    Declare a custom event with the `Event` keyword using a delegate signature, typically `Public Event MyEvent(sender As Object, e As MyEventArgs)`. Raise it with `RaiseEvent MyEvent(Me, args)` (the compiler guards so no exception is thrown if no handlers are attached). Handlers receive the `sender` (the object that raised the event) and the `e` argument (an EventArgs-derived class carrying event data), matching the delegate signature. You can also use the custom event syntax (`Custom Event ... AddHandler/RemoveHandler/RaiseEvent`) for fine control over storage and raising.

- id: vbnet-events-04
  answer: |
    Use `WithEvents`/`Handles` when the event source is a fixed field known at design time and you want simple, declarative, compiler-checked wiring that is easy to read and automatically managed. Use `AddHandler`/`RemoveHandler` when you need runtime flexibility: dynamically created objects, adding/removing handlers conditionally or repeatedly, sharing one handler across many sources, or explicitly controlling lifetime to prevent memory leaks. In short: WithEvents/Handles for static, simple cases; AddHandler/RemoveHandler for dynamic, manual cases.

- id: vbnet-oop-01
  answer: |
    `Inherits` establishes implementation inheritance from a base class; a class can have at most ONE `Inherits` (VB supports single class inheritance only). `Implements` adopts an interface's contract; a class can `Implements` many interfaces (comma-separated). You may combine them: a class can Inherits one base class and Implement any number of interfaces. Interfaces provide only the contract (no implementation, except default interface methods), while Inherits carries actual base implementation.

- id: vbnet-oop-02
  answer: |
    - `Overridable`: marks a base-class member that derived classes are allowed to override.
    - `Overrides`: used in a derived class to provide a new implementation of an `Overridable` (or `MustOverride`) member.
    - `MustOverride`: a base member with no implementation that derived classes MUST override; the containing class must be marked `MustInherit`.
    - `MustInherit`: an abstract class that cannot be instantiated with `New`; it may contain `MustOverride` members and/or concrete members.
    - `NotOverridable` (the default for non-Overridable members once overridden): seals a member so further derived classes cannot override it (used on an `Overrides` member to stop the chain).

- id: vbnet-oop-03
  answer: |
    `Shared` marks a member (method, field, property, event) as belonging to the type itself rather than to any instance — there is one copy shared across all instances (and usable without any instance). An instance member belongs to a specific object and requires an instance. A Shared member is accessed via the type name: `MyClass.SharedMethod()` or `MyClass.SharedField`, not through an object variable (though VB permits `obj.SharedMember` syntactically, the type is what matters). Shared members cannot directly access instance members without an instance reference.

- id: vbnet-oop-04
  answer: |
    - `Me` refers to the current instance (like `this`).
    - `MyBase` refers to the base class of the current instance, used to call base-class members that are shadowed/overridden (bypasses the current override).
    - `MyClass` refers to the current class's own implementation, bypassing any overrides — it calls the member as defined in this class even if a derived class has overridden it.
    `MyClass` differs from `Me` when code in a base class calls a method that is `Overridable` and has been overridden in a derived class: `Me.Method()` would invoke the derived override, whereas `MyClass.Method()` invokes this class's own (non-overridden) version. `MyClass` cannot be used to access Shared members and is effectively a "call my own version" mechanism.

- id: vbnet-convarr-01
  answer: |
    - `CType(expression, type)` performs any conversion the compiler supports, including widening, narrowing, and user-defined/conversion operators; it is the most flexible.
    - `DirectCast(expression, type)` requires that the runtime type already is the target type or a compatible one (same type, or inheritance/interface relationship) — it does NOT perform data conversion, only a reference/type check, so it is faster but throws InvalidCastException if the types are unrelated.
    - `TryCast(expression, type)` is like DirectCast but for reference types (and nullable): instead of throwing, it returns `Nothing` when the cast fails. Use TryCast when a failed cast is expected/normal; use DirectCast when you know the type is correct and want a fast, strict check; use CType when an actual conversion is needed.

- id: vbnet-convarr-02
  answer: |
    A widening conversion preserves information with no data loss or overflow risk (e.g. Integer→Long, derived→base) and is always allowed. A narrowing conversion can lose data or throw (e.g. Long→Integer, Double→Integer) and risks OverflowException at runtime. With `Option Strict On`, implicit narrowing conversions are NOT allowed — you must perform them explicitly with `CType`, `CInt`, `DirectCast`, etc. With `Option Strict Off`, the compiler permits them implicitly, which is convenient but unsafe.

- id: vbnet-convarr-03
  answer: |
    VB arrays are 0-based: the first element is index 0. To resize an array while keeping its existing contents use `ReDim Preserve arr(newUpperBound)` (without `Preserve` the contents are discarded; `Preserve` can only change the last dimension and is relatively expensive). `Dim a(n)` declares an array whose UPPER BOUND is `n`, meaning it has `n + 1` elements indexed 0 through `n` (it does not create `n` elements). To create exactly `n` elements you write `Dim a(n - 1)` or use `New Integer(n) {}`.

- id: vbnet-convarr-04
  answer: |
    `CInt`, `CStr`, `CDbl` (and siblings like `CLng`, `CBool`, `CDate`) are VB type-conversion functions that convert an expression to Integer, String, Double, etc., applying VB's conversion semantics (e.g. rounding for CInt, locale-aware for dates/numbers). You should use `&` rather than `+` for string concatenation because `+` is also the numeric addition operator: if an operand happens to be numeric or `Nothing`, `+` may attempt numeric addition or throw a type mismatch, whereas `&` coerces both operands to String and always concatenates, making intent unambiguous and safe (`"x" & Nothing` yields "x").
