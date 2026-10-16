# nvidia/nemotron-3.5-super-vl — closed-book answers for vbnet (openrouter, 2026-10-16); answerer saw only _prompts/vbnet.md
- id: vbnet-syntax-01
  answer: |
    Declare variables with `Dim`: `Dim count As Integer = 0` (or `Dim count = 0` when Option Infer is on; several names can follow one `Dim`). Statements terminate at a line break — no semicolon is required, and `:` separates multiple statements on one line. Continue a statement across lines with a space-underscore ` _` at the end of the line; in modern VB many contexts (after commas, operators, etc.) allow implicit continuation so `_` is often unnecessary.
- id: vbnet-syntax-02
  answer: |
    `Option Strict` controls implicit narrowing conversions, late binding, and implicit conversion to Object; `On` forces explicit casts and disallows late binding, giving compile-time type safety. `Option Explicit` requires every variable to be declared before use (catches typos). `Option Infer` allows local variable type inference, e.g. `Dim x = 1` becomes `Integer`. `Option Strict On` matters because it prevents silent data loss from implicit narrowing and runtime failures from late binding, surfacing type errors at compile time instead of at runtime.
- id: vbnet-syntax-03
  answer: |
    A `Module` is a single shared instance that cannot be instantiated or inherited; its members are implicitly `Shared` and are accessed through the module name. A `Class` is a type blueprint: it can be instantiated (unless `MustInherit`), supports inheritance, and holds per-instance state by default. Use `Module` for shared helper members; use `Class` for objects with identity and state.
- id: vbnet-syntax-04
  answer: |
    A `Sub` performs an action and returns no value. A `Function` computes and returns a value. A `Function` returns its value with `Return expression` or by assigning to the function name itself (`FunctionName = expression`) followed by `Exit Function`; the assignment style is legacy but still supported.
- id: vbnet-props-01
  answer: |
    An auto-implemented property, `Public Property Name As String`, lets the compiler generate the backing field and the Get/Set boilerplate automatically. A full property declares an explicit backing field plus custom getter/setter logic, which is needed for validation, lazy initialization, change notification, or transforming the stored value.
- id: vbnet-props-02
  answer: |
    The compiler-generated backing field is named with an underscore prefix, e.g. `_Name` for a property `Name`. Initialize an auto-property with an initializer: `Public Property Name As String = "default"` or `Public Property Items As New List(Of Integer)()`.
- id: vbnet-props-03
  answer: |
    Declare `Public ReadOnly Property Name As String` (Get only) or `Public WriteOnly Property Name As String` (Set only). Yes — an auto-implemented property can be `ReadOnly`; it can be initialized with an initializer and assigned within the class (e.g., in a constructor), but exposes no public setter.
- id: vbnet-props-04
  answer: |
    A default property is an indexed property marked `Default` that callers can use without naming the property: `cust(1)` instead of `cust.Item(1)`. Constraint: it must accept at least one parameter (a parameterless default property is not allowed), and a class can have only one default property.
- id: vbnet-null-01
  answer: |
    `Nothing` is the default value of a type. For a reference type it is a null reference — no object instance. For a value type it is the type's default (0 for numerics, `False` for Boolean, zeroed structure), not a null reference; to model "missing" for value types use a nullable value type (`Nullable(Of T)` / `T?`).
- id: vbnet-null-02
  answer: |
    Declare `Dim x As Integer?` or `Dim x As Nullable(Of Integer)`. Check it with `x.HasValue` or `x Is Nothing` / `x IsNot Nothing`. Read safely with `x.GetValueOrDefault()` or guard with `If(x.HasValue, x.Value, fallback)`; `x.Value` throws `InvalidOperationException` when there is no value.
- id: vbnet-null-03
  answer: |
    `If(a, b)` (two-operand) is the null-coalescing operator: returns `a` if it is not `Nothing`, otherwise `b`. `If(a, b, c)` is the ternary conditional. `IIf(a, b, c)` is a function, so all three arguments are always evaluated and the result is `Object` (boxing/casting overhead, and the unused branch can still throw). `If` is preferred because it short-circuits, is type-safe, and evaluates only the chosen branch.
- id: vbnet-null-04
  answer: |
    `Is Nothing` / `IsNot Nothing` compare reference identity, which is the correct way to test whether a reference is null. `= Nothing` invokes value-equality semantics (and any overloaded `=` operator), can behave unexpectedly or throw depending on the type, and is disallowed for some comparisons under `Option Strict On`; it does not reliably detect a null reference.
- id: vbnet-errors-01
  answer: |
    `Try ... [Catch ex As Exception [When filter]] ... [Finally] ... End Try`. `Catch` handles matching exceptions; `Finally` runs unconditionally — after the `Try` completes, after a `Catch` handles the error, and even across `Return`, `Exit`, or `GoTo` leaving the block (unless the process is terminated).
- id: vbnet-errors-02
  answer: |
    `Catch ... When filter` evaluates a boolean filter before the catch binds. If the filter is false, the exception is not caught and continues propagating with its stack trace intact. This is better than catching and re-checking inside the block because a failing filter never swallows the exception, so outer handlers still see it and the original stack trace is preserved.
- id: vbnet-errors-03
  answer: |
    `Throw` rethrows the current exception and preserves the original stack trace. `Throw ex` rethrows the exception object but resets the stack trace to the current location, losing where the error originated. Always use bare `Throw` to keep diagnostic information.
- id: vbnet-errors-04
  answer: |
    `On Error GoTo` / `Err` / `Resume` is legacy unstructured error handling (VB6 style): global error state, line-label spaghetti flow, and no typed matching. Structured `Try/Catch` is preferred because it is scoped, supports typed and filtered catches, guarantees `Finally`, preserves stack traces, and produces clearer, safer code. `On Error` remains mainly for legacy migration compatibility.
