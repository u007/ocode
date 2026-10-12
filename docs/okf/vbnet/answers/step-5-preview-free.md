# step-5-preview-free — closed-book answers for vbnet (opencode-go, 2026-10-12); answerer saw only _prompts/vbnet.md
Here is my closed-book answer sheet — all from my own knowledge, nothing looked up:

- id: vbnet-syntax-01
  answer: |
    Declare with `Dim name As Type = value` (the initializer is optional). Other declaration keywords exist for narrower purposes (`Const` for a compile-time constant, `Static` for a value that persists between calls, `ReDim` for arrays), but `Dim` is the normal one. Statements are terminated implicitly by the end of the line — VB has no statement terminator like C#'s semicolon. You can put several statements on one line only by separating them with a colon: `x = 1 : y = 2`.
    To continue one statement across lines: a line-continuation character, an underscore `_` at the end of the line preceded by a space (`Dim x As Integer = 1 + _`), which is the classic form. Since VB 10 (VS 2010), implicit line continuation is allowed in many positions with no underscore: after binary operators, after a comma in an argument list, after an opening `(` or `{`, after `=` in assignments and initializers, before and after `.` in member access chains, and after keywords like `And`/`Or`/`Then`. Underscores are still required anywhere implicit continuation isn't permitted (e.g. in the middle of an identifier or a string literal).

- id: vbnet-syntax-02
  answer: |
    The three switches (declared per file at the top, or set in project properties) control compile-time checking:
    - `Option Explicit On` (default): every variable must be declared before use. `Off` allows undeclared variables and auto-creates them as Object — a classic typo hazard.
    - `Option Strict On` (default Off): forbids (a) implicit narrowing conversions (e.g. `Integer x = 1.5` becomes a compile error), (b) late binding, (c) implicit typing of variables as Object. Widening conversions are still allowed implicitly.
    - `Option Infer On` (default): lets the compiler infer a local's type from its initializer, so `Dim x = 5` makes x an Integer rather than Object.
    `Option Strict On` matters because type mismatches, data loss, and misspelled member names are caught at compile time instead of throwing at runtime. It also forces early binding, which is faster and lets the IDE check members, and it removes silent precision/overflow loss (a Long-to-Integer downcast that truncates, or a Double-to-Single rounding, becomes an error you must make explicit with `CInt`/`CDbl`/`CType`).

- id: vbnet-syntax-03
  answer: |
    `Module` and `Class` are both typedef containers, but a Module is closer to a "static class" in C#. Every member of a Module is implicitly Shared — you access it as `ModuleName.Member` without creating an instance (My like `String.IsNullOrEmpty`). A Module cannot be instantiated, cannot be used as a type of a variable, has no parameterized/shared constructor, and cannot participate in inheritance (it's implicitly sealed and cannot Inherits or Implements members that need instances).
    A `Class` is an instantiable reference type: you create instances with `New`, members are instance members by default, it can inherit one base class with `Inherits`, implement interfaces with `Implements`, have constructors/finalizers, and hold per-instance state. Rule of thumb: use a Module for global-ish helpers/functions and shared state; use a Class for anything you intend to `New` up or describe a domain object with behaviour.

- id: vbnet-syntax-04
  answer: |
    A `Sub` is a procedure that performs actions but returns no value; it ends with `End Sub` and (since VB 2008) can use `Return` just to exit early. A `Function` returns a value; it can be used in an expression (`Dim n = GetTotal()`), and you must return a value in every code path.
    A Function returns a value in one of two ways: the modern `Return <value>` statement, or the legacy form of assigning to the function's own name (`FunctionName = value`) and falling off the end. `Return` is preferred — clearer and it exits immediately; assigning the name is kept only for old VB6-ported code.

- id: vbnet-props-01
  answer: |
    An auto-implemented property (VB 10+, `Public Property Name As String`) lets the compiler generate the hidden backing field, the Get, and the Set. There is no place to put code, so it's only for "plain" data storage.
    A full property declares the field yourself (`Private _name As String`) and writes `Get`/`Set` (or `Get` only, or `Set` only) blocks with code inside: validation, lazy computation, raising events, logging, returning a derived value, or giving the accessors different accessibility (`Public Property X As String` with `Private Set`). Note that VB cannot give the two accessors of an *auto*-property different accessibility — that asymmetry is one of the main reasons to fall back to a full property, along with wanting any logic at all.

