# php knowledge benchmark — answers
# Model: opencode-go/muse-spark-1.2   (provider-stripped id: muse-spark-1.2)
# CLOSED-BOOK via ocode run -dir <empty> (no corpus access). corpus_rev 1.

```yaml
- id: php-types-01
  answer: |
    `declare(strict_types=1);` enables strict scalar type checking for the file it appears in.
    Where: It must be the very first statement in the file, immediately after `<?php` (and before any other code except other declare directives). It is a per-file, per-call-site directive - it affects calls *made from* that file, not the file where the function is defined. No block form; only `declare(strict_types=1);` at top.
    Effect vs default (coercive) mode: By default PHP is coercive/weak: if a function is typed `function f(int $x)` and you call `f("3")` or `f(3.2)` PHP will juggle/coerce to int if possible. With `strict_types=1` no coercion is done - the argument type must match the declared type exactly or a `TypeError` is thrown. Same applies to return types (strict check on return value) and to internal functions when called from a strict file. Without it, only class/interface type hints are strict; scalar types are coerced.

- id: php-types-02
  answer: |
    `==` is loose equality with type juggling: PHP converts operands to a common type before comparing.
    `===` is strict/identical equality: no juggling, must be same type *and* same value.
    Surprise examples (PHP 8 still has many, though `0 == "foo"` was fixed in PHP 8.0):
    - `"123" == 123` is true, `"123" === 123` is false.
    - `"0e12345" == "0e54321"` is true (both parsed as float 0 in scientific notation).
    - `0 == false`, `"" == false`, `"0" == false` are all true with `==`, false with `===`.
    - `in_array(0, ["a","b","c"])` returns true with loose check because `"a" == 0` after juggling. Must use `in_array(0, [...], true)` for strict.

- id: php-types-03
  answer: |
    Added for function/method parameters, returns, and properties:
    - Union `T1|T2` (PHP 8.0): value may be *either* type. e.g. `int|string`, `Foo|Bar`. `T|null` is a union including null.
    - Intersection `T1&T2` (PHP 8.1): value must satisfy *all* types at once. Only useful for class/interface types: e.g. `Countable&Iterator` means object implements both.
    - Nullable `?T` (PHP 7.1): sugar for allowing null. `?T` is exactly equivalent to `T|null`. e.g. `?string` === `string|null`. As of PHP 8.0 you can use `T|null` directly, and in PHP 8.2+ `?T` is just short form. `?T` cannot be combined standalone like `?T|int` - use `T|int|null` instead. Nullable also implicitly allows default value `null` if no default given.

- id: php-types-04
  answer: |
    - `void` (PHP 7.1): function returns no meaningful value. You must not `return $value;` - only `return;` or fall off end. `return null;` is error. Correct for procedures/side-effect functions.
    - `never` (PHP 8.1): function *never returns* at all - it always `throw`s, `exit`/`die()`, or infinite loop. Static analysers know code after call is unreachable. Use for `function abort(string $msg): never { throw new ... }`. Void *does* return (to caller), never does not.
    - `mixed` (PHP 8.0): accepts *any* value including null, objects, etc. Equivalent to `object|resource|array|string|int|float|bool|null`. Correct when you truly handle any type and cannot be more specific. Standalone only. In doc terms: void = nothing, never = no return, mixed = anything.

- id: php-types-05
  answer: |
    Typed class constants: `public const string FOO = "bar";`, `private const int MAX = 10;`
    Added in PHP 8.3. Previously constants could not have type declarations.
    Can be typed with any type usable for properties (including union/intersection, e.g. `const int|string X = 1;`).
    Override rule: if parent/interface defines a typed constant, a child class overriding it must declare a compatible type - effectively invariant/same type. You cannot remove the type or change it to an incompatible type; the value must satisfy the declared type. Visibility may not be narrowed. `final` constants cannot be overridden.

- id: php-enums-01
  answer: |
    Pure enum (PHP 8.1): `enum Suit { case Hearts; case Diamonds; case Clubs; case Spades; }` - cases are names only, no scalar equivalent.
    Backed enum: `enum Suit: string { case Hearts = 'H'; case Diamonds = 'D'; }` or `: int`. Each case has a backing `value` (string|int) accessible as `Suit::Hearts->value`.
    Need backed when you need to persist/serialize/interop with scalars - DB, JSON, API, config - and want `from()`/`tryFrom()` to hydrate from scalar. Pure enums have no value and cannot be created from scalar.

- id: php-enums-02
  answer: |
    - `Suit::cases(): array` - static method returning list of all cases in declaration order, e.g. `[Suit::Hearts, Suit::Diamonds, ...]`.
    - `Suit::from(int|string $value): static` - backed enums only. Returns the case whose backing value === $value. Throws `ValueError` if no match.
    - `Suit::tryFrom(int|string $value): ?static` - backed enums only. Same lookup but returns `null` instead of throwing on invalid value. Use when input is untrusted.

- id: php-enums-03
  answer: |
    Yes: enums can have methods (instance and static), class constants, and can `implement` interfaces (and use traits). Example: `enum Suit implements JsonSerializable { const FOO=1; public function label(): string {...} }`
    Restrictions vs normal class:
    - Cannot have instance properties / state (no property hooks/instance vars; cases are singletons) - and no `__construct` with args for cases.
    - Cannot be instantiated with `new`.
    - Cannot extend a class nor be extended; cannot be `extends`, but can implement interfaces.
    - Cannot be cloned, `__clone` forbidden.
    - Cases are final singletons; comparison is by identity.
    - No inheritance hierarchy among enums.

- id: php-enums-04
  answer: |
    Yes, same instance. Each enum case is a singleton object created once. `$a = Suit::Hearts; $b = Suit::Hearts;` then `$a === $b` is true (identical). `==` also true. No duplication.
    Effect: you can compare with `===` (preferred) or `==` safely, and `match` works because `match` uses strict `===` identity check. `match($suit) { Suit::Hearts => ... }` matches exactly that singleton. Also usable as array keys and in `===` checks without worrying about distinct objects.

- id: php-oop-01
  answer: |
    Constructor property promotion (PHP 8.0) lets you declare and initialize properties directly in the constructor parameter list by adding a visibility (or `readonly`) modifier.
    `public function __construct(private string $name, public int $age = 0) {}` automatically creates `private string $name` and `public int $age` properties and assigns the arguments to them. Replaces boilerplate:
    ```php
    private string $name;
    public function __construct(string $name) { $this->name = $name; }
    ```
    Promoted params can have default values, types, attributes, and are regular properties thereafter.

- id: php-oop-02
  answer: |
    `readonly` properties (PHP 8.1): `public readonly string $name;` may be written *once* and then become immutable. Must be typed. Can only be initialized where declared or inside the declaring class's `__construct` (once). After initialization any write throws `Error`. Only within declaring class, not subclass outside.
    `readonly` class (PHP 8.2): `readonly class Foo { public string $a; public int $b; }` marks *every* declared instance property as readonly, and prevents dynamic properties. Equivalent to marking each property `readonly`. Also forbids mutable properties in that class. You still must initialize each in constructor.

- id: php-oop-03
  answer: |
    - `$this` is the current *object instance* (available only in non-static methods). `$this->method()` is instance dispatch.
    - `self` refers to the *class where the code is written* (compile-time binding). `self::foo()` always calls the parent/base definition's version even if called via subclass.
    - `static` refers to the *called class at runtime* (late static binding, LSB). `static::foo()` dispatches to the most derived class that made the call.
    LSB (introduced PHP 5.3) solves the inheritance problem where a parent static method/property access using `self::` cannot be overridden polymorphically. Using `static::`, `static::$prop`, `new static()`, `get_called_class()` respects the actual subclass, enabling factory methods and overridden static members.

- id: php-oop-04
  answer: |
    Both introduced in PHP 8.4.
    - Asymmetric visibility: separate visibility for read vs write, e.g. `public private(set) string $name;` or `public protected(set) int $count;` - you can expose a property as publicly readable but only privately/protectedly writable.
    - Property hooks: define `get`/`set` logic directly on the property without a backing method: `public string $fullName { get => $this->first . ' ' . $this->last; set(string $v) => ... }`
    They let you avoid boilerplate getter/setter methods (`getName()`/`setName()`), explicit backing fields, and proxy objects for computed/validated properties, while keeping direct property syntax `$obj->prop`.

- id: php-oop-05
  answer: |
    `#[\Override]` (PHP 8.3) is an attribute you place on a method to declare intent to override a parent class or interface method.
    `#[Override] public function foo(): void {}`
    At compile/lint time the engine checks that a parent/interface actually defines a method with that name/signature. If not, it emits an error/warning.
    Catches bugs like: misspelled method name, wrong signature after parent rename, forgetting to update child after interface change, thinking you override when you actually create a new method.

