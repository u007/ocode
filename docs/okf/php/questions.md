# PHP Benchmark — Human Render

> **Generated from `questions.yaml` (corpus_rev 1). Do not grade from this file
> — `questions.yaml` is the source of truth.** Hand-synced; if they disagree,
> the YAML wins.

Legend: **W** = weight (1–3), **D** = difficulty. Rubric shows scored points
(`•`) and partial-credit levels (`~`).

---

### php-types-01 · types · W3 · medium
**Q:** What does `declare(strict_types=1)` do, where must it go, and how does it change argument handling vs coercive mode?
**A:** Must be the first statement; per-file, affecting calls made *from* that file. Strict mode rejects scalar mismatches with a `TypeError` (a `"5"` string won't fill an `int`); coercive default silently coerces compatible scalars. Sole exception: `int` still widens to a declared `float`.
• first statement / per-file • strict: no coercion, TypeError on mismatch • coercive default coerces scalars ~ "enforces types" without the coercion contrast

### php-types-02 · types · W3 · easy
**Q:** `==` vs `===`, and a surprising type-juggling result.
**A:** `===` = same value and type, no conversion. `==` coerces to a common type first. Since 8.0 `0 == "foo"` is false (non-numeric string compared as string), but `"1e2" == "100"` and `null == false` are true. Prefer `===`.
• === value+type no coercion • == coerces first • a concrete juggling example ~ "=== is stricter" with no mechanism/example

### php-types-03 · types · W2 · medium
**Q:** Union, intersection, and nullable types; how `?T` relates to `T|null`.
**A:** Union `A|B` = any listed type (8.0). Intersection `A&B` = satisfies all (class/interface only, 8.1). `?T` is shorthand for `T|null`; DNF like `(A&B)|null` is allowed since 8.2. `?` can't combine with a multi-member union.
• union = any of listed • intersection = all (class/interface only) • ?T = T|null ~ defines union but conflates ?T or omits intersection

### php-types-04 · types · W2 · medium
**Q:** Contrast `never`, `void`, and `mixed`.
**A:** `void` (8.0) returns no value. `never` (8.1) never returns normally — always throws/exits (bottom type). `mixed` (8.0) is the top type (accepts anything, includes null). `never` ≠ `void`: void returns, never does not.
• void returns no value • never never returns (bottom type) • mixed accepts any (top type) ~ confuses never with void, or "mixed = anything" only

### php-types-05 · types · W1 · medium
**Q:** Typed class constants — which version, and the rule when a subclass overrides one?
**A:** Since PHP 8.3, class/interface/enum/trait constants may declare a type (`const string VERSION = '1.0';`); before 8.3 it was implicit from the value. The type is enforced at compile time, and an overriding child constant must be type-compatible with the parent's declaration. `self`/`static`/`parent` and `never`/`void`/`callable` aren't allowed as constant types.
• constants explicitly typed since 8.3 • overriding child constant must be type-compatible with the parent ~ "constants can have types" without version or override rule

### php-enums-01 · enums · W2 · easy
**Q:** Pure vs backed enum (8.1), and when you need a backed one.
**A:** Enums arrived 8.1. Pure enum cases have identity only; backed enum cases have an int/string `->value`. Use backed for serialization/persistence/DB/API mapping; pure for a closed set of named options.
• pure = no scalar value • backed = int/string ->value for persistence ~ "named constants" without the pure/backed split

### php-enums-02 · enums · W2 · medium
**Q:** `cases()`, `from()`, `tryFrom()` — and how the last two differ on an invalid value.
**A:** `cases()` returns all cases in declaration order (all enums). Backed enums add `from()`/`tryFrom()` (strict lookup by backing value): `from()` throws `\ValueError`, `tryFrom()` returns `null`.
• cases() all cases in order • from() throws \ValueError • tryFrom() returns null ~ swaps the from/tryFrom failure behavior

### php-enums-03 · enums · W2 · medium
**Q:** Can enums have methods/constants/interfaces? Key restrictions vs a class?
**A:** Yes — methods (with `$this` = current case), constants, interfaces (backed → `BackedEnum`, all → `UnitEnum`), traits (no properties). But: no instance state/properties, cannot `new`, implicitly final (no extending).
• methods/constants/interfaces allowed • no instance state; can't instantiate • implicitly final ~ "yes to methods" without the no-state restriction

### php-enums-04 · enums · W1 · medium
**Q:** Two vars hold `Suit::Hearts` — same instance? Effect on comparison/match?
**A:** Each case is a singleton, so all references are the same instance. `===` is the idiomatic compare, and cases are safe as `match` subjects/arms (match uses `===`). Can't clone a case.
• each case is a singleton • so ===/match(strict) is correct ~ "compare with ==" without the singleton insight

### php-oop-01 · oop · W2 · easy
**Q:** Constructor property promotion (8.0), and what it replaces.
**A:** A visibility/`readonly` modifier on a constructor param declares and assigns the property automatically (`__construct(private string $name)`), removing the declare + list + `$this->name = $name` boilerplate. Only modified params are promoted.
• modifier on ctor param declares+assigns property • removes the boilerplate ~ "shorthand" without the modifier-triggers-promotion detail

### php-oop-02 · oop · W3 · medium
**Q:** `readonly` properties (8.1) vs `readonly` classes (8.2) — when writable, what the class adds?
**A:** A readonly property must be typed and is written once from the declaring class scope; later writes throw `Error` (runtime-set, not a constant). A readonly class (8.2) makes all properties readonly, forbids dynamic properties, needs typed non-static props, and is extendable only by readonly classes. 8.3 allows re-init during `__clone`.
• readonly prop: typed, write-once in scope, then Error • readonly class = all props readonly / no dynamic props • runtime-set OR extended only by readonly class ~ "like a constant" without write-once-in-scope

### php-oop-03 · oop · W2 · hard
**Q:** `self` vs `static` vs `$this`, and what late static binding solves.
**A:** `$this` = the instance. `self` = the class where the code is written (compile time). `static` = the class actually called at runtime (late static binding). `new self()` in a parent builds the parent; `new static()` builds the actual subclass — the fix for factory/fluent methods.
• $this = instance; self = compile-time defining class • static = runtime called class (LSB) • new static() builds subclass vs new self() the parent ~ self vs static noted but compile-vs-runtime implicit

### php-oop-04 · oop · W1 · hard
**Q:** Asymmetric visibility and property hooks — which version, and what they avoid?
**A:** Both are PHP 8.4. Asymmetric visibility gives a wider read than write scope (`public private(set) int $calls;`), removing write-guarding getters. Property hooks define inline `get`/`set` on a property (`public string $name { get => ...; set => ...; }`), avoiding separate getter/setter methods and backing fields.
• both introduced in 8.4 • asymmetric = wider read than write (public private(set)) • hooks = inline get/set, no separate methods ~ one feature right but wrong version or omits the other

### php-oop-05 · oop · W1 · medium
**Q:** What does `#[\Override]` (8.3) do, and what bug does it catch?
**A:** Declares a method is meant to override a parent/interface method; the engine errors at compile time if no matching parent method exists. Catches the silent bug where a typo or a removed/renamed parent method turns your "override" into a never-called new method (like Java `@Override`). Not on constructors.
• asserts it overrides a parent/interface method (8.3) • compile error if no matching parent method (catches typos) ~ "marks an override" without the no-parent guarantee

### php-closures-01 · closures · W2 · medium
**Q:** Closure (`function () use (...) {}`) vs arrow function (`fn () => ...`) capture.
**A:** Arrow fn (7.4) auto-captures enclosing vars by value on use — no `use`. A classic closure captures nothing implicitly; you list vars in `use (...)`, captured by value at definition time unless `use (&$x)`. Both are by-value by default; arrow fns are limited to one expression.
• arrow fn auto-captures by value (no use) • closure needs explicit use(...), by value at definition ~ "fn is shorthand" without auto-capture-by-value

### php-closures-02 · closures · W2 · medium
**Q:** First-class callable syntax `strlen(...)` (8.1) — what does it produce?
**A:** Since 8.1, `(...)` makes a `Closure` from a callable without calling it (`strlen(...)`, `$obj->m(...)`, `Foo::s(...)`). Replaces string/array callables with a type-safe, analyzable, scope-respecting reference; result is a normal Closure.
• (...) makes a Closure without invoking (8.1) • replaces string/array callables, type-safe ~ "a way to call a function" — misses the Closure result

### php-closures-03 · closures · W1 · hard
**Q:** `Closure::bind`/`bindTo` — effect on `$this` and scope.
**A:** Return a *new* closure bound to a given object as `$this` and optionally a class scope (closures are immutable, original unchanged). Binding the scope grants access to the object's private/protected members; binding only the object gives public access.
• returns a NEW closure with $this bound (original unchanged) • binding scope grants private/protected access ~ "sets $this" without new-closure or scope

### php-closures-04 · closures · W2 · medium
**Q:** `use ($x)` vs `use (&$x)`, and what the closure sees if `$x` changes later.
**A:** `use ($x)` = by value, snapshotted at definition, so later outer changes are invisible. `use (&$x)` = by reference, shares the variable and sees/mutates later changes. Capturing loop vars by value fixes the "all closures see the last value" bug.
• use($x) by value, snapshot at definition • use(&$x) by reference, shared/mutable ~ knows one direction not the snapshot-vs-shared consequence

### php-error-01 · error-handling · W2 · medium
**Q:** The throwable hierarchy; `Error` vs `Exception`; catching both.
**A:** All catchable types implement `Throwable`, with sibling branches `Exception` (app-level) and `Error` (engine/bug faults: `TypeError`, `ValueError`, `DivisionByZeroError`, `UnhandledMatchError`). Neither extends the other; `catch (\Throwable)` catches both — catching `\Exception` misses `Error`s.
• Throwable root; Error & Exception siblings • Error = engine faults, Exception = app-level • catch \Throwable for both; Exception misses Error ~ "both are exceptions" without the distinction

### php-error-02 · error-handling · W2 · medium
**Q:** `try`/`catch`/`finally` — when does `finally` run, and what wins if both `return`?
**A:** `finally` always runs (success, throw, or return; even before an uncaught exception propagates). If both try/catch and finally `return`, the finally return wins; a `throw` in finally supersedes a pending exception. Use it for cleanup on every path.
• finally always runs • return/throw in finally overrides the try/catch return or pending exception ~ "finally runs cleanup" without the override

### php-error-03 · error-handling · W2 · medium
**Q:** Define a custom exception and chain a lower-level one — and why.
**A:** Extend `\Exception` (or a subclass). Pass the caught exception as the 3rd `$previous` arg: `throw new DomainException('...', 0, $e);`; retrieve with `getPrevious()`. Chaining preserves the root cause and trace while rethrowing a domain-meaningful type.
• extend \Exception for a custom type • pass original as $previous (3rd arg) • getPrevious() / preserves root cause ~ extends Exception but no $previous chaining

### php-error-04 · error-handling · W2 · hard
**Q:** `set_error_handler` vs `try`/`catch`; making warnings (e.g. `fopen`) catchable.
**A:** `try`/`catch` only catches thrown `Throwable`s, not `E_WARNING`/`E_NOTICE` (a failed `fopen` just warns and continues). `set_error_handler()` intercepts those globally; have it throw an `\ErrorException` so a surrounding `try`/`catch` can handle them. Some faults are already `Error`s (e.g. `DivisionByZeroError` in 8.0+).
• try/catch catches Throwables, not E_WARNING/E_NOTICE • set_error_handler intercepts them globally • throw \ErrorException to make warnings catchable ~ "use set_error_handler" without the throw-ErrorException bridge

### php-arrays-01 · arrays · W2 · easy
**Q:** Single `array` type — "list" vs "associative", and how a PHP array is structured.
**A:** One array type: an ordered key→value map. A "list" has sequential 0-based int keys with no gaps (`array_is_list()`, 8.1); an "associative array" uses string/sparse int keys. Same underlying ordered hash-map; insertion order is preserved — the distinction is convention.
• one array type = ordered map/hash • list = 0-based int keys; assoc = string/sparse (same type) ~ "lists indexed, maps keyed" without the single ordered-map insight

### php-arrays-02 · arrays · W2 · medium
**Q:** Spread/unpacking with string keys (8.1); keyed destructuring.
**A:** `[...$a, ...$b]` unpacks into a new array; since 8.1 string keys unpack too (later dupes overwrite; int keys are renumbered). Destructuring is the inverse: `[$x, $y] = $arr` by position, `['id' => $id] = $arr` by key (order-independent); can nest/skip.
• [...$a] unpacks; string keys since 8.1 (dupes overwrite, ints renumbered) • destructure by position or by ['k'=>$v] ~ knows spread OR destructuring but not the 8.1 string-key detail

### php-arrays-03 · arrays · W3 · hard
**Q:** Copy-on-write value semantics vs `&` references.
**A:** Arrays are value types: `$b = $a` (or pass-by-value) logically copies, but PHP defers the physical copy until a write — copy-on-write. Once either side is written they diverge; the copy never affects the original. A `&` reference makes both names the same variable, so a write is shared — which is why mutating a by-value array param doesn't affect the caller unless it's `&$arr`.
• assignment/pass-by-value logically copies (COW defers physical copy) • modifying the copy doesn't affect the original • & makes both names one variable, write is shared ~ "arrays are copied" without COW or the &-contrast

### php-arrays-04 · arrays · W2 · medium
**Q:** Array key coercion — what do `"1"`, `1.9`, `true`, `null` become?
**A:** Keys are only int or string. Canonical integer strings become ints (`"1"` → `1`, so `$a["1"]` == `$a[1]`; but `"01"`/`"1.0"` stay strings); floats truncate toward zero (`1.9` → `1`, deprecated 8.1+); `true`→`1`, `false`→`0`; `null`→`""`.
• keys only int/string; integer-like strings → int ("1"→1) • float truncates, bool→1/0, null→"" ~ "keys get coerced" with only one correct case

### php-null-01 · null-safety · W3 · easy
**Q:** `??` (null coalescing) vs `?:` (short ternary / elvis).
**A:** `$a ?? $b` = `$a` if set and not null, else `$b` — isset-based, no notice on undefined (the safe default operator). `$a ?: $b` = `$a` if truthy, else `$b` — so `0`, `""`, `"0"`, `[]`, `false` fall through, and it warns on undefined. Use `??` for maybe-null/missing, `?:` only for a real truthiness test.
• ?? checks null/unset (isset-based), no notice • ?: checks truthiness — 0/""/[]/false fall through ~ "both give a default" without null-vs-falsy

### php-null-02 · null-safety · W2 · medium
**Q:** Nullsafe `?->` (8.0) and what "short-circuiting" means in `$a?->b()->c`.
**A:** `$obj?->method()` yields `null` instead of erroring when the left is null. Short-circuit: once a link is null, the rest of the chain is skipped and the whole expression is null — so if `$a` is null, `b()` is never called nor `->c` evaluated. Reads/calls only, not an assignment target.
• ?-> yields null when left is null (8.0) • short-circuits: a null skips the rest → whole expr null ~ "avoids null errors" without the short-circuit semantics

### php-null-03 · null-safety · W1 · easy
**Q:** `??=` and how it differs from `$x = $x ?? $y`.
**A:** `$x ??= $y` assigns `$y` only if `$x` is null/unset, else leaves it. Unlike a plain rewrite, the right side is evaluated (and `$x` written) only when needed — lazy. Idiomatic "set a default if not already set": `$opts['timeout'] ??= 30`.
• assigns only if left is null/unset (else no-op) • short-circuits: right side evaluated only when needed ~ "sets a default" without the only-if-null detail

### php-null-04 · null-safety · W2 · medium
**Q:** `isset` vs `empty` vs `=== null`; a value where isset and empty disagree.
**A:** `isset($x)` = set and not null (no notice on undefined). `empty($x)` = falsy OR unset (`0`, `"0"`, `""`, `null`, `false`, `[]`). `$x === null` strictly tests null but warns if undefined. They disagree on `$x = 0`: isset true (it's set), empty true (0 is falsy). `empty` is "falsy-or-unset", not "not set".
• isset = set and not null (no notice) • empty = falsy or unset • a disagreeing value, e.g. 0/"0" ~ "existence vs emptiness" with no falsy-set example

### php-match-01 · match-control · W3 · medium
**Q:** `match` vs `switch` — comparison, fallthrough, expression, unmatched.
**A:** `match` (8.0) compares with strict `===`; `switch` uses loose `==`. `match` has no fallthrough (no `break`; comma-share arms) and is an expression returning a value; `switch` is a statement. Unmatched with no `default` → `match` throws `\UnhandledMatchError`, `switch` silently does nothing.
• match === vs switch == • match no fallthrough; is a value-returning expression • unmatched match throws \UnhandledMatchError ~ "match is the modern switch" with one difference

### php-match-02 · match-control · W2 · medium
**Q:** Strict-comparison surprises `match` can cause vs `switch`, e.g. matching `0`.
**A:** `===` means no juggling: `match("1")` won't hit a `1 =>` arm, `match(0)` won't hit `"foo" =>`/`false =>` like loose `switch` would. Usually desired, but bites when input strings must hit int arms — match the exact type or cast. Upside: no `0 == "abc"` false matches.
• === = no juggling: "1" won't match a 1 arm • must match exact type/cast, but avoids switch's loose false-matches ~ "match is strict" without a concrete consequence

### php-match-03 · match-control · W1 · medium
**Q:** When is `switch` still the better fit than `match`?
**A:** When you want fallthrough (cases sharing a statement tail), when a branch runs a block of statements rather than one value (match arms are single expressions), or when you deliberately want loose `==`. `match` = value-returning exhaustive dispatch; `switch` = multi-statement/side-effect/fallthrough flow.
• switch for fallthrough or multi-statement branches • match is single-expression/value-returning ~ "switch for old code" with no real reason

### php-match-04 · match-control · W1 · medium
**Q:** Using `match(true)` to replace an if/elseif chain, and why it works.
**A:** `match (true)` puts a boolean condition in each arm; the first arm `=== true` is chosen, giving a value-returning if/elseif chain: `match (true) { $n < 0 => 'neg', $n === 0 => 'zero', default => 'pos' }`. Works because match evaluates arms top-to-bottom (strict) against the subject `true`.
• match(true) selects first arm whose condition === true • expression-form if/elseif (returns a value, top-to-bottom) ~ "use match(true)" without the true-subject strict match
