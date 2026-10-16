# nvidia/nemotron-3.5-super-vl — with-skill answers for php (openrouter, 2026-10-16); answerer saw only _prompts/php.md; derived/php.nvidia__nemotron-3.5-super-vl.SKILL.md body (front matter stripped) prepended to the sheet as active guidance
- id: php-types-01
  answer: |
    `declare(strict_types=1)` enables strict type checking for scalar type declarations (int, float, string, bool) in the file where it appears. It must be the very first statement in the file (only the opening `<?php` tag and whitespace may precede it), and its scope is per-file: it only affects calls made inside that file, not the declarations themselves. In default coercive mode PHP silently casts mismatched scalar arguments (e.g. passing `"5"` to an `int` parameter). In strict mode such mismatches throw a `TypeError`; only the exact declared type (or an allowed widening/narrowing via implicit casts from int to float) is accepted.
- id: php-types-02
  answer: |
    `==` is loose equality: operands are coerced to a common type before comparison. `===` is strict identity: operands must be the same type and the same value. A surprising case of type juggling: `0 == "abc"` is `true`, because the non-numeric string is coerced to `0`; likewise `"0" == 0` and `true == 1` are `true`, while `"0" == false` is also `true`.
- id: php-types-03
  answer: |
    Union types (`T1|T2`, PHP 8.0) accept a value of any one of the listed types. Intersection types (`T1&T2`, PHP 8.1) require a value to satisfy all listed types simultaneously (class/interface types only, not scalars). Nullable `?T` is shorthand for `T|null`, allowing `null` in addition to `T`; `?T` and `T|null` are equivalent. `?T` also commonly appears with a default, e.g. `?T $x = null`, making the parameter optional.
- id: php-types-04
  answer: |
    `never` (PHP 8.1) is a return-only type for functions that never return normally: they always `exit`, `die`, `throw`, or loop forever. `void` is return-only for subroutines that return nothing; an early bare `return;` is allowed, and the function implicitly returns `null`. `mixed` accepts any type including `null` and imposes no checking, appropriate for generic containers or truly dynamic data. Use `never` for halting functions, `void` for side-effect routines, and `mixed` only when no specific type applies.
- id: php-types-05
  answer: |
    Typed class constants allow a type declaration on a const, e.g. `public const string NAME = 'x';`, added in PHP 8.3. When a subclass overrides a typed constant, it must declare a compatible type — re-declaring with an incompatible type is a fatal error — so the constant's type is effectively invariant across the hierarchy.
- id: php-enums-01
  answer: |
    A pure enum's cases have no scalar backing value; each case is a singleton object. A backed enum's cases carry an `int` or `string` `value` (all cases must share the same backing type), enabling lookup by value via `from()`/`tryFrom()`. Use a backed enum when you need to map cases to stored or serialized values (database rows, API payloads); use a pure enum when cases are purely symbolic.
- id: php-enums-02
  answer: |
    `cases()` returns an array of all cases of the enum. `from($value)` returns the matching case for a backing value, or throws a `ValueError` if none matches. `tryFrom($value)` returns the matching case, or `null` if no match exists. So `from()` is strict (throws on invalid input) while `tryFrom()` is lenient (returns null).
- id: php-enums-03
  answer: |
    Yes: enums can have methods, constants, and can implement interfaces. Restrictions versus a normal class: cases are singletons and cannot be instantiated with `new`; an enum cannot extend another class or enum (and nothing can extend an enum); enums cannot have instance properties or constructors/destructors; they cannot be cloned or unserialized normally; and all cases are defined statically at compile time.
- id: php-enums-04
  answer: |
    Yes: enum cases are singletons, so both variables refer to the exact same instance. That makes strict identity comparison (`===`) reliable and means `match` against a case works unambiguously — matching is by identity, not by value equality, so there is no risk of two distinct equal cases confusing the match.
- id: php-oop-01
  answer: |
    Constructor property promotion (PHP 8.0) lets you declare and initialize properties directly in the constructor signature, e.g. `public function __construct(public string $name) {}`. It replaces the boilerplate of declaring a property and then writing `$this->name = $name;` in the constructor body.
