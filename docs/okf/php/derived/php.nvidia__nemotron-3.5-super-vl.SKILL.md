---
name: php-tuning-nvidia-nemotron-3.5-super-vl
description: >
  Corrective PHP knowledge for nvidia/nemotron-3.5-super-vl, targeting its gaps
  in OOP (readonly classes and inheritance, new static() vs new self()) and
  closures (implicit arrow-function capture, what first-class callable syntax
  replaces). Directive rules the model must follow.
when_to_use: >
  Load when the provider-stripped model id (see stack-detection.md) resolves to
  exactly `nvidia/nemotron-3.5-super-vl` (e.g. `openrouter/nvidia/nemotron-3.5-super-vl`
  → `nvidia/nemotron-3.5-super-vl`; the `:free` route suffix is not part of the id)
  AND the repository is a PHP project (composer.json present, or *.php files — per
  meta.yaml detection). For any other model or non-PHP repo, do not load.
tuned_for: nvidia/nemotron-3.5-super-vl
tuned_version: "3.5"
stack: php
source_scorecard: ../scores/nvidia__nemotron-3.5-super-vl.md
threshold: 0.85
revalidate_when: model_version changes
---
# PHP corrections for nvidia/nemotron-3.5-super-vl

<!-- kaizen:digest -->
## OOP: readonly and late static binding

- A `readonly` property must be typed, is written exactly once from inside the declaring class's scope (usually the constructor), and any second write, even inside the class, throws `Error`.
- A `readonly` property is not a constant: each instance receives its value at runtime. It cannot have a default value at declaration.
- A `readonly` class (8.2) makes every declared property readonly, forbids dynamic properties, and requires typed, non-static properties.
- A `readonly` class CAN be extended. The rule is that it may only extend, and be extended by, another `readonly` class. Never say a readonly class is final or unextendable.
- `self` is fixed at compile time to the class where the code is written; `static` resolves at runtime to the class actually called (late static binding).
- The canonical LSB case is instantiation: `new self()` in a parent method always builds the parent, `new static()` builds the called subclass. Write factories, `create()`/`make()` helpers and fluent `with*()` methods with `new static()` / `static::method()` so subclasses get their own type back.

## Closures: implicit capture and first-class callables

- An arrow function `fn () => expr` captures every enclosing variable it uses implicitly, by value, with no `use` clause. That implicit capture is the difference from a closure, not just the missing by-reference option.
- A classic closure `function () use ($x) {}` captures nothing implicitly: every outer variable must be listed in `use (...)`, and is snapshotted by value at definition time unless written `use (&$x)`.
- Arrow functions cannot capture by reference and cannot mutate the outer variable; use a closure with `use (&$x)` for that.
- First-class callable syntax `strlen(...)`, `$obj->method(...)`, `Foo::stat(...)` (8.1) creates a `Closure` without invoking it. Its purpose is to REPLACE string and array callables (`'strlen'`, `[$obj, 'method']`, `'Foo::stat'`): the result is a typed, IDE-analyzable reference that respects visibility, so it works for private methods from inside the class where a string callable would not.
- Prefer `(...)` over string/array callables when passing callbacks to `array_map`, `usort`, event registrations and the like.
<!-- /kaizen:digest -->
