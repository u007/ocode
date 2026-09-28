- id: vbnet-syntax-01
  answer: |
    Variables are declared with `Dim` (e.g., `Dim x As Integer`). Statements are terminated by a newline — no semicolon or other terminator is needed. To continue one statement across multiple lines, place a space followed by an underscore (` _`) at the end of the line.

- id: vbnet-syntax-02
  answer: |
    `Option Strict` controls type safety: when `On`, it disallows implicit narrowing conversions and late binding, requiring explicit casts. `Option Explicit` requires all variables to be declared before use. `Option Infer` allows the compiler to infer the type from the initializer when `Dim` is used without an `As` clause. `Option Strict On` matters because it catches potential runtime errors (like data loss from narrowing conversions) at compile time.

- id: vbnet-syntax-03
  answer: |
    A `Module` is a static container — all its members are implicitly `Shared`, it cannot be instantiated, and it cannot inherit from or implement interfaces. A `Class` is a reference type that can be instantiated, supports inheritance, interface implementation, and can have both instance and shared members.

- id: vbnet-syntax-04
  answer: |
    A `Sub` performs an action and does not return a value. A `Function` performs an action and returns a value. A `Function` returns a value via the `Return` statement (or by assigning a value to the function name in older code).

- id: vbnet-props-01
  answer: |
    An auto-implemented property is declared without an explicit backing field or custom logic — the compiler generates a private backing field automatically (e.g., `Public Property Name As String`). A full property has an explicit backing field with custom `Get`/`Set` logic, allowing validation, computation, or other code in the accessors.

- id: vbnet-props-02
  answer: |
    The backing field of an auto-implemented property is compiler-generated and its name is not directly accessible in source code (it appears as `_PropertyName` in IL). You initialize an auto-property with a default value using an initializer: `Public Property Name As String = "default"`.

- id: vbnet-props-03
  answer: |
    A read-only property is declared with `ReadOnly` before `Property` (e.g., `ReadOnly Property Count As Integer`). A write-only property is declared with `WriteOnly`. Yes, an auto-implemented property can be `ReadOnly` (supported since Visual Basic 14 / VS 2015), in which case it can only be set in the constructor or via an initializer.

- id: vbnet-props-04
  answer: |
    A default property is the property that is accessed when no property name is specified on an object (declared with the `Default` keyword). The constraint is that a default property must have at least one parameter — it cannot be parameterless.

- id: vbnet-null-01
  answer: |
    `Nothing` is the default value for a type. For a reference type, `Nothing` means a null reference (no object). For a value type, `Nothing` means the default value of that type (e.g., 0 for `Integer`, `False` for `Boolean`). For a nullable value type, `Nothing` means a null value (`HasValue` is `False`).

- id: vbnet-null-02
  answer: |
    Declare a nullable value type with `?` or `Nullable(Of T)`: `Dim x As Integer?` or `Dim x As Nullable(Of Integer)`. Check it with `If x.HasValue Then ...` and read its value with `x.Value`. You can also use `If(x, defaultValue)` for null-coalescing.

- id: vbnet-null-03
  answer: |
    `If(a, b)` is the null-coalescing operator — it returns `a` if `a` is not `Nothing`, otherwise `b`. It short-circuits (only evaluates the needed branch) and is type-safe. `IIf(a, b, c)` is a legacy function that evaluates all three arguments before returning, which can cause side effects or errors. `If` is preferred because it short-circuits, avoids unnecessary evaluation, and provides better type inference.

- id: vbnet-null-04
  answer: |
    `Is Nothing` / `IsNot Nothing` use reference equality, which is the correct way to test whether a reference is null. Using `= Nothing` can be problematic because the `=` operator can be overloaded for a type, leading to unexpected behavior rather than a true null check.

- id: vbnet-errors-01
  answer: |
    Structure: `Try` ... `Catch ex As Exception` ... `Finally` ... `End Try`. The `Try` block contains code that might throw. The `Catch` block handles exceptions. The `Finally` block runs regardless of whether an exception was thrown or caught — it executes even if an exception propagates unhandled, making it ideal for cleanup code.

- id: vbnet-errors-02
  answer: |
    `Catch ex As Exception When condition` filters exceptions by a boolean condition, so the catch block only executes when the condition is true. This is preferred over catching all exceptions and re-checking inside the block because it avoids the overhead of entering and exiting the catch block for non-matching exceptions, and it keeps the filtering logic declarative.

- id: vbnet-errors-03
  answer: |
    `Throw` (with no argument) rethrows the current exception while preserving the original stack trace. `Throw ex` throws the exception object but resets the stack trace to the current location, losing the original source of the error. Use `Throw` when you want to preserve the full stack trace for debugging.

- id: vbnet-errors-04
  answer: |
    `On Error Goto` / `Err` is the legacy, unstructured error handling from VB6. It uses labels and the `Err` object. Structured `Try/Catch` is preferred because it is type-safe, provides better performance, supports exception filtering, and produces more maintainable code with clear separation of normal and error-handling logic.

