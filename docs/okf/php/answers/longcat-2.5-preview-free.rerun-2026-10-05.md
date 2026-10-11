- id: php-types-01
  answer: |
    `declare(strict_types=1)` enables strict type checking for scalar type declarations (int, float, string, bool) at the call site. It must be the very first statement in the file — before any namespace declaration, use statements, or other code. In coercive mode (the default), PHP silently coerces values to match the declared type (e.g., passing the string "42" to an `int` parameter works, passing `1.5` to an `int` parameter truncates to `1`). In strict mode, only exact type matches are accepted; any mismatch throws a `TypeError`. Note that strict_types affects the file where the call is made, not the file where the function is defined.

- id: php-types-02
  answer: |
    `==` is loose comparison — it performs type juggling before comparing values. `===` is strict comparison — it requires both the value AND the type to be identical. A surprising case with `==`: in PHP < 8.0, `0 == "abc"` evaluates to `true` because the non-numeric string "abc" is coerced to integer 0. (PHP 8.0 fixed this to `false`.) Another example: `"1e2" == "100"` is `true` because both are coerced to the float 100.0, but `"1e2" === "100"` is `false` because they are different strings.

- id: php-types-03
  answer: |
    Union types (PHP 8.0) allow a parameter, return, or property to accept multiple types, written as `A|B`. Intersection types (PHP 8.1) require a value to satisfy ALL listed types simultaneously, written as `A&B` — useful for requiring multiple interfaces. Nullable types allow `null` in addition to a type. The `?T` syntax is exactly equivalent to `T|null` — they are interchangeable in all contexts, and `?T` is simply shorthand.

- id: php-types-04
  answer: |
    `never` (PHP 8.1) is a return type indicating the function never returns normally — it always throws an exception, calls `exit()`, or triggers a fatal error. `void` indicates the function completes execution but returns no meaningful value (either no `return` statement or a bare `return;`). `mixed` is the top type — any value is acceptable, equivalent to `array|bool|callable|int|float|null|object|resource|string`. Use `never` for functions that always diverge, `void` for functions that complete but produce no result, and `mixed` when any type is genuinely acceptable (e.g., a passthrough function).

- id: php-types-05
  answer: |
    Typed class constants (PHP 8.3) allow specifying a type on class constants, e.g., `const int FOO = 42;`. When a subclass overrides a typed constant, the overriding type must be compatible (covariant) with the parent's type — you cannot widen or change the type arbitrarily. This ensures type safety when constants are accessed through inheritance.

- id: php-enums-01
  answer: |
    A pure enum is a set of named constants with no associated data — cases are just names. A backed enum (PHP 8.1) associates each case with a scalar value (int or string), declared with `: int` or `: string` after the enum name. You need a backed enum when you must store, serialize, or compare the underlying value — e.g., persisting to a database, mapping to API responses, or comparing against external data. Pure enums suffice when you only need type-safe named constants.

- id: php-enums-02
  answer: |
    `cases()` returns an array of all enum cases in declaration order. `from()` returns the enum case for a given backing value, throwing a `ValueError` if the value is not found. `tryFrom()` does the same lookup but returns `null` instead of throwing when the value is invalid — it is the non-throwing variant, useful when the input may not correspond to a valid case.

- id: php-enums-03
  answer: |
    Yes, enums can have methods, constants, and implement interfaces. Key restrictions versus a normal class: enums cannot be instantiated with `new`, cannot have properties, cannot extend other classes (they implicitly extend the internal `UnitEnum` base for pure enums or `BackedEnum` for backed enums), and all cases are inherently static and public. Enums also cannot have a constructor or destructor.

- id: php-enums-04
  answer: |
    Yes, enum cases are singletons — `Suit::Hearts` always refers to the same instance throughout the application. This means `===` comparison works correctly (identity comparison), and `match` works as expected because it uses strict comparison. Two variables holding `Suit::Hearts` are the same object, so `$a === $b` is `true`.

- id: php-oop-01
  answer: |
    Constructor property promotion (PHP 8.0) allows declaring and initializing class properties directly in the constructor parameter list by prefixing parameters with visibility modifiers (`public`, `private`, `protected`) and/or `readonly`. It replaces the boilerplate of separately declaring properties at the top of the class and then assigning them in the constructor body (`$this->x = $x`).

- id: php-oop-02
  answer: |
    Readonly properties (PHP 8.1) can only be initialized once, and only from within the class scope (typically in the constructor or a method of the same class). Any subsequent write attempt throws an `Error`. Readonly classes (PHP 8.2) automatically mark all declared properties as readonly and additionally prevent the creation of dynamic properties on instances of that class.

