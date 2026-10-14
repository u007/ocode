---
name: ror-tuning-cat-jam-free
description: Corrective Rails transaction guidance for cat-jam-free — how exceptions roll back a transaction block (and how ActiveRecord::Rollback differs), and when to use save!/create! instead of save/create. Loaded only in Rails repos when this exact model is active.
when_to_use: The active model id, provider-stripped, is exactly `cat-jam-free` AND the repo uses Ruby on Rails (see docs/okf/_schema/stack-detection.md). Do not load for other models or non-Rails repos.
# --- Kaizen metadata ---
tuned_for: cat-jam-free
tuned_version: "alpha"
stack: ror
source_scorecard: ../scores/cat-jam-free.md
threshold: 0.85
revalidate_when: model_version changes   # STALE on any version bump — re-benchmark
---
# Rails tuning — cat-jam-free

## Callbacks & transactions

- **Any exception that escapes an `ActiveRecord::Base.transaction do … end`
  block rolls the transaction back AND re-raises to the caller.** That is the
  normal way a transaction aborts: a `save!`/`create!`/`update!` that fails, or
  any other raised error, undoes every write in the block. Do not present
  `raise ActiveRecord::Rollback` as the only way to force a rollback.
- **`ActiveRecord::Rollback` is the one exception Rails swallows.** It also rolls
  back, but the transaction block catches it and returns `nil` without
  re-raising, so the caller never sees an error. Use it only for a deliberate
  silent abort; when the caller must know the work failed, raise a real error.
- **`save` vs `save!`: the choice is about control flow, not taste.** `save`
  returns `false` (errors in `record.errors`) and the caller must branch on it —
  the controller pattern (`if @post.save … else render :new, status:
  :unprocessable_entity`). Use `save!`/`create!`/`update!` when a failure must
  ABORT the surrounding flow — inside a `transaction` block or a service object —
  because a non-bang `save` returning `false` there does not roll anything back
  and the remaining writes still commit.
