# cat-jam-free — WITH-SKILL closed-book answers for php (opencode-go, 2026-10-14)
# Delivery: derived/php.cat-jam-free.SKILL.md body (front matter stripped) prepended to _prompts/php.md; one
# `ocode run -m opencode-go/cat-jam-free -effort med` session, isolated HOME with every tool denied, cwd holding only the sheet;
# session audited in sqlite: 1 user + 1 assistant message, 0 tool calls.

- id: php-types-01
  answer: |
    `declare(strict_types=1);` must be the very first statement in the file. It makes scalar type checks strict for that file only: no implicit coercion for declared parameter, return, or property types. In default coercive mode, PHP may convert compatible scalars (for example string "1" to int); under strict_types this throws a TypeError instead.

- id: php-types-02
  answer: |
    `==` compares values after type juggling; `===` compares both value and type without coercion. A classic surprise is `0 == "foo"` being true in PHP 7 and earlier because the non-numeric string is converted to integer 0; PHP 8 changed non-numeric string comparisons so this is no longer true. Other loose surprises include `null == false`, `0 == false`, and `'' == 0`.

- id: php-types-03
  answer: |
    Union types (`A|B`) accept a value matching any listed type. Intersection types (`A&B`) accept a value matching every listed type. Nullable types accept either the given type or null. `?T` is shorthand for `T|null`; it is the nullable form of a single type, while `T|null` is the equivalent union form.

- id: php-types-04
  answer: |
    `void` means the function returns no useful value and must not return a value. `never` means the function never returns normally, such as because it throws or exits. `mixed` accepts any value, including null. Use `void` for ordinary side-effect procedures, `never` for terminal/error helpers, and `mixed` only when deliberately accepting anything.

- id: php-types-05
  answer: |
    Typed class constants, added in PHP 8.3, declare a type on a constant, for example `public const int MAX = 10;`. A subclass overriding a typed constant must keep the same type; typed constant types are invariant.

- id: php-enums-01
  answer: |
    A pure enum has no scalar backing value. A backed enum is declared with `enum Name: int` or `enum Name: string` and each case has a unique scalar backing value. Use a backed enum when cases must be stored, transmitted, or mapped to database/API values.

- id: php-enums-02
  answer: |
    `cases()` returns all enum cases in declaration order. `from($value)` returns the matching case or throws a `ValueError` for an invalid value. `tryFrom($value)` returns the matching case or null for an invalid value.

- id: php-enums-03
  answer: |
    Yes, enums can define methods and constants and can implement interfaces. Unlike normal classes, enums cannot be extended or subclassed, cannot have mutable instance state or dynamic properties, and their cases are singleton instances that cannot be created with `new`.

- id: php-enums-04
  answer: |
    They are the same singleton instance, so `$a === $b` is true. In `match`, cases are compared with strict identity, so the same singleton matches exactly.

- id: php-oop-01
  answer: |
    Constructor property promotion lets visibility, type, readonly, and default values be declared directly on constructor parameters; PHP then declares and assigns the corresponding property automatically. It replaces the usual boilerplate of declaring class properties and assigning them in the constructor body.

- id: php-oop-02
  answer: |
    A readonly property can be written only during initialization, normally in the constructor or from the declaring class scope, and only once. Any later write throws `Error`. It must be typed. A readonly class makes all declared instance properties readonly, forbids dynamic properties, and only readonly classes can extend or be extended by readonly classes.

- id: php-oop-03
  answer: |
    `$this` is the current object instance. `self` is resolved at compile time to the class where the code is written. `static` uses late static binding, resolving at runtime to the class actually called. This solves factory and fluent methods in parent classes incorrectly creating or referring to the parent instead of a subclass.

- id: php-oop-04
  answer: |
    Asymmetric visibility and property hooks were introduced in PHP 8.4. Asymmetric visibility allows different read and write visibility. Property hooks allow getter and setter logic directly on a property. Together they help avoid manually written accessor methods and boilerplate getters/setters while preserving encapsulation.

- id: php-oop-05
  answer: |
    `#[\Override]` declares that a method is intended to override a parent or interface method. It catches the bug where a method name, signature, or target no longer actually overrides anything, such as due to typos or upstream API changes.

- id: php-closures-01
  answer: |
    A traditional closure uses `use` to explicitly import variables from the surrounding scope. An arrow function automatically captures variables by value from the enclosing scope and consists of a single expression whose result is returned implicitly.

- id: php-closures-02
  answer: |
    `strlen(...)` is first-class callable syntax introduced in PHP 8.1. It does not call the function; it produces a `Closure` callable representing `strlen`.

