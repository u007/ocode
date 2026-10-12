---
name: ruby-tuning-step-5-preview-free
description: >
  Corrective Ruby knowledge for step-5-preview-free, targeting its gaps in
  argument passing: the Ruby 3.0 keyword/positional separation and the rules
  for `*` and `**` in method definitions, calls and destructuring.
when_to_use: >
  Load when the provider-stripped model id (see stack-detection.md) resolves to
  exactly `step-5-preview-free` AND the repository is a Ruby project (Gemfile
  or *.gemspec present, or *.rb files — per meta.yaml detection). For any other
  model or non-Ruby repo, do not load.
tuned_for: step-5-preview-free
tuned_version: "5-preview"
stack: ruby
source_scorecard: ../scores/step-5-preview-free.md
threshold: 0.85
revalidate_when: model_version changes
---
# Ruby corrections for step-5-preview-free

## Collections and argument-passing idioms

- Ruby 3.0 separated keyword arguments from positional Hashes in ONE direction only: a Hash passed positionally to a method that declares keywords (`def m(key:)` or `def m(**opts)`) is no longer converted to keywords and raises `ArgumentError`. Pass it with `m(**hash)`.
- The other direction still works: keywords passed to a method that accepts NO keywords (`def m(*args)` or `def m(a, b)`) are collected into a trailing positional Hash. It does not raise.
- `**opts` coexists with named keywords in the same parameter list: `def m(name:, retries: 3, **rest)` is idiomatic. `rest` receives every keyword not named explicitly.
- `*args` never captures keywords and `**opts` never captures positionals. `def m(*args, **opts, &blk)` and `target(*args, **opts, &blk)` is the forward-everything idiom; `def m(...) = target(...)` forwards all three at once.
- Anonymous `*` and `**` parameters (`def m(*, **)`) may be forwarded bare as `target(*, **)` (3.2+).
- `m(**{})` passes no keywords at all; it does not inject an empty positional Hash.
- `**` is not valid in multiple assignment. `first, **rest = hash` is a SyntaxError. Use the splat for arrays only (`head, *tail = list`) and pattern matching for hashes: `case hash in {a:, **rest}`.
- `Hash.new(0)` returns one shared default and stores nothing on read; `Hash.new { |h, k| h[k] = [] }` stores a fresh object per missing key, so it does create keys and changes `size`/`key?`. State this rule plainly; do not append alternative idioms you cannot verify.
- When unsure of a syntax edge case, write the one form you know runs; do not list a speculative variant followed by a retraction in the same answer.
