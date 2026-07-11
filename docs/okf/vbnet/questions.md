# VB.NET Benchmark — Human Render

> **Generated from `questions.yaml` (corpus_rev 1). Do not grade from this file
> — `questions.yaml` is the source of truth.** Hand-synced; if they disagree,
> the YAML wins.

Legend: **W** = weight (1–3), **D** = difficulty. Rubric shows scored points
(`•`) and partial-credit levels (`~`).

---

### vbnet-syntax-01 · syntax-basics · W2 · easy
**Q:** How do you declare a variable, how are statements terminated, and how do you continue a statement across lines?
**A:** `Dim name As Type [= value]`. Statements end at end-of-line — no semicolons, one per line. Split with a trailing ` _`, though most breaks (after commas/operators/`(`) are implicit continuations since VB 10.
• Dim name As Type [= value] • line-terminated, no semicolons • trailing ` _` / implicit continuation ~ shows Dim..As but misses termination/continuation

### vbnet-syntax-02 · syntax-basics, conversions-arrays · W3 · medium
**Q:** What do `Option Strict`/`Explicit`/`Infer` control, and why does `Strict On` matter?
**A:** Explicit On = must declare vars; Infer On = infer type from initializer. Strict On disallows implicit narrowing conversions and late binding, turning type-unsafe conversions/late-bind into compile errors — the key type-safety setting.
• Explicit=declare, Infer=infer type • Strict On disallows implicit narrowing AND late binding • Strict On → compile errors (type safety) ~ 'stricter' without narrowing/late-binding

### vbnet-syntax-03 · syntax-basics, oop · W2 · medium
**Q:** Difference between a `Module` and a `Class`?
**A:** Module members are implicitly Shared and it can't be instantiated (a namespace-wide helper holder); a Class is instantiated with `New` and holds instance state.
• Module members implicitly Shared / not instantiable • Class instantiated with New, holds instance state ~ 'module=static, class=normal' without not-instantiable

### vbnet-syntax-04 · syntax-basics · W2 · easy
**Q:** `Sub` vs `Function`, and how does a Function return a value?
**A:** Sub returns nothing; Function has a return type. Return via `Return value` OR by assigning to the function's own name and falling through to `End Function`.
• Sub returns nothing; Function returns a value • Return value OR assign to function name ~ distinction but misses name-assignment form

### vbnet-props-01 · properties · W2 · easy
**Q:** Auto-implemented vs full property with a backing field?
**A:** Auto (`Public Property Name As String`) makes the compiler generate a hidden backing field + trivial Get/Set. Full spells out the field and Get/Set block for custom logic.
• auto: compiler generates field + trivial Get/Set • full: explicit Get/Set for logic ~ names auto-property but not the generated field

### vbnet-props-02 · properties · W2 · medium
**Q:** What is an auto-property's backing field named, and how do you default it?
**A:** The field is `_<PropertyName>` (e.g. `_Name`), usable directly inside the class. Default with an initializer: `Public Property Name As String = "unset"`.
• backing field is _<Name> • default via `= value` on the declaration ~ knows there's a hidden field but not _Name

### vbnet-props-03 · properties · W2 · medium
**Q:** How do you declare ReadOnly/WriteOnly properties, and can an auto-property be ReadOnly?
**A:** `ReadOnly` = Get only, `WriteOnly` = Set only. A ReadOnly auto-property IS allowed (backing field set in ctor/initializer); there's no WriteOnly auto-property.
• ReadOnly=Get only, WriteOnly=Set only • ReadOnly auto-property allowed (field set in ctor/init) ~ keywords named but not the auto-property nuance

### vbnet-props-04 · properties · W1 · hard
**Q:** What is a default property, and what constraint does it have?
**A:** `Default` lets `obj(i)` index into the instance (shorthand for `obj.Item(i)`). It must take ≥1 parameter (parameterized); a type can have only one.
• Default enables obj(i) indexer • must take ≥1 parameter ~ describes indexer use but misses parameter requirement

