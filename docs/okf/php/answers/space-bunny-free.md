- id: php-types-01
  answer: |
    `declare(strict_types=1);` enables strict type checking for scalar parameter and return types. It must be the first statement in the PHP file. The setting applies according to the file containing the call, not according to where the called function was defined.

    By default, PHP coerces values such as numeric strings and integers to compatible scalar parameter types. In strict mode, those conversions generally cause a `TypeError`; for example, passing `"10"` to an `int` parameter fails, although the permitted widening from `int` to `float` still works.

- id: php-types-02
  answer: |
    `==` performs type-juggling and value comparison, while `===` requires both the same value and the same type. `===` is therefore the safer choice when `"0"` and `0`, or `false` and `0`, must remain distinct.

    A surprising result is that `"0" == 0` and `false == 0` are both true, despite the operands having different types.

- id: php-types-03
  answer: |
    A union type such as `int|string` accepts a value of either declared type. An intersection type such as `Countable&ArrayAccess` requires an object to satisfy all the listed types.

    A nullable declaration such as `?int` is shorthand for `int|null`; they are equivalent and permit either an integer or `null`.

- id: php-types-04
  answer: |
    `never` is the return type of a function that never completes normally, such as one that always throws, calls `exit`, or recursively calls itself indefinitely. It may be used for exhaustive failure paths.

    `void` means the function does not return a usable value. It may finish without a `return`, or use `return;`, but cannot return an expression.

    `mixed` is the broadest type and accepts any value, including `null`. Use it when the function deliberately handles values of unknown or varying types.

- id: php-types-05
  answer: |
    PHP 8.3 added types to class constants, for example:

    ```php
    class Config
    {
        public const string MODE = 'production';
    }
    ```

    Constant types are invariant. If a subclass overrides a typed constant, it must declare exactly the same type; it may change the value, but it cannot widen, narrow, or remove the type.

- id: php-enums-01
  answer: |
    A pure enum has named cases but no associated scalar values:

    ```php
    enum Suit
    {
        case Hearts;
        case Spades;
    }
    ```

    A backed enum assigns each case an `int` or `string` backing value:

    ```php
    enum Suit: string
    {
        case Hearts = 'H';
        case Spades = 'S';
    }
    ```

    A backed enum is needed when cases must map to database values, configuration values, or other scalar identifiers and be converted back into cases reliably. A single backed enum cannot mix string and integer backing types.

- id: php-enums-02
  answer: |
    `cases()` returns an array of all enum cases, with case names as array keys:

    ```php
    $cases = Suit::cases();
    ```

    On a backed enum, `from($value)` returns the case having that backing value. If no case matches, it throws a `ValueError`.

    `tryFrom($value)` performs the same lookup but returns `null` instead of throwing when no case matches. Supplying an unsupported argument type can still produce a `TypeError`.

- id: php-enums-03
  answer: |
    Enums may define methods and constants, implement interfaces, and use traits. They are restricted compared with ordinary classes:

    - An enum is final and cannot extend a class or another enum.
    - Cases cannot be instantiated with `new` or otherwise subclassed.
    - Enums cannot declare properties or dynamic properties.
    - A constructor is not publicly available; cases cannot contain per-instance state.
    - Enum cases cannot be cloned or unserialized.
    - Each case is a singleton instance.

- id: php-enums-04
  answer: |
    Yes. Enum cases are singleton objects, so two variables assigned `Suit::Hearts` refer to the same case instance. Consequently, `$a === $b` is true, and either variable matches this arm:

    ```php
    $result = match ($card->suit) {
        Suit::Hearts => 'Red',
        Suit::Diamonds => 'Red',
        default => 'Black',
    };
    ```

    `match` uses strict comparison, but matching the same enum case succeeds because it is the same singleton instance.

- id: php-oop-01
  answer: |
    Constructor property promotion, introduced in PHP 8.0, combines a constructor parameter with an automatically declared class property:

    ```php
    class User
    {
        public function __construct(
            public readonly string $name,
            private int $age
        ) {}
    }
    ```

    It replaces the separate matching property declarations and the assignments that would otherwise initialize them in the constructor body. Property visibility and `readonly` can be specified directly on the promoted parameter.

