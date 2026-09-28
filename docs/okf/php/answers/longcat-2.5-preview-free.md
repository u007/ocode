- id: php-types-01
  answer: |
    declare(strict_types=1) must be the very first statement in a PHP file (it can only follow declare(ticks=1) or a namespace declaration, and must precede any other code). It forces strict type checking for function calls made FROM the file where it is declared — argument types must match the declared parameter types exactly. In the default coercive mode, PHP will silently coerce compatible types: a function declared as foo(int $x) called as foo("5") converts the string "5" to integer 5. Under strict_types=1, that same call throws a TypeError. The declaration is file-scoped: it governs calls originating in that file, not the file where the function itself is defined.

- id: php-types-02
  answer: |
    == is loose comparison: it performs type juggling before comparing, so values of different types may be considered equal after conversion. === is strict comparison: it compares both value and type, with no coercion. A classic surprising case: in PHP 7, "abc" == 0 evaluates to true because the non-numeric string "abc" is coerced to integer 0 (this changed to false in PHP 8). Another: null == false is true, null == "" is true, null == 0 is true, and null == [] is true — all due to type juggling. With ===, none of those are true.

- id: php-types-03
  answer: |
    A union type allows a parameter, return, or property to accept one of several types, declared as int|string (PHP 8.0). An intersection type (PHP 8.1) requires a value to implement ALL listed types simultaneously, declared as ClassA&ClassB — used primarily for interfaces. A nullable type declared as ?T is exactly equivalent to T|null; they are interchangeable. For example, ?int and int|null are the same type. Nullable types can be used in union types but not with null explicitly repeated (int|null|null is a fatal error).

- id: php-types-04
  answer: |
    void (PHP 7.1) is a return-only type indicating the function returns no meaningful value (it implicitly returns null, but the value must not be used). It cannot be used as a parameter type, property type, or in a union. never (PHP 8.1) is also return-only, indicating the function never returns normally — it must throw, exit, or trigger an error. mixed (PHP 8.0) is the top type meaning "any value" — equivalent to the union of all types (object|resource|array|string|float|int|bool|null). mixed can be used anywhere (parameter, return, property). Use void when a function has side effects but no result, never when the function always diverges, and mixed when you genuinely accept or return anything.

- id: php-types-05
  answer: |
    Typed class constants (PHP 8.3) allow specifying a type on a class constant: const int MAX = 100. When a subclass overrides a typed constant, the type must match exactly (it is invariant) — declaring a different type is a compile-time error. The visibility can be widened (e.g., a protected constant can be redeclared as public), but the type must remain the same. This prevents accidental type changes in subclasses.

- id: php-enums-01
  answer: |
    A pure enum has cases that are simple named constants with no associated values: enum Suit { case Hearts; case Spades; }. A backed enum has cases with scalar (string or int) values attached: enum Status: string { case Active = 'active'; }. Backed enums implement the BackedEnum interface and support from() and tryFrom(). You need a backed enum when you must persist or transmit the enum as a scalar (e.g., to a database, JSON API, or session), or when you need to look up a case from a raw value. Pure enums are sufficient when the case name itself carries all needed information.

- id: php-enums-02
  answer: |
    cases() returns an array of all cases defined on the enum, in declaration order. from() accepts a scalar (string or int matching the backing type) and returns the corresponding enum case; if no case matches, it throws ValueError. tryFrom() is the safe variant: it returns null instead of throwing when no case matches the given value. This lets you handle invalid input without exception handling: Status::tryFrom($input) ?? Status::Unknown.

- id: php-enums-03
  answer: |
    Yes, enums can have methods (both instance and static), class constants, and can implement any number of interfaces. Key restrictions versus a normal class: enums cannot be instantiated with new or unserialized via unserialize() of crafted strings; they cannot be cloned; they cannot have a constructor or destructor; they cannot have static properties; they cannot declare properties at all; they cannot extend a class (they implicitly implement UnitEnum or BackedEnum); they cannot use __call or __callStatic; and their cases must be public. Methods and constants added to an enum behave normally otherwise.

