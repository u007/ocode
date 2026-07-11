---
model_id: tencent/hy3
model_version: "3.0"
evaluated_via: novita
evaluated_on: 2026-07-11
stack: php
stack_corpus_rev: 1
threshold: 0.75
---

# Scorecard — tencent/hy3 on php

> Valid ONLY for `tencent/hy3` @ `3.0`. A version bump invalidates this
> scorecard — re-benchmark.

Grader independent (Opus 4.8); answers produced closed-book via
`ocode run -dir <empty>` (no corpus access).

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| php-types-01 | types | 3 | 3 | 3 | 1.00 | first-statement/per-file, strict TypeError, coercive default all correct |
| php-types-02 | types | 3 | 3 | 2 | 0.67 | ===/== mechanism correct; example WRONG for graded version — claims `0 == "php"` true and `"" == 0` true (both false in 8.0+, PHP-7 behavior); only `null == ""` correct → point 3 denied |
| php-types-03 | types | 2 | 3 | 3 | 1.00 | union/intersection(class-only)/`?T`=`T\|null` all correct |
| php-types-04 | types | 2 | 3 | 3 | 1.00 | void/never/mixed correctly contrasted |
| php-types-05 | types | 1 | 2 | 1 | 0.50 | MISS: claims typed class constants added in PHP 8.1 — wrong, it is 8.3; override-compatibility rule correct |
| php-enums-01 | enums | 2 | 2 | 2 | 1.00 | pure vs backed + persistence rationale correct |
| php-enums-02 | enums | 2 | 3 | 3 | 1.00 | cases()/from() ValueError/tryFrom() null all correct |
| php-enums-03 | enums | 2 | 3 | 3 | 1.00 | methods/no-state/final all present; minor aux error — claims backed enums "may define a constructor" (enums have no constructor), award stands |
| php-enums-04 | enums | 1 | 2 | 2 | 1.00 | singleton identity + === /match correct |
| php-oop-01 | oop | 2 | 2 | 2 | 1.00 | promotion mechanics correct |
| php-oop-02 | oop | 3 | 3 | 3 | 1.00 | readonly prop write-once, readonly class, extension constraint all correct |
| php-oop-03 | oop | 2 | 3 | 3 | 1.00 | self/static/$this + LSB `new static()` correct |
| php-oop-04 | oop | 1 | 3 | 3 | 1.00 | 8.4, asymmetric visibility, property hooks all correct |
| php-oop-05 | oop | 1 | 2 | 2 | 1.00 | `#[\Override]` intent + compile-time check correct |
| php-closures-01 | closures | 2 | 2 | 2 | 1.00 | arrow auto-capture vs explicit use() correct |
| php-closures-02 | closures | 2 | 2 | 2 | 1.00 | first-class callable produces Closure, type-safe correct |
| php-closures-03 | closures | 1 | 2 | 2 | 1.00 | new bound closure + scope→private access correct |
| php-closures-04 | closures | 2 | 2 | 2 | 1.00 | use($x) snapshot vs use(&$x) shared correct |
| php-error-01 | error-handling | 2 | 3 | 3 | 1.00 | Throwable root, Error vs Exception, catch Throwable correct; minor aux error — calls Exception/Error "final" (they are not), award stands |
| php-error-02 | error-handling | 2 | 2 | 2 | 1.00 | finally always runs + return-override correct |
| php-error-03 | error-handling | 2 | 3 | 3 | 1.00 | extend/`$previous` chain/getPrevious() correct |
| php-error-04 | error-handling | 2 | 3 | 3 | 1.00 | try/catch vs set_error_handler + ErrorException bridge correct |
| php-arrays-01 | arrays | 2 | 2 | 2 | 1.00 | single ordered-map + list vs assoc correct |
| php-arrays-02 | arrays | 2 | 2 | 2 | 1.00 | 8.1 string-key unpack + destructuring correct |
| php-arrays-03 | arrays | 3 | 3 | 3 | 1.00 | COW value semantics + &-reference contrast correct |
| php-arrays-04 | arrays | 2 | 2 | 2 | 1.00 | key coercion "1"→1, 1.9→1, true→1, null→"" correct |
| php-null-01 | null-safety | 3 | 2 | 2 | 1.00 | ?? (isset-based) vs ?: (truthy) correct |
| php-null-02 | null-safety | 2 | 2 | 2 | 1.00 | ?-> null-yield + short-circuit correct |
| php-null-03 | null-safety | 1 | 2 | 2 | 1.00 | ??= only-if-null + lazy RHS correct |
| php-null-04 | null-safety | 2 | 3 | 3 | 1.00 | isset/empty/===null + disagreement example correct |
| php-match-01 | match-control | 3 | 3 | 3 | 1.00 | strict ===, no fallthrough/expression, UnhandledMatchError correct |
| php-match-02 | match-control | 2 | 2 | 2 | 1.00 | match strictness correct; aux error — claims `switch('foo'){case 0:}` matches via `'foo'==0` (PHP-7 behavior, false in 8.0+); match points stand |
| php-match-03 | match-control | 1 | 2 | 2 | 1.00 | switch for fallthrough/statement-blocks correct |
| php-match-04 | match-control | 1 | 2 | 2 | 1.00 | match(true) if/elseif replacement correct |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| types | 0.86 | 5 | ok | omit (strong) |
| enums | 1.00 | 4 | ok | omit (strong) |
| oop | 1.00 | 5 | ok | omit (strong) |
| closures | 1.00 | 4 | ok | omit (strong) |
| error-handling | 1.00 | 4 | ok | omit (strong) |
| arrays | 1.00 | 4 | ok | omit (strong) |
| null-safety | 1.00 | 4 | ok | omit (strong) |
| match-control | 1.00 | 4 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

types = (1.00×3 + 0.67×3 + 1.00×2 + 1.00×2 + 0.50×1) / 11 = 9.5/11 = 0.864

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 64.5 / 66 = 97.7%
```

## Derivation targets

Tags below threshold (`< 0.75`): **none** — every tag scores ≥ 0.75 (lowest is
`types` at 0.86). **No derivation**: `derived/php.tencent__hy3.SKILL.md` is NOT
created. tencent/hy3 knows this stack well; only two isolated version/PHP-7-era
misses (typed-constant version, pre-8.0 `==` juggling) survive, neither dragging
a tag under threshold.