- id: php-oop-02
  answer: |
    PHP 8.1 introduced `readonly` properties. A readonly property must have a type and can receive its one initial value only from the scope where it is declared, commonly from its constructor. Once initialized, no method can modify or unset it.

    PHP 8.2 introduced `readonly` classes. Marking a class readonly makes all its instance properties readonly, requires those properties to be typed, and prevents dynamic properties. A non-readonly class cannot extend a readonly class. This provides class-wide immutability rather than having to mark each property separately.

- id: php-oop-03
  answer: |
    `$this` refers to the object on which the current instance method was invoked.

    `self` refers to the class where the method was lexically defined. `self::method()` therefore uses that defining class, even when the method was inherited by another class.

    `static` is resolved at runtime using the called class. Thus, if `Base::create()` contains `new static()`, calling it as `Child::create()` constructs a `Child`; using `new self()` would construct a `Base`.

    This is late static binding, which preserves polymorphic behavior for static calls and `new` inside inherited methods.

- id: php-oop-04
  answer: |
    PHP 8.4 introduced both asymmetric property visibility and property hooks.

    Asymmetric visibility allows broader access for reading than for writing:

    ```php
    public private(set) string $status = 'draft';
    ```

    Everyone can read `$status`, but only the declaring class can assign it.

    Property hooks let a declared property define custom `get` and optional `set` behavior. They support computed/virtual properties and can intercept writes while still allowing direct property syntax. They avoid many manual getter/setter methods and reduce the need for property-specific `__get()` and `__set()` magic methods.

- id: php-oop-05
  answer: |
    PHP 8.3's `#[\Override]` attribute marks a method as intentionally overriding a parent method or implementing an interface method. The engine then verifies that such a method actually exists.

    It primarily catches refactoring mistakes where a method name is misspelled, a parent method was renamed or removed, or an interface implementation was accidentally renamed. Such a method might otherwise compile as an unrelated new method, leaving the intended override missing. Ordinary incompatible-signature errors are still enforced separately by PHP.

- id: php-closures-01
  answer: |
    A normal closure does not automatically capture outer variables; they must be listed with `use`, either by value or by reference:

    ```php
    $factor = 2;
    $double = function (int $n) use ($factor): int {
        return $n * $factor;
    };
    ```

    An arrow function automatically captures variables used in its expression, by value, from the surrounding scope:

    ```php
    $double = fn (int $n): int => $n * $factor;
    ```

    Arrow functions are single-expression functions whose expression is their return value and do not use a `use` clause.

- id: php-closures-02
  answer: |
    First-class callable syntax, introduced in PHP 8.1, converts a callable into a `Closure` without immediately calling it:

    ```php
    $length = strlen(...);
    $value = $length('hello');
    ```

    It can also be used with static methods and instance methods, such as `Foo::bar(...)` and `$object->method(...)`. This provides a typed, first-class alternative to manually writing `'strlen'`, `[$object, 'method']`, or repeatedly using `call_user_func()`.

- id: php-closures-03
  answer: |
    `Closure::bind()` and a closure's `bindTo()` method return a new closure associated with a different object and/or class scope:

    ```php
    $bound = Closure::bind($closure, $object, SomeClass::class);
    ```

    Rebinding changes which object becomes `$this` inside the closure. Changing the scope can also change which private or protected class members the closure may access. The original closure is not modified. If rebinding cannot succeed, the operation reports failure rather than changing it.

- id: php-closures-04
  answer: |
    `use ($x)` imports `$x` by value, while `use (&$x)` imports it by reference.

    ```php
    $x = 1;
    $read = function () use ($x) {
        return $x;
    };
    $reference = function () use (&$x) {
        return $x;
    };

    $x = 2;
    ```

    After `$x` changes, `$read()` still returns `1`, its captured snapshot. `$reference()` sees and can modify the current value, so it returns `2`.

