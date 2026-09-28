- id: vbnet-syntax-01
  answer: |
    Variables are declared with `Dim` (e.g., `Dim x As Integer = 5`). Statements are terminated by a newline (no semicolon needed). To continue one statement across lines, use a space followed by an underscore `_` (line continuation character) at the end of each line.

- id: vbnet-syntax-02
  answer: |
    `Option Strict` controls implicit type conversions: `On` disallows narrowing implicit conversions and late binding; `Off` (default in older VB) allows them. `Option Explicit` requires all variables to be declared before use (`On` is the modern default). `Option Infer` enables local type inference — the compiler infers the variable's type from its initializer (e.g., `Dim x = 5` makes x an Integer). `Option Strict On` matters because it catches type mismatch and data-loss errors at compile time rather than runtime.

- id: vbnet-syntax-03
  answer: |
    A `Module` is a sealed (NotInheritable) container whose members are implicitly `Shared` — it cannot be instantiated or inherited from, and you refer to its members directly (e.g., `ModuleName.Method()`). A `Class` is a reference type that can be instantiated, inherited from, and implements interfaces; its members are instance-level by default.

- id: vbnet-syntax-04
  answer: |
    A `Sub` is a method that performs an action but returns no value. A `Function` returns a value — you use the `Return` statement (e.g., `Return result`) or, in older VB, assign to the function name. Functions can also use `Exit Function` to return early.

- id: vbnet-props-01
  answer: |
    An auto-implemented property is declared in one line without a Get/Set body (e.g., `Public Property Name As String`). The compiler auto-generates a private backing field and trivial Get/Set accessors. A full property explicitly defines a backing field and custom logic inside Get and Set, allowing validation, computation, or other code to run on access.

- id: vbnet-props-02
  answer: |
    The backing field of an auto-implemented property is compiler-generated with a name like `_PropertyName` or a name that is not directly accessible in source. You initialize an auto-property with a default value by assigning it inline: `Public Property Count As Integer = 0`. For read-only auto-properties you can also assign in the constructor (VB 14+ / VS 2015+).

- id: vbnet-props-03
  answer: |
    A read-only property is declared with only a `Get` accessor (either `Public ReadOnly Property X As T` with auto-implementation, or a full property with only Get). A write-only property has only a `Set` accessor (`WriteOnly Property`). Yes, an auto-implemented property can be `ReadOnly` — the compiler still generates a backing field that can only be assigned in the constructor.

- id: vbnet-props-04
  answer: |
    A default property is the property used when no property name is specified after an object reference (e.g., `Dim v = obj(0)` for an indexer). The constraint is that a default property must take at least one parameter (it must be parameterized). In C# terms this corresponds to an indexer; in VB you mark it with the `Default` keyword.

- id: vbnet-null-01
  answer: |
    `Nothing` in VB is equivalent to C#'s `null` for reference types (the member `Nothing` means "no object reference"). For value types, `Nothing` means the default value of that type (e.g., 0 for Integer, False for Boolean) — it is not "null" in the reference sense but rather the zero-initialized default. For nullable value types (`Integer?`), `Nothing` does represent a null state (HasValue = False).

- id: vbnet-null-02
  answer: |
    Declare a nullable value type with `?` or `Of` syntax: `Dim x As Integer?` or `Dim x As Nullable(Of Integer)`. Check it with `.HasValue` or `If(x.HasValue, ...)`. Read the value with `.Value` (throws if no value) or with the null-coalescing operator: `If(x, defaultValue)`.

- id: vbnet-null-03
  answer: |
    `If(a, b)` (the binary/null-coalescing form) is a true ternary/If operator: when `a` is a nullable and `b` is its underlying type, `If(a, b)` returns a.Value if a has a value, otherwise b — and it short-circuits (only one branch evaluates). `IIf(a, b, c)` is a regular function call that always evaluates both b and c before the function runs. `If` is preferred because it is type-safe, short-circuits (avoiding side effects or NullReferenceExceptions), and handles null-coalescing naturally.

