# vbnet knowledge benchmark — answers
# Model: opencode-go/muse-spark-1.2   (provider-stripped id: muse-spark-1.2)
# CLOSED-BOOK via ocode2 run -dir /tmp/kaizen-vbnet-answer (empty dir, no corpus access). corpus_rev 1.

```yaml
- id: vbnet-syntax-01
  answer: |
    Variable declaration: `Dim x As Integer = 5`, `Dim s As String`, `Private _f As Double`, `Dim y = 10` (with Option Infer). `Dim` is most common; also `Public`/`Private`/`Const`.
    Statement termination: statements are terminated by newline - VB is line-oriented. `:` (colon) can put multiple statements on one physical line: `x = 1 : y = 2`.
    Line continuation: explicit: space + underscore ` _` at end of line then newline. Since VB 2010 implicit line continuation after `,` `&` `+` `AndAlso` `OrElse` `=` `(` `{` etc., so `_` is often not needed. Example: `Dim r = LongMethod(arg1, _` newline `arg2)`

- id: vbnet-syntax-02
  answer: |
    `Option Explicit` - Controls whether variables must be declared. `On` (default) = compiler error for undeclared variable/implicit declaration. `Off` = undeclared variables become `Object`.
    `Option Strict` - Controls strict typing. `On` = disallows implicit narrowing conversions, implicit conversions Object->typed, and late binding. `Off` (=default historically) allows them with runtime checks.
    `Option Infer` - Controls local type inference for `Dim x = ...` without `As` clause. `On` (default since 2008) = infers type from initializer; `Off` = infers `Object`.
    `Option Strict On` matters for type-safety, performance and correctness: catches narrowing/late-binding bugs at compile time instead of InvalidCastException at runtime, avoids hidden COM/Variant-style behavior.

- id: vbnet-syntax-03
  answer: |
    `Class` = instantiable blueprint, can have instance and Shared members, constructors, can `Inherits`/`Implements`, must be instantiated with `New`.
    `Module` = essentially a `NotInheritable` `Class` with only `Shared` members, private constructor, cannot be instantiated or inherited. Members are implicitly `Shared` and can be accessed without qualification (imported as global) within its namespace. Compiled as sealed abstract class with all static members. Use for utility/extension methods and VB6 compatibility. One Module cannot be generic and cannot implement interfaces in the same way.

- id: vbnet-syntax-04
  answer: |
    `Sub` = routine that does not return a value: `Sub DoWork(x As Integer) ... End Sub` called as `DoWork(5)`.
    `Function` = routine that returns a value with a declared return type: `Function Add(a As Integer, b As Integer) As Integer ... End Function`.
    Return: `Return expr` (preferred) or legacy assignment to function name `Add = a+b` then `Exit Function`. `Return` exits immediately. Functions return `Nothing`/default if no Return executed.

- id: vbnet-props-01
  answer: |
    Auto-implemented property: `Public Property Name As String` - compiler generates hidden backing field, and trivial Get/Set. No code needed. Requires VB 2010+.
    Full/explicit property: you declare a private backing field and explicit `Get ... End Get` / `Set(value ...) ... End Set` to add logic, validation, notifications, lazy init, readOnly/writeOnly, different accessibility. Example: `Private _name As String : Public Property Name As String : Get: Return _name : End Get : Set: _name=value : End Set : End Property`.

- id: vbnet-props-02
  answer: |
    The compiler-generated backing field is named `_<PropertyName>` (underscore + property name) and is accessible only within the containing class (e.g., for `Property Foo As Integer` the field is `_Foo`). You can reference it directly inside the class, though it is normally private.
    Initialization: inline initializer: `Public Property Name As String = "default"` or `Public Property Count As Integer = 42`. Since VB 2015 this also works for `ReadOnly` auto-properties, which can then only be set inline or in the constructor/`New`.

- id: vbnet-props-03
  answer: |
    `ReadOnly Property X As Integer : Get ... End Get : End Property` (no Set) ; `WriteOnly Property X As Integer : Set(value As Integer) ... End Set : End Property` (no Get).
    Yes, an auto-implemented property can be `ReadOnly`: `Public ReadOnly Property Id As Guid` and `Public ReadOnly Property Name As String = "a"` - but because there is no setter it can only be set inline via initializer or in the constructor (`Me._Name = ...` via hidden field or assignment `Name = value` in `Sub New`). `WriteOnly` cannot be auto-implemented. You can also give different accessibility: `Public Property X As String : Get : End Get : Private Set(value) : ... : End Set`.

- id: vbnet-props-04
  answer: |
    A `Default` property is the indexer of a class - invoked without dot-name, by `instance(args)` syntax. Declared `Default Public Property Item(index As Integer) As String` ... . Only one `Default` per class allowed. Constraints: must be parameterized (takes at least one parameter), cannot be `Shared`, cannot be `Private`, cannot explicitly work with `ParamArray`? Must be instance property, name conventionally `Item`. Enables `obj(0)` instead of `obj.Item(0)` and is how indexers are done in VB (unlike C# `this[]`).

- id: vbnet-null-01
  answer: |
    `Nothing` is the default/empty value. For reference types it means null reference - no object. `Dim s As String = Nothing` implies `Is Nothing` = true.
    For value types it means default value of that type, not null: `Dim i As Integer = Nothing` => `0`, `Dim b As Boolean = Nothing` => `False`, `Dim d As Date = Nothing` => `#1/1/0001#`. Assigning `Nothing` to a value-type variable resets it to default; assigning to nullable value type (`Integer?`) sets it to null state.

- id: vbnet-null-02
  answer: |
    Nullable value type: `Dim x As Integer?` (shorthand) or `Dim x As Nullable(Of Integer)` or `Dim x As Nullable(Of Date) = Nothing`.
    Check: `If x.HasValue Then ...`, `If x Is Nothing` / `IsNot Nothing` also works, `If x IsNot Nothing Then ...`.
    Read safely: `x.Value` (throws if null), `x.GetValueOrDefault()` or `x.GetValueOrDefault(default)`, or null-coalescing `If(x, 0)` / `If(x.HasValue, x.Value, -1)`. Pattern: `If x.HasValue Then Dim v = x.Value`.

- id: vbnet-null-03
  answer: |
    `IIf(condition, truePart, falsePart)` is a legacy `Function` from Microsoft.VisualBasic: always evaluates both truePart and falsePart, returns `Object` (requires cast), no short-circuit, can cause NullReference/side effects.
    `If` operator has two forms: 1) ternary `If(cond, a, b)` - short-circuiting, strongly-typed, only evaluates chosen branch, infers return type; 2) null-coalescing `If(a, b)` - returns `a` if not Nothing else `b`. Preferred because it is short-circuited, type-safe (no Object), preserves generics/nullability, more efficient and avoids double evaluation bugs.

