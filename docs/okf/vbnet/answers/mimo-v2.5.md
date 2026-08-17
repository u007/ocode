```yaml
- id: vbnet-syntax-01
  answer: |
    Declare a variable with `Dim name As Type` (e.g. `Dim x As Integer = 5`). If the type can be inferred, `Dim x = 5` works with `Option Infer On`. Other modifiers like `Private`, `Public`, `Dim` also introduce declarations. VB statements are terminated by a newline — each line is one statement; no semicolons. To continue one statement across multiple lines, place an underscore `_` (preceded by a space) at the end of each continuation line. The line-continuation character `_` tells the compiler the next line is part of the same statement.

- id: vbnet-syntax-02
  answer: |
    `Option Explicit On` forces all variables to be declared before use, preventing typos from creating accidental variables. `Option Infer On` allows the compiler to infer the type of a local variable from its initializer so you can write `Dim x = 5` instead of `Dim x As Integer = 5`. `Option Strict On` disallows implicit narrowing conversions, late binding, and implicit typing to `Object`; the compiler enforces type safety at compile time. `Option Strict On` matters because without it VB silently allows risky implicit conversions and late-bound calls that can cause runtime errors, making the language much less safe and harder to debug.

- id: vbnet-syntax-03
  answer: |
    A `Module` is implicitly `Shared` (static) — all its members are shared, it cannot be instantiated, and its members are accessible directly by name without qualifying them with the module name (they are effectively imported into the enclosing namespace). A `Class` can contain both instance and shared members, is instantiated to create objects, and supports inheritance, interfaces, constructors, and destructors. In short: a Module is a container of globally accessible static members; a Class is a blueprint for objects with both instance and shared behavior.

- id: vbnet-syntax-04
  answer: |
    A `Sub` is a procedure that does not return a value (no return type in its signature). A `Function` is a procedure that returns a value — the return type is declared with `As ReturnType` after the parameter list. A `Function` returns its value via the `Return` statement (e.g. `Return result`) or, in older style, by assigning to the function name itself. Example: `Function Add(a As Integer, b As Integer) As Integer` then `Return a + b`.

- id: vbnet-props-01
  answer: |
    An auto-implemented property is declared without an explicit `Get`/`Set` body or backing field — the compiler generates both automatically: `Public Property Name As String`. A full property has an explicit private backing field and custom `Get`/`Set` blocks that can include validation, side effects, or computation. Auto-implemented properties are concise for simple get/set storage; full properties are needed when the property must do more than store and retrieve a value.

- id: vbnet-props-02
  answer: |
    The backing field of an auto-implemented property has no accessible name in code — the compiler generates it behind the scenes (typically as a hidden field like `<Name>k__BackingField`). You initialize an auto-property with a default value using inline assignment at declaration: `Public Property Name As String = "default"`. This default value is applied whenever a new instance is created.

- id: vbnet-props-03
  answer: |
    A read-only property has only a `Get` accessor: `Public ReadOnly Property Name As String` with a `Get` block (and no `Set`). A write-only property has only a `Set` accessor: `Public WriteOnly Property Name As String`. Yes, an auto-implemented property can be `ReadOnly`: `Public ReadOnly Property Id As Integer = 42` — the compiler generates a get-only backing field whose value can be set only via inline initialization or in the constructor.

- id: vbnet-props-04
  answer: |
    A default property is a property that is accessed implicitly when an object is used directly — for example `form1.Controls(0)` implicitly accesses the default `Item` property of the `ControlCollection`. It is declared with the `Default` keyword: `Default Public Property Item(index As Integer) As String`. A class or module may have at most one default property, and it must accept at least one parameter (it is an indexer-style property).

- id: vbnet-null-01
  answer: |
    `Nothing` in VB represents the null value — the absence of an object reference. For a reference type, `Nothing` means the variable does not reference any object (a null reference). For a value type, assigning `Nothing` is equivalent to assigning the type's default value (e.g. `0` for `Integer`, `False` for `Boolean`); the variable is not truly "null" but simply holds its default. Value types cannot be null unless declared as nullable (`Nullable(Of T)` or `T?`).

- id: vbnet-null-02
  answer: |
    Declare a nullable value type with the `?` suffix: `Dim x As Integer?`. This creates a `Nullable(Of Integer)`. Check whether it has a value using `x.HasValue` (Boolean) or `x IsNot Nothing`. Read its value safely with `x.Value` (throws `InvalidOperationException` if no value) or `x.GetValueOrDefault()` (returns the default of the underlying type if no value). You can also use the `If` operator: `Dim val = If(x, 0)` to provide a fallback.

- id: vbnet-null-03
  answer: |
    `If(a, b)` is the two-argument null-coalescing operator: it returns `a` if `a IsNot Nothing`, otherwise it returns `b`. It short-circuits — `b` is evaluated only if `a` is `Nothing`. `IIf(a, b, c)` is a function: it evaluates all three arguments eagerly before the call, then returns `b` if `a` is `True`, otherwise `c`. It does not short-circuit and always returns `Object`. `If` is preferred because it short-circuits (avoids unnecessary evaluation and side effects), is type-safe (returns the actual type of the operands), and is the proper null-coalescing operator. `IIf` is legacy and always evaluates both branches.

- id: vbnet-null-04
  answer: |
    You test with `Is Nothing` / `IsNot Nothing` because `Is` performs a reference identity check — it asks whether the variable actually references no object. Using `= Nothing` could invoke a user-defined `=` operator overload on the object, producing an unintended result. `Is Nothing` is unambiguous and always does the right thing: it checks whether the reference is null. The `=` operator for reference types compares values (if overloaded), not identity, so it is the wrong tool for a null check.

- id: vbnet-errors-01
  answer: |
    Structure:
    ```
    Try
        ' code that may throw
    Catch ex As ExceptionType
        ' handle specific exception
    Catch
        ' handle any other exception
    Finally
        ' cleanup code
    End Try
    ```
    `Try` contains the guarded code. One or more `Catch` blocks handle exceptions (most specific first). An optional `Finally` block runs after the `Try` and any matching `Catch` block have completed — it always executes regardless of whether an exception occurred, was caught, or was re-thrown, making it ideal for resource cleanup.

- id: vbnet-errors-02
  answer: |
    `Catch ... When condition` adds a filter: the `Catch` block only handles the exception if `condition` evaluates to `True` at the time the exception is caught. If the condition is `False`, the exception propagates as if that `Catch` block did not exist (other `Catch` blocks may still handle it). This is preferable to catching and then manually re-throwing inside the block because the filter keeps the exception propagation clean — the exception never enters the handler in the first place if the filter fails, avoiding unnecessary code and preserving the original exception state.

- id: vbnet-errors-03
  answer: |
    `Throw` (bare re-throw) re-throws the current exception while preserving the original stack trace, so debugging shows where the exception actually originated. `Throw ex` re-throws the exception but resets the stack trace to the current line, losing the original location information. This matters because the stack trace is critical for diagnosing the root cause; `Throw ex` makes it much harder to trace where the error truly occurred. Always use bare `Throw` when re-throwing to preserve diagnostics.

- id: vbnet-errors-04
  answer: |
    `On Error Goto label` redirects execution to a labeled error handler when a runtime error occurs; `On Error Resume Next` suppresses the error and continues on the next line. The global `Err` object holds the error number, description, and source. Structured `Try/Catch` is preferred because it is block-scoped (no global state), type-safe (catches specific exception types), supports `Finally` for guaranteed cleanup, allows `When` filters, is easier to follow and maintain, and is thread-safe — `Err` is a global mutable object, which is problematic in multithreaded code. `On Error` is legacy and should be avoided in modern VB code.

- id: vbnet-linq-01
  answer: |
    Basic shape:
    ```vb
    Dim result = From item In collection
                 Where item.Property > someValue
                 Select item.OtherProperty
    ```
    Keywords involved: `From` (identifies the data source), `Where` (filters elements based on a condition), `Select` (projects each element into the desired output shape). Additional keywords include `Order By`, `Take`, `Skip`, `Group By`, `Into`, and `Aggregate`.

- id: vbnet-linq-02
  answer: |
    ```vb
    Dim result = From item In collection
                 Group By item.Category Into ProductGroup = Group, TotalPrice = Sum(item.Price)
    ```
    Or using `Group By ... Into Group`:
    ```vb
    Dim result = From item In collection
                 Group item By item.Category Into g = Group
                 Select Category = g.Key, Items = g
    ```
    `Group By` partitions elements by a key. `Into` specifies aggregations on each group — `Group` gives the grouped items, and aggregate functions like `Sum`, `Count`, `Min`, `Max`, `Average` compute values per group.

- id: vbnet-linq-03
  answer: |
    `Aggregate` is a VB query keyword that computes a single scalar result from a collection, similar to how SQL `SELECT COUNT(*)` works. Example: `Dim total = Aggregate item In orders Into Sum(item.Amount)`. Unlike `From`, which produces a sequence of elements that can be filtered, projected, and ordered, `Aggregate` collapses the entire source into one value using aggregation functions (`Sum`, `Count`, `Min`, `Max`, `Average`, etc.). `Aggregate` is for producing a single result; `From` is for producing a collection.

- id: vbnet-linq-04
  answer: |
    Deferred (lazy) execution means the query expression is not evaluated when it is defined — the expression builds a query plan that is executed later when the results are enumerated (e.g. in a `For Each` loop or when iterated). This allows efficient, on-demand evaluation and composition. To force immediate execution, call a materializing method: `.ToList()`, `.ToArray()`, `.ToDictionary()`, `.Count()`, `.First()`, `.Any()`, `.Sum()`, `.Min()`, `.Max()`, `.Single()`, etc. These methods evaluate the query and return a concrete result or collection right away.

- id: vbnet-events-01
  answer: |
    `WithEvents` is declared on a class-level field to tell the compiler the object can raise events: `Private WithEvents btn As Button`. The `Handles` keyword in a method's `Handles` clause connects the method to a specific event at compile time: `Private Sub btn_Click(sender As Object, e As EventArgs) Handles btn.Click`. This is a compile-time mechanism — the compiler wires the handler automatically when the object is assigned. It only works with class-level fields (not local variables) and only for events exposed by the declared type.

- id: vbnet-events-02
  answer: |
    `AddHandler` dynamically subscribes a method to an event at runtime: `AddHandler btn.Click, AddressOf OnClick`. `RemoveHandler` unsubscribes: `RemoveHandler btn.Click, AddressOf OnClick`. `AddressOf` creates a delegate referencing the handler method. You must use `AddHandler`/`RemoveHandler` instead of `Handles` when: the event source is not known at compile time (created dynamically at runtime), you need to unsubscribe/unhook, the object doesn't expose `WithEvents` support (e.g. obtained through an interface or late binding), or you need to dynamically change which handler is subscribed at runtime.

- id: vbnet-events-03
  answer: |
    Declare with `Public Event` on a class, optionally specifying delegate parameters: `Public Event DataReceived(sender As Object, e As DataEventArgs)`. Raise with `RaiseEvent DataReceived(Me, New DataEventArgs(data))`. Handlers receive arguments matching the event delegate signature: `Private Sub OnDataReceived(sender As Object, e As DataEventArgs) Handles obj.DataReceived`. You can also declare using a custom delegate type: `Public Event DataReceived As DataReceivedHandler`. For events with no parameters, just `Public Event SomethingHappened` and `RaiseEvent SomethingHappened()`.

- id: vbnet-events-04
  answer: |
    Choose `WithEvents`/`Handles` when the event source is known at compile time (design-time controls, form-level objects), you want declarative readable code, the handler is fixed for the object's lifetime, and you don't need to unsubscribe — it's simpler and more maintainable for standard UI scenarios. Choose `AddHandler`/`RemoveHandler` when the event source is created dynamically at runtime, you need to unsubscribe, the object is obtained through an interface or doesn't support `WithEvents`, or you need to dynamically change handlers at runtime — they are the only option for dynamic/event-subscription scenarios.

- id: vbnet-oop-01
  answer: |
    `Inherits` specifies the single base class from which a class derives: `Public Class Child Inherits Parent`. A class can inherit from exactly one base class (single inheritance). `Implements` specifies which interfaces a class fulfills: `Public Class MyClass Implements IResizable, IDrawable`. A class can implement multiple interfaces (unlimited). Interfaces themselves can inherit from multiple other interfaces with `Inherits`. So: one `Inherits`, unlimited `Implements`.

- id: vbnet-oop-02
  answer: |
    `Overridable` (on a base-class member) marks it as virtual — it can be overridden in derived classes. `Overrides` (on a derived-class member) redefines an `Overridable` or `MustOverride` member. `MustOverride` (on a base-class member) forces derived classes to provide an implementation — the member has no body, and the containing class must be `MustInherit`. `MustInherit` (on a class) makes it abstract — it cannot be instantiated directly and must be inherited. `NotOverridable` (on an overridden member in a derived class) prevents further overriding in subclasses (seals the override). These mirror C#'s `virtual`, `override`, `abstract`, `abstract class`, and `sealed override` respectively.

- id: vbnet-oop-03
  answer: |
    `Shared` (equivalent to `static` in C#) means the member belongs to the type itself rather than to any particular instance. A shared member exists in one copy shared by all instances and is accessed through the class name: `MyClass.SharedMethod()`. An instance member is tied to a specific object and accessed through an instance variable: `obj.InstanceMethod()`. Shared members cannot access instance members directly (there is no `Me` in a shared context), while instance members can access both instance and shared members.

- id: vbnet-oop-04
  answer: |
    `Me` refers to the current instance — used to access instance members, pass the current object, or qualify member access. `MyBase` refers to the immediate base class — used to call parent constructors, overridden methods, or inherited members (analogous to `base` in C#). `MyClass` refers to the current instance as if it were not overridden — it bypasses polymorphic dispatch and calls the implementation in the current class. `MyClass` differs from `Me` when called from within an overridden method: `MyClass.Method()` calls the non-overridden version (the current class's own implementation), while `Me.Method()` calls the most-derived override (respecting polymorphism). Outside of overridden contexts, they behave identically.

- id: vbnet-convarr-01
  answer: |
    `CType(expression, type)` is the general-purpose conversion — it uses any available conversion operator (Widening or Narrowing, including user-defined operators) and works with both value types and reference types. `DirectCast(expression, type)` requires a direct runtime type relationship (inheritance or interface implementation) and does not use conversion operators; it throws `InvalidCastException` on failure. `TryCast(expression, type)` is like `DirectCast` but returns `Nothing` instead of throwing on failure — it only works with reference types. Use `CType` for general conversions, `DirectCast` when you know the runtime type is compatible and want a fast direct cast, and `TryCast` when the conversion might fail and you want to handle that gracefully without exception handling.

- id: vbnet-convarr-02
  answer: |
    A widening conversion always succeeds at runtime and never loses data — e.g. `Integer` to `Long`, `Single` to `Double`, a derived type to its base type. A narrowing conversion may fail or lose information — e.g. `Long` to `Integer`, `Double` to `Integer`, `String` to `Integer`. With `Option Strict On`, widening conversions are allowed implicitly (no explicit cast needed), but narrowing conversions are disallowed at compile time — you must use an explicit conversion function (`CType`, `DirectCast`, etc.) to acknowledge the potential loss. With `Option Strict Off`, both are allowed implicitly, which can cause silent data loss or runtime exceptions.

- id: vbnet-convarr-03
  answer: |
    VB arrays are 0-based by default. The upper bound in `Dim a(n)` specifies the maximum index, not the element count — so `Dim a(4)` creates an array with 5 elements (indices 0 through 4). To resize an array while preserving its existing contents, use `ReDim Preserve array(newSize)`. `Preserve` retains the current element values; without it, `ReDim` clears the array. Note: `ReDim Preserve` can only resize the last dimension of a multi-dimensional array.

- id: vbnet-convarr-04
  answer: |
    `CInt`, `CStr`, `CDbl` (and similar `CByte`, `CDate`, `CDec`, `CLng`, `CSng`, `CBool`, `CChar`) are legacy VB conversion functions that convert an expression to `Integer`, `String`, `Double`, etc. respectively. `CType(expression, type)` is the general-purpose equivalent. Use `&` (not `+`) for string concatenation because `+` is ambiguous: when both operands are strings it concatenates, but when one is numeric it may perform arithmetic addition (especially with `Option Strict Off`). The `&` operator always performs string concatenation regardless of operand types, making intent clear and avoiding unexpected numeric addition.
