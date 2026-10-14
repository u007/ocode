# cat-jam-free — closed-book answers for php (opencode-go, 2026-10-14); answerer saw only _prompts/php.md
# Delivery: one `ocode run -m opencode-go/cat-jam-free -effort med` session, sheet attached via -f, empty scratch cwd;
# session audited in sqlite: 1 user + 1 assistant message, 0 tool calls.

- id: php-types-01
  answer: |
    `declare(strict_types=1)` enables strict scalar typing. It must be the very first statement in a PHP file (immediately after `<?php`, before namespace/use or other code). In strict mode, scalar arguments must match parameter types exactly (with no implicit coercion), so passing `"1"` to an `int` parameter throws a `TypeError`; in the default coercive mode PHP juggles compatible scalar values.
- id: php-types-02
  answer: |
    `==` is loose equality and may coerce types before comparing; `===` is identity and requires the same type and value. A surprising loose case is `0 == "php"` evaluating to `true`, because the string is coerced to the integer `0`.
- id: php-types-03
  answer: |
    A union type (`A|B`) accepts any listed type, an intersection type (`A&B`) requires an object implementing all listed interfaces/types, and a nullable type allows one type plus null. `?T` is shorthand for `T|null`; they are equivalent for a single type.
- id: php-types-04
  answer: |
    `void` means the function returns no usable value (it may omit `return` or return empty). `never` means execution never completes normally (it must throw or terminate). `mixed` accepts or returns any value, including null. Use `void` for side-effect functions, `never` for functions that always throw/exit, and `mixed` when type information is intentionally absent.
- id: php-types-05
  answer: |
    Typed class constants, added in PHP 8.3, allow a type on `const` declarations. When a subclass overrides a typed constant, the overriding constant must use a compatible type; changing the declared type is not allowed in ordinary inheritance.
- id: php-enums-01
  answer: |
    A pure enum has no scalar backing value; a backed enum declares `enum E: int` or `enum E: string` and assigns a scalar value to each case. You need a backed enum when cases must map to or be constructed from an integer/string value, such as database or API representation.
- id: php-enums-02
  answer: |
    `cases()` returns all enum cases in declaration order. `from($value)` returns the matching backed-enum case or throws `ValueError` for an invalid value. `tryFrom($value)` returns the matching case or `null` for an invalid value. Pure enums do not have `from`/`tryFrom` or `->value`.
- id: php-enums-03
  answer: |
    Yes, enums can have methods, constants, and implement interfaces. Unlike classes, enums cannot be instantiated directly with `new`, cannot extend another class, and enum cases carry no mutable instance state.
- id: php-enums-04
  answer: |
    The same enum case is a canonical singleton instance, so `Suit::Hearts === Suit::Hearts` is true. This makes identity comparison and `match` reliable; a match arm keyed by `Suit::Hearts` matches exactly that case.
- id: php-oop-01
  answer: |
    Constructor property promotion (PHP 8.0) lets constructor parameters declare class properties directly, e.g. `public function __construct(public string $name) {}`. PHP creates the property and assigns it automatically, replacing separate property declarations and constructor assignments.
- id: php-oop-02
  answer: |
    A `readonly` property may be initialized only once, normally in the declaring class's constructor (or with a constant default at declaration); after initialization it cannot be reassigned. A `readonly` class makes all instance properties readonly, centralizing that immutability rule.
- id: php-oop-03
  answer: |
    `$this` is the current object instance. `self::` refers to the class where the method is written. `static::` uses late static binding and refers to the runtime-called class. Late static binding solves calls from a base class that need to resolve to the subclass actually invoked.
- id: php-oop-04
  answer: |
    Asymmetric visibility and property hooks were introduced in PHP 8.4. Asymmetric visibility lets read and write access differ, e.g. `public private(set)`. Property hooks allow getter/setter logic inside the property declaration, reducing the need for explicit accessor methods and manual backing fields.
- id: php-oop-05
  answer: |
    `#[\Override]` (PHP 8.3) declares that a method is intended to override a parent or interface method. It causes a compile-time error if no matching overridden method exists, catching typos, signatures changed elsewhere, or methods that accidentally became new members instead of overrides.
- id: php-closures-01
  answer: |
    A closure must explicitly import variables with `use`; by default it captures them by value. An arrow function automatically captures outer variables by value, with no `use` clause. Arrow functions cannot capture by reference, while closures can via `use (&$x)`.
- id: php-closures-02
  answer: |
    First-class callable syntax such as `strlen(...)` (PHP 8.1) evaluates to a `Closure` object wrapping the callable. It can be stored, passed, and invoked later.