- id: php-enums-04
  answer: |
    Yes, each enum case is a singleton instance. Two variables holding Suit::Hearts reference the exact same object in memory. This means identity comparison (===) works reliably: $a === $b is true when both hold Suit::Hearts. It also means match works correctly, because match uses strict comparison (===), and enum cases are compared by identity. You can safely use enum cases in match expressions and as array keys (they are valid object keys).

- id: php-oop-01
  answer: |
    Constructor property promotion (PHP 8.0) allows you to declare and initialize class properties directly in the constructor parameter list by prefixing a parameter with a visibility modifier (public, protected, private) or readonly. For example: public function __construct(private string $name, protected int $age) {} declares and assigns $name and $age as properties. It replaces the verbose pattern of declaring properties at the top of the class and then assigning them in the constructor body, reducing boilerplate significantly.

- id: php-oop-02
  answer: |
    Readonly properties (PHP 8.1) can be initialized only once, and only from within the scope where they are declared (the declaring class scope). After initialization, any attempt to modify them throws an error. They are useful for value objects and DTOs. Readonly classes (PHP 8.2) mark ALL declared properties as readonly implicitly — you don't need to annotate each one. Additionally, readonly classes prevent the creation of dynamic properties (undeclared properties cannot be added). A readonly class can only extend another readonly class.

- id: php-oop-03
  answer: |
    self refers to the class where the method or property is defined — it is resolved at compile time. static refers to the class that was actually called at runtime — this is late static binding (LSB). $this refers to the current object instance. Late static binding solves the problem where a static method in a parent class needs to create an instance of or reference the called class rather than the defining class. For example, with `new self()` in a parent static method called from a child, you always get the parent class. With `new static()`, you get the child class — this is the core of the Singleton and Active Record patterns.

- id: php-oop-04
  answer: |
    Asymmetric visibility and property hooks were both introduced in PHP 8.4. Asymmetric visibility allows different visibility for reading versus writing — e.g., public private(set) int $x means the property is publicly readable but only privately writable. This lets you expose immutable data without a separate getter method. Property hooks allow defining get and/or set logic directly on a property declaration: public int $x { get { return $this->x * 2; } set(int $v) { $this->x = abs($v); } }. They let you avoid boilerplate getter/setter methods while still adding validation, lazy initialization, or computed values, and they integrate with asymmetric visibility.

- id: php-oop-05
  answer: |
    The #[\Override] attribute (PHP 8.3) marks a method as intending to override a parent class method or implement an interface/trait method. If no such override actually exists (e.g., the parent method was renamed or removed, or the method name is misspelled), PHP throws a compile-time error. It catches the class of bug where a developer thinks they are overriding a method but are not — for example, renaming a method in a parent class and forgetting to update the child, resulting in a silent behavioral change instead of an error.

- id: php-closures-01
  answer: |
    A closure defined with `function () use ($x) {}` requires an explicit use clause to capture variables from the parent scope, capturing by value by default. An arrow function `fn () => expr` automatically captures all variables from the parent scope by value without needing use — the capture is implicit and always by-value. Arrow functions are limited to a single expression (which becomes the return value) and cannot contain statements. Both capture by value by default; neither automatically captures by reference.

- id: php-closures-02
  answer: |
    The first-class callable syntax (PHP 8.1) lets you create a Closure from any callable by appending (...) to it: strlen(...), $obj->method(...), Foo::bar(...). It produces a Closure object that wraps the callable. It is equivalent to Closure::fromCallable(strlen) but is more concise and works in contexts where a callable expression is expected. The resulting closure can be stored, passed, called with arguments, or used with array_map, usort, etc.

- id: php-closures-03
  answer: |
    Closure::bind(Closure $closure, ?object $newThis, object|string|null $newScope) and its instance-method equivalent $closure->bindTo($newThis, $newScope) rebind the closure's $this and/or class scope. After binding, $this inside the closure refers to the new object, and the class scope determines which private and protected members of that object's class are accessible from within the closure. This is how you can give a closure access to an object's private state without making that state public.

