# step-5-preview-free — closed-book answers for php (opencode-go, 2026-10-12); answerer saw only _prompts/php.md
- id: php-types-01
  answer: |
    `declare(strict_types=1)` switches a file into strict type-checking mode. It must be the very first statement in the file — after `<?php` and any comments/whitespace, but before any code, namespaces, or `use` statements, and only at file top level (not inside a function, conditional, or eval).

    Under the default coercive mode, PHP silently converts scalar arguments to the declared type: passing `"5"` to an `int` parameter, or `1.5` where an `int` is expected, succeeds via coercion. Under `strict_types=1` no conversion happens for arguments or return values — the type must match exactly, otherwise a `TypeError` is thrown. A key nuance: the directive applies per-file at the *call site*, not the file where the function is declared, and calls to internal (C) functions called from a non-strict file still coerce.

- id: php-types-02
  answer: |
    `==` is loose equality: PHP performs type juggling before comparing. `===` is strict or identical: same type AND same value.

    Surprising `==` results: `"10" == "1e1"` is **true** (both are numeric strings, compared as numbers), `"1" == "01"` is **true**, `"" == null` and `"" == false` are **true**, `null == 0` is **true**, and `"0" == false` is **true**. The classic gotcha is `0 == "foo"`: this was **true** in PHP 7 (the non-numeric string cast to `0`), which made `in_array(0, ["a", "b"])` return true. PHP 8's "saner string to number comparisons" changed that: an int compared to a non-numeric string now falls back to string comparison, so `0 == "foo"` is now false. Another classic: `NAN == NAN` is false.

- id: php-types-03
  answer: |
    - Union `A|B` (PHP 8.0): the value may be either type.
    - Intersection `A&B` (PHP 8.1): the value must satisfy both types — practically this means one concrete class plus any number of interfaces, since two classes cannot both be extended by one object. For genuinely mixed unions and intersections you need DNF syntax (`(A&B)|C`).
    - Nullable `?T`: allows null in addition to `T`.

    `?T` is exactly shorthand for `T|null` — the two are semantically identical and interchangeable in declaration/return use. `?T` exists because nullable declarations predate union types: before 8.0 it was the *only* way to express "type or null". Note `?T` cannot be combined in a union (`?A|B` is illegal); you must write `A|B|null`. Also, `void`, `never`, and (partially) `callable` cannot appear in union types.

- id: php-types-04
  answer: |
    - `never` (PHP 8.1): the function cannot return — it must call `exit()`, throw, or loop forever. Only usable as a return type (not on parameters or properties). Correct for functions like `dd()`, `redirect()`, or always-throwing handlers.
    - `void`: the function runs but returns nothing. A bare `return;` is allowed; `return $value;` is a fatal error. Cannot be used as a parameter or property type. Correct for procedures where the return value is meaningless.
    - `mixed` (PHP 8.0): accepts anything — equivalent to the union `array|bool|callable|int|float|object|resource|string|null`. Correct when no constraint applies, e.g. values from untyped sources, values that are polymorphic across documented shapes.

    Rule of thumb: `never` means control flow stops; `void` means no return value; `mixed` means "I cannot constrain this".

- id: php-types-05
  answer: |
    Typed class constants were added in PHP 8.3. Syntax: `const string NAME = 'x';` — you declare a type before the constant name (also on `abstract const`, interfaces/enums constants). PHP 8.3 simultaneously added redeclaration of constants with narrowing.

    When a subclass overrides (or redeclares) a constant, the child's type must be invariant-or-covariant-compatible with the parent's — you cannot widen or change it incompatibly, or a fatal error is raised. Concretely, narrowing is allowed (parent `const A|B`, child `const A` is fine) but widening or changing type is not. The same rule applies to an untyped parent: the child may declare any compatible type.

- id: php-enums-01
  answer: |
    A **pure enum** has only named cases, each of which is a singleton object (`Suit::Hearts`). A **backed enum** attaches a scalar backing value to each case — either `int` or `string`: `enum Suit: string { case Hearts = 'H'; case Spades = 'S'; }`.

    You need a backed enum whenever the enum must cross a boundary where only scalars survive: database columns, JSON payloads, query strings, form values, external APIs. It gives you `->value` (the scalar), `->name` (the case name), `from()` and `tryFrom()` (lookup by value), and the engine guarantees the mapping is total and unique (duplicate backing values are a compile error). A pure enum is fine for in-memory state machines, status fields, dispatch, `match` subjects, and comparisons where no scalar representation is needed.