- id: vbnet-null-04
  answer: |
    `Is Nothing` / `IsNot Nothing` use reference equality, which is the correct way to test for null. Using `= Nothing` invokes the overloaded `=` operator if the type defines one, which can produce unexpected results. `Is` performs a reference comparison that is unambiguous and consistent regardless of operator overloading.

- id: vbnet-errors-01
  answer: |
    Structure: `Try` ... `[Catch ex As SomeException When condition] ... [Catch ex As Exception] ... [Finally] ... End Try`. The `Finally` block runs always — whether an exception occurred, was caught, or no exception happened at all (including after `Return` or `Exit`). It is used for cleanup (closing files, releasing resources).

- id: vbnet-errors-02
  answer: |
    A `Catch ... When` filter lets you catch an exception only when a Boolean condition is true (e.g., `Catch ex As IOException When ex.HResult = -2147024864`). It is preferred over catching broadly and re-checking inside the block because: (1) it avoids entering the Catch block at all when the condition is false (so you don't accidentally swallow unrelated exceptions), (2) the original stack trace is preserved for the runtime to potentially re-evaluate outer handlers, and (3) it keeps error-handling logic declarative.

- id: vbnet-errors-03
  answer: |
    Bare `Throw` rethrows the current exception preserving the original stack trace. `Throw ex` throws the same exception object but resets the stack trace to the current location, making debugging harder. Use bare `Throw` when you want to log-and-rethrow or just rethrow without modification. Use `Throw ex` only when you intentionally want to wrap it in a new exception (and typically you'd use `Throw New Exception(..., ex)` with InnerException instead).

- id: vbnet-errors-04
  answer: |
    Legacy error handling uses `On Error Goto <label>` to jump to an error-handling routine, `On Error Resume Next` to ignore errors and continue, and the `Err` object (Err.Number, Err.Description) to inspect the last error. Structured `Try/Catch/Finally` is preferred because: it is type-safe, supports filtering, preserves stack traces, handles cleanup via Finally, works across all .NET languages, and avoids the spaghetti-flow of Goto-based error handling.

- id: vbnet-linq-01
  answer: |
    Shape: `From` ... `Where` ... `Select`. Example:
    ```vb
    Dim query = From c In customers
                Where c.City = "London"
                Select c.Name, c.Phone
    ```
    Keywords: `From` (source), `Where` (filter), `Select` (projection).

- id: vbnet-linq-02
  answer: |
    ```vb
    Dim query = From p In products
                Group By Category = p.Category Into Group
                Select Category, Count = Group.Count()
    ```
    `Group By key Into alias` creates a grouping; `Into` defines an alias for the group (or aggregate). You can then apply aggregate functions like `Count()`, `Sum()`, `Average()`, `Max()`, `Min()`.

- id: vbnet-linq-03
  answer: |
    The `Aggregate` query keyword in VB applies an accumulator function over a sequence — it's the query-syntax equivalent of `Enumerable.Aggregate`. It lets you specify a seed and a function to combine elements. Unlike `From` (which establishes the data source/range variable), `Aggregate` performs the actual folding computation over that source.

- id: vbnet-linq-04
  answer: |
    Deferred (lazy) execution means the LINQ query is not executed when it is defined — only when you iterate over it (e.g., `For Each`, `ToList()`, `ToArray()`, `Count()`, `First()`). This allows composing queries efficiently and avoids unnecessary work. To force immediate execution, use a terminal operator like `ToList()`, `ToArray()`, `Count()`, `First()`, `Sum()`, etc.

- id: vbnet-events-01
  answer: |
    `WithEvents` declares an object variable whose events you want to handle (must be a field/local of a class-level or module-level scope). The `Handles` clause on a method links that method to the event of the WithEvents variable. Example:
    ```vb
    Private WithEvents btn As Button
    Private Sub btn_Click(sender As Object, e As EventArgs) Handles btn.Click
    ```
    The handler signature must match the event's delegate signature.

- id: vbnet-events-02
  answer: |
    `AddHandler` dynamically connects an event to a delegate/method at runtime; `RemoveHandler` disconnects it. `AddressOf` produces a delegate pointing to the named method (e.g., `AddHandler obj.EventName, AddressOf MyHandler`). You must use them instead of `Handles` when: the event object isn't known at compile time, you need to add/remove handlers dynamically, or you're working with events on objects that aren't declared WithEvents.

- id: vbnet-events-03
  answer: |
    Declare a custom event with: `Public Event MyEvent As EventHandler(Of MyEventArgs)` (or a custom delegate type). Raise it with: `RaiseEvent MyEvent(Me, New MyEventArgs(...))`. Handlers receive the sender (first argument) and the event args object (second argument), matching the delegate signature. You can also declare custom event accessors with `Custom Event` using AddHandler/RemoveHandler/RaiseEvent.

- id: vbnet-events-04
  answer: |
    Use `WithEvents`/`Handles` when the event source is known at design time and you want compile-time checking and clean declarative syntax. Use `AddHandler`/`RemoveHandler` when you need dynamic wiring/unwiring at runtime, when the object isn't available at compile time, or when multiple handlers need to be added/removed conditionally.

- id: vbnet-oop-01
  answer: |
    `Inherits` specifies the single base class a class derives from — a class can inherit from exactly one class (single inheritance). `Implements` specifies that a class implements an interface — a class can implement multiple interfaces. Interfaces define contracts; the implementing class must provide all members.

- id: vbnet-oop-02
  answer: |
    `Overridable`: a member can be overridden in derived classes. `Overrides`: a derived class provides its own implementation of an Overridable/MustOverride base member. `MustOverride`: an abstract member that derived classes must override (the class itself must be `MustInherit`/abstract). `MustInherit`: an abstract class that cannot be instantiated directly — only derived from. `NotOverridable`: prevents further overriding (default for Overrides members).

- id: vbnet-oop-03
  answer: |
    `Shared` means the member belongs to the type itself rather than to any instance. It is accessed via the class/module name (e.g., `ClassName.Method()` or `ClassName.Field`) without creating an instance. Instance members require an object reference (`obj.Method()`). Shared members share a single copy across all instances and cannot access instance members directly.

- id: vbnet-oop-04
  answer: |
    `Me` refers to the current instance and uses virtual dispatch (calls the most derived override). `MyBase` refers to the base class implementation, used to call a base member that's been overridden (e.g., `MyBase.ToString()`). `MyClass` refers to the current class's implementation and bypasses virtual dispatch — it calls the version defined in the current class even if a derived class overrides it. `MyClass` differs from `Me` when a virtual member is overridden: `Me` calls the derived override; `MyClass` calls the current class's version.

- id: vbnet-convarr-01
  answer: |
    `CType`: general-purpose conversion that works for widening and narrowing conversions, intrinsic types, and types with conversion operators. `DirectCast`: requires an inheritance or interface-implementation relationship (no conversion operators); faster because no conversion logic runs, but throws InvalidCastException if types aren't related. `TryCast`: like DirectCast but returns `Nothing` on failure instead of throwing; only works with reference types.

- id: vbnet-convarr-02
  answer: |
    A widening conversion preserves all data (e.g., Integer → Double) and can be done implicitly. A narrowing conversion may lose data (e.g., Double → Integer) and requires an explicit cast. With `Option Strict On`, narrowing implicit conversions are disallowed at compile time — you must use an explicit conversion (CType, DirectCast, CInt, etc.). With `Option Strict Off`, narrowing implicit conversions are allowed at compile time but may fail at runtime.

- id: vbnet-convarr-03
  answer: |
    Yes, VB arrays are 0-based (index 0 to n-1) when declared normally. To resize while keeping contents, use `ReDim Preserve arr(newSize)` — only the last dimension can be preserved. In `Dim a(n)`, the `n` is the upper bound (not the count), so the array has `n + 1` elements (indices 0 through n).

- id: vbnet-convarr-04
  answer: |
    `CInt`, `CStr`, `CDbl` (and `CLng`, `CDec`, `CBool`, etc.) are explicit type-conversion functions that convert a value to the named type (with rounding/parsing as appropriate). Use `&` for string concatenation instead of `+` because `+` is ambiguous — it can mean addition or concatenation depending on operand types, and with `Option Strict On` it disallows `String + Numeric`. `&` always concatenates (converting operands to String), making intent clear and avoiding accidental arithmetic.
