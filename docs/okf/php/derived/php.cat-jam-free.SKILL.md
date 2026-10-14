---
name: php-tuning-cat-jam-free
description: >
  Corrective PHP knowledge for cat-jam-free, targeting its gaps in OOP:
  readonly properties and classes, and late static binding.
when_to_use: >
  Load when the provider-stripped model id (see stack-detection.md) resolves to
  exactly `cat-jam-free` AND the repository is a PHP project (composer.json
  present, or *.php files — per meta.yaml detection). For any other model or
  non-PHP repo, do not load.
tuned_for: cat-jam-free
tuned_version: "alpha"
stack: php
source_scorecard: ../scores/cat-jam-free.md
threshold: 0.85
revalidate_when: model_version changes
---
# PHP corrections for cat-jam-free

## OOP: readonly and late static binding

- A `readonly` property CANNOT have a default value at declaration (`public readonly int $x = 1;` is a compile error). It is initialized at runtime, once, from inside the declaring class's scope, usually in the constructor.
- A `readonly` property must be typed. Any second write, even inside the class, throws `Error`.
- A `readonly` property is not a constant: each instance gets its value at runtime.
- A `readonly` class (8.2) makes every declared property readonly, forbids dynamic properties, and requires typed, non-static properties.
- A `readonly` class may only extend, and be extended by, another `readonly` class.
- `self` is fixed at compile time to the class where the code is written. `static` resolves at runtime to the class actually called (late static binding).
- `new self()` in a parent method always builds the parent; `new static()` builds the called subclass. Use `new static()` / `static::method()` in factories and fluent methods meant to work for subclasses.