- id: vbnet-null-04
  answer: |
    Use `Is Nothing` / `IsNot Nothing` for reference equality because `Is` tests reference identity.
    `= Nothing` invokes `=` operator overload (`Operator =`) which may be overloaded, may do value equality, may treat `Nothing` as default value (0/""/Date) causing surprising true for value types/strings, and may throw if left side is Nothing. `Is` cannot be overloaded and correctly tests null identity. For strings, `= Nothing` etc also confuses `""` vs `Nothing`. Compiler warns with Option Strict.

- id: vbnet-errors-01
  answer: |
    ```
    Try
        ' protected code
    Catch ex As SpecificException When condition
        ' handle
    Catch ex As Exception
        ' handle other
    Finally
        ' cleanup
    End Try
    ```
    `Try` is required, `Catch` blocks zero or more in order from specific to general, optional `When` filter, optional `Finally`. `Finally` always runs after Try/Catch whether code succeeded, exception was thrown, handled, or unhandled, or after Return/Exit, for resource cleanup. Even if no Catch matches, Finally runs before propagation.

- id: vbnet-errors-02
  answer: |
    `Catch ex As IOException When ex.Message.Contains("x")` / `When` exception filter prevents entering the Catch block unless condition true; if false the exception is considered not caught and continues searching outer handlers without unwinding stack.
    Why vs `Catch` then `If` inside: filter preserves original stack trace, allows debugger to see unhandled point, avoids catching and rethrowing, avoids executing Finally/Stack unwind prematurely, and is more efficient. Used for conditional handling without losing stack diagnostics.

- id: vbnet-errors-03
  answer: |
    Inside `Catch ex As Exception`:
    `Throw` (parameterless) rethrows the original exception preserving its StackTrace and Watson info.
    `Throw ex` throws the caught variable as a new throw point, resetting StackTrace to the current line, losing original call site. This makes debugging impossible.
    Always use bare `Throw` to preserve trace; use `Throw ex` only if intentionally wrapping/resetting.

- id: vbnet-errors-04
  answer: |
    Legacy: `On Error GoTo Label`, `On Error Resume Next`, `On Error GoTo 0`, with global `Err` object (`Err.Number`, `Err.Description`, `Err.Clear`, `Resume`, `Resume Next`). Unstructured goto-based, procedure-global error state, hard to nest, easy to hide errors.
    `Try/Catch/Finally` preferred: structured, block-scoped, exception-type filtered, supports `When` and `Finally`, preserves stack traces, composes with `Using`, enforced by Option Strict, interoperable with .NET exceptions, clearer control flow.

