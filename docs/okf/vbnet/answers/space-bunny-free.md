- id: vbnet-syntax-01
  answer: |
    Use `Dim` to declare a variable, optionally specifying its type and initial value:

    Dim count As Integer = 0
    Dim name As String = "Ada"

    In VB.NET, the end of a line normally terminates a statement; a colon can also separate statements. An explicit line-continuation underscore (`_`) can continue a statement:

    Dim total As Integer = _
        firstValue + secondValue

    Modern VB.NET also supports implicit line continuation in many syntactic positions, such as after a binary operator or an opening parenthesis.

- id: vbnet-syntax-02
  answer: |
    `Option Strict On` restricts implicit conversions that could lose data and disallows late-bound operations. Safe widening conversions remain allowed.

    `Option Explicit On` requires variables and other identifiers to be explicitly declared, preventing accidental undeclared variables.

    `Option Infer On` infers a local variable's type from its initializer, so `Dim count = 10` declares an `Integer`.

    `Option Strict On` matters because it catches potentially dangerous narrowing and implicit conversions at compile time rather than allowing silent truncation, overflow, or unintended late-bound behavior.

- id: vbnet-syntax-03
  answer: |
    A `Module` cannot be instantiated; it is primarily a container for shared procedures, constants, fields, types, and other module-level declarations. Its procedure and data members are effectively shared.

    A `Class` describes object behavior and can be instantiated, has instance members, supports inheritance, and can implement interfaces.

- id: vbnet-syntax-04
  answer: |
    A `Sub` is a procedure that does not return a value. A `Function` returns a value and normally declares a return type with `As`.

    A function can assign its result to the function's return value or use `Return expression`:

    Function DoubleIt(ByVal value As Integer) As Integer
        Return value * 2
    End Function

- id: vbnet-props-01
  answer: |
    An auto-implemented property has no explicit `Get` or `Set` block. The compiler creates a private backing field and the accessors:

    Public Property Name As String

    A full property uses a manually declared backing field and explicit accessors, allowing validation, transformation, or other control during access:

    Private _name As String

    Public Property Name As String
        Get
            Return _name
        End Get
        Set(value As String)
            _name = value
        End Set
    End Property

- id: vbnet-props-02
  answer: |
    The compiler-generated backing field for `Public Property Name As String` is named `<Name>k__BackingField`. It is an implementation detail and should not be referenced directly.

    An auto-property can be initialized in its declaration:

    Public Property Count As Integer = 0
    Public Property Name As String = ""

    It can also be assigned in a constructor or through an object initializer where assignment is permitted.

- id: vbnet-props-03
  answer: |
    A read-only property has a getter but no setter, while a write-only property has a setter but no getter:

    Private _createdAt As DateTime

    Public ReadOnly Property CreatedAt As DateTime
        Get
            Return _createdAt
        End Get
    End Property

    Public WriteOnly Property Secret As String
        Set(value As String)
            ' Store value without exposing a getter
        End Set
    End Property

    Yes, an auto-implemented property can be `ReadOnly`, but it can be assigned only during initialization or by the containing type's constructor:

    Public ReadOnly Property Id As Integer = 42

- id: vbnet-props-04
  answer: |
    A default property is a property accessed without explicitly naming `Item`, usually through indexing:

    obj(0)

    It is commonly declared as `Default Property Item(...)`. A type can have at most one default property, and the default property must accept at least one index parameter.

- id: vbnet-null-01
  answer: |
    `Nothing` represents the absence of a value in a VB.NET reference: for a reference type, it means the variable contains no object reference, equivalent to `null`.

    For a non-nullable value type, assigning `Nothing` produces that type's default value, such as `0` for `Integer`; it does not make the value type itself absent.

    For a nullable value type such as `Integer?`, `Nothing` specifically means the nullable currently contains no value and its `HasValue` property is `False`.

- id: vbnet-null-02
  answer: |
    Append `?` or use `Nullable(Of T)`:

    Dim age As Integer?
    Dim score As Nullable(Of Double)

    Check `.HasValue` before reading `.Value`:

    If age.HasValue Then
        Dim value As Integer = age.Value
    End If

    For a safe default, use `.GetValueOrDefault()` or its overload, or the `If` operator:

    Dim value As Integer = age.GetValueOrDefault()
    Dim displayAge As Integer = If(age, 0)

- id: vbnet-null-03
  answer: |
    `If(a, b)` is the binary conditional/null-coalescing operator. It evaluates `a` and returns it if it is `True` and not `Nothing` or `DBNull`; otherwise it returns `b`.

    `IIf(a, b, c)` is a three-argument function that tests `a` and returns either `b` or `c`, but it evaluates `b` and `c` before calling the function. It can therefore cause unnecessary work or side effects and may return `Nothing` or raise a conversion error when `a` is not Boolean.

    `If` is preferred because it short-circuits, has strongly typed branches, and behaves predictably for null-coalescing and conditional selection.

