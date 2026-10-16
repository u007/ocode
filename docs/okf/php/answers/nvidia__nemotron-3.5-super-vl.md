# nvidia/nemotron-3.5-super-vl — closed-book answers for php (openrouter, 2026-10-16); answerer saw only _prompts/php.md
- id: php-types-01
  answer: |
    `declare(strict_types=1)` opts the file into strict type checking for scalar parameter and return-type declarations. It must be the first statement in the file (only whitespace/comments may precede it). In strict mode, passing a wrongly typed scalar (e.g. `"5"` to an `int` parameter) or returning the wrong type throws a `TypeError` instead of being silently coerced. Default coercive mode casts scalars where possible. Strictness is determined by the file containing the call, not the file defining the function.
- id: php-types-02
  answer: |
    `==` is loose equality: operands are coerced to a common type before comparing. `===` is strict: equal only if same type and same value. Surprising case: `0 == "abc"` is true because the string is cast to int `0`; likewise `null == false` is true and `"123" == 123` is true.
- id: php-types-03
  answer: |
    Union types (`A|B`, PHP 8.0) accept a value of any one listed type. Intersection types (`A&B`, PHP 8.1) require all listed types (class/interface types only). Nullable `?T` is shorthand for `T|null` (PHP 7.1), meaning the value may be `T` or `null`.
- id: php-types-04
  answer: |
    `never` (PHP 8.1) is return-only: the function never returns (always throws, exits, or loops forever). `void` is return-only: the function returns no usable value (bare `return;` allowed). `mixed` (PHP 8.0) accepts/returns anything (`array|bool|float|int|object|string|null`). Use `never` for terminating functions, `void` for side-effect procedures, `mixed` only when no narrower type fits.
- id: php-types-05
  answer: |
    Typed class constants (`public const string FOO = 'x'`) were added in PHP 8.3. When a subclass overrides a typed constant, the type must match exactly — constant types are invariant, so covariance/contravariance is forbidden and a mismatched type is an error.
- id: php-enums-01
  answer: |
    A pure enum's cases have no scalar values; each case is a singleton object. A backed enum (int or string) assigns each case a scalar value, implements `BackedEnum`, and gains `from()`. Use a backed enum when you must map cases to/from storable scalars, such as database columns or API payloads.
- id: php-enums-02
  answer: |
    `cases()` returns an array of all cases. `from($value)` (backed enums only) returns the matching case and throws `ValueError` if none matches. `tryFrom($value)` returns the matching case or `null`. Use `from` for trusted values and `tryFrom` for untrusted input.
- id: php-enums-03
  answer: |
    Yes — enums can have methods, constants, and implement interfaces. Restrictions vs classes: no instance properties (constants only), no constructor, cannot be extended or instantiated with `new`, implicitly final, cases are singletons, cannot be cloned or serialized.
- id: php-enums-04
  answer: |
    Yes — enum cases are singletons, so both variables refer to the same instance. Identity comparison (`===`) holds, and since `match` compares strictly, `match($x) { Suit::Hearts => ... }` matches based on the case identity.
- id: php-oop-01
  answer: |
    Constructor property promotion (PHP 8.0) declares, types, and initializes a property directly in the constructor signature: `__construct(public string $name) {}` creates `$this->name`. It replaces declaring the property separately and then assigning `$this->name = $name` in the body.
- id: php-oop-02
  answer: |
    `readonly` properties (8.1) may be initialized only once, and only in the scope where they are declared; later reassignment throws an `Error`. `readonly` classes (8.2) make every property implicitly readonly and cannot be extended. Class-level readonly enforces immutability across all properties at once.
- id: php-oop-03
  answer: |
    `$this` is the current object instance. `self` refers to the class where the code is written, resolved at compile time (early binding). `static` refers to the class used at runtime (late static binding). LSB solves inherited methods referencing the actual subclass — e.g. `static::create()` or `static::SOME_CONST` resolves to the child class, which `self::` would not.
- id: php-oop-04
  answer: |
    Asymmetric visibility and property hooks were introduced in PHP 8.4. Asymmetric visibility (`private(set) string $name`) allows public read but restricted write. Property hooks (`get`/`set` blocks) define computed or validated access inline. They let you avoid separate getter/setter methods and `__get`/`__set` magic.
- id: php-oop-05
  answer: |
    `#[\Override]` (PHP 8.3) marks a method as intended to override a parent or interface method. If it does not actually override one, PHP throws a fatal error. It catches typos and signature drift where a method meant to override silently stops doing so (e.g. after a parent method is renamed).
- id: php-closures-01
  answer: |
    A closure's `use ($x)` captures by value; `use (&$x)` captures by reference. An arrow function (`fn () => ...`) always captures by value only (no by-reference option) and automatically binds `$this` from the enclosing scope. Both snapshot variables at creation time unless captured by reference.
- id: php-closures-02
  answer: |
    `strlen(...)` is first-class callable syntax (PHP 8.1): it converts any callable into a `Closure` object (e.g. `Closure($string): int`) that can be stored, passed around, type-hinted against `Closure`, and invoked like `$strlen("abc")`.
