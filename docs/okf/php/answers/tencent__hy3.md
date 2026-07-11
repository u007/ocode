# php knowledge benchmark — answers
# Model: novita-ai/tencent/hy3   (provider-stripped id: tencent/hy3)
# CLOSED-BOOK via ocode run -dir <empty> (no corpus access). corpus_rev 1.

```yaml
- id: php-types-01
  answer: |
    `declare(strict_types=1);` enables strict typing for the file it appears in. It MUST be the very first statement after the opening `<?php` tag — there can be no other statement (not even whitespace-producing output or a namespace/require) before it, or it is ignored. The directive is per-file and applies at the call site: it governs how scalar arguments are handled for function/method calls made from within that file, and return type checks on functions defined in that file. In the default coercive ("weak") mode, scalar arguments are type-juggled to the declared scalar type when possible (e.g. "5" becomes int 5). In strict mode, the argument's type must match the declared type exactly, otherwise a `TypeError` is thrown — no implicit conversion is performed. Note it affects only scalar and return types, not class/array/callable types.

- id: php-types-02
  answer: |
    `==` is the loose equality operator: it compares values after applying type juggling (conversion), so `"5" == 5` is true. `===` is the strict (identical) operator: it compares both value and type with no conversion, so `"5" === 5` is false. A surprising case: `0 == "php"` is true because when an integer is compared with a string, the string is converted to an integer — "php" becomes 0. Equally surprising: `"" == 0` is true, and `null == ""` is true. `===` avoids these traps.

- id: php-types-03
  answer: |
    Union types `T1|T2` (PHP 8.0) allow a value of any listed type. Intersection types `T1&T2` (PHP 8.1) require the value to satisfy all listed types simultaneously (only usable with class/interface types, not scalars). Nullable types are written `?T` and are exactly shorthand for `T|null` — i.e. `?int` is identical to `int|null`. The `?` form exists for conciseness; both denote a type that may be the given type or null. (Note: you cannot combine `?T` with a union that already includes null, e.g. `?int|null` is illegal.)

- id: php-types-04
  answer: |
    `never` (8.1): the function never returns a value — it always throws or calls `exit`/`die`. Correct for functions that abort control flow. `void`: the function completes but returns no value; it may only be used with an empty `return;` or no return. Correct for actions without a meaningful return. `mixed`: accepts/returns any type; it is implicitly a union of all types and cannot be combined with other types in a union (e.g. `mixed|int` is illegal). Correct when a value's type is genuinely unknown or unrestricted. A `never` return type must not return at all; `void` may run to completion; `mixed` returns something of any type.

- id: php-types-05
  answer: |
    Typed class constants were added in PHP 8.1, letting you declare a type on a class/interface constant, e.g. `public const int FOO = 1;`. When a subclass overrides a typed constant, the overriding constant's type must be compatible — specifically it may be widened (covariant: you can use a supertype of the parent's type) but not narrowed, and the value must conform to the declared type. Incompatible or narrower types trigger a fatal error. (Interface constants may also carry types that implementers must respect.)

- id: php-enums-01
  answer: |
    A pure enum (e.g. `enum Suit { case Hearts; }`) has no associated scalar value; each case is a singleton object identified only by name. A backed enum (e.g. `enum Suit: string { case Hearts = 'H'; }`) associates each case with an integer or string scalar value. You need a backed enum when you must serialize, persist, or compare an enum to/against a scalar — e.g. storing a status in a database column, sending it in JSON, or looking one up from user/network input via `from()`/`tryFrom()`.

- id: php-enums-02
  answer: |
    `cases()` is a static method on the enum that returns an array of all defined case objects (in declaration order). `from(int|string $value)` returns the case whose backing value equals `$value`; if no such case exists it throws a `ValueError`. `tryFrom(int|string $value)` behaves identically but returns `null` instead of throwing when there is no matching case. (`from`/`tryFrom` exist only on backed enums — pure enums have neither, since they have no backing values to look up.)

- id: php-enums-03
  answer: |
    Yes — enums may declare methods, constants, and implement interfaces, and a backed enum may even define a constructor (invoked from each case's declaration). Key restrictions versus a normal class: enums cannot extend another class (and no class can extend an enum) — there is no class inheritance; enum cases cannot be instantiated with `new`; a pure enum cannot have a backing value, and a backed enum must give every case a unique scalar value of the declared backing type; enums are not cloneable; and you cannot use dynamic properties. They can, however, have static methods, traits (with limitations), and implement interfaces.

- id: php-enums-04
  answer: |
    Enum cases are singletons, so two variables holding `Suit::Hearts` reference the exact same single instance. Because of that, `===` comparison returns true, and the case is reliably usable as a `match` arm and as an array key / Set-like identity check — there is no risk of two "equal but separate" instances failing an identity comparison, which is a common pitfall with plain objects used as enums.

- id: php-oop-01
  answer: |
    Constructor property promotion (PHP 8.0) lets you declare and initialize a class property directly in the constructor signature using a visibility modifier, e.g. `public function __construct(public int $x) {}`. This replaces the older, verbose pattern of first declaring the property at class level, then assigning it inside the constructor body (`$this->x = $x;`). Promotion is shorthand for both the declaration and the assignment, and only parameters with a visibility keyword are promoted.

- id: php-oop-02
  answer: |
    `readonly` properties (8.1) may be initialized exactly once — either inline or within the declaring class (typically the constructor) — and thereafter cannot be modified (nor unset). A `readonly` class (8.2) implicitly makes every declared property readonly and forbids adding dynamic properties; it cannot be extended by a class that adds non-readonly properties. Marking the whole class saves repeating `readonly` on each property and guarantees deep immutability of all instance state. Note a readonly property can hold a mutable object whose internals can still change — the reference itself is immutable, not the object's contents.

- id: php-oop-03
  answer: |
    `$this` refers to the current object instance and is only available in instance (non-static) methods. `self` refers to the class in which the method is written, resolved at compile/definition time, and is used for static members and self:: calls that do not change with inheritance. `static` also refers to a class but is resolved at runtime to the class that was actually invoked (late static binding), so in an inherited context `static::` points to the child. Late static binding solves the problem of calling a static method/accessing a static property from within a parent's method and having it refer to the child class rather than the parent — e.g. a factory `return new static();` in a base class that should instantiate the subclass.

- id: php-oop-04
  answer: |
    Asymmetric visibility and property hooks were both introduced in PHP 8.4. Asymmetric visibility lets a property have different visibility for read vs. write, e.g. `public private(set) string $name;` (readable publicly, writable only from within the class). Property hooks (`get` and `set`) let you define computed/intercepted accessors inline, e.g. `public string $name { get => strtoupper($this->first); set { $this->first = $value; } }`. Together they let you avoid hand-writing boilerplate getter/setter methods and avoid exposing public mutable properties while keeping simple, expressive syntax.

- id: php-oop-05
  answer: |
    The `#[\Override]` attribute (PHP 8.3) is placed on a method to declare that it is intended to override a method inherited from a parent class or interface. The engine verifies at compile time that a parent method actually exists to override; if the parent method was renamed, removed, or never existed, a fatal error is raised. It catches the bug where a supposed override silently becomes a brand-new independent method — for example a typo in the method name, or a parent refactor that breaks the link — which would otherwise go unnoticed and cause subtle behavioral errors.

