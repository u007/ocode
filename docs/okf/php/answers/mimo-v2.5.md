```yaml
- id: php-types-01
  answer: |
    `declare(strict_types=1)` enables strict typing for scalar type declarations
    and return types in that file. It must appear as the very first statement
    of a PHP script or the first statement in a file after `<?php` (before any
    other code). With strict typing, PHP will not perform implicit type coercion:
    if you pass an int where a string is expected, a TypeError is thrown. In the
    default coercive mode, PHP silently converts compatible types (e.g., int to
    float, or a numeric string to int).

- id: php-types-02
  answer: |
    `==` performs loose comparison with type juggling, while `===` performs strict
    comparison that also checks type equality. A surprising result of `==`: `0 == "foo"` is
    true in PHP 7 (and false in PHP 8 with numeric string comparison changes),
    but a classic example is `0 == ""` which is true because an empty string
    is cast to `0`. Another famous one: `false == null` is true, `null == ""` is true,
    yet `false == ""` is true, creating a non-transitive-feeling set.

- id: php-types-03
  answer: |
    Union types use `|` to declare multiple accepted types, e.g., `int|string`.
    Intersection types use `&` to require a value implement multiple interfaces,
    e.g., `Countable&Iterator`. Nullable types declare that a parameter can be
    `null` or the specified type; `?T` is shorthand for `T|null` (available for
    function parameters, return types, and class property types as of PHP 8.0+
    for promoted properties, though the `?T` shorthand itself has been around
    since 7.1). The difference is purely syntactic sugar: `?int` and `int|null`
    are equivalent.

- id: php-types-04
  answer: |
    `never` (PHP 8.1): indicates a function never returns — it always throws an
    exception or terminates (e.g., with `exit`). No code follows the call.
    `void`: indicates the function returns nothing meaningful. The function must
    have no return statement (or a bare `return;`). Callers must not use the return
    value.
    `mixed`: the function accepts/returns any type, equivalent to no type
    declaration. Useful when you want to be explicit that anything is allowed.
    `never` is correct for functions that always throw/exit. `void` is correct
    for functions with side effects that return nothing. `mixed` is correct when
    the function genuinely handles any type.

- id: php-types-05
  answer: |
    Typed class constants use the syntax `const TypeName CONST_NAME = value;`
    and were added in PHP 8.3. They allow specifying a type for a class constant.
    When a subclass overrides a typed constant, the overriding constant's type
    must be the same as the parent's declared type — PHP enforces type compatibility,
    similar to property/return type inheritance rules. A narrower or wider type
    is not allowed.

- id: php-enums-01
  answer: |
    A pure enum (no backing type) has cases that are not scalar values; each case
    is its own singleton object. A backed enum carries a scalar value (`string` or
    `int`) associated with each case, set via `: string` or `: int` after the
    enum keyword. You need a backed enum when you need to serialize/deserialize
    enum cases to/from database values, API payloads, or config files — backed
    enums provide `::from()` and `::tryFrom()` for scalar-to-case conversion and
    a `$->value` property for case-to-scalar conversion. Pure enums have neither.

- id: php-enums-02
  answer: |
    `cases()` (actually `Enum::cases()`) returns an array of all cases of the
    enum. `::from($value)` takes a scalar (int or string for backed enums) and
    returns the matching case; if no case matches, it throws a `\ValueError`.
    `::tryFrom($value)` does the same lookup but returns `null` instead of throwing
    when the value does not match any case. The key difference: `from()` throws on
    invalid input, `tryFrom()` returns null.

- id: php-enums-03
  answer: |
    Yes, enums can have methods, constants (regular constants only, not typed
    constants as of PHP 8.3 per enum spec), and implement interfaces. Key
    restrictions versus a normal class: enums cannot be instantiated with `new`,
    they cannot extend other classes (they implicitly extend `UnitEnum` or
    `BackedEnum`), they cannot have normal properties (only constants and methods),
    and a backed enum's `scalar` value is implicitly defined per case and cannot be
    declared as a separate property. Enums are final by default — they cannot be
    extended or subclassed.

- id: php-enums-04
  answer: |
    Yes, they are the same instance. Enum cases are singletons — there is exactly
    one instance of each case per enum. This means `Suit::Hearts === Suit::Hearts`
    is true, and identity comparison works reliably. In a `match` expression, each
    arm uses `===` (strict comparison) by default, so matching against an enum case
    works correctly. The singleton nature also means you can safely use identity
    checks (`===`) rather than string/int comparison.

- id: php-oop-01
  answer: |
    Constructor property promotion (PHP 8.0) lets you declare public, protected,
    or private properties directly in the constructor parameter list, e.g.:
    `public function __construct(private string $name)`. It replaces the
    boilerplate of declaring a class property and then assigning `$this->name = $name`
    in the constructor body. Under the hood, the property is still declared on the
    class and `$this->name` is assigned in the constructor; it is purely a shorthand.

- id: php-oop-02
  answer: |
    A `readonly` property (PHP 8.1) can only be written to once — typically in the
    constructor or at declaration time — and cannot be modified thereafter. Any
    attempt to write to it after initialization throws an error. A `readonly` class
    (PHP 8.2) marks all declared properties as implicitly readonly, so you do not
    need to annotate each one. The class-level modifier also prevents adding
    non-readonly dynamic properties. This is useful for immutable value objects
    where every property is immutable.

- id: php-oop-03
  answer: |
    `self` refers to the class where the method is defined, resolved at compile
    time. `static` refers to the class the method is called on, resolved at
    runtime (late static binding). `$this` is the actual object instance. Late
    static binding solves the problem of `self` always referring to the declaring
    class: when a child class overrides a method or constant and uses `static::`
    instead of `self::`, the child's version is resolved, enabling polymorphic
    behavior for static members. `$this` always points to the concrete instance,
    regardless of where the method was defined.

- id: php-oop-04
  answer: |
    Asymmetric visibility and property hooks were introduced in PHP 8.4. Asymmetric
    visibility allows different access levels for reading and writing a property,
    e.g., `public private(set) string $name` — publicly readable but privately
    writable. Property hooks allow defining `get` and `set` logic directly on a
    property declaration (replacing trivial getter/setter methods). Together they
    let you avoid writing boilerplate getter and setter methods, and avoid exposing
    public setters that should be private while keeping public read access.

- id: php-oop-05
  answer: |
    The `#[\Override]` attribute (PHP 8.3) marks a method that is intended to
    override a parent class method or implement an interface method. If the method
    does not actually override or implement anything (no parent method with that
    name exists), PHP throws an error at compile/definition time. This catches the
    class of bug where a typo in a method name silently creates a new method
    instead of overriding the intended one, or where a parent method was renamed
    and the child was not updated.