- id: php-error-01
  answer: |
    `Throwable` is the top-level interface implemented by both `Exception` and `Error`.

    `Exception` generally represents expected, recoverable conditions such as failed file operations or invalid application input. `Error` generally represents language-level or engine-level problems such as `TypeError`, `ArgumentCountError`, `ParseError`, or `DivisionByZeroError`. `Error` is not a subclass of `Exception`.

    To catch both categories:

    ```php
    try {
        // code
    } catch (Exception $e) {
        // expected exceptions
    } catch (Error $e) {
        // engine/language errors
    }
    ```

    Alternatively, catch `Throwable` to handle both.

- id: php-error-02
  answer: |
    `try` contains code that may throw. PHP evaluates matching `catch` clauses, if any, and then executes `finally` when control leaves the `try`/`catch` structure, including through `return`, `break`, `continue`, or a propagating exception.

    If `finally` does not return, an earlier return value is preserved. If both the `try`/`catch` and `finally` contain a `return`, the `finally` return wins, and the earlier return is discarded. Similarly, a throw from `finally` supersedes pending normal completion or an earlier exception.

- id: php-error-03
  answer: |
    Define a custom exception by extending `Exception` or one of its subclasses, such as `RuntimeException`:

    ```php
    final class OrderFailed extends RuntimeException
    {
    }

    try {
        createOrder();
    } catch (PDOException $e) {
        throw new OrderFailed('Could not create order', 0, $e);
    }
    ```

    The third constructor argument is the previous throwable. It is retrieved with `getPrevious()`. Chaining preserves the lower-level cause and its stack trace while adding higher-level context appropriate to the caller.

- id: php-error-04
  answer: |
    `try`/`catch` handles values thrown as `Exception` or `Error` objects. Most PHP warnings are diagnostics, not thrown objects, so `try`/`catch` does not catch them directly. For example, `fopen()` can emit a warning and return `false` without throwing.

    `set_error_handler()` registers a callback for handled error severities. The callback can convert selected warnings into an exception:

    ```php
    set_error_handler(
        function (
            int $severity,
            string $message,
            string $file,
            int $line
        ): bool {
            if (!(error_reporting() & $severity)) {
                return false;
            }

            throw new ErrorException(
                $message, 0, $severity, $file, $line
            );
        }
    );

    try {
        $file = fopen('/missing/file', 'r');
    } catch (ErrorException $e) {
        // Handle the warning as an exception.
    } finally {
        restore_error_handler();
    }
    ```

    Returning `false` from the handler delegates to PHP's normal error handling.

- id: php-arrays-01
  answer: |
    A list is an array whose keys are consecutive integers starting at zero. An associative array uses meaningful string keys or arbitrary integer keys.

    PHP has no separate list data structure. Every array is implemented internally as an ordered hash table containing key/value pairs. Keys may be integers or strings, values may have any type, and arrays can contain other arrays.

- id: php-arrays-02
  answer: |
    The spread operator unpacks array elements into another array literal or into destructuring:

    ```php
    $combined = [...$first, ...$second];
    [$head, ...$tail] = $values;
    ```

    PHP 8.1 added support for unpacking arrays with string keys. Later string keys with the same name override earlier ones; integer keys are still renumbered rather than preserved.

    Destructuring assigns values in positional or keyed order:

    ```php
    [$x, $y] = [10, 20];
    ['name' => $name, 'id' => $id] = $user;

    list('name' => $name, 'id' => $id) = $user;
    ```

    Keyed destructuring retrieves the named keys rather than depending on their position. The short `[...]` syntax and `list()` support the same destructuring behavior.

- id: php-arrays-03
  answer: |
    Ordinary assignment or passing an array gives the destination a value that is logically independent from the source. PHP initially uses copy-on-write, so both may share the same internal storage as long as neither is modified. Once one is modified, PHP separates the storage, and later modifications do not affect the other.

    Objects contained inside the array are not deeply copied. Both arrays may still refer to the same object, so changing that object's properties can be visible through either array.

    With `&`, the two variables are references to the same value container. Passing by reference or modifying through the reference immediately affects the other variable, with no copy-on-write separation.

- id: php-arrays-04
  answer: |
    PHP converts array keys to integers or strings according to these rules:

    - `"1"` becomes integer `1`.
    - `1.9` becomes integer `1`, truncating toward zero.
    - `true` becomes integer `1`; `false` becomes integer `0`.
    - `null` becomes the empty string `""`.

    Decimal integer strings without a leading `+`, such as `"1"`, are converted. Strings such as `"01"`, `"1.0"`, and `"-1"` remain string keys.

