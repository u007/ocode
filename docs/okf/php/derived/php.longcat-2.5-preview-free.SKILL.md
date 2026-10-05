---
name: php-tuning-longcat-2.5-preview-free
description: >
  Corrective PHP knowledge for longcat-2.5-preview-free, targeting its gaps in
  OOP (readonly, late static binding), closures (first-class callable syntax,
  Closure::bind) and match vs switch control flow.
when_to_use: >
  Load when the provider-stripped model id (see stack-detection.md) resolves to
  exactly `longcat-2.5-preview-free` AND the repository is a PHP project
  (composer.json present, or *.php files — per meta.yaml detection). For any
  other model or non-PHP repo, do not load.
tuned_for: longcat-2.5-preview-free
tuned_version: "2.5-preview"
stack: php
source_scorecard: ../scores/longcat-2.5-preview-free.rerun-2026-10-05.md
threshold: 0.85
revalidate_when: model_version changes
---
# PHP corrections for longcat-2.5-preview-free

## OOP: readonly and late static binding

- A `readonly` property must be typed and is write-once, only from the declaring class's scope; any later write, even inside the class, throws `Error`.
- A `readonly` property is not a constant: its value is set at runtime (usually in the constructor).
- A `readonly` class (8.2) makes every declared property readonly, forbids dynamic properties, and requires typed, non-static properties.
- A `readonly` class may only extend, and be extended by, another `readonly` class.
- `$this` is the current instance and does not exist in static methods.
- `self` resolves at compile time to the class where the code is written; `static` resolves at runtime to the class actually called (late static binding).
- `new self()` in a parent always builds the parent; `new static()` builds the called subclass. Use `new static()` and `static::method()` in factories and fluent/static methods meant to work for subclasses.

## Closures: first-class callables and binding

- `strlen(...)`, `$obj->method(...)`, `Foo::bar(...)` and `$obj(...)` create a `Closure` without calling it (8.1).
- First-class callable syntax replaces string and array callables (`'strlen'`, `[$obj, 'method']`) with a type-safe, IDE/static-analysis-friendly reference.
- Unlike string/array callables, it resolves in the current scope, so it can reference private/protected methods from inside the class.
- `Closure::bind($c, $obj, $scope)` and `$c->bindTo($obj, $scope)` return a NEW closure; the original closure is unchanged.
- Binding `$this` alone gives only public access; passing the class scope (second argument, class name or object) is what grants access to private/protected members.
- Closures and arrow functions defined inside a method capture `$this` automatically; binding re-targets it explicitly.

## match vs switch

- `match` compares with `===`, with no type juggling: `match("1")` does not hit a `1 =>` arm, and `match(0)` does not hit a `"foo" =>` or `false =>` arm.
- Consequence: a string from input will not match integer arms; cast to the arm's type first or write arms of the matching type.
- This also avoids `switch`'s loose false matches (`0 == "abc"`, `"1" == "01"`).
- Keep `switch` for fallthrough (several cases sharing a tail by omitting `break`).
- Keep `switch` when branches are statement blocks with side effects; each `match` arm is a single expression that yields a value.
- Keep `switch` when loose `==` comparison is intended.
- Use `match` for value-returning dispatch; with no matching arm and no `default` it throws `UnhandledMatchError`.