- id: php-closures-01
  answer: |
    A closure (`function () use ($x) { ... }`) captures variables at definition
    time by value (copying their current value), unless you use `&$x` to capture
    by reference. An arrow function (`fn() => ...`) automatically captures all
    variables from the parent scope by value — there is no `use` clause. The
    arrow function is a single-expression shorthand and cannot contain statements.
    The key difference is that arrow functions implicitly capture by value (no
    `use` needed) and are limited to one expression, while closures are full
    function bodies with explicit `use` for variable capture.

- id: php-closures-02
  answer: |
    First-class callable syntax `strlen(...)` (PHP 8.1) produces a Closure object
    that wraps the function `strlen`. It is equivalent to `Closure::fromCallable('strlen')`.
    The resulting Closure can be passed anywhere a callable is expected. This also
    works for methods: `$obj->method(...)` produces a Closure bound to that object
    and method.

- id: php-closures-03
  answer: |
    `Closure::bind($closure, $newThis, $newScope)` creates a new closure that is
    a copy of `$closure` but bound to a different object (`$newThis`) and a
    different scope (`$newScope`). `bindTo` is the instance method equivalent
    (`$closure->bindTo($newThis, $newScope)`). They affect `$this` inside the
    closure (making `$newThis` available as `$this`) and determine which private
    and protected members are accessible (based on `$newScope`). Passing `null`
    for `$newScope` uses the closure's original scope. Passing `null` for
    `$newThis` unbinds the closure from any object.