- id: php-null-01
  answer: |
    `??` is the null-coalescing operator. It returns its left operand when it exists and is not `null`; otherwise it evaluates and returns the right operand. An undefined variable on the left does not produce a warning.

    `?:` is the short ternary or elvis operator. It returns the left operand when it is truthy; otherwise it returns the right operand. It treats values such as `false`, `0`, `0.0`, `""`, `"0"`, `[]`, and `null` as falsey.

    Thus, `0 ?? 10` is `0`, while `0 ?: 10` is `10`.

- id: php-null-02
  answer: |
    PHP 8.0's nullsafe operator `?->` returns `null` instead of attempting the operation when the left-hand value is `null` or unset.

    For:

    ```php
    $result = $a?->b()->c;
    ```

    If `$a` is `null` or unset, evaluation short-circuits and the remainder of the chain is not evaluated. If `$a` is non-null, normal execution continues.

    Short-circuiting here does not protect the entire chain from a later null result. If `b()` returns `null`, `->c` will fail; use `$a?->b()?->c()` when every stage may be null.

- id: php-null-03
  answer: |
    `??=` performs null-coalescing assignment:

    ```php
    $x ??= $y;
    ```

    If `$x` is uninitialized or `null`, it receives `$y`; otherwise `$x` is left unchanged. For a simple variable, it is semantically equivalent to:

    ```php
    $x = $x ?? $y;
    ```

    Both evaluate the right-hand side only when needed and avoid warning about an uninitialized left-hand variable.

- id: php-null-04
  answer: |
    `isset($x)` is true when `$x` exists and is not `null`; it does not warn for an undefined variable.

    `empty($x)` is true for an undefined or `null` variable and also for falsey values such as `false`, `0`, `0.0`, `""`, `"0"`, and an empty array. It likewise does not warn for an undefined variable.

    `$x === null` checks specifically whether the value is `null`, but evaluating an undefined `$x` can produce a warning.

    For example, `0` makes `isset($x)` true while `empty($x)` is true.

- id: php-match-01
  answer: |
    `match` uses strict comparison (`===`), not loose comparison. `switch` uses loose comparison (`==`).

    `match` has no fallthrough: it stops after the first matching arm. `switch` normally continues into later cases until it reaches `break`, allowing deliberate fallthrough.

    Each `match` arm is an expression whose value becomes the result of the whole `match`, which can be assigned or returned. A traditional `switch` is a control-flow statement.

    If no `match` arm matches, `match` throws an `UnhandledMatchError`, unless a `default` arm is present. A `switch` without a match and without `default` simply does nothing.

- id: php-match-02
  answer: |
    Strict matching does not perform the type juggling that may occur in `switch`. For example:

    ```php
    switch ("0") {
        case 0:
            // Matches because "0" == 0.
    }

    match ("0") {
        0 => 'wrong',
        default => 'strict result',
    };
    ```

    The `match` does not match integer `0` because `"0"` is a string. Similarly, `false`, `null`, and string `"0"` do not strictly match integer `0` in a `match`, although loose `switch` comparisons may group them with `0`. Object values also match by identity rather than merely having equivalent properties.

- id: php-match-03
  answer: |
    `switch` is still preferable when loose comparison is intentional, when several case labels must intentionally share one block, or when deliberate fallthrough is required. It also fits traditional procedural control flow built around `break` and `continue`.

    A `match` expression is generally better when selecting one value, especially when strict type distinctions matter and no fallthrough is intended.

- id: php-match-04
  answer: |
    Use `match(true)` when each arm is a boolean condition:

    ```php
    $label = match (true) {
        $score >= 90 => 'A',
        $score >= 80 => 'B',
        $score >= 70 => 'C',
        default => 'F',
    };
    ```

    `match` compares its subject, `true`, against each arm expression using `===`. The conditions are evaluated in order, the first expression that evaluates to boolean `true` supplies the result, and `default` handles all remaining cases. This is similar to an `if`/`elseif` chain, but the conditions must produce actual booleans rather than merely truthy values.