- id: php-closures-01
  answer: |
    A classic closure `function () use ($x) {}` explicitly imports variables from the enclosing scope via the `use` list; without it, those variables are not visible. An arrow function `fn () => ...` (PHP 7.4) implicitly captures all variables used inside it from the enclosing scope by value, with no `use` clause needed. Both capture by value (a copy) by default; arrow functions are limited to a single expression whose value is implicitly returned, while closures can contain multiple statements and explicit `return`s.

- id: php-closures-02
  answer: |
    The first-class callable syntax `strlen(...)` (PHP 8.1) creates a `Closure` object that represents that function/method/invocable without invoking it. Writing `strlen(...)` is equivalent to `Closure::fromCallable('strlen')`. It produces a callable you can pass around and later invoke (e.g. `array_map(strlen(...), $arr)`), and it preserves the original function's referential identity and type information. It also works for methods and static calls, e.g. `Foo::bar(...)` or `$obj->method(...)`.

- id: php-closures-03
  answer: |
    `Closure::bind($closure, $newThis, $newScope)` and its instance method `$closure->bindTo($newThis, $newScope)` create a new closure with a different bound object and/or class scope. `$newThis` sets what `$this` refers to inside the closure, and `$newScope` sets the class scope used for visibility checks, allowing the closure to access private/protected members of `$newThis`. By default a closure has no bound `$this` (or inherits it from where it was defined); binding lets you "repoint" it so it behaves as though defined inside a particular object/class. Pass null for `$newThis` to unbind the object while keeping (or changing) the scope.

