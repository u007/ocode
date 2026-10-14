# cat-jam-free — WITH-SKILL closed-book answers for vbnet (opencode-go, 2026-10-14)
# Delivery: derived/vbnet.cat-jam-free.SKILL.md body (front matter stripped) prepended to _prompts/vbnet.md; one
# `ocode run -m opencode-go/cat-jam-free -effort med` session, isolated HOME with every tool denied, cwd holding only the sheet;
# session audited in sqlite: 1 user + 1 assistant message, 0 tool calls.

- id: vbnet-syntax-01
  answer: |
    Declare with `Dim name As Type` (optionally `= initialValue`), or `Dim name = value` when `Option Infer On` allows type inference. Statements end at the end of a line; use `:` to put several statements on one physical line. To continue one statement across lines, end the line with a space and an underscore `_` line-continuation character; modern VB also supports many implicit line continuations without `_`.

- id: vbnet-syntax-02
  answer: |
    `Option Explicit` requires variables to be declared before use. `Option Infer` lets the compiler infer a local variable's type from its initializer. `Option Strict` controls implicit conversions and late binding; `On` restricts implicit conversions to widening conversions, prohibits late binding, and avoids implicit `Object` typing. It matters because it catches type errors at compile time instead of runtime and prevents accidental narrowing or late-bound calls.

- id: vbnet-syntax-03
  answer: |
    A `Module` is a reference type whose members are effectively `Shared`; it cannot be instantiated and is used for shared utilities/helpers. A `Class` defines an instance type: objects are created with `New`, and classes support constructors, inheritance, and interfaces. Modules cannot inherit classes, while classes can.

- id: vbnet-syntax-04
  answer: |
    A `Sub` performs work and returns no value; it is called as a statement. A `Function` returns a value and can be used in expressions. A `Function` declares a return type (`Function Name(...) As T`) and returns a value with `Return value` (or, in older style, by assigning to the function name).

- id: vbnet-props-01
  answer: |
    An auto-implemented property is declared like `Public Property Name As String`, letting the compiler generate the private backing field and standard Get/Set accessors. A full property explicitly declares the backing field and Get/Set blocks, allowing validation, lazy initialization, computed values, or different access levels.

- id: vbnet-props-02
  answer: |
    The backing field is compiler-generated and not directly accessible from source; in expanded/generated code it is conventionally named with an underscore prefix such as `_PropertyName`. Set a default with an initializer in the declaration, e.g. `Public Property Count As Integer = 0`, or assign it in a constructor.

- id: vbnet-props-03
  answer: |
    Use `ReadOnly Property Name As T` for read-only and `WriteOnly Property Name As T` for write-only, providing only the appropriate accessor. Auto-implemented properties are normally both Get and Set; modern VB supports read-only auto-properties with an initializer/constructor assignment, but write-only auto-properties are not practical and need a full property.

- id: vbnet-props-04
  answer: |
    A default property lets an instance be indexed/called without naming the property, such as `obj(index)`. A type may have only one default property, and it must be parameterized like an indexer (e.g. `Default Public Property Item(index As Integer) As T`).

- id: vbnet-null-01
  answer: |
    `Nothing` is the default value of a type. For reference types it means a null reference, i.e. no object. For value types it means the zero-initialized value, such as `0`, `False`, or an empty structure; it is not a null reference unless the type is `Nullable`.

- id: vbnet-null-02
  answer: |
    Declare with the nullable shorthand `Dim x As Integer?` or `As Nullable(Of Integer)`. Test with `x.HasValue`; read with `x.Value` after checking, or use `x.GetValueOrDefault()` to avoid exceptions. A nullable with no value compares equal to `Nothing`.

- id: vbnet-null-03
  answer: |
    `If(a, b)` is a short-circuiting operator: the two-argument form performs null coalescing, and the three-argument form performs conditional selection like a ternary. `IIf(a, b, c)` is an ordinary function, so it evaluates both `b` and `c` before choosing. `If` is preferred because it short-circuits, avoids side effects, infers a specific type, handles nullable values, and does not evaluate unused branches.

- id: vbnet-null-04
  answer: |
    `Is Nothing` / `IsNot Nothing` test reference identity, exactly null. Using `= Nothing` invokes equality semantics and can be ambiguous or wrong, especially when equality is overridden or `Nothing` converts to a value-type default. Use `Is` for reliable reference null tests.

- id: vbnet-errors-01
  answer: |
    Structure: `Try ... Catch ... Finally ... End Try`, with `Try` code, zero or more type-specific `Catch` blocks, and an optional `Finally`. `Finally` runs whether the Try succeeds, an exception is caught, or control exits early, making it suitable for cleanup. Order catches from most specific to general.

- id: vbnet-errors-02
  answer: |
    `Catch ... When condition` catches the exception only if the filter evaluates true; otherwise it continues propagating. This avoids entering a catch block only to inspect and rethrow, preserving the original state/stack handling and keeping error handling declarative.

- id: vbnet-errors-03
  answer: |
    `Throw` rethrows the current exception and preserves its stack trace. `Throw ex` throws the captured exception as if new at that point, resetting the stack trace and hiding the original location. Prefer `Throw`; wrap with `InnerException` if you need to add context.