- id: vbnet-linq-01
  answer: |
    Shape: `From x In collection Where condition Select projection`. Example: `Dim results = From p In people Where p.Age > 18 Select p.Name`. Keywords: `From` (source), `Where` (filter), `Select` (projection).

- id: vbnet-linq-02
  answer: |
    Group and aggregate with `Group By ... Into`. Example: `Group p By p.Department Into g = Group` then `Select Department, Count = g.Count()`. The `Into` clause names the group result, and aggregate functions like `Count()`, `Sum()`, `Average()` are applied to the group.

- id: vbnet-linq-03
  answer: |
    The `Aggregate` query keyword performs a custom accumulation over a collection (e.g., `Aggregate x Into Sum(x.Value)`). `From` defines the data source and range variables. `Aggregate` is used for folding/reducing operations, while `From` is the starting point that establishes the source.

- id: vbnet-linq-04
  answer: |
    Deferred (lazy) execution means the query is not executed when defined — it is only executed when the result is enumerated (e.g., in a `For Each` loop). To force immediate execution, use methods like `.ToList()`, `.ToArray()`, `.Count()`, `.First()`, or `.Sum()`.

- id: vbnet-events-01
  answer: |
    `WithEvents` on a variable declaration indicates that the object can raise events that you want to handle. `Handles` on a method declaration connects that method to an event of the `WithEvents` variable. Example: `WithEvents Button1 As Button` and `Sub Button1_Click() Handles Button1.Click`.

- id: vbnet-events-02
  answer: |
    `AddHandler` dynamically connects an event handler to an event at runtime. `RemoveHandler` disconnects it. `AddressOf` creates a delegate pointing to the method. You must use `AddHandler`/`RemoveHandler` instead of `Handles` when the event or handler is not known at compile time, when you need to attach/detach dynamically, or when handling events from objects not declared `WithEvents`.

- id: vbnet-events-03
  answer: |
    Declare a custom event with `Public Event Name As EventHandler` (or a custom delegate type). Raise it with `RaiseEvent Name(sender, e)`. Handlers receive arguments through the event delegate's parameters — typically `sender` (the object raising the event) and `e` (an `EventArgs` subclass containing event data).

- id: vbnet-events-04
  answer: |
    Choose `WithEvents`/`Handles` when the event source and handler are known at compile time and you want declarative, designer-friendly wiring (common in UI code). Choose `AddHandler`/`RemoveHandler` when you need dynamic subscription, the event source is determined at runtime, you need to attach/detach multiple handlers, or the object is not declared `WithEvents`.

- id: vbnet-oop-01
  answer: |
    `Inherits` specifies the base class a class derives from — a class can inherit from only one base class. `Implements` specifies that a class implements an interface — a class can implement multiple interfaces.

- id: vbnet-oop-02
  answer: |
    `Overridable` allows a member to be overridden in derived classes. `Overrides` overrides an inherited `Overridable` or `MustOverride` member. `MustOverride` requires derived classes to override the member (the class must be `MustInherit`). `MustInherit` makes a class abstract — it cannot be instantiated directly. `NotOverridable` prevents further overriding in derived classes.

- id: vbnet-oop-03
  answer: |
    `Shared` means the member belongs to the type itself rather than to any instance. It is accessed via the class name (e.g., `ClassName.Member`) rather than through an instance variable. Instance members require an object instance to be accessed.

- id: vbnet-oop-04
  answer: |
    `Me` refers to the current instance. `MyBase` refers to the base class of the current instance (used to call base class members). `MyClass` refers to the current class, bypassing any overrides in derived classes. `MyClass` differs from `Me` when a method is overridden — `MyClass.Method()` calls the current class's version even if a derived class overrides it, while `Me.Method()` calls the most-derived override.

- id: vbnet-convarr-01
  answer: |
    `CType` is a general-purpose conversion that works between compatible types (including user-defined conversions). `DirectCast` performs a direct cast that requires the types to be related by inheritance or interface implementation — it is faster but less flexible. `TryCast` attempts a cast and returns `Nothing` if it fails (reference types only), avoiding an exception.

- id: vbnet-convarr-02
  answer: |
    A widening conversion preserves all data (e.g., `Integer` to `Long`) — it is always safe. A narrowing conversion may lose data (e.g., `Long` to `Integer`) — it requires an explicit cast. `Option Strict On` allows implicit widening conversions but requires explicit casts for narrowing conversions.

- id: vbnet-convarr-03
  answer: |
    Yes, VB arrays are 0-based. Resize while keeping contents with `ReDim Preserve`. In `Dim a(n)`, the bound `n` is the upper index, so the array has `n + 1` elements (indices 0 through n).

- id: vbnet-convarr-04
  answer: |
    `CInt`, `CStr`, `CDbl` are conversion functions that convert a value to `Integer`, `String`, or `Double` respectively. Use `&` (not `+`) for string concatenation because `&` always performs string concatenation (converting operands to strings), while `+` can cause type mismatch errors or unexpected numeric addition when one operand is a string and the other is numeric.