- id: php-closures-04
  answer: |
    `use ($x)` captures $x by value — the closure gets a copy of $x's value at the time the closure is defined. Later changes to $x outside the closure do not affect the closure's copy. `use (&$x)` captures $x by reference — the closure sees the current value of $x at the time it is called, so if $x changes after the closure is defined, the closure observes the updated value.

- id: php-error-01
  answer: |
    The hierarchy: Throwable is the top-level interface. It has two main implementors: Exception (for userland exceptions and most built-in exceptions like RuntimeException, InvalidArgumentException, etc.) and Error (for internal PHP engine errors like TypeError, ValueError, ParseError, ArithmeticError, DivisionByZeroError, etc.). Error and Exception are sibling classes, both implementing Throwable directly. To catch both, use catch (Throwable $e). Catching Exception alone will miss Errors (e.g., TypeError from strict_types), and catching Error alone will miss Exceptions.

- id: php-error-02
  answer: |
    The finally block always executes after the try and catch blocks, regardless of whether an exception was thrown, caught, or neither — even if there is a return, break, or continue in the try/catch. If both the try/catch and the finally block contain return statements, the return in finally takes precedence and overrides the try/catch return value. The finally block runs before the function actually returns to the caller.

- id: php-error-03
  answer: |
    Define a custom exception by extending Exception or a more specific built-in subclass: class MyException extends RuntimeException {}. Chain a lower-level exception by passing it as the third argument to the constructor: throw new MyException("high-level message", 0, $previousException). Chaining preserves the full call stack and original error context, which aids debugging — you can walk the chain via getPrevious() to find the root cause. Without chaining, you lose the original stack trace and context.

- id: php-error-04
  answer: |
    set_error_handler registers a callback that intercepts PHP errors (warnings, notices, deprecations) — it does not throw by default. try/catch catches Throwables (Exceptions and Errors). To make PHP warnings catchable as exceptions, register an error handler that converts them to ErrorException: set_error_handler(function($severity, $message, $file, $line) { throw new ErrorException($message, 0, $severity, $file, $line); }). Then wrap the code in try/catch. This bridges the gap between procedural error reporting and exception-based error handling.

- id: php-arrays-01
  answer: |
    A list is an array with integer keys that are consecutive and start from 0 (i.e., keys 0, 1, 2, ..., n-1 in order) — or more generally, any array where the keys are 0 through count-1 without gaps. An associative array has string keys or non-sequential integer keys. Internally, all PHP arrays are ordered hash tables (hash maps) that maintain insertion order. Each element is a bucket storing a key (which can be int or string), a value (zval), and a hash. The data structure uses a doubly-linked list to preserve iteration order separate from the hash lookup.

- id: php-arrays-02
  answer: |
    The spread operator (...) in array literals merges arrays: [...$a, ...$b]. For integer keys, values are appended and keys are renumbered sequentially. For string keys, later values overwrite earlier ones with the same key (string-keyed spread support was added in PHP 8.1). Keyed destructuring lets you extract by key: ['x' => $a, 'y' => $b] = ['x' => 1, 'y' => 2] assigns 1 to $a and 2 to $b. Similarly, [$a, $b] = [1, 2] or list($a, $b) = [1, 2] works with positional keys. Destructuring skips elements by omitting keys in the pattern.

- id: php-arrays-03
  answer: |
    PHP arrays are value types with copy-on-write semantics. Assigning an array to a variable or passing it to a function does NOT immediately copy the array — instead, a reference count is incremented and the underlying structure is shared. The actual copy is deferred until one of the holders modifies the array (copy-on-write). This makes array passing cheap. Using & creates a true reference: both variables share the same underlying storage, so modifications through one are visible through the other immediately. Without &, modifications to a copy do not affect the original after a write.

- id: php-arrays-04
  answer: |
    PHP coerces array keys as follows: a string "1" that is a valid integer literal becomes integer 1. A float 1.9 is truncated to integer 1 (the fractional part is discarded, not rounded). Boolean true becomes integer 1. Null becomes an empty string "". These coercions mean that $arr["1"], $arr[1.9], $arr[true], and $arr[null] all refer to the same element as $arr[1] (or $arr[""] for null).