- id: vbnet-errors-04
  answer: |
    Legacy handling uses `On Error Goto label` and the global `Err` object, with manual `Resume`, and can hide or obscure errors. Structured `Try/Catch/Finally` is preferred because it is block-scoped, supports typed catches and filters, automatically unwinds the stack, and provides `Finally` for deterministic cleanup.

- id: vbnet-linq-01
  answer: |
    Shape: `Dim result = From x In source Where condition Select projection`, commonly with `Where`, `Select`, and `From`/`In`. Common additions include `Let`, `Order By`, `Group By`, and `Aggregate`. Result creation is deferred until enumerated.

- id: vbnet-linq-02
  answer: |
    Use `Group By key Into ...`, for example `From x In items Group By x.Category Into Group, Count()` or `... Into Total = Sum(x.Amount)`. This creates groups by key and lets you compute aggregates such as `Count`, `Sum`, `Average`, `Min`, and `Max`.

- id: vbnet-linq-03
  answer: |
    `Aggregate` starts a query that computes an aggregate result rather than a sequence, e.g. `Aggregate x In source Into Sum()` or `Into Count()`. Unlike `From`, which yields elements for projection/filtering, `Aggregate` is intended to reduce input to an aggregated scalar/value.

- id: vbnet-linq-04
  answer: |
    Deferred execution means the query is only executed when enumerated, and it re-evaluates on each enumeration. Force immediate execution by calling a terminating operator such as `ToList()`, `ToArray()`, `ToDictionary()`, `Count()`, `First()`, or another eager conversion/method.

- id: vbnet-events-01
  answer: |
    Declare an instance field with `WithEvents`, then write a method with a `Handles source.Event` clause. The compiler wires the handler with no explicit `AddHandler` call. One `Handles` clause can bind one method to several events comma-separated. Reassigning the `WithEvents` field rewires handlers automatically. The field must be a class-level field; it cannot be a local or in a structure.

- id: vbnet-events-02
  answer: |
    `AddHandler source.Event, handler` attaches a delegate to an event and `RemoveHandler` detaches it. `AddressOf Handler` creates the delegate from a method. They are required when `Handles` cannot be used: shared events, events on structures, sources not held in `WithEvents` fields (locals, collection items, runtime-created objects), attaching lambdas, and explicit unsubscription before the subscriber ends. Pair `AddHandler` with `RemoveHandler` to avoid leaks.

- id: vbnet-events-03
  answer: |
    Declare `Public Event Changed(sender As Object, e As EventArgs)` or a custom delegate event, and raise it with `RaiseEvent Changed(Me, e)`. Handlers have a signature matching the event delegate and are attached with `Handles` or `AddHandler`; they receive the declared arguments, conventionally `sender` and an event-args object.

- id: vbnet-events-04
  answer: |
    Choose `WithEvents`/`Handles` for declarative, lifetime-bound handling of fields that the class owns; it is simpler and handles rewiring. Choose `AddHandler`/`RemoveHandler` for dynamic sources, runtime-created objects, shared/structure events, lambdas, multiple sources, or any case needing explicit detach.

- id: vbnet-oop-01
  answer: |
    `Inherits BaseClass` specifies the single base class; a class can inherit from only one. `Implements I1, I2` declares interface implementations; a class can implement many. Structures can implement interfaces but cannot inherit classes.

- id: vbnet-oop-02
  answer: |
    `Overridable` marks a base member as overridable; `Overrides` redefines it in a derived class; `MustOverride` makes an abstract member that derived classes must override; `MustInherit` makes an abstract class that cannot be instantiated; `NotOverridable` prevents further overriding of an overridden member.

- id: vbnet-oop-03
  answer: |
    `Shared` makes a member belong to the type rather than an instance, so there is one value/method across all instances. Access it through the type name. It cannot directly access instance members without an instance.

- id: vbnet-oop-04
  answer: |
    `Me` is the current instance with normal virtual dispatch. `MyBase` accesses the base-class implementation. `MyClass` also refers to the current instance, but member calls are not virtually dispatched; they bind to the current class's implementation. `MyClass` differs from `Me` when overrides are involved and you want to avoid polymorphic recursion/dispatch.

- id: vbnet-convarr-01
  answer: |
    `CType` performs general conversion and may call conversion operators; it can throw on invalid data. `DirectCast` only changes type within an inheritance/interface relationship or unboxes the exact type; it does not use conversion operators and throws if incompatible. `TryCast` attempts a reference/nullable cast and returns `Nothing` instead of throwing on failure.

- id: vbnet-convarr-02
  answer: |
    A widening conversion cannot lose information and is always safe. A narrowing conversion may lose data or precision and can fail. With `Option Strict On`, widening conversions are allowed implicitly, while narrowing conversions require an explicit cast.

- id: vbnet-convarr-03
  answer: |
    VB.NET arrays are zero-based. `Dim a(n)` creates an array whose upper bound is `n`, so it has `n + 1` elements indexed `0..n`. Resize while preserving contents with `ReDim Preserve a(newUpperBound)`; this can only change the last dimension.

- id: vbnet-convarr-04
  answer: |
    `CInt`, `CStr`, and `CDbl` are built-in conversion functions that convert expressions to `Integer`, `String`, and `Double`, often with parsing/rounding semantics. Use `&` for string concatenation because it always converts operands to strings; `+` may perform numeric addition or cause type errors, especially under `Option Strict`.