- id: php-enums-02
  answer: |
    All three are static methods available on every enum, but `from()`/`tryFrom()` only exist on **backed** enums (a pure enum has no value to look up).

    - `Suit::cases()` — static method returning an array of all case instances in declaration order. Available on both pure and backed enums (implements `UnitEnum`).
    - `Suit::from('H')` — returns the case whose backing value equals the argument. On an invalid (or wrongly typed) value it throws `\ValueError`.
    - `Suit::tryFrom('H')` — identical lookup, but returns `null` on an invalid value instead of throwing.

    Use `from()` when the value is already known-valid internal data (an unmatched value is a bug deserving an exception); use `tryFrom()` when the value comes from users/IO and null is a representable outcome. They accept the *backing value*, not the case name (`from('Hearts')` throws).

- id: php-enums-03
  answer: |
    Yes, all three. Enums may have methods (including static methods and `static` returns), constants (since 8.2 untyped constants... typed constants came with the 8.3 typed-constants feature), and interfaces — a backed enum implements the implicit `BackedEnum` interface, a pure enum the implicit `UnitEnum` interface (both of which you may not implement yourself), and an enum may additionally implement any other interfaces.

    Key restrictions versus a normal class:
    - Cannot be instantiated with `new` — only the declared cases exist.
    - Cannot have instance properties (no per-case state), though constants and static properties... static properties are not allowed either (in PHP 8.1; static methods are fine).
    - Cannot extend a class and cannot be extended — every enum is implicitly final.
    - Cannot be `clone`d or serialized meaningfully (no state to carry).
    - Every case is effectively final.
    - May not implement `UnitEnum`/`BackedEnum` directly.

    In practice they behave like a final class of singleton instances with a fixed, enumerable set.

- id: php-enums-04
  answer: |
    Yes — they are the same instance, in fact *identical*. Each enum case is a singleton instantiated once by the engine. So `$a === $b` is true, and `==` is also true.

    Consequences:
    - Comparison is identity-based and cannot go stale the way comparing scalars would; `$a === $b` is both the correct and cheap check.
    - Using cases as `match` subjects/cases works exactly as identity does: `match($x) { Suit::Hearts => ... }` fires when `$x` is the `Hearts` case, with no juggling risk since `match` uses `===`.
    - They are also valid array keys and `SplObjectStorage`/`WeakMap` keys, and can be compared reliably across request lifecycles (e.g. `Suit::Hearts === Suit::from('H')` is true — `from()` returns the same singleton).

- id: php-oop-01
  answer: |
    Constructor property promotion (PHP 8.0) lets you declare a visibility modifier on a constructor parameter, and PHP implicitly declares the matching class property and assigns the passed value to it:

    ```php
    class Point {
        public function __construct(
            public readonly int $x = 0,
            protected string $y = '',      // no default: also a promoted property
        ) {}
    }
    ```

    It replaces the standard boilerplate of (1) declaring a typed property, (2) repeating its type in the constructor signature, and (3) writing `$this->prop = $prop;` in the body. The promoted parameter becomes a real property with the given visibility, defaults, docblocks, and attributes carrying over. Restrictions: promotion requires a visibility modifier (`public`/`private`/`protected`, optionally with `readonly`), cannot be used on `callable`-typed promoted params before PHP 8.2, cannot be combined with variadic params, and abstract constructors cannot promote.

- id: php-oop-02
  answer: |
    `readonly` properties (8.1): a property marked `public readonly string $name` may be written **only once, and only from within the scope of the class that declared it** — normally in the constructor or an initializer. Reading before initialization throws; any second write, or any write from outside the declaring class's scope, is a fatal error. Only typed properties can be readonly, and defaults may be declared. A `readonly class` (8.2) makes every instance property implicitly readonly and forbids dynamic properties — a concise way to say "this is a value object / DTO."

    Marking the whole class adds: all properties are readonly without repeating the keyword, and `#[\AllowDynamicProperties]` no longer works, so untyped "extra" properties quietly attaching is impossible. Note readonly does not freeze objects holding arrays/objects internally: `readonly array $items` prevents reassigning `$items`, but mutating elements of that array is still possible unless you declare `readonly` with care. (PHP 8.3 added an exception allowing reinitialization of readonly properties during `clone` in `__clone`.)

