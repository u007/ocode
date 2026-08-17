---
name: ror-tuning-mimo-v2.5
description: Corrective Rails migrations/schema guidance for the exact gaps mimo-v2.5 tests weak on — the purpose of the schema.rb/structure.sql dump, and the two classic dangerous-migration patterns on large production tables (locking column defaults/NOT NULL, and index builds). Loaded only in Rails repos when this exact model is active.
when_to_use: The active model id, provider-stripped, is exactly `mimo-v2.5` AND the repo uses Ruby on Rails (see docs/okf/_schema/stack-detection.md). Do not load for other models or non-Rails repos.
# --- Kaizen metadata ---
tuned_for: mimo-v2.5
tuned_version: "1"
stack: ror
source_scorecard: ../scores/mimo-v2.5.md
threshold: 0.75
revalidate_when: model_version changes   # STALE on any version bump — re-benchmark
---

# Rails tuning — mimo-v2.5

> Generated from `../scores/mimo-v2.5.md` (corpus_rev 1). Covers **only**
> `migrations-schema` (0.72), the sole tag this exact model scored below 0.75
> on. It says nothing about activerecord, querying, callbacks-transactions,
> controllers-routing, views-helpers, concerns-services, or caching-jobs — the
> model already handles those well (all ≥ 0.94), and restating them would
> waste prompt/cache budget.

## Migrations & schema (weak: migrations-schema 0.72)

- **`schema.rb` and `structure.sql` are both the canonical schema *dump*, not
  documentation.** Their job is to let `db:schema:load` build a fresh
  development/test database directly from that dump, without replaying the
  full migration history. State this purpose explicitly, not just "one is
  Ruby, one is SQL" — the DSL-vs-SQL distinction is *why* you switch formats,
  not *what* either file is for.

- **Name BOTH classic dangerous-migration patterns on a large production
  table, not just one:**
  1. Adding a column with a `default:` or `null: false` constraint (or adding
     `NOT NULL` to an existing column with data) can trigger a full
     table-rewrite/table-scan and lock the table. Safe approach: add the
     column nullable, backfill the data in batches, then add the constraint
     as a separate migration.
  2. **Building an index takes a write lock for the duration of the build.**
     Safe approach on Postgres: `add_index :table, :column, algorithm:
     :concurrently` combined with `disable_ddl_transaction!` at the top of the
     migration class, so the index builds without blocking writes. Do not
     substitute an unrelated risk (e.g. "removing a column") for this one —
     the index-build lock is the specific second foot-gun to name, distinct
     from the column-default/NOT NULL case.

---

*Regenerate this file whenever `mimo-v2.5`'s version changes or the `ror`
corpus revision bumps.*
