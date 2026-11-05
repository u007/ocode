# Seeds & Demo Data Checklist

Seeds are the first rows the real schema ever holds, and demo seeds are what every
stakeholder judges the product by. Two rules: a seed run is idempotent (run it ten
times, get one copy), and the demo seed set covers every screen the mock shows.

Contents: 1 Idempotency · 2 Reference data vs demo data · 3 Demo coverage · 4 Seed
shape · 5 Environments · 6 Signals in the demo code

## 1. Idempotency

- Every seed row has a deterministic natural key or a fixed id so a re-run can find it.
  Random ids, `now()` timestamps as keys, or "insert and hope" are findings.
- The seed runner looks the row up by that key, then inserts if absent or updates if
  present, as two explicit statements. Do not rely on a blanket upsert-on-conflict: it
  hides a missing unique constraint (the insert "succeeds" twice because nothing
  conflicts) and silently overwrites columns the user edited. The lookup makes the
  missing constraint visible and the overwrite a decision.
- The unique constraints from `data-integrity.md` §2 are the backstop: with them in
  place a buggy seed fails loudly instead of doubling the data. A seed that runs twice
  and produces two rows proves the constraint is missing; treat it as a schema finding,
  not a seed bug.
- Relationships in seeds reference parents by natural key, resolved at run time, never
  by hard-coded surrogate ids that differ per environment.
- A seed run is one transaction per logical set, so a failure halfway leaves nothing
  partial.
- Seeds that create users set passwords only from an environment secret or a known
  demo value that is documented as demo-only and refused in production.
- Re-running a seed never resets rows a user changed unless that is the documented
  behaviour of a named "reset demo" command.

## 2. Reference data vs demo data

Keep the two sets separate; they have different lifecycles.

- **Reference data** (statuses, roles, permissions, countries, currencies, units,
  categories the product ships with): required in every environment, versioned with the
  schema, idempotent by code. Treat changes as migrations.
- **Demo data** (sample customers, orders, activity): optional, loaded only in dev,
  preview and demo environments, deletable as a set.
- Every demo row carries a marker (`is_demo`, a demo tenant id, or a `source =
  'seed'` value) so it can be listed, excluded from reports and removed in one query.
- Reference data must never depend on demo data; demo data may depend on reference
  data.

## 3. Demo coverage

A demo that shows an empty list on one screen or a stubbed chart on another loses the
room. Map the inventory to the seed set:

- Every screen in the inventory has enough rows to show its list, its filters and its
  pagination (more than one page where pagination exists).
- Every status and every enum value appears at least once so each badge, filter option
  and transition is visible.
- Every role from the RBAC matrix (`security-rbac.md` §2) has a demo user, so the
  demo can switch perspectives.
- Every relationship is exercised: a customer with no orders and one with many; a
  parent with children; an archived and a merged record.
- Edge cases are present on purpose: long names, unicode, a record created a year ago,
  a future-dated item, zero and negative amounts where valid, a near-duplicate pair for
  the duplicate-detection demo.
- Dashboard and report tiles have enough history to draw a trend, not a single point.
- The demo narrative is written down: which records to open in which order to tell the
  story, so anyone can run the demo.

## 4. Seed shape

- Seeds go through the same write path and the same validation as the form (service
  layer or a loader that calls it), not raw SQL that bypasses rules. Raw SQL is
  acceptable only for reference data that the write path cannot express.
- Seed definitions are data files (YAML/JSON/CSV) or typed fixtures, reviewed like code,
  with the natural key explicit per row.
- Timestamps in demo data are relative to "now" at run time so the demo never ages
  into "everything happened in 2024".
- Generated bulk data (for performance demos) uses a fixed random seed so runs are
  reproducible.
- No real personal data in seeds, ever; use obviously fictional names and reserved
  example domains.

## 5. Environments

- Three commands exist and are named in the report: load reference data (safe
  everywhere), load demo data (refused in production unless a demo tenant is explicitly
  enabled), reset demo data (deletes by marker, reloads).
- Production runs reference seeds as part of deploy, after migrations, idempotently.
- Test suites seed from the same definitions so tests exercise real reference data.
- A fresh clone can reach a running demo with one documented command; if it cannot,
  that is a finding.

## 6. Signals in the demo code

- Mock data hard-coded in components or a JSON file the UI reads directly: nothing to
  seed, nothing to validate, and the model was never exercised.
- A `seed.sql` of bare `INSERT` statements with literal ids: runs once, then fails or
  doubles.
- Seeds that `DELETE FROM` every table first: cannot run against a shared or
  production database and hide missing idempotency.
- Seeds that bypass validation and so create rows the form could never create.
- No demo user for one of the roles the UI shows.
- The same sample row duplicated under slightly different names because the author
  copied and edited: the duplicate problem, already present before launch.