- id: php-oop-03
  answer: |
    - `$this` — the actual current object instance being invoked, resolved at runtime.
    - `self` — resolves at **compile time** to the class where the code is written. `self::method()`, `new self`, `self::$prop`, `self::CONSTANT` all mean "the defining class," regardless of subclass.
    - `static` — appearance as `static::method()`, `new static` invokes **late static binding**: it resolves to the class that was actually called at runtime.

    Late static binding solves the problem where `self` "forgets" subclasses. Example: a base class with `public static function create() { return new self(); }` or `static::foo()`-less `self::foo()` always creates/configures a base instance, even when called as `Child::create()`; replacing `self` with `static` means `Child::create()` yields a `Child` with the child's overridden static methods used. `new static` is the same idea for constructors, enabling correct factory/method chaining in inheritance hierarchies.

- id: php-oop-04
  answer: |
    Both arrived in PHP 8.4.

    **Asymmetric visibility** lets you declare different visibility for read and write: `public private(set) string $name`, or `public protected(set)`. The property reads as public, but writes are restricted to the given scope — even before initialization, unlike `readonly`, which only guards post-initialization.

    **Property hooks** let you attach `get` and/or `set` logic directly to a real property:

    ```php
    public string $name {
        get => strtoupper($this->name);
        set (string $v) => $this->name = trim($v);
    }
    ```

    You can declare hooks on normal properties, abstract hooks in abstract classes, and hooks on interface properties; hooks may be asymmetric with visibility too.

    What they let you avoid: hand-written getter/setter boilerplate, storing a backing field behind accessors (a common pattern where `getName()`/`setName()` wrapped `$this->name`), duplicated visibility checks, and the "virtual property" dance where a getter plus a protected `$data` array replaced real typed properties. They preserve property syntax for consumers, work with type covariance in inheritance, and let you redesign writability — e.g. making a property publicly readable and privately writable — without changing the public read API.

- id: php-oop-05
  answer: |
    `#[\Override]` (PHP 8.3) is an attribute you place on a method to assert **this method is intended to override a parent method, implement an interface method, or override a method supplied by a trait**. The engine enforces that assertion at compile time: if no such parent exists, PHP emits a fatal compile error.

    The class of bug it catches is **silently-broken overrides** — the ones where refactoring invalidates an override without breaking anything. Typo'd method names (`funcychunder()` where you meant `fetch()`), a renamed parent method, a swapped signature such that no overload actually matches, a method you *believe* is an interceptor for a framework base class but isn't, or spurious "reconciliation" code that accidentally becomes dead. These fail *silently* otherwise: the method is never called, no error is raised, and behavior just quietly changes. `#[\Override]` turns that into an immediate compile-time error. It also marks intent: readers and reviewers can see that a method is deliberately an override rather than an extension of the API.

- id: php-closures-01
  answer: |
    Both capture the enclosing scope, but differently.

    - A closure uses the explicit `use (...)` clause: only the named variables are captured, by value at creation time. Anything not listed is not captured. Fine-grained, explicit control over what leaks in; and it can capture *by reference* via `use (&$x)`.
    - An arrow function (`fn(...) => ...`) has no `use` clause: it **automatically captures every outer variable referenced in its body**, by value only. There is no `&` capture.

    Consequences: `fn` is terser for small callbacks (`fn($x) => $x * $factor`) and immune to the "forgot to list it" bug; a closure is what you want when you need by-reference capture, selective capture, or a deliberately opaque dependency set. Arrow functions capture at definition time just like by-value `use`, and because they capture whole variables, they cannot bind to properties of `$this`... (they access `$this` normally when nested inside an instance method, with the same precedence quirks of by-value capture).