- id: php-closures-04
  answer: |
    `use ($x)` captures the value of `$x` at definition time — the closure always
    sees the original value, even if `$x` changes later. `use (&$x)` captures a
    reference to `$x`, so the closure sees the current value of `$x` at call time.
    If `$x` changes after the closure is defined, a value-captured closure still
    sees the old value, while a reference-captured closure sees the new value.

- id: php-error-01
  answer: |
    PHP 7+ has a unified throwable hierarchy rooted at `Throwable`. `Exception`
    is the base class for user-land and most library exceptions. `Error` is the
    base class for engine-level errors (TypeError, ValueError, ParseError,
    ArithmeticError, etc.). Both `Exception` and `Error` implement the `Throwable`
    interface. To catch both, you can catch `Throwable` directly, or catch
    `Exception` and `Error` separately (or use two catch blocks). In PHP 5
    exceptions and errors were separate hierarchies; PHP 7 unified them so any
    throwable can be caught.

- id: php-error-02
  answer: |
    `finally` always runs after the `try` and `catch` blocks complete, regardless
    of whether an exception was thrown, caught, or the block finished normally.
    If both a `catch` block and the `finally` block contain a `return`, the
    `finally` block's return value wins — it overwrites the return from the
    `catch`. This is because `finally` executes after the `try`/`catch` returns
    but before control actually leaves the method. The `finally` return silently
    replaces the earlier one, which can be surprising.

- id: php-error-03
  answer: |
    Define a custom exception by extending `Exception` (or another exception
    class):
    `class AppException extends Exception {}`
    To chain a lower-level exception into it, pass it as the third argument to
    the parent constructor: `new AppException('msg', 0, $lowerException)`.
    This sets `$previous`, accessible via `getPrevious()`. Chaining preserves the
    causal chain so debugging shows the original root cause while wrapping it in
    a domain-specific exception type.

- id: php-error-04
  answer: |
    `set_error_handler` registers a function to handle PHP errors (warnings,
    notices, etc.) that are not thrown as exceptions. `try`/`catch` handles
    exceptions and `Error` instances. To make PHP warnings catchable as
    exceptions, use `set_error_handler` to throw an exception (typically
    `ErrorException`) inside the error handler, then wrap the risky code in
    `try`/`catch`. Alternatively, in PHP 8+ many warnings have been converted
    to `Error` exceptions, reducing the need for this pattern.

- id: php-arrays-01
  answer: |
    PHP has one `array` type internally. A "list" is an array with sequential
    integer keys starting at 0, with no gaps. An "associative array" has string
    keys (or non-sequential integer keys). Internally, PHP arrays are ordered
    hash maps — they store key-value pairs in insertion order using a hash table
    with linked-list chaining for collisions. Both lists and associative arrays use
    the same underlying `array` type; the distinction is a convention, not a
    language-enforced type.

- id: php-arrays-02
  answer: |
    The spread operator `...` unpacks an array into individual arguments or,
    since PHP 7.4, into array literals `[...$arr]`. In PHP 8.1, string keys are
    preserved when spreading into `[...]` — previously string keys were ignored.
    Destructuring with `[...]` or `list()` assigns array elements to variables
    by position: `$a, $b = [1, 2]` or `[$a, $b] = [1, 2]`. Keyed destructuring
    uses associative syntax: `['key1' => $a, 'key2' => $b] = [...]` or
    `list('key1' => $a, 'key2' => $b) = [...]`. Missing keys result in undefined
    variable warnings unless defaults are provided.

- id: php-arrays-03
  answer: |
    Assigning an array to a new variable or passing it to a function copies it
    (PHP uses copy-on-write, so the copy is deferred until one of the copies is
    modified). After assignment, modifying one does not affect the other. Using `&`
    creates a reference: both variables point to the same array, so changes to one
    are visible in the other. `array_merge` and `[...$arr1, ...$arr2]` create new
    arrays. References are useful for functions that need to modify the caller's
    array, but generally should be avoided in favor of returning values.

- id: php-arrays-04
  answer: |
    PHP coerces array keys as follows: string keys that are valid decimal integers
    (and not leading-zero padded like "01") are cast to int. So `"1"` becomes `1`
    (int). Float keys like `1.9` are truncated to `1` (int). `true` becomes `1`
    (int). `null` becomes `""` (empty string). So `$arr["1"]`, `$arr[1.9]`, and
    `$arr[true]` all access the same slot (key `1`), while `$arr[null]` accesses
    key `""`.

