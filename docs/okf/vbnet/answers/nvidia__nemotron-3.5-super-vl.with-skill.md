# nvidia/nemotron-3.5-super-vl — with-skill answers for vbnet (openrouter, 2026-10-16); answerer saw only _prompts/vbnet.md; derived/vbnet.nvidia__nemotron-3.5-super-vl.SKILL.md body (front matter stripped) prepended to the sheet as active guidance
- id: vbnet-syntax-01
  answer: |
    Declare a variable with `Dim` (`Dim name As Type`, or `Dim name = value` to let the type be inferred). Statements end at a line break — no semicolon is required, though a colon `:` can separate multiple statements on one line. To continue one statement across multiple lines, end the line with ` _` (space followed by an underscore); modern VB also allows implicit line continuation in many contexts (after commas, operators, open parentheses, etc.).
- id: vbnet-syntax-02
  answer: |
    `Option Explicit` requires every variable to be declared before use. `Option Strict` forbids implicit narrowing conversions, late binding, and implicit `Object` returns, forcing explicit casts. `Option Infer` lets local variables infer their type from their initializer. `Option Strict On` matters because it turns silent, potentially data-losing or runtime-failing operations into compile errors instead of letting them happen quietly at runtime.
- id: vbnet-syntax-03
  answer: |
    A `Module` is a shared-only singleton: it cannot be instantiated, inherited, or implement interfaces, and its members are accessed through the module name (all members are implicitly `Shared`). A `Class` is a normal type: it can be instantiated, supports single inheritance, implements interfaces, and can hold instance state and instance members.
- id: vbnet-syntax-04
  answer: |
    A `Sub` performs an action and returns no value; it is called as a statement. A `Function` computes and returns a value and is used in expressions. Return the value with `Return expression`, or (classic VB style) assign to the function's own name (`FunctionName = expression`) and then exit.
- id: vbnet-props-01
  answer: |
    An auto-implemented property (`Public Property Name As String`) lets the compiler generate the hidden backing field and the get/set logic automatically; you only write the declaration. A full property declares an explicit backing field and custom `Get`/`Set` accessors, allowing validation, lazy initialization, change notification, or other logic.
- id: vbnet-props-02
  answer: |
    The backing field of an auto-property `Name` is also named `Name` — fields and properties live in separate namespaces, so there is no conflict. Initialize it inline: `Public Property Name As String = "default"` or `Public Property Items As New List(Of String)()`.
- id: vbnet-props-03
  answer: |
    Declare `ReadOnly Property Name As String` (only a `Get` accessor) for read-only, or `WriteOnly Property Password As String` (only a `Set` accessor) for write-only; omit the accessor you don't want. Yes — an auto-implemented property can be `ReadOnly` (`Public ReadOnly Property Name As String`), and the compiler exposes only the getter.
- id: vbnet-props-04
  answer: |
    A default property is an indexer-style property marked `Default` that takes at least one parameter, so consumers can write `collection(i)` instead of `collection.Item(i)`. Constraint: it must accept at least one parameter, and a class may have only one default property.
- id: vbnet-null-01
  answer: |
    `Nothing` is VB's default-value literal. For a reference type it is a null reference (refers to no object). For a value type it is the type's default (zero-initialized: `0` for `Integer`, `0001-01-01` for `Date`, etc.). For `Nullable(Of T)`, `Nothing` means `HasValue = False` (null).
- id: vbnet-null-02
  answer: |
    Declare `Dim x As Integer?` (shorthand for `Nullable(Of Integer)`). Check it with `x.HasValue`, or `x Is Nothing` / `x IsNot Nothing`. Read safely with `x.GetValueOrDefault()` (returns the default if null) or `x.Value` (throws if null); coalesce with `If(x, defaultValue)`.
- id: vbnet-null-03
  answer: |
    `If(a, b)` with two arguments is null coalescing: returns `a` if it is not `Nothing`, otherwise `b`. `If(cond, truePart, falsePart)` with three arguments is a conditional operator that short-circuits (evaluates only the chosen branch). `IIf(a, b, c)` is a function: all three arguments are always evaluated eagerly, it returns `Object`, and it can throw in the unchosen branch (e.g., divide-by-zero). `If` is preferred because it short-circuits and is type-safe.
- id: vbnet-null-04
  answer: |
    `Is Nothing` / `IsNot Nothing` performs a reference-identity null check and is reliable. `= Nothing` invokes the `=` operator, which can be overloaded, may compare values rather than references, and with nullable types yields a lifted `Nothing` result that behaves unpredictably in conditions — so `Is Nothing` is the correct test.
- id: vbnet-errors-01
  answer: |
    `Try` runs the guarded code; one or more `Catch [ex As Type] [When filter]` blocks handle matching exceptions; `Finally` holds cleanup that always runs when control leaves the `Try` — on normal completion, exception, `Return`, or `Exit` — even if a `Catch` rethrows. Close with `End Try`.
- id: vbnet-errors-02
  answer: |
    `Catch ... When filter` catches only when the Boolean filter is true; if false, the exception keeps unwinding the stack with its original stack trace intact. It is better than catching and re-checking inside the block because it avoids unwinding exceptions you cannot handle, preserves diagnostic information, and avoids extra catch logic.
- id: vbnet-errors-03
  answer: |
    Bare `Throw` rethrows the current exception and preserves the original stack trace. `Throw ex` rethrows but resets the stack trace to the current location, losing where the failure originally occurred. Use bare `Throw` to keep the full diagnostic information.
