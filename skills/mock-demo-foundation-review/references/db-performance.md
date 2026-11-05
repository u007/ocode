# Database Performance Checklist

A mock runs against ten rows. Audit it against ten million. Every list, search, count
and dashboard tile in the inventory implies a query; write the query down and ask what
makes it fast at scale. Severity follows retrofit cost: wrong grain and missing keys are
expensive, a missing index is cheap.

Contents: 1 Query inventory · 2 Indexing · 3 Lists & pagination · 4 Search · 5 Counts,
totals, dashboards · 6 Write path · 7 Schema shape · 8 Growth & retention · 9 Signals in
the demo code

## 1. Query inventory

- For every screen, list the queries it needs: the list query, its filters, its sort,
  each count or badge, each dropdown's source, each "related" panel.
- For each query name the table, the predicate columns, the sort columns and the
  expected row count at year one and year three.
- A screen that needs more than a handful of queries, or a query that joins more than
  four tables to render a row, is a grain or design finding, not an index finding.

## 2. Indexing

- Every foreign key has an index. Every unique constraint is an index. Every column in
  a list filter or sort has one, composite where the filter and sort combine
  (`(org_id, status, created_at DESC)`).
- Tenant / owner column leads every composite index on tenant-scoped tables.
- Partial indexes for the common narrow case (`WHERE deleted_at IS NULL`,
  `WHERE status = 'open'`).
- Match columns for duplicate detection (`email_normalized`, `name_key`) are indexed;
  trigram or full-text indexes where "contains" search is a real requirement.
- No index on a column nobody filters by; write amplification is a cost too.
- Name the index for each query in §1 or mark the query as a finding.

## 3. Lists & pagination

- Server-side pagination on every list; keyset (cursor) pagination for feeds,
  activity logs and anything that grows without bound. Offset paging past page 100 on
  a large table is a finding.
- The sort key is unique (append the id) so pages are stable.
- Filters apply before pagination in SQL, never on a fetched page.
- Row counts are not computed on every page load for large tables; show "1,000+" or
  compute lazily.
- Infinite scroll in the mock still implies a cursor; check what the demo does when
  the list exceeds one page.

## 4. Search

- Decide per field what "search" means: prefix (index works), contains (trigram /
  full-text), fuzzy (dedicated index or external engine).
- Global search across entities is a separate index or a materialized search table,
  not five `LIKE '%x%'` queries.
- Search results are paginated and scoped to the tenant.
- Autocomplete pickers debounce, cap results, and hit an indexed prefix.

## 5. Counts, totals, dashboards

- Every number on a dashboard tile is either a cheap indexed count, a maintained
  counter, or a precomputed aggregate with a stated refresh interval. A `COUNT(*)` over
  a large table per page view is a finding.
- Totals on parent rows ("12 orders, $4,310") are maintained counters with a
  reconciliation job, or computed with an index-covered query, or cached with a TTL;
  the mock must pick one.
- Time-series charts read from pre-bucketed rollups once the fact table is large.
- Reporting queries do not run against the transactional primary at scale: plan a read
  replica or a warehouse feed from the start by keeping ids and timestamps clean (see
  `data-integrity.md` §9).

## 6. Write path

- One statement per logical write; no read-modify-write from the client for counters
  or balances.
- Bulk imports batch inserts and defer non-essential index updates; they do not loop
  single inserts over HTTP.
- Hot rows (a tenant row updated on every event, a global counter) are a finding;
  shard the counter or move it to an event table.
- Long transactions that span user think-time (open a transaction, show a form, commit
  on submit) are a finding; use optimistic versions instead.
- Cascading deletes over large child sets run as jobs, not inline in a request.

## 7. Schema shape

- Wide tables with many nullable columns that only some rows use suggest a missing
  subtype table or a JSON column with a documented shape.
- JSON columns are for genuinely variable data; anything filtered, sorted or joined on
  is a real column with an index.
- Large text and blobs live in their own table or object storage so the hot table
  stays narrow.
- Polymorphic references (`subject_type`, `subject_id`) cannot be foreign keys; accept
  them only for audit and activity tables, never for business relationships.
- Enum columns are small integers or constrained text with a lookup table; strings
  compared by case-insensitive match on every query are a finding.

## 8. Growth & retention

- Identify the tables that grow without bound (events, logs, messages, audit). Each
  needs a retention or archival rule and a partitioning plan (by time or tenant) before
  it is large, because partitioning a live table is the expensive retrofit.
- File and attachment metadata rows stay small; bytes go to object storage.
- Soft-deleted rows are excluded by partial indexes and periodically archived.
- Estimate row counts per table at year one and year three from the PM's volume
  assumptions; put the estimate in the report so the index and partition decisions are
  checkable.

## 9. Signals in the demo code

Patterns that look fine in a prototype and become the first production incident:

- Fetch all then filter / sort / paginate in the browser.
- N+1: a list query followed by one query per row for a related name, count or status.
- `SELECT *` into a UI that shows three columns.
- A "search" that is `LIKE '%term%'` on an unindexed column.
- A dashboard that recounts every table on mount.
- Mock data served from a JSON file or in-memory array with no notion of pages.
- Client-side state as the only cache for data the server should own.
- Dates stored or compared as strings.

Each of these is a finding only if the plan is to keep the code; if the demo is a
throwaway, note the pattern in the foundation contract so the real build avoids it.
