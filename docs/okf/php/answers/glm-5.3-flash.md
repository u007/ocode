```yaml
- id: php-types-01
  answer: |
    declare(strict_types=1) enables strict type checking for scalar type declarations in that file. It must be the very first statement in the file (only whitespace, comments, and other declare() blocks may precede it). In the default coercive mode, scalar arguments are coerced where possible ("5" becomes int 5, 1.9 is truncated to 1 for an int parameter, numeric strings coerce). In strict mode, arguments must already match the declared scalar type or a TypeError is thrown; the only permitted widening is int → float. Important nuance: strictness applies at the call site — the file making the call decides, not the file declaring the parameter. Untyped parameters are unaffected and still coerce.

- id: php-types-02
  answer: |
    == compares values after type juggling (coercion); === requires identical type AND value. Surprising cases: before PHP 8, 0 == "foo" was true ("foo" cast to int 0); "1e3" == "1000" is true with == but false with ===; 1 == "1" is true but 1 === "1" is false; null == false is true. PHP 8 made string↔number comparisons saner ("foo" == 0 is now false), but == still coerces (e.g. "0" == 0 is true).

- id: php-types-03
  answer: |
    Union types (PHP 8.0), e.g. int|string, accept any of the listed types. Intersection types (PHP 8.1), e.g. Countable&Traversable, require the value to satisfy ALL listed class/interface types (only class-ish types; no scalars). Nullable types: ?T means "T or null", and for a single type T it is exactly equivalent to the union T|null. You cannot write ?int|string; you must write int|string|null. Without null in the type, passing null throws TypeError even in coercive mode.

- id: php-types-04
  answer: |
    void: the function returns nothing — it must not `return <value>` (a bare return is fine); callers get null. Correct for side-effecting functions. never (PHP 8.1): the function can never return control to the caller — it always throws, calls exit/die, or loops forever; it does not even implicitly return null. Correct for throwing helpers and exit points; it tells the type system the code after the call is unreachable. mixed (PHP 8.0): the value can be anything including null — equivalent to having no type declared. Correct for pass-through/untyped APIs. Order of "bottom-ness": never is a subtype of everything; mixed is the top type.

- id: php-types-05
  answer: |
    Typed class constants were added in PHP 8.3: `const int MAX = 10;`. When a subclass overrides a typed constant, the child's type must be compatible following covariance (same rule as return types): the child constant's type must be a subtype of (or identical to) the parent's type — narrowing is allowed, widening is a fatal error. This differs from properties, which must remain invariant.

- id: php-enums-01
  answer: |
    A pure enum has cases with no associated value (`enum Suit { case Hearts; }`). A backed enum declares a single scalar backing type — string or int (`enum Suit: string { case Hearts = 'H'; }`) — and every case must carry a value. You need a backed enum when you must map cases to/from scalar values, e.g. storing them in a database column, JSON, query parameters, or config, and then rehydrating with from()/tryFrom().

- id: php-enums-02
  answer: |
    cases() is a static method returning all cases in declaration order as an array. On backed enums, from($value) is a static method that returns the case whose backing value matches, or throws ValueError if no case matches; tryFrom($value) returns null instead of throwing. Both are static. from()/tryFrom() do not exist on pure enums (there is no backing value to search by).

- id: php-enums-03
  answer: |
    Yes — enums can have methods (also static methods), class constants, implement interfaces, and use traits; each case can even provide its own method implementations when the enum declares abstract methods. Restrictions versus a normal class: enums are implicitly final (no inheritance), cannot have constructors or destructors, cannot have instance properties, cannot be instantiated with new (cases are the only instances and are singletons), no dynamic properties, and cases cannot be created at runtime.

- id: php-enums-04
  answer: |
    Yes, they are the exact same instance — enum cases are singletons, so $a === $b is true and $a == $b is true; there is effectively identity-based equality, and they can be used as array keys and in WeakMaps. match() uses strict comparison (===), so `match($suit) { Suit::Hearts => ... }` matches correctly; comparing two different cases never matches.

- id: php-oop-01
  answer: |
    Constructor property promotion lets you declare, type, and assign a property directly in the constructor parameter list: `public function __construct(private int $x) {}` creates the private int property $x and assigns the argument to it. It replaces the boilerplate of separately declaring the property at class level and then writing `$this->x = $x;` in the constructor body. You can combine it with default values, readonly, and variadics (variadics aren't promoted).

- id: php-oop-02
  answer: |
    A readonly property (8.1) can be written exactly once — its initialization — and only from within the scope of the class that declares it (typically the constructor, or a default value); any later write or unset throws Error. Deserialization and (since 8.3) __clone are special-cased to allow (re)initialization. `readonly class` (8.2) marks every declared instance property readonly, forbids untyped properties, and forbids dynamic properties; such a class cannot be extended by a non-readonly class. It adds a whole-class immutability guarantee without annotating each property.

- id: php-oop-03
  answer: |
    self resolves to the class where the code is physically written (compile-time binding) — self::method() and new self use that class. static resolves to the actual runtime class of the object (late static binding) — static::method() and new static dispatch to the subclass. $this is the current object instance (method calls on $this also honor LSB for statics via static::). Late static binding solves calling back into overridden static methods/constructors from parent-class code: a parent helper can say new static or static::create() and get the child class instead of the parent.

- id: php-oop-04
  answer: |
    Both were introduced in PHP 8.4. Asymmetric visibility gives read and write different scopes: `public private(set) int $x` is publicly readable but only privately writable (also protected(set)). Property hooks let you define get/set hooks inline on a property: `public string $name { get => ucfirst($this->name); set => trim($value); }` — including "virtual" properties that have no backing field. They let you avoid writing boilerplate getter/setter methods and separate stored-vs-computed property plumbing; the class keeps a property-shaped API while intercepting access.

- id: php-oop-05
  answer: |
    #[\Override] is applied to a method to declare "this is meant to override a parent class method or interface method." PHP then verifies at load time that such a parent method actually exists; if it doesn't, an error is thrown. It catches the class of bug where a method name has a typo or a parent method is renamed/removed and the child override silently stops being called (becoming dead code) — turning a silent behavioral bug into a loud error.

- id: php-closures-01
  answer: |
    A traditional closure captures nothing automatically — you must list variables in `use (...)`; they are snapshotted by value at definition time (or by reference with &). An arrow function (fn) automatically captures, by value, the variables it uses from the enclosing scope at the moment of definition — no use clause, no reference capture. Arrow functions also have a single-expression body and inherit the scope without boilerplate; closures have full statement bodies and explicit (possibly by-reference) capture.

- id: php-closures-02
  answer: |
    `strlen(...)` is first-class callable syntax (PHP 8.1): the literal `...` (no argument passed) turns a reference to a function/method into a Closure object. It is equivalent to Closure::fromCallable('strlen') and produces an actual Closure instance that can be passed around, composed, and invoked later. It also works for instance/static methods: $obj->method(...), Foo::method(...).

- id: php-closures-03
  answer: |
    Closure::bind($closure, $newThis, $newScope) and bindTo() create a new closure with a different binding: $newThis sets what $this refers to inside the closure, and $newScope (a class name or object) sets the class scope, i.e. which private/protected properties and methods the closure may access. Inside the rebound closure, $this is the bound object and visibility is as if the code were written inside that class. bindTo() just returns the new closure; bind() additionally accepts a scope argument separately from the object (needed for static closures). Passing null for $this unbinds it.

- id: php-closures-04
  answer: |
    use ($x) copies the value of $x at the time the closure is created; if the outer $x changes later, the closure still sees the old snapshot. use (&$x) binds by reference: the closure sees the current value of $x whenever it runs, and writes inside the closure propagate back to the outer variable. So with plain use, later changes to $x are invisible to the closure; with &, they are visible.

- id: php-error-01
  answer: |
    The hierarchy: Throwable is the base interface; it has two direct implementations, Error and Exception. Error represents internal/engine-level failures not meant to be routinely caught: TypeError, ValueError, ArithmeticError/DivisionByZeroError, ParseError, UnhandledMatchError, AssertionError, etc. Exception is the base for userland and many legacy runtime errors (RuntimeException, LogicException, PDOException, ...). To catch both, use `catch (Throwable $e)`; since PHP 7.1 a single catch can also list a union: `catch (TypeError | ValueError $e)`, or use multiple catch blocks, with more specific ones first.

- id: php-error-02
  answer: |
    finally runs after the try (and any matching catch) no matter how the block exits — normally, via return/break/continue, or via an uncaught exception. If both try/catch and finally contain return, the finally return wins: it executes last, its value becomes the function's return value, and the try's return value is discarded (the try's return expression is still evaluated). If finally throws or returns while an exception is in flight, that exception is replaced/swallowed. Using control flow (return/throw) in finally is generally discouraged for this reason.

- id: php-error-03
  answer: |
    Define a custom exception by extending Exception (or a subclass): `class ServiceException extends Exception {}`. Chain by passing the previous exception as the third constructor argument: `catch (DbException $e) { throw new ServiceException('Failed to load user', 0, $e); }` (the wrapped exception is retrievable via $e->getPrevious()). Chaining preserves the original message, code, and stack trace of the low-level failure, so you can translate it into your layer's vocabulary for logging/diagnostics without losing the root cause. (PHP 8.0+ also records the previous exception thrown in the same try when rethrowing.)

- id: php-error-04
  answer: |
    try/catch only handles thrown Throwable objects that unwind the call stack. set_error_handler registers a callback for non-fatal runtime errors PHP reports — warnings, notices, deprecations (fatals are not routable through it). It's a diagnostic hook, not control flow — but its classic trick is to convert errors into exceptions so they become catchable: `set_error_handler(function ($severity, $message, $file, $line) { if (!(error_reporting() & $severity)) return false; throw new ErrorException($message, 0, $severity, $file, $line); });` With that handler installed, a warning emitted by fopen("missing.txt", "r") throws ErrorException, which you can catch in a try/catch around the call.

- id: php-arrays-01
  answer: |
    PHP has a single array type: an ordered map. A "list" is just an array whose keys are consecutive integers starting at 0; an associative array has arbitrary int/string keys. Internally an array is an ordered hash table (zend_array): buckets addressed by a hash of the key, with insertion order preserved (iteration order = insertion order, not sorted). Keys are ints or strings; canonical numeric strings are converted to int keys. Lists get a memory-optimized "packed" representation. Values are zvals and the structure is refcounted with copy-on-write.

- id: php-arrays-02
  answer: |
    Spread in array literals: `[...$a, ...$b]` appends all elements of each array; since PHP 8.1 string keys are supported, and later unpacked entries overwrite earlier ones with the same key (before 8.1 only integer keys were allowed). Spread in function calls (`$f(...$args)`) passes elements as arguments — since PHP 8.1 string-keyed entries are passed as named arguments. Keyed destructuring (PHP 7.1) pulls values out by key in an assignment: `['a' => $x, 0 => $y] = $arr;` — equivalent to list('a' => $x, 0 => $y) = $arr; you can skip positions with empty slots (`[, $b] = $list`) and mix list() and keyed syntax.

- id: php-arrays-03
  answer: |
    Arrays are value types: assignment and parameter passing create a logically independent copy, so mutating one variable never affects the other. Physically, PHP uses copy-on-write: both variables initially share the same refcounted buffer, and a true deep copy is made only when one side is modified — so passing big arrays is cheap as long as they're not mutated. A & reference is different: both names alias the same array storage, so any change through either name is immediately visible through both, and no COW copy occurs on write.

- id: php-arrays-04
  answer: |
    Keys are coerced: "1" (a canonical decimal-integer string) becomes int 1; 1.9 is truncated toward zero to int 1; true becomes int 1 (false becomes 0); null becomes the empty string "". Strings that are not valid integers (e.g. "1.5", "08", " 1", "-1" is valid actually) stay strings — note "-1" is a valid int key, but "08" and "1.5" remain string keys. So $a[true] and $a[1] hit the same slot.

- id: php-null-01
  answer: |
    $a ?? $b returns $a if it exists and is not null, else $b — isset()-like semantics, so it never emits a notice for an undefined variable, and only null (or undefined) triggers the fallback. $a ?: $b (elvis) evaluates to $a if $a is truthy, else $b — it uses truthiness, so false, 0, "0", "", [] all trigger the fallback, and an undefined $a raises a warning. Example: $x = "" → $x ?? 'd' gives "", but $x ?: 'd' gives 'd'.

- id: php-null-02
  answer: |
    ?-> is the nullsafe method/property access operator (PHP 8.0): if the left operand is null, the access evaluates to null and the call is not performed; otherwise it behaves like ->. Short-circuiting means that once a nullsafe link in a chain produces null, the entire rest of the chain is skipped and the whole chain evaluates to null — in $a?->b()->c, if $a is null, neither b() nor c() runs and the expression is null. (Only a null-safe link short-circuits: if $a is non-null but b() returns null, then ->c on null is still an error.)

- id: php-null-03
  answer: |
    $x ??= $y assigns $y to $x only if $x is null or undefined. It is functionally the shorthand of $x = $x ?? $y, with the same isset-like, notice-free semantics for an undefined $x and lazy evaluation: the right-hand side $y is only evaluated when $x is null (which also holds for plain ??). The difference is purely syntactic sugar — ??= avoids re-reading/rewriting the variable in the common case; the resulting value is the same.

- id: php-null-04
  answer: |
    isset($x) is true if $x exists (or the array key exists) AND is not null — false for null and for undefined variables. empty($x) is true if $x doesn't exist or is "falsy": null, false, 0, "0", "", [], 0.0. $x === null is a strict null check (with a warning if $x is undefined, so guard with isset first). Disagreement example: $x = 0 — isset($x) is true but empty($x) is true as well, so a cleaner disagreement is $x = "a" (isset true, empty false) versus $x = null (isset false, empty true); for a single value where one is true and the other false on the "emptiness" axis: 0 is set (isset true) yet empty (empty true), while null is not set (isset false) yet empty (empty true).

- id: php-match-01
  answer: |
    Differences: (1) Comparison — match uses strict identity (===) between the subject and each arm condition; switch uses loose ==. (2) Fallthrough — match has none: each arm is independent, no break needed, and one arm can list multiple comma-separated conditions; switch falls through into subsequent cases unless you break. (3) Expression vs statement — match is an expression that yields a value ($result = match($x) {...}); switch is a statement. (4) Unmatched values — match throws UnhandledMatchError if nothing matches unless a default arm exists; switch simply does nothing if no case matches.

- id: php-match-02
  answer: |
    Strict comparison bites when types don't line up: match("0") will NOT hit arm `0 => ...` ("0" !== 0), while switch("0") with case 0 matches via loose ==. Similarly match(false) won't hit an arm `0` (false !== 0), and pre-PHP 8 switch would match 0 against even "foo" (0 == "foo" was true). A non-matching subject throws UnhandledMatchError at runtime — a switch would have silently fallen through instead.

- id: php-match-03
  answer: |
    switch is still better when an arm needs multiple statements or complex control flow — match arms are single expressions only; when you genuinely want fallthrough behavior where several cases execute sequentially; when you intentionally (and carefully) want loose comparison so that e.g. "1" and 1 match the same branch; or when you tolerate unhandled subjects without an exception (switch ignores a non-matching subject; match needs a default or throws). It's also the only option in code that must run on PHP < 8.0.

- id: php-match-04
  answer: |
    `match(true) { $age >= 65 => 'senior', $age >= 18 => 'adult', default => 'minor' }` — each arm condition is compared strictly against the subject true, so the first condition that evaluates to true is selected. It works because match compares subject and arm expressions with ===; making the subject the literal true turns arbitrary boolean condition expressions into match arms. Since match is an expression, the whole if/elseif chain collapses into a single value-producing expression, and arms are evaluated top-to-bottom like elseif branches.
```