- id: vbnet-props-02
  answer: |
    The compiler-generated backing field of `Public Property Name As String` is literally `_Name` (an underscore plus the property name) — same casing conventions as a hand-written field. It's private to the class, so you can't reference it by name in normal code; use the property name instead.
    To initialize an auto-property with a default value, use the inline initializer: `Public Property Count As Integer = 0`. Before VB 10 that wasn't available and you had to either initialize in a constructor or write the full property with a field initializer: `Private _count As Integer = 0`.

- id: vbnet-props-03
  answer: |
    Read-only: `Public ReadOnly Property Name As String` — the property has only a Get (for a full property: just a `Get...End Get` block, no `Set`), so consumers can read but not assign it. Write-only: `Public WriteOnly Property Name As String` with only a `Set` block — the mirror image.
    Can an auto-implemented property be ReadOnly? Yes — `Public ReadOnly ID As Integer` is a legal auto-property; the compiler generates the field and only a Getter, and the value can only be assigned from within the declaring type's constructors (and from Shared/instance initializers), not from outside. WriteOnly auto-properties are also syntactically allowed (field + Setter only) though they are almost never useful, since you can't read the value back.

- id: vbnet-props-04
  answer: |
    A default property is a parameterized property — VB's equivalent of a C# indexer — declared with the `Default` modifier: `Default Property Item(ByVal index As Integer) As String` (older style: `Default ReadOnly Property Item...`). It lets consumers index an object with `obj(2)` instead of `obj.Item(2)`, and the compiler maps `obj(i)` to it.
    Constraints: a default property must take at least one parameter (it makes no sense parameterless), a class may have at most one default property (parameterized overloads of that single default property are allowed, and the accessibility rule is that it must be Public), and it cannot be Shared. That "only one default property per class" limit plus the existing `Me.Item` convention is why indexers in VB are conventionally still named `Item`.

- id: vbnet-null-01
  answer: |
    `Nothing` in VB means "no value of this type" — the default value that a type's default constructor produces. That gives it two very different meanings:
    - Reference types: `Nothing` is the null reference — there is no object, no identity, and touching members raises NullReferenceException.
    - Value types (Integer, Boolean, Date, structures): `Nothing` is not a null reference; the runtime converts it to the type's default value, so `Dim x As Integer = Nothing` yields 0, `Dim b As Boolean = Nothing` yields False, a Date yields `Date.MinValue`. There is no "null Integer" unless you declare it nullable.
    - Nullable value types (`Integer?`): `Nothing` means the value is absent — `HasValue` is False and `.Value` throws.

- id: vbnet-null-02
  answer: |
    Declare with either of two forms: `Dim x As Integer?` (short form) or `Dim x As Nullable(Of Integer)`.
    To test it, use `x.HasValue` or `x Is Nothing` / `x IsNot Nothing` (the lifted `Is` operator works on Nullable(Of T)). To read it safely, prefer `x.GetValueOrDefault()` (returns the default, 0 here, if null) or the two-argument `If(x, fallback)` expression. `x.Value` also reads the value but throws InvalidOperationException when null, so only use it after you know `HasValue` is True. Assignment is plain (`x = 5`, or leave unassigned to be null).