- id: php-null-01
  answer: |
    ?? (null coalescing) checks whether the left operand is null — it uses isset semantics, so it returns the left operand if it exists and is not null, otherwise the right operand. It does not emit warnings for undefined variables or array keys. ?: (short ternary) checks truthiness — it evaluates the left operand as a boolean. So $x = 0 yields 0 with ?? but 'default' with ?:. Another difference: ?? does not emit a notice for undefined variables; ?: does emit a warning for undefined variables.

- id: php-null-02
  answer: |
    The nullsafe operator ?-> (PHP 8.0) returns null if the left-hand side is null, instead of throwing a fatal error. For example, $a?->b() returns null if $a is null. Short-circuiting means that if any step in the chain produces null, the entire remaining chain is skipped and the expression evaluates to null. In $a?->b()->c, if $a is null, neither b() nor c is called — the whole expression is null. If $a->b() returns null, then c is not accessed on it. Each ?-> link short-circuits the rest of the chain when null is encountered.

- id: php-null-03
  answer: |
    The ??= operator assigns the right operand to the left operand only if the left operand is null (or undefined). $x ??= $y is semantically equivalent to $x = $x ?? $y. The practical difference is that ??= only performs a write when $x is null, whereas $x = $x ?? $y always evaluates and assigns back. This matters for array keys or expensive expressions where you want to avoid unnecessary writes. In terms of final state, they are equivalent for simple variables, but ??= is more concise and signals intent more clearly.

- id: php-null-04
  answer: |
    isset($x) returns true if $x exists and is not null. empty($x) returns true if $x is "empty": null, false, 0, 0.0, "", "0", [], or undefined. $x === null checks specifically whether $x is null (and nothing else). A value where isset and empty disagree: $x = "0" — isset returns true (it exists and is not null), but empty returns true ("0" is one of the empty values). They disagree: isset says it's set, empty says it's empty. Another clear case: $x = null — isset is false, empty is true.

- id: php-match-01
  answer: |
    match uses strict comparison (===) while switch uses loose comparison (==). match does not fallthrough — each arm is independent and no break is needed; switch falls through to the next case unless break is used. match is an expression (it returns a value); switch is a statement. If no arm matches and no default is provided, match throws UnhandledMatchError; switch silently does nothing. match supports multiple conditions per arm separated by commas (OR logic): match($x) { 1, 2, 3 => 'small' }; switch requires stacked case labels for that. match arms can also be expressions.

- id: php-match-02
  answer: |
    Because match uses strict comparison (===), it avoids the type-juggling surprises of switch. For example, match(0) { "abc" => 'a' } does NOT match in PHP 8 — whereas switch(0) { case "abc": } would have matched in PHP 7 (0 == "abc" was true). A more concrete surprise: match(true) { 1 => 'one' } does NOT match (true === 1 is false), but switch(true) { case 1: } DOES match (true == 1 is true). Similarly, match(1) { "1" => 'str' } does NOT match, while switch(1) { case "1": } does. This strictness is usually safer but can surprise those accustomed to switch's loose comparison.

- id: php-match-03
  answer: |
    switch is still the better fit when you need intentional fallthrough — multiple cases executing the same block (case 1: case 2: case 3:). switch is also preferable when you deliberately want loose comparison for backward compatibility (though this is rare and often a code smell). Additionally, switch can be more readable when matching many scalar values against a single variable with shared bodies, and in legacy codebases where switch is the established convention.

- id: php-match-04
  answer: |
    You write match(true) { $a > 5 => 'big', $a > 0 => 'small', default => 'zero' }. It works because match compares the subject (true) against each arm's expression using strict comparison (===). Each condition expression is evaluated to a boolean; when a condition returns true, the comparison true === true succeeds, so that arm matches. The first matching arm wins, providing the same short-circuit behavior as an if/elseif chain. It works because the subject is always true, and each arm's expression must evaluate to true for the strict comparison to succeed.