- id: vbnet-linq-01
  answer: |
    VB query syntax is `From`-first: 
    ```
    Dim q = From x In customers
            Where x.City = "London"
            Select x.Name
    ' or with projection:
    Dim q2 = From p In products
             Where p.Price > 100
             Select New With {p.Name, .Tax = p.Price*0.2}
    ```
    Keywords: `From`, `In`, `Where`, `Select`, `Order By`/`Ascending`/`Descending`, `Distinct`, `Take`, `Skip`. Fluent equivalent is `customers.Where(Function(x) x.City="London").Select(Function(x) x.Name)`.

- id: vbnet-linq-02
  answer: |
    ```
    Dim q = From p In products
            Group By cat = p.Category Into Group, Count(), Total = Sum(p.Price)
            Where Group.Count > 2
            Order By cat
            Select cat, Group, Total
    ' variant:
    Dim q2 = From p In products
             Group p By p.Category Into G = Group
             Select Category, Items = G
    ```
    `Group By` defines keys, `Into` introduces aggregation aliases and `Group` (the group sequence). Aggregates like `Count()`, `Sum()`, `Min()`, `Max()`, `Average()` apply to group.

- id: vbnet-linq-03
  answer: |
    `Aggregate` collapses a sequence to a scalar, unlike `From` which yields a sequence (deferred enumeration).
    Query: `Dim total = Aggregate n In numbers Into Sum(n), Average(n)` or `Aggregate p In products Into Count()` . It's syntactic sugar for `numbers.Sum()` / method call. `From ... Select` projects each element; `Aggregate ... Into` without `Group By` aggregates over the entire source. Can also use `Aggregate ... Into Group` variant? No - that's `Group By .. Into`. Plain `Aggregate` has no `Where` per-element filtering except via source.

- id: vbnet-linq-04
  answer: |
    Deferred/lazy execution: LINQ query definition does not run against source until enumerated (`For Each`, `ToList` etc.). Each enumeration re-executes query, sees current source data. Enables composition and streaming.
    Force immediate execution (materialize) with `ToList()`, `ToArray()`, `ToDictionary()`, or scalar termination like `Count()`, `First()`, `Sum()`. In VB: `Dim list = (From x In src Where x>5 Select x).ToList()`.

- id: vbnet-events-01
  answer: |
    `WithEvents` declares a variable whose events you want to handle: `Private WithEvents btn As Button`. 
    `Handles` declaratively binds a method to that variable's event: `Private Sub btn_Click(sender As Object, e As EventArgs) Handles btn.Click` . Compiler auto-wires via hidden AddHandler/RemoveHandler when the variable is assigned (updates on reassignment). No manual wiring needed; designer-generated controls work this way. `WithEvents` only on class/struct member, not local.

- id: vbnet-events-02
  answer: |
    `AddHandler obj.Event, AddressOf Method` dynamically attaches `Method` as handler at runtime; `RemoveHandler obj.Event, AddressOf Method` detaches.
    `AddressOf` creates a delegate instance pointing to the method (signature must match delegate).
    Must use them instead of `Handles` when: source is not a `WithEvents` variable (local variables, arrays, objects received as parameters, controls created at runtime in a loop, events from interface), or when you need conditional/runtime add/remove, multiple handlers, anonymous lambdas, or to avoid memory leaks by detaching.

- id: vbnet-events-03
  answer: |
    Declaration: `Public Event SomethingHappened As EventHandler` or parameterized `Public Event DataReady(sender As Object, e As DataEventArgs)` or `Public Event Clicked As EventHandler(Of ClickEventArgs)`.
    Custom delegate style: `Public Event ThresholdReached As EventHandler(Of ThresholdEventArgs)` with class `ThresholdEventArgs : Inherits EventArgs`.
    Raise: `RaiseEvent SomethingHappened(Me, EventArgs.Empty)` or `RaiseEvent DataReady(Me, args)` - only inside declaring class; raised on same thread.
    Handlers receive `sender As Object` and `e As EventArgs` (or custom args) and signatures must match the event's delegate. Called synchronously in invocation order.

- id: vbnet-events-04
  answer: |
    Choose `WithEvents`/`Handles` for static, design-time wiring: simple forms, single known instance per variable, cleaner declarative syntax, designer support.
    Choose `AddHandler`/`RemoveHandler` for dynamic/runtime wiring: locally created controls, collections/loops, need to attach/detach multiple times, handle events from objects you don't own as member fields, add lambda/closure handlers, or need fine-grained lifetime control to prevent leaks. Often both mix in one class.