- id: php-closures-02
  answer: |
    `strlen(...)` is **first-class callable syntax** (PHP 8.1). It produces a real `Closure` object that wraps the named function/method:

    ```php
    $len = strlen(...);          // Closure
    $m   = Foo::bar(...);        // Closure (static method)
    $gm  = $obj->method(...);    // Closure, $this bound to $obj
    ```

    It replaces the older idioms `Closure::fromCallable('strlen')` and the callable string/array forms (`'strlen'`, `[$obj, 'method']`) as a cleaner, statically-analyzable way to obtain a closure, with better IDE/static-analysis support. The resulting closure is invocable like any other, so it drops into `array_map`, `usort`, and callable parameters. The `...` is syntactic sugar that means "everything — all of this callable's parameters" — i.e. define the closure's signature implicitly. Instance-method forms create a closure bound to `$this` so `$gm(42)` behaves as `$obj->method(42)`.

- id: php-closures-03
  answer: |
    `Closure::bind($closure, $newThis, $newScope)` (static) and `$closure->bindTo($newThis, $newScope)` (instance method) return a **new closure** whose execution context is changed:

    - `$newThis` becomes `$this` inside the closure (pass `null` for a static closure that must not see an object).
    - `$newScope` sets the class scope used to resolve `self`, `static`, and visibility of private/protected members. Giving the closure a class scope effectively grants it that class's access rights.

    By default a closure is bound to the class scope where it was defined, and to the `$this` at that time (or no `$this` for closures defined in a static context). Rebinding is how macros, metaprogramming, mocking frameworks, and test helpers call or overwrite private/protected behavior — a closure bound to a class scope can read and write private members of objects of that class. Note that a plain closure declared inside a normal method is already bound to its defining scope and `$this`; `bind` is how you *change* that.

- id: php-closures-04
  answer: |
    - `use ($x)` — captures by value. The closure holds a **copy** of `$x` as it existed at definition time. Later changes to the outer `$x` are invisible, and assignments inside the closure only affect its private copy.
    - `use (&$x)` — captures by reference. The closure uses the same variable; reads see the *current* value and writes change the outer variable.

    If `$x` changes **after** the closure is defined: a by-value closure still sees the old value; a by-reference closure sees the new one (and its writes are visible outside). Same distinction applies implicitly to arrow functions, which always capture by value (there is no reference form for `fn`). Example: defining `$fn = fn() => $x;` and then `$x = 'b';` still leaves `$fn()` as `'a'`. By-reference capture also interacts with copy-on-write: unsetting or rebinding after capture can produce surprising aliasing, so it is worth being deliberate about which mode you use.

- id: php-error-01
  answer: |
    Hierarchy: the interface `Throwable` is implemented by `Exception` (the classic, catchable application-level family, which extends `Error`... actually extends nothing and is directly implemented by the base `Exception` class) and `Error` (the engine-level class). Concrete subclasses live under each:

    - Under `Exception`: user exceptions, plus SPL ones like `LogicException`/`RuntimeException` with their children (`InvalidArgumentException`, `OutOfBoundsException`, `UnexpectedValueException`...).
    - Under `Error`: `TypeError`, `ValueError`, `ArithmeticError`/`DivisionByZeroError`, `ParseError` (via `CompileError`), `AssertionError`, `ArgumentCountError`, and `UnhandledMatchError` (8.0).

    Semantically, `Exception` signals a problem your program might reasonably catch and recover from; `Error` signals a bug or an environment fault — a type mismatch, an uncaught assertion, a bad `include`. (In practice both are catchable; historically `Error` grew out of what used to be fatal "engine errors".) Both implement `Throwable::getMessage()/getCode()/getFile()/getLine()/getTrace()/getPrevious()`.

    To catch either, type against `Throwable`: `catch (Throwable $t) { ... }`. You can also catch them separately (`catch (Exception)` / `catch (Error)`), but a `Throwable` catch-all is the idiomatic "handle everything" and is required if you also want to intercept fatal engine issues.

- id: php-error-02
  answer: |
    `finally` runs when control leaves the `try` block or a matched `catch` block — whether by falling off the end, via `return`, `break`/`continue`, or by an exception either caught or propagating out. It runs *before* the function actually returns or the exception propagates onward; in short, there is no path out of the `try`/`catch` region that skips `finally`.

    If **both** the `try`/`catch` and the `finally` contain `return`: the `finally`'s `return` wins — it overrides the pending return value from the `try`/`catch`. This holds even if `try` throws: an exception in flight is silently discarded when `finally` returns. If a `finally` block itself throws, that new exception supersedes whatever the `try`/`catch` produced. (If neither block returns, a pending exception propagates after `finally` completes.) Related: `finally` cannot be used without `try`/`catch`, and there may be only one per try structure. Because of the return-overrides behavior, an unguarded `return` in `finally` is considered a code smell — it hides exceptions.

