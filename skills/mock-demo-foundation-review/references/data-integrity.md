# Data Integrity Checklist

Audit the model derived from the mock against every section. Each unmet item is a
finding; severity follows retrofit cost (identity first, polish last).

Contents: 1 Identity · 2 Duplicate prevention · 3 Ownership & tenancy · 4 Referential
integrity · 5 Lifecycle & state · 6 Audit & time · 7 Column discipline · 8 Concurrency &
idempotency · 9 Reporting readiness · 10 Growth path

## 1. Identity

- Every entity has a stable surrogate primary key (UUIDv7 / ULID / bigint). Never a
  name, email, or anything a user can edit.
- Every entity also names its **natural key**: the real-world fact that makes two rows
  the same thing (email for a person, tax id for a company, SKU for a product, date +
  location + subject for an event). If no natural key can be named, the entity is
  underspecified; that is a finding.
- IDs are exposed in URLs, exports and APIs so other systems can refer back to a row.
- IDs never change on edit, merge, or import.

## 2. Duplicate prevention

The most expensive thing to fix later. Demand all layers:

- **Database unique constraint** on the natural key, scoped to the tenant when there is
  one (`UNIQUE (org_id, lower(email))`). A uniqueness rule that lives only in UI code or
  application code does not exist.
- **Normalized match columns**: store a canonical form beside the display form
  (`email_normalized`, `phone_e164`, `name_key` = trimmed, case-folded, diacritics and
  punctuation removed). Constrain the canonical column, show the original.
- **Search before create**: a create form for a keyed entity looks up existing matches as
  the user types the key field and offers "use existing" before "create new".
- **Soft match on submit**: near matches (same phone, same name + postcode, Levenshtein
  on name) produce a "possible duplicate" interstitial, not a silent insert and not a
  hard block.
- **Reference by id, never by text**: every place the UI links to another entity is a
  picker bound to the id, never a free-text field that stores a name.
- **Lookup values are tables or enums, not free text**: status, category, country,
  currency, unit. Free text here is how "USA", "U.S.A.", "United States" and "us" become
  four countries.
- **Merge path exists**: a way to merge two records and keep every foreign key pointing
  at the survivor, with a `merged_into_id` on the loser and a log entry. Without it,
  every duplicate that slips through is permanent.
- **Imports and APIs go through the same uniqueness rules** as the form. Bulk paths are
  where most duplicates are born.

## 3. Ownership & tenancy

- Every row that belongs to someone has an owner column (`org_id`, `account_id`,
  `user_id`) from the first migration, even in a single-tenant first release. Adding it
  to a populated table later means backfilling guesses.
- Every unique constraint and every list query is scoped by that column.
- Row-level access is enforced server-side; the UI hiding a row is not access control.

## 4. Referential integrity

- Every relationship the UI implies is a real foreign key with a declared delete rule
  (`RESTRICT` by default; `CASCADE` only for true child rows such as line items).
- Nothing is "linked" by storing a name, code or email of the other record.
- Many-to-many relationships are join tables with their own unique constraint on the
  pair.
- Orphans are impossible by construction, not cleaned up by a cron job.

## 5. Lifecycle & state

- Status is an explicit column with a closed set of values and a documented transition
  table. Buttons in the mock reveal the transitions; write them down.
- Terminal states (cancelled, archived, deleted) are states, not row deletions.
- Soft delete (`deleted_at`) for anything referenced elsewhere or needed in reporting.
  Hard delete only for true PII erasure, and then via a documented path.
- "Active" is derived from state, not a separate boolean that can disagree with it.

## 6. Audit & time

- `created_at`, `updated_at` on every table, set by the database or the write layer,
  never by the client.
- `created_by`, `updated_by` on every user-editable table.
- All timestamps stored in UTC with timezone awareness; the user's zone is a display
  concern. Dates without a time (birthday, due date) are `date`, not midnight timestamps.
- Financial, legal or compliance-relevant changes get an append-only history or event
  table (who, what, before, after, when). A mock with an "activity" or "history" panel
  implies this; one without it still needs it for money and permissions.

