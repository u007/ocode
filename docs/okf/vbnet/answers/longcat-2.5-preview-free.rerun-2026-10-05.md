- id: vbnet-syntax-01
  answer: |
    Variables are declared with `Dim` (e.g., `Dim x As Integer`). Statements are terminated by a newline or, when multiple statements appear on one line, by a colon (`:`). To continue a single statement across multiple lines, place a space followed by an underscore (` _`) at the end of the line; the compiler joins the next line as a continuation.

- id: vbnet-syntax-02
  answer: |
    `Option Strict` controls implicit type conversions — when `On`, narrowing implicit conversions (e.g., `Long` to `Integer`) are disallowed, preventing silent data loss and runtime errors. `Option Explicit` requires all variables to be declared before use (`On` is the default). `Option Infer` enables type inference on `Dim` declarations without `As` (e.g., `Dim x = 5` infers `Integer`). `Option Strict On` matters because it catches type-mismatch bugs at compile time rather than allowing risky implicit conversions that can fail at runtime.

- id: vbnet-syntax-03
  answer: |
    A `Module` is a static container whose members are implicitly `Shared`; it cannot be instantiated or inherited and serves as a global utility holder. A `Class` is a reference type that can be instantiated with `New`, supports inheritance, and can have instance (non-shared) members, constructors, and implement interfaces.

- id: vbnet-syntax-04
  answer: |
    A `Sub` performs an action and does not return a value. A `Function` performs an action and returns a value — you use the `Return` statement (e.g., `Return x`) or, in legacy style, assign a value to the function's name. The return type is declared with `As` after the parameter list.

- id: vbnet-props-01
  answer: |
    An auto-implemented property is declared without an explicit backing field or accessor body: `Public Property Name As String`. The compiler generates a hidden backing field and default `Get`/`Set` accessors. A full property declares an explicit backing field and custom `Get`/`Set` logic, allowing validation, computation, or other code in the accessors.

- id: vbnet-props-02
  answer: |
    The backing field of an auto-implemented property is named with an underscore prefix matching the property name (e.g., `_Name` for `Name`). You initialize an auto-property with a default value using an initializer: `Public Property Name As String = "default"`.

- id: vbnet-props-03
  answer: |
    A read-only property uses `ReadOnly` before `Property` and has only a `Get` accessor. A write-only property uses `WriteOnly` and has only a `Set` accessor. An auto-implemented property can be `ReadOnly` (VB 14+), in which case it can be set in the constructor or via an initializer but has no public setter.

- id: vbnet-props-04
  answer: |
    A default property is the member accessed when no member name is specified on an instance (e.g., `Dim s = obj(0)`). It must be parameterized (take at least one argument), making it an indexed property. Only one default property is allowed per class.

- id: vbnet-null-01
  answer: |
    `Nothing` represents the default value of a type. For a reference type, it means a null reference (no object). For a value type, it means the default value of that type (e.g., `0` for `Integer`, `False` for `Boolean`). For a nullable value type, it means a null (no value) state.

- id: vbnet-null-02
  answer: |
    Declare a nullable value type with `Nullable(Of T)` or the shorthand `T?` (e.g., `Dim x As Integer?`). Check whether it has a value with `.HasValue` (returns `Boolean`). Read the value safely with `.Value` (throws if null) or use the null-coalescing operator `If(x, defaultValue)` or `x.GetValueOrDefault()`.

- id: vbnet-null-03
  answer: |
    `If(a, b)` is the null-coalescing operator — it returns `a` if `a` is not `Nothing`, otherwise `b`. `IIf(a, b, c)` is a 3-argument conditional function that evaluates both `b` and `c` regardless of the condition. `If` is preferred because it is type-safe, short-circuits (only evaluates the needed branch), and avoids the overhead and side effects of evaluating both branches.

- id: vbnet-null-04
  answer: |
    `Is Nothing` / `IsNot Nothing` use reference identity comparison, which is the correct way to test whether a reference is null. The `=` operator can be overloaded by a type and may not perform a true null comparison, leading to unexpected results. `Is` guarantees reference equality semantics.

- id: vbnet-errors-01
  answer: |
    Structure: `Try` ... [code] ... `Catch ex As Exception` ... [handler] ... `Finally` ... [cleanup] ... `End Try`. The `Finally` block always runs — whether an exception was thrown, caught, or not — making it the place for guaranteed cleanup (closing files, releasing resources).

- id: vbnet-errors-02
  answer: |
    `Catch ex As Exception When condition` filters exceptions by a Boolean condition, so the handler only catches exceptions matching that condition. It is preferred over catching all exceptions and re-checking inside the block because it avoids the overhead of unwinding the stack for exceptions you don't handle, and it lets more specific handlers remain reachable.

- id: vbnet-errors-03
  answer: |
    Bare `Throw` rethrows the original exception while preserving the original stack trace, which is critical for debugging. `Throw ex` rethrows the exception but resets the stack trace to the current location, losing the original throw site. Always use bare `Throw` when rethrowing.

- id: vbnet-errors-04
  answer: |
    `On Error Goto` is legacy unstructured error handling that jumps to a labeled line when an error occurs; the `Err` object holds the current error number and description. Structured `Try/Catch` is preferred because it is type-safe, supports exception filtering, guarantees `Finally` cleanup, integrates with the CLR exception hierarchy, and produces more maintainable code.