- id: php-closures-01
  answer: |
    Closure `function () use ($x) {}` requires explicit `use` list to import outer variables. By default by-value copy at definition time. No automatic capture.
    Arrow function `fn() => expr` captures *all* variables used from parent scope automatically and lexically by value (copy) with no `use` clause. It also auto-captures `$this`. Arrow body is single expression, not statement block. Arrow capture is by value only (cannot capture by reference), and sees the value at definition? Actually by-value copy is taken when arrow is defined? In PHP arrow captures by value semantics at call time lexically, but effectively value-bound. Closure is more verbose but can capture by reference `use (&$x)`.

- id: php-closures-02
  answer: |
    First-class callable syntax `...` (PHP 8.1): `strlen(...)` or `$obj->method(...)` or `Class::method(...)` creates a `Closure` object representing that callable *without* calling it.
    It produces a `Closure` (which is `callable`) that can be stored, passed to `array_map`, invoked later: `$fn = strlen(...); $fn("hello") === 5;` Equivalent to `Closure::fromCallable('strlen')` or `fn(...$args) => strlen(...$args)`, but handles scope, static, and arity correctly and is more concise.

- id: php-closures-03
  answer: |
    `Closure::bind(Closure $c, ?object $newThis, string|object|null $scope)` (static) and `$closure->bindTo(?object $newThis, string|object|null $scope)` rebind a closure's `$this` and its class scope (visibility context).
    After binding, inside the closure `$this` refers to `$newThis` (or null for static closure) and private/protected members are resolved as if the closure were a method of `$scope` class. Allows a closure defined outside a class to access private properties/methods of an object, used for encapsulation hacks, proxy/hydrator patterns.