- id: php-oop-03
  answer: |
    `self` refers to the class where the method is written (compile-time binding). `static` refers to the class that was actually called at runtime (late static binding). `$this` refers to the current object instance. Late static binding solves the problem where `self` always resolves to the class where it appears, not the class that was invoked — `static` defers the resolution to runtime, enabling proper inheritance behavior for static methods and properties.

- id: php-oop-04
  answer: |
    Asymmetric visibility (PHP 8.4) allows different visibility for reading versus writing a property, e.g., `public private(set) string $name` — readable publicly but writable only within the class. Property hooks (PHP 8.4) allow intercepting property access with `get` and `set` hooks, enabling custom logic on read/write without explicit getter/setter methods. Together they let you avoid boilerplate getter/setter methods, reduce validation code, and enforce invariants more cleanly.

- id: php-oop-05
  answer: |
    The `#[\Override]` attribute (PHP 8.3) marks a method as intentionally overriding a parent class method or implementing an interface method. It catches the bug where a method name is misspelled or a parent method is renamed — PHP will throw a compile-time error if no matching parent or interface method exists, preventing silent failures where the override was never actually applied.

- id: php-closures-01
  answer: |
    A closure defined with `function () use ($x) {}` captures variables by value — it receives a copy of `$x` at the time the closure is defined. An arrow function `fn () => $x` automatically captures variables by value from the enclosing scope without needing a `use` clause. The key difference is syntactic: arrow functions have implicit by-value capture, while closures require explicit `use` and can optionally capture by reference with `use (&$x)`.

- id: php-closures-02
  answer: |
    The first-class callable syntax `strlen(...)` (PHP 8.1) creates a `Closure` object from any callable. The `...` is a literal placeholder indicating that arguments will be supplied later. It produces a Closure that can be stored in a variable, passed as an argument, or returned — enabling deferred invocation of any callable without needing `Closure::fromCallable()`.

- id: php-closures-03
  answer: |
    `Closure::bind($closure, $newThis, $newScope)` and `$closure->bindTo($newThis, $newScope)` rebind the closure's `$this` and class scope to a different object and class. This allows the closure to access private and protected members of the bound object's class. The scope parameter controls which class's private/protected members are accessible from within the closure.

- id: php-closures-04
  answer: |
    `use ($x)` captures by value — the closure sees the value of `$x` at the moment the closure is defined. `use (&$x)` captures by reference — the closure sees the current value of `$x` at the time the closure is called. If `$x` changes after the closure is defined, a by-value closure will not see the change, while a by-reference closure will.

- id: php-error-01
  answer: |
    The throwable hierarchy: `Throwable` is the base interface. It is implemented by two main branches: `Error` (for runtime errors like `TypeError`, `DivisionByZeroError`, `ParseError`) and `Exception` (for userland exceptions). `Error` typically represents fatal or recoverable engine errors, while `Exception` represents conditions that user code can catch and handle. To catch both, use `catch (Throwable $t)` — this catches any `Error` or `Exception`.

- id: php-error-02
  answer: |
    `finally` always executes after the `try` and `catch` blocks complete, regardless of whether an exception was thrown, caught, or not. If both the `try`/`catch` block and the `finally` block contain `return` statements, the `finally` block's return value overrides and replaces the value from the `try`/`catch` block. The `finally` block runs even if the `try` block contains `return`, `break`, or `continue`.

- id: php-error-03
  answer: |
    Define a custom exception by extending `Exception` or one of its subclasses. Chain a lower-level exception by passing it as the third argument to the constructor: `throw new MyException("High-level message", 0, $previousException)`. Chaining preserves the full stack trace context, making it easier to debug by showing the complete chain of events that led to the error, rather than just the top-level symptom.

- id: php-error-04
  answer: |
    `set_error_handler` converts traditional PHP errors (warnings, notices, deprecations) into `ErrorException` objects, making them catchable with `try`/`catch`. `try`/`catch` only catches `Throwable` instances (exceptions and errors), not traditional PHP errors. To make warnings catchable: `set_error_handler(function($severity, $message, $file, $line) { throw new ErrorException($message, 0, $severity, $file, $line); });` — this transforms any PHP error into an exception that can be caught.

- id: php-arrays-01
  answer: |
    A "list" is an array with sequential integer keys starting from 0 (e.g., `[1, 2, 3]` or `['a', 'b', 'c']`). An associative array has string keys or non-sequential integer keys (e.g., `['name' => 'John', 'age' => 30]`). Under the hood, a PHP array is an ordered hash map (a hash table that maintains insertion order) — it can function as a list, a dictionary, a stack, a queue, or any other ordered collection, all with the same `array` type.

