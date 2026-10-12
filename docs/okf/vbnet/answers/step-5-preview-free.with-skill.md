# step-5-preview-free — WITH-SKILL validation answers for vbnet (opencode-go, 2026-10-12); closed-book,
# derived/vbnet.step-5-preview-free.SKILL.md body prepended to _prompts/vbnet.md as active guidance.
- id: vbnet-syntax-01
  answer: |
    Declare with Dim (other forms exist: Const, Static, and access modifiers like Private/Public inside classes):
        Dim total As Integer
        Dim name As String = "abc"

    Statements are terminated by the line break itself (newline). There is no semicolon. Several
    statements can be placed on one line separated by a colon ':'.

    Multi-line continuation: end the line with the line-continuation character, underscore, preceded
    by a space: ' _'. Since VB 2010, "implicit line continuation" lets you break lines without the
    underscore in many positions (after commas, after binary operators/conversions, after an opening
    parenthesis or brace, before closing parentheses). XML literal continuation still needs '_'.
- id: vbnet-syntax-02
  answer: |
    Option Explicit On: every variable must be declared before use (the default). Without it a typo
    silently creates a new Object variable.

    Option Strict On: forbids implicit narrowing (data-loss) conversions and late binding; type
    mismatches become compile errors instead of runtime traps. The default is Off.

    Option Infer On: local type inference. 'Dim x = 5' makes x an Integer instead of Object when On
    (default On), Object when Off.

    Why Option Strict On matters: it makes type errors surface at compile time rather than at
    runtime, prevents silent data loss (e.g. Double -> Integer), forbids late-bound calls, and
    forces you to write explicit conversions (CInt, CType, TryCast), which documents intent.