- id: vbnet-oop-01
  answer: |
    `Inherits` = class inheritance: `Class Dog : Inherits Animal`. A class can inherit from exactly one base class (single inheritance from `Object` if none specified).
    `Implements` = interface implementation: `Class C : Implements IDisposable, IComparable`. A class can implement multiple interfaces (and also inherit one class and implement interfaces). `Implements` requires implementing each interface member (`Sub Dispose() Implements IDisposable.Dispose`).

- id: vbnet-oop-02
  answer: |
    `Overridable` - marks member as virtual, allowing derived classes to override it.
    `Overrides` - in derived class, provides overriding implementation of an Overridable/MustOverride base member (polymorphic).
    `MustOverride` - abstract member with no implementation in this class; must be overridden in non-abstract derived class (C# abstract).
    `MustInherit` - marks class as abstract - cannot be instantiated, meant to be base; often contains MustOverride members.
    `NotOverridable` - seals an override so further derived classes cannot override it (applied to `Overrides` member: `Public Overrides NotOverridable Sub Foo()`).

- id: vbnet-oop-03
  answer: |
    `Shared` (C# static) member belongs to the type itself, not to any instance; one storage location shared by all instances, no `Me`/`MyBase`, cannot access instance members directly. Lifetime = AppDomain.
    Instance member belongs to each object (`New`) and can access Shared+instance members.
    Access Shared via type name: `ClassName.SharedMember` or instance name (warned). Prefer `MyClass.Foo` vs `obj.Foo`. Example: `Public Shared Count As Integer` shared counter; `Public Shared Function Create()...` factory. Shared constructor `Shared Sub New()` runs once per type.

- id: vbnet-oop-04
  answer: |
    `Me` = current instance, polymorphic - virtual dispatch respects most-derived override.
    `MyBase` = base class view of this instance - calls base implementation non-virtually: `MyBase.ToString()` calls `Object.ToString` bypassing own override.
    `MyClass` = like `Me` but non-virtual dispatch pinned to the class where the code is written: `MyClass.Foo()` always invokes `Foo` defined in this class, not a derived override. Difference from `Me` appears when a derived class overrides `Foo` and base code calls `MyClass.Foo()` vs `Me.Foo()`: `Me` would dispatch to derived override, `MyClass` would not.

- id: vbnet-convarr-01
  answer: |
    `CType(expr, T)` - general conversion operator using widening/narrowing or user-defined `CType` operators; runtime throws InvalidCast if impossible.
    `DirectCast(expr, T)` - direct cast requiring inheritance/interface identity, no conversion or parsing; succeeds only if runtime type is T or derives/implements T; faster, stricter; throws InvalidCast if not; only for reference/interface types (or identical value types).
    `TryCast(expr, T)` - like DirectCast but returns `Nothing` on failure instead of throwing; only for reference types (`Class` constrained). Pattern: `Dim s = TryCast(obj, String) : If s IsNot Nothing Then ...`.
    Use DirectCast when you know type, TryCast for safe test, CType for numeric/parsing conversions.

- id: vbnet-convarr-02
  answer: |
    Widening conversion = source fits in destination without data or precision loss (e.g., `Integer` -> `Long`, `String` -> `Object`). Always succeeds.
    Narrowing conversion = may lose data/precision or range and can fail: `Long` -> `Integer`, `Double` -> `Integer`, `Object` -> `String`.
    With `Option Strict Off`, narrowing allowed implicitly with runtime check. With `Option Strict On`, narrowing conversions must be explicit via `CType`/`DirectCast`/`CInt` etc.; compiler errors on implicit narrowing, while widening remains implicitly allowed.

- id: vbnet-convarr-03
  answer: |
    Yes, VB.NET arrays are 0-based (`LBound = 0`). `Dim a(5) As Integer` declares array with upper bound 5, i.e., length 6 with indices 0..5 (compatibility with VB6 bound syntax, unlike C# length). Safer VB.NET style: `Dim a As Integer() = New Integer(5) {}` or `Dim a() As Integer = {1,2,3}`. Resize while keeping contents: `ReDim Preserve a(newUpperBound)` - `Preserve` keeps existing values, only can change last dimension. `ReDim` without Preserve zeros/clears.

- id: vbnet-convarr-04
  answer: |
    `CInt(expr)`, `CStr(expr)`, `CDbl(expr)`, `CBool`, `CDate`, `CLng`, `CDec` etc. are intrinsic conversion functions (equivalent to `CType(expr,T)` but optimized and with VB semantics, e.g., `CInt` rounds to nearest even - bankers rounding). Use them for narrowing/widening with Option Strict On clarity.
    Use `&` for concatenation because `&` always concatenates strings (treats Null/Nothing as ""), while `+` is ambiguous: if operands are numeric-lookalike it will attempt addition and may do arithmetic (`"1" + 2 = 3`) or throw; `&` always coerces both sides to String and concatenates (`"1" & 2 = "12"`), safer in VB.
```