- id: php-closures-04
  answer: |
    `use ($x)` captures by value: closure gets a copy of `$x`'s value at the moment the closure is *defined*. Later changes to outer `$x` are not seen inside, and changes inside do not affect outer.
    `use (&$x)` captures by reference: closure holds an alias to the outer variable. It sees the current value at *call time*, and mutations inside/outside are shared.
    Example: `$x=1; $f=function() use ($x){return $x;}; $x=2; $f()` returns 1. With `use (&$x)` it returns 2.

- id: php-error-01
  answer: |
    Hierarchy: `Throwable` is root interface. Two branches: `Exception` and `Error`. Both implement `Throwable`. `Exception` subtree: `LogicException`, `RuntimeException`, etc. (user-recoverable). `Error` subtree: `TypeError`, `ValueError`, `ParseError`, `ArithmeticError`, `AssertionError`, etc. (engine/internal failures, many formerly fatal).
    Difference: `Exception` is for application-level recoverable conditions you throw; `Error` indicates programming or engine mistakes typically not to be recovered.
    To catch both: `catch (Throwable $e)`. `catch (Exception $e)` catches only Exceptions, not Errors. `catch (Error $e)` only Errors.

- id: php-error-02
  answer: |
    `try { ... } catch (E $e) { ... } finally { ... }`
    `finally` *always* runs after try/catch completes, whether exception was thrown, caught, not caught, or a return/break/continue was executed. Used for cleanup.
    If both try/catch and finally have `return` (or throw), the finally's return/throw *overrides* the earlier one - the first return value is discarded and the finally's value is returned. Same for exception: exception from try is suppressed if finally returns or throws.