### vbnet-null-01 · nullability · W3 · medium
**Q:** What does `Nothing` mean, and how does it differ for reference vs value types?
**A:** Nothing = the default value of a type. Reference type → null reference. Value type → its default (Integer→0, Boolean→False), NOT null; `Dim i As Integer = Nothing` leaves i=0.
• Nothing = default value • reference → null • value type → its default (0/False), not null ~ 'Nothing means null' without value-type caveat

### vbnet-null-02 · nullability · W2 · medium
**Q:** How do you declare a nullable value type and read it safely?
**A:** `Nullable(Of T)` or `Integer?`. Check `HasValue` (or `IsNot Nothing`) before reading `.Value`, which throws when empty. Assigning Nothing sets the no-value state.
• Nullable(Of T) / T? • check HasValue/IsNot Nothing before .Value ~ declares Integer? but no HasValue/.Value safety

### vbnet-null-03 · nullability · W3 · hard
**Q:** Contrast `If(a,b)`/`If(c,t,f)` with `IIf`. Why prefer `If`?
**A:** `If(a,b)` = null-coalescing; `If(c,t,f)` = short-circuit ternary (only taken branch runs). `IIf` is a function that evaluates BOTH branches — side effects run, `IIf(x IsNot Nothing, x.Value, 0)` can still throw, and it returns Object.
• If(a,b)=coalesce, If(c,t,f)=short-circuit ternary • IIf evaluates both branches • so IIf runs side effects/throws; If is safe & type-safe ~ 'If is better' without both-branches reason

### vbnet-null-04 · nullability · W2 · easy
**Q:** Why test null with `Is Nothing`/`IsNot Nothing` not `= Nothing`?
**A:** Is/IsNot compare reference identity — the correct null test. `= Nothing` does value comparison (a String treats it as `""`, so `"" = Nothing` is True) or won't compile; it doesn't reliably test for null.
• Is/IsNot test reference identity • = Nothing does value comparison (String→"") / wrong ~ says use Is Nothing without why = is wrong

### vbnet-errors-01 · error-handling · W2 · easy
**Q:** Structure of `Try/Catch/Finally` and when Finally runs?
**A:** Try wraps code; `Catch ex As Type` handles (specific first); optional Finally runs unconditionally — exception or not, even on early exit — for cleanup. Ends with `End Try`.
• Try wraps; Catch ex As Type (specific first) • Finally always runs (cleanup) ~ names Try/Catch but misses always-runs Finally

### vbnet-errors-02 · error-handling · W2 · medium
**Q:** What does `Catch ... When` do, and why prefer it over catch-then-recheck?
**A:** `When condition` is an exception filter: the Catch only fires if the condition is True, else the exception keeps propagating. It evaluates before the stack unwinds and avoids catch-and-rethrow.
• When adds a boolean filter; fires only if True, else propagates • runs before unwinding / avoids rethrow ~ 'conditional catch' without before-unwind benefit

### vbnet-errors-03 · error-handling · W3 · hard
**Q:** In a Catch, difference between `Throw` and `Throw ex` when rethrowing?
**A:** Bare `Throw` preserves the original stack trace. `Throw ex` resets the trace to the rethrow line, discarding the origin. Use bare `Throw`; wrap in a new exception (InnerException) to add context.
• bare Throw preserves stack trace • Throw ex resets the trace ~ 'use Throw' without the stack-trace reason

### vbnet-errors-04 · error-handling · W2 · medium
**Q:** What is legacy `On Error Goto`/`Err`, and why is `Try/Catch` preferred?
**A:** `On Error Goto label` + `Err` is the unstructured VB6-inherited per-procedure jump model. Try/Catch is structured — scoped, nestable, typed — and preferred; you can't mix the two in one method.
• On Error/Err = legacy unstructured (VB6) per-procedure jump • Try/Catch structured, scoped, nestable, typed; can't mix in one method ~ 'On Error is old' without structured contrast