- id: vbnet-null-04
  answer: |
    `Is Nothing` and `IsNot Nothing` test whether a reference is the special `Nothing` value using the language's reference test.

    Using `= Nothing` can invoke a user-defined equality operator or value-equality semantics, so two different objects might compare equal, or the comparison might be invalid. `Is` cannot be redefined and therefore reliably expresses reference absence.

- id: vbnet-errors-01
  answer: |
    A structured exception-handling block has this structure:

    Try
        ' Statements that may throw
    Catch ex As Exception
        ' Handle matching exceptions
    Finally
        ' Cleanup
    End Try

    `Finally` runs after the `Try` and any applicable `Catch` code, whether execution completes normally, returns, or an exception is propagating. It runs before control leaves the `Try` construct and is used for cleanup that must occur in all cases.

- id: vbnet-errors-02
  answer: |
    A `Catch ... When` filter selects whether a matching exception should be handled:

    Catch ex As IOException When ex.HResult = 12345
        ' Handle only this case
    End Catch

    The filter is evaluated when the exception is considered. If it returns `True`, that catch block handles the exception; if it returns `False`, the same exception proceeds to later catch clauses.

    Using a filter is clearer and safer than catching a broad exception and rechecking it inside the block, and it avoids unnecessary handling or rethrowing.

- id: vbnet-errors-03
  answer: |
    Inside a catch block, a bare `Throw` rethrows the current exception:

    Catch ex As Exception
        Log(ex)
        Throw
    End Catch

    `Throw ex` throws the exception object again as though it were a new exception. It can reset or obscure the original stack-trace location and loses some of the value of preserving the original throw site.

    Use bare `Throw` when the goal is to rethrow so that the original exception, stack trace, and diagnostic context are retained.

- id: vbnet-errors-04
  answer: |
    `On Error GoTo label` is VB's legacy, unstructured error-handling mechanism. After a runtime error, execution transfers to a label, where code typically examines `Err.Number`, `Err.Description`, and related `Err` members. Related forms include `On Error Resume Next`, `Resume`, and `On Error GoTo 0`.

    Modern `Try/Catch/Finally` associates handling and cleanup with explicit lexical blocks, supports typed exceptions and filters, preserves exception information more reliably, and makes control flow easier to understand.

- id: vbnet-linq-01
  answer: |
    A basic VB query filters and projects like this:

    Dim adultNames =
        From person In people
        Where person.Age >= 18
        Select person.Name

    The principal keywords are `From ... In`, `Where`, and `Select`. Queries can also use clauses such as `Let`, `Order By`, `Group By`, and `Join`.

- id: vbnet-linq-02
  answer: |
    The `Group By ... Into` form groups the source and exposes each group for aggregation:

    Dim totals =
        From order In orders
        Group order By order.CustomerId Into CustomerOrders = Group
        Select CustomerOrders.Key,
               CustomerOrders.Count(),
               CustomerOrders.Sum(Function(o) o.Amount)

    `Group` creates a collection of elements for each key. After the `Into` alias, methods such as `Count`, `Sum`, `Average`, and `Max` can aggregate the group.

- id: vbnet-linq-03
  answer: |
    `Aggregate` is a terminal query operator that reduces a sequence to a single result by repeatedly combining elements with an accumulator, similar to `Enumerable.Aggregate`.

    For example, the method form is:

    Dim total = amounts.Aggregate(0, Function(sum, amount) sum + amount)

    `From` begins a query by introducing a source sequence and the range variable over its elements. `Aggregate` consumes a sequence and produces one final accumulated result; it is a reduction rather than an enumeration clause.

- id: vbnet-linq-04
  answer: |
    Deferred or lazy execution means a LINQ query does not enumerate its source when the query expression is constructed. It builds a query or iterator that executes when enumerated, so enumerating it again can run the query again and reflect later source changes.

    Force materialization with an immediate operator such as `ToList`, `ToArray`, or `ToDictionary`:

    Dim result = (From item In items Select item.Name).ToList()

- id: vbnet-events-01
  answer: |
    `WithEvents` declares a field that can have its events wired by the compiler:

    Private WithEvents worker As Worker

    Private Sub Worker_Completed(sender As Object, e As EventArgs) _
        Handles worker.Completed
    End Sub

    The `Handles` clause, placed in the procedure declaration, connects the named procedure to the event on the `WithEvents` object. The compiler performs the equivalent event subscription automatically, so no explicit `AddHandler` is required.

- id: vbnet-events-02
  answer: |
    `AddHandler` attaches a procedure to an event, and `RemoveHandler` detaches it:

    AddHandler worker.Completed, AddressOf Worker_Completed
    RemoveHandler worker.Completed, AddressOf Worker_Completed

    `AddressOf` creates a delegate identifying the procedure; it does not execute the procedure immediately.

    Use `AddHandler` and `RemoveHandler` when subscribing dynamically, conditionally, or at runtime, when the event source is not a `WithEvents` field, or when handlers must be explicitly balanced and removed. `Handles` is a compile-time declaration and cannot express arbitrary runtime subscription changes.