- id: php-error-03
  answer: |
    ```php
    class MyException extends Exception {}
    try {
      // low-level
    } catch (IOException $e) {
      throw new MyException("Failed to save", 0, $e);
    }
    ```
    Constructor signature `__construct(string $msg, int $code=0, ?Throwable $previous=null)`. Pass previous as third argument. Retrieve via `$e->getPrevious()`.
    Why chain: preserves original stack trace and cause (root cause analysis), avoids losing diagnostic info when wrapping lower-level failures in domain-level exceptions, allows unwrapping/logging full chain.

- id: php-error-04
  answer: |
    `try`/`catch` catches `Throwable` objects thrown via `throw`. `set_error_handler(callable)` handles PHP's error system (warnings, notices, user errors from `trigger_error`, `fopen` warnings) which are *not* exceptions and do not enter catch blocks.
    To make warnings catchable: install an error handler that converts errors to `ErrorException`:
    ```php
    set_error_handler(fn($sev,$msg,$file,$line) => throw new ErrorException($msg,0,$sev,$file,$line));
    // or temporarily:
    // or use try with custom handler and restore
    ```
    Then `try { fopen(...); } catch (ErrorException $e) {...}` works. Remember to `restore_error_handler()`. Since PHP 8 many internal functions throw `ValueError`/`TypeError` instead of warnings, but file/warning cases still need conversion.

- id: php-arrays-01
  answer: |
    PHP has only one `array` type which is an ordered hash map (dictionary + linked list for insertion order). Conceptually:
    - "List" (vector) convention: keys are contiguous ints `0..n-1` (e.g. `[1,2,3]`). Functions like `array_is_list()` (PHP 8.1) test this. Treated as JSON array.
    - "Associative array" (map/dict) convention: keys are strings or non-contiguous ints (e `["name"=>"Bob", 5=>...]`). Treated as JSON object.
    But structurally both are same hash table with order preservation; distinction is by key pattern, not separate type.

- id: php-arrays-02
  answer: |
    Spread `...` for unpacking arrays: `$c = [...$a, ...$b];` or `func(...$arr)` . For arrays, pre-8.1 spread only allowed integer keys (string keys threw error) and reindexed ints. Since PHP 8.1 string keys are supported: `[...$a,...$b]` preserves string keys, later values overwrite earlier duplicate keys; integer keys are still reindexed sequentially? In array unpacking ints are reindexed.
    Keyed destructuring: `[$a,$b] = $arr;` positional; `["key" => $v, 1 => $x] = $arr;` or `list($a,$b)=$arr;` Since PHP 7.1 you can destructure by keys: `["name" => $name, "age"=>$age] = $user;` Also `[...]` short syntax supports that style.

- id: php-arrays-03
  answer: |
    Arrays are copy-on-write values, not references. `$b = $a;` semantically copies the array but internally shares the same zval/buffer until one side is mutated, then a real copy is made (CoW). Passing `function f(array $x)` also copies-on-write - mutating `$x` inside does not affect caller unless `&` reference. `$b =& $a;` creates an alias: both variables point to same array; mutating either mutates the shared one, and CoW is disabled.

- id: php-arrays-04
  answer: |
    Array keys are coerced to `int` or `string` only (no float/bool/null distinct):
    - `"1"` (numeric string that is canonical int) => `int 1`
    - `1.9` (float) => truncated to `int 1`
    - `true` => `int 1`, `false` => `int 0`
    - `null` => `string ""` (empty string)
    Also `"1.5"` (non-int numeric string) stays string `"1.5"`? But `"1"` and `1` collide, as do `1`, `1.9`, `true`. Example: `["1"=> "a", 1=>"b", 1.9=>"c", true=>"d"]` all same key `1`.

- id: php-null-01
  answer: |
    `??` null coalesce: `$a ?? $b` returns `$a` if `$a` exists and is not null, otherwise `$b`. Only checks definedness/null; falsy `0`, `""`, `false` are returned as-is.
    `?:` elvis/short ternary: `$a ?: $b` returns `$a` if `$a` is *truthy*, otherwise `$b`. Empty values `0`, `""`, `"0"`, `false`, `[]`, `null` all trigger `$b`.
    So `0 ?? 5` => `0`, but `0 ?: 5` => `5`.

