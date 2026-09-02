```yaml
- id: vbnet-syntax-01
  answer: |
    Declare with `Dim name As Type [= initializer]`, e.g. `Dim count As Integer = 10`.
    Statements are terminated by the end of the line (newline), not a semicolon;
    two statements can be put on one line with a colon (`:`). To continue a statement
    across lines, end the line with a space followed by an underscore (`_`); since
    VB 10 (VS 2010) many contexts (after commas, operators, open parentheses, etc.)
    allow implicit line continuation without the underscore.
- id: vbnet-syntax-02
  answer: |
    - Option Explicit (On): requires every variable to be declared before use; Off
      allows undeclared names to be implicitly created (bad).
    - Option Strict (On): restricts implicit type conversions — implicit narrowing
      conversions, late binding, and implicit Object↔type conversions become
      compile-time errors instead of runtime surprises.
    - Option Infer (On): the compiler infers a local variable's type from its
      initializer when no `As` clause is given (Dim x = 5 is Integer).
    Option Strict On matters because type errors, lossy conversions, and typos from
    late binding are caught at compile time rather than failing (or silently
    truncating) at runtime — it is the recommended setting for new code.
- id: vbnet-syntax-03
  answer: |
    A Module is a container of implicitly Shared members: it cannot be instantiated,
    cannot be inherited, and its members can be called without qualifying them with
    the module name (they're promoted into the enclosing namespace scope). It is
    effectively a sealed class whose members are all static. A Class is a reference
    type that can be instantiated, supports instance members, inheritance,
    interfaces, and all OOP features. Use a Module for small utility/helper
    functions; use a Class for anything with state or identity.
- id: vbnet-syntax-04
  answer: |
    A Sub performs an action and returns no value; a Function returns a value (its
    declared return type). A Function returns a value either by assigning to the
    function's own name (`Function Add(a, b) As Integer ... Add = a + b ...`) or —
    preferred — with a `Return expression` statement, which exits immediately.
    Execution falls off the end of a Function without Return, the default value of
    the return type is returned (unlike C#, which requires a return on all paths).
- id: vbnet-props-01
  answer: |
    An auto-implemented property is declared without accessor bodies:
    `Public Property Name As String` — the compiler silently generates a hidden
    backing field plus trivial Get/Set accessors. A full property declares an
    explicit private backing field and writes Get/Set code yourself, which you need
    when adding logic (validation, change notification, lazy loading), using
    different access levels or logic per accessor, or needing side effects.
- id: vbnet-props-02
  answer: |
    The compiler-generated backing field is named `_<PropertyName>` (e.g. `_Name`
    for property `Name`); it is compiler-generated and not meant to be referenced
    directly in source code. Initialize with an inline initializer (VB 2015+):
    `Public Property Name As String = "default"`.
- id: vbnet-props-03
  answer: |
    Read-only: mark it ReadOnly and supply only a Get accessor
    (`Public ReadOnly Property Id As Integer`). Write-only: mark it WriteOnly and
    supply only a Set. Yes — an auto-implemented property can be ReadOnly; it can be
    initialized with an initializer (`Public ReadOnly Property Id As Integer = 42`)
    or assigned only within the class constructor.
- id: vbnet-props-04
  answer: |
    A default property is marked `Default` and lets you index the object directly —
    `obj(3)` instead of `obj.Item(3)` (e.g. collection indexers). Constraint: a
    default property must take at least one parameter (since VB.NET, parameterless
    defaults are not allowed), and a class can have only one default property.
- id: vbnet-null-01
  answer: |
    `Nothing` in VB is the default value of a type. For a reference type, Nothing is
    a null reference — the variable points to no object. For a value type, Nothing
    means the type's zero/default value: `Dim i As Integer = Nothing` is simply 0,
    not null. So a plain Integer can never be null; you need a nullable wrapper for
    that.
- id: vbnet-null-02
  answer: |
    Declare with the `?` suffix or Nullable(Of T): `Dim n As Integer?` /
    `Dim n As Nullable(Of Integer)`. Check with `n.HasValue` (or `n IsNot Nothing`);
    read the value via `n.Value` only after confirming HasValue, or use
    `n.GetValueOrDefault()` / coalescing `If(n, 0)` to get a fallback safely —
    reading `.Value` on a Nothing nullable throws InvalidOperationException.
- id: vbnet-null-03
  answer: |
    `If` is an operator: binary `If(a, b)` is null-coalescing (returns a unless
    a is Nothing/default, else b) and ternary `If(a, b, c)` is a conditional that
    short-circuits — only the chosen branch is evaluated, and the result keeps the
    proper static type. `IIf(a, b, c)` is a legacy Microsoft.VisualBasic function:
    it evaluates all three arguments eagerly (so b and c can throw or cause side
    effects even when not selected) and returns Object (boxed, weakly typed).
    `If` is preferred for lazy evaluation, type safety, and null-coalescing support.
- id: vbnet-null-04
  answer: |
    `Is Nothing` / `IsNot Nothing` performs reference identity comparison against
    the null reference and works for every reference type. `= Nothing` uses value
    equality: it requires the type to define the `=` operator, can invoke overloaded
    equality/conversion semantics — e.g. for String, `s = Nothing` compares against
    the empty string and is True for `""` — so it tests the wrong thing or fails to
    compile. It also breaks if the variable itself is Nothing under an overloaded
    operator. So use `Is Nothing` to test for null references unambiguously.
- id: vbnet-errors-01
  answer: |
    Structure: `Try` (risky code) → one or more `Catch [ex As Type] [When filter]`
    handlers, tested in order → optional `Finally` (cleanup) → `End Try`.
    `Finally` always runs: after the Try completes, after a Catch handles an
    exception, and even when leaving the block via Return/Exit — but not if the
    process dies. It's the guaranteed place to close files, connections, etc.
- id: vbnet-errors-02
  answer: |
    `Catch ex As Exception When condition` is an exception filter: the Catch body
    runs only if the exception type matches AND the When condition is True;
    otherwise the exception is treated as unhandled by that block and continues
    propagating. This is better than catching-then-rechecking-and-rethrowing
    because the When check happens during first-pass handling without unwinding the
    stack: the original stack trace and local state are preserved, and no
    throw/rethrow round trip is needed.
- id: vbnet-errors-03
  answer: |
    `Throw ex` rethrows but resets the exception's stack trace to the current
    statement, destroying where the error originally occurred. Bare `Throw`
    rethrows the caught exception as-is, preserving the original stack trace and
    inner information. Always use bare `Throw` to rethrow.
- id: vbnet-errors-04
  answer: |
    Legacy unstructured handling: `On Error GoTo label` (jump to a handler),
    `On Error Resume Next` (skip failing statements), `On Error GoTo 0` (disable),
    with the `Err` object (Err.Number, Err.Description, Err.Clear) and Resume
    statements. It is procedural, produces spaghetti control flow, silently hides
    errors, has no scope guarantees or cleanup equivalent, and is hard to maintain.
    Structured Try/Catch/Finally is preferred: typed catches, exception filters,
    guaranteed Finally cleanup, nested/stacked scoping, better diagnostics, and no
    accidental global "resume next" behavior.
- id: vbnet-linq-01
  answer: |
    Basic shape:
    Dim names = From c In customers
                Where c.City = "London"
                Select c.Name
    Keywords: `From` (source and range variable), `Where` (filter),
    `Select` (projection). (`Order By`, `Take`/`Skip`, `Group By`, etc. extend this.)
- id: vbnet-linq-02
  answer: |
    Group By ... Into form:
    Dim q = From o In orders
            Group By CustId = o.CustomerId Into Group,
                     Total = Sum(o.Amount),
                     Count()
    `Group By <key> Into ...` groups the elements by the key expression; `Group`
    (optionally aliased, e.g. `Items = Group`) is the sequence of each group's
    elements, and the aggregate calls (Count(), Sum(...), Average(...), Min/Max...)
    compute values per group. Select can then project the key and aggregates.
- id: vbnet-linq-03
  answer: |
    `Aggregate` applies aggregation over a sequence and yields a single scalar
    value: `Dim total = Aggregate o In orders Into Sum(o.Amount), Count()`.
    It differs from `From` in that `From` introduces a range variable over a
    collection and produces a queryable sequence (many rows), whereas `Aggregate`
    collapses the whole source (or the group it appears inside) into one result.
- id: vbnet-linq-04
  answer: |
    Deferred (lazy) execution: building the query (From/Where/Select...) does not
    run it — it only describes it. The query executes when it is enumerated, e.g.
    in a `For Each` loop; each enumeration re-executes it against the (possibly
    changed) source. Force immediate execution by materializing or aggregating:
    `.ToList()`, `.ToArray()`, `.ToDictionary(...)`, or terminal operators like
    `.Count()`, `.Sum()`, `.First()` (an `Aggregate` clause also forces execution).
- id: vbnet-events-01
  answer: |
    Declare the object with `WithEvents` at class level:
    `Private WithEvents btn As Button`, then write a handler method whose signature
    matches the event and append `Handles btn.Click` (multiple events can be listed:
    `Handles btn.Click, btn.DoubleClick`). The compiler automatically wires the
    method as a handler for events raised by the object in that variable; it fires
    whenever the variable holds an object (and unwires when it is Nothing/replaced).
- id: vbnet-events-02
  answer: |
    `AddHandler obj.EventName, AddressOf HandlerMethod` dynamically attaches a
    handler; `RemoveHandler obj.EventName, AddressOf HandlerMethod` detaches it.
    `AddressOf` produces the delegate pointing at the handler method (it can also
    reference lambdas). You must use them instead of `Handles` when the target is
    not a WithEvents class field — e.g. objects created at runtime, elements of
    arrays/collections, late-bound or shared events — or when you need conditional,
    multiple, or dynamic attach/detach of handlers at runtime.
- id: vbnet-events-03
  answer: |
    Declare the event with its argument signature on the class:
    `Public Event Changed(ByVal sender As Object, ByVal e As MyEventArgs)`
    (or the delegate form `Public Event Changed As EventHandler(Of MyEventArgs)`),
    then raise it inside the class with `RaiseEvent Changed(Me, New MyArgs(...))`.
    Handlers are invoked synchronously with exactly those arguments — `sender` is
    whatever the raiser passed (usually Me) and the event-args object carries the
    data. (A `Custom Event ... AddHandler/RemoveHandler/RaiseEvent End Event` block
    allows custom storage/raising logic.)
- id: vbnet-events-04
  answer: |
    Choose WithEvents/Handles when the source object is a class-level field known at
    design time: it's declarative, minimal, and auto-wired/unwired by the compiler.
    Choose AddHandler/RemoveHandler when handlers must be dynamic: the object lives
    in a collection/array or is created at runtime, you need several or conditional
    handlers, anonymous handlers/lambdas, per-instance wiring of many controls, or
    you must detach handlers (e.g. to allow garbage collection or change behavior).
- id: vbnet-oop-01
  answer: |
    `Inherits` derives a class from a base class (implementation inheritance) —
    a VB class can inherit at most ONE base class. `Implements` declares that the
    class fulfils interface contract(s) — a class can implement MANY interfaces.
    Members implementing an interface must say so explicitly, e.g.
    `Public Sub Foo() Implements IFoo.Bar`. You can combine both:
    `Class C : Inherits B : Implements I1, I2`.
- id: vbnet-oop-02
  answer: |
    - Overridable: marks a member as virtual — derived classes may override it.
    - Overrides: a derived member that replaces an inherited Overridable member.
    - MustOverride: an abstract member with no body that every concrete derived
      class must override; only allowed inside a MustInherit class.
    - MustInherit: marks the class abstract — it cannot be instantiated directly
      and is meant to be inherited.
    - NotOverridable: seals a member — prevents further overriding in classes
      derived from it (it's the implicit default for members not marked
      Overridable; explicitly used on an Overrides member to stop the chain).
- id: vbnet-oop-03
  answer: |
    `Shared` marks a member as belonging to the type itself rather than to any
    instance (static): there is one copy shared by all instances, and shared
    methods cannot access instance state directly. Access it through the class
    name: `ClassName.Member` (VB also allows access via an instance variable, but
    the class-name form is preferred and clearer).
- id: vbnet-oop-04
  answer: |
    `Me` is a reference to the current instance (like C# `this`). `MyBase` invokes
    the base class implementation of members (MyBase.Method(), MyBase.New(...)),
    bypassing the override. `MyClass` calls members using the implementation
    defined in the class where it appears, as if they were NotOverridable — i.e. it
    performs a NON-virtual call. MyClass differs from Me when Me's dynamic dispatch
    would invoke a derived override: MyClass always executes the version in the
    containing class (typically used in constructors and base classes to guarantee
    their own implementation is run).
- id: vbnet-convarr-01
  answer: |
    - CType(expr, T): general-purpose cast/conversion; performs widening, narrowing,
      and user-defined conversions; throws InvalidCastException on failure.
    - DirectCast(expr, T): performs only reference-type identity conversions (and
      boxing/unboxing) — no conversion logic, so the object must actually be of T
      (or derived from T); slightly faster; throws InvalidCastException otherwise.
    - TryCast(expr, T): safe cast for reference types; returns Nothing instead of
      throwing if the cast fails — check for Nothing after use.
    Use TryCast when failure is expected/possible, DirectCast for pure up/down
    reference casts, CType when you need defined conversions (including numeric or
    user-defined ones).
- id: vbnet-convarr-02
  answer: |
    A widening conversion can never lose information or fail (e.g. Integer → Long,
    derived → base); a narrowing conversion may lose data or fail at runtime (e.g.
    Long → Integer, Double → Integer, base → derived, String → Integer). With
    Option Strict On, implicit narrowing conversions are compile-time errors — you
    must cast explicitly (CType/CInt/...); widening conversions remain implicit.
    (Option Strict On also forbids late binding and implicit Object conversions.)
- id: vbnet-convarr-03
  answer: |
    Yes, VB arrays are 0-based: indices run 0..upper bound. In `Dim a(n) As T`, n is
    the UPPER BOUND, so the array has n+1 elements (indices 0 through n).
    `ReDim a(m)` resizes it but discards the contents; `ReDim Preserve a(m)` resizes
    while keeping existing element values (for multidimensional arrays only the
    last dimension may change).
- id: vbnet-convarr-04
  answer: |
    CInt/CStr/CDbl are VB runtime conversion functions that convert an expression to
    Integer, String, and Double respectively (with VB semantics — e.g. CInt rounds
    to even and throws on invalid input). Use `&` for string concatenation because
    `&` unambiguously means "concatenate as strings" (converting operands, treating
    Nothing as empty), whereas `+` also means numeric addition — with Option Strict
    Off, `"1" + 2` evaluates numerically to 3, and `+` can throw or misbehave when
    an operand is Nothing or non-string. `&` is always safe and explicit.
```