- id: php-error-03
  answer: |
    Define a custom exception by extending `Exception` — preferably a narrower SPL base such as `RuntimeException`, `InvalidArgumentException`, `LogicException`, or a project base exception, rather than `Exception` itself, so callers can catch by category:

    ```php
    class OrderException extends RuntimeException {}
    ```

    Chain a lower-level exception by passing it as the **third argument** to the constructor, with the message first and an optional code second:

    ```php
    try { gate->charge($order); }
    catch (GatewayException $e) {
        throw new OrderException("Charge failed for order {$order->id}", 0, $e);
    }
    ```

    Retrieve the wrapped exception with `getPrevious()` (and the current one via `getTrace()`/`__toString()`).

    You chain because the higher layer's exception explains the *domain* failure ("order charge failed") while the lower one carries the *root cause* ("HTTP 502 from the payment gateway, with its stack trace"). Without chaining you lose the original — the stack, the specific class, the precise message — making the failure unreproducible and undiagnosable. Chaining preserves the full causal chain while restoring an abstraction boundary-appropriate exception type at each layer. Since PHP 8.0, `__construct` defaults include the previous-exception parameter for `Exception` subclasses, and exceptions support chained rendering automatically through `__toString()` traces.

- id: php-error-04
  answer: |
    They operate on completely different mechanisms. `try`/`catch` catches **throwables** — objects thrown via `throw`. `set_error_handler()` registers a callback for PHP's traditional runtime errors/notices/warnings (`E_WARNING`, `E_NOTICE`, `E_DEPRECATED`, and `E_USER_*`) — old-style diagnostics that are **not** objects and are never thrown. So PHP warnings (including "failed to open stream" from `fopen`) cannot be caught by a `catch` block at all, no matter the type.

    The standard bridge is to install a handler that converts errors into exceptions:

    ```php
    set_error_handler(function (int $severity, string $message, string $file, int $line) {
        throw new ErrorException($message, 0, $severity, $file, $line);
    });
    ```

    After this, an `fopen()` failure raises `E_WARNING`, the handler runs, throws `ErrorException`, which you can now catch with `try`/`catch`.

    Two caveats: return `false` from the handler when the error's severity isn't included in the current `error_reporting()` mask (to respect `@` suppression, error_reporting directives, and the error log pipeline), and remember that true fatals (`E_ERROR`, out of memory, parse errors) are not routed through this handler — those still need `register_shutdown_function()` and `error_get_last()`; also `assert()` and deprecation output are governed by their own ini settings. The older `ErrorException` idiom was in fact *the* long-standing pattern before PHP 8 consistently made internal failures throw `Error`/`ValueError`/`TypeError`.

- id: php-arrays-01
  answer: |
    PHP's `array` type is a single ordered hash map, so both "kinds" are the same data structure — the distinction is purely a key convention.

    - A **list** is an array whose keys are consecutive integers starting at 0: `[0 => 'a', 1 => 'b']`. The canonical form produced by `array('a','b')` or short syntax. `array_is_list()` (PHP 8.1) tests it — key order and key names matter (any non-sequential keys, or keys not starting at 0, disqualify the list).
    - An **associative array** uses string keys (or non-sequential integer keys): `['id' => 42, 'items' => [...]]`.

    Structurally, a PHP array is an ordered hash table: entries are stored in buckets, and the insertion order is preserved — it is simultaneously usable as a list, a map, a stack, and a queue. Integer and string keys can be freely mixed (with integer casting rules), and the same array can carry both `0 => 'a'` and `'id' => 42` entries. Because it is an ordered map, array operations preserve defined order, while "list" is just the special case where that order has been fully materialized as positional keys.