- id: vbnet-events-03
  answer: |
    Declare a custom event in a class and raise it with `RaiseEvent`:

    Public Class Publisher
        Public Event Changed As EventHandler

        Public Sub UpdateValue()
            RaiseEvent Changed(Me, EventArgs.Empty)
        End Sub
    End Class

    A handler receives the sender and an `EventArgs` instance:

    Private Sub OnChanged(sender As Object, e As EventArgs) _
        Handles publisher.Changed
    End Sub

    A strongly typed event such as `EventHandler(Of MyEventArgs)` gives handlers a specific `EventArgs` subtype instead of the base `EventArgs` type.

- id: vbnet-events-04
  answer: |
    Choose `WithEvents` and `Handles` when the event source is declared as a `WithEvents` field, the connection is fixed at compile time, and the subscription should last for that field's lifetime. They are especially convenient for form and control events and require no manual detachment code.

    Choose `AddHandler` and `RemoveHandler` for dynamic, conditional, temporary, or runtime-selected event sources, or when subscriptions must be explicitly removed—for example, when a long-lived object is being disposed. A `Handles` clause cannot replace that explicit lifecycle management.

- id: vbnet-oop-01
  answer: |
    `Inherits` specifies a base class, while `Implements` specifies one or more interfaces whose contract the class fulfills:

    Class Customer
        Inherits Person
        Implements IDisposable, IComparable(Of Customer)
    End Class

    A VB.NET class can inherit from at most one base class but can implement any number of interfaces. Interfaces can be listed together in one `Implements` clause.

- id: vbnet-oop-02
  answer: |
    `Overridable` allows a derived class to override a member.

    `Overrides` replaces an inherited virtual or abstract member in a derived class.

    `MustOverride` declares an abstract member with no implementation; every concrete derived class must override it.

    `MustInherit` declares an abstract class that cannot be instantiated directly and must have a derived class.

    `NotOverridable` prevents further overriding. In VB.NET, members are generally non-overridable unless virtual behavior is explicitly enabled with `Overridable` or `MustOverride`.

- id: vbnet-oop-03
  answer: |
    A `Shared` member belongs to the type rather than to an individual instance. There is one common copy, and all instances access that same member.

    It is accessed through the type name:

    ClassName.SharedMember

    An instance member belongs to each object and is normally accessed through an instance:

    instance.Member

    Shared mutable state requires care because it is shared across threads as well as instances.

- id: vbnet-oop-04
  answer: |
    `Me` refers to the current object instance.

    `MyBase` refers to the current class's immediate base class and is commonly used to access inherited or protected members or call the base implementation directly.

    `MyClass` is not an object instance; it identifies the current class as written in the source and suppresses virtual override dispatch.

    The distinction is visible when a derived class overrides a method. `Me.Method()` may dispatch to the derived override, while `MyClass.Method()` calls the implementation declared in the current class. `MyBase.Method()` directly targets the base-class implementation.

- id: vbnet-convarr-01
  answer: |
    `CType(value, TargetType)` performs an explicit conversion, including primitive, object, and applicable user-defined conversions. It can throw if the conversion is impossible or overflows.

    `DirectCast(value, TargetType)` performs a conversion whose relationship is known at compile time, such as an interface cast, reference conversion, or numeric narrowing. It throws `InvalidCastException` if the object is incompatible.

    `TryCast(value, TargetType)` performs a tested reference or interface conversion and returns `Nothing` when that conversion is not possible. Use it when a failed reference conversion is an expected outcome rather than an exceptional condition.

- id: vbnet-convarr-02
  answer: |
    A widening conversion moves to a type that can represent the original range without loss, such as `Integer` to `Long` or `Byte` to `Integer`.

    A narrowing conversion can lose information or overflow, such as `Long` to `Integer` or `Double` to `Integer`.

    With `Option Strict On`, safe widening conversions may remain implicit, while narrowing or otherwise unsafe conversions must be explicit, for example with `CType` or `DirectCast`. The compiler then catches potentially lossy conversions instead of allowing them silently.

- id: vbnet-convarr-03
  answer: |
    VB.NET arrays are 0-based by default, although an explicit lower bound such as `Dim a(1 To n)` is possible.

    `Dim a(n)` declares an array whose lower bound is 0 and upper bound is `n`, so it contains `n + 1` elements.

    A one-dimensional array can be resized while retaining its contents with:

    ReDim Preserve a(UBound(a) + 1)

    Alternatively:

    Array.Resize(a, a.Length + 1)

    `ReDim Preserve` can resize only the last dimension of a multidimensional array, and `Array.Resize` produces an array with a lower bound of zero.

- id: vbnet-convarr-04
  answer: |
    `CInt(value)` explicitly converts a value to `Integer`, `CStr(value)` converts it to `String`, and `CDbl(value)` converts it to a `Double`. `CInt` uses rounding, and text conversions can fail if the text is not valid for the target type.

    Use `&` for string concatenation because it explicitly converts operands to strings and concatenates them:

    Dim message As String = "Count: " & count

    `+` is an addition operator. With numeric operands it performs arithmetic, and with strings it can have legacy coercion behavior or invoke overloaded operators, making it unsuitable when string concatenation is intended.