- id: vbnet-linq-01
  answer: |
    `Dim q = From c In customers Where c.City = "London" Select c.Name`. Keywords: `From` (defines the source and range variable), `Where` (filters), `Select` (projects). `Order By`, `Take`, `Skip`, etc. can extend it.
- id: vbnet-linq-02
  answer: |
    `Dim g = From c In customers Group By c.City Into CityCustomers = Group, Count()`. `Into` accepts aggregators such as `Count`, `Sum`, `Min`, `Max`, `Average`, `Aggregate`, and `Group`; `Into Group = Group` captures the grouped elements for each key.
- id: vbnet-linq-03
  answer: |
    `Aggregate` computes a single scalar result from a sequence: `Aggregate c In customers Into Sum(c.Age)` or `Into Count()`. Unlike `From`, which produces a query sequence (`IEnumerable`) that can be further composed and enumerated, `Aggregate` is a terminal operation yielding one value.
- id: vbnet-linq-04
  answer: |
    Deferred execution means the query is built but not run until it is enumerated (e.g., `For Each`, data binding, or a terminal operator). Force immediate execution with a terminal extension such as `.ToList()`, `.ToArray()`, `.Count()`, `.First()`, or `.ToDictionary()`.
- id: vbnet-events-01
  answer: |
    `WithEvents` marks a declared object variable so the compiler can wire its events; a method with a `Handles` clause, e.g. `Private Sub btn_Click(...) Handles Button1.Click`, is bound at compile time to that object's event. The handler signature must match the event delegate, and wiring exists for the lifetime of the `WithEvents` object.
- id: vbnet-events-02
  answer: |
    `AddHandler source.Event, AddressOf Handler` attaches a handler at runtime; `RemoveHandler` detaches it. `AddressOf` creates a delegate referring to the method. Use them instead of `Handles` when the source object isn't known at declaration time, handlers must be attached/detached dynamically, or the handler lifetime differs from the source's.
- id: vbnet-events-03
  answer: |
    Declare `Public Event ProcessComplete(sender As Object, e As ProcessEventArgs)` and raise it with `RaiseEvent ProcessComplete(Me, e)`. Handlers are subs matching the delegate signature (commonly `Sub(sender As Object, e As EventArgs)`); custom delegate types are also supported.
- id: vbnet-events-04
  answer: |
    Choose `WithEvents`/`Handles` for stable, design-time-known sources — less boilerplate, simple and the default for UI controls. Choose `AddHandler`/`RemoveHandler` for dynamic sources, runtime attach/detach, optional handlers, or when you must explicitly disconnect to avoid lifetime leaks; it is more verbose but more flexible.
- id: vbnet-oop-01
  answer: |
    `Inherits` is class inheritance and a class can have exactly one base class. `Implements` is interface implementation and a class may implement many interfaces. Implementing an interface requires providing all its members unless the class is `MustInherit`.
- id: vbnet-oop-02
  answer: |
    `Overridable` allows a derived class to override a member (VB members are non-overridable by default). `Overrides` provides the derived implementation. `MustOverride` declares an abstract member with no body that derived classes must override. `MustInherit` makes a class abstract so it cannot be instantiated directly. `NotOverridable` explicitly seals a member (the default behavior).
- id: vbnet-oop-03
  answer: |
    `Shared` means the member belongs to the type itself, with one storage location shared by all instances; access it via `TypeName.Member` (access through an instance variable works but is discouraged). An instance member has separate storage per object and is accessed through an object reference.
- id: vbnet-oop-04
  answer: |
    `Me` is the current instance; `MyBase` accesses the base class portion (base constructors and members); `MyClass` refers to the current instance but calls are non-virtual, bypassing overrides. `MyClass` differs from `Me` when a member is overridable and a derived class overrides it: `Me.Method()` dispatches to the derived override, while `MyClass.Method()` calls this class's own implementation.
- id: vbnet-convarr-01
  answer: |
    `CType` performs general conversions — widening, narrowing, and user-defined conversions — and throws on failure. `DirectCast` is a pure type cast (same type or inheritance, no conversion); it is faster but throws `InvalidCastException` on a bad type. `TryCast` behaves like `DirectCast` but returns `Nothing` on failure (reference/nullable types only), so use it when the type is uncertain and you want to avoid exceptions.
- id: vbnet-convarr-02
  answer: |
    Widening conversions are safe and preserve value (e.g., `Integer` to `Double`); implicit widening is allowed even with `Option Strict On`. Narrowing conversions may lose data or fail (e.g., `Double` to `Integer`); with `Option Strict On` they require an explicit cast (`CType`, `CInt`, etc.). `Option Strict On` also bans late binding and implicit `Object` conversions.
- id: vbnet-convarr-03
  answer: |
    Yes, VB arrays are 0-based. Resize while keeping contents with `ReDim Preserve arr(newUpperBound)` (for multidimensional arrays, only the last dimension may change with `Preserve`). `Dim a(n)` sets the upper bound to `n`, so the array has `n + 1` elements (indices `0` through `n`) — `n` is the last index, not the count.
- id: vbnet-convarr-04
  answer: |
    `CInt`, `CStr`, and `CDbl` are legacy explicit conversion functions to Integer, String, and Double (culture-aware); modern code generally prefers `CType`, `DirectCast`, or the `Convert` class. Use `&`, not `+`, for string concatenation: `&` always treats operands as text and is type-safe, while `+` is ambiguous (numeric addition vs. concatenation) and can produce unexpected results or errors with mixed types.