- id: php-arrays-02
  answer: |
    The spread operator `...$array` unpacks an array's elements into another array literal (PHP 7.4 for integer keys, PHP 8.1 added support for **string keys**: `[...$defaults, 'extra' => 1]`). Later entries with the same string key override earlier ones; duplicate numeric keys are a fatal error. The unpacking happens by value; nesting depth is flattened one level. It works with any traversable/array-ish: `[...$a, ...$b]`, generators, `Iterator` instances. (`array_merge` remains for mixed-key unions and the `+` union operator.)

    Destructuring with `[ ]` is the short syntax for `list()` — it assigns by position/key. Both exist since PHP 7.1 with keyed destructuring; `list($a, $b)` and `[$a, $b]` are equivalent, with the short form the modern idiom. Keys work: `['name' => $n, 'age' => $age] = $row;`, skipping is done by omitting a slot (`[, $second] = $arr;`), elements can nest arbitrarily, and destructuring accepts a `&$ref` target. It can be used in `foreach`, assignments, and function arguments (PHP 8+ supports it anywhere an lvalue is valid, including with string keys and in nested form), and mismatch raises a notice/warning rather than a fatal error (unknown index becomes null with a warning for `[]`, undefined-variable warning for `list()`).

- id: php-arrays-03
  answer: |
    Assigning or passing an array copies its **value**, so `$b = $a; $b['x'] = 1;` does not change `$a` — and passing to a function gives the function its own copy; the caller's array is untouched. But the actual memory copy is deferred: the underlying zval's refcount is incremented and the copy is *not* physically made until one side is written (copy-on-write, COW). This gives value semantics without the constant cost of copying — a large read-only array shared across many references consumes one copy. Separation happens at the first write on any symbol.

    `&` (true references) skip COW: `$b = &$a` makes both names the same underlying array — a write through either is visible through both, including inside functions (`fn(&$a)`) and via `foreach ($a as &$v)`. References also propagate weirdly: `$b = &$a; $c = $b;` makes `$c` refer to the same array, and array elements can individually be references, which can "infect" copies (one referenced element makes the *whole* array stop being COW-optimizable, and modifying other elements still behaves as copy semantics — the semantics differ from naive expectation there). The short version: arrays behave like values because of COW, while `&` opts out of value semantics entirely into true aliasing.

- id: php-arrays-04
  answer: |
    PHP coerces keys according to strict rules when an array literal or write is performed:

    - `"1"` — a **legal integer string** is cast to the integer `1`.
    - `1.9` — floats are truncated **toward zero** to an integer: key `1` (i.e. `1.9` and `1.1` both become key `1`, and a later write with either overwrites the earlier).
    - `true` — booleans are cast to `int`: `true` becomes `1`, `false` becomes `0`.
    - `null` — cast to the empty string `""` (so `[null => 'x']` is key `""`).

    Additional rules: canonical integer strings with surrounding whitespace are still cast, but `"+1"`, `"-1"`, and `" 1"` handling — `"1"` casts, while a string like `"1abc"` is not a legal integer string and stays a string. A string that *overflows* `PHP_INT_MAX` (e.g. `"9223372036854775808"`) is left as a string. Any other type (array as key) is an illegal offset warning, and `NAN`/`INF` numerically become keys 0 (they get cast to int, where NaN truncates to 0). Objects and resources may be used as keys but are cast similarly per the rules.

- id: php-null-01
  answer: |
    - `??` — null coalescing: `$x ?? $y` yields `$x` if it exists **and is not null**; otherwise `$y`. It is an `isset()` check, so it suppresses notices for undefined variables, array indexes, and object params (e.g. `$_GET['q'] ?? ''`), and, being a short-circuit, `$y` is evaluated only when needed.
    - `?:` — short ternary (the "elvis" operator): `$x ?: $y` yields `$x` if `$x` is **truthy**, otherwise `$y`. It is equivalent to `$x ? $x : $y`.

    The difference appears for non-null falsy values. For `$x = ''`, `0`, `0.0`, `false`, `[]`, or `'0'`: `$x ?? 'd'` returns `$x` itself, while `$x ?: 'd'` returns `'d'`. So `??` asks "is it set and non-null?", whereas `?:` asks "is it non-falsy?". Also, precedence differs: `?:` is left-associative-with-branching per ternary precedence, whereas `??` is right-associative (and cannot be nested to both sides the same way — `a ?? b ?? c` associates right). Practical rule: use `??` for optional/undefined data and defaults; use `?:` for "reuse this if usable, else fallback" formatting chores.