### vbnet-linq-01 · linq-query · W2 · medium
**Q:** Shape of a basic VB LINQ query that filters + projects, and the keywords?
**A:** `From x In xs Where predicate Select projection` (e.g. `From n In numbers Where n > 0 Select n * 2`). Keywords: From, Where, Select, Order By, Distinct, Take/Skip, Group By, Join, Aggregate. A VB query must begin with From or Aggregate, but unlike C# can end with any clause; Select is optional.
• From x In xs Where ... Select ... • names the keywords ~ recognizes query syntax but garbles VB keywords (C# form)

### vbnet-linq-02 · linq-query · W2 · hard
**Q:** How do you group and aggregate in VB query syntax (`Group By ... Into`)?
**A:** `From p In people Group By p.City Into Group, Count(), Avg = Average(p.Age)`. `Into Group` binds the grouped sequence; aggregate functions with optional aliases live in the Into clause. Also `Aggregate ... Into Sum(n)` for a whole-query aggregate.
• Group By <key> Into Group [, aggregates] • aggregates live in the Into clause (aliasable) ~ mentions Group By but not the Into clause

### vbnet-linq-03 · linq-query · W1 · medium
**Q:** What is the `Aggregate` query keyword, and how does it differ from `From`?
**A:** `Aggregate n In nums Into Sum(n)` yields a single scalar aggregate directly, whereas `From` yields a sequence. It's a VB query keyword with no direct C# query-syntax equivalent.
• Aggregate yields a scalar (Into Sum/Count/...) • vs From which yields a sequence ~ 'it aggregates' without scalar-vs-sequence

### vbnet-linq-04 · linq-query · W2 · medium
**Q:** What is deferred execution, and how do you force immediate execution?
**A:** A query runs when enumerated (e.g. `For Each`), re-executing each iteration against current data — not at definition. Force it with `.ToList()`/`.ToArray()`/`.Count()` to materialize a snapshot.
• runs on enumeration, not definition (re-runs each iteration) • force with ToList/ToArray/Count ~ 'lazy' without how to force

### vbnet-events-01 · events · W3 · medium
**Q:** Explain `WithEvents` + `Handles`.
**A:** Declare the source with `Private WithEvents btn As Button`, then a handler with `... Handles btn.Click`. The compiler auto-wires it (no AddHandler) and rewires on reassignment. One handler can list multiple events. Not allowed on locals or Structures.
• WithEvents field + Handles clause auto-wires • no explicit AddHandler; can list multiple events ~ names WithEvents/Handles but not the auto-wiring

### vbnet-events-02 · events · W2 · medium
**Q:** What do `AddHandler`/`RemoveHandler` do, what's `AddressOf`, and when are they required?
**A:** `AddHandler source.Event, AddressOf Handler` attaches at runtime; `RemoveHandler` detaches. `AddressOf` yields the delegate. Required for dynamic wiring, sources not in a WithEvents field, `Shared` events, and Structures.
• AddHandler/RemoveHandler attach/detach at runtime; AddressOf gives the delegate • needed for dynamic/Shared/Structure wiring ~ describes AddHandler but omits AddressOf or when-required

### vbnet-events-03 · events · W2 · medium
**Q:** How do you declare a custom event and raise it?
**A:** `Public Event Completed As EventHandler` (or with an implicit param list). Raise with `RaiseEvent Completed(Me, args)` — calls all handlers, no-op if none. Handlers match the delegate signature.
• declare with Event [As DelegateType] • raise with RaiseEvent Name(args) (no-op if none) ~ declares Event but uses C#-style invoke

### vbnet-events-04 · events · W1 · easy
**Q:** When choose `WithEvents`/`Handles` vs `AddHandler`/`RemoveHandler`?
**A:** WithEvents/Handles for static, declarative compile-time wiring. AddHandler/RemoveHandler for dynamic runtime attach/detach, non-WithEvents sources, transient objects, and explicit unsubscribe.
• WithEvents/Handles: static/declarative wiring • AddHandler/RemoveHandler: dynamic attach/detach + unsubscribe ~ a preference without static-vs-dynamic reasoning

### vbnet-oop-01 · oop · W3 · easy
**Q:** `Inherits` vs `Implements`, and how many of each can a class have?
**A:** `Inherits` = single base class (one only). `Implements` = one or more interfaces, and VB additionally requires each member to state `... Implements IFoo.Method` to bind it.
• Inherits = single base (one); Implements = interfaces (many) • per-member `Implements IFoo.Member` clause ~ distinguishes keywords but misses per-member clause

### vbnet-oop-02 · oop · W3 · hard
**Q:** Explain `Overridable`/`Overrides`/`MustOverride`/`MustInherit`/`NotOverridable`.
**A:** Overridable = virtual (not virtual by default); Overrides = replace it. MustOverride = abstract member (forces override); its class must be MustInherit = abstract class (not instantiable). NotOverridable = sealed override.
• Overridable=virtual, Overrides=replace • MustOverride=abstract member, MustInherit=abstract class • NotOverridable=sealed override ~ 2 of the keywords right, others confused/omitted

### vbnet-oop-03 · oop · W2 · medium
**Q:** What does `Shared` mean, and how is a shared member accessed?
**A:** Shared (VB's `static`) = belongs to the type, one copy for all instances, can't touch instance state/`Me`. Access via the type name. Module members are implicitly Shared.
• Shared = belongs to type not instance (VB's static) • accessed via type name; no instance state/Me ~ 'like static' without no-instance-state detail

### vbnet-oop-04 · oop · W2 · hard
**Q:** Distinguish `Me`, `MyBase`, `MyClass`. When does `MyClass` differ from `Me`?
**A:** Me = current instance; MyBase = base-class member access. MyClass calls THIS class's implementation bypassing virtual dispatch, so it differs from Me only for Overridable members a subclass has overridden.
• Me=current instance, MyBase=base access • MyClass calls this class's impl, bypassing override ~ Me/MyBase right but MyClass wrong/omitted

### vbnet-convarr-01 · conversions-arrays · W3 · hard
**Q:** Compare `CType`, `DirectCast`, `TryCast`.
**A:** CType does any widening/narrowing conversion via runtime helpers/operators (throws on failure). DirectCast needs a real inheritance/exact-type relationship (no converting, just reinterpret/unbox), throws otherwise. TryCast = reference types only, returns Nothing on failure.
• CType = any widening/narrowing via runtime helpers • DirectCast requires inheritance/exact type (throws otherwise) • TryCast = ref types only, Nothing on failure ~ one right but conflates the other two

### vbnet-convarr-02 · conversions-arrays · W2 · medium
**Q:** Widening vs narrowing conversion, and how does `Option Strict On` treat each?
**A:** Widening always succeeds without loss (Integer→Long); narrowing may lose data or fail (Long→Integer). Strict On allows implicit widening but requires narrowing to be explicit (else compile error).
• widening = safe/no loss; narrowing = may lose/fail • Strict On: widening implicit, narrowing must be explicit ~ defines them but not the Option Strict rule

### vbnet-convarr-03 · conversions-arrays · W2 · medium
**Q:** Are VB arrays 0-based, how do you resize keeping contents, and what does `Dim a(n)` mean?
**A:** 0-based. `Dim a(n)` sets the upper bound to n → n+1 elements (n is the last index, not the length). `ReDim` resizes but discards; `ReDim Preserve` keeps existing contents.
• 0-based; Dim a(n) = upper bound n → n+1 elements • ReDim discards; ReDim Preserve keeps contents ~ 0-based but treats Dim a(n) as n elements, or omits Preserve

### vbnet-convarr-04 · conversions-arrays, syntax-basics · W2 · easy
**Q:** What are `CInt`/`CStr`/`CDbl`, and why use `&` not `+` for string concat?
**A:** Compact intrinsic conversion operators (CInt→Integer with banker's rounding, etc.). Use `&` because it always concatenates as strings; `+` is overloaded and ambiguous (numeric vs string) under Option Strict Off.
• CInt/CStr/CDbl = intrinsic conversion operators • & always concatenates; + is ambiguous ~ names converters but not the &-vs-+ reason