- id: php-arrays-02
  answer: |
    The spread operator `...` unpacks an array into individual elements. In function calls, it spreads array elements as separate arguments (string keys are not supported in function calls). In array literals (PHP 8.1+), it merges arrays and supports string keys: `['a' => 1, ...['b' => 2]]` produces `['a' => 1, 'b' => 2]`. Keyed destructuring with `list()` or `[]` allows extracting specific keys: `['name' => $name, 'age' => $age] = $user;` assigns `$user['name']` to `$name` and `$user['age']` to `$age`.

- id: php-arrays-03
  answer: |
    PHP arrays use copy-on-write semantics — assigning an array to a new variable or passing it to a function does not immediately copy the data. Instead, both variables reference the same underlying array until one is modified, at which point a copy is made (copy-on-write). Using `&` (reference) creates a true reference where both variables point to the same data — modifications through one variable are immediately visible through the other, with no copy-on-write behavior.

- id: php-arrays-04
  answer: |
    PHP coerces array keys as follows: the string `"1"` becomes integer `1`; the float `1.9` becomes integer `1` (truncated toward zero); the boolean `true` becomes integer `1`; `null` becomes the empty string `""`. Only integers and strings are valid array keys — floats, booleans, and null are silently coerced to integers or strings.

- id: php-null-01
  answer: |
    `??` (null coalescing) checks if the left operand is null (using `isset` semantics — null or undefined) and returns the right operand if so. `?:` (short ternary / "elvis") checks if the left operand is truthy and returns the right operand if falsy. The key difference: `??` only treats `null` (and undefined) as "missing", while `?:` treats any falsy value (`0`, `""`, `false`, `null`, `[]`, `"0"`) as "missing". For example, `0 ?? 'default'` returns `0`, but `0 ?: 'default'` returns `'default'`.

- id: php-null-02
  answer: |
    The nullsafe operator `?->` (PHP 8.0) short-circuits the entire chain if any part evaluates to null. For `$a?->b()->c`, if `$a` is null, the entire expression immediately returns `null` without evaluating `b()` or accessing `c`. If `$a` is not null but `$a->b()` returns null, then accessing `c` would also short-circuit to null. This avoids nested null checks and prevents "call on a member of null" errors.

- id: php-null-03
  answer: |
    The `??=` operator assigns the right operand to the left operand only if the left operand is null (or undefined). It differs from `$x = $x ?? $y` in that it is more concise and uses short-circuit evaluation — `$y` is only evaluated if `$x` is null. Functionally, `$x ??= $y` is equivalent to `$x = $x ?? $y` but avoids unnecessary evaluation of the right-hand side.

- id: php-null-04
  answer: |
    `isset($x)` returns `true` if `$x` exists and is not `null`. `empty($x)` returns `true` if `$x` does not exist or is falsy (`0`, `""`, `false`, `null`, `[]`, `"0"`). `$x === null` returns `true` only if `$x` is exactly `null`. A value where `isset` and `empty` disagree: `$x = "0"` — `isset` returns `true` (it exists and is not null), but `empty` returns `true` (because `"0"` is falsy). Another: `$x = "1"` — `isset` returns `true`, `empty` returns `false`.

- id: php-match-01
  answer: |
    `match` uses strict comparison (`===`), while `switch` uses loose comparison (`==`). `match` does not fall through — each arm is independent and no `break` is needed. `match` is an expression that returns a value; `switch` is a statement. `match` throws an `UnhandledMatchError` if no arm matches and no `default` is provided; `switch` silently does nothing if no case matches.

- id: php-match-02
  answer: |
    Because `match` uses strict comparison, `match(0) { 0 => 'a', "0" => 'b' }` will match the first arm (`0 === 0` is true), whereas `switch (0) { case "0": }` would match because `"0" == 0` is true in loose comparison. This can be surprising if you expect the string `"0"` to match integer `0` — in `match` it will not, because `"0" === 0` is `false`.

- id: php-match-03
  answer: |
    `switch` is still the better fit when you need fallthrough behavior (multiple cases executing the same code block without `break`), when you intentionally want loose comparison semantics, when working with legacy code that already uses `switch`, or when you need to execute the same block for multiple values without repeating the body.

- id: php-match-04
  answer: |
    `match(true) { condition1 => value1, condition2 => value2, default => value3 }` works because each condition is evaluated in order, and the first condition that strictly equals `true` is matched. It works because `match` compares the subject (`true`) against each arm's condition using `===`. This replaces an if/elseif chain by making the subject always `true` and putting the actual conditions in the arms, providing a concise, expression-based alternative with strict comparison and no fallthrough.