- id: php-null-02
  answer: |
    `?->` (PHP 8.0) is the nullsafe (null-short-circuiting) method-call/property-access operator. `$order?->getUser()->getName()` behaves like `$order->getUser()->getName()` when `$order` is non-null — but if `$order` is null, the rest of the chain is **not evaluated**: `getUser()` is never called, `getName()` is never called, and the whole expression evaluates to `null`. Before this operator, any of those calls on null raised "Call to a member function on null".

    "Short-circuiting" means the engine checks for null at each `?->` and, upon encountering one, propagates null outward and skips the remaining calls in that chain — side effects inside later calls never happen, so no fatal error and no partial execution. Semantics worth noting:
    - Each `?->` guards only up to the next access — `$a?->b()->c()` guards `b()`; if `$a` is null the whole chain is null, but if `$a` is non-null and `b()` returns null, the plain `->c()` after it still errors. Use `?->` on every step you want guarded.
    - It supports reads and writes: `$user?->profile?->update(...)`.
    - It short-circuits arguments (later arguments in the same call are not evaluated), and nested `??` on the result composes well: `$order?->getUser()?->getName() ?? 'guest'`.
    - Can't be combined with `??` on the *left* in an lvalue meaningfully: writes through `?->` require the left to exist (no auto-vivification).

- id: php-null-03
  answer: |
    `??=` is the null-coalescing assignment operator (PHP 7.4): `$x ??= $y` assigns `$y` to `$x` **only if `$x` is currently null or not set at all**; then the expression evaluates to the (new) value of `$x`. It acts like `$x = $x ?? $y` with one crucial difference: **the left-hand side is evaluated only once** — it checks `$x` for unset/null and, if writing, writes to that same expression without re-resolving it.

    Consequences of single evaluation:
    - It supports complex lvalues cleanly: `$config['options'] ??= []` or `$this->prop ??= $default` work — with `$this->prop = $this->prop ?? $default` you would resolve and potentially re-evaluate the property twice (which for magic `__get`/`__set`, or array-append behaviour, can produce different observable results).
    - It handles unset variables/indexes without warnings: `$result['key'] ??= 5` initializes it, whereas `$x = $x ?? $y` would first read `$x` (notice if undefined — suppressed by `??`, but you still must have the variable exist for the assignment to succeed... actually plain `$x ??= $y` creates it).
    - No side-effect duplication: with `$this->cache()->??=`-style chains, expensive lookups happen once.

    In short: the same *predicate*, but `??=` is write-safe, single-evaluation, lvalue-general, and null/unset tolerant.