- id: php-closures-04
  answer: |
    `use ($x)` captures `$x` by value — a snapshot/copy taken when the closure is defined, so the closure sees the value `$x` held at definition time regardless of later changes. `use (&$x)` captures by reference, so the closure sees the current value of `$x` and can also modify the caller's variable. Thus, if `$x` changes after a by-value capture, the closure still returns the old value; with a by-reference capture it returns the new value. Likewise, a by-reference closure's writes are visible to the outer scope.

- id: php-error-01
  answer: |
    The throwable hierarchy is rooted at the `Throwable` interface. Two final classes implement it: `Exception` (and its subclasses) and `Error` (and its subclasses such as `TypeError`, `ValueError`, `ParseError`, `ArithmeticError`). `Exception` is meant for program-level, catchable, often expected error conditions raised by user code; `Error` represents more serious internal/PHP-engine conditions (e.g. type errors, undefined functions, parse errors). To catch both kinds, catch the common `Throwable` (or catch `Exception` then `Error`, or vice versa). Note that some fatal situations (`TypeError`, etc.) are `Error`s and would NOT be caught by `catch (Exception $e)` alone.

- id: php-error-02
  answer: |
    `finally` runs unconditionally after the `try` (and any matching `catch`) block completes — whether or not an exception was thrown and whether or not it was caught — and before control leaves the surrounding function/block. It runs even when `try`/`catch` executes `return`, `break`, or `continue`. If both the `try`/`catch` and the `finally` contain a `return`, the `finally`'s `return` value wins and overrides the earlier one: the earlier return is deferred, then the `finally` return executes and determines the actual returned value.

- id: php-error-03
  answer: |
    Define a custom exception by extending `Exception` (or a more specific subclass): `class MyException extends Exception {}`. To chain a lower-level exception, pass it as the third argument of the constructor (the `$previous` parameter): `throw new MyException("Context", 0, $previous)`. Chaining preserves the original low-level cause as part of the exception chain so that handlers/debuggers can walk `$e->getPrevious()` and see the root cause while still presenting a higher-level, domain-meaningful error at each layer — without losing the underlying details.

- id: php-error-04
  answer: |
    `set_error_handler()` intercepts non-fatal errors and warnings (which in PHP are not objects and cannot be caught by `try`/`catch`) and routes them to a callback; `try`/`catch` only handles `Throwable` instances. So a warning from `fopen()` on a missing file is normally just a warning, not catchable directly. To make warnings catchable as exceptions, install an error handler (e.g. `set_error_handler(fn($no,$str)=>throw new \ErrorException($str,$no))`) that converts eligible errors/warnings into an `ErrorException`, which you can then `catch`. (In PHP 8 some former warnings became `Error`/`Exception` subtypes; legacy warnings still need this conversion.)

- id: php-arrays-01
  answer: |
    PHP has a single `array` type used for both purposes, but conventionally a "list" is an array whose keys are sequential integers starting at 0 with no gaps, while an "associative array" uses string (or arbitrary) keys. Internally a PHP array is an ordered map: a hash table that preserves insertion order, mapping keys (int or string) to values. So regardless of name, the structure is the same ordered key→value map; "list" is just the special case of contiguous integer keys, and the engine optimizes that case.

- id: php-arrays-02
  answer: |
    The spread operator `...$arr` unpacks an array's elements into another array literal or into an argument list: `[...$a, ...$b]`. In PHP < 8.1 unpacking an array with string keys raised a warning and dropped the string keys; from PHP 8.1 onward string keys are preserved and unpacked (last value wins on duplicate keys). Keyed destructuring `[$a, $b] = $arr` (or the `list()` syntax, including with keys `['id' => $id, 'name' => $name]`) extracts elements by position (or by key in the associative form) into variables, and you can skip positions with empty slots.

- id: php-arrays-03
  answer: |
    PHP arrays are value types with copy-on-write semantics: assigning (`$b = $a`) or passing an array to a function does NOT immediately copy the data — both share the same underlying buffer until one of them is modified, at which point the modification triggers a copy. This makes large arrays cheap to pass around. By contrast, `&` references (`$b = &$a`) create an alias so that `$a` and `$b` are the same container — mutating one is visible through the other, with no copy-on-write separation. References therefore break the value semantics and can cause surprising shared-state effects.

- id: php-arrays-04
  answer: |
    PHP coerces array keys to a canonical form: integer-equivalent strings become integers, floats are truncated to ints, booleans become 0/1, and null becomes the empty string. Specifically: `"1"` becomes the integer key `1`; `1.9` becomes the integer `1` (float → int truncation); `true` becomes the integer `1`; `null` becomes the string key `""` (empty string). If two keys coerce to the same value they collide and the later value overwrites the earlier.