- id: php-null-02
  answer: |
    Nullsafe `?->` (PHP 8.0): `$a?->b()->c` safely navigates nullable chain. If `$a` is null, evaluation short-circuits to null instead of error.
    Short-circuiting means if any `?->` segment evaluates to null, the rest of the chain is *not evaluated/called* and the whole expression yields null. E.g. `$a?->b()->c` if `$a` is null, `b()` is never called and `c` never accessed. If `b()` returns null, `->c` is still skipped when chain is `$a?->b()?->c`? For `$a?->b()->c`, if `$a` non-null but `b()` returns null, then `->c` on null would error - you need `?->` at each nullable step.

- id: php-null-03
  answer: |
    `$x ??= $y` (PHP 7.4) assigns `$y` to `$x` only if `$x` is null or not defined, otherwise leaves `$x` unchanged. Returns the resulting value.
    Difference from `$x = $x ?? $y`: functionally similar but `??=` evaluates `$x` once and does not emit undefined-variable notice if `$x` not set, and avoids double fetch (important if `$x` is property/array access with side effects or expensive). `$x = $x ?? $y` evaluates `$x` twice and will notice if uninitialized in strict contexts? ?? suppresses notice but still semantic duplicate.

- id: php-null-04
  answer: |
    - `isset($x)` true iff variable exists and value !== null.
    - `empty($x)` true iff variable does not exist *or* value is falsy (`null`, `false`, `0`, `0.0`, `"0"`, `""`, `[]`). Essentially `!isset($x) || $x == false`.
    - `$x === null` true only if value is exactly null (variable must exist, error if undefined without ?? check).
    Divergence: ` $x = 0;` or `$x = ""` or `$x = false`: `isset($x)` is true, `empty($x)` is true, `$x===null` is false. Or canonical: `$x = 0` => `isset` true (not null) but `empty` true (considered empty). Another: `$x = 0` => `isset` true vs `empty` false? Actually for `empty` true. Better sharp: `$x = false` => `isset` true, `empty` true, `===null` false.

- id: php-match-01
  answer: |
    `match` (PHP 8.0) vs `switch`:
    - Comparison: `match` uses strict `===`, `switch` uses loose `==` with juggling.
    - Fallthrough: `match` has no fallthrough; each arm `cond => expr`. `switch` falls through unless `break`/`return`.
    - Expression vs statement: `match` is an expression returning a value (must be used/assigned); `switch` is a statement.
    - Exhaustiveness: `match` throws `UnhandledMatchError` if no arm matches and no `default`, `switch` silently does nothing.
    - Syntax: `match` arms can be comma-separated values `1,2 => ...` and require single expression.

- id: php-match-02
  answer: |
    Because `match` is strict, cases that `switch` would match loosely no longer match.
    E.g. `match(0) { "0" => ..., false => ..., "" => ..., null => ... }` matches *none* (throws) because `0 === "0"` false, `0 === false` false. In `switch(0)`, `case "0":`, `case false:`, `case "":` all match 0 due to loose comparison.
    Another surprise: `match(0)` will not match `"foo"`? Actually `switch` `0 == "foo"` was true before PHP 8, so `switch` had bugs; `match` fixes by strictness. So code migrating from switch expecting `0` to match `false` breaks intentionally.

- id: php-match-03
  answer: |
    Use `switch` when you need: multiple statements per case (with side effects), fallthrough intentional (`case A: case B:` can be done in match but fallthrough logic with shared block and break is more natural in switch), early `break`/`continue` control flow, or when you want statement form without return value, or when you intentionally want loose comparison. Also when you need non-expression cases (e.g., mixing imperative code). Match cannot contain statement blocks, only expressions.

- id: php-match-04
  answer: |
    `match(true)` acts as if/elseif chain: each arm is a boolean condition expression compared strictly to `true`. First true condition wins.
    ```php
    $r = match(true) {
      $age < 13 => "child",
      $age < 20 => "teen",
      default => "adult",
    };
    ```
    Works because `match` evaluates each arm's left side, then `=== true`. Conditions evaluate to bool, so ordering matters like `if/elseif`. Unlike `switch(true)`, strictness avoids juggling surprises, and it is an expression.
```