- id: php-closures-03
  answer: |
    `Closure::bind` / `bindTo` clone a closure rebound to a different `$this` and/or class scope. `bindTo($newThis, $scope)` changes what `$this` refers to inside the closure and which private/protected members it may access (the scope governs visibility).
- id: php-closures-04
  answer: |
    `use ($x)` captures `$x` by value — the closure sees its definition-time value. `use (&$x)` captures by reference — the closure sees `$x`'s current value at call time. If `$x` changes after the closure is defined, a by-value closure sees the old value; a by-reference closure sees the new one.
- id: php-error-01
  answer: |
    `Throwable` is the top interface; `Exception` and `Error` both implement it. `Exception` covers ordinary application exceptions; `Error` covers internal PHP faults (`TypeError`, `ParseError`, etc.). `catch (Throwable $e)` catches both; `catch (Exception $e)` does not catch `Error`.
- id: php-error-02
  answer: |
    `finally` always runs after the `try`/`catch` block completes — whether it exits normally, returns, or throws (unless the process dies via `exit`/fatal error). If both `try`/`catch` and `finally` contain `return`, the `finally` return wins and replaces the earlier value.
- id: php-error-03
  answer: |
    `class MyException extends Exception {}`. Chain with the `$previous` parameter: `throw new MyException("msg", 0, $originalException)`. Chaining preserves the lower-level exception's message and stack trace so wrapping does not lose the root cause.
- id: php-error-04
  answer: |
    `set_error_handler` intercepts PHP's traditional errors (warnings, notices) through a callback; `try`/`catch` only handles `Throwable` exceptions. To make warnings catchable, install an error handler that converts errors into exceptions — typically `throw new ErrorException($message, 0, $severity, $file, $line)` — then `fopen` failures etc. can be caught with `try`/`catch`.
- id: php-arrays-01
  answer: |
    A "list" has sequential integer keys starting at 0; an associative array uses string keys (or non-sequential integers). A PHP array is actually an ordered hashmap mapping int or string keys to values, preserving insertion order, so one array can mix numeric and string keys.
- id: php-arrays-02
  answer: |
    The spread operator `...$arr` inside `[]` unpacks an array into the new one; since PHP 8.1, string keys are preserved. Destructuring with `list()`/`[]` assigns elements by position; keyed destructuring (`['key' => $var] = $arr`) picks elements by string key.
- id: php-arrays-03
  answer: |
    Arrays are value types: assignment or passing copies the array, using copy-on-write so data is duplicated only when mutated. `&` references make both variables point to the same internal array, so mutations are visible through both.
- id: php-arrays-04
  answer: |
    Keys cast to int when the value is a valid integer; floats truncate toward zero; booleans and null become strings. `"1"` → int `1`; `1.9` → int `1`; `true` → int `1`; `null` → string `""`.
- id: php-null-01
  answer: |
    `??` (null coalescing) returns the left side if it exists and is not null, otherwise the right; it raises no notice for undefined variables (isset semantics). `?:` (elvis) returns the left side if truthy, otherwise the right — so falsy non-null values like `0`, `""`, `[]`, `false` fall through.
- id: php-null-02
  answer: |
    `?->` (nullsafe, PHP 8.0) short-circuits: if the left side is null, the rest of the chain is not evaluated and the result is null. For `$a?->b()->c`, if `$a` is null then `b()` is never called and `c` is never accessed; the whole expression is null.
- id: php-null-03
  answer: |
    `??=` assigns the right side to the left only if the left is not set or is null. Unlike `$x = $x ?? $y`, it writes only once, works on array offsets and properties (`$arr['k'] ??= $v`), and does not reassign an already-set variable.
- id: php-null-04
  answer: |
    `isset($x)` is true if `$x` exists and is not null. `empty($x)` is true if `$x` is falsy (`""`, `0`, `0.0`, `"0"`, `[]`, `false`, `null`, or undefined). `$x === null` is true only for null. They disagree on `null`: `isset(null)` is false while `empty(null)` is true (an undefined variable behaves the same way).
- id: php-match-01
  answer: |
    `match` uses strict comparison (`===`), has no fallthrough, is an expression that evaluates to a value, and is exhaustive — an unmatched subject throws `UnhandledMatchError`. `switch` uses loose comparison (`==`), allows fallthrough, is a statement, and silently does nothing if nothing matches unless `default` is present.
- id: php-match-02
  answer: |
    Strict comparison means `"0"` does not match `0`, `true` does not match `1`, etc. A `switch` would coerce (`0 == "0"` is true) and match, so `match` can miss cases `switch` would catch — e.g. `match(0) { "0" => ... }` matches nothing.
- id: php-match-03
  answer: |
    `switch` is still better when you need fallthrough (multiple cases sharing one body), want loose comparison, or only need side effects (statements) rather than a value, plus when you want a silent `default` catch-all instead of an unmatched error.
- id: php-match-04
  answer: |
    `match(true) { $cond1 => ..., $cond2 => ..., default => ... }` evaluates each arm's expression as a boolean and compares strictly to `true`, so the first true condition wins. It works because any truthy condition yields `true === true`, turning an if/elseif chain into a single expression.