- id: vbnet-linq-01
  answer: |
    ```vb
    Dim results = From item In collection
                  Where item.IsActive
                  Select item.Name
    ```
    Keywords: `From` (source), `In` (range variable over source), `Where` (filter), `Select` (projection).

- id: vbnet-linq-02
  answer: |
    ```vb
    Dim grouped = From item In items
                  Group By item.Category Into Group
                  Select Category, Count = Group.Count()
    ```
    `Group By ... Into` groups elements by a key; the `Into` alias (`Group`) represents the collection of elements in each group, which you can aggregate with `.Count()`, `.Sum()`, `.Average()`, etc.

- id: vbnet-linq-03
  answer: |
    `Aggregate` performs a custom accumulation over a sequence (e.g., `Aggregate Function(acc, x) acc + x`). `From` starts a query and defines the source collection and range variable. `From` sets up the data source; `Aggregate` transforms the sequence into a single result.

- id: vbnet-linq-04
  answer: |
    Deferred (lazy) execution means the query is not executed when defined — only when the result is enumerated (e.g., in a `For Each` loop). To force immediate execution, use a terminal operator like `.ToList()`, `.ToArray()`, `.Count()`, `.First()`, or `.Sum()`, which materialize the results.

- id: vbnet-events-01
  answer: |
    Declare a variable with `WithEvents` (e.g., `Private WithEvents btn As Button`). Then declare a method with the `Handles` clause specifying the object and event (e.g., `Private Sub btn_Click(sender As Object, e As EventArgs) Handles btn.Click`). VB automatically wires the method to the event when the object is assigned.

- id: vbnet-events-02
  answer: |
    `AddHandler` dynamically wires a method to an event at runtime; `RemoveHandler` unwires it. `AddressOf` creates a delegate pointing to the method. You must use `AddHandler`/`RemoveHandler` instead of `Handles` when the event source or handler is determined at runtime, when handling events from multiple objects with one method, or when the object isn't declared `WithEvents`.

- id: vbnet-events-03
  answer: |
    Declare a custom event with `Event` (e.g., `Public Event SomethingChanged(sender As Object, e As EventArgs)`). Raise it with `RaiseEvent SomethingChanged(Me, e)`. Handlers receive the sender and event arguments through the standard `(sender As Object, e As ...)` signature, matching the delegate type implied by the event declaration.

- id: vbnet-events-04
  answer: |
    Choose `WithEvents`/`Handles` when the event source is known at design time and the wiring is static — it is cleaner and auto-managed. Choose `AddHandler`/`RemoveHandler` when the source is dynamic, when you need to attach/detach at runtime, when one method handles events from multiple objects, or when the object isn't available at declaration time.

- id: vbnet-oop-01
  answer: |
    `Inherits` specifies the single base class a class derives from (a class can inherit from only one class). `Implements` specifies interfaces the class implements (a class can implement multiple interfaces). `Inherits` provides implementation reuse; `Implements` provides contract adherence.

- id: vbnet-oop-02
  answer: |
    `Overridable` marks a method/property in a base class as overridable. `Overrides` in a derived class replaces the base implementation. `MustOverride` declares a member that derived classes must override (makes the class `MustInherit`). `MustInherit` declares an abstract class that cannot be instantiated. `NotOverridable` prevents further overriding in derived classes (default for `Overrides` members).

- id: vbnet-oop-03
  answer: |
    `Shared` means the member belongs to the type itself, not to any instance. It is accessed via the type name (e.g., `ClassName.MemberName`) rather than through an instance. All instances share the same shared member. Instance members require an object reference and have separate state per instance.

- id: vbnet-oop-04
  answer: |
    `Me` refers to the current instance. `MyBase` refers to the base class implementation (used to call base members). `MyClass` refers to the current class's implementation, bypassing any overrides in derived classes. `MyClass` differs from `Me` when a method is overridden: `Me` calls the most-derived override, while `MyClass` calls the current class's version.

- id: vbnet-convarr-01
  answer: |
    `CType` performs general conversions between compatible types (including user-defined conversions). `DirectCast` performs a direct cast requiring an inheritance or interface relationship — it is faster because no conversion logic runs, but throws `InvalidCastException` on failure. `TryCast` is like `DirectCast` but returns `Nothing` on failure instead of throwing. Use `DirectCast`/`TryCast` for reference-type casts; `CType` for numeric/string conversions.

- id: vbnet-convarr-02
  answer: |
    A widening conversion preserves all data (e.g., `Integer` to `Long`) — it is always safe. A narrowing conversion may lose data (e.g., `Long` to `Integer`) — it requires an explicit cast. `Option Strict On` allows implicit widening conversions but disallows implicit narrowing conversions, forcing the programmer to acknowledge potential data loss.

- id: vbnet-convarr-03
  answer: |
    Yes, VB arrays are 0-based by default. Resize while keeping contents with `ReDim Preserve` (e.g., `ReDim Preserve arr(newSize)`). In `Dim a(n)`, `n` is the upper bound, so the array has `n + 1` elements (indices 0 through `n`).

- id: vbnet-convarr-04
  answer: |
    `CInt`, `CStr`, `CDbl` etc. are explicit conversion functions that convert a value to the named type. Use `&` (not `+`) for string concatenation because `&` always performs string concatenation (converting operands to strings), while `+` can throw an error or produce unexpected results when one operand is a string and the other is numeric.