- id: php-oop-02
  answer: |
    A `readonly` property (PHP 8.1) must be typed, can be initialized only once within the declaring class's scope (typically the constructor), and any subsequent write — even from inside the class — throws `Error`; it cannot have a default value at declaration. A `readonly` class (PHP 8.2) makes every declared property readonly, forbids dynamic properties, and requires typed non-static properties. It is not final: it can be extended and can extend, but only another `readonly` class may extend or be extended by it.
- id: php-oop-03
  answer: |
    `$this` refers to the current object instance. `self` is bound at compile time to the class where the code is written, so `self::method()` or `new self()` always refers to that exact class. `static` resolves at runtime to the class actually called — late static binding. LSB solves the problem that `new self()` in a parent method always builds the parent, whereas `new static()` builds the subclass that was invoked; factories, `create()`/`make()` helpers, and fluent `with*()` methods should use `new static()` / `static::method()` so subclasses get their own type back.
- id: php-oop-04
  answer: |
    Asymmetric visibility (PHP 8.4) lets a property be, for example, `public` for reads but `private(set)` for writes, decoupling read and write access. Property hooks (PHP 8.4) let you define `get` and `set` accessors on a property to compute lazily, validate, or transform values without writing getter/setter methods and backing fields. Together they avoid getter/setter boilerplate.
- id: php-oop-05
  answer: |
    `#[\Override]` (PHP 8.3) marks a method as intended to override a parent or interface method; the engine raises an error if the method does not actually override one. It catches bugs from misspelled method names, renamed parent methods, and signature drift — cases where a developer meant to override but accidentally defined a new, unrelated method.
- id: php-closures-01
  answer: |
    A closure (`function () use ($x) {}`) captures nothing implicitly; every outer variable used inside must be listed in `use()`, and each is snapshotted by value at definition time unless `use (&$x)` is used. An arrow function (`fn () => ...`) implicitly captures by value every enclosing variable it references, with no `use` clause; it cannot capture by reference, and its body is a single expression only.
- id: php-closures-02
  answer: |
    The first-class callable syntax (PHP 8.1) — e.g. `strlen(...)`, `$obj->method(...)`, `Foo::stat(...)` — creates a `Closure` object referencing the callable without invoking it. It replaces string and array callables (`'strlen'`, `[$obj, 'method']`) with a typed, statically analyzable reference that respects visibility, so it can reference private methods from inside the class where string callables could not.
- id: php-closures-03
  answer: |
    `Closure::bind($closure, $newThis, $newScope)` and `bindTo` clone a closure rebound to a new `$this` object and/or new class scope. `$this` inside the closure then refers to the new object, and the new scope determines which private/protected members the closure may access; the original closure is left unchanged.
- id: php-closures-04
  answer: |
    `use ($x)` captures `$x` by value: the closure sees a snapshot of `$x` as it was when the closure was defined, so later changes to `$x` are invisible inside. `use (&$x)` captures by reference: the closure sees the current value of `$x`, and assignments to `$x` inside the closure are visible outside (and vice versa) after the closure is defined.
- id: php-error-01
  answer: |
    `Throwable` is the base interface implemented by both `Exception` and `Error`. `Exception` represents application-level exceptional conditions; `Error` represents internal PHP/engine problems (e.g. `TypeError`, `ParseError`, `FatalError`). Both can be caught with `catch`; catching `Throwable` catches both, while `catch (Exception $e)` catches only `Exception` and misses `Error`.
- id: php-error-02
  answer: |
    `finally` runs unconditionally after `try` and/or `catch` complete — including when `try` or `catch` executes a `return` or throws. If both a `try`/`catch` block and the `finally` block contain a `return`, the `finally` block's return wins and replaces the earlier returned value (note: `exit`/`die` still skip `finally`).
- id: php-error-03
  answer: |
    Define `class MyException extends Exception {}` and instantiate it with the message, code, and a third `$previous` argument holding the lower-level exception, e.g. `throw new MyException("failed", 0, $e);`. The cause is retrievable via `getPrevious()`. Chaining preserves the original stack trace and root cause when translating low-level failures into domain exceptions across abstraction layers.