- id: php-closures-03
  answer: |
    `Closure::bind` and `bindTo` create a copy of a closure with a new bound `$this` object and/or class scope. This changes which object `$this` refers to and can grant access to private or protected members depending on the scope provided.

- id: php-closures-04
  answer: |
    `use ($x)` captures `$x` by value at the time the closure is created, so later changes to the outer `$x` are not seen. `use (&$x)` captures by reference, so the closure sees the current value of `$x` whenever it runs.

- id: php-error-01
  answer: |
    Both `Error` and `Exception` implement `Throwable`. `Error` represents engine, internal, or severe programming errors such as `TypeError` and `ParseError`. `Exception` represents application-level exception conditions. Catch both with `catch (Throwable $e)` or separate `catch` blocks for each.

- id: php-error-02
  answer: |
    `finally` runs after the try and catch blocks, whether or not an exception occurred and whether or not an earlier block returned. A `return` in `finally` overrides a return from try or catch and suppresses a propagating exception.

- id: php-error-03
  answer: |
    Define a custom exception by extending `Exception`, then pass a previous throwable to the constructor's third parameter: `new CustomException($message, $code, $previous)`. Chaining preserves the original cause and stack trace for debugging while exposing a clearer domain-level error.

- id: php-error-04
  answer: |
    `set_error_handler` handles PHP error levels such as warnings and notices; `try`/`catch` only handles thrown `Throwable` objects. Convert warnings to catchable exceptions by installing an error handler that throws `ErrorException`, for example from `E_WARNING`.

- id: php-arrays-01
  answer: |
    PHP has one ordered map type. A "list" is conventionally an array with sequential integer keys starting at 0. An associative array uses meaningful string or integer keys. Internally, an array is an ordered hash table preserving insertion order and using copy-on-write semantics.

- id: php-arrays-02
  answer: |
    The spread operator in array literals unpacks iterable values into the new array. Since PHP 8.1, string keys are also supported; numeric keys are generally reindexed. Array destructuring with `[$a, $b] = $arr` or `list($a, $b) = $arr` assigns by position, while keyed forms such as `['id' => $id] = $row` assign by key.

- id: php-arrays-03
  answer: |
    Assignment or passing an array behaves as a value copy; implementation uses copy-on-write, so the actual copy is deferred until mutation. A `&` reference makes variables share the same array zval, so writes through either alias are visible to all aliases with no copy-on-write separation.

- id: php-arrays-04
  answer: |
    Array keys are coerced: numeric strings become integers, floats are truncated to integers, `true` becomes 1, `false` becomes 0, and `null` becomes the empty string `""`. Therefore `"1"`, `1.9`, and `true` can collide with integer key `1`, while `null` uses `""`.

- id: php-null-01
  answer: |
    `??` returns the left operand if it is set and not null, otherwise the right; it does not count falsy values such as 0, false, or empty string as null. `?:` returns the left operand only if it is truthy, otherwise the right, and may emit an undefined-variable notice when appropriate.

- id: php-null-02
  answer: |
    The nullsafe operator `?->` returns null if the left-hand side is null and otherwise accesses the property or calls the method. In a chain, a null result short-circuits the rest of that `?->` chain, preventing calls on null without additional null checks.

- id: php-null-03
  answer: |
    `??=` assigns the right side to the left side only when the left side is null or unset. Unlike `$x = $x ?? $y`, it does not write a new value when the left side already exists and is non-null.

- id: php-null-04
  answer: |
    `isset($x)` is true only if `$x` exists and is not null. `empty($x)` is true if `$x` is unset or falsy. `$x === null` is true only for an actual null value and may emit a notice for an undefined variable. They disagree for a truthy value such as `$x = 'abc'`: `isset` is true while `empty` is false.

- id: php-match-01
  answer: |
    `match` is an expression, uses strict `===` comparison, has no fallthrough, and throws `UnhandledMatchError` if no arm matches. `switch` is a statement, uses loose `==` comparison, allows fallthrough unless `break` is used, and does nothing when no case matches.

- id: php-match-02
  answer: |
    Because matching is strict, `match (0)` will not match `false`, `null`, or the string `'0'`; only integer `0` matches. Loose `switch` can incorrectly group those values under a `case 0`.

- id: php-match-03
  answer: |
    `switch` is better when cases need multiple statements or side effects, when fallthrough is intentionally useful, or when loose comparison behavior is desired.

- id: php-match-04
  answer: |
    With `match(true)`, each case key is a boolean condition. The first arm whose condition is strictly equal to `true` is selected, so it behaves like an ordered `if`/`elseif` chain while still being an expression.