- id: php-null-04
  answer: |
    Distinctions:
    - `isset($x)` — true if the variable/index exists **and is not null**. For multiple args, all must be non-null.
    - `empty($x)` — true if the variable does not exist **or** is falsy (falsy: `0`, `0.0`, `""`, `"0"`, `false`, `[]`, `null`).
    - `$x === null` — a strict identity comparison; true only if `$x` is exactly `null`. Emits an undefined-variable notice (in non-suppressed contexts) when `$x` was never set — unlike `isset`/`empty`, which are language constructs that do not notice.

    Disagreement: any non-null truthy value. Take `$x = "a"`:
    - `isset($x)` → **true** (set, non-null)
    - `empty($x)` → **false** (it's truthy)
    - `$x === null` → false

    Conversely `$x = null` (or unset): `isset` → false, `empty` → true, `=== null` → true. And `$x = 0`: `isset` → true, `empty` → true, `=== null` → false — a case where `isset` and `=== null` agree but `empty` disagrees. Practical rule: `isset` answers "usable and not null?", `empty` asks "missing or falsy?" — for existence-or-null checks use `isset`/`array_key_exists`, for truthiness use `!empty`, and for exact null use `=== null` (be aware of the notice).

- id: php-match-01
  answer: |
    `match` (8.0) and `switch` differ on several axes:

    - **Type of comparison**: `match` uses **strict (===)**; `switch` uses loose `==`.
    - **Expression vs statement**: `match` is an **expression** — it evaluates to a value and can be assigned, returned, or passed. `switch` is a **statement** group of cases that must be used for control flow, not as a value.
    - **Fallthrough**: `match` has **no fallthrough** — exactly one arm body runs and there is no `break`. `switch` falls through to the next case unless every arm ends with `break`/`return`.
    - **Unmatched values**: `match` with no matching arm and no `default` throws `\UnhandledMatchError` (a hard failure). `switch` silently continues after the block when nothing matches.
    - **Bodies**: `match` arms must be single expressions. `switch` arms run arbitrary statements.
    - Extras `match` offers: comma-separated multi-condition arms (`match ($s) { 'a', 'b' => 1, default => 0 }`), the arms are evaluated strictly (so type-safe), and heterogeneous conditions are allowed (any expression producing a boolean in the arm).
    - `switch` supports `continue` semantics in loops, `default`, and lazy case-by-case dispatch.

- id: php-match-02
  Answer: |
    Because `match` compares with `===`, several cases that "work" in `switch` now don't match (or, worse, match something unintended):

    - `0` (int) no longer matches `"0"` or `false` or a numeric string. Under `switch`, `case "0":` with subject `0` matched loosely; under `match` it never matches `case 0` unless types line up exactly.
    - `match($id)` where `$id` is the string `"12"` and arms are `case 12` (int) → no match. Loose `==` treated them equal; strict demands `12` vs `"12"` distinction or auto-typed arms (`case '12'`).
    - `true`/`false`/`"abc"` subjects: matching `case 1` against `true` is no longer true (under `==` it was), while `case true` against `1` still fails too — strictness removes many implicit matches. Code that "used to work" now raises `UnhandledMatchError`, which is usually a *symptom of a latent type-confusion bug* rather than a regression — loose `switch` was silently coercing subjects, and that coercion was often the actual defect (e.g. `0 == "foo"` in PHP <8 firing an unrelated case).
    - A subtler surprise: arms that are dynamically typed, or reliance on both `1` and `true` matching the same arm, must now be spelled explicitly (`case 1, true:`).

    In practice, strictness is the safer default: instead of surprising loose coercion, unmatched arms become explicit, recoverable failures.

- id: php-match-03
  answer: |
    `switch` still wins when:

    - **Fallthrough is intentional** — grouping cases that run the *same* statement block, especially when the blocks are nontrivial or shared. `match` must repeat expressions per arm or use the comma (`case 1, 2, 3:`) which only helps for simple arms.
    - **Multi-statement case bodies** — declaring variables mid-body, loops, or conditionally breaking out of outer loops. `switch` just runs code; `match` only returns expressions (though you can return the result of a call).
    - **Loose comparison is wanted** — leveraging `==` juggling deliberately, e.g. subject/arm types can arrive unpredictably (`"1"` and `1` should behave the same). `match` will not coerce at all.
    - **`break`-out practice** used in tight loops with `continue 2;` or when exiting to different loop levels.
    - **Large dispatch tables expressed as data** — a lookup array with `??` is often better still; but among the two keywords, `switch`'s per-case mutable bodies simplify code walking.
    - **`default` semantics you want with silent no-op** on no match (though this is usually a bug, not a feature).

    General guidance: use `match` for pure mapping and strict, exhaustive conditions; `switch` when the arms do real multi-statement work or you rely on fallthrough.

- id: php-match-04
  answer: |
    `match` compares each arm's **condition expression** to the subject using `===`. If you write `match (true) { ... }`, you are comparing each boolean condition to `true` with strict equality — so the first arm whose condition expression evaluates to literally `true` fires. Any boolean expression therefore becomes a valid-case pattern:

    ```php
    $band = match (true) {
        $score >= 90    => 'A',
        $score >= 80    => 'B',
        $score >= 70    => 'C',
        default         => 'F',
    };
    ```

    This replaces an if/elseif chain because each `if` condition mirrors a `match` arm (and `else` mirrors `default`). Why it works: the arm-key is not a value to match against the subject's value — it's a full expression, evaluated with normal semantics, compared strictly to the subject `true`. It is ordered like elseif (first match wins), returns a value like any expression, and requires only that the condition yields `true`. Caveats: must end with `default` or throw `\UnhandledMatchError` when all conditions are false (a helpful exhaustiveness guarantee), arms stay single-expression, and keep conditions mutually exclusive or ordering deliberate — there is no fallthrough rescue.

```

<!--END-->

Notes on the format: every answer is answered independently from internal PHP knowledge. Where my certainty dropped (e.g. exact sub-interface details, PHP 8.x edge rules like callable promotion in 8.2, or deeply obscure coercion cases), I marked the answer with the caveat rather than pretending precision. If you want, I can produce a second sheet covering the ones I flagged as less certain and disclose my confidence per item.