- id: php-null-01
  answer: |
    `??` (null coalescing) returns the left operand only if it is NOT null (i.e. it checks strictly for null), otherwise the right operand; it does not raise a notice if the left variable is undefined. `?:` (the short/elvis ternary) returns the left operand only if it is truthy; if the left is falsy (false, 0, "", "0", 0.0, [], null) it returns the right operand, and it WILL emit a notice if the left variable is undefined. So `??` tests "is null" while `?:` tests "is falsy".

- id: php-null-02
  answer: |
    The nullsafe operator `?->` (PHP 8.0) short-circuits method/property access: if the expression on its left evaluates to null, the entire chain immediately returns null and the rest of the chain is not evaluated (no error/warning raised). For `$a?->b()->c`, if `$a` is null, evaluation stops and the whole expression yields null; only if `$a` is non-null does it call `b()` and then access `c()` (each subsequent `?->` re-checks null). This lets you safely traverse nullable object chains without nested `if` checks, removing the need for null-guards.

- id: php-null-03
  answer: |
    `??=` is the null-coalescing assignment operator: it assigns the right side to the left variable only if the left variable is null, i.e. `$x ??= $y;` is equivalent to `$x = ($x !== null) ? $x : $y;` — which is subtly different from `$x = $x ?? $y` in that `??=` evaluates the left-hand side only once and never emits an undefined-variable notice. In both forms, if `$x` already holds a non-null value it is left untouched and `$y` is not assigned.

- id: php-null-04
  answer: |
    `isset($x)` returns true if `$x` exists and is not null (it never emits a notice for undefined vars). `empty($x)` returns true if `$x` does not exist OR is falsy (false, 0, 0.0, "", "0", [], null). `$x === null` returns true only when `$x` is the null value and is a real type check. A value where `isset` and `empty` disagree: any non-empty, non-null truthy value, e.g. `$x = "text"` → `isset($x)` is true but `empty($x)` is false. (Conversely `0` shows them agreeing-as-true: isset true, empty true — the interesting disagreement is truthy scalars.)

- id: php-match-01
  answer: |
    `match` differs from `switch` in several ways: (1) comparison is strict (`===`) in `match`, whereas `switch` uses loose (`==`) comparison; (2) `match` has no fallthrough — exactly one arm runs, and there is no `break` needed; (3) `match` is an expression that evaluates to and returns a value, so it can be assigned, while `switch` is a statement; (4) if no arm (and no `default`) matches, `match` throws an `UnhandledMatchError`, whereas `switch` with no match simply does nothing. `match` arms also support comma-separated multiple conditions.

- id: php-match-02
  answer: |
    Because `match` uses strict `===` comparison, it will not match a value to an arm of a different type — a surprise that `switch` (loose `==`) would avoid, often dangerously. For example, `match($status)` where `$status = 0`: an arm like `'active' => ...` or `'0' => ...` will NOT match (strict int vs string), causing an `UnhandledMatchError` unless a `default` is present — whereas `switch($status){ case 'active': }` would silently not match either (correct here) but `switch('foo'){ case 0: }` WOULD wrongly match because 'foo' == 0. The practical surprise is that falsy/zero values and string-numeric mismatches no longer silently collide; you must provide exact-typed arms or a `default`, or you'll get an exception instead of a silent fallthrough.

- id: php-match-03
  answer: |
    `switch` is still preferable when you need fall-through (intentionally executing the next case's body with `continue`/`break` control), when the body of each case is one or more statements rather than a single expression (match bodies must be expressions, though you can wrap in a closure), when you rely on loose comparison semantics, or when you need the `break`/`continue` interaction inside loops where `match` arms can't easily express side-effecting multi-statement logic. Also, cases involving complex statement-level control flow or many shared fall-through paths are more natural in `switch`.

- id: php-match-04
  answer: |
    You can use `match(true)` to replace an if/elseif chain by writing each arm's condition as a boolean expression, e.g. `match (true) { $x > 10 => 'big', $x > 0 => 'positive', default => 'other' }`. Each condition evaluates to a boolean, and `match` strictly compares the subject (`true`) against each arm's boolean result in order, selecting the first arm whose condition is `true`. It works because the comparison is `true === <condition result>`, and since arms are evaluated top-to-bottom with no fallthrough, it replicates ordered if/elseif semantics while returning a value like a switch-free expression.
```