- id: vbnet-errors-04
  answer: |
    `On Error GoTo` / `On Error Resume Next` with `Err.Number`, `Err.Description`, and `Resume` is VB6-era unstructured, global-state error handling using GOTO-style flow. Structured `Try/Catch` is preferred: it is scoped, typed, filterable, guarantees `Finally` cleanup, and produces readable, maintainable code.
- id: vbnet-linq-01
  answer: |
    `Dim londonNames = From c In customers Where c.City = "London" Select c.Name`. The keywords are `From` (data source and range variable), `Where` (filter), and `Select` (projection); queries are translated into calls to LINQ extension methods.
- id: vbnet-linq-02
  answer: |
    `From c In customers Group By c.City Into cityGroup = Group, Count(c)`. The `Group By ... Into` form groups elements by a key; `Group` gives the elements of each group, and aggregates like `Count`, `Sum`, `Average`, `Min`, `Max` can be computed per group.
- id: vbnet-linq-03
  answer: |
    `Aggregate c In customers Into Sum(c.Age)` executes immediately and returns a single scalar result. `From` builds a deferred, composable query returning an `IEnumerable` sequence. `Aggregate` is eager and terminal; `From` is lazy and composable.
- id: vbnet-linq-04
  answer: |
    Deferred execution means the query is not run when it is built; it runs when the result is enumerated (e.g., `For Each`, data binding). Force immediate execution with `ToList()`, `ToArray()`, `ToDictionary()`, `Count()`, `Aggregate`, or simply by iterating the results right away.
- id: vbnet-events-01
  answer: |
    `WithEvents` marks a field so that its events can be handled declaratively; a method with a `Handles object.EventName` clause is bound to that event at compile time. The handler's signature must match the event's delegate type.
- id: vbnet-events-02
  answer: |
    `AddHandler event, AddressOf handler` subscribes a method to an event at runtime; `RemoveHandler` unsubscribes it; `AddressOf` creates a delegate to a method. Use them instead of `Handles` when the source is not a `WithEvents` member (a local variable, a runtime-created object, or another class's instance), or when handlers must be added/removed conditionally.
- id: vbnet-events-03
  answer: |
    Declare `Public Custom Event MyEvent As EventHandler` with `AddHandler(ByVal value As EventHandler)`, `RemoveHandler(...)`, and `RaiseEvent(ByVal sender As Object, ByVal e As EventArgs)` accessors (typically storing delegates in a backing field). Raise it with `RaiseEvent MyEvent(Me, e)`. Handlers receive arguments per the delegate — conventionally `(sender As Object, e As EventArgs)`.
- id: vbnet-events-04
  answer: |
    Choose `WithEvents`/`Handles` for fixed, compile-time-known sources owned by the class — it is simple and declarative. Choose `AddHandler`/`RemoveHandler` for dynamic sources, runtime wiring, conditional subscription, or when you must unsubscribe to avoid keeping the source alive.
- id: vbnet-oop-01
  answer: |
    `Inherits` establishes class inheritance (VB allows exactly one base class — no multiple class inheritance). `Implements` declares interface implementation (a class may implement many interfaces). Every implemented interface member must carry its own `Implements IFoo.Member` clause.
- id: vbnet-oop-02
  answer: |
    `Overridable` marks a base member as virtual. `Overrides` replaces an overridable member in a derived class. `MustOverride` declares an abstract member with no body; its class must be `MustInherit`. `MustInherit` makes the class non-instantiable. `NotOverridable` may only appear with `Overrides` and seals that override so no further subclass can override it. Members are non-overridable by default.
- id: vbnet-oop-03
  answer: |
    `Shared` means the member belongs to the type itself rather than to any instance — there is one shared storage location for all instances, and there is no `Me`. Access it via the type name (`ClassName.SharedMember`); accessing it through an instance works but is discouraged. A shared member can directly access only other shared members.
- id: vbnet-oop-04
  answer: |
    `Me` is the current instance. `MyBase` is the base class portion of the current instance, used to call base constructors and overridden base members. `MyClass` refers to the current instance but calls methods non-virtually (as if `NotOverridable`), bypassing derived overrides. They differ when a derived class overrides a method: `Me.Method()` calls the derived override, while `MyClass.Method()` calls this class's own implementation.
- id: vbnet-convarr-01
  answer: |
    `CType(expr, type)` performs conversions — widening, narrowing, and user-defined conversions — and throws on invalid conversion. `DirectCast(expr, type)` is a pure type cast (reference/inheritance only, no conversion); it is faster but throws `InvalidCastException` on mismatch. `TryCast(expr, type)` is like `DirectCast` but returns `Nothing` instead of throwing; use it when the type is uncertain.
- id: vbnet-convarr-02
  answer: |
    Widening converts to a superset type with no data loss (`Integer` → `Long`); `Option Strict On` allows implicit widening. Narrowing may lose data or fail (`Long` → `Integer`, `Double` → `Integer`); `Option Strict On` forbids implicit narrowing and requires an explicit cast or conversion function.
- id: vbnet-convarr-03
  answer: |
    Yes, VB arrays are 0-based. Resize while keeping contents with `ReDim Preserve a(newUpperBound)` (for multidimensional arrays, only the last dimension may be resized when preserving data). `Dim a(n)` declares an array whose upper bound is `n` — that is `n + 1` elements, indices `0` through `n`.
- id: vbnet-convarr-04
  answer: |
    `CInt`, `CStr`, and `CDbl` are VB's intrinsic conversion functions to `Integer`, `String`, and `Double`. Use `&` for string concatenation: `+` is primarily addition and can perform arithmetic with numeric operands or behave ambiguously, while `&` always converts operands to strings and concatenates them.