## 7. Column discipline

Types, lengths and enums first; they are cheap at creation and painful to tighten once
rows exist (every tightening is a backfill plus a failed-migration risk).

- **The column type is the narrowest type that holds the real values**: integer for
  counts, numeric for measured quantities, boolean for flags, date / timestamptz for
  time, uuid for ids, jsonb only for documented variable shapes. A text column holding
  numbers, dates, booleans or ids is a finding; it defeats indexing, sorting,
  validation and reporting at once.
- **Every text column has a length limit** sized to the real domain (names ~200,
  emails 320, codes ~50, URLs 2048, notes a stated cap). Unbounded text on every column
  means no input is ever rejected and nothing bounds the row. Keep genuinely long
  content (descriptions, bodies) in a deliberately unbounded column or its own table.
- **Closed value sets are enforced by the database**: a native enum type where the
  engine supports it (PostgreSQL `CREATE TYPE … AS ENUM`, MySQL `ENUM`), otherwise a
  `CHECK (status IN (…))` constraint or a foreign key to a lookup table. Pick by how
  often the set changes: native enum or check for sets that change with code (status,
  type), lookup table for sets users or admins manage (categories, regions). A text
  column with the allowed values only in application code is a finding.
- **Check constraints for the obvious ranges**: non-negative quantities, percentages in
  0 to 100, end after start, a positive amount on a debit. They cost nothing and catch
  the bug the form missed.
- **Defaults are set in the schema** for columns that have a natural default (status
  on create, `created_at`, booleans), so imports and other clients get the same values.
- Money is an integer minor unit plus a currency code, never a float.
- Quantities carry a unit. Percentages state their base.
- Nullable means "legitimately unknown", not "we did not think about it". Required in
  the form means `NOT NULL` in the table, and vice versa.
- Names are split only if the product needs the parts; never guess a split from a full
  name, never force a split on cultures that do not use it.
- Phone numbers are stored in E.164 beside the user's entry. Addresses are structured
  or a single blob, consciously chosen, not half of each.
- Free-text fields have a length limit and a stated purpose.
- Derived values (totals, counts, "last order date") are computed, or cached with a
  documented refresh rule. A mock showing a total next to a list does not imply a
  stored column.
- Files: store a reference (bucket + key + content type + size + checksum), never the
  bytes in the row, never only a URL that can expire.

## 8. Concurrency & idempotency

- Edit forms carry a version (or `updated_at`) and the save rejects a stale version.
  Last-write-wins silently destroys other users' edits.
- Create and payment submits are idempotent: a client-generated idempotency key or a
  natural-key constraint makes a double-click or a retried request a no-op.
- Counters and balances are updated with atomic statements or ledgers, never
  read-modify-write from the client.

## 9. Reporting readiness

The analyst lens. A model that cannot answer "how many, by what, over time" forces a
second data model later.

- Every fact row has an event time distinct from its row-creation time when they differ
  (order placed vs row inserted).
- Dimensions that reports will group by (category, region, channel, owner, status) are
  ids into lookup tables, not free text.
- Source / origin is recorded on rows that can arrive by several paths (form, import,
  API, migration). Without it, data quality problems cannot be traced.
- Nothing needed for a report lives only in a JSON blob, a note field, or the UI state.
- Deleted and merged rows remain countable (soft delete, merge log).
- Slowly changing facts (a customer's plan, an employee's team) are history tables or
  effective-dated rows when the report must show "as of then", not "as of now".

## 10. Growth path

Do not demand these today; demand that nothing in the model forbids them tomorrow.

- A second client (mobile, API consumer, import job) can write without bypassing rules
  because the rules are in the database and the write layer, not the form.
- A second tenant can be added because every row already has an owner.
- A search index or warehouse can be fed because rows have stable ids, timestamps and
  typed dimensions.
- Migrations are forward-only files in version control from the first table.
- Configuration that will vary by customer (labels, statuses, required fields) is
  modeled as data when the product clearly heads that way; otherwise kept in code and
  noted as a known future change.