- id: php-null-01
  answer: |
    `??` (null coalescing) returns the left operand if it is not null, otherwise
    the right operand. It checks only for null. `?:` (short ternary / elvis) returns
    the left operand if it is truthy, otherwise the right operand. So if the left
    is `0`, `""`, `false`, or `null`, `?:` returns the right side, whereas `??`
    only returns the right side if the left is `null` — `0`, `""`, and `false` are
    returned as-is by `??`. This means `0 ?: 'default'` returns `'default'`, but
    `0 ?? 'default'` returns `0`.

- id: php-null-02
  answer: |
    The nullsafe operator `?->` calls a method or accesses a property, but only
    if the object it is called on is not null. If the left side is null, the
    entire expression short-circuits and returns null without calling the method.
    In `$a?->b()->c`, if `$a` is null, the whole expression returns null — `b()`
    is never called. However, if `$a` is not null but `b()` returns null, then
    `->c` on null would throw an error (the short-circuit only applies to the
    initial null check). Short-circuiting means once a null is encountered in the
    chain, the rest of the chain is skipped.

- id: php-null-03
  answer: |
    `$x ??= $y` assigns `$y` to `$x` only if `$x` is null (or not set). It is
    equivalent to `$x = $x ?? $y`. The difference from a manual `$x = $x ?? $y`
    is minimal in behavior but `??=` is more concise and avoids evaluating `$x`
    twice. Note: if `$x` is not set at all, `??=` will not emit an undefined
    variable warning (same as `??`), whereas a plain `$x = $x ?? $y` without
    `??` would.

- id: php-null-04
  answer: |
    `isset($x)` returns true if `$x` is declared and is not null. `empty($x)`
    returns true if `$x` is not set, is null, or is a falsy value (0, "", false,
    [], etc.). `$x === null` returns true only if `$x` is exactly null (or not set,
    which throws a warning). A value where they disagree: `0` — `isset(0)` is
    true (it is set and not null), but `empty(0)` is true (it is falsy). Similarly,
    `""` is isset but empty.

- id: php-match-01
  answer: |
    `match` uses strict comparison (`===`) while `switch` uses loose comparison
    (`==`). `match` has no fallthrough — each arm is independent. `match` is an
    expression (returns a value), while `switch` is a statement. When no arm
    matches and there is no `default`, `match` throws an `UnhandledMatchError`,
    whereas `switch` simply does nothing. `match` also evaluates the matched value
    only once, while `switch` may evaluate it multiple times across cases.

- id: php-match-02
  answer: |
    Because `match` uses `===`, it does not coerce types. A `switch` with `==`
    would consider `0 == "0"` and `0 == false` as matches. With `match`, matching
    `0` against the string `"0"` fails, and matching `0` against `false` also
    fails. So `match(0) { 0 => 'zero', "0" => 'string zero' }` matches `'zero'`
    and the `"0"` arm is never reached, while a `switch` with `==` would match
    both arms. This can surprise developers migrating from `switch`.

- id: php-match-03
  answer: |
    `switch` is still better when you need loose comparison behavior (matching
    multiple types against the same value), when you need fallthrough logic (executing
    multiple cases), or when working with legacy code where `switch` patterns are
    established. Also, `switch` with `case:` and `break` is needed when you want
    to execute multiple statements per case without wrapping in a block, though
    `match` supports this with braces too. In practice, `match` covers most cases,
    but `switch` remains useful for fallthrough patterns and loose-type matching
    scenarios.

- id: php-match-04
  answer: |
    `match(true)` evaluates the conditions in each arm against `true` using strict
    comparison. So `match(true) { $x > 0 => 'positive', $x === 0 => 'zero', ... }`
    works because each condition returns a boolean, and `true === true` is true.
    This replaces an if/elseif chain because each arm is a boolean expression, and
    the first matching arm (strictly equal to true) wins. It is concise, avoids
    repetition of the matched variable, and guarantees exactly one arm executes.
```