- id: php-closures-03
  answer: |
    `Closure::bind()` and `bindTo()` create a closure with a different bound object and/or scope class. This changes `$this` and allows access to private/protected members according to the bound scope; binding to `null` unbinds the object.
- id: php-closures-04
  answer: |
    `use ($x)` captures the value at closure creation, so later changes to `$x` are not seen. `use (&$x)` captures by reference, so the closure sees and can modify the current value of `$x`. Thus, if `$x` changes after definition, the reference closure sees the new value.
- id: php-error-01
  answer: |
    `Throwable` is the base interface. `Error` represents engine/internals errors (`TypeError`, `ParseError`, etc.), while `Exception` represents normal user-thrown exceptions. Both can be caught with `catch (Throwable $e)` or `catch (Exception|Error $e)`.
- id: php-error-02
  answer: |
    `finally` runs after the `try` block and any matching `catch`, whether execution completes normally, returns, or throws. If both `catch` and `finally` return, the `finally` return takes precedence and overrides the earlier return.
- id: php-error-03
  answer: |
    Define a custom exception by extending `Exception` (or another exception subclass), then pass the lower-level throwable as the `previous` constructor argument. Chaining preserves the original error context and stack trace while exposing a higher-level, domain-specific exception.
- id: php-error-04
  answer: |
    `set_error_handler` handles PHP errors/warnings/notices as callback events, while `try`/`catch` works with `Throwable` objects. Warnings can be made catchable by installing an error handler that throws an `ErrorException` (respecting `error_reporting()`).
- id: php-arrays-01
  answer: |
    A "list" is an array with sequential integer keys starting at 0; an associative array has string or arbitrary integer keys. A PHP array is one ordered hash map/hash table structure, preserving insertion order for both key types.
- id: php-arrays-02
  answer: |
    Array spread (`...`) unpacks iterable elements into an array literal. Since PHP 8.1, string keys are supported; duplicate string keys follow later-overwrites-earlier collision rules. Keyed destructuring uses `['key' => $var]` or `list('key' => $var)` to assign values by key.
- id: php-arrays-03
  answer: |
    Array assignment or passing is value semantics implemented with copy-on-write: the array is logically copied, but internal data is shared until a mutation triggers separation. A `&` reference creates an actual alias, so both variables always refer to the same underlying array.
- id: php-arrays-04
  answer: |
    PHP coerces array keys: numeric string `"1"` becomes integer `1`; float `1.9` is truncated to integer `1`; boolean `true` becomes integer `1`; `null` becomes the empty string `""`. Non-numeric strings remain strings.
- id: php-null-01
  answer: |
    `??` returns the left operand if it exists and is not null; otherwise it returns the right operand and tolerates undefined variables. `?:` returns the left operand only if it is truthy; it treats `0`, `""`, `false`, `null`, and `[]` as fallback values and warns on an undefined variable.
- id: php-null-02
  answer: |
    `?->` calls a property, method, or element only when the left side is not null; otherwise the whole expression evaluates to null. In a chain such as `$a?->b()->c`, if `$a` is null the call to `b()` is not evaluated and the chain short-circuits to null.
- id: php-null-03
  answer: |
    `??=` assigns the right-hand value only if the variable is unset or null. It evaluates the left side once and avoids re-evaluating it, whereas `$x = $x ?? $y` conceptually reads `$x` twice.
- id: php-null-04
  answer: |
    `isset($x)` is true only when `$x` exists and is not null. `empty($x)` is true when `$x` is missing or falsy. `$x === null` is true only when an existing variable has the value null. They disagree for `$x = 0`: `isset` is true, `empty` is true, and `=== null` is false.
- id: php-match-01
  answer: |
    `match` is an expression that returns a value, uses strict `===` comparison, has no fallthrough, and throws `UnhandledMatchError` for an unmatched value. `switch` is a statement, uses loose comparison, allows fallthrough, and does nothing if no case matches.
- id: php-match-02
  answer: |
    Because `match` uses strict comparison, loose equivalences do not match. For example, with value `0`, a `switch` may match `case 'foo'` because `0 == 'foo'` is true, while `match` does not.
- id: php-match-03
  answer: |
    `switch` is still useful when loose comparison is desired, when intentional fallthrough groups cases, or when the branches need to execute statements as part of control flow rather than produce a single expression value.
- id: php-match-04
  answer: |
    `match (true) { $cond1 => ..., $cond2 => ...; default => ...; }` compares `true` strictly to each arm expression. The first arm whose expression evaluates to `true` wins, so it behaves like an if/elseif chain with guaranteed single-branch selection.