- id: vbnet-syntax-03
  answer: |
    A Module is a static container. It is never instantiated; all its members are implicitly Shared
    (one global copy for the application's lifetime), module variables behave as global variables,
    and members are reached as ModuleName.Member. A Module cannot Inherit a base class, cannot
    implement interfaces, and has no instance constructors (only Shared Sub New).

    A Class is an instantiable reference type. Objects are created with New, each instance carries
    its own copy of instance fields, and the class supports one base class via Inherits, multiple
    interfaces via Implements, constructors, overloads, and events.

    Rule of thumb: Module = stateless helpers and application-wide constants/state; Class = objects
    you want to instantiate repeatedly with per-object behaviour.
- id: vbnet-syntax-04
  answer: |
    A Sub performs actions and returns nothing; it is called as a statement (DoWork() or Call
    DoWork()) and cannot appear where a value is expected. Early exit with Exit Sub.

    A Function computes and returns a value, so it can be used inside expressions:
    Dim r = Add(1, 2). It returns either with a Return statement (Return 42, the modern style) or
    by assigning a value to the function's own name (older style: Foo = 42, then Exit Function).
    Early exit with Exit Function or Return.

    In short: Sub = procedure with no result; Function = procedure with a result. A caller does not
    put parentheses around a Sub call's empty argument list, while a Function's result value is what
    the expression evaluates to.
- id: vbnet-props-01
  answer: |
    An auto-implemented property is declared in one line and the compiler supplies the backing
    field plus trivial Get/Set:
        Public Property Name As String
    You cannot add custom logic, and the field is not accessible from source.

    A full property declares the backing field yourself and may contain arbitrary logic:
        Private _name As String
        Public Property Name As String
            Get
                Return _name
            End Get
            Set(ByVal value As String)
                _name = value.Trim()
            End Set
        End Property

    Differences: auto-properties are concise but behaviour-free; a full property lets you validate,
    transform, lazily compute, raise events, and give Get and Set different accessibility (e.g.
    Private Set). Caller syntax is identical, so you can later expand an auto-property into a full
    one without breaking callers.
- id: vbnet-props-02
  answer: |
    The compiler generates the backing field and names it '_' followed by the property name — for
    'Property Name As String' the field is '_Name'. It is hidden from your source code; you refer
    to it only via reflection.

    Initialize with an initializer in the auto-property declaration:
        Public Property Age As Integer = 0
        Public Property Id As Guid = Guid.NewGuid()
    The initializer expression must be evaluable at compile time (or New-based) and is only allowed
    on auto-implemented properties. In a full property you initialize the backing field instead
    (field initializer or constructor).
- id: vbnet-props-03
  answer: |
    ReadOnly: declare with ReadOnly and supply only a Get block:
        Public ReadOnly Property Count As Integer
            Get
                Return items.Count
            End Get
        End Property
    WriteOnly: declare with WriteOnly and supply only a Set block:
        Public WriteOnly Property Password As String
            Set(ByVal value As String)
                storedHash = Hash(value)
            End Set
        End Property

    Yes, an auto-implemented property can be ReadOnly — it is one of the few variations auto
    properties support:
        Public ReadOnly Property Created As Date = Now
        (or initialized to a default like Public ReadOnly Property Id As Integer = 0)
    A WriteOnly auto-property is effectively pointless since the value could never be read back,
    so it is always written by hand when genuinely needed.
- id: vbnet-props-04
  answer: |
    A default property is invoked without naming the property: given 'Default' on a parameterized
    property, you index the object directly and it runs:
        Dim v = cell(0, 1)   ' calls the default property of cell

    Declare it with the Default modifier on the property (or the older <DefaultProperty("Item")>
    attribute on the class):
        Default Public ReadOnly Property Item(ByVal i As Integer) As String

    Constraint: it must be parameterized — it takes at least one parameter (a .NET default property
    is an indexer; the old VB6 parameterless default like Text no longer exists). A type can have
    only one default property, and it cannot be Shared.
- id: vbnet-null-01
  answer: |
    Nothing means "the default value of the type". For a reference type that is null: the variable
    holds no object reference, and 'obj Is Nothing' is the canonical test; calling a member on it
    raises NullReferenceException.

    For a value type, Nothing is not a null reference but the type's default value: Integer becomes
    0, Boolean False, Date 1/1/0001 00:00:00, and a Structure has all its fields defaulted. So
    'Dim i As Integer = Nothing' yields a perfectly valid 0, not an error. To make a value type
    actually nullable you must use Nullable(Of T) (Integer?).
- id: vbnet-null-02
  answer: |
    Declare with the generic form or the '?' shorthand:
        Dim x As Nullable(Of Integer)
        Dim y As Integer?

    Check with the HasValue property (x.HasValue is True when a value is present). You may also
    test 'x IsNot Nothing', which maps to HasValue.

    Read safely with GetValueOrDefault: x.GetValueOrDefault() returns the value or default(T) (0
    for Integer); x.GetValueOrDefault(42) returns the fallback. x.Value returns the value but
    throws InvalidOperationException when null — guard it with HasValue first, or combine:
    Dim result = If(x.HasValue, x.Value, 0).
- id: vbnet-null-03
  answer: |
    If(a, b) is the null-coalescing operator: returns a when a is not Nothing (or when the
    Nullable HasValue), otherwise b. If(cond, b, c) is the conditional operator: returns b when
    cond is True, else c. Both short-circuit — only the selected operand is evaluated, so
    If(x = 0, 0, 10 / x) never divides by zero.

    IIf(a, b, c) is the legacy function, not an operator. It evaluates ALL THREE arguments every
    time, then picks one: IIf(x = 0, 0, 10 / x) still throws DivideByZeroException, and it returns
    Object (boxes value results), costing precision and speed.

    Prefer If: correct short-circuit semantics, generic (type-preserving, no boxing), and faster.
- id: vbnet-null-04
  answer: |
    Is / IsNot compare references — it is a pure identity check: "does this variable currently
    point at an object?".

    '=' is a value comparison resolved through equality operators / Equals, and its behaviour is
    misleading for null tests. On value types, 'x = Nothing' compares against the default value,
    so it is True whenever x is 0/default — not what you mean. On reference types the result
    depends on how equality is defined on the type (op_Equality can be overloaded), so '=Nothing'
    is not guaranteed to detect "no object". In generic code Is Nothing is the only comparison
    that works for an unconstrained T, and it stays fully type-safe under Option Strict (no
    late-bound equality call).
- id: vbnet-errors-01
  answer: |
    Structure:
        Try
            risky statements
        Catch ex As IOException
            type-specific handler
        Catch ex As Exception
            general handler
        Finally
            cleanup
        End Try

    Try is required; Catch and Finally are independently optional (Try/Finally with no Catch, or
    Try/Catch with no Finally, are both valid). Several Catch clauses are tried top to bottom, so
    list specific exception types before Exception — the first matching clause handles it. Exit Try
    leaves the block.

    Finally runs when the Try completes, when a Catch has handled an exception, when no handler
    matches and the exception propagates out (during unwind, before the caller sees it), and after
    a Return/Exit inside Try or Catch — i.e. always, exactly once. Use it for guaranteed cleanup:
    closing files/connections, Dispose, resetting state. (If both Catch and Finally exist, Finally
    runs after the handler.)
- id: vbnet-errors-02
  answer: |
    'Catch ex As IOException When ex.HResult = 32' attaches a When filter to the Catch. The clause
    runs only if the exception type matches AND the Boolean filter evaluates True; otherwise the
    exception is not handled and keeps propagating to outer handlers unchanged.

    Why it beats "catch then re-check inside": with the re-check pattern, a filter that fails has
    to rethrow (Throw or Throw ex) to pass the exception along — Throw loses the original stack
    trace, and you pay for a catch that only forwards. The When clause never enters the handler, so
    the original exception, stack trace and context survive untouched for the next handler. It also
    keeps the condition next to the clause it gates, which reads better.
- id: vbnet-errors-03
  answer: |
    'Throw' (bare) rethrows the current exception object preserving its stack trace — the trace
    still points at the line that originally threw, which is the root cause.

    'Throw ex' rethrows the same object but resets the stack trace to the Throw statement. The
    method and line where the error actually happened are lost; diagnostics now show the handler
    as the origin.

    Both keep the same exception type and message, but only bare Throw keeps the origin, so always
    write 'Throw' to rethrow. When you need to add context, wrap and preserve the original as the
    inner exception: Throw New ApplicationException("context", ex).
- id: vbnet-errors-04
  answer: |
    Legacy unstructured handling (VB6 compatibility): 'On Error Goto label' transfers control to a
    labeled statement; 'On Error Resume Next' simply continues at the statement after the failure;
    'On Error Goto 0' disables trapping. Inspection and raising go through the Err object
    (Err.Number, Err.Description, Err.Clear, Err.Raise). There are no real exception objects, just
    a jump plus global error state.

    Try/Catch is preferred because: exceptions are typed objects, so a handler can be selective
    (Catch ex As SqlException) and inspect StackTrace and InnerException; handling is block-scoped
    and nests with structure; Finally guarantees cleanup; 'Resume Next' demands an Err check after
    every single statement and hides failures; structured blocks type-check and perform better.
    On Error survives only for legacy compatibility, not for new development.
- id: vbnet-linq-01
  answer: |
    Basic shape:
        Dim adults = From p In people
                     Where p.Age > 18
                     Order By p.Name
                     Select p.Name, p.Age

    Keywords: From introduces the range variable (p) and the source sequence (people); Where
    filters; Select projects (selecting several fields builds an anonymous type;
    'Select New With {.N = p.Name}' is the explicit form). Related clauses: Order By (Ascending /
    Descending), Let (bind an intermediate value), Distinct, Take/Skip, Join, Group By, Aggregate.

    A query expression must start with From (or Aggregate) and produces an IEnumerable(Of T); it is
    lazily evaluated. In VB, clauses are JScript-case-insensitive and may span lines; range
    variables are typed by inference.
- id: vbnet-linq-02
  answer: |
    Grouping requires an Into clause — Into is mandatory in a Group By clause, there is no form
    that omits it:

        From p In people Group By p.City Into Group

    Aggregates go into that same Into clause, never into a later Select. The group itself may be
    aliased:

        Dim q = From p In people
                Group By p.City Into Group, Total = Count(), AvgAge = Average(p.Age)

    Alias the group and project:
        From p In people
        Group By p.City Into g = Group, Total = Count()
        Order By Total Descending
        Select City, Total, Members = g

    After the clause the key is reachable by its member name (City) and the group by 'Group' or
    its alias; a trailing Select is optional. Multiple keys are written comma-separated in the
    Group By clause.
- id: vbnet-linq-03
  answer: |
    Aggregate is a query starter, like From, and the clause that follows its range is Into (never
    In):

        Dim total = Aggregate n In nums Where n > 0 Into Sum(n)

    It applies the built-in aggregate functions only: Sum, Count, LongCount, Average, Min, Max,
    Any, All (Any/All are two of those functions, not custom accumulators). One aggregate yields a
    scalar; several, e.g. Into Sum(n), Max(n), Count(), yield a single anonymous object — not a
    sequence. It can also appear as a later clause over a nested sequence:

        From c In customers Aggregate o In c.Orders Into Total = Sum(o.Amount)

    Difference from From: From projects streams of elements yielding enumerable sequences;
    Aggregate consumes the range and yields a computed value. It is NOT Enumerable.Aggregate — a
    custom fold is the method call .Aggregate(seed, Function(acc, x) ...).
- id: vbnet-linq-04
  answer: |
    Deferred (lazy) execution: the query variable stores the query definition, not the results. The
    query only runs when its result is enumerated (For Each, For n = 0 To q.Count - 1, or a
    consuming method such as Count()), and it re-reads the source at that moment, so changes to
    the source between enumerations are reflected. This allows queries to be composed, passed
    around, and reused without work being done, and avoids computing values nobody iterates.

    Force immediate execution by consuming it into a concrete container:
        Dim list = q.ToList()
        Dim arr = q.ToArray()
        Dim dict = q.ToDictionary(Function(x) x.Id)
        Dim lookup = q.ToLookup(Function(x) x.Key)
    (Forcing matters when you want a snapshot of the data at a point in time.)
- id: vbnet-events-01
  answer: |
    WithEvents + Handles wires events declaratively at compile time. Declare the field with the
    modifier, then mark a parameter-compatible Sub with the Handles clause:

        Private WithEvents timer As System.Timers.Timer

        Private Sub timer_Elapsed(ByVal sender As Object,
                                  ByVal e As System.Timers.ElapsedEventArgs) _
                                  Handles timer.Elapsed

    The compiler generates the AddHandler for you (when the object is assigned to the field) and
    the corresponding RemoveHandler when the field's object is replaced/collected — no manual
    subscription code.

    Limitations: Handles can only reference events of WithEvents fields in the same class (or the
    class's own / MyBase events), not events of local variables or expression results; one Sub may
    list several events comma-separated (Handles a.Ev, b.Ev); handlers must be Sub (not Function)
    and match the delegate signature.
- id: vbnet-events-02
  answer: |
    'AddHandler obj.Ev, AddressOf HandlerSub' subscribes HandlerSub to obj.Ev at runtime;
    'RemoveHandler obj.Ev, AddressOf HandlerSub' removes the subscription. AddressOf converts the
    method name into a delegate instance of the event's delegate type — HandlerSub's signature
    (sender As Object, e As EventArgs or a custom EventArgs subclass) must match the event's
    delegate.

    You must use them instead of Handles when:
    - the publisher is not a WithEvents field — objects created inside a method, controls built
      dynamically at runtime;
    - the event is Shared (Handles cannot hook a Shared event);
    - you need conditional or short-lived subscriptions (attach in one method, detach in another);
    - you want to unsubscribe so the publisher does not keep the subscriber alive through the
      delegate (the classic leak: event source outlives the handler object).
- id: vbnet-events-03
  answer: |
    Simple form — declare the event, raise it with RaiseEvent from inside the declaring class:

        Public Event ThresholdReached(ByVal sender As Object, ByVal e As EventArgs)

        Sub Check(ByVal v As Integer)
            If v > 10 Then
                RaiseEvent ThresholdReached(Me, EventArgs.Empty)
            End If
        End Sub

    Handlers are Subroutines whose parameters match the delegate signature:
        Private Sub OnThreshold(ByVal sender As Object, ByVal e As EventArgs)
            Dim form = DirectCast(sender, MyForm)
        End Sub
        AddHandler ThresholdReached... / via Handles obj.ThresholdReached

    By convention the first argument (sender) is the object raising the event, normally passed as
    Me, and the second (e) carries the data (EventArgs.Empty, or a custom subclass such as
    MyEventArgs with extra fields). Events are typically typed EventHandler or EventHandler(Of T)
    or a custom delegate. If you need custom storage/dispatch, declare a Custom Event ... End Event
    with AddHandler/RemoveHandler/RaiseEvent accessors.
- id: vbnet-events-04
  answer: |
    WithEvents/Handles: least code, compile-time checked, subscription is generated automatically.
    Best when the publisher is a field of the class and the subscription should last for the
    field's lifetime (typical for form controls and designer-generated code). Downside: you cannot
    detach at will — no unsubscribe lever, and lifetime is tied to the field.

    AddHandler/RemoveHandler: manual runtime wiring, but the only option when the publisher is not
    a WithEvents field (locals, dynamically created objects), when the event is Shared, or when
    subscription lifetime must be controlled (attach on demand, detach to let the publisher be
    collected / plug memory leaks). Downside: you must remember to unsubscribe, and AddressOf
    subs are created per call site (store the delegate if you need fine-grained control).

    Rule of thumb: permanent field-long-lived — Handles; anything local, dynamic, shared, or
    short-lived — AddHandler/RemoveHandler.
- id: vbnet-oop-01
  answer: |
    Inherits is class inheritance: a class derives from exactly ONE base class
    (Class Dog Inherits Animal), inheriting its members and behaviour. If no Inherits is written
    the class implicitly derives from System.Object.

    Implements is interface implementation: a class may implement one or MORE interfaces,
    comma-separated in a single clause —
        Class Dog Implements IComparable(Of Dog), IDisposable
    and each interface member must be realized by a member tagged
    'Public Sub Dispose() Implements IDisposable.Dispose'.

    Counts: Inherits = zero or one (single inheritance only); Implements = any number. So VB
    inheritance is single; multiple reuse of contracts comes from interfaces.
- id: vbnet-oop-02
  answer: |
    Overridable — marks a base member as legal to override (Public Overridable Sub Speak()).
    VB methods are NOT overridable by default; Overridable opts in.

    Overrides — on the derived member, supplies the replacement implementation that must match the
    base signature (Public Overrides Sub Speak()). Use MyBase.Speak() inside to still call the base
    version. (Overloads/Shadows handle same-name-different-signature cases.)

    MustOverride — an abstract member: declaration only, no body
    (Public MustOverride Function Area() As Double). The containing class must then be
    MustInherit, and any concrete derived class MUST implement it.

    MustInherit — an abstract class: cannot be instantiated with New, only used as a base; may mix
    abstract and concrete members.

    NotOverridable — prevents further overriding; normally applied to an Overridden member to seal
    it (Public NotOverridable Overrides Sub Speak()). For sealing an entire class you use
    NotInheritable.
- id: vbnet-oop-03
  answer: |
    A Shared member belongs to the type itself, not to any instance: a single copy exists for the
    whole application, shared by all instances and usable even when no instance exists. An instance
    member belongs to each object — every instance has its own copy of fields and executes methods
    against its own state.

    Access a Shared member through the class name: ClassName.Member (SomeModule.Foo(),
    Integer.MaxValue). VB also tolerates instance.Member syntactically, but it resolves to the
    single shared value, so it is misleading and should be avoided.

    Consequences: a Shared method cannot use Me/MyClass/MyBase or instance members directly (only
    other Shared members); Shared Sub New is a static constructor run once before first use;
    Shared fields are process-wide mutable state, so guard them for thread safety.
- id: vbnet-oop-04
  answer: |
    Me — a reference to the current instance of the class containing the code (this). Calls made
    through Me are virtual: Me.Speak() dispatches to the most derived Overrides.

    MyBase — the current instance viewed as its base class. Used to call the base implementation
    directly (MyBase.Speak()) and for constructor chaining (MyBase.New(...), which must be the
    first statement of Sub New).

    MyClass — like Me, but typed as the class where the code is written: member access through
    MyClass bypasses overriding, invoking THIS class's implementation. So MyClass.Speak() calls
    this class's version even if a derived class overrides Speak(), where Me.Speak() would call
    the override.

    MyClass and Me differ exactly when a derived class overrides a member the class uses
    internally — identical otherwise.
- id: vbnet-convarr-01
  answer: |
    CType(expr, T) — the general conversion. Handles widening and narrowing conversions between
    value types, String conversions, reference conversions between related types, and both
    unboxing and boxing; it honors any user-defined conversion operator on the types. Throws
    InvalidCastException or OverflowException when impossible. Use it as the default cast, and
    it is required to unbox.

    DirectCast(expr, T) — a reference-type cast with NO conversion. It succeeds only if the
    runtime type already is T or derives from/implements T; otherwise InvalidCastException. It is
    slightly cheaper than CType when the types already match, but it cannot be used for value-type
    conversions or unboxing (use CType for those).

    TryCast(expr, T) — same restrictions as DirectCast (reference types only, no unboxing) but
    returns Nothing on failure instead of throwing. Use it when a failed cast is an expected,
    non-exceptional outcome, and follow it with a Nothing test.
- id: vbnet-convarr-02
  answer: |
    Widening conversion: no data loss — every source value is representable in the target
    (Integer -> Long -> Double, derived -> base class or interface, value type -> Object). Safe,
    and allowed implicitly.

    Narrowing conversion: may lose data, precision or range, or fail (Long -> Integer can
    OverflowException; Double -> Integer rounds and can overflow; base -> derived reference throws
    InvalidCastException; String -> number). Must be written explicitly with a conversion function
    (CInt, CDbl, CByte, CType, DirectCast) or an explicit cast.

    Option Strict On: implicit widening conversions are fine, but every implicit narrowing
    conversion and every late-bound call is a compile error — you are forced to express the
    narrowing explicitly, so failures are decided at compile time instead of at runtime.
    (Option Strict Off, the default, silently permits both and defers the failure, or data loss,
    to runtime.)
- id: vbnet-convarr-03
  answer: |
    Yes — VB.NET arrays are 0-based.

    In 'Dim a(n)', n is the upper bound, not the element count, so the array holds n+1 elements
    indexed 0..n. 'Dim a(4)' gives five slots a(0)..a(4). (Equivalent to
    Dim a = New Integer(4) {}.) But use New with a size to set every element's initial values:
    Dim primes = New Integer() {2, 3, 5, 7}.

    Resize while keeping contents with ReDim Preserve:
        ReDim Preserve a(9)   ' new size, existing elements copied
    Plain ReDim reallocates and wipes contents (elements reset to default). With a multidimensional
    array, ReDim Preserve may only change the LAST dimension. ReDim Preserve also requires the
    array be single-dimension or you resize only the last dimension, and is O(n) copying — for
    frequent growth prefer List(Of T).
- id: vbnet-convarr-04
  answer: |
    CInt / CStr / CDbl are the VB conversion functions (Microsoft.VisualBasic): CInt converts to a
    32-bit Integer, CStr to a String, CDbl to a Double. CInt on a Double applies banker's rounding
    (round half to even: CInt(2.5) = 2, CInt(3.5) = 4), and CStr formats using the current culture.
    They are the explicit conversions Option Strict demands for narrowing steps; CType is the
    general form.

    Use '&' for concatenation because '+' has two meanings in VB: numeric addition when operands
    are numeric and concatenation only when they are already strings. The result therefore depends
    on runtime data — "5" + 3 evaluates to 8, while trying to add a non-numeric string can throw
    under Option Strict. '&' is always string concatenation: every operand is converted to String
    and joined, so 'name & " (" & count & ")"' is unambiguous; '&=' appends in place.