- id: vbnet-null-03
  answer: |
    `If(a, b)` is VB's null-coalescing operator (VB 9). It evaluates `a`, and if `a` is a reference null (or a Nullable(Of T) null) it returns `b`, otherwise it returns `a`. Crucially it **short-circuits**: `b` is not evaluated unless it's actually needed, so no side effects and no exceptions from an expensive or unsafe fallback expression. With three arguments, `If(condition, x, y)` is a proper ternary conditional that also short-circuits.
    `IIf(a, b, c)` is the legacy function from VB6/Microsoft.VisualBasic, kept for compatibility. It is an ordinary function, so **all arguments are evaluated before the call** — both branches always run (what should be the "else" expression's cost, exceptions, or null-reference crash happens regardless), and its use of Object parameters/boxing means it doesn't behave like a coalescing operator for Nullable value types. `If` is preferred because it is genuinely short-circuiting, is type-preserving/generic, and can be inlined by the compiler into a branch; `IIf` should be treated as deprecated.

- id: vbnet-null-04
  answer: |
    `Is` / `IsNot` are reference-identity (and Nullable-null-state) comparisons that go straight to the runtime with no user code involved: `x Is Nothing` is true exactly when the reference is null (or a nullable has no value). `=` and `<>` are the *value* operators, and value equality is user-overloadable — a class can define a custom `Operator =`, so `x = Nothing` can invoke that overload and mean anything the type author wanted (or fail to compile when Option Strict On requires the operand types to be compatible). In modern VB with `Nothing`, using `Is` also reads as unambiguously "identity/null test" to a reviewer. Note the Nullable wrinkle: with value types `x = Nothing` compares against the type's default value, whereas `Is Nothing` tests the HasValue flag — another reason to standardize on `Is`/`IsNot`.

- id: vbnet-errors-01
  answer: |
    Structure: `Try ... Catch [ex As SpecificException] [When filter] ... [Finally ...] End Try`. The risky code goes in Try; one or more Catch clauses follow, ordered from most specific exception type to the most general (`Catch ex As Exception` last), optionally binding the exception object to a variable (`ex`) so you can inspect `.Message`, `.StackTrace`, `.InnerException`, `.Data`. `Finally` holds cleanup code that runs whether the Try completed normally, a Catch ran, a `Throw`/`Exit Try`/`Return` transferred control out, or even an exception escaped unhandled — it runs on the way out in all cases (once per Try). Use Finally for releasing resources deterministically (Close/Dispose, delete temp files, reset state). VB also has `Exit Try` to leave the block early, and `Try...Catch` nests just like `Using`.

- id: vbnet-errors-02
  answer: |
    `Catch ex As IOException When ex.HResult = SomeCode` (or `Catch When File.InUse` for a filter with no bound exception) attaches a boolean When filter to a Catch clause. The filter is evaluated *before* the Catch body is entered; if it returns False, that Catch clause doesn't handle the exception at all and the runtime keeps looking for a later Catch clause (in this Try, then calling frames) whose type — and filter — match.
    Why prefer it to "catch, then re-check inside": catching first already commits that clause to the exception — you then have to re-`Throw` (with the stack-trace caveat of `Throw ex`) or clutter the code with `Return`. The When filter avoids entering the handler at all for conditions it can't handle, avoids the unwinding-side-effect problem of catching and rethrowing, keeps the filter next to the clause that needs it, and (unlike a plain Catch-all + `If` inside) lets the runtime continue matching later Catch clauses and outer handlers cleanly.

- id: vbnet-errors-03
  answer: |
    `Throw` with no argument rethrows the exception currently being handled, preserving its original stack trace, augmented stack trace, and `HResult`. `Throw ex` (re-throwing the bound exception object explicitly) *resets* the exception's stack trace to the current throw point, so you lose the original call site information — which is exactly the information you need when reading a production stack trace. Rethrowing with the object bound also loses the "rethrowing" marker the runtime uses for exception filters elsewhere in the chain.
    It matters for diagnosability: a bare `Throw` keeps `ex.StackTrace` pointing at where the error really occurred. If you must add context, wrap instead of rethrow: `Throw New ApplicationException("loading profile failed", ex)`, which preserves the original as `InnerException`. Also don't `Throw ex` from the bottom of a catch you want to be transparent.

- id: vbnet-errors-04
  answer: |
    The legacy mechanism is VB6-era "unstructured" error handling: `On Error Goto <label>` (jump to a labelled error handler at the end of the routine), `On Error Resume Next` (ignore the error and continue at the next statement), `On Error Goto 0` (disable handling), plus the `Err` object holding the last error's `Err.Number`, `Err.Description`, `Err.Source`, with `Err.Raise` to raise and `Err.Clear` to reset. It's still available and occasionally seen in VB6-migrated code.
    Structured `Try/Catch/Finally` is preferred in modern VB because: it's type-based and hierarchical (catch IOException separately from ArgumentException) instead of comparing error numbers; it carries a real stack trace and supports InnerException wrapping; it scopes handlers to blocks so errors can't silently jump across a whole routine; `Finally` gives deterministic cleanup; it's the .NET standard so it composes with libraries and other .NET languages; and it avoids `Resume Next`-style "swallow the error and carry on with corrupt state" behaviour. Roughly: On Error jumps involve a single global-ish error state per routine, whereas exceptions are objects that propagate until something handles them.

- id: vbnet-linq-01
  answer: |
    Basic filter-and-project query:
        Dim cheapNames = From p In products
                         Where p.Price < 50
                         Select p.Name
    Keywords: `From` (introduces the data source and a range variable, `From p In products`), `In` (names the source), `Where` (filter predicate, lazily evaluated per element), `Select` (projection — what shape each result element takes, here just the name string). Additional clauses you'll often see: `Order By p.Price Descending`, `Join`, `Distinct`, `Take`/`Skip` via method calls. The query is just an expression that produces an IEnumerable (lazy, see vbnet-linq-04), so without a `.ToList()`/`For Each` nothing runs.

- id: vbnet-linq-02
  answer: |
    Grouping uses `Group By`; use the `Into` clause to name the group variable and attach aggregates (unqualified `Aggregate` functions like `Sum(...)`, `Count()`, `Max(...)` can also be written as an `Aggregate` clause at the end of the query):
        Dim byCategory = From p In products
                         Group By p.Category Into g = Group, TotalPrice = Sum(p.Price)
                         Select Category, TotalPrice, Items = g
    `Group By p.Category` buckets the source by the key expression; `Into g = Group` binds `g` to the resulting group (IGrouping carries `.Key` and is enumerable, so `g.Count()`, `g.Sum(...)` work downstream); `TotalPrice = Sum(p.Price)` computes an aggregate over the whole group. You can also skip `Into` for the simple case where you just need the key variable and the default group, or chain `Group By` with `Where`/`Order By`. `Select` then shapes one output element per group.

- id: vbnet-linq-03
  answer: |
    `Aggregate` is VB's clause-level fold/reduce over a sequence — the query-syntax equivalent of `Enumerable.Aggregate`, combined with the standard aggregate functions:
        Dim stats = From o In orders
                    Where o.Year = 2026
                    Aggregate o In orders Into Sum(o.Amount), Max(o.Amount), Count()
    or the standalone form `Dim total = Aggregate n In numbers In Sum(n)`. It folds the whole (filtered) sequence into scalar result(s) at the end of the query. It differs from `From` in that `From` *begins* a query by introducing a data source and range variables for element-by-element processing (then Where/Select/etc. shape a sequence), while `Aggregate` *terminates* one by collapsing the sequence into numbers/value(s). `Aggregate ... Into` also lets you write custom accumulators using the special forms `All(...)` / `Any(...)`. So: From = source/range plumbing, Aggregate = final reduction.

- id: vbnet-linq-04
  answer: |
    Deferred (lazy) execution means the query expression builds an object describing the operations — it doesn't execute when declared, and doesn't execute when you enumerate once only... rather, each enumeration re-runs the whole pipeline against the source as it exists *at that moment*. Chained query operators just wrap previous queries, so a big chain costs nothing until enumerated. Consequences: if the source collection changes (item added/removed) the query sees the change when re-enumerated, predicates with side effects run on every enumeration, and the range variable isn't "captured" values but live references.
    To force immediate execution in VB you materialize it: append `.ToList()`, `.ToArray()`, `.ToDictionary()`, `.ToLookup()` (which execute and store results), or force evaluation via a scalar aggregate such as `.Count()`, `.First()`, `.Sum()`, `.Max()` (they also execute the source, though they consume it rather than cache it). `For Each ... ` executes it too, once per loop.

- id: vbnet-events-01
  answer: |
    `WithEvents` + `Handles` is declarative event wiring done at compile time. Declare the source object as a field with the `WithEvents` modifier, then give a matching Sub the `Handles` clause listing the event(s) it services:
        Private WithEvents _timer As New Timer()
        Private Sub OnTick(sender As Object, e As EventArgs) Handles _timer.Elapsed
    The compiler synthesizes the AddHandler/RemoveHandler calls (on construction and disposal/GC boundaries), checks at compile time that the event and the handler signature exist and are compatible, and lets one method handle several events with a comma list (`Handles _timer.Elapsed, _timer.Tick`) or `Handles MyBase.Events` style patterns. The handler's parameters must match the event's delegate (typically `(sender As Object, e As EventArgs)` or a subclass). `WithEvents` only works on fields (not locals), and the field's lifetime is the lifetime of the subscription.

- id: vbnet-events-02
  answer: |
    `AddHandler obj.SomeEvent, AddressOf Handler` attaches a delegate (created from the method via `AddressOf`) to an event at runtime; `RemoveHandler obj.SomeEvent, AddressOf Handler` detaches the same delegate. `AddressOf` is what turns a method group into a delegate instance matching the event's delegate type; without it you'd be assigning a method reference, not a delegate.
    Use them instead of `Handles` whenever: the event source is a local variable, a `New`ed object inside a loop (e.g. dynamically created buttons/controls), the result of a property or method call, or otherwise not a `WithEvents` field; when you need to attach/detach subscriptions dynamically at runtime based on state (on-demand hookups, avoiding memory leaks by unsubscribing long-lived listeners); when you need the same handler attached to many objects computed at runtime; or when you must control the subscription's lifetime explicitly. `Handles` is compile-time fixed on a field, so it simply cannot express those cases.

- id: vbnet-events-03
  answer: |
    Declare an event with `Public Event ThresholdExceeded(sender As Object, e As ThresholdEventArgs)` (or a simpler `Public Event DataReady()` if you need no payload; the standard .NET pattern is `(sender As Object, e As SomeEventArgs)` where `SomeEventArgs` derives from EventArgs). Raise it with `RaiseEvent ThresholdExceeded(Me, New ThresholdEventArgs(value))` from inside the declaring class — a no-op if nothing is subscribed.
    Handlers receive the arguments through the delegate: `Private Sub OnThreshold(sender As Object, e As ThresholdEventArgs) Handles src.ThresholdExceeded`, where `sender` is the publisher (usually `Me`) and `e` carries the payload in its properties. The compiler generates a private backing delegate field for a normal Event declaration, and the compiler-enforced signature rule means handlers must accept the parameters (they may ignore them, but VB doesn't let you "opt out" of parameters unless you use customization). A `Custom Event` (with explicit `AddHandler`/`RemoveHandler`/`RaiseEvent` accessors) lets you implement your own listener list, thread-safe invocation, or event interception.

- id: vbnet-events-04
  answer: |
    Prefer `WithEvents`/`Handles` when the publisher is a fixed, long-lived field of the consumer (typical form/component/designer code), you want the wiring checked at compile time (signature match, event actually exists), you like the declarative "one method handles this event" readability, and lifetime management being automatic is a plus. It's the default for form events and controls.
    Prefer `AddHandler`/`RemoveHandler` when the publisher is created dynamically, is a local/expression, is one of many instances you wire in a loop, when you need to subscribe only conditionally and unsubscribe when done (event-handler leaks / lifetime control), when routing events to handlers chosen at runtime, or when the two aren't in the designer relationship Handles requires. Handles cannot express temporal subscription; AddHandler cannot give you compile-time signature checking of the event source, so it's a trade-off of expressiveness vs static checking.

- id: vbnet-oop-01
  answer: |
    `Inherits BaseClass` declares single-class inheritance: the class becomes a derived class of exactly one base class (.NET has no multiple class inheritance). The `Inherits` statement must be the first statement in the class body — immediately after the `Class` line, no attributes/blank lines in between — and there can be only one. It's also optional (unlike some languages).
    `Implements IOne, ITwo` declares that the class fulfils one or more interfaces: `Implements` can list multiple comma-separated interfaces on one line (or several `Implements` lines), and you can then satisfy interface members by tagging them: `Public Sub CompareTo(...) Implements IComparable.CompareTo`. Interfaces carry no implementation, only contracts.
    So a class has at most one `Inherits` (one base class) and zero-to-many `Implements`; the two are complementary and both may appear on the same class.

- id: vbnet-oop-02
  answer: |
    - `Overridable`: marks a base-class method/property as one that derived classes are allowed to override (the virtual marker). Default members are not overridable.
    - `Overrides`: in a derived class, replaces the base implementation of an `Overridable` (or `MustOverride`) member with new behaviour; without it, a same-named member merely hides/shadow the base one (a warning), not override.
    - `MustOverride`: declares an abstract member — no method body at all — that derived classes must supply with `Overrides`; forces the class to be `MustInherit`.
    - `MustInherit`: marks a class as abstract: it cannot be instantiated with `New`, and exists only as a base class.
    - `NotOverridable`: forbids further overriding of a member that overrides something, or (applied to a class) forbids inheritance — the VB equivalent of C#'s `sealed`. It's the default for non-overridable members; use it deliberately to stop a specific `Overrides` from being overridden further down the chain.
    Together: `MustInherit` class with `MustOverride` members = interface-like template; `Overridable` + `Overrides` = polymorphic dispatch; `NotOverridable` = close the door again.

- id: vbnet-oop-03
  answer: |
    `Shared` on a member means the member belongs to the *type* rather than to any instance — one single storage location/one method shared by all instances, like `static` in C#. There's exactly one copy of a Shared field regardless of how many objects exist (or none), and a Shared method can't reference instance state because there is no `Me`.
    Access it qualified by the type name: `MathHelper.Round(x)` or `ClassName.Member` — not through an instance (accessing a Shared member via an object variable is legal but produces a warning and is misleading). A `Shared` member also can't be `Overridable`, and a `Shared Sub New()` is the shared constructor that runs once before first use. Also note `Static` on a *local* variable is a different mechanism (per-call-site persistence, not a class member).

- id: vbnet-oop-04
  answer: |
    - `Me` is the current instance — a reference to the object whose code is running (`Me.Field`, `Me.Method()`); it's needed to disambiguate a field from a parameter of the same name and to pass the instance itself (`RaiseEvent ... (Me, e)`).
    - `MyBase` gives access to the base class's members, bypassing any override: `MyBase.Dispose()` calls the base implementation directly — the standard way to extend a base method without infinite recursion. It's also the only way to reference base-class-only members that are shadowed.
    - `MyClass` behaves like `Me` for *field* access, but any method call made through `MyClass` is treated as non-virtual (as if the member were `NotOverridable`): it always runs the implementation in the current class, even if a derived class overrode it.
    So `MyClass` differs from `Me` when (and only when) the current class's member is `Overridable` and a derived class has overridden it: with `Me`, the derived override executes; with `MyClass`, YOUR implementation executes. Use `MyClass` when your class must guarantee its own behaviour even to its own subclasses; use `Me` (the normal case) when overridden behaviour should win.

- id: vbnet-convarr-01
  answer: |
    - `CType(expr, T)`: the general-purpose conversion. At compile time the compiler looks for the correct conversion — implicit/explicit language conversions and user-defined `Widening`/`Narrowing` operator overloads (e.g. a struct that defines `Widening Operator CType(x As MyStruct) As Integer`) — and emits a call to it. It works between unrelated types as long as a conversion is defined (String→Integer, Double→String, custom types). Throws InvalidCastException (or OverflowException) when no conversion applies at runtime.
    - `DirectCast(expr, T)`: a strict, no-conversion cast. It requires that the runtime type of expr already *be* T or something derived from T (for reference types, an inheritance/interface relationship — the object isn't converted, just re-typed). It never invokes user-defined conversion operators, so it's the correct and fastest choice when the relationship is true inheritance, and it fails (InvalidCastException) otherwise. Best when the object was boxed/wrapped as Object but you know its real type.
    - `TryCast(expr, T)`: like DirectCast but safe — returns `Nothing` instead of throwing when the runtime type doesn't match. Restricted to reference types (interfaces and classes; it cannot convert between value types). Use it when a mismatch is a legitimate expected outcome and you have a fallback.
    Chooser's rule: know the object's type and want an exception on error → DirectCast; want safe failure → TryCast; need a value conversion (numeric rounding, string parsing, custom operators) → CType.

- id: vbnet-convarr-02
  answer: |
    A widening conversion moves to a type that can represent every value of the source with no loss — e.g. Integer→Long, Single→Double, any type→Object (a derived→base reference conversion is also widening). A narrowing conversion may lose data or fail — Long→Integer (overflow/truncation), Double→Integer (fractional truncation + overflow), Decimal→Single, and base→derived downcasts; parsing String→Integer is narrowing.
    Under `Option Strict On`, widening conversions are allowed silently; narrowing conversions must be written explicitly with a conversion function or keyword (`CInt`, `CDbl`, `CType`, `DirectCast`), otherwise you get compile-time error BC30512 "Option Strict On disallows implicit conversions from X to Y". Under `Option Strict Off`, narrowing attempts happen at runtime with whatever success/loss the runtime binder produces — silently truncating, overflowing, or throwing.

- id: vbnet-convarr-03
  answer: |
    VB.NET arrays are 0-based: `Dim a(9) As Integer` declares ten elements indexed 0 through 9. The number in `Dim a(n)` is the **upper bound**, not the length — the array has n + 1 elements; the last valid index is `a.GetUpperBound(0)`, the length is `a.Length`, and the legacy helper `UBound(a)` returns n. So `Dim a(3)` is a 4-element array, and `Dim a(-1)`. Use `a.Length` in loops rather than hard-coding.
    Resize while keeping the existing contents with `ReDim Preserve a(20)` — `Preserve` copies the survivors (as many as fit) into the newly sized array. Plain `ReDim a(20)` resizes but wipes the contents. Caveat: for multidimensional arrays, `Preserve` can only resize the *last* dimension. You can also express explicit bounds: `Dim a(0 To 9)`.

- id: vbnet-convarr-04
  answer: |
    `CInt`, `CStr`, `CDbl` (and siblings CBool, CLng, CDec, CSng, CDate, CChar, CShort, CByte, CObj, CType) are VB's built-in conversion functions from the Microsoft.VisualBasic namespace. `CInt` returns Integer, `CDbl` Double, `CStr` String, etc. — they perform the runtime narrowing/widening conversions (including rounding: `CInt(1.5)` = 2, banker's rounding; `CInt(-1.5)` = -2) and, for strings, culture-aware parsing per `Option Compare`/current culture settings. They exist because they work even with `Option Strict On` and make narrowing explicit.
    Use `&` rather than `+` for string concatenation because `+` is *overloaded between addition and concatenation* based on operand runtime types: `Dim s = 5 + "10"` can behave as integer addition (15, then string "15") or string concat depending on the operand types, `Option Strict`, and ordering — a classic source of surprising results and compile errors. `&` unambiguously means "convert operands to String and concatenate", is always well-defined, and reads as intentional concatenation.
```

Notes on honesty: I flagged the two spots where my recall is least firm — the exact variant of the VB `Group By ... Into` clause spelling (`Into g = Group`, `TotalPrice = Sum(...)`) and whether `WriteOnly` auto-properties are meaningful — but everything else is stated from knowledge rather than looked up. Unsure overall on: none of the answers are "I have no idea", but `vbnet-props-03` (WriteOnly auto-property usefulness) and `vbnet-props-04` (exact accessibility rule for default properties) carry the most residual doubt.

## Recap

1. ASKED — Answer 32 VB.NET questions closed-book from the attached sheet, no file/web lookups, return YAML `{id, answer}`.
2. WORKED — BUILT full answer sheet: syntax, properties, nulls, errors, LINQ, events, OOP, conversions/arrays — all 32 ids, with uncertainty flagged only on `Group By Into` spelling, WriteOnly auto-props, and the default-property accessibility rule.
3. FOUND — Most questions are standard language semantics I know solidly; recall is weakest on the exact `Group By ... Into g = Group` clause variant and the WriteOnly-auto-property edge case.
4. DECIDED — Answered every item independently from own knowledge; no repo files opened, nothing looked up. BOTTOM LINE: 32/32 answered from memory, no invalidating retrieval.
5. NEXT — None — the sheet is complete and ready to grade.
