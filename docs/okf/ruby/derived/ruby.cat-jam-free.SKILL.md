---
name: ruby-tuning-cat-jam-free
description: >
  Corrective Ruby knowledge for cat-jam-free, targeting its gaps in object
  method mechanics: the method_missing contract and private/protected
  visibility rules.
when_to_use: >
  Load when the provider-stripped model id (see stack-detection.md) resolves to
  exactly `cat-jam-free` AND the repository is a Ruby project (Gemfile
  or *.gemspec present, or *.rb files — per meta.yaml detection). For any other
  model or non-Ruby repo, do not load.
tuned_for: cat-jam-free
tuned_version: "alpha"
stack: ruby
source_scorecard: ../scores/cat-jam-free.md
threshold: 0.85
revalidate_when: model_version changes
---
# Ruby corrections for cat-jam-free

## Objects and methods: method_missing and visibility

- A correct `method_missing(name, *args, &block)` handles only the names it recognizes and calls `super` for everything else, so unknown calls still raise `NoMethodError`. Whenever you explain or write `method_missing`, state the `super` fallthrough explicitly.
- Pair every `method_missing` with `respond_to_missing?(name, include_private = false)` that returns true for exactly the handled names (and `super` otherwise), so `respond_to?` and `method(:name)` stay honest.
- Private methods cannot be called with an explicit receiver other than `self`. Since Ruby 2.7, `self.foo` is allowed for ANY private method (not only setters); `other.foo` still raises `NoMethodError`. Do not say `self.` is forbidden.
- Protected methods may be called with an explicit receiver, but only from inside an instance method of the same class or a subclass (the caller's context decides), typically to compare two instances (`other.value`).
- Methods are public by default; state that when contrasting the three visibilities.