- id: php-error-04
  answer: |
    `set_error_handler` installs a callback for PHP errors/warnings/notices, which by default cannot be caught with `try/catch`. To make warnings catchable, register an error handler that converts them into exceptions, e.g. `set_error_handler(fn($sev, $msg, $file, $line) => throw new ErrorException($msg, 0, $sev, $file, $line))`, after which `fopen` warnings etc. can be caught normally.
- id: php-arrays-01
  answer: |
    A "list" is an array with sequential integer keys starting at 0 (`['a', 'b']`). An associative array uses string keys or non-sequential integer keys. PHP's single `array` type is actually an ordered hashmap: an insertion-ordered dictionary mapping int or string keys to values, and a single array can mix both key types.
- id: php-arrays-02
  answer: |
    The spread operator (`...$arr`) unpacks an array/traversable inline inside another array literal (PHP 8.1 allows string keys too). String keys are preserved; numeric keys are re-indexed sequentially, and on key collision later values win. Keyed destructuring with `list()` / `[...]` assigns by position (`[$a, $b] = $arr`) or by key (`['a' => $x] = $arr`).
- id: php-arrays-03
  answer: |
    PHP arrays use copy-on-write: assigning or passing an array copies only the handle to the same underlying value; the data is duplicated only when one of the copies is modified. By contrast, `&` references create a true alias: both variables point to the same data and mutating one is immediately visible through the other, with no copy-on-write separation.
- id: php-arrays-04
  answer: |
    Array keys are cast: integer strings become ints (`"1"` → `1`), floats truncate toward zero (`1.9` → `1`), booleans cast to int (`true` → `1`, `false` → `0`), and `null` becomes the empty string (`""`). So `"1"`, `1.9`, and `true` all end up as key `1`, while `null` ends up as key `""`.
- id: php-null-01
  answer: |
    `??` (null coalescing) returns the left operand if it is set and not null, otherwise the right; it does not raise a notice for undefined variables. `?:` (elvis) returns the left operand if it is truthy, otherwise the right — it treats `0`, `""`, `false`, `[]`, and `null` all as falsy and emits a notice if the left operand is undefined.
- id: php-null-02
  answer: |
    The nullsafe operator `?->` (PHP 8.0) short-circuits a chain: if `$a` is null, `$a?->b()->c` evaluates to `null` without calling `b()` or accessing `c`. Short-circuiting means evaluation stops at the first null in the chain and nothing downstream is executed.
- id: php-null-03
  answer: |
    `??=` assigns the right operand to the left only if the left is not set or is null. It is equivalent to `$x = $x ?? $y` for simple variables, but `??=` evaluates the left side only once (important for array offsets or object properties) and is an assignment expression rather than a separate read-plus-write.
- id: php-null-04
  answer: |
    `isset($x)` is true when `$x` exists and is not null. `empty($x)` is true when `$x` is falsy (null, `0`, `0.0`, `""`, `false`, `[]`, or undefined). `$x === null` is true only when `$x` is exactly null. They disagree on `null`: `isset(null)` is false while `empty(null)` is true; they also disagree on an undefined variable (`isset` false, `empty` true with no notice).
- id: php-match-01
  answer: |
    `match` uses strict comparison (`===`), has no fallthrough (only one arm executes), is an expression that yields a value, and throws `UnhandledMatchError` if no arm matches. `switch` uses loose comparison (`==`), falls through unless `break` is used, is a statement, and silently does nothing when no case matches.
- id: php-match-02
  answer: |
    Strict matching can surprise: `match(0) { '0' => ... }` will not match the string arm because `0 === '0'` is false, whereas `switch(0) { case '0': }` would match via coercion. Likewise `match(0)` skips a `false` arm (`0 === false` is false), which a loose `switch` would not.
- id: php-match-03
  answer: |
    `switch` is still better when you need fallthrough (multiple cases sharing one block), when you only care about side effects and not a returned value, or when intentional loose comparison is desired. `match` is preferred for value-producing, exhaustive, strict comparisons.
- id: php-match-04
  answer: |
    `match(true) { $x > 0 => 'positive', $x < 0 => 'negative', default => 'zero' }` replaces an if/elseif chain. Each arm is a boolean expression compared strictly to `true`; the first arm whose expression evaluates to `true` wins, and because `match` is an expression, its result can be returned or assigned directly.
